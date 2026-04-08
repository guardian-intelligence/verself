package billingclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

var (
	ErrPaymentRequired = errors.New("billing-client: payment required")
	ErrForbidden       = errors.New("billing-client: forbidden")
	ErrUnexpected      = errors.New("billing-client: unexpected response")
)

// ServiceClient is the supported integration surface for other forge-metal services.
// It wraps the generated OpenAPI client and normalizes the billing error model.
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
	QuotaResult    = QuotaCheckOutputBody
	QuotaViolation = QuotaViolationJSON
	Reservation    = ReservationJSON
)

func (c *ServiceClient) CheckQuotas(ctx context.Context, orgID uint64, productID string, concurrentCount uint64, reqEditors ...RequestEditorFn) (QuotaResult, error) {
	resp, err := c.inner.CheckQuotasWithResponse(ctx, CheckQuotasJSONRequestBody{
		OrgId:           int64(orgID),
		ProductId:       productID,
		ConcurrentCount: int64(concurrentCount),
	}, reqEditors...)
	if err != nil {
		return QuotaResult{}, err
	}
	if resp.JSON200 != nil {
		return *resp.JSON200, nil
	}
	return QuotaResult{}, unexpected("check quotas", resp.HTTPResponse, resp.ApplicationproblemJSONDefault)
}

func (c *ServiceClient) Reserve(
	ctx context.Context,
	jobID int64,
	orgID uint64,
	productID string,
	actorID string,
	concurrentCount uint64,
	sourceType string,
	sourceRef string,
	allocation map[string]float64,
	reqEditors ...RequestEditorFn,
) (Reservation, error) {
	resp, err := c.inner.ReserveWithResponse(ctx, ReserveJSONRequestBody{
		JobId:           jobID,
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
	switch {
	case resp.JSON200 != nil:
		return resp.JSON200.Reservation, nil
	default:
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
}

func (c *ServiceClient) Settle(ctx context.Context, reservation Reservation, actualSeconds uint32, reqEditors ...RequestEditorFn) error {
	resp, err := c.inner.SettleWithResponse(ctx, SettleJSONRequestBody{
		Reservation:   reservation,
		ActualSeconds: int32(actualSeconds),
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
	resp, err := c.inner.VoidWithResponse(ctx, VoidJSONRequestBody{
		Reservation: reservation,
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

// Generated returns the underlying generated client for direct access to all
// billing-service endpoints. Use this for read-only proxy operations where
// custom error mapping is unnecessary.
func (c *ServiceClient) Generated() ClientWithResponsesInterface {
	return c.inner
}

func statusCode(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}
