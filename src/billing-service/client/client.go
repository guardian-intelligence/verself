package billingclient

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/forge-metal/apiwire"
)

var (
	ErrPaymentRequired = errors.New("billing-client: payment required")
	ErrForbidden       = errors.New("billing-client: forbidden")
	ErrUnexpected      = errors.New("billing-client: unexpected response")
)

type ServiceClient struct {
	inner ClientWithResponsesInterface
}

func New(baseURL string, opts ...ClientOption) (*ServiceClient, error) {
	inner, err := NewClientWithResponses(baseURL, opts...)
	if err != nil {
		return nil, err
	}
	return &ServiceClient{inner: inner}, nil
}

func NewFromGenerated(inner ClientWithResponsesInterface) *ServiceClient {
	return &ServiceClient{inner: inner}
}

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
	UnitRates        map[string]int64
	CostPerSec       int64
	WindowStart      time.Time
	ExpiresAt        time.Time
	RenewBy          time.Time
}

func (c *ServiceClient) GetBalance(ctx context.Context, orgID uint64, reqEditors ...RequestEditorFn) (apiwire.BillingBalance, error) {
	orgIDWire := apiwire.Uint64(orgID).String()
	resp, err := c.inner.GetBalanceWithResponse(ctx, orgIDWire, reqEditors...)
	if err != nil {
		return apiwire.BillingBalance{}, err
	}
	if resp.JSON200 != nil {
		return parseBillingBalance(*resp.JSON200)
	}
	return apiwire.BillingBalance{}, unexpected("get balance", resp.HTTPResponse, resp.ApplicationproblemJSONDefault)
}

func (c *ServiceClient) ListSubscriptions(ctx context.Context, orgID uint64, reqEditors ...RequestEditorFn) (apiwire.BillingSubscriptions, error) {
	orgIDWire := apiwire.Uint64(orgID).String()
	resp, err := c.inner.ListSubscriptionsWithResponse(ctx, orgIDWire, reqEditors...)
	if err != nil {
		return apiwire.BillingSubscriptions{}, err
	}
	if resp.JSON200 != nil {
		return parseBillingSubscriptions(*resp.JSON200)
	}
	return apiwire.BillingSubscriptions{}, unexpected("list subscriptions", resp.HTTPResponse, resp.ApplicationproblemJSONDefault)
}

func (c *ServiceClient) ListGrants(ctx context.Context, orgID uint64, productID string, active bool, reqEditors ...RequestEditorFn) (apiwire.BillingGrants, error) {
	orgIDWire := apiwire.Uint64(orgID).String()
	params := &ListGrantsParams{}
	if productID != "" {
		params.ProductId = &productID
	}
	if active {
		params.Active = &active
	}
	resp, err := c.inner.ListGrantsWithResponse(ctx, orgIDWire, params, reqEditors...)
	if err != nil {
		return apiwire.BillingGrants{}, err
	}
	if resp.JSON200 != nil {
		return parseBillingGrants(*resp.JSON200)
	}
	return apiwire.BillingGrants{}, unexpected("list grants", resp.HTTPResponse, resp.ApplicationproblemJSONDefault)
}

func (c *ServiceClient) CreateCheckout(ctx context.Context, orgID uint64, productID string, amountCents int64, successURL string, cancelURL string, reqEditors ...RequestEditorFn) (string, error) {
	orgIDWire := apiwire.Uint64(orgID).String()
	resp, err := c.inner.CreateCheckoutWithResponse(ctx, CreateCheckoutJSONRequestBody{
		OrgId:       orgIDWire,
		ProductId:   productID,
		AmountCents: amountCents,
		SuccessUrl:  successURL,
		CancelUrl:   cancelURL,
	}, reqEditors...)
	if err != nil {
		return "", err
	}
	if resp.JSON200 != nil {
		return resp.JSON200.Url, nil
	}
	return "", unexpected("create checkout", resp.HTTPResponse, resp.ApplicationproblemJSONDefault)
}

