package billingapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/forge-metal/apiwire"
	auth "github.com/forge-metal/auth-middleware"
	"github.com/forge-metal/billing-service/internal/billing"
)

const defaultInternalRole = "billing_internal"

type Config struct {
	Version      string
	ListenAddr   string
	Client       *billing.Client
	Logger       *slog.Logger
	InternalRole string
}

type Handler struct {
	client       *billing.Client
	logger       *slog.Logger
	internalRole string
}

func NewAPI(mux *http.ServeMux, cfg Config) huma.API {
	version := cfg.Version
	if version == "" {
		version = "2.0.0"
	}
	config := huma.DefaultConfig("Billing Service", version)
	if cfg.ListenAddr != "" {
		config.OpenAPI.Servers = []*huma.Server{{URL: "http://" + cfg.ListenAddr}}
	}
	api := humago.New(mux, config)
	RegisterRoutes(api, cfg)
	apiwire.ApplyOpenAPIWireDefaults(api)
	return api
}

func OpenAPIDowngradeYAML() ([]byte, error) {
	api := NewAPI(http.NewServeMux(), Config{Version: "2.0.0", ListenAddr: "127.0.0.1:4242", InternalRole: defaultInternalRole})
	return api.OpenAPI().DowngradeYAML()
}

func OpenAPIYAML() ([]byte, error) {
	api := NewAPI(http.NewServeMux(), Config{Version: "2.0.0", ListenAddr: "127.0.0.1:4242", InternalRole: defaultInternalRole})
	return api.OpenAPI().YAML()
}

type body[T any] struct {
	Body T `required:"true"`
}

type OrgPath struct {
	OrgID string `path:"org_id" pattern:"^[0-9]+$"`
}

type GrantsInput struct {
	OrgPath
	ProductID string `query:"product_id,omitempty" maxLength:"255"`
	Active    bool   `query:"active,omitempty"`
}

type (
	BalanceResponse       = apiwire.BillingBalance
	GrantResponse         = apiwire.BillingGrant
	GrantsResponse        = apiwire.BillingGrants
	SubscriptionsResponse = apiwire.BillingSubscriptions
	SubscriptionResponse  = apiwire.BillingSubscription
)

type CreateCheckoutRequest struct {
	OrgID       apiwire.DecimalUint64 `json:"org_id"`
	ProductID   string                `json:"product_id" minLength:"1" maxLength:"255"`
	AmountCents int64                 `json:"amount_cents" minimum:"1" maximum:"9007199254740991"`
	SuccessURL  string                `json:"success_url" minLength:"1" maxLength:"2048"`
	CancelURL   string                `json:"cancel_url" minLength:"1" maxLength:"2048"`
}

type CreateSubscriptionRequest struct {
	OrgID      apiwire.DecimalUint64 `json:"org_id"`
	PlanID     string                `json:"plan_id" minLength:"1" maxLength:"255"`
	Cadence    string                `json:"cadence,omitempty" enum:"monthly,annual"`
	SuccessURL string                `json:"success_url" minLength:"1" maxLength:"2048"`
	CancelURL  string                `json:"cancel_url" minLength:"1" maxLength:"2048"`
}

type URLResponse struct {
	URL string `json:"url"`
}

type ReserveWindowRequest struct {
	OrgID           apiwire.DecimalUint64 `json:"org_id"`
	ProductID       string                `json:"product_id" minLength:"1" maxLength:"255"`
	ActorID         string                `json:"actor_id" minLength:"1" maxLength:"255"`
	ConcurrentCount uint64                `json:"concurrent_count" minimum:"0" maximum:"9007199254740991"`
	SourceType      string                `json:"source_type" minLength:"1" maxLength:"255"`
	SourceRef       string                `json:"source_ref" minLength:"1" maxLength:"255"`
	Allocation      map[string]float64    `json:"allocation" minProperties:"1"`
}

type ReserveWindowResult struct {
	Reservation apiwire.BillingWindowReservation `json:"reservation"`
}

type SettleWindowRequest struct {
	WindowID       string         `json:"window_id" minLength:"1" maxLength:"255"`
	ActualQuantity uint32         `json:"actual_quantity" minimum:"0"`
	UsageSummary   map[string]any `json:"usage_summary,omitempty"`
}

type VoidWindowRequest struct {
	WindowID string `json:"window_id" minLength:"1" maxLength:"255"`
}

type VoidWindowResult struct {
	WindowID string `json:"window_id"`
}

