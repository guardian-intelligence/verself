package zfs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"go.opentelemetry.io/otel/attribute"
)

// CommitResult describes a successful Checkpoint commit. NewGeneration is
// the typed snapshot now living on the volume's persistent dataset.
type CommitResult struct {
	Source        MountClone
	Volume        Volume
	NewGeneration Generation
	CommittedAt   time.Time
	UsedBytes     uint64
	WrittenBytes  uint64
}

// Commit transfers a writable mount clone into the volume's persistent
// snapshot chain. Implementation:
//
//  1. Snapshot the clone with the caller-supplied name + attribution props.
//  2. zfs send / send -i the snapshot into the volume dataset.
//     - First save (no parent): full send creates the volume dataset and
//       lands @<name> as the first generation.
//     - Subsequent saves: incremental send -i <parent_snap> <clone_snap>
//       adds @<name> to the existing chain. Cost is O(delta_bytes).
//  3. Look up used / written bytes from the new snapshot for telemetry.
//
// The clone snapshot on the workload-side is destroyed unconditionally on
// return. If the receive into the volume dataset fails for ANY reason
// (concurrent save lost the receive race, bytes diverge unexpectedly, IO),
// the volume dataset is NOT touched — old generations stay addressable for
// the conflict path. Guest seal and host blockdev flush stay with the
// caller; they are not ZFS concerns.
func (vl *VolumeLifecycle) Commit(ctx context.Context, mount MountClone, volume Volume, parent *Generation, newGenerationName string) (result CommitResult, retErr error) {
	leaseID := mount.Lease().ID()
	mountName := mount.Name()
	volumeDataset := volume.Dataset()

	if err := vl.ops.ZFSEnsureFilesystem(ctx, CheckpointDatasetRoot(vl.roots)); err != nil {
		return CommitResult{}, fmt.Errorf("ensure checkpoint dataset root: %w", err)
	}

	cloneSnapName := strings.TrimSpace(newGenerationName)
	if cloneSnapName == "" {
		cloneSnapName = "gen-" + strings.ToLower(ulid.Make().String())
	}
	if err := ValidateSnapshotName(cloneSnapName); err != nil {
		return CommitResult{}, fmt.Errorf("new generation name: %w", err)
	}
	cloneSnap, err := NewSnapshot(mount.Dataset(), cloneSnapName)
	if err != nil {
		return CommitResult{}, err
	}
	committedAt := time.Now().UTC()
	committedAtStr := committedAt.Format(time.RFC3339Nano)
	parentRef := ""
	if parent != nil {
		parentRef = parent.String()
	}

	snapCtx, endSnap := startSpan(ctx, "vmorchestrator.zfs.commit_snapshot",
		attribute.String("lease.id", leaseID),
		attribute.String("filesystem.name", mountName),
		attribute.String("zfs.dataset", mount.Dataset()),
		attribute.String("zfs.snapshot", cloneSnap.String()),
		attribute.String("zfs.parent_snapshot", parentRef),
	)
	snapErr := vl.ops.ZFSSnapshot(snapCtx, mount.Dataset(), cloneSnapName, map[string]string{
		PropLeaseID:             leaseID,
		PropFilesystemMount:     mountName,
		PropFilesystemTargetRef: volumeDataset,
		PropCommittedAt:         committedAtStr,
	})
	endSnap(snapErr)
	if snapErr != nil {
		return CommitResult{}, fmt.Errorf("snapshot filesystem mount %s: %w", mountName, snapErr)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), Timeout)
		defer cancel()
		if destroyErr := vl.ops.ZFSDestroy(cleanupCtx, cloneSnap.String()); destroyErr != nil {
			vl.logger.WarnContext(context.Background(), "commit clone-side snapshot cleanup failed",
				"lease_id", leaseID,
				"mount_name", mountName,
				"snapshot", cloneSnap.String(),
				"error", destroyErr,
			)
		}
	}()

	sendCtx, endSend := startSpan(ctx, "vmorchestrator.zfs.send_receive",
		attribute.String("lease.id", leaseID),
		attribute.String("filesystem.name", mountName),
		attribute.String("zfs.snapshot", cloneSnap.String()),
		attribute.String("zfs.target_dataset", volumeDataset),
		attribute.String("zfs.parent_snapshot", parentRef),
	)
	sendErr := vl.ops.ZFSSendReceiveIncremental(sendCtx, parentRef, cloneSnap.String(), volumeDataset)
	endSend(sendErr)
	if sendErr != nil {
		return CommitResult{}, fmt.Errorf("send filesystem mount %s -> %s: %w", cloneSnap, volumeDataset, sendErr)
	}

	newGen, err := volume.Generation(cloneSnapName)
	if err != nil {
		return CommitResult{}, err
	}

	usedBytes, _ := vl.snapshotBytes(ctx, newGen.String(), "used")
	writtenBytes, _ := vl.snapshotBytes(ctx, newGen.String(), "written")

	return CommitResult{
		Source:        mount,
		Volume:        volume,
		NewGeneration: newGen,
		CommittedAt:   committedAt,
		UsedBytes:     usedBytes,
		WrittenBytes:  writtenBytes,
	}, nil
}

// snapshotBytes reads a numeric ZFS property from a snapshot. Errors are
// non-fatal — the bytes columns in checkpoint_operations are diagnostic
// telemetry, not control-plane decisions, so a missing value just lands
// as zero.
func (vl *VolumeLifecycle) snapshotBytes(ctx context.Context, snapshot, key string) (uint64, error) {
	value, err := vl.ops.ZFSGetProperty(ctx, snapshot, key)
	if err != nil {
		return 0, err
	}
	value = strings.TrimSpace(value)
	if value == "" || value == "-" {
		return 0, nil
	}
	var out uint64
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("non-numeric zfs %s value %q", key, value)
		}
		out = out*10 + uint64(ch-'0')
	}
	return out, nil
}
