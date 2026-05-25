package billingapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/verself/billing-service/internal/billing"
	"github.com/verself/billing-service/internal/contractapi"
	"github.com/verself/billing-service/internal/internalcontractapi"
	"github.com/verself/service-runtime/humaapi"
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

func NewAPI(mux *http.ServeMux, cfg Config) huma.API {
	version := cfg.Version
	if version == "" {
		version = "2.0.0"
	}
	config := humaapi.DefaultConfig("Billing Service", version)
	if cfg.ListenAddr != "" {
		config.Servers = []*huma.Server{{URL: "http://" + cfg.ListenAddr}}
	}
	api := humago.New(mux, config)
	applyPublicSecurityScheme(api)
	RegisterPublicRoutes(api, cfg)
	humaapi.ApplyOpenAPIWireDefaults(api)
	return api
}

func NewInternalAPI(mux *http.ServeMux, cfg Config) huma.API {
	version := cfg.Version
	if version == "" {
		version = "2.0.0"
	}
	config := humaapi.DefaultConfig("Billing Internal API", version)
	if cfg.ListenAddr != "" {
		config.Servers = []*huma.Server{{URL: serverURL(cfg.ListenAddr)}}
	}
	api := humago.New(mux, config)
	applyInternalSecurityScheme(api)
	RegisterInternalRoutes(api, cfg)
	humaapi.ApplyOpenAPIWireDefaults(api)
	return api
}

func serverURL(addr string) string {
	if strings.Contains(addr, "://") {
		return addr
	}
	return "http://" + addr
}

func RegisterPublicRoutes(api huma.API, cfg Config) {
	h := &Handler{client: cfg.Client, logger: cfg.Logger, internalPeers: cfg.InternalPeers, stripeWebhookSecret: cfg.StripeWebhookSecret, billingReturnOrigins: cfg.BillingReturnOrigins, installationID: cfg.InstallationID}
	registerPublicBillingContractRoute(api, cfg.Authorizer, contractapi.GetBillingEntitlements.Descriptor, "Get org entitlements view", h.getEntitlements)
	registerPublicBillingContractRoute(api, cfg.Authorizer, contractapi.ListBillingGrants.Descriptor, "List org credit grants", h.listGrants)
	registerPublicBillingContractRoute(api, cfg.Authorizer, contractapi.ListBillingDocuments.Descriptor, "List issued billing documents", h.listDocuments)
	registerPublicBillingContractRoute(api, cfg.Authorizer, contractapi.GetBillingStatement.Descriptor, "Preview current statement", h.getStatement)
	registerPublicBillingContractRoute(api, cfg.Authorizer, contractapi.ListBillingContracts.Descriptor, "List org contracts", h.listContracts)
	registerPublicBillingContractRoute(api, cfg.Authorizer, contractapi.ListBillingPlans.Descriptor, "List active plans", h.listPlans)
	registerPublicBillingContractRoute(api, cfg.Authorizer, contractapi.CreateBillingCheckout.Descriptor, "Create credit checkout", h.createCheckout)
	registerPublicBillingContractRoute(api, cfg.Authorizer, contractapi.CreateBillingContract.Descriptor, "Create contract checkout", h.createContract)
	registerPublicBillingContractRoute(api, cfg.Authorizer, contractapi.CreateBillingContractChange.Descriptor, "Create contract change", h.createContractChange)
	registerPublicBillingContractRoute(api, cfg.Authorizer, contractapi.CancelBillingContract.Descriptor, "Cancel contract", h.cancelContract)
	registerPublicBillingContractRoute(api, cfg.Authorizer, contractapi.CreateBillingPortal.Descriptor, "Create Stripe portal session", h.createPortal)

	// HAProxy exposes only this path publicly; Huma keeps it out of the generated customer SDK.
	api.Adapter().Handle(&huma.Operation{OperationID: "stripe-webhook", Method: http.MethodPost, Path: "/webhooks/stripe", Hidden: true}, h.stripeWebhook)
}

