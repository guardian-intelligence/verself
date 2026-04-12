package billing

import (
	"fmt"
	"testing"
	"time"
)

// TestEntitlementsViewMatchesFunderPrecedence is the load-bearing contract test
// between the entitlements view-model and the reserve-time funder. The view's
// "next-to-spend" position in any cell is read by customers as a claim about
// what the funder will burn first; if the two ever drift, this test fires.
//
// For each pool surfaced by buildEntitlementsView, we project the pool back
// down to the scopedGrantBalance shape the funder consumes, run planGrantFunding
// against a unit charge that exactly matches the pool's coverage, and assert
// that the funder's first leg is the same grant the view shows on top.
func TestEntitlementsViewMatchesFunderPrecedence(t *testing.T) {
	t.Parallel()

	productID := "sandbox"
	productNames := map[string]string{productID: "Sandbox"}
	bucketCatalog := map[string]bucketCatalogRow{
		"compute":       {DisplayName: "Compute", SortOrder: 10},
		"block_storage": {DisplayName: "Block Storage", SortOrder: 30},
	}
	skuCatalog := map[string]skuCatalogRow{
		"premium_nvme": {DisplayName: "Premium NVMe", BucketID: "block_storage", ProductID: productID},
	}

	// Two grants per (scope, source) so the within-pool ordering claim is
	// non-trivial. Earlier expiry sorts first, matching listGrantBalances.
	grants := []GrantBalance{
		// SKU-scope on (block_storage, premium_nvme)
		viewFundingGrant("sku-free-a", SourceFreeTier, GrantScopeSKU, productID, "block_storage", "premium_nvme", 50, 7),
		viewFundingGrant("sku-free-b", SourceFreeTier, GrantScopeSKU, productID, "block_storage", "premium_nvme", 50, 30),
		viewFundingGrant("sku-sub-a", SourceSubscription, GrantScopeSKU, productID, "block_storage", "premium_nvme", 50, 14),
		// Bucket-scope on (compute, *)
		viewFundingGrant("bkt-free-a", SourceFreeTier, GrantScopeBucket, productID, "compute", "", 50, 7),
		viewFundingGrant("bkt-free-b", SourceFreeTier, GrantScopeBucket, productID, "compute", "", 50, 30),
		viewFundingGrant("bkt-sub-a", SourceSubscription, GrantScopeBucket, productID, "compute", "", 50, 14),
		// Product-scope on sandbox
		viewFundingGrant("prd-free-a", SourceFreeTier, GrantScopeProduct, productID, "", "", 50, 7),
		viewFundingGrant("prd-promo-a", SourcePromo, GrantScopeProduct, productID, "", "", 50, 14),
		viewFundingGrant("prd-promo-b", SourcePromo, GrantScopeProduct, productID, "", "", 50, 30),
		// Account-scope universal
		viewFundingGrant("acc-pur-a", SourcePurchase, GrantScopeAccount, "", "", "", 50, 7),
		viewFundingGrant("acc-promo-a", SourcePromo, GrantScopeAccount, "", "", "", 50, 14),
	}

	view := buildEntitlementsView(42, grants, productNames, bucketCatalog, skuCatalog)

	cells := walkViewCells(view)
	if got, want := len(cells), uniqueScopeSourceCells(grants); got != want {
		t.Fatalf("walkViewCells = %d cells, want %d", got, want)
	}

	for _, cell := range cells {
		t.Run(cell.label, func(t *testing.T) {
			pool := cell.pool
			if len(pool.Entries) == 0 {
				t.Fatalf("%s: pool has no entries", cell.label)
			}
			cellGrants := scopedGrantsFromPool(t, pool)
			charge := chargeForPoolCoverage(pool)
			legs, err := planGrantFunding(productID, []chargeLine{charge}, cellGrants)
			if err != nil {
				t.Fatalf("%s: planGrantFunding: %v", cell.label, err)
			}
			if len(legs) == 0 {
				t.Fatalf("%s: funder produced no legs", cell.label)
			}
			top := pool.Entries[0]
			if got := legs[0].GrantID.String(); got != top.GrantID {
				t.Fatalf("%s: funder picked %s first; view shows %s", cell.label, got, top.GrantID)
			}
			if legs[0].GrantScopeType != pool.ScopeType {
				t.Fatalf("%s: funder leg scope %s != view scope %s", cell.label, legs[0].GrantScopeType, pool.ScopeType)
			}
			if legs[0].Source != pool.Source {
				t.Fatalf("%s: funder leg source %s != view source %s", cell.label, legs[0].Source, pool.Source)
			}
		})
	}
}

// TestEntitlementsViewEmptyOrgRendersEmpty pins the empty-grants contract that
// the rent-a-sandbox UI relies on for its empty state.
func TestEntitlementsViewEmptyOrgRendersEmpty(t *testing.T) {
	t.Parallel()
	view := buildEntitlementsView(99, nil, nil, nil, nil)
	if view.OrgID != 99 {
		t.Fatalf("OrgID = %d, want 99", view.OrgID)
	}
	if len(view.Universal) != 0 || len(view.Products) != 0 {
		t.Fatalf("expected empty view, got %+v", view)
	}
}

