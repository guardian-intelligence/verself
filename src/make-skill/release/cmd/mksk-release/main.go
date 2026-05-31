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
	packageName                 = "mksk"
	cargoManifest               = "src/make-skill/Cargo.toml"
	cargoAboutConfig            = "src/make-skill/about.toml"
	releaseTarTarget            = "//src/make-skill:release_tar"
	binaryTarget                = "//src/make-skill:mksk"
	releaseVersionEnv           = "MKSK_RELEASE_VERSION"
	defaultOutputRoot           = "artifacts/releases"
	defaultSourceRef            = "HEAD"
	defaultBuilderID            = "spiffe://prod.verself.sh/svc/release-builder"
	defaultFlavor               = "default"
	defaultPlatform             = "linux/amd64"
	metadataBaseURL             = "https://oci.verself.sh/releases/mksk"
	sourceRepository            = "https://github.com/guardian-intelligence/verself.git"
	defaultRegistryURL          = "http://127.0.0.1:5080"
	defaultPublicURL            = "https://oci.verself.sh"
	defaultRepository           = "verself/mksk"
	defaultRegistryUser         = "artifact-publisher"
	defaultRegistryPasswordFile = "/etc/zot/publisher-password"
	rulesRustToolsRepo          = "rules_rust++rust+rust_linux_x86_64__x86_64-unknown-linux-gnu__stable_tools"
)

var (
	finalSemVerRE   = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	rcSemVerRE      = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-rc\.(0|[1-9][0-9]*)$`)
	nightlySemVerRE = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-nightly\.[0-9]{8}\.(0|[1-9][0-9]*)$`)
	gitCommitRE     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	tokenRE         = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*[a-z0-9]$`)
)

type Channel string

const (
	ChannelNightly Channel = "nightly"
	ChannelRC      Channel = "rc"
	ChannelStable  Channel = "stable"
)

type Platform struct {
	OS   string
	Arch string
}

func (p Platform) String() string {
	return p.OS + "/" + p.Arch
}

func (p Platform) token() string {
	return p.OS + "-" + p.Arch
}

type ReleaseSubject struct {
	Package          string
	Version          string
	Channel          Channel
	SourceRepository string
	SourceRef        string
	SourceCommit     string
	Platform         Platform
	Flavor           string
}

type BuildRequest struct {
	RepoRoot   string
	ToolsTar   string
	OutputRoot string
	Subject    ReleaseSubject
	BuilderID  string
	AllowDirty bool
}

type EvidenceFile struct {
	Path      string
	Digest    string
	SizeBytes int64
	MediaType string
}

type EvidenceSet struct {
	Artifact        EvidenceFile
	SourceSBOM      EvidenceFile
	ArtifactSBOM    EvidenceFile
	LicenseEvidence EvidenceFile
	Provenance      EvidenceFile
}

type BuildBundle struct {
	Subject  ReleaseSubject
	Root     string
	Evidence EvidenceSet
}

type releasePaths struct {
	root     string
	artifact string
	sbom     string
	licenses string
	evidence string
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
		fmt.Fprintln(os.Stderr, "mksk-release: "+err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing subcommand: build")
	}
	switch args[0] {
	case "build":
		return runBuild(ctx, args[1:])
	case "publish":
		return runPublish(ctx, args[1:])
	case "admit", "tag":
		return runFutureSubcommand(args[0])
	case "-h", "--help", "help":
		printUsage(os.Stdout)
		return nil
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, `mksk-release <subcommand> [flags]

Subcommands:
  build    Build make-skill artifacts and standard evidence without publishing.
  publish  Build then publish artifact and evidence to OCI.
  admit    Ask distribution-service to admit a published digest (not yet implemented).
  tag      Create post-admission release tags (not yet implemented).
