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
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/verself/apiwire"
	billingclient "github.com/verself/billing-service/client"
	"github.com/verself/sandbox-rental-service/internal/store"
	vmorchestrator "github.com/verself/vm-orchestrator"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/sandbox-rental-service/internal/scheduler"
)

const (
	KindDirect                  = "direct"
	KindVMSession               = "vm_session"
	SourceKindAPI               = "api"
	SourceKindExecutionSchedule = "execution_schedule"
	SourceKindGitHubAction      = "github_actions"
	SourceKindForgejoAction     = "forgejo_actions"
	SourceKindSourceHosting     = "source_code_hosting"
	SourceKindCanary            = "canary"
	SourceKindVMSession         = "vm_session"
	WorkloadKindDirect          = "direct"
	WorkloadKindRunner          = "runner"

	DefaultRunnerClassLabel      = "verself-4vcpu-ubuntu-2404"
	defaultProductID             = "sandbox"
	defaultRunCommand            = "echo hello"
	RunnerProviderGitHub         = "github"
	RunnerProviderForgejo        = "forgejo"
	RunnerBootstrapGitHubJIT     = "github_jit"
	RunnerBootstrapForgejoOneJob = "forgejo_one_job"

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
	EnqueueRunnerCapacityReconcileTx(ctx context.Context, tx pgx.Tx, req scheduler.RunnerCapacityReconcileRequest) (scheduler.ProbeResult, error)
	EnqueueRunnerAllocateTx(ctx context.Context, tx pgx.Tx, req scheduler.RunnerAllocateRequest) (scheduler.ProbeResult, error)
	EnqueueRunnerJobBindTx(ctx context.Context, tx pgx.Tx, req scheduler.RunnerJobBindRequest) (scheduler.ProbeResult, error)
	EnqueueRunnerCleanup(ctx context.Context, req scheduler.RunnerCleanupRequest) (scheduler.ProbeResult, error)
	EnqueueRunnerRepositorySyncTx(ctx context.Context, tx pgx.Tx, req scheduler.RunnerRepositorySyncRequest) (scheduler.ProbeResult, error)
	EnqueueRunnerRepositorySync(ctx context.Context, req scheduler.RunnerRepositorySyncRequest) (scheduler.ProbeResult, error)
}