func RegisterInternalRoutes(api huma.API, cfg Config) {
	h := &Handler{client: cfg.Client, logger: cfg.Logger, internalPeers: cfg.InternalPeers, stripeWebhookSecret: cfg.StripeWebhookSecret, billingReturnOrigins: cfg.BillingReturnOrigins, installationID: cfg.InstallationID}
	registerInternalBillingContractRoute(api, h.internalPeers, internalcontractapi.EnsureBillingOrganization.Descriptor, "Ensure billing organization", h.ensureBillingOrganization)
	registerInternalBillingContractRoute(api, h.internalPeers, internalcontractapi.SetOrganizationTrustTier.Descriptor, "Set organization trust tier", h.setOrganizationTrustTier)
	registerInternalBillingContractRoute(api, h.internalPeers, internalcontractapi.ApplyBillingPlanPromotion.Descriptor, "Apply billing plan promotion", h.applyBillingPlanPromotion)
	registerInternalBillingContractRoute(api, h.internalPeers, internalcontractapi.CancelBillingPlanPromotion.Descriptor, "Cancel billing plan promotion", h.cancelBillingPlanPromotion)
	registerInternalBillingContractRoute(api, h.internalPeers, internalcontractapi.GetStorageEntitlement.Descriptor, "Get storage entitlement", h.getStorageEntitlement)
	registerInternalBillingContractRoute(api, h.internalPeers, internalcontractapi.ReserveWindow.Descriptor, "Reserve billing window", h.reserveWindow)
	registerInternalBillingContractRoute(api, h.internalPeers, internalcontractapi.ActivateWindow.Descriptor, "Activate billing window", h.activateWindow)
	registerInternalBillingContractRoute(api, h.internalPeers, internalcontractapi.SettleWindow.Descriptor, "Settle billing window", h.settleWindow)
	registerInternalBillingContractRoute(api, h.internalPeers, internalcontractapi.VoidWindow.Descriptor, "Void billing window", h.voidWindow)
}

func (h *Handler) getEntitlements(ctx context.Context, orgID billing.OrgID, _ *contractapi.EmptyInput) (*contractapi.GetBillingEntitlementsOutput, error) {
	view, err := h.client.ListEntitlementsView(ctx, orgID)
	if err != nil {
		return nil, h.internalError(ctx, "get entitlements", err)
	}
	return &contractapi.GetBillingEntitlementsOutput{Body: entitlementsResponse(view)}, nil
}

func (h *Handler) listGrants(ctx context.Context, orgID billing.OrgID, input *contractapi.GrantsQueryInput) (*contractapi.ListBillingGrantsOutput, error) {
	grants, err := h.client.ListGrantBalances(ctx, orgID, string(input.ProductID))
	if err != nil {
		return nil, h.internalError(ctx, "list grants", err)
	}
	out := make(contractapi.BillingGrants, 0, len(grants))
	for _, grant := range grants {
		out = append(out, grantResponse(grant))
	}
	return &contractapi.ListBillingGrantsOutput{Body: contractapi.ListBillingGrantsOutputBody{Grants: out}}, nil
}

func (h *Handler) listDocuments(ctx context.Context, orgID billing.OrgID, input *contractapi.DocumentsQueryInput) (*contractapi.ListBillingDocumentsOutput, error) {
	documents, err := h.client.ListDocuments(ctx, orgID, string(input.ProductID))
	if err != nil {
		return nil, h.internalError(ctx, "list documents", err)
	}
	out := make(contractapi.BillingDocuments, 0, len(documents))
	for _, document := range documents {
		out = append(out, documentResponse(h.installationID, orgID, document))
	}
	return &contractapi.ListBillingDocumentsOutput{Body: contractapi.ListBillingDocumentsOutputBody{Documents: out}}, nil
}

func (h *Handler) getStatement(ctx context.Context, orgID billing.OrgID, input *contractapi.ProductQueryInput) (*contractapi.GetBillingStatementOutput, error) {
	statement, err := h.client.PreviewStatement(ctx, orgID, string(input.ProductID))
	if err != nil {
		return nil, h.internalError(ctx, "get statement", err)
	}
	return &contractapi.GetBillingStatementOutput{Body: statementResponse(statement)}, nil
}

func (h *Handler) listContracts(ctx context.Context, orgID billing.OrgID, _ *contractapi.EmptyInput) (*contractapi.ListBillingContractsOutput, error) {
	contracts, err := h.client.ListContracts(ctx, orgID)
	if err != nil {
		return nil, h.internalError(ctx, "list contracts", err)
	}
	out := make(contractapi.BillingContracts, 0, len(contracts))
	for _, contract := range contracts {
		out = append(out, contractResponse(contract))
	}
	return &contractapi.ListBillingContractsOutput{Body: contractapi.ListBillingContractsOutputBody{Contracts: out}}, nil
}

