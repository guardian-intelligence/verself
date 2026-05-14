package billing

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

type BillingBucketId = string

type BillingCadence = string

type BillingChangeId = string

type BillingDisplayName = string

type BillingEntitlementPeriodId = string

type BillingFinalizationId = string

type BillingLabel = string

type BillingMode = string

type BillingPolicyVersion = string

type BillingScopeId = string

type BillingScopeType = string

type BillingSkuId = string

type BillingSource = string

type BillingSourceReferenceId = string

type BillingState = string

type BillingTier = string

type CheckoutAmountCents = int64

type ContractId = string

type CoverageLabel = string

type Currency = string

type CycleId = string

type DecimalInt64 = string

type DecimalUint64 = string

type DisplayName = string

type DocumentId = string

type DocumentKind = string

type DocumentNumber = string

type EntitlementState = string

type IdempotencyKey = string

type OrgId = string

type PaymentState = string

type PlanId = string

type PricingPhase = string

type ProblemCode = string

type ProblemDetail = string

type ProblemType = string

type ProductId = string

type QuantityUnit = string

type RequestId = string

type ResourceName = string

type TraceParent = string

type URL = string

type BillingContracts []BillingContract

type BillingDocuments []BillingDocument

type BillingEntitlementBucketSections []BillingEntitlementBucketSection

type BillingEntitlementProductSections []BillingEntitlementProductSection

type BillingEntitlementSlots []BillingEntitlementSlot

type BillingEntitlementSourceTotals []BillingEntitlementSourceTotal

type BillingGrants []BillingGrant

type BillingPlans []BillingPlan

type BillingStatementGrantSummaries []BillingStatementGrantSummary

type BillingStatementLineItems []BillingStatementLineItem

type BillingContract struct {
	CadenceKind               BillingCadence   `json:"cadence_kind"`
	ContractID                ContractId       `json:"contract_id"`
	EndsAt                    *string          `json:"ends_at,omitempty"`
	EntitlementState          EntitlementState `json:"entitlement_state"`
	PaymentState              PaymentState     `json:"payment_state"`
	PendingChangeEffectiveAt  *string          `json:"pending_change_effective_at,omitempty"`
	PendingChangeID           *BillingChangeId `json:"pending_change_id,omitempty"`
	PendingChangeTargetPlanID *PlanId          `json:"pending_change_target_plan_id,omitempty"`
	PendingChangeType         *BillingState    `json:"pending_change_type,omitempty"`
	PhaseEnd                  *string          `json:"phase_end,omitempty"`
	PhaseID                   string           `json:"phase_id"`
	PhaseStart                *string          `json:"phase_start,omitempty"`
	PlanID                    PlanId           `json:"plan_id"`
	ProductID                 ProductId        `json:"product_id"`
	StartsAt                  string           `json:"starts_at"`
	Status                    BillingState     `json:"status"`
}

type BillingContractChangeResponse struct {
	ChangeID        BillingChangeId        `json:"change_id"`
	DocumentID      *DocumentId            `json:"document_id,omitempty"`
	FinalizationID  *BillingFinalizationId `json:"finalization_id,omitempty"`
	PriceDeltaUnits DecimalUint64          `json:"price_delta_units"`
	Status          BillingState           `json:"status"`
	URL             URL                    `json:"url"`
}

