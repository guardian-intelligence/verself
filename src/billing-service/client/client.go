package billingclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/forge-metal/apiwire"
)

type ServiceClient struct {
	baseURL        *url.URL
	httpClient     *http.Client
	requestEditors []RequestEditorFn
}

type (
	Client              = ServiceClient
	ClientWithResponses = ServiceClient
)

func New(baseURL string, opts ...ClientOption) (*ServiceClient, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, fmt.Errorf("billing-client: parse base url: %w", err)
	}
	client := &ServiceClient{
		baseURL:    parsed,
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(client); err != nil {
			return nil, err
		}
	}
	if client.httpClient == nil {
		client.httpClient = http.DefaultClient
	}
	if client.baseURL == nil {
		return nil, fmt.Errorf("billing-client: base url is required")
	}
	return client, nil
}

func NewClientWithResponses(baseURL string, opts ...ClientOption) (*ClientWithResponses, error) {
	client, err := New(baseURL, opts...)
	if err != nil {
		return nil, err
	}
	return (*ClientWithResponses)(client), nil
}

func NewFromGenerated(client *ServiceClient) *ServiceClient {
	return client
}

func WithBaseURL(baseURL string) ClientOption {
	return func(c *ServiceClient) error {
		parsed, err := url.Parse(strings.TrimSpace(baseURL))
		if err != nil {
			return fmt.Errorf("billing-client: parse base url: %w", err)
		}
		c.baseURL = parsed
		return nil
	}
}

func WithHTTPClient(doer *http.Client) ClientOption {
	return func(c *ServiceClient) error {
		c.httpClient = doer
		return nil
	}
}

func WithRequestEditorFn(fn RequestEditorFn) ClientOption {
	return func(c *ServiceClient) error {
		c.requestEditors = append(c.requestEditors, fn)
		return nil
	}
}

func (c *ServiceClient) GetEntitlements(ctx context.Context, orgID uint64, reqEditors ...RequestEditorFn) (apiwire.BillingEntitlementsView, error) {
	var out apiwire.BillingEntitlementsView
	if err := c.getJSON(ctx, "/internal/billing/v1/orgs/"+apiwire.Uint64(orgID).String()+"/entitlements", nil, &out, "get entitlements", nil, reqEditors...); err != nil {
		return apiwire.BillingEntitlementsView{}, err
	}
	return out, nil
}

func (c *ServiceClient) ListContracts(ctx context.Context, orgID uint64, reqEditors ...RequestEditorFn) (apiwire.BillingContracts, error) {
	var out apiwire.BillingContracts
	if err := c.getJSON(ctx, "/internal/billing/v1/orgs/"+apiwire.Uint64(orgID).String()+"/contracts", nil, &out, "list contracts", nil, reqEditors...); err != nil {
		return apiwire.BillingContracts{}, err
	}
	return out, nil
}

func (c *ServiceClient) ListPlans(ctx context.Context, productID string, reqEditors ...RequestEditorFn) (apiwire.BillingPlans, error) {
	var out apiwire.BillingPlans
	if err := c.getJSON(ctx, "/internal/billing/v1/products/"+url.PathEscape(productID)+"/plans", nil, &out, "list plans", nil, reqEditors...); err != nil {
		return apiwire.BillingPlans{}, err
	}
	return out, nil
}

func (c *ServiceClient) ListGrants(ctx context.Context, orgID uint64, productID string, active bool, reqEditors ...RequestEditorFn) (apiwire.BillingGrants, error) {
	query := url.Values{}
	if productID != "" {
		query.Set("product_id", productID)
	}
	if active {
		query.Set("active", "true")
	}
	var out apiwire.BillingGrants
	if err := c.getJSON(ctx, "/internal/billing/v1/orgs/"+apiwire.Uint64(orgID).String()+"/grants", query, &out, "list grants", nil, reqEditors...); err != nil {
		return apiwire.BillingGrants{}, err
	}
	return out, nil
}

