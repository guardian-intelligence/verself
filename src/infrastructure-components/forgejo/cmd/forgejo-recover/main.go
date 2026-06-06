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
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	apiVersion      = "forgejo.guardianintelligence.org/v1alpha1"
	kind            = "ForgejoInstance"
	defaultRepoRoot = "/home/ubuntu/.local/state/guardian/repo/current"
	defaultResource = "forgejo"
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
	RuntimeArtifact string        `json:"runtimeArtifact"`
	RuntimeRoot     string        `json:"runtimeRoot"`
	ConfigPath      string        `json:"configPath"`
	WorkDir         string        `json:"workDir"`
	DataDir         string        `json:"dataDir"`
	LogDir          string        `json:"logDir"`
	RepositoriesDir string        `json:"repositoriesDir"`
	ReportPath      string        `json:"reportPath"`
	User            string        `json:"user"`
	Group           string        `json:"group"`
	Server          serverConfig  `json:"server"`
	OpenBao         openBaoConfig `json:"openBao"`
}

type serverConfig struct {
	HTTPAddr string `json:"httpAddr"`
	HTTPPort int    `json:"httpPort"`
	Domain   string `json:"domain"`
	RootURL  string `json:"rootURL"`
}

type openBaoConfig struct {
	Address            string    `json:"address"`
	CACert             string    `json:"caCert"`
	SecretKeyRef       objectRef `json:"secretKeyRef"`
	InternalTokenRef   objectRef `json:"internalTokenRef"`
	LFSJWTSecretRef    objectRef `json:"lfsJWTSecretRef"`
	OAuthJWTSecretRef  objectRef `json:"oauthJWTSecretRef"`
	AutomationTokenRef objectRef `json:"automationTokenRef"`
}

type secretPathSpec struct {
	Path        string        `json:"path"`
	Key         string        `json:"key"`
	Source      string        `json:"source"`
	Generate    *generateSpec `json:"generate"`
	ProducerRef *objectRef    `json:"producerRef"`
}

type generateSpec struct {
	Bytes    int    `json:"bytes"`
	Encoding string `json:"encoding"`
}

type loadedConfig struct {
	cfg                   config
	secretKeySecret       secretPathSpec
	internalTokenSecret   secretPathSpec
	lfsJWTSecret          secretPathSpec
	oauthJWTSecret        secretPathSpec
	automationTokenSecret secretPathSpec
}

