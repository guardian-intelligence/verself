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
	"github.com/verself/guardian-specification/cli/internal/uploadbundle"
	"github.com/verself/guardian-specification/internal/formatio"
	"github.com/verself/guardian-specification/internal/specdoc"
)

type guardianDocument = specdoc.Document
type lifecycleHookSpec = specdoc.LifecycleHook
type nomadJobSpec = specdoc.NomadJob

type condition struct {
	Type     string `json:"type" yaml:"type" toml:"type" toon:"type"`
	Status   string `json:"status" yaml:"status" toml:"status" toon:"status"`
	Reason   string `json:"reason" yaml:"reason" toml:"reason" toon:"reason"`
	Message  string `json:"message,omitempty" yaml:"message,omitempty" toml:"message,omitempty" toon:"message,omitempty"`
	Resource string `json:"resource,omitempty" yaml:"resource,omitempty" toml:"resource,omitempty" toon:"resource,omitempty"`
}

type boardResult struct {
	Name               string           `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty" toon:"name,omitempty"`
	ReadyToFly         string           `json:"ready_to_fly" yaml:"ready_to_fly" toml:"ready_to_fly" toon:"ready_to_fly"`
	ExecutionMode      string           `json:"execution_mode" yaml:"execution_mode" toml:"execution_mode" toon:"execution_mode"`
	StaticConfigDigest string           `json:"static_config_digest,omitempty" yaml:"static_config_digest,omitempty" toml:"static_config_digest,omitempty" toon:"static_config_digest,omitempty"`
	StaticConfig       configResult     `json:"static_config" yaml:"static_config" toml:"static_config" toon:"static_config"`
	Access             hookResult       `json:"access" yaml:"access" toml:"access" toon:"access"`
	Upload             uploadResult     `json:"upload" yaml:"upload" toml:"upload" toon:"upload"`
	Nomad              nomadPlanResult  `json:"nomad" yaml:"nomad" toml:"nomad" toon:"nomad"`
	Jobs               []nomadJobResult `json:"jobs" yaml:"jobs" toml:"jobs" toon:"jobs"`
	Conditions         []condition      `json:"conditions" yaml:"conditions" toml:"conditions" toon:"conditions"`
}

type configResult struct {
	BaseURL        string `json:"base_url" yaml:"base_url" toml:"base_url" toon:"base_url"`
	CredentialsRef string `json:"credentials_ref" yaml:"credentials_ref" toml:"credentials_ref" toon:"credentials_ref"`
}

type uploadResult struct {
	Digest            string     `json:"digest,omitempty" yaml:"digest,omitempty" toml:"digest,omitempty" toon:"digest,omitempty"`
	ObservedDigest    string     `json:"observed_digest,omitempty" yaml:"observed_digest,omitempty" toml:"observed_digest,omitempty" toon:"observed_digest,omitempty"`
	Format            string     `json:"format,omitempty" yaml:"format,omitempty" toml:"format,omitempty" toon:"format,omitempty"`
	FileCount         int        `json:"file_count" yaml:"file_count" toml:"file_count" toon:"file_count"`
	CompressedBytes   int64      `json:"compressed_bytes,omitempty" yaml:"compressed_bytes,omitempty" toml:"compressed_bytes,omitempty" toon:"compressed_bytes,omitempty"`
	UncompressedBytes int64      `json:"uncompressed_bytes,omitempty" yaml:"uncompressed_bytes,omitempty" toml:"uncompressed_bytes,omitempty" toon:"uncompressed_bytes,omitempty"`
	Run               hookResult `json:"run" yaml:"run" toml:"run" toon:"run"`
	Verify            hookResult `json:"verify" yaml:"verify" toml:"verify" toon:"verify"`
	Status            string     `json:"status" yaml:"status" toml:"status" toon:"status"`
	Reason            string     `json:"reason" yaml:"reason" toml:"reason" toon:"reason"`
}

type hookResult struct {
	Argv   []string `json:"argv" yaml:"argv" toml:"argv" toon:"argv"`
	Status string   `json:"status" yaml:"status" toml:"status" toon:"status"`
	Reason string   `json:"reason" yaml:"reason" toml:"reason" toon:"reason"`
}

