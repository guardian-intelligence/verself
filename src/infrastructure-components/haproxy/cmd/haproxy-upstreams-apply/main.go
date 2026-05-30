package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	verselfotel "github.com/verself/observability/otel"
)

type stringList []string

const (
	maxUint32AsInt64 = int64(1<<32 - 1)

	haproxyGroup = "haproxy"
	haproxyUser  = "haproxy"

	haproxyConfigDir       = "/etc/haproxy"
	haproxyCertDir         = "/etc/haproxy/certs"
	haproxyMapDir          = "/etc/haproxy/maps"
	haproxyConfigPath      = "/etc/haproxy/haproxy.cfg"
	haproxyPublicHostsPath = "/etc/haproxy/maps/public-hosts.map"
	haproxyDataDir         = "/var/lib/haproxy"
	haproxyLogDir          = "/var/log/haproxy"
	haproxySystemdUnitPath = "/etc/systemd/system/haproxy.service"
	haproxyProfileBin      = "/opt/verself/profile/bin/haproxy"
	haproxyProfileLibDir   = "/opt/verself/profile/lib/haproxy"
	siteDomainPath         = "/etc/verself/domain"
)

func (s *stringList) String() string {
	return strings.Join(*s, ",")
}

func (s *stringList) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("empty value")
	}
	*s = append(*s, value)
	return nil
}

type config struct {
	source               string
	dest                 string
	group                string
	haproxyUser          string
	haproxyBin           string
	haproxyConfigs       stringList
	haproxyLDLibraryPath string
	reloadUnit           string
	daemon               bool
	skipHostState        bool
}

type fileSwap struct {
	path    string
	content []byte
	old     []byte
	existed bool
	group   string
	mode    fs.FileMode
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "haproxy-upstreams-apply: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg := config{}
	fs := flag.NewFlagSet("haproxy-upstreams-apply", flag.ContinueOnError)
	fs.StringVar(&cfg.source, "source", "", "Rendered Nomad upstream config path.")
	fs.StringVar(&cfg.dest, "dest", "/etc/haproxy/nomad-upstreams.cfg", "Installed HAProxy upstream config path.")
	fs.StringVar(&cfg.group, "group", "haproxy", "Group owner for installed config.")
	fs.StringVar(&cfg.haproxyUser, "haproxy-user", "haproxy", "User used for HAProxy config validation.")
	fs.StringVar(&cfg.haproxyBin, "haproxy-bin", "haproxy", "Path to the HAProxy binary.")
	fs.Var(&cfg.haproxyConfigs, "haproxy-config", "HAProxy config to validate; repeat in HAProxy load order.")
	fs.StringVar(&cfg.haproxyLDLibraryPath, "haproxy-ld-library-path", "/opt/aws-lc/lib/x86_64-linux-gnu", "LD_LIBRARY_PATH used when invoking HAProxy.")
	fs.StringVar(&cfg.reloadUnit, "reload-unit", "haproxy.service", "systemd unit to reload after a valid upstream swap.")
	fs.BoolVar(&cfg.daemon, "daemon", false, "Apply once, then stay alive until SIGINT or SIGTERM.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if cfg.source == "" {
		return errors.New("--source is required")
	}
	if cfg.dest == "" {
		return errors.New("--dest is required")
	}
	if len(cfg.haproxyConfigs) == 0 {
		cfg.haproxyConfigs = append(cfg.haproxyConfigs, "/etc/haproxy/haproxy.cfg", cfg.dest)
	}
	shutdown, err := initTelemetry()
	if err != nil {
		// HAProxy upstream convergence is the availability path; telemetry
		// failure is reported but must not prevent a valid edge reload.
		fmt.Fprintf(os.Stderr, "haproxy-upstreams-apply: telemetry disabled: %v\n", err)
	} else {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := shutdown(shutdownCtx); err != nil {
				fmt.Fprintf(os.Stderr, "haproxy-upstreams-apply: telemetry shutdown: %v\n", err)
			}
		}()
	}
	changed, err := applyOnceWithTelemetry(context.Background(), cfg)
	if err != nil {
		return err
	}
	fmt.Printf("haproxy-upstreams-apply: changed=%t\n", changed)
	if !cfg.daemon {
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	return nil
}

