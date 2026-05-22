package jobs

import "strings"

type providerWorkflowRun struct {
	Provider               string
	InstallationID         int64
	RepositoryID           int64
	RunID                  int64
	RunAttempt             int64
	RepositoryFullName     string
	EventName              string
	HeadSHA                string
	HeadBranch             string
	HeadRepositoryFullName string
	BaseSHA                string
	BaseBranch             string
	WorkflowPath           string
	PullRequestNumber      int64
	CommitCount            int64
}

func (r providerWorkflowRun) githubDogfoodMainPromotion(ref goldenWorkflowRunRef) (bool, string) {
	if r.Provider != RunnerProviderGitHub {
		return false, "provider is not github"
	}
	if r.EventName != "push" {
		return false, "github workflow run is not a push event"
	}
	if r.HeadBranch != durableDogfoodBranch {
		return false, "github workflow run is not on dogfood main"
	}
	if r.PullRequestNumber != 0 {
		return false, "github workflow run is associated with a pull request"
	}
	if ref.HeadSHA != "" && r.HeadSHA != "" && !strings.EqualFold(ref.HeadSHA, r.HeadSHA) {
		return false, "github workflow run head sha does not match promotion request"
	}
	return true, ""
}

func firstNonZero(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
