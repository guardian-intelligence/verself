package e2e_test

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSubmitExecutionAPI_ForgejoRunnerUsesRepoGolden(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e tests require real databases")
	}

	env := startRepoBootstrapEnv(t, &fakeRunner{
		delay:     400 * time.Millisecond,
		logs:      "forgejo runner completed\n",
		commitSHA: "",
	})
	defer env.close()
	env.runner.setCommitSHA(env.repoHead)

	imported := importRepoAgainstServer(t, env.ctx, env.rentalServer.URL, env.token, env.repoPath)
	repo := waitForRepoState(t, env.ctx, env.rentalServer.URL, env.token, imported.RepoID, "ready")
	generations := listRepoGenerations(t, env.ctx, env.rentalServer.URL, env.token, repo.RepoID)
	if len(generations) != 1 {
		t.Fatalf("expected 1 generation, got %d", len(generations))
	}
	activeGeneration := generations[0]
	if activeGeneration.SnapshotRef == "" {
		t.Fatal("expected active snapshot_ref")
	}

	submit := submitForgejoRunner(t, env.ctx, env.rentalServer.URL, env.token, repo.RepoID)
	execution := waitForExecutionState(t, env.ctx, env.rentalServer.URL, env.token, submit.ExecutionID, "succeeded")
	if execution.Kind != "forgejo_runner" {
		t.Fatalf("expected kind=forgejo_runner, got %q", execution.Kind)
	}
	if execution.GoldenGenerationID != repo.ActiveGoldenGenerationID {
		t.Fatalf("expected golden_generation_id=%s, got %s", repo.ActiveGoldenGenerationID, execution.GoldenGenerationID)
	}
	if execution.LatestAttempt.RunnerName == "" {
		t.Fatal("expected runner_name on forgejo_runner attempt")
	}
	if execution.ProviderRunID != "run-123" {
		t.Fatalf("expected provider_run_id=run-123, got %q", execution.ProviderRunID)
	}
	if execution.ProviderJobID != "job-456" {
		t.Fatalf("expected provider_job_id=job-456, got %q", execution.ProviderJobID)
	}

	lastConfig, lastJob := env.runner.lastInvocation()
	expectedGoldenZvol := snapshotDatasetToGoldenZvol(activeGeneration.SnapshotRef)
	if lastConfig.GoldenZvol != expectedGoldenZvol {
		t.Fatalf("expected forgejo runner golden zvol=%s, got %s", expectedGoldenZvol, lastConfig.GoldenZvol)
	}
	if lastJob.Env["FORGE_METAL_PROVIDER_RUN_ID"] != "run-123" {
		t.Fatalf("provider run id env: got %q", lastJob.Env["FORGE_METAL_PROVIDER_RUN_ID"])
	}
	if lastJob.Env["FORGE_METAL_PROVIDER_JOB_ID"] != "job-456" {
		t.Fatalf("provider job id env: got %q", lastJob.Env["FORGE_METAL_PROVIDER_JOB_ID"])
	}
	if lastJob.Env["FORGEJO_RUNNER_LABEL"] != "forge-metal" {
		t.Fatalf("runner label env: got %q", lastJob.Env["FORGEJO_RUNNER_LABEL"])
	}
	if !strings.Contains(strings.Join(lastJob.RunCommand, " "), "forgejo-runner") {
		t.Fatalf("expected forgejo-runner command, got %q", strings.Join(lastJob.RunCommand, " "))
	}

	assertWarmGoldenBillingWindow(t, env.ctx, env.pg.rentalDB, submit.AttemptID)
	flushBillingMetering(t, env.ctx, env.billingServer)

	var (
		eventRepoID        string
		eventGenerationID  string
		eventProviderRunID string
		eventProviderJobID string
		eventWorkflowJob   string
		eventRunnerName    string
	)
	if err := env.queryCHConn.QueryRow(env.ctx, `
		SELECT repo_id, golden_generation_id, provider_run_id, provider_job_id, workflow_job_name, runner_name
		FROM forge_metal.job_events
		WHERE org_id = $1 AND execution_id = $2 AND kind = 'forgejo_runner'
	`, testOrgID, submit.ExecutionID).Scan(
		&eventRepoID,
		&eventGenerationID,
		&eventProviderRunID,
		&eventProviderJobID,
		&eventWorkflowJob,
		&eventRunnerName,
	); err != nil {
		t.Fatalf("query forgejo_runner job_event payload: %v", err)
	}
	if eventRepoID != repo.RepoID {
		t.Fatalf("expected repo_id=%s, got %s", repo.RepoID, eventRepoID)
	}
	if eventGenerationID != repo.ActiveGoldenGenerationID {
		t.Fatalf("expected golden_generation_id=%s, got %s", repo.ActiveGoldenGenerationID, eventGenerationID)
	}
	if eventProviderRunID != "run-123" || eventProviderJobID != "job-456" {
		t.Fatalf("unexpected provider ids in job_event: run=%q job=%q", eventProviderRunID, eventProviderJobID)
	}
	if eventWorkflowJob != "build" {
		t.Fatalf("expected workflow_job_name=build, got %q", eventWorkflowJob)
	}
	if eventRunnerName == "" {
		t.Fatal("expected runner_name mirrored into job_events")
	}

	var meteringCount uint64
	orgIDStr := strconv.FormatUint(testOrgID, 10)
	if err := env.queryCHConn.QueryRow(env.ctx,
		"SELECT count() FROM forge_metal.metering WHERE org_id = $1 AND product_id = $2 AND source_ref = $3",
		orgIDStr, "sandbox", submit.AttemptID,
	).Scan(&meteringCount); err != nil {
		t.Fatalf("query forgejo_runner metering: %v", err)
	}
	if meteringCount != 1 {
		t.Fatalf("expected 1 forgejo_runner metering row, got %d", meteringCount)
	}

	assertSystemLogMirrored(t, env.ctx, env.queryCHConn, submit.AttemptID)
}

func submitForgejoRunner(t *testing.T, ctx context.Context, baseURL, token, repoID string) executionSubmitView {
	t.Helper()
	body := map[string]any{
		"kind":              "forgejo_runner",
		"product_id":        "sandbox",
		"repo_id":           repoID,
		"provider_run_id":   "run-123",
		"provider_job_id":   "job-456",
		"workflow_job_name": "build",
	}
	return doJSONRequest[executionSubmitView](t, ctx, baseURL+"/api/v1/executions", token, http.MethodPost, body, http.StatusCreated)
}

func snapshotDatasetToGoldenZvol(snapshotRef string) string {
	dataset := strings.TrimSpace(snapshotRef)
	if idx := strings.Index(dataset, "@"); idx >= 0 {
		dataset = dataset[:idx]
	}
	if slash := strings.Index(dataset, "/"); slash >= 0 {
		parts := strings.SplitN(dataset, "/", 2)
		if len(parts) == 2 {
			dataset = parts[1]
		}
	}
	return strings.Trim(dataset, "/")
}
