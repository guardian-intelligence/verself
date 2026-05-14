package notificationsinternalclient

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

type EmailAddress = string

type GrafanaLabelKey = string

type GrafanaLabelValue = string

type GrafanaString = string

type GrafanaURL = string

type IdempotencyKey = string

type NotificationBody = string

type NotificationSmallCount = int64

type NotificationTitle = string

type OrgId = string

type ProblemCode = string

type ProblemDetail = string

type ProblemType = string

type RequestId = string

type RequiredNotificationBody = string

type ResourceName = string

type SubjectId = string

type TraceParent = string

type WorkflowKey = string

type WorkflowRunId = string

type NotificationPriority string

const (
	NotificationPriorityHIGH   NotificationPriority = "high"
	NotificationPriorityLOW    NotificationPriority = "low"
	NotificationPriorityNORMAL NotificationPriority = "normal"
)

type GrafanaAlerts []GrafanaAlert

type WorkflowRecipients []WorkflowRecipient

type GrafanaLabelMap map[string]GrafanaLabelValue

type GrafanaAlert struct {
	Status       *GrafanaString   `json:"status,omitempty"`
	Labels       *GrafanaLabelMap `json:"labels,omitempty"`
	Annotations  *GrafanaLabelMap `json:"annotations,omitempty"`
	StartsAt     *string          `json:"startsAt,omitempty"`
	EndsAt       *string          `json:"endsAt,omitempty"`
	GeneratorURL *GrafanaURL      `json:"generatorURL,omitempty"`
	DashboardURL *GrafanaURL      `json:"dashboardURL,omitempty"`
	PanelURL     *GrafanaURL      `json:"panelURL,omitempty"`
	SilenceURL   *GrafanaURL      `json:"silenceURL,omitempty"`
	Fingerprint  *GrafanaString   `json:"fingerprint,omitempty"`
}

type GrafanaWebhookPayload struct {
	Receiver          *GrafanaString     `json:"receiver,omitempty"`
	Status            *GrafanaString     `json:"status,omitempty"`
	OrgID             *OrgId             `json:"orgId,omitempty"`
	GroupKey          *GrafanaString     `json:"groupKey,omitempty"`
	ExternalURL       *GrafanaURL        `json:"externalURL,omitempty"`
	Title             *NotificationTitle `json:"title,omitempty"`
	Message           *NotificationBody  `json:"message,omitempty"`
	CommonLabels      *GrafanaLabelMap   `json:"commonLabels,omitempty"`
	CommonAnnotations *GrafanaLabelMap   `json:"commonAnnotations,omitempty"`
	Alerts            *GrafanaAlerts     `json:"alerts,omitempty"`
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

type NotificationWorkflowTriggerResult struct {
	WorkflowRunID            WorkflowRunId          `json:"workflow_run_id"`
	WorkflowKey              WorkflowKey            `json:"workflow_key"`
	OrgID                    OrgId                  `json:"org_id"`
	AcceptedAt               string                 `json:"accepted_at"`
	RecipientCount           NotificationSmallCount `json:"recipient_count"`
	WebNotificationsAccepted NotificationSmallCount `json:"web_notifications_accepted"`
	EmailDeliveriesQueued    NotificationSmallCount `json:"email_deliveries_queued"`
	SuppressedCount          NotificationSmallCount `json:"suppressed_count"`
	Traceparent              *TraceParent           `json:"traceparent,omitempty"`
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

type WorkflowRecipient struct {
	SubjectID *SubjectId    `json:"subject_id,omitempty"`
	Email     *EmailAddress `json:"email,omitempty"`
}

type ReceiveGrafanaAlertWebhookRequest struct {
	Body GrafanaWebhookPayload `json:"body"`
}

type ReceiveGrafanaAlertWebhookResponse struct {
	StatusCode   int
	Body         []byte
	Result       *NotificationWorkflowTriggerResult
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type TriggerNotificationWorkflowInputBody struct {
	ActionURL          *ActionURL               `json:"action_url,omitempty"`
	Body               RequiredNotificationBody `json:"body"`
	Data               *map[string]any          `json:"data,omitempty"`
	OrgID              OrgId                    `json:"org_id"`
	Priority           *NotificationPriority    `json:"priority,omitempty"`
	Recipients         WorkflowRecipients       `json:"recipients"`
	TargetResourceName *ResourceName            `json:"targetResourceName,omitempty"`
	Title              *NotificationTitle       `json:"title,omitempty"`
	Traceparent        *TraceParent             `json:"traceparent,omitempty"`
}

type TriggerNotificationWorkflowRequest struct {
	IdempotencyKey IdempotencyKey
	WorkflowKey    WorkflowKey
	Body           TriggerNotificationWorkflowInputBody `json:"body"`
}

type TriggerNotificationWorkflowResponse struct {
	StatusCode   int
	Body         []byte
	Result       *NotificationWorkflowTriggerResult
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

func (c *Client) ReceiveGrafanaAlertWebhook(ctx context.Context, request ReceiveGrafanaAlertWebhookRequest, reqEditors ...RequestEditorFn) (*ReceiveGrafanaAlertWebhookResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newReceiveGrafanaAlertWebhookRequest(ctx, request)
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
	return parseReceiveGrafanaAlertWebhookResponse(resp)
}

func (c *Client) newReceiveGrafanaAlertWebhookRequest(ctx context.Context, request ReceiveGrafanaAlertWebhookRequest) (*http.Request, error) {
	path := "/internal/v1/integrations/grafana/alerts"
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

func (c *Client) TriggerNotificationWorkflow(ctx context.Context, request TriggerNotificationWorkflowRequest, reqEditors ...RequestEditorFn) (*TriggerNotificationWorkflowResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newTriggerNotificationWorkflowRequest(ctx, request)
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
	return parseTriggerNotificationWorkflowResponse(resp)
}

func (c *Client) newTriggerNotificationWorkflowRequest(ctx context.Context, request TriggerNotificationWorkflowRequest) (*http.Request, error) {
	path := "/internal/v1/workflows/{workflow_key}/trigger"
	path = strings.ReplaceAll(path, "{workflow_key}", url.PathEscape(fmt.Sprint(request.WorkflowKey)))
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

func parseReceiveGrafanaAlertWebhookResponse(resp *http.Response) (*ReceiveGrafanaAlertWebhookResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ReceiveGrafanaAlertWebhookResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 202 {
		var decoded NotificationWorkflowTriggerResult
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

func parseTriggerNotificationWorkflowResponse(resp *http.Response) (*TriggerNotificationWorkflowResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &TriggerNotificationWorkflowResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 202 {
		var decoded NotificationWorkflowTriggerResult
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
