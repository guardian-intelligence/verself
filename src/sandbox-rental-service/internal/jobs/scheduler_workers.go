package jobs

import (
	"context"
	"fmt"

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

func RegisterSchedulerWorkers(workers *river.Workers, service *Service) error {
	if service == nil {
		return fmt.Errorf("register scheduler workers: nil jobs service")
	}
	river.AddWorker(workers, &ExecutionAdvanceWorker{service: service})
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
