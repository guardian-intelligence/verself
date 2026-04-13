package billingclient

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/forge-metal/apiwire"
)

var (
	ErrPaymentRequired  = errors.New("billing-client: payment required")
	ErrForbidden        = errors.New("billing-client: forbidden")
	ErrNoStripeCustomer = errors.New("billing-client: no stripe customer")
	ErrContractNotFound = errors.New("billing-client: contract not found")
	ErrUnexpected       = errors.New("billing-client: unexpected response")
)

const problemTypeNoStripeCustomer = "urn:forge-metal:problem:billing:no-stripe-customer"

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
	OrgId            uint64
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
	SKURates         map[string]int64
	CostPerSec       int64
	WindowStart      time.Time
	ActivatedAt      *time.Time
	ExpiresAt        time.Time
	RenewBy          time.Time
}

func (c *ServiceClient) GetEntitlements(ctx context.Context, orgID uint64, reqEditors ...RequestEditorFn) (apiwire.BillingEntitlementsView, error) {
	orgIDWire := apiwire.Uint64(orgID).String()
	resp, err := c.inner.GetEntitlementsWithResponse(ctx, orgIDWire, reqEditors...)
	if err != nil {
		return apiwire.BillingEntitlementsView{}, err
	}
	if resp.JSON200 != nil {
		return parseBillingEntitlements(*resp.JSON200)
	}
	return apiwire.BillingEntitlementsView{}, unexpected("get entitlements", resp.HTTPResponse, resp.ApplicationproblemJSONDefault)
}

func (c *ServiceClient) ListContracts(ctx context.Context, orgID uint64, reqEditors ...RequestEditorFn) (apiwire.BillingContracts, error) {
	orgIDWire := apiwire.Uint64(orgID).String()
	resp, err := c.inner.ListContractsWithResponse(ctx, orgIDWire, reqEditors...)
	if err != nil {
		return apiwire.BillingContracts{}, err
	}
	if resp.JSON200 != nil {
		return parseBillingContracts(*resp.JSON200)
	}
	return apiwire.BillingContracts{}, unexpected("list contracts", resp.HTTPResponse, resp.ApplicationproblemJSONDefault)
}

func (c *ServiceClient) ListPlans(ctx context.Context, productID string, reqEditors ...RequestEditorFn) (apiwire.BillingPlans, error) {
	resp, err := c.inner.ListPlansWithResponse(ctx, productID, reqEditors...)
	if err != nil {
		return apiwire.BillingPlans{}, err
	}
	if resp.JSON200 != nil {
		return parseBillingPlans(*resp.JSON200)
	}
	return apiwire.BillingPlans{}, unexpected("list plans", resp.HTTPResponse, resp.ApplicationproblemJSONDefault)
}

func (c *ServiceClient) ListGrants(ctx context.Context, orgID uint64, productID string, active bool, reqEditors ...RequestEditorFn) (apiwire.BillingGrants, error) {
	orgIDWire := apiwire.Uint64(orgID).String()
	params := &ListGrantsParams{}
	if productID != "" {
		params.ProductId = &productID
	}
	if active {
		params.Active = &active
	}
	resp, err := c.inner.ListGrantsWithResponse(ctx, orgIDWire, params, reqEditors...)
	if err != nil {
		return apiwire.BillingGrants{}, err
	}
	if resp.JSON200 != nil {
		return parseBillingGrants(*resp.JSON200)
	}
	return apiwire.BillingGrants{}, unexpected("list grants", resp.HTTPResponse, resp.ApplicationproblemJSONDefault)
}

func (c *ServiceClient) GetStatement(ctx context.Context, orgID uint64, productID string, reqEditors ...RequestEditorFn) (apiwire.BillingStatement, error) {
	orgIDWire := apiwire.Uint64(orgID).String()
	resp, err := c.inner.GetStatementWithResponse(ctx, orgIDWire, &GetStatementParams{ProductId: productID}, reqEditors...)
	if err != nil {
		return apiwire.BillingStatement{}, err
	}
	if resp.JSON200 != nil {
		return parseBillingStatement(*resp.JSON200)
	}
	return apiwire.BillingStatement{}, unexpected("get statement", resp.HTTPResponse, resp.ApplicationproblemJSONDefault)
}

