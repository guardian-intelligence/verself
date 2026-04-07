// Package fmotel initializes OpenTelemetry trace and log providers for
// forge-metal Go services. Each service calls Init once at startup; the
// returned shutdown function flushes pending spans/logs on exit.
//
// Traces and logs are exported via OTLP gRPC to the local OTel Collector
// (default 127.0.0.1:4317) which forwards to ClickHouse.
package fmotel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config controls the OTel providers. Only ServiceName is required.
type Config struct {
	ServiceName    string
	ServiceVersion string
	OTLPEndpoint   string // gRPC endpoint; default "127.0.0.1:4317"
}

// Init sets up trace and log providers, registers the global TracerProvider
// and W3C TraceContext propagator, and returns:
//   - a shutdown function that flushes pending data
//   - an slog.Logger bridged to OTel (trace_id/span_id injected automatically
//     when callers use slog.InfoContext(ctx, ...))
func Init(ctx context.Context, cfg Config) (shutdown func(context.Context) error, logger *slog.Logger, err error) {
	endpoint := cfg.OTLPEndpoint
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
		sdktrace.WithBatcher(traceExp),
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
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
		sdklog.WithResource(res),
	)

	// --- Register globals ---

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// --- slog bridge ---
	// The OTel Log SDK automatically extracts trace_id and span_id from the
	// context passed to slog.*Context methods, so callers get trace-log
	// correlation for free.
	//
	// The resolveHandler wrapper fixes a gap in otelslog's convertValue:
	// named string types (e.g. stripe.EventType) are slog KindAny values
	// whose reflect.Kind is String, but the bridge only handles concrete
	// string — not named types — so they render as "unhandled: (T) v".

	logger = slog.New(&resolveHandler{otelslog.NewHandler(cfg.ServiceName,
		otelslog.WithLoggerProvider(lp),
	)})

	shutdown = func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), lp.Shutdown(ctx))
	}
	return shutdown, logger, nil
}

// resolveHandler wraps an slog.Handler to coerce named string types
// (reflect.Kind == String but not string) into plain slog.StringValue
// before the otelslog bridge sees them.
type resolveHandler struct {
	slog.Handler
}

func (h *resolveHandler) Handle(ctx context.Context, r slog.Record) error {
	resolved := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		resolved.AddAttrs(resolveAttr(a))
		return true
	})
	return h.Handler.Handle(ctx, resolved)
}

func (h *resolveHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	ra := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		ra[i] = resolveAttr(a)
	}
	return &resolveHandler{h.Handler.WithAttrs(ra)}
}

func (h *resolveHandler) WithGroup(name string) slog.Handler {
	return &resolveHandler{h.Handler.WithGroup(name)}
}

func resolveAttr(a slog.Attr) slog.Attr {
	if a.Value.Kind() == slog.KindAny {
		if v := a.Value.Any(); v != nil {
			if reflect.TypeOf(v).Kind() == reflect.String {
				return slog.String(a.Key, reflect.ValueOf(v).String())
			}
		}
	}
	return a
}
