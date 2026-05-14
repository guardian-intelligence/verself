// Code generated from the Smithy API contract. DO NOT EDIT.

package profileinternalclient

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

const ServiceName = "profile-service"

type ArtifactPath = string

type DataRightsActionDescription = string

type DataRightsActionName = string

type DataRightsCategory = string

type DataRightsReason = string

type DataRightsRequestId = string

type DataRightsRequestType = string

type DataRightsStatus = string

type DecimalUint64 = string

type MediaType = string

type OrgId = string

type ProblemCode = string

type ProblemDetail = string

type ProblemType = string

type RequestId = string

type SHA256Hex = string

type SubjectId = string

type TraceParent = string

type ProfileArtifacts []ProfileArtifact

type ProfileErasureActions []ProfileDataRightsErasureAction

type ProfileRetainedCategories []ProfileDataRightsRetainedCategory

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

type ProfileArtifact struct {
	Path        ArtifactPath  `json:"path"`
	ContentType MediaType     `json:"content_type"`
	Rows        DecimalUint64 `json:"rows"`
	Bytes       DecimalUint64 `json:"bytes"`
	SHA256      SHA256Hex     `json:"sha256"`
}

type ProfileDataRightsErasureAction struct {
	Name        DataRightsActionName         `json:"name"`
	Rows        DecimalUint64                `json:"rows"`
	Description *DataRightsActionDescription `json:"description,omitempty"`
}

type ProfileDataRightsManifest struct {
	RequestID          DataRightsRequestId       `json:"request_id"`
	RequestType        DataRightsRequestType     `json:"request_type"`
	Status             DataRightsStatus          `json:"status"`
	OrgID              *OrgId                    `json:"org_id,omitempty"`
	SubjectID          *SubjectId                `json:"subject_id,omitempty"`
	Artifacts          ProfileArtifacts          `json:"artifacts"`
	ErasureActions     ProfileErasureActions     `json:"erasure_actions"`
	RetainedCategories ProfileRetainedCategories `json:"retained_categories"`
	RecordCounts       map[string]any            `json:"record_counts"`
	CompletedAt        string                    `json:"completed_at"`
}

type ProfileDataRightsRetainedCategory struct {
	Category DataRightsCategory `json:"category"`
	Reason   DataRightsReason   `json:"reason"`
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

type ProfileDataRightsStatusRequest struct {
	RequestID DataRightsRequestId
}

type ProfileDataRightsOutputBody struct {
	Manifest ProfileDataRightsManifest `json:"manifest"`
}

type ProfileDataRightsStatusResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ProfileDataRightsOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ProfileDataRightsInputBody struct {
	OrgID       *OrgId              `json:"org_id,omitempty"`
	RequestID   DataRightsRequestId `json:"request_id"`
	RequestedAt string              `json:"requested_at"`
	RequestedBy SubjectId           `json:"requested_by"`
	SubjectID   *SubjectId          `json:"subject_id,omitempty"`
	Traceparent *TraceParent        `json:"traceparent,omitempty"`
}

type ProfileOrgExportRequest struct {
	Body ProfileDataRightsInputBody `json:"body"`
}

type ProfileOrgExportResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ProfileDataRightsOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ProfileSubjectErasureRequest struct {
	Body ProfileDataRightsInputBody `json:"body"`
}

type ProfileSubjectErasureResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ProfileDataRightsOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ProfileSubjectExportRequest struct {
	Body ProfileDataRightsInputBody `json:"body"`
}

type ProfileSubjectExportResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ProfileDataRightsOutputBody
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

func (c *Client) ProfileDataRightsStatus(ctx context.Context, request ProfileDataRightsStatusRequest, reqEditors ...RequestEditorFn) (*ProfileDataRightsStatusResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newProfileDataRightsStatusRequest(ctx, request)
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
	return parseProfileDataRightsStatusResponse(resp)
}

func (c *Client) newProfileDataRightsStatusRequest(ctx context.Context, request ProfileDataRightsStatusRequest) (*http.Request, error) {
	path := "/internal/v1/data-rights/requests/{request_id}"
	path = strings.ReplaceAll(path, "{request_id}", url.PathEscape(fmt.Sprint(request.RequestID)))
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

func (c *Client) ProfileOrgExport(ctx context.Context, request ProfileOrgExportRequest, reqEditors ...RequestEditorFn) (*ProfileOrgExportResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newProfileOrgExportRequest(ctx, request)
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
	return parseProfileOrgExportResponse(resp)
}

func (c *Client) newProfileOrgExportRequest(ctx context.Context, request ProfileOrgExportRequest) (*http.Request, error) {
	path := "/internal/v1/data-rights/org-export"
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

func (c *Client) ProfileSubjectErasure(ctx context.Context, request ProfileSubjectErasureRequest, reqEditors ...RequestEditorFn) (*ProfileSubjectErasureResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newProfileSubjectErasureRequest(ctx, request)
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
	return parseProfileSubjectErasureResponse(resp)
}

func (c *Client) newProfileSubjectErasureRequest(ctx context.Context, request ProfileSubjectErasureRequest) (*http.Request, error) {
	path := "/internal/v1/data-rights/subject-erasure"
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

func (c *Client) ProfileSubjectExport(ctx context.Context, request ProfileSubjectExportRequest, reqEditors ...RequestEditorFn) (*ProfileSubjectExportResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newProfileSubjectExportRequest(ctx, request)
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
	return parseProfileSubjectExportResponse(resp)
}

func (c *Client) newProfileSubjectExportRequest(ctx context.Context, request ProfileSubjectExportRequest) (*http.Request, error) {
	path := "/internal/v1/data-rights/subject-export"
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

func parseProfileDataRightsStatusResponse(resp *http.Response) (*ProfileDataRightsStatusResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ProfileDataRightsStatusResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ProfileDataRightsOutputBody
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

func parseProfileOrgExportResponse(resp *http.Response) (*ProfileOrgExportResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ProfileOrgExportResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ProfileDataRightsOutputBody
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

func parseProfileSubjectErasureResponse(resp *http.Response) (*ProfileSubjectErasureResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ProfileSubjectErasureResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ProfileDataRightsOutputBody
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

func parseProfileSubjectExportResponse(resp *http.Response) (*ProfileSubjectExportResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ProfileSubjectExportResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ProfileDataRightsOutputBody
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
