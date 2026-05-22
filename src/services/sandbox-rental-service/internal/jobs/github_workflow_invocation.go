package jobs

import "strings"

type githubWorkflowInvocation struct {
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

func (i githubWorkflowInvocation) dogfoodMainPromotion(ref goldenWorkflowRunRef) (bool, string) {
	if i.EventName != "push" {
		return false, "github workflow run is not a push event"
	}
	if i.HeadBranch != durableDogfoodBranch {
		return false, "github workflow run is not on dogfood main"
	}
	if i.PullRequestNumber != 0 {
		return false, "github workflow run is associated with a pull request"
	}
	if ref.HeadSHA != "" && i.HeadSHA != "" && !strings.EqualFold(ref.HeadSHA, i.HeadSHA) {
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