type runtimeSecrets struct {
	secretKey      string
	internalToken  string
	lfsJWTSecret   string
	oauthJWTSecret string
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
		fmt.Fprintln(os.Stderr, "forgejo-recover: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: forgejo-recover <recover|server|automation-token> [flags]")
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
		secrets, err := readRuntimeSecretsWithRetry(opts, loaded)
		if err != nil {
			return err
		}
		if err := writeConfig(loaded.cfg); err != nil {
			return err
		}
		if err := runForgejoRecoverCommands(loaded.cfg, secrets); err != nil {
			return err
		}
		return writeReport(loaded.cfg.ReportPath, report{
			Component:             "forgejo",
			ResourceName:          opts.resourceName,
			RuntimeArtifactDigest: digest,
			Conditions: []condition{
				conditionTrue(opts.resourceName, "ForgejoRuntimeInstalled", "RuntimeReady", "repo-built Forgejo runtime is installed"),
				conditionTrue(opts.resourceName, "ForgejoSecretsReady", "SecretsReady", "Forgejo runtime secrets are available in OpenBao"),
				conditionTrue(opts.resourceName, "ForgejoConfigWritten", "ConfigReady", "Forgejo configuration is written"),
				conditionTrue(opts.resourceName, "ForgejoRecoveryComplete", "Recovered", "Forgejo is ready for Nomad to start"),
			},
		})
	case "server":
		opts, loaded, err := loadValidated(args[1:])
		if err != nil {
			return err
		}
		if os.Geteuid() != 0 {
			return errors.New("server must run as root")
		}
		secrets, err := readRuntimeSecretsWithRetry(opts, loaded)
		if err != nil {
			return err
		}
		if err := writeConfig(loaded.cfg); err != nil {
			return err
		}
		return execServer(loaded.cfg, secrets)
	case "automation-token":
		opts, loaded, err := loadValidated(args[1:])
		if err != nil {
			return err
		}
		if os.Geteuid() != 0 {
			return errors.New("automation-token must run as root")
		}
		return ensureAutomationToken(opts, loaded)
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
	opts := options{repoRoot: defaultRepoRoot, resourceName: defaultResource}
	fs := flag.NewFlagSet("forgejo-recover", flag.ContinueOnError)
	fs.StringVar(&opts.repoRoot, "repo-root", opts.repoRoot, "Boarded repo root.")
	fs.StringVar(&opts.resourceGraph, "resource-graph", "", "Guardian resource graph document path.")
	fs.StringVar(&opts.resourceName, "resource-name", opts.resourceName, "ForgejoInstance resource name.")
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
		return loadedConfig{}, fmt.Errorf("decode ForgejoInstance spec: %w", err)
	}
	secretKey, err := loadSecretRef(resources, cfg.OpenBao.SecretKeyRef, "secretKeyRef")
	if err != nil {
		return loadedConfig{}, err
	}
	internalToken, err := loadSecretRef(resources, cfg.OpenBao.InternalTokenRef, "internalTokenRef")
	if err != nil {
		return loadedConfig{}, err
	}
	lfsJWTSecret, err := loadSecretRef(resources, cfg.OpenBao.LFSJWTSecretRef, "lfsJWTSecretRef")
	if err != nil {
		return loadedConfig{}, err
	}
	oauthJWTSecret, err := loadSecretRef(resources, cfg.OpenBao.OAuthJWTSecretRef, "oauthJWTSecretRef")
	if err != nil {
		return loadedConfig{}, err
	}
	automationToken, err := loadSecretRef(resources, cfg.OpenBao.AutomationTokenRef, "automationTokenRef")
	if err != nil {
		return loadedConfig{}, err
	}
	return loadedConfig{cfg: cfg, secretKeySecret: secretKey, internalTokenSecret: internalToken, lfsJWTSecret: lfsJWTSecret, oauthJWTSecret: oauthJWTSecret, automationTokenSecret: automationToken}, nil
}

func loadSecretRef(resources map[string]resource, ref objectRef, field string) (secretPathSpec, error) {
	resource, ok := resources[resourceKey(ref.APIVersion, ref.Kind, ref.Name)]
	if !ok {
		return secretPathSpec{}, fmt.Errorf("Guardian resource graph missing Forgejo %s %s/%s/%s", field, ref.APIVersion, ref.Kind, ref.Name)
	}
	if resource.APIVersion != "openbao.guardianintelligence.org/v1alpha1" || resource.Kind != "SecretPath" {
		return secretPathSpec{}, fmt.Errorf("ForgejoInstance.spec.openBao.%s must target openbao.guardianintelligence.org/v1alpha1/SecretPath", field)
	}
	var secret secretPathSpec
	decoder := json.NewDecoder(bytes.NewReader(resource.Spec))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&secret); err != nil {
		return secretPathSpec{}, fmt.Errorf("decode Forgejo SecretPath %q: %w", resource.Metadata.Name, err)
	}
	if err := validateSecretPath(secret); err != nil {
		return secretPathSpec{}, fmt.Errorf("Forgejo SecretPath %q: %w", resource.Metadata.Name, err)
	}
	return secret, nil
}

func resourceKey(apiVersion string, kind string, name string) string {
	return apiVersion + "/" + kind + "/" + name
}

