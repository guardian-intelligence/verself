// Command guardian evaluates Guardian Specification config documents.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
	"github.com/verself/guardian-specification/internal/formatio"
	"github.com/verself/guardian-specification/internal/guardianconfig"
	"github.com/verself/guardian-specification/internal/specdoc"
	"github.com/verself/guardian-specification/internal/toolcatalog"
	"github.com/verself/guardian-specification/internal/toolrun"
)

const (
	defaultFlyDocumentPath = ".guardian/fly/document.json"
	defaultBazelBuildRoot  = ".guardian/build"
	bazelBuildManifestPath = ".guardian/build/manifest.json"
	hookOutputTailBytes    = 12000
)

type guardianDocument struct {
	Source   specdoc.Document
	Compiled specdoc.CompiledDocument
}

type condition struct {
	Type     string `json:"type" yaml:"type" toml:"type" toon:"type"`
	Status   string `json:"status" yaml:"status" toml:"status" toon:"status"`
	Reason   string `json:"reason" yaml:"reason" toml:"reason" toon:"reason"`
	Message  string `json:"message,omitempty" yaml:"message,omitempty" toml:"message,omitempty" toon:"message,omitempty"`
	Resource string `json:"resource,omitempty" yaml:"resource,omitempty" toml:"resource,omitempty" toon:"resource,omitempty"`
}

type preflightResult struct {
	Profile        string            `json:"profile" yaml:"profile" toml:"profile" toon:"profile"`
	Name           string            `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty" toon:"name,omitempty"`
	Status         string            `json:"status" yaml:"status" toml:"status" toon:"status"`
	ReadyToFly     string            `json:"ready_to_fly" yaml:"ready_to_fly" toml:"ready_to_fly" toon:"ready_to_fly"`
	ExecutionMode  string            `json:"execution_mode" yaml:"execution_mode" toml:"execution_mode" toon:"execution_mode"`
	ResourceDigest string            `json:"resource_digest,omitempty" yaml:"resource_digest,omitempty" toml:"resource_digest,omitempty" toon:"resource_digest,omitempty"`
	Entrypoint     resourceRefResult `json:"entrypoint" yaml:"entrypoint" toml:"entrypoint" toon:"entrypoint"`
	Preflight      ansibleResult     `json:"preflight" yaml:"preflight" toml:"preflight" toon:"preflight"`
	Upload         uploadResult      `json:"upload" yaml:"upload" toml:"upload" toon:"upload"`
	Conditions     []condition       `json:"conditions" yaml:"conditions" toml:"conditions" toon:"conditions"`
}

type resourceRefResult struct {
	APIVersion string `json:"apiVersion" yaml:"apiVersion" toml:"apiVersion" toon:"apiVersion"`
	Kind       string `json:"kind" yaml:"kind" toml:"kind" toon:"kind"`
	Name       string `json:"name" yaml:"name" toml:"name" toon:"name"`
}

type uploadResult struct {
	Status string `json:"status" yaml:"status" toml:"status" toon:"status"`
	Reason string `json:"reason" yaml:"reason" toml:"reason" toon:"reason"`
}

type hookResult struct {
	Argv    []string `json:"argv" yaml:"argv" toml:"argv" toon:"argv"`
	Status  string   `json:"status" yaml:"status" toml:"status" toon:"status"`
	Reason  string   `json:"reason" yaml:"reason" toml:"reason" toon:"reason"`
	Message string   `json:"message,omitempty" yaml:"message,omitempty" toml:"message,omitempty" toon:"message,omitempty"`
}

type ansibleResult struct {
	Playbook string `json:"playbook" yaml:"playbook" toml:"playbook" toon:"playbook"`
	Status   string `json:"status" yaml:"status" toml:"status" toon:"status"`
	Reason   string `json:"reason" yaml:"reason" toml:"reason" toon:"reason"`
	Message  string `json:"message,omitempty" yaml:"message,omitempty" toml:"message,omitempty" toon:"message,omitempty"`
}

type flyResult struct {
	Profile        string            `json:"profile" yaml:"profile" toml:"profile" toon:"profile"`
	Name           string            `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty" toon:"name,omitempty"`
	Status         string            `json:"status" yaml:"status" toml:"status" toon:"status"`
	ReadyToFly     string            `json:"ready_to_fly" yaml:"ready_to_fly" toml:"ready_to_fly" toon:"ready_to_fly"`
	ExecutionMode  string            `json:"execution_mode" yaml:"execution_mode" toml:"execution_mode" toon:"execution_mode"`
	ResourceDigest string            `json:"resource_digest,omitempty" yaml:"resource_digest,omitempty" toml:"resource_digest,omitempty" toon:"resource_digest,omitempty"`
	Entrypoint     resourceRefResult `json:"entrypoint" yaml:"entrypoint" toml:"entrypoint" toon:"entrypoint"`
	Nomad          nomadResult       `json:"nomad" yaml:"nomad" toml:"nomad" toon:"nomad"`
	Conditions     []condition       `json:"conditions" yaml:"conditions" toml:"conditions" toon:"conditions"`
}

type nomadResult struct {
	Address   string           `json:"address" yaml:"address" toml:"address" toon:"address"`
	Namespace string           `json:"namespace" yaml:"namespace" toml:"namespace" toon:"namespace"`
	Status    string           `json:"status" yaml:"status" toml:"status" toon:"status"`
	Reason    string           `json:"reason" yaml:"reason" toml:"reason" toon:"reason"`
	Message   string           `json:"message,omitempty" yaml:"message,omitempty" toml:"message,omitempty" toon:"message,omitempty"`
	Jobs      []nomadJobResult `json:"jobs" yaml:"jobs" toml:"jobs" toon:"jobs"`
}

type nomadJobResult struct {
	Name        string `json:"name" yaml:"name" toml:"name" toon:"name"`
	Path        string `json:"path" yaml:"path" toml:"path" toon:"path"`
	Type        string `json:"type,omitempty" yaml:"type,omitempty" toml:"type,omitempty" toon:"type,omitempty"`
	InputDigest string `json:"input_digest,omitempty" yaml:"input_digest,omitempty" toml:"input_digest,omitempty" toon:"input_digest,omitempty"`
	Status      string `json:"status" yaml:"status" toml:"status" toon:"status"`
	Reason      string `json:"reason" yaml:"reason" toml:"reason" toon:"reason"`
	Message     string `json:"message,omitempty" yaml:"message,omitempty" toml:"message,omitempty" toon:"message,omitempty"`
}

type commandOptions struct {
	Config        string
	Profile       string
	Output        string
	WorkspaceRoot string
	Stream        bool
	DryRun        bool
	Help          bool
}

type eventWriter struct {
	enabled bool
	stderr  io.Writer
}

type progressEvent struct {
	At       string `json:"at"`
	Phase    string `json:"phase"`
	Status   string `json:"status"`
	Resource string `json:"resource,omitempty"`
	Message  string `json:"message,omitempty"`
}

func main() {
	args := os.Args[1:]
	if toolName := invokedToolName(os.Args[0]); toolName != "" {
		args = append([]string{"run", toolName, "--"}, args...)
	}
	os.Exit(run(args, os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) < 1 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "run":
		return runTool(args[1:], stdout, stderr)
	case "preflight":
		return runPreflight(args[1:], stdout, stderr)
	case "profiles":
		return runProfiles(args[1:], stdout, stderr)
	case "fly":
		return runFly(args[1:], stdout, stderr)
	case "-h", "--help", "help":
		usage(stderr)
		return 0
	default:
		return commandUsageError(stderr, "guardian", fmt.Sprintf("unknown command: %s", args[0]), rootCommandSuggestions(args))
	}
}

func invokedToolName(arg0 string) string {
	base := filepath.Base(arg0)
	base = strings.TrimSuffix(base, ".exe")
	if base == "" || base == "guardian" {
		return ""
	}
	return base
}

func runTool(args []string, stdout io.Writer, stderr io.Writer) int {
	opts, operands, toolArgs, execute, ok := parseRunFlags(args, stderr)
	if !ok {
		return 2
	}
	if opts.Help {
		runUsage(stderr)
		return 0
	}
	modeCount := 0
	for _, enabled := range []bool{opts.List, opts.Which, opts.Verify, opts.InstallShims} {
		if enabled {
			modeCount++
		}
	}
	if modeCount > 1 {
		return commandUsageError(stderr, "guardian run", "choose only one of --list, --which, --verify, or --install-shims", nil)
	}
	if opts.List {
		if execute || len(operands) != 0 {
			return commandUsageError(stderr, "guardian run", "--list does not accept a tool or tool arguments", nil)
		}
		return runToolList(opts, stdout, stderr)
	}
	if opts.InstallShims {
		if execute || len(operands) != 0 {
			return commandUsageError(stderr, "guardian run", "--install-shims does not accept a tool or tool arguments", nil)
		}
		return runToolInstallShims(opts, stdout, stderr)
	}
	if len(operands) == 0 {
		return commandUsageError(stderr, "guardian run", "tool name is required", nil)
	}
	if len(operands) != 1 {
		return commandUsageError(stderr, "guardian run", fmt.Sprintf("expected one tool, got %d", len(operands)), runOperandSuggestions(operands))
	}
	toolName := operands[0]
	if opts.Which {
		if execute {
			return commandUsageError(stderr, "guardian run", "--which cannot be combined with -- tool arguments", nil)
		}
		return runToolWhich(opts, toolName, stdout, stderr)
	}
	if opts.Verify {
		if execute {
			return commandUsageError(stderr, "guardian run", "--verify cannot be combined with -- tool arguments", nil)
		}
		return runToolVerify(opts, toolName, stdout, stderr)
	}
	if !execute {
		return commandUsageError(stderr, "guardian run", "expected -- before tool arguments", runOperandSuggestions(operands))
	}
	workspaceRoot, ok := commandWorkspaceRoot("guardian run", stderr)
	if !ok {
		return 1
	}
	tool, err := toolcatalog.Resolve(workspaceRoot, toolName)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "guardian run: %v\n", err)
		return 1
	}
	if toolName == "bazel" && len(toolArgs) > 0 && toolArgs[0] == "build" {
		return runBazelBuildTool(context.Background(), tool, toolArgs, workspaceRoot, stdout, stderr)
	}
	return toolrun.Exec(context.Background(), tool, toolArgs, toolrun.Options{
		WorkDir: workspaceRoot,
		Stdout:  stdout,
		Stderr:  stderr,
	})
}

