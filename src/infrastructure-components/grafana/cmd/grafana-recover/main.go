package main

import (
	"archive/tar"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
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
	apiVersion      = "grafana.guardianintelligence.org/v1alpha1"
	kind            = "GrafanaInstance"
	defaultRepoRoot = "/home/ubuntu/.local/state/guardian/repo"
	defaultResource = "grafana"
)

type options struct {
	repoRoot         string
	resourceGraph    string
	resourceName     string
	openBaoTokenFile string
}

type document struct {
	Entrypoint json.RawMessage `json:"entrypoint"`
	Resources  []resource      `json:"resources"`
}

type resource struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   metadata        `json:"metadata"`
	Spec       json.RawMessage `json:"spec"`
}

type metadata struct {
	Name string `json:"name"`
}

type objectRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

type config struct {
	RuntimeArtifact string         `json:"runtimeArtifact"`
	RuntimeRoot     string         `json:"runtimeRoot"`
	ConfigPath      string         `json:"configPath"`
	DataDir         string         `json:"dataDir"`
	LogDir          string         `json:"logDir"`
	ReportPath      string         `json:"reportPath"`
	User            string         `json:"user"`
	Group           string         `json:"group"`
	Server          serverConfig   `json:"server"`
	Database        databaseConfig `json:"database"`
	OpenBao         openBaoConfig  `json:"openBao"`
	AuthJWT         authJWTConfig  `json:"authJWT"`
}

type serverConfig struct {
	HTTPAddr string `json:"httpAddr"`
	HTTPPort int    `json:"httpPort"`
	Domain   string `json:"domain"`
	RootURL  string `json:"rootURL"`
}

type databaseConfig struct {
	Host    string `json:"host"`
	Name    string `json:"name"`
	User    string `json:"user"`
	SSLMode string `json:"sslMode"`
}

type openBaoConfig struct {
	Address          string    `json:"address"`
	CACert           string    `json:"caCert"`
	AdminPasswordRef objectRef `json:"adminPasswordRef"`
	SecretKeyRef     objectRef `json:"secretKeyRef"`
}

type authJWTConfig struct {
	Enabled       bool   `json:"enabled"`
	HeaderName    string `json:"headerName"`
	EmailClaim    string `json:"emailClaim"`
	UsernameClaim string `json:"usernameClaim"`
	JWKSetURL     string `json:"jwkSetURL"`
	CacheTTL      string `json:"cacheTTL"`
	AutoSignUp    bool   `json:"autoSignUp"`
}

type secretPathSpec struct {
	Path     string        `json:"path"`
	Key      string        `json:"key"`
	Source   string        `json:"source"`
	Generate *generateSpec `json:"generate"`
}

type generateSpec struct {
	Bytes    int    `json:"bytes"`
	Encoding string `json:"encoding"`
}

type loadedConfig struct {
	cfg                 config
	adminPasswordSecret secretPathSpec
	secretKeySecret     secretPathSpec
}

type runtimeSecrets struct {
	adminPassword string
	secretKey     string
}

type condition struct {
	Type     string `json:"type"`
	Status   string `json:"status"`
	Reason   string `json:"reason"`
	Resource string `json:"resource"`
	Message  string `json:"message,omitempty"`
}

type report struct {
	Component             string      `json:"component"`
	ResourceName          string      `json:"resourceName"`
	RuntimeArtifactDigest string      `json:"runtimeArtifactDigest,omitempty"`
	Conditions            []condition `json:"conditions"`
}

