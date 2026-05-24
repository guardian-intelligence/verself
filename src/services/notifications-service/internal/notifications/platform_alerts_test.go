package notifications

import (
	"strings"
	"testing"
	"time"
)

func TestCrossServiceFailureWorkflow(t *testing.T) {
	failure := crossServiceFailure{
		Timestamp:        time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC),
		ServiceName:      "sandbox-rental-service",
		TraceID:          "325de96c5561ed22076f46c02997ccf7",
		SpanID:           "87a61a258bec6599",
		LocalID:          "spiffe://spiffe.verself.sh/svc/sandbox-rental-service",
		ExpectedServerID: "spiffe://spiffe.verself.sh/svc/iam-service",
		PeerService:      "iam-service",
		Method:           "POST",
		URLScheme:        "https",
		ServerAddress:    "127.0.0.1",
		ServerPort:       "4241",
		URLPath:          "/internal/v1/authorization/authorize",
		StatusMessage:    "context deadline exceeded",
		Duration:         5001 * time.Millisecond,
	}
	workflow := crossServiceFailureWorkflow(CrossServiceFailureAlertConfig{
		OrgID: "371564185181576922",
		Email: "alerts@example.com",
	}, failure)

	if workflow.WorkflowKey != crossServiceFailureWorkflowKey {
		t.Fatalf("workflow key = %q", workflow.WorkflowKey)
	}
	if !strings.HasPrefix(workflow.IdempotencyKey, "platform:cross_service_call_failed:") {
		t.Fatalf("idempotency key = %q", workflow.IdempotencyKey)
	}
	if len(workflow.IdempotencyKey) > 128 {
		t.Fatalf("idempotency key too long: %d", len(workflow.IdempotencyKey))
	}
	secondFailure := failure
	secondFailure.TraceID = "425de96c5561ed22076f46c02997ccf7"
	secondFailure.SpanID = "97a61a258bec6599"
	if got := crossServiceFailureDedupeKey(secondFailure); got != workflow.IdempotencyKey {
		t.Fatalf("same hourly failure dedupe key = %q, want %q", got, workflow.IdempotencyKey)
	}
	nextHourFailure := failure
	nextHourFailure.Timestamp = nextHourFailure.Timestamp.Add(time.Hour)
	if got := crossServiceFailureDedupeKey(nextHourFailure); got == workflow.IdempotencyKey {
		t.Fatalf("next-hour failure reused dedupe key %q", got)
	}
	if workflow.Traceparent != "00-"+failure.TraceID+"-"+failure.SpanID+"-01" {
		t.Fatalf("traceparent = %q", workflow.Traceparent)
	}
	if !strings.Contains(workflow.Body, "endpoint=https://127.0.0.1:4241/internal/v1/authorization/authorize") {
		t.Fatalf("body missing endpoint: %q", workflow.Body)
	}
	if len([]rune(workflow.Title)) > 120 || len([]rune(workflow.Body)) > 500 {
		t.Fatalf("notification text exceeded limits: title=%d body=%d", len([]rune(workflow.Title)), len([]rune(workflow.Body)))
	}
}

func TestCrossServiceFailureTargetServiceFromSPIFFEID(t *testing.T) {
	failure := crossServiceFailure{
		ExpectedServerID: "spiffe://spiffe.verself.sh/svc/iam-service",
	}
	if got := failure.targetService(); got != "iam-service" {
		t.Fatalf("targetService = %q", got)
	}
}