func runBazelBuildTool(ctx context.Context, tool toolcatalog.ResolvedTool, args []string, workspaceRoot string, stdout io.Writer, stderr io.Writer) int {
	buildRoot := filepath.Join(workspaceRoot, filepath.FromSlash(defaultBazelBuildRoot))
	if err := os.MkdirAll(buildRoot, 0o755); err != nil {
		_, _ = fmt.Fprintf(stderr, "guardian run: create %s: %v\n", defaultBazelBuildRoot, err)
		return 1
	}
	bepPath, hasBEPPath := bazelBuildEventJSONPath(args, workspaceRoot)
	if !hasBEPPath {
		bepPath = filepath.Join(buildRoot, "build-events.json")
	}
	bazelArgs := append([]string(nil), args...)
	bazelArgs = append(bazelArgs,
		"--experimental_convenience_symlinks=ignore",
		"--nobuild_event_publish_all_actions",
	)
	if !hasBEPPath {
		bazelArgs = append(bazelArgs, "--build_event_json_file="+bepPath)
	}
	code := toolrun.Exec(ctx, tool, bazelArgs, toolrun.Options{
		WorkDir: workspaceRoot,
		Stdout:  stdout,
		Stderr:  stderr,
	})
	if code != 0 {
		return code
	}
	if err := materializeBazelBuildOutputs(workspaceRoot, bepPath); err != nil {
		_, _ = fmt.Fprintf(stderr, "guardian run: materialize Bazel outputs: %v\n", err)
		return 1
	}
	return 0
}

func bazelBuildEventJSONPath(args []string, workspaceRoot string) (string, bool) {
	for i, arg := range args {
		if value, ok := strings.CutPrefix(arg, "--build_event_json_file="); ok {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if filepath.IsAbs(value) {
				return value, true
			}
			return filepath.Join(workspaceRoot, value), true
		}
		if arg == "--build_event_json_file" && i+1 < len(args) {
			value := strings.TrimSpace(args[i+1])
			if value == "" {
				continue
			}
			if filepath.IsAbs(value) {
				return value, true
			}
			return filepath.Join(workspaceRoot, value), true
		}
	}
	return "", false
}

func runToolList(opts runOptions, stdout io.Writer, stderr io.Writer) int {
	workspaceRoot, ok := commandWorkspaceRoot("guardian run", stderr)
	if !ok {
		return 1
	}
	names, err := toolcatalog.ToolNames(workspaceRoot)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "guardian run: %v\n", err)
		return 1
	}
	if err := writeOutput(stdout, opts.Output, toolListResult{Tools: names}); err != nil {
		_, _ = fmt.Fprintf(stderr, "guardian run: %v\n", err)
		return 2
	}
	return 0
}

func runToolWhich(opts runOptions, toolName string, stdout io.Writer, stderr io.Writer) int {
	result, code := resolveAndEnsureTool("guardian run", toolName, stderr)
	if code != 0 {
		return code
	}
	if err := writeOutput(stdout, opts.Output, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "guardian run: %v\n", err)
		return 2
	}
	return 0
}

func runToolVerify(opts runOptions, toolName string, stdout io.Writer, stderr io.Writer) int {
	result, code := resolveAndEnsureTool("guardian run", toolName, stderr)
	if code != 0 {
		return code
	}
	result.Status = "ready"
	if err := writeOutput(stdout, opts.Output, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "guardian run: %v\n", err)
		return 2
	}
	return 0
}

func resolveAndEnsureTool(commandName string, toolName string, stderr io.Writer) (toolWhichResult, int) {
	workspaceRoot, ok := commandWorkspaceRoot(commandName, stderr)
	if !ok {
		return toolWhichResult{}, 1
	}
	tool, err := toolcatalog.Resolve(workspaceRoot, toolName)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", commandName, err)
		return toolWhichResult{}, 1
	}
	path, err := toolrun.EnsureExecutable(context.Background(), tool, toolrun.Options{WorkDir: workspaceRoot, Stderr: stderr})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", commandName, err)
		return toolWhichResult{}, 1
	}
	return toolWhichResult{
		Tool:       tool.Name,
		Platform:   tool.Platform,
		Ref:        tool.Ref,
		Digest:     tool.Digest,
		Admission:  tool.Admission,
		Executable: path,
		Status:     "present",
	}, 0
}

func runToolInstallShims(opts runOptions, stdout io.Writer, stderr io.Writer) int {
	if strings.TrimSpace(opts.BinDir) == "" {
		return commandUsageError(stderr, "guardian run", "--bin-dir is required for --install-shims", nil)
	}
	workspaceRoot, ok := commandWorkspaceRoot("guardian run", stderr)
	if !ok {
		return 1
	}
	tools, err := toolcatalog.ResolveAll(workspaceRoot)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "guardian run: %v\n", err)
		return 1
	}
	executable, err := os.Executable()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "guardian run: resolve guardian executable: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(opts.BinDir, 0o755); err != nil {
		_, _ = fmt.Fprintf(stderr, "guardian run: create %s: %v\n", opts.BinDir, err)
		return 1
	}
	var installed []string
	for _, tool := range tools {
		path := filepath.Join(opts.BinDir, tool.Name)
		if err := installShim(path, executable); err != nil {
			_, _ = fmt.Fprintf(stderr, "guardian run: %v\n", err)
			return 1
		}
		installed = append(installed, path)
	}
	if err := writeOutput(stdout, opts.Output, toolShimResult{Installed: installed}); err != nil {
		_, _ = fmt.Fprintf(stderr, "guardian run: %v\n", err)
		return 2
	}
	return 0
}

func installShim(path string, target string) error {
	if current, err := os.Readlink(path); err == nil {
		if current == target {
			return nil
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("replace shim %s: %w", path, err)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		if _, statErr := os.Stat(path); statErr == nil {
			return fmt.Errorf("%s exists and is not a Guardian shim symlink", path)
		}
		return fmt.Errorf("inspect shim %s: %w", path, err)
	}
	if err := os.Symlink(target, path); err != nil {
		return fmt.Errorf("create shim %s -> %s: %w", path, target, err)
	}
	return nil
}

type toolListResult struct {
	Tools []string `json:"tools" yaml:"tools" toml:"tools" toon:"tools"`
}

type toolWhichResult struct {
	Tool       string `json:"tool" yaml:"tool" toml:"tool" toon:"tool"`
	Platform   string `json:"platform" yaml:"platform" toml:"platform" toon:"platform"`
	Ref        string `json:"ref" yaml:"ref" toml:"ref" toon:"ref"`
	Digest     string `json:"digest" yaml:"digest" toml:"digest" toon:"digest"`
	Admission  string `json:"admission" yaml:"admission" toml:"admission" toon:"admission"`
	Executable string `json:"executable" yaml:"executable" toml:"executable" toon:"executable"`
	Status     string `json:"status" yaml:"status" toml:"status" toon:"status"`
}

type toolShimResult struct {
	Installed []string `json:"installed" yaml:"installed" toml:"installed" toon:"installed"`
}

type runOptions struct {
	Output       string
	BinDir       string
	List         bool
	Which        bool
	Verify       bool
	InstallShims bool
	Help         bool
}

func parseRunFlags(args []string, stderr io.Writer) (runOptions, []string, []string, bool, bool) {
	before, toolArgs, execute := splitRunInvocationArgs(args)
	fs := flag.NewFlagSet("guardian run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := runOptions{Output: "yaml"}
	fs.StringVar(&opts.Output, "o", "yaml", "output format: yaml | json | toml | toon")
	fs.StringVar(&opts.Output, "output", "yaml", "output format: yaml | json | toml | toon")
	fs.StringVar(&opts.BinDir, "bin-dir", "", "directory for Guardian tool shims")
	fs.BoolVar(&opts.List, "list", false, "list catalog tools available on the current platform")
	fs.BoolVar(&opts.Which, "which", false, "print the verified cached executable path for the tool")
	fs.BoolVar(&opts.Verify, "verify", false, "verify digest, admission, and executable availability for the tool")
	fs.BoolVar(&opts.InstallShims, "install-shims", false, "install Guardian tool shims into --bin-dir")
	fs.BoolVar(&opts.Help, "h", false, "show help for guardian run")
	fs.BoolVar(&opts.Help, "help", false, "show help for guardian run")
	flagArgs, operands, err := splitCommandArgs(before)
	if err != nil {
		_ = commandUsageError(stderr, "guardian run", err.Error(), nil)
		return runOptions{}, nil, nil, false, false
	}
	if err := fs.Parse(flagArgs); err != nil {
		_ = commandUsageError(stderr, "guardian run", err.Error(), similarRunFlags(err))
		return runOptions{}, nil, nil, false, false
	}
	operands = append(operands, fs.Args()...)
	opts.Output = strings.TrimSpace(opts.Output)
	return opts, operands, toolArgs, execute, true
}

func splitRunInvocationArgs(args []string) ([]string, []string, bool) {
	for i, arg := range args {
		if arg == "--" {
			return append([]string(nil), args[:i]...), append([]string(nil), args[i+1:]...), true
		}
	}
	return append([]string(nil), args...), nil, false
}

func commandWorkspaceRoot(commandName string, stderr io.Writer) (string, bool) {
	cwd, err := os.Getwd()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: cwd: %v\n", commandName, err)
		return "", false
	}
	workspaceRoot, err := guardianconfig.DiscoverWorkspaceRoot(cwd)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", commandName, err)
		return "", false
	}
	return workspaceRoot, true
}

