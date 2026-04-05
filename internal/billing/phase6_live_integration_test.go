//go:build integration

package billing

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	tbtypes "github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

// TestDisputeSufficientBalanceAgainstLiveHost exercises the happy path:
// org has enough credit balance to cover the dispute, so no suspension occurs.
// Verifies: grant debited, StripeHolding credited, task completed, billing event logged.
func TestDisputeSufficientBalanceAgainstLiveHost(t *testing.T) {
	t.Parallel()

	env := newLivePhase1Env(t)
	orgID := OrgID(8_500_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))
	productID := fmt.Sprintf("billing-live-phase6-dispute-ok-%d", time.Now().UnixNano())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	// Set up: org + product + a purchase grant of 5000 with a known stripe_reference_id.
	if err := env.client.EnsureOrg(ctx, orgID, fmt.Sprintf("org-%d", orgID)); err != nil {
		t.Fatalf("ensure org: %v", err)
	}
	ensureMeteredProduct(t, env.pg, productID)

	piID := fmt.Sprintf("pi_test_phase6_%d", time.Now().UnixNano())
	seedGrantWithStripeRef(t, env, orgID, productID, piID, 5000)

	// Record StripeHolding before.
	holdingBefore := lookupOperatorPostedCredits(t, env.tbClient, AcctStripeHolding)

	// Dispute for 3000 (less than grant balance).
	disputeID := fmt.Sprintf("dp_test_phase6_ok_%d", time.Now().UnixNano())
	disputeAmount := uint64(3000)
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

	// Claim + dispatch.
	task, ok, err := env.client.claimTask(ctx)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if !ok {
		t.Fatal("expected to claim a task")
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

	// Verify TigerBeetle: grant balance reduced by 3000.
	balance, err := env.client.GetOrgBalance(ctx, orgID)
	if err != nil {
		t.Fatalf("get org balance: %v", err)
	}
	expectedRemaining := uint64(5000 - 3000)
	if balance.CreditAvailable != expectedRemaining {
		t.Fatalf("expected credit available %d, got %d", expectedRemaining, balance.CreditAvailable)
	}

	// Verify TigerBeetle: StripeHolding credits increased by dispute amount.
	holdingAfter := lookupOperatorPostedCredits(t, env.tbClient, AcctStripeHolding)
	holdingDelta := holdingAfter - holdingBefore
	if holdingDelta != disputeAmount {
		t.Fatalf("expected StripeHolding credits to increase by %d, got delta %d", disputeAmount, holdingDelta)
	}

	// Verify TigerBeetle: dispute transfer exists.
	transfers, err := env.tbClient.LookupTransfers([]tbtypes.Uint128{DisputeDebitID(TaskID(taskID), 0).raw})
	if err != nil {
		t.Fatalf("lookup dispute transfer: %v", err)
	}
	if len(transfers) != 1 {
		t.Fatalf("expected 1 dispute transfer, got %d", len(transfers))
	}
	if transfers[0].Code != uint16(KindDisputeDebit) {
		t.Fatalf("expected transfer code %d, got %d", KindDisputeDebit, transfers[0].Code)
	}

	// Verify PG: dispute_opened billing event with task_id.
	var eventCount int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM billing_events
		WHERE org_id = $1 AND event_type = 'dispute_opened' AND task_id = $2
	`, orgIDStr, taskID).Scan(&eventCount); err != nil {
		t.Fatalf("count dispute events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected exactly 1 dispute_opened event with task_id, got %d", eventCount)
	}

	// Verify NO suspension — all subscriptions should not be suspended.
	var suspendedCount int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM subscriptions
		WHERE org_id = $1 AND status = 'suspended'
	`, orgIDStr).Scan(&suspendedCount); err != nil {
		t.Fatalf("count suspended: %v", err)
	}
	if suspendedCount != 0 {
		t.Fatalf("expected 0 suspended subscriptions, got %d", suspendedCount)
	}

	t.Logf("verified dispute sufficient balance: org_id=%s task_id=%d debited=%d remaining=%d",
		orgIDStr, taskID, disputeAmount, expectedRemaining)
}

