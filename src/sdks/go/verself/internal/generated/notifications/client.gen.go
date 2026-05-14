// Code generated from the Smithy API contract. DO NOT EDIT.

package notifications

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

const ServiceName = "notifications-service"

type ActionURL = string

type DecimalInt64 = string

type IdempotencyKey = string

type NotificationBody = string

type NotificationId = string

type NotificationKind = string

type NotificationPageSize = int64

type NotificationTitle = string

type OrgId = string

type PageToken = string

type PreferenceVersion = int64

type ProblemCode = string

type ProblemDetail = string

type ProblemType = string

type RequestId = string

type RequiredNotificationBody = string

type ResourceName = string

type SubjectId = string

type TraceParent = string

type UnreadCount = int64

type NotificationPriority string

const (
	NotificationPriorityHIGH   NotificationPriority = "high"
	NotificationPriorityLOW    NotificationPriority = "low"
	NotificationPriorityNORMAL NotificationPriority = "normal"
)

type NotificationsList []NotificationRecord

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

type NotificationAccepted struct {
	EventID     NotificationId `json:"event_id"`
	Traceparent *TraceParent   `json:"traceparent,omitempty"`
}

type NotificationPreferencesView struct {
	Enabled      bool              `json:"enabled"`
	WebEnabled   bool              `json:"web_enabled"`
	EmailEnabled bool              `json:"email_enabled"`
	PushEnabled  bool              `json:"push_enabled"`
	SmsEnabled   bool              `json:"sms_enabled"`
	Version      PreferenceVersion `json:"version"`
	UpdatedAt    string            `json:"updated_at"`
	UpdatedBy    *SubjectId        `json:"updated_by,omitempty"`
}

type NotificationRecord struct {
	NotificationID     NotificationId           `json:"notification_id"`
	ResourceName       ResourceName             `json:"resourceName"`
	OrgID              OrgId                    `json:"org_id"`
	RecipientSubjectID SubjectId                `json:"recipient_subject_id"`
	RecipientSequence  DecimalInt64             `json:"recipient_sequence"`
	Kind               NotificationKind         `json:"kind"`
	Priority           NotificationPriority     `json:"priority"`
	Title              NotificationTitle        `json:"title"`
	Body               RequiredNotificationBody `json:"body"`
	ActionURL          *ActionURL               `json:"action_url,omitempty"`
	TargetResourceName *ResourceName            `json:"targetResourceName,omitempty"`
	CreatedAt          string                   `json:"created_at"`
	ExpiresAt          *string                  `json:"expires_at,omitempty"`
	ReadAt             *string                  `json:"read_at,omitempty"`
	DismissedAt        *string                  `json:"dismissed_at,omitempty"`
}

