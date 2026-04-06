package ci

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const zfsTimeout = 30 * time.Second

func ensureDataset(ctx context.Context, dataset string) error {
	return runZFS(ctx, "create", "-p", dataset)
}

func destroyDatasetRecursive(ctx context.Context, dataset string) error {
	exists, err := datasetExists(ctx, dataset)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	return runZFS(ctx, "destroy", "-R", "-f", dataset)
}

func zfsClone(ctx context.Context, snapshot, target string) error {
	return runZFS(ctx, "clone", snapshot, target)
}

func zfsSnapshot(ctx context.Context, snapshot string) error {
	return runZFS(ctx, "snapshot", snapshot)
}

func zfsDestroy(ctx context.Context, target string, recursive bool) error {
	args := []string{"destroy"}
	if recursive {
		args = append(args, "-R", "-f")
	}
	args = append(args, target)
	return runZFS(ctx, args...)
}

func snapshotExists(ctx context.Context, snapshot string) (bool, error) {
	return zfsExists(ctx, snapshot)
}

func datasetExists(ctx context.Context, dataset string) (bool, error) {
	return zfsExists(ctx, dataset)
}

func zfsExists(ctx context.Context, target string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, zfsTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "zfs", "list", "-H", target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "does not exist") {
			return false, nil
		}
		return false, fmt.Errorf("zfs list %s: %s: %w", target, strings.TrimSpace(string(out)), err)
	}
	return true, nil
}

func mountDataset(ctx context.Context, dataset string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, zfsTimeout)
	defer cancel()

	devPath := "/dev/zvol/" + dataset
	for start := time.Now(); ; {
		if _, err := os.Stat(devPath); err == nil {
			break
		}
		if time.Since(start) > zfsTimeout {
			return "", fmt.Errorf("device %s did not appear", devPath)
		}
		time.Sleep(50 * time.Millisecond)
	}

	mountDir, err := os.MkdirTemp("", "forge-metal-zvol-*")
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "mount", devPath, mountDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(mountDir)
		return "", fmt.Errorf("mount %s: %s: %w", devPath, strings.TrimSpace(string(out)), err)
	}
	return mountDir, nil
}

func unmountDataset(ctx context.Context, mountDir string) error {
	ctx, cancel := context.WithTimeout(ctx, zfsTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "umount", mountDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback: lazy unmount detaches immediately even if busy.
		// Without this, a busy mount leaks permanently because nothing
		// in the system retries or reaps it.
		lazyCmd := exec.CommandContext(ctx, "umount", "-l", mountDir)
		if lazyOut, lazyErr := lazyCmd.CombinedOutput(); lazyErr != nil {
			return fmt.Errorf("umount %s: %s (lazy fallback: %s: %w)", mountDir,
				strings.TrimSpace(string(out)), strings.TrimSpace(string(lazyOut)), lazyErr)
		}
	}
	return os.RemoveAll(mountDir)
}

func checkFilesystem(ctx context.Context, dataset string) error {
	ctx, cancel := context.WithTimeout(ctx, zfsTimeout)
	defer cancel()
	devPath := "/dev/zvol/" + dataset
	cmd := exec.CommandContext(ctx, "fsck.ext4", "-n", devPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("fsck.ext4 %s: %s: %w", devPath, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func runZFS(ctx context.Context, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, zfsTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "zfs", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zfs %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
	return nil
}
