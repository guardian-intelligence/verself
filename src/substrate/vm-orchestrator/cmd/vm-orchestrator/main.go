package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	verselfotel "github.com/verself/observability/otel"
	vmorchestrator "github.com/verself/vm-orchestrator"
	vmrpc "github.com/verself/vm-orchestrator/proto/v1"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"google.golang.org/grpc"
)

const (
	maxMessageSize              = 32 << 20
	defaultShutdownDrainTimeout = 70 * time.Minute
	shutdownDrainPollInterval   = 5 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg := vmorchestrator.DefaultConfig()

	var (
		listenUnix           string
		socketGroup          string
		shutdownDrainTimeout time.Duration
	)

	flag.StringVar(&listenUnix, "listen-unix", vmorchestrator.DefaultSocketPath, "Unix socket path for the vm-orchestrator API")
	flag.StringVar(&socketGroup, "socket-group", "vm-clients", "Group that should own the Unix API socket")
	flag.DurationVar(&shutdownDrainTimeout, "shutdown-drain-timeout", defaultShutdownDrainTimeout, "Maximum time to keep serving existing leases after SIGTERM before shutdown")
	flag.StringVar(&cfg.Pool, "pool", cfg.Pool, "ZFS pool used for VM datasets")
	flag.StringVar(&cfg.ImageDataset, "image-dataset", cfg.ImageDataset, "ZFS dataset under the pool containing composable image zvol snapshots")
	flag.StringVar(&cfg.GoldenDataset, "golden-dataset", cfg.GoldenDataset, "ZFS dataset under the pool containing immutable durable zvol generations")
	flag.StringVar(&cfg.WorkloadDataset, "workload-dataset", cfg.WorkloadDataset, "ZFS dataset for ephemeral VM leases")
	flag.StringVar(&cfg.StorageKeyDir, "storage-key-dir", cfg.StorageKeyDir, "Root-only directory containing raw per-org ZFS storage keys")
	flag.StringVar(&cfg.DefaultSubstrateRef, "default-substrate-ref", cfg.DefaultSubstrateRef, "Composable image ref cloned as the substrate root disk for new leases (toolchain images compose on top via FilesystemMounts)")
	flag.StringVar(&cfg.KernelPath, "kernel-path", cfg.KernelPath, "Path to vmlinux on the host")
	flag.StringVar(&cfg.FirecrackerBin, "firecracker-bin", cfg.FirecrackerBin, "Path to firecracker binary")
	flag.StringVar(&cfg.JailerBin, "jailer-bin", cfg.JailerBin, "Path to jailer binary")
	flag.StringVar(&cfg.JailerRoot, "jailer-root", cfg.JailerRoot, "Jailer chroot root directory")
	flag.StringVar(&cfg.SnapshotCacheDir, "snapshot-cache-dir", cfg.SnapshotCacheDir, "Directory for reusable Firecracker VM snapshot artifacts")
	flag.BoolVar(&cfg.FirecrackerSnapshotsEnabled, "firecracker-snapshots", cfg.FirecrackerSnapshotsEnabled, "Enable Firecracker VM snapshot restore and creation")
	flag.IntVar(&cfg.JailerUID, "jailer-uid", cfg.JailerUID, "UID used for the jailer process")
	flag.IntVar(&cfg.JailerGID, "jailer-gid", cfg.JailerGID, "GID used for the jailer process")
	// Per-VM shape is now a request-time LeaseSpec parameter;
	// flag-level --vcpus / --memory-mib have been removed. Operators tune
	// per-org ceilings via the VMResourceBounds table in sandbox-rental-service.
	flag.StringVar(&cfg.HostInterface, "host-interface", cfg.HostInterface, "Default uplink interface for guest egress")
	flag.StringVar(&cfg.GuestPoolCIDR, "guest-pool-cidr", cfg.GuestPoolCIDR, "IPv4 pool reserved for Firecracker guests")
	flag.StringVar(&cfg.StateDBPath, "state-db-path", cfg.StateDBPath, "Path to durable host runtime SQLite WAL ledger")
	flag.StringVar(&cfg.HostServiceIP, "host-service-ip", cfg.HostServiceIP, "Host-only service IP exposed to Firecracker guests")
	flag.IntVar(&cfg.HostServicePort, "host-service-port", cfg.HostServicePort, "Host-only HTTP reverse proxy port exposed to Firecracker guests")
	flag.StringVar(&cfg.TelemetryFaultProfile, "telemetry-fault-profile", os.Getenv(vmorchestrator.TelemetryFaultProfileEnvVar), "Verification-only host-side telemetry fault profile: empty, gap_once@N, or regression_once@N")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(filepath.Dir(listenUnix), 0o755); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	if err := os.RemoveAll(listenUnix); err != nil {
		return fmt.Errorf("remove stale socket %s: %w", listenUnix, err)
	}

	otelShutdown, logger, err := verselfotel.Init(ctx, verselfotel.Config{
		ServiceName:    "vm-orchestrator",
		ServiceVersion: "0.2.0",
	})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() { _ = otelShutdown(context.Background()) }()
	slog.SetDefault(logger)

	listener, err := net.Listen("unix", listenUnix)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", listenUnix, err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(listenUnix)
	}()

	if err := setSocketOwnership(listenUnix, socketGroup); err != nil {
		return err
	}
	if err := os.Chmod(listenUnix, 0o660); err != nil {
		return fmt.Errorf("chmod socket %s: %w", listenUnix, err)
	}

	server := grpc.NewServer(
		grpc.MaxRecvMsgSize(maxMessageSize),
		grpc.MaxSendMsgSize(maxMessageSize),
		grpc.Creds(vmorchestrator.NewPeerCredsTransportCredentials()),
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
	vmService, err := vmorchestrator.NewAPIServer(cfg, logger)
	if err != nil {
		return err
	}
	defer func() { _ = vmService.Close() }()
	vmrpc.RegisterVMServiceServer(server, vmService)

	startupCtx, startupSpan := otel.Tracer("vm-orchestrator").Start(ctx, "daemon.startup")
	slog.InfoContext(startupCtx, "vm-orchestrator listening", "socket", listenUnix, "socket_group", socketGroup)
	startupSpan.End()

	errCh := make(chan error, 1)
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && ctx.Err() == nil {
			errCh <- fmt.Errorf("serve vm-orchestrator: %w", serveErr)
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, shutdownSpan := otel.Tracer("vm-orchestrator").Start(context.Background(), "daemon.shutdown")
		activeLeases, drainErr := vmService.StartDrain(shutdownCtx)
		if drainErr != nil {
			shutdownSpan.RecordError(drainErr)
			slog.WarnContext(shutdownCtx, "vm-orchestrator drain state unavailable", "error", drainErr)
		}
		slog.InfoContext(shutdownCtx, "vm-orchestrator draining", "active_leases", activeLeases, "timeout", shutdownDrainTimeout.String())
		shutdownSpan.End()

		if err := waitForLeaseDrain(context.Background(), vmService, shutdownDrainTimeout, shutdownDrainPollInterval); err != nil {
			slog.WarnContext(context.Background(), "vm-orchestrator drain timeout", "error", err)
		}

		drainDone := make(chan struct{})
		go func() {
			server.GracefulStop()
			close(drainDone)
		}()

		select {
		case <-drainDone:
		case <-time.After(5 * time.Second):
			server.Stop()
		}
		return nil
	case err := <-errCh:
		return err
	}
}

func waitForLeaseDrain(ctx context.Context, server *vmorchestrator.APIServer, timeout, interval time.Duration) error {
	if timeout <= 0 {
		timeout = defaultShutdownDrainTimeout
	}
	if interval <= 0 {
		interval = shutdownDrainPollInterval
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		active, err := server.ActiveLeaseCount(waitCtx)
		if err != nil {
			return err
		}
		if active == 0 {
			slog.InfoContext(waitCtx, "vm-orchestrator lease drain complete")
			return nil
		}
		slog.InfoContext(waitCtx, "vm-orchestrator waiting for active leases", "active_leases", active)
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("active leases did not drain before shutdown deadline: %w", waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func setSocketOwnership(path, groupName string) error {
	group, err := user.LookupGroup(groupName)
	if err != nil {
		return fmt.Errorf("lookup socket group %s: %w", groupName, err)
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return fmt.Errorf("parse gid for group %s: %w", groupName, err)
	}
	if err := os.Chown(path, -1, gid); err != nil {
		return fmt.Errorf("chown socket %s to group %s: %w", path, groupName, err)
	}
	return nil
}
