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
	if doc.Code != "provider_webhook.header_invalid" {
		t.Fatalf("code = %q, want provider_webhook.header_invalid", doc.Code)
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
	if doc.Code != "provider_webhook.signature_invalid" {
		t.Fatalf("code = %q, want provider_webhook.signature_invalid", doc.Code)
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
