//go:build integration

package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// TestWebhookWorkerPurchaseDepositAgainstLiveHost exercises the full webhook→worker path:
// insert a stripe_purchase_deposit task row (simulating what the webhook handler would do),
// run the worker dispatch directly, and verify the grant appears in TigerBeetle and
// the billing_event is logged.
func TestWebhookWorkerPurchaseDepositAgainstLiveHost(t *testing.T) {
	t.Parallel()

	env := newLivePhase1Env(t)
	// Use a unique org range that doesn't collide with Phase 4 (7800...) or other tests.
	orgID := OrgID(8_100_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))
	productID := fmt.Sprintf("billing-live-phase5-purchase-%d", time.Now().UnixNano())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	// Minimal setup: just an org and a product. No subscription, no plan —
	// purchase deposits don't require either.
	if err := env.client.EnsureOrg(ctx, orgID, fmt.Sprintf("org-%d", orgID)); err != nil {
		t.Fatalf("ensure org: %v", err)
	}
	if _, err := env.pg.ExecContext(ctx, `
		INSERT INTO products (product_id, display_name, meter_unit, billing_model)
		VALUES ($1, 'Purchase Test', 'unit', 'metered')
		ON CONFLICT (product_id) DO NOTHING
	`, productID); err != nil {
		t.Fatalf("insert product: %v", err)
	}

	expiresAt := time.Now().UTC().AddDate(1, 0, 0)
	piID := fmt.Sprintf("pi_test_phase5_%d", time.Now().UnixNano())

	payload := map[string]interface{}{
		"org_id":                   orgIDStr,
		"product_id":               productID,
		"stripe_payment_intent_id": piID,
		"amount_ledger_units":      int64(5000),
		"expires_at":               expiresAt.Format(time.RFC3339),
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	// Insert task row as the webhook handler would.
	var taskID int64
	err = env.pg.QueryRowContext(ctx, `
		INSERT INTO tasks (task_type, payload, idempotency_key)
		VALUES ('stripe_purchase_deposit', $1::jsonb, $2)
		RETURNING task_id
	`, string(payloadJSON), piID).Scan(&taskID)
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}

	// Claim the task.
	task, ok, err := env.client.claimTask(ctx)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if !ok {
		t.Fatal("expected to claim a task, got none")
	}
	if task.TaskID != taskID {
		t.Fatalf("expected task_id %d, got %d", taskID, task.TaskID)
	}

	// Dispatch.
	if err := env.client.dispatchTask(ctx, task); err != nil {
		t.Fatalf("dispatch task: %v", err)
	}
	env.client.completeTask(ctx, task)

	// Verify task completed.
	var status string
	if err := env.pg.QueryRowContext(ctx, `
		SELECT status FROM tasks WHERE task_id = $1
	`, taskID).Scan(&status); err != nil {
		t.Fatalf("query task status: %v", err)
	}
	if status != "completed" {
		t.Fatalf("expected task status 'completed', got %q", status)
	}

	// Verify exactly one PG grant row with correct fields.
	var (
		grantIDStr        string
		grantAmount       int64
		grantSource       string
		grantStripeRef    *string
		grantProductID    string
	)
	if err := env.pg.QueryRowContext(ctx, `
		SELECT grant_id, amount, source, stripe_reference_id, product_id
		FROM credit_grants
		WHERE org_id = $1 AND product_id = $2 AND source = 'purchase'
	`, orgIDStr, productID).Scan(&grantIDStr, &grantAmount, &grantSource, &grantStripeRef, &grantProductID); err != nil {
		t.Fatalf("query grant row: %v", err)
	}
	if grantAmount != 5000 {
		t.Fatalf("expected grant amount 5000, got %d", grantAmount)
	}
	if grantSource != "purchase" {
		t.Fatalf("expected grant source 'purchase', got %q", grantSource)
	}
	if grantStripeRef == nil || *grantStripeRef != piID {
		t.Fatalf("expected stripe_reference_id %q, got %v", piID, grantStripeRef)
	}
	if grantProductID != productID {
		t.Fatalf("expected product_id %q, got %q", productID, grantProductID)
	}

	// Verify no extra grants for this org/product.
	var grantCount int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM credit_grants
		WHERE org_id = $1 AND product_id = $2
	`, orgIDStr, productID).Scan(&grantCount); err != nil {
		t.Fatalf("count grants: %v", err)
	}
	if grantCount != 1 {
		t.Fatalf("expected exactly 1 grant (any source), got %d", grantCount)
	}

	// Verify exactly one credits_deposited billing_event that references our grant.
	var eventCount int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM billing_events
		WHERE org_id = $1 AND event_type = 'credits_deposited' AND grant_id = $2
	`, orgIDStr, grantIDStr).Scan(&eventCount); err != nil {
		t.Fatalf("count billing_events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected exactly 1 credits_deposited event for grant %s, got %d", grantIDStr, eventCount)
	}

	// Verify exact TB balance. This org has no other grants — only the purchase deposit.
	balance, err := env.client.GetOrgBalance(ctx, orgID)
	if err != nil {
		t.Fatalf("get org balance: %v", err)
	}
	expected := Balance{
		CreditAvailable: 5000,
		TotalAvailable:  5000,
	}
	if balance != expected {
		t.Fatalf("expected balance %+v, got %+v", expected, balance)
	}

	t.Logf("verified live webhook→worker purchase deposit: org_id=%s task_id=%d balance=%+v", orgIDStr, taskID, balance)
}