`)
}

func runBuild(ctx context.Context, args []string) error {
	req, err := parseBuildRequest(ctx, args)
	if err != nil {
		return err
	}
	bundle, err := buildBundle(ctx, req)
	if err != nil {
		return err
	}
	return stdoutf("build bundle: %s\n", bundle.Root)
}

func runPublish(ctx context.Context, args []string) error {
	req, err := parsePublishRequest(ctx, args)
	if err != nil {
		return err
	}
	if err := req.Signer.Preflight(ctx); err != nil {
		return err
	}
	credential, err := registryCredential(req.RegistryUsername, req.RegistryPasswordFile)
	if err != nil {
		return err
	}
	bundle, err := buildBundle(ctx, req.Build)
	if err != nil {
		return err
	}
	target, err := newPublishTarget(req, credential)
	if err != nil {
		return err
	}
	result, err := publishBundle(ctx, target, req, bundle)
	if err != nil {
		return err
	}
	publicRef := publicOCIReference(req.PublicRegistryURL, req.Repository, result.Subject.Descriptor.Digest.String())
	envelope, err := newReleaseSigningEnvelope(bundle, publicRef, result, req.Ceremony)
	if err != nil {
		return err
	}
	signature, err := req.Signer.SignRelease(ctx, envelope)
	if err != nil {
		return err
	}
	return stdoutf(
		"build bundle: %s\noci repository: %s/%s\noci reference: %s\noci digest: %s\noci referrers: %d\nrelease input digest: %s\nsignature: %s\nsigning payload sha256: %s\n",
		bundle.Root,
		strings.TrimRight(req.RegistryURL, "/"),
		strings.Trim(req.Repository, "/"),
		publicRef,
		result.Subject.Descriptor.Digest,
		len(result.Referrers),
		envelope.ReleaseInputDigest,
		signature.Mode,
		signature.PayloadDigest,
	)
}

func runFutureSubcommand(name string) error {
	return fmt.Errorf("%s is not implemented yet", name)
}

type releaseFlagValues struct {
	repoRoot     string
	toolsTar     string
	outputRoot   string
	sourceRef    string
	sourceCommit string
	channel      string
	version      string
	platform     string
	flavor       string
	builderID    string
	fromRC       string
	nightly      bool
	rc           bool
	stable       bool
	allowDirty   bool
}

func parseBuildRequest(ctx context.Context, args []string) (BuildRequest, error) {
	return parseReleaseBuildRequest(ctx, args, "mksk-release build")
}

func parseReleaseBuildRequest(ctx context.Context, args []string, commandName string) (BuildRequest, error) {
	values := defaultReleaseFlagValues()
	fs := flag.NewFlagSet(commandName, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	bindReleaseFlags(fs, &values)
	if err := fs.Parse(args); err != nil {
		return BuildRequest{}, err
	}
	if fs.NArg() != 0 {
		return BuildRequest{}, fmt.Errorf("unexpected positional args: %s", strings.Join(fs.Args(), " "))
	}
	return buildRequestFromReleaseFlags(ctx, values)
}

func defaultReleaseFlagValues() releaseFlagValues {
	return releaseFlagValues{
		outputRoot: defaultOutputRoot,
		sourceRef:  defaultSourceRef,
		platform:   defaultPlatform,
		flavor:     defaultFlavor,
		builderID:  defaultBuilderID,
	}
}

func bindReleaseFlags(fs *flag.FlagSet, values *releaseFlagValues) {
	fs.StringVar(&values.repoRoot, "repo-root", "", "Repository root. Defaults to git rev-parse --show-toplevel.")
	fs.StringVar(&values.toolsTar, "tools-tar", "", "Bazel-built make-skill release tools tar.")
	fs.StringVar(&values.outputRoot, "out-dir", defaultOutputRoot, "Directory for build bundle outputs.")
	fs.StringVar(&values.sourceRef, "source-ref", defaultSourceRef, "Git ref recorded as release source.")
	fs.StringVar(&values.sourceCommit, "source-commit", "", "Resolved 40-character git source commit. Only valid with --channel/--version.")
	fs.StringVar(&values.channel, "channel", "", "Exact release channel: nightly, rc, or stable.")
	fs.StringVar(&values.version, "version", "", "Exact release version.")
	fs.StringVar(&values.platform, "platform", defaultPlatform, "Release platform as OS/ARCH.")
	fs.StringVar(&values.flavor, "flavor", defaultFlavor, "Opaque release flavor token.")
	fs.StringVar(&values.builderID, "builder-id", defaultBuilderID, "SLSA builder id recorded in provenance.")
	fs.StringVar(&values.fromRC, "from-rc", "", "RC tag to rebuild as a stable release, e.g. mksk-v0.2.0-rc.2.")
	fs.BoolVar(&values.nightly, "nightly", false, "Derive the next nightly version from --source-ref.")
	fs.BoolVar(&values.rc, "rc", false, "Derive the next RC version from --source-ref.")
	fs.BoolVar(&values.stable, "stable", false, "Derive a stable version from --source-ref or --from-rc.")
	fs.BoolVar(&values.allowDirty, "allow-dirty", false, "Allow building from a dirty local workspace for inspection.")
}

func buildRequestFromReleaseFlags(ctx context.Context, values releaseFlagValues) (BuildRequest, error) {
	repoRoot := strings.TrimSpace(values.repoRoot)
	if err := fillRepoRoot(ctx, &repoRoot); err != nil {
		return BuildRequest{}, err
	}
	outputRoot := absoluteFromRepo(repoRoot, values.outputRoot)
	intent, err := releaseIntentFromFlags(values)
	if err != nil {
		return BuildRequest{}, err
	}
	platform, err := parsePlatform(values.platform)
	if err != nil {
		return BuildRequest{}, err
	}
	subject, err := intent.deriveSubject(ctx, releaseDerivationConfig{
		RepoRoot:   repoRoot,
		OutputRoot: outputRoot,
		Platform:   platform,
		Flavor:     values.flavor,
		Now:        time.Now().UTC(),
	})
	if err != nil {
		return BuildRequest{}, err
	}
	if err := validateSubject(subject); err != nil {
		return BuildRequest{}, err
	}
	req := BuildRequest{
		RepoRoot:   repoRoot,
		ToolsTar:   strings.TrimSpace(values.toolsTar),
		OutputRoot: outputRoot,
		Subject:    subject,
		BuilderID:  strings.TrimSpace(values.builderID),
		AllowDirty: values.allowDirty,
	}
	if req.ToolsTar == "" {
		return BuildRequest{}, fmt.Errorf("--tools-tar is required")
	}
	if req.BuilderID == "" {
		return BuildRequest{}, fmt.Errorf("--builder-id is required")
	}
	return req, nil
}

func releaseIntentFromFlags(values releaseFlagValues) (releaseIntent, error) {
	exactRequested := strings.TrimSpace(values.channel) != "" || strings.TrimSpace(values.version) != "" || strings.TrimSpace(values.sourceCommit) != ""
	intentCount := 0
	for _, enabled := range []bool{values.nightly, values.rc, values.stable, exactRequested} {
		if enabled {
			intentCount++
		}
	}
	if intentCount != 1 {
		return nil, fmt.Errorf("choose exactly one release intent: --nightly, --rc, --stable, or --channel with --version")
	}
	if values.fromRC != "" && !values.stable {
		return nil, fmt.Errorf("--from-rc is only valid with --stable")
	}
	switch {
	case values.nightly:
		return nightlyIntent{SourceRef: values.sourceRef}, nil
	case values.rc:
		return rcIntent{SourceRef: values.sourceRef}, nil
	case values.stable:
		return stableIntent{SourceRef: values.sourceRef, FromRC: strings.TrimSpace(values.fromRC)}, nil
	default:
		if strings.TrimSpace(values.channel) == "" || strings.TrimSpace(values.version) == "" {
			return nil, fmt.Errorf("--channel and --version are required for an exact release subject")
		}
		channel, err := parseChannel(values.channel)
		if err != nil {
			return nil, err
		}
		return exactIntent{
			Channel:      channel,
			Version:      strings.TrimSpace(values.version),
			SourceRef:    values.sourceRef,
			SourceCommit: strings.TrimSpace(values.sourceCommit),
		}, nil
	}
}

func buildBundle(ctx context.Context, req BuildRequest) (BuildBundle, error) {
	source, err := prepareBuildSource(ctx, req)
	if err != nil {
		return BuildBundle{}, err
	}
	defer source.cleanup()
	toolsDir, cleanup, err := extractTools(req.ToolsTar, []string{"bazelisk", "cargo-about", "syft"})
	if err != nil {
		return BuildBundle{}, err
	}
	defer cleanup()
	bazelisk := filepath.Join(toolsDir, "bin", "bazelisk")
	toolEnv, err := releaseToolEnv(req.OutputRoot, req.Subject)
	if err != nil {
		return BuildBundle{}, err
	}
	startupOptions := releaseBazelStartupOptions(toolEnv)
	commandOptions := releaseBazelCommandOptions(toolEnv)
	started := time.Now().UTC()
	releaseVersionFlag := rustReleaseVersionFlag(req.Subject.Version)
	testArgs := append([]string{}, startupOptions...)
	testArgs = append(testArgs, "test")
	testArgs = append(testArgs, commandOptions...)
	testArgs = append(testArgs,
		releaseVersionFlag,
		"//src/make-skill:core_test",
		"//src/make-skill:exec_test",
		"//src/make-skill:cli_test",
	)
	if _, err := runCommandWithEnv(ctx, source.root, bazelisk, toolEnv, testArgs...); err != nil {
		return BuildBundle{}, err
	}
	mkskBinary, err := bazelOutputFile(ctx, source.root, bazelisk, toolEnv, startupOptions, commandOptions, binaryTarget, releaseVersionFlag)
	if err != nil {
		return BuildBundle{}, err
	}
	if err := verifyMkskVersion(ctx, mkskBinary, req.Subject.Version); err != nil {
		return BuildBundle{}, err
	}
	releaseTar, err := bazelOutputFile(ctx, source.root, bazelisk, toolEnv, startupOptions, commandOptions, releaseTarTarget, releaseVersionFlag)
	if err != nil {
		return BuildBundle{}, err
	}
	execRoot, err := bazelExecutionRoot(ctx, source.root, bazelisk, toolEnv, startupOptions, commandOptions)
	if err != nil {
		return BuildBundle{}, err
	}
	rustToolsBin, err := releaseRustToolsBinDir(execRoot)
	if err != nil {
		return BuildBundle{}, err
	}
	paths := releasePaths{
		root:     filepath.Join(req.OutputRoot, packageName, buildRootName(req.Subject)),
		artifact: "artifact",
		sbom:     "sbom",
		licenses: "licenses",
		evidence: "evidence",
	}
	if err := makeReleaseDirs(paths); err != nil {
		return BuildBundle{}, err
	}
	artifactPath := filepath.Join(paths.root, paths.artifact, "make-skill.tar")
	if err := copyFile(releaseTar, artifactPath, 0o644); err != nil {
		return BuildBundle{}, err
	}
	artifactFile, err := evidenceFile(artifactPath, "application/vnd.verself.mksk.release.tar")
	if err != nil {
		return BuildBundle{}, err
	}
	if err := os.WriteFile(artifactPath+".sha256", []byte(artifactFile.Digest+"  make-skill.tar\n"), 0o644); err != nil {
		return BuildBundle{}, err
	}
	sourceSBOM, artifactSBOM, err := generateSBOMs(ctx, source.root, toolsDir, artifactPath, req.Subject.Version, paths, toolEnv)
	if err != nil {
		return BuildBundle{}, err
	}
	licenseEvidence, err := generateLicenses(ctx, source.root, toolsDir, rustToolsBin, paths, toolEnv)
	if err != nil {
		return BuildBundle{}, err
	}
	finished := time.Now().UTC()
	provenancePath := filepath.Join(paths.root, paths.evidence, "make-skill.provenance.intoto.json")
	if err := writeSLSAProvenance(provenancePath, provenanceInput{
		artifactDigest:  artifactFile.Digest,
		artifactBytes:   artifactFile.SizeBytes,
		builderID:       req.BuilderID,
		subject:         req.Subject,
		workspaceDirty:  source.dirty,
		started:         started,
		finished:        finished,
		invocationID:    "build-" + utcStamp(started) + "-" + shortSHA(req.Subject.SourceCommit),
		releaseTarLabel: releaseTarTarget,
	}); err != nil {
		return BuildBundle{}, err
	}
	provenance, err := evidenceFile(provenancePath, "application/vnd.in-toto+json")
	if err != nil {
		return BuildBundle{}, err
	}
	if err := writeText(filepath.Join(paths.root, "README.txt"), []string{
		"make-skill release build bundle",
		"package=" + req.Subject.Package,
		"version=" + req.Subject.Version,
		"channel=" + string(req.Subject.Channel),
		"platform=" + req.Subject.Platform.String(),
		"flavor=" + req.Subject.Flavor,
		"source_repository=" + req.Subject.SourceRepository,
		"source_ref=" + req.Subject.SourceRef,
		"source_commit=" + req.Subject.SourceCommit,
		fmt.Sprintf("workspace_dirty=%t", source.dirty),
		"builder_id=" + req.BuilderID,
		"bazel_target=" + releaseTarTarget,
		"release_metadata_url=" + releaseMetadataURL(req.Subject.Version),
		"artifact=artifact/make-skill.tar",
		"artifact_sha256=" + artifactFile.Digest,
		"sbom_artifact=sbom/make-skill.artifact.spdx.json",
		"sbom_source=sbom/make-skill.source.spdx.json",
		"licenses=licenses/make-skill.cargo-about.json",
		"provenance=evidence/make-skill.provenance.intoto.json",
	}); err != nil {
		return BuildBundle{}, err
	}
	if err := writeChecksums(paths.root); err != nil {
		return BuildBundle{}, err
	}
	return BuildBundle{
		Subject: req.Subject,
		Root:    paths.root,
		Evidence: EvidenceSet{
			Artifact:        artifactFile,
			SourceSBOM:      sourceSBOM,
			ArtifactSBOM:    artifactSBOM,
			LicenseEvidence: licenseEvidence,
			Provenance:      provenance,
		},
	}, nil
}

func prepareBuildSource(ctx context.Context, req BuildRequest) (buildSource, error) {
	headCommit, err := resolveCommit(ctx, req.RepoRoot, "HEAD")
	if err != nil {
		return buildSource{}, err
	}
	if req.Subject.SourceCommit == headCommit {
		dirty, err := dirtyWorkspace(ctx, req.RepoRoot)
		if err != nil {
			return buildSource{}, err
		}
		if dirty && !req.AllowDirty {
			return buildSource{}, fmt.Errorf("workspace is dirty; commit changes or pass --allow-dirty for inspection")
		}
		return buildSource{root: req.RepoRoot, dirty: dirty, cleanup: func() {}}, nil
	}
	worktree, cleanup, err := addWorktree(ctx, req.RepoRoot, req.Subject.SourceCommit, "mksk-build-"+shortSHA(req.Subject.SourceCommit))
	if err != nil {
		return buildSource{}, err
	}
	return buildSource{root: worktree, dirty: false, cleanup: cleanup}, nil
}

func validateSubject(subject ReleaseSubject) error {
	if subject.Package != packageName {
		return fmt.Errorf("package must be %q", packageName)
	}
	if subject.SourceCommit == "" || !gitCommitRE.MatchString(subject.SourceCommit) {
		return fmt.Errorf("source commit must be a 40-character lowercase git sha")
	}
	if subject.SourceRepository == "" {
		return fmt.Errorf("source repository is required")
	}
	if subject.SourceRef == "" {
		return fmt.Errorf("source ref is required")
	}
	if subject.Flavor == "" {
		return fmt.Errorf("flavor is required")
	}
	if !tokenRE.MatchString(subject.Flavor) {
		return fmt.Errorf("flavor must match %s", tokenRE.String())
	}
	switch subject.Channel {
	case ChannelNightly:
		if !nightlySemVerRE.MatchString(subject.Version) {
			return fmt.Errorf("nightly version must match MAJOR.MINOR.PATCH-nightly.YYYYMMDD.N")
		}
	case ChannelRC:
		if !rcSemVerRE.MatchString(subject.Version) {
			return fmt.Errorf("rc version must match MAJOR.MINOR.PATCH-rc.N")
		}
	case ChannelStable:
		if !finalSemVerRE.MatchString(subject.Version) {
			return fmt.Errorf("stable version must be final SemVer MAJOR.MINOR.PATCH")
		}
	default:
		return fmt.Errorf("channel must be nightly, rc, or stable")
	}
	if subject.Platform.String() != defaultPlatform {
		return fmt.Errorf("unsupported platform %s", subject.Platform.String())
	}
	return nil
}

func parseChannel(value string) (Channel, error) {
	switch Channel(strings.TrimSpace(value)) {
	case ChannelNightly:
		return ChannelNightly, nil
	case ChannelRC:
		return ChannelRC, nil
	case ChannelStable:
		return ChannelStable, nil
	default:
		return "", fmt.Errorf("--channel must be nightly, rc, or stable")
	}
}

func parsePlatform(value string) (Platform, error) {
	osPart, archPart, ok := strings.Cut(strings.TrimSpace(value), "/")
	if !ok || osPart == "" || archPart == "" {
		return Platform{}, fmt.Errorf("--platform must be OS/ARCH")
	}
	platform := Platform{OS: osPart, Arch: archPart}
	if platform.String() != defaultPlatform {
		return Platform{}, fmt.Errorf("unsupported platform %s", platform.String())
	}
	return platform, nil
}

func buildRootName(subject ReleaseSubject) string {
	return strings.Join([]string{
		"build",
		string(subject.Channel),
		subject.Version,
		subject.Platform.token(),
		subject.Flavor,
		shortSHA(subject.SourceCommit),
	}, "-")
}

func makeReleaseDirs(paths releasePaths) error {
	if strings.TrimSpace(paths.root) == "" || filepath.Clean(paths.root) == string(filepath.Separator) {
		return fmt.Errorf("refusing to clear invalid release output root %q", paths.root)
	}
	if err := os.RemoveAll(paths.root); err != nil {
		return fmt.Errorf("clear release output root %s: %w", paths.root, err)
	}
	for _, rel := range []string{"", paths.artifact, paths.sbom, paths.licenses, paths.evidence} {
		if err := os.MkdirAll(filepath.Join(paths.root, rel), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func generateSBOMs(ctx context.Context, sourceRoot, toolsDir, artifactPath, version string, paths releasePaths, env map[string]string) (EvidenceFile, EvidenceFile, error) {
	syft := filepath.Join(toolsDir, "bin", "syft")
	artifactOut := filepath.Join(paths.root, paths.sbom, "make-skill.artifact.spdx.json")
	sourceOut := filepath.Join(paths.root, paths.sbom, "make-skill.source.spdx.json")
	if _, err := runCommandWithEnv(ctx, sourceRoot, syft, env, "scan", "file:"+artifactPath, "-q", "--source-name", packageName, "--source-version", version, "-o", "spdx-json="+artifactOut); err != nil {
		return EvidenceFile{}, EvidenceFile{}, err
	}
	sourceDir := filepath.Join(sourceRoot, "src", "make-skill")
	if _, err := runCommandWithEnv(ctx, sourceRoot, syft, env, "scan", "dir:"+sourceDir, "--exclude", "**/target/**", "-q", "--source-name", packageName, "--source-version", version, "-o", "spdx-json="+sourceOut); err != nil {
		return EvidenceFile{}, EvidenceFile{}, err
	}
	artifactSBOM, err := evidenceFile(artifactOut, "application/spdx+json")
	if err != nil {
		return EvidenceFile{}, EvidenceFile{}, err
	}
	sourceSBOM, err := evidenceFile(sourceOut, "application/spdx+json")
	if err != nil {
		return EvidenceFile{}, EvidenceFile{}, err
	}
	return sourceSBOM, artifactSBOM, nil
}

func generateLicenses(ctx context.Context, sourceRoot, toolsDir, rustToolsBin string, paths releasePaths, env map[string]string) (EvidenceFile, error) {
	cargoAbout := filepath.Join(toolsDir, "bin", "cargo-about")
	out := filepath.Join(paths.root, paths.licenses, "make-skill.cargo-about.json")
	licenseEnv := commandEnvWithPrependedPath(env, filepath.Join(toolsDir, "bin"), rustToolsBin)
	if _, err := runCommandWithEnv(ctx, sourceRoot, cargoAbout, licenseEnv, "generate", "--config", cargoAboutConfig, "--manifest-path", cargoManifest, "--workspace", "--frozen", "--format", "json", "--output-file", out); err != nil {
		return EvidenceFile{}, err
	}
	return evidenceFile(out, "application/json")
}

type provenanceInput struct {
	artifactDigest  string
	artifactBytes   int64
	builderID       string
	subject         ReleaseSubject
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
					"channel":     string(input.subject.Channel),
					"flavor":      input.subject.Flavor,
					"package":     input.subject.Package,
					"platform":    input.subject.Platform.String(),
					"releaseURL":  releaseMetadataURL(input.subject.Version),
					"sourceRef":   input.subject.SourceRef,
					"version":     input.subject.Version,
				},
				"internalParameters": map[string]any{
					"workspaceDirty": input.workspaceDirty,
				},
				"resolvedDependencies": []map[string]any{
					{
						"uri": input.subject.SourceRepository,
						"digest": map[string]string{
							"gitCommit": input.subject.SourceCommit,
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

func fillRepoRoot(ctx context.Context, repoRoot *string) error {
	if strings.TrimSpace(*repoRoot) != "" {
		*repoRoot = filepath.Clean(*repoRoot)
		return nil
	}
	root, err := gitOutput(ctx, "", "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	*repoRoot = filepath.Clean(root)
	return nil
}

func absoluteFromRepo(repoRoot, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(repoRoot, path)
}

func rustReleaseVersionFlag(version string) string {
	return "--@rules_rust//rust/settings:extra_rustc_env=" + releaseVersionEnv + "=" + version
}

func releaseMetadataURL(version string) string {
	return metadataBaseURL + "/" + version
}

func verifyMkskVersion(ctx context.Context, binaryPath, version string) error {
	result, err := runCommand(ctx, "", binaryPath, "--version")
	if err != nil {
		return err
	}
	lines := nonEmptyLines(result.stdout)
	if len(lines) < 2 {
		return fmt.Errorf("mksk --version returned %d non-empty lines, want release metadata", len(lines))
	}
	if want := "mksk " + version; lines[0] != want {
		return fmt.Errorf("mksk --version line = %q, want %q", lines[0], want)
	}
	if want := "release: " + releaseMetadataURL(version); lines[1] != want {
		return fmt.Errorf("mksk --version release line = %q, want %q", lines[1], want)
	}
	return nil
}

func resolveCommit(ctx context.Context, repoRoot, ref string) (string, error) {
	out, err := gitOutput(ctx, repoRoot, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	if !gitCommitRE.MatchString(out) {
		return "", fmt.Errorf("git ref %q resolved to invalid commit %q", ref, out)
	}
	return out, nil
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

func addWorktree(ctx context.Context, repoRoot, sourceCommit, name string) (string, func(), error) {
	worktree, err := os.MkdirTemp("", "verself-"+name+"-")
	if err != nil {
		return "", nil, err
	}
	_ = os.Remove(worktree)
	cleanup := func() {
		_, _ = runCommand(context.Background(), repoRoot, "git", "worktree", "remove", "--force", worktree)
	}
	if _, err := runCommand(ctx, repoRoot, "git", "worktree", "add", "--detach", worktree, sourceCommit); err != nil {
		cleanup()
		return "", nil, err
	}
	return worktree, cleanup, nil
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
	cqueryArgs = append(cqueryArgs, buildOptions...)
	cqueryArgs = append(cqueryArgs, "--output=files", target)
	files, err := runCommandWithEnv(ctx, repoRoot, bazelisk, env, cqueryArgs...)
	if err != nil {
		return "", err
	}
	lines := nonEmptyLines(files.stdout)
	if len(lines) != 1 {
		return "", fmt.Errorf("expected one output for %s, got %d", target, len(lines))
	}
	execRoot, err := bazelExecutionRoot(ctx, repoRoot, bazelisk, env, startupOptions, commandOptions)
	if err != nil {
		return "", err
	}
	out := filepath.Join(execRoot, lines[0])
	if _, err := os.Stat(out); err != nil {
		return "", fmt.Errorf("bazel output %s: %w", out, err)
	}
	return out, nil
}

func bazelExecutionRoot(ctx context.Context, repoRoot, bazelisk string, env map[string]string, startupOptions []string, commandOptions []string) (string, error) {
	infoArgs := append([]string{}, startupOptions...)
	infoArgs = append(infoArgs, "info")
	infoArgs = append(infoArgs, commandOptions...)
	infoArgs = append(infoArgs, "execution_root")
	execRoot, err := runCommandWithEnv(ctx, repoRoot, bazelisk, env, infoArgs...)
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(execRoot.stdout)
	if root == "" {
		return "", fmt.Errorf("bazel info execution_root returned empty output")
	}
	return root, nil
}

func runCommand(ctx context.Context, cwd, program string, args ...string) (commandResult, error) {
	return runCommandWithEnv(ctx, cwd, program, nil, args...)
}

func runCommandWithEnv(ctx context.Context, cwd, program string, env map[string]string, args ...string) (commandResult, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	if strings.TrimSpace(cwd) != "" {
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

func releaseToolEnv(outRoot string, subject ReleaseSubject) (map[string]string, error) {
	root := filepath.Join(outRoot, "work", "tool-env", packageName, buildRootName(subject))
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

func releaseBazelStartupOptions(env map[string]string) []string {
	return []string{
		"--output_user_root=" + filepath.Join(env["XDG_CACHE_HOME"], "bazel-output"),
		// Bazel servers inherit captured command pipes; release builds need them to exit.
		"--max_idle_secs=1",
	}
}

func releaseBazelCommandOptions(env map[string]string) []string {
	return []string{
		"--disk_cache=" + filepath.Join(env["XDG_CACHE_HOME"], "bazel-disk"),
		"--repository_cache=" + filepath.Join(env["XDG_CACHE_HOME"], "bazel-repo"),
		// The release bundle lives under the workspace; Bazel rejects repo contents caches there.
		"--repo_contents_cache=",
	}
}

func releaseRustToolsBinDir(executionRoot string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(executionRoot, "external", rulesRustToolsRepo, "bin", "rustc"))
	if err != nil {
		return "", err
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("expected one rustc in %s, got %d", filepath.Join(executionRoot, "external", rulesRustToolsRepo, "bin"), len(matches))
	}
	return filepath.Dir(matches[0]), nil
}

func commandEnvWithPrependedPath(env map[string]string, dirs ...string) map[string]string {
	out := make(map[string]string, len(env)+1)
	for key, value := range env {
		out[key] = value
	}
	pathValue := os.Getenv("PATH")
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok && key == "PATH" {
			pathValue = value
			break
		}
	}
	prefix := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if strings.TrimSpace(dir) != "" {
			prefix = append(prefix, dir)
		}
	}
	if len(prefix) > 0 {
		pathValue = strings.Join(append(prefix, pathValue), string(os.PathListSeparator))
	}
	out["PATH"] = pathValue
	return out
}

func mergeCommandEnv(base []string, overrides map[string]string) []string {
	out := make([]string, 0, len(base)+len(overrides))
	seen := map[string]bool{}
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, exists := overrides[key]; exists {
			continue
		}
		seen[key] = true
		out = append(out, entry)
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		if seen[key] {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = append(out, key+"="+overrides[key])
	}
	return out
}

func extractTools(toolsTar string, tools []string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "verself-mksk-release-tools-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() {
		_ = os.RemoveAll(dir)
	}
	if err := extractPlainTar(toolsTar, dir); err != nil {
		cleanup()
		return "", nil, err
	}
	for _, tool := range tools {
		if err := requireExecutable(filepath.Join(dir, "bin", tool), "release tool "+tool); err != nil {
			cleanup()
			return "", nil, err
		}
	}
	return dir, cleanup, nil
}

func requireExecutable(path, label string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s %s: %w", label, path, err)
	}
	if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%s %s is not executable", label, path)
	}
	return nil
}

func extractPlainTar(path, dest string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	tr := tar.NewReader(file)
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
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported tar entry %s type %d", header.Name, header.Typeflag)
		}
	}
}

func tarPermissionMode(header *tar.Header) (fs.FileMode, error) {
	if header.Mode < 0 {
		return 0, fmt.Errorf("tar entry %s has negative mode", header.Name)
	}
	return fs.FileMode(header.Mode).Perm(), nil
}

func stdoutf(format string, args ...any) error {
	_, err := fmt.Fprintf(os.Stdout, format, args...)
	return err
}

func safeJoin(root, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe path %q", name)
	}
	return filepath.Join(root, clean), nil
}

func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func evidenceFile(path, mediaType string) (EvidenceFile, error) {
	digest, size, err := fileSHA256(path)
	if err != nil {
		return EvidenceFile{}, err
	}
	return EvidenceFile{
		Path:      path,
		Digest:    digest,
		SizeBytes: size,
		MediaType: mediaType,
	}, nil
}

func fileSHA256(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	h := sha256.New()
	n, err := io.Copy(h, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func writeChecksums(root string) error {
	var lines []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if filepath.ToSlash(rel) == "checksums.sha256" {
			return nil
		}
		sum, _, err := fileSHA256(path)
		if err != nil {
			return err
		}
		lines = append(lines, sum+"  "+filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
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
