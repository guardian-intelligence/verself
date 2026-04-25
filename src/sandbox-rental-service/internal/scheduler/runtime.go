// Package scheduler owns sandbox-rental-service queue and scheduling runtime.
// vm-orchestrator remains the VM execution boundary; River only queues durable
// control-plane work that may later call vm-orchestrator.
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
	QueueExecution    = "execution"
	QueueOrchestrator = "orchestrator"
	QueueRunner       = "runner"
	QueueReconcile    = "reconcile"
	QueueWebhook      = "webhook"

	ExecutionAdvanceKind        = "execution.advance"
	RunnerCapacityReconcileKind = "runner.capacity.reconcile"
	RunnerAllocateKind          = "runner.allocate"
	RunnerJobBindKind           = "runner.job.bind"
	RunnerCleanupKind           = "runner.cleanup"
	RunnerRepositorySyncKind    = "runner.repository.sync"

	DefaultExecutionMaxWorkers = 4
)

var tracer = otel.Tracer("sandbox-rental-service/scheduler")

type Config struct {
	Logger              *slog.Logger
	RegisterWorkers     func(*river.Workers) error
	ExecutionMaxWorkers int
}

type Runtime struct {
	client *river.Client[pgx.Tx]
	logger *slog.Logger
}

type ProbeResult struct {
	JobID  int64
	Kind   string
	Queue  string
	Status string
}

type ExecutionAdvanceRequest struct {
	ExecutionID   string
	AttemptID     string
	OrgID         uint64
	ActorID       string
	CorrelationID string
	TraceParent   string
}

type ExecutionAdvanceResult struct {
	JobID  int64
	Kind   string
	Queue  string
	Status string
}

type RunnerCapacityReconcileRequest struct {
	Provider               string
	ProviderInstallationID int64
	ProviderRepositoryID   int64
	ProviderJobID          int64
	CorrelationID          string
	TraceParent            string
}

type RunnerAllocateRequest struct {
	AllocationID  string
	CorrelationID string
	TraceParent   string
}

type RunnerJobBindRequest struct {
	Provider      string
	ProviderJobID int64
	CorrelationID string
	TraceParent   string
}

type RunnerCleanupRequest struct {
	AllocationID  string
	CorrelationID string
	TraceParent   string
}

type RunnerRepositorySyncRequest struct {
	Provider             string
	ProviderRepositoryID int64
	CorrelationID        string
	TraceParent          string
}

