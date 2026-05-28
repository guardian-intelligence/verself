package releaseworkflow

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const (
	DefaultArtifactRoot    = "/artifacts/releases"
	DefaultWorkRoot        = "/artifacts/releases/work"
	DefaultReleaseToolsTar = "local/share/distribution-release-tools.tar"
	DefaultGitBin          = "git"
	DefaultBuilderID       = "spiffe://prod.verself.sh/svc/distribution-service"
)

type CommandRunnerConfig struct {
	ArtifactRoot    string
	WorkRoot        string
	ReleaseToolsTar string
	GitBinary       string
	BuilderID       string
}

type CommandRunner struct {
	artifactRoot    string
	workRoot        string
	releaseToolsTar string
	gitBinary       string
	builderID       string
	executor        CommandExecutor
}

type CommandExecutor interface {
	Run(ctx context.Context, command Command) (CommandResult, error)
}

type Command struct {
	Dir  string
	Name string
	Args []string
	Env  map[string]string
}

type CommandResult struct {
	Stdout string
	Stderr string
}

type OSCommandExecutor struct{}

func NewCommandRunner(cfg CommandRunnerConfig) (*CommandRunner, error) {
	artifactRoot := defaultString(cfg.ArtifactRoot, DefaultArtifactRoot)
	workRoot := defaultString(cfg.WorkRoot, DefaultWorkRoot)
	releaseToolsTar := defaultString(cfg.ReleaseToolsTar, DefaultReleaseToolsTar)
	gitBinary := defaultString(cfg.GitBinary, DefaultGitBin)
	builderID := defaultString(cfg.BuilderID, DefaultBuilderID)
	for name, value := range map[string]string{
		"artifact root":     artifactRoot,
		"work root":         workRoot,
		"release tools tar": releaseToolsTar,
		"git binary":        gitBinary,
		"builder id":        builderID,
	} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%w: %s is required", ErrInvalidInput, name)
		}
	}
	return &CommandRunner{
		artifactRoot:    filepath.Clean(artifactRoot),
		workRoot:        filepath.Clean(workRoot),
		releaseToolsTar: releaseToolsTar,
		gitBinary:       gitBinary,
		builderID:       builderID,
		executor:        OSCommandExecutor{},
	}, nil
}

func NewCommandRunnerForTest(cfg CommandRunnerConfig, executor CommandExecutor) (*CommandRunner, error) {
	runner, err := NewCommandRunner(cfg)
	if err != nil {
		return nil, err
	}
	if executor == nil {
		return nil, fmt.Errorf("%w: command executor is required", ErrInvalidInput)
	}
	runner.executor = executor
	return runner, nil
}

func (r *CommandRunner) ResolveSource(ctx context.Context, input ScheduledNightlyReleaseInput) (PinnedSource, error) {
	if err := validateScheduledNightlyInput(input); err != nil {
		return PinnedSource{}, err
	}
	return ResolvePinnedSource(ctx, r.executor, input.Package, input.Source, r.gitBinary)
}

func (r *CommandRunner) BuildNightly(ctx context.Context, input NightlyReleaseInput) (ReleaseWorkflowResult, error) {
	if err := validateNightlyInput(input); err != nil {
		return ReleaseWorkflowResult{}, err
	}
	return r.build(ctx, releaseBuild{
		Package: input.Package,
		Channel: ChannelNightly,
		Source:  input.Source,
	})
}

func (r *CommandRunner) BuildReleaseCandidate(ctx context.Context, input ReleaseCandidateInput) (ReleaseWorkflowResult, error) {
	if err := validateReleaseCandidateInput(input); err != nil {
		return ReleaseWorkflowResult{}, err
	}
	return r.build(ctx, releaseBuild{
		Package: input.Package,
		Channel: ChannelRC,
		Version: input.Version,
		Source:  input.Source,
	})
}

func (r *CommandRunner) BuildStable(ctx context.Context, input StableReleaseInput) (ReleaseWorkflowResult, error) {
	if err := validateStableInput(input); err != nil {
		return ReleaseWorkflowResult{}, err
	}
	return r.build(ctx, releaseBuild{
		Package: input.Package,
		Channel: ChannelStable,
		Version: input.Version,
		Source:  input.Source,
	})
}

type releaseBuild struct {
	Package string
	Channel string
	Version string
	Source  PinnedSource
}

