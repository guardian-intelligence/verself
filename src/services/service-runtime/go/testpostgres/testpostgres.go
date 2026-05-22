package testpostgres

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const requiredEnv = "VERSELF_TEST_POSTGRES_REQUIRED"

type Options struct {
	Database       string
	StartupTimeout time.Duration
}

type Cluster struct {
	DSN       string
	DataDir   string
	SocketDir string
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func Start(ctx context.Context, t testing.TB, opts Options) *Cluster {
	t.Helper()
	if opts.Database == "" {
		opts.Database = "verself_test"
	}
	if opts.StartupTimeout == 0 {
		opts.StartupTimeout = 10 * time.Second
	}
	required := os.Getenv(requiredEnv) == "1"
	if os.Geteuid() == 0 {
		unavailable(t, required, "postgres refuses to run as root")
	}

	initdb := requireProgram(t, required, "initdb")
	postgres := requireProgram(t, required, "postgres")
	createdb := requireProgram(t, required, "createdb")
	pgIsReady := requireProgram(t, required, "pg_isready")

	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	socketDir := filepath.Join(root, "socket")
	if err := os.MkdirAll(socketDir, 0o700); err != nil {
		t.Fatalf("create postgres socket dir: %v", err)
	}

	run(t, ctx, nil, initdb, "-A", "trust", "-U", "postgres", "-D", dataDir, "--no-instructions")

	startCtx, cancel := context.WithCancel(ctx)
	logs := &lockedBuffer{}
	cmd := exec.CommandContext(startCtx, postgres,
		"-D", dataDir,
		"-k", socketDir,
		"-h", "",
		"-p", "5432",
		"-c", "listen_addresses=",
		"-c", "fsync=off",
		"-c", "synchronous_commit=off",
		"-c", "full_page_writes=off",
	)
	cmd.Env = postgresEnv()
	cmd.Stdout = logs
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() {
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cancel()
			_ = cmd.Process.Kill()
			<-done
		}
		cancel()
		if t.Failed() {
			t.Logf("postgres log:\n%s", logs.String())
		}
	})

	deadline := time.Now().Add(opts.StartupTimeout)
	for {
		ready := exec.CommandContext(ctx, pgIsReady, "-h", socketDir, "-p", "5432", "-U", "postgres", "-d", "postgres", "-q")
		ready.Env = postgresEnv()
		if err := ready.Run(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("postgres did not become ready within %s\n%s", opts.StartupTimeout, logs.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	run(t, ctx, logs, createdb, "-h", socketDir, "-p", "5432", "-U", "postgres", opts.Database)

	return &Cluster{
		DSN:       dsn(socketDir, opts.Database),
		DataDir:   dataDir,
		SocketDir: socketDir,
	}
}

func unavailable(t testing.TB, required bool, reason string) {
	t.Helper()
	if required {
		t.Fatalf("local postgres test substrate unavailable: %s", reason)
	}
	t.Skipf("local postgres test substrate unavailable: %s", reason)
}

func requireProgram(t testing.TB, required bool, name string) string {
	t.Helper()
	path, err := findProgram(name)
	if err == nil {
		return path
	}
	unavailable(t, required, err.Error())
	return ""
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
	return "", fmt.Errorf("%s not found on PATH or under /usr/lib/postgresql/*/bin", name)
}

func candidateBinDirs() []string {
	dirs := []string{
		"/opt/verself/profile/bin",
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

func run(t testing.TB, ctx context.Context, inheritedLogs *lockedBuffer, program string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = postgresEnv()
	var localLogs lockedBuffer
	if inheritedLogs != nil {
		cmd.Stdout = inheritedLogs
		cmd.Stderr = inheritedLogs
	} else {
		cmd.Stdout = &localLogs
		cmd.Stderr = &localLogs
	}
	if err := cmd.Run(); err != nil {
		if inheritedLogs != nil {
			t.Fatalf("%s %s: %v\n%s", program, strings.Join(args, " "), err, inheritedLogs.String())
		}
		t.Fatalf("%s %s: %v\n%s", program, strings.Join(args, " "), err, localLogs.String())
	}
}

func postgresEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+2)
	for _, value := range env {
		key, _, ok := strings.Cut(value, "=")
		if ok && strings.HasPrefix(key, "PG") {
			continue
		}
		out = append(out, value)
	}
	out = append(out, "LC_ALL=C", "TZ=UTC")
	return out
}

func dsn(socketDir, database string) string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.User("postgres"),
		Path:   "/" + database,
	}
	q := u.Query()
	q.Set("host", socketDir)
	q.Set("sslmode", "disable")
	u.RawQuery = q.Encode()
	return u.String()
}
