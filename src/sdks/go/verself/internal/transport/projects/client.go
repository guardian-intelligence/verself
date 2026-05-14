package projects

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

const ServiceName = "projects-service"

type ActorId = string

type DecimalInt64 = string

type DisplayName = string

type EnvironmentId = string

type IdempotencyKey = string

type OrgId = string

type PageSize = int64

type PageToken = string

type ProblemCode = string

type ProblemDetail = string

type ProblemType = string

type ProjectDescription = string

type ProjectId = string

type ProjectSlug = string

type RequestId = string

type ResourceName = string

type TraceParent = string

type ProjectEnvironmentKind string

const (
	ProjectEnvironmentKindCUSTOM      ProjectEnvironmentKind = "custom"
	ProjectEnvironmentKindDEVELOPMENT ProjectEnvironmentKind = "development"
	ProjectEnvironmentKindPREVIEW     ProjectEnvironmentKind = "preview"
	ProjectEnvironmentKindPRODUCTION  ProjectEnvironmentKind = "production"
)

type ProjectEnvironmentState string

const (
	ProjectEnvironmentStateACTIVE   ProjectEnvironmentState = "active"
	ProjectEnvironmentStateARCHIVED ProjectEnvironmentState = "archived"
)

type ProjectState string

const (
	ProjectStateACTIVE   ProjectState = "active"
	ProjectStateARCHIVED ProjectState = "archived"
)

type ProjectEnvironments []ProjectEnvironmentSummary

type ProjectsList []ProjectSummary

type ProjectProtectionPolicy map[string]string

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

type ProjectEnvironmentSummary struct {
	EnvironmentID       EnvironmentId            `json:"environment_id"`
	ResourceName        ResourceName             `json:"resourceName"`
	ProjectID           ProjectId                `json:"project_id"`
	ProjectResourceName ResourceName             `json:"projectResourceName"`
	OrgID               OrgId                    `json:"org_id"`
	Slug                ProjectSlug              `json:"slug"`
	DisplayName         DisplayName              `json:"display_name"`
	Kind                ProjectEnvironmentKind   `json:"kind"`
	State               ProjectEnvironmentState  `json:"state"`
	Version             DecimalInt64             `json:"version"`
	ProtectionPolicy    *ProjectProtectionPolicy `json:"protection_policy,omitempty"`
	CreatedBy           ActorId                  `json:"created_by"`
	UpdatedBy           ActorId                  `json:"updated_by"`
	CreatedAt           string                   `json:"created_at"`
	UpdatedAt           string                   `json:"updated_at"`
	ArchivedAt          *string                  `json:"archived_at,omitempty"`
}

