package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	forgejoUser                         = "forgejo"
	forgejoGroup                        = "forgejo"
	forgejoBinary                       = "local/bin/forgejo"
	sqliteBinary                        = "local/bin/sqlite3"
	appINIPath                          = "/etc/forgejo/app.ini"
	siteDomainPath                      = "/etc/verself/domain"
	workDir                             = "/var/lib/forgejo"
	repositoryDir                       = "/var/lib/forgejo/repositories"
	dataDir                             = "/var/lib/forgejo/data"
	lfsDir                              = "/var/lib/forgejo/data/lfs"
	logDir                              = "/var/lib/forgejo/log"
	credstoreDir                        = "/etc/credstore/forgejo"
	automationTokenPath                 = "/etc/credstore/forgejo/automation-token"
	automationUsernamePath              = "/etc/credstore/forgejo/automation-username"
	automationEmailPath                 = "/etc/credstore/forgejo/automation-email"
	automationUsername                  = "forgejo-automation"
	automationFullName                  = "Forgejo Automation"
	automationTokenName                 = "verself-automation"
	automationTokenScopes               = "all"
	openbaoAddr                         = "https://127.0.0.1:8200"
	openbaoCACertPath                    = "/etc/openbao/tls/cert.pem"
	openbaoRootTokenPath                 = "/etc/credstore/openbao/root-token"
	runtimeSecretKVMount                 = "kv-runtime"
	runtimeSecretNamespace               = "runtime"
	runtimeSecretCreatedAtPath           = "/etc/credstore/openbao/runtime-secrets-runtime-created-at"
	forgejoAutomationRuntimeSecretName   = "source-code-hosting-service.forgejo.automation_token"
	defaultHTTPAddr                      = "127.0.0.1"
)

type config struct {
	SiteDomain string
	ForgejoDomain string
	HTTPAddr string
	HTTPPort string
}

type openbao struct {
	client *http.Client
	token  string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "forgejo-node-runner: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("expected exactly one mode")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	switch args[0] {
	case "prepare":
		return prepare(ctx)
	case "serve":
		return serve()
	default:
		return fmt.Errorf("unknown mode %q", args[0])
	}
}

func prepare(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	uid, gid, err := forgejoIDs()
	if err != nil {
		return err
	}
	if err := ensureDirs(uid, gid); err != nil {
		return err
	}
	if err := writeAppINI(cfg, uid, gid); err != nil {
		return err
	}
	if err := forgejo(ctx, "migrate", "--config", appINIPath, "--work-path", workDir); err != nil {
		return err
	}
	if err := forgejo(ctx, "admin", "regenerate", "keys", "--config", appINIPath, "--work-path", workDir); err != nil {
		return err
	}
	token, err := ensureAutomationToken(ctx, cfg, uid, gid)
	if err != nil {
		return err
	}
	bao, err := openbaoClient()
	if err != nil {
		return err
	}
	createdAt, err := runtimeSecretCreatedAt()
	if err != nil {
		return err
	}
	return bao.writeRuntimeSecret(ctx, forgejoAutomationRuntimeSecretName, token, createdAt)
}

func serve() error {
	return syscall.Exec(forgejoBinary, []string{
		forgejoBinary,
		"web",
		"--config",
		appINIPath,
		"--work-path",
		workDir,
	}, forgejoEnv())
}

func loadConfig() (config, error) {
	siteDomain, err := readRequiredFile(siteDomainPath)
	if err != nil {
		return config{}, err
	}
	httpPort := strings.TrimSpace(os.Getenv("NOMAD_PORT_http"))
	if httpPort == "" {
		httpPort = "3000"
	}
	forgejoDomain := strings.TrimSpace(os.Getenv("FORGEJO_DOMAIN"))
	if forgejoDomain == "" {
		forgejoDomain = "git." + siteDomain
	}
	return config{
		SiteDomain: siteDomain,
		ForgejoDomain: forgejoDomain,
		HTTPAddr: firstNonEmpty(strings.TrimSpace(os.Getenv("FORGEJO_HTTP_ADDR")), defaultHTTPAddr),
		HTTPPort: httpPort,
	}, nil
}

func ensureDirs(uid, gid int) error {
	for _, item := range []struct {
		path string
		mode os.FileMode
	}{
		{"/etc/forgejo", 0o750},
		{workDir, 0o750},
		{repositoryDir, 0o750},
		{dataDir, 0o750},
		{lfsDir, 0o750},
		{logDir, 0o750},
		{credstoreDir, 0o750},
	} {
		if err := os.MkdirAll(item.path, item.mode); err != nil {
			return fmt.Errorf("mkdir %s: %w", item.path, err)
		}
		if err := os.Chown(item.path, uid, gid); err != nil {
			return fmt.Errorf("chown %s: %w", item.path, err)
		}
		if err := os.Chmod(item.path, item.mode); err != nil {
			return fmt.Errorf("chmod %s: %w", item.path, err)
		}
	}
	return nil
}