type userCredentials struct {
	uid    int
	gid    int
	groups []int
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "grafana-recover: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: grafana-recover <recover|server> [flags]")
	}
	switch args[0] {
	case "recover":
		opts, loaded, err := loadValidated(args[1:])
		if err != nil {
			return err
		}
		if os.Geteuid() != 0 {
			return errors.New("recover must run as root")
		}
		digest, err := installRuntime(opts.repoRoot, loaded.cfg)
		if err != nil {
			return err
		}
		if err := ensureAccount(loaded.cfg); err != nil {
			return err
		}
		if err := prepareDirectories(loaded.cfg); err != nil {
			return err
		}
		if _, err := readSecretsWithRetry(opts, loaded); err != nil {
			return err
		}
		if err := writeConfig(loaded.cfg); err != nil {
			return err
		}
		rep := report{
			Component:             "grafana",
			ResourceName:          opts.resourceName,
			RuntimeArtifactDigest: digest,
			Conditions: []condition{
				conditionTrue(opts.resourceName, "GrafanaRuntimeInstalled", "RuntimeReady", "repo-built Grafana runtime is installed"),
				conditionTrue(opts.resourceName, "GrafanaSecretsReady", "SecretsReady", "Grafana runtime secrets are available in OpenBao"),
				conditionTrue(opts.resourceName, "GrafanaConfigWritten", "ConfigReady", "Grafana configuration is written"),
				conditionTrue(opts.resourceName, "GrafanaRecoveryComplete", "Recovered", "Grafana is ready for Nomad to start"),
			},
		}
		return writeReport(loaded.cfg.ReportPath, rep)
	case "server":
		opts, loaded, err := loadValidated(args[1:])
		if err != nil {
			return err
		}
		if os.Geteuid() != 0 {
			return errors.New("server must run as root")
		}
		secrets, err := readSecretsWithRetry(opts, loaded)
		if err != nil {
			return err
		}
		return execServer(loaded.cfg, secrets)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func loadValidated(args []string) (options, loadedConfig, error) {
	opts, err := parseOptions(args)
	if err != nil {
		return options{}, loadedConfig{}, err
	}
	loaded, err := loadConfig(opts)
	if err != nil {
		return options{}, loadedConfig{}, err
	}
	if err := validateConfig(loaded.cfg); err != nil {
		return options{}, loadedConfig{}, err
	}
	return opts, loaded, nil
}

func parseOptions(args []string) (options, error) {
	opts := options{
		repoRoot:     defaultRepoRoot,
		resourceName: defaultResource,
	}
	fs := flag.NewFlagSet("grafana-recover", flag.ContinueOnError)
	fs.StringVar(&opts.repoRoot, "repo-root", opts.repoRoot, "Boarded repo root.")
	fs.StringVar(&opts.resourceGraph, "resource-graph", "", "Guardian resource graph document path.")
	fs.StringVar(&opts.resourceName, "resource-name", opts.resourceName, "GrafanaInstance resource name.")
	fs.StringVar(&opts.openBaoTokenFile, "openbao-token-file", "", "Nomad-provided OpenBao token file.")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	opts.repoRoot = strings.TrimSpace(opts.repoRoot)
	opts.resourceGraph = strings.TrimSpace(opts.resourceGraph)
	opts.resourceName = strings.TrimSpace(opts.resourceName)
	opts.openBaoTokenFile = strings.TrimSpace(opts.openBaoTokenFile)
	if opts.repoRoot == "" || opts.resourceName == "" || opts.openBaoTokenFile == "" {
		return options{}, errors.New("--repo-root, --resource-name, and --openbao-token-file are required")
	}
	repoRoot, err := filepath.Abs(opts.repoRoot)
	if err != nil {
		return options{}, fmt.Errorf("resolve repo root: %w", err)
	}
	opts.repoRoot = repoRoot
	if opts.resourceGraph == "" {
		opts.resourceGraph = filepath.Join(opts.repoRoot, "workspace/.guardian/fly/document.json")
	}
	return opts, nil
}

func loadConfig(opts options) (loadedConfig, error) {
	body, err := os.ReadFile(opts.resourceGraph)
	if err != nil {
		return loadedConfig{}, fmt.Errorf("read Guardian resource graph: %w", err)
	}
	var doc document
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return loadedConfig{}, fmt.Errorf("decode Guardian resource graph: %w", err)
	}
	resources := map[string]resource{}
	for _, resource := range doc.Resources {
		key := resourceKey(resource.APIVersion, resource.Kind, resource.Metadata.Name)
		if _, exists := resources[key]; exists {
			return loadedConfig{}, fmt.Errorf("Guardian resource graph duplicates %s", key)
		}
		resources[key] = resource
	}
	instance, ok := resources[resourceKey(apiVersion, kind, opts.resourceName)]
	if !ok {
		return loadedConfig{}, fmt.Errorf("Guardian resource graph missing %s %q", kind, opts.resourceName)
	}
	var cfg config
	instanceDecoder := json.NewDecoder(bytes.NewReader(instance.Spec))
	instanceDecoder.DisallowUnknownFields()
	if err := instanceDecoder.Decode(&cfg); err != nil {
		return loadedConfig{}, fmt.Errorf("decode GrafanaInstance spec: %w", err)
	}
	admin, err := loadSecretRef(resources, cfg.OpenBao.AdminPasswordRef, "adminPasswordRef")
	if err != nil {
		return loadedConfig{}, err
	}
	secretKey, err := loadSecretRef(resources, cfg.OpenBao.SecretKeyRef, "secretKeyRef")
	if err != nil {
		return loadedConfig{}, err
	}
	return loadedConfig{cfg: cfg, adminPasswordSecret: admin, secretKeySecret: secretKey}, nil
}

