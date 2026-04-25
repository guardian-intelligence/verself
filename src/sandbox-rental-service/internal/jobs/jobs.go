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
	"net/http"
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
	KindDirect                  = "direct"
	KindVMSession               = "vm_session"
	SourceKindAPI               = "api"
	SourceKindExecutionSchedule = "execution_schedule"
	SourceKindGitHubAction      = "github_actions"
	SourceKindSourceHosting     = "source_code_hosting"
	SourceKindCanary            = "canary"
	SourceKindVMSession         = "vm_session"
	WorkloadKindDirect          = "direct"
	WorkloadKindGitHubRunner    = "github_runner"

	DefaultRunnerClassLabel = "metal-4vcpu-ubuntu-2404"
	defaultProductID        = "sandbox"
	defaultRunCommand       = "echo hello"

	billingSKUComputeVCPUMs             = "sandbox_compute_amd_epyc_4484px_vcpu_ms"
	billingSKUMemoryGiBMs               = "sandbox_memory_standard_gib_ms"
	billingSKUExecutionRootStorageGiBMs = "sandbox_execution_root_storage_premium_nvme_gib_ms"
	billingMiBPerGiB                    = 1024
	billingBytesPerGiB                  = 1024 * 1024 * 1024

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
	ErrQuotaExceeded          = errors.New("sandbox-rental: quota exceeded")
	ErrExecutionMissing       = errors.New("sandbox-rental: execution missing")
	ErrRunnerUnavailable      = errors.New("sandbox-rental: runner unavailable")
	ErrRunnerClassMissing     = errors.New("sandbox-rental: runner class missing")
	ErrBillingPaymentRequired = errors.New("sandbox-rental: billing payment required")
	ErrBillingForbidden       = errors.New("sandbox-rental: billing forbidden")
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
	RunID            uuid.UUID
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
	BillingSummary   RunBillingSummary
	GitHub           GitHubRunMetadata
	Schedule         ScheduleRunMetadata
	StickyDiskMounts []StickyDiskMountRecord
}

