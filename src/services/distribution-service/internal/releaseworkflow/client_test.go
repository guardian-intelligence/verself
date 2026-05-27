package releaseworkflow

import (
	"errors"
	"testing"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	tclient "go.temporal.io/sdk/client"
)

const testCommit = "0123456789abcdef0123456789abcdef01234567"

func TestReleaseWorkflowIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "nightly",
			got:  releaseWorkflowID(PackageMksk, ChannelNightly, "", testCommit),
			want: "release/mksk/nightly/" + testCommit,
		},
		{
			name: "rc",
			got:  releaseWorkflowID(PackageMksk, ChannelRC, "0.2.0-rc.1", testCommit),
			want: "release/mksk/rc/0-2-0-rc-1/" + testCommit,
		},
		{
			name: "stable",
			got:  releaseWorkflowID(PackageMksk, ChannelStable, "0.2.0", testCommit),
			want: "release/mksk/stable/0-2-0/" + testCommit,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.got != tt.want {
				t.Fatalf("workflow id = %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestValidateInputs(t *testing.T) {
	t.Parallel()

	validSource := PinnedSource{Ref: "main", Commit: testCommit}
	cases := []struct {
		name string
		err  error
	}{
		{
			name: "nightly",
			err: validateNightlyInput(NightlyReleaseInput{
				Package: PackageMksk,
				Source:  validSource,
			}),
		},
		{
			name: "rc",
			err: validateReleaseCandidateInput(ReleaseCandidateInput{
				Package: PackageMksk,
				Version: "0.2.0-rc.1",
				Source:  validSource,
			}),
		},
		{
			name: "stable",
			err: validateStableInput(StableReleaseInput{
				Package: PackageMksk,
				Version: "0.2.0",
				Source:  validSource,
			}),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.err != nil {
				t.Fatalf("validate returned error: %v", tc.err)
			}
		})
	}
}

func TestValidateRejectsWrongChannelVersions(t *testing.T) {
	t.Parallel()

	validSource := PinnedSource{Ref: "main", Commit: testCommit}
	cases := []struct {
		name string
		err  error
	}{
		{
			name: "rc requires rc prerelease",
			err: validateReleaseCandidateInput(ReleaseCandidateInput{
				Package: PackageMksk,
				Version: "0.2.0",
				Source:  validSource,
			}),
		},
		{
			name: "stable rejects prerelease",
			err: validateStableInput(StableReleaseInput{
				Package: PackageMksk,
				Version: "0.2.0-rc.1",
				Source:  validSource,
			}),
		},
		{
			name: "commit must be full sha",
			err: validateNightlyInput(NightlyReleaseInput{
				Package: PackageMksk,
				Source:  PinnedSource{Ref: "main", Commit: "0123456789ab"},
			}),
		},
		{
			name: "source ref cannot contain whitespace",
			err: validateNightlyInput(NightlyReleaseInput{
				Package: PackageMksk,
				Source:  PinnedSource{Ref: "main branch", Commit: testCommit},
			}),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !errors.Is(tc.err, ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", tc.err)
			}
		})
	}
}

func TestNightlyScheduleOptions(t *testing.T) {
	t.Parallel()

	client := &Client{namespace: DefaultNamespace, taskQueue: DefaultTaskQueue}
	opts := client.nightlyScheduleOptions(NightlyScheduleRequest{
		Package:   PackageMksk,
		SourceRef: "main",
		HourUTC:   5,
		MinuteUTC: 30,
		Jitter:    10 * time.Minute,
		Paused:    true,
	})

	if opts.ID != "release/mksk/nightly" {
		t.Fatalf("schedule id = %q", opts.ID)
	}
	if opts.Overlap != enumspb.SCHEDULE_OVERLAP_POLICY_SKIP {
		t.Fatalf("overlap = %v", opts.Overlap)
	}
	if !opts.PauseOnFailure {
		t.Fatal("PauseOnFailure = false")
	}
	if opts.CatchupWindow != defaultCatchupWindow {
		t.Fatalf("catchup window = %s", opts.CatchupWindow)
	}
	if !opts.Paused {
		t.Fatal("paused = false")
	}
	if opts.Spec.TimeZoneName != "UTC" {
		t.Fatalf("timezone = %q", opts.Spec.TimeZoneName)
	}
	if len(opts.Spec.Calendars) != 1 {
		t.Fatalf("calendar count = %d", len(opts.Spec.Calendars))
	}
	calendar := opts.Spec.Calendars[0]
	if got := singleRangeStart(t, calendar.Hour, "hour"); got != 5 {
		t.Fatalf("hour = %d", got)
	}
	if got := singleRangeStart(t, calendar.Minute, "minute"); got != 30 {
		t.Fatalf("minute = %d", got)
	}

	action, ok := opts.Action.(*tclient.ScheduleWorkflowAction)
	if !ok {
		t.Fatalf("action type = %T", opts.Action)
	}
	if action.Workflow != ScheduledNightlyReleaseWorkflowName {
		t.Fatalf("workflow = %v", action.Workflow)
	}
	if action.TaskQueue != DefaultTaskQueue {
		t.Fatalf("task queue = %q", action.TaskQueue)
	}
	if len(action.Args) != 1 {
		t.Fatalf("arg count = %d", len(action.Args))
	}
	input, ok := action.Args[0].(ScheduledNightlyReleaseInput)
	if !ok {
		t.Fatalf("arg type = %T", action.Args[0])
	}
	if input.Package != PackageMksk || input.Source.Ref != "main" {
		t.Fatalf("input = %+v", input)
	}
}

func singleRangeStart(t *testing.T, ranges []tclient.ScheduleRange, field string) int {
	t.Helper()
	if len(ranges) != 1 {
		t.Fatalf("%s range count = %d", field, len(ranges))
	}
	return ranges[0].Start
}
