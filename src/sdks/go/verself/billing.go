package verself

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	billingcore "github.com/verself/verself-go/internal/generated/billing"
)

type BillingGrant struct {
	GrantID             string     `json:"grant_id"`
	ScopeType           string     `json:"scope_type"`
	ScopeProductID      string     `json:"scope_product_id"`
	ScopeBucketID       string     `json:"scope_bucket_id"`
	ScopeSkuID          string     `json:"scope_sku_id"`
	Source              string     `json:"source"`
	SourceReferenceID   string     `json:"source_reference_id"`
	EntitlementPeriodID string     `json:"entitlement_period_id"`
	PolicyVersion       string     `json:"policy_version"`
	StartsAt            time.Time  `json:"starts_at"`
	PeriodStart         *time.Time `json:"period_start,omitempty"`
	PeriodEnd           *time.Time `json:"period_end,omitempty"`
	Available           string     `json:"available"`
	Pending             string     `json:"pending"`
	ExpiresAt           *time.Time `json:"expires_at,omitempty"`
}

type BillingGrants struct {
	Grants []BillingGrant `json:"grants"`
}

type BillingDocument struct {
	DocumentID             string     `json:"document_id"`
	ResourceName           string     `json:"resourceName"`
	DocumentNumber         string     `json:"document_number"`
	DocumentKind           string     `json:"document_kind"`
	FinalizationID         string     `json:"finalization_id"`
	ProductID              string     `json:"product_id"`
	CycleID                string     `json:"cycle_id"`
	Status                 string     `json:"status"`
	PaymentStatus          string     `json:"payment_status"`
	PeriodStart            time.Time  `json:"period_start"`
	PeriodEnd              time.Time  `json:"period_end"`
	IssuedAt               *time.Time `json:"issued_at,omitempty"`
	Currency               string     `json:"currency"`
	SubtotalUnits          string     `json:"subtotal_units"`
	AdjustmentUnits        string     `json:"adjustment_units"`
	TaxUnits               string     `json:"tax_units"`
	TotalDueUnits          string     `json:"total_due_units"`
	StripeHostedInvoiceURL string     `json:"stripe_hosted_invoice_url,omitempty"`
	StripeInvoicePDFURL    string     `json:"stripe_invoice_pdf_url,omitempty"`
	StripePaymentIntentID  string     `json:"stripe_payment_intent_id,omitempty"`
}

type BillingDocuments struct {
	Documents []BillingDocument `json:"documents"`
}

type BillingContract struct {
	ContractID                string     `json:"contract_id"`
	ProductID                 string     `json:"product_id"`
	PlanID                    string     `json:"plan_id"`
	CadenceKind               string     `json:"cadence_kind"`
	Status                    string     `json:"status"`
	PaymentState              string     `json:"payment_state"`
	EntitlementState          string     `json:"entitlement_state"`
	PhaseID                   string     `json:"phase_id"`
	StartsAt                  time.Time  `json:"starts_at"`
	EndsAt                    *time.Time `json:"ends_at,omitempty"`
	PhaseStart                *time.Time `json:"phase_start,omitempty"`
	PhaseEnd                  *time.Time `json:"phase_end,omitempty"`
	PendingChangeID           string     `json:"pending_change_id,omitempty"`
	PendingChangeType         string     `json:"pending_change_type,omitempty"`
	PendingChangeTargetPlanID string     `json:"pending_change_target_plan_id,omitempty"`
	PendingChangeEffectiveAt  *time.Time `json:"pending_change_effective_at,omitempty"`
}

type BillingContracts struct {
	Contracts []BillingContract `json:"contracts"`
}

type BillingEntitlementSourceTotal struct {
	Source           string     `json:"source"`
	Label            string     `json:"label"`
	PlanID           string     `json:"plan_id"`
	AvailableUnits   string     `json:"available_units"`
	PendingUnits     string     `json:"pending_units"`
	PeriodStartUnits string     `json:"period_start_units"`
	InlineExpiresAt  *time.Time `json:"inline_expires_at,omitempty"`
}

type BillingEntitlementSlot struct {
	ScopeType        string                          `json:"scope_type"`
	ProductID        string                          `json:"product_id"`
	ProductDisplay   string                          `json:"product_display"`
	BucketID         string                          `json:"bucket_id"`
	BucketDisplay    string                          `json:"bucket_display"`
	SkuID            string                          `json:"sku_id"`
	SkuDisplay       string                          `json:"sku_display"`
	CoverageLabel    string                          `json:"coverage_label"`
	AvailableUnits   string                          `json:"available_units"`
	PendingUnits     string                          `json:"pending_units"`
	PeriodStartUnits string                          `json:"period_start_units"`
	SpentUnits       string                          `json:"spent_units"`
	Sources          []BillingEntitlementSourceTotal `json:"sources"`
}

