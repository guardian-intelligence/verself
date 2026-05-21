package jobs

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	billingclient "github.com/verself/billing-service/client"
	"github.com/verself/sandbox-rental-service/internal/store"
	secretsclient "github.com/verself/secrets-service/client"
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
	billingMaxJSONSafePositiveInt       = uint64(1<<53 - 1)

	StateQueued     = "queued"
	StateReserved   = "reserved"
	StateLaunching  = "launching"
	StateRunning    = "running"
	StateFinalizing = "finalizing"
	StateSucceeded  = "succeeded"
	StateFailed     = "failed"
	StateCanceled   = "canceled"
	StateLost       = "lost"

	leaseTTLGraceSeconds = 10 * 60
	leaseAcquireTimeout  = 500 * time.Millisecond
	leaseReadyTimeout    = 45 * time.Second
	execStartTimeout     = 2 * time.Second

	runnerAllocateDeadline   = 2 * time.Second
	runnerBootstrapDeadline  = 5 * time.Second
	runnerSubmitDeadline     = 7 * time.Second
	runnerListeningDeadline  = 60 * time.Second
	runnerAssignmentDeadline = 90 * time.Second

	githubWorkflowJobTerminalWait = 2 * time.Minute
	githubWorkflowJobTerminalPoll = 2 * time.Second
)

var (
	ErrQuotaExceeded          = errors.New("sandbox-rental: quota exceeded")
	ErrExecutionMissing       = errors.New("sandbox-rental: execution missing")
	ErrRunnerUnavailable      = errors.New("sandbox-rental: runner unavailable")
	ErrRunnerClassMissing     = errors.New("sandbox-rental: runner class missing")
	ErrBillingPaymentRequired = errors.New("sandbox-rental: billing payment required")
	ErrBillingForbidden       = errors.New("sandbox-rental: billing forbidden")
	ErrDurableCacheInvalid    = errors.New("sandbox-rental: durable cache invalid")
	ErrDurableCacheMissing    = errors.New("sandbox-rental: durable cache missing")
	ErrDurableCacheBusy       = errors.New("sandbox-rental: durable cache busy")
)

var tracer = otel.Tracer("sandbox-rental-service/jobs")

type Runner interface {
	GetCapacity(ctx context.Context) (vmorchestrator.Capacity, error)
	EnsureOrgRuntime(ctx context.Context, spec vmorchestrator.OrgRuntimeShape) (vmorchestrator.OrgRuntimeStatus, error)
	AcquireLease(ctx context.Context, key string, spec vmorchestrator.LeaseSpec) (vmorchestrator.LeaseRecord, error)
	RenewLease(ctx context.Context, leaseID, key string, extendSeconds uint64, allowlist []string) (time.Time, error)
	GetLease(ctx context.Context, leaseID string) (vmorchestrator.LeaseRecord, error)
	ReleaseLease(ctx context.Context, leaseID, key string) error
	StartExec(ctx context.Context, leaseID, key string, spec vmorchestrator.ExecSpec) (vmorchestrator.ExecRecord, error)
	GetExec(ctx context.Context, leaseID, execID string, includeOutput bool) (vmorchestrator.ExecRecord, error)
	WaitExec(ctx context.Context, leaseID, execID string, includeOutput bool) (vmorchestrator.ExecRecord, error)
	CancelExec(ctx context.Context, leaseID, execID, key, reason string) (bool, error)
	CheckpointGoldenVM(ctx context.Context, leaseID, key, operationID, checkpointID string) (vmorchestrator.GoldenVMCheckpointRecord, error)
	PruneGoldenVMSnapshot(ctx context.Context, key, operationID, snapshotID, snapshotKey, rootSnapshotRef, orgID string) (vmorchestrator.GoldenVMPruneRecord, error)
	CommitFilesystemMount(ctx context.Context, leaseID, key, operationID, mountName, scopeID, parentSnapshotRef, newGenerationName string) (vmorchestrator.FilesystemCommitRecord, error)
	PruneFilesystemGeneration(ctx context.Context, key, operationID, durableGenerationID, scopeID, snapshotRef, orgID string) (vmorchestrator.FilesystemPruneRecord, error)
}

type SchedulerRuntime interface {
	EnqueueExecutionAdvanceTx(ctx context.Context, tx pgx.Tx, req scheduler.ExecutionAdvanceRequest) (scheduler.ExecutionAdvanceResult, error)
	EnqueueRunnerCapacityReconcileTx(ctx context.Context, tx pgx.Tx, req scheduler.RunnerCapacityReconcileRequest) (scheduler.ProbeResult, error)
	EnqueueRunnerAllocateTx(ctx context.Context, tx pgx.Tx, req scheduler.RunnerAllocateRequest) (scheduler.ProbeResult, error)
	EnqueueRunnerJobBindTx(ctx context.Context, tx pgx.Tx, req scheduler.RunnerJobBindRequest) (scheduler.ProbeResult, error)
	EnqueueRunnerCleanup(ctx context.Context, req scheduler.RunnerCleanupRequest) (scheduler.ProbeResult, error)
	EnqueueRunnerRepositorySyncTx(ctx context.Context, tx pgx.Tx, req scheduler.RunnerRepositorySyncRequest) (scheduler.ProbeResult, error)
	EnqueueRunnerRepositorySync(ctx context.Context, req scheduler.RunnerRepositorySyncRequest) (scheduler.ProbeResult, error)
	EnqueueGoldenVMCreateTx(ctx context.Context, tx pgx.Tx, req scheduler.GoldenVMCreateRequest) (scheduler.ProbeResult, error)
	EnqueueGoldenVMCreate(ctx context.Context, req scheduler.GoldenVMCreateRequest) (scheduler.ProbeResult, error)
	EnqueueGoldenRunPromoteTx(ctx context.Context, tx pgx.Tx, req scheduler.GoldenRunPromoteRequest) (scheduler.ProbeResult, error)
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
	Resources              VMResources                      `json:"resources"`
	FilesystemMounts       []vmorchestrator.FilesystemMount `json:"-"`
	AttemptID              uuid.UUID                        `json:"-"`
	RunnerAllocationID     uuid.UUID                        `json:"-"`
	RunnerBootstrapKind    string                           `json:"-"`
	RunnerBootstrapPayload string                           `json:"-"`
}

type ExecutionRecord struct {
	RunID            uuid.UUID
	ExecutionID      uuid.UUID
	OrgID            string
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
	PGX                    *pgxpool.Pool
	CH                     driver.Conn
	CHDatabase             string
	Orchestrator           Runner
	Billing                *billingclient.Client
	Secrets                *secretsclient.Client
	Bounds                 VMResourceBounds
	GitHubRunner           *GitHubRunner
	ForgejoRunner          *ForgejoRunner
	Scheduler              SchedulerRuntime
	Logger                 *slog.Logger
	WorkloadTimeout        time.Duration
	CheckoutBundleStoreDir string
}

type executionWorkItem struct {
	ExecutionID      uuid.UUID
	AttemptID        uuid.UUID
	OrgID            string
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
	AttemptState     string
	StartedAt        *time.Time
	LeaseID          string
	ExecID           string
	CorrelationID    string
	Resources        VMResources
	FilesystemMounts []vmorchestrator.FilesystemMount
}

