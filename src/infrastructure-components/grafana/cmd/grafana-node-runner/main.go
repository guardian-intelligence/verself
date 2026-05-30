package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	grafanaUser  = "grafana"
	grafanaGroup = "grafana"
	spireGroup   = "spire_workload"

	configDir       = "/etc/grafana"
	credstoreDir    = "/etc/credstore/grafana"
	dataDir         = "/var/lib/grafana"
	logDir          = "/var/log/grafana"
	provisioningDir = "/etc/grafana/provisioning"
	datasourceDir   = "/etc/grafana/provisioning/datasources"
	dashboardDir    = "/etc/grafana/provisioning/dashboards"
	spiffeDir       = "/var/lib/grafana/clickhouse-spiffe"
	dashboardJSON   = "/etc/grafana/dashboards"

	adminPasswordPath = "/etc/credstore/grafana/admin-password"
	secretKeyPath     = "/etc/credstore/grafana/secret-key"
	clickHouseCAPath  = "/etc/credstore/grafana/clickhouse-server-ca.pem"
	datasourcePath    = "/etc/grafana/provisioning/datasources/clickhouse.yml"
	grafanaConfigPath = "/etc/grafana/grafana.ini"
	helperConfigPath  = "/etc/grafana/clickhouse-spiffe-helper.conf"

	siteDomainPath = "/etc/verself/domain"
)

func main() {
	if filepath.Base(os.Args[0]) == "refresh-grafana-clickhouse-datasource" {
		if err := refreshClickHouseDatasource(context.Background()); err != nil {
			fatal(err)
		}
		return
	}
	if len(os.Args) < 2 {
		fatal(errors.New("subcommand is required"))
	}
	ctx := context.Background()
	var err error
	switch os.Args[1] {
	case "prepare":
		err = prepare(ctx)
	case "serve":
		err = serve(ctx)
	case "reset-admin-password":
		err = resetAdminPassword(ctx)
	case "refresh-clickhouse-datasource":
		err = refreshClickHouseDatasource(ctx)
	default:
		err = fmt.Errorf("unknown subcommand %q", os.Args[1])
	}
	if err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "grafana-node-runner: %v\n", err)
	os.Exit(1)
}

func prepare(ctx context.Context) error {
	runtime := strings.TrimSpace(os.Getenv("VERSELF_GRAFANA_RUNTIME"))
	if runtime == "" {
		return errors.New("VERSELF_GRAFANA_RUNTIME is required")
	}
	if err := ensureGroup(ctx, grafanaGroup); err != nil {
		return err
	}
	if err := ensureUser(ctx, grafanaUser, grafanaGroup, dataDir); err != nil {
		return err
	}
	if err := runCommand(ctx, "usermod", "-a", "-G", spireGroup, grafanaUser); err != nil {
		return fmt.Errorf("add grafana to %s: %w", spireGroup, err)
	}
	ids, err := lookupIDs()
	if err != nil {
		return err
	}
	for _, dir := range []dirSpec{
		{Path: configDir, UID: 0, GID: ids.grafanaGID, Mode: 0750},
		{Path: credstoreDir, UID: 0, GID: ids.grafanaGID, Mode: 0750},
		{Path: dataDir, UID: ids.grafanaUID, GID: ids.grafanaGID, Mode: 0750},
		{Path: logDir, UID: ids.grafanaUID, GID: ids.grafanaGID, Mode: 0750},
		{Path: provisioningDir, UID: 0, GID: ids.grafanaGID, Mode: 0750},
		{Path: datasourceDir, UID: 0, GID: ids.grafanaGID, Mode: 0750},
		{Path: dashboardDir, UID: 0, GID: ids.grafanaGID, Mode: 0750},
		{Path: dashboardJSON, UID: 0, GID: ids.grafanaGID, Mode: 0750},
		{Path: spiffeDir, UID: ids.grafanaUID, GID: ids.grafanaGID, Mode: 0700},
	} {
		if err := ensureDir(dir); err != nil {
			return err
		}
	}
	if err := ensureSecretFile(adminPasswordPath, 32, ids); err != nil {
		return err
	}
	if err := ensureSecretFile(secretKeyPath, 64, ids); err != nil {
		return err
	}
	if err := copyFile("/etc/clickhouse-server/tls/server-ca.pem", clickHouseCAPath, 0640, 0, ids.grafanaGID); err != nil {
		return err
	}
	siteDomain, err := readTrimmed(siteDomainPath)
	if err != nil {
		return err
	}
	if err := writeFile(grafanaConfigPath, []byte(grafanaINI(siteDomain)), 0640, 0, ids.grafanaGID); err != nil {
		return err
	}
	if err := writeFile(helperConfigPath, []byte(helperConfig(runtime)), 0640, 0, ids.grafanaGID); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(dashboardDir, "verself.yml"), []byte(dashboardProvider()), 0640, 0, ids.grafanaGID); err != nil {
		return err
	}
	return refreshClickHouseDatasource(ctx)
}

