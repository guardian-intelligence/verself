package vmorchestrator

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func detachedTraceContext(ctx context.Context) context.Context {
	out := context.Background()
	if ctx == nil {
		return out
	}
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.IsValid() {
		out = trace.ContextWithSpanContext(out, spanContext)
	}
	if bg := baggage.FromContext(ctx); bg.Len() > 0 {
		out = baggage.ContextWithBaggage(out, bg)
	}
	return out
}

func startStepSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, func(error)) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, span := tracer.Start(ctx, name, trace.WithAttributes(attrs...))
	return ctx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
}
