//go:build integration

package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
)

// TestDisputeDebitSufficientBalanceAgainstLiveHost exercises the happy path:
// org has enough credit to cover the dispute, so the grant is debited,
// StripeHolding is credited, and the org is NOT suspended.
func TestDisputeDebitSufficientBalanceAgainstLiveHost(t *testing.T) {
	t.Parallel()

	env := newLivePhase1Env(t)
	orgID := OrgID(8_600_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))
	productID := fmt.Sprintf("billing-live-phase6-dispute-ok-%d", time.Now().UnixNano())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	// Set up org, product, and a purchase grant with 5000 ledger units.
	if err := env.client.EnsureOrg(ctx, orgID, fmt.Sprintf("org-%d", orgID)); err != nil {
		t.Fatalf("ensure org: %v", err)
	}
	if _, err := env.pg.ExecContext(ctx, `
		INSERT INTO products (product_id, display_name, meter_unit, billing_model)
		VALUES ($1, 'Dispute Test OK', 'unit', 'metered')
		ON CONFLICT (product_id) DO NOTHING
	`, productID); err != nil {
		t.Fatalf("insert product: %v", err)
	}

	// Create a grant with a known stripe_reference_id so we can verify
	// that HandleDispute targets it first.
	piID := fmt.Sprintf("pi_dispute_ok_%d", time.Now().UnixNano())
	grant := seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourcePurchase, 5000)

	// Set the stripe_reference_id on the grant so HandleDispute can match it.
	grantIDStr := grantIDToString(grant.grantID)
	if _, err := env.pg.ExecContext(ctx, `
		UPDATE credit_grants SET stripe_reference_id = $1 WHERE grant_id = $2
	`, piID, grantIDStr); err != nil {
		t.Fatalf("set stripe_reference_id: %v", err)
	}

	// Create a subscription so suspension can be verified as NOT happening.
	planID := productID + "-plan"
	if _, err := env.pg.ExecContext(ctx, `
		INSERT INTO plans (plan_id, product_id, display_name, unit_rates, active)
		VALUES ($1, $2, 'Dispute Plan', '{}', true)
		ON CONFLICT (plan_id) DO NOTHING
	`, planID, productID); err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	now := time.Now().UTC()
	if _, err := env.pg.ExecContext(ctx, `
		INSERT INTO subscriptions (org_id, plan_id, product_id, cadence, current_period_start, current_period_end, status)
		VALUES ($1, $2, $3, 'monthly', $4, $5, 'active')
	`, orgIDStr, planID, productID, now, now.Add(30*24*time.Hour)); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}

	// Record StripeHolding before dispute.
	holdingBefore := lookupOperatorPostedCredits(t, env.tbClient, AcctStripeHolding)

	// Dispute for 3000 out of 5000 available.
	disputeAmount := uint64(3000)
	disputeID := fmt.Sprintf("dp_test_phase6_ok_%d", time.Now().UnixNano())

	payload := map[string]interface{}{
		"org_id":                   orgIDStr,
		"stripe_dispute_id":        disputeID,
		"stripe_payment_intent_id": piID,
		"amount_ledger_units":      int64(disputeAmount),
	}
	payloadJSON, _ := json.Marshal(payload)

	var taskID int64
	err := env.pg.QueryRowContext(ctx, `
		INSERT INTO tasks (task_type, payload, idempotency_key)
		VALUES ('stripe_dispute_debit', $1::jsonb, $2)
		RETURNING task_id
	`, string(payloadJSON), disputeID).Scan(&taskID)
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}

	// Claim and dispatch.
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

	// Verify TigerBeetle: grant debited by exactly 3000.
	requireGrantBalance(t, env.tbClient, grant.grantID, 2000, 0, 3000)

	// Verify TigerBeetle: StripeHolding credited by at least disputeAmount.
	// StripeHolding is shared across parallel tests, so we use at-least semantics.
	holdingAfter := lookupOperatorPostedCredits(t, env.tbClient, AcctStripeHolding)
	if holdingAfter-holdingBefore < disputeAmount {
		t.Fatalf("expected StripeHolding to increase by at least %d, got %d (before=%d after=%d)",
			disputeAmount, holdingAfter-holdingBefore, holdingBefore, holdingAfter)
	}

	// Verify billing event logged with task_id.
	var eventCount int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM billing_events
		WHERE org_id = $1 AND event_type = 'dispute_opened' AND task_id = $2
	`, orgIDStr, taskID).Scan(&eventCount); err != nil {
		t.Fatalf("count billing_events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected exactly 1 dispute_opened event with task_id=%d, got %d", taskID, eventCount)
	}

	// Verify org is NOT suspended (balance was sufficient).
	var subStatus string
	if err := env.pg.QueryRowContext(ctx, `
		SELECT status FROM subscriptions WHERE org_id = $1 AND product_id = $2
	`, orgIDStr, productID).Scan(&subStatus); err != nil {
		t.Fatalf("query subscription status: %v", err)
	}
	if subStatus != "active" {
		t.Fatalf("expected subscription status 'active', got %q", subStatus)
	}

	// Verify no org_suspended event.
	var suspendCount int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM billing_events
		WHERE org_id = $1 AND event_type = 'org_suspended'
	`, orgIDStr).Scan(&suspendCount); err != nil {
		t.Fatalf("count org_suspended events: %v", err)
	}
	if suspendCount != 0 {
		t.Fatalf("expected 0 org_suspended events, got %d", suspendCount)
	}

	t.Logf("verified dispute debit (sufficient): org_id=%s task_id=%d debited=%d remaining=%d",
		orgIDStr, taskID, disputeAmount, 2000)
}

