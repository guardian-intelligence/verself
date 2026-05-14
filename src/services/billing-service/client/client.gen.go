// Code generated from the Smithy API contract. DO NOT EDIT.

package billingclient

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

const ServiceName = "billing-service"

type ActorId = string

type BillingSourceRef = string

type BillingSourceType = string

type BillingWindowId = string

type DecimalUint64 = string

type OrgId = string

type PlanId = string

type PricingPhase = string

type ProblemCode = string

type ProblemDetail = string

type ProblemType = string

type ProductId = string

type RequestId = string

type ReservationShape = string

type SafeInt64 = int64

type SafeUint64 = int64

type TraceParent = string

type WindowQuantity = int64

type WindowSequence = int64

type BillingAllocation map[string]float64

type BillingSKURates map[string]DecimalUint64

type BillingSettleResult struct {
	ActualQuantity      WindowQuantity  `json:"actual_quantity"`
	BillableQuantity    WindowQuantity  `json:"billable_quantity"`
	BilledChargeUnits   DecimalUint64   `json:"billed_charge_units"`
	SettledAt           string          `json:"settled_at"`
	WindowID            BillingWindowId `json:"window_id"`
	WriteoffChargeUnits DecimalUint64   `json:"writeoff_charge_units"`
	WriteoffQuantity    WindowQuantity  `json:"writeoff_quantity"`
}

type BillingWindowReservation struct {
	ActivatedAt         *string           `json:"activated_at,omitempty"`
	ActorID             ActorId           `json:"actor_id"`
	Allocation          BillingAllocation `json:"allocation"`
	CostPerUnit         DecimalUint64     `json:"cost_per_unit"`
	ExpiresAt           string            `json:"expires_at"`
	OrgID               OrgId             `json:"org_id"`
	PlanID              PlanId            `json:"plan_id"`
	PricingPhase        PricingPhase      `json:"pricing_phase"`
	ProductID           ProductId         `json:"product_id"`
	RenewBy             *string           `json:"renew_by,omitempty"`
	ReservationShape    ReservationShape  `json:"reservation_shape"`
	ReservedChargeUnits DecimalUint64     `json:"reserved_charge_units"`
	ReservedQuantity    WindowQuantity    `json:"reserved_quantity"`
	SkuRates            BillingSKURates   `json:"sku_rates"`
	SourceRef           BillingSourceRef  `json:"source_ref"`
	SourceType          BillingSourceType `json:"source_type"`
	WindowID            BillingWindowId   `json:"window_id"`
	WindowSeq           WindowSequence    `json:"window_seq"`
	WindowStart         string            `json:"window_start"`
}

type PaymentRequiredError struct {
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

type ActivateWindowInputBody struct {
	ActivatedAt string          `json:"activated_at"`
	WindowID    BillingWindowId `json:"window_id"`
}

type ActivateWindowRequest struct {
	Body ActivateWindowInputBody `json:"body"`
}

type ReserveWindowOutputBody struct {
	Reservation BillingWindowReservation `json:"reservation"`
}

type ActivateWindowResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ReserveWindowOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ReserveWindowInputBody struct {
	ActorID          ActorId           `json:"actor_id"`
	Allocation       BillingAllocation `json:"allocation"`
	BillingJobID     *SafeInt64        `json:"billing_job_id,omitempty"`
	ConcurrentCount  SafeUint64        `json:"concurrent_count"`
	OrgID            OrgId             `json:"org_id"`
	ProductID        ProductId         `json:"product_id"`
	ReservationShape ReservationShape  `json:"reservation_shape"`
	ReservedQuantity WindowQuantity    `json:"reserved_quantity"`
	SourceRef        BillingSourceRef  `json:"source_ref"`
	SourceType       BillingSourceType `json:"source_type"`
	WindowSeq        WindowSequence    `json:"window_seq"`
}

type ReserveWindowRequest struct {
	Body ReserveWindowInputBody `json:"body"`
}

type ReserveWindowResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ReserveWindowOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type SettleWindowInputBody struct {
	ActualQuantity WindowQuantity  `json:"actual_quantity"`
	UsageSummary   *map[string]any `json:"usage_summary,omitempty"`
	WindowID       BillingWindowId `json:"window_id"`
}

type SettleWindowRequest struct {
	Body SettleWindowInputBody `json:"body"`
}

type SettleWindowResponse struct {
	StatusCode   int
	Body         []byte
	Result       *BillingSettleResult
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type VoidWindowInputBody struct {
	WindowID BillingWindowId `json:"window_id"`
}

type VoidWindowRequest struct {
	Body VoidWindowInputBody `json:"body"`
}

type VoidWindowOutputBody struct {
	WindowID BillingWindowId `json:"window_id"`
}

type VoidWindowResponse struct {
	StatusCode   int
	Body         []byte
	Result       *VoidWindowOutputBody
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

func (c *Client) ActivateWindow(ctx context.Context, request ActivateWindowRequest, reqEditors ...RequestEditorFn) (*ActivateWindowResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newActivateWindowRequest(ctx, request)
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
	return parseActivateWindowResponse(resp)
}

func (c *Client) newActivateWindowRequest(ctx context.Context, request ActivateWindowRequest) (*http.Request, error) {
	path := "/internal/billing/v1/activate"
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

func (c *Client) ReserveWindow(ctx context.Context, request ReserveWindowRequest, reqEditors ...RequestEditorFn) (*ReserveWindowResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newReserveWindowRequest(ctx, request)
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
	return parseReserveWindowResponse(resp)
}

func (c *Client) newReserveWindowRequest(ctx context.Context, request ReserveWindowRequest) (*http.Request, error) {
	path := "/internal/billing/v1/reserve"
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

func (c *Client) SettleWindow(ctx context.Context, request SettleWindowRequest, reqEditors ...RequestEditorFn) (*SettleWindowResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newSettleWindowRequest(ctx, request)
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
	return parseSettleWindowResponse(resp)
}

func (c *Client) newSettleWindowRequest(ctx context.Context, request SettleWindowRequest) (*http.Request, error) {
	path := "/internal/billing/v1/settle"
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

func (c *Client) VoidWindow(ctx context.Context, request VoidWindowRequest, reqEditors ...RequestEditorFn) (*VoidWindowResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newVoidWindowRequest(ctx, request)
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
	return parseVoidWindowResponse(resp)
}

func (c *Client) newVoidWindowRequest(ctx context.Context, request VoidWindowRequest) (*http.Request, error) {
	path := "/internal/billing/v1/void"
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

func parseActivateWindowResponse(resp *http.Response) (*ActivateWindowResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ActivateWindowResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ReserveWindowOutputBody
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

func parseReserveWindowResponse(resp *http.Response) (*ReserveWindowResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ReserveWindowResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ReserveWindowOutputBody
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

func parseSettleWindowResponse(resp *http.Response) (*SettleWindowResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &SettleWindowResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded BillingSettleResult
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

func parseVoidWindowResponse(resp *http.Response) (*VoidWindowResponse, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &VoidWindowResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded VoidWindowOutputBody
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