func runPreflight(args []string, stdout io.Writer, stderr io.Writer) int {
	opts, ok := parseProfileFlags("guardian preflight", args, stderr)
	if !ok {
		return 2
	}
	if opts.Help {
		preflightUsage(stderr)
		return 0
	}
	emitter := eventWriter{enabled: opts.Stream, stderr: stderr}
	doc, ok := resolveDocument("guardian preflight", &opts, stderr)
	if !ok {
		return 1
	}
	result := evaluatePreflight(doc, opts, emitter)
	if err := writeOutput(stdout, opts.Output, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "guardian preflight: %v\n", err)
		return 2
	}
	if hasFalseCondition(result.Conditions) {
		return 1
	}
	return 0
}

func runFly(args []string, stdout io.Writer, stderr io.Writer) int {
	opts, ok := parseProfileFlags("guardian fly", args, stderr)
	if !ok {
		return 2
	}
	if opts.Help {
		flyUsage(stderr)
		return 0
	}
	emitter := eventWriter{enabled: opts.Stream, stderr: stderr}
	doc, ok := resolveDocument("guardian fly", &opts, stderr)
	if !ok {
		return 1
	}
	result := evaluateFly(doc, opts, emitter)
	if err := writeOutput(stdout, opts.Output, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "guardian fly: %v\n", err)
		return 2
	}
	if hasFalseCondition(result.Conditions) {
		return 1
	}
	return 0
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func runProfiles(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) < 1 {
		return commandUsageError(stderr, "guardian profiles", "expected list or show", nil)
	}
	switch args[0] {
	case "list":
		return runProfilesList(args[1:], stdout, stderr)
	case "show":
		return runProfilesShow(args[1:], stdout, stderr)
	case "-h", "--help", "help":
		profilesUsage(stderr)
		return 0
	default:
		return commandUsageError(stderr, "guardian profiles", fmt.Sprintf("unknown command: %s", args[0]), prefixedSuggestions("guardian profiles", args[0], []string{"list", "show"}))
	}
}

func runProfilesList(args []string, stdout io.Writer, stderr io.Writer) int {
	opts, ok := parseProfileFlags("guardian profiles list", args, stderr)
	if !ok {
		return 2
	}
	if opts.Help {
		profilesUsage(stderr)
		return 0
	}
	resolution, err := guardianconfig.ResolveProfile(guardianconfig.ResolveOptions{
		ConfigPath: opts.Config,
		Profile:    opts.Profile,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "guardian profiles list: %v\n", err)
		return 1
	}
	writeWarnings(stderr, resolution.Warnings)
	result := profilesListResult{
		DefaultProfile: resolution.RootConfig.DefaultProfile,
		Profiles:       guardianconfig.ListProfiles(resolution),
	}
	if err := writeOutput(stdout, opts.Output, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "guardian profiles list: %v\n", err)
		return 2
	}
	return 0
}

func runProfilesShow(args []string, stdout io.Writer, stderr io.Writer) int {
	opts, ok := parseProfileFlags("guardian profiles show", args, stderr)
	if !ok {
		return 2
	}
	if opts.Help {
		profilesUsage(stderr)
		return 0
	}
	resolution, err := guardianconfig.ResolveProfile(guardianconfig.ResolveOptions{
		ConfigPath: opts.Config,
		Profile:    opts.Profile,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "guardian profiles show: %v\n", err)
		return 1
	}
	writeWarnings(stderr, resolution.Warnings)
	result := profileShowResult{
		Profile:        resolution.ProfileName,
		Default:        resolution.ProfileName == resolution.RootConfig.DefaultProfile,
		ConfigRoot:     resolution.ConfigRoot,
		RootConfigPath: resolution.RootConfigPath,
		DocumentPath:   resolution.DocumentPath,
	}
	if err := writeOutput(stdout, opts.Output, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "guardian profiles show: %v\n", err)
		return 2
	}
	return 0
}

type profilesListResult struct {
	DefaultProfile string   `json:"default_profile" yaml:"default_profile" toml:"default_profile" toon:"default_profile"`
	Profiles       []string `json:"profiles" yaml:"profiles" toml:"profiles" toon:"profiles"`
}

type profileShowResult struct {
	Profile        string `json:"profile" yaml:"profile" toml:"profile" toon:"profile"`
	Default        bool   `json:"default" yaml:"default" toml:"default" toon:"default"`
	ConfigRoot     string `json:"config_root,omitempty" yaml:"config_root,omitempty" toml:"config_root,omitempty" toon:"config_root,omitempty"`
	RootConfigPath string `json:"root_config_path,omitempty" yaml:"root_config_path,omitempty" toml:"root_config_path,omitempty" toon:"root_config_path,omitempty"`
	DocumentPath   string `json:"document_path" yaml:"document_path" toml:"document_path" toon:"document_path"`
}

func parseProfileFlags(name string, args []string, stderr io.Writer) (commandOptions, bool) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := commandOptions{}
	bindCommonFlags(fs, &opts)
	flagArgs, operands, err := splitCommandArgs(args)
	if err != nil {
		_ = commandUsageError(stderr, name, err.Error(), nil)
		return commandOptions{}, false
	}
	if err := fs.Parse(flagArgs); err != nil {
		_ = commandUsageError(stderr, name, err.Error(), similarCommonFlags(err))
		return commandOptions{}, false
	}
	if err := setPositionalProfile(&opts, operands); err != nil {
		_ = commandUsageError(stderr, name, err.Error(), nil)
		return commandOptions{}, false
	}
	if err := normalizeCommonOptions(&opts); err != nil {
		_ = commandUsageError(stderr, name, err.Error(), nil)
		return commandOptions{}, false
	}
	return opts, true
}

func bindCommonFlags(fs *flag.FlagSet, opts *commandOptions) {
	fs.StringVar(&opts.Config, "f", "", "Guardian config override path")
	fs.StringVar(&opts.Config, "config", "", "Guardian config override path")
	fs.StringVar(&opts.Output, "o", "yaml", "output format: yaml | json | toml | toon")
	fs.StringVar(&opts.Output, "output", "yaml", "output format: yaml | json | toml | toon")
	fs.StringVar(&opts.Output, "format", "yaml", "alias for --output")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "plan without mutating remote state")
	fs.BoolVar(&opts.DryRun, "plan", false, "alias for --dry-run")
	fs.BoolVar(&opts.Stream, "stream", false, "write newline-delimited JSON progress events to stderr")
	fs.BoolVar(&opts.Help, "h", false, "show help")
	fs.BoolVar(&opts.Help, "help", false, "show help")
}

func splitCommandArgs(args []string) ([]string, []string, error) {
	var flagArgs []string
	var operands []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			operands = append(operands, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			operands = append(operands, arg)
			continue
		}
		flagArgs = append(flagArgs, arg)
		if flagRequiresValue(arg) && !strings.Contains(arg, "=") {
			i++
			if i >= len(args) {
				return nil, nil, fmt.Errorf("%s requires a value", arg)
			}
			flagArgs = append(flagArgs, args[i])
		}
	}
	return flagArgs, operands, nil
}

func flagRequiresValue(arg string) bool {
	name := strings.TrimLeft(arg, "-")
	if before, _, ok := strings.Cut(name, "="); ok {
		name = before
	}
	switch name {
	case "f", "config", "format", "o", "output", "bin-dir":
		return true
	default:
		return false
	}
}

func setPositionalProfile(opts *commandOptions, operands []string) error {
	if len(operands) > 1 {
		return fmt.Errorf("expected at most one profile operand, got %d", len(operands))
	}
	if len(operands) == 0 {
		return nil
	}
	opts.Profile = operands[0]
	return nil
}

func normalizeCommonOptions(opts *commandOptions) error {
	opts.Output = strings.TrimSpace(opts.Output)
	return nil
}

