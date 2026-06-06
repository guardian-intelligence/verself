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
	"strings"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
	"github.com/verself/guardian-specification/internal/formatio"
	"github.com/verself/guardian-specification/internal/specdoc"
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

type boardResult struct {
	Name           string            `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty" toon:"name,omitempty"`
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
	Argv   []string `json:"argv" yaml:"argv" toml:"argv" toon:"argv"`
	Status string   `json:"status" yaml:"status" toml:"status" toon:"status"`
	Reason string   `json:"reason" yaml:"reason" toml:"reason" toon:"reason"`
}

type kernelResult struct {
	OpenBaoPrepare hookResult `json:"openbao_prepare" yaml:"openbao_prepare" toml:"openbao_prepare" toon:"openbao_prepare"`
	Nomad          hookResult `json:"nomad" yaml:"nomad" toml:"nomad" toon:"nomad"`
	Verify         hookResult `json:"verify" yaml:"verify" toml:"verify" toon:"verify"`
	Status         string     `json:"status" yaml:"status" toml:"status" toon:"status"`
	Reason         string     `json:"reason" yaml:"reason" toml:"reason" toon:"reason"`
}

type flyResult struct {
	Name           string            `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty" toon:"name,omitempty"`
	ReadyToFly     string            `json:"ready_to_fly" yaml:"ready_to_fly" toml:"ready_to_fly" toon:"ready_to_fly"`
	ExecutionMode  string            `json:"execution_mode" yaml:"execution_mode" toml:"execution_mode" toon:"execution_mode"`
	ResourceDigest string            `json:"resource_digest,omitempty" yaml:"resource_digest,omitempty" toml:"resource_digest,omitempty" toon:"resource_digest,omitempty"`
	UploadDigest   string            `json:"upload_digest,omitempty" yaml:"upload_digest,omitempty" toml:"upload_digest,omitempty" toon:"upload_digest,omitempty"`
	Entrypoint     resourceRefResult `json:"entrypoint" yaml:"entrypoint" toml:"entrypoint" toon:"entrypoint"`
	Nomad          hookResult        `json:"nomad" yaml:"nomad" toml:"nomad" toon:"nomad"`
	Conditions     []condition       `json:"conditions" yaml:"conditions" toml:"conditions" toon:"conditions"`
}

type commandOptions struct {
	File          string
	Output        string
	WorkspaceRoot string
	Stream        bool
	DryRun        bool
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
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) < 1 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "board":
		return runBoard(args[1:], stdout, stderr)
	case "fly":
		return runFly(args[1:], stdout, stderr)
	case "-h", "--help", "help":
		usage(stderr)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "guardian: unknown command: %s\n", args[0])
		usage(stderr)
		return 2
	}
}

func runBoard(args []string, stdout io.Writer, stderr io.Writer) int {
	opts, ok := parseCommonFlags("guardian board", args, stderr)
	if !ok {
		return 2
	}
	emitter := eventWriter{enabled: opts.Stream, stderr: stderr}
	doc, err := loadDocument(opts.File)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "guardian board: %v\n", err)
		return 1
	}
	result := evaluateBoard(doc, opts, emitter)
	if err := writeOutput(stdout, opts.Output, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "guardian board: %v\n", err)
		return 2
	}
	if hasFalseCondition(result.Conditions) {
		return 1
	}
	return 0
}

func runFly(args []string, stdout io.Writer, stderr io.Writer) int {
	opts, ok := parseCommonFlags("guardian fly", args, stderr)
	if !ok {
		return 2
	}
	emitter := eventWriter{enabled: opts.Stream, stderr: stderr}
	doc, err := loadDocument(opts.File)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "guardian fly: %v\n", err)
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

func parseCommonFlags(name string, args []string, stderr io.Writer) (commandOptions, bool) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	opts := commandOptions{}
	bindCommonFlags(fs, &opts)
	flagArgs, operands, err := splitCommandArgs(args)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return commandOptions{}, false
	}
	if err := fs.Parse(flagArgs); err != nil {
		return commandOptions{}, false
	}
	if err := setPositionalFile(&opts, operands); err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return commandOptions{}, false
	}
	if err := normalizeCommonOptions(&opts); err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return commandOptions{}, false
	}
	return opts, true
}