type jobEventRow struct {
	ExecutionID            uuid.UUID `ch:"execution_id"`
	AttemptID              uuid.UUID `ch:"attempt_id"`
	OrgID                  string    `ch:"org_id"`
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
	OrgID              string    `ch:"org_id"`
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

func (s *Service) Submit(ctx context.Context, orgID string, actorID string, req SubmitRequest) (executionID uuid.UUID, attemptID uuid.UUID, err error) {
	phaseStarted := time.Now().UTC()
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
		MaxWallSeconds:       mustInt64FromUint64(req.MaxWallSeconds, "max wall seconds"),
		RequestedVcpus:       int32FromUint32(req.Resources.VCPUs, "requested vcpus"),
		RequestedMemoryMib:   int32FromUint32(req.Resources.MemoryMiB, "requested memory mib"),
		RequestedRootDiskGib: int32FromUint32(req.Resources.RootDiskGiB, "requested root disk gib"),
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
	if err := s.insertExecutionFilesystemMounts(ctx, tx, executionID, mounts); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	bootstrapSecretName := ""
	if req.WorkloadKind == WorkloadKindRunner && req.RunnerAllocationID != uuid.Nil {
		bootstrapSecretName, err = s.attachRunnerAllocationExecutionTx(ctx, tx, req.RunnerAllocationID, executionID, attemptID, req.RunnerBootstrapKind, req.RunnerBootstrapPayload)
		if err != nil {
			return uuid.Nil, uuid.Nil, err
		}
		defer func() {
			if err != nil && bootstrapSecretName != "" {
				_ = s.deleteRunnerBootstrapSecret(context.Background(), bootstrapSecretName, "submit-failed:"+attemptID.String())
			}
		}()
	}
	if err := s.enqueueExecutionAdvance(ctx, tx, executionID, attemptID, orgID, actorID, correlationID); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("commit submit: %w", err)
	}
	span.SetAttributes(attribute.String("execution.id", executionID.String()), attribute.String("attempt.id", attemptID.String()))
	phaseRow := sandboxPhaseBase(ctx, "sandbox-rental", "sandbox.execution.submit", "succeeded", "", phaseStarted, time.Now().UTC(), sandboxPhaseAttrs{
		"workload_kind": req.WorkloadKind,
		"source_kind":   req.SourceKind,
	})
	phaseRow.OrgID = orgID
	phaseRow.ExecutionID = executionID
	phaseRow.AttemptID = attemptID
	phaseRow.Provider = req.Provider
	phaseRow.ExternalProvider = req.ExternalProvider
	phaseRow.ProviderJobID = parseUint64Decimal(req.ExternalTaskID)
	phaseRow.RunnerClass = req.RunnerClass
	phaseRow.CorrelationID = correlationID
	go s.writeSandboxPhaseEvent(detachedContext(ctx), phaseRow)
	_ = s.writeJobEvent(context.Background(), jobEventRow{
		ExecutionID: executionID, AttemptID: attemptID, OrgID: orgID, ActorID: actorID,
		Kind: req.Kind, SourceKind: req.SourceKind, WorkloadKind: req.WorkloadKind, RunnerClass: req.RunnerClass,
		ExternalProvider: req.ExternalProvider, ExternalTaskID: req.ExternalTaskID, Provider: req.Provider, ProductID: req.ProductID,
		RunCommand: req.RunCommand, Status: StateQueued, CorrelationID: correlationID, CreatedAt: now,
	})
	return executionID, attemptID, nil
}

