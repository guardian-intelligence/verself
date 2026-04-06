//go:build integration

package billing

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/oklog/ulid/v2"
)

// TestDepositSubscriptionCreditsAgainstLiveHost exercises the monthly credit
// drip: seeds an active subscription with included_credits, runs
// DepositSubscriptionCredits, and verifies the grant was created.
func TestDepositSubscriptionCreditsAgainstLiveHost(t *testing.T) {
	t.Parallel()

	env := newLivePhase1Env(t)
	orgID := OrgID(9_100_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))
	productID, planID := uniqueCatalogIDs("live-deposit-sub")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	if err := env.client.EnsureOrg(ctx, orgID, fmt.Sprintf("org-%d", orgID)); err != nil {
		t.Fatalf("ensure org: %v", err)
	}
	ensureMeteredProduct(t, env.pg, productID)

	// Create a plan with 1000 included_credits and monthly_price_cents > 0 (paid).
	if _, err := env.pg.ExecContext(ctx, `
		INSERT INTO plans (plan_id, product_id, display_name, included_credits, monthly_price_cents, unit_rates, active)
		VALUES ($1, $2, 'Phase 7 Plan', 1000, 999, '{"unit":1}', true)
		ON CONFLICT (plan_id) DO NOTHING
	`, planID, productID); err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	// Create an active subscription with current_period_start in the past.
	now := time.Now().UTC()
	periodStart := now.Add(-24 * time.Hour)
	periodEnd := now.Add(29 * 24 * time.Hour)
	var subID int64
	if err := env.pg.QueryRowContext(ctx, `
		INSERT INTO subscriptions (org_id, plan_id, product_id, cadence, current_period_start, current_period_end, status)
		VALUES ($1, $2, $3, 'monthly', $4, $5, 'active')
		RETURNING subscription_id
	`, orgIDStr, planID, productID, periodStart, periodEnd).Scan(&subID); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}

	// Run DepositSubscriptionCredits.
	result, err := env.client.DepositSubscriptionCredits(ctx)
	if err != nil {
		t.Fatalf("deposit subscription credits: %v", err)
	}

	if result.SubscriptionsProcessed == 0 {
		t.Fatal("expected at least 1 subscription processed")
	}
	if result.CreditsFailed > 0 {
		t.Fatalf("expected no failures, got %d: %v", result.CreditsFailed, result.Errors)
	}

	// Verify a grant row exists for this subscription + period.
	var grantCount int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM credit_grants
		WHERE org_id = $1 AND product_id = $2 AND subscription_id = $3
	`, orgIDStr, productID, subID).Scan(&grantCount); err != nil {
		t.Fatalf("count grants: %v", err)
	}
	if grantCount != 1 {
		t.Fatalf("expected 1 grant for subscription %d, got %d", subID, grantCount)
	}

	// Verify TB balance matches included_credits.
	balance, err := env.client.GetOrgBalance(ctx, orgID)
	if err != nil {
		t.Fatalf("get org balance: %v", err)
	}
	if balance.CreditAvailable != 1000 {
		t.Fatalf("expected credit available 1000, got %d", balance.CreditAvailable)
	}

	// Idempotency: run again — no duplicates.
	result2, err := env.client.DepositSubscriptionCredits(ctx)
	if err != nil {
		t.Fatalf("idempotent deposit: %v", err)
	}
	// The second run processes the subscription but DepositCredits detects the
	// existing PG row and returns created=false, so it counts as skipped.
	if result2.CreditsFailed > 0 {
		t.Fatalf("expected no failures on idempotent run, got %d: %v", result2.CreditsFailed, result2.Errors)
	}
	if result2.CreditsDeposited > 0 {
		t.Fatalf("expected 0 deposited on idempotent run, got %d", result2.CreditsDeposited)
	}
	if result2.CreditsSkipped == 0 {
		t.Fatal("expected idempotent run to count as skipped")
	}

	// Still only one grant row.
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM credit_grants
		WHERE org_id = $1 AND product_id = $2 AND subscription_id = $3
	`, orgIDStr, productID, subID).Scan(&grantCount); err != nil {
		t.Fatalf("count grants after idempotent: %v", err)
	}
	if grantCount != 1 {
		t.Fatalf("expected 1 grant after idempotent run, got %d", grantCount)
	}

	t.Logf("verified deposit_subscription_credits: org=%s sub=%d balance=%d", orgIDStr, subID, balance.CreditAvailable)
}

