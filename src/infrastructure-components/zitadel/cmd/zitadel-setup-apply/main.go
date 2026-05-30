package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
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

	verselfotel "github.com/verself/observability/otel"
)

const (
	defaultZitadelBin       = "local/bin/zitadel"
	defaultZitadelConfig    = "/etc/zitadel/config.yaml"
	defaultZitadelSteps     = "/etc/zitadel/steps.yaml"
	defaultZitadelMasterkey = "/etc/credstore/zitadel/masterkey"
	defaultDiscoveryHosts   = "/etc/verself/auth-discovery-hosts"
	defaultZitadelUser      = "zitadel"
	defaultZitadelGroup     = "zitadel"

	openbaoAddr          = "https://127.0.0.1:8200"
	openbaoCACertPath    = "/etc/openbao/tls/cert.pem"
	openbaoRootTokenPath = "/etc/credstore/openbao/root-token"
	siteConfigMount      = "site-config"
	trustDomainPath      = "/etc/verself/spiffe-trust-domain"
)

type config struct {
	domain             string
	zitadelBin         string
	zitadelConfigPath  string
	zitadelStepsPath   string
	zitadelMasterkey   string
	discoveryHostsPath string
	zitadelUser        string
	zitadelGroup       string
	runSetup           bool
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "zitadel-setup-apply: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	cfg := config{
		domain:             envOr("VERSELF_ZITADEL_EXTERNAL_DOMAIN", ""),
		zitadelBin:         envOr("VERSELF_ZITADEL_BIN", defaultZitadelBin),
		zitadelConfigPath:  envOr("VERSELF_ZITADEL_CONFIG_PATH", defaultZitadelConfig),
		zitadelStepsPath:   envOr("VERSELF_ZITADEL_STEPS_PATH", defaultZitadelSteps),
		zitadelMasterkey:   envOr("VERSELF_ZITADEL_MASTERKEY_PATH", defaultZitadelMasterkey),
		discoveryHostsPath: envOr("VERSELF_AUTH_DISCOVERY_HOSTS_PATH", defaultDiscoveryHosts),
		zitadelUser:        envOr("VERSELF_ZITADEL_USER", defaultZitadelUser),
		zitadelGroup:       envOr("VERSELF_ZITADEL_GROUP", defaultZitadelGroup),
		runSetup:           true,
	}
	fs := flag.NewFlagSet("zitadel-setup-apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&cfg.domain, "external-domain", cfg.domain, "Zitadel external domain.")
	fs.StringVar(&cfg.zitadelBin, "zitadel-bin", cfg.zitadelBin, "Zitadel binary path.")
	fs.StringVar(&cfg.zitadelConfigPath, "config", cfg.zitadelConfigPath, "Zitadel config path.")
	fs.StringVar(&cfg.zitadelStepsPath, "steps", cfg.zitadelStepsPath, "Zitadel setup steps path.")
	fs.StringVar(&cfg.zitadelMasterkey, "masterkey", cfg.zitadelMasterkey, "Zitadel masterkey path.")
	fs.StringVar(&cfg.discoveryHostsPath, "discovery-hosts", cfg.discoveryHostsPath, "Local discovery hosts file path.")
	fs.StringVar(&cfg.zitadelUser, "zitadel-user", cfg.zitadelUser, "User to run zitadel setup as.")
	fs.StringVar(&cfg.zitadelGroup, "zitadel-group", cfg.zitadelGroup, "Group to own Zitadel config files.")
	fs.BoolVar(&cfg.runSetup, "run-setup", cfg.runSetup, "Run zitadel setup after applying config.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional args: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(cfg.domain) == "" {
		domain, err := siteDomain()
		if err != nil {
			return err
		}
		cfg.domain = domain
	}
	if err := cfg.validate(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	shutdown, err := initTelemetry(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "zitadel-setup-apply: telemetry disabled: %v\n", err)
	} else {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := shutdown(shutdownCtx); err != nil {
				fmt.Fprintf(stderr, "zitadel-setup-apply: telemetry shutdown: %v\n", err)
			}
		}()
	}
	return apply(ctx, cfg, stdout, stderr)
}