func (r *CommandRunner) build(ctx context.Context, input releaseBuild) (ReleaseWorkflowResult, error) {
	dist, err := LookupDistributable(input.Package)
	if err != nil {
		return ReleaseWorkflowResult{}, err
	}
	checkout, err := r.prepareCheckout(ctx, dist, input.Source, releaseWorkKey(input))
	if err != nil {
		return ReleaseWorkflowResult{}, err
	}
	toolsDir, cleanup, err := extractReleaseTools(r.releaseToolsTar, []string{"bazelisk"})
	if err != nil {
		return ReleaseWorkflowResult{}, err
	}
	defer cleanup()
	toolEnv, err := r.releaseToolEnv(input)
	if err != nil {
		return ReleaseWorkflowResult{}, err
	}
	releaseBinary, err := r.bazelOutputFile(ctx, checkout, filepath.Join(toolsDir, "bin", "bazelisk"), toolEnv, dist.ReleaseTarget)
	if err != nil {
		return ReleaseWorkflowResult{}, err
	}
	packageTools, err := r.bazelOutputFile(ctx, checkout, filepath.Join(toolsDir, "bin", "bazelisk"), toolEnv, dist.ReleaseToolsTarget)
	if err != nil {
		return ReleaseWorkflowResult{}, err
	}
	args := []string{
		"build",
		"--repo-root", checkout,
		"--tools-tar", packageTools,
		"--artifact-root", r.artifactRoot,
		"--channel", input.Channel,
		"--source-ref", input.Source.Ref,
		"--source-commit", input.Source.Commit,
		"--builder-id", r.builderID,
	}
	if strings.TrimSpace(input.Version) != "" {
		args = append(args, "--version", strings.TrimSpace(input.Version))
	}
	result, err := r.run(ctx, "", releaseBinary, args...)
	if err != nil {
		return ReleaseWorkflowResult{}, err
	}
	artifactRoot, err := parseReleaseArtifactRoot(result.Stdout)
	if err != nil {
		return ReleaseWorkflowResult{}, err
	}
	if !pathWithin(r.artifactRoot, artifactRoot) {
		return ReleaseWorkflowResult{}, fmt.Errorf("package release artifact root %s is outside %s", artifactRoot, r.artifactRoot)
	}
	version, err := releaseMetadataValue(filepath.Join(artifactRoot, "README.txt"), "version")
	if err != nil {
		return ReleaseWorkflowResult{}, err
	}
	if err := verifyReleaseEvidence(artifactRoot, dist, input, version, r.builderID); err != nil {
		return ReleaseWorkflowResult{}, err
	}
	return ReleaseWorkflowResult{
		Package:      input.Package,
		Channel:      input.Channel,
		Version:      version,
		Source:       input.Source,
		ArtifactRoot: artifactRoot,
	}, nil
}

func (r *CommandRunner) prepareCheckout(ctx context.Context, dist Distributable, source PinnedSource, workKey string) (string, error) {
	if err := validatePinnedSource(source); err != nil {
		return "", err
	}
	checkout := filepath.Join(r.workRoot, "checkouts", dist.Package, source.Commit, safePathToken(workKey))
	if err := os.RemoveAll(checkout); err != nil {
		return "", fmt.Errorf("clear checkout %s: %w", checkout, err)
	}
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		return "", fmt.Errorf("create checkout %s: %w", checkout, err)
	}
	if _, err := r.run(ctx, checkout, r.gitBinary, "init", "."); err != nil {
		return "", err
	}
	if _, err := r.run(ctx, checkout, r.gitBinary, "remote", "add", "origin", dist.SourceRepo); err != nil {
		return "", err
	}
	if err := r.fetchSource(ctx, checkout, source); err != nil {
		return "", err
	}
	if _, err := r.run(ctx, checkout, r.gitBinary, "checkout", "--detach", source.Commit); err != nil {
		return "", err
	}
	if err := r.materializeSourceRef(ctx, checkout, source); err != nil {
		return "", err
	}
	return checkout, nil
}

func (r *CommandRunner) releaseToolEnv(input releaseBuild) (map[string]string, error) {
	root := filepath.Join(r.workRoot, "tool-env", "distribution-release", safePathToken(releaseWorkKey(input)))
	env := map[string]string{
		"HOME":           filepath.Join(root, "home"),
		"XDG_CACHE_HOME": filepath.Join(root, "cache"),
		"BAZELISK_HOME":  filepath.Join(root, "bazelisk"),
		"CARGO_HOME":     filepath.Join(root, "cargo"),
	}
	for _, dir := range env {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create release tool env %s: %w", dir, err)
		}
	}
	return env, nil
}

