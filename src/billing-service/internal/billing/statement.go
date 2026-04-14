package billing

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"
)

type statementLineKey struct {
	ProductID    string
	PlanID       string
	BucketID     string
	SKUID        string
	PricingPhase string
	UnitRate     uint64
}

type statementGrantKey struct {
	ScopeType      string
	ScopeProductID string
	ScopeBucketID  string
	Source         string
}

func (c *Client) PreviewStatement(ctx context.Context, orgID OrgID, productID string) (Statement, error) {
	if productID == "" {
		return Statement{}, fmt.Errorf("product_id is required")
	}
	cycle, err := c.EnsureOpenBillingCycle(ctx, orgID, productID)
	if err != nil {
		return Statement{}, err
	}
	now, err := c.BusinessNow(ctx, c.queries, orgID, productID)
	if err != nil {
		return Statement{}, err
	}
	return c.statementForCycle(ctx, orgID, productID, cycle, now, true)
}

func (c *Client) statementForCycle(ctx context.Context, orgID OrgID, productID string, cycle billingCycle, generatedAt time.Time, includeGrantSummaries bool) (Statement, error) {
	windows, err := c.statementWindows(ctx, orgID, productID, cycle)
	if err != nil {
		return Statement{}, err
	}
	statement := Statement{OrgID: orgID, ProductID: productID, PeriodStart: cycle.StartsAt, PeriodEnd: cycle.EndsAt, PeriodSource: "billing_cycle", GeneratedAt: generatedAt.UTC(), Currency: cleanNonEmpty(cycle.Currency, "usd"), UnitLabel: "ledger_units"}
	lines := map[statementLineKey]*StatementLineItem{}
	summaries := map[statementGrantKey]*StatementGrantSummary{}
	if includeGrantSummaries {
		grants, err := c.ListGrantBalances(ctx, orgID, productID)
		if err != nil {
			return Statement{}, err
		}
		for _, grant := range grants {
			key := statementGrantKey{ScopeType: grant.ScopeType, ScopeProductID: grant.ScopeProductID, ScopeBucketID: grant.ScopeBucketID, Source: grant.Source}
			summary := summaries[key]
			if summary == nil {
				summary = &StatementGrantSummary{ScopeType: grant.ScopeType, ScopeProductID: grant.ScopeProductID, ScopeBucketID: grant.ScopeBucketID, Source: grant.Source}
				summaries[key] = summary
			}
			summary.Available += grant.Available
			summary.Pending += grant.Pending
		}
	}
	for _, window := range windows {
		switch window.State {
		case "settled":
			addSettledStatementWindow(&statement, lines, window)
		case "reserved", "active":
			addReservedStatementWindow(&statement, lines, window)
		}
	}
	statement.LineItems = sortedStatementLines(lines)
	if includeGrantSummaries {
		statement.GrantSummaries = sortedStatementSummaries(summaries)
	}
	return statement, nil
}

func (c *Client) statementWindows(ctx context.Context, orgID OrgID, productID string, cycle billingCycle) ([]persistedWindow, error) {
	rows, err := c.pg.Query(ctx, `
		SELECT window_id
		FROM billing_windows
		WHERE org_id = $1
		  AND product_id = $2
		  AND state IN ('reserved','active','settled')
		  AND window_start >= $3
		  AND window_start < $4
		ORDER BY window_start, window_seq, window_id
	`, orgIDText(orgID), productID, cycle.StartsAt, cycle.EndsAt)
	if err != nil {
		return nil, fmt.Errorf("query statement windows: %w", err)
	}
	defer rows.Close()
	out := []persistedWindow{}
	for rows.Next() {
		var windowID string
		if err := rows.Scan(&windowID); err != nil {
			return nil, fmt.Errorf("scan statement window: %w", err)
		}
		window, err := c.loadWindow(ctx, windowID)
		if err != nil {
			return nil, err
		}
		out = append(out, window)
	}
	return out, rows.Err()
}

