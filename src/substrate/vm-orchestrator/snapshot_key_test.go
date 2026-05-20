package vmorchestrator

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildSnapshotKeyUsesPreparedZFSOrigins(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	fc := writeSnapshotKeyInput(t, tmp, "firecracker", "fc")
	jailer := writeSnapshotKeyInput(t, tmp, "jailer", "jailer")
	kernel := writeSnapshotKeyInput(t, tmp, "vmlinux", "kernel")
	rootDataset := "pool/orgs/org_a/workloads/lease-a/root"
	mountDataset := "pool/orgs/org_a/workloads/lease-a/mounts/00-workspace"
	rootOrigin := "pool/orgs/org_a/images/substrate@ready"
	mountOrigin := "pool/orgs/org_a/goldens/workspace/generations/main@sealed"
	ops := &filesystemMountTestPrivOps{properties: map[string]map[string]string{
		rootDataset:  {"origin": rootOrigin},
		mountDataset: {"origin": mountOrigin},
		rootOrigin:   {"guid": "root-guid-a"},
		mountOrigin:  {"guid": "mount-guid-a"},
	}}
	cfg := DefaultConfig()
	cfg.Pool = "pool"
	cfg.FirecrackerBin = fc
	cfg.JailerBin = jailer
	cfg.KernelPath = kernel
	o := New(cfg, nil, WithPrivOps(ops))
	spec := LeaseSpec{Resources: VMResources{VCPUs: 4, MemoryMiB: 8192, RootDiskGiB: 80}}
	mounts := []preparedFilesystemMount{{
		Spec: FilesystemMount{
			Name:      "workspace",
			SourceRef: "pool/orgs/org_a/goldens/workspace/generations/main@sealed",
			MountPath: "/workspace",
			FSType:    "ext4",
		},
		DriveID: "fs0",
		Dataset: mountDataset,
	}}

	first, err := o.buildSnapshotKey(ctx, rootDataset, spec, mounts, "console=ttyS0")
	if err != nil {
		t.Fatalf("build first key: %v", err)
	}
	ops.properties[rootOrigin]["guid"] = "root-guid-b"
	second, err := o.buildSnapshotKey(ctx, rootDataset, spec, mounts, "console=ttyS0")
	if err != nil {
		t.Fatalf("build second key: %v", err)
	}
	if first.String() == second.String() {
		t.Fatalf("snapshot key did not change after prepared root origin guid changed: %s", first.String())
	}
}

func writeSnapshotKeyInput(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}
