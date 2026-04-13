package apiwire

import "time"

const MaxSafeInteger = 9007199254740991

type OrgID = DecimalUint64

type BillingGrant struct {
	GrantID             string        `json:"grant_id"`
	ScopeType           string        `json:"scope_type"`
	ScopeProductID      string        `json:"scope_product_id"`
	ScopeBucketID       string        `json:"scope_bucket_id"`
	ScopeSKUID          string        `json:"scope_sku_id"`
	Source              string        `json:"source"`
	SourceReferenceID   string        `json:"source_reference_id"`
	EntitlementPeriodID string        `json:"entitlement_period_id"`
	PolicyVersion       string        `json:"policy_version"`
	StartsAt            time.Time     `json:"starts_at"`
	PeriodStart         *time.Time    `json:"period_start,omitempty"`
	PeriodEnd           *time.Time    `json:"period_end,omitempty"`
	Available           DecimalUint64 `json:"available"`
	Pending             DecimalUint64 `json:"pending"`
	ExpiresAt           *time.Time    `json:"expires_at,omitempty"`
}

type BillingGrants struct {
	Grants []BillingGrant `json:"grants"`
}

type BillingStatement struct {
	OrgID          OrgID                          `json:"org_id"`
	ProductID      string                         `json:"product_id"`
	PeriodStart    time.Time                      `json:"period_start"`
	PeriodEnd      time.Time                      `json:"period_end"`
	PeriodSource   string                         `json:"period_source"`
	GeneratedAt    time.Time                      `json:"generated_at"`
	Currency       string                         `json:"currency"`
	UnitLabel      string                         `json:"unit_label"`
	LineItems      []BillingStatementLineItem     `json:"line_items"`
	GrantSummaries []BillingStatementGrantSummary `json:"grant_summaries"`
	Totals         BillingStatementTotals         `json:"totals"`
}

// BillingStatementLineItem carries per-line drain attribution: each row is
// one (plan, bucket, sku, pricing_phase, unit_rate) and the Applied*Units
// fields break down which sources funded ChargeUnits. The customer-facing
// invoice renders this as a receipt-style breakdown without a secondary
// bucket-summary table.
type BillingStatementLineItem struct {
	ProductID         string        `json:"product_id"`
	PlanID            string        `json:"plan_id"`
	BucketID          string        `json:"bucket_id"`
	BucketDisplayName string        `json:"bucket_display_name"`
	SKUID             string        `json:"sku_id"`
	SKUDisplayName    string        `json:"sku_display_name"`
	QuantityUnit      string        `json:"quantity_unit"`
	PricingPhase      string        `json:"pricing_phase"`
	Quantity          float64       `json:"quantity"`
	UnitRate          DecimalUint64 `json:"unit_rate"`
	ChargeUnits       DecimalUint64 `json:"charge_units"`
	FreeTierUnits     DecimalUint64 `json:"free_tier_units"`
	ContractUnits     DecimalUint64 `json:"contract_units"`
	PurchaseUnits     DecimalUint64 `json:"purchase_units"`
	PromoUnits        DecimalUint64 `json:"promo_units"`
	RefundUnits       DecimalUint64 `json:"refund_units"`
	ReceivableUnits   DecimalUint64 `json:"receivable_units"`
	ReservedUnits     DecimalUint64 `json:"reserved_units"`
}

type BillingStatementGrantSummary struct {
	ScopeType      string        `json:"scope_type"`
	ScopeProductID string        `json:"scope_product_id"`
	ScopeBucketID  string        `json:"scope_bucket_id"`
	Source         string        `json:"source"`
	Available      DecimalUint64 `json:"available"`
	Pending        DecimalUint64 `json:"pending"`
}

type BillingStatementTotals struct {
	ChargeUnits     DecimalUint64 `json:"charge_units"`
	FreeTierUnits   DecimalUint64 `json:"free_tier_units"`
	ContractUnits   DecimalUint64 `json:"contract_units"`
	PurchaseUnits   DecimalUint64 `json:"purchase_units"`
	PromoUnits      DecimalUint64 `json:"promo_units"`
	RefundUnits     DecimalUint64 `json:"refund_units"`
	ReceivableUnits DecimalUint64 `json:"receivable_units"`
	ReservedUnits   DecimalUint64 `json:"reserved_units"`
	TotalDueUnits   DecimalUint64 `json:"total_due_units"`
}

