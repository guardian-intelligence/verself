package billingapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/verself/billing-service/internal/billing"
	"github.com/verself/domain-transfer-objects"
	runtimeiam "github.com/verself/service-runtime/iam"
	workloadauth "github.com/verself/service-runtime/workload"
)

const (
	problemTypeNoStripeCustomer = "urn:verself:problem:billing:no-stripe-customer"
)

type Config struct {
	Version              string
	ListenAddr           string
	Client               *billing.Client
	Logger               *slog.Logger
	Authorizer           runtimeiam.OperationAuthorizer
	InternalPeers        []spiffeid.ID
	StripeWebhookSecret  string
	BillingReturnOrigins []string
	InstallationID       string
}

type Handler struct {
	client               *billing.Client
	logger               *slog.Logger
	internalPeers        []spiffeid.ID
	stripeWebhookSecret  string
	billingReturnOrigins []string
	installationID       string
}

type body[T any] struct {
	Body T `required:"true"`
}

type emptyInput struct{}

type GrantsInput struct {
	ProductID string `query:"product_id,omitempty" maxLength:"255"`
	Active    bool   `query:"active,omitempty"`
}

type DocumentsInput struct {
	ProductID string `query:"product_id,omitempty" maxLength:"255"`
}

type StatementInput struct {
	ProductID string `query:"product_id" required:"true" minLength:"1" maxLength:"255"`
}

type ProductQuery struct {
	ProductID string `query:"product_id" required:"true" minLength:"1" maxLength:"255"`
}

type ContractPath struct {
	ContractID string `path:"contract_id" minLength:"1" maxLength:"255"`
}

type CreateContractChangeInput struct {
	ContractPath
	Body dto.BillingCreateContractChangeRequest `required:"true"`
}

type CancelContractInput struct {
	ContractPath
}

func NewAPI(mux *http.ServeMux, cfg Config) huma.API {
	version := cfg.Version
	if version == "" {
		version = "2.0.0"
	}
	config := huma.DefaultConfig("Billing Service", version)
	if cfg.ListenAddr != "" {
		config.Servers = []*huma.Server{{URL: "http://" + cfg.ListenAddr}}
	}
	api := humago.New(mux, config)
	applyPublicSecurityScheme(api)
	RegisterPublicRoutes(api, cfg)
	dto.ApplyOpenAPIWireDefaults(api)
	return api
}

func OpenAPIYAML() ([]byte, error) {
	api := NewAPI(http.NewServeMux(), Config{Version: "2.0.0", InstallationID: "inst_openapi"})
	api.OpenAPI().Servers = []*huma.Server{{URL: "https://billing.api.verself.sh"}}
	return api.OpenAPI().YAML()
}

func OpenAPIDowngradeYAML() ([]byte, error) {
	api := NewAPI(http.NewServeMux(), Config{Version: "2.0.0", InstallationID: "inst_openapi"})
	api.OpenAPI().Servers = []*huma.Server{{URL: "https://billing.api.verself.sh"}}
	return api.OpenAPI().DowngradeYAML()
}

func NewInternalAPI(mux *http.ServeMux, cfg Config) huma.API {
	version := cfg.Version
	if version == "" {
		version = "2.0.0"
	}
	config := huma.DefaultConfig("Billing Internal API", version)
	if cfg.ListenAddr != "" {
		config.Servers = []*huma.Server{{URL: serverURL(cfg.ListenAddr)}}
	}
	api := humago.New(mux, config)
	applyInternalSecurityScheme(api)
	RegisterInternalRoutes(api, cfg)
	dto.ApplyOpenAPIWireDefaults(api)
	return api
}

func InternalOpenAPIYAML() ([]byte, error) {
	api := NewInternalAPI(http.NewServeMux(), Config{Version: "2.0.0", ListenAddr: "https://127.0.0.1:4255"})
	return api.OpenAPI().YAML()
}

func InternalOpenAPIDowngradeYAML() ([]byte, error) {
	api := NewInternalAPI(http.NewServeMux(), Config{Version: "2.0.0", ListenAddr: "https://127.0.0.1:4255"})
	return api.OpenAPI().DowngradeYAML()
}

func serverURL(addr string) string {
	if strings.Contains(addr, "://") {
		return addr
	}
	return "http://" + addr
}

