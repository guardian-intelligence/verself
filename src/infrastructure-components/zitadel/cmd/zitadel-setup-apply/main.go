package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
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

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	verselfotel "github.com/verself/observability/otel"
)

const (
	defaultZitadelBin       = "local/bin/zitadel"
	defaultZitadelConfig    = "/etc/zitadel/config.yaml"
	defaultZitadelSteps     = "/etc/zitadel/steps.yaml"
	defaultZitadelMasterkey = ""
	defaultZitadelAdminPAT  = "/etc/zitadel/admin.pat"
	defaultDiscoveryHosts   = "/etc/verself/auth-discovery-hosts"
	defaultZitadelUser      = "zitadel"
	defaultZitadelGroup     = "zitadel"
)

type config struct {
	domain             string
	zitadelBin         string
	zitadelConfigPath  string
	zitadelStepsPath   string
	zitadelMasterkey   string
	adminPATPath       string
	adminPATSecrets    []string
	discoveryHostsPath string
	zitadelUser        string
	zitadelGroup       string
	openBaoAddr        string
	openBaoCACert      string
	openBaoToken       string
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
		adminPATPath:       envOr("VERSELF_ZITADEL_ADMIN_PAT_PATH", defaultZitadelAdminPAT),
		adminPATSecrets:    splitCSV(envOr("VERSELF_ZITADEL_ADMIN_PAT_OPENBAO_SECRETS", "")),
		discoveryHostsPath: envOr("VERSELF_AUTH_DISCOVERY_HOSTS_PATH", defaultDiscoveryHosts),
		zitadelUser:        envOr("VERSELF_ZITADEL_USER", defaultZitadelUser),
		zitadelGroup:       envOr("VERSELF_ZITADEL_GROUP", defaultZitadelGroup),
		openBaoAddr:        envOr("BAO_ADDR", envOr("VAULT_ADDR", "")),
		openBaoCACert:      envOr("BAO_CACERT", envOr("VAULT_CACERT", "")),
		openBaoToken:       envOr("BAO_TOKEN", envOr("VAULT_TOKEN", "")),
		runSetup:           true,
	}
	fs := flag.NewFlagSet("zitadel-setup-apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&cfg.domain, "external-domain", cfg.domain, "Zitadel external domain.")
	fs.StringVar(&cfg.zitadelBin, "zitadel-bin", cfg.zitadelBin, "Zitadel binary path.")
	fs.StringVar(&cfg.zitadelConfigPath, "config", cfg.zitadelConfigPath, "Zitadel config path.")
	fs.StringVar(&cfg.zitadelStepsPath, "steps", cfg.zitadelStepsPath, "Zitadel setup steps path.")
	fs.StringVar(&cfg.zitadelMasterkey, "masterkey", cfg.zitadelMasterkey, "Zitadel masterkey path.")
	fs.StringVar(&cfg.adminPATPath, "admin-pat-path", cfg.adminPATPath, "Zitadel admin PAT path.")
	fs.StringVar(&cfg.discoveryHostsPath, "discovery-hosts", cfg.discoveryHostsPath, "Local discovery hosts file path.")
	fs.StringVar(&cfg.zitadelUser, "zitadel-user", cfg.zitadelUser, "User to run zitadel setup as.")
	fs.StringVar(&cfg.zitadelGroup, "zitadel-group", cfg.zitadelGroup, "Group to own Zitadel config files.")
	fs.StringVar(&cfg.openBaoAddr, "openbao-addr", cfg.openBaoAddr, "OpenBao address for publishing generated Zitadel admin PATs.")
	fs.StringVar(&cfg.openBaoCACert, "openbao-ca-cert", cfg.openBaoCACert, "OpenBao CA certificate path.")
	fs.StringVar(&cfg.openBaoToken, "openbao-token", cfg.openBaoToken, "OpenBao token. Defaults to BAO_TOKEN or VAULT_TOKEN.")
	fs.BoolVar(&cfg.runSetup, "run-setup", cfg.runSetup, "Run zitadel setup after applying config.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional args: %s", strings.Join(fs.Args(), " "))
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
		"--admin-pat-path":  cfg.adminPATPath,
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
	if len(cfg.adminPATSecrets) > 0 {
		if strings.TrimSpace(cfg.openBaoAddr) == "" {
			return fmt.Errorf("--openbao-addr is required when admin PAT OpenBao secrets are configured")
		}
		if strings.TrimSpace(cfg.openBaoToken) == "" {
			return fmt.Errorf("--openbao-token is required when admin PAT OpenBao secrets are configured")
		}
		for _, secret := range cfg.adminPATSecrets {
			if strings.TrimSpace(secret) == "" || strings.ContainsAny(secret, "/ ") {
				return fmt.Errorf("invalid OpenBao admin PAT secret name %q", secret)
			}
		}
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
	if err := ensureSystemAccount(cfg.zitadelUser, cfg.zitadelGroup); err != nil {
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
	if err := ensureDirectory(filepath.Dir(cfg.zitadelConfigPath), uid, gid, 0o700); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := ensureDirectory("/var/lib/zitadel", uid, gid, 0o700); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := ensureDirectory(filepath.Dir(cfg.zitadelMasterkey), 0, gid, 0o750); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := ensureDirectory(filepath.Dir(cfg.adminPATPath), uid, gid, 0o700); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := ensureDirectory(filepath.Dir(cfg.discoveryHostsPath), 0, 0, 0o755); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := normalizeConfigFile(cfg.zitadelConfigPath, cfg.domain, uid, gid); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := chmodChownExisting(cfg.zitadelStepsPath, uid, gid, 0o600); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := normalizeSecretFile(cfg.zitadelMasterkey, 0, gid, 0o640); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := writeFileIfChanged(cfg.discoveryHostsPath, renderDiscoveryHosts(cfg.domain), 0, 0, 0o644); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := removeFileIfExists(cfg.adminPATPath); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if !cfg.runSetup {
		span.SetStatus(codes.Ok, "")
		return nil
	}
	if err := runZitadelBootstrap(ctx, cfg, uid, gid, stdout, stderr); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := publishAdminPAT(ctx, cfg, stdout); err != nil {
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

func normalizeSecretFile(path string, uid, gid int, mode fs.FileMode) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return fmt.Errorf("%s is empty", path)
	}
	// Nomad templates render a trailing newline by default; Zitadel counts it
	// when validating the 32-byte master key.
	if err := writeFileIfChanged(path, []byte(value), uid, gid, mode); err != nil {
		return err
	}
	return chmodChownExisting(path, uid, gid, mode)
}

func runZitadelBootstrap(ctx context.Context, cfg config, uid, gid int, stdout, stderr io.Writer) error {
	// PostgreSQL ownership is reconciled by substrate-control-plane; Zitadel
	// only initializes its own internals here.
	if err := runZitadelCommand(ctx, cfg, uid, gid, stdout, stderr,
		"init",
		"zitadel",
		"--config", cfg.zitadelConfigPath,
	); err != nil {
		return fmt.Errorf("run zitadel init: %w", err)
	}
	if err := runZitadelCommand(ctx, cfg, uid, gid, stdout, stderr,
		"setup",
		"--masterkeyFile", cfg.zitadelMasterkey,
		"--config", cfg.zitadelConfigPath,
		"--steps", cfg.zitadelStepsPath,
	); err != nil {
		return fmt.Errorf("run zitadel setup: %w", err)
	}
	return nil
}

func runZitadelCommand(ctx context.Context, cfg config, uid, gid int, stdout, stderr io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, cfg.zitadelBin, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = append(os.Environ(), "HOME=/var/lib/zitadel")
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}}
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func publishAdminPAT(ctx context.Context, cfg config, stdout io.Writer) error {
	if len(cfg.adminPATSecrets) == 0 {
		return nil
	}
	body, err := os.ReadFile(cfg.adminPATPath)
	if err != nil {
		return fmt.Errorf("read generated Zitadel admin PAT: %w", err)
	}
	value := strings.TrimSpace(string(body))
	if value == "" {
		return fmt.Errorf("generated Zitadel admin PAT is empty")
	}
	client, err := newBaoClient(cfg)
	if err != nil {
		return err
	}
	for _, secret := range cfg.adminPATSecrets {
		if err := client.writeRuntimeSecret(ctx, secret, value); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "zitadel-setup-apply: published admin PAT %s sha256=%s\n", secret, fingerprint(value))
	}
	if err := os.Remove(cfg.adminPATPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove generated Zitadel admin PAT: %w", err)
	}
	return nil
}

