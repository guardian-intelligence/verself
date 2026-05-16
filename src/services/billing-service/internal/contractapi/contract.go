package contractapi

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

type BillingBucketID string

type BillingCadence string

type BillingChangeID string

type BillingDisplayName string

type BillingEntitlementPeriodID string

type BillingFinalizationID string

type BillingLabel string

type BillingMode string

type BillingPolicyVersion string

type BillingScopeID string

type BillingScopeType string

type BillingSkuID string

type BillingSource string

type BillingSourceReferenceID string

type BillingState string

type BillingTier string

type CheckoutAmountCents int64

type ContractID string

type CoverageLabel string

type Currency string

type CycleID string

type DecimalInt64 string

type DecimalUint64 string

type DocumentID string

type DocumentKind string

type DocumentNumber string

type EntitlementState string

type OrgID string

type PaymentState string

type PlanID string

type PricingPhase string

type ProductID string

type QuantityUnit string

type URL string

type DisplayName string

type IdempotencyKey string

type ProblemCode string

type ProblemDetail string

type ProblemType string

type RequestID string

type ResourceName string

type SafeUint64 int64

type TraceParent string

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
	CadenceKind               BillingCadence   `json:"cadence_kind" required:"true" minLength:"1" maxLength:"128"`
	ContractID                ContractID       `json:"contract_id" required:"true" minLength:"1" maxLength:"255"`
	EndsAt                    *string          `json:"ends_at,omitempty"`
	EntitlementState          EntitlementState `json:"entitlement_state" required:"true" minLength:"1" maxLength:"128"`
	PaymentState              PaymentState     `json:"payment_state" required:"true" minLength:"1" maxLength:"128"`
	PendingChangeEffectiveAt  *string          `json:"pending_change_effective_at,omitempty"`
	PendingChangeID           *BillingChangeID `json:"pending_change_id,omitempty" minLength:"1" maxLength:"255"`
	PendingChangeTargetPlanID *PlanID          `json:"pending_change_target_plan_id,omitempty" minLength:"1" maxLength:"255"`
	PendingChangeType         *BillingState    `json:"pending_change_type,omitempty" minLength:"1" maxLength:"128"`
	PhaseEnd                  *string          `json:"phase_end,omitempty"`
	PhaseID                   string           `json:"phase_id" required:"true"`
	PhaseStart                *string          `json:"phase_start,omitempty"`
	PlanID                    PlanID           `json:"plan_id" required:"true" minLength:"1" maxLength:"255"`
	ProductID                 ProductID        `json:"product_id" required:"true" minLength:"1" maxLength:"255"`
	StartsAt                  string           `json:"starts_at" required:"true"`
	Status                    BillingState     `json:"status" required:"true" minLength:"1" maxLength:"128"`
}

type BillingContractChangeResponse struct {
	ChangeID        BillingChangeID        `json:"change_id" required:"true" minLength:"1" maxLength:"255"`
	DocumentID      *DocumentID            `json:"document_id,omitempty" minLength:"1" maxLength:"255"`
	FinalizationID  *BillingFinalizationID `json:"finalization_id,omitempty" minLength:"1" maxLength:"255"`
	PriceDeltaUnits DecimalUint64          `json:"price_delta_units" required:"true" pattern:"^[0-9]+$"`
	Status          BillingState           `json:"status" required:"true" minLength:"1" maxLength:"128"`
	URL             URL                    `json:"url" required:"true" minLength:"1" maxLength:"8192"`
}

