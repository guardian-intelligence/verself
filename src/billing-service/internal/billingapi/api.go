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

const (
	defaultInternalRole         = "billing_internal"
	problemTypeNoStripeCustomer = "urn:forge-metal:problem:billing:no-stripe-customer"
)

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

type StatementInput struct {
	OrgPath
	ProductID string `query:"product_id" required:"true" minLength:"1" maxLength:"255"`
}

type (
	BalanceResponse       = apiwire.BillingBalance
	GrantResponse         = apiwire.BillingGrant
	GrantsResponse        = apiwire.BillingGrants
	StatementResponse     = apiwire.BillingStatement
	SubscriptionsResponse = apiwire.BillingSubscriptions
	SubscriptionResponse  = apiwire.BillingSubscription
)

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
		SKURates:            decimalSKURates(reservation.SKURates),
		CostPerUnit:         apiwire.Uint64(reservation.CostPerUnit),
		WindowStart:         reservation.WindowStart,
		ActivatedAt:         reservation.ActivatedAt,
		ExpiresAt:           reservation.ExpiresAt,
		RenewBy:             reservation.RenewBy,
	}
}

func decimalSKURates(skuRates map[string]uint64) map[string]apiwire.DecimalUint64 {
	if len(skuRates) == 0 {
		return map[string]apiwire.DecimalUint64{}
	}
	out := make(map[string]apiwire.DecimalUint64, len(skuRates))
	for unit, rate := range skuRates {
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
	huma.Get(public, "/orgs/{org_id}/statement", handler.getStatement, operation("get-statement", "Get current billing statement for an org"))
	huma.Get(public, "/orgs/{org_id}/subscriptions", handler.listSubscriptions, operation("list-subscriptions", "List subscriptions for an org"))
	huma.Post(public, "/checkout", handler.createCheckout, operation("create-checkout", "Create a Stripe checkout session"))
	huma.Post(public, "/subscribe", handler.createSubscription, operation("create-subscription", "Create a Stripe subscription checkout", http.StatusInternalServerError))
	huma.Post(public, "/portal", handler.createPortal, operation("create-portal", "Create a Stripe customer portal session", http.StatusUnprocessableEntity, http.StatusInternalServerError))

	service := huma.NewGroup(api, "/internal/billing/v1")
	service.UseMiddleware(requireInternalRoleMiddleware(api, handler.internalRole))
	huma.Post(service, "/reserve", handler.reserveWindow, operation("reserve-window", "Reserve a billing window", http.StatusPaymentRequired, http.StatusForbidden, http.StatusInternalServerError))
	huma.Post(service, "/activate", handler.activateWindow, operation("activate-window", "Activate a reserved billing window", http.StatusNotFound, http.StatusBadRequest, http.StatusInternalServerError))
	huma.Post(service, "/settle", handler.settleWindow, operation("settle-window", "Settle a reserved billing window", http.StatusNotFound, http.StatusBadRequest, http.StatusInternalServerError))
	huma.Post(service, "/void", handler.voidWindow, operation("void-window", "Void a reserved billing window", http.StatusNotFound, http.StatusBadRequest, http.StatusInternalServerError))
	huma.Post(service, "/subscription-provider-events", handler.applySubscriptionProviderEvent, operation("apply-subscription-provider-event", "Apply a subscription provider event", http.StatusBadRequest, http.StatusInternalServerError))
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
			GrantID:             grant.GrantID.String(),
			ScopeType:           grant.ScopeType.String(),
			ScopeProductID:      grant.ScopeProductID,
			ScopeBucketID:       grant.ScopeBucketID,
			Source:              grant.Source.String(),
			SourceReferenceID:   grant.SourceReferenceID,
			EntitlementPeriodID: grant.EntitlementPeriodID,
			PolicyVersion:       grant.PolicyVersion,
			StartsAt:            grant.StartsAt,
			PeriodStart:         grant.PeriodStart,
			PeriodEnd:           grant.PeriodEnd,
			Available:           apiwire.Uint64(grant.Available),
			Pending:             apiwire.Uint64(grant.Pending),
			ExpiresAt:           grant.ExpiresAt,
		})
	}
	return &body[GrantsResponse]{Body: GrantsResponse{Grants: out}}, nil
}