func RegisterPublicRoutes(api huma.API, cfg Config) {
	h := &Handler{client: cfg.Client, logger: cfg.Logger, internalPeers: cfg.InternalPeers, stripeWebhookSecret: cfg.StripeWebhookSecret, billingReturnOrigins: cfg.BillingReturnOrigins, installationID: cfg.InstallationID}
	registerPublicBillingRoute(api, cfg.Authorizer, huma.Operation{OperationID: "get-billing-entitlements", Method: http.MethodGet, Path: "/api/v1/entitlements", Summary: "Get org entitlements view"}, readPolicy("billing_entitlements", "read", "billing.entitlements.read"), h.getEntitlements)
	registerPublicBillingRoute(api, cfg.Authorizer, huma.Operation{OperationID: "list-billing-grants", Method: http.MethodGet, Path: "/api/v1/grants", Summary: "List org credit grants"}, readPolicy("billing_grant", "list", "billing.grant.list"), h.listGrants)
	registerPublicBillingRoute(api, cfg.Authorizer, huma.Operation{OperationID: "list-billing-documents", Method: http.MethodGet, Path: "/api/v1/billing-documents", Summary: "List issued billing documents"}, readPolicy("billing_document", "list", "billing.document.list"), h.listDocuments)
	registerPublicBillingRoute(api, cfg.Authorizer, huma.Operation{OperationID: "get-billing-statement", Method: http.MethodGet, Path: "/api/v1/statement", Summary: "Preview current statement"}, readPolicy("billing_statement", "read", "billing.statement.read"), h.getStatement)
	registerPublicBillingRoute(api, cfg.Authorizer, huma.Operation{OperationID: "list-billing-contracts", Method: http.MethodGet, Path: "/api/v1/contracts", Summary: "List org contracts"}, readPolicy("billing_contract", "list", "billing.contract.list"), h.listContracts)
	registerPublicBillingRoute(api, cfg.Authorizer, huma.Operation{OperationID: "list-billing-plans", Method: http.MethodGet, Path: "/api/v1/plans", Summary: "List active plans"}, readPolicy("billing_plan", "list", "billing.plan.list"), h.listPlans)
	registerPublicBillingRoute(api, cfg.Authorizer, huma.Operation{OperationID: "create-billing-checkout", Method: http.MethodPost, Path: "/api/v1/checkout", Summary: "Create credit checkout", DefaultStatus: http.StatusOK}, checkoutPolicy("billing_checkout", "create", "billing.checkout.create"), h.createCheckout)
	registerPublicBillingRoute(api, cfg.Authorizer, huma.Operation{OperationID: "create-billing-contract", Method: http.MethodPost, Path: "/api/v1/contracts", Summary: "Create contract checkout", DefaultStatus: http.StatusOK}, checkoutPolicy("billing_contract_checkout", "create", "billing.contract_checkout.create"), h.createContract)
	registerPublicBillingRoute(api, cfg.Authorizer, huma.Operation{OperationID: "create-billing-contract-change", Method: http.MethodPost, Path: "/api/v1/contracts/{contract_id}/changes", Summary: "Create contract change", DefaultStatus: http.StatusOK}, checkoutPolicy("billing_contract_change", "create", "billing.contract_change.create"), h.createContractChange)
	registerPublicBillingRoute(api, cfg.Authorizer, huma.Operation{OperationID: "cancel-billing-contract", Method: http.MethodPost, Path: "/api/v1/contracts/{contract_id}/cancel", Summary: "Cancel contract", DefaultStatus: http.StatusOK}, checkoutPolicy("billing_contract", "cancel", "billing.contract.cancel"), h.cancelContract)
	registerPublicBillingRoute(api, cfg.Authorizer, huma.Operation{OperationID: "create-billing-portal", Method: http.MethodPost, Path: "/api/v1/portal", Summary: "Create Stripe portal session", DefaultStatus: http.StatusOK}, checkoutPolicy("billing_portal", "create", "billing.portal.create"), h.createPortal)

	// HAProxy exposes only this path publicly; Huma keeps it out of the generated customer SDK.
	api.Adapter().Handle(&huma.Operation{OperationID: "stripe-webhook", Method: http.MethodPost, Path: "/webhooks/stripe", Hidden: true}, h.stripeWebhook)
}

