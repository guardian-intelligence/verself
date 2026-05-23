package emailclient

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

const ServiceName = "email-service"

type AccountId = string

type EmailAddress = string

type EmailId = string

type IdempotencyKey = string

type MailBodyHTML = string

type MailBodyText = string

type MailboxDisplayName = string

type MailboxId = string

type EmailServiceURL = string

type EmailProviderName = string

type MailboxStatusError = string

type ProblemCode = string

type ProblemDetail = string

type ProblemType = string

type RequestId = string

type TraceParent = string

type MailboxSyncAccounts map[string]MailboxSyncAccountStatusView

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

type MailBodyResult struct {
	AccountID AccountId    `json:"account_id"`
	EmailID   EmailId      `json:"email_id"`
	TextBody  MailBodyText `json:"text_body"`
	HtmlBody  MailBodyHTML `json:"html_body"`
	FetchedAt string       `json:"fetched_at"`
}

type MailMutationResult struct {
	EmailID EmailId `json:"email_id"`
	Status  string  `json:"status"`
}

type MailboxAccountView struct {
	AccountID        AccountId          `json:"account_id"`
	EmailAddress     EmailAddress       `json:"email_address"`
	DisplayName      MailboxDisplayName `json:"display_name"`
	DefaultMailboxID *MailboxId         `json:"default_mailbox_id,omitempty"`
}

type MailboxSyncAccountStatusView struct {
	AccountID       AccountId           `json:"account_id"`
	Running         bool                `json:"running"`
	Connected       bool                `json:"connected"`
	LastSyncAt      *string             `json:"last_sync_at,omitempty"`
	LastEventAt     *string             `json:"last_event_at,omitempty"`
	LastConnectedAt *string             `json:"last_connected_at,omitempty"`
	LastError       *MailboxStatusError `json:"last_error,omitempty"`
}

type MailboxSyncStatusView struct {
	StartedAt       string                      `json:"started_at"`
	StalwartBaseURL EmailServiceURL             `json:"stalwart_base_url"`
	PublicBaseURL   EmailServiceURL             `json:"public_base_url"`
	MailboxSync     MailboxSyncWorkerStatusView `json:"mailbox_sync"`
	SenderProvider  EmailProviderName           `json:"sender_provider"`
}

