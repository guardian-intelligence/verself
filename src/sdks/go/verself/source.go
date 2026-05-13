package verself

import (
	"context"
	"encoding/json"
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
	RepoID              string     `json:"repo_id"`
	ResourceName        string     `json:"resourceName"`
	OrgID               string     `json:"org_id"`
	OrgSlug             string     `json:"org_slug"`
	ProjectID           string     `json:"project_id"`
	ProjectResourceName string     `json:"projectResourceName"`
	ProjectSlug         string     `json:"project_slug"`
	Name                string     `json:"name"`
	Description         string     `json:"description"`
	DefaultBranch       string     `json:"default_branch"`
	Visibility          string     `json:"visibility"`
	State               string     `json:"state"`
	Version             int32      `json:"version"`
	Backend             string     `json:"backend"`
	GitHTTPURL          string     `json:"git_http_url"`
	LastPushedAt        *time.Time `json:"last_pushed_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
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
	GrantID      string    `json:"grant_id"`
	ResourceName string    `json:"resourceName"`
	RepoID       string    `json:"repo_id"`
	Ref          string    `json:"ref"`
	Token        string    `json:"token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type SourceGitCredential struct {
	CredentialID string    `json:"credential_id"`
	ResourceName string    `json:"resourceName"`
	OrgID        string    `json:"org_id"`
	Username     string    `json:"username"`
	Token        string    `json:"token"`
	TokenPrefix  string    `json:"token_prefix"`
	Scopes       []string  `json:"scopes"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
}

type SourceWorkflowRun struct {
	WorkflowRunID          string            `json:"workflow_run_id"`
	ResourceName           string            `json:"resourceName"`
	OrgID                  string            `json:"org_id"`
	ProjectID              string            `json:"project_id"`
	ProjectResourceName    string            `json:"projectResourceName"`
	RepoID                 string            `json:"repo_id"`
	RepositoryResourceName string            `json:"repositoryResourceName"`
	ActorID                string            `json:"actor_id"`
	Backend                string            `json:"backend"`
	WorkflowPath           string            `json:"workflow_path"`
	Ref                    string            `json:"ref"`
	Inputs                 map[string]string `json:"inputs"`
	State                  string            `json:"state"`
	BackendDispatchID      string            `json:"backend_dispatch_id,omitempty"`
	FailureReason          string            `json:"failure_reason,omitempty"`
	TraceID                string            `json:"trace_id,omitempty"`
	DispatchedAt           *time.Time        `json:"dispatched_at,omitempty"`
	CreatedAt              time.Time         `json:"created_at"`
	UpdatedAt              time.Time         `json:"updated_at"`
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
	client *sourcecore.Client
}

func (c *SourceClient) ListRepositories(ctx context.Context, options ListSourceRepositoriesOptions) (SourceRepositoryList, error) {
	if c == nil || c.client == nil {
		return SourceRepositoryList{}, fmt.Errorf("verself sdk: source client is not initialized")
	}
	request := sourcecore.ListSourceRepositoriesRequest{}
	if strings.TrimSpace(options.ProjectID) != "" {
		projectID, err := parseUUIDInput(options.ProjectID, "project id")
		if err != nil {
			return SourceRepositoryList{}, err
		}
		id := sourcecore.ProjectId(projectID.String())
		request.ProjectID = &id
	}
	response, err := c.client.ListSourceRepositories(ctx, request)
	if err != nil {
		return SourceRepositoryList{}, err
	}
	if response.Result == nil {
		return SourceRepositoryList{}, sourceAPIError("list repositories", response.StatusCode, response.Body)
	}
	return sourceRepositoryListFromGenerated(response.Result.Repositories)
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
	body := sourcecore.CreateSourceRepositoryInputBody{ProjectID: sourcecore.ProjectId(projectID.String())}
	if strings.TrimSpace(input.Description) != "" {
		description := sourcecore.RepositoryDescription(strings.TrimSpace(input.Description))
		body.Description = &description
	}
	if strings.TrimSpace(input.DefaultBranch) != "" {
		defaultBranch := sourcecore.BranchName(strings.TrimSpace(input.DefaultBranch))
		body.DefaultBranch = &defaultBranch
	}
	response, err := c.client.CreateSourceRepository(ctx, sourcecore.CreateSourceRepositoryRequest{
		IdempotencyKey: sourcecore.IdempotencyKey(key),
		Body:           body,
	})
	if err != nil {
		return SourceRepository{}, err
	}
	if response.Result == nil {
		return SourceRepository{}, sourceAPIError("create repository", response.StatusCode, response.Body)
	}
	return sourceRepositoryFromGenerated(*response.Result)
}

func (c *SourceClient) GetRepository(ctx context.Context, repoID string) (SourceRepository, error) {
	if c == nil || c.client == nil {
		return SourceRepository{}, fmt.Errorf("verself sdk: source client is not initialized")
	}
	id, err := parseUUIDInput(repoID, "repository id")
	if err != nil {
		return SourceRepository{}, err
	}
	response, err := c.client.GetSourceRepository(ctx, sourcecore.GetSourceRepositoryRequest{RepoID: sourcecore.RepositoryId(id.String())})
	if err != nil {
		return SourceRepository{}, err
	}
	if response.Result == nil {
		return SourceRepository{}, sourceAPIError("get repository", response.StatusCode, response.Body)
	}
	return sourceRepositoryFromGenerated(*response.Result)
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
	body := sourcecore.CreateSourceGitCredentialInputBody{Scopes: sourceGitScopes(scopes)}
	if strings.TrimSpace(input.Label) != "" {
		label := sourcecore.GitCredentialLabel(strings.TrimSpace(input.Label))
		body.Label = &label
	}
	if input.ExpiresInSeconds > 0 {
		expiresInSeconds := sourcecore.CredentialExpirySeconds(input.ExpiresInSeconds)
		body.ExpiresInSeconds = &expiresInSeconds
	}
	response, err := c.client.CreateSourceGitCredential(ctx, sourcecore.CreateSourceGitCredentialRequest{
		IdempotencyKey: sourcecore.IdempotencyKey(key),
		Body:           body,
	})
	if err != nil {
		return SourceGitCredential{}, err
	}
	if response.Result == nil {
		return SourceGitCredential{}, sourceAPIError("create git credential", response.StatusCode, response.Body)
	}
	return sourceGitCredentialFromGenerated(*response.Result)
}

func (c *SourceClient) ListRefs(ctx context.Context, repoID string) (SourceRefs, error) {
	if c == nil || c.client == nil {
		return SourceRefs{}, fmt.Errorf("verself sdk: source client is not initialized")
	}
	id, err := parseUUIDInput(repoID, "repository id")
	if err != nil {
		return SourceRefs{}, err
	}
	response, err := c.client.ListSourceRefs(ctx, sourcecore.ListSourceRefsRequest{RepoID: sourcecore.RepositoryId(id.String())})
	if err != nil {
		return SourceRefs{}, err
	}
	if response.Result == nil {
		return SourceRefs{}, sourceAPIError("list refs", response.StatusCode, response.Body)
	}
	return sourceRefsFromGenerated(response.Result.Refs), nil
}

func (c *SourceClient) GetTree(ctx context.Context, options GetSourceTreeOptions) (SourceTree, error) {
	if c == nil || c.client == nil {
		return SourceTree{}, fmt.Errorf("verself sdk: source client is not initialized")
	}
	repoID, err := parseUUIDInput(options.RepoID, "repository id")
	if err != nil {
		return SourceTree{}, err
	}
	request := sourcecore.GetSourceTreeRequest{RepoID: sourcecore.RepositoryId(repoID.String())}
	if strings.TrimSpace(options.Ref) != "" {
		value := sourcecore.GitRef(strings.TrimSpace(options.Ref))
		request.Ref = &value
	}
	if strings.TrimSpace(options.Path) != "" {
		value := sourcecore.RepositoryPath(strings.TrimSpace(options.Path))
		request.Path = &value
	}
	response, err := c.client.GetSourceTree(ctx, request)
	if err != nil {
		return SourceTree{}, err
	}
	if response.Result == nil {
		return SourceTree{}, sourceAPIError("get tree", response.StatusCode, response.Body)
	}
	return sourceTreeFromGenerated(response.Result.Entries), nil
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
	request := sourcecore.GetSourceBlobRequest{
		RepoID: sourcecore.RepositoryId(repoID.String()),
		Path:   sourcecore.RepositoryPath(path),
	}
	if strings.TrimSpace(options.Ref) != "" {
		value := sourcecore.GitRef(strings.TrimSpace(options.Ref))
		request.Ref = &value
	}
	response, err := c.client.GetSourceBlob(ctx, request)
	if err != nil {
		return SourceBlob{}, err
	}
	if response.Result == nil {
		return SourceBlob{}, sourceAPIError("get blob", response.StatusCode, response.Body)
	}
	return sourceBlobFromGenerated(*response.Result), nil
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
	body := sourcecore.CreateSourceCheckoutGrantInputBody{}
	if strings.TrimSpace(input.Ref) != "" {
		value := sourcecore.GitRef(strings.TrimSpace(input.Ref))
		body.Ref = &value
	}
	response, err := c.client.CreateSourceCheckoutGrant(ctx, sourcecore.CreateSourceCheckoutGrantRequest{
		IdempotencyKey: sourcecore.IdempotencyKey(key),
		RepoID:         sourcecore.RepositoryId(repoID.String()),
		Body:           body,
	})
	if err != nil {
		return SourceCheckoutGrant{}, err
	}
	if response.Result == nil {
		return SourceCheckoutGrant{}, sourceAPIError("create checkout grant", response.StatusCode, response.Body)
	}
	return sourceCheckoutGrantFromGenerated(*response.Result)
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
	body := sourcecore.CreateSourceWorkflowRunInputBody{
		ProjectID:    sourcecore.ProjectId(projectID.String()),
		WorkflowPath: sourcecore.WorkflowPath(workflowPath),
	}
	if strings.TrimSpace(input.Ref) != "" {
		value := sourcecore.GitRef(strings.TrimSpace(input.Ref))
		body.Ref = &value
	}
	if input.Inputs != nil {
		inputs := sourceWorkflowInputs(input.Inputs)
		body.Inputs = &inputs
	}
	response, err := c.client.CreateSourceWorkflowRun(ctx, sourcecore.CreateSourceWorkflowRunRequest{
		IdempotencyKey: sourcecore.IdempotencyKey(key),
		RepoID:         sourcecore.RepositoryId(repoID.String()),
		Body:           body,
	})
	if err != nil {
		return SourceWorkflowRun{}, err
	}
	if response.Result == nil {
		return SourceWorkflowRun{}, sourceAPIError("create workflow run", response.StatusCode, response.Body)
	}
	return sourceWorkflowRunFromGenerated(*response.Result)
}

func (c *SourceClient) ListWorkflowRuns(ctx context.Context, repoID string) (SourceWorkflowRunList, error) {
	if c == nil || c.client == nil {
		return SourceWorkflowRunList{}, fmt.Errorf("verself sdk: source client is not initialized")
	}
	id, err := parseUUIDInput(repoID, "repository id")
	if err != nil {
		return SourceWorkflowRunList{}, err
	}
	response, err := c.client.ListSourceWorkflowRuns(ctx, sourcecore.ListSourceWorkflowRunsRequest{RepoID: sourcecore.RepositoryId(id.String())})
	if err != nil {
		return SourceWorkflowRunList{}, err
	}
	if response.Result == nil {
		return SourceWorkflowRunList{}, sourceAPIError("list workflow runs", response.StatusCode, response.Body)
	}
	return sourceWorkflowRunListFromGenerated(response.Result.WorkflowRuns)
}

func (c *SourceClient) GetWorkflowRun(ctx context.Context, workflowRunID string) (SourceWorkflowRun, error) {
	if c == nil || c.client == nil {
		return SourceWorkflowRun{}, fmt.Errorf("verself sdk: source client is not initialized")
	}
	id, err := parseUUIDInput(workflowRunID, "workflow run id")
	if err != nil {
		return SourceWorkflowRun{}, err
	}
	response, err := c.client.GetSourceWorkflowRun(ctx, sourcecore.GetSourceWorkflowRunRequest{WorkflowRunID: sourcecore.WorkflowRunId(id.String())})
	if err != nil {
		return SourceWorkflowRun{}, err
	}
	if response.Result == nil {
		return SourceWorkflowRun{}, sourceAPIError("get workflow run", response.StatusCode, response.Body)
	}
	return sourceWorkflowRunFromGenerated(*response.Result)
}

func sourceRepositoryListFromGenerated(input sourcecore.Repositories) (SourceRepositoryList, error) {
	out := SourceRepositoryList{Repositories: make([]SourceRepository, 0, len(input))}
	for _, repo := range input {
		converted, err := sourceRepositoryFromGenerated(repo)
		if err != nil {
			return SourceRepositoryList{}, err
		}
		out.Repositories = append(out.Repositories, converted)
	}
	return out, nil
}

func sourceRepositoryFromGenerated(input sourcecore.RepositorySummary) (SourceRepository, error) {
	version, err := sourceRepositoryVersion(input.Version)
	if err != nil {
		return SourceRepository{}, err
	}
	lastPushedAt, err := parseGeneratedOptionalTime(input.LastPushedAt, "source repository last_pushed_at")
	if err != nil {
		return SourceRepository{}, err
	}
	createdAt, err := parseGeneratedTime(input.CreatedAt, "source repository created_at")
	if err != nil {
		return SourceRepository{}, err
	}
	updatedAt, err := parseGeneratedTime(input.UpdatedAt, "source repository updated_at")
	if err != nil {
		return SourceRepository{}, err
	}
	return SourceRepository{
		RepoID:              input.RepoID,
		ResourceName:        input.ResourceName,
		OrgID:               input.OrgID,
		OrgSlug:             input.OrgSlug,
		ProjectID:           input.ProjectID,
		ProjectResourceName: input.ProjectResourceName,
		ProjectSlug:         input.ProjectSlug,
		Name:                input.Name,
		Description:         input.Description,
		DefaultBranch:       input.DefaultBranch,
		Visibility:          input.Visibility,
		State:               input.State,
		Version:             version,
		Backend:             input.Backend,
		GitHTTPURL:          input.GitHttpURL,
		LastPushedAt:        lastPushedAt,
		CreatedAt:           createdAt,
		UpdatedAt:           updatedAt,
	}, nil
}

func sourceRefsFromGenerated(input sourcecore.Refs) SourceRefs {
	out := SourceRefs{Refs: make([]SourceRef, 0, len(input))}
	for _, ref := range input {
		out.Refs = append(out.Refs, SourceRef{Name: ref.Name, Commit: ref.Commit})
	}
	return out
}

func sourceTreeFromGenerated(input sourcecore.TreeEntries) SourceTree {
	out := SourceTree{Entries: make([]SourceTreeEntry, 0, len(input))}
	for _, entry := range input {
		out.Entries = append(out.Entries, SourceTreeEntry{
			Path: entry.Path,
			Type: entry.Type,
			Sha:  entry.Sha,
			Size: entry.Size,
		})
	}
	return out
}

func sourceBlobFromGenerated(input sourcecore.SourceBlobView) SourceBlob {
	out := SourceBlob{
		Name:     input.Name,
		Path:     input.Path,
		Sha:      input.Sha,
		Size:     input.Size,
		Encoding: input.Encoding,
		Content:  input.Content,
	}
	if input.DownloadURL != nil {
		out.DownloadURL = *input.DownloadURL
	}
	return out
}

func sourceCheckoutGrantFromGenerated(input sourcecore.CheckoutGrantSummary) (SourceCheckoutGrant, error) {
	expiresAt, err := parseGeneratedTime(input.ExpiresAt, "source checkout grant expires_at")
	if err != nil {
		return SourceCheckoutGrant{}, err
	}
	return SourceCheckoutGrant{
		GrantID:      input.GrantID,
		ResourceName: input.ResourceName,
		RepoID:       input.RepoID,
		Ref:          input.Ref,
		Token:        input.Token,
		ExpiresAt:    expiresAt,
	}, nil
}

func sourceGitCredentialFromGenerated(input sourcecore.GitCredentialSummary) (SourceGitCredential, error) {
	expiresAt, err := parseGeneratedTime(input.ExpiresAt, "source git credential expires_at")
	if err != nil {
		return SourceGitCredential{}, err
	}
	createdAt, err := parseGeneratedTime(input.CreatedAt, "source git credential created_at")
	if err != nil {
		return SourceGitCredential{}, err
	}
	out := SourceGitCredential{
		CredentialID: input.CredentialID,
		ResourceName: input.ResourceName,
		OrgID:        input.OrgID,
		Username:     input.Username,
		Token:        input.Token,
		TokenPrefix:  input.TokenPrefix,
		ExpiresAt:    expiresAt,
		CreatedAt:    createdAt,
	}
	if input.Scopes != nil {
		out.Scopes = make([]string, 0, len(input.Scopes))
		for _, scope := range input.Scopes {
			out.Scopes = append(out.Scopes, string(scope))
		}
	}
	return out, nil
}

func sourceWorkflowRunListFromGenerated(input sourcecore.WorkflowRuns) (SourceWorkflowRunList, error) {
	out := SourceWorkflowRunList{WorkflowRuns: make([]SourceWorkflowRun, 0, len(input))}
	for _, run := range input {
		converted, err := sourceWorkflowRunFromGenerated(run)
		if err != nil {
			return SourceWorkflowRunList{}, err
		}
		out.WorkflowRuns = append(out.WorkflowRuns, converted)
	}
	return out, nil
}

func sourceWorkflowRunFromGenerated(input sourcecore.WorkflowRunSummary) (SourceWorkflowRun, error) {
	dispatchedAt, err := parseGeneratedOptionalTime(input.DispatchedAt, "source workflow run dispatched_at")
	if err != nil {
		return SourceWorkflowRun{}, err
	}
	createdAt, err := parseGeneratedTime(input.CreatedAt, "source workflow run created_at")
	if err != nil {
		return SourceWorkflowRun{}, err
	}
	updatedAt, err := parseGeneratedTime(input.UpdatedAt, "source workflow run updated_at")
	if err != nil {
		return SourceWorkflowRun{}, err
	}
	out := SourceWorkflowRun{
		WorkflowRunID:          input.WorkflowRunID,
		ResourceName:           input.ResourceName,
		OrgID:                  input.OrgID,
		ProjectID:              input.ProjectID,
		ProjectResourceName:    input.ProjectResourceName,
		RepoID:                 input.RepoID,
		RepositoryResourceName: input.RepositoryResourceName,
		ActorID:                input.ActorID,
		Backend:                input.Backend,
		WorkflowPath:           input.WorkflowPath,
		Ref:                    input.Ref,
		Inputs:                 sourceWorkflowInputsFromGenerated(input.Inputs),
		State:                  input.State,
		DispatchedAt:           dispatchedAt,
		CreatedAt:              createdAt,
		UpdatedAt:              updatedAt,
	}
	if input.BackendDispatchID != nil {
		out.BackendDispatchID = *input.BackendDispatchID
	}
	if input.FailureReason != nil {
		out.FailureReason = *input.FailureReason
	}
	if input.TraceID != nil {
		out.TraceID = *input.TraceID
	}
	return out, nil
}

func sourceAPIError(operation string, statusCode int, body []byte) error {
	var problem struct {
		Title  *string `json:"title"`
		Detail *string `json:"detail"`
	}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &problem)
	}
	return apiErrorFields("Source API", operation, statusCode, problem.Title, problem.Detail, body)
}

func sourceGitScopes(input []string) sourcecore.GitScopes {
	out := make(sourcecore.GitScopes, 0, len(input))
	for _, scope := range input {
		out = append(out, sourcecore.GitScope(scope))
	}
	return out
}

func sourceWorkflowInputs(input map[string]string) sourcecore.WorkflowInputs {
	out := make(sourcecore.WorkflowInputs, len(input))
	for key, value := range input {
		out[key] = sourcecore.WorkflowInputValue(value)
	}
	return out
}

func sourceWorkflowInputsFromGenerated(input sourcecore.WorkflowInputs) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = string(value)
	}
	return out
}

func sourceRepositoryVersion(input sourcecore.RepositoryVersion) (int32, error) {
	if input < -2147483648 || input > 2147483647 {
		return 0, fmt.Errorf("verself sdk: source repository version %d is outside int32 bounds", input)
	}
	return int32(input), nil // #nosec G115 -- value is checked against the int32 range above.
}