func loadSecretRef(resources map[string]resource, ref objectRef, field string) (secretPathSpec, error) {
	resource, ok := resources[resourceKey(ref.APIVersion, ref.Kind, ref.Name)]
	if !ok {
		return secretPathSpec{}, fmt.Errorf("Guardian resource graph missing Grafana %s %s/%s/%s", field, ref.APIVersion, ref.Kind, ref.Name)
	}
	if resource.APIVersion != "openbao.guardianintelligence.org/v1alpha1" || resource.Kind != "SecretPath" {
		return secretPathSpec{}, fmt.Errorf("GrafanaInstance.spec.openBao.%s must target openbao.guardianintelligence.org/v1alpha1/SecretPath", field)
	}
	var secret secretPathSpec
	decoder := json.NewDecoder(bytes.NewReader(resource.Spec))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&secret); err != nil {
		return secretPathSpec{}, fmt.Errorf("decode Grafana SecretPath %q: %w", resource.Metadata.Name, err)
	}
	if err := validateSecretPath(secret); err != nil {
		return secretPathSpec{}, fmt.Errorf("Grafana SecretPath %q: %w", resource.Metadata.Name, err)
	}
	return secret, nil
}

func resourceKey(apiVersion string, kind string, name string) string {
	return apiVersion + "/" + kind + "/" + name
}

func validateConfig(cfg config) error {
	for name, value := range map[string]string{
		"runtimeArtifact":            cfg.RuntimeArtifact,
		"runtimeRoot":                cfg.RuntimeRoot,
		"configPath":                 cfg.ConfigPath,
		"dataDir":                    cfg.DataDir,
		"logDir":                     cfg.LogDir,
		"reportPath":                 cfg.ReportPath,
		"user":                       cfg.User,
		"group":                      cfg.Group,
		"server.httpAddr":            cfg.Server.HTTPAddr,
		"server.domain":              cfg.Server.Domain,
		"server.rootURL":             cfg.Server.RootURL,
		"database.host":              cfg.Database.Host,
		"database.name":              cfg.Database.Name,
		"database.user":              cfg.Database.User,
		"database.sslMode":           cfg.Database.SSLMode,
		"openBao.address":            cfg.OpenBao.Address,
		"openBao.caCert":             cfg.OpenBao.CACert,
		"openBao.adminPassword.name": cfg.OpenBao.AdminPasswordRef.Name,
		"openBao.secretKey.name":     cfg.OpenBao.SecretKeyRef.Name,
		"authJWT.headerName":         cfg.AuthJWT.HeaderName,
		"authJWT.emailClaim":         cfg.AuthJWT.EmailClaim,
		"authJWT.usernameClaim":      cfg.AuthJWT.UsernameClaim,
		"authJWT.jwkSetURL":          cfg.AuthJWT.JWKSetURL,
		"authJWT.cacheTTL":           cfg.AuthJWT.CacheTTL,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("GrafanaInstance.spec.%s is required", name)
		}
	}
	if filepath.IsAbs(cfg.RuntimeArtifact) || strings.Contains(filepath.ToSlash(cfg.RuntimeArtifact), "../") {
		return errors.New("GrafanaInstance.spec.runtimeArtifact must be repo-relative")
	}
	for name, value := range map[string]string{
		"runtimeRoot":    cfg.RuntimeRoot,
		"configPath":     cfg.ConfigPath,
		"dataDir":        cfg.DataDir,
		"logDir":         cfg.LogDir,
		"reportPath":     cfg.ReportPath,
		"database.host":  cfg.Database.Host,
		"openBao.caCert": cfg.OpenBao.CACert,
	} {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("GrafanaInstance.spec.%s must be an absolute path", name)
		}
	}
	if cfg.Server.HTTPPort <= 0 || cfg.Server.HTTPPort > 65535 {
		return errors.New("GrafanaInstance.spec.server.httpPort must be between 1 and 65535")
	}
	if !strings.HasPrefix(cfg.Server.RootURL, "https://") {
		return errors.New("GrafanaInstance.spec.server.rootURL must use https")
	}
	if !strings.HasPrefix(cfg.OpenBao.Address, "http://") && !strings.HasPrefix(cfg.OpenBao.Address, "https://") {
		return errors.New("GrafanaInstance.spec.openBao.address must be an HTTP URL")
	}
	return nil
}

