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

	_ "github.com/lib/pq"
	"github.com/stripe/stripe-go/v85"
	tb "github.com/tigerbeetle/tigerbeetle-go"
	tbtypes "github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

type livePhase1Env struct {
	client   *Client
	pg       *sql.DB
	tbClient tb.Client
}

func TestEnsureOrgAgainstLiveHost(t *testing.T) {
	t.Parallel()

	env := newLivePhase1Env(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	orgID := OrgID(7_000_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))
	displayName := fmt.Sprintf("billing-live-phase1-%d", time.Now().UTC().Unix())
	if err := env.client.EnsureOrg(ctx, orgID, displayName); err != nil {
		t.Fatalf("ensure org %d: %v", orgID, err)
	}
	t.Logf("verified ensure_org for org_id=%d", orgID)

	var count int
	orgIDText := strconv.FormatUint(uint64(orgID), 10)
	if err := env.pg.QueryRowContext(ctx, `SELECT count(*) FROM orgs WHERE org_id = $1`, orgIDText).Scan(&count); err != nil {
		t.Fatalf("query org row: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one org row for %s, got %d", orgIDText, count)
	}

	requireLiveAccounts(t, env.tbClient, []struct {
		id    AccountID
		code  uint16
		flags tbtypes.AccountFlags
	}{
		{id: OperatorAccountID(AcctRevenue), code: uint16(AcctRevenue), flags: tbtypes.AccountFlags{DebitsMustNotExceedCredits: true, History: true}},
		{id: OperatorAccountID(AcctFreeTierPool), code: uint16(AcctFreeTierPool), flags: tbtypes.AccountFlags{DebitsMustNotExceedCredits: true, History: true}},
		{id: OperatorAccountID(AcctStripeHolding), code: uint16(AcctStripeHolding), flags: tbtypes.AccountFlags{History: true}},
		{id: OperatorAccountID(AcctPromoPool), code: uint16(AcctPromoPool), flags: tbtypes.AccountFlags{DebitsMustNotExceedCredits: true, History: true}},
		{id: OperatorAccountID(AcctFreeTierExpense), code: uint16(AcctFreeTierExpense), flags: tbtypes.AccountFlags{History: true}},
		{id: OperatorAccountID(AcctExpiredCredits), code: uint16(AcctExpiredCredits), flags: tbtypes.AccountFlags{History: true}},
	}, 0, 0)
}

func TestGrantBalanceAgainstLiveHost(t *testing.T) {
	t.Parallel()

	env := newLivePhase1Env(t)
	orgID := OrgID(7_100_000_000_000_000_000 + uint64(time.Now().UTC().Unix()%1_000_000))

	freeTierGrant := seedGrantForTest(t, env.client, env.pg, env.tbClient, orgID, SourceFreeTier, 125)
	subscriptionGrant := seedGrantForTest(t, env.client, env.pg, env.tbClient, orgID, SourceSubscription, 275)

	requireGrantAccount(t, env.tbClient, freeTierGrant)
	requireGrantAccount(t, env.tbClient, subscriptionGrant)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	balance, err := env.client.GetOrgBalance(ctx, orgID)
	if err != nil {
		t.Fatalf("get org balance: %v", err)
	}
	expected := Balance{
		FreeTierAvailable: 125,
		CreditAvailable:   275,
		TotalAvailable:    400,
	}
	if balance != expected {
		t.Fatalf("expected balance %+v, got %+v", expected, balance)
	}
	t.Logf("verified grant balances for org_id=%d free_tier=%d credit=%d", orgID, expected.FreeTierAvailable, expected.CreditAvailable)
}

func newLivePhase1Env(t *testing.T) livePhase1Env {
	t.Helper()

	pgDSN := os.Getenv("FORGE_METAL_BILLING_LIVE_PG_DSN")
	tbAddress := os.Getenv("FORGE_METAL_BILLING_LIVE_TB_ADDRESS")
	clusterIDText := os.Getenv("FORGE_METAL_BILLING_LIVE_TB_CLUSTER_ID")
	if pgDSN == "" || tbAddress == "" || clusterIDText == "" {
		t.Skip("set FORGE_METAL_BILLING_LIVE_PG_DSN, FORGE_METAL_BILLING_LIVE_TB_ADDRESS, and FORGE_METAL_BILLING_LIVE_TB_CLUSTER_ID")
	}

	clusterID, err := strconv.ParseUint(clusterIDText, 10, 64)
	if err != nil {
		t.Fatalf("parse FORGE_METAL_BILLING_LIVE_TB_CLUSTER_ID: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pg, err := sql.Open("postgres", pgDSN)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() {
		_ = pg.Close()
	})
	if err := pg.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}

	tbClient, err := tb.NewClient(tbtypes.ToUint128(clusterID), []string{tbAddress})
	if err != nil {
		t.Fatalf("create tigerbeetle client: %v", err)
	}
	t.Cleanup(tbClient.Close)

	cfg := DefaultConfig()
	cfg.PgDSN = pgDSN
	cfg.StripeSecretKey = "sk_test_placeholder"
	cfg.StripeWebhookSecret = "whsec_test_placeholder"
	cfg.TigerBeetleAddresses = []string{tbAddress}
	cfg.TigerBeetleClusterID = clusterID

	client, err := NewClient(tbClient, pg, stripe.NewClient(cfg.StripeSecretKey), cfg)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	return livePhase1Env{
		client:   client,
		pg:       pg,
		tbClient: tbClient,
	}
}

func requireLiveAccounts(t *testing.T, tbClient tb.Client, expected []struct {
	id    AccountID
	code  uint16
	flags tbtypes.AccountFlags
}, userData64 uint64, userData32 uint32,
) {
	t.Helper()

	accountIDs := make([]tbtypes.Uint128, 0, len(expected))
	for _, item := range expected {
		accountIDs = append(accountIDs, item.id.raw)
	}

	accounts, err := tbClient.LookupAccounts(accountIDs)
	if err != nil {
		t.Fatalf("lookup accounts: %v", err)
	}
	if len(accounts) != len(expected) {
		t.Fatalf("expected %d accounts, got %d", len(expected), len(accounts))
	}

	byID := make(map[tbtypes.Uint128]tbtypes.Account, len(accounts))
	for _, account := range accounts {
		byID[account.ID] = account
	}

	for _, item := range expected {
		account, ok := byID[item.id.raw]
		if !ok {
			t.Fatalf("missing account %v", item.id.raw)
		}
		if account.Code != item.code {
			t.Fatalf("account %v: expected code %d, got %d", item.id.raw, item.code, account.Code)
		}
		if account.Ledger != 1 {
			t.Fatalf("account %v: expected ledger 1, got %d", item.id.raw, account.Ledger)
		}
		if account.UserData64 != userData64 {
			t.Fatalf("account %v: expected user_data_64 %d, got %d", item.id.raw, userData64, account.UserData64)
		}
		if account.UserData32 != userData32 {
			t.Fatalf("account %v: expected user_data_32 %d, got %d", item.id.raw, userData32, account.UserData32)
		}
		if flags := account.AccountFlags(); flags != item.flags {
			t.Fatalf("account %v: expected flags %+v, got %+v", item.id.raw, item.flags, flags)
		}
	}
}
