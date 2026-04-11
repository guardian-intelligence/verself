package vmorchestrator

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DirectPrivOps executes privileged operations by shelling out directly.
// Requires the calling process to run as root.
type DirectPrivOps struct{}

func (DirectPrivOps) ZFSClone(ctx context.Context, snapshot, target, jobID string) error {
	ctx, cancel := context.WithTimeout(ctx, zfsTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "zfs", "clone",
		"-o", "forge:job_id="+jobID,
		"-o", "forge:created_at="+time.Now().UTC().Format(time.RFC3339),
		snapshot, target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zfs clone %s -> %s: %s: %w",
			snapshot, target, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (DirectPrivOps) ZFSSnapshot(ctx context.Context, dataset, snapshotName string, properties map[string]string) error {
	ctx, cancel := context.WithTimeout(ctx, zfsTimeout)
	defer cancel()

	if err := validateZFSSnapshotName(snapshotName); err != nil {
		return err
	}
	if strings.Contains(dataset, "@") {
		return fmt.Errorf("zfs dataset must not include snapshot suffix: %s", dataset)
	}

	args := []string{"snapshot"}
	for key, value := range properties {
		args = append(args, "-o", key+"="+value)
	}
	target := dataset + "@" + snapshotName
	args = append(args, target)

	cmd := exec.CommandContext(ctx, "zfs", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zfs snapshot %s: %s: %w",
			target, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (DirectPrivOps) ZFSDestroy(ctx context.Context, dataset string) error {
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

func (DirectPrivOps) TapCreate(ctx context.Context, tapName, hostCIDR string) error {
	if err := runCmd(ctx, "ip", "tuntap", "add", tapName, "mode", "tap"); err != nil {
		return fmt.Errorf("create tap %s: %w", tapName, err)
	}
	if err := runCmd(ctx, "ip", "addr", "add", hostCIDR, "dev", tapName); err != nil {
		return fmt.Errorf("assign ip %s to %s: %w", hostCIDR, tapName, err)
	}
	return nil
}

func (DirectPrivOps) TapUp(ctx context.Context, tapName string) error {
	return runCmd(ctx, "ip", "link", "set", tapName, "up")
}

func (DirectPrivOps) TapDelete(ctx context.Context, tapName string) error {
	return runCmd(ctx, "ip", "link", "del", tapName)
}

func (DirectPrivOps) SetupJail(ctx context.Context, jailRoot, zvolDev, kernelSrc string, uid, gid int) error {
	for _, dir := range []string{jailRoot, filepath.Join(jailRoot, "run")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}

	// Place kernel in jail. Try hardlink first (instant, same filesystem),
	// fall back to copy (cross-filesystem). The kernel is read-only so
	// hardlinking is safe — all jails share the same inode.
	kernelDst := filepath.Join(jailRoot, "vmlinux")
	if err := os.Link(kernelSrc, kernelDst); err != nil {
		if linkErr := copyFile(kernelSrc, kernelDst); linkErr != nil {
			return fmt.Errorf("place kernel in jail: %w", linkErr)
		}
	}
	if err := os.Chown(kernelDst, uid, gid); err != nil {
		return fmt.Errorf("chown kernel: %w", err)
	}

	// Create zvol block device node inside jail.
	major, minor, err := deviceMajorMinor(ctx, zvolDev)
	if err != nil {
		return fmt.Errorf("device major/minor: %w", err)
	}

	rootfsDev := filepath.Join(jailRoot, "rootfs")
	mknodCmd := exec.CommandContext(ctx, "mknod", rootfsDev, "b",
		strconv.FormatUint(uint64(major), 10),
		strconv.FormatUint(uint64(minor), 10))
	if out, mknodErr := mknodCmd.CombinedOutput(); mknodErr != nil {
		return fmt.Errorf("mknod %s: %s: %w", rootfsDev, strings.TrimSpace(string(out)), mknodErr)
	}
	if err := os.Chown(rootfsDev, uid, gid); err != nil {
		return fmt.Errorf("chown rootfs device: %w", err)
	}

	// Create metrics file (regular file, not FIFO).
	metricsFile := filepath.Join(jailRoot, "metrics.json")
	if err := os.WriteFile(metricsFile, nil, 0o644); err != nil {
		return fmt.Errorf("create metrics file: %w", err)
	}
	if err := os.Chown(metricsFile, uid, gid); err != nil {
		return fmt.Errorf("chown metrics file: %w", err)
	}

	return nil
}

func (DirectPrivOps) StartJailer(_ context.Context, jobID string, cfg JailerConfig) (*JailerProcess, error) {
	args := []string{
		"--id", jobID,
		"--exec-file", cfg.FirecrackerBin,
		"--uid", strconv.Itoa(cfg.UID),
		"--gid", strconv.Itoa(cfg.GID),
		"--chroot-base-dir", cfg.ChrootBaseDir,
		"--", // Separator: args after this go to Firecracker.
		"--api-sock", "/run/firecracker.sock",
	}

	cmd := exec.Command(cfg.JailerBin, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("jailer stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("jailer stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start jailer: %w", err)
	}

	return &JailerProcess{
		Pid:    cmd.Process.Pid,
		Stdout: stdout,
		Stderr: stderr,
		waitFn: cmd.Wait,
		killFn: cmd.Process.Kill,
	}, nil
}

func (DirectPrivOps) Chmod(_ context.Context, path string, mode uint32) error {
	if err := os.Chmod(path, os.FileMode(mode)); err != nil {
		return fmt.Errorf("chmod %s to %o: %w", path, mode, err)
	}
	return nil
}

// copyFile is a helper for SetupJail kernel placement.
func copyFile(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer s.Close()

	d, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer d.Close()

	if _, err := io.Copy(d, s); err != nil {
		return fmt.Errorf("copy %s -> %s: %w", src, dst, err)
	}
	return nil
}
