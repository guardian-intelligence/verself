package vmorchestrator

import (
	"context"
	"reflect"
	"testing"

	"github.com/verself/vm-orchestrator/vmproto"
	"github.com/verself/vm-orchestrator/zfs"
)

func TestNormalizeFilesystemMounts(t *testing.T) {
	mounts, err := normalizeFilesystemMounts([]FilesystemMount{{
		Name:      "data",
		SourceRef: "workspace-base",
		MountPath: "/mnt/data/",
		ReadOnly:  false,
	}}, filesystemMountSourceRequired)
	if err != nil {
		t.Fatalf("normalizeFilesystemMounts returned error: %v", err)
	}
	want := []FilesystemMount{{
		Name:      "data",
		SourceRef: "workspace-base",
		MountPath: "/mnt/data",
		FSType:    "ext4",
		ReadOnly:  false,
	}}
	if !reflect.DeepEqual(mounts, want) {
		t.Fatalf("mounts mismatch\n got: %#v\nwant: %#v", mounts, want)
	}
}

func TestNormalizeFilesystemMountsRejectsUnsafeMountPath(t *testing.T) {
	_, err := normalizeFilesystemMounts([]FilesystemMount{{
		Name:      "bad",
		SourceRef: "workspace-base",
		MountPath: "/proc/verself",
	}}, filesystemMountSourceRequired)
	if err == nil {
		t.Fatal("expected unsafe mount path to be rejected")
	}
}

func TestNormalizeFilesystemMountsRejectsHostPathRefs(t *testing.T) {
	cases := []FilesystemMount{
		{Name: "bad/name", SourceRef: "workspace-base", MountPath: "/mnt/data"},
		{Name: "bad@snap", SourceRef: "workspace-base", MountPath: "/mnt/data"},
		{Name: "data", SourceRef: "images/workspace-base", MountPath: "/mnt/data"},
		{Name: "data", SourceRef: "workspace-base@ready", MountPath: "/mnt/data"},
	}
	for _, tc := range cases {
		if _, err := normalizeFilesystemMounts([]FilesystemMount{tc}, filesystemMountSourceRequired); err == nil {
			t.Fatalf("expected host-shaped ref to be rejected: %#v", tc)
		}
	}
}

func TestNormalizeFilesystemMountsAllowsEmptyGoldenSource(t *testing.T) {
	mounts, err := normalizeFilesystemMounts([]FilesystemMount{{
		Name:      "workspace-empty",
		MountPath: "/mnt/data",
	}}, filesystemMountSourceOptional)
	if err != nil {
		t.Fatalf("normalizeFilesystemMounts returned error: %v", err)
	}
	if got := mounts[0].SourceRef; got != "" {
		t.Fatalf("source ref = %q, want empty", got)
	}
}

func TestNormalizeFilesystemMountsAllowsGoldenGenerationSource(t *testing.T) {
	sourceRef := "pool/orgs/org_a/goldens/21a47db4-8a9d-4fb4-9072-1c727eaa3d65/generations/01KR70P9CK4F3XT6A9C732HMZC@sealed"
	mounts, err := normalizeFilesystemMounts([]FilesystemMount{{
		Name:      "workspace-generation",
		SourceRef: sourceRef,
		MountPath: "/mnt/data",
	}}, filesystemMountSourceOptional)
	if err != nil {
		t.Fatalf("normalizeFilesystemMounts returned error: %v", err)
	}
	if got := mounts[0].SourceRef; got != sourceRef {
		t.Fatalf("source ref = %q, want %q", got, sourceRef)
	}
}

func TestPreparedFilesystemMountsBecomeGuestManifest(t *testing.T) {
	manifest := guestFilesystemMounts([]preparedFilesystemMount{{
		Spec: FilesystemMount{
			Name:      "data",
			SourceRef: "workspace-base",
			MountPath: "/mnt/data",
			FSType:    "ext4",
			ReadOnly:  false,
		},
		DriveID:         "fm0",
		GuestDevicePath: "/dev/vdb",
	}})
	want := []vmproto.FilesystemMount{{
		Name:       "data",
		DriveID:    "fm0",
		DevicePath: "/dev/vdb",
		MountPath:  "/mnt/data",
		FSType:     "ext4",
		ReadOnly:   false,
	}}
	if !reflect.DeepEqual(manifest, want) {
		t.Fatalf("guest manifest mismatch\n got: %#v\nwant: %#v", manifest, want)
	}
}