func addSettledStatementWindow(statement *Statement, lines map[statementLineKey]*StatementLineItem, window persistedWindow) {
	statement.Totals.ChargeUnits += window.BilledChargeUnits
	statement.Totals.ReservedUnits += 0
	statement.Totals.TotalDueUnits += sourceTotal(window.FundingLegs, "receivable")
	statement.Totals.FreeTierUnits += sourceTotal(window.FundingLegs, "free_tier")
	statement.Totals.ContractUnits += sourceTotal(window.FundingLegs, "contract")
	statement.Totals.PurchaseUnits += sourceTotal(window.FundingLegs, "purchase")
	statement.Totals.PromoUnits += sourceTotal(window.FundingLegs, "promo")
	statement.Totals.RefundUnits += sourceTotal(window.FundingLegs, "refund")
	statement.Totals.ReceivableUnits += sourceTotal(window.FundingLegs, "receivable")
	for skuID, rate := range window.RateContext.SKURates {
		charge := uint64(window.BillableQuantity) * uint64FromFloatRate(window.Allocation[skuID], rate)
		if charge == 0 {
			continue
		}
		item := statementLine(lines, window, skuID, rate)
		item.Quantity += float64(window.BillableQuantity) * window.Allocation[skuID]
		item.ChargeUnits += charge
		item.FreeTierUnits += componentSourceTotal(window.FundingLegs, skuID, "free_tier")
		item.ContractUnits += componentSourceTotal(window.FundingLegs, skuID, "contract")
		item.PurchaseUnits += componentSourceTotal(window.FundingLegs, skuID, "purchase")
		item.PromoUnits += componentSourceTotal(window.FundingLegs, skuID, "promo")
		item.RefundUnits += componentSourceTotal(window.FundingLegs, skuID, "refund")
		item.ReceivableUnits += componentSourceTotal(window.FundingLegs, skuID, "receivable")
	}
}

func addReservedStatementWindow(statement *Statement, lines map[statementLineKey]*StatementLineItem, window persistedWindow) {
	for _, leg := range window.FundingLegs {
		statement.Totals.ReservedUnits += leg.Amount
		if leg.ComponentSKUID == "" {
			continue
		}
		item := statementLine(lines, window, leg.ComponentSKUID, window.RateContext.SKURates[leg.ComponentSKUID])
		item.ReservedUnits += leg.Amount
	}
}

func statementLine(lines map[statementLineKey]*StatementLineItem, window persistedWindow, skuID string, rate uint64) *StatementLineItem {
	bucketID := window.RateContext.SKUBuckets[skuID]
	key := statementLineKey{ProductID: window.ProductID, PlanID: window.PricingPlanID, BucketID: bucketID, SKUID: skuID, PricingPhase: window.PricingPhase, UnitRate: rate}
	item := lines[key]
	if item == nil {
		quantityUnit := window.RateContext.SKUQuantityUnits[skuID]
		if quantityUnit == "" {
			quantityUnit = "unit"
		}
		item = &StatementLineItem{ProductID: window.ProductID, PlanID: window.PricingPlanID, BucketID: bucketID, BucketDisplayName: window.RateContext.BucketDisplayNames[bucketID], SKUID: skuID, SKUDisplayName: window.RateContext.SKUDisplayNames[skuID], QuantityUnit: quantityUnit, PricingPhase: window.PricingPhase, UnitRate: rate}
		lines[key] = item
	}
	return item
}

func sortedStatementLines(lines map[statementLineKey]*StatementLineItem) []StatementLineItem {
	keys := make([]statementLineKey, 0, len(lines))
	for key := range lines {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].BucketID != keys[j].BucketID {
			return keys[i].BucketID < keys[j].BucketID
		}
		return keys[i].SKUID < keys[j].SKUID
	})
	out := make([]StatementLineItem, 0, len(keys))
	for _, key := range keys {
		out = append(out, *lines[key])
	}
	return out
}

func sortedStatementSummaries(summaries map[statementGrantKey]*StatementGrantSummary) []StatementGrantSummary {
	keys := make([]statementGrantKey, 0, len(summaries))
	for key := range summaries {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if sourcePriority(keys[i].Source) != sourcePriority(keys[j].Source) {
			return sourcePriority(keys[i].Source) < sourcePriority(keys[j].Source)
		}
		if keys[i].ScopeType != keys[j].ScopeType {
			return keys[i].ScopeType < keys[j].ScopeType
		}
		return keys[i].ScopeBucketID < keys[j].ScopeBucketID
	})
	out := make([]StatementGrantSummary, 0, len(keys))
	for _, key := range keys {
		out = append(out, *summaries[key])
	}
	return out
}

func sourceTotal(legs []fundingLeg, source string) uint64 {
	var out uint64
	for _, leg := range legs {
		if leg.Source == source {
			out += leg.Amount
		}
	}
	return out
}

func componentSourceTotal(legs []fundingLeg, skuID string, source string) uint64 {
	var out uint64
	for _, leg := range legs {
		if leg.Source == source && leg.ComponentSKUID == skuID {
			out += leg.Amount
		}
	}
	return out
}

func uint64FromFloatRate(units float64, rate uint64) uint64 {
	return uint64(math.Ceil(units * float64(rate)))
}
