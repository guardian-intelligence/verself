// Command vm-orchestrator-cli is the privileged operator surface for the
// vm-orchestrator daemon. It speaks gRPC over the daemon's Unix socket and
// invokes RPCs that the daemon gates behind SO_PEERCRED uid=0. Nomad uses
// `seed-catalog` after daemon startup to materialize composable image zvols.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	verselfotel "github.com/verself/observability/otel"
	vmorchestrator "github.com/verself/vm-orchestrator"
	vmrpc "github.com/verself/vm-orchestrator/proto/v1"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	grpcCodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	grpcStatus "google.golang.org/grpc/status"
)

func main() {
	if len(os.Args) < 2 {
		printRootUsage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "seed-image":
		os.Exit(runSeedImage(os.Args[2:]))
	case "seed-catalog":
		os.Exit(runSeedCatalog(os.Args[2:]))
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
	fmt.Fprintln(os.Stderr, "  seed-image     materialize one composable image zvol via the daemon")
	fmt.Fprintln(os.Stderr, "  seed-catalog   materialize a JSON catalog of composable image zvols in order")
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

type seedCatalogFlags struct {
	socket       string
	catalog      string
	allowDestroy bool
	timeout      time.Duration
}

type seedCatalog struct {
	Images []seedCatalogImage `json:"images"`
}

type seedCatalogImage struct {
	Ref             string `json:"ref"`
	Strategy        string `json:"strategy"`
	SourcePath      string `json:"source_path"`
	SizeBytes       uint64 `json:"size_bytes"`
	Volblocksize    string `json:"volblocksize"`
	FilesystemLabel string `json:"filesystem_label"`
}

func runSeedImage(args []string) int {
	fs := flag.NewFlagSet("seed-image", flag.ExitOnError)
	cfg := seedImageFlags{}
	fs.StringVar(&cfg.socket, "socket", vmorchestrator.DefaultSocketPath, "Unix socket path of the vm-orchestrator daemon")
	fs.StringVar(&cfg.ref, "ref", "", "image ref to materialize (e.g. substrate, gh-actions-runner)")
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
	defer func() { _ = otelShutdown(context.Background()) }()

	tracer := otel.Tracer("vm-orchestrator-cli")
	ctx, span := tracer.Start(rootCtx, "vmorchestrator.cli.seed_image", trace.WithAttributes(
		attribute.String("image.ref", cfg.ref),
		attribute.String("seed.strategy", cfg.strategy),
		attribute.Int64("seed.size_bytes", int64FromUint64(cfg.sizeBytes, "seed size bytes")),
		attribute.String("source.path", cfg.sourcePath),
	))
	defer span.End()

	conn, err := dial(ctx, cfg.socket)
	if err != nil {
		failSpan(span, err)
		fmt.Fprintf(os.Stderr, "dial %s: %v\n", cfg.socket, err)
		return 1
	}
	defer func() { _ = conn.Close() }()

	rpcCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()
	client := vmrpc.NewVMServiceClient(conn)
	resp, rpcErr := seedImage(rpcCtx, client, cfg)
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
		attribute.Int64("seed.seeded_bytes", int64FromUint64(resp.GetSeededBytes(), "seeded bytes")),
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

func runSeedCatalog(args []string) int {
	fs := flag.NewFlagSet("seed-catalog", flag.ExitOnError)
	cfg := seedCatalogFlags{}
	fs.StringVar(&cfg.socket, "socket", vmorchestrator.DefaultSocketPath, "Unix socket path of the vm-orchestrator daemon")
	fs.StringVar(&cfg.catalog, "catalog", "", "JSON catalog containing images to seed in order")
	fs.BoolVar(&cfg.allowDestroy, "allow-destroying-active-clones", false, "destroy any workload clones derived from previous images")
	fs.DurationVar(&cfg.timeout, "timeout", 30*time.Minute, "client-side RPC deadline for the full catalog")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := cfg.validate(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		fs.Usage()
		return 2
	}
	catalog, err := loadSeedCatalog(cfg.catalog)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
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
	defer func() { _ = otelShutdown(context.Background()) }()

	tracer := otel.Tracer("vm-orchestrator-cli")
	ctx, span := tracer.Start(rootCtx, "vmorchestrator.cli.seed_catalog", trace.WithAttributes(
		attribute.String("seed.catalog_path", cfg.catalog),
		attribute.Int("seed.image_count", len(catalog.Images)),
	))
	defer span.End()

	conn, err := dial(ctx, cfg.socket)
	if err != nil {
		failSpan(span, err)
		fmt.Fprintf(os.Stderr, "dial %s: %v\n", cfg.socket, err)
		return 1
	}
	defer func() { _ = conn.Close() }()

	rpcCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()
	client := vmrpc.NewVMServiceClient(conn)
	for _, image := range catalog.Images {
		imageCfg := image.flags(cfg)
		resp, rpcErr := seedImage(rpcCtx, client, imageCfg)
		if rpcErr != nil {
			failSpan(span, rpcErr)
			fmt.Fprintf(os.Stderr, "SeedImage %s failed: %v\n", imageCfg.ref, rpcErr)
			return 1
		}
		fmt.Printf("seed: ref=%s outcome=%s dataset=%s snapshot=%s digest=%s seeded_bytes=%d dependents_torn=%d\n",
			resp.GetImageRef(),
			resp.GetOutcome().String(),
			resp.GetDataset(),
			resp.GetSnapshot(),
			resp.GetSourceDigest(),
			resp.GetSeededBytes(),
			resp.GetDependentsTorn(),
		)
	}
	return 0
}

func dial(ctx context.Context, socket string) (*grpc.ClientConn, error) {
	return grpc.NewClient("unix:"+socket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, target string) (net.Conn, error) {
			d := net.Dialer{}
			return d.DialContext(ctx, "unix", strings.TrimPrefix(target, "unix:"))
		}),
	)
}

func seedImage(ctx context.Context, client vmrpc.VMServiceClient, cfg seedImageFlags) (*vmrpc.SeedImageResponse, error) {
	for {
		resp, err := client.SeedImage(ctx, cfg.request())
		if err == nil {
			return resp, nil
		}
		if grpcStatus.Code(err) != grpcCodes.Unavailable {
			return nil, err
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
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

func (c seedCatalogFlags) validate() error {
	if strings.TrimSpace(c.catalog) == "" {
		return fmt.Errorf("--catalog is required")
	}
	return nil
}

func loadSeedCatalog(path string) (seedCatalog, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return seedCatalog{}, fmt.Errorf("read seed catalog %s: %w", path, err)
	}
	var catalog seedCatalog
	if err := json.Unmarshal(body, &catalog); err != nil {
		return seedCatalog{}, fmt.Errorf("decode seed catalog %s: %w", path, err)
	}
	if len(catalog.Images) == 0 {
		return seedCatalog{}, fmt.Errorf("seed catalog %s has no images", path)
	}
	for i, image := range catalog.Images {
		if err := image.flags(seedCatalogFlags{}).validate(); err != nil {
			return seedCatalog{}, fmt.Errorf("seed catalog %s image %d: %w", path, i, err)
		}
	}
	return catalog, nil
}

func (c seedCatalogImage) flags(parent seedCatalogFlags) seedImageFlags {
	return seedImageFlags{
		socket:          parent.socket,
		ref:             c.Ref,
		strategy:        c.Strategy,
		sourcePath:      c.SourcePath,
		sizeBytes:       c.SizeBytes,
		volblocksize:    c.Volblocksize,
		filesystemLabel: c.FilesystemLabel,
		allowDestroy:    parent.allowDestroy,
		timeout:         parent.timeout,
	}
}

func (c seedImageFlags) request() *vmrpc.SeedImageRequest {
	return &vmrpc.SeedImageRequest{
		ImageRef:                    c.ref,
		Strategy:                    c.protoStrategy(),
		SizeBytes:                   c.sizeBytes,
		Volblocksize:                c.volblocksize,
		SourcePath:                  c.sourcePath,
		FilesystemLabel:             c.filesystemLabel,
		AllowDestroyingActiveClones: c.allowDestroy,
	}
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
