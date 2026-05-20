package vmorchestrator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/vm-orchestrator/vmproto"
	"github.com/verself/vm-orchestrator/zfs"
)

var tracer = otel.Tracer("vm-orchestrator")

const (
	defaultTrustClass      = "trusted"
	leaseBootTimeout       = 45 * time.Second
	firecrackerStepTimeout = 5 * time.Second
	maxBufferedGuestLogs   = 10 * 1024 * 1024
	maxFilesystemMounts    = 99
)

// firecrackerStep is one PUT against the Firecracker API socket. The
// boot path runs them in declaration order under
// vmorchestrator.firecracker.configure_all; each step gets its own child
// span keyed off step.name and a per-step timeout (defaulting to
// firecrackerStepTimeout when zero) so a slow step is observable in
// traces without lengthening the rest of the pipeline.
type firecrackerStep struct {
	name    string
	timeout time.Duration
	fn      func(context.Context) error
}

type Config struct {
	Pool                        string
	ImageDataset                string
	GoldenDataset               string
	WorkloadDataset             string
	StorageKeyDir               string
	DefaultSubstrateRef         string
	KernelPath                  string
	FirecrackerBin              string
	JailerBin                   string
	JailerRoot                  string
	SnapshotCacheDir            string
	FirecrackerSnapshotsEnabled bool
	JailerUID                   int
	JailerGID                   int
	Bounds                      VMResourceBounds
	HostInterface               string
	GuestPoolCIDR               string
	StateDBPath                 string
	HostServiceIP               string
	HostServicePort             int

	// Host-side deterministic telemetry faults are verification-only and must
	// be empty in normal service operation.
	TelemetryFaultProfile string
}

func DefaultConfig() Config {
	return Config{
		Pool:                        "vspool",
		ImageDataset:                "images",
		GoldenDataset:               "goldens",
		WorkloadDataset:             "workloads",
		StorageKeyDir:               "/var/lib/verself/vm-orchestrator/storage-keys",
		DefaultSubstrateRef:         "substrate",
		KernelPath:                  "/var/lib/verself/guest-images/vmlinux",
		FirecrackerBin:              "/usr/local/bin/firecracker",
		JailerBin:                   "/usr/local/bin/jailer",
		JailerRoot:                  "/srv/jailer",
		SnapshotCacheDir:            "/srv/jailer/firecracker-snapshot-cache",
		FirecrackerSnapshotsEnabled: true,
		JailerUID:                   10000,
		JailerGID:                   10000,
		Bounds:                      DefaultBounds,
		GuestPoolCIDR:               defaultGuestPoolCIDR,
		StateDBPath:                 defaultStateDBPath,
		HostServiceIP:               defaultHostServiceIP,
		HostServicePort:             defaultHostServicePort,
	}
}

type LeaseState int

const (
	LeaseStateUnspecified LeaseState = iota
	LeaseStateAcquiring
	LeaseStateReady
	LeaseStateDraining
	LeaseStateReleased
	LeaseStateExpired
	LeaseStateCrashed
)

func (s LeaseState) Terminal() bool {
	return s == LeaseStateReleased || s == LeaseStateExpired || s == LeaseStateCrashed
}

type ExecState int

const (
	ExecStateUnspecified ExecState = iota
	ExecStatePending
	ExecStateRunning
	ExecStateExited
	ExecStateFailed
	ExecStateCanceled
	ExecStateKilledByLeaseExpiry
)

func (s ExecState) Terminal() bool {
	return s == ExecStateExited || s == ExecStateFailed || s == ExecStateCanceled || s == ExecStateKilledByLeaseExpiry
}

type LeaseEventType string

const (
	LeaseEventLeaseAcquired           LeaseEventType = "lease_acquired"
	LeaseEventVMBooting               LeaseEventType = "vm_booting"
	LeaseEventVMReady                 LeaseEventType = "vm_ready"
	LeaseEventLeaseRenewed            LeaseEventType = "lease_renewed"
	LeaseEventExecStarted             LeaseEventType = "exec_started"
	LeaseEventExecFinished            LeaseEventType = "exec_finished"
	LeaseEventExecCanceled            LeaseEventType = "exec_canceled"
	LeaseEventFilesystemCommitStarted LeaseEventType = "filesystem_commit_started"
	LeaseEventFilesystemCommitted     LeaseEventType = "filesystem_committed"
	LeaseEventFilesystemCommitFailed  LeaseEventType = "filesystem_commit_failed"
	LeaseEventVMShutdown              LeaseEventType = "vm_shutdown"
	LeaseEventLeaseExpired            LeaseEventType = "lease_expired"
	LeaseEventLeaseReleased           LeaseEventType = "lease_released"
	LeaseEventLeaseCrashed            LeaseEventType = "lease_crashed"
	LeaseEventTelemetryDiagnostic     LeaseEventType = "telemetry_diagnostic"
)

type LeaseSpec struct {
	Resources        VMResources
	TTLSeconds       uint64
	TrustClass       string
	NetworkMode      string
	StorageNamespace StorageNamespace
	FilesystemMounts []FilesystemMount
}

type StorageNamespace struct {
	OrgID      string
	QuotaBytes uint64
}

type FilesystemMount struct {
	Name        string
	OperationID string
	SourceRef   string
	MountPath   string
	BindPaths   []string
	FSType      string
	ReadOnly    bool
	Required    bool
}

type preparedFilesystemMount struct {
	Spec            FilesystemMount
	DriveID         string
	Dataset         string
	HostDevicePath  string
	JailDevicePath  string
	GuestDevicePath string
	clone           zfs.MountClone
}

type ExecSpec struct {
	Argv           []string
	WorkingDir     string
	Env            map[string]string
	MaxWallSeconds uint64
}

type ExecResult struct {
	ExitCode               int
	Output                 string
	Duration               time.Duration
	StartedAt              time.Time
	FirstByteAt            time.Time
	ExitedAt               time.Time
	StdoutBytes            uint64
	StderrBytes            uint64
	DroppedLogBytes        uint64
	ZFSWritten             uint64
	RootfsProvisionedBytes uint64
	Metrics                *VMMetrics
}

type LeaseRuntime struct {
	LeaseID      string
	Lease        zfs.Lease
	Dataset      string
	Network      NetworkLease
	Mounts       []preparedFilesystemMount
	MountResults []FilesystemMountResult

	apiSocketPath   string
	jailRoot        string
	control         *guestControl
	jailer          *JailerProcess
	metricsPath     string
	cancelTelemetry context.CancelFunc
	telemetryDone   chan struct{}

	waitDone     chan error
	jailerExited atomic.Bool
	logWg        sync.WaitGroup

	cleanups []func()
	logger   *slog.Logger
}

type Orchestrator struct {
	cfg     Config
	roots   zfs.Roots
	logger  *slog.Logger
	ops     PrivOps
	volumes *zfs.VolumeLifecycle
	keys    *StorageKeyManager
	journal func(durableJournalEntry)
}

