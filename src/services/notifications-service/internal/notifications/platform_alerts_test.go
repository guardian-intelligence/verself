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
	workflow := crossServiceFailureWorkflow(PlatformAlertConfig{
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

func TestCacheMissWorkflowDurableCache(t *testing.T) {
	miss := cacheMissAlert{
		Timestamp:            time.Date(2026, 5, 26, 18, 15, 0, 0, time.UTC),
		Kind:                 "durable_cache",
		OrgID:                "371564185181576922",
		Provider:             "github",
		ProviderRepositoryID: 922337,
		ProviderRunID:        4567,
		ProviderRunAttempt:   2,
		ProviderJobID:        8910,
		ExecutionID:          "018f0000-0000-7000-8000-000000000001",
		AttemptID:            "018f0000-0000-7000-8000-000000000002",
		OperationID:          "018f0000-0000-7000-8000-000000000003",
		CacheName:            "bazel-cache",
		EventName:            "durable.cache.select",
		Result:               "miss",
		Reason:               "current_generation_missing",
		TraceID:              "325de96c5561ed22076f46c02997ccf7",
		SpanID:               "87a61a258bec6599",
	}

	workflow := cacheMissWorkflow(PlatformAlertConfig{
		OrgID: "371564185181576922",
		Email: "alerts@example.com",
	}, miss)

	if workflow.WorkflowKey != durableCacheMissWorkflowKey {
		t.Fatalf("workflow key = %q", workflow.WorkflowKey)
	}
	if !strings.HasPrefix(workflow.IdempotencyKey, "platform:cache_miss:") {
		t.Fatalf("idempotency key = %q", workflow.IdempotencyKey)
	}
	if len(workflow.IdempotencyKey) > 128 {
		t.Fatalf("idempotency key too long: %d", len(workflow.IdempotencyKey))
	}
	if workflow.ResourceKind != "durable_cache" {
		t.Fatalf("resource kind = %q", workflow.ResourceKind)
	}
	if workflow.ResourceID != miss.OperationID+":"+miss.CacheName {
		t.Fatalf("resource id = %q", workflow.ResourceID)
	}
	if workflow.Traceparent != "00-"+miss.TraceID+"-"+miss.SpanID+"-01" {
		t.Fatalf("traceparent = %q", workflow.Traceparent)
	}
	if !strings.Contains(workflow.Title, "bazel-cache") {
		t.Fatalf("title missing cache name: %q", workflow.Title)
	}
	if !strings.Contains(workflow.Body, "reason=current_generation_missing") {
		t.Fatalf("body missing reason: %q", workflow.Body)
	}
	if len([]rune(workflow.Title)) > 120 || len([]rune(workflow.Body)) > 500 {
		t.Fatalf("notification text exceeded limits: title=%d body=%d", len([]rune(workflow.Title)), len([]rune(workflow.Body)))
	}
}

func TestCacheMissWorkflowFirecrackerSnapshotVM(t *testing.T) {
	miss := cacheMissAlert{
		Timestamp:               time.Date(2026, 5, 26, 18, 20, 0, 0, time.UTC),
		Kind:                    "firecracker_snapshot_vm",
		OrgID:                   "371564185181576922",
		Provider:                "github",
		ProviderRepositoryID:    922337,
		ProviderRunID:           4568,
		ExecutionID:             "018f0000-0000-7000-8000-000000000011",
		AttemptID:               "018f0000-0000-7000-8000-000000000012",
		OperationID:             "018f0000-0000-7000-8000-000000000013",
		EventName:               "golden.vm.lookup",
		Result:                  "miss",
		Reason:                  "current_snapshot_missing",
		JobShapeID:              "018f0000-0000-7000-8000-000000000014",
		SourceGenerationSetHash: "source-generation-set",
	}

	workflow := cacheMissWorkflow(PlatformAlertConfig{
		OrgID: "371564185181576922",
		Email: "alerts@example.com",
	}, miss)

	if workflow.WorkflowKey != goldenVMMissWorkflowKey {
		t.Fatalf("workflow key = %q", workflow.WorkflowKey)
	}
	if workflow.ResourceKind != "golden_vm_snapshot" {
		t.Fatalf("resource kind = %q", workflow.ResourceKind)
	}
	if workflow.ResourceID != miss.OperationID {
		t.Fatalf("resource id = %q", workflow.ResourceID)
	}
	if !strings.Contains(workflow.Title, "Firecracker snapshot VM") {
		t.Fatalf("title = %q", workflow.Title)
	}
	if !strings.Contains(workflow.Body, "job_shape_id="+miss.JobShapeID) {
		t.Fatalf("body missing job shape: %q", workflow.Body)
	}
	if workflow.Traceparent != "" {
		t.Fatalf("traceparent = %q", workflow.Traceparent)
	}
}
