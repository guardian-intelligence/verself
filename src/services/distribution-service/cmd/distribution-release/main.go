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
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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
	defaultReleaseUser  = "distribution_release"
	defaultReleaseGroup = "zot"
	defaultBuilderID    = "spiffe://prod.verself.sh/svc/distribution-release"
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
	gitSHARE        = regexp.MustCompile(`^[a-f0-9]{40}$`)
)

type mkskConfig struct {
	repoRoot     string
	toolsTar     string
	toolsDir     string
	outRoot      string
	releaseRoot  string
	local        bool
	versionPR    bool
	channel      string
	version      string
	sourceRef    string
	sourceCommit string
	builderID    string
	platform     string
	site         string
	nomadAddr    string
	nomadJobID   string
	wait         bool
	waitTimeout  time.Duration
}

type releasePaths struct {
	root     string
	artifact string
	sbom     string
	licenses string
	evidence string
	tests    string
}

type releaseOutput struct {
	paths             releasePaths
	version           string
	channel           string
	platform          string
	platformOS        string
	platformArch      string
	sourceRef         string
	sourceCommit      string
	artifactPath      string
	provenancePath    string
	artifactSBOMPath  string
	sourceSBOMPath    string
	licensesPath      string
	testEvidencePaths []string
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

type prepareRootConfig struct {
	artifactRoot string
	home         string
	userName     string
	groupName    string
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
  mksk  Dispatch, publish, or locally inspect make-skill releases.
`)
}

func runMksk(ctx context.Context, args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "publish":
			return runMkskPublish(ctx, args[1:])
		case "prepare-root":
			return runMkskPrepareRoot(args[1:])
		case "local":
			return runMkskLocal(ctx, args[1:], true)
		case "dispatch":
			return runMkskLocal(ctx, args[1:], false)
		case "-h", "--help", "help":
			printMkskUsage(os.Stdout)
			return nil
		}
	}
	return runMkskLocal(ctx, args, false)
}

func printMkskUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, `distribution-release mksk [flags]
distribution-release mksk local [flags]
distribution-release mksk prepare-root [flags]
distribution-release mksk publish [flags]

Default mksk mode dispatches a deployed Nomad release job. Use --local for
inspection artifacts without publication.
`)
}

func runMkskPrepareRoot(args []string) error {
	cfg := prepareRootConfig{
		userName:  defaultReleaseUser,
		groupName: defaultReleaseGroup,
	}
	fs := flag.NewFlagSet("distribution-release mksk prepare-root", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&cfg.artifactRoot, "artifact-root", "", "Exact output root for release artifacts.")
	fs.StringVar(&cfg.home, "home", "", "Per-allocation home directory for release tooling.")
	fs.StringVar(&cfg.userName, "user", defaultReleaseUser, "Unix user that owns release directories.")
	fs.StringVar(&cfg.groupName, "group", defaultReleaseGroup, "Unix group that owns release directories.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional args: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(cfg.artifactRoot) == "" {
		return fmt.Errorf("--artifact-root is required")
	}
	if strings.TrimSpace(cfg.home) == "" {
		return fmt.Errorf("--home is required")
	}
	artifactRoot, err := cleanRootUnder(cfg.artifactRoot, "/artifacts/releases/mksk")
	if err != nil {
		return err
	}
	home, err := cleanRootUnder(cfg.home, "/tmp/distribution-release-mksk")
	if err != nil {
		return err
	}
	uid, gid, err := lookupOwner(cfg.userName, cfg.groupName)
	if err != nil {
		return err
	}
	for _, dir := range []string{"/artifacts", "/artifacts/releases"} {
		if err := ensureDir(dir, 0o755); err != nil {
			return err
		}
	}
	for _, dir := range []string{"/artifacts/releases/mksk", artifactRoot, "/tmp/distribution-release-mksk", filepath.Dir(home), home} {
		if err := ensureOwnedDir(dir, 0o750, uid, gid); err != nil {
			return err
		}
	}
	return nil
}

func runMkskLocal(ctx context.Context, args []string, forceLocal bool) error {
	cfg := mkskConfig{}
	fs := flag.NewFlagSet("distribution-release mksk", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&cfg.repoRoot, "repo-root", "", "Repository root. Defaults to git rev-parse --show-toplevel.")
	fs.StringVar(&cfg.toolsTar, "tools-tar", "", "Bazel-built distribution release tools tar.")
	fs.StringVar(&cfg.toolsDir, "tools-dir", "", "Directory containing release tools under bin/.")
	fs.StringVar(&cfg.outRoot, "out-dir", defaultOutDir, "Directory for inspectable release outputs.")
	fs.BoolVar(&cfg.local, "local", false, "Build locally and emit inspection artifacts instead of dispatching the prod release job.")
	fs.BoolVar(&cfg.versionPR, "version-pr", false, "Generate an inspectable release-plz version PR patch in a temporary worktree.")
	fs.StringVar(&cfg.channel, "channel", "", "Release channel: nightly, rc, or stable.")
	fs.StringVar(&cfg.version, "version", "", "Explicit package version. Required for rc and stable; optional for nightly.")
	fs.StringVar(&cfg.sourceRef, "source-ref", defaultSourceRef, "Git ref recorded as the source revision.")
	fs.StringVar(&cfg.builderID, "builder-id", defaultBuilderID, "SLSA builder id to place in local provenance.")
	fs.StringVar(&cfg.platform, "platform", "linux/amd64", "Release platform recorded in provenance.")
	fs.StringVar(&cfg.site, "site", "prod", "Deployment site used for Nomad dispatch.")
	fs.StringVar(&cfg.nomadAddr, "nomad-addr", "", "Nomad HTTP address. Defaults to an SSH tunnel to the selected site.")
	fs.StringVar(&cfg.nomadJobID, "nomad-job", "distribution-release-mksk", "Parameterized Nomad job ID.")
	fs.BoolVar(&cfg.wait, "wait", true, "Wait for the dispatched Nomad release job to finish.")
	fs.DurationVar(&cfg.waitTimeout, "wait-timeout", 2*time.Hour, "Maximum time to wait for the dispatched Nomad release job.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional args: %s", strings.Join(fs.Args(), " "))
	}
	if forceLocal {
		cfg.local = true
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
	if cfg.local {
		out, err := generateReleaseArtifacts(ctx, cfg)
		if err != nil {
			return err
		}
		return stdoutf("release artifacts: %s\n", out.paths.root)
	}
	return dispatchMkskRelease(ctx, cfg)
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

func generateReleaseArtifacts(ctx context.Context, cfg mkskConfig) (releaseOutput, error) {
	if strings.TrimSpace(cfg.channel) == "" {
		return releaseOutput{}, fmt.Errorf("--channel is required unless --version-pr is set")
	}
	sourceCommit := strings.TrimSpace(cfg.sourceCommit)
	var err error
	if sourceCommit == "" {
		sourceCommit, err = resolveCommit(ctx, cfg.repoRoot, cfg.sourceRef)
		if err != nil {
			return releaseOutput{}, err
		}
	}
	source, err := prepareBuildSource(ctx, cfg, sourceCommit)
	if err != nil {
		return releaseOutput{}, err
	}
	defer source.cleanup()
	toolsDir, cleanup, err := resolveTools(cfg)
	if err != nil {
		return releaseOutput{}, err
	}
	defer cleanup()
	bazelisk := filepath.Join(toolsDir, "bin", "bazelisk")

	workspace, err := workspaceVersion(filepath.Join(source.root, mkskCargoManifest))
	if err != nil {
		return releaseOutput{}, err
	}
	version, err := resolveMkskVersion(cfg.channel, cfg.version, workspace, time.Now().UTC())
	if err != nil {
		return releaseOutput{}, err
	}
	platformOS, platformArch, err := parsePlatform(cfg.platform)
	if err != nil {
		return releaseOutput{}, err
	}
	started := time.Now().UTC()
	bazelReleaseVersionFlag := rustReleaseVersionFlag(version)
	testArgs := []string{
		"test",
		bazelReleaseVersionFlag,
		"//src/make-skill:core_test",
		"//src/make-skill:exec_test",
		"//src/make-skill:cli_test",
	}
	if _, err := runCommand(ctx, source.root, bazelisk, testArgs...); err != nil {
		return releaseOutput{}, err
	}
	mkskBinary, err := bazelOutputFile(ctx, bazelisk, source.root, mkskBinaryTarget, bazelReleaseVersionFlag)
	if err != nil {
		return releaseOutput{}, err
	}
	if err := verifyMkskVersion(ctx, mkskBinary, version); err != nil {
		return releaseOutput{}, err
	}
	releaseTar, err := bazelOutputFile(ctx, bazelisk, source.root, mkskReleaseTar, bazelReleaseVersionFlag)
	if err != nil {
		return releaseOutput{}, err
	}
	short := shortSHA(sourceCommit)
	releaseRoot := filepath.Join(cfg.outRoot, mkskPackageName, cfg.channel+"-"+version+"-"+short)
	if strings.TrimSpace(cfg.releaseRoot) != "" {
		releaseRoot = filepath.Clean(cfg.releaseRoot)
	}
	paths := releasePaths{
		root:     releaseRoot,
		artifact: "artifact",
		sbom:     "sbom",
		licenses: "licenses",
		evidence: "evidence",
		tests:    "tests",
	}
	if err := makeReleaseDirs(paths); err != nil {
		return releaseOutput{}, err
	}
	artifactPath := filepath.Join(paths.root, paths.artifact, "make-skill.tar")
	if err := copyFile(releaseTar, artifactPath, 0o644); err != nil {
		return releaseOutput{}, err
	}
	artifactDigest, artifactBytes, err := fileSHA256(artifactPath)
	if err != nil {
		return releaseOutput{}, err
	}
	if err := os.WriteFile(artifactPath+".sha256", []byte(artifactDigest+"  make-skill.tar\n"), 0o644); err != nil {
		return releaseOutput{}, err
	}
	if err := copyTestXML(source.root, paths); err != nil {
		return releaseOutput{}, err
	}
	if err := generateSBOMs(ctx, source.root, toolsDir, artifactPath, version, paths); err != nil {
		return releaseOutput{}, err
	}
	if err := generateLicenses(ctx, source.root, toolsDir, paths); err != nil {
		return releaseOutput{}, err
	}
	finished := time.Now().UTC()
	provenancePath := filepath.Join(paths.root, paths.evidence, "make-skill.provenance.intoto.json")
	if err := writeSLSAProvenance(provenancePath, provenanceInput{
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
		return releaseOutput{}, err
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
		return releaseOutput{}, err
	}
	if err := writeChecksums(paths.root); err != nil {
		return releaseOutput{}, err
	}
	return releaseOutput{
		paths:             paths,
		version:           version,
		channel:           cfg.channel,
		platform:          cfg.platform,
		platformOS:        platformOS,
		platformArch:      platformArch,
		sourceRef:         cfg.sourceRef,
		sourceCommit:      sourceCommit,
		artifactPath:      artifactPath,
		provenancePath:    provenancePath,
		artifactSBOMPath:  filepath.Join(paths.root, paths.sbom, "make-skill.artifact.spdx.json"),
		sourceSBOMPath:    filepath.Join(paths.root, paths.sbom, "make-skill.source.spdx.json"),
		licensesPath:      filepath.Join(paths.root, paths.licenses, "make-skill.cargo-about.json"),
		testEvidencePaths: testEvidencePaths(paths),
	}, nil
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

func generateSBOMs(ctx context.Context, sourceRoot, toolsDir, artifactPath, version string, paths releasePaths) error {
	syft := filepath.Join(toolsDir, "bin", "syft")
	artifactOut := filepath.Join(paths.root, paths.sbom, "make-skill.artifact.spdx.json")
	sourceOut := filepath.Join(paths.root, paths.sbom, "make-skill.source.spdx.json")
	if _, err := runCommand(ctx, sourceRoot, syft, "scan", "file:"+artifactPath, "-q", "--source-name", mkskPackageName, "--source-version", version, "-o", "spdx-json="+artifactOut); err != nil {
		return err
	}
	sourceDir := filepath.Join(sourceRoot, "src", "make-skill")
	if _, err := runCommand(ctx, sourceRoot, syft, "scan", "dir:"+sourceDir, "--exclude", "**/target/**", "-q", "--source-name", mkskPackageName, "--source-version", version, "-o", "spdx-json="+sourceOut); err != nil {
		return err
	}
	return nil
}

func generateLicenses(ctx context.Context, sourceRoot, toolsDir string, paths releasePaths) error {
	cargoAbout := filepath.Join(toolsDir, "bin", "cargo-about")
	out := filepath.Join(paths.root, paths.licenses, "make-skill.cargo-about.json")
	_, err := runCommand(ctx, sourceRoot, cargoAbout, "generate", "--config", mkskCargoAbout, "--manifest-path", mkskCargoManifest, "--workspace", "--frozen", "--format", "json", "--output-file", out)
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

func parsePlatform(platform string) (string, string, error) {
	platform = strings.TrimSpace(platform)
	osPart, archPart, ok := strings.Cut(platform, "/")
	if !ok || strings.TrimSpace(osPart) == "" || strings.TrimSpace(archPart) == "" || strings.Contains(archPart, "/") {
		return "", "", fmt.Errorf("--platform must be formatted as os/arch")
	}
	return osPart, archPart, nil
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

func bazelOutputFile(ctx context.Context, bazelisk, repoRoot, target string, buildOptions ...string) (string, error) {
	buildArgs := append([]string{"build"}, buildOptions...)
	buildArgs = append(buildArgs, target)
	if _, err := runCommand(ctx, repoRoot, bazelisk, buildArgs...); err != nil {
		return "", err
	}
	cqueryArgs := append([]string{"cquery", "--output=files"}, buildOptions...)
	cqueryArgs = append(cqueryArgs, target)
	files, err := runCommand(ctx, repoRoot, bazelisk, cqueryArgs...)
	if err != nil {
		return "", err
	}
	lines := nonEmptyLines(files.stdout)
	if len(lines) != 1 {
		return "", fmt.Errorf("expected one output for %s, got %d", target, len(lines))
	}
	execroot, err := runCommand(ctx, repoRoot, bazelisk, "info", "execution_root")
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
	return runCommandInputEnv(ctx, cwd, program, args, nil, nil)
}

func runCommandEnv(ctx context.Context, cwd, program string, args []string, env map[string]string) (commandResult, error) {
	return runCommandInputEnv(ctx, cwd, program, args, env, nil)
}

func runCommandInputEnv(ctx context.Context, cwd, program string, args []string, env map[string]string, stdin io.Reader) (commandResult, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	if len(env) > 0 {
		cmd.Env = os.Environ()
		for key, value := range env {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	if stdin != nil {
		cmd.Stdin = stdin
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
	if err := verifyReleaseTools(dir); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return dir, cleanup, nil
}

func resolveTools(cfg mkskConfig) (string, func(), error) {
	if strings.TrimSpace(cfg.toolsDir) != "" {
		dir := filepath.Clean(cfg.toolsDir)
		if err := verifyReleaseTools(dir); err != nil {
			return "", func() {}, err
		}
		return dir, func() {}, nil
	}
	if strings.TrimSpace(cfg.toolsTar) == "" {
		return "", func() {}, fmt.Errorf("--tools-tar or --tools-dir is required")
	}
	return extractTools(cfg.toolsTar)
}

func verifyReleaseTools(dir string) error {
	for _, tool := range []string{"bazelisk", "cargo-about", "cosign", "oras", "release-plz", "syft"} {
		path := filepath.Join(dir, "bin", tool)
		if st, err := os.Stat(path); err != nil {
			return fmt.Errorf("release tool %s: %w", tool, err)
		} else if st.Mode()&0o111 == 0 {
			return fmt.Errorf("release tool %s is not executable", tool)
		}
	}
	return nil
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

func cleanRootUnder(path, prefix string) (string, error) {
	cleanPath := filepath.Clean(path)
	cleanPrefix := filepath.Clean(prefix)
	if !filepath.IsAbs(cleanPath) {
		return "", fmt.Errorf("%s must be absolute", path)
	}
	if cleanPath == cleanPrefix || !strings.HasPrefix(cleanPath, cleanPrefix+string(filepath.Separator)) {
		return "", fmt.Errorf("%s must be under %s", cleanPath, cleanPrefix)
	}
	return cleanPath, nil
}

func lookupOwner(userName, groupName string) (int, int, error) {
	u, err := user.Lookup(userName)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup user %s: %w", userName, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse uid for %s: %w", userName, err)
	}
	g, err := user.LookupGroup(groupName)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup group %s: %w", groupName, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse gid for %s: %w", groupName, err)
	}
	return uid, gid, nil
}

func ensureDir(path string, mode fs.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

func ensureOwnedDir(path string, mode fs.FileMode, uid, gid int) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
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

func testEvidencePaths(paths releasePaths) []string {
	names := []string{"cli_test.xml", "core_test.xml", "exec_test.xml"}
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, filepath.Join(paths.root, paths.tests, name))
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

func shortSHA(value string) string {
	if len(value) < 12 {
		return value
	}
	return value[:12]
}

func utcStamp(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}
