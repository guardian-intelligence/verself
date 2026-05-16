package zfs

import (
	"context"
	"errors"
	"testing"
)

type lifecycleOps struct {
	ensures []string
	clones  []string
	creates []string
	sets    []string
	mkfs    []string
	usedErr error
}

func (o *lifecycleOps) ZFSClone(_ context.Context, snapshot, target, _ string) error {
	o.clones = append(o.clones, snapshot+" -> "+target)
	return nil
}

func (o *lifecycleOps) ZFSSnapshot(context.Context, string, string, map[string]string) error {
	return nil
}

func (o *lifecycleOps) ZFSDestroy(context.Context, string) error { return nil }

func (o *lifecycleOps) ZFSDestroyRecursive(context.Context, string) error { return nil }

func (o *lifecycleOps) ZFSEnsureFilesystem(_ context.Context, dataset string) error {
	o.ensures = append(o.ensures, dataset)
	return nil
}

func (o *lifecycleOps) ZFSSnapshotExists(context.Context, string) (bool, error) {
	return false, nil
}

func (o *lifecycleOps) ZFSDatasetExists(context.Context, string) (bool, error) {
	return false, nil
}

func (o *lifecycleOps) ZFSSetProperty(_ context.Context, dataset, key, value string) error {
	o.sets = append(o.sets, dataset+" "+key+"="+value)
	return nil
}

func (o *lifecycleOps) ZFSGetProperty(context.Context, string, string) (string, error) {
	return "", nil
}

func (o *lifecycleOps) ZFSCreateVolume(_ context.Context, dataset string, _ uint64, _ string) error {
	o.creates = append(o.creates, dataset)
	return nil
}

func (o *lifecycleOps) ZFSCreateSparseVolume(_ context.Context, dataset string, _ uint64, _ string) error {
	o.creates = append(o.creates, dataset)
	return nil
}

func (o *lifecycleOps) ZFSWriteVolumeFromFile(context.Context, string, string) (uint64, error) {
	return 0, nil
}

func (o *lifecycleOps) ZFSMkfs(_ context.Context, devicePath, _, _ string) error {
	o.mkfs = append(o.mkfs, devicePath)
	return nil
}

func (o *lifecycleOps) ZFSEnsureVolumeSizeExt4(context.Context, string, uint64) error {
	return nil
}

func (o *lifecycleOps) ZFSRename(context.Context, string, string) error { return nil }

func (o *lifecycleOps) ZFSPromote(context.Context, string) error { return nil }

func (o *lifecycleOps) ZFSListChildren(context.Context, string) ([]string, error) {
	return nil, nil
}

func (o *lifecycleOps) ZFSUsed(context.Context, string) (uint64, error) {
	return 0, o.usedErr
}

func (o *lifecycleOps) ZFSWritten(context.Context, string) (uint64, error) {
	return 0, nil
}

func (o *lifecycleOps) UnmountStaleZvolMounts(context.Context, string) (int, error) {
	return 0, nil
}

func TestPrepareSubstrateCloneEnsuresLeaseDatasetParent(t *testing.T) {
	roots := Roots{Pool: "pool", ImageDataset: "images", GoldenDataset: "goldens", WorkloadDataset: "workloads"}
	ops := &lifecycleOps{}
	lifecycle := NewVolumeLifecycle(roots, ops, nil)
	lease, err := NewLease(roots, "org_a", "lease-a")
	if err != nil {
		t.Fatal(err)
	}
	image, err := NewImage(roots, "substrate")
	if err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.PrepareSubstrateClone(context.Background(), lease, image); err != nil {
		t.Fatal(err)
	}
	if got, want := ops.ensures[0], "pool/orgs/org_a/workloads/lease-a"; got != want {
		t.Fatalf("ensured dataset = %q, want %q", got, want)
	}
	if got, want := ops.clones[0], "pool/images/substrate@ready -> pool/orgs/org_a/workloads/lease-a/root"; got != want {
		t.Fatalf("clone = %q, want %q", got, want)
	}
}

func TestPrepareFilesystemMountsEnsureMountParent(t *testing.T) {
	roots := Roots{Pool: "pool", ImageDataset: "images", GoldenDataset: "goldens", WorkloadDataset: "workloads"}
	ops := &lifecycleOps{}
	lifecycle := NewVolumeLifecycle(roots, ops, nil)
	lease, err := NewLease(roots, "org_a", "lease-a")
	if err != nil {
		t.Fatal(err)
	}
	image, err := NewImage(roots, "gh-actions-runner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.PrepareMount(context.Background(), lease, image, 0, "toolchain", "op-a"); err != nil {
		t.Fatal(err)
	}
	if got, want := ops.ensures[0], "pool/orgs/org_a/workloads/lease-a/mounts"; got != want {
		t.Fatalf("ensured cloned mount parent = %q, want %q", got, want)
	}
	if _, err := lifecycle.PrepareEmptyMount(context.Background(), lease, 1, "workspace", "op-b"); err != nil {
		t.Fatal(err)
	}
	if got, want := ops.ensures[1], "pool/orgs/org_a/workloads/lease-a/mounts"; got != want {
		t.Fatalf("ensured empty mount parent = %q, want %q", got, want)
	}
}

func TestCommitReturnsStatsErrors(t *testing.T) {
	roots := Roots{Pool: "pool", ImageDataset: "images", GoldenDataset: "goldens", WorkloadDataset: "workloads"}
	ops := &lifecycleOps{usedErr: errors.New("zfs used failed")}
	lifecycle := NewVolumeLifecycle(roots, ops, nil)
	lease, err := NewLease(roots, "org_a", "lease-a")
	if err != nil {
		t.Fatal(err)
	}
	cloneDataset, err := lease.MountDataset(0, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	clone := MountClone{lease: lease, dataset: cloneDataset, name: "workspace"}
	volume, err := NewVolume(roots, "org_a", "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	_, err = lifecycle.Commit(context.Background(), clone, volume, nil, "generation-a", "op-a")
	if err == nil {
		t.Fatal("Commit returned nil error for zfs stats failure")
	}
}
