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

type (
	DisplayName              = string
	EmailAddress             = string
	IAMMemberName            = string
	IAMRoleName              = string
	IdempotencyKey           = string
	MemberId                 = string
	MemberResourceName       = string
	OrgId                    = string
	OrgSlug                  = string
	OrganizationResourceName = string
	OrganizationVersion      = int64
	PageSize                 = int64
	PageToken                = string
	PermissionName           = string
	PolicyEtag               = string
	PolicyVersion            = int64
	ProblemCode              = string
	ProblemDetail            = string
	ProblemType              = string
	RequestId                = string
	TraceParent              = string
)

type (
	IAMMembers        []IAMMemberName
	IAMPolicyBindings []IAMPolicyBinding
	Members           []MemberSummary
	Organizations     []OrganizationSummary
	Permissions       []PermissionName
)

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
}

type OrganizationSummary struct {
	OrgID        OrgId                    `json:"orgId"`
	ResourceName OrganizationResourceName `json:"resourceName"`
	Slug         *OrgSlug                 `json:"slug,omitempty"`
	DisplayName  DisplayName              `json:"displayName"`
	Version      OrganizationVersion      `json:"version"`
}

type IAMPolicy struct {
	Version  PolicyVersion     `json:"version"`
	Bindings IAMPolicyBindings `json:"bindings"`
	Etag     *PolicyEtag       `json:"etag,omitempty"`
}