type BillingDocument struct {
	AdjustmentUnits        DecimalInt64          `json:"adjustment_units"`
	Currency               Currency              `json:"currency"`
	CycleID                CycleId               `json:"cycle_id"`
	DocumentID             DocumentId            `json:"document_id"`
	DocumentKind           DocumentKind          `json:"document_kind"`
	DocumentNumber         DocumentNumber        `json:"document_number"`
	FinalizationID         BillingFinalizationId `json:"finalization_id"`
	IssuedAt               *string               `json:"issued_at,omitempty"`
	PaymentStatus          PaymentState          `json:"payment_status"`
	PeriodEnd              string                `json:"period_end"`
	PeriodStart            string                `json:"period_start"`
	ProductID              ProductId             `json:"product_id"`
	ResourceName           ResourceName          `json:"resourceName"`
	Status                 BillingState          `json:"status"`
	StripeHostedInvoiceURL *URL                  `json:"stripe_hosted_invoice_url,omitempty"`
	StripeInvoicePdfURL    *URL                  `json:"stripe_invoice_pdf_url,omitempty"`
	StripePaymentIntentID  *string               `json:"stripe_payment_intent_id,omitempty"`
	SubtotalUnits          DecimalUint64         `json:"subtotal_units"`
	TaxUnits               DecimalUint64         `json:"tax_units"`
	TotalDueUnits          DecimalUint64         `json:"total_due_units"`
}

type BillingEntitlementBucketSection struct {
	BucketID    BillingBucketId         `json:"bucket_id"`
	BucketSlot  *BillingEntitlementSlot `json:"bucket_slot,omitempty"`
	DisplayName DisplayName             `json:"display_name"`
	SkuSlots    BillingEntitlementSlots `json:"sku_slots"`
}

type BillingEntitlementProductSection struct {
	Buckets     BillingEntitlementBucketSections `json:"buckets"`
	DisplayName DisplayName                      `json:"display_name"`
	ProductID   ProductId                        `json:"product_id"`
	ProductSlot *BillingEntitlementSlot          `json:"product_slot,omitempty"`
}

type BillingEntitlementSlot struct {
	AvailableUnits   DecimalUint64                  `json:"available_units"`
	BucketDisplay    BillingDisplayName             `json:"bucket_display"`
	BucketID         BillingScopeId                 `json:"bucket_id"`
	CoverageLabel    CoverageLabel                  `json:"coverage_label"`
	PendingUnits     DecimalUint64                  `json:"pending_units"`
	PeriodStartUnits DecimalUint64                  `json:"period_start_units"`
	ProductDisplay   BillingDisplayName             `json:"product_display"`
	ProductID        BillingScopeId                 `json:"product_id"`
	ScopeType        BillingScopeType               `json:"scope_type"`
	SkuDisplay       BillingDisplayName             `json:"sku_display"`
	SkuID            BillingSkuId                   `json:"sku_id"`
	Sources          BillingEntitlementSourceTotals `json:"sources"`
	SpentUnits       DecimalUint64                  `json:"spent_units"`
}

type BillingEntitlementSourceTotal struct {
	AvailableUnits   DecimalUint64  `json:"available_units"`
	InlineExpiresAt  *string        `json:"inline_expires_at,omitempty"`
	Label            BillingLabel   `json:"label"`
	PendingUnits     DecimalUint64  `json:"pending_units"`
	PeriodStartUnits DecimalUint64  `json:"period_start_units"`
	PlanID           BillingScopeId `json:"plan_id"`
	Source           BillingSource  `json:"source"`
}

type BillingEntitlementsView struct {
	OrgID     OrgId                             `json:"org_id"`
	Products  BillingEntitlementProductSections `json:"products"`
	Universal BillingEntitlementSlot            `json:"universal"`
}

type BillingGrant struct {
	Available           DecimalUint64              `json:"available"`
	EntitlementPeriodID BillingEntitlementPeriodId `json:"entitlement_period_id"`
	ExpiresAt           *string                    `json:"expires_at,omitempty"`
	GrantID             string                     `json:"grant_id"`
	Pending             DecimalUint64              `json:"pending"`
	PeriodEnd           *string                    `json:"period_end,omitempty"`
	PeriodStart         *string                    `json:"period_start,omitempty"`
	PolicyVersion       BillingPolicyVersion       `json:"policy_version"`
	ScopeBucketID       BillingScopeId             `json:"scope_bucket_id"`
	ScopeProductID      BillingScopeId             `json:"scope_product_id"`
	ScopeSkuID          BillingSkuId               `json:"scope_sku_id"`
	ScopeType           BillingScopeType           `json:"scope_type"`
	Source              BillingSource              `json:"source"`
	SourceReferenceID   BillingSourceReferenceId   `json:"source_reference_id"`
	StartsAt            string                     `json:"starts_at"`
}

