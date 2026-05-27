package releaseworkflow

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	DefaultSourceRepository       = "https://github.com/guardian-intelligence/verself.git"
	DefaultArtifactRoot           = "/artifacts/releases"
	DefaultWorkRoot               = "/artifacts/releases/work"
	DefaultReleaseToolsTar        = "local/share/distribution-release-tools.tar"
	DefaultDistributionReleaseBin = "local/bin/distribution-release"
	DefaultGitBin                 = "git"
	DefaultBuilderID              = "spiffe://prod.verself.sh/svc/distribution-service"
)

type CommandRunnerConfig struct {
	SourceRepository          string
	ArtifactRoot              string
	WorkRoot                  string
	ReleaseToolsTar           string
	DistributionReleaseBinary string
	GitBinary                 string
	BuilderID                 string
}

type CommandRunner struct {
	sourceRepository          string
	artifactRoot              string
	workRoot                  string
	releaseToolsTar           string
	distributionReleaseBinary string
	gitBinary                 string
	builderID                 string
	executor                  CommandExecutor
}

type CommandExecutor interface {
	Run(ctx context.Context, command Command) (CommandResult, error)
}

type Command struct {
	Dir  string
	Name string
	Args []string
}

type CommandResult struct {
	Stdout string
	Stderr string
}

type OSCommandExecutor struct{}

func NewCommandRunner(cfg CommandRunnerConfig) (*CommandRunner, error) {
	sourceRepository := defaultString(cfg.SourceRepository, DefaultSourceRepository)
	artifactRoot := defaultString(cfg.ArtifactRoot, DefaultArtifactRoot)
	workRoot := defaultString(cfg.WorkRoot, DefaultWorkRoot)
	releaseToolsTar := defaultString(cfg.ReleaseToolsTar, DefaultReleaseToolsTar)
	distributionReleaseBinary := defaultString(cfg.DistributionReleaseBinary, DefaultDistributionReleaseBin)
	gitBinary := defaultString(cfg.GitBinary, DefaultGitBin)
	builderID := defaultString(cfg.BuilderID, DefaultBuilderID)
	for name, value := range map[string]string{
		"source repository":           sourceRepository,
		"artifact root":               artifactRoot,
		"work root":                   workRoot,
		"release tools tar":           releaseToolsTar,
		"distribution release binary": distributionReleaseBinary,
		"git binary":                  gitBinary,
		"builder id":                  builderID,
	} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%w: %s is required", ErrInvalidInput, name)
		}
	}
	return &CommandRunner{
		sourceRepository:          sourceRepository,
		artifactRoot:              filepath.Clean(artifactRoot),
		workRoot:                  filepath.Clean(workRoot),
		releaseToolsTar:           releaseToolsTar,
		distributionReleaseBinary: distributionReleaseBinary,
		gitBinary:                 gitBinary,
		builderID:                 builderID,
		executor:                  OSCommandExecutor{},
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
	ref := strings.TrimSpace(input.Source.Ref)
	if gitCommitPattern.MatchString(ref) {
		return PinnedSource{Ref: ref, Commit: ref}, nil
	}
	result, err := r.run(ctx, "", r.gitBinary, append([]string{"ls-remote", r.sourceRepository}, lsRemotePatterns(ref)...)...)
	if err != nil {
		return PinnedSource{}, fmt.Errorf("resolve source ref %s: %w", ref, err)
	}
	commit, err := selectRemoteCommit(ref, result.Stdout)
	if err != nil {
		return PinnedSource{}, err
	}
	return PinnedSource{Ref: ref, Commit: commit}, nil
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
	if input.Package != PackageMksk {
		return ReleaseWorkflowResult{}, fmt.Errorf("%w: unsupported package %q", ErrInvalidInput, input.Package)
	}
	checkout, err := r.prepareCheckout(ctx, input.Source, releaseWorkKey(input))
	if err != nil {
		return ReleaseWorkflowResult{}, err
	}
	args := []string{
		"mksk",
		"--repo-root", checkout,
		"--tools-tar", r.releaseToolsTar,
		"--out-dir", r.artifactRoot,
		"--channel", input.Channel,
		"--source-ref", input.Source.Ref,
		"--builder-id", r.builderID,
	}
	if strings.TrimSpace(input.Version) != "" {
		args = append(args, "--version", strings.TrimSpace(input.Version))
	}
	result, err := r.run(ctx, "", r.distributionReleaseBinary, args...)
	if err != nil {
		return ReleaseWorkflowResult{}, err
	}
	artifactRoot, err := parseReleaseArtifactRoot(result.Stdout)
	if err != nil {
		return ReleaseWorkflowResult{}, err
	}
	version, err := releaseMetadataValue(filepath.Join(artifactRoot, "README.txt"), "version")
	if err != nil {
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

func (r *CommandRunner) prepareCheckout(ctx context.Context, source PinnedSource, workKey string) (string, error) {
	if err := validatePinnedSource(source); err != nil {
		return "", err
	}
	checkout := filepath.Join(r.workRoot, "checkouts", PackageMksk, source.Commit, safePathToken(workKey))
	if err := os.RemoveAll(checkout); err != nil {
		return "", fmt.Errorf("clear checkout %s: %w", checkout, err)
	}
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		return "", fmt.Errorf("create checkout %s: %w", checkout, err)
	}
	if _, err := r.run(ctx, checkout, r.gitBinary, "init", "."); err != nil {
		return "", err
	}
	if _, err := r.run(ctx, checkout, r.gitBinary, "remote", "add", "origin", r.sourceRepository); err != nil {
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
	if r == nil || r.executor == nil {
		return CommandResult{}, fmt.Errorf("%w: command runner is not initialized", ErrInvalidInput)
	}
	return r.executor.Run(ctx, Command{Dir: dir, Name: name, Args: args})
}

func (OSCommandExecutor) Run(ctx context.Context, command Command) (CommandResult, error) {
	cmd := exec.CommandContext(ctx, command.Name, command.Args...)
	if strings.TrimSpace(command.Dir) != "" {
		cmd.Dir = command.Dir
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
	return "", fmt.Errorf("distribution-release output did not include artifact root")
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