type Option func(*Orchestrator)

func WithPrivOps(ops PrivOps) Option {
	return func(o *Orchestrator) {
		o.ops = ops
	}
}

func WithStorageKeyManager(keys *StorageKeyManager) Option {
	return func(o *Orchestrator) {
		o.keys = keys
	}
}

func withDurableJournal(journal func(durableJournalEntry)) Option {
	return func(o *Orchestrator) {
		o.journal = journal
	}
}

func New(cfg Config, logger *slog.Logger, opts ...Option) *Orchestrator {
	base := DefaultConfig()
	if cfg.Pool != "" {
		base = cfg
	}
	if base.ImageDataset == "" {
		base.ImageDataset = "images"
	}
	if base.GoldenDataset == "" {
		base.GoldenDataset = "goldens"
	}
	if base.WorkloadDataset == "" {
		base.WorkloadDataset = "workloads"
	}
	if base.StorageKeyDir == "" {
		base.StorageKeyDir = "/var/lib/verself/vm-orchestrator/storage-keys"
	}
	if base.Bounds == (VMResourceBounds{}) {
		base.Bounds = DefaultBounds
	}
	if base.HostServiceIP == "" {
		base.HostServiceIP = defaultHostServiceIP
	}
	if base.HostServicePort == 0 {
		base.HostServicePort = defaultHostServicePort
	}
	if logger == nil {
		logger = slog.Default()
	}
	if base.DefaultSubstrateRef == "" {
		base.DefaultSubstrateRef = "substrate"
	}
	if base.SnapshotCacheDir == "" {
		base.SnapshotCacheDir = filepath.Join(base.JailerRoot, "firecracker-snapshot-cache")
	}
	o := &Orchestrator{cfg: base, logger: logger, ops: DirectPrivOps{}}
	o.roots = zfs.Roots{
		Pool:            base.Pool,
		ImageDataset:    base.ImageDataset,
		GoldenDataset:   base.GoldenDataset,
		WorkloadDataset: base.WorkloadDataset,
	}
	// opts may override ops; volumes binds to whichever ops the final
	// orchestrator carries, so build it after the option loop.
	for _, opt := range opts {
		opt(o)
	}
	if o.keys == nil {
		o.keys = NewStorageKeyManager(NewFileStorageKeyProvider(base.StorageKeyDir), logger)
	}
	o.volumes = zfs.NewVolumeLifecycle(o.roots, o.ops, logger)
	return o
}

func (o *Orchestrator) releaseStorageKeyMaterial(ctx context.Context, orgID, leaseID string, hold *StorageKeyHold) {
	if hold == nil {
		return
	}
	lastRef := hold.Release(ctx)
	if !lastRef {
		return
	}
	o.logger.InfoContext(ctx, "storage key material idle", "org_id", orgID, "lease_id", leaseID)
}

// normalizeLeaseSpec fills in defaults and re-validates the VM shape
// against the host bounds. Validation at this layer is a defense in depth
// for callers that build LeaseSpec directly; the RPC path already checks
// at rpc_convert, but in-process constructors (tests, tracer bullets)
// still flow through here.
func normalizeLeaseSpec(spec LeaseSpec, cfg Config) (LeaseSpec, error) {
	spec.TrustClass = strings.TrimSpace(spec.TrustClass)
	if spec.TrustClass == "" {
		spec.TrustClass = defaultTrustClass
	}
	spec.StorageNamespace.OrgID = strings.TrimSpace(spec.StorageNamespace.OrgID)
	if !zfs.IsValidRef(spec.StorageNamespace.OrgID) {
		return LeaseSpec{}, fmt.Errorf("storage_namespace.org_id is required and must be a safe ref")
	}
	if spec.StorageNamespace.QuotaBytes == 0 {
		return LeaseSpec{}, fmt.Errorf("storage_namespace.quota_bytes is required")
	}
	spec.Resources = spec.Resources.Normalize()
	bounds := cfg.Bounds
	if bounds == (VMResourceBounds{}) {
		bounds = DefaultBounds
	}
	if err := spec.Resources.Validate(bounds); err != nil {
		return LeaseSpec{}, err
	}
	if spec.TTLSeconds == 0 {
		spec.TTLSeconds = 5 * 60
	}
	mounts, err := normalizeFilesystemMounts(spec.FilesystemMounts, filesystemMountSourceOptional)
	if err != nil {
		return LeaseSpec{}, err
	}
	spec.FilesystemMounts = mounts
	return spec, nil
}

type filesystemMountSourceMode uint8

const (
	filesystemMountSourceRequired filesystemMountSourceMode = iota
	filesystemMountSourceOptional
)

