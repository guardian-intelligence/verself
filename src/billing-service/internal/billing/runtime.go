package billing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
	"github.com/riverqueue/rivercontrib/otelriver"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	QueueBillingProvider  = "billing_provider"
	QueueBillingDelivery  = "billing_delivery"
	QueueBillingReconcile = "billing_reconcile"

	ProviderEventApplyKind          = "billing.provider_event.apply"
	ProviderEventApplyPendingKind   = "billing.provider_event.apply_pending"
	EventDeliveryProjectPendingKind = "billing.event_delivery.project_pending"
	LedgerDispatchPendingKind       = "billing.ledger.dispatch_pending"
	LedgerReconcileKind             = "billing.ledger.reconcile"
	MeteringProjectWindowKind       = "billing.metering.project_window"
	MeteringProjectPendingKind      = "billing.metering.project_pending"
	DueWorkApplyPendingKind         = "billing.due_work.apply_pending"
)

type Runtime struct {
	client *river.Client[pgx.Tx]
	logger *slog.Logger
}

type ProviderEventApplyArgs struct {
	EventID string `json:"event_id" river:"unique"`
}

func (ProviderEventApplyArgs) Kind() string { return ProviderEventApplyKind }

type ProviderEventApplyPendingArgs struct {
	Limit int `json:"limit"`
}

func (ProviderEventApplyPendingArgs) Kind() string { return ProviderEventApplyPendingKind }

type EventDeliveryProjectPendingArgs struct {
	Sink  string `json:"sink" river:"unique"`
	Limit int    `json:"limit"`
}

func (EventDeliveryProjectPendingArgs) Kind() string { return EventDeliveryProjectPendingKind }

type LedgerDispatchPendingArgs struct {
	Limit int `json:"limit"`
}

func (LedgerDispatchPendingArgs) Kind() string { return LedgerDispatchPendingKind }

type LedgerReconcileArgs struct {
	Limit int `json:"limit"`
}

func (LedgerReconcileArgs) Kind() string { return LedgerReconcileKind }

type MeteringProjectWindowArgs struct {
	WindowID string `json:"window_id" river:"unique"`
}

func (MeteringProjectWindowArgs) Kind() string { return MeteringProjectWindowKind }

type MeteringProjectPendingArgs struct {
	Limit int `json:"limit"`
}

func (MeteringProjectPendingArgs) Kind() string { return MeteringProjectPendingKind }

type DueWorkApplyPendingArgs struct {
	Limit int `json:"limit"`
}

func (DueWorkApplyPendingArgs) Kind() string { return DueWorkApplyPendingKind }

type ProviderEventApplyWorker struct {
	river.WorkerDefaults[ProviderEventApplyArgs]
	billing *Client
}

type ProviderEventApplyPendingWorker struct {
	river.WorkerDefaults[ProviderEventApplyPendingArgs]
	billing *Client
}

type EventDeliveryProjectPendingWorker struct {
	river.WorkerDefaults[EventDeliveryProjectPendingArgs]
	billing *Client
}

type LedgerDispatchPendingWorker struct {
	river.WorkerDefaults[LedgerDispatchPendingArgs]
	billing *Client
}

type LedgerReconcileWorker struct {
	river.WorkerDefaults[LedgerReconcileArgs]
	billing *Client
}

type MeteringProjectWindowWorker struct {
	river.WorkerDefaults[MeteringProjectWindowArgs]
	billing *Client
}

type MeteringProjectPendingWorker struct {
	river.WorkerDefaults[MeteringProjectPendingArgs]
	billing *Client
}

type DueWorkApplyPendingWorker struct {
	river.WorkerDefaults[DueWorkApplyPendingArgs]
	billing *Client
}

