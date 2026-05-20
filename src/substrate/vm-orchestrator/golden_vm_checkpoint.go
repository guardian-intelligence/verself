package vmorchestrator

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/verself/vm-orchestrator/zfs"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const goldenVMControlReconnectSettle = 100 * time.Millisecond

type GoldenVMCheckpointInput struct {
	OperationID  string
	CheckpointID string
}

func (r *LeaseRuntime) goldenCheckpointMountSnapshot(mountName string) (zfs.Snapshot, bool) {
	if r == nil || r.GoldenCheckpoint == nil {
		return zfs.Snapshot{}, false
	}
	for _, mount := range r.GoldenCheckpoint.MountSnapshots {
		if mount.MountName != mountName {
			continue
		}
		snap, err := zfs.ParseSnapshot(mount.SnapshotRef)
		if err != nil {
			return zfs.Snapshot{}, false
		}
		return snap, true
	}
	return zfs.Snapshot{}, false
}

func (o *Orchestrator) CheckpointGoldenVM(ctx context.Context, runtime *LeaseRuntime, input GoldenVMCheckpointInput) (out GoldenVMCheckpointRecord, retErr error) {
	if runtime == nil {
		return GoldenVMCheckpointRecord{}, fmt.Errorf("lease runtime is not ready")
	}
	operationID := strings.TrimSpace(input.OperationID)
	if operationID == "" {
		return GoldenVMCheckpointRecord{}, fmt.Errorf("operation id is required")
	}
	if !zfs.IsValidRef(operationID) {
		return GoldenVMCheckpointRecord{}, fmt.Errorf("operation id is invalid")
	}
	checkpointID := strings.TrimSpace(input.CheckpointID)
	if checkpointID == "" {
		return GoldenVMCheckpointRecord{}, fmt.Errorf("checkpoint id is required")
	}
	if !zfs.IsValidRef(checkpointID) {
		return GoldenVMCheckpointRecord{}, fmt.Errorf("checkpoint id is invalid")
	}
	if runtime.control == nil || runtime.apiClient == nil {
		return GoldenVMCheckpointRecord{}, fmt.Errorf("guest and firecracker controls are required")
	}
	store := o.snapshotStore()
	if !store.Enabled() {
		return GoldenVMCheckpointRecord{}, fmt.Errorf("firecracker snapshot store is disabled")
	}

	ctx = detachedTraceContext(ctx)
	ctx, span := tracer.Start(ctx, "vmorchestrator.golden_vm.checkpoint", trace.WithAttributes(
		attribute.String("lease.id", runtime.LeaseID),
		attribute.String("golden_vm.operation_id", operationID),
		attribute.String("golden_vm.checkpoint_id", checkpointID),
		attribute.Int("filesystem.mount_count", len(runtime.Mounts)),
	))
	defer func() {
		if retErr != nil {
			span.RecordError(retErr)
			span.SetStatus(otelcodes.Error, retErr.Error())
		}
		span.End()
	}()

	beforeCtx, endBeforeSpan := startStepSpan(ctx, "vmorchestrator.guest.before_golden_snapshot",
		attribute.String("lease.id", runtime.LeaseID),
		attribute.String("golden_vm.operation_id", operationID),
	)
	beforeErr := runtime.control.beforeGoldenSnapshot(beforeCtx, runtime.LeaseID, operationID)
	endBeforeSpan(beforeErr)
	if beforeErr != nil {
		return GoldenVMCheckpointRecord{}, fmt.Errorf("before golden snapshot: %w", beforeErr)
	}

	flushCtx, endFlushSpan := startStepSpan(ctx, "vmorchestrator.golden_vm.block_flush",
		attribute.String("lease.id", runtime.LeaseID),
		attribute.Int("filesystem.mount_count", len(runtime.Mounts)),
	)
	flushErr := o.flushCheckpointDevices(flushCtx, runtime)
	endFlushSpan(flushErr)
	if flushErr != nil {
		return GoldenVMCheckpointRecord{}, flushErr
	}
	_ = runtime.control.close()
	runtime.control = nil
	time.Sleep(goldenVMControlReconnectSettle)

	pauseCtx, cancelPause := context.WithTimeout(ctx, firecrackerStepTimeout)
	pauseCtx, endPauseSpan := startStepSpan(pauseCtx, "vmorchestrator.firecracker.pause",
		attribute.String("lease.id", runtime.LeaseID),
	)
	pauseErr := runtime.apiClient.patchVM(pauseCtx, "Paused")
	endPauseSpan(pauseErr)
	cancelPause()
	if pauseErr != nil {
		return GoldenVMCheckpointRecord{}, fmt.Errorf("pause VM: %w", pauseErr)
	}
	paused := true
	defer func() {
		if paused {
			resumeCtx, cancel := context.WithTimeout(context.Background(), firecrackerStepTimeout)
			defer cancel()
			_ = runtime.apiClient.patchVM(resumeCtx, "Resumed")
		}
	}()

	rootSnapshot, rootSnapshotGUID, err := o.snapshotLeaseDataset(ctx, runtime.Dataset, checkpointID, operationID)
	if err != nil {
		return GoldenVMCheckpointRecord{}, fmt.Errorf("snapshot root dataset: %w", err)
	}
	rootVolume, err := zfs.NewVolume(o.roots, runtime.Lease.OrgID(), "vmroot-"+checkpointID)
	if err != nil {
		return GoldenVMCheckpointRecord{}, err
	}
	rootCommit, err := o.volumes.CommitSnapshot(ctx, rootSnapshot, rootVolume, nil, "root", operationID)
	if err != nil {
		return GoldenVMCheckpointRecord{}, fmt.Errorf("commit root checkpoint: %w", err)
	}
	rootSnapshotRef := rootCommit.NewGeneration.String()
	rootSnapshotGUID = firstNonEmpty(rootCommit.SnapshotGUID, rootSnapshotGUID)

	mountSnapshots := make([]GoldenVMMountCheckpoint, 0, len(runtime.Mounts))
	for _, mount := range runtime.Mounts {
		snap, guid, err := o.snapshotLeaseDataset(ctx, mount.clone.Dataset(), checkpointID, operationID)
		if err != nil {
			return GoldenVMCheckpointRecord{}, fmt.Errorf("snapshot mount %s: %w", mount.Spec.Name, err)
		}
		mountSnapshots = append(mountSnapshots, GoldenVMMountCheckpoint{
			MountName:    mount.Spec.Name,
			SnapshotRef:  snap.String(),
			SnapshotGUID: guid,
		})
	}
	runtime.GoldenCheckpoint = &GoldenVMCheckpointRecord{
		LeaseID:        runtime.LeaseID,
		OperationID:    operationID,
		CheckpointID:   checkpointID,
		MountSnapshots: mountSnapshots,
	}

	key := SnapshotKey{value: checkpointID}
	paths, err := store.BuildPaths(ctx, key, runtime.jailRoot)
	if err != nil {
		return GoldenVMCheckpointRecord{}, fmt.Errorf("build snapshot artifact paths: %w", err)
	}
	createCtx, cancelCreate := context.WithTimeout(ctx, snapshotCreateTimeout)
	createCtx, endCreateSpan := startStepSpan(createCtx, "vmorchestrator.firecracker.snapshot_create",
		attribute.String("lease.id", runtime.LeaseID),
		attribute.String("firecracker.snapshot_key", key.String()),
	)
	createErr := runtime.apiClient.createSnapshot(createCtx, paths.StateJailPath, paths.MemJailPath)
	endCreateSpan(createErr)
	cancelCreate()
	if createErr != nil {
		return GoldenVMCheckpointRecord{}, fmt.Errorf("create firecracker snapshot: %w", createErr)
	}
	artifact, err := store.Publish(ctx, key, paths)
	if err != nil {
		return GoldenVMCheckpointRecord{}, fmt.Errorf("publish firecracker snapshot: %w", err)
	}
	stateBytes, memoryBytes, err := snapshotArtifactSizes(artifact)
	if err != nil {
		return GoldenVMCheckpointRecord{}, err
	}

	resumeCtx, cancelResume := context.WithTimeout(ctx, firecrackerStepTimeout)
	resumeCtx, endResumeSpan := startStepSpan(resumeCtx, "vmorchestrator.firecracker.resume_after_checkpoint",
		attribute.String("lease.id", runtime.LeaseID),
	)
	resumeErr := runtime.apiClient.patchVM(resumeCtx, "Resumed")
	endResumeSpan(resumeErr)
	cancelResume()
	if resumeErr != nil {
		return GoldenVMCheckpointRecord{}, fmt.Errorf("resume VM after checkpoint: %w", resumeErr)
	}
	paused = false

	out = GoldenVMCheckpointRecord{
		LeaseID:            runtime.LeaseID,
		OperationID:        operationID,
		CheckpointID:       checkpointID,
		SnapshotKey:        key.String(),
		RootSnapshotRef:    rootSnapshotRef,
		RootSnapshotGUID:   rootSnapshotGUID,
		VMStateArtifactRef: artifact.Key,
		MemoryArtifactRef:  artifact.Key,
		StateBytes:         stateBytes,
		MemoryBytes:        memoryBytes,
		CheckpointedAt:     time.Now().UTC(),
		MountSnapshots:     mountSnapshots,
	}
	runtime.GoldenCheckpoint = &out
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.String("firecracker.snapshot_key", out.SnapshotKey),
		attribute.String("golden_vm.root_snapshot_ref", out.RootSnapshotRef),
		uint64TraceAttribute("firecracker.snapshot_state_bytes", out.StateBytes),
		uint64TraceAttribute("firecracker.snapshot_memory_bytes", out.MemoryBytes),
	)
	return out, nil
}

