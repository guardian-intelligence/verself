package jobs

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/forge-metal/apiwire"
	billingclient "github.com/forge-metal/billing-service/client"
	vmorchestrator "github.com/forge-metal/vm-orchestrator"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/forge-metal/sandbox-rental-service/internal/scheduler"
)

const (
	KindDirect               = "direct"
	KindVMSession            = "vm_session"
	SourceKindAPI            = "api"
	SourceKindGitHubAction   = "github_actions"
	SourceKindCanary         = "canary"
	SourceKindVMSession      = "vm_session"
	WorkloadKindDirect       = "direct"
	WorkloadKindGitHubRunner = "github_runner"

	DefaultRunnerClassLabel = "metal-4vcpu-ubuntu-2404"
	defaultProductID        = "sandbox"
	defaultRunCommand       = "echo hello"

	billingSKUComputeVCPUMs     = "sandbox_compute_amd_epyc_4484px_vcpu_ms"
	billingSKUMemoryGiBMs       = "sandbox_memory_standard_gib_ms"
	billingSKUBlockStorageGiBMs = "sandbox_block_storage_premium_nvme_gib_ms"
	billingMiBPerGiB            = 1024
	billingBytesPerGiB          = 1024 * 1024 * 1024

	StateQueued     = "queued"
	StateReserved   = "reserved"
	StateLaunching  = "launching"
	StateRunning    = "running"
	StateFinalizing = "finalizing"
	StateSucceeded  = "succeeded"
	StateFailed     = "failed"
	StateCanceled   = "canceled"
	StateLost       = "lost"
)

var (
	ErrQuotaExceeded      = errors.New("sandbox-rental: quota exceeded")
	ErrExecutionMissing   = errors.New("sandbox-rental: execution missing")
	ErrRunnerUnavailable  = errors.New("sandbox-rental: runner unavailable")
	ErrRunnerClassMissing = errors.New("sandbox-rental: runner class missing")
)

var tracer = otel.Tracer("sandbox-rental-service/jobs")

type Runner interface {
	GetCapacity(ctx context.Context) (vmorchestrator.Capacity, error)
	AcquireLease(ctx context.Context, key string, spec vmorchestrator.LeaseSpec) (vmorchestrator.LeaseRecord, error)
	RenewLease(ctx context.Context, leaseID, key string, extendSeconds uint64, allowlist []string) (time.Time, error)
	ReleaseLease(ctx context.Context, leaseID, key string) error
	StartExec(ctx context.Context, leaseID, key string, spec vmorchestrator.ExecSpec) (vmorchestrator.ExecRecord, error)
	WaitExec(ctx context.Context, leaseID, execID string, includeOutput bool) (vmorchestrator.ExecRecord, error)
	CancelExec(ctx context.Context, leaseID, execID, key, reason string) (bool, error)
	CommitFilesystemMount(ctx context.Context, leaseID, key, mountName, targetSourceRef string) (vmorchestrator.FilesystemCommitRecord, error)
}

type SchedulerRuntime interface {
	EnqueueExecutionAdvanceTx(ctx context.Context, tx pgx.Tx, req scheduler.ExecutionAdvanceRequest) (scheduler.ExecutionAdvanceResult, error)
	EnqueueGitHubCapacityReconcileTx(ctx context.Context, tx pgx.Tx, req scheduler.GitHubCapacityReconcileRequest) (scheduler.ProbeResult, error)
	EnqueueGitHubRunnerAllocateTx(ctx context.Context, tx pgx.Tx, req scheduler.GitHubRunnerAllocateRequest) (scheduler.ProbeResult, error)
	EnqueueGitHubJobBindTx(ctx context.Context, tx pgx.Tx, req scheduler.GitHubJobBindRequest) (scheduler.ProbeResult, error)
	EnqueueGitHubRunnerCleanup(ctx context.Context, req scheduler.GitHubRunnerCleanupRequest) (scheduler.ProbeResult, error)
	EnqueueProbe(ctx context.Context, req scheduler.ProbeRequest) (scheduler.ProbeResult, error)
}

type SubmitRequest struct {
	Kind               string                           `json:"kind,omitempty"`
	RunnerClass        string                           `json:"runner_class,omitempty"`
	ProductID          string                           `json:"product_id,omitempty"`
	Provider           string                           `json:"provider,omitempty"`
	IdempotencyKey     string                           `json:"idempotency_key"`
	SourceKind         string                           `json:"source_kind,omitempty"`
	WorkloadKind       string                           `json:"workload_kind,omitempty"`
	SourceRef          string                           `json:"source_ref,omitempty"`
	ExternalProvider   string                           `json:"external_provider,omitempty"`
	ExternalTaskID     string                           `json:"external_task_id,omitempty"`
	RunCommand         string                           `json:"run_command,omitempty"`
	MaxWallSeconds     uint64                           `json:"max_wall_seconds,omitempty"`
	Resources          apiwire.VMResources              `json:"resources"`
	FilesystemMounts   []vmorchestrator.FilesystemMount `json:"-"`
	StickyDiskMounts   []StickyDiskMountSpec            `json:"-"`
	AttemptID          uuid.UUID                        `json:"-"`
	GitHubAllocationID uuid.UUID                        `json:"-"`
	GitHubJITConfig    string                           `json:"-"`
}

type ExecutionRecord struct {
	ExecutionID      uuid.UUID
	OrgID            uint64
	ActorID          string
	Kind             string
	SourceKind       string
	WorkloadKind     string
	SourceRef        string
	RunnerClass      string
	ExternalProvider string
	ExternalTaskID   string
	Provider         string
	ProductID        string
	Status           string
	CorrelationID    string
	IdempotencyKey   string
	RunCommand       string
	LatestAttempt    AttemptRecord
	CreatedAt        time.Time
	UpdatedAt        time.Time
	BillingWindows   []BillingWindow
}