func RegisterInternalRoutes(api huma.API, cfg Config) {
	h := &Handler{client: cfg.Client, logger: cfg.Logger, internalPeers: cfg.InternalPeers, stripeWebhookSecret: cfg.StripeWebhookSecret, billingReturnOrigins: cfg.BillingReturnOrigins, installationID: cfg.InstallationID}
	service := huma.NewGroup(api, "/internal/billing/v1")
	service.UseMiddleware(requireInternalPeerMiddleware(api, h.internalPeers))
	huma.Post(service, "/reserve", h.reserveWindow, internalOp("reserve-window", "Reserve billing window", http.StatusPaymentRequired, http.StatusForbidden))
	huma.Post(service, "/activate", h.activateWindow, internalOp("activate-window", "Activate billing window", http.StatusNotFound))
	huma.Post(service, "/settle", h.settleWindow, internalOp("settle-window", "Settle billing window", http.StatusNotFound))
	huma.Post(service, "/void", h.voidWindow, internalOp("void-window", "Void billing window", http.StatusNotFound))
}

func internalOp(id, summary string, errors ...int) func(*huma.Operation) {
	return func(operation *huma.Operation) {
		operation.OperationID = id
		operation.Summary = summary
		operation.Errors = errors
		operation.Security = []map[string][]string{{"mutualTLS": {}}}
	}
}

func (h *Handler) getEntitlements(ctx context.Context, orgID billing.OrgID, _ *emptyInput) (*body[dto.BillingEntitlementsView], error) {
	view, err := h.client.ListEntitlementsView(ctx, orgID)
	if err != nil {
		return nil, h.internalError(ctx, "get entitlements", err)
	}
	return &body[dto.BillingEntitlementsView]{Body: entitlementsResponse(view)}, nil
}

func (h *Handler) listGrants(ctx context.Context, orgID billing.OrgID, input *GrantsInput) (*body[dto.BillingGrants], error) {
	grants, err := h.client.ListGrantBalances(ctx, orgID, input.ProductID)
	if err != nil {
		return nil, h.internalError(ctx, "list grants", err)
	}
	out := make([]dto.BillingGrant, 0, len(grants))
	for _, grant := range grants {
		out = append(out, dto.BillingGrant{GrantID: grant.GrantID, ScopeType: grant.ScopeType, ScopeProductID: grant.ScopeProductID, ScopeBucketID: grant.ScopeBucketID, ScopeSKUID: grant.ScopeSKUID, Source: grant.Source, SourceReferenceID: grant.SourceReferenceID, EntitlementPeriodID: grant.EntitlementPeriodID, PolicyVersion: grant.PolicyVersion, StartsAt: grant.StartsAt, PeriodStart: grant.PeriodStart, PeriodEnd: grant.PeriodEnd, Available: dto.Uint64(grant.Available), Pending: dto.Uint64(grant.Pending), ExpiresAt: grant.ExpiresAt})
	}
	return &body[dto.BillingGrants]{Body: dto.BillingGrants{Grants: out}}, nil
}

func (h *Handler) listDocuments(ctx context.Context, orgID billing.OrgID, input *DocumentsInput) (*body[dto.BillingDocuments], error) {
	documents, err := h.client.ListDocuments(ctx, orgID, input.ProductID)
	if err != nil {
		return nil, h.internalError(ctx, "list documents", err)
	}
	out := make([]dto.BillingDocument, 0, len(documents))
	for _, document := range documents {
		out = append(out, documentResponse(h.installationID, orgID, document))
	}
	return &body[dto.BillingDocuments]{Body: dto.BillingDocuments{Documents: out}}, nil
}

func (h *Handler) getStatement(ctx context.Context, orgID billing.OrgID, input *StatementInput) (*body[dto.BillingStatement], error) {
	statement, err := h.client.PreviewStatement(ctx, orgID, input.ProductID)
	if err != nil {
		return nil, h.internalError(ctx, "get statement", err)
	}
	return &body[dto.BillingStatement]{Body: statementResponse(statement)}, nil
}

