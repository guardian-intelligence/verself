// Code generated from the Smithy API contract. DO NOT EDIT.

package iamclient

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

const ServiceName = "iam-service"

type AuthorizationRelation = string

type AuthorizationResourceId = string

type AuthorizationResourceType = string

type DisplayName = string

type EmailAddress = string

type FamilyName = string

type GivenName = string

type OrgSlug = string

type OrganizationResourceName = string

type OrganizationVersion = int64

type PermissionName = string

type ProblemCode = string

type ProblemDetail = string

type ProblemType = string

type ProviderOrgId = string

type RequestId = string

type ResourcePermissionName = string

type SubjectId = string

type TraceParent = string

type ZedToken = string

type IAMAuthorizationSubjectType string

const (
	IAMAuthorizationSubjectTypeSERVICEACCOUNT IAMAuthorizationSubjectType = "service_account"
	IAMAuthorizationSubjectTypeUSER           IAMAuthorizationSubjectType = "user"
	IAMAuthorizationSubjectTypeWORKLOAD       IAMAuthorizationSubjectType = "workload"
)

type Permissions []PermissionName

type AuthorizeOperationResult struct {
	OrgID       ProviderOrgId           `json:"org_id"`
	Subject     IAMAuthorizationSubject `json:"subject"`
	Permissions Permissions             `json:"permissions"`
	ZedToken    *ZedToken               `json:"zed_token,omitempty"`
}

