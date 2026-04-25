package vmorchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type FilesystemCommitResult struct {
	LeaseID         string
	MountName       string
	TargetSourceRef string
	Snapshot        string
	CommittedAt     time.Time
}

func (o *Orchestrator) imageDatasetRoot() string {
	return fmt.Sprintf("%s/%s", o.cfg.Pool, o.cfg.ImageDataset)
}

func (o *Orchestrator) imageDataset(sourceRef string) string {
	return fmt.Sprintf("%s/%s/%s", o.cfg.Pool, o.cfg.ImageDataset, sourceRef)
}

func (r *LeaseRuntime) mountByName(name string) (preparedFilesystemMount, bool) {
	for _, mount := range r.Mounts {
		if mount.Spec.Name == name {
			return mount, true
		}
	}
	return preparedFilesystemMount{}, false
}

func (o *Orchestrator) CommitFilesystemMount(ctx context.Context, runtime *LeaseRuntime, mountName, targetSourceRef string) (FilesystemCommitResult, error) {
	if runtime == nil {
		return FilesystemCommitResult{}, fmt.Errorf("lease runtime is not ready")
	}
	mountName = strings.TrimSpace(mountName)
	targetSourceRef = strings.TrimSpace(targetSourceRef)
	if mountName == "" {
		return FilesystemCommitResult{}, fmt.Errorf("mount name is required")
	}
	if targetSourceRef == "" {
		return FilesystemCommitResult{}, fmt.Errorf("target source ref is required")
	}
	if !filesystemRefPattern.MatchString(targetSourceRef) {
		return FilesystemCommitResult{}, fmt.Errorf("target source ref is invalid")
	}

	ctx = detachedTraceContext(ctx)
	ctx, span := tracer.Start(ctx, "vmorchestrator.filesystem.commit", trace.WithAttributes(
		attribute.String("lease.id", runtime.LeaseID),
		attribute.String("filesystem.name", mountName),
		attribute.String("filesystem.target_source_ref", targetSourceRef),
		attribute.String("filesystem.target_dataset", o.imageDataset(targetSourceRef)),
	))
	var (
		result        FilesystemCommitResult
		err           error
		targetCreated bool
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(otelcodes.Error, err.Error())
		}
		span.End()
	}()

	mount, ok := runtime.mountByName(mountName)
	if !ok {
		err = fmt.Errorf("unknown filesystem mount %q", mountName)
		return FilesystemCommitResult{}, err
	}
	if mount.Spec.ReadOnly {
		err = fmt.Errorf("filesystem mount %q is read-only", mountName)
		return FilesystemCommitResult{}, err
	}
	if runtime.control == nil {
		err = fmt.Errorf("guest control is not available")
		return FilesystemCommitResult{}, err
	}

	targetDataset := o.imageDataset(targetSourceRef)
	targetExists, existsErr := zfsDatasetExists(ctx, targetDataset)
	if existsErr != nil {
		err = fmt.Errorf("check target filesystem dataset %s: %w", targetDataset, existsErr)
		return FilesystemCommitResult{}, err
	}
	if targetExists {
		err = fmt.Errorf("target filesystem dataset %s already exists", targetDataset)
		return FilesystemCommitResult{}, err
	}

	if err = o.ops.ZFSEnsureFilesystem(ctx, o.imageDatasetRoot()); err != nil {
		err = fmt.Errorf("ensure image dataset root: %w", err)
		return FilesystemCommitResult{}, err
	}

	sealCtx, sealEnd := startStepSpan(ctx, "vmorchestrator.guest.filesystem_seal",
		attribute.String("lease.id", runtime.LeaseID),
		attribute.String("filesystem.name", mountName),
		attribute.String("filesystem.mount_path", mount.Spec.MountPath),
	)
	sealErr := runtime.control.sealFilesystem(sealCtx, runtime.LeaseID, mountName, mount.Spec.MountPath)
	sealEnd(sealErr)
	if sealErr != nil {
		err = fmt.Errorf("seal guest filesystem %s: %w", mountName, sealErr)
		return FilesystemCommitResult{}, err
	}

	flushCtx, flushEnd := startStepSpan(ctx, "vmorchestrator.block.flush",
		attribute.String("lease.id", runtime.LeaseID),
		attribute.String("filesystem.name", mountName),
		attribute.String("block.device", mount.HostDevicePath),
	)
	flushErr := o.ops.FlushBlockDevice(flushCtx, mount.HostDevicePath)
	flushEnd(flushErr)
	if flushErr != nil {
		err = fmt.Errorf("flush filesystem mount device %s: %w", mountName, flushErr)
		return FilesystemCommitResult{}, err
	}

	tempSnapshot := "commit-" + strings.ToLower(ulid.Make().String())
	if err = validateZFSSnapshotName(tempSnapshot); err != nil {
		return FilesystemCommitResult{}, err
	}
	tempSnapshotPath := mount.Dataset + "@" + tempSnapshot
	sourceSnapshotCtx, sourceSnapshotEnd := startStepSpan(ctx, "vmorchestrator.zfs.snapshot",
		attribute.String("lease.id", runtime.LeaseID),
		attribute.String("filesystem.name", mountName),
		attribute.String("zfs.dataset", mount.Dataset),
		attribute.String("zfs.snapshot", tempSnapshotPath),
	)
	snapshotProps := map[string]string{
		"vs:lease_id":              runtime.LeaseID,
		"vs:filesystem_mount":      mountName,
		"vs:filesystem_target_ref": targetSourceRef,
		"vs:committed_at":          time.Now().UTC().Format(time.RFC3339Nano),
	}
	if snapErr := o.ops.ZFSSnapshot(sourceSnapshotCtx, mount.Dataset, tempSnapshot, snapshotProps); snapErr != nil {
		sourceSnapshotEnd(snapErr)
		err = fmt.Errorf("snapshot filesystem mount %s: %w", mountName, snapErr)
		return FilesystemCommitResult{}, err
	}
	sourceSnapshotEnd(nil)
	defer func() {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), zfsTimeout)
		defer cancelCleanup()
		if destroyErr := o.ops.ZFSDestroy(cleanupCtx, tempSnapshotPath); destroyErr != nil {
			o.logger.WarnContext(context.Background(), "commit snapshot cleanup failed", "lease_id", runtime.LeaseID, "mount_name", mountName, "snapshot", tempSnapshotPath, "error", destroyErr)
		}
	}()

	sendReceiveCtx, sendReceiveEnd := startStepSpan(ctx, "vmorchestrator.zfs.send_receive",
		attribute.String("lease.id", runtime.LeaseID),
		attribute.String("filesystem.name", mountName),
		attribute.String("zfs.snapshot", tempSnapshotPath),
		attribute.String("zfs.target", targetDataset),
	)
	targetCreated = true
	if sendErr := o.ops.ZFSSendReceive(sendReceiveCtx, tempSnapshotPath, targetDataset); sendErr != nil {
		sendReceiveEnd(sendErr)
		err = fmt.Errorf("send filesystem mount %s into %s: %w", mountName, targetDataset, sendErr)
		return FilesystemCommitResult{}, err
	}
	sendReceiveEnd(nil)
	defer func() {
		if err == nil {
			return
		}
		if !targetCreated {
			return
		}
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), zfsTimeout)
		defer cancelCleanup()
		if destroyErr := o.ops.ZFSDestroyRecursive(cleanupCtx, targetDataset); destroyErr != nil {
			o.logger.WarnContext(context.Background(), "commit target cleanup failed", "lease_id", runtime.LeaseID, "mount_name", mountName, "target_dataset", targetDataset, "error", destroyErr)
		}
	}()

	readySnapshotCtx, readySnapshotEnd := startStepSpan(ctx, "vmorchestrator.zfs.snapshot",
		attribute.String("lease.id", runtime.LeaseID),
		attribute.String("filesystem.name", mountName),
		attribute.String("zfs.dataset", targetDataset),
		attribute.String("zfs.snapshot", targetDataset+"@ready"),
	)
	if readyErr := o.ops.ZFSSnapshot(readySnapshotCtx, targetDataset, "ready", map[string]string{
		"vs:lease_id":              runtime.LeaseID,
		"vs:filesystem_mount":      mountName,
		"vs:filesystem_source_ref": targetSourceRef,
		"vs:committed_at":          time.Now().UTC().Format(time.RFC3339Nano),
	}); readyErr != nil {
		readySnapshotEnd(readyErr)
		err = fmt.Errorf("snapshot committed filesystem %s at %s: %w", mountName, targetDataset+"@ready", readyErr)
		return FilesystemCommitResult{}, err
	}
	readySnapshotEnd(nil)

	result = FilesystemCommitResult{
		LeaseID:         runtime.LeaseID,
		MountName:       mountName,
		TargetSourceRef: targetSourceRef,
		Snapshot:        targetDataset + "@ready",
		CommittedAt:     time.Now().UTC(),
	}
	return result, nil
}