func billingOrgID(id string) (billing.OrgID, error) {
	parsed, err := apiwire.ParseUint64(id)
	if err != nil {
		return 0, huma.Error400BadRequest("invalid org_id", err)
	}
	return billingOrgIDFromUint64(parsed)
}

func billingOrgIDFromWire(id apiwire.DecimalUint64) (billing.OrgID, error) {
	return billingOrgIDFromUint64(id.Uint64())
}

func billingOrgIDFromUint64(parsed uint64) (billing.OrgID, error) {
	if parsed == 0 {
		return 0, huma.Error400BadRequest("org_id must be positive")
	}
	return billing.OrgID(parsed), nil
}

func windowReservationResponse(reservation billing.WindowReservation) apiwire.BillingWindowReservation {
	return apiwire.BillingWindowReservation{
		WindowID:            reservation.WindowID,
		OrgID:               apiwire.Uint64(uint64(reservation.OrgID)),
		ProductID:           reservation.ProductID,
		PlanID:              reservation.PlanID,
		ActorID:             reservation.ActorID,
		SourceType:          reservation.SourceType,
		SourceRef:           reservation.SourceRef,
		WindowSeq:           reservation.WindowSeq,
		ReservationShape:    string(reservation.ReservationShape),
		ReservedQuantity:    reservation.ReservedQuantity,
		ReservedChargeUnits: apiwire.Uint64(reservation.ReservedChargeUnits),
		PricingPhase:        string(reservation.PricingPhase),
		Allocation:          reservation.Allocation,
		UnitRates:           decimalUnitRates(reservation.UnitRates),
		CostPerUnit:         apiwire.Uint64(reservation.CostPerUnit),
		WindowStart:         reservation.WindowStart,
		ExpiresAt:           reservation.ExpiresAt,
		RenewBy:             reservation.RenewBy,
	}
}

func decimalUnitRates(unitRates map[string]uint64) map[string]apiwire.DecimalUint64 {
	if len(unitRates) == 0 {
		return map[string]apiwire.DecimalUint64{}
	}
	out := make(map[string]apiwire.DecimalUint64, len(unitRates))
	for unit, rate := range unitRates {
		out[unit] = apiwire.Uint64(rate)
	}
	return out
}

func settleResultResponse(result billing.SettleResult) apiwire.BillingSettleResult {
	return apiwire.BillingSettleResult{
		WindowID:            result.WindowID,
		ActualQuantity:      result.ActualQuantity,
		BillableQuantity:    result.BillableQuantity,
		WriteoffQuantity:    result.WriteoffQuantity,
		BilledChargeUnits:   apiwire.Uint64(result.BilledChargeUnits),
		WriteoffChargeUnits: apiwire.Uint64(result.WriteoffChargeUnits),
		SettledAt:           result.SettledAt,
	}
}

func RegisterRoutes(api huma.API, cfg Config) {
	handler := &Handler{
		client:       cfg.Client,
		logger:       cfg.Logger,
		internalRole: firstNonEmpty(cfg.InternalRole, defaultInternalRole),
	}

	public := huma.NewGroup(api, "/internal/billing/v1")
	huma.Get(public, "/orgs/{org_id}/balance", handler.getBalance, operation("get-balance", "Get org grant balance"))
	huma.Get(public, "/orgs/{org_id}/grants", handler.listGrants, operation("list-grants", "List credit grants for an org"))
	huma.Get(public, "/orgs/{org_id}/subscriptions", handler.listSubscriptions, operation("list-subscriptions", "List subscriptions for an org"))
	huma.Post(public, "/checkout", handler.createCheckout, operation("create-checkout", "Create a Stripe checkout session"))
	huma.Post(public, "/subscribe", handler.createSubscription, operation("create-subscription", "Create a Stripe subscription checkout", http.StatusNotImplemented, http.StatusInternalServerError))

	service := huma.NewGroup(api, "/internal/billing/v1")
	service.UseMiddleware(requireInternalRoleMiddleware(api, handler.internalRole))
	huma.Post(service, "/reserve", handler.reserveWindow, operation("reserve-window", "Reserve a billing window", http.StatusPaymentRequired, http.StatusForbidden, http.StatusInternalServerError))
	huma.Post(service, "/settle", handler.settleWindow, operation("settle-window", "Settle a reserved billing window", http.StatusNotFound, http.StatusBadRequest, http.StatusInternalServerError))
	huma.Post(service, "/void", handler.voidWindow, operation("void-window", "Void a reserved billing window", http.StatusNotFound, http.StatusBadRequest, http.StatusInternalServerError))
}