type AttemptRecord struct {
	AttemptID     uuid.UUID
	AttemptSeq    int
	State         string
	LeaseID       string
	ExecID        string
	BillingJobID  int64
	FailureReason string
	ExitCode      int
	DurationMs    int64
	ZFSWritten    int64
	StdoutBytes   int64
	StderrBytes   int64
	TraceID       string
	StartedAt     *time.Time
	CompletedAt   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type BillingWindow struct {
	AttemptID        uuid.UUID
	BillingWindowID  string
	WindowSeq        int
	ReservationShape string
	ReservedQuantity int
	ActualQuantity   int
	PricingPhase     string
	State            string
	WindowStart      time.Time
	CreatedAt        time.Time
	SettledAt        *time.Time
}

type Service struct {
	PG               *sql.DB
	PGX              *pgxpool.Pool
	CH               driver.Conn
	CHDatabase       string
	Orchestrator     Runner
	Billing          *billingclient.ServiceClient
	Bounds           apiwire.VMResourceBounds
	GitHubRunner     *GitHubRunner
	Scheduler        SchedulerRuntime
	Logger           *slog.Logger
	WorkloadTimeout  time.Duration
	CheckoutCacheDir string
}

type executionWorkItem struct {
	ExecutionID      uuid.UUID
	AttemptID        uuid.UUID
	OrgID            uint64
	ActorID          string
	Kind             string
	SourceKind       string
	WorkloadKind     string
	SourceRef        string
	RunnerClass      string
	ExternalProvider string
	ExternalTaskID   string
	Provider         string
	ProductID        string
	RunCommand       string
	MaxWallSeconds   uint64
	LeaseID          string
	ExecID           string
	CorrelationID    string
	Resources        apiwire.VMResources
	FilesystemMounts []vmorchestrator.FilesystemMount
}

type jobEventRow struct {
	ExecutionID      uuid.UUID `ch:"execution_id"`
	AttemptID        uuid.UUID `ch:"attempt_id"`
	OrgID            uint64    `ch:"org_id"`
	ActorID          string    `ch:"actor_id"`
	Kind             string    `ch:"kind"`
	SourceKind       string    `ch:"source_kind"`
	WorkloadKind     string    `ch:"workload_kind"`
	RunnerClass      string    `ch:"runner_class"`
	ExternalProvider string    `ch:"external_provider"`
	ExternalTaskID   string    `ch:"external_task_id"`
	Provider         string    `ch:"provider"`
	ProductID        string    `ch:"product_id"`
	RunCommand       string    `ch:"run_command"`
	Status           string    `ch:"status"`
	ExitCode         int32     `ch:"exit_code"`
	DurationMs       int64     `ch:"duration_ms"`
	ZFSWritten       uint64    `ch:"zfs_written"`
	StdoutBytes      uint64    `ch:"stdout_bytes"`
	StderrBytes      uint64    `ch:"stderr_bytes"`
	BillingJobID     int64     `ch:"billing_job_id"`
	ChargeUnits      uint64    `ch:"charge_units"`
	PricingPhase     string    `ch:"pricing_phase"`
	CorrelationID    string    `ch:"correlation_id"`
	StartedAt        time.Time `ch:"started_at"`
	CompletedAt      time.Time `ch:"completed_at"`
	CreatedAt        time.Time `ch:"created_at"`
	TraceID          string    `ch:"trace_id"`
}

type jobLogRow struct {
	ExecutionID uuid.UUID `ch:"execution_id"`
	AttemptID   uuid.UUID `ch:"attempt_id"`
	Seq         uint32    `ch:"seq"`
	Stream      string    `ch:"stream"`
	Chunk       string    `ch:"chunk"`
	CreatedAt   time.Time `ch:"created_at"`
}

func (s *Service) Submit(ctx context.Context, orgID uint64, actorID string, req SubmitRequest) (executionID uuid.UUID, attemptID uuid.UUID, err error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.execution.submit")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	req, err = s.normalizeSubmitRequest(req)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if s.PGX == nil || s.Orchestrator == nil || s.Billing == nil {
		return uuid.Nil, uuid.Nil, ErrRunnerUnavailable
	}
	correlationID := CorrelationIDFromContext(ctx)
	executionID = uuid.New()
	attemptID = req.AttemptID
	if attemptID == uuid.Nil {
		attemptID = uuid.New()
	}
	now := time.Now().UTC()
	tx, err := s.PGX.Begin(ctx)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("begin submit tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row := tx.QueryRow(ctx, `INSERT INTO executions (
		execution_id, org_id, actor_id, kind, source_kind, workload_kind, source_ref,
		runner_class, external_provider, external_task_id, provider, product_id,
		state, correlation_id, idempotency_key, run_command, max_wall_seconds,
		requested_vcpus, requested_memory_mib, requested_root_disk_gib, requested_kernel_image,
		created_at, updated_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$22)
	ON CONFLICT (org_id, idempotency_key) DO NOTHING
	RETURNING execution_id`,
		executionID, orgID, actorID, req.Kind, req.SourceKind, req.WorkloadKind, req.SourceRef,
		req.RunnerClass, req.ExternalProvider, req.ExternalTaskID, req.Provider, req.ProductID,
		StateQueued, correlationID, req.IdempotencyKey, req.RunCommand, req.MaxWallSeconds,
		int(req.Resources.VCPUs), int(req.Resources.MemoryMiB), int(req.Resources.RootDiskGiB), string(req.Resources.KernelImage),
		now)
	var inserted uuid.UUID
	if err := row.Scan(&inserted); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return s.existingSubmission(ctx, orgID, req.IdempotencyKey)
		}
		return uuid.Nil, uuid.Nil, fmt.Errorf("insert execution: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO execution_attempts (
		attempt_id, execution_id, attempt_seq, state, created_at, updated_at
	) VALUES ($1,$2,1,$3,$4,$4)`, attemptID, executionID, StateQueued, now); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("insert attempt: %w", err)
	}
	mounts := req.FilesystemMounts
	if mounts == nil {
		mounts, err = s.runnerClassFilesystemMounts(ctx, tx, req.RunnerClass)
		if err != nil {
			return uuid.Nil, uuid.Nil, err
		}
	}
	for _, sticky := range req.StickyDiskMounts {
		mounts = append(mounts, stickyDiskFilesystemMount(sticky))
	}
	if err := s.insertExecutionFilesystemMounts(ctx, tx, executionID, mounts); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if err := s.insertExecutionStickyDiskMounts(ctx, tx, executionID, attemptID, req.StickyDiskMounts); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if req.WorkloadKind == WorkloadKindGitHubRunner && req.GitHubAllocationID != uuid.Nil {
		if s.GitHubRunner == nil {
			return uuid.Nil, uuid.Nil, ErrGitHubRunnerNotConfigured
		}
		if err := s.GitHubRunner.attachAllocationExecutionTx(ctx, tx, req.GitHubAllocationID, executionID, attemptID, req.GitHubJITConfig); err != nil {
			return uuid.Nil, uuid.Nil, err
		}
	}
	if err := s.enqueueExecutionAdvance(ctx, tx, executionID, attemptID, orgID, actorID, correlationID); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("commit submit: %w", err)
	}
	span.SetAttributes(attribute.String("execution.id", executionID.String()), attribute.String("attempt.id", attemptID.String()))
	_ = s.writeJobEvent(context.Background(), jobEventRow{
		ExecutionID: executionID, AttemptID: attemptID, OrgID: orgID, ActorID: actorID,
		Kind: req.Kind, SourceKind: req.SourceKind, WorkloadKind: req.WorkloadKind, RunnerClass: req.RunnerClass,
		ExternalProvider: req.ExternalProvider, ExternalTaskID: req.ExternalTaskID, Provider: req.Provider, ProductID: req.ProductID,
		RunCommand: req.RunCommand, Status: StateQueued, CorrelationID: correlationID, CreatedAt: now,
	})
	return executionID, attemptID, nil
}

