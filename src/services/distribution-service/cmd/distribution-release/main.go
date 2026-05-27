package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	mkskPackageName     = "mksk"
	mkskCargoManifest   = "src/make-skill/Cargo.toml"
	mkskCargoAbout      = "src/make-skill/about.toml"
	mkskReleaseTar      = "//src/make-skill:release_tar"
	mkskBinaryTarget    = "//src/make-skill:mksk"
	mkskBazelTarget     = "//src/make-skill:release_tar"
	mkskReleaseVersion  = "MKSK_RELEASE_VERSION"
	defaultBuilderID    = "spiffe://prod.verself.sh/svc/distribution-service"
	defaultOutDir       = "artifacts/releases"
	defaultSourceRef    = "HEAD"
	releaseMetadataBase = "https://oci.verself.sh/releases/mksk"
	sourceRepositoryURL = "https://github.com/guardian-intelligence/verself.git"
	repoURL             = "https://github.com/guardian-intelligence/verself"
)

var (
	finalSemVerRE   = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	rcSemVerRE      = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-rc\.(0|[1-9][0-9]*)$`)
	nightlySemVerRE = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-nightly\.[0-9]{8}\.(0|[1-9][0-9]*)$`)
)

type mkskConfig struct {
	repoRoot  string
	toolsTar  string
	outRoot   string
	versionPR bool
	channel   string
	version   string
	sourceRef string
	builderID string
	platform  string
}

type releasePaths struct {
	root     string
	artifact string
	sbom     string
	licenses string
	evidence string
	tests    string
}

type commandResult struct {
	stdout string
	stderr string
}

type buildSource struct {
	root    string
	dirty   bool
	cleanup func()
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "distribution-release: "+err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing subcommand: mksk")
	}
	switch args[0] {
	case "mksk":
		return runMksk(ctx, args[1:])
	case "-h", "--help", "help":
		printUsage(os.Stdout)
		return nil
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, `distribution-release <subcommand> [flags]

Subcommands:
  mksk  Generate make-skill release inspection artifacts.
`)
}

func runMksk(ctx context.Context, args []string) error {
	cfg := mkskConfig{}
	fs := flag.NewFlagSet("distribution-release mksk", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&cfg.repoRoot, "repo-root", "", "Repository root. Defaults to git rev-parse --show-toplevel.")
	fs.StringVar(&cfg.toolsTar, "tools-tar", "", "Bazel-built distribution release tools tar.")
	fs.StringVar(&cfg.outRoot, "out-dir", defaultOutDir, "Directory for inspectable release outputs.")
	fs.BoolVar(&cfg.versionPR, "version-pr", false, "Generate an inspectable release-plz version PR patch in a temporary worktree.")
	fs.StringVar(&cfg.channel, "channel", "", "Release channel: nightly, rc, or stable.")
	fs.StringVar(&cfg.version, "version", "", "Explicit package version. Required for rc and stable; optional for nightly.")
	fs.StringVar(&cfg.sourceRef, "source-ref", defaultSourceRef, "Git ref recorded as the source revision.")
	fs.StringVar(&cfg.builderID, "builder-id", defaultBuilderID, "SLSA builder id to place in local provenance.")
	fs.StringVar(&cfg.platform, "platform", "linux/amd64", "Release platform recorded in provenance.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional args: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(cfg.repoRoot) == "" {
		root, err := gitOutput(ctx, "", "rev-parse", "--show-toplevel")
		if err != nil {
			return err
		}
		cfg.repoRoot = root
	}
	cfg.repoRoot = filepath.Clean(cfg.repoRoot)
	if !filepath.IsAbs(cfg.outRoot) {
		cfg.outRoot = filepath.Join(cfg.repoRoot, cfg.outRoot)
	}
	if cfg.versionPR {
		return generateVersionPRPreview(ctx, cfg)
	}
	return generateReleaseArtifacts(ctx, cfg)
}

