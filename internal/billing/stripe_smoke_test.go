//go:build stripe_smoke

package billing

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stripe/stripe-go/v85"
)

func newStripeSmokeEnv(t *testing.T) *Client {
	t.Helper()

	env := newLivePhase1Env(t)
	stripeKey := os.Getenv("FORGE_METAL_STRIPE_SECRET_KEY")
	if stripeKey == "" {
		t.Skip("set FORGE_METAL_STRIPE_SECRET_KEY")
	}

	env.client.stripe = stripe.NewClient(stripeKey)
	env.client.cfg.StripeSecretKey = stripeKey
	env.client.cfg.StripeWebhookSecret = os.Getenv("FORGE_METAL_STRIPE_WEBHOOK_SECRET")
	if env.client.cfg.StripeWebhookSecret == "" {
		env.client.cfg.StripeWebhookSecret = "whsec_placeholder"
	}

	return env.client
}

func TestStripeSmoke_CreateCheckoutSession(t *testing.T) {
	client := newStripeSmokeEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	orgID := OrgID(9_900_000_000_000_000_001)
	productID := fmt.Sprintf("smoke-checkout-%d", time.Now().UnixNano())

	if err := client.EnsureOrg(ctx, orgID, "smoke-checkout-org"); err != nil {
		t.Fatalf("ensure org: %v", err)
	}
	if _, err := client.pg.ExecContext(ctx, `
		INSERT INTO products (product_id, display_name, meter_unit, billing_model)
		VALUES ($1, 'Smoke Test Credits', 'unit', 'metered')
		ON CONFLICT DO NOTHING
	`, productID); err != nil {
		t.Fatalf("insert product: %v", err)
	}

	url, err := client.CreateCheckoutSession(ctx, orgID, productID, CheckoutParams{
		AmountCents: 1000, // $10.00
		SuccessURL:  "https://example.com/success",
		CancelURL:   "https://example.com/cancel",
	})
	if err != nil {
		t.Fatalf("create checkout session: %v", err)
	}
	if !strings.HasPrefix(url, "https://checkout.stripe.com/") {
		t.Fatalf("expected checkout.stripe.com URL, got %q", url)
	}

	t.Logf("PASS: Checkout session created: %s", url)
}

func TestStripeSmoke_CreateSubscription(t *testing.T) {
	client := newStripeSmokeEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// First, create a real Stripe Price for the test.
	priceParams := &stripe.PriceCreateParams{
		Currency:   stripe.String("usd"),
		UnitAmount: stripe.Int64(2000), // $20/month
		Recurring: &stripe.PriceCreateRecurringParams{
			Interval: stripe.String("month"),
		},
		ProductData: &stripe.PriceCreateProductDataParams{
			Name: stripe.String("Smoke Test Subscription"),
		},
	}
	price, err := client.stripe.V1Prices.Create(ctx, priceParams)
	if err != nil {
		t.Fatalf("create stripe price: %v", err)
	}
	t.Logf("created Stripe price: %s ($%.2f/month)", price.ID, float64(price.UnitAmount)/100)

	orgID := OrgID(9_900_000_000_000_000_002)
	productID := fmt.Sprintf("smoke-sub-%d", time.Now().UnixNano())
	planID := productID + "-plan"

	if err := client.EnsureOrg(ctx, orgID, "smoke-sub-org"); err != nil {
		t.Fatalf("ensure org: %v", err)
	}
	if _, err := client.pg.ExecContext(ctx, `
		INSERT INTO products (product_id, display_name, meter_unit, billing_model)
		VALUES ($1, 'Smoke Sub Product', 'unit', 'metered')
		ON CONFLICT DO NOTHING
	`, productID); err != nil {
		t.Fatalf("insert product: %v", err)
	}
	if _, err := client.pg.ExecContext(ctx, `
		INSERT INTO plans (plan_id, product_id, display_name, stripe_monthly_price_id, unit_rates, active)
		VALUES ($1, $2, 'Smoke Plan', $3, '{"unit": 1}'::jsonb, true)
		ON CONFLICT DO NOTHING
	`, planID, productID, price.ID); err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	url, err := client.CreateSubscription(ctx, orgID, planID, CadenceMonthly, "https://example.com/success", "https://example.com/cancel")
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	if !strings.HasPrefix(url, "https://checkout.stripe.com/") {
		t.Fatalf("expected checkout.stripe.com URL, got %q", url)
	}

	t.Logf("PASS: Subscription checkout session created: %s", url)
}

