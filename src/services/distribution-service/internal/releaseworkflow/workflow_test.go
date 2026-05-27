package releaseworkflow

import (
	"context"
	"testing"

	"go.temporal.io/sdk/testsuite"
)

type fakeActivityRunner struct {
	source PinnedSource
}

func (f fakeActivityRunner) ResolveSource(context.Context, ScheduledNightlyReleaseInput) (PinnedSource, error) {
	return f.source, nil
}

func (f fakeActivityRunner) BuildNightly(_ context.Context, input NightlyReleaseInput) (ReleaseWorkflowResult, error) {
	return ReleaseWorkflowResult{
		Package:      input.Package,
		Channel:      ChannelNightly,
		Version:      "0.2.0-nightly.20260527.1",
		Source:       input.Source,
		ArtifactRoot: "/artifacts/releases/mksk/nightly-0.2.0-nightly.20260527.1-" + input.Source.Commit[:12],
	}, nil
}

func (f fakeActivityRunner) BuildReleaseCandidate(_ context.Context, input ReleaseCandidateInput) (ReleaseWorkflowResult, error) {
	return ReleaseWorkflowResult{
		Package:      input.Package,
		Channel:      ChannelRC,
		Version:      input.Version,
		Source:       input.Source,
		ArtifactRoot: "/artifacts/releases/mksk/rc-" + input.Version + "-" + input.Source.Commit[:12],
	}, nil
}

func (f fakeActivityRunner) BuildStable(_ context.Context, input StableReleaseInput) (ReleaseWorkflowResult, error) {
	return ReleaseWorkflowResult{
		Package:      input.Package,
		Channel:      ChannelStable,
		Version:      input.Version,
		Source:       input.Source,
		ArtifactRoot: "/artifacts/releases/mksk/stable-" + input.Version + "-" + input.Source.Commit[:12],
	}, nil
}

func TestScheduledNightlyReleaseWorkflowResolvesSourceAndBuilds(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	source := PinnedSource{Ref: "main", Commit: testCommit}
	Register(env, Activities{Runner: fakeActivityRunner{source: source}})

	env.ExecuteWorkflow(ScheduledNightlyReleaseWorkflow, ScheduledNightlyReleaseInput{
		Package: PackageMksk,
		Source:  FloatingSource{Ref: "main"},
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result ReleaseWorkflowResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if result.Package != PackageMksk || result.Channel != ChannelNightly || result.Source != source {
		t.Fatalf("unexpected workflow result: %+v", result)
	}
}

func TestReleaseCandidateWorkflowRejectsStableVersion(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	Register(env, Activities{Runner: fakeActivityRunner{source: PinnedSource{Ref: "HEAD", Commit: testCommit}}})

	env.ExecuteWorkflow(ReleaseCandidateWorkflow, ReleaseCandidateInput{
		Package: PackageMksk,
		Version: "0.2.0",
		Source:  PinnedSource{Ref: "HEAD", Commit: testCommit},
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err == nil {
		t.Fatal("expected workflow error")
	}
}