func (h *Handler) listPlans(ctx context.Context, _ billing.OrgID, input *contractapi.ProductQueryInput) (*contractapi.ListBillingPlansOutput, error) {
	plans, err := h.client.ListPlans(ctx, string(input.ProductID))
	if err != nil {
		return nil, h.internalError(ctx, "list plans", err)
	}
	out := make(contractapi.BillingPlans, 0, len(plans))
	for _, plan := range plans {
		out = append(out, publicPlanResponse(plan))
	}
	return &contractapi.ListBillingPlansOutput{Body: contractapi.ListBillingPlansOutputBody{Plans: out}}, nil
}

func (h *Handler) createCheckout(ctx context.Context, orgID billing.OrgID, input *contractapi.CheckoutInput) (*contractapi.BillingURLResponseOutput, error) {
	if err := validateBillingReturnURLs(ctx, h.billingReturnOrigins,
		billingReturnURLField{Name: "success_url", URL: string(input.Body.SuccessURL)},
		billingReturnURLField{Name: "cancel_url", URL: string(input.Body.CancelURL)},
	); err != nil {
		return nil, err
	}
	url, err := h.client.CreateCheckoutSession(ctx, orgID, string(input.Body.ProductID), billing.CheckoutParams{AmountCents: int64(input.Body.AmountCents), SuccessURL: string(input.Body.SuccessURL), CancelURL: string(input.Body.CancelURL)})
	if err != nil {
		return nil, h.internalError(ctx, "create checkout", err)
	}
	return &contractapi.BillingURLResponseOutput{Body: contractapi.BillingURLResponse{URL: contractapi.URL(url)}}, nil
}

func (h *Handler) createContract(ctx context.Context, orgID billing.OrgID, input *contractapi.CreateBillingContractInput) (*contractapi.BillingURLResponseOutput, error) {
	if err := validateBillingReturnURLs(ctx, h.billingReturnOrigins,
		billingReturnURLField{Name: "success_url", URL: string(input.Body.SuccessURL)},
		billingReturnURLField{Name: "cancel_url", URL: string(input.Body.CancelURL)},
	); err != nil {
		return nil, err
	}
	cadence := billing.BillingCadence("")
	if input.Body.Cadence != nil {
		cadence = billing.BillingCadence(*input.Body.Cadence)
	}
	url, err := h.client.CreateContract(ctx, orgID, string(input.Body.PlanID), cadence, string(input.Body.SuccessURL), string(input.Body.CancelURL))
	if err != nil {
		if errors.Is(err, billing.ErrUnsupportedCadence) || errors.Is(err, billing.ErrUnsupportedChange) {
			return nil, huma.Error400BadRequest("unsupported contract request", err)
		}
		return nil, h.internalError(ctx, "create contract", err)
	}
	return &contractapi.BillingURLResponseOutput{Body: contractapi.BillingURLResponse{URL: contractapi.URL(url)}}, nil
}

func (h *Handler) createContractChange(ctx context.Context, orgID billing.OrgID, input *contractapi.CreateBillingContractChangeInput) (*contractapi.CreateBillingContractChangeOutput, error) {
	if err := validateBillingReturnURLs(ctx, h.billingReturnOrigins,
		billingReturnURLField{Name: "success_url", URL: string(input.Body.SuccessURL)},
		billingReturnURLField{Name: "cancel_url", URL: string(input.Body.CancelURL)},
	); err != nil {
		return nil, err
	}
	result, err := h.client.CreateContractChange(ctx, orgID, string(input.ContractID), billing.ContractChangeRequest{TargetPlanID: string(input.Body.TargetPlanID), SuccessURL: string(input.Body.SuccessURL), CancelURL: string(input.Body.CancelURL)})
	if err != nil {
		if errors.Is(err, billing.ErrContractNotFound) {
			return nil, huma.Error404NotFound("contract not found")
		}
		if errors.Is(err, billing.ErrUnsupportedChange) {
			return nil, huma.Error400BadRequest("unsupported contract change", err)
		}
		return nil, h.internalError(ctx, "create contract change", err)
	}
	return &contractapi.CreateBillingContractChangeOutput{Body: contractapi.BillingContractChangeResponse{
		URL:             contractapi.URL(result.URL),
		ChangeID:        contractapi.BillingChangeID(result.ChangeID),
		FinalizationID:  optionalTypedString[contractapi.BillingFinalizationID](result.FinalizationID),
		DocumentID:      optionalTypedString[contractapi.DocumentID](result.DocumentID),
		Status:          contractapi.BillingState(result.Status),
		PriceDeltaUnits: decimalUint64(result.PriceDeltaUnits),
	}}, nil
}

