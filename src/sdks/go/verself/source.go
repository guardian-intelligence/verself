package verself

import (
	"context"
	"fmt"
	"strings"
	"time"

	sourcecore "github.com/verself/verself-go/internal/generated/source"
)

const (
	SourceGitScopeRepoRead  = "repo:read"
	SourceGitScopeRepoWrite = "repo:write"
)

type SourceRepository struct {
	RepoID        string     `json:"repo_id"`
	OrgID         string     `json:"org_id"`
	OrgSlug       string     `json:"org_slug"`
	ProjectID     string     `json:"project_id"`
	ProjectSlug   string     `json:"project_slug"`
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	DefaultBranch string     `json:"default_branch"`
	Visibility    string     `json:"visibility"`
	State         string     `json:"state"`
	Version       int32      `json:"version"`
	Backend       string     `json:"backend"`
	GitHTTPURL    string     `json:"git_http_url"`
	LastPushedAt  *time.Time `json:"last_pushed_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type SourceRepositoryList struct {
	Repositories []SourceRepository `json:"repositories"`
}

type SourceRef struct {
	Name   string `json:"name"`
	Commit string `json:"commit"`
}

type SourceRefs struct {
	Refs []SourceRef `json:"refs"`
}

type SourceTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Sha  string `json:"sha"`
	Size int64  `json:"size"`
}

type SourceTree struct {
	Entries []SourceTreeEntry `json:"entries"`
}

type SourceBlob struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Sha         string `json:"sha"`
	Size        int64  `json:"size"`
	Encoding    string `json:"encoding"`
	Content     string `json:"content"`
	DownloadURL string `json:"download_url,omitempty"`
}

type SourceCheckoutGrant struct {
	GrantID   string    `json:"grant_id"`
	RepoID    string    `json:"repo_id"`
	Ref       string    `json:"ref"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type SourceGitCredential struct {
	CredentialID string    `json:"credential_id"`
	OrgID        string    `json:"org_id"`
	Username     string    `json:"username"`
	Token        string    `json:"token"`
	TokenPrefix  string    `json:"token_prefix"`
	Scopes       []string  `json:"scopes"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
}

type SourceWorkflowRun struct {
	WorkflowRunID     string            `json:"workflow_run_id"`
	OrgID             string            `json:"org_id"`
	ProjectID         string            `json:"project_id"`
	RepoID            string            `json:"repo_id"`
	ActorID           string            `json:"actor_id"`
	Backend           string            `json:"backend"`
	WorkflowPath      string            `json:"workflow_path"`
	Ref               string            `json:"ref"`
	Inputs            map[string]string `json:"inputs"`
	State             string            `json:"state"`
	BackendDispatchID string            `json:"backend_dispatch_id,omitempty"`
	FailureReason     string            `json:"failure_reason,omitempty"`
	TraceID           string            `json:"trace_id,omitempty"`
	DispatchedAt      *time.Time        `json:"dispatched_at,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

type SourceWorkflowRunList struct {
	WorkflowRuns []SourceWorkflowRun `json:"workflow_runs"`
}

type SourceMutationOptions struct {
	IdempotencyKey string
}

type ListSourceRepositoriesOptions struct {
	ProjectID string
}

type CreateSourceRepositoryInput struct {
	ProjectID      string
	Description    string
	DefaultBranch  string
	IdempotencyKey string
}

type CreateSourceGitCredentialInput struct {
	Label            string
	ExpiresInSeconds int64
	Scopes           []string
	IdempotencyKey   string
}

type GetSourceTreeOptions struct {
	RepoID string
	Ref    string
	Path   string
}

type GetSourceBlobOptions struct {
	RepoID string
	Ref    string
	Path   string
}

type CreateSourceCheckoutGrantInput struct {
	RepoID         string
	Ref            string
	IdempotencyKey string
}

type CreateSourceWorkflowRunInput struct {
	RepoID         string
	ProjectID      string
	WorkflowPath   string
	Ref            string
	Inputs         map[string]string
	IdempotencyKey string
}

type SourceClient struct {
	client *sourcecore.ClientWithResponses
}

func (c *SourceClient) ListRepositories(ctx context.Context, options ListSourceRepositoriesOptions) (SourceRepositoryList, error) {
	if c == nil || c.client == nil {
		return SourceRepositoryList{}, fmt.Errorf("verself sdk: source client is not initialized")
	}
	params := &sourcecore.ListSourceRepositoriesParams{}
	if strings.TrimSpace(options.ProjectID) != "" {
		projectID, err := parseUUIDInput(options.ProjectID, "project id")
		if err != nil {
			return SourceRepositoryList{}, err
		}
		params.ProjectId = &projectID
	}
	response, err := c.client.ListSourceRepositoriesWithResponse(ctx, params)
	if err != nil {
		return SourceRepositoryList{}, err
	}
	if response.JSON200 == nil {
		return SourceRepositoryList{}, sourceAPIError("list repositories", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return sourceRepositoryListFromGenerated(*response.JSON200), nil
}

func (c *SourceClient) CreateRepository(ctx context.Context, input CreateSourceRepositoryInput) (SourceRepository, error) {
	if c == nil || c.client == nil {
		return SourceRepository{}, fmt.Errorf("verself sdk: source client is not initialized")
	}
	projectID, err := parseUUIDInput(input.ProjectID, "project id")
	if err != nil {
		return SourceRepository{}, err
	}
	key, err := mutationKey("source-repository", input.IdempotencyKey)
	if err != nil {
		return SourceRepository{}, err
	}
	body := sourcecore.CreateSourceRepositoryJSONRequestBody{ProjectId: projectID.String()}
	if strings.TrimSpace(input.Description) != "" {
		description := strings.TrimSpace(input.Description)
		body.Description = &description
	}
	if strings.TrimSpace(input.DefaultBranch) != "" {
		defaultBranch := strings.TrimSpace(input.DefaultBranch)
		body.DefaultBranch = &defaultBranch
	}
	response, err := c.client.CreateSourceRepositoryWithResponse(ctx, &sourcecore.CreateSourceRepositoryParams{IdempotencyKey: key}, body)
	if err != nil {
		return SourceRepository{}, err
	}
	if response.JSON201 == nil {
		return SourceRepository{}, sourceAPIError("create repository", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return sourceRepositoryFromGenerated(*response.JSON201), nil
}

func (c *SourceClient) GetRepository(ctx context.Context, repoID string) (SourceRepository, error) {
	if c == nil || c.client == nil {
		return SourceRepository{}, fmt.Errorf("verself sdk: source client is not initialized")
	}
	id, err := parseUUIDInput(repoID, "repository id")
	if err != nil {
		return SourceRepository{}, err
	}
	response, err := c.client.GetSourceRepositoryWithResponse(ctx, id)
	if err != nil {
		return SourceRepository{}, err
	}
	if response.JSON200 == nil {
		return SourceRepository{}, sourceAPIError("get repository", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return sourceRepositoryFromGenerated(*response.JSON200), nil
}

func (c *SourceClient) CreateGitCredential(ctx context.Context, input CreateSourceGitCredentialInput) (SourceGitCredential, error) {
	if c == nil || c.client == nil {
		return SourceGitCredential{}, fmt.Errorf("verself sdk: source client is not initialized")
	}
	key, err := mutationKey("source-git-credential", input.IdempotencyKey)
	if err != nil {
		return SourceGitCredential{}, err
	}
	scopes := compactStrings(input.Scopes)
	if len(scopes) == 0 {
		return SourceGitCredential{}, fmt.Errorf("verself sdk: source git credential requires at least one scope")
	}
	body := sourcecore.CreateSourceGitCredentialJSONRequestBody{Scopes: &scopes}
	if strings.TrimSpace(input.Label) != "" {
		label := strings.TrimSpace(input.Label)
		body.Label = &label
	}
	if input.ExpiresInSeconds > 0 {
		body.ExpiresInSeconds = &input.ExpiresInSeconds
	}
	response, err := c.client.CreateSourceGitCredentialWithResponse(ctx, &sourcecore.CreateSourceGitCredentialParams{IdempotencyKey: key}, body)
	if err != nil {
		return SourceGitCredential{}, err
	}
	if response.JSON201 == nil {
		return SourceGitCredential{}, sourceAPIError("create git credential", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return sourceGitCredentialFromGenerated(*response.JSON201), nil
}

func (c *SourceClient) ListRefs(ctx context.Context, repoID string) (SourceRefs, error) {
	if c == nil || c.client == nil {
		return SourceRefs{}, fmt.Errorf("verself sdk: source client is not initialized")
	}
	id, err := parseUUIDInput(repoID, "repository id")
	if err != nil {
		return SourceRefs{}, err
	}
	response, err := c.client.ListSourceRefsWithResponse(ctx, id)
	if err != nil {
		return SourceRefs{}, err
	}
	if response.JSON200 == nil {
		return SourceRefs{}, sourceAPIError("list refs", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return sourceRefsFromGenerated(*response.JSON200), nil
}

func (c *SourceClient) GetTree(ctx context.Context, options GetSourceTreeOptions) (SourceTree, error) {
	if c == nil || c.client == nil {
		return SourceTree{}, fmt.Errorf("verself sdk: source client is not initialized")
	}
	repoID, err := parseUUIDInput(options.RepoID, "repository id")
	if err != nil {
		return SourceTree{}, err
	}
	params := &sourcecore.GetSourceTreeParams{}
	if strings.TrimSpace(options.Ref) != "" {
		ref := strings.TrimSpace(options.Ref)
		params.Ref = &ref
	}
	if strings.TrimSpace(options.Path) != "" {
		path := strings.TrimSpace(options.Path)
		params.Path = &path
	}
	response, err := c.client.GetSourceTreeWithResponse(ctx, repoID, params)
	if err != nil {
		return SourceTree{}, err
	}
	if response.JSON200 == nil {
		return SourceTree{}, sourceAPIError("get tree", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return sourceTreeFromGenerated(*response.JSON200), nil
}

func (c *SourceClient) GetBlob(ctx context.Context, options GetSourceBlobOptions) (SourceBlob, error) {
	if c == nil || c.client == nil {
		return SourceBlob{}, fmt.Errorf("verself sdk: source client is not initialized")
	}
	repoID, err := parseUUIDInput(options.RepoID, "repository id")
	if err != nil {
		return SourceBlob{}, err
	}
	path := strings.TrimSpace(options.Path)
	if path == "" {
		return SourceBlob{}, fmt.Errorf("verself sdk: source blob path is required")
	}
	params := &sourcecore.GetSourceBlobParams{Path: path}
	if strings.TrimSpace(options.Ref) != "" {
		ref := strings.TrimSpace(options.Ref)
		params.Ref = &ref
	}
	response, err := c.client.GetSourceBlobWithResponse(ctx, repoID, params)
	if err != nil {
		return SourceBlob{}, err
	}
	if response.JSON200 == nil {
		return SourceBlob{}, sourceAPIError("get blob", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return sourceBlobFromGenerated(*response.JSON200), nil
}

func (c *SourceClient) CreateCheckoutGrant(ctx context.Context, input CreateSourceCheckoutGrantInput) (SourceCheckoutGrant, error) {
	if c == nil || c.client == nil {
		return SourceCheckoutGrant{}, fmt.Errorf("verself sdk: source client is not initialized")
	}
	repoID, err := parseUUIDInput(input.RepoID, "repository id")
	if err != nil {
		return SourceCheckoutGrant{}, err
	}
	key, err := mutationKey("source-checkout", input.IdempotencyKey)
	if err != nil {
		return SourceCheckoutGrant{}, err
	}
	body := sourcecore.CreateSourceCheckoutGrantJSONRequestBody{}
	if strings.TrimSpace(input.Ref) != "" {
		ref := strings.TrimSpace(input.Ref)
		body.Ref = &ref
	}
	response, err := c.client.CreateSourceCheckoutGrantWithResponse(ctx, repoID, &sourcecore.CreateSourceCheckoutGrantParams{IdempotencyKey: key}, body)
	if err != nil {
		return SourceCheckoutGrant{}, err
	}
	if response.JSON201 == nil {
		return SourceCheckoutGrant{}, sourceAPIError("create checkout grant", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return sourceCheckoutGrantFromGenerated(*response.JSON201), nil
}

func (c *SourceClient) CreateWorkflowRun(ctx context.Context, input CreateSourceWorkflowRunInput) (SourceWorkflowRun, error) {
	if c == nil || c.client == nil {
		return SourceWorkflowRun{}, fmt.Errorf("verself sdk: source client is not initialized")
	}
	repoID, err := parseUUIDInput(input.RepoID, "repository id")
	if err != nil {
		return SourceWorkflowRun{}, err
	}
	projectID, err := parseUUIDInput(input.ProjectID, "project id")
	if err != nil {
		return SourceWorkflowRun{}, err
	}
	key, err := mutationKey("source-workflow", input.IdempotencyKey)
	if err != nil {
		return SourceWorkflowRun{}, err
	}
	workflowPath := strings.TrimSpace(input.WorkflowPath)
	if workflowPath == "" {
		return SourceWorkflowRun{}, fmt.Errorf("verself sdk: source workflow path is required")
	}
	body := sourcecore.CreateSourceWorkflowRunJSONRequestBody{
		ProjectId:    projectID.String(),
		WorkflowPath: workflowPath,
	}
	if strings.TrimSpace(input.Ref) != "" {
		ref := strings.TrimSpace(input.Ref)
		body.Ref = &ref
	}
	if input.Inputs != nil {
		inputs := copyStringMap(input.Inputs)
		body.Inputs = &inputs
	}
	response, err := c.client.CreateSourceWorkflowRunWithResponse(ctx, repoID, &sourcecore.CreateSourceWorkflowRunParams{IdempotencyKey: key}, body)
	if err != nil {
		return SourceWorkflowRun{}, err
	}
	if response.JSON201 == nil {
		return SourceWorkflowRun{}, sourceAPIError("create workflow run", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return sourceWorkflowRunFromGenerated(*response.JSON201), nil
}

func (c *SourceClient) ListWorkflowRuns(ctx context.Context, repoID string) (SourceWorkflowRunList, error) {
	if c == nil || c.client == nil {
		return SourceWorkflowRunList{}, fmt.Errorf("verself sdk: source client is not initialized")
	}
	id, err := parseUUIDInput(repoID, "repository id")
	if err != nil {
		return SourceWorkflowRunList{}, err
	}
	response, err := c.client.ListSourceWorkflowRunsWithResponse(ctx, id)
	if err != nil {
		return SourceWorkflowRunList{}, err
	}
	if response.JSON200 == nil {
		return SourceWorkflowRunList{}, sourceAPIError("list workflow runs", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return sourceWorkflowRunListFromGenerated(*response.JSON200), nil
}

func (c *SourceClient) GetWorkflowRun(ctx context.Context, workflowRunID string) (SourceWorkflowRun, error) {
	if c == nil || c.client == nil {
		return SourceWorkflowRun{}, fmt.Errorf("verself sdk: source client is not initialized")
	}
	id, err := parseUUIDInput(workflowRunID, "workflow run id")
	if err != nil {
		return SourceWorkflowRun{}, err
	}
	response, err := c.client.GetSourceWorkflowRunWithResponse(ctx, id)
	if err != nil {
		return SourceWorkflowRun{}, err
	}
	if response.JSON200 == nil {
		return SourceWorkflowRun{}, sourceAPIError("get workflow run", response.StatusCode(), response.ApplicationproblemJSONDefault, response.Body)
	}
	return sourceWorkflowRunFromGenerated(*response.JSON200), nil
}

func sourceRepositoryListFromGenerated(input sourcecore.RepositoryList) SourceRepositoryList {
	out := SourceRepositoryList{}
	if input.Repositories != nil {
		out.Repositories = make([]SourceRepository, 0, len(*input.Repositories))
		for _, repo := range *input.Repositories {
			out.Repositories = append(out.Repositories, sourceRepositoryFromGenerated(repo))
		}
	}
	return out
}

func sourceRepositoryFromGenerated(input sourcecore.Repository) SourceRepository {
	return SourceRepository{
		RepoID:        input.RepoId,
		OrgID:         input.OrgId,
		OrgSlug:       input.OrgSlug,
		ProjectID:     input.ProjectId,
		ProjectSlug:   input.ProjectSlug,
		Name:          input.Name,
		Description:   input.Description,
		DefaultBranch: input.DefaultBranch,
		Visibility:    input.Visibility,
		State:         input.State,
		Version:       input.Version,
		Backend:       input.Backend,
		GitHTTPURL:    input.GitHttpUrl,
		LastPushedAt:  input.LastPushedAt,
		CreatedAt:     input.CreatedAt,
		UpdatedAt:     input.UpdatedAt,
	}
}

func sourceRefsFromGenerated(input sourcecore.RefList) SourceRefs {
	out := SourceRefs{}
	if input.Refs != nil {
		out.Refs = make([]SourceRef, 0, len(*input.Refs))
		for _, ref := range *input.Refs {
			out.Refs = append(out.Refs, SourceRef{Name: ref.Name, Commit: ref.Commit})
		}
	}
	return out
}

func sourceTreeFromGenerated(input sourcecore.Tree) SourceTree {
	out := SourceTree{}
	if input.Entries != nil {
		out.Entries = make([]SourceTreeEntry, 0, len(*input.Entries))
		for _, entry := range *input.Entries {
			out.Entries = append(out.Entries, SourceTreeEntry{
				Path: entry.Path,
				Type: entry.Type,
				Sha:  entry.Sha,
				Size: entry.Size,
			})
		}
	}
	return out
}

func sourceBlobFromGenerated(input sourcecore.Blob) SourceBlob {
	out := SourceBlob{
		Name:     input.Name,
		Path:     input.Path,
		Sha:      input.Sha,
		Size:     input.Size,
		Encoding: input.Encoding,
		Content:  input.Content,
	}
	if input.DownloadUrl != nil {
		out.DownloadURL = *input.DownloadUrl
	}
	return out
}

func sourceCheckoutGrantFromGenerated(input sourcecore.CheckoutGrant) SourceCheckoutGrant {
	return SourceCheckoutGrant{
		GrantID:   input.GrantId,
		RepoID:    input.RepoId,
		Ref:       input.Ref,
		Token:     input.Token,
		ExpiresAt: input.ExpiresAt,
	}
}

func sourceGitCredentialFromGenerated(input sourcecore.GitCredential) SourceGitCredential {
	out := SourceGitCredential{
		CredentialID: input.CredentialId,
		OrgID:        input.OrgId,
		Username:     input.Username,
		Token:        input.Token,
		TokenPrefix:  input.TokenPrefix,
		ExpiresAt:    input.ExpiresAt,
		CreatedAt:    input.CreatedAt,
	}
	if input.Scopes != nil {
		out.Scopes = append([]string(nil), (*input.Scopes)...)
	}
	return out
}

func sourceWorkflowRunListFromGenerated(input sourcecore.WorkflowRunList) SourceWorkflowRunList {
	out := SourceWorkflowRunList{}
	if input.WorkflowRuns != nil {
		out.WorkflowRuns = make([]SourceWorkflowRun, 0, len(*input.WorkflowRuns))
		for _, run := range *input.WorkflowRuns {
			out.WorkflowRuns = append(out.WorkflowRuns, sourceWorkflowRunFromGenerated(run))
		}
	}
	return out
}

func sourceWorkflowRunFromGenerated(input sourcecore.WorkflowRun) SourceWorkflowRun {
	out := SourceWorkflowRun{
		WorkflowRunID: input.WorkflowRunId,
		OrgID:         input.OrgId,
		ProjectID:     input.ProjectId,
		RepoID:        input.RepoId,
		ActorID:       input.ActorId,
		Backend:       input.Backend,
		WorkflowPath:  input.WorkflowPath,
		Ref:           input.Ref,
		Inputs:        copyStringMap(input.Inputs),
		State:         input.State,
		DispatchedAt:  input.DispatchedAt,
		CreatedAt:     input.CreatedAt,
		UpdatedAt:     input.UpdatedAt,
	}
	if input.BackendDispatchId != nil {
		out.BackendDispatchID = *input.BackendDispatchId
	}
	if input.FailureReason != nil {
		out.FailureReason = *input.FailureReason
	}
	if input.TraceId != nil {
		out.TraceID = *input.TraceId
	}
	return out
}

func sourceAPIError(operation string, statusCode int, model *sourcecore.ErrorModel, body []byte) error {
	var title *string
	var detail *string
	if model != nil {
		title = model.Title
		detail = model.Detail
	}
	return apiErrorFields("Source API", operation, statusCode, title, detail, body)
}

func compactStrings(input []string) []string {
	out := make([]string, 0, len(input))
	seen := map[string]struct{}{}
	for _, value := range input {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}
