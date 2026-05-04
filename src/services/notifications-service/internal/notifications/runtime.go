package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
	"github.com/riverqueue/rivercontrib/otelriver"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	QueueFanout     = "notifications_fanout"
	QueueProjection = "notifications_projection"
	QueueReconcile  = "notifications_reconcile"

	EventFanoutKind           = "notifications.event.fanout"
	ProjectionPendingKind     = "notifications.projection.project_pending"
	NotificationReconcileKind = "notifications.reconcile"
)

type Runtime struct {
	client *river.Client[pgx.Tx]
	logger *slog.Logger
}

type EventFanoutArgs struct {
	EventSource string `json:"event_source" river:"unique"`
	EventID     string `json:"event_id" river:"unique"`
	TraceParent string `json:"trace_parent,omitempty"`
	SubmittedAt string `json:"submitted_at"`
}

func (EventFanoutArgs) Kind() string { return EventFanoutKind }

type ProjectionPendingArgs struct {
	Limit       int    `json:"limit"`
	TraceParent string `json:"trace_parent,omitempty"`
	SubmittedAt string `json:"submitted_at"`
}

func (ProjectionPendingArgs) Kind() string { return ProjectionPendingKind }

type ReconcileArgs struct {
	Limit       int    `json:"limit"`
	TraceParent string `json:"trace_parent,omitempty"`
	SubmittedAt string `json:"submitted_at"`
}

func (ReconcileArgs) Kind() string { return NotificationReconcileKind }

type EventFanoutWorker struct {
	river.WorkerDefaults[EventFanoutArgs]
	service *Service
}

type ProjectionPendingWorker struct {
	river.WorkerDefaults[ProjectionPendingArgs]
	service *Service
}

type ReconcileWorker struct {
	river.WorkerDefaults[ReconcileArgs]
	service *Service
}

func NewRuntime(pool *pgxpool.Pool, svc *Service, logger *slog.Logger) (*Runtime, error) {
	if pool == nil {
		return nil, fmt.Errorf("notifications runtime requires pgx pool")
	}
	if svc == nil {
		return nil, fmt.Errorf("notifications runtime requires service")
	}
	if logger == nil {
		logger = slog.Default()
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, &EventFanoutWorker{service: svc})
	river.AddWorker(workers, &ProjectionPendingWorker{service: svc})
	river.AddWorker(workers, &ReconcileWorker{service: svc})
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger: logger,
		Middleware: []rivertype.Middleware{
			traceContextMiddleware(),
			otelriver.NewMiddleware(&otelriver.MiddlewareConfig{
				DurationUnit:                "ms",
				EnableSemanticMetrics:       true,
				EnableWorkSpanJobKindSuffix: true,
			}),
		},
		Queues: map[string]river.QueueConfig{
			QueueFanout:     {MaxWorkers: 4},
			QueueProjection: {MaxWorkers: 2},
			QueueReconcile:  {MaxWorkers: 1},
		},
		Workers: workers,
	})
	if err != nil {
		return nil, fmt.Errorf("create notifications river client: %w", err)
	}
	runtime := &Runtime{client: client, logger: logger}
	svc.SetRuntime(runtime)
	return runtime, nil
}

func (r *Runtime) Start(ctx context.Context) error {
	if err := r.client.Start(ctx); err != nil {
		return fmt.Errorf("start notifications river client: %w", err)
	}
	r.logger.InfoContext(ctx, "notifications river runtime started")
	return nil
}

func (r *Runtime) Stop(ctx context.Context) error {
	if err := r.client.Stop(ctx); err != nil {
		return fmt.Errorf("stop notifications river client: %w", err)
	}
	return nil
}

func (r *Runtime) EnqueueEventFanoutTx(ctx context.Context, tx pgx.Tx, eventSource string, eventID string, traceparent string) error {
	_, err := r.client.InsertTx(ctx, tx, EventFanoutArgs{
		EventSource: strings.TrimSpace(eventSource),
		EventID:     strings.TrimSpace(eventID),
		TraceParent: strings.TrimSpace(traceparent),
		SubmittedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}, &river.InsertOpts{
		MaxAttempts: 5,
		Queue:       QueueFanout,
		Tags:        []string{"fanout"},
		UniqueOpts:  river.UniqueOpts{ByArgs: true, ByQueue: true},
	})
	if err != nil {
		return fmt.Errorf("enqueue notification fanout: %w", err)
	}
	return nil
}

func (r *Runtime) EnqueueProjectionPendingTx(ctx context.Context, tx pgx.Tx, traceparent string) error {
	_, err := r.client.InsertTx(ctx, tx, ProjectionPendingArgs{
		Limit:       100,
		TraceParent: strings.TrimSpace(traceparent),
		SubmittedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}, scannerInsertOpts(QueueProjection, "projection", time.Second))
	if err != nil {
		return fmt.Errorf("enqueue notification projection: %w", err)
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
		{args: ProjectionPendingArgs{Limit: 100, SubmittedAt: time.Now().UTC().Format(time.RFC3339Nano)}, opts: scannerInsertOpts(QueueProjection, "projection", cadence)},
		{args: ReconcileArgs{Limit: 1000, SubmittedAt: time.Now().UTC().Format(time.RFC3339Nano)}, opts: scannerInsertOpts(QueueReconcile, "reconcile", cadence)},
	}
	for _, job := range jobs {
		if _, err := r.client.Insert(ctx, job.args, job.opts); err != nil {
			return fmt.Errorf("enqueue notifications maintenance %s: %w", job.args.Kind(), err)
		}
	}
	return nil
}

func scannerInsertOpts(queue string, tag string, cadence time.Duration) *river.InsertOpts {
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

func traceContextMiddleware() rivertype.Middleware {
	return river.WorkerMiddlewareFunc(func(ctx context.Context, job *rivertype.JobRow, doInner func(context.Context) error) error {
		var args struct {
			TraceParent string `json:"trace_parent"`
		}
		if err := json.Unmarshal(job.EncodedArgs, &args); err == nil {
			if traceparent := strings.TrimSpace(args.TraceParent); traceparent != "" {
				ctx = propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{"traceparent": traceparent})
			}
		}
		return doInner(ctx)
	})
}

func (w *EventFanoutWorker) Work(ctx context.Context, job *river.Job[EventFanoutArgs]) error {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("notification.event_source", job.Args.EventSource),
		attribute.String("notification.event_id", job.Args.EventID),
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", EventFanoutKind),
		attribute.String("river.queue", QueueFanout),
	)
	err := w.service.ProcessEvent(ctx, job.Args.EventSource, job.Args.EventID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

func (w *ProjectionPendingWorker) Work(ctx context.Context, job *river.Job[ProjectionPendingArgs]) error {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("notification.limit", job.Args.Limit),
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", ProjectionPendingKind),
		attribute.String("river.queue", QueueProjection),
	)
	err := w.service.ProjectPendingLedger(ctx, job.Args.Limit)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

func (w *ReconcileWorker) Work(ctx context.Context, job *river.Job[ReconcileArgs]) error {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("notification.limit", job.Args.Limit),
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", NotificationReconcileKind),
		attribute.String("river.queue", QueueReconcile),
	)
	err := w.service.Reconcile(ctx, job.Args.Limit)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}