type BillingDocument struct {
	AdjustmentUnits        DecimalInt64          `json:"adjustment_units" required:"true" pattern:"^-?[0-9]+$"`
	Currency               Currency              `json:"currency" required:"true" minLength:"1" maxLength:"32"`
	CycleID                CycleID               `json:"cycle_id" required:"true" minLength:"1" maxLength:"255"`
	DocumentID             DocumentID            `json:"document_id" required:"true" minLength:"1" maxLength:"255"`
	DocumentKind           DocumentKind          `json:"document_kind" required:"true" minLength:"1" maxLength:"255"`
	DocumentNumber         DocumentNumber        `json:"document_number" required:"true" minLength:"1" maxLength:"255"`
	FinalizationID         BillingFinalizationID `json:"finalization_id" required:"true" minLength:"1" maxLength:"255"`
	IssuedAt               *string               `json:"issued_at,omitempty"`
	PaymentStatus          PaymentState          `json:"payment_status" required:"true" minLength:"1" maxLength:"128"`
	PeriodEnd              string                `json:"period_end" required:"true"`
	PeriodStart            string                `json:"period_start" required:"true"`
	ProductID              ProductID             `json:"product_id" required:"true" minLength:"1" maxLength:"255"`
	ResourceName           ResourceName          `json:"resourceName" required:"true" minLength:"1" maxLength:"4096" pattern:"^urn:verself:.+$"`
	Status                 BillingState          `json:"status" required:"true" minLength:"1" maxLength:"128"`
	StripeHostedInvoiceURL *URL                  `json:"stripe_hosted_invoice_url,omitempty" minLength:"1" maxLength:"8192"`
	StripeInvoicePdfURL    *URL                  `json:"stripe_invoice_pdf_url,omitempty" minLength:"1" maxLength:"8192"`
	StripePaymentIntentID  *string               `json:"stripe_payment_intent_id,omitempty"`
	SubtotalUnits          DecimalUint64         `json:"subtotal_units" required:"true" pattern:"^[0-9]+$"`
	TaxUnits               DecimalUint64         `json:"tax_units" required:"true" pattern:"^[0-9]+$"`
	TotalDueUnits          DecimalUint64         `json:"total_due_units" required:"true" pattern:"^[0-9]+$"`
}

type BillingEntitlementBucketSection struct {
	BucketID    BillingBucketID         `json:"bucket_id" required:"true" maxLength:"255"`
	BucketSlot  *BillingEntitlementSlot `json:"bucket_slot,omitempty"`
	DisplayName DisplayName             `json:"display_name" required:"true" minLength:"1" maxLength:"120"`
	SkuSlots    BillingEntitlementSlots `json:"sku_slots" required:"true"`
}

type BillingEntitlementProductSection struct {
	Buckets     BillingEntitlementBucketSections `json:"buckets" required:"true"`
	DisplayName DisplayName                      `json:"display_name" required:"true" minLength:"1" maxLength:"120"`
	ProductID   ProductID                        `json:"product_id" required:"true" minLength:"1" maxLength:"255"`
	ProductSlot *BillingEntitlementSlot          `json:"product_slot,omitempty"`
}

type BillingEntitlementSlot struct {
	AvailableUnits   DecimalUint64                  `json:"available_units" required:"true" pattern:"^[0-9]+$"`
	BucketDisplay    BillingDisplayName             `json:"bucket_display" required:"true" maxLength:"255"`
	BucketID         BillingScopeID                 `json:"bucket_id" required:"true" maxLength:"255"`
	CoverageLabel    CoverageLabel                  `json:"coverage_label" required:"true" minLength:"1" maxLength:"255"`
	PendingUnits     DecimalUint64                  `json:"pending_units" required:"true" pattern:"^[0-9]+$"`
	PeriodStartUnits DecimalUint64                  `json:"period_start_units" required:"true" pattern:"^[0-9]+$"`
	ProductDisplay   BillingDisplayName             `json:"product_display" required:"true" maxLength:"255"`
	ProductID        BillingScopeID                 `json:"product_id" required:"true" maxLength:"255"`
	ScopeType        BillingScopeType               `json:"scope_type" required:"true" minLength:"1" maxLength:"255"`
	SkuDisplay       BillingDisplayName             `json:"sku_display" required:"true" maxLength:"255"`
	SkuID            BillingSkuID                   `json:"sku_id" required:"true" maxLength:"255"`
	Sources          BillingEntitlementSourceTotals `json:"sources" required:"true"`
	SpentUnits       DecimalUint64                  `json:"spent_units" required:"true" pattern:"^[0-9]+$"`
}