func validateConfig(cfg config) error {
	for name, value := range map[string]string{
		"runtimeArtifact":                 cfg.RuntimeArtifact,
		"runtimeRoot":                     cfg.RuntimeRoot,
		"configPath":                      cfg.ConfigPath,
		"workDir":                         cfg.WorkDir,
		"dataDir":                         cfg.DataDir,
		"logDir":                          cfg.LogDir,
		"repositoriesDir":                 cfg.RepositoriesDir,
		"reportPath":                      cfg.ReportPath,
		"user":                            cfg.User,
		"group":                           cfg.Group,
		"server.httpAddr":                 cfg.Server.HTTPAddr,
		"server.domain":                   cfg.Server.Domain,
		"server.rootURL":                  cfg.Server.RootURL,
		"openBao.address":                 cfg.OpenBao.Address,
		"openBao.caCert":                  cfg.OpenBao.CACert,
		"openBao.secretKeyRef.name":       cfg.OpenBao.SecretKeyRef.Name,
		"openBao.internalTokenRef.name":   cfg.OpenBao.InternalTokenRef.Name,
		"openBao.lfsJWTSecretRef.name":    cfg.OpenBao.LFSJWTSecretRef.Name,
		"openBao.oauthJWTSecretRef.name":  cfg.OpenBao.OAuthJWTSecretRef.Name,
		"openBao.automationTokenRef.name": cfg.OpenBao.AutomationTokenRef.Name,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("ForgejoInstance.spec.%s is required", name)
		}
	}
	if filepath.IsAbs(cfg.RuntimeArtifact) || strings.Contains(filepath.ToSlash(cfg.RuntimeArtifact), "../") {
		return errors.New("ForgejoInstance.spec.runtimeArtifact must be repo-relative")
	}
	for name, value := range map[string]string{
		"runtimeRoot":     cfg.RuntimeRoot,
		"configPath":      cfg.ConfigPath,
		"workDir":         cfg.WorkDir,
		"dataDir":         cfg.DataDir,
		"logDir":          cfg.LogDir,
		"repositoriesDir": cfg.RepositoriesDir,
		"reportPath":      cfg.ReportPath,
		"openBao.caCert":  cfg.OpenBao.CACert,
	} {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("ForgejoInstance.spec.%s must be an absolute path", name)
		}
	}
	if cfg.Server.HTTPPort <= 0 || cfg.Server.HTTPPort > 65535 {
		return errors.New("ForgejoInstance.spec.server.httpPort must be between 1 and 65535")
	}
	if !strings.HasPrefix(cfg.Server.RootURL, "https://") {
		return errors.New("ForgejoInstance.spec.server.rootURL must use https")
	}
	if !strings.HasPrefix(cfg.OpenBao.Address, "http://") && !strings.HasPrefix(cfg.OpenBao.Address, "https://") {
		return errors.New("ForgejoInstance.spec.openBao.address must be an HTTP URL")
	}
	return nil
}

func validateSecretPath(secret secretPathSpec) error {
	if strings.TrimSpace(secret.Path) == "" || strings.TrimSpace(secret.Key) == "" || strings.TrimSpace(secret.Source) == "" {
		return errors.New("path, key, and source are required")
	}
	switch secret.Source {
	case "generated":
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
	case "producedBy":
		if secret.ProducerRef == nil {
			return errors.New("producedBy SecretPath requires producerRef")
		}
		return nil
	default:
		return fmt.Errorf("unsupported SecretPath source %q", secret.Source)
	}
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
		return "", fmt.Errorf("chmod Forgejo runtime release: %w", err)
	}
	if err := promoteRuntime(cfg.RuntimeRoot, release); err != nil {
		return "", err
	}
	return digest, nil
}

