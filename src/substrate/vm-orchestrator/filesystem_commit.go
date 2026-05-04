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
	LeaseID         string
	MountName       string
	TargetSourceRef string
	Snapshot        string
	CommittedAt     time.Time
}

func (r *LeaseRuntime) mountByName(name string) (preparedFilesystemMount, bool) {
	for _, mount := range r.Mounts {
		if mount.Spec.Name == name {
			return mount, true
		}
	}
	return preparedFilesystemMount{}, false
}

func (o *Orchestrator) CommitFilesystemMount(ctx context.Context, runtime *LeaseRuntime, mountName, targetSourceRef string) (result FilesystemCommitResult, retErr error) {
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
	targetImage, imgErr := zfs.NewImage(o.roots, targetSourceRef)
	if imgErr != nil {
		return FilesystemCommitResult{}, fmt.Errorf("target source ref is invalid")
	}

	ctx = detachedTraceContext(ctx)
	ctx, span := tracer.Start(ctx, "vmorchestrator.filesystem.commit", trace.WithAttributes(
		attribute.String("lease.id", runtime.LeaseID),
		attribute.String("filesystem.name", mountName),
		attribute.String("filesystem.target_source_ref", targetSourceRef),
		attribute.String("filesystem.target_dataset", targetImage.Dataset()),
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
	if runtime.control == nil {
		return FilesystemCommitResult{}, fmt.Errorf("guest control is not available")
	}

	sealCtx, sealEnd := startStepSpan(ctx, "vmorchestrator.guest.filesystem_seal",
		attribute.String("lease.id", runtime.LeaseID),
		attribute.String("filesystem.name", mountName),
		attribute.String("filesystem.mount_path", mount.Spec.MountPath),
	)
	sealErr := runtime.control.sealFilesystem(sealCtx, runtime.LeaseID, mountName, mount.Spec.MountPath)
	sealEnd(sealErr)
	if sealErr != nil {
		return FilesystemCommitResult{}, fmt.Errorf("seal guest filesystem %s: %w", mountName, sealErr)
	}

	flushCtx, flushEnd := startStepSpan(ctx, "vmorchestrator.block.flush",
		attribute.String("lease.id", runtime.LeaseID),
		attribute.String("filesystem.name", mountName),
		attribute.String("block.device", mount.HostDevicePath),
	)
	flushErr := o.ops.FlushBlockDevice(flushCtx, mount.HostDevicePath)
	flushEnd(flushErr)
	if flushErr != nil {
		return FilesystemCommitResult{}, fmt.Errorf("flush filesystem mount device %s: %w", mountName, flushErr)
	}

	commit, commitErr := o.volumes.Commit(ctx, mount.clone, targetImage)
	if commitErr != nil {
		return FilesystemCommitResult{}, commitErr
	}
	return FilesystemCommitResult{
		LeaseID:         runtime.LeaseID,
		MountName:       mountName,
		TargetSourceRef: targetSourceRef,
		Snapshot:        commit.ReadySnapshot.String(),
		CommittedAt:     commit.CommittedAt,
	}, nil
}