func (r *CommandRunner) bazelOutputFile(ctx context.Context, repoRoot, bazelisk string, env map[string]string, target string) (string, error) {
	startupOptions := releaseBazelStartupOptions(env)
	commandOptions := []string{
		"--disk_cache=" + filepath.Join(env["XDG_CACHE_HOME"], "bazel-disk"),
		"--repository_cache=" + filepath.Join(env["XDG_CACHE_HOME"], "bazel-repo"),
	}
	buildArgs := append([]string{}, startupOptions...)
	buildArgs = append(buildArgs, "build")
	buildArgs = append(buildArgs, commandOptions...)
	buildArgs = append(buildArgs, target)
	if _, err := r.runWithEnv(ctx, repoRoot, bazelisk, env, buildArgs...); err != nil {
		return "", err
	}
	cqueryArgs := append([]string{}, startupOptions...)
	cqueryArgs = append(cqueryArgs, "cquery")
	cqueryArgs = append(cqueryArgs, commandOptions...)
	cqueryArgs = append(cqueryArgs, "--output=files", target)
	files, err := r.runWithEnv(ctx, repoRoot, bazelisk, env, cqueryArgs...)
	if err != nil {
		return "", err
	}
	lines := nonEmptyLines(files.Stdout)
	if len(lines) != 1 {
		return "", fmt.Errorf("expected one output for %s, got %d", target, len(lines))
	}
	infoArgs := append([]string{}, startupOptions...)
	infoArgs = append(infoArgs, "info")
	infoArgs = append(infoArgs, commandOptions...)
	infoArgs = append(infoArgs, "execution_root")
	execroot, err := r.runWithEnv(ctx, repoRoot, bazelisk, env, infoArgs...)
	if err != nil {
		return "", err
	}
	out := filepath.Join(strings.TrimSpace(execroot.Stdout), lines[0])
	if _, err := os.Stat(out); err != nil {
		return "", fmt.Errorf("bazel output %s: %w", out, err)
	}
	return out, nil
}

func releaseBazelStartupOptions(env map[string]string) []string {
	return []string{
		"--output_user_root=" + filepath.Join(env["XDG_CACHE_HOME"], "bazel-output"),
		// Bazel servers inherit captured command pipes; release workers need them to exit.
		"--max_idle_secs=1",
	}
}

func releaseWorkKey(input releaseBuild) string {
	version := strings.TrimSpace(input.Version)
	if version == "" {
		version = "derived"
	}
	return strings.Join([]string{input.Channel, version, input.Source.Commit}, "-")
}

func (r *CommandRunner) fetchSource(ctx context.Context, checkout string, source PinnedSource) error {
	_, commitErr := r.run(ctx, checkout, r.gitBinary, "fetch", "--depth=1", "origin", source.Commit)
	if commitErr == nil {
		return nil
	}
	ref := strings.TrimSpace(source.Ref)
	if ref != "" && ref != source.Commit {
		_, err := r.run(ctx, checkout, r.gitBinary, "fetch", "--depth=1", "origin", ref)
		return err
	}
	return commitErr
}

func (r *CommandRunner) materializeSourceRef(ctx context.Context, checkout string, source PinnedSource) error {
	ref := localSourceRef(source.Ref)
	if ref == "" {
		return nil
	}
	_, err := r.run(ctx, checkout, r.gitBinary, "update-ref", ref, source.Commit)
	return err
}

func localSourceRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || ref == "HEAD" || gitCommitPattern.MatchString(ref) {
		return ""
	}
	if strings.HasPrefix(ref, "refs/heads/") || strings.HasPrefix(ref, "refs/tags/") {
		return ref
	}
	if strings.HasPrefix(ref, "refs/") {
		return ""
	}
	return "refs/heads/" + ref
}

func (r *CommandRunner) run(ctx context.Context, dir string, name string, args ...string) (CommandResult, error) {
	return r.runWithEnv(ctx, dir, name, nil, args...)
}

func (r *CommandRunner) runWithEnv(ctx context.Context, dir string, name string, env map[string]string, args ...string) (CommandResult, error) {
	if r == nil || r.executor == nil {
		return CommandResult{}, fmt.Errorf("%w: command runner is not initialized", ErrInvalidInput)
	}
	return r.executor.Run(ctx, Command{Dir: dir, Name: name, Args: args, Env: env})
}