func (h *Handler) cancelContract(ctx context.Context, orgID billing.OrgID, input *contractapi.ContractMutationInput) (*contractapi.BillingCancelContractResponseOutput, error) {
	contract, err := h.client.CancelContract(ctx, orgID, string(input.ContractID))
	if err != nil {
		if errors.Is(err, billing.ErrContractNotFound) {
			return nil, huma.Error404NotFound("contract not found")
		}
		return nil, h.internalError(ctx, "cancel contract", err)
	}
	return &contractapi.BillingCancelContractResponseOutput{Body: contractapi.BillingCancelContractResponseOutputBody{Contract: contractResponse(contract)}}, nil
}

func (h *Handler) createPortal(ctx context.Context, orgID billing.OrgID, input *contractapi.CreateBillingPortalInput) (*contractapi.BillingURLResponseOutput, error) {
	if err := validateBillingReturnURLs(ctx, h.billingReturnOrigins, billingReturnURLField{Name: "return_url", URL: string(input.Body.ReturnURL)}); err != nil {
		return nil, err
	}
	url, err := h.client.CreatePortalSession(ctx, orgID, string(input.Body.ReturnURL))
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
	return &contractapi.BillingURLResponseOutput{Body: contractapi.BillingURLResponse{URL: contractapi.URL(url)}}, nil
}

func (h *Handler) ensureBillingOrganization(ctx context.Context, input *internalcontractapi.EnsureBillingOrganizationInput) (*internalcontractapi.BillingOrganizationOutput, error) {
	trustTier := ""
	if input.Body.TrustTier != nil {
		trustTier = string(*input.Body.TrustTier)
	}
	org, err := h.client.EnsureOrgRecord(ctx, billing.OrgID(input.Body.OrgID), input.Body.DisplayName, trustTier)
	if err != nil {
		return nil, h.billingOrgMutationError(ctx, "ensure billing organization", err)
	}
	return &internalcontractapi.BillingOrganizationOutput{Body: internalcontractapi.BillingOrganizationOutputBody{Organization: billingOrganizationResponse(org)}}, nil
}

func (h *Handler) setOrganizationTrustTier(ctx context.Context, input *internalcontractapi.SetOrganizationTrustTierInput) (*internalcontractapi.BillingOrganizationOutput, error) {
	org, err := h.client.SetOrganizationTrustTier(ctx, billing.OrgID(input.OrgID), string(input.Body.TrustTier))
	if err != nil {
		return nil, h.billingOrgMutationError(ctx, "set organization trust tier", err)
	}
	return &internalcontractapi.BillingOrganizationOutput{Body: internalcontractapi.BillingOrganizationOutputBody{Organization: billingOrganizationResponse(org)}}, nil
}

func (h *Handler) applyBillingPlanPromotion(ctx context.Context, input *internalcontractapi.ApplyBillingPlanPromotionInput) (*internalcontractapi.ApplyBillingPlanPromotionOutput, error) {
	record, err := h.client.ApplyPlanPromotion(ctx, billing.OrgID(input.OrgID), string(input.Body.ProductID), string(input.Body.PlanID), int(input.Body.PercentOff), string(input.Body.Reason))
	if err != nil {
		return nil, h.billingOrgMutationError(ctx, "apply billing plan promotion", err)
	}
	return &internalcontractapi.ApplyBillingPlanPromotionOutput{Body: internalcontractapi.ApplyBillingPlanPromotionOutputBody{Promotion: billingPlanPromotionResponse(record)}}, nil
}

func (h *Handler) cancelBillingPlanPromotion(ctx context.Context, input *internalcontractapi.CancelBillingPlanPromotionInput) (*internalcontractapi.CancelBillingPlanPromotionOutput, error) {
	record, err := h.client.CancelPlanPromotion(ctx, billing.OrgID(input.OrgID), string(input.Body.ProductID), string(input.Body.Reason))
	if err != nil {
		return nil, h.billingOrgMutationError(ctx, "cancel billing plan promotion", err)
	}
	return &internalcontractapi.CancelBillingPlanPromotionOutput{Body: internalcontractapi.CancelBillingPlanPromotionOutputBody{Cancellation: billingPlanPromotionCancellationResponse(record)}}, nil
}

func (h *Handler) getStorageEntitlement(ctx context.Context, input *internalcontractapi.GetStorageEntitlementInput) (*internalcontractapi.GetStorageEntitlementOutput, error) {
	orgID, err := billingOrgIDFromWire(input.Body.OrgID)
	if err != nil {
		return nil, err
	}
	entitlement, err := h.client.GetStorageEntitlement(ctx, orgID, string(input.Body.ProductID))
	if err != nil {
		return nil, h.internalError(ctx, "get storage entitlement", err)
	}
	return &internalcontractapi.GetStorageEntitlementOutput{
		Body: internalcontractapi.GetStorageEntitlementOutputBody{
			Entitlement: storageEntitlementResponse(entitlement),
		},
	}, nil
}