func (h *Handler) listContracts(ctx context.Context, orgID billing.OrgID, _ *emptyInput) (*body[dto.BillingContracts], error) {
	contracts, err := h.client.ListContracts(ctx, orgID)
	if err != nil {
		return nil, h.internalError(ctx, "list contracts", err)
	}
	out := make([]dto.BillingContract, 0, len(contracts))
	for _, contract := range contracts {
		out = append(out, contractResponse(contract))
	}
	return &body[dto.BillingContracts]{Body: dto.BillingContracts{Contracts: out}}, nil
}

func (h *Handler) listPlans(ctx context.Context, _ billing.OrgID, input *ProductQuery) (*body[dto.BillingPlans], error) {
	plans, err := h.client.ListPlans(ctx, input.ProductID)
	if err != nil {
		return nil, h.internalError(ctx, "list plans", err)
	}
	out := make([]dto.BillingPlan, 0, len(plans))
	for _, plan := range plans {
		out = append(out, dto.BillingPlan{PlanID: plan.PlanID, ProductID: plan.ProductID, DisplayName: plan.DisplayName, BillingMode: plan.BillingMode, Tier: plan.Tier, Currency: plan.Currency, MonthlyAmountCents: dto.Uint64(plan.MonthlyAmountCents), AnnualAmountCents: dto.Uint64(plan.AnnualAmountCents), Active: plan.Active, IsDefault: plan.IsDefault})
	}
	return &body[dto.BillingPlans]{Body: dto.BillingPlans{Plans: out}}, nil
}

func (h *Handler) createCheckout(ctx context.Context, orgID billing.OrgID, input *body[dto.BillingCreateCheckoutRequest]) (*body[dto.BillingURLResponse], error) {
	if err := validateBillingReturnURLs(ctx, h.billingReturnOrigins,
		billingReturnURLField{Name: "success_url", URL: input.Body.SuccessURL},
		billingReturnURLField{Name: "cancel_url", URL: input.Body.CancelURL},
	); err != nil {
		return nil, err
	}
	url, err := h.client.CreateCheckoutSession(ctx, orgID, input.Body.ProductID, billing.CheckoutParams{AmountCents: input.Body.AmountCents, SuccessURL: input.Body.SuccessURL, CancelURL: input.Body.CancelURL})
	if err != nil {
		return nil, h.internalError(ctx, "create checkout", err)
	}
	return &body[dto.BillingURLResponse]{Body: dto.BillingURLResponse{URL: url}}, nil
}

func (h *Handler) createContract(ctx context.Context, orgID billing.OrgID, input *body[dto.BillingCreateContractRequest]) (*body[dto.BillingURLResponse], error) {
	if err := validateBillingReturnURLs(ctx, h.billingReturnOrigins,
		billingReturnURLField{Name: "success_url", URL: input.Body.SuccessURL},
		billingReturnURLField{Name: "cancel_url", URL: input.Body.CancelURL},
	); err != nil {
		return nil, err
	}
	url, err := h.client.CreateContract(ctx, orgID, input.Body.PlanID, billing.BillingCadence(input.Body.Cadence), input.Body.SuccessURL, input.Body.CancelURL)
	if err != nil {
		if errors.Is(err, billing.ErrUnsupportedCadence) || errors.Is(err, billing.ErrUnsupportedChange) {
			return nil, huma.Error400BadRequest("unsupported contract request", err)
		}
		return nil, h.internalError(ctx, "create contract", err)
	}
	return &body[dto.BillingURLResponse]{Body: dto.BillingURLResponse{URL: url}}, nil
}