// TestDisputeInsufficientBalanceSuspendsOrgAgainstLiveHost exercises the sad path:
// dispute amount exceeds org's total credit balance, so org gets suspended.
func TestDisputeInsufficientBalanceSuspendsOrgAgainstLiveHost(t *testing.T) {
	t.Parallel()

	env := newLivePhase1Env(t)
	orgID := OrgID(8_600_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))
	productID := fmt.Sprintf("billing-live-phase6-dispute-suspend-%d", time.Now().UnixNano())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	// Set up: org + product + subscription (to be suspended) + small grant.
	if err := env.client.EnsureOrg(ctx, orgID, fmt.Sprintf("org-%d", orgID)); err != nil {
		t.Fatalf("ensure org: %v", err)
	}
	ensureMeteredProduct(t, env.pg, productID)

	piID := fmt.Sprintf("pi_test_phase6_suspend_%d", time.Now().UnixNano())
	seedGrantWithStripeRef(t, env, orgID, productID, piID, 1000)

	// Create an active subscription so SuspendOrg has something to suspend.
	planID := productID + "-plan"
	if _, err := env.pg.ExecContext(ctx, `
		INSERT INTO plans (plan_id, product_id, display_name, unit_rates, active)
		VALUES ($1, $2, 'Phase 6 Plan', '{"unit":1}', true)
		ON CONFLICT (plan_id) DO NOTHING
	`, planID, productID); err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	now := time.Now().UTC()
	var subID int64
	if err := env.pg.QueryRowContext(ctx, `
		INSERT INTO subscriptions (org_id, plan_id, product_id, cadence, current_period_start, current_period_end, status)
		VALUES ($1, $2, $3, 'monthly', $4, $5, 'active')
		RETURNING subscription_id
	`, orgIDStr, planID, productID, now, now.Add(30*24*time.Hour)).Scan(&subID); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}

	// Dispute for 5000 — more than the 1000 available.
	disputeID := fmt.Sprintf("dp_test_phase6_suspend_%d", time.Now().UnixNano())
	disputeAmount := uint64(5000)
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

	// Claim + dispatch.
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

	// Verify task completed.
	var taskStatus string
	if err := env.pg.QueryRowContext(ctx, `
		SELECT status FROM tasks WHERE task_id = $1
	`, taskID).Scan(&taskStatus); err != nil {
		t.Fatalf("query task status: %v", err)
	}
	if taskStatus != "completed" {
		t.Fatalf("expected task status 'completed', got %q", taskStatus)
	}

	// Verify TigerBeetle: grant fully drained (only had 1000, dispute was 5000).
	balance, err := env.client.GetOrgBalance(ctx, orgID)
	if err != nil {
		t.Fatalf("get org balance: %v", err)
	}
	if balance.CreditAvailable != 0 {
		t.Fatalf("expected credit available 0 (drained), got %d", balance.CreditAvailable)
	}

	// Verify subscription is now suspended.
	var subStatus string
	if err := env.pg.QueryRowContext(ctx, `
		SELECT status FROM subscriptions WHERE subscription_id = $1
	`, subID).Scan(&subStatus); err != nil {
		t.Fatalf("query subscription status: %v", err)
	}
	if subStatus != "suspended" {
		t.Fatalf("expected subscription status 'suspended', got %q", subStatus)
	}

	// Verify org_suspended billing event.
	var suspendEventCount int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM billing_events
		WHERE org_id = $1 AND event_type = 'org_suspended'
	`, orgIDStr).Scan(&suspendEventCount); err != nil {
		t.Fatalf("count org_suspended events: %v", err)
	}
	if suspendEventCount != 1 {
		t.Fatalf("expected exactly 1 org_suspended event, got %d", suspendEventCount)
	}

	// Verify dispute_opened event records the shortfall.
	var disputePayload string
	if err := env.pg.QueryRowContext(ctx, `
		SELECT payload::text FROM billing_events
		WHERE org_id = $1 AND event_type = 'dispute_opened' AND task_id = $2
	`, orgIDStr, taskID).Scan(&disputePayload); err != nil {
		t.Fatalf("query dispute event: %v", err)
	}
	var dp map[string]interface{}
	if err := json.Unmarshal([]byte(disputePayload), &dp); err != nil {
		t.Fatalf("parse dispute payload: %v", err)
	}
	if suspended, ok := dp["org_suspended"].(bool); !ok || !suspended {
		t.Fatalf("expected org_suspended=true in dispute payload, got %v", dp["org_suspended"])
	}

	// Verify Reserve returns ErrOrgSuspended.
	env.client.cfg.ReservationWindowSecs = 60
	env.client.cfg.PendingTimeoutSecs = 600
	_, reserveErr := env.client.Reserve(ctx, ReserveRequest{
		JobID:      JobID(time.Now().UTC().UnixNano() % 1_000_000_000),
		OrgID:      orgID,
		ProductID:  productID,
		ActorID:    "actor-dispute-test",
		Allocation: map[string]float64{"unit": 1},
		SourceType: "job",
		SourceRef:  "dispute-test",
	})
	if reserveErr != ErrOrgSuspended {
		t.Fatalf("expected ErrOrgSuspended after dispute suspension, got %v", reserveErr)
	}

	t.Logf("verified dispute insufficient balance → suspension: org_id=%s task_id=%d sub_id=%d drained=1000/5000",
		orgIDStr, taskID, subID)
}

// TestDisputePrioritizesDisputedGrantAgainstLiveHost verifies that grants
// matching the disputed payment's stripe_reference_id are debited first,
// before other org grants.
func TestDisputePrioritizesDisputedGrantAgainstLiveHost(t *testing.T) {
	t.Parallel()

	env := newLivePhase1Env(t)
	orgID := OrgID(8_700_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))
	productID := fmt.Sprintf("billing-live-phase6-priority-%d", time.Now().UnixNano())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	if err := env.client.EnsureOrg(ctx, orgID, fmt.Sprintf("org-%d", orgID)); err != nil {
		t.Fatalf("ensure org: %v", err)
	}
	ensureMeteredProduct(t, env.pg, productID)

	// Seed two grants: one with a different stripe ref (older), one with the disputed ref (newer).
	otherPI := fmt.Sprintf("pi_other_%d", time.Now().UnixNano())
	otherGrant := seedGrantWithStripeRef(t, env, orgID, productID, otherPI, 3000)

	time.Sleep(2 * time.Millisecond) // ensure distinct ULID timestamps

	disputedPI := fmt.Sprintf("pi_disputed_%d", time.Now().UnixNano())
	disputedGrant := seedGrantWithStripeRef(t, env, orgID, productID, disputedPI, 3000)

	// Dispute 2000 against the disputed PI.
	disputeID := fmt.Sprintf("dp_test_phase6_priority_%d", time.Now().UnixNano())
	payload := map[string]interface{}{
		"org_id":                   orgIDStr,
		"stripe_dispute_id":        disputeID,
		"stripe_payment_intent_id": disputedPI,
		"amount_ledger_units":      int64(2000),
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
		t.Fatal("expected to claim a task")
	}

	if err := env.client.dispatchTask(ctx, task); err != nil {
		t.Fatalf("dispatch task: %v", err)
	}
	env.client.completeTask(ctx, task)

	// The disputed grant should be debited 2000, the other grant should be untouched.
	disputedAcct, err := env.tbClient.LookupAccounts([]tbtypes.Uint128{GrantAccountID(disputedGrant.grantID).raw})
	if err != nil || len(disputedAcct) != 1 {
		t.Fatalf("lookup disputed grant account: %v (len=%d)", err, len(disputedAcct))
	}
	disputedAvail, _ := availableFromAccount(disputedAcct[0])
	if disputedAvail != 1000 {
		t.Fatalf("expected disputed grant available=1000 (3000-2000), got %d", disputedAvail)
	}

	otherAcct, err := env.tbClient.LookupAccounts([]tbtypes.Uint128{GrantAccountID(otherGrant.grantID).raw})
	if err != nil || len(otherAcct) != 1 {
		t.Fatalf("lookup other grant account: %v (len=%d)", err, len(otherAcct))
	}
	otherAvail, _ := availableFromAccount(otherAcct[0])
	if otherAvail != 3000 {
		t.Fatalf("expected other grant available=3000 (untouched), got %d", otherAvail)
	}

	t.Logf("verified dispute prioritization: disputed grant=%d other grant=%d",
		disputedAvail, otherAvail)
}

// seedGrantWithStripeRef is like seedGrantForProductTest but sets stripe_reference_id,
// which HandleDispute uses to prioritize the disputed grant.
func seedGrantWithStripeRef(t fatalHelper, env livePhase1Env, orgID OrgID, productID, stripeRefID string, amount uint64) seededGrant {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	grantID := NewGrantID()
	grantIDStr := ulid.ULID(grantID).String()
	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	if _, err := env.pg.ExecContext(ctx, `
		INSERT INTO credit_grants (grant_id, org_id, product_id, amount, source, stripe_reference_id)
		VALUES ($1, $2, $3, $4, 'purchase', $5)
	`, grantIDStr, orgIDStr, productID, int64(amount), stripeRefID); err != nil {
		t.Fatalf("insert grant with stripe ref: %v", err)
	}

	if err := env.client.createGrantAccount(grantID, orgID, SourcePurchase); err != nil {
		t.Fatalf("create grant account: %v", err)
	}

	syntheticTaskID := TaskID(binary.BigEndian.Uint64(grantID[8:16]))
	transfer := tbtypes.Transfer{
		ID:              StripeDepositID(syntheticTaskID, KindStripeDeposit).raw,
		DebitAccountID:  OperatorAccountID(AcctStripeHolding).raw,
		CreditAccountID: GrantAccountID(grantID).raw,
		UserData64:      uint64(orgID),
		Code:            uint16(KindStripeDeposit),
		Ledger:          1,
		Amount:          tbtypes.ToUint128(amount),
	}

	results, err := env.tbClient.CreateTransfers([]tbtypes.Transfer{transfer})
	if err != nil {
		t.Fatalf("fund grant account: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("fund grant: unexpected result %+v", results[0])
	}

	return seededGrant{
		grantID:    grantID,
		orgID:      orgID,
		sourceType: SourcePurchase,
		amount:     amount,
	}
}
