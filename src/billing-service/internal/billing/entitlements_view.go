package billing

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/lib/pq"
)

type EntitlementsView struct {
	OrgID     OrgID
	Universal []EntitlementPool
	Products  []EntitlementProductSection
}

type EntitlementProductSection struct {
	ProductID    string
	DisplayName  string
	ProductPools []EntitlementPool
	Buckets      []EntitlementBucketSection
}

type EntitlementBucketSection struct {
	BucketID    string
	DisplayName string
	Pools       []EntitlementPool
}

type EntitlementPool struct {
	ScopeType      GrantScopeType
	ProductID      string
	ProductDisplay string
	BucketID       string
	BucketDisplay  string
	SKUID          string
	SKUDisplay     string
	CoverageLabel  string
	Source         GrantSourceType
	Entries        []EntitlementGrantEntry
}

type EntitlementGrantEntry struct {
	GrantID     string
	Available   uint64
	Pending     uint64
	StartsAt    time.Time
	PeriodStart *time.Time
	PeriodEnd   *time.Time
	ExpiresAt   *time.Time
}

const (
	coverageUniversal = "Usable anywhere"
	coverageProduct   = "Any bucket in %s"
	coverageBucketAny = "Any %s SKU"
)

// ListEntitlementsView returns the org's open credit grants grouped server-side
// into the customer-facing pools defined in `docs/billing-architecture.md`. The
// shape never sums across (scope, source) — see grant_funding_plan.go for the
// precedence the funder applies.
func (c *Client) ListEntitlementsView(ctx context.Context, orgID OrgID) (EntitlementsView, error) {
	if err := ctx.Err(); err != nil {
		return EntitlementsView{}, err
	}

	grants, err := c.listGrantBalances(ctx, orgID, "")
	if err != nil {
		return EntitlementsView{}, fmt.Errorf("list grants: %w", err)
	}
	if len(grants) == 0 {
		return EntitlementsView{OrgID: orgID}, nil
	}

	productNames, bucketCatalog, skuCatalog, err := c.loadEntitlementCatalogs(ctx, grants)
	if err != nil {
		return EntitlementsView{}, err
	}
	return buildEntitlementsView(orgID, grants, productNames, bucketCatalog, skuCatalog), nil
}

func (c *Client) loadEntitlementCatalogs(ctx context.Context, grants []GrantBalance) (map[string]string, map[string]bucketCatalogRow, map[string]skuCatalogRow, error) {
	productIDs := stringSet{}
	bucketIDs := stringSet{}
	skuIDs := stringSet{}
	for _, grant := range grants {
		if grant.ScopeProductID != "" {
			productIDs.add(grant.ScopeProductID)
		}
		if grant.ScopeBucketID != "" {
			bucketIDs.add(grant.ScopeBucketID)
		}
		if grant.ScopeSKUID != "" {
			skuIDs.add(grant.ScopeSKUID)
		}
	}
	productNames, err := c.lookupProductNames(ctx, productIDs.values())
	if err != nil {
		return nil, nil, nil, err
	}
	bucketCatalog, err := c.lookupBucketCatalog(ctx, bucketIDs.values())
	if err != nil {
		return nil, nil, nil, err
	}
	skuCatalog, err := c.lookupSKUCatalog(ctx, skuIDs.values())
	if err != nil {
		return nil, nil, nil, err
	}
	return productNames, bucketCatalog, skuCatalog, nil
}

