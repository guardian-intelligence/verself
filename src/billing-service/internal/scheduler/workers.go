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
	ProjectPendingBillingEventDeliveries(ctx context.Context, limit int) (int, error)
	ProjectBillingEventDelivery(ctx context.Context, eventID string, sink string, generation int) (bool, error)
	ReconcileEntitlements(ctx context.Context, limit int) (int, error)
	ApplyPendingProviderEvents(ctx context.Context, limit int) (int, error)
	ApplyProviderEvent(ctx context.Context, eventID string) (bool, error)
	RolloverDueBillingCycles(ctx context.Context, limit int) (int, error)
	FinalizeDueBillingCycles(ctx context.Context, limit int) (int, error)
}

type meteringProjectPendingWorker struct {
	river.WorkerDefaults[MeteringProjectPendingArgs]
	client BillingWorkClient
	logger *slog.Logger
}

type eventDeliveryProjectPendingWorker struct {
	river.WorkerDefaults[EventDeliveryProjectPendingArgs]
	client BillingWorkClient
	logger *slog.Logger
}

type eventDeliveryProjectWorker struct {
	river.WorkerDefaults[EventDeliveryProjectArgs]
	client BillingWorkClient
	logger *slog.Logger
}

type entitlementsReconcileWorker struct {
	river.WorkerDefaults[EntitlementsReconcileArgs]
	client BillingWorkClient
	logger *slog.Logger
}

type providerEventApplyPendingWorker struct {
	river.WorkerDefaults[ProviderEventApplyPendingArgs]
	client BillingWorkClient
	logger *slog.Logger
}

type providerEventApplyWorker struct {
	river.WorkerDefaults[ProviderEventApplyArgs]
	client BillingWorkClient
	logger *slog.Logger
}

type cycleRolloverPendingWorker struct {
	river.WorkerDefaults[CycleRolloverPendingArgs]
	client BillingWorkClient
	logger *slog.Logger
}

