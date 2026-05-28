package releaseworkflow

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

const (
	ResolveSourceActivityName                = "verself.distribution.release.resolve-source.v1"
	BuildNightlyReleaseActivityName          = "verself.distribution.release.build-nightly.v1"
	BuildReleaseCandidateActivityName        = "verself.distribution.release.build-rc.v1"
	BuildStableReleaseActivityName           = "verself.distribution.release.build-stable.v1"
	defaultBuildActivityStartToCloseTimeout  = 8 * time.Hour
	defaultBuildActivityScheduleToCloseLimit = 10 * time.Hour
	defaultResolveActivityTimeout            = 5 * time.Minute
)

type Registry interface {
	RegisterWorkflowWithOptions(interface{}, workflow.RegisterOptions)
	RegisterActivityWithOptions(interface{}, activity.RegisterOptions)
}

type Activities struct {
	Runner ActivityRunner
}

type ActivityRunner interface {
	ResolveSource(ctx context.Context, input ScheduledNightlyReleaseInput) (PinnedSource, error)
	BuildNightly(ctx context.Context, input NightlyReleaseInput) (ReleaseWorkflowResult, error)
	BuildReleaseCandidate(ctx context.Context, input ReleaseCandidateInput) (ReleaseWorkflowResult, error)
	BuildStable(ctx context.Context, input StableReleaseInput) (ReleaseWorkflowResult, error)
}

type ReleaseWorkflowResult struct {
	Package      string       `json:"package"`
	Channel      string       `json:"channel"`
	Version      string       `json:"version"`
	Source       PinnedSource `json:"source"`
	ArtifactRoot string       `json:"artifact_root"`
}

func Register(registry Registry, activities Activities) {
	registry.RegisterWorkflowWithOptions(NightlyReleaseWorkflow, workflow.RegisterOptions{Name: NightlyReleaseWorkflowName})
	registry.RegisterWorkflowWithOptions(ScheduledNightlyReleaseWorkflow, workflow.RegisterOptions{Name: ScheduledNightlyReleaseWorkflowName})
	registry.RegisterWorkflowWithOptions(ReleaseCandidateWorkflow, workflow.RegisterOptions{Name: ReleaseCandidateWorkflowName})
	registry.RegisterWorkflowWithOptions(StableReleaseWorkflow, workflow.RegisterOptions{Name: StableReleaseWorkflowName})
	registry.RegisterActivityWithOptions(activities.ResolveSource, activity.RegisterOptions{Name: ResolveSourceActivityName})
	registry.RegisterActivityWithOptions(activities.BuildNightly, activity.RegisterOptions{Name: BuildNightlyReleaseActivityName})
	registry.RegisterActivityWithOptions(activities.BuildReleaseCandidate, activity.RegisterOptions{Name: BuildReleaseCandidateActivityName})
	registry.RegisterActivityWithOptions(activities.BuildStable, activity.RegisterOptions{Name: BuildStableReleaseActivityName})
}

func RegisterWorker(workerInstance worker.Worker, runner ActivityRunner) {
	Register(workerInstance, Activities{Runner: runner})
}

func NightlyReleaseWorkflow(ctx workflow.Context, input NightlyReleaseInput) (ReleaseWorkflowResult, error) {
	if err := validateNightlyInput(input); err != nil {
		return ReleaseWorkflowResult{}, temporal.NewNonRetryableApplicationError(err.Error(), "InvalidReleaseInput", err)
	}
	var result ReleaseWorkflowResult
	if err := workflow.ExecuteActivity(buildActivityContext(ctx), BuildNightlyReleaseActivityName, input).Get(ctx, &result); err != nil {
		return ReleaseWorkflowResult{}, err
	}
	return result, nil
}

func ScheduledNightlyReleaseWorkflow(ctx workflow.Context, input ScheduledNightlyReleaseInput) (ReleaseWorkflowResult, error) {
	if err := validateScheduledNightlyInput(input); err != nil {
		return ReleaseWorkflowResult{}, temporal.NewNonRetryableApplicationError(err.Error(), "InvalidReleaseInput", err)
	}
	var source PinnedSource
	if err := workflow.ExecuteActivity(resolveActivityContext(ctx), ResolveSourceActivityName, input).Get(ctx, &source); err != nil {
		return ReleaseWorkflowResult{}, err
	}
	return NightlyReleaseWorkflow(ctx, NightlyReleaseInput{
		Package: input.Package,
		Source:  source,
	})
}

func ReleaseCandidateWorkflow(ctx workflow.Context, input ReleaseCandidateInput) (ReleaseWorkflowResult, error) {
	if err := validateReleaseCandidateInput(input); err != nil {
		return ReleaseWorkflowResult{}, temporal.NewNonRetryableApplicationError(err.Error(), "InvalidReleaseInput", err)
	}
	var result ReleaseWorkflowResult
	if err := workflow.ExecuteActivity(buildActivityContext(ctx), BuildReleaseCandidateActivityName, input).Get(ctx, &result); err != nil {
		return ReleaseWorkflowResult{}, err
	}
	return result, nil
}

func StableReleaseWorkflow(ctx workflow.Context, input StableReleaseInput) (ReleaseWorkflowResult, error) {
	if err := validateStableInput(input); err != nil {
		return ReleaseWorkflowResult{}, temporal.NewNonRetryableApplicationError(err.Error(), "InvalidReleaseInput", err)
	}
	var result ReleaseWorkflowResult
	if err := workflow.ExecuteActivity(buildActivityContext(ctx), BuildStableReleaseActivityName, input).Get(ctx, &result); err != nil {
		return ReleaseWorkflowResult{}, err
	}
	return result, nil
}

func (a Activities) ResolveSource(ctx context.Context, input ScheduledNightlyReleaseInput) (PinnedSource, error) {
	if a.Runner == nil {
		return PinnedSource{}, fmt.Errorf("%w: release activity runner is required", ErrInvalidInput)
	}
	return a.Runner.ResolveSource(ctx, input)
}

func (a Activities) BuildNightly(ctx context.Context, input NightlyReleaseInput) (ReleaseWorkflowResult, error) {
	if a.Runner == nil {
		return ReleaseWorkflowResult{}, fmt.Errorf("%w: release activity runner is required", ErrInvalidInput)
	}
	return a.Runner.BuildNightly(ctx, input)
}

func (a Activities) BuildReleaseCandidate(ctx context.Context, input ReleaseCandidateInput) (ReleaseWorkflowResult, error) {
	if a.Runner == nil {
		return ReleaseWorkflowResult{}, fmt.Errorf("%w: release activity runner is required", ErrInvalidInput)
	}
	return a.Runner.BuildReleaseCandidate(ctx, input)
}

func (a Activities) BuildStable(ctx context.Context, input StableReleaseInput) (ReleaseWorkflowResult, error) {
	if a.Runner == nil {
		return ReleaseWorkflowResult{}, fmt.Errorf("%w: release activity runner is required", ErrInvalidInput)
	}
	return a.Runner.BuildStable(ctx, input)
}

func resolveActivityContext(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: defaultResolveActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    5,
		},
	})
}

func buildActivityContext(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout:    defaultBuildActivityStartToCloseTimeout,
		ScheduleToCloseTimeout: defaultBuildActivityScheduleToCloseLimit,
		HeartbeatTimeout:       time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
	})
}