func (OSCommandExecutor) Run(ctx context.Context, command Command) (CommandResult, error) {
	cmd := exec.CommandContext(ctx, command.Name, command.Args...)
	if strings.TrimSpace(command.Dir) != "" {
		cmd.Dir = command.Dir
	}
	if len(command.Env) > 0 {
		cmd.Env = mergeCommandEnv(os.Environ(), command.Env)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := CommandResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err == nil {
		return result, nil
	}
	detail := strings.TrimSpace(result.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(result.Stdout)
	}
	if detail == "" {
		detail = err.Error()
	}
	return result, fmt.Errorf("%s %s: %s", command.Name, strings.Join(command.Args, " "), detail)
}

func lsRemotePatterns(ref string) []string {
	if strings.HasPrefix(ref, "refs/") {
		return []string{ref}
	}
	return []string{ref, "refs/heads/" + ref, "refs/tags/" + ref, "refs/tags/" + ref + "^{}"}
}

func selectRemoteCommit(ref string, output string) (string, error) {
	preferred := remoteRefPriority(ref)
	bestPriority := len(preferred) + 1
	bestCommit := ""
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !gitCommitPattern.MatchString(fields[0]) {
			continue
		}
		priority := len(preferred)
		for i, candidate := range preferred {
			if fields[1] == candidate {
				priority = i
				break
			}
		}
		if priority < bestPriority {
			bestPriority = priority
			bestCommit = fields[0]
		}
	}
	if bestCommit == "" {
		return "", fmt.Errorf("%w: source ref %q did not resolve to a commit", ErrInvalidInput, ref)
	}
	return bestCommit, nil
}

func remoteRefPriority(ref string) []string {
	if strings.HasPrefix(ref, "refs/") {
		return []string{ref, ref + "^{}"}
	}
	return []string{
		"refs/heads/" + ref,
		"refs/tags/" + ref + "^{}",
		"refs/tags/" + ref,
		ref,
	}
}

func parseReleaseArtifactRoot(output string) (string, error) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		root, ok := strings.CutPrefix(line, "release artifacts: ")
		if ok && strings.TrimSpace(root) != "" {
			return filepath.Clean(strings.TrimSpace(root)), nil
		}
	}
	return "", fmt.Errorf("package release output did not include artifact root")
}

func releaseMetadataValue(path string, key string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read release metadata %s: %w", path, err)
	}
	prefix := key + "="
	for _, line := range strings.Split(string(data), "\n") {
		value, ok := strings.CutPrefix(strings.TrimSpace(line), prefix)
		if ok {
			value = strings.TrimSpace(value)
			if value == "" {
				return "", fmt.Errorf("release metadata %s has empty %s", path, key)
			}
			return value, nil
		}
	}
	return "", fmt.Errorf("release metadata %s is missing %s", path, key)
}

func verifyReleaseEvidence(root string, dist Distributable, input releaseBuild, version string, builderID string) error {
	switch dist.EvidencePolicy {
	case EvidencePolicyStandardCLIV1:
		return verifyStandardEvidence(root, dist, input, version, builderID)
	default:
		return fmt.Errorf("%w: unsupported evidence policy %q for %s", ErrInvalidInput, dist.EvidencePolicy, dist.Package)
	}
}

func verifyStandardEvidence(root string, dist Distributable, input releaseBuild, version string, builderID string) error {
	readme := filepath.Join(root, "README.txt")
	values, err := releaseMetadataValues(readme)
	if err != nil {
		return err
	}
	for key, want := range map[string]string{
		"package":       dist.Package,
		"channel":       input.Channel,
		"version":       version,
		"source_ref":    input.Source.Ref,
		"source_commit": input.Source.Commit,
		"builder_id":    builderID,
	} {
		got, ok := values[key]
		if !ok || got != want {
			return fmt.Errorf("release metadata %s %s = %q, want %q", readme, key, got, want)
		}
	}
	checksums, err := verifyChecksumManifest(root)
	if err != nil {
		return err
	}
	artifactRel, err := requiredReleaseMetadata(values, readme, "artifact")
	if err != nil {
		return err
	}
	artifactPath, _, err := verifyReleaseFile(root, artifactRel, checksums)
	if err != nil {
		return err
	}
	artifactDigest, err := fileSHA256(artifactPath)
	if err != nil {
		return fmt.Errorf("hash release artifact: %w", err)
	}
	if got := values["artifact_sha256"]; got != artifactDigest {
		return fmt.Errorf("release metadata artifact_sha256 = %q, want %q", got, artifactDigest)
	}
	provenanceRel, err := requiredReleaseMetadata(values, readme, "provenance")
	if err != nil {
		return err
	}
	provenancePath, _, err := verifyReleaseFile(root, provenanceRel, checksums)
	if err != nil {
		return err
	}
	if err := verifyProvenance(provenancePath, artifactDigest, dist, input, version, builderID); err != nil {
		return err
	}
	for _, key := range []string{"sbom_artifact", "sbom_source", "licenses"} {
		rel, err := requiredReleaseMetadata(values, readme, key)
		if err != nil {
			return err
		}
		path, _, err := verifyReleaseFile(root, rel, checksums)
		if err != nil {
			return err
		}
		if err := verifyJSONEvidence(path, key); err != nil {
			return err
		}
	}
	testsPattern, err := requiredReleaseMetadata(values, readme, "tests")
	if err != nil {
		return err
	}
	if err := verifyTestEvidence(root, testsPattern, checksums); err != nil {
		return err
	}
	return nil
}

