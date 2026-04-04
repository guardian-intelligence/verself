package billing

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode"

	_ "github.com/lib/pq"
	"github.com/stripe/stripe-go/v85"
	tb "github.com/tigerbeetle/tigerbeetle-go"
	tbtypes "github.com/tigerbeetle/tigerbeetle-go/pkg/types"
	"pgregory.net/rapid"
)

var ensureOrgStateMachineRunCounter atomic.Uint64

func TestEnsureOrgStateMachine(t *testing.T) {
	t.Parallel()

	env := newPhase1TestEnv(t)

	cfg := DefaultConfig()
	cfg.PgDSN = env.pgDSN
	cfg.StripeSecretKey = "sk_test_placeholder"
	cfg.StripeWebhookSecret = "whsec_test_placeholder"
	cfg.TigerBeetleAddresses = []string{env.tbAddress}
	cfg.TigerBeetleClusterID = env.clusterID

	client, err := NewClient(env.tbClient, env.pg, stripe.NewClient(cfg.StripeSecretKey), cfg)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	rapid.Check(t, func(t *rapid.T) {
		sm := &ensureOrgStateMachine{
			client:           client,
			pg:               env.pg,
			tbClient:         env.tbClient,
			orgOffset:        OrgID(ensureOrgStateMachineRunCounter.Add(1) * 1_000_000),
			seen:             make(map[OrgID]struct{}),
			expectedBalances: make(map[OrgID]Balance),
			grants:           make(map[GrantID]seededGrant),
		}
		t.Repeat(rapid.StateMachineActions(sm))
	})
}

