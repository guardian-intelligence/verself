// Package fmotel initializes OpenTelemetry trace and log providers for
// forge-metal Go services. Each service calls Init once at startup; the
// returned shutdown function flushes pending spans/logs on exit.
//
// Traces and logs are exported via OTLP gRPC. Endpoint selection follows
// the OTel SDK default: OTEL_EXPORTER_OTLP_ENDPOINT if set, otherwise
// 127.0.0.1:4317.
//
// Every span created under a context carrying W3C baggage members with
// key prefix `forge_metal.` receives those members as span attributes
// (e.g. baggage `forge_metal.deploy_id=X` → span attribute
// `forge_metal.deploy_id=X`). Services and callers do not wire this per
// endpoint — the SpanProcessor registered here does it for the whole
// TracerProvider.
package fmotel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// BaggageAttributePrefix is copied onto every span from W3C baggage members
// whose key starts with this prefix. Kept exported so tests can assert the
// contract.
const BaggageAttributePrefix = "forge_metal."

const telemetryExportInterval = time.Second

// Config controls the OTel providers. Only ServiceName is required.
type Config struct {
	ServiceName    string
	ServiceVersion string
}

// Init sets up trace and log providers, registers the global TracerProvider
// and W3C TraceContext+Baggage propagator, and returns:
//   - a shutdown function that flushes pending data
//   - an slog.Logger bridged to OTel (trace_id/span_id injected automatically
//     when callers use slog.InfoContext(ctx, ...))
func Init(ctx context.Context, cfg Config) (shutdown func(context.Context) error, logger *slog.Logger, err error) {
	// Honor OTEL_EXPORTER_OTLP_ENDPOINT transparently; otlptracegrpc/otlploggrpc
	// read it by default, but the exporter spec treats the env value as a URL
	// ("http://host:port") while WithEndpoint wants "host:port". If the user
	// set the env var with a scheme, strip it; otherwise fall through to the
	// SDK default (127.0.0.1:4317).
	endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	endpoint = strings.TrimPrefix(endpoint, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")
	endpoint = strings.TrimSuffix(endpoint, "/")
	if endpoint == "" {
		endpoint = "127.0.0.1:4317"
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("otel: build resource: %w", err)
	}

	// --- Trace provider ---

	traceExp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("otel: trace exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(baggageSpanProcessor{}),
		sdktrace.WithBatcher(traceExp, sdktrace.WithBatchTimeout(telemetryExportInterval)),
		sdktrace.WithResource(res),
	)

	// --- Log provider ---

	logExp, err := otlploggrpc.New(ctx,
		otlploggrpc.WithEndpoint(endpoint),
		otlploggrpc.WithInsecure(),
	)
	if err != nil {
		_ = tp.Shutdown(ctx)
		return nil, nil, fmt.Errorf("otel: log exporter: %w", err)
	}

	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp, sdklog.WithExportInterval(telemetryExportInterval))),
		sdklog.WithResource(res),
	)

	// --- Register globals ---

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(textMapPropagator())

	// The OTel Log SDK automatically extracts trace_id and span_id from the
	// context passed to slog.*Context methods, so callers get trace-log
	// correlation for free. The redacting wrapper is a last-resort guard for
	// bearer/JWT-shaped material that should never be handed to logging in the
	// first place.
	logger = slog.New(redactingHandler{next: otelslog.NewHandler(cfg.ServiceName,
		otelslog.WithLoggerProvider(lp),
	)})

	shutdown = func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), lp.Shutdown(ctx))
	}
	return shutdown, logger, nil
}

func textMapPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

const redactedSecret = "[redacted:secret]"

var (
	bearerValuePattern = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	jwtValuePattern    = regexp.MustCompile(`\b[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)
)

type redactingHandler struct {
	next slog.Handler
}

func (h redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h redactingHandler) Handle(ctx context.Context, record slog.Record) error {
	redacted := slog.NewRecord(record.Time, record.Level, redactString(record.Message), record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		redacted.AddAttrs(redactAttr(attr))
		return true
	})
	return h.next.Handle(ctx, redacted)
}

func (h redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		redacted = append(redacted, redactAttr(attr))
	}
	return redactingHandler{next: h.next.WithAttrs(redacted)}
}

func (h redactingHandler) WithGroup(name string) slog.Handler {
	return redactingHandler{next: h.next.WithGroup(name)}
}

func redactAttr(attr slog.Attr) slog.Attr {
	if attr.Key == "" {
		return attr
	}
	value := attr.Value.Resolve()
	if sensitiveLogKey(attr.Key) {
		return slog.String(attr.Key, redactedSecret)
	}
	attr.Value = redactValue(value)
	return attr
}

func redactValue(value slog.Value) slog.Value {
	value = value.Resolve()
	switch value.Kind() {
	case slog.KindString:
		return slog.StringValue(redactString(value.String()))
	case slog.KindGroup:
		attrs := value.Group()
		redacted := make([]slog.Attr, 0, len(attrs))
		for _, attr := range attrs {
			redacted = append(redacted, redactAttr(attr))
		}
		return slog.GroupValue(redacted...)
	case slog.KindAny:
		if value.Any() == nil {
			return value
		}
		text := fmt.Sprint(value.Any())
		redacted := redactString(text)
		if redacted != text {
			return slog.StringValue(redacted)
		}
		return value
	default:
		return value
	}
}

func redactString(value string) string {
	value = bearerValuePattern.ReplaceAllString(value, "Bearer [redacted:jwt]")
	return jwtValuePattern.ReplaceAllString(value, "[redacted:jwt]")
}

func sensitiveLogKey(key string) bool {
	normalized := strings.ToLower(strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(strings.TrimSpace(key)))
	switch normalized {
	case "authorization", "proxy_authorization", "cookie", "set_cookie", "token", "jwt", "access_token", "id_token", "refresh_token", "client_secret", "password", "passwd", "secret":
		return true
	default:
		return strings.HasSuffix(normalized, "_token") ||
			strings.HasSuffix(normalized, "_jwt") ||
			strings.HasSuffix(normalized, "_secret") ||
			strings.HasSuffix(normalized, "_password")
	}
}

// baggageSpanProcessor copies W3C baggage members with key prefix
// `forge_metal.` onto every started span as span attributes with the same
// key and value. It is the single projection point for observability
// correlation — services wire otelhttp + otlptracegrpc, the processor
// does the rest.
type baggageSpanProcessor struct{}

func (baggageSpanProcessor) OnStart(parentCtx context.Context, span sdktrace.ReadWriteSpan) {
	members := baggage.FromContext(parentCtx).Members()
	if len(members) == 0 {
		return
	}
	attrs := make([]attribute.KeyValue, 0, len(members))
	for _, m := range members {
		key := m.Key()
		if !strings.HasPrefix(key, BaggageAttributePrefix) {
			continue
		}
		attrs = append(attrs, attribute.String(key, m.Value()))
	}
	if len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
}

func (baggageSpanProcessor) OnEnd(sdktrace.ReadOnlySpan)      {}
func (baggageSpanProcessor) Shutdown(context.Context) error   { return nil }
func (baggageSpanProcessor) ForceFlush(context.Context) error { return nil }
