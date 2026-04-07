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

	logger = slog.New(otelslog.NewHandler(cfg.ServiceName,
		otelslog.WithLoggerProvider(lp),
	))

	shutdown = func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), lp.Shutdown(ctx))
	}
	return shutdown, logger, nil
}