type flyResult struct {
	Name               string           `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty" toon:"name,omitempty"`
	ReadyToFly         string           `json:"ready_to_fly" yaml:"ready_to_fly" toml:"ready_to_fly" toon:"ready_to_fly"`
	ExecutionMode      string           `json:"execution_mode" yaml:"execution_mode" toml:"execution_mode" toon:"execution_mode"`
	StaticConfigDigest string           `json:"static_config_digest,omitempty" yaml:"static_config_digest,omitempty" toml:"static_config_digest,omitempty" toon:"static_config_digest,omitempty"`
	UploadDigest       string           `json:"upload_digest,omitempty" yaml:"upload_digest,omitempty" toml:"upload_digest,omitempty" toon:"upload_digest,omitempty"`
	Nomad              nomadPlanResult  `json:"nomad" yaml:"nomad" toml:"nomad" toon:"nomad"`
	Jobs               []nomadJobResult `json:"jobs" yaml:"jobs" toml:"jobs" toon:"jobs"`
	Conditions         []condition      `json:"conditions" yaml:"conditions" toml:"conditions" toon:"conditions"`
}

type nomadPlanResult struct {
	Address   string `json:"address" yaml:"address" toml:"address" toon:"address"`
	Namespace string `json:"namespace" yaml:"namespace" toml:"namespace" toon:"namespace"`
}

type nomadJobResult struct {
	Path        string   `json:"path" yaml:"path" toml:"path" toon:"path"`
	RequiredFor []string `json:"required_for,omitempty" yaml:"required_for,omitempty" toml:"required_for,omitempty" toon:"required_for,omitempty"`
	Status      string   `json:"status" yaml:"status" toml:"status" toon:"status"`
	Reason      string   `json:"reason" yaml:"reason" toml:"reason" toon:"reason"`
}

type commandOptions struct {
	File     string
	Output   string
	RepoRoot string
	Stream   bool
	DryRun   bool
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
	fs.StringVar(&opts.RepoRoot, "repo-root", "", "checkout root used for upload bundle and Nomad job paths")
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
	case "f", "file", "format", "o", "output", "repo-root":
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
	if opts.RepoRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("cwd: %w", err)
		}
		opts.RepoRoot = cwd
	}
	opts.Output = strings.TrimSpace(opts.Output)
	return nil
}

func loadDocument(path string) (guardianDocument, error) {
	if strings.EqualFold(filepath.Ext(path), ".cue") {
		return loadCUEDocument(path)
	}
	var doc guardianDocument
	if err := formatio.DecodeFile(path, &doc); err != nil {
		return guardianDocument{}, fmt.Errorf("load document: %w", err)
	}
	if err := validateDocument(doc); err != nil {
		return guardianDocument{}, err
	}
	return doc, nil
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
	var doc guardianDocument
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return guardianDocument{}, fmt.Errorf("decode CUE document: %w", err)
	}
	if err := validateDocument(doc); err != nil {
		return guardianDocument{}, err
	}
	return doc, nil
}