func initTelemetry() (func(context.Context) error, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	shutdown, _, err := verselfotel.Init(ctx, verselfotel.Config{
		ServiceName: "haproxy-upstreams-apply",
	})
	if err != nil {
		return nil, err
	}
	return shutdown, nil
}

func applyOnceWithTelemetry(ctx context.Context, cfg config) (bool, error) {
	tracer := otel.Tracer("github.com/verself/infrastructure-components/haproxy/cmd/haproxy-upstreams-apply")
	_, span := tracer.Start(ctx, "haproxy_upstreams.apply",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("haproxy.upstreams.source", cfg.source),
			attribute.String("haproxy.upstreams.dest", cfg.dest),
			attribute.String("haproxy.reload_unit", cfg.reloadUnit),
			attribute.Bool("haproxy.upstreams.daemon", cfg.daemon),
		),
	)
	defer span.End()
	changed, err := applyOnce(cfg)
	span.SetAttributes(attribute.Bool("haproxy.upstreams.changed", changed))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return false, err
	}
	span.SetStatus(codes.Ok, "")
	return changed, nil
}

func applyOnce(cfg config) (bool, error) {
	if !cfg.skipHostState {
		if err := ensureHostState(cfg); err != nil {
			return false, err
		}
	}
	content, err := os.ReadFile(cfg.source)
	if err != nil {
		return false, fmt.Errorf("read source %s: %w", cfg.source, err)
	}
	if !bytes.Contains(content, []byte("\nbackend ")) {
		return false, fmt.Errorf("%s does not look like an HAProxy backend config", cfg.source)
	}
	swaps := []fileSwap{}
	if swap, changed, err := plannedSwap(cfg.dest, content, cfg.group, 0o640); err != nil {
		return false, err
	} else if changed {
		swaps = append(swaps, swap)
	}
	if len(swaps) == 0 {
		return false, nil
	}
	if err := applySwaps(swaps); err != nil {
		return false, err
	}
	if err := validateHAProxy(cfg); err != nil {
		if restoreErr := restoreSwaps(swaps); restoreErr != nil {
			return false, fmt.Errorf("haproxy validation failed after writing config set: %w; restore failed: %v", err, restoreErr)
		}
		return false, fmt.Errorf("haproxy validation failed after writing config set; previous files restored: %w", err)
	}
	if cfg.reloadUnit != "" {
		if err := reloadOrStartUnit(cfg.reloadUnit); err != nil {
			return false, err
		}
	}
	return true, nil
}

type hostIDs struct {
	haproxyUID int
	haproxyGID int
	admGID     int
	rootGID    int
}

type dirSpec struct {
	path string
	uid  int
	gid  int
	mode fs.FileMode
}

