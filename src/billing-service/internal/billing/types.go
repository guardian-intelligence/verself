package billing

import (
	"context"
	"fmt"
	"time"
)

const (
	AcctGrantCode uint16 = 9
)

type OperatorAcctType uint16

const (
	AcctRevenue       OperatorAcctType = 3
	AcctStripeHolding OperatorAcctType = 5
)

type XferKind uint8

const (
	KindReservation   XferKind = 1
	KindSettlement    XferKind = 2
	KindVoid          XferKind = 3
	KindStripeDeposit XferKind = 5
)

type (
	OrgID uint64
	JobID int64
)

type PricingPhase string

const (
	PricingPhaseIncluded PricingPhase = "included"
	PricingPhaseMetered  PricingPhase = "metered"
)

type ReservationShape string

const (
	ReservationShapeTime  ReservationShape = "time"
	ReservationShapeUnits ReservationShape = "units"
)

type GrantSourceType uint8

const (
	SourceFreeTier     GrantSourceType = 1
	SourceSubscription GrantSourceType = 2
	SourcePurchase     GrantSourceType = 3
	SourcePromo        GrantSourceType = 4
	SourceRefund       GrantSourceType = 5
)

type Statement struct {
	OrgID           OrgID
	ProductID       string
	PeriodStart     time.Time
	PeriodEnd       time.Time
	PeriodSource    string
	GeneratedAt     time.Time
	Currency        string
	UnitLabel       string
	LineItems       []StatementLineItem
	BucketSummaries []StatementBucketSummary
	GrantSummaries  []StatementGrantSummary
	Totals          StatementTotals
}

type StatementLineItem struct {
	ProductID         string
	PlanID            string
	BucketID          string
	BucketDisplayName string
	SKUID             string
	SKUDisplayName    string
	QuantityUnit      string
	PricingPhase      string
	Quantity          float64
	UnitRate          uint64
	ChargeUnits       uint64
}

type StatementBucketSummary struct {
	ProductID         string
	BucketID          string
	BucketDisplayName string
	ChargeUnits       uint64
	FreeTierUnits     uint64
	SubscriptionUnits uint64
	PurchaseUnits     uint64
	PromoUnits        uint64
	RefundUnits       uint64
	ReceivableUnits   uint64
	ReservedUnits     uint64
}

type StatementGrantSummary struct {
	ScopeType      GrantScopeType
	ScopeProductID string
	ScopeBucketID  string
	Source         GrantSourceType
	Available      uint64
	Pending        uint64
}

type StatementTotals struct {
	ChargeUnits       uint64
	FreeTierUnits     uint64
	SubscriptionUnits uint64
	PurchaseUnits     uint64
	PromoUnits        uint64
	RefundUnits       uint64
	ReceivableUnits   uint64
	ReservedUnits     uint64
	TotalDueUnits     uint64
}

type GrantBalance struct {
	GrantID             GrantID
	ScopeType           GrantScopeType
	ScopeProductID      string
	ScopeBucketID       string
	ScopeSKUID          string
	Source              GrantSourceType
	SourceReferenceID   string
	EntitlementPeriodID string
	PolicyVersion       string
	StartsAt            time.Time
	PeriodStart         *time.Time
	PeriodEnd           *time.Time
	ExpiresAt           *time.Time
	Available           uint64
	Pending             uint64
}

type ReservePolicy struct {
	Shape                 ReservationShape `json:"shape"`
	TargetQuantity        uint32           `json:"target_quantity"`
	MinQuantity           uint32           `json:"min_quantity"`
	AllowPartialReserve   bool             `json:"allow_partial_reserve"`
	RenewSlackQuantity    uint32           `json:"renew_slack_quantity"`
	OperatorGraceQuantity uint32           `json:"operator_grace_quantity"`
}

type WindowFundingLeg struct {
	GrantID             GrantID         `json:"grant_id"`
	TransferID          TransferID      `json:"transfer_id"`
	ChargeProductID     string          `json:"charge_product_id"`
	ChargeBucketID      string          `json:"charge_bucket_id"`
	ChargeSKUID         string          `json:"charge_sku_id,omitempty"`
	Amount              uint64          `json:"amount"`
	Source              GrantSourceType `json:"source"`
	GrantScopeType      GrantScopeType  `json:"grant_scope_type"`
	GrantScopeProductID string          `json:"grant_scope_product_id"`
	GrantScopeBucketID  string          `json:"grant_scope_bucket_id"`
	GrantScopeSKUID     string          `json:"grant_scope_sku_id,omitempty"`
}

