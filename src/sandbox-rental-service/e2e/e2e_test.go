// End-to-end tests that start real PG, TigerBeetle, and ClickHouse processes,
// wire billing-service and sandbox-rental-service in-process via testharness
// packages, and exercise the execution control plane end to end.
package e2e_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

func TestExecutionDirectFullFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e tests require real databases")
	}

	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	// ---- 1. Infrastructure ----

	pg := startPostgresForE2E(t)
	tbAddr, tbClient, tbClusterID := startTigerBeetleForE2E(t)
	billingCHConn, chAddress := startClickHouseForE2E(t)
	rentalCHConn, err := openClickHouseConn(chAddress)
	if err != nil {
		t.Fatalf("open rental clickhouse conn: %v", err)
	}
	defer rentalCHConn.Close()
	queryCHConn, err := openClickHouseConn(chAddress)
	if err != nil {
		t.Fatalf("open query clickhouse conn: %v", err)
	}
	defer queryCHConn.Close()
	authProvider := newTestAuthProvider(t)
	defer authProvider.Close()
	stripeKeys := requireStripeTestKeys(t)

	// ---- 2. Billing service (in-process) ----

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
	defer billingServer.Close()

	workerCtx, workerCancel := context.WithCancel(ctx)
	defer workerCancel()
	go func() {
		if err := billingServer.RunProjector(workerCtx, 200*time.Millisecond); err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("billing projector: %v", err)
		}
	}()

	// ---- 3. Seed test data ----

	if err := billingServer.SeedOrg(ctx, testOrgID, "E2E Test Org", "new"); err != nil {
		t.Fatalf("ensure org: %v", err)
	}

	if _, err := pg.billingDB.ExecContext(ctx, `
		INSERT INTO products (product_id, display_name, meter_unit, billing_model, reserve_policy)
		VALUES ('sandbox', 'Sandbox', 'vcpu_second', 'metered', '{"shape":"time","target_quantity":300,"min_quantity":1,"allow_partial_reserve":true,"renew_slack_quantity":30}'::jsonb)
		ON CONFLICT DO NOTHING
	`); err != nil {
		t.Fatalf("insert product: %v", err)
	}

	// Plan with known unit rates: vcpu=100, gib=50 ledger-units/sec.
	// With BillingVCPUs=2 and BillingMemMiB=2048 (2 GiB):
	//   CostPerSec = 2*100 + 2*50 = 300
	if _, err := pg.billingDB.ExecContext(ctx, `
		INSERT INTO plans (plan_id, product_id, display_name, billing_mode, included_credits, unit_rates, is_default, active)
		VALUES ('sandbox-default', 'sandbox', 'Sandbox PAYG', 'prepaid', 0, '{"vcpu":100,"gib":50}'::jsonb, true, true)
		ON CONFLICT DO NOTHING
	`); err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	const seedCredits uint64 = 5_000_000
	expiresAt := time.Now().Add(24 * time.Hour)
	if _, err := billingServer.SeedCredits(ctx, testOrgID, "sandbox", seedCredits, "purchase", "e2e-test-seed", expiresAt); err != nil {
		t.Fatalf("deposit credits: %v", err)
	}

	balanceBefore, _, err := billingServer.GetBalance(ctx, testOrgID)
	if err != nil {
		t.Fatalf("get balance: %v", err)
	}
	if balanceBefore != seedCredits {
		t.Fatalf("expected credit_available=%d, got %d", seedCredits, balanceBefore)
	}

	// ---- 4. Sandbox rental service (in-process) ----

	billingHTTPClient, err := billingclient.New(billingServer.URL)
	if err != nil {
		t.Fatalf("create billing HTTP client: %v", err)
	}

	runner := &fakeRunner{
		delay: 200 * time.Millisecond,
		logs:  "hello from direct e2e\n",
	}

	rentalServer := rentaltestharness.NewServer(rentaltestharness.Config{
		PG:            pg.rentalDB,
		CH:            rentalCHConn,
		CHDatabase:    "forge_metal",
		Runner:        runner,
		Billing:       billingHTTPClient,
		BillingVCPUs:  2,
		BillingMemMiB: 2048,
		AuthCfg:       authProvider.authConfig(testAudience),
		Logger:        logger,
	})
	defer rentalServer.Close()

	// ---- 5. Sign JWT ----

	token := authProvider.signToken(t, jwt.MapClaims{
		"iss":                                   authProvider.URL,
		"sub":                                   testUserID,
		"aud":                                   []string{testAudience},
		"exp":                                   time.Now().Add(time.Hour).Unix(),
		"urn:zitadel:iam:user:resourceowner:id": strconv.FormatUint(testOrgID, 10),
	})

	// ---- 6. Submit execution ----

	submitBody := map[string]any{
		"kind":            "direct",
		"run_command":     "echo hello from direct e2e",
		"product_id":      "sandbox",
		"idempotency_key": "e2e-direct-full-flow",
	}
	bodyBytes, err := json.Marshal(submitBody)
	if err != nil {
		t.Fatalf("marshal submit body: %v", err)
	}
	req, _ := http.NewRequest("POST", rentalServer.URL+"/api/v1/executions", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("submit execution: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var submitResp struct {
		ExecutionID string `json:"execution_id"`
		AttemptID   string `json:"attempt_id"`
		Status      string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&submitResp); err != nil {
		t.Fatalf("decode submit response: %v", err)
	}
	if submitResp.ExecutionID == "" || submitResp.AttemptID == "" {
		t.Fatalf("expected execution_id and attempt_id, got execution=%q attempt=%q", submitResp.ExecutionID, submitResp.AttemptID)
	}
	if submitResp.Status != "running" && submitResp.Status != "launching" && submitResp.Status != "reserved" {
		t.Fatalf("expected non-terminal status, got %q", submitResp.Status)
	}

	// ---- 7. Poll for completion ----

	type attemptView struct {
		AttemptID  string `json:"attempt_id"`
		State      string `json:"state"`
		ExitCode   int    `json:"exit_code"`
		DurationMs int64  `json:"duration_ms"`
		CommitSHA  string `json:"commit_sha"`
	}
	type executionView struct {
		ExecutionID string      `json:"execution_id"`
		Status      string      `json:"status"`
		Kind        string      `json:"kind"`
		RunCommand  string      `json:"run_command"`
		Latest      attemptView `json:"latest_attempt"`
	}

	var execution executionView
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		pollReq, _ := http.NewRequest("GET", rentalServer.URL+"/api/v1/executions/"+submitResp.ExecutionID, nil)
		pollReq.Header.Set("Authorization", "Bearer "+token)
		pollResp, err := http.DefaultClient.Do(pollReq)
		if err != nil {
			t.Fatalf("poll execution: %v", err)
		}
		_ = json.NewDecoder(pollResp.Body).Decode(&execution)
		pollResp.Body.Close()
		if execution.Status != "queued" && execution.Status != "reserved" && execution.Status != "launching" && execution.Status != "running" && execution.Status != "finalizing" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// ---- 8. Assert: execution completed ----

	if execution.Status != "succeeded" {
		t.Fatalf("expected execution status=succeeded, got %q", execution.Status)
	}
	if execution.Kind != "direct" {
		t.Fatalf("expected kind=direct, got %q", execution.Kind)
	}
	if execution.RunCommand != "echo hello from direct e2e" {
		t.Fatalf("expected persisted run_command, got %q", execution.RunCommand)
	}
	if execution.Latest.AttemptID != submitResp.AttemptID {
		t.Fatalf("expected latest attempt=%s, got %s", submitResp.AttemptID, execution.Latest.AttemptID)
	}
	if execution.Latest.State != "succeeded" {
		t.Fatalf("expected latest attempt state=succeeded, got %q", execution.Latest.State)
	}
	if execution.Latest.ExitCode != 0 {
		t.Fatalf("expected exit_code=0, got %d", execution.Latest.ExitCode)
	}

	// ---- 9. Assert: TigerBeetle credits consumed ----

	balanceAfter, _, err := billingServer.GetBalance(ctx, testOrgID)
	if err != nil {
		t.Fatalf("get balance after: %v", err)
	}
	if balanceAfter >= balanceBefore {
		t.Fatalf("credits not consumed: before=%d after=%d", balanceBefore, balanceAfter)
	}
	consumed := balanceBefore - balanceAfter

	const expectedCost uint64 = 300
	if consumed != expectedCost {
		t.Fatalf("expected %d credits consumed, got %d (before=%d after=%d)", expectedCost, consumed, balanceBefore, balanceAfter)
	}

	// ---- 10. Assert: PG execution + attempt + billing window records ----

	assertExecutionProjection(t, ctx, pg.rentalDB, submitResp.ExecutionID, submitResp.AttemptID, "echo hello from direct e2e")
	assertBillingWindow(t, ctx, pg.rentalDB, submitResp.AttemptID)

	// ---- 11. Assert: ClickHouse metering ----

	flushCtx, flushCancel := context.WithTimeout(ctx, 5*time.Second)
	defer flushCancel()
	if _, err := billingServer.ProjectPendingWindows(flushCtx); err != nil {
		t.Fatalf("project billing windows: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	var meteringCount uint64
	orgIDStr := strconv.FormatUint(testOrgID, 10)
	if err := queryCHConn.QueryRow(ctx,
		"SELECT count() FROM forge_metal.metering WHERE org_id = $1 AND product_id = $2 AND source_ref = $3",
		orgIDStr, "sandbox", submitResp.AttemptID,
	).Scan(&meteringCount); err != nil {
		t.Fatalf("query metering: %v", err)
	}
	if meteringCount != 1 {
		t.Fatalf("expected exactly 1 metering row, got %d", meteringCount)
	}

	// ---- 12. Assert: ClickHouse job_events + job_logs ----

	var eventCount uint64
	if err := queryCHConn.QueryRow(ctx,
		"SELECT count() FROM forge_metal.job_events WHERE org_id = $1 AND execution_id = $2",
		testOrgID, submitResp.ExecutionID,
	).Scan(&eventCount); err != nil {
		t.Fatalf("query job_events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected exactly 1 job_event, got %d", eventCount)
	}

	var logCount uint64
	if err := queryCHConn.QueryRow(ctx,
		"SELECT count() FROM forge_metal.job_logs WHERE attempt_id = $1",
		submitResp.AttemptID,
	).Scan(&logCount); err != nil {
		t.Fatalf("query job_logs: %v", err)
	}
	if logCount < 1 {
		t.Fatalf("expected at least 1 job_log row, got %d", logCount)
	}
}

func publicGitCloneURLForTestRepo(t *testing.T, repoPath, urlPath string) string {
	t.Helper()
	t.Setenv("FORGE_METAL_REPO_SCAN_E2E_ALLOW_FILE_PROTOCOL", "1")
	cloneURL := "https://93.184.216.34/" + strings.TrimPrefix(urlPath, "/")
	if !strings.HasSuffix(cloneURL, ".git") {
		cloneURL += ".git"
	}

	count := 0
	if existing := strings.TrimSpace(os.Getenv("GIT_CONFIG_COUNT")); existing != "" {
		parsed, err := strconv.Atoi(existing)
		if err != nil {
			t.Fatalf("parse GIT_CONFIG_COUNT=%q: %v", existing, err)
		}
		count = parsed
	}
	t.Setenv("GIT_CONFIG_COUNT", strconv.Itoa(count+1))
	t.Setenv(fmt.Sprintf("GIT_CONFIG_KEY_%d", count), "url."+repoPath+".insteadOf")
	t.Setenv(fmt.Sprintf("GIT_CONFIG_VALUE_%d", count), cloneURL)
	return cloneURL
}

func assertExecutionProjection(t *testing.T, ctx context.Context, db *sql.DB, executionID, attemptID, runCommand string) {
	t.Helper()

	var (
		status           string
		kind             string
		latestAttemptID  string
		persistedCommand string
		attemptState     string
		exitCode         int
	)
	if err := db.QueryRowContext(ctx, `
		SELECT e.status, e.kind, e.latest_attempt_id::text, e.run_command, a.state, COALESCE(a.exit_code, 0)
		FROM executions e
		JOIN execution_attempts a ON a.attempt_id = e.latest_attempt_id
		WHERE e.execution_id = $1
	`, executionID).Scan(&status, &kind, &latestAttemptID, &persistedCommand, &attemptState, &exitCode); err != nil {
		t.Fatalf("query execution projection: %v", err)
	}
	if status != "succeeded" {
		t.Fatalf("expected PG execution status=succeeded, got %q", status)
	}
	if kind != "direct" {
		t.Fatalf("expected PG kind=direct, got %q", kind)
	}
	if latestAttemptID != attemptID {
		t.Fatalf("expected latest_attempt_id=%s, got %s", attemptID, latestAttemptID)
	}
	if persistedCommand != runCommand {
		t.Fatalf("expected persisted run_command=%s, got %s", runCommand, persistedCommand)
	}
	if attemptState != "succeeded" {
		t.Fatalf("expected PG attempt state=succeeded, got %q", attemptState)
	}
	if exitCode != 0 {
		t.Fatalf("expected PG attempt exit_code=0, got %d", exitCode)
	}
}

func assertBillingWindow(t *testing.T, ctx context.Context, db *sql.DB, attemptID string) {
	t.Helper()

	var (
		state            string
		reservedQuantity int
		actualQuantity   int
		pricingPhase     string
	)
	if err := db.QueryRowContext(ctx, `
		SELECT state, reserved_quantity, actual_quantity, pricing_phase
		FROM execution_billing_windows
		WHERE attempt_id = $1 AND window_seq = 0
	`, attemptID).Scan(&state, &reservedQuantity, &actualQuantity, &pricingPhase); err != nil {
		t.Fatalf("query billing window: %v", err)
	}
	if state != "settled" {
		t.Fatalf("expected billing window state=settled, got %q", state)
	}
	if reservedQuantity != 300 {
		t.Fatalf("expected reserved_quantity=300, got %d", reservedQuantity)
	}
	if actualQuantity != 1 {
		t.Fatalf("expected actual_quantity=1, got %d", actualQuantity)
	}
	if pricingPhase == "" {
		t.Fatal("expected non-empty pricing_phase")
	}
}
