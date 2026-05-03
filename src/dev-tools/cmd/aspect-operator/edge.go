package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	edgecontract "github.com/verself/host-configuration/edgecontract"
	opch "github.com/verself/operator-runtime/clickhouse"
	opruntime "github.com/verself/operator-runtime/runtime"
	"gopkg.in/yaml.v3"
)

var haproxyRuntimeTableRe = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,127}$`)

type edgeConfig struct {
	repoRoot string
	site     string
	format   string
}

type edgeRuntimeOptions struct {
	operatorRuntimeOptions
	show   string
	format string
	table  string
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
	case "render":
		return cmdEdgeRender(args[1:])
	case "runtime":
		return cmdEdgeRuntime(args[1:])
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
	bundle, err := edgecontract.Build(edgecontract.Config{RepoRoot: cfg.repoRoot, Site: cfg.site})
	if err != nil {
		return err
	}
	issues := append([]string{}, bundle.Issues...)
	issues = append(issues, bundle.ArtifactIssues()...)
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
	bundle, err := edgecontract.Build(edgecontract.Config{RepoRoot: cfg.repoRoot, Site: cfg.site})
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

func cmdEdgeRender(args []string) error {
	cfg, err := parseEdgeFlags("edge render", args)
	if err != nil {
		return err
	}
	bundle, err := edgecontract.Build(edgecontract.Config{RepoRoot: cfg.repoRoot, Site: cfg.site})
	if err != nil {
		return err
	}
	if len(bundle.Issues) > 0 {
		for _, issue := range bundle.Issues {
			fmt.Fprintln(os.Stderr, issue)
		}
		return exitError{code: 1}
	}
	if err := bundle.WriteArtifacts(); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "edge artifacts rendered: %s %s %s\n",
		bundle.Sources.HAProxyTemplate,
		bundle.Sources.PublicHostsMap,
		bundle.Sources.InitialUpstreamsMap,
	)
	return nil
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
  check      Validate topology, Nomad service registrations, and generated HAProxy artifacts
  manifest   Emit the derived edge contract manifest
  render     Rewrite HAProxy artifacts from the edge contract
  runtime    Read HAProxy runtime API state through the operator SSH path

Common flags:
  --repo-root <path>  verself-sh checkout root
  --site <site>       deployment site (default: prod)
  --format <format>   manifest format: text, json, yaml

Runtime flags:
  --show <info|stat|sni|errors|table>
  --format <text|typed|json>  applies to info and stat
  --table <name>              optional for --show=table
`)
}

func writeEdgeManifest(w io.Writer, format string, manifest edgecontract.Manifest) error {
	switch format {
	case "text":
		if _, err := fmt.Fprintf(w, "version: %s\nsite: %s\n", manifest.Version, manifest.Site); err != nil {
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

func cmdEdgeRuntime(args []string) error {
	opts := edgeRuntimeOptions{
		show:   "info",
		format: "text",
	}
	addOperatorRuntimeFlags(&opts.operatorRuntimeOptions)
	fs := flagSet("edge runtime")
	fs.StringVar(&opts.site, "site", opts.site, "Deploy site")
	fs.StringVar(&opts.repoRoot, "repo-root", "", "verself-sh checkout root (defaults to cwd)")
	fs.StringVar(&opts.show, "show", opts.show, "Runtime view: info, stat, sni, errors, or table")
	fs.StringVar(&opts.format, "format", opts.format, "Runtime output format for info/stat: text, typed, or json")
	fs.StringVar(&opts.table, "table", "", "Stick table name for --show=table")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("edge runtime: unexpected positional args: %s", strings.Join(fs.Args(), " "))
	}
	command, err := edgeRuntimeCommand(opts)
	if err != nil {
		return err
	}
	return runOperatorRuntime("edge.runtime."+opts.show, opts.operatorRuntimeOptions, false, opch.Config{Database: "verself"}, func(rt *opruntime.Runtime, _ *opch.Client) error {
		out, err := runHAProxyRuntimeCommand(rt, command)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(out)
		return err
	})
}

func edgeRuntimeCommand(opts edgeRuntimeOptions) (string, error) {
	show := strings.ToLower(strings.TrimSpace(opts.show))
	format := strings.ToLower(strings.TrimSpace(opts.format))
	if format == "" {
		format = "text"
	}
	if format != "text" && format != "typed" && format != "json" {
		return "", fmt.Errorf("edge runtime: --format must be text, typed, or json")
	}
	formatSuffix := ""
	if format != "text" {
		formatSuffix = " " + format
	}
	switch show {
	case "info":
		return "show info" + formatSuffix, nil
	case "stat":
		return "show stat" + formatSuffix, nil
	case "sni":
		if format != "text" {
			return "", fmt.Errorf("edge runtime: --show=sni only supports --format=text")
		}
		return "show ssl sni", nil
	case "errors":
		if format != "text" {
			return "", fmt.Errorf("edge runtime: --show=errors only supports --format=text")
		}
		return "show errors", nil
	case "table":
		if format != "text" {
			return "", fmt.Errorf("edge runtime: --show=table only supports --format=text")
		}
		table := strings.TrimSpace(opts.table)
		if table == "" {
			return "show table", nil
		}
		if !haproxyRuntimeTableRe.MatchString(table) {
			return "", fmt.Errorf("edge runtime: invalid --table %q", table)
		}
		return "show table " + table, nil
	default:
		return "", fmt.Errorf("edge runtime: --show must be info, stat, sni, errors, or table")
	}
}

func runHAProxyRuntimeCommand(rt *opruntime.Runtime, command string) ([]byte, error) {
	if rt == nil || rt.SSH == nil {
		return nil, fmt.Errorf("edge runtime: operator SSH runtime is required")
	}
	script := `import socket
import sys

sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
sock.connect("/run/haproxy/admin.sock")
sock.sendall((sys.argv[1] + "\n").encode("utf-8"))
sock.shutdown(socket.SHUT_WR)
chunks = []
while True:
    chunk = sock.recv(65536)
    if not chunk:
        break
    chunks.append(chunk)
sys.stdout.buffer.write(b"".join(chunks))
`
	scriptWord, err := opruntime.ShellWord(script)
	if err != nil {
		return nil, err
	}
	commandWord, err := opruntime.ShellWord(command)
	if err != nil {
		return nil, err
	}
	return rt.SSH.Exec(rt.Ctx, "sudo /usr/bin/python3 -c "+scriptWord+" "+commandWord)
}
