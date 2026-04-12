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
// For each pool surfaced by buildEntitlementsView, we run planGrantFunding
// against the *full* grant set with a unit charge that lands cleanly on this
// cell's scope (i.e. no narrower scope can shadow it), and assert that the
// funder's first leg is the same grant the view shows on top. Feeding the
// full grant set is the spec's stated bar — feeding only the cell's grants
// would silently miss cross-scope ordering bugs.
func TestEntitlementsViewMatchesFunderPrecedence(t *testing.T) {
	t.Parallel()

	productID := "sandbox"
	productNames := map[string]string{productID: "Sandbox"}
	// `isolated` is a bucket with no Bucket- or SKU-scope grants, so charges
	// against it fall through to Product- and Account-scope. That's the only
	// way to test wider-scope cells against the full grant set without a
	// narrower scope catching the charge first.
	bucketCatalog := map[string]bucketCatalogRow{
		"compute":       {DisplayName: "Compute", SortOrder: 10},
		"block_storage": {DisplayName: "Block Storage", SortOrder: 30},
		"isolated":      {DisplayName: "Isolated", SortOrder: 90},
	}
	skuCatalog := map[string]skuCatalogRow{
		"premium_nvme": {DisplayName: "Premium NVMe", BucketID: "block_storage", ProductID: productID},
	}

	// Two grants per (scope, source) where possible so within-pool ordering
	// is non-trivial. Earlier expiry sorts first, matching listGrantBalances.
	//
	// Wider-scope cells use *disjoint* sources from narrower scopes on
	// purpose. The funder consumes most-specific first within each source, so
	// e.g. a Universal/promo cell would be unreachable for *any* charge in
	// `sandbox` if there were also a Product/promo grant — Product/promo
	// would drain first. Each cell we test must have a charge target that
	// reaches it without a narrower-same-source pool shadowing it; that's
	// why product/account here use `promo` and `refund` rather than every
	// source.
	grants := []GrantBalance{
		// SKU-scope on (block_storage, premium_nvme): free_tier + subscription.
		viewFundingGrant("sku-free-a", SourceFreeTier, GrantScopeSKU, productID, "block_storage", "premium_nvme", 50, 7),
		viewFundingGrant("sku-free-b", SourceFreeTier, GrantScopeSKU, productID, "block_storage", "premium_nvme", 50, 30),
		viewFundingGrant("sku-sub-a", SourceSubscription, GrantScopeSKU, productID, "block_storage", "premium_nvme", 50, 14),
		// Bucket-scope on (compute, *): free_tier + subscription. Same sources
		// as SKU above is fine: SKU is in block_storage, Bucket is in compute.
		viewFundingGrant("bkt-free-a", SourceFreeTier, GrantScopeBucket, productID, "compute", "", 50, 7),
		viewFundingGrant("bkt-free-b", SourceFreeTier, GrantScopeBucket, productID, "compute", "", 50, 30),
		viewFundingGrant("bkt-sub-a", SourceSubscription, GrantScopeBucket, productID, "compute", "", 50, 14),
		// Product-scope on sandbox: promo only (no narrower same-source grant).
		viewFundingGrant("prd-promo-a", SourcePromo, GrantScopeProduct, productID, "", "", 50, 14),
		viewFundingGrant("prd-promo-b", SourcePromo, GrantScopeProduct, productID, "", "", 50, 30),
		// Account-scope universal: purchase + refund only (no narrower
		// same-source grant). If you add purchase or refund anywhere narrower,
		// the matching account cell becomes unreachable and this test will
		// fire — that's the intended canary.
		viewFundingGrant("acc-pur-a", SourcePurchase, GrantScopeAccount, "", "", "", 50, 7),
		viewFundingGrant("acc-pur-b", SourcePurchase, GrantScopeAccount, "", "", "", 50, 30),
		viewFundingGrant("acc-ref-a", SourceRefund, GrantScopeAccount, "", "", "", 50, 14),
	}

	allGrants := scopedGrantsFromBalances(grants)

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
			// Construct a charge that lands cleanly on this cell's scope. The
			// funder consumes most-specific first; for a wider-scope cell to
			// be reachable, the charge must not match any narrower scope.
			charge := chargeForCell(pool)
			// Run against the full grant set, restricted to grants of the
			// cell's source so the source-precedence inside the funder
			// doesn't surface a different (more-specific or earlier-source)
			// pool first.
			candidate := grantsForSource(allGrants, pool.Source)
			legs, err := planGrantFunding(productID, []chargeLine{charge}, candidate)
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

// TestEntitlementsViewFundingPrefersTighterScope is the cross-scope drift
// canary the within-pool test cannot catch on its own. It deliberately puts
// SKU-scope and Bucket-scope grants on the *same* (bucket, sku) target. A
// charge that names the SKU must drain the SKU pool first — the bucket pool
// must remain untouched until the SKU pool is empty. If the funder ever stops
// honouring scope precedence, or the view starts showing the bucket pool's
// top entry as next-to-spend for that target, this test fires.
func TestEntitlementsViewFundingPrefersTighterScope(t *testing.T) {
	t.Parallel()

	productID := "sandbox"
	bucketCatalog := map[string]bucketCatalogRow{
		"block_storage": {DisplayName: "Block Storage", SortOrder: 30},
	}
	skuCatalog := map[string]skuCatalogRow{
		"premium_nvme": {DisplayName: "Premium NVMe", BucketID: "block_storage", ProductID: productID},
	}
	grants := []GrantBalance{
		viewFundingGrant("sku-free", SourceFreeTier, GrantScopeSKU, productID, "block_storage", "premium_nvme", 10, 7),
		viewFundingGrant("bkt-free", SourceFreeTier, GrantScopeBucket, productID, "block_storage", "", 10, 7),
	}
	view := buildEntitlementsView(43, grants, map[string]string{productID: "Sandbox"}, bucketCatalog, skuCatalog)

	bucket := view.Products[0].Buckets[0]
	if got := len(bucket.Pools); got != 2 {
		t.Fatalf("bucket pool count = %d, want 2 (one SKU, one Bucket)", got)
	}
	// Find the SKU-scope pool and assert its top entry is what the funder
	// drains for a (block_storage, premium_nvme) charge against the full set.
	var skuPool EntitlementPool
	for _, p := range bucket.Pools {
		if p.ScopeType == GrantScopeSKU {
			skuPool = p
		}
	}
	if skuPool.ScopeType != GrantScopeSKU {
		t.Fatal("no SKU pool surfaced")
	}
	legs, err := planGrantFunding(productID, []chargeLine{
		{BucketID: "block_storage", SKUID: "premium_nvme", AmountUnits: 1},
	}, scopedGrantsFromBalances(grants))
	if err != nil {
		t.Fatalf("planGrantFunding: %v", err)
	}
	if legs[0].GrantScopeType != GrantScopeSKU {
		t.Fatalf("funder pulled %s first; should be sku scope", legs[0].GrantScopeType)
	}
	if legs[0].GrantID.String() != skuPool.Entries[0].GrantID {
		t.Fatalf("funder pulled %s; sku pool top is %s", legs[0].GrantID, skuPool.Entries[0].GrantID)
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

// scopedGrantsFromBalances projects the test fixture's GrantBalances into the
// scopedGrantBalance shape the funder consumes.
func scopedGrantsFromBalances(grants []GrantBalance) []scopedGrantBalance {
	out := make([]scopedGrantBalance, 0, len(grants))
	for _, g := range grants {
		out = append(out, scopedGrantBalance{
			GrantID:        g.GrantID,
			Source:         g.Source,
			ScopeType:      g.ScopeType,
			ScopeProductID: g.ScopeProductID,
			ScopeBucketID:  g.ScopeBucketID,
			ScopeSKUID:     g.ScopeSKUID,
			AvailableUnits: g.Available,
		})
	}
	return out
}

// grantsForSource is the source-precedence isolation knob: planGrantFunding
// walks GrantSourceFundingOrder inside each scope, so a free-tier grant in
// the same scope as a promo grant would always drain first. Restricting the
// candidate set to the cell's source lets us assert "this cell's top entry
// is what the funder picks" without being lied to by source ordering.
func grantsForSource(grants []scopedGrantBalance, source GrantSourceType) []scopedGrantBalance {
	out := make([]scopedGrantBalance, 0, len(grants))
	for _, g := range grants {
		if g.Source == source {
			out = append(out, g)
		}
	}
	return out
}

// chargeForCell builds a unit charge that lands on the cell's scope without
// being shadowed by a narrower scope in the test fixture. SKU cells charge
// the SKU; bucket cells charge the bucket with no SKU; product/account cells
// charge the `isolated` bucket which has no narrower-scope grants.
func chargeForCell(pool EntitlementPool) chargeLine {
	switch pool.ScopeType {
	case GrantScopeSKU:
		return chargeLine{BucketID: pool.BucketID, SKUID: pool.SKUID, AmountUnits: 1}
	case GrantScopeBucket:
		return chargeLine{BucketID: pool.BucketID, AmountUnits: 1}
	default:
		return chargeLine{BucketID: "isolated", AmountUnits: 1}
	}
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
