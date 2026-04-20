package billingclient

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/forge-metal/apiwire"
)

var (
	ErrPaymentRequired  = errors.New("billing-client: payment required")
	ErrForbidden        = errors.New("billing-client: forbidden")
	ErrNoStripeCustomer = errors.New("billing-client: no stripe customer")
	ErrContractNotFound = errors.New("billing-client: contract not found")
	ErrUnexpected       = errors.New("billing-client: unexpected response")
)

const problemTypeNoStripeCustomer = "urn:forge-metal:problem:billing:no-stripe-customer"

type RequestEditorFn func(ctx context.Context, req *http.Request) error

const (
	ReservationShapeTime  = "time"
	ReservationShapeCount = "count"
)

type Reservation struct {
	WindowId         string
	JobId            int64
	OrgId            uint64
	ProductId        string
	PlanId           string
	ActorId          string
	SourceType       string
	SourceRef        string
	WindowSeq        uint32
	ReservationShape string
	ReservedQuantity uint32
	PricingPhase     string
	Allocation       map[string]float64
	SKURates         map[string]int64
	CostPerUnit      int64
	WindowStart      time.Time
	ActivatedAt      *time.Time
	ExpiresAt        time.Time
	RenewBy          time.Time
}

type ClientOption func(*ServiceClient) error

type ErrorModel struct {
	Schema   *string `json:"$schema,omitempty"`
	Detail   *string `json:"detail,omitempty"`
	Errors   any     `json:"errors,omitempty"`
	Instance *string `json:"instance,omitempty"`
	Status   *int64  `json:"status,omitempty"`
	Title    *string `json:"title,omitempty"`
	Type     *string `json:"type,omitempty"`
}

type HTTPError struct {
	Op         string
	StatusCode int
	Problem    *ErrorModel
	Body       []byte
	Cause      error
}

func (e *HTTPError) Error() string {
	if e == nil {
		return "billing-client: nil error"
	}
	detail := e.detail()
	if detail != "" {
		return e.Op + ": " + detail
	}
	if e.Cause != nil {
		return e.Op + ": " + e.Cause.Error()
	}
	return e.Op
}

func (e *HTTPError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *HTTPError) detail() string {
	if e == nil || e.Problem == nil || e.Problem.Detail == nil {
		return ""
	}
	return *e.Problem.Detail
}

type requestBodyDecoder interface {
	UnmarshalJSON([]byte) error
}

type billingResponse interface {
	StatusCode() int
}

type (
	BillingURLResponse                 = apiwire.BillingURLResponse
	BillingCreateCheckoutRequest       = apiwire.BillingCreateCheckoutRequest
	BillingCreateContractRequest       = apiwire.BillingCreateContractRequest
	BillingCreateContractChangeRequest = apiwire.BillingCreateContractChangeRequest
	BillingCreatePortalSessionRequest  = apiwire.BillingCreatePortalSessionRequest
	BillingCancelContractRequest       = apiwire.BillingCancelContractRequest
	BillingReserveWindowRequest        = apiwire.BillingReserveWindowRequest
	BillingActivateWindowRequest       = apiwire.BillingActivateWindowRequest
	BillingSettleWindowRequest         = apiwire.BillingSettleWindowRequest
	BillingVoidWindowRequest           = apiwire.BillingVoidWindowRequest
	BillingReserveWindowResult         = apiwire.BillingReserveWindowResult
	BillingActivateWindowResult        = apiwire.BillingActivateWindowResult
	BillingSettleResult                = apiwire.BillingSettleResult
	BillingVoidWindowResult            = apiwire.BillingVoidWindowResult
	BillingContracts                   = apiwire.BillingContracts
	BillingPlans                       = apiwire.BillingPlans
	BillingGrants                      = apiwire.BillingGrants
	BillingStatement                   = apiwire.BillingStatement
	BillingGrant                       = apiwire.BillingGrant
	BillingStatementLineItem           = apiwire.BillingStatementLineItem
	BillingStatementGrantSummary       = apiwire.BillingStatementGrantSummary
	BillingStatementTotals             = apiwire.BillingStatementTotals
	BillingPlan                        = apiwire.BillingPlan
	BillingContractChangeResponse      = apiwire.BillingContractChangeResponse
	BillingCancelContractResponse      = apiwire.BillingCancelContractResponse
	BillingContract                    = apiwire.BillingContract
	BillingEntitlementSlot             = apiwire.BillingEntitlementSlot
	BillingEntitlementSourceTotal      = apiwire.BillingEntitlementSourceTotal
	BillingEntitlementBucketSection    = apiwire.BillingEntitlementBucketSection
	BillingEntitlementProductSection   = apiwire.BillingEntitlementProductSection
	BillingEntitlementsView            = apiwire.BillingEntitlementsView
	BillingWindowReservation           = apiwire.BillingWindowReservation
)