func NewRuntime(pool *pgxpool.Pool, billingClient *Client, logger *slog.Logger) (*Runtime, error) {
	if pool == nil {
		return nil, fmt.Errorf("billing runtime requires pgx pool")
	}
	if billingClient == nil {
		return nil, fmt.Errorf("billing runtime requires billing client")
	}
	if logger == nil {
		logger = slog.Default()
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, &ProviderEventApplyWorker{billing: billingClient})
	river.AddWorker(workers, &ProviderEventApplyPendingWorker{billing: billingClient})
	river.AddWorker(workers, &EventDeliveryProjectPendingWorker{billing: billingClient})
	river.AddWorker(workers, &LedgerDispatchPendingWorker{billing: billingClient})
	river.AddWorker(workers, &LedgerReconcileWorker{billing: billingClient})
	river.AddWorker(workers, &MeteringProjectWindowWorker{billing: billingClient})
	river.AddWorker(workers, &MeteringProjectPendingWorker{billing: billingClient})
	river.AddWorker(workers, &DueWorkApplyPendingWorker{billing: billingClient})

	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger: logger,
		Middleware: []rivertype.Middleware{
			otelriver.NewMiddleware(&otelriver.MiddlewareConfig{
				DurationUnit:                "ms",
				EnableSemanticMetrics:       true,
				EnableWorkSpanJobKindSuffix: true,
			}),
		},
		Queues:  queueConfig(),
		Workers: workers,
	})
	if err != nil {
		return nil, fmt.Errorf("create billing river client: %w", err)
	}
	runtime := &Runtime{client: client, logger: logger}
	billingClient.SetRuntime(runtime)
	return runtime, nil
}

func (r *Runtime) Start(ctx context.Context) error {
	if err := r.client.Start(ctx); err != nil {
		return fmt.Errorf("start billing river client: %w", err)
	}
	r.logger.InfoContext(ctx, "billing river runtime started", "queues", queueNames())
	return nil
}

func (r *Runtime) Stop(ctx context.Context) error {
	if err := r.client.Stop(ctx); err != nil {
		return fmt.Errorf("stop billing river client: %w", err)
	}
	r.logger.InfoContext(ctx, "billing river runtime stopped")
	return nil
}

func (r *Runtime) EnqueueProviderEventApplyTx(ctx context.Context, tx pgx.Tx, eventID string) error {
	_, err := r.client.InsertTx(ctx, tx, ProviderEventApplyArgs{EventID: eventID}, &river.InsertOpts{
		MaxAttempts: 5,
		Queue:       QueueBillingProvider,
		Tags:        []string{"provider-event"},
		UniqueOpts:  river.UniqueOpts{ByArgs: true, ByQueue: true},
	})
	if err != nil {
		return fmt.Errorf("enqueue provider event apply: %w", err)
	}
	return nil
}

func (r *Runtime) EnqueueMeteringProjectWindowTx(ctx context.Context, tx pgx.Tx, windowID string) error {
	_, err := r.client.InsertTx(ctx, tx, MeteringProjectWindowArgs{WindowID: windowID}, &river.InsertOpts{
		MaxAttempts: 5,
		Queue:       QueueBillingDelivery,
		Tags:        []string{"metering"},
		UniqueOpts:  river.UniqueOpts{ByArgs: true, ByQueue: true},
	})
	if err != nil {
		return fmt.Errorf("enqueue metering project window: %w", err)
	}
	return nil
}

func (r *Runtime) EnqueueMaintenance(ctx context.Context, cadence time.Duration) error {
	if cadence < time.Second {
		cadence = time.Second
	}
	jobs := []struct {
		args river.JobArgs
		opts *river.InsertOpts
	}{
		{args: EventDeliveryProjectPendingArgs{Sink: clickHouseBillingEventsSink, Limit: 100}, opts: scannerInsertOpts(QueueBillingReconcile, cadence, "event-delivery")},
		{args: LedgerDispatchPendingArgs{Limit: 100}, opts: scannerInsertOpts(QueueBillingDelivery, cadence, "ledger-dispatch")},
		{args: LedgerReconcileArgs{Limit: 1000}, opts: scannerInsertOpts(QueueBillingReconcile, cadence, "ledger-reconcile")},
		{args: ProviderEventApplyPendingArgs{Limit: 100}, opts: scannerInsertOpts(QueueBillingReconcile, cadence, "provider-event")},
		{args: MeteringProjectPendingArgs{Limit: 100}, opts: scannerInsertOpts(QueueBillingReconcile, cadence, "metering")},
		{args: DueWorkApplyPendingArgs{Limit: 100}, opts: scannerInsertOpts(QueueBillingReconcile, cadence, "due-work")},
	}
	for _, job := range jobs {
		if _, err := r.client.Insert(ctx, job.args, job.opts); err != nil {
			return fmt.Errorf("enqueue billing maintenance %s: %w", job.args.Kind(), err)
		}
	}
	return nil
}

