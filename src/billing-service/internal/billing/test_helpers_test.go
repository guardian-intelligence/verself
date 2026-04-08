package billing

import (
	"context"
	"database/sql"
	"encoding/json"
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
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stripe/stripe-go/v85"
	tb "github.com/tigerbeetle/tigerbeetle-go"
	tbtypes "github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

// ---------------------------------------------------------------------------
// SOPS-based Stripe credential loading
// ---------------------------------------------------------------------------

// stripeTestKeys holds Stripe test-mode credentials loaded from SOPS-encrypted
// secrets. The test suite requires real Stripe test-mode keys — mocks are not used.
type stripeTestKeys struct {
	PublishableKey    string
	SecretKey         string
	WebhookEndpointID string
}

// sopsSecrets caches the decrypted secrets map across all tests in the package.
// Decryption happens once per test binary invocation.
var (
	sopsOnce    sync.Once
	sopsMap     map[string]string
	sopsLoadErr error
)

func loadSOPSSecrets() (map[string]string, error) {
	sopsOnce.Do(func() {
		sopsPath := sopsSecretsPath()

		if _, err := os.Stat(sopsPath); err != nil {
			sopsLoadErr = fmt.Errorf("secrets file not found at %s: %w\n\n"+
				"  Run from the repo root, or check that the repo is complete.", sopsPath, err)
			return
		}

		sopsBin, err := exec.LookPath("sops")
		if err != nil {
			sopsLoadErr = fmt.Errorf("sops binary not found: %w\n\n"+
				"  Install dev tools:\n"+
				"    cd src/platform/ansible && ansible-playbook playbooks/setup-dev.yml", err)
			return
		}

		cmd := exec.Command(sopsBin, "-d", "--output-type", "json", sopsPath)
		output, err := cmd.Output()
		if err != nil {
			stderr := ""
			if ee, ok := err.(*exec.ExitError); ok {
				stderr = string(ee.Stderr)
			}
			sopsLoadErr = fmt.Errorf("sops decrypt failed: %w\n%s\n\n"+
				"  Check that your Age private key exists at ~/.config/sops/age/keys.txt\n"+
				"  If missing, run: cd src/platform/ansible && ansible-playbook playbooks/setup-sops.yml", err, stderr)
			return
		}

		var secrets map[string]string
		if err := json.Unmarshal(output, &secrets); err != nil {
			sopsLoadErr = fmt.Errorf("parse sops json output: %w", err)
			return
		}
		sopsMap = secrets
	})
	return sopsMap, sopsLoadErr
}

// sopsSecretsPath returns the absolute path to the SOPS-encrypted secrets file,
// resolved relative to this source file (same pattern as postgresMigrationPaths).
func sopsSecretsPath() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("resolve caller for sops secrets path")
	}
	return filepath.Clean(filepath.Join(
		filepath.Dir(file), "..", "platform", "ansible", "group_vars", "all", "secrets.sops.yml",
	))
}

func requireSOPSSecret(t *testing.T, key string) string {
	t.Helper()

	secrets, err := loadSOPSSecrets()
	if err != nil {
		t.Fatalf("loading SOPS secrets: %v", err)
	}

	value, ok := secrets[key]
	if !ok || value == "" {
		t.Fatalf("required secret %q not found in SOPS secrets file.\n\n"+
			"  Add it with:\n"+
			"    sops set src/platform/ansible/group_vars/all/secrets.sops.yml '[\"%s\"]' '\"<value>\"'\n\n"+
			"  Or edit interactively:\n"+
			"    make edit-secrets", key, key)
	}

	return value
}

// requireStripeTestKeys loads Stripe test-mode credentials from the
// SOPS-encrypted secrets file. Fatals with setup instructions if sops is
// missing, decryption fails, or any key is absent.
func requireStripeTestKeys(t *testing.T) stripeTestKeys {
	t.Helper()

	pk := requireSOPSSecret(t, "stripe_publishable_key")
	sk := requireSOPSSecret(t, "stripe_secret_key")
	weID := requireSOPSSecret(t, "stripe_test_webhook_endpoint_id")

	if !strings.HasPrefix(pk, "pk_test_") {
		t.Fatalf("stripe_publishable_key must start with pk_test_ (got prefix %q) — test-mode keys only", truncateKey(pk))
	}
	if !strings.HasPrefix(sk, "sk_test_") {
		t.Fatalf("stripe_secret_key must start with sk_test_ (got prefix %q) — test-mode keys only", truncateKey(sk))
	}
	if !strings.HasPrefix(weID, "we_") {
		t.Fatalf("stripe_test_webhook_endpoint_id must start with we_ (got %q)", weID)
	}

	return stripeTestKeys{
		PublishableKey:    pk,
		SecretKey:         sk,
		WebhookEndpointID: weID,
	}
}

func truncateKey(key string) string {
	if len(key) <= 12 {
		return key
	}
	return key[:12] + "..."
}

// ---------------------------------------------------------------------------
// Test environment: PG + TB + Stripe
// ---------------------------------------------------------------------------

type billingTestEnv struct {
	pgDSN    string
	pg       *sql.DB
	tb       tb.Client
	tbAddr   string
	stripe   stripeTestKeys
	client   *Client
	stripeSC *stripe.Client
}

// newBillingTestEnv starts PostgreSQL and TigerBeetle, loads Stripe keys from
// SOPS, and constructs a fully wired billing.Client. The returned stripeSC is
// a raw Stripe SDK client for test-side verification (e.g. session retrieval).
func newBillingTestEnv(t *testing.T) billingTestEnv {
	t.Helper()

	sk := requireStripeTestKeys(t)
	pg, pgDSN := startPostgresForBillingTests(t)
	tbAddr, tbClient, clusterID := startTigerBeetleForBillingTests(t)

	sc := stripe.NewClient(sk.SecretKey)

	cfg := DefaultConfig()
	cfg.StripeSecretKey = sk.SecretKey
	cfg.TigerBeetleAddresses = []string{tbAddr}
	cfg.TigerBeetleClusterID = clusterID

	client, err := NewClient(tbClient, pg, sc, noopMeteringWriter{}, cfg)
	if err != nil {
		t.Fatalf("new billing client: %v", err)
	}

	return billingTestEnv{
		pgDSN:    pgDSN,
		pg:       pg,
		tb:       tbClient,
		tbAddr:   tbAddr,
		stripe:   sk,
		client:   client,
		stripeSC: sc,
	}
}

// ---------------------------------------------------------------------------
// PostgreSQL helpers
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// TigerBeetle helpers
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// General helpers
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Metering stubs (temporary until ClickHouse test infra lands)
// ---------------------------------------------------------------------------

type noopMeteringWriter struct{}

func (noopMeteringWriter) InsertMeteringRow(_ context.Context, _ MeteringRow) error {
	return nil
}