func (s *Service) enqueueExecutionAdvance(ctx context.Context, tx pgx.Tx, executionID, attemptID uuid.UUID, orgID string, actorID, correlationID string) error {
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

func (s *Service) existingSubmission(ctx context.Context, orgID string, idempotencyKey string) (uuid.UUID, uuid.UUID, error) {
	row, err := s.storeQueries().GetExistingSubmission(ctx, store.GetExistingSubmissionParams{
		OrgID:          dbOrgID(orgID),
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("load existing execution: %w", err)
	}
	return row.ExecutionID, row.AttemptID, nil
}

type runnerClassRecord struct {
	Resources VMResources
	ProductID string
}

func (s *Service) runnerClassResources(ctx context.Context, runnerClass string) (runnerClassRecord, bool, error) {
	row, err := s.storeQueries().GetRunnerClassResources(ctx, store.GetRunnerClassResourcesParams{RunnerClass: runnerClass})
	if errors.Is(err, pgx.ErrNoRows) {
		return runnerClassRecord{}, false, nil
	}
	if err != nil {
		return runnerClassRecord{}, false, fmt.Errorf("load runner class resources: %w", err)
	}
	return runnerClassRecord{
		Resources: VMResources{
			VCPUs:       uint32FromInt32(row.Vcpus, "runner class vcpus"),
			MemoryMiB:   uint32FromInt32(row.MemoryMib, "runner class memory mib"),
			RootDiskGiB: uint32FromInt32(row.RootfsGib, "runner class root disk gib"),
			KernelImage: KernelImageDefault,
		},
		ProductID: row.ProductID,
	}, true, nil
}

func (s *Service) runnerClassFilesystemMounts(ctx context.Context, tx pgx.Tx, runnerClass string) ([]vmorchestrator.FilesystemMount, error) {
	return s.runnerClassFilesystemMountsFromQueries(ctx, store.New(tx), runnerClass)
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

func (s *Service) AdvanceExecution(ctx context.Context, executionID, attemptID uuid.UUID) error {
	ctx, span := tracer.Start(ctx, "sandbox-rental.execution.run", trace.WithAttributes(
		attribute.String("execution.id", executionID.String()),
		attribute.String("attempt.id", attemptID.String()),
	))
	defer span.End()
	loadStarted := time.Now().UTC()
	item, err := s.loadWorkItem(ctx, executionID, attemptID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	ctx = WithCorrelationID(ctx, item.CorrelationID)
	s.recordExecutionPhase(ctx, item, "sandbox-rental", "sandbox.execution.load_work", "succeeded", "", loadStarted, time.Now().UTC(), nil)
	switch item.AttemptState {
	case StateQueued:
	case StateRunning:
		return s.resumeRunningExecution(ctx, span, item)
	case StateSucceeded, StateFailed, StateCanceled, StateLost:
		return nil
	default:
		return fmt.Errorf("execution attempt %s cannot advance from state %s", item.AttemptID, item.AttemptState)
	}
	billingJobID := billingJobIDForAttempt(item.AttemptID)
	if err := s.transition(ctx, item, StateQueued, StateReserved, "reserved", map[string]any{"billing_job_id": billingJobID}); err != nil {
		return err
	}
	reserveStarted := time.Now().UTC()
	reservation, err := s.reserveBilling(ctx, item, billingJobID)
	reserveCompleted := time.Now().UTC()
	result, reason := sandboxPhaseResult(err)
	s.recordExecutionPhase(ctx, item, "sandbox-rental", "sandbox.billing.reserve", result, reason, reserveStarted, reserveCompleted, sandboxPhaseAttrs{
		"billing_job_id": strconv.FormatInt(billingJobID, 10),
	})
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
	durableStarted := time.Now().UTC()
	durablePlan, err := s.prepareDurableCaches(ctx, item)
	durableCompleted := time.Now().UTC()
	result, reason = sandboxPhaseResult(err)
	s.recordExecutionPhase(ctx, item, "sandbox-rental", "sandbox.durable.prepare", result, reason, durableStarted, durableCompleted, nil)
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(detachedContext(ctx), 5*time.Second)
		defer cancel()
		_ = s.voidBillingWindow(cleanupCtx, reservation)
		_ = s.markBillingWindow(ctx, item.AttemptID, reservation.WindowID, "voided", 0, billingclient.BillingSettleResult{})
		return s.failAttempt(ctx, item, "durable_cache_prepare_failed", err)
	}
	if durablePlan.Enabled {
		item.FilesystemMounts = append(item.FilesystemMounts, durablePlan.filesystemMounts()...)
	}
	quotaStarted := time.Now().UTC()
	storageQuotaBytes, err := s.durableStorageQuotaBytes(ctx, item)
	quotaCompleted := time.Now().UTC()
	result, reason = sandboxPhaseResult(err)
	s.recordExecutionPhase(ctx, item, "sandbox-rental", "sandbox.durable.storage_quota", result, reason, quotaStarted, quotaCompleted, nil)
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(detachedContext(ctx), 5*time.Second)
		defer cancel()
		_ = s.voidBillingWindow(cleanupCtx, reservation)
		_ = s.markBillingWindow(ctx, item.AttemptID, reservation.WindowID, "voided", 0, billingclient.BillingSettleResult{})
		s.failDurableCaches(ctx, durablePlan, "durable_storage_entitlement_failed", err)
		return s.failAttempt(ctx, item, "durable_storage_entitlement_failed", err)
	}
	acquireCtx, cancelAcquire := context.WithTimeout(ctx, leaseAcquireTimeout)
	leaseStarted := time.Now().UTC()
	lease, err := s.Orchestrator.AcquireLease(acquireCtx, item.AttemptID.String()+":lease", vmorchestrator.LeaseSpec{
		Resources:   vmResourcesForLease(item.Resources),
		TTLSeconds:  leaseTTLSeconds(item, s.WorkloadTimeout),
		TrustClass:  "trusted",
		NetworkMode: "nat",
		StorageNamespace: vmorchestrator.StorageNamespace{
			OrgID:      item.OrgID,
			QuotaBytes: storageQuotaBytes,
		},
		FilesystemMounts: item.FilesystemMounts,
		GoldenVM:         durablePlan.GoldenVM.Activation,
	})
	leaseCompleted := time.Now().UTC()
	cancelAcquire()
	if err == nil {
		item.LeaseID = lease.LeaseID
	}
	result, reason = sandboxPhaseResult(err)
	s.recordExecutionPhase(ctx, item, "sandbox-rental", "vm.lease.acquire", result, reason, leaseStarted, leaseCompleted, sandboxPhaseAttrs{
		"activation_mode": string(lease.Activation.Mode),
		"snapshot_key":    lease.Activation.SnapshotKey,
	})
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(detachedContext(ctx), 5*time.Second)
		defer cancel()
		_ = s.voidBillingWindow(cleanupCtx, reservation)
		_ = s.markBillingWindow(ctx, item.AttemptID, reservation.WindowID, "voided", 0, billingclient.BillingSettleResult{})
		reason := "lease_acquire_failed"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			reason = "lease_acquire_timeout"
		}
		s.failDurableCaches(ctx, durablePlan, reason, err)
		return s.failAttempt(ctx, item, reason, err)
	}
	_ = s.setAttemptLeaseExec(ctx, item.AttemptID, lease.LeaseID, "")
	if lease.State != vmorchestrator.LeaseStateReady {
		readyStarted := time.Now().UTC()
		lease, err = s.waitLeaseReady(ctx, lease.LeaseID, leaseReadyTimeout)
		readyCompleted := time.Now().UTC()
		result, reason = sandboxPhaseResult(err)
		s.recordExecutionPhase(ctx, item, "sandbox-rental", "vm.lease.ready_wait", result, reason, readyStarted, readyCompleted, nil)
		if err != nil {
			cleanupCtx, cancel := context.WithTimeout(detachedContext(ctx), 5*time.Second)
			defer cancel()
			_ = s.Orchestrator.ReleaseLease(cleanupCtx, item.LeaseID, item.AttemptID.String()+":ready-timeout-release")
			_ = s.voidBillingWindow(cleanupCtx, reservation)
			_ = s.markBillingWindow(ctx, item.AttemptID, reservation.WindowID, "voided", 0, billingclient.BillingSettleResult{})
			reason := "lease_ready_failed"
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				reason = "lease_ready_timeout"
			}
			s.failDurableCaches(ctx, durablePlan, reason, err)
			return s.failAttempt(ctx, item, reason, err)
		}
	}
	span.SetAttributes(
		attribute.Int("filesystem.requested_mount_count", len(item.FilesystemMounts)),
		attribute.Int("filesystem.result_count", len(lease.FilesystemMounts)),
		attribute.String("golden_vm.activation_mode", string(lease.Activation.Mode)),
		attribute.String("golden_vm.miss_reason", lease.Activation.MissReason),
	)
	if durablePlan.GoldenVM.Enabled {
		restoreEvent := durablePlan.GoldenVM.event(item.ExecutionID, item.AttemptID, durablePlan.Identity, goldenVMEventRestore, "succeeded", lease.Activation.MissReason)
		restoreEvent.ActivationMode = string(lease.Activation.Mode)
		restoreEvent.SnapshotKey = firstNonEmpty(lease.Activation.SnapshotKey, durablePlan.GoldenVM.Activation.SnapshotKey)
		_ = s.appendGoldenVMEvent(ctx, restoreEvent)
	}
	if len(item.FilesystemMounts) > 0 && len(lease.FilesystemMounts) == 0 && s.Logger != nil {
		s.Logger.WarnContext(ctx, "lease returned no filesystem mount results",
			"execution_id", item.ExecutionID,
			"attempt_id", item.AttemptID,
			"lease_id", lease.LeaseID,
			"requested_mount_count", len(item.FilesystemMounts),
		)
	}
	mountResultsStarted := time.Now().UTC()
	durablePlan, err = s.recordDurableLeaseMountResults(ctx, durablePlan, lease.FilesystemMounts)
	mountResultsCompleted := time.Now().UTC()
	result, reason = sandboxPhaseResult(err)
	s.recordExecutionPhase(ctx, item, "sandbox-rental", "sandbox.durable.mount_results", result, reason, mountResultsStarted, mountResultsCompleted, sandboxPhaseAttrs{
		"mount_result_count": strconv.Itoa(len(lease.FilesystemMounts)),
	})
	if err != nil {
		s.cleanupLeaseAndReservation(ctx, lease.LeaseID, reservation)
		s.failDurableCaches(ctx, durablePlan, "required_durable_mount_failed", err)
		return s.failAttempt(ctx, item, "required_durable_mount_failed", err)
	}

	execSpec := vmorchestrator.ExecSpec{
		Argv:           []string{"sh", "-c", item.RunCommand},
		WorkingDir:     "/workspace",
		Env:            s.executionEnv(ctx, item),
		MaxWallSeconds: maxWallSeconds(item, s.WorkloadTimeout),
	}
	execCtx, cancelExec := context.WithTimeout(ctx, execStartTimeout)
	execStartStarted := time.Now().UTC()
	execRecord, err := s.Orchestrator.StartExec(execCtx, lease.LeaseID, item.AttemptID.String()+":exec", execSpec)
	execStartCompleted := time.Now().UTC()
	cancelExec()
	if err == nil {
		item.ExecID = execRecord.ExecID
	}
	result, reason = sandboxPhaseResult(err)
	s.recordExecutionPhase(ctx, item, "sandbox-rental", "vm.exec.start", result, reason, execStartStarted, execStartCompleted, nil)
	if err != nil {
		s.cleanupLeaseAndReservation(ctx, lease.LeaseID, reservation)
		s.failDurableCaches(ctx, durablePlan, "exec_start_failed", err)
		return s.failAttempt(ctx, item, "exec_start_failed", err)
	}
	_ = s.setAttemptLeaseExec(ctx, item.AttemptID, lease.LeaseID, execRecord.ExecID)
	activateStarted := time.Now().UTC()
	activated, err := s.activateBillingWindow(ctx, reservation, execRecord.StartedAt)
	activateCompleted := time.Now().UTC()
	result, reason = sandboxPhaseResult(err)
	s.recordExecutionPhase(ctx, item, "sandbox-rental", "sandbox.billing.activate", result, reason, activateStarted, activateCompleted, sandboxPhaseAttrs{
		"billing_window_id": reservation.WindowID,
	})
	if err != nil {
		_, _ = s.Orchestrator.CancelExec(detachedContext(ctx), lease.LeaseID, execRecord.ExecID, item.AttemptID.String()+":cancel", "billing_activate_failed")
		s.cleanupLeaseAndReservation(ctx, lease.LeaseID, reservation)
		s.failDurableCaches(ctx, durablePlan, "billing_activate_failed", err)
		return s.failAttempt(ctx, item, "billing_activate_failed", err)
	}
	reservation = activated
	if err := s.markRunning(ctx, item, execRecord.StartedAt); err != nil {
		return err
	}
	item.AttemptState = StateRunning
	item.StartedAt = &execRecord.StartedAt
	return s.waitForExecutionAndFinalize(ctx, span, item, lease.LeaseID, execRecord, reservation, durablePlan)
}