type AttemptRecord struct {
	AttemptID              uuid.UUID
	AttemptSeq             int
	State                  string
	LeaseID                string
	ExecID                 string
	BillingJobID           int64
	FailureReason          string
	ExitCode               int
	DurationMs             int64
	ZFSWritten             int64
	StdoutBytes            int64
	StderrBytes            int64
	RootfsProvisionedBytes int64
	BootTimeUs             int64
	BlockReadBytes         int64
	BlockWriteBytes        int64
	NetRXBytes             int64
	NetTXBytes             int64
	VCPUExitCount          int64
	TraceID                string
	StartedAt              *time.Time
	CompletedAt            *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type BillingWindow struct {
	AttemptID           uuid.UUID
	BillingWindowID     string
	WindowSeq           int
	ReservationShape    string
	ReservedQuantity    int
	ActualQuantity      int
	ReservedChargeUnits uint64
	BilledChargeUnits   uint64
	WriteoffChargeUnits uint64
	CostPerUnit         uint64
	PricingPhase        string
	State               string
	WindowStart         time.Time
	CreatedAt           time.Time
	SettledAt           *time.Time
}

type Service struct {
	PG               *sql.DB
	PGX              *pgxpool.Pool
	CH               driver.Conn
	CHDatabase       string
	Orchestrator     Runner
	Billing          *billingclient.ClientWithResponses
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
	ExecutionID            uuid.UUID `ch:"execution_id"`
	AttemptID              uuid.UUID `ch:"attempt_id"`
	OrgID                  uint64    `ch:"org_id"`
	ActorID                string    `ch:"actor_id"`
	Kind                   string    `ch:"kind"`
	SourceKind             string    `ch:"source_kind"`
	WorkloadKind           string    `ch:"workload_kind"`
	SourceRef              string    `ch:"source_ref"`
	RunnerClass            string    `ch:"runner_class"`
	ExternalProvider       string    `ch:"external_provider"`
	ExternalTaskID         string    `ch:"external_task_id"`
	Provider               string    `ch:"provider"`
	ProductID              string    `ch:"product_id"`
	LeaseID                string    `ch:"lease_id"`
	ExecID                 string    `ch:"exec_id"`
	RepositoryFullName     string    `ch:"repository_full_name"`
	WorkflowName           string    `ch:"workflow_name"`
	JobName                string    `ch:"job_name"`
	HeadBranch             string    `ch:"head_branch"`
	HeadSHA                string    `ch:"head_sha"`
	GitHubInstallationID   uint64    `ch:"github_installation_id"`
	GitHubRunID            uint64    `ch:"github_run_id"`
	GitHubJobID            uint64    `ch:"github_job_id"`
	ScheduleID             string    `ch:"schedule_id"`
	ScheduleDisplayName    string    `ch:"schedule_display_name"`
	TemporalWorkflowID     string    `ch:"temporal_workflow_id"`
	TemporalRunID          string    `ch:"temporal_run_id"`
	RunCommand             string    `ch:"run_command"`
	Status                 string    `ch:"status"`
	ExitCode               int32     `ch:"exit_code"`
	DurationMs             int64     `ch:"duration_ms"`
	ZFSWritten             uint64    `ch:"zfs_written"`
	StdoutBytes            uint64    `ch:"stdout_bytes"`
	StderrBytes            uint64    `ch:"stderr_bytes"`
	BillingJobID           int64     `ch:"billing_job_id"`
	ReservedChargeUnits    uint64    `ch:"reserved_charge_units"`
	BilledChargeUnits      uint64    `ch:"billed_charge_units"`
	WriteoffChargeUnits    uint64    `ch:"writeoff_charge_units"`
	CostPerUnit            uint64    `ch:"cost_per_unit"`
	PricingPhase           string    `ch:"pricing_phase"`
	RootfsProvisionedBytes uint64    `ch:"rootfs_provisioned_bytes"`
	BootTimeUs             uint64    `ch:"boot_time_us"`
	BlockReadBytes         uint64    `ch:"block_read_bytes"`
	BlockWriteBytes        uint64    `ch:"block_write_bytes"`
	NetRXBytes             uint64    `ch:"net_rx_bytes"`
	NetTXBytes             uint64    `ch:"net_tx_bytes"`
	VCPUExitCount          uint64    `ch:"vcpu_exit_count"`
	CorrelationID          string    `ch:"correlation_id"`
	StartedAt              time.Time `ch:"started_at"`
	CompletedAt            time.Time `ch:"completed_at"`
	CreatedAt              time.Time `ch:"created_at"`
	TraceID                string    `ch:"trace_id"`
}

type jobLogRow struct {
	ExecutionID        uuid.UUID `ch:"execution_id"`
	AttemptID          uuid.UUID `ch:"attempt_id"`
	OrgID              uint64    `ch:"org_id"`
	SourceKind         string    `ch:"source_kind"`
	WorkloadKind       string    `ch:"workload_kind"`
	RunnerClass        string    `ch:"runner_class"`
	ExternalProvider   string    `ch:"external_provider"`
	ProductID          string    `ch:"product_id"`
	CorrelationID      string    `ch:"correlation_id"`
	RepositoryFullName string    `ch:"repository_full_name"`
	WorkflowName       string    `ch:"workflow_name"`
	JobName            string    `ch:"job_name"`
	HeadBranch         string    `ch:"head_branch"`
	ScheduleID         string    `ch:"schedule_id"`
	Seq                uint32    `ch:"seq"`
	Stream             string    `ch:"stream"`
	Chunk              string    `ch:"chunk"`
	CreatedAt          time.Time `ch:"created_at"`
}

type billingStatusError struct {
	Operation  string
	StatusCode int
	Detail     string
	Cause      error
}

func (e *billingStatusError) Error() string {
	if e == nil {
		return "sandbox-rental: billing error"
	}
	switch {
	case e.Detail != "":
		return e.Operation + ": " + e.Detail
	case e.Cause != nil:
		return e.Operation + ": " + e.Cause.Error()
	default:
		return e.Operation
	}
}

func (e *billingStatusError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
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
	if s.PGX == nil || s.Scheduler == nil {
		return uuid.Nil, uuid.Nil, ErrRunnerUnavailable
	}
	req, err = s.normalizeSubmitRequest(ctx, req)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
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

func (s *Service) runnerClassResources(ctx context.Context, runnerClass string) (apiwire.VMResources, string, bool, error) {
	var (
		productID                   string
		vcpus, memoryMiB, rootfsGiB int
	)
	err := s.PGX.QueryRow(ctx, `SELECT product_id, vcpus, memory_mib, rootfs_gib FROM runner_classes WHERE runner_class = $1 AND active`, runnerClass).Scan(&productID, &vcpus, &memoryMiB, &rootfsGiB)
	if errors.Is(err, pgx.ErrNoRows) {
		return apiwire.VMResources{}, "", false, nil
	}
	if err != nil {
		return apiwire.VMResources{}, "", false, fmt.Errorf("load runner class resources: %w", err)
	}
	return apiwire.VMResources{
		VCPUs:       uint32(vcpus),
		MemoryMiB:   uint32(memoryMiB),
		RootDiskGiB: uint32(rootfsGiB),
		KernelImage: apiwire.KernelImageDefault,
	}, productID, true, nil
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
		_ = s.voidBillingWindow(cleanupCtx, reservation)
		_ = s.markBillingWindow(ctx, item.AttemptID, reservation.WindowID, "voided", 0, apiwire.BillingSettleResult{})
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
	activated, err := s.activateBillingWindow(ctx, reservation, execRecord.StartedAt)
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
		_ = s.markBillingWindow(terminalCtx, item.AttemptID, reservation.WindowID, "voided", 0, apiwire.BillingSettleResult{})
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
	settleResult, err := s.settleBillingWindow(ctx, reservation, uint32(clampUint32(durationMs)), usageSummary(finalExec))
	if err != nil {
		return s.failAttempt(ctx, item, "billing_settle_failed", err)
	}
	_ = s.markBillingWindow(ctx, item.AttemptID, reservation.WindowID, "settled", int(durationMs), settleResult)
	state := StateSucceeded
	reason := ""
	if finalExec.ExitCode != 0 || finalExec.State == vmorchestrator.ExecStateFailed {
		state = StateFailed
		reason = "exec_failed"
	}
	if err := s.completeAttempt(ctx, item, state, reason, finalExec, durationMs, completedAt); err != nil {
		return err
	}
	runRecord, err := s.loadRun(ctx, item.OrgID, item.ExecutionID, false, false)
	if err == nil {
		runRecord.Status = state
		runRecord.LatestAttempt.TraceID = span.SpanContext().TraceID().String()
		_ = s.writeExecutionLogs(context.Background(), *runRecord, finalExec.Output)
		_ = s.writeJobEvent(context.Background(), jobEventRowForRun(*runRecord))
	} else if s.Logger != nil {
		s.Logger.WarnContext(ctx, "load run projection after execution failed", "execution_id", item.ExecutionID, "attempt_id", item.AttemptID, "error", err)
	}
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
	const maxJSONSafePositiveInt = uint64(1<<53 - 1)
	raw := binary.BigEndian.Uint64(attemptID[:8]) & maxJSONSafePositiveInt
	if raw == 0 {
		return 1
	}
	return int64(raw)
}

func (s *Service) reserveBilling(ctx context.Context, item executionWorkItem, billingJobID int64) (apiwire.BillingWindowReservation, error) {
	// Billing rates are SKU-ms rates; the customer's requested shape is
	// what we charge for — not the host capacity headroom. Each SKU's
	// advertised unit translates directly from VMResources: vCPUs into
	// compute vCPU-ms, MemoryMiB into memory GiB-ms, RootDiskGiB into
	// block-storage GiB-ms. Windows settle in millisecond quantities so
	// the final magnitudes are (unit × duration_ms) per SKU.
	res := item.Resources.Normalize()
	allocation := map[string]float64{
		billingSKUComputeVCPUMs:             float64(res.VCPUs),
		billingSKUMemoryGiBMs:               float64(res.MemoryMiB) / billingMiBPerGiB,
		billingSKUExecutionRootStorageGiBMs: float64(res.RootDiskGiB),
	}
	return s.reserveBillingWindow(ctx, apiwire.BillingReserveWindowRequest{
		OrgID:            apiwire.Uint64(item.OrgID),
		ProductID:        item.ProductID,
		ActorID:          item.ActorID,
		ConcurrentCount:  1,
		SourceType:       item.SourceKind,
		SourceRef:        item.ExecutionID.String(),
		WindowSeq:        1,
		ReservationShape: string(billingclient.Time),
		ReservedQuantity: 0,
		BillingJobID:     billingJobID,
		Allocation:       allocation,
	})
}

func (s *Service) insertBillingWindow(ctx context.Context, attemptID uuid.UUID, reservation apiwire.BillingWindowReservation) error {
	payload, _ := json.Marshal(reservation)
	_, err := s.PGX.Exec(ctx, `INSERT INTO execution_billing_windows (
		attempt_id, window_seq, billing_window_id, reservation_shape, reserved_quantity, actual_quantity,
		reserved_charge_units, billed_charge_units, writeoff_charge_units, cost_per_unit,
		pricing_phase, state, window_start, created_at, reservation_jsonb
	) VALUES ($1,$2,$3,$4,$5,0,$6,0,0,$7,$8,'reserved',$9,$10,$11)`,
		attemptID,
		reservation.WindowSeq,
		reservation.WindowID,
		reservation.ReservationShape,
		reservation.ReservedQuantity,
		reservation.ReservedChargeUnits.Uint64(),
		reservation.CostPerUnit.Uint64(),
		reservation.PricingPhase,
		reservation.WindowStart,
		time.Now().UTC(),
		payload,
	)
	if err != nil {
		return fmt.Errorf("insert billing window: %w", err)
	}
	return nil
}

func (s *Service) markBillingWindow(ctx context.Context, attemptID uuid.UUID, windowID, state string, actual int, settled apiwire.BillingSettleResult) error {
	_, err := s.PGX.Exec(ctx, `UPDATE execution_billing_windows
		SET state = $1,
		    actual_quantity = $2,
		    billed_charge_units = $3,
		    writeoff_charge_units = $4,
		    settled_at = $5
		WHERE attempt_id = $6
		  AND billing_window_id = $7`,
		state,
		actual,
		settled.BilledChargeUnits.Uint64(),
		settled.WriteoffChargeUnits.Uint64(),
		time.Now().UTC(),
		attemptID,
		windowID,
	)
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
	metrics := exec.Metrics
	traceID := trace.SpanContextFromContext(ctx).TraceID().String()
	tx, err := s.PGX.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE executions SET state = $1, updated_at = $2 WHERE execution_id = $3`, state, now, item.ExecutionID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE execution_attempts
		SET state = $1,
		    failure_reason = $2,
		    exit_code = $3,
		    duration_ms = $4,
		    zfs_written = $5,
		    stdout_bytes = $6,
		    stderr_bytes = $7,
		    rootfs_provisioned_bytes = $8,
		    boot_time_us = $9,
		    block_read_bytes = $10,
		    block_write_bytes = $11,
		    net_rx_bytes = $12,
		    net_tx_bytes = $13,
		    vcpu_exit_count = $14,
		    trace_id = $15,
		    completed_at = $16,
		    updated_at = $17
		WHERE attempt_id = $18
		  AND state = $19`,
		state,
		reason,
		exec.ExitCode,
		durationMs,
		int64(exec.ZFSWritten),
		int64(exec.StdoutBytes),
		int64(exec.StderrBytes),
		int64(exec.RootfsProvisionedBytes),
		vmMetricUint64(metrics, func(m *vmorchestrator.VMMetrics) uint64 { return m.BootTimeUs }),
		vmMetricUint64(metrics, func(m *vmorchestrator.VMMetrics) uint64 { return m.BlockReadBytes }),
		vmMetricUint64(metrics, func(m *vmorchestrator.VMMetrics) uint64 { return m.BlockWriteBytes }),
		vmMetricUint64(metrics, func(m *vmorchestrator.VMMetrics) uint64 { return m.NetRxBytes }),
		vmMetricUint64(metrics, func(m *vmorchestrator.VMMetrics) uint64 { return m.NetTxBytes }),
		vmMetricUint64(metrics, func(m *vmorchestrator.VMMetrics) uint64 { return m.VCPUExitCount }),
		traceID,
		completedAt,
		now,
		item.AttemptID,
		StateRunning,
	)
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
	traceID := trace.SpanContextFromContext(ctx).TraceID().String()
	tx, err := s.PGX.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE executions SET state = $1, updated_at = $2 WHERE execution_id = $3`, StateFailed, now, item.ExecutionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE execution_attempts SET state = $1, failure_reason = $2, trace_id = $3, completed_at = $4, updated_at = $4 WHERE attempt_id = $5`, StateFailed, reason, traceID, now, item.AttemptID); err != nil {
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

func (s *Service) cleanupLeaseAndReservation(ctx context.Context, leaseID string, reservation apiwire.BillingWindowReservation) {
	cleanupCtx, cancel := context.WithTimeout(detachedContext(ctx), 5*time.Second)
	defer cancel()
	if leaseID != "" {
		_ = s.Orchestrator.ReleaseLease(cleanupCtx, leaseID, reservation.WindowID+":release")
	}
	_ = s.voidBillingWindow(cleanupCtx, reservation)
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
	return s.GetRun(ctx, orgID, executionID)
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
	rows, err := s.PGX.Query(ctx, `SELECT
			attempt_id,
			billing_window_id,
			window_seq,
			reservation_shape,
			reserved_quantity,
			actual_quantity,
			reserved_charge_units,
			billed_charge_units,
			writeoff_charge_units,
			cost_per_unit,
			pricing_phase,
			state,
			window_start,
			created_at,
			settled_at
		FROM execution_billing_windows
		WHERE attempt_id = $1
		ORDER BY window_seq`, attemptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BillingWindow{}
	for rows.Next() {
		var window BillingWindow
		if err := rows.Scan(&window.AttemptID, &window.BillingWindowID, &window.WindowSeq, &window.ReservationShape, &window.ReservedQuantity, &window.ActualQuantity, &window.ReservedChargeUnits, &window.BilledChargeUnits, &window.WriteoffChargeUnits, &window.CostPerUnit, &window.PricingPhase, &window.State, &window.WindowStart, &window.CreatedAt, &window.SettledAt); err != nil {
			return nil, err
		}
		out = append(out, window)
	}
	return out, rows.Err()
}

func (s *Service) writeExecutionLogs(ctx context.Context, record ExecutionRecord, logs string) error {
	if logs == "" {
		return nil
	}
	_, err := s.PGX.Exec(ctx, `INSERT INTO execution_logs (execution_id, attempt_id, seq, stream, chunk, created_at) VALUES ($1,$2,1,'combined',$3,$4)`, record.ExecutionID, record.LatestAttempt.AttemptID, logs, time.Now().UTC())
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
	if err := batch.AppendStruct(&jobLogRow{
		ExecutionID:        record.ExecutionID,
		AttemptID:          record.LatestAttempt.AttemptID,
		OrgID:              record.OrgID,
		SourceKind:         record.SourceKind,
		WorkloadKind:       record.WorkloadKind,
		RunnerClass:        record.RunnerClass,
		ExternalProvider:   record.ExternalProvider,
		ProductID:          record.ProductID,
		CorrelationID:      record.CorrelationID,
		RepositoryFullName: record.GitHub.RepositoryFullName,
		WorkflowName:       record.GitHub.WorkflowName,
		JobName:            record.GitHub.JobName,
		HeadBranch:         record.GitHub.HeadBranch,
		ScheduleID:         zeroUUIDString(record.Schedule.ScheduleID),
		Seq:                1,
		Stream:             "combined",
		Chunk:              logs,
		CreatedAt:          time.Now().UTC(),
	}); err != nil {
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

func jobEventRowForRun(record ExecutionRecord) jobEventRow {
	return jobEventRow{
		ExecutionID:            record.ExecutionID,
		AttemptID:              record.LatestAttempt.AttemptID,
		OrgID:                  record.OrgID,
		ActorID:                record.ActorID,
		Kind:                   record.Kind,
		SourceKind:             record.SourceKind,
		WorkloadKind:           record.WorkloadKind,
		SourceRef:              record.SourceRef,
		RunnerClass:            record.RunnerClass,
		ExternalProvider:       record.ExternalProvider,
		ExternalTaskID:         record.ExternalTaskID,
		Provider:               record.Provider,
		ProductID:              record.ProductID,
		LeaseID:                record.LatestAttempt.LeaseID,
		ExecID:                 record.LatestAttempt.ExecID,
		RepositoryFullName:     record.GitHub.RepositoryFullName,
		WorkflowName:           record.GitHub.WorkflowName,
		JobName:                record.GitHub.JobName,
		HeadBranch:             record.GitHub.HeadBranch,
		HeadSHA:                record.GitHub.HeadSHA,
		GitHubInstallationID:   int64ToUint64(record.GitHub.InstallationID),
		GitHubRunID:            int64ToUint64(record.GitHub.RunID),
		GitHubJobID:            int64ToUint64(record.GitHub.JobID),
		ScheduleID:             zeroUUIDString(record.Schedule.ScheduleID),
		ScheduleDisplayName:    record.Schedule.DisplayName,
		TemporalWorkflowID:     record.Schedule.TemporalWorkflowID,
		TemporalRunID:          record.Schedule.TemporalRunID,
		RunCommand:             record.RunCommand,
		Status:                 record.Status,
		ExitCode:               int32(record.LatestAttempt.ExitCode),
		DurationMs:             record.LatestAttempt.DurationMs,
		ZFSWritten:             int64ToUint64(record.LatestAttempt.ZFSWritten),
		StdoutBytes:            int64ToUint64(record.LatestAttempt.StdoutBytes),
		StderrBytes:            int64ToUint64(record.LatestAttempt.StderrBytes),
		BillingJobID:           record.LatestAttempt.BillingJobID,
		ReservedChargeUnits:    record.BillingSummary.ReservedChargeUnits,
		BilledChargeUnits:      record.BillingSummary.BilledChargeUnits,
		WriteoffChargeUnits:    record.BillingSummary.WriteoffChargeUnits,
		CostPerUnit:            record.BillingSummary.CostPerUnit,
		PricingPhase:           record.BillingSummary.PricingPhase,
		RootfsProvisionedBytes: int64ToUint64(record.LatestAttempt.RootfsProvisionedBytes),
		BootTimeUs:             int64ToUint64(record.LatestAttempt.BootTimeUs),
		BlockReadBytes:         int64ToUint64(record.LatestAttempt.BlockReadBytes),
		BlockWriteBytes:        int64ToUint64(record.LatestAttempt.BlockWriteBytes),
		NetRXBytes:             int64ToUint64(record.LatestAttempt.NetRXBytes),
		NetTXBytes:             int64ToUint64(record.LatestAttempt.NetTXBytes),
		VCPUExitCount:          int64ToUint64(record.LatestAttempt.VCPUExitCount),
		CorrelationID:          record.CorrelationID,
		StartedAt:              derefTime(record.LatestAttempt.StartedAt),
		CompletedAt:            derefTime(record.LatestAttempt.CompletedAt),
		CreatedAt:              time.Now().UTC(),
		TraceID:                record.LatestAttempt.TraceID,
	}
}

func int64ToUint64(value int64) uint64 {
	if value <= 0 {
		return 0
	}
	return uint64(value)
}

func zeroUUIDString(value uuid.UUID) string {
	if value == uuid.Nil {
		return ""
	}
	return value.String()
}

func derefTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}

func (s *Service) normalizeSubmitRequest(ctx context.Context, req SubmitRequest) (SubmitRequest, error) {
	req.Kind = firstNonEmpty(strings.TrimSpace(req.Kind), KindDirect)
	req.SourceKind = firstNonEmpty(strings.TrimSpace(req.SourceKind), SourceKindAPI)
	req.WorkloadKind = firstNonEmpty(strings.TrimSpace(req.WorkloadKind), WorkloadKindDirect)
	req.RunnerClass = firstNonEmpty(strings.TrimSpace(req.RunnerClass), DefaultRunnerClassLabel)
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
	classResources, classProductID, ok, err := s.runnerClassResources(ctx, req.RunnerClass)
	if err != nil {
		return SubmitRequest{}, err
	}
	if !ok {
		return SubmitRequest{}, fmt.Errorf("%w: %s", ErrRunnerClassMissing, req.RunnerClass)
	}
	req.ProductID = firstNonEmpty(strings.TrimSpace(req.ProductID), classProductID, defaultProductID)
	if req.ProductID != classProductID {
		return SubmitRequest{}, fmt.Errorf("runner_class %s belongs to product %s, got product_id %s", req.RunnerClass, classProductID, req.ProductID)
	}
	// Runner classes are product defaults. Fill omitted fields from the class
	// before bounds validation so billing, traces, and VM admission agree.
	req.Resources = vmResourcesWithDefaults(req.Resources, classResources)
	bounds := s.Bounds
	if bounds == (apiwire.VMResourceBounds{}) {
		bounds = apiwire.DefaultBounds
	}
	if err := req.Resources.Validate(bounds); err != nil {
		return SubmitRequest{}, err
	}
	return req, nil
}

func vmResourcesWithDefaults(resources, defaults apiwire.VMResources) apiwire.VMResources {
	if resources.VCPUs == 0 {
		resources.VCPUs = defaults.VCPUs
	}
	if resources.MemoryMiB == 0 {
		resources.MemoryMiB = defaults.MemoryMiB
	}
	if resources.RootDiskGiB == 0 {
		resources.RootDiskGiB = defaults.RootDiskGiB
	}
	if resources.KernelImage == "" {
		resources.KernelImage = defaults.KernelImage
	}
	return resources
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

func vmMetricUint64(metrics *vmorchestrator.VMMetrics, pick func(*vmorchestrator.VMMetrics) uint64) int64 {
	if metrics == nil || pick == nil {
		return 0
	}
	return int64(pick(metrics))
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

func (s *Service) reserveBillingWindow(ctx context.Context, request apiwire.BillingReserveWindowRequest) (apiwire.BillingWindowReservation, error) {
	windowSeq, err := billingInt32("window_seq", request.WindowSeq)
	if err != nil {
		return apiwire.BillingWindowReservation{}, err
	}
	reservedQuantity, err := billingInt32("reserved_quantity", request.ReservedQuantity)
	if err != nil {
		return apiwire.BillingWindowReservation{}, err
	}
	concurrentCount, err := int64FromUint64("concurrent_count", request.ConcurrentCount)
	if err != nil {
		return apiwire.BillingWindowReservation{}, err
	}
	billingJobID := request.BillingJobID
	resp, err := s.Billing.ReserveWindowWithResponse(ctx, billingclient.BillingReserveWindowRequest{
		ActorId:          request.ActorID,
		Allocation:       request.Allocation,
		BillingJobId:     &billingJobID,
		ConcurrentCount:  concurrentCount,
		OrgId:            request.OrgID.String(),
		ProductId:        request.ProductID,
		ReservationShape: billingclient.BillingReserveWindowRequestReservationShape(request.ReservationShape),
		ReservedQuantity: reservedQuantity,
		SourceRef:        request.SourceRef,
		SourceType:       request.SourceType,
		WindowSeq:        windowSeq,
	})
	if err != nil {
		return apiwire.BillingWindowReservation{}, fmt.Errorf("reserve billing window: %w", err)
	}
	switch resp.StatusCode() {
	case http.StatusOK:
		out, err := decodeBillingResponseBody[apiwire.BillingReserveWindowResult]("decode reserve billing window response", resp.Body)
		if err != nil {
			return apiwire.BillingWindowReservation{}, err
		}
		return out.Reservation, nil
	case http.StatusPaymentRequired:
		return apiwire.BillingWindowReservation{}, newBillingStatusError("reserve billing window", resp.StatusCode(), resp.ApplicationproblemJSON402, ErrBillingPaymentRequired)
	case http.StatusForbidden:
		return apiwire.BillingWindowReservation{}, newBillingStatusError("reserve billing window", resp.StatusCode(), resp.ApplicationproblemJSON403, ErrBillingForbidden)
	default:
		return apiwire.BillingWindowReservation{}, newBillingStatusError("reserve billing window", resp.StatusCode(), firstBillingProblem(resp.ApplicationproblemJSON422, resp.ApplicationproblemJSON500), nil)
	}
}

func (s *Service) activateBillingWindow(ctx context.Context, reservation apiwire.BillingWindowReservation, activatedAt time.Time) (apiwire.BillingWindowReservation, error) {
	resp, err := s.Billing.ActivateWindowWithResponse(ctx, billingclient.BillingActivateWindowRequest{
		ActivatedAt: activatedAt.UTC(),
		WindowId:    reservation.WindowID,
	})
	if err != nil {
		return apiwire.BillingWindowReservation{}, fmt.Errorf("activate billing window: %w", err)
	}
	switch resp.StatusCode() {
	case http.StatusOK:
		out, err := decodeBillingResponseBody[apiwire.BillingActivateWindowResult]("decode activate billing window response", resp.Body)
		if err != nil {
			return apiwire.BillingWindowReservation{}, err
		}
		return out.Reservation, nil
	default:
		return apiwire.BillingWindowReservation{}, newBillingStatusError("activate billing window", resp.StatusCode(), firstBillingProblem(resp.ApplicationproblemJSON404, resp.ApplicationproblemJSON422, resp.ApplicationproblemJSON500), nil)
	}
}

func (s *Service) settleBillingWindow(ctx context.Context, reservation apiwire.BillingWindowReservation, actualQuantity uint32, usageSummary map[string]any) (apiwire.BillingSettleResult, error) {
	actualQuantityInt, err := billingInt32("actual_quantity", actualQuantity)
	if err != nil {
		return apiwire.BillingSettleResult{}, err
	}
	req := billingclient.BillingSettleWindowRequest{
		ActualQuantity: actualQuantityInt,
		WindowId:       reservation.WindowID,
	}
	if usageSummary != nil {
		req.UsageSummary = &usageSummary
	}
	resp, err := s.Billing.SettleWindowWithResponse(ctx, req)
	if err != nil {
		return apiwire.BillingSettleResult{}, fmt.Errorf("settle billing window: %w", err)
	}
	switch resp.StatusCode() {
	case http.StatusOK:
		return decodeBillingResponseBody[apiwire.BillingSettleResult]("decode settle billing window response", resp.Body)
	default:
		return apiwire.BillingSettleResult{}, newBillingStatusError("settle billing window", resp.StatusCode(), firstBillingProblem(resp.ApplicationproblemJSON404, resp.ApplicationproblemJSON422, resp.ApplicationproblemJSON500), nil)
	}
}

func (s *Service) voidBillingWindow(ctx context.Context, reservation apiwire.BillingWindowReservation) error {
	resp, err := s.Billing.VoidWindowWithResponse(ctx, billingclient.BillingVoidWindowRequest{
		WindowId: reservation.WindowID,
	})
	if err != nil {
		return fmt.Errorf("void billing window: %w", err)
	}
	switch resp.StatusCode() {
	case http.StatusOK:
		return nil
	default:
		return newBillingStatusError("void billing window", resp.StatusCode(), firstBillingProblem(resp.ApplicationproblemJSON404, resp.ApplicationproblemJSON422, resp.ApplicationproblemJSON500), nil)
	}
}

func decodeBillingResponseBody[T any](operation string, body []byte) (T, error) {
	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("%s: %w", operation, err)
	}
	return out, nil
}

func newBillingStatusError(operation string, statusCode int, problem *billingclient.ErrorModel, cause error) error {
	detail := http.StatusText(statusCode)
	if problem != nil && problem.Detail != nil && *problem.Detail != "" {
		detail = *problem.Detail
	}
	return &billingStatusError{
		Operation:  operation,
		StatusCode: statusCode,
		Detail:     detail,
		Cause:      cause,
	}
}

func firstBillingProblem(problems ...*billingclient.ErrorModel) *billingclient.ErrorModel {
	for _, problem := range problems {
		if problem != nil {
			return problem
		}
	}
	return nil
}

func billingInt32(field string, value uint32) (int32, error) {
	// The generated client renders these wire fields as int32, so fail loudly
	// if a wider sandbox quantity would otherwise wrap during JSON encoding.
	if value > math.MaxInt32 {
		return 0, fmt.Errorf("%s exceeds int32 range", field)
	}
	return int32(value), nil
}

func int64FromUint64(field string, value uint64) (int64, error) {
	if value > math.MaxInt64 {
		return 0, fmt.Errorf("%s exceeds int64 range", field)
	}
	return int64(value), nil
}
