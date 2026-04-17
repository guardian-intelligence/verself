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
	"strings"

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
	otel.SetTextMapPropagator(textMapPropagator())

	// The OTel Log SDK automatically extracts trace_id and span_id from the
	// context passed to slog.*Context methods, so callers get trace-log
	// correlation for free. Named-string types are handled by implementing
	// slog.LogValuer at their definition sites, not by a wrapping handler.
	logger = slog.New(otelslog.NewHandler(cfg.ServiceName,
		otelslog.WithLoggerProvider(lp),
	))

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

// baggageSpanProcessor copies W3C baggage members with key prefix
// `forge_metal.` onto every started span as span attributes with the same
// key and value. It is the single projection point for observability
// correlation — services wire otelhttp + otlptracegrpc, the processor
// does the rest.
type baggageSpanProcessor struct{}

func (baggageSpanProcessor) OnStart(parentCtx context.Context, span sdktrace.ReadWriteSpan) {
	bag := baggage.FromContext(parentCtx)
	for _, m := range bag.Members() {
		key := m.Key()
		if !strings.HasPrefix(key, BaggageAttributePrefix) {
			continue
		}
		span.SetAttributes(attribute.String(key, m.Value()))
	}
}

func (baggageSpanProcessor) OnEnd(sdktrace.ReadOnlySpan)            {}
func (baggageSpanProcessor) Shutdown(context.Context) error         { return nil }
func (baggageSpanProcessor) ForceFlush(context.Context) error       { return nil }
