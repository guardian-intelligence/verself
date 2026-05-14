package governance

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

type AuditActorId = string

type AuditActorType = string

type AuditCredentialId = string

type AuditCursor = string

type AuditErrorCode = string

type AuditEventId = string

type AuditEventOperationName = string

type AuditEventSource = string

type AuditEventsLimit = int64

type AuditHMACKeyId = string

type AuditTargetId = string

type AuditTargetType = string

type ContentDisposition = string

type DataExportArchive = []byte

type DataExportId = string

type DecimalUint64 = string

type ExportArtifactPath = string

type ExportDownloadURL = string

type ExportErrorCode = string

type ExportErrorMessage = string

type ExportFormat = string

type ExportScope = string

type ExportState = string

type GovernanceAuditEventName = string

type GovernanceOrgId = string

type GovernancePermissionName = string

type HMACHex = string

type IdempotencyKey = string

type MediaType = string

type ProblemCode = string

type ProblemDetail = string

type ProblemType = string

type RequestId = string

type ResourceName = string

type SHA256Hex = string

type TraceId = string

type TraceParent = string

type AuditListOrder string

const (
	AuditListOrderASC  AuditListOrder = "asc"
	AuditListOrderDESC AuditListOrder = "desc"
)

type AuditOutcome string

const (
	AuditOutcomeALLOWED AuditOutcome = "allowed"
	AuditOutcomeDENIED  AuditOutcome = "denied"
	AuditOutcomeERROR   AuditOutcome = "error"
)

type AuditEvents []AuditEvent

type DataExportFiles []DataExportFile

type DataExportJobs []DataExportJob

type ExportScopes []ExportScope

type AuditEvent struct {
	EventID            AuditEventId             `json:"event_id"`
	RecordedAt         string                   `json:"recorded_at"`
	OrgID              GovernanceOrgId          `json:"org_id"`
	Sequence           DecimalUint64            `json:"sequence"`
	EventSource        AuditEventSource         `json:"event_source"`
	EventName          AuditEventOperationName  `json:"event_name"`
	AuditEvent         GovernanceAuditEventName `json:"audit_event"`
	ActorType          AuditActorType           `json:"actor_type"`
	ActorID            AuditActorId             `json:"actor_id"`
	CredentialID       *AuditCredentialId       `json:"credential_id,omitempty"`
	TargetType         AuditTargetType          `json:"target_type"`
	TargetID           *AuditTargetId           `json:"target_id,omitempty"`
	TargetResourceName *ResourceName            `json:"targetResourceName,omitempty"`
	Permission         GovernancePermissionName `json:"permission"`
	Outcome            AuditOutcome             `json:"outcome"`
	ErrorCode          *AuditErrorCode          `json:"error_code,omitempty"`
	TraceID            *TraceId                 `json:"trace_id,omitempty"`
	DetailSHA256       SHA256Hex                `json:"detail_sha256"`
	PrevHMAC           HMACHex                  `json:"prev_hmac"`
	RowHMAC            HMACHex                  `json:"row_hmac"`
	HMACKeyID          *AuditHMACKeyId          `json:"hmac_key_id,omitempty"`
}

