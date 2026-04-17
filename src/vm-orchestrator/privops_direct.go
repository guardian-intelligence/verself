package vmorchestrator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/vishvananda/netlink"
)

type DirectPrivOps struct{}

func (DirectPrivOps) ZFSClone(ctx context.Context, snapshot, target, leaseID string) error {
	ctx, cancel := context.WithTimeout(ctx, zfsTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "zfs", "clone",
		"-o", "forge:lease_id="+leaseID,
		"-o", "forge:created_at="+time.Now().UTC().Format(time.RFC3339),
		snapshot, target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zfs clone %s -> %s: %s: %w", snapshot, target, strings.TrimSpace(string(out)), err)
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
		return fmt.Errorf("zfs snapshot %s: %s: %w", target, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (DirectPrivOps) ZFSDestroy(ctx context.Context, dataset string) error {
	ctx, cancel := context.WithTimeout(ctx, zfsTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "zfs", "destroy", dataset)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zfs destroy %s: %s: %w", dataset, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (DirectPrivOps) ZFSDestroyRecursive(ctx context.Context, dataset string) error {
	ctx, cancel := context.WithTimeout(ctx, zfsTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "zfs", "destroy", "-R", dataset)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zfs destroy -R %s: %s: %w", dataset, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (DirectPrivOps) ZFSEnsureFilesystem(ctx context.Context, dataset string) error {
	ctx, cancel := context.WithTimeout(ctx, zfsTimeout)
	defer cancel()
	if strings.TrimSpace(dataset) == "" || strings.Contains(dataset, "@") {
		return fmt.Errorf("zfs filesystem dataset is invalid: %s", dataset)
	}
	exists, err := zfsDatasetExists(ctx, dataset)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	cmd := exec.CommandContext(ctx, "zfs", "create", "-p", dataset)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zfs create -p %s: %s: %w", dataset, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (DirectPrivOps) ZFSSendReceive(ctx context.Context, snapshot, target string) error {
	ctx, cancel := context.WithTimeout(ctx, zfsTimeout)
	defer cancel()
	if strings.TrimSpace(snapshot) == "" || !strings.Contains(snapshot, "@") {
		return fmt.Errorf("zfs send snapshot is invalid: %s", snapshot)
	}
	if strings.TrimSpace(target) == "" || strings.Contains(target, "@") {
		return fmt.Errorf("zfs receive target is invalid: %s", target)
	}
	send := exec.CommandContext(ctx, "zfs", "send", snapshot)
	recv := exec.CommandContext(ctx, "zfs", "receive", "-u", "-F", target)
	pipe, err := send.StdoutPipe()
	if err != nil {
		return fmt.Errorf("zfs send stdout pipe: %w", err)
	}
	recv.Stdin = pipe
	send.Stderr = new(strings.Builder)
	recv.Stderr = new(strings.Builder)
	if err := recv.Start(); err != nil {
		return fmt.Errorf("start zfs receive %s: %w", target, err)
	}
	if err := send.Start(); err != nil {
		_ = recv.Process.Kill()
		return fmt.Errorf("start zfs send %s: %w", snapshot, err)
	}
	sendErr := send.Wait()
	// exec.Cmd owns the StdoutPipe lifecycle and closes it during Wait; closing
	// it here races the command's cleanup path and reports a false failure.
	recvErr := recv.Wait()
	if sendErr != nil || recvErr != nil {
		sendStderr := ""
		recvStderr := ""
		if b, ok := send.Stderr.(*strings.Builder); ok {
			sendStderr = b.String()
		}
		if b, ok := recv.Stderr.(*strings.Builder); ok {
			recvStderr = b.String()
		}
		return fmt.Errorf("zfs send %s | receive %s failed: send=%v %s receive=%v %s", snapshot, target, sendErr, strings.TrimSpace(sendStderr), recvErr, strings.TrimSpace(recvStderr))
	}
	return nil
}

func (DirectPrivOps) ZFSSetProperty(ctx context.Context, dataset, key, value string) error {
	ctx, cancel := context.WithTimeout(ctx, zfsTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "zfs", "set", key+"="+value, dataset)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zfs set %s=%s on %s: %s: %w", key, value, dataset, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (DirectPrivOps) FlushBlockDevice(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, zfsTimeout)
	defer cancel()
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/dev/") {
		return fmt.Errorf("block device path is invalid: %s", path)
	}
	cmd := exec.CommandContext(ctx, "blockdev", "--flushbufs", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("blockdev --flushbufs %s: %s: %w", path, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (DirectPrivOps) TapCreate(ctx context.Context, tapName, hostCIDR string, ownerUID, ownerGID int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tap := &netlink.Tuntap{
		LinkAttrs: netlink.LinkAttrs{Name: tapName},
		Mode:      netlink.TUNTAP_MODE_TAP,
		Owner:     uint32(max(ownerUID, 0)),
		Group:     uint32(max(ownerGID, 0)),
	}
	if err := netlink.LinkAdd(tap); err != nil {
		return fmt.Errorf("create tap %s: %w", tapName, err)
	}
	link, err := netlink.LinkByName(tapName)
	if err != nil {
		return fmt.Errorf("lookup tap %s after create: %w", tapName, err)
	}
	addr, err := netlink.ParseAddr(hostCIDR)
	if err != nil {
		_ = netlink.LinkDel(link)
		return fmt.Errorf("parse host cidr %s: %w", hostCIDR, err)
	}
	if err := netlink.AddrAdd(link, addr); err != nil {
		_ = netlink.LinkDel(link)
		return fmt.Errorf("assign ip %s to %s: %w", hostCIDR, tapName, err)
	}
	return nil
}

func (DirectPrivOps) TapUp(ctx context.Context, tapName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	link, err := netlink.LinkByName(tapName)
	if err != nil {
		return fmt.Errorf("lookup tap %s: %w", tapName, err)
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("set tap %s up: %w", tapName, err)
	}
	return nil
}

func (DirectPrivOps) TapDelete(ctx context.Context, tapName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	link, err := netlink.LinkByName(tapName)
	if err != nil {
		if errors.Is(err, netlink.LinkNotFoundError{}) {
			return nil
		}
		return fmt.Errorf("lookup tap %s: %w", tapName, err)
	}
	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("delete tap %s: %w", tapName, err)
	}
	return nil
}

func (DirectPrivOps) SetupJail(ctx context.Context, jailRoot, kernelSrc string, uid, gid int, devices []JailBlockDevice) error {
	for _, dir := range []string{jailRoot, filepath.Join(jailRoot, "run"), filepath.Join(jailRoot, "drives")} {
		if err := ensureJailDirectory(dir); err != nil {
			return err
		}
	}
	kernelDst := filepath.Join(jailRoot, "vmlinux")
	if err := os.Link(kernelSrc, kernelDst); err != nil {
		if linkErr := copyFile(kernelSrc, kernelDst); linkErr != nil {
			return fmt.Errorf("place kernel in jail: %w", linkErr)
		}
	}
	if err := os.Chown(kernelDst, uid, gid); err != nil {
		return fmt.Errorf("chown kernel: %w", err)
	}

	if len(devices) == 0 {
		return fmt.Errorf("at least one jail block device is required")
	}
	for _, device := range devices {
		if err := createJailBlockDevice(ctx, jailRoot, device, uid, gid); err != nil {
			return err
		}
	}

	metricsFile := filepath.Join(jailRoot, "metrics.json")
	if err := os.WriteFile(metricsFile, nil, 0o644); err != nil {
		return fmt.Errorf("create metrics file: %w", err)
	}
	if err := os.Chown(metricsFile, uid, gid); err != nil {
		return fmt.Errorf("chown metrics file: %w", err)
	}
	return nil
}

func createJailBlockDevice(ctx context.Context, jailRoot string, device JailBlockDevice, uid, gid int) error {
	if strings.TrimSpace(device.HostPath) == "" || strings.TrimSpace(device.JailPath) == "" {
		return fmt.Errorf("jail block device host and jail paths are required")
	}
	major, minor, err := deviceMajorMinor(ctx, device.HostPath)
	if err != nil {
		return fmt.Errorf("device major/minor %s: %w", device.HostPath, err)
	}
	rel := strings.TrimPrefix(filepath.Clean(device.JailPath), string(os.PathSeparator))
	if rel == "." || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("invalid jail device path %q", device.JailPath)
	}
	devicePath := filepath.Join(jailRoot, rel)
	if err := ensureJailDirectory(filepath.Dir(devicePath)); err != nil {
		return err
	}
	_ = os.Remove(devicePath)
	mknodCmd := exec.CommandContext(ctx, "mknod", devicePath, "b", strconv.FormatUint(uint64(major), 10), strconv.FormatUint(uint64(minor), 10))
	if out, mknodErr := mknodCmd.CombinedOutput(); mknodErr != nil {
		return fmt.Errorf("mknod %s: %s: %w", devicePath, strings.TrimSpace(string(out)), mknodErr)
	}
	if err := os.Chown(devicePath, uid, gid); err != nil {
		return fmt.Errorf("chown jail device %s: %w", devicePath, err)
	}
	return nil
}

func ensureJailDirectory(path string) error {
	const mode os.FileMode = 0o755
	if err := os.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("mkdir jail directory %s: %w", path, err)
	}
	// systemd UMask applies to MkdirAll, so pin chroot directory visibility explicitly.
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod jail directory %s to %o: %w", path, mode, err)
	}
	return nil
}

func (DirectPrivOps) StartJailer(_ context.Context, leaseID string, cfg JailerConfig) (*JailerProcess, error) {
	args := []string{
		"--id", leaseID,
		"--exec-file", cfg.FirecrackerBin,
		"--uid", strconv.Itoa(cfg.UID),
		"--gid", strconv.Itoa(cfg.GID),
		"--chroot-base-dir", cfg.ChrootBaseDir,
		"--",
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