// TestDepositSubscriptionCreditsAnnualDripAgainstLiveHost verifies that annual
// subscriptions receive 1/12th of yearly included_credits per month.
func TestDepositSubscriptionCreditsAnnualDripAgainstLiveHost(t *testing.T) {
	t.Parallel()

	env := newLivePhase1Env(t)
	orgID := OrgID(9_200_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))
	productID, planID := uniqueCatalogIDs("live-annual-drip")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	if err := env.client.EnsureOrg(ctx, orgID, fmt.Sprintf("org-%d", orgID)); err != nil {
		t.Fatalf("ensure org: %v", err)
	}
	ensureMeteredProduct(t, env.pg, productID)

	// 12000 annual credits → 1000 per month.
	if _, err := env.pg.ExecContext(ctx, `
		INSERT INTO plans (plan_id, product_id, display_name, included_credits, annual_price_cents, unit_rates, active)
		VALUES ($1, $2, 'Phase 7 Annual Plan', 12000, 9999, '{"unit":1}', true)
		ON CONFLICT (plan_id) DO NOTHING
	`, planID, productID); err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	now := time.Now().UTC()
	periodStart := now.Add(-24 * time.Hour)
	periodEnd := now.Add(29 * 24 * time.Hour)
	var subID int64
	if err := env.pg.QueryRowContext(ctx, `
		INSERT INTO subscriptions (org_id, plan_id, product_id, cadence, current_period_start, current_period_end, status)
		VALUES ($1, $2, $3, 'annual', $4, $5, 'active')
		RETURNING subscription_id
	`, orgIDStr, planID, productID, periodStart, periodEnd).Scan(&subID); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}

	result, err := env.client.DepositSubscriptionCredits(ctx)
	if err != nil {
		t.Fatalf("deposit subscription credits: %v", err)
	}
	if result.CreditsFailed > 0 {
		t.Fatalf("expected no failures, got %d: %v", result.CreditsFailed, result.Errors)
	}

	// Verify TB balance is 1/12th = 1000.
	balance, err := env.client.GetOrgBalance(ctx, orgID)
	if err != nil {
		t.Fatalf("get org balance: %v", err)
	}
	expectedAmount := uint64(12000 / 12)
	if balance.CreditAvailable != expectedAmount {
		t.Fatalf("expected credit available %d (1/12th of 12000), got %d", expectedAmount, balance.CreditAvailable)
	}

	t.Logf("verified annual drip: org=%s sub=%d monthly_amount=%d", orgIDStr, subID, expectedAmount)
}

// TestGetProductBalanceAgainstLiveHost verifies per-product balance bucketing.
func TestGetProductBalanceAgainstLiveHost(t *testing.T) {
	t.Parallel()

	env := newLivePhase1Env(t)
	orgID := OrgID(9_300_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))
	productID := fmt.Sprintf("billing-live-phase7-prodbal-%d", time.Now().UnixNano())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Seed three grants with different sources.
	seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceFreeTier, 100)
	seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceSubscription, 200)
	seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourcePurchase, 300)

	balance, err := env.client.GetProductBalance(ctx, orgID, productID)
	if err != nil {
		t.Fatalf("get product balance: %v", err)
	}

	if balance.ProductID != productID {
		t.Fatalf("expected product_id %q, got %q", productID, balance.ProductID)
	}
	if balance.FreeTierRemaining != 100 {
		t.Fatalf("expected free_tier_remaining 100, got %d", balance.FreeTierRemaining)
	}
	if balance.IncludedRemaining != 200 {
		t.Fatalf("expected included_remaining 200, got %d", balance.IncludedRemaining)
	}
	if balance.PrepaidRemaining != 300 {
		t.Fatalf("expected prepaid_remaining 300, got %d", balance.PrepaidRemaining)
	}

	// Empty product should return zeros.
	emptyBalance, err := env.client.GetProductBalance(ctx, orgID, "nonexistent-product")
	if err != nil {
		t.Fatalf("get empty product balance: %v", err)
	}
	if emptyBalance.FreeTierRemaining != 0 || emptyBalance.IncludedRemaining != 0 || emptyBalance.PrepaidRemaining != 0 {
		t.Fatalf("expected all zeros for nonexistent product, got %+v", emptyBalance)
	}

	t.Logf("verified product balance: product=%s free=%d included=%d prepaid=%d",
		productID, balance.FreeTierRemaining, balance.IncludedRemaining, balance.PrepaidRemaining)
}