func (c *ServiceClient) CreateCheckout(ctx context.Context, orgID uint64, productID string, amountCents int64, successURL string, cancelURL string, reqEditors ...RequestEditorFn) (string, error) {
	orgIDWire := apiwire.Uint64(orgID).String()
	resp, err := c.inner.CreateCheckoutWithResponse(ctx, CreateCheckoutJSONRequestBody{
		OrgId:       orgIDWire,
		ProductId:   productID,
		AmountCents: amountCents,
		SuccessUrl:  successURL,
		CancelUrl:   cancelURL,
	}, reqEditors...)
	if err != nil {
		return "", err
	}
	if resp.JSON200 != nil {
		return resp.JSON200.Url, nil
	}
	return "", unexpected("create checkout", resp.HTTPResponse, resp.ApplicationproblemJSONDefault)
}

func (c *ServiceClient) CreateContract(ctx context.Context, orgID uint64, planID string, cadence string, successURL string, cancelURL string, reqEditors ...RequestEditorFn) (string, error) {
	orgIDWire := apiwire.Uint64(orgID).String()
	body := CreateContractJSONRequestBody{
		OrgId:      orgIDWire,
		PlanId:     planID,
		SuccessUrl: successURL,
		CancelUrl:  cancelURL,
	}
	if cadence != "" {
		wireCadence := BillingCreateContractRequestCadence(cadence)
		body.Cadence = &wireCadence
	}
	resp, err := c.inner.CreateContractWithResponse(ctx, body, reqEditors...)
	if err != nil {
		return "", err
	}
	if resp.JSON200 != nil {
		return resp.JSON200.Url, nil
	}
	return "", unexpected("create contract", resp.HTTPResponse, firstProblem(resp.ApplicationproblemJSON400, resp.ApplicationproblemJSON500, resp.ApplicationproblemJSON422))
}

func (c *ServiceClient) CreateContractChange(ctx context.Context, orgID uint64, contractID string, targetPlanID string, successURL string, cancelURL string, reqEditors ...RequestEditorFn) (apiwire.BillingContractChangeResponse, error) {
	orgIDWire := apiwire.Uint64(orgID).String()
	resp, err := c.inner.CreateContractChangeWithResponse(ctx, contractID, CreateContractChangeJSONRequestBody{
		OrgId:        orgIDWire,
		TargetPlanId: targetPlanID,
		SuccessUrl:   successURL,
		CancelUrl:    cancelURL,
	}, reqEditors...)
	if err != nil {
		return apiwire.BillingContractChangeResponse{}, err
	}
	if resp.JSON200 != nil {
		priceDelta, err := parseDecimalUint64(resp.JSON200.PriceDeltaUnits, "price_delta_units")
		if err != nil {
			return apiwire.BillingContractChangeResponse{}, err
		}
		return apiwire.BillingContractChangeResponse{
			URL:        resp.JSON200.Url,
			ChangeID:   resp.JSON200.ChangeId,
			InvoiceID:  resp.JSON200.InvoiceId,
			Status:     resp.JSON200.Status,
			PriceDelta: priceDelta,
		}, nil
	}
	if statusCode(resp.HTTPResponse) == http.StatusNotFound {
		return apiwire.BillingContractChangeResponse{}, fmt.Errorf("%w: %s", ErrContractNotFound, detail(resp.ApplicationproblemJSON404, resp.HTTPResponse))
	}
	return apiwire.BillingContractChangeResponse{}, unexpected("create contract change", resp.HTTPResponse, firstProblem(resp.ApplicationproblemJSON400, resp.ApplicationproblemJSON404, resp.ApplicationproblemJSON500, resp.ApplicationproblemJSON422))
}

