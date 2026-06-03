package githubintegration

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	sandboxrentalclient "github.com/verself/sandbox-rental-service/client"
)

func TestServiceReady(t *testing.T) {
	if err := (&Service{}).Ready(context.Background()); err != nil {
		t.Fatalf("Ready: %v", err)
	}
}

func TestServiceReadyHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (&Service{}).Ready(ctx); err == nil {
		t.Fatal("Ready succeeded for canceled context")
	}
}

func TestVerifyGitHubSignature(t *testing.T) {
	payload := []byte(`{"zen":"practicality"}`)
	secret := "webhook-secret"
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if err := verifyGitHubSignature(secret, payload, signature); err != nil {
		t.Fatalf("verifyGitHubSignature valid signature: %v", err)
	}
	if err := verifyGitHubSignature(secret, payload, "sha256=00"); err == nil {
		t.Fatal("verifyGitHubSignature accepted invalid signature")
	}
	if err := verifyGitHubSignature(secret, payload, hex.EncodeToString(mac.Sum(nil))); err == nil {
		t.Fatal("verifyGitHubSignature accepted signature without sha256 prefix")
	}
}

func TestWebhookHandlerReportsMultipleHeaderProblems(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/github/webhooks", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	(&Service{}).WebhookHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	var doc webhookProblemDocument
	if err := json.NewDecoder(rec.Body).Decode(&doc); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if doc.Code != string(problemProviderWebhookHeaderInvalid) {
		t.Fatalf("code = %q, want %s", doc.Code, problemProviderWebhookHeaderInvalid)
	}
	if len(doc.Errors) != 3 {
		t.Fatalf("len(errors) = %d, want 3: %+v", len(doc.Errors), doc.Errors)
	}
}

func TestWebhookHandlerReportsSignatureProblem(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/github/webhooks", strings.NewReader(`{"zen":"practicality"}`))
	req.Header.Set("X-GitHub-Event", "ping")
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	req.Header.Set("X-Hub-Signature-256", "sha256=00")
	rec := httptest.NewRecorder()

	(&Service{cfg: Config{WebhookSecret: "webhook-secret"}}).WebhookHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	var doc webhookProblemDocument
	if err := json.NewDecoder(rec.Body).Decode(&doc); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if doc.Code != string(problemProviderWebhookSignatureInvalid) {
		t.Fatalf("code = %q, want %s", doc.Code, problemProviderWebhookSignatureInvalid)
	}
	if len(doc.Errors) != 1 || doc.Errors[0].Pointer != "header:X-Hub-Signature-256" {
		t.Fatalf("unexpected errors: %+v", doc.Errors)
	}
}

func TestRunnerClassForLabels(t *testing.T) {
	svc := &Service{cfg: Config{RunnerClassPrefix: "verself-"}}
	got, err := svc.runnerClassForLabels([]string{"self-hosted", "linux", "verself-ci-large"})
	if err != nil {
		t.Fatalf("runnerClassForLabels: %v", err)
	}
	if got != "verself-ci-large" {
		t.Fatalf("runnerClassForLabels = %q, want verself-ci-large", got)
	}
	if _, err := svc.runnerClassForLabels([]string{"self-hosted", "linux"}); err == nil {
		t.Fatal("runnerClassForLabels accepted labels without a Verself class")
	}
}

func TestGitHubCacheManifestRef(t *testing.T) {
	if got := githubCacheManifestRef(workflowObservation{PullRequestNumber: 42, BaseSHA: "base-sha", HeadSHA: "head-sha"}); got != "base-sha" {
		t.Fatalf("PR manifest ref = %q, want base-sha", got)
	}
	if got := githubCacheManifestRef(workflowObservation{HeadSHA: "head-sha", HeadBranch: "main"}); got != "head-sha" {
		t.Fatalf("push manifest ref = %q, want head-sha", got)
	}
	if got := githubCacheManifestRef(workflowObservation{}); got != "main" {
		t.Fatalf("fallback manifest ref = %q, want main", got)
	}
}

