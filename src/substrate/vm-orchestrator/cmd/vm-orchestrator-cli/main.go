// Command vm-orchestrator-cli is the privileged operator surface for the
// vm-orchestrator daemon. It speaks gRPC over the daemon's Unix socket and
// invokes RPCs that the daemon gates behind SO_PEERCRED uid=0. Nomad uses
// `seed-catalog` after daemon startup to materialize composable image zvols.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
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
	case "stage-guest-images":
		os.Exit(runStageGuestImages(os.Args[2:]))
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
	fmt.Fprintln(os.Stderr, "  stage-guest-images   stage substrate and toolchain image files from deploy artifacts")
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

type stageGuestImagesFlags struct {
	substrateInputs string
	toolchainImages string
	guestImagesDir  string
	workDir         string
}

var (
	substrateOutputFiles = []string{
		"substrate.ext4",
		"vmlinux",
		"vmlinux.config",
		"sbom.txt",
		"manifest.json",
	}
	toolchainOutputFiles = []string{
		"toolchains/gh-actions-runner.ext4",
		"toolchains/gh-actions-runner.manifest.json",
		"toolchains/forgejo-runner.ext4",
		"toolchains/forgejo-runner.manifest.json",
	}
)

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

func runStageGuestImages(args []string) int {
	fs := flag.NewFlagSet("stage-guest-images", flag.ExitOnError)
	cfg := stageGuestImagesFlags{}
	fs.StringVar(&cfg.substrateInputs, "substrate-inputs", "", "Directory containing extracted substrate input bundle")
	fs.StringVar(&cfg.toolchainImages, "toolchain-images", "", "Directory containing extracted toolchain image bundle")
	fs.StringVar(&cfg.guestImagesDir, "guest-images-dir", "/var/lib/verself/guest-images", "Host guest-image staging directory")
	fs.StringVar(&cfg.workDir, "work-dir", "/tmp/verself-substrate-build", "Scratch directory for substrate rebuilds")
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
	ctx, span := tracer.Start(rootCtx, "vmorchestrator.cli.stage_guest_images", trace.WithAttributes(
		attribute.String("guest_images.dir", cfg.guestImagesDir),
		attribute.String("substrate.inputs_dir", cfg.substrateInputs),
		attribute.String("toolchain.inputs_dir", cfg.toolchainImages),
	))
	defer span.End()

	result, err := stageGuestImages(ctx, cfg)
	if err != nil {
		failSpan(span, err)
		fmt.Fprintf(os.Stderr, "stage guest images: %v\n", err)
		return 1
	}
	span.SetAttributes(
		attribute.Bool("substrate.rebuilt", result.SubstrateRebuilt),
		attribute.Bool("toolchain.staged", result.ToolchainStaged),
		attribute.String("substrate.digest", result.SubstrateDigest),
		attribute.String("toolchain.digest", result.ToolchainDigest),
	)
	fmt.Printf("stage-guest-images: substrate_rebuilt=%t substrate_digest=%s toolchain_staged=%t toolchain_digest=%s\n",
		result.SubstrateRebuilt,
		result.SubstrateDigest,
		result.ToolchainStaged,
		result.ToolchainDigest,
	)
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

func (c stageGuestImagesFlags) validate() error {
	if strings.TrimSpace(c.substrateInputs) == "" {
		return fmt.Errorf("--substrate-inputs is required")
	}
	if strings.TrimSpace(c.toolchainImages) == "" {
		return fmt.Errorf("--toolchain-images is required")
	}
	if strings.TrimSpace(c.guestImagesDir) == "" {
		return fmt.Errorf("--guest-images-dir is required")
	}
	if strings.TrimSpace(c.workDir) == "" {
		return fmt.Errorf("--work-dir is required")
	}
	return nil
}

type stageGuestImagesResult struct {
	SubstrateRebuilt bool
	SubstrateDigest  string
	ToolchainStaged  bool
	ToolchainDigest  string
}

func stageGuestImages(ctx context.Context, cfg stageGuestImagesFlags) (stageGuestImagesResult, error) {
	if err := os.MkdirAll(cfg.guestImagesDir, 0o755); err != nil {
		return stageGuestImagesResult{}, fmt.Errorf("create guest images dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.guestImagesDir, "toolchains"), 0o755); err != nil {
		return stageGuestImagesResult{}, fmt.Errorf("create toolchain dir: %w", err)
	}

	substrateDigest, err := digestDirectory(cfg.substrateInputs)
	if err != nil {
		return stageGuestImagesResult{}, fmt.Errorf("digest substrate inputs: %w", err)
	}
	toolchainDigest, err := digestDirectory(cfg.toolchainImages)
	if err != nil {
		return stageGuestImagesResult{}, fmt.Errorf("digest toolchain images: %w", err)
	}

	result := stageGuestImagesResult{
		SubstrateDigest: substrateDigest,
		ToolchainDigest: toolchainDigest,
	}
	current, err := stagedArtifactsCurrent(cfg.guestImagesDir, ".substrate-inputs.sha256", substrateDigest, substrateOutputFiles)
	if err != nil {
		return result, err
	}
	if !current {
		if err := rebuildSubstrate(ctx, cfg, substrateDigest); err != nil {
			return result, err
		}
		result.SubstrateRebuilt = true
	}
	current, err = stagedArtifactsCurrent(cfg.guestImagesDir, filepath.Join("toolchains", ".toolchain-images.sha256"), toolchainDigest, toolchainOutputFiles)
	if err != nil {
		return result, err
	}
	if !current {
		if err := stageToolchainImages(cfg, toolchainDigest); err != nil {
			return result, err
		}
		result.ToolchainStaged = true
	}
	return result, nil
}

func rebuildSubstrate(ctx context.Context, cfg stageGuestImagesFlags, digest string) error {
	builder := filepath.Join(cfg.substrateInputs, "build-substrate")
	if err := requireRegularFile(builder); err != nil {
		return fmt.Errorf("substrate builder: %w", err)
	}
	if err := os.Chmod(builder, 0o755); err != nil {
		return fmt.Errorf("chmod substrate builder: %w", err)
	}
	outputDir := filepath.Join(cfg.workDir, "output")
	if err := os.RemoveAll(cfg.workDir); err != nil {
		return fmt.Errorf("reset substrate work dir: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create substrate output dir: %w", err)
	}

	cmd := exec.CommandContext(ctx, builder)
	cmd.Env = append(os.Environ(),
		"SUBSTRATE_INPUTS="+cfg.substrateInputs,
		"SUBSTRATE_OUTPUT_DIR="+outputDir,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run build-substrate: %w", err)
	}
	if err := stageFilesAtomically(outputDir, cfg.guestImagesDir, substrateOutputFiles); err != nil {
		return fmt.Errorf("stage substrate outputs: %w", err)
	}
	if err := writeDigestFile(filepath.Join(cfg.guestImagesDir, ".substrate-inputs.sha256"), digest); err != nil {
		return err
	}
	return nil
}

func stageToolchainImages(cfg stageGuestImagesFlags, digest string) error {
	if err := stageFilesAtomically(cfg.toolchainImages, cfg.guestImagesDir, toolchainOutputFiles); err != nil {
		return fmt.Errorf("stage toolchain images: %w", err)
	}
	if err := writeDigestFile(filepath.Join(cfg.guestImagesDir, "toolchains", ".toolchain-images.sha256"), digest); err != nil {
		return err
	}
	return nil
}

func stagedArtifactsCurrent(root, digestRel, digest string, relPaths []string) (bool, error) {
	currentDigest, err := os.ReadFile(filepath.Join(root, digestRel))
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("read %s: %w", digestRel, err)
	}
	if strings.TrimSpace(string(currentDigest)) != digest {
		return false, nil
	}
	for _, rel := range relPaths {
		if err := requireRegularFile(filepath.Join(root, rel)); err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, fmt.Errorf("inspect staged %s: %w", rel, err)
		}
	}
	return true, nil
}