func scannerInsertOpts(queue string, cadence time.Duration, tag string) *river.InsertOpts {
	return &river.InsertOpts{
		MaxAttempts: 3,
		Queue:       queue,
		Tags:        []string{tag},
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByQueue:  true,
			ByPeriod: cadence,
		},
	}
}

func (w *ProviderEventApplyWorker) Work(ctx context.Context, job *river.Job[ProviderEventApplyArgs]) error {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("billing.provider_event_id", job.Args.EventID), attribute.Int64("river.job_id", job.ID), attribute.String("river.job_kind", ProviderEventApplyKind), attribute.String("river.queue", QueueBillingProvider))
	_, err := w.billing.ApplyProviderEvent(ctx, job.Args.EventID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

func (w *ProviderEventApplyPendingWorker) Work(ctx context.Context, job *river.Job[ProviderEventApplyPendingArgs]) error {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.Int("billing.limit", job.Args.Limit), attribute.Int64("river.job_id", job.ID), attribute.String("river.job_kind", ProviderEventApplyPendingKind), attribute.String("river.queue", QueueBillingReconcile))
	_, err := w.billing.ApplyPendingProviderEvents(ctx, job.Args.Limit)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

func (w *EventDeliveryProjectPendingWorker) Work(ctx context.Context, job *river.Job[EventDeliveryProjectPendingArgs]) error {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("billing.sink", job.Args.Sink), attribute.Int("billing.limit", job.Args.Limit), attribute.Int64("river.job_id", job.ID), attribute.String("river.job_kind", EventDeliveryProjectPendingKind), attribute.String("river.queue", QueueBillingReconcile))
	_, err := w.billing.ProjectPendingBillingEventDeliveries(ctx, job.Args.Limit)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

func (w *LedgerDispatchPendingWorker) Work(ctx context.Context, job *river.Job[LedgerDispatchPendingArgs]) error {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.Int("billing.limit", job.Args.Limit), attribute.Int64("river.job_id", job.ID), attribute.String("river.job_kind", LedgerDispatchPendingKind), attribute.String("river.queue", QueueBillingDelivery))
	_, err := w.billing.DispatchPendingLedgerCommands(ctx, job.Args.Limit)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

func (w *LedgerReconcileWorker) Work(ctx context.Context, job *river.Job[LedgerReconcileArgs]) error {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.Int("billing.limit", job.Args.Limit), attribute.Int64("river.job_id", job.ID), attribute.String("river.job_kind", LedgerReconcileKind), attribute.String("river.queue", QueueBillingReconcile))
	_, err := w.billing.ReconcileLedger(ctx, job.Args.Limit)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

func (w *MeteringProjectWindowWorker) Work(ctx context.Context, job *river.Job[MeteringProjectWindowArgs]) error {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("billing.window_id", job.Args.WindowID), attribute.Int64("river.job_id", job.ID), attribute.String("river.job_kind", MeteringProjectWindowKind), attribute.String("river.queue", QueueBillingDelivery))
	_, err := w.billing.ProjectMeteringWindow(ctx, job.Args.WindowID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

func (w *MeteringProjectPendingWorker) Work(ctx context.Context, job *river.Job[MeteringProjectPendingArgs]) error {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.Int("billing.limit", job.Args.Limit), attribute.Int64("river.job_id", job.ID), attribute.String("river.job_kind", MeteringProjectPendingKind), attribute.String("river.queue", QueueBillingReconcile))
	_, err := w.billing.ProjectPendingMeteringWindows(ctx, job.Args.Limit)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

func (w *DueWorkApplyPendingWorker) Work(ctx context.Context, job *river.Job[DueWorkApplyPendingArgs]) error {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.Int("billing.limit", job.Args.Limit), attribute.Int64("river.job_id", job.ID), attribute.String("river.job_kind", DueWorkApplyPendingKind), attribute.String("river.queue", QueueBillingReconcile))
	_, err := w.billing.ApplyPendingDueBillingWork(ctx, job.Args.Limit)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

func queueConfig() map[string]river.QueueConfig {
	return map[string]river.QueueConfig{
		QueueBillingProvider:  {MaxWorkers: 2},
		QueueBillingDelivery:  {MaxWorkers: 2},
		QueueBillingReconcile: {MaxWorkers: 1},
	}
}

func queueNames() []string {
	return []string{QueueBillingProvider, QueueBillingDelivery, QueueBillingReconcile}
}