type BillingEntitlementBucketSection struct {
	BucketID    string                   `json:"bucket_id"`
	DisplayName string                   `json:"display_name"`
	BucketSlot  *BillingEntitlementSlot  `json:"bucket_slot,omitempty"`
	SkuSlots    []BillingEntitlementSlot `json:"sku_slots"`
}

type BillingEntitlementProductSection struct {
	ProductID   string                            `json:"product_id"`
	DisplayName string                            `json:"display_name"`
	ProductSlot *BillingEntitlementSlot           `json:"product_slot,omitempty"`
	Buckets     []BillingEntitlementBucketSection `json:"buckets"`
}

type BillingEntitlementsView struct {
	OrgID     string                             `json:"org_id"`
	Universal BillingEntitlementSlot             `json:"universal"`
	Products  []BillingEntitlementProductSection `json:"products"`
}

type BillingPlan struct {
	PlanID             string `json:"plan_id"`
	ProductID          string `json:"product_id"`
	DisplayName        string `json:"display_name"`
	Tier               string `json:"tier"`
	BillingMode        string `json:"billing_mode"`
	Currency           string `json:"currency"`
	MonthlyAmountCents string `json:"monthly_amount_cents"`
	AnnualAmountCents  string `json:"annual_amount_cents"`
	Active             bool   `json:"active"`
	IsDefault          bool   `json:"is_default"`
}

type BillingPlans struct {
	Plans []BillingPlan `json:"plans"`
}

type BillingStatementGrantSummary struct {
	Source         string `json:"source"`
	ScopeType      string `json:"scope_type"`
	ScopeProductID string `json:"scope_product_id"`
	ScopeBucketID  string `json:"scope_bucket_id"`
	Available      string `json:"available"`
	Pending        string `json:"pending"`
}

type BillingStatementLineItem struct {
	ProductID         string  `json:"product_id"`
	BucketID          string  `json:"bucket_id"`
	BucketDisplayName string  `json:"bucket_display_name"`
	SkuID             string  `json:"sku_id"`
	SkuDisplayName    string  `json:"sku_display_name"`
	PlanID            string  `json:"plan_id"`
	PricingPhase      string  `json:"pricing_phase"`
	Quantity          float64 `json:"quantity"`
	QuantityUnit      string  `json:"quantity_unit"`
	UnitRate          string  `json:"unit_rate"`
	ReservedUnits     string  `json:"reserved_units"`
	ContractUnits     string  `json:"contract_units"`
	FreeTierUnits     string  `json:"free_tier_units"`
	PromoUnits        string  `json:"promo_units"`
	PurchaseUnits     string  `json:"purchase_units"`
	ReceivableUnits   string  `json:"receivable_units"`
	RefundUnits       string  `json:"refund_units"`
	ChargeUnits       string  `json:"charge_units"`
}

type BillingStatementTotals struct {
	ReservedUnits   string `json:"reserved_units"`
	ContractUnits   string `json:"contract_units"`
	FreeTierUnits   string `json:"free_tier_units"`
	PromoUnits      string `json:"promo_units"`
	PurchaseUnits   string `json:"purchase_units"`
	ReceivableUnits string `json:"receivable_units"`
	RefundUnits     string `json:"refund_units"`
	ChargeUnits     string `json:"charge_units"`
	TotalDueUnits   string `json:"total_due_units"`
}

type BillingStatement struct {
	OrgID          string                         `json:"org_id"`
	ProductID      string                         `json:"product_id"`
	PeriodSource   string                         `json:"period_source"`
	PeriodStart    time.Time                      `json:"period_start"`
	PeriodEnd      time.Time                      `json:"period_end"`
	GeneratedAt    time.Time                      `json:"generated_at"`
	Currency       string                         `json:"currency"`
	UnitLabel      string                         `json:"unit_label"`
	Totals         BillingStatementTotals         `json:"totals"`
	GrantSummaries []BillingStatementGrantSummary `json:"grant_summaries"`
	LineItems      []BillingStatementLineItem     `json:"line_items"`
}

type BillingURL struct {
	URL string `json:"url"`
}

type BillingContractChange struct {
	URL            string `json:"url"`
	ChangeID       string `json:"change_id"`
	FinalizationID string `json:"finalization_id,omitempty"`
	DocumentID     string `json:"document_id,omitempty"`
	Status         string `json:"status"`
	PriceDelta     string `json:"price_delta_units"`
}

