package vmorchestrator

import (
	"reflect"
	"testing"

	"github.com/verself/vm-orchestrator/vmproto"
	"github.com/verself/vm-orchestrator/zfs"
)

func TestNormalizeFilesystemMounts(t *testing.T) {
	mounts, err := normalizeFilesystemMounts([]FilesystemMount{{
		Name:      "viteplus",
		SourceRef: "viteplus",
		MountPath: "/opt/verself/nodejs/",
		ReadOnly:  true,
	}})
	if err != nil {
		t.Fatalf("normalizeFilesystemMounts returned error: %v", err)
	}
	want := []FilesystemMount{{
		Name:      "viteplus",
		SourceRef: "viteplus",
		MountPath: "/opt/verself/nodejs",
		FSType:    "ext4",
		ReadOnly:  true,
	}}
	if !reflect.DeepEqual(mounts, want) {
		t.Fatalf("mounts mismatch\n got: %#v\nwant: %#v", mounts, want)
	}
}

func TestNormalizeFilesystemMountsRejectsUnsafeMountPath(t *testing.T) {
	_, err := normalizeFilesystemMounts([]FilesystemMount{{
		Name:      "bad",
		SourceRef: "viteplus",
		MountPath: "/proc/verself",
	}})
	if err == nil {
		t.Fatal("expected unsafe mount path to be rejected")
	}
}

func TestNormalizeFilesystemMountsRejectsHostPathRefs(t *testing.T) {
	cases := []FilesystemMount{
		{Name: "bad/name", SourceRef: "viteplus", MountPath: "/opt/verself/nodejs"},
		{Name: "bad@snap", SourceRef: "viteplus", MountPath: "/opt/verself/nodejs"},
		{Name: "viteplus", SourceRef: "images/viteplus", MountPath: "/opt/verself/nodejs"},
		{Name: "viteplus", SourceRef: "viteplus@ready", MountPath: "/opt/verself/nodejs"},
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
			Name:      "viteplus",
			SourceRef: "viteplus",
			MountPath: "/opt/verself/nodejs",
			FSType:    "ext4",
			ReadOnly:  true,
		},
		DriveID:         "fm0",
		GuestDevicePath: "/dev/vdb",
	}})
	want := []vmproto.FilesystemMount{{
		Name:       "viteplus",
		DriveID:    "fm0",
		DevicePath: "/dev/vdb",
		MountPath:  "/opt/verself/nodejs",
		FSType:     "ext4",
		ReadOnly:   true,
	}}
	if !reflect.DeepEqual(manifest, want) {
		t.Fatalf("guest manifest mismatch\n got: %#v\nwant: %#v", manifest, want)
	}
}

func TestImageSnapshotUsesConfiguredImageDataset(t *testing.T) {
	roots := zfs.Roots{Pool: "pool", ImageDataset: "images", WorkloadDataset: "workloads"}
	img, err := zfs.NewImage(roots, "viteplus")
	if err != nil {
		t.Fatalf("NewImage: %v", err)
	}
	if got, want := img.Snapshot().String(), "pool/images/viteplus@ready"; got != want {
		t.Fatalf("image snapshot = %q, want %q", got, want)
	}
}