func ensureHostState(cfg config) error {
	ctx := context.Background()
	if err := ensureGroup(ctx, haproxyGroup); err != nil {
		return err
	}
	if err := ensureUser(ctx, haproxyUser, haproxyGroup, haproxyDataDir); err != nil {
		return err
	}
	_ = runCommand(ctx, "usermod", "-a", "-G", "adm", haproxyUser)
	ids, err := lookupHostIDs()
	if err != nil {
		return err
	}
	for _, dir := range []dirSpec{
		{path: haproxyConfigDir, uid: 0, gid: ids.haproxyGID, mode: 0750},
		{path: haproxyCertDir, uid: 0, gid: ids.haproxyGID, mode: 0750},
		{path: haproxyMapDir, uid: 0, gid: ids.haproxyGID, mode: 0750},
		{path: haproxyDataDir, uid: ids.haproxyUID, gid: ids.haproxyGID, mode: 0750},
		{path: haproxyLogDir, uid: ids.haproxyUID, gid: ids.admGID, mode: 02750},
		{path: filepath.Dir(haproxyProfileBin), uid: 0, gid: ids.rootGID, mode: 0755},
		{path: haproxyProfileLibDir, uid: 0, gid: ids.rootGID, mode: 0755},
		{path: "/var/www/verself/.well-known", uid: 0, gid: ids.rootGID, mode: 0755},
	} {
		if err := ensureDir(dir); err != nil {
			return err
		}
	}
	if err := copyExecutable(cfg.haproxyBin, haproxyProfileBin); err != nil {
		return err
	}
	if err := copyLibraries(filepath.Join(filepath.Dir(filepath.Dir(cfg.haproxyBin)), "lib"), haproxyProfileLibDir); err != nil {
		return err
	}
	siteDomain, err := readSiteDomain()
	if err != nil {
		return err
	}
	if err := ensurePlaceholderCertificate(siteDomain, ids); err != nil {
		return err
	}
	if err := atomicWrite(haproxyConfigPath, []byte(baseHAProxyConfig()), haproxyGroup, 0640); err != nil {
		return err
	}
	if err := atomicWrite(haproxyPublicHostsPath, []byte(publicHostsMap(siteDomain)), haproxyGroup, 0640); err != nil {
		return err
	}
	if err := atomicWrite("/var/www/verself/.well-known/verself", []byte(discoveryManifest(siteDomain)), "", 0644); err != nil {
		return err
	}
	if err := os.WriteFile(haproxySystemdUnitPath, []byte(haproxySystemdUnit()), 0644); err != nil {
		return fmt.Errorf("write %s: %w", haproxySystemdUnitPath, err)
	}
	return systemctl("daemon-reload")
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

func lookupHostIDs() (hostIDs, error) {
	uid, err := userID(haproxyUser)
	if err != nil {
		return hostIDs{}, err
	}
	gid, err := groupID(haproxyGroup)
	if err != nil {
		return hostIDs{}, err
	}
	admGID, err := groupID("adm")
	if err != nil {
		return hostIDs{}, err
	}
	rootGID, err := groupID("root")
	if err != nil {
		return hostIDs{}, err
	}
	return hostIDs{haproxyUID: uid, haproxyGID: gid, admGID: admGID, rootGID: rootGID}, nil
}

func userID(name string) (int, error) {
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

func ensureDir(dir dirSpec) error {
	if err := os.MkdirAll(dir.path, dir.mode); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir.path, err)
	}
	if err := os.Chown(dir.path, dir.uid, dir.gid); err != nil {
		return fmt.Errorf("chown %s: %w", dir.path, err)
	}
	if err := os.Chmod(dir.path, dir.mode); err != nil {
		return fmt.Errorf("chmod %s: %w", dir.path, err)
	}
	return nil
}

func copyExecutable(src, dst string) error {
	return copyFileMode(src, dst, 0755)
}

func copyLibrary(src, dst string) error {
	return copyFileMode(src, dst, 0644)
}

