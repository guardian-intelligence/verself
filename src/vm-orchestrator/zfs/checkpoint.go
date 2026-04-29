package zfs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
)

const checkpointSnapshotPrefix = "ckpt-"

// CheckpointResult describes a successful checkpoint snapshot. VersionID is
// the UUID embedded in the snapshot short name (with hyphens stripped) and is
// also the public identifier surfaced in vmproto/vmrpc responses.
type CheckpointResult struct {
	Lease     Lease
	Snapshot  Snapshot
	VersionID string
	Ref       string
	CreatedAt time.Time
}

// Checkpoint takes an immutable snapshot of the lease's root dataset under the
// ckpt-<versionID> short name and stamps it with attribution properties
// (lease id, ref, version, created_at). It is a single-phase ZFS operation
// with no rollback to perform on failure. Authorization (allowed-save-refs
// gate) lives outside this package.
func (vl *VolumeLifecycle) Checkpoint(ctx context.Context, lease Lease, ref string) (CheckpointResult, error) {
	ref = strings.TrimSpace(ref)
	versionID := uuid.NewString()
	snapshotName := checkpointSnapshotPrefix + strings.ReplaceAll(versionID, "-", "")
	snap, err := NewSnapshot(lease.RootDataset(), snapshotName)
	if err != nil {
		return CheckpointResult{}, err
	}

	createdAt := time.Now().UTC()
	snapCtx, end := startSpan(ctx, "vmorchestrator.checkpoint.snapshot",
		attribute.String("lease.id", lease.ID()),
		attribute.String("zfs.dataset", lease.RootDataset()),
		attribute.String("zfs.snapshot", snap.String()),
		attribute.String("checkpoint.ref", ref),
		attribute.String("checkpoint.version", versionID),
	)
	snapCtx, cancel := context.WithTimeout(snapCtx, Timeout)
	defer cancel()
	snapErr := vl.ops.ZFSSnapshot(snapCtx, lease.RootDataset(), snapshotName, map[string]string{
		PropLeaseID:             lease.ID(),
		PropCheckpointRef:       ref,
		PropCheckpointVersion:   versionID,
		PropCheckpointCreated:   createdAt.Format(time.RFC3339Nano),
		PropCheckpointOperation: "save",
	})
	end(snapErr)
	if snapErr != nil {
		return CheckpointResult{}, fmt.Errorf("checkpoint snapshot %s: %w", snap, snapErr)
	}

	return CheckpointResult{
		Lease:     lease,
		Snapshot:  snap,
		VersionID: versionID,
		Ref:       ref,
		CreatedAt: createdAt,
	}, nil
}
