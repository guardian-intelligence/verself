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
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
)

type guardianDocument struct {
	Source   specdoc.Document
	Compiled specdoc.CompiledDocument
}

type lifecycleHookSpec = specdoc.LifecycleHook

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
	Access         hookResult        `json:"access" yaml:"access" toml:"access" toon:"access"`
	Upload         uploadResult      `json:"upload" yaml:"upload" toml:"upload" toon:"upload"`
	Kernel         kernelResult      `json:"kernel" yaml:"kernel" toml:"kernel" toon:"kernel"`
	Conditions     []condition       `json:"conditions" yaml:"conditions" toml:"conditions" toon:"conditions"`
}

type resourceRefResult struct {
	APIVersion string `json:"apiVersion" yaml:"apiVersion" toml:"apiVersion" toon:"apiVersion"`
	Kind       string `json:"kind" yaml:"kind" toml:"kind" toon:"kind"`
	Name       string `json:"name" yaml:"name" toml:"name" toon:"name"`
}

type uploadResult struct {
	Digest  string     `json:"digest,omitempty" yaml:"digest,omitempty" toml:"digest,omitempty" toon:"digest,omitempty"`
	Run     hookResult `json:"run" yaml:"run" toml:"run" toon:"run"`
	Extract hookResult `json:"extract" yaml:"extract" toml:"extract" toon:"extract"`
	Verify  hookResult `json:"verify" yaml:"verify" toml:"verify" toon:"verify"`
	Status  string     `json:"status" yaml:"status" toml:"status" toon:"status"`
	Reason  string     `json:"reason" yaml:"reason" toml:"reason" toon:"reason"`
}

type hookResult struct {
	Argv    []string `json:"argv" yaml:"argv" toml:"argv" toon:"argv"`
	Status  string   `json:"status" yaml:"status" toml:"status" toon:"status"`
	Reason  string   `json:"reason" yaml:"reason" toml:"reason" toon:"reason"`
	Message string   `json:"message,omitempty" yaml:"message,omitempty" toml:"message,omitempty" toon:"message,omitempty"`
}

type kernelResult struct {
	OpenBaoPrepare hookResult `json:"openbao_prepare" yaml:"openbao_prepare" toml:"openbao_prepare" toon:"openbao_prepare"`
	Nomad          hookResult `json:"nomad" yaml:"nomad" toml:"nomad" toon:"nomad"`
	Verify         hookResult `json:"verify" yaml:"verify" toml:"verify" toon:"verify"`
	Status         string     `json:"status" yaml:"status" toml:"status" toon:"status"`
	Reason         string     `json:"reason" yaml:"reason" toml:"reason" toon:"reason"`
}