func (h *Handler) createContractChange(ctx context.Context, orgID billing.OrgID, input *CreateContractChangeInput) (*body[dto.BillingContractChangeResponse], error) {
	if err := validateBillingReturnURLs(ctx, h.billingReturnOrigins,
		billingReturnURLField{Name: "success_url", URL: input.Body.SuccessURL},
		billingReturnURLField{Name: "cancel_url", URL: input.Body.CancelURL},
	); err != nil {
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
	return &body[dto.BillingContractChangeResponse]{Body: dto.BillingContractChangeResponse{URL: result.URL, ChangeID: result.ChangeID, FinalizationID: result.FinalizationID, DocumentID: result.DocumentID, Status: result.Status, PriceDelta: dto.Uint64(result.PriceDeltaUnits)}}, nil
}

func (h *Handler) cancelContract(ctx context.Context, orgID billing.OrgID, input *CancelContractInput) (*body[dto.BillingCancelContractResponse], error) {
	contract, err := h.client.CancelContract(ctx, orgID, input.ContractID)
	if err != nil {
		if errors.Is(err, billing.ErrContractNotFound) {
			return nil, huma.Error404NotFound("contract not found")
		}
		return nil, h.internalError(ctx, "cancel contract", err)
	}
	return &body[dto.BillingCancelContractResponse]{Body: dto.BillingCancelContractResponse{Contract: contractResponse(contract)}}, nil
}

func (h *Handler) createPortal(ctx context.Context, orgID billing.OrgID, input *body[dto.BillingCreatePortalSessionRequest]) (*body[dto.BillingURLResponse], error) {
	if err := validateBillingReturnURLs(ctx, h.billingReturnOrigins, billingReturnURLField{Name: "return_url", URL: input.Body.ReturnURL}); err != nil {
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
	return &body[dto.BillingURLResponse]{Body: dto.BillingURLResponse{URL: url}}, nil
}

func (h *Handler) reserveWindow(ctx context.Context, input *body[dto.BillingReserveWindowRequest]) (*body[dto.BillingReserveWindowResult], error) {
	orgID, err := billingOrgIDFromWire(input.Body.OrgID)
	if err != nil {
		return nil, err
	}
	reservation, err := h.client.ReserveWindow(ctx, billing.ReserveRequest{OrgID: orgID, ProductID: input.Body.ProductID, ActorID: input.Body.ActorID, ConcurrentCount: input.Body.ConcurrentCount, SourceType: input.Body.SourceType, SourceRef: input.Body.SourceRef, WindowSeq: input.Body.WindowSeq, ReservationShape: input.Body.ReservationShape, ReservedQuantity: input.Body.ReservedQuantity, BillingJobID: input.Body.BillingJobID, Allocation: input.Body.Allocation})
	if err != nil {
		return nil, h.windowError(ctx, "reserve", err)
	}
	return &body[dto.BillingReserveWindowResult]{Body: dto.BillingReserveWindowResult{Reservation: reservationResponse(reservation)}}, nil
}

func (h *Handler) activateWindow(ctx context.Context, input *body[dto.BillingActivateWindowRequest]) (*body[dto.BillingActivateWindowResult], error) {
	reservation, err := h.client.ActivateWindow(ctx, input.Body.WindowID, input.Body.ActivatedAt)
	if err != nil {
		return nil, h.windowError(ctx, "activate", err)
	}
	return &body[dto.BillingActivateWindowResult]{Body: dto.BillingActivateWindowResult{Reservation: reservationResponse(reservation)}}, nil
}

func (h *Handler) settleWindow(ctx context.Context, input *body[dto.BillingSettleWindowRequest]) (*body[dto.BillingSettleResult], error) {
	result, err := h.client.SettleWindow(ctx, input.Body.WindowID, input.Body.ActualQuantity, input.Body.UsageSummary)
	if err != nil {
		return nil, h.windowError(ctx, "settle", err)
	}
	return &body[dto.BillingSettleResult]{Body: dto.BillingSettleResult{WindowID: result.WindowID, ActualQuantity: result.ActualQuantity, BillableQuantity: result.BillableQuantity, WriteoffQuantity: result.WriteoffQuantity, BilledChargeUnits: dto.Uint64(result.BilledChargeUnits), WriteoffChargeUnits: dto.Uint64(result.WriteoffChargeUnits), SettledAt: result.SettledAt}}, nil
}

func (h *Handler) voidWindow(ctx context.Context, input *body[dto.BillingVoidWindowRequest]) (*body[dto.BillingVoidWindowResult], error) {
	if err := h.client.VoidWindow(ctx, input.Body.WindowID); err != nil {
		return nil, h.windowError(ctx, "void", err)
	}
	return &body[dto.BillingVoidWindowResult]{Body: dto.BillingVoidWindowResult{WindowID: input.Body.WindowID}}, nil
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

func statementResponse(statement billing.Statement) dto.BillingStatement {
	items := make([]dto.BillingStatementLineItem, 0, len(statement.LineItems))
	for _, line := range statement.LineItems {
		items = append(items, dto.BillingStatementLineItem{ProductID: line.ProductID, PlanID: line.PlanID, BucketID: line.BucketID, BucketDisplayName: line.BucketDisplayName, SKUID: line.SKUID, SKUDisplayName: line.SKUDisplayName, QuantityUnit: line.QuantityUnit, PricingPhase: line.PricingPhase, Quantity: line.Quantity, UnitRate: dto.Uint64(line.UnitRate), ChargeUnits: dto.Uint64(line.ChargeUnits), FreeTierUnits: dto.Uint64(line.FreeTierUnits), ContractUnits: dto.Uint64(line.ContractUnits), PurchaseUnits: dto.Uint64(line.PurchaseUnits), PromoUnits: dto.Uint64(line.PromoUnits), RefundUnits: dto.Uint64(line.RefundUnits), ReceivableUnits: dto.Uint64(line.ReceivableUnits), ReservedUnits: dto.Uint64(line.ReservedUnits)})
	}
	summaries := make([]dto.BillingStatementGrantSummary, 0, len(statement.GrantSummaries))
	for _, summary := range statement.GrantSummaries {
		summaries = append(summaries, dto.BillingStatementGrantSummary{ScopeType: summary.ScopeType, ScopeProductID: summary.ScopeProductID, ScopeBucketID: summary.ScopeBucketID, Source: summary.Source, Available: dto.Uint64(summary.Available), Pending: dto.Uint64(summary.Pending)})
	}
	return dto.BillingStatement{OrgID: dto.Uint64(uint64(statement.OrgID)), ProductID: statement.ProductID, PeriodStart: statement.PeriodStart, PeriodEnd: statement.PeriodEnd, PeriodSource: statement.PeriodSource, GeneratedAt: statement.GeneratedAt, Currency: statement.Currency, UnitLabel: statement.UnitLabel, LineItems: items, GrantSummaries: summaries, Totals: dto.BillingStatementTotals{ChargeUnits: dto.Uint64(statement.Totals.ChargeUnits), FreeTierUnits: dto.Uint64(statement.Totals.FreeTierUnits), ContractUnits: dto.Uint64(statement.Totals.ContractUnits), PurchaseUnits: dto.Uint64(statement.Totals.PurchaseUnits), PromoUnits: dto.Uint64(statement.Totals.PromoUnits), RefundUnits: dto.Uint64(statement.Totals.RefundUnits), ReceivableUnits: dto.Uint64(statement.Totals.ReceivableUnits), ReservedUnits: dto.Uint64(statement.Totals.ReservedUnits), TotalDueUnits: dto.Uint64(statement.Totals.TotalDueUnits)}}
}

func documentResponse(installationID string, orgID billing.OrgID, document billing.DocumentRecord) dto.BillingDocument {
	return dto.BillingDocument{
		DocumentID:             document.DocumentID,
		ResourceName:           dto.ResourceNameBillingDocument(installationID, strconv.FormatUint(uint64(orgID), 10), document.DocumentID),
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
		SubtotalUnits:          dto.Uint64(document.SubtotalUnits),
		AdjustmentUnits:        dto.Int64(document.AdjustmentUnits),
		TaxUnits:               dto.Uint64(document.TaxUnits),
		TotalDueUnits:          dto.Uint64(document.TotalDueUnits),
		StripeHostedInvoiceURL: document.StripeHostedInvoiceURL,
		StripeInvoicePDFURL:    document.StripeInvoicePDFURL,
		StripePaymentIntentID:  document.StripePaymentIntentID,
	}
}

func entitlementsResponse(view billing.EntitlementsView) dto.BillingEntitlementsView {
	products := make([]dto.BillingEntitlementProductSection, 0, len(view.Products))
	for _, product := range view.Products {
		buckets := make([]dto.BillingEntitlementBucketSection, 0, len(product.Buckets))
		for _, bucket := range product.Buckets {
			skus := make([]dto.BillingEntitlementSlot, 0, len(bucket.SKUSlots))
			for _, slot := range bucket.SKUSlots {
				skus = append(skus, entitlementSlot(slot))
			}
			buckets = append(buckets, dto.BillingEntitlementBucketSection{BucketID: bucket.BucketID, DisplayName: bucket.DisplayName, BucketSlot: entitlementSlotPtr(bucket.BucketSlot), SKUSlots: skus})
		}
		products = append(products, dto.BillingEntitlementProductSection{ProductID: product.ProductID, DisplayName: product.DisplayName, ProductSlot: entitlementSlotPtr(product.ProductSlot), Buckets: buckets})
	}
	return dto.BillingEntitlementsView{OrgID: dto.Uint64(uint64(view.OrgID)), Universal: entitlementSlot(view.Universal), Products: products}
}

func entitlementSlotPtr(slot *billing.EntitlementSlot) *dto.BillingEntitlementSlot {
	if slot == nil {
		return nil
	}
	out := entitlementSlot(*slot)
	return &out
}

func entitlementSlot(slot billing.EntitlementSlot) dto.BillingEntitlementSlot {
	sources := make([]dto.BillingEntitlementSourceTotal, 0, len(slot.Sources))
	for _, source := range slot.Sources {
		sources = append(sources, dto.BillingEntitlementSourceTotal{Source: source.Source, PlanID: source.PlanID, Label: source.Label, PeriodStartUnits: dto.Uint64(source.PeriodStartUnits), AvailableUnits: dto.Uint64(source.AvailableUnits), PendingUnits: dto.Uint64(source.PendingUnits), InlineExpiresAt: source.InlineExpiresAt})
	}
	return dto.BillingEntitlementSlot{ScopeType: slot.ScopeType, ProductID: slot.ProductID, ProductDisplay: slot.ProductDisplay, BucketID: slot.BucketID, BucketDisplay: slot.BucketDisplay, SKUID: slot.SKUID, SKUDisplay: slot.SKUDisplay, CoverageLabel: slot.CoverageLabel, PeriodStartUnits: dto.Uint64(slot.PeriodStartUnits), SpentUnits: dto.Uint64(slot.SpentUnits), PendingUnits: dto.Uint64(slot.PendingUnits), AvailableUnits: dto.Uint64(slot.AvailableUnits), Sources: sources}
}

func contractResponse(contract billing.ContractRecord) dto.BillingContract {
	return dto.BillingContract{ContractID: contract.ContractID, ProductID: contract.ProductID, PlanID: contract.PlanID, PhaseID: contract.PhaseID, CadenceKind: contract.CadenceKind, Status: contract.Status, PaymentState: contract.PaymentState, EntitlementState: contract.EntitlementState, PendingChangeID: contract.PendingChangeID, PendingChangeType: contract.PendingChangeType, PendingChangeTargetPlanID: contract.PendingChangeTargetPlanID, PendingChangeEffectiveAt: contract.PendingChangeEffectiveAt, StartsAt: contract.StartsAt, EndsAt: contract.EndsAt, PhaseStart: contract.PhaseStart, PhaseEnd: contract.PhaseEnd}
}

func reservationResponse(reservation billing.WindowReservation) dto.BillingWindowReservation {
	rates := map[string]dto.DecimalUint64{}
	for sku, rate := range reservation.SKURates {
		rates[sku] = dto.Uint64(rate)
	}
	return dto.BillingWindowReservation{WindowID: reservation.WindowID, OrgID: dto.Uint64(uint64(reservation.OrgID)), ProductID: reservation.ProductID, PlanID: reservation.PlanID, ActorID: reservation.ActorID, SourceType: reservation.SourceType, SourceRef: reservation.SourceRef, WindowSeq: reservation.WindowSeq, ReservationShape: reservation.ReservationShape, ReservedQuantity: reservation.ReservedQuantity, ReservedChargeUnits: dto.Uint64(reservation.ReservedChargeUnits), PricingPhase: reservation.PricingPhase, Allocation: reservation.Allocation, SKURates: rates, CostPerUnit: dto.Uint64(reservation.CostPerUnit), WindowStart: reservation.WindowStart, ActivatedAt: reservation.ActivatedAt, ExpiresAt: reservation.ExpiresAt, RenewBy: reservation.RenewBy}
}

func billingOrgIDFromWire(id dto.DecimalUint64) (billing.OrgID, error) {
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
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "missing spiffe peer")
			return
		}
		if _, ok := allowed[peerID]; ok {
			next(ctx)
			return
		}
		_ = huma.WriteErr(api, ctx, http.StatusForbidden, "unexpected spiffe peer")
	}
}
