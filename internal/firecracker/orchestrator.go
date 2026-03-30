package firecracker

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config holds settings for the Firecracker orchestrator.
type Config struct {
	Pool           string // ZFS pool name, e.g. "benchpool"
	GoldenZvol     string // zvol name under pool, e.g. "golden-zvol"
	CIDataset      string // dataset for job clones, e.g. "ci"
	KernelPath     string // path to vmlinux on host, e.g. "/var/lib/ci/vmlinux"
	FirecrackerBin string // path to firecracker binary
	JailerBin      string // path to jailer binary
	JailerRoot     string // chroot base dir, e.g. "/srv/jailer"
	JailerUID      int    // unprivileged UID for jailer
	JailerGID      int    // unprivileged GID for jailer
	VCPUs          int    // vCPU count per VM (default 2)
	MemoryMiB      int    // memory per VM in MiB (default 512)
	HostInterface  string // outbound interface for NAT (auto-detected if empty)
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Pool:           "benchpool",
		GoldenZvol:     "golden-zvol",
		CIDataset:      "ci",
		KernelPath:     "/var/lib/ci/vmlinux",
		FirecrackerBin: "/nix/var/nix/profiles/default/bin/firecracker",
		JailerBin:      "/nix/var/nix/profiles/default/bin/jailer",
		JailerRoot:     "/srv/jailer",
		JailerUID:      10000,
		JailerGID:      10000,
		VCPUs:          2,
		MemoryMiB:      512,
	}
}

// JobConfig describes the CI job to run inside the VM.
type JobConfig struct {
	JobID   string            `json:"job_id"`
	Command []string          `json:"command"`
	Env     map[string]string `json:"env"`
	WorkDir string            `json:"work_dir"`
}

// JobResult holds the outcome of a VM job execution.
type JobResult struct {
	ExitCode      int
	Logs          string
	Duration      time.Duration
	CloneTime     time.Duration
	JailSetupTime time.Duration
	VMBootTime    time.Duration
	CleanupTime   time.Duration
	ZFSWritten    uint64
	Metrics       *VMMetrics
}

// Orchestrator manages the full lifecycle of a Firecracker VM job.
type Orchestrator struct {
	cfg    Config
	logger *slog.Logger
}

// New creates an Orchestrator from configuration.
func New(cfg Config, logger *slog.Logger) *Orchestrator {
	if cfg.VCPUs == 0 {
		cfg.VCPUs = 2
	}
	if cfg.MemoryMiB == 0 {
		cfg.MemoryMiB = 512
	}
	return &Orchestrator{cfg: cfg, logger: logger}
}

// goldenSnapshot returns the full golden zvol snapshot path.
func (o *Orchestrator) goldenSnapshot() string {
	return fmt.Sprintf("%s/%s@ready", o.cfg.Pool, o.cfg.GoldenZvol)
}

// cloneDataset returns the full dataset path for a job clone.
func (o *Orchestrator) cloneDataset(jobID string) string {
	return fmt.Sprintf("%s/%s/%s", o.cfg.Pool, o.cfg.CIDataset, jobID)
}

// jailDir returns the jail root directory path for a job.
// Matches jailer convention: <base>/<exec-name>/<id>/root/
func (o *Orchestrator) jailDir(jobID string) string {
	return filepath.Join(o.cfg.JailerRoot, "firecracker", jobID, "root")
}

