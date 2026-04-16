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

	fmotel "github.com/forge-metal/otel"
	vmorchestrator "github.com/forge-metal/vm-orchestrator"
	vmrpc "github.com/forge-metal/vm-orchestrator/proto/v1"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"google.golang.org/grpc"
)

const maxMessageSize = 32 << 20

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg := vmorchestrator.DefaultConfig()

	var (
		listenUnix  string
		socketGroup string
	)

	flag.StringVar(&listenUnix, "listen-unix", vmorchestrator.DefaultSocketPath, "Unix socket path for the vm-orchestrator API")
	flag.StringVar(&socketGroup, "socket-group", "vm-clients", "Group that should own the Unix API socket")
	flag.StringVar(&cfg.Pool, "pool", cfg.Pool, "ZFS pool used for VM datasets")
	flag.StringVar(&cfg.GoldenZvol, "golden-zvol", cfg.GoldenZvol, "Base guest golden zvol name")
	flag.StringVar(&cfg.ImageDataset, "image-dataset", cfg.ImageDataset, "ZFS dataset under the pool containing composable image zvol snapshots")
	flag.StringVar(&cfg.WorkloadDataset, "workload-dataset", cfg.WorkloadDataset, "ZFS dataset for ephemeral VM leases")
	flag.StringVar(&cfg.KernelPath, "kernel-path", cfg.KernelPath, "Path to vmlinux on the host")
	flag.StringVar(&cfg.FirecrackerBin, "firecracker-bin", cfg.FirecrackerBin, "Path to firecracker binary")
	flag.StringVar(&cfg.JailerBin, "jailer-bin", cfg.JailerBin, "Path to jailer binary")
	flag.StringVar(&cfg.JailerRoot, "jailer-root", cfg.JailerRoot, "Jailer chroot root directory")
	flag.IntVar(&cfg.JailerUID, "jailer-uid", cfg.JailerUID, "UID used for the jailer process")
	flag.IntVar(&cfg.JailerGID, "jailer-gid", cfg.JailerGID, "GID used for the jailer process")
	// Per-VM shape is now a request-time parameter via apiwire.VMResources;
	// flag-level --vcpus / --memory-mib have been removed. Operators tune
	// per-org ceilings via the VMResourceBounds table in sandbox-rental-service.
	flag.StringVar(&cfg.HostInterface, "host-interface", cfg.HostInterface, "Default uplink interface for guest egress")
	flag.StringVar(&cfg.GuestPoolCIDR, "guest-pool-cidr", cfg.GuestPoolCIDR, "IPv4 pool reserved for Firecracker guests")
	flag.StringVar(&cfg.StateDBPath, "state-db-path", cfg.StateDBPath, "Path to durable host runtime SQLite WAL ledger")
	flag.StringVar(&cfg.HostServiceIP, "host-service-ip", cfg.HostServiceIP, "Host-only service IP exposed to Firecracker guests")
	flag.IntVar(&cfg.HostServicePort, "host-service-port", cfg.HostServicePort, "Host-only HTTP reverse proxy port exposed to Firecracker guests")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(filepath.Dir(listenUnix), 0o755); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	if err := os.RemoveAll(listenUnix); err != nil {
		return fmt.Errorf("remove stale socket %s: %w", listenUnix, err)
	}

	otelShutdown, logger, err := fmotel.Init(ctx, fmotel.Config{
		ServiceName:    "vm-orchestrator",
		ServiceVersion: "0.2.0",
	})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer otelShutdown(context.Background())
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
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
	vmService, err := vmorchestrator.NewAPIServer(cfg, logger)
	if err != nil {
		return err
	}
	defer vmService.Close()
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
		slog.InfoContext(shutdownCtx, "vm-orchestrator stopping")
		shutdownSpan.End()

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
