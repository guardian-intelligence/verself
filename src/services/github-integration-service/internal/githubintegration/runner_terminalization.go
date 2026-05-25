package githubintegration

import (
	"context"
	"errors"
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

func queuedJobCapacityRef(event workflowJobWebhook, runnerClass string) runnerCapacityRef {
	return runnerCapacityRef{
		OrgID:                  event.OrgID,
		InstallationBindingID:  event.InstallationBindingID,
		RepositoryBindingID:    event.RepositoryBindingID,
		ProviderInstallationID: event.Installation.ID,
		ProviderRepositoryID:   event.Repository.ID,
		RepositoryFullName:     event.Repository.FullName,
		ProviderRunID:          event.WorkflowJob.RunID,
		ProviderRunAttempt:     event.WorkflowJob.RunAttempt,
		ProviderJobID:          event.WorkflowJob.ID,
		RunnerClass:            runnerClass,
	}
}

func (s *Service) ensureQueuedProviderDemand(ctx context.Context, event workflowJobWebhook, deliveryID string, runnerClass string, jobShapeID string, trustClass string) (store.EnsureProviderDemandRow, error) {
	return s.queries.EnsureProviderDemand(ctx, store.EnsureProviderDemandParams{
		DemandID:               pgUUID(uuid.New()),
		ProviderJobID:          event.WorkflowJob.ID,
		OrgID:                  event.OrgID,
		InstallationBindingID:  pgUUID(event.InstallationBindingID),
		RepositoryBindingID:    pgUUID(event.RepositoryBindingID),
		ProviderInstallationID: event.Installation.ID,
		ProviderRepositoryID:   event.Repository.ID,
		RepositoryFullName:     event.Repository.FullName,
		ProviderRunID:          event.WorkflowJob.RunID,
		ProviderRunAttempt:     event.WorkflowJob.RunAttempt,
		JobShapeID:             jobShapeID,
		TrustClass:             trustClass,
		RunnerClass:            runnerClass,
		LastDeliveryID:         deliveryID,
		UpdatedAt:              pgTime(time.Now().UTC()),
	})
}

func providerDemandTerminalForCapacity(state string) bool {
	switch strings.TrimSpace(state) {
	case "assigned", "completed", "capacity_failed", "jit_failed", "sandbox_failed":
		return true
	default:
		return false
	}
}

func (s *Service) terminalizeQueuedJobFailure(ctx context.Context, ref runnerCapacityRef, demandState string, code githubProblemCode, err error, retryable bool) error {
	problems := runnerProblemSet{}
	problems.add(githubRunnerProblemFromError(code, err, withProblemRetryable(retryable)))
	if terminalizeErr := s.terminalizeRunnerCapacity(ctx, ref, runnerCapacityFailure{
		DemandState:       demandState,
		Problems:          problems,
		SurfaceToProvider: true,
	}); terminalizeErr != nil {
		return errors.Join(err, terminalizeErr)
	}
	return err
}

func (s *Service) terminalizeRunnerCapacity(ctx context.Context, ref runnerCapacityRef, failure runnerCapacityFailure) error {
	if s == nil || s.cfg.PG == nil {
		return ErrConfiguration
	}
	if failure.Problems.empty() {
		failure.Problems.add(githubRunnerProblemFromCatalog(problemGithubRunnerCapacityFailed))
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
	surfaceQueued := false
	if failure.SurfaceToProvider && demandFailed {
		queued, err := enqueueProviderSurfaceCommandTx(ctx, qtx, ref, failure.Problems, now)
		if err != nil {
			return err
		}
		surfaceQueued = queued
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
	if surfaceQueued {
		s.writeEvent(ctx, githubEventFromMetadata(meta, "github.provider_surface.enqueued", "pending", failure.Problems.reason(), now, time.Now().UTC()))
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
