package zfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type lifecycleOps struct {
	ensures  []string
	clones   []string
	creates  []string
	destroys []string
	graphs   []string
	sets     []string
	mkfs     []string
	receives []string
	usedErr  error

	datasets   map[string]bool
	snapshots  map[string]bool
	properties map[string]string
}

func (o *lifecycleOps) ZFSClone(_ context.Context, snapshot, target, _ string) error {
	o.clones = append(o.clones, snapshot+" -> "+target)
	return nil
}

func (o *lifecycleOps) ZFSSnapshot(context.Context, string, string, map[string]string) error {
	return nil
}

func (o *lifecycleOps) ZFSDestroy(context.Context, string) error { return nil }

func (o *lifecycleOps) ZFSDestroyDatasetTree(_ context.Context, dataset string) error {
	o.destroys = append(o.destroys, dataset)
	for child := range o.datasets {
		if child == dataset || strings.HasPrefix(child, dataset+"/") {
			delete(o.datasets, child)
		}
	}
	for snapshot := range o.snapshots {
		if strings.HasPrefix(snapshot, dataset+"@") || strings.HasPrefix(snapshot, dataset+"/") {
			delete(o.snapshots, snapshot)
		}
	}
	return nil
}

func (o *lifecycleOps) ZFSDestroyDependencyGraph(ctx context.Context, dataset string) error {
	o.graphs = append(o.graphs, dataset)
	return o.ZFSDestroyDatasetTree(ctx, dataset)
}

func (o *lifecycleOps) ZFSCreateEncryptedFilesystem(_ context.Context, dataset string, _ []byte) error {
	if o.datasets == nil {
		o.datasets = map[string]bool{}
	}
	o.datasets[dataset] = true
	return nil
}

func (o *lifecycleOps) ZFSEnsureFilesystem(_ context.Context, dataset string) error {
	o.ensures = append(o.ensures, dataset)
	if o.datasets == nil {
		o.datasets = map[string]bool{}
	}
	o.datasets[dataset] = true
	return nil
}

func (o *lifecycleOps) ZFSLoadKey(context.Context, string, []byte) error { return nil }

func (o *lifecycleOps) ZFSUnloadKey(context.Context, string) error { return nil }

func (o *lifecycleOps) ZFSSnapshotExists(_ context.Context, snapshot string) (bool, error) {
	if strings.HasPrefix(snapshot, "pool/images/") {
		return true, nil
	}
	return o.snapshots[snapshot], nil
}

func (o *lifecycleOps) ZFSDatasetExists(_ context.Context, dataset string) (bool, error) {
	return o.datasets[dataset], nil
}

func (o *lifecycleOps) ZFSSetProperty(_ context.Context, dataset, key, value string) error {
	o.sets = append(o.sets, dataset+" "+key+"="+value)
	if o.properties == nil {
		o.properties = map[string]string{}
	}
	o.properties[propertyKey(dataset, key)] = value
	return nil
}