func validateSecretPath(secret secretPathSpec) error {
	if strings.TrimSpace(secret.Path) == "" || strings.TrimSpace(secret.Key) == "" || strings.TrimSpace(secret.Source) == "" {
		return errors.New("path, key, and source are required")
	}
	if secret.Source == "generated" {
		if secret.Generate == nil {
			return errors.New("generated SecretPath requires generate")
		}
		if secret.Generate.Bytes <= 0 {
			return errors.New("generated SecretPath bytes must be positive")
		}
		switch secret.Generate.Encoding {
		case "hex", "base64url", "alphanumeric":
			return nil
		default:
			return fmt.Errorf("unsupported generated encoding %q", secret.Generate.Encoding)
		}
	}
	return nil
}

func installRuntime(repoRoot string, cfg config) (string, error) {
	artifact := filepath.Join(repoRoot, filepath.FromSlash(cfg.RuntimeArtifact))
	digest, err := fileSHA256(artifact)
	if err != nil {
		return "", err
	}
	release := filepath.Join(cfg.RuntimeRoot, "releases", strings.ReplaceAll(digest, ":", "-"))
	if !runtimeInstalled(release) {
		if err := extractRuntimeTar(artifact, release); err != nil {
			return "", err
		}
	}
	if err := os.Chmod(release, 0o755); err != nil {
		return "", fmt.Errorf("chmod Grafana runtime release: %w", err)
	}
	if err := promoteRuntime(cfg.RuntimeRoot, release); err != nil {
		return "", err
	}
	return digest, nil
}

func runtimeInstalled(release string) bool {
	for _, rel := range []string{"bin/grafana-recover", "grafana/bin/grafana", "plugins/grafana-clickhouse-datasource"} {
		stat, err := os.Stat(filepath.Join(release, rel))
		if err != nil {
			return false
		}
		if rel == "plugins/grafana-clickhouse-datasource" {
			if !stat.IsDir() {
				return false
			}
			continue
		}
		if !stat.Mode().IsRegular() {
			return false
		}
	}
	return true
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open Grafana runtime artifact: %w", err)
	}
	defer func() { _ = file.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("hash Grafana runtime artifact: %w", err)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func extractRuntimeTar(artifact string, release string) error {
	if err := os.MkdirAll(filepath.Dir(release), 0o755); err != nil {
		return fmt.Errorf("create Grafana runtime release parent: %w", err)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(release), "."+filepath.Base(release)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create Grafana runtime staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := extractTar(artifact, tmp); err != nil {
		return err
	}
	if !runtimeInstalled(tmp) {
		return errors.New("Grafana runtime artifact missing grafana, plugin, or grafana-recover")
	}
	if err := os.RemoveAll(release); err != nil {
		return fmt.Errorf("remove stale Grafana runtime release: %w", err)
	}
	if err := os.Rename(tmp, release); err != nil {
		return fmt.Errorf("publish Grafana runtime release: %w", err)
	}
	return nil
}

func extractTar(artifact string, dest string) error {
	file, err := os.Open(artifact)
	if err != nil {
		return fmt.Errorf("open Grafana runtime artifact: %w", err)
	}
	defer func() { _ = file.Close() }()
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
			return fmt.Errorf("read Grafana runtime tar: %w", err)
		}
		target, err := safeTarTarget(destAbs, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			mode := directoryModeOrDefault(header.Mode)
			if err := os.MkdirAll(target, mode); err != nil {
				return fmt.Errorf("create runtime directory %s: %w", header.Name, err)
			}
			if err := os.Chmod(target, mode); err != nil {
				return fmt.Errorf("chmod runtime directory %s: %w", header.Name, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create runtime file parent %s: %w", header.Name, err)
			}
			if err := writeRegularFile(target, tr, modeOrDefault(header.Mode, 0o644)); err != nil {
				return fmt.Errorf("extract runtime file %s: %w", header.Name, err)
			}
		default:
			return fmt.Errorf("unsupported runtime tar entry %s type %d", header.Name, header.Typeflag)
		}
	}
}

func safeTarTarget(destAbs string, name string) (string, error) {
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("runtime tar entry %s is absolute", name)
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." {
		return destAbs, nil
	}
	if strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("runtime tar entry %s escapes destination", name)
	}
	target := filepath.Join(destAbs, clean)
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if targetAbs != destAbs && !strings.HasPrefix(targetAbs, destAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("runtime tar entry %s escapes destination", name)
	}
	return targetAbs, nil
}