func (cfg config) validate() error {
	missing := []string{}
	for name, value := range map[string]string{
		"--external-domain": cfg.domain,
		"--zitadel-bin":     cfg.zitadelBin,
		"--config":          cfg.zitadelConfigPath,
		"--steps":           cfg.zitadelStepsPath,
		"--masterkey":       cfg.zitadelMasterkey,
		"--discovery-hosts": cfg.discoveryHostsPath,
		"--zitadel-user":    cfg.zitadelUser,
		"--zitadel-group":   cfg.zitadelGroup,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required flags: %s", strings.Join(missing, ", "))
	}
	if strings.ContainsAny(cfg.domain, "/:@") || strings.Contains(cfg.domain, "{{") {
		return fmt.Errorf("invalid external domain %q", cfg.domain)
	}
	return nil
}

func initTelemetry(ctx context.Context) (func(context.Context) error, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	shutdown, _, err := verselfotel.Init(ctx, verselfotel.Config{ServiceName: "zitadel-setup-apply"})
	if err != nil {
		return nil, err
	}
	return shutdown, nil
}

func apply(ctx context.Context, cfg config, stdout, stderr io.Writer) error {
	tracer := otel.Tracer("github.com/verself/infrastructure-components/zitadel/cmd/zitadel-setup-apply")
	ctx, span := tracer.Start(ctx, "zitadel_setup.apply")
	defer span.End()
	span.SetAttributes(attribute.String("zitadel.external_domain", cfg.domain))
	if err := ensureAccount(ctx, cfg.zitadelUser, cfg.zitadelGroup); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	uid, gid, err := lookupIDs(cfg.zitadelUser, cfg.zitadelGroup)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	secrets, err := loadSiteSecrets(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := ensureDirectory("/var/lib/zitadel", uid, gid, 0o750); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := ensureDirectory(filepath.Dir(cfg.zitadelConfigPath), uid, gid, 0o700); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := ensureDirectory(filepath.Dir(cfg.zitadelMasterkey), 0, gid, 0o750); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := ensureDirectory(filepath.Dir(cfg.discoveryHostsPath), 0, 0, 0o755); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := writeFileIfChanged(cfg.zitadelConfigPath, renderZitadelConfig(cfg.domain, secrets), uid, gid, 0o600); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := writeFileIfChanged(cfg.zitadelStepsPath, renderZitadelSteps(cfg.domain, secrets), uid, gid, 0o600); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := writeFileIfChanged(cfg.zitadelMasterkey, []byte(secrets.ZitadelMasterKey), 0, gid, 0o640); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := writeFileIfChanged(cfg.discoveryHostsPath, renderDiscoveryHosts(cfg.domain), 0, 0, 0o644); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if !cfg.runSetup {
		span.SetStatus(codes.Ok, "")
		return nil
	}
	if err := runZitadelSetup(ctx, cfg, uid, gid, stdout, stderr); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

func normalizeConfigFile(path, domain string, uid, gid int) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	next, err := normalizeExternalDomain(raw, domain)
	if err != nil {
		return fmt.Errorf("normalize %s: %w", path, err)
	}
	return writeFileIfChanged(path, next, uid, gid, 0o600)
}

func normalizeExternalDomain(content []byte, domain string) ([]byte, error) {
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "ExternalDomain:") {
			lines[i] = leadingWhitespace(line) + "ExternalDomain: " + domain
			found = true
		}
		if strings.Contains(line, "Managed by Ansible") {
			lines[i] = strings.ReplaceAll(line, "Managed by Ansible", "Managed by Nomad")
		}
	}
	if !found {
		return nil, errors.New("ExternalDomain key is missing")
	}
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}

func leadingWhitespace(value string) string {
	return value[:len(value)-len(strings.TrimLeft(value, " \t"))]
}

func renderDiscoveryHosts(domain string) []byte {
	return []byte("127.0.0.1 localhost\n::1 localhost ip6-localhost ip6-loopback\n127.0.0.1 " + domain + "\n")
}

type siteSecrets struct {
	PostgresAdminPassword string
	ZitadelAdminPassword  string
	ZitadelDBPassword     string
	ZitadelMasterKey      string
	ResendAPIKey          string
}