func (s *Service) resumeRunningExecution(ctx context.Context, span trace.Span, item executionWorkItem) error {
	if item.LeaseID == "" || item.ExecID == "" {
		return fmt.Errorf("execution attempt %s is running without lease or exec identity", item.AttemptID)
	}
	execRecord, err := s.Orchestrator.GetExec(ctx, item.LeaseID, item.ExecID, false)
	if err != nil {
		return fmt.Errorf("resume running execution get exec: %w", err)
	}
	if execRecord.StartedAt.IsZero() && item.StartedAt != nil {
		execRecord.StartedAt = *item.StartedAt
	}
	reservation, err := s.latestBillingReservation(ctx, item)
	if err != nil {
		return err
	}
	return s.waitForExecutionAndFinalize(ctx, span, item, item.LeaseID, execRecord, reservation, durableCachePlan{})
}

func (s *Service) waitLeaseReady(ctx context.Context, leaseID string, timeout time.Duration) (vmorchestrator.LeaseRecord, error) {
	if s.Orchestrator == nil {
		return vmorchestrator.LeaseRecord{}, ErrRunnerUnavailable
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		lease, err := s.Orchestrator.GetLease(waitCtx, leaseID)
		if err != nil {
			return vmorchestrator.LeaseRecord{}, fmt.Errorf("get lease while waiting for readiness: %w", err)
		}
		switch lease.State {
		case vmorchestrator.LeaseStateReady:
			return lease, nil
		case vmorchestrator.LeaseStateAcquiring:
		case vmorchestrator.LeaseStateReleased, vmorchestrator.LeaseStateExpired, vmorchestrator.LeaseStateCrashed:
			if lease.TerminalReason != "" {
				return vmorchestrator.LeaseRecord{}, fmt.Errorf("lease %s reached terminal state before ready: %s", leaseID, lease.TerminalReason)
			}
			return vmorchestrator.LeaseRecord{}, fmt.Errorf("lease %s reached terminal state before ready", leaseID)
		default:
			return vmorchestrator.LeaseRecord{}, fmt.Errorf("lease %s returned unexpected state", leaseID)
		}
		select {
		case <-waitCtx.Done():
			return vmorchestrator.LeaseRecord{}, waitCtx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) waitForExecutionAndFinalize(ctx context.Context, span trace.Span, item executionWorkItem, leaseID string, execRecord vmorchestrator.ExecRecord, reservation billingclient.BillingWindowReservation, durablePlan durableCachePlan) error {
	renewCtx, stopRenew := context.WithCancel(detachedContext(ctx))
	defer stopRenew()
	go s.renewLeaseLoop(renewCtx, leaseID, item.AttemptID.String())
	waitCtx := ctx
	if timeout := workloadTimeout(item, s.WorkloadTimeout); timeout > 0 {
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	waitStarted := time.Now().UTC()
	finalExec, waitErr := s.Orchestrator.WaitExec(waitCtx, leaseID, execRecord.ExecID, true)
	waitCompleted := time.Now().UTC()
	result, reason := sandboxPhaseResult(waitErr)
	s.recordExecutionPhase(ctx, item, "sandbox-rental", "vm.exec.wait", result, reason, waitStarted, waitCompleted, nil)
	if waitErr != nil {
		span.RecordError(waitErr)
		span.SetStatus(codes.Error, waitErr.Error())
		if ctx.Err() != nil {
			if s.Logger != nil {
				s.Logger.WarnContext(ctx, "execution wait interrupted by worker context",
					"execution_id", item.ExecutionID,
					"attempt_id", item.AttemptID,
					"lease_id", leaseID,
					"exec_id", execRecord.ExecID,
					"error", waitErr,
				)
			}
			return fmt.Errorf("execution wait interrupted: %w", waitErr)
		}
		terminalCtx := detachedContext(ctx)
		_, _ = s.Orchestrator.CancelExec(terminalCtx, leaseID, execRecord.ExecID, item.AttemptID.String()+":timeout", "execution_wait_failed")
		s.failDurableCaches(terminalCtx, durablePlan, "exec_wait_failed", waitErr)
		failErr := s.failAttempt(terminalCtx, item, "exec_wait_failed", waitErr)
		s.cleanupLeaseAndReservation(terminalCtx, leaseID, reservation)
		_ = s.markBillingWindow(terminalCtx, item.AttemptID, reservation.WindowID, "voided", 0, billingclient.BillingSettleResult{})
		if failErr != nil {
			return failErr
		}
		return nil
	}
	if item.AttemptState == StateRunning {
		if err := s.transition(ctx, item, StateRunning, StateFinalizing, "exec_finished", nil); err != nil {
			return err
		}
	}
	outcome, outcomeErr := s.executionTerminalOutcome(ctx, item, finalExec)
	if outcomeErr != nil {
		span.RecordError(outcomeErr)
		if s.Logger != nil {
			s.Logger.WarnContext(ctx, "execution outcome verification failed", "execution_id", item.ExecutionID, "attempt_id", item.AttemptID, "error", outcomeErr)
		}
	}
	goldenVMCreateQueued := false
	var durableErr error
	if shouldQueueGoldenVMCreate(durablePlan, outcome.SealDecision) {
		enqueueStarted := time.Now().UTC()
		goldenVMCreateQueued, durableErr = s.enqueueGoldenVMCreate(ctx, item, leaseID, execRecord.ExecID, durablePlan)
		result, reason = sandboxPhaseResult(durableErr)
		s.recordExecutionPhase(ctx, item, "sandbox-rental", "golden.vm.create.enqueue", result, reason, enqueueStarted, time.Now().UTC(), nil)
		if durableErr != nil {
			s.failGoldenVMCreateEnqueue(ctx, item, durablePlan, durableErr)
			durableErr = s.finalizeDurableCaches(ctx, item, leaseID, durablePlan, outcome.SealDecision)
		}
	} else {
		durableErr = s.finalizeDurableCaches(ctx, item, leaseID, durablePlan, outcome.SealDecision)
	}
	if durableErr != nil {
		span.RecordError(durableErr)
		if s.Logger != nil {
			s.Logger.WarnContext(ctx, "durable cache finalization failed", "execution_id", item.ExecutionID, "attempt_id", item.AttemptID, "error", durableErr)
		}
	}

	completedAt := finalExec.ExitedAt
	if completedAt.IsZero() {
		completedAt = time.Now().UTC()
	}
	startedAt := execRecord.StartedAt
	if startedAt.IsZero() {
		startedAt = finalExec.StartedAt
	}
	if startedAt.IsZero() {
		startedAt = completedAt
	}
	durationMs := completedAt.Sub(startedAt).Milliseconds()
	if durationMs < 1 {
		durationMs = 1
	}
	settleStarted := time.Now().UTC()
	settleResult, err := s.settleBillingWindow(ctx, reservation, uint32(clampUint32(durationMs)), usageSummary(finalExec))
	settleCompleted := time.Now().UTC()
	result, reason = sandboxPhaseResult(err)
	s.recordExecutionPhase(ctx, item, "sandbox-rental", "sandbox.billing.settle", result, reason, settleStarted, settleCompleted, sandboxPhaseAttrs{
		"billing_window_id": reservation.WindowID,
		"duration_ms":       strconv.FormatInt(durationMs, 10),
	})
	if err != nil {
		failErr := s.failAttempt(ctx, item, "billing_settle_failed", err)
		if releaseErr := s.Orchestrator.ReleaseLease(detachedContext(ctx), leaseID, item.AttemptID.String()+":release-after-settle-failed"); releaseErr != nil && s.Logger != nil {
			s.Logger.WarnContext(ctx, "release lease after billing settle failure failed", "lease_id", leaseID, "error", releaseErr)
		}
		if failErr != nil {
			return failErr
		}
		return nil
	}
	_ = s.markBillingWindow(ctx, item.AttemptID, reservation.WindowID, "settled", int(durationMs), settleResult)
	completeStarted := time.Now().UTC()
	err = s.completeAttempt(ctx, item, outcome.State, outcome.Reason, finalExec, durationMs, completedAt)
	completeCompleted := time.Now().UTC()
	result, reason = sandboxPhaseResult(err)
	s.recordExecutionPhase(ctx, item, "sandbox-rental", "sandbox.execution.complete", result, reason, completeStarted, completeCompleted, sandboxPhaseAttrs{
		"terminal_state":  outcome.State,
		"terminal_reason": outcome.Reason,
	})
	if err != nil {
		if releaseErr := s.Orchestrator.ReleaseLease(detachedContext(ctx), leaseID, item.AttemptID.String()+":release-after-complete-failed"); releaseErr != nil && s.Logger != nil {
			s.Logger.WarnContext(ctx, "release lease after completion failure failed", "lease_id", leaseID, "error", releaseErr)
		}
		return err
	}
	if !goldenVMCreateQueued {
		releaseStarted := time.Now().UTC()
		releaseErr := s.Orchestrator.ReleaseLease(detachedContext(ctx), leaseID, item.AttemptID.String()+":release")
		releaseCompleted := time.Now().UTC()
		result, reason = sandboxPhaseResult(releaseErr)
		s.recordExecutionPhase(ctx, item, "sandbox-rental", "vm.lease.release", result, reason, releaseStarted, releaseCompleted, nil)
		if releaseErr != nil && s.Logger != nil {
			s.Logger.WarnContext(ctx, "release lease failed", "lease_id", leaseID, "error", releaseErr)
		}
	}
	runRecord, err := s.loadRun(ctx, item.OrgID, item.ExecutionID, false)
	if err == nil {
		runRecord.Status = outcome.State
		runRecord.LatestAttempt.TraceID = span.SpanContext().TraceID().String()
		_ = s.writeExecutionLogs(context.Background(), *runRecord, finalExec.Output)
		_ = s.writeJobEvent(context.Background(), jobEventRowForRun(*runRecord))
	} else if s.Logger != nil {
		s.Logger.WarnContext(ctx, "load run projection after execution failed", "execution_id", item.ExecutionID, "attempt_id", item.AttemptID, "error", err)
	}
	if item.WorkloadKind == WorkloadKindRunner {
		markStarted := time.Now().UTC()
		s.MarkRunnerExecutionExited(detachedContext(ctx), item.ExecutionID)
		s.recordExecutionPhase(ctx, item, "sandbox-rental", "sandbox.runner.mark_exited", "succeeded", "", markStarted, time.Now().UTC(), nil)
	}
	return nil
}

type executionTerminalOutcome struct {
	State        string
	Reason       string
	SealDecision durableSealDecision
}

type githubWorkflowJobResult struct {
	Status     string
	Conclusion string
	Observed   bool
}

func (r githubWorkflowJobResult) terminal() bool {
	return r.Observed && strings.TrimSpace(r.Status) == "completed"
}

func (r githubWorkflowJobResult) outcome() executionTerminalOutcome {
	status := strings.TrimSpace(r.Status)
	conclusion := strings.TrimSpace(r.Conclusion)
	if !r.Observed {
		reason := "github_job_result_missing"
		return executionTerminalOutcome{State: StateFailed, Reason: reason, SealDecision: durableSealDecision{SkipReason: reason}}
	}
	if status != "completed" {
		reason := "github_job_not_completed"
		if status != "" {
			reason += ": " + status
		}
		return executionTerminalOutcome{State: StateFailed, Reason: reason, SealDecision: durableSealDecision{SkipReason: reason}}
	}
	switch conclusion {
	case "success":
		return executionTerminalOutcome{State: StateSucceeded, SealDecision: durableSealDecision{Commit: true}}
	case "":
		reason := "github_job_conclusion_missing"
		return executionTerminalOutcome{State: StateFailed, Reason: reason, SealDecision: durableSealDecision{SkipReason: reason}}
	default:
		reason := "github_job_" + conclusion
		return executionTerminalOutcome{State: StateFailed, Reason: reason, SealDecision: durableSealDecision{SkipReason: reason}}
	}
}

func executionOutcomeFromExec(finalExec vmorchestrator.ExecRecord) executionTerminalOutcome {
	sealDecision := durableSealDecisionForExec(finalExec)
	if sealDecision.Commit {
		return executionTerminalOutcome{State: StateSucceeded, SealDecision: sealDecision}
	}
	state := StateFailed
	switch finalExec.State {
	case vmorchestrator.ExecStateCanceled:
		state = StateCanceled
	case vmorchestrator.ExecStateKilledByLeaseExpiry:
		state = StateLost
	}
	return executionTerminalOutcome{State: state, Reason: sealDecision.SkipReason, SealDecision: sealDecision}
}

func executionOutcomeFromGitHubJobResult(status, conclusion string, observed bool) executionTerminalOutcome {
	return githubWorkflowJobResult{Status: status, Conclusion: conclusion, Observed: observed}.outcome()
}

func (s *Service) executionTerminalOutcome(ctx context.Context, item executionWorkItem, finalExec vmorchestrator.ExecRecord) (executionTerminalOutcome, error) {
	outcome := executionOutcomeFromExec(finalExec)
	if item.WorkloadKind != WorkloadKindRunner || item.Provider != RunnerProviderGitHub {
		return outcome, nil
	}
	if !outcome.SealDecision.Commit {
		return outcome, nil
	}
	if s.GitHubRunner == nil {
		reason := "github_runner_not_configured"
		return executionTerminalOutcome{State: StateFailed, Reason: reason, SealDecision: durableSealDecision{SkipReason: reason}}, nil
	}
	identity, err := s.runnerExecutionIdentity(ctx, item.ExecutionID, item.AttemptID)
	if err != nil {
		reason := durableFailureReason("github_job_identity_failed", err)
		return executionTerminalOutcome{State: StateFailed, Reason: reason, SealDecision: durableSealDecision{SkipReason: reason}}, err
	}
	resultStarted := time.Now().UTC()
	result, pollCount, wait, err := s.waitForGitHubWorkflowJobResult(ctx, identity)
	resultCompleted := time.Now().UTC()
	phaseResult, phaseReason := sandboxPhaseResult(err)
	s.recordRunnerIdentityPhase(ctx, identity, item, "sandbox-rental", "github.job.result_wait", phaseResult, phaseReason, resultStarted, resultCompleted, sandboxPhaseAttrs{
		"github.workflow_job.poll_count": strconv.Itoa(pollCount),
		"github.workflow_job.wait_ms":    strconv.FormatInt(wait.Milliseconds(), 10),
	})
	if err != nil {
		reason := durableFailureReason("github_job_result_wait_failed", err)
		return executionTerminalOutcome{State: StateFailed, Reason: reason, SealDecision: durableSealDecision{SkipReason: reason}}, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.Int64("github.job_id", identity.ProviderJobID),
		attribute.Int64("github.workflow_run.attempt", identity.ProviderRunAttempt),
		attribute.String("github.workflow_job.status", result.Status),
		attribute.String("github.workflow_job.conclusion", result.Conclusion),
		attribute.Bool("github.workflow_job.observed", result.Observed),
		attribute.Int("github.workflow_job.poll_count", pollCount),
		attribute.Int64("github.workflow_job.wait_ms", wait.Milliseconds()),
	)
	return result.outcome(), nil
}

func (s *Service) waitForGitHubWorkflowJobResult(ctx context.Context, identity RunnerExecutionIdentity) (githubWorkflowJobResult, int, time.Duration, error) {
	start := time.Now()
	deadline := start.Add(githubWorkflowJobTerminalWait)
	var last githubWorkflowJobResult
	pollCount := 0
	for {
		jobs, err := s.GitHubRunner.refreshWorkflowRunJobs(ctx, identity)
		if err != nil {
			return last, pollCount, time.Since(start), err
		}
		status, conclusion, observed := jobs.jobResult(identity.ProviderJobID)
		last = githubWorkflowJobResult{Status: status, Conclusion: conclusion, Observed: observed}
		pollCount++
		if last.terminal() {
			return last, pollCount, time.Since(start), nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return last, pollCount, time.Since(start), nil
		}
		sleep := githubWorkflowJobTerminalPoll
		if remaining < sleep {
			sleep = remaining
		}
		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return last, pollCount, time.Since(start), ctx.Err()
		case <-timer.C:
		}
	}
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
		MaxWallSeconds:   uint64FromInt64(row.MaxWallSeconds, "max wall seconds"),
		AttemptState:     row.AttemptState,
		StartedAt:        timePtrFromPG(row.StartedAt),
		LeaseID:          row.LeaseID,
		ExecID:           row.ExecID,
		CorrelationID:    row.CorrelationID,
		Resources: VMResources{
			VCPUs:       uint32FromInt32(row.RequestedVcpus, "requested vcpus"),
			MemoryMiB:   uint32FromInt32(row.RequestedMemoryMib, "requested memory mib"),
			RootDiskGiB: uint32FromInt32(row.RequestedRootDiskGib, "requested root disk gib"),
			KernelImage: KernelImageRef(row.RequestedKernelImage),
		},
	}
	mounts, err := s.loadExecutionFilesystemMounts(ctx, executionID)
	if err != nil {
		return executionWorkItem{}, err
	}
	item.FilesystemMounts = mounts
	if _, _, err := s.runnerClassResources(ctx, item.RunnerClass); err != nil {
		return executionWorkItem{}, err
	}
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
	raw := binary.BigEndian.Uint64(attemptID[:8]) & billingMaxJSONSafePositiveInt
	if raw == 0 {
		return 1
	}
	return int64(raw)
}

func (s *Service) reserveBilling(ctx context.Context, item executionWorkItem, billingJobID int64) (billingclient.BillingWindowReservation, error) {
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
	jobID := billingclient.SafeInt64(billingJobID)
	resp, err := s.Billing.ReserveWindow(ctx, billingclient.ReserveWindowRequest{
		Body: billingclient.ReserveWindowInputBody{
			ActorID:          billingclient.ActorId(item.ActorID),
			Allocation:       billingclient.BillingAllocation(allocation),
			BillingJobID:     &jobID,
			ConcurrentCount:  1,
			OrgID:            billingclient.OrgId(item.OrgID),
			ProductID:        billingclient.ProductId(item.ProductID),
			ReservationShape: billingclient.ReservationShape("time"),
			ReservedQuantity: 0,
			SourceRef:        billingclient.BillingSourceRef(item.ExecutionID.String()),
			SourceType:       billingclient.BillingSourceType(item.SourceKind),
			WindowSeq:        1,
		},
	})
	if err != nil {
		return billingclient.BillingWindowReservation{}, fmt.Errorf("reserve billing window: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		if resp.Result == nil {
			return billingclient.BillingWindowReservation{}, newBillingStatusError("reserve billing window", resp.StatusCode, resp.Problem, nil)
		}
		return resp.Result.Reservation, nil
	case http.StatusPaymentRequired:
		return billingclient.BillingWindowReservation{}, newBillingStatusError("reserve billing window", resp.StatusCode, resp.Problem, ErrBillingPaymentRequired)
	case http.StatusForbidden:
		return billingclient.BillingWindowReservation{}, newBillingStatusError("reserve billing window", resp.StatusCode, resp.Problem, ErrBillingForbidden)
	default:
		return billingclient.BillingWindowReservation{}, newBillingStatusError("reserve billing window", resp.StatusCode, resp.Problem, nil)
	}
}

func (s *Service) durableStorageQuotaBytes(ctx context.Context, item executionWorkItem) (uint64, error) {
	return s.durableStorageQuotaBytesForOrgProduct(ctx, item.OrgID, item.ProductID)
}

func (s *Service) insertBillingWindow(ctx context.Context, attemptID uuid.UUID, reservation billingclient.BillingWindowReservation) error {
	payload, _ := json.Marshal(reservation)
	reservedChargeUnits, err := billingDecimalUint64(reservation.ReservedChargeUnits, "reserved charge units")
	if err != nil {
		return err
	}
	costPerUnit, err := billingDecimalUint64(reservation.CostPerUnit, "cost per unit")
	if err != nil {
		return err
	}
	windowStart, err := billingTime(reservation.WindowStart, "window start")
	if err != nil {
		return err
	}
	if err := s.storeQueries().InsertExecutionBillingWindow(ctx, store.InsertExecutionBillingWindowParams{
		AttemptID:           attemptID,
		WindowSeq:           int32FromInt64(int64(reservation.WindowSeq), "billing window seq"),
		BillingWindowID:     reservation.WindowID,
		ReservationShape:    reservation.ReservationShape,
		ReservedQuantity:    int32FromInt64(int64(reservation.ReservedQuantity), "reserved quantity"),
		ReservedChargeUnits: mustInt64FromUint64(reservedChargeUnits, "reserved charge units"),
		CostPerUnit:         mustInt64FromUint64(costPerUnit, "cost per unit"),
		PricingPhase:        string(reservation.PricingPhase),
		WindowStart:         pgTime(windowStart),
		CreatedAt:           pgTime(time.Now().UTC()),
		ReservationJsonb:    payload,
	}); err != nil {
		return fmt.Errorf("insert billing window: %w", err)
	}
	return nil
}

func (s *Service) markBillingWindow(ctx context.Context, attemptID uuid.UUID, windowID, state string, actual int, settled billingclient.BillingSettleResult) error {
	billedChargeUnits, err := billingDecimalUint64(settled.BilledChargeUnits, "billed charge units")
	if err != nil {
		return err
	}
	writeoffChargeUnits, err := billingDecimalUint64(settled.WriteoffChargeUnits, "writeoff charge units")
	if err != nil {
		return err
	}
	return s.storeQueries().MarkExecutionBillingWindow(ctx, store.MarkExecutionBillingWindowParams{
		State:               state,
		ActualQuantity:      int32FromInt(actual, "actual quantity"),
		BilledChargeUnits:   mustInt64FromUint64(billedChargeUnits, "billed charge units"),
		WriteoffChargeUnits: mustInt64FromUint64(writeoffChargeUnits, "writeoff charge units"),
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
		ExitCode:               int32FromInt(exec.ExitCode, "exec exit code"),
		DurationMs:             durationMs,
		ZfsWritten:             mustInt64FromUint64(exec.ZFSWritten, "zfs written"),
		StdoutBytes:            mustInt64FromUint64(exec.StdoutBytes, "stdout bytes"),
		StderrBytes:            mustInt64FromUint64(exec.StderrBytes, "stderr bytes"),
		RootfsProvisionedBytes: mustInt64FromUint64(exec.RootfsProvisionedBytes, "rootfs provisioned bytes"),
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
		FromState:              StateFinalizing,
	})
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("execution attempt %s is not in expected state %s", item.AttemptID, StateFinalizing)
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
	if err == nil {
		s.failOpenDurableOperationsForAttempt(detachedContext(ctx), item, reason, cause)
		if item.WorkloadKind == WorkloadKindRunner {
			s.MarkRunnerExecutionExited(detachedContext(ctx), item.ExecutionID)
		}
	}
	return err
}

func (s *Service) cleanupLeaseAndReservation(ctx context.Context, leaseID string, reservation billingclient.BillingWindowReservation) {
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

func (s *Service) GetExecution(ctx context.Context, orgID string, executionID uuid.UUID) (*ExecutionRecord, error) {
	return s.GetRun(ctx, orgID, executionID)
}

func (s *Service) GetExecutionLogs(ctx context.Context, orgID string, executionID uuid.UUID) (uuid.UUID, string, error) {
	attemptID, err := s.storeQueries().GetLatestAttemptForExecution(ctx, store.GetLatestAttemptForExecutionParams{
		OrgID:       dbOrgID(orgID),
		ExecutionID: executionID,
	})
	if err != nil {
		return uuid.Nil, "", ErrExecutionMissing
	}
	chunks, err := s.storeQueries().ListExecutionLogChunks(ctx, store.ListExecutionLogChunksParams{
		OrgID:     dbOrgID(orgID),
		AttemptID: attemptID,
	})
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
			ReservedChargeUnits: uint64FromInt64(row.ReservedChargeUnits, "reserved charge units"),
			BilledChargeUnits:   uint64FromInt64(row.BilledChargeUnits, "billed charge units"),
			WriteoffChargeUnits: uint64FromInt64(row.WriteoffChargeUnits, "writeoff charge units"),
			CostPerUnit:         uint64FromInt64(row.CostPerUnit, "cost per unit"),
			PricingPhase:        row.PricingPhase,
			State:               row.State,
			WindowStart:         timeFromPG(row.WindowStart),
			CreatedAt:           timeFromPG(row.CreatedAt),
			SettledAt:           timePtrFromPG(row.SettledAt),
		})
	}
	return out, nil
}