func (s *Service) enqueueExecutionAdvance(ctx context.Context, tx pgx.Tx, executionID, attemptID uuid.UUID, orgID uint64, actorID, correlationID string) error {
	if s.Scheduler != nil {
		_, err := s.Scheduler.EnqueueExecutionAdvanceTx(ctx, tx, scheduler.ExecutionAdvanceRequest{
			ExecutionID:   executionID.String(),
			AttemptID:     attemptID.String(),
			OrgID:         orgID,
			ActorID:       actorID,
			CorrelationID: correlationID,
			TraceParent:   traceParent(ctx),
		})
		return err
	}
	return fmt.Errorf("scheduler runtime unavailable")
}

func (s *Service) existingSubmission(ctx context.Context, orgID uint64, idempotencyKey string) (uuid.UUID, uuid.UUID, error) {
	row := s.PGX.QueryRow(ctx, `SELECT e.execution_id, a.attempt_id
		FROM executions e
		JOIN execution_attempts a ON a.execution_id = e.execution_id
		WHERE e.org_id = $1 AND e.idempotency_key = $2
		ORDER BY a.attempt_seq DESC
		LIMIT 1`, orgID, idempotencyKey)
	var executionID, attemptID uuid.UUID
	if err := row.Scan(&executionID, &attemptID); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("load existing execution: %w", err)
	}
	return executionID, attemptID, nil
}

func (s *Service) runnerClassFilesystemMounts(ctx context.Context, tx pgx.Tx, runnerClass string) ([]vmorchestrator.FilesystemMount, error) {
	rows, err := tx.Query(ctx, `SELECT mount_name, source_ref, mount_path, fs_type, read_only
		FROM runner_class_filesystem_mounts
		WHERE runner_class = $1 AND active
		ORDER BY sort_order, mount_name`, runnerClass)
	if err != nil {
		return nil, fmt.Errorf("load runner class filesystem mounts: %w", err)
	}
	defer rows.Close()
	out := []vmorchestrator.FilesystemMount{}
	for rows.Next() {
		var mount vmorchestrator.FilesystemMount
		if err := rows.Scan(&mount.Name, &mount.SourceRef, &mount.MountPath, &mount.FSType, &mount.ReadOnly); err != nil {
			return nil, fmt.Errorf("scan runner class filesystem mount: %w", err)
		}
		out = append(out, mount)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runner class filesystem mounts: %w", err)
	}
	return out, nil
}

