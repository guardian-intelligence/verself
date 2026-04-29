// Command cue-renderer compiles the CUE topology graph at
// src/cue-renderer/ into the artefacts the rest of the platform
// consumes. See src/cue-renderer/README.md for the bigger picture.
//
// Subcommands:
//
//	cue-renderer generate            # write every renderer's output to disk
//	cue-renderer render <name>       # write one renderer to stdout (debug)
//	cue-renderer list                # print the registered artefacts
//
// Tracing wraps the whole pipeline in a single root span so every render
// is one trace; per-artefact events attach as child spans of that root.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.opentelemetry.io/otel/attribute"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/render/bazeldevtools"
	"github.com/verself/cue-renderer/internal/render/bazeldevtoolsmodule"
	"github.com/verself/cue-renderer/internal/render/bazelguestimages"
	"github.com/verself/cue-renderer/internal/render/bazelmodule"
	"github.com/verself/cue-renderer/internal/render/bazelservertools"
	"github.com/verself/cue-renderer/internal/render/catalog"
	"github.com/verself/cue-renderer/internal/render/clusters"
	"github.com/verself/cue-renderer/internal/render/components"
	"github.com/verself/cue-renderer/internal/render/deploy"
	"github.com/verself/cue-renderer/internal/render/dns"
	"github.com/verself/cue-renderer/internal/render/endpoints"
	"github.com/verself/cue-renderer/internal/render/nftables"
	"github.com/verself/cue-renderer/internal/render/ops"
	"github.com/verself/cue-renderer/internal/render/postgres"
	"github.com/verself/cue-renderer/internal/render/routes"
	"github.com/verself/cue-renderer/internal/render/runtime"
	"github.com/verself/cue-renderer/internal/render/spire"
	"github.com/verself/cue-renderer/internal/render/systemd"
	"github.com/verself/cue-renderer/internal/spans"
)

const (
	defaultTopologyDir = "src/cue-renderer"
	defaultInstance    = "prod"
	defaultRepoRoot    = "."
)

// renderers is the complete set of generated artefacts currently owned by the
// Go tool.
func renderers() []render.Renderer {
	return []render.Renderer{
		bazeldevtools.Renderer{},
		bazeldevtoolsmodule.Renderer{},
		bazelguestimages.Renderer{},
		bazelmodule.Renderer{},
		bazelservertools.Renderer{},
		catalog.Renderer{},
		clusters.Renderer{},
		components.Renderer{},
		deploy.Renderer{},
		dns.Renderer{},
		endpoints.Renderer{},
		nftables.Renderer{},
		ops.Renderer{},
		postgres.Renderer{},
		routes.Renderer{},
		runtime.Renderer{},
		spire.Renderer{},
		systemd.Renderer{},
	}
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage(os.Stderr)
		return fmt.Errorf("missing subcommand")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "generate":
		return cmdGenerate(rest)
	case "render":
		return cmdRender(rest)
	case "list":
		return cmdList(rest)
	case "-h", "--help", "help":
		printUsage(os.Stdout)
		return nil
	default:
		printUsage(os.Stderr)
		return fmt.Errorf("unknown subcommand %q", sub)
	}
}

type commonFlags struct {
	repoRoot    string
	topologyDir string
	instance    string
	outputDir   string
}

func registerCommon(fs *flag.FlagSet) *commonFlags {
	c := &commonFlags{}
	fs.StringVar(&c.repoRoot, "repo-root", defaultRepoRoot, "repository root (used to resolve --topology-dir if relative)")
	fs.StringVar(&c.topologyDir, "topology-dir", defaultTopologyDir, "directory containing the CUE topology")
	fs.StringVar(&c.instance, "instance", defaultInstance, "topology instance name under src/cue-renderer/instances")
	fs.StringVar(&c.outputDir, "output-dir", "", "directory the generate command writes the cache layout under (required for generate)")
	return c
}

