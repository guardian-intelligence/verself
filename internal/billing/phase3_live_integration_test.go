//go:build integration

package billing

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/oklog/ulid/v2"
)

func TestDepositCreditsAgainstLiveHost(t *testing.T) {
	t.Parallel()

	env := newLivePhase1Env(t)
	orgID := OrgID(7_300_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))
	productID, planID := uniqueCatalogIDs("live-deposit")

	subID := ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID, map[string]uint64{"unit": 1}, nil, false)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	periodStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	err := env.client.DepositCredits(ctx, nil, CreditGrant{
		OrgID:          orgID,
		ProductID:      productID,
		Amount:         1000,
		Source:         "subscription",
		SubscriptionID: &subID,
		PeriodStart:    &periodStart,
		PeriodEnd:      &periodEnd,
	})
	if err != nil {
		t.Fatalf("deposit credits: %v", err)
	}

	// Verify PG grant row.
	var grantIDStr string
	var amount int64
	if err := env.pg.QueryRowContext(ctx, `
		SELECT grant_id, amount
		FROM credit_grants
		WHERE org_id = $1 AND product_id = $2 AND source = 'subscription'
	`, strconv.FormatUint(uint64(orgID), 10), productID).Scan(&grantIDStr, &amount); err != nil {
		t.Fatalf("query grant row: %v", err)
	}
	if amount != 1000 {
		t.Fatalf("expected amount 1000, got %d", amount)
	}

	// Verify TB balance.
	parsedULID, err := ulid.ParseStrict(grantIDStr)
	if err != nil {
		t.Fatalf("parse grant ULID: %v", err)
	}
	requireGrantBalance(t, env.tbClient, GrantID(parsedULID), 1000, 0, 0)

	// Verify idempotency — second call should be no-op.
	err = env.client.DepositCredits(ctx, nil, CreditGrant{
		OrgID:          orgID,
		ProductID:      productID,
		Amount:         1000,
		Source:         "subscription",
		SubscriptionID: &subID,
		PeriodStart:    &periodStart,
		PeriodEnd:      &periodEnd,
	})
	if err != nil {
		t.Fatalf("idempotent deposit: %v", err)
	}

	// Still only one grant row.
	var count int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM credit_grants
		WHERE org_id = $1 AND product_id = $2
	`, strconv.FormatUint(uint64(orgID), 10), productID).Scan(&count); err != nil {
		t.Fatalf("count grants: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 grant, got %d", count)
	}

	t.Logf("verified live deposit_credits org_id=%d product_id=%s grant_id=%s", orgID, productID, grantIDStr)
}

func TestExpireCreditsAgainstLiveHost(t *testing.T) {
	t.Parallel()

	env := newLivePhase1Env(t)
	orgID := OrgID(7_400_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))
	productID, planID := uniqueCatalogIDs("live-expire")

	ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID, map[string]uint64{"unit": 1}, nil, false)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Seed an expired grant.
	past := time.Now().UTC().Add(-1 * time.Hour)
	grant := seedGrantWithExpiry(t, env.client, env.pg, env.tbClient, orgID, productID, SourceSubscription, 500, &past)

	result, err := env.client.ExpireCredits(ctx)
	if err != nil {
		t.Fatalf("expire credits: %v", err)
	}
	// ExpireCredits is a global sweep — other expired grants may exist on the
	// shared live DB. Assert >= 1 on the aggregate, then assert exact state on
	// the specific grant we seeded.
	if result.GrantsExpired < 1 {
		t.Fatalf("expected at least 1 expired in global sweep, got %d", result.GrantsExpired)
	}
	if result.GrantsFailed != 0 {
		t.Fatalf("expected 0 failures, got %d: %v", result.GrantsFailed, result.Errors)
	}

	// Assert exact state on our specific grant — these are not affected by
	// other parallel tests because they're scoped to this grant_id.
	grantIDStr := ulid.ULID(grant.grantID).String()

	var closedAt sql.NullTime
	if err := env.pg.QueryRowContext(ctx, `
		SELECT closed_at FROM credit_grants WHERE grant_id = $1
	`, grantIDStr).Scan(&closedAt); err != nil {
		t.Fatalf("query closed_at: %v", err)
	}
	if !closedAt.Valid {
		t.Fatal("expected closed_at to be set for our grant")
	}

	// TB balance must be exactly drained: 0 available, 0 pending, 500 consumed.
	requireGrantBalance(t, env.tbClient, grant.grantID, 0, 0, 500)

	// Verify exactly one billing event for our specific grant.
	var eventCount int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM billing_events
		WHERE event_type = 'credits_expired' AND grant_id = $1
	`, grantIDStr).Scan(&eventCount); err != nil {
		t.Fatalf("count expiry billing events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected exactly 1 credits_expired event for grant %s, got %d", grantIDStr, eventCount)
	}

	t.Logf("verified live expire_credits org_id=%d grant_id=%s expired=%d units=%d",
		orgID, grantIDStr, result.GrantsExpired, result.UnitsExpired)
}