type BillingContract struct {
	ContractID       string     `json:"contract_id"`
	ProductID        string     `json:"product_id"`
	PlanID           string     `json:"plan_id"`
	PhaseID          string     `json:"phase_id"`
	CadenceKind      string     `json:"cadence_kind"`
	Status           string     `json:"status"`
	PaymentState     string     `json:"payment_state"`
	EntitlementState string     `json:"entitlement_state"`
	StartsAt         time.Time  `json:"starts_at"`
	EndsAt           *time.Time `json:"ends_at,omitempty"`
	PhaseStart       *time.Time `json:"phase_start,omitempty"`
	PhaseEnd         *time.Time `json:"phase_end,omitempty"`
}

type BillingContracts struct {
	Contracts []BillingContract `json:"contracts"`
}

type BillingPlan struct {
	PlanID             string        `json:"plan_id"`
	ProductID          string        `json:"product_id"`
	DisplayName        string        `json:"display_name"`
	BillingMode        string        `json:"billing_mode"`
	Tier               string        `json:"tier"`
	Currency           string        `json:"currency"`
	MonthlyAmountCents DecimalUint64 `json:"monthly_amount_cents"`
	AnnualAmountCents  DecimalUint64 `json:"annual_amount_cents"`
	Active             bool          `json:"active"`
	IsDefault          bool          `json:"is_default"`
}

type BillingPlans struct {
	Plans []BillingPlan `json:"plans"`
}

// BillingEntitlementsView is the slot-keyed customer-facing view of an org's
// open credit. The customer reads this top-to-bottom (account → product →
// bucket → sku), which is the inverse of the funder's most-specific-first
// consumption order. That inversion is intentional: rows answer "what coverage
// do I have," not "what drains first." See billing-service entitlements_view.go.
type BillingEntitlementsView struct {
	OrgID     OrgID                              `json:"org_id"`
	Universal BillingEntitlementSlot             `json:"universal"`
	Products  []BillingEntitlementProductSection `json:"products"`
}

type BillingEntitlementProductSection struct {
	ProductID   string                            `json:"product_id"`
	DisplayName string                            `json:"display_name"`
	ProductSlot *BillingEntitlementSlot           `json:"product_slot,omitempty"`
	Buckets     []BillingEntitlementBucketSection `json:"buckets"`
}

type BillingEntitlementBucketSection struct {
	BucketID    string                   `json:"bucket_id"`
	DisplayName string                   `json:"display_name"`
	BucketSlot  *BillingEntitlementSlot  `json:"bucket_slot,omitempty"`
	SKUSlots    []BillingEntitlementSlot `json:"sku_slots"`
}

// BillingEntitlementSlot is one row in the customer's entitlements table. The
// row totals never sum across slots — every slot answers a different coverage
// question. The Sources slice is the breakdown that lives inside the
// Period-started-with and Available cells.
type BillingEntitlementSlot struct {
	ScopeType        string                          `json:"scope_type" enum:"account,product,bucket,sku"`
	ProductID        string                          `json:"product_id"`
	ProductDisplay   string                          `json:"product_display"`
	BucketID         string                          `json:"bucket_id"`
	BucketDisplay    string                          `json:"bucket_display"`
	SKUID            string                          `json:"sku_id"`
	SKUDisplay       string                          `json:"sku_display"`
	CoverageLabel    string                          `json:"coverage_label"`
	PeriodStartUnits DecimalUint64                   `json:"period_start_units"`
	SpentUnits       DecimalUint64                   `json:"spent_units"`
	PendingUnits     DecimalUint64                   `json:"pending_units"`
	AvailableUnits   DecimalUint64                   `json:"available_units"`
	Sources          []BillingEntitlementSourceTotal `json:"sources"`
}

// BillingEntitlementSourceTotal is one entry in a slot's Sources slice, keyed
// by (source, plan_id). For non-contract sources plan_id is empty, so they
// collapse to one entry per source. Multi-plan customers get one entry per
// active plan. PeriodStartUnits is non-zero only for grants that have a period
// containing now; ad-hoc top-ups and promos contribute zero there. Inline
// expiry is surfaced only when at least one grant inside this entry is
// non-period (period-bound expiries are implicit in the period boundary).
type BillingEntitlementSourceTotal struct {
	Source           string        `json:"source" enum:"free_tier,contract,purchase,promo,refund,receivable"`
	PlanID           string        `json:"plan_id"`
	Label            string        `json:"label"`
	PeriodStartUnits DecimalUint64 `json:"period_start_units"`
	AvailableUnits   DecimalUint64 `json:"available_units"`
	InlineExpiresAt  *time.Time    `json:"inline_expires_at,omitempty"`
}

type BillingCreateCheckoutRequest struct {
	OrgID       OrgID  `json:"org_id"`
	ProductID   string `json:"product_id" minLength:"1" maxLength:"255"`
	AmountCents int64  `json:"amount_cents" minimum:"1" maximum:"9007199254740991"`
	SuccessURL  string `json:"success_url" minLength:"1" maxLength:"2048"`
	CancelURL   string `json:"cancel_url" minLength:"1" maxLength:"2048"`
}

