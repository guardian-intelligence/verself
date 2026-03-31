package firecracker

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
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
	HostInterface  string // outbound interface for guest egress (auto-detected if empty)
	GuestPoolCIDR  string // guest IPv4 pool subdivided into /30s
	NetworkLeaseDir string // persistent lease directory for guest network slots
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Pool:           "benchpool",
		GoldenZvol:     "golden-zvol",
		CIDataset:      "ci",
		KernelPath:     "/var/lib/ci/vmlinux",
		FirecrackerBin: "/usr/local/bin/firecracker",
		JailerBin:      "/usr/local/bin/jailer",
		JailerRoot:     "/srv/jailer",
			JailerUID:      10000,
			JailerGID:      10000,
			VCPUs:          2,
			MemoryMiB:      2048,
			GuestPoolCIDR:  defaultGuestPoolCIDR,
			NetworkLeaseDir: defaultLeaseDir,
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

func (o *Orchestrator) goldenSnapshot() string {
	return fmt.Sprintf("%s/%s@ready", o.cfg.Pool, o.cfg.GoldenZvol)
}

func (o *Orchestrator) cloneDataset(jobID string) string {
	return fmt.Sprintf("%s/%s/%s", o.cfg.Pool, o.cfg.CIDataset, jobID)
}

func (o *Orchestrator) jailDir(jobID string) string {
	return filepath.Join(o.cfg.JailerRoot, "firecracker", jobID, "root")
}

func (o *Orchestrator) ciDatasetPrefix() string {
	return fmt.Sprintf("%s/%s/", o.cfg.Pool, o.cfg.CIDataset)
}

// Run executes a CI job inside a Firecracker VM.
func (o *Orchestrator) Run(ctx context.Context, job JobConfig) (result JobResult, err error) {
	if _, parseErr := uuid.Parse(job.JobID); parseErr != nil {
		err = fmt.Errorf("invalid job ID (must be UUID): %w", parseErr)
		return
	}

	// --- 1. Verify golden snapshot ---
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
	cloneDuration := time.Since(cloneStart)
	o.logger.Info("zvol cloned", "job_id", job.JobID, "duration_ms", cloneDuration.Milliseconds(), "dataset", dataset)

	result, err = o.runDataset(ctx, job, dataset, true)
	if err != nil {
		return result, err
	}
	result.CloneTime = cloneDuration
	return result, nil
}

// RunDataset executes a job against an existing zvol dataset. When destroyAfter
// is true, the dataset is destroyed during cleanup.
func (o *Orchestrator) RunDataset(ctx context.Context, job JobConfig, dataset string, destroyAfter bool) (JobResult, error) {
	if _, parseErr := uuid.Parse(job.JobID); parseErr != nil {
		return JobResult{}, fmt.Errorf("invalid job ID (must be UUID): %w", parseErr)
	}
	return o.runDataset(ctx, job, dataset, destroyAfter)
}