func (h *Handler) reserveWindow(ctx context.Context, input *internalcontractapi.ReserveWindowInput) (*internalcontractapi.ReserveWindowOutput, error) {
	orgID, err := billingOrgIDFromWire(input.Body.OrgID)
	if err != nil {
		return nil, err
	}
	concurrentCount, err := safeUint64(input.Body.ConcurrentCount, "concurrent_count")
	if err != nil {
		return nil, err
	}
	windowSeq, err := windowSequence(input.Body.WindowSeq, "window_seq")
	if err != nil {
		return nil, err
	}
	reservedQuantity, err := windowQuantity(input.Body.ReservedQuantity, "reserved_quantity")
	if err != nil {
		return nil, err
	}
	billingJobID := int64(0)
	if input.Body.BillingJobID != nil {
		billingJobID = int64(*input.Body.BillingJobID)
	}
	reservation, err := h.client.ReserveWindow(ctx, billing.ReserveRequest{
		OrgID:            orgID,
		ProductID:        string(input.Body.ProductID),
		ActorID:          string(input.Body.ActorID),
		ConcurrentCount:  concurrentCount,
		SourceType:       string(input.Body.SourceType),
		SourceRef:        string(input.Body.SourceRef),
		WindowSeq:        windowSeq,
		ReservationShape: string(input.Body.ReservationShape),
		ReservedQuantity: reservedQuantity,
		BillingJobID:     billingJobID,
		Allocation:       billingAllocation(input.Body.Allocation),
	})
	if err != nil {
		return nil, h.windowError(ctx, "reserve", err)
	}
	return &internalcontractapi.ReserveWindowOutput{Body: internalcontractapi.ReserveWindowOutputBody{Reservation: reservationResponse(reservation)}}, nil
}

func (h *Handler) activateWindow(ctx context.Context, input *internalcontractapi.ActivateWindowInput) (*internalcontractapi.ReserveWindowOutput, error) {
	activatedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(input.Body.ActivatedAt))
	if err != nil {
		return nil, badRequest("activated_at must be an RFC3339 timestamp")
	}
	reservation, err := h.client.ActivateWindow(ctx, string(input.Body.WindowID), activatedAt)
	if err != nil {
		return nil, h.windowError(ctx, "activate", err)
	}
	return &internalcontractapi.ReserveWindowOutput{Body: internalcontractapi.ReserveWindowOutputBody{Reservation: reservationResponse(reservation)}}, nil
}

func (h *Handler) settleWindow(ctx context.Context, input *internalcontractapi.SettleWindowInput) (*internalcontractapi.SettleWindowOutput, error) {
	actualQuantity, err := windowQuantity(input.Body.ActualQuantity, "actual_quantity")
	if err != nil {
		return nil, err
	}
	result, err := h.client.SettleWindow(ctx, string(input.Body.WindowID), actualQuantity, billingUsageSummary(input.Body.UsageSummary))
	if err != nil {
		return nil, h.windowError(ctx, "settle", err)
	}
	return &internalcontractapi.SettleWindowOutput{Body: settlementResponse(result)}, nil
}

func (h *Handler) voidWindow(ctx context.Context, input *internalcontractapi.VoidWindowInput) (*internalcontractapi.VoidWindowOutput, error) {
	if err := h.client.VoidWindow(ctx, string(input.Body.WindowID)); err != nil {
		return nil, h.windowError(ctx, "void", err)
	}
	return &internalcontractapi.VoidWindowOutput{Body: internalcontractapi.VoidWindowOutputBody{WindowID: input.Body.WindowID}}, nil
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

func (h *Handler) billingOrgMutationError(ctx context.Context, operation string, err error) error {
	switch {
	case errors.Is(err, billing.ErrOrgNotFound):
		return huma.Error404NotFound("billing organization not found", err)
	case errors.Is(err, billing.ErrContractNotFound):
		return huma.Error404NotFound("billing contract not found", err)
	case errors.Is(err, billing.ErrUnsupportedTrustTier), errors.Is(err, billing.ErrUnsupportedChange):
		return huma.Error400BadRequest(operation, err)
	default:
		return h.internalError(ctx, operation, err)
	}
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
