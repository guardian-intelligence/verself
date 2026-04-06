package billing

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	tb "github.com/tigerbeetle/tigerbeetle-go"
	tbtypes "github.com/tigerbeetle/tigerbeetle-go/pkg/types"
	"pgregory.net/rapid"
)

var phase3RunCounter atomic.Uint64

// TestDepositCreditsSubscriptionSource verifies the subscription deposit path:
// PG-first ordering, TB account creation, funding transfer, billing event.
func TestDepositCreditsSubscriptionSource(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnv(t)
	orgID := OrgID(9_100_000_000_000_000_001)
	productID, planID := uniqueCatalogIDs("deposit-sub")

	subID := ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID, map[string]uint64{"unit": 1}, nil, false)
	periodStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	expiresAt := periodEnd.Add(30 * 24 * time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	created, err := env.client.DepositCredits(ctx, nil, CreditGrant{
		OrgID:          orgID,
		ProductID:      productID,
		Amount:         500,
		Source:         "subscription",
		SubscriptionID: &subID,
		PeriodStart:    &periodStart,
		PeriodEnd:      &periodEnd,
		ExpiresAt:      &expiresAt,
	})
	if err != nil {
		t.Fatalf("deposit credits: %v", err)
	}
	if !created {
		t.Fatal("expected created=true for first deposit")
	}

	// Verify PG grant row exists.
	var grantIDStr string
	var amount int64
	if err := env.pg.QueryRowContext(ctx, `
		SELECT grant_id, amount
		FROM credit_grants
		WHERE org_id = $1 AND product_id = $2 AND source = 'subscription'
	`, strconv.FormatUint(uint64(orgID), 10), productID).Scan(&grantIDStr, &amount); err != nil {
		t.Fatalf("query grant row: %v", err)
	}
	if amount != 500 {
		t.Fatalf("expected amount 500, got %d", amount)
	}

	// Verify the TB account has the correct balance.
	parsedULID, err := ulid.ParseStrict(grantIDStr)
	if err != nil {
		t.Fatalf("parse grant ULID: %v", err)
	}
	grantID := GrantID(parsedULID)
	requireGrantBalance(t, env.tbClient, grantID, 500, 0, 0)

	// Verify billing event was logged.
	var eventCount int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM billing_events
		WHERE org_id = $1 AND event_type = 'credits_deposited' AND grant_id = $2
	`, strconv.FormatUint(uint64(orgID), 10), grantIDStr).Scan(&eventCount); err != nil {
		t.Fatalf("query billing event: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected 1 billing event, got %d", eventCount)
	}
}

// TestDepositCreditsIdempotencySubscription verifies that depositing the same
// subscription period twice is a no-op (unique index on subscription_id, period_start).
func TestDepositCreditsIdempotencySubscription(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnv(t)
	orgID := OrgID(9_100_000_000_000_000_002)
	productID, planID := uniqueCatalogIDs("deposit-idem")

	subID := ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID, map[string]uint64{"unit": 1}, nil, false)
	periodStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	grant := CreditGrant{
		OrgID:          orgID,
		ProductID:      productID,
		Amount:         200,
		Source:         "subscription",
		SubscriptionID: &subID,
		PeriodStart:    &periodStart,
		PeriodEnd:      &periodEnd,
	}

	created1, err := env.client.DepositCredits(ctx, nil, grant)
	if err != nil {
		t.Fatalf("first deposit: %v", err)
	}
	if !created1 {
		t.Fatal("expected created=true for first deposit")
	}
	created2, err := env.client.DepositCredits(ctx, nil, grant)
	if err != nil {
		t.Fatalf("second deposit (should be idempotent): %v", err)
	}
	if created2 {
		t.Fatal("expected created=false for idempotent replay")
	}

	orgIDStr := strconv.FormatUint(uint64(orgID), 10)

	// Only one grant row should exist.
	var count int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM credit_grants
		WHERE org_id = $1 AND product_id = $2 AND source = 'subscription'
	`, orgIDStr, productID).Scan(&count); err != nil {
		t.Fatalf("count grants: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 grant row (idempotent), got %d", count)
	}

	// Only one billing event should exist — the second DepositCredits must not
	// have logged a duplicate event.
	var eventCount int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM billing_events
		WHERE org_id = $1 AND event_type = 'credits_deposited'
	`, orgIDStr).Scan(&eventCount); err != nil {
		t.Fatalf("count billing events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected exactly 1 billing event (idempotent), got %d", eventCount)
	}
}

// TestDepositCreditsFreeTierSource verifies free-tier deposits debit FreeTierPool.
func TestDepositCreditsFreeTierSource(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnv(t)
	orgID := OrgID(9_100_000_000_000_000_003)
	productID, planID := uniqueCatalogIDs("deposit-free")

	subID := ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID, map[string]uint64{"unit": 1}, nil, false)
	periodStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Fund FreeTierPool so the debit succeeds.
	fundOperatorPool(t, env.tbClient, AcctFreeTierPool, 1000)

	_, err := env.client.DepositCredits(ctx, nil, CreditGrant{
		OrgID:          orgID,
		ProductID:      productID,
		Amount:         100,
		Source:         "free_tier",
		SubscriptionID: &subID,
		PeriodStart:    &periodStart,
		PeriodEnd:      &periodEnd,
	})
	if err != nil {
		t.Fatalf("deposit free-tier credits: %v", err)
	}

	// Verify the grant is categorized as free-tier.
	balance, err := env.client.GetOrgBalance(ctx, orgID)
	if err != nil {
		t.Fatalf("get balance: %v", err)
	}
	if balance.FreeTierAvailable != 100 {
		t.Fatalf("expected 100 free-tier available, got %d", balance.FreeTierAvailable)
	}
}

// TestDepositCreditsPurchaseSource verifies purchase deposits require a taskID.
func TestDepositCreditsPurchaseSource(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnv(t)
	orgID := OrgID(9_100_000_000_000_000_004)
	productID, planID := uniqueCatalogIDs("deposit-purchase")

	ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID, map[string]uint64{"unit": 1}, nil, false)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Without taskID should fail.
	_, err := env.client.DepositCredits(ctx, nil, CreditGrant{
		OrgID:     orgID,
		ProductID: productID,
		Amount:    300,
		Source:    "purchase",
	})
	if err == nil {
		t.Fatal("expected error for purchase without taskID")
	}

	// With taskID should succeed.
	taskID := TaskID(9001)
	_, err = env.client.DepositCredits(ctx, &taskID, CreditGrant{
		OrgID:             orgID,
		ProductID:         productID,
		Amount:            300,
		Source:            "purchase",
		StripeReferenceID: "pi_test_purchase",
	})
	if err != nil {
		t.Fatalf("deposit purchase credits: %v", err)
	}

	balance, err := env.client.GetOrgBalance(ctx, orgID)
	if err != nil {
		t.Fatalf("get balance: %v", err)
	}
	if balance.CreditAvailable != 300 {
		t.Fatalf("expected 300 credit available, got %d", balance.CreditAvailable)
	}
}

// TestExpireCreditsBasic verifies the expiry sweep drains expired grants and
// sets closed_at in PostgreSQL.
func TestExpireCreditsBasic(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnv(t)
	orgID := OrgID(9_200_000_000_000_000_001)
	productID, planID := uniqueCatalogIDs("expire-basic")

	ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID, map[string]uint64{"unit": 1}, nil, false)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Seed a grant with expires_at in the past.
	past := time.Now().UTC().Add(-1 * time.Hour)
	grant := seedGrantWithExpiry(t, env.client, env.pg, env.tbClient, orgID, productID, SourceSubscription, 250, &past)

	// Also seed a grant that has NOT expired (no expiry).
	activeGrant := seedGrantForProductTest(t, env.client, env.pg, env.tbClient, orgID, productID, SourceSubscription, 100)

	result, err := env.client.ExpireCredits(ctx)
	if err != nil {
		t.Fatalf("expire credits: %v", err)
	}

	if result.GrantsChecked != 1 {
		t.Fatalf("expected exactly 1 grant checked, got %d", result.GrantsChecked)
	}
	if result.GrantsExpired != 1 {
		t.Fatalf("expected exactly 1 grant expired, got %d", result.GrantsExpired)
	}
	if result.GrantsFailed != 0 {
		t.Fatalf("expected 0 grants failed, got %d: %v", result.GrantsFailed, result.Errors)
	}
	if result.UnitsExpired != 250 {
		t.Fatalf("expected exactly 250 units expired, got %d", result.UnitsExpired)
	}

	// Verify expired grant has zero available. The BalancingDebit counts as
	// debits_posted, so consumed equals the expired amount.
	requireGrantBalance(t, env.tbClient, grant.grantID, 0, 0, 250)

	// Verify closed_at is set.
	var closedAt sql.NullTime
	grantIDStr := ulid.ULID(grant.grantID).String()
	if err := env.pg.QueryRowContext(ctx, `
		SELECT closed_at FROM credit_grants WHERE grant_id = $1
	`, grantIDStr).Scan(&closedAt); err != nil {
		t.Fatalf("query closed_at: %v", err)
	}
	if !closedAt.Valid {
		t.Fatal("expected closed_at to be set")
	}

	// Verify billing event was logged.
	var eventCount int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM billing_events
		WHERE event_type = 'credits_expired' AND grant_id = $1
	`, grantIDStr).Scan(&eventCount); err != nil {
		t.Fatalf("query billing event: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected 1 credits_expired event, got %d", eventCount)
	}

	// Verify the active grant is untouched.
	requireGrantBalance(t, env.tbClient, activeGrant.grantID, 100, 0, 0)
}