func (c *ServiceClient) CreateSubscription(ctx context.Context, orgID uint64, planID string, cadence string, successURL string, cancelURL string, reqEditors ...RequestEditorFn) (string, error) {
	orgIDWire := apiwire.Uint64(orgID).String()
	body := CreateSubscriptionJSONRequestBody{
		OrgId:      orgIDWire,
		PlanId:     planID,
		SuccessUrl: successURL,
		CancelUrl:  cancelURL,
	}
	if cadence != "" {
		wireCadence := BillingCreateSubscriptionRequestCadence(cadence)
		body.Cadence = &wireCadence
	}
	resp, err := c.inner.CreateSubscriptionWithResponse(ctx, body, reqEditors...)
	if err != nil {
		return "", err
	}
	if resp.JSON200 != nil {
		return resp.JSON200.Url, nil
	}
	return "", unexpected("create subscription", resp.HTTPResponse, firstProblem(resp.ApplicationproblemJSON500, resp.ApplicationproblemJSON422))
}

func (c *ServiceClient) CreatePortalSession(ctx context.Context, orgID uint64, returnURL string, reqEditors ...RequestEditorFn) (string, error) {
	orgIDWire := apiwire.Uint64(orgID).String()
	resp, err := c.inner.CreatePortalWithResponse(ctx, CreatePortalJSONRequestBody{
		OrgId:     orgIDWire,
		ReturnUrl: returnURL,
	}, reqEditors...)
	if err != nil {
		return "", err
	}
	if resp.JSON200 != nil {
		return resp.JSON200.Url, nil
	}
	return "", unexpected("create portal session", resp.HTTPResponse, firstProblem(resp.ApplicationproblemJSON500, resp.ApplicationproblemJSON422))
}

func (c *ServiceClient) Reserve(
	ctx context.Context,
	_ int64,
	orgID uint64,
	productID string,
	actorID string,
	concurrentCount uint64,
	sourceType string,
	sourceRef string,
	allocation map[string]float64,
	reqEditors ...RequestEditorFn,
) (Reservation, error) {
	orgIDWire := apiwire.Uint64(orgID).String()
	concurrentCountWire, err := uint64ToInt64(concurrentCount, "concurrent_count")
	if err != nil {
		return Reservation{}, err
	}
	resp, err := c.inner.ReserveWindowWithResponse(ctx, ReserveWindowJSONRequestBody{
		OrgId:           orgIDWire,
		ProductId:       productID,
		ActorId:         actorID,
		ConcurrentCount: concurrentCountWire,
		SourceType:      sourceType,
		SourceRef:       sourceRef,
		Allocation:      allocation,
	}, reqEditors...)
	if err != nil {
		return Reservation{}, err
	}
	if resp.JSON200 != nil {
		return parseReservation(resp.JSON200.Reservation)
	}
	switch statusCode(resp.HTTPResponse) {
	case http.StatusPaymentRequired:
		return Reservation{}, fmt.Errorf("%w: %s", ErrPaymentRequired, detail(resp.ApplicationproblemJSON402, resp.HTTPResponse))
	case http.StatusForbidden:
		return Reservation{}, fmt.Errorf("%w: %s", ErrForbidden, detail(resp.ApplicationproblemJSON403, resp.HTTPResponse))
	case http.StatusUnprocessableEntity:
		return Reservation{}, fmt.Errorf("billing-client: reserve bad request: %s", detail(resp.ApplicationproblemJSON422, resp.HTTPResponse))
	default:
		return Reservation{}, unexpected("reserve", resp.HTTPResponse, firstProblem(resp.ApplicationproblemJSON500, resp.ApplicationproblemJSON422))
	}
}

func parseReservation(in BillingWindowReservation) (Reservation, error) {
	orgID, err := apiwire.ParseUint64(in.OrgId)
	if err != nil {
		return Reservation{}, fmt.Errorf("billing-client: reservation org_id: %w", err)
	}
	unitRates, err := parseInt64UnitRates(in.UnitRates)
	if err != nil {
		return Reservation{}, err
	}
	costPerUnit, err := parseUint64DecimalAsInt64(in.CostPerUnit, "cost_per_unit")
	if err != nil {
		return Reservation{}, err
	}
	var renewBy time.Time
	if in.RenewBy != nil {
		renewBy = in.RenewBy.UTC()
	}
	return Reservation{
		WindowId:         in.WindowId,
		OrgId:            orgID,
		ProductId:        in.ProductId,
		PlanId:           in.PlanId,
		ActorId:          in.ActorId,
		SourceType:       in.SourceType,
		SourceRef:        in.SourceRef,
		WindowSeq:        in.WindowSeq,
		ReservationShape: in.ReservationShape,
		WindowSecs:       in.ReservedQuantity,
		PricingPhase:     in.PricingPhase,
		Allocation:       in.Allocation,
		UnitRates:        unitRates,
		CostPerSec:       costPerUnit,
		WindowStart:      in.WindowStart.UTC(),
		ExpiresAt:        in.ExpiresAt.UTC(),
		RenewBy:          renewBy,
	}, nil
}

