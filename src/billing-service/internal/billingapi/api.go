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

type ProductPath struct {
	ProductID string `path:"product_id" minLength:"1" maxLength:"255"`
}

type SubscriptionPath struct {
	SubscriptionID string `path:"subscription_id" pattern:"^[0-9]+$"`
}

type CancelSubscriptionInput struct {
	SubscriptionPath
	Body apiwire.BillingCancelSubscriptionRequest `required:"true"`
}

type (
	GrantResponse              = apiwire.BillingGrant
	GrantsResponse             = apiwire.BillingGrants
	StatementResponse          = apiwire.BillingStatement
	SubscriptionsResponse      = apiwire.BillingSubscriptions
	SubscriptionResponse       = apiwire.BillingSubscription
	PlansResponse              = apiwire.BillingPlans
	PlanResponse               = apiwire.BillingPlan
	CancelSubscriptionResponse = apiwire.BillingCancelSubscriptionResponse
	EntitlementsResponse       = apiwire.BillingEntitlementsView
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
	huma.Get(public, "/orgs/{org_id}/entitlements", handler.getEntitlements, operation("get-entitlements", "Get the entitlements view for an org"))
	huma.Get(public, "/orgs/{org_id}/grants", handler.listGrants, operation("list-grants", "List credit grants for an org"))
	huma.Get(public, "/orgs/{org_id}/statement", handler.getStatement, operation("get-statement", "Get current billing statement for an org"))
	huma.Get(public, "/orgs/{org_id}/subscriptions", handler.listSubscriptions, operation("list-subscriptions", "List subscriptions for an org"))
	huma.Get(public, "/products/{product_id}/plans", handler.listPlans, operation("list-plans", "List active subscription plans for a product"))
	huma.Post(public, "/checkout", handler.createCheckout, operation("create-checkout", "Create a Stripe checkout session"))
	huma.Post(public, "/subscribe", handler.createSubscription, operation("create-subscription", "Create a Stripe subscription checkout", http.StatusInternalServerError))
	huma.Post(public, "/portal", handler.createPortal, operation("create-portal", "Create a Stripe customer portal session", http.StatusUnprocessableEntity, http.StatusInternalServerError))
	huma.Post(public, "/subscriptions/{subscription_id}/cancel", handler.cancelSubscription, operation("cancel-subscription", "Cancel a Stripe subscription", http.StatusNotFound, http.StatusInternalServerError))

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

func (h *Handler) getEntitlements(ctx context.Context, input *OrgPath) (*body[EntitlementsResponse], error) {
	client, err := h.requireClient()
	if err != nil {
		return nil, err
	}
	orgID, err := billingOrgID(input.OrgID)
	if err != nil {
		return nil, err
	}
	view, err := client.ListEntitlementsView(ctx, orgID)
	if err != nil {
		return nil, h.internalServerError(ctx, "get entitlements", err, "org_id", uint64(orgID))
	}
	return &body[EntitlementsResponse]{Body: entitlementsResponse(orgID, view)}, nil
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
		return nil, h.internalServerError(ctx, "list grants", err, "org_id", uint64(orgID), "product_id", input.ProductID)
	}
	out := make([]GrantResponse, 0, len(grants))
	for _, grant := range grants {
		out = append(out, GrantResponse{
			GrantID:             grant.GrantID.String(),
			ScopeType:           grant.ScopeType.String(),
			ScopeProductID:      grant.ScopeProductID,
			ScopeBucketID:       grant.ScopeBucketID,
			ScopeSKUID:          grant.ScopeSKUID,
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
		return nil, h.internalServerError(ctx, "get statement", err, "org_id", uint64(orgID), "product_id", input.ProductID)
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

func entitlementsResponse(orgID billing.OrgID, view billing.EntitlementsView) apiwire.BillingEntitlementsView {
	return apiwire.BillingEntitlementsView{
		OrgID:     apiwire.Uint64(uint64(orgID)),
		Universal: entitlementPoolList(view.Universal),
		Products:  entitlementProductSections(view.Products),
	}
}

func entitlementProductSections(in []billing.EntitlementProductSection) []apiwire.BillingEntitlementProductSection {
	out := make([]apiwire.BillingEntitlementProductSection, 0, len(in))
	for _, section := range in {
		out = append(out, apiwire.BillingEntitlementProductSection{
			ProductID:    section.ProductID,
			DisplayName:  section.DisplayName,
			ProductPools: entitlementPoolList(section.ProductPools),
			Buckets:      entitlementBucketSections(section.Buckets),
		})
	}
	return out
}

func entitlementBucketSections(in []billing.EntitlementBucketSection) []apiwire.BillingEntitlementBucketSection {
	out := make([]apiwire.BillingEntitlementBucketSection, 0, len(in))
	for _, section := range in {
		out = append(out, apiwire.BillingEntitlementBucketSection{
			BucketID:    section.BucketID,
			DisplayName: section.DisplayName,
			Pools:       entitlementPoolList(section.Pools),
		})
	}
	return out
}

func entitlementPoolList(in []billing.EntitlementPool) []apiwire.BillingEntitlementPool {
	out := make([]apiwire.BillingEntitlementPool, 0, len(in))
	for _, pool := range in {
		entries := make([]apiwire.BillingEntitlementGrantEntry, 0, len(pool.Entries))
		for _, entry := range pool.Entries {
			entries = append(entries, apiwire.BillingEntitlementGrantEntry{
				GrantID:     entry.GrantID,
				Available:   apiwire.Uint64(entry.Available),
				Pending:     apiwire.Uint64(entry.Pending),
				StartsAt:    entry.StartsAt,
				PeriodStart: entry.PeriodStart,
				PeriodEnd:   entry.PeriodEnd,
				ExpiresAt:   entry.ExpiresAt,
			})
		}
		out = append(out, apiwire.BillingEntitlementPool{
			ScopeType:      pool.ScopeType.String(),
			ProductID:      pool.ProductID,
			ProductDisplay: pool.ProductDisplay,
			BucketID:       pool.BucketID,
			BucketDisplay:  pool.BucketDisplay,
			SKUID:          pool.SKUID,
			SKUDisplay:     pool.SKUDisplay,
			CoverageLabel:  pool.CoverageLabel,
			Source:         pool.Source.String(),
			SourceLabel:    billing.GrantSourceLabel(pool.Source),
			Entries:        entries,
		})
	}
	return out
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
		return nil, h.internalServerError(ctx, "list subscriptions", err, "org_id", uint64(orgID))
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

func (h *Handler) listPlans(ctx context.Context, input *ProductPath) (*body[PlansResponse], error) {
	client, err := h.requireClient()
	if err != nil {
		return nil, err
	}
	plans, err := client.ListPlans(ctx, input.ProductID)
	if err != nil {
		return nil, h.internalServerError(ctx, "list plans", err, "product_id", input.ProductID)
	}
	out := make([]PlanResponse, 0, len(plans))
	for _, plan := range plans {
		out = append(out, PlanResponse{
			PlanID:             plan.PlanID,
			ProductID:          plan.ProductID,
			DisplayName:        plan.DisplayName,
			BillingMode:        plan.BillingMode,
			Tier:               plan.Tier,
			Currency:           plan.Currency,
			MonthlyAmountCents: apiwire.Uint64(plan.MonthlyAmountCents),
			AnnualAmountCents:  apiwire.Uint64(plan.AnnualAmountCents),
			Active:             plan.Active,
			IsDefault:          plan.IsDefault,
		})
	}
	return &body[PlansResponse]{Body: PlansResponse{Plans: out}}, nil
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
		return nil, h.internalServerError(ctx, "create checkout", err, "org_id", uint64(orgID), "product_id", input.Body.ProductID)
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
		return nil, h.internalServerError(ctx, "create subscription checkout", err, "org_id", uint64(orgID), "plan_id", input.Body.PlanID, "cadence", input.Body.Cadence)
	}
	return &body[apiwire.BillingURLResponse]{Body: apiwire.BillingURLResponse{URL: url}}, nil
}

func (h *Handler) cancelSubscription(ctx context.Context, input *CancelSubscriptionInput) (*body[CancelSubscriptionResponse], error) {
	client, err := h.requireClient()
	if err != nil {
		return nil, err
	}
	orgID, err := billingOrgIDFromWire(input.Body.OrgID)
	if err != nil {
		return nil, err
	}
	subscriptionID, err := apiwire.ParseInt64(input.SubscriptionID)
	if err != nil {
		return nil, huma.Error400BadRequest("subscription_id must be positive", err)
	}
	if subscriptionID <= 0 {
		return nil, huma.Error400BadRequest("subscription_id must be positive")
	}
	subscription, err := client.CancelSubscription(ctx, orgID, subscriptionID)
	if err != nil {
		if errors.Is(err, billing.ErrSubscriptionNotFound) {
			return nil, huma.Error404NotFound("subscription not found", err)
		}
		return nil, h.internalServerError(ctx, "cancel subscription", err, "org_id", uint64(orgID), "subscription_id", subscriptionID)
	}
	return &body[CancelSubscriptionResponse]{Body: CancelSubscriptionResponse{
		Subscription: SubscriptionResponse{
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
		},
	}}, nil
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
		return nil, h.internalServerError(ctx, "create portal session", err, "org_id", uint64(orgID))
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
		return nil, h.reserveWindowError(ctx, err)
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

func (h *Handler) reserveWindowError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, billing.ErrInsufficientBalance):
		return huma.Error402PaymentRequired("reserve", err)
	case errors.Is(err, billing.ErrOrgSuspended):
		return huma.Error403Forbidden("reserve", err)
	default:
		return h.internalServerError(ctx, "reserve", err)
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
		return h.internalServerError(ctx, "settle", err, "window_id", input.WindowID, "actual_quantity", input.ActualQuantity)
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
		return h.internalServerError(ctx, "activate", err, "window_id", windowID)
	}
}

func (h *Handler) voidWindowError(ctx context.Context, windowID string, err error) error {
	switch {
	case errors.Is(err, billing.ErrWindowNotFound):
		return huma.Error404NotFound("window not found")
	case errors.Is(err, billing.ErrWindowAlreadySettled):
		return huma.Error400BadRequest("window already settled")
	default:
		return h.internalServerError(ctx, "void", err, "window_id", windowID)
	}
}

func (h *Handler) internalServerError(ctx context.Context, operation string, err error, attrs ...any) error {
	args := make([]any, 0, len(attrs)+2)
	args = append(args, attrs...)
	args = append(args, "error", err)
	h.logError(ctx, "billing api "+operation, args...)
	return huma.Error500InternalServerError(operation, err)
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