func writeRegularFile(path string, r io.Reader, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, copyErr := io.Copy(file, r); copyErr != nil {
		_ = file.Close()
		return copyErr
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func directoryModeOrDefault(raw int64) os.FileMode {
	mode := modeOrDefault(raw, 0o755)
	if mode&0o400 != 0 {
		mode |= 0o100
	}
	if mode&0o040 != 0 {
		mode |= 0o010
	}
	if mode&0o004 != 0 {
		mode |= 0o001
	}
	if mode&0o111 == 0 {
		mode |= 0o111
	}
	return mode
}

func modeOrDefault(raw int64, fallback os.FileMode) os.FileMode {
	if raw <= 0 {
		return fallback
	}
	return os.FileMode(raw & 0o7777)
}

func promoteRuntime(root string, release string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create Grafana runtime root: %w", err)
	}
	next := filepath.Join(root, "current.next")
	current := filepath.Join(root, "current")
	_ = os.Remove(next)
	if err := os.Symlink(release, next); err != nil {
		return fmt.Errorf("create Grafana runtime symlink: %w", err)
	}
	if info, err := os.Lstat(current); err == nil && info.Mode()&os.ModeSymlink == 0 {
		if err := os.RemoveAll(current); err != nil {
			_ = os.Remove(next)
			return fmt.Errorf("remove old Grafana runtime current path: %w", err)
		}
	}
	if err := os.Rename(next, current); err != nil {
		_ = os.Remove(next)
		return fmt.Errorf("promote Grafana runtime symlink: %w", err)
	}
	return nil
}

func ensureAccount(cfg config) error {
	if err := ensureGroup(cfg.Group); err != nil {
		return err
	}
	if _, err := user.Lookup(cfg.User); err != nil {
		if _, ok := err.(user.UnknownUserError); !ok {
			return fmt.Errorf("lookup Grafana user: %w", err)
		}
		if err := command("/usr/sbin/useradd", "--system", "--gid", cfg.Group, "--home-dir", cfg.DataDir, "--shell", "/usr/sbin/nologin", "--no-create-home", cfg.User); err != nil {
			return err
		}
	}
	return nil
}

func ensureGroup(groupName string) error {
	if _, err := user.LookupGroup(groupName); err != nil {
		if _, ok := err.(user.UnknownGroupError); !ok {
			return fmt.Errorf("lookup group %s: %w", groupName, err)
		}
		if err := command("/usr/sbin/groupadd", "--system", groupName); err != nil {
			return err
		}
	}
	return nil
}

func prepareDirectories(cfg config) error {
	serviceUser, err := user.Lookup(cfg.User)
	if err != nil {
		return fmt.Errorf("lookup Grafana user: %w", err)
	}
	serviceGroup, err := user.LookupGroup(cfg.Group)
	if err != nil {
		return fmt.Errorf("lookup Grafana group: %w", err)
	}
	uid, err := parseID(serviceUser.Uid, "uid")
	if err != nil {
		return err
	}
	gid, err := parseID(serviceGroup.Gid, "gid")
	if err != nil {
		return err
	}
	for _, dir := range []struct {
		path string
		uid  int
		gid  int
		mode os.FileMode
	}{
		{cfg.DataDir, uid, gid, 0o750},
		{filepath.Join(cfg.DataDir, "provisioning"), uid, gid, 0o750},
		{filepath.Join(cfg.DataDir, "provisioning", "alerting"), uid, gid, 0o750},
		{filepath.Join(cfg.DataDir, "provisioning", "dashboards"), uid, gid, 0o750},
		{filepath.Join(cfg.DataDir, "provisioning", "datasources"), uid, gid, 0o750},
		{filepath.Join(cfg.DataDir, "provisioning", "plugins"), uid, gid, 0o750},
		{cfg.LogDir, uid, gid, 0o750},
		{filepath.Dir(cfg.ConfigPath), 0, gid, 0o750},
		{filepath.Dir(cfg.ReportPath), 0, 0, 0o755},
	} {
		if err := mkdirOwned(dir.path, dir.uid, dir.gid, dir.mode); err != nil {
			return err
		}
	}
	return nil
}

func mkdirOwned(path string, uid int, gid int, mode os.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("create directory %s: %w", path, err)
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("chown directory %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod directory %s: %w", path, err)
	}
	return nil
}