type BillingEntitlementSourceTotal struct {
	AvailableUnits   DecimalUint64  `json:"available_units" required:"true" pattern:"^[0-9]+$"`
	InlineExpiresAt  *string        `json:"inline_expires_at,omitempty"`
	Label            BillingLabel   `json:"label" required:"true" maxLength:"255"`
	PendingUnits     DecimalUint64  `json:"pending_units" required:"true" pattern:"^[0-9]+$"`
	PeriodStartUnits DecimalUint64  `json:"period_start_units" required:"true" pattern:"^[0-9]+$"`
	PlanID           BillingScopeID `json:"plan_id" required:"true" maxLength:"255"`
	Source           BillingSource  `json:"source" required:"true" maxLength:"255"`
}

type BillingEntitlementsView struct {
	DurableStorageQuotaBytes SafeUint64                        `json:"durable_storage_quota_bytes" required:"true" minimum:"0" maximum:"9007199254740991"`
	OrgID                    OrgID                             `json:"org_id" required:"true" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
	Products                 BillingEntitlementProductSections `json:"products" required:"true"`
	Universal                BillingEntitlementSlot            `json:"universal" required:"true"`
}

type BillingGrant struct {
	Available           DecimalUint64              `json:"available" required:"true" pattern:"^[0-9]+$"`
	EntitlementPeriodID BillingEntitlementPeriodID `json:"entitlement_period_id" required:"true" maxLength:"255"`
	ExpiresAt           *string                    `json:"expires_at,omitempty"`
	GrantID             string                     `json:"grant_id" required:"true"`
	Pending             DecimalUint64              `json:"pending" required:"true" pattern:"^[0-9]+$"`
	PeriodEnd           *string                    `json:"period_end,omitempty"`
	PeriodStart         *string                    `json:"period_start,omitempty"`
	PolicyVersion       BillingPolicyVersion       `json:"policy_version" required:"true" maxLength:"255"`
	ScopeBucketID       BillingScopeID             `json:"scope_bucket_id" required:"true" maxLength:"255"`
	ScopeProductID      BillingScopeID             `json:"scope_product_id" required:"true" maxLength:"255"`
	ScopeSkuID          BillingSkuID               `json:"scope_sku_id" required:"true" maxLength:"255"`
	ScopeType           BillingScopeType           `json:"scope_type" required:"true" minLength:"1" maxLength:"255"`
	Source              BillingSource              `json:"source" required:"true" maxLength:"255"`
	SourceReferenceID   BillingSourceReferenceID   `json:"source_reference_id" required:"true" maxLength:"255"`
	StartsAt            string                     `json:"starts_at" required:"true"`
}

type BillingPlan struct {
	Active             bool          `json:"active" required:"true"`
	AnnualAmountCents  DecimalUint64 `json:"annual_amount_cents" required:"true" pattern:"^[0-9]+$"`
	BillingMode        BillingMode   `json:"billing_mode" required:"true" minLength:"1" maxLength:"128"`
	Currency           Currency      `json:"currency" required:"true" minLength:"1" maxLength:"32"`
	DisplayName        DisplayName   `json:"display_name" required:"true" minLength:"1" maxLength:"120"`
	IsDefault          bool          `json:"is_default" required:"true"`
	MonthlyAmountCents DecimalUint64 `json:"monthly_amount_cents" required:"true" pattern:"^[0-9]+$"`
	PlanID             PlanID        `json:"plan_id" required:"true" minLength:"1" maxLength:"255"`
	ProductID          ProductID     `json:"product_id" required:"true" minLength:"1" maxLength:"255"`
	Tier               BillingTier   `json:"tier" required:"true" minLength:"1" maxLength:"128"`
}

type BillingStatement struct {
	Currency       Currency                       `json:"currency" required:"true" minLength:"1" maxLength:"32"`
	GeneratedAt    string                         `json:"generated_at" required:"true"`
	GrantSummaries BillingStatementGrantSummaries `json:"grant_summaries" required:"true"`
	LineItems      BillingStatementLineItems      `json:"line_items" required:"true"`
	OrgID          OrgID                          `json:"org_id" required:"true" pattern:"^org_[0-9A-HJKMNP-TV-Z]{26}$"`
	PeriodEnd      string                         `json:"period_end" required:"true"`
	PeriodSource   string                         `json:"period_source" required:"true"`
	PeriodStart    string                         `json:"period_start" required:"true"`
	ProductID      ProductID                      `json:"product_id" required:"true" minLength:"1" maxLength:"255"`
	Totals         BillingStatementTotals         `json:"totals" required:"true"`
	UnitLabel      string                         `json:"unit_label" required:"true"`
}

type BillingStatementGrantSummary struct {
	Available      DecimalUint64    `json:"available" required:"true" pattern:"^[0-9]+$"`
	Pending        DecimalUint64    `json:"pending" required:"true" pattern:"^[0-9]+$"`
	ScopeBucketID  BillingScopeID   `json:"scope_bucket_id" required:"true" maxLength:"255"`
	ScopeProductID BillingScopeID   `json:"scope_product_id" required:"true" maxLength:"255"`
	ScopeType      BillingScopeType `json:"scope_type" required:"true" minLength:"1" maxLength:"255"`
	Source         BillingSource    `json:"source" required:"true" maxLength:"255"`
}

type BillingStatementLineItem struct {
	BucketDisplayName BillingDisplayName `json:"bucket_display_name" required:"true" maxLength:"255"`
	BucketID          BillingBucketID    `json:"bucket_id" required:"true" maxLength:"255"`
	ChargeUnits       DecimalUint64      `json:"charge_units" required:"true" pattern:"^[0-9]+$"`
	ContractUnits     DecimalUint64      `json:"contract_units" required:"true" pattern:"^[0-9]+$"`
	FreeTierUnits     DecimalUint64      `json:"free_tier_units" required:"true" pattern:"^[0-9]+$"`
	PlanID            BillingScopeID     `json:"plan_id" required:"true" maxLength:"255"`
	PricingPhase      PricingPhase       `json:"pricing_phase" required:"true" minLength:"1" maxLength:"128"`
	ProductID         ProductID          `json:"product_id" required:"true" minLength:"1" maxLength:"255"`
	PromoUnits        DecimalUint64      `json:"promo_units" required:"true" pattern:"^[0-9]+$"`
	PurchaseUnits     DecimalUint64      `json:"purchase_units" required:"true" pattern:"^[0-9]+$"`
	Quantity          float64            `json:"quantity" required:"true"`
	QuantityUnit      QuantityUnit       `json:"quantity_unit" required:"true" minLength:"1" maxLength:"128"`
	ReceivableUnits   DecimalUint64      `json:"receivable_units" required:"true" pattern:"^[0-9]+$"`
	RefundUnits       DecimalUint64      `json:"refund_units" required:"true" pattern:"^[0-9]+$"`
	ReservedUnits     DecimalUint64      `json:"reserved_units" required:"true" pattern:"^[0-9]+$"`
	SkuDisplayName    BillingDisplayName `json:"sku_display_name" required:"true" maxLength:"255"`
	SkuID             BillingSkuID       `json:"sku_id" required:"true" maxLength:"255"`
	UnitRate          DecimalUint64      `json:"unit_rate" required:"true" pattern:"^[0-9]+$"`
}

type BillingStatementTotals struct {
	ChargeUnits     DecimalUint64 `json:"charge_units" required:"true" pattern:"^[0-9]+$"`
	ContractUnits   DecimalUint64 `json:"contract_units" required:"true" pattern:"^[0-9]+$"`
	FreeTierUnits   DecimalUint64 `json:"free_tier_units" required:"true" pattern:"^[0-9]+$"`
	PromoUnits      DecimalUint64 `json:"promo_units" required:"true" pattern:"^[0-9]+$"`
	PurchaseUnits   DecimalUint64 `json:"purchase_units" required:"true" pattern:"^[0-9]+$"`
	ReceivableUnits DecimalUint64 `json:"receivable_units" required:"true" pattern:"^[0-9]+$"`
	RefundUnits     DecimalUint64 `json:"refund_units" required:"true" pattern:"^[0-9]+$"`
	ReservedUnits   DecimalUint64 `json:"reserved_units" required:"true" pattern:"^[0-9]+$"`
	TotalDueUnits   DecimalUint64 `json:"total_due_units" required:"true" pattern:"^[0-9]+$"`
}

type BillingURLResponse struct {
	URL URL `json:"url" required:"true" minLength:"1" maxLength:"8192"`
}

type CheckoutInputBody struct {
	AmountCents CheckoutAmountCents `json:"amount_cents" required:"true" minimum:"1" maximum:"9007199254740991"`
	CancelURL   URL                 `json:"cancel_url" required:"true" minLength:"1" maxLength:"8192"`
	ProductID   ProductID           `json:"product_id" required:"true" minLength:"1" maxLength:"255"`
	SuccessURL  URL                 `json:"success_url" required:"true" minLength:"1" maxLength:"8192"`
}

type CheckoutInput struct {
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           CheckoutInputBody
}

type ContractMutationInput struct {
	ContractID     ContractID     `path:"contract_id" required:"true" minLength:"1" maxLength:"255"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
}