type SubmitRequest struct {
	Kind                   string                           `json:"kind,omitempty"`
	RunnerClass            string                           `json:"runner_class,omitempty"`
	ProductID              string                           `json:"product_id,omitempty"`
	Provider               string                           `json:"provider,omitempty"`
	IdempotencyKey         string                           `json:"idempotency_key"`
	SourceKind             string                           `json:"source_kind,omitempty"`
	WorkloadKind           string                           `json:"workload_kind,omitempty"`
	SourceRef              string                           `json:"source_ref,omitempty"`
	ExternalProvider       string                           `json:"external_provider,omitempty"`
	ExternalTaskID         string                           `json:"external_task_id,omitempty"`
	RunCommand             string                           `json:"run_command,omitempty"`
	MaxWallSeconds         uint64                           `json:"max_wall_seconds,omitempty"`
	Resources              apiwire.VMResources              `json:"resources"`
	FilesystemMounts       []vmorchestrator.FilesystemMount `json:"-"`
	StickyDiskMounts       []StickyDiskMountSpec            `json:"-"`
	AttemptID              uuid.UUID                        `json:"-"`
	RunnerAllocationID     uuid.UUID                        `json:"-"`
	RunnerBootstrapKind    string                           `json:"-"`
	RunnerBootstrapPayload string                           `json:"-"`
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
	Runner           RunnerRunMetadata
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
	ForgejoRunner    *ForgejoRunner
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
	ProviderInstallationID uint64    `ch:"provider_installation_id"`
	ProviderRunID          uint64    `ch:"provider_run_id"`
	ProviderJobID          uint64    `ch:"provider_job_id"`
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
	qtx := store.New(tx)
	if _, err := qtx.InsertExecution(ctx, store.InsertExecutionParams{
		ExecutionID:          executionID,
		OrgID:                dbOrgID(orgID),
		ActorID:              actorID,
		Kind:                 req.Kind,
		SourceKind:           req.SourceKind,
		WorkloadKind:         req.WorkloadKind,
		SourceRef:            req.SourceRef,
		RunnerClass:          req.RunnerClass,
		ExternalProvider:     req.ExternalProvider,
		ExternalTaskID:       req.ExternalTaskID,
		Provider:             req.Provider,
		ProductID:            req.ProductID,
		State:                StateQueued,
		CorrelationID:        correlationID,
		IdempotencyKey:       req.IdempotencyKey,
		RunCommand:           req.RunCommand,
		MaxWallSeconds:       int64(req.MaxWallSeconds),
		RequestedVcpus:       int32(req.Resources.VCPUs),
		RequestedMemoryMib:   int32(req.Resources.MemoryMiB),
		RequestedRootDiskGib: int32(req.Resources.RootDiskGiB),
		RequestedKernelImage: string(req.Resources.KernelImage),
		CreatedAt:            pgTime(now),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return s.existingSubmission(ctx, orgID, req.IdempotencyKey)
		}
		return uuid.Nil, uuid.Nil, fmt.Errorf("insert execution: %w", err)
	}
	if err := qtx.InsertExecutionAttempt(ctx, store.InsertExecutionAttemptParams{
		AttemptID:   attemptID,
		ExecutionID: executionID,
		State:       StateQueued,
		CreatedAt:   pgTime(now),
	}); err != nil {
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
	if req.WorkloadKind == WorkloadKindRunner && req.RunnerAllocationID != uuid.Nil {
		if err := s.attachRunnerAllocationExecutionTx(ctx, tx, req.RunnerAllocationID, executionID, attemptID, req.RunnerBootstrapKind, req.RunnerBootstrapPayload); err != nil {
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
	row, err := s.storeQueries().GetExistingSubmission(ctx, store.GetExistingSubmissionParams{
		OrgID:          dbOrgID(orgID),
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("load existing execution: %w", err)
	}
	return row.ExecutionID, row.AttemptID, nil
}

func (s *Service) runnerClassResources(ctx context.Context, runnerClass string) (apiwire.VMResources, string, bool, error) {
	row, err := s.storeQueries().GetRunnerClassResources(ctx, store.GetRunnerClassResourcesParams{RunnerClass: runnerClass})
	if errors.Is(err, pgx.ErrNoRows) {
		return apiwire.VMResources{}, "", false, nil
	}
	if err != nil {
		return apiwire.VMResources{}, "", false, fmt.Errorf("load runner class resources: %w", err)
	}
	return apiwire.VMResources{
		VCPUs:       uint32(row.Vcpus),
		MemoryMiB:   uint32(row.MemoryMib),
		RootDiskGiB: uint32(row.RootfsGib),
		KernelImage: apiwire.KernelImageDefault,
	}, row.ProductID, true, nil
}

func (s *Service) runnerClassFilesystemMounts(ctx context.Context, tx pgx.Tx, runnerClass string) ([]vmorchestrator.FilesystemMount, error) {
	rows, err := store.New(tx).ListRunnerClassFilesystemMounts(ctx, store.ListRunnerClassFilesystemMountsParams{RunnerClass: runnerClass})
	if err != nil {
		return nil, fmt.Errorf("load runner class filesystem mounts: %w", err)
	}
	out := make([]vmorchestrator.FilesystemMount, 0, len(rows))
	for _, row := range rows {
		out = append(out, vmorchestrator.FilesystemMount{
			Name:      row.MountName,
			SourceRef: row.SourceRef,
			MountPath: row.MountPath,
			FSType:    row.FsType,
			ReadOnly:  row.ReadOnly,
		})
	}
	return out, nil
}

func (s *Service) insertExecutionFilesystemMounts(ctx context.Context, tx pgx.Tx, executionID uuid.UUID, mounts []vmorchestrator.FilesystemMount) error {
	qtx := store.New(tx)
	for idx, mount := range mounts {
		if err := qtx.InsertExecutionFilesystemMount(ctx, store.InsertExecutionFilesystemMountParams{
			ExecutionID: executionID,
			MountName:   mount.Name,
			SourceRef:   mount.SourceRef,
			MountPath:   mount.MountPath,
			FsType:      firstNonEmpty(mount.FSType, "ext4"),
			ReadOnly:    mount.ReadOnly,
			SortOrder:   int32(idx),
			CreatedAt:   pgTime(time.Now().UTC()),
		}); err != nil {
			return fmt.Errorf("insert execution filesystem mount %s: %w", mount.Name, err)
		}
	}
	return nil
}

func (s *Service) insertExecutionStickyDiskMounts(ctx context.Context, tx pgx.Tx, executionID, attemptID uuid.UUID, mounts []StickyDiskMountSpec) error {
	qtx := store.New(tx)
	for idx, mount := range mounts {
		if mount.MountID == uuid.Nil {
			mount.MountID = uuid.New()
		}
		if err := qtx.InsertExecutionStickyDiskMount(ctx, store.InsertExecutionStickyDiskMountParams{
			MountID:         mount.MountID,
			ExecutionID:     executionID,
			AttemptID:       attemptID,
			AllocationID:    mount.AllocationID,
			MountName:       mount.MountName,
			KeyHash:         mount.KeyHash,
			Key:             mount.Key,
			MountPath:       mount.MountPath,
			BaseGeneration:  mount.BaseGeneration,
			SourceRef:       mount.SourceRef,
			TargetSourceRef: mount.TargetSourceRef,
			SaveState:       stickyDiskStateNotRequested,
			SortOrder:       int32(idx),
			CreatedAt:       pgTime(time.Now().UTC()),
		}); err != nil {
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
	if item.WorkloadKind == WorkloadKindRunner && s.GitHubRunner != nil && item.SourceKind == SourceKindGitHubAction {
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
	if item.WorkloadKind == WorkloadKindRunner {
		s.MarkRunnerExecutionExited(detachedContext(ctx), item.ExecutionID)
	}
	return nil
}

func (s *Service) loadWorkItem(ctx context.Context, executionID, attemptID uuid.UUID) (executionWorkItem, error) {
	row, err := s.storeQueries().GetExecutionWorkItem(ctx, store.GetExecutionWorkItemParams{
		ExecutionID: executionID,
		AttemptID:   attemptID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return executionWorkItem{}, ErrExecutionMissing
		}
		return executionWorkItem{}, fmt.Errorf("load execution work item: %w", err)
	}
	item := executionWorkItem{
		ExecutionID:      row.ExecutionID,
		AttemptID:        row.AttemptID,
		OrgID:            orgIDFromDB(row.OrgID),
		ActorID:          row.ActorID,
		Kind:             row.Kind,
		SourceKind:       row.SourceKind,
		WorkloadKind:     row.WorkloadKind,
		SourceRef:        row.SourceRef,
		RunnerClass:      row.RunnerClass,
		ExternalProvider: row.ExternalProvider,
		ExternalTaskID:   row.ExternalTaskID,
		Provider:         row.Provider,
		ProductID:        row.ProductID,
		RunCommand:       row.RunCommand,
		MaxWallSeconds:   uint64(row.MaxWallSeconds),
		LeaseID:          row.LeaseID,
		ExecID:           row.ExecID,
		CorrelationID:    row.CorrelationID,
		Resources: apiwire.VMResources{
			VCPUs:       uint32(row.RequestedVcpus),
			MemoryMiB:   uint32(row.RequestedMemoryMib),
			RootDiskGiB: uint32(row.RequestedRootDiskGib),
			KernelImage: apiwire.KernelImageRef(row.RequestedKernelImage),
		},
	}
	mounts, err := s.loadExecutionFilesystemMounts(ctx, executionID)
	if err != nil {
		return executionWorkItem{}, err
	}
	item.FilesystemMounts = mounts
	return item, nil
}

func (s *Service) loadExecutionFilesystemMounts(ctx context.Context, executionID uuid.UUID) ([]vmorchestrator.FilesystemMount, error) {
	rows, err := s.storeQueries().ListExecutionFilesystemMounts(ctx, store.ListExecutionFilesystemMountsParams{ExecutionID: executionID})
	if err != nil {
		return nil, fmt.Errorf("load execution filesystem mounts: %w", err)
	}
	out := make([]vmorchestrator.FilesystemMount, 0, len(rows))
	for _, row := range rows {
		out = append(out, vmorchestrator.FilesystemMount{
			Name:      row.MountName,
			SourceRef: row.SourceRef,
			MountPath: row.MountPath,
			FSType:    row.FsType,
			ReadOnly:  row.ReadOnly,
		})
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
	if err := s.storeQueries().InsertExecutionBillingWindow(ctx, store.InsertExecutionBillingWindowParams{
		AttemptID:           attemptID,
		WindowSeq:           int32(reservation.WindowSeq),
		BillingWindowID:     reservation.WindowID,
		ReservationShape:    reservation.ReservationShape,
		ReservedQuantity:    int32(reservation.ReservedQuantity),
		ReservedChargeUnits: int64(reservation.ReservedChargeUnits.Uint64()),
		CostPerUnit:         int64(reservation.CostPerUnit.Uint64()),
		PricingPhase:        reservation.PricingPhase,
		WindowStart:         pgTime(reservation.WindowStart),
		CreatedAt:           pgTime(time.Now().UTC()),
		ReservationJsonb:    payload,
	}); err != nil {
		return fmt.Errorf("insert billing window: %w", err)
	}
	return nil
}

func (s *Service) markBillingWindow(ctx context.Context, attemptID uuid.UUID, windowID, state string, actual int, settled apiwire.BillingSettleResult) error {
	return s.storeQueries().MarkExecutionBillingWindow(ctx, store.MarkExecutionBillingWindowParams{
		State:               state,
		ActualQuantity:      int32(actual),
		BilledChargeUnits:   int64(settled.BilledChargeUnits.Uint64()),
		WriteoffChargeUnits: int64(settled.WriteoffChargeUnits.Uint64()),
		SettledAt:           pgTime(time.Now().UTC()),
		AttemptID:           attemptID,
		BillingWindowID:     windowID,
	})
}

func (s *Service) transition(ctx context.Context, item executionWorkItem, from, to, reason string, values map[string]any) error {
	now := time.Now().UTC()
	var billingJobID int64
	if values != nil {
		if value, ok := values["billing_job_id"].(int64); ok {
			billingJobID = value
		}
	}
	tx, err := s.PGX.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := store.New(tx)
	if err := qtx.SetExecutionState(ctx, store.SetExecutionStateParams{State: to, UpdatedAt: pgTime(now), ExecutionID: item.ExecutionID}); err != nil {
		return err
	}
	rows, err := qtx.CASAttemptState(ctx, store.CASAttemptStateParams{
		ToState:      to,
		BillingJobID: billingJobID,
		UpdatedAt:    pgTime(now),
		AttemptID:    item.AttemptID,
		FromState:    from,
	})
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("execution attempt %s is not in expected state %s", item.AttemptID, from)
	}
	if err := qtx.InsertExecutionEvent(ctx, store.InsertExecutionEventParams{
		ExecutionID: item.ExecutionID,
		AttemptID:   item.AttemptID,
		FromState:   from,
		ToState:     to,
		Reason:      reason,
		CreatedAt:   pgTime(now),
	}); err != nil {
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
	qtx := store.New(tx)
	if err := qtx.SetExecutionState(ctx, store.SetExecutionStateParams{State: StateRunning, UpdatedAt: pgTime(now), ExecutionID: item.ExecutionID}); err != nil {
		return err
	}
	rows, err := qtx.MarkAttemptRunningCAS(ctx, store.MarkAttemptRunningCASParams{
		ToState:   StateRunning,
		StartedAt: pgTime(startedAt),
		UpdatedAt: pgTime(now),
		AttemptID: item.AttemptID,
		FromState: StateLaunching,
	})
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("execution attempt %s is not in expected state %s", item.AttemptID, StateLaunching)
	}
	if err := qtx.InsertExecutionEvent(ctx, store.InsertExecutionEventParams{
		ExecutionID: item.ExecutionID,
		AttemptID:   item.AttemptID,
		FromState:   StateLaunching,
		ToState:     StateRunning,
		Reason:      "exec_started",
		CreatedAt:   pgTime(now),
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) setAttemptLeaseExec(ctx context.Context, attemptID uuid.UUID, leaseID, execID string) error {
	return s.storeQueries().SetAttemptLeaseExec(ctx, store.SetAttemptLeaseExecParams{
		LeaseID:   leaseID,
		ExecID:    execID,
		UpdatedAt: pgTime(time.Now().UTC()),
		AttemptID: attemptID,
	})
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
	qtx := store.New(tx)
	if err := qtx.SetExecutionState(ctx, store.SetExecutionStateParams{State: state, UpdatedAt: pgTime(now), ExecutionID: item.ExecutionID}); err != nil {
		return err
	}
	rows, err := qtx.CompleteAttemptCAS(ctx, store.CompleteAttemptCASParams{
		State:                  state,
		FailureReason:          reason,
		ExitCode:               int32(exec.ExitCode),
		DurationMs:             durationMs,
		ZfsWritten:             int64(exec.ZFSWritten),
		StdoutBytes:            int64(exec.StdoutBytes),
		StderrBytes:            int64(exec.StderrBytes),
		RootfsProvisionedBytes: int64(exec.RootfsProvisionedBytes),
		BootTimeUs:             vmMetricUint64(metrics, func(m *vmorchestrator.VMMetrics) uint64 { return m.BootTimeUs }),
		BlockReadBytes:         vmMetricUint64(metrics, func(m *vmorchestrator.VMMetrics) uint64 { return m.BlockReadBytes }),
		BlockWriteBytes:        vmMetricUint64(metrics, func(m *vmorchestrator.VMMetrics) uint64 { return m.BlockWriteBytes }),
		NetRxBytes:             vmMetricUint64(metrics, func(m *vmorchestrator.VMMetrics) uint64 { return m.NetRxBytes }),
		NetTxBytes:             vmMetricUint64(metrics, func(m *vmorchestrator.VMMetrics) uint64 { return m.NetTxBytes }),
		VcpuExitCount:          vmMetricUint64(metrics, func(m *vmorchestrator.VMMetrics) uint64 { return m.VCPUExitCount }),
		TraceID:                traceID,
		CompletedAt:            pgTime(completedAt),
		UpdatedAt:              pgTime(now),
		AttemptID:              item.AttemptID,
		FromState:              StateRunning,
	})
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("execution attempt %s is not in expected state %s", item.AttemptID, StateRunning)
	}
	if err := qtx.InsertExecutionEvent(ctx, store.InsertExecutionEventParams{
		ExecutionID: item.ExecutionID,
		AttemptID:   item.AttemptID,
		FromState:   StateRunning,
		ToState:     StateFinalizing,
		Reason:      "exec_finished",
		CreatedAt:   pgTime(now),
	}); err != nil {
		return err
	}
	if err := qtx.InsertExecutionEvent(ctx, store.InsertExecutionEventParams{
		ExecutionID: item.ExecutionID,
		AttemptID:   item.AttemptID,
		FromState:   StateFinalizing,
		ToState:     state,
		Reason:      reason,
		CreatedAt:   pgTime(now),
	}); err != nil {
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
	qtx := store.New(tx)
	if err := qtx.SetExecutionState(ctx, store.SetExecutionStateParams{State: StateFailed, UpdatedAt: pgTime(now), ExecutionID: item.ExecutionID}); err != nil {
		return err
	}
	if err := qtx.MarkAttemptFailed(ctx, store.MarkAttemptFailedParams{
		State:         StateFailed,
		FailureReason: reason,
		TraceID:       traceID,
		CompletedAt:   pgTime(now),
		AttemptID:     item.AttemptID,
	}); err != nil {
		return err
	}
	if err := qtx.InsertExecutionEvent(ctx, store.InsertExecutionEventParams{
		ExecutionID: item.ExecutionID,
		AttemptID:   item.AttemptID,
		FromState:   "",
		ToState:     StateFailed,
		Reason:      reason,
		CreatedAt:   pgTime(now),
	}); err != nil {
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
	attemptID, err := s.storeQueries().GetLatestAttemptForExecution(ctx, store.GetLatestAttemptForExecutionParams{
		OrgID:       dbOrgID(orgID),
		ExecutionID: executionID,
	})
	if err != nil {
		return uuid.Nil, "", ErrExecutionMissing
	}
	chunks, err := s.storeQueries().ListExecutionLogChunks(ctx, store.ListExecutionLogChunksParams{AttemptID: attemptID})
	if err != nil {
		return uuid.Nil, "", err
	}
	var builder strings.Builder
	for _, chunk := range chunks {
		builder.WriteString(chunk)
	}
	return attemptID, builder.String(), nil
}

func (s *Service) listBillingWindows(ctx context.Context, attemptID uuid.UUID) ([]BillingWindow, error) {
	rows, err := s.storeQueries().ListExecutionBillingWindows(ctx, store.ListExecutionBillingWindowsParams{AttemptID: attemptID})
	if err != nil {
		return nil, err
	}
	out := make([]BillingWindow, 0, len(rows))
	for _, row := range rows {
		out = append(out, BillingWindow{
			AttemptID:           row.AttemptID,
			BillingWindowID:     row.BillingWindowID,
			WindowSeq:           int(row.WindowSeq),
			ReservationShape:    row.ReservationShape,
			ReservedQuantity:    int(row.ReservedQuantity),
			ActualQuantity:      int(row.ActualQuantity),
			ReservedChargeUnits: uint64(row.ReservedChargeUnits),
			BilledChargeUnits:   uint64(row.BilledChargeUnits),
			WriteoffChargeUnits: uint64(row.WriteoffChargeUnits),
			CostPerUnit:         uint64(row.CostPerUnit),
			PricingPhase:        row.PricingPhase,
			State:               row.State,
			WindowStart:         timeFromPG(row.WindowStart),
			CreatedAt:           timeFromPG(row.CreatedAt),
			SettledAt:           timePtrFromPG(row.SettledAt),
		})
	}
	return out, nil
}

func (s *Service) writeExecutionLogs(ctx context.Context, record ExecutionRecord, logs string) error {
	if logs == "" {
		return nil
	}
	if err := s.storeQueries().InsertExecutionLog(ctx, store.InsertExecutionLogParams{
		ExecutionID: record.ExecutionID,
		AttemptID:   record.LatestAttempt.AttemptID,
		Chunk:       logs,
		CreatedAt:   pgTime(time.Now().UTC()),
	}); err != nil {
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
		RepositoryFullName: record.Runner.RepositoryFullName,
		WorkflowName:       record.Runner.WorkflowName,
		JobName:            record.Runner.JobName,
		HeadBranch:         record.Runner.HeadBranch,
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
		RepositoryFullName:     record.Runner.RepositoryFullName,
		WorkflowName:           record.Runner.WorkflowName,
		JobName:                record.Runner.JobName,
		HeadBranch:             record.Runner.HeadBranch,
		HeadSHA:                record.Runner.HeadSHA,
		ProviderInstallationID: int64ToUint64(record.Runner.ProviderInstallationID),
		ProviderRunID:          int64ToUint64(record.Runner.ProviderRunID),
		ProviderJobID:          int64ToUint64(record.Runner.ProviderJobID),
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
	case WorkloadKindDirect, WorkloadKindRunner:
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
		"VERSELF_EXECUTION_ID": item.ExecutionID.String(),
		"VERSELF_ATTEMPT_ID":   item.AttemptID.String(),
		"VERSELF_RUNNER_CLASS": item.RunnerClass,
		"VERSELF_SOURCE_KIND":  item.SourceKind,
	}
	if item.WorkloadKind == WorkloadKindRunner {
		for key, value := range s.runnerExecEnv(ctx, item.ExecutionID, item.AttemptID) {
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