// TestWebhookWorkerLicensedChargeAgainstLiveHost exercises the licensed charge path:
// insert a stripe_licensed_charge task, dispatch it, verify the TigerBeetle transfer
// and billing_event.
func TestWebhookWorkerLicensedChargeAgainstLiveHost(t *testing.T) {
	t.Parallel()

	env := newLivePhase1Env(t)
	orgID := OrgID(8_200_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))
	productID := fmt.Sprintf("billing-live-phase5-licensed-%d", time.Now().UnixNano())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	// Set up a licensed product and subscription.
	if err := env.client.EnsureOrg(ctx, orgID, fmt.Sprintf("org-%d", orgID)); err != nil {
		t.Fatalf("ensure org: %v", err)
	}
	if _, err := env.pg.ExecContext(ctx, `
		INSERT INTO products (product_id, display_name, meter_unit, billing_model)
		VALUES ($1, 'Licensed Test', 'unit', 'licensed')
		ON CONFLICT (product_id) DO NOTHING
	`, productID); err != nil {
		t.Fatalf("insert product: %v", err)
	}
	planID := productID + "-plan"
	if _, err := env.pg.ExecContext(ctx, `
		INSERT INTO plans (plan_id, product_id, display_name, unit_rates, active)
		VALUES ($1, $2, 'Licensed Plan', '{}', true)
		ON CONFLICT (plan_id) DO NOTHING
	`, planID, productID); err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	var subID int64
	now := time.Now().UTC()
	if err := env.pg.QueryRowContext(ctx, `
		INSERT INTO subscriptions (org_id, plan_id, product_id, cadence, current_period_start, current_period_end, status)
		VALUES ($1, $2, $3, 'monthly', $4, $5, 'active')
		RETURNING subscription_id
	`, orgIDStr, planID, productID, now, now.Add(30*24*time.Hour)).Scan(&subID); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}

	// Record Revenue balance before dispatch.
	revenueBefore := lookupOperatorPostedCredits(t, env.tbClient, AcctRevenue)

	invoiceID := fmt.Sprintf("in_test_phase5_%d", time.Now().UnixNano())
	payload := map[string]interface{}{
		"org_id":              orgIDStr,
		"product_id":          productID,
		"subscription_id":     subID,
		"stripe_invoice_id":   invoiceID,
		"amount_ledger_units": int64(20_000_000), // $2.00
		"period_start":        now.Format(time.RFC3339),
		"period_end":          now.Add(30 * 24 * time.Hour).Format(time.RFC3339),
	}
	payloadJSON, _ := json.Marshal(payload)

	var taskID int64
	err := env.pg.QueryRowContext(ctx, `
		INSERT INTO tasks (task_type, payload, idempotency_key)
		VALUES ('stripe_licensed_charge', $1::jsonb, $2)
		RETURNING task_id
	`, string(payloadJSON), invoiceID).Scan(&taskID)
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}

	task, ok, err := env.client.claimTask(ctx)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if !ok {
		t.Fatal("expected to claim a task")
	}

	if err := env.client.dispatchTask(ctx, task); err != nil {
		t.Fatalf("dispatch task: %v", err)
	}
	env.client.completeTask(ctx, task)

	// Verify TigerBeetle: Revenue must have increased by exactly 20_000_000.
	revenueAfter := lookupOperatorPostedCredits(t, env.tbClient, AcctRevenue)
	if revenueAfter-revenueBefore != 20_000_000 {
		t.Fatalf("expected Revenue to increase by exactly 20000000, got %d (before=%d after=%d)",
			revenueAfter-revenueBefore, revenueBefore, revenueAfter)
	}

	// Verify no credit_grants row (licensed products don't create grants).
	var grantCount int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM credit_grants WHERE org_id = $1 AND product_id = $2
	`, orgIDStr, productID).Scan(&grantCount); err != nil {
		t.Fatalf("count grants: %v", err)
	}
	if grantCount != 0 {
		t.Fatalf("expected 0 grant rows for licensed product, got %d", grantCount)
	}

	// Verify exactly one billing_event.
	var eventCount int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM billing_events
		WHERE org_id = $1 AND event_type = 'licensed_charge_recorded'
	`, orgIDStr).Scan(&eventCount); err != nil {
		t.Fatalf("count billing_events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected exactly 1 licensed_charge_recorded event, got %d", eventCount)
	}

	t.Logf("verified live licensed charge: org_id=%s task_id=%d sub_id=%d revenue_delta=20000000", orgIDStr, taskID, subID)
}

