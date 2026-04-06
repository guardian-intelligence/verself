package fastsandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const zfsTimeout = 30 * time.Second

// zvolDevicePath returns the block device path for a ZFS zvol.
// e.g. "forgepool/ci/job-abc" -> "/dev/zvol/forgepool/ci/job-abc"
func zvolDevicePath(dataset string) string {
	return "/dev/zvol/" + dataset
}

// zfsClone creates a ZFS clone from a snapshot.
// Works for both datasets and zvols — the clone inherits the type.
func zfsClone(ctx context.Context, snapshot, target string) error {
	ctx, cancel := context.WithTimeout(ctx, zfsTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "zfs", "clone",
		"-o", "forge:job_id="+filepath.Base(target),
		"-o", "forge:created_at="+time.Now().UTC().Format(time.RFC3339),
		snapshot, target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zfs clone %s -> %s: %s: %w",
			snapshot, target, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// zfsDestroy removes a ZFS dataset/zvol. No -r flag: only destroys
// the exact dataset, not children. Caller must validate the path.
func zfsDestroy(ctx context.Context, dataset string) error {
	ctx, cancel := context.WithTimeout(ctx, zfsTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "zfs", "destroy", dataset)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zfs destroy %s: %s: %w",
			dataset, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// zfsSnapshotExists checks if a ZFS snapshot exists.
func zfsSnapshotExists(ctx context.Context, snapshot string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, zfsTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "zfs", "list", "-H", snapshot)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "does not exist") {
			return false, nil
		}
		return false, fmt.Errorf("zfs list %s: %w", snapshot, err)
	}
	return true, nil
}

// zfsWritten returns bytes written to a dataset since it was cloned.
func zfsWritten(ctx context.Context, dataset string) (uint64, error) {
	ctx, cancel := context.WithTimeout(ctx, zfsTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "zfs", "get", "-H", "-p", "-o", "value", "written", dataset)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("zfs get written %s: %w", dataset, err)
	}
	return strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
}

// mountZvol mounts a block device (zvol) to a temporary directory.
// Returns the mount path. Caller must unmount when done.
func mountZvol(ctx context.Context, devicePath string) (string, error) {
	// Wait briefly for the zvol device node to appear after clone.
	if err := waitForDevice(ctx, devicePath); err != nil {
		return "", fmt.Errorf("wait for device %s: %w", devicePath, err)
	}

	mountDir, err := os.MkdirTemp("", "fc-mount-*")
	if err != nil {
		return "", fmt.Errorf("create mount dir: %w", err)
	}

	cmd := exec.CommandContext(ctx, "mount", devicePath, mountDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		os.Remove(mountDir)
		return "", fmt.Errorf("mount %s: %s: %w", devicePath, strings.TrimSpace(string(out)), err)
	}
	return mountDir, nil
}

// unmount unmounts a filesystem and removes the mount directory.
func unmount(ctx context.Context, mountDir string) error {
	cmd := exec.CommandContext(ctx, "umount", mountDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("umount %s: %s: %w", mountDir, strings.TrimSpace(string(out)), err)
	}
	os.Remove(mountDir)
	return nil
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