type CreateBillingContractChangeInputBody struct {
	CancelURL    URL    `json:"cancel_url" required:"true" minLength:"1" maxLength:"8192"`
	SuccessURL   URL    `json:"success_url" required:"true" minLength:"1" maxLength:"8192"`
	TargetPlanID PlanID `json:"target_plan_id" required:"true" minLength:"1" maxLength:"255"`
}

type CreateBillingContractChangeInput struct {
	ContractID     ContractID     `path:"contract_id" required:"true" minLength:"1" maxLength:"255"`
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           CreateBillingContractChangeInputBody
}

type CreateBillingContractInputBody struct {
	Cadence    *BillingCadence `json:"cadence,omitempty" minLength:"1" maxLength:"128"`
	CancelURL  URL             `json:"cancel_url" required:"true" minLength:"1" maxLength:"8192"`
	PlanID     PlanID          `json:"plan_id" required:"true" minLength:"1" maxLength:"255"`
	SuccessURL URL             `json:"success_url" required:"true" minLength:"1" maxLength:"8192"`
}

type CreateBillingContractInput struct {
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           CreateBillingContractInputBody
}

type CreateBillingPortalInputBody struct {
	ReturnURL URL `json:"return_url" required:"true" minLength:"1" maxLength:"8192"`
}