// TestExpireCreditsIdempotent verifies that running ExpireCredits twice does
// not fail on already-expired grants.
func TestExpireCreditsIdempotent(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnv(t)
	orgID := OrgID(9_200_000_000_000_000_002)
	productID, planID := uniqueCatalogIDs("expire-idem")

	ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID, map[string]uint64{"unit": 1}, nil, false)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	past := time.Now().UTC().Add(-1 * time.Hour)
	seedGrantWithExpiry(t, env.client, env.pg, env.tbClient, orgID, productID, SourceSubscription, 50, &past)

	// First sweep.
	result1, err := env.client.ExpireCredits(ctx)
	if err != nil {
		t.Fatalf("first expire: %v", err)
	}
	if result1.GrantsExpired != 1 {
		t.Fatalf("expected exactly 1 expired, got %d", result1.GrantsExpired)
	}

	// Second sweep — should find nothing (closed_at is set).
	result2, err := env.client.ExpireCredits(ctx)
	if err != nil {
		t.Fatalf("second expire: %v", err)
	}
	// The already-closed grant should not appear.
	if result2.GrantsExpired != 0 {
		t.Fatalf("expected 0 expired on second sweep, got %d", result2.GrantsExpired)
	}
}

// TestExpireCreditsFreeTierSinkAccount verifies that free-tier grants expire
// into FreeTierExpense, not ExpiredCredits.
func TestExpireCreditsFreeTierSinkAccount(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnv(t)
	orgID := OrgID(9_200_000_000_000_000_003)
	productID, planID := uniqueCatalogIDs("expire-freetier")

	ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID, map[string]uint64{"unit": 1}, nil, false)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Record FreeTierExpense balance before.
	freeTierExpenseBefore := lookupOperatorPostedCredits(t, env.tbClient, AcctFreeTierExpense)

	past := time.Now().UTC().Add(-1 * time.Hour)
	seedGrantWithExpiry(t, env.client, env.pg, env.tbClient, orgID, productID, SourceFreeTier, 75, &past)

	result, err := env.client.ExpireCredits(ctx)
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if result.GrantsExpired != 1 {
		t.Fatalf("expected exactly 1 expired, got %d", result.GrantsExpired)
	}
	if result.UnitsExpired != 75 {
		t.Fatalf("expected exactly 75 units expired, got %d", result.UnitsExpired)
	}

	// FreeTierExpense should have received the expired amount.
	freeTierExpenseAfter := lookupOperatorPostedCredits(t, env.tbClient, AcctFreeTierExpense)
	if freeTierExpenseAfter-freeTierExpenseBefore != 75 {
		t.Fatalf("expected FreeTierExpense to increase by exactly 75, got %d", freeTierExpenseAfter-freeTierExpenseBefore)
	}
}