func (c *ServiceClient) CancelContract(ctx context.Context, orgID uint64, contractID string, reqEditors ...RequestEditorFn) (apiwire.BillingCancelContractResponse, error) {
	orgIDWire := apiwire.Uint64(orgID).String()
	resp, err := c.inner.CancelContractWithResponse(ctx, contractID, CancelContractJSONRequestBody{
		OrgId: orgIDWire,
	}, reqEditors...)
	if err != nil {
		return apiwire.BillingCancelContractResponse{}, err
	}
	if resp.JSON200 != nil {
		contract, err := parseBillingContract(resp.JSON200.Contract)
		if err != nil {
			return apiwire.BillingCancelContractResponse{}, err
		}
		return apiwire.BillingCancelContractResponse{Contract: contract}, nil
	}
	if statusCode(resp.HTTPResponse) == http.StatusNotFound {
		return apiwire.BillingCancelContractResponse{}, fmt.Errorf("%w: %s", ErrContractNotFound, detail(resp.ApplicationproblemJSON404, resp.HTTPResponse))
	}
	return apiwire.BillingCancelContractResponse{}, unexpected("cancel contract", resp.HTTPResponse, firstProblem(resp.ApplicationproblemJSON500, resp.ApplicationproblemJSON404, resp.ApplicationproblemJSON422))
}

func (c *ServiceClient) CreatePortalSession(ctx context.Context, orgID uint64, returnURL string, reqEditors ...RequestEditorFn) (string, error) {
	orgIDWire := apiwire.Uint64(orgID).String()
	resp, err := c.inner.CreatePortalWithResponse(ctx, CreatePortalJSONRequestBody{
		OrgId:     orgIDWire,
		ReturnUrl: returnURL,
	}, reqEditors...)
	if err != nil {
		return "", err
	}
	if resp.JSON200 != nil {
		return resp.JSON200.Url, nil
	}
	if statusCode(resp.HTTPResponse) == http.StatusUnprocessableEntity && problemType(resp.ApplicationproblemJSON422) == problemTypeNoStripeCustomer {
		return "", fmt.Errorf("%w: %s", ErrNoStripeCustomer, detail(resp.ApplicationproblemJSON422, resp.HTTPResponse))
	}
	return "", unexpected("create portal session", resp.HTTPResponse, firstProblem(resp.ApplicationproblemJSON500, resp.ApplicationproblemJSON422))
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
	windowSeq uint32,
	allocation map[string]float64,
	reqEditors ...RequestEditorFn,
) (Reservation, error) {
	orgIDWire := apiwire.Uint64(orgID).String()
	concurrentCountWire, err := uint64ToInt64(concurrentCount, "concurrent_count")
	if err != nil {
		return Reservation{}, err
	}
	windowSeqWire, err := uint32ToInt32(windowSeq, "window_seq")
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
		WindowSeq:       windowSeqWire,
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
	case http.StatusBadRequest:
		return Reservation{}, fmt.Errorf("billing-client: reserve bad request: %s", detail(resp.ApplicationproblemJSON400, resp.HTTPResponse))
	case http.StatusUnprocessableEntity:
		return Reservation{}, fmt.Errorf("billing-client: reserve bad request: %s", detail(resp.ApplicationproblemJSON422, resp.HTTPResponse))
	default:
		return Reservation{}, unexpected("reserve", resp.HTTPResponse, firstProblem(resp.ApplicationproblemJSON500, resp.ApplicationproblemJSON422))
	}
}

