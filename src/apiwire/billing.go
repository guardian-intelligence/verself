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

type BillingWindowReservation struct {
	WindowID            string                   `json:"window_id"`
	OrgID               OrgID                    `json:"org_id"`
	ProductID           string                   `json:"product_id"`
	PlanID              string                   `json:"plan_id"`
	ActorID             string                   `json:"actor_id"`
	SourceType          string                   `json:"source_type"`
	SourceRef           string                   `json:"source_ref"`
	WindowSeq           uint32                   `json:"window_seq"`
	ReservationShape    string                   `json:"reservation_shape"`
	ReservedQuantity    uint32                   `json:"reserved_quantity"`
	ReservedChargeUnits DecimalUint64            `json:"reserved_charge_units"`
	PricingPhase        string                   `json:"pricing_phase"`
	Allocation          map[string]float64       `json:"allocation"`
	UnitRates           map[string]DecimalUint64 `json:"unit_rates"`
	CostPerUnit         DecimalUint64            `json:"cost_per_unit"`
	WindowStart         time.Time                `json:"window_start"`
	ExpiresAt           time.Time                `json:"expires_at"`
	RenewBy             *time.Time               `json:"renew_by,omitempty"`
}

type BillingSettleResult struct {
	WindowID            string        `json:"window_id"`
	ActualQuantity      uint32        `json:"actual_quantity"`
	BillableQuantity    uint32        `json:"billable_quantity"`
	WriteoffQuantity    uint32        `json:"writeoff_quantity"`
	BilledChargeUnits   DecimalUint64 `json:"billed_charge_units"`
	WriteoffChargeUnits DecimalUint64 `json:"writeoff_charge_units"`
	SettledAt           time.Time     `json:"settled_at"`
}