type MailboxSyncWorkerStatusView struct {
	Running         bool                `json:"running"`
	LastDiscoveryAt *string             `json:"last_discovery_at,omitempty"`
	LastError       *MailboxStatusError `json:"last_error,omitempty"`
	Accounts        MailboxSyncAccounts `json:"accounts"`
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

type MailAccountRequest struct{}

type MailAccountResponse struct {
	StatusCode   int
	Body         []byte
	Result       *MailboxAccountView
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type MailBodyRequest struct {
	EmailID EmailId
}

type MailBodyResponse struct {
	StatusCode   int
	Body         []byte
	Result       *MailBodyResult
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type MailFlagRequest struct {
	EmailID        EmailId
	IdempotencyKey IdempotencyKey
}

type MailFlagResponse struct {
	StatusCode   int
	Body         []byte
	Result       *MailMutationResult
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type MailMarkReadRequest struct {
	EmailID        EmailId
	IdempotencyKey IdempotencyKey
}

type MailMarkReadResponse struct {
	StatusCode   int
	Body         []byte
	Result       *MailMutationResult
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type MailMarkUnreadRequest struct {
	EmailID        EmailId
	IdempotencyKey IdempotencyKey
}

type MailMarkUnreadResponse struct {
	StatusCode   int
	Body         []byte
	Result       *MailMutationResult
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type MailMoveInputBody struct {
	MailboxID MailboxId `json:"mailbox_id"`
}

type MailMoveRequest struct {
	EmailID        EmailId
	IdempotencyKey IdempotencyKey
	Body           MailMoveInputBody `json:"body"`
}

type MailMoveResponse struct {
	StatusCode   int
	Body         []byte
	Result       *MailMutationResult
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type MailSyncStatusRequest struct{}

type MailSyncStatusOutputBody struct {
	Status MailboxSyncStatusView `json:"status"`
}

type MailSyncStatusResponse struct {
	StatusCode   int
	Body         []byte
	Result       *MailSyncStatusOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type MailTrashRequest struct {
	EmailID        EmailId
	IdempotencyKey IdempotencyKey
}

type MailTrashResponse struct {
	StatusCode   int
	Body         []byte
	Result       *MailMutationResult
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type MailUnflagRequest struct {
	EmailID        EmailId
	IdempotencyKey IdempotencyKey
}

type MailUnflagResponse struct {
	StatusCode   int
	Body         []byte
	Result       *MailMutationResult
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

func (c *Client) MailAccount(ctx context.Context, request MailAccountRequest, reqEditors ...RequestEditorFn) (*MailAccountResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newMailAccountRequest(ctx, request)
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
	return parseMailAccountResponse(resp)
}

func (c *Client) newMailAccountRequest(ctx context.Context, request MailAccountRequest) (*http.Request, error) {
	path := "/api/v1/email/account"
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

func (c *Client) MailBody(ctx context.Context, request MailBodyRequest, reqEditors ...RequestEditorFn) (*MailBodyResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newMailBodyRequest(ctx, request)
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
	return parseMailBodyResponse(resp)
}

func (c *Client) newMailBodyRequest(ctx context.Context, request MailBodyRequest) (*http.Request, error) {
	path := "/api/v1/email/emails/{email_id}/body"
	path = strings.ReplaceAll(path, "{email_id}", url.PathEscape(fmt.Sprint(request.EmailID)))
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

func (c *Client) MailFlag(ctx context.Context, request MailFlagRequest, reqEditors ...RequestEditorFn) (*MailFlagResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newMailFlagRequest(ctx, request)
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
	return parseMailFlagResponse(resp)
}

func (c *Client) newMailFlagRequest(ctx context.Context, request MailFlagRequest) (*http.Request, error) {
	path := "/api/v1/email/emails/{email_id}/flag"
	path = strings.ReplaceAll(path, "{email_id}", url.PathEscape(fmt.Sprint(request.EmailID)))
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

func (c *Client) MailMarkRead(ctx context.Context, request MailMarkReadRequest, reqEditors ...RequestEditorFn) (*MailMarkReadResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newMailMarkReadRequest(ctx, request)
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
	return parseMailMarkReadResponse(resp)
}

func (c *Client) newMailMarkReadRequest(ctx context.Context, request MailMarkReadRequest) (*http.Request, error) {
	path := "/api/v1/email/emails/{email_id}/read"
	path = strings.ReplaceAll(path, "{email_id}", url.PathEscape(fmt.Sprint(request.EmailID)))
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

func (c *Client) MailMarkUnread(ctx context.Context, request MailMarkUnreadRequest, reqEditors ...RequestEditorFn) (*MailMarkUnreadResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newMailMarkUnreadRequest(ctx, request)
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
	return parseMailMarkUnreadResponse(resp)
}

func (c *Client) newMailMarkUnreadRequest(ctx context.Context, request MailMarkUnreadRequest) (*http.Request, error) {
	path := "/api/v1/email/emails/{email_id}/unread"
	path = strings.ReplaceAll(path, "{email_id}", url.PathEscape(fmt.Sprint(request.EmailID)))
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

func (c *Client) MailMove(ctx context.Context, request MailMoveRequest, reqEditors ...RequestEditorFn) (*MailMoveResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newMailMoveRequest(ctx, request)
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
	return parseMailMoveResponse(resp)
}

func (c *Client) newMailMoveRequest(ctx context.Context, request MailMoveRequest) (*http.Request, error) {
	path := "/api/v1/email/emails/{email_id}/move"
	path = strings.ReplaceAll(path, "{email_id}", url.PathEscape(fmt.Sprint(request.EmailID)))
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

func (c *Client) MailSyncStatus(ctx context.Context, request MailSyncStatusRequest, reqEditors ...RequestEditorFn) (*MailSyncStatusResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newMailSyncStatusRequest(ctx, request)
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
	return parseMailSyncStatusResponse(resp)
}

func (c *Client) newMailSyncStatusRequest(ctx context.Context, request MailSyncStatusRequest) (*http.Request, error) {
	path := "/api/v1/email/sync/status"
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

func (c *Client) MailTrash(ctx context.Context, request MailTrashRequest, reqEditors ...RequestEditorFn) (*MailTrashResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newMailTrashRequest(ctx, request)
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
	return parseMailTrashResponse(resp)
}

func (c *Client) newMailTrashRequest(ctx context.Context, request MailTrashRequest) (*http.Request, error) {
	path := "/api/v1/email/emails/{email_id}/trash"
	path = strings.ReplaceAll(path, "{email_id}", url.PathEscape(fmt.Sprint(request.EmailID)))
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

func (c *Client) MailUnflag(ctx context.Context, request MailUnflagRequest, reqEditors ...RequestEditorFn) (*MailUnflagResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newMailUnflagRequest(ctx, request)
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
	return parseMailUnflagResponse(resp)
}

func (c *Client) newMailUnflagRequest(ctx context.Context, request MailUnflagRequest) (*http.Request, error) {
	path := "/api/v1/email/emails/{email_id}/unflag"
	path = strings.ReplaceAll(path, "{email_id}", url.PathEscape(fmt.Sprint(request.EmailID)))
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

func parseMailAccountResponse(resp *http.Response) (*MailAccountResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &MailAccountResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded MailboxAccountView
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

func parseMailBodyResponse(resp *http.Response) (*MailBodyResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &MailBodyResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded MailBodyResult
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

func parseMailFlagResponse(resp *http.Response) (*MailFlagResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &MailFlagResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded MailMutationResult
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

func parseMailMarkReadResponse(resp *http.Response) (*MailMarkReadResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &MailMarkReadResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded MailMutationResult
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

func parseMailMarkUnreadResponse(resp *http.Response) (*MailMarkUnreadResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &MailMarkUnreadResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded MailMutationResult
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

func parseMailMoveResponse(resp *http.Response) (*MailMoveResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &MailMoveResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded MailMutationResult
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

func parseMailSyncStatusResponse(resp *http.Response) (*MailSyncStatusResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &MailSyncStatusResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded MailSyncStatusOutputBody
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

func parseMailTrashResponse(resp *http.Response) (*MailTrashResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &MailTrashResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded MailMutationResult
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

func parseMailUnflagResponse(resp *http.Response) (*MailUnflagResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &MailUnflagResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded MailMutationResult
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
