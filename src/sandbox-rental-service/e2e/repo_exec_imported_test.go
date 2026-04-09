package e2e_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"
)

type executionSubmitView struct {
	ExecutionID string `json:"execution_id"`
	AttemptID   string `json:"attempt_id"`
	Status      string `json:"status"`
}

type attemptView struct {
	AttemptID      string `json:"attempt_id"`
	State          string `json:"state"`
	ExitCode       int    `json:"exit_code"`
	DurationMs     int64  `json:"duration_ms"`
	GoldenSnapshot string `json:"golden_snapshot"`
	RunnerName     string `json:"runner_name"`
}

type executionView struct {
	ExecutionID        string      `json:"execution_id"`
	Status             string      `json:"status"`
	Kind               string      `json:"kind"`
	RepoID             string      `json:"repo_id"`
	GoldenGenerationID string      `json:"golden_generation_id"`
	ProviderRunID      string      `json:"provider_run_id"`
	ProviderJobID      string      `json:"provider_job_id"`
	Repo               string      `json:"repo"`
	RepoURL            string      `json:"repo_url"`
	Ref                string      `json:"ref"`
	CommitSHA          string      `json:"commit_sha"`
	LatestAttempt      attemptView `json:"latest_attempt"`
}

func TestSubmitExecutionAPI_ImportedRepoUsesActiveGolden(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e tests require real databases")
	}

	env := startRepoBootstrapEnv(t, &fakeRunner{
		delay:       400 * time.Millisecond,
		logs:        "repo exec complete\n",
		commitSHA:   "",
		requireWarm: true,
	})
	defer env.close()
	env.runner.setCommitSHA(env.repoHead)

	imported := importRepoAgainstServer(t, env.ctx, env.rentalServer.URL, env.token, env.repoPath)
	repo := waitForRepoState(t, env.ctx, env.rentalServer.URL, env.token, imported.RepoID, "ready")
	if repo.ActiveGoldenGenerationID == "" {
		t.Fatal("expected active generation before repo exec")
	}

	submit := submitImportedRepoExec(t, env.ctx, env.rentalServer.URL, env.token, repo.RepoID)
	if submit.ExecutionID == "" || submit.AttemptID == "" {
		t.Fatalf("expected execution+attempt ids, got execution=%q attempt=%q", submit.ExecutionID, submit.AttemptID)
	}

	execution := waitForExecutionState(t, env.ctx, env.rentalServer.URL, env.token, submit.ExecutionID, "succeeded")
	if execution.Kind != "repo_exec" {
		t.Fatalf("expected kind=repo_exec, got %q", execution.Kind)
	}
	if execution.RepoID != repo.RepoID {
		t.Fatalf("expected repo_id=%s, got %s", repo.RepoID, execution.RepoID)
	}
	if execution.GoldenGenerationID != repo.ActiveGoldenGenerationID {
		t.Fatalf("expected golden_generation_id=%s, got %s", repo.ActiveGoldenGenerationID, execution.GoldenGenerationID)
	}
	if execution.LatestAttempt.State != "succeeded" {
		t.Fatalf("expected latest attempt succeeded, got %q", execution.LatestAttempt.State)
	}
	if execution.LatestAttempt.GoldenSnapshot == "" {
		t.Fatal("expected golden_snapshot on repo exec attempt")
	}
	if execution.CommitSHA != env.repoHead {
		t.Fatalf("expected commit_sha=%s, got %s", env.repoHead, execution.CommitSHA)
	}

	assertWarmGoldenBillingWindow(t, env.ctx, env.pg.rentalDB, submit.AttemptID)
	flushBillingMetering(t, env.ctx, env.billingServer)

	var meteringCount uint64
	orgIDStr := strconv.FormatUint(testOrgID, 10)
	if err := env.queryCHConn.QueryRow(env.ctx,
		"SELECT count() FROM forge_metal.metering WHERE org_id = $1 AND product_id = $2 AND source_ref = $3",
		orgIDStr, "sandbox", submit.AttemptID,
	).Scan(&meteringCount); err != nil {
		t.Fatalf("query repo_exec metering: %v", err)
	}
	if meteringCount != 1 {
		t.Fatalf("expected 1 metering row for repo_exec, got %d", meteringCount)
	}

	var (
		eventRepoID       string
		eventGenerationID string
		eventKind         string
	)
	if err := env.queryCHConn.QueryRow(env.ctx, `
		SELECT repo_id, golden_generation_id, kind
		FROM forge_metal.job_events
		WHERE org_id = $1 AND execution_id = $2
	`, testOrgID, submit.ExecutionID).Scan(&eventRepoID, &eventGenerationID, &eventKind); err != nil {
		t.Fatalf("query repo_exec job_event payload: %v", err)
	}
	if eventKind != "repo_exec" {
		t.Fatalf("expected job_event kind=repo_exec, got %q", eventKind)
	}
	if eventRepoID != repo.RepoID {
		t.Fatalf("expected repo_id=%s, got %s", repo.RepoID, eventRepoID)
	}
	if eventGenerationID != repo.ActiveGoldenGenerationID {
		t.Fatalf("expected golden_generation_id=%s, got %s", repo.ActiveGoldenGenerationID, eventGenerationID)
	}

	assertSystemLogMirrored(t, env.ctx, env.queryCHConn, submit.AttemptID)
}