type BillingCancelContractResponse struct {
	Contract BillingContract `json:"contract"`
}

type BillingListGrantsOptions struct {
	ProductID string
	Active    bool
}

type BillingListDocumentsOptions struct {
	ProductID string
}

type BillingListPlansOptions struct {
	ProductID string
}

type BillingCheckoutInput struct {
	ProductID      string
	AmountCents    int64
	SuccessURL     string
	CancelURL      string
	IdempotencyKey string
}

type BillingContractInput struct {
	PlanID         string
	Cadence        string
	SuccessURL     string
	CancelURL      string
	IdempotencyKey string
}

type BillingContractChangeInput struct {
	ContractID     string
	TargetPlanID   string
	SuccessURL     string
	CancelURL      string
	IdempotencyKey string
}

type BillingPortalInput struct {
	ReturnURL      string
	IdempotencyKey string
}

type BillingCancelContractInput struct {
	ContractID     string
	IdempotencyKey string
}

type BillingStatementOptions struct {
	ProductID string
}

type BillingClient struct {
	client *billingcore.Client
}

func (c *BillingClient) GetEntitlements(ctx context.Context) (BillingEntitlementsView, error) {
	if c == nil || c.client == nil {
		return BillingEntitlementsView{}, fmt.Errorf("verself sdk: billing client is not initialized")
	}
	response, err := c.client.GetBillingEntitlements(ctx, billingcore.GetBillingEntitlementsRequest{})
	if err != nil {
		return BillingEntitlementsView{}, err
	}
	if response.Result == nil {
		return BillingEntitlementsView{}, billingAPIError("get entitlements", response.StatusCode, response.Problem, response.Body)
	}
	return billingFromGenerated[BillingEntitlementsView](*response.Result)
}

func (c *BillingClient) ListGrants(ctx context.Context, options BillingListGrantsOptions) (BillingGrants, error) {
	if c == nil || c.client == nil {
		return BillingGrants{}, fmt.Errorf("verself sdk: billing client is not initialized")
	}
	request := billingcore.ListBillingGrantsRequest{}
	if strings.TrimSpace(options.ProductID) != "" {
		productID := billingcore.ProductId(strings.TrimSpace(options.ProductID))
		request.ProductID = &productID
	}
	if options.Active {
		active := true
		request.Active = &active
	}
	response, err := c.client.ListBillingGrants(ctx, request)
	if err != nil {
		return BillingGrants{}, err
	}
	if response.Result == nil {
		return BillingGrants{}, billingAPIError("list grants", response.StatusCode, response.Problem, response.Body)
	}
	return billingFromGenerated[BillingGrants](*response.Result)
}

func (c *BillingClient) ListDocuments(ctx context.Context, options BillingListDocumentsOptions) (BillingDocuments, error) {
	if c == nil || c.client == nil {
		return BillingDocuments{}, fmt.Errorf("verself sdk: billing client is not initialized")
	}
	request := billingcore.ListBillingDocumentsRequest{}
	if strings.TrimSpace(options.ProductID) != "" {
		productID := billingcore.ProductId(strings.TrimSpace(options.ProductID))
		request.ProductID = &productID
	}
	response, err := c.client.ListBillingDocuments(ctx, request)
	if err != nil {
		return BillingDocuments{}, err
	}
	if response.Result == nil {
		return BillingDocuments{}, billingAPIError("list documents", response.StatusCode, response.Problem, response.Body)
	}
	return billingFromGenerated[BillingDocuments](*response.Result)
}

func (c *BillingClient) ListContracts(ctx context.Context) (BillingContracts, error) {
	if c == nil || c.client == nil {
		return BillingContracts{}, fmt.Errorf("verself sdk: billing client is not initialized")
	}
	response, err := c.client.ListBillingContracts(ctx, billingcore.ListBillingContractsRequest{})
	if err != nil {
		return BillingContracts{}, err
	}
	if response.Result == nil {
		return BillingContracts{}, billingAPIError("list contracts", response.StatusCode, response.Problem, response.Body)
	}
	return billingFromGenerated[BillingContracts](*response.Result)
}

