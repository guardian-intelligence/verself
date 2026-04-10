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

func (c *ServiceClient) GetBalance(ctx context.Context, orgID uint64, reqEditors ...RequestEditorFn) (BalanceResponse, error) {
	orgIDWire := apiwire.Uint64(orgID).String()
	resp, err := c.inner.GetBalanceWithResponse(ctx, orgIDWire, reqEditors...)
	if err != nil {
		return BalanceResponse{}, err
	}
	if resp.JSON200 != nil {
		return *resp.JSON200, nil
	}
	return BalanceResponse{}, unexpected("get balance", resp.HTTPResponse, resp.ApplicationproblemJSONDefault)
}

func (c *ServiceClient) ListSubscriptions(ctx context.Context, orgID uint64, reqEditors ...RequestEditorFn) (SubscriptionsResponse, error) {
	orgIDWire := apiwire.Uint64(orgID).String()
	resp, err := c.inner.ListSubscriptionsWithResponse(ctx, orgIDWire, reqEditors...)
	if err != nil {
		return SubscriptionsResponse{}, err
	}
	if resp.JSON200 != nil {
		return *resp.JSON200, nil
	}
	return SubscriptionsResponse{}, unexpected("list subscriptions", resp.HTTPResponse, resp.ApplicationproblemJSONDefault)
}

func (c *ServiceClient) ListGrants(ctx context.Context, orgID uint64, productID string, active bool, reqEditors ...RequestEditorFn) (GrantsResponse, error) {
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
		return GrantsResponse{}, err
	}
	if resp.JSON200 != nil {
		return *resp.JSON200, nil
	}
	return GrantsResponse{}, unexpected("list grants", resp.HTTPResponse, resp.ApplicationproblemJSONDefault)
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
		wireCadence := CreateSubscriptionRequestCadence(cadence)
		body.Cadence = &wireCadence
	}
	resp, err := c.inner.CreateSubscriptionWithResponse(ctx, body, reqEditors...)
	if err != nil {
		return "", err
	}
	if resp.JSON200 != nil {
		return resp.JSON200.Url, nil
	}
	return "", unexpected("create subscription", resp.HTTPResponse, firstProblem(resp.ApplicationproblemJSON501, resp.ApplicationproblemJSON500, resp.ApplicationproblemJSON422))
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

func parseReservation(in WindowReservationResponse) (Reservation, error) {
	orgID, err := apiwire.ParseUint64(in.OrgId)
	if err != nil {
		return Reservation{}, fmt.Errorf("billing-client: reservation org_id: %w", err)
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
		UnitRates:        in.UnitRates,
		CostPerSec:       in.CostPerUnit,
		WindowStart:      in.WindowStart.UTC(),
		ExpiresAt:        in.ExpiresAt.UTC(),
		RenewBy:          renewBy,
	}, nil
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