func (c *ServiceClient) ListDocuments(ctx context.Context, orgID uint64, productID string, reqEditors ...RequestEditorFn) (apiwire.BillingDocuments, error) {
	query := url.Values{}
	if productID != "" {
		query.Set("product_id", productID)
	}
	var out apiwire.BillingDocuments
	if err := c.getJSON(ctx, "/internal/billing/v1/orgs/"+apiwire.Uint64(orgID).String()+"/documents", query, &out, "list documents", nil, reqEditors...); err != nil {
		return apiwire.BillingDocuments{}, err
	}
	return out, nil
}

func (c *ServiceClient) GetStatement(ctx context.Context, orgID uint64, productID string, reqEditors ...RequestEditorFn) (apiwire.BillingStatement, error) {
	query := url.Values{}
	if productID != "" {
		query.Set("product_id", productID)
	}
	var out apiwire.BillingStatement
	if err := c.getJSON(ctx, "/internal/billing/v1/orgs/"+apiwire.Uint64(orgID).String()+"/statement", query, &out, "get statement", nil, reqEditors...); err != nil {
		return apiwire.BillingStatement{}, err
	}
	return out, nil
}

func (c *ServiceClient) CreateCheckout(ctx context.Context, orgID uint64, productID string, amountCents int64, successURL string, cancelURL string, reqEditors ...RequestEditorFn) (string, error) {
	body := apiwire.BillingCreateCheckoutRequest{
		OrgID:       apiwire.Uint64(orgID),
		ProductID:   productID,
		AmountCents: amountCents,
		SuccessURL:  successURL,
		CancelURL:   cancelURL,
	}
	var out apiwire.BillingURLResponse
	if err := c.postJSON(ctx, "/internal/billing/v1/checkout", body, &out, "create checkout", nil, reqEditors...); err != nil {
		return "", err
	}
	return out.URL, nil
}

func (c *ServiceClient) CreateContract(ctx context.Context, orgID uint64, planID string, cadence string, successURL string, cancelURL string, reqEditors ...RequestEditorFn) (string, error) {
	body := apiwire.BillingCreateContractRequest{
		OrgID:      apiwire.Uint64(orgID),
		PlanID:     planID,
		SuccessURL: successURL,
		CancelURL:  cancelURL,
	}
	if cadence != "" {
		body.Cadence = cadence
	}
	var out apiwire.BillingURLResponse
	if err := c.postJSON(ctx, "/internal/billing/v1/contracts", body, &out, "create contract", nil, reqEditors...); err != nil {
		return "", err
	}
	return out.URL, nil
}

func (c *ServiceClient) CreateContractChange(ctx context.Context, orgID uint64, contractID string, targetPlanID string, successURL string, cancelURL string, reqEditors ...RequestEditorFn) (apiwire.BillingContractChangeResponse, error) {
	body := apiwire.BillingCreateContractChangeRequest{
		OrgID:        apiwire.Uint64(orgID),
		TargetPlanID: targetPlanID,
		SuccessURL:   successURL,
		CancelURL:    cancelURL,
	}
	var out apiwire.BillingContractChangeResponse
	if err := c.postJSON(ctx, "/internal/billing/v1/contracts/"+url.PathEscape(contractID)+"/changes", body, &out, "create contract change", ErrContractNotFound, reqEditors...); err != nil {
		return apiwire.BillingContractChangeResponse{}, err
	}
	return out, nil
}

func (c *ServiceClient) CancelContract(ctx context.Context, orgID uint64, contractID string, reqEditors ...RequestEditorFn) (apiwire.BillingCancelContractResponse, error) {
	body := apiwire.BillingCancelContractRequest{OrgID: apiwire.Uint64(orgID)}
	var out apiwire.BillingCancelContractResponse
	if err := c.postJSON(ctx, "/internal/billing/v1/contracts/"+url.PathEscape(contractID)+"/cancel", body, &out, "cancel contract", ErrContractNotFound, reqEditors...); err != nil {
		return apiwire.BillingCancelContractResponse{}, err
	}
	return out, nil
}

func (c *ServiceClient) CreatePortalSession(ctx context.Context, orgID uint64, returnURL string, reqEditors ...RequestEditorFn) (string, error) {
	body := apiwire.BillingCreatePortalSessionRequest{
		OrgID:     apiwire.Uint64(orgID),
		ReturnURL: returnURL,
	}
	var out apiwire.BillingURLResponse
	if err := c.postJSON(ctx, "/internal/billing/v1/portal", body, &out, "create portal session", nil, reqEditors...); err != nil {
		return "", err
	}
	return out.URL, nil
}