func (s *Service) latestBillingReservation(ctx context.Context, item executionWorkItem) (billingclient.BillingWindowReservation, error) {
	windows, err := s.listBillingWindows(ctx, item.AttemptID)
	if err != nil {
		return billingclient.BillingWindowReservation{}, err
	}
	if len(windows) == 0 {
		return billingclient.BillingWindowReservation{}, fmt.Errorf("execution attempt %s has no billing window", item.AttemptID)
	}
	window := windows[len(windows)-1]
	return billingclient.BillingWindowReservation{
		WindowID:            window.BillingWindowID,
		OrgID:               billingclient.OrgId(item.OrgID),
		ProductID:           billingclient.ProductId(item.ProductID),
		ActorID:             billingclient.ActorId(item.ActorID),
		SourceType:          billingclient.BillingSourceType(item.SourceKind),
		SourceRef:           billingclient.BillingSourceRef(item.ExecutionID.String()),
		WindowSeq:           billingclient.WindowSequence(window.WindowSeq),
		ReservationShape:    billingclient.ReservationShape(window.ReservationShape),
		ReservedQuantity:    billingclient.WindowQuantity(window.ReservedQuantity),
		ReservedChargeUnits: billingclient.DecimalUint64(strconv.FormatUint(window.ReservedChargeUnits, 10)),
		PricingPhase:        billingclient.PricingPhase(window.PricingPhase),
		CostPerUnit:         billingclient.DecimalUint64(strconv.FormatUint(window.CostPerUnit, 10)),
		WindowStart:         window.WindowStart.UTC().Format(time.RFC3339Nano),
	}, nil
}

