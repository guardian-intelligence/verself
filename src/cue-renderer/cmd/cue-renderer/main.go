// Command cue-renderer compiles the CUE topology graph at
// src/cue-renderer/ into the artefacts the rest of the platform
// consumes. See src/cue-renderer/README.md for the bigger picture.
//
// Subcommands:
//
//	cue-renderer generate            # write every renderer's output to disk
//	cue-renderer check               # exit non-zero if any output is stale
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

	"go.opentelemetry.io/otel/attribute"

	"github.com/verself/cue-renderer/internal/load"
	"github.com/verself/cue-renderer/internal/render"
	"github.com/verself/cue-renderer/internal/spans"
)

const (
	defaultTopologyDir  = "src/cue-renderer"
	defaultInstance     = "./instances/local:topology"
	defaultRepoRoot     = "."
	defaultGeneratedDir = "src/platform/ansible/group_vars/all/generated"
)

// renderers is the set of artefacts the Go tool currently owns. The
// scaffold ships with none — the framework (load + spans + WritableFS +
// the Renderer interface) is the deliverable, and the first real
// renderer to land is the per-component nftables snippet generator.
//
// Adding a renderer is one line here plus its package under
// internal/render/. Renderers not yet ported continue to be produced by
// topology.py until they appear here.
func renderers() []render.Renderer {
	return nil
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
	case "check":
		return cmdCheck(rest)
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
	flushSpans  bool
}

func registerCommon(fs *flag.FlagSet) *commonFlags {
	c := &commonFlags{}
	fs.StringVar(&c.repoRoot, "repo-root", defaultRepoRoot, "repository root (paths in OutputPath() are resolved against this)")
	fs.StringVar(&c.topologyDir, "topology-dir", defaultTopologyDir, "directory containing the CUE topology")
	fs.StringVar(&c.instance, "instance", defaultInstance, "CUE load.Instance argument")
	fs.BoolVar(&c.flushSpans, "flush-spans", true, "emit OTel proof spans for this run")
	return c
}

// resolved returns the absolute repo root and topology dir; the topology
// dir is what cuelang.org/go's load.Config.Dir wants, and OutputPath
// values are resolved against the repo root.
func (c *commonFlags) resolved() (repoRoot, topoDir string, err error) {
	repoRoot, err = filepath.Abs(c.repoRoot)
	if err != nil {
		return "", "", err
	}
	topoDir = c.topologyDir
	if !filepath.IsAbs(topoDir) {
		topoDir = filepath.Join(repoRoot, topoDir)
	}
	return repoRoot, topoDir, nil
}

// withPipeline runs body inside a spans.Pipeline (the outer trace
// envelope) when --flush-spans is on, and unwrapped otherwise. Tests
// disable spans because there's no OTLP collector to talk to.
func withPipeline(ctx context.Context, c *commonFlags, body func(context.Context, recorder) error) error {
	if !c.flushSpans {
		return body(ctx, noopRecorder{})
	}
	return spans.Pipeline(ctx, c.topologyDir, defaultGeneratedDir, func(pctx context.Context, rec *spans.Recorder) error {
		return body(pctx, &spanRecorder{rec: rec})
	})
}

// recorder is the minimal interface body needs from spans.Recorder, so
// the noop variant doesn't have to drag the OTel SDK in.
type recorder interface {
	Record(ctx context.Context, name string, extras ...attribute.KeyValue)
}

type spanRecorder struct{ rec *spans.Recorder }

func (s *spanRecorder) Record(ctx context.Context, name string, extras ...attribute.KeyValue) {
	s.rec.Record(ctx, name, extras...)
}

type noopRecorder struct{}

func (noopRecorder) Record(_ context.Context, _ string, _ ...attribute.KeyValue) {}