func (c *ServiceClient) Reserve(ctx context.Context, jobID int64, orgID uint64, productID string, actorID string, concurrentCount uint64, sourceType string, sourceRef string, windowSeq uint32, windowMillis uint32, allocation map[string]float64, reqEditors ...RequestEditorFn) (Reservation, error) {
	body := apiwire.BillingReserveWindowRequest{
		OrgID:           apiwire.Uint64(orgID),
		ProductID:       productID,
		ActorID:         actorID,
		ConcurrentCount: concurrentCount,
		SourceType:      sourceType,
		SourceRef:       sourceRef,
		WindowSeq:       windowSeq,
		WindowMillis:    windowMillis,
		BillingJobID:    jobID,
		Allocation:      allocation,
	}
	var out apiwire.BillingReserveWindowResult
	if err := c.postJSON(ctx, "/internal/billing/v1/reserve", body, &out, "reserve", ErrPaymentRequired, reqEditors...); err != nil {
		return Reservation{}, err
	}
	return parseReservation(out.Reservation)
}

func (c *ServiceClient) Activate(ctx context.Context, reservation Reservation, activatedAt time.Time, reqEditors ...RequestEditorFn) (Reservation, error) {
	body := apiwire.BillingActivateWindowRequest{
		WindowID:    reservation.WindowId,
		ActivatedAt: activatedAt.UTC(),
	}
	var out apiwire.BillingActivateWindowResult
	if err := c.postJSON(ctx, "/internal/billing/v1/activate", body, &out, "activate", nil, reqEditors...); err != nil {
		return Reservation{}, err
	}
	return parseReservation(out.Reservation)
}

func (c *ServiceClient) Settle(ctx context.Context, reservation Reservation, actualQuantity uint32, usageSummary map[string]any, reqEditors ...RequestEditorFn) error {
	body := apiwire.BillingSettleWindowRequest{
		WindowID:       reservation.WindowId,
		ActualQuantity: actualQuantity,
	}
	if usageSummary != nil {
		body.UsageSummary = usageSummary
	}
	var out apiwire.BillingSettleResult
	return c.postJSON(ctx, "/internal/billing/v1/settle", body, &out, "settle", nil, reqEditors...)
}

func (c *ServiceClient) Void(ctx context.Context, reservation Reservation, reqEditors ...RequestEditorFn) error {
	body := apiwire.BillingVoidWindowRequest{WindowID: reservation.WindowId}
	var out apiwire.BillingVoidWindowResult
	return c.postJSON(ctx, "/internal/billing/v1/void", body, &out, "void", nil, reqEditors...)
}

func parseReservation(in apiwire.BillingWindowReservation) (Reservation, error) {
	orgID := in.OrgID.Uint64()
	skuRates := make(map[string]int64, len(in.SKURates))
	for key, value := range in.SKURates {
		parsed := value.Uint64()
		if parsed > math.MaxInt64 {
			return Reservation{}, fmt.Errorf("billing-client: reservation sku rate %s exceeds int64", key)
		}
		skuRates[key] = int64(parsed)
	}
	costPerMillis := in.CostPerUnit.Uint64()
	if costPerMillis > math.MaxInt64 {
		return Reservation{}, fmt.Errorf("billing-client: reservation cost_per_unit exceeds int64")
	}
	reservation := Reservation{
		WindowId:         in.WindowID,
		OrgId:            orgID,
		ProductId:        in.ProductID,
		PlanId:           in.PlanID,
		ActorId:          in.ActorID,
		SourceType:       in.SourceType,
		SourceRef:        in.SourceRef,
		WindowSeq:        in.WindowSeq,
		ReservationShape: in.ReservationShape,
		WindowMillis:     in.ReservedQuantity,
		PricingPhase:     in.PricingPhase,
		Allocation:       in.Allocation,
		SKURates:         skuRates,
		CostPerMillis:    int64(costPerMillis),
		WindowStart:      in.WindowStart.UTC(),
		ExpiresAt:        in.ExpiresAt.UTC(),
	}
	if in.ActivatedAt != nil {
		value := in.ActivatedAt.UTC()
		reservation.ActivatedAt = &value
	}
	if in.RenewBy != nil {
		reservation.RenewBy = in.RenewBy.UTC()
	}
	return reservation, nil
}

