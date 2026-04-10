package billingclient

import (
	"context"
	"errors"
	"fmt"
	"math"
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
	orgIDWire, err := uint64ToInt64(orgID, "org_id")
	if err != nil {
		return Reservation{}, err
	}
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

func parseReservation(in WindowReservation) (Reservation, error) {
	var renewBy time.Time
	if in.RenewBy != nil {
		renewBy = in.RenewBy.UTC()
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

func (c *ServiceClient) Generated() ClientWithResponsesInterface {
	return c.inner
}

func statusCode(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}
