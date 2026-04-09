// End-to-end tests that start real PG, TigerBeetle, and ClickHouse processes,
// wire billing-service and sandbox-rental-service in-process via testharness
// packages, and exercise the execution control plane end to end.
package e2e_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func TestExecutionRepoExecFullFlow(t *testing.T) {
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
	repoPath, repoRef, repoHead := createFixtureRepo(t, "next-bun-monorepo")

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

	// ---- 4. Sandbox rental service (in-process) ----

	billingHTTPClient, err := billingclient.New(billingServer.URL)
	if err != nil {
		t.Fatalf("create billing HTTP client: %v", err)
	}

	runner := &fakeRunner{
		// First repo exec forces an on-demand warm. The same attempt is then
		// billed for the warm plus the retry, which currently totals 3s here.
		delay:       1500 * time.Millisecond,
		logs:        "hello from repo exec e2e\n",
		commitSHA:   repoHead,
		requireWarm: true,
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
		"kind":       "repo_exec",
		"repo":       "toy-next-bun-monorepo",
		"repo_url":   repoPath,
		"ref":        repoRef,
		"product_id": "sandbox",
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
		Repo        string      `json:"repo"`
		RepoURL     string      `json:"repo_url"`
		Ref         string      `json:"ref"`
		CommitSHA   string      `json:"commit_sha"`
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
	if execution.Kind != "repo_exec" {
		t.Fatalf("expected kind=repo_exec, got %q", execution.Kind)
	}
	if execution.Repo != "toy-next-bun-monorepo" {
		t.Fatalf("expected repo=toy-next-bun-monorepo, got %q", execution.Repo)
	}
	if execution.CommitSHA != repoHead {
		t.Fatalf("expected commit_sha=%s, got %s", repoHead, execution.CommitSHA)
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

	const expectedCost uint64 = 900
	if consumed != expectedCost {
		t.Fatalf("expected %d credits consumed, got %d (before=%d after=%d)", expectedCost, consumed, balanceBefore, balanceAfter)
	}

	// ---- 10. Assert: PG execution + attempt + billing window records ----

	assertExecutionProjection(t, ctx, pg.rentalDB, submitResp.ExecutionID, submitResp.AttemptID, repoHead)
	assertBillingWindow(t, ctx, pg.rentalDB, submitResp.AttemptID)

	// ---- 11. Assert: ClickHouse metering ----

	flushCtx, flushCancel := context.WithTimeout(ctx, 5*time.Second)
	defer flushCancel()
	if err := billingServer.FlushMetering(flushCtx); err != nil {
		t.Logf("metering writer close (non-fatal): %v", err)
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

func createFixtureRepo(t *testing.T, fixture string) (path, ref, head string) {
	t.Helper()

	repoRoot := filepath.Join(t.TempDir(), fixture)
	copyDir(t, fixtureSourcePath(t, fixture), repoRoot)

	git := mustLookPath(t, "git")
	runCmd(t, exec.Command(git, "init", "--initial-branch=main", repoRoot))
	runCmd(t, exec.Command(git, "-C", repoRoot, "config", "user.name", "Forge Metal E2E"))
	runCmd(t, exec.Command(git, "-C", repoRoot, "config", "user.email", "e2e@forge-metal.local"))
	runCmd(t, exec.Command(git, "-C", repoRoot, "add", "."))
	runCmd(t, exec.Command(git, "-C", repoRoot, "commit", "-m", "fixture"))

	out, err := exec.Command(git, "-C", repoRoot, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %s", strings.TrimSpace(string(out)))
	}
	return repoRoot, "refs/heads/main", strings.TrimSpace(string(out))
}

func fixtureSourcePath(t *testing.T, fixture string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "platform", "test", "fixtures", fixture))
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()

	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dst, err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("readdir %s: %v", src, err)
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		info, err := entry.Info()
		if err != nil {
			t.Fatalf("stat %s: %v", srcPath, err)
		}
		if info.IsDir() {
			copyDir(t, srcPath, dstPath)
			continue
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			t.Fatalf("read %s: %v", srcPath, err)
		}
		if err := os.WriteFile(dstPath, data, info.Mode()); err != nil {
			t.Fatalf("write %s: %v", dstPath, err)
		}
	}
}

func assertExecutionProjection(t *testing.T, ctx context.Context, db *sql.DB, executionID, attemptID, commitSHA string) {
	t.Helper()

	var (
		status          string
		kind            string
		latestAttemptID string
		persistedCommit string
		attemptState    string
		exitCode        int
	)
	if err := db.QueryRowContext(ctx, `
		SELECT e.status, e.kind, e.latest_attempt_id::text, e.commit_sha, a.state, COALESCE(a.exit_code, 0)
		FROM executions e
		JOIN execution_attempts a ON a.attempt_id = e.latest_attempt_id
		WHERE e.execution_id = $1
	`, executionID).Scan(&status, &kind, &latestAttemptID, &persistedCommit, &attemptState, &exitCode); err != nil {
		t.Fatalf("query execution projection: %v", err)
	}
	if status != "succeeded" {
		t.Fatalf("expected PG execution status=succeeded, got %q", status)
	}
	if kind != "repo_exec" {
		t.Fatalf("expected PG kind=repo_exec, got %q", kind)
	}
	if latestAttemptID != attemptID {
		t.Fatalf("expected latest_attempt_id=%s, got %s", attemptID, latestAttemptID)
	}
	if persistedCommit != commitSHA {
		t.Fatalf("expected persisted commit_sha=%s, got %s", commitSHA, persistedCommit)
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
		state         string
		windowSeconds int
		actualSeconds int
		pricingPhase  string
	)
	if err := db.QueryRowContext(ctx, `
		SELECT state, window_seconds, actual_seconds, pricing_phase
		FROM execution_billing_windows
		WHERE attempt_id = $1 AND window_seq = 0
	`, attemptID).Scan(&state, &windowSeconds, &actualSeconds, &pricingPhase); err != nil {
		t.Fatalf("query billing window: %v", err)
	}
	if state != "settled" {
		t.Fatalf("expected billing window state=settled, got %q", state)
	}
	if windowSeconds != 300 {
		t.Fatalf("expected window_seconds=300, got %d", windowSeconds)
	}
	if actualSeconds != 3 {
		t.Fatalf("expected actual_seconds=3, got %d", actualSeconds)
	}
	if pricingPhase == "" {
		t.Fatal("expected non-empty pricing_phase")
	}
}
