package iam

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

type DisplayName = string

type EmailAddress = string

type IdempotencyKey = string

type MemberId = string

type MemberResourceName = string

type OrgAclVersion = int64

type OrgId = string

type OrgSlug = string

type OrganizationResourceName = string

type OrganizationVersion = int64

type PageSize = int64

type PageToken = string

type ProblemCode = string

type ProblemDetail = string

type ProblemType = string

type RequestId = string

type TraceParent = string

type OrganizationRole string

const (
	OrganizationRoleADMIN  OrganizationRole = "admin"
	OrganizationRoleMEMBER OrganizationRole = "member"
	OrganizationRoleOWNER  OrganizationRole = "owner"
)

type Members []MemberSummary

type Organizations []OrganizationSummary

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

type ListMembersOutputBody struct {
	Members       Members    `json:"members"`
	NextPageToken *PageToken `json:"nextPageToken,omitempty"`
}

type ListOrganizationsOutputBody struct {
	Organizations Organizations `json:"organizations"`
	NextPageToken *PageToken    `json:"nextPageToken,omitempty"`
}

type MemberSummary struct {
	OrgID        OrgId              `json:"orgId"`
	MemberID     MemberId           `json:"memberId"`
	ResourceName MemberResourceName `json:"resourceName"`
	Email        EmailAddress       `json:"email"`
	DisplayName  DisplayName        `json:"displayName"`
	Role         OrganizationRole   `json:"role"`
}