func TestGitHubRunnerNameIsUniquePerCapacityInstance(t *testing.T) {
	first, err := githubRunnerName(789)
	if err != nil {
		t.Fatalf("githubRunnerName: %v", err)
	}
	second, err := githubRunnerName(789)
	if err != nil {
		t.Fatalf("githubRunnerName second call: %v", err)
	}
	if first == second {
		t.Fatalf("runner name reused for distinct capacity instances: %q", first)
	}
	if !strings.HasPrefix(first, "verself-789-") || !strings.HasPrefix(second, "verself-789-") {
		t.Fatalf("runner names do not preserve provider job prefix: %q %q", first, second)
	}
}

func TestRunnerClassLockKeyIsScopedToRepositoryAndClass(t *testing.T) {
	repoAKey1, repoAKey2 := runnerClassLockKey(456, "verself-ci-large")
	repoAAgainKey1, repoAAgainKey2 := runnerClassLockKey(456, "verself-ci-large")
	if repoAKey1 != repoAAgainKey1 || repoAKey2 != repoAAgainKey2 {
		t.Fatal("runnerClassLockKey is not stable for the same repository and class")
	}
	repoBKey1, repoBKey2 := runnerClassLockKey(789, "verself-ci-large")
	if repoAKey1 == repoBKey1 && repoAKey2 == repoBKey2 {
		t.Fatal("runnerClassLockKey collapsed distinct repositories into one lock")
	}
	otherClassKey1, otherClassKey2 := runnerClassLockKey(456, "verself-ci-small")
	if repoAKey1 == otherClassKey1 && repoAKey2 == otherClassKey2 {
		t.Fatal("runnerClassLockKey collapsed distinct runner classes into one lock")
	}
}

func TestBuildGitHubJobShapeCanonicalizesLabels(t *testing.T) {
	var event workflowJobWebhook
	event.Installation.ID = 123
	event.Repository.ID = 456
	event.Repository.FullName = "guardian-intelligence/verself-sh"
	event.WorkflowJob = workflowJobPayload{
		ID:           789,
		RunID:        111,
		RunAttempt:   2,
		Name:         "test",
		WorkflowName: "ci",
		Labels:       []string{"verself-ci-large", "linux", "self-hosted"},
	}
	workflow := workflowObservation{
		ProviderRepositoryID:   456,
		RepositoryFullName:     "guardian-intelligence/verself-sh",
		EventName:              "pull_request",
		HeadRepositoryFullName: "guardian-intelligence/verself-sh",
		WorkflowPath:           ".github/workflows/ci.yml",
	}
	first, err := buildGitHubJobShape(event, workflow, "verself-ci-large", "cache-sha")
	if err != nil {
		t.Fatalf("buildGitHubJobShape: %v", err)
	}
	event.WorkflowJob.Labels = []string{"self-hosted", "verself-ci-large", "linux", "linux"}
	second, err := buildGitHubJobShape(event, workflow, "verself-ci-large", "cache-sha")
	if err != nil {
		t.Fatalf("buildGitHubJobShape second: %v", err)
	}
	if first.JobShapeID != second.JobShapeID {
		t.Fatalf("job shape changed for reordered labels: %q != %q", first.JobShapeID, second.JobShapeID)
	}
	if first.Shape.TrustClass != trustClassPR {
		t.Fatalf("trust class = %q, want %q", first.Shape.TrustClass, trustClassPR)
	}
}