// buildEntitlementsView is the pure grouping/ordering pass that the request
// handler and the funding-equivalence test both share. It must agree with the
// precedence in grant_funding_plan.go — see entitlements_view_funding_test.go
// for the contract.
func buildEntitlementsView(
	orgID OrgID,
	grants []GrantBalance,
	productNames map[string]string,
	bucketCatalog map[string]bucketCatalogRow,
	skuCatalog map[string]skuCatalogRow,
) EntitlementsView {
	view := EntitlementsView{OrgID: orgID}
	if len(grants) == 0 {
		return view
	}

	type poolKey struct {
		ScopeType GrantScopeType
		ProductID string
		BucketID  string
		SKUID     string
		Source    GrantSourceType
	}
	pools := map[poolKey]*EntitlementPool{}

	for _, grant := range grants {
		if grant.Available == 0 && grant.Pending == 0 {
			continue
		}
		key := poolKey{
			ScopeType: grant.ScopeType,
			ProductID: grant.ScopeProductID,
			BucketID:  grant.ScopeBucketID,
			SKUID:     grant.ScopeSKUID,
			Source:    grant.Source,
		}
		pool, ok := pools[key]
		if !ok {
			pool = &EntitlementPool{
				ScopeType:      grant.ScopeType,
				ProductID:      grant.ScopeProductID,
				ProductDisplay: productNames[grant.ScopeProductID],
				BucketID:       grant.ScopeBucketID,
				BucketDisplay:  bucketCatalog[grant.ScopeBucketID].DisplayName,
				SKUID:          grant.ScopeSKUID,
				Source:         grant.Source,
			}
			if sku, ok := skuCatalog[grant.ScopeSKUID]; ok {
				pool.SKUDisplay = sku.DisplayName
			}
			pool.CoverageLabel = coverageLabelForPool(*pool)
			pools[key] = pool
		}
		pool.Entries = append(pool.Entries, EntitlementGrantEntry{
			GrantID:     grant.GrantID.String(),
			Available:   grant.Available,
			Pending:     grant.Pending,
			StartsAt:    grant.StartsAt,
			PeriodStart: grant.PeriodStart,
			PeriodEnd:   grant.PeriodEnd,
			ExpiresAt:   grant.ExpiresAt,
		})
	}

	sourceRank := grantSourceRank()

	type productGroup struct {
		section EntitlementProductSection
		buckets map[string]*EntitlementBucketSection
	}
	productGroups := map[string]*productGroup{}

	for _, pool := range pools {
		// Entries are appended in expires-asc order because listGrantBalances
		// already orders the underlying rows that way; the funder uses the same
		// tiebreaker once scope and source agree.
		switch pool.ScopeType {
		case GrantScopeAccount:
			view.Universal = append(view.Universal, *pool)
		default:
			pid := pool.ProductID
			group, ok := productGroups[pid]
			if !ok {
				group = &productGroup{
					section: EntitlementProductSection{
						ProductID:   pid,
						DisplayName: productNames[pid],
					},
					buckets: map[string]*EntitlementBucketSection{},
				}
				if group.section.DisplayName == "" {
					group.section.DisplayName = pid
				}
				productGroups[pid] = group
			}
			if pool.ScopeType == GrantScopeProduct {
				group.section.ProductPools = append(group.section.ProductPools, *pool)
				continue
			}
			bucket, ok := group.buckets[pool.BucketID]
			if !ok {
				bucket = &EntitlementBucketSection{
					BucketID:    pool.BucketID,
					DisplayName: bucketCatalog[pool.BucketID].DisplayName,
				}
				if bucket.DisplayName == "" {
					bucket.DisplayName = pool.BucketID
				}
				group.buckets[pool.BucketID] = bucket
			}
			bucket.Pools = append(bucket.Pools, *pool)
		}
	}

	sort.SliceStable(view.Universal, func(i, j int) bool {
		return sourceRank[view.Universal[i].Source] < sourceRank[view.Universal[j].Source]
	})

	for _, group := range productGroups {
		sort.SliceStable(group.section.ProductPools, func(i, j int) bool {
			return sourceRank[group.section.ProductPools[i].Source] < sourceRank[group.section.ProductPools[j].Source]
		})
		bucketList := make([]EntitlementBucketSection, 0, len(group.buckets))
		for _, bucket := range group.buckets {
			sort.SliceStable(bucket.Pools, func(i, j int) bool {
				return poolLess(bucket.Pools[i], bucket.Pools[j], sourceRank)
			})
			bucketList = append(bucketList, *bucket)
		}
		sort.SliceStable(bucketList, func(i, j int) bool {
			a := bucketCatalog[bucketList[i].BucketID]
			b := bucketCatalog[bucketList[j].BucketID]
			if a.SortOrder != b.SortOrder {
				return a.SortOrder < b.SortOrder
			}
			return bucketList[i].BucketID < bucketList[j].BucketID
		})
		group.section.Buckets = bucketList
		view.Products = append(view.Products, group.section)
	}

	sort.SliceStable(view.Products, func(i, j int) bool {
		ai, aj := view.Products[i].DisplayName, view.Products[j].DisplayName
		if ai != aj {
			return ai < aj
		}
		return view.Products[i].ProductID < view.Products[j].ProductID
	})

	return view
}

