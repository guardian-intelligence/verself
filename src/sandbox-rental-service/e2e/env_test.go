package e2e_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	billingclient "github.com/forge-metal/billing-service/client"
	billingtestharness "github.com/forge-metal/billing-service/testharness"
	rentaltestharness "github.com/forge-metal/sandbox-rental-service/testharness"
)

type sandboxE2EEnv struct {
	ctx           context.Context
	pg            pgEnv
	billingServer *billingtestharness.Server
	billingClient *billingclient.ServiceClient
	rentalServer  *rentaltestharness.Server
	authProvider  *testAuthProvider
	queryCHConn   chdriver.Conn
	rentalCHConn  chdriver.Conn
}

type anyQueryRower interface {
	QueryRow(context.Context, string, ...any) chdriver.Row
}

func startSandboxE2EEnv(t *testing.T, runner *fakeRunner) *sandboxE2EEnv {
	t.Helper()

	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	pg := startPostgresForE2E(t)
	tbAddr, tbClient, tbClusterID := startTigerBeetleForE2E(t)
	billingCHConn, chAddress := startClickHouseForE2E(t)
	queryCHConn, err := openClickHouseConn(chAddress)
	if err != nil {
		t.Fatalf("open query clickhouse conn: %v", err)
	}
	authProvider := newTestAuthProvider(t)
	stripeKeys := requireStripeTestKeys(t)

	billingServer := billingtestharness.NewServer(billingtestharness.Config{
		PG:              pg.billingDB,
		TBClient:        tbClient,
		TBAddresses:     []string{tbAddr},
		TBClusterID:     tbClusterID,
		CHConn:          billingCHConn,
		CHDatabase:      "forge_metal",
		StripeSecretKey: stripeKeys.SecretKey,
		Logger:          logger,
	})

	workerCtx, workerCancel := context.WithCancel(ctx)
	go func() {
		if err := billingServer.RunProjector(workerCtx, 200*time.Millisecond); err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("billing projector: %v", err)
		}
	}()
	t.Cleanup(workerCancel)

	seedSandboxProductAndCredits(t, ctx, pg.billingDB, billingServer)

	billingHTTPClient, err := billingclient.New(billingServer.URL)
	if err != nil {
		t.Fatalf("create billing HTTP client: %v", err)
	}

	rentalCHConn, err := openClickHouseConn(chAddress)
	if err != nil {
		t.Fatalf("open rental clickhouse conn: %v", err)
	}
	rentalServer := rentaltestharness.NewServer(rentaltestharness.Config{
		PG:               pg.rentalDB,
		CH:               rentalCHConn,
		CHDatabase:       "forge_metal",
		Runner:           runner,
		Billing:          billingHTTPClient,
		WebhookSecretKEK: "0000000000000000000000000000000000000000000000000000000000000000",
		BillingVCPUs:     2,
		BillingMemMiB:    2048,
		AuthCfg:          authProvider.authConfig(testAudience),
		Logger:           logger,
	})

	return &sandboxE2EEnv{
		ctx:           ctx,
		pg:            pg,
		billingServer: billingServer,
		billingClient: billingHTTPClient,
		rentalServer:  rentalServer,
		authProvider:  authProvider,
		queryCHConn:   queryCHConn,
		rentalCHConn:  rentalCHConn,
	}
}

func (e *sandboxE2EEnv) close() {
	e.rentalServer.Close()
	e.billingServer.Close()
	e.authProvider.Close()
	_ = e.queryCHConn.Close()
	_ = e.rentalCHConn.Close()
}

func seedSandboxProductAndCredits(t *testing.T, ctx context.Context, billingDB *sql.DB, billingServer *billingtestharness.Server) uint64 {
	t.Helper()

	if err := billingServer.SeedOrg(ctx, testOrgID, "E2E Test Org", "new"); err != nil {
		t.Fatalf("ensure org: %v", err)
	}

	if _, err := billingDB.ExecContext(ctx, `
		INSERT INTO products (product_id, display_name, meter_unit, billing_model, reserve_policy)
		VALUES ('sandbox', 'Sandbox', 'vcpu_second', 'metered', '{"shape":"time","target_quantity":300,"min_quantity":1,"allow_partial_reserve":true,"renew_slack_quantity":30}'::jsonb)
		ON CONFLICT DO NOTHING
	`); err != nil {
		t.Fatalf("insert product: %v", err)
	}

	if _, err := billingDB.ExecContext(ctx, `
		INSERT INTO plans (plan_id, product_id, display_name, billing_mode, included_credits, unit_rates, is_default, active)
		VALUES ('sandbox-default', 'sandbox', 'Sandbox PAYG', 'prepaid', 0, '{"vcpu":100,"gib":50}'::jsonb, true, true)
		ON CONFLICT DO NOTHING
	`); err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	const seedCredits uint64 = 5_000_000
	expiresAt := time.Now().Add(24 * time.Hour)
	if _, err := billingServer.SeedCredits(ctx, testOrgID, "sandbox", seedCredits, "purchase", "sandbox-e2e-seed", expiresAt); err != nil {
		t.Fatalf("deposit credits: %v", err)
	}
	balanceBefore, _, err := billingServer.GetBalance(ctx, testOrgID)
	if err != nil {
		t.Fatalf("get balance: %v", err)
	}
	if balanceBefore != seedCredits {
		t.Fatalf("expected credit_available=%d, got %d", seedCredits, balanceBefore)
	}
	return balanceBefore
}

func flushBillingMetering(t *testing.T, ctx context.Context, billingServer *billingtestharness.Server) {
	t.Helper()
	flushCtx, flushCancel := context.WithTimeout(ctx, 5*time.Second)
	defer flushCancel()
	if _, err := billingServer.ProjectPendingWindows(flushCtx); err != nil {
		t.Fatalf("project billing windows: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
}
