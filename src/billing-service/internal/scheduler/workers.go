package scheduler

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/riverqueue/river"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type BillingWorkClient interface {
	ProjectPendingWindows(ctx context.Context, limit int) (int, error)
	ProjectPendingOutboxEvents(ctx context.Context, limit int) (int, error)
	ReconcileEntitlements(ctx context.Context, limit int) (int, error)
}

type meteringProjectPendingWorker struct {
	river.WorkerDefaults[MeteringProjectPendingArgs]
	client BillingWorkClient
	logger *slog.Logger
}

type outboxProjectPendingWorker struct {
	river.WorkerDefaults[OutboxProjectPendingArgs]
	client BillingWorkClient
	logger *slog.Logger
}

type entitlementsReconcileWorker struct {
	river.WorkerDefaults[EntitlementsReconcileArgs]
	client BillingWorkClient
	logger *slog.Logger
}

func (w *meteringProjectPendingWorker) Work(ctx context.Context, job *river.Job[MeteringProjectPendingArgs]) error {
	ctx, span := tracer.Start(ctx, "billing.scheduler.metering_project_pending")
	defer span.End()

	limit := normalizedLimit(job.Args.Limit, defaultProjectLimit)
	span.SetAttributes(
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", KindMeteringProjectPending),
		attribute.String("river.queue", QueueMetering),
		attribute.Int("billing.project.limit", limit),
	)

	count, err := w.client.ProjectPendingWindows(ctx, limit)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(attribute.Int("billing.projected_window_count", count))
	w.logger.InfoContext(ctx, "billing metering windows projected", "river_job_id", job.ID, "limit", limit, "count", count)
	return nil
}

func (w *outboxProjectPendingWorker) Work(ctx context.Context, job *river.Job[OutboxProjectPendingArgs]) error {
	ctx, span := tracer.Start(ctx, "billing.scheduler.outbox_project_pending")
	defer span.End()

	limit := normalizedLimit(job.Args.Limit, defaultProjectLimit)
	span.SetAttributes(
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", KindOutboxProjectPending),
		attribute.String("river.queue", QueueOutbox),
		attribute.Int("billing.project.limit", limit),
	)

	count, err := w.client.ProjectPendingOutboxEvents(ctx, limit)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(attribute.Int("billing.projected_outbox_event_count", count))
	w.logger.InfoContext(ctx, "billing outbox events projected", "river_job_id", job.ID, "limit", limit, "count", count)
	return nil
}

func (w *entitlementsReconcileWorker) Work(ctx context.Context, job *river.Job[EntitlementsReconcileArgs]) error {
	ctx, span := tracer.Start(ctx, "billing.scheduler.entitlements_reconcile")
	defer span.End()

	limit := normalizedLimit(job.Args.Limit, defaultReconcileLimit)
	span.SetAttributes(
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", KindEntitlementsReconcile),
		attribute.String("river.queue", QueueReconcile),
		attribute.Int("billing.reconcile.limit", limit),
	)

	count, err := w.client.ReconcileEntitlements(ctx, limit)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(attribute.Int("billing.reconciled_org_count", count))
	w.logger.InfoContext(ctx, "billing entitlements reconciled", "river_job_id", job.ID, "limit", limit, "count", count)
	return nil
}

func registerWorkers(workers *river.Workers, logger *slog.Logger, client BillingWorkClient) error {
	if client == nil {
		return fmt.Errorf("register billing scheduler workers: nil billing work client")
	}
	river.AddWorker(workers, &meteringProjectPendingWorker{client: client, logger: logger})
	river.AddWorker(workers, &outboxProjectPendingWorker{client: client, logger: logger})
	river.AddWorker(workers, &entitlementsReconcileWorker{client: client, logger: logger})
	return nil
}

func normalizedLimit(limit int, fallback int) int {
	if limit > 0 {
		return limit
	}
	return fallback
}
