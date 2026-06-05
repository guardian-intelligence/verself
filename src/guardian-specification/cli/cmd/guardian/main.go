// Command guardian evaluates Guardian Specification config documents.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
	"github.com/verself/guardian-specification/internal/formatio"
	"github.com/verself/guardian-specification/internal/specdoc"
)

type guardianDocument = specdoc.Document
type staticConfig = specdoc.StaticConfig
type substrateSpec = specdoc.Substrate
type sshAccessSpec = specdoc.SSHAccess
type seedSpec = specdoc.Seed
type seedPath = specdoc.SeedPath
type nomadSpec = specdoc.Nomad
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
	Substrate          substrateResult  `json:"substrate" yaml:"substrate" toml:"substrate" toon:"substrate"`
	Access             accessResult     `json:"access" yaml:"access" toml:"access" toon:"access"`
	Seed               seedResult       `json:"seed" yaml:"seed" toml:"seed" toon:"seed"`
	Nomad              nomadPlanResult  `json:"nomad" yaml:"nomad" toml:"nomad" toon:"nomad"`
	Jobs               []nomadJobResult `json:"jobs" yaml:"jobs" toml:"jobs" toon:"jobs"`
	Conditions         []condition      `json:"conditions" yaml:"conditions" toml:"conditions" toon:"conditions"`
}

type configResult struct {
	BaseURL        string `json:"base_url" yaml:"base_url" toml:"base_url" toon:"base_url"`
	CredentialsRef string `json:"credentials_ref" yaml:"credentials_ref" toml:"credentials_ref" toon:"credentials_ref"`
}

type substrateResult struct {
	StateDir string `json:"state_dir" yaml:"state_dir" toml:"state_dir" toon:"state_dir"`
}

type accessResult struct {
	Method             string `json:"method" yaml:"method" toml:"method" toon:"method"`
	Target             string `json:"target" yaml:"target" toml:"target" toon:"target"`
	KnownHostsFile     string `json:"known_hosts_file" yaml:"known_hosts_file" toml:"known_hosts_file" toon:"known_hosts_file"`
	FallbackConfigured bool   `json:"fallback_configured" yaml:"fallback_configured" toml:"fallback_configured" toon:"fallback_configured"`
	FallbackTarget     string `json:"fallback_target,omitempty" yaml:"fallback_target,omitempty" toml:"fallback_target,omitempty" toon:"fallback_target,omitempty"`
}

type seedResult struct {
	Digest      string             `json:"digest,omitempty" yaml:"digest,omitempty" toml:"digest,omitempty" toon:"digest,omitempty"`
	Root        string             `json:"root,omitempty" yaml:"root,omitempty" toml:"root,omitempty" toon:"root,omitempty"`
	TargetRoot  string             `json:"target_root" yaml:"target_root" toml:"target_root" toon:"target_root"`
	SourceCount int                `json:"source_count" yaml:"source_count" toml:"source_count" toon:"source_count"`
	Sources     []seedSourceResult `json:"sources" yaml:"sources" toml:"sources" toon:"sources"`
}

type seedSourceResult struct {
	Source string `json:"source" yaml:"source" toml:"source" toon:"source"`
	Target string `json:"target" yaml:"target" toml:"target" toon:"target"`
	Mode   string `json:"mode" yaml:"mode" toml:"mode" toon:"mode"`
	SHA256 string `json:"sha256,omitempty" yaml:"sha256,omitempty" toml:"sha256,omitempty" toon:"sha256,omitempty"`
	Status string `json:"status" yaml:"status" toml:"status" toon:"status"`
	Reason string `json:"reason" yaml:"reason" toml:"reason" toon:"reason"`
}