func resolveDocument(commandName string, opts *commandOptions, stderr io.Writer) (guardianDocument, bool) {
	resolution, err := guardianconfig.ResolveProfile(guardianconfig.ResolveOptions{
		ConfigPath: opts.Config,
		Profile:    opts.Profile,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", commandName, err)
		return guardianDocument{}, false
	}
	writeWarnings(stderr, resolution.Warnings)
	opts.WorkspaceRoot = resolution.WorkspaceRoot
	opts.Profile = resolution.ProfileName
	opts.Config = resolution.DocumentPath
	doc, err := loadDocument(resolution.DocumentPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", commandName, err)
		return guardianDocument{}, false
	}
	return doc, true
}

func writeWarnings(stderr io.Writer, warnings []string) {
	for _, warning := range warnings {
		_, _ = fmt.Fprintln(stderr, warning)
	}
}

func loadDocument(path string) (guardianDocument, error) {
	if strings.EqualFold(filepath.Ext(path), ".cue") {
		return loadCUEDocument(path)
	}
	var doc specdoc.Document
	if err := formatio.DecodeFile(path, &doc); err != nil {
		return guardianDocument{}, fmt.Errorf("load document: %w", err)
	}
	compiled, err := specdoc.Compile(doc)
	if err != nil {
		return guardianDocument{}, err
	}
	return guardianDocument{Source: doc, Compiled: compiled}, nil
}

func loadCUEDocument(path string) (guardianDocument, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return guardianDocument{}, fmt.Errorf("resolve CUE path: %w", err)
	}
	moduleRoot, err := findCUEModuleRoot(filepath.Dir(abs))
	if err != nil {
		return guardianDocument{}, err
	}
	cfg := &load.Config{
		Dir:        filepath.Dir(abs),
		ModuleRoot: moduleRoot,
	}
	instances := load.Instances([]string{abs}, cfg)
	if len(instances) != 1 {
		return guardianDocument{}, fmt.Errorf("load CUE: expected one instance, got %d", len(instances))
	}
	instance := instances[0]
	if instance.Err != nil {
		return guardianDocument{}, fmt.Errorf("load CUE: %w", instance.Err)
	}
	if instance.Incomplete {
		return guardianDocument{}, fmt.Errorf("load CUE: incomplete instance")
	}
	value := cuecontext.New().BuildInstance(instance)
	if err := value.Err(); err != nil {
		return guardianDocument{}, fmt.Errorf("build CUE: %w", err)
	}
	if err := value.Validate(cue.Concrete(true)); err != nil {
		return guardianDocument{}, fmt.Errorf("CUE document is not concrete: %w", err)
	}
	data, err := value.MarshalJSON()
	if err != nil {
		return guardianDocument{}, fmt.Errorf("marshal CUE document: %w", err)
	}
	var doc specdoc.Document
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return guardianDocument{}, fmt.Errorf("decode CUE document: %w", err)
	}
	compiled, err := specdoc.Compile(doc)
	if err != nil {
		return guardianDocument{}, err
	}
	return guardianDocument{Source: doc, Compiled: compiled}, nil
}

func findCUEModuleRoot(start string) (string, error) {
	dir := start
	for {
		if stat, err := os.Stat(filepath.Join(dir, "cue.mod")); err == nil && stat.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

func evaluatePreflight(doc guardianDocument, opts commandOptions, emitter eventWriter) preflightResult {
	emitter.emit("preflight.load", "ok", "", "loaded Guardian config document")
	mode := "preflight"
	if opts.DryRun {
		mode = "dry_run"
	}
	resourceDigest := digestValue(doc.Source.Resources)
	preflight := doc.Compiled.FlySpec.Preflight
	result := preflightResult{
		Profile:        opts.Profile,
		Name:           doc.Compiled.Fly.Metadata.Name,
		Status:         "blocked",
		ReadyToFly:     "no",
		ExecutionMode:  mode,
		ResourceDigest: resourceDigest,
		Entrypoint:     refResult(doc.Source.Entrypoint),
		Preflight:      ansiblePending(preflight),
		Upload: uploadResult{
			Status: "pending",
			Reason: "NotStarted",
		},
	}
	result.Conditions = append(result.Conditions, conditionTrue("ProfileLoaded", "ProfileResolved", "Guardian profile resolved to a resource graph", "profile."+opts.Profile))
	result.Conditions = append(result.Conditions, conditionTrue("ResourceGraphResolved", "RefsResolved", "Guardian resource refs resolved", "resources"))
	result.Conditions = append(result.Conditions, conditionTrue("PreflightConfigured", "AnsiblePlaybookConfigured", "preflight Ansible playbook is configured", "preflight"))
	if err := writeFlyDocument(opts.WorkspaceRoot, doc.Source); err != nil {
		result.Upload.Status = "blocked"
		result.Upload.Reason = "ResourceGraphWriteFailed"
		result.Conditions = append(result.Conditions, conditionFalse("FlyDocumentMaterialized", "WriteFailed", err.Error(), "preflight.resources"))
		return result
	}
	result.Conditions = append(result.Conditions, conditionTrue("FlyDocumentMaterialized", "DocumentWritten", "Guardian resource graph was written for component-owned Nomad tasks", "preflight.resources"))
	if err := preparePreflightWorkspace(opts.WorkspaceRoot); err != nil {
		result.Upload.Status = "blocked"
		result.Upload.Reason = "BuildArtifactsMissing"
		result.Conditions = append(result.Conditions, conditionFalse("LocalArtifactsPresent", "BuildArtifactsMissing", err.Error(), "preflight.upload"))
		return result
	}
	result.Upload.Status = "prepared"
	result.Upload.Reason = "WorkspaceReady"
	result.Conditions = append(result.Conditions, conditionTrue("LocalArtifactsPresent", "WorkspaceReady", "workspace graph and materialized Bazel outputs are present locally", "preflight.upload"))
	if hasFalseCondition(result.Conditions) {
		return result
	}
	if opts.DryRun {
		result.Status = "ready"
		result.Conditions = append(result.Conditions, conditionTrue("PreflightReady", "DryRun", "dry run verified local preflight inputs without mutating the target", "preflight"))
		return result
	}
	preflightRun, err := runPreflightPlaybook(doc, opts, resourceDigest, emitter)
	result.Preflight = preflightRun
	if err != nil {
		result.Upload.Status = "blocked"
		result.Upload.Reason = "PreflightFailed"
		result.Conditions = append(result.Conditions, conditionFalse("PreflightReady", preflightRun.Reason, hookFailureMessage("preflight playbook failed", hookResult{Status: preflightRun.Status, Reason: preflightRun.Reason, Message: preflightRun.Message}), "preflight"))
		return result
	}
	result.Upload.Status = "ready"
	result.Upload.Reason = "AnsibleVerified"
	result.ReadyToFly = "yes"
	result.Status = "ready"
	result.Conditions = append(result.Conditions, conditionTrue("RemoteTreeVerified", "AnsibleVerified", "preflight playbook verified the materialized repo tree", "preflight.upload"))
	result.Conditions = append(result.Conditions, conditionTrue("ReadyToFly", "PreflightComplete", "preflight completed successfully", "preflight"))
	return result
}

func ansiblePending(preflight specdoc.Preflight) ansibleResult {
	return ansibleResult{Playbook: preflight.Ansible.Playbook, Status: "pending", Reason: "NotStarted"}
}

type ansibleSSHTarget struct {
	Host       string
	User       string
	Port       string
	CommonArgs []string
	Executable string
	Target     string
}

func runPreflightPlaybook(doc guardianDocument, opts commandOptions, resourceDigest string, emitter eventWriter) (ansibleResult, error) {
	preflight := doc.Compiled.FlySpec.Preflight
	result := ansibleResult{Playbook: preflight.Ansible.Playbook, Status: "blocked", Reason: "AnsibleFailed"}
	playbookPath, err := workspaceRelativePath(opts.WorkspaceRoot, preflight.Ansible.Playbook)
	if err != nil {
		result.Reason = "PlaybookPathInvalid"
		result.Message = err.Error()
		return result, err
	}
	uvxPath := filepath.Join(opts.WorkspaceRoot, filepath.FromSlash(defaultBazelBuildRoot+"/bazel-bin/src/tools/dev/binaries/uvx"))
	if _, err := os.Stat(uvxPath); err != nil {
		result.Reason = "AnsibleRunnerMissing"
		result.Message = defaultBazelBuildRoot + "/bazel-bin/src/tools/dev/binaries/uvx is missing; run guardian run bazel -- build //src/tools/dev/binaries:uv_tools before preflight"
		return result, fmt.Errorf("%s: %w", result.Message, err)
	}
	target, err := parseAnsibleSSHTarget(doc.Compiled.SubstrateSpec.Remote.SSH)
	if err != nil {
		result.Reason = "SubstrateSSHInvalid"
		result.Message = err.Error()
		return result, err
	}
	tmpDir, err := os.MkdirTemp("", "guardian-preflight-*")
	if err != nil {
		result.Reason = "TempDirFailed"
		result.Message = err.Error()
		return result, err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	inventoryPath := filepath.Join(tmpDir, "inventory.ini")
	if err := os.WriteFile(inventoryPath, []byte(ansibleInventory(doc.Compiled.Substrate.Metadata.Name, target)), 0o600); err != nil {
		result.Reason = "InventoryWriteFailed"
		result.Message = err.Error()
		return result, err
	}
	varsPath := filepath.Join(tmpDir, "vars.json")
	vars, err := ansibleVars(doc, opts, resourceDigest, target, tmpDir)
	if err != nil {
		result.Reason = "VarsBuildFailed"
		result.Message = err.Error()
		return result, err
	}
	varsData, err := json.MarshalIndent(vars, "", "  ")
	if err != nil {
		result.Reason = "VarsEncodeFailed"
		result.Message = err.Error()
		return result, err
	}
	varsData = append(varsData, '\n')
	if err := os.WriteFile(varsPath, varsData, 0o600); err != nil {
		result.Reason = "VarsWriteFailed"
		result.Message = err.Error()
		return result, err
	}
	argv := []string{
		"--from", "ansible-core==2.20.3",
		"ansible-playbook",
		"-i", inventoryPath,
		"--extra-vars", "@" + varsPath,
		playbookPath,
	}
	emitter.emit("preflight.ansible", "start", preflight.Ansible.Playbook, "running preflight playbook")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, uvxPath, argv...)
	cmd.Dir = opts.WorkspaceRoot
	cmd.Env = append(os.Environ(), "ANSIBLE_RETRY_FILES_ENABLED=False")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		result.Message = commandFailureMessage(err, stdout.Bytes(), stderr.Bytes())
		emitter.emit("preflight.ansible", "blocked", preflight.Ansible.Playbook, result.Message)
		return result, err
	}
	result.Status = "ready"
	result.Reason = "AnsibleSucceeded"
	emitter.emit("preflight.ansible", "ok", preflight.Ansible.Playbook, "preflight playbook completed")
	return result, nil
}

func workspaceRelativePath(workspaceRoot string, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("%s must be workspace-relative", rel)
	}
	path := filepath.Join(workspaceRoot, filepath.FromSlash(rel))
	inside, err := pathIsInside(workspaceRoot, path)
	if err != nil {
		return "", err
	}
	if !inside {
		return "", fmt.Errorf("%s escapes workspace root", rel)
	}
	return path, nil
}

