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
	ErrPaymentRequired      = errors.New("billing-client: payment required")
	ErrForbidden            = errors.New("billing-client: forbidden")
	ErrNoStripeCustomer     = errors.New("billing-client: no stripe customer")
	ErrSubscriptionNotFound = errors.New("billing-client: subscription not found")
	ErrUnexpected           = errors.New("billing-client: unexpected response")
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

func (c *ServiceClient) ListSubscriptions(ctx context.Context, orgID uint64, reqEditors ...RequestEditorFn) (apiwire.BillingSubscriptions, error) {
	orgIDWire := apiwire.Uint64(orgID).String()
	resp, err := c.inner.ListSubscriptionsWithResponse(ctx, orgIDWire, reqEditors...)
	if err != nil {
		return apiwire.BillingSubscriptions{}, err
	}
	if resp.JSON200 != nil {
		return parseBillingSubscriptions(*resp.JSON200)
	}
	return apiwire.BillingSubscriptions{}, unexpected("list subscriptions", resp.HTTPResponse, resp.ApplicationproblemJSONDefault)
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

func (c *ServiceClient) CreateSubscription(ctx context.Context, orgID uint64, planID string, cadence string, successURL string, cancelURL string, reqEditors ...RequestEditorFn) (string, error) {
	orgIDWire := apiwire.Uint64(orgID).String()
	body := CreateSubscriptionJSONRequestBody{
		OrgId:      orgIDWire,
		PlanId:     planID,
		SuccessUrl: successURL,
		CancelUrl:  cancelURL,
	}
	if cadence != "" {
		wireCadence := BillingCreateSubscriptionRequestCadence(cadence)
		body.Cadence = &wireCadence
	}
	resp, err := c.inner.CreateSubscriptionWithResponse(ctx, body, reqEditors...)
	if err != nil {
		return "", err
	}
	if resp.JSON200 != nil {
		return resp.JSON200.Url, nil
	}
	return "", unexpected("create subscription", resp.HTTPResponse, firstProblem(resp.ApplicationproblemJSON500, resp.ApplicationproblemJSON422))
}

func (c *ServiceClient) CancelSubscription(ctx context.Context, orgID uint64, subscriptionID int64, reqEditors ...RequestEditorFn) (apiwire.BillingCancelSubscriptionResponse, error) {
	orgIDWire := apiwire.Uint64(orgID).String()
	subscriptionIDWire := apiwire.Int64(subscriptionID).String()
	resp, err := c.inner.CancelSubscriptionWithResponse(ctx, subscriptionIDWire, CancelSubscriptionJSONRequestBody{
		OrgId: orgIDWire,
	}, reqEditors...)
	if err != nil {
		return apiwire.BillingCancelSubscriptionResponse{}, err
	}
	if resp.JSON200 != nil {
		subscription, err := parseBillingSubscription(resp.JSON200.Subscription)
		if err != nil {
			return apiwire.BillingCancelSubscriptionResponse{}, err
		}
		return apiwire.BillingCancelSubscriptionResponse{Subscription: subscription}, nil
	}
	if statusCode(resp.HTTPResponse) == http.StatusNotFound {
		return apiwire.BillingCancelSubscriptionResponse{}, fmt.Errorf("%w: %s", ErrSubscriptionNotFound, detail(resp.ApplicationproblemJSON404, resp.HTTPResponse))
	}
	return apiwire.BillingCancelSubscriptionResponse{}, unexpected("cancel subscription", resp.HTTPResponse, firstProblem(resp.ApplicationproblemJSON500, resp.ApplicationproblemJSON404, resp.ApplicationproblemJSON422))
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

func (c *ServiceClient) ApplySubscriptionProviderEvent(ctx context.Context, request apiwire.BillingApplySubscriptionProviderEventRequest, reqEditors ...RequestEditorFn) error {
	body := ApplySubscriptionProviderEventJSONRequestBody{
		EventType:              request.EventType,
		OrgId:                  request.OrgID.String(),
		PlanId:                 request.PlanID,
		ProductId:              request.ProductID,
		Provider:               BillingApplySubscriptionProviderEventRequestProvider(request.Provider),
		ProviderSubscriptionId: request.ProviderSubscriptionID,
		CurrentPeriodStart:     request.CurrentPeriodStart,
		CurrentPeriodEnd:       request.CurrentPeriodEnd,
	}
	if request.Cadence != "" {
		value := BillingApplySubscriptionProviderEventRequestCadence(request.Cadence)
		body.Cadence = &value
	}
	if request.Status != "" {
		body.Status = &request.Status
	}
	if request.ProviderCheckoutSessionID != "" {
		body.ProviderCheckoutSessionId = &request.ProviderCheckoutSessionID
	}
	if request.ProviderCustomerID != "" {
		body.ProviderCustomerId = &request.ProviderCustomerID
	}
	if request.PaymentState != "" {
		value := BillingApplySubscriptionProviderEventRequestPaymentState(request.PaymentState)
		body.PaymentState = &value
	}
	if request.EntitlementState != "" {
		value := BillingApplySubscriptionProviderEventRequestEntitlementState(request.EntitlementState)
		body.EntitlementState = &value
	}
	resp, err := c.inner.ApplySubscriptionProviderEventWithResponse(ctx, body, reqEditors...)
	if err != nil {
		return err
	}
	if resp.JSON200 != nil && resp.JSON200.Applied {
		return nil
	}
	return unexpected("apply subscription provider event", resp.HTTPResponse, firstProblem(resp.ApplicationproblemJSON500, resp.ApplicationproblemJSON400))
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
	allocation map[string]float64,
	reqEditors ...RequestEditorFn,
) (Reservation, error) {
	orgIDWire := apiwire.Uint64(orgID).String()
	concurrentCountWire, err := uint64ToInt64(concurrentCount, "concurrent_count")
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
	var universalIn []BillingEntitlementPool
	if in.Universal != nil {
		universalIn = *in.Universal
	}
	universal, err := parseEntitlementPools(universalIn, "universal")
	if err != nil {
		return apiwire.BillingEntitlementsView{}, err
	}
	var productsIn []BillingEntitlementProductSection
	if in.Products != nil {
		productsIn = *in.Products
	}
	products := make([]apiwire.BillingEntitlementProductSection, 0, len(productsIn))
	for i, section := range productsIn {
		var productPoolsIn []BillingEntitlementPool
		if section.ProductPools != nil {
			productPoolsIn = *section.ProductPools
		}
		productPools, err := parseEntitlementPools(productPoolsIn, fmt.Sprintf("products[%d].product_pools", i))
		if err != nil {
			return apiwire.BillingEntitlementsView{}, err
		}
		var bucketsIn []BillingEntitlementBucketSection
		if section.Buckets != nil {
			bucketsIn = *section.Buckets
		}
		buckets := make([]apiwire.BillingEntitlementBucketSection, 0, len(bucketsIn))
		for j, bucket := range bucketsIn {
			var poolsIn []BillingEntitlementPool
			if bucket.Pools != nil {
				poolsIn = *bucket.Pools
			}
			pools, err := parseEntitlementPools(poolsIn, fmt.Sprintf("products[%d].buckets[%d].pools", i, j))
			if err != nil {
				return apiwire.BillingEntitlementsView{}, err
			}
			buckets = append(buckets, apiwire.BillingEntitlementBucketSection{
				BucketID:    bucket.BucketId,
				DisplayName: bucket.DisplayName,
				Pools:       pools,
			})
		}
		products = append(products, apiwire.BillingEntitlementProductSection{
			ProductID:    section.ProductId,
			DisplayName:  section.DisplayName,
			ProductPools: productPools,
			Buckets:      buckets,
		})
	}
	return apiwire.BillingEntitlementsView{
		OrgID:     orgID,
		Universal: universal,
		Products:  products,
	}, nil
}

func parseEntitlementPools(in []BillingEntitlementPool, label string) ([]apiwire.BillingEntitlementPool, error) {
	out := make([]apiwire.BillingEntitlementPool, 0, len(in))
	for i, pool := range in {
		var entriesIn []BillingEntitlementGrantEntry
		if pool.Entries != nil {
			entriesIn = *pool.Entries
		}
		entries := make([]apiwire.BillingEntitlementGrantEntry, 0, len(entriesIn))
		for k, entry := range entriesIn {
			available, err := parseDecimalUint64(entry.Available, fmt.Sprintf("%s[%d].entries[%d].available", label, i, k))
			if err != nil {
				return nil, err
			}
			pending, err := parseDecimalUint64(entry.Pending, fmt.Sprintf("%s[%d].entries[%d].pending", label, i, k))
			if err != nil {
				return nil, err
			}
			entries = append(entries, apiwire.BillingEntitlementGrantEntry{
				GrantID:     entry.GrantId,
				Available:   available,
				Pending:     pending,
				StartsAt:    entry.StartsAt,
				PeriodStart: entry.PeriodStart,
				PeriodEnd:   entry.PeriodEnd,
				ExpiresAt:   entry.ExpiresAt,
			})
		}
		out = append(out, apiwire.BillingEntitlementPool{
			ScopeType:      string(pool.ScopeType),
			ProductID:      pool.ProductId,
			ProductDisplay: pool.ProductDisplay,
			BucketID:       pool.BucketId,
			BucketDisplay:  pool.BucketDisplay,
			SKUID:          pool.SkuId,
			SKUDisplay:     pool.SkuDisplay,
			CoverageLabel:  pool.CoverageLabel,
			Source:         string(pool.Source),
			SourceLabel:    pool.SourceLabel,
			Entries:        entries,
		})
	}
	return out, nil
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
			unitRate, err := parseDecimalUint64(line.UnitRate, "line_items.unit_rate")
			if err != nil {
				return apiwire.BillingStatement{}, err
			}
			chargeUnits, err := parseDecimalUint64(line.ChargeUnits, "line_items.charge_units")
			if err != nil {
				return apiwire.BillingStatement{}, err
			}
			lineItems = append(lineItems, apiwire.BillingStatementLineItem{
				ProductID:         line.ProductId,
				PlanID:            line.PlanId,
				BucketID:          line.BucketId,
				BucketDisplayName: line.BucketDisplayName,
				SKUID:             line.SkuId,
				SKUDisplayName:    line.SkuDisplayName,
				QuantityUnit:      line.QuantityUnit,
				PricingPhase:      line.PricingPhase,
				Quantity:          line.Quantity,
				UnitRate:          unitRate,
				ChargeUnits:       chargeUnits,
			})
		}
	}

	bucketSummaries := make([]apiwire.BillingStatementBucketSummary, 0)
	if in.BucketSummaries != nil {
		bucketSummaries = make([]apiwire.BillingStatementBucketSummary, 0, len(*in.BucketSummaries))
		for _, bucket := range *in.BucketSummaries {
			parsed, err := parseBillingStatementBucketSummary(bucket)
			if err != nil {
				return apiwire.BillingStatement{}, err
			}
			bucketSummaries = append(bucketSummaries, parsed)
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
		OrgID:           orgID,
		ProductID:       in.ProductId,
		PeriodStart:     in.PeriodStart.UTC(),
		PeriodEnd:       in.PeriodEnd.UTC(),
		PeriodSource:    in.PeriodSource,
		GeneratedAt:     in.GeneratedAt.UTC(),
		Currency:        in.Currency,
		UnitLabel:       in.UnitLabel,
		LineItems:       lineItems,
		BucketSummaries: bucketSummaries,
		GrantSummaries:  grantSummaries,
		Totals:          totals,
	}, nil
}

func parseBillingStatementBucketSummary(in BillingStatementBucketSummary) (apiwire.BillingStatementBucketSummary, error) {
	chargeUnits, err := parseDecimalUint64(in.ChargeUnits, "bucket_summaries.charge_units")
	if err != nil {
		return apiwire.BillingStatementBucketSummary{}, err
	}
	freeTierUnits, err := parseDecimalUint64(in.FreeTierUnits, "bucket_summaries.free_tier_units")
	if err != nil {
		return apiwire.BillingStatementBucketSummary{}, err
	}
	subscriptionUnits, err := parseDecimalUint64(in.SubscriptionUnits, "bucket_summaries.subscription_units")
	if err != nil {
		return apiwire.BillingStatementBucketSummary{}, err
	}
	purchaseUnits, err := parseDecimalUint64(in.PurchaseUnits, "bucket_summaries.purchase_units")
	if err != nil {
		return apiwire.BillingStatementBucketSummary{}, err
	}
	promoUnits, err := parseDecimalUint64(in.PromoUnits, "bucket_summaries.promo_units")
	if err != nil {
		return apiwire.BillingStatementBucketSummary{}, err
	}
	refundUnits, err := parseDecimalUint64(in.RefundUnits, "bucket_summaries.refund_units")
	if err != nil {
		return apiwire.BillingStatementBucketSummary{}, err
	}
	receivableUnits, err := parseDecimalUint64(in.ReceivableUnits, "bucket_summaries.receivable_units")
	if err != nil {
		return apiwire.BillingStatementBucketSummary{}, err
	}
	reservedUnits, err := parseDecimalUint64(in.ReservedUnits, "bucket_summaries.reserved_units")
	if err != nil {
		return apiwire.BillingStatementBucketSummary{}, err
	}
	return apiwire.BillingStatementBucketSummary{
		ProductID:         in.ProductId,
		BucketID:          in.BucketId,
		BucketDisplayName: in.BucketDisplayName,
		ChargeUnits:       chargeUnits,
		FreeTierUnits:     freeTierUnits,
		SubscriptionUnits: subscriptionUnits,
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
	subscriptionUnits, err := parseDecimalUint64(in.SubscriptionUnits, "totals.subscription_units")
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
		ChargeUnits:       chargeUnits,
		FreeTierUnits:     freeTierUnits,
		SubscriptionUnits: subscriptionUnits,
		PurchaseUnits:     purchaseUnits,
		PromoUnits:        promoUnits,
		RefundUnits:       refundUnits,
		ReceivableUnits:   receivableUnits,
		ReservedUnits:     reservedUnits,
		TotalDueUnits:     totalDueUnits,
	}, nil
}

func parseBillingSubscriptions(in BillingSubscriptions) (apiwire.BillingSubscriptions, error) {
	if in.Subscriptions == nil {
		return apiwire.BillingSubscriptions{}, nil
	}
	out := make([]apiwire.BillingSubscription, 0, len(*in.Subscriptions))
	for _, subscription := range *in.Subscriptions {
		parsed, err := parseBillingSubscription(subscription)
		if err != nil {
			return apiwire.BillingSubscriptions{}, err
		}
		out = append(out, parsed)
	}
	return apiwire.BillingSubscriptions{Subscriptions: out}, nil
}

func parseBillingSubscription(subscription BillingSubscription) (apiwire.BillingSubscription, error) {
	subscriptionID, err := parseDecimalInt64(subscription.SubscriptionId, "subscription_id")
	if err != nil {
		return apiwire.BillingSubscription{}, err
	}
	return apiwire.BillingSubscription{
		SubscriptionID:     subscriptionID,
		ContractID:         subscription.ContractId,
		ProductID:          subscription.ProductId,
		PlanID:             subscription.PlanId,
		Cadence:            subscription.Cadence,
		Status:             subscription.Status,
		PaymentState:       subscription.PaymentState,
		EntitlementState:   subscription.EntitlementState,
		CurrentPeriodStart: subscription.CurrentPeriodStart,
		CurrentPeriodEnd:   subscription.CurrentPeriodEnd,
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

func parseDecimalInt64(value string, field string) (apiwire.DecimalInt64, error) {
	parsed, err := apiwire.ParseInt64(value)
	if err != nil {
		return apiwire.Int64(0), fmt.Errorf("billing-client: %s: %w", field, err)
	}
	return apiwire.Int64(parsed), nil
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