func operation(id, summary string, errors ...int) func(*huma.Operation) {
	return func(op *huma.Operation) {
		op.OperationID = id
		op.Summary = summary
		op.Errors = errors
	}
}

func (h *Handler) getBalance(ctx context.Context, input *OrgPath) (*body[BalanceResponse], error) {
	client, err := h.requireClient()
	if err != nil {
		return nil, err
	}
	orgID, err := billingOrgID(input.OrgID)
	if err != nil {
		return nil, err
	}
	balance, err := client.GetOrgBalance(ctx, orgID)
	if err != nil {
		return nil, huma.Error500InternalServerError("get balance", err)
	}
	return &body[BalanceResponse]{Body: BalanceResponse{
		OrgID:             apiwire.Uint64(uint64(orgID)),
		FreeTierAvailable: apiwire.Uint64(balance.FreeTierAvailable),
		FreeTierPending:   apiwire.Uint64(balance.FreeTierPending),
		CreditAvailable:   apiwire.Uint64(balance.CreditAvailable),
		CreditPending:     apiwire.Uint64(balance.CreditPending),
		TotalAvailable:    apiwire.Uint64(balance.TotalAvailable),
	}}, nil
}

func (h *Handler) listGrants(ctx context.Context, input *GrantsInput) (*body[GrantsResponse], error) {
	client, err := h.requireClient()
	if err != nil {
		return nil, err
	}
	orgID, err := billingOrgID(input.OrgID)
	if err != nil {
		return nil, err
	}
	grants, err := client.ListGrantBalances(ctx, orgID, input.ProductID)
	if err != nil {
		return nil, huma.Error500InternalServerError("list grants", err)
	}
	out := make([]GrantResponse, 0, len(grants))
	for _, grant := range grants {
		out = append(out, GrantResponse{
			GrantID:   grant.GrantID.String(),
			Source:    grant.Source.String(),
			Available: apiwire.Uint64(grant.Available),
			Pending:   apiwire.Uint64(grant.Pending),
			ExpiresAt: grant.ExpiresAt,
		})
	}
	return &body[GrantsResponse]{Body: GrantsResponse{Grants: out}}, nil
}

func (h *Handler) listSubscriptions(ctx context.Context, input *OrgPath) (*body[SubscriptionsResponse], error) {
	client, err := h.requireClient()
	if err != nil {
		return nil, err
	}
	orgID, err := billingOrgID(input.OrgID)
	if err != nil {
		return nil, err
	}
	subscriptions, err := client.ListSubscriptions(ctx, orgID)
	if err != nil {
		return nil, huma.Error500InternalServerError("list subscriptions", err)
	}
	out := make([]SubscriptionResponse, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		out = append(out, SubscriptionResponse{
			SubscriptionID:     apiwire.Int64(subscription.SubscriptionID),
			ProductID:          subscription.ProductID,
			PlanID:             subscription.PlanID,
			Cadence:            subscription.Cadence,
			Status:             subscription.Status,
			CurrentPeriodStart: subscription.CurrentPeriodStart,
			CurrentPeriodEnd:   subscription.CurrentPeriodEnd,
		})
	}
	return &body[SubscriptionsResponse]{Body: SubscriptionsResponse{Subscriptions: out}}, nil
}

func (h *Handler) createCheckout(ctx context.Context, input *body[CreateCheckoutRequest]) (*body[URLResponse], error) {
	client, err := h.requireClient()
	if err != nil {
		return nil, err
	}
	orgID, err := billingOrgIDFromWire(input.Body.OrgID)
	if err != nil {
		return nil, err
	}
	url, err := client.CreateCheckoutSession(ctx, orgID, input.Body.ProductID, billing.CheckoutParams{
		AmountCents: input.Body.AmountCents,
		SuccessURL:  input.Body.SuccessURL,
		CancelURL:   input.Body.CancelURL,
	})
	if err != nil {
		return nil, huma.Error500InternalServerError("create checkout", err)
	}
	return &body[URLResponse]{Body: URLResponse{URL: url}}, nil
}

func (h *Handler) createSubscription(ctx context.Context, input *body[CreateSubscriptionRequest]) (*body[URLResponse], error) {
	client, err := h.requireClient()
	if err != nil {
		return nil, err
	}
	orgID, err := billingOrgIDFromWire(input.Body.OrgID)
	if err != nil {
		return nil, err
	}
	url, err := client.CreateSubscription(ctx, orgID, input.Body.PlanID, billing.BillingCadence(input.Body.Cadence), input.Body.SuccessURL, input.Body.CancelURL)
	if err != nil {
		return nil, huma.Error501NotImplemented("subscription checkout", err)
	}
	return &body[URLResponse]{Body: URLResponse{URL: url}}, nil
}

