package billingclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
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

type (
	Balance       = BalanceOutputBody
	Grants        = GrantsOutputBody
	Subscriptions = SubscriptionsOutputBody
)

type Reservation struct {
	WindowId         string
	JobId            int64
	OrgId            int64
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

type SettleWindowResult = SettleOutputBody

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
	resp, err := c.inner.ReserveWindowWithResponse(ctx, ReserveWindowJSONRequestBody{
		OrgId:           int64(orgID),
		ProductId:       productID,
		ActorId:         actorID,
		ConcurrentCount: int64(concurrentCount),
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
		return Reservation{}, fmt.Errorf("%w: %s", ErrPaymentRequired, detail(resp.ApplicationproblemJSONDefault, resp.HTTPResponse))
	case http.StatusForbidden:
		return Reservation{}, fmt.Errorf("%w: %s", ErrForbidden, detail(resp.ApplicationproblemJSONDefault, resp.HTTPResponse))
	case http.StatusBadRequest:
		return Reservation{}, fmt.Errorf("billing-client: reserve bad request: %s", detail(resp.ApplicationproblemJSONDefault, resp.HTTPResponse))
	default:
		return Reservation{}, unexpected("reserve", resp.HTTPResponse, resp.ApplicationproblemJSONDefault)
	}
}

func parseReservation(in WindowReservationJSON) (Reservation, error) {
	windowStart, err := time.Parse(time.RFC3339Nano, in.WindowStart)
	if err != nil {
		return Reservation{}, fmt.Errorf("parse window_start: %w", err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, in.ExpiresAt)
	if err != nil {
		return Reservation{}, fmt.Errorf("parse expires_at: %w", err)
	}
	var renewBy time.Time
	if in.RenewBy != nil && *in.RenewBy != "" {
		renewBy, err = time.Parse(time.RFC3339Nano, *in.RenewBy)
		if err != nil {
			return Reservation{}, fmt.Errorf("parse renew_by: %w", err)
		}
	}
	return Reservation{
		WindowId:         in.WindowId,
		OrgId:            in.OrgId,
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
		WindowStart:      windowStart.UTC(),
		ExpiresAt:        expiresAt.UTC(),
		RenewBy:          renewBy.UTC(),
	}, nil
}

func (c *ServiceClient) Settle(ctx context.Context, reservation Reservation, actualQuantity uint32, reqEditors ...RequestEditorFn) error {
	resp, err := c.inner.SettleWindowWithResponse(ctx, SettleWindowJSONRequestBody{
		WindowId:       reservation.WindowId,
		ActualQuantity: int32(actualQuantity),
	}, reqEditors...)
	if err != nil {
		return err
	}
	if resp.JSON200 != nil {
		return nil
	}
	if statusCode(resp.HTTPResponse) == http.StatusBadRequest {
		return fmt.Errorf("billing-client: settle bad request: %s", detail(resp.ApplicationproblemJSONDefault, resp.HTTPResponse))
	}
	return unexpected("settle", resp.HTTPResponse, resp.ApplicationproblemJSONDefault)
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
		return fmt.Errorf("billing-client: void bad request: %s", detail(resp.ApplicationproblemJSONDefault, resp.HTTPResponse))
	}
	return unexpected("void", resp.HTTPResponse, resp.ApplicationproblemJSONDefault)
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

func (c *ServiceClient) Generated() ClientWithResponsesInterface {
	return c.inner
}

func statusCode(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}