func TestImageSnapshotUsesConfiguredImageDataset(t *testing.T) {
	roots := zfs.Roots{Pool: "pool", ImageDataset: "images", WorkloadDataset: "workloads"}
	img, err := zfs.NewImage(roots, "workspace-base")
	if err != nil {
		t.Fatalf("NewImage: %v", err)
	}
	if got, want := img.Snapshot().String(), "pool/images/workspace-base@ready"; got != want {
		t.Fatalf("image snapshot = %q, want %q", got, want)
	}
}

func TestPrepareFilesystemMountsFallsBackToEmptyMountWhenGenerationSnapshotIsMissing(t *testing.T) {
	ops := &filesystemMountTestPrivOps{cloneErr: zfs.ErrSnapshotNotFound}
	var journal []durableJournalEntry
	o := New(Config{
		Pool:            "pool",
		ImageDataset:    "images",
		GoldenDataset:   "goldens",
		WorkloadDataset: "workloads",
	}, nil, WithPrivOps(ops), withDurableJournal(func(entry durableJournalEntry) {
		journal = append(journal, entry)
	}))
	lease, err := zfs.NewLease(o.roots, "org_a", "lease-a")
	if err != nil {
		t.Fatal(err)
	}
	const sourceRef = "pool/orgs/org_a/goldens/scope-a/generations/gen-a@sealed"
	mounts, err := o.prepareFilesystemMounts(context.Background(), lease, []FilesystemMount{{
		Name:        "workspace",
		OperationID: "op-a",
		SourceRef:   sourceRef,
		MountPath:   "/workspace",
		FSType:      "ext4",
		Required:    true,
	}})
	if err != nil {
		t.Fatalf("prepareFilesystemMounts returned error: %v", err)
	}
	if len(mounts) != 1 {
		t.Fatalf("prepared mount count = %d, want 1", len(mounts))
	}
	if got, want := mounts[0].Dataset, "pool/orgs/org_a/workloads/lease-a/mounts/00-workspace"; got != want {
		t.Fatalf("prepared dataset = %q, want %q", got, want)
	}
	if len(ops.clones) != 1 {
		t.Fatalf("clone count = %d, want 1", len(ops.clones))
	}
	if got, want := ops.creates[0], "pool/orgs/org_a/workloads/lease-a/mounts/00-workspace"; got != want {
		t.Fatalf("created sparse volume = %q, want %q", got, want)
	}
	if got, want := ops.mkfs[0], "/dev/zvol/pool/orgs/org_a/workloads/lease-a/mounts/00-workspace"; got != want {
		t.Fatalf("mkfs device = %q, want %q", got, want)
	}
	if !journalContainsPhase(journal, "source_snapshot_missing") {
		t.Fatalf("journal did not include source_snapshot_missing: %#v", journal)
	}
}

type filesystemMountTestPrivOps struct {
	clones   []string
	creates  []string
	ensures  []string
	sets     []string
	mkfs     []string
	cloneErr error
}

func (o *filesystemMountTestPrivOps) ZFSClone(_ context.Context, snapshot, target, _ string) error {
	o.clones = append(o.clones, snapshot+" -> "+target)
	return o.cloneErr
}

func (*filesystemMountTestPrivOps) ZFSSnapshot(context.Context, string, string, map[string]string) error {
	return nil
}

func (*filesystemMountTestPrivOps) ZFSDestroy(context.Context, string) error { return nil }

func (*filesystemMountTestPrivOps) ZFSDestroyRecursive(context.Context, string) error { return nil }

func (*filesystemMountTestPrivOps) ZFSCreateEncryptedFilesystem(context.Context, string, []byte) error {
	return nil
}

