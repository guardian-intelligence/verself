package fastsandbox

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/forge-metal/fast-sandbox/vmproto"
)

// Config holds settings for the Firecracker orchestrator.
type Config struct {
	Pool            string // ZFS pool name, e.g. "forgepool"
	GoldenZvol      string // zvol name under pool, e.g. "golden-zvol"
	CIDataset       string // dataset for job clones, e.g. "ci"
	KernelPath      string // path to vmlinux on host, e.g. "/var/lib/ci/vmlinux"
	FirecrackerBin  string // path to firecracker binary
	JailerBin       string // path to jailer binary
	JailerRoot      string // chroot base dir, e.g. "/srv/jailer"
	JailerUID       int    // unprivileged UID for jailer
	JailerGID       int    // unprivileged GID for jailer
	VCPUs           int    // vCPU count per VM (default 2)
	MemoryMiB       int    // memory per VM in MiB (default 512)
	HostInterface   string // outbound interface for guest egress (auto-detected if empty)
	GuestPoolCIDR   string // guest IPv4 pool subdivided into /30s
	NetworkLeaseDir string // persistent lease directory for guest network slots
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Pool:            "forgepool",
		GoldenZvol:      "golden-zvol",
		CIDataset:       "ci",
		KernelPath:      "/var/lib/ci/vmlinux",
		FirecrackerBin:  "/usr/local/bin/firecracker",
		JailerBin:       "/usr/local/bin/jailer",
		JailerRoot:      "/srv/jailer",
		JailerUID:       10000,
		JailerGID:       10000,
		VCPUs:           2,
		MemoryMiB:       2048,
		GuestPoolCIDR:   defaultGuestPoolCIDR,
		NetworkLeaseDir: defaultLeaseDir,
	}
}

// JobConfig describes the CI job to run inside the VM.
type JobConfig struct {
	JobID          string            `json:"job_id"`
	PrepareCommand []string          `json:"prepare_command,omitempty"`
	PrepareWorkDir string            `json:"prepare_work_dir,omitempty"`
	RunCommand     []string          `json:"run_command"`
	RunWorkDir     string            `json:"run_work_dir,omitempty"`
	Services       []string          `json:"services,omitempty"`
	Env            map[string]string `json:"env"`
}

type PhaseResult struct {
	Name       string
	ExitCode   int
	DurationMS int64
}

// JobResult holds the outcome of a VM job execution.
type JobResult struct {
	ExitCode             int
	Logs                 string
	SerialLogs           string
	Duration             time.Duration
	CloneTime            time.Duration
	JailSetupTime        time.Duration
	VMBootTime           time.Duration
	BootToReadyDuration  time.Duration
	PrepareDuration      time.Duration
	RunDuration          time.Duration
	ServiceStartDuration time.Duration
	VMExitWaitDuration   time.Duration
	CleanupTime          time.Duration
	ZFSWritten           uint64
	StdoutBytes          uint64
	StderrBytes          uint64
	DroppedLogBytes      uint64
	ForcedShutdown       bool
	PhaseResults         []PhaseResult
	FailurePhase         string
	Metrics              *VMMetrics
}

const firecrackerAPIStepTimeout = 5 * time.Second

// Orchestrator manages the full lifecycle of a Firecracker VM job.
type Orchestrator struct {
	cfg    Config
	logger *slog.Logger
	ops    PrivOps
}

