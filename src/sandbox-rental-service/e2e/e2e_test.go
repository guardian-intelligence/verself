// End-to-end tests that start real PG, TigerBeetle, and ClickHouse processes,
// wire billing-service and sandbox-rental-service in-process via testharness
// packages, and exercise the full authenticated rental flow.
package e2e_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	billingclient "github.com/forge-metal/billing-service/client"
	billingtestharness "github.com/forge-metal/billing-service/testharness"
	rentaltestharness "github.com/forge-metal/sandbox-rental-service/testharness"
)

const (
	testOrgID    uint64 = 999001
	testUserID          = "user-e2e-test"
	testAudience        = "sandbox-project"
)

func TestSandboxRentalFullFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e tests require real databases")
	}

	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	// ---- 1. Infrastructure ----

	pg := startPostgresForE2E(t)
	tbAddr, tbClient, tbClusterID := startTigerBeetleForE2E(t)
	chConn, _ := startClickHouseForE2E(t)
	authProvider := newTestAuthProvider(t)
	defer authProvider.Close()
	stripeKeys := requireStripeTestKeys(t)

	// ---- 2. Billing service (in-process) ----

	billingServer := billingtestharness.NewServer(billingtestharness.Config{
		PG:              pg.billingDB,
		TBClient:        tbClient,
		TBAddresses:     []string{tbAddr},
		TBClusterID:     tbClusterID,
		CHConn:          chConn,
		CHDatabase:      "forge_metal",
		StripeSecretKey: stripeKeys.SecretKey,
		Logger:          logger,
	})
	defer billingServer.Close()
	t.Logf("billing-service at %s", billingServer.URL)

	workerCtx, workerCancel := context.WithCancel(ctx)
	defer workerCancel()
	go billingServer.RunWorker(workerCtx, 200*time.Millisecond)

	// ---- 3. Seed test data ----

	if err := billingServer.SeedOrg(ctx, testOrgID, "E2E Test Org"); err != nil {
		t.Fatalf("ensure org: %v", err)
	}

	if _, err := pg.billingDB.ExecContext(ctx, `
		INSERT INTO products (product_id, display_name, meter_unit, billing_model)
		VALUES ('sandbox', 'Sandbox', 'vcpu_second', 'metered')
		ON CONFLICT DO NOTHING
	`); err != nil {
		t.Fatalf("insert product: %v", err)
	}

	// Plan with known unit rates: vcpu=100, gib=50 ledger-units/sec.
	// With BillingVCPUs=2 and BillingMemMiB=2048 (2 GiB):
	//   CostPerSec = 2*100 + 2*50 = 300
	if _, err := pg.billingDB.ExecContext(ctx, `
		INSERT INTO plans (plan_id, product_id, display_name, included_credits, unit_rates, is_default, active)
		VALUES ('sandbox-default', 'sandbox', 'Sandbox PAYG', 0, '{"vcpu":100,"gib":50}'::jsonb, true, true)
		ON CONFLICT DO NOTHING
	`); err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	const seedCredits uint64 = 5_000_000
	expiresAt := time.Now().Add(24 * time.Hour)
	created, err := billingServer.SeedCredits(ctx, testOrgID, "sandbox", seedCredits, "purchase", "e2e-test-seed", expiresAt)
	if err != nil {
		t.Fatalf("deposit credits: %v", err)
	}
	if !created {
		t.Fatal("expected new grant, got idempotent replay")
	}

	balanceBefore, _, err := billingServer.GetBalance(ctx, testOrgID)
	if err != nil {
		t.Fatalf("get balance: %v", err)
	}
	if balanceBefore != seedCredits {
		t.Fatalf("expected credit_available=%d, got %d", seedCredits, balanceBefore)
	}
	t.Logf("balance before: credit_available=%d", balanceBefore)

	// ---- 4. Sandbox rental service (in-process) ----

	billingHTTPClient, err := billingclient.New(billingServer.URL)
	if err != nil {
		t.Fatalf("create billing HTTP client: %v", err)
	}

	runner := &fakeRunner{
		delay: 1500 * time.Millisecond, // 1.5s → actualSeconds=1
		logs:  "hello from e2e\n",
	}

	rentalServer := rentaltestharness.NewServer(rentaltestharness.Config{
		PG:            pg.rentalDB,
		CH:            chConn,
		CHDatabase:    "forge_metal",
		Runner:        runner,
		Billing:       billingHTTPClient,
		BillingVCPUs:  2,
		BillingMemMiB: 2048,
		AuthCfg:       authProvider.authConfig(testAudience),
		Logger:        logger,
	})
	defer rentalServer.Close()
	t.Logf("sandbox-rental-service at %s", rentalServer.URL)

	// ---- 5. Sign JWT ----

	token := authProvider.signToken(t, jwt.MapClaims{
		"iss":                                   authProvider.URL,
		"sub":                                   testUserID,
		"aud":                                   []string{testAudience},
		"exp":                                   time.Now().Add(time.Hour).Unix(),
		"urn:zitadel:iam:user:resourceowner:id": strconv.FormatUint(testOrgID, 10),
	})

	// ---- 6. Submit sandbox job ----

	submitBody := `{"repo_url":"https://github.com/example/test-repo"}`
	req, _ := http.NewRequest("POST", rentalServer.URL+"/api/v1/jobs", strings.NewReader(submitBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("submit job: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		body, _ := json.Marshal(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, body)
	}

	var submitResp struct {
		JobID  string `json:"job_id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&submitResp); err != nil {
		t.Fatalf("decode submit response: %v", err)
	}
	if submitResp.JobID == "" {
		t.Fatal("empty job_id in response")
	}
	if submitResp.Status != "running" {
		t.Fatalf("expected status=running, got %q", submitResp.Status)
	}
	t.Logf("job submitted: %s", submitResp.JobID)

	// ---- 7. Poll for completion ----

	type jobPollResult struct {
		Status     string `json:"status"`
		ExitCode   int    `json:"exit_code"`
		DurationMs int64  `json:"duration_ms"`
	}

	var jobResult jobPollResult
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		pollReq, _ := http.NewRequest("GET", rentalServer.URL+"/api/v1/jobs/"+submitResp.JobID, nil)
		pollReq.Header.Set("Authorization", "Bearer "+token)
		pollResp, err := http.DefaultClient.Do(pollReq)
		if err != nil {
			t.Fatalf("poll job: %v", err)
		}
		_ = json.NewDecoder(pollResp.Body).Decode(&jobResult)
		pollResp.Body.Close()
		if jobResult.Status != "running" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// ---- 8. Assert: job completed ----

	if jobResult.Status != "completed" {
		t.Fatalf("expected status=completed, got %q", jobResult.Status)
	}
	if jobResult.ExitCode != 0 {
		t.Fatalf("expected exit_code=0, got %d", jobResult.ExitCode)
	}
	t.Logf("job completed: exit_code=%d duration_ms=%d", jobResult.ExitCode, jobResult.DurationMs)

	// ---- 9. Assert: TigerBeetle credits consumed ----

	balanceAfter, _, err := billingServer.GetBalance(ctx, testOrgID)
	if err != nil {
		t.Fatalf("get balance after: %v", err)
	}
	if balanceAfter >= balanceBefore {
		t.Fatalf("credits not consumed: before=%d after=%d", balanceBefore, balanceAfter)
	}
	consumed := balanceBefore - balanceAfter

	// CostPerSec = 2*100 + 2*50 = 300, actualSeconds = 1 → 300 units
	const expectedCost uint64 = 300
	if consumed != expectedCost {
		t.Fatalf("expected %d credits consumed, got %d (before=%d after=%d)",
			expectedCost, consumed, balanceBefore, balanceAfter)
	}
	t.Logf("credits consumed: %d (expected %d)", consumed, expectedCost)

	// ---- 10. Assert: PG job record ----

	var pgStatus string
	if err := pg.rentalDB.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = $1`, submitResp.JobID).Scan(&pgStatus); err != nil {
		t.Fatalf("query job record: %v", err)
	}
	if pgStatus != "completed" {
		t.Fatalf("expected PG status=completed, got %q", pgStatus)
	}

	// ---- 11. Assert: ClickHouse metering ----

	flushCtx, flushCancel := context.WithTimeout(ctx, 5*time.Second)
	defer flushCancel()
	if err := billingServer.FlushMetering(flushCtx); err != nil {
		t.Logf("metering writer close (non-fatal): %v", err)
	}
	time.Sleep(500 * time.Millisecond) // allow CH to flush

	var meteringCount uint64
	orgIDStr := strconv.FormatUint(testOrgID, 10)
	if err := chConn.QueryRow(ctx,
		"SELECT count() FROM forge_metal.metering WHERE org_id = $1 AND product_id = $2",
		orgIDStr, "sandbox",
	).Scan(&meteringCount); err != nil {
		t.Fatalf("query metering: %v", err)
	}
	if meteringCount < 1 {
		t.Fatalf("expected at least 1 metering row, got %d", meteringCount)
	}
	t.Logf("metering rows: %d", meteringCount)

	// ---- 12. Assert: ClickHouse sandbox_job_events ----

	var eventCount uint64
	if err := chConn.QueryRow(ctx,
		"SELECT count() FROM forge_metal.sandbox_job_events WHERE org_id = $1",
		testOrgID,
	).Scan(&eventCount); err != nil {
		t.Fatalf("query sandbox_job_events: %v", err)
	}
	if eventCount < 1 {
		t.Fatalf("expected at least 1 sandbox_job_event, got %d", eventCount)
	}
	t.Logf("sandbox_job_events: %d", eventCount)

	t.Logf("PASS: full sandbox rental flow verified (credits=%d->%d, consumed=%d, metering=%d, events=%d)",
		balanceBefore, balanceAfter, consumed, meteringCount, eventCount)
}