type dirSpec struct {
	Path string
	UID  int
	GID  int
	Mode os.FileMode
}

type ids struct {
	grafanaUID int
	grafanaGID int
	rootGID    int
}

func ensureGroup(ctx context.Context, name string) error {
	if err := runCommand(ctx, "getent", "group", name); err == nil {
		return nil
	}
	return runCommand(ctx, "groupadd", "--system", name)
}

func ensureUser(ctx context.Context, name, group, home string) error {
	if err := runCommand(ctx, "id", "-u", name); err == nil {
		return nil
	}
	return runCommand(ctx, "useradd", "--system", "--gid", group, "--home-dir", home, "--shell", "/usr/sbin/nologin", "--no-create-home", name)
}

func lookupIDs() (ids, error) {
	uid, err := lookupUserID(grafanaUser)
	if err != nil {
		return ids{}, err
	}
	gid, err := lookupGroupID(grafanaGroup)
	if err != nil {
		return ids{}, err
	}
	rootGID, err := lookupGroupID("root")
	if err != nil {
		return ids{}, err
	}
	return ids{grafanaUID: uid, grafanaGID: gid, rootGID: rootGID}, nil
}

func lookupUserID(name string) (int, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, fmt.Errorf("lookup user %s: %w", name, err)
	}
	id, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, fmt.Errorf("parse uid for %s: %w", name, err)
	}
	return id, nil
}

func lookupGroupID(name string) (int, error) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, fmt.Errorf("lookup group %s: %w", name, err)
	}
	id, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, fmt.Errorf("parse gid for %s: %w", name, err)
	}
	return id, nil
}

func ensureDir(dir dirSpec) error {
	if err := os.MkdirAll(dir.Path, dir.Mode); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir.Path, err)
	}
	if err := os.Chown(dir.Path, dir.UID, dir.GID); err != nil {
		return fmt.Errorf("chown %s: %w", dir.Path, err)
	}
	if err := os.Chmod(dir.Path, dir.Mode); err != nil {
		return fmt.Errorf("chmod %s: %w", dir.Path, err)
	}
	return nil
}

func ensureSecretFile(path string, length int, ids ids) error {
	if _, err := os.Stat(path); err == nil {
		if err := os.Chown(path, 0, ids.grafanaGID); err != nil {
			return fmt.Errorf("chown %s: %w", path, err)
		}
		if err := os.Chmod(path, 0640); err != nil {
			return fmt.Errorf("chmod %s: %w", path, err)
		}
		return nil
	}
	value, err := randomAlphaNum(length)
	if err != nil {
		return err
	}
	return writeFile(path, []byte(value), 0640, 0, ids.grafanaGID)
}

func randomAlphaNum(length int) (string, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	out := make([]byte, length)
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	for i, b := range buf {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out), nil
}

