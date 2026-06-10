package main

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/nomad/api"
	"go.opentelemetry.io/otel/trace"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const otherTestDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const testCommit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const otherTestCommit = "cccccccccccccccccccccccccccccccccccccccc"

func TestWorkloadOCIAttestationRowsVerifiedPodmanTask(t *testing.T) {
	observedAt := time.Unix(1710000000, 123000000).UTC()
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10},
		SpanID:  trace.SpanID{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18},
	})
	alloc := testAllocation("127.0.0.1:5080/verself/analytics-service@"+testDigest, "podman")
	rows := workloadOCIAttestationRows(t.Context(), "gamma", "default", alloc, deployMeta{
		DeployRunKey:   "deploy-run",
		DeploySHA:      testCommit,
		SpecSHA256:     "spec",
		ArtifactSHA256: "artifact",
	}, observedAt, spanContext, fixedMeasurement(testDigest, "podman_container_inspect_labels"), fixedArtifact(verifiedArtifact(testCommit)))
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
	if row.MeasuredDigest != testDigest {
		t.Fatalf("measured digest = %q, want %q", row.MeasuredDigest, testDigest)
	}
	if row.Decision != "verified" || row.MeasurementSource != "podman_container_inspect_labels" {
		t.Fatalf("unexpected decision/source: %q/%q", row.Decision, row.MeasurementSource)
	}
	if row.DistributionArtifactDigest != testDigest || row.DistributionArtifactState != "available" || row.DistributionVerificationDecision != "allowed" {
		t.Fatalf("distribution evidence was not copied: %#v", row)
	}
	if row.DistributionSourceRepository != workloadOCISourceRepository || row.DistributionSourceCommit != testCommit {
		t.Fatalf("distribution source evidence was not copied: %#v", row)
	}
	if row.SLSAPredicateType != workloadOCIPredicateSLSAProvenance || row.SLSASubjectDigest != testDigest {
		t.Fatalf("SLSA evidence was not copied: %#v", row)
	}
	if row.SigningKeyID != "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff" {
		t.Fatalf("signing key id was not copied: %#v", row)
	}
	if row.SourceCommit != testCommit || row.DeployRunKey != "deploy-run" {
		t.Fatalf("deploy metadata was not copied: %#v", row)
	}
	if row.TraceID == "" || row.SpanID == "" {
		t.Fatalf("trace correlation was not populated: %#v", row)
	}
	if !row.ObservedAt.Equal(observedAt) {
		t.Fatalf("observed_at = %s, want %s", row.ObservedAt, observedAt)
	}
}

func TestWorkloadOCIAttestationRowsUnadmittedWithoutDistributionArtifact(t *testing.T) {
	alloc := testAllocation("127.0.0.1:5080/verself/analytics-service@"+testDigest, "podman")
	rows := workloadOCIAttestationRows(t.Context(), "gamma", "default", alloc, deployMeta{
		DeploySHA: testCommit,
	}, time.Now().UTC(), trace.SpanContext{}, fixedMeasurement(testDigest, "podman_container_inspect_name"), fixedArtifact(workloadOCIArtifactEvidence{
		reason: "distribution artifact lookup rejected: not found",
	}))
	if len(rows) != 1 {
		t.Fatalf("expected one attestation row, got %d", len(rows))
	}
	if rows[0].Decision != "unadmitted" {
		t.Fatalf("decision = %q, want unadmitted", rows[0].Decision)
	}
	if rows[0].Reason == "" {
		t.Fatalf("expected reason for unadmitted row")
	}
}

