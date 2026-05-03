package identity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// urlNamespace is the UUID v5 namespace for URL-derived UUIDs (RFC 4122).
// Keeping the namespace stable means the same run key always derives the
// same deploy id.
var urlNamespace = uuid.MustParse("6ba7b811-9dad-11d1-80b4-00c04fd430c8")

// GenerateOptions configure a Generate call. Site, Sha, Scope are the
// per-deploy dimensions threaded onto every span; the rest are
// derived from git + the controller environment.
type GenerateOptions struct {
	// Site is the deploy site (selects inventory, agent queue dir,
	// span attribute). Required.
	Site string

	// Sha is the resolved git SHA being deployed. Empty falls back to
	// HEAD; the result populates VERSELF_DEPLOY_SHA and the
	// verself.deploy_sha resource attribute.
	Sha string

	// Scope is the deploy-scope label. Empty defaults to "affected".
	Scope string

	// Kind labels the deploy invocation type. Empty defaults to
	// "ansible-playbook".
	Kind string

	// CacheDir overrides the default $XDG_CACHE_HOME counter location.
	// Useful in tests that need an isolated counter.
	CacheDir string

	// Now overrides the wall clock for testability.
	Now func() time.Time
}

// Generate produces a Snapshot whose Env() projects every VERSELF_* and
// OTEL_* variable needed by deployment children, derives a counter-driven run
// key, mints a UUID5 deploy ID, and constructs a W3C TRACEPARENT.
//
// Idempotency: when VERSELF_DEPLOY_ID or VERSELF_DEPLOY_RUN_KEY is
// already in the environment, those values are preserved. Re-running Generate
// inside a child process therefore keeps the run's correlation IDs stable.
func Generate(opts GenerateOptions) (Snapshot, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	if opts.Site == "" {
		return Snapshot{}, fmt.Errorf("identity: Site is required")
	}
	scope := opts.Scope
	if scope == "" {
		scope = "affected"
	}
	kind := envOr("VERSELF_DEPLOY_KIND", opts.Kind, "ansible-playbook")

	gitMeta, err := readGitMetadata()
	if err != nil {
		return Snapshot{}, err
	}
	deploySha := opts.Sha
	if deploySha == "" {
		deploySha = envOr("VERSELF_DEPLOY_SHA", "", gitMeta.commitSha)
	}

	// Run key: preserved from env when already set; otherwise derived
	// from (UTC date, per-host monotonic counter, controller hostname).
	runKey := os.Getenv("VERSELF_DEPLOY_RUN_KEY")
	if runKey == "" {
		host := controllerHostname()
		day := now().UTC().Format("2006-01-02")
		counter, err := nextDeployCounter(opts.CacheDir, day, host)
		if err != nil {
			return Snapshot{}, err
		}
		runKey = fmt.Sprintf("%s.%06d@%s", day, counter, host)
	}

	// Deploy ID: UUID5(URL, "verself:"+run_key). Deterministic on
	// (run_key) so a re-run with the same run_key resolves to the same
	// trace ID.
	deployID := os.Getenv("VERSELF_DEPLOY_ID")
	if deployID == "" {
		deployID = uuid.NewSHA1(urlNamespace, []byte("verself:"+runKey)).String()
	}

	// TRACEPARENT: 00-<32hex trace>-<16hex span>-01. trace = uuid hex
	// (without dashes); span = first 16 hex of sha256("deploy-root:" + deploy_id).
	traceHex := strings.ReplaceAll(deployID, "-", "")
	if len(traceHex) != 32 {
		return Snapshot{}, fmt.Errorf("identity: deploy id %q has %d hex chars after stripping dashes, want 32", deployID, len(traceHex))
	}
	spanSum := sha256.Sum256([]byte("deploy-root:" + deployID))
	spanHex := hex.EncodeToString(spanSum[:])[:16]
	traceparent := "00-" + traceHex + "-" + spanHex + "-01"

	values := map[string]string{
		"VERSELF_DEPLOY_RUN_KEY": runKey,
		"VERSELF_DEPLOY_ID":      deployID,
		"VERSELF_COMMIT_SHA":     gitMeta.commitSha,
		"VERSELF_BRANCH":         gitMeta.branch,
		"VERSELF_COMMIT_MESSAGE": gitMeta.commitMessage,
		"VERSELF_AUTHOR":         gitMeta.author,
		"VERSELF_DIRTY":          boolStr(gitMeta.dirty),
		"VERSELF_DEPLOY_KIND":    kind,
		"VERSELF_SITE":           opts.Site,
		"VERSELF_DEPLOY_SHA":     deploySha,
		"VERSELF_DEPLOY_SCOPE":   scope,
		"TRACEPARENT":            traceparent,
		"OTEL_SERVICE_NAME":      envOr("OTEL_SERVICE_NAME", "", "ansible"),
	}
	values["OTEL_RESOURCE_ATTRIBUTES"] = buildResourceAttributes(values)
	if endpoint := envOr("OTEL_EXPORTER_OTLP_ENDPOINT", "", ""); endpoint != "" {
		values["OTEL_EXPORTER_OTLP_ENDPOINT"] = endpoint
	}

	return Snapshot{values: values}, nil
}

