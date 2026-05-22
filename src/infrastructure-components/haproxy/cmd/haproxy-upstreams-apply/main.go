package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
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

const maxUint32AsInt64 = int64(1<<32 - 1)

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
	authEdgeDomain       string
	authEdgeConfig       string
	authEdgePublicMap    string
	authEdgeDiscovery    string
	cliClientIDPath      string
	productAudiencePath  string
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
	fs.StringVar(&cfg.haproxyBin, "haproxy-bin", "/opt/verself/profile/bin/haproxy", "Path to the HAProxy binary.")
	fs.Var(&cfg.haproxyConfigs, "haproxy-config", "HAProxy config to validate; repeat in HAProxy load order.")
	fs.StringVar(&cfg.haproxyLDLibraryPath, "haproxy-ld-library-path", "/opt/aws-lc/lib/x86_64-linux-gnu", "LD_LIBRARY_PATH used when invoking HAProxy.")
	fs.StringVar(&cfg.reloadUnit, "reload-unit", "haproxy.service", "systemd unit to reload after a valid upstream swap.")
	fs.BoolVar(&cfg.daemon, "daemon", false, "Apply once, then stay alive until SIGINT or SIGTERM.")
	fs.StringVar(&cfg.authEdgeDomain, "auth-edge-domain", "", "Product apex domain whose same-origin auth routes should be installed.")
	fs.StringVar(&cfg.authEdgeConfig, "auth-edge-haproxy-config", "/etc/haproxy/haproxy.cfg", "Static HAProxy config to normalize when --auth-edge-domain is set.")
	fs.StringVar(&cfg.authEdgePublicMap, "auth-edge-public-hosts-map", "/etc/haproxy/maps/public-hosts.map", "Public host map to normalize when --auth-edge-domain is set.")
	fs.StringVar(&cfg.authEdgeDiscovery, "auth-edge-discovery-manifest", "/var/www/verself/.well-known/verself", "Verself discovery manifest to normalize when --auth-edge-domain is set.")
	fs.StringVar(&cfg.cliClientIDPath, "auth-edge-cli-client-id-path", "/etc/credstore/iam-service/oidc-cli-client-id", "CLI OIDC client ID credential path.")
	fs.StringVar(&cfg.productAudiencePath, "auth-edge-product-audience-path", "/etc/credstore/iam-service/auth-audience", "Product API auth audience credential path.")
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
	if cfg.authEdgeDomain != "" {
		authSwaps, err := planAuthEdgeSwaps(cfg)
		if err != nil {
			return false, err
		}
		swaps = append(swaps, authSwaps...)
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
		if err := systemctl("reload", cfg.reloadUnit); err != nil {
			return false, err
		}
	}
	return true, nil
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

