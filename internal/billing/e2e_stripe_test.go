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
	"github.com/stripe/stripe-go/v85"
	tbtypes "github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

// TestE2EFullCreditLifecycleWithStripe exercises the entire credit lifecycle
// against live TigerBeetle, PostgreSQL, ClickHouse, and Stripe test mode:
//
//  1. Stripe: create Customer + Product + Price + Invoice, pay the invoice
//  2. DepositCredits (subscription source): fund a grant from the paid invoice
//  3. Reserve + Settle: consume part of the grant, write metering to ClickHouse
//  4. DepositCredits (purchase source): fund a second grant from a PaymentIntent
//  5. ExpireCredits: expire a short-lived grant and verify drain
//  6. RecordLicensedCharge: recognize a licensed invoice as revenue
//  7. Verify: TB balances, PG catalog, CH metering, billing_events audit trail
//
// Requires env vars:
//
//	FORGE_METAL_BILLING_LIVE_PG_DSN
//	FORGE_METAL_BILLING_LIVE_TB_ADDRESS
//	FORGE_METAL_BILLING_LIVE_TB_CLUSTER_ID
//	FORGE_METAL_BILLING_LIVE_CH_ADDR
//	FORGE_METAL_BILLING_LIVE_CH_PASSWORD
//	FORGE_METAL_E2E_STRIPE_SECRET_KEY
func TestE2EFullCreditLifecycleWithStripe(t *testing.T) {
	stripeKey := os.Getenv("FORGE_METAL_E2E_STRIPE_SECRET_KEY")
	chAddr := os.Getenv("FORGE_METAL_BILLING_LIVE_CH_ADDR")
	chPassword := os.Getenv("FORGE_METAL_BILLING_LIVE_CH_PASSWORD")
	if stripeKey == "" || chAddr == "" || chPassword == "" {
		t.Skip("set FORGE_METAL_E2E_STRIPE_SECRET_KEY, FORGE_METAL_BILLING_LIVE_CH_ADDR, FORGE_METAL_BILLING_LIVE_CH_PASSWORD")
	}

	env := newLivePhase1Env(t)
	sc := stripe.NewClient(stripeKey)

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

	env.client.SetMeteringWriter(NewClickHouseMeteringWriter(chConn, "forge_metal"))
	env.client.cfg.ReservationWindowSecs = 60
	env.client.cfg.PendingTimeoutSecs = 600

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ts := time.Now().UTC().Unix()
	orgID := OrgID(8_000_000_000_000_000_000 + uint64(ts%1_000_000))
	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	if err := env.client.EnsureOrg(ctx, orgID, fmt.Sprintf("e2e-stripe-%d", ts)); err != nil {
		t.Fatalf("ensure org: %v", err)
	}
	t.Logf("step 0: org %d provisioned in PG", orgID)

	// ---------------------------------------------------------------
	// Step 1: Stripe — create Customer, Product, Price, Invoice, pay it
	// ---------------------------------------------------------------
	customer, err := sc.V1Customers.Create(ctx, &stripe.CustomerCreateParams{
		Name:  stripe.String(fmt.Sprintf("e2e-test-org-%d", ts)),
		Email: stripe.String(fmt.Sprintf("e2e-%d@forge-metal.test", ts)),
	})
	if err != nil {
		t.Fatalf("stripe create customer: %v", err)
	}
	t.Logf("step 1a: stripe customer %s", customer.ID)

	stripeProduct, err := sc.V1Products.Create(ctx, &stripe.ProductCreateParams{
		Name: stripe.String(fmt.Sprintf("e2e-metered-%d", ts)),
	})
	if err != nil {
		t.Fatalf("stripe create product: %v", err)
	}
	t.Logf("step 1b: stripe product %s", stripeProduct.ID)

	// Create invoice item + invoice, then finalize and pay.
	_, err = sc.V1InvoiceItems.Create(ctx, &stripe.InvoiceItemCreateParams{
		Customer: stripe.String(customer.ID),
		Amount:   stripe.Int64(2000),
		Currency: stripe.String("usd"),
	})
	if err != nil {
		t.Fatalf("stripe create invoice item: %v", err)
	}

	dueDate := time.Now().Add(24 * time.Hour).Unix()
	invoice, err := sc.V1Invoices.Create(ctx, &stripe.InvoiceCreateParams{
		Customer:         stripe.String(customer.ID),
		CollectionMethod: stripe.String("send_invoice"),
		DueDate:          stripe.Int64(dueDate),
	})
	if err != nil {
		t.Fatalf("stripe create invoice: %v", err)
	}
	t.Logf("step 1d: stripe invoice %s (draft)", invoice.ID)

	invoice, err = sc.V1Invoices.FinalizeInvoice(ctx, invoice.ID, &stripe.InvoiceFinalizeInvoiceParams{})
	if err != nil {
		t.Fatalf("stripe finalize invoice: %v", err)
	}

	// In test mode, finalize may auto-pay. Only call Pay if still open.
	if invoice.Status != "paid" {
		invoice, err = sc.V1Invoices.Pay(ctx, invoice.ID, &stripe.InvoicePayParams{
			PaidOutOfBand: stripe.Bool(true),
		})
		if err != nil {
			t.Fatalf("stripe pay invoice: %v", err)
		}
	}
	if invoice.Status != "paid" {
		t.Fatalf("expected invoice status paid, got %s", invoice.Status)
	}
	t.Logf("step 1e: stripe invoice %s paid (status=%s)", invoice.ID, invoice.Status)

	// ---------------------------------------------------------------
	// Step 2: DepositCredits — subscription source from the paid invoice
	// ---------------------------------------------------------------
	meteredProductID := fmt.Sprintf("e2e-metered-prod-%d", ts)
	meteredPlanID := meteredProductID + "-plan"

	subID := ensureMeteredPlanForTest(t, env.client, env.pg, orgID, meteredProductID, meteredPlanID,
		map[string]uint64{"unit": 1}, nil, false)

	periodStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	expiresAt := periodEnd.Add(30 * 24 * time.Hour)

	err = env.client.DepositCredits(ctx, nil, CreditGrant{
		OrgID:             orgID,
		ProductID:         meteredProductID,
		Amount:            500,
		Source:            "subscription",
		StripeReferenceID: invoice.ID,
		SubscriptionID:    &subID,
		PeriodStart:       &periodStart,
		PeriodEnd:         &periodEnd,
		ExpiresAt:         &expiresAt,
	})
	if err != nil {
		t.Fatalf("deposit subscription credits: %v", err)
	}

	// Read back the grant from PG to verify.
	var subGrantIDStr string
	if err := env.pg.QueryRowContext(ctx, `
		SELECT grant_id FROM credit_grants
		WHERE org_id = $1 AND product_id = $2 AND source = 'subscription'
	`, orgIDStr, meteredProductID).Scan(&subGrantIDStr); err != nil {
		t.Fatalf("query sub grant: %v", err)
	}
	subGrantULID, _ := ulid.ParseStrict(subGrantIDStr)
	subGrantID := GrantID(subGrantULID)
	requireGrantBalance(t, env.tbClient, subGrantID, 500, 0, 0)
	t.Logf("step 2: subscription deposit grant=%s amount=500 stripe_invoice=%s", subGrantIDStr, invoice.ID)

	// Verify idempotency.
	err = env.client.DepositCredits(ctx, nil, CreditGrant{
		OrgID:             orgID,
		ProductID:         meteredProductID,
		Amount:            500,
		Source:            "subscription",
		StripeReferenceID: invoice.ID,
		SubscriptionID:    &subID,
		PeriodStart:       &periodStart,
		PeriodEnd:         &periodEnd,
		ExpiresAt:         &expiresAt,
	})
	if err != nil {
		t.Fatalf("idempotent subscription deposit: %v", err)
	}
	requireGrantBalance(t, env.tbClient, subGrantID, 500, 0, 0) // unchanged

	// ---------------------------------------------------------------
	// Step 3: Reserve + Settle — write metering to ClickHouse
	// ---------------------------------------------------------------
	jobID := JobID(time.Now().UTC().UnixNano() % 1_000_000_000)
	sourceRef := strconv.FormatInt(int64(jobID), 10)

	reservation, err := env.client.Reserve(ctx, ReserveRequest{
		JobID:      jobID,
		OrgID:      orgID,
		ProductID:  meteredProductID,
		ActorID:    "e2e-user",
		Allocation: map[string]float64{"unit": 1},
		SourceType: "job",
		SourceRef:  sourceRef,
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if reservation.PricingPhase != PricingPhaseIncluded {
		t.Fatalf("expected included phase, got %s", reservation.PricingPhase)
	}

	if err := env.client.Settle(ctx, &reservation, 45); err != nil {
		t.Fatalf("settle: %v", err)
	}
	requireGrantBalance(t, env.tbClient, subGrantID, 455, 0, 45)
	t.Logf("step 3: reserve+settle job_id=%d actual_seconds=45 cost=45 remaining=455", jobID)

	// Verify ClickHouse metering row.
	var chChargeUnits uint64
	var chPricingPhase string
	var chSubUnits uint64
	if err := chConn.QueryRow(ctx, `
		SELECT charge_units, pricing_phase, subscription_units
		FROM forge_metal.metering
		WHERE org_id = $1 AND product_id = $2 AND source_ref = $3
		LIMIT 1
	`, orgIDStr, meteredProductID, sourceRef).Scan(&chChargeUnits, &chPricingPhase, &chSubUnits); err != nil {
		t.Fatalf("query metering: %v", err)
	}
	if chChargeUnits != 45 {
		t.Fatalf("metering charge_units: expected 45, got %d", chChargeUnits)
	}
	if chPricingPhase != "included" {
		t.Fatalf("metering pricing_phase: expected included, got %s", chPricingPhase)
	}
	if chSubUnits != 45 {
		t.Fatalf("metering subscription_units: expected 45, got %d", chSubUnits)
	}
	t.Logf("step 3 verified: ClickHouse metering row charge_units=%d pricing_phase=%s", chChargeUnits, chPricingPhase)

	// ---------------------------------------------------------------
	// Step 4: DepositCredits — purchase source (task-based idempotency)
	// ---------------------------------------------------------------
	// Insert a task row simulating a stripe_purchase_deposit webhook handler.
	var purchaseTaskID int64
	if err := env.pg.QueryRowContext(ctx, `
		INSERT INTO tasks (task_type, idempotency_key, payload, status)
		VALUES ('stripe_purchase_deposit', $1, $2::jsonb, 'claimed')
		RETURNING task_id
	`, fmt.Sprintf("pi_%d_e2e_purchase", ts),
		mustJSON(map[string]interface{}{
			"org_id":     orgIDStr,
			"product_id": meteredProductID,
			"amount":     200,
		}),
	).Scan(&purchaseTaskID); err != nil {
		t.Fatalf("insert purchase task: %v", err)
	}

	taskID := TaskID(purchaseTaskID)
	err = env.client.DepositCredits(ctx, &taskID, CreditGrant{
		OrgID:             orgID,
		ProductID:         meteredProductID,
		Amount:            200,
		Source:            "purchase",
		StripeReferenceID: fmt.Sprintf("pi_%d_e2e_purchase", ts),
	})
	if err != nil {
		t.Fatalf("deposit purchase credits: %v", err)
	}

	// Verify purchase grant.
	var purchaseGrantIDStr string
	if err := env.pg.QueryRowContext(ctx, `
		SELECT grant_id FROM credit_grants
		WHERE org_id = $1 AND product_id = $2 AND source = 'purchase'
	`, orgIDStr, meteredProductID).Scan(&purchaseGrantIDStr); err != nil {
		t.Fatalf("query purchase grant: %v", err)
	}
	purchaseULID, _ := ulid.ParseStrict(purchaseGrantIDStr)
	purchaseGrantID := GrantID(purchaseULID)
	requireGrantBalance(t, env.tbClient, purchaseGrantID, 200, 0, 0)
	t.Logf("step 4: purchase deposit grant=%s amount=200 task_id=%d", purchaseGrantIDStr, purchaseTaskID)

	// ---------------------------------------------------------------
	// Step 5: ExpireCredits — expire a short-lived grant
	// ---------------------------------------------------------------
	past := time.Now().UTC().Add(-1 * time.Hour)
	expiringGrant := seedGrantWithExpiry(t, env.client, env.pg, env.tbClient, orgID, meteredProductID, SourceSubscription, 100, &past)

	expireResult, err := env.client.ExpireCredits(ctx)
	if err != nil {
		t.Fatalf("expire credits: %v", err)
	}
	if expireResult.GrantsFailed != 0 {
		t.Fatalf("expire credits: %d failures: %v", expireResult.GrantsFailed, expireResult.Errors)
	}
	if expireResult.GrantsExpired < 1 {
		t.Fatalf("expected at least 1 grant expired, got %d", expireResult.GrantsExpired)
	}

	// Verify expired grant is drained and closed.
	requireGrantBalance(t, env.tbClient, expiringGrant.grantID, 0, 0, 100)
	var closedAt sql.NullTime
	expiringIDStr := ulid.ULID(expiringGrant.grantID).String()
	if err := env.pg.QueryRowContext(ctx, `
		SELECT closed_at FROM credit_grants WHERE grant_id = $1
	`, expiringIDStr).Scan(&closedAt); err != nil {
		t.Fatalf("query closed_at: %v", err)
	}
	if !closedAt.Valid {
		t.Fatal("expected closed_at to be set on expired grant")
	}

	// Verify the ExpiredCredits operator account received the units.
	expiryTransfer, err := env.client.lookupTransfer(CreditExpiryID(expiringGrant.grantID))
	if err != nil {
		t.Fatalf("lookup expiry transfer: %v", err)
	}
	expiredAmount, _ := uint128ToUint64(expiryTransfer.Amount)
	if expiredAmount != 100 {
		t.Fatalf("expected 100 units expired, got %d", expiredAmount)
	}
	t.Logf("step 5: expired grant=%s units=%d", expiringIDStr, expiredAmount)

	// ---------------------------------------------------------------
	// Step 6: RecordLicensedCharge — licensed invoice recognition
	// ---------------------------------------------------------------
	licensedProductID := fmt.Sprintf("e2e-licensed-prod-%d", ts)
	if _, err := env.pg.ExecContext(ctx, `
		INSERT INTO products (product_id, display_name, meter_unit, billing_model)
		VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING
	`, licensedProductID, "E2E Licensed Product", "unit", "licensed"); err != nil {
		t.Fatalf("insert licensed product: %v", err)
	}

	// Use a unique task for the licensed charge.
	var licensedTaskID int64
	if err := env.pg.QueryRowContext(ctx, `
		INSERT INTO tasks (task_type, idempotency_key, payload, status)
		VALUES ('stripe_licensed_charge', $1, $2::jsonb, 'claimed')
		RETURNING task_id
	`, invoice.ID,
		mustJSON(map[string]interface{}{
			"org_id":            orgIDStr,
			"product_id":        licensedProductID,
			"stripe_invoice_id": invoice.ID,
			"amount":            2000,
		}),
	).Scan(&licensedTaskID); err != nil {
		t.Fatalf("insert licensed task: %v", err)
	}

	// Record Revenue balance before.
	revenueBefore := lookupOperatorCreditsPosted(t, env.tbClient, AcctRevenue)

	err = env.client.RecordLicensedCharge(ctx, TaskID(licensedTaskID), LicensedCharge{
		OrgID:           orgID,
		ProductID:       licensedProductID,
		SubscriptionID:  subID,
		StripeInvoiceID: invoice.ID,
		Amount:          2000,
		PeriodStart:     periodStart,
		PeriodEnd:       periodEnd,
	})
	if err != nil {
		t.Fatalf("record licensed charge: %v", err)
	}

	revenueAfter := lookupOperatorCreditsPosted(t, env.tbClient, AcctRevenue)
	if revenueAfter-revenueBefore != 2000 {
		t.Fatalf("Revenue: expected increase of 2000, got %d", revenueAfter-revenueBefore)
	}
	t.Logf("step 6: licensed charge amount=2000 stripe_invoice=%s task_id=%d revenue_delta=%d",
		invoice.ID, licensedTaskID, revenueAfter-revenueBefore)

	// ---------------------------------------------------------------
	// Step 7: Final verification — billing_events audit trail
	// ---------------------------------------------------------------
	eventTypes := []string{
		"credits_deposited",
		"credits_expired",
		"licensed_charge_recorded",
	}
	for _, et := range eventTypes {
		var count int
		if err := env.pg.QueryRowContext(ctx, `
			SELECT count(*) FROM billing_events WHERE org_id = $1 AND event_type = $2
		`, orgIDStr, et).Scan(&count); err != nil {
			t.Fatalf("count %s events: %v", et, err)
		}
		if count < 1 {
			t.Fatalf("expected at least 1 %s event for org %s, got %d", et, orgIDStr, count)
		}
		t.Logf("step 7: billing_events %s count=%d", et, count)
	}

	// Verify total grant count.
	var totalGrants int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM credit_grants WHERE org_id = $1
	`, orgIDStr).Scan(&totalGrants); err != nil {
		t.Fatalf("count grants: %v", err)
	}
	// 3 grants: subscription deposit, purchase deposit, expired grant
	if totalGrants != 3 {
		t.Fatalf("expected 3 total grants, got %d", totalGrants)
	}

	t.Logf("E2E PASS: org=%d stripe_customer=%s stripe_invoice=%s "+
		"grants=3 metering_row=confirmed revenue_delta=2000 expired_units=100",
		orgID, customer.ID, invoice.ID)
}

func lookupOperatorCreditsPosted(t fatalHelper, tbClient interface{ LookupAccounts([]tbtypes.Uint128) ([]tbtypes.Account, error) }, acctType OperatorAcctType) uint64 {
	t.Helper()

	accounts, err := tbClient.LookupAccounts([]tbtypes.Uint128{OperatorAccountID(acctType).raw})
	if err != nil {
		t.Fatalf("lookup operator account %d: %v", acctType, err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected 1 operator account %d, got %d", acctType, len(accounts))
	}
	v, err := uint128ToUint64(accounts[0].CreditsPosted)
	if err != nil {
		t.Fatalf("credits_posted overflow: %v", err)
	}
	return v
}
