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

type Balance struct {
	FreeTierAvailable uint64
	FreeTierPending   uint64
	CreditAvailable   uint64
	CreditPending     uint64
	TotalAvailable    uint64
}

type GrantBalance struct {
	GrantID   GrantID
	Source    GrantSourceType
	ExpiresAt *time.Time
	Available uint64
	Pending   uint64
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
	GrantID    GrantID         `json:"grant_id"`
	TransferID TransferID      `json:"transfer_id"`
	Amount     uint64          `json:"amount"`
	Source     GrantSourceType `json:"source"`
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
	UnitRates           map[string]uint64  `json:"unit_rates"`
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
	WindowID            string             `ch:"window_id"`
	OrgID               string             `ch:"org_id"`
	ActorID             string             `ch:"actor_id"`
	ProductID           string             `ch:"product_id"`
	SourceType          string             `ch:"source_type"`
	SourceRef           string             `ch:"source_ref"`
	WindowSeq           uint32             `ch:"window_seq"`
	ReservationShape    string             `ch:"reservation_shape"`
	StartedAt           time.Time          `ch:"started_at"`
	EndedAt             time.Time          `ch:"ended_at"`
	ReservedQuantity    uint64             `ch:"reserved_quantity"`
	ActualQuantity      uint64             `ch:"actual_quantity"`
	BillableQuantity    uint64             `ch:"billable_quantity"`
	WriteoffQuantity    uint64             `ch:"writeoff_quantity"`
	PricingPhase        string             `ch:"pricing_phase"`
	Dimensions          map[string]float64 `ch:"dimensions"`
	ChargeUnits         uint64             `ch:"charge_units"`
	WriteoffChargeUnits uint64             `ch:"writeoff_charge_units"`
	FreeTierUnits       uint64             `ch:"free_tier_units"`
	SubscriptionUnits   uint64             `ch:"subscription_units"`
	PurchaseUnits       uint64             `ch:"purchase_units"`
	PromoUnits          uint64             `ch:"promo_units"`
	RefundUnits         uint64             `ch:"refund_units"`
	ReceivableUnits     uint64             `ch:"receivable_units"`
	PlanID              string             `ch:"plan_id"`
	CostPerUnit         uint64             `ch:"cost_per_unit"`
	RecordedAt          time.Time          `ch:"recorded_at"`
	TraceID             string             `ch:"trace_id"`
}

type MeteringWriter interface {
	InsertMeteringRow(ctx context.Context, row MeteringRow) error
}

type CreditGrant struct {
	OrgID             OrgID
	ProductID         string
	Amount            uint64
	Source            string
	StripeReferenceID string
	ExpiresAt         *time.Time
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
	OrgID              string
	ProductID          string
	PlanID             string
	Cadence            string
	Status             string
	CurrentPeriodStart *time.Time
	CurrentPeriodEnd   *time.Time
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