type invoiceFinalizePendingWorker struct {
	river.WorkerDefaults[InvoiceFinalizePendingArgs]
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

func (w *eventDeliveryProjectPendingWorker) Work(ctx context.Context, job *river.Job[EventDeliveryProjectPendingArgs]) error {
	ctx, span := tracer.Start(ctx, "billing.scheduler.event_delivery_project_pending")
	defer span.End()

	limit := normalizedLimit(job.Args.Limit, defaultProjectLimit)
	span.SetAttributes(
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", KindEventDeliveryProjectPending),
		attribute.String("river.queue", QueueEventDelivery),
		attribute.Int("billing.project.limit", limit),
	)

	count, err := w.client.ProjectPendingBillingEventDeliveries(ctx, limit)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(attribute.Int("billing.projected_billing_event_delivery_count", count))
	w.logger.InfoContext(ctx, "billing event deliveries projected", "river_job_id", job.ID, "limit", limit, "count", count)
	return nil
}

func (w *eventDeliveryProjectWorker) Work(ctx context.Context, job *river.Job[EventDeliveryProjectArgs]) error {
	ctx, span := tracer.Start(ctx, "billing.scheduler.event_delivery_project")
	defer span.End()

	span.SetAttributes(
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", KindEventDeliveryProject),
		attribute.String("river.queue", QueueEventDelivery),
		attribute.String("billing.event_id", job.Args.EventID),
		attribute.String("billing.event_sink", job.Args.Sink),
		attribute.Int("billing.event_generation", job.Args.Generation),
	)

	projected, err := w.client.ProjectBillingEventDelivery(ctx, job.Args.EventID, job.Args.Sink, job.Args.Generation)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(attribute.Bool("billing.event_projected", projected))
	w.logger.InfoContext(ctx, "billing event delivery projected", "river_job_id", job.ID, "event_id", job.Args.EventID, "sink", job.Args.Sink, "generation", job.Args.Generation, "projected", projected)
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

func (w *providerEventApplyPendingWorker) Work(ctx context.Context, job *river.Job[ProviderEventApplyPendingArgs]) error {
	ctx, span := tracer.Start(ctx, "billing.scheduler.provider_event_apply_pending")
	defer span.End()

	limit := normalizedLimit(job.Args.Limit, defaultProjectLimit)
	span.SetAttributes(
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", KindProviderEventApplyPending),
		attribute.String("river.queue", QueueProvider),
		attribute.Int("billing.provider_event.limit", limit),
	)

	count, err := w.client.ApplyPendingProviderEvents(ctx, limit)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(attribute.Int("billing.provider_event.applied_count", count))
	w.logger.InfoContext(ctx, "billing provider events applied", "river_job_id", job.ID, "limit", limit, "count", count)
	return nil
}

func (w *providerEventApplyWorker) Work(ctx context.Context, job *river.Job[ProviderEventApplyArgs]) error {
	ctx, span := tracer.Start(ctx, "billing.scheduler.provider_event_apply")
	defer span.End()

	span.SetAttributes(
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", KindProviderEventApply),
		attribute.String("river.queue", QueueProvider),
		attribute.String("billing.provider_event.event_id", job.Args.EventID),
	)

	applied, err := w.client.ApplyProviderEvent(ctx, job.Args.EventID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(attribute.Bool("billing.provider_event.applied", applied))
	w.logger.InfoContext(ctx, "billing provider event applied", "river_job_id", job.ID, "event_id", job.Args.EventID, "applied", applied)
	return nil
}

func (w *cycleRolloverPendingWorker) Work(ctx context.Context, job *river.Job[CycleRolloverPendingArgs]) error {
	ctx, span := tracer.Start(ctx, "billing.scheduler.cycle_rollover_pending")
	defer span.End()

	limit := normalizedLimit(job.Args.Limit, defaultProjectLimit)
	span.SetAttributes(
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", KindCycleRolloverPending),
		attribute.String("river.queue", QueueBilling),
		attribute.Int("billing.cycle.limit", limit),
	)

	count, err := w.client.RolloverDueBillingCycles(ctx, limit)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(attribute.Int("billing.cycle.rolled_over_count", count))
	w.logger.InfoContext(ctx, "billing cycles rolled over", "river_job_id", job.ID, "limit", limit, "count", count)
	return nil
}

func (w *invoiceFinalizePendingWorker) Work(ctx context.Context, job *river.Job[InvoiceFinalizePendingArgs]) error {
	ctx, span := tracer.Start(ctx, "billing.scheduler.invoice_finalize_pending")
	defer span.End()

	limit := normalizedLimit(job.Args.Limit, defaultProjectLimit)
	span.SetAttributes(
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", KindInvoiceFinalizePending),
		attribute.String("river.queue", QueueBilling),
		attribute.Int("billing.invoice.limit", limit),
	)

	count, err := w.client.FinalizeDueBillingCycles(ctx, limit)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(attribute.Int("billing.invoice.finalized_count", count))
	w.logger.InfoContext(ctx, "billing invoices finalized", "river_job_id", job.ID, "limit", limit, "count", count)
	return nil
}

func registerWorkers(workers *river.Workers, logger *slog.Logger, client BillingWorkClient) error {
	if client == nil {
		return fmt.Errorf("register billing scheduler workers: nil billing work client")
	}
	river.AddWorker(workers, &meteringProjectPendingWorker{client: client, logger: logger})
	river.AddWorker(workers, &eventDeliveryProjectPendingWorker{client: client, logger: logger})
	river.AddWorker(workers, &eventDeliveryProjectWorker{client: client, logger: logger})
	river.AddWorker(workers, &entitlementsReconcileWorker{client: client, logger: logger})
	river.AddWorker(workers, &providerEventApplyPendingWorker{client: client, logger: logger})
	river.AddWorker(workers, &providerEventApplyWorker{client: client, logger: logger})
	river.AddWorker(workers, &cycleRolloverPendingWorker{client: client, logger: logger})
	river.AddWorker(workers, &invoiceFinalizePendingWorker{client: client, logger: logger})
	return nil
}

func normalizedLimit(limit int, fallback int) int {
	if limit > 0 {
		return limit
	}
	return fallback
}