func copyLibraries(srcDir, dstDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("read %s: %w", srcDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if err := copyLibrary(filepath.Join(srcDir, entry.Name()), filepath.Join(dstDir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyFileMode(src, dst string, mode fs.FileMode) error {
	body, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, body, mode); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Chown(tmp, 0, 0); err != nil {
		return fmt.Errorf("chown %s: %w", tmp, err)
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("install %s: %w", dst, err)
	}
	return nil
}

func readSiteDomain() (string, error) {
	body, err := os.ReadFile(siteDomainPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", siteDomainPath, err)
	}
	domain := strings.TrimSpace(string(body))
	if domain == "" {
		return "", fmt.Errorf("%s is empty", siteDomainPath)
	}
	return domain, nil
}

func ensurePlaceholderCertificate(siteDomain string, ids hostIDs) error {
	pemPath := filepath.Join(haproxyCertDir, "default.pem")
	if info, err := os.Stat(pemPath); err == nil && info.Size() > 0 {
		if err := os.Chown(pemPath, 0, ids.haproxyGID); err != nil {
			return fmt.Errorf("chown %s: %w", pemPath, err)
		}
		return os.Chmod(pemPath, 0640)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate HAProxy placeholder key: %w", err)
	}
	now := time.Now().UTC()
	tpl := x509.Certificate{
		SerialNumber: serialNumber(),
		Subject:      pkix.Name{CommonName: siteDomain},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{siteDomain, "*." + siteDomain, "*.api." + siteDomain, "localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tpl, &tpl, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("create HAProxy placeholder certificate: %w", err)
	}
	var pemBody bytes.Buffer
	if err := pem.Encode(&pemBody, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}); err != nil {
		return err
	}
	if err := pem.Encode(&pemBody, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		return err
	}
	return atomicWrite(pemPath, pemBody.Bytes(), haproxyGroup, 0640)
}

func serialNumber() *big.Int {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		panic(err)
	}
	return n
}

func baseHAProxyConfig() string {
	return `global
  log stdout format raw local0
  stats socket /run/haproxy/admin.sock mode 660 level admin expose-fd listeners
  crt-base /etc/haproxy/certs
  maxconn 20000
  hard-stop-after 5s
  ssl-default-bind-options ssl-min-ver TLSv1.3 no-tls-tickets
  ssl-default-bind-ciphersuites TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256

defaults
  mode http
  log global
  option dontlognull
  option http-keep-alive
  timeout connect 1s
  timeout client 30s
  timeout server 30s
  timeout tunnel 4h
  timeout http-request 5s
  timeout http-keep-alive 10s
  unique-id-format %[uuid()]
  unique-id-header X-Request-ID

frontend fe_http
  guid fe_http
  bind 0.0.0.0:80
  http-request redirect scheme https code 308

frontend fe_https
  guid fe_https
  bind 0.0.0.0:443 ssl crt /etc/haproxy/certs alpn h2,http/1.1
  http-request set-var(txn.host) req.hdr(host),lower,field(1,:)
  http-request set-header X-Real-IP %[src]
  http-request set-header X-Forwarded-For %[src]
  http-request set-header X-Forwarded-Host %[req.hdr(host)]
  http-request set-header X-Forwarded-Proto https
  http-response set-header Strict-Transport-Security "max-age=31536000"

  acl method_post method POST
  acl auth_device_authorization_path path -i /oauth/v2/device_authorization
  acl auth_path path -i /api/v1/auth/password-login /api/v1/auth/password-reset /api/v1/auth/password-reset/complete /api/v1/auth/password-reset/consume /api/v1/auth/callback /api/v1/auth/session /api/v1/auth/sync-session /api/v1/auth/organization /api/v1/auth/resource-token /api/v1/auth/logout /api/v1/auth/accounts /api/v1/auth/sessions /api/v1/auth/invites/accept
  acl auth_accounts_path path_beg /api/v1/auth/accounts/
  acl auth_sessions_path path_beg /api/v1/auth/sessions/
  acl auth_device_logins_path path_beg /api/v1/auth/device-logins/
  acl auth_idp_path path_beg /api/v1/auth/idp/
  acl verself_discovery path -i /.well-known/verself
  acl zitadel_oidc_path path -i /.well-known/openid-configuration /.well-known/webfinger /robots.txt
  acl zitadel_oidc_path path_beg /oauth/ /oauth/v2/ /oidc/ /oidc/v1/ /saml/ /ui/login /ui/console /idps/
  acl jmap_session path -i /jmap/session
  acl stripe_webhook path -i /webhooks/stripe
  acl source_forgejo_webhook path -i /webhooks/forgejo

  http-request return status 200 content-type application/json file /var/www/verself/.well-known/verself hdr Cache-Control "public, max-age=300" if verself_discovery
  use_backend be_route_product_apex_iam_service_public_api if method_post auth_device_authorization_path
  use_backend be_route_product_apex_iam_service_public_api if auth_path
  use_backend be_route_product_apex_iam_service_public_api if auth_accounts_path
  use_backend be_route_product_apex_iam_service_public_api if auth_sessions_path
  use_backend be_route_product_apex_iam_service_public_api if auth_device_logins_path
  use_backend be_route_product_apex_iam_service_public_api if auth_idp_path
  use_backend be_route_product_auth_zitadel_oidc if zitadel_oidc_path
  use_backend be_email_jmap_session if jmap_session
  use_backend be_billing_stripe_webhook if stripe_webhook
  use_backend be_source_forgejo_webhook if source_forgejo_webhook
  use_backend %[var(txn.host),map(/etc/haproxy/maps/public-hosts.map,be_not_found)]

frontend fe_haproxy_metrics
  guid fe_haproxy_metrics
  bind 127.0.0.1:8404
  option socket-stats
  http-request use-service prometheus-exporter if { path -i /metrics }
  http-request return status 404

frontend fe_firecracker_host_http
  guid fe_firecracker_host_http
  bind 10.255.0.1:18080
  http-request set-header X-Forwarded-Proto http
  acl method_get method GET
  acl method_post method POST
  acl forgejo_smart_http path_reg ^/[^/]+/[^/]+(\.git)?/(info/refs|git-upload-pack)$
  use_backend be_firecracker_sandbox_h2c if method_get { path -i /internal/sandbox/v1/runner-bootstrap }
  use_backend be_firecracker_sandbox_h2c if method_post { path -i /internal/sandbox/v1/github-checkout/bundle }
  use_backend be_firecracker_sandbox_h2c if method_post { path -i /internal/sandbox/v1/bazel-telemetry/invocations }
  use_backend be_route_product_git_source_code_hosting_service_git_smart_http if method_get forgejo_smart_http
  use_backend be_route_product_git_source_code_hosting_service_git_smart_http if method_post forgejo_smart_http
  default_backend be_forbidden

frontend fe_firecracker_npm_registry
  guid fe_firecracker_npm_registry
  bind 10.255.0.1:4873
  default_backend be_route_product_npm_registry_verdaccio

backend be_not_found
  guid be_not_found
  http-request return status 404

backend be_forbidden
  guid be_forbidden
  http-request return status 403

backend be_route_product_access_pomerium_access_portal
  guid be_route_product_access_pomerium_access_portal
  server pomerium 127.0.0.1:4180 guid be_route_product_access_pomerium_access_portal_srv_pomerium

backend be_route_product_dashboard_grafana_operator_ui
  guid be_route_product_dashboard_grafana_operator_ui
  server pomerium 127.0.0.1:4180 guid be_route_product_dashboard_grafana_operator_ui_srv_pomerium
`
}

func publicHostsMap(siteDomain string) string {
	entries := []struct {
		host    string
		backend string
	}{
		{"guardianintelligence.org", "be_route_company_apex_company_frontend"},
		{siteDomain, "be_route_product_apex_verself_web_frontend"},
		{"access." + siteDomain, "be_route_product_access_pomerium_access_portal"},
		{"dashboard." + siteDomain, "be_route_product_dashboard_grafana_operator_ui"},
		{"git." + siteDomain, "be_firecracker_forgejo"},
		{"npm." + siteDomain, "be_route_product_npm_registry_verdaccio"},
		{"mail." + siteDomain, "be_route_product_mail_stalwart_jmap"},
		{"analytics.api." + siteDomain, "be_route_product_analytics_api_analytics_service_otlp_http"},
		{"billing.api." + siteDomain, "be_route_product_billing_api_billing_public_api"},
		{"distribution.api." + siteDomain, "be_route_product_distribution_api_distribution_service_public_api"},
		{"github.api." + siteDomain, "be_route_product_github_api_github_integration_service_public_api"},
		{"governance.api." + siteDomain, "be_route_product_governance_api_governance_service_public_api"},
		{"iam.api." + siteDomain, "be_route_product_iam_api_iam_service_public_api"},
		{"email.api." + siteDomain, "be_route_product_email_api_email_service_public_api"},
		{"notifications.api." + siteDomain, "be_route_product_notifications_api_notifications_service_public_api"},
		{"profile.api." + siteDomain, "be_route_product_profile_api_profile_service_public_api"},
		{"projects.api." + siteDomain, "be_route_product_projects_api_projects_service_public_api"},
		{"sandbox.api." + siteDomain, "be_route_product_sandbox_api_sandbox_rental_public_api"},
		{"secrets.api." + siteDomain, "be_route_product_secrets_api_secrets_service_public_api"},
		{"source.api." + siteDomain, "be_route_product_source_api_source_code_hosting_service_public_api"},
		{"oci." + siteDomain, "be_route_product_distribution_api_distribution_service_public_api"},
	}
	var b strings.Builder
	for _, entry := range entries {
		fmt.Fprintf(&b, "%s %s\n", entry.host, entry.backend)
	}
	return b.String()
}

func discoveryManifest(siteDomain string) string {
	return fmt.Sprintf(`{"issuer":"https://%s","site":"%s"}`+"\n", siteDomain, siteDomain)
}

func haproxySystemdUnit() string {
	return `[Unit]
Description=HAProxy public edge
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
User=haproxy
Group=haproxy
SupplementaryGroups=adm
Environment=LD_LIBRARY_PATH=/opt/verself/profile/lib/haproxy
ExecStartPre=/opt/verself/profile/bin/haproxy -c -f /etc/haproxy/haproxy.cfg -f /etc/haproxy/nomad-upstreams.cfg
ExecStart=/opt/verself/profile/bin/haproxy -Ws -f /etc/haproxy/haproxy.cfg -f /etc/haproxy/nomad-upstreams.cfg -p /run/haproxy/haproxy.pid -S /run/haproxy/master.sock
ExecReload=/opt/verself/profile/bin/haproxy -c -f /etc/haproxy/haproxy.cfg -f /etc/haproxy/nomad-upstreams.cfg
ExecReload=/bin/kill -USR2 $MAINPID
Restart=on-failure
RestartSec=5
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
NoNewPrivileges=true
RuntimeDirectory=haproxy
RuntimeDirectoryMode=0750

[Install]
WantedBy=multi-user.target
`
}

func plannedSwap(path string, content []byte, group string, mode fs.FileMode) (fileSwap, bool, error) {
	oldContent, oldErr := os.ReadFile(path)
	if oldErr != nil && !errors.Is(oldErr, os.ErrNotExist) {
		return fileSwap{}, false, fmt.Errorf("read existing %s: %w", path, oldErr)
	}
	if oldErr == nil && bytes.Equal(oldContent, content) {
		return fileSwap{}, false, nil
	}
	return fileSwap{
		path:    path,
		content: content,
		old:     oldContent,
		existed: oldErr == nil,
		group:   group,
		mode:    mode,
	}, true, nil
}

func applySwaps(swaps []fileSwap) error {
	for _, swap := range swaps {
		if err := atomicWrite(swap.path, swap.content, swap.group, swap.mode); err != nil {
			return err
		}
	}
	return nil
}

func restoreSwaps(swaps []fileSwap) error {
	var restoreErr error
	for i := len(swaps) - 1; i >= 0; i-- {
		swap := swaps[i]
		if swap.existed {
			if err := atomicWrite(swap.path, swap.old, swap.group, swap.mode); err != nil {
				restoreErr = errors.Join(restoreErr, err)
			}
			continue
		}
		if err := os.Remove(swap.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("remove %s: %w", swap.path, err))
		}
	}
	return restoreErr
}

func atomicWrite(path string, content []byte, group string, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if group != "" {
		gid, err := groupID(group)
		if err != nil {
			_ = tmp.Close()
			return err
		}
		if err := tmp.Chown(0, gid); err != nil {
			_ = tmp.Close()
			return fmt.Errorf("chown %s root:%s: %w", tmpName, group, err)
		}
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, path, err)
	}
	return nil
}