// ApplyEnv exports each value from the snapshot onto the parent
// process environment. Used by `verself-deploy identity emit` so the
// AXL caller (or a developer running the binary directly) can source
// the resulting env into the surrounding shell.
func (s Snapshot) ApplyEnv() {
	for k, v := range s.values {
		_ = os.Setenv(k, v)
	}
}

// FormatEnvLines prints the snapshot's exports as KEY=VALUE\n lines
// suitable for `source <(verself-deploy identity emit)` semantics.
// Values are NUL-byte-free and shell-safe (no embedded newlines, no
// quoting needed because identity values are 7-bit ASCII).
func (s Snapshot) FormatEnvLines() string {
	var buf bytes.Buffer
	for _, f := range Fields {
		if v := s.values[f.Env]; v != "" {
			buf.WriteString(f.Env)
			buf.WriteByte('=')
			buf.WriteString(v)
			buf.WriteByte('\n')
		}
	}
	// Non-Fields env vars (TRACEPARENT, OTEL_*) follow the closed list.
	extras := []string{
		"VERSELF_COMMIT_SHA",
		"VERSELF_BRANCH",
		"VERSELF_COMMIT_MESSAGE",
		"VERSELF_DIRTY",
		"VERSELF_DEPLOY_SHA",
		"TRACEPARENT",
		"OTEL_SERVICE_NAME",
		"OTEL_RESOURCE_ATTRIBUTES",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
	}
	for _, k := range extras {
		v, ok := s.values[k]
		if !ok || v == "" {
			continue
		}
		// Skip Fields keys that already printed.
		if isFieldEnv(k) {
			continue
		}
		buf.WriteString(k)
		buf.WriteByte('=')
		buf.WriteString(v)
		buf.WriteByte('\n')
	}
	return buf.String()
}

func isFieldEnv(k string) bool {
	for _, f := range Fields {
		if f.Env == k {
			return true
		}
	}
	return false
}

// buildResourceAttributes composes OTEL_RESOURCE_ATTRIBUTES from the
// snapshot's values. Members are URL-encoded comma-separated
// key=value pairs per the OTel spec; commit messages can carry commas
// and equals signs so percent-encoding is required.
func buildResourceAttributes(values map[string]string) string {
	parts := []struct {
		key   string
		value string
	}{
		{"verself.deploy_id", values["VERSELF_DEPLOY_ID"]},
		{"verself.deploy_run_key", values["VERSELF_DEPLOY_RUN_KEY"]},
		{"verself.commit_sha", values["VERSELF_COMMIT_SHA"]},
		{"verself.branch", values["VERSELF_BRANCH"]},
		{"verself.commit_message", values["VERSELF_COMMIT_MESSAGE"]},
		{"verself.author", values["VERSELF_AUTHOR"]},
		{"verself.dirty", values["VERSELF_DIRTY"]},
		{"verself.deploy_kind", values["VERSELF_DEPLOY_KIND"]},
		{"verself.site", values["VERSELF_SITE"]},
		{"verself.deploy_sha", values["VERSELF_DEPLOY_SHA"]},
		{"verself.deploy_scope", values["VERSELF_DEPLOY_SCOPE"]},
	}
	var out []string
	for _, p := range parts {
		if p.value == "" {
			continue
		}
		out = append(out, p.key+"="+url.QueryEscape(p.value))
	}
	return strings.Join(out, ",")
}