func cmdGenerate(args []string) error {
	flags := flag.NewFlagSet("generate", flag.ContinueOnError)
	common := registerCommon(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	repoRoot, topoDir, err := common.resolved()
	if err != nil {
		return err
	}
	return withPipeline(context.Background(), common, func(ctx context.Context, rec recorder) error {
		loaded, err := load.Topology(topoDir, common.instance)
		if err != nil {
			return err
		}
		rec.Record(ctx, "topology.cue.export_graph", attribute.String("topology.graph_sha256", loaded.GraphSHA256))

		mem := render.NewMemFS()
		for _, r := range renderers() {
			if err := r.Render(ctx, loaded, mem); err != nil {
				return fmt.Errorf("render %s: %w", r.Name(), err)
			}
		}
		// Two-phase write: render into MemFS first so a failure in any
		// renderer aborts the whole run before mutating disk. Only flush
		// to OSFS once every render succeeded.
		disk := render.OSFS{Root: repoRoot}
		for _, p := range mem.Paths() {
			data, _ := mem.Get(p)
			if err := disk.WriteFile(p, data); err != nil {
				return fmt.Errorf("write %s: %w", p, err)
			}
			rec.Record(ctx,
				"topology.generated.render_artifact",
				attribute.String("topology.generated_file", p),
				attribute.String("topology.generated_sha256", mem.SHA256(p)),
			)
		}
		return nil
	})
}

func cmdCheck(args []string) error {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	common := registerCommon(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	repoRoot, topoDir, err := common.resolved()
	if err != nil {
		return err
	}
	return withPipeline(context.Background(), common, func(ctx context.Context, rec recorder) error {
		loaded, err := load.Topology(topoDir, common.instance)
		if err != nil {
			return err
		}
		rec.Record(ctx, "topology.cue.export_graph", attribute.String("topology.graph_sha256", loaded.GraphSHA256))

		mem := render.NewMemFS()
		for _, r := range renderers() {
			if err := r.Render(ctx, loaded, mem); err != nil {
				return fmt.Errorf("render %s: %w", r.Name(), err)
			}
		}
		stale, err := mem.DiffAgainstDisk(repoRoot)
		if err != nil {
			return err
		}
		for _, p := range mem.Paths() {
			rec.Record(ctx,
				"topology.generated.freshness_check",
				attribute.String("topology.generated_file", p),
				attribute.String("topology.generated_sha256", mem.SHA256(p)),
				attribute.Bool("topology.generated_fresh", !contains(stale, p)),
			)
		}
		if len(stale) > 0 {
			for _, p := range stale {
				fmt.Fprintf(os.Stderr, "stale: %s\n", p)
			}
			return fmt.Errorf("%d artefact(s) stale; rerun `cue-renderer generate`", len(stale))
		}
		return nil
	})
}

func cmdRender(args []string) error {
	flags := flag.NewFlagSet("render", flag.ContinueOnError)
	common := registerCommon(flags)
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
	return withPipeline(context.Background(), common, func(ctx context.Context, _ recorder) error {
		loaded, err := load.Topology(topoDir, common.instance)
		if err != nil {
			return err
		}
		mem := render.NewMemFS()
		if err := r.Render(ctx, loaded, mem); err != nil {
			return fmt.Errorf("render %s: %w", r.Name(), err)
		}
		// `render <name>` writes to stdout for human inspection, joining
		// every file the renderer produced. Ordered by path so the dump
		// is reproducible.
		for _, p := range mem.Paths() {
			data, _ := mem.Get(p)
			fmt.Fprintf(os.Stdout, "# %s\n", p)
			if _, err := os.Stdout.Write(data); err != nil {
				return err
			}
		}
		return nil
	})
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

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `usage: cue-renderer <subcommand> [flags]

subcommands:
  generate        render every artefact to disk
  check           diff every artefact against disk; exit 1 on drift
  render <name>   render a single artefact to stdout (debug)
  list            print the registered artefacts

global flags:
  --repo-root      path to repo root (default ".")
  --topology-dir   path to CUE topology root (default "src/cue-renderer")
  --instance       CUE load.Instance (default "./instances/local:topology")
  --flush-spans    emit OTel proof spans (default true)`)
}