func writeConfig(cfg config) error {
	gid, err := lookupGroupID(cfg.Group)
	if err != nil {
		return err
	}
	body := []byte(renderConfig(cfg))
	return writeOwnedFile(cfg.ConfigPath, body, 0, gid, 0o640)
}

func renderConfig(cfg config) string {
	return fmt.Sprintf(`[paths]
data = %s
logs = %s
plugins = $__env{GF_PATHS_PLUGINS}
provisioning = %s

[server]
protocol = http
http_addr = %s
http_port = %d
domain = %s
root_url = %s
serve_from_sub_path = false

[database]
type = postgres
host = $__env{GF_DATABASE_HOST}
name = $__env{GF_DATABASE_NAME}
user = $__env{GF_DATABASE_USER}
ssl_mode = $__env{GF_DATABASE_SSL_MODE}

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
signout_redirect_url = %s/.pomerium/sign_out

[auth.jwt]
enabled = %t
header_name = %s
email_claim = %s
username_claim = %s
jwk_set_url = %s
cache_ttl = %s
auto_sign_up = %t
url_login = false

[auth.anonymous]
enabled = false

[log]
mode = console
level = info
`, cfg.DataDir, cfg.LogDir, filepath.Join(cfg.DataDir, "provisioning"), cfg.Server.HTTPAddr, cfg.Server.HTTPPort, cfg.Server.Domain, cfg.Server.RootURL, strings.TrimRight(cfg.Server.RootURL, "/"), cfg.AuthJWT.Enabled, cfg.AuthJWT.HeaderName, cfg.AuthJWT.EmailClaim, cfg.AuthJWT.UsernameClaim, cfg.AuthJWT.JWKSetURL, cfg.AuthJWT.CacheTTL, cfg.AuthJWT.AutoSignUp)
}

func writeOwnedFile(path string, body []byte, uid int, gid int, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent for %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary file for %s: %w", path, err)
	}
	if err := os.Chown(tmpPath, uid, gid); err != nil {
		return fmt.Errorf("chown temporary file for %s: %w", path, err)
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return fmt.Errorf("chmod temporary file for %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("promote %s: %w", path, err)
	}
	return nil
}

func lookupGroupID(groupName string) (int, error) {
	g, err := user.LookupGroup(groupName)
	if err != nil {
		return 0, fmt.Errorf("lookup group %s: %w", groupName, err)
	}
	gid, err := parseID(g.Gid, "gid")
	if err != nil {
		return 0, err
	}
	return gid, nil
}

func readSecretsWithRetry(opts options, loaded loadedConfig) (runtimeSecrets, error) {
	return retryValue("Grafana OpenBao secrets", 120*time.Second, func() (runtimeSecrets, error) {
		token, err := readTokenFile(opts.openBaoTokenFile)
		if err != nil {
			return runtimeSecrets{}, err
		}
		client, err := newOpenBaoClient(loaded.cfg.OpenBao)
		if err != nil {
			return runtimeSecrets{}, err
		}
		admin, err := client.readOrCreateSecret(token, loaded.adminPasswordSecret)
		if err != nil {
			return runtimeSecrets{}, err
		}
		secretKey, err := client.readOrCreateSecret(token, loaded.secretKeySecret)
		if err != nil {
			return runtimeSecrets{}, err
		}
		return runtimeSecrets{adminPassword: admin, secretKey: secretKey}, nil
	})
}

func readTokenFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open OpenBao token file: %w", err)
	}
	defer func() { _ = file.Close() }()
	body, err := io.ReadAll(io.LimitReader(file, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read OpenBao token file: %w", err)
	}
	token := strings.TrimSpace(string(body))
	if token == "" {
		return "", errors.New("OpenBao token file was empty")
	}
	return token, nil
}