func parseBillingBalance(in BillingBalance) (apiwire.BillingBalance, error) {
	orgID, err := parseDecimalUint64(in.OrgId, "org_id")
	if err != nil {
		return apiwire.BillingBalance{}, err
	}
	freeTierAvailable, err := parseDecimalUint64(in.FreeTierAvailable, "free_tier_available")
	if err != nil {
		return apiwire.BillingBalance{}, err
	}
	freeTierPending, err := parseDecimalUint64(in.FreeTierPending, "free_tier_pending")
	if err != nil {
		return apiwire.BillingBalance{}, err
	}
	creditAvailable, err := parseDecimalUint64(in.CreditAvailable, "credit_available")
	if err != nil {
		return apiwire.BillingBalance{}, err
	}
	creditPending, err := parseDecimalUint64(in.CreditPending, "credit_pending")
	if err != nil {
		return apiwire.BillingBalance{}, err
	}
	totalAvailable, err := parseDecimalUint64(in.TotalAvailable, "total_available")
	if err != nil {
		return apiwire.BillingBalance{}, err
	}
	return apiwire.BillingBalance{
		OrgID:             orgID,
		FreeTierAvailable: freeTierAvailable,
		FreeTierPending:   freeTierPending,
		CreditAvailable:   creditAvailable,
		CreditPending:     creditPending,
		TotalAvailable:    totalAvailable,
	}, nil
}

func parseBillingGrants(in BillingGrants) (apiwire.BillingGrants, error) {
	if in.Grants == nil {
		return apiwire.BillingGrants{}, nil
	}
	out := make([]apiwire.BillingGrant, 0, len(*in.Grants))
	for _, grant := range *in.Grants {
		available, err := parseDecimalUint64(grant.Available, "available")
		if err != nil {
			return apiwire.BillingGrants{}, err
		}
		pending, err := parseDecimalUint64(grant.Pending, "pending")
		if err != nil {
			return apiwire.BillingGrants{}, err
		}
		out = append(out, apiwire.BillingGrant{
			GrantID:   grant.GrantId,
			Source:    grant.Source,
			Available: available,
			Pending:   pending,
			ExpiresAt: grant.ExpiresAt,
		})
	}
	return apiwire.BillingGrants{Grants: out}, nil
}

func parseBillingSubscriptions(in BillingSubscriptions) (apiwire.BillingSubscriptions, error) {
	if in.Subscriptions == nil {
		return apiwire.BillingSubscriptions{}, nil
	}
	out := make([]apiwire.BillingSubscription, 0, len(*in.Subscriptions))
	for _, subscription := range *in.Subscriptions {
		subscriptionID, err := parseDecimalInt64(subscription.SubscriptionId, "subscription_id")
		if err != nil {
			return apiwire.BillingSubscriptions{}, err
		}
		out = append(out, apiwire.BillingSubscription{
			SubscriptionID:     subscriptionID,
			ProductID:          subscription.ProductId,
			PlanID:             subscription.PlanId,
			Cadence:            subscription.Cadence,
			Status:             subscription.Status,
			CurrentPeriodStart: subscription.CurrentPeriodStart,
			CurrentPeriodEnd:   subscription.CurrentPeriodEnd,
		})
	}
	return apiwire.BillingSubscriptions{Subscriptions: out}, nil
}

func parseDecimalUint64(value string, field string) (apiwire.DecimalUint64, error) {
	parsed, err := apiwire.ParseUint64(value)
	if err != nil {
		return apiwire.Uint64(0), fmt.Errorf("billing-client: %s: %w", field, err)
	}
	return apiwire.Uint64(parsed), nil
}

