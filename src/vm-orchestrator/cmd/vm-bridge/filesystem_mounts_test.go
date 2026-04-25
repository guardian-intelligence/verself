package main

import (
	"os"
	"strings"
	"testing"

	"github.com/verself/vm-orchestrator/vmproto"
)

func TestValidateFilesystemMountAcceptsVitePlusMount(t *testing.T) {
	err := validateFilesystemMount(vmproto.FilesystemMount{
		Name:       "viteplus",
		DriveID:    "fm0",
		DevicePath: "/dev/vdb",
		MountPath:  "/opt/verself/nodejs",
		FSType:     "ext4",
		ReadOnly:   true,
	})
	if err != nil {
		t.Fatalf("validateFilesystemMount returned error: %v", err)
	}
}

func TestValidateFilesystemMountRejectsPseudoFilesystemTargets(t *testing.T) {
	err := validateFilesystemMount(vmproto.FilesystemMount{
		Name:       "bad",
		DevicePath: "/dev/vdb",
		MountPath:  "/run/verself",
		FSType:     "ext4",
	})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected not allowed error, got %v", err)
	}
}

func TestRemoveEmptyLostFound(t *testing.T) {
	mountPath := t.TempDir()
	lostFound := mountPath + "/lost+found"
	if err := os.Mkdir(lostFound, 0o700); err != nil {
		t.Fatalf("mkdir lost+found: %v", err)
	}
	if err := removeEmptyLostFound(mountPath); err != nil {
		t.Fatalf("removeEmptyLostFound returned error: %v", err)
	}
	if _, err := os.Stat(lostFound); !os.IsNotExist(err) {
		t.Fatalf("lost+found still exists, err=%v", err)
	}
}

func TestRemoveEmptyLostFoundLeavesNonEmptyDirectory(t *testing.T) {
	mountPath := t.TempDir()
	lostFound := mountPath + "/lost+found"
	if err := os.Mkdir(lostFound, 0o700); err != nil {
		t.Fatalf("mkdir lost+found: %v", err)
	}
	if err := os.WriteFile(lostFound+"/orphan", []byte("data"), 0o600); err != nil {
		t.Fatalf("write orphan: %v", err)
	}
	if err := removeEmptyLostFound(mountPath); err != nil {
		t.Fatalf("removeEmptyLostFound returned error: %v", err)
	}
	if _, err := os.Stat(lostFound); err != nil {
		t.Fatalf("lost+found should remain: %v", err)
	}
}
