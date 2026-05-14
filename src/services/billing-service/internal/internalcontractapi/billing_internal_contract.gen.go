// Code generated from the Smithy API contract. DO NOT EDIT.

package internalcontractapi

import (
	"context"
)

type OperationDescriptor struct {
	ShapeID             string
	OperationID         string
	Method              string
	Path                string
	DefaultStatus       int
	Readonly            bool
	Paginated           bool
	Identity            IdentityDescriptor
	Authorization       AuthorizationDescriptor
	Audit               AuditDescriptor
	RateLimitBucket     string
	RequestBodyMaxBytes int64
	RequestPayload      PayloadDescriptor
	ResponsePayload     PayloadDescriptor
	ResponseHeaders     []HeaderDescriptor
	Idempotency         IdempotencyDescriptor
	SDK                 SDKDescriptor
	Problems            []ProblemDescriptor
}

type IdentityDescriptor struct {
	Mode       string
	Audience   string
	Principals []string
}

type AuthorizationDescriptor struct {
	Permission         string
	OrganizationSource string
	OrganizationMember string
}

type AuditDescriptor struct {
	Event    string
	Resource string
	Action   string
}

type IdempotencyDescriptor struct {
	Policy string
	Header string
	Member string
}

type PayloadDescriptor struct {
	Member    string
	Target    string
	Kind      string
	MediaType string
	Streaming bool
	Sensitive bool
	Required  bool
}

type HeaderDescriptor struct {
	Member string
	Name   string
}

type SDKDescriptor struct {
	Module    string
	Method    string
	Paginated bool
	Retryable bool
}

type ProblemDescriptor struct {
	ShapeID string
	Type    string
	Code    string
	Status  int
}

type Operation[Input any, Output any] struct {
	Descriptor OperationDescriptor
}

type Handler[Input any, Output any] func(context.Context, *Input) (*Output, error)

type ActorID string

type BillingSourceRef string

type BillingSourceType string

type BillingWindowID string

type DecimalUint64 string

type OrgID string

type PlanID string

type PricingPhase string

type ProductID string

type ReservationShape string

type SafeInt64 int64

type SafeUint64 int64

type WindowQuantity int64

type WindowSequence int

type ProblemCode string

type ProblemDetail string

type ProblemType string

type RequestID string

type TraceParent string

type BillingAllocation map[string]float64

type BillingSKURates map[string]DecimalUint64

type ActivateWindowInputBody struct {
	ActivatedAt string          `json:"activated_at" required:"true"`
	WindowID    BillingWindowID `json:"window_id" required:"true" minLength:"1" maxLength:"255"`
}

type ActivateWindowInput struct {
	Body ActivateWindowInputBody
}

type BillingSettleResult struct {
	ActualQuantity      WindowQuantity  `json:"actual_quantity" required:"true" minimum:"0" maximum:"4294967295"`
	BillableQuantity    WindowQuantity  `json:"billable_quantity" required:"true" minimum:"0" maximum:"4294967295"`
	BilledChargeUnits   DecimalUint64   `json:"billed_charge_units" required:"true" pattern:"^[0-9]+$"`
	SettledAt           string          `json:"settled_at" required:"true"`
	WindowID            BillingWindowID `json:"window_id" required:"true" minLength:"1" maxLength:"255"`
	WriteoffChargeUnits DecimalUint64   `json:"writeoff_charge_units" required:"true" pattern:"^[0-9]+$"`
	WriteoffQuantity    WindowQuantity  `json:"writeoff_quantity" required:"true" minimum:"0" maximum:"4294967295"`
}

