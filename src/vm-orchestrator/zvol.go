package vmorchestrator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// zvolDevicePath returns the block device path for a ZFS zvol.
// e.g. "vspool/workloads/lease-abc" -> "/dev/zvol/vspool/workloads/lease-abc"
func zvolDevicePath(dataset string) string {
	return "/dev/zvol/" + dataset
}

// waitForDevice polls for a device node to appear (zvols take a moment
// after clone for udev to create the node). Timeout via context.
func waitForDevice(ctx context.Context, path string) error {
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("device %s did not appear: %w", path, ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// deviceMajorMinor returns the major/minor numbers of a block device.
// Uses -L to follow symlinks (/dev/zvol/... are symlinks to /dev/zdN).
func deviceMajorMinor(ctx context.Context, devicePath string) (uint32, uint32, error) {
	cmd := exec.CommandContext(ctx, "stat", "-L", "-c", "%t %T", devicePath)
	out, err := cmd.Output()
	if err != nil {
		return 0, 0, fmt.Errorf("stat %s: %w", devicePath, err)
	}
	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected stat output: %q", string(out))
	}
	major, err := strconv.ParseUint(parts[0], 16, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("parse major %q: %w", parts[0], err)
	}
	minor, err := strconv.ParseUint(parts[1], 16, 32)
	if err != nil {
		return 0, 0, fmt.Errorf("parse minor %q: %w", parts[1], err)
	}
	return uint32(major), uint32(minor), nil
}