type baoClient struct {
	addr  string
	token string
	http  *http.Client
}

func newBaoClient(cfg config) (*baoClient, error) {
	addr := strings.TrimRight(strings.TrimSpace(cfg.openBaoAddr), "/")
	if addr == "" {
		return nil, fmt.Errorf("OpenBao address is required")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.openBaoCACert != "" {
		pem, err := os.ReadFile(cfg.openBaoCACert)
		if err != nil {
			return nil, fmt.Errorf("read OpenBao CA certificate: %w", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("parse OpenBao CA certificate %s", cfg.openBaoCACert)
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	}
	return &baoClient{
		addr:  addr,
		token: strings.TrimSpace(cfg.openBaoToken),
		http:  &http.Client{Transport: transport, Timeout: 5 * time.Second},
	}, nil
}

func (c *baoClient) writeRuntimeSecret(ctx context.Context, name, value string) error {
	return c.write(ctx, "v1/kv-runtime/data/secret/org/"+url.PathEscape(name), map[string]any{"data": map[string]any{"value": value}})
}

func (c *baoClient) write(ctx context.Context, path string, body any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode OpenBao request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.addr+"/"+path, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("X-Vault-Token", c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("openbao POST %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("openbao POST %s status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func ensureSystemAccount(name, group string) error {
	if _, err := user.LookupGroup(group); err != nil {
		if _, ok := err.(user.UnknownGroupError); !ok {
			return fmt.Errorf("lookup group %s: %w", group, err)
		}
		if err := exec.Command("/usr/sbin/groupadd", "--system", group).Run(); err != nil {
			return fmt.Errorf("create group %s: %w", group, err)
		}
	}
	if _, err := user.Lookup(name); err != nil {
		if _, ok := err.(user.UnknownUserError); !ok {
			return fmt.Errorf("lookup user %s: %w", name, err)
		}
		cmd := exec.Command("/usr/sbin/useradd", "--system", "--gid", group, "--home-dir", "/var/lib/zitadel", "--shell", "/usr/sbin/nologin", "--no-create-home", name)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("create user %s: %w", name, err)
		}
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

func removeFileIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale %s: %w", path, err)
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

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func fingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