func (s *Service) insertExecutionFilesystemMounts(ctx context.Context, tx pgx.Tx, executionID uuid.UUID, mounts []vmorchestrator.FilesystemMount) error {
	for idx, mount := range mounts {
		if _, err := tx.Exec(ctx, `INSERT INTO execution_filesystem_mounts (
			execution_id, mount_name, source_ref, mount_path, fs_type, read_only, sort_order, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			executionID, mount.Name, mount.SourceRef, mount.MountPath, firstNonEmpty(mount.FSType, "ext4"), mount.ReadOnly, idx, time.Now().UTC()); err != nil {
			return fmt.Errorf("insert execution filesystem mount %s: %w", mount.Name, err)
		}
	}
	return nil
}

func (s *Service) insertExecutionStickyDiskMounts(ctx context.Context, tx pgx.Tx, executionID, attemptID uuid.UUID, mounts []StickyDiskMountSpec) error {
	for idx, mount := range mounts {
		if mount.MountID == uuid.Nil {
			mount.MountID = uuid.New()
		}
		if _, err := tx.Exec(ctx, `INSERT INTO execution_sticky_disk_mounts (
			mount_id, execution_id, attempt_id, allocation_id, mount_name, key_hash, key, mount_path,
			base_generation, source_ref, target_source_ref, save_requested, save_state, committed_generation,
			failure_reason, sort_order, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,false,$12,0,'',$13,$14,$14)`,
			mount.MountID, executionID, attemptID, mount.AllocationID, mount.MountName, mount.KeyHash, mount.Key, mount.MountPath,
			mount.BaseGeneration, mount.SourceRef, mount.TargetSourceRef, stickyDiskStateNotRequested, idx, time.Now().UTC()); err != nil {
			return fmt.Errorf("insert execution sticky disk mount %s: %w", mount.MountName, err)
		}
	}
	return nil
}

func (s *Service) AdvanceExecution(ctx context.Context, executionID, attemptID uuid.UUID) error {
	ctx, span := tracer.Start(ctx, "sandbox-rental.execution.run", trace.WithAttributes(
		attribute.String("execution.id", executionID.String()),
		attribute.String("attempt.id", attemptID.String()),
	))
	defer span.End()
	item, err := s.loadWorkItem(ctx, executionID, attemptID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	ctx = WithCorrelationID(ctx, item.CorrelationID)
	billingJobID := billingJobIDForAttempt(item.AttemptID)
	if err := s.transition(ctx, item, StateQueued, StateReserved, "reserved", map[string]any{"billing_job_id": billingJobID}); err != nil {
		return err
	}
	reservation, err := s.reserveBilling(ctx, item, billingJobID)
	if err != nil {
		_ = s.failAttempt(context.Background(), item, "billing_reserve_failed", err)
		return err
	}
	if err := s.insertBillingWindow(ctx, item.AttemptID, reservation); err != nil {
		return err
	}
	if err := s.transition(ctx, item, StateReserved, StateLaunching, "launching", nil); err != nil {
		return err
	}

	lease, err := s.Orchestrator.AcquireLease(ctx, item.AttemptID.String()+":lease", vmorchestrator.LeaseSpec{
		Resources:        item.Resources,
		TTLSeconds:       300,
		TrustClass:       "trusted",
		NetworkMode:      "nat",
		FilesystemMounts: item.FilesystemMounts,
	})
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(detachedContext(ctx), 5*time.Second)
		defer cancel()
		_ = s.Billing.Void(cleanupCtx, reservation)
		_ = s.markBillingWindow(ctx, item.AttemptID, reservation.WindowId, "voided", 0)
		return s.failAttempt(ctx, item, "lease_acquire_failed", err)
	}
	item.LeaseID = lease.LeaseID
	_ = s.setAttemptLeaseExec(ctx, item.AttemptID, lease.LeaseID, "")

	renewCtx, stopRenew := context.WithCancel(detachedContext(ctx))
	defer stopRenew()
	go s.renewLeaseLoop(renewCtx, lease.LeaseID, item.AttemptID.String())

	execSpec := vmorchestrator.ExecSpec{
		Argv:           []string{"sh", "-c", item.RunCommand},
		WorkingDir:     "/workspace",
		Env:            s.executionEnv(ctx, item),
		MaxWallSeconds: maxWallSeconds(item, s.WorkloadTimeout),
	}
	execRecord, err := s.Orchestrator.StartExec(ctx, lease.LeaseID, item.AttemptID.String()+":exec", execSpec)
	if err != nil {
		s.cleanupLeaseAndReservation(ctx, lease.LeaseID, reservation)
		return s.failAttempt(ctx, item, "exec_start_failed", err)
	}
	item.ExecID = execRecord.ExecID
	_ = s.setAttemptLeaseExec(ctx, item.AttemptID, lease.LeaseID, execRecord.ExecID)
	activated, err := s.Billing.Activate(ctx, reservation, execRecord.StartedAt)
	if err != nil {
		_, _ = s.Orchestrator.CancelExec(detachedContext(ctx), lease.LeaseID, execRecord.ExecID, item.AttemptID.String()+":cancel", "billing_activate_failed")
		s.cleanupLeaseAndReservation(ctx, lease.LeaseID, reservation)
		return s.failAttempt(ctx, item, "billing_activate_failed", err)
	}
	reservation = activated
	if err := s.markRunning(ctx, item, execRecord.StartedAt); err != nil {
		return err
	}

	waitCtx := ctx
	if timeout := workloadTimeout(item, s.WorkloadTimeout); timeout > 0 {
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	finalExec, waitErr := s.Orchestrator.WaitExec(waitCtx, lease.LeaseID, execRecord.ExecID, true)
	if waitErr != nil {
		span.RecordError(waitErr)
		span.SetStatus(codes.Error, waitErr.Error())
		terminalCtx := detachedContext(ctx)
		_, _ = s.Orchestrator.CancelExec(terminalCtx, lease.LeaseID, execRecord.ExecID, item.AttemptID.String()+":timeout", "execution_wait_failed")
		s.cleanupLeaseAndReservation(terminalCtx, lease.LeaseID, reservation)
		_ = s.markBillingWindow(terminalCtx, item.AttemptID, reservation.WindowId, "voided", 0)
		return s.failAttempt(terminalCtx, item, "exec_wait_failed", waitErr)
	}
	if item.WorkloadKind == WorkloadKindGitHubRunner && s.GitHubRunner != nil {
		if err := s.GitHubRunner.CommitPendingStickyDisks(ctx, item, lease.LeaseID); err != nil && s.Logger != nil {
			s.Logger.WarnContext(ctx, "sticky disk async commits failed", "execution_id", item.ExecutionID, "attempt_id", item.AttemptID, "error", err)
		}
	}
	stopRenew()
	if err := s.Orchestrator.ReleaseLease(detachedContext(ctx), lease.LeaseID, item.AttemptID.String()+":release"); err != nil {
		s.Logger.WarnContext(ctx, "release lease failed", "lease_id", lease.LeaseID, "error", err)
	}

	completedAt := finalExec.ExitedAt
	if completedAt.IsZero() {
		completedAt = time.Now().UTC()
	}
	durationMs := completedAt.Sub(execRecord.StartedAt).Milliseconds()
	if durationMs < 1 {
		durationMs = 1
	}
	if err := s.Billing.Settle(ctx, reservation, uint32(clampUint32(durationMs)), usageSummary(finalExec)); err != nil {
		return s.failAttempt(ctx, item, "billing_settle_failed", err)
	}
	_ = s.markBillingWindow(ctx, item.AttemptID, reservation.WindowId, "settled", int(durationMs))
	state := StateSucceeded
	reason := ""
	if finalExec.ExitCode != 0 || finalExec.State == vmorchestrator.ExecStateFailed {
		state = StateFailed
		reason = "exec_failed"
	}
	if err := s.completeAttempt(ctx, item, state, reason, finalExec, durationMs, completedAt); err != nil {
		return err
	}
	_ = s.writeExecutionLogs(context.Background(), item.ExecutionID, item.AttemptID, finalExec.Output)
	_ = s.writeJobEvent(context.Background(), jobEventRow{
		ExecutionID: item.ExecutionID, AttemptID: item.AttemptID, OrgID: item.OrgID, ActorID: item.ActorID,
		Kind: item.Kind, SourceKind: item.SourceKind, WorkloadKind: item.WorkloadKind, RunnerClass: item.RunnerClass,
		ExternalProvider: item.ExternalProvider, ExternalTaskID: item.ExternalTaskID, Provider: item.Provider, ProductID: item.ProductID,
		RunCommand: item.RunCommand, Status: state, ExitCode: int32(finalExec.ExitCode), DurationMs: durationMs,
		ZFSWritten: finalExec.ZFSWritten, StdoutBytes: finalExec.StdoutBytes, StderrBytes: finalExec.StderrBytes,
		BillingJobID: billingJobID, PricingPhase: reservation.PricingPhase, CorrelationID: item.CorrelationID,
		StartedAt: execRecord.StartedAt, CompletedAt: completedAt, CreatedAt: time.Now().UTC(), TraceID: span.SpanContext().TraceID().String(),
	})
	if item.WorkloadKind == WorkloadKindGitHubRunner && s.GitHubRunner != nil {
		s.GitHubRunner.MarkExecutionExited(detachedContext(ctx), item.ExecutionID)
	}
	return nil
}

func (s *Service) loadWorkItem(ctx context.Context, executionID, attemptID uuid.UUID) (executionWorkItem, error) {
	row := s.PGX.QueryRow(ctx, `SELECT e.execution_id, a.attempt_id, e.org_id, e.actor_id, e.kind, e.source_kind, e.workload_kind, e.source_ref,
		e.runner_class, e.external_provider, e.external_task_id, e.provider, e.product_id, e.run_command, e.max_wall_seconds, e.correlation_id,
		e.requested_vcpus, e.requested_memory_mib, e.requested_root_disk_gib, e.requested_kernel_image,
		COALESCE(a.lease_id, ''), COALESCE(a.exec_id, '')
		FROM executions e JOIN execution_attempts a ON a.execution_id = e.execution_id
		WHERE e.execution_id = $1 AND a.attempt_id = $2`, executionID, attemptID)
	var item executionWorkItem
	var (
		vcpus, memMiB, diskGiB int
		kernelImage            string
	)
	if err := row.Scan(&item.ExecutionID, &item.AttemptID, &item.OrgID, &item.ActorID, &item.Kind, &item.SourceKind, &item.WorkloadKind, &item.SourceRef, &item.RunnerClass, &item.ExternalProvider, &item.ExternalTaskID, &item.Provider, &item.ProductID, &item.RunCommand, &item.MaxWallSeconds, &item.CorrelationID, &vcpus, &memMiB, &diskGiB, &kernelImage, &item.LeaseID, &item.ExecID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return executionWorkItem{}, ErrExecutionMissing
		}
		return executionWorkItem{}, fmt.Errorf("load execution work item: %w", err)
	}
	item.Resources = apiwire.VMResources{
		VCPUs:       uint32(vcpus),
		MemoryMiB:   uint32(memMiB),
		RootDiskGiB: uint32(diskGiB),
		KernelImage: apiwire.KernelImageRef(kernelImage),
	}
	mounts, err := s.loadExecutionFilesystemMounts(ctx, executionID)
	if err != nil {
		return executionWorkItem{}, err
	}
	item.FilesystemMounts = mounts
	return item, nil
}

func (s *Service) loadExecutionFilesystemMounts(ctx context.Context, executionID uuid.UUID) ([]vmorchestrator.FilesystemMount, error) {
	rows, err := s.PGX.Query(ctx, `SELECT mount_name, source_ref, mount_path, fs_type, read_only
		FROM execution_filesystem_mounts
		WHERE execution_id = $1
		ORDER BY sort_order, mount_name`, executionID)
	if err != nil {
		return nil, fmt.Errorf("load execution filesystem mounts: %w", err)
	}
	defer rows.Close()
	out := []vmorchestrator.FilesystemMount{}
	for rows.Next() {
		var mount vmorchestrator.FilesystemMount
		if err := rows.Scan(&mount.Name, &mount.SourceRef, &mount.MountPath, &mount.FSType, &mount.ReadOnly); err != nil {
			return nil, fmt.Errorf("scan execution filesystem mount: %w", err)
		}
		out = append(out, mount)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate execution filesystem mounts: %w", err)
	}
	return out, nil
}

func billingJobIDForAttempt(attemptID uuid.UUID) int64 {
	// A sandbox-local sequence collides after sandbox DB resets while billing
	// keeps historical windows; the attempt UUID is the cross-service identity.
	raw := binary.BigEndian.Uint64(attemptID[:8])
	return int64(raw & math.MaxInt64)
}

func (s *Service) reserveBilling(ctx context.Context, item executionWorkItem, billingJobID int64) (billingclient.Reservation, error) {
	// Billing rates are SKU-ms rates; the customer's requested shape is
	// what we charge for — not the host capacity headroom. Each SKU's
	// advertised unit translates directly from VMResources: vCPUs into
	// compute vCPU-ms, MemoryMiB into memory GiB-ms, RootDiskGiB into
	// block-storage GiB-ms. Windows settle in millisecond quantities so
	// the final magnitudes are (unit × duration_ms) per SKU.
	res := item.Resources.Normalize()
	allocation := map[string]float64{
		billingSKUComputeVCPUMs:     float64(res.VCPUs),
		billingSKUMemoryGiBMs:       float64(res.MemoryMiB) / billingMiBPerGiB,
		billingSKUBlockStorageGiBMs: float64(res.RootDiskGiB),
	}
	return s.Billing.Reserve(ctx, billingJobID, item.OrgID, item.ProductID, item.ActorID, 1, item.SourceKind, item.ExecutionID.String(), 1, allocation)
}

func (s *Service) insertBillingWindow(ctx context.Context, attemptID uuid.UUID, reservation billingclient.Reservation) error {
	payload, _ := json.Marshal(reservation)
	_, err := s.PGX.Exec(ctx, `INSERT INTO execution_billing_windows (
		attempt_id, window_seq, billing_window_id, reservation_shape, reserved_quantity, actual_quantity,
		pricing_phase, state, window_start, created_at, reservation_jsonb
	) VALUES ($1,$2,$3,$4,$5,0,$6,'reserved',$7,$8,$9)`,
		attemptID, reservation.WindowSeq, reservation.WindowId, reservation.ReservationShape, reservation.WindowMillis, reservation.PricingPhase, reservation.WindowStart, time.Now().UTC(), payload)
	if err != nil {
		return fmt.Errorf("insert billing window: %w", err)
	}
	return nil
}

func (s *Service) markBillingWindow(ctx context.Context, attemptID uuid.UUID, windowID, state string, actual int) error {
	_, err := s.PGX.Exec(ctx, `UPDATE execution_billing_windows SET state = $1, actual_quantity = $2, settled_at = $3 WHERE attempt_id = $4 AND billing_window_id = $5`, state, actual, time.Now().UTC(), attemptID, windowID)
	return err
}

func (s *Service) transition(ctx context.Context, item executionWorkItem, from, to, reason string, values map[string]any) error {
	now := time.Now().UTC()
	var billingJobID any
	if values != nil {
		billingJobID = values["billing_job_id"]
	}
	tx, err := s.PGX.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE executions SET state = $1, updated_at = $2 WHERE execution_id = $3`, to, now, item.ExecutionID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE execution_attempts SET state = $1, billing_job_id = COALESCE($2, billing_job_id), updated_at = $3 WHERE attempt_id = $4 AND state = $5`, to, billingJobID, now, item.AttemptID, from)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("execution attempt %s is not in expected state %s", item.AttemptID, from)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO execution_events (execution_id, attempt_id, from_state, to_state, reason, created_at) VALUES ($1,$2,$3,$4,$5,$6)`, item.ExecutionID, item.AttemptID, from, to, reason, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) markRunning(ctx context.Context, item executionWorkItem, startedAt time.Time) error {
	now := time.Now().UTC()
	tx, err := s.PGX.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE executions SET state = $1, updated_at = $2 WHERE execution_id = $3`, StateRunning, now, item.ExecutionID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE execution_attempts SET state = $1, started_at = $2, updated_at = $3 WHERE attempt_id = $4 AND state = $5`, StateRunning, startedAt, now, item.AttemptID, StateLaunching)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("execution attempt %s is not in expected state %s", item.AttemptID, StateLaunching)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO execution_events (execution_id, attempt_id, from_state, to_state, reason, created_at) VALUES ($1,$2,$3,$4,$5,$6)`, item.ExecutionID, item.AttemptID, StateLaunching, StateRunning, "exec_started", now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) setAttemptLeaseExec(ctx context.Context, attemptID uuid.UUID, leaseID, execID string) error {
	_, err := s.PGX.Exec(ctx, `UPDATE execution_attempts SET lease_id = NULLIF($1, ''), exec_id = NULLIF($2, ''), updated_at = $3 WHERE attempt_id = $4`, leaseID, execID, time.Now().UTC(), attemptID)
	return err
}

func (s *Service) completeAttempt(ctx context.Context, item executionWorkItem, state, reason string, exec vmorchestrator.ExecRecord, durationMs int64, completedAt time.Time) error {
	now := time.Now().UTC()
	tx, err := s.PGX.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE executions SET state = $1, updated_at = $2 WHERE execution_id = $3`, state, now, item.ExecutionID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE execution_attempts SET state = $1, failure_reason = $2, exit_code = $3, duration_ms = $4, zfs_written = $5, stdout_bytes = $6, stderr_bytes = $7, completed_at = $8, updated_at = $9 WHERE attempt_id = $10 AND state = $11`,
		state, reason, exec.ExitCode, durationMs, int64(exec.ZFSWritten), int64(exec.StdoutBytes), int64(exec.StderrBytes), completedAt, now, item.AttemptID, StateRunning)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("execution attempt %s is not in expected state %s", item.AttemptID, StateRunning)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO execution_events (execution_id, attempt_id, from_state, to_state, reason, created_at) VALUES ($1,$2,$3,$4,$5,$6)`, item.ExecutionID, item.AttemptID, StateRunning, StateFinalizing, "exec_finished", now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO execution_events (execution_id, attempt_id, from_state, to_state, reason, created_at) VALUES ($1,$2,$3,$4,$5,$6)`, item.ExecutionID, item.AttemptID, StateFinalizing, state, reason, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) failAttempt(ctx context.Context, item executionWorkItem, reason string, cause error) error {
	now := time.Now().UTC()
	tx, err := s.PGX.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE executions SET state = $1, updated_at = $2 WHERE execution_id = $3`, StateFailed, now, item.ExecutionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE execution_attempts SET state = $1, failure_reason = $2, completed_at = $3, updated_at = $3 WHERE attempt_id = $4`, StateFailed, reason, now, item.AttemptID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO execution_events (execution_id, attempt_id, from_state, to_state, reason, created_at) VALUES ($1,$2,'',$3,$4,$5)`, item.ExecutionID, item.AttemptID, StateFailed, reason, now); err != nil {
		return err
	}
	err = tx.Commit(ctx)
	if cause != nil {
		slog.Default().WarnContext(ctx, "execution failed", "execution_id", item.ExecutionID, "attempt_id", item.AttemptID, "reason", reason, "error", cause)
	}
	return err
}

