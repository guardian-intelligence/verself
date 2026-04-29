// Command vm-orchestrator-cli is the privileged operator surface for the
// vm-orchestrator daemon. It speaks gRPC over the daemon's Unix socket and
// invokes RPCs that the daemon gates behind SO_PEERCRED uid=0. The first
// subcommand is `seed-image`, used by Ansible at deploy time to materialize
// composable image zvols; further deploy-time RPCs land here as needed.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	verselfotel "github.com/verself/otel"
	vmorchestrator "github.com/verself/vm-orchestrator"
	vmrpc "github.com/verself/vm-orchestrator/proto/v1"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	if len(os.Args) < 2 {
		printRootUsage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "seed-image":
		os.Exit(runSeedImage(os.Args[2:]))
	case "-h", "--help", "help":
		printRootUsage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", os.Args[1])
		printRootUsage()
		os.Exit(2)
	}
}

func printRootUsage() {
	fmt.Fprintln(os.Stderr, "usage: vm-orchestrator-cli <subcommand> [flags]")
	fmt.Fprintln(os.Stderr, "subcommands:")
	fmt.Fprintln(os.Stderr, "  seed-image    materialize a composable image zvol via the daemon")
}

type seedImageFlags struct {
	socket          string
	ref             string
	strategy        string
	sourcePath      string
	sizeBytes       uint64
	volblocksize    string
	filesystemLabel string
	allowDestroy    bool
	timeout         time.Duration
}

func runSeedImage(args []string) int {
	fs := flag.NewFlagSet("seed-image", flag.ExitOnError)
	cfg := seedImageFlags{}
	fs.StringVar(&cfg.socket, "socket", vmorchestrator.DefaultSocketPath, "Unix socket path of the vm-orchestrator daemon")
	fs.StringVar(&cfg.ref, "ref", "", "image ref to materialize (e.g. substrate, gh-actions-runner, sticky-empty)")
	fs.StringVar(&cfg.strategy, "strategy", "", "seed strategy: dd_from_file or mkfs_ext4")
	fs.StringVar(&cfg.sourcePath, "source-path", "", "host artifact for dd_from_file")
	fs.Uint64Var(&cfg.sizeBytes, "size-bytes", 0, "zvol size in bytes")
	fs.StringVar(&cfg.volblocksize, "volblocksize", "", "ZFS volblocksize (default 16K)")
	fs.StringVar(&cfg.filesystemLabel, "filesystem-label", "", "filesystem label for mkfs_ext4")
	fs.BoolVar(&cfg.allowDestroy, "allow-destroying-active-clones", false, "destroy any workload clones derived from the previous image")
	fs.DurationVar(&cfg.timeout, "timeout", 30*time.Minute, "client-side RPC deadline")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := cfg.validate(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		fs.Usage()
		return 2
	}
	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	otelShutdown, _, err := verselfotel.Init(rootCtx, verselfotel.Config{
		ServiceName:    "vm-orchestrator-cli",
		ServiceVersion: "0.1.0",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "otel init: %v\n", err)
		return 1
	}
	defer otelShutdown(context.Background())

	tracer := otel.Tracer("vm-orchestrator-cli")
	ctx, span := tracer.Start(rootCtx, "vmorchestrator.cli.seed_image", trace.WithAttributes(
		attribute.String("image.ref", cfg.ref),
		attribute.String("seed.strategy", cfg.strategy),
		attribute.Int64("seed.size_bytes", int64(cfg.sizeBytes)),
		attribute.String("source.path", cfg.sourcePath),
	))
	defer span.End()

	conn, err := grpc.NewClient("unix:"+cfg.socket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, target string) (net.Conn, error) {
			d := net.Dialer{}
			return d.DialContext(ctx, "unix", strings.TrimPrefix(target, "unix:"))
		}),
	)
	if err != nil {
		failSpan(span, err)
		fmt.Fprintf(os.Stderr, "dial %s: %v\n", cfg.socket, err)
		return 1
	}
	defer conn.Close()

	rpcCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()
	client := vmrpc.NewVMServiceClient(conn)
	resp, rpcErr := client.SeedImage(rpcCtx, &vmrpc.SeedImageRequest{
		ImageRef:                    cfg.ref,
		Strategy:                    cfg.protoStrategy(),
		SizeBytes:                   cfg.sizeBytes,
		Volblocksize:                cfg.volblocksize,
		SourcePath:                  cfg.sourcePath,
		FilesystemLabel:             cfg.filesystemLabel,
		AllowDestroyingActiveClones: cfg.allowDestroy,
	})
	if rpcErr != nil {
		failSpan(span, rpcErr)
		fmt.Fprintf(os.Stderr, "SeedImage failed: %v\n", rpcErr)
		return 1
	}
	span.SetAttributes(
		attribute.String("seed.outcome", strings.ToLower(strings.TrimPrefix(resp.GetOutcome().String(), "SEED_OUTCOME_"))),
		attribute.String("seed.dataset", resp.GetDataset()),
		attribute.String("seed.snapshot", resp.GetSnapshot()),
		attribute.String("seed.source_digest", resp.GetSourceDigest()),
		attribute.Int64("seed.seeded_bytes", int64(resp.GetSeededBytes())),
		attribute.Int("seed.dependents_torn", int(resp.GetDependentsTorn())),
	)
	fmt.Printf("seed: ref=%s outcome=%s dataset=%s snapshot=%s digest=%s seeded_bytes=%d dependents_torn=%d\n",
		resp.GetImageRef(),
		resp.GetOutcome().String(),
		resp.GetDataset(),
		resp.GetSnapshot(),
		resp.GetSourceDigest(),
		resp.GetSeededBytes(),
		resp.GetDependentsTorn(),
	)
	return 0
}

func failSpan(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

func (c seedImageFlags) validate() error {
	if strings.TrimSpace(c.ref) == "" {
		return fmt.Errorf("--ref is required")
	}
	switch c.strategy {
	case "dd_from_file":
		if strings.TrimSpace(c.sourcePath) == "" {
			return fmt.Errorf("--source-path is required for dd_from_file")
		}
	case "mkfs_ext4":
		if strings.TrimSpace(c.filesystemLabel) == "" {
			return fmt.Errorf("--filesystem-label is required for mkfs_ext4")
		}
	default:
		return fmt.Errorf("--strategy must be dd_from_file or mkfs_ext4")
	}
	if c.sizeBytes == 0 {
		return fmt.Errorf("--size-bytes is required")
	}
	return nil
}

func (c seedImageFlags) protoStrategy() vmrpc.SeedStrategy {
	switch c.strategy {
	case "dd_from_file":
		return vmrpc.SeedStrategy_SEED_STRATEGY_DD_FROM_FILE
	case "mkfs_ext4":
		return vmrpc.SeedStrategy_SEED_STRATEGY_MKFS_EXT4
	}
	return vmrpc.SeedStrategy_SEED_STRATEGY_UNSPECIFIED
}