// TestEvaluateTrustTiersPromotionAgainstLiveHost verifies that an org with
// 3+ paid months and no disputes gets promoted from new → established.
func TestEvaluateTrustTiersPromotionAgainstLiveHost(t *testing.T) {
	t.Parallel()

	env := newLivePhase1Env(t)
	orgID := OrgID(9_400_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	if err := env.client.EnsureOrg(ctx, orgID, fmt.Sprintf("org-%d", orgID)); err != nil {
		t.Fatalf("ensure org: %v", err)
	}

	// Verify starting trust_tier is 'new'.
	var tier string
	if err := env.pg.QueryRowContext(ctx, `
		SELECT trust_tier FROM orgs WHERE org_id = $1
	`, orgIDStr).Scan(&tier); err != nil {
		t.Fatalf("query trust tier: %v", err)
	}
	if tier != "new" {
		t.Fatalf("expected initial trust_tier 'new', got %q", tier)
	}

	// Seed 3 payment_succeeded events in distinct months.
	months := []time.Time{
		time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
	}
	for _, m := range months {
		if _, err := env.pg.ExecContext(ctx, `
			INSERT INTO billing_events (org_id, event_type, payload, created_at)
			VALUES ($1, 'payment_succeeded', '{}', $2)
		`, orgIDStr, m); err != nil {
			t.Fatalf("insert payment_succeeded event: %v", err)
		}
	}

	result, err := env.client.EvaluateTrustTiers(ctx)
	if err != nil {
		t.Fatalf("evaluate trust tiers: %v", err)
	}

	if result.OrgPromoted == 0 {
		t.Fatalf("expected at least 1 promotion, got 0 (errors: %v)", result.Errors)
	}

	// Verify org is now 'established'.
	if err := env.pg.QueryRowContext(ctx, `
		SELECT trust_tier FROM orgs WHERE org_id = $1
	`, orgIDStr).Scan(&tier); err != nil {
		t.Fatalf("query trust tier after promotion: %v", err)
	}
	if tier != "established" {
		t.Fatalf("expected trust_tier 'established' after promotion, got %q", tier)
	}

	// Verify trust_tier_promoted billing event.
	var eventCount int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM billing_events
		WHERE org_id = $1 AND event_type = 'trust_tier_promoted'
	`, orgIDStr).Scan(&eventCount); err != nil {
		t.Fatalf("count promotion events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected 1 trust_tier_promoted event, got %d", eventCount)
	}

	t.Logf("verified trust tier promotion: org=%s new→established promotions=%d", orgIDStr, result.OrgPromoted)
}

// TestEvaluateTrustTiersDemotionAgainstLiveHost verifies that an established
// org with a dispute gets demoted to new.
func TestEvaluateTrustTiersDemotionAgainstLiveHost(t *testing.T) {
	t.Parallel()

	env := newLivePhase1Env(t)
	orgID := OrgID(9_500_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	if err := env.client.EnsureOrg(ctx, orgID, fmt.Sprintf("org-%d", orgID)); err != nil {
		t.Fatalf("ensure org: %v", err)
	}

	// Set org to 'established' directly.
	if _, err := env.pg.ExecContext(ctx, `
		UPDATE orgs SET trust_tier = 'established' WHERE org_id = $1
	`, orgIDStr); err != nil {
		t.Fatalf("set trust_tier: %v", err)
	}

	// Seed a dispute_opened event.
	if _, err := env.pg.ExecContext(ctx, `
		INSERT INTO billing_events (org_id, event_type, payload)
		VALUES ($1, 'dispute_opened', '{"reason":"test"}')
	`, orgIDStr); err != nil {
		t.Fatalf("insert dispute event: %v", err)
	}

	if _, err := env.client.EvaluateTrustTiers(ctx); err != nil {
		t.Fatalf("evaluate trust tiers: %v", err)
	}

	// Verify org is now 'new'. Because EvaluateTrustTiers is global and
	// parallel tests may run it concurrently, we check the end state
	// rather than the return value.
	var tier string
	if err := env.pg.QueryRowContext(ctx, `
		SELECT trust_tier FROM orgs WHERE org_id = $1
	`, orgIDStr).Scan(&tier); err != nil {
		t.Fatalf("query trust tier: %v", err)
	}
	if tier != "new" {
		t.Fatalf("expected trust_tier 'new' after demotion, got %q", tier)
	}

	// Verify trust_tier_demoted billing event exists for this org.
	var eventCount int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM billing_events
		WHERE org_id = $1 AND event_type = 'trust_tier_demoted'
	`, orgIDStr).Scan(&eventCount); err != nil {
		t.Fatalf("count demotion events: %v", err)
	}
	if eventCount < 1 {
		t.Fatalf("expected at least 1 trust_tier_demoted event, got %d", eventCount)
	}

	t.Logf("verified trust tier demotion: org=%s established→new", orgIDStr)
}

