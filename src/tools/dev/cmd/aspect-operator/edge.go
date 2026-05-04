package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	edgecontract "github.com/verself/host-configuration/edgecontract"
	"gopkg.in/yaml.v3"
)

type edgeConfig struct {
	repoRoot   string
	site       string
	format     string
	nomadIndex string
}

func cmdEdge(args []string) error {
	if len(args) == 0 {
		printEdgeUsage(os.Stderr)
		return fmt.Errorf("missing edge subcommand")
	}
	switch args[0] {
	case "check":
		return cmdEdgeCheck(args[1:])
	case "manifest":
		return cmdEdgeManifest(args[1:])
	case "-h", "--help", "help":
		printEdgeUsage(os.Stdout)
		return nil
	default:
		printEdgeUsage(os.Stderr)
		return fmt.Errorf("unknown edge subcommand: %s", args[0])
	}
}

func cmdEdgeCheck(args []string) error {
	cfg, err := parseEdgeFlags("edge check", args)
	if err != nil {
		return err
	}
	bundle, err := edgecontract.Build(edgecontract.Config{RepoRoot: cfg.repoRoot, Site: cfg.site, NomadIndex: cfg.nomadIndex})
	if err != nil {
		return err
	}
	issues := append([]string{}, bundle.Issues...)
	sort.Strings(issues)
	if len(issues) > 0 {
		for _, issue := range issues {
			fmt.Fprintln(os.Stderr, issue)
		}
		return exitError{code: 1}
	}
	_, _ = fmt.Fprintf(os.Stdout,
		"edge contract ok: site=%s routes=%d nomad_upstreams=%d upstream_keys=%d guid_objects=%d\n",
		bundle.Manifest.Site,
		len(bundle.Manifest.Routes),
		len(bundle.Manifest.NomadUpstreams),
		len(bundle.Manifest.UpstreamKeys),
		len(bundle.Manifest.Frontends)+len(bundle.Manifest.Backends)+len(bundle.Manifest.Servers),
	)
	return nil
}

func cmdEdgeManifest(args []string) error {
	cfg, err := parseEdgeFlags("edge manifest", args)
	if err != nil {
		return err
	}
	bundle, err := edgecontract.Build(edgecontract.Config{RepoRoot: cfg.repoRoot, Site: cfg.site, NomadIndex: cfg.nomadIndex})
	if err != nil {
		return err
	}
	if len(bundle.Issues) > 0 {
		for _, issue := range bundle.Issues {
			fmt.Fprintln(os.Stderr, issue)
		}
		return exitError{code: 1}
	}
	return writeEdgeManifest(os.Stdout, cfg.format, bundle.Manifest)
}

func parseEdgeFlags(name string, args []string) (edgeConfig, error) {
	cfg := edgeConfig{
		site:   edgecontract.DefaultSite,
		format: "text",
	}
	fs := flagSet(name)
	fs.StringVar(&cfg.repoRoot, "repo-root", "", "Path to the verself-sh checkout root.")
	fs.StringVar(&cfg.site, "site", edgecontract.DefaultSite, "Deployment site whose Nomad jobs should be checked.")
	fs.StringVar(&cfg.format, "format", "text", "Manifest output format: text, json, or yaml.")
	fs.StringVar(&cfg.nomadIndex, "nomad-index", "", "Path to the Bazel-built Nomad component index JSON.")
	if err := fs.Parse(args); err != nil {
		return edgeConfig{}, err
	}
	if fs.NArg() != 0 {
		return edgeConfig{}, fmt.Errorf("%s: unexpected positional args: %s", name, strings.Join(fs.Args(), " "))
	}
	if cfg.repoRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return edgeConfig{}, fmt.Errorf("%s: cwd: %w", name, err)
		}
		cfg.repoRoot = cwd
	}
	abs, err := filepath.Abs(cfg.repoRoot)
	if err != nil {
		return edgeConfig{}, fmt.Errorf("%s: resolve --repo-root: %w", name, err)
	}
	cfg.repoRoot = abs
	if cfg.nomadIndex != "" && !filepath.IsAbs(cfg.nomadIndex) {
		cfg.nomadIndex = filepath.Join(cfg.repoRoot, cfg.nomadIndex)
	}
	if cfg.site == "" {
		return edgeConfig{}, fmt.Errorf("%s: --site is required", name)
	}
	switch cfg.format {
	case "text", "json", "yaml":
	default:
		return edgeConfig{}, fmt.Errorf("%s: unsupported --format=%s", name, cfg.format)
	}
	return cfg, nil
}

func printEdgeUsage(w *os.File) {
	_, _ = fmt.Fprint(w, `aspect-operator edge <subcommand> [flags]

Subcommands:
  check      Validate topology, Nomad service registrations, and authored HAProxy contracts
  manifest   Emit the derived edge contract manifest

Common flags:
  --repo-root <path>  verself-sh checkout root
  --site <site>       deployment site (default: prod)
  --nomad-index <path> Bazel-built Nomad component index JSON
  --format <format>   manifest format: text, json, yaml
`)
}

func writeEdgeManifest(w io.Writer, format string, manifest edgecontract.Manifest) error {
	switch format {
	case "text":
		if _, err := fmt.Fprintf(w, "site: %s\n", manifest.Site); err != nil {
			return err
		}
		keys := make([]string, 0, len(manifest.Summary))
		for key := range manifest.Summary {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if _, err := fmt.Fprintf(w, "%s: %d\n", key, manifest.Summary[key]); err != nil {
				return err
			}
		}
		return nil
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(manifest)
	case "yaml":
		enc := yaml.NewEncoder(w)
		if err := enc.Encode(manifest); err != nil {
			_ = enc.Close()
			return err
		}
		return enc.Close()
	default:
		return fmt.Errorf("unsupported manifest format %q", format)
	}
}