// TestDisputeDebitInsufficientBalanceSuspendsOrgAgainstLiveHost exercises the sad path:
// org does NOT have enough credit to cover the dispute, so the grant is drained
// and the org is suspended.
func TestDisputeDebitInsufficientBalanceSuspendsOrgAgainstLiveHost(t *testing.T) {
	t.Parallel()

	env := newLivePhase1Env(t)
	orgID := OrgID(8_700_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))
	productID := fmt.Sprintf("billing-live-phase6-dispute-suspend-%d", time.Now().UnixNano())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	// Set up org, product, and a small grant with only 1000 ledger units.
	if err := env.client.EnsureOrg(ctx, orgID, fmt.Sprintf("org-%d", orgID)); err != nil {
		t.Fatalf("ensure org: %v", err)
	}
	if _, err := env.pg.ExecContext(ctx, `
		INSERT INTO products (product_id, display_name, meter_unit, billing_model)
		VALUES ($1, 'Dispute Test Suspend', 'unit', 'metered')
		ON CONFLICT (product_id) DO NOTHING
	`, productID); err != nil {
		t.Fatalf("insert product: %v", err)
	}

	grant := seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourcePurchase, 1000)

	// Create an active subscription that should be suspended.
	planID := productID + "-plan"
	if _, err := env.pg.ExecContext(ctx, `
		INSERT INTO plans (plan_id, product_id, display_name, unit_rates, active)
		VALUES ($1, $2, 'Dispute Plan', '{}', true)
		ON CONFLICT (plan_id) DO NOTHING
	`, planID, productID); err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	now := time.Now().UTC()
	if _, err := env.pg.ExecContext(ctx, `
		INSERT INTO subscriptions (org_id, plan_id, product_id, cadence, current_period_start, current_period_end, status)
		VALUES ($1, $2, $3, 'monthly', $4, $5, 'active')
	`, orgIDStr, planID, productID, now, now.Add(30*24*time.Hour)); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}

	// Dispute for 5000 but only 1000 available — should trigger suspension.
	disputeAmount := uint64(5000)
	disputeID := fmt.Sprintf("dp_test_phase6_suspend_%d", time.Now().UnixNano())

	payload := map[string]interface{}{
		"org_id":                   orgIDStr,
		"stripe_dispute_id":        disputeID,
		"stripe_payment_intent_id": "pi_fake_suspend",
		"amount_ledger_units":      int64(disputeAmount),
	}
	payloadJSON, _ := json.Marshal(payload)

	var taskID int64
	err := env.pg.QueryRowContext(ctx, `
		INSERT INTO tasks (task_type, payload, idempotency_key)
		VALUES ('stripe_dispute_debit', $1::jsonb, $2)
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
		t.Fatal("expected to claim a task, got none")
	}

	if err := env.client.dispatchTask(ctx, task); err != nil {
		t.Fatalf("dispatch task: %v", err)
	}
	env.client.completeTask(ctx, task)

	// Verify TigerBeetle: grant fully drained (clamped to 1000).
	requireGrantBalance(t, env.tbClient, grant.grantID, 0, 0, 1000)

	// Verify subscription is suspended.
	var subStatus string
	if err := env.pg.QueryRowContext(ctx, `
		SELECT status FROM subscriptions WHERE org_id = $1 AND product_id = $2
	`, orgIDStr, productID).Scan(&subStatus); err != nil {
		t.Fatalf("query subscription status: %v", err)
	}
	if subStatus != "suspended" {
		t.Fatalf("expected subscription status 'suspended', got %q", subStatus)
	}

	// Verify org_suspended billing event logged.
	var suspendCount int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM billing_events
		WHERE org_id = $1 AND event_type = 'org_suspended'
	`, orgIDStr).Scan(&suspendCount); err != nil {
		t.Fatalf("count org_suspended events: %v", err)
	}
	if suspendCount != 1 {
		t.Fatalf("expected exactly 1 org_suspended event, got %d", suspendCount)
	}

	// Verify dispute_opened billing event has org_suspended=true.
	var disputePayload string
	if err := env.pg.QueryRowContext(ctx, `
		SELECT payload FROM billing_events
		WHERE org_id = $1 AND event_type = 'dispute_opened' AND task_id = $2
	`, orgIDStr, taskID).Scan(&disputePayload); err != nil {
		t.Fatalf("query dispute event payload: %v", err)
	}
	var payloadData map[string]interface{}
	if err := json.Unmarshal([]byte(disputePayload), &payloadData); err != nil {
		t.Fatalf("parse dispute event payload: %v", err)
	}
	if suspended, ok := payloadData["org_suspended"].(bool); !ok || !suspended {
		t.Fatalf("expected org_suspended=true in dispute event payload, got %v", payloadData)
	}
	if debited, ok := payloadData["total_debited_ledger_units"].(float64); !ok || uint64(debited) != 1000 {
		t.Fatalf("expected total_debited=1000, got %v", payloadData["total_debited_ledger_units"])
	}

	// Verify Reserve is now blocked for this org.
	env.client.cfg.ReservationWindowSecs = 60
	env.client.cfg.PendingTimeoutSecs = 600
	_, reserveErr := env.client.Reserve(ctx, ReserveRequest{
		JobID:      JobID(time.Now().UTC().UnixNano() % 1_000_000_000),
		OrgID:      orgID,
		ProductID:  productID,
		ActorID:    "actor-post-dispute",
		Allocation: map[string]float64{"unit": 1},
		SourceType: "job",
		SourceRef:  "test",
	})
	if reserveErr != ErrOrgSuspended {
		t.Fatalf("expected ErrOrgSuspended after dispute suspension, got %v", reserveErr)
	}

	t.Logf("verified dispute debit (insufficient): org_id=%s task_id=%d drained=%d suspended=true reserve_blocked=true",
		orgIDStr, taskID, 1000)
}

