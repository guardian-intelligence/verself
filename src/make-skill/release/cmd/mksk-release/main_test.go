package main

import (
	"archive/tar"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"oras.land/oras-go/v2/content/memory"
)

func TestValidateSubject(t *testing.T) {
	commit := strings.Repeat("a", 40)
	platform := Platform{OS: "linux", Arch: "amd64"}
	tests := []struct {
		name    string
		subject ReleaseSubject
		wantErr bool
	}{
		{
			name: "nightly",
			subject: ReleaseSubject{
				Package:          packageName,
				Version:          "0.2.0-nightly.20260527.1",
				Channel:          ChannelNightly,
				SourceRepository: sourceRepository,
				SourceRef:        "HEAD",
				SourceCommit:     commit,
				Platform:         platform,
				Flavor:           defaultFlavor,
			},
		},
		{
			name: "rc",
			subject: ReleaseSubject{
				Package:          packageName,
				Version:          "0.2.0-rc.1",
				Channel:          ChannelRC,
				SourceRepository: sourceRepository,
				SourceRef:        "HEAD",
				SourceCommit:     commit,
				Platform:         platform,
				Flavor:           "glibc-default",
			},
		},
		{
			name: "stable",
			subject: ReleaseSubject{
				Package:          packageName,
				Version:          "0.2.0",
				Channel:          ChannelStable,
				SourceRepository: sourceRepository,
				SourceRef:        "HEAD",
				SourceCommit:     commit,
				Platform:         platform,
				Flavor:           defaultFlavor,
			},
		},
		{
			name: "stable rejects rc bytes",
			subject: ReleaseSubject{
				Package:          packageName,
				Version:          "0.2.0-rc.1",
				Channel:          ChannelStable,
				SourceRepository: sourceRepository,
				SourceRef:        "HEAD",
				SourceCommit:     commit,
				Platform:         platform,
				Flavor:           defaultFlavor,
			},
			wantErr: true,
		},
		{
			name: "rc rejects final",
			subject: ReleaseSubject{
				Package:          packageName,
				Version:          "0.2.0",
				Channel:          ChannelRC,
				SourceRepository: sourceRepository,
				SourceRef:        "HEAD",
				SourceCommit:     commit,
				Platform:         platform,
				Flavor:           defaultFlavor,
			},
			wantErr: true,
		},
		{
			name: "requires flavor",
			subject: ReleaseSubject{
				Package:          packageName,
				Version:          "0.2.0",
				Channel:          ChannelStable,
				SourceRepository: sourceRepository,
				SourceRef:        "HEAD",
				SourceCommit:     commit,
				Platform:         platform,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSubject(tt.subject)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateSubject() error = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}

func TestReleaseIntentFromFlagsRequiresSingleIntent(t *testing.T) {
	if _, err := releaseIntentFromFlags(releaseFlagValues{nightly: true, rc: true}); err == nil {
		t.Fatal("releaseIntentFromFlags() error = nil for multiple intents")
	}
	if _, err := releaseIntentFromFlags(releaseFlagValues{}); err == nil {
		t.Fatal("releaseIntentFromFlags() error = nil for missing intent")
	}
	intent, err := releaseIntentFromFlags(releaseFlagValues{channel: "rc", version: "0.2.0-rc.1", sourceRef: "HEAD"})
	if err != nil {
		t.Fatalf("releaseIntentFromFlags() error = %v", err)
	}
	if _, ok := intent.(exactIntent); !ok {
		t.Fatalf("releaseIntentFromFlags() = %T, want exactIntent", intent)
	}
}

func TestWorkspacePackageVersion(t *testing.T) {
	version, err := workspacePackageVersion(`[package]
version = "9.9.9"

[workspace.package]
version = "0.2.0"
edition = "2024"
`)
	if err != nil {
		t.Fatalf("workspacePackageVersion() error = %v", err)
	}
	if version != "0.2.0" {
		t.Fatalf("workspacePackageVersion() = %q, want 0.2.0", version)
	}
}

func TestNightlyIntentDerivesVersionFromCommitAndLocalCounter(t *testing.T) {
	repoRoot, commit := initReleaseGitRepo(t, "0.2.0")
	outputRoot := t.TempDir()
	existing := filepath.Join(outputRoot, packageName, "build-nightly-0.2.0-nightly.20260528.3-linux-amd64-default-"+shortSHA(commit))
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	subject, err := nightlyIntent{SourceRef: "HEAD"}.deriveSubject(context.Background(), releaseDerivationConfig{
		RepoRoot:   repoRoot,
		OutputRoot: outputRoot,
		Platform:   Platform{OS: "linux", Arch: "amd64"},
		Flavor:     defaultFlavor,
		Now:        time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("deriveSubject() error = %v", err)
	}
	if subject.Version != "0.2.0-nightly.20260528.4" {
		t.Fatalf("nightly version = %q", subject.Version)
	}
	if subject.SourceCommit != commit {
		t.Fatalf("source commit = %q, want %q", subject.SourceCommit, commit)
	}
}

func TestRCIntentReusesExistingTagForSameCommit(t *testing.T) {
	repoRoot, _ := initReleaseGitRepo(t, "0.2.0")
	runGitTest(t, repoRoot, "tag", "mksk-v0.2.0-rc.1")
	subject, err := rcIntent{SourceRef: "HEAD"}.deriveSubject(context.Background(), releaseDerivationConfig{
		RepoRoot:   repoRoot,
		OutputRoot: t.TempDir(),
		Platform:   Platform{OS: "linux", Arch: "amd64"},
		Flavor:     defaultFlavor,
		Now:        time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("deriveSubject() error = %v", err)
	}
	if subject.Version != "0.2.0-rc.1" {
		t.Fatalf("rc version = %q", subject.Version)
	}
}

func TestStableIntentFromRCTag(t *testing.T) {
	repoRoot, commit := initReleaseGitRepo(t, "0.2.0")
	runGitTest(t, repoRoot, "tag", "mksk-v0.2.0-rc.2")
	subject, err := stableIntent{FromRC: "mksk-v0.2.0-rc.2"}.deriveSubject(context.Background(), releaseDerivationConfig{
		RepoRoot:   repoRoot,
		OutputRoot: t.TempDir(),
		Platform:   Platform{OS: "linux", Arch: "amd64"},
		Flavor:     defaultFlavor,
		Now:        time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("deriveSubject() error = %v", err)
	}
	if subject.Version != "0.2.0" {
		t.Fatalf("stable version = %q", subject.Version)
	}
	if subject.SourceRef != "mksk-v0.2.0-rc.2" {
		t.Fatalf("source ref = %q", subject.SourceRef)
	}
	if subject.SourceCommit != commit {
		t.Fatalf("source commit = %q, want %q", subject.SourceCommit, commit)
	}
}

func TestParsePlatform(t *testing.T) {
	got, err := parsePlatform("linux/amd64")
	if err != nil {
		t.Fatalf("parsePlatform() error = %v", err)
	}
	if got.String() != "linux/amd64" {
		t.Fatalf("parsePlatform() = %s", got.String())
	}
	if _, err := parsePlatform("darwin/arm64"); err == nil {
		t.Fatal("parsePlatform(darwin/arm64) error = nil")
	}
}

func TestReleaseMetadataURL(t *testing.T) {
	got := releaseMetadataURL("0.2.0-rc.1")
	want := "https://oci.verself.sh/releases/mksk/0.2.0-rc.1"
	if got != want {
		t.Fatalf("releaseMetadataURL() = %q, want %q", got, want)
	}
}

func TestReleaseOCIReference(t *testing.T) {
	got, err := releaseOCIReference(ReleaseSubject{
		Package:          packageName,
		Version:          "0.2.0-nightly.20260528.1",
		Channel:          ChannelNightly,
		SourceRepository: sourceRepository,
		SourceRef:        "main",
		SourceCommit:     strings.Repeat("a", 40),
		Platform:         Platform{OS: "linux", Arch: "amd64"},
		Flavor:           defaultFlavor,
	})
	if err != nil {
		t.Fatalf("releaseOCIReference() error = %v", err)
	}
	want := "mksk-v0.2.0-nightly.20260528.1-linux-amd64-default"
	if got != want {
		t.Fatalf("releaseOCIReference() = %q, want %q", got, want)
	}
	public := publicOCIReference("https://oci.verself.sh", "verself/mksk", "sha256:abc")
	if public != "oci.verself.sh/verself/mksk@sha256:abc" {
		t.Fatalf("publicOCIReference() = %q", public)
	}
}

func TestRegistryCredentialRequiresPair(t *testing.T) {
	if _, err := registryCredential("artifact-publisher", ""); err == nil {
		t.Fatal("registryCredential() error = nil")
	}
	password := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(password, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := registryCredential("artifact-publisher", password)
	if err != nil {
		t.Fatalf("registryCredential() error = %v", err)
	}
	if got.Username != "artifact-publisher" || got.Password != "secret" {
		t.Fatalf("registryCredential() = %#v", got)
	}
}

func TestReleaseSignerFromFlags(t *testing.T) {
	signer, err := releaseSignerFromFlags(publishFlagValues{signingMode: signingModeDisabled})
	if err != nil {
		t.Fatalf("releaseSignerFromFlags() error = %v", err)
	}
	if _, ok := signer.(disabledSigner); !ok {
		t.Fatalf("releaseSignerFromFlags() = %T, want disabledSigner", signer)
	}
	token := filepath.Join(t.TempDir(), "openbao-token")
	if err := os.WriteFile(token, []byte("token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err = releaseSignerFromFlags(publishFlagValues{
		signingMode:         signingModeOpenBaoTransit,
		openBaoAddr:         "https://openbao.api.verself.sh",
		openBaoTokenFile:    token,
		openBaoTransitMount: "transit",
		openBaoKey:          "mksk-release-root",
	})
	if err != nil {
		t.Fatalf("releaseSignerFromFlags(openbao) error = %v", err)
	}
	if _, ok := signer.(openBaoTransitSigner); !ok {
		t.Fatalf("releaseSignerFromFlags(openbao) = %T, want openBaoTransitSigner", signer)
	}
	if err := signer.Preflight(context.Background()); err == nil {
		t.Fatal("openBaoTransitSigner.Preflight() error = nil")
	}
}

func TestPublishBundlePublishesEvidenceReferrers(t *testing.T) {
	root := t.TempDir()
	subject := ReleaseSubject{
		Package:          packageName,
		Version:          "0.2.0-nightly.20260528.1",
		Channel:          ChannelNightly,
		SourceRepository: sourceRepository,
		SourceRef:        "HEAD",
		SourceCommit:     strings.Repeat("a", 40),
		Platform:         Platform{OS: "linux", Arch: "amd64"},
		Flavor:           defaultFlavor,
	}
	bundle := BuildBundle{
		Subject: subject,
		Root:    root,
		Evidence: EvidenceSet{
			Artifact:        writeEvidenceForTest(t, root, "artifact/make-skill.tar", "tar", "application/vnd.verself.mksk.release.tar"),
			ArtifactSBOM:    writeEvidenceForTest(t, root, "sbom/make-skill.artifact.spdx.json", "{}", "application/spdx+json"),
			SourceSBOM:      writeEvidenceForTest(t, root, "sbom/make-skill.source.spdx.json", "{}", "application/spdx+json"),
			LicenseEvidence: writeEvidenceForTest(t, root, "licenses/make-skill.cargo-about.json", "{}", "application/json"),
			Provenance:      writeEvidenceForTest(t, root, "evidence/make-skill.provenance.intoto.json", "{}", "application/vnd.in-toto+json"),
			TestResults: []EvidenceFile{
				writeEvidenceForTest(t, root, "tests/cli_test.xml", "<testsuite/>", "application/xml"),
			},
		},
	}
	req := PublishRequest{
		Build: BuildRequest{
			Subject:   subject,
			BuilderID: defaultBuilderID,
		},
		PublicRegistryURL: defaultPublicURL,
		Repository:        defaultRepository,
	}
	result, err := publishBundle(context.Background(), memory.New(), req, bundle)
	if err != nil {
		t.Fatalf("publishBundle() error = %v", err)
	}
	if result.Reference != "mksk-v0.2.0-nightly.20260528.1-linux-amd64-default" {
		t.Fatalf("reference = %q", result.Reference)
	}
	if len(result.Referrers) != 5 {
		t.Fatalf("referrers = %d, want 5", len(result.Referrers))
	}
}

func TestExtractToolsRequiresBuildTools(t *testing.T) {
	toolsTar := writeReleaseToolsTar(t, []string{"bazelisk", "cargo-about"})
	_, cleanup, err := extractTools(toolsTar, []string{"bazelisk", "cargo-about", "syft"})
	if cleanup != nil {
		defer cleanup()
	}
	if err == nil {
		t.Fatal("extractTools() error = nil, want missing syft error")
	}
	if !strings.Contains(err.Error(), "syft") {
		t.Fatalf("extractTools() error = %v, want syft detail", err)
	}
}

func TestMergeCommandEnvOverridesHome(t *testing.T) {
	got := mergeCommandEnv([]string{"PATH=/bin", "HOME=/home/user", "KEEP=1"}, map[string]string{
		"HOME":           "/tmp/release-home",
		"XDG_CACHE_HOME": "/tmp/release-cache",
	})
	want := []string{"PATH=/bin", "KEEP=1", "HOME=/tmp/release-home", "XDG_CACHE_HOME=/tmp/release-cache"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeCommandEnv() = %#v, want %#v", got, want)
	}
}

func TestReleaseBazelOptionsUseShortLivedServer(t *testing.T) {
	env := map[string]string{"XDG_CACHE_HOME": "/artifacts/releases/work/tool-env/mksk/cache"}
	gotStartup := releaseBazelStartupOptions(env)
	wantStartup := []string{
		"--output_user_root=/artifacts/releases/work/tool-env/mksk/cache/bazel-output",
		"--max_idle_secs=1",
	}
	if !reflect.DeepEqual(gotStartup, wantStartup) {
		t.Fatalf("releaseBazelStartupOptions() = %#v, want %#v", gotStartup, wantStartup)
	}
	gotCommand := releaseBazelCommandOptions(env)
	wantCommand := []string{
		"--disk_cache=/artifacts/releases/work/tool-env/mksk/cache/bazel-disk",
		"--repository_cache=/artifacts/releases/work/tool-env/mksk/cache/bazel-repo",
		"--repo_contents_cache=",
	}
	if !reflect.DeepEqual(gotCommand, wantCommand) {
		t.Fatalf("releaseBazelCommandOptions() = %#v, want %#v", gotCommand, wantCommand)
	}
}

func TestReleaseRustToolsBinDir(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "external", rulesRustToolsRepo, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "rustc"), []byte("rustc"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := releaseRustToolsBinDir(root)
	if err != nil {
		t.Fatalf("releaseRustToolsBinDir() error = %v", err)
	}
	if got != bin {
		t.Fatalf("releaseRustToolsBinDir() = %q, want %q", got, bin)
	}
}

func TestCommandEnvWithPrependedPath(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	got := commandEnvWithPrependedPath(map[string]string{
		"HOME": "/tmp/home",
	}, "/opt/release/bin", "/opt/rust/bin")
	wantPath := strings.Join([]string{"/opt/release/bin", "/opt/rust/bin", "/usr/bin"}, string(os.PathListSeparator))
	if got["PATH"] != wantPath {
		t.Fatalf("PATH = %q, want %q", got["PATH"], wantPath)
	}
	if got["HOME"] != "/tmp/home" {
		t.Fatalf("HOME = %q", got["HOME"])
	}
}

func TestSafeJoinRejectsTraversal(t *testing.T) {
	if _, err := safeJoin("/tmp/root", "../escape"); err == nil {
		t.Fatal("safeJoin() error = nil for traversal")
	}
	if _, err := safeJoin("/tmp/root", "/absolute"); err == nil {
		t.Fatal("safeJoin() error = nil for absolute path")
	}
}

func writeReleaseToolsTar(t *testing.T, tools []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tools.tar")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(file)
	for _, name := range tools {
		body := []byte("#!/bin/sh\n")
		header := &tar.Header{
			Name: "bin/" + name,
			Mode: 0o755,
			Size: int64(len(body)),
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeEvidenceForTest(t *testing.T, root string, name string, body string, mediaType string) EvidenceFile {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := evidenceFile(path, mediaType)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func initReleaseGitRepo(t *testing.T, version string) (string, string) {
	t.Helper()
	repoRoot := t.TempDir()
	runGitTest(t, repoRoot, "init")
	runGitTest(t, repoRoot, "config", "user.email", "release-test@verself.sh")
	runGitTest(t, repoRoot, "config", "user.name", "release test")
	runGitTest(t, repoRoot, "config", "tag.gpgSign", "false")
	runGitTest(t, repoRoot, "config", "tag.forceSignAnnotated", "false")
	manifestPath := filepath.Join(repoRoot, cargoManifest)
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `[workspace]
members = []

[workspace.package]
version = "` + version + `"
edition = "2024"
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repoRoot, "add", cargoManifest)
	runGitTest(t, repoRoot, "commit", "-m", "initial")
	return repoRoot, strings.TrimSpace(runGitTest(t, repoRoot, "rev-parse", "HEAD"))
}

func runGitTest(t *testing.T, repoRoot string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out)
}
