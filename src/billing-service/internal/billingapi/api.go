package billingapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/forge-metal/apiwire"
	workloadauth "github.com/forge-metal/auth-middleware/workload"
	"github.com/forge-metal/billing-service/internal/billing"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
)

const (
	problemTypeNoStripeCustomer = "urn:forge-metal:problem:billing:no-stripe-customer"
)

type Config struct {
	Version             string
	ListenAddr          string
	Client              *billing.Client
	Logger              *slog.Logger
	InternalPeers       []spiffeid.ID
	StripeWebhookSecret string
}

type Handler struct {
	client              *billing.Client
	logger              *slog.Logger
	internalPeers       []spiffeid.ID
	stripeWebhookSecret string
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

type DocumentsInput struct {
	OrgPath
	ProductID string `query:"product_id,omitempty" maxLength:"255"`
}

type StatementInput struct {
	OrgPath
	ProductID string `query:"product_id" required:"true" minLength:"1" maxLength:"255"`
}

type ProductPath struct {
	ProductID string `path:"product_id" minLength:"1" maxLength:"255"`
}

type ContractPath struct {
	ContractID string `path:"contract_id" minLength:"1" maxLength:"255"`
}

type CreateContractChangeInput struct {
	ContractPath
	Body apiwire.BillingCreateContractChangeRequest `required:"true"`
}

type CancelContractInput struct {
	ContractPath
	Body apiwire.BillingCancelContractRequest `required:"true"`
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

func OpenAPIYAML() ([]byte, error) {
	api := NewAPI(http.NewServeMux(), Config{Version: "2.0.0"})
	api.OpenAPI().Servers = []*huma.Server{{URL: "https://billing.api.anveio.com"}}
	return api.OpenAPI().YAML()
}

func OpenAPIDowngradeYAML() ([]byte, error) {
	api := NewAPI(http.NewServeMux(), Config{Version: "2.0.0"})
	api.OpenAPI().Servers = []*huma.Server{{URL: "https://billing.api.anveio.com"}}
	return api.OpenAPI().DowngradeYAML()
}

func RegisterRoutes(api huma.API, cfg Config) {
	h := &Handler{client: cfg.Client, logger: cfg.Logger, internalPeers: cfg.InternalPeers, stripeWebhookSecret: cfg.StripeWebhookSecret}
	public := huma.NewGroup(api, "/internal/billing/v1")
	huma.Get(public, "/orgs/{org_id}/entitlements", h.getEntitlements, op("get-entitlements", "Get org entitlements view"))
	huma.Get(public, "/orgs/{org_id}/grants", h.listGrants, op("list-grants", "List org credit grants"))
	huma.Get(public, "/orgs/{org_id}/documents", h.listDocuments, op("list-documents", "List issued billing documents"))
	huma.Get(public, "/orgs/{org_id}/statement", h.getStatement, op("get-statement", "Preview current statement"))
	huma.Get(public, "/orgs/{org_id}/contracts", h.listContracts, op("list-contracts", "List org contracts"))
	huma.Get(public, "/products/{product_id}/plans", h.listPlans, op("list-plans", "List active plans"))
	huma.Post(public, "/checkout", h.createCheckout, op("create-checkout", "Create credit checkout"))
	huma.Post(public, "/contracts", h.createContract, op("create-contract", "Create contract checkout"))
	huma.Post(public, "/contracts/{contract_id}/changes", h.createContractChange, op("create-contract-change", "Create contract change"))
	huma.Post(public, "/contracts/{contract_id}/cancel", h.cancelContract, op("cancel-contract", "Cancel contract"))
	huma.Post(public, "/portal", h.createPortal, op("create-portal", "Create Stripe portal session"))

	service := huma.NewGroup(api, "/internal/billing/v1")
	service.UseMiddleware(requireInternalPeerMiddleware(api, h.internalPeers))
	huma.Post(service, "/reserve", h.reserveWindow, op("reserve-window", "Reserve billing window", http.StatusPaymentRequired, http.StatusForbidden))
	huma.Post(service, "/activate", h.activateWindow, op("activate-window", "Activate billing window", http.StatusNotFound))
	huma.Post(service, "/settle", h.settleWindow, op("settle-window", "Settle billing window", http.StatusNotFound))
	huma.Post(service, "/void", h.voidWindow, op("void-window", "Void billing window", http.StatusNotFound))

	// Caddy exposes only this path publicly; Huma keeps the OpenAPI surface focused on internal callers.
	api.Adapter().Handle(&huma.Operation{OperationID: "stripe-webhook", Method: http.MethodPost, Path: "/webhooks/stripe", Hidden: true}, h.stripeWebhook)
}

func op(id, summary string, errors ...int) func(*huma.Operation) {
	return func(operation *huma.Operation) {
		operation.OperationID = id
		operation.Summary = summary
		operation.Errors = errors
	}
}

func (h *Handler) getEntitlements(ctx context.Context, input *OrgPath) (*body[apiwire.BillingEntitlementsView], error) {
	orgID, err := billingOrgID(input.OrgID)
	if err != nil {
		return nil, err
	}
	view, err := h.client.ListEntitlementsView(ctx, orgID)
	if err != nil {
		return nil, h.internalError(ctx, "get entitlements", err)
	}
	return &body[apiwire.BillingEntitlementsView]{Body: entitlementsResponse(view)}, nil
}

func (h *Handler) listGrants(ctx context.Context, input *GrantsInput) (*body[apiwire.BillingGrants], error) {
	orgID, err := billingOrgID(input.OrgID)
	if err != nil {
		return nil, err
	}
	grants, err := h.client.ListGrantBalances(ctx, orgID, input.ProductID)
	if err != nil {
		return nil, h.internalError(ctx, "list grants", err)
	}
	out := make([]apiwire.BillingGrant, 0, len(grants))
	for _, grant := range grants {
		out = append(out, apiwire.BillingGrant{GrantID: grant.GrantID, ScopeType: grant.ScopeType, ScopeProductID: grant.ScopeProductID, ScopeBucketID: grant.ScopeBucketID, ScopeSKUID: grant.ScopeSKUID, Source: grant.Source, SourceReferenceID: grant.SourceReferenceID, EntitlementPeriodID: grant.EntitlementPeriodID, PolicyVersion: grant.PolicyVersion, StartsAt: grant.StartsAt, PeriodStart: grant.PeriodStart, PeriodEnd: grant.PeriodEnd, Available: apiwire.Uint64(grant.Available), Pending: apiwire.Uint64(grant.Pending), ExpiresAt: grant.ExpiresAt})
	}
	return &body[apiwire.BillingGrants]{Body: apiwire.BillingGrants{Grants: out}}, nil
}

func (h *Handler) listDocuments(ctx context.Context, input *DocumentsInput) (*body[apiwire.BillingDocuments], error) {
	orgID, err := billingOrgID(input.OrgID)
	if err != nil {
		return nil, err
	}
	documents, err := h.client.ListDocuments(ctx, orgID, input.ProductID)
	if err != nil {
		return nil, h.internalError(ctx, "list documents", err)
	}
	out := make([]apiwire.BillingDocument, 0, len(documents))
	for _, document := range documents {
		out = append(out, documentResponse(document))
	}
	return &body[apiwire.BillingDocuments]{Body: apiwire.BillingDocuments{Documents: out}}, nil
}

func (h *Handler) getStatement(ctx context.Context, input *StatementInput) (*body[apiwire.BillingStatement], error) {
	orgID, err := billingOrgID(input.OrgID)
	if err != nil {
		return nil, err
	}
	statement, err := h.client.PreviewStatement(ctx, orgID, input.ProductID)
	if err != nil {
		return nil, h.internalError(ctx, "get statement", err)
	}
	return &body[apiwire.BillingStatement]{Body: statementResponse(statement)}, nil
}

func (h *Handler) listContracts(ctx context.Context, input *OrgPath) (*body[apiwire.BillingContracts], error) {
	orgID, err := billingOrgID(input.OrgID)
	if err != nil {
		return nil, err
	}
	contracts, err := h.client.ListContracts(ctx, orgID)
	if err != nil {
		return nil, h.internalError(ctx, "list contracts", err)
	}
	out := make([]apiwire.BillingContract, 0, len(contracts))
	for _, contract := range contracts {
		out = append(out, contractResponse(contract))
	}
	return &body[apiwire.BillingContracts]{Body: apiwire.BillingContracts{Contracts: out}}, nil
}

func (h *Handler) listPlans(ctx context.Context, input *ProductPath) (*body[apiwire.BillingPlans], error) {
	plans, err := h.client.ListPlans(ctx, input.ProductID)
	if err != nil {
		return nil, h.internalError(ctx, "list plans", err)
	}
	out := make([]apiwire.BillingPlan, 0, len(plans))
	for _, plan := range plans {
		out = append(out, apiwire.BillingPlan{PlanID: plan.PlanID, ProductID: plan.ProductID, DisplayName: plan.DisplayName, BillingMode: plan.BillingMode, Tier: plan.Tier, Currency: plan.Currency, MonthlyAmountCents: apiwire.Uint64(plan.MonthlyAmountCents), AnnualAmountCents: apiwire.Uint64(plan.AnnualAmountCents), Active: plan.Active, IsDefault: plan.IsDefault})
	}
	return &body[apiwire.BillingPlans]{Body: apiwire.BillingPlans{Plans: out}}, nil
}

func (h *Handler) createCheckout(ctx context.Context, input *body[apiwire.BillingCreateCheckoutRequest]) (*body[apiwire.BillingURLResponse], error) {
	orgID, err := billingOrgIDFromWire(input.Body.OrgID)
	if err != nil {
		return nil, err
	}
	url, err := h.client.CreateCheckoutSession(ctx, orgID, input.Body.ProductID, billing.CheckoutParams{AmountCents: input.Body.AmountCents, SuccessURL: input.Body.SuccessURL, CancelURL: input.Body.CancelURL})
	if err != nil {
		return nil, h.internalError(ctx, "create checkout", err)
	}
	return &body[apiwire.BillingURLResponse]{Body: apiwire.BillingURLResponse{URL: url}}, nil
}

func (h *Handler) createContract(ctx context.Context, input *body[apiwire.BillingCreateContractRequest]) (*body[apiwire.BillingURLResponse], error) {
	orgID, err := billingOrgIDFromWire(input.Body.OrgID)
	if err != nil {
		return nil, err
	}
	url, err := h.client.CreateContract(ctx, orgID, input.Body.PlanID, billing.BillingCadence(input.Body.Cadence), input.Body.SuccessURL, input.Body.CancelURL)
	if err != nil {
		if errors.Is(err, billing.ErrUnsupportedCadence) || errors.Is(err, billing.ErrUnsupportedChange) {
			return nil, huma.Error400BadRequest("unsupported contract request", err)
		}
		return nil, h.internalError(ctx, "create contract", err)
	}
	return &body[apiwire.BillingURLResponse]{Body: apiwire.BillingURLResponse{URL: url}}, nil
}

func (h *Handler) createContractChange(ctx context.Context, input *CreateContractChangeInput) (*body[apiwire.BillingContractChangeResponse], error) {
	orgID, err := billingOrgIDFromWire(input.Body.OrgID)
	if err != nil {
		return nil, err
	}
	result, err := h.client.CreateContractChange(ctx, orgID, input.ContractID, billing.ContractChangeRequest{TargetPlanID: input.Body.TargetPlanID, SuccessURL: input.Body.SuccessURL, CancelURL: input.Body.CancelURL})
	if err != nil {
		if errors.Is(err, billing.ErrContractNotFound) {
			return nil, huma.Error404NotFound("contract not found")
		}
		if errors.Is(err, billing.ErrUnsupportedChange) {
			return nil, huma.Error400BadRequest("unsupported contract change", err)
		}
		return nil, h.internalError(ctx, "create contract change", err)
	}
	return &body[apiwire.BillingContractChangeResponse]{Body: apiwire.BillingContractChangeResponse{URL: result.URL, ChangeID: result.ChangeID, FinalizationID: result.FinalizationID, DocumentID: result.DocumentID, Status: result.Status, PriceDelta: apiwire.Uint64(result.PriceDeltaUnits)}}, nil
}

func (h *Handler) cancelContract(ctx context.Context, input *CancelContractInput) (*body[apiwire.BillingCancelContractResponse], error) {
	orgID, err := billingOrgIDFromWire(input.Body.OrgID)
	if err != nil {
		return nil, err
	}
	contract, err := h.client.CancelContract(ctx, orgID, input.ContractID)
	if err != nil {
		if errors.Is(err, billing.ErrContractNotFound) {
			return nil, huma.Error404NotFound("contract not found")
		}
		return nil, h.internalError(ctx, "cancel contract", err)
	}
	return &body[apiwire.BillingCancelContractResponse]{Body: apiwire.BillingCancelContractResponse{Contract: contractResponse(contract)}}, nil
}

func (h *Handler) createPortal(ctx context.Context, input *body[apiwire.BillingCreatePortalSessionRequest]) (*body[apiwire.BillingURLResponse], error) {
	orgID, err := billingOrgIDFromWire(input.Body.OrgID)
	if err != nil {
		return nil, err
	}
	url, err := h.client.CreatePortalSession(ctx, orgID, input.Body.ReturnURL)
	if err != nil {
		if errors.Is(err, billing.ErrNoStripeCustomer) {
			problem := huma.Error422UnprocessableEntity("no stripe customer linked to this org", err)
			if model, ok := problem.(*huma.ErrorModel); ok {
				model.Type = problemTypeNoStripeCustomer
			}
			return nil, problem
		}
		return nil, h.internalError(ctx, "create portal", err)
	}
	return &body[apiwire.BillingURLResponse]{Body: apiwire.BillingURLResponse{URL: url}}, nil
}

func (h *Handler) reserveWindow(ctx context.Context, input *body[apiwire.BillingReserveWindowRequest]) (*body[apiwire.BillingReserveWindowResult], error) {
	orgID, err := billingOrgIDFromWire(input.Body.OrgID)
	if err != nil {
		return nil, err
	}
	reservation, err := h.client.ReserveWindow(ctx, billing.ReserveRequest{OrgID: orgID, ProductID: input.Body.ProductID, ActorID: input.Body.ActorID, ConcurrentCount: input.Body.ConcurrentCount, SourceType: input.Body.SourceType, SourceRef: input.Body.SourceRef, WindowSeq: input.Body.WindowSeq, ReservationShape: input.Body.ReservationShape, ReservedQuantity: input.Body.ReservedQuantity, BillingJobID: input.Body.BillingJobID, Allocation: input.Body.Allocation})
	if err != nil {
		return nil, h.windowError(ctx, "reserve", err)
	}
	return &body[apiwire.BillingReserveWindowResult]{Body: apiwire.BillingReserveWindowResult{Reservation: reservationResponse(reservation)}}, nil
}

func (h *Handler) activateWindow(ctx context.Context, input *body[apiwire.BillingActivateWindowRequest]) (*body[apiwire.BillingActivateWindowResult], error) {
	reservation, err := h.client.ActivateWindow(ctx, input.Body.WindowID, input.Body.ActivatedAt)
	if err != nil {
		return nil, h.windowError(ctx, "activate", err)
	}
	return &body[apiwire.BillingActivateWindowResult]{Body: apiwire.BillingActivateWindowResult{Reservation: reservationResponse(reservation)}}, nil
}

func (h *Handler) settleWindow(ctx context.Context, input *body[apiwire.BillingSettleWindowRequest]) (*body[apiwire.BillingSettleResult], error) {
	result, err := h.client.SettleWindow(ctx, input.Body.WindowID, input.Body.ActualQuantity, input.Body.UsageSummary)
	if err != nil {
		return nil, h.windowError(ctx, "settle", err)
	}
	return &body[apiwire.BillingSettleResult]{Body: apiwire.BillingSettleResult{WindowID: result.WindowID, ActualQuantity: result.ActualQuantity, BillableQuantity: result.BillableQuantity, WriteoffQuantity: result.WriteoffQuantity, BilledChargeUnits: apiwire.Uint64(result.BilledChargeUnits), WriteoffChargeUnits: apiwire.Uint64(result.WriteoffChargeUnits), SettledAt: result.SettledAt}}, nil
}

func (h *Handler) voidWindow(ctx context.Context, input *body[apiwire.BillingVoidWindowRequest]) (*body[apiwire.BillingVoidWindowResult], error) {
	if err := h.client.VoidWindow(ctx, input.Body.WindowID); err != nil {
		return nil, h.windowError(ctx, "void", err)
	}
	return &body[apiwire.BillingVoidWindowResult]{Body: apiwire.BillingVoidWindowResult{WindowID: input.Body.WindowID}}, nil
}

func (h *Handler) stripeWebhook(ctx huma.Context) {
	payload, err := io.ReadAll(ctx.BodyReader())
	if err != nil {
		writePlainError(ctx, http.StatusBadRequest, "read webhook body")
		return
	}
	if h.stripeWebhookSecret == "" {
		writePlainError(ctx, http.StatusInternalServerError, "stripe webhook secret is not configured")
		return
	}
	if err := h.client.HandleStripeWebhook(ctx.Context(), payload, ctx.Header("Stripe-Signature"), h.stripeWebhookSecret); err != nil {
		writePlainError(ctx, http.StatusBadRequest, "stripe webhook rejected")
		return
	}
	ctx.SetStatus(http.StatusNoContent)
}

func writePlainError(ctx huma.Context, status int, message string) {
	ctx.SetStatus(status)
	ctx.SetHeader("Content-Type", "text/plain; charset=utf-8")
	_, _ = ctx.BodyWriter().Write([]byte(message))
}

func (h *Handler) windowError(ctx context.Context, op string, err error) error {
	switch {
	case errors.Is(err, billing.ErrInsufficientBalance), errors.Is(err, billing.ErrPaymentRequired):
		return huma.Error402PaymentRequired(op, err)
	case errors.Is(err, billing.ErrOrgSuspended), errors.Is(err, billing.ErrForbidden):
		return huma.Error403Forbidden(op, err)
	case errors.Is(err, billing.ErrWindowNotFound):
		return huma.Error404NotFound("window not found", err)
	case errors.Is(err, billing.ErrWindowNotReserved), errors.Is(err, billing.ErrWindowNotActivated), errors.Is(err, billing.ErrWindowAlreadySettled), errors.Is(err, billing.ErrWindowAlreadyVoided):
		return huma.Error400BadRequest(op, err)
	default:
		return h.internalError(ctx, op, err)
	}
}

func statementResponse(statement billing.Statement) apiwire.BillingStatement {
	items := make([]apiwire.BillingStatementLineItem, 0, len(statement.LineItems))
	for _, line := range statement.LineItems {
		items = append(items, apiwire.BillingStatementLineItem{ProductID: line.ProductID, PlanID: line.PlanID, BucketID: line.BucketID, BucketDisplayName: line.BucketDisplayName, SKUID: line.SKUID, SKUDisplayName: line.SKUDisplayName, QuantityUnit: line.QuantityUnit, PricingPhase: line.PricingPhase, Quantity: line.Quantity, UnitRate: apiwire.Uint64(line.UnitRate), ChargeUnits: apiwire.Uint64(line.ChargeUnits), FreeTierUnits: apiwire.Uint64(line.FreeTierUnits), ContractUnits: apiwire.Uint64(line.ContractUnits), PurchaseUnits: apiwire.Uint64(line.PurchaseUnits), PromoUnits: apiwire.Uint64(line.PromoUnits), RefundUnits: apiwire.Uint64(line.RefundUnits), ReceivableUnits: apiwire.Uint64(line.ReceivableUnits), ReservedUnits: apiwire.Uint64(line.ReservedUnits)})
	}
	summaries := make([]apiwire.BillingStatementGrantSummary, 0, len(statement.GrantSummaries))
	for _, summary := range statement.GrantSummaries {
		summaries = append(summaries, apiwire.BillingStatementGrantSummary{ScopeType: summary.ScopeType, ScopeProductID: summary.ScopeProductID, ScopeBucketID: summary.ScopeBucketID, Source: summary.Source, Available: apiwire.Uint64(summary.Available), Pending: apiwire.Uint64(summary.Pending)})
	}
	return apiwire.BillingStatement{OrgID: apiwire.Uint64(uint64(statement.OrgID)), ProductID: statement.ProductID, PeriodStart: statement.PeriodStart, PeriodEnd: statement.PeriodEnd, PeriodSource: statement.PeriodSource, GeneratedAt: statement.GeneratedAt, Currency: statement.Currency, UnitLabel: statement.UnitLabel, LineItems: items, GrantSummaries: summaries, Totals: apiwire.BillingStatementTotals{ChargeUnits: apiwire.Uint64(statement.Totals.ChargeUnits), FreeTierUnits: apiwire.Uint64(statement.Totals.FreeTierUnits), ContractUnits: apiwire.Uint64(statement.Totals.ContractUnits), PurchaseUnits: apiwire.Uint64(statement.Totals.PurchaseUnits), PromoUnits: apiwire.Uint64(statement.Totals.PromoUnits), RefundUnits: apiwire.Uint64(statement.Totals.RefundUnits), ReceivableUnits: apiwire.Uint64(statement.Totals.ReceivableUnits), ReservedUnits: apiwire.Uint64(statement.Totals.ReservedUnits), TotalDueUnits: apiwire.Uint64(statement.Totals.TotalDueUnits)}}
}

func documentResponse(document billing.DocumentRecord) apiwire.BillingDocument {
	return apiwire.BillingDocument{
		DocumentID:             document.DocumentID,
		DocumentNumber:         document.DocumentNumber,
		DocumentKind:           document.DocumentKind,
		FinalizationID:         document.FinalizationID,
		ProductID:              document.ProductID,
		CycleID:                document.CycleID,
		Status:                 document.Status,
		PaymentStatus:          document.PaymentStatus,
		PeriodStart:            document.PeriodStart,
		PeriodEnd:              document.PeriodEnd,
		IssuedAt:               document.IssuedAt,
		Currency:               document.Currency,
		SubtotalUnits:          apiwire.Uint64(document.SubtotalUnits),
		AdjustmentUnits:        apiwire.Int64(document.AdjustmentUnits),
		TaxUnits:               apiwire.Uint64(document.TaxUnits),
		TotalDueUnits:          apiwire.Uint64(document.TotalDueUnits),
		StripeHostedInvoiceURL: document.StripeHostedInvoiceURL,
		StripeInvoicePDFURL:    document.StripeInvoicePDFURL,
		StripePaymentIntentID:  document.StripePaymentIntentID,
	}
}

func entitlementsResponse(view billing.EntitlementsView) apiwire.BillingEntitlementsView {
	products := make([]apiwire.BillingEntitlementProductSection, 0, len(view.Products))
	for _, product := range view.Products {
		buckets := make([]apiwire.BillingEntitlementBucketSection, 0, len(product.Buckets))
		for _, bucket := range product.Buckets {
			skus := make([]apiwire.BillingEntitlementSlot, 0, len(bucket.SKUSlots))
			for _, slot := range bucket.SKUSlots {
				skus = append(skus, entitlementSlot(slot))
			}
			buckets = append(buckets, apiwire.BillingEntitlementBucketSection{BucketID: bucket.BucketID, DisplayName: bucket.DisplayName, BucketSlot: entitlementSlotPtr(bucket.BucketSlot), SKUSlots: skus})
		}
		products = append(products, apiwire.BillingEntitlementProductSection{ProductID: product.ProductID, DisplayName: product.DisplayName, ProductSlot: entitlementSlotPtr(product.ProductSlot), Buckets: buckets})
	}
	return apiwire.BillingEntitlementsView{OrgID: apiwire.Uint64(uint64(view.OrgID)), Universal: entitlementSlot(view.Universal), Products: products}
}

func entitlementSlotPtr(slot *billing.EntitlementSlot) *apiwire.BillingEntitlementSlot {
	if slot == nil {
		return nil
	}
	out := entitlementSlot(*slot)
	return &out
}

func entitlementSlot(slot billing.EntitlementSlot) apiwire.BillingEntitlementSlot {
	sources := make([]apiwire.BillingEntitlementSourceTotal, 0, len(slot.Sources))
	for _, source := range slot.Sources {
		sources = append(sources, apiwire.BillingEntitlementSourceTotal{Source: source.Source, PlanID: source.PlanID, Label: source.Label, PeriodStartUnits: apiwire.Uint64(source.PeriodStartUnits), AvailableUnits: apiwire.Uint64(source.AvailableUnits), PendingUnits: apiwire.Uint64(source.PendingUnits), InlineExpiresAt: source.InlineExpiresAt})
	}
	return apiwire.BillingEntitlementSlot{ScopeType: slot.ScopeType, ProductID: slot.ProductID, ProductDisplay: slot.ProductDisplay, BucketID: slot.BucketID, BucketDisplay: slot.BucketDisplay, SKUID: slot.SKUID, SKUDisplay: slot.SKUDisplay, CoverageLabel: slot.CoverageLabel, PeriodStartUnits: apiwire.Uint64(slot.PeriodStartUnits), SpentUnits: apiwire.Uint64(slot.SpentUnits), PendingUnits: apiwire.Uint64(slot.PendingUnits), AvailableUnits: apiwire.Uint64(slot.AvailableUnits), Sources: sources}
}

func contractResponse(contract billing.ContractRecord) apiwire.BillingContract {
	return apiwire.BillingContract{ContractID: contract.ContractID, ProductID: contract.ProductID, PlanID: contract.PlanID, PhaseID: contract.PhaseID, CadenceKind: contract.CadenceKind, Status: contract.Status, PaymentState: contract.PaymentState, EntitlementState: contract.EntitlementState, PendingChangeID: contract.PendingChangeID, PendingChangeType: contract.PendingChangeType, PendingChangeTargetPlanID: contract.PendingChangeTargetPlanID, PendingChangeEffectiveAt: contract.PendingChangeEffectiveAt, StartsAt: contract.StartsAt, EndsAt: contract.EndsAt, PhaseStart: contract.PhaseStart, PhaseEnd: contract.PhaseEnd}
}

func reservationResponse(reservation billing.WindowReservation) apiwire.BillingWindowReservation {
	rates := map[string]apiwire.DecimalUint64{}
	for sku, rate := range reservation.SKURates {
		rates[sku] = apiwire.Uint64(rate)
	}
	return apiwire.BillingWindowReservation{WindowID: reservation.WindowID, OrgID: apiwire.Uint64(uint64(reservation.OrgID)), ProductID: reservation.ProductID, PlanID: reservation.PlanID, ActorID: reservation.ActorID, SourceType: reservation.SourceType, SourceRef: reservation.SourceRef, WindowSeq: reservation.WindowSeq, ReservationShape: reservation.ReservationShape, ReservedQuantity: reservation.ReservedQuantity, ReservedChargeUnits: apiwire.Uint64(reservation.ReservedChargeUnits), PricingPhase: reservation.PricingPhase, Allocation: reservation.Allocation, SKURates: rates, CostPerUnit: apiwire.Uint64(reservation.CostPerUnit), WindowStart: reservation.WindowStart, ActivatedAt: reservation.ActivatedAt, ExpiresAt: reservation.ExpiresAt, RenewBy: reservation.RenewBy}
}

func billingOrgID(id string) (billing.OrgID, error) {
	parsed, err := strconv.ParseUint(id, 10, 64)
	if err != nil || parsed == 0 {
		return 0, huma.Error400BadRequest("invalid org_id", err)
	}
	return billing.OrgID(parsed), nil
}

func billingOrgIDFromWire(id apiwire.DecimalUint64) (billing.OrgID, error) {
	if id.Uint64() == 0 {
		return 0, huma.Error400BadRequest("org_id must be positive")
	}
	return billing.OrgID(id.Uint64()), nil
}

func (h *Handler) internalError(ctx context.Context, operation string, err error) error {
	if h.logger != nil {
		h.logger.ErrorContext(ctx, "billing api "+operation, "error", err)
	}
	return huma.Error500InternalServerError(operation, err)
}

func requireInternalPeerMiddleware(api huma.API, peers []spiffeid.ID) func(huma.Context, func(huma.Context)) {
	allowed := map[spiffeid.ID]struct{}{}
	for _, peer := range peers {
		if !peer.IsZero() {
			allowed[peer] = struct{}{}
		}
	}
	return func(ctx huma.Context, next func(huma.Context)) {
		peerID, ok := workloadauth.PeerIDFromContext(ctx.Context())
		if !ok {
			huma.WriteErr(api, ctx, http.StatusUnauthorized, "missing spiffe peer")
			return
		}
		if _, ok := allowed[peerID]; ok {
			next(ctx)
			return
		}
		huma.WriteErr(api, ctx, http.StatusForbidden, "unexpected spiffe peer")
	}
}
