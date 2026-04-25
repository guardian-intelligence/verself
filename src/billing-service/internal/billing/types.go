package billing

import (
	"errors"
	"time"

	"github.com/verself/billing-service/internal/billing/ledger"
)

type OrgID uint64

type Config struct {
	StripeSecretKey           string
	EventDeliveryProjectEvery time.Duration
	EntitlementReconcileEvery time.Duration
	LedgerDispatchEvery       time.Duration
	LedgerReconcileEvery      time.Duration
	PendingTimeout            time.Duration
	UseStripe                 bool
}

func DefaultConfig() Config {
	return Config{
		EventDeliveryProjectEvery: time.Second,
		EntitlementReconcileEvery: time.Hour,
		LedgerDispatchEvery:       time.Second,
		LedgerReconcileEvery:      time.Minute,
		PendingTimeout:            time.Hour,
		UseStripe:                 true,
	}
}

var (
	ErrInvalidConfig        = errors.New("billing: invalid config")
	ErrPaymentRequired      = errors.New("billing: payment required")
	ErrForbidden            = errors.New("billing: forbidden")
	ErrContractNotFound     = errors.New("billing: contract not found")
	ErrNoStripeCustomer     = errors.New("billing: no stripe customer")
	ErrUnsupportedChange    = errors.New("billing: unsupported contract change")
	ErrUnsupportedCadence   = errors.New("billing: unsupported cadence")
	ErrInsufficientBalance  = errors.New("billing: insufficient balance")
	ErrOrgSuspended         = errors.New("billing: org suspended")
	ErrWindowNotFound       = errors.New("billing: window not found")
	ErrWindowNotReserved    = errors.New("billing: window not reserved")
	ErrWindowNotActivated   = errors.New("billing: window not activated")
	ErrWindowSourceConflict = errors.New("billing: window source conflict")
	ErrWindowAlreadySettled = errors.New("billing: window already settled")
	ErrWindowAlreadyVoided  = errors.New("billing: window already voided")
)

const (
	ReservationShapeTime  = "time"
	ReservationShapeCount = "count"
)

type CheckoutParams struct {
	AmountCents int64
	SuccessURL  string
	CancelURL   string
}

type ContractChangeRequest struct {
	TargetPlanID string
	SuccessURL   string
	CancelURL    string
}

type ContractChangeResult struct {
	URL             string
	ChangeID        string
	FinalizationID  string
	DocumentID      string
	Status          string
	PriceDeltaUnits uint64
}

type DueWorkSummary struct {
	CyclesRolledOver       uint64
	ContractChangesApplied uint64
	EntitlementsEnsured    uint64
}

type BusinessClockState struct {
	OrgID       OrgID
	ProductID   string
	ScopeKind   string
	ScopeID     string
	BusinessNow time.Time
	HasOverride bool
	Generation  uint64
	DueWork     DueWorkSummary
	Repair      BusinessClockRepairSummary
}

type BusinessClockRepairSummary struct {
	PreviousBusinessNow *time.Time
	VoidedCycleIDs      []string
	ClosedGrantIDs      []string
	ReassignedWindowIDs []string
	CurrentCycleID      string
}

type BillingCadence string

const CadenceMonthly BillingCadence = "monthly"

type WindowReservation struct {
	WindowID            string             `json:"window_id"`
	OrgID               OrgID              `json:"org_id"`
	ProductID           string             `json:"product_id"`
	PlanID              string             `json:"plan_id"`
	ActorID             string             `json:"actor_id"`
	SourceType          string             `json:"source_type"`
	SourceRef           string             `json:"source_ref"`
	WindowSeq           uint32             `json:"window_seq"`
	ReservationShape    string             `json:"reservation_shape"`
	ReservedQuantity    uint32             `json:"reserved_quantity"`
	ReservedChargeUnits uint64             `json:"reserved_charge_units"`
	PricingPhase        string             `json:"pricing_phase"`
	Allocation          map[string]float64 `json:"allocation"`
	SKURates            map[string]uint64  `json:"sku_rates"`
	CostPerUnit         uint64             `json:"cost_per_unit"`
	WindowStart         time.Time          `json:"window_start"`
	ActivatedAt         *time.Time         `json:"activated_at,omitempty"`
	ExpiresAt           time.Time          `json:"expires_at"`
	RenewBy             *time.Time         `json:"renew_by,omitempty"`
}