type CreateBillingPortalInput struct {
	IdempotencyKey IdempotencyKey `header:"Idempotency-Key" required:"true" minLength:"8" maxLength:"128"`
	Body           CreateBillingPortalInputBody
}

type DocumentsQueryInput struct {
	ProductID ProductID `query:"product_id" minLength:"1" maxLength:"255"`
}

type EmptyInput struct{}

type GrantsQueryInput struct {
	Active    bool      `query:"active"`
	ProductID ProductID `query:"product_id" minLength:"1" maxLength:"255"`
}

type ProductQueryInput struct {
	ProductID ProductID `query:"product_id" required:"true" minLength:"1" maxLength:"255"`
}

type ConflictError struct {
	Type        ProblemType    `json:"type" required:"true" pattern:"^(https://.+|urn:verself:problem:.+)$"`
	Title       string         `json:"title" required:"true"`
	Status      int            `json:"status" required:"true"`
	Detail      *ProblemDetail `json:"detail,omitempty" maxLength:"4096"`
	Instance    *string        `json:"instance,omitempty"`
	Code        ProblemCode    `json:"code" required:"true" pattern:"^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$"`
	RequestID   *RequestID     `json:"requestId,omitempty" minLength:"8" maxLength:"128"`
	Traceparent *TraceParent   `json:"traceparent,omitempty" minLength:"55" maxLength:"255"`
}

