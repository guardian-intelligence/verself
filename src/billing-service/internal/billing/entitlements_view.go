package billing

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// EntitlementsView is the slot-keyed customer view of an org's open credit.
// The catalog is the spine: account → product → bucket → sku, top to bottom.
// The funder consumes the inverse order (sku → bucket → product → account)
// because most-specific scope drains first. Both orderings are correct; do not
// "fix" the display by aligning it to the funder. Rows answer "what coverage
// do I have," not "what drains first."
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

// EntitlementSlot is one row in the customer's entitlements table. The four
// scalar columns are the customer-visible aggregates; Sources is the cell
// breakdown rendered inside the Period-started-with and Available cells.
type EntitlementSlot struct {
	ScopeType        GrantScopeType
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

// EntitlementSourceTotal aggregates one (source, plan_id) inside a slot.
// Multiple contract plans contributing to the same slot fan out to one
// SourceTotal per plan; non-contract sources collapse to one entry per
// source. TopGrantID is the funder's next-to-drain grant for this cell — it
// is the contract surface the funding-equivalence test pins, but the apiwire
// mapping does not surface it because customers never need to read a raw
// grant id.
type EntitlementSourceTotal struct {
	Source           GrantSourceType
	PlanID           string
	Label            string
	PeriodStartUnits uint64
	AvailableUnits   uint64
	PendingUnits     uint64
	InlineExpiresAt  *time.Time
	TopGrantID       string
}

// ListEntitlementsView returns the org's entitlements rendered against the
// full active catalog. The catalog is queried independently of the grant set
// so empty SKU rows still surface — the customer can see "this product has
// 5 SKUs and you have credit for 2 of them" without consulting a pricing
// page, and adding a new SKU appears in the table with no code change.
func (c *Client) ListEntitlementsView(ctx context.Context, orgID OrgID) (EntitlementsView, error) {
	if err := ctx.Err(); err != nil {
		return EntitlementsView{}, err
	}

	catalog, err := c.loadEntitlementCatalog(ctx)
	if err != nil {
		return EntitlementsView{}, err
	}

	grants, err := c.listGrantBalances(ctx, orgID, "")
	if err != nil {
		return EntitlementsView{}, fmt.Errorf("list grants: %w", err)
	}

	now := c.clock().UTC()
	return buildEntitlementsView(orgID, now, catalog, grants), nil
}

// entitlementCatalog is the in-memory shape consumed by buildEntitlementsView.
// loadEntitlementCatalog hand-assembles it from three queries — products, the
// active SKUs joined to credit_buckets, and the per-bucket sort order — so
// the spine that drives row enumeration is explicit and not derivable from
// the grant set.
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

func (c *Client) loadEntitlementCatalog(ctx context.Context) (entitlementCatalog, error) {
	productRows, err := c.pg.QueryContext(ctx, `
		SELECT product_id, display_name
		FROM products
		ORDER BY display_name ASC, product_id ASC
	`)
	if err != nil {
		return entitlementCatalog{}, fmt.Errorf("query products: %w", err)
	}
	type productRec struct {
		productID, displayName string
	}
	var productRecs []productRec
	for productRows.Next() {
		var rec productRec
		if err := productRows.Scan(&rec.productID, &rec.displayName); err != nil {
			productRows.Close()
			return entitlementCatalog{}, fmt.Errorf("scan product: %w", err)
		}
		productRecs = append(productRecs, rec)
	}
	if err := productRows.Err(); err != nil {
		productRows.Close()
		return entitlementCatalog{}, fmt.Errorf("iterate products: %w", err)
	}
	productRows.Close()

	skuRows, err := c.pg.QueryContext(ctx, `
		SELECT s.product_id, s.bucket_id, s.sku_id, s.display_name,
		       b.display_name AS bucket_display, b.sort_order
		FROM skus s
		JOIN credit_buckets b ON b.bucket_id = s.bucket_id
		WHERE s.active = true
		ORDER BY s.product_id ASC, b.sort_order ASC, b.bucket_id ASC, s.display_name ASC, s.sku_id ASC
	`)
	if err != nil {
		return entitlementCatalog{}, fmt.Errorf("query catalog skus: %w", err)
	}
	defer skuRows.Close()

	products := make([]entitlementCatalogProduct, len(productRecs))
	productLookup := map[string]int{}
	for i, rec := range productRecs {
		products[i] = entitlementCatalogProduct{
			ProductID:   rec.productID,
			DisplayName: rec.displayName,
		}
		productLookup[rec.productID] = i
	}

	type bucketKey struct{ productID, bucketID string }
	bucketLookup := map[bucketKey]int{}

	for skuRows.Next() {
		var productID, bucketID, skuID, skuDisplay, bucketDisplay string
		var sortOrder int
		if err := skuRows.Scan(&productID, &bucketID, &skuID, &skuDisplay, &bucketDisplay, &sortOrder); err != nil {
			return entitlementCatalog{}, fmt.Errorf("scan catalog sku: %w", err)
		}
		pIdx, ok := productLookup[productID]
		if !ok {
			// FK guarantees this is unreachable in practice; if it ever
			// fires, surface the SKU under a stale product so the credit
			// stays visible rather than vanishing.
			products = append(products, entitlementCatalogProduct{ProductID: productID, DisplayName: productID})
			pIdx = len(products) - 1
			productLookup[productID] = pIdx
		}
		bk := bucketKey{productID: productID, bucketID: bucketID}
		bIdx, ok := bucketLookup[bk]
		if !ok {
			products[pIdx].Buckets = append(products[pIdx].Buckets, entitlementCatalogBucket{
				BucketID:    bucketID,
				DisplayName: bucketDisplay,
				SortOrder:   sortOrder,
			})
			bIdx = len(products[pIdx].Buckets) - 1
			bucketLookup[bk] = bIdx
		}
		products[pIdx].Buckets[bIdx].SKUs = append(products[pIdx].Buckets[bIdx].SKUs, entitlementCatalogSKU{
			SKUID:       skuID,
			DisplayName: skuDisplay,
		})
	}
	if err := skuRows.Err(); err != nil {
		return entitlementCatalog{}, fmt.Errorf("iterate catalog skus: %w", err)
	}

	return entitlementCatalog{Products: products}, nil
}

// buildEntitlementsView is the pure slot-tree builder that the request handler
// and the funding-equivalence test both call. It is the only function that
// maps grants to display rows; all aggregation lives here. The display order
// is account → product → bucket → sku; the funder consumes the inverse order.
// Both are deliberate.
func buildEntitlementsView(orgID OrgID, now time.Time, catalog entitlementCatalog, grants []GrantBalance) EntitlementsView {
	view := EntitlementsView{
		OrgID:     orgID,
		Universal: newUniversalSlot(),
	}

	productSections := make([]EntitlementProductSection, 0, len(catalog.Products))
	productLookup := map[string]int{}
	for _, product := range catalog.Products {
		section := EntitlementProductSection{
			ProductID:   product.ProductID,
			DisplayName: product.DisplayName,
			Buckets:     make([]EntitlementBucketSection, 0, len(product.Buckets)),
		}
		for _, bucket := range product.Buckets {
			bucketSec := EntitlementBucketSection{
				BucketID:    bucket.BucketID,
				DisplayName: bucket.DisplayName,
				SKUSlots:    make([]EntitlementSlot, 0, len(bucket.SKUs)),
			}
			for _, sku := range bucket.SKUs {
				bucketSec.SKUSlots = append(bucketSec.SKUSlots, EntitlementSlot{
					ScopeType:      GrantScopeSKU,
					ProductID:      product.ProductID,
					ProductDisplay: product.DisplayName,
					BucketID:       bucket.BucketID,
					BucketDisplay:  bucket.DisplayName,
					SKUID:          sku.SKUID,
					SKUDisplay:     sku.DisplayName,
					CoverageLabel:  sku.DisplayName,
				})
			}
			section.Buckets = append(section.Buckets, bucketSec)
		}
		productLookup[product.ProductID] = len(productSections)
		productSections = append(productSections, section)
	}

	type slotKey struct {
		scope     GrantScopeType
		productID string
		bucketID  string
		skuID     string
	}
	type sourceKey struct {
		source GrantSourceType
		planID string
	}
	type sourceAcc struct {
		key             sourceKey
		label           string
		periodStart     uint64
		available       uint64
		pending         uint64
		inlineExpiresAt *time.Time
		topGrantID      string
	}
	type slotAcc struct {
		periodStart uint64
		available   uint64
		spent       uint64
		pending     uint64
		sourceOrder []sourceKey
		sources     map[sourceKey]*sourceAcc
	}
	slotAccs := map[slotKey]*slotAcc{}

	getSlotAcc := func(key slotKey) *slotAcc {
		acc, ok := slotAccs[key]
		if !ok {
			acc = &slotAcc{sources: map[sourceKey]*sourceAcc{}}
			slotAccs[key] = acc
		}
		return acc
	}

	pushGrant := func(key slotKey, grant GrantBalance) {
		acc := getSlotAcc(key)
		acc.available += grant.Available
		acc.spent += grant.Spent
		acc.pending += grant.Pending
		periodCovered := grant.Period != nil && grant.Period.Contains(now)
		if periodCovered {
			acc.periodStart += grant.OriginalAmount
		}
		sk := sourceKey{source: grant.Source, planID: grant.PlanID}
		src, ok := acc.sources[sk]
		if !ok {
			src = &sourceAcc{
				key:        sk,
				label:      GrantSourceLabel(grant.Source, grant.PlanDisplayName),
				topGrantID: grant.GrantID.String(),
			}
			acc.sources[sk] = src
			acc.sourceOrder = append(acc.sourceOrder, sk)
		}
		src.available += grant.Available
		src.pending += grant.Pending
		if periodCovered {
			src.periodStart += grant.OriginalAmount
		}
		// Inline expiry surfaces only for non-period grants. Period-bound
		// expiries are implicit in the period boundary, which is shared by
		// every period grant in the same row, so surfacing them inline would
		// be visual noise. The earliest non-period expiry in the cell is the
		// one customers care about ("when does my next thing disappear").
		if !periodCovered && grant.ExpiresAt != nil {
			if src.inlineExpiresAt == nil || grant.ExpiresAt.Before(*src.inlineExpiresAt) {
				expires := *grant.ExpiresAt
				src.inlineExpiresAt = &expires
			}
		}
	}

	bucketIndexOf := func(productIdx int, bucketID string) int {
		for i, b := range productSections[productIdx].Buckets {
			if b.BucketID == bucketID {
				return i
			}
		}
		return -1
	}
	skuIndexOf := func(productIdx, bucketIdx int, skuID string) int {
		for i, slot := range productSections[productIdx].Buckets[bucketIdx].SKUSlots {
			if slot.SKUID == skuID {
				return i
			}
		}
		return -1
	}
	ensureProduct := func(productID string) int {
		if idx, ok := productLookup[productID]; ok {
			return idx
		}
		// Off-catalog fallback: a grant references a product that's not on
		// the active spine (deactivated, deleted upstream of FK enforcement).
		// Surface it at the end so credit never silently disappears.
		productSections = append(productSections, EntitlementProductSection{
			ProductID:   productID,
			DisplayName: productID,
		})
		idx := len(productSections) - 1
		productLookup[productID] = idx
		return idx
	}
	ensureBucket := func(productIdx int, bucketID string) int {
		if idx := bucketIndexOf(productIdx, bucketID); idx != -1 {
			return idx
		}
		productSections[productIdx].Buckets = append(productSections[productIdx].Buckets, EntitlementBucketSection{
			BucketID:    bucketID,
			DisplayName: bucketID,
		})
		return len(productSections[productIdx].Buckets) - 1
	}
	ensureSKU := func(productIdx, bucketIdx int, skuID string) int {
		if idx := skuIndexOf(productIdx, bucketIdx, skuID); idx != -1 {
			return idx
		}
		section := productSections[productIdx]
		bucket := section.Buckets[bucketIdx]
		productSections[productIdx].Buckets[bucketIdx].SKUSlots = append(productSections[productIdx].Buckets[bucketIdx].SKUSlots, EntitlementSlot{
			ScopeType:      GrantScopeSKU,
			ProductID:      section.ProductID,
			ProductDisplay: section.DisplayName,
			BucketID:       bucket.BucketID,
			BucketDisplay:  bucket.DisplayName,
			SKUID:          skuID,
			SKUDisplay:     skuID,
			CoverageLabel:  skuID,
		})
		return len(productSections[productIdx].Buckets[bucketIdx].SKUSlots) - 1
	}

	for _, grant := range grants {
		switch grant.ScopeType {
		case GrantScopeAccount:
			pushGrant(slotKey{scope: GrantScopeAccount}, grant)
		case GrantScopeProduct:
			ensureProduct(grant.ScopeProductID)
			pushGrant(slotKey{scope: GrantScopeProduct, productID: grant.ScopeProductID}, grant)
		case GrantScopeBucket:
			pIdx := ensureProduct(grant.ScopeProductID)
			ensureBucket(pIdx, grant.ScopeBucketID)
			pushGrant(slotKey{scope: GrantScopeBucket, productID: grant.ScopeProductID, bucketID: grant.ScopeBucketID}, grant)
		case GrantScopeSKU:
			pIdx := ensureProduct(grant.ScopeProductID)
			bIdx := ensureBucket(pIdx, grant.ScopeBucketID)
			ensureSKU(pIdx, bIdx, grant.ScopeSKUID)
			pushGrant(slotKey{scope: GrantScopeSKU, productID: grant.ScopeProductID, bucketID: grant.ScopeBucketID, skuID: grant.ScopeSKUID}, grant)
		}
	}

	finalize := func(target *EntitlementSlot, key slotKey) {
		acc, ok := slotAccs[key]
		if !ok {
			return
		}
		target.PeriodStartUnits = acc.periodStart
		target.SpentUnits = acc.spent
		target.PendingUnits = acc.pending
		target.AvailableUnits = acc.available
		sources := make([]EntitlementSourceTotal, 0, len(acc.sourceOrder))
		for _, sk := range acc.sourceOrder {
			src := acc.sources[sk]
			sources = append(sources, EntitlementSourceTotal{
				Source:           src.key.source,
				PlanID:           src.key.planID,
				Label:            src.label,
				PeriodStartUnits: src.periodStart,
				AvailableUnits:   src.available,
				PendingUnits:     src.pending,
				InlineExpiresAt:  src.inlineExpiresAt,
				TopGrantID:       src.topGrantID,
			})
		}
		sortSourceTotals(sources)
		target.Sources = sources
	}

	finalize(&view.Universal, slotKey{scope: GrantScopeAccount})

	for pIdx := range productSections {
		section := &productSections[pIdx]
		productKey := slotKey{scope: GrantScopeProduct, productID: section.ProductID}
		if _, ok := slotAccs[productKey]; ok {
			slot := newProductSlot(*section)
			finalize(&slot, productKey)
			section.ProductSlot = &slot
		}
		for bIdx := range section.Buckets {
			bucket := &section.Buckets[bIdx]
			bk := slotKey{scope: GrantScopeBucket, productID: section.ProductID, bucketID: bucket.BucketID}
			if _, ok := slotAccs[bk]; ok {
				slot := newBucketSlot(*section, *bucket)
				finalize(&slot, bk)
				bucket.BucketSlot = &slot
			}
			for sIdx := range bucket.SKUSlots {
				sku := &bucket.SKUSlots[sIdx]
				skuKey := slotKey{scope: GrantScopeSKU, productID: section.ProductID, bucketID: bucket.BucketID, skuID: sku.SKUID}
				finalize(sku, skuKey)
			}
		}
	}

	view.Products = productSections
	return view
}

func newUniversalSlot() EntitlementSlot {
	return EntitlementSlot{
		ScopeType:     GrantScopeAccount,
		CoverageLabel: "Usable anywhere",
	}
}

func newProductSlot(section EntitlementProductSection) EntitlementSlot {
	display := section.DisplayName
	if display == "" {
		display = section.ProductID
	}
	return EntitlementSlot{
		ScopeType:      GrantScopeProduct,
		ProductID:      section.ProductID,
		ProductDisplay: section.DisplayName,
		CoverageLabel:  fmt.Sprintf("Any bucket in %s", display),
	}
}

func newBucketSlot(section EntitlementProductSection, bucket EntitlementBucketSection) EntitlementSlot {
	display := bucket.DisplayName
	if display == "" {
		display = bucket.BucketID
	}
	return EntitlementSlot{
		ScopeType:      GrantScopeBucket,
		ProductID:      section.ProductID,
		ProductDisplay: section.DisplayName,
		BucketID:       bucket.BucketID,
		BucketDisplay:  bucket.DisplayName,
		CoverageLabel:  fmt.Sprintf("Any %s SKU", display),
	}
}

func sortSourceTotals(totals []EntitlementSourceTotal) {
	rank := grantSourceRank()
	sort.SliceStable(totals, func(i, j int) bool {
		ri, rj := rank[totals[i].Source], rank[totals[j].Source]
		if ri != rj {
			return ri < rj
		}
		return totals[i].PlanID < totals[j].PlanID
	})
}

func grantSourceRank() map[GrantSourceType]int {
	rank := make(map[GrantSourceType]int, len(GrantSourceFundingOrder))
	for i, source := range GrantSourceFundingOrder {
		rank[source] = i
	}
	return rank
}
