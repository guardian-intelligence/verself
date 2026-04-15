package vmorchestrator

import (
	"bufio"
	"context"
	"errors"
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

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/forge-metal/apiwire"
	"github.com/forge-metal/vm-orchestrator/vmproto"
)

var tracer = otel.Tracer("vm-orchestrator")

const (
	defaultTrustClass      = "trusted"
	firecrackerStepTimeout = 5 * time.Second
	maxBufferedGuestLogs   = 10 * 1024 * 1024
)

type Config struct {
	Pool            string
	GoldenZvol      string
	WorkloadDataset string
	KernelPath      string
	FirecrackerBin  string
	JailerBin       string
	JailerRoot      string
	JailerUID       int
	JailerGID       int
	Bounds          apiwire.VMResourceBounds
	HostInterface   string
	GuestPoolCIDR   string
	StateDBPath     string
	HostServiceIP   string
	HostServicePort int
}

func DefaultConfig() Config {
	return Config{
		Pool:            "forgepool",
		GoldenZvol:      "golden-zvol",
		WorkloadDataset: "workloads",
		KernelPath:      "/var/lib/forge-metal/guest-artifacts/vmlinux",
		FirecrackerBin:  "/usr/local/bin/firecracker",
		JailerBin:       "/usr/local/bin/jailer",
		JailerRoot:      "/srv/jailer",
		JailerUID:       10000,
		JailerGID:       10000,
		Bounds:          apiwire.DefaultBounds,
		GuestPoolCIDR:   defaultGuestPoolCIDR,
		StateDBPath:     defaultStateDBPath,
		HostServiceIP:   defaultHostServiceIP,
		HostServicePort: defaultHostServicePort,
	}
}

type LeaseState int

const (
	LeaseStateUnspecified LeaseState = iota
	LeaseStateAcquiring
	LeaseStateReady
	LeaseStateDraining
	LeaseStateReleased
	LeaseStateExpired
	LeaseStateCrashed
)

func (s LeaseState) Terminal() bool {
	return s == LeaseStateReleased || s == LeaseStateExpired || s == LeaseStateCrashed
}

type ExecState int

const (
	ExecStateUnspecified ExecState = iota
	ExecStatePending
	ExecStateRunning
	ExecStateExited
	ExecStateFailed
	ExecStateCanceled
	ExecStateKilledByLeaseExpiry
)

func (s ExecState) Terminal() bool {
	return s == ExecStateExited || s == ExecStateFailed || s == ExecStateCanceled || s == ExecStateKilledByLeaseExpiry
}

type LeaseEventType string

const (
	LeaseEventLeaseAcquired       LeaseEventType = "lease_acquired"
	LeaseEventVMBooting           LeaseEventType = "vm_booting"
	LeaseEventVMReady             LeaseEventType = "vm_ready"
	LeaseEventLeaseRenewed        LeaseEventType = "lease_renewed"
	LeaseEventExecStarted         LeaseEventType = "exec_started"
	LeaseEventExecFinished        LeaseEventType = "exec_finished"
	LeaseEventExecCanceled        LeaseEventType = "exec_canceled"
	LeaseEventCheckpointSaved     LeaseEventType = "checkpoint_saved"
	LeaseEventVMShutdown          LeaseEventType = "vm_shutdown"
	LeaseEventLeaseExpired        LeaseEventType = "lease_expired"
	LeaseEventLeaseReleased       LeaseEventType = "lease_released"
	LeaseEventLeaseCrashed        LeaseEventType = "lease_crashed"
	LeaseEventTelemetryDiagnostic LeaseEventType = "telemetry_diagnostic"
)

type LeaseSpec struct {
	Resources               apiwire.VMResources
	FromCheckpointRef       string
	TTLSeconds              uint64
	TrustClass              string
	CheckpointSaveAllowlist []string
	NetworkMode             string
}

type ExecSpec struct {
	Argv           []string
	WorkingDir     string
	Env            map[string]string
	MaxWallSeconds uint64
}

type ExecResult struct {
	ExitCode               int
	Output                 string
	Duration               time.Duration
	StartedAt              time.Time
	FirstByteAt            time.Time
	ExitedAt               time.Time
	StdoutBytes            uint64
	StderrBytes            uint64
	DroppedLogBytes        uint64
	ZFSWritten             uint64
	RootfsProvisionedBytes uint64
	Metrics                *VMMetrics
}