func requiredReleaseMetadata(values map[string]string, path string, key string) (string, error) {
	value := strings.TrimSpace(values[key])
	if value == "" {
		return "", fmt.Errorf("release metadata %s is missing %s", path, key)
	}
	return value, nil
}

func releaseMetadataValues(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read release metadata %s: %w", path, err)
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			values[key] = strings.TrimSpace(value)
		}
	}
	return values, nil
}

func verifyChecksumManifest(root string) (map[string]bool, error) {
	manifest := filepath.Join(root, "checksums.sha256")
	data, err := os.ReadFile(manifest)
	if err != nil {
		return nil, fmt.Errorf("read checksum manifest: %w", err)
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		sum, rel, ok := strings.Cut(line, "  ")
		if !ok {
			return nil, fmt.Errorf("checksum manifest line is malformed: %q", line)
		}
		if !sha256HexPattern.MatchString(sum) {
			return nil, fmt.Errorf("checksum manifest has invalid sha256: %q", sum)
		}
		path, err := safeJoin(root, filepath.FromSlash(rel))
		if err != nil {
			return nil, err
		}
		got, err := fileSHA256(path)
		if err != nil {
			return nil, fmt.Errorf("hash checksum entry %s: %w", rel, err)
		}
		if got != sum {
			return nil, fmt.Errorf("checksum mismatch for %s", rel)
		}
		seen[rel] = true
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("checksum manifest is empty")
	}
	return seen, nil
}

var sha256HexPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func verifyReleaseFile(root string, rel string, checksums map[string]bool) (string, string, error) {
	path, checksumRel, err := releaseRelativePath(root, rel)
	if err != nil {
		return "", "", err
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", "", fmt.Errorf("release evidence %s: %w", checksumRel, err)
	}
	if st.IsDir() {
		return "", "", fmt.Errorf("release evidence %s is a directory", checksumRel)
	}
	if st.Size() == 0 {
		return "", "", fmt.Errorf("release evidence %s is empty", checksumRel)
	}
	if !checksums[checksumRel] {
		return "", "", fmt.Errorf("checksum manifest is missing %s", checksumRel)
	}
	return path, checksumRel, nil
}

func releaseRelativePath(root string, rel string) (string, string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", "", fmt.Errorf("release evidence path is empty")
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("unsafe release evidence path %q", rel)
	}
	return filepath.Join(root, clean), filepath.ToSlash(clean), nil
}

func verifyJSONEvidence(path string, label string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s evidence: %w", label, err)
	}
	if !json.Valid(data) {
		return fmt.Errorf("%s evidence is not valid JSON", label)
	}
	return nil
}

func verifyTestEvidence(root string, pattern string, checksums map[string]bool) error {
	glob, err := releaseRelativeGlob(root, pattern)
	if err != nil {
		return err
	}
	matches, err := filepath.Glob(glob)
	if err != nil {
		return fmt.Errorf("release test evidence pattern %q: %w", pattern, err)
	}
	if len(matches) == 0 {
		return fmt.Errorf("release test evidence pattern %q matched no files", pattern)
	}
	sort.Strings(matches)
	for _, path := range matches {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("release test evidence %s: %w", path, err)
		}
		if _, _, err := verifyReleaseFile(root, filepath.ToSlash(rel), checksums); err != nil {
			return err
		}
	}
	return nil
}

func releaseRelativeGlob(root string, pattern string) (string, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", fmt.Errorf("release evidence pattern is empty")
	}
	clean := filepath.Clean(filepath.FromSlash(pattern))
	if clean == "." || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe release evidence pattern %q", pattern)
	}
	return filepath.Join(root, filepath.FromSlash(pattern)), nil
}

