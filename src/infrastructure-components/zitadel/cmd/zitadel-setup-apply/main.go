package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
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

	verselfotel "github.com/verself/observability/otel"
)

const (
	defaultZitadelBin       = "/opt/verself/profile/bin/zitadel"
	defaultZitadelConfig    = "/etc/zitadel/config.yaml"
	defaultZitadelSteps     = "/etc/zitadel/steps.yaml"
	defaultZitadelMasterkey = "/etc/credstore/zitadel/masterkey"
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
		"--external-domain":  cfg.domain,
		"--zitadel-bin":      cfg.zitadelBin,
		"--config":           cfg.zitadelConfigPath,
		"--steps":            cfg.zitadelStepsPath,
		"--masterkey":        cfg.zitadelMasterkey,
		"--discovery-hosts":  cfg.discoveryHostsPath,
		"--zitadel-user":     cfg.zitadelUser,
		"--zitadel-group":    cfg.zitadelGroup,
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
	if err := chmodChownExisting(cfg.zitadelMasterkey, 0, gid, 0o640); err != nil {
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

func runZitadelSetup(ctx context.Context, cfg config, uid, gid int, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, cfg.zitadelBin,
		"setup",
		"--masterkeyFile", cfg.zitadelMasterkey,
		"--config", cfg.zitadelConfigPath,
		"--steps", cfg.zitadelStepsPath,
	)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = append(os.Environ(), "HOME=/var/lib/zitadel")
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run zitadel setup: %w", err)
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