func generateVersionPRPreview(ctx context.Context, cfg mkskConfig) error {
	if strings.TrimSpace(cfg.toolsTar) == "" {
		return fmt.Errorf("--tools-tar is required")
	}
	sourceCommit, err := resolveCommit(ctx, cfg.repoRoot, cfg.sourceRef)
	if err != nil {
		return err
	}
	toolsDir, cleanup, err := extractTools(cfg.toolsTar)
	if err != nil {
		return err
	}
	defer cleanup()

	short := shortSHA(sourceCommit)
	out := filepath.Join(cfg.outRoot, mkskPackageName, "version-pr-"+utcStamp(time.Now().UTC())+"-"+short)
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	worktree, err := os.MkdirTemp("", "verself-mksk-version-pr-")
	if err != nil {
		return err
	}
	_ = os.Remove(worktree)
	branch := "mksk-release-preview-" + utcStamp(time.Now().UTC()) + "-" + short
	cleanupWorktree := func() {
		_, _ = runCommand(context.Background(), cfg.repoRoot, "git", "worktree", "remove", "--force", worktree)
		_, _ = runCommand(context.Background(), cfg.repoRoot, "git", "branch", "-D", branch)
	}
	defer cleanupWorktree()
	if _, err := runCommand(ctx, cfg.repoRoot, "git", "worktree", "add", "-b", branch, worktree, sourceCommit); err != nil {
		return err
	}
	releasePlz := filepath.Join(toolsDir, "bin", "release-plz")
	result, releaseErr := runCommand(ctx, worktree, releasePlz, "update", "--manifest-path", mkskCargoManifest, "--allow-dirty", "--repo-url", repoURL)
	logBody := strings.TrimSpace(result.stdout + result.stderr)
	if err := os.WriteFile(filepath.Join(out, "release-plz.log"), []byte(logBody+"\n"), 0o644); err != nil {
		return err
	}
	diff, diffErr := runCommand(ctx, worktree, "git", "diff", "--", "src/make-skill")
	if diffErr != nil {
		return diffErr
	}
	if err := os.WriteFile(filepath.Join(out, "release-plz.patch"), []byte(diff.stdout), 0o644); err != nil {
		return err
	}
	if err := writeText(filepath.Join(out, "README.txt"), []string{
		"make-skill release-plz version PR preview",
		"source_ref=" + cfg.sourceRef,
		"source_commit=" + sourceCommit,
		"release_plz_log=release-plz.log",
		"release_plz_patch=release-plz.patch",
	}); err != nil {
		return err
	}
	if releaseErr != nil {
		return fmt.Errorf("release-plz update failed; artifacts are in %s: %w", out, releaseErr)
	}
	return stdoutf("version PR preview: %s\n", out)
}

