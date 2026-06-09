package main

import (
	"testing"
	"time"

	"github.com/hashicorp/nomad/api"
	"go.opentelemetry.io/otel/trace"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestWorkloadOCIAttestationRowsPinnedPodmanTask(t *testing.T) {
	observedAt := time.Unix(1710000000, 123000000).UTC()
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
		SpanID:  trace.SpanID{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
	})
	alloc := testAllocation("127.0.0.1:5080/verself/analytics-service@"+testDigest, "podman")
	rows := workloadOCIAttestationRows("gamma", "default", alloc, deployMeta{
		DeployRunKey:   "deploy-run",
		DeploySHA:      "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		SpecSHA256:     "spec",
		ArtifactSHA256: "artifact",
	}, observedAt, spanContext)
	if len(rows) != 1 {
		t.Fatalf("expected one attestation row, got %d", len(rows))
	}
	row := rows[0]
	if row.Site != "gamma" || row.NomadNamespace != "default" || row.NomadJobID != "analytics-service" || row.NomadGroup != "api" || row.NomadTask != "analytics" {
		t.Fatalf("unexpected nomad coordinates: %#v", row)
	}
	if row.DeclaredDigest != testDigest {
		t.Fatalf("declared digest = %q, want %q", row.DeclaredDigest, testDigest)
	}
	if row.Decision != "unmeasured" || row.MeasurementSource != "nomad_job_spec" {
		t.Fatalf("unexpected decision/source: %q/%q", row.Decision, row.MeasurementSource)
	}
	if row.SourceCommit != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" || row.DeployRunKey != "deploy-run" {
		t.Fatalf("deploy metadata was not copied: %#v", row)
	}
	if row.TraceID == "" || row.SpanID == "" {
		t.Fatalf("trace correlation was not populated: %#v", row)
	}
	if !row.ObservedAt.Equal(observedAt) {
		t.Fatalf("observed_at = %s, want %s", row.ObservedAt, observedAt)
	}
}

func TestWorkloadOCIAttestationRowsMissingDigest(t *testing.T) {
	rows := workloadOCIAttestationRows("gamma", "default", testAllocation("127.0.0.1:5080/verself/analytics-service:latest", "podman"), deployMeta{}, time.Now().UTC(), trace.SpanContext{})
	if len(rows) != 1 {
		t.Fatalf("expected one attestation row, got %d", len(rows))
	}
	if rows[0].Decision != "declared_digest_missing" {
		t.Fatalf("decision = %q, want declared_digest_missing", rows[0].Decision)
	}
	if rows[0].DeclaredDigest != "" {
		t.Fatalf("declared digest = %q, want empty", rows[0].DeclaredDigest)
	}
}

func TestWorkloadOCIAttestationRowsFailedAllocation(t *testing.T) {
	alloc := testAllocation("127.0.0.1:5080/verself/analytics-service@"+testDigest, "podman")
	alloc.ClientStatus = api.AllocClientStatusFailed
	alloc.TaskStates["analytics"].State = "dead"

	rows := workloadOCIAttestationRows("gamma", "default", alloc, deployMeta{}, time.Now().UTC(), trace.SpanContext{})
	if len(rows) != 1 {
		t.Fatalf("expected one attestation row, got %d", len(rows))
	}
	if rows[0].AllocClientStatus != api.AllocClientStatusFailed || rows[0].TaskState != "dead" {
		t.Fatalf("unexpected allocation status/task state: %#v", rows[0])
	}
	if rows[0].Decision != "unmeasured" {
		t.Fatalf("decision = %q, want unmeasured", rows[0].Decision)
	}
}

func TestWorkloadOCIAttestationRowsIgnoreNonPodmanTask(t *testing.T) {
	rows := workloadOCIAttestationRows("gamma", "default", testAllocation("127.0.0.1:5080/verself/analytics-service@"+testDigest, "raw_exec"), deployMeta{}, time.Now().UTC(), trace.SpanContext{})
	if len(rows) != 0 {
		t.Fatalf("expected no rows, got %d", len(rows))
	}
}

func testAllocation(imageRef, driver string) *api.Allocation {
	groupName := "api"
	return &api.Allocation{
		ID:                "alloc-1",
		Namespace:         "default",
		JobID:             "analytics-service",
		TaskGroup:         groupName,
		NodeID:            "node-1",
		NodeName:          "node-a",
		ClientStatus:      api.AllocClientStatusRunning,
		ModifyIndex:       42,
		TaskStates:        map[string]*api.TaskState{"analytics": {State: "running"}},
		DeploymentID:      "deployment-1",
		ClientDescription: "running",
		Job: &api.Job{
			TaskGroups: []*api.TaskGroup{
				{
					Name: &groupName,
					Tasks: []*api.Task{
						{
							Name:   "analytics",
							Driver: driver,
							Config: map[string]interface{}{"image": imageRef},
						},
					},
				},
			},
		},
	}
}