type BillingWindowReservation struct {
	ActivatedAt         *string           `json:"activated_at,omitempty"`
	ActorID             ActorID           `json:"actor_id" required:"true" minLength:"1" maxLength:"255"`
	Allocation          BillingAllocation `json:"allocation" required:"true"`
	CostPerUnit         DecimalUint64     `json:"cost_per_unit" required:"true" pattern:"^[0-9]+$"`
	ExpiresAt           string            `json:"expires_at" required:"true"`
	OrgID               OrgID             `json:"org_id" required:"true" pattern:"^[0-9]+$"`
	PlanID              PlanID            `json:"plan_id" required:"true" minLength:"1" maxLength:"255"`
	PricingPhase        PricingPhase      `json:"pricing_phase" required:"true" minLength:"1" maxLength:"128"`
	ProductID           ProductID         `json:"product_id" required:"true" minLength:"1" maxLength:"255"`
	RenewBy             *string           `json:"renew_by,omitempty"`
	ReservationShape    ReservationShape  `json:"reservation_shape" required:"true" minLength:"1" maxLength:"128"`
	ReservedChargeUnits DecimalUint64     `json:"reserved_charge_units" required:"true" pattern:"^[0-9]+$"`
	ReservedQuantity    WindowQuantity    `json:"reserved_quantity" required:"true" minimum:"0" maximum:"4294967295"`
	SkuRates            BillingSKURates   `json:"sku_rates" required:"true"`
	SourceRef           BillingSourceRef  `json:"source_ref" required:"true" minLength:"1" maxLength:"255"`
	SourceType          BillingSourceType `json:"source_type" required:"true" minLength:"1" maxLength:"255"`
	WindowID            BillingWindowID   `json:"window_id" required:"true" minLength:"1" maxLength:"255"`
	WindowSeq           WindowSequence    `json:"window_seq" required:"true" minimum:"0" maximum:"2147483647"`
	WindowStart         string            `json:"window_start" required:"true"`
}

type ReserveWindowInputBody struct {
	ActorID          ActorID           `json:"actor_id" required:"true" minLength:"1" maxLength:"255"`
	Allocation       BillingAllocation `json:"allocation" required:"true"`
	BillingJobID     *SafeInt64        `json:"billing_job_id,omitempty" minimum:"-9007199254740991" maximum:"9007199254740991"`
	ConcurrentCount  SafeUint64        `json:"concurrent_count" required:"true" minimum:"0" maximum:"9007199254740991"`
	OrgID            OrgID             `json:"org_id" required:"true" pattern:"^[0-9]+$"`
	ProductID        ProductID         `json:"product_id" required:"true" minLength:"1" maxLength:"255"`
	ReservationShape ReservationShape  `json:"reservation_shape" required:"true" minLength:"1" maxLength:"128"`
	ReservedQuantity WindowQuantity    `json:"reserved_quantity" required:"true" minimum:"0" maximum:"4294967295"`
	SourceRef        BillingSourceRef  `json:"source_ref" required:"true" minLength:"1" maxLength:"255"`
	SourceType       BillingSourceType `json:"source_type" required:"true" minLength:"1" maxLength:"255"`
	WindowSeq        WindowSequence    `json:"window_seq" required:"true" minimum:"0" maximum:"2147483647"`
}

type ReserveWindowInput struct {
	Body ReserveWindowInputBody
}

type SettleWindowInputBody struct {
	ActualQuantity WindowQuantity  `json:"actual_quantity" required:"true" minimum:"0" maximum:"4294967295"`
	UsageSummary   *map[string]any `json:"usage_summary,omitempty"`
	WindowID       BillingWindowID `json:"window_id" required:"true" minLength:"1" maxLength:"255"`
}

type SettleWindowInput struct {
	Body SettleWindowInputBody
}

type VoidWindowInputBody struct {
	WindowID BillingWindowID `json:"window_id" required:"true" minLength:"1" maxLength:"255"`
}

type VoidWindowInput struct {
	Body VoidWindowInputBody
}

type PaymentRequiredError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type PermissionDeniedError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type ResourceNotFoundError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type ServiceUnavailableError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type ValidationFailedError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type ReserveWindowOutputBody struct {
	Reservation BillingWindowReservation `json:"reservation" required:"true"`
}

