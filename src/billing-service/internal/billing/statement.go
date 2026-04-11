package billing

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const fallbackStatementPeriodDays = 30

type statementPeriod struct {
	Start  time.Time
	End    time.Time
	Source string
}

type statementLineItemKey struct {
	ProductID    string
	PlanID       string
	BucketID     string
	ComponentID  string
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
	var start sql.NullTime
	var end sql.NullTime
	err := c.pg.QueryRowContext(ctx, `
		SELECT current_period_start, current_period_end
		FROM subscriptions
		WHERE org_id = $1
		  AND product_id = $2
		  AND status <> 'canceled'
		ORDER BY current_period_end DESC NULLS LAST, subscription_id DESC
		LIMIT 1
	`, strconv.FormatUint(uint64(orgID), 10), productID).Scan(&start, &end)
	if err != nil && err != sql.ErrNoRows {
		return statementPeriod{}, fmt.Errorf("load statement period: %w", err)
	}
	if start.Valid && end.Valid && end.Time.After(start.Time) {
		return statementPeriod{Start: start.Time.UTC(), End: end.Time.UTC(), Source: "subscription"}, nil
	}

	now := c.clock().UTC()
	return statementPeriod{Start: now.AddDate(0, 0, -fallbackStatementPeriodDays), End: now, Source: "rolling_30d"}, nil
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
	buckets := map[string]*StatementBucketSummary{}
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
			if err := addSettledStatementRow(&statement, buckets, lineItems, window, row); err != nil {
				return Statement{}, fmt.Errorf("statement aggregate window %s: %w", window.WindowID, err)
			}
		case "reserved":
			if !window.ExpiresAt.After(generatedAt) {
				continue
			}
			if err := addReservedStatementWindow(&statement, buckets, window); err != nil {
				return Statement{}, fmt.Errorf("statement reserved window %s: %w", window.WindowID, err)
			}
		}
	}

	statement.LineItems = sortedStatementLineItems(lineItems)
	statement.BucketSummaries = sortedStatementBuckets(buckets)
	statement.GrantSummaries = sortedStatementGrantSummaries(grantSummaries)
	return statement, nil
}

func addSettledStatementRow(
	statement *Statement,
	buckets map[string]*StatementBucketSummary,
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

	for _, bucketID := range sortedUint64MapKeys(row.BucketChargeUnits) {
		bucket := statementBucket(buckets, window.ProductID, bucketID)
		if err := addStatementBucketMetering(bucket, row, bucketID); err != nil {
			return err
		}
	}

	rateContext, err := completeRateContext(window)
	if err != nil {
		return err
	}
	quantities := row.ComponentQuantities
	for _, componentID := range sortedUint64MapKeys(row.ComponentChargeUnits) {
		unitRate := rateContext.UnitRates[componentID]
		if unitRate == 0 {
			unitRate = rateContext.ComponentCostPerUnit[componentID]
		}
		bucketID := bucketForDimension(window.ProductID, componentID, rateContext.RateBuckets)
		pricingPhase := string(window.PricingPhase)
		key := statementLineItemKey{
			ProductID:    window.ProductID,
			PlanID:       window.PlanID,
			BucketID:     bucketID,
			ComponentID:  componentID,
			PricingPhase: pricingPhase,
			UnitRate:     unitRate,
		}
		item := lineItems[key]
		if item == nil {
			item = &StatementLineItem{
				ProductID:    key.ProductID,
				PlanID:       key.PlanID,
				BucketID:     key.BucketID,
				ComponentID:  key.ComponentID,
				PricingPhase: key.PricingPhase,
				Description:  statementLineDescription(componentID, bucketID),
				UnitRate:     key.UnitRate,
			}
			lineItems[key] = item
		}
		item.Quantity += quantities[componentID]
		item.ChargeUnits, err = safeAddUint64(item.ChargeUnits, row.ComponentChargeUnits[componentID])
		if err != nil {
			return err
		}
	}
	return nil
}

func addReservedStatementWindow(statement *Statement, buckets map[string]*StatementBucketSummary, window persistedWindow) error {
	for _, leg := range window.FundingLegs {
		var err error
		statement.Totals.ReservedUnits, err = safeAddUint64(statement.Totals.ReservedUnits, leg.Amount)
		if err != nil {
			return err
		}
		bucket := statementBucket(buckets, window.ProductID, leg.ChargeBucketID)
		bucket.ReservedUnits, err = safeAddUint64(bucket.ReservedUnits, leg.Amount)
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
	totals.SubscriptionUnits, err = safeAddUint64(totals.SubscriptionUnits, row.SubscriptionUnits)
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

func addStatementBucketMetering(bucket *StatementBucketSummary, row MeteringRow, bucketID string) error {
	var err error
	bucket.ChargeUnits, err = safeAddUint64(bucket.ChargeUnits, row.BucketChargeUnits[bucketID])
	if err != nil {
		return err
	}
	bucket.FreeTierUnits, err = safeAddUint64(bucket.FreeTierUnits, row.BucketFreeTierUnits[bucketID])
	if err != nil {
		return err
	}
	bucket.SubscriptionUnits, err = safeAddUint64(bucket.SubscriptionUnits, row.BucketSubscriptionUnits[bucketID])
	if err != nil {
		return err
	}
	bucket.PurchaseUnits, err = safeAddUint64(bucket.PurchaseUnits, row.BucketPurchaseUnits[bucketID])
	if err != nil {
		return err
	}
	bucket.PromoUnits, err = safeAddUint64(bucket.PromoUnits, row.BucketPromoUnits[bucketID])
	if err != nil {
		return err
	}
	bucket.RefundUnits, err = safeAddUint64(bucket.RefundUnits, row.BucketRefundUnits[bucketID])
	if err != nil {
		return err
	}
	bucket.ReceivableUnits, err = safeAddUint64(bucket.ReceivableUnits, row.BucketReceivableUnits[bucketID])
	return err
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

func statementBucket(buckets map[string]*StatementBucketSummary, productID string, bucketID string) *StatementBucketSummary {
	bucket := buckets[bucketID]
	if bucket == nil {
		bucket = &StatementBucketSummary{ProductID: productID, BucketID: bucketID}
		buckets[bucketID] = bucket
	}
	return bucket
}

func statementLineDescription(componentID string, bucketID string) string {
	label := strings.ReplaceAll(componentID, "_", " ")
	if bucketID == "" {
		return label
	}
	return label + " (" + bucketID + ")"
}

func statementLineItemID(key statementLineItemKey) string {
	return key.ProductID + ":" + key.PlanID + ":" + key.BucketID + ":" + key.ComponentID + ":" + key.PricingPhase + ":" + strconv.FormatUint(key.UnitRate, 10)
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

func sortedStatementBuckets(buckets map[string]*StatementBucketSummary) []StatementBucketSummary {
	if len(buckets) == 0 {
		return nil
	}
	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]StatementBucketSummary, 0, len(buckets))
	for _, key := range keys {
		out = append(out, *buckets[key])
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