type IdempotencyPayloadMismatchError struct {
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

type RateLimitedError struct {
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

type UnauthenticatedError struct {
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

type ListBillingDocumentsOutputBody struct {
	Documents BillingDocuments `json:"documents" required:"true"`
}

type ListBillingDocumentsOutput struct {
	Body ListBillingDocumentsOutputBody
}

type BillingURLResponseOutput struct {
	Body BillingURLResponse
}

type ListBillingContractsOutputBody struct {
	Contracts BillingContracts `json:"contracts" required:"true"`
}

type ListBillingContractsOutput struct {
	Body ListBillingContractsOutputBody
}

type BillingCancelContractResponseOutputBody struct {
	Contract BillingContract `json:"contract" required:"true"`
}

type BillingCancelContractResponseOutput struct {
	Body BillingCancelContractResponseOutputBody
}

type CreateBillingContractChangeOutput struct {
	Body BillingContractChangeResponse
}

type GetBillingEntitlementsOutput struct {
	Body BillingEntitlementsView
}

type ListBillingGrantsOutputBody struct {
	Grants BillingGrants `json:"grants" required:"true"`
}

type ListBillingGrantsOutput struct {
	Body ListBillingGrantsOutputBody
}

type ListBillingPlansOutputBody struct {
	Plans BillingPlans `json:"plans" required:"true"`
}

type ListBillingPlansOutput struct {
	Body ListBillingPlansOutputBody
}

type GetBillingStatementOutput struct {
	Body BillingStatement
}

var Operations = []OperationDescriptor{
	ListBillingDocuments.Descriptor,
	CreateBillingCheckout.Descriptor,
	ListBillingContracts.Descriptor,
	CreateBillingContract.Descriptor,
	CancelBillingContract.Descriptor,
	CreateBillingContractChange.Descriptor,
	GetBillingEntitlements.Descriptor,
	ListBillingGrants.Descriptor,
	ListBillingPlans.Descriptor,
	CreateBillingPortal.Descriptor,
	GetBillingStatement.Descriptor,
}

var ListBillingDocuments = Operation[DocumentsQueryInput, ListBillingDocumentsOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.billing.v1#ListBillingDocuments",
		OperationID:         "list-billing-documents",
		Method:              "GET",
		Path:                "/api/v1/billing-documents",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "billing:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "billing.document.list", Resource: "billing_document_resource", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "billing.documents", Method: "list", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var CreateBillingCheckout = Operation[CheckoutInput, BillingURLResponseOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.billing.v1#CreateBillingCheckout",
		OperationID:         "create-billing-checkout",
		Method:              "POST",
		Path:                "/api/v1/checkout",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "billing:checkout", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "billing.checkout.create", Resource: "billing_entitlements", Action: "create"},
		RateLimitBucket:     "billing_mutation",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "billing.checkout", Method: "create", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var ListBillingContracts = Operation[EmptyInput, ListBillingContractsOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.billing.v1#ListBillingContracts",
		OperationID:         "list-billing-contracts",
		Method:              "GET",
		Path:                "/api/v1/contracts",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "billing:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "billing.contract.list", Resource: "billing_contract_resource", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "billing.contracts", Method: "list", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var CreateBillingContract = Operation[CreateBillingContractInput, BillingURLResponseOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.billing.v1#CreateBillingContract",
		OperationID:         "create-billing-contract",
		Method:              "POST",
		Path:                "/api/v1/contracts",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "billing:checkout", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "billing.contract_checkout.create", Resource: "billing_contract_resource", Action: "create"},
		RateLimitBucket:     "billing_mutation",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "billing.contracts", Method: "create", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var CancelBillingContract = Operation[ContractMutationInput, BillingCancelContractResponseOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.billing.v1#CancelBillingContract",
		OperationID:         "cancel-billing-contract",
		Method:              "POST",
		Path:                "/api/v1/contracts/{contract_id}/cancel",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "billing:checkout", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "billing.contract.cancel", Resource: "billing_contract_resource", Action: "cancel"},
		RateLimitBucket:     "billing_mutation",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "billing.contracts", Method: "cancel", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var CreateBillingContractChange = Operation[CreateBillingContractChangeInput, CreateBillingContractChangeOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.billing.v1#CreateBillingContractChange",
		OperationID:         "create-billing-contract-change",
		Method:              "POST",
		Path:                "/api/v1/contracts/{contract_id}/changes",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "billing:checkout", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "billing.contract_change.create", Resource: "billing_contract_resource", Action: "create"},
		RateLimitBucket:     "billing_mutation",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "billing.contracts", Method: "createChange", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#ConflictError", Type: "urn:verself:problem:conflict:state", Code: "conflict.state", Status: 409},
			{ShapeID: "verself.common.v1#IdempotencyPayloadMismatchError", Type: "urn:verself:problem:conflict:idempotency_payload_mismatch", Code: "conflict.idempotency_payload_mismatch", Status: 409},
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ResourceNotFoundError", Type: "urn:verself:problem:resource:not_found", Code: "resource.not_found", Status: 404},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var GetBillingEntitlements = Operation[EmptyInput, GetBillingEntitlementsOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.billing.v1#GetBillingEntitlements",
		OperationID:         "get-billing-entitlements",
		Method:              "GET",
		Path:                "/api/v1/entitlements",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "billing:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "billing.entitlements.read", Resource: "billing_entitlements", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "billing.entitlements", Method: "get", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var ListBillingGrants = Operation[GrantsQueryInput, ListBillingGrantsOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.billing.v1#ListBillingGrants",
		OperationID:         "list-billing-grants",
		Method:              "GET",
		Path:                "/api/v1/grants",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "billing:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "billing.grant.list", Resource: "billing_grant_resource", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "billing.grants", Method: "list", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var ListBillingPlans = Operation[ProductQueryInput, ListBillingPlansOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.billing.v1#ListBillingPlans",
		OperationID:         "list-billing-plans",
		Method:              "GET",
		Path:                "/api/v1/plans",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "billing:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "billing.plan.list", Resource: "billing_plan_resource", Action: "list"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "billing.plans", Method: "list", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

var CreateBillingPortal = Operation[CreateBillingPortalInput, BillingURLResponseOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.billing.v1#CreateBillingPortal",
		OperationID:         "create-billing-portal",
		Method:              "POST",
		Path:                "/api/v1/portal",
		DefaultStatus:       200,
		Readonly:            false,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli"}},
		Authorization:       AuthorizationDescriptor{Permission: "billing:checkout", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "billing.portal.create", Resource: "billing_contract_resource", Action: "create"},
		RateLimitBucket:     "billing_mutation",
		RequestBodyMaxBytes: 65536,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "idempotency_key_header", Header: "Idempotency-Key", Member: "idempotencyKey"},
		SDK:                 SDKDescriptor{Module: "billing.portal", Method: "create", Paginated: false, Retryable: false},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
			{ShapeID: "verself.common.v1#ValidationFailedError", Type: "urn:verself:problem:request:validation_failed", Code: "request.validation_failed", Status: 400},
		},
	},
}