type LeaseRuntime struct {
	LeaseID string
	Dataset string
	Network NetworkLease

	control         *guestControl
	jailer          *JailerProcess
	metricsPath     string
	cancelTelemetry context.CancelFunc
	telemetryDone   chan struct{}

	waitDone     chan error
	jailerExited atomic.Bool
	serialBuf    strings.Builder
	logWg        sync.WaitGroup

	cleanups []func()
	logger   *slog.Logger
}

type Orchestrator struct {
	cfg    Config
	logger *slog.Logger
	ops    PrivOps
}

type Option func(*Orchestrator)

func WithPrivOps(ops PrivOps) Option {
	return func(o *Orchestrator) {
		o.ops = ops
	}
}

func New(cfg Config, logger *slog.Logger, opts ...Option) *Orchestrator {
	base := DefaultConfig()
	if cfg.Pool != "" {
		base = cfg
	}
	if base.Bounds == (apiwire.VMResourceBounds{}) {
		base.Bounds = apiwire.DefaultBounds
	}
	if base.HostServiceIP == "" {
		base.HostServiceIP = defaultHostServiceIP
	}
	if base.HostServicePort == 0 {
		base.HostServicePort = defaultHostServicePort
	}
	if logger == nil {
		logger = slog.Default()
	}
	o := &Orchestrator{cfg: base, logger: logger, ops: DirectPrivOps{}}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// normalizeLeaseSpec fills in defaults and re-validates the VM shape
// against the host bounds. Validation at this layer is a defense in depth
// for callers that build LeaseSpec directly; the RPC path already checks
// at rpc_convert, but in-process constructors (tests, tracer bullets)
// still flow through here.
func normalizeLeaseSpec(spec LeaseSpec, cfg Config) (LeaseSpec, error) {
	spec.TrustClass = strings.TrimSpace(spec.TrustClass)
	if spec.TrustClass == "" {
		spec.TrustClass = defaultTrustClass
	}
	spec.Resources = spec.Resources.Normalize()
	bounds := cfg.Bounds
	if bounds == (apiwire.VMResourceBounds{}) {
		bounds = apiwire.DefaultBounds
	}
	if err := spec.Resources.Validate(bounds); err != nil {
		return LeaseSpec{}, err
	}
	if spec.TTLSeconds == 0 {
		spec.TTLSeconds = 5 * 60
	}
	return spec, nil
}

func normalizeExecSpec(spec ExecSpec) ExecSpec {
	spec.WorkingDir = strings.TrimSpace(spec.WorkingDir)
	if spec.Env == nil {
		spec.Env = map[string]string{}
	}
	return spec
}

func validateExecSpec(spec ExecSpec) error {
	if len(spec.Argv) == 0 {
		return fmt.Errorf("argv is required")
	}
	for _, arg := range spec.Argv {
		if strings.ContainsRune(arg, 0) {
			return fmt.Errorf("argv contains NUL byte")
		}
	}
	return nil
}

func (o *Orchestrator) goldenZvolDataset() string {
	return fmt.Sprintf("%s/%s", o.cfg.Pool, o.cfg.GoldenZvol)
}

func (o *Orchestrator) goldenSnapshot() string {
	return o.goldenZvolDataset() + "@ready"
}

func (o *Orchestrator) leaseDataset(leaseID string) string {
	return fmt.Sprintf("%s/%s/%s", o.cfg.Pool, o.cfg.WorkloadDataset, leaseID)
}

func (o *Orchestrator) jailDir(leaseID string) string {
	return filepath.Join(o.cfg.JailerRoot, "firecracker", leaseID, "root")
}

func (o *Orchestrator) workloadDatasetPrefix() string {
	return fmt.Sprintf("%s/%s/", o.cfg.Pool, o.cfg.WorkloadDataset)
}

func (o *Orchestrator) destroyDisposableWorkloadDataset(ctx context.Context, dataset string) error {
	if !strings.HasPrefix(dataset, o.workloadDatasetPrefix()) {
		return nil
	}
	return o.ops.ZFSDestroy(ctx, dataset)
}

func (o *Orchestrator) BootLease(ctx context.Context, leaseID string, spec LeaseSpec, observer LeaseObserver) (*LeaseRuntime, error) {
	normalized, normErr := normalizeLeaseSpec(spec, o.cfg)
	if normErr != nil {
		return nil, fmt.Errorf("normalize lease spec: %w", normErr)
	}
	spec = normalized
	ctx, span := tracer.Start(ctx, "vmorchestrator.lease.boot",
		trace.WithAttributes(
			attribute.String("lease.id", leaseID),
			attribute.Int("vmresources.vcpus", int(spec.Resources.VCPUs)),
			attribute.Int("vmresources.memory_mib", int(spec.Resources.MemoryMiB)),
			attribute.Int("vmresources.root_disk_gib", int(spec.Resources.RootDiskGiB)),
			attribute.String("vmresources.kernel_image", string(spec.Resources.KernelImage)),
		),
	)
	var err error
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	snapshotCtx, endSnapshotSpan := startStepSpan(ctx, "vmorchestrator.zfs.snapshot_check",
		attribute.String("lease.id", leaseID),
		attribute.String("zfs.snapshot", o.goldenSnapshot()),
	)
	exists, checkErr := zfsSnapshotExists(snapshotCtx, o.goldenSnapshot())
	endSnapshotSpan(checkErr)
	if checkErr != nil {
		err = fmt.Errorf("check golden snapshot: %w", checkErr)
		return nil, err
	}
	if !exists {
		err = fmt.Errorf("golden snapshot %s does not exist", o.goldenSnapshot())
		return nil, err
	}

	dataset := o.leaseDataset(leaseID)
	cloneCtx, endCloneSpan := startStepSpan(ctx, "vmorchestrator.zfs.clone",
		attribute.String("lease.id", leaseID),
		attribute.String("zfs.snapshot", o.goldenSnapshot()),
		attribute.String("zfs.dataset", dataset),
	)
	if cloneErr := o.ops.ZFSClone(cloneCtx, o.goldenSnapshot(), dataset, leaseID); cloneErr != nil {
		endCloneSpan(cloneErr)
		err = fmt.Errorf("clone zvol: %w", cloneErr)
		return nil, err
	}
	endCloneSpan(nil)

	// Apply per-clone refquota matching the requested root disk size. This
	// is a write cap — the guest-visible zvol volsize is inherited from the
	// golden (unchanged), but writes past refquota return ENOSPC. Refquota
	// is also what billing aggregates `disk_gib_ms` from. refreservation
	// pre-reserves the space so parallel leases can't starve each other.
	quotaValue := fmt.Sprintf("%dG", spec.Resources.RootDiskGiB)
	quotaCtx, endQuotaSpan := startStepSpan(ctx, "vmorchestrator.zvol.quota_set",
		attribute.String("lease.id", leaseID),
		attribute.String("zfs.dataset", dataset),
		attribute.Int("vmresources.root_disk_gib", int(spec.Resources.RootDiskGiB)),
	)
	if quotaErr := o.ops.ZFSSetProperty(quotaCtx, dataset, "refquota", quotaValue); quotaErr != nil {
		endQuotaSpan(quotaErr)
		err = fmt.Errorf("zfs refquota: %w", quotaErr)
		_ = o.destroyDisposableWorkloadDataset(context.Background(), dataset)
		return nil, err
	}
	if reserveErr := o.ops.ZFSSetProperty(quotaCtx, dataset, "refreservation", quotaValue); reserveErr != nil {
		endQuotaSpan(reserveErr)
		err = fmt.Errorf("zfs refreservation: %w", reserveErr)
		_ = o.destroyDisposableWorkloadDataset(context.Background(), dataset)
		return nil, err
	}
	endQuotaSpan(nil)

	runtime, bootErr := o.bootDataset(ctx, leaseID, spec, dataset, observer)
	if bootErr != nil {
		_ = o.destroyDisposableWorkloadDataset(context.Background(), dataset)
		err = bootErr
		return nil, err
	}
	runtime.cleanups = append(runtime.cleanups, func() {
		if destroyErr := o.destroyDisposableWorkloadDataset(context.Background(), dataset); destroyErr != nil {
			runtime.logger.WarnContext(context.Background(), "zvol destroy failed", "error", destroyErr, "dataset", dataset)
		}
	})
	return runtime, nil
}

func (o *Orchestrator) bootDataset(ctx context.Context, leaseID string, spec LeaseSpec, dataset string, observer LeaseObserver) (*LeaseRuntime, error) {
	logger := o.logger.With("lease_id", leaseID, "dataset", dataset)
	runtime := &LeaseRuntime{
		LeaseID: leaseID,
		Dataset: dataset,
		logger:  logger,
	}
	cleanupOnErr := true
	defer func() {
		if cleanupOnErr {
			runtime.Cleanup("boot_failed")
		}
	}()

	devPath := zvolDevicePath(dataset)
	deviceCtx, endDeviceSpan := startStepSpan(ctx, "vmorchestrator.zvol.wait_device",
		attribute.String("lease.id", leaseID),
		attribute.String("zfs.dataset", dataset),
		attribute.String("device.path", devPath),
	)
	if err := waitForDevice(deviceCtx, devPath); err != nil {
		endDeviceSpan(err)
		return nil, fmt.Errorf("wait for zvol device %s: %w", devPath, err)
	}
	endDeviceSpan(nil)

	jailRoot := o.jailDir(leaseID)
	jailCtx, endJailSpan := startStepSpan(ctx, "vmorchestrator.jail.setup",
		attribute.String("lease.id", leaseID),
		attribute.String("jail.root", jailRoot),
		attribute.String("device.path", devPath),
	)
	if err := o.ops.SetupJail(jailCtx, jailRoot, devPath, o.cfg.KernelPath, o.cfg.JailerUID, o.cfg.JailerGID); err != nil {
		endJailSpan(err)
		return nil, fmt.Errorf("setup jail: %w", err)
	}
	endJailSpan(nil)

	leaseJailDir := filepath.Dir(jailRoot)
	runtime.cleanups = append(runtime.cleanups, func() {
		// Never remove the shared jailer base; concurrent failed boots can otherwise erase live lease chroots.
		if filepath.Base(leaseJailDir) == leaseID {
			_ = os.RemoveAll(leaseJailDir)
		}
	})

	netCfg := NetworkPoolConfig{
		PoolCIDR:      o.cfg.GuestPoolCIDR,
		StateDBPath:   o.cfg.StateDBPath,
		HostInterface: o.cfg.HostInterface,
		TapOwnerUID:   o.cfg.JailerUID,
		TapOwnerGID:   o.cfg.JailerGID,
	}
	netCtx, endNetworkSpan := startStepSpan(ctx, "vmorchestrator.network.setup",
		attribute.String("lease.id", leaseID),
		attribute.String("network.pool_cidr", netCfg.PoolCIDR),
	)
	netSetup, netCleanup, err := setupNetwork(netCtx, leaseID, netCfg, o.ops)
	endNetworkSpan(err)
	if err != nil {
		return nil, fmt.Errorf("setup network: %w", err)
	}
	runtime.Network = netSetup.Lease
	runtime.cleanups = append(runtime.cleanups, netCleanup)

	apiSockHost := filepath.Join(jailRoot, "run", "firecracker.sock")
	controlSockHost := filepath.Join(jailRoot, "run", "forge-control.sock")
	runtime.metricsPath = filepath.Join(jailRoot, "metrics.json")

	jailerCtx, endJailerSpan := startStepSpan(ctx, "vmorchestrator.jailer.start",
		attribute.String("lease.id", leaseID),
	)
	jailer, err := o.ops.StartJailer(jailerCtx, leaseID, JailerConfig{
		FirecrackerBin: o.cfg.FirecrackerBin,
		JailerBin:      o.cfg.JailerBin,
		ChrootBaseDir:  o.cfg.JailerRoot,
		UID:            o.cfg.JailerUID,
		GID:            o.cfg.JailerGID,
	})
	endJailerSpan(err)
	if err != nil {
		return nil, fmt.Errorf("start jailer: %w", err)
	}
	runtime.jailer = jailer
	// Surface the Firecracker PID on the lease.boot span so traces are joinable
	// to host cgroup/process-level metrics without another query.
	trace.SpanFromContext(ctx).SetAttributes(attribute.Int("firecracker.pid", jailer.Pid))
	if err := NewAllocator(netCfg).AttachPID(ctx, leaseID, jailer.Pid); err != nil {
		return nil, fmt.Errorf("record network lease pid: %w", err)
	}
	runtime.cleanups = append(runtime.cleanups, func() {
		if !runtime.jailerExited.Load() {
			_ = jailer.Kill()
			_ = jailer.Wait()
		}
	})
	if jailer.Stdout != nil {
		runtime.logWg.Add(1)
		go captureSerialOutput(jailer.Stdout, &runtime.serialBuf, &runtime.logWg)
	}
	if jailer.Stderr != nil {
		runtime.logWg.Add(1)
		go captureSerialOutput(jailer.Stderr, &runtime.serialBuf, &runtime.logWg)
	}
	runtime.waitDone = make(chan error, 1)
	go func() { runtime.waitDone <- jailer.Wait() }()

	apiSocketCtx, endAPISocketSpan := startStepSpan(ctx, "vmorchestrator.firecracker.api_socket_wait",
		attribute.String("lease.id", leaseID),
		attribute.String("socket.path", apiSockHost),
	)
	if err := waitForSocket(apiSocketCtx, apiSockHost); err != nil {
		endAPISocketSpan(err)
		return nil, fmt.Errorf("wait for API socket: %w", err)
	}
	endAPISocketSpan(nil)

	client := newAPIClient(apiSockHost)
	// Kernel cmdline rendered from the canonical apiwire flag list plus any
	// lease-specific overrides. See src/apiwire/vmresources.go for why each
	// flag is on the base list (or deliberately off).
	bootArgs := apiwire.RenderCmdline(apiwire.DefaultKernelCmdlineBase)
	apiSteps := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"metrics", func(stepCtx context.Context) error { return client.putMetrics(stepCtx, "/metrics.json") }},
		{"boot-source", func(stepCtx context.Context) error { return client.putBootSource(stepCtx, "/vmlinux", bootArgs) }},
		{"rootfs", func(stepCtx context.Context) error { return client.putDrive(stepCtx, "rootfs", "/rootfs", true) }},
		{"machine-config", func(stepCtx context.Context) error {
			return client.putMachineConfig(stepCtx, int(spec.Resources.VCPUs), int(spec.Resources.MemoryMiB))
		}},
		{"network", func(stepCtx context.Context) error {
			return client.putNetworkInterface(stepCtx, "eth0", netSetup.Lease.TapName, netSetup.Lease.MAC)
		}},
		{"vsock", func(stepCtx context.Context) error {
			cid := uint32(netSetup.Lease.SlotIndex) + 3
			return client.putVsock(stepCtx, cid, "/run/forge-control.sock")
		}},
		{"entropy", func(stepCtx context.Context) error { return client.putEntropy(stepCtx) }},
	}
	// Roll up the seven FC API PUTs under a single parent span so dashboards
	// can chart "total configure time" without summing across step children.
	configureCtx, endConfigureAll := startStepSpan(ctx, "vmorchestrator.firecracker.configure_all",
		attribute.String("lease.id", leaseID),
		attribute.Int("firecracker.step_count", len(apiSteps)),
		attribute.Int("vmresources.vcpus", int(spec.Resources.VCPUs)),
		attribute.Int("vmresources.memory_mib", int(spec.Resources.MemoryMiB)),
		attribute.Int("vmresources.root_disk_gib", int(spec.Resources.RootDiskGiB)),
	)
	for _, step := range apiSteps {
		stepCtx, cancel := context.WithTimeout(configureCtx, firecrackerStepTimeout)
		stepCtx, endStepSpan := startStepSpan(stepCtx, "vmorchestrator.firecracker.configure",
			attribute.String("lease.id", leaseID),
			attribute.String("firecracker.step", step.name),
		)
		stepErr := step.fn(stepCtx)
		endStepSpan(stepErr)
		cancel()
		if stepErr != nil {
			endConfigureAll(stepErr)
			return nil, fmt.Errorf("configure VM %s: %w", step.name, stepErr)
		}
	}
	endConfigureAll(nil)

	startCtx, cancel := context.WithTimeout(ctx, firecrackerStepTimeout)
	startCtx, endStartSpan := startStepSpan(startCtx, "vmorchestrator.firecracker.instance_start",
		attribute.String("lease.id", leaseID),
	)
	startErr := client.startInstance(startCtx)
	endStartSpan(startErr)
	cancel()
	if startErr != nil {
		return nil, fmt.Errorf("start VM: %w", startErr)
	}

	controlSocketCtx, endControlSocketSpan := startStepSpan(ctx, "vmorchestrator.guest.control_socket_wait",
		attribute.String("lease.id", leaseID),
		attribute.String("socket.path", controlSockHost),
	)
	if err := waitForPath(controlSocketCtx, controlSockHost); err != nil {
		endControlSocketSpan(err)
		return nil, fmt.Errorf("wait for guest control socket: %w", err)
	}
	endControlSocketSpan(nil)
	if err := o.ops.Chmod(ctx, controlSockHost, 0o770); err != nil {
		return nil, fmt.Errorf("chmod guest control socket: %w", err)
	}

	telemetryCtx, telemetryCancel := context.WithCancel(ctx)
	runtime.cancelTelemetry = telemetryCancel
	runtime.telemetryDone = make(chan struct{})
	go func() {
		defer close(runtime.telemetryDone)
		if err := streamGuestTelemetry(telemetryCtx, controlSockHost, leaseID, observer, logger, nil); err != nil && !errors.Is(err, context.Canceled) {
			logger.WarnContext(ctx, "guest telemetry stream ended", "lease_id", leaseID, "error", err)
		}
	}()

	controlCtx, endControlConnectSpan := startStepSpan(ctx, "vmorchestrator.guest.control_connect",
		attribute.String("lease.id", leaseID),
		attribute.String("socket.path", controlSockHost),
	)
	control, err := connectGuestControl(controlCtx, controlSockHost, vmproto.GuestPort, leaseID)
	endControlConnectSpan(err)
	if err != nil {
		return nil, fmt.Errorf("connect guest control: %w", err)
	}
	runtime.control = control
	runtime.cleanups = append(runtime.cleanups, func() { _ = control.close() })

	_, endHelloSpan := startStepSpan(ctx, "vmorchestrator.guest.hello", attribute.String("lease.id", leaseID))
	hello, err := control.awaitHello(ctx)
	helloObservedAt := time.Now()
	if err != nil {
		endHelloSpan(err)
		return nil, err
	}
	endHelloSpan(nil)
	recordGuestBootTimingSpans(ctx, leaseID, hello, helloObservedAt)
	if err := control.initLease(ctx, leaseID, netSetup.Lease.GuestNetworkConfig(o.cfg.HostServiceIP, o.cfg.HostServicePort)); err != nil {
		return nil, err
	}

	cleanupOnErr = false
	return runtime, nil
}