func TestParseWebhookMetadata(t *testing.T) {
	payload, err := json.Marshal(workflowJobWebhook{
		Action: "queued",
		Installation: struct {
			ID int64 `json:"id"`
		}{ID: 123},
		Repository: struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
		}{ID: 456, FullName: "guardian-intelligence/verself-sh"},
		WorkflowJob: workflowJobPayload{ID: 789, RunID: 111, RunAttempt: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	meta, err := parseWebhookMetadata(payload)
	if err != nil {
		t.Fatalf("parseWebhookMetadata: %v", err)
	}
	if meta.Action != "queued" || meta.InstallationID != 123 || meta.RepositoryID != 456 || meta.RepositoryFullName != "guardian-intelligence/verself-sh" || meta.RunID != 111 || meta.RunAttempt != 2 || meta.JobID != 789 {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
}

func TestOffsetPageTokenRoundTrip(t *testing.T) {
	token := encodeOffsetPageToken(250)
	if token == "" {
		t.Fatal("empty token")
	}
	offset, err := decodeOffsetPageToken(token)
	if err != nil {
		t.Fatalf("decodeOffsetPageToken: %v", err)
	}
	if offset != 250 {
		t.Fatalf("offset = %d, want 250", offset)
	}
}

func TestOffsetPageTokenRejectsMalformedInput(t *testing.T) {
	if _, err := decodeOffsetPageToken("not-valid-base64"); err == nil {
		t.Fatal("decodeOffsetPageToken accepted malformed token")
	}
}

func TestSandboxObservationFromWebhookUsesOnlyProviderObservedRunner(t *testing.T) {
	var event workflowJobWebhook
	event.Action = "queued"
	event.Installation.ID = 123
	event.Repository.ID = 456
	event.Repository.FullName = "guardian-intelligence/verself-sh"
	event.WorkflowJob = workflowJobPayload{
		ID:         789,
		RunID:      111,
		RunAttempt: 2,
		Status:     "queued",
		Labels:     []string{"self-hosted", "linux", "x64", "verself-4vcpu-ubuntu-2404"},
	}
	queued := sandboxObservationFromWebhook(event, "delivery-1")
	if queued.RunnerID != nil || queued.RunnerName != nil {
		t.Fatalf("queued observation leaked JIT runner intent: runner_id=%v runner_name=%v", queued.RunnerID, queued.RunnerName)
	}

	event.WorkflowJob.Status = "in_progress"
	event.WorkflowJob.RunnerID = 987
	event.WorkflowJob.RunnerName = "verself-789-abcdef1234"
	assigned := sandboxObservationFromWebhook(event, "delivery-2")
	if assigned.RunnerID == nil || string(*assigned.RunnerID) != "987" {
		t.Fatalf("assigned observation runner_id = %v, want 987", assigned.RunnerID)
	}
	if assigned.RunnerName == nil || string(*assigned.RunnerName) != "verself-789-abcdef1234" {
		t.Fatalf("assigned observation runner_name = %v", assigned.RunnerName)
	}
}

func TestCreateFailureCheckRunUsesRepositoryEndpoint(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotBody githubCheckRunRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	svc := &Service{
		cfg:    Config{APIBaseURL: server.URL},
		client: server.Client(),
		tokens: map[int64]githubInstallationToken{
			42: {Token: "cached-token", ExpiresAt: time.Now().UTC().Add(time.Hour)},
		},
	}
	if err := svc.createFailureCheckRun(context.Background(), 42, "guardian-intelligence/verself", "abc123", "surface-key", "summary", "details"); err != nil {
		t.Fatalf("createFailureCheckRun: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/repos/guardian-intelligence/verself/check-runs" {
		t.Fatalf("path = %s", gotPath)
	}
	if gotAuth != "Bearer cached-token" {
		t.Fatalf("authorization = %s", gotAuth)
	}
	if gotBody.HeadSHA != "abc123" || gotBody.Status != "completed" || gotBody.Conclusion != "failure" {
		t.Fatalf("check run body = %+v", gotBody)
	}
	if gotBody.ExternalID != "surface-key" || gotBody.Output.Summary != "summary" || gotBody.Output.Text != "details" {
		t.Fatalf("check run output = %+v", gotBody)
	}
}

func TestProviderSurfaceCreatesFailureCheckRunForWorkflowHead(t *testing.T) {
	var gotBody githubCheckRunRequest
	calls := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer cached-token" {
			t.Fatalf("authorization = %s", r.Header.Get("Authorization"))
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/guardian-intelligence/verself/actions/runs/123":
			_, _ = w.Write([]byte(`{"id":123,"head_sha":"abc123"}`))
		case "POST /repos/guardian-intelligence/verself/check-runs":
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Fatalf("decode check run: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected GitHub request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	svc := &Service{
		cfg:    Config{APIBaseURL: server.URL},
		client: server.Client(),
		tokens: map[int64]githubInstallationToken{
			42: {Token: "cached-token", ExpiresAt: time.Now().UTC().Add(time.Hour)},
		},
	}
	cmd := providerSurfaceCommand{
		CommandKey:             "surface-key",
		ProviderInstallationID: 42,
		RepositoryFullName:     "guardian-intelligence/verself",
		ProviderRunID:          123,
		ProviderJobID:          456,
		RunnerClass:            "verself-4vcpu-ubuntu-2404",
		PrimaryProblemCode:     "runner_allocation.snapshot_restore_failed",
		PrimaryProblemDetail:   "restored_chrony_source_wait_failed",
	}

	if err := svc.executeCreateFailureCheckRunSurface(context.Background(), cmd); err != nil {
		t.Fatalf("executeCreateFailureCheckRunSurface: %v", err)
	}
	if len(calls) != 2 || calls[0] != "GET /repos/guardian-intelligence/verself/actions/runs/123" || calls[1] != "POST /repos/guardian-intelligence/verself/check-runs" {
		t.Fatalf("calls = %v", calls)
	}
	if gotBody.HeadSHA != "abc123" || gotBody.ExternalID != "surface-key" || gotBody.Conclusion != "failure" {
		t.Fatalf("check run body = %+v", gotBody)
	}
	if !strings.Contains(gotBody.Output.Text, "runner_class=verself-4vcpu-ubuntu-2404") || !strings.Contains(gotBody.Output.Text, "snapshot_restore_failed") {
		t.Fatalf("check run text = %q", gotBody.Output.Text)
	}
}

func TestSandboxRunnerCapacityTerminalWithoutAssignment(t *testing.T) {
	if !sandboxRunnerCapacityTerminalWithoutAssignment(sandboxrentalclient.RunnerAllocationStatus{State: "failed"}) {
		t.Fatal("failed unassigned allocation was not terminal capacity")
	}
	assignedJob := sandboxrentalclient.DecimalUint64("42")
	if sandboxRunnerCapacityTerminalWithoutAssignment(sandboxrentalclient.RunnerAllocationStatus{
		State:                 "failed",
		AssignedProviderJobID: &assignedJob,
	}) {
		t.Fatal("assigned allocation should not be treated as failed capacity for origin demand")
	}
	attemptState := "lost"
	if !sandboxRunnerCapacityTerminalWithoutAssignment(sandboxrentalclient.RunnerAllocationStatus{State: "vm_submitted", AttemptState: &attemptState}) {
		t.Fatal("lost attempt did not fail unassigned capacity")
	}
	detail := sandboxrentalclient.ProblemDetail("lease_ready_timeout")
	status := sandboxrentalclient.RunnerAllocationStatus{
		State: "failed",
		Problems: sandboxrentalclient.ProblemOccurrences{{
			Code:   sandboxrentalclient.ProblemCode("runner_allocation.runner_listening_deadline_exceeded"),
			Detail: &detail,
		}},
	}
	if got := sandboxRunnerAllocationProblemReason(status); got != "runner_allocation.runner_listening_deadline_exceeded:lease_ready_timeout" {
		t.Fatalf("problem reason = %q", got)
	}
}

func TestSandboxRunnerGuestTimeSyncPrecisionGateReason(t *testing.T) {
	for _, reason := range []string{
		"runner_allocation.guest_time_sync_precision_gate_failed:lease 01K reached terminal state before ready",
		"runner_allocation.snapshot_restore_failed:lease 01KT632XZKAJAGVMHEJ45WHGRY reached terminal state before ready: guest control protocol violation in await_after_restore_result: restored wall clock offset 3813174ns exceeds 1000000ns after host step",
		"runner_allocation.snapshot_restore_failed:lease 01K reached terminal state before ready: guest control protocol violation in await_after_restore_result: restored chrony sync: chrony sync wait: exit status 1",
		"runner_allocation.snapshot_restore_failed:lease 01K reached terminal state before ready: guest control protocol violation in await_after_restore_result: restored chrony sync: chrony skew 15.000ppm exceeds 10.000ppm",
	} {
		if !sandboxRunnerGuestTimeSyncPrecisionGateReason(reason) {
			t.Fatalf("sandboxRunnerGuestTimeSyncPrecisionGateReason(%q) = false, want true", reason)
		}
	}

	if sandboxRunnerGuestTimeSyncPrecisionGateReason("runner_allocation.snapshot_restore_failed:read restored PTP time: open /dev/ptp0: no such file or directory") {
		t.Fatal("PTP availability failure should not be classified as precision gate failure")
	}
}

func TestRunnerCapacityAssignmentDeadlineExceeded(t *testing.T) {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	if !runnerCapacityAssignmentDeadlineExceeded(pgTime(now), now) {
		t.Fatal("deadline at now should be expired")
	}
	if !runnerCapacityAssignmentDeadlineExceeded(pgTime(now.Add(-time.Second)), now) {
		t.Fatal("past deadline should be expired")
	}
	if runnerCapacityAssignmentDeadlineExceeded(pgTime(now.Add(time.Second)), now) {
		t.Fatal("future deadline should not be expired")
	}
	if runnerCapacityAssignmentDeadlineExceeded(pgtype.Timestamptz{}, now) {
		t.Fatal("missing deadline should not be expired")
	}
}

func TestProviderSurfaceCommandKeyCreatesFailureCheckRunOnce(t *testing.T) {
	ref := runnerCapacityRef{
		ProviderInstallationID: 42,
		ProviderRepositoryID:   99,
		ProviderRunID:          123,
		ProviderJobID:          1001,
	}
	sameRun := ref
	sameRun.ProviderJobID = 1002
	if providerSurfaceCommandKey(ref) != providerSurfaceCommandKey(sameRun) {
		t.Fatal("provider surface key should dedupe jobs in the same workflow run")
	}
	otherRun := ref
	otherRun.ProviderRunID = 124
	if providerSurfaceCommandKey(ref) == providerSurfaceCommandKey(otherRun) {
		t.Fatal("provider surface key collapsed distinct workflow runs")
	}
}

func TestProviderSurfaceFailureResult(t *testing.T) {
	if got := providerSurfaceFailureResult(false); got != "retryable" {
		t.Fatalf("nonterminal result = %q, want retryable", got)
	}
	if got := providerSurfaceFailureResult(true); got != "failed" {
		t.Fatalf("terminal result = %q, want failed", got)
	}
}

func TestWebhookStaleProblemPreservedAsRunnerProblem(t *testing.T) {
	webhookProblems := newWebhookProblemSet(providerWebhookProcessingStaleProblem())
	runnerProblems := runnerProblemSetFromWebhookProblems(webhookProblems)
	if len(runnerProblems.problems) != 1 {
		t.Fatalf("len(runnerProblems) = %d, want 1", len(runnerProblems.problems))
	}
	problem := runnerProblems.problems[0]
	if problem.Code != string(problemProviderWebhookProcessingStale) {
		t.Fatalf("runner problem code = %q", problem.Code)
	}
	if problem.Type != "urn:verself:problem:provider_webhook:processing_stale" {
		t.Fatalf("runner problem type = %q", problem.Type)
	}
}

func TestProviderDemandTerminalForCapacity(t *testing.T) {
	for _, state := range []string{"assigned", "completed", "capacity_failed", "jit_failed", "sandbox_failed"} {
		if !providerDemandTerminalForCapacity(state) {
			t.Fatalf("%s should be terminal for capacity", state)
		}
	}
	for _, state := range []string{"", "demand_recorded", "capacity_requested"} {
		if providerDemandTerminalForCapacity(state) {
			t.Fatalf("%s should remain eligible for capacity", state)
		}
	}
}