func (h *Handler) reserveWindow(ctx context.Context, input *body[ReserveWindowRequest]) (*body[ReserveWindowResult], error) {
	client, err := h.requireClient()
	if err != nil {
		return nil, err
	}
	orgID, err := billingOrgIDFromWire(input.Body.OrgID)
	if err != nil {
		return nil, err
	}
	reservation, err := client.ReserveWindow(ctx, billing.ReserveRequest{
		OrgID:           orgID,
		ProductID:       input.Body.ProductID,
		ActorID:         input.Body.ActorID,
		Allocation:      input.Body.Allocation,
		ConcurrentCount: input.Body.ConcurrentCount,
		SourceType:      input.Body.SourceType,
		SourceRef:       input.Body.SourceRef,
	})
	if err != nil {
		return nil, reserveWindowError(err)
	}
	return &body[ReserveWindowResult]{Body: ReserveWindowResult{Reservation: windowReservationResponse(reservation)}}, nil
}

func (h *Handler) settleWindow(ctx context.Context, input *body[SettleWindowRequest]) (*body[apiwire.BillingSettleResult], error) {
	client, err := h.requireClient()
	if err != nil {
		return nil, err
	}
	result, err := client.SettleWindow(ctx, input.Body.WindowID, input.Body.ActualQuantity, input.Body.UsageSummary)
	if err != nil {
		return nil, h.settleWindowError(ctx, input.Body, err)
	}
	return &body[apiwire.BillingSettleResult]{Body: settleResultResponse(result)}, nil
}

func (h *Handler) voidWindow(ctx context.Context, input *body[VoidWindowRequest]) (*body[VoidWindowResult], error) {
	client, err := h.requireClient()
	if err != nil {
		return nil, err
	}
	if err := client.VoidWindow(ctx, input.Body.WindowID); err != nil {
		return nil, h.voidWindowError(ctx, input.Body.WindowID, err)
	}
	return &body[VoidWindowResult]{Body: VoidWindowResult{WindowID: input.Body.WindowID}}, nil
}

func (h *Handler) requireClient() (*billing.Client, error) {
	if h.client == nil {
		return nil, huma.Error500InternalServerError("billing client unavailable")
	}
	return h.client, nil
}

func reserveWindowError(err error) error {
	switch {
	case errors.Is(err, billing.ErrInsufficientBalance):
		return huma.Error402PaymentRequired("reserve", err)
	case errors.Is(err, billing.ErrOrgSuspended):
		return huma.Error403Forbidden("reserve", err)
	default:
		return huma.Error500InternalServerError("reserve", err)
	}
}

func (h *Handler) settleWindowError(ctx context.Context, input SettleWindowRequest, err error) error {
	switch {
	case errors.Is(err, billing.ErrWindowNotFound):
		return huma.Error404NotFound("window not found")
	case errors.Is(err, billing.ErrWindowAlreadyVoided):
		return huma.Error400BadRequest("window already voided")
	default:
		h.logError(ctx, "settle billing window", "window_id", input.WindowID, "actual_quantity", input.ActualQuantity, "error", err)
		return huma.Error500InternalServerError("settle", err)
	}
}

func (h *Handler) voidWindowError(ctx context.Context, windowID string, err error) error {
	switch {
	case errors.Is(err, billing.ErrWindowNotFound):
		return huma.Error404NotFound("window not found")
	case errors.Is(err, billing.ErrWindowAlreadySettled):
		return huma.Error400BadRequest("window already settled")
	default:
		h.logError(ctx, "void billing window", "window_id", windowID, "error", err)
		return huma.Error500InternalServerError("void", err)
	}
}

func (h *Handler) logError(ctx context.Context, msg string, args ...any) {
	if h.logger != nil {
		h.logger.ErrorContext(ctx, msg, args...)
	}
}

func requireInternalRoleMiddleware(api huma.API, role string) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		identity := auth.FromContext(ctx.Context())
		if identity == nil {
			huma.WriteErr(api, ctx, http.StatusUnauthorized, "missing identity")
			return
		}
		for _, candidate := range identity.Roles {
			if candidate == role {
				next(ctx)
				return
			}
		}
		huma.WriteErr(api, ctx, http.StatusForbidden, "missing internal billing role")
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