func validateDocument(doc guardianDocument) error {
	return specdoc.Validate(doc)
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
	result := boardResult{
		Name:               doc.Name,
		ReadyToFly:         "no",
		ExecutionMode:      mode,
		StaticConfigDigest: digestValue(doc.StaticConfig),
		StaticConfig: configResult{
			BaseURL:        doc.StaticConfig.BaseURL,
			CredentialsRef: doc.StaticConfig.CredentialsRef,
		},
		Access: hookPending(doc.Board.Access),
		Upload: uploadResult{
			Run:    hookPending(doc.Board.Upload.Run),
			Verify: hookPending(doc.Board.Upload.Verify),
			Status: "pending",
			Reason: "NotStarted",
		},
		Nomad: nomadPlanResult{
			Address:   doc.Nomad.Address,
			Namespace: doc.Nomad.Namespace,
		},
		Jobs: declaredNomadJobs(doc.Nomad.Jobs),
	}
	result.Conditions = append(result.Conditions, conditionTrue("AccessHookConfigured", "HookConfigured", "access lifecycle hook is configured", "board.access"))
	result.Conditions = append(result.Conditions, conditionTrue("UploadHooksConfigured", "HooksConfigured", "upload lifecycle hooks are configured", "board.upload"))
	prepared, err := prepareUpload(opts.RepoRoot)
	if err != nil {
		result.Upload.Status = "blocked"
		result.Upload.Reason = "BuildArtifactsMissing"
		result.Conditions = append(result.Conditions, conditionFalse("UploadPrepared", "BuildArtifactsMissing", err.Error(), "board.upload"))
		return result
	}
	defer prepared.Cleanup()
	result.Upload.Digest = prepared.Manifest.ArchiveSHA256
	result.Upload.Format = prepared.Manifest.Format
	result.Upload.FileCount = len(prepared.Manifest.Files)
	result.Upload.CompressedBytes = prepared.Manifest.CompressedBytes
	result.Upload.UncompressedBytes = prepared.Manifest.UncompressedBytes
	result.Upload.Status = "prepared"
	result.Upload.Reason = "BundleReady"
	result.Conditions = append(result.Conditions, conditionTrue("UploadPrepared", "BundleReady", "workspace upload bundle is built locally", "board.upload"))
	if hasFalseCondition(result.Conditions) {
		return result
	}
	if opts.DryRun {
		result.Conditions = append(result.Conditions, conditionTrue("BoardAccess", "DryRun", "dry run did not execute the access hook", "board.access"))
		result.Conditions = append(result.Conditions, conditionTrue("UploadVerify", "DryRun", "dry run prepared the upload bundle without mutating the target", "board.upload"))
		return result
	}
	accessResult, _ := runLifecycleHook("board.access", doc.Board.Access, opts.RepoRoot, prepared, emitter)
	result.Access = accessResult
	if accessResult.Status != "ready" {
		result.Upload.Status = "blocked"
		result.Upload.Reason = "AccessHookFailed"
		result.Conditions = append(result.Conditions, conditionFalse("BoardAccess", accessResult.Reason, "access hook failed", "board.access"))
		return result
	}
	result.Conditions = append(result.Conditions, conditionTrue("BoardAccess", "HookSucceeded", "access hook completed", "board.access"))
	runResult, _ := runLifecycleHook("board.upload.run", doc.Board.Upload.Run, opts.RepoRoot, prepared, emitter)
	result.Upload.Run = runResult
	if runResult.Status != "ready" {
		result.Upload.Status = "blocked"
		result.Upload.Reason = "UploadHookFailed"
		result.Conditions = append(result.Conditions, conditionFalse("UploadRun", runResult.Reason, "upload run hook failed", "board.upload.run"))
		return result
	}
	result.Conditions = append(result.Conditions, conditionTrue("UploadRun", "HookSucceeded", "upload run hook completed", "board.upload.run"))
	verifyResult, verifyStdout := runLifecycleHook("board.upload.verify", doc.Board.Upload.Verify, opts.RepoRoot, prepared, emitter)
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
	result.Upload.ObservedDigest = observedDigest
	if observedDigest != prepared.Manifest.ArchiveSHA256 {
		result.Upload.Status = "blocked"
		result.Upload.Reason = "DigestMismatch"
		result.Conditions = append(result.Conditions, conditionFalse("UploadVerify", "DigestMismatch", fmt.Sprintf("observed digest %s did not match local digest %s", observedDigest, prepared.Manifest.ArchiveSHA256), "board.upload.verify"))
		return result
	}
	result.Upload.Status = "ready"
	result.Upload.Reason = "DigestVerified"
	result.Conditions = append(result.Conditions, conditionTrue("UploadVerify", "DigestVerified", "verify hook observed the expected upload digest", "board.upload.verify"))
	result.Conditions = append(result.Conditions, conditionTrue("ReadyToFly", "UploadVerified", "workspace upload digest is verified", "board.upload"))
	result.ReadyToFly = "yes"
	return result
}

func hookPending(hook lifecycleHookSpec) hookResult {
	return hookResult{Argv: hook.Argv, Status: "pending", Reason: "NotStarted"}
}

type preparedUpload struct {
	Path     string
	Manifest uploadbundle.Manifest
}

func (p preparedUpload) Cleanup() {
	if p.Path != "" {
		_ = os.Remove(p.Path)
	}
}

func prepareUpload(repoRoot string) (preparedUpload, error) {
	tmp, err := os.CreateTemp("", "guardian-upload-*.tar.zst")
	if err != nil {
		return preparedUpload{}, fmt.Errorf("create upload bundle temp file: %w", err)
	}
	tmpPath := tmp.Name()
	manifest, buildErr := uploadbundle.BuildWorkspaceTarZstd(repoRoot, tmp)
	closeErr := tmp.Close()
	if buildErr != nil {
		_ = os.Remove(tmpPath)
		return preparedUpload{}, buildErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return preparedUpload{}, fmt.Errorf("close upload bundle: %w", closeErr)
	}
	return preparedUpload{
		Path:     tmpPath,
		Manifest: manifest,
	}, nil
}