type BillingPlan struct {
	Active             bool          `json:"active"`
	AnnualAmountCents  DecimalUint64 `json:"annual_amount_cents"`
	BillingMode        BillingMode   `json:"billing_mode"`
	Currency           Currency      `json:"currency"`
	DisplayName        DisplayName   `json:"display_name"`
	IsDefault          bool          `json:"is_default"`
	MonthlyAmountCents DecimalUint64 `json:"monthly_amount_cents"`
	PlanID             PlanId        `json:"plan_id"`
	ProductID          ProductId     `json:"product_id"`
	Tier               BillingTier   `json:"tier"`
}

type BillingStatement struct {
	Currency       Currency                       `json:"currency"`
	GeneratedAt    string                         `json:"generated_at"`
	GrantSummaries BillingStatementGrantSummaries `json:"grant_summaries"`
	LineItems      BillingStatementLineItems      `json:"line_items"`
	OrgID          OrgId                          `json:"org_id"`
	PeriodEnd      string                         `json:"period_end"`
	PeriodSource   string                         `json:"period_source"`
	PeriodStart    string                         `json:"period_start"`
	ProductID      ProductId                      `json:"product_id"`
	Totals         BillingStatementTotals         `json:"totals"`
	UnitLabel      string                         `json:"unit_label"`
}

type BillingStatementGrantSummary struct {
	Available      DecimalUint64    `json:"available"`
	Pending        DecimalUint64    `json:"pending"`
	ScopeBucketID  BillingScopeId   `json:"scope_bucket_id"`
	ScopeProductID BillingScopeId   `json:"scope_product_id"`
	ScopeType      BillingScopeType `json:"scope_type"`
	Source         BillingSource    `json:"source"`
}

type BillingStatementLineItem struct {
	BucketDisplayName BillingDisplayName `json:"bucket_display_name"`
	BucketID          BillingBucketId    `json:"bucket_id"`
	ChargeUnits       DecimalUint64      `json:"charge_units"`
	ContractUnits     DecimalUint64      `json:"contract_units"`
	FreeTierUnits     DecimalUint64      `json:"free_tier_units"`
	PlanID            BillingScopeId     `json:"plan_id"`
	PricingPhase      PricingPhase       `json:"pricing_phase"`
	ProductID         ProductId          `json:"product_id"`
	PromoUnits        DecimalUint64      `json:"promo_units"`
	PurchaseUnits     DecimalUint64      `json:"purchase_units"`
	Quantity          float64            `json:"quantity"`
	QuantityUnit      QuantityUnit       `json:"quantity_unit"`
	ReceivableUnits   DecimalUint64      `json:"receivable_units"`
	RefundUnits       DecimalUint64      `json:"refund_units"`
	ReservedUnits     DecimalUint64      `json:"reserved_units"`
	SkuDisplayName    BillingDisplayName `json:"sku_display_name"`
	SkuID             BillingSkuId       `json:"sku_id"`
	UnitRate          DecimalUint64      `json:"unit_rate"`
}

type BillingStatementTotals struct {
	ChargeUnits     DecimalUint64 `json:"charge_units"`
	ContractUnits   DecimalUint64 `json:"contract_units"`
	FreeTierUnits   DecimalUint64 `json:"free_tier_units"`
	PromoUnits      DecimalUint64 `json:"promo_units"`
	PurchaseUnits   DecimalUint64 `json:"purchase_units"`
	ReceivableUnits DecimalUint64 `json:"receivable_units"`
	RefundUnits     DecimalUint64 `json:"refund_units"`
	ReservedUnits   DecimalUint64 `json:"reserved_units"`
	TotalDueUnits   DecimalUint64 `json:"total_due_units"`
}