// New creates an Orchestrator from configuration. Options override defaults;
// without WithPrivOps, DirectPrivOps{} is used (requires root).
func New(cfg Config, logger *slog.Logger, opts ...Option) *Orchestrator {
	if cfg.VCPUs == 0 {
		cfg.VCPUs = 2
	}
	if cfg.MemoryMiB == 0 {
		cfg.MemoryMiB = 2048
	}
	o := &Orchestrator{cfg: cfg, logger: logger, ops: DirectPrivOps{}}
	for _, opt := range opts {
		opt(o)
	}
	return o
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
	if len(job.RunCommand) == 0 {
		err = fmt.Errorf("job run command is required")
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
	if cloneErr := o.ops.ZFSClone(ctx, o.goldenSnapshot(), dataset, job.JobID); cloneErr != nil {
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
	if len(job.RunCommand) == 0 {
		return JobResult{}, fmt.Errorf("job run command is required")
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
				if destroyErr := o.ops.ZFSDestroy(context.Background(), dataset); destroyErr != nil {
					logger.Warn("zvol destroy failed", "err", destroyErr)
				}
			}
		})
	}

	devPath := zvolDevicePath(dataset)
	if deviceErr := waitForDevice(ctx, devPath); deviceErr != nil {
		return result, fmt.Errorf("wait for zvol device %s: %w", devPath, deviceErr)
	}

	jailStart := time.Now()
	jailRoot := o.jailDir(job.JobID)
	if jailErr := o.ops.SetupJail(ctx, jailRoot, devPath, o.cfg.KernelPath, o.cfg.JailerUID, o.cfg.JailerGID); jailErr != nil {
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
	netSetup, netCleanup, netErr := setupNetwork(ctx, job.JobID, netCfg, o.ops)
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
	controlSockHost := filepath.Join(jailRoot, "run", "forge-control.sock")
	metricsPathHost := filepath.Join(jailRoot, "metrics.json")

	jailer, startErr := o.ops.StartJailer(ctx, job.JobID, JailerConfig{
		FirecrackerBin: o.cfg.FirecrackerBin,
		JailerBin:      o.cfg.JailerBin,
		ChrootBaseDir:  o.cfg.JailerRoot,
		UID:            o.cfg.JailerUID,
		GID:            o.cfg.JailerGID,
	})
	if startErr != nil {
		return result, fmt.Errorf("start jailer: %w", startErr)
	}
	if attachErr := NewAllocator(netCfg).AttachPID(ctx, job.JobID, jailer.Pid); attachErr != nil {
		return result, fmt.Errorf("record network lease pid: %w", attachErr)
	}

	var jailerExited atomic.Bool
	cleanups = append(cleanups, func() {
		if !jailerExited.Load() {
			_ = jailer.Kill()
			_ = jailer.Wait()
		}
	})

	var serialBuf strings.Builder
	var logWg sync.WaitGroup
	if jailer.Stdout != nil {
		logWg.Add(1)
		go captureSerialOutput(jailer.Stdout, &serialBuf, &logWg)
	}
	if jailer.Stderr != nil {
		logWg.Add(1)
		go captureSerialOutput(jailer.Stderr, &serialBuf, &logWg)
	}

	logger.Info("jailer started", "pid", jailer.Pid)

	if waitErr := waitForSocket(ctx, apiSockHost); waitErr != nil {
		return result, fmt.Errorf("wait for API socket: %w", waitErr)
	}

	bootStart := time.Now()
	client := newAPIClient(apiSockHost)

	bootArgs := "root=/dev/vda rw console=ttyS0 reboot=k panic=1 init=/sbin/init"

	apiSteps := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"metrics", func(stepCtx context.Context) error { return client.putMetrics(stepCtx, "/metrics.json") }},
		{"boot-source", func(stepCtx context.Context) error { return client.putBootSource(stepCtx, "/vmlinux", bootArgs) }},
		{"rootfs", func(stepCtx context.Context) error { return client.putDrive(stepCtx, "rootfs", "/rootfs", true) }},
		{"machine-config", func(stepCtx context.Context) error {
			return client.putMachineConfig(stepCtx, o.cfg.VCPUs, o.cfg.MemoryMiB)
		}},
		{"network", func(stepCtx context.Context) error {
			return client.putNetworkInterface(stepCtx, "eth0", netSetup.Lease.TapName, netSetup.Lease.MAC)
		}},
		{"vsock", func(stepCtx context.Context) error {
			// Each VM needs a unique CID on the host. CID 0-2 are reserved
			// (0=hypervisor, 1=reserved, 2=host). Derive from the network
			// slot index which is already unique per concurrent VM.
			cid := uint32(netSetup.Lease.SlotIndex) + 3
			return client.putVsock(stepCtx, cid, "/run/forge-control.sock")
		}},
		{"entropy", func(stepCtx context.Context) error { return client.putEntropy(stepCtx) }},
	}

	for _, step := range apiSteps {
		stepStart := time.Now()
		logger.Info("configuring firecracker", "step", step.name)
		stepCtx, cancel := context.WithTimeout(ctx, firecrackerAPIStepTimeout)
		apiErr := step.fn(stepCtx)
		cancel()
		if apiErr != nil {
			return result, fmt.Errorf("configure VM %s: %w", step.name, apiErr)
		}
		logger.Info("configured firecracker",
			"step", step.name,
			"duration_ms", time.Since(stepStart).Milliseconds(),
		)
	}

	startVMStart := time.Now()
	logger.Info("configuring firecracker", "step", "instance-start")
	startCtx, cancel := context.WithTimeout(ctx, firecrackerAPIStepTimeout)
	startVMErr := client.startInstance(startCtx)
	cancel()
	if startVMErr != nil {
		return result, fmt.Errorf("start VM: %w", startVMErr)
	}
	logger.Info("configured firecracker",
		"step", "instance-start",
		"duration_ms", time.Since(startVMStart).Milliseconds(),
	)
	result.VMBootTime = time.Since(bootStart)
	logger.Info("VM started", "boot_ms", result.VMBootTime.Milliseconds())

	waitDone := make(chan error, 1)
	go func() { waitDone <- jailer.Wait() }()

	control, controlErr := connectGuestControl(ctx, controlSockHost, vmproto.GuestPort)
	if controlErr != nil {
		_ = jailer.Kill()
		<-waitDone
		jailerExited.Store(true)
		logWg.Wait()
		result.SerialLogs = serialBuf.String()
		return result, fmt.Errorf("connect guest control: %w", controlErr)
	}
	defer control.close()

	controlDone := make(chan struct {
		result guestControlResult
		err    error
	}, 1)
	go func() {
		controlResult, err := control.run(job, netSetup.Lease, logger)
		controlDone <- struct {
			result guestControlResult
			err    error
		}{result: controlResult, err: err}
	}()

	var (
		controlResult guestControlResult
		controlRunErr error
	)

	select {
	case outcome := <-controlDone:
		controlResult = outcome.result
		controlRunErr = outcome.err
	case <-ctx.Done():
		_ = control.send(vmproto.TypeCancel, vmproto.Cancel{Reason: ctx.Err().Error()})
		select {
		case outcome := <-controlDone:
			controlResult = outcome.result
			controlRunErr = ctx.Err()
			if outcome.err != nil {
				controlRunErr = outcome.err
			}
		case <-time.After(vmproto.CancelGracePeriod):
			_ = jailer.Kill()
			<-waitDone
			jailerExited.Store(true)
			logWg.Wait()
			result.Logs = controlResult.logs
			result.SerialLogs = serialBuf.String()
			return result, ctx.Err()
		}
	}

	shutdownWaitStart := time.Now()
	select {
	case waitErr := <-waitDone:
		result.VMExitWaitDuration = time.Since(shutdownWaitStart)
		jailerExited.Store(true)
		if waitErr != nil && controlRunErr == nil {
			controlRunErr = fmt.Errorf("wait for VM exit: %w", waitErr)
		}
		logger.Info("VM process exited after guest shutdown", "wait_ms", result.VMExitWaitDuration.Milliseconds())
	case <-time.After(15 * time.Second):
		result.VMExitWaitDuration = time.Since(shutdownWaitStart)
		result.ForcedShutdown = true
		logger.Warn("VM did not exit after guest shutdown; killing Firecracker", "wait_ms", result.VMExitWaitDuration.Milliseconds())
		_ = jailer.Kill()
		<-waitDone
		jailerExited.Store(true)
		if controlRunErr == nil {
			controlRunErr = fmt.Errorf("timed out waiting for VM exit after guest shutdown")
		}
	}

	logWg.Wait()
	result.Logs = controlResult.logs
	result.SerialLogs = serialBuf.String()
	result.ExitCode = controlResult.result.ExitCode
	result.PrepareDuration = time.Duration(controlResult.result.PrepareDurationMS) * time.Millisecond
	result.RunDuration = time.Duration(controlResult.result.RunDurationMS) * time.Millisecond
	result.ServiceStartDuration = time.Duration(controlResult.result.ServiceStartDurationMS) * time.Millisecond
	result.BootToReadyDuration = time.Duration(controlResult.hello.BootToReadyMS) * time.Millisecond
	result.StdoutBytes = controlResult.result.StdoutBytes
	result.StderrBytes = controlResult.result.StderrBytes
	result.DroppedLogBytes = controlResult.result.DroppedLogBytes
	result.PhaseResults = append([]PhaseResult(nil), controlResult.phases...)
	result.FailurePhase = firstFailedPhase(controlResult.phases)
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

	return result, controlRunErr
}

func firstFailedPhase(phases []PhaseResult) string {
	for _, phase := range phases {
		if phase.ExitCode != 0 {
			return phase.Name
		}
	}
	return ""
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

func captureSerialOutput(reader io.Reader, dst *strings.Builder, wg *sync.WaitGroup) {
	defer wg.Done()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if dst.Len() < 10*1024*1024 {
			dst.WriteString(line)
			dst.WriteByte('\n')
		}
	}
}