func runLifecycleHook(name string, hook lifecycleHookSpec, repoRoot string, prepared preparedUpload, emitter eventWriter) (hookResult, []byte) {
	result := hookResult{Argv: hook.Argv, Status: "blocked", Reason: "HookFailed"}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	emitter.emit(name, "start", "", "running lifecycle hook")
	stdout, _, err := execHook(ctx, hook, repoRoot, prepared)
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

func execHook(ctx context.Context, hook lifecycleHookSpec, repoRoot string, prepared preparedUpload) ([]byte, []byte, error) {
	if len(hook.Argv) == 0 {
		return nil, nil, errors.New("hook argv is empty")
	}
	cmd := exec.CommandContext(ctx, hook.Argv[0], hook.Argv[1:]...)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), uploadHookEnv(repoRoot, prepared)...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func uploadHookEnv(repoRoot string, prepared preparedUpload) []string {
	return []string{
		"GUARDIAN_REPO_ROOT=" + repoRoot,
		"GUARDIAN_UPLOAD_BUNDLE=" + prepared.Path,
		"GUARDIAN_UPLOAD_FORMAT=" + prepared.Manifest.Format,
		"GUARDIAN_EXPECTED_DIGEST=" + prepared.Manifest.ArchiveSHA256,
		"GUARDIAN_UPLOAD_DIGEST=" + prepared.Manifest.ArchiveSHA256,
		"GUARDIAN_UPLOAD_COMPRESSED_BYTES=" + fmt.Sprint(prepared.Manifest.CompressedBytes),
		"GUARDIAN_UPLOAD_UNCOMPRESSED_BYTES=" + fmt.Sprint(prepared.Manifest.UncompressedBytes),
	}
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

func declaredNomadJobs(jobs []nomadJobSpec) []nomadJobResult {
	results := make([]nomadJobResult, 0, len(jobs))
	for _, job := range jobs {
		results = append(results, nomadJobResult{
			Path:        job.Path,
			RequiredFor: job.RequiredFor,
			Status:      "declared",
			Reason:      "Configured",
		})
	}
	return results
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
		Name:               doc.Name,
		ReadyToFly:         "no",
		ExecutionMode:      mode,
		StaticConfigDigest: boardResult.StaticConfigDigest,
		UploadDigest:       boardResult.Upload.Digest,
		Nomad: nomadPlanResult{
			Address:   doc.Nomad.Address,
			Namespace: doc.Nomad.Namespace,
		},
	}
	result.Conditions = append(result.Conditions, conditionFromBoard(boardResult, opts.DryRun))
	jobsReady := evaluateNomadJobs(doc.Nomad.Jobs, opts.RepoRoot, &result)
	if opts.DryRun {
		result.Conditions = append(result.Conditions, conditionTrue("NomadSubmission", "DryRun", "dry run completed without submitting to Nomad", "nomad"))
	} else {
		result.Conditions = append(result.Conditions, conditionFalse("NomadSubmission", "ExecutorUnavailable", "live Nomad submission is not implemented; rerun with --dry-run", "nomad"))
	}
	boardReadyForFly := boardResult.ReadyToFly == "yes" || (opts.DryRun && !hasFalseCondition(boardResult.Conditions))
	if boardReadyForFly && jobsReady && opts.DryRun {
		result.ReadyToFly = "yes"
	}
	return result
}

func evaluateNomadJobs(jobs []nomadJobSpec, repoRoot string, result *flyResult) bool {
	allReady := true
	for _, job := range jobs {
		resolved := job.Path
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(repoRoot, resolved)
		}
		jobResult := nomadJobResult{
			Path:        job.Path,
			RequiredFor: job.RequiredFor,
			Status:      "ready",
			Reason:      "PathResolved",
		}
		stat, err := os.Lstat(resolved)
		if err != nil {
			allReady = false
			jobResult.Status = "blocked"
			jobResult.Reason = "PathMissing"
		} else if stat.Mode()&os.ModeSymlink != 0 {
			allReady = false
			jobResult.Status = "blocked"
			jobResult.Reason = "PathIsSymlink"
		} else if !stat.Mode().IsRegular() {
			allReady = false
			jobResult.Status = "blocked"
			jobResult.Reason = "PathNotRegularFile"
		}
		result.Jobs = append(result.Jobs, jobResult)
	}
	if allReady {
		result.Conditions = append(result.Conditions, conditionTrue("NomadJobsResolved", "PathsResolved", "declared Nomad job files are present", "nomad.jobs"))
	} else {
		result.Conditions = append(result.Conditions, conditionFalse("NomadJobsResolved", "Blocked", "one or more Nomad job files are missing or invalid", "nomad.jobs"))
	}
	return allReady
}

func conditionFromBoard(result boardResult, dryRun bool) condition {
	if result.ReadyToFly == "yes" {
		return conditionTrue("BoardingReady", "ReadyToFly", "workspace upload is present on the target", "board")
	}
	if dryRun && !hasFalseCondition(result.Conditions) {
		return conditionTrue("BoardingReady", "DryRun", "dry run prepared the upload bundle without proving remote boarding", "board")
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
  guardian fly <config.cue|yaml|json|toml|toon> --dry-run [-o yaml|json|toml|toon] [--stream]

board loads a FlyProcedure config document, builds the workspace upload bundle,
runs the access and upload lifecycle hooks, and reports whether the verify hook
observed the same upload digest.

fly loads the same document and wraps the declared Nomad jobs. Live
Nomad submission is not implemented yet; use --dry-run for the current slice.
`)
}
