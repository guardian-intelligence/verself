package jobs

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/verself/sandbox-rental-service/internal/store"
	"go.opentelemetry.io/otel/attribute"
)

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

type githubWorkflowRunResponse struct {
	ID         int64  `json:"id"`
	RunAttempt int64  `json:"run_attempt"`
	Event      string `json:"event"`
	HeadSHA    string `json:"head_sha"`
	HeadBranch string `json:"head_branch"`
	Path       string `json:"path"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	HeadRepository struct {
		FullName string `json:"full_name"`
	} `json:"head_repository"`
	PullRequests []struct {
		Number int64 `json:"number"`
		Head   struct {
			SHA  string `json:"sha"`
			Ref  string `json:"ref"`
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
		Base struct {
			SHA  string `json:"sha"`
			Ref  string `json:"ref"`
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"base"`
	} `json:"pull_requests"`
}

func (r *GitHubRunner) refreshWorkflowRunInvocationForRun(ctx context.Context, ref goldenWorkflowRunRef) (githubWorkflowInvocation, error) {
	repository := strings.TrimSpace(ref.RepositoryFullName)
	owner, repo, ok := strings.Cut(repository, "/")
	if !ok || owner == "" || repo == "" {
		return githubWorkflowInvocation{}, fmt.Errorf("github repository must be owner/name")
	}
	if ref.ProviderRunID <= 0 {
		return githubWorkflowInvocation{}, fmt.Errorf("github provider run id is required")
	}
	token, err := r.installationToken(ctx, ref.ProviderInstallationID)
	if err != nil {
		return githubWorkflowInvocation{}, err
	}
	var resp githubWorkflowRunResponse
	path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d", url.PathEscape(owner), url.PathEscape(repo), ref.ProviderRunID)
	if err := r.githubRequest(ctx, http.MethodGet, path, token, nil, &resp, http.StatusOK); err != nil {
		return githubWorkflowInvocation{}, err
	}
	invocation := githubWorkflowInvocationFromRun(ref, resp)
	if invocation.PullRequestNumber > 0 {
		// Commit count is the only PR-API-dependent field. Best-effort: a PR
		// API hiccup must not break invocation refresh or golden promotion.
		if commits, err := r.fetchPullRequestCommitCount(ctx, ref.ProviderInstallationID, owner, repo, invocation.PullRequestNumber); err == nil {
			invocation.CommitCount = commits
		}
	}
	if r.service != nil && r.service.PGX != nil {
		if err := r.service.storeQueries().UpsertGitHubWorkflowInvocation(ctx, invocation.upsertParams(time.Now().UTC())); err != nil {
			return githubWorkflowInvocation{}, err
		}
	}
	return invocation, nil
}

type githubPullRequestResponse struct {
	Commits int64 `json:"commits"`
}

// fetchPullRequestCommitCount returns the PR's commit count for the flight
// widget's commit pill. Non-PR (push) runs never call this.
func (r *GitHubRunner) fetchPullRequestCommitCount(ctx context.Context, installationID int64, owner, repo string, number int64) (int64, error) {
	ctx, span := tracer.Start(ctx, "github.pr.commits.fetch")
	defer span.End()
	span.SetAttributes(
		attribute.String("github.repository_full_name", owner+"/"+repo),
		attribute.Int64("github.pr.number", number),
	)
	token, err := r.installationToken(ctx, installationID)
	if err != nil {
		span.RecordError(err)
		return 0, err
	}
	var resp githubPullRequestResponse
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", url.PathEscape(owner), url.PathEscape(repo), number)
	if err := r.githubRequest(ctx, http.MethodGet, path, token, nil, &resp, http.StatusOK); err != nil {
		span.RecordError(err)
		return 0, err
	}
	span.SetAttributes(attribute.Int64("github.pr.commits", resp.Commits))
	return resp.Commits, nil
}

func githubWorkflowInvocationFromRun(ref goldenWorkflowRunRef, run githubWorkflowRunResponse) githubWorkflowInvocation {
	invocation := githubWorkflowInvocation{
		InstallationID:         ref.ProviderInstallationID,
		RepositoryID:           ref.ProviderRepositoryID,
		RunID:                  firstNonZero(run.ID, ref.ProviderRunID),
		RunAttempt:             firstNonZero(run.RunAttempt, ref.ProviderRunAttempt),
		RepositoryFullName:     firstNonEmpty(run.Repository.FullName, ref.RepositoryFullName),
		EventName:              strings.TrimSpace(run.Event),
		HeadSHA:                strings.TrimSpace(firstNonEmpty(run.HeadSHA, ref.HeadSHA)),
		HeadBranch:             strings.TrimSpace(run.HeadBranch),
		HeadRepositoryFullName: strings.TrimSpace(run.HeadRepository.FullName),
		WorkflowPath:           strings.TrimSpace(run.Path),
	}
	if invocation.HeadRepositoryFullName == "" {
		invocation.HeadRepositoryFullName = invocation.RepositoryFullName
	}
	if len(run.PullRequests) > 0 {
		pr := run.PullRequests[0]
		invocation.PullRequestNumber = pr.Number
		invocation.BaseSHA = strings.TrimSpace(pr.Base.SHA)
		invocation.BaseBranch = strings.TrimSpace(pr.Base.Ref)
		if pr.Head.SHA != "" {
			invocation.HeadSHA = strings.TrimSpace(pr.Head.SHA)
		}
		if pr.Head.Ref != "" {
			invocation.HeadBranch = strings.TrimSpace(pr.Head.Ref)
		}
		if pr.Head.Repo.FullName != "" {
			invocation.HeadRepositoryFullName = strings.TrimSpace(pr.Head.Repo.FullName)
		}
	}
	return invocation
}

func (i githubWorkflowInvocation) upsertParams(now time.Time) store.UpsertGitHubWorkflowInvocationParams {
	return store.UpsertGitHubWorkflowInvocationParams{
		ProviderInstallationID: i.InstallationID,
		ProviderRepositoryID:   i.RepositoryID,
		ProviderRunID:          i.RunID,
		ProviderRunAttempt:     i.RunAttempt,
		RepositoryFullName:     i.RepositoryFullName,
		EventName:              i.EventName,
		HeadSha:                i.HeadSHA,
		HeadBranch:             i.HeadBranch,
		HeadRepositoryFullName: i.HeadRepositoryFullName,
		BaseSha:                i.BaseSHA,
		BaseBranch:             i.BaseBranch,
		WorkflowPath:           i.WorkflowPath,
		PullRequestNumber:      i.PullRequestNumber,
		CommitCount:            i.CommitCount,
		UpdatedAt:              pgTime(now),
	}
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