type openBaoClient struct {
	address string
	client  *http.Client
}

func newOpenBaoClient(cfg openBaoConfig) (*openBaoClient, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("load system cert pool: %w", err)
	}
	if cfg.CACert != "" {
		body, err := os.ReadFile(cfg.CACert)
		if err != nil {
			return nil, fmt.Errorf("read OpenBao CA cert: %w", err)
		}
		if !pool.AppendCertsFromPEM(body) {
			return nil, fmt.Errorf("OpenBao CA cert %s did not contain a PEM certificate", cfg.CACert)
		}
	}
	return &openBaoClient{
		address: strings.TrimRight(cfg.Address, "/"),
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS13},
			},
		},
	}, nil
}

func (c *openBaoClient) readOrCreateSecret(token string, secret secretPathSpec) (string, error) {
	value, exists, err := c.readSecret(token, secret)
	if err != nil {
		return "", err
	}
	if exists {
		return value, nil
	}
	if secret.Source != "generated" || secret.Generate == nil {
		return "", fmt.Errorf("OpenBao secret %s is absent and source is %q", secret.Path, secret.Source)
	}
	generated, err := generatedSecretValue(*secret.Generate)
	if err != nil {
		return "", err
	}
	if err := c.writeSecret(token, secret, generated); err != nil {
		return "", err
	}
	return generated, nil
}