// TestDisputeDebitMultiGrantWaterfallAgainstLiveHost verifies that disputes
// spanning multiple grants debit them in order: disputed-payment grant first,
// then remaining grants.
func TestDisputeDebitMultiGrantWaterfallAgainstLiveHost(t *testing.T) {
	t.Parallel()

	env := newLivePhase1Env(t)
	orgID := OrgID(8_800_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))
	productID := fmt.Sprintf("billing-live-phase6-multi-%d", time.Now().UnixNano())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	if err := env.client.EnsureOrg(ctx, orgID, fmt.Sprintf("org-%d", orgID)); err != nil {
		t.Fatalf("ensure org: %v", err)
	}
	if _, err := env.pg.ExecContext(ctx, `
		INSERT INTO products (product_id, display_name, meter_unit, billing_model)
		VALUES ($1, 'Dispute Multi Test', 'unit', 'metered')
		ON CONFLICT (product_id) DO NOTHING
	`, productID); err != nil {
		t.Fatalf("insert product: %v", err)
	}

	// Create two grants: one matching the disputed payment (2000), one not (3000).
	piID := fmt.Sprintf("pi_dispute_multi_%d", time.Now().UnixNano())

	grantA := seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourcePurchase, 2000)
	grantAStr := grantIDToString(grantA.grantID)
	if _, err := env.pg.ExecContext(ctx, `
		UPDATE credit_grants SET stripe_reference_id = $1 WHERE grant_id = $2
	`, piID, grantAStr); err != nil {
		t.Fatalf("set stripe_reference_id on grant A: %v", err)
	}

	grantB := seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourcePurchase, 3000)

	// Dispute for 4000: should drain all of grantA (2000) + 2000 from grantB.
	disputeAmount := uint64(4000)
	disputeID := fmt.Sprintf("dp_test_phase6_multi_%d", time.Now().UnixNano())

	payload := map[string]interface{}{
		"org_id":                   orgIDStr,
		"stripe_dispute_id":        disputeID,
		"stripe_payment_intent_id": piID,
		"amount_ledger_units":      int64(disputeAmount),
	}
	payloadJSON, _ := json.Marshal(payload)

	var taskID int64
	err := env.pg.QueryRowContext(ctx, `
		INSERT INTO tasks (task_type, payload, idempotency_key)
		VALUES ('stripe_dispute_debit', $1::jsonb, $2)
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
		t.Fatal("expected to claim a task, got none")
	}

	if err := env.client.dispatchTask(ctx, task); err != nil {
		t.Fatalf("dispatch task: %v", err)
	}
	env.client.completeTask(ctx, task)

	// Grant A (disputed payment's grant): fully drained.
	requireGrantBalance(t, env.tbClient, grantA.grantID, 0, 0, 2000)

	// Grant B (fallback): debited by 2000, leaving 1000.
	requireGrantBalance(t, env.tbClient, grantB.grantID, 1000, 0, 2000)

	// Verify event payload shows 2 grants debited, no suspension.
	var disputePayload string
	if err := env.pg.QueryRowContext(ctx, `
		SELECT payload FROM billing_events
		WHERE org_id = $1 AND event_type = 'dispute_opened' AND task_id = $2
	`, orgIDStr, taskID).Scan(&disputePayload); err != nil {
		t.Fatalf("query dispute event payload: %v", err)
	}
	var payloadData map[string]interface{}
	if err := json.Unmarshal([]byte(disputePayload), &payloadData); err != nil {
		t.Fatalf("parse dispute event payload: %v", err)
	}
	if grantsDebited, ok := payloadData["grants_debited"].(float64); !ok || int(grantsDebited) != 2 {
		t.Fatalf("expected grants_debited=2, got %v", payloadData["grants_debited"])
	}
	if suspended, ok := payloadData["org_suspended"].(bool); !ok || suspended {
		t.Fatalf("expected org_suspended=false, got %v", payloadData["org_suspended"])
	}

	t.Logf("verified multi-grant dispute waterfall: org_id=%s task_id=%d grantA_drained=2000 grantB_debited=2000 grantB_remaining=1000",
		orgIDStr, taskID)
}

func grantIDToString(g GrantID) string {
	return ulid.ULID(g).String()
}