func parseDecimalInt64(value string, field string) (apiwire.DecimalInt64, error) {
	parsed, err := apiwire.ParseInt64(value)
	if err != nil {
		return apiwire.Int64(0), fmt.Errorf("billing-client: %s: %w", field, err)
	}
	return apiwire.Int64(parsed), nil
}

func parseInt64UnitRates(in map[string]string) (map[string]int64, error) {
	if len(in) == 0 {
		return map[string]int64{}, nil
	}
	out := make(map[string]int64, len(in))
	for unit, rate := range in {
		parsed, err := parseUint64DecimalAsInt64(rate, "unit_rates."+unit)
		if err != nil {
			return nil, err
		}
		out[unit] = parsed
	}
	return out, nil
}

func parseUint64DecimalAsInt64(value string, field string) (int64, error) {
	parsed, err := apiwire.ParseUint64(value)
	if err != nil {
		return 0, fmt.Errorf("billing-client: %s: %w", field, err)
	}
	if parsed > math.MaxInt64 {
		return 0, fmt.Errorf("billing-client: %s %d exceeds internal int64 range", field, parsed)
	}
	return int64(parsed), nil
}

func (c *ServiceClient) Settle(ctx context.Context, reservation Reservation, actualQuantity uint32, reqEditors ...RequestEditorFn) error {
	actualQuantityWire, err := uint32ToInt32(actualQuantity, "actual_quantity")
	if err != nil {
		return err
	}
	resp, err := c.inner.SettleWindowWithResponse(ctx, SettleWindowJSONRequestBody{
		WindowId:       reservation.WindowId,
		ActualQuantity: actualQuantityWire,
	}, reqEditors...)
	if err != nil {
		return err
	}
	if resp.JSON200 != nil {
		return nil
	}
	if statusCode(resp.HTTPResponse) == http.StatusBadRequest {
		return fmt.Errorf("billing-client: settle bad request: %s", detail(resp.ApplicationproblemJSON400, resp.HTTPResponse))
	}
	return unexpected("settle", resp.HTTPResponse, firstProblem(resp.ApplicationproblemJSON500, resp.ApplicationproblemJSON404, resp.ApplicationproblemJSON422))
}

func (c *ServiceClient) Void(ctx context.Context, reservation Reservation, reqEditors ...RequestEditorFn) error {
	resp, err := c.inner.VoidWindowWithResponse(ctx, VoidWindowJSONRequestBody{
		WindowId: reservation.WindowId,
	}, reqEditors...)
	if err != nil {
		return err
	}
	if resp.JSON200 != nil {
		return nil
	}
	if statusCode(resp.HTTPResponse) == http.StatusBadRequest {
		return fmt.Errorf("billing-client: void bad request: %s", detail(resp.ApplicationproblemJSON400, resp.HTTPResponse))
	}
	return unexpected("void", resp.HTTPResponse, firstProblem(resp.ApplicationproblemJSON500, resp.ApplicationproblemJSON404, resp.ApplicationproblemJSON422))
}

func uint64ToInt64(value uint64, field string) (int64, error) {
	if value > math.MaxInt64 {
		return 0, fmt.Errorf("billing-client: %s %d exceeds generated OpenAPI int64 range", field, value)
	}
	return int64(value), nil
}

func uint32ToInt32(value uint32, field string) (int32, error) {
	if value > math.MaxInt32 {
		return 0, fmt.Errorf("billing-client: %s %d exceeds generated OpenAPI int32 range", field, value)
	}
	return int32(value), nil
}

func firstProblem(problems ...*ErrorModel) *ErrorModel {
	for _, problem := range problems {
		if problem != nil {
			return problem
		}
	}
	return nil
}

func detail(problem *ErrorModel, resp *http.Response) string {
	if problem != nil && problem.Detail != nil && *problem.Detail != "" {
		return *problem.Detail
	}
	if resp != nil {
		return resp.Status
	}
	return "unknown error"
}

func unexpected(op string, resp *http.Response, problem *ErrorModel) error {
	status := "no response"
	if resp != nil {
		status = resp.Status
	}
	if problem != nil && problem.Detail != nil && *problem.Detail != "" {
		return fmt.Errorf("%w: %s %s: %s", ErrUnexpected, op, status, *problem.Detail)
	}
	return fmt.Errorf("%w: %s %s", ErrUnexpected, op, status)
}

func statusCode(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}