type ProjectSummary struct {
	ProjectID          ProjectId           `json:"project_id"`
	ResourceName       ResourceName        `json:"resourceName"`
	OrgID              OrgId               `json:"org_id"`
	Slug               ProjectSlug         `json:"slug"`
	RedirectedFromSlug *ProjectSlug        `json:"redirected_from_slug,omitempty"`
	DisplayName        DisplayName         `json:"display_name"`
	Description        *ProjectDescription `json:"description,omitempty"`
	State              ProjectState        `json:"state"`
	Version            DecimalInt64        `json:"version"`
	CreatedBy          ActorId             `json:"created_by"`
	UpdatedBy          ActorId             `json:"updated_by"`
	CreatedAt          string              `json:"created_at"`
	UpdatedAt          string              `json:"updated_at"`
	ArchivedAt         *string             `json:"archived_at,omitempty"`
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

type ProjectLifecycleInputBody struct {
	Version DecimalInt64 `json:"version"`
}

type ArchiveProjectRequest struct {
	ProjectID      ProjectId
	IdempotencyKey IdempotencyKey
	Body           ProjectLifecycleInputBody `json:"body"`
}

type ArchiveProjectResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ProjectSummary
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ProjectEnvironmentLifecycleInputBody struct {
	Version DecimalInt64 `json:"version"`
}

type ArchiveProjectEnvironmentRequest struct {
	ProjectID      ProjectId
	EnvironmentID  EnvironmentId
	IdempotencyKey IdempotencyKey
	Body           ProjectEnvironmentLifecycleInputBody `json:"body"`
}

type ArchiveProjectEnvironmentResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ProjectEnvironmentSummary
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type CreateProjectInputBody struct {
	Slug        *ProjectSlug        `json:"slug,omitempty"`
	DisplayName DisplayName         `json:"display_name"`
	Description *ProjectDescription `json:"description,omitempty"`
}

type CreateProjectRequest struct {
	IdempotencyKey IdempotencyKey
	Body           CreateProjectInputBody `json:"body"`
}

type CreateProjectResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ProjectSummary
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type CreateProjectEnvironmentInputBody struct {
	Slug             ProjectSlug              `json:"slug"`
	DisplayName      DisplayName              `json:"display_name"`
	Kind             ProjectEnvironmentKind   `json:"kind"`
	ProtectionPolicy *ProjectProtectionPolicy `json:"protection_policy,omitempty"`
}

type CreateProjectEnvironmentRequest struct {
	ProjectID      ProjectId
	IdempotencyKey IdempotencyKey
	Body           CreateProjectEnvironmentInputBody `json:"body"`
}

type CreateProjectEnvironmentResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ProjectEnvironmentSummary
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type GetProjectRequest struct {
	ProjectID ProjectId
}

type GetProjectResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ProjectSummary
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ListProjectEnvironmentsRequest struct {
	ProjectID ProjectId
}

type ListProjectEnvironmentsOutputBody struct {
	Environments ProjectEnvironments `json:"environments"`
}

type ListProjectEnvironmentsResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ListProjectEnvironmentsOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ListProjectsRequest struct {
	State  *ProjectState
	Limit  *PageSize
	Cursor *PageToken
}

type ListProjectsOutputBody struct {
	Projects   ProjectsList `json:"projects"`
	NextCursor *PageToken   `json:"next_cursor,omitempty"`
}

type ListProjectsResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ListProjectsOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type PatchProjectInputBody struct {
	Version     DecimalInt64        `json:"version"`
	Slug        *ProjectSlug        `json:"slug,omitempty"`
	DisplayName *DisplayName        `json:"display_name,omitempty"`
	Description *ProjectDescription `json:"description,omitempty"`
}

type PatchProjectRequest struct {
	ProjectID      ProjectId
	IdempotencyKey IdempotencyKey
	Body           PatchProjectInputBody `json:"body"`
}

type PatchProjectResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ProjectSummary
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type PatchProjectEnvironmentInputBody struct {
	Version          DecimalInt64             `json:"version"`
	DisplayName      *DisplayName             `json:"display_name,omitempty"`
	ProtectionPolicy *ProjectProtectionPolicy `json:"protection_policy,omitempty"`
}

type PatchProjectEnvironmentRequest struct {
	ProjectID      ProjectId
	EnvironmentID  EnvironmentId
	IdempotencyKey IdempotencyKey
	Body           PatchProjectEnvironmentInputBody `json:"body"`
}

type PatchProjectEnvironmentResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ProjectEnvironmentSummary
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type RestoreProjectRequest struct {
	ProjectID      ProjectId
	IdempotencyKey IdempotencyKey
	Body           ProjectLifecycleInputBody `json:"body"`
}

type RestoreProjectResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ProjectSummary
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

func (c *Client) ArchiveProject(ctx context.Context, request ArchiveProjectRequest, reqEditors ...RequestEditorFn) (*ArchiveProjectResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newArchiveProjectRequest(ctx, request)
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
	return parseArchiveProjectResponse(resp)
}

func (c *Client) newArchiveProjectRequest(ctx context.Context, request ArchiveProjectRequest) (*http.Request, error) {
	path := "/api/v1/projects/{project_id}/archive"
	path = strings.ReplaceAll(path, "{project_id}", url.PathEscape(fmt.Sprint(request.ProjectID)))
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

func (c *Client) ArchiveProjectEnvironment(ctx context.Context, request ArchiveProjectEnvironmentRequest, reqEditors ...RequestEditorFn) (*ArchiveProjectEnvironmentResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newArchiveProjectEnvironmentRequest(ctx, request)
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
	return parseArchiveProjectEnvironmentResponse(resp)
}

func (c *Client) newArchiveProjectEnvironmentRequest(ctx context.Context, request ArchiveProjectEnvironmentRequest) (*http.Request, error) {
	path := "/api/v1/projects/{project_id}/environments/{environment_id}/archive"
	path = strings.ReplaceAll(path, "{environment_id}", url.PathEscape(fmt.Sprint(request.EnvironmentID)))
	path = strings.ReplaceAll(path, "{project_id}", url.PathEscape(fmt.Sprint(request.ProjectID)))
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

func (c *Client) CreateProject(ctx context.Context, request CreateProjectRequest, reqEditors ...RequestEditorFn) (*CreateProjectResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newCreateProjectRequest(ctx, request)
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
	return parseCreateProjectResponse(resp)
}

func (c *Client) newCreateProjectRequest(ctx context.Context, request CreateProjectRequest) (*http.Request, error) {
	path := "/api/v1/projects"
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

func (c *Client) CreateProjectEnvironment(ctx context.Context, request CreateProjectEnvironmentRequest, reqEditors ...RequestEditorFn) (*CreateProjectEnvironmentResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newCreateProjectEnvironmentRequest(ctx, request)
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
	return parseCreateProjectEnvironmentResponse(resp)
}

func (c *Client) newCreateProjectEnvironmentRequest(ctx context.Context, request CreateProjectEnvironmentRequest) (*http.Request, error) {
	path := "/api/v1/projects/{project_id}/environments"
	path = strings.ReplaceAll(path, "{project_id}", url.PathEscape(fmt.Sprint(request.ProjectID)))
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

func (c *Client) GetProject(ctx context.Context, request GetProjectRequest, reqEditors ...RequestEditorFn) (*GetProjectResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newGetProjectRequest(ctx, request)
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
	return parseGetProjectResponse(resp)
}

func (c *Client) newGetProjectRequest(ctx context.Context, request GetProjectRequest) (*http.Request, error) {
	path := "/api/v1/projects/{project_id}"
	path = strings.ReplaceAll(path, "{project_id}", url.PathEscape(fmt.Sprint(request.ProjectID)))
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

func (c *Client) ListProjectEnvironments(ctx context.Context, request ListProjectEnvironmentsRequest, reqEditors ...RequestEditorFn) (*ListProjectEnvironmentsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newListProjectEnvironmentsRequest(ctx, request)
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
	return parseListProjectEnvironmentsResponse(resp)
}

func (c *Client) newListProjectEnvironmentsRequest(ctx context.Context, request ListProjectEnvironmentsRequest) (*http.Request, error) {
	path := "/api/v1/projects/{project_id}/environments"
	path = strings.ReplaceAll(path, "{project_id}", url.PathEscape(fmt.Sprint(request.ProjectID)))
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

func (c *Client) ListProjects(ctx context.Context, request ListProjectsRequest, reqEditors ...RequestEditorFn) (*ListProjectsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newListProjectsRequest(ctx, request)
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
	return parseListProjectsResponse(resp)
}

func (c *Client) newListProjectsRequest(ctx context.Context, request ListProjectsRequest) (*http.Request, error) {
	path := "/api/v1/projects"
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	if request.Cursor != nil {
		query.Set("cursor", fmt.Sprint(*request.Cursor))
	}
	if request.Limit != nil {
		query.Set("limit", fmt.Sprint(*request.Limit))
	}
	if request.State != nil {
		query.Set("state", fmt.Sprint(*request.State))
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) PatchProject(ctx context.Context, request PatchProjectRequest, reqEditors ...RequestEditorFn) (*PatchProjectResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newPatchProjectRequest(ctx, request)
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
	return parsePatchProjectResponse(resp)
}

func (c *Client) newPatchProjectRequest(ctx context.Context, request PatchProjectRequest) (*http.Request, error) {
	path := "/api/v1/projects/{project_id}"
	path = strings.ReplaceAll(path, "{project_id}", url.PathEscape(fmt.Sprint(request.ProjectID)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	requestBody, err := json.Marshal(request.Body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "PATCH", endpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprint(request.IdempotencyKey))
	return req, nil
}

func (c *Client) PatchProjectEnvironment(ctx context.Context, request PatchProjectEnvironmentRequest, reqEditors ...RequestEditorFn) (*PatchProjectEnvironmentResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newPatchProjectEnvironmentRequest(ctx, request)
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
	return parsePatchProjectEnvironmentResponse(resp)
}

func (c *Client) newPatchProjectEnvironmentRequest(ctx context.Context, request PatchProjectEnvironmentRequest) (*http.Request, error) {
	path := "/api/v1/projects/{project_id}/environments/{environment_id}"
	path = strings.ReplaceAll(path, "{environment_id}", url.PathEscape(fmt.Sprint(request.EnvironmentID)))
	path = strings.ReplaceAll(path, "{project_id}", url.PathEscape(fmt.Sprint(request.ProjectID)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	requestBody, err := json.Marshal(request.Body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "PATCH", endpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprint(request.IdempotencyKey))
	return req, nil
}

func (c *Client) RestoreProject(ctx context.Context, request RestoreProjectRequest, reqEditors ...RequestEditorFn) (*RestoreProjectResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newRestoreProjectRequest(ctx, request)
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
	return parseRestoreProjectResponse(resp)
}

func (c *Client) newRestoreProjectRequest(ctx context.Context, request RestoreProjectRequest) (*http.Request, error) {
	path := "/api/v1/projects/{project_id}/restore"
	path = strings.ReplaceAll(path, "{project_id}", url.PathEscape(fmt.Sprint(request.ProjectID)))
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

func parseArchiveProjectResponse(resp *http.Response) (*ArchiveProjectResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ArchiveProjectResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ProjectSummary
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

func parseArchiveProjectEnvironmentResponse(resp *http.Response) (*ArchiveProjectEnvironmentResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ArchiveProjectEnvironmentResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ProjectEnvironmentSummary
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

func parseCreateProjectResponse(resp *http.Response) (*CreateProjectResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &CreateProjectResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 201 {
		var decoded ProjectSummary
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

func parseCreateProjectEnvironmentResponse(resp *http.Response) (*CreateProjectEnvironmentResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &CreateProjectEnvironmentResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 201 {
		var decoded ProjectEnvironmentSummary
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

func parseGetProjectResponse(resp *http.Response) (*GetProjectResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &GetProjectResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ProjectSummary
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

func parseListProjectEnvironmentsResponse(resp *http.Response) (*ListProjectEnvironmentsResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ListProjectEnvironmentsResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ListProjectEnvironmentsOutputBody
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

func parseListProjectsResponse(resp *http.Response) (*ListProjectsResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ListProjectsResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ListProjectsOutputBody
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

func parsePatchProjectResponse(resp *http.Response) (*PatchProjectResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &PatchProjectResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ProjectSummary
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

func parsePatchProjectEnvironmentResponse(resp *http.Response) (*PatchProjectEnvironmentResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &PatchProjectEnvironmentResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ProjectEnvironmentSummary
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

func parseRestoreProjectResponse(resp *http.Response) (*RestoreProjectResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &RestoreProjectResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ProjectSummary
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
