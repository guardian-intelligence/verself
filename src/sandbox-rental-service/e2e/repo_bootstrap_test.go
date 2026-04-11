package e2e_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/golang-jwt/jwt/v5"

	billingclient "github.com/forge-metal/billing-service/client"
	billingtestharness "github.com/forge-metal/billing-service/testharness"
	rentaltestharness "github.com/forge-metal/sandbox-rental-service/testharness"
)

type repoView struct {
	RepoID                   string          `json:"repo_id"`
	State                    string          `json:"state"`
	CompatibilityStatus      string          `json:"compatibility_status"`
	CompatibilitySummary     json.RawMessage `json:"compatibility_summary"`
	LastScannedSHA           string          `json:"last_scanned_sha"`
	ActiveGoldenGenerationID string          `json:"active_golden_generation_id"`
	LastReadySHA             string          `json:"last_ready_sha"`
	LastError                string          `json:"last_error"`
}

type goldenGenerationView struct {
	GoldenGenerationID string `json:"golden_generation_id"`
	State              string `json:"state"`
	TriggerReason      string `json:"trigger_reason"`
	ExecutionID        string `json:"execution_id"`
	AttemptID          string `json:"attempt_id"`
	SnapshotRef        string `json:"snapshot_ref"`
	FailureReason      string `json:"failure_reason"`
	FailureDetail      string `json:"failure_detail"`
}

type repoBootstrapEnv struct {
	ctx           context.Context
	pg            pgEnv
	billingServer *billingtestharness.Server
	billingClient *billingclient.ServiceClient
	rentalServer  *rentaltestharness.Server
	authProvider  *testAuthProvider
	queryCHConn   anyQueryRower
	token         string
	webhookSecret string
	runner        *fakeRunner
	balanceBefore uint64
	repoPath      string
	repoHead      string
}

type anyQueryRower interface {
	QueryRow(context.Context, string, ...any) chdriver.Row
}

func TestImportRepoAPI_CompatibleWorkflowBootstrapsGolden(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e tests require real databases")
	}

	env := startRepoBootstrapEnv(t, &fakeRunner{
		delay:     400 * time.Millisecond,
		logs:      "bootstrap complete\n",
		commitSHA: "",
	})
	defer env.close()
	env.runner.setCommitSHA(env.repoHead)

	imported := importRepoAgainstServer(t, env.ctx, env.rentalServer.URL, env.token, env.repoPath)
	if imported.CompatibilityStatus != "compatible" {
		t.Fatalf("compatibility_status: got %q summary=%s", imported.CompatibilityStatus, imported.CompatibilitySummary)
	}
	if imported.State != "preparing" && imported.State != "ready" {
		t.Fatalf("repo state after import: got %q", imported.State)
	}

	repo := waitForRepoState(t, env.ctx, env.rentalServer.URL, env.token, imported.RepoID, "ready")
	if repo.ActiveGoldenGenerationID == "" {
		t.Fatal("expected active_golden_generation_id")
	}
	if repo.LastReadySHA != env.repoHead {
		t.Fatalf("expected last_ready_sha=%s, got %s", env.repoHead, repo.LastReadySHA)
	}

	generations := listRepoGenerations(t, env.ctx, env.rentalServer.URL, env.token, repo.RepoID)
	if len(generations) != 1 {
		t.Fatalf("expected 1 golden generation, got %d", len(generations))
	}
	generation := generations[0]
	if generation.State != "ready" {
		t.Fatalf("expected generation state=ready, got %q", generation.State)
	}
	if generation.TriggerReason != "bootstrap" {
		t.Fatalf("expected trigger_reason=bootstrap, got %q", generation.TriggerReason)
	}
	if generation.AttemptID == "" || generation.ExecutionID == "" {
		t.Fatalf("expected execution+attempt ids on generation, got execution=%q attempt=%q", generation.ExecutionID, generation.AttemptID)
	}
	if generation.SnapshotRef == "" {
		t.Fatal("expected snapshot_ref on ready generation")
	}

	assertWarmGoldenExecutionProjection(t, env.ctx, env.pg.rentalDB, generation.ExecutionID, generation.AttemptID, env.repoHead)
	assertWarmGoldenBillingWindow(t, env.ctx, env.pg.rentalDB, generation.AttemptID)
	assertWarmGoldenBillingSpent(t, env.ctx, env.billingServer, env.balanceBefore, 300)
	flushBillingMetering(t, env.ctx, env.billingServer)
	assertWarmGoldenClickHouse(t, env.ctx, env.queryCHConn, imported.RepoID, generation.GoldenGenerationID, generation.ExecutionID, generation.AttemptID)
}

