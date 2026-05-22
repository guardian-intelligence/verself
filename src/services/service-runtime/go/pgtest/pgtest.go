package pgtest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"net/url"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

const port = "55432"

var safeIdent = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

type Template struct {
	Service     string
	Fingerprint string
	Migrate     func(context.Context, string) error
	Seed        func(context.Context, string) error
}

type Database struct {
	DSN          string
	Name         string
	TemplateName string
}

func Acquire(ctx context.Context, t testing.TB, template Template) *Database {
	t.Helper()
	if err := template.validate(); err != nil {
		t.Fatalf("pgtest template: %v", err)
	}
	adminDSN, err := ensureLocalServer(ctx)
	if err != nil {
		t.Fatalf("pgtest substrate unavailable: %v", err)
	}
	admin, err := sql.Open("postgres", adminDSN)
	if err != nil {
		t.Fatalf("open pgtest admin postgres: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if err := admin.PingContext(ctx); err != nil {
		t.Fatalf("ping pgtest admin postgres: %v", err)
	}

	templateName := templateName(template)
	ensureTemplate(ctx, t, admin, adminDSN, templateName, template)
	dbName := testDatabaseName(template.Service)
	dropDatabase(ctx, t, admin, dbName)
	execAdmin(ctx, t, admin, "CREATE DATABASE "+quoteIdent(dbName)+" TEMPLATE "+quoteIdent(templateName))
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		dropDatabase(cleanupCtx, t, admin, dbName)
	})

	dsn, err := databaseDSN(adminDSN, dbName)
	if err != nil {
		t.Fatalf("build test postgres dsn: %v", err)
	}
	return &Database{DSN: dsn, Name: dbName, TemplateName: templateName}
}

func (template Template) validate() error {
	if strings.TrimSpace(template.Service) == "" {
		return errors.New("service is required")
	}
	if strings.TrimSpace(template.Fingerprint) == "" {
		return errors.New("fingerprint is required")
	}
	if template.Migrate == nil {
		return errors.New("migrate is required")
	}
	return nil
}

func ensureTemplate(ctx context.Context, t testing.TB, admin *sql.DB, adminDSN, name string, template Template) {
	t.Helper()
	lock := advisoryLockKey("pgtest:" + name)
	execAdmin(ctx, t, admin, "SELECT pg_advisory_lock($1)", lock)
	defer execAdmin(context.Background(), t, admin, "SELECT pg_advisory_unlock($1)", lock)

	var exists bool
	if err := admin.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).Scan(&exists); err != nil {
		t.Fatalf("check pgtest template %s: %v", name, err)
	}
	if exists {
		return
	}

	scratch := name + "_build"
	dropDatabase(ctx, t, admin, scratch)
	execAdmin(ctx, t, admin, "CREATE DATABASE "+quoteIdent(scratch)+" TEMPLATE template0")
	scratchDSN, err := databaseDSN(adminDSN, scratch)
	if err != nil {
		t.Fatalf("build pgtest template dsn: %v", err)
	}
	if err := template.Migrate(ctx, scratchDSN); err != nil {
		dropDatabase(context.Background(), t, admin, scratch)
		t.Fatalf("migrate pgtest template %s: %v", name, err)
	}
	if template.Seed != nil {
		if err := template.Seed(ctx, scratchDSN); err != nil {
			dropDatabase(context.Background(), t, admin, scratch)
			t.Fatalf("seed pgtest template %s: %v", name, err)
		}
	}
	terminateDatabase(ctx, t, admin, scratch)
	execAdmin(ctx, t, admin, "ALTER DATABASE "+quoteIdent(scratch)+" WITH ALLOW_CONNECTIONS false")
	execAdmin(ctx, t, admin, "ALTER DATABASE "+quoteIdent(scratch)+" RENAME TO "+quoteIdent(name))
}

