// Package scheduler owns sandbox-rental-service queue and scheduling runtime.
// vm-orchestrator remains the VM execution boundary; River only queues durable
// control-plane work that may later call vm-orchestrator.
package scheduler

import (
	"context"
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
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const (
	QueueExecution    = "execution"
	QueueOrchestrator = "orchestrator"
	QueueRunner       = "runner"
	QueueScheduler    = "scheduler"
	QueueReconcile    = "reconcile"
	QueueWebhook      = "webhook"

	ProbeKind = "scheduler.probe"
)

var tracer = otel.Tracer("sandbox-rental-service/scheduler")

type Config struct {
	Logger *slog.Logger
}

type Runtime struct {
	client *river.Client[pgx.Tx]
	logger *slog.Logger
}

type ProbeRequest struct {
	Message           string
	OrgID             uint64
	ActorID           string
	VerificationRunID string
	CorrelationID     string
}

type ProbeResult struct {
	JobID  int64
	Kind   string
	Queue  string
	Status string
}

type ProbeArgs struct {
	Message           string `json:"message,omitempty"`
	OrgID             uint64 `json:"org_id,omitempty"`
	ActorID           string `json:"actor_id,omitempty"`
	VerificationRunID string `json:"verification_run_id,omitempty"`
	CorrelationID     string `json:"correlation_id,omitempty"`
	SubmittedAt       string `json:"submitted_at"`
}

func (ProbeArgs) Kind() string { return ProbeKind }

func (ProbeArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 3,
		Queue:       QueueScheduler,
		Tags:        []string{"scheduler-probe"},
	}
}

type ProbeWorker struct {
	river.WorkerDefaults[ProbeArgs]
	logger *slog.Logger
}

func (w *ProbeWorker) Work(ctx context.Context, job *river.Job[ProbeArgs]) error {
	ctx, span := tracer.Start(ctx, "sandbox-rental.scheduler.probe.complete")
	defer span.End()

	span.SetAttributes(
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", ProbeKind),
		attribute.String("river.queue", QueueScheduler),
		attribute.String("verification.run_id", job.Args.VerificationRunID),
		attribute.String("fm.correlation_id", job.Args.CorrelationID),
	)
	w.logger.InfoContext(ctx, "scheduler probe completed",
		"river_job_id", job.ID,
		"river_job_kind", ProbeKind,
		"river_queue", QueueScheduler,
		"verification_run_id", job.Args.VerificationRunID,
		"fm_correlation_id", job.Args.CorrelationID,
	)
	return nil
}

func NewRuntime(pool *pgxpool.Pool, cfg Config) (*Runtime, error) {
	if pool == nil {
		return nil, fmt.Errorf("scheduler runtime requires pgx pool")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	workers := river.NewWorkers()
	river.AddWorker(workers, &ProbeWorker{logger: logger})

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
		return nil, fmt.Errorf("create river client: %w", err)
	}

	return &Runtime{client: client, logger: logger}, nil
}

func (r *Runtime) Start(ctx context.Context) error {
	if err := r.client.Start(ctx); err != nil {
		return fmt.Errorf("start river client: %w", err)
	}
	r.logger.InfoContext(ctx, "scheduler runtime started", "queues", queueNames())
	return nil
}

func (r *Runtime) Stop(ctx context.Context) error {
	if err := r.client.Stop(ctx); err != nil {
		return fmt.Errorf("stop river client: %w", err)
	}
	r.logger.InfoContext(ctx, "scheduler runtime stopped")
	return nil
}

func (r *Runtime) EnqueueProbe(ctx context.Context, req ProbeRequest) (ProbeResult, error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.scheduler.probe.submit")
	defer span.End()

	args := ProbeArgs{
		Message:           strings.TrimSpace(req.Message),
		OrgID:             req.OrgID,
		ActorID:           strings.TrimSpace(req.ActorID),
		VerificationRunID: strings.TrimSpace(req.VerificationRunID),
		CorrelationID:     strings.TrimSpace(req.CorrelationID),
		SubmittedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
	result, err := r.client.Insert(ctx, args, nil)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return ProbeResult{}, fmt.Errorf("enqueue scheduler probe: %w", err)
	}

	job := result.Job
	span.SetAttributes(
		attribute.Int64("river.job_id", job.ID),
		attribute.String("river.job_kind", job.Kind),
		attribute.String("river.queue", job.Queue),
		attribute.String("verification.run_id", args.VerificationRunID),
		attribute.String("fm.correlation_id", args.CorrelationID),
	)
	r.logger.InfoContext(ctx, "scheduler probe enqueued",
		"river_job_id", job.ID,
		"river_job_kind", job.Kind,
		"river_queue", job.Queue,
		"verification_run_id", args.VerificationRunID,
		"fm_correlation_id", args.CorrelationID,
	)
	return ProbeResult{
		JobID:  job.ID,
		Kind:   job.Kind,
		Queue:  job.Queue,
		Status: string(job.State),
	}, nil
}

func queueConfig() map[string]river.QueueConfig {
	return map[string]river.QueueConfig{
		QueueExecution:    {MaxWorkers: 4},
		QueueOrchestrator: {MaxWorkers: 2},
		QueueRunner:       {MaxWorkers: 4},
		QueueScheduler:    {MaxWorkers: 1},
		QueueReconcile:    {MaxWorkers: 1},
		QueueWebhook:      {MaxWorkers: 2},
	}
}

func queueNames() []string {
	return []string{
		QueueExecution,
		QueueOrchestrator,
		QueueRunner,
		QueueScheduler,
		QueueReconcile,
		QueueWebhook,
	}
}
