package governanceclient

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
	APIActivityCursor        = string
	APIActivityEventsLimit   = int64
	APIActivityOperation     = string
	APIActivityService       = string
	ActorType                = string
	ActorUID                 = string
	CredentialUID            = string
	DataExportArchive        = []byte
	DataExportID             = string
	DecimalUint64            = string
	ExportArtifactPath       = string
	ExportDownloadURL        = string
	ExportErrorCode          = string
	ExportErrorMessage       = string
	ExportFormat             = string
	ExportScope              = string
	ExportState              = string
	GovernanceOrgID          = string
	GovernancePermissionName = string
	HMACHex                  = string
	MediaType                = string
	OCSFClassName            = string
	OCSFMetadataUID          = string
	OCSFStatus               = string
	ResourceName             = string
	ResourceType             = string
	ResourceUID              = string
	SHA256Hex                = string
	SpanID                   = string
	TraceID                  = string
)

type APIActivityListOrder string

const (
	APIActivityListOrderASC  APIActivityListOrder = "asc"
	APIActivityListOrderDESC APIActivityListOrder = "desc"
)

type APIActivityEvent struct {
	MetadataUID             OCSFMetadataUID          `json:"metadata_uid"`
	Time                    string                   `json:"time"`
	OrgID                   GovernanceOrgID          `json:"org_id"`
	Sequence                DecimalUint64            `json:"sequence"`
	OCSFVersion             string                   `json:"ocsf_version"`
	CategoryUID             uint8                    `json:"category_uid"`
	CategoryName            string                   `json:"category_name"`
	ClassUID                uint16                   `json:"class_uid"`
	ClassName               OCSFClassName            `json:"class_name"`
	TypeUID                 uint32                   `json:"type_uid"`
	ActivityID              uint8                    `json:"activity_id"`
	ActivityName            string                   `json:"activity_name"`
	ActionID                uint8                    `json:"action_id"`
	Action                  string                   `json:"action"`
	StatusID                uint8                    `json:"status_id"`
	Status                  OCSFStatus               `json:"status"`
	StatusCode              string                   `json:"status_code"`
	SeverityID              uint8                    `json:"severity_id"`
	Severity                string                   `json:"severity"`
	APIService              APIActivityService       `json:"api_service"`
	APIOperation            APIActivityOperation     `json:"api_operation"`
	APIVersion              string                   `json:"api_version,omitempty"`
	ActorType               ActorType                `json:"actor_type"`
	ActorUID                ActorUID                 `json:"actor_uid"`
	ActorName               string                   `json:"actor_name,omitempty"`
	CredentialUID           *CredentialUID           `json:"credential_uid,omitempty"`
	PrimaryResourceType     ResourceType             `json:"primary_resource_type"`
	PrimaryResourceUID      *ResourceUID             `json:"primary_resource_uid,omitempty"`
	PrimaryResourceName     string                   `json:"primary_resource_name,omitempty"`
	PrimaryResourceFullName *ResourceName            `json:"primary_resource_full_name,omitempty"`
	Permission              GovernancePermissionName `json:"permission"`
	HTTPRequestUID          *string                  `json:"http_request_uid,omitempty"`
	HTTPMethod              string                   `json:"http_method"`
	HTTPRoute               string                   `json:"http_route"`
	HTTPArgs                string                   `json:"http_args,omitempty"`
	HTTPUserAgent           string                   `json:"http_user_agent,omitempty"`
	SrcEndpointIP           string                   `json:"src_endpoint_ip,omitempty"`
	SrcEndpointName         string                   `json:"src_endpoint_name,omitempty"`
	HTTPResponseCode        uint16                   `json:"http_response_code"`
	TraceUID                *TraceID                 `json:"trace_uid,omitempty"`
	SpanUID                 *SpanID                  `json:"span_uid,omitempty"`
	OCSFSHA256              SHA256Hex                `json:"ocsf_sha256"`
	PrevHMAC                HMACHex                  `json:"prev_hmac"`
	RowHMAC                 HMACHex                  `json:"row_hmac"`
	HMACKeyID               *string                  `json:"hmac_key_id,omitempty"`
}