func (c *BillingClient) ListPlans(ctx context.Context, options BillingListPlansOptions) (BillingPlans, error) {
	if c == nil || c.client == nil {
		return BillingPlans{}, fmt.Errorf("verself sdk: billing client is not initialized")
	}
	productID := strings.TrimSpace(options.ProductID)
	if productID == "" {
		return BillingPlans{}, fmt.Errorf("verself sdk: billing plans product id is required")
	}
	response, err := c.client.ListBillingPlans(ctx, billingcore.ListBillingPlansRequest{ProductID: billingcore.ProductId(productID)})
	if err != nil {
		return BillingPlans{}, err
	}
	if response.Result == nil {
		return BillingPlans{}, billingAPIError("list plans", response.StatusCode, response.Problem, response.Body)
	}
	return billingFromGenerated[BillingPlans](*response.Result)
}

func (c *BillingClient) GetStatement(ctx context.Context, options BillingStatementOptions) (BillingStatement, error) {
	if c == nil || c.client == nil {
		return BillingStatement{}, fmt.Errorf("verself sdk: billing client is not initialized")
	}
	productID := strings.TrimSpace(options.ProductID)
	if productID == "" {
		return BillingStatement{}, fmt.Errorf("verself sdk: billing statement product id is required")
	}
	response, err := c.client.GetBillingStatement(ctx, billingcore.GetBillingStatementRequest{ProductID: billingcore.ProductId(productID)})
	if err != nil {
		return BillingStatement{}, err
	}
	if response.Result == nil {
		return BillingStatement{}, billingAPIError("get statement", response.StatusCode, response.Problem, response.Body)
	}
	return billingFromGenerated[BillingStatement](*response.Result)
}

func (c *BillingClient) CreateCheckoutSession(ctx context.Context, input BillingCheckoutInput) (BillingURL, error) {
	if c == nil || c.client == nil {
		return BillingURL{}, fmt.Errorf("verself sdk: billing client is not initialized")
	}
	if strings.TrimSpace(input.ProductID) == "" || strings.TrimSpace(input.SuccessURL) == "" || strings.TrimSpace(input.CancelURL) == "" {
		return BillingURL{}, fmt.Errorf("verself sdk: billing checkout requires product id, success url, and cancel url")
	}
	key, err := mutationKey("billing-checkout", input.IdempotencyKey)
	if err != nil {
		return BillingURL{}, err
	}
	body := billingcore.CheckoutInputBody{
		ProductID:   billingcore.ProductId(strings.TrimSpace(input.ProductID)),
		AmountCents: input.AmountCents,
		SuccessURL:  billingcore.URL(strings.TrimSpace(input.SuccessURL)),
		CancelURL:   billingcore.URL(strings.TrimSpace(input.CancelURL)),
	}
	response, err := c.client.CreateBillingCheckout(ctx, billingcore.CreateBillingCheckoutRequest{IdempotencyKey: billingcore.IdempotencyKey(key), Body: body})
	if err != nil {
		return BillingURL{}, err
	}
	if response.Result == nil {
		return BillingURL{}, billingAPIError("create checkout", response.StatusCode, response.Problem, response.Body)
	}
	return billingFromGenerated[BillingURL](*response.Result)
}

func (c *BillingClient) CreateContractSession(ctx context.Context, input BillingContractInput) (BillingURL, error) {
	if c == nil || c.client == nil {
		return BillingURL{}, fmt.Errorf("verself sdk: billing client is not initialized")
	}
	if strings.TrimSpace(input.PlanID) == "" || strings.TrimSpace(input.SuccessURL) == "" || strings.TrimSpace(input.CancelURL) == "" {
		return BillingURL{}, fmt.Errorf("verself sdk: billing contract requires plan id, success url, and cancel url")
	}
	key, err := mutationKey("billing-contract", input.IdempotencyKey)
	if err != nil {
		return BillingURL{}, err
	}
	body := billingcore.CreateBillingContractInputBody{
		PlanID:     billingcore.PlanId(strings.TrimSpace(input.PlanID)),
		SuccessURL: billingcore.URL(strings.TrimSpace(input.SuccessURL)),
		CancelURL:  billingcore.URL(strings.TrimSpace(input.CancelURL)),
	}
	if strings.TrimSpace(input.Cadence) != "" {
		cadence := billingcore.BillingCadence(strings.TrimSpace(input.Cadence))
		body.Cadence = &cadence
	}
	response, err := c.client.CreateBillingContract(ctx, billingcore.CreateBillingContractRequest{IdempotencyKey: billingcore.IdempotencyKey(key), Body: body})
	if err != nil {
		return BillingURL{}, err
	}
	if response.Result == nil {
		return BillingURL{}, billingAPIError("create contract", response.StatusCode, response.Problem, response.Body)
	}
	return billingFromGenerated[BillingURL](*response.Result)
}

