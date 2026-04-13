package billing

import (
	"fmt"
	"testing"
	"time"
)

// TestEntitlementsViewMatchesFunderPrecedence is the load-bearing contract
// test between the entitlements view-model and the reserve-time funder. The
// view's "next-to-spend" position in any cell is read by customers as a claim
// about what the funder will burn first; if the two ever drift, this test
// fires.
//
// For each rendered cell — i.e., each (slot, source, plan_id) → SourceTotal
// with non-zero available — we run planGrantFunding against the *full* grant
// set with a unit charge that lands cleanly on this cell's scope (no narrower
// scope can shadow it), and assert that the funder's first leg is the same
// grant the view shows on top (SourceTotal.TopGrantID). Feeding the full
// grant set is the spec's stated bar — feeding only the cell's grants would
// silently miss cross-scope ordering bugs.
func TestEntitlementsViewMatchesFunderPrecedence(t *testing.T) {
	t.Parallel()

	productID := "sandbox"
	now := time.Date(2026, time.April, 12, 0, 0, 0, 0, time.UTC)
	// `isolated` is a bucket with no Bucket- or SKU-scope grants in this
	// fixture, so charges against it fall through to Product- and
	// Account-scope. That's the only way to test wider-scope cells against
	// the full grant set without a narrower scope catching the charge first.
	catalog := buildFundingTestCatalog(productID)

	// Wider-scope cells use *disjoint* sources from narrower scopes on
	// purpose. The funder consumes most-specific first within each source,
	// so e.g. a Universal/promo cell would be unreachable for *any* charge
	// in `sandbox` if there were also a Product/promo grant — Product/promo
	// would drain first. Each cell we test must have a charge target that
	// reaches it without a narrower-same-source slot shadowing it; that's
	// why product/account here use `promo` and `purchase`/`refund` rather
	// than every source. Preserving this disjointness is load-bearing — if
	// you tighten it, half the cells in this test become unreachable and
	// the assertions go silently weak. Document the change here if you
	// touch the fixture.
	grants := []GrantBalance{
		// SKU-scope on (block_storage, premium_nvme): free_tier + subscription.
		viewFundingGrant("sku-free-a", SourceFreeTier, GrantScopeSKU, productID, "block_storage", "premium_nvme", 50, 7),
		viewFundingGrant("sku-free-b", SourceFreeTier, GrantScopeSKU, productID, "block_storage", "premium_nvme", 50, 30),
		viewFundingGrant("sku-sub-a", SourceSubscription, GrantScopeSKU, productID, "block_storage", "premium_nvme", 50, 14),
		// Bucket-scope on (compute, *): free_tier + subscription.
		viewFundingGrant("bkt-free-a", SourceFreeTier, GrantScopeBucket, productID, "compute", "", 50, 7),
		viewFundingGrant("bkt-free-b", SourceFreeTier, GrantScopeBucket, productID, "compute", "", 50, 30),
		viewFundingGrant("bkt-sub-a", SourceSubscription, GrantScopeBucket, productID, "compute", "", 50, 14),
		// Product-scope on sandbox: promo only (no narrower same-source grant).
		viewFundingGrant("prd-promo-a", SourcePromo, GrantScopeProduct, productID, "", "", 50, 14),
		viewFundingGrant("prd-promo-b", SourcePromo, GrantScopeProduct, productID, "", "", 50, 30),
		// Account-scope universal: purchase + refund only (no narrower
		// same-source grant). If you add purchase or refund anywhere
		// narrower, the matching account cell becomes unreachable and this
		// test will fire — that's the intended canary.
		viewFundingGrant("acc-pur-a", SourcePurchase, GrantScopeAccount, "", "", "", 50, 7),
		viewFundingGrant("acc-pur-b", SourcePurchase, GrantScopeAccount, "", "", "", 50, 30),
		viewFundingGrant("acc-ref-a", SourceRefund, GrantScopeAccount, "", "", "", 50, 14),
	}

	allGrants := scopedGrantsFromBalances(grants)
	view := buildEntitlementsView(42, now, catalog, grants)

	cells := walkViewCells(view)
	if got, want := len(cells), uniqueScopeSourceCells(grants); got != want {
		t.Fatalf("walkViewCells = %d cells, want %d", got, want)
	}

	for _, cell := range cells {
		t.Run(cell.label, func(t *testing.T) {
			if cell.total.AvailableUnits == 0 {
				t.Fatalf("%s: empty cell surfaced from accumulator", cell.label)
			}
			charge := chargeForCell(cell.slot)
			candidate := grantsForSource(allGrants, cell.total.Source)
			legs, err := planGrantFunding(productID, []chargeLine{charge}, candidate)
			if err != nil {
				t.Fatalf("%s: planGrantFunding: %v", cell.label, err)
			}
			if len(legs) == 0 {
				t.Fatalf("%s: funder produced no legs", cell.label)
			}
			if got := legs[0].GrantID.String(); got != cell.total.TopGrantID {
				t.Fatalf("%s: funder picked %s first; view shows %s", cell.label, got, cell.total.TopGrantID)
			}
			if legs[0].GrantScopeType != cell.slot.ScopeType {
				t.Fatalf("%s: funder leg scope %s != view scope %s", cell.label, legs[0].GrantScopeType, cell.slot.ScopeType)
			}
			if legs[0].Source != cell.total.Source {
				t.Fatalf("%s: funder leg source %s != view source %s", cell.label, legs[0].Source, cell.total.Source)
			}
		})
	}
}

