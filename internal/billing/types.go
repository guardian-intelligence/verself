package billing

import (
	"fmt"
	"time"
)

type OrgAcctType uint16

const (
	AcctGrant OrgAcctType = 9
)

type OperatorAcctType uint16

const (
	AcctRevenue         OperatorAcctType = 3
	AcctFreeTierPool    OperatorAcctType = 4
	AcctStripeHolding   OperatorAcctType = 5
	AcctPromoPool       OperatorAcctType = 6
	AcctFreeTierExpense OperatorAcctType = 7
	AcctExpiredCredits  OperatorAcctType = 8
)

type XferKind uint8

const (
	KindReservation         XferKind = 1
	KindSettlement          XferKind = 2
	KindVoid                XferKind = 3
	KindFreeTierReset       XferKind = 4
	KindStripeDeposit       XferKind = 5
	KindSubscriptionDeposit XferKind = 6
	KindPromoCredit         XferKind = 7
	KindDisputeDebit        XferKind = 8
	KindCreditExpiry        XferKind = 9
)

type PricingPhase string

const (
	PricingPhaseFreeTier PricingPhase = "free_tier"
	PricingPhaseIncluded PricingPhase = "included"
	PricingPhaseOverage  PricingPhase = "overage"
	PricingPhaseLicensed PricingPhase = "licensed"
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

type ProductBalance struct {
	ProductID         string
	FreeTierRemaining uint64
	IncludedRemaining uint64
	PrepaidRemaining  uint64
}

type GrantLeg struct {
	GrantID    GrantID
	TransferID TransferID
	Amount     uint64
}

type Reservation struct {
	JobID        JobID
	OrgID        OrgID
	ProductID    string
	PlanID       string
	ActorID      string
	WindowSeq    uint32
	WindowSecs   uint32
	WindowStart  time.Time
	PricingPhase PricingPhase
	Allocation   map[string]float64
	UnitRates    map[string]uint64
	CostPerSec   uint64
	GrantLegs    []GrantLeg
}

type QuotaResult struct {
	Allowed    bool
	Violations []QuotaViolation
}

type QuotaViolation struct {
	Dimension string
	Window    string
	Limit     uint64
	Current   uint64
}

type ReserveRequest struct {
	JobID      JobID
	OrgID      OrgID
	ProductID  string
	ActorID    string
	Allocation map[string]float64
}

type CreditGrant struct {
	OrgID             OrgID
	ProductID         string
	Amount            uint64
	Source            string
	StripeReferenceID string
	SubscriptionID    *int64
	PeriodStart       *time.Time
	PeriodEnd         *time.Time
	ExpiresAt         *time.Time
}

type LicensedCharge struct {
	OrgID           OrgID
	ProductID       string
	SubscriptionID  int64
	StripeInvoiceID string
	Amount          uint64
	PeriodStart     time.Time
	PeriodEnd       time.Time
}

type ExpireResult struct {
	GrantsChecked int
	GrantsExpired int
	GrantsFailed  int
	UnitsExpired  uint64
	Errors        []error
}

type CancelSubscriptionRequest struct {
	SubscriptionID        int64
	Immediate             bool
	RefundAnnualProration bool
	VoidRemainingCredits  bool
}

type DepositResult struct {
	SubscriptionsProcessed int
	CreditsDeposited       int
	CreditsSkipped         int
	CreditsFailed          int
	Errors                 []error
}

type TrustTierResult struct {
	OrgPromoted int
	OrgDemoted  int
	Errors      []error
}

type CheckoutParams struct {
	AmountCents int64
	SuccessURL  string
	CancelURL   string
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