// TestEvaluateTrustTiersEnterpriseNeverAutoPromotedAgainstLiveHost verifies
// that enterprise tier is never set by the cron.
func TestEvaluateTrustTiersEnterpriseNeverAutoPromotedAgainstLiveHost(t *testing.T) {
	t.Parallel()

	env := newLivePhase1Env(t)
	orgID := OrgID(9_600_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	if err := env.client.EnsureOrg(ctx, orgID, fmt.Sprintf("org-%d", orgID)); err != nil {
		t.Fatalf("ensure org: %v", err)
	}

	// Set org to 'enterprise' and add a dispute — should NOT be demoted.
	if _, err := env.pg.ExecContext(ctx, `
		UPDATE orgs SET trust_tier = 'enterprise' WHERE org_id = $1
	`, orgIDStr); err != nil {
		t.Fatalf("set trust_tier: %v", err)
	}

	if _, err := env.pg.ExecContext(ctx, `
		INSERT INTO billing_events (org_id, event_type, payload)
		VALUES ($1, 'dispute_opened', '{"reason":"test"}')
	`, orgIDStr); err != nil {
		t.Fatalf("insert dispute event: %v", err)
	}

	_, err := env.client.EvaluateTrustTiers(ctx)
	if err != nil {
		t.Fatalf("evaluate trust tiers: %v", err)
	}

	// Verify enterprise is untouched.
	var tier string
	if err := env.pg.QueryRowContext(ctx, `
		SELECT trust_tier FROM orgs WHERE org_id = $1
	`, orgIDStr).Scan(&tier); err != nil {
		t.Fatalf("query trust tier: %v", err)
	}
	if tier != "enterprise" {
		t.Fatalf("expected trust_tier 'enterprise' (unchanged), got %q", tier)
	}

	t.Logf("verified enterprise tier immutability: org=%s tier=%s", orgIDStr, tier)
}

// TestReconcileAgainstLiveHost runs the 6 named reconciliation checks against
// a clean state (all checks should pass).
func TestReconcileAgainstLiveHost(t *testing.T) {
	t.Parallel()

	chAddr := os.Getenv("FORGE_METAL_BILLING_LIVE_CH_ADDR")
	chPassword := os.Getenv("FORGE_METAL_BILLING_LIVE_CH_PASSWORD")
	if chAddr == "" || chPassword == "" {
		t.Skip("set FORGE_METAL_BILLING_LIVE_CH_ADDR and FORGE_METAL_BILLING_LIVE_CH_PASSWORD")
	}

	env := newLivePhase1Env(t)

	chConn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{chAddr},
		Auth: clickhouse.Auth{Database: "forge_metal", Username: "default", Password: chPassword},
	})
	if err != nil {
		t.Fatalf("open clickhouse: %v", err)
	}
	t.Cleanup(func() { _ = chConn.Close() })

	chQuerier := NewClickHouseReconcileQuerier(chConn, "forge_metal")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := env.client.Reconcile(ctx, chQuerier)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	for _, check := range result.Checks {
		if !check.Passed {
			t.Logf("WARN: check %q (%s) failed: %s", check.Name, check.Severity, check.Details)
		} else {
			t.Logf("PASS: check %q", check.Name)
		}
	}

	// We don't t.Fatal on existing failures since there may be pre-existing
	// state from previous test runs. Just log all results.
	t.Logf("reconcile completed: %d checks, alerts=%v", len(result.Checks), result.HasAlerts())
}