func (o *Orchestrator) runDataset(ctx context.Context, job JobConfig, dataset string, destroyAfter bool) (result JobResult, err error) {
	start := time.Now()
	logger := o.logger.With("job_id", job.JobID, "dataset", dataset)

	var cleanups []func()
	defer func() {
		cleanupStart := time.Now()
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
		result.CleanupTime = time.Since(cleanupStart)
		result.Duration = time.Since(start)
	}()

	if destroyAfter {
		cleanups = append(cleanups, func() {
			if strings.HasPrefix(dataset, o.ciDatasetPrefix()) {
				if destroyErr := zfsDestroy(context.Background(), dataset); destroyErr != nil {
					logger.Warn("zvol destroy failed", "err", destroyErr)
				}
			}
		})
	}

	devPath := zvolDevicePath(dataset)
	mountDir, mountErr := mountZvol(ctx, devPath)
	if mountErr != nil {
		return result, fmt.Errorf("mount zvol: %w", mountErr)
	}

	if writeErr := writeJobConfig(mountDir, job); writeErr != nil {
		_ = unmount(ctx, mountDir)
		return result, fmt.Errorf("write job config: %w", writeErr)
	}

	if umountErr := unmount(ctx, mountDir); umountErr != nil {
		return result, fmt.Errorf("unmount zvol: %w", umountErr)
	}
	logger.Info("job config written to zvol")

	jailStart := time.Now()
	jailRoot := o.jailDir(job.JobID)
	if jailErr := o.setupJail(ctx, jailRoot, devPath); jailErr != nil {
		return result, fmt.Errorf("setup jail: %w", jailErr)
	}
	result.JailSetupTime = time.Since(jailStart)
	logger.Info("jail ready", "duration_ms", result.JailSetupTime.Milliseconds())

	jailBase := filepath.Dir(filepath.Dir(jailRoot))
	cleanups = append(cleanups, func() {
		_ = os.RemoveAll(jailBase)
	})

	netCfg := NetworkPoolConfig{
		PoolCIDR:      o.cfg.GuestPoolCIDR,
		LeaseDir:      o.cfg.NetworkLeaseDir,
		HostInterface: o.cfg.HostInterface,
	}
	netSetup, netCleanup, netErr := setupNetwork(ctx, job.JobID, netCfg)
	if netErr != nil {
		return result, fmt.Errorf("setup network: %w", netErr)
	}
	cleanups = append(cleanups, netCleanup)
	logger.Info("network ready",
		"tap", netSetup.Lease.TapName,
		"subnet", netSetup.Lease.SubnetCIDR,
		"guest_ip", netSetup.Lease.GuestIP,
	)

	apiSockHost := filepath.Join(jailRoot, "run", "firecracker.sock")
	metricsPathHost := filepath.Join(jailRoot, "metrics.json")

	jailerCmd, startErr := o.startJailer(job.JobID)
	if startErr != nil {
		return result, fmt.Errorf("start jailer: %w", startErr)
	}

	var logBuf strings.Builder
	var logWg sync.WaitGroup

	stdout, pipeErr := jailerCmd.StdoutPipe()
	if pipeErr != nil {
		return result, fmt.Errorf("jailer stdout pipe: %w", pipeErr)
	}

	if execErr := jailerCmd.Start(); execErr != nil {
		return result, fmt.Errorf("jailer exec: %w", execErr)
	}
	if attachErr := NewAllocator(netCfg).AttachPID(ctx, job.JobID, jailerCmd.Process.Pid); attachErr != nil {
		return result, fmt.Errorf("record network lease pid: %w", attachErr)
	}

	var jailerExited atomic.Bool
	cleanups = append(cleanups, func() {
		if !jailerExited.Load() {
			_ = jailerCmd.Process.Kill()
			_, _ = jailerCmd.Process.Wait()
		}
	})

	logWg.Add(1)
	go func() {
		defer logWg.Done()
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if logBuf.Len() < 10*1024*1024 {
				logBuf.WriteString(line)
				logBuf.WriteByte('\n')
			}
		}
	}()

	logger.Info("jailer started", "pid", jailerCmd.Process.Pid)

	if waitErr := waitForSocket(ctx, apiSockHost); waitErr != nil {
		return result, fmt.Errorf("wait for API socket: %w", waitErr)
	}

	bootStart := time.Now()
	client := newAPIClient(apiSockHost)

	bootArgs := fmt.Sprintf(
		"root=/dev/vda rw console=ttyS0 reboot=k panic=1 %s init=/sbin/init",
		netSetup.BootIPArg,
	)

	apiSteps := []struct {
		name string
		fn   func() error
	}{
		{"metrics", func() error { return client.putMetrics(ctx, "/metrics.json") }},
		{"boot-source", func() error { return client.putBootSource(ctx, "/vmlinux", bootArgs) }},
		{"rootfs", func() error { return client.putDrive(ctx, "rootfs", "/rootfs", true) }},
			{"machine-config", func() error { return client.putMachineConfig(ctx, o.cfg.VCPUs, o.cfg.MemoryMiB) }},
			{"network", func() error {
				return client.putNetworkInterface(ctx, "eth0", netSetup.Lease.TapName, netSetup.Lease.MAC)
			}},
		}

	for _, step := range apiSteps {
		if apiErr := step.fn(); apiErr != nil {
			return result, fmt.Errorf("configure VM %s: %w", step.name, apiErr)
		}
	}

	if startVMErr := client.startInstance(ctx); startVMErr != nil {
		return result, fmt.Errorf("start VM: %w", startVMErr)
	}
	result.VMBootTime = time.Since(bootStart)
	logger.Info("VM started", "boot_ms", result.VMBootTime.Milliseconds())

	waitDone := make(chan error, 1)
	go func() { waitDone <- jailerCmd.Wait() }()

	select {
	case <-waitDone:
	case <-ctx.Done():
		_ = jailerCmd.Process.Kill()
		<-waitDone
	}
	jailerExited.Store(true)

	logWg.Wait()
	result.Logs = logBuf.String()
	result.ExitCode = parseGuestExitCode(result.Logs)
	logger.Info("VM exited", "exit_code", result.ExitCode)

	result.Metrics = parseMetricsFile(metricsPathHost)

	bgCtx, bgCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer bgCancel()
	if written, writtenErr := zfsWritten(bgCtx, dataset); writtenErr == nil {
		result.ZFSWritten = written
	}

	logger.Info("job complete",
		"exit_code", result.ExitCode,
		"total_ms", time.Since(start).Milliseconds(),
		"boot_ms", result.VMBootTime.Milliseconds(),
		"zfs_written_mb", result.ZFSWritten/(1024*1024),
	)

	return result, nil
}