type flyResult struct {
	Profile        string            `json:"profile" yaml:"profile" toml:"profile" toon:"profile"`
	Name           string            `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty" toon:"name,omitempty"`
	Status         string            `json:"status" yaml:"status" toml:"status" toon:"status"`
	ReadyToFly     string            `json:"ready_to_fly" yaml:"ready_to_fly" toml:"ready_to_fly" toon:"ready_to_fly"`
	ExecutionMode  string            `json:"execution_mode" yaml:"execution_mode" toml:"execution_mode" toon:"execution_mode"`
	ResourceDigest string            `json:"resource_digest,omitempty" yaml:"resource_digest,omitempty" toml:"resource_digest,omitempty" toon:"resource_digest,omitempty"`
	UploadDigest   string            `json:"upload_digest,omitempty" yaml:"upload_digest,omitempty" toml:"upload_digest,omitempty" toon:"upload_digest,omitempty"`
	Entrypoint     resourceRefResult `json:"entrypoint" yaml:"entrypoint" toml:"entrypoint" toon:"entrypoint"`
	Nomad          hookResult        `json:"nomad" yaml:"nomad" toml:"nomad" toon:"nomad"`
	Conditions     []condition       `json:"conditions" yaml:"conditions" toml:"conditions" toon:"conditions"`
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
		return runFlyCommand(args[1:], stdout, stderr)
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
	return toolrun.Exec(context.Background(), tool, toolArgs, toolrun.Options{
		WorkDir: workspaceRoot,
		Stdout:  stdout,
		Stderr:  stderr,
	})
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

func runFlyCommand(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
		flyUsage(stderr)
		return 0
	}
	if len(args) > 0 && args[0] == "run" {
		return runFlyRun(args[1:], stdout, stderr)
	}
	return runFly(args, stdout, stderr)
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

func runFlyRun(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
		flyRunUsage(stderr)
		return 0
	}
	before, remoteArgs, ok := splitFlyRunArgs(args)
	if !ok {
		return commandUsageError(stderr, "guardian fly run", "expected -- before remote tool", nil)
	}
	if len(remoteArgs) == 0 {
		return commandUsageError(stderr, "guardian fly run", "remote tool is required", nil)
	}
	opts, ok := parseProfileFlags("guardian fly run", before, stderr)
	if !ok {
		return 2
	}
	if opts.Help {
		flyRunUsage(stderr)
		return 0
	}
	emitter := eventWriter{enabled: opts.Stream, stderr: stderr}
	doc, ok := resolveDocument("guardian fly run", &opts, stderr)
	if !ok {
		return 1
	}
	fly := evaluateFly(doc, opts, emitter)
	if hasFalseCondition(fly.Conditions) || fly.Status != "ready" {
		_, _ = fmt.Fprintf(stderr, "guardian fly run: fly did not converge for profile %q\n", opts.Profile)
		return 1
	}
	toolName := remoteArgs[0]
	if _, err := toolcatalog.Resolve(opts.WorkspaceRoot, toolName); err != nil {
		_, _ = fmt.Fprintf(stderr, "guardian fly run: %v\n", err)
		return 1
	}
	return runRemoteGuardianTool(doc.Compiled.SubstrateSpec.Remote, toolName, remoteArgs[1:], stdout, stderr)
}

func splitFlyRunArgs(args []string) ([]string, []string, bool) {
	for i, arg := range args {
		if arg == "--" {
			return append([]string(nil), args[:i]...), append([]string(nil), args[i+1:]...), true
		}
	}
	return nil, nil, false
}

func runRemoteGuardianTool(remote specdoc.Remote, toolName string, toolArgs []string, stdout io.Writer, stderr io.Writer) int {
	if len(remote.SSH) == 0 || strings.TrimSpace(remote.Guardian) == "" || strings.TrimSpace(remote.RepoRoot) == "" {
		_, _ = fmt.Fprintln(stderr, "guardian fly run: substrate remote guardian is not configured")
		return 1
	}
	verify := remoteGuardianCommand(remote, []string{"run", toolName, "--verify", "-o", "json"})
	if output, err := execRemoteCommand(context.Background(), remote.SSH, verify, nil, nil); err != nil {
		_, _ = fmt.Fprintf(stderr, "guardian fly run: remote tool verification failed: %v\n%s", err, outputTail(output, 1600))
		return 1
	}
	runArgs := append([]string{"run", toolName, "--"}, toolArgs...)
	command := remoteGuardianCommand(remote, runArgs)
	_, err := execRemoteCommand(context.Background(), remote.SSH, command, stdout, stderr)
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	_, _ = fmt.Fprintf(stderr, "guardian fly run: remote command failed: %v\n", err)
	return 1
}