func bindCommonFlags(fs *flag.FlagSet, opts *commandOptions) {
	fs.StringVar(&opts.File, "f", "", "Guardian config document path")
	fs.StringVar(&opts.File, "file", "", "Guardian config document path")
	fs.StringVar(&opts.Output, "o", "yaml", "output format: yaml | json | toml | toon")
	fs.StringVar(&opts.Output, "output", "yaml", "output format: yaml | json | toml | toon")
	fs.StringVar(&opts.Output, "format", "yaml", "alias for --output")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "plan without mutating remote state")
	fs.BoolVar(&opts.DryRun, "plan", false, "alias for --dry-run")
	fs.BoolVar(&opts.Stream, "stream", false, "write newline-delimited JSON progress events to stderr")
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
	case "f", "file", "format", "o", "output":
		return true
	default:
		return false
	}
}

func setPositionalFile(opts *commandOptions, operands []string) error {
	if len(operands) > 1 {
		return fmt.Errorf("expected one file operand, got %d", len(operands))
	}
	if len(operands) == 0 {
		return nil
	}
	if opts.File != "" {
		return errors.New("file specified by both --file and positional operand")
	}
	opts.File = operands[0]
	return nil
}

func normalizeCommonOptions(opts *commandOptions) error {
	if strings.TrimSpace(opts.File) == "" {
		return errors.New("file is required")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cwd: %w", err)
	}
	workspaceRoot, err := discoverWorkspaceRoot(cwd)
	if err != nil {
		return err
	}
	opts.WorkspaceRoot = workspaceRoot
	opts.Output = strings.TrimSpace(opts.Output)
	return nil
}

func discoverWorkspaceRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve cwd: %w", err)
	}
	for {
		if stat, err := os.Stat(filepath.Join(dir, "MODULE.bazel")); err == nil && stat.Mode().IsRegular() {
			return dir, nil
		}
		if stat, err := os.Stat(filepath.Join(dir, "WORKSPACE")); err == nil && stat.Mode().IsRegular() {
			return dir, nil
		}
		if stat, err := os.Stat(filepath.Join(dir, "WORKSPACE.bazel")); err == nil && stat.Mode().IsRegular() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("guardian must be run from inside a Bazel workspace")
		}
		dir = parent
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

