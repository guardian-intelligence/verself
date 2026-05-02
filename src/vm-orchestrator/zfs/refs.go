// Package zfs centralizes typed refs, name validation, property keys, and
// ZFS fact reads used by vm-orchestrator. It does not perform privileged host
// mutations; those live behind PrivOps in the parent package.
package zfs

import "fmt"

// Roots names the host-level dataset slots vm-orchestrator manages. It is
// constructed once at startup from Config and passed by value into refs.
type Roots struct {
	Pool            string // e.g. "vspool"
	ImageDataset    string // e.g. "images"
	WorkloadDataset string // e.g. "workloads"
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
