package releaseworkflow

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

type fakeCommandExecutor struct {
	t            *testing.T
	artifactRoot string
	commands     []Command
}

func (f *fakeCommandExecutor) Run(_ context.Context, command Command) (CommandResult, error) {
	f.commands = append(f.commands, command)
	switch command.Name {
	case "git":
		if len(command.Args) >= 1 && command.Args[0] == "ls-remote" {
			return CommandResult{Stdout: testCommit + "\trefs/heads/main\n"}, nil
		}
		return CommandResult{}, nil
	case "distribution-release":
		f.t.Helper()
		root := filepath.Join(f.artifactRoot, PackageMksk, "nightly-0.2.0-nightly.20260527.1-"+testCommit[:12])
		if err := os.MkdirAll(root, 0o755); err != nil {
			f.t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "README.txt"), []byte("version=0.2.0-nightly.20260527.1\n"), 0o644); err != nil {
			f.t.Fatal(err)
		}
		return CommandResult{Stdout: "release artifacts: " + root + "\n"}, nil
	default:
		f.t.Fatalf("unexpected command: %+v", command)
		return CommandResult{}, nil
	}
}

func TestCommandRunnerResolveSource(t *testing.T) {
	t.Parallel()

	executor := &fakeCommandExecutor{t: t, artifactRoot: t.TempDir()}
	runner, err := NewCommandRunnerForTest(CommandRunnerConfig{
		SourceRepository:          "https://example.invalid/repo.git",
		ArtifactRoot:              executor.artifactRoot,
		WorkRoot:                  t.TempDir(),
		ReleaseToolsTar:           "tools.tar",
		DistributionReleaseBinary: "distribution-release",
		GitBinary:                 "git",
		BuilderID:                 DefaultBuilderID,
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

	executor := &fakeCommandExecutor{t: t, artifactRoot: t.TempDir()}
	runner, err := NewCommandRunnerForTest(CommandRunnerConfig{
		SourceRepository:          "https://example.invalid/repo.git",
		ArtifactRoot:              executor.artifactRoot,
		WorkRoot:                  t.TempDir(),
		ReleaseToolsTar:           "tools.tar",
		DistributionReleaseBinary: "distribution-release",
		GitBinary:                 "git",
		BuilderID:                 DefaultBuilderID,
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
		if executor.commands[i].Name == "distribution-release" {
			releaseCommand = &executor.commands[i]
		}
	}
	if releaseCommand == nil {
		t.Fatal("distribution-release was not invoked")
	}
	for _, want := range []string{"--channel", ChannelNightly, "--source-ref", "main", "--builder-id", DefaultBuilderID} {
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