type APIActivityFilters struct {
	ActorType     *ActorType            `json:"actor_type,omitempty"`
	ActorUID      *ActorUID             `json:"actor_uid,omitempty"`
	APIService    *APIActivityService   `json:"api_service,omitempty"`
	APIOperation  *APIActivityOperation `json:"api_operation,omitempty"`
	ActivityID    *uint8                `json:"activity_id,omitempty"`
	CredentialUID *CredentialUID        `json:"credential_uid,omitempty"`
	ResourceUID   *ResourceUID          `json:"resource_uid,omitempty"`
	ResourceType  *ResourceType         `json:"resource_type,omitempty"`
	StatusID      *uint8                `json:"status_id,omitempty"`
	StatusCode    *string               `json:"status_code,omitempty"`
	TraceUID      *TraceID              `json:"trace_uid,omitempty"`
}

type APIActivityEvents []APIActivityEvent

type DataExportFile struct {
	Path        ExportArtifactPath `json:"path"`
	ContentType MediaType          `json:"content_type"`
	Rows        DecimalUint64      `json:"rows"`
	Bytes       DecimalUint64      `json:"bytes"`
	SHA256      SHA256Hex          `json:"sha256"`
}

type (
	DataExportFiles []DataExportFile
	DataExportJobs  []DataExportJob
	ExportScopes    []ExportScope
)