type ReserveRequest struct {
	OrgID            OrgID
	ProductID        string
	ActorID          string
	ConcurrentCount  uint64
	SourceType       string
	SourceRef        string
	WindowSeq        uint32
	ReservationShape string
	ReservedQuantity uint32
	Allocation       map[string]float64
	BillingJobID     int64
}

type SettleResult struct {
	WindowID            string
	ActualQuantity      uint32
	BillableQuantity    uint32
	WriteoffQuantity    uint32
	BilledChargeUnits   uint64
	WriteoffChargeUnits uint64
	SettledAt           time.Time
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

type ContractRecord struct {
	ContractID                string
	ProductID                 string
	PlanID                    string
	PhaseID                   string
	CadenceKind               string
	Status                    string
	PaymentState              string
	EntitlementState          string
	PendingChangeID           string
	PendingChangeType         string
	PendingChangeTargetPlanID string
	PendingChangeEffectiveAt  *time.Time
	StartsAt                  time.Time
	EndsAt                    *time.Time
	PhaseStart                *time.Time
	PhaseEnd                  *time.Time
}

type DocumentRecord struct {
	DocumentID             string
	DocumentNumber         string
	DocumentKind           string
	FinalizationID         string
	ProductID              string
	CycleID                string
	Status                 string
	PaymentStatus          string
	PeriodStart            time.Time
	PeriodEnd              time.Time
	IssuedAt               *time.Time
	Currency               string
	SubtotalUnits          uint64
	AdjustmentUnits        int64
	TaxUnits               uint64
	TotalDueUnits          uint64
	StripeHostedInvoiceURL string
	StripeInvoicePDFURL    string
	StripePaymentIntentID  string
}

type GrantBalance struct {
	OrgID               OrgID
	GrantID             string
	ScopeType           string
	ScopeProductID      string
	ScopeBucketID       string
	ScopeSKUID          string
	Source              string
	SourceReferenceID   string
	EntitlementPeriodID string
	PolicyVersion       string
	PlanID              string
	PlanTier            string
	PlanDisplayName     string
	StartsAt            time.Time
	PeriodStart         *time.Time
	PeriodEnd           *time.Time
	ExpiresAt           *time.Time
	OriginalAmount      uint64
	Amount              uint64
	Available           uint64
	Pending             uint64
	Spent               uint64
	ledgerAccountID     ledger.ID
}

type Statement struct {
	OrgID          OrgID
	ProductID      string
	PeriodStart    time.Time
	PeriodEnd      time.Time
	PeriodSource   string
	GeneratedAt    time.Time
	Currency       string
	UnitLabel      string
	LineItems      []StatementLineItem
	GrantSummaries []StatementGrantSummary
	Totals         StatementTotals
}

type StatementLineItem struct {
	ProductID         string
	PlanID            string
	BucketID          string
	BucketOrder       int
	BucketDisplayName string
	SKUID             string
	SKUDisplayName    string
	QuantityUnit      string
	PricingPhase      string
	Quantity          float64
	UnitRate          uint64
	ChargeUnits       uint64
	FreeTierUnits     uint64
	ContractUnits     uint64
	PurchaseUnits     uint64
	PromoUnits        uint64
	RefundUnits       uint64
	ReceivableUnits   uint64
	ReservedUnits     uint64
}

type StatementGrantSummary struct {
	ScopeType      string
	ScopeProductID string
	ScopeBucketID  string
	Source         string
	Available      uint64
	Pending        uint64
}

type StatementTotals struct {
	ChargeUnits     uint64
	FreeTierUnits   uint64
	ContractUnits   uint64
	PurchaseUnits   uint64
	PromoUnits      uint64
	RefundUnits     uint64
	ReceivableUnits uint64
	ReservedUnits   uint64
	TotalDueUnits   uint64
}