type WindowReservation struct {
	WindowID            string             `json:"window_id"`
	OrgID               OrgID              `json:"org_id"`
	ProductID           string             `json:"product_id"`
	PlanID              string             `json:"plan_id"`
	ActorID             string             `json:"actor_id"`
	SourceType          string             `json:"source_type"`
	SourceRef           string             `json:"source_ref"`
	WindowSeq           uint32             `json:"window_seq"`
	ReservationShape    ReservationShape   `json:"reservation_shape"`
	ReservedQuantity    uint32             `json:"reserved_quantity"`
	ReservedChargeUnits uint64             `json:"reserved_charge_units"`
	PricingPhase        PricingPhase       `json:"pricing_phase"`
	Allocation          map[string]float64 `json:"allocation"`
	SKURates            map[string]uint64  `json:"sku_rates"`
	CostPerUnit         uint64             `json:"cost_per_unit"`
	WindowStart         time.Time          `json:"window_start"`
	ActivatedAt         *time.Time         `json:"activated_at,omitempty"`
	ExpiresAt           time.Time          `json:"expires_at"`
	RenewBy             *time.Time         `json:"renew_by,omitempty"`
}

type ReserveRequest struct {
	OrgID           OrgID
	ProductID       string
	ActorID         string
	Allocation      map[string]float64
	ConcurrentCount uint64
	SourceType      string
	SourceRef       string
}

type SettleResult struct {
	WindowID            string    `json:"window_id"`
	ActualQuantity      uint32    `json:"actual_quantity"`
	BillableQuantity    uint32    `json:"billable_quantity"`
	WriteoffQuantity    uint32    `json:"writeoff_quantity"`
	BilledChargeUnits   uint64    `json:"billed_charge_units"`
	WriteoffChargeUnits uint64    `json:"writeoff_charge_units"`
	SettledAt           time.Time `json:"settled_at"`
}

type MeteringRow struct {
	WindowID                string             `ch:"window_id"`
	OrgID                   string             `ch:"org_id"`
	ActorID                 string             `ch:"actor_id"`
	ProductID               string             `ch:"product_id"`
	SourceType              string             `ch:"source_type"`
	SourceRef               string             `ch:"source_ref"`
	WindowSeq               uint32             `ch:"window_seq"`
	ReservationShape        string             `ch:"reservation_shape"`
	StartedAt               time.Time          `ch:"started_at"`
	EndedAt                 time.Time          `ch:"ended_at"`
	ReservedQuantity        uint64             `ch:"reserved_quantity"`
	ActualQuantity          uint64             `ch:"actual_quantity"`
	BillableQuantity        uint64             `ch:"billable_quantity"`
	WriteoffQuantity        uint64             `ch:"writeoff_quantity"`
	PricingPhase            string             `ch:"pricing_phase"`
	Dimensions              map[string]float64 `ch:"dimensions"`
	ComponentQuantities     map[string]float64 `ch:"component_quantities"`
	ComponentChargeUnits    map[string]uint64  `ch:"component_charge_units"`
	BucketChargeUnits       map[string]uint64  `ch:"bucket_charge_units"`
	ChargeUnits             uint64             `ch:"charge_units"`
	WriteoffChargeUnits     uint64             `ch:"writeoff_charge_units"`
	FreeTierUnits           uint64             `ch:"free_tier_units"`
	SubscriptionUnits       uint64             `ch:"subscription_units"`
	PurchaseUnits           uint64             `ch:"purchase_units"`
	PromoUnits              uint64             `ch:"promo_units"`
	RefundUnits             uint64             `ch:"refund_units"`
	ReceivableUnits         uint64             `ch:"receivable_units"`
	BucketFreeTierUnits     map[string]uint64  `ch:"bucket_free_tier_units"`
	BucketSubscriptionUnits map[string]uint64  `ch:"bucket_subscription_units"`
	BucketPurchaseUnits     map[string]uint64  `ch:"bucket_purchase_units"`
	BucketPromoUnits        map[string]uint64  `ch:"bucket_promo_units"`
	BucketRefundUnits       map[string]uint64  `ch:"bucket_refund_units"`
	BucketReceivableUnits   map[string]uint64  `ch:"bucket_receivable_units"`
	UsageEvidence           map[string]uint64  `ch:"usage_evidence"`
	PlanID                  string             `ch:"plan_id"`
	CostPerUnit             uint64             `ch:"cost_per_unit"`
	RecordedAt              time.Time          `ch:"recorded_at"`
	TraceID                 string             `ch:"trace_id"`
}

type MeteringWriter interface {
	InsertMeteringRow(ctx context.Context, row MeteringRow) error
}

type CreditGrant struct {
	OrgID               OrgID
	ScopeType           GrantScopeType
	ScopeProductID      string
	ScopeBucketID       string
	ScopeSKUID          string
	Amount              uint64
	Source              string
	SourceReferenceID   string
	EntitlementPeriodID string
	PolicyVersion       string
	StartsAt            *time.Time
	PeriodStart         *time.Time
	PeriodEnd           *time.Time
	ExpiresAt           *time.Time
}

type CheckoutParams struct {
	AmountCents int64
	SuccessURL  string
	CancelURL   string
}

type BillingCadence string

const (
	CadenceMonthly BillingCadence = "monthly"
	CadenceAnnual  BillingCadence = "annual"
)

type SubscriptionRecord struct {
	SubscriptionID     int64
	ContractID         string
	OrgID              string
	ProductID          string
	PlanID             string
	Cadence            string
	Status             string
	PaymentState       EntitlementPaymentState
	EntitlementState   EntitlementState
	CurrentPeriodStart *time.Time
	CurrentPeriodEnd   *time.Time
}