type AuditFilters struct {
	ActorID            *AuditActorId             `json:"actor_id,omitempty"`
	AuditEvent         *GovernanceAuditEventName `json:"audit_event,omitempty"`
	CredentialID       *AuditCredentialId        `json:"credential_id,omitempty"`
	EventName          *AuditEventOperationName  `json:"event_name,omitempty"`
	EventSource        *AuditEventSource         `json:"event_source,omitempty"`
	Outcome            *AuditOutcome             `json:"outcome,omitempty"`
	TargetID           *AuditTargetId            `json:"target_id,omitempty"`
	TargetType         *AuditTargetType          `json:"target_type,omitempty"`
	TargetResourceName *ResourceName             `json:"targetResourceName,omitempty"`
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

type DataExportFile struct {
	Path        ExportArtifactPath `json:"path"`
	ContentType MediaType          `json:"content_type"`
	Rows        DecimalUint64      `json:"rows"`
	Bytes       DecimalUint64      `json:"bytes"`
	SHA256      SHA256Hex          `json:"sha256"`
}

type DataExportJob struct {
	ExportID       DataExportId        `json:"export_id"`
	ResourceName   ResourceName        `json:"resourceName"`
	OrgID          GovernanceOrgId     `json:"org_id"`
	RequestedBy    AuditActorId        `json:"requested_by"`
	Scopes         ExportScopes        `json:"scopes"`
	IncludeLogs    bool                `json:"include_logs"`
	Format         ExportFormat        `json:"format"`
	State          ExportState         `json:"state"`
	ArtifactSHA256 *SHA256Hex          `json:"artifact_sha256,omitempty"`
	ArtifactBytes  DecimalUint64       `json:"artifact_bytes"`
	DownloadURL    *ExportDownloadURL  `json:"download_url,omitempty"`
	Files          DataExportFiles     `json:"files"`
	CreatedAt      string              `json:"created_at"`
	UpdatedAt      string              `json:"updated_at"`
	CompletedAt    *string             `json:"completed_at,omitempty"`
	ExpiresAt      string              `json:"expires_at"`
	ErrorCode      *ExportErrorCode    `json:"error_code,omitempty"`
	ErrorMessage   *ExportErrorMessage `json:"error_message,omitempty"`
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

type CreateDataExportInputBody struct {
	Scopes      *ExportScopes `json:"scopes,omitempty"`
	IncludeLogs *bool         `json:"include_logs,omitempty"`
}

type CreateDataExportRequest struct {
	IdempotencyKey IdempotencyKey
	Body           CreateDataExportInputBody `json:"body"`
}

type CreateDataExportOutputBody struct {
	Export DataExportJob `json:"export"`
}

type CreateDataExportResponse struct {
	StatusCode   int
	Body         []byte
	Result       *CreateDataExportOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type DownloadDataExportRequest struct {
	ExportID DataExportId
}

type DownloadDataExportOutput struct {
	ContentType        MediaType
	ContentDisposition ContentDisposition
	Body               DataExportArchive
}

type DownloadDataExportResponse struct {
	StatusCode   int
	Body         []byte
	Result       *DownloadDataExportOutput
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type GetDataExportRequest struct {
	ExportID DataExportId
}

type GetDataExportOutputBody struct {
	Export DataExportJob `json:"export"`
}

type GetDataExportResponse struct {
	StatusCode   int
	Body         []byte
	Result       *GetDataExportOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ListAuditEventsRequest struct {
	Limit              *AuditEventsLimit
	Cursor             *AuditCursor
	Order              *AuditListOrder
	ActorID            *AuditActorId
	AuditEvent         *GovernanceAuditEventName
	CredentialID       *AuditCredentialId
	EventName          *AuditEventOperationName
	EventSource        *AuditEventSource
	Outcome            *AuditOutcome
	TargetID           *AuditTargetId
	TargetType         *AuditTargetType
	TargetResourceName *ResourceName
}

type ListAuditEventsOutputBody struct {
	Events     AuditEvents      `json:"events"`
	NextCursor *AuditCursor     `json:"next_cursor,omitempty"`
	Limit      AuditEventsLimit `json:"limit"`
	Filters    AuditFilters     `json:"filters"`
}

type ListAuditEventsResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ListAuditEventsOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ListDataExportsRequest struct{}

type ListDataExportsOutputBody struct {
	Exports DataExportJobs `json:"exports"`
}

type ListDataExportsResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ListDataExportsOutputBody
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

func (c *Client) CreateDataExport(ctx context.Context, request CreateDataExportRequest, reqEditors ...RequestEditorFn) (*CreateDataExportResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newCreateDataExportRequest(ctx, request)
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
	return parseCreateDataExportResponse(resp)
}

func (c *Client) newCreateDataExportRequest(ctx context.Context, request CreateDataExportRequest) (*http.Request, error) {
	path := "/api/v1/governance/exports"
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

func (c *Client) DownloadDataExport(ctx context.Context, request DownloadDataExportRequest, reqEditors ...RequestEditorFn) (*DownloadDataExportResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newDownloadDataExportRequest(ctx, request)
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
	return parseDownloadDataExportResponse(resp)
}

func (c *Client) newDownloadDataExportRequest(ctx context.Context, request DownloadDataExportRequest) (*http.Request, error) {
	path := "/api/v1/governance/exports/{export_id}/download"
	path = strings.ReplaceAll(path, "{export_id}", url.PathEscape(fmt.Sprint(request.ExportID)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/gzip")
	return req, nil
}

func (c *Client) GetDataExport(ctx context.Context, request GetDataExportRequest, reqEditors ...RequestEditorFn) (*GetDataExportResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newGetDataExportRequest(ctx, request)
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
	return parseGetDataExportResponse(resp)
}

func (c *Client) newGetDataExportRequest(ctx context.Context, request GetDataExportRequest) (*http.Request, error) {
	path := "/api/v1/governance/exports/{export_id}"
	path = strings.ReplaceAll(path, "{export_id}", url.PathEscape(fmt.Sprint(request.ExportID)))
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

func (c *Client) ListAuditEvents(ctx context.Context, request ListAuditEventsRequest, reqEditors ...RequestEditorFn) (*ListAuditEventsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newListAuditEventsRequest(ctx, request)
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
	return parseListAuditEventsResponse(resp)
}

func (c *Client) newListAuditEventsRequest(ctx context.Context, request ListAuditEventsRequest) (*http.Request, error) {
	path := "/api/v1/governance/audit/events"
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	if request.ActorID != nil {
		query.Set("actor_id", fmt.Sprint(*request.ActorID))
	}
	if request.AuditEvent != nil {
		query.Set("audit_event", fmt.Sprint(*request.AuditEvent))
	}
	if request.CredentialID != nil {
		query.Set("credential_id", fmt.Sprint(*request.CredentialID))
	}
	if request.Cursor != nil {
		query.Set("cursor", fmt.Sprint(*request.Cursor))
	}
	if request.EventName != nil {
		query.Set("event_name", fmt.Sprint(*request.EventName))
	}
	if request.EventSource != nil {
		query.Set("event_source", fmt.Sprint(*request.EventSource))
	}
	if request.Limit != nil {
		query.Set("limit", fmt.Sprint(*request.Limit))
	}
	if request.Order != nil {
		query.Set("order", fmt.Sprint(*request.Order))
	}
	if request.Outcome != nil {
		query.Set("outcome", fmt.Sprint(*request.Outcome))
	}
	if request.TargetResourceName != nil {
		query.Set("targetResourceName", fmt.Sprint(*request.TargetResourceName))
	}
	if request.TargetID != nil {
		query.Set("target_id", fmt.Sprint(*request.TargetID))
	}
	if request.TargetType != nil {
		query.Set("target_type", fmt.Sprint(*request.TargetType))
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) ListDataExports(ctx context.Context, request ListDataExportsRequest, reqEditors ...RequestEditorFn) (*ListDataExportsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newListDataExportsRequest(ctx, request)
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
	return parseListDataExportsResponse(resp)
}

func (c *Client) newListDataExportsRequest(ctx context.Context, request ListDataExportsRequest) (*http.Request, error) {
	path := "/api/v1/governance/exports"
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

func parseCreateDataExportResponse(resp *http.Response) (*CreateDataExportResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &CreateDataExportResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 201 {
		var decoded CreateDataExportOutputBody
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

func parseDownloadDataExportResponse(resp *http.Response) (*DownloadDataExportResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &DownloadDataExportResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		decoded := DownloadDataExportOutput{
			ContentType:        MediaType(resp.Header.Get("Content-Type")),
			ContentDisposition: ContentDisposition(resp.Header.Get("Content-Disposition")),
			Body:               DataExportArchive(append([]byte(nil), body...)),
		}
		result.Result = &decoded
		return result, nil
	}
	result.Problem = decodeProblem(body)
	return result, nil
}

func parseGetDataExportResponse(resp *http.Response) (*GetDataExportResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &GetDataExportResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded GetDataExportOutputBody
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

func parseListAuditEventsResponse(resp *http.Response) (*ListAuditEventsResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ListAuditEventsResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ListAuditEventsOutputBody
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

func parseListDataExportsResponse(resp *http.Response) (*ListDataExportsResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ListDataExportsResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ListDataExportsOutputBody
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