func evaluateBoard(doc guardianDocument, opts commandOptions, emitter eventWriter) boardResult {
	emitter.emit("board.load", "ok", "", "loaded Guardian config document")
	mode := "preflight"
	if opts.DryRun {
		mode = "dry_run"
	}
	substrateSpec := doc.Compiled.SubstrateSpec
	result := boardResult{
		Name:           doc.Compiled.Fly.Metadata.Name,
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
	result.Conditions = append(result.Conditions, conditionTrue("ResourceGraphResolved", "RefsResolved", "Guardian resource refs resolved", "resources"))
	result.Conditions = append(result.Conditions, conditionTrue("AccessHookConfigured", "HookConfigured", "access lifecycle hook is configured", "board.access"))
	result.Conditions = append(result.Conditions, conditionTrue("UploadHooksConfigured", "HooksConfigured", "upload lifecycle hooks are configured", "board.upload"))
	result.Conditions = append(result.Conditions, conditionTrue("KernelHooksConfigured", "HooksConfigured", "Nomad executor kernel hooks are configured", "board.kernel"))
	if err := writeFlyDocument(opts.WorkspaceRoot, doc.Source); err != nil {
		result.Upload.Status = "blocked"
		result.Upload.Reason = "ResourceGraphWriteFailed"
		result.Conditions = append(result.Conditions, conditionFalse("ResourceGraphMaterialized", "WriteFailed", err.Error(), "board.resources"))
		return result
	}
	result.Conditions = append(result.Conditions, conditionTrue("ResourceGraphMaterialized", "DocumentWritten", "Guardian resource graph was written for component-owned Nomad tasks", "board.resources"))
	if err := prepareBoardWorkspace(opts.WorkspaceRoot); err != nil {
		result.Upload.Status = "blocked"
		result.Upload.Reason = "BuildArtifactsMissing"
		result.Conditions = append(result.Conditions, conditionFalse("UploadPrepared", "BuildArtifactsMissing", err.Error(), "board.upload"))
		return result
	}
	result.Upload.Status = "prepared"
	result.Upload.Reason = "WorkspaceReady"
	result.Conditions = append(result.Conditions, conditionTrue("UploadPrepared", "WorkspaceReady", "workspace graph and build artifacts are present locally", "board.upload"))
	if hasFalseCondition(result.Conditions) {
		return result
	}
	if opts.DryRun {
		result.Conditions = append(result.Conditions, conditionTrue("BoardAccess", "DryRun", "dry run did not execute the access hook", "board.access"))
		result.Conditions = append(result.Conditions, conditionTrue("UploadExtract", "DryRun", "dry run did not materialize the workspace on the target", "board.upload.extract"))
		result.Conditions = append(result.Conditions, conditionTrue("UploadVerify", "DryRun", "dry run verified local boarding inputs without mutating the target", "board.upload.verify"))
		result.Conditions = append(result.Conditions, conditionTrue("KernelReady", "DryRun", "dry run did not execute kernel recovery hooks", "board.kernel"))
		return result
	}
	accessResult, _ := runLifecycleHook("board.access", substrateSpec.Access, opts.WorkspaceRoot, emitter)
	result.Access = accessResult
	if accessResult.Status != "ready" {
		result.Upload.Status = "blocked"
		result.Upload.Reason = "AccessHookFailed"
		result.Conditions = append(result.Conditions, conditionFalse("BoardAccess", accessResult.Reason, "access hook failed", "board.access"))
		return result
	}
	result.Conditions = append(result.Conditions, conditionTrue("BoardAccess", "HookSucceeded", "access hook completed", "board.access"))
	runResult, _ := runLifecycleHook("board.upload.run", substrateSpec.Upload.Run, opts.WorkspaceRoot, emitter)
	result.Upload.Run = runResult
	if runResult.Status != "ready" {
		result.Upload.Status = "blocked"
		result.Upload.Reason = "UploadHookFailed"
		result.Conditions = append(result.Conditions, conditionFalse("UploadRun", runResult.Reason, "upload run hook failed", "board.upload.run"))
		return result
	}
	result.Conditions = append(result.Conditions, conditionTrue("UploadRun", "HookSucceeded", "upload run hook completed", "board.upload.run"))
	extractResult, _ := runLifecycleHook("board.upload.extract", substrateSpec.Upload.Extract, opts.WorkspaceRoot, emitter)
	result.Upload.Extract = extractResult
	if extractResult.Status != "ready" {
		result.Upload.Status = "blocked"
		result.Upload.Reason = "ExtractHookFailed"
		result.Conditions = append(result.Conditions, conditionFalse("UploadExtract", extractResult.Reason, "upload extract hook failed", "board.upload.extract"))
		return result
	}
	result.Conditions = append(result.Conditions, conditionTrue("UploadExtract", "HookSucceeded", "upload extract hook completed", "board.upload.extract"))
	verifyResult, verifyStdout := runLifecycleHook("board.upload.verify", substrateSpec.Upload.Verify, opts.WorkspaceRoot, emitter)
	result.Upload.Verify = verifyResult
	if verifyResult.Status != "ready" {
		result.Upload.Status = "blocked"
		result.Upload.Reason = "VerifyHookFailed"
		result.Conditions = append(result.Conditions, conditionFalse("UploadVerify", verifyResult.Reason, "upload verify hook failed", "board.upload.verify"))
		return result
	}
	observedDigest, err := extractObservedDigest(string(verifyStdout))
	if err != nil {
		result.Upload.Status = "blocked"
		result.Upload.Reason = "DigestMissing"
		result.Conditions = append(result.Conditions, conditionFalse("UploadVerify", "DigestMissing", err.Error(), "board.upload.verify"))
		return result
	}
	result.Upload.Digest = observedDigest
	result.Upload.Status = "ready"
	result.Upload.Reason = "TreeVerified"
	result.Conditions = append(result.Conditions, conditionTrue("UploadVerify", "TreeVerified", "verify hook proved the boarded workspace tree and printed its digest", "board.upload.verify"))
	result.Conditions = append(result.Conditions, conditionTrue("UploadReady", "UploadVerified", "workspace upload is extracted and verified", "board.upload"))
	if !runKernelHooks(&result, substrateSpec.Kernel, opts.WorkspaceRoot, emitter) {
		return result
	}
	result.Conditions = append(result.Conditions, conditionTrue("KernelReady", "KernelBootstrapped", "Nomad is running with OpenBao integration inputs available", "board.kernel"))
	result.Conditions = append(result.Conditions, conditionTrue("ReadyToFly", "KernelBootstrapped", "Nomad can run component-owned recovery jobs", "board.kernel"))
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

func runKernelHooks(result *boardResult, kernel specdoc.Kernel, workspaceRoot string, emitter eventWriter) bool {
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
			name:      "board.kernel.openbao_prepare",
			resource:  "board.kernel.openbao_prepare",
			hook:      kernel.OpenBaoPrepare,
			assign:    func(h hookResult) { result.Kernel.OpenBaoPrepare = h },
			condition: "OpenBaoRuntimePrepared",
			message:   "OpenBao runtime and CA are prepared before Nomad starts",
		},
		{
			name:      "board.kernel.nomad",
			resource:  "board.kernel.nomad",
			hook:      kernel.Nomad,
			assign:    func(h hookResult) { result.Kernel.Nomad = h },
			condition: "NomadKernelReady",
			message:   "Nomad agent is running with OpenBao integration available",
		},
		{
			name:      "board.kernel.verify",
			resource:  "board.kernel.verify",
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
			result.Conditions = append(result.Conditions, conditionFalse(step.condition, hookResult.Reason, "kernel hook failed", step.resource))
			return false
		}
		result.Conditions = append(result.Conditions, conditionTrue(step.condition, "HookSucceeded", step.message, step.resource))
	}
	result.Kernel.Status = "ready"
	result.Kernel.Reason = "KernelBootstrapped"
	return true
}

