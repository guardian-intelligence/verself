package vmorchestrator

import (
	"reflect"
	"testing"

	"github.com/verself/vm-orchestrator/vmproto"
	"github.com/verself/vm-orchestrator/zfs"
)

func TestNormalizeFilesystemMounts(t *testing.T) {
	mounts, err := normalizeFilesystemMounts([]FilesystemMount{{
		Name:      "data",
		SourceRef: "sticky-empty",
		MountPath: "/mnt/data/",
		ReadOnly:  false,
	}})
	if err != nil {
		t.Fatalf("normalizeFilesystemMounts returned error: %v", err)
	}
	want := []FilesystemMount{{
		Name:      "data",
		SourceRef: "sticky-empty",
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
		SourceRef: "sticky-empty",
		MountPath: "/proc/verself",
	}})
	if err == nil {
		t.Fatal("expected unsafe mount path to be rejected")
	}
}

func TestNormalizeFilesystemMountsRejectsHostPathRefs(t *testing.T) {
	cases := []FilesystemMount{
		{Name: "bad/name", SourceRef: "sticky-empty", MountPath: "/mnt/data"},
		{Name: "bad@snap", SourceRef: "sticky-empty", MountPath: "/mnt/data"},
		{Name: "data", SourceRef: "images/sticky-empty", MountPath: "/mnt/data"},
		{Name: "data", SourceRef: "sticky-empty@ready", MountPath: "/mnt/data"},
	}
	for _, tc := range cases {
		if _, err := normalizeFilesystemMounts([]FilesystemMount{tc}); err == nil {
			t.Fatalf("expected host-shaped ref to be rejected: %#v", tc)
		}
	}
}

func TestPreparedFilesystemMountsBecomeGuestManifest(t *testing.T) {
	manifest := guestFilesystemMounts([]preparedFilesystemMount{{
		Spec: FilesystemMount{
			Name:      "data",
			SourceRef: "sticky-empty",
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
	img, err := zfs.NewImage(roots, "sticky-empty")
	if err != nil {
		t.Fatalf("NewImage: %v", err)
	}
	if got, want := img.Snapshot().String(), "pool/images/sticky-empty@ready"; got != want {
		t.Fatalf("image snapshot = %q, want %q", got, want)
	}
}