func (h *Handler) getStatement(ctx context.Context, input *StatementInput) (*body[StatementResponse], error) {
	client, err := h.requireClient()
	if err != nil {
		return nil, err
	}
	orgID, err := billingOrgID(input.OrgID)
	if err != nil {
		return nil, err
	}
	statement, err := client.PreviewStatement(ctx, orgID, input.ProductID)
	if err != nil {
		return nil, huma.Error500InternalServerError("get statement", err)
	}
	return &body[StatementResponse]{Body: statementResponse(statement)}, nil
}

func statementResponse(statement billing.Statement) apiwire.BillingStatement {
	lineItems := make([]apiwire.BillingStatementLineItem, 0, len(statement.LineItems))
	for _, line := range statement.LineItems {
		lineItems = append(lineItems, apiwire.BillingStatementLineItem{
			ProductID:         line.ProductID,
			PlanID:            line.PlanID,
			BucketID:          line.BucketID,
			BucketDisplayName: line.BucketDisplayName,
			SKUID:             line.SKUID,
			SKUDisplayName:    line.SKUDisplayName,
			QuantityUnit:      line.QuantityUnit,
			PricingPhase:      line.PricingPhase,
			Quantity:          line.Quantity,
			UnitRate:          apiwire.Uint64(line.UnitRate),
			ChargeUnits:       apiwire.Uint64(line.ChargeUnits),
		})
	}
	bucketSummaries := make([]apiwire.BillingStatementBucketSummary, 0, len(statement.BucketSummaries))
	for _, bucket := range statement.BucketSummaries {
		bucketSummaries = append(bucketSummaries, apiwire.BillingStatementBucketSummary{
			ProductID:         bucket.ProductID,
			BucketID:          bucket.BucketID,
			BucketDisplayName: bucket.BucketDisplayName,
			ChargeUnits:       apiwire.Uint64(bucket.ChargeUnits),
			FreeTierUnits:     apiwire.Uint64(bucket.FreeTierUnits),
			SubscriptionUnits: apiwire.Uint64(bucket.SubscriptionUnits),
			PurchaseUnits:     apiwire.Uint64(bucket.PurchaseUnits),
			PromoUnits:        apiwire.Uint64(bucket.PromoUnits),
			RefundUnits:       apiwire.Uint64(bucket.RefundUnits),
			ReceivableUnits:   apiwire.Uint64(bucket.ReceivableUnits),
			ReservedUnits:     apiwire.Uint64(bucket.ReservedUnits),
		})
	}
	grantSummaries := make([]apiwire.BillingStatementGrantSummary, 0, len(statement.GrantSummaries))
	for _, grant := range statement.GrantSummaries {
		grantSummaries = append(grantSummaries, apiwire.BillingStatementGrantSummary{
			ScopeType:      grant.ScopeType.String(),
			ScopeProductID: grant.ScopeProductID,
			ScopeBucketID:  grant.ScopeBucketID,
			Source:         grant.Source.String(),
			Available:      apiwire.Uint64(grant.Available),
			Pending:        apiwire.Uint64(grant.Pending),
		})
	}
	return apiwire.BillingStatement{
		OrgID:           apiwire.Uint64(uint64(statement.OrgID)),
		ProductID:       statement.ProductID,
		PeriodStart:     statement.PeriodStart,
		PeriodEnd:       statement.PeriodEnd,
		PeriodSource:    statement.PeriodSource,
		GeneratedAt:     statement.GeneratedAt,
		Currency:        statement.Currency,
		UnitLabel:       statement.UnitLabel,
		LineItems:       lineItems,
		BucketSummaries: bucketSummaries,
		GrantSummaries:  grantSummaries,
		Totals: apiwire.BillingStatementTotals{
			ChargeUnits:       apiwire.Uint64(statement.Totals.ChargeUnits),
			FreeTierUnits:     apiwire.Uint64(statement.Totals.FreeTierUnits),
			SubscriptionUnits: apiwire.Uint64(statement.Totals.SubscriptionUnits),
			PurchaseUnits:     apiwire.Uint64(statement.Totals.PurchaseUnits),
			PromoUnits:        apiwire.Uint64(statement.Totals.PromoUnits),
			RefundUnits:       apiwire.Uint64(statement.Totals.RefundUnits),
			ReceivableUnits:   apiwire.Uint64(statement.Totals.ReceivableUnits),
			ReservedUnits:     apiwire.Uint64(statement.Totals.ReservedUnits),
			TotalDueUnits:     apiwire.Uint64(statement.Totals.TotalDueUnits),
		},
	}
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
			ContractID:         subscription.ContractID,
			ProductID:          subscription.ProductID,
			PlanID:             subscription.PlanID,
			Cadence:            subscription.Cadence,
			Status:             subscription.Status,
			PaymentState:       string(subscription.PaymentState),
			EntitlementState:   string(subscription.EntitlementState),
			CurrentPeriodStart: subscription.CurrentPeriodStart,
			CurrentPeriodEnd:   subscription.CurrentPeriodEnd,
		})
	}
	return &body[SubscriptionsResponse]{Body: SubscriptionsResponse{Subscriptions: out}}, nil
}