// TestEntitlementsViewFundingPrefersTighterScope is the cross-scope drift
// canary the within-cell test cannot catch on its own. It deliberately puts
// SKU-scope and Bucket-scope grants on the *same* (bucket, sku) target. A
// charge that names the SKU must drain the SKU slot first; the bucket slot
// must remain untouched until the SKU slot is empty. If the funder ever stops
// honouring scope precedence, or the view starts claiming the bucket slot's
// top entry is next-to-spend for that target, this test fires.
func TestEntitlementsViewFundingPrefersTighterScope(t *testing.T) {
	t.Parallel()

	productID := "sandbox"
	now := time.Date(2026, time.April, 12, 0, 0, 0, 0, time.UTC)
	catalog := buildFundingTestCatalog(productID)
	grants := []GrantBalance{
		viewFundingGrant("sku-free", SourceFreeTier, GrantScopeSKU, productID, "block_storage", "premium_nvme", 10, 7),
		viewFundingGrant("bkt-free", SourceFreeTier, GrantScopeBucket, productID, "block_storage", "", 10, 7),
	}
	view := buildEntitlementsView(43, now, catalog, grants)

	productSection, ok := findProductSection(view, productID)
	if !ok {
		t.Fatal("product section missing from view")
	}
	bucket, ok := findBucket(productSection, "block_storage")
	if !ok {
		t.Fatal("block_storage bucket missing from product section")
	}
	if bucket.BucketSlot == nil {
		t.Fatal("expected bucket slot ('Any Block Storage SKU') to be populated")
	}
	skuSlot, ok := findSKU(bucket, "premium_nvme")
	if !ok {
		t.Fatal("premium_nvme SKU slot missing from bucket")
	}
	if len(skuSlot.Sources) != 1 || skuSlot.Sources[0].Source != SourceFreeTier {
		t.Fatalf("expected one free-tier source on SKU slot, got %+v", skuSlot.Sources)
	}
	skuTop := skuSlot.Sources[0].TopGrantID

	legs, err := planGrantFunding(productID, []chargeLine{
		{BucketID: "block_storage", SKUID: "premium_nvme", AmountUnits: 1},
	}, scopedGrantsFromBalances(grants))
	if err != nil {
		t.Fatalf("planGrantFunding: %v", err)
	}
	if legs[0].GrantScopeType != GrantScopeSKU {
		t.Fatalf("funder pulled %s first; should be sku scope", legs[0].GrantScopeType)
	}
	if legs[0].GrantID.String() != skuTop {
		t.Fatalf("funder pulled %s; sku slot top is %s", legs[0].GrantID, skuTop)
	}
}