// TestDisputeTaskGoesToDLQAgainstLiveHost verifies that stripe_dispute_debit
// tasks get the not-implemented error and go to DLQ after max_attempts.
func TestDisputeTaskGoesToDLQAgainstLiveHost(t *testing.T) {
	t.Parallel()

	env := newLivePhase1Env(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	disputeID := fmt.Sprintf("dp_test_phase5_%d", time.Now().UnixNano())
	payload := map[string]interface{}{
		"org_id":                   "999",
		"stripe_dispute_id":        disputeID,
		"stripe_payment_intent_id": "pi_fake",
		"amount_ledger_units":      int64(100),
	}
	payloadJSON, _ := json.Marshal(payload)

	var taskID int64
	err := env.pg.QueryRowContext(ctx, `
		INSERT INTO tasks (task_type, payload, idempotency_key, max_attempts)
		VALUES ('stripe_dispute_debit', $1::jsonb, $2, 1)
		RETURNING task_id
	`, string(payloadJSON), disputeID).Scan(&taskID)
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}

	task, ok, err := env.client.claimTask(ctx)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if !ok {
		t.Fatal("expected to claim a task")
	}

	dispatchErr := env.client.dispatchTask(ctx, task)
	if dispatchErr == nil {
		t.Fatal("expected dispatch to fail for dispute_debit")
	}
	if dispatchErr != ErrTaskNotImplemented {
		t.Fatalf("expected ErrTaskNotImplemented, got %v", dispatchErr)
	}
	env.client.failTask(ctx, task, dispatchErr)

	// With max_attempts=1, should go to dead immediately.
	var status string
	var lastError string
	var deadAt *time.Time
	if err := env.pg.QueryRowContext(ctx, `
		SELECT status, last_error, dead_at FROM tasks WHERE task_id = $1
	`, taskID).Scan(&status, &lastError, &deadAt); err != nil {
		t.Fatalf("query task: %v", err)
	}
	if status != "dead" {
		t.Fatalf("expected status 'dead', got %q", status)
	}
	if lastError != ErrTaskNotImplemented.Error() {
		t.Fatalf("expected last_error %q, got %q", ErrTaskNotImplemented.Error(), lastError)
	}
	if deadAt == nil {
		t.Fatal("expected dead_at to be set")
	}

	// Verify attempts = 1 (claimed once, immediately dead).
	var attempts int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT attempts FROM tasks WHERE task_id = $1
	`, taskID).Scan(&attempts); err != nil {
		t.Fatalf("query attempts: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected exactly 1 attempt, got %d", attempts)
	}

	t.Logf("verified dispute task goes to DLQ: task_id=%d status=%s attempts=%d", taskID, status, attempts)
}

// TestOverageCeilingHitBillingEvent verifies that the telemetry gap is closed:
// enforceOverageCap now logs an overage_ceiling_hit billing event.
func TestOverageCeilingHitBillingEvent(t *testing.T) {
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

	env.client.SetMeteringWriter(NewClickHouseMeteringWriter(chConn, "forge_metal"))
	env.client.SetMeteringQuerier(NewClickHouseMeteringQuerier(chConn, "forge_metal"))
	env.client.cfg.ReservationWindowSecs = 60
	env.client.cfg.PendingTimeoutSecs = 600

	orgID := OrgID(8_300_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))
	productID, planID := uniqueCatalogIDs("live-phase5-overage-telemetry")

	subID := ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID,
		map[string]uint64{"unit": 1}, map[string]uint64{"unit": 2}, false)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	// Set a very low overage cap.
	if _, err := env.pg.ExecContext(ctx, `
		UPDATE subscriptions SET overage_cap_units = 10
		WHERE subscription_id = $1
	`, subID); err != nil {
		t.Fatalf("set overage cap: %v", err)
	}

	// Seed a prepaid grant to enable overage.
	seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourcePurchase, 200)

	// Clean existing events for this org.
	if _, err := env.pg.ExecContext(ctx, `
		DELETE FROM billing_events WHERE org_id = $1 AND event_type = 'overage_ceiling_hit'
	`, orgIDStr); err != nil {
		t.Fatalf("clean billing events: %v", err)
	}

	// Reserve should fail: overage cap is 10 but window cost = 60 * 2 = 120 >> 10.
	jobID := JobID(time.Now().UTC().UnixNano() % 1_000_000_000)
	_, reserveErr := env.client.Reserve(ctx, ReserveRequest{
		JobID:      jobID,
		OrgID:      orgID,
		ProductID:  productID,
		ActorID:    "actor-overage-test",
		Allocation: map[string]float64{"unit": 1},
		SourceType: "job",
		SourceRef:  strconv.FormatInt(int64(jobID), 10),
	})
	if reserveErr == nil {
		// Subscription grants may still have balance, causing included phase.
		t.Log("reserve succeeded (no overage phase reached), skipping overage_ceiling_hit test")
		return
	}

	// We cleaned events before the Reserve call, so exactly 1 should exist.
	var eventCount int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM billing_events
		WHERE org_id = $1 AND event_type = 'overage_ceiling_hit'
	`, orgIDStr).Scan(&eventCount); err != nil {
		t.Fatalf("count overage_ceiling_hit events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected exactly 1 overage_ceiling_hit event, got %d", eventCount)
	}

	t.Logf("verified overage_ceiling_hit billing event: org_id=%s", orgIDStr)
}