func TestRecordLicensedChargeAgainstLiveHost(t *testing.T) {
	t.Parallel()

	env := newLivePhase1Env(t)
	orgID := OrgID(7_500_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))
	productID := fmt.Sprintf("licensed-live-%d", time.Now().UnixNano())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := env.client.EnsureOrg(ctx, orgID, "licensed-live-org"); err != nil {
		t.Fatalf("ensure org: %v", err)
	}

	if _, err := env.pg.ExecContext(ctx, `
		INSERT INTO products (product_id, display_name, meter_unit, billing_model)
		VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING
	`, productID, "Licensed Live Product", "unit", "licensed"); err != nil {
		t.Fatalf("insert product: %v", err)
	}

	// Record Revenue balance before the charge.
	revenueBefore := lookupOperatorPostedCredits(t, env.tbClient, AcctRevenue)

	taskID := TaskID(time.Now().UTC().UnixNano() % 1_000_000_000)
	invoiceID := fmt.Sprintf("in_test_licensed_live_%d", time.Now().UnixNano())
	err := env.client.RecordLicensedCharge(ctx, taskID, LicensedCharge{
		OrgID:           orgID,
		ProductID:       productID,
		SubscriptionID:  1001,
		StripeInvoiceID: invoiceID,
		Amount:          5000,
		PeriodStart:     time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:       time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("record licensed charge: %v", err)
	}

	// Verify TigerBeetle: Revenue account must have increased by exactly 5000.
	revenueAfter := lookupOperatorPostedCredits(t, env.tbClient, AcctRevenue)
	if revenueAfter-revenueBefore != 5000 {
		t.Fatalf("expected Revenue to increase by exactly 5000, got %d (before=%d after=%d)",
			revenueAfter-revenueBefore, revenueBefore, revenueAfter)
	}

	// Verify no credit_grants row was created (licensed products don't create grants).
	orgIDStr := strconv.FormatUint(uint64(orgID), 10)
	var grantCount int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM credit_grants WHERE org_id = $1 AND product_id = $2
	`, orgIDStr, productID).Scan(&grantCount); err != nil {
		t.Fatalf("count grants: %v", err)
	}
	if grantCount != 0 {
		t.Fatalf("expected 0 grant rows for licensed product, got %d", grantCount)
	}

	// Verify exactly one billing event with correct fields.
	var eventCount int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM billing_events
		WHERE org_id = $1 AND event_type = 'licensed_charge_recorded'
	`, orgIDStr).Scan(&eventCount); err != nil {
		t.Fatalf("count billing events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected exactly 1 licensed_charge_recorded event, got %d", eventCount)
	}

	t.Logf("verified live record_licensed_charge org_id=%d product_id=%s task_id=%d revenue_delta=5000", orgID, productID, taskID)
}