func parseReservation(in BillingWindowReservation) (Reservation, error) {
	orgID, err := apiwire.ParseUint64(in.OrgId)
	if err != nil {
		return Reservation{}, fmt.Errorf("billing-client: reservation org_id: %w", err)
	}
	skuRates, err := parseInt64SKURates(in.SkuRates)
	if err != nil {
		return Reservation{}, err
	}
	costPerUnit, err := parseUint64DecimalAsInt64(in.CostPerUnit, "cost_per_unit")
	if err != nil {
		return Reservation{}, err
	}
	var renewBy time.Time
	if in.RenewBy != nil {
		renewBy = in.RenewBy.UTC()
	}
	var activatedAt *time.Time
	if in.ActivatedAt != nil {
		value := in.ActivatedAt.UTC()
		activatedAt = &value
	}
	return Reservation{
		WindowId:         in.WindowId,
		OrgId:            orgID,
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
		SKURates:         skuRates,
		CostPerSec:       costPerUnit,
		WindowStart:      in.WindowStart.UTC(),
		ActivatedAt:      activatedAt,
		ExpiresAt:        in.ExpiresAt.UTC(),
		RenewBy:          renewBy,
	}, nil
}

func parseBillingEntitlements(in BillingEntitlementsView) (apiwire.BillingEntitlementsView, error) {
	orgID, err := parseDecimalUint64(in.OrgId, "org_id")
	if err != nil {
		return apiwire.BillingEntitlementsView{}, err
	}
	universal, err := parseEntitlementSlot(in.Universal, "universal")
	if err != nil {
		return apiwire.BillingEntitlementsView{}, err
	}
	var productsIn []BillingEntitlementProductSection
	if in.Products != nil {
		productsIn = *in.Products
	}
	products := make([]apiwire.BillingEntitlementProductSection, 0, len(productsIn))
	for i, section := range productsIn {
		productSlot, err := parseEntitlementSlotPtr(section.ProductSlot, fmt.Sprintf("products[%d].product_slot", i))
		if err != nil {
			return apiwire.BillingEntitlementsView{}, err
		}
		var bucketsIn []BillingEntitlementBucketSection
		if section.Buckets != nil {
			bucketsIn = *section.Buckets
		}
		buckets := make([]apiwire.BillingEntitlementBucketSection, 0, len(bucketsIn))
		for j, bucket := range bucketsIn {
			bucketSlot, err := parseEntitlementSlotPtr(bucket.BucketSlot, fmt.Sprintf("products[%d].buckets[%d].bucket_slot", i, j))
			if err != nil {
				return apiwire.BillingEntitlementsView{}, err
			}
			var skuSlotsIn []BillingEntitlementSlot
			if bucket.SkuSlots != nil {
				skuSlotsIn = *bucket.SkuSlots
			}
			skuSlots := make([]apiwire.BillingEntitlementSlot, 0, len(skuSlotsIn))
			for k, slot := range skuSlotsIn {
				parsed, err := parseEntitlementSlot(slot, fmt.Sprintf("products[%d].buckets[%d].sku_slots[%d]", i, j, k))
				if err != nil {
					return apiwire.BillingEntitlementsView{}, err
				}
				skuSlots = append(skuSlots, parsed)
			}
			buckets = append(buckets, apiwire.BillingEntitlementBucketSection{
				BucketID:    bucket.BucketId,
				DisplayName: bucket.DisplayName,
				BucketSlot:  bucketSlot,
				SKUSlots:    skuSlots,
			})
		}
		products = append(products, apiwire.BillingEntitlementProductSection{
			ProductID:   section.ProductId,
			DisplayName: section.DisplayName,
			ProductSlot: productSlot,
			Buckets:     buckets,
		})
	}
	return apiwire.BillingEntitlementsView{
		OrgID:     orgID,
		Universal: universal,
		Products:  products,
	}, nil
}