func (c *BillingClient) CreateContractChangeSession(ctx context.Context, input BillingContractChangeInput) (BillingContractChange, error) {
	if c == nil || c.client == nil {
		return BillingContractChange{}, fmt.Errorf("verself sdk: billing client is not initialized")
	}
	contractID := strings.TrimSpace(input.ContractID)
	if contractID == "" || strings.TrimSpace(input.TargetPlanID) == "" || strings.TrimSpace(input.SuccessURL) == "" || strings.TrimSpace(input.CancelURL) == "" {
		return BillingContractChange{}, fmt.Errorf("verself sdk: billing contract change requires contract id, target plan id, success url, and cancel url")
	}
	key, err := mutationKey("billing-contract-change", input.IdempotencyKey)
	if err != nil {
		return BillingContractChange{}, err
	}
	body := billingcore.CreateBillingContractChangeInputBody{
		TargetPlanID: billingcore.PlanId(strings.TrimSpace(input.TargetPlanID)),
		SuccessURL:   billingcore.URL(strings.TrimSpace(input.SuccessURL)),
		CancelURL:    billingcore.URL(strings.TrimSpace(input.CancelURL)),
	}
	response, err := c.client.CreateBillingContractChange(ctx, billingcore.CreateBillingContractChangeRequest{ContractID: billingcore.ContractId(contractID), IdempotencyKey: billingcore.IdempotencyKey(key), Body: body})
	if err != nil {
		return BillingContractChange{}, err
	}
	if response.Result == nil {
		return BillingContractChange{}, billingAPIError("create contract change", response.StatusCode, response.Problem, response.Body)
	}
	return billingFromGenerated[BillingContractChange](*response.Result)
}

func (c *BillingClient) CreatePortalSession(ctx context.Context, input BillingPortalInput) (BillingURL, error) {
	if c == nil || c.client == nil {
		return BillingURL{}, fmt.Errorf("verself sdk: billing client is not initialized")
	}
	if strings.TrimSpace(input.ReturnURL) == "" {
		return BillingURL{}, fmt.Errorf("verself sdk: billing portal return url is required")
	}
	key, err := mutationKey("billing-portal", input.IdempotencyKey)
	if err != nil {
		return BillingURL{}, err
	}
	body := billingcore.CreateBillingPortalInputBody{ReturnURL: billingcore.URL(strings.TrimSpace(input.ReturnURL))}
	response, err := c.client.CreateBillingPortal(ctx, billingcore.CreateBillingPortalRequest{IdempotencyKey: billingcore.IdempotencyKey(key), Body: body})
	if err != nil {
		return BillingURL{}, err
	}
	if response.Result == nil {
		return BillingURL{}, billingAPIError("create portal", response.StatusCode, response.Problem, response.Body)
	}
	return billingFromGenerated[BillingURL](*response.Result)
}

func (c *BillingClient) CancelContract(ctx context.Context, input BillingCancelContractInput) (BillingCancelContractResponse, error) {
	if c == nil || c.client == nil {
		return BillingCancelContractResponse{}, fmt.Errorf("verself sdk: billing client is not initialized")
	}
	contractID := strings.TrimSpace(input.ContractID)
	if contractID == "" {
		return BillingCancelContractResponse{}, fmt.Errorf("verself sdk: billing contract id is required")
	}
	key, err := mutationKey("billing-contract-cancel", input.IdempotencyKey)
	if err != nil {
		return BillingCancelContractResponse{}, err
	}
	response, err := c.client.CancelBillingContract(ctx, billingcore.CancelBillingContractRequest{ContractID: billingcore.ContractId(contractID), IdempotencyKey: billingcore.IdempotencyKey(key)})
	if err != nil {
		return BillingCancelContractResponse{}, err
	}
	if response.Result == nil {
		return BillingCancelContractResponse{}, billingAPIError("cancel contract", response.StatusCode, response.Problem, response.Body)
	}
	return billingFromGenerated[BillingCancelContractResponse](*response.Result)
}

func billingAPIError(operation string, statusCode int, model *billingcore.ErrorModel, body []byte) error {
	var title *string
	var detail *string
	if model != nil {
		title = model.Title
		detail = model.Detail
	}
	return apiErrorFields("Billing API", operation, statusCode, title, detail, body)
}

func billingFromGenerated[T any](input any) (T, error) {
	var out T
	raw, err := json.Marshal(input)
	if err != nil {
		return out, fmt.Errorf("verself sdk: encode billing response: %w", err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("verself sdk: decode billing response: %w", err)
	}
	return out, nil
}