// resolved returns the absolute repo root and topology dir; the topology
// dir is what cuelang.org/go's load.Config.Dir wants, and OutputPath
// values are resolved against the repo root.
func (c *commonFlags) resolved() (repoRoot, topoDir string, err error) {
	root := c.repoRoot
	if root == defaultRepoRoot {
		// Bazel runs binaries from the execroot; BUILD_WORKSPACE_DIRECTORY
		// is the checked-out source tree where generated files must land.
		if workspace := strings.TrimSpace(os.Getenv("BUILD_WORKSPACE_DIRECTORY")); workspace != "" {
			root = workspace
		}
	}
	repoRoot, err = filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	topoDir = c.topologyDir
	if !filepath.IsAbs(topoDir) {
		topoDir = filepath.Join(repoRoot, topoDir)
	}
	return repoRoot, topoDir, nil
}

// withPipeline runs body inside a spans.Pipeline. spans.Pipeline installs a
// no-op tracer when OTEL_EXPORTER_OTLP_ENDPOINT is unset, so local runs never
// dial telemetry infrastructure.
func withPipeline(ctx context.Context, c *commonFlags, body func(context.Context, *spans.Recorder) error) error {
	return spans.Pipeline(ctx, c.topologyDir, c.instance, body)
}

func cmdGenerate(args []string) error {
	flags := flag.NewFlagSet("generate", flag.ContinueOnError)
	common := registerCommon(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(common.outputDir) == "" {
		return fmt.Errorf("--output-dir is required for generate")
	}
	repoRoot, topoDir, err := common.resolved()
	if err != nil {
		return err
	}
	// Bazel run shifts cwd to execroot, so relative --output-dir must
	// anchor against the resolved repo root rather than the process cwd.
	outputDir := common.outputDir
	if !filepath.IsAbs(outputDir) {
		outputDir = filepath.Join(repoRoot, outputDir)
	}
	return withPipeline(context.Background(), common, func(ctx context.Context, rec *spans.Recorder) error {
		rec.SetRootAttributes(attribute.String("topology.output_dir", outputDir))
		mem, artifacts, err := loadAndRender(ctx, rec, topoDir, common.instance)
		if err != nil {
			return err
		}
		// Two-phase write: render into MemFS first so a failure in any
		// renderer aborts the whole run before mutating disk. Only flush
		// to OSFS once every render succeeded.
		disk := render.OSFS{Root: outputDir}
		for _, p := range mem.Paths() {
			data, _ := mem.Get(p)
			if err := disk.WriteFile(p, data); err != nil {
				return fmt.Errorf("write %s: %w", p, err)
			}
			rec.Record(ctx,
				"topology.generated.render_artifact",
				attribute.String("topology.artifact", artifacts[p]),
				attribute.String("topology.generated_file", p),
				attribute.String("topology.generated_sha256", mem.SHA256(p)),
				attribute.Int("topology.bytes", len(data)),
			)
		}
		return nil
	})
}

// loadAndRender shares the front half of every command: load the CUE
// topology, attach the four input-digest attributes to the root span,
// emit the three `topology.cue.export_*` events, and run every renderer
// into a MemFS. Callers diverge on what they do with the MemFS — write
// it to disk (generate) or print a single artefact (render).
func loadAndRender(ctx context.Context, rec *spans.Recorder, topoDir, instance string) (*render.MemFS, map[string]string, error) {
	loaded, err := load.Topology(topoDir, instance)
	if err != nil {
		return nil, nil, err
	}
	inputAttrs := []attribute.KeyValue{
		attribute.String("topology.graph_sha256", loaded.GraphSHA256),
		attribute.String("topology.topology_sha256", loaded.TopologySHA256),
		attribute.String("topology.config_sha256", loaded.ConfigSHA256),
		attribute.String("topology.catalog_sha256", loaded.CatalogSHA256),
	}
	rec.SetRootAttributes(inputAttrs...)
	rec.Record(ctx, "topology.cue.export_graph", inputAttrs...)
	rec.Record(ctx, "topology.cue.export_config", attribute.String("topology.config_sha256", loaded.ConfigSHA256))
	rec.Record(ctx, "topology.cue.export_catalog", attribute.String("topology.catalog_sha256", loaded.CatalogSHA256))

	mem, artifacts, err := renderAll(ctx, loaded)
	if err != nil {
		return nil, nil, err
	}
	return mem, artifacts, nil
}

func cmdRender(args []string) error {
	flags := flag.NewFlagSet("render", flag.ContinueOnError)
	common := registerCommon(flags)
	selectedPath := flags.String("path", "", "repo-relative output path to print from a multi-file renderer")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		printUsage(os.Stderr)
		return fmt.Errorf("render requires exactly one artefact name")
	}
	name := flags.Arg(0)
	r, ok := rendererByName(name)
	if !ok {
		return fmt.Errorf("unknown renderer %q (try `cue-renderer list`)", name)
	}
	_, topoDir, err := common.resolved()
	if err != nil {
		return err
	}
	return withPipeline(context.Background(), common, func(ctx context.Context, _ *spans.Recorder) error {
		loaded, err := load.Topology(topoDir, common.instance)
		if err != nil {
			return err
		}
		mem := render.NewMemFS()
		if err := r.Render(ctx, loaded, mem); err != nil {
			return fmt.Errorf("render %s: %w", r.Name(), err)
		}
		if *selectedPath != "" {
			data, ok := mem.Get(*selectedPath)
			if !ok {
				return fmt.Errorf("renderer %s did not write %s", r.Name(), *selectedPath)
			}
			_, err := os.Stdout.Write(data)
			return err
		}
		paths := mem.Paths()
		if len(paths) == 1 {
			data, _ := mem.Get(paths[0])
			_, err := os.Stdout.Write(data)
			return err
		}
		// Multi-file renderers keep a reproducible debug dump.
		for _, p := range paths {
			data, _ := mem.Get(p)
			fmt.Fprintf(os.Stdout, "# %s\n", p)
			if _, err := os.Stdout.Write(data); err != nil {
				return err
			}
		}
		return nil
	})
}