func (h *Handler) createCheckout(ctx context.Context, input *body[apiwire.BillingCreateCheckoutRequest]) (*body[apiwire.BillingURLResponse], error) {
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
	return &body[apiwire.BillingURLResponse]{Body: apiwire.BillingURLResponse{URL: url}}, nil
}

func (h *Handler) createSubscription(ctx context.Context, input *body[apiwire.BillingCreateSubscriptionRequest]) (*body[apiwire.BillingURLResponse], error) {
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
		return nil, huma.Error500InternalServerError("create subscription checkout", err)
	}
	return &body[apiwire.BillingURLResponse]{Body: apiwire.BillingURLResponse{URL: url}}, nil
}

func (h *Handler) createPortal(ctx context.Context, input *body[apiwire.BillingCreatePortalSessionRequest]) (*body[apiwire.BillingURLResponse], error) {
	client, err := h.requireClient()
	if err != nil {
		return nil, err
	}
	orgID, err := billingOrgIDFromWire(input.Body.OrgID)
	if err != nil {
		return nil, err
	}
	url, err := client.CreatePortalSession(ctx, orgID, input.Body.ReturnURL)
	if err != nil {
		if errors.Is(err, billing.ErrNoStripeCustomer) {
			problem := huma.Error422UnprocessableEntity("no stripe customer linked to this org", err)
			if model, ok := problem.(*huma.ErrorModel); ok {
				model.Type = problemTypeNoStripeCustomer
			}
			return nil, problem
		}
		return nil, huma.Error500InternalServerError("create portal session", err)
	}
	return &body[apiwire.BillingURLResponse]{Body: apiwire.BillingURLResponse{URL: url}}, nil
}