func parseAnsibleSSHTarget(ssh []string) (ansibleSSHTarget, error) {
	if len(ssh) < 2 {
		return ansibleSSHTarget{}, errors.New("substrate remote ssh argv must include ssh and a target")
	}
	target := ansibleSSHTarget{Executable: ssh[0]}
	args := ssh[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-T", "-N":
			continue
		case "-o":
			i++
			if i >= len(args) {
				return ansibleSSHTarget{}, errors.New("ssh -o requires a value")
			}
			target.CommonArgs = append(target.CommonArgs, "-o", args[i])
		case "-p":
			i++
			if i >= len(args) {
				return ansibleSSHTarget{}, errors.New("ssh -p requires a value")
			}
			target.Port = args[i]
		case "-i", "-F", "-J":
			i++
			if i >= len(args) {
				return ansibleSSHTarget{}, fmt.Errorf("ssh %s requires a value", arg)
			}
			target.CommonArgs = append(target.CommonArgs, arg, args[i])
		case "-l":
			i++
			if i >= len(args) {
				return ansibleSSHTarget{}, errors.New("ssh -l requires a value")
			}
			target.User = args[i]
		default:
			if strings.HasPrefix(arg, "-o") && len(arg) > 2 {
				target.CommonArgs = append(target.CommonArgs, arg)
				continue
			}
			if strings.HasPrefix(arg, "-") {
				target.CommonArgs = append(target.CommonArgs, arg)
				continue
			}
			if target.Target != "" {
				return ansibleSSHTarget{}, fmt.Errorf("ssh argv includes more than one target: %s and %s", target.Target, arg)
			}
			target.Target = arg
		}
	}
	if target.Target == "" {
		return ansibleSSHTarget{}, errors.New("ssh argv target is required")
	}
	host := target.Target
	if user, rest, ok := strings.Cut(host, "@"); ok {
		if target.User == "" {
			target.User = user
		}
		host = rest
	}
	if host == "" {
		return ansibleSSHTarget{}, errors.New("ssh target host is empty")
	}
	target.Host = host
	if target.User == "" {
		target.User = "ubuntu"
	}
	return target, nil
}

func ansibleInventory(hostAlias string, target ansibleSSHTarget) string {
	alias := sanitizeInventoryName(hostAlias)
	fields := []string{
		alias,
		"ansible_host=" + inventoryValue(target.Host),
		"ansible_user=" + inventoryValue(target.User),
		"ansible_ssh_executable=" + inventoryValue(target.Executable),
		"ansible_python_interpreter=/usr/bin/python3",
	}
	if target.Port != "" {
		fields = append(fields, "ansible_port="+inventoryValue(target.Port))
	}
	if len(target.CommonArgs) > 0 {
		fields = append(fields, "ansible_ssh_common_args="+inventoryValue(strings.Join(target.CommonArgs, " ")))
	}
	return strings.Join(fields, " ") + "\n"
}

func sanitizeInventoryName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "guardian-substrate"
	}
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	return b.String()
}

func inventoryValue(value string) string {
	if value == "" {
		return "''"
	}
	if strings.ContainsAny(value, " \t\n'\"") {
		return strconv.Quote(value)
	}
	return value
}

func ansibleVars(doc guardianDocument, opts commandOptions, resourceDigest string, target ansibleSSHTarget, tmpDir string) (map[string]any, error) {
	remote := doc.Compiled.SubstrateSpec.Remote
	workspaceState := gitWorkspaceState(opts.WorkspaceRoot)
	return map[string]any{
		"guardian_document":               doc.Source,
		"guardian_entrypoint":             refResult(doc.Source.Entrypoint),
		"guardian_profile":                opts.Profile,
		"guardian_resource_digest":        resourceDigest,
		"guardian_workspace_git_revision": workspaceState.Revision,
		"guardian_workspace_git_clean":    workspaceState.Clean,
		"guardian_workspace_root":         opts.WorkspaceRoot,
		"guardian_fly_document_path":      filepath.Join(opts.WorkspaceRoot, filepath.FromSlash(defaultFlyDocumentPath)),
		"guardian_bazel_manifest_path":    filepath.Join(opts.WorkspaceRoot, filepath.FromSlash(bazelBuildManifestPath)),
		"guardian_bazel_output_root":      filepath.Join(opts.WorkspaceRoot, filepath.FromSlash(defaultBazelBuildRoot+"/bazel-bin")),
		"guardian_preflight_work_dir":     tmpDir,
		"guardian_remote_repo_root":       remote.RepoRoot,
		"guardian_rsync_bin":              filepath.Join(opts.WorkspaceRoot, filepath.FromSlash(defaultBazelBuildRoot+"/bazel-bin/src/guardian-specification/tools/rsync")),
		"guardian_rsync_target":           target.User + "@" + target.Host,
		"guardian_rsync_ssh_command":      ansibleRsyncSSHCommand(target),
		"guardian_substrate_resource":     doc.Compiled.Substrate,
		"guardian_substrate_remote":       remote,
		"guardian_ansible_inventory_host": sanitizeInventoryName(doc.Compiled.Substrate.Metadata.Name),
	}, nil
}

type gitState struct {
	Revision string
	Clean    bool
}

func gitWorkspaceState(workspaceRoot string) gitState {
	revisionOut, err := exec.Command("git", "-C", workspaceRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		return gitState{}
	}
	revision := strings.TrimSpace(string(revisionOut))
	if revision == "" {
		return gitState{}
	}
	statusOut, err := exec.Command("git", "-C", workspaceRoot, "status", "--porcelain", "--untracked-files=no").Output()
	if err != nil {
		return gitState{Revision: revision}
	}
	return gitState{Revision: revision, Clean: strings.TrimSpace(string(statusOut)) == ""}
}

func ansibleRsyncSSHCommand(target ansibleSSHTarget) string {
	argv := []string{target.Executable}
	if target.Port != "" {
		argv = append(argv, "-p", target.Port)
	}
	argv = append(argv, target.CommonArgs...)
	parts := make([]string, 0, len(argv))
	for _, arg := range argv {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func preparePreflightWorkspace(workspaceRoot string) error {
	for _, rel := range []string{
		defaultFlyDocumentPath,
		bazelBuildManifestPath,
		defaultBazelBuildRoot + "/bazel-bin",
		defaultBazelBuildRoot + "/bazel-bin/src/tools/dev/binaries/uvx",
		defaultBazelBuildRoot + "/bazel-bin/src/guardian-specification/tools/rsync",
	} {
		path := filepath.Join(workspaceRoot, filepath.FromSlash(rel))
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%s is missing; run guardian run bazel -- build before preflight", rel)
			}
			return fmt.Errorf("stat %s: %w", rel, err)
		}
	}
	return nil
}

type bazelBuildManifest struct {
	Outputs []bazelBuildOutput `json:"outputs"`
}

type bazelBuildOutput struct {
	Path       string `json:"path"`
	SourceURI  string `json:"source_uri"`
	Digest     string `json:"digest,omitempty"`
	Length     string `json:"length,omitempty"`
	TargetPath string `json:"target_path"`
}

type bazelBuildEvent struct {
	ID struct {
		NamedSet *struct {
			ID string `json:"id"`
		} `json:"namedSet,omitempty"`
	} `json:"id"`
	NamedSetOfFiles *bazelNamedSetOfFiles `json:"namedSetOfFiles,omitempty"`
	Completed       *struct {
		Success     bool               `json:"success"`
		OutputGroup []bazelOutputGroup `json:"outputGroup"`
	} `json:"completed,omitempty"`
}

type bazelNamedSetOfFiles struct {
	Files    []bazelOutputFile `json:"files"`
	FileSets []bazelFileSet    `json:"fileSets"`
}

type bazelOutputGroup struct {
	Name     string         `json:"name"`
	FileSets []bazelFileSet `json:"fileSets"`
}

type bazelFileSet struct {
	ID string `json:"id"`
}

type bazelOutputFile struct {
	Name       string   `json:"name"`
	URI        string   `json:"uri"`
	PathPrefix []string `json:"pathPrefix"`
	Digest     string   `json:"digest"`
	Length     string   `json:"length"`
}

