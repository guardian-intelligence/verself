package release

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
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
	QueuePublication = "release_publication"
	QueueReconcile   = "release_reconcile"

	PublicationDispatchKind = "release.publication.dispatch"
	ReleaseReconcileKind    = "release.reconcile"
)

type Runtime struct {
	client *river.Client[pgx.Tx]
	logger *slog.Logger
}

type PublicationDispatchArgs struct {
	PublicationID string `json:"publication_id" river:"unique"`
	TraceParent   string `json:"trace_parent,omitempty"`
	SubmittedAt   string `json:"submitted_at"`
}

func (PublicationDispatchArgs) Kind() string { return PublicationDispatchKind }

type ReconcileArgs struct {
	Limit       int    `json:"limit"`
	TraceParent string `json:"trace_parent,omitempty"`
	SubmittedAt string `json:"submitted_at"`
}

func (ReconcileArgs) Kind() string { return ReleaseReconcileKind }

type PublicationDispatchWorker struct {
	river.WorkerDefaults[PublicationDispatchArgs]
	service *Service
}

type ReconcileWorker struct {
	river.WorkerDefaults[ReconcileArgs]
	service *Service
}

func NewRuntime(pool *pgxpool.Pool, svc *Service, logger *slog.Logger) (*Runtime, error) {
	if pool == nil {
		return nil, fmt.Errorf("release runtime requires pgx pool")
	}
	if svc == nil {
		return nil, fmt.Errorf("release runtime requires service")
	}
	if logger == nil {
		logger = slog.Default()
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, &PublicationDispatchWorker{service: svc})
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
			QueuePublication: {MaxWorkers: 2},
			QueueReconcile:   {MaxWorkers: 1},
		},
		Workers: workers,
	})
	if err != nil {
		return nil, fmt.Errorf("create release river client: %w", err)
	}
	runtime := &Runtime{client: client, logger: logger}
	svc.SetRuntime(runtime)
	return runtime, nil
}

func (r *Runtime) Start(ctx context.Context) error {
	if err := r.client.Start(ctx); err != nil {
		return fmt.Errorf("start release river client: %w", err)
	}
	r.logger.InfoContext(ctx, "release river runtime started")
	return nil
}

func (r *Runtime) Stop(ctx context.Context) error {
	if err := r.client.Stop(ctx); err != nil {
		return fmt.Errorf("stop release river client: %w", err)
	}
	return nil
}

func (r *Runtime) EnqueuePublicationDispatch(ctx context.Context, publicationID string, traceparent string) error {
	_, err := r.client.Insert(ctx, PublicationDispatchArgs{
		PublicationID: strings.TrimSpace(publicationID),
		TraceParent:   strings.TrimSpace(traceparent),
		SubmittedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}, &river.InsertOpts{
		MaxAttempts: 5,
		Queue:       QueuePublication,
		Tags:        []string{"publication"},
		UniqueOpts: river.UniqueOpts{
			ByArgs:  true,
			ByQueue: true,
			ByState: []rivertype.JobState{
				rivertype.JobStateAvailable,
				rivertype.JobStatePending,
				rivertype.JobStateRetryable,
				rivertype.JobStateRunning,
				rivertype.JobStateScheduled,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("enqueue release publication dispatch: %w", err)
	}
	return nil
}

func (r *Runtime) EnqueueMaintenance(ctx context.Context, cadence time.Duration) error {
	if cadence < time.Second {
		cadence = time.Second
	}
	_, err := r.client.Insert(ctx, ReconcileArgs{
		Limit:       100,
		SubmittedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}, &river.InsertOpts{
		MaxAttempts: 3,
		Queue:       QueueReconcile,
		Tags:        []string{"reconcile"},
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByQueue:  true,
			ByPeriod: cadence,
		},
	})
	if err != nil {
		return fmt.Errorf("enqueue release maintenance: %w", err)
	}
	return nil
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

func (w *PublicationDispatchWorker) Work(ctx context.Context, job *river.Job[PublicationDispatchArgs]) error {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("release.publication_id", job.Args.PublicationID),
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", PublicationDispatchKind),
		attribute.String("river.queue", QueuePublication),
	)
	publicationID, err := uuid.Parse(strings.TrimSpace(job.Args.PublicationID))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	_, err = w.service.DispatchPublication(ctx, Principal{Actor: "release-worker"}, publicationID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

func (w *ReconcileWorker) Work(ctx context.Context, job *river.Job[ReconcileArgs]) error {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int("release.limit", job.Args.Limit),
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", ReleaseReconcileKind),
		attribute.String("river.queue", QueueReconcile),
	)
	err := w.service.Reconcile(ctx, job.Args.Limit)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}