type flyResult struct {
	Name               string           `json:"name,omitempty" yaml:"name,omitempty" toml:"name,omitempty" toon:"name,omitempty"`
	ReadyToFly         string           `json:"ready_to_fly" yaml:"ready_to_fly" toml:"ready_to_fly" toon:"ready_to_fly"`
	ExecutionMode      string           `json:"execution_mode" yaml:"execution_mode" toml:"execution_mode" toon:"execution_mode"`
	StaticConfigDigest string           `json:"static_config_digest,omitempty" yaml:"static_config_digest,omitempty" toml:"static_config_digest,omitempty" toon:"static_config_digest,omitempty"`
	SeedDigest         string           `json:"seed_digest,omitempty" yaml:"seed_digest,omitempty" toml:"seed_digest,omitempty" toon:"seed_digest,omitempty"`
	SeedRoot           string           `json:"seed_root,omitempty" yaml:"seed_root,omitempty" toml:"seed_root,omitempty" toon:"seed_root,omitempty"`
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

type seedManifest struct {
	StaticConfig staticConfig        `json:"static_config"`
	Files        []seedManifestFile  `json:"files"`
	NomadJobs    []seedManifestNomad `json:"nomad_jobs"`
}

type seedManifestFile struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Mode   string `json:"mode"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type seedManifestNomad struct {
	Path        string   `json:"path"`
	RequiredFor []string `json:"required_for,omitempty"`
}

type commandOptions struct {
	File     string
	Output   string
	RepoRoot string
	Stream   bool
	DryRun   bool
	Render   bool
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
	if opts.Render {
		if err := writeOutput(stdout, opts.Output, doc); err != nil {
			_, _ = fmt.Fprintf(stderr, "guardian board: %v\n", err)
			return 2
		}
		return 0
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
	if opts.Render {
		if err := writeOutput(stdout, opts.Output, doc); err != nil {
			_, _ = fmt.Fprintf(stderr, "guardian fly: %v\n", err)
			return 2
		}
		return 0
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
	fs.StringVar(&opts.Output, "o", "yaml", "output format: yaml | json | toml | toon | text | table | dot | mermaid")
	fs.StringVar(&opts.Output, "output", "yaml", "output format: yaml | json | toml | toon | text | table | dot | mermaid")
	fs.StringVar(&opts.Output, "format", "yaml", "alias for --output")
	fs.StringVar(&opts.RepoRoot, "repo-root", "", "checkout root used for relative seed source and Nomad job paths")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "plan without mutating remote state")
	fs.BoolVar(&opts.DryRun, "plan", false, "alias for --dry-run")
	fs.BoolVar(&opts.Render, "render", false, "render the concrete config document without boarding or flying")
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
		Name:          doc.Name,
		ReadyToFly:    "no",
		ExecutionMode: mode,
		StaticConfig: configResult{
			BaseURL:        doc.StaticConfig.BaseURL,
			CredentialsRef: doc.StaticConfig.CredentialsRef,
		},
		Substrate: substrateResult{
			StateDir: doc.Board.Substrate.StateDir,
		},
		Access: accessStatus(doc.Board.Access.SSH),
		Seed: seedResult{
			TargetRoot:  doc.Board.Seed.TargetRoot,
			SourceCount: len(doc.Board.Seed.Paths),
		},
		Nomad: nomadPlanResult{
			Address:   doc.Nomad.Address,
			Namespace: doc.Nomad.Namespace,
		},
		Jobs: declaredNomadJobs(doc.Nomad.Jobs),
	}
	if !path.IsAbs(doc.Board.Seed.TargetRoot) {
		result.Conditions = append(result.Conditions, conditionFalse("Seed", "InvalidTargetRoot", "board.seed.targetRoot must be an absolute remote path", "board.seed.targetRoot"))
		return result
	}
	if !path.IsAbs(doc.Board.Substrate.StateDir) {
		result.Conditions = append(result.Conditions, conditionFalse("Substrate", "InvalidStateDir", "board.substrate.stateDir must be an absolute remote path", "board.substrate.stateDir"))
		return result
	}
	result.Conditions = append(result.Conditions, conditionTrue("AccessConfigured", "SSHConfigured", "SSH access configuration is complete", "board.access.ssh"))
	result.Conditions = append(result.Conditions, conditionTrue("SubstrateConfigured", "StateDirConfigured", "substrate state directory is configured", "board.substrate"))
	plan, conditions := buildSeedPlan(doc, opts.RepoRoot)
	result.Conditions = append(result.Conditions, conditions...)
	result.StaticConfigDigest = plan.StaticConfigDigest
	result.Seed.Digest = plan.Digest
	result.Seed.Root = plan.Root
	result.Seed.Sources = plan.Sources
	if hasFalseCondition(result.Conditions) {
		return result
	}
	result.Conditions = append(result.Conditions, conditionTrue("ReadyToFly", "SeedPlanned", "local seed inputs are deterministic and ready for remote materialization", "board.seed"))
	result.ReadyToFly = "yes"
	return result
}

func accessStatus(ssh sshAccessSpec) accessResult {
	status := accessResult{
		Method:         "ssh",
		Target:         fmt.Sprintf("%s@%s:%d", ssh.User, ssh.Host, ssh.Port),
		KnownHostsFile: ssh.KnownHostsFile,
	}
	if ssh.WireGuardFallback != nil {
		status.FallbackConfigured = true
		status.FallbackTarget = fmt.Sprintf("%s@%s:%d", ssh.User, ssh.WireGuardFallback.Host, ssh.WireGuardFallback.Port)
		if ssh.WireGuardFallback.Interface != "" {
			status.FallbackTarget += " via " + ssh.WireGuardFallback.Interface
		}
	}
	return status
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

func buildSeedPlan(doc guardianDocument, repoRoot string) (seedPlan, []condition) {
	plan := seedPlan{
		StaticConfigDigest: digestValue(doc.StaticConfig),
		Root:               doc.Board.Seed.TargetRoot,
	}
	manifest := seedManifest{
		StaticConfig: doc.StaticConfig,
		NomadJobs:    make([]seedManifestNomad, 0, len(doc.Nomad.Jobs)),
	}
	var conditions []condition
	for _, job := range doc.Nomad.Jobs {
		manifest.NomadJobs = append(manifest.NomadJobs, seedManifestNomad{Path: job.Path, RequiredFor: job.RequiredFor})
	}
	allReady := true
	for _, seedPath := range doc.Board.Seed.Paths {
		sourceResult := seedSourceResult{
			Source: seedPath.Source,
			Target: seedPath.Target,
			Mode:   seedPath.Mode,
			Status: "ready",
			Reason: "PathResolved",
		}
		if err := validateSeedTarget(seedPath.Target); err != nil {
			allReady = false
			sourceResult.Status = "blocked"
			sourceResult.Reason = "InvalidTarget"
			conditions = append(conditions, conditionFalse("SeedSource", "InvalidTarget", err.Error(), seedPath.Target))
			plan.Sources = append(plan.Sources, sourceResult)
			continue
		}
		if err := validateMode(seedPath.Mode); err != nil {
			allReady = false
			sourceResult.Status = "blocked"
			sourceResult.Reason = "InvalidMode"
			conditions = append(conditions, conditionFalse("SeedSource", "InvalidMode", err.Error(), seedPath.Target))
			plan.Sources = append(plan.Sources, sourceResult)
			continue
		}
		absoluteSource := seedPath.Source
		if !filepath.IsAbs(absoluteSource) {
			absoluteSource = filepath.Join(repoRoot, absoluteSource)
		}
		digest, size, err := fileSHA256(absoluteSource)
		if err != nil {
			allReady = false
			sourceResult.Status = "blocked"
			sourceResult.Reason = "SourceUnavailable"
			conditions = append(conditions, conditionFalse("SeedSource", "SourceUnavailable", err.Error(), seedPath.Source))
			plan.Sources = append(plan.Sources, sourceResult)
			continue
		}
		sourceResult.SHA256 = digest
		plan.Sources = append(plan.Sources, sourceResult)
		manifest.Files = append(manifest.Files, seedManifestFile{
			Source: seedPath.Source,
			Target: path.Clean(seedPath.Target),
			Mode:   seedPath.Mode,
			SHA256: digest,
			Size:   size,
		})
	}
	if !allReady {
		return plan, conditions
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		conditions = append(conditions, conditionFalse("Seed", "ManifestEncodingFailed", err.Error(), "board.seed"))
		return plan, conditions
	}
	sum := sha256.Sum256(manifestBytes)
	hexDigest := hex.EncodeToString(sum[:])
	plan.Digest = "sha256:" + hexDigest
	plan.Root = path.Join(doc.Board.Seed.TargetRoot, "sha256-"+hexDigest)
	conditions = append(conditions, conditionTrue("Seed", "DigestComputed", "seed manifest digest computed", "board.seed"))
	return plan, conditions
}

type seedPlan struct {
	StaticConfigDigest string
	Digest             string
	Root               string
	Sources            []seedSourceResult
}

func validateSeedTarget(target string) error {
	if strings.TrimSpace(target) == "" {
		return errors.New("seed target is required")
	}
	if path.IsAbs(target) {
		return fmt.Errorf("seed target %q must be relative", target)
	}
	if strings.Contains(target, "\\") {
		return fmt.Errorf("seed target %q must use slash separators", target)
	}
	clean := path.Clean(target)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("seed target %q escapes the seed root", target)
	}
	return nil
}

func validateMode(mode string) error {
	if len(mode) != 4 || mode[0] != '0' {
		return fmt.Errorf("mode %q must be a four-digit octal string", mode)
	}
	if _, err := strconv.ParseUint(mode, 8, 32); err != nil {
		return fmt.Errorf("mode %q must be octal", mode)
	}
	return nil
}

func fileSHA256(filename string) (string, int64, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return "", 0, fmt.Errorf("%s: %w", filename, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", 0, fmt.Errorf("%s: symlink seed sources are not allowed", filename)
	}
	if !info.Mode().IsRegular() {
		return "", 0, fmt.Errorf("%s: seed source must be a regular file", filename)
	}
	file, err := os.Open(filename)
	if err != nil {
		return "", 0, fmt.Errorf("%s: %w", filename, err)
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", 0, fmt.Errorf("%s: %w", filename, err)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), info.Size(), nil
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
		SeedDigest:         boardResult.Seed.Digest,
		SeedRoot:           boardResult.Seed.Root,
		Nomad: nomadPlanResult{
			Address:   doc.Nomad.Address,
			Namespace: doc.Nomad.Namespace,
		},
	}
	result.Conditions = append(result.Conditions, conditionFromBoard(boardResult))
	jobsReady := evaluateNomadJobs(doc.Nomad.Jobs, opts.RepoRoot, &result)
	if opts.DryRun {
		result.Conditions = append(result.Conditions, conditionTrue("NomadSubmission", "DryRun", "dry run completed without submitting to Nomad", "nomad"))
	} else {
		result.Conditions = append(result.Conditions, conditionFalse("NomadSubmission", "ExecutorUnavailable", "live Nomad submission is not implemented; rerun with --dry-run", "nomad"))
	}
	if boardResult.ReadyToFly == "yes" && jobsReady && opts.DryRun {
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

func conditionFromBoard(result boardResult) condition {
	if result.ReadyToFly == "yes" {
		return conditionTrue("BoardingReady", "ReadyToFly", "boarding inputs are ready for fly", "board")
	}
	return conditionFalse("BoardingReady", "NotReadyToFly", "boarding inputs are not ready for fly", "board")
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
	if isProjectedOutputFormat(format) {
		return writeProjectedOutput(w, format, value)
	}
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
  guardian board <config.cue|yaml|json|toml|toon> [-o yaml|json|toml|toon|text|table|dot|mermaid] [--dry-run] [--stream]
  guardian fly <config.cue|yaml|json|toml|toon> --dry-run [-o yaml|json|toml|toon|text|table|dot|mermaid] [--stream]

board loads a FlyProcedure config document, checks SSH access configuration,
computes the content-addressed seed, and reports whether fly can be planned.

fly loads the same document and wraps the declared Nomad jobs. Live
Nomad submission is not implemented yet; use --dry-run for the current slice.
`)
}
