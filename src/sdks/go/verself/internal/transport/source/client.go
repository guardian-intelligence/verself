package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const ServiceName = "source-code-hosting-service"

type ActorId = string

type BackendDispatchId = string

type BlobDownloadURL = string

type BranchName = string

type CheckoutGrantId = string

type CheckoutGrantToken = string

type CredentialExpirySeconds = int64

type FailureReason = string

type GitCredentialId = string

type GitCredentialLabel = string

type GitObjectEncoding = string

type GitObjectName = string

type GitObjectSha = string

type GitObjectType = string

type GitRef = string

type GitScope = string

type GitToken = string

type GitTokenPrefix = string

type GitURL = string

type GitUsername = string

type IdempotencyKey = string

type OrgId = string

type OrgSlug = string

type ProblemCode = string

type ProblemDetail = string

type ProblemType = string

type ProjectId = string

type ProjectSlug = string

type RepositoryDescription = string

type RepositoryId = string

type RepositoryName = string

type RepositoryPath = string

type RepositoryVersion = int64

type RepositoryVisibility = string

type RequestId = string

type ResourceName = string

type SafeByteCount = int64

type SourceBackend = string

type SourceState = string

type TraceId = string

type TraceParent = string

type WorkflowInputName = string

type WorkflowInputValue = string

type WorkflowPath = string

type WorkflowRunId = string

type GitScopes []GitScope

type Refs []SourceRefView

type Repositories []RepositorySummary

type TreeEntries []TreeEntry

type WorkflowRuns []WorkflowRunSummary

type WorkflowInputs map[string]WorkflowInputValue

type CheckoutGrantSummary struct {
	GrantID      CheckoutGrantId    `json:"grant_id"`
	ResourceName ResourceName       `json:"resourceName"`
	RepoID       RepositoryId       `json:"repo_id"`
	Ref          GitRef             `json:"ref"`
	Token        CheckoutGrantToken `json:"token"`
	ExpiresAt    string             `json:"expires_at"`
}

type ConflictError struct {
	Type        ProblemType    `json:"type"`
	Title       string         `json:"title"`
	Status      int64          `json:"status"`
	Detail      *ProblemDetail `json:"detail,omitempty"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code"`
	RequestID   *RequestId     `json:"requestId,omitempty"`
	Traceparent *TraceParent   `json:"traceparent,omitempty"`
}

type GitCredentialSummary struct {
	CredentialID GitCredentialId `json:"credential_id"`
	ResourceName ResourceName    `json:"resourceName"`
	OrgID        OrgId           `json:"org_id"`
	Username     GitUsername     `json:"username"`
	Token        GitToken        `json:"token"`
	Scopes       GitScopes       `json:"scopes"`
	TokenPrefix  GitTokenPrefix  `json:"token_prefix"`
	ExpiresAt    string          `json:"expires_at"`
	CreatedAt    string          `json:"created_at"`
}

type IdempotencyPayloadMismatchError struct {
	Type        ProblemType    `json:"type"`
	Title       string         `json:"title"`
	Status      int64          `json:"status"`
	Detail      *ProblemDetail `json:"detail,omitempty"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code"`
	RequestID   *RequestId     `json:"requestId,omitempty"`
	Traceparent *TraceParent   `json:"traceparent,omitempty"`
}

type PermissionDeniedError struct {
	Type        ProblemType    `json:"type"`
	Title       string         `json:"title"`
	Status      int64          `json:"status"`
	Detail      *ProblemDetail `json:"detail,omitempty"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code"`
	RequestID   *RequestId     `json:"requestId,omitempty"`
	Traceparent *TraceParent   `json:"traceparent,omitempty"`
}

type RateLimitedError struct {
	Type        ProblemType    `json:"type"`
	Title       string         `json:"title"`
	Status      int64          `json:"status"`
	Detail      *ProblemDetail `json:"detail,omitempty"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code"`
	RequestID   *RequestId     `json:"requestId,omitempty"`
	Traceparent *TraceParent   `json:"traceparent,omitempty"`
}