func serve(ctx context.Context) error {
	runtime := strings.TrimSpace(os.Getenv("VERSELF_GRAFANA_RUNTIME"))
	if runtime == "" {
		return errors.New("VERSELF_GRAFANA_RUNTIME is required")
	}
	env, err := grafanaEnv(runtime)
	if err != nil {
		return err
	}
	return execReplace(ctx, filepath.Join(runtime, "grafana", "bin", "grafana"), env, "server", "--homepath", filepath.Join(runtime, "grafana"), "--config", grafanaConfigPath)
}

func resetAdminPassword(ctx context.Context) error {
	runtime := strings.TrimSpace(os.Getenv("VERSELF_GRAFANA_RUNTIME"))
	if runtime == "" {
		return errors.New("VERSELF_GRAFANA_RUNTIME is required")
	}
	env, err := grafanaEnv(runtime)
	if err != nil {
		return err
	}
	adminPassword, err := readTrimmed(adminPasswordPath)
	if err != nil {
		return err
	}
	var lastErr error
	for range 30 {
		lastErr = runCommandEnv(ctx, env, filepath.Join(runtime, "grafana", "bin", "grafana"), "cli", "--homepath", filepath.Join(runtime, "grafana"), "--config", grafanaConfigPath, "admin", "reset-admin-password", adminPassword)
		if lastErr == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("reset Grafana admin password: %w", lastErr)
}

func grafanaEnv(runtime string) ([]string, error) {
	adminPassword, err := readTrimmed(adminPasswordPath)
	if err != nil {
		return nil, err
	}
	secretKey, err := readTrimmed(secretKeyPath)
	if err != nil {
		return nil, err
	}
	env := append(os.Environ(),
		"GF_SECURITY_ADMIN_PASSWORD="+adminPassword,
		"GF_SECURITY_SECRET_KEY="+secretKey,
		"GF_PATHS_PLUGINS="+filepath.Join(runtime, "plugins"),
		"HOME="+dataDir,
	)
	return env, nil
}

func refreshClickHouseDatasource(ctx context.Context) error {
	ids, err := lookupIDs()
	if err != nil {
		return err
	}
	ca, err := readRequired(clickHouseCAPath)
	if err != nil {
		return err
	}
	cert, err := readRequired(filepath.Join(spiffeDir, "svid.pem"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	key, err := readRequired(filepath.Join(spiffeDir, "svid_key.pem"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	body := datasourceYAML(string(ca), string(cert), string(key))
	if err := writeFile(datasourcePath, []byte(body), 0640, 0, ids.grafanaGID); err != nil {
		return err
	}
	return reloadDatasource(ctx)
}

func readRequired(path string) ([]byte, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, fmt.Errorf("%s is empty", path)
	}
	return body, nil
}

func reloadDatasource(ctx context.Context) error {
	adminPassword, err := readTrimmed(adminPasswordPath)
	if err != nil {
		return err
	}
	client := http.Client{Timeout: 2 * time.Second}
	healthReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:4300/api/health", nil)
	if err != nil {
		return err
	}
	healthResp, err := client.Do(healthReq)
	if err != nil {
		return nil
	}
	_, _ = io.Copy(io.Discard, healthResp.Body)
	_ = healthResp.Body.Close()
	if healthResp.StatusCode < 200 || healthResp.StatusCode >= 300 {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:4300/api/admin/provisioning/datasources/reload", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth("admin", adminPassword)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("reload Grafana datasource: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("reload Grafana datasource: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func execReplace(ctx context.Context, name string, env []string, args ...string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("look up %s: %w", name, err)
	}
	_ = ctx
	return syscall.Exec(path, append([]string{name}, args...), env)
}

func runCommand(ctx context.Context, name string, args ...string) error {
	return runCommandEnv(ctx, os.Environ(), name, args...)
}

func runCommandEnv(ctx context.Context, env []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, msg)
		}
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func readTrimmed(path string) (string, error) {
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
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
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

func copyFile(src, dst string, mode os.FileMode, uid, gid int) error {
	body, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	return writeFile(dst, body, mode, uid, gid)
}

func grafanaINI(siteDomain string) string {
	grafanaDomain := "dashboard." + siteDomain
	return fmt.Sprintf(`[paths]
data = /var/lib/grafana
logs = /var/log/grafana
provisioning = /etc/grafana/provisioning

[server]
protocol = http
http_addr = 127.0.0.1
http_port = 4300
domain = %s
root_url = https://%s/
serve_from_sub_path = false

[database]
type = sqlite3
path = grafana.db

[security]
admin_user = admin
admin_password = $__env{GF_SECURITY_ADMIN_PASSWORD}
secret_key = $__env{GF_SECURITY_SECRET_KEY}
cookie_secure = true
cookie_samesite = strict
disable_gravatar = true
allow_embedding = false

[users]
allow_sign_up = false
auto_assign_org = true
auto_assign_org_role = Viewer

[analytics]
reporting_enabled = false
check_for_updates = false
check_for_plugin_updates = false
feedback_links_enabled = false

[plugins]
plugin_admin_enabled = false
plugin_admin_external_manage_enabled = false
plugin_catalog_url =
preinstall_disabled = true
preinstall_auto_update = false

[auth]
disable_login_form = false
oauth_auto_login = false
signout_redirect_url = https://%s/.pomerium/sign_out

[auth.jwt]
enabled = true
header_name = X-Pomerium-Jwt-Assertion
email_claim = email
username_claim = email
jwk_set_url = https://%s/.well-known/pomerium/jwks.json
cache_ttl = 60m
auto_sign_up = true
url_login = false

[auth.anonymous]
enabled = false

[log]
mode = console
level = info
`, grafanaDomain, grafanaDomain, grafanaDomain, grafanaDomain)
}

func helperConfig(runtime string) string {
	return fmt.Sprintf(`agent_address = "/run/spire-agent/sockets/agent.sock"
cert_dir = "/var/lib/grafana/clickhouse-spiffe"
daemon_mode = true
svid_file_name = "svid.pem"
svid_key_file_name = "svid_key.pem"
svid_bundle_file_name = "bundle.pem"
cert_file_mode = 0640
key_file_mode = 0600
cmd = %q
`, filepath.Join(runtime, "bin", "refresh-grafana-clickhouse-datasource"))
}

func dashboardProvider() string {
	return `apiVersion: 1

providers:
  - name: verself
    orgId: 1
    folder: Verself
    type: file
    disableDeletion: false
    allowUiUpdates: false
    updateIntervalSeconds: 30
    options:
      path: /etc/grafana/dashboards
`
}

func datasourceYAML(ca, cert, key string) string {
	return fmt.Sprintf(`apiVersion: 1

datasources:
  - name: Verself ClickHouse
    uid: verself-clickhouse
    type: grafana-clickhouse-datasource
    access: proxy
    isDefault: true
    editable: false
    jsonData:
      defaultDatabase: default
      host: 127.0.0.1
      port: 9440
      protocol: native
      secure: true
      username: grafana_observer
      tlsAuth: true
      tlsAuthWithCACert: true
      tlsSkipVerify: false
      logs:
        defaultDatabase: default
        defaultTable: otel_logs
        otelEnabled: true
        otelVersion: latest
        timeColumn: Timestamp
        levelColumn: SeverityText
        messageColumn: Body
      traces:
        defaultDatabase: default
        defaultTable: otel_traces
        otelEnabled: true
        otelVersion: latest
        durationUnit: nanoseconds
        traceIdColumn: TraceId
        spanIdColumn: SpanId
        parentSpanIdColumn: ParentSpanId
        operationNameColumn: SpanName
        serviceNameColumn: ServiceName
        startTimeColumn: Timestamp
        durationColumn: Duration
    secureJsonData:
      tlsCACert: |
%s
      tlsClientCert: |
%s
      tlsClientKey: |
%s
`, indent(ca), indent(cert), indent(key))
}

func indent(value string) string {
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	for i, line := range lines {
		lines[i] = "        " + line
	}
	return strings.Join(lines, "\n")
}