func (o *lifecycleOps) ZFSGetProperty(_ context.Context, target, key string) (string, error) {
	if o.properties != nil {
		if value, ok := o.properties[propertyKey(target, key)]; ok {
			return value, nil
		}
	}
	switch key {
	case sourceDigestProperty:
		if strings.HasPrefix(target, "pool/images/") {
			return "aaaaaaaa", nil
		}
		return "", nil
	case "encryption":
		return "aes-256-gcm", nil
	case "encryptionroot":
		const prefix = "pool/orgs/"
		if strings.HasPrefix(target, prefix) {
			parts := strings.Split(strings.TrimPrefix(target, prefix), "/")
			if len(parts) > 0 && parts[0] != "" {
				return prefix + parts[0], nil
			}
		}
		return "", nil
	case "keystatus":
		return "available", nil
	}
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

func (o *lifecycleOps) ZFSReceiveSnapshot(_ context.Context, sourceSnapshot, targetDataset string) error {
	o.receives = append(o.receives, sourceSnapshot+" -> "+targetDataset)
	if o.datasets == nil {
		o.datasets = map[string]bool{}
	}
	if o.snapshots == nil {
		o.snapshots = map[string]bool{}
	}
	o.datasets[targetDataset] = true
	o.snapshots[targetDataset+"@ready"] = true
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
	if !containsString(ops.ensures, "pool/orgs/org_a/workloads/lease-a") {
		t.Fatalf("lease dataset parent was not ensured: %#v", ops.ensures)
	}
	if got, want := ops.receives[0], "pool/images/substrate@ready -> pool/orgs/org_a/images/substrate-aaaaaaaa"; got != want {
		t.Fatalf("receive = %q, want %q", got, want)
	}
	if !containsString(ops.sets, "pool/orgs/org_a/images/substrate-aaaaaaaa@ready vs:source_digest=aaaaaaaa") {
		t.Fatalf("org image source digest was not set: %#v", ops.sets)
	}
	if got, want := ops.clones[0], "pool/orgs/org_a/images/substrate-aaaaaaaa@ready -> pool/orgs/org_a/workloads/lease-a/root"; got != want {
		t.Fatalf("clone = %q, want %q", got, want)
	}
}

func TestEnsureOrgImageReusesMatchingDigest(t *testing.T) {
	roots := Roots{Pool: "pool", ImageDataset: "images", GoldenDataset: "goldens", WorkloadDataset: "workloads"}
	ops := &lifecycleOps{
		datasets: map[string]bool{
			"pool/orgs/org_a/images/substrate-aaaaaaaa": true,
		},
		snapshots: map[string]bool{
			"pool/orgs/org_a/images/substrate-aaaaaaaa@ready": true,
		},
		properties: map[string]string{
			propertyKey("pool/images/substrate@ready", sourceDigestProperty):                     "aaaaaaaa",
			propertyKey("pool/orgs/org_a/images/substrate-aaaaaaaa@ready", sourceDigestProperty): "aaaaaaaa",
		},
	}
	lifecycle := NewVolumeLifecycle(roots, ops, nil)
	image, err := NewImage(roots, "substrate")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := lifecycle.EnsureOrgImage(context.Background(), "org_a", image)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := snapshot.String(), "pool/orgs/org_a/images/substrate-aaaaaaaa@ready"; got != want {
		t.Fatalf("snapshot = %q, want %q", got, want)
	}
	if len(ops.receives) != 0 {
		t.Fatalf("received despite matching digest: %#v", ops.receives)
	}
	if len(ops.destroys) != 0 {
		t.Fatalf("destroyed despite matching digest: %#v", ops.destroys)
	}
}

func TestEnsureOrgImageLeavesOlderDigestCopyInPlace(t *testing.T) {
	roots := Roots{Pool: "pool", ImageDataset: "images", GoldenDataset: "goldens", WorkloadDataset: "workloads"}
	ops := &lifecycleOps{
		datasets: map[string]bool{
			"pool/orgs/org_a/images/substrate-aaaaaaaa": true,
		},
		snapshots: map[string]bool{
			"pool/orgs/org_a/images/substrate-aaaaaaaa@ready": true,
		},
		properties: map[string]string{
			propertyKey("pool/images/substrate@ready", sourceDigestProperty):                     "bbbbbbbb",
			propertyKey("pool/orgs/org_a/images/substrate-aaaaaaaa@ready", sourceDigestProperty): "aaaaaaaa",
		},
	}
	lifecycle := NewVolumeLifecycle(roots, ops, nil)
	image, err := NewImage(roots, "substrate")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.EnsureOrgImage(context.Background(), "org_a", image); err != nil {
		t.Fatal(err)
	}
	if len(ops.destroys) != 0 {
		t.Fatalf("destroyed older digest copy: %#v", ops.destroys)
	}
	if got, want := ops.receives[0], "pool/images/substrate@ready -> pool/orgs/org_a/images/substrate-bbbbbbbb"; got != want {
		t.Fatalf("receive = %q, want %q", got, want)
	}
	if !containsString(ops.sets, "pool/orgs/org_a/images/substrate-bbbbbbbb@ready vs:source_digest=bbbbbbbb") {
		t.Fatalf("new digest was not set: %#v", ops.sets)
	}
}

func TestEnsureOrgImageRefreshesMismatchedTargetDigest(t *testing.T) {
	roots := Roots{Pool: "pool", ImageDataset: "images", GoldenDataset: "goldens", WorkloadDataset: "workloads"}
	ops := &lifecycleOps{
		datasets: map[string]bool{
			"pool/orgs/org_a/images/substrate-bbbbbbbb": true,
		},
		snapshots: map[string]bool{
			"pool/orgs/org_a/images/substrate-bbbbbbbb@ready": true,
		},
		properties: map[string]string{
			propertyKey("pool/images/substrate@ready", sourceDigestProperty):                     "bbbbbbbb",
			propertyKey("pool/orgs/org_a/images/substrate-bbbbbbbb@ready", sourceDigestProperty): "aaaaaaaa",
		},
	}
	lifecycle := NewVolumeLifecycle(roots, ops, nil)
	image, err := NewImage(roots, "substrate")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.EnsureOrgImage(context.Background(), "org_a", image); err != nil {
		t.Fatal(err)
	}
	if got, want := ops.destroys[0], "pool/orgs/org_a/images/substrate-bbbbbbbb"; got != want {
		t.Fatalf("destroy = %q, want %q", got, want)
	}
	if got, want := ops.receives[0], "pool/images/substrate@ready -> pool/orgs/org_a/images/substrate-bbbbbbbb"; got != want {
		t.Fatalf("receive = %q, want %q", got, want)
	}
}

func TestSeedDigestUsesSourceContent(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "seed.ext4")
	fixedTime := time.Unix(1700000000, 0)
	writeSeedFile := func(content string) {
		t.Helper()
		if err := os.WriteFile(sourcePath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(sourcePath, fixedTime, fixedTime); err != nil {
			t.Fatal(err)
		}
	}
	spec := SeedSpec{
		Strategy:     SeedStrategyDDFromFile,
		SizeBytes:    4,
		VolBlockSize: "16K",
		SourcePath:   sourcePath,
	}
	writeSeedFile("aaaa")
	first, err := seedDigest(spec)
	if err != nil {
		t.Fatal(err)
	}
	writeSeedFile("bbbb")
	second, err := seedDigest(spec)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("seed digest did not change after same-size content rewrite: %s", first)
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
	if !containsString(ops.ensures, "pool/orgs/org_a/images") {
		t.Fatalf("org image parent was not ensured: %#v", ops.ensures)
	}
	if !containsString(ops.ensures, "pool/orgs/org_a/workloads/lease-a/mounts") {
		t.Fatalf("cloned mount parent was not ensured: %#v", ops.ensures)
	}
	if _, err := lifecycle.PrepareEmptyMount(context.Background(), lease, 1, "workspace", "op-b", 10<<30); err != nil {
		t.Fatal(err)
	}
	if !containsString(ops.ensures, "pool/orgs/org_a/workloads/lease-a/mounts") {
		t.Fatalf("empty mount parent was not ensured: %#v", ops.ensures)
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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func propertyKey(target, key string) string {
	return target + "\x00" + key
}