func loadSiteSecrets(ctx context.Context) (siteSecrets, error) {
	client, err := openbaoClient()
	if err != nil {
		return siteSecrets{}, err
	}
	var secrets siteSecrets
	items := []struct {
		Name string
		Dest *string
	}{
		{Name: "postgresql_admin_password", Dest: &secrets.PostgresAdminPassword},
		{Name: "zitadel_admin_password", Dest: &secrets.ZitadelAdminPassword},
		{Name: "zitadel_db_password", Dest: &secrets.ZitadelDBPassword},
		{Name: "zitadel_masterkey", Dest: &secrets.ZitadelMasterKey},
		{Name: "resend_api_key", Dest: &secrets.ResendAPIKey},
	}
	for _, item := range items {
		value, err := client.siteSecret(ctx, item.Name)
		if err != nil {
			return siteSecrets{}, err
		}
		*item.Dest = value
	}
	if len(secrets.ZitadelMasterKey) != 32 {
		return siteSecrets{}, fmt.Errorf("OpenBao site secret zitadel_masterkey must be exactly 32 bytes, got %d", len(secrets.ZitadelMasterKey))
	}
	if err := validateZitadelBootstrapPassword(secrets.ZitadelAdminPassword); err != nil {
		return siteSecrets{}, fmt.Errorf("OpenBao site secret zitadel_admin_password: %w", err)
	}
	return secrets, nil
}

func validateZitadelBootstrapPassword(value string) error {
	if len(value) < 8 {
		return errors.New("must be at least 8 bytes")
	}
	var hasUpper, hasLower, hasNumber, hasSymbol bool
	for _, r := range value {
		switch {
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= '0' && r <= '9':
			hasNumber = true
		case r >= '!' && r <= '/' || r >= ':' && r <= '@' || r >= '[' && r <= '`' || r >= '{' && r <= '~':
			hasSymbol = true
		}
	}
	missing := []string{}
	for _, requirement := range []struct {
		name string
		ok   bool
	}{
		{name: "uppercase", ok: hasUpper},
		{name: "lowercase", ok: hasLower},
		{name: "number", ok: hasNumber},
		{name: "symbol", ok: hasSymbol},
	} {
		name, ok := requirement.name, requirement.ok
		if !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing %s", strings.Join(missing, ", "))
	}
	return nil
}

