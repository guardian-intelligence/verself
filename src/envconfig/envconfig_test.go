package envconfig_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/forge-metal/envconfig"
)

func TestLoaderAggregatesAllErrors(t *testing.T) {
	t.Setenv("FM_TEST_COUNT", "not-an-int")
	os.Unsetenv("FM_TEST_REQUIRED")

	l := envconfig.New()
	_ = l.RequireString("FM_TEST_REQUIRED")
	_ = l.Int("FM_TEST_COUNT", 1)
	_ = l.RequireCredential("fm-test-missing-credential")

	err := l.Err()
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{"FM_TEST_REQUIRED", "FM_TEST_COUNT", "fm-test-missing-credential"} {
		if !strings.Contains(msg, want) {
			t.Errorf("expected aggregated error to mention %q, got %q", want, msg)
		}
	}
}

func TestLoaderReturnsFallbacksOnEmpty(t *testing.T) {
	os.Unsetenv("FM_TEST_LISTEN_ADDR")
	os.Unsetenv("FM_TEST_COUNT")
	os.Unsetenv("FM_TEST_FLAG")
	os.Unsetenv("FM_TEST_INTERVAL")

	l := envconfig.New()
	if got := l.String("FM_TEST_LISTEN_ADDR", "127.0.0.1:9999"); got != "127.0.0.1:9999" {
		t.Errorf("String fallback: got %q", got)
	}
	if got := l.Int("FM_TEST_COUNT", 7); got != 7 {
		t.Errorf("Int fallback: got %d", got)
	}
	if got := l.Bool("FM_TEST_FLAG", true); got != true {
		t.Errorf("Bool fallback: got %v", got)
	}
	if got := l.Duration("FM_TEST_INTERVAL", 3*time.Second); got != 3*time.Second {
		t.Errorf("Duration fallback: got %v", got)
	}
	if err := l.Err(); err != nil {
		t.Fatalf("no errors expected, got %v", err)
	}
}

func TestLoaderParsesValidValues(t *testing.T) {
	t.Setenv("FM_TEST_COUNT", "42")
	t.Setenv("FM_TEST_FLAG", "yes")
	t.Setenv("FM_TEST_INTERVAL", "15s")
	t.Setenv("FM_TEST_URL", "https://example.com")

	l := envconfig.New()
	if got := l.Int("FM_TEST_COUNT", 0); got != 42 {
		t.Errorf("Int: got %d", got)
	}
	if got := l.Bool("FM_TEST_FLAG", false); got != true {
		t.Errorf("Bool yes: got %v", got)
	}
	if got := l.Duration("FM_TEST_INTERVAL", 0); got != 15*time.Second {
		t.Errorf("Duration: got %v", got)
	}
	if got := l.RequireURL("FM_TEST_URL"); got != "https://example.com" {
		t.Errorf("RequireURL: got %q", got)
	}
	if err := l.Err(); err != nil {
		t.Fatalf("no errors expected, got %v", err)
	}
}

func TestLoaderRejectsRelativeURL(t *testing.T) {
	t.Setenv("FM_TEST_URL", "not-a-url")
	l := envconfig.New()
	_ = l.RequireURL("FM_TEST_URL")
	if err := l.Err(); err == nil {
		t.Fatal("expected URL rejection")
	}
}

func TestRequireCredentialReadsTrimmedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit-hmac-key")
	if err := os.WriteFile(path, []byte("  secret-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envconfig.CredentialsDirectoryEnv, dir)

	l := envconfig.New()
	got := l.RequireCredential("audit-hmac-key")
	if got != "secret-value" {
		t.Errorf("RequireCredential: got %q", got)
	}
	if err := l.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRequireCredentialEmptyFileFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty")
	if err := os.WriteFile(path, []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envconfig.CredentialsDirectoryEnv, dir)

	l := envconfig.New()
	_ = l.RequireCredential("empty")
	if err := l.Err(); err == nil {
		t.Fatal("expected empty-credential failure")
	}
}

func TestCredentialOrIsSilentOnMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(envconfig.CredentialsDirectoryEnv, dir)

	l := envconfig.New()
	got := l.CredentialOr("absent", "fallback-value")
	if got != "fallback-value" {
		t.Errorf("CredentialOr fallback: got %q", got)
	}
	if err := l.Err(); err != nil {
		t.Fatalf("expected no accumulated error, got %v", err)
	}
}

func TestRequireCredentialPathChecksExistence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(envconfig.CredentialsDirectoryEnv, dir)

	l := envconfig.New()
	_ = l.RequireCredentialPath("does-not-exist")
	err := l.Err()
	if err == nil {
		t.Fatal("expected missing-file failure")
	}
	if !errors.Is(err, os.ErrNotExist) {
		// os.Stat wraps os.PathError; errors.Is traverses.
		t.Errorf("expected errors.Is(err, os.ErrNotExist), got %v", err)
	}
}
