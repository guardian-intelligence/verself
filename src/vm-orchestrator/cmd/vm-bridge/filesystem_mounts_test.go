package main

import (
	"strings"
	"testing"

	"github.com/forge-metal/vm-orchestrator/vmproto"
)

func TestValidateFilesystemMountAcceptsVitePlusMount(t *testing.T) {
	err := validateFilesystemMount(vmproto.FilesystemMount{
		Name:       "viteplus",
		DriveID:    "fm0",
		DevicePath: "/dev/vdb",
		MountPath:  "/opt/forge-metal/nodejs",
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
		MountPath:  "/run/forge-metal",
		FSType:     "ext4",
	})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected not allowed error, got %v", err)
	}
}