func (r *LeaseRuntime) Exec(ctx context.Context, spec ExecSpec, handleCheckpoint checkpointHandler) (ExecResult, error) {
	if r == nil || r.control == nil {
		return ExecResult{}, fmt.Errorf("lease runtime is not ready")
	}
	if err := validateExecSpec(spec); err != nil {
		return ExecResult{}, err
	}
	result, err := r.control.exec(ctx, r.LeaseID, spec, handleCheckpoint, r.logger)
	result.Metrics = parseMetricsFile(r.metricsPath)
	if written, writtenErr := zfsWritten(context.Background(), r.Dataset); writtenErr == nil {
		result.ZFSWritten = written
	}
	if provisioned, volsizeErr := zfsVolsize(context.Background(), r.Dataset); volsizeErr == nil {
		result.RootfsProvisionedBytes = provisioned
	}
	return result, err
}

func (r *LeaseRuntime) CancelExec(execID, reason string) error {
	if r == nil || r.control == nil {
		return nil
	}
	return r.control.cancelExec(execID, reason)
}

func (r *LeaseRuntime) Cleanup(reason string) {
	if r == nil {
		return
	}
	if r.control != nil {
		_ = r.control.shutdown()
	}
	if r.cancelTelemetry != nil {
		r.cancelTelemetry()
	}
	if r.waitDone != nil {
		select {
		case <-r.waitDone:
			r.jailerExited.Store(true)
		case <-time.After(2 * time.Second):
			if r.jailer != nil {
				_ = r.jailer.Kill()
			}
			<-r.waitDone
			r.jailerExited.Store(true)
		}
	}
	if r.telemetryDone != nil {
		<-r.telemetryDone
	}
	r.logWg.Wait()
	for i := len(r.cleanups) - 1; i >= 0; i-- {
		r.cleanups[i]()
	}
	if r.logger != nil {
		r.logger.Info("lease runtime cleaned up", "lease_id", r.LeaseID, "reason", reason)
	}
}

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

func waitForPath(ctx context.Context, path string) error {
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat %s: %w", path, err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("path %s not present: %w", path, ctx.Err())
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
		if dst.Len() < maxBufferedGuestLogs {
			dst.WriteString(line)
			dst.WriteByte('\n')
		}
	}
}