type OrganizationSummary struct {
	OrgID         OrgId                    `json:"orgId"`
	ResourceName  OrganizationResourceName `json:"resourceName"`
	Slug          *OrgSlug                 `json:"slug,omitempty"`
	DisplayName   DisplayName              `json:"displayName"`
	CallerRole    OrganizationRole         `json:"callerRole"`
	Version       OrganizationVersion      `json:"version"`
	OrgACLVersion OrgAclVersion            `json:"orgAclVersion"`
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

type GetMemberRequest struct {
	OrgID    OrgId
	MemberID MemberId
}

type GetMemberResponse struct {
	StatusCode   int
	Body         []byte
	Result       *MemberSummary
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type GetOrganizationRequest struct {
	OrgID OrgId
}

type GetOrganizationResponse struct {
	StatusCode   int
	Body         []byte
	Result       *OrganizationSummary
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ListMembersRequest struct {
	OrgID     OrgId
	PageSize  *PageSize
	PageToken *PageToken
}

type ListMembersResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ListMembersOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ListOrganizationsRequest struct {
	PageSize  *PageSize
	PageToken *PageToken
}

type ListOrganizationsResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ListOrganizationsOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type UpdateMemberRoleInputBody struct {
	Role                  OrganizationRole `json:"role"`
	ExpectedRole          OrganizationRole `json:"expectedRole"`
	ExpectedOrgACLVersion OrgAclVersion    `json:"expectedOrgAclVersion"`
}

type UpdateMemberRoleRequest struct {
	OrgID          OrgId
	MemberID       MemberId
	IdempotencyKey IdempotencyKey
	Body           UpdateMemberRoleInputBody `json:"body"`
}

type UpdateMemberRoleResponse struct {
	StatusCode   int
	Body         []byte
	Result       *MemberSummary
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type UpdateOrganizationInputBody struct {
	Slug        *OrgSlug            `json:"slug,omitempty"`
	DisplayName *DisplayName        `json:"displayName,omitempty"`
	Version     OrganizationVersion `json:"version"`
}

type UpdateOrganizationRequest struct {
	OrgID          OrgId
	IdempotencyKey IdempotencyKey
	Body           UpdateOrganizationInputBody `json:"body"`
}

type UpdateOrganizationResponse struct {
	StatusCode   int
	Body         []byte
	Result       *OrganizationSummary
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

func (c *Client) GetMember(ctx context.Context, request GetMemberRequest, reqEditors ...RequestEditorFn) (*GetMemberResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newGetMemberRequest(ctx, request)
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
	return parseGetMemberResponse(resp)
}

func (c *Client) newGetMemberRequest(ctx context.Context, request GetMemberRequest) (*http.Request, error) {
	path := "/api/v1/orgs/{orgId}/members/{memberId}"
	path = strings.ReplaceAll(path, "{memberId}", url.PathEscape(fmt.Sprint(request.MemberID)))
	path = strings.ReplaceAll(path, "{orgId}", url.PathEscape(fmt.Sprint(request.OrgID)))
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

func (c *Client) GetOrganization(ctx context.Context, request GetOrganizationRequest, reqEditors ...RequestEditorFn) (*GetOrganizationResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newGetOrganizationRequest(ctx, request)
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
	return parseGetOrganizationResponse(resp)
}

func (c *Client) newGetOrganizationRequest(ctx context.Context, request GetOrganizationRequest) (*http.Request, error) {
	path := "/api/v1/orgs/{orgId}"
	path = strings.ReplaceAll(path, "{orgId}", url.PathEscape(fmt.Sprint(request.OrgID)))
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

func (c *Client) ListMembers(ctx context.Context, request ListMembersRequest, reqEditors ...RequestEditorFn) (*ListMembersResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newListMembersRequest(ctx, request)
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
	return parseListMembersResponse(resp)
}

func (c *Client) newListMembersRequest(ctx context.Context, request ListMembersRequest) (*http.Request, error) {
	path := "/api/v1/orgs/{orgId}/members"
	path = strings.ReplaceAll(path, "{orgId}", url.PathEscape(fmt.Sprint(request.OrgID)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	if request.PageSize != nil {
		query.Set("page_size", fmt.Sprint(*request.PageSize))
	}
	if request.PageToken != nil {
		query.Set("page_token", fmt.Sprint(*request.PageToken))
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) ListOrganizations(ctx context.Context, request ListOrganizationsRequest, reqEditors ...RequestEditorFn) (*ListOrganizationsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newListOrganizationsRequest(ctx, request)
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
	return parseListOrganizationsResponse(resp)
}

func (c *Client) newListOrganizationsRequest(ctx context.Context, request ListOrganizationsRequest) (*http.Request, error) {
	path := "/api/v1/orgs"
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	if request.PageSize != nil {
		query.Set("page_size", fmt.Sprint(*request.PageSize))
	}
	if request.PageToken != nil {
		query.Set("page_token", fmt.Sprint(*request.PageToken))
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) UpdateMemberRole(ctx context.Context, request UpdateMemberRoleRequest, reqEditors ...RequestEditorFn) (*UpdateMemberRoleResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newUpdateMemberRoleRequest(ctx, request)
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
	return parseUpdateMemberRoleResponse(resp)
}

func (c *Client) newUpdateMemberRoleRequest(ctx context.Context, request UpdateMemberRoleRequest) (*http.Request, error) {
	path := "/api/v1/orgs/{orgId}/members/{memberId}/role"
	path = strings.ReplaceAll(path, "{memberId}", url.PathEscape(fmt.Sprint(request.MemberID)))
	path = strings.ReplaceAll(path, "{orgId}", url.PathEscape(fmt.Sprint(request.OrgID)))
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

func (c *Client) UpdateOrganization(ctx context.Context, request UpdateOrganizationRequest, reqEditors ...RequestEditorFn) (*UpdateOrganizationResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newUpdateOrganizationRequest(ctx, request)
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
	return parseUpdateOrganizationResponse(resp)
}

func (c *Client) newUpdateOrganizationRequest(ctx context.Context, request UpdateOrganizationRequest) (*http.Request, error) {
	path := "/api/v1/orgs/{orgId}"
	path = strings.ReplaceAll(path, "{orgId}", url.PathEscape(fmt.Sprint(request.OrgID)))
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

func parseGetMemberResponse(resp *http.Response) (*GetMemberResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &GetMemberResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded MemberSummary
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

func parseGetOrganizationResponse(resp *http.Response) (*GetOrganizationResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &GetOrganizationResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded OrganizationSummary
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

func parseListMembersResponse(resp *http.Response) (*ListMembersResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ListMembersResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ListMembersOutputBody
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

func parseListOrganizationsResponse(resp *http.Response) (*ListOrganizationsResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ListOrganizationsResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ListOrganizationsOutputBody
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

func parseUpdateMemberRoleResponse(resp *http.Response) (*UpdateMemberRoleResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &UpdateMemberRoleResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded MemberSummary
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

func parseUpdateOrganizationResponse(resp *http.Response) (*UpdateOrganizationResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &UpdateOrganizationResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded OrganizationSummary
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