func (s *Service) cleanupLeaseAndReservation(ctx context.Context, leaseID string, reservation billingclient.Reservation) {
	cleanupCtx, cancel := context.WithTimeout(detachedContext(ctx), 5*time.Second)
	defer cancel()
	if leaseID != "" {
		_ = s.Orchestrator.ReleaseLease(cleanupCtx, leaseID, reservation.WindowId+":release")
	}
	_ = s.Billing.Void(cleanupCtx, reservation)
}

func (s *Service) renewLeaseLoop(ctx context.Context, leaseID, keyPrefix string) {
	ticker := time.NewTicker(4 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			renewCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_, err := s.Orchestrator.RenewLease(renewCtx, leaseID, keyPrefix+":renew:"+time.Now().UTC().Format(time.RFC3339Nano), 300, nil)
			cancel()
			if err != nil {
				slog.Default().WarnContext(ctx, "lease renewal failed", "lease_id", leaseID, "error", err)
			}
		}
	}
}

func (s *Service) GetExecution(ctx context.Context, orgID uint64, executionID uuid.UUID) (*ExecutionRecord, error) {
	row := s.PGX.QueryRow(ctx, `SELECT e.execution_id, e.org_id, e.actor_id, e.kind, e.source_kind, e.workload_kind, e.source_ref, e.runner_class, e.external_provider, e.external_task_id, e.provider, e.product_id, e.state, e.correlation_id, e.idempotency_key, e.run_command, e.created_at, e.updated_at,
		a.attempt_id, a.attempt_seq, a.state, COALESCE(a.lease_id,''), COALESCE(a.exec_id,''), COALESCE(a.billing_job_id, 0), a.failure_reason, a.exit_code, a.duration_ms, a.zfs_written, a.stdout_bytes, a.stderr_bytes, a.trace_id, a.started_at, a.completed_at, a.created_at, a.updated_at
		FROM executions e JOIN execution_attempts a ON a.execution_id = e.execution_id
		WHERE e.org_id = $1 AND e.execution_id = $2 ORDER BY a.attempt_seq DESC LIMIT 1`, orgID, executionID)
	var record ExecutionRecord
	var attempt AttemptRecord
	if err := row.Scan(&record.ExecutionID, &record.OrgID, &record.ActorID, &record.Kind, &record.SourceKind, &record.WorkloadKind, &record.SourceRef, &record.RunnerClass, &record.ExternalProvider, &record.ExternalTaskID, &record.Provider, &record.ProductID, &record.Status, &record.CorrelationID, &record.IdempotencyKey, &record.RunCommand, &record.CreatedAt, &record.UpdatedAt,
		&attempt.AttemptID, &attempt.AttemptSeq, &attempt.State, &attempt.LeaseID, &attempt.ExecID, &attempt.BillingJobID, &attempt.FailureReason, &attempt.ExitCode, &attempt.DurationMs, &attempt.ZFSWritten, &attempt.StdoutBytes, &attempt.StderrBytes, &attempt.TraceID, &attempt.StartedAt, &attempt.CompletedAt, &attempt.CreatedAt, &attempt.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrExecutionMissing
		}
		return nil, err
	}
	record.LatestAttempt = attempt
	windows, _ := s.listBillingWindows(ctx, attempt.AttemptID)
	record.BillingWindows = windows
	return &record, nil
}