func (c *openBaoClient) readSecret(token string, secret secretPathSpec) (string, bool, error) {
	status, raw, err := c.apiRaw(token, http.MethodGet, strings.Trim(secret.Path, "/"), nil, "", http.StatusOK, http.StatusNotFound)
	if err != nil {
		return "", false, err
	}
	if status == http.StatusNotFound {
		return "", false, nil
	}
	var response struct {
		Data struct {
			Data map[string]any `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return "", false, fmt.Errorf("decode OpenBao secret %s: %w", secret.Path, err)
	}
	value, ok := response.Data.Data[secret.Key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", false, fmt.Errorf("OpenBao secret %s missing string key %q", secret.Path, secret.Key)
	}
	return value, true, nil
}

func (c *openBaoClient) writeSecret(token string, secret secretPathSpec, value string) error {
	body, err := json.Marshal(map[string]any{
		"data": map[string]string{secret.Key: value},
	})
	if err != nil {
		return fmt.Errorf("encode OpenBao secret write: %w", err)
	}
	_, _, err = c.apiRaw(token, http.MethodPost, strings.Trim(secret.Path, "/"), bytes.NewReader(body), "application/json", http.StatusOK, http.StatusNoContent)
	return err
}

func (c *openBaoClient) apiRaw(token string, method string, path string, body io.Reader, contentType string, expected ...int) (int, []byte, error) {
	req, err := http.NewRequest(method, c.address+"/v1/"+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("X-Vault-Token", token)
	if body != nil && contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("openbao %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if readErr != nil {
		return resp.StatusCode, nil, fmt.Errorf("read openbao %s %s response: %w", method, path, readErr)
	}
	for _, status := range expected {
		if resp.StatusCode == status {
			return resp.StatusCode, raw, nil
		}
	}
	return resp.StatusCode, raw, fmt.Errorf("openbao %s %s status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
}

func generatedSecretValue(spec generateSpec) (string, error) {
	if spec.Bytes <= 0 {
		return "", errors.New("generated secret bytes must be positive")
	}
	raw := make([]byte, spec.Bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate secret material: %w", err)
	}
	switch spec.Encoding {
	case "hex":
		return hex.EncodeToString(raw), nil
	case "base64url":
		return base64.RawURLEncoding.EncodeToString(raw), nil
	case "alphanumeric":
		return randomAlphanumeric(spec.Bytes)
	default:
		return "", fmt.Errorf("unsupported generated secret encoding %q", spec.Encoding)
	}
}

func randomAlphanumeric(length int) (string, error) {
	if length <= 0 {
		return "", errors.New("generated alphanumeric secret length must be positive")
	}
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	out := make([]byte, length)
	max := big.NewInt(int64(len(alphabet)))
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("generate alphanumeric secret material: %w", err)
		}
		out[i] = alphabet[n.Int64()]
	}
	return string(out), nil
}

func execServer(cfg config, secrets runtimeSecrets) error {
	command := []string{
		filepath.Join(cfg.RuntimeRoot, "current/grafana/bin/grafana"),
		"server",
		"--homepath",
		filepath.Join(cfg.RuntimeRoot, "current/grafana"),
		"--config",
		cfg.ConfigPath,
	}
	return execAsUserWithEnv(cfg.User, command, grafanaEnv(cfg, secrets))
}

func grafanaEnv(cfg config, secrets runtimeSecrets) []string {
	return []string{
		"HOME=" + cfg.DataDir,
		"GF_DATABASE_HOST=" + cfg.Database.Host,
		"GF_DATABASE_NAME=" + cfg.Database.Name,
		"GF_DATABASE_SSL_MODE=" + cfg.Database.SSLMode,
		"GF_DATABASE_USER=" + cfg.Database.User,
		"GF_PATHS_PLUGINS=" + filepath.Join(cfg.RuntimeRoot, "current/plugins"),
		"GF_SECURITY_ADMIN_PASSWORD=" + secrets.adminPassword,
		"GF_SECURITY_SECRET_KEY=" + secrets.secretKey,
		"OTEL_RESOURCE_ATTRIBUTES=verself.supervisor=nomad",
		"OTEL_SERVICE_NAME=grafana",
		"VERSELF_SUPERVISOR=nomad",
	}
}

func execAsUserWithEnv(userName string, command []string, env []string) error {
	if len(command) == 0 {
		return errors.New("command is required")
	}
	creds, err := lookupUserCredentials(userName)
	if err != nil {
		return err
	}
	if err := syscall.Setgroups(creds.groups); err != nil {
		return fmt.Errorf("set supplementary groups for %s: %w", userName, err)
	}
	if err := syscall.Setgid(creds.gid); err != nil {
		return fmt.Errorf("set gid for %s: %w", userName, err)
	}
	if err := syscall.Setuid(creds.uid); err != nil {
		return fmt.Errorf("set uid for %s: %w", userName, err)
	}
	return syscall.Exec(command[0], command, env)
}

func lookupUserCredentials(userName string) (userCredentials, error) {
	u, err := user.Lookup(userName)
	if err != nil {
		return userCredentials{}, fmt.Errorf("lookup user %s: %w", userName, err)
	}
	uid, err := parseID(u.Uid, "uid")
	if err != nil {
		return userCredentials{}, err
	}
	gid, err := parseID(u.Gid, "gid")
	if err != nil {
		return userCredentials{}, err
	}
	groupIDs, err := u.GroupIds()
	if err != nil {
		return userCredentials{}, fmt.Errorf("load supplementary groups for %s: %w", userName, err)
	}
	groups := make([]int, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		group, err := parseID(groupID, "supplementary gid")
		if err != nil {
			return userCredentials{}, err
		}
		groups = append(groups, group)
	}
	return userCredentials{uid: uid, gid: gid, groups: groups}, nil
}

func parseID(value string, label string) (int, error) {
	id, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s %s: %w", label, value, err)
	}
	return id, nil
}

func retry(label string, timeout time.Duration, op func() error) error {
	_, err := retryValue(label, timeout, func() (struct{}, error) {
		return struct{}{}, op()
	})
	return err
}

func retryValue[T any](label string, timeout time.Duration, op func() (T, error)) (T, error) {
	var zero T
	deadline := time.Now().Add(timeout)
	var last error
	for {
		value, err := op()
		if err == nil {
			return value, nil
		}
		last = err
		if time.Now().After(deadline) {
			return zero, fmt.Errorf("%s did not become ready: %w", label, last)
		}
		time.Sleep(2 * time.Second)
	}
}

func command(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("run %s: %w: %s", args[0], err, sanitizeOutput(output))
	}
	return nil
}

func sanitizeOutput(out []byte) string {
	text := strings.TrimSpace(string(out))
	if len(text) > 512 {
		return text[:512]
	}
	return text
}

func writeReport(path string, rep report) error {
	body, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Grafana report: %w", err)
	}
	body = append(body, '\n')
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("write Grafana report: %w", err)
	}
	return nil
}

func conditionTrue(resourceName string, t string, reason string, message string) condition {
	return condition{Type: t, Status: "True", Reason: reason, Resource: resourceName, Message: message}
}