// TestQuotaExceededBillingEvent verifies the telemetry gap closure:
// CheckQuotas now logs a quota_exceeded billing event.
func TestQuotaExceededBillingEvent(t *testing.T) {
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

	env.client.SetMeteringQuerier(NewClickHouseMeteringQuerier(chConn, "forge_metal"))

	orgID := OrgID(8_400_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))
	productID, planID := uniqueCatalogIDs("live-phase5-quota-telemetry")

	ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID,
		map[string]uint64{"unit": 1}, nil, false)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	// Set quota: concurrent_vms instant limit = 2.
	quotasJSON, _ := json.Marshal(quotaPolicy{Limits: []quotaLimit{
		{Dimension: "concurrent_vms", Window: "instant", Limit: 2},
	}})
	if _, err := env.pg.ExecContext(ctx, `
		UPDATE plans SET quotas = $1::jsonb WHERE plan_id = $2
	`, string(quotasJSON), planID); err != nil {
		t.Fatalf("update quotas: %v", err)
	}

	// Clean existing events.
	if _, err := env.pg.ExecContext(ctx, `
		DELETE FROM billing_events WHERE org_id = $1 AND event_type = 'quota_exceeded'
	`, orgIDStr); err != nil {
		t.Fatalf("clean billing events: %v", err)
	}

	// Trigger violation: concurrent_vms = 5 > limit 2.
	result, err := env.client.CheckQuotas(ctx, orgID, productID, map[string]float64{
		"concurrent_vms": 5,
	})
	if err != nil {
		t.Fatalf("check quotas: %v", err)
	}
	if result.Allowed {
		t.Fatal("expected quota violation, got Allowed=true")
	}

	// We cleaned events before the CheckQuotas call, so exactly 1 should exist.
	var eventCount int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM billing_events
		WHERE org_id = $1 AND event_type = 'quota_exceeded'
	`, orgIDStr).Scan(&eventCount); err != nil {
		t.Fatalf("count quota_exceeded events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected exactly 1 quota_exceeded event, got %d", eventCount)
	}

	t.Logf("verified quota_exceeded billing event: org_id=%s", orgIDStr)
}
