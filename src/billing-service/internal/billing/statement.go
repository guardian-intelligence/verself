package billing

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"
)

type statementPeriod struct {
	Start  time.Time
	End    time.Time
	Source string
}

type statementLineItemKey struct {
	ProductID    string
	PlanID       string
	BucketID     string
	SKUID        string
	PricingPhase string
	UnitRate     uint64
}

type statementGrantSummaryKey struct {
	ScopeType      GrantScopeType
	ScopeProductID string
	ScopeBucketID  string
	Source         GrantSourceType
}

func (c *Client) PreviewStatement(ctx context.Context, orgID OrgID, productID string) (Statement, error) {
	if err := ctx.Err(); err != nil {
		return Statement{}, err
	}
	if productID == "" {
		return Statement{}, fmt.Errorf("product_id is required")
	}

	period, err := c.currentStatementPeriod(ctx, orgID, productID)
	if err != nil {
		return Statement{}, err
	}
	grants, err := c.ListGrantBalances(ctx, orgID, productID)
	if err != nil {
		return Statement{}, err
	}
	windows, err := c.statementWindows(ctx, orgID, productID, period)
	if err != nil {
		return Statement{}, err
	}
	return buildStatement(orgID, productID, period, grants, windows, c.clock().UTC())
}

func (c *Client) currentStatementPeriod(ctx context.Context, orgID OrgID, productID string) (statementPeriod, error) {
	cycle, err := c.EnsureOpenBillingCycle(ctx, orgID, productID)
	if err != nil {
		return statementPeriod{}, err
	}
	if cycle.EndsAt.After(cycle.StartsAt) {
		return statementPeriod{Start: cycle.StartsAt.UTC(), End: cycle.EndsAt.UTC(), Source: "billing_cycle"}, nil
	}
	return statementPeriod{}, fmt.Errorf("open billing cycle %s has invalid interval", cycle.CycleID)
}