func writeAppINI(cfg config, uid, gid int) error {
	body := forgejoConfig(cfg)
	if err := writeFile(appINIPath, []byte(body), 0o640, uid, gid); err != nil {
		return err
	}
	return nil
}

func forgejoConfig(cfg config) string {
	return fmt.Sprintf(`; Forgejo configuration managed by Nomad.
; https://forgejo.org/docs/latest/admin/config-cheat-sheet/

APP_NAME = Verself
RUN_MODE = prod
RUN_USER = forgejo
WORK_PATH = %s

[server]
DOMAIN = %s
ROOT_URL = https://%s/
HTTP_PORT = %s
HTTP_ADDR = %s
SSH_DOMAIN = %s
DISABLE_SSH = true
START_SSH_SERVER = false
LFS_START_SERVER = true
OFFLINE_MODE = true

[database]
DB_TYPE = sqlite3
PATH = %s/forgejo.db

[repository]
ROOT = %s
DEFAULT_BRANCH = main

[lfs]
PATH = %s

[log]
MODE = console
LEVEL = info
ROOT_PATH = %s

[security]
INSTALL_LOCK = true

[service]
DISABLE_REGISTRATION = true
ALLOW_ONLY_EXTERNAL_REGISTRATION = false
REQUIRE_SIGNIN_VIEW = true
ENABLE_BASIC_AUTHENTICATION = false
ENABLE_INTERNAL_SIGNIN = false

[oauth2]
ENABLED = false

[openid]
ENABLE_OPENID_SIGNIN = false
ENABLE_OPENID_SIGNUP = false

[actions]
ENABLED = true
`, workDir, cfg.ForgejoDomain, cfg.ForgejoDomain, cfg.HTTPPort, cfg.HTTPAddr, cfg.ForgejoDomain, dataDir, repositoryDir, lfsDir, logDir)
}

func ensureAutomationToken(ctx context.Context, cfg config, uid, gid int) (string, error) {
	email := automationUsername + "@" + cfg.SiteDomain
	exists, err := automationUserExists(ctx)
	if err != nil {
		return "", err
	}
	if !exists {
		if err := forgejo(ctx,
			"admin", "user", "create",
			"--config", appINIPath,
			"--work-path", workDir,
			"--username", automationUsername,
			"--email", email,
			"--fullname", automationFullName,
			"--random-password",
			"--random-password-length", "32",
			"--admin",
		); err != nil {
			return "", err
		}
	}
	if err := ensureAutomationUserAdmin(ctx); err != nil {
		return "", err
	}
	token, err := existingToken()
	if err != nil {
		return "", err
	}
	if token == "" {
		token, err = forgejoOutput(ctx,
			"admin", "user", "generate-access-token",
			"--config", appINIPath,
			"--work-path", workDir,
			"--username", automationUsername,
			"--token-name", automationTokenName,
			"--raw",
			"--scopes", automationTokenScopes,
		)
		if err != nil {
			return "", err
		}
		token = strings.TrimSpace(token)
		if token == "" {
			return "", fmt.Errorf("Forgejo generated empty automation token")
		}
		if err := writeFile(automationTokenPath, []byte(token), 0o600, 0, 0); err != nil {
			return "", err
		}
	}
	if err := writeFile(automationUsernamePath, []byte(automationUsername), 0o644, 0, 0); err != nil {
		return "", err
	}
	if err := writeFile(automationEmailPath, []byte(email), 0o644, 0, 0); err != nil {
		return "", err
	}
	if err := os.Chown(credstoreDir, uid, gid); err != nil {
		return "", fmt.Errorf("chown %s: %w", credstoreDir, err)
	}
	return token, nil
}

func automationUserExists(ctx context.Context) (bool, error) {
	out, err := forgejoOutput(ctx, "admin", "user", "list", "--config", appINIPath, "--work-path", workDir)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == automationUsername {
			return true, nil
		}
	}
	return false, nil
}

func ensureAutomationUserAdmin(ctx context.Context) error {
	query := "SELECT is_admin FROM user WHERE lower_name = '" + automationUsername + "';"
	out, err := sqlite(ctx, query)
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) == "1" {
		return nil
	}
	return sqliteExec(ctx, "UPDATE user SET is_admin = 1 WHERE lower_name = '"+automationUsername+"';")
}

func existingToken() (string, error) {
	body, err := os.ReadFile(automationTokenPath)
	if err == nil {
		return strings.TrimSpace(string(body)), nil
	}
	if os.IsNotExist(err) {
		return "", nil
	}
	return "", fmt.Errorf("read %s: %w", automationTokenPath, err)
}

func forgejo(ctx context.Context, args ...string) error {
	_, err := forgejoOutput(ctx, args...)
	return err
}

func forgejoOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, forgejoBinary, args...)
	cmd.Env = forgejoEnv()
	if err := runCommandAsForgejo(cmd); err != nil {
		return "", err
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("forgejo %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	if text := strings.TrimSpace(stderr.String()); text != "" {
		fmt.Fprintln(os.Stderr, text)
	}
	return stdout.String(), nil
}