func TestWorkloadOCIAttestationRowsSourceCommitMismatch(t *testing.T) {
	alloc := testAllocation("127.0.0.1:5080/verself/analytics-service@"+testDigest, "podman")
	rows := workloadOCIAttestationRows(t.Context(), "gamma", "default", alloc, deployMeta{
		DeploySHA: testCommit,
	}, time.Now().UTC(), trace.SpanContext{}, fixedMeasurement(testDigest, "podman_container_inspect_name"), fixedArtifact(verifiedArtifact(otherTestCommit)))
	if len(rows) != 1 {
		t.Fatalf("expected one attestation row, got %d", len(rows))
	}
	if rows[0].Decision != "source_commit_mismatch" {
		t.Fatalf("decision = %q, want source_commit_mismatch", rows[0].Decision)
	}
}

func TestWorkloadOCIAttestationRowsRuntimeMismatchDoesNotResolveDistribution(t *testing.T) {
	alloc := testAllocation("127.0.0.1:5080/verself/analytics-service@"+testDigest, "podman")
	resolved := false
	rows := workloadOCIAttestationRows(t.Context(), "gamma", "default", alloc, deployMeta{}, time.Now().UTC(), trace.SpanContext{}, fixedMeasurement(otherTestDigest, "podman_container_inspect_name"), func(context.Context, string) workloadOCIArtifactEvidence {
		resolved = true
		return verifiedArtifact(testDigest)
	})
	if len(rows) != 1 {
		t.Fatalf("expected one attestation row, got %d", len(rows))
	}
	if rows[0].Decision != "mismatched" {
		t.Fatalf("decision = %q, want mismatched", rows[0].Decision)
	}
	if resolved {
		t.Fatalf("distribution evidence should not be resolved when runtime digest mismatches")
	}
}