func TestRefreshRepoAPI_FailedRefreshPreservesActiveGolden(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e tests require real databases")
	}

	env := startRepoBootstrapEnv(t, &fakeRunner{
		delay:     400 * time.Millisecond,
		logs:      "bootstrap complete\n",
		commitSHA: "",
	})
	defer env.close()
	env.runner.setCommitSHA(env.repoHead)

	imported := importRepoAgainstServer(t, env.ctx, env.rentalServer.URL, env.token, env.repoPath)
	repo := waitForRepoState(t, env.ctx, env.rentalServer.URL, env.token, imported.RepoID, "ready")
	originalGenerationID := repo.ActiveGoldenGenerationID
	if originalGenerationID == "" {
		t.Fatal("expected active generation after bootstrap")
	}

	env.runner.setError(errors.New("forced refresh failure"))
	refresh := refreshRepoAgainstServer(t, env.ctx, env.rentalServer.URL, env.token, repo.RepoID)
	if refresh.Generation.GoldenGenerationID == "" {
		t.Fatal("expected refresh generation id")
	}
	if refresh.Generation.TriggerReason != "manual_refresh" {
		t.Fatalf("expected manual_refresh trigger, got %q", refresh.Generation.TriggerReason)
	}

	degraded := waitForRepoState(t, env.ctx, env.rentalServer.URL, env.token, repo.RepoID, "degraded")
	if degraded.ActiveGoldenGenerationID != originalGenerationID {
		t.Fatalf("expected active generation to remain %s, got %s", originalGenerationID, degraded.ActiveGoldenGenerationID)
	}
	if degraded.LastError == "" {
		t.Fatal("expected repo last_error after failed refresh")
	}

	generations := listRepoGenerations(t, env.ctx, env.rentalServer.URL, env.token, repo.RepoID)
	if len(generations) != 2 {
		t.Fatalf("expected 2 golden generations, got %d", len(generations))
	}
	latest := generations[0]
	if latest.GoldenGenerationID != refresh.Generation.GoldenGenerationID {
		t.Fatalf("expected newest generation %s, got %s", refresh.Generation.GoldenGenerationID, latest.GoldenGenerationID)
	}
	if latest.State != "failed" {
		t.Fatalf("expected failed refresh generation, got %q", latest.State)
	}
	if latest.AttemptID == "" {
		t.Fatal("expected refresh attempt_id")
	}
	if latest.FailureReason == "" {
		t.Fatal("expected refresh failure_reason")
	}
	var readyCount int
	for _, generation := range generations {
		if generation.State == "ready" {
			readyCount++
		}
	}
	if readyCount != 1 {
		t.Fatalf("expected 1 still-ready generation, got %d", readyCount)
	}

	assertWarmGoldenBillingWindow(t, env.ctx, env.pg.rentalDB, latest.AttemptID)
	flushBillingMetering(t, env.ctx, env.billingServer)
	assertWarmGoldenClickHouse(t, env.ctx, env.queryCHConn, repo.RepoID, latest.GoldenGenerationID, latest.ExecutionID, latest.AttemptID)

	var meteringCount uint64
	orgIDStr := strconv.FormatUint(testOrgID, 10)
	if err := env.queryCHConn.QueryRow(env.ctx,
		"SELECT count() FROM forge_metal.metering WHERE org_id = $1 AND product_id = $2",
		orgIDStr, "sandbox",
	).Scan(&meteringCount); err != nil {
		t.Fatalf("query metering count: %v", err)
	}
	if meteringCount != 2 {
		t.Fatalf("expected 2 metering rows after bootstrap+failed refresh, got %d", meteringCount)
	}
}