func parseEntitlementSlotPtr(in *BillingEntitlementSlot, label string) (*apiwire.BillingEntitlementSlot, error) {
	if in == nil {
		return nil, nil
	}
	parsed, err := parseEntitlementSlot(*in, label)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func parseEntitlementSlot(in BillingEntitlementSlot, label string) (apiwire.BillingEntitlementSlot, error) {
	periodStart, err := parseDecimalUint64(in.PeriodStartUnits, label+".period_start_units")
	if err != nil {
		return apiwire.BillingEntitlementSlot{}, err
	}
	spent, err := parseDecimalUint64(in.SpentUnits, label+".spent_units")
	if err != nil {
		return apiwire.BillingEntitlementSlot{}, err
	}
	pending, err := parseDecimalUint64(in.PendingUnits, label+".pending_units")
	if err != nil {
		return apiwire.BillingEntitlementSlot{}, err
	}
	available, err := parseDecimalUint64(in.AvailableUnits, label+".available_units")
	if err != nil {
		return apiwire.BillingEntitlementSlot{}, err
	}
	var sourcesIn []BillingEntitlementSourceTotal
	if in.Sources != nil {
		sourcesIn = *in.Sources
	}
	sources := make([]apiwire.BillingEntitlementSourceTotal, 0, len(sourcesIn))
	for i, src := range sourcesIn {
		srcPeriodStart, err := parseDecimalUint64(src.PeriodStartUnits, fmt.Sprintf("%s.sources[%d].period_start_units", label, i))
		if err != nil {
			return apiwire.BillingEntitlementSlot{}, err
		}
		srcAvailable, err := parseDecimalUint64(src.AvailableUnits, fmt.Sprintf("%s.sources[%d].available_units", label, i))
		if err != nil {
			return apiwire.BillingEntitlementSlot{}, err
		}
		sources = append(sources, apiwire.BillingEntitlementSourceTotal{
			Source:           string(src.Source),
			PlanID:           src.PlanId,
			Label:            src.Label,
			PeriodStartUnits: srcPeriodStart,
			AvailableUnits:   srcAvailable,
			InlineExpiresAt:  src.InlineExpiresAt,
		})
	}
	return apiwire.BillingEntitlementSlot{
		ScopeType:        string(in.ScopeType),
		ProductID:        in.ProductId,
		ProductDisplay:   in.ProductDisplay,
		BucketID:         in.BucketId,
		BucketDisplay:    in.BucketDisplay,
		SKUID:            in.SkuId,
		SKUDisplay:       in.SkuDisplay,
		CoverageLabel:    in.CoverageLabel,
		PeriodStartUnits: periodStart,
		SpentUnits:       spent,
		PendingUnits:     pending,
		AvailableUnits:   available,
		Sources:          sources,
	}, nil
}

func parseBillingGrants(in BillingGrants) (apiwire.BillingGrants, error) {
	if in.Grants == nil {
		return apiwire.BillingGrants{}, nil
	}
	out := make([]apiwire.BillingGrant, 0, len(*in.Grants))
	for _, grant := range *in.Grants {
		available, err := parseDecimalUint64(grant.Available, "available")
		if err != nil {
			return apiwire.BillingGrants{}, err
		}
		pending, err := parseDecimalUint64(grant.Pending, "pending")
		if err != nil {
			return apiwire.BillingGrants{}, err
		}
		out = append(out, apiwire.BillingGrant{
			GrantID:        grant.GrantId,
			ScopeType:      grant.ScopeType,
			ScopeProductID: grant.ScopeProductId,
			ScopeBucketID:  grant.ScopeBucketId,
			Source:         grant.Source,
			Available:      available,
			Pending:        pending,
			ExpiresAt:      grant.ExpiresAt,
		})
	}
	return apiwire.BillingGrants{Grants: out}, nil
}

func parseBillingStatement(in BillingStatement) (apiwire.BillingStatement, error) {
	orgID, err := parseDecimalUint64(in.OrgId, "org_id")
	if err != nil {
		return apiwire.BillingStatement{}, err
	}
	lineItems := make([]apiwire.BillingStatementLineItem, 0)
	if in.LineItems != nil {
		lineItems = make([]apiwire.BillingStatementLineItem, 0, len(*in.LineItems))
		for _, line := range *in.LineItems {
			parsed, err := parseBillingStatementLineItem(line)
			if err != nil {
				return apiwire.BillingStatement{}, err
			}
			lineItems = append(lineItems, parsed)
		}
	}

	grantSummaries := make([]apiwire.BillingStatementGrantSummary, 0)
	if in.GrantSummaries != nil {
		grantSummaries = make([]apiwire.BillingStatementGrantSummary, 0, len(*in.GrantSummaries))
		for _, grant := range *in.GrantSummaries {
			available, err := parseDecimalUint64(grant.Available, "grant_summaries.available")
			if err != nil {
				return apiwire.BillingStatement{}, err
			}
			pending, err := parseDecimalUint64(grant.Pending, "grant_summaries.pending")
			if err != nil {
				return apiwire.BillingStatement{}, err
			}
			grantSummaries = append(grantSummaries, apiwire.BillingStatementGrantSummary{
				ScopeType:      grant.ScopeType,
				ScopeProductID: grant.ScopeProductId,
				ScopeBucketID:  grant.ScopeBucketId,
				Source:         grant.Source,
				Available:      available,
				Pending:        pending,
			})
		}
	}

	totals, err := parseBillingStatementTotals(in.Totals)
	if err != nil {
		return apiwire.BillingStatement{}, err
	}
	return apiwire.BillingStatement{
		OrgID:          orgID,
		ProductID:      in.ProductId,
		PeriodStart:    in.PeriodStart.UTC(),
		PeriodEnd:      in.PeriodEnd.UTC(),
		PeriodSource:   in.PeriodSource,
		GeneratedAt:    in.GeneratedAt.UTC(),
		Currency:       in.Currency,
		UnitLabel:      in.UnitLabel,
		LineItems:      lineItems,
		GrantSummaries: grantSummaries,
		Totals:         totals,
	}, nil
}

func parseBillingStatementLineItem(in BillingStatementLineItem) (apiwire.BillingStatementLineItem, error) {
	unitRate, err := parseDecimalUint64(in.UnitRate, "line_items.unit_rate")
	if err != nil {
		return apiwire.BillingStatementLineItem{}, err
	}
	chargeUnits, err := parseDecimalUint64(in.ChargeUnits, "line_items.charge_units")
	if err != nil {
		return apiwire.BillingStatementLineItem{}, err
	}
	freeTierUnits, err := parseDecimalUint64(in.FreeTierUnits, "line_items.free_tier_units")
	if err != nil {
		return apiwire.BillingStatementLineItem{}, err
	}
	contractUnits, err := parseDecimalUint64(in.ContractUnits, "line_items.contract_units")
	if err != nil {
		return apiwire.BillingStatementLineItem{}, err
	}
	purchaseUnits, err := parseDecimalUint64(in.PurchaseUnits, "line_items.purchase_units")
	if err != nil {
		return apiwire.BillingStatementLineItem{}, err
	}
	promoUnits, err := parseDecimalUint64(in.PromoUnits, "line_items.promo_units")
	if err != nil {
		return apiwire.BillingStatementLineItem{}, err
	}
	refundUnits, err := parseDecimalUint64(in.RefundUnits, "line_items.refund_units")
	if err != nil {
		return apiwire.BillingStatementLineItem{}, err
	}
	receivableUnits, err := parseDecimalUint64(in.ReceivableUnits, "line_items.receivable_units")
	if err != nil {
		return apiwire.BillingStatementLineItem{}, err
	}
	reservedUnits, err := parseDecimalUint64(in.ReservedUnits, "line_items.reserved_units")
	if err != nil {
		return apiwire.BillingStatementLineItem{}, err
	}
	return apiwire.BillingStatementLineItem{
		ProductID:         in.ProductId,
		PlanID:            in.PlanId,
		BucketID:          in.BucketId,
		BucketDisplayName: in.BucketDisplayName,
		SKUID:             in.SkuId,
		SKUDisplayName:    in.SkuDisplayName,
		QuantityUnit:      in.QuantityUnit,
		PricingPhase:      in.PricingPhase,
		Quantity:          in.Quantity,
		UnitRate:          unitRate,
		ChargeUnits:       chargeUnits,
		FreeTierUnits:     freeTierUnits,
		ContractUnits:     contractUnits,
		PurchaseUnits:     purchaseUnits,
		PromoUnits:        promoUnits,
		RefundUnits:       refundUnits,
		ReceivableUnits:   receivableUnits,
		ReservedUnits:     reservedUnits,
	}, nil
}

func parseBillingStatementTotals(in BillingStatementTotals) (apiwire.BillingStatementTotals, error) {
	chargeUnits, err := parseDecimalUint64(in.ChargeUnits, "totals.charge_units")
	if err != nil {
		return apiwire.BillingStatementTotals{}, err
	}
	freeTierUnits, err := parseDecimalUint64(in.FreeTierUnits, "totals.free_tier_units")
	if err != nil {
		return apiwire.BillingStatementTotals{}, err
	}
	contractUnits, err := parseDecimalUint64(in.ContractUnits, "totals.contract_units")
	if err != nil {
		return apiwire.BillingStatementTotals{}, err
	}
	purchaseUnits, err := parseDecimalUint64(in.PurchaseUnits, "totals.purchase_units")
	if err != nil {
		return apiwire.BillingStatementTotals{}, err
	}
	promoUnits, err := parseDecimalUint64(in.PromoUnits, "totals.promo_units")
	if err != nil {
		return apiwire.BillingStatementTotals{}, err
	}
	refundUnits, err := parseDecimalUint64(in.RefundUnits, "totals.refund_units")
	if err != nil {
		return apiwire.BillingStatementTotals{}, err
	}
	receivableUnits, err := parseDecimalUint64(in.ReceivableUnits, "totals.receivable_units")
	if err != nil {
		return apiwire.BillingStatementTotals{}, err
	}
	reservedUnits, err := parseDecimalUint64(in.ReservedUnits, "totals.reserved_units")
	if err != nil {
		return apiwire.BillingStatementTotals{}, err
	}
	totalDueUnits, err := parseDecimalUint64(in.TotalDueUnits, "totals.total_due_units")
	if err != nil {
		return apiwire.BillingStatementTotals{}, err
	}
	return apiwire.BillingStatementTotals{
		ChargeUnits:     chargeUnits,
		FreeTierUnits:   freeTierUnits,
		ContractUnits:   contractUnits,
		PurchaseUnits:   purchaseUnits,
		PromoUnits:      promoUnits,
		RefundUnits:     refundUnits,
		ReceivableUnits: receivableUnits,
		ReservedUnits:   reservedUnits,
		TotalDueUnits:   totalDueUnits,
	}, nil
}

func parseBillingContracts(in BillingContracts) (apiwire.BillingContracts, error) {
	if in.Contracts == nil {
		return apiwire.BillingContracts{}, nil
	}
	out := make([]apiwire.BillingContract, 0, len(*in.Contracts))
	for _, contract := range *in.Contracts {
		parsed, err := parseBillingContract(contract)
		if err != nil {
			return apiwire.BillingContracts{}, err
		}
		out = append(out, parsed)
	}
	return apiwire.BillingContracts{Contracts: out}, nil
}

func parseBillingContract(contract BillingContract) (apiwire.BillingContract, error) {
	return apiwire.BillingContract{
		ContractID:       contract.ContractId,
		ProductID:        contract.ProductId,
		PlanID:           contract.PlanId,
		PhaseID:          contract.PhaseId,
		CadenceKind:      contract.CadenceKind,
		Status:           contract.Status,
		PaymentState:     contract.PaymentState,
		EntitlementState: contract.EntitlementState,
		StartsAt:         contract.StartsAt.UTC(),
		EndsAt:           contract.EndsAt,
		PhaseStart:       contract.PhaseStart,
		PhaseEnd:         contract.PhaseEnd,
	}, nil
}

func parseBillingPlans(in BillingPlans) (apiwire.BillingPlans, error) {
	if in.Plans == nil {
		return apiwire.BillingPlans{}, nil
	}
	out := make([]apiwire.BillingPlan, 0, len(*in.Plans))
	for _, plan := range *in.Plans {
		monthlyAmount, err := parseDecimalUint64(plan.MonthlyAmountCents, "monthly_amount_cents")
		if err != nil {
			return apiwire.BillingPlans{}, err
		}
		annualAmount, err := parseDecimalUint64(plan.AnnualAmountCents, "annual_amount_cents")
		if err != nil {
			return apiwire.BillingPlans{}, err
		}
		out = append(out, apiwire.BillingPlan{
			PlanID:             plan.PlanId,
			ProductID:          plan.ProductId,
			DisplayName:        plan.DisplayName,
			BillingMode:        plan.BillingMode,
			Tier:               plan.Tier,
			Currency:           plan.Currency,
			MonthlyAmountCents: monthlyAmount,
			AnnualAmountCents:  annualAmount,
			Active:             plan.Active,
			IsDefault:          plan.IsDefault,
		})
	}
	return apiwire.BillingPlans{Plans: out}, nil
}

func parseDecimalUint64(value string, field string) (apiwire.DecimalUint64, error) {
	parsed, err := apiwire.ParseUint64(value)
	if err != nil {
		return apiwire.Uint64(0), fmt.Errorf("billing-client: %s: %w", field, err)
	}
	return apiwire.Uint64(parsed), nil
}

func parseInt64SKURates(in map[string]string) (map[string]int64, error) {
	if len(in) == 0 {
		return map[string]int64{}, nil
	}
	out := make(map[string]int64, len(in))
	for unit, rate := range in {
		parsed, err := parseUint64DecimalAsInt64(rate, "sku_rates."+unit)
		if err != nil {
			return nil, err
		}
		out[unit] = parsed
	}
	return out, nil
}

func parseUint64DecimalAsInt64(value string, field string) (int64, error) {
	parsed, err := apiwire.ParseUint64(value)
	if err != nil {
		return 0, fmt.Errorf("billing-client: %s: %w", field, err)
	}
	if parsed > math.MaxInt64 {
		return 0, fmt.Errorf("billing-client: %s %d exceeds internal int64 range", field, parsed)
	}
	return int64(parsed), nil
}

func (c *ServiceClient) Activate(ctx context.Context, reservation Reservation, activatedAt time.Time, reqEditors ...RequestEditorFn) (Reservation, error) {
	resp, err := c.inner.ActivateWindowWithResponse(ctx, ActivateWindowJSONRequestBody{
		WindowId:    reservation.WindowId,
		ActivatedAt: activatedAt.UTC(),
	}, reqEditors...)
	if err != nil {
		return Reservation{}, err
	}
	if resp.JSON200 != nil {
		return parseReservation(resp.JSON200.Reservation)
	}
	if statusCode(resp.HTTPResponse) == http.StatusBadRequest {
		return Reservation{}, fmt.Errorf("billing-client: activate bad request: %s", detail(resp.ApplicationproblemJSON400, resp.HTTPResponse))
	}
	return Reservation{}, unexpected("activate", resp.HTTPResponse, firstProblem(resp.ApplicationproblemJSON500, resp.ApplicationproblemJSON404, resp.ApplicationproblemJSON422))
}

func (c *ServiceClient) Settle(ctx context.Context, reservation Reservation, actualQuantity uint32, usageSummary map[string]any, reqEditors ...RequestEditorFn) error {
	actualQuantityWire, err := uint32ToInt32(actualQuantity, "actual_quantity")
	if err != nil {
		return err
	}
	body := SettleWindowJSONRequestBody{
		WindowId:       reservation.WindowId,
		ActualQuantity: actualQuantityWire,
	}
	if usageSummary != nil {
		body.UsageSummary = &usageSummary
	}
	resp, err := c.inner.SettleWindowWithResponse(ctx, body, reqEditors...)
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

func problemType(problem *ErrorModel) string {
	if problem == nil || problem.Type == nil {
		return ""
	}
	return *problem.Type
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

func statusCode(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}