var GetBillingStatement = Operation[ProductQueryInput, GetBillingStatementOutput]{
	Descriptor: OperationDescriptor{
		ShapeID:             "verself.billing.v1#GetBillingStatement",
		OperationID:         "get-billing-statement",
		Method:              "GET",
		Path:                "/api/v1/statement",
		DefaultStatus:       200,
		Readonly:            true,
		Paginated:           false,
		Identity:            IdentityDescriptor{Mode: "bearer", Audience: "verself-api", Principals: []string{"browser", "cli", "workload"}},
		Authorization:       AuthorizationDescriptor{Permission: "billing:read", OrganizationSource: "token_org_id", OrganizationMember: ""},
		Audit:               AuditDescriptor{Event: "billing.statement.read", Resource: "billing_document_resource", Action: "read"},
		RateLimitBucket:     "read",
		RequestBodyMaxBytes: 0,
		RequestPayload:      PayloadDescriptor{},
		ResponsePayload:     PayloadDescriptor{},
		ResponseHeaders:     []HeaderDescriptor{},
		Idempotency:         IdempotencyDescriptor{Policy: "", Header: "", Member: ""},
		SDK:                 SDKDescriptor{Module: "billing.statement", Method: "get", Paginated: false, Retryable: true},
		Problems: []ProblemDescriptor{
			{ShapeID: "verself.common.v1#PermissionDeniedError", Type: "urn:verself:problem:auth:permission_denied", Code: "auth.permission_denied", Status: 403},
			{ShapeID: "verself.common.v1#RateLimitedError", Type: "urn:verself:problem:quota:rate_limited", Code: "quota.rate_limited", Status: 429},
			{ShapeID: "verself.common.v1#ServiceUnavailableError", Type: "urn:verself:problem:service:unavailable", Code: "service.unavailable", Status: 503},
			{ShapeID: "verself.common.v1#UnauthenticatedError", Type: "urn:verself:problem:auth:unauthenticated", Code: "auth.unauthenticated", Status: 401},
		},
	},
}