func (s *Service) GetExecutionLogs(ctx context.Context, orgID uint64, executionID uuid.UUID) (uuid.UUID, string, error) {
	var attemptID uuid.UUID
	if err := s.PGX.QueryRow(ctx, `SELECT a.attempt_id FROM executions e JOIN execution_attempts a ON a.execution_id = e.execution_id WHERE e.org_id = $1 AND e.execution_id = $2 ORDER BY a.attempt_seq DESC LIMIT 1`, orgID, executionID).Scan(&attemptID); err != nil {
		return uuid.Nil, "", ErrExecutionMissing
	}
	rows, err := s.PGX.Query(ctx, `SELECT chunk FROM execution_logs WHERE attempt_id = $1 ORDER BY seq ASC`, attemptID)
	if err != nil {
		return uuid.Nil, "", err
	}
	defer rows.Close()
	var builder strings.Builder
	for rows.Next() {
		var chunk string
		if err := rows.Scan(&chunk); err != nil {
			return uuid.Nil, "", err
		}
		builder.WriteString(chunk)
	}
	return attemptID, builder.String(), rows.Err()
}

func (s *Service) listBillingWindows(ctx context.Context, attemptID uuid.UUID) ([]BillingWindow, error) {
	rows, err := s.PGX.Query(ctx, `SELECT attempt_id, billing_window_id, window_seq, reservation_shape, reserved_quantity, actual_quantity, pricing_phase, state, window_start, created_at, settled_at FROM execution_billing_windows WHERE attempt_id = $1 ORDER BY window_seq`, attemptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BillingWindow{}
	for rows.Next() {
		var window BillingWindow
		if err := rows.Scan(&window.AttemptID, &window.BillingWindowID, &window.WindowSeq, &window.ReservationShape, &window.ReservedQuantity, &window.ActualQuantity, &window.PricingPhase, &window.State, &window.WindowStart, &window.CreatedAt, &window.SettledAt); err != nil {
			return nil, err
		}
		out = append(out, window)
	}
	return out, rows.Err()
}

func (s *Service) writeExecutionLogs(ctx context.Context, executionID, attemptID uuid.UUID, logs string) error {
	if logs == "" {
		return nil
	}
	_, err := s.PGX.Exec(ctx, `INSERT INTO execution_logs (execution_id, attempt_id, seq, stream, chunk, created_at) VALUES ($1,$2,1,'combined',$3,$4)`, executionID, attemptID, logs, time.Now().UTC())
	if err != nil {
		return err
	}
	if s.CH == nil {
		return nil
	}
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO "+s.CHDatabase+".job_logs")
	if err != nil {
		return err
	}
	if err := batch.AppendStruct(&jobLogRow{ExecutionID: executionID, AttemptID: attemptID, Seq: 1, Stream: "combined", Chunk: logs, CreatedAt: time.Now().UTC()}); err != nil {
		return err
	}
	return batch.Send()
}

func (s *Service) writeJobEvent(ctx context.Context, row jobEventRow) error {
	if s.CH == nil {
		return nil
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}
	if row.StartedAt.IsZero() {
		row.StartedAt = row.CreatedAt
	}
	if row.CompletedAt.IsZero() {
		row.CompletedAt = row.CreatedAt
	}
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO "+s.CHDatabase+".job_events")
	if err != nil {
		return err
	}
	if err := batch.AppendStruct(&row); err != nil {
		return err
	}
	return batch.Send()
}

func (s *Service) normalizeSubmitRequest(req SubmitRequest) (SubmitRequest, error) {
	req.Kind = firstNonEmpty(strings.TrimSpace(req.Kind), KindDirect)
	req.SourceKind = firstNonEmpty(strings.TrimSpace(req.SourceKind), SourceKindAPI)
	req.WorkloadKind = firstNonEmpty(strings.TrimSpace(req.WorkloadKind), WorkloadKindDirect)
	req.RunnerClass = firstNonEmpty(strings.TrimSpace(req.RunnerClass), DefaultRunnerClassLabel)
	req.ProductID = firstNonEmpty(strings.TrimSpace(req.ProductID), defaultProductID)
	req.Provider = strings.TrimSpace(req.Provider)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	req.SourceRef = strings.TrimSpace(req.SourceRef)
	req.ExternalProvider = strings.TrimSpace(req.ExternalProvider)
	req.ExternalTaskID = strings.TrimSpace(req.ExternalTaskID)
	req.RunCommand = strings.TrimSpace(req.RunCommand)
	if req.IdempotencyKey == "" {
		return SubmitRequest{}, fmt.Errorf("idempotency_key is required")
	}
	if req.RunCommand == "" && req.WorkloadKind == WorkloadKindDirect {
		req.RunCommand = defaultRunCommand
	}
	switch req.WorkloadKind {
	case WorkloadKindDirect, WorkloadKindGitHubRunner:
	default:
		return SubmitRequest{}, fmt.Errorf("unsupported workload_kind %q", req.WorkloadKind)
	}
	// Apply defaults + bounds check at intake so the customer sees 400 on
	// an out-of-bounds shape before any billing reservation is attempted.
	req.Resources = req.Resources.Normalize()
	bounds := s.Bounds
	if bounds == (apiwire.VMResourceBounds{}) {
		bounds = apiwire.DefaultBounds
	}
	if err := req.Resources.Validate(bounds); err != nil {
		return SubmitRequest{}, err
	}
	return req, nil
}

func (s *Service) executionEnv(ctx context.Context, item executionWorkItem) map[string]string {
	env := map[string]string{
		"FORGE_METAL_EXECUTION_ID": item.ExecutionID.String(),
		"FORGE_METAL_ATTEMPT_ID":   item.AttemptID.String(),
		"FORGE_METAL_RUNNER_CLASS": item.RunnerClass,
		"FORGE_METAL_SOURCE_KIND":  item.SourceKind,
	}
	if item.WorkloadKind == WorkloadKindGitHubRunner && s.GitHubRunner != nil {
		for key, value := range s.GitHubRunner.execEnv(ctx, item.ExecutionID, item.AttemptID) {
			env[key] = value
		}
	}
	return env
}

func usageSummary(exec vmorchestrator.ExecRecord) map[string]any {
	return map[string]any{
		"lease_id":                 exec.LeaseID,
		"exec_id":                  exec.ExecID,
		"exit_code":                exec.ExitCode,
		"stdout_bytes":             exec.StdoutBytes,
		"stderr_bytes":             exec.StderrBytes,
		"zfs_written":              exec.ZFSWritten,
		"rootfs_provisioned_bytes": exec.RootfsProvisionedBytes,
	}
}

func workloadTimeout(item executionWorkItem, configured time.Duration) time.Duration {
	if item.MaxWallSeconds > 0 {
		return time.Duration(item.MaxWallSeconds) * time.Second
	}
	if configured > 0 {
		return configured
	}
	return 2 * time.Hour
}

func maxWallSeconds(item executionWorkItem, configured time.Duration) uint64 {
	if item.MaxWallSeconds > 0 {
		return item.MaxWallSeconds
	}
	if configured > 0 {
		return uint64(configured.Seconds())
	}
	return 2 * 60 * 60
}

func clampUint32(value int64) uint32 {
	if value <= 0 {
		return 0
	}
	if value > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func traceParent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	return carrier.Get("traceparent")
}

func detachedContext(ctx context.Context) context.Context {
	return trace.ContextWithSpanContext(context.Background(), trace.SpanContextFromContext(ctx))
}