func (h *Handler) applySubscriptionProviderEvent(ctx context.Context, input *body[apiwire.BillingApplySubscriptionProviderEventRequest]) (*body[apiwire.BillingApplySubscriptionProviderEventResponse], error) {
	client, err := h.requireClient()
	if err != nil {
		return nil, err
	}
	orgID, err := billingOrgIDFromWire(input.Body.OrgID)
	if err != nil {
		return nil, err
	}
	if err := client.ApplySubscriptionProviderEvent(ctx, billing.SubscriptionProviderEvent{
		Provider:                  input.Body.Provider,
		EventType:                 input.Body.EventType,
		OrgID:                     orgID,
		ProductID:                 input.Body.ProductID,
		PlanID:                    input.Body.PlanID,
		Cadence:                   input.Body.Cadence,
		Status:                    input.Body.Status,
		ProviderSubscriptionID:    input.Body.ProviderSubscriptionID,
		ProviderCheckoutSessionID: input.Body.ProviderCheckoutSessionID,
		ProviderCustomerID:        input.Body.ProviderCustomerID,
		CurrentPeriodStart:        input.Body.CurrentPeriodStart,
		CurrentPeriodEnd:          input.Body.CurrentPeriodEnd,
		PaymentState:              billing.EntitlementPaymentState(input.Body.PaymentState),
		EntitlementState:          billing.EntitlementState(input.Body.EntitlementState),
	}); err != nil {
		return nil, huma.Error400BadRequest("apply subscription provider event", err)
	}
	return &body[apiwire.BillingApplySubscriptionProviderEventResponse]{Body: apiwire.BillingApplySubscriptionProviderEventResponse{Applied: true}}, nil
}

func (h *Handler) reserveWindow(ctx context.Context, input *body[apiwire.BillingReserveWindowRequest]) (*body[apiwire.BillingReserveWindowResult], error) {
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
	return &body[apiwire.BillingReserveWindowResult]{Body: apiwire.BillingReserveWindowResult{Reservation: windowReservationResponse(reservation)}}, nil
}

func (h *Handler) settleWindow(ctx context.Context, input *body[apiwire.BillingSettleWindowRequest]) (*body[apiwire.BillingSettleResult], error) {
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

func (h *Handler) activateWindow(ctx context.Context, input *body[apiwire.BillingActivateWindowRequest]) (*body[apiwire.BillingActivateWindowResult], error) {
	client, err := h.requireClient()
	if err != nil {
		return nil, err
	}
	reservation, err := client.ActivateWindow(ctx, input.Body.WindowID, input.Body.ActivatedAt)
	if err != nil {
		return nil, h.activateWindowError(ctx, input.Body.WindowID, err)
	}
	return &body[apiwire.BillingActivateWindowResult]{Body: apiwire.BillingActivateWindowResult{Reservation: windowReservationResponse(reservation)}}, nil
}

func (h *Handler) voidWindow(ctx context.Context, input *body[apiwire.BillingVoidWindowRequest]) (*body[apiwire.BillingVoidWindowResult], error) {
	client, err := h.requireClient()
	if err != nil {
		return nil, err
	}
	if err := client.VoidWindow(ctx, input.Body.WindowID); err != nil {
		return nil, h.voidWindowError(ctx, input.Body.WindowID, err)
	}
	return &body[apiwire.BillingVoidWindowResult]{Body: apiwire.BillingVoidWindowResult{WindowID: input.Body.WindowID}}, nil
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

func (h *Handler) settleWindowError(ctx context.Context, input apiwire.BillingSettleWindowRequest, err error) error {
	switch {
	case errors.Is(err, billing.ErrWindowNotFound):
		return huma.Error404NotFound("window not found")
	case errors.Is(err, billing.ErrWindowAlreadyVoided):
		return huma.Error400BadRequest("window already voided")
	case errors.Is(err, billing.ErrWindowNotActivated):
		return huma.Error400BadRequest("window not activated")
	default:
		h.logError(ctx, "settle billing window", "window_id", input.WindowID, "actual_quantity", input.ActualQuantity, "error", err)
		return huma.Error500InternalServerError("settle", err)
	}
}

func (h *Handler) activateWindowError(ctx context.Context, windowID string, err error) error {
	switch {
	case errors.Is(err, billing.ErrWindowNotFound):
		return huma.Error404NotFound("window not found")
	case errors.Is(err, billing.ErrWindowAlreadySettled):
		return huma.Error400BadRequest("window already settled")
	case errors.Is(err, billing.ErrWindowAlreadyVoided):
		return huma.Error400BadRequest("window already voided")
	default:
		h.logError(ctx, "activate billing window", "window_id", windowID, "error", err)
		return huma.Error500InternalServerError("activate", err)
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