func (c *ServiceClient) getJSON(ctx context.Context, path string, query url.Values, out any, op string, notFoundSentinel error, reqEditors ...RequestEditorFn) error {
	resp, body, err := c.do(ctx, http.MethodGet, path, query, nil, reqEditors...)
	if err != nil {
		if resp == nil {
			return fmt.Errorf("billing-client: %s: %w", op, err)
		}
		return c.problemError(op, resp, body, notFoundSentinel)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("billing-client: decode %s response: %w", op, err)
	}
	return nil
}

func (c *ServiceClient) postJSON(ctx context.Context, path string, body any, out any, op string, notFoundSentinel error, reqEditors ...RequestEditorFn) error {
	resp, payload, err := c.do(ctx, http.MethodPost, path, nil, body, reqEditors...)
	if err != nil {
		if resp == nil {
			return fmt.Errorf("billing-client: %s: %w", op, err)
		}
		return c.problemError(op, resp, payload, notFoundSentinel)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("billing-client: decode %s response: %w", op, err)
	}
	return nil
}

func (c *ServiceClient) do(ctx context.Context, method, path string, query url.Values, body any, reqEditors ...RequestEditorFn) (*http.Response, []byte, error) {
	endpoint, err := c.resolveURL(path, query)
	if err != nil {
		return nil, nil, err
	}
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("billing-client: marshal request body: %w", err)
		}
		reader = strings.NewReader(string(buf))
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, editor := range c.requestEditors {
		if editor == nil {
			continue
		}
		if err := editor(ctx, req); err != nil {
			return nil, nil, err
		}
	}
	for _, editor := range reqEditors {
		if editor == nil {
			continue
		}
		if err := editor(ctx, req); err != nil {
			return nil, nil, err
		}
	}
	client := c.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, nil, err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, payload, nil
	}
	return resp, payload, c.problemError(method+" "+path, resp, payload, nil)
}

func (c *ServiceClient) resolveURL(path string, query url.Values) (string, error) {
	endpoint, err := c.baseURL.Parse("." + path)
	if err != nil {
		return "", fmt.Errorf("billing-client: parse request path: %w", err)
	}
	if query != nil {
		endpoint.RawQuery = query.Encode()
	}
	return endpoint.String(), nil
}

func (c *ServiceClient) problemError(op string, resp *http.Response, body []byte, notFoundSentinel error) error {
	statusCode := 0
	if resp != nil {
		statusCode = resp.StatusCode
	}
	problem := decodeProblem(body)
	switch statusCode {
	case http.StatusPaymentRequired:
		return &HTTPError{Op: op, StatusCode: statusCode, Problem: problem, Body: body, Cause: ErrPaymentRequired}
	case http.StatusForbidden:
		return &HTTPError{Op: op, StatusCode: statusCode, Problem: problem, Body: body, Cause: ErrForbidden}
	case http.StatusNotFound:
		if notFoundSentinel != nil {
			return &HTTPError{Op: op, StatusCode: statusCode, Problem: problem, Body: body, Cause: notFoundSentinel}
		}
		return &HTTPError{Op: op, StatusCode: statusCode, Problem: problem, Body: body, Cause: ErrUnexpected}
	case http.StatusUnprocessableEntity:
		if problem != nil && problem.Type != nil && *problem.Type == problemTypeNoStripeCustomer {
			return &HTTPError{Op: op, StatusCode: statusCode, Problem: problem, Body: body, Cause: ErrNoStripeCustomer}
		}
	}
	return &HTTPError{Op: op, StatusCode: statusCode, Problem: problem, Body: body, Cause: ErrUnexpected}
}

func decodeProblem(body []byte) *ErrorModel {
	if len(body) == 0 {
		return nil
	}
	var problem ErrorModel
	if err := json.Unmarshal(body, &problem); err != nil {
		return nil
	}
	return &problem
}
