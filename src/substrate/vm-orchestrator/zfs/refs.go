// Package zfs centralizes typed refs, name validation, property keys, and
// ZFS fact reads used by vm-orchestrator. It does not perform privileged host
// mutations; those live behind PrivOps in the parent package.
package zfs

import (
	"fmt"
	"strings"
)

// Roots names the host-level dataset slots vm-orchestrator manages. It is
// constructed once at startup from Config and passed by value into refs.
//
// CheckpointDataset is the parent under which per-volume snapshot-chain
// datasets live (vspool/checkpoints/<volume_id>). ImageDataset still hosts
// the immutable substrate / boot images (vspool/images/<source_ref>) that
// are seeded once and used as the boot rootfs origin; that abstraction is
// architecturally separate from customer Checkpoint volumes and stays
// per-image with a single @ready snapshot.
type Roots struct {
	Pool              string // e.g. "vspool"
	ImageDataset      string // e.g. "images"
	CheckpointDataset string // e.g. "checkpoints"
	WorkloadDataset   string // e.g. "workloads"
}

// Image refs an immutable composable base by service-authorized source ref.
// The source ref is validated against the ref pattern at construction.
type Image struct {
	roots Roots
	ref   string
}

func NewImage(roots Roots, sourceRef string) (Image, error) {
	if err := ValidateRef(sourceRef); err != nil {
		return Image{}, fmt.Errorf("image source ref: %w", err)
	}
	return Image{roots: roots, ref: sourceRef}, nil
}

func (i Image) Dataset() string { return i.roots.Pool + "/" + i.roots.ImageDataset + "/" + i.ref }
func (i Image) Snapshot() Snapshot {
	return Snapshot{dataset: i.Dataset(), name: "ready"}
}
func (i Image) SourceRef() string { return i.ref }

// Lease refs the writable workload subtree for one running VM lease. The
// lease ID is validated against the ref pattern at construction.
type Lease struct {
	roots Roots
	id    string
}

func NewLease(roots Roots, leaseID string) (Lease, error) {
	if err := ValidateRef(leaseID); err != nil {
		return Lease{}, fmt.Errorf("lease id: %w", err)
	}
	return Lease{roots: roots, id: leaseID}, nil
}

func (l Lease) RootDataset() string {
	return l.roots.Pool + "/" + l.roots.WorkloadDataset + "/" + l.id
}

// MountDataset names the per-mount workload clone for the lease. The mount
// name is sanitized; the index is zero-padded so dataset names sort lexically.
func (l Lease) MountDataset(index int, name string) string {
	return fmt.Sprintf("%s/%s/%s-fs-%02d-%s", l.roots.Pool, l.roots.WorkloadDataset, l.id, index, SanitizeComponent(name))
}

func (l Lease) ID() string { return l.id }

// Mount produces a typed ref for one filesystem clone of this lease. The
// resulting MountClone carries the lease so cleanup paths cannot destroy
// datasets outside the lease's workload subtree.
func (l Lease) Mount(index int, name string) MountClone {
	return MountClone{lease: l, index: index, name: name}
}

// MountClone refs one writable per-mount clone for a lease. Constructed only
// via Lease.Mount, so the dataset path is always under the workload prefix
// for that lease.
type MountClone struct {
	lease Lease
	index int
	name  string
}

func (m MountClone) Dataset() string { return m.lease.MountDataset(m.index, m.name) }
func (m MountClone) Lease() Lease    { return m.lease }
func (m MountClone) Index() int      { return m.index }
func (m MountClone) Name() string    { return m.name }

// WorkloadPrefix is the path prefix that bounds disposable lease datasets.
// Cleanup paths use it to assert containment before destructive ZFS ops.
func WorkloadPrefix(roots Roots) string {
	return roots.Pool + "/" + roots.WorkloadDataset + "/"
}

// ImageDatasetRoot is the parent dataset under which committed image
// datasets live. ZFSEnsureFilesystem on this path creates it if missing.
func ImageDatasetRoot(roots Roots) string {
	return roots.Pool + "/" + roots.ImageDataset
}

