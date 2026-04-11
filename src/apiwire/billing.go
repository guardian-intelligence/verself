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
	GrantID   string        `json:"grant_id"`
	Source    string        `json:"source"`
	Available DecimalUint64 `json:"available"`
	Pending   DecimalUint64 `json:"pending"`
	ExpiresAt *time.Time    `json:"expires_at,omitempty"`
}

type BillingGrants struct {
	Grants []BillingGrant `json:"grants"`
}

type BillingSubscription struct {
	SubscriptionID     DecimalInt64 `json:"subscription_id"`
	ProductID          string       `json:"product_id"`
	PlanID             string       `json:"plan_id"`
	Cadence            string       `json:"cadence"`
	Status             string       `json:"status"`
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
	UnitRates           map[string]DecimalUint64 `json:"unit_rates"`
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