// Run executes a CI job inside a Firecracker VM.
//
// Full lifecycle with LIFO cleanup on any error:
//
//	clone zvol -> mount & write config -> jail setup -> network ->
//	start jailer -> configure VM -> boot -> wait -> collect metrics -> cleanup
func (o *Orchestrator) Run(ctx context.Context, job JobConfig) (result JobResult, err error) {
	start := time.Now()
	logger := o.logger.With("job_id", job.JobID)

	// LIFO cleanup stack (Velo pattern).
	var cleanups []func()
	defer func() {
		cleanupStart := time.Now()
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
		result.CleanupTime = time.Since(cleanupStart)
		result.Duration = time.Since(start)
	}()

	// --- 1. Verify golden snapshot exists ---
	exists, checkErr := zfsSnapshotExists(ctx, o.goldenSnapshot())
	if checkErr != nil {
		err = fmt.Errorf("check golden snapshot: %w", checkErr)
		return
	}
	if !exists {
		err = fmt.Errorf("golden snapshot %s does not exist — run golden image setup first", o.goldenSnapshot())
		return
	}

	// --- 2. Clone zvol ---
	cloneStart := time.Now()
	dataset := o.cloneDataset(job.JobID)
	if cloneErr := zfsClone(ctx, o.goldenSnapshot(), dataset); cloneErr != nil {
		err = fmt.Errorf("clone zvol: %w", cloneErr)
		return
	}
	result.CloneTime = time.Since(cloneStart)
	logger.Info("zvol cloned", "duration_ms", result.CloneTime.Milliseconds(), "dataset", dataset)

	cleanups = append(cleanups, func() {
		if destroyErr := zfsDestroy(context.Background(), dataset); destroyErr != nil {
			logger.Warn("zvol destroy failed", "err", destroyErr)
		}
	})

	// --- 3. Mount clone, write job config, unmount ---
	devPath := zvolDevicePath(dataset)
	mountDir, mountErr := mountZvol(ctx, devPath)
	if mountErr != nil {
		err = fmt.Errorf("mount zvol: %w", mountErr)
		return
	}

	if writeErr := writeJobConfig(mountDir, job); writeErr != nil {
		unmount(ctx, mountDir)
		err = fmt.Errorf("write job config: %w", writeErr)
		return
	}

	if umountErr := unmount(ctx, mountDir); umountErr != nil {
		err = fmt.Errorf("unmount zvol: %w", umountErr)
		return
	}
	logger.Info("job config written to zvol")

	// --- 4. Set up jail ---
	jailStart := time.Now()
	jailRoot := o.jailDir(job.JobID)
	if jailErr := o.setupJail(ctx, jailRoot, devPath); jailErr != nil {
		err = fmt.Errorf("setup jail: %w", jailErr)
		return
	}
	result.JailSetupTime = time.Since(jailStart)
	logger.Info("jail ready", "duration_ms", result.JailSetupTime.Milliseconds())

	// Cleanup: remove the entire jail tree (grandparent of root/).
	jailBase := filepath.Dir(filepath.Dir(jailRoot))
	cleanups = append(cleanups, func() {
		os.RemoveAll(jailBase)
	})

	// --- 5. Set up network ---
	net, netCleanup, netErr := setupNetwork(ctx, job.JobID, o.cfg.HostInterface)
	if netErr != nil {
		err = fmt.Errorf("setup network: %w", netErr)
		return
	}
	cleanups = append(cleanups, netCleanup)
	logger.Info("network ready", "tap", net.TapName)

	// --- 6. Start jailer ---
	apiSockHost := filepath.Join(jailRoot, "run", "firecracker.sock")
	metricsPathHost := filepath.Join(jailRoot, "metrics.fifo")

	jailerCmd, startErr := o.startJailer(ctx, job.JobID)
	if startErr != nil {
		err = fmt.Errorf("start jailer: %w", startErr)
		return
	}

	// Capture serial output from jailer stdout.
	var logBuf strings.Builder
	var logWg sync.WaitGroup

	stdout, pipeErr := jailerCmd.StdoutPipe()
	if pipeErr != nil {
		err = fmt.Errorf("jailer stdout pipe: %w", pipeErr)
		return
	}
	// Stderr goes to parent's stderr (terminal) for jailer diagnostics.
	// Serial console output (from guest init) goes through stdout pipe.

	if execErr := jailerCmd.Start(); execErr != nil {
		err = fmt.Errorf("jailer exec: %w", execErr)
		return
	}
	cleanups = append(cleanups, func() {
		if jailerCmd.Process != nil {
			jailerCmd.Process.Kill()
			jailerCmd.Wait()
		}
	})

	logWg.Add(1)
	go func() {
		defer logWg.Done()
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			logBuf.WriteString(line)
			logBuf.WriteByte('\n')
		}
	}()

	logger.Info("jailer started", "pid", jailerCmd.Process.Pid)

	// --- 7. Wait for API socket ---
	if waitErr := waitForSocket(ctx, apiSockHost); waitErr != nil {
		err = fmt.Errorf("wait for API socket: %w", waitErr)
		return
	}

	// --- 8. Configure VM via API ---
	bootStart := time.Now()
	client := newAPIClient(apiSockHost)

	// Boot args: serial console, static IP, init path.
	bootArgs := fmt.Sprintf("console=ttyS0 reboot=k panic=1 %s init=/sbin/init", net.BootIPArg)

	// Order matters: boot-source, drives, machine-config, network, then start.
	apiSteps := []struct {
		name string
		fn   func() error
	}{
		{"logger", func() error { return client.putLogger(ctx, "/dev/null") }},
		{"metrics", func() error { return client.putMetrics(ctx, "/metrics.fifo") }},
		{"boot-source", func() error { return client.putBootSource(ctx, "/vmlinux", bootArgs) }},
		{"rootfs", func() error { return client.putDrive(ctx, "rootfs", "/rootfs", true) }},
		{"machine-config", func() error { return client.putMachineConfig(ctx, o.cfg.VCPUs, o.cfg.MemoryMiB) }},
		{"network", func() error {
			return client.putNetworkInterface(ctx, "eth0", net.TapName, net.MAC)
		}},
	}

	for _, step := range apiSteps {
		if apiErr := step.fn(); apiErr != nil {
			err = fmt.Errorf("configure VM %s: %w", step.name, apiErr)
			return
		}
	}

	// --- 9. Start VM ---
	if startVMErr := client.startInstance(ctx); startVMErr != nil {
		err = fmt.Errorf("start VM: %w", startVMErr)
		return
	}
	result.VMBootTime = time.Since(bootStart)
	logger.Info("VM started", "boot_ms", result.VMBootTime.Milliseconds())

	// --- 10. Wait for VM exit ---
	// The jailer process exits when Firecracker exits (which happens
	// when the guest init calls os.Exit or the VM is killed).
	waitErr := jailerCmd.Wait()
	logWg.Wait()
	result.Logs = logBuf.String()

	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
			logger.Warn("jailer wait error", "err", waitErr)
		}
	}
	// Prevent cleanup from killing an already-exited process.
	jailerCmd.Process = nil

	logger.Info("VM exited", "exit_code", result.ExitCode)

	// --- 11. Flush and parse metrics ---
	// FlushMetrics may fail if VM already exited. Best-effort.
	result.Metrics = parseMetricsFile(metricsPathHost)

	// --- 12. Read ZFS written bytes ---
	if written, writtenErr := zfsWritten(ctx, dataset); writtenErr == nil {
		result.ZFSWritten = written
	}

	logger.Info("job complete",
		"exit_code", result.ExitCode,
		"total_ms", time.Since(start).Milliseconds(),
		"clone_ms", result.CloneTime.Milliseconds(),
		"boot_ms", result.VMBootTime.Milliseconds(),
		"zfs_written_mb", result.ZFSWritten/(1024*1024),
	)

	return
}