func TestImportRepoAPI_ReimportReadyRepoDoesNotQueueSecondBootstrap(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e tests require real databases")
	}

	env := startRepoBootstrapEnv(t, &fakeRunner{
		delay:     400 * time.Millisecond,
		logs:      "bootstrap complete\n",
		commitSHA: "",
	})
	defer env.close()
	env.runner.setCommitSHA(env.repoHead)

	imported := importRepoAgainstServer(t, env.ctx, env.rentalServer.URL, env.token, env.repoPath)
	ready := waitForRepoState(t, env.ctx, env.rentalServer.URL, env.token, imported.RepoID, "ready")
	if ready.ActiveGoldenGenerationID == "" {
		t.Fatal("expected active generation after bootstrap")
	}

	reimported := importRepoAgainstServer(t, env.ctx, env.rentalServer.URL, env.token, env.repoPath)
	if reimported.State != "ready" {
		t.Fatalf("expected reimported repo to stay ready, got %q", reimported.State)
	}
	if reimported.ActiveGoldenGenerationID != ready.ActiveGoldenGenerationID {
		t.Fatalf("expected active generation to remain %s, got %s", ready.ActiveGoldenGenerationID, reimported.ActiveGoldenGenerationID)
	}

	generations := listRepoGenerations(t, env.ctx, env.rentalServer.URL, env.token, ready.RepoID)
	if len(generations) != 1 {
		t.Fatalf("expected 1 generation after reimport, got %d", len(generations))
	}
}

func startRepoBootstrapEnv(t *testing.T, runner *fakeRunner) *repoBootstrapEnv {
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
	repoFixture := createWorkflowRepoFixture(t, map[string]string{
		"package.json": `{
  "name": "example",
  "packageManager": "pnpm@10.0.0",
  "scripts": {
    "ci": "echo ok"
  }
}
`,
		".github/workflows/ci.yml": `
name: ci
on:
  push:
jobs:
  build:
    runs-on: forge-metal
    steps:
      - run: echo ok
	`,
	})

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

	seedCredits := seedSandboxProductAndCredits(t, ctx, pg.billingDB, billingServer)

	billingHTTPClient, err := billingclient.New(billingServer.URL)
	if err != nil {
		t.Fatalf("create billing HTTP client: %v", err)
	}

	rentalCHConn, err := openClickHouseConn(chAddress)
	if err != nil {
		t.Fatalf("open rental clickhouse conn: %v", err)
	}
	rentalServer := rentaltestharness.NewServer(rentaltestharness.Config{
		PG:                        pg.rentalDB,
		CH:                        rentalCHConn,
		CHDatabase:                "forge_metal",
		Runner:                    runner,
		Billing:                   billingHTTPClient,
		PlatformOrgID:             testOrgID,
		ForgejoWebhookSecret:      "forgejo-webhook-secret",
		BillingVCPUs:              2,
		BillingMemMiB:             2048,
		ForgejoURL:                "https://git.example.invalid",
		ForgejoRunnerLabel:        "forge-metal",
		ForgejoRunnerToken:        "runner-registration-token",
		ForgejoRunnerBinaryURL:    "https://downloads.example.invalid/forgejo-runner",
		ForgejoRunnerBinarySHA256: "90f0a8ea246748f2a89b03ba5f0688491ada6dc34fc01f6d9a33a7a891a38018",
		AuthCfg:                   authProvider.authConfig(testAudience),
		Logger:                    logger,
	})

	token := authProvider.signToken(t, jwt.MapClaims{
		"iss":                                   authProvider.URL,
		"sub":                                   testUserID,
		"aud":                                   []string{testAudience},
		"exp":                                   time.Now().Add(time.Hour).Unix(),
		"urn:zitadel:iam:user:resourceowner:id": strconv.FormatUint(testOrgID, 10),
	})

	return &repoBootstrapEnv{
		ctx:           ctx,
		pg:            pg,
		billingServer: billingServer,
		billingClient: billingHTTPClient,
		rentalServer:  rentalServer,
		authProvider:  authProvider,
		queryCHConn:   queryCHConn,
		token:         token,
		webhookSecret: "forgejo-webhook-secret",
		runner:        runner,
		balanceBefore: seedCredits,
		repoPath:      repoFixture.CloneURL,
		repoHead:      repoFixture.Head,
	}
}