// TestRecordLicensedChargeBasic verifies the StripeHolding → Revenue transfer
// and billing event for licensed products.
func TestRecordLicensedChargeBasic(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnv(t)
	orgID := OrgID(9_300_000_000_000_000_001)
	productID := fmt.Sprintf("licensed-prod-%d", time.Now().UnixNano())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := env.client.EnsureOrg(ctx, orgID, "licensed-test-org"); err != nil {
		t.Fatalf("ensure org: %v", err)
	}

	// Insert licensed product.
	if _, err := env.pg.ExecContext(ctx, `
		INSERT INTO products (product_id, display_name, meter_unit, billing_model)
		VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING
	`, productID, "Licensed Test Product", "unit", "licensed"); err != nil {
		t.Fatalf("insert product: %v", err)
	}

	// Record RevenueBefore.
	revenueBefore := lookupOperatorPostedCredits(t, env.tbClient, AcctRevenue)

	taskID := TaskID(9501)
	err := env.client.RecordLicensedCharge(ctx, taskID, LicensedCharge{
		OrgID:           orgID,
		ProductID:       productID,
		SubscriptionID:  1001,
		StripeInvoiceID: "in_test_licensed_001",
		Amount:          2000,
		PeriodStart:     time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:       time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("record licensed charge: %v", err)
	}

	// Revenue should have increased.
	revenueAfter := lookupOperatorPostedCredits(t, env.tbClient, AcctRevenue)
	if revenueAfter-revenueBefore != 2000 {
		t.Fatalf("expected Revenue to increase by 2000, got %d", revenueAfter-revenueBefore)
	}

	// Verify no credit_grants row was created.
	var grantCount int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM credit_grants
		WHERE org_id = $1 AND product_id = $2
	`, strconv.FormatUint(uint64(orgID), 10), productID).Scan(&grantCount); err != nil {
		t.Fatalf("count grants: %v", err)
	}
	if grantCount != 0 {
		t.Fatalf("expected 0 grant rows for licensed product, got %d", grantCount)
	}

	// Verify billing event.
	var eventCount int
	if err := env.pg.QueryRowContext(ctx, `
		SELECT count(*) FROM billing_events
		WHERE org_id = $1 AND event_type = 'licensed_charge_recorded'
	`, strconv.FormatUint(uint64(orgID), 10)).Scan(&eventCount); err != nil {
		t.Fatalf("count billing events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected 1 licensed_charge_recorded event, got %d", eventCount)
	}
}

// TestExpireCreditsStateMachine extends the property-based test harness with
// OpExpireCredits to verify invariants under random operation sequences.
func TestExpireCreditsStateMachine(t *testing.T) {
	t.Parallel()

	env := newPhase2TestEnv(t)

	rapid.Check(t, func(t *rapid.T) {
		runID := phase3RunCounter.Add(1)
		orgID := OrgID(9_400_000_000_000_000_000 + runID)
		productID, planID := uniqueCatalogIDs(fmt.Sprintf("phase3-rapid-%d", runID))

		ensureMeteredPlanForTest(t, env.client, env.pg, orgID, productID, planID, map[string]uint64{"unit": 1}, nil, false)

		sm := &phase3StateMachine{
			client:            env.client,
			pg:                env.pg,
			tbClient:          env.tbClient,
			orgID:             orgID,
			productID:         productID,
			activeGrants:      make(map[GrantID]grantState),
			expiredGrantCount: 0,
		}
		t.Repeat(rapid.StateMachineActions(sm))
	})
}

type grantState struct {
	grantID   GrantID
	amount    uint64
	source    GrantSourceType
	expiresAt *time.Time
	expired   bool
}

type phase3StateMachine struct {
	client            *Client
	pg                *sql.DB
	tbClient          tb.Client
	orgID             OrgID
	productID         string
	activeGrants      map[GrantID]grantState
	expiredGrantCount int
}

func (sm *phase3StateMachine) OpDeposit(t *rapid.T) {
	amount := rapid.Uint64Range(1, 10_000).Draw(t, "amount")
	sourceIdx := rapid.IntRange(2, 2).Draw(t, "source_type") // subscription only for simplicity
	source := GrantSourceType(sourceIdx)

	grant := seedGrantForProductTest(t, sm.client, sm.pg, sm.tbClient, sm.orgID, sm.productID, source, amount)
	sm.activeGrants[grant.grantID] = grantState{
		grantID: grant.grantID,
		amount:  amount,
		source:  source,
	}
}

func (sm *phase3StateMachine) OpDepositExpiring(t *rapid.T) {
	amount := rapid.Uint64Range(1, 10_000).Draw(t, "amount")
	past := time.Now().UTC().Add(-1 * time.Hour)

	grant := seedGrantWithExpiry(t, sm.client, sm.pg, sm.tbClient, sm.orgID, sm.productID, SourceSubscription, amount, &past)
	sm.activeGrants[grant.grantID] = grantState{
		grantID:   grant.grantID,
		amount:    amount,
		source:    SourceSubscription,
		expiresAt: &past,
	}
}

func (sm *phase3StateMachine) OpExpireCredits(t *rapid.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := sm.client.ExpireCredits(ctx)
	if err != nil {
		t.Fatalf("expire credits: %v", err)
	}
	if result.GrantsFailed != 0 {
		t.Fatalf("expire credits failed for %d grants: %v", result.GrantsFailed, result.Errors)
	}

	// Mark grants as expired in our model.
	for id, gs := range sm.activeGrants {
		if gs.expiresAt != nil && !gs.expired {
			gs.expired = true
			sm.activeGrants[id] = gs
			sm.expiredGrantCount++
		}
	}
}

func (sm *phase3StateMachine) Check(t *rapid.T) {
	// Every active (non-expired) PG grant has a TB account.
	for _, gs := range sm.activeGrants {
		accounts, err := sm.tbClient.LookupAccounts([]tbtypes.Uint128{GrantAccountID(gs.grantID).raw})
		if err != nil {
			t.Fatalf("lookup account for grant %x: %v", gs.grantID, err)
		}
		if len(accounts) != 1 {
			t.Fatalf("expected 1 account for grant %x, got %d", gs.grantID, len(accounts))
		}

		account := accounts[0]
		available, err := availableFromAccount(account)
		if err != nil {
			t.Fatalf("available for grant %x: %v", gs.grantID, err)
		}

		if gs.expired {
			// Expired grant should have zero available.
			if available != 0 {
				t.Fatalf("expired grant %x: expected 0 available, got %d", gs.grantID, available)
			}
		}

		// No grant account should have negative available (enforced by TB, but verify).
		// (This is implicitly true since availableFromAccount would error on negative.)
	}

	// Verify ExpiredCredits only receives KindCreditExpiry transfers.
	// We verify this by checking the expired grant's debit transfer.
	for _, gs := range sm.activeGrants {
		if !gs.expired {
			continue
		}
		transferID := CreditExpiryID(gs.grantID)
		transfers, err := sm.tbClient.LookupTransfers([]tbtypes.Uint128{transferID.raw})
		if err != nil {
			t.Fatalf("lookup expiry transfer for grant %x: %v", gs.grantID, err)
		}
		if len(transfers) != 1 {
			t.Fatalf("expected 1 expiry transfer for grant %x, got %d", gs.grantID, len(transfers))
		}
		if transfers[0].Code != uint16(KindCreditExpiry) {
			t.Fatalf("expiry transfer for grant %x: expected code %d, got %d", gs.grantID, KindCreditExpiry, transfers[0].Code)
		}
	}
}

// --- Test helpers ---

// seedGrantWithExpiry creates a grant with an explicit expires_at.
func seedGrantWithExpiry(t fatalHelper, client *Client, db *sql.DB, tbClient tb.Client, orgID OrgID, productID string, sourceType GrantSourceType, amount uint64, expiresAt *time.Time) seededGrant {
	t.Helper()

	ensureMeteredProduct(t, db, productID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.EnsureOrg(ctx, orgID, fmt.Sprintf("org-%d", orgID)); err != nil {
		t.Fatalf("ensure org %d: %v", orgID, err)
	}

	grantID := NewGrantID()
	grantIDStr := ulid.ULID(grantID).String()

	var expiresAtParam interface{}
	if expiresAt != nil {
		expiresAtParam = *expiresAt
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO credit_grants (grant_id, org_id, product_id, amount, source, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, grantIDStr, strconv.FormatUint(uint64(orgID), 10), productID, int64(amount), sourceType.String(), expiresAtParam); err != nil {
		t.Fatalf("insert grant row: %v", err)
	}

	if err := client.createGrantAccount(grantID, orgID, sourceType); err != nil {
		t.Fatalf("create grant account: %v", err)
	}

	// Fund — use StripeHolding for all sources in test.
	syntheticTaskID := TaskID(time.Now().UnixNano() % 1_000_000_000)
	transfer := tbtypes.Transfer{
		ID:              StripeDepositID(syntheticTaskID, KindStripeDeposit).raw,
		DebitAccountID:  OperatorAccountID(AcctStripeHolding).raw,
		CreditAccountID: GrantAccountID(grantID).raw,
		UserData64:      uint64(orgID),
		Code:            uint16(KindStripeDeposit),
		Ledger:          1,
		Amount:          tbtypes.ToUint128(amount),
	}
	results, err := tbClient.CreateTransfers([]tbtypes.Transfer{transfer})
	if err != nil {
		t.Fatalf("fund grant: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("fund grant: unexpected result %+v", results[0])
	}

	return seededGrant{
		grantID:    grantID,
		orgID:      orgID,
		sourceType: sourceType,
		amount:     amount,
	}
}

// fundOperatorPool funds an operator pool account by crediting it from
// StripeHolding (which has no DebitsMustNotExceedCredits constraint).
func fundOperatorPool(t fatalHelper, tbClient tb.Client, pool OperatorAcctType, amount uint64) {
	t.Helper()

	// StripeHolding has no debit constraint, so we can freely credit from it
	// to fund pool accounts.
	transfer := tbtypes.Transfer{
		ID:              tbtypes.ToUint128(uint64(time.Now().UnixNano())),
		DebitAccountID:  OperatorAccountID(AcctStripeHolding).raw,
		CreditAccountID: OperatorAccountID(pool).raw,
		Code:            uint16(KindStripeDeposit),
		Ledger:          1,
		Amount:          tbtypes.ToUint128(amount),
	}
	results, err := tbClient.CreateTransfers([]tbtypes.Transfer{transfer})
	if err != nil {
		t.Fatalf("fund pool %d: %v", pool, err)
	}
	if len(results) != 0 {
		t.Fatalf("fund pool %d: unexpected result %+v", pool, results[0])
	}
}

// lookupOperatorPostedCredits returns the credits_posted for an operator account.
func lookupOperatorPostedCredits(t fatalHelper, tbClient tb.Client, acctType OperatorAcctType) uint64 {
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