func stageFilesAtomically(srcRoot, dstRoot string, relPaths []string) error {
	stageDir, err := os.MkdirTemp(dstRoot, ".stage-guest-images-*")
	if err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}
	removeStage := true
	defer func() {
		if removeStage {
			_ = os.RemoveAll(stageDir)
		}
	}()
	for _, rel := range relPaths {
		src := filepath.Join(srcRoot, rel)
		if err := requireRegularFile(src); err != nil {
			return fmt.Errorf("source %s: %w", rel, err)
		}
		if err := copyFile(src, filepath.Join(stageDir, rel), 0o644); err != nil {
			return err
		}
	}
	for _, rel := range relPaths {
		dst := filepath.Join(dstRoot, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("create parent for %s: %w", dst, err)
		}
		if err := os.Rename(filepath.Join(stageDir, rel), dst); err != nil {
			return fmt.Errorf("rename staged %s: %w", rel, err)
		}
	}
	return nil
}

func writeDigestFile(path, digest string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create digest temp file: %w", err)
	}
	tmpPath := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := io.WriteString(tmp, digest+"\n"); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write digest temp file: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod digest temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close digest temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename digest file: %w", err)
	}
	removeTmp = false
	return nil
}

func requireRegularFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	return nil
}

func copyFile(src, dst string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create parent for %s: %w", dst, err)
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dst, err)
	}
	if err := os.Chmod(dst, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", dst, err)
	}
	return nil
}

func digestDirectory(root string) (string, error) {
	var paths []string
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	}); err != nil {
		return "", fmt.Errorf("walk %s: %w", root, err)
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("%s contains no files", root)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, rel := range paths {
		path := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "path %s\nmode %s\nsize %d\n", rel, info.Mode().String(), info.Size())
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(h, "symlink %s\n", target)
			continue
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("%s is not a regular file", path)
		}
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(h, f); err != nil {
			_ = f.Close()
			return "", err
		}
		if err := f.Close(); err != nil {
			return "", err
		}
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
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
