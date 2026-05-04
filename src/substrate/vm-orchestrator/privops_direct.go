//go:build linux

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

	"github.com/verself/vm-orchestrator/zfs"
)

type DirectPrivOps struct{}

func (DirectPrivOps) ZFSClone(ctx context.Context, snapshot, target, leaseID string) error {
	ctx, cancel := context.WithTimeout(ctx, zfs.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "zfs", "clone",
		"-o", "vs:lease_id="+leaseID,
		"-o", "vs:created_at="+time.Now().UTC().Format(time.RFC3339),
		snapshot, target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zfs clone %s -> %s: %s: %w", snapshot, target, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (DirectPrivOps) ZFSSnapshot(ctx context.Context, dataset, snapshotName string, properties map[string]string) error {
	ctx, cancel := context.WithTimeout(ctx, zfs.Timeout)
	defer cancel()
	if err := zfs.ValidateSnapshotName(snapshotName); err != nil {
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
	ctx, cancel := context.WithTimeout(ctx, zfs.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "zfs", "destroy", dataset)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zfs destroy %s: %s: %w", dataset, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (DirectPrivOps) ZFSDestroyRecursive(ctx context.Context, dataset string) error {
	ctx, cancel := context.WithTimeout(ctx, zfs.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "zfs", "destroy", "-R", dataset)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zfs destroy -R %s: %s: %w", dataset, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (DirectPrivOps) ZFSEnsureFilesystem(ctx context.Context, dataset string) error {
	ctx, cancel := context.WithTimeout(ctx, zfs.Timeout)
	defer cancel()
	if strings.TrimSpace(dataset) == "" || strings.Contains(dataset, "@") {
		return fmt.Errorf("zfs filesystem dataset is invalid: %s", dataset)
	}
	exists, err := zfs.DatasetExists(ctx, dataset)
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
	ctx, cancel := context.WithTimeout(ctx, zfs.Timeout)
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

func (DirectPrivOps) ZFSSnapshotExists(ctx context.Context, snapshot string) (bool, error) {
	return zfs.SnapshotExists(ctx, snapshot)
}

func (DirectPrivOps) ZFSDatasetExists(ctx context.Context, dataset string) (bool, error) {
	return zfs.DatasetExists(ctx, dataset)
}

func (DirectPrivOps) ZFSSetProperty(ctx context.Context, dataset, key, value string) error {
	ctx, cancel := context.WithTimeout(ctx, zfs.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "zfs", "set", key+"="+value, dataset)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zfs set %s=%s on %s: %s: %w", key, value, dataset, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// ZFSGetProperty reads a single property value via `zfs get -H -p -o value`.
// A user property that has never been set returns "-" from zfs(8); we map
// that to an empty string so callers can compare against expected digests
// without special-casing the sentinel.
func (DirectPrivOps) ZFSGetProperty(ctx context.Context, target, key string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, zfs.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "zfs", "get", "-H", "-p", "-o", "value", key, target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("zfs get %s on %s: %s: %w", key, target, strings.TrimSpace(string(out)), err)
	}
	value := strings.TrimSpace(string(out))
	if value == "-" {
		return "", nil
	}
	return value, nil
}

// ZFSCreateVolume creates a sparse-by-default zvol with lz4 compression and
// the requested volblocksize. Compression matches the prior Ansible block;
// callers needing different compression should plumb a flag rather than
// branching here.
func (DirectPrivOps) ZFSCreateVolume(ctx context.Context, dataset string, sizeBytes uint64, volblocksize string) error {
	ctx, cancel := context.WithTimeout(ctx, zfs.Timeout)
	defer cancel()
	if strings.TrimSpace(dataset) == "" || strings.Contains(dataset, "@") {
		return fmt.Errorf("zfs volume dataset is invalid: %s", dataset)
	}
	if sizeBytes == 0 {
		return fmt.Errorf("zfs volume size must be > 0")
	}
	if strings.TrimSpace(volblocksize) == "" {
		return fmt.Errorf("zfs volume volblocksize is required")
	}
	args := []string{
		"create", "-V", strconv.FormatUint(sizeBytes, 10),
		"-o", "volblocksize=" + volblocksize,
		"-o", "compression=lz4",
		dataset,
	}
	cmd := exec.CommandContext(ctx, "zfs", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zfs create -V %d %s: %s: %w", sizeBytes, dataset, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// ZFSWriteVolumeFromFile dd's the source file's bytes onto a zvol device with
// fsync at end. Returns total bytes written so the caller can record the
// seeded size on the trace span. This is the seed equivalent of the legacy
// Ansible "Write ext4 rootfs to staging zvol" task.
func (DirectPrivOps) ZFSWriteVolumeFromFile(ctx context.Context, devicePath, sourcePath string) (uint64, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if !strings.HasPrefix(devicePath, "/dev/zvol/") {
		return 0, fmt.Errorf("device path must be under /dev/zvol/: %s", devicePath)
	}
	src, err := os.Open(sourcePath)
	if err != nil {
		return 0, fmt.Errorf("open source %s: %w", sourcePath, err)
	}
	defer func() { _ = src.Close() }()
	info, err := src.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat source %s: %w", sourcePath, err)
	}
	if info.Size() <= 0 {
		return 0, fmt.Errorf("source %s is empty", sourcePath)
	}
	dst, err := os.OpenFile(devicePath, os.O_WRONLY, 0)
	if err != nil {
		return 0, fmt.Errorf("open device %s: %w", devicePath, err)
	}
	defer func() { _ = dst.Close() }()
	bytesWritten, copyErr := io.Copy(dst, src)
	copiedBytes, copiedErr := uint64FromNonNegativeInt64(bytesWritten, "bytes written")
	if copiedErr != nil {
		return 0, copiedErr
	}
	if copyErr != nil {
		return copiedBytes, fmt.Errorf("copy %s -> %s: %w", sourcePath, devicePath, copyErr)
	}
	if err := dst.Sync(); err != nil {
		return copiedBytes, fmt.Errorf("fsync %s: %w", devicePath, err)
	}
	if err := ctx.Err(); err != nil {
		return copiedBytes, err
	}
	return copiedBytes, nil
}

// ZFSMkfs runs mkfs on a zvol device. fsType currently must be "ext4"; other
// filesystems can be added by extending the switch, but each addition must
// confirm its mkfs(8) accepts the -F (force) and -L (label) flags we use.
func (DirectPrivOps) ZFSMkfs(ctx context.Context, devicePath, fsType, label string) error {
	ctx, cancel := context.WithTimeout(ctx, zfs.Timeout)
	defer cancel()
	if !strings.HasPrefix(devicePath, "/dev/zvol/") {
		return fmt.Errorf("device path must be under /dev/zvol/: %s", devicePath)
	}
	switch fsType {
	case "ext4":
		args := []string{"-F"}
		if label != "" {
			args = append(args, "-L", label)
		}
		args = append(args, devicePath)
		cmd := exec.CommandContext(ctx, "mkfs.ext4", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("mkfs.ext4 %s: %s: %w", devicePath, strings.TrimSpace(string(out)), err)
		}
		return nil
	}
	return fmt.Errorf("unsupported filesystem %q", fsType)
}

// ZFSRename moves a dataset name. Used by the seed flow to atomically promote
// a populated `<image>-staging` zvol into place as `<image>` after the staging
// snapshot has been taken.
func (DirectPrivOps) ZFSRename(ctx context.Context, from, to string) error {
	ctx, cancel := context.WithTimeout(ctx, zfs.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "zfs", "rename", from, to)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zfs rename %s -> %s: %s: %w", from, to, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// ZFSListChildren returns the fully-qualified names of the direct children of
// dataset (depth 1). Used by the seed flow to find workload clones that need
// to be torn down before the image they are derived from can be destroyed.
func (DirectPrivOps) ZFSListChildren(ctx context.Context, dataset string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, zfs.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "zfs", "list", "-H", "-o", "name", "-d", "1", "-r", dataset)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "does not exist") {
			return nil, nil
		}
		return nil, fmt.Errorf("zfs list -d 1 %s: %s: %w", dataset, strings.TrimSpace(string(out)), err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	children := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == dataset {
			continue
		}
		children = append(children, line)
	}
	return children, nil
}

// UnmountStaleZvolMounts force-unmounts every VFS mount whose source is a
// device under /dev/zvol/<pool>/. Callers run this before destroying clones,
// because `zfs destroy -f` only force-unmounts ZFS-native mounts; ext4 over
// zvol is a regular VFS mount and must be detached separately.
func (DirectPrivOps) UnmountStaleZvolMounts(ctx context.Context, pool string) (int, error) {
	if strings.TrimSpace(pool) == "" {
		return 0, fmt.Errorf("pool is required")
	}
	prefix := "/dev/zvol/" + pool + "/"
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return 0, fmt.Errorf("read /proc/mounts: %w", err)
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if !strings.HasPrefix(fields[0], prefix) {
			continue
		}
		umountCtx, cancel := context.WithTimeout(ctx, zfs.Timeout)
		cmd := exec.CommandContext(umountCtx, "umount", "-l", fields[1])
		out, umountErr := cmd.CombinedOutput()
		cancel()
		if umountErr != nil {
			return count, fmt.Errorf("umount -l %s: %s: %w", fields[1], strings.TrimSpace(string(out)), umountErr)
		}
		count++
	}
	return count, nil
}

func (DirectPrivOps) FlushBlockDevice(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, zfs.Timeout)
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
	uid, uidErr := uint32FromInt(ownerUID, "tap owner uid")
	if uidErr != nil {
		return uidErr
	}
	gid, gidErr := uint32FromInt(ownerGID, "tap owner gid")
	if gidErr != nil {
		return gidErr
	}
	tap := &netlink.Tuntap{
		LinkAttrs: netlink.LinkAttrs{Name: tapName},
		Mode:      netlink.TUNTAP_MODE_TAP,
		Owner:     uid,
		Group:     gid,
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
	defer func() { _ = s.Close() }()
	d, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer func() { _ = d.Close() }()
	if _, err := io.Copy(d, s); err != nil {
		return fmt.Errorf("copy %s -> %s: %w", src, dst, err)
	}
	return nil
}