type DataExportJob struct {
	ExportID       DataExportID        `json:"export_id"`
	ResourceName   ResourceName        `json:"resourceName"`
	OrgID          GovernanceOrgID     `json:"org_id"`
	RequestedBy    ActorUID            `json:"requested_by"`
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

type CreateDataExportInputBody struct {
	Scopes      *ExportScopes `json:"scopes,omitempty"`
	IncludeLogs *bool         `json:"include_logs,omitempty"`
}

type ListAPIActivitiesRequest struct {
	Limit         *APIActivityEventsLimit
	Cursor        *APIActivityCursor
	Order         *APIActivityListOrder
	ActorUID      *ActorUID
	ActorType     *ActorType
	APIService    *APIActivityService
	APIOperation  *APIActivityOperation
	ActivityID    *uint8
	CredentialUID *CredentialUID
	ResourceUID   *ResourceUID
	ResourceType  *ResourceType
	StatusID      *uint8
	StatusCode    *string
	TraceUID      *TraceID
}

type ListAPIActivitiesOutputBody struct {
	APIActivities APIActivityEvents      `json:"api_activities"`
	NextCursor    *APIActivityCursor     `json:"next_cursor,omitempty"`
	Limit         APIActivityEventsLimit `json:"limit"`
	Filters       APIActivityFilters     `json:"filters"`
}

type ListAPIActivitiesResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ListAPIActivitiesOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type (
	ListDataExportsRequest    struct{}
	ListDataExportsOutputBody struct {
		Exports DataExportJobs `json:"exports"`
	}
)

type ListDataExportsResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ListDataExportsOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type CreateDataExportRequest struct {
	IdempotencyKey string
	Body           CreateDataExportInputBody
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

type (
	GetDataExportRequest    struct{ ExportID DataExportID }
	GetDataExportOutputBody struct {
		Export DataExportJob `json:"export"`
	}
)

type GetDataExportResponse struct {
	StatusCode   int
	Body         []byte
	Result       *GetDataExportOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type (
	DownloadDataExportRequest  struct{ ExportID DataExportID }
	DownloadDataExportResponse struct {
		StatusCode         int
		Body               []byte
		ContentType        string
		ContentDisposition string
		Problem            *ErrorModel
		HTTPResponse       *http.Response
	}
)

type (
	RequestEditorFn func(ctx context.Context, req *http.Request) error
	HTTPRequestDoer interface {
		Do(req *http.Request) (*http.Response, error)
	}
)
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

func (c *Client) ListAPIActivities(ctx context.Context, request ListAPIActivitiesRequest, reqEditors ...RequestEditorFn) (*ListAPIActivitiesResponse, error) {
	req, err := c.newRequest(ctx, "GET", "/api/v1/governance/ocsf/api-activities", nil)
	if err != nil {
		return nil, err
	}
	query := req.URL.Query()
	setQuery(query, "limit", request.Limit)
	setQuery(query, "cursor", request.Cursor)
	setQuery(query, "order", request.Order)
	setQuery(query, "actor_uid", request.ActorUID)
	setQuery(query, "actor_type", request.ActorType)
	setQuery(query, "api_service", request.APIService)
	setQuery(query, "api_operation", request.APIOperation)
	setQuery(query, "activity_id", request.ActivityID)
	setQuery(query, "credential_uid", request.CredentialUID)
	setQuery(query, "resource_uid", request.ResourceUID)
	setQuery(query, "resource_type", request.ResourceType)
	setQuery(query, "status_id", request.StatusID)
	setQuery(query, "status_code", request.StatusCode)
	setQuery(query, "trace_uid", request.TraceUID)
	req.URL.RawQuery = query.Encode()
	if err := c.edit(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	result := &ListAPIActivitiesResponse{}
	if err := parseJSONResponse(resp, 200, &result.StatusCode, &result.Body, &result.HTTPResponse, &result.Result, &result.Problem); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) ListDataExports(ctx context.Context, request ListDataExportsRequest, reqEditors ...RequestEditorFn) (*ListDataExportsResponse, error) {
	req, err := c.newRequest(ctx, "GET", "/api/v1/governance/exports", nil)
	if err != nil {
		return nil, err
	}
	if err := c.edit(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	result := &ListDataExportsResponse{}
	if err := parseJSONResponse(resp, 200, &result.StatusCode, &result.Body, &result.HTTPResponse, &result.Result, &result.Problem); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) CreateDataExport(ctx context.Context, request CreateDataExportRequest, reqEditors ...RequestEditorFn) (*CreateDataExportResponse, error) {
	req, err := c.newRequest(ctx, "POST", "/api/v1/governance/exports", request.Body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Idempotency-Key", request.IdempotencyKey)
	if err := c.edit(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	result := &CreateDataExportResponse{}
	if err := parseJSONResponse(resp, 201, &result.StatusCode, &result.Body, &result.HTTPResponse, &result.Result, &result.Problem); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) GetDataExport(ctx context.Context, request GetDataExportRequest, reqEditors ...RequestEditorFn) (*GetDataExportResponse, error) {
	req, err := c.newRequest(ctx, "GET", "/api/v1/governance/exports/"+url.PathEscape(string(request.ExportID)), nil)
	if err != nil {
		return nil, err
	}
	if err := c.edit(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	result := &GetDataExportResponse{}
	if err := parseJSONResponse(resp, 200, &result.StatusCode, &result.Body, &result.HTTPResponse, &result.Result, &result.Problem); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) DownloadDataExport(ctx context.Context, request DownloadDataExportRequest, reqEditors ...RequestEditorFn) (*DownloadDataExportResponse, error) {
	req, err := c.newRequest(ctx, "GET", "/api/v1/governance/exports/"+url.PathEscape(string(request.ExportID))+"/download", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/gzip")
	if err := c.edit(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &DownloadDataExportResponse{StatusCode: resp.StatusCode, Body: body, ContentType: resp.Header.Get("Content-Type"), ContentDisposition: resp.Header.Get("Content-Disposition"), HTTPResponse: resp}
	if resp.StatusCode != 200 {
		result.Problem = decodeProblem(body)
	}
	return result, nil
}

func (c *Client) newRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
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

func (c *Client) edit(ctx context.Context, req *http.Request, reqEditors []RequestEditorFn) error {
	for _, editor := range c.requestEditors {
		if err := editor(ctx, req); err != nil {
			return err
		}
	}
	for _, editor := range reqEditors {
		if editor != nil {
			if err := editor(ctx, req); err != nil {
				return err
			}
		}
	}
	return nil
}

func setQuery[T any](query url.Values, key string, value *T) {
	if value != nil {
		query.Set(key, fmt.Sprint(*value))
	}
}

func parseJSONResponse[T any](resp *http.Response, success int, statusCode *int, responseBody *[]byte, httpResponse **http.Response, result **T, problem **ErrorModel) error {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	*statusCode = resp.StatusCode
	*responseBody = body
	*httpResponse = resp
	if resp.StatusCode != success {
		*problem = decodeProblem(body)
		return nil
	}
	var decoded T
	if len(body) > 0 {
		if err := json.Unmarshal(body, &decoded); err != nil {
			return err
		}
	}
	*result = &decoded
	return nil
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