type ReserveWindowOutput struct {
	Body ReserveWindowOutputBody
}

type SettleWindowOutput struct {
	Body BillingSettleResult
}

type VoidWindowOutputBody struct {
	WindowID BillingWindowID `json:"window_id" required:"true" minLength:"1" maxLength:"255"`
}

type VoidWindowOutput struct {
	Body VoidWindowOutputBody
}

var Operations = []OperationDescriptor{
	ActivateWindow.Descriptor,
	ReserveWindow.Descriptor,
	SettleWindow.Descriptor,
	VoidWindow.Descriptor,
}

var ActivateWindow = Operation[ActivateWindowInput, ReserveWindowOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.billing.v1#ActivateWindow",
		OperationID:         "activate-window",
		Method:              "POST",
		Path:                "/internal/billing/v1/activate",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "spiffe_mtls", Audience: "billing-service", Principals: []string{"workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "billing:window:write", OrganizationSource: "request_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "billing.window.activate", Resource: "billing_window", Action: "update"},
		RateLimitBucket:     "internal_mutation",
		RequestBodyMaxBytes: 8192,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "billingInternal.windows", Method: "activate", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var ReserveWindow = Operation[ReserveWindowInput, ReserveWindowOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.billing.v1#ReserveWindow",
		OperationID:         "reserve-window",
		Method:              "POST",
		Path:                "/internal/billing/v1/reserve",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "spiffe_mtls", Audience: "billing-service", Principals: []string{"workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "billing:window:write", OrganizationSource: "body_org_id", OrganizationMember: "org_id"},
		Audit:               AuditDescriptor{Event: "billing.window.reserve", Resource: "billing_window", Action: "create"},
		RateLimitBucket:     "internal_mutation",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "billingInternal.windows", Method: "reserve", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PaymentRequiredError", Type: "urn:verself:problem:billing:payment_required", Code: "billing.payment_required", Status: 402},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var SettleWindow = Operation[SettleWindowInput, SettleWindowOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.billing.v1#SettleWindow",
		OperationID:         "settle-window",
		Method:              "POST",
		Path:                "/internal/billing/v1/settle",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "spiffe_mtls", Audience: "billing-service", Principals: []string{"workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "billing:window:write", OrganizationSource: "request_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "billing.window.settle", Resource: "billing_window", Action: "update"},
		RateLimitBucket:     "internal_mutation",
		RequestBodyMaxBytes: 8192,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "billingInternal.windows", Method: "settle", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var VoidWindow = Operation[VoidWindowInput, VoidWindowOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.billing.v1#VoidWindow",
		OperationID:         "void-window",
		Method:              "POST",
		Path:                "/internal/billing/v1/void",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "spiffe_mtls", Audience: "billing-service", Principals: []string{"workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "billing:window:write", OrganizationSource: "request_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "billing.window.void", Resource: "billing_window", Action: "delete"},
		RateLimitBucket:     "internal_mutation",
		RequestBodyMaxBytes: 8192,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "billingInternal.windows", Method: "void", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

type Handlers = PublicHandlers

type PublicHandlers interface {
	ActivateWindow(context.Context, *ActivateWindowInput) (*ReserveWindowOutput, error)
	ReserveWindow(context.Context, *ReserveWindowInput) (*ReserveWindowOutput, error)
	SettleWindow(context.Context, *SettleWindowInput) (*SettleWindowOutput, error)
	VoidWindow(context.Context, *VoidWindowInput) (*VoidWindowOutput, error)
}

type ActivateWindowHandler = Handler[ActivateWindowInput, ReserveWindowOutput]

type ReserveWindowHandler = Handler[ReserveWindowInput, ReserveWindowOutput]

type SettleWindowHandler = Handler[SettleWindowInput, SettleWindowOutput]

type VoidWindowHandler = Handler[VoidWindowInput, VoidWindowOutput]