func ensureLocalServer(ctx context.Context) (string, error) {
	root, err := localRoot()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("create pgtest root %s: %w", root, err)
	}
	lockFile, err := os.OpenFile(filepath.Join(root, "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return "", fmt.Errorf("open pgtest lock: %w", err)
	}
	defer func() { _ = lockFile.Close() }()
	if err := flock(lockFile, syscall.LOCK_EX); err != nil {
		return "", fmt.Errorf("lock pgtest root: %w", err)
	}
	defer func() { _ = flock(lockFile, syscall.LOCK_UN) }()

	initdb, err := findProgram("initdb")
	if err != nil {
		return "", err
	}
	postgres, err := findProgram("postgres")
	if err != nil {
		return "", err
	}
	pgIsReady, err := findProgram("pg_isready")
	if err != nil {
		return "", err
	}

	dataDir := filepath.Join(root, "data")
	socketDir := filepath.Join(root, "socket")
	if err := os.MkdirAll(socketDir, 0o700); err != nil {
		return "", fmt.Errorf("create pgtest socket dir: %w", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "PG_VERSION")); errors.Is(err, os.ErrNotExist) {
		if err := run(ctx, nil, initdb, "-A", "trust", "-U", "postgres", "-D", dataDir, "--no-instructions"); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", fmt.Errorf("stat pgtest cluster: %w", err)
	}

	if ready(ctx, pgIsReady, socketDir) {
		return localDSN(socketDir), nil
	}
	cleanupStalePostgres(dataDir, socketDir)
	if !ready(ctx, pgIsReady, socketDir) {
		if err := startPostgres(root, dataDir, socketDir, postgres); err != nil {
			return "", err
		}
		if err := waitReady(ctx, pgIsReady, socketDir); err != nil {
			return "", err
		}
	}
	return localDSN(socketDir), nil
}

func startPostgres(root, dataDir, socketDir, postgres string) error {
	logFile, err := os.OpenFile(filepath.Join(root, "postgres.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open pgtest postgres log: %w", err)
	}
	cmd := exec.Command(postgres,
		"-D", dataDir,
		"-k", socketDir,
		"-h", "",
		"-p", port,
		"-c", "listen_addresses=",
		"-c", "fsync=off",
		"-c", "synchronous_commit=off",
		"-c", "full_page_writes=off",
	)
	cmd.Env = postgresEnv()
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("start pgtest postgres: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("release pgtest postgres: %w", err)
	}
	_ = logFile.Close()
	return nil
}