func materializeBazelBuildOutputs(workspaceRoot string, bepPath string) error {
	file, err := os.Open(bepPath)
	if err != nil {
		return fmt.Errorf("open build event file: %w", err)
	}
	defer func() { _ = file.Close() }()
	namedSets := map[string]bazelNamedSetOfFiles{}
	var completedSets []string
	decoder := json.NewDecoder(file)
	for {
		var event bazelBuildEvent
		if err := decoder.Decode(&event); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("decode build event: %w", err)
		}
		if event.ID.NamedSet != nil && event.NamedSetOfFiles != nil {
			namedSets[event.ID.NamedSet.ID] = *event.NamedSetOfFiles
		}
		if event.Completed != nil && event.Completed.Success {
			for _, group := range event.Completed.OutputGroup {
				if group.Name != "default" {
					continue
				}
				for _, set := range group.FileSets {
					if set.ID != "" {
						completedSets = append(completedSets, set.ID)
					}
				}
			}
		}
	}
	if len(completedSets) == 0 {
		return errors.New("Bazel build did not report default output files")
	}
	buildRoot := filepath.Join(workspaceRoot, filepath.FromSlash(defaultBazelBuildRoot))
	materializedRoot := filepath.Join(buildRoot, "bazel-bin")
	if err := os.RemoveAll(materializedRoot); err != nil {
		return fmt.Errorf("clear materialized bazel-bin: %w", err)
	}
	seenFiles := map[string]bool{}
	seenSets := map[string]bool{}
	var manifest bazelBuildManifest
	for _, setID := range completedSets {
		files, err := flattenBazelFileSet(setID, namedSets, seenSets)
		if err != nil {
			return err
		}
		for _, output := range files {
			targetPath, err := bazelOutputTargetPath(output)
			if err != nil {
				return err
			}
			if seenFiles[targetPath] {
				continue
			}
			seenFiles[targetPath] = true
			sourcePath, err := fileURIPath(output.URI)
			if err != nil {
				return err
			}
			destination := filepath.Join(buildRoot, filepath.FromSlash(targetPath))
			if err := copyMaterializedOutput(sourcePath, destination); err != nil {
				return err
			}
			manifest.Outputs = append(manifest.Outputs, bazelBuildOutput{
				Path:       strings.Join(append(append([]string(nil), output.PathPrefix...), output.Name), "/"),
				SourceURI:  output.URI,
				Digest:     output.Digest,
				Length:     output.Length,
				TargetPath: targetPath,
			})
		}
	}
	sort.Slice(manifest.Outputs, func(i, j int) bool {
		return manifest.Outputs[i].TargetPath < manifest.Outputs[j].TargetPath
	})
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode build manifest: %w", err)
	}
	data = append(data, '\n')
	return writeWorkspaceFile(filepath.Join(workspaceRoot, filepath.FromSlash(bazelBuildManifestPath)), data, 0o644)
}

func flattenBazelFileSet(setID string, namedSets map[string]bazelNamedSetOfFiles, seen map[string]bool) ([]bazelOutputFile, error) {
	if seen[setID] {
		return nil, nil
	}
	seen[setID] = true
	set, ok := namedSets[setID]
	if !ok {
		return nil, fmt.Errorf("Bazel build referenced missing file set %s", setID)
	}
	files := append([]bazelOutputFile(nil), set.Files...)
	for _, child := range set.FileSets {
		childFiles, err := flattenBazelFileSet(child.ID, namedSets, seen)
		if err != nil {
			return nil, err
		}
		files = append(files, childFiles...)
	}
	return files, nil
}

func bazelOutputTargetPath(output bazelOutputFile) (string, error) {
	nameParts := strings.Split(output.Name, "/")
	for _, part := range append(append([]string(nil), output.PathPrefix...), nameParts...) {
		if strings.TrimSpace(part) == "" || part == "." || part == ".." || strings.Contains(part, string(filepath.Separator)) || strings.Contains(part, "\x00") {
			return "", fmt.Errorf("Bazel output has invalid path segment %q", part)
		}
	}
	if len(output.PathPrefix) >= 3 && output.PathPrefix[0] == "bazel-out" && output.PathPrefix[len(output.PathPrefix)-1] == "bin" {
		return filepath.ToSlash(filepath.Join(append([]string{"bazel-bin"}, nameParts...)...)), nil
	}
	parts := append([]string(nil), output.PathPrefix...)
	parts = append(parts, nameParts...)
	return filepath.ToSlash(filepath.Join(parts...)), nil
}

func fileURIPath(uri string) (string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("parse output URI %q: %w", uri, err)
	}
	if parsed.Scheme != "file" {
		return "", fmt.Errorf("unsupported Bazel output URI scheme %q", parsed.Scheme)
	}
	if parsed.Path == "" {
		return "", fmt.Errorf("Bazel output URI %q has no path", uri)
	}
	return parsed.Path, nil
}

func copyMaterializedOutput(src string, dst string) error {
	stat, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat Bazel output %s: %w", src, err)
	}
	if stat.IsDir() {
		return copyMaterializedDirectory(src, dst)
	}
	return copyMaterializedFile(src, dst, stat.Mode().Perm())
}

func copyMaterializedDirectory(src string, dst string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("Bazel output %s is not a regular file", path)
		}
		return copyMaterializedFile(path, target, info.Mode().Perm())
	})
}

func copyMaterializedFile(src string, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open Bazel output %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create output directory %s: %w", filepath.Dir(dst), err)
	}
	tmp := dst + ".tmp." + fmt.Sprint(os.Getpid())
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("create materialized output %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("copy materialized output %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close materialized output %s: %w", dst, err)
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("chmod materialized output %s: %w", dst, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("promote materialized output %s: %w", dst, err)
	}
	return nil
}

func writeWorkspaceFile(path string, body []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent directory for %s: %w", path, err)
	}
	tmp := path + ".tmp." + fmt.Sprint(os.Getpid())
	if err := os.WriteFile(tmp, body, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("promote %s: %w", path, err)
	}
	return nil
}

func pathIsInside(root string, candidate string) (bool, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(absRoot, absCandidate)
	if err != nil {
		return false, err
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."), nil
}

func hookFailureMessage(prefix string, result hookResult) string {
	if strings.TrimSpace(result.Message) == "" {
		return prefix
	}
	return prefix + ": " + result.Message
}

func commandFailureMessage(err error, stdout []byte, stderr []byte) string {
	parts := []string{strings.TrimSpace(err.Error())}
	if tail := outputTail(stderr, hookOutputTailBytes); tail != "" {
		parts = append(parts, "stderr: "+tail)
	}
	if tail := outputTail(stdout, hookOutputTailBytes); tail != "" {
		parts = append(parts, "stdout: "+tail)
	}
	return strings.Join(parts, "\n")
}

func outputTail(output []byte, limit int) string {
	trimmed := strings.TrimSpace(string(output))
	if len(trimmed) <= limit {
		return trimmed
	}
	return "..." + trimmed[len(trimmed)-limit:]
}

func refResult(ref specdoc.ObjectRef) resourceRefResult {
	return resourceRefResult{APIVersion: ref.APIVersion, Kind: ref.Kind, Name: ref.Name}
}

func digestValue(value any) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func nomadPending(nomad specdoc.NomadFly) nomadResult {
	result := nomadResult{
		Address:   nomad.Address,
		Namespace: nomad.Namespace,
		Status:    "pending",
		Reason:    "NotStarted",
	}
	for _, job := range nomad.Jobs {
		result.Jobs = append(result.Jobs, nomadJobResult{
			Name:   job.Name,
			Path:   job.Path,
			Status: "pending",
			Reason: "NotStarted",
		})
	}
	return result
}

func evaluateFly(doc guardianDocument, opts commandOptions, emitter eventWriter) flyResult {
	emitter.emit("fly.load", "ok", "", "loaded Guardian config document")
	preflightResult := evaluatePreflight(doc, opts, emitter)
	mode := "apply"
	if opts.DryRun {
		mode = "dry_run"
	}
	result := flyResult{
		Profile:        opts.Profile,
		Name:           doc.Compiled.Fly.Metadata.Name,
		Status:         "blocked",
		ReadyToFly:     "no",
		ExecutionMode:  mode,
		ResourceDigest: preflightResult.ResourceDigest,
		Entrypoint:     preflightResult.Entrypoint,
		Nomad:          nomadPending(doc.Compiled.FlySpec.Nomad),
	}
	result.Conditions = append(result.Conditions, conditionFromPreflight(preflightResult, opts.DryRun))
	preflightReadyForFly := preflightResult.ReadyToFly == "yes" || (opts.DryRun && !hasFalseCondition(preflightResult.Conditions))
	if !preflightReadyForFly {
		return result
	}
	if opts.DryRun {
		result.Nomad.Status = "ready"
		result.Nomad.Reason = "DryRun"
		for i := range result.Nomad.Jobs {
			result.Nomad.Jobs[i].Status = "ready"
			result.Nomad.Jobs[i].Reason = "DryRun"
		}
		result.Conditions = append(result.Conditions, conditionTrue("NomadJobsReady", "DryRun", "dry run did not submit Nomad jobs", "fly.nomad"))
		result.Status = "ready"
		result.ReadyToFly = "yes"
		return result
	}
	nomadResult := runNomadJobs(doc, opts, preflightResult.ResourceDigest, emitter)
	result.Nomad = nomadResult
	if nomadResult.Status != "ready" {
		result.Conditions = append(result.Conditions, conditionFalse("NomadJobsReady", nomadResult.Reason, nomadResult.Message, "fly.nomad"))
		return result
	}
	result.Conditions = append(result.Conditions, conditionTrue("NomadJobsReady", "JobsConverged", "declared Nomad jobs converged", "fly.nomad"))
	result.Status = "ready"
	result.ReadyToFly = "yes"
	return result
}

func runNomadJobs(doc guardianDocument, opts commandOptions, resourceDigest string, emitter eventWriter) nomadResult {
	nomad := doc.Compiled.FlySpec.Nomad
	result := nomadPending(nomad)
	result.Status = "running"
	result.Reason = "SubmittingJobs"
	manifest, err := readBazelBuildManifest(opts.WorkspaceRoot)
	if err != nil {
		result.Status = "blocked"
		result.Reason = "BuildManifestUnreadable"
		result.Message = err.Error()
		return result
	}
	for i, job := range nomad.Jobs {
		emitter.emit("fly.nomad", "start", job.Name, "submitting Nomad job")
		jobResult, err := prepareNomadJobResult(opts.WorkspaceRoot, resourceDigest, manifest, job)
		if err != nil {
			jobResult.Name = job.Name
			jobResult.Path = job.Path
			jobResult.Status = "blocked"
			jobResult.Reason = "LocalInputInvalid"
			jobResult.Message = err.Error()
			result.Jobs[i] = jobResult
			result.Status = "blocked"
			result.Reason = jobResult.Reason
			result.Message = jobResult.Name + ": " + jobResult.Message
			emitter.emit("fly.nomad", "blocked", job.Name, jobResult.Message)
			return result
		}
		result.Jobs[i] = jobResult
		remoteResult, err := runRemoteNomadJob(doc.Compiled.SubstrateSpec.Remote, nomad, jobResult, opts.WorkspaceRoot)
		if err != nil {
			jobResult.Status = "blocked"
			jobResult.Reason = "RemoteNomadFailed"
			jobResult.Message = err.Error()
			result.Jobs[i] = jobResult
			result.Status = "blocked"
			result.Reason = jobResult.Reason
			result.Message = jobResult.Name + ": " + jobResult.Message
			emitter.emit("fly.nomad", "blocked", job.Name, jobResult.Message)
			return result
		}
		result.Jobs[i] = remoteResult
		if remoteResult.Status != "ready" {
			result.Status = "blocked"
			result.Reason = remoteResult.Reason
			result.Message = remoteResult.Name + ": " + remoteResult.Message
			emitter.emit("fly.nomad", "blocked", job.Name, remoteResult.Message)
			return result
		}
		emitter.emit("fly.nomad", "ok", job.Name, "Nomad job converged")
	}
	result.Status = "ready"
	result.Reason = "JobsConverged"
	return result
}

func readBazelBuildManifest(workspaceRoot string) (map[string]bazelBuildOutput, error) {
	path := filepath.Join(workspaceRoot, filepath.FromSlash(bazelBuildManifestPath))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", bazelBuildManifestPath, err)
	}
	var manifest bazelBuildManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode %s: %w", bazelBuildManifestPath, err)
	}
	byTargetPath := map[string]bazelBuildOutput{}
	for _, output := range manifest.Outputs {
		if output.TargetPath == "" {
			continue
		}
		byTargetPath[output.TargetPath] = output
	}
	return byTargetPath, nil
}