type BillingURLResponse struct {
	URL URL `json:"url"`
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

type CancelBillingContractRequest struct {
	ContractID     ContractId
	IdempotencyKey IdempotencyKey
}

type BillingCancelContractResponseOutputBody struct {
	Contract BillingContract `json:"contract"`
}

type CancelBillingContractResponse struct {
	StatusCode   int
	Body         []byte
	Result       *BillingCancelContractResponseOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type CheckoutInputBody struct {
	AmountCents CheckoutAmountCents `json:"amount_cents"`
	CancelURL   URL                 `json:"cancel_url"`
	ProductID   ProductId           `json:"product_id"`
	SuccessURL  URL                 `json:"success_url"`
}

type CreateBillingCheckoutRequest struct {
	IdempotencyKey IdempotencyKey
	Body           CheckoutInputBody `json:"body"`
}

type CreateBillingCheckoutResponse struct {
	StatusCode   int
	Body         []byte
	Result       *BillingURLResponse
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type CreateBillingContractInputBody struct {
	Cadence    *BillingCadence `json:"cadence,omitempty"`
	CancelURL  URL             `json:"cancel_url"`
	PlanID     PlanId          `json:"plan_id"`
	SuccessURL URL             `json:"success_url"`
}

type CreateBillingContractRequest struct {
	IdempotencyKey IdempotencyKey
	Body           CreateBillingContractInputBody `json:"body"`
}

type CreateBillingContractResponse struct {
	StatusCode   int
	Body         []byte
	Result       *BillingURLResponse
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type CreateBillingContractChangeInputBody struct {
	CancelURL    URL    `json:"cancel_url"`
	SuccessURL   URL    `json:"success_url"`
	TargetPlanID PlanId `json:"target_plan_id"`
}

type CreateBillingContractChangeRequest struct {
	ContractID     ContractId
	IdempotencyKey IdempotencyKey
	Body           CreateBillingContractChangeInputBody `json:"body"`
}

type CreateBillingContractChangeResponse struct {
	StatusCode   int
	Body         []byte
	Result       *BillingContractChangeResponse
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type CreateBillingPortalInputBody struct {
	ReturnURL URL `json:"return_url"`
}

type CreateBillingPortalRequest struct {
	IdempotencyKey IdempotencyKey
	Body           CreateBillingPortalInputBody `json:"body"`
}

type CreateBillingPortalResponse struct {
	StatusCode   int
	Body         []byte
	Result       *BillingURLResponse
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type GetBillingEntitlementsRequest struct{}

type GetBillingEntitlementsResponse struct {
	StatusCode   int
	Body         []byte
	Result       *BillingEntitlementsView
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type GetBillingStatementRequest struct {
	ProductID ProductId
}

type GetBillingStatementResponse struct {
	StatusCode   int
	Body         []byte
	Result       *BillingStatement
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ListBillingContractsRequest struct{}

type ListBillingContractsOutputBody struct {
	Contracts BillingContracts `json:"contracts"`
}

type ListBillingContractsResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ListBillingContractsOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ListBillingDocumentsRequest struct {
	ProductID *ProductId
}

type ListBillingDocumentsOutputBody struct {
	Documents BillingDocuments `json:"documents"`
}

type ListBillingDocumentsResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ListBillingDocumentsOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ListBillingGrantsRequest struct {
	Active    *bool
	ProductID *ProductId
}

type ListBillingGrantsOutputBody struct {
	Grants BillingGrants `json:"grants"`
}

type ListBillingGrantsResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ListBillingGrantsOutputBody
	Problem      *ErrorModel
	HTTPResponse *http.Response
}

type ListBillingPlansRequest struct {
	ProductID ProductId
}

type ListBillingPlansOutputBody struct {
	Plans BillingPlans `json:"plans"`
}

type ListBillingPlansResponse struct {
	StatusCode   int
	Body         []byte
	Result       *ListBillingPlansOutputBody
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

func (c *Client) CancelBillingContract(ctx context.Context, request CancelBillingContractRequest, reqEditors ...RequestEditorFn) (*CancelBillingContractResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newCancelBillingContractRequest(ctx, request)
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
	return parseCancelBillingContractResponse(resp)
}

func (c *Client) newCancelBillingContractRequest(ctx context.Context, request CancelBillingContractRequest) (*http.Request, error) {
	path := "/api/v1/contracts/{contract_id}/cancel"
	path = strings.ReplaceAll(path, "{contract_id}", url.PathEscape(fmt.Sprint(request.ContractID)))
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

func (c *Client) CreateBillingCheckout(ctx context.Context, request CreateBillingCheckoutRequest, reqEditors ...RequestEditorFn) (*CreateBillingCheckoutResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newCreateBillingCheckoutRequest(ctx, request)
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
	return parseCreateBillingCheckoutResponse(resp)
}

func (c *Client) newCreateBillingCheckoutRequest(ctx context.Context, request CreateBillingCheckoutRequest) (*http.Request, error) {
	path := "/api/v1/checkout"
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

func (c *Client) CreateBillingContract(ctx context.Context, request CreateBillingContractRequest, reqEditors ...RequestEditorFn) (*CreateBillingContractResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newCreateBillingContractRequest(ctx, request)
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
	return parseCreateBillingContractResponse(resp)
}

func (c *Client) newCreateBillingContractRequest(ctx context.Context, request CreateBillingContractRequest) (*http.Request, error) {
	path := "/api/v1/contracts"
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

func (c *Client) CreateBillingContractChange(ctx context.Context, request CreateBillingContractChangeRequest, reqEditors ...RequestEditorFn) (*CreateBillingContractChangeResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newCreateBillingContractChangeRequest(ctx, request)
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
	return parseCreateBillingContractChangeResponse(resp)
}

func (c *Client) newCreateBillingContractChangeRequest(ctx context.Context, request CreateBillingContractChangeRequest) (*http.Request, error) {
	path := "/api/v1/contracts/{contract_id}/changes"
	path = strings.ReplaceAll(path, "{contract_id}", url.PathEscape(fmt.Sprint(request.ContractID)))
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

func (c *Client) CreateBillingPortal(ctx context.Context, request CreateBillingPortalRequest, reqEditors ...RequestEditorFn) (*CreateBillingPortalResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newCreateBillingPortalRequest(ctx, request)
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
	return parseCreateBillingPortalResponse(resp)
}

func (c *Client) newCreateBillingPortalRequest(ctx context.Context, request CreateBillingPortalRequest) (*http.Request, error) {
	path := "/api/v1/portal"
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

func (c *Client) GetBillingEntitlements(ctx context.Context, request GetBillingEntitlementsRequest, reqEditors ...RequestEditorFn) (*GetBillingEntitlementsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newGetBillingEntitlementsRequest(ctx, request)
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
	return parseGetBillingEntitlementsResponse(resp)
}

func (c *Client) newGetBillingEntitlementsRequest(ctx context.Context, request GetBillingEntitlementsRequest) (*http.Request, error) {
	path := "/api/v1/entitlements"
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

func (c *Client) GetBillingStatement(ctx context.Context, request GetBillingStatementRequest, reqEditors ...RequestEditorFn) (*GetBillingStatementResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newGetBillingStatementRequest(ctx, request)
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
	return parseGetBillingStatementResponse(resp)
}

func (c *Client) newGetBillingStatementRequest(ctx context.Context, request GetBillingStatementRequest) (*http.Request, error) {
	path := "/api/v1/statement"
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("product_id", fmt.Sprint(request.ProductID))
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) ListBillingContracts(ctx context.Context, request ListBillingContractsRequest, reqEditors ...RequestEditorFn) (*ListBillingContractsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newListBillingContractsRequest(ctx, request)
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
	return parseListBillingContractsResponse(resp)
}

func (c *Client) newListBillingContractsRequest(ctx context.Context, request ListBillingContractsRequest) (*http.Request, error) {
	path := "/api/v1/contracts"
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

func (c *Client) ListBillingDocuments(ctx context.Context, request ListBillingDocumentsRequest, reqEditors ...RequestEditorFn) (*ListBillingDocumentsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newListBillingDocumentsRequest(ctx, request)
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
	return parseListBillingDocumentsResponse(resp)
}

func (c *Client) newListBillingDocumentsRequest(ctx context.Context, request ListBillingDocumentsRequest) (*http.Request, error) {
	path := "/api/v1/billing-documents"
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	if request.ProductID != nil {
		query.Set("product_id", fmt.Sprint(*request.ProductID))
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) ListBillingGrants(ctx context.Context, request ListBillingGrantsRequest, reqEditors ...RequestEditorFn) (*ListBillingGrantsResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newListBillingGrantsRequest(ctx, request)
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
	return parseListBillingGrantsResponse(resp)
}

func (c *Client) newListBillingGrantsRequest(ctx context.Context, request ListBillingGrantsRequest) (*http.Request, error) {
	path := "/api/v1/grants"
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	if request.Active != nil {
		query.Set("active", fmt.Sprint(*request.Active))
	}
	if request.ProductID != nil {
		query.Set("product_id", fmt.Sprint(*request.ProductID))
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) ListBillingPlans(ctx context.Context, request ListBillingPlansRequest, reqEditors ...RequestEditorFn) (*ListBillingPlansResponse, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("%s SDK transport: client is not initialized", ServiceName)
	}
	req, err := c.newListBillingPlansRequest(ctx, request)
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
	return parseListBillingPlansResponse(resp)
}

func (c *Client) newListBillingPlansRequest(ctx context.Context, request ListBillingPlansRequest) (*http.Request, error) {
	path := "/api/v1/plans"
	endpoint, err := url.Parse(c.server + path)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("product_id", fmt.Sprint(request.ProductID))
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func parseCancelBillingContractResponse(resp *http.Response) (*CancelBillingContractResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &CancelBillingContractResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded BillingCancelContractResponseOutputBody
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

func parseCreateBillingCheckoutResponse(resp *http.Response) (*CreateBillingCheckoutResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &CreateBillingCheckoutResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded BillingURLResponse
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

func parseCreateBillingContractResponse(resp *http.Response) (*CreateBillingContractResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &CreateBillingContractResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded BillingURLResponse
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

func parseCreateBillingContractChangeResponse(resp *http.Response) (*CreateBillingContractChangeResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &CreateBillingContractChangeResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded BillingContractChangeResponse
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

func parseCreateBillingPortalResponse(resp *http.Response) (*CreateBillingPortalResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &CreateBillingPortalResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded BillingURLResponse
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

func parseGetBillingEntitlementsResponse(resp *http.Response) (*GetBillingEntitlementsResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &GetBillingEntitlementsResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded BillingEntitlementsView
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

func parseGetBillingStatementResponse(resp *http.Response) (*GetBillingStatementResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &GetBillingStatementResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded BillingStatement
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

func parseListBillingContractsResponse(resp *http.Response) (*ListBillingContractsResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ListBillingContractsResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ListBillingContractsOutputBody
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

func parseListBillingDocumentsResponse(resp *http.Response) (*ListBillingDocumentsResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ListBillingDocumentsResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ListBillingDocumentsOutputBody
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

func parseListBillingGrantsResponse(resp *http.Response) (*ListBillingGrantsResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ListBillingGrantsResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ListBillingGrantsOutputBody
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

func parseListBillingPlansResponse(resp *http.Response) (*ListBillingPlansResponse, error) {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &ListBillingPlansResponse{StatusCode: resp.StatusCode, Body: body, HTTPResponse: resp}
	if resp.StatusCode == 200 {
		var decoded ListBillingPlansOutputBody
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
