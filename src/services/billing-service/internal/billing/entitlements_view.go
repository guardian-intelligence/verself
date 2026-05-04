package billing

import (
	"context"
	"fmt"
	"sort"
	"time"
)

type EntitlementsView struct {
	OrgID     OrgID
	Universal EntitlementSlot
	Products  []EntitlementProductSection
}

type EntitlementProductSection struct {
	ProductID   string
	DisplayName string
	ProductSlot *EntitlementSlot
	Buckets     []EntitlementBucketSection
}

type EntitlementBucketSection struct {
	BucketID    string
	DisplayName string
	BucketSlot  *EntitlementSlot
	SKUSlots    []EntitlementSlot
}

type EntitlementSlot struct {
	ScopeType        string
	ProductID        string
	ProductDisplay   string
	BucketID         string
	BucketDisplay    string
	SKUID            string
	SKUDisplay       string
	CoverageLabel    string
	PeriodStartUnits uint64
	SpentUnits       uint64
	PendingUnits     uint64
	AvailableUnits   uint64
	Sources          []EntitlementSourceTotal
}

type EntitlementSourceTotal struct {
	Source           string
	PlanID           string
	Label            string
	PeriodStartUnits uint64
	AvailableUnits   uint64
	PendingUnits     uint64
	InlineExpiresAt  *time.Time
}

type entitlementCatalog struct {
	Products []entitlementCatalogProduct
}

type entitlementCatalogProduct struct {
	ProductID   string
	DisplayName string
	Buckets     []entitlementCatalogBucket
}

type entitlementCatalogBucket struct {
	BucketID    string
	DisplayName string
	SortOrder   int
	SKUs        []entitlementCatalogSKU
}

type entitlementCatalogSKU struct {
	SKUID       string
	DisplayName string
}

func (c *Client) ListEntitlementsView(ctx context.Context, orgID OrgID) (EntitlementsView, error) {
	catalog, err := c.loadEntitlementCatalog(ctx)
	if err != nil {
		return EntitlementsView{}, err
	}
	for _, product := range catalog.Products {
		if err := c.EnsureCurrentEntitlements(ctx, orgID, product.ProductID); err != nil {
			return EntitlementsView{}, err
		}
	}
	grantsByID := map[string]GrantBalance{}
	nowByProduct := map[string]time.Time{}
	for _, product := range catalog.Products {
		now, err := c.BusinessNow(ctx, c.queries, orgID, product.ProductID)
		if err != nil {
			return EntitlementsView{}, err
		}
		nowByProduct[product.ProductID] = now
		productGrants, err := c.ListGrantBalances(ctx, orgID, product.ProductID)
		if err != nil {
			return EntitlementsView{}, err
		}
		for _, grant := range productGrants {
			grantsByID[grant.GrantID] = grant
		}
	}
	grants := make([]GrantBalance, 0, len(grantsByID))
	for _, grant := range grantsByID {
		grants = append(grants, grant)
	}
	sort.SliceStable(grants, func(i, j int) bool {
		return grants[i].StartsAt.Before(grants[j].StartsAt) || (grants[i].StartsAt.Equal(grants[j].StartsAt) && grants[i].GrantID < grants[j].GrantID)
	})
	defaultNow, err := c.BusinessNow(ctx, c.queries, orgID, "")
	if err != nil {
		return EntitlementsView{}, err
	}
	return buildEntitlementsView(orgID, defaultNow, nowByProduct, catalog, grants), nil
}