func TestCreditGrantsSchemaMatchesCurrentBillingMigration(t *testing.T) {
	t.Parallel()

	pg, _ := startPostgresForBillingTests(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rows, err := pg.QueryContext(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'credit_grants'
		ORDER BY ordinal_position
	`)
	if err != nil {
		t.Fatalf("query credit_grants columns: %v", err)
	}
	defer rows.Close()

	columns := make(map[string]struct{})
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		columns[column] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate columns: %v", err)
	}

	for _, missing := range []string{"consumed", "expired", "remaining", "account_type"} {
		if _, ok := columns[missing]; ok {
			t.Fatalf("unexpected legacy column %s still present", missing)
		}
	}
	if _, ok := columns["closed_at"]; !ok {
		t.Fatal("expected closed_at column on credit_grants")
	}

	// Verify grant_id is TEXT (ULID), not BIGINT (identity sequence)
	var grantIDType string
	if err := pg.QueryRowContext(ctx, `
		SELECT data_type
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'credit_grants' AND column_name = 'grant_id'
	`).Scan(&grantIDType); err != nil {
		t.Fatalf("query grant_id data type: %v", err)
	}
	if grantIDType != "text" {
		t.Fatalf("expected grant_id type text (ULID), got %q", grantIDType)
	}

	// Verify no credit_grants_grant_id_seq sequence exists (identity sequence removed)
	var seqCount int
	if err := pg.QueryRowContext(ctx, `
		SELECT count(*)
		FROM information_schema.sequences
		WHERE sequence_schema = 'public' AND sequence_name = 'credit_grants_grant_id_seq'
	`).Scan(&seqCount); err != nil {
		t.Fatalf("query credit_grants sequence: %v", err)
	}
	if seqCount != 0 {
		t.Fatal("unexpected credit_grants_grant_id_seq sequence still exists")
	}

	var activeIndexDef string
	if err := pg.QueryRowContext(ctx, `
		SELECT indexdef
		FROM pg_indexes
		WHERE schemaname = 'public' AND indexname = 'idx_credit_grants_active'
	`).Scan(&activeIndexDef); err != nil {
		t.Fatalf("query idx_credit_grants_active: %v", err)
	}
	if !strings.Contains(activeIndexDef, "closed_at IS NULL") {
		t.Fatalf("expected idx_credit_grants_active to filter on closed_at IS NULL, got %q", activeIndexDef)
	}

	var subscriptionIndexDef string
	if err := pg.QueryRowContext(ctx, `
		SELECT indexdef
		FROM pg_indexes
		WHERE schemaname = 'public' AND indexname = 'idx_credit_grants_subscription_period'
	`).Scan(&subscriptionIndexDef); err != nil {
		t.Fatalf("query idx_credit_grants_subscription_period: %v", err)
	}
	if strings.Contains(subscriptionIndexDef, "account_type") {
		t.Fatalf("expected idx_credit_grants_subscription_period to exclude account_type, got %q", subscriptionIndexDef)
	}
}

type ensureOrgStateMachine struct {
	client           *Client
	pg               *sql.DB
	tbClient         tb.Client
	orgOffset        OrgID
	seen             map[OrgID]struct{}
	expectedBalances map[OrgID]Balance
	grants           map[GrantID]seededGrant
}

func (sm *ensureOrgStateMachine) OpEnsureOrg(t *rapid.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	orgID := sm.orgOffset + OrgID(rapid.Uint64Range(1, 128).Draw(t, "org_id"))
	displayName := validDisplayName(rapid.String().Draw(t, "display_name"))

	if err := sm.client.EnsureOrg(ctx, orgID, displayName); err != nil {
		t.Fatalf("ensure org %d: %v", orgID, err)
	}

	sm.seen[orgID] = struct{}{}
}

func (sm *ensureOrgStateMachine) OpDepositCredits(t *rapid.T) {
	orgID := sm.orgOffset + OrgID(rapid.Uint64Range(1, 128).Draw(t, "org_id"))
	sourceType := GrantSourceType(rapid.Uint64Range(1, 5).Draw(t, "source_type"))
	amount := rapid.Uint64Range(1, 1_000_000).Draw(t, "amount")

	grant := seedGrantForTest(t, sm.client, sm.pg, sm.tbClient, orgID, sourceType, amount)
	sm.seen[orgID] = struct{}{}
	sm.grants[grant.grantID] = grant

	balance := sm.expectedBalances[orgID]
	var err error
	if sourceType.IsFreeTier() {
		balance.FreeTierAvailable, err = safeAddUint64(balance.FreeTierAvailable, amount)
		if err != nil {
			t.Fatalf("sum expected free tier available: %v", err)
		}
	} else {
		balance.CreditAvailable, err = safeAddUint64(balance.CreditAvailable, amount)
		if err != nil {
			t.Fatalf("sum expected credit available: %v", err)
		}
	}
	balance.TotalAvailable, err = safeAddUint64(balance.FreeTierAvailable, balance.CreditAvailable)
	if err != nil {
		t.Fatalf("sum expected total available: %v", err)
	}
	sm.expectedBalances[orgID] = balance
}

func (sm *ensureOrgStateMachine) Check(t *rapid.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sm.requireOperatorAccounts(t)

	for orgID := range sm.seen {
		orgIDText := strconv.FormatUint(uint64(orgID), 10)

		var count int
		if err := sm.pg.QueryRowContext(ctx, `SELECT count(*) FROM orgs WHERE org_id = $1`, orgIDText).Scan(&count); err != nil {
			t.Fatalf("query org %s: %v", orgIDText, err)
		}
		if count != 1 {
			t.Fatalf("expected exactly one org row for %s, got %d", orgIDText, count)
		}
	}

	for orgID, expected := range sm.expectedBalances {
		balance, err := sm.client.GetOrgBalance(ctx, orgID)
		if err != nil {
			t.Fatalf("get balance for %d: %v", orgID, err)
		}
		if balance != expected {
			t.Fatalf("expected balance %+v for org %d, got %+v", expected, orgID, balance)
		}
	}

	for _, grant := range sm.grants {
		requireGrantAccount(t, sm.tbClient, grant)
	}
}

func (sm *ensureOrgStateMachine) requireOperatorAccounts(t *rapid.T) {
	t.Helper()

	expected := []struct {
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
	}

	sm.requireAccounts(t, expected, 0, 0)
}

func (sm *ensureOrgStateMachine) requireAccounts(t *rapid.T, expected []struct {
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

	accounts, err := sm.tbClient.LookupAccounts(accountIDs)
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

		flags := account.AccountFlags()
		if flags != item.flags {
			t.Fatalf("account %v: expected flags %+v, got %+v", item.id.raw, item.flags, flags)
		}
	}
}

type phase1TestEnv struct {
	pgDSN     string
	pg        *sql.DB
	tbAddress string
	tbClient  tb.Client
	clusterID uint64
}

func newPhase1TestEnv(t *testing.T) phase1TestEnv {
	t.Helper()

	pg, pgDSN := startPostgresForBillingTests(t)
	tbAddress, tbClient, clusterID := startTigerBeetleForBillingTests(t)

	return phase1TestEnv{
		pgDSN:     pgDSN,
		pg:        pg,
		tbAddress: tbAddress,
		tbClient:  tbClient,
		clusterID: clusterID,
	}
}

func startPostgresForBillingTests(t *testing.T) (*sql.DB, string) {
	t.Helper()

	initdb := mustLookPath(t, "initdb")
	pgCtl := mustLookPath(t, "pg_ctl")
	createdb := mustLookPath(t, "createdb")
	psql := mustLookPath(t, "psql")
	dataDir := filepath.Join(t.TempDir(), "pgdata")
	socketDir := filepath.Join(t.TempDir(), "pgsocket")
	if err := os.MkdirAll(socketDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", socketDir, err)
	}

	runCmd(t, exec.Command(initdb, "-D", dataDir, "--auth=trust", "--username=postgres", "--encoding=UTF8", "--locale=C.UTF-8"))
	t.Log("postgres initdb complete")

	port := freePort(t)
	logFile := filepath.Join(t.TempDir(), "postgres.log")
	start := exec.Command(pgCtl, "-D", dataDir, "-l", logFile, "-w", "start", "-o", fmt.Sprintf("-p %d -k %s -h 127.0.0.1", port, socketDir))
	runCmd(t, start)
	t.Logf("postgres started on 127.0.0.1:%d", port)
	t.Cleanup(func() {
		stop := exec.Command(pgCtl, "-D", dataDir, "-m", "immediate", "stop")
		_ = stop.Run()
	})

	runCmd(t, exec.Command(createdb, "-h", "127.0.0.1", "-p", strconv.Itoa(port), "-U", "postgres", "sandbox"))
	t.Log("sandbox database created")

	for _, migrationPath := range postgresMigrationPaths(t) {
		runCmd(t, exec.Command(psql,
			"-h", "127.0.0.1",
			"-p", strconv.Itoa(port),
			"-U", "postgres",
			"-d", "sandbox",
			"-v", "ON_ERROR_STOP=1",
			"-f", migrationPath,
		))
	}
	t.Log("postgres billing migrations applied")

	sandboxDSN := fmt.Sprintf("postgres://postgres@127.0.0.1:%d/sandbox?sslmode=disable", port)
	db := openPostgres(t, sandboxDSN)
	t.Log("postgres sandbox connection ready")
	t.Cleanup(func() {
		_ = db.Close()
	})

	return db, sandboxDSN
}

func openPostgres(t *testing.T, dsn string) *sql.DB {
	t.Helper()

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("sql open %s: %v", dsn, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping %s: %v", dsn, err)
	}

	return db
}

func startTigerBeetleForBillingTests(t *testing.T) (string, tb.Client, uint64) {
	t.Helper()

	const clusterID = 0

	tbBin := findTigerBeetleBinary(t)
	dataFile := filepath.Join(t.TempDir(), "data.tigerbeetle")
	address := fmt.Sprintf("127.0.0.1:%d", freePort(t))

	runCmd(t, exec.Command(tbBin, "format", "--cluster=0", "--replica=0", "--replica-count=1", dataFile))
	t.Log("tigerbeetle data file formatted")

	cmd := exec.Command(tbBin, "start", "--addresses="+address, dataFile)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start tigerbeetle: %v", err)
	}
	t.Logf("tigerbeetle started on %s", address)
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	waitForTCP(t, address)
	t.Log("tigerbeetle tcp ready")

	client, err := tb.NewClient(tbtypes.ToUint128(clusterID), []string{address})
	if err != nil {
		t.Fatalf("create tigerbeetle client: %v", err)
	}
	t.Log("tigerbeetle client ready")
	t.Cleanup(client.Close)

	return address, client, clusterID
}

func postgresMigrationPaths(t *testing.T) []string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve caller")
	}

	paths, err := filepath.Glob(filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "postgresql-migrations", "[0-9][0-9][0-9]_*.up.sql")))
	if err != nil {
		t.Fatalf("glob postgres migrations: %v", err)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		t.Fatal("no postgres migrations found")
	}

	return paths
}

func findTigerBeetleBinary(t *testing.T) string {
	t.Helper()

	if value := os.Getenv("FORGE_METAL_TIGERBEETLE_BIN"); value != "" {
		return value
	}
	if path, err := exec.LookPath("tigerbeetle"); err == nil {
		return path
	}

	t.Skip("tigerbeetle binary not found; set FORGE_METAL_TIGERBEETLE_BIN")
	return ""
}

func mustLookPath(t *testing.T, name string) string {
	t.Helper()

	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("look up %s: %v", name, err)
	}
	return path
}

func runCmd(t *testing.T, cmd *exec.Cmd) {
	t.Helper()

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(cmd.Args, " "), err, output)
	}
}

func freePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate port: %v", err)
	}
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port
}

func waitForTCP(t *testing.T, address string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", address)
}

func validDisplayName(raw string) string {
	var builder strings.Builder
	runes := 0
	for _, r := range raw {
		if r == 0 || !unicode.IsPrint(r) {
			continue
		}
		builder.WriteRune(r)
		runes++
		if runes == 24 {
			break
		}
	}
	if builder.Len() == 0 {
		return "org"
	}
	return builder.String()
}
