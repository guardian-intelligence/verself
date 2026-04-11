package e2e_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
)

func TestRepoLifecycleAndGoldenGenerationActivation(t *testing.T) {
	if testing.Short() {
		t.Skip("repo lifecycle test requires real postgres")
	}

	ctx := context.Background()
	pg := startPostgresForE2E(t)
	svc := &jobs.Service{PG: pg.rentalDB}

	repo, err := svc.CreateRepo(ctx, jobs.CreateRepoRequest{
		OrgID:          testOrgID,
		Provider:       "forgejo",
		ProviderRepoID: "repo-001",
		Owner:          "acme",
		Name:           "webapp",
		CloneURL:       "https://93.184.216.34/acme/webapp.git",
	})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	if repo.State != jobs.RepoStateImporting {
		t.Fatalf("repo state: got %q", repo.State)
	}
	if repo.RunnerProfileSlug != jobs.RunnerProfileForgeMetal {
		t.Fatalf("runner_profile_slug: got %q", repo.RunnerProfileSlug)
	}

	repos, err := svc.ListRepos(ctx, testOrgID)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(repos))
	}

	repo, err = svc.RecordRepoCompatibility(ctx, repo.RepoID, jobs.RepoCompatibilityResult{
		Compatible:     true,
		LastScannedSHA: "sha-bootstrap",
	})
	if err != nil {
		t.Fatalf("RecordRepoCompatibility: %v", err)
	}
	if repo.State != jobs.RepoStateWaitingForBootstrap {
		t.Fatalf("repo state after compatibility: got %q", repo.State)
	}
	if repo.CompatibilityStatus != jobs.CompatibilityStatusCompatible {
		t.Fatalf("compatibility_status: got %q", repo.CompatibilityStatus)
	}

	gen1, err := svc.CreateGoldenGeneration(ctx, repo.RepoID, jobs.CreateGoldenGenerationRequest{
		SourceRef: "refs/heads/main",
		SourceSHA: "sha-bootstrap",
	})
	if err != nil {
		t.Fatalf("CreateGoldenGeneration gen1: %v", err)
	}
	if gen1.State != jobs.GenerationStateQueued {
		t.Fatalf("gen1 state: got %q", gen1.State)
	}

	repo, err = svc.GetRepo(ctx, testOrgID, repo.RepoID)
	if err != nil {
		t.Fatalf("GetRepo preparing: %v", err)
	}
	if repo.State != jobs.RepoStatePreparing {
		t.Fatalf("repo state after generation create: got %q", repo.State)
	}

	executionID := uuid.New()
	attemptID := uuid.New()
	insertMinimalExecutionAttempt(t, ctx, pg.rentalDB, executionID, attemptID)
	if err := svc.AttachGoldenGenerationExecution(ctx, gen1.GoldenGenerationID, executionID, attemptID, attemptID.String()); err != nil {
		t.Fatalf("AttachGoldenGenerationExecution gen1: %v", err)
	}
	if err := svc.SetGoldenGenerationState(ctx, gen1.GoldenGenerationID, jobs.GenerationStateBuilding, "", ""); err != nil {
		t.Fatalf("SetGoldenGenerationState building gen1: %v", err)
	}
	if err := svc.SetGoldenGenerationState(ctx, gen1.GoldenGenerationID, jobs.GenerationStateSanitizing, "", ""); err != nil {
		t.Fatalf("SetGoldenGenerationState sanitizing gen1: %v", err)
	}
	if err := svc.ActivateGoldenGeneration(ctx, repo.RepoID, gen1.GoldenGenerationID, "snapshot-1"); err != nil {
		t.Fatalf("ActivateGoldenGeneration gen1: %v", err)
	}

	repo, err = svc.GetRepo(ctx, testOrgID, repo.RepoID)
	if err != nil {
		t.Fatalf("GetRepo ready: %v", err)
	}
	if repo.State != jobs.RepoStateReady {
		t.Fatalf("repo ready state: got %q", repo.State)
	}
	if repo.ActiveGoldenGenerationID == nil || *repo.ActiveGoldenGenerationID != gen1.GoldenGenerationID {
		t.Fatalf("active_golden_generation_id: got %v", repo.ActiveGoldenGenerationID)
	}
	if repo.LastReadySHA != "sha-bootstrap" {
		t.Fatalf("last_ready_sha: got %q", repo.LastReadySHA)
	}

	gen1, err = svc.GetGoldenGeneration(ctx, repo.RepoID, gen1.GoldenGenerationID)
	if err != nil {
		t.Fatalf("GetGoldenGeneration gen1: %v", err)
	}
	if gen1.State != jobs.GenerationStateReady {
		t.Fatalf("gen1 ready state: got %q", gen1.State)
	}
	if gen1.ExecutionID == nil || *gen1.ExecutionID != executionID {
		t.Fatalf("gen1 execution_id: got %v", gen1.ExecutionID)
	}
	if gen1.AttemptID == nil || *gen1.AttemptID != attemptID {
		t.Fatalf("gen1 attempt_id: got %v", gen1.AttemptID)
	}
	if gen1.SnapshotRef != "snapshot-1" {
		t.Fatalf("gen1 snapshot_ref: got %q", gen1.SnapshotRef)
	}
	if gen1.ActivatedAt == nil {
		t.Fatal("expected gen1 activated_at")
	}

	gen2, err := svc.CreateGoldenGeneration(ctx, repo.RepoID, jobs.CreateGoldenGenerationRequest{
		SourceRef:     "refs/heads/main",
		SourceSHA:     "sha-refresh-failed",
		TriggerReason: jobs.GenerationTriggerDefaultBranchPush,
	})
	if err != nil {
		t.Fatalf("CreateGoldenGeneration gen2: %v", err)
	}
	if err := svc.SetGoldenGenerationState(ctx, gen2.GoldenGenerationID, jobs.GenerationStateFailed, "warm_failed", "refresh failed"); err != nil {
		t.Fatalf("SetGoldenGenerationState failed gen2: %v", err)
	}

	repo, err = svc.GetRepo(ctx, testOrgID, repo.RepoID)
	if err != nil {
		t.Fatalf("GetRepo degraded: %v", err)
	}
	if repo.State != jobs.RepoStateDegraded {
		t.Fatalf("repo degraded state: got %q", repo.State)
	}
	if repo.ActiveGoldenGenerationID == nil || *repo.ActiveGoldenGenerationID != gen1.GoldenGenerationID {
		t.Fatalf("repo active generation after failed refresh: got %v", repo.ActiveGoldenGenerationID)
	}
	if repo.LastError != "refresh failed" {
		t.Fatalf("repo last_error: got %q", repo.LastError)
	}

	gen2, err = svc.GetGoldenGeneration(ctx, repo.RepoID, gen2.GoldenGenerationID)
	if err != nil {
		t.Fatalf("GetGoldenGeneration gen2: %v", err)
	}
	if gen2.State != jobs.GenerationStateFailed {
		t.Fatalf("gen2 failed state: got %q", gen2.State)
	}
	if gen2.FailureReason != "warm_failed" {
		t.Fatalf("gen2 failure_reason: got %q", gen2.FailureReason)
	}

	gen3, err := svc.CreateGoldenGeneration(ctx, repo.RepoID, jobs.CreateGoldenGenerationRequest{
		SourceRef:     "refs/heads/main",
		SourceSHA:     "sha-refresh-success",
		TriggerReason: jobs.GenerationTriggerDefaultBranchPush,
	})
	if err != nil {
		t.Fatalf("CreateGoldenGeneration gen3: %v", err)
	}
	if err := svc.SetGoldenGenerationState(ctx, gen3.GoldenGenerationID, jobs.GenerationStateBuilding, "", ""); err != nil {
		t.Fatalf("SetGoldenGenerationState building gen3: %v", err)
	}
	if err := svc.SetGoldenGenerationState(ctx, gen3.GoldenGenerationID, jobs.GenerationStateSanitizing, "", ""); err != nil {
		t.Fatalf("SetGoldenGenerationState sanitizing gen3: %v", err)
	}
	if err := svc.ActivateGoldenGeneration(ctx, repo.RepoID, gen3.GoldenGenerationID, "snapshot-2"); err != nil {
		t.Fatalf("ActivateGoldenGeneration gen3: %v", err)
	}

	repo, err = svc.GetRepo(ctx, testOrgID, repo.RepoID)
	if err != nil {
		t.Fatalf("GetRepo ready after refresh: %v", err)
	}
	if repo.State != jobs.RepoStateReady {
		t.Fatalf("repo ready after refresh: got %q", repo.State)
	}
	if repo.ActiveGoldenGenerationID == nil || *repo.ActiveGoldenGenerationID != gen3.GoldenGenerationID {
		t.Fatalf("active_golden_generation_id after refresh: got %v", repo.ActiveGoldenGenerationID)
	}
	if repo.LastReadySHA != "sha-refresh-success" {
		t.Fatalf("last_ready_sha after refresh: got %q", repo.LastReadySHA)
	}
	if repo.LastError != "" {
		t.Fatalf("expected cleared last_error, got %q", repo.LastError)
	}

	gen1, err = svc.GetGoldenGeneration(ctx, repo.RepoID, gen1.GoldenGenerationID)
	if err != nil {
		t.Fatalf("GetGoldenGeneration gen1 after supersede: %v", err)
	}
	if gen1.State != jobs.GenerationStateSuperseded {
		t.Fatalf("gen1 superseded state: got %q", gen1.State)
	}
	if gen1.SupersededAt == nil {
		t.Fatal("expected gen1 superseded_at")
	}

	gen3, err = svc.GetGoldenGeneration(ctx, repo.RepoID, gen3.GoldenGenerationID)
	if err != nil {
		t.Fatalf("GetGoldenGeneration gen3: %v", err)
	}
	if gen3.State != jobs.GenerationStateReady {
		t.Fatalf("gen3 ready state: got %q", gen3.State)
	}
	if gen3.SnapshotRef != "snapshot-2" {
		t.Fatalf("gen3 snapshot_ref: got %q", gen3.SnapshotRef)
	}
}