func (c *Client) loadEntitlementCatalog(ctx context.Context) (entitlementCatalog, error) {
	rows, err := c.queries.ListEntitlementCatalogRows(ctx)
	if err != nil {
		return entitlementCatalog{}, fmt.Errorf("query entitlement catalog: %w", err)
	}
	products := []entitlementCatalogProduct{}
	productIdx := map[string]int{}
	bucketIdx := map[string]map[string]int{}
	for _, row := range rows {
		pi, ok := productIdx[row.ProductID]
		if !ok {
			products = append(products, entitlementCatalogProduct{ProductID: row.ProductID, DisplayName: row.ProductDisplayName})
			pi = len(products) - 1
			productIdx[row.ProductID] = pi
			bucketIdx[row.ProductID] = map[string]int{}
		}
		if !row.BucketID.Valid || !row.SkuID.Valid {
			continue
		}
		bi, ok := bucketIdx[row.ProductID][row.BucketID.String]
		if !ok {
			order := 0
			if row.SortOrder.Valid {
				order = int(row.SortOrder.Int32)
			}
			products[pi].Buckets = append(products[pi].Buckets, entitlementCatalogBucket{BucketID: row.BucketID.String, DisplayName: row.BucketDisplayName.String, SortOrder: order})
			bi = len(products[pi].Buckets) - 1
			bucketIdx[row.ProductID][row.BucketID.String] = bi
		}
		products[pi].Buckets[bi].SKUs = append(products[pi].Buckets[bi].SKUs, entitlementCatalogSKU{SKUID: row.SkuID.String, DisplayName: row.SkuDisplayName.String})
	}
	return entitlementCatalog{Products: products}, nil
}

func buildEntitlementsView(orgID OrgID, defaultNow time.Time, nowByProduct map[string]time.Time, catalog entitlementCatalog, grants []GrantBalance) EntitlementsView {
	view := EntitlementsView{OrgID: orgID, Universal: EntitlementSlot{ScopeType: "account", CoverageLabel: "All products"}}
	productIndex := map[string]int{}
	bucketIndex := map[string]map[string]int{}
	for _, product := range catalog.Products {
		section := EntitlementProductSection{ProductID: product.ProductID, DisplayName: product.DisplayName, Buckets: make([]EntitlementBucketSection, 0, len(product.Buckets))}
		for _, bucket := range product.Buckets {
			bucketSection := EntitlementBucketSection{BucketID: bucket.BucketID, DisplayName: bucket.DisplayName, SKUSlots: make([]EntitlementSlot, 0, len(bucket.SKUs))}
			for _, sku := range bucket.SKUs {
				bucketSection.SKUSlots = append(bucketSection.SKUSlots, EntitlementSlot{ScopeType: "sku", ProductID: product.ProductID, ProductDisplay: product.DisplayName, BucketID: bucket.BucketID, BucketDisplay: bucket.DisplayName, SKUID: sku.SKUID, SKUDisplay: sku.DisplayName, CoverageLabel: sku.DisplayName})
			}
			section.Buckets = append(section.Buckets, bucketSection)
		}
		bucketIndex[product.ProductID] = map[string]int{}
		for i, bucket := range section.Buckets {
			bucketIndex[product.ProductID][bucket.BucketID] = i
		}
		productIndex[product.ProductID] = len(view.Products)
		view.Products = append(view.Products, section)
	}
	for _, grant := range grants {
		now := grantBusinessNow(defaultNow, nowByProduct, grant)
		periodStart := uint64(0)
		if grant.PeriodStart != nil && grant.PeriodEnd != nil && !now.Before(*grant.PeriodStart) && now.Before(*grant.PeriodEnd) {
			periodStart = grant.OriginalAmount
		}
		add := func(slot *EntitlementSlot) {
			if slot == nil {
				return
			}
			slot.PeriodStartUnits += periodStart
			slot.SpentUnits += grant.Spent
			slot.PendingUnits += grant.Pending
			slot.AvailableUnits += grant.Available
			addSourceTotal(slot, grant, periodStart)
		}
		switch grant.ScopeType {
		case "account":
			add(&view.Universal)
		case "product":
			pi, ok := productIndex[grant.ScopeProductID]
			if !ok {
				continue
			}
			if view.Products[pi].ProductSlot == nil {
				view.Products[pi].ProductSlot = &EntitlementSlot{ScopeType: "product", ProductID: grant.ScopeProductID, ProductDisplay: view.Products[pi].DisplayName, CoverageLabel: view.Products[pi].DisplayName}
			}
			add(view.Products[pi].ProductSlot)
		case "bucket":
			pi, ok := productIndex[grant.ScopeProductID]
			if !ok {
				continue
			}
			bi, ok := bucketIndex[grant.ScopeProductID][grant.ScopeBucketID]
			if !ok {
				continue
			}
			bucket := &view.Products[pi].Buckets[bi]
			if bucket.BucketSlot == nil {
				bucket.BucketSlot = &EntitlementSlot{ScopeType: "bucket", ProductID: grant.ScopeProductID, ProductDisplay: view.Products[pi].DisplayName, BucketID: grant.ScopeBucketID, BucketDisplay: bucket.DisplayName, CoverageLabel: bucket.DisplayName}
			}
			add(bucket.BucketSlot)
		case "sku":
			pi, ok := productIndex[grant.ScopeProductID]
			if !ok {
				continue
			}
			bi, ok := bucketIndex[grant.ScopeProductID][grant.ScopeBucketID]
			if !ok {
				continue
			}
			for i := range view.Products[pi].Buckets[bi].SKUSlots {
				if view.Products[pi].Buckets[bi].SKUSlots[i].SKUID == grant.ScopeSKUID {
					add(&view.Products[pi].Buckets[bi].SKUSlots[i])
					break
				}
			}
		}
	}
	sortEntitlementSources(&view.Universal)
	for pi := range view.Products {
		sortEntitlementSources(view.Products[pi].ProductSlot)
		for bi := range view.Products[pi].Buckets {
			sortEntitlementSources(view.Products[pi].Buckets[bi].BucketSlot)
			for si := range view.Products[pi].Buckets[bi].SKUSlots {
				sortEntitlementSources(&view.Products[pi].Buckets[bi].SKUSlots[si])
			}
		}
	}
	return view
}

