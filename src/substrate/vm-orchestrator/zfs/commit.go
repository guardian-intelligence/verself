package zfs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"go.opentelemetry.io/otel/attribute"
)

// CommitResult describes a successful filesystem commit. The ready snapshot is
// the new immutable @ready cursor on the target image dataset; callers map it
// into their public DTOs.
type CommitResult struct {
	Source        MountClone
	Target        Image
	ReadySnapshot Snapshot
	CommittedAt   time.Time
}

// Commit transfers a writable mount clone into a new immutable image dataset
// in three ZFS phases:
//  1. snapshot the mount with a unique commit-<ulid> name + attribution props
//  2. zfs send | zfs receive into target.Dataset()
//  3. snapshot the target as @ready + attribution props
//
// The temp snapshot is destroyed unconditionally on return. If any step after
// send/receive fails, the partially-created target dataset is recursively
// destroyed so a failed commit leaves no on-disk artifact behind. Guest seal
// and host blockdev flush stay with the caller; they are not ZFS concerns.
func (vl *VolumeLifecycle) Commit(ctx context.Context, mount MountClone, target Image) (result CommitResult, retErr error) {
	leaseID := mount.Lease().ID()
	mountName := mount.Name()
	targetRef := target.SourceRef()
	targetDataset := target.Dataset()

	existsCtx, endExists := startSpan(ctx, "vmorchestrator.zfs.target_check",
		attribute.String("lease.id", leaseID),
		attribute.String("filesystem.name", mountName),
		attribute.String("zfs.target", targetDataset),
	)
	exists, existsErr := vl.ops.ZFSDatasetExists(existsCtx, targetDataset)
	endExists(existsErr)
	if existsErr != nil {
		return CommitResult{}, fmt.Errorf("check target filesystem dataset %s: %w", targetDataset, existsErr)
	}
	if exists {
		return CommitResult{}, fmt.Errorf("target filesystem dataset %s already exists", targetDataset)
	}
	if ensureErr := vl.ops.ZFSEnsureFilesystem(ctx, ImageDatasetRoot(vl.roots)); ensureErr != nil {
		return CommitResult{}, fmt.Errorf("ensure image dataset root: %w", ensureErr)
	}

	tempName := "commit-" + strings.ToLower(ulid.Make().String())
	tempSnap, snapNameErr := NewSnapshot(mount.Dataset(), tempName)
	if snapNameErr != nil {
		return CommitResult{}, snapNameErr
	}
	committedAt := time.Now().UTC()
	committedAtStr := committedAt.Format(time.RFC3339Nano)

	sourceCtx, endSource := startSpan(ctx, "vmorchestrator.zfs.snapshot",
		attribute.String("lease.id", leaseID),
		attribute.String("filesystem.name", mountName),
		attribute.String("zfs.dataset", mount.Dataset()),
		attribute.String("zfs.snapshot", tempSnap.String()),
	)
	sourceErr := vl.ops.ZFSSnapshot(sourceCtx, mount.Dataset(), tempName, map[string]string{
		PropLeaseID:             leaseID,
		PropFilesystemMount:     mountName,
		PropFilesystemTargetRef: targetRef,
		PropCommittedAt:         committedAtStr,
	})
	endSource(sourceErr)
	if sourceErr != nil {
		return CommitResult{}, fmt.Errorf("snapshot filesystem mount %s: %w", mountName, sourceErr)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), Timeout)
		defer cancel()
		if destroyErr := vl.ops.ZFSDestroy(cleanupCtx, tempSnap.String()); destroyErr != nil {
			vl.logger.WarnContext(context.Background(), "commit snapshot cleanup failed",
				"lease_id", leaseID,
				"mount_name", mountName,
				"snapshot", tempSnap.String(),
				"error", destroyErr,
			)
		}
	}()

	sendCtx, endSend := startSpan(ctx, "vmorchestrator.zfs.send_receive",
		attribute.String("lease.id", leaseID),
		attribute.String("filesystem.name", mountName),
		attribute.String("zfs.snapshot", tempSnap.String()),
		attribute.String("zfs.target", targetDataset),
	)
	sendErr := vl.ops.ZFSSendReceive(sendCtx, tempSnap.String(), targetDataset)
	endSend(sendErr)
	if sendErr != nil {
		return CommitResult{}, fmt.Errorf("send filesystem mount %s into %s: %w", mountName, targetDataset, sendErr)
	}
	// Target now exists. Any error before the function returns successfully
	// must not leave a partial dataset on disk; the named-return retErr
	// drives the recursive destroy.
	defer func() {
		if retErr == nil {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), Timeout)
		defer cancel()
		if destroyErr := vl.ops.ZFSDestroyRecursive(cleanupCtx, targetDataset); destroyErr != nil {
			vl.logger.WarnContext(context.Background(), "commit target cleanup failed",
				"lease_id", leaseID,
				"mount_name", mountName,
				"target_dataset", targetDataset,
				"error", destroyErr,
			)
		}
	}()

	readySnap, readyNameErr := NewSnapshot(targetDataset, "ready")
	if readyNameErr != nil {
		return CommitResult{}, readyNameErr
	}
	readyCtx, endReady := startSpan(ctx, "vmorchestrator.zfs.snapshot",
		attribute.String("lease.id", leaseID),
		attribute.String("filesystem.name", mountName),
		attribute.String("zfs.dataset", targetDataset),
		attribute.String("zfs.snapshot", readySnap.String()),
	)
	readyErr := vl.ops.ZFSSnapshot(readyCtx, targetDataset, "ready", map[string]string{
		PropLeaseID:             leaseID,
		PropFilesystemMount:     mountName,
		PropFilesystemSourceRef: targetRef,
		PropCommittedAt:         committedAtStr,
	})
	endReady(readyErr)
	if readyErr != nil {
		return CommitResult{}, fmt.Errorf("snapshot committed filesystem %s at %s: %w", mountName, readySnap, readyErr)
	}

	return CommitResult{
		Source:        mount,
		Target:        target,
		ReadySnapshot: readySnap,
		CommittedAt:   committedAt,
	}, nil
}