func waitReady(ctx context.Context, pgIsReady, socketDir string) error {
	deadline := time.Now().Add(10 * time.Second)
	for {
		if ready(ctx, pgIsReady, socketDir) {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("pgtest postgres did not become ready")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func ready(ctx context.Context, pgIsReady, socketDir string) bool {
	cmd := exec.CommandContext(ctx, pgIsReady, "-h", socketDir, "-p", port, "-U", "postgres", "-d", "postgres", "-q")
	cmd.Env = postgresEnv()
	return cmd.Run() == nil
}

func cleanupStalePostgres(dataDir, socketDir string) {
	pid, ok := readPostmasterPID(filepath.Join(dataDir, "postmaster.pid"))
	if ok && processExists(pid) {
		return
	}
	_ = os.Remove(filepath.Join(dataDir, "postmaster.pid"))
	_ = os.Remove(filepath.Join(socketDir, ".s.PGSQL."+port))
	_ = os.Remove(filepath.Join(socketDir, ".s.PGSQL."+port+".lock"))
}

func readPostmasterPID(path string) (int, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	line, _, _ := strings.Cut(string(raw), "\n")
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(line), "%d", &pid); err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func localRoot() (string, error) {
	if current, err := osuser.Current(); err == nil && strings.TrimSpace(current.HomeDir) != "" {
		return filepath.Join(current.HomeDir, ".cache", "verself", "pgtest"), nil
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".cache", "verself", "pgtest"), nil
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".cache", "verself", "pgtest"), nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", errors.New("HOME is required for local pgtest server")
	}
	return filepath.Join(cache, "verself", "pgtest"), nil
}

func localDSN(socketDir string) string {
	u := url.URL{Scheme: "postgres", User: url.User("postgres"), Path: "/postgres"}
	q := u.Query()
	q.Set("host", socketDir)
	q.Set("port", port)
	q.Set("sslmode", "disable")
	u.RawQuery = q.Encode()
	return u.String()
}

func dropDatabase(ctx context.Context, t testing.TB, admin *sql.DB, name string) {
	t.Helper()
	terminateDatabase(ctx, t, admin, name)
	execAdmin(ctx, t, admin, "DROP DATABASE IF EXISTS "+quoteIdent(name)+" WITH (FORCE)")
}

func terminateDatabase(ctx context.Context, t testing.TB, admin *sql.DB, name string) {
	t.Helper()
	execAdmin(ctx, t, admin, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()", name)
}

func execAdmin(ctx context.Context, t testing.TB, admin *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := admin.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("pgtest exec %q: %v", query, err)
	}
}

func databaseDSN(base, database string) (string, error) {
	if !safeIdent.MatchString(database) {
		return "", fmt.Errorf("unsafe database name %q", database)
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	u.Path = "/" + database
	return u.String(), nil
}

func templateName(template Template) string {
	sum := sha256.Sum256([]byte(template.Service + "\x00" + template.Fingerprint))
	return "vs_tpl_" + identPart(template.Service, 20) + "_" + hex.EncodeToString(sum[:8])
}

func testDatabaseName(service string) string {
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		binary.BigEndian.PutUint64(nonce[:], uint64(time.Now().UnixNano()))
	}
	return "vs_test_" + identPart(service, 20) + "_" + hex.EncodeToString(nonce[:])
}

func identPart(value string, max int) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		out = "service"
	}
	if len(out) > max {
		out = out[:max]
	}
	return out
}

func quoteIdent(name string) string {
	if !safeIdent.MatchString(name) {
		panic("unsafe postgres identifier: " + name)
	}
	return `"` + name + `"`
}

func advisoryLockKey(value string) int64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(value))
	return int64(h.Sum32())
}

func flock(file *os.File, how int) error {
	fd := file.Fd()
	if fd > uintptr(^uint(0)>>1) {
		return fmt.Errorf("file descriptor %d overflows int", fd)
	}
	return syscall.Flock(int(fd), how)
}

func run(ctx context.Context, logs *os.File, program string, args ...string) error {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = postgresEnv()
	if logs != nil {
		cmd.Stdout = logs
		cmd.Stderr = logs
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %w\n%s", program, strings.Join(args, " "), err, string(out))
	}
	return nil
}

func findProgram(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	for _, dir := range candidateBinDirs() {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return path, nil
		}
	}
	matches, err := filepath.Glob("/usr/lib/postgresql/*/bin/" + name)
	if err != nil {
		return "", err
	}
	for i := len(matches) - 1; i >= 0; i-- {
		if info, statErr := os.Stat(matches[i]); statErr == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return matches[i], nil
		}
	}
	return "", fmt.Errorf("%s not found in PATH, known tool dirs, or /usr/lib/postgresql/*/bin", name)
}

func candidateBinDirs() []string {
	dirs := []string{
		"/opt/verself/profile/bin",
		"/home/runner/.nix-profile/bin",
		"/home/ubuntu/.nix-profile/bin",
		"/usr/local/bin",
		"/usr/bin",
		"/bin",
		"/nix/var/nix/profiles/default/bin",
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		dirs = append(dirs, filepath.Join(home, ".nix-profile/bin"))
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		dirs = append(dirs, filepath.Join(home, ".nix-profile/bin"))
	}
	return dirs
}

func postgresEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+2)
	for _, value := range env {
		key, _, ok := strings.Cut(value, "=")
		if ok && (strings.HasPrefix(key, "PG") || key == "RUNNER_TRACKING_ID") {
			continue
		}
		out = append(out, value)
	}
	out = append(out, "LC_ALL=C", "TZ=UTC")
	return out
}