func TestStripeSmoke_WebhookVerification(t *testing.T) {
	secret := os.Getenv("FORGE_METAL_STRIPE_WEBHOOK_SECRET")
	if secret == "" {
		t.Skip("set FORGE_METAL_STRIPE_WEBHOOK_SECRET")
	}

	// Test that an invalid signature is rejected.
	_, err := VerifyWebhook([]byte(`{"type":"test"}`), "t=1234,v1=badsig", secret)
	if err == nil {
		t.Fatal("expected verification to fail with bad signature")
	}
	t.Logf("PASS: Bad signature correctly rejected: %v", err)
}

func TestStripeSmoke_WorkerPurchaseDepositEndToEnd(t *testing.T) {
	client := newStripeSmokeEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	orgID := OrgID(9_900_000_000_000_000_000 + uint64(time.Now().UTC().UnixNano()%1_000_000))
	orgIDStr := strconv.FormatUint(uint64(orgID), 10)
	productID := fmt.Sprintf("smoke-worker-%d", time.Now().UnixNano())

	if err := client.EnsureOrg(ctx, orgID, "smoke-worker-org"); err != nil {
		t.Fatalf("ensure org: %v", err)
	}
	if _, err := client.pg.ExecContext(ctx, `
		INSERT INTO products (product_id, display_name, meter_unit, billing_model)
		VALUES ($1, 'Worker Smoke Test', 'unit', 'metered')
		ON CONFLICT DO NOTHING
	`, productID); err != nil {
		t.Fatalf("insert product: %v", err)
	}

	// Simulate what the webhook handler does: insert a task.
	piID := fmt.Sprintf("pi_smoke_%d", time.Now().UnixNano())
	expiresAt := time.Now().UTC().AddDate(1, 0, 0)
	payload := mustJSON(map[string]interface{}{
		"org_id":                   orgIDStr,
		"product_id":               productID,
		"stripe_payment_intent_id": piID,
		"amount_ledger_units":      int64(10_000_000), // $1.00
		"expires_at":               expiresAt.Format(time.RFC3339),
	})

	var taskID int64
	if err := client.pg.QueryRowContext(ctx, `
		INSERT INTO tasks (task_type, payload, idempotency_key)
		VALUES ('stripe_purchase_deposit', $1::jsonb, $2)
		RETURNING task_id
	`, payload, piID).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Logf("inserted task %d (idempotency_key=%s)", taskID, piID)

	// Claim and dispatch.
	task, ok, err := client.claimTask(ctx)
	if err != nil || !ok {
		t.Fatalf("claim task: ok=%v err=%v", ok, err)
	}
	if task.TaskID != taskID {
		t.Fatalf("claimed wrong task: expected %d, got %d", taskID, task.TaskID)
	}

	if err := client.dispatchTask(ctx, task); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	client.completeTask(ctx, task)
	t.Logf("task %d dispatched and completed", taskID)

	// Verify: task status = completed.
	var status string
	if err := client.pg.QueryRowContext(ctx, `
		SELECT status FROM tasks WHERE task_id = $1
	`, taskID).Scan(&status); err != nil {
		t.Fatalf("query task: %v", err)
	}
	if status != "completed" {
		t.Fatalf("expected task status 'completed', got %q", status)
	}

	// Verify: grant exists in PG.
	var grantCount int
	if err := client.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM credit_grants
		WHERE org_id = $1 AND product_id = $2 AND source = 'purchase'
	`, orgIDStr, productID).Scan(&grantCount); err != nil {
		t.Fatalf("count grants: %v", err)
	}
	if grantCount != 1 {
		t.Fatalf("expected 1 grant, got %d", grantCount)
	}

	// Verify: TB balance = $1.00 = 10,000,000 ledger units.
	balance, err := client.GetOrgBalance(ctx, orgID)
	if err != nil {
		t.Fatalf("get balance: %v", err)
	}
	if balance.CreditAvailable != 10_000_000 {
		t.Fatalf("expected 10,000,000 ledger units, got %d", balance.CreditAvailable)
	}

	// Verify: billing_event logged.
	var eventCount int
	if err := client.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM billing_events
		WHERE org_id = $1 AND event_type = 'credits_deposited'
	`, orgIDStr).Scan(&eventCount); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if eventCount < 1 {
		t.Fatalf("expected at least 1 credits_deposited event, got %d", eventCount)
	}

	t.Logf("PASS: Full worker pipeline: task=%d → grant created → TB balance=%d → event logged", taskID, balance.CreditAvailable)
}

