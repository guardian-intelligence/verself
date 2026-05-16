package vmorchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/vm-orchestrator/zfs"
)

type FilesystemCommitResult struct {
	LeaseID       string
	MountName     string
	VolumeDataset string
	Snapshot      string
	UsedBytes     uint64
	WrittenBytes  uint64
	CommittedAt   time.Time
}

func (r *LeaseRuntime) mountByName(name string) (preparedFilesystemMount, bool) {
	for _, mount := range r.Mounts {
		if mount.Spec.Name == name {
			return mount, true
		}
	}
	return preparedFilesystemMount{}, false
}

// FilesystemCommitInput parameterizes the golden environment commit. VolumeID
// is the golden scope ID the caller (sandbox-rental) owns in Postgres.
// ParentSnapshotRef is the previous generation's full <dataset>@<snap>
// when the mount was restored from an existing generation; empty for first
// save. The orchestrator picks the new generation snapshot name and
// returns it to the caller.
type FilesystemCommitInput struct {
	MountName         string
	VolumeID          string
	OperationID       string
	ParentSnapshotRef string
	// NewGenerationName is the snapshot short name for the new generation.
	// Caller-supplied so product state and ZFS state share the same identity
	// before the commit runs; if empty, the orchestrator falls back to a
	// gen-<ulid> name (used by tests / direct CLI callers).
	NewGenerationName string
	Journal           func(phase, sourceDatasetRef, workingDatasetRef, sealedDatasetRef, errorMessage string)
}

func (o *Orchestrator) CommitFilesystemMount(ctx context.Context, runtime *LeaseRuntime, input FilesystemCommitInput) (result FilesystemCommitResult, retErr error) {
	if runtime == nil {
		return FilesystemCommitResult{}, fmt.Errorf("lease runtime is not ready")
	}
	mountName := strings.TrimSpace(input.MountName)
	if mountName == "" {
		return FilesystemCommitResult{}, fmt.Errorf("mount name is required")
	}
	volumeID := strings.TrimSpace(input.VolumeID)
	if volumeID == "" {
		return FilesystemCommitResult{}, fmt.Errorf("volume id is required")
	}
	operationID := strings.TrimSpace(input.OperationID)
	if operationID == "" {
		return FilesystemCommitResult{}, fmt.Errorf("operation id is required")
	}
	if !zfs.IsValidRef(operationID) {
		return FilesystemCommitResult{}, fmt.Errorf("operation id is invalid")
	}
	volume, vErr := zfs.NewVolume(o.roots, runtime.Lease.OrgID(), volumeID)
	if vErr != nil {
		return FilesystemCommitResult{}, fmt.Errorf("volume id is invalid: %w", vErr)
	}
	var parent *zfs.Generation
	if ref := strings.TrimSpace(input.ParentSnapshotRef); ref != "" {
		parsed, pErr := zfs.ParseGeneration(o.roots, ref)
		if pErr != nil {
			return FilesystemCommitResult{}, fmt.Errorf("parent snapshot ref: %w", pErr)
		}
		if parsed.Volume().Dataset() != volume.Dataset() {
			return FilesystemCommitResult{}, fmt.Errorf("parent snapshot %s does not belong to volume %s", ref, volume.Dataset())
		}
		parent = &parsed
	}

	ctx = detachedTraceContext(ctx)
	ctx, span := tracer.Start(ctx, "vmorchestrator.filesystem.commit", trace.WithAttributes(
		attribute.String("lease.id", runtime.LeaseID),
		attribute.String("filesystem.name", mountName),
		attribute.String("filesystem.volume_id", volumeID),
		attribute.String("filesystem.operation_id", operationID),
		attribute.String("filesystem.volume_dataset", volume.Dataset()),
		attribute.String("filesystem.parent_snapshot", input.ParentSnapshotRef),
	))
	defer func() {
		if retErr != nil {
			span.RecordError(retErr)
			span.SetStatus(otelcodes.Error, retErr.Error())
		}
		span.End()
	}()

	mount, ok := runtime.mountByName(mountName)
	if !ok {
		return FilesystemCommitResult{}, fmt.Errorf("unknown filesystem mount %q", mountName)
	}
	if mount.Spec.ReadOnly {
		return FilesystemCommitResult{}, fmt.Errorf("filesystem mount %q is read-only", mountName)
	}
	if strings.TrimSpace(mount.Spec.OperationID) != "" && strings.TrimSpace(mount.Spec.OperationID) != operationID {
		return FilesystemCommitResult{}, fmt.Errorf("filesystem mount %q belongs to operation %s, not %s", mountName, mount.Spec.OperationID, operationID)
	}
	if runtime.control == nil {
		return FilesystemCommitResult{}, fmt.Errorf("guest control is not available")
	}
	journal := func(phase, sourceDatasetRef, workingDatasetRef, sealedDatasetRef, errorMessage string) {
		if input.Journal != nil {
			input.Journal(phase, sourceDatasetRef, workingDatasetRef, sealedDatasetRef, errorMessage)
		}
	}

	journal("seal_started", input.ParentSnapshotRef, mount.clone.Dataset(), "", "")
	sealCtx, sealEnd := startStepSpan(ctx, "vmorchestrator.guest.filesystem_seal",
		attribute.String("lease.id", runtime.LeaseID),
		attribute.String("filesystem.name", mountName),
		attribute.String("filesystem.mount_path", mount.Spec.MountPath),
	)
	sealErr := runtime.control.sealFilesystem(sealCtx, runtime.LeaseID, mountName, mount.Spec.MountPath)
	sealEnd(sealErr)
	if sealErr != nil {
		journal("failed", input.ParentSnapshotRef, mount.clone.Dataset(), "", sealErr.Error())
		return FilesystemCommitResult{}, fmt.Errorf("seal guest filesystem %s: %w", mountName, sealErr)
	}

	journal("flush_started", input.ParentSnapshotRef, mount.clone.Dataset(), "", "")
	flushCtx, flushEnd := startStepSpan(ctx, "vmorchestrator.block.flush",
		attribute.String("lease.id", runtime.LeaseID),
		attribute.String("filesystem.name", mountName),
		attribute.String("block.device", mount.HostDevicePath),
	)
	flushErr := o.ops.FlushBlockDevice(flushCtx, mount.HostDevicePath)
	flushEnd(flushErr)
	if flushErr != nil {
		journal("failed", input.ParentSnapshotRef, mount.clone.Dataset(), "", flushErr.Error())
		return FilesystemCommitResult{}, fmt.Errorf("flush filesystem mount device %s: %w", mountName, flushErr)
	}

	journal("zfs_commit_started", input.ParentSnapshotRef, mount.clone.Dataset(), "", "")
	commit, commitErr := o.volumes.Commit(ctx, mount.clone, volume, parent, input.NewGenerationName, operationID)
	if commitErr != nil {
		journal("failed", input.ParentSnapshotRef, mount.clone.Dataset(), "", commitErr.Error())
		return FilesystemCommitResult{}, commitErr
	}
	journal("committed", input.ParentSnapshotRef, mount.clone.Dataset(), commit.NewGeneration.String(), "")
	return FilesystemCommitResult{
		LeaseID:       runtime.LeaseID,
		MountName:     mountName,
		VolumeDataset: volume.Dataset(),
		Snapshot:      commit.NewGeneration.String(),
		UsedBytes:     commit.UsedBytes,
		WrittenBytes:  commit.WrittenBytes,
		CommittedAt:   commit.CommittedAt,
	}, nil
}