func generateReleaseArtifacts(ctx context.Context, cfg mkskConfig) error {
	if strings.TrimSpace(cfg.toolsTar) == "" {
		return fmt.Errorf("--tools-tar is required")
	}
	if strings.TrimSpace(cfg.channel) == "" {
		return fmt.Errorf("--channel is required unless --version-pr is set")
	}
	sourceCommit, err := resolveCommit(ctx, cfg.repoRoot, cfg.sourceRef)
	if err != nil {
		return err
	}
	source, err := prepareBuildSource(ctx, cfg, sourceCommit)
	if err != nil {
		return err
	}
	defer source.cleanup()
	toolsDir, cleanup, err := extractTools(cfg.toolsTar)
	if err != nil {
		return err
	}
	defer cleanup()
	bazelisk := filepath.Join(toolsDir, "bin", "bazelisk")

	workspace, err := workspaceVersion(filepath.Join(source.root, mkskCargoManifest))
	if err != nil {
		return err
	}
	version, err := resolveMkskVersion(cfg.channel, cfg.version, workspace, time.Now().UTC())
	if err != nil {
		return err
	}
	toolEnv, err := releaseToolEnv(cfg.outRoot, cfg.channel, version, sourceCommit)
	if err != nil {
		return err
	}
	bazelStartupOptions := releaseBazelStartupOptions(toolEnv)
	bazelCommandOptions := releaseBazelCommandOptions(toolEnv)
	started := time.Now().UTC()
	bazelReleaseVersionFlag := rustReleaseVersionFlag(version)
	testArgs := append([]string{}, bazelStartupOptions...)
	testArgs = append(testArgs, "test")
	testArgs = append(testArgs, bazelCommandOptions...)
	testArgs = append(testArgs,
		bazelReleaseVersionFlag,
		"//src/make-skill:core_test",
		"//src/make-skill:exec_test",
		"//src/make-skill:cli_test",
	)
	if _, err := runCommandWithEnv(ctx, source.root, bazelisk, toolEnv, testArgs...); err != nil {
		return err
	}
	mkskBinary, err := bazelOutputFile(ctx, source.root, bazelisk, toolEnv, bazelStartupOptions, bazelCommandOptions, mkskBinaryTarget, bazelReleaseVersionFlag)
	if err != nil {
		return err
	}
	if err := verifyMkskVersion(ctx, mkskBinary, version); err != nil {
		return err
	}
	releaseTar, err := bazelOutputFile(ctx, source.root, bazelisk, toolEnv, bazelStartupOptions, bazelCommandOptions, mkskReleaseTar, bazelReleaseVersionFlag)
	if err != nil {
		return err
	}
	short := shortSHA(sourceCommit)
	paths := releasePaths{
		root:     filepath.Join(cfg.outRoot, mkskPackageName, cfg.channel+"-"+version+"-"+short),
		artifact: "artifact",
		sbom:     "sbom",
		licenses: "licenses",
		evidence: "evidence",
		tests:    "tests",
	}
	if err := makeReleaseDirs(paths); err != nil {
		return err
	}
	artifactPath := filepath.Join(paths.root, paths.artifact, "make-skill.tar")
	if err := copyFile(releaseTar, artifactPath, 0o644); err != nil {
		return err
	}
	artifactDigest, artifactBytes, err := fileSHA256(artifactPath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(artifactPath+".sha256", []byte(artifactDigest+"  make-skill.tar\n"), 0o644); err != nil {
		return err
	}
	if err := copyTestXML(source.root, paths); err != nil {
		return err
	}
	if err := generateSBOMs(ctx, source.root, toolsDir, artifactPath, version, paths, toolEnv); err != nil {
		return err
	}
	if err := generateLicenses(ctx, source.root, toolsDir, paths, toolEnv); err != nil {
		return err
	}
	finished := time.Now().UTC()
	if err := writeSLSAProvenance(filepath.Join(paths.root, paths.evidence, "make-skill.provenance.intoto.json"), provenanceInput{
		artifactDigest:  artifactDigest,
		artifactBytes:   artifactBytes,
		builderID:       cfg.builderID,
		channel:         cfg.channel,
		version:         version,
		platform:        cfg.platform,
		sourceRef:       cfg.sourceRef,
		sourceCommit:    sourceCommit,
		workspaceDirty:  source.dirty,
		started:         started,
		finished:        finished,
		invocationID:    "local-" + utcStamp(started) + "-" + short,
		releaseTarLabel: mkskBazelTarget,
	}); err != nil {
		return err
	}
	if err := writeText(filepath.Join(paths.root, "README.txt"), []string{
		"make-skill local release inspection artifacts",
		"package=mksk",
		"version=" + version,
		"channel=" + cfg.channel,
		"platform=" + cfg.platform,
		"source_ref=" + cfg.sourceRef,
		"source_commit=" + sourceCommit,
		fmt.Sprintf("workspace_dirty=%t", source.dirty),
		"builder_id=" + cfg.builderID,
		"bazel_target=" + mkskBazelTarget,
		"release_metadata_url=" + releaseMetadataURL(version),
		"artifact=artifact/make-skill.tar",
		"artifact_sha256=" + artifactDigest,
		"sbom_artifact=sbom/make-skill.artifact.spdx.json",
		"sbom_source=sbom/make-skill.source.spdx.json",
		"licenses=licenses/make-skill.cargo-about.json",
		"provenance=evidence/make-skill.provenance.intoto.json",
		"tests=tests/*.xml",
	}); err != nil {
		return err
	}
	if err := writeChecksums(paths.root); err != nil {
		return err
	}
	return stdoutf("release artifacts: %s\n", paths.root)
}

func prepareBuildSource(ctx context.Context, cfg mkskConfig, sourceCommit string) (buildSource, error) {
	headCommit, err := resolveCommit(ctx, cfg.repoRoot, "HEAD")
	if err != nil {
		return buildSource{}, err
	}
	if sourceCommit == headCommit {
		dirty, err := dirtyWorkspace(ctx, cfg.repoRoot)
		if err != nil {
			return buildSource{}, err
		}
		return buildSource{root: cfg.repoRoot, dirty: dirty, cleanup: func() {}}, nil
	}
	worktree, err := os.MkdirTemp("", "verself-mksk-release-source-")
	if err != nil {
		return buildSource{}, err
	}
	_ = os.Remove(worktree)
	cleanup := func() {
		_, _ = runCommand(context.Background(), cfg.repoRoot, "git", "worktree", "remove", "--force", worktree)
	}
	if _, err := runCommand(ctx, cfg.repoRoot, "git", "worktree", "add", "--detach", worktree, sourceCommit); err != nil {
		cleanup()
		return buildSource{}, err
	}
	return buildSource{root: worktree, dirty: false, cleanup: cleanup}, nil
}

func makeReleaseDirs(paths releasePaths) error {
	if strings.TrimSpace(paths.root) == "" || filepath.Clean(paths.root) == string(filepath.Separator) {
		return fmt.Errorf("refusing to clear invalid release output root %q", paths.root)
	}
	// Output roots are deterministic, so stale inspection files must not survive into checksums.
	if err := os.RemoveAll(paths.root); err != nil {
		return fmt.Errorf("clear release output root %s: %w", paths.root, err)
	}
	for _, rel := range []string{"", paths.artifact, paths.sbom, paths.licenses, paths.evidence, paths.tests} {
		if err := os.MkdirAll(filepath.Join(paths.root, rel), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func generateSBOMs(ctx context.Context, sourceRoot, toolsDir, artifactPath, version string, paths releasePaths, env map[string]string) error {
	syft := filepath.Join(toolsDir, "bin", "syft")
	artifactOut := filepath.Join(paths.root, paths.sbom, "make-skill.artifact.spdx.json")
	sourceOut := filepath.Join(paths.root, paths.sbom, "make-skill.source.spdx.json")
	if _, err := runCommandWithEnv(ctx, sourceRoot, syft, env, "scan", "file:"+artifactPath, "-q", "--source-name", mkskPackageName, "--source-version", version, "-o", "spdx-json="+artifactOut); err != nil {
		return err
	}
	sourceDir := filepath.Join(sourceRoot, "src", "make-skill")
	if _, err := runCommandWithEnv(ctx, sourceRoot, syft, env, "scan", "dir:"+sourceDir, "--exclude", "**/target/**", "-q", "--source-name", mkskPackageName, "--source-version", version, "-o", "spdx-json="+sourceOut); err != nil {
		return err
	}
	return nil
}

func generateLicenses(ctx context.Context, sourceRoot, toolsDir string, paths releasePaths, env map[string]string) error {
	cargoAbout := filepath.Join(toolsDir, "bin", "cargo-about")
	out := filepath.Join(paths.root, paths.licenses, "make-skill.cargo-about.json")
	_, err := runCommandWithEnv(ctx, sourceRoot, cargoAbout, env, "generate", "--config", mkskCargoAbout, "--manifest-path", mkskCargoManifest, "--workspace", "--frozen", "--format", "json", "--output-file", out)
	return err
}

func copyTestXML(repoRoot string, paths releasePaths) error {
	tests := map[string]string{
		"core_test.xml": filepath.Join(repoRoot, "bazel-testlogs", "src", "make-skill", "core_test", "test.xml"),
		"exec_test.xml": filepath.Join(repoRoot, "bazel-testlogs", "src", "make-skill", "exec_test", "test.xml"),
		"cli_test.xml":  filepath.Join(repoRoot, "bazel-testlogs", "src", "make-skill", "cli_test", "test.xml"),
	}
	for name, src := range tests {
		if err := copyFile(src, filepath.Join(paths.root, paths.tests, name), 0o644); err != nil {
			return err
		}
	}
	return nil
}

type provenanceInput struct {
	artifactDigest  string
	artifactBytes   int64
	builderID       string
	channel         string
	version         string
	platform        string
	sourceRef       string
	sourceCommit    string
	workspaceDirty  bool
	started         time.Time
	finished        time.Time
	invocationID    string
	releaseTarLabel string
}

func writeSLSAProvenance(path string, input provenanceInput) error {
	statement := map[string]any{
		"_type": "https://in-toto.io/Statement/v1",
		"subject": []map[string]any{
			{
				"name": "make-skill.tar",
				"digest": map[string]string{
					"sha256": input.artifactDigest,
				},
			},
		},
		"predicateType": "https://slsa.dev/provenance/v1",
		"predicate": map[string]any{
			"buildDefinition": map[string]any{
				"buildType": "https://bazel.build/pkg_tar",
				"externalParameters": map[string]any{
					"bazelTarget": input.releaseTarLabel,
					"channel":     input.channel,
					"package":     mkskPackageName,
					"platform":    input.platform,
					"releaseURL":  releaseMetadataURL(input.version),
					"version":     input.version,
				},
				"internalParameters": map[string]any{
					"workspaceDirty": input.workspaceDirty,
				},
				"resolvedDependencies": []map[string]any{
					{
						"uri": sourceRepositoryURL,
						"digest": map[string]string{
							"gitCommit": input.sourceCommit,
						},
					},
				},
			},
			"runDetails": map[string]any{
				"builder": map[string]string{
					"id": input.builderID,
				},
				"metadata": map[string]any{
					"finishedOn":   input.finished.Format(time.RFC3339Nano),
					"invocationID": input.invocationID,
					"startedOn":    input.started.Format(time.RFC3339Nano),
				},
			},
		},
	}
	payload, err := json.MarshalIndent(statement, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(payload, '\n'), 0o644)
}

func resolveMkskVersion(channel, explicit, workspaceVersion string, now time.Time) (string, error) {
	channel = strings.TrimSpace(channel)
	explicit = strings.TrimSpace(explicit)
	switch channel {
	case "nightly":
		if explicit == "" {
			return workspaceVersion + "-nightly." + now.Format("20060102") + ".1", nil
		}
		if !nightlySemVerRE.MatchString(explicit) {
			return "", fmt.Errorf("nightly version must match MAJOR.MINOR.PATCH-nightly.YYYYMMDD.N")
		}
		return explicit, nil
	case "rc":
		if !rcSemVerRE.MatchString(explicit) {
			return "", fmt.Errorf("rc version must match MAJOR.MINOR.PATCH-rc.N")
		}
		return explicit, nil
	case "stable":
		if !finalSemVerRE.MatchString(explicit) {
			return "", fmt.Errorf("stable version must be final SemVer MAJOR.MINOR.PATCH")
		}
		return explicit, nil
	default:
		return "", fmt.Errorf("--channel must be nightly, rc, or stable")
	}
}

func workspaceVersion(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	version, ok := parseWorkspaceVersion(string(data))
	if !ok {
		return "", fmt.Errorf("%s must define [workspace.package] version", path)
	}
	return version, nil
}

func parseWorkspaceVersion(manifest string) (string, bool) {
	inWorkspacePackage := false
	for _, line := range strings.Split(manifest, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inWorkspacePackage = line == "[workspace.package]"
			continue
		}
		if !inWorkspacePackage {
			continue
		}
		rest, ok := strings.CutPrefix(line, "version")
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		rest, ok = strings.CutPrefix(rest, "=")
		if !ok {
			continue
		}
		version, ok := tomlString(strings.TrimSpace(rest))
		return version, ok
	}
	return "", false
}

func tomlString(value string) (string, bool) {
	body, ok := strings.CutPrefix(value, `"`)
	if !ok {
		return "", false
	}
	end := strings.IndexByte(body, '"')
	if end == -1 {
		return "", false
	}
	return body[:end], true
}

func rustReleaseVersionFlag(version string) string {
	return "--@rules_rust//rust/settings:extra_rustc_env=" + mkskReleaseVersion + "=" + version
}

func releaseMetadataURL(version string) string {
	return releaseMetadataBase + "/" + version
}

func verifyMkskVersion(ctx context.Context, binaryPath, version string) error {
	result, err := runCommand(ctx, "", binaryPath, "--version")
	if err != nil {
		return err
	}
	lines := nonEmptyLines(result.stdout)
	if len(lines) != 2 {
		return fmt.Errorf("mksk --version returned %d non-empty lines, want 2", len(lines))
	}
	if want := "mksk " + version; lines[0] != want {
		return fmt.Errorf("mksk --version first line = %q, want %q", lines[0], want)
	}
	if want := "release: " + releaseMetadataURL(version); lines[1] != want {
		return fmt.Errorf("mksk --version release line = %q, want %q", lines[1], want)
	}
	return nil
}

func resolveCommit(ctx context.Context, repoRoot, ref string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		ref = defaultSourceRef
	}
	return gitOutput(ctx, repoRoot, "rev-parse", "--verify", ref+"^{commit}")
}

func dirtyWorkspace(ctx context.Context, repoRoot string) (bool, error) {
	out, err := gitOutput(ctx, repoRoot, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func gitOutput(ctx context.Context, repoRoot string, args ...string) (string, error) {
	result, err := runCommand(ctx, repoRoot, "git", args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.stdout), nil
}

func bazelOutputFile(ctx context.Context, repoRoot, bazelisk string, env map[string]string, startupOptions []string, commandOptions []string, target string, buildOptions ...string) (string, error) {
	buildArgs := append([]string{}, startupOptions...)
	buildArgs = append(buildArgs, "build")
	buildArgs = append(buildArgs, commandOptions...)
	buildArgs = append(buildArgs, buildOptions...)
	buildArgs = append(buildArgs, target)
	if _, err := runCommandWithEnv(ctx, repoRoot, bazelisk, env, buildArgs...); err != nil {
		return "", err
	}
	cqueryArgs := append([]string{}, startupOptions...)
	cqueryArgs = append(cqueryArgs, "cquery")
	cqueryArgs = append(cqueryArgs, commandOptions...)
	cqueryArgs = append(cqueryArgs, "--output=files")
	cqueryArgs = append(cqueryArgs, buildOptions...)
	cqueryArgs = append(cqueryArgs, target)
	files, err := runCommandWithEnv(ctx, repoRoot, bazelisk, env, cqueryArgs...)
	if err != nil {
		return "", err
	}
	lines := nonEmptyLines(files.stdout)
	if len(lines) != 1 {
		return "", fmt.Errorf("expected one output for %s, got %d", target, len(lines))
	}
	infoArgs := append([]string{}, startupOptions...)
	infoArgs = append(infoArgs, "info")
	infoArgs = append(infoArgs, commandOptions...)
	infoArgs = append(infoArgs, "execution_root")
	execroot, err := runCommandWithEnv(ctx, repoRoot, bazelisk, env, infoArgs...)
	if err != nil {
		return "", err
	}
	out := filepath.Join(strings.TrimSpace(execroot.stdout), lines[0])
	if _, err := os.Stat(out); err != nil {
		return "", fmt.Errorf("bazel output %s: %w", out, err)
	}
	return out, nil
}

func runCommand(ctx context.Context, cwd, program string, args ...string) (commandResult, error) {
	return runCommandWithEnv(ctx, cwd, program, nil, args...)
}

func runCommandWithEnv(ctx context.Context, cwd, program string, env map[string]string, args ...string) (commandResult, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	if len(env) > 0 {
		cmd.Env = mergeCommandEnv(os.Environ(), env)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := commandResult{stdout: stdout.String(), stderr: stderr.String()}
	if err == nil {
		return result, nil
	}
	detail := strings.TrimSpace(result.stderr)
	if detail == "" {
		detail = strings.TrimSpace(result.stdout)
	}
	if detail == "" {
		detail = err.Error()
	}
	return result, fmt.Errorf("%s %s: %s", program, strings.Join(args, " "), detail)
}

func releaseToolEnv(outRoot, channel, version, sourceCommit string) (map[string]string, error) {
	root := filepath.Join(outRoot, "work", "tool-env", mkskPackageName, channel+"-"+version+"-"+shortSHA(sourceCommit))
	dirs := map[string]string{
		"HOME":           filepath.Join(root, "home"),
		"XDG_CACHE_HOME": filepath.Join(root, "cache"),
		"BAZELISK_HOME":  filepath.Join(root, "bazelisk"),
		"CARGO_HOME":     filepath.Join(root, "cargo"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create release tool env %s: %w", dir, err)
		}
	}
	return dirs, nil
}

func releaseBazelStartupOptions(env map[string]string) []string {
	return []string{"--output_user_root=" + filepath.Join(env["XDG_CACHE_HOME"], "bazel-output")}
}

func releaseBazelCommandOptions(env map[string]string) []string {
	return []string{
		"--disk_cache=" + filepath.Join(env["XDG_CACHE_HOME"], "bazel-disk"),
		"--repository_cache=" + filepath.Join(env["XDG_CACHE_HOME"], "bazel-repo"),
	}
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
		value := overrides[key]
		out = append(out, key+"="+value)
	}
	return out
}

func extractTools(toolsTar string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "verself-distribution-release-tools-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	if err := extractPlainTar(toolsTar, dir); err != nil {
		cleanup()
		return "", func() {}, err
	}
	for _, tool := range []string{"bazelisk", "cargo-about", "cosign", "oras", "release-plz", "syft"} {
		path := filepath.Join(dir, "bin", tool)
		if st, err := os.Stat(path); err != nil {
			cleanup()
			return "", func() {}, fmt.Errorf("release tool %s: %w", tool, err)
		} else if st.Mode()&0o111 == 0 {
			cleanup()
			return "", func() {}, fmt.Errorf("release tool %s is not executable", tool)
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

func stdoutf(format string, args ...any) error {
	_, err := fmt.Fprintf(os.Stdout, format, args...)
	return err
}

func safeJoin(root, name string) (string, error) {
	clean := filepath.Clean(name)
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return "", fmt.Errorf("unsafe tar path %q", name)
	}
	return filepath.Join(root, clean), nil
}

func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func fileSHA256(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func writeChecksums(root string) error {
	var lines []string
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "checksums.sha256" {
			return nil
		}
		sum, _, err := fileSHA256(path)
		if err != nil {
			return err
		}
		lines = append(lines, sum+"  "+filepath.ToSlash(rel))
		return nil
	}); err != nil {
		return err
	}
	sort.Strings(lines)
	return os.WriteFile(filepath.Join(root, "checksums.sha256"), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func writeText(path string, lines []string) error {
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
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

func shortSHA(value string) string {
	if len(value) < 12 {
		return value
	}
	return value[:12]
}

func utcStamp(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}