type RepositorySummary struct {
	RepoID              RepositoryId          `json:"repo_id"`
	ResourceName        ResourceName          `json:"resourceName"`
	OrgID               OrgId                 `json:"org_id"`
	ProjectID           ProjectId             `json:"project_id"`
	ProjectResourceName ResourceName          `json:"projectResourceName"`
	OrgSlug             OrgSlug               `json:"org_slug"`
	ProjectSlug         ProjectSlug           `json:"project_slug"`
	GitHttpURL          GitURL                `json:"git_http_url"`
	Name                RepositoryName        `json:"name"`
	Description         RepositoryDescription `json:"description"`
	DefaultBranch       BranchName            `json:"default_branch"`
	Backend             SourceBackend         `json:"backend"`
	Visibility          RepositoryVisibility  `json:"visibility"`
	State               SourceState           `json:"state"`
	Version             RepositoryVersion     `json:"version"`
	LastPushedAt        *string               `json:"last_pushed_at,omitempty"`
	CreatedAt           string                `json:"created_at"`
	UpdatedAt           string                `json:"updated_at"`
}

type ResourceNotFoundError struct {
	Type        ProblemType    `json:"type"`
	Title       string         `json:"title"`
	Status      int64          `json:"status"`
	Detail      *ProblemDetail `json:"detail,omitempty"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code"`
	RequestID   *RequestId     `json:"requestId,omitempty"`
	Traceparent *TraceParent   `json:"traceparent,omitempty"`
}

type ServiceUnavailableError struct {
	Type        ProblemType    `json:"type"`
	Title       string         `json:"title"`
	Status      int64          `json:"status"`
	Detail      *ProblemDetail `json:"detail,omitempty"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code"`
	RequestID   *RequestId     `json:"requestId,omitempty"`
	Traceparent *TraceParent   `json:"traceparent,omitempty"`
}

type SourceBlobView struct {
	Path        RepositoryPath    `json:"path"`
	Name        GitObjectName     `json:"name"`
	Encoding    GitObjectEncoding `json:"encoding"`
	Content     string            `json:"content"`
	Size        SafeByteCount     `json:"size"`
	Sha         GitObjectSha      `json:"sha"`
	DownloadURL *BlobDownloadURL  `json:"download_url,omitempty"`
}

type SourceRefView struct {
	Name   GitRef       `json:"name"`
	Commit GitObjectSha `json:"commit"`
}

type TreeEntry struct {
	Path RepositoryPath `json:"path"`
	Type GitObjectType  `json:"type"`
	Size SafeByteCount  `json:"size"`
	Sha  GitObjectSha   `json:"sha"`
}

type UnauthenticatedError struct {
	Type        ProblemType    `json:"type"`
	Title       string         `json:"title"`
	Status      int64          `json:"status"`
	Detail      *ProblemDetail `json:"detail,omitempty"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code"`
	RequestID   *RequestId     `json:"requestId,omitempty"`
	Traceparent *TraceParent   `json:"traceparent,omitempty"`
}

type ValidationFailedError struct {
	Type        ProblemType    `json:"type"`
	Title       string         `json:"title"`
	Status      int64          `json:"status"`
	Detail      *ProblemDetail `json:"detail,omitempty"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code"`
	RequestID   *RequestId     `json:"requestId,omitempty"`
	Traceparent *TraceParent   `json:"traceparent,omitempty"`
}