func verifyProvenance(path string, artifactDigest string, dist Distributable, input releaseBuild, version string, builderID string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read provenance: %w", err)
	}
	var statement struct {
		Type          string `json:"_type"`
		PredicateType string `json:"predicateType"`
		Subject       []struct {
			Name   string            `json:"name"`
			Digest map[string]string `json:"digest"`
		} `json:"subject"`
		Predicate struct {
			BuildDefinition struct {
				ExternalParameters map[string]any `json:"externalParameters"`
				ResolvedDeps       []struct {
					URI    string            `json:"uri"`
					Digest map[string]string `json:"digest"`
				} `json:"resolvedDependencies"`
			} `json:"buildDefinition"`
			RunDetails struct {
				Builder map[string]string `json:"builder"`
			} `json:"runDetails"`
		} `json:"predicate"`
	}
	if err := json.Unmarshal(data, &statement); err != nil {
		return fmt.Errorf("parse provenance: %w", err)
	}
	if statement.Type != "https://in-toto.io/Statement/v1" {
		return fmt.Errorf("provenance _type = %q", statement.Type)
	}
	if statement.PredicateType != "https://slsa.dev/provenance/v1" {
		return fmt.Errorf("provenance predicateType = %q", statement.PredicateType)
	}
	if len(statement.Subject) != 1 || statement.Subject[0].Digest["sha256"] != artifactDigest {
		return fmt.Errorf("provenance subject digest does not match artifact")
	}
	params := statement.Predicate.BuildDefinition.ExternalParameters
	for key, want := range map[string]string{
		"channel":  input.Channel,
		"package":  dist.Package,
		"version":  version,
		"platform": "linux/amd64",
	} {
		if got, ok := params[key].(string); !ok || got != want {
			return fmt.Errorf("provenance externalParameters.%s = %v, want %q", key, params[key], want)
		}
	}
	if got := statement.Predicate.RunDetails.Builder["id"]; got != builderID {
		return fmt.Errorf("provenance builder id = %q, want %q", got, builderID)
	}
	foundSource := false
	for _, dep := range statement.Predicate.BuildDefinition.ResolvedDeps {
		if dep.URI == dist.SourceRepo && dep.Digest["gitCommit"] == input.Source.Commit {
			foundSource = true
		}
	}
	if !foundSource {
		return fmt.Errorf("provenance missing authorized source dependency")
	}
	return nil
}

func extractReleaseTools(toolsTar string, tools []string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "verself-distribution-release-tools-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	if err := extractPlainTar(toolsTar, dir); err != nil {
		cleanup()
		return "", func() {}, err
	}
	for _, tool := range tools {
		if err := requireExecutable(filepath.Join(dir, "bin", tool), "release tool "+tool); err != nil {
			cleanup()
			return "", func() {}, err
		}
	}
	return dir, cleanup, nil
}

func extractPlainTar(path, dest string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	tr := tar.NewReader(f)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeJoin(dest, header.Name)
		if err != nil {
			return err
		}
		mode, err := tarPermissionMode(header)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, mode); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, tr)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
}

func tarPermissionMode(header *tar.Header) (fs.FileMode, error) {
	if header.Mode < 0 || header.Mode > 0o7777 {
		return 0, fmt.Errorf("unsupported tar mode %o for %q", header.Mode, header.Name)
	}
	return header.FileInfo().Mode().Perm(), nil
}

func requireExecutable(path, label string) error {
	st, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if st.Mode()&0o111 == 0 {
		return fmt.Errorf("%s is not executable", label)
	}
	return nil
}

func safeJoin(root, name string) (string, error) {
	clean := filepath.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return "", fmt.Errorf("unsafe path %q", name)
	}
	return filepath.Join(root, clean), nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func mergeCommandEnv(base []string, overrides map[string]string) []string {
	out := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, replace := overrides[key]; replace {
				continue
			}
		}
		out = append(out, entry)
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = append(out, key+"="+overrides[key])
	}
	return out
}

func nonEmptyLines(value string) []string {
	var out []string
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func pathWithin(root string, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func safePathToken(value string) string {
	value = strings.TrimSpace(value)
	var out strings.Builder
	lastDash := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out.WriteRune(unicode.ToLower(r))
			lastDash = false
			continue
		}
		if !lastDash {
			out.WriteByte('-')
			lastDash = true
		}
	}
	token := strings.Trim(out.String(), "-")
	if token == "" {
		return "release"
	}
	return token
}