type NotificationSummary struct {
	OrgID              OrgId                       `json:"org_id"`
	SubjectID          SubjectId                   `json:"subject_id"`
	UnreadCount        UnreadCount                 `json:"unread_count"`
	LatestSequence     DecimalInt64                `json:"latest_sequence"`
	ReadUpToSequence   DecimalInt64                `json:"read_up_to_sequence"`
	Preferences        NotificationPreferencesView `json:"preferences"`
	LatestNotification *NotificationRecord         `json:"latest_notification,omitempty"`
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

type AdvanceNotificationReadCursorInputBody struct {
	ReadUpToSequence DecimalInt64 `json:"read_up_to_sequence"`
}

type AdvanceNotificationReadCursorRequest struct {
	IdempotencyKey IdempotencyKey
	Body           AdvanceNotificationReadCursorInputBody `json:"body"`
}

type AdvanceNotificationReadCursorResponse struct {
	StatusCode   int
	Body         []byte
	Result       *NotificationSummary
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ClearNotificationsRequest struct {
	IdempotencyKey IdempotencyKey
}

type ClearNotificationsResponse struct {
	StatusCode   int
	Body         []byte
	Result       *NotificationSummary
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type DismissNotificationRequest struct {
	IdempotencyKey IdempotencyKey
	NotificationID NotificationId
}

type DismissNotificationResponse struct {
	StatusCode   int
	Body         []byte
	Result       *NotificationSummary
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type GetNotificationSummaryRequest struct {
}

type GetNotificationSummaryResponse struct {
	StatusCode   int
	Body         []byte
	Result       *NotificationSummary
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ListNotificationsRequest struct {
	Cursor *PageToken
	Limit  *NotificationPageSize
}

type ListNotificationsOutputBody struct {
	Summary       NotificationSummary `json:"summary"`
	Notifications NotificationsList   `json:"notifications"`
	NextCursor    *PageToken          `json:"next_cursor,omitempty"`
}

type ListNotificationsResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ListNotificationsOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type MarkNotificationReadRequest struct {
	IdempotencyKey IdempotencyKey
	NotificationID NotificationId
}

type MarkNotificationReadResponse struct {
	StatusCode   int
	Body         []byte
	Result       *NotificationSummary
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type PublishTestNotificationInputBody struct {
	ActionURL *ActionURL         `json:"action_url,omitempty"`
	Body      *NotificationBody  `json:"body,omitempty"`
	Title     *NotificationTitle `json:"title,omitempty"`
}

type PublishTestNotificationRequest struct {
	IdempotencyKey IdempotencyKey
	Body           PublishTestNotificationInputBody `json:"body"`
}

type PublishTestNotificationResponse struct {
	StatusCode   int
	Body         []byte
	Result       *NotificationAccepted
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type PutNotificationPreferencesInputBody struct {
	EmailEnabled *bool             `json:"email_enabled,omitempty"`
	Enabled      bool              `json:"enabled"`
	PushEnabled  *bool             `json:"push_enabled,omitempty"`
	SmsEnabled   *bool             `json:"sms_enabled,omitempty"`
	Version      PreferenceVersion `json:"version"`
	WebEnabled   *bool             `json:"web_enabled,omitempty"`
}

type PutNotificationPreferencesRequest struct {
	IdempotencyKey IdempotencyKey
	Body           PutNotificationPreferencesInputBody `json:"body"`
}

type PutNotificationPreferencesResponse struct {
	StatusCode   int
	Body         []byte
	Result       *NotificationSummary
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

func (c *Client) AdvanceNotificationReadCursor(ctx context.Context, request AdvanceNotificationReadCursorRequest, reqEditors ...RequestEditorFn) (*AdvanceNotificationReadCursorResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newAdvanceNotificationReadCursorRequest(ctx, request)
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
	return parseAdvanceNotificationReadCursorResponse(resp)
}

func (c *Client) newAdvanceNotificationReadCursorRequest(ctx context.Context, request AdvanceNotificationReadCursorRequest) (*http.Request, error) {
	path := "/api/v1/notifications/read-cursor"
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

func (c *Client) ClearNotifications(ctx context.Context, request ClearNotificationsRequest, reqEditors ...RequestEditorFn) (*ClearNotificationsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newClearNotificationsRequest(ctx, request)
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
	return parseClearNotificationsResponse(resp)
}

func (c *Client) newClearNotificationsRequest(ctx context.Context, request ClearNotificationsRequest) (*http.Request, error) {
	path := "/api/v1/notifications/clear"
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprint(request.IdempotencyKey))
	return req, nil
}

func (c *Client) DismissNotification(ctx context.Context, request DismissNotificationRequest, reqEditors ...RequestEditorFn) (*DismissNotificationResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newDismissNotificationRequest(ctx, request)
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
	return parseDismissNotificationResponse(resp)
}

func (c *Client) newDismissNotificationRequest(ctx context.Context, request DismissNotificationRequest) (*http.Request, error) {
	path := "/api/v1/notifications/{notification_id}/dismiss"
	path = strings.ReplaceAll(path, "{notification_id}", url.PathEscape(fmt.Sprint(request.NotificationID)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprint(request.IdempotencyKey))
	return req, nil
}

func (c *Client) GetNotificationSummary(ctx context.Context, request GetNotificationSummaryRequest, reqEditors ...RequestEditorFn) (*GetNotificationSummaryResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newGetNotificationSummaryRequest(ctx, request)
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
	return parseGetNotificationSummaryResponse(resp)
}

func (c *Client) newGetNotificationSummaryRequest(ctx context.Context, request GetNotificationSummaryRequest) (*http.Request, error) {
	path := "/api/v1/notifications/summary"
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

func (c *Client) ListNotifications(ctx context.Context, request ListNotificationsRequest, reqEditors ...RequestEditorFn) (*ListNotificationsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newListNotificationsRequest(ctx, request)
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
	return parseListNotificationsResponse(resp)
}

func (c *Client) newListNotificationsRequest(ctx context.Context, request ListNotificationsRequest) (*http.Request, error) {
	path := "/api/v1/notifications"
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
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) MarkNotificationRead(ctx context.Context, request MarkNotificationReadRequest, reqEditors ...RequestEditorFn) (*MarkNotificationReadResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newMarkNotificationReadRequest(ctx, request)
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
	return parseMarkNotificationReadResponse(resp)
}

func (c *Client) newMarkNotificationReadRequest(ctx context.Context, request MarkNotificationReadRequest) (*http.Request, error) {
	path := "/api/v1/notifications/{notification_id}/read"
	path = strings.ReplaceAll(path, "{notification_id}", url.PathEscape(fmt.Sprint(request.NotificationID)))
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprint(request.IdempotencyKey))
	return req, nil
}

func (c *Client) PublishTestNotification(ctx context.Context, request PublishTestNotificationRequest, reqEditors ...RequestEditorFn) (*PublishTestNotificationResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newPublishTestNotificationRequest(ctx, request)
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
	return parsePublishTestNotificationResponse(resp)
}

func (c *Client) newPublishTestNotificationRequest(ctx context.Context, request PublishTestNotificationRequest) (*http.Request, error) {
	path := "/api/v1/notifications/test"
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

func (c *Client) PutNotificationPreferences(ctx context.Context, request PutNotificationPreferencesRequest, reqEditors ...RequestEditorFn) (*PutNotificationPreferencesResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newPutNotificationPreferencesRequest(ctx, request)
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
	return parsePutNotificationPreferencesResponse(resp)
}

func (c *Client) newPutNotificationPreferencesRequest(ctx context.Context, request PutNotificationPreferencesRequest) (*http.Request, error) {
	path := "/api/v1/notifications/preferences"
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	requestBody, err := json.Marshal(request.Body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "PUT", endpoint.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", fmt.Sprint(request.IdempotencyKey))
	return req, nil
}

func parseAdvanceNotificationReadCursorResponse(resp *http.Response) (*AdvanceNotificationReadCursorResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &AdvanceNotificationReadCursorResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded NotificationSummary
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

func parseClearNotificationsResponse(resp *http.Response) (*ClearNotificationsResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ClearNotificationsResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded NotificationSummary
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

func parseDismissNotificationResponse(resp *http.Response) (*DismissNotificationResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &DismissNotificationResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded NotificationSummary
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

func parseGetNotificationSummaryResponse(resp *http.Response) (*GetNotificationSummaryResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &GetNotificationSummaryResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded NotificationSummary
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

func parseListNotificationsResponse(resp *http.Response) (*ListNotificationsResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ListNotificationsResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ListNotificationsOutputBody
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

func parseMarkNotificationReadResponse(resp *http.Response) (*MarkNotificationReadResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &MarkNotificationReadResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded NotificationSummary
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

func parsePublishTestNotificationResponse(resp *http.Response) (*PublishTestNotificationResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &PublishTestNotificationResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 202 {
		var decoded NotificationAccepted
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

func parsePutNotificationPreferencesResponse(resp *http.Response) (*PutNotificationPreferencesResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &PutNotificationPreferencesResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded NotificationSummary
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
