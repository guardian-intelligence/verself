package apiwire

import "time"

const MaxSafeInteger = 9007199254740991

type OrgID = DecimalUint64

type BillingBalance struct {
	OrgID             OrgID         `json:"org_id"`
	FreeTierAvailable DecimalUint64 `json:"free_tier_available"`
	FreeTierPending   DecimalUint64 `json:"free_tier_pending"`
	CreditAvailable   DecimalUint64 `json:"credit_available"`
	CreditPending     DecimalUint64 `json:"credit_pending"`
	TotalAvailable    DecimalUint64 `json:"total_available"`
}

type BillingGrant struct {
	GrantID             string        `json:"grant_id"`
	ScopeType           string        `json:"scope_type"`
	ScopeProductID      string        `json:"scope_product_id"`
	ScopeBucketID       string        `json:"scope_bucket_id"`
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
	OrgID           OrgID                           `json:"org_id"`
	ProductID       string                          `json:"product_id"`
	PeriodStart     time.Time                       `json:"period_start"`
	PeriodEnd       time.Time                       `json:"period_end"`
	PeriodSource    string                          `json:"period_source"`
	GeneratedAt     time.Time                       `json:"generated_at"`
	Currency        string                          `json:"currency"`
	UnitLabel       string                          `json:"unit_label"`
	LineItems       []BillingStatementLineItem      `json:"line_items"`
	BucketSummaries []BillingStatementBucketSummary `json:"bucket_summaries"`
	GrantSummaries  []BillingStatementGrantSummary  `json:"grant_summaries"`
	Totals          BillingStatementTotals          `json:"totals"`
}

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
}

type BillingStatementBucketSummary struct {
	ProductID         string        `json:"product_id"`
	BucketID          string        `json:"bucket_id"`
	BucketDisplayName string        `json:"bucket_display_name"`
	ChargeUnits       DecimalUint64 `json:"charge_units"`
	FreeTierUnits     DecimalUint64 `json:"free_tier_units"`
	SubscriptionUnits DecimalUint64 `json:"subscription_units"`
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
	ChargeUnits       DecimalUint64 `json:"charge_units"`
	FreeTierUnits     DecimalUint64 `json:"free_tier_units"`
	SubscriptionUnits DecimalUint64 `json:"subscription_units"`
	PurchaseUnits     DecimalUint64 `json:"purchase_units"`
	PromoUnits        DecimalUint64 `json:"promo_units"`
	RefundUnits       DecimalUint64 `json:"refund_units"`
	ReceivableUnits   DecimalUint64 `json:"receivable_units"`
	ReservedUnits     DecimalUint64 `json:"reserved_units"`
	TotalDueUnits     DecimalUint64 `json:"total_due_units"`
}

type BillingSubscription struct {
	SubscriptionID     DecimalInt64 `json:"subscription_id"`
	ContractID         string       `json:"contract_id"`
	ProductID          string       `json:"product_id"`
	PlanID             string       `json:"plan_id"`
	Cadence            string       `json:"cadence"`
	Status             string       `json:"status"`
	PaymentState       string       `json:"payment_state"`
	EntitlementState   string       `json:"entitlement_state"`
	CurrentPeriodStart *time.Time   `json:"current_period_start,omitempty"`
	CurrentPeriodEnd   *time.Time   `json:"current_period_end,omitempty"`
}

type BillingSubscriptions struct {
	Subscriptions []BillingSubscription `json:"subscriptions"`
}

type BillingCreateCheckoutRequest struct {
	OrgID       OrgID  `json:"org_id"`
	ProductID   string `json:"product_id" minLength:"1" maxLength:"255"`
	AmountCents int64  `json:"amount_cents" minimum:"1" maximum:"9007199254740991"`
	SuccessURL  string `json:"success_url" minLength:"1" maxLength:"2048"`
	CancelURL   string `json:"cancel_url" minLength:"1" maxLength:"2048"`
}

type BillingCreateSubscriptionRequest struct {
	OrgID      OrgID  `json:"org_id"`
	PlanID     string `json:"plan_id" minLength:"1" maxLength:"255"`
	Cadence    string `json:"cadence,omitempty" enum:"monthly,annual"`
	SuccessURL string `json:"success_url" minLength:"1" maxLength:"2048"`
	CancelURL  string `json:"cancel_url" minLength:"1" maxLength:"2048"`
}

type BillingCreatePortalSessionRequest struct {
	OrgID     OrgID  `json:"org_id"`
	ReturnURL string `json:"return_url" minLength:"1" maxLength:"2048"`
}

type BillingApplySubscriptionProviderEventRequest struct {
	Provider                  string     `json:"provider" enum:"stripe"`
	EventType                 string     `json:"event_type" minLength:"1" maxLength:"255"`
	OrgID                     OrgID      `json:"org_id"`
	ProductID                 string     `json:"product_id" minLength:"1" maxLength:"255"`
	PlanID                    string     `json:"plan_id" minLength:"1" maxLength:"255"`
	Cadence                   string     `json:"cadence,omitempty" enum:"monthly,annual"`
	Status                    string     `json:"status,omitempty" maxLength:"255"`
	ProviderSubscriptionID    string     `json:"provider_subscription_id" minLength:"1" maxLength:"255"`
	ProviderCheckoutSessionID string     `json:"provider_checkout_session_id,omitempty" maxLength:"255"`
	ProviderCustomerID        string     `json:"provider_customer_id,omitempty" maxLength:"255"`
	CurrentPeriodStart        *time.Time `json:"current_period_start,omitempty"`
	CurrentPeriodEnd          *time.Time `json:"current_period_end,omitempty"`
	PaymentState              string     `json:"payment_state,omitempty" enum:"not_required,pending,paid,failed,uncollectible,refunded"`
	EntitlementState          string     `json:"entitlement_state,omitempty" enum:"scheduled,active,grace,closed,voided"`
}

type BillingApplySubscriptionProviderEventResponse struct {
	Applied bool `json:"applied"`
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
	WindowSeq           uint32                   `json:"window_seq" minimum:"0" maximum:"4294967295"`
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