func (c *Client) statementWindows(ctx context.Context, orgID OrgID, productID string, period statementPeriod) ([]persistedWindow, error) {
	rows, err := c.pg.QueryContext(ctx, `
		SELECT window_id
		FROM billing_windows
		WHERE org_id = $1
		  AND product_id = $2
		  AND state IN ('reserved', 'settled')
		  AND window_start >= $3
		  AND window_start < $4
		ORDER BY window_start ASC, window_seq ASC, window_id ASC
	`, strconv.FormatUint(uint64(orgID), 10), productID, period.Start, period.End)
	if err != nil {
		return nil, fmt.Errorf("query statement windows: %w", err)
	}
	defer rows.Close()

	var windowIDs []string
	for rows.Next() {
		var windowID string
		if err := rows.Scan(&windowID); err != nil {
			return nil, fmt.Errorf("scan statement window id: %w", err)
		}
		windowIDs = append(windowIDs, windowID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate statement windows: %w", err)
	}

	windows := make([]persistedWindow, 0, len(windowIDs))
	for _, windowID := range windowIDs {
		window, err := c.loadPersistedWindow(ctx, windowID)
		if err != nil {
			return nil, fmt.Errorf("load statement window %s: %w", windowID, err)
		}
		windows = append(windows, window)
	}
	return windows, nil
}

func buildStatement(
	orgID OrgID,
	productID string,
	period statementPeriod,
	grants []GrantBalance,
	windows []persistedWindow,
	generatedAt time.Time,
) (Statement, error) {
	statement := Statement{
		OrgID:        orgID,
		ProductID:    productID,
		PeriodStart:  period.Start.UTC(),
		PeriodEnd:    period.End.UTC(),
		PeriodSource: period.Source,
		GeneratedAt:  generatedAt.UTC(),
		Currency:     "usd",
		UnitLabel:    "ledger_units",
	}
	lineItems := map[statementLineItemKey]*StatementLineItem{}
	grantSummaries := map[statementGrantSummaryKey]*StatementGrantSummary{}

	for _, grant := range grants {
		if err := addStatementGrantSummary(grantSummaries, grant); err != nil {
			return Statement{}, err
		}
	}

	for _, window := range windows {
		switch window.State {
		case "settled":
			row, err := buildMeteringRow(window)
			if err != nil {
				return Statement{}, fmt.Errorf("statement metering row %s: %w", window.WindowID, err)
			}
			if err := addSettledStatementRow(&statement, lineItems, window, row); err != nil {
				return Statement{}, fmt.Errorf("statement aggregate window %s: %w", window.WindowID, err)
			}
		case "reserved":
			if !window.ExpiresAt.After(generatedAt) {
				continue
			}
			if err := addReservedStatementWindow(&statement, lineItems, window); err != nil {
				return Statement{}, fmt.Errorf("statement reserved window %s: %w", window.WindowID, err)
			}
		}
	}

	statement.LineItems = sortedStatementLineItems(lineItems)
	statement.GrantSummaries = sortedStatementGrantSummaries(grantSummaries)
	return statement, nil
}

func addSettledStatementRow(
	statement *Statement,
	lineItems map[statementLineItemKey]*StatementLineItem,
	window persistedWindow,
	row MeteringRow,
) error {
	var err error
	statement.Totals.ChargeUnits, err = safeAddUint64(statement.Totals.ChargeUnits, row.ChargeUnits)
	if err != nil {
		return err
	}
	if err := addStatementAppliedTotals(&statement.Totals, row); err != nil {
		return err
	}

	rateContext, err := completeRateContext(window)
	if err != nil {
		return err
	}

	quantities := row.ComponentQuantities
	for _, skuID := range sortedUint64MapKeys(row.ComponentChargeUnits) {
		sku, ok := rateContext.SKUDetails[skuID]
		if !ok || sku.DisplayName == "" || sku.BucketID == "" || sku.BucketDisplayName == "" || sku.QuantityUnit == "" {
			return fmt.Errorf("sku metadata missing for %s", skuID)
		}
		unitRate := rateContext.SKURates[skuID]
		pricingPhase := string(window.PricingPhase)
		key := statementLineItemKey{
			ProductID:    window.ProductID,
			PlanID:       window.PlanID,
			BucketID:     sku.BucketID,
			SKUID:        skuID,
			PricingPhase: pricingPhase,
			UnitRate:     unitRate,
		}
		item := lineItems[key]
		if item == nil {
			item = &StatementLineItem{
				ProductID:         key.ProductID,
				PlanID:            key.PlanID,
				BucketID:          key.BucketID,
				BucketDisplayName: sku.BucketDisplayName,
				SKUID:             key.SKUID,
				SKUDisplayName:    sku.DisplayName,
				QuantityUnit:      sku.QuantityUnit,
				PricingPhase:      key.PricingPhase,
				UnitRate:          key.UnitRate,
			}
			lineItems[key] = item
		}
		item.Quantity += quantities[skuID]
		item.ChargeUnits, err = safeAddUint64(item.ChargeUnits, row.ComponentChargeUnits[skuID])
		if err != nil {
			return err
		}
		item.FreeTierUnits, err = safeAddUint64(item.FreeTierUnits, row.ComponentFreeTierUnits[skuID])
		if err != nil {
			return err
		}
		item.ContractUnits, err = safeAddUint64(item.ContractUnits, row.ComponentContractUnits[skuID])
		if err != nil {
			return err
		}
		item.PurchaseUnits, err = safeAddUint64(item.PurchaseUnits, row.ComponentPurchaseUnits[skuID])
		if err != nil {
			return err
		}
		item.PromoUnits, err = safeAddUint64(item.PromoUnits, row.ComponentPromoUnits[skuID])
		if err != nil {
			return err
		}
		item.RefundUnits, err = safeAddUint64(item.RefundUnits, row.ComponentRefundUnits[skuID])
		if err != nil {
			return err
		}
		item.ReceivableUnits, err = safeAddUint64(item.ReceivableUnits, row.ComponentReceivableUnits[skuID])
		if err != nil {
			return err
		}
	}
	return nil
}

func addReservedStatementWindow(
	statement *Statement,
	lineItems map[statementLineItemKey]*StatementLineItem,
	window persistedWindow,
) error {
	rateContext, err := completeRateContext(window)
	if err != nil {
		return err
	}
	// The legs are sized to the reservation quantity; aggregate by ChargeSKUID
	// so each reserved line can credit the right SKU row. Legs without a
	// ChargeSKUID (legacy pre-tightened reserve path) are silently dropped from
	// per-line reserved totals but still credited to the statement-level total
	// so the customer's top-line reserved figure remains honest.
	for _, leg := range window.FundingLegs {
		statement.Totals.ReservedUnits, err = safeAddUint64(statement.Totals.ReservedUnits, leg.Amount)
		if err != nil {
			return err
		}
		if leg.ChargeSKUID == "" {
			continue
		}
		sku, ok := rateContext.SKUDetails[leg.ChargeSKUID]
		if !ok || sku.DisplayName == "" || sku.BucketID == "" || sku.BucketDisplayName == "" || sku.QuantityUnit == "" {
			return fmt.Errorf("sku metadata missing for reserved leg %s", leg.ChargeSKUID)
		}
		unitRate := rateContext.SKURates[leg.ChargeSKUID]
		key := statementLineItemKey{
			ProductID:    window.ProductID,
			PlanID:       window.PlanID,
			BucketID:     sku.BucketID,
			SKUID:        leg.ChargeSKUID,
			PricingPhase: string(window.PricingPhase),
			UnitRate:     unitRate,
		}
		item := lineItems[key]
		if item == nil {
			item = &StatementLineItem{
				ProductID:         key.ProductID,
				PlanID:            key.PlanID,
				BucketID:          key.BucketID,
				BucketDisplayName: sku.BucketDisplayName,
				SKUID:             key.SKUID,
				SKUDisplayName:    sku.DisplayName,
				QuantityUnit:      sku.QuantityUnit,
				PricingPhase:      key.PricingPhase,
				UnitRate:          key.UnitRate,
			}
			lineItems[key] = item
		}
		item.ReservedUnits, err = safeAddUint64(item.ReservedUnits, leg.Amount)
		if err != nil {
			return err
		}
	}
	return nil
}

func addStatementAppliedTotals(totals *StatementTotals, row MeteringRow) error {
	var err error
	totals.FreeTierUnits, err = safeAddUint64(totals.FreeTierUnits, row.FreeTierUnits)
	if err != nil {
		return err
	}
	totals.ContractUnits, err = safeAddUint64(totals.ContractUnits, row.ContractUnits)
	if err != nil {
		return err
	}
	totals.PurchaseUnits, err = safeAddUint64(totals.PurchaseUnits, row.PurchaseUnits)
	if err != nil {
		return err
	}
	totals.PromoUnits, err = safeAddUint64(totals.PromoUnits, row.PromoUnits)
	if err != nil {
		return err
	}
	totals.RefundUnits, err = safeAddUint64(totals.RefundUnits, row.RefundUnits)
	if err != nil {
		return err
	}
	totals.ReceivableUnits, err = safeAddUint64(totals.ReceivableUnits, row.ReceivableUnits)
	if err != nil {
		return err
	}
	totals.TotalDueUnits = totals.ReceivableUnits
	return nil
}

func addStatementGrantSummary(summaries map[statementGrantSummaryKey]*StatementGrantSummary, grant GrantBalance) error {
	key := statementGrantSummaryKey{
		ScopeType:      grant.ScopeType,
		ScopeProductID: grant.ScopeProductID,
		ScopeBucketID:  grant.ScopeBucketID,
		Source:         grant.Source,
	}
	summary := summaries[key]
	if summary == nil {
		summary = &StatementGrantSummary{
			ScopeType:      grant.ScopeType,
			ScopeProductID: grant.ScopeProductID,
			ScopeBucketID:  grant.ScopeBucketID,
			Source:         grant.Source,
		}
		summaries[key] = summary
	}
	var err error
	summary.Available, err = safeAddUint64(summary.Available, grant.Available)
	if err != nil {
		return err
	}
	summary.Pending, err = safeAddUint64(summary.Pending, grant.Pending)
	return err
}

func statementLineItemID(key statementLineItemKey) string {
	return key.ProductID + ":" + key.PlanID + ":" + key.BucketID + ":" + key.SKUID + ":" + key.PricingPhase + ":" + strconv.FormatUint(key.UnitRate, 10)
}

func sortedStatementLineItems(items map[statementLineItemKey]*StatementLineItem) []StatementLineItem {
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(items))
	byID := make(map[string]*StatementLineItem, len(items))
	for key, item := range items {
		id := statementLineItemID(key)
		keys = append(keys, id)
		byID[id] = item
	}
	sort.Strings(keys)
	out := make([]StatementLineItem, 0, len(items))
	for _, key := range keys {
		out = append(out, *byID[key])
	}
	return out
}

func sortedStatementGrantSummaries(summaries map[statementGrantSummaryKey]*StatementGrantSummary) []StatementGrantSummary {
	if len(summaries) == 0 {
		return nil
	}
	keys := make([]string, 0, len(summaries))
	byID := make(map[string]*StatementGrantSummary, len(summaries))
	for key, summary := range summaries {
		id := key.ScopeType.String() + ":" + key.ScopeProductID + ":" + key.ScopeBucketID + ":" + key.Source.String()
		keys = append(keys, id)
		byID[id] = summary
	}
	sort.Strings(keys)
	out := make([]StatementGrantSummary, 0, len(summaries))
	for _, key := range keys {
		out = append(out, *byID[key])
	}
	return out
}