type BillingCreateContractRequest struct {
	OrgID      OrgID  `json:"org_id"`
	PlanID     string `json:"plan_id" minLength:"1" maxLength:"255"`
	Cadence    string `json:"cadence,omitempty" enum:"monthly"`
	SuccessURL string `json:"success_url" minLength:"1" maxLength:"2048"`
	CancelURL  string `json:"cancel_url" minLength:"1" maxLength:"2048"`
}

type BillingCreatePortalSessionRequest struct {
	OrgID     OrgID  `json:"org_id"`
	ReturnURL string `json:"return_url" minLength:"1" maxLength:"2048"`
}

type BillingCancelContractRequest struct {
	OrgID OrgID `json:"org_id"`
}

type BillingCancelContractResponse struct {
	Contract BillingContract `json:"contract"`
}

type BillingURLResponse struct {
	URL string `json:"url"`
}

type BillingReserveWindowRequest struct {
	OrgID           OrgID              `json:"org_id"`
	ProductID       string             `json:"product_id" minLength:"1" maxLength:"255"`
	ActorID         string             `json:"actor_id" minLength:"1" maxLength:"255"`
	ConcurrentCount uint64             `json:"concurrent_count" minimum:"0" maximum:"9007199254740991"`
	SourceType      string             `json:"source_type" minLength:"1" maxLength:"255"`
	SourceRef       string             `json:"source_ref" minLength:"1" maxLength:"255"`
	WindowSeq       uint32             `json:"window_seq" minimum:"0" maximum:"2147483647"`
	Allocation      map[string]float64 `json:"allocation" minProperties:"1"`
}

type BillingReserveWindowResult struct {
	Reservation BillingWindowReservation `json:"reservation"`
}

type BillingActivateWindowRequest struct {
	WindowID    string    `json:"window_id" minLength:"1" maxLength:"255"`
	ActivatedAt time.Time `json:"activated_at"`
}

type BillingActivateWindowResult struct {
	Reservation BillingWindowReservation `json:"reservation"`
}

type BillingWindowReservation struct {
	WindowID            string                   `json:"window_id"`
	OrgID               OrgID                    `json:"org_id"`
	ProductID           string                   `json:"product_id"`
	PlanID              string                   `json:"plan_id"`
	ActorID             string                   `json:"actor_id"`
	SourceType          string                   `json:"source_type"`
	SourceRef           string                   `json:"source_ref"`
	WindowSeq           uint32                   `json:"window_seq" minimum:"0" maximum:"2147483647"`
	ReservationShape    string                   `json:"reservation_shape"`
	ReservedQuantity    uint32                   `json:"reserved_quantity" minimum:"0" maximum:"4294967295"`
	ReservedChargeUnits DecimalUint64            `json:"reserved_charge_units"`
	PricingPhase        string                   `json:"pricing_phase"`
	Allocation          map[string]float64       `json:"allocation"`
	SKURates            map[string]DecimalUint64 `json:"sku_rates"`
	CostPerUnit         DecimalUint64            `json:"cost_per_unit"`
	WindowStart         time.Time                `json:"window_start"`
	ActivatedAt         *time.Time               `json:"activated_at,omitempty"`
	ExpiresAt           time.Time                `json:"expires_at"`
	RenewBy             *time.Time               `json:"renew_by,omitempty"`
}

type BillingSettleWindowRequest struct {
	WindowID       string         `json:"window_id" minLength:"1" maxLength:"255"`
	ActualQuantity uint32         `json:"actual_quantity" minimum:"0" maximum:"4294967295"`
	UsageSummary   map[string]any `json:"usage_summary,omitempty"`
}

type BillingSettleResult struct {
	WindowID            string        `json:"window_id"`
	ActualQuantity      uint32        `json:"actual_quantity" minimum:"0" maximum:"4294967295"`
	BillableQuantity    uint32        `json:"billable_quantity" minimum:"0" maximum:"4294967295"`
	WriteoffQuantity    uint32        `json:"writeoff_quantity" minimum:"0" maximum:"4294967295"`
	BilledChargeUnits   DecimalUint64 `json:"billed_charge_units"`
	WriteoffChargeUnits DecimalUint64 `json:"writeoff_charge_units"`
	SettledAt           time.Time     `json:"settled_at"`
}

type BillingVoidWindowRequest struct {
	WindowID string `json:"window_id" minLength:"1" maxLength:"255"`
}

type BillingVoidWindowResult struct {
	WindowID string `json:"window_id"`
}
