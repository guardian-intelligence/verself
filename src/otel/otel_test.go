package verselfotel

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// TestBaggageSpanProcessorProjectsVerselfMembers is the contract under
// which every service span in the repo gets verself.* attributes
// without per-service wiring. If this breaks, deploy correlation queries
// return zero rows.
func TestBaggageSpanProcessorProjectsVerselfMembers(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(baggageSpanProcessor{}),
		sdktrace.WithSpanProcessor(recorder),
	)
	t.Cleanup(func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	})

	deployID, err := baggage.NewMember("verself.deploy_id", "deploy-1")
	if err != nil {
		t.Fatalf("deploy member: %v", err)
	}
	unrelated, err := baggage.NewMember("other.noise", "drop-me")
	if err != nil {
		t.Fatalf("unrelated member: %v", err)
	}
	bag, err := baggage.New(deployID, unrelated)
	if err != nil {
		t.Fatalf("baggage: %v", err)
	}
	ctx := baggage.ContextWithBaggage(context.Background(), bag)

	tracer := tp.Tracer("test")
	_, rootSpan := tracer.Start(ctx, "root")
	rootCtx := trace.ContextWithSpan(ctx, rootSpan)
	_, childSpan := tracer.Start(rootCtx, "child")
	childSpan.End()
	rootSpan.End()

	ended := recorder.Ended()
	if len(ended) != 2 {
		t.Fatalf("ended spans = %d, want 2", len(ended))
	}
	for _, s := range ended {
		if got := findAttr(s.Attributes(), "verself.deploy_id"); got != "deploy-1" {
			t.Fatalf("span %q: verself.deploy_id = %q, want deploy-1", s.Name(), got)
		}
		for _, a := range s.Attributes() {
			if string(a.Key) == "other.noise" {
				t.Fatalf("span %q: unexpected non-verself attribute leaked", s.Name())
			}
		}
	}
}

func TestTextMapPropagatorIncludesTraceContextAndBaggage(t *testing.T) {
	fields := textMapPropagator().Fields()
	if !containsAll(fields, "traceparent", "tracestate", "baggage") {
		t.Fatalf("fields = %v, want traceparent/tracestate/baggage", fields)
	}

	member, err := baggage.NewMember("verself.deploy_id", "deploy-1")
	if err != nil {
		t.Fatalf("new baggage member: %v", err)
	}
	bag, err := baggage.New(member)
	if err != nil {
		t.Fatalf("new baggage: %v", err)
	}
	ctx := baggage.ContextWithBaggage(context.Background(), bag)
	traceID, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	if err != nil {
		t.Fatalf("trace id: %v", err)
	}
	spanID, err := trace.SpanIDFromHex("0102030405060708")
	if err != nil {
		t.Fatalf("span id: %v", err)
	}
	ctx = trace.ContextWithSpanContext(ctx, trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	}))

	carrier := propagation.MapCarrier{}
	textMapPropagator().Inject(ctx, carrier)
	if got := carrier.Get("traceparent"); got == "" {
		t.Fatal("expected traceparent to be injected")
	}
	if got := carrier.Get("baggage"); got == "" {
		t.Fatal("expected baggage to be injected")
	}
}

func findAttr(attrs []attribute.KeyValue, key string) string {
	for _, a := range attrs {
		if string(a.Key) == key {
			return a.Value.AsString()
		}
	}
	return ""
}

func containsAll(values []string, want ...string) bool {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	for _, value := range want {
		if _, ok := set[value]; !ok {
			return false
		}
	}
	return true
}