func remoteGuardianCommand(remote specdoc.Remote, guardianArgs []string) string {
	workspace := filepath.Join(remote.RepoRoot, "workspace")
	parts := []string{"cd", shellQuote(workspace), "&&", "exec", shellQuote(remote.Guardian)}
	for _, arg := range guardianArgs {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func execRemoteCommand(ctx context.Context, ssh []string, remoteCommand string, stdout io.Writer, stderr io.Writer) ([]byte, error) {
	if len(ssh) == 0 {
		return nil, errors.New("ssh argv is empty")
	}
	argv := append(append([]string(nil), ssh[1:]...), remoteCommand)
	cmd := exec.CommandContext(ctx, ssh[0], argv...)
	if stdout != nil || stderr != nil {
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		return nil, cmd.Run()
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	return output.Bytes(), err
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
	substrateSpec := doc.Compiled.SubstrateSpec
	result := preflightResult{
		Profile:        opts.Profile,
		Name:           doc.Compiled.Fly.Metadata.Name,
		Status:         "blocked",
		ReadyToFly:     "no",
		ExecutionMode:  mode,
		ResourceDigest: digestValue(doc.Source.Resources),
		Entrypoint:     refResult(doc.Source.Entrypoint),
		Access:         hookPending(substrateSpec.Access),
		Upload: uploadResult{
			Run:     hookPending(substrateSpec.Upload.Run),
			Extract: hookPending(substrateSpec.Upload.Extract),
			Verify:  hookPending(substrateSpec.Upload.Verify),
			Status:  "pending",
			Reason:  "NotStarted",
		},
		Kernel: kernelPending(substrateSpec.Kernel),
	}
	result.Conditions = append(result.Conditions, conditionTrue("ProfileLoaded", "ProfileResolved", "Guardian profile resolved to a resource graph", "profile."+opts.Profile))
	result.Conditions = append(result.Conditions, conditionTrue("ResourceGraphResolved", "RefsResolved", "Guardian resource refs resolved", "resources"))
	result.Conditions = append(result.Conditions, conditionTrue("AccessHookConfigured", "HookConfigured", "access lifecycle hook is configured", "preflight.access"))
	result.Conditions = append(result.Conditions, conditionTrue("UploadHooksConfigured", "HooksConfigured", "upload lifecycle hooks are configured", "preflight.upload"))
	result.Conditions = append(result.Conditions, conditionTrue("KernelHooksConfigured", "HooksConfigured", "Nomad executor kernel hooks are configured", "preflight.kernel"))
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
	result.Conditions = append(result.Conditions, conditionTrue("LocalArtifactsPresent", "WorkspaceReady", "workspace graph and build artifacts are present locally", "preflight.upload"))
	if hasFalseCondition(result.Conditions) {
		return result
	}
	if opts.DryRun {
		result.Status = "ready"
		result.Conditions = append(result.Conditions, conditionTrue("SubstrateConnected", "DryRun", "dry run did not execute the access hook", "preflight.access"))
		result.Conditions = append(result.Conditions, conditionTrue("RemoteTreeMaterialized", "DryRun", "dry run did not materialize the workspace on the target", "preflight.upload.extract"))
		result.Conditions = append(result.Conditions, conditionTrue("RemoteTreeVerified", "DryRun", "dry run verified local preflight inputs without mutating the target", "preflight.upload.verify"))
		result.Conditions = append(result.Conditions, conditionTrue("KernelReady", "DryRun", "dry run did not execute kernel recovery hooks", "preflight.kernel"))
		return result
	}
	accessResult, _ := runLifecycleHook("preflight.access", substrateSpec.Access, opts.WorkspaceRoot, emitter)
	result.Access = accessResult
	if accessResult.Status != "ready" {
		result.Upload.Status = "blocked"
		result.Upload.Reason = "AccessHookFailed"
		result.Conditions = append(result.Conditions, conditionFalse("SubstrateConnected", accessResult.Reason, hookFailureMessage("access hook failed", accessResult), "preflight.access"))
		return result
	}
	result.Conditions = append(result.Conditions, conditionTrue("SubstrateConnected", "HookSucceeded", "access hook completed", "preflight.access"))
	runResult, _ := runLifecycleHook("preflight.upload.run", substrateSpec.Upload.Run, opts.WorkspaceRoot, emitter)
	result.Upload.Run = runResult
	if runResult.Status != "ready" {
		result.Upload.Status = "blocked"
		result.Upload.Reason = "UploadHookFailed"
		result.Conditions = append(result.Conditions, conditionFalse("RemoteTreeMaterialized", runResult.Reason, hookFailureMessage("upload run hook failed", runResult), "preflight.upload.run"))
		return result
	}
	extractResult, _ := runLifecycleHook("preflight.upload.extract", substrateSpec.Upload.Extract, opts.WorkspaceRoot, emitter)
	result.Upload.Extract = extractResult
	if extractResult.Status != "ready" {
		result.Upload.Status = "blocked"
		result.Upload.Reason = "ExtractHookFailed"
		result.Conditions = append(result.Conditions, conditionFalse("RemoteTreeMaterialized", extractResult.Reason, hookFailureMessage("upload extract hook failed", extractResult), "preflight.upload.extract"))
		return result
	}
	result.Conditions = append(result.Conditions, conditionTrue("RemoteTreeMaterialized", "HookSucceeded", "remote repo tree was materialized", "preflight.upload.extract"))
	verifyResult, verifyStdout := runLifecycleHook("preflight.upload.verify", substrateSpec.Upload.Verify, opts.WorkspaceRoot, emitter)
	result.Upload.Verify = verifyResult
	if verifyResult.Status != "ready" {
		result.Upload.Status = "blocked"
		result.Upload.Reason = "VerifyHookFailed"
		result.Conditions = append(result.Conditions, conditionFalse("RemoteTreeVerified", verifyResult.Reason, hookFailureMessage("upload verify hook failed", verifyResult), "preflight.upload.verify"))
		return result
	}
	observedDigest, err := extractObservedDigest(string(verifyStdout))
	if err != nil {
		result.Upload.Status = "blocked"
		result.Upload.Reason = "DigestMissing"
		result.Conditions = append(result.Conditions, conditionFalse("RemoteTreeVerified", "DigestMissing", err.Error(), "preflight.upload.verify"))
		return result
	}
	result.Upload.Digest = observedDigest
	result.Upload.Status = "ready"
	result.Upload.Reason = "TreeVerified"
	result.Conditions = append(result.Conditions, conditionTrue("RemoteTreeVerified", "TreeVerified", "verify hook proved the remote workspace tree and printed its digest", "preflight.upload.verify"))
	result.Conditions = append(result.Conditions, conditionTrue("RemoteGuardianVerified", "TreeVerified", "remote Guardian artifacts were verified as part of the repo tree", "preflight.upload.verify"))
	if !runKernelHooks(&result, substrateSpec.Kernel, opts.WorkspaceRoot, emitter) {
		return result
	}
	result.Conditions = append(result.Conditions, conditionTrue("KernelVerified", "KernelBootstrapped", "Nomad is running with OpenBao integration inputs available", "preflight.kernel"))
	result.Conditions = append(result.Conditions, conditionTrue("ReadyToFly", "KernelBootstrapped", "Nomad can run component-owned recovery jobs", "preflight.kernel"))
	result.Status = "ready"
	result.ReadyToFly = "yes"
	return result
}

func hookPending(hook lifecycleHookSpec) hookResult {
	return hookResult{Argv: hook.Argv, Status: "pending", Reason: "NotStarted"}
}

func kernelPending(kernel specdoc.Kernel) kernelResult {
	return kernelResult{
		OpenBaoPrepare: hookPending(kernel.OpenBaoPrepare),
		Nomad:          hookPending(kernel.Nomad),
		Verify:         hookPending(kernel.Verify),
		Status:         "pending",
		Reason:         "NotStarted",
	}
}

func runKernelHooks(result *preflightResult, kernel specdoc.Kernel, workspaceRoot string, emitter eventWriter) bool {
	result.Kernel.Status = "running"
	result.Kernel.Reason = "KernelRecoveryStarted"
	steps := []struct {
		name      string
		resource  string
		hook      lifecycleHookSpec
		assign    func(hookResult)
		condition string
		message   string
	}{
		{
			name:      "preflight.kernel.openbao_prepare",
			resource:  "preflight.kernel.openbao_prepare",
			hook:      kernel.OpenBaoPrepare,
			assign:    func(h hookResult) { result.Kernel.OpenBaoPrepare = h },
			condition: "OpenBaoInputsPrepared",
			message:   "OpenBao runtime and CA are prepared before Nomad starts",
		},
		{
			name:      "preflight.kernel.nomad",
			resource:  "preflight.kernel.nomad",
			hook:      kernel.Nomad,
			assign:    func(h hookResult) { result.Kernel.Nomad = h },
			condition: "NomadActive",
			message:   "Nomad agent is running with OpenBao integration available",
		},
		{
			name:      "preflight.kernel.verify",
			resource:  "preflight.kernel.verify",
			hook:      kernel.Verify,
			assign:    func(h hookResult) { result.Kernel.Verify = h },
			condition: "KernelVerified",
			message:   "kernel recovery verification passed",
		},
	}
	for _, step := range steps {
		hookResult, _ := runLifecycleHook(step.name, step.hook, workspaceRoot, emitter)
		step.assign(hookResult)
		if hookResult.Status != "ready" {
			result.Kernel.Status = "blocked"
			result.Kernel.Reason = hookResult.Reason
			result.Conditions = append(result.Conditions, conditionFalse(step.condition, hookResult.Reason, hookFailureMessage("kernel hook failed", hookResult), step.resource))
			return false
		}
		result.Conditions = append(result.Conditions, conditionTrue(step.condition, "HookSucceeded", step.message, step.resource))
	}
	result.Kernel.Status = "ready"
	result.Kernel.Reason = "KernelBootstrapped"
	return true
}

func preparePreflightWorkspace(workspaceRoot string) error {
	for _, rel := range []string{
		defaultFlyDocumentPath,
		"bazel-bin",
	} {
		path := filepath.Join(workspaceRoot, filepath.FromSlash(rel))
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%s is missing; build the repo before preflight", rel)
			}
			return fmt.Errorf("stat %s: %w", rel, err)
		}
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

func runLifecycleHook(name string, hook lifecycleHookSpec, workspaceRoot string, emitter eventWriter) (hookResult, []byte) {
	result := hookResult{Argv: hook.Argv, Status: "blocked", Reason: "HookFailed"}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	emitter.emit(name, "start", "", "running lifecycle hook")
	stdout, stderr, err := execHook(ctx, hook, workspaceRoot)
	if err != nil {
		result.Reason = "HookFailed"
		result.Message = commandFailureMessage(err, stdout, stderr)
		emitter.emit(name, "blocked", "", result.Message)
		return result, stdout
	}
	result.Status = "ready"
	result.Reason = "HookSucceeded"
	emitter.emit(name, "ok", "", "lifecycle hook completed")
	return result, stdout
}

func execHook(ctx context.Context, hook lifecycleHookSpec, workspaceRoot string) ([]byte, []byte, error) {
	if len(hook.Argv) == 0 {
		return nil, nil, errors.New("hook argv is empty")
	}
	cmd := exec.CommandContext(ctx, hook.Argv[0], hook.Argv[1:]...)
	cmd.Dir = workspaceRoot
	cmd.Env = os.Environ()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func hookFailureMessage(prefix string, result hookResult) string {
	if strings.TrimSpace(result.Message) == "" {
		return prefix
	}
	return prefix + ": " + result.Message
}

func commandFailureMessage(err error, stdout []byte, stderr []byte) string {
	parts := []string{strings.TrimSpace(err.Error())}
	if tail := outputTail(stderr, 1600); tail != "" {
		parts = append(parts, "stderr: "+tail)
	}
	if tail := outputTail(stdout, 1600); tail != "" {
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

func extractObservedDigest(output string) (string, error) {
	var decoded map[string]any
	if err := json.Unmarshal([]byte(output), &decoded); err == nil {
		for _, key := range []string{"observed_digest", "digest", "upload_digest", "sha256"} {
			if value, ok := decoded[key].(string); ok {
				return normalizeDigest(value)
			}
		}
	}
	for _, token := range strings.Fields(output) {
		if digest, err := normalizeDigest(token); err == nil {
			return digest, nil
		}
	}
	return "", errors.New("verify hook output did not contain a sha256 digest")
}

func normalizeDigest(value string) (string, error) {
	digest := strings.TrimSpace(value)
	digest = strings.Trim(digest, `"'`)
	if len(digest) == 64 {
		digest = "sha256:" + digest
	}
	if len(digest) != len("sha256:")+64 || !strings.HasPrefix(digest, "sha256:") {
		return "", fmt.Errorf("not a sha256 digest: %q", value)
	}
	for _, r := range strings.TrimPrefix(digest, "sha256:") {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return "", fmt.Errorf("not a lowercase sha256 digest: %q", value)
		}
	}
	return digest, nil
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
		UploadDigest:   preflightResult.Upload.Digest,
		Entrypoint:     preflightResult.Entrypoint,
		Nomad:          hookPending(doc.Compiled.FlySpec.Nomad.Run),
	}
	result.Conditions = append(result.Conditions, conditionFromPreflight(preflightResult, opts.DryRun))
	preflightReadyForFly := preflightResult.ReadyToFly == "yes" || (opts.DryRun && !hasFalseCondition(preflightResult.Conditions))
	if !preflightReadyForFly {
		return result
	}
	if opts.DryRun {
		result.Conditions = append(result.Conditions, conditionTrue("NomadJobReady", "DryRun", "dry run did not submit a Nomad job", "fly.nomad"))
		result.Status = "ready"
		result.ReadyToFly = "yes"
		return result
	}
	nomadResult, _ := runLifecycleHook("fly.nomad.run", doc.Compiled.FlySpec.Nomad.Run, opts.WorkspaceRoot, emitter)
	result.Nomad = nomadResult
	if nomadResult.Status != "ready" {
		result.Conditions = append(result.Conditions, conditionFalse("NomadJobReady", nomadResult.Reason, hookFailureMessage("Nomad job hook failed", nomadResult), "fly.nomad"))
		return result
	}
	result.Conditions = append(result.Conditions, conditionTrue("NomadJobReady", "HookSucceeded", "Nomad job hook completed", "fly.nomad"))
	result.Status = "ready"
	result.ReadyToFly = "yes"
	return result
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
		return conditionTrue("PreflightReady", "ReadyToFly", "remote repo tree and kernel prerequisites are verified", "preflight")
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
  guardian fly run [-f <config>] [profile] -- <tool> <args...>
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
verifies local build artifacts, runs the Substrate access, upload, and kernel
lifecycle hooks, and reports whether the target is ready for Nomad-driven fly.
`)
}

func flyUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, `guardian fly

usage:
  guardian fly [-f <config>] [profile] [--dry-run] [-o yaml|json|toml|toon] [--stream]
  guardian fly run [-f <config>] [profile] -- <tool> <args...>

fly runs the same preflight phase and then runs the FlyProcedure Nomad job hook
from the materialized workspace.
`)
}

func flyRunUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, `guardian fly run

usage:
  guardian fly run [-f <config>] [profile] -- <tool> <args...>

fly run converges the profile first, verifies the remote Guardian catalog tool,
and then executes that catalog tool on the remote host.
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
	case "guardian fly", "guardian fly run":
		if command == "guardian fly run" {
			flyRunUsage(w)
			return
		}
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
