package governanceinternalclient

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

const ServiceName = "governance-service"

type (
	APIActivityAction        = string
	APIActivityOperation     = string
	APIActivityService       = string
	APIEventCode             = string
	ActorType                = string
	ActorUID                 = string
	CredentialUID            = string
	DecimalUint64            = string
	GovernanceOrgID          = string
	GovernancePermissionName = string
	HMACHex                  = string
	HTTPHost                 = string
	HTTPMethod               = string
	HTTPReferrer             = string
	HTTPResponseMessage      = string
	HTTPResponseStatus       = string
	HTTPSafeArgs             = string
	HTTPScheme               = string
	HTTPXForwardedFor        = string
	OCSFMetadataUID          = string
	ProblemCode              = string
	ProblemDetail            = string
	ProblemStatusCode        = string
	ProblemType              = string
	RequestID                = string
	ResourceName             = string
	ResourceType             = string
	ResourceUID              = string
	SpanID                   = string
	TraceID                  = string
	TraceParent              = string
)

type APIActivityStatus string

const (
	APIActivityStatusSuccess APIActivityStatus = "Success"
	APIActivityStatusFailure APIActivityStatus = "Failure"
	APIActivityStatusOther   APIActivityStatus = "Other"
)

type AuthorizationDecision string

const (
	AuthorizationDecisionAllowed AuthorizationDecision = "Allowed"
	AuthorizationDecisionDenied  AuthorizationDecision = "Denied"
)

type APIActivityAccepted struct {
	MetadataUID OCSFMetadataUID `json:"metadata_uid"`
	Sequence    DecimalUint64   `json:"sequence"`
	RowHMAC     HMACHex         `json:"row_hmac"`
}

type APIActivityHTTPRequest struct {
	Method          HTTPMethod         `json:"method"`
	Route           string             `json:"route"`
	RequestUID      *RequestID         `json:"request_uid,omitempty"`
	Args            *HTTPSafeArgs      `json:"args,omitempty"`
	UserAgent       *string            `json:"user_agent,omitempty"`
	SrcEndpointIP   *string            `json:"src_endpoint_ip,omitempty"`
	SrcEndpointName *string            `json:"src_endpoint_name,omitempty"`
	XForwardedFor   *HTTPXForwardedFor `json:"x_forwarded_for,omitempty"`
	Referrer        *HTTPReferrer      `json:"referrer,omitempty"`
	Host            *HTTPHost          `json:"host,omitempty"`
	Scheme          *HTTPScheme        `json:"scheme,omitempty"`
}

type APIActivityHTTPResponse struct {
	Code    uint16               `json:"code"`
	Message *HTTPResponseMessage `json:"message,omitempty"`
	Status  *HTTPResponseStatus  `json:"status,omitempty"`
}

type APIActivityResource struct {
	Type     ResourceType  `json:"type"`
	UID      *ResourceUID  `json:"uid,omitempty"`
	Name     *string       `json:"name,omitempty"`
	FullName *ResourceName `json:"full_name,omitempty"`
	Role     *string       `json:"role,omitempty"`
	RoleID   *uint8        `json:"role_id,omitempty"`
}

type APIActivityResources []APIActivityResource

type APIActivityRecord struct {
	OrgID                 GovernanceOrgID          `json:"org_id"`
	APIService            APIActivityService       `json:"api_service"`
	APIOperation          APIActivityOperation     `json:"api_operation"`
	APIEventCode          APIEventCode             `json:"api_event_code"`
	APIAction             APIActivityAction        `json:"api_action"`
	APIVersion            *string                  `json:"api_version,omitempty"`
	ActorType             ActorType                `json:"actor_type"`
	ActorUID              ActorUID                 `json:"actor_uid"`
	ActorName             *string                  `json:"actor_name,omitempty"`
	ActorEmail            *string                  `json:"actor_email,omitempty"`
	CredentialUID         *CredentialUID           `json:"credential_uid,omitempty"`
	Permission            GovernancePermissionName `json:"permission"`
	Resources             APIActivityResources     `json:"resources"`
	HTTPRequest           APIActivityHTTPRequest   `json:"http_request"`
	HTTPResponse          APIActivityHTTPResponse  `json:"http_response"`
	AuthorizationDecision AuthorizationDecision    `json:"authorization_decision"`
	Status                APIActivityStatus        `json:"status"`
	StatusCode            ProblemStatusCode        `json:"status_code"`
	StatusDetail          *string                  `json:"status_detail,omitempty"`
	TraceUID              *TraceID                 `json:"trace_uid,omitempty"`
	SpanUID               *SpanID                  `json:"span_uid,omitempty"`
	ObservedAt            *string                  `json:"observed_at,omitempty"`
	Unmapped              *map[string]any          `json:"unmapped,omitempty"`
}

type AppendAPIActivityRequest struct {
	Body APIActivityRecord `json:"body"`
}

type AppendAPIActivityOutputBody struct {
	Accepted APIActivityAccepted `json:"accepted"`
}

type AppendAPIActivityResponse struct {
	StatusCode   int
	Body         []byte
	Result       *AppendAPIActivityOutputBody
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

func (c *Client) AppendAPIActivity(ctx context.Context, request AppendAPIActivityRequest, reqEditors ...RequestEditorFn) (*AppendAPIActivityResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newAppendAPIActivityRequest(ctx, request)
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
	return parseAppendAPIActivityResponse(resp)
}

func (c *Client) newAppendAPIActivityRequest(ctx context.Context, request AppendAPIActivityRequest) (*http.Request, error) {
	endpoint, err := url.Parse(c.server + "/internal/v1/ocsf/api-activities")
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

func parseAppendAPIActivityResponse(resp *http.Response) (*AppendAPIActivityResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &AppendAPIActivityResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 202 {
		var decoded AppendAPIActivityOutputBody
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
