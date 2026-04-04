package billing

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"strconv"
	"time"

	"github.com/oklog/ulid/v2"
	tb "github.com/tigerbeetle/tigerbeetle-go"
	tbtypes "github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

const billingTestProductID = "billing-test-product"

type fatalHelper interface {
	Helper()
	Fatalf(format string, args ...any)
}

type seededGrant struct {
	grantID    GrantID
	orgID      OrgID
	sourceType GrantSourceType
	amount     uint64
}

func ensureTestProduct(t fatalHelper, db *sql.DB) {
	t.Helper()

	ensureMeteredProduct(t, db, billingTestProductID)
}

func ensureMeteredProduct(t fatalHelper, db *sql.DB, productID string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := db.ExecContext(ctx, `
		INSERT INTO products (product_id, display_name, meter_unit, billing_model)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING
	`, productID, "Billing Test Product", "unit", "metered"); err != nil {
		t.Fatalf("insert test product: %v", err)
	}
}

func seedGrantForTest(t fatalHelper, client *Client, db *sql.DB, tbClient tb.Client, orgID OrgID, sourceType GrantSourceType, amount uint64) seededGrant {
	t.Helper()

	return seedGrantForProductTest(t, client, db, tbClient, orgID, billingTestProductID, sourceType, amount)
}

// seedGrantForProductTest creates a grant using the PG-first ordering from
// the spec: generate ULID → insert PG catalog row → create TB account → fund.
func seedGrantForProductTest(t fatalHelper, client *Client, db *sql.DB, tbClient tb.Client, orgID OrgID, productID string, sourceType GrantSourceType, amount uint64) seededGrant {
	t.Helper()

	ensureMeteredProduct(t, db, productID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.EnsureOrg(ctx, orgID, fmt.Sprintf("org-%d", orgID)); err != nil {
		t.Fatalf("ensure org %d: %v", orgID, err)
	}

	// 1. Generate ULID (application-generated, not database-generated)
	grantID := NewGrantID()
	grantIDStr := ulid.ULID(grantID).String()

	// 2. Insert PG catalog row first (serialization point)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO credit_grants (grant_id, org_id, product_id, amount, source)
		VALUES ($1, $2, $3, $4, $5)
	`, grantIDStr, strconv.FormatUint(uint64(orgID), 10), productID, int64(amount), sourceType.String()); err != nil {
		t.Fatalf("insert grant row: %v", err)
	}

	// 3. Create TB account (TigerBeetle last)
	if err := client.createGrantAccount(grantID, orgID, sourceType); err != nil {
		t.Fatalf("create grant account: %v", err)
	}

	// 4. Fund the account. Derive a unique transfer ID from the ULID's
	// random tail so each seeded grant gets its own transfer.
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

	results, err := tbClient.CreateTransfers([]tbtypes.Transfer{transfer})
	if err != nil {
		t.Fatalf("fund grant account: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("fund grant account: unexpected create result %+v", results[0])
	}

	return seededGrant{
		grantID:    grantID,
		orgID:      orgID,
		sourceType: sourceType,
		amount:     amount,
	}
}

func requireGrantAccount(t fatalHelper, tbClient tb.Client, grant seededGrant) {
	t.Helper()

	accounts, err := tbClient.LookupAccounts([]tbtypes.Uint128{GrantAccountID(grant.grantID).raw})
	if err != nil {
		t.Fatalf("lookup grant account: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected 1 grant account, got %d", len(accounts))
	}

	account := accounts[0]
	if account.Code != AcctGrantCode {
		t.Fatalf("grant %x: expected code %d, got %d", grant.grantID, AcctGrantCode, account.Code)
	}
	if account.Ledger != 1 {
		t.Fatalf("grant %x: expected ledger 1, got %d", grant.grantID, account.Ledger)
	}
	if account.UserData64 != uint64(grant.orgID) {
		t.Fatalf("grant %x: expected user_data_64 %d, got %d", grant.grantID, grant.orgID, account.UserData64)
	}
	if account.UserData32 != uint32(grant.sourceType) {
		t.Fatalf("grant %x: expected user_data_32 %d, got %d", grant.grantID, grant.sourceType, account.UserData32)
	}
	if flags := account.AccountFlags(); flags != (tbtypes.AccountFlags{DebitsMustNotExceedCredits: true, History: true}) {
		t.Fatalf("grant %x: unexpected flags %+v", grant.grantID, flags)
	}

	available, err := availableFromAccount(account)
	if err != nil {
		t.Fatalf("grant %x available: %v", grant.grantID, err)
	}
	if available != grant.amount {
		t.Fatalf("grant %x: expected available %d, got %d", grant.grantID, grant.amount, available)
	}

	pending, err := pendingFromAccount(account)
	if err != nil {
		t.Fatalf("grant %x pending: %v", grant.grantID, err)
	}
	if pending != 0 {
		t.Fatalf("grant %x: expected pending 0, got %d", grant.grantID, pending)
	}

	consumed, err := consumedFromAccount(account)
	if err != nil {
		t.Fatalf("grant %x consumed: %v", grant.grantID, err)
	}
	if consumed != 0 {
		t.Fatalf("grant %x: expected consumed 0, got %d", grant.grantID, consumed)
	}
}

func requireGrantBalance(t fatalHelper, tbClient tb.Client, grantID GrantID, wantAvailable, wantPending, wantConsumed uint64) {
	t.Helper()

	accounts, err := tbClient.LookupAccounts([]tbtypes.Uint128{GrantAccountID(grantID).raw})
	if err != nil {
		t.Fatalf("lookup grant account %x: %v", grantID, err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected 1 grant account for %x, got %d", grantID, len(accounts))
	}

	account := accounts[0]
	available, err := availableFromAccount(account)
	if err != nil {
		t.Fatalf("grant %x available: %v", grantID, err)
	}
	pending, err := pendingFromAccount(account)
	if err != nil {
		t.Fatalf("grant %x pending: %v", grantID, err)
	}
	consumed, err := consumedFromAccount(account)
	if err != nil {
		t.Fatalf("grant %x consumed: %v", grantID, err)
	}

	if available != wantAvailable || pending != wantPending || consumed != wantConsumed {
		t.Fatalf(
			"grant %x: expected available=%d pending=%d consumed=%d, got available=%d pending=%d consumed=%d",
			grantID,
			wantAvailable,
			wantPending,
			wantConsumed,
			available,
			pending,
			consumed,
		)
	}
}
