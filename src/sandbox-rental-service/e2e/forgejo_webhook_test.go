package e2e_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestForgejoWebhook_PullRequestCreatesBilledRunnerExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e tests require real databases")
	}

	env := startRepoBootstrapEnv(t, &fakeRunner{
		delay:     400 * time.Millisecond,
		logs:      "forgejo webhook runner completed\n",
		commitSHA: "",
	})
	defer env.close()
	env.runner.setCommitSHA(env.repoHead)

	imported := importRepoAgainstServer(t, env.ctx, env.rentalServer.URL, env.token, env.repoPath)
	repo := waitForRepoState(t, env.ctx, env.rentalServer.URL, env.token, imported.RepoID, "ready")

	body := map[string]any{
		"action": "opened",
		"number": 42,
		"repository": map[string]any{
			"full_name":      "acme/example",
			"clone_url":      env.repoPath,
			"default_branch": "main",
			"name":           "example",
			"owner": map[string]any{
				"login": "acme",
			},
		},
		"pull_request": map[string]any{
			"head": map[string]any{
				"ref": "feature/webhook",
				"sha": env.repoHead,
			},
			"base": map[string]any{
				"ref": "main",
			},
		},
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal webhook payload: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, env.rentalServer.URL+"/webhooks/forgejo", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("build webhook request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forgejo-Event", "pull_request")
	req.Header.Set("X-Forgejo-Delivery", "delivery-pr-42")
	req.Header.Set("X-Forgejo-Signature", signForgejoWebhook(bodyBytes, env.webhookSecret))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post webhook: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 from webhook, got %d", resp.StatusCode)
	}

	var accepted struct {
		Status      string `json:"status"`
		ExecutionID string `json:"execution_id"`
		AttemptID   string `json:"attempt_id"`
		RepoID      string `json:"repo_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&accepted); err != nil {
		t.Fatalf("decode webhook response: %v", err)
	}
	if accepted.Status != "accepted" {
		t.Fatalf("expected webhook status=accepted, got %q", accepted.Status)
	}
	if accepted.ExecutionID == "" || accepted.AttemptID == "" {
		t.Fatalf("expected execution+attempt ids from webhook, got execution=%q attempt=%q", accepted.ExecutionID, accepted.AttemptID)
	}
	if accepted.RepoID != repo.RepoID {
		t.Fatalf("expected webhook repo_id=%s, got %s", repo.RepoID, accepted.RepoID)
	}

	execution := waitForExecutionState(t, env.ctx, env.rentalServer.URL, env.token, accepted.ExecutionID, "succeeded")
	if execution.Kind != "forgejo_runner" {
		t.Fatalf("expected kind=forgejo_runner, got %q", execution.Kind)
	}
	if execution.ProviderRunID != "delivery-pr-42" {
		t.Fatalf("expected provider_run_id=delivery-pr-42, got %q", execution.ProviderRunID)
	}
	if execution.ProviderJobID != "pr-42" {
		t.Fatalf("expected provider_job_id=pr-42, got %q", execution.ProviderJobID)
	}

	assertWarmGoldenBillingWindow(t, env.ctx, env.pg.rentalDB, accepted.AttemptID)
	flushBillingMetering(t, env.ctx, env.billingServer)

	var eventCount uint64
	if err := env.queryCHConn.QueryRow(env.ctx, `
		SELECT count()
		FROM forge_metal.job_events
		WHERE org_id = $1 AND execution_id = $2 AND kind = 'forgejo_runner'
	`, testOrgID, accepted.ExecutionID).Scan(&eventCount); err != nil {
		t.Fatalf("query webhook job_events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected 1 forgejo_runner job_event from webhook, got %d", eventCount)
	}

	var meteringCount uint64
	orgIDStr := strconv.FormatUint(testOrgID, 10)
	if err := env.queryCHConn.QueryRow(env.ctx,
		"SELECT count() FROM forge_metal.metering WHERE org_id = $1 AND source_ref = $2",
		orgIDStr, accepted.AttemptID,
	).Scan(&meteringCount); err != nil {
		t.Fatalf("query webhook metering: %v", err)
	}
	if meteringCount != 1 {
		t.Fatalf("expected 1 metering row from webhook execution, got %d", meteringCount)
	}
}

func signForgejoWebhook(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