func normalizeFilesystemMounts(mounts []FilesystemMount, sourceMode filesystemMountSourceMode) ([]FilesystemMount, error) {
	if len(mounts) == 0 {
		return nil, nil
	}
	if len(mounts) > maxFilesystemMounts {
		return nil, fmt.Errorf("filesystem_mounts exceeds %d entries", maxFilesystemMounts)
	}
	seenNames := map[string]struct{}{}
	seenPaths := map[string]struct{}{}
	out := make([]FilesystemMount, 0, len(mounts))
	for idx, mount := range mounts {
		mount.Name = strings.TrimSpace(mount.Name)
		mount.OperationID = strings.TrimSpace(mount.OperationID)
		mount.SourceRef = strings.TrimSpace(mount.SourceRef)
		mount.MountPath = filepath.Clean(strings.TrimSpace(mount.MountPath))
		for bindIdx, bindPath := range mount.BindPaths {
			mount.BindPaths[bindIdx] = filepath.Clean(strings.TrimSpace(bindPath))
		}
		mount.FSType = firstNonEmpty(strings.TrimSpace(mount.FSType), "ext4")
		if mount.Name == "" {
			return nil, fmt.Errorf("filesystem_mounts[%d].name is required", idx)
		}
		if !zfs.IsValidRef(mount.Name) {
			return nil, fmt.Errorf("filesystem_mounts[%d].name is invalid", idx)
		}
		if mount.OperationID != "" && !zfs.IsValidRef(mount.OperationID) {
			return nil, fmt.Errorf("filesystem_mounts[%d].operation_id is invalid", idx)
		}
		if mount.SourceRef == "" && sourceMode == filesystemMountSourceRequired {
			return nil, fmt.Errorf("filesystem_mounts[%d].source_ref is required", idx)
		}
		if mount.SourceRef != "" && sourceMode == filesystemMountSourceRequired && !zfs.IsValidRef(mount.SourceRef) {
			return nil, fmt.Errorf("filesystem_mounts[%d].source_ref is invalid", idx)
		}
		if mount.MountPath == "." || !strings.HasPrefix(mount.MountPath, "/") || mount.MountPath == "/" {
			return nil, fmt.Errorf("filesystem_mounts[%d].mount_path must be an absolute non-root path", idx)
		}
		if strings.HasPrefix(mount.MountPath, "/proc") || strings.HasPrefix(mount.MountPath, "/sys") || strings.HasPrefix(mount.MountPath, "/dev") || strings.HasPrefix(mount.MountPath, "/run") {
			return nil, fmt.Errorf("filesystem_mounts[%d].mount_path is not allowed", idx)
		}
		seenBindPaths := map[string]struct{}{}
		for bindIdx, bindPath := range mount.BindPaths {
			if bindPath == "." || !strings.HasPrefix(bindPath, "/") || bindPath == "/" {
				return nil, fmt.Errorf("filesystem_mounts[%d].bind_paths[%d] must be an absolute non-root path", idx, bindIdx)
			}
			if strings.HasPrefix(bindPath, "/proc") || strings.HasPrefix(bindPath, "/sys") || strings.HasPrefix(bindPath, "/dev") || strings.HasPrefix(bindPath, "/run") {
				return nil, fmt.Errorf("filesystem_mounts[%d].bind_paths[%d] is not allowed", idx, bindIdx)
			}
			if _, ok := seenBindPaths[bindPath]; ok {
				return nil, fmt.Errorf("filesystem_mounts[%d].bind_paths[%d] is duplicated", idx, bindIdx)
			}
			seenBindPaths[bindPath] = struct{}{}
		}
		if mount.FSType != "ext4" {
			return nil, fmt.Errorf("filesystem_mounts[%d].fs_type %q is unsupported", idx, mount.FSType)
		}
		if _, ok := seenNames[mount.Name]; ok {
			return nil, fmt.Errorf("filesystem_mounts[%d].name is duplicated", idx)
		}
		if _, ok := seenPaths[mount.MountPath]; ok {
			return nil, fmt.Errorf("filesystem_mounts[%d].mount_path is duplicated", idx)
		}
		seenNames[mount.Name] = struct{}{}
		seenPaths[mount.MountPath] = struct{}{}
		out = append(out, mount)
	}
	return out, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeExecSpec(spec ExecSpec) ExecSpec {
	spec.WorkingDir = strings.TrimSpace(spec.WorkingDir)
	if spec.Env == nil {
		spec.Env = map[string]string{}
	}
	return spec
}

func validateExecSpec(spec ExecSpec) error {
	if len(spec.Argv) == 0 {
		return fmt.Errorf("argv is required")
	}
	for _, arg := range spec.Argv {
		if strings.ContainsRune(arg, 0) {
			return fmt.Errorf("argv contains NUL byte")
		}
	}
	return nil
}

func (o *Orchestrator) jailDir(leaseID string) string {
	return filepath.Join(o.cfg.JailerRoot, "firecracker", leaseID, "root")
}

func (o *Orchestrator) BootLease(ctx context.Context, leaseID string, spec LeaseSpec, observer LeaseObserver) (*LeaseRuntime, error) {
	normalized, normErr := normalizeLeaseSpec(spec, o.cfg)
	if normErr != nil {
		return nil, fmt.Errorf("normalize lease spec: %w", normErr)
	}
	spec = normalized
	ctx, cancelBoot := context.WithTimeout(ctx, leaseBootTimeout)
	defer cancelBoot()
	ctx, span := tracer.Start(ctx, "vmorchestrator.lease.boot",
		trace.WithAttributes(
			attribute.String("lease.id", leaseID),
			attribute.Int64("lease.boot_timeout_ms", leaseBootTimeout.Milliseconds()),
			attribute.Int("vmresources.vcpus", int(spec.Resources.VCPUs)),
			attribute.Int("vmresources.memory_mib", int(spec.Resources.MemoryMiB)),
			attribute.Int("vmresources.root_disk_gib", int(spec.Resources.RootDiskGiB)),
			attribute.String("vmresources.kernel_image", string(spec.Resources.KernelImage)),
			attribute.Int("filesystem.mount_count", len(spec.FilesystemMounts)),
		),
	)
	var err error
	var keyHold *StorageKeyHold
	defer func() {
		if err != nil {
			if keyHold != nil {
				o.releaseStorageKeyMaterial(context.Background(), spec.StorageNamespace.OrgID, leaseID, keyHold)
			}
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	keyHold, err = o.keys.Acquire(ctx, spec.StorageNamespace.OrgID, leaseID)
	if err != nil {
		err = fmt.Errorf("acquire storage key: %w", err)
		return nil, err
	}

	namespace := zfs.StorageNamespace{
		OrgID:      spec.StorageNamespace.OrgID,
		QuotaBytes: spec.StorageNamespace.QuotaBytes,
	}
	namespaceCtx, endNamespaceSpan := startStepSpan(ctx, "vmorchestrator.zfs.namespace_ensure",
		attribute.String("lease.id", leaseID),
		attribute.String("org.id", namespace.OrgID),
		uint64TraceAttribute("storage.quota_bytes", namespace.QuotaBytes),
	)
	rawKey := keyHold.Key()
	ensureErr := o.volumes.EnsureEncryptedStorageNamespace(namespaceCtx, namespace, rawKey)
	for i := range rawKey {
		rawKey[i] = 0
	}
	endNamespaceSpan(ensureErr)
	o.releaseStorageKeyMaterial(context.Background(), spec.StorageNamespace.OrgID, leaseID, keyHold)
	keyHold = nil
	if ensureErr != nil {
		err = fmt.Errorf("ensure storage namespace: %w", ensureErr)
		return nil, err
	}
	lease, leaseRefErr := zfs.NewLease(o.roots, spec.StorageNamespace.OrgID, leaseID)
	if leaseRefErr != nil {
		err = fmt.Errorf("lease ref: %w", leaseRefErr)
		return nil, err
	}
	substrate, bootErr := zfs.NewImage(o.roots, o.cfg.DefaultSubstrateRef)
	if bootErr != nil {
		err = fmt.Errorf("substrate image ref: %w", bootErr)
		return nil, err
	}
	rootCloneCtx, endRootCloneSpan := startStepSpan(ctx, "vmorchestrator.zfs.root_clone",
		attribute.String("lease.id", leaseID),
		attribute.String("org.id", spec.StorageNamespace.OrgID),
		attribute.String("zfs.dataset", lease.RootDataset()),
		attribute.String("zfs.image_ref", substrate.Ref()),
	)
	prepErr := o.volumes.PrepareSubstrateClone(rootCloneCtx, lease, substrate)
	endRootCloneSpan(prepErr)
	if prepErr != nil {
		err = prepErr
		return nil, err
	}
	dataset := lease.RootDataset()

	rootBytes := uint64(spec.Resources.RootDiskGiB) * 1024 * 1024 * 1024
	rootResizeCtx, endRootResizeSpan := startStepSpan(ctx, "vmorchestrator.zfs.root_resize_ext4",
		attribute.String("lease.id", leaseID),
		attribute.String("org.id", spec.StorageNamespace.OrgID),
		attribute.String("zfs.dataset", lease.RootDataset()),
		uint64TraceAttribute("zfs.size_bytes", rootBytes),
		attribute.Int("vmresources.root_disk_gib", int(spec.Resources.RootDiskGiB)),
	)
	resizeErr := o.volumes.ResizeLeaseRootExt4(rootResizeCtx, lease, rootBytes)
	endRootResizeSpan(resizeErr)
	if resizeErr != nil {
		_ = o.volumes.DestroyLeaseRoot(context.Background(), lease)
		err = resizeErr
		return nil, err
	}

	mountsCtx, endMountsSpan := startStepSpan(ctx, "vmorchestrator.zfs.mounts_prepare",
		attribute.String("lease.id", leaseID),
		attribute.String("org.id", spec.StorageNamespace.OrgID),
		attribute.Int("filesystem.mount_count", len(spec.FilesystemMounts)),
	)
	mounts, mountErr := o.prepareFilesystemMounts(mountsCtx, lease, spec.FilesystemMounts)
	endMountsSpan(mountErr)
	if mountErr != nil {
		_ = o.volumes.DestroyLeaseRoot(context.Background(), lease)
		err = mountErr
		return nil, err
	}

	runtime, bootErr := o.bootDataset(ctx, lease, spec, dataset, mounts, observer)
	if bootErr != nil {
		_ = o.volumes.DestroyLeaseRoot(context.Background(), lease)
		err = bootErr
		return nil, err
	}
	runtime.cleanups = append(runtime.cleanups, func() {
		o.cleanupLeaseDatasets(runtime, lease, dataset, mounts)
	})
	return runtime, nil
}

func (o *Orchestrator) cleanupLeaseDatasets(runtime *LeaseRuntime, lease zfs.Lease, rootDataset string, mounts []preparedFilesystemMount) {
	ctx := context.Background()
	for _, mount := range mounts {
		o.appendDurableJournal(durableJournalEntry{
			OperationID:       firstNonEmpty(mount.Spec.OperationID, lease.ID()),
			LeaseID:           lease.ID(),
			MountName:         mount.Spec.Name,
			Phase:             "destroy_started",
			SourceDatasetRef:  mount.Spec.SourceRef,
			WorkingDatasetRef: mount.Dataset,
		})
	}
	if destroyErr := o.volumes.DestroyLeaseRoot(ctx, lease); destroyErr != nil {
		for _, mount := range mounts {
			o.appendDurableJournal(durableJournalEntry{
				OperationID:       firstNonEmpty(mount.Spec.OperationID, lease.ID()),
				LeaseID:           lease.ID(),
				MountName:         mount.Spec.Name,
				Phase:             "failed",
				SourceDatasetRef:  mount.Spec.SourceRef,
				WorkingDatasetRef: mount.Dataset,
				ErrorMessage:      destroyErr.Error(),
			})
		}
		logger := o.logger
		if runtime != nil && runtime.logger != nil {
			logger = runtime.logger
		}
		if logger != nil {
			logger.WarnContext(ctx, "lease zvol tree destroy failed",
				"error", destroyErr,
				"dataset", lease.Dataset(),
				"root_dataset", rootDataset,
				"filesystem_mount_count", len(mounts),
			)
		}
		return
	}
	for _, mount := range mounts {
		o.appendDurableJournal(durableJournalEntry{
			OperationID:       firstNonEmpty(mount.Spec.OperationID, lease.ID()),
			LeaseID:           lease.ID(),
			MountName:         mount.Spec.Name,
			Phase:             "destroyed",
			SourceDatasetRef:  mount.Spec.SourceRef,
			WorkingDatasetRef: mount.Dataset,
		})
	}
}

func (o *Orchestrator) prepareFilesystemMounts(ctx context.Context, lease zfs.Lease, mounts []FilesystemMount) ([]preparedFilesystemMount, error) {
	if len(mounts) == 0 {
		return nil, nil
	}
	prepared := make([]preparedFilesystemMount, 0, len(mounts))
	for idx, mount := range mounts {
		var (
			clone   zfs.MountClone
			prepErr error
		)
		operationID := firstNonEmpty(mount.OperationID, lease.ID())
		mountCtx, endMountSpan := startStepSpan(ctx, "vmorchestrator.zfs.mount_prepare",
			attribute.String("lease.id", lease.ID()),
			attribute.String("org.id", lease.OrgID()),
			attribute.String("filesystem.name", mount.Name),
			attribute.String("filesystem.mount_path", mount.MountPath),
			attribute.String("filesystem.source_ref", mount.SourceRef),
			attribute.String("filesystem.source_kind", filesystemMountSourceKind(mount.SourceRef)),
			attribute.String("filesystem.operation_id", operationID),
			attribute.Bool("filesystem.required", mount.Required),
			attribute.Bool("filesystem.read_only", mount.ReadOnly),
			attribute.Int("filesystem.sort_order", idx),
			attribute.Int("filesystem.bind_path_count", len(mount.BindPaths)),
		)
		mountSpan := trace.SpanFromContext(mountCtx)
		o.appendDurableJournal(durableJournalEntry{
			OperationID:      operationID,
			LeaseID:          lease.ID(),
			MountName:        mount.Name,
			Phase:            "mount_prepare_started",
			SourceDatasetRef: mount.SourceRef,
		})
		if mount.SourceRef == "" {
			clone, prepErr = o.volumes.PrepareEmptyMount(mountCtx, lease, idx, mount.Name, operationID)
		} else if strings.Contains(mount.SourceRef, "@") {
			gen, genErr := zfs.ParseGeneration(o.roots, mount.SourceRef)
			if genErr != nil {
				prepErr = fmt.Errorf("filesystem mount %s source: %w", mount.Name, genErr)
				endMountSpan(prepErr)
				return prepared, prepErr
			}
			if gen.Volume().OrgID() != lease.OrgID() {
				prepErr = fmt.Errorf("filesystem mount %s source belongs to another org", mount.Name)
				endMountSpan(prepErr)
				return prepared, prepErr
			}
			mountSpan.SetAttributes(attribute.String("filesystem.source_snapshot", gen.Snapshot().String()))
			clone, prepErr = o.volumes.PrepareMountFromSnapshot(mountCtx, lease, gen.Snapshot(), idx, mount.Name, operationID)
			if errors.Is(prepErr, zfs.ErrSnapshotNotFound) {
				mountSpan.SetAttributes(
					attribute.Bool("filesystem.source_snapshot_missing", true),
					attribute.String("filesystem.fallback", "empty_mount"),
				)
				o.logger.InfoContext(ctx, "filesystem mount source snapshot missing",
					"org_id", lease.OrgID(),
					"lease_id", lease.ID(),
					"mount_name", mount.Name,
					"operation_id", operationID,
					"source_snapshot", gen.Snapshot().String(),
					"fallback", "empty_mount",
				)
				o.appendDurableJournal(durableJournalEntry{
					OperationID:      operationID,
					LeaseID:          lease.ID(),
					MountName:        mount.Name,
					Phase:            "source_snapshot_missing",
					SourceDatasetRef: mount.SourceRef,
					ErrorMessage:     prepErr.Error(),
				})
				clone, prepErr = o.volumes.PrepareEmptyMount(mountCtx, lease, idx, mount.Name, operationID)
			}
		} else {
			image, imgErr := zfs.NewImage(o.roots, mount.SourceRef)
			if imgErr != nil {
				prepErr = fmt.Errorf("filesystem mount %s: %w", mount.Name, imgErr)
				endMountSpan(prepErr)
				return prepared, prepErr
			}
			mountSpan.SetAttributes(attribute.String("zfs.image_ref", image.Ref()))
			clone, prepErr = o.volumes.PrepareMount(mountCtx, lease, image, idx, mount.Name, operationID)
		}
		if prepErr != nil {
			o.appendDurableJournal(durableJournalEntry{
				OperationID:      operationID,
				LeaseID:          lease.ID(),
				MountName:        mount.Name,
				Phase:            "failed",
				SourceDatasetRef: mount.SourceRef,
				ErrorMessage:     prepErr.Error(),
			})
			endMountSpan(prepErr)
			return prepared, prepErr
		}
		target := clone.Dataset()
		o.appendDurableJournal(durableJournalEntry{
			OperationID:       operationID,
			LeaseID:           lease.ID(),
			MountName:         mount.Name,
			Phase:             "mounted",
			SourceDatasetRef:  mount.SourceRef,
			WorkingDatasetRef: target,
		})
		driveID := fmt.Sprintf("fs%d", idx)
		guestDevicePath := guestVirtioDevicePath(idx)
		mountSpan.SetAttributes(
			attribute.String("zfs.dataset", target),
			attribute.String("firecracker.drive_id", driveID),
			attribute.String("guest.device_path", guestDevicePath),
		)
		prepared = append(prepared, preparedFilesystemMount{
			Spec:            mount,
			DriveID:         driveID,
			Dataset:         target,
			HostDevicePath:  zvolDevicePath(target),
			JailDevicePath:  "/drives/" + driveID,
			GuestDevicePath: guestDevicePath,
			clone:           clone,
		})
		endMountSpan(nil)
	}
	return prepared, nil
}

func filesystemMountSourceKind(sourceRef string) string {
	sourceRef = strings.TrimSpace(sourceRef)
	if sourceRef == "" {
		return "empty"
	}
	if strings.Contains(sourceRef, "@") {
		return "generation_snapshot"
	}
	return "image"
}

func (o *Orchestrator) appendDurableJournal(entry durableJournalEntry) {
	if o.journal != nil && strings.TrimSpace(entry.OperationID) != "" {
		o.journal(entry)
	}
}

func guestVirtioDevicePath(index int) string {
	if index < 0 {
		return "/dev/vdz"
	}
	ordinal := index + 1 // /dev/vda is the root device.
	var suffix []byte
	for {
		suffix = append([]byte{byte('a' + ordinal%26)}, suffix...)
		ordinal = ordinal/26 - 1
		if ordinal < 0 {
			break
		}
	}
	return "/dev/vd" + string(suffix)
}

func guestFilesystemMounts(mounts []preparedFilesystemMount) []vmproto.FilesystemMount {
	if len(mounts) == 0 {
		return nil
	}
	out := make([]vmproto.FilesystemMount, 0, len(mounts))
	for _, mount := range mounts {
		out = append(out, vmproto.FilesystemMount{
			Name:       mount.Spec.Name,
			DriveID:    mount.DriveID,
			DevicePath: mount.GuestDevicePath,
			MountPath:  mount.Spec.MountPath,
			BindPaths:  append([]string(nil), mount.Spec.BindPaths...),
			FSType:     mount.Spec.FSType,
			ReadOnly:   mount.Spec.ReadOnly,
			Required:   mount.Spec.Required,
		})
	}
	return out
}

func (o *Orchestrator) bootDataset(ctx context.Context, lease zfs.Lease, spec LeaseSpec, dataset string, mounts []preparedFilesystemMount, observer LeaseObserver) (*LeaseRuntime, error) {
	leaseID := lease.ID()
	logger := o.logger.With("lease_id", leaseID, "dataset", dataset)
	telemetryFaultProfile, err := telemetryFaultProfileFromConfig(o.cfg)
	if err != nil {
		return nil, err
	}
	jailRoot := o.jailDir(leaseID)
	runtime := &LeaseRuntime{
		LeaseID:  leaseID,
		Lease:    lease,
		Dataset:  dataset,
		Mounts:   mounts,
		jailRoot: jailRoot,
		logger:   logger,
	}
	cleanupOnErr := true
	defer func() {
		if cleanupOnErr {
			runtime.Cleanup("boot_failed")
		}
	}()

	devPath := zvolDevicePath(dataset)
	deviceCtx, endDeviceSpan := startStepSpan(ctx, "vmorchestrator.zvol.wait_device",
		attribute.String("lease.id", leaseID),
		attribute.String("zfs.dataset", dataset),
		attribute.String("device.path", devPath),
	)
	if err := waitForDevice(deviceCtx, devPath); err != nil {
		endDeviceSpan(err)
		return nil, fmt.Errorf("wait for zvol device %s: %w", devPath, err)
	}
	endDeviceSpan(nil)

	jailDevices := []JailBlockDevice{{HostPath: devPath, JailPath: "/rootfs"}}
	for _, mount := range mounts {
		mountDeviceCtx, endMountDeviceSpan := startStepSpan(ctx, "vmorchestrator.zvol.mount_wait_device",
			attribute.String("lease.id", leaseID),
			attribute.String("filesystem.name", mount.Spec.Name),
			attribute.String("zfs.dataset", mount.Dataset),
			attribute.String("device.path", mount.HostDevicePath),
		)
		if err := waitForDevice(mountDeviceCtx, mount.HostDevicePath); err != nil {
			endMountDeviceSpan(err)
			return nil, fmt.Errorf("wait for filesystem zvol device %s: %w", mount.HostDevicePath, err)
		}
		endMountDeviceSpan(nil)
		jailDevices = append(jailDevices, JailBlockDevice{HostPath: mount.HostDevicePath, JailPath: mount.JailDevicePath})
	}

	jailCtx, endJailSpan := startStepSpan(ctx, "vmorchestrator.jail.setup",
		attribute.String("lease.id", leaseID),
		attribute.String("jail.root", jailRoot),
		attribute.String("device.path", devPath),
		attribute.Int("filesystem.mount_count", len(mounts)),
	)
	if err := o.ops.SetupJail(jailCtx, jailRoot, o.cfg.KernelPath, o.cfg.JailerUID, o.cfg.JailerGID, jailDevices); err != nil {
		endJailSpan(err)
		return nil, fmt.Errorf("setup jail: %w", err)
	}
	endJailSpan(nil)

	leaseJailDir := filepath.Dir(jailRoot)
	runtime.cleanups = append(runtime.cleanups, func() {
		// Never remove the shared jailer base; concurrent failed boots can otherwise erase live lease chroots.
		if filepath.Base(leaseJailDir) == leaseID {
			_ = os.RemoveAll(leaseJailDir)
		}
	})

	netCfg := NetworkPoolConfig{
		PoolCIDR:      o.cfg.GuestPoolCIDR,
		StateDBPath:   o.cfg.StateDBPath,
		HostInterface: o.cfg.HostInterface,
		TapOwnerUID:   o.cfg.JailerUID,
		TapOwnerGID:   o.cfg.JailerGID,
	}
	netCtx, endNetworkSpan := startStepSpan(ctx, "vmorchestrator.network.setup",
		attribute.String("lease.id", leaseID),
		attribute.String("network.pool_cidr", netCfg.PoolCIDR),
	)
	netSetup, netCleanup, err := setupNetwork(netCtx, leaseID, netCfg, o.ops)
	endNetworkSpan(err)
	if err != nil {
		return nil, fmt.Errorf("setup network: %w", err)
	}
	runtime.Network = netSetup.Lease
	runtime.cleanups = append(runtime.cleanups, netCleanup)

	apiSockHost := filepath.Join(jailRoot, "run", "firecracker.sock")
	controlSockHost := filepath.Join(jailRoot, "run", "vs-control.sock")
	runtime.metricsPath = filepath.Join(jailRoot, "metrics.json")
	runtime.apiSocketPath = apiSockHost

	jailerCtx, endJailerSpan := startStepSpan(ctx, "vmorchestrator.jailer.start",
		attribute.String("lease.id", leaseID),
	)
	jailer, err := o.ops.StartJailer(jailerCtx, leaseID, JailerConfig{
		FirecrackerBin: o.cfg.FirecrackerBin,
		JailerBin:      o.cfg.JailerBin,
		ChrootBaseDir:  o.cfg.JailerRoot,
		UID:            o.cfg.JailerUID,
		GID:            o.cfg.JailerGID,
	})
	endJailerSpan(err)
	if err != nil {
		return nil, fmt.Errorf("start jailer: %w", err)
	}
	runtime.jailer = jailer
	// Surface the Firecracker PID on the lease.boot span so traces are joinable
	// to host cgroup/process-level metrics without another query.
	trace.SpanFromContext(ctx).SetAttributes(attribute.Int("firecracker.pid", jailer.Pid))
	if err := NewAllocator(netCfg).AttachPID(ctx, leaseID, jailer.Pid); err != nil {
		return nil, fmt.Errorf("record network lease pid: %w", err)
	}
	runtime.cleanups = append(runtime.cleanups, func() {
		if !runtime.jailerExited.Load() {
			_ = jailer.Kill()
			_ = jailer.Wait()
		}
	})
	if jailer.Stdout != nil {
		runtime.logWg.Add(1)
		go captureSerialOutput(jailer.Stdout, &runtime.logWg)
	}
	if jailer.Stderr != nil {
		runtime.logWg.Add(1)
		go captureSerialOutput(jailer.Stderr, &runtime.logWg)
	}
	runtime.waitDone = make(chan error, 1)
	ctx, cancelOnJailerExit := context.WithCancelCause(ctx)
	go func() {
		waitErr := jailer.Wait()
		runtime.jailerExited.Store(true)
		if waitErr != nil {
			cancelOnJailerExit(fmt.Errorf("firecracker exited during boot: %w", waitErr))
		} else {
			cancelOnJailerExit(fmt.Errorf("firecracker exited during boot"))
		}
		runtime.waitDone <- waitErr
	}()

	apiSocketCtx, endAPISocketSpan := startStepSpan(ctx, "vmorchestrator.firecracker.api_socket_wait",
		attribute.String("lease.id", leaseID),
		attribute.String("socket.path", apiSockHost),
	)
	if err := waitForSocket(apiSocketCtx, apiSockHost); err != nil {
		endAPISocketSpan(err)
		return nil, fmt.Errorf("wait for API socket: %w", err)
	}
	endAPISocketSpan(nil)

	client := newAPIClient(apiSockHost)
	// Kernel cmdline flags live with the privileged Firecracker adapter.
	bootArgs := RenderCmdline(DefaultKernelCmdlineBase)
	activationMode, err := o.activateFirecracker(ctx, runtime, spec, mounts, client, netSetup, bootArgs)
	if err != nil {
		return nil, err
	}

	controlSocketCtx, endControlSocketSpan := startStepSpan(ctx, "vmorchestrator.guest.control_socket_wait",
		attribute.String("lease.id", leaseID),
		attribute.String("socket.path", controlSockHost),
	)
	if err := waitForPath(controlSocketCtx, controlSockHost); err != nil {
		endControlSocketSpan(err)
		return nil, fmt.Errorf("wait for guest control socket: %w", err)
	}
	endControlSocketSpan(nil)
	if err := o.ops.Chmod(ctx, controlSockHost, 0o770); err != nil {
		return nil, fmt.Errorf("chmod guest control socket: %w", err)
	}

	// Telemetry is lease-lifetime; the boot timeout context ends once the VM is ready.
	telemetryBaseCtx := detachedTraceContext(ctx)
	telemetryCtx, telemetryCancel := context.WithCancel(telemetryBaseCtx)
	runtime.cancelTelemetry = telemetryCancel
	runtime.telemetryDone = make(chan struct{})
	go func() {
		defer close(runtime.telemetryDone)
		if err := streamGuestTelemetry(telemetryCtx, controlSockHost, leaseID, observer, logger, telemetryFaultProfile); err != nil && !errors.Is(err, context.Canceled) {
			logger.WarnContext(telemetryBaseCtx, "guest telemetry stream ended", "lease_id", leaseID, "error", err)
		}
	}()

	controlCtx, endControlConnectSpan := startStepSpan(ctx, "vmorchestrator.guest.control_connect",
		attribute.String("lease.id", leaseID),
		attribute.String("socket.path", controlSockHost),
	)
	control, err := connectGuestControl(controlCtx, controlSockHost, vmproto.GuestPort, leaseID)
	endControlConnectSpan(err)
	if err != nil {
		return nil, fmt.Errorf("connect guest control: %w", err)
	}
	runtime.control = control
	runtime.cleanups = append(runtime.cleanups, func() { _ = control.close() })

	_, endHelloSpan := startStepSpan(ctx, "vmorchestrator.guest.hello", attribute.String("lease.id", leaseID))
	hello, err := control.awaitHello(ctx)
	helloObservedAt := time.Now()
	if err != nil {
		endHelloSpan(err)
		return nil, err
	}
	endHelloSpan(nil)
	recordGuestBootTimingSpans(ctx, leaseID, hello, helloObservedAt)
	mountResults, err := control.initLease(ctx, leaseID, netSetup.Lease.GuestNetworkConfig(o.cfg.HostServiceIP, o.cfg.HostServicePort), guestFilesystemMounts(mounts), activationMode)
	runtime.MountResults = filesystemMountResults(mounts, mountResults)
	trace.SpanFromContext(ctx).SetAttributes(attribute.Int("filesystem.result_count", len(runtime.MountResults)))
	if err != nil {
		return nil, err
	}

	cleanupOnErr = false
	return runtime, nil
}

func (o *Orchestrator) activateFirecracker(ctx context.Context, runtime *LeaseRuntime, spec LeaseSpec, mounts []preparedFilesystemMount, client *apiClient, netSetup *networkSetup, bootArgs string) (ActivationMode, error) {
	store := o.snapshotStore()
	if store.Enabled() {
		keyCtx, endKeySpan := startStepSpan(ctx, "vmorchestrator.firecracker.snapshot_key_build",
			attribute.String("lease.id", runtime.LeaseID),
			attribute.String("zfs.dataset", runtime.Dataset),
			attribute.Int("filesystem.mount_count", len(mounts)),
		)
		key, err := o.buildSnapshotKey(keyCtx, runtime.Dataset, spec, mounts, bootArgs)
		if err == nil {
			trace.SpanFromContext(keyCtx).SetAttributes(attribute.String("firecracker.snapshot_key", key.String()))
		}
		endKeySpan(err)
		if err != nil {
			return "", fmt.Errorf("build firecracker snapshot key: %w", err)
		}
		lookupCtx, endLookupSpan := startStepSpan(ctx, "vmorchestrator.firecracker.snapshot_lookup",
			attribute.String("lease.id", runtime.LeaseID),
			attribute.String("firecracker.snapshot_key", key.String()),
		)
		artifact, ok, err := store.Lookup(lookupCtx, key)
		trace.SpanFromContext(lookupCtx).SetAttributes(attribute.Bool("firecracker.snapshot_cache_hit", ok))
		endLookupSpan(err)
		if err != nil {
			return "", fmt.Errorf("lookup firecracker snapshot: %w", err)
		}
		if ok {
			mode, err := o.restoreFirecrackerSnapshot(ctx, runtime, client, netSetup, store, artifact)
			if err != nil {
				return "", err
			}
			trace.SpanFromContext(ctx).SetAttributes(
				attribute.String("firecracker.activation_mode", string(mode)),
				attribute.String("firecracker.snapshot_key", key.String()),
			)
			return mode, nil
		}
		mode, err := o.coldBootFirecracker(ctx, runtime, spec, mounts, client, netSetup, bootArgs)
		if err != nil {
			return "", err
		}
		trace.SpanFromContext(ctx).SetAttributes(
			attribute.String("firecracker.activation_mode", string(mode)),
			attribute.String("firecracker.snapshot_key", key.String()),
			attribute.Bool("firecracker.snapshot_cache_miss", true),
			attribute.Bool("firecracker.snapshot_create_deferred", true),
		)
		return mode, nil
	}
	mode, err := o.coldBootFirecracker(ctx, runtime, spec, mounts, client, netSetup, bootArgs)
	if err != nil {
		return "", err
	}
	trace.SpanFromContext(ctx).SetAttributes(attribute.String("firecracker.activation_mode", string(mode)))
	return mode, nil
}

func (o *Orchestrator) snapshotStore() *SnapshotStore {
	if !o.cfg.FirecrackerSnapshotsEnabled {
		return &SnapshotStore{}
	}
	return NewSnapshotStore(o.cfg.SnapshotCacheDir, o.cfg.JailerUID, o.cfg.JailerGID)
}

func (o *Orchestrator) restoreFirecrackerSnapshot(ctx context.Context, runtime *LeaseRuntime, client *apiClient, netSetup *networkSetup, store *SnapshotStore, artifact SnapshotArtifact) (ActivationMode, error) {
	leaseID := runtime.LeaseID
	stageCtx, endStageSpan := startStepSpan(ctx, "vmorchestrator.firecracker.snapshot_stage",
		attribute.String("lease.id", leaseID),
		attribute.String("firecracker.snapshot_key", artifact.Key),
	)
	paths, cleanup, err := store.StageForJail(stageCtx, artifact, runtime.jailRoot)
	endStageSpan(err)
	if err != nil {
		return "", fmt.Errorf("stage firecracker snapshot: %w", err)
	}
	runtime.cleanups = append(runtime.cleanups, cleanup)

	metricsCtx, cancel := context.WithTimeout(ctx, firecrackerStepTimeout)
	metricsCtx, endMetricsSpan := startStepSpan(metricsCtx, "vmorchestrator.firecracker.restore_metrics",
		attribute.String("lease.id", leaseID),
	)
	metricsErr := client.putMetrics(metricsCtx, "/metrics.json")
	endMetricsSpan(metricsErr)
	cancel()
	if metricsErr != nil {
		return "", fmt.Errorf("configure restore metrics: %w", metricsErr)
	}

	restoreCtx, cancel := context.WithTimeout(ctx, firecrackerStepTimeout)
	restoreCtx, endRestoreSpan := startStepSpan(restoreCtx, "vmorchestrator.firecracker.snapshot_load",
		attribute.String("lease.id", leaseID),
		attribute.String("firecracker.snapshot_key", artifact.Key),
		attribute.String("network.tap", netSetup.Lease.TapName),
	)
	restoreErr := client.loadSnapshot(restoreCtx, paths.StateJailPath, paths.MemJailPath, false, []networkOverrideReq{{
		IfaceID:     snapshotIfaceID,
		HostDevName: netSetup.Lease.TapName,
	}})
	endRestoreSpan(restoreErr)
	cancel()
	if restoreErr != nil {
		return "", fmt.Errorf("load firecracker snapshot: %w", restoreErr)
	}

	resumeCtx, cancel := context.WithTimeout(ctx, firecrackerStepTimeout)
	resumeCtx, endResumeSpan := startStepSpan(resumeCtx, "vmorchestrator.firecracker.snapshot_resume",
		attribute.String("lease.id", leaseID),
	)
	resumeErr := client.patchVM(resumeCtx, "Resumed")
	endResumeSpan(resumeErr)
	cancel()
	if resumeErr != nil {
		return "", fmt.Errorf("resume restored firecracker snapshot: %w", resumeErr)
	}
	return ActivationModeSnapshotRestore, nil
}

func (o *Orchestrator) coldBootFirecracker(ctx context.Context, runtime *LeaseRuntime, spec LeaseSpec, mounts []preparedFilesystemMount, client *apiClient, netSetup *networkSetup, bootArgs string) (ActivationMode, error) {
	leaseID := runtime.LeaseID
	guestMAC := netSetup.Lease.MAC
	if o.cfg.FirecrackerSnapshotsEnabled {
		guestMAC = snapshotGuestMAC
	}
	apiSteps := []firecrackerStep{
		{name: "metrics", fn: func(stepCtx context.Context) error { return client.putMetrics(stepCtx, "/metrics.json") }},
		{name: "boot-source", fn: func(stepCtx context.Context) error { return client.putBootSource(stepCtx, "/vmlinux", bootArgs) }},
		{name: "rootfs", fn: func(stepCtx context.Context) error { return client.putDrive(stepCtx, "rootfs", "/rootfs", true, false) }},
	}
	for _, mount := range mounts {
		mount := mount
		apiSteps = append(apiSteps, firecrackerStep{
			name: "drive-" + mount.DriveID,
			fn: func(stepCtx context.Context) error {
				return client.putDrive(stepCtx, mount.DriveID, mount.JailDevicePath, false, mount.Spec.ReadOnly)
			},
		})
	}
	apiSteps = append(apiSteps, []firecrackerStep{
		{name: "machine-config", fn: func(stepCtx context.Context) error {
			return client.putMachineConfig(stepCtx, int(spec.Resources.VCPUs), int(spec.Resources.MemoryMiB))
		}},
		{name: "network", fn: func(stepCtx context.Context) error {
			return client.putNetworkInterface(stepCtx, snapshotIfaceID, netSetup.Lease.TapName, guestMAC)
		}},
		{name: "vsock", fn: func(stepCtx context.Context) error {
			slotIndex, slotErr := uint32FromInt(netSetup.Lease.SlotIndex, "network slot index")
			if slotErr != nil {
				return slotErr
			}
			cid := slotIndex + 3
			return client.putVsock(stepCtx, cid, "/run/vs-control.sock")
		}},
		{name: "entropy", fn: func(stepCtx context.Context) error { return client.putEntropy(stepCtx) }},
	}...)
	if err := o.configureFirecracker(ctx, leaseID, spec, apiSteps); err != nil {
		return "", err
	}

	startCtx, cancel := context.WithTimeout(ctx, firecrackerStepTimeout)
	startCtx, endStartSpan := startStepSpan(startCtx, "vmorchestrator.firecracker.instance_start",
		attribute.String("lease.id", leaseID),
	)
	startErr := client.startInstance(startCtx)
	endStartSpan(startErr)
	cancel()
	if startErr != nil {
		return "", fmt.Errorf("start VM: %w", startErr)
	}
	return ActivationModeColdBoot, nil
}

func (o *Orchestrator) configureFirecracker(ctx context.Context, leaseID string, spec LeaseSpec, apiSteps []firecrackerStep) error {
	// Roll up the FC API PUTs under a single parent span so dashboards
	// can chart "total configure time" without summing across step children.
	configureCtx, endConfigureAll := startStepSpan(ctx, "vmorchestrator.firecracker.configure_all",
		attribute.String("lease.id", leaseID),
		attribute.Int("firecracker.step_count", len(apiSteps)),
		attribute.Int("vmresources.vcpus", int(spec.Resources.VCPUs)),
		attribute.Int("vmresources.memory_mib", int(spec.Resources.MemoryMiB)),
		attribute.Int("vmresources.root_disk_gib", int(spec.Resources.RootDiskGiB)),
	)
	for _, step := range apiSteps {
		timeout := step.timeout
		if timeout <= 0 {
			timeout = firecrackerStepTimeout
		}
		stepCtx, cancel := context.WithTimeout(configureCtx, timeout)
		stepCtx, endStepSpan := startStepSpan(stepCtx, "vmorchestrator.firecracker.configure",
			attribute.String("lease.id", leaseID),
			attribute.String("firecracker.step", step.name),
			attribute.Int64("firecracker.step_timeout_ms", timeout.Milliseconds()),
		)
		stepErr := step.fn(stepCtx)
		endStepSpan(stepErr)
		cancel()
		if stepErr != nil {
			endConfigureAll(stepErr)
			return fmt.Errorf("configure VM %s: %w", step.name, stepErr)
		}
	}
	endConfigureAll(nil)
	return nil
}

func filesystemMountResults(mounts []preparedFilesystemMount, results []vmproto.FilesystemMountResult) []FilesystemMountResult {
	if len(results) == 0 {
		return nil
	}
	byKey := make(map[string]preparedFilesystemMount, len(mounts))
	for _, mount := range mounts {
		byKey[filesystemMountResultKey(mount.Spec.Name, mount.Spec.MountPath)] = mount
	}
	out := make([]FilesystemMountResult, 0, len(results))
	for _, result := range results {
		mount := byKey[filesystemMountResultKey(result.Name, result.MountPath)]
		out = append(out, FilesystemMountResult{
			Name:        result.Name,
			MountPath:   result.MountPath,
			OperationID: mount.Spec.OperationID,
			Mounted:     result.Mounted,
			Required:    result.Required,
			Error:       strings.TrimSpace(result.Error),
		})
	}
	return out
}

func (r *LeaseRuntime) Exec(ctx context.Context, spec ExecSpec) (ExecResult, error) {
	if r == nil || r.control == nil {
		return ExecResult{}, fmt.Errorf("lease runtime is not ready")
	}
	if err := validateExecSpec(spec); err != nil {
		return ExecResult{}, err
	}
	result, err := r.control.exec(ctx, r.LeaseID, spec, r.logger)
	result.Metrics = parseMetricsFile(r.metricsPath)
	if written, writtenErr := zfs.Written(context.Background(), r.Dataset); writtenErr == nil {
		result.ZFSWritten = written
	}
	if provisioned, volsizeErr := zfs.Volsize(context.Background(), r.Dataset); volsizeErr == nil {
		result.RootfsProvisionedBytes = provisioned
	}
	return result, err
}

func (r *LeaseRuntime) CancelExec(execID, reason string) error {
	if r == nil || r.control == nil {
		return nil
	}
	return r.control.cancelExec(execID, reason)
}

func (r *LeaseRuntime) Cleanup(reason string) {
	if r == nil {
		return
	}
	if r.control != nil {
		_ = r.control.shutdown()
	}
	if r.cancelTelemetry != nil {
		r.cancelTelemetry()
	}
	if r.waitDone != nil {
		select {
		case <-r.waitDone:
			r.jailerExited.Store(true)
		case <-time.After(2 * time.Second):
			if r.jailer != nil {
				_ = r.jailer.Kill()
			}
			<-r.waitDone
			r.jailerExited.Store(true)
		}
	}
	if r.telemetryDone != nil {
		<-r.telemetryDone
	}
	r.logWg.Wait()
	for i := len(r.cleanups) - 1; i >= 0; i-- {
		r.cleanups[i]()
	}
	if r.logger != nil {
		r.logger.Info("lease runtime cleaned up", "lease_id", r.LeaseID, "reason", reason)
	}
}

func waitForSocket(ctx context.Context, path string) error {
	for {
		conn, dialErr := net.DialTimeout("unix", path, 100*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("API socket %s not connectable: %w", path, contextDoneErr(ctx))
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func waitForPath(ctx context.Context, path string) error {
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat %s: %w", path, err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("path %s not present: %w", path, contextDoneErr(ctx))
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func captureSerialOutput(reader io.Reader, wg *sync.WaitGroup) {
	defer wg.Done()
	_, _ = io.Copy(io.Discard, reader)
}