func TestSubmitExecutionAPI_ImportedRepoRejectsPreparingRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e tests require real databases")
	}

	env := startRepoBootstrapEnv(t, &fakeRunner{
		delay:     3 * time.Second,
		logs:      "slow bootstrap\n",
		commitSHA: "",
	})
	defer env.close()
	env.runner.setCommitSHA(env.repoHead)

	imported := importRepoAgainstServer(t, env.ctx, env.rentalServer.URL, env.token, env.repoPath)
	if imported.State != "preparing" {
		t.Fatalf("expected repo import to remain preparing, got %q", imported.State)
	}

	expectRepoExecStatus(t, env.ctx, env.rentalServer.URL, env.token, imported.RepoID, http.StatusConflict)
	waitForRepoState(t, env.ctx, env.rentalServer.URL, env.token, imported.RepoID, "ready")

	flushBillingMetering(t, env.ctx, env.billingServer)
	var eventCount uint64
	if err := env.queryCHConn.QueryRow(env.ctx,
		"SELECT count() FROM forge_metal.job_events WHERE org_id = $1 AND kind = 'repo_exec'",
		testOrgID,
	).Scan(&eventCount); err != nil {
		t.Fatalf("query repo_exec event count: %v", err)
	}
	if eventCount != 0 {
		t.Fatalf("expected 0 repo_exec job_events for rejected submit, got %d", eventCount)
	}
}

func submitImportedRepoExec(t *testing.T, ctx context.Context, baseURL, token, repoID string) executionSubmitView {
	t.Helper()
	body := map[string]any{
		"kind":       "repo_exec",
		"product_id": "sandbox",
		"repo_id":    repoID,
	}
	return doJSONRequest[executionSubmitView](t, ctx, baseURL+"/api/v1/executions", token, http.MethodPost, body, http.StatusCreated)
}

func expectRepoExecStatus(t *testing.T, ctx context.Context, baseURL, token, repoID string, wantStatus int) {
	t.Helper()
	body := map[string]any{
		"kind":       "repo_exec",
		"product_id": "sandbox",
		"repo_id":    repoID,
	}
	_ = doJSONRequestStatusOnly(t, ctx, baseURL+"/api/v1/executions", token, http.MethodPost, body, wantStatus)
}

func getExecutionAgainstServer(t *testing.T, ctx context.Context, baseURL, token, executionID string) executionView {
	t.Helper()
	return doJSONRequest[executionView](t, ctx, baseURL+"/api/v1/executions/"+executionID, token, http.MethodGet, nil, http.StatusOK)
}

func waitForExecutionState(t *testing.T, ctx context.Context, baseURL, token, executionID, terminalState string) executionView {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		execution := getExecutionAgainstServer(t, ctx, baseURL, token, executionID)
		switch execution.Status {
		case terminalState:
			return execution
		case "queued", "reserved", "launching", "running", "finalizing":
			time.Sleep(100 * time.Millisecond)
			continue
		default:
			t.Fatalf("execution reached unexpected state %q", execution.Status)
		}
	}
	t.Fatalf("execution did not reach %s before timeout", terminalState)
	return executionView{}
}