type IAMPolicyBinding struct {
	Role    IAMRoleName `json:"role"`
	Members IAMMembers  `json:"members"`
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

type GetIamPolicyRequest struct {
	OrgID OrgId
}

type GetIamPolicyResponse struct {
	StatusCode   int
	Body         []byte
	Result       *IAMPolicy
	Problem      *ErrorModel
	HTTPResponse *http.Response
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

type SetIamPolicyInputBody struct {
	Policy IAMPolicy `json:"policy"`
}

type SetIamPolicyRequest struct {
	OrgID          OrgId
	IdempotencyKey IdempotencyKey
	Body           SetIamPolicyInputBody `json:"body"`
}

type SetIamPolicyResponse struct {
	StatusCode   int
	Body         []byte
	Result       *IAMPolicy
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type TestIamPermissionsInputBody struct {
	Permissions Permissions `json:"permissions"`
}

type TestIamPermissionsRequest struct {
	OrgID OrgId
	Body  TestIamPermissionsInputBody `json:"body"`
}

type TestIamPermissionsOutputBody struct {
	Permissions Permissions `json:"permissions"`
}

type TestIamPermissionsResponse struct {
	StatusCode   int
	Body         []byte
	Result       *TestIamPermissionsOutputBody
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

func (c *Client) GetIamPolicy(ctx context.Context, request GetIamPolicyRequest, reqEditors ...RequestEditorFn) (*GetIamPolicyResponse, error) {
	req, err := c.newGetIamPolicyRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req, reqEditors...)
	if err != nil {
		return nil, err
	}
	status, body, result, problem, err := decodeResponse[IAMPolicy](resp, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &GetIamPolicyResponse{StatusCode: status, Body: body, Result: result, Problem: problem, HTTPResponse: resp}, nil
}

func (c *Client) newGetIamPolicyRequest(ctx context.Context, request GetIamPolicyRequest) (*http.Request, error) {
	path := "/api/v1/orgs/{orgId}/iamPolicy:get"
	path = strings.ReplaceAll(path, "{orgId}", url.PathEscape(fmt.Sprint(request.OrgID)))
	return c.newRequest(ctx, http.MethodPost, path, nil)
}

func (c *Client) GetMember(ctx context.Context, request GetMemberRequest, reqEditors ...RequestEditorFn) (*GetMemberResponse, error) {
	req, err := c.newGetMemberRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req, reqEditors...)
	if err != nil {
		return nil, err
	}
	status, body, result, problem, err := decodeResponse[MemberSummary](resp, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &GetMemberResponse{StatusCode: status, Body: body, Result: result, Problem: problem, HTTPResponse: resp}, nil
}

func (c *Client) newGetMemberRequest(ctx context.Context, request GetMemberRequest) (*http.Request, error) {
	path := "/api/v1/orgs/{orgId}/members/{memberId}"
	path = strings.ReplaceAll(path, "{memberId}", url.PathEscape(fmt.Sprint(request.MemberID)))
	path = strings.ReplaceAll(path, "{orgId}", url.PathEscape(fmt.Sprint(request.OrgID)))
	return c.newRequest(ctx, http.MethodGet, path, nil)
}

func (c *Client) GetOrganization(ctx context.Context, request GetOrganizationRequest, reqEditors ...RequestEditorFn) (*GetOrganizationResponse, error) {
	req, err := c.newGetOrganizationRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req, reqEditors...)
	if err != nil {
		return nil, err
	}
	status, body, result, problem, err := decodeResponse[OrganizationSummary](resp, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &GetOrganizationResponse{StatusCode: status, Body: body, Result: result, Problem: problem, HTTPResponse: resp}, nil
}

func (c *Client) newGetOrganizationRequest(ctx context.Context, request GetOrganizationRequest) (*http.Request, error) {
	path := "/api/v1/orgs/{orgId}"
	path = strings.ReplaceAll(path, "{orgId}", url.PathEscape(fmt.Sprint(request.OrgID)))
	return c.newRequest(ctx, http.MethodGet, path, nil)
}

func (c *Client) ListMembers(ctx context.Context, request ListMembersRequest, reqEditors ...RequestEditorFn) (*ListMembersResponse, error) {
	req, err := c.newListMembersRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req, reqEditors...)
	if err != nil {
		return nil, err
	}
	status, body, result, problem, err := decodeResponse[ListMembersOutputBody](resp, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &ListMembersResponse{StatusCode: status, Body: body, Result: result, Problem: problem, HTTPResponse: resp}, nil
}

func (c *Client) newListMembersRequest(ctx context.Context, request ListMembersRequest) (*http.Request, error) {
	path := "/api/v1/orgs/{orgId}/members"
	path = strings.ReplaceAll(path, "{orgId}", url.PathEscape(fmt.Sprint(request.OrgID)))
	endpoint, err := c.endpoint(path)
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
	return c.newRequestFromURL(ctx, http.MethodGet, endpoint, nil)
}

func (c *Client) ListOrganizations(ctx context.Context, request ListOrganizationsRequest, reqEditors ...RequestEditorFn) (*ListOrganizationsResponse, error) {
	req, err := c.newListOrganizationsRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req, reqEditors...)
	if err != nil {
		return nil, err
	}
	status, body, result, problem, err := decodeResponse[ListOrganizationsOutputBody](resp, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &ListOrganizationsResponse{StatusCode: status, Body: body, Result: result, Problem: problem, HTTPResponse: resp}, nil
}

func (c *Client) newListOrganizationsRequest(ctx context.Context, request ListOrganizationsRequest) (*http.Request, error) {
	endpoint, err := c.endpoint("/api/v1/orgs")
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
	return c.newRequestFromURL(ctx, http.MethodGet, endpoint, nil)
}

func (c *Client) SetIamPolicy(ctx context.Context, request SetIamPolicyRequest, reqEditors ...RequestEditorFn) (*SetIamPolicyResponse, error) {
	req, err := c.newSetIamPolicyRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req, reqEditors...)
	if err != nil {
		return nil, err
	}
	status, body, result, problem, err := decodeResponse[IAMPolicy](resp, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &SetIamPolicyResponse{StatusCode: status, Body: body, Result: result, Problem: problem, HTTPResponse: resp}, nil
}

func (c *Client) newSetIamPolicyRequest(ctx context.Context, request SetIamPolicyRequest) (*http.Request, error) {
	path := "/api/v1/orgs/{orgId}/iamPolicy:set"
	path = strings.ReplaceAll(path, "{orgId}", url.PathEscape(fmt.Sprint(request.OrgID)))
	req, err := c.newRequest(ctx, http.MethodPost, path, request.Body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Idempotency-Key", fmt.Sprint(request.IdempotencyKey))
	return req, nil
}

func (c *Client) TestIamPermissions(ctx context.Context, request TestIamPermissionsRequest, reqEditors ...RequestEditorFn) (*TestIamPermissionsResponse, error) {
	req, err := c.newTestIamPermissionsRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req, reqEditors...)
	if err != nil {
		return nil, err
	}
	status, body, result, problem, err := decodeResponse[TestIamPermissionsOutputBody](resp, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &TestIamPermissionsResponse{StatusCode: status, Body: body, Result: result, Problem: problem, HTTPResponse: resp}, nil
}

func (c *Client) newTestIamPermissionsRequest(ctx context.Context, request TestIamPermissionsRequest) (*http.Request, error) {
	path := "/api/v1/orgs/{orgId}/iamPolicy:testPermissions"
	path = strings.ReplaceAll(path, "{orgId}", url.PathEscape(fmt.Sprint(request.OrgID)))
	return c.newRequest(ctx, http.MethodPost, path, request.Body)
}

func (c *Client) UpdateOrganization(ctx context.Context, request UpdateOrganizationRequest, reqEditors ...RequestEditorFn) (*UpdateOrganizationResponse, error) {
	req, err := c.newUpdateOrganizationRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req, reqEditors...)
	if err != nil {
		return nil, err
	}
	status, body, result, problem, err := decodeResponse[OrganizationSummary](resp, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &UpdateOrganizationResponse{StatusCode: status, Body: body, Result: result, Problem: problem, HTTPResponse: resp}, nil
}

func (c *Client) newUpdateOrganizationRequest(ctx context.Context, request UpdateOrganizationRequest) (*http.Request, error) {
	path := "/api/v1/orgs/{orgId}"
	path = strings.ReplaceAll(path, "{orgId}", url.PathEscape(fmt.Sprint(request.OrgID)))
	req, err := c.newRequest(ctx, http.MethodPatch, path, request.Body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Idempotency-Key", fmt.Sprint(request.IdempotencyKey))
	return req, nil
}

func (c *Client) endpoint(path string) (*url.URL, error) {
	return url.Parse(c.server + path)
}

func (c *Client) newRequest(ctx context.Context, method string, path string, body any) (*http.Request, error) {
	endpoint, err := c.endpoint(path)
	if err != nil {
		return nil, err
	}
	return c.newRequestFromURL(ctx, method, endpoint, body)
}

func (c *Client) newRequestFromURL(ctx context.Context, method string, endpoint *url.URL, body any) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		requestBody, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(requestBody)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *Client) do(req *http.Request, reqEditors ...RequestEditorFn) (*http.Response, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	for _, editor := range c.requestEditors {
		if err := editor(req.Context(), req); err != nil {
			return nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(req.Context(), req); err != nil {
				return nil, err
			}
		}
	}
	return c.client.Do(req)
}

func decodeResponse[T any](resp *http.Response, okStatus int) (int, []byte, *T, *ErrorModel, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, nil, nil, err
	}
	if resp.StatusCode == okStatus {
		var decoded T
		if len(body) > 0 {
			if err := json.Unmarshal(body, &decoded); err != nil {
				return resp.StatusCode, body, nil, nil, err
			}
		}
		return resp.StatusCode, body, &decoded, nil, nil
	}
	return resp.StatusCode, body, nil, decodeProblem(body), nil
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