func renderAll(ctx context.Context, loaded load.Loaded) (*render.MemFS, map[string]string, error) {
	mem := render.NewMemFS()
	artifacts := map[string]string{}
	for _, r := range renderers() {
		rendered := render.NewMemFS()
		if err := r.Render(ctx, loaded, rendered); err != nil {
			return nil, nil, fmt.Errorf("render %s: %w", r.Name(), err)
		}
		for _, p := range rendered.Paths() {
			if owner, ok := artifacts[p]; ok {
				return nil, nil, fmt.Errorf("render %s wrote %s already owned by %s", r.Name(), p, owner)
			}
			data, _ := rendered.Get(p)
			if err := mem.WriteFile(p, data); err != nil {
				return nil, nil, err
			}
			artifacts[p] = r.Name()
		}
	}
	return mem, artifacts, nil
}

func cmdList(args []string) error {
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	if err := flags.Parse(args); err != nil {
		return err
	}
	rs := renderers()
	names := make([]string, 0, len(rs))
	for _, r := range rs {
		names = append(names, r.Name())
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Println(n)
	}
	return nil
}

func rendererByName(name string) (render.Renderer, bool) {
	for _, r := range renderers() {
		if r.Name() == name {
			return r, true
		}
	}
	return nil, false
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `usage: cue-renderer <subcommand> [flags]

subcommands:
  generate        render every artefact to disk
  render <name>   render a single artefact to stdout (debug); use --path for one file from multi-file renderers
  list            print the registered artefacts

global flags:
  --repo-root      path to repo root (default ".")
  --topology-dir   path to CUE topology root (default "src/cue-renderer")
  --instance       topology instance name under instances/ (default "prod")
  --output-dir     cache directory the generate command writes into (required for generate)`)
}
