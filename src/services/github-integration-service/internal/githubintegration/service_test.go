package githubintegration

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