type WorkflowRunSummary struct {
	WorkflowRunID          WorkflowRunId      `json:"workflow_run_id"`
	ResourceName           ResourceName       `json:"resourceName"`
	OrgID                  OrgId              `json:"org_id"`
	ProjectID              ProjectId          `json:"project_id"`
	ProjectResourceName    ResourceName       `json:"projectResourceName"`
	RepoID                 RepositoryId       `json:"repo_id"`
	RepositoryResourceName ResourceName       `json:"repositoryResourceName"`
	ActorID                ActorId            `json:"actor_id"`
	Backend                SourceBackend      `json:"backend"`
	WorkflowPath           WorkflowPath       `json:"workflow_path"`
	Ref                    GitRef             `json:"ref"`
	Inputs                 WorkflowInputs     `json:"inputs"`
	State                  SourceState        `json:"state"`
	BackendDispatchID      *BackendDispatchId `json:"backend_dispatch_id,omitempty"`
	FailureReason          *FailureReason     `json:"failure_reason,omitempty"`
	TraceID                *TraceId           `json:"trace_id,omitempty"`
	DispatchedAt           *string            `json:"dispatched_at,omitempty"`
	CreatedAt              string             `json:"created_at"`
	UpdatedAt              string             `json:"updated_at"`
}

type CreateSourceCheckoutGrantInputBody struct {
	Ref *GitRef `json:"ref,omitempty"`
}

type CreateSourceCheckoutGrantRequest struct {
	IdempotencyKey IdempotencyKey
	RepoID         RepositoryId
	Body           CreateSourceCheckoutGrantInputBody `json:"body"`
}