// CheckpointDatasetRoot is the parent dataset under which per-volume
// Checkpoint datasets live. Each volume_id maps to one persistent dataset
// here whose snapshots represent the generation chain.
func CheckpointDatasetRoot(roots Roots) string {
	return roots.Pool + "/" + roots.CheckpointDataset
}

// Volume refs the persistent ZFS dataset for one Checkpoint volume.
// Generations are snapshots on this dataset; the dataset itself is never
// written to directly (only via `zfs recv` from a clone snapshot).
type Volume struct {
	roots    Roots
	volumeID string
}

// NewVolume validates the volume id against the ref alphabet and returns a
// typed Volume. Volume IDs are UUID strings in product code, but the type
// accepts the broader ref alphabet so refactors that change the id format
// don't ripple here.
func NewVolume(roots Roots, volumeID string) (Volume, error) {
	if err := ValidateRef(volumeID); err != nil {
		return Volume{}, fmt.Errorf("volume id: %w", err)
	}
	return Volume{roots: roots, volumeID: volumeID}, nil
}

// Dataset returns the persistent ZFS dataset path for this volume.
func (v Volume) Dataset() string {
	return CheckpointDatasetRoot(v.roots) + "/" + v.volumeID
}

// VolumeID returns the customer-visible volume identifier.
func (v Volume) VolumeID() string { return v.volumeID }

// Generation builds a typed generation snapshot for this volume. The short
// name must match the snapshot name pattern (no '@' or '/'); callers
// usually pass "gen-<ulid>" produced by Commit.
func (v Volume) Generation(name string) (Generation, error) {
	snap, err := NewSnapshot(v.Dataset(), name)
	if err != nil {
		return Generation{}, err
	}
	return Generation{volume: v, snapshot: snap}, nil
}

// Generation is a typed snapshot on a Volume. It is the on-disk identity of
// one Checkpoint generation: <volume_dataset>@<gen_name>.
type Generation struct {
	volume   Volume
	snapshot Snapshot
}

func (g Generation) Volume() Volume     { return g.volume }
func (g Generation) Snapshot() Snapshot { return g.snapshot }
func (g Generation) Name() string       { return g.snapshot.name }
func (g Generation) String() string     { return g.snapshot.String() }

// ParseGeneration parses "<dataset>@<name>" from product state and asserts
// the dataset lives under the checkpoint prefix for this Roots. Returns an
// error if the prefix doesn't match or either component is malformed.
func ParseGeneration(roots Roots, ref string) (Generation, error) {
	dataset, name, ok := strings.Cut(ref, "@")
	if !ok {
		return Generation{}, fmt.Errorf("invalid generation ref %q: missing '@'", ref)
	}
	prefix := CheckpointDatasetRoot(roots) + "/"
	if !strings.HasPrefix(dataset, prefix) {
		return Generation{}, fmt.Errorf("invalid generation ref %q: not under checkpoint root", ref)
	}
	volumeID := strings.TrimPrefix(dataset, prefix)
	if strings.ContainsRune(volumeID, '/') {
		return Generation{}, fmt.Errorf("invalid generation ref %q: nested checkpoint dataset", ref)
	}
	v, err := NewVolume(roots, volumeID)
	if err != nil {
		return Generation{}, err
	}
	return v.Generation(name)
}

// Snapshot is a typed dataset@name pair. Constructed only via Image, Lease,
// or NewSnapshot so the name is always validated.
type Snapshot struct {
	dataset string
	name    string
}

func NewSnapshot(dataset, name string) (Snapshot, error) {
	if err := ValidateSnapshotName(name); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{dataset: dataset, name: name}, nil
}

func (s Snapshot) Dataset() string { return s.dataset }
func (s Snapshot) Name() string    { return s.name }
func (s Snapshot) String() string  { return s.dataset + "@" + s.name }