func runtimeInstalled(release string) bool {
	for _, rel := range []string{"bin/forgejo", "bin/forgejo-recover", "bin/git", "bin/sqlite3"} {
		stat, err := os.Stat(filepath.Join(release, rel))
		if err != nil || !stat.Mode().IsRegular() {
			return false
		}
	}
	return true
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open Forgejo runtime artifact: %w", err)
	}
	defer func() { _ = file.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("hash Forgejo runtime artifact: %w", err)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func extractRuntimeTar(artifact string, release string) error {
	if err := os.MkdirAll(filepath.Dir(release), 0o755); err != nil {
		return fmt.Errorf("create Forgejo runtime release parent: %w", err)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(release), "."+filepath.Base(release)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create Forgejo runtime staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := extractTar(artifact, tmp); err != nil {
		return err
	}
	if !runtimeInstalled(tmp) {
		return errors.New("Forgejo runtime artifact missing forgejo, forgejo-recover, git, or sqlite3")
	}
	if err := os.RemoveAll(release); err != nil {
		return fmt.Errorf("remove stale Forgejo runtime release: %w", err)
	}
	if err := os.Rename(tmp, release); err != nil {
		return fmt.Errorf("publish Forgejo runtime release: %w", err)
	}
	return nil
}

func extractTar(artifact string, dest string) error {
	file, err := os.Open(artifact)
	if err != nil {
		return fmt.Errorf("open Forgejo runtime artifact: %w", err)
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
			return fmt.Errorf("read Forgejo runtime tar: %w", err)
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
		return fmt.Errorf("create Forgejo runtime root: %w", err)
	}
	next := filepath.Join(root, "current.next")
	current := filepath.Join(root, "current")
	_ = os.Remove(next)
	if err := os.Symlink(release, next); err != nil {
		return fmt.Errorf("create Forgejo runtime symlink: %w", err)
	}
	if info, err := os.Lstat(current); err == nil && info.Mode()&os.ModeSymlink == 0 {
		if err := os.RemoveAll(current); err != nil {
			_ = os.Remove(next)
			return fmt.Errorf("remove old Forgejo runtime current path: %w", err)
		}
	}
	if err := os.Rename(next, current); err != nil {
		_ = os.Remove(next)
		return fmt.Errorf("promote Forgejo runtime symlink: %w", err)
	}
	return nil
}

func ensureAccount(cfg config) error {
	if err := ensureGroup(cfg.Group); err != nil {
		return err
	}
	if _, err := user.Lookup(cfg.User); err != nil {
		if _, ok := err.(user.UnknownUserError); !ok {
			return fmt.Errorf("lookup Forgejo user: %w", err)
		}
		if err := command("/usr/sbin/useradd", "--system", "--gid", cfg.Group, "--home-dir", cfg.WorkDir, "--shell", "/bin/bash", "--create-home", cfg.User); err != nil {
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
		return fmt.Errorf("lookup Forgejo user: %w", err)
	}
	serviceGroup, err := user.LookupGroup(cfg.Group)
	if err != nil {
		return fmt.Errorf("lookup Forgejo group: %w", err)
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
		{cfg.WorkDir, uid, gid, 0o750},
		{cfg.DataDir, uid, gid, 0o750},
		{cfg.LogDir, uid, gid, 0o750},
		{cfg.RepositoriesDir, uid, gid, 0o750},
		{filepath.Join(cfg.DataDir, "lfs"), uid, gid, 0o750},
		{filepath.Dir(cfg.ConfigPath), uid, gid, 0o750},
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

func readRuntimeSecretsWithRetry(opts options, loaded loadedConfig) (runtimeSecrets, error) {
	return retryValue("Forgejo OpenBao secrets", 120*time.Second, func() (runtimeSecrets, error) {
		token, err := readTokenFile(opts.openBaoTokenFile)
		if err != nil {
			return runtimeSecrets{}, err
		}
		client, err := newOpenBaoClient(loaded.cfg.OpenBao)
		if err != nil {
			return runtimeSecrets{}, err
		}
		secretKey, err := client.readOrCreateSecret(token, loaded.secretKeySecret)
		if err != nil {
			return runtimeSecrets{}, err
		}
		internalToken, err := client.readOrCreateSecret(token, loaded.internalTokenSecret)
		if err != nil {
			return runtimeSecrets{}, err
		}
		lfsJWTSecret, err := client.readOrCreateSecret(token, loaded.lfsJWTSecret)
		if err != nil {
			return runtimeSecrets{}, err
		}
		oauthJWTSecret, err := client.readOrCreateSecret(token, loaded.oauthJWTSecret)
		if err != nil {
			return runtimeSecrets{}, err
		}
		return runtimeSecrets{secretKey: secretKey, internalToken: internalToken, lfsJWTSecret: lfsJWTSecret, oauthJWTSecret: oauthJWTSecret}, nil
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
	body, err := json.Marshal(map[string]any{"data": map[string]string{secret.Key: value}})
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
		return "", fmt.Errorf("unsupported generated encoding %q", spec.Encoding)
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

func writeConfig(cfg config) error {
	creds, err := lookupUserCredentials(cfg.User)
	if err != nil {
		return err
	}
	return writeOwnedFile(cfg.ConfigPath, []byte(renderConfig(cfg)), creds.uid, creds.gid, 0o600)
}

func renderConfig(cfg config) string {
	return fmt.Sprintf(`APP_NAME = Verself
RUN_MODE = prod
RUN_USER = %s
WORK_PATH = %s

[server]
DOMAIN = %s
ROOT_URL = %s
HTTP_PORT = %d
HTTP_ADDR = %s
SSH_DOMAIN = %s
DISABLE_SSH = true
START_SSH_SERVER = false
LFS_START_SERVER = true
OFFLINE_MODE = true
LFS_JWT_SECRET_URI = file:/proc/self/fd/5

[database]
DB_TYPE = sqlite3
PATH = %s

[repository]
ROOT = %s
DEFAULT_BRANCH = main

[git]
PATH = %s

[lfs]
PATH = %s

[log]
MODE = console
LEVEL = info
ROOT_PATH = %s

[security]
INSTALL_LOCK = true
SECRET_KEY_URI = file:/proc/self/fd/3
INTERNAL_TOKEN_URI = file:/proc/self/fd/4

[service]
DISABLE_REGISTRATION = true
ALLOW_ONLY_EXTERNAL_REGISTRATION = false
REQUIRE_SIGNIN_VIEW = true
ENABLE_BASIC_AUTHENTICATION = false
ENABLE_INTERNAL_SIGNIN = false

[oauth2]
ENABLED = false
JWT_SECRET_URI = file:/proc/self/fd/6

[openid]
ENABLE_OPENID_SIGNIN = false
ENABLE_OPENID_SIGNUP = false

[actions]
ENABLED = true
`, cfg.User, cfg.WorkDir, cfg.Server.Domain, cfg.Server.RootURL, cfg.Server.HTTPPort, cfg.Server.HTTPAddr, cfg.Server.Domain, filepath.Join(cfg.DataDir, "forgejo.db"), cfg.RepositoriesDir, filepath.Join(cfg.RuntimeRoot, "current/bin/git"), filepath.Join(cfg.DataDir, "lfs"), cfg.LogDir)
}

func runForgejoRecoverCommands(cfg config, secrets runtimeSecrets) error {
	env := forgejoEnv(cfg)
	for _, argv := range [][]string{
		{forgejoBin(cfg), "migrate", "--config", cfg.ConfigPath, "--work-path", cfg.WorkDir},
	} {
		if _, err := runAsUserCaptureWithSecrets(cfg.User, env, argv, secrets); err != nil {
			return err
		}
	}
	return nil
}

func execServer(cfg config, secrets runtimeSecrets) error {
	return runAsUserForegroundWithSecrets(cfg.User, forgejoEnv(cfg), []string{forgejoBin(cfg), "web", "--config", cfg.ConfigPath}, secrets)
}

func ensureAutomationToken(opts options, loaded loadedConfig) error {
	token, err := readTokenFile(opts.openBaoTokenFile)
	if err != nil {
		return err
	}
	secrets, err := readRuntimeSecretsWithRetry(opts, loaded)
	if err != nil {
		return err
	}
	client, err := newOpenBaoClient(loaded.cfg.OpenBao)
	if err != nil {
		return err
	}
	if existing, exists, err := client.readSecret(token, loaded.automationTokenSecret); err != nil {
		return err
	} else if exists && existing != "" {
		return writeAutomationReport(opts, loaded, conditionTrue(opts.resourceName, "ForgejoAutomationTokenReady", "SecretAlreadyPresent", "Forgejo automation token is already present in OpenBao"))
	}
	if err := waitForgejoReady(loaded.cfg, secrets); err != nil {
		return err
	}
	if err := ensureAutomationUser(loaded.cfg, secrets); err != nil {
		return err
	}
	automationToken, err := generateAutomationToken(loaded.cfg, secrets)
	if err != nil {
		return err
	}
	if err := client.writeSecret(token, loaded.automationTokenSecret, automationToken); err != nil {
		return err
	}
	return writeAutomationReport(opts, loaded, conditionTrue(opts.resourceName, "ForgejoAutomationTokenReady", "SecretWritten", "Forgejo automation token is present in OpenBao"))
}

func writeAutomationReport(opts options, loaded loadedConfig, automationCondition condition) error {
	digest, err := fileSHA256(filepath.Join(opts.repoRoot, filepath.FromSlash(loaded.cfg.RuntimeArtifact)))
	if err != nil {
		return err
	}
	return writeReport(loaded.cfg.ReportPath, report{
		Component:             "forgejo",
		ResourceName:          opts.resourceName,
		RuntimeArtifactDigest: digest,
		Conditions: []condition{
			conditionTrue(opts.resourceName, "ForgejoRuntimeInstalled", "RuntimeReady", "repo-built Forgejo runtime is installed"),
			conditionTrue(opts.resourceName, "ForgejoSecretsReady", "SecretsReady", "Forgejo runtime secrets are available in OpenBao"),
			conditionTrue(opts.resourceName, "ForgejoConfigWritten", "ConfigReady", "Forgejo configuration is written"),
			conditionTrue(opts.resourceName, "ForgejoRecoveryComplete", "Recovered", "Forgejo is ready for Nomad to start"),
			automationCondition,
		},
	})
}

func waitForgejoReady(cfg config, secrets runtimeSecrets) error {
	var last error
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := runAsUserCaptureWithSecrets(cfg.User, forgejoEnv(cfg), []string{forgejoBin(cfg), "admin", "user", "list", "--config", cfg.ConfigPath}, secrets); err == nil {
			if _, err := os.Stat(filepath.Join(cfg.DataDir, "forgejo.db")); err == nil {
				return nil
			}
			last = err
		} else {
			last = err
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("Forgejo did not become ready for automation token setup: %w", last)
}

func ensureAutomationUser(cfg config, secrets runtimeSecrets) error {
	exists, err := sqliteValue(cfg, secrets, "SELECT 1 FROM user WHERE lower_name = 'forgejo-automation';")
	if err != nil {
		return err
	}
	if strings.TrimSpace(exists) != "1" {
		if _, err := runAsUserCaptureWithSecrets(cfg.User, forgejoEnv(cfg), []string{
			forgejoBin(cfg),
			"admin", "user", "create",
			"--config", cfg.ConfigPath,
			"--username", "forgejo-automation",
			"--email", "forgejo-automation@" + cfg.Server.Domain,
			"--fullname", "Forgejo Automation",
			"--random-password",
			"--random-password-length", "32",
		}, secrets); err != nil {
			return err
		}
	}
	_, err = runAsUserCaptureWithSecrets(cfg.User, forgejoEnv(cfg), []string{sqliteBin(cfg), filepath.Join(cfg.DataDir, "forgejo.db"), "UPDATE user SET is_admin = 1 WHERE lower_name = 'forgejo-automation';"}, secrets)
	return err
}

func generateAutomationToken(cfg config, secrets runtimeSecrets) (string, error) {
	out, err := runAsUserCaptureWithSecrets(cfg.User, forgejoEnv(cfg), []string{
		forgejoBin(cfg),
		"admin", "user", "generate-access-token",
		"--config", cfg.ConfigPath,
		"--username", "forgejo-automation",
		"--token-name", "verself-automation-" + strconv.FormatInt(time.Now().Unix(), 10),
		"--raw",
		"--scopes", "all",
	}, secrets)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", errors.New("Forgejo generated an empty automation token")
	}
	return token, nil
}

func sqliteValue(cfg config, secrets runtimeSecrets, query string) (string, error) {
	out, err := runAsUserCaptureWithSecrets(cfg.User, forgejoEnv(cfg), []string{sqliteBin(cfg), filepath.Join(cfg.DataDir, "forgejo.db"), query}, secrets)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func forgejoEnv(cfg config) []string {
	return []string{
		"HOME=" + cfg.WorkDir,
		"FORGEJO_WORK_DIR=" + cfg.WorkDir,
		"PATH=" + filepath.Join(cfg.RuntimeRoot, "current/bin") + ":/usr/bin:/bin",
		"OTEL_RESOURCE_ATTRIBUTES=verself.supervisor=nomad",
		"OTEL_SERVICE_NAME=forgejo",
		"VERSELF_SUPERVISOR=nomad",
	}
}

func forgejoBin(cfg config) string {
	return filepath.Join(cfg.RuntimeRoot, "current/bin/forgejo")
}

func sqliteBin(cfg config) string {
	return filepath.Join(cfg.RuntimeRoot, "current/bin/sqlite3")
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
	groups, err := u.GroupIds()
	if err != nil {
		return userCredentials{}, fmt.Errorf("lookup supplementary groups for %s: %w", userName, err)
	}
	out := userCredentials{uid: uid, gid: gid}
	for _, group := range groups {
		parsed, err := parseID(group, "supplementary gid")
		if err != nil {
			return userCredentials{}, err
		}
		out.groups = append(out.groups, parsed)
	}
	return out, nil
}

func runAsUserCapture(userName string, env []string, command []string) ([]byte, error) {
	return runAsUserCaptureWithExtraFiles(userName, env, command, nil)
}

func runAsUserCaptureWithSecrets(userName string, env []string, command []string, secrets runtimeSecrets) ([]byte, error) {
	creds, err := lookupUserCredentials(userName)
	if err != nil {
		return nil, err
	}
	files, err := secretTempFiles(secrets, creds.uid, creds.gid)
	if err != nil {
		return nil, err
	}
	defer closeFiles(files)
	return runAsUserCaptureWithExtraFiles(userName, env, command, files)
}

func runAsUserCaptureWithExtraFiles(userName string, env []string, command []string, extraFiles []*os.File) ([]byte, error) {
	if len(command) == 0 {
		return nil, errors.New("command is required")
	}
	creds, err := lookupUserCredentials(userName)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Env = mergedEnv(env)
	cmd.Dir = "/"
	cmd.ExtraFiles = extraFiles
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid:    uint32(creds.uid),
			Gid:    uint32(creds.gid),
			Groups: intSliceToUint32(creds.groups),
		},
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("run %s: %w: %s", filepath.Base(command[0]), err, sanitizeOutput(output))
	}
	return output, nil
}

func secretTempFiles(secrets runtimeSecrets, uid int, gid int) ([]*os.File, error) {
	if secrets.secretKey == "" || secrets.internalToken == "" || secrets.lfsJWTSecret == "" || secrets.oauthJWTSecret == "" {
		return nil, errors.New("Forgejo runtime secrets must be non-empty")
	}
	values := []struct {
		label string
		value string
	}{
		{"secret-key", secrets.secretKey},
		{"internal-token", secrets.internalToken},
		{"lfs-jwt-secret", secrets.lfsJWTSecret},
		{"oauth-jwt-secret", secrets.oauthJWTSecret},
	}
	files := make([]*os.File, 0, len(values))
	for _, item := range values {
		file, err := unlinkedTempFile(item.label, item.value, uid, gid)
		if err != nil {
			closeFiles(files)
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

func unlinkedTempFile(label string, value string, uid int, gid int) (*os.File, error) {
	file, err := os.CreateTemp("", ".forgejo-"+label+"-*")
	if err != nil {
		return nil, fmt.Errorf("create in-memory Forgejo %s file: %w", label, err)
	}
	path := file.Name()
	defer func() { _ = os.Remove(path) }()
	if _, err := io.WriteString(file, value); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("write in-memory Forgejo %s file: %w", label, err)
	}
	if err := file.Chown(uid, gid); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("chown in-memory Forgejo %s file: %w", label, err)
	}
	if err := file.Chmod(0o400); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("chmod in-memory Forgejo %s file: %w", label, err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("rewind in-memory Forgejo %s file: %w", label, err)
	}
	if err := os.Remove(path); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("unlink in-memory Forgejo %s file: %w", label, err)
	}
	return file, nil
}

func runAsUserForegroundWithSecrets(userName string, env []string, command []string, secrets runtimeSecrets) error {
	if len(command) == 0 {
		return errors.New("command is required")
	}
	creds, err := lookupUserCredentials(userName)
	if err != nil {
		return err
	}
	files, err := secretTempFiles(secrets, creds.uid, creds.gid)
	if err != nil {
		return err
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Env = mergedEnv(env)
	cmd.Dir = "/"
	cmd.ExtraFiles = files
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid:    uint32(creds.uid),
			Gid:    uint32(creds.gid),
			Groups: intSliceToUint32(creds.groups),
		},
	}
	if err := cmd.Start(); err != nil {
		closeFiles(files)
		return fmt.Errorf("start %s: %w", filepath.Base(command[0]), err)
	}
	closeFiles(files)

	signals := make(chan os.Signal, 8)
	stopForwarding := make(chan struct{})
	done := make(chan struct{})
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP, syscall.SIGQUIT)
	go func() {
		defer close(done)
		for {
			select {
			case sig := <-signals:
				if cmd.Process != nil {
					_ = cmd.Process.Signal(sig)
				}
			case <-stopForwarding:
				return
			}
		}
	}()
	err = cmd.Wait()
	signal.Stop(signals)
	close(stopForwarding)
	<-done
	if err != nil {
		return fmt.Errorf("run %s: %w", filepath.Base(command[0]), err)
	}
	return nil
}

func closeFiles(files []*os.File) {
	for _, file := range files {
		_ = file.Close()
	}
}

func intSliceToUint32(values []int) []uint32 {
	out := make([]uint32, 0, len(values))
	for _, value := range values {
		out = append(out, uint32(value))
	}
	return out
}

func mergedEnv(overrides []string) []string {
	overrideKeys := map[string]struct{}{}
	for _, item := range overrides {
		key, _, ok := strings.Cut(item, "=")
		if ok {
			overrideKeys[key] = struct{}{}
		}
	}
	out := make([]string, 0, len(os.Environ())+len(overrides))
	for _, item := range os.Environ() {
		key, _, ok := strings.Cut(item, "=")
		if ok {
			if _, overridden := overrideKeys[key]; overridden {
				continue
			}
		}
		out = append(out, item)
	}
	out = append(out, overrides...)
	return out
}

func parseID(raw string, label string) (int, error) {
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse %s %q: %w", label, raw, err)
	}
	if value < 0 {
		return 0, fmt.Errorf("%s %q must be non-negative", label, raw)
	}
	return int(value), nil
}

func retry(name string, timeout time.Duration, fn func() error) error {
	deadline := time.Now().Add(timeout)
	var last error
	for {
		if err := fn(); err == nil {
			return nil
		} else {
			last = err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s did not become ready: %w", name, last)
		}
		time.Sleep(time.Second)
	}
}

func retryValue[T any](name string, timeout time.Duration, fn func() (T, error)) (T, error) {
	var zero T
	var out T
	err := retry(name, timeout, func() error {
		value, err := fn()
		if err != nil {
			return err
		}
		out = value
		return nil
	})
	if err != nil {
		return zero, err
	}
	return out, nil
}

func command(args ...string) error {
	if len(args) == 0 {
		return errors.New("command is required")
	}
	cmd := exec.Command(args[0], args[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("run %s: %w: %s", args[0], err, sanitizeOutput(output))
	}
	return nil
}

func sanitizeOutput(out []byte) string {
	trimmed := strings.TrimSpace(string(out))
	if len(trimmed) > 4096 {
		return trimmed[:4096] + "...<truncated>"
	}
	return trimmed
}

func writeReport(path string, rep report) error {
	body, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Forgejo report: %w", err)
	}
	body = append(body, '\n')
	return writeOwnedFile(path, body, 0, 0, 0o644)
}

func conditionTrue(resource string, conditionType string, reason string, message string) condition {
	return condition{Type: conditionType, Status: "True", Reason: reason, Resource: resource, Message: message}
}