func (o *filesystemMountTestPrivOps) ZFSEnsureFilesystem(_ context.Context, dataset string) error {
	o.ensures = append(o.ensures, dataset)
	return nil
}

func (*filesystemMountTestPrivOps) ZFSLoadKey(context.Context, string, []byte) error { return nil }

func (*filesystemMountTestPrivOps) ZFSUnloadKey(context.Context, string) error { return nil }

func (*filesystemMountTestPrivOps) ZFSSnapshotExists(context.Context, string) (bool, error) {
	return false, nil
}

func (*filesystemMountTestPrivOps) ZFSDatasetExists(context.Context, string) (bool, error) {
	return false, nil
}

func (o *filesystemMountTestPrivOps) ZFSSetProperty(_ context.Context, dataset, key, value string) error {
	o.sets = append(o.sets, dataset+" "+key+"="+value)
	return nil
}

func (*filesystemMountTestPrivOps) ZFSGetProperty(_ context.Context, target, key string) (string, error) {
	switch key {
	case "encryption":
		return "aes-256-gcm", nil
	case "encryptionroot":
		const prefix = "pool/orgs/"
		return prefix + "org_a", nil
	case "keystatus":
		return "available", nil
	default:
		return "", nil
	}
}

func (*filesystemMountTestPrivOps) ZFSCreateVolume(context.Context, string, uint64, string) error {
	return nil
}

func (o *filesystemMountTestPrivOps) ZFSCreateSparseVolume(_ context.Context, dataset string, _ uint64, _ string) error {
	o.creates = append(o.creates, dataset)
	return nil
}

func (*filesystemMountTestPrivOps) ZFSReceiveSnapshot(context.Context, string, string) error {
	return nil
}

func (*filesystemMountTestPrivOps) ZFSWriteVolumeFromFile(context.Context, string, string) (uint64, error) {
	return 0, nil
}

func (o *filesystemMountTestPrivOps) ZFSMkfs(_ context.Context, devicePath, _, _ string) error {
	o.mkfs = append(o.mkfs, devicePath)
	return nil
}

func (*filesystemMountTestPrivOps) ZFSEnsureVolumeSizeExt4(context.Context, string, uint64) error {
	return nil
}

func (*filesystemMountTestPrivOps) ZFSRename(context.Context, string, string) error { return nil }

func (*filesystemMountTestPrivOps) ZFSPromote(context.Context, string) error { return nil }

func (*filesystemMountTestPrivOps) ZFSListChildren(context.Context, string) ([]string, error) {
	return nil, nil
}

func (*filesystemMountTestPrivOps) ZFSUsed(context.Context, string) (uint64, error) { return 0, nil }

func (*filesystemMountTestPrivOps) ZFSWritten(context.Context, string) (uint64, error) {
	return 0, nil
}

func (*filesystemMountTestPrivOps) UnmountStaleZvolMounts(context.Context, string) (int, error) {
	return 0, nil
}

func (*filesystemMountTestPrivOps) FlushBlockDevice(context.Context, string) error { return nil }

func (*filesystemMountTestPrivOps) TapCreate(context.Context, string, string, int, int) error {
	return nil
}

func (*filesystemMountTestPrivOps) TapUp(context.Context, string) error { return nil }

func (*filesystemMountTestPrivOps) TapDelete(context.Context, string) error { return nil }

func (*filesystemMountTestPrivOps) SetupJail(context.Context, string, string, int, int, []JailBlockDevice) error {
	return nil
}

func (*filesystemMountTestPrivOps) PlaceJailBlockDevice(context.Context, string, JailBlockDevice, int, int) error {
	return nil
}

func (*filesystemMountTestPrivOps) StartJailer(context.Context, string, JailerConfig) (*JailerProcess, error) {
	return nil, nil
}

func (*filesystemMountTestPrivOps) Chmod(context.Context, string, uint32) error { return nil }

func journalContainsPhase(entries []durableJournalEntry, phase string) bool {
	for _, entry := range entries {
		if entry.Phase == phase {
			return true
		}
	}
	return false
}
