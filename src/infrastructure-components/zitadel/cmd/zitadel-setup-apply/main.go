package main

import (
	"archive/tar"
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
	defaultRepoRoot               = "/home/ubuntu/.local/state/guardian/repo/current"
	defaultZitadelBin             = "local/bin/zitadel"
	defaultZitadelConfig          = "/etc/zitadel/config.yaml"
	defaultZitadelSteps           = "/etc/zitadel/steps.yaml"
	defaultZitadelMasterkeySecret = "zitadel.masterkey"
	defaultZitadelAdminPAT        = ""
	defaultZitadelUser            = "zitadel"
	defaultZitadelGroup           = "zitadel"
)

type mode string

const (
	modeSetup mode = "setup"
	modeStart mode = "start"
)

type config struct {
	mode                mode
	domain              string
	zitadelBin          string
	zitadelConfigPath   string
	zitadelConfigSource string
	zitadelStepsPath    string
	zitadelStepsSource  string
	masterkeySecret     string
	adminPATPath        string
	adminPATSecrets     []string
	adminPasswordSecret string
	smtpPasswordSecret  string
	zitadelUser         string
	zitadelGroup        string
	openBaoAddr         string
	openBaoCACert       string
	openBaoToken        string
	openBaoTokenFile    string
	runSetup            bool
	repoRoot            string
	resourceGraph       string
	resourceName        string
	runtimeArtifact     string
	runtimeRoot         string
	instanceName        string
	instanceOrgName     string
	humanUserName       string
	humanFirstName      string
	humanLastName       string
	humanEmail          string
	machineUserName     string
	machineName         string
	smtpHost            string
	smtpUser            string
	smtpFrom            string
	smtpFromName        string
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "zitadel-setup-apply: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	cfg := config{
		mode:              mode(envOr("VERSELF_ZITADEL_MODE", string(modeSetup))),
		domain:            envOr("VERSELF_ZITADEL_EXTERNAL_DOMAIN", ""),
		zitadelBin:        envOr("VERSELF_ZITADEL_BIN", defaultZitadelBin),
		zitadelConfigPath: envOr("VERSELF_ZITADEL_CONFIG_PATH", defaultZitadelConfig),
		zitadelStepsPath:  envOr("VERSELF_ZITADEL_STEPS_PATH", defaultZitadelSteps),
		masterkeySecret:   envOr("VERSELF_ZITADEL_MASTERKEY_OPENBAO_SECRET", defaultZitadelMasterkeySecret),
		adminPATPath:      envOr("VERSELF_ZITADEL_ADMIN_PAT_PATH", defaultZitadelAdminPAT),
		adminPATSecrets:   splitCSV(envOr("VERSELF_ZITADEL_ADMIN_PAT_OPENBAO_SECRETS", "")),
		zitadelUser:       envOr("VERSELF_ZITADEL_USER", defaultZitadelUser),
		zitadelGroup:      envOr("VERSELF_ZITADEL_GROUP", defaultZitadelGroup),
		openBaoAddr:       "",
		openBaoCACert:     "",
		runSetup:          true,
		repoRoot:          defaultRepoRoot,
		resourceName:      "zitadel",
	}
	fs := flag.NewFlagSet("zitadel-setup-apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	modeValue := string(cfg.mode)
	fs.StringVar(&modeValue, "mode", modeValue, "Operation mode: setup or start.")
	fs.StringVar(&cfg.domain, "external-domain", cfg.domain, "Zitadel external domain.")
	fs.StringVar(&cfg.zitadelBin, "zitadel-bin", cfg.zitadelBin, "Zitadel binary path.")
	fs.StringVar(&cfg.zitadelConfigPath, "config", cfg.zitadelConfigPath, "Zitadel config path.")
	fs.StringVar(&cfg.zitadelStepsPath, "steps", cfg.zitadelStepsPath, "Zitadel setup steps path.")
	fs.StringVar(&cfg.masterkeySecret, "masterkey-openbao-secret", cfg.masterkeySecret, "OpenBao runtime secret containing the Zitadel masterkey.")
	fs.StringVar(&cfg.adminPATPath, "admin-pat-path", cfg.adminPATPath, "Zitadel admin PAT path.")
	fs.StringVar(&cfg.zitadelUser, "zitadel-user", cfg.zitadelUser, "User to run zitadel setup as.")
	fs.StringVar(&cfg.zitadelGroup, "zitadel-group", cfg.zitadelGroup, "Group to own Zitadel config files.")
	fs.StringVar(&cfg.openBaoAddr, "openbao-addr", cfg.openBaoAddr, "OpenBao address for publishing generated Zitadel admin PATs.")
	fs.StringVar(&cfg.openBaoCACert, "openbao-ca-cert", cfg.openBaoCACert, "OpenBao CA certificate path.")
	fs.StringVar(&cfg.openBaoTokenFile, "openbao-token-file", cfg.openBaoTokenFile, "File containing the OpenBao workload token.")
	fs.StringVar(&cfg.repoRoot, "repo-root", cfg.repoRoot, "Boarded repo root.")
	fs.StringVar(&cfg.resourceGraph, "resource-graph", cfg.resourceGraph, "Guardian resource graph document path.")
	fs.StringVar(&cfg.resourceName, "resource-name", cfg.resourceName, "ZitadelCluster resource name.")
	fs.BoolVar(&cfg.runSetup, "run-setup", cfg.runSetup, "Run zitadel setup after applying config.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if cfg.resourceGraph != "" {
		next, err := applyResourceGraphConfig(cfg)
		if err != nil {
			return err
		}
		cfg = next
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional args: %s", strings.Join(fs.Args(), " "))
	}
	cfg.mode = mode(modeValue)
	openBaoToken, err := readRequiredCredentialFile(cfg.openBaoTokenFile, "--openbao-token-file")
	if err != nil {
		return err
	}
	cfg.openBaoToken = openBaoToken
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
	if cfg.mode != modeSetup && cfg.mode != modeStart {
		return fmt.Errorf("invalid --mode %q", cfg.mode)
	}
	missing := []string{}
	for name, value := range map[string]string{
		"--external-domain":          cfg.domain,
		"--zitadel-bin":              cfg.zitadelBin,
		"--config":                   cfg.zitadelConfigPath,
		"--masterkey-openbao-secret": cfg.masterkeySecret,
		"--zitadel-user":             cfg.zitadelUser,
		"--zitadel-group":            cfg.zitadelGroup,
	} {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	if cfg.mode == modeSetup {
		for name, value := range map[string]string{
			"--steps":          cfg.zitadelStepsPath,
			"--admin-pat-path": cfg.adminPATPath,
		} {
			if strings.TrimSpace(value) == "" {
				missing = append(missing, name)
			}
		}
	}
	if cfg.resourceGraph != "" {
		for name, value := range map[string]string{
			"ZitadelCluster.spec.runtimeArtifact":           cfg.runtimeArtifact,
			"ZitadelCluster.spec.runtimeRoot":               cfg.runtimeRoot,
			"ZitadelCluster.spec.openBao.adminPasswordRef":  cfg.adminPasswordSecret,
			"ZitadelCluster.spec.openBao.smtpPasswordRef":   cfg.smtpPasswordSecret,
			"ZitadelCluster.spec.instance.name":             cfg.instanceName,
			"ZitadelCluster.spec.instance.orgName":          cfg.instanceOrgName,
			"ZitadelCluster.spec.instance.human.userName":   cfg.humanUserName,
			"ZitadelCluster.spec.instance.human.firstName":  cfg.humanFirstName,
			"ZitadelCluster.spec.instance.human.lastName":   cfg.humanLastName,
			"ZitadelCluster.spec.instance.human.email":      cfg.humanEmail,
			"ZitadelCluster.spec.instance.machine.userName": cfg.machineUserName,
			"ZitadelCluster.spec.instance.machine.name":     cfg.machineName,
			"ZitadelCluster.spec.smtp.host":                 cfg.smtpHost,
			"ZitadelCluster.spec.smtp.user":                 cfg.smtpUser,
			"ZitadelCluster.spec.smtp.from":                 cfg.smtpFrom,
			"ZitadelCluster.spec.smtp.fromName":             cfg.smtpFromName,
		} {
			if strings.TrimSpace(value) == "" {
				missing = append(missing, name)
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required flags: %s", strings.Join(missing, ", "))
	}
	if strings.ContainsAny(cfg.domain, "/:@") || strings.Contains(cfg.domain, "{{") {
		return fmt.Errorf("invalid external domain %q", cfg.domain)
	}
	if strings.ContainsAny(cfg.masterkeySecret, "/ ") {
		return fmt.Errorf("invalid OpenBao masterkey secret name %q", cfg.masterkeySecret)
	}
	if strings.TrimSpace(cfg.openBaoAddr) == "" {
		return fmt.Errorf("--openbao-addr is required")
	}
	if strings.TrimSpace(cfg.openBaoToken) == "" {
		return fmt.Errorf("--openbao-token-file is required")
	}
	if cfg.mode == modeSetup && len(cfg.adminPATSecrets) > 0 {
		if strings.TrimSpace(cfg.openBaoAddr) == "" {
			return fmt.Errorf("--openbao-addr is required when admin PAT OpenBao secrets are configured")
		}
		if strings.TrimSpace(cfg.openBaoToken) == "" {
			return fmt.Errorf("--openbao-token-file is required when admin PAT OpenBao secrets are configured")
		}
		for _, secret := range cfg.adminPATSecrets {
			if strings.TrimSpace(secret) == "" || strings.ContainsAny(secret, "/ ") {
				return fmt.Errorf("invalid OpenBao admin PAT secret name %q", secret)
			}
		}
	}
	return nil
}

func readRequiredCredentialFile(path, flagName string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("%s is required", flagName)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", flagName, err)
	}
	value := strings.TrimSpace(string(body))
	if value == "" {
		return "", fmt.Errorf("%s is empty", flagName)
	}
	return value, nil
}

func requireRegularFile(path, flagName string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("%s is required", flagName)
	}
	stat, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", flagName, err)
	}
	if !stat.Mode().IsRegular() {
		return fmt.Errorf("%s must be a regular file: %s", flagName, path)
	}
	return nil
}

