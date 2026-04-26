package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/verself/sandbox-rental-service/internal/scheduler"
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

type RunnerCapacityReconcileWorker struct {
	river.WorkerDefaults[scheduler.RunnerCapacityReconcileArgs]
	service *Service
}

type RunnerAllocateWorker struct {
	river.WorkerDefaults[scheduler.RunnerAllocateArgs]
	service *Service
}

type RunnerJobBindWorker struct {
	river.WorkerDefaults[scheduler.RunnerJobBindArgs]
	service *Service
}

type RunnerCleanupWorker struct {
	river.WorkerDefaults[scheduler.RunnerCleanupArgs]
	service *Service
}

type RunnerRepositorySyncWorker struct {
	river.WorkerDefaults[scheduler.RunnerRepositorySyncArgs]
	service *Service
}

func RegisterSchedulerWorkers(workers *river.Workers, service *Service) error {
	if service == nil {
		return fmt.Errorf("register scheduler workers: nil jobs service")
	}
	river.AddWorker(workers, &ExecutionAdvanceWorker{service: service})
	river.AddWorker(workers, &RunnerCapacityReconcileWorker{service: service})
	river.AddWorker(workers, &RunnerAllocateWorker{service: service})
	river.AddWorker(workers, &RunnerJobBindWorker{service: service})
	river.AddWorker(workers, &RunnerCleanupWorker{service: service})
	river.AddWorker(workers, &RunnerRepositorySyncWorker{service: service})
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
		attribute.String("verself.correlation_id", args.CorrelationID),
	)

	if err := w.service.AdvanceExecution(ctx, executionID, attemptID); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

func (w *RunnerCapacityReconcileWorker) Work(ctx context.Context, job *river.Job[scheduler.RunnerCapacityReconcileArgs]) error {
	args := job.Args
	ctx = WithCorrelationID(ctx, args.CorrelationID)
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("runner.provider", args.Provider),
		attribute.Int64("runner.provider_installation_id", args.ProviderInstallationID),
		attribute.Int64("runner.provider_repository_id", args.ProviderRepositoryID),
		attribute.Int64("runner.provider_job_id", args.ProviderJobID),
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", scheduler.RunnerCapacityReconcileKind),
		attribute.String("river.queue", scheduler.QueueRunner),
		attribute.String("verself.correlation_id", args.CorrelationID),
	)
	if err := w.service.ReconcileRunnerCapacity(ctx, args.Provider, args.ProviderJobID); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

func (w *RunnerAllocateWorker) Work(ctx context.Context, job *river.Job[scheduler.RunnerAllocateArgs]) error {
	args := job.Args
	ctx = WithCorrelationID(ctx, args.CorrelationID)
	allocationID, err := uuid.Parse(args.AllocationID)
	if err != nil {
		return fmt.Errorf("parse allocation_id: %w", err)
	}
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("runner.allocation_id", allocationID.String()),
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", scheduler.RunnerAllocateKind),
		attribute.String("river.queue", scheduler.QueueRunner),
		attribute.String("verself.correlation_id", args.CorrelationID),
	)
	if err := w.service.AllocateRunner(ctx, allocationID); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

func (w *RunnerJobBindWorker) Work(ctx context.Context, job *river.Job[scheduler.RunnerJobBindArgs]) error {
	args := job.Args
	ctx = WithCorrelationID(ctx, args.CorrelationID)
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("runner.provider", args.Provider),
		attribute.Int64("runner.provider_job_id", args.ProviderJobID),
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", scheduler.RunnerJobBindKind),
		attribute.String("river.queue", scheduler.QueueRunner),
		attribute.String("verself.correlation_id", args.CorrelationID),
	)
	if err := w.service.BindRunnerJob(ctx, args.Provider, args.ProviderJobID); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

func (w *RunnerCleanupWorker) Work(ctx context.Context, job *river.Job[scheduler.RunnerCleanupArgs]) error {
	args := job.Args
	ctx = WithCorrelationID(ctx, args.CorrelationID)
	allocationID, err := uuid.Parse(args.AllocationID)
	if err != nil {
		return fmt.Errorf("parse allocation_id: %w", err)
	}
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("runner.allocation_id", allocationID.String()),
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", scheduler.RunnerCleanupKind),
		attribute.String("river.queue", scheduler.QueueRunner),
		attribute.String("verself.correlation_id", args.CorrelationID),
	)
	if err := w.service.CleanupRunner(ctx, allocationID); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

func (w *RunnerRepositorySyncWorker) Work(ctx context.Context, job *river.Job[scheduler.RunnerRepositorySyncArgs]) error {
	args := job.Args
	ctx = WithCorrelationID(ctx, args.CorrelationID)
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("runner.provider", args.Provider),
		attribute.Int64("runner.provider_repository_id", args.ProviderRepositoryID),
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", scheduler.RunnerRepositorySyncKind),
		attribute.String("river.queue", scheduler.QueueRunner),
		attribute.String("verself.correlation_id", args.CorrelationID),
	)
	if err := w.service.SyncRunnerRepository(ctx, args.Provider, args.ProviderRepositoryID); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}
