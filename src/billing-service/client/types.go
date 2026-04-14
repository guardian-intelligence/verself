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

type Reservation struct {
	WindowId         string
	JobId            int64
	OrgId            uint64
	ProductId        string
	PlanId           string
	ActorId          string
	SourceType       string
	SourceRef        string
	WindowSeq        int32
	ReservationShape string
	WindowSecs       int32
	PricingPhase     string
	Allocation       map[string]float64
	SKURates         map[string]int64
	CostPerSec       int64
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

type BillingURLResponse = apiwire.BillingURLResponse
type BillingCreateCheckoutRequest = apiwire.BillingCreateCheckoutRequest
type BillingCreateContractRequest = apiwire.BillingCreateContractRequest
type BillingCreateContractChangeRequest = apiwire.BillingCreateContractChangeRequest
type BillingCreatePortalSessionRequest = apiwire.BillingCreatePortalSessionRequest
type BillingCancelContractRequest = apiwire.BillingCancelContractRequest
type BillingReserveWindowRequest = apiwire.BillingReserveWindowRequest
type BillingActivateWindowRequest = apiwire.BillingActivateWindowRequest
type BillingSettleWindowRequest = apiwire.BillingSettleWindowRequest
type BillingVoidWindowRequest = apiwire.BillingVoidWindowRequest
type BillingReserveWindowResult = apiwire.BillingReserveWindowResult
type BillingActivateWindowResult = apiwire.BillingActivateWindowResult
type BillingSettleResult = apiwire.BillingSettleResult
type BillingVoidWindowResult = apiwire.BillingVoidWindowResult
type BillingContracts = apiwire.BillingContracts
type BillingPlans = apiwire.BillingPlans
type BillingGrants = apiwire.BillingGrants
type BillingStatement = apiwire.BillingStatement
type BillingGrant = apiwire.BillingGrant
type BillingStatementLineItem = apiwire.BillingStatementLineItem
type BillingStatementGrantSummary = apiwire.BillingStatementGrantSummary
type BillingStatementTotals = apiwire.BillingStatementTotals
type BillingPlan = apiwire.BillingPlan
type BillingContractChangeResponse = apiwire.BillingContractChangeResponse
type BillingCancelContractResponse = apiwire.BillingCancelContractResponse
type BillingContract = apiwire.BillingContract
type BillingEntitlementSlot = apiwire.BillingEntitlementSlot
type BillingEntitlementSourceTotal = apiwire.BillingEntitlementSourceTotal
type BillingEntitlementBucketSection = apiwire.BillingEntitlementBucketSection
type BillingEntitlementProductSection = apiwire.BillingEntitlementProductSection
type BillingEntitlementsView = apiwire.BillingEntitlementsView
type BillingWindowReservation = apiwire.BillingWindowReservation