func planAuthEdgeSwaps(cfg config) ([]fileSwap, error) {
	staticConfig, err := os.ReadFile(cfg.authEdgeConfig)
	if err != nil {
		return nil, fmt.Errorf("read auth edge HAProxy config %s: %w", cfg.authEdgeConfig, err)
	}
	nextStatic, err := normalizeAuthEdgeHAProxy(staticConfig, cfg.authEdgeDomain)
	if err != nil {
		return nil, err
	}
	publicMap, err := os.ReadFile(cfg.authEdgePublicMap)
	if err != nil {
		return nil, fmt.Errorf("read auth edge public host map %s: %w", cfg.authEdgePublicMap, err)
	}
	nextPublicMap := normalizeAuthEdgePublicHosts(publicMap, cfg.authEdgeDomain)
	discovery, err := renderAuthEdgeDiscovery(cfg)
	if err != nil {
		return nil, err
	}
	swaps := []fileSwap{}
	for _, candidate := range []fileSwap{
		{path: cfg.authEdgeConfig, content: nextStatic, group: "haproxy", mode: 0o640},
		{path: cfg.authEdgePublicMap, content: nextPublicMap, group: "haproxy", mode: 0o640},
		{path: cfg.authEdgeDiscovery, content: discovery, group: "root", mode: 0o644},
	} {
		swap, changed, err := plannedSwap(candidate.path, candidate.content, candidate.group, candidate.mode)
		if err != nil {
			return nil, err
		}
		if changed {
			swaps = append(swaps, swap)
		}
	}
	return swaps, nil
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

func normalizeAuthEdgeHAProxy(content []byte, domain string) ([]byte, error) {
	if strings.TrimSpace(domain) == "" {
		return nil, errors.New("auth edge domain is required")
	}
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	out := make([]string, 0, len(lines)+8)
	hasOIDCExact := hasLine(lines, "acl zitadel_oidc_path path -i /.well-known/openid-configuration /.well-known/webfinger /robots.txt")
	hasOIDCPrefix := hasLine(lines, "acl zitadel_oidc_path path_beg /oauth/ /oauth/v2/ /oidc/ /oidc/v1/ /saml/ /ui/login /ui/console")
	hasProductClaims405 := hasLine(lines, "http-request return status 405 if host_verself zitadel_product_token_claims !method_post")
	hasProductClaimsBackend := hasLine(lines, "use_backend be_zitadel_product_token_claims if host_verself method_post zitadel_product_token_claims")
	hasOIDCBackend := hasLine(lines, "use_backend be_route_product_auth_zitadel_oidc if host_verself zitadel_oidc_path")
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trim, "acl host_zitadel "):
			continue
		case strings.HasPrefix(trim, "acl host_zitadel_actions "):
			continue
		case trim == "acl forgejo_actions path -i /api/actions":
			continue
		case trim == "acl forgejo_actions_prefix path_beg /api/actions/":
			continue
		case strings.HasPrefix(trim, "http-request return status 405 if host_zitadel "):
			continue
		case strings.HasPrefix(trim, "http-request return status 405 if host_zitadel_actions "):
			continue
		case strings.HasPrefix(trim, "use_backend be_zitadel_product_token_claims if host_zitadel "):
			continue
		case strings.HasPrefix(trim, "use_backend be_zitadel_product_token_claims if host_zitadel_actions "):
			continue
		case strings.Contains(trim, "be_sandbox_forgejo_actions_webhook"):
			continue
		case strings.Contains(trim, "be_firecracker_forgejo") && (strings.Contains(trim, "forgejo_actions") || strings.Contains(trim, "forgejo_actions_prefix")):
			continue
		case strings.HasPrefix(trim, "acl auth_path path -i "):
			line = "  acl auth_path path -i /api/v1/auth/login /api/v1/auth/callback /api/v1/auth/session /api/v1/auth/organization /api/v1/auth/resource-token /api/v1/auth/logout /api/v1/auth/sessions /api/v1/auth/invites/accept"
		case strings.HasPrefix(trim, "acl route_1_path path -i "):
			line = "  acl route_1_path path -i /api/v1/auth/login /api/v1/auth/callback /api/v1/auth/session /api/v1/auth/organization /api/v1/auth/resource-token /api/v1/auth/logout /api/v1/auth/sessions /api/v1/auth/invites/accept"
		}
		trim = strings.TrimSpace(line)
		if trim == "acl zitadel_oidc_path path -i /.well-known/openid-configuration /.well-known/webfinger /robots.txt" {
			hasOIDCExact = true
		}
		if trim == "acl zitadel_oidc_path path_beg /oauth/ /oauth/v2/ /oidc/ /oidc/v1/ /saml/ /ui/login /ui/console" {
			hasOIDCPrefix = true
		}
		if trim == "http-request return status 405 if host_verself zitadel_product_token_claims !method_post" {
			hasProductClaims405 = true
		}
		if trim == "use_backend be_zitadel_product_token_claims if host_verself method_post zitadel_product_token_claims" {
			hasProductClaimsBackend = true
		}
		if trim == "use_backend be_route_product_auth_zitadel_oidc if host_verself zitadel_oidc_path" {
			hasOIDCBackend = true
		}
		out = append(out, line)
		if strings.HasPrefix(trim, "acl stripe_source src ") {
			if !hasOIDCExact {
				out = append(out, "  acl zitadel_oidc_path path -i /.well-known/openid-configuration /.well-known/webfinger /robots.txt")
				hasOIDCExact = true
			}
			if !hasOIDCPrefix {
				out = append(out, "  acl zitadel_oidc_path path_beg /oauth/ /oauth/v2/ /oidc/ /oidc/v1/ /saml/ /ui/login /ui/console")
				hasOIDCPrefix = true
			}
		}
		if trim == "http-request return status 405 if host_billing stripe_webhook !method_post" && !hasProductClaims405 {
			out = append(out, "  http-request return status 405 if host_verself zitadel_product_token_claims !method_post")
			hasProductClaims405 = true
		}
		if strings.HasPrefix(trim, "use_backend be_source_forgejo_webhook ") && !hasProductClaimsBackend {
			out = append(out, "  use_backend be_zitadel_product_token_claims if host_verself method_post zitadel_product_token_claims")
			hasProductClaimsBackend = true
			if !hasOIDCBackend {
				out = append(out, "  use_backend be_route_product_auth_zitadel_oidc if host_verself zitadel_oidc_path")
				hasOIDCBackend = true
			}
		}
		if trim == "use_backend be_zitadel_product_token_claims if host_verself method_post zitadel_product_token_claims" && !hasOIDCBackend {
			out = append(out, "  use_backend be_route_product_auth_zitadel_oidc if host_verself zitadel_oidc_path")
			hasOIDCBackend = true
		}
	}
	if !hasOIDCExact || !hasOIDCPrefix || !hasProductClaims405 || !hasProductClaimsBackend || !hasOIDCBackend {
		return nil, errors.New("auth edge HAProxy config did not contain expected insertion anchors")
	}
	return []byte(strings.Join(out, "\n") + "\n"), nil
}

func hasLine(lines []string, needle string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) == needle {
			return true
		}
	}
	return false
}

func normalizeAuthEdgePublicHosts(content []byte, domain string) []byte {
	legacyAuth := "auth." + domain
	legacyActions := "zitadel-actions." + domain
	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, legacyAuth+" ") || strings.HasPrefix(trim, legacyActions+" ") {
			continue
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n") + "\n")
}

func renderAuthEdgeDiscovery(cfg config) ([]byte, error) {
	raw, err := os.ReadFile(cfg.authEdgeDiscovery)
	if err != nil {
		return nil, fmt.Errorf("read discovery manifest %s: %w", cfg.authEdgeDiscovery, err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("decode discovery manifest %s: %w", cfg.authEdgeDiscovery, err)
	}
	auth, _ := manifest["auth"].(map[string]any)
	if auth == nil {
		auth = map[string]any{}
		manifest["auth"] = auth
	}
	cliClientID, err := readTrimmedOptional(cfg.cliClientIDPath)
	if err != nil {
		return nil, err
	}
	productAudience, err := readTrimmedOptional(cfg.productAudiencePath)
	if err != nil {
		return nil, err
	}
	auth["issuer_url"] = "https://" + cfg.authEdgeDomain
	auth["cli_client_id"] = cliClientID
	auth["product_api_audience"] = productAudience
	out, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode discovery manifest: %w", err)
	}
	return append(out, '\n'), nil
}

func readTrimmedOptional(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return strings.TrimSpace(string(raw)), nil
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

func systemctl(action, unit string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", action, unit).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s %s: %w: %s", action, unit, err, strings.TrimSpace(string(out)))
	}
	return nil
}