func prepareBoardWorkspace(workspaceRoot string) error {
	for _, rel := range []string{
		defaultFlyDocumentPath,
		"bazel-bin",
	} {
		path := filepath.Join(workspaceRoot, filepath.FromSlash(rel))
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("%s is missing; build the repo before boarding", rel)
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
	stdout, _, err := execHook(ctx, hook, workspaceRoot)
	if err != nil {
		result.Reason = "HookFailed"
		emitter.emit(name, "blocked", "", strings.TrimSpace(err.Error()))
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
	boardResult := evaluateBoard(doc, opts, emitter)
	mode := "apply"
	if opts.DryRun {
		mode = "dry_run"
	}
	result := flyResult{
		Name:           doc.Compiled.Fly.Metadata.Name,
		ReadyToFly:     "no",
		ExecutionMode:  mode,
		ResourceDigest: boardResult.ResourceDigest,
		UploadDigest:   boardResult.Upload.Digest,
		Entrypoint:     boardResult.Entrypoint,
		Nomad:          hookPending(doc.Compiled.FlySpec.Nomad.Run),
	}
	result.Conditions = append(result.Conditions, conditionFromBoard(boardResult, opts.DryRun))
	boardReadyForFly := boardResult.ReadyToFly == "yes" || (opts.DryRun && !hasFalseCondition(boardResult.Conditions))
	if !boardReadyForFly {
		return result
	}
	if opts.DryRun {
		result.Conditions = append(result.Conditions, conditionTrue("NomadJobReady", "DryRun", "dry run did not submit a Nomad job", "fly.nomad"))
		result.ReadyToFly = "yes"
		return result
	}
	nomadResult, _ := runLifecycleHook("fly.nomad.run", doc.Compiled.FlySpec.Nomad.Run, opts.WorkspaceRoot, emitter)
	result.Nomad = nomadResult
	if nomadResult.Status != "ready" {
		result.Conditions = append(result.Conditions, conditionFalse("NomadJobReady", nomadResult.Reason, "Nomad job hook failed", "fly.nomad"))
		return result
	}
	result.Conditions = append(result.Conditions, conditionTrue("NomadJobReady", "HookSucceeded", "Nomad job hook completed", "fly.nomad"))
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

func conditionFromBoard(result boardResult, dryRun bool) condition {
	if result.ReadyToFly == "yes" {
		return conditionTrue("BoardingReady", "ReadyToFly", "workspace upload is verified on the target", "board")
	}
	if dryRun && !hasFalseCondition(result.Conditions) {
		return conditionTrue("BoardingReady", "DryRun", "dry run verified local boarding inputs without proving remote boarding", "board")
	}
	return conditionFalse("BoardingReady", "NotReadyToFly", "workspace upload is not present on the target", "board")
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
	_, _ = fmt.Fprint(w, `guardian

usage:
  guardian board <config.cue|yaml|json|toml|toon> [-o yaml|json|toml|toon] [--dry-run] [--stream]
  guardian fly <config.cue|yaml|json|toml|toon> [--dry-run] [-o yaml|json|toml|toon] [--stream]

board loads a FlyProcedure config document, verifies local build artifacts,
runs the Substrate access, upload, and kernel lifecycle hooks, and reports
whether the target is ready for Nomad-driven fly.

fly runs the same boarding phase and then runs the FlyProcedure Nomad job hook
from the boarded workspace.
	`)
}
