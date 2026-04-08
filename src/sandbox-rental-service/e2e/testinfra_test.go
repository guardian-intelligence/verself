// Test infrastructure: spawns real PG (two databases), TigerBeetle, and
// ClickHouse processes per test. Loads Stripe test-mode keys from SOPS.
// Helpers are duplicated from billing/test_helpers_test.go because Go's
// _test.go visibility rules prevent cross-module imports.
package e2e_test

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

	"github.com/ClickHouse/clickhouse-go/v2"
	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	_ "github.com/lib/pq"
	tb "github.com/tigerbeetle/tigerbeetle-go"
	tbtypes "github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

// ---------------------------------------------------------------------------
// SOPS secret loading (same pattern as billing/test_helpers_test.go)
// ---------------------------------------------------------------------------

type stripeTestKeys struct {
	PublishableKey    string
	SecretKey         string
	WebhookEndpointID string
}

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
			sopsLoadErr = fmt.Errorf("sops decrypt failed: %w\n%s", err, stderr)
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

func sopsSecretsPath() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("resolve caller for sops secrets path")
	}
	return filepath.Clean(filepath.Join(
		filepath.Dir(file), "..", "..", "platform", "ansible", "group_vars", "all", "secrets.sops.yml",
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
		t.Fatalf("required secret %q not found in SOPS secrets file", key)
	}
	return value
}

func requireStripeTestKeys(t *testing.T) stripeTestKeys {
	t.Helper()
	pk := requireSOPSSecret(t, "stripe_publishable_key")
	sk := requireSOPSSecret(t, "stripe_secret_key")
	weID := requireSOPSSecret(t, "stripe_test_webhook_endpoint_id")
	if !strings.HasPrefix(pk, "pk_test_") {
		t.Fatalf("stripe_publishable_key must start with pk_test_")
	}
	if !strings.HasPrefix(sk, "sk_test_") {
		t.Fatalf("stripe_secret_key must start with sk_test_")
	}
	if !strings.HasPrefix(weID, "we_") {
		t.Fatalf("stripe_test_webhook_endpoint_id must start with we_")
	}
	return stripeTestKeys{PublishableKey: pk, SecretKey: sk, WebhookEndpointID: weID}
}

// ---------------------------------------------------------------------------
// PostgreSQL: one instance, two databases
// ---------------------------------------------------------------------------

type pgEnv struct {
	billingDB  *sql.DB
	billingDSN string
	rentalDB   *sql.DB
	rentalDSN  string
}

func startPostgresForE2E(t *testing.T) pgEnv {
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

	port := freePort(t)
	logFile := filepath.Join(t.TempDir(), "postgres.log")
	runCmd(t, exec.Command(pgCtl, "-D", dataDir, "-l", logFile, "-w", "start",
		"-o", fmt.Sprintf("-p %d -k %s -h 127.0.0.1", port, socketDir)))
	t.Logf("postgres started on 127.0.0.1:%d", port)
	t.Cleanup(func() {
		_ = exec.Command(pgCtl, "-D", dataDir, "-m", "immediate", "stop").Run()
	})

	// Create and migrate billing database
	runCmd(t, exec.Command(createdb, "-h", "127.0.0.1", "-p", strconv.Itoa(port), "-U", "postgres", "sandbox"))
	for _, path := range migrationPaths(t, "billing-service", "postgresql-migrations") {
		runCmd(t, exec.Command(psql, "-h", "127.0.0.1", "-p", strconv.Itoa(port), "-U", "postgres",
			"-d", "sandbox", "-v", "ON_ERROR_STOP=1", "-f", path))
	}
	billingDSN := fmt.Sprintf("postgres://postgres@127.0.0.1:%d/sandbox?sslmode=disable", port)
	billingDB := openPG(t, billingDSN)

	// Create and migrate sandbox-rental database
	runCmd(t, exec.Command(createdb, "-h", "127.0.0.1", "-p", strconv.Itoa(port), "-U", "postgres", "sandbox_rental"))
	for _, path := range migrationPaths(t, "sandbox-rental-service", "migrations") {
		runCmd(t, exec.Command(psql, "-h", "127.0.0.1", "-p", strconv.Itoa(port), "-U", "postgres",
			"-d", "sandbox_rental", "-v", "ON_ERROR_STOP=1", "-f", path))
	}
	rentalDSN := fmt.Sprintf("postgres://postgres@127.0.0.1:%d/sandbox_rental?sslmode=disable", port)
	rentalDB := openPG(t, rentalDSN)

	t.Log("postgres: billing + rental databases ready")
	return pgEnv{billingDB: billingDB, billingDSN: billingDSN, rentalDB: rentalDB, rentalDSN: rentalDSN}
}

