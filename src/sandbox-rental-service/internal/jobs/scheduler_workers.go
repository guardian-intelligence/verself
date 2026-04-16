package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/forge-metal/sandbox-rental-service/internal/scheduler"
	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type ExecutionAdvanceWorker struct {
	river.WorkerDefaults[scheduler.ExecutionAdvanceArgs]
	service *Service
}

func (w *ExecutionAdvanceWorker) Timeout(*river.Job[scheduler.ExecutionAdvanceArgs]) time.Duration {
	// vm-orchestrator enforces exec max_wall; River's default one-minute timeout kills GitHub runners before they can report completion.
	return -1
}

type GitHubCapacityReconcileWorker struct {
	river.WorkerDefaults[scheduler.GitHubCapacityReconcileArgs]
	service *Service
}

type GitHubRunnerAllocateWorker struct {
	river.WorkerDefaults[scheduler.GitHubRunnerAllocateArgs]
	service *Service
}

type GitHubJobBindWorker struct {
	river.WorkerDefaults[scheduler.GitHubJobBindArgs]
	service *Service
}

type GitHubRunnerCleanupWorker struct {
	river.WorkerDefaults[scheduler.GitHubRunnerCleanupArgs]
	service *Service
}

func RegisterSchedulerWorkers(workers *river.Workers, service *Service) error {
	if service == nil {
		return fmt.Errorf("register scheduler workers: nil jobs service")
	}
	river.AddWorker(workers, &ExecutionAdvanceWorker{service: service})
	river.AddWorker(workers, &GitHubCapacityReconcileWorker{service: service})
	river.AddWorker(workers, &GitHubRunnerAllocateWorker{service: service})
	river.AddWorker(workers, &GitHubJobBindWorker{service: service})
	river.AddWorker(workers, &GitHubRunnerCleanupWorker{service: service})
	return nil
}

func (w *ExecutionAdvanceWorker) Work(ctx context.Context, job *river.Job[scheduler.ExecutionAdvanceArgs]) error {
	args := job.Args
	ctx = WithCorrelationID(ctx, args.CorrelationID)

	executionID, err := uuid.Parse(args.ExecutionID)
	if err != nil {
		return fmt.Errorf("parse execution_id: %w", err)
	}
	attemptID, err := uuid.Parse(args.AttemptID)
	if err != nil {
		return fmt.Errorf("parse attempt_id: %w", err)
	}

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("execution.id", executionID.String()),
		attribute.String("attempt.id", attemptID.String()),
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", scheduler.ExecutionAdvanceKind),
		attribute.String("river.queue", scheduler.QueueExecution),
		attribute.String("fm.correlation_id", args.CorrelationID),
	)

	if err := w.service.AdvanceExecution(ctx, executionID, attemptID); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

func (w *GitHubCapacityReconcileWorker) Work(ctx context.Context, job *river.Job[scheduler.GitHubCapacityReconcileArgs]) error {
	args := job.Args
	ctx = WithCorrelationID(ctx, args.CorrelationID)
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int64("github.installation_id", args.InstallationID),
		attribute.Int64("github.repository_id", args.RepositoryID),
		attribute.Int64("github.job_id", args.GitHubJobID),
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", scheduler.GitHubCapacityReconcileKind),
		attribute.String("river.queue", scheduler.QueueRunner),
		attribute.String("fm.correlation_id", args.CorrelationID),
	)
	if w.service.GitHubRunner == nil {
		return ErrGitHubRunnerNotConfigured
	}
	if err := w.service.GitHubRunner.ReconcileCapacity(ctx, args.GitHubJobID); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

func (w *GitHubRunnerAllocateWorker) Work(ctx context.Context, job *river.Job[scheduler.GitHubRunnerAllocateArgs]) error {
	args := job.Args
	ctx = WithCorrelationID(ctx, args.CorrelationID)
	allocationID, err := uuid.Parse(args.AllocationID)
	if err != nil {
		return fmt.Errorf("parse allocation_id: %w", err)
	}
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("github.allocation_id", allocationID.String()),
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", scheduler.GitHubRunnerAllocateKind),
		attribute.String("river.queue", scheduler.QueueRunner),
		attribute.String("fm.correlation_id", args.CorrelationID),
	)
	if w.service.GitHubRunner == nil {
		return ErrGitHubRunnerNotConfigured
	}
	if err := w.service.GitHubRunner.AllocateRunner(ctx, allocationID); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

func (w *GitHubJobBindWorker) Work(ctx context.Context, job *river.Job[scheduler.GitHubJobBindArgs]) error {
	args := job.Args
	ctx = WithCorrelationID(ctx, args.CorrelationID)
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int64("github.job_id", args.GitHubJobID),
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", scheduler.GitHubJobBindKind),
		attribute.String("river.queue", scheduler.QueueRunner),
		attribute.String("fm.correlation_id", args.CorrelationID),
	)
	if w.service.GitHubRunner == nil {
		return ErrGitHubRunnerNotConfigured
	}
	if err := w.service.GitHubRunner.BindJob(ctx, args.GitHubJobID); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

func (w *GitHubRunnerCleanupWorker) Work(ctx context.Context, job *river.Job[scheduler.GitHubRunnerCleanupArgs]) error {
	args := job.Args
	ctx = WithCorrelationID(ctx, args.CorrelationID)
	allocationID, err := uuid.Parse(args.AllocationID)
	if err != nil {
		return fmt.Errorf("parse allocation_id: %w", err)
	}
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("github.allocation_id", allocationID.String()),
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", scheduler.GitHubRunnerCleanupKind),
		attribute.String("river.queue", scheduler.QueueRunner),
		attribute.String("fm.correlation_id", args.CorrelationID),
	)
	if w.service.GitHubRunner == nil {
		return ErrGitHubRunnerNotConfigured
	}
	if err := w.service.GitHubRunner.CleanupRunner(ctx, allocationID); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}