func sqlite(ctx context.Context, query string) (string, error) {
	cmd := exec.CommandContext(ctx, sqliteBinary, filepath.Join(dataDir, "forgejo.db"), query)
	if err := runCommandAsForgejo(cmd); err != nil {
		return "", err
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("sqlite query: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func sqliteExec(ctx context.Context, query string) error {
	_, err := sqlite(ctx, query)
	return err
}

func openbaoClient() (openbao, error) {
	tokenBody, err := os.ReadFile(openbaoRootTokenPath)
	if err != nil {
		return openbao{}, fmt.Errorf("read OpenBao root token: %w", err)
	}
	certBody, err := os.ReadFile(openbaoCACertPath)
	if err != nil {
		return openbao{}, fmt.Errorf("read OpenBao CA cert: %w", err)
	}
	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(certBody); !ok {
		return openbao{}, fmt.Errorf("parse OpenBao CA cert %s", openbaoCACertPath)
	}
	return openbao{
		token: strings.TrimSpace(string(tokenBody)),
		client: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
			},
		},
	}, nil
}

func (b openbao) writeRuntimeSecret(ctx context.Context, name, value, createdAt string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	path := runtimeSecretKVMount + "/data/secret/org/" + url.PathEscape(name)
	payload := map[string]any{
		"data": map[string]string{
			"org_id":      runtimeSecretNamespace,
			"kind":        "secret",
			"name":        name,
			"scope_level": "org",
			"source_id":   "",
			"env_id":      "",
			"branch":      "",
			"value":       strings.TrimSpace(value),
			"created_at":  createdAt,
			"updated_at":  now,
		},
	}
	if _, err := b.request(ctx, http.MethodPost, path, payload, http.StatusOK, http.StatusNoContent); err != nil {
		return fmt.Errorf("write OpenBao runtime secret %s: %w", name, err)
	}
	return nil
}

func (b openbao) request(ctx context.Context, method, path string, body any, expected ...int) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal OpenBao request %s %s: %w", method, path, err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, openbaoAddr+"/v1/"+strings.TrimLeft(path, "/"), reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Vault-Token", b.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OpenBao %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read OpenBao response %s %s: %w", method, path, err)
	}
	for _, status := range expected {
		if resp.StatusCode == status {
			return raw, nil
		}
	}
	return nil, fmt.Errorf("OpenBao %s %s status %d", method, path, resp.StatusCode)
}

func runtimeSecretCreatedAt() (string, error) {
	body, err := os.ReadFile(runtimeSecretCreatedAtPath)
	if err == nil {
		value := strings.TrimSpace(string(body))
		if value == "" {
			return "", fmt.Errorf("%s is empty", runtimeSecretCreatedAtPath)
		}
		return value, nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("read %s: %w", runtimeSecretCreatedAtPath, err)
	}
	value := time.Now().UTC().Format(time.RFC3339)
	if err := writeFile(runtimeSecretCreatedAtPath, []byte(value+"\n"), 0o640, 0, 0); err != nil {
		return "", err
	}
	return value, nil
}

func forgejoIDs() (int, int, error) {
	u, err := user.Lookup(forgejoUser)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup user %s: %w", forgejoUser, err)
	}
	g, err := user.LookupGroup(forgejoGroup)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup group %s: %w", forgejoGroup, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse uid for %s: %w", forgejoUser, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse gid for %s: %w", forgejoGroup, err)
	}
	return uid, gid, nil
}

func runCommandAsForgejo(cmd *exec.Cmd) error {
	uid, gid, err := forgejoIDs()
	if err != nil {
		return err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(uid),
			Gid: uint32(gid),
		},
	}
	return nil
}

func forgejoEnv() []string {
	localBin := "local/bin"
	if cwd, err := os.Getwd(); err == nil {
		localBin = filepath.Join(cwd, "local/bin")
	}
	return withEnv(os.Environ(), map[string]string{
		"HOME":              workDir,
		"FORGEJO_WORK_DIR":  workDir,
		"PATH":              localBin + ":/usr/bin:/bin",
	})
}

func withEnv(env []string, values map[string]string) []string {
	out := make([]string, 0, len(env)+len(values))
	seen := make(map[string]struct{}, len(values))
	for _, item := range env {
		key, _, ok := strings.Cut(item, "=")
		if ok {
			if _, replace := values[key]; replace {
				if _, done := seen[key]; !done {
					out = append(out, key+"="+values[key])
					seen[key] = struct{}{}
				}
				continue
			}
		}
		out = append(out, item)
	}
	for key, value := range values {
		if _, done := seen[key]; !done {
			out = append(out, key+"="+value)
		}
	}
	return out
}

func readRequiredFile(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	value := strings.TrimSpace(string(body))
	if value == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return value, nil
}

func writeFile(path string, body []byte, mode os.FileMode, uid, gid int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, mode); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Chown(tmp, uid, gid); err != nil {
		return fmt.Errorf("chown %s: %w", tmp, err)
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("install %s: %w", path, err)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
