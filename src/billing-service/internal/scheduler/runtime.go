package scheduler

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
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

const (
	QueueMetering  = "metering"
	QueueOutbox    = "outbox"
	QueueReconcile = "reconcile"

	KindMeteringProjectPending  = "billing.metering.project_pending_windows"
	KindOutboxProjectPending    = "billing.outbox.project_pending_events"
	KindEntitlementsReconcile   = "billing.entitlements.reconcile"
	periodicMeteringProjectorID = "billing-metering-projector"
	periodicOutboxProjectorID   = "billing-outbox-projector"
	periodicEntitlementsID      = "billing-entitlements-reconcile"

	defaultProjectLimit    = 100
	defaultReconcileLimit  = 10000
	defaultProjectEvery    = time.Second
	defaultReconcileEvery  = time.Hour
	minimumPeriodicJobStep = time.Second
)

var tracer = otel.Tracer("billing-service/scheduler")

type Config struct {
	Logger         *slog.Logger
	Client         BillingWorkClient
	ProjectEvery   time.Duration
	ReconcileEvery time.Duration
	ProjectLimit   int
	ReconcileLimit int
}

type Runtime struct {
	client *river.Client[pgx.Tx]
	logger *slog.Logger
}

type JobResult struct {
	JobID  int64
	Kind   string
	Queue  string
	Status string
}

type enqueueableArgs interface {
	river.JobArgs
	river.JobArgsWithInsertOpts
}

type submittedAtArgs struct {
	TraceParent string `json:"trace_parent,omitempty"`
	SubmittedAt string `json:"submitted_at"`
}

func newSubmittedAt() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func traceContextMiddleware() rivertype.Middleware {
	return river.WorkerMiddlewareFunc(func(ctx context.Context, job *rivertype.JobRow, doInner func(context.Context) error) error {
		var args submittedAtArgs
		if err := json.Unmarshal(job.EncodedArgs, &args); err == nil {
			if traceParent := strings.TrimSpace(args.TraceParent); traceParent != "" {
				ctx = propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{
					"traceparent": traceParent,
				})
			}
		}
		return doInner(ctx)
	})
}

func queueConfig() map[string]river.QueueConfig {
	return map[string]river.QueueConfig{
		QueueMetering:  {MaxWorkers: 2},
		QueueOutbox:    {MaxWorkers: 1},
		QueueReconcile: {MaxWorkers: 1},
	}
}

func queueNames() []string {
	return []string{
		QueueMetering,
		QueueOutbox,
		QueueReconcile,
	}
}

func normalizeConfig(cfg Config) (Config, error) {
	if cfg.Client == nil {
		return Config{}, fmt.Errorf("scheduler runtime requires billing work client")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.ProjectEvery == 0 {
		cfg.ProjectEvery = defaultProjectEvery
	}
	if cfg.ReconcileEvery == 0 {
		cfg.ReconcileEvery = defaultReconcileEvery
	}
	if cfg.ProjectEvery < minimumPeriodicJobStep {
		return Config{}, fmt.Errorf("scheduler project interval must be >= %s, got %s", minimumPeriodicJobStep, cfg.ProjectEvery)
	}
	if cfg.ReconcileEvery < minimumPeriodicJobStep {
		return Config{}, fmt.Errorf("scheduler reconcile interval must be >= %s, got %s", minimumPeriodicJobStep, cfg.ReconcileEvery)
	}
	cfg.ProjectLimit = normalizedLimit(cfg.ProjectLimit, defaultProjectLimit)
	cfg.ReconcileLimit = normalizedLimit(cfg.ReconcileLimit, defaultReconcileLimit)
	return cfg, nil
}

func periodicJobs(cfg Config) []*river.PeriodicJob {
	return []*river.PeriodicJob{
		river.NewPeriodicJob(
			river.PeriodicInterval(cfg.ProjectEvery),
			func() (river.JobArgs, *river.InsertOpts) {
				return MeteringProjectPendingArgs{Limit: cfg.ProjectLimit, SubmittedAt: newSubmittedAt()}, nil
			},
			&river.PeriodicJobOpts{ID: periodicMeteringProjectorID, RunOnStart: true},
		),
		river.NewPeriodicJob(
			river.PeriodicInterval(cfg.ProjectEvery),
			func() (river.JobArgs, *river.InsertOpts) {
				return OutboxProjectPendingArgs{Limit: cfg.ProjectLimit, SubmittedAt: newSubmittedAt()}, nil
			},
			&river.PeriodicJobOpts{ID: periodicOutboxProjectorID, RunOnStart: true},
		),
		river.NewPeriodicJob(
			river.PeriodicInterval(cfg.ReconcileEvery),
			func() (river.JobArgs, *river.InsertOpts) {
				return EntitlementsReconcileArgs{Limit: cfg.ReconcileLimit, SubmittedAt: newSubmittedAt()}, nil
			},
			&river.PeriodicJobOpts{ID: periodicEntitlementsID, RunOnStart: true},
		),
	}
}

func NewRuntime(pool *pgxpool.Pool, cfg Config) (*Runtime, error) {
	if pool == nil {
		return nil, fmt.Errorf("scheduler runtime requires pgx pool")
	}
	normalized, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}

	workers := river.NewWorkers()
	if err := registerWorkers(workers, normalized.Logger, normalized.Client); err != nil {
		return nil, err
	}

	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger: normalized.Logger,
		Middleware: []rivertype.Middleware{
			traceContextMiddleware(),
			otelriver.NewMiddleware(&otelriver.MiddlewareConfig{
				DurationUnit:                "ms",
				EnableSemanticMetrics:       true,
				EnableWorkSpanJobKindSuffix: true,
			}),
		},
		PeriodicJobs: periodicJobs(normalized),
		Queues:       queueConfig(),
		Workers:      workers,
	})
	if err != nil {
		return nil, fmt.Errorf("create river client: %w", err)
	}

	return &Runtime{client: client, logger: normalized.Logger}, nil
}

func (r *Runtime) Start(ctx context.Context) error {
	if err := r.client.Start(ctx); err != nil {
		return fmt.Errorf("start river client: %w", err)
	}
	r.logger.InfoContext(ctx, "billing scheduler runtime started", "queues", queueNames())
	return nil
}

func (r *Runtime) Stop(ctx context.Context) error {
	if err := r.client.Stop(ctx); err != nil {
		return fmt.Errorf("stop river client: %w", err)
	}
	r.logger.InfoContext(ctx, "billing scheduler runtime stopped")
	return nil
}

func enqueueTx[T enqueueableArgs](ctx context.Context, client *river.Client[pgx.Tx], tx pgx.Tx, args T) (JobResult, error) {
	result, err := client.InsertTx(ctx, tx, args, nil)
	if err != nil {
		return JobResult{}, fmt.Errorf("enqueue %s: %w", args.Kind(), err)
	}
	job := result.Job
	return JobResult{
		JobID:  job.ID,
		Kind:   job.Kind,
		Queue:  job.Queue,
		Status: string(job.State),
	}, nil
}