func prepareNomadJobResult(workspaceRoot string, resourceDigest string, manifest map[string]bazelBuildOutput, job specdoc.NomadJob) (nomadJobResult, error) {
	localJobPath := filepath.Join(workspaceRoot, filepath.FromSlash(job.Path))
	jobBytes, err := os.ReadFile(localJobPath)
	if err != nil {
		return nomadJobResult{}, fmt.Errorf("read Nomad job %s: %w", job.Path, err)
	}
	h := sha256.New()
	_, _ = h.Write([]byte(resourceDigest))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(job.Name))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(job.Path))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(jobBytes)
	for _, artifactPath := range job.ArtifactPaths {
		output, ok := manifest[artifactPath]
		if !ok {
			return nomadJobResult{}, fmt.Errorf("%s is missing from %s; run guardian run bazel -- build before fly", artifactPath, bazelBuildManifestPath)
		}
		if strings.TrimSpace(output.Digest) == "" {
			return nomadJobResult{}, fmt.Errorf("%s has no digest in %s", artifactPath, bazelBuildManifestPath)
		}
		localArtifactPath := filepath.Join(workspaceRoot, filepath.FromSlash(defaultBazelBuildRoot), filepath.FromSlash(artifactPath))
		if _, err := os.Stat(localArtifactPath); err != nil {
			return nomadJobResult{}, fmt.Errorf("stat materialized artifact %s: %w", artifactPath, err)
		}
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(artifactPath))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(output.Digest))
	}
	return nomadJobResult{
		Name:        job.Name,
		Path:        job.Path,
		InputDigest: "sha256:" + hex.EncodeToString(h.Sum(nil)),
		Status:      "pending",
		Reason:      "NotStarted",
	}, nil
}

func runRemoteNomadJob(remote specdoc.Remote, nomad specdoc.NomadFly, job nomadJobResult, workspaceRoot string) (nomadJobResult, error) {
	if remote.RepoRoot == "" || len(remote.SSH) == 0 {
		return job, errors.New("Substrate.spec.remote.repoRoot and ssh are required for guardian fly")
	}
	script := remoteNomadJobScript(remote.RepoRoot, nomad, job)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	argv := append([]string{}, remote.SSH...)
	argv = append(argv, script)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = workspaceRoot
	cmd.Env = os.Environ()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if out, decErr := decodeNomadJobResult(stdout.Bytes()); decErr == nil && out.Name != "" {
			return out, nil
		}
		return job, errors.New(commandFailureMessage(err, stdout.Bytes(), stderr.Bytes()))
	}
	out, err := decodeNomadJobResult(stdout.Bytes())
	if err != nil {
		return job, fmt.Errorf("decode Nomad job result for %s: %w; stdout: %s", job.Name, err, outputTail(stdout.Bytes(), hookOutputTailBytes))
	}
	return out, nil
}

func decodeNomadJobResult(data []byte) (nomadJobResult, error) {
	var out nomadJobResult
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return nomadJobResult{}, err
	}
	return out, nil
}

func remoteNomadJobScript(repoRoot string, nomad specdoc.NomadFly, job nomadJobResult) string {
	varFile := strings.Join([]string{
		"guardian_repo_root = " + strconv.Quote(repoRoot),
		"guardian_job_input_digest = " + strconv.Quote(job.InputDigest),
		"",
	}, "\n")
	return fmt.Sprintf(`set -eu
nomad=/opt/verself/profile/bin/nomad
job_name=%s
job_path=%s
var_file="$(mktemp)"
trap 'rm -f "$var_file"' EXIT
cat >"$var_file" <<'GUARDIAN_NOMAD_VARS'
%sGUARDIAN_NOMAD_VARS
test -f "$job_path"
export NOMAD_ADDR=%s
export NOMAD_NAMESPACE=%s
"$nomad" job validate -var-file "$var_file" "$job_path" >&2
"$nomad" job run -detach -var-file "$var_file" "$job_path" >&2
python3 - "$nomad" "$job_name" %s "$job_path" "$var_file" %s %s <<'PY'
import json
import subprocess
import sys
import time

nomad, name, result_path, job_file, var_file, input_digest, fallback_type = sys.argv[1:]

def run_json(args):
    raw = subprocess.check_output([nomad, *args], text=True)
    return json.loads(raw)

def inspect_job_type():
    payload = run_json(["job", "inspect", "-json", name])
    job = payload.get("Job") if isinstance(payload, dict) else None
    if not isinstance(job, dict):
        job = payload if isinstance(payload, dict) else {}
    return str(job.get("Type") or fallback_type or "")

def latest_alloc():
    try:
        allocs = run_json(["job", "allocs", "-json", name])
    except subprocess.CalledProcessError:
        return {}
    if not isinstance(allocs, list) or not allocs:
        return {}
    return max(allocs, key=lambda alloc: alloc.get("CreateIndex", 0) if isinstance(alloc, dict) else 0)

def emit(status, reason, message, job_type=""):
    print(json.dumps({
        "name": name,
        "path": result_path,
        "type": job_type,
        "input_digest": input_digest,
        "status": status,
        "reason": reason,
        "message": message,
    }, sort_keys=True))

def retry_failed_allocation(job_type, alloc_id):
    if job_type == "batch":
        subprocess.run([nomad, "job", "stop", "-purge", "-yes", name], check=False, text=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        subprocess.run([nomad, "job", "run", "-detach", "-var-file", var_file, job_file], check=False, text=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        return
    if job_type in {"service", "system"} and alloc_id:
        subprocess.run([nomad, "alloc", "stop", "-detach", "-no-shutdown-delay", alloc_id], check=False, text=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

job_type = ""
last = {}
retried = False
for second in range(601):
    if not job_type:
        job_type = inspect_job_type()
    last = latest_alloc()
    client_status = str(last.get("ClientStatus") or "")
    alloc_id = str(last.get("ID") or "")
    if job_type == "batch" and client_status == "complete":
        emit("ready", "BatchComplete", "latest batch allocation completed", job_type)
        raise SystemExit(0)
    if job_type in {"service", "system"} and client_status == "running":
        emit("ready", "AllocationRunning", "latest allocation is running", job_type)
        raise SystemExit(0)
    if client_status in {"failed", "lost"}:
        if not retried:
            retried = True
            retry_failed_allocation(job_type, alloc_id)
            time.sleep(1)
            continue
        emit("blocked", "AllocationFailed", f"latest allocation {alloc_id} is {client_status}", job_type)
        raise SystemExit(2)
    time.sleep(1)

message = "timed out waiting for allocation"
if last:
    message += ": " + json.dumps({
        "id": last.get("ID"),
        "client_status": last.get("ClientStatus"),
        "desired_status": last.get("DesiredStatus"),
    }, sort_keys=True)
emit("blocked", "WaitTimeout", message, job_type)
raise SystemExit(2)
PY
`, shellQuote(job.Name), shellQuote(repoRoot+"/workspace/"+job.Path), varFile, shellQuote(nomad.Address), shellQuote(nomad.Namespace), shellQuote(job.Path), shellQuote(job.InputDigest), shellQuote(""))
}

