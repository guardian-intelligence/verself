// Package envconfig loads process configuration from environment variables
// and systemd LoadCredential= files. It is the single source of truth for how
// Verself Go binaries consume their runtime settings.
//
// The Loader accumulates errors instead of failing on the first missing value
// so one misconfigured deploy reports every missing env and credential at once
// rather than forcing five round-trips through Ansible + systemd.
package envconfig

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// CredentialsDirectoryEnv is the systemd-provided env pointing at the
// per-unit credential directory (LoadCredential=, LoadCredentialEncrypted=).
const CredentialsDirectoryEnv = "CREDENTIALS_DIRECTORY"

// Loader accumulates every env/credential failure encountered during startup.
// Call Err() once all values have been read.
type Loader struct {
	errs []error
}

// New returns a fresh Loader.
func New() *Loader {
	return &Loader{}
}

// Err returns a joined error containing every accumulated failure, or nil if
// all required values were present and parseable.
func (l *Loader) Err() error {
	if len(l.errs) == 0 {
		return nil
	}
	return errors.Join(l.errs...)
}

func (l *Loader) fail(key, problem string) {
	l.errs = append(l.errs, fmt.Errorf("envconfig: %s %s", key, problem))
}

func (l *Loader) failErr(key string, err error) {
	l.errs = append(l.errs, fmt.Errorf("envconfig: %s: %w", key, err))
}

// String returns the trimmed value of key or fallback if unset/empty.
func (l *Loader) String(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

// RequireString returns the trimmed value of key. Records a failure if unset.
func (l *Loader) RequireString(key string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		l.fail(key, "is required")
		return ""
	}
	return value
}

// Int parses key as an integer or returns fallback if unset. Records a
// failure (and returns fallback) if the value is set but unparseable.
func (l *Loader) Int(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		l.fail(key, fmt.Sprintf("must be an integer (got %q)", raw))
		return fallback
	}
	return parsed
}

// Int64 is the int64 analogue of Int.
func (l *Loader) Int64(key string, fallback int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		l.fail(key, fmt.Sprintf("must be an int64 (got %q)", raw))
		return fallback
	}
	return parsed
}

// Uint64 is the uint64 analogue of Int.
func (l *Loader) Uint64(key string, fallback uint64) uint64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		l.fail(key, fmt.Sprintf("must be a uint64 (got %q)", raw))
		return fallback
	}
	return parsed
}

// Uint parses key as an unsigned int (capped at 32 bits so it fits on 32-bit
// builds) or returns fallback if unset/empty. Records a failure on parse
// error.
func (l *Loader) Uint(key string, fallback uint) uint {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		l.fail(key, fmt.Sprintf("must be an unsigned integer (got %q)", raw))
		return fallback
	}
	return uint(parsed)
}

// Bool parses key as a boolean (strconv.ParseBool plus the friendly yes/y
// variants) or returns fallback if unset.
func (l *Loader) Bool(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	switch strings.ToLower(raw) {
	case "1", "t", "true", "yes", "y":
		return true
	case "0", "f", "false", "no", "n":
		return false
	}
	l.fail(key, fmt.Sprintf("must be a boolean (got %q)", raw))
	return fallback
}

// Duration parses key as a time.Duration (via time.ParseDuration) or returns
// fallback if unset.
func (l *Loader) Duration(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		l.fail(key, fmt.Sprintf("must be a duration (got %q): %v", raw, err))
		return fallback
	}
	return parsed
}

// URL returns the trimmed value of key if it parses as an absolute URL with a
// host. Returns fallback if unset; records a failure if the value is set but
// malformed.
func (l *Loader) URL(key, fallback string) string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	if err := validateURL(raw); err != nil {
		l.failErr(key, err)
		return fallback
	}
	return raw
}

// RequireURL is the required-value analogue of URL.
func (l *Loader) RequireURL(key string) string {
	raw := l.RequireString(key)
	if raw == "" {
		return ""
	}
	if err := validateURL(raw); err != nil {
		l.failErr(key, err)
		return ""
	}
	return raw
}

func validateURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if !parsed.IsAbs() || parsed.Hostname() == "" {
		return fmt.Errorf("must be an absolute URL with host (got %q)", raw)
	}
	return nil
}

// CredentialPath returns <CREDENTIALS_DIRECTORY>/<name> without reading the
// file. Records a failure if CREDENTIALS_DIRECTORY is unset.
func (l *Loader) CredentialPath(name string) string {
	base := strings.TrimSpace(os.Getenv(CredentialsDirectoryEnv))
	if base == "" {
		l.fail(CredentialsDirectoryEnv, "is required for credential "+name)
		return ""
	}
	return filepath.Join(base, name)
}

// RequireCredentialPath returns the credential path and confirms the file
// exists and is readable.
func (l *Loader) RequireCredentialPath(name string) string {
	path := l.CredentialPath(name)
	if path == "" {
		return ""
	}
	if _, err := os.Stat(path); err != nil {
		l.failErr("credential "+name, err)
		return ""
	}
	return path
}

// RequireCredential reads <CREDENTIALS_DIRECTORY>/<name> and returns the
// trimmed file contents. An empty file is treated as a missing credential.
func (l *Loader) RequireCredential(name string) string {
	path := l.CredentialPath(name)
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		l.failErr("credential "+name, err)
		return ""
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		l.fail("credential "+name, "is empty")
		return ""
	}
	return value
}

// CredentialOr reads the credential file if it exists and falls back silently
// otherwise. Use only for genuinely optional credentials (e.g. mailbox
// forward-to overrides). No error is accumulated.
func (l *Loader) CredentialOr(name, fallback string) string {
	base := strings.TrimSpace(os.Getenv(CredentialsDirectoryEnv))
	if base == "" {
		return fallback
	}
	data, err := os.ReadFile(filepath.Join(base, name))
	if err != nil {
		return fallback
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return fallback
	}
	return value
}

// RequireFile reads the absolute path and returns its trimmed contents.
// Records a failure if the file is missing, unreadable, or empty.
func (l *Loader) RequireFile(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		l.errs = append(l.errs, errors.New("envconfig: RequireFile called with empty path"))
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		l.failErr("file "+path, err)
		return ""
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		l.fail("file "+path, "is empty")
		return ""
	}
	return value
}