type Handlers = PublicHandlers

type PublicHandlers interface {
	ListBillingDocuments(context.Context, *DocumentsQueryInput) (*ListBillingDocumentsOutput, error)
	CreateBillingCheckout(context.Context, *CheckoutInput) (*BillingURLResponseOutput, error)
	ListBillingContracts(context.Context, *EmptyInput) (*ListBillingContractsOutput, error)
	CreateBillingContract(context.Context, *CreateBillingContractInput) (*BillingURLResponseOutput, error)
	CancelBillingContract(context.Context, *ContractMutationInput) (*BillingCancelContractResponseOutput, error)
	CreateBillingContractChange(context.Context, *CreateBillingContractChangeInput) (*CreateBillingContractChangeOutput, error)
	GetBillingEntitlements(context.Context, *EmptyInput) (*GetBillingEntitlementsOutput, error)
	ListBillingGrants(context.Context, *GrantsQueryInput) (*ListBillingGrantsOutput, error)
	ListBillingPlans(context.Context, *ProductQueryInput) (*ListBillingPlansOutput, error)
	CreateBillingPortal(context.Context, *CreateBillingPortalInput) (*BillingURLResponseOutput, error)
	GetBillingStatement(context.Context, *ProductQueryInput) (*GetBillingStatementOutput, error)
}

type ListBillingDocumentsHandler = Handler[DocumentsQueryInput, ListBillingDocumentsOutput]

type CreateBillingCheckoutHandler = Handler[CheckoutInput, BillingURLResponseOutput]

type ListBillingContractsHandler = Handler[EmptyInput, ListBillingContractsOutput]

type CreateBillingContractHandler = Handler[CreateBillingContractInput, BillingURLResponseOutput]

type CancelBillingContractHandler = Handler[ContractMutationInput, BillingCancelContractResponseOutput]

type CreateBillingContractChangeHandler = Handler[CreateBillingContractChangeInput, CreateBillingContractChangeOutput]

type GetBillingEntitlementsHandler = Handler[EmptyInput, GetBillingEntitlementsOutput]

type ListBillingGrantsHandler = Handler[GrantsQueryInput, ListBillingGrantsOutput]

type ListBillingPlansHandler = Handler[ProductQueryInput, ListBillingPlansOutput]

type CreateBillingPortalHandler = Handler[CreateBillingPortalInput, BillingURLResponseOutput]

type GetBillingStatementHandler = Handler[ProductQueryInput, GetBillingStatementOutput]