type CreateSourceCheckoutGrantResponse struct {
	StatusCode   int
	Body         []byte
	Result       *CheckoutGrantSummary
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type CreateSourceGitCredentialInputBody struct {
	ExpiresInSeconds *CredentialExpirySeconds `json:"expires_in_seconds,omitempty"`
	Label            *GitCredentialLabel      `json:"label,omitempty"`
	Scopes           GitScopes                `json:"scopes"`
}

type CreateSourceGitCredentialRequest struct {
	IdempotencyKey IdempotencyKey
	Body           CreateSourceGitCredentialInputBody `json:"body"`
}

type CreateSourceGitCredentialResponse struct {
	StatusCode   int
	Body         []byte
	Result       *GitCredentialSummary
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type CreateSourceRepositoryInputBody struct {
	DefaultBranch *BranchName            `json:"default_branch,omitempty"`
	Description   *RepositoryDescription `json:"description,omitempty"`
	ProjectID     ProjectId              `json:"project_id"`
}

type CreateSourceRepositoryRequest struct {
	IdempotencyKey IdempotencyKey
	Body           CreateSourceRepositoryInputBody `json:"body"`
}

type CreateSourceRepositoryResponse struct {
	StatusCode   int
	Body         []byte
	Result       *RepositorySummary
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type CreateSourceWorkflowRunInputBody struct {
	Inputs       *WorkflowInputs `json:"inputs,omitempty"`
	ProjectID    ProjectId       `json:"project_id"`
	Ref          *GitRef         `json:"ref,omitempty"`
	WorkflowPath WorkflowPath    `json:"workflow_path"`
}

type CreateSourceWorkflowRunRequest struct {
	IdempotencyKey IdempotencyKey
	RepoID         RepositoryId
	Body           CreateSourceWorkflowRunInputBody `json:"body"`
}

type CreateSourceWorkflowRunResponse struct {
	StatusCode   int
	Body         []byte
	Result       *WorkflowRunSummary
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type GetSourceBlobRequest struct {
	Path   RepositoryPath
	Ref    *GitRef
	RepoID RepositoryId
}

type GetSourceBlobResponse struct {
	StatusCode   int
	Body         []byte
	Result       *SourceBlobView
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type GetSourceRepositoryRequest struct {
	RepoID RepositoryId
}

type GetSourceRepositoryResponse struct {
	StatusCode   int
	Body         []byte
	Result       *RepositorySummary
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type GetSourceTreeRequest struct {
	Path   *RepositoryPath
	Ref    *GitRef
	RepoID RepositoryId
}

type GetSourceTreeOutputBody struct {
	Entries TreeEntries `json:"entries"`
}

type GetSourceTreeResponse struct {
	StatusCode   int
	Body         []byte
	Result       *GetSourceTreeOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type GetSourceWorkflowRunRequest struct {
	WorkflowRunID WorkflowRunId
}

type GetSourceWorkflowRunResponse struct {
	StatusCode   int
	Body         []byte
	Result       *WorkflowRunSummary
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ListSourceRefsRequest struct {
	RepoID RepositoryId
}

type ListSourceRefsOutputBody struct {
	Refs Refs `json:"refs"`
}

type ListSourceRefsResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ListSourceRefsOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ListSourceRepositoriesRequest struct {
	ProjectID *ProjectId
}

type ListSourceRepositoriesOutputBody struct {
	Repositories Repositories `json:"repositories"`
}

type ListSourceRepositoriesResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ListSourceRepositoriesOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ListSourceWorkflowRunsRequest struct {
	RepoID RepositoryId
}

type ListSourceWorkflowRunsOutputBody struct {
	WorkflowRuns WorkflowRuns `json:"workflow_runs"`
}

type ListSourceWorkflowRunsResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ListSourceWorkflowRunsOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type RequestEditorFn func(ctx context.Context, req *http.Request) error

type HTTPRequestDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type ClientOption func(*Client)

type Client struct {
	server         string
	client         HTTPRequestDoer
	requestEditors []RequestEditorFn
}

func NewClient(server string, opts ...ClientOption) (*Client, error) {
	server = strings.TrimRight(strings.TrimSpace(server), "/")
	if server == "" {
		return nil, fmt.Errorf("%s SDK transport: server URL is required", ServiceName)
	}
	client := &Client{server: server, client: http.DefaultClient}
	for _, opt := range opts {
		opt(client)
	}
	if client.client == nil {
		return nil, fmt.Errorf("%s SDK transport: HTTP client is required", ServiceName)
	}
	return client, nil
}

func WithHTTPClient(client HTTPRequestDoer) ClientOption {
	return func(c *Client) { c.client = client }
}

func WithRequestEditorFn(editor RequestEditorFn) ClientOption {
	return func(c *Client) {
		if editor != nil {
			c.requestEditors = append(c.requestEditors, editor)
		}
	}
}

func (c *Client) CreateSourceCheckoutGrant(ctx context.Context, request CreateSourceCheckoutGrantRequest, reqEditors ...RequestEditorFn) (*CreateSourceCheckoutGrantResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newCreateSourceCheckoutGrantRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseCreateSourceCheckoutGrantResponse(resp)
}

func (c *Client) newCreateSourceCheckoutGrantRequest(ctx context.Context, request CreateSourceCheckoutGrantRequest) (*http.Request, error) {
	path := "/api/v1/repos/{repo_id}/checkout-grants"
	path = strings.ReplaceAll(path, "{repo_id}", url.PathEscape(fmt.Sprint(request.RepoID)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	requestBody, err := json.Marshal(request.Body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprint(request.IdempotencyKey))
	return req, nil
}

func (c *Client) CreateSourceGitCredential(ctx context.Context, request CreateSourceGitCredentialRequest, reqEditors ...RequestEditorFn) (*CreateSourceGitCredentialResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newCreateSourceGitCredentialRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseCreateSourceGitCredentialResponse(resp)
}

func (c *Client) newCreateSourceGitCredentialRequest(ctx context.Context, request CreateSourceGitCredentialRequest) (*http.Request, error) {
	path := "/api/v1/git-credentials"
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	requestBody, err := json.Marshal(request.Body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprint(request.IdempotencyKey))
	return req, nil
}

func (c *Client) CreateSourceRepository(ctx context.Context, request CreateSourceRepositoryRequest, reqEditors ...RequestEditorFn) (*CreateSourceRepositoryResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newCreateSourceRepositoryRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseCreateSourceRepositoryResponse(resp)
}

func (c *Client) newCreateSourceRepositoryRequest(ctx context.Context, request CreateSourceRepositoryRequest) (*http.Request, error) {
	path := "/api/v1/repos"
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	requestBody, err := json.Marshal(request.Body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprint(request.IdempotencyKey))
	return req, nil
}

func (c *Client) CreateSourceWorkflowRun(ctx context.Context, request CreateSourceWorkflowRunRequest, reqEditors ...RequestEditorFn) (*CreateSourceWorkflowRunResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newCreateSourceWorkflowRunRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseCreateSourceWorkflowRunResponse(resp)
}

func (c *Client) newCreateSourceWorkflowRunRequest(ctx context.Context, request CreateSourceWorkflowRunRequest) (*http.Request, error) {
	path := "/api/v1/repos/{repo_id}/workflow-runs"
	path = strings.ReplaceAll(path, "{repo_id}", url.PathEscape(fmt.Sprint(request.RepoID)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	requestBody, err := json.Marshal(request.Body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprint(request.IdempotencyKey))
	return req, nil
}

func (c *Client) GetSourceBlob(ctx context.Context, request GetSourceBlobRequest, reqEditors ...RequestEditorFn) (*GetSourceBlobResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newGetSourceBlobRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseGetSourceBlobResponse(resp)
}

func (c *Client) newGetSourceBlobRequest(ctx context.Context, request GetSourceBlobRequest) (*http.Request, error) {
	path := "/api/v1/repos/{repo_id}/blob"
	path = strings.ReplaceAll(path, "{repo_id}", url.PathEscape(fmt.Sprint(request.RepoID)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("path", fmt.Sprint(request.Path))
	if request.Ref != nil {
		query.Set("ref", fmt.Sprint(*request.Ref))
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) GetSourceRepository(ctx context.Context, request GetSourceRepositoryRequest, reqEditors ...RequestEditorFn) (*GetSourceRepositoryResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newGetSourceRepositoryRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseGetSourceRepositoryResponse(resp)
}

func (c *Client) newGetSourceRepositoryRequest(ctx context.Context, request GetSourceRepositoryRequest) (*http.Request, error) {
	path := "/api/v1/repos/{repo_id}"
	path = strings.ReplaceAll(path, "{repo_id}", url.PathEscape(fmt.Sprint(request.RepoID)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) GetSourceTree(ctx context.Context, request GetSourceTreeRequest, reqEditors ...RequestEditorFn) (*GetSourceTreeResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newGetSourceTreeRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseGetSourceTreeResponse(resp)
}

func (c *Client) newGetSourceTreeRequest(ctx context.Context, request GetSourceTreeRequest) (*http.Request, error) {
	path := "/api/v1/repos/{repo_id}/tree"
	path = strings.ReplaceAll(path, "{repo_id}", url.PathEscape(fmt.Sprint(request.RepoID)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	if request.Path != nil {
		query.Set("path", fmt.Sprint(*request.Path))
	}
	if request.Ref != nil {
		query.Set("ref", fmt.Sprint(*request.Ref))
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) GetSourceWorkflowRun(ctx context.Context, request GetSourceWorkflowRunRequest, reqEditors ...RequestEditorFn) (*GetSourceWorkflowRunResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newGetSourceWorkflowRunRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseGetSourceWorkflowRunResponse(resp)
}

func (c *Client) newGetSourceWorkflowRunRequest(ctx context.Context, request GetSourceWorkflowRunRequest) (*http.Request, error) {
	path := "/api/v1/workflow-runs/{workflow_run_id}"
	path = strings.ReplaceAll(path, "{workflow_run_id}", url.PathEscape(fmt.Sprint(request.WorkflowRunID)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) ListSourceRefs(ctx context.Context, request ListSourceRefsRequest, reqEditors ...RequestEditorFn) (*ListSourceRefsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newListSourceRefsRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseListSourceRefsResponse(resp)
}

func (c *Client) newListSourceRefsRequest(ctx context.Context, request ListSourceRefsRequest) (*http.Request, error) {
	path := "/api/v1/repos/{repo_id}/refs"
	path = strings.ReplaceAll(path, "{repo_id}", url.PathEscape(fmt.Sprint(request.RepoID)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) ListSourceRepositories(ctx context.Context, request ListSourceRepositoriesRequest, reqEditors ...RequestEditorFn) (*ListSourceRepositoriesResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newListSourceRepositoriesRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseListSourceRepositoriesResponse(resp)
}

func (c *Client) newListSourceRepositoriesRequest(ctx context.Context, request ListSourceRepositoriesRequest) (*http.Request, error) {
	path := "/api/v1/repos"
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	if request.ProjectID != nil {
		query.Set("project_id", fmt.Sprint(*request.ProjectID))
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) ListSourceWorkflowRuns(ctx context.Context, request ListSourceWorkflowRunsRequest, reqEditors ...RequestEditorFn) (*ListSourceWorkflowRunsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newListSourceWorkflowRunsRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return nil, err
			}
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return parseListSourceWorkflowRunsResponse(resp)
}

func (c *Client) newListSourceWorkflowRunsRequest(ctx context.Context, request ListSourceWorkflowRunsRequest) (*http.Request, error) {
	path := "/api/v1/repos/{repo_id}/workflow-runs"
	path = strings.ReplaceAll(path, "{repo_id}", url.PathEscape(fmt.Sprint(request.RepoID)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func parseCreateSourceCheckoutGrantResponse(resp *http.Response) (*CreateSourceCheckoutGrantResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &CreateSourceCheckoutGrantResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 201 {
		var decoded CheckoutGrantSummary
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseCreateSourceGitCredentialResponse(resp *http.Response) (*CreateSourceGitCredentialResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &CreateSourceGitCredentialResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 201 {
		var decoded GitCredentialSummary
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseCreateSourceRepositoryResponse(resp *http.Response) (*CreateSourceRepositoryResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &CreateSourceRepositoryResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 201 {
		var decoded RepositorySummary
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseCreateSourceWorkflowRunResponse(resp *http.Response) (*CreateSourceWorkflowRunResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &CreateSourceWorkflowRunResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 201 {
		var decoded WorkflowRunSummary
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseGetSourceBlobResponse(resp *http.Response) (*GetSourceBlobResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &GetSourceBlobResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded SourceBlobView
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseGetSourceRepositoryResponse(resp *http.Response) (*GetSourceRepositoryResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &GetSourceRepositoryResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded RepositorySummary
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseGetSourceTreeResponse(resp *http.Response) (*GetSourceTreeResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &GetSourceTreeResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded GetSourceTreeOutputBody
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseGetSourceWorkflowRunResponse(resp *http.Response) (*GetSourceWorkflowRunResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &GetSourceWorkflowRunResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded WorkflowRunSummary
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseListSourceRefsResponse(resp *http.Response) (*ListSourceRefsResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ListSourceRefsResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ListSourceRefsOutputBody
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseListSourceRepositoriesResponse(resp *http.Response) (*ListSourceRepositoriesResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ListSourceRepositoriesResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ListSourceRepositoriesOutputBody
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseListSourceWorkflowRunsResponse(resp *http.Response) (*ListSourceWorkflowRunsResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ListSourceWorkflowRunsResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ListSourceWorkflowRunsOutputBody
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return nil, err
			}
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

type ErrorModel struct {
	Schema      *string          `json:"$schema,omitempty"`
	Type        *string          `json:"type,omitempty"`
	Title       *string          `json:"title,omitempty"`
	Status      *int64           `json:"status,omitempty"`
	Detail      *string          `json:"detail,omitempty"`
	Instance    *string          `json:"instance,omitempty"`
	Code        *string          `json:"code,omitempty"`
	RequestID   *string          `json:"requestId,omitempty"`
	Traceparent *string          `json:"traceparent,omitempty"`
	Errors      []map[string]any `json:"errors,omitempty"`
}

func decodeProblem(body []byte) *ErrorModel {
	if len(body) == 0 {
		return nil
	}
	var problem ErrorModel
	if err := json.Unmarshal(body, &problem); err != nil {
		return nil
	}
	return &problem
}
