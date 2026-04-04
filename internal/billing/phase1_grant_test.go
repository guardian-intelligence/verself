package billing

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := db.ExecContext(ctx, `
		INSERT INTO products (product_id, display_name, meter_unit, billing_model)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING
	`, billingTestProductID, "Billing Test Product", "unit", "metered"); err != nil {
		t.Fatalf("insert test product: %v", err)
	}
}

func seedGrantForTest(t fatalHelper, client *Client, db *sql.DB, tbClient tb.Client, orgID OrgID, sourceType GrantSourceType, amount uint64) seededGrant {
	t.Helper()

	ensureTestProduct(t, db)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.EnsureOrg(ctx, orgID, fmt.Sprintf("org-%d", orgID)); err != nil {
		t.Fatalf("ensure org %d: %v", orgID, err)
	}

	var grantID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO credit_grants (org_id, product_id, amount, source)
		VALUES ($1, $2, $3, $4)
		RETURNING grant_id
	`, strconv.FormatUint(uint64(orgID), 10), billingTestProductID, int64(amount), sourceType.String()).Scan(&grantID); err != nil {
		t.Fatalf("insert grant row: %v", err)
	}

	if err := client.createGrantAccount(GrantID(grantID), orgID, sourceType); err != nil {
		t.Fatalf("create grant account: %v", err)
	}

	transfer := tbtypes.Transfer{
		ID:              StripeDepositID(TaskID(grantID), KindStripeDeposit).raw,
		DebitAccountID:  OperatorAccountID(AcctStripeHolding).raw,
		CreditAccountID: GrantAccountID(GrantID(grantID)).raw,
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
		grantID:    GrantID(grantID),
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
	if account.Code != uint16(AcctGrant) {
		t.Fatalf("grant %d: expected code %d, got %d", grant.grantID, AcctGrant, account.Code)
	}
	if account.Ledger != 1 {
		t.Fatalf("grant %d: expected ledger 1, got %d", grant.grantID, account.Ledger)
	}
	if account.UserData64 != uint64(grant.orgID) {
		t.Fatalf("grant %d: expected user_data_64 %d, got %d", grant.grantID, grant.orgID, account.UserData64)
	}
	if account.UserData32 != uint32(grant.sourceType) {
		t.Fatalf("grant %d: expected user_data_32 %d, got %d", grant.grantID, grant.sourceType, account.UserData32)
	}
	if flags := account.AccountFlags(); flags != (tbtypes.AccountFlags{DebitsMustNotExceedCredits: true, History: true}) {
		t.Fatalf("grant %d: unexpected flags %+v", grant.grantID, flags)
	}

	available, err := availableFromAccount(account)
	if err != nil {
		t.Fatalf("grant %d available: %v", grant.grantID, err)
	}
	if available != grant.amount {
		t.Fatalf("grant %d: expected available %d, got %d", grant.grantID, grant.amount, available)
	}

	pending, err := pendingFromAccount(account)
	if err != nil {
		t.Fatalf("grant %d pending: %v", grant.grantID, err)
	}
	if pending != 0 {
		t.Fatalf("grant %d: expected pending 0, got %d", grant.grantID, pending)
	}

	consumed, err := consumedFromAccount(account)
	if err != nil {
		t.Fatalf("grant %d consumed: %v", grant.grantID, err)
	}
	if consumed != 0 {
		t.Fatalf("grant %d: expected consumed 0, got %d", grant.grantID, consumed)
	}
}