// setupJail creates the jail directory structure and places the
// kernel and zvol device node inside it.
func (o *Orchestrator) setupJail(ctx context.Context, jailRoot, zvolDevPath string) error {
	// Create jail directory structure.
	for _, dir := range []string{jailRoot, filepath.Join(jailRoot, "run")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}

	// Copy kernel to jail. Firecracker reads it from inside the chroot.
	kernelDst := filepath.Join(jailRoot, "vmlinux")
	if err := copyFile(o.cfg.KernelPath, kernelDst); err != nil {
		return fmt.Errorf("copy kernel: %w", err)
	}
	if err := os.Chown(kernelDst, o.cfg.JailerUID, o.cfg.JailerGID); err != nil {
		return fmt.Errorf("chown kernel: %w", err)
	}

	// Create zvol block device node inside jail.
	// The jailer creates /dev/kvm, /dev/net/tun, /dev/urandom but we
	// must create the zvol device node ourselves.
	major, minor, err := deviceMajorMinor(ctx, zvolDevPath)
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
	if err := os.Chown(rootfsDev, o.cfg.JailerUID, o.cfg.JailerGID); err != nil {
		return fmt.Errorf("chown rootfs device: %w", err)
	}

	return nil
}

// startJailer launches the jailer process. Returns the exec.Cmd
// (not yet started — caller should set up pipes and call Start).
func (o *Orchestrator) startJailer(ctx context.Context, jobID string) (*exec.Cmd, error) {
	args := []string{
		"--id", jobID,
		"--exec-file", o.cfg.FirecrackerBin,
		"--uid", strconv.Itoa(o.cfg.JailerUID),
		"--gid", strconv.Itoa(o.cfg.JailerGID),
		"--chroot-base-dir", o.cfg.JailerRoot,
		"--new-pid-ns",
		"--", // Separator: args after this go to Firecracker.
		"--api-sock", "/run/firecracker.sock",
	}

	cmd := exec.CommandContext(ctx, o.cfg.JailerBin, args...)
	return cmd, nil
}

// waitForSocket polls until the Unix socket file appears.
func waitForSocket(ctx context.Context, path string) error {
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("API socket %s did not appear: %w", path, ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// writeJobConfig writes the job config JSON to /etc/ci/job.json
// on the mounted zvol filesystem.
func writeJobConfig(mountDir string, job JobConfig) error {
	configDir := filepath.Join(mountDir, "etc", "ci")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", configDir, err)
	}

	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal job config: %w", err)
	}

	configPath := filepath.Join(configDir, "job.json")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}
	return nil
}

// copyFile copies src to dst. Used for placing the kernel in the jail.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	return os.WriteFile(dst, data, 0644)
}