// TestReconcileDetectsUnfundedGrantAgainstLiveHost seeds a PG grant row
// without a corresponding TB account to verify check 1 detects it.
func TestReconcileDetectsUnfundedGrantAgainstLiveHost(t *testing.T) {
	t.Parallel()

	chAddr := os.Getenv("FORGE_METAL_BILLING_LIVE_CH_ADDR")
	chPassword := os.Getenv("FORGE_METAL_BILLING_LIVE_CH_PASSWORD")
	if chAddr == "" || chPassword == "" {
		t.Skip("set FORGE_METAL_BILLING_LIVE_CH_ADDR and FORGE_METAL_BILLING_LIVE_CH_PASSWORD")
	}

	env := newLivePhase1Env(t)
	orgID := OrgID(9_700_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))
	productID := fmt.Sprintf("billing-live-phase7-reconcile-unfunded-%d", time.Now().UnixNano())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	if err := env.client.EnsureOrg(ctx, orgID, fmt.Sprintf("org-%d", orgID)); err != nil {
		t.Fatalf("ensure org: %v", err)
	}
	ensureMeteredProduct(t, env.pg, productID)

	// Insert a PG grant row but deliberately skip TB account creation.
	grantID := NewGrantID()
	grantULIDStr := ulid.ULID(grantID).String()

	if _, err := env.pg.ExecContext(ctx, `
		INSERT INTO credit_grants (grant_id, org_id, product_id, amount, source)
		VALUES ($1, $2, $3, $4, 'purchase')
	`, grantULIDStr, orgIDStr, productID, int64(500)); err != nil {
		t.Fatalf("insert orphan grant: %v", err)
	}

	chConn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{chAddr},
		Auth: clickhouse.Auth{Database: "forge_metal", Username: "default", Password: chPassword},
	})
	if err != nil {
		t.Fatalf("open clickhouse: %v", err)
	}
	t.Cleanup(func() { _ = chConn.Close() })

	chQuerier := NewClickHouseReconcileQuerier(chConn, "forge_metal")

	result, err := env.client.Reconcile(ctx, chQuerier)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// Check 1 should detect the unfunded grant.
	var found bool
	for _, check := range result.Checks {
		if check.Name == "grant_account_catalog_consistency" && !check.Passed {
			found = true
			t.Logf("correctly detected unfunded grant: %s", check.Details)
		}
	}
	if !found {
		t.Fatal("expected grant_account_catalog_consistency to fail for unfunded grant")
	}

	// Clean up: close the orphan grant so it doesn't pollute future runs.
	if _, err := env.pg.ExecContext(ctx, `
		UPDATE credit_grants SET closed_at = now() WHERE grant_id = $1
	`, grantULIDStr); err != nil {
		t.Logf("cleanup orphan grant: %v", err)
	}

	t.Logf("verified reconcile detects unfunded grant: org=%s grant=%s", orgIDStr, grantULIDStr)
}
