package releaseworkflow

import (
	"archive/tar"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type fakeCommandExecutor struct {
	t            *testing.T
	artifactRoot string
	execRoot     string
	commands     []Command
}

func (f *fakeCommandExecutor) Run(_ context.Context, command Command) (CommandResult, error) {
	f.commands = append(f.commands, command)
	switch {
	case command.Name == "git":
		if len(command.Args) >= 1 && command.Args[0] == "ls-remote" {
			return CommandResult{Stdout: testCommit + "\trefs/heads/main\n"}, nil
		}
		return CommandResult{}, nil
	case strings.HasSuffix(command.Name, "/bin/bazelisk"):
		return f.runBazelisk(command)
	case strings.HasSuffix(command.Name, "/mksk-release"):
		f.t.Helper()
		root := filepath.Join(f.artifactRoot, PackageMksk, "nightly-0.2.0-nightly.20260527.1-"+testCommit[:12])
		writeStandardReleaseEvidence(f.t, root)
		return CommandResult{Stdout: "release artifacts: " + root + "\n"}, nil
	default:
		f.t.Fatalf("unexpected command: %+v", command)
		return CommandResult{}, nil
	}
}

func (f *fakeCommandExecutor) runBazelisk(command Command) (CommandResult, error) {
	f.t.Helper()
	if slices.Contains(command.Args, "cquery") {
		rel := "bazel-out/k8-fastbuild/bin/src/make-skill/release/cmd/mksk-release/mksk-release_/mksk-release"
		if slices.Contains(command.Args, "//src/make-skill/release:release_tools_artifact") {
			rel = "bazel-out/k8-fastbuild/bin/src/make-skill/release/mksk-release-tools.tar"
		}
		path := filepath.Join(f.execRoot, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			f.t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o755); err != nil {
			f.t.Fatal(err)
		}
		return CommandResult{Stdout: rel + "\n"}, nil
	}
	if slices.Contains(command.Args, "info") {
		return CommandResult{Stdout: f.execRoot + "\n"}, nil
	}
	return CommandResult{}, nil
}

func TestCommandRunnerResolveSource(t *testing.T) {
	t.Parallel()

	executor := &fakeCommandExecutor{t: t, artifactRoot: t.TempDir()}
	runner, err := NewCommandRunnerForTest(CommandRunnerConfig{
		ArtifactRoot:    executor.artifactRoot,
		WorkRoot:        t.TempDir(),
		ReleaseToolsTar: "tools.tar",
		GitBinary:       "git",
		BuilderID:       DefaultBuilderID,
	}, executor)
	if err != nil {
		t.Fatal(err)
	}

	source, err := runner.ResolveSource(context.Background(), ScheduledNightlyReleaseInput{
		Package: PackageMksk,
		Source:  FloatingSource{Ref: "main"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if source != (PinnedSource{Ref: "main", Commit: testCommit}) {
		t.Fatalf("source = %+v", source)
	}
}

func TestCommandRunnerBuildNightlyInvokesDistributionRelease(t *testing.T) {
	t.Parallel()

	executor := &fakeCommandExecutor{t: t, artifactRoot: t.TempDir(), execRoot: t.TempDir()}
	runner, err := NewCommandRunnerForTest(CommandRunnerConfig{
		ArtifactRoot:    executor.artifactRoot,
		WorkRoot:        t.TempDir(),
		ReleaseToolsTar: writeReleaseToolsTar(t, []string{"bazelisk"}),
		GitBinary:       "git",
		BuilderID:       DefaultBuilderID,
	}, executor)
	if err != nil {
		t.Fatal(err)
	}

	result, err := runner.BuildNightly(context.Background(), NightlyReleaseInput{
		Package: PackageMksk,
		Source:  PinnedSource{Ref: "main", Commit: testCommit},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != "0.2.0-nightly.20260527.1" || result.Channel != ChannelNightly {
		t.Fatalf("result = %+v", result)
	}
	var releaseCommand *Command
	for i := range executor.commands {
		if strings.HasSuffix(executor.commands[i].Name, "/mksk-release") {
			releaseCommand = &executor.commands[i]
		}
	}
	if releaseCommand == nil {
		t.Fatal("mksk-release was not invoked")
	}
	for _, want := range []string{"build", "--channel", ChannelNightly, "--source-ref", "main", "--source-commit", testCommit, "--builder-id", DefaultBuilderID} {
		if !slices.Contains(releaseCommand.Args, want) {
			t.Fatalf("release args missing %q: %v", want, releaseCommand.Args)
		}
	}
}

func TestParseReleaseArtifactRoot(t *testing.T) {
	t.Parallel()

	got, err := parseReleaseArtifactRoot("log\nrelease artifacts: /tmp/out\n")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/out" {
		t.Fatalf("artifact root = %q", got)
	}
}

func TestSafeJoinRejectsTraversal(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"..", "../x", "/tmp/x"} {
		if _, err := safeJoin("/tmp/root", name); err == nil {
			t.Fatalf("safeJoin accepted %q", name)
		}
	}
}

func TestPathWithin(t *testing.T) {
	t.Parallel()

	if !pathWithin("/artifacts/releases", "/artifacts/releases/mksk/run") {
		t.Fatal("pathWithin rejected release child")
	}
	for _, path := range []string{"/artifacts/releases", "/artifacts", "/tmp/release"} {
		if pathWithin("/artifacts/releases", path) {
			t.Fatalf("pathWithin accepted %q", path)
		}
	}
}

func writeReleaseToolsTar(t *testing.T, tools []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tools.tar")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(file)
	if err := writer.WriteHeader(&tar.Header{Name: "bin", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools {
		if err := writer.WriteHeader(&tar.Header{Name: "bin/" + tool, Typeflag: tar.TypeReg, Mode: 0o755, Size: 0}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeStandardReleaseEvidence(t *testing.T, root string) {
	t.Helper()
	files := map[string]string{
		"artifact/make-skill.tar":              "artifact bytes",
		"sbom/make-skill.artifact.spdx.json":   `{"spdxVersion":"SPDX-2.3"}`,
		"sbom/make-skill.source.spdx.json":     `{"spdxVersion":"SPDX-2.3"}`,
		"licenses/make-skill.cargo-about.json": `{"licenses":[]}`,
		"tests/core_test.xml":                  "<testsuite/>",
		"tests/exec_test.xml":                  "<testsuite/>",
		"tests/cli_test.xml":                   "<testsuite/>",
	}
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	artifactDigest, err := fileSHA256(filepath.Join(root, "artifact", "make-skill.tar"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "artifact", "make-skill.tar.sha256"), []byte(artifactDigest+"  make-skill.tar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	readme := strings.Join([]string{
		"package=mksk",
		"version=0.2.0-nightly.20260527.1",
		"channel=nightly",
		"source_ref=main",
		"source_commit=" + testCommit,
		"builder_id=" + DefaultBuilderID,
		"artifact=artifact/make-skill.tar",
		"artifact_sha256=" + artifactDigest,
		"sbom_artifact=sbom/make-skill.artifact.spdx.json",
		"sbom_source=sbom/make-skill.source.spdx.json",
		"licenses=licenses/make-skill.cargo-about.json",
		"provenance=evidence/make-skill.provenance.intoto.json",
		"tests=tests/*.xml",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(root, "README.txt"), []byte(readme), 0o644); err != nil {
		t.Fatal(err)
	}
	provenance := `{
  "_type": "https://in-toto.io/Statement/v1",
  "predicateType": "https://slsa.dev/provenance/v1",
  "subject": [{"name": "make-skill.tar", "digest": {"sha256": "` + artifactDigest + `"}}],
  "predicate": {
    "buildDefinition": {
      "externalParameters": {"channel": "nightly", "package": "mksk", "platform": "linux/amd64", "version": "0.2.0-nightly.20260527.1"},
      "resolvedDependencies": [{"uri": "https://github.com/guardian-intelligence/verself.git", "digest": {"gitCommit": "` + testCommit + `"}}]
    },
    "runDetails": {"builder": {"id": "` + DefaultBuilderID + `"}}
  }
}`
	if err := os.MkdirAll(filepath.Join(root, "evidence"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "evidence", "make-skill.provenance.intoto.json"), []byte(provenance), 0o644); err != nil {
		t.Fatal(err)
	}
	writeChecksumManifest(t, root)
}

func writeChecksumManifest(t *testing.T, root string) {
	t.Helper()
	var lines []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
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
		sum, err := fileSHA256(path)
		if err != nil {
			return err
		}
		lines = append(lines, sum+"  "+filepath.ToSlash(rel))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	slices.Sort(lines)
	if err := os.WriteFile(filepath.Join(root, "checksums.sha256"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLocalSourceRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ref  string
		want string
	}{
		{ref: "main", want: "refs/heads/main"},
		{ref: "refs/heads/release", want: "refs/heads/release"},
		{ref: "refs/tags/v0.2.0", want: "refs/tags/v0.2.0"},
		{ref: "HEAD", want: ""},
		{ref: testCommit, want: ""},
		{ref: "refs/pull/1/head", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			t.Parallel()
			if got := localSourceRef(tt.ref); got != tt.want {
				t.Fatalf("local source ref = %q, want %q", got, tt.want)
			}
		})
	}
}
