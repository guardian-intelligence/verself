package e2e_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
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

func TestForgejoWebhook_InvalidSignatureReturnsUnauthorized(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e tests require real databases")
	}

	env := startRepoBootstrapEnv(t, &fakeRunner{delay: 200 * time.Millisecond})
	defer env.close()

	respBody, status := postForgejoWebhook(
		t,
		env.rentalServer.URL,
		[]byte(`{"action":"opened"}`),
		"pull_request",
		"delivery-invalid-signature",
		signForgejoWebhook([]byte(`{"action":"opened"}`), "wrong-secret"),
	)
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401 from invalid signature, got %d", status)
	}
	if !strings.Contains(respBody, "invalid forgejo signature") {
		t.Fatalf("expected invalid signature error, got %q", respBody)
	}
}

func TestForgejoWebhook_MissingEventHeaderReturnsBadRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e tests require real databases")
	}

	env := startRepoBootstrapEnv(t, &fakeRunner{delay: 200 * time.Millisecond})
	defer env.close()

	body := []byte(`{"action":"opened"}`)
	respBody, status := postForgejoWebhook(
		t,
		env.rentalServer.URL,
		body,
		"",
		"delivery-missing-event",
		signForgejoWebhook(body, env.webhookSecret),
	)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing event header, got %d", status)
	}
	if !strings.Contains(respBody, "missing forgejo event headers") {
		t.Fatalf("expected missing header error, got %q", respBody)
	}
}

func TestForgejoWebhook_MissingDeliveryHeaderReturnsBadRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e tests require real databases")
	}

	env := startRepoBootstrapEnv(t, &fakeRunner{delay: 200 * time.Millisecond})
	defer env.close()

	body := []byte(`{"action":"opened"}`)
	respBody, status := postForgejoWebhook(
		t,
		env.rentalServer.URL,
		body,
		"pull_request",
		"",
		signForgejoWebhook(body, env.webhookSecret),
	)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing delivery header, got %d", status)
	}
	if !strings.Contains(respBody, "missing forgejo event headers") {
		t.Fatalf("expected missing header error, got %q", respBody)
	}
}

func TestForgejoWebhook_UnsupportedEventIsIgnored(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e tests require real databases")
	}

	env := startRepoBootstrapEnv(t, &fakeRunner{delay: 200 * time.Millisecond})
	defer env.close()

	body := []byte(`{"repository":{"full_name":"acme/example"}}`)
	respBody, status := postForgejoWebhook(
		t,
		env.rentalServer.URL,
		body,
		"star",
		"delivery-ignored",
		signForgejoWebhook(body, env.webhookSecret),
	)
	if status != http.StatusOK {
		t.Fatalf("expected 200 for unsupported event, got %d body=%q", status, respBody)
	}

	var ignored struct {
		Status     string `json:"status"`
		Event      string `json:"event"`
		DeliveryID string `json:"delivery_id"`
		Message    string `json:"message"`
	}
	if err := json.Unmarshal([]byte(respBody), &ignored); err != nil {
		t.Fatalf("decode ignored webhook response: %v", err)
	}
	if ignored.Status != "ignored" {
		t.Fatalf("expected ignored status, got %q", ignored.Status)
	}
	if ignored.Event != "star" {
		t.Fatalf("expected event=star, got %q", ignored.Event)
	}
	if ignored.DeliveryID != "delivery-ignored" {
		t.Fatalf("expected delivery_id=delivery-ignored, got %q", ignored.DeliveryID)
	}
}

func TestForgejoWebhook_BodyTooLargeReturnsRequestEntityTooLarge(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e tests require real databases")
	}

	env := startRepoBootstrapEnv(t, &fakeRunner{delay: 200 * time.Millisecond})
	defer env.close()

	body := bytes.Repeat([]byte("a"), (1<<20)+1)
	respBody, status := postForgejoWebhook(
		t,
		env.rentalServer.URL,
		body,
		"pull_request",
		"delivery-too-large",
		"",
	)
	if status != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized body, got %d", status)
	}
	if !strings.Contains(respBody, "request body too large") {
		t.Fatalf("expected oversized body error, got %q", respBody)
	}
}

func postForgejoWebhook(
	t *testing.T,
	baseURL string,
	body []byte,
	eventType string,
	deliveryID string,
	signature string,
) (string, int) {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, baseURL+"/webhooks/forgejo", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build webhook request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if eventType != "" {
		req.Header.Set("X-Forgejo-Event", eventType)
	}
	if deliveryID != "" {
		req.Header.Set("X-Forgejo-Delivery", deliveryID)
	}
	if signature != "" {
		req.Header.Set("X-Forgejo-Signature", signature)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post webhook: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read webhook response: %v", err)
	}
	return string(respBody), resp.StatusCode
}

func signForgejoWebhook(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