// setupJail creates the jail directory structure and places the
// kernel, zvol device node, and metrics file inside it.
func (o *Orchestrator) setupJail(ctx context.Context, jailRoot, zvolDevPath string) error {
	for _, dir := range []string{jailRoot, filepath.Join(jailRoot, "run")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}

	// Place kernel in jail. Try hardlink first (instant, same filesystem),
	// fall back to copy (cross-filesystem). The kernel is read-only so
	// hardlinking is safe — all jails share the same inode.
	kernelDst := filepath.Join(jailRoot, "vmlinux")
	if err := os.Link(o.cfg.KernelPath, kernelDst); err != nil {
		if linkErr := copyFile(o.cfg.KernelPath, kernelDst); linkErr != nil {
			return fmt.Errorf("place kernel in jail: %w", linkErr)
		}
	}
	if err := os.Chown(kernelDst, o.cfg.JailerUID, o.cfg.JailerGID); err != nil {
		return fmt.Errorf("chown kernel: %w", err)
	}

	// Create zvol block device node inside jail.
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

	// Create metrics file (regular file, not FIFO).
	// Firecracker appends NDJSON to this. A regular file avoids the
	// reader-must-be-open-before-writer deadlock that FIFOs have.
	metricsFile := filepath.Join(jailRoot, "metrics.json")
	if err := os.WriteFile(metricsFile, nil, 0644); err != nil {
		return fmt.Errorf("create metrics file: %w", err)
	}
	if err := os.Chown(metricsFile, o.cfg.JailerUID, o.cfg.JailerGID); err != nil {
		return fmt.Errorf("chown metrics file: %w", err)
	}

	return nil
}

// startJailer builds the jailer exec.Cmd.
//
// No --new-pid-ns for the tracer bullet. The PID namespace adds a
// fork that makes jailerCmd.Wait() ambiguous about which process
// exited. Without it, the jailer execs Firecracker directly, so
// Wait() returns exactly when the VM shuts down. PID namespace
// isolation is a phase 2 concern — the VM already isolates processes.
func (o *Orchestrator) startJailer(jobID string) (*exec.Cmd, error) {
	args := []string{
		"--id", jobID,
		"--exec-file", o.cfg.FirecrackerBin,
		"--uid", strconv.Itoa(o.cfg.JailerUID),
		"--gid", strconv.Itoa(o.cfg.JailerGID),
		"--chroot-base-dir", o.cfg.JailerRoot,
		"--", // Separator: args after this go to Firecracker.
		"--api-sock", "/run/firecracker.sock",
	}

	cmd := exec.Command(o.cfg.JailerBin, args...)
	return cmd, nil
}

// waitForSocket polls until the Unix socket is connectable.
func waitForSocket(ctx context.Context, path string) error {
	for {
		conn, dialErr := net.DialTimeout("unix", path, 100*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("API socket %s not connectable: %w", path, ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

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

func copyFile(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer s.Close()

	d, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer d.Close()

	if _, err := io.Copy(d, s); err != nil {
		return fmt.Errorf("copy %s -> %s: %w", src, dst, err)
	}
	return nil
}

// parseGuestExitCode extracts the exit code from serial console output.
// The guest init prints "FORGEVM_EXIT_CODE=N" before shutdown.
// Falls back to -1 if the marker is not found (e.g., kernel panic).
func parseGuestExitCode(logs string) int {
	const marker = "FORGEVM_EXIT_CODE="
	for _, line := range strings.Split(logs, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, marker) {
			if code, err := strconv.Atoi(line[len(marker):]); err == nil {
				return code
			}
		}
	}
	return -1
}