func openPG(t *testing.T, dsn string) *sql.DB {
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
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func migrationPaths(t *testing.T, service, subdir string) []string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve caller")
	}
	pattern := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", service, subdir, "[0-9][0-9][0-9]_*.up.sql"))
	paths, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob %s: %v", pattern, err)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		t.Fatalf("no migrations found matching %s", pattern)
	}
	return paths
}

// ---------------------------------------------------------------------------
// TigerBeetle
// ---------------------------------------------------------------------------

func startTigerBeetleForE2E(t *testing.T) (string, tb.Client, uint64) {
	t.Helper()

	const clusterID = 0
	tbBin := findTigerBeetleBinary(t)
	dataFile := filepath.Join(t.TempDir(), "data.tigerbeetle")
	address := fmt.Sprintf("127.0.0.1:%d", freePort(t))

	runCmd(t, exec.Command(tbBin, "format", "--cluster=0", "--replica=0", "--replica-count=1", dataFile))

	cmd := exec.Command(tbBin, "start", "--addresses="+address, dataFile)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start tigerbeetle: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	waitForTCP(t, address)
	t.Logf("tigerbeetle ready on %s", address)

	client, err := tb.NewClient(tbtypes.ToUint128(clusterID), []string{address})
	if err != nil {
		t.Fatalf("create tigerbeetle client: %v", err)
	}
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
// ClickHouse
// ---------------------------------------------------------------------------

func startClickHouseForE2E(t *testing.T) (chdriver.Conn, string) {
	t.Helper()

	chBin := findClickHouseBinary(t)
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	tmpPath := filepath.Join(tmpDir, "tmp")
	userFilesPath := filepath.Join(tmpDir, "user_files")
	formatSchemaPath := filepath.Join(tmpDir, "format_schemas")
	logDir := filepath.Join(tmpDir, "log")
	for _, d := range []string{dataDir, tmpPath, userFilesPath, formatSchemaPath, logDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	tcpPort := freePort(t)
	httpPort := freePort(t)
	address := fmt.Sprintf("127.0.0.1:%d", tcpPort)

	cmd := exec.Command(chBin, "server", "--",
		"--path="+dataDir+"/",
		"--tmp_path="+tmpPath+"/",
		"--user_files_path="+userFilesPath+"/",
		"--format_schema_path="+formatSchemaPath+"/",
		"--tcp_port="+strconv.Itoa(tcpPort),
		"--http_port="+strconv.Itoa(httpPort),
		"--listen_host=127.0.0.1",
		"--logger.log="+filepath.Join(logDir, "clickhouse-server.log"),
		"--logger.errorlog="+filepath.Join(logDir, "clickhouse-server.err.log"),
		"--logger.level=warning",
		"--mark_cache_size=5368709120",
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start clickhouse: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	waitForTCP(t, address)
	t.Logf("clickhouse ready on %s", address)

	conn, err := openClickHouseConn(address)
	if err != nil {
		t.Fatalf("open clickhouse: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create database and apply schemas
	if err := conn.Exec(ctx, "CREATE DATABASE IF NOT EXISTS forge_metal"); err != nil {
		t.Fatalf("create forge_metal database: %v", err)
	}

	for _, schemaFile := range clickHouseSchemaPaths(t) {
		data, err := os.ReadFile(schemaFile)
		if err != nil {
			t.Fatalf("read schema %s: %v", schemaFile, err)
		}
		// Split on semicolons — each file may have multiple statements
		for _, stmt := range strings.Split(string(data), ";") {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			if err := conn.Exec(ctx, stmt); err != nil {
				t.Fatalf("apply schema %s: %v\nstatement: %s", schemaFile, err, stmt[:min(200, len(stmt))])
			}
		}
	}

	t.Log("clickhouse: forge_metal database + schemas ready")
	return conn, address
}

func openClickHouseConn(address string) (chdriver.Conn, error) {
	return clickhouse.Open(&clickhouse.Options{
		Addr: []string{address},
		Auth: clickhouse.Auth{Username: "default"},
	})
}

func findClickHouseBinary(t *testing.T) string {
	t.Helper()
	if value := os.Getenv("FORGE_METAL_CLICKHOUSE_BIN"); value != "" {
		return value
	}
	if path, err := exec.LookPath("clickhouse"); err == nil {
		return path
	}
	t.Skip("clickhouse binary not found; set FORGE_METAL_CLICKHOUSE_BIN")
	return ""
}

func clickHouseSchemaPaths(t *testing.T) []string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve caller")
	}
	migrationsDir := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "platform", "migrations"))
	schemas := []string{
		filepath.Join(migrationsDir, "004_billing_metering.up.sql"),
		filepath.Join(migrationsDir, "007_sandbox_job_logs.up.sql"),
	}
	for _, s := range schemas {
		if _, err := os.Stat(s); err != nil {
			t.Fatalf("ClickHouse schema not found: %s", s)
		}
	}
	return schemas
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
	deadline := time.Now().Add(30 * time.Second)
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