// TestSettleMeteringRowAgainstLiveHost verifies that Settle writes a metering
// row to ClickHouse when a MeteringWriter is configured.
func TestSettleMeteringRowAgainstLiveHost(t *testing.T) {
	t.Parallel()

	chAddr := os.Getenv("FORGE_METAL_BILLING_LIVE_CH_ADDR")
	chPassword := os.Getenv("FORGE_METAL_BILLING_LIVE_CH_PASSWORD")
	if chAddr == "" || chPassword == "" {
		t.Skip("set FORGE_METAL_BILLING_LIVE_CH_ADDR and FORGE_METAL_BILLING_LIVE_CH_PASSWORD")
	}

	chConn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{chAddr},
		Auth: clickhouse.Auth{
			Database: "forge_metal",
			Username: "default",
			Password: chPassword,
		},
	})
	if err != nil {
		t.Fatalf("open clickhouse: %v", err)
	}
	t.Cleanup(func() { _ = chConn.Close() })

	env := newLivePhase1EnvWithMetering(t, NewClickHouseMeteringWriter(chConn, "forge_metal"), noopMeteringQuerier{})
	env.client.cfg.ReservationWindowSecs = 60
	env.client.cfg.PendingTimeoutSecs = 600

	orgID := OrgID(7_600_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))
	productID, planID := uniqueCatalogIDs("live-metering")

	ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID, map[string]uint64{"unit": 1}, nil, false)
	seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceSubscription, 200)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	jobID := JobID(time.Now().UTC().UnixNano() % 1_000_000_000)
	reservation, err := env.client.Reserve(ctx, ReserveRequest{
		JobID:      jobID,
		OrgID:      orgID,
		ProductID:  productID,
		ActorID:    "user-live-metering",
		Allocation: map[string]float64{"unit": 1},
		SourceType: "job",
		SourceRef:  strconv.FormatInt(int64(jobID), 10),
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}

	if err := env.client.Settle(ctx, &reservation, 30); err != nil {
		t.Fatalf("settle: %v", err)
	}

	// Verify metering row in ClickHouse.
	orgIDStr := strconv.FormatUint(uint64(orgID), 10)
	sourceRef := strconv.FormatInt(int64(jobID), 10)

	var rowCount uint64
	if err := chConn.QueryRow(ctx, `
		SELECT count()
		FROM forge_metal.metering
		WHERE org_id = $1 AND product_id = $2 AND source_ref = $3
	`, orgIDStr, productID, sourceRef).Scan(&rowCount); err != nil {
		t.Fatalf("query metering rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("expected 1 metering row, got %d", rowCount)
	}

	var (
		mOrgID        string
		mActorID      string
		mProductID    string
		mSourceType   string
		mPricingPhase string
		mChargeUnits  uint64
		mBilledSecs   uint32
		mSubUnits     uint64
	)
	if err := chConn.QueryRow(ctx, `
		SELECT org_id, actor_id, product_id, source_type, pricing_phase,
		       charge_units, billed_seconds, subscription_units
		FROM forge_metal.metering
		WHERE org_id = $1 AND product_id = $2 AND source_ref = $3
		LIMIT 1
	`, orgIDStr, productID, sourceRef).Scan(
		&mOrgID, &mActorID, &mProductID, &mSourceType, &mPricingPhase,
		&mChargeUnits, &mBilledSecs, &mSubUnits,
	); err != nil {
		t.Fatalf("scan metering row: %v", err)
	}

	if mOrgID != orgIDStr {
		t.Fatalf("metering org_id: expected %s, got %s", orgIDStr, mOrgID)
	}
	if mActorID != "user-live-metering" {
		t.Fatalf("metering actor_id: expected user-live-metering, got %s", mActorID)
	}
	if mProductID != productID {
		t.Fatalf("metering product_id: expected %s, got %s", productID, mProductID)
	}
	if mSourceType != "job" {
		t.Fatalf("metering source_type: expected job, got %s", mSourceType)
	}
	if mPricingPhase != "included" {
		t.Fatalf("metering pricing_phase: expected included, got %s", mPricingPhase)
	}
	if mChargeUnits != 30 {
		t.Fatalf("metering charge_units: expected 30, got %d", mChargeUnits)
	}
	if mBilledSecs != 30 {
		t.Fatalf("metering billed_seconds: expected 30, got %d", mBilledSecs)
	}
	if mSubUnits != 30 {
		t.Fatalf("metering subscription_units: expected 30, got %d", mSubUnits)
	}

	t.Logf("verified live metering row: org_id=%s product_id=%s source_ref=%s charge_units=%d pricing_phase=%s",
		mOrgID, mProductID, sourceRef, mChargeUnits, mPricingPhase)
}
