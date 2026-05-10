package jobs

import "testing"

func TestDogfoodMainPromotionGate(t *testing.T) {
	ref := goldenWorkflowRunRef{HeadSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	tests := []struct {
		name       string
		invocation githubWorkflowInvocation
		wantOK     bool
	}{
		{
			name: "push main",
			invocation: githubWorkflowInvocation{
				EventName:  "push",
				HeadBranch: "main",
				HeadSHA:    ref.HeadSHA,
			},
			wantOK: true,
		},
		{
			name: "pull request main head name cannot promote",
			invocation: githubWorkflowInvocation{
				EventName:         "pull_request",
				HeadBranch:        "main",
				HeadSHA:           ref.HeadSHA,
				PullRequestNumber: 42,
			},
			wantOK: false,
		},
		{
			name: "feature branch push cannot promote dogfood main",
			invocation: githubWorkflowInvocation{
				EventName:  "push",
				HeadBranch: "feature",
				HeadSHA:    ref.HeadSHA,
			},
			wantOK: false,
		},
		{
			name: "mismatched sha cannot promote",
			invocation: githubWorkflowInvocation{
				EventName:  "push",
				HeadBranch: "main",
				HeadSHA:    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			},
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := tt.invocation.dogfoodMainPromotion(ref)
			if got != tt.wantOK {
				t.Fatalf("dogfoodMainPromotion = %v, want %v", got, tt.wantOK)
			}
		})
	}
}

func TestGoldenWorkspaceShapeUsesExpandedJobNameAsMatrixKey(t *testing.T) {
	identity := RunnerExecutionIdentity{JobName: "test-node (node: 22, os: ubuntu-24.04)"}
	if got, want := githubMatrixKey(identity), identity.JobName; got != want {
		t.Fatalf("githubMatrixKey = %q, want %q", got, want)
	}
}

func TestGoldenWorkspacePromotionCandidateIsMainOnly(t *testing.T) {
	if !goldenWorkspacePromotionCandidate(RunnerExecutionIdentity{HeadBranch: "main"}) {
		t.Fatal("main branch should be a promotion candidate")
	}
	if goldenWorkspacePromotionCandidate(RunnerExecutionIdentity{HeadBranch: "feature"}) {
		t.Fatal("feature branch should not be a promotion candidate")
	}
}

func TestGitHubJobAllowsGoldenSealRequiresSuccessConclusion(t *testing.T) {
	state := githubWorkflowRunJobsState{
		JobStatuses: map[int64]string{
			1: "completed",
			2: "completed",
			3: "in_progress",
		},
		JobConclusions: map[int64]string{
			1: "success",
			2: "cancelled",
		},
	}
	if allowed, reason, settled := githubJobAllowsGoldenSeal(state, 1); !allowed || reason != "" || !settled {
		t.Fatalf("success job = (%v, %q, %v), want allowed", allowed, reason, settled)
	}
	if allowed, reason, settled := githubJobAllowsGoldenSeal(state, 2); allowed || reason != "github job concluded cancelled" || !settled {
		t.Fatalf("cancelled job = (%v, %q, %v), want settled skip", allowed, reason, settled)
	}
	if allowed, reason, settled := githubJobAllowsGoldenSeal(state, 3); allowed || reason != "github job is not complete" || settled {
		t.Fatalf("running job = (%v, %q, %v), want unsettled skip", allowed, reason, settled)
	}
}