type openbao struct {
	client *http.Client
	token  string
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

func (b openbao) siteSecret(ctx context.Context, name string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openbaoAddr+"/v1/"+siteConfigMount+"/data/config/"+name, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Vault-Token", b.token)
	resp, err := b.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("read OpenBao site secret %s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("read OpenBao site secret %s: status %d", name, resp.StatusCode)
	}
	var payload struct {
		Data struct {
			Data struct {
				Value string `json:"value"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode OpenBao site secret %s: %w", name, err)
	}
	value := strings.TrimSpace(payload.Data.Data.Value)
	if value == "" {
		return "", fmt.Errorf("OpenBao site secret %s is empty", name)
	}
	return value, nil
}

func renderZitadelConfig(domain string, secrets siteSecrets) []byte {
	sender := "noreply@notify." + domain
	return []byte(fmt.Sprintf(`# Zitadel configuration.
Log:
  Level: info
  Formatter:
    Format: json

Port: 8085
ExternalDomain: %s
ExternalPort: 443
ExternalSecure: true

TLS:
  Enabled: false

Database:
  postgres:
    Host: 127.0.0.1
    Port: 5432
    Database: zitadel
    User:
      Username: zitadel
      Password: %s
      SSL:
        Mode: disable
    Admin:
      Username: postgres
      Password: %s
      SSL:
        Mode: disable
    MaxOpenConns: 10
    MaxIdleConns: 5
    MaxConnLifetime: 30m

DefaultInstance:
  Features:
    LoginV2:
      Required: false
  DomainPolicy:
    SMTPSenderAddressMatchesInstanceDomain: false
  SMTPConfiguration:
    SMTP:
      Host: "smtp.resend.com:465"
      User: "resend"
      Password: %s
    TLS: true
    From: %s
    FromName: "verself"
  LabelPolicy:
    DisableWatermark: true
    HideLoginNameSuffix: true
  LoginPolicy:
    AllowRegister: false

Machine:
  Identification:
    PrivateIp:
      Enabled: false
    Hostname:
      Enabled: true

Instrumentation:
  Trace:
    Exporter:
      Type: grpc
      Endpoint: 127.0.0.1:4317
      Insecure: true
    Fraction: 1
  Metrics:
    Enabled: true
`, domain, yamlQuote(secrets.ZitadelDBPassword), yamlQuote(secrets.PostgresAdminPassword), yamlQuote(secrets.ResendAPIKey), yamlQuote(sender)))
}

func renderZitadelSteps(domain string, secrets siteSecrets) []byte {
	return []byte(fmt.Sprintf(`# Zitadel first-instance bootstrap.
FirstInstance:
  InstanceName: verself
  PatPath: "/etc/zitadel/admin.pat"
  Org:
    Name: Guardian Intelligence LLC
    Human:
      UserName: anveio
      FirstName: Anveio
      LastName: Platform
      Email:
        Address: %s
        Verified: true
      Password: %s
      PasswordChangeRequired: false
    Machine:
      Machine:
        Username: verself-admin
        Name: verself admin
      Pat:
        ExpirationDate: "2099-01-01T00:00:00Z"
`, yamlQuote("anveio@"+domain), yamlQuote(secrets.ZitadelAdminPassword)))
}

func yamlQuote(value string) string {
	return strconv.Quote(value)
}

func siteDomain() (string, error) {
	body, err := os.ReadFile(trustDomainPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", trustDomainPath, err)
	}
	trustDomain := strings.TrimSpace(string(body))
	if trustDomain == "" {
		return "", fmt.Errorf("%s is empty", trustDomainPath)
	}
	if strings.HasPrefix(trustDomain, "spiffe.") {
		return strings.TrimPrefix(trustDomain, "spiffe."), nil
	}
	return trustDomain, nil
}

func ensureAccount(ctx context.Context, userName, groupName string) error {
	if err := ensureGroup(ctx, groupName); err != nil {
		return err
	}
	return ensureUser(ctx, userName, groupName, "/var/lib/zitadel")
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

func runCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
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

func runZitadelSetup(ctx context.Context, cfg config, uid, gid int, stdout, stderr io.Writer) error {
	if err := runZitadel(ctx, cfg, uid, gid, stdout, stderr, "init", "--config", cfg.zitadelConfigPath); err != nil {
		return err
	}
	return runZitadel(ctx, cfg, uid, gid, stdout, stderr, "setup", "--masterkeyFile", cfg.zitadelMasterkey, "--config", cfg.zitadelConfigPath, "--steps", cfg.zitadelStepsPath)
}

func runZitadel(ctx context.Context, cfg config, uid, gid int, stdout, stderr io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, cfg.zitadelBin,
		args...,
	)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = append(os.Environ(), "HOME=/var/lib/zitadel")
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run zitadel %s: %w", args[0], err)
	}
	return nil
}

func ensureDirectory(path string, uid, gid int, mode fs.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("mkdir %s: %w", path, err)
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

func chmodChownExisting(path string, uid, gid int, mode fs.FileMode) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

func writeFileIfChanged(path string, content []byte, uid, gid int, mode fs.FileMode) error {
	old, err := os.ReadFile(path)
	if err == nil && bytes.Equal(old, content) {
		if err := os.Chown(path, uid, gid); err != nil {
			return fmt.Errorf("chown %s: %w", path, err)
		}
		if err := os.Chmod(path, mode); err != nil {
			return fmt.Errorf("chmod %s: %w", path, err)
		}
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
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
	if err := tmp.Chown(uid, gid); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chown %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, path, err)
	}
	return nil
}

func lookupIDs(userName, groupName string) (int, int, error) {
	u, err := user.Lookup(userName)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup user %s: %w", userName, err)
	}
	g, err := user.LookupGroup(groupName)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup group %s: %w", groupName, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse uid for %s: %w", userName, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse gid for %s: %w", groupName, err)
	}
	return uid, gid, nil
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