type guardianDocument struct {
	Entrypoint json.RawMessage    `json:"entrypoint,omitempty"`
	Resources  []guardianResource `json:"resources"`
}

type guardianResource struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   resourceMeta    `json:"metadata"`
	Spec       json.RawMessage `json:"spec"`
}

type resourceMeta struct {
	Name string `json:"name"`
}

type objectRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

type zitadelClusterSpec struct {
	ExternalDomain  string `json:"externalDomain"`
	RuntimeArtifact string `json:"runtimeArtifact"`
	RuntimeRoot     string `json:"runtimeRoot"`
	ConfigFile      string `json:"configFile"`
	SetupStepsFile  string `json:"setupStepsFile"`
	AdminPATPath    string `json:"adminPATPath"`
	User            string `json:"user"`
	Group           string `json:"group"`
	OpenBao         struct {
		Address            string      `json:"address"`
		CACert             string      `json:"caCert"`
		MasterkeySecretRef objectRef   `json:"masterkeySecretRef"`
		AdminPasswordRef   objectRef   `json:"adminPasswordRef"`
		AdminPATSecretRefs []objectRef `json:"adminPATSecretRefs"`
		SMTPPasswordRef    objectRef   `json:"smtpPasswordRef"`
	} `json:"openBao"`
	Instance struct {
		Name    string `json:"name"`
		OrgName string `json:"orgName"`
		Human   struct {
			UserName  string `json:"userName"`
			FirstName string `json:"firstName"`
			LastName  string `json:"lastName"`
			Email     string `json:"email"`
		} `json:"human"`
		Machine struct {
			UserName string `json:"userName"`
			Name     string `json:"name"`
		} `json:"machine"`
	} `json:"instance"`
	SMTP struct {
		Host     string `json:"host"`
		User     string `json:"user"`
		From     string `json:"from"`
		FromName string `json:"fromName"`
	} `json:"smtp"`
}