func coverageLabelForPool(pool EntitlementPool) string {
	switch pool.ScopeType {
	case GrantScopeAccount:
		return coverageUniversal
	case GrantScopeProduct:
		name := pool.ProductDisplay
		if name == "" {
			name = pool.ProductID
		}
		return fmt.Sprintf(coverageProduct, name)
	case GrantScopeBucket:
		name := pool.BucketDisplay
		if name == "" {
			name = pool.BucketID
		}
		return fmt.Sprintf(coverageBucketAny, name)
	case GrantScopeSKU:
		if pool.SKUDisplay != "" {
			return pool.SKUDisplay
		}
		return pool.SKUID
	}
	return ""
}

// poolLess orders the pools inside a single bucket section. By construction
// only GrantScopeBucket ("Any X SKU") and GrantScopeSKU pools land here, so
// the ordering question is just "header row first, then per-SKU rows." This
// is intentionally distinct from GrantScopeFundingOrder (which puts SKU first
// because the funder consumes most-specific first); display order and
// consumption order serve different purposes and must not be conflated.
func poolLess(a, b EntitlementPool, sourceRank map[GrantSourceType]int) bool {
	if a.ScopeType != b.ScopeType {
		return a.ScopeType == GrantScopeBucket
	}
	if a.SKUDisplay != b.SKUDisplay {
		return a.SKUDisplay < b.SKUDisplay
	}
	if a.SKUID != b.SKUID {
		return a.SKUID < b.SKUID
	}
	return sourceRank[a.Source] < sourceRank[b.Source]
}

func grantSourceRank() map[GrantSourceType]int {
	rank := make(map[GrantSourceType]int, len(GrantSourceFundingOrder))
	for i, source := range GrantSourceFundingOrder {
		rank[source] = i
	}
	return rank
}

type bucketCatalogRow struct {
	DisplayName string
	SortOrder   int
}

type skuCatalogRow struct {
	DisplayName string
	BucketID    string
	ProductID   string
}

func (c *Client) lookupProductNames(ctx context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := c.pg.QueryContext(ctx, `
		SELECT product_id, display_name
		FROM products
		WHERE product_id = ANY($1)
	`, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("query product names: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("scan product name: %w", err)
		}
		out[id] = name
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate product names: %w", err)
	}
	return out, nil
}

func (c *Client) lookupBucketCatalog(ctx context.Context, ids []string) (map[string]bucketCatalogRow, error) {
	out := map[string]bucketCatalogRow{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := c.pg.QueryContext(ctx, `
		SELECT bucket_id, display_name, sort_order
		FROM credit_buckets
		WHERE bucket_id = ANY($1)
	`, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("query bucket catalog: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		var order int
		if err := rows.Scan(&id, &name, &order); err != nil {
			return nil, fmt.Errorf("scan bucket catalog: %w", err)
		}
		out[id] = bucketCatalogRow{DisplayName: name, SortOrder: order}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bucket catalog: %w", err)
	}
	return out, nil
}

func (c *Client) lookupSKUCatalog(ctx context.Context, ids []string) (map[string]skuCatalogRow, error) {
	out := map[string]skuCatalogRow{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := c.pg.QueryContext(ctx, `
		SELECT sku_id, display_name, bucket_id, product_id
		FROM skus
		WHERE sku_id = ANY($1)
	`, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("query sku catalog: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row skuCatalogRow
		var id string
		if err := rows.Scan(&id, &row.DisplayName, &row.BucketID, &row.ProductID); err != nil {
			return nil, fmt.Errorf("scan sku catalog: %w", err)
		}
		out[id] = row
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sku catalog: %w", err)
	}
	return out, nil
}

type stringSet map[string]struct{}

func (s stringSet) add(value string) {
	s[value] = struct{}{}
}

func (s stringSet) values() []string {
	out := make([]string, 0, len(s))
	for v := range s {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