type AuthorizeResourceResult struct {
	OrgID               ProviderOrgId           `json:"org_id"`
	Subject             IAMAuthorizationSubject `json:"subject"`
	OperationPermission *PermissionName         `json:"operation_permission,omitempty"`
	Resource            IAMResourceRef          `json:"resource"`
	ResourcePermission  ResourcePermissionName  `json:"resource_permission"`
	Allowed             bool                    `json:"allowed"`
	ZedToken            *ZedToken               `json:"zed_token,omitempty"`
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

type HumanProfileSummary struct {
	SubjectID   SubjectId    `json:"subject_id"`
	Email       EmailAddress `json:"email"`
	GivenName   GivenName    `json:"given_name"`
	FamilyName  FamilyName   `json:"family_name"`
	DisplayName DisplayName  `json:"display_name"`
	SyncedAt    string       `json:"synced_at"`
}

type IAMAuthorizationSubject struct {
	Type IAMAuthorizationSubjectType `json:"type"`
	ID   SubjectId                   `json:"id"`
}

type IAMResourceRef struct {
	Type AuthorizationResourceType `json:"type"`
	ID   AuthorizationResourceId   `json:"id"`
}

type OrganizationProfile struct {
	OrgID          ProviderOrgId             `json:"org_id"`
	Slug           OrgSlug                   `json:"slug"`
	DisplayName    DisplayName               `json:"display_name"`
	State          string                    `json:"state"`
	Version        OrganizationVersion       `json:"version"`
	UpdatedAt      string                    `json:"updated_at"`
	RedirectedFrom *OrgSlug                  `json:"redirected_from,omitempty"`
	ResourceName   *OrganizationResourceName `json:"resourceName,omitempty"`
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

type ResolveOrganizationOutputBody struct {
	Organization OrganizationProfile `json:"organization"`
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

type WriteResourceParentEdgeResult struct {
	Resource  IAMResourceRef        `json:"resource"`
	Relation  AuthorizationRelation `json:"relation"`
	Parent    IAMResourceRef        `json:"parent"`
	ZedToken  *ZedToken             `json:"zed_token,omitempty"`
	Operation *PermissionName       `json:"operation,omitempty"`
}

type AuthorizeOperationInputBody struct {
	OrgID       ProviderOrgId           `json:"org_id"`
	Subject     IAMAuthorizationSubject `json:"subject"`
	Permissions Permissions             `json:"permissions"`
	MinZedToken *ZedToken               `json:"min_zed_token,omitempty"`
}

type AuthorizeOperationRequest struct {
	Body AuthorizeOperationInputBody `json:"body"`
}

type AuthorizeOperationResponse struct {
	StatusCode   int
	Body         []byte
	Result       *AuthorizeOperationResult
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type AuthorizeResourceInputBody struct {
	OrgID               ProviderOrgId           `json:"org_id"`
	Subject             IAMAuthorizationSubject `json:"subject"`
	OperationPermission *PermissionName         `json:"operation_permission,omitempty"`
	Resource            IAMResourceRef          `json:"resource"`
	ResourcePermission  ResourcePermissionName  `json:"resource_permission"`
	MinZedToken         *ZedToken               `json:"min_zed_token,omitempty"`
}

type AuthorizeResourceRequest struct {
	Body AuthorizeResourceInputBody `json:"body"`
}

type AuthorizeResourceResponse struct {
	StatusCode   int
	Body         []byte
	Result       *AuthorizeResourceResult
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ResolveOrganizationInputBody struct {
	OrgID         *ProviderOrgId `json:"org_id,omitempty"`
	RequireActive bool           `json:"require_active"`
	Slug          *OrgSlug       `json:"slug,omitempty"`
}

type ResolveOrganizationRequest struct {
	Body ResolveOrganizationInputBody `json:"body"`
}

type ResolveOrganizationResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ResolveOrganizationOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type UpdateHumanProfileInputBody struct {
	GivenName   GivenName    `json:"given_name"`
	FamilyName  FamilyName   `json:"family_name"`
	DisplayName *DisplayName `json:"display_name,omitempty"`
}

type UpdateHumanProfileRequest struct {
	SubjectID SubjectId
	Body      UpdateHumanProfileInputBody `json:"body"`
}

type UpdateHumanProfileResponse struct {
	StatusCode   int
	Body         []byte
	Result       *HumanProfileSummary
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type WriteResourceParentEdgeInputBody struct {
	OrgID     ProviderOrgId         `json:"org_id"`
	Resource  IAMResourceRef        `json:"resource"`
	Relation  AuthorizationRelation `json:"relation"`
	Parent    IAMResourceRef        `json:"parent"`
	Operation *PermissionName       `json:"operation,omitempty"`
}

type WriteResourceParentEdgeRequest struct {
	Body WriteResourceParentEdgeInputBody `json:"body"`
}

type WriteResourceParentEdgeResponse struct {
	StatusCode   int
	Body         []byte
	Result       *WriteResourceParentEdgeResult
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

func (c *Client) AuthorizeOperation(ctx context.Context, request AuthorizeOperationRequest, reqEditors ...RequestEditorFn) (*AuthorizeOperationResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newAuthorizeOperationRequest(ctx, request)
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
	return parseAuthorizeOperationResponse(resp)
}

func (c *Client) newAuthorizeOperationRequest(ctx context.Context, request AuthorizeOperationRequest) (*http.Request, error) {
	path := "/internal/v1/authorization/authorize"
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
	return req, nil
}

func (c *Client) AuthorizeResource(ctx context.Context, request AuthorizeResourceRequest, reqEditors ...RequestEditorFn) (*AuthorizeResourceResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newAuthorizeResourceRequest(ctx, request)
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
	return parseAuthorizeResourceResponse(resp)
}

func (c *Client) newAuthorizeResourceRequest(ctx context.Context, request AuthorizeResourceRequest) (*http.Request, error) {
	path := "/internal/v1/authorization/resources/authorize"
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
	return req, nil
}

func (c *Client) ResolveOrganization(ctx context.Context, request ResolveOrganizationRequest, reqEditors ...RequestEditorFn) (*ResolveOrganizationResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newResolveOrganizationRequest(ctx, request)
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
	return parseResolveOrganizationResponse(resp)
}

func (c *Client) newResolveOrganizationRequest(ctx context.Context, request ResolveOrganizationRequest) (*http.Request, error) {
	path := "/internal/v1/organizations/resolve"
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
	return req, nil
}

func (c *Client) UpdateHumanProfile(ctx context.Context, request UpdateHumanProfileRequest, reqEditors ...RequestEditorFn) (*UpdateHumanProfileResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newUpdateHumanProfileRequest(ctx, request)
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
	return parseUpdateHumanProfileResponse(resp)
}

func (c *Client) newUpdateHumanProfileRequest(ctx context.Context, request UpdateHumanProfileRequest) (*http.Request, error) {
	path := "/internal/v1/subjects/{subject_id}/human-profile"
	path = strings.ReplaceAll(path, "{subject_id}", url.PathEscape(fmt.Sprint(request.SubjectID)))
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
	return req, nil
}

func (c *Client) WriteResourceParentEdge(ctx context.Context, request WriteResourceParentEdgeRequest, reqEditors ...RequestEditorFn) (*WriteResourceParentEdgeResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newWriteResourceParentEdgeRequest(ctx, request)
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
	return parseWriteResourceParentEdgeResponse(resp)
}

func (c *Client) newWriteResourceParentEdgeRequest(ctx context.Context, request WriteResourceParentEdgeRequest) (*http.Request, error) {
	path := "/internal/v1/authorization/resources/parent-edge"
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
	return req, nil
}

func parseAuthorizeOperationResponse(resp *http.Response) (*AuthorizeOperationResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &AuthorizeOperationResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded AuthorizeOperationResult
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

func parseAuthorizeResourceResponse(resp *http.Response) (*AuthorizeResourceResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &AuthorizeResourceResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded AuthorizeResourceResult
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

func parseResolveOrganizationResponse(resp *http.Response) (*ResolveOrganizationResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ResolveOrganizationResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ResolveOrganizationOutputBody
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

func parseUpdateHumanProfileResponse(resp *http.Response) (*UpdateHumanProfileResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &UpdateHumanProfileResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded HumanProfileSummary
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

func parseWriteResourceParentEdgeResponse(resp *http.Response) (*WriteResourceParentEdgeResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &WriteResourceParentEdgeResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded WriteResourceParentEdgeResult
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