func applyResourceGraphConfig(cfg config) (config, error) {
	body, err := os.ReadFile(cfg.resourceGraph)
	if err != nil {
		return config{}, fmt.Errorf("read Guardian resource graph: %w", err)
	}
	var doc guardianDocument
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return config{}, fmt.Errorf("decode Guardian resource graph: %w", err)
	}
	name := strings.TrimSpace(cfg.resourceName)
	if name == "" {
		name = "zitadel"
	}
	var matches []guardianResource
	for _, resource := range doc.Resources {
		if resource.APIVersion == "zitadel.guardianintelligence.org/v1alpha1" && resource.Kind == "ZitadelCluster" && resource.Metadata.Name == name {
			matches = append(matches, resource)
		}
	}
	if len(matches) != 1 {
		return config{}, fmt.Errorf("expected exactly one ZitadelCluster resource named %q, found %d", name, len(matches))
	}
	var spec zitadelClusterSpec
	specDecoder := json.NewDecoder(bytes.NewReader(matches[0].Spec))
	specDecoder.DisallowUnknownFields()
	if err := specDecoder.Decode(&spec); err != nil {
		return config{}, fmt.Errorf("decode ZitadelCluster %q: %w", name, err)
	}
	cfg.domain = spec.ExternalDomain
	cfg.runtimeArtifact = spec.RuntimeArtifact
	cfg.runtimeRoot = spec.RuntimeRoot
	cfg.zitadelBin = filepath.Join(spec.RuntimeRoot, "current", "bin", "zitadel")
	cfg.zitadelConfigSource, err = repoWorkspacePath(cfg.repoRoot, spec.ConfigFile)
	if err != nil {
		return config{}, fmt.Errorf("ZitadelCluster %q spec.configFile: %w", name, err)
	}
	cfg.zitadelStepsSource, err = repoWorkspacePath(cfg.repoRoot, spec.SetupStepsFile)
	if err != nil {
		return config{}, fmt.Errorf("ZitadelCluster %q spec.setupStepsFile: %w", name, err)
	}
	cfg.zitadelConfigPath = defaultZitadelConfig
	cfg.zitadelStepsPath = defaultZitadelSteps
	cfg.adminPATPath = spec.AdminPATPath
	cfg.zitadelUser = spec.User
	cfg.zitadelGroup = spec.Group
	cfg.openBaoAddr = spec.OpenBao.Address
	cfg.openBaoCACert = spec.OpenBao.CACert
	cfg.masterkeySecret = refName(spec.OpenBao.MasterkeySecretRef)
	cfg.adminPasswordSecret = refName(spec.OpenBao.AdminPasswordRef)
	cfg.smtpPasswordSecret = refName(spec.OpenBao.SMTPPasswordRef)
	cfg.adminPATSecrets = nil
	for _, ref := range spec.OpenBao.AdminPATSecretRefs {
		cfg.adminPATSecrets = append(cfg.adminPATSecrets, refName(ref))
	}
	cfg.instanceName = spec.Instance.Name
	cfg.instanceOrgName = spec.Instance.OrgName
	cfg.humanUserName = spec.Instance.Human.UserName
	cfg.humanFirstName = spec.Instance.Human.FirstName
	cfg.humanLastName = spec.Instance.Human.LastName
	cfg.humanEmail = spec.Instance.Human.Email
	cfg.machineUserName = spec.Instance.Machine.UserName
	cfg.machineName = spec.Instance.Machine.Name
	cfg.smtpHost = spec.SMTP.Host
	cfg.smtpUser = spec.SMTP.User
	cfg.smtpFrom = spec.SMTP.From
	cfg.smtpFromName = spec.SMTP.FromName
	if cfg.resourceGraph == "" {
		cfg.resourceGraph = filepath.Join(cfg.repoRoot, "workspace/.guardian/fly/document.json")
	}
	return cfg, nil
}