// TestEntitlementsViewEmptyOrgRendersCatalog pins the empty-grants contract
// the rent-a-sandbox UI relies on: even with zero grants, the catalog spine
// renders, and every active SKU appears as a $0 row. The "no active credits"
// empty-state card has been deleted; the catalog itself communicates emptiness.
func TestEntitlementsViewEmptyOrgRendersCatalog(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.April, 12, 0, 0, 0, 0, time.UTC)
	catalog := buildFundingTestCatalog("sandbox")
	view := buildEntitlementsView(99, now, catalog, nil)

	if view.OrgID != 99 {
		t.Fatalf("OrgID = %d, want 99", view.OrgID)
	}
	if view.Universal.AvailableUnits != 0 || len(view.Universal.Sources) != 0 {
		t.Fatalf("expected empty Universal slot, got %+v", view.Universal)
	}
	if view.Universal.CoverageLabel != "Usable anywhere" {
		t.Fatalf("Universal coverage label = %q", view.Universal.CoverageLabel)
	}
	if got := len(view.Products); got != 1 {
		t.Fatalf("expected 1 product section, got %d", got)
	}
	product := view.Products[0]
	if product.ProductSlot != nil {
		t.Fatalf("expected nil ProductSlot on empty org, got %+v", product.ProductSlot)
	}
	totalSKUs := 0
	for _, bucket := range product.Buckets {
		if bucket.BucketSlot != nil {
			t.Fatalf("expected nil BucketSlot on empty org, got %+v", bucket.BucketSlot)
		}
		for _, sku := range bucket.SKUSlots {
			if sku.AvailableUnits != 0 || len(sku.Sources) != 0 {
				t.Fatalf("expected empty SKU slot %s, got %+v", sku.SKUID, sku)
			}
			totalSKUs++
		}
	}
	if totalSKUs == 0 {
		t.Fatal("expected catalog spine to surface SKU rows even with no grants")
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

// buildFundingTestCatalog builds the in-memory catalog the funding tests use.
// `compute`, `block_storage`, and `isolated` buckets each carry one SKU. The
// `isolated` bucket exists solely so wider-scope cells (product, account)
// have a charge target that no narrower scope can shadow.
func buildFundingTestCatalog(productID string) entitlementCatalog {
	return entitlementCatalog{
		Products: []entitlementCatalogProduct{
			{
				ProductID:   productID,
				DisplayName: "Sandbox",
				Buckets: []entitlementCatalogBucket{
					{
						BucketID:    "compute",
						DisplayName: "Compute",
						SortOrder:   10,
						SKUs: []entitlementCatalogSKU{
							{SKUID: "compute_sku", DisplayName: "Compute"},
						},
					},
					{
						BucketID:    "block_storage",
						DisplayName: "Block Storage",
						SortOrder:   30,
						SKUs: []entitlementCatalogSKU{
							{SKUID: "premium_nvme", DisplayName: "Premium NVMe"},
						},
					},
					{
						BucketID:    "isolated",
						DisplayName: "Isolated",
						SortOrder:   90,
						SKUs: []entitlementCatalogSKU{
							{SKUID: "isolated_sku", DisplayName: "Isolated"},
						},
					},
				},
			},
		},
	}
}

type viewCell struct {
	label string
	slot  EntitlementSlot
	total EntitlementSourceTotal
}

func walkViewCells(view EntitlementsView) []viewCell {
	var cells []viewCell
	emit := func(label string, slot EntitlementSlot) {
		for _, total := range slot.Sources {
			if total.AvailableUnits == 0 {
				continue
			}
			cells = append(cells, viewCell{
				label: fmt.Sprintf("%s/%s/%s", label, total.Source, sentinel(total.PlanID)),
				slot:  slot,
				total: total,
			})
		}
	}
	emit("universal", view.Universal)
	for _, prod := range view.Products {
		if prod.ProductSlot != nil {
			emit("product/"+prod.ProductID, *prod.ProductSlot)
		}
		for _, bucket := range prod.Buckets {
			if bucket.BucketSlot != nil {
				emit("bucket/"+bucket.BucketID, *bucket.BucketSlot)
			}
			for _, sku := range bucket.SKUSlots {
				emit("sku/"+bucket.BucketID+"/"+sku.SKUID, sku)
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

func findProductSection(view EntitlementsView, productID string) (EntitlementProductSection, bool) {
	for _, p := range view.Products {
		if p.ProductID == productID {
			return p, true
		}
	}
	return EntitlementProductSection{}, false
}

func findBucket(section EntitlementProductSection, bucketID string) (EntitlementBucketSection, bool) {
	for _, b := range section.Buckets {
		if b.BucketID == bucketID {
			return b, true
		}
	}
	return EntitlementBucketSection{}, false
}

func findSKU(bucket EntitlementBucketSection, skuID string) (EntitlementSlot, bool) {
	for _, slot := range bucket.SKUSlots {
		if slot.SKUID == skuID {
			return slot, true
		}
	}
	return EntitlementSlot{}, false
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
func chargeForCell(slot EntitlementSlot) chargeLine {
	switch slot.ScopeType {
	case GrantScopeSKU:
		return chargeLine{BucketID: slot.BucketID, SKUID: slot.SKUID, AmountUnits: 1}
	case GrantScopeBucket:
		return chargeLine{BucketID: slot.BucketID, AmountUnits: 1}
	default:
		return chargeLine{BucketID: "isolated", AmountUnits: 1}
	}
}

func uniqueScopeSourceCells(grants []GrantBalance) int {
	type key struct {
		scope   GrantScopeType
		product string
		bucket  string
		sku     string
		source  GrantSourceType
	}
	seen := map[key]struct{}{}
	for _, g := range grants {
		seen[key{g.ScopeType, g.ScopeProductID, g.ScopeBucketID, g.ScopeSKUID, g.Source}] = struct{}{}
	}
	return len(seen)
}

// viewFundingGrant builds a GrantBalance whose grant id is deterministic in
// the (source, scope, *, ref) tuple — same scheme as testScopedGrant — so the
// rendered top-grant id and the funder's leg id agree. The funding tests do
// not exercise the period predicate; TestEntitlementsViewPeriodStartAggregation
// covers it directly.
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
		OriginalAmount: available,
		Available:      available,
	}
}

// TestEntitlementsViewPeriodStartAggregation pins the boundary semantics of
// the half-open `period_start <= now < period_end` predicate that drives the
// "Period started with" column. Date arithmetic is a famous bug nest; this
// test exists so a future maintainer who refactors `periodCovered` cannot
// silently flip a `<` to `<=` (or forget that PeriodStart/PeriodEnd are
// independently nullable in Go even though the schema constrains them to
// move together) without one of these subtests firing.
func TestEntitlementsViewPeriodStartAggregation(t *testing.T) {
	t.Parallel()

	productID := "sandbox"
	now := time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC)
	catalog := buildFundingTestCatalog(productID)

	periodStart := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	staleStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	staleEnd := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC) // exactly == now's open boundary case
	futureStart := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	futureEnd := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	noPeriodGrantStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	// Each grant lands on the universal slot so the test reads off
	// view.Universal directly. Within the universal slot we get one
	// SourceTotal per source, so the per-source PeriodStart expectations
	// can be checked precisely.
	grants := []GrantBalance{
		{
			GrantID:        sourceReferenceGrantID(42, SourceFreeTier, GrantScopeAccount, "", "", "", "in-period-1"),
			ScopeType:      GrantScopeAccount,
			Source:         SourceFreeTier,
			StartsAt:       periodStart,
			PeriodStart:    &periodStart,
			PeriodEnd:      &periodEnd,
			OriginalAmount: 100,
			Available:      90,
		},
		{
			GrantID:        sourceReferenceGrantID(42, SourceFreeTier, GrantScopeAccount, "", "", "", "in-period-2"),
			ScopeType:      GrantScopeAccount,
			Source:         SourceFreeTier,
			StartsAt:       periodStart,
			PeriodStart:    &periodStart,
			PeriodEnd:      &periodEnd,
			OriginalAmount: 50,
			Available:      50,
		},
		{
			// `now` falls exactly on the closed-side boundary of the prior
			// period; the half-open predicate must EXCLUDE this grant.
			GrantID:        sourceReferenceGrantID(42, SourceSubscription, GrantScopeAccount, "", "", "", "boundary-prior"),
			ScopeType:      GrantScopeAccount,
			Source:         SourceSubscription,
			StartsAt:       staleStart,
			PeriodStart:    &staleStart,
			PeriodEnd:      &staleEnd,
			OriginalAmount: 200,
			Available:      0,
		},
		{
			// future-period grant — should not contribute.
			GrantID:        sourceReferenceGrantID(42, SourceSubscription, GrantScopeAccount, "", "", "", "future-period"),
			ScopeType:      GrantScopeAccount,
			Source:         SourceSubscription,
			StartsAt:       futureStart,
			PeriodStart:    &futureStart,
			PeriodEnd:      &futureEnd,
			OriginalAmount: 75,
			Available:      75,
		},
		{
			// non-period grant (purchase) — should not contribute to
			// PeriodStartUnits but its Available counts.
			GrantID:        sourceReferenceGrantID(42, SourcePurchase, GrantScopeAccount, "", "", "", "no-period"),
			ScopeType:      GrantScopeAccount,
			Source:         SourcePurchase,
			StartsAt:       noPeriodGrantStart,
			OriginalAmount: 500,
			Available:      500,
		},
	}

	view := buildEntitlementsView(42, now, catalog, grants)

	if got, want := view.Universal.PeriodStartUnits, uint64(150); got != want {
		t.Fatalf("Universal.PeriodStartUnits = %d, want %d (sum of in-period free-tier grants only)", got, want)
	}
	if got, want := view.Universal.AvailableUnits, uint64(715); got != want {
		t.Fatalf("Universal.AvailableUnits = %d, want %d (sum of all available)", got, want)
	}

	freeTier := findUniversalSource(t, view, SourceFreeTier)
	if got, want := freeTier.PeriodStartUnits, uint64(150); got != want {
		t.Fatalf("free_tier.PeriodStartUnits = %d, want %d", got, want)
	}
	subscription := findUniversalSource(t, view, SourceSubscription)
	if got, want := subscription.PeriodStartUnits, uint64(0); got != want {
		t.Fatalf("subscription.PeriodStartUnits = %d, want %d (boundary-prior excluded by half-open, future-period excluded by start>now)", got, want)
	}
	purchase := findUniversalSource(t, view, SourcePurchase)
	if got, want := purchase.PeriodStartUnits, uint64(0); got != want {
		t.Fatalf("purchase.PeriodStartUnits = %d, want %d (non-period grant)", got, want)
	}
	if got, want := purchase.AvailableUnits, uint64(500); got != want {
		t.Fatalf("purchase.AvailableUnits = %d, want %d", got, want)
	}
}

// TestEntitlementsViewPeriodStartHalfOpenInclusiveStart asserts that a grant
// whose period_start is exactly equal to `now` IS counted (closed left side).
// Sibling test to TestEntitlementsViewPeriodStartAggregation's open-right
// boundary case.
func TestEntitlementsViewPeriodStartHalfOpenInclusiveStart(t *testing.T) {
	t.Parallel()
	productID := "sandbox"
	now := time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC)
	catalog := buildFundingTestCatalog(productID)
	end := now.Add(30 * 24 * time.Hour)
	grants := []GrantBalance{{
		GrantID:        sourceReferenceGrantID(42, SourceFreeTier, GrantScopeAccount, "", "", "", "now-equals-start"),
		ScopeType:      GrantScopeAccount,
		Source:         SourceFreeTier,
		StartsAt:       now,
		PeriodStart:    &now,
		PeriodEnd:      &end,
		OriginalAmount: 42,
		Available:      42,
	}}
	view := buildEntitlementsView(42, now, catalog, grants)
	if view.Universal.PeriodStartUnits != 42 {
		t.Fatalf("now == period_start should be inclusive; got PeriodStartUnits=%d", view.Universal.PeriodStartUnits)
	}
}

// TestEntitlementsViewInlineExpiry pins the inline-expiry surfacing rule:
// non-period grants surface their earliest ExpiresAt on the source total;
// period-bound grants do not (their expiry is implicit in the period
// boundary, shown once at the row level if at all). Multiple non-period
// grants in the same source collapse to the earliest expiry.
func TestEntitlementsViewInlineExpiry(t *testing.T) {
	t.Parallel()
	productID := "sandbox"
	now := time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC)
	catalog := buildFundingTestCatalog(productID)

	earlyExpiry := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	lateExpiry := time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)
	periodStart := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)

	grants := []GrantBalance{
		{
			// Non-period purchase, late expiry.
			GrantID:        sourceReferenceGrantID(42, SourcePurchase, GrantScopeAccount, "", "", "", "late-purchase"),
			ScopeType:      GrantScopeAccount,
			Source:         SourcePurchase,
			StartsAt:       now.Add(-24 * time.Hour),
			ExpiresAt:      &lateExpiry,
			OriginalAmount: 100,
			Available:      100,
		},
		{
			// Non-period purchase, early expiry — must win.
			GrantID:        sourceReferenceGrantID(42, SourcePurchase, GrantScopeAccount, "", "", "", "early-purchase"),
			ScopeType:      GrantScopeAccount,
			Source:         SourcePurchase,
			StartsAt:       now.Add(-24 * time.Hour),
			ExpiresAt:      &earlyExpiry,
			OriginalAmount: 50,
			Available:      50,
		},
		{
			// Period-bound free-tier grant with its own ExpiresAt — must NOT
			// surface inline because its expiry is implicit in the period.
			GrantID:        sourceReferenceGrantID(42, SourceFreeTier, GrantScopeAccount, "", "", "", "period-grant"),
			ScopeType:      GrantScopeAccount,
			Source:         SourceFreeTier,
			StartsAt:       periodStart,
			PeriodStart:    &periodStart,
			PeriodEnd:      &periodEnd,
			ExpiresAt:      &periodEnd,
			OriginalAmount: 30,
			Available:      30,
		},
	}
	view := buildEntitlementsView(42, now, catalog, grants)

	purchase := findUniversalSource(t, view, SourcePurchase)
	if purchase.InlineExpiresAt == nil {
		t.Fatalf("purchase source missing inline expiry")
	}
	if !purchase.InlineExpiresAt.Equal(earlyExpiry) {
		t.Fatalf("purchase inline expiry = %s, want earliest = %s", purchase.InlineExpiresAt, earlyExpiry)
	}
	freeTier := findUniversalSource(t, view, SourceFreeTier)
	if freeTier.InlineExpiresAt != nil {
		t.Fatalf("period-bound free_tier source should not surface inline expiry; got %s", freeTier.InlineExpiresAt)
	}
}

func findUniversalSource(t *testing.T, view EntitlementsView, source GrantSourceType) EntitlementSourceTotal {
	t.Helper()
	for _, total := range view.Universal.Sources {
		if total.Source == source {
			return total
		}
	}
	t.Fatalf("universal slot has no %s source; sources=%+v", source, view.Universal.Sources)
	return EntitlementSourceTotal{}
}
