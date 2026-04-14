package fmotel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestCorrelationFromHeadersSanitizesAndMaps(t *testing.T) {
	headers := http.Header{}
	deployID := strings.ToUpper(uuid.NewString())
	headers.Set(headerDeployID, deployID)
	headers.Set(headerDeployRunKey, " 2026-04-11.000137@controller-a ")
	headers.Set(headerTaskTemplateID, "template-123")
	headers.Set(headerTaskInstanceID, "instance-abc")
	headers.Set(headerProbeID, "probe-42")
	headers.Set(headerCorrelationID, "correlation-99")

	meta := CorrelationFromHeaders(headers)
	if got, want := meta.DeployID, strings.ToLower(deployID); got != want {
		t.Fatalf("deploy id = %q, want %q", got, want)
	}
	if got, want := meta.DeployRunKey, "2026-04-11.000137@controller-a"; got != want {
		t.Fatalf("deploy run key = %q, want %q", got, want)
	}
	if got, want := meta.TaskTemplateID, "template-123"; got != want {
		t.Fatalf("task template id = %q, want %q", got, want)
	}
	if got, want := meta.TaskInstanceID, "instance-abc"; got != want {
		t.Fatalf("task instance id = %q, want %q", got, want)
	}
	if got, want := meta.ProbeID, "probe-42"; got != want {
		t.Fatalf("probe id = %q, want %q", got, want)
	}
	if got, want := meta.CorrelationID, "correlation-99"; got != want {
		t.Fatalf("correlation id = %q, want %q", got, want)
	}

	attrs := meta.Attributes()
	if len(attrs) != 6 {
		t.Fatalf("attributes length = %d, want 6", len(attrs))
	}
}

func TestCorrelationFromHeadersDropsControlCharacters(t *testing.T) {
	headers := http.Header{}
	headers.Set(headerTaskInstanceID, "instance-\x01-abc")
	headers.Set(headerCorrelationID, " ok ")

	meta := CorrelationFromHeaders(headers)
	if meta.TaskInstanceID != "" {
		t.Fatalf("task instance id = %q, want empty", meta.TaskInstanceID)
	}
	if got, want := meta.CorrelationID, "ok"; got != want {
		t.Fatalf("correlation id = %q, want %q", got, want)
	}
}

func TestCorrelationMiddlewareAnnotatesSpanAndContext(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	oldProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(oldProvider)
		if err := tp.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown tracer provider: %v", err)
		}
	})

	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "request")

	req := httptest.NewRequest(http.MethodGet, "http://example.com", nil).WithContext(ctx)
	req.Header.Set(headerDeployID, uuid.NewString())
	req.Header.Set(headerCorrelationID, " correlation-1 ")

	var seenMeta CorrelationMetadata
	handler := CorrelationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ok bool
		seenMeta, ok = CorrelationFromContext(r.Context())
		if !ok {
			t.Fatal("expected correlation metadata in context")
		}
		if got, want := seenMeta.CorrelationID, "correlation-1"; got != want {
			t.Fatalf("correlation id = %q, want %q", got, want)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	span.End()

	if got, want := rr.Code, http.StatusNoContent; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if seenMeta.DeployID == "" {
		t.Fatal("expected deploy id in context")
	}

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	attrs := ended[0].Attributes()
	if len(attrs) < 2 {
		t.Fatalf("span attributes = %d, want at least 2", len(attrs))
	}
	if ended[0].Status().Code != codes.Unset {
		t.Fatalf("status code = %v, want unset", ended[0].Status().Code)
	}
	if got := findAttribute(attrs, attrCorrelationID); got != "correlation-1" {
		t.Fatalf("span correlation id = %q, want %q", got, "correlation-1")
	}
	if got := findAttribute(attrs, attrDeployID); got == "" {
		t.Fatal("expected deploy id span attribute")
	}
}

func TestTextMapPropagatorIncludesTraceContextAndBaggage(t *testing.T) {
	fields := textMapPropagator().Fields()
	if !containsAll(fields, "traceparent", "tracestate", "baggage") {
		t.Fatalf("fields = %v, want traceparent/tracestate/baggage", fields)
	}

	member, err := baggage.NewMember("forge_metal.deploy_id", "deploy-1")
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

func findAttribute(attrs []attribute.KeyValue, key string) string {
	for _, attr := range attrs {
		if string(attr.Key) == key {
			return attr.Value.AsString()
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
