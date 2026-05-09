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

type FilesystemAttachResult struct {
	LeaseID         string
	MountName       string
	SourceRef       string
	MountPath       string
	FSType          string
	ReadOnly        bool
	GuestDevicePath string
	AttachedAt      time.Time
}

func (o *Orchestrator) AttachFilesystemMount(ctx context.Context, runtime *LeaseRuntime, mount FilesystemMount, emptySizeBytes uint64) (result FilesystemAttachResult, retErr error) {
	if runtime == nil {
		return FilesystemAttachResult{}, fmt.Errorf("lease runtime is not ready")
	}
	normalized, normErr := normalizeFilesystemMounts([]FilesystemMount{mount}, filesystemMountSourceOptional)
	if normErr != nil {
		return FilesystemAttachResult{}, normErr
	}
	mount = normalized[0]
	if mount.ReadOnly {
		return FilesystemAttachResult{}, fmt.Errorf("dynamic filesystem mount %q must be writable", mount.Name)
	}
	if emptySizeBytes == 0 {
		emptySizeBytes = defaultCheckpointBytes
	}
	slotIndex := -1
	var slot checkpointDriveSlot
	for idx, candidate := range runtime.Slots {
		if !candidate.InUse {
			slotIndex = idx
			slot = candidate
			break
		}
	}
	if slotIndex < 0 {
		return FilesystemAttachResult{}, fmt.Errorf("no checkpoint drive slots available")
	}
	for _, existing := range runtime.Mounts {
		if existing.Spec.Name == mount.Name {
			return FilesystemAttachResult{}, fmt.Errorf("filesystem mount %q already exists", mount.Name)
		}
	}

	ctx = detachedTraceContext(ctx)
	ctx, span := tracer.Start(ctx, "vmorchestrator.filesystem.attach", trace.WithAttributes(
		attribute.String("lease.id", runtime.LeaseID),
		attribute.String("filesystem.name", mount.Name),
		attribute.String("filesystem.source_ref", mount.SourceRef),
		attribute.String("filesystem.mount_path", mount.MountPath),
		attribute.String("firecracker.drive_id", slot.DriveID),
		attribute.String("guest.device_path", slot.GuestDevicePath),
	))
	defer func() {
		if retErr != nil {
			span.RecordError(retErr)
			span.SetStatus(otelcodes.Error, retErr.Error())
		}
		span.End()
	}()

	mountIndex := len(runtime.Mounts)
	var clone zfs.MountClone
	var prepErr error
	sourceRef := strings.TrimSpace(mount.SourceRef)
	if sourceRef == "" {
		clone, prepErr = o.volumes.PrepareEmptyMount(ctx, runtime.Lease, mountIndex, mount.Name, emptySizeBytes)
	} else {
		// Checkpoint generation refs come in as <volume_dataset>@<gen_name>;
		// Substrate / boot images come in as a bare ref (no '@'). The two
		// share the prepared mount path; only the snapshot resolution
		// differs.
		var snap zfs.Snapshot
		if strings.Contains(sourceRef, "@") {
			gen, parseErr := zfs.ParseGeneration(o.roots, sourceRef)
			if parseErr != nil {
				return FilesystemAttachResult{}, fmt.Errorf("filesystem mount %s source ref: %w", mount.Name, parseErr)
			}
			snap = gen.Snapshot()
		} else {
			image, imgErr := zfs.NewImage(o.roots, sourceRef)
			if imgErr != nil {
				return FilesystemAttachResult{}, fmt.Errorf("filesystem mount %s: %w", mount.Name, imgErr)
			}
			snap = image.Snapshot()
		}
		clone, prepErr = o.volumes.PrepareMountFromSnapshot(ctx, runtime.Lease, snap, mountIndex, mount.Name)
	}
	if prepErr != nil {
		return FilesystemAttachResult{}, prepErr
	}
	cleanupClone := true
	defer func() {
		if cleanupClone {
			_ = o.volumes.DestroyMount(context.Background(), clone)
		}
	}()

	hostDevicePath := zvolDevicePath(clone.Dataset())
	deviceCtx, endDeviceSpan := startStepSpan(ctx, "vmorchestrator.zvol.dynamic_mount_wait_device",
		attribute.String("lease.id", runtime.LeaseID),
		attribute.String("filesystem.name", mount.Name),
		attribute.String("zfs.dataset", clone.Dataset()),
		attribute.String("device.path", hostDevicePath),
	)
	deviceErr := waitForDevice(deviceCtx, hostDevicePath)
	endDeviceSpan(deviceErr)
	if deviceErr != nil {
		return FilesystemAttachResult{}, fmt.Errorf("wait for dynamic filesystem zvol device %s: %w", hostDevicePath, deviceErr)
	}

	if err := o.ops.PlaceJailBlockDevice(ctx, runtime.jailRoot, JailBlockDevice{HostPath: hostDevicePath, JailPath: slot.ActiveJailPath}, o.cfg.JailerUID, o.cfg.JailerGID); err != nil {
		return FilesystemAttachResult{}, err
	}
	client := newAPIClient(runtime.apiSocketPath)
	patchCtx, endPatch := startStepSpan(ctx, "vmorchestrator.firecracker.drive_patch",
		attribute.String("lease.id", runtime.LeaseID),
		attribute.String("firecracker.drive_id", slot.DriveID),
		attribute.String("firecracker.path_on_host", slot.ActiveJailPath),
	)
	patchErr := client.patchDrive(patchCtx, slot.DriveID, slot.ActiveJailPath)
	endPatch(patchErr)
	if patchErr != nil {
		return FilesystemAttachResult{}, fmt.Errorf("patch checkpoint drive %s: %w", slot.DriveID, patchErr)
	}
	prepared := preparedFilesystemMount{
		Spec:            mount,
		DriveID:         slot.DriveID,
		Dataset:         clone.Dataset(),
		HostDevicePath:  hostDevicePath,
		JailDevicePath:  slot.ActiveJailPath,
		GuestDevicePath: slot.GuestDevicePath,
		clone:           clone,
	}
	runtime.Mounts = append(runtime.Mounts, prepared)
	runtime.cleanups = append(runtime.cleanups, func() {
		if destroyErr := o.volumes.DestroyMount(context.Background(), prepared.clone); destroyErr != nil {
			runtime.logger.WarnContext(context.Background(), "dynamic filesystem mount zvol destroy failed", "error", destroyErr, "dataset", prepared.Dataset)
		}
	})
	slot.InUse = true
	slot.MountName = mount.Name
	slot.FilesystemMountID = mountIndex
	runtime.Slots[slotIndex] = slot
	cleanupClone = false

	attachedAt := time.Now().UTC()
	return FilesystemAttachResult{
		LeaseID:         runtime.LeaseID,
		MountName:       mount.Name,
		SourceRef:       mount.SourceRef,
		MountPath:       mount.MountPath,
		FSType:          mount.FSType,
		ReadOnly:        mount.ReadOnly,
		GuestDevicePath: slot.GuestDevicePath,
		AttachedAt:      attachedAt,
	}, nil
}