func writeFlyDocument(workspaceRoot string, doc specdoc.Document) error {
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode fly document: %w", err)
	}
	body = append(body, '\n')
	path := filepath.Join(workspaceRoot, filepath.FromSlash(defaultFlyDocumentPath))
	inside, err := pathIsInside(workspaceRoot, path)
	if err != nil {
		return err
	}
	if !inside {
		return errors.New("fly document path escaped workspace root")
	}
	return writeWorkspaceFile(path, body, 0o644)
}

func conditionFromPreflight(result preflightResult, dryRun bool) condition {
	if result.ReadyToFly == "yes" {
		return conditionTrue("PreflightReady", "ReadyToFly", "preflight verified the target is ready for fly", "preflight")
	}
	if dryRun && !hasFalseCondition(result.Conditions) {
		return conditionTrue("PreflightReady", "DryRun", "dry run verified local preflight inputs without mutating the target", "preflight")
	}
	return conditionFalse("PreflightReady", "NotReadyToFly", "preflight did not prove the target is ready for fly", "preflight")
}

func hasFalseCondition(conditions []condition) bool {
	for _, cond := range conditions {
		if cond.Status == "False" {
			return true
		}
	}
	return false
}

func conditionTrue(conditionType string, reason string, message string, resource string) condition {
	return condition{Type: conditionType, Status: "True", Reason: reason, Message: message, Resource: resource}
}

func conditionFalse(conditionType string, reason string, message string, resource string) condition {
	return condition{Type: conditionType, Status: "False", Reason: reason, Message: message, Resource: resource}
}

func writeOutput(w io.Writer, format string, value any) error {
	return formatio.Write(w, format, value)
}

func (w eventWriter) emit(phase string, status string, resource string, message string) {
	if !w.enabled {
		return
	}
	event := progressEvent{
		At:       time.Now().UTC().Format(time.RFC3339Nano),
		Phase:    phase,
		Status:   status,
		Resource: resource,
		Message:  message,
	}
	_ = json.NewEncoder(w.stderr).Encode(event)
}

func usage(w io.Writer) {
	rootUsage(w)
}

func rootUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, `guardian

usage:
  guardian run --list [-o yaml|json|toml|toon]
  guardian run --install-shims --bin-dir <dir> [-o yaml|json|toml|toon]
  guardian run <tool> --which [-o yaml|json|toml|toon]
  guardian run <tool> --verify [-o yaml|json|toml|toon]
  guardian run <tool> -- <args...>
  guardian profiles list [-o yaml|json|toml|toon]
  guardian profiles show [profile] [-o yaml|json|toml|toon]
  guardian preflight [-f <config>] [profile] [-o yaml|json|toml|toon] [--dry-run] [--stream]
  guardian fly [-f <config>] [profile] [--dry-run] [-o yaml|json|toml|toon] [--stream]
`)
}

func runUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, `guardian run

usage:
  guardian run --list [-o yaml|json|toml|toon]
  guardian run --install-shims --bin-dir <dir> [-o yaml|json|toml|toon]
  guardian run <tool> --which [-o yaml|json|toml|toon]
  guardian run <tool> --verify [-o yaml|json|toml|toon]
  guardian run <tool> -- <args...>

run resolves repo-declared catalog tools. Management flags inspect or install
verified Guardian shims; execution requires -- before tool arguments so
Guardian flags and tool flags cannot be confused.
`)
}

func profilesUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, `guardian profiles

usage:
  guardian profiles list [-f <config>] [-o yaml|json|toml|toon]
  guardian profiles show [profile] [-f <config>] [-o yaml|json|toml|toon]
`)
}

func preflightUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, `guardian preflight

usage:
  guardian preflight [-f <config>] [profile] [-o yaml|json|toml|toon] [--dry-run] [--stream]

preflight resolves a Guardian profile, writes the generated fly document,
verifies local build artifacts, runs the configured preflight playbook, and
reports whether the target is ready for Nomad-driven fly.
`)
}

func flyUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, `guardian fly

usage:
  guardian fly [-f <config>] [profile] [--dry-run] [-o yaml|json|toml|toon] [--stream]

fly runs the same preflight phase and then runs the FlyProcedure Nomad job hook
from the materialized workspace.
`)
}

func commandUsageError(w io.Writer, command string, message string, suggestions []string) int {
	_, _ = fmt.Fprintf(w, "%s: %s\n", command, message)
	if len(suggestions) > 0 {
		_, _ = fmt.Fprintln(w, "\nDid you mean?")
		for _, suggestion := range suggestions {
			_, _ = fmt.Fprintf(w, "  %s\n", suggestion)
		}
	}
	_, _ = fmt.Fprintln(w)
	printCommandUsage(w, command)
	return 2
}

func printCommandUsage(w io.Writer, command string) {
	if strings.HasPrefix(command, "guardian profiles") {
		profilesUsage(w)
		return
	}
	switch command {
	case "guardian run":
		runUsage(w)
	case "guardian preflight":
		preflightUsage(w)
	case "guardian fly":
		flyUsage(w)
	default:
		rootUsage(w)
	}
}

func rootCommands() []string {
	return []string{"run", "profiles", "preflight", "fly", "help"}
}

func rootCommandSuggestions(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	input := args[0]
	if input == "tool" {
		if len(args) >= 2 {
			switch args[1] {
			case "list":
				return []string{"guardian run --list"}
			case "which":
				if len(args) >= 3 {
					return []string{"guardian run " + args[2] + " --which"}
				}
				return []string{"guardian run <tool> --which"}
			case "verify":
				if len(args) >= 3 {
					return []string{"guardian run " + args[2] + " --verify"}
				}
				return []string{"guardian run <tool> --verify"}
			case "install-shims":
				return []string{"guardian run --install-shims --bin-dir <dir>"}
			}
		}
		return []string{
			"guardian run --list",
			"guardian run <tool> --which",
			"guardian run <tool> --verify",
			"guardian run --install-shims --bin-dir <dir>",
		}
	}
	return prefixedSuggestions("guardian", input, rootCommands())
}

func runOperandSuggestions(operands []string) []string {
	if len(operands) == 0 {
		return nil
	}
	switch operands[0] {
	case "list":
		return []string{"guardian run --list"}
	case "install-shims":
		return []string{"guardian run --install-shims --bin-dir <dir>"}
	case "which":
		if len(operands) >= 2 {
			return []string{"guardian run " + operands[1] + " --which"}
		}
		return []string{"guardian run <tool> --which"}
	case "verify":
		if len(operands) >= 2 {
			return []string{"guardian run " + operands[1] + " --verify"}
		}
		return []string{"guardian run <tool> --verify"}
	default:
		return nil
	}
}

func prefixedSuggestions(prefix string, input string, candidates []string) []string {
	matches := similarTerms(input, candidates)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, prefix+" "+match)
	}
	return out
}

func similarRunFlags(err error) []string {
	name, ok := unknownFlag(err)
	if !ok {
		return nil
	}
	return similarFlags(name, []string{"list", "which", "verify", "install-shims", "bin-dir", "output", "o", "help", "h"})
}

func similarCommonFlags(err error) []string {
	name, ok := unknownFlag(err)
	if !ok {
		return nil
	}
	return similarFlags(name, []string{"f", "config", "output", "o", "format", "dry-run", "plan", "stream", "help", "h"})
}

func unknownFlag(err error) (string, bool) {
	const prefix = "flag provided but not defined: -"
	if err == nil || !strings.HasPrefix(err.Error(), prefix) {
		return "", false
	}
	return strings.TrimLeft(strings.TrimPrefix(err.Error(), prefix), "-"), true
}

func similarFlags(input string, candidates []string) []string {
	matches := similarTerms(input, candidates)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		prefix := "--"
		if len(match) == 1 {
			prefix = "-"
		}
		out = append(out, prefix+match)
	}
	return out
}

func similarTerms(input string, candidates []string) []string {
	input = strings.ToLower(strings.TrimSpace(input))
	type match struct {
		value    string
		distance int
	}
	var matches []match
	for _, candidate := range candidates {
		distance := levenshtein(input, strings.ToLower(candidate))
		if distance <= 2 {
			matches = append(matches, match{value: candidate, distance: distance})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].distance != matches[j].distance {
			return matches[i].distance < matches[j].distance
		}
		return matches[i].value < matches[j].value
	})
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, match.value)
	}
	return out
}

func levenshtein(a string, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return len(b)
	}
	if b == "" {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i, ar := range a {
		curr[0] = i + 1
		for j, br := range b {
			cost := 1
			if ar == br {
				cost = 0
			}
			curr[j+1] = minInt(curr[j]+1, prev[j+1]+1, prev[j]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func minInt(values ...int) int {
	best := values[0]
	for _, value := range values[1:] {
		if value < best {
			best = value
		}
	}
	return best
}