func (e *repoBootstrapEnv) close() {
	e.rentalServer.Close()
	e.billingServer.Close()
	e.authProvider.Close()
	type closer interface{ Close() error }
	if conn, ok := e.queryCHConn.(closer); ok {
		_ = conn.Close()
	}
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
	if _, err := billingServer.SeedCredits(ctx, testOrgID, "sandbox", seedCredits, "purchase", "repo-bootstrap-seed", expiresAt); err != nil {
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

func importRepoAgainstServer(t *testing.T, ctx context.Context, baseURL, token, repoPath string) repoView {
	t.Helper()

	body := map[string]any{
		"provider":         "forgejo",
		"provider_repo_id": "acme/example",
		"owner":            "acme",
		"name":             "example",
		"full_name":        "acme/example",
		"clone_url":        repoPath,
		"default_branch":   "main",
	}
	return doJSONRequest[repoView](t, ctx, baseURL+"/api/v1/repos", token, http.MethodPost, body, http.StatusCreated)
}

func getRepoAgainstServer(t *testing.T, ctx context.Context, baseURL, token, repoID string) repoView {
	t.Helper()
	return doJSONRequest[repoView](t, ctx, baseURL+"/api/v1/repos/"+repoID, token, http.MethodGet, nil, http.StatusOK)
}

func listRepoGenerations(t *testing.T, ctx context.Context, baseURL, token, repoID string) []goldenGenerationView {
	t.Helper()
	return doJSONRequest[[]goldenGenerationView](t, ctx, baseURL+"/api/v1/repos/"+repoID+"/generations", token, http.MethodGet, nil, http.StatusOK)
}

type refreshRepoResponse struct {
	Repo          repoView             `json:"repo"`
	Generation    goldenGenerationView `json:"generation"`
	ExecutionID   string               `json:"execution_id"`
	AttemptID     string               `json:"attempt_id"`
	TriggerReason string               `json:"trigger_reason"`
}

func refreshRepoAgainstServer(t *testing.T, ctx context.Context, baseURL, token, repoID string) refreshRepoResponse {
	t.Helper()
	return doJSONRequest[refreshRepoResponse](t, ctx, baseURL+"/api/v1/repos/"+repoID+"/refresh", token, http.MethodPost, nil, http.StatusAccepted)
}

func waitForRepoState(t *testing.T, ctx context.Context, baseURL, token, repoID, terminalState string) repoView {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		repo := getRepoAgainstServer(t, ctx, baseURL, token, repoID)
		if repo.State == terminalState {
			return repo
		}
		if repo.State == "failed" && terminalState != "failed" {
			t.Fatalf("repo reached failed unexpectedly: last_error=%s", repo.LastError)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("repo did not reach %s before timeout", terminalState)
	return repoView{}
}

func assertWarmGoldenExecutionProjection(t *testing.T, ctx context.Context, db *sql.DB, executionID, attemptID, commitSHA string) {
	t.Helper()

	var (
		status          string
		kind            string
		latestAttemptID string
		persistedCommit string
		attemptState    string
	)
	if err := db.QueryRowContext(ctx, `
		SELECT e.status, e.kind, e.latest_attempt_id::text, e.commit_sha, a.state
		FROM executions e
		JOIN execution_attempts a ON a.attempt_id = e.latest_attempt_id
		WHERE e.execution_id = $1
	`, executionID).Scan(&status, &kind, &latestAttemptID, &persistedCommit, &attemptState); err != nil {
		t.Fatalf("query warm execution projection: %v", err)
	}
	if status != "succeeded" {
		t.Fatalf("expected warm execution status=succeeded, got %q", status)
	}
	if kind != "warm_golden" {
		t.Fatalf("expected warm kind=warm_golden, got %q", kind)
	}
	if latestAttemptID != attemptID {
		t.Fatalf("expected latest_attempt_id=%s, got %s", attemptID, latestAttemptID)
	}
	if persistedCommit != commitSHA {
		t.Fatalf("expected persisted commit_sha=%s, got %s", commitSHA, persistedCommit)
	}
	if attemptState != "succeeded" {
		t.Fatalf("expected attempt state=succeeded, got %q", attemptState)
	}
}

func assertWarmGoldenBillingWindow(t *testing.T, ctx context.Context, db *sql.DB, attemptID string) {
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
		t.Fatalf("query warm billing window: %v", err)
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

func assertWarmGoldenBillingSpent(t *testing.T, ctx context.Context, billingServer *billingtestharness.Server, balanceBefore, expectedConsumed uint64) {
	t.Helper()
	balanceAfter, _, err := billingServer.GetBalance(ctx, testOrgID)
	if err != nil {
		t.Fatalf("get balance after warm golden: %v", err)
	}
	if balanceAfter >= balanceBefore {
		t.Fatalf("expected credits consumed for warm golden: before=%d after=%d", balanceBefore, balanceAfter)
	}
	consumed := balanceBefore - balanceAfter
	if consumed != expectedConsumed {
		t.Fatalf("expected %d credits consumed, got %d", expectedConsumed, consumed)
	}
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

func assertWarmGoldenClickHouse(t *testing.T, ctx context.Context, ch anyQueryRower, repoID, generationID, executionID, attemptID string) {
	t.Helper()

	var meteringCount uint64
	orgIDStr := strconv.FormatUint(testOrgID, 10)
	if err := ch.QueryRow(ctx,
		"SELECT count() FROM forge_metal.metering WHERE org_id = $1 AND product_id = $2 AND source_ref = $3",
		orgIDStr, "sandbox", attemptID,
	).Scan(&meteringCount); err != nil {
		t.Fatalf("query warm metering: %v", err)
	}
	if meteringCount != 1 {
		t.Fatalf("expected exactly 1 metering row for warm golden, got %d", meteringCount)
	}

	var (
		eventRepoID       string
		eventGenerationID string
		eventKind         string
	)
	if err := ch.QueryRow(ctx, `
		SELECT repo_id, golden_generation_id, kind
		FROM forge_metal.job_events
		WHERE org_id = $1 AND execution_id = $2
	`, testOrgID, executionID).Scan(&eventRepoID, &eventGenerationID, &eventKind); err != nil {
		t.Fatalf("query warm job_event payload: %v", err)
	}
	if eventKind != "warm_golden" {
		t.Fatalf("expected warm_golden kind, got %q", eventKind)
	}
	if eventRepoID != repoID {
		t.Fatalf("expected repo_id=%s, got %s", repoID, eventRepoID)
	}
	if eventGenerationID != generationID {
		t.Fatalf("expected golden_generation_id=%s, got %s", generationID, eventGenerationID)
	}

	assertSystemLogMirrored(t, ctx, ch, attemptID)
}

func assertSystemLogMirrored(t *testing.T, ctx context.Context, ch anyQueryRower, attemptID string) {
	t.Helper()

	var systemCount uint64
	if err := ch.QueryRow(ctx,
		"SELECT count() FROM forge_metal.job_logs WHERE attempt_id = $1 AND stream = 'system'",
		attemptID,
	).Scan(&systemCount); err != nil {
		t.Fatalf("query system log rows: %v", err)
	}
	if systemCount == 0 {
		t.Fatalf("expected mirrored system logs for attempt %s", attemptID)
	}
}

func doJSONRequest[T any](t *testing.T, ctx context.Context, url, token, method string, body any, wantStatus int) T {
	t.Helper()

	var reader *strings.Reader
	if body == nil {
		reader = strings.NewReader("")
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = strings.NewReader(string(data))
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request %s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected %d from %s %s, got %d: %s", wantStatus, method, url, resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

func doJSONRequestStatusOnly(t *testing.T, ctx context.Context, url, token, method string, body any, wantStatus int) []byte {
	t.Helper()

	var reader *strings.Reader
	if body == nil {
		reader = strings.NewReader("")
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = strings.NewReader(string(data))
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request %s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		t.Fatalf("expected %d from %s %s, got %d: %s", wantStatus, method, url, resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	return payload
}