func (s *Service) writeExecutionLogs(ctx context.Context, record ExecutionRecord, logs string) error {
	if logs == "" {
		return nil
	}
	if err := s.storeQueries().InsertExecutionLog(ctx, store.InsertExecutionLogParams{
		ExecutionID: record.ExecutionID,
		OrgID:       dbOrgID(record.OrgID),
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
	if err := batch.Send(); err != nil {
		return err
	}
	if rows := sandboxPhaseRowsForRunnerLog(record, logs); len(rows) > 0 {
		go func() {
			if err := s.writeSandboxPhaseEvents(context.Background(), rows); err != nil && s.Logger != nil {
				s.Logger.WarnContext(context.Background(), "runner log phase event insert failed", "execution_id", record.ExecutionID, "error", err)
			}
		}()
	}
	return nil
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
		ExitCode:               int32FromInt(record.LatestAttempt.ExitCode, "exit code"),
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
	classRec, ok, err := s.runnerClassResources(ctx, req.RunnerClass)
	if err != nil {
		return SubmitRequest{}, err
	}
	if !ok {
		return SubmitRequest{}, fmt.Errorf("%w: %s", ErrRunnerClassMissing, req.RunnerClass)
	}
	req.ProductID = firstNonEmpty(strings.TrimSpace(req.ProductID), classRec.ProductID, defaultProductID)
	if req.ProductID != classRec.ProductID {
		return SubmitRequest{}, fmt.Errorf("runner_class %s belongs to product %s, got product_id %s", req.RunnerClass, classRec.ProductID, req.ProductID)
	}
	// Runner classes are product defaults. Fill omitted fields from the class
	// before bounds validation so billing, traces, and VM admission agree.
	req.Resources = vmResourcesWithDefaults(req.Resources, classRec.Resources)
	bounds := s.Bounds
	if bounds == (VMResourceBounds{}) {
		bounds = DefaultBounds
	}
	if err := req.Resources.Validate(bounds); err != nil {
		return SubmitRequest{}, err
	}
	return req, nil
}

func vmResourcesWithDefaults(resources, defaults VMResources) VMResources {
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
	return mustInt64FromUint64(pick(metrics), "vm metric")
}

func workloadTimeout(item executionWorkItem, configured time.Duration) time.Duration {
	if item.MaxWallSeconds > 0 {
		return durationFromSeconds(item.MaxWallSeconds, "max wall seconds")
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

func leaseTTLSeconds(item executionWorkItem, configured time.Duration) uint64 {
	return maxWallSeconds(item, configured) + leaseTTLGraceSeconds
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

func (s *Service) activateBillingWindow(ctx context.Context, reservation billingclient.BillingWindowReservation, activatedAt time.Time) (billingclient.BillingWindowReservation, error) {
	resp, err := s.Billing.ActivateWindow(ctx, billingclient.ActivateWindowRequest{
		Body: billingclient.ActivateWindowInputBody{
			ActivatedAt: activatedAt.UTC().Format(time.RFC3339Nano),
			WindowID:    billingclient.BillingWindowId(reservation.WindowID),
		},
	})
	if err != nil {
		return billingclient.BillingWindowReservation{}, fmt.Errorf("activate billing window: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		if resp.Result == nil {
			return billingclient.BillingWindowReservation{}, newBillingStatusError("activate billing window", resp.StatusCode, resp.Problem, nil)
		}
		return resp.Result.Reservation, nil
	default:
		return billingclient.BillingWindowReservation{}, newBillingStatusError("activate billing window", resp.StatusCode, resp.Problem, nil)
	}
}

func (s *Service) settleBillingWindow(ctx context.Context, reservation billingclient.BillingWindowReservation, actualQuantity uint32, usageSummary map[string]any) (billingclient.BillingSettleResult, error) {
	actualQuantityInt, err := billingWireInt64("actual_quantity", actualQuantity)
	if err != nil {
		return billingclient.BillingSettleResult{}, err
	}
	req := billingclient.SettleWindowRequest{Body: billingclient.SettleWindowInputBody{
		ActualQuantity: billingclient.WindowQuantity(actualQuantityInt),
		WindowID:       billingclient.BillingWindowId(reservation.WindowID),
	}}
	if usageSummary != nil {
		req.Body.UsageSummary = &usageSummary
	}
	resp, err := s.Billing.SettleWindow(ctx, req)
	if err != nil {
		return billingclient.BillingSettleResult{}, fmt.Errorf("settle billing window: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		if resp.Result == nil {
			return billingclient.BillingSettleResult{}, newBillingStatusError("settle billing window", resp.StatusCode, resp.Problem, nil)
		}
		return *resp.Result, nil
	default:
		return billingclient.BillingSettleResult{}, newBillingStatusError("settle billing window", resp.StatusCode, resp.Problem, nil)
	}
}

func (s *Service) voidBillingWindow(ctx context.Context, reservation billingclient.BillingWindowReservation) error {
	resp, err := s.Billing.VoidWindow(ctx, billingclient.VoidWindowRequest{
		Body: billingclient.VoidWindowInputBody{
			WindowID: billingclient.BillingWindowId(reservation.WindowID),
		},
	})
	if err != nil {
		return fmt.Errorf("void billing window: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	default:
		return newBillingStatusError("void billing window", resp.StatusCode, resp.Problem, nil)
	}
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

func billingWireInt64(field string, value uint32) (int64, error) {
	if uint64(value) > billingMaxJSONSafePositiveInt {
		return 0, fmt.Errorf("%s exceeds billing wire range", field)
	}
	return int64(value), nil
}

func billingDecimalUint64(value billingclient.DecimalUint64, field string) (uint64, error) {
	raw := strings.TrimSpace(string(value))
	if raw == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s is not an unsigned decimal: %w", field, err)
	}
	return parsed, nil
}

func billingSafeUint64(value billingclient.SafeUint64, field string) (uint64, error) {
	if value < 0 {
		return 0, fmt.Errorf("%s must be non-negative", field)
	}
	return uint64(value), nil
}

func billingTime(value string, field string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, fmt.Errorf("%s is required", field)
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s is not RFC3339: %w", field, err)
	}
	return parsed.UTC(), nil
}

func int64FromUint64(field string, value uint64) (int64, error) {
	if value > math.MaxInt64 {
		return 0, fmt.Errorf("%s exceeds int64 range", field)
	}
	return int64(value), nil // #nosec G115 -- value is checked against MaxInt64 above.
}