// TestFundingPrecedenceConstantsAreStable pins the precedence ordering. Any
// change to GrantScopeFundingOrder or GrantSourceFundingOrder must be a
// deliberate edit to this test, since the view-model rendering and the funder
// consumption walk both bake these constants in.
func TestFundingPrecedenceConstantsAreStable(t *testing.T) {
	t.Parallel()
	wantScope := []GrantScopeType{GrantScopeSKU, GrantScopeBucket, GrantScopeProduct, GrantScopeAccount}
	if len(GrantScopeFundingOrder) != len(wantScope) {
		t.Fatalf("GrantScopeFundingOrder len = %d, want %d", len(GrantScopeFundingOrder), len(wantScope))
	}
	for i := range wantScope {
		if GrantScopeFundingOrder[i] != wantScope[i] {
			t.Fatalf("GrantScopeFundingOrder[%d] = %s, want %s", i, GrantScopeFundingOrder[i], wantScope[i])
		}
	}
	wantSource := []GrantSourceType{SourceFreeTier, SourceSubscription, SourcePurchase, SourcePromo, SourceRefund}
	if len(GrantSourceFundingOrder) != len(wantSource) {
		t.Fatalf("GrantSourceFundingOrder len = %d, want %d", len(GrantSourceFundingOrder), len(wantSource))
	}
	for i := range wantSource {
		if GrantSourceFundingOrder[i] != wantSource[i] {
			t.Fatalf("GrantSourceFundingOrder[%d] = %s, want %s", i, GrantSourceFundingOrder[i], wantSource[i])
		}
	}
}

type viewCell struct {
	label string
	pool  EntitlementPool
}

func walkViewCells(view EntitlementsView) []viewCell {
	var cells []viewCell
	for _, p := range view.Universal {
		cells = append(cells, viewCell{label: "universal/" + p.Source.String(), pool: p})
	}
	for _, prod := range view.Products {
		for _, p := range prod.ProductPools {
			cells = append(cells, viewCell{label: "product/" + prod.ProductID + "/" + p.Source.String(), pool: p})
		}
		for _, bucket := range prod.Buckets {
			for _, p := range bucket.Pools {
				label := fmt.Sprintf("bucket/%s/%s/%s/%s", bucket.BucketID, p.ScopeType, p.Source, sentinel(p.SKUID))
				cells = append(cells, viewCell{label: label, pool: p})
			}
		}
	}
	return cells
}

func sentinel(s string) string {
	if s == "" {
		return "_"
	}
	return s
}

// scopedGrantsFromPool projects a view pool back to the funder's input shape,
// preserving entry order so the funder's tiebreak agrees with the view's.
func scopedGrantsFromPool(t *testing.T, pool EntitlementPool) []scopedGrantBalance {
	t.Helper()
	out := make([]scopedGrantBalance, 0, len(pool.Entries))
	for _, entry := range pool.Entries {
		gid, err := ParseGrantID(entry.GrantID)
		if err != nil {
			t.Fatalf("ParseGrantID(%q): %v", entry.GrantID, err)
		}
		out = append(out, scopedGrantBalance{
			GrantID:        gid,
			Source:         pool.Source,
			ScopeType:      pool.ScopeType,
			ScopeProductID: pool.ProductID,
			ScopeBucketID:  pool.BucketID,
			ScopeSKUID:     pool.SKUID,
			AvailableUnits: entry.Available,
		})
	}
	return out
}

// chargeForPoolCoverage builds a unit charge whose (bucket, sku) shape exactly
// matches what the pool covers. SKU-scoped grants only fund charges that name
// the SKU; bucket/product/account-scoped grants accept SKUID="".
func chargeForPoolCoverage(pool EntitlementPool) chargeLine {
	bucketID := pool.BucketID
	if bucketID == "" {
		bucketID = "compute"
	}
	return chargeLine{BucketID: bucketID, SKUID: pool.SKUID, AmountUnits: 1}
}

func uniqueScopeSourceCells(grants []GrantBalance) int {
	type key struct {
		scope    GrantScopeType
		product  string
		bucket   string
		sku      string
		source   GrantSourceType
	}
	seen := map[key]struct{}{}
	for _, g := range grants {
		seen[key{g.ScopeType, g.ScopeProductID, g.ScopeBucketID, g.ScopeSKUID, g.Source}] = struct{}{}
	}
	return len(seen)
}

// viewFundingGrant builds a GrantBalance whose grant id is deterministic in
// the (source, scope, *, ref) tuple — same scheme as testScopedGrant — so the
// rendered entry id and the funder's leg id agree.
func viewFundingGrant(ref string, source GrantSourceType, scope GrantScopeType, productID, bucketID, skuID string, available uint64, expiresInDays int) GrantBalance {
	expires := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(expiresInDays) * 24 * time.Hour)
	return GrantBalance{
		GrantID:        sourceReferenceGrantID(42, source, scope, productID, bucketID, skuID, ref),
		ScopeType:      scope,
		ScopeProductID: productID,
		ScopeBucketID:  bucketID,
		ScopeSKUID:     skuID,
		Source:         source,
		StartsAt:       time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		ExpiresAt:      &expires,
		Available:      available,
	}
}