func TestWorkloadOCIAttestationRowsMissingDigest(t *testing.T) {
	rows := workloadOCIAttestationRows(t.Context(), "gamma", "default", testAllocation("127.0.0.1:5080/verself/analytics-service:latest", "podman"), deployMeta{}, time.Now().UTC(), trace.SpanContext{}, fixedMeasurement(testDigest, "podman_container_inspect_name"), nil)
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

func TestWorkloadOCIAttestationRowsMismatchedDigest(t *testing.T) {
	alloc := testAllocation("127.0.0.1:5080/verself/analytics-service@"+testDigest, "podman")
	rows := workloadOCIAttestationRows(t.Context(), "gamma", "default", alloc, deployMeta{}, time.Now().UTC(), trace.SpanContext{}, fixedMeasurement(otherTestDigest, "podman_container_inspect_name"), nil)
	if len(rows) != 1 {
		t.Fatalf("expected one attestation row, got %d", len(rows))
	}
	if rows[0].Decision != "mismatched" {
		t.Fatalf("decision = %q, want mismatched", rows[0].Decision)
	}
	if rows[0].MeasuredDigest != otherTestDigest {
		t.Fatalf("measured digest = %q, want %q", rows[0].MeasuredDigest, otherTestDigest)
	}
}

func TestWorkloadOCIAttestationRowsIgnoreNonPodmanTask(t *testing.T) {
	rows := workloadOCIAttestationRows(t.Context(), "gamma", "default", testAllocation("127.0.0.1:5080/verself/analytics-service@"+testDigest, "raw_exec"), deployMeta{}, time.Now().UTC(), trace.SpanContext{}, fixedMeasurement(testDigest, "podman_container_inspect_name"), nil)
	if len(rows) != 0 {
		t.Fatalf("expected no rows, got %d", len(rows))
	}
}

func TestPodmanMeasuredDigestUsesInspectDigest(t *testing.T) {
	digest, err := podmanMeasuredDigest(podmanContainerInspect{
		ID:          "container-1",
		ImageDigest: testDigest,
		ImageName:   "127.0.0.1:5080/verself/analytics-service:local",
	})
	if err != nil {
		t.Fatalf("podman measured digest failed: %v", err)
	}
	if digest != testDigest {
		t.Fatalf("digest = %q, want %q", digest, testDigest)
	}
}

func TestPodmanMeasuredDigestFallsBackToImageReference(t *testing.T) {
	digest, err := podmanMeasuredDigest(podmanContainerInspect{
		ID:        "container-1",
		ImageName: "127.0.0.1:5080/verself/analytics-service@" + testDigest,
	})
	if err != nil {
		t.Fatalf("podman measured digest failed: %v", err)
	}
	if digest != testDigest {
		t.Fatalf("digest = %q, want %q", digest, testDigest)
	}
}

func TestPodmanMeasurementIdentitySourceLabels(t *testing.T) {
	req := testPodmanMeasurementRequest()
	source, reason := podmanMeasurementIdentitySource(req, podmanContainerInspect{
		Name: "unexpected-name",
		Config: &podmanContainerConfig{Labels: map[string]string{
			labelNomadAllocID:   req.AllocID,
			labelNomadNamespace: req.Namespace,
			labelNomadJobID:     req.JobID,
			labelNomadTaskGroup: req.TaskGroup,
			labelNomadTaskName:  req.TaskName,
		}},
	})
	if source != "podman_container_inspect_labels" || reason != "" {
		t.Fatalf("source/reason = %q/%q", source, reason)
	}
}

func TestPodmanMeasurementIdentitySourceRejectsLabelMismatch(t *testing.T) {
	req := testPodmanMeasurementRequest()
	_, reason := podmanMeasurementIdentitySource(req, podmanContainerInspect{
		Name: podmanContainerName(req.AllocID, req.TaskName),
		Config: &podmanContainerConfig{Labels: map[string]string{
			labelNomadAllocID:  req.AllocID,
			labelNomadTaskName: "wrong-task",
		}},
	})
	if reason == "" {
		t.Fatalf("expected label mismatch reason")
	}
}

func TestPodmanMeasurementIdentitySourceFallsBackToName(t *testing.T) {
	req := testPodmanMeasurementRequest()
	source, reason := podmanMeasurementIdentitySource(req, podmanContainerInspect{
		Name: podmanContainerName(req.AllocID, req.TaskName),
	})
	if source != "podman_container_inspect_name" || reason != "" {
		t.Fatalf("source/reason = %q/%q", source, reason)
	}
}

func fixedMeasurement(digest, source string) workloadOCIRuntimeMeasurer {
	return func(context.Context, *api.Allocation, allocationOCITask) workloadOCIRuntimeMeasurement {
		return workloadOCIRuntimeMeasurement{digest: digest, source: source}
	}
}

func fixedArtifact(artifact workloadOCIArtifactEvidence) workloadOCIArtifactResolver {
	return func(context.Context, string) workloadOCIArtifactEvidence {
		return artifact
	}
}

func verifiedArtifact(sourceCommit string) workloadOCIArtifactEvidence {
	return workloadOCIArtifactEvidence{
		digest:                testDigest,
		state:                 "available",
		repository:            "verself/analytics-service",
		publicRef:             "127.0.0.1:5080/verself/analytics-service@" + testDigest,
		policyRef:             "distribution-policies/deployments/oci-digest-v1",
		verificationDecision:  "allowed",
		verificationReason:    "required OCI referrers and policy inputs verified",
		builderID:             "spiffe://gamma.verself.sh/svc/deployment-service",
		signerIdentity:        "spiffe://gamma.verself.sh/svc/deployment-service",
		sourceRepository:      workloadOCISourceRepository,
		sourceCommit:          sourceCommit,
		sourceRef:             "refs/heads/main",
		slsaPredicateType:     workloadOCIPredicateSLSAProvenance,
		slsaSubjectDigest:     testDigest,
		slsaDocumentDigest:    "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		slsaOCIReferrerDigest: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		signingKeyID:          "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
	}
}

func testPodmanMeasurementRequest() podmanMeasurementRequest {
	return podmanMeasurementRequest{
		AllocID:   "alloc-1",
		Namespace: "default",
		JobID:     "analytics-service",
		TaskGroup: "api",
		TaskName:  "analytics",
		ImageRef:  "127.0.0.1:5080/verself/analytics-service@" + testDigest,
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