type PlanRecord struct {
	PlanID             string
	ProductID          string
	DisplayName        string
	BillingMode        string
	Tier               string
	Currency           string
	MonthlyAmountCents uint64
	AnnualAmountCents  uint64
	Active             bool
	IsDefault          bool
}

type SubscriptionProviderEvent struct {
	Provider                  string
	EventType                 string
	OrgID                     OrgID
	ProductID                 string
	PlanID                    string
	Cadence                   string
	Status                    string
	ProviderSubscriptionID    string
	ProviderCheckoutSessionID string
	ProviderCustomerID        string
	CurrentPeriodStart        *time.Time
	CurrentPeriodEnd          *time.Time
	PaymentState              EntitlementPaymentState
	EntitlementState          EntitlementState
}

type EntitlementCadence string

const (
	EntitlementCadenceMonthly EntitlementCadence = "monthly"
	EntitlementCadenceAnnual  EntitlementCadence = "annual"
)

type EntitlementAnchorKind string

const (
	AnchorCalendarMonth      EntitlementAnchorKind = "calendar_month"
	AnchorSubscriptionPeriod EntitlementAnchorKind = "subscription_period"
)

type EntitlementProrationMode string

const (
	ProrationNone       EntitlementProrationMode = "none"
	ProrationByTimeLeft EntitlementProrationMode = "prorate_by_time_left"
)

type EntitlementPaymentState string

const (
	PaymentNotRequired   EntitlementPaymentState = "not_required"
	PaymentPending       EntitlementPaymentState = "pending"
	PaymentPaid          EntitlementPaymentState = "paid"
	PaymentFailed        EntitlementPaymentState = "failed"
	PaymentUncollectible EntitlementPaymentState = "uncollectible"
	PaymentRefunded      EntitlementPaymentState = "refunded"
)

type EntitlementState string

const (
	EntitlementScheduled EntitlementState = "scheduled"
	EntitlementActive    EntitlementState = "active"
	EntitlementGrace     EntitlementState = "grace"
	EntitlementClosed    EntitlementState = "closed"
	EntitlementVoided    EntitlementState = "voided"
)

type EntitlementPolicy struct {
	PolicyID       string
	Source         GrantSourceType
	ProductID      string
	ScopeType      GrantScopeType
	ScopeProductID string
	ScopeBucketID  string
	ScopeSKUID     string
	AmountUnits    uint64
	Cadence        EntitlementCadence
	AnchorKind     EntitlementAnchorKind
	ProrationMode  EntitlementProrationMode
	PolicyVersion  string
	ActiveFrom     time.Time
	ActiveUntil    *time.Time
}

type EntitlementPeriod struct {
	PeriodID          string
	OrgID             OrgID
	ProductID         string
	Source            GrantSourceType
	PolicyID          string
	ContractID        string
	ScopeType         GrantScopeType
	ScopeProductID    string
	ScopeBucketID     string
	ScopeSKUID        string
	AmountUnits       uint64
	PeriodStart       time.Time
	PeriodEnd         time.Time
	PolicyVersion     string
	PaymentState      EntitlementPaymentState
	EntitlementState  EntitlementState
	SourceReferenceID string
	CreatedReason     string
}

type BillingEvent struct {
	EventID       string    `ch:"event_id"`
	EventType     string    `ch:"event_type"`
	AggregateType string    `ch:"aggregate_type"`
	AggregateID   string    `ch:"aggregate_id"`
	OrgID         string    `ch:"org_id"`
	ProductID     string    `ch:"product_id"`
	OccurredAt    time.Time `ch:"occurred_at"`
	Payload       string    `ch:"payload"`
	RecordedAt    time.Time `ch:"recorded_at"`
}

func ParseGrantSourceType(source string) (GrantSourceType, error) {
	switch source {
	case "free_tier":
		return SourceFreeTier, nil
	case "subscription":
		return SourceSubscription, nil
	case "purchase":
		return SourcePurchase, nil
	case "promo":
		return SourcePromo, nil
	case "refund":
		return SourceRefund, nil
	default:
		return 0, fmt.Errorf("unknown grant source %q", source)
	}
}

func (t GrantSourceType) String() string {
	switch t {
	case SourceFreeTier:
		return "free_tier"
	case SourceSubscription:
		return "subscription"
	case SourcePurchase:
		return "purchase"
	case SourcePromo:
		return "promo"
	case SourceRefund:
		return "refund"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}

func (t GrantSourceType) IsFreeTier() bool {
	return t == SourceFreeTier
}

// GrantSourceLabel is the customer-facing label for a grant source. The
// entitlements view exposes both the raw enum and this label so the frontend
// never has to translate.
func GrantSourceLabel(source GrantSourceType) string {
	switch source {
	case SourceFreeTier:
		return "Free tier"
	case SourceSubscription:
		return "Plan"
	case SourcePurchase:
		return "Top-up"
	case SourcePromo:
		return "Promo"
	case SourceRefund:
		return "Refund"
	default:
		return source.String()
	}
}