type ExecutionAdvanceArgs struct {
	ExecutionID   string `json:"execution_id"`
	AttemptID     string `json:"attempt_id"`
	OrgID         uint64 `json:"org_id,omitempty"`
	ActorID       string `json:"actor_id,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
	TraceParent   string `json:"trace_parent,omitempty"`
	SubmittedAt   string `json:"submitted_at"`
}

func (ExecutionAdvanceArgs) Kind() string { return ExecutionAdvanceKind }

func (ExecutionAdvanceArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 3,
		Queue:       QueueExecution,
		Tags:        []string{"execution"},
	}
}

type RunnerCapacityReconcileArgs struct {
	Provider               string `json:"provider"`
	ProviderInstallationID int64  `json:"provider_installation_id,omitempty"`
	ProviderRepositoryID   int64  `json:"provider_repository_id,omitempty"`
	ProviderJobID          int64  `json:"provider_job_id,omitempty"`
	CorrelationID          string `json:"correlation_id,omitempty"`
	TraceParent            string `json:"trace_parent,omitempty"`
	SubmittedAt            string `json:"submitted_at"`
}

func (RunnerCapacityReconcileArgs) Kind() string { return RunnerCapacityReconcileKind }

func (RunnerCapacityReconcileArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 5,
		Queue:       QueueRunner,
		Tags:        []string{"runner", "capacity"},
	}
}

type RunnerAllocateArgs struct {
	AllocationID  string `json:"allocation_id"`
	CorrelationID string `json:"correlation_id,omitempty"`
	TraceParent   string `json:"trace_parent,omitempty"`
	SubmittedAt   string `json:"submitted_at"`
}

func (RunnerAllocateArgs) Kind() string { return RunnerAllocateKind }

func (RunnerAllocateArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 3,
		Queue:       QueueRunner,
		Tags:        []string{"runner", "allocate"},
	}
}

type RunnerJobBindArgs struct {
	Provider      string `json:"provider"`
	ProviderJobID int64  `json:"provider_job_id"`
	CorrelationID string `json:"correlation_id,omitempty"`
	TraceParent   string `json:"trace_parent,omitempty"`
	SubmittedAt   string `json:"submitted_at"`
}

func (RunnerJobBindArgs) Kind() string { return RunnerJobBindKind }

func (RunnerJobBindArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 5,
		Queue:       QueueRunner,
		Tags:        []string{"runner", "job", "bind"},
	}
}

type RunnerCleanupArgs struct {
	AllocationID  string `json:"allocation_id"`
	CorrelationID string `json:"correlation_id,omitempty"`
	TraceParent   string `json:"trace_parent,omitempty"`
	SubmittedAt   string `json:"submitted_at"`
}

func (RunnerCleanupArgs) Kind() string { return RunnerCleanupKind }

func (RunnerCleanupArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 5,
		Queue:       QueueRunner,
		Tags:        []string{"runner", "cleanup"},
	}
}

type RunnerRepositorySyncArgs struct {
	Provider             string `json:"provider"`
	ProviderRepositoryID int64  `json:"provider_repository_id"`
	CorrelationID        string `json:"correlation_id,omitempty"`
	TraceParent          string `json:"trace_parent,omitempty"`
	SubmittedAt          string `json:"submitted_at"`
}

func (RunnerRepositorySyncArgs) Kind() string { return RunnerRepositorySyncKind }

func (RunnerRepositorySyncArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 5,
		Queue:       QueueRunner,
		Tags:        []string{"runner", "repository", "sync"},
	}
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
	if cfg.RegisterWorkers != nil {
		if err := cfg.RegisterWorkers(workers); err != nil {
			return nil, fmt.Errorf("register scheduler workers: %w", err)
		}
	}

	queues := queueConfig(cfg)
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger: logger,
		Middleware: []rivertype.Middleware{
			executionTraceContextMiddleware(),
			otelriver.NewMiddleware(&otelriver.MiddlewareConfig{
				DurationUnit:                "ms",
				EnableSemanticMetrics:       true,
				EnableWorkSpanJobKindSuffix: true,
			}),
		},
		Queues:  queues,
		Workers: workers,
	})
	if err != nil {
		return nil, fmt.Errorf("create river client: %w", err)
	}

	return &Runtime{client: client, logger: logger}, nil
}

func executionTraceContextMiddleware() rivertype.Middleware {
	return river.WorkerMiddlewareFunc(func(ctx context.Context, job *rivertype.JobRow, doInner func(context.Context) error) error {
		var args struct {
			TraceParent string `json:"trace_parent"`
		}
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

func (r *Runtime) EnqueueExecutionAdvanceTx(ctx context.Context, tx pgx.Tx, req ExecutionAdvanceRequest) (ExecutionAdvanceResult, error) {
	args := ExecutionAdvanceArgs{
		ExecutionID:   strings.TrimSpace(req.ExecutionID),
		AttemptID:     strings.TrimSpace(req.AttemptID),
		OrgID:         req.OrgID,
		ActorID:       strings.TrimSpace(req.ActorID),
		CorrelationID: strings.TrimSpace(req.CorrelationID),
		TraceParent:   strings.TrimSpace(req.TraceParent),
		SubmittedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}
	result, err := r.client.InsertTx(ctx, tx, args, nil)
	if err != nil {
		return ExecutionAdvanceResult{}, fmt.Errorf("enqueue execution advance: %w", err)
	}
	job := result.Job
	return ExecutionAdvanceResult{
		JobID:  job.ID,
		Kind:   job.Kind,
		Queue:  job.Queue,
		Status: string(job.State),
	}, nil
}

func (r *Runtime) EnqueueRunnerCapacityReconcileTx(ctx context.Context, tx pgx.Tx, req RunnerCapacityReconcileRequest) (ProbeResult, error) {
	args := RunnerCapacityReconcileArgs{
		Provider:               strings.TrimSpace(req.Provider),
		ProviderInstallationID: req.ProviderInstallationID,
		ProviderRepositoryID:   req.ProviderRepositoryID,
		ProviderJobID:          req.ProviderJobID,
		CorrelationID:          strings.TrimSpace(req.CorrelationID),
		TraceParent:            strings.TrimSpace(req.TraceParent),
		SubmittedAt:            time.Now().UTC().Format(time.RFC3339Nano),
	}
	result, err := r.client.InsertTx(ctx, tx, args, nil)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("enqueue runner capacity reconcile: %w", err)
	}
	job := result.Job
	return ProbeResult{JobID: job.ID, Kind: job.Kind, Queue: job.Queue, Status: string(job.State)}, nil
}

func (r *Runtime) EnqueueRunnerAllocateTx(ctx context.Context, tx pgx.Tx, req RunnerAllocateRequest) (ProbeResult, error) {
	args := RunnerAllocateArgs{
		AllocationID:  strings.TrimSpace(req.AllocationID),
		CorrelationID: strings.TrimSpace(req.CorrelationID),
		TraceParent:   strings.TrimSpace(req.TraceParent),
		SubmittedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}
	result, err := r.client.InsertTx(ctx, tx, args, nil)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("enqueue runner allocate: %w", err)
	}
	job := result.Job
	return ProbeResult{JobID: job.ID, Kind: job.Kind, Queue: job.Queue, Status: string(job.State)}, nil
}

func (r *Runtime) EnqueueRunnerJobBindTx(ctx context.Context, tx pgx.Tx, req RunnerJobBindRequest) (ProbeResult, error) {
	args := RunnerJobBindArgs{
		Provider:      strings.TrimSpace(req.Provider),
		ProviderJobID: req.ProviderJobID,
		CorrelationID: strings.TrimSpace(req.CorrelationID),
		TraceParent:   strings.TrimSpace(req.TraceParent),
		SubmittedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}
	result, err := r.client.InsertTx(ctx, tx, args, nil)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("enqueue runner job bind: %w", err)
	}
	job := result.Job
	return ProbeResult{JobID: job.ID, Kind: job.Kind, Queue: job.Queue, Status: string(job.State)}, nil
}

func (r *Runtime) EnqueueRunnerCleanup(ctx context.Context, req RunnerCleanupRequest) (ProbeResult, error) {
	args := RunnerCleanupArgs{
		AllocationID:  strings.TrimSpace(req.AllocationID),
		CorrelationID: strings.TrimSpace(req.CorrelationID),
		TraceParent:   strings.TrimSpace(req.TraceParent),
		SubmittedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}
	result, err := r.client.Insert(ctx, args, nil)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("enqueue runner cleanup: %w", err)
	}
	job := result.Job
	return ProbeResult{JobID: job.ID, Kind: job.Kind, Queue: job.Queue, Status: string(job.State)}, nil
}

func (r *Runtime) EnqueueRunnerRepositorySyncTx(ctx context.Context, tx pgx.Tx, req RunnerRepositorySyncRequest) (ProbeResult, error) {
	args := RunnerRepositorySyncArgs{
		Provider:             strings.TrimSpace(req.Provider),
		ProviderRepositoryID: req.ProviderRepositoryID,
		CorrelationID:        strings.TrimSpace(req.CorrelationID),
		TraceParent:          strings.TrimSpace(req.TraceParent),
		SubmittedAt:          time.Now().UTC().Format(time.RFC3339Nano),
	}
	result, err := r.client.InsertTx(ctx, tx, args, nil)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("enqueue runner repository sync: %w", err)
	}
	job := result.Job
	return ProbeResult{JobID: job.ID, Kind: job.Kind, Queue: job.Queue, Status: string(job.State)}, nil
}

func (r *Runtime) EnqueueRunnerRepositorySync(ctx context.Context, req RunnerRepositorySyncRequest) (ProbeResult, error) {
	args := RunnerRepositorySyncArgs{
		Provider:             strings.TrimSpace(req.Provider),
		ProviderRepositoryID: req.ProviderRepositoryID,
		CorrelationID:        strings.TrimSpace(req.CorrelationID),
		TraceParent:          strings.TrimSpace(req.TraceParent),
		SubmittedAt:          time.Now().UTC().Format(time.RFC3339Nano),
	}
	result, err := r.client.Insert(ctx, args, nil)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("enqueue runner repository sync: %w", err)
	}
	job := result.Job
	return ProbeResult{JobID: job.ID, Kind: job.Kind, Queue: job.Queue, Status: string(job.State)}, nil
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

func queueConfig(cfg Config) map[string]river.QueueConfig {
	return map[string]river.QueueConfig{
		QueueExecution:    {MaxWorkers: normalizeMaxWorkers(cfg.ExecutionMaxWorkers, DefaultExecutionMaxWorkers)},
		QueueOrchestrator: {MaxWorkers: 2},
		QueueRunner:       {MaxWorkers: 4},
		QueueReconcile:    {MaxWorkers: 1},
		QueueWebhook:      {MaxWorkers: 2},
	}
}

func normalizeMaxWorkers(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func queueNames() []string {
	return []string{
		QueueExecution,
		QueueOrchestrator,
		QueueRunner,
		QueueReconcile,
		QueueWebhook,
	}
}