func repoWorkspacePath(repoRoot, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" || filepath.IsAbs(rel) || containsParent(rel) {
		return "", fmt.Errorf("must be a repo-relative path: %s", rel)
	}
	return filepath.Join(repoRoot, "workspace", filepath.FromSlash(rel)), nil
}

func refName(ref objectRef) string {
	return strings.TrimSpace(ref.Name)
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
	if cfg.runtimeArtifact != "" {
		if err := installRuntimeArtifact(cfg); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
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
	if err := installStaticConfigFiles(cfg, uid, gid); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := requireRegularFile(cfg.zitadelConfigPath, "--config"); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if cfg.mode == modeSetup {
		if err := requireRegularFile(cfg.zitadelStepsPath, "--steps"); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
	if err := ensureDirectory("/var/lib/zitadel", uid, gid, 0o700); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	masterkey, err := readMasterkey(ctx, cfg)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if cfg.mode == modeStart {
		if err := runZitadelServer(ctx, cfg, masterkey, uid, gid, stdout, stderr); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		span.SetStatus(codes.Ok, "")
		return nil
	}
	if err := ensureDirectory(filepath.Dir(filepath.Dir(cfg.adminPATPath)), uid, gid, 0o700); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := ensureDirectory(filepath.Dir(cfg.adminPATPath), uid, gid, 0o700); err != nil {
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
	if err := runZitadelBootstrap(ctx, cfg, masterkey, uid, gid, stdout, stderr); err != nil {
		if removeErr := removeFileIfExists(transientMachineKeyPath(cfg)); removeErr != nil {
			err = errors.Join(err, fmt.Errorf("remove transient Zitadel machine key: %w", removeErr))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := removeFileIfExists(transientMachineKeyPath(cfg)); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("remove transient Zitadel machine key: %w", err)
	}
	if err := publishAdminPAT(ctx, cfg, stdout); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

func installRuntimeArtifact(cfg config) error {
	if filepath.IsAbs(cfg.runtimeArtifact) || containsParent(cfg.runtimeArtifact) {
		return fmt.Errorf("runtime artifact must be repo-relative: %s", cfg.runtimeArtifact)
	}
	if strings.TrimSpace(cfg.runtimeRoot) == "" || !filepath.IsAbs(cfg.runtimeRoot) {
		return fmt.Errorf("runtime root must be absolute: %s", cfg.runtimeRoot)
	}
	artifact := filepath.Join(cfg.repoRoot, filepath.FromSlash(cfg.runtimeArtifact))
	digest, err := fileSHA256(artifact)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.runtimeRoot, 0o755); err != nil {
		return fmt.Errorf("create runtime root: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(cfg.runtimeRoot, "install.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open runtime install lock: %w", err)
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock runtime install: %w", err)
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	release := filepath.Join(cfg.runtimeRoot, "releases", strings.ReplaceAll(digest, ":", "-"))
	if !regularFile(filepath.Join(release, "bin/zitadel")) {
		if err := extractRuntimeArtifact(artifact, release); err != nil {
			return err
		}
	}
	if err := os.Chmod(release, 0o755); err != nil {
		return fmt.Errorf("chmod runtime release: %w", err)
	}
	return promoteRuntime(cfg.runtimeRoot, release)
}

func installStaticConfigFiles(cfg config, uid, gid int) error {
	if strings.TrimSpace(cfg.zitadelConfigSource) != "" {
		if err := ensureDirectory(filepath.Dir(cfg.zitadelConfigPath), uid, gid, 0o700); err != nil {
			return err
		}
		if err := installStaticFile(cfg.zitadelConfigSource, cfg.zitadelConfigPath, uid, gid, 0o600); err != nil {
			return err
		}
	}
	if strings.TrimSpace(cfg.zitadelStepsSource) != "" {
		if err := ensureDirectory(filepath.Dir(cfg.zitadelStepsPath), uid, gid, 0o700); err != nil {
			return err
		}
		if err := installStaticFile(cfg.zitadelStepsSource, cfg.zitadelStepsPath, uid, gid, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func installStaticFile(source, dest string, uid, gid int, mode fs.FileMode) error {
	body, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read static config %s: %w", source, err)
	}
	return writeFileIfChanged(dest, body, uid, gid, mode)
}

func containsParent(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open runtime artifact: %w", err)
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("hash runtime artifact: %w", err)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func regularFile(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && stat.Mode().IsRegular()
}

func extractRuntimeArtifact(artifact string, release string) error {
	if err := os.MkdirAll(filepath.Dir(release), 0o755); err != nil {
		return fmt.Errorf("create runtime release parent: %w", err)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(release), "."+filepath.Base(release)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create runtime staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := os.Chmod(tmp, 0o755); err != nil {
		return fmt.Errorf("chmod runtime staging directory: %w", err)
	}
	if err := extractTar(artifact, tmp); err != nil {
		return err
	}
	if !regularFile(filepath.Join(tmp, "bin/zitadel")) {
		return errors.New("Zitadel runtime artifact missing bin/zitadel")
	}
	if err := os.RemoveAll(release); err != nil {
		return fmt.Errorf("remove stale runtime release: %w", err)
	}
	if err := os.Rename(tmp, release); err != nil {
		return fmt.Errorf("publish runtime release: %w", err)
	}
	return nil
}

func extractTar(artifact string, dest string) error {
	file, err := os.Open(artifact)
	if err != nil {
		return fmt.Errorf("open runtime artifact: %w", err)
	}
	defer file.Close()
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	tr := tar.NewReader(file)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read runtime artifact tar: %w", err)
		}
		target, err := safeTarTarget(destAbs, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, modeOrDefault(header.Mode, 0o755)); err != nil {
				return fmt.Errorf("create runtime directory %s: %w", header.Name, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create runtime parent %s: %w", header.Name, err)
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, modeOrDefault(header.Mode, 0o644))
			if err != nil {
				return fmt.Errorf("create runtime file %s: %w", header.Name, err)
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return fmt.Errorf("write runtime file %s: %w", header.Name, err)
			}
			if err := out.Close(); err != nil {
				return fmt.Errorf("close runtime file %s: %w", header.Name, err)
			}
		default:
			return fmt.Errorf("unsupported runtime artifact tar entry %s type %d", header.Name, header.Typeflag)
		}
	}
}

func safeTarTarget(destAbs string, name string) (string, error) {
	if filepath.IsAbs(name) || containsParent(name) {
		return "", fmt.Errorf("unsafe runtime artifact tar entry %s", name)
	}
	target := filepath.Join(destAbs, name)
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if targetAbs != destAbs && !strings.HasPrefix(targetAbs, destAbs+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe runtime artifact tar entry %s", name)
	}
	return targetAbs, nil
}

func modeOrDefault(mode int64, fallback os.FileMode) os.FileMode {
	if mode == 0 {
		return fallback
	}
	return os.FileMode(mode).Perm()
}

func promoteRuntime(root string, release string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create runtime root: %w", err)
	}
	next := filepath.Join(root, "current.next")
	current := filepath.Join(root, "current")
	if err := os.Remove(next); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale runtime current symlink: %w", err)
	}
	if err := os.Symlink(release, next); err != nil {
		return fmt.Errorf("create runtime current symlink: %w", err)
	}
	if stat, err := os.Lstat(current); err == nil && stat.Mode()&os.ModeSymlink == 0 {
		if err := os.RemoveAll(current); err != nil {
			return fmt.Errorf("remove non-symlink runtime current path: %w", err)
		}
	}
	if err := os.Rename(next, current); err != nil {
		return fmt.Errorf("publish runtime current symlink: %w", err)
	}
	return nil
}

func readMasterkey(ctx context.Context, cfg config) (string, error) {
	client, err := newBaoClient(cfg)
	if err != nil {
		return "", err
	}
	value, found, err := client.readRuntimeSecret(ctx, cfg.masterkeySecret)
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if !found || value == "" {
		return "", fmt.Errorf("OpenBao runtime secret %s is empty or missing", cfg.masterkeySecret)
	}
	return value, nil
}

func runZitadelBootstrap(ctx context.Context, cfg config, masterkey string, uid, gid int, stdout, stderr io.Writer) error {
	// Zitadel bootstrap only initializes its own internals.
	if err := runZitadelCommand(ctx, cfg, masterkey, uid, gid, stdout, stderr,
		"init",
		"zitadel",
		"--config", cfg.zitadelConfigPath,
	); err != nil {
		return fmt.Errorf("run zitadel init: %w", err)
	}
	if err := runZitadelCommand(ctx, cfg, masterkey, uid, gid, stdout, stderr,
		"setup",
		"--masterkeyFromEnv",
		"--config", cfg.zitadelConfigPath,
		"--steps", cfg.zitadelStepsPath,
	); err != nil {
		return fmt.Errorf("run zitadel setup: %w", err)
	}
	return nil
}

func runZitadelServer(ctx context.Context, cfg config, masterkey string, uid, gid int, stdout, stderr io.Writer) error {
	return runZitadelCommand(ctx, cfg, masterkey, uid, gid, stdout, stderr,
		"start",
		"--masterkeyFromEnv",
		"--config", cfg.zitadelConfigPath,
	)
}

func runZitadelCommand(ctx context.Context, cfg config, masterkey string, uid, gid int, stdout, stderr io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, cfg.zitadelBin, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	env, err := buildZitadelEnv(ctx, cfg, masterkey)
	if err != nil {
		return err
	}
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}}
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func buildZitadelEnv(ctx context.Context, cfg config, masterkey string) ([]string, error) {
	env := append(os.Environ(),
		"HOME=/var/lib/zitadel",
		"ZITADEL_MASTERKEY="+strings.TrimSpace(masterkey),
		"ZITADEL_EXTERNALDOMAIN="+strings.TrimSpace(cfg.domain),
		"ZITADEL_EXTERNALPORT=443",
		"ZITADEL_EXTERNALSECURE=true",
		"ZITADEL_TLS_ENABLED=false",
	)
	if cfg.resourceGraph == "" {
		return env, nil
	}
	client, err := newBaoClient(cfg)
	if err != nil {
		return nil, err
	}
	smtpPassword, found, err := client.readRuntimeSecret(ctx, cfg.smtpPasswordSecret)
	if err != nil {
		return nil, err
	}
	if found && strings.TrimSpace(smtpPassword) != "" {
		env = append(env,
			"ZITADEL_DEFAULTINSTANCE_SMTPCONFIGURATION_SMTP_HOST="+strings.TrimSpace(cfg.smtpHost),
			"ZITADEL_DEFAULTINSTANCE_SMTPCONFIGURATION_SMTP_USER="+strings.TrimSpace(cfg.smtpUser),
			"ZITADEL_DEFAULTINSTANCE_SMTPCONFIGURATION_SMTP_PASSWORD="+strings.TrimSpace(smtpPassword),
			"ZITADEL_DEFAULTINSTANCE_SMTPCONFIGURATION_TLS=true",
			"ZITADEL_DEFAULTINSTANCE_SMTPCONFIGURATION_FROM="+strings.TrimSpace(cfg.smtpFrom),
			"ZITADEL_DEFAULTINSTANCE_SMTPCONFIGURATION_FROMNAME="+strings.TrimSpace(cfg.smtpFromName),
		)
	}
	if cfg.mode != modeSetup {
		return env, nil
	}
	adminPassword, found, err := client.readRuntimeSecret(ctx, cfg.adminPasswordSecret)
	if err != nil {
		return nil, err
	}
	if !found || strings.TrimSpace(adminPassword) == "" {
		return nil, fmt.Errorf("OpenBao runtime secret %s is empty or missing", cfg.adminPasswordSecret)
	}
	env = append(env,
		"ZITADEL_FIRSTINSTANCE_INSTANCENAME="+strings.TrimSpace(cfg.instanceName),
		"ZITADEL_FIRSTINSTANCE_MACHINEKEYPATH="+transientMachineKeyPath(cfg),
		"ZITADEL_FIRSTINSTANCE_PATPATH="+strings.TrimSpace(cfg.adminPATPath),
		"ZITADEL_FIRSTINSTANCE_ORG_NAME="+strings.TrimSpace(cfg.instanceOrgName),
		"ZITADEL_FIRSTINSTANCE_ORG_HUMAN_USERNAME="+strings.TrimSpace(cfg.humanUserName),
		"ZITADEL_FIRSTINSTANCE_ORG_HUMAN_FIRSTNAME="+strings.TrimSpace(cfg.humanFirstName),
		"ZITADEL_FIRSTINSTANCE_ORG_HUMAN_LASTNAME="+strings.TrimSpace(cfg.humanLastName),
		"ZITADEL_FIRSTINSTANCE_ORG_HUMAN_EMAIL_ADDRESS="+strings.TrimSpace(cfg.humanEmail),
		"ZITADEL_FIRSTINSTANCE_ORG_HUMAN_EMAIL_VERIFIED=true",
		"ZITADEL_FIRSTINSTANCE_ORG_HUMAN_PASSWORD="+strings.TrimSpace(adminPassword),
		"ZITADEL_FIRSTINSTANCE_ORG_HUMAN_PASSWORDCHANGEREQUIRED=false",
		"ZITADEL_FIRSTINSTANCE_ORG_MACHINE_MACHINE_USERNAME="+strings.TrimSpace(cfg.machineUserName),
		"ZITADEL_FIRSTINSTANCE_ORG_MACHINE_MACHINE_NAME="+strings.TrimSpace(cfg.machineName),
		"ZITADEL_FIRSTINSTANCE_ORG_MACHINE_MACHINEKEY_EXPIRATIONDATE=2099-01-01T00:00:00Z",
		"ZITADEL_FIRSTINSTANCE_ORG_MACHINE_MACHINEKEY_TYPE=1",
		"ZITADEL_FIRSTINSTANCE_ORG_MACHINE_PAT_EXPIRATIONDATE=2099-01-01T00:00:00Z",
	)
	return env, nil
}

func transientMachineKeyPath(cfg config) string {
	return filepath.Join(filepath.Dir(strings.TrimSpace(cfg.adminPATPath)), "machine-key.json")
}

func publishAdminPAT(ctx context.Context, cfg config, stdout io.Writer) error {
	if len(cfg.adminPATSecrets) == 0 {
		return nil
	}
	client, err := newBaoClient(cfg)
	if err != nil {
		return err
	}
	value, generated, err := readGeneratedAdminPAT(cfg.adminPATPath)
	if err != nil {
		return err
	}
	if !generated {
		// Zitadel only writes PatPath while creating the first instance; retries
		// must find the already-published value in OpenBao.
		return requirePublishedAdminPAT(ctx, client, cfg.adminPATSecrets, stdout)
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

func readGeneratedAdminPAT(path string) (string, bool, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read generated Zitadel admin PAT: %w", err)
	}
	value := strings.TrimSpace(string(body))
	if value == "" {
		return "", false, fmt.Errorf("generated Zitadel admin PAT is empty")
	}
	return value, true, nil
}

func requirePublishedAdminPAT(ctx context.Context, client *baoClient, secrets []string, stdout io.Writer) error {
	for _, secret := range secrets {
		value, found, err := client.readRuntimeSecret(ctx, secret)
		if err != nil {
			return err
		}
		if !found || strings.TrimSpace(value) == "" {
			return fmt.Errorf("generated Zitadel admin PAT is missing and OpenBao runtime secret %s is empty or missing", secret)
		}
		fmt.Fprintf(stdout, "zitadel-setup-apply: admin PAT %s already present in OpenBao sha256=%s\n", secret, fingerprint(value))
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

func (c *baoClient) readRuntimeSecret(ctx context.Context, name string) (string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.addr+"/v1/kv-runtime/data/secret/org/"+url.PathEscape(name), http.NoBody)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("X-Vault-Token", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("openbao GET runtime secret %s: %w", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusNotFound {
		return "", false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("openbao GET runtime secret %s status %d: %s", name, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var decoded struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", false, fmt.Errorf("decode OpenBao runtime secret %s: %w", name, err)
	}
	data, _ := decoded.Data["data"].(map[string]any)
	return strings.TrimSpace(fmt.Sprint(data["value"])), true, nil
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