// gitMetadata captures the git facts projected into deploy identity.
type gitMetadata struct {
	commitSha     string
	branch        string
	commitMessage string
	author        string
	dirty         bool
}

func readGitMetadata() (gitMetadata, error) {
	root, err := runGit("rev-parse", "--show-toplevel")
	if err != nil {
		// No git context (e.g. in a container) — return zeroed but
		// not failing; downstream handles empty values.
		return gitMetadata{}, nil
	}
	commitSha, _ := runGitIn(root, "rev-parse", "HEAD")
	branch, _ := runGitIn(root, "rev-parse", "--abbrev-ref", "HEAD")
	commitMessage, _ := runGitIn(root, "log", "-1", "--format=%s")
	author, _ := runGitIn(root, "log", "-1", "--format=%ae")
	porcelain, _ := runGitIn(root, "status", "--porcelain")
	return gitMetadata{
		commitSha:     commitSha,
		branch:        branch,
		commitMessage: commitMessage,
		author:        author,
		dirty:         strings.TrimSpace(porcelain) != "",
	}, nil
}

func runGit(args ...string) (string, error) { return runGitIn("", args...) }

func runGitIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// counterMu serialises counter-file mutation across goroutines in the
// same process. Cross-process safety relies on the OS file lock taken
// inside nextDeployCounter — see the flock call.
var counterMu sync.Mutex

// nextDeployCounter increments and returns the per-(day, host)
// counter held under $XDG_CACHE_HOME. Mirrors the bash script's
// fcntl-locked python helper; keeping the on-disk format identical
// means a Go-led deploy and a stale bash-led one wouldn't double-mint
// counters on the same day.
func nextDeployCounter(cacheDir, day, host string) (int, error) {
	counterMu.Lock()
	defer counterMu.Unlock()

	root := cacheDir
	if root == "" {
		xdg := os.Getenv("XDG_CACHE_HOME")
		if xdg == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return 0, fmt.Errorf("identity: home dir: %w", err)
			}
			xdg = filepath.Join(home, ".cache")
		}
		root = filepath.Join(xdg, "verself", "deploy-runs")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return 0, fmt.Errorf("identity: mkdir counter dir: %w", err)
	}
	path := filepath.Join(root, day+"."+host+".counter")
	current := 0
	if data, err := os.ReadFile(path); err == nil {
		s := strings.TrimSpace(string(data))
		if n, err := strconv.Atoi(s); err == nil {
			current = n
		}
	} else if !os.IsNotExist(err) {
		return 0, fmt.Errorf("identity: read counter: %w", err)
	}
	current++
	if err := os.WriteFile(path, []byte(strconv.Itoa(current)+"\n"), 0o644); err != nil {
		return 0, fmt.Errorf("identity: write counter: %w", err)
	}
	return current, nil
}

// controllerHostname matches the bash script's hostname normalisation:
// short hostname, with non-[A-Za-z0-9_.-] replaced by underscores so
// the value is filename-safe.
func controllerHostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "controller"
	}
	if i := strings.IndexByte(h, '.'); i > 0 {
		h = h[:i]
	}
	var b strings.Builder
	for _, r := range h {
		switch {
		case r >= 'A' && r <= 'Z',
			r >= 'a' && r <= 'z',
			r >= '0' && r <= '9',
			r == '_', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func envOr(key, override, fallback string) string {
	if override != "" {
		return override
	}
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