func TestRepoCompatibilityActionRequired(t *testing.T) {
	if testing.Short() {
		t.Skip("repo lifecycle test requires real postgres")
	}

	ctx := context.Background()
	pg := startPostgresForE2E(t)
	svc := &jobs.Service{PG: pg.rentalDB}

	repo, err := svc.CreateRepo(ctx, jobs.CreateRepoRequest{
		OrgID:          testOrgID,
		Provider:       "forgejo",
		ProviderRepoID: "repo-002",
		Owner:          "acme",
		Name:           "legacy",
		CloneURL:       "https://93.184.216.34/acme/legacy.git",
	})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	repo, err = svc.RecordRepoCompatibility(ctx, repo.RepoID, jobs.RepoCompatibilityResult{
		Compatible:           false,
		CompatibilityStatus:  "unsupported_labels",
		CompatibilitySummary: []byte(`{"unsupported_labels":["ubuntu-latest"]}`),
		LastScannedSHA:       "sha-legacy",
	})
	if err != nil {
		t.Fatalf("RecordRepoCompatibility incompatible: %v", err)
	}
	if repo.State != jobs.RepoStateActionRequired {
		t.Fatalf("repo action_required state: got %q", repo.State)
	}
	if repo.CompatibilityStatus != "unsupported_labels" {
		t.Fatalf("compatibility_status: got %q", repo.CompatibilityStatus)
	}
}

func insertMinimalExecutionAttempt(t *testing.T, ctx context.Context, db *sql.DB, executionID, attemptID uuid.UUID) {
	t.Helper()

	now := time.Date(2026, time.April, 9, 0, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO executions (
			execution_id, org_id, actor_id, kind, provider, product_id, status, latest_attempt_id, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, '', 'sandbox', $5, $6, $7, $7
		)
	`, executionID, int64(testOrgID), testUserID, jobs.KindWarmGolden, jobs.StateQueued, attemptID, now); err != nil {
		t.Fatalf("insert execution: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO execution_attempts (
			attempt_id, execution_id, attempt_seq, state, created_at, updated_at
		) VALUES ($1, $2, 1, $3, $4, $4)
	`, attemptID, executionID, jobs.StateQueued, now); err != nil {
		t.Fatalf("insert execution attempt: %v", err)
	}
}