func TestStripeSmoke_DisputeHandlerEndToEnd(t *testing.T) {
	client := newStripeSmokeEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create an org with a grant, then dispute it.
	orgID := OrgID(9_900_000_000_000_000_004)
	orgIDStr := strconv.FormatUint(uint64(orgID), 10)
	productID := fmt.Sprintf("smoke-dispute-%d", time.Now().UnixNano())

	if err := client.EnsureOrg(ctx, orgID, "smoke-dispute-org"); err != nil {
		t.Fatalf("ensure org: %v", err)
	}
	if _, err := client.pg.ExecContext(ctx, `
		INSERT INTO products (product_id, display_name, meter_unit, billing_model)
		VALUES ($1, 'Dispute Test', 'unit', 'metered')
		ON CONFLICT DO NOTHING
	`, productID); err != nil {
		t.Fatalf("insert product: %v", err)
	}

	// Seed a grant with a known stripe_reference_id (simulating a paid PI).
	piID := fmt.Sprintf("pi_disputed_%d", time.Now().UnixNano())
	taskIDForGrant := TaskID(time.Now().UnixNano() % 1_000_000_000)
	if err := client.DepositCredits(ctx, &taskIDForGrant, CreditGrant{
		OrgID:             orgID,
		ProductID:         productID,
		Amount:            5_000_000, // $0.50
		Source:            "purchase",
		StripeReferenceID: piID,
	}); err != nil {
		t.Fatalf("seed grant: %v", err)
	}

	// Verify balance before dispute.
	balBefore, err := client.GetOrgBalance(ctx, orgID)
	if err != nil {
		t.Fatalf("get balance before: %v", err)
	}
	t.Logf("balance before dispute: %+v", balBefore)

	// Create a subscription so we can check suspension.
	planID := productID + "-plan"
	if _, err := client.pg.ExecContext(ctx, `
		INSERT INTO plans (plan_id, product_id, display_name, unit_rates, active)
		VALUES ($1, $2, 'Dispute Plan', '{"unit":1}'::jsonb, true)
		ON CONFLICT DO NOTHING
	`, planID, productID); err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	if _, err := client.pg.ExecContext(ctx, `
		INSERT INTO subscriptions (org_id, plan_id, product_id, status)
		VALUES ($1, $2, $3, 'active')
	`, orgIDStr, planID, productID); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}

	// Dispute for MORE than the grant balance → should drain + suspend.
	disputeTaskID := TaskID(time.Now().UnixNano()%1_000_000_000 + 1)
	err = client.HandleDispute(ctx, orgID, disputeTaskID, piID, 10_000_000) // $1.00 > $0.50
	if err != nil {
		t.Fatalf("handle dispute: %v", err)
	}

	// Verify: balance is drained.
	balAfter, err := client.GetOrgBalance(ctx, orgID)
	if err != nil {
		t.Fatalf("get balance after: %v", err)
	}
	if balAfter.CreditAvailable != 0 {
		t.Fatalf("expected 0 credit after dispute, got %d", balAfter.CreditAvailable)
	}

	// Verify: org is suspended (subscription status = 'suspended').
	var subStatus string
	if err := client.pg.QueryRowContext(ctx, `
		SELECT status FROM subscriptions WHERE org_id = $1 AND product_id = $2
	`, orgIDStr, productID).Scan(&subStatus); err != nil {
		t.Fatalf("query subscription: %v", err)
	}
	if subStatus != "suspended" {
		t.Fatalf("expected subscription status 'suspended', got %q", subStatus)
	}

	// Verify: billing_event logged.
	var eventCount int
	if err := client.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM billing_events
		WHERE org_id = $1 AND event_type = 'dispute_opened'
	`, orgIDStr).Scan(&eventCount); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if eventCount < 1 {
		t.Fatalf("expected at least 1 dispute_opened event")
	}

	t.Logf("PASS: Dispute handled: balance drained ($0.50 of $1.00 dispute), org suspended, event logged")
}

func TestStripeSmoke_TrustTierEvalDLQ(t *testing.T) {
	client := newStripeSmokeEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	key := fmt.Sprintf("trust_tier_evaluate:smoke_%d", time.Now().UnixNano())
	payload := mustJSON(map[string]interface{}{"as_of_date": "2026-04-05"})

	var taskID int64
	if err := client.pg.QueryRowContext(ctx, `
		INSERT INTO tasks (task_type, payload, idempotency_key, max_attempts)
		VALUES ('trust_tier_evaluate', $1::jsonb, $2, 1)
		RETURNING task_id
	`, payload, key).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	task, ok, err := client.claimTask(ctx)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}

	dispatchErr := client.dispatchTask(ctx, task)
	if dispatchErr != ErrTaskNotImplemented {
		t.Fatalf("expected ErrTaskNotImplemented, got %v", dispatchErr)
	}
	client.failTask(ctx, task, dispatchErr)

	var status string
	if err := client.pg.QueryRowContext(ctx, `
		SELECT status FROM tasks WHERE task_id = $1
	`, taskID).Scan(&status); err != nil {
		t.Fatalf("query task: %v", err)
	}
	if status != "dead" {
		t.Fatalf("expected 'dead', got %q", status)
	}

	t.Logf("PASS: trust_tier_evaluate → DLQ (not yet implemented)")
}