func (o *Orchestrator) flushCheckpointDevices(ctx context.Context, runtime *LeaseRuntime) error {
	if err := o.ops.FlushBlockDevice(ctx, zvolDevicePath(runtime.Dataset)); err != nil {
		return fmt.Errorf("flush root block device: %w", err)
	}
	for _, mount := range runtime.Mounts {
		if err := o.ops.FlushBlockDevice(ctx, mount.HostDevicePath); err != nil {
			return fmt.Errorf("flush filesystem mount %s: %w", mount.Spec.Name, err)
		}
	}
	return nil
}

func (o *Orchestrator) snapshotLeaseDataset(ctx context.Context, dataset, checkpointID, operationID string) (zfs.Snapshot, string, error) {
	snapshotName := "checkpoint-" + checkpointID
	snap, err := zfs.NewSnapshot(dataset, snapshotName)
	if err != nil {
		return zfs.Snapshot{}, "", err
	}
	snapCtx, endSnapSpan := startStepSpan(ctx, "vmorchestrator.zfs.checkpoint_snapshot",
		attribute.String("zfs.dataset", dataset),
		attribute.String("zfs.snapshot", snap.String()),
		attribute.String("golden_vm.operation_id", operationID),
	)
	snapErr := o.ops.ZFSSnapshot(snapCtx, dataset, snapshotName, map[string]string{
		"vs:operation_id": operationID,
		"vs:checkpoint":   checkpointID,
	})
	endSnapSpan(snapErr)
	if snapErr != nil {
		return zfs.Snapshot{}, "", snapErr
	}
	guid, err := o.snapshotGUID(ctx, snap.String())
	if err != nil {
		return zfs.Snapshot{}, "", err
	}
	return snap, guid, nil
}

func snapshotArtifactSizes(artifact SnapshotArtifact) (uint64, uint64, error) {
	stateInfo, err := os.Stat(artifact.StatePath)
	if err != nil {
		return 0, 0, fmt.Errorf("stat snapshot state artifact: %w", err)
	}
	memInfo, err := os.Stat(artifact.MemPath)
	if err != nil {
		return 0, 0, fmt.Errorf("stat snapshot memory artifact: %w", err)
	}
	if stateInfo.Size() < 0 || memInfo.Size() < 0 {
		return 0, 0, fmt.Errorf("snapshot artifact size is negative")
	}
	stateBytes, err := uint64FromNonNegativeInt64(stateInfo.Size(), "snapshot state artifact bytes")
	if err != nil {
		return 0, 0, err
	}
	memBytes, err := uint64FromNonNegativeInt64(memInfo.Size(), "snapshot memory artifact bytes")
	if err != nil {
		return 0, 0, err
	}
	return stateBytes, memBytes, nil
}
