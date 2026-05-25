package githubintegration

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/verself/github-integration-service/internal/store"
)

const (
	runnerJITConfigTimeout       = 10 * time.Second
	runnerSandboxSubmitTimeout   = 10 * time.Second
	runnerProviderSurfaceTimeout = 10 * time.Second
)

type runnerCapacityRef struct {
	OrgID                  string
	InstallationBindingID  uuid.UUID
	RepositoryBindingID    uuid.UUID
	ProviderInstallationID int64
	ProviderRepositoryID   int64
	RepositoryFullName     string
	ProviderRunID          int64
	ProviderRunAttempt     int64
	ProviderJobID          int64
	RunnerID               int64
	RunnerName             string
	RunnerClass            string
	AllocationID           uuid.UUID
	ExecutionID            uuid.UUID
	AttemptID              uuid.UUID
}

type runnerCapacityFailure struct {
	DemandState          string
	Problems             runnerProblemSet
	RequireRunnerFailure bool
	DeleteRunner         bool
	SurfaceToProvider    bool
	OutboxCommandSHA256  string
	OutboxState          string
}

func (s *Service) terminalizeRunnerCapacity(ctx context.Context, ref runnerCapacityRef, failure runnerCapacityFailure) error {
	if s == nil || s.cfg.PG == nil {
		return ErrConfiguration
	}
	if failure.Problems.empty() {
		failure.Problems.add(githubRunnerProblem("runner_capacity", "github_runner.capacity_failed", "GitHub runner capacity failed", "", false))
	}
	if strings.TrimSpace(failure.DemandState) == "" {
		failure.DemandState = "capacity_failed"
	}
	if strings.TrimSpace(failure.OutboxState) == "" {
		failure.OutboxState = "failed"
	}
	now := time.Now().UTC()
	tx, err := s.cfg.PG.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	qtx := store.New(tx)

	runnerFailed := strings.TrimSpace(ref.RunnerName) == ""
	if strings.TrimSpace(ref.RunnerName) != "" {
		rows, err := qtx.MarkRunnerInstanceFailed(ctx, store.MarkRunnerInstanceFailedParams{
			UpdatedAt:  pgTime(now),
			RunnerName: ref.RunnerName,
		})
		if err != nil {
			return err
		}
		runnerFailed = rows > 0
		if runnerFailed {
			if err := appendRunnerInstanceProblems(ctx, qtx, ref.RunnerName, failure.Problems); err != nil {
				return err
			}
		}
	}

	demandFailed := false
	if ref.ProviderJobID != 0 && (!failure.RequireRunnerFailure || runnerFailed) {
		rows, err := qtx.MarkProviderDemandFailed(ctx, store.MarkProviderDemandFailedParams{
			State:         failure.DemandState,
			UpdatedAt:     pgTime(now),
			ProviderJobID: ref.ProviderJobID,
		})
		if err != nil {
			return err
		}
		demandFailed = rows > 0
		if demandFailed {
			if err := appendProviderDemandProblems(ctx, qtx, ref.ProviderJobID, failure.Problems); err != nil {
				return err
			}
		}
	}
	if strings.TrimSpace(failure.OutboxCommandSHA256) != "" {
		if err := qtx.MarkProviderOutboxFailed(ctx, store.MarkProviderOutboxFailedParams{
			CommandKind:   "sandbox_submit_runner_job",
			CommandSha256: failure.OutboxCommandSHA256,
			State:         failure.OutboxState,
			FailureReason: truncate(failure.Problems.reason(), 1024),
			UpdatedAt:     pgTime(now),
		}); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if failure.RequireRunnerFailure && !runnerFailed {
		return nil
	}

	meta := runnerCapacityMetadata(ref)
	s.writeEvent(ctx, githubEventFromMetadata(meta, "github.runner.capacity.failed", "failed", failure.Problems.reason(), now, time.Now().UTC()))
	if failure.DeleteRunner {
		s.deleteRunnerCapacity(ctx, ref)
	}
	if failure.SurfaceToProvider && demandFailed {
		s.surfaceRunnerCapacityFailureToProvider(ctx, ref, failure.Problems.reason())
	}
	return nil
}

func (s *Service) deleteRunnerCapacity(ctx context.Context, ref runnerCapacityRef) {
	owner, _, ok := strings.Cut(strings.TrimSpace(ref.RepositoryFullName), "/")
	if !ok || owner == "" || ref.ProviderInstallationID == 0 {
		return
	}
	if ref.RunnerID != 0 {
		if err := s.deleteRunner(ctx, ref.ProviderInstallationID, owner, ref.RunnerID); err != nil && s.cfg.Logger != nil {
			s.cfg.Logger.WarnContext(ctx, "delete failed github runner capacity",
				"runner_name", ref.RunnerName,
				"runner_id", ref.RunnerID,
				"error", err)
		}
		return
	}
	if strings.TrimSpace(ref.RunnerName) != "" {
		if err := s.deleteRunnerByName(ctx, ref.ProviderInstallationID, owner, ref.RunnerName); err != nil && s.cfg.Logger != nil {
			s.cfg.Logger.WarnContext(ctx, "delete failed github runner capacity by name",
				"runner_name", ref.RunnerName,
				"error", err)
		}
	}
}

func (s *Service) surfaceRunnerCapacityFailureToProvider(ctx context.Context, ref runnerCapacityRef, reason string) {
	if ref.ProviderInstallationID == 0 || ref.ProviderRunID == 0 || strings.TrimSpace(ref.RepositoryFullName) == "" {
		return
	}
	started := time.Now().UTC()
	result := "succeeded"
	cancelReason := reason
	cancelCtx, cancel := context.WithTimeout(ctx, runnerProviderSurfaceTimeout)
	defer cancel()
	if err := s.cancelWorkflowRun(cancelCtx, ref.ProviderInstallationID, ref.RepositoryFullName, ref.ProviderRunID); err != nil {
		result = "failed"
		cancelReason = err.Error()
		if s.cfg.Logger != nil {
			s.cfg.Logger.WarnContext(ctx, "cancel failed github workflow run after internal capacity failure",
				"provider_run_id", ref.ProviderRunID,
				"provider_job_id", ref.ProviderJobID,
				"error", err)
		}
		s.recordRunnerProviderSurfaceProblem(ctx, ref, err)
	}
	s.writeEvent(ctx, githubEventFromMetadata(runnerCapacityMetadata(ref), "github.workflow_run.cancel_requested", result, truncate(cancelReason, 1024), started, time.Now().UTC()))
}

func (s *Service) recordRunnerProviderSurfaceProblem(ctx context.Context, ref runnerCapacityRef, cause error) {
	problems := runnerProblemSet{}
	problems.add(githubRunnerProblemFromError("provider_surface", "github_runner.provider_surface_failed", "Failed to surface runner failure to GitHub", cause, true))
	tx, err := s.cfg.PG.Begin(ctx)
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	qtx := store.New(tx)
	if err := appendProviderDemandProblems(ctx, qtx, ref.ProviderJobID, problems); err != nil {
		return
	}
	if err := appendRunnerInstanceProblems(ctx, qtx, ref.RunnerName, problems); err != nil {
		return
	}
	_ = tx.Commit(ctx)
}

func runnerCapacityMetadata(ref runnerCapacityRef) webhookMetadata {
	return webhookMetadata{
		EventName:             "workflow_job",
		DeliveryID:            "runner-capacity:" + firstNonEmpty(ref.RunnerName, ref.ProviderJobString()),
		OrgID:                 ref.OrgID,
		InstallationBindingID: ref.InstallationBindingID,
		RepositoryBindingID:   ref.RepositoryBindingID,
		InstallationID:        ref.ProviderInstallationID,
		RepositoryID:          ref.ProviderRepositoryID,
		RepositoryFullName:    ref.RepositoryFullName,
		RunID:                 ref.ProviderRunID,
		RunAttempt:            ref.ProviderRunAttempt,
		JobID:                 ref.ProviderJobID,
		RunnerID:              ref.RunnerID,
		RunnerName:            ref.RunnerName,
		RunnerClass:           ref.RunnerClass,
		AllocationID:          ref.AllocationID,
		ExecutionID:           ref.ExecutionID,
		AttemptID:             ref.AttemptID,
	}
}

func (ref runnerCapacityRef) ProviderJobString() string {
	if ref.ProviderJobID == 0 {
		return "unknown"
	}
	return strconv.FormatInt(ref.ProviderJobID, 10)
}