func grantBusinessNow(defaultNow time.Time, nowByProduct map[string]time.Time, grant GrantBalance) time.Time {
	if grant.ScopeProductID != "" {
		if now, ok := nowByProduct[grant.ScopeProductID]; ok {
			return now
		}
	}
	return defaultNow
}

func addSourceTotal(slot *EntitlementSlot, grant GrantBalance, periodStart uint64) {
	label := entitlementSourceLabel(grant.Source, grant.PlanDisplayName)
	for i := range slot.Sources {
		if slot.Sources[i].Source == grant.Source && slot.Sources[i].PlanID == grant.PlanID {
			slot.Sources[i].PeriodStartUnits += periodStart
			slot.Sources[i].AvailableUnits += grant.Available
			slot.Sources[i].PendingUnits += grant.Pending
			if periodStart == 0 && grant.ExpiresAt != nil && (slot.Sources[i].InlineExpiresAt == nil || grant.ExpiresAt.Before(*slot.Sources[i].InlineExpiresAt)) {
				v := *grant.ExpiresAt
				slot.Sources[i].InlineExpiresAt = &v
			}
			return
		}
	}
	var inline *time.Time
	if periodStart == 0 && grant.ExpiresAt != nil {
		v := *grant.ExpiresAt
		inline = &v
	}
	slot.Sources = append(slot.Sources, EntitlementSourceTotal{Source: grant.Source, PlanID: grant.PlanID, Label: label, PeriodStartUnits: periodStart, AvailableUnits: grant.Available, PendingUnits: grant.Pending, InlineExpiresAt: inline})
}

func sortEntitlementSources(slot *EntitlementSlot) {
	if slot == nil {
		return
	}
	sort.SliceStable(slot.Sources, func(i, j int) bool {
		if sourcePriority(slot.Sources[i].Source) != sourcePriority(slot.Sources[j].Source) {
			return sourcePriority(slot.Sources[i].Source) < sourcePriority(slot.Sources[j].Source)
		}
		return slot.Sources[i].PlanID < slot.Sources[j].PlanID
	})
}

func entitlementSourceLabel(source, planDisplay string) string {
	if source == "contract" && planDisplay != "" {
		return planDisplay
	}
	switch source {
	case "free_tier":
		return "Free tier"
	case "contract":
		return "Subscription"
	case "purchase":
		return "Purchased credits"
	case "promo":
		return "Promo credits"
	case "refund":
		return "Refund credits"
	default:
		return source
	}
}
