package zfs

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("vm-orchestrator")

// PrivZFS is the privileged ZFS adapter VolumeLifecycle depends on. The
// parent vm-orchestrator package's DirectPrivOps satisfies this contract;
// the interface exists to narrow the surface the lifecycle types against
// and to keep the privilege-boundary contract expressed where the consumer
// lives.
type PrivZFS interface {
	ZFSClone(ctx context.Context, snapshot, target, leaseID string) error
	ZFSSnapshot(ctx context.Context, dataset, name string, props map[string]string) error
	ZFSDestroy(ctx context.Context, dataset string) error
	ZFSDestroyRecursive(ctx context.Context, dataset string) error
	ZFSEnsureFilesystem(ctx context.Context, dataset string) error
	ZFSSendReceive(ctx context.Context, snapshot, target string) error
	ZFSSetProperty(ctx context.Context, dataset, key, value string) error
	ZFSGetProperty(ctx context.Context, target, key string) (string, error)
	ZFSSnapshotExists(ctx context.Context, snapshot string) (bool, error)
	ZFSDatasetExists(ctx context.Context, dataset string) (bool, error)
	ZFSCreateVolume(ctx context.Context, dataset string, sizeBytes uint64, volblocksize string) error
	ZFSWriteVolumeFromFile(ctx context.Context, devicePath, sourcePath string) (uint64, error)
	ZFSMkfs(ctx context.Context, devicePath, fsType, label string) error
	ZFSRename(ctx context.Context, from, to string) error
	ZFSListChildren(ctx context.Context, dataset string) ([]string, error)
	UnmountStaleZvolMounts(ctx context.Context, pool string) (int, error)
}

// VolumeLifecycle owns the lease-scoped ZFS workflows: clone the golden
// snapshot for a lease root, clone an image snapshot for each mount, and
// destroy disposable workload datasets on cleanup. It does not own commit
// or checkpoint workflows yet; those live in the parent package and migrate
// in a later PR.
type VolumeLifecycle struct {
	roots  Roots
	ops    PrivZFS
	logger *slog.Logger
}

func NewVolumeLifecycle(roots Roots, ops PrivZFS, logger *slog.Logger) *VolumeLifecycle {
	if logger == nil {
		logger = slog.Default()
	}
	return &VolumeLifecycle{roots: roots, ops: ops, logger: logger}
}

// PrepareLeaseRoot asserts boot.Snapshot() exists and clones it into
// lease.RootDataset(). The boot image is the same composable Image type
// every other mount uses; the daemon's default is configured in
// Config.DefaultBootImageRef and resolved by the caller. On error the
// dataset is not created and the caller has nothing to roll back.
func (vl *VolumeLifecycle) PrepareLeaseRoot(ctx context.Context, lease Lease, boot Image) error {
	bootSnap := boot.Snapshot()
	target := lease.RootDataset()

	checkCtx, endCheck := startSpan(ctx, "vmorchestrator.zfs.snapshot_check",
		attribute.String("lease.id", lease.ID()),
		attribute.String("image.ref", boot.SourceRef()),
		attribute.String("zfs.snapshot", bootSnap.String()),
	)
	exists, err := vl.ops.ZFSSnapshotExists(checkCtx, bootSnap.String())
	endCheck(err)
	if err != nil {
		return fmt.Errorf("check boot snapshot: %w", err)
	}
	if !exists {
		return fmt.Errorf("boot snapshot %s does not exist", bootSnap)
	}

	cloneCtx, endClone := startSpan(ctx, "vmorchestrator.zfs.clone",
		attribute.String("lease.id", lease.ID()),
		attribute.String("image.ref", boot.SourceRef()),
		attribute.String("zfs.snapshot", bootSnap.String()),
		attribute.String("zfs.dataset", target),
	)
	cloneErr := vl.ops.ZFSClone(cloneCtx, bootSnap.String(), target, lease.ID())
	endClone(cloneErr)
	if cloneErr != nil {
		return fmt.Errorf("clone zvol: %w", cloneErr)
	}
	return nil
}

// EnsureRoots creates the image and workload parent datasets if they do
// not already exist. Idempotent; safe to call on every daemon startup. The
// firecracker Ansible role used to ensure these via direct zfs(8) shell-out
// — that responsibility now lives here so the privilege contract stays
// "vm-clients socket access plus host root for the daemon."
func (vl *VolumeLifecycle) EnsureRoots(ctx context.Context) error {
	if err := vl.ops.ZFSEnsureFilesystem(ctx, ImageDatasetRoot(vl.roots)); err != nil {
		return fmt.Errorf("ensure image dataset root: %w", err)
	}
	workloadRoot := strings.TrimSuffix(WorkloadPrefix(vl.roots), "/")
	if err := vl.ops.ZFSEnsureFilesystem(ctx, workloadRoot); err != nil {
		return fmt.Errorf("ensure workload dataset root: %w", err)
	}
	return nil
}

// PrepareMount asserts the image's @ready snapshot exists and clones it
// into the per-mount lease dataset. Returns the typed mount clone so the
// caller can derive host/jail/guest device paths without re-stringifying
// the dataset.
func (vl *VolumeLifecycle) PrepareMount(ctx context.Context, lease Lease, image Image, index int, name string) (MountClone, error) {
	source := image.Snapshot()
	clone := lease.Mount(index, name)
	target := clone.Dataset()

	checkCtx, endCheck := startSpan(ctx, "vmorchestrator.zfs.mount_snapshot_check",
		attribute.String("lease.id", lease.ID()),
		attribute.String("filesystem.name", name),
		attribute.String("filesystem.source_ref", image.SourceRef()),
		attribute.String("zfs.snapshot", source.String()),
	)
	exists, err := vl.ops.ZFSSnapshotExists(checkCtx, source.String())
	endCheck(err)
	if err != nil {
		return MountClone{}, fmt.Errorf("check filesystem snapshot %s: %w", source, err)
	}
	if !exists {
		return MountClone{}, fmt.Errorf("filesystem snapshot %s does not exist", source)
	}

	cloneCtx, endClone := startSpan(ctx, "vmorchestrator.zfs.mount_clone",
		attribute.String("lease.id", lease.ID()),
		attribute.String("filesystem.name", name),
		attribute.String("filesystem.source_ref", image.SourceRef()),
		attribute.String("zfs.snapshot", source.String()),
		attribute.String("zfs.dataset", target),
	)
	cloneErr := vl.ops.ZFSClone(cloneCtx, source.String(), target, lease.ID())
	endClone(cloneErr)
	if cloneErr != nil {
		return MountClone{}, fmt.Errorf("clone filesystem zvol %s -> %s: %w", source, target, cloneErr)
	}
	return clone, nil
}

// DestroyLeaseRoot destroys lease.RootDataset(). Containment is structural
// via the Lease type; there is no runtime prefix check.
func (vl *VolumeLifecycle) DestroyLeaseRoot(ctx context.Context, lease Lease) error {
	return vl.ops.ZFSDestroy(ctx, lease.RootDataset())
}

// DestroyMount destroys mount.Dataset(). Containment is structural via the
// MountClone type.
func (vl *VolumeLifecycle) DestroyMount(ctx context.Context, mount MountClone) error {
	return vl.ops.ZFSDestroy(ctx, mount.Dataset())
}

// startSpan opens a child span with the given attributes and returns a
// terminator that records the span outcome on close. Mirrors the parent
// package's startStepSpan helper so trace shapes stay identical.
func startSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, func(error)) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, span := tracer.Start(ctx, name, trace.WithAttributes(attrs...))
	return ctx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
}