func groupID(group string) (int, error) {
	g, err := user.LookupGroup(group)
	if err != nil {
		return 0, fmt.Errorf("lookup group %s: %w", group, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, fmt.Errorf("parse gid for %s: %w", group, err)
	}
	return gid, nil
}

func validateHAProxy(cfg config) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	argv := []string{"-c"}
	for _, config := range cfg.haproxyConfigs {
		argv = append(argv, "-f", config)
	}
	cmd := exec.CommandContext(ctx, cfg.haproxyBin, argv...)
	cmd.Env = withLDLibraryPath(os.Environ(), cfg.haproxyLDLibraryPath)
	if cfg.haproxyUser != "" {
		credential, err := userCredential(cfg.haproxyUser)
		if err != nil {
			return err
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: credential}
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", cfg.haproxyBin, strings.Join(argv, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func userCredential(name string) (*syscall.Credential, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return nil, fmt.Errorf("lookup user %s: %w", name, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return nil, fmt.Errorf("parse uid for %s: %w", name, err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return nil, fmt.Errorf("parse gid for %s: %w", name, err)
	}
	credentialUID, err := uint32FromInt(uid, "uid", name)
	if err != nil {
		return nil, err
	}
	credentialGID, err := uint32FromInt(gid, "gid", name)
	if err != nil {
		return nil, err
	}
	return &syscall.Credential{Uid: credentialUID, Gid: credentialGID}, nil
}

func uint32FromInt(value int, field string, userName string) (uint32, error) {
	if value < 0 || int64(value) > maxUint32AsInt64 {
		return 0, fmt.Errorf("%s for %s exceeds uint32 range: %d", field, userName, value)
	}
	return uint32(value), nil // #nosec G115 -- value is checked against uint32 range above.
}

func withLDLibraryPath(env []string, path string) []string {
	if path == "" {
		return env
	}
	for i, kv := range env {
		if strings.HasPrefix(kv, "LD_LIBRARY_PATH=") {
			env[i] = kv + ":" + path
			return env
		}
	}
	return append(env, "LD_LIBRARY_PATH="+path)
}

func reloadOrStartUnit(unit string) error {
	if err := systemctl("is-active", "--quiet", unit); err == nil {
		return systemctl("reload", unit)
	}
	return systemctl("enable", "--now", unit)
}

func systemctl(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
