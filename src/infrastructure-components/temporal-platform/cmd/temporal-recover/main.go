package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	apiVersion      = "temporal.guardianintelligence.org/v1alpha1"
	kind            = "TemporalPlatform"
	defaultRepoRoot = "/home/ubuntu/.local/state/guardian/repo/current"
	defaultResource = "temporal"
)

type options struct {
	repoRoot      string
	resourceGraph string
	resourceName  string
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

type config struct {
	RuntimeArtifact string        `json:"runtimeArtifact"`
	RuntimeRoot     string        `json:"runtimeRoot"`
	StateDir        string        `json:"stateDir"`
	AuthConfigPath  string        `json:"authConfigPath"`
	ReportPath      string        `json:"reportPath"`
	User            string        `json:"user"`
	Group           string        `json:"group"`
	WorkloadGroup   string        `json:"workloadGroup"`
	Persistence     persistence   `json:"persistence"`
	Authorization   authorization `json:"authorization"`
	Bootstrap       bootstrap     `json:"bootstrap"`
}

type persistence struct {
	User               string `json:"user"`
	SocketDir          string `json:"socketDir"`
	DefaultDatabase    string `json:"defaultDatabase"`
	VisibilityDatabase string `json:"visibilityDatabase"`
	DefaultMaxConns    int    `json:"defaultMaxConns"`
	VisibilityMaxConns int    `json:"visibilityMaxConns"`
}

type authorization struct {
	SystemAdminIDs  []string        `json:"systemAdminIDs,omitempty"`
	SystemWriterIDs []string        `json:"systemWriterIDs,omitempty"`
	SystemReaderIDs []string        `json:"systemReaderIDs,omitempty"`
	NamespaceRoles  []namespaceRole `json:"namespaceRoles,omitempty"`
}

type namespaceRole struct {
	SPIFFEID  string `json:"spiffeID"`
	Namespace string `json:"namespace"`
	Role      string `json:"role"`
}

type bootstrap struct {
	Namespaces []bootstrapNamespace `json:"namespaces"`
}

type bootstrapNamespace struct {
	Name      string `json:"name"`
	Retention string `json:"retention"`
}

type report struct {
	Component      string   `json:"component"`
	Resource       string   `json:"resource"`
	RuntimeDigest  string   `json:"runtime_digest"`
	RuntimeRoot    string   `json:"runtime_root"`
	StateDir       string   `json:"state_dir"`
	AuthConfigPath string   `json:"auth_config_path"`
	ResourceGraph  string   `json:"resource_graph"`
	User           string   `json:"user"`
	Group          string   `json:"group"`
	WorkloadGroup  string   `json:"workload_group"`
	Databases      []string `json:"databases"`
	Namespaces     []string `json:"namespaces"`
	RecoveredAt    string   `json:"recovered_at"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "temporal-recover: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: temporal-recover recover [--repo-root PATH] [--resource-graph PATH] [--resource-name NAME]")
	}
	command := args[0]
	switch command {
	case "recover":
		opts, cfg, err := load(args[1:])
		if err != nil {
			return err
		}
		return recoverTemporal(opts, cfg)
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func load(args []string) (options, config, error) {
	opts, err := parseOptions(args)
	if err != nil {
		return options{}, config{}, err
	}
	cfg, err := loadConfig(opts)
	if err != nil {
		return options{}, config{}, err
	}
	if err := validateConfig(cfg); err != nil {
		return options{}, config{}, err
	}
	return opts, cfg, nil
}

func parseOptions(args []string) (options, error) {
	opts := options{repoRoot: defaultRepoRoot, resourceName: defaultResource}
	fs := flag.NewFlagSet("temporal-recover", flag.ContinueOnError)
	fs.StringVar(&opts.repoRoot, "repo-root", opts.repoRoot, "Boarded repo root.")
	fs.StringVar(&opts.resourceGraph, "resource-graph", "", "Guardian resource graph document path.")
	fs.StringVar(&opts.resourceName, "resource-name", opts.resourceName, "TemporalPlatform resource name.")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	opts.repoRoot = strings.TrimSpace(opts.repoRoot)
	opts.resourceGraph = strings.TrimSpace(opts.resourceGraph)
	opts.resourceName = strings.TrimSpace(opts.resourceName)
	if opts.repoRoot == "" || opts.resourceName == "" {
		return options{}, errors.New("--repo-root and --resource-name are required")
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

func loadConfig(opts options) (config, error) {
	body, err := os.ReadFile(opts.resourceGraph)
	if err != nil {
		return config{}, fmt.Errorf("read Guardian resource graph: %w", err)
	}
	var doc document
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return config{}, fmt.Errorf("decode Guardian resource graph: %w", err)
	}
	resources := map[string]resource{}
	for _, resource := range doc.Resources {
		key := resourceKey(resource.APIVersion, resource.Kind, resource.Metadata.Name)
		if _, exists := resources[key]; exists {
			return config{}, fmt.Errorf("Guardian resource graph duplicates %s", key)
		}
		resources[key] = resource
	}
	platform, ok := resources[resourceKey(apiVersion, kind, opts.resourceName)]
	if !ok {
		return config{}, fmt.Errorf("Guardian resource graph missing %s %q", kind, opts.resourceName)
	}
	var cfg config
	cfgDecoder := json.NewDecoder(bytes.NewReader(platform.Spec))
	cfgDecoder.DisallowUnknownFields()
	if err := cfgDecoder.Decode(&cfg); err != nil {
		return config{}, fmt.Errorf("decode TemporalPlatform spec: %w", err)
	}
	return cfg, nil
}

func resourceKey(apiVersion string, kind string, name string) string {
	return apiVersion + "/" + kind + "/" + name
}

func validateConfig(cfg config) error {
	for name, value := range map[string]string{
		"runtimeArtifact":                cfg.RuntimeArtifact,
		"runtimeRoot":                    cfg.RuntimeRoot,
		"stateDir":                       cfg.StateDir,
		"authConfigPath":                 cfg.AuthConfigPath,
		"reportPath":                     cfg.ReportPath,
		"user":                           cfg.User,
		"group":                          cfg.Group,
		"workloadGroup":                  cfg.WorkloadGroup,
		"persistence.user":               cfg.Persistence.User,
		"persistence.socketDir":          cfg.Persistence.SocketDir,
		"persistence.defaultDatabase":    cfg.Persistence.DefaultDatabase,
		"persistence.visibilityDatabase": cfg.Persistence.VisibilityDatabase,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("TemporalPlatform.spec.%s is required", name)
		}
	}
	if filepath.IsAbs(cfg.RuntimeArtifact) || strings.Contains(filepath.ToSlash(cfg.RuntimeArtifact), "../") {
		return errors.New("TemporalPlatform.spec.runtimeArtifact must be repo-relative")
	}
	for name, value := range map[string]string{
		"runtimeRoot":    cfg.RuntimeRoot,
		"stateDir":       cfg.StateDir,
		"authConfigPath": cfg.AuthConfigPath,
		"reportPath":     cfg.ReportPath,
		"socketDir":      cfg.Persistence.SocketDir,
	} {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("TemporalPlatform.spec.%s must be an absolute path", name)
		}
	}
	if cfg.Persistence.DefaultMaxConns <= 0 || cfg.Persistence.VisibilityMaxConns <= 0 {
		return errors.New("TemporalPlatform.spec.persistence connection limits must be positive")
	}
	if len(cfg.Bootstrap.Namespaces) == 0 {
		return errors.New("TemporalPlatform.spec.bootstrap.namespaces must contain at least one namespace")
	}
	for _, ns := range cfg.Bootstrap.Namespaces {
		if strings.TrimSpace(ns.Name) == "" || strings.TrimSpace(ns.Retention) == "" {
			return errors.New("TemporalPlatform.spec.bootstrap.namespaces entries require name and retention")
		}
	}
	for _, role := range cfg.Authorization.NamespaceRoles {
		if strings.TrimSpace(role.SPIFFEID) == "" || strings.TrimSpace(role.Namespace) == "" || strings.TrimSpace(role.Role) == "" {
			return errors.New("TemporalPlatform.spec.authorization.namespaceRoles entries require spiffeID, namespace, and role")
		}
	}
	return nil
}

func recoverTemporal(opts options, cfg config) error {
	if err := ensureGroup(cfg.Group); err != nil {
		return err
	}
	if err := ensureGroup(cfg.WorkloadGroup); err != nil {
		return err
	}
	if err := ensureUser(cfg); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.StateDir, 0o750); err != nil {
		return fmt.Errorf("create Temporal state directory: %w", err)
	}
	uid, gid, err := numericUserGroup(cfg.User, cfg.Group)
	if err != nil {
		return err
	}
	if err := os.Chown(cfg.StateDir, uid, gid); err != nil {
		return fmt.Errorf("chown Temporal state directory: %w", err)
	}
	digest, err := installRuntime(opts.repoRoot, cfg)
	if err != nil {
		return err
	}
	if err := writeJSONFile(cfg.AuthConfigPath, cfg.Authorization, 0o640); err != nil {
		return fmt.Errorf("write Temporal authorization config: %w", err)
	}
	if err := os.Chown(cfg.AuthConfigPath, uid, gid); err != nil {
		return fmt.Errorf("chown Temporal authorization config: %w", err)
	}
	projectedGraph := filepath.Join(filepath.Dir(cfg.ReportPath), "document.json")
	graphBody, err := os.ReadFile(opts.resourceGraph)
	if err != nil {
		return fmt.Errorf("read Guardian resource graph for projection: %w", err)
	}
	if err := writeFileAtomic(projectedGraph, graphBody, 0o640); err != nil {
		return fmt.Errorf("write Temporal resource graph projection: %w", err)
	}
	if err := os.Chown(projectedGraph, uid, gid); err != nil {
		return fmt.Errorf("chown Temporal resource graph projection: %w", err)
	}
	namespaces := make([]string, 0, len(cfg.Bootstrap.Namespaces))
	for _, namespace := range cfg.Bootstrap.Namespaces {
		namespaces = append(namespaces, namespace.Name)
	}
	rep := report{
		Component:      "temporal",
		Resource:       opts.resourceName,
		RuntimeDigest:  digest,
		RuntimeRoot:    cfg.RuntimeRoot,
		StateDir:       cfg.StateDir,
		AuthConfigPath: cfg.AuthConfigPath,
		ResourceGraph:  projectedGraph,
		User:           cfg.User,
		Group:          cfg.Group,
		WorkloadGroup:  cfg.WorkloadGroup,
		Databases:      []string{cfg.Persistence.DefaultDatabase, cfg.Persistence.VisibilityDatabase},
		Namespaces:     namespaces,
		RecoveredAt:    time.Now().UTC().Format(time.RFC3339),
	}
	if err := writeJSONFile(cfg.ReportPath, rep, 0o644); err != nil {
		return fmt.Errorf("write Temporal recovery report: %w", err)
	}
	return nil
}

func ensureGroup(groupName string) error {
	if commandQuiet("/usr/bin/getent", "group", groupName) == nil {
		return nil
	}
	if err := command("/usr/sbin/groupadd", "--system", groupName); err != nil {
		return fmt.Errorf("create group %s: %w", groupName, err)
	}
	return nil
}

func ensureUser(cfg config) error {
	if commandQuiet("/usr/bin/id", "-u", cfg.User) != nil {
		if err := command("/usr/sbin/useradd", "--system", "--gid", cfg.Group, "--groups", cfg.WorkloadGroup, "--home-dir", cfg.StateDir, "--shell", "/usr/sbin/nologin", "--no-create-home", cfg.User); err != nil {
			return fmt.Errorf("create user %s: %w", cfg.User, err)
		}
		return nil
	}
	if err := command("/usr/sbin/usermod", "--append", "--groups", cfg.WorkloadGroup, cfg.User); err != nil {
		return fmt.Errorf("ensure %s is in %s: %w", cfg.User, cfg.WorkloadGroup, err)
	}
	return nil
}

func numericUserGroup(userName string, groupName string) (int, int, error) {
	uidRaw, err := output("/usr/bin/id", "-u", userName)
	if err != nil {
		return -1, -1, fmt.Errorf("lookup uid for %s: %w", userName, err)
	}
	gidRaw, err := output("/usr/bin/getent", "group", groupName)
	if err != nil {
		return -1, -1, fmt.Errorf("lookup gid for %s: %w", groupName, err)
	}
	var uid int
	if _, err := fmt.Sscanf(strings.TrimSpace(uidRaw), "%d", &uid); err != nil {
		return -1, -1, fmt.Errorf("parse uid for %s: %w", userName, err)
	}
	parts := strings.Split(strings.TrimSpace(gidRaw), ":")
	if len(parts) < 3 {
		return -1, -1, fmt.Errorf("parse group entry for %s", groupName)
	}
	var gid int
	if _, err := fmt.Sscanf(parts[2], "%d", &gid); err != nil {
		return -1, -1, fmt.Errorf("parse gid for %s: %w", groupName, err)
	}
	return uid, gid, nil
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
		return "", fmt.Errorf("chmod Temporal runtime release: %w", err)
	}
	if err := promoteRuntime(cfg.RuntimeRoot, release); err != nil {
		return "", err
	}
	return digest, nil
}

func runtimeInstalled(release string) bool {
	for _, rel := range []string{"bin/temporal-bootstrap", "bin/temporal-recover", "bin/temporal-schema", "bin/temporal-server", "bin/verself-temporal-server"} {
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
		return "", fmt.Errorf("open Temporal runtime artifact: %w", err)
	}
	defer func() { _ = file.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("hash Temporal runtime artifact: %w", err)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func extractRuntimeTar(artifact string, release string) error {
	if err := os.MkdirAll(filepath.Dir(release), 0o755); err != nil {
		return fmt.Errorf("create Temporal runtime release parent: %w", err)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(release), "."+filepath.Base(release)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create Temporal runtime staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := extractTar(artifact, tmp); err != nil {
		return err
	}
	if !runtimeInstalled(tmp) {
		return errors.New("Temporal runtime artifact missing required binaries")
	}
	if err := os.RemoveAll(release); err != nil {
		return fmt.Errorf("remove stale Temporal runtime release: %w", err)
	}
	if err := os.Rename(tmp, release); err != nil {
		return fmt.Errorf("publish Temporal runtime release: %w", err)
	}
	return nil
}

func extractTar(artifact string, dest string) error {
	file, err := os.Open(artifact)
	if err != nil {
		return fmt.Errorf("open Temporal runtime artifact: %w", err)
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
			return fmt.Errorf("read Temporal runtime tar: %w", err)
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
		return fmt.Errorf("create Temporal runtime root: %w", err)
	}
	next := filepath.Join(root, "current.next")
	current := filepath.Join(root, "current")
	_ = os.Remove(next)
	if err := os.Symlink(release, next); err != nil {
		return fmt.Errorf("create Temporal runtime symlink: %w", err)
	}
	if info, err := os.Lstat(current); err == nil && info.Mode()&os.ModeSymlink == 0 {
		if err := os.RemoveAll(current); err != nil {
			return fmt.Errorf("remove non-symlink Temporal current path: %w", err)
		}
	}
	if err := os.Rename(next, current); err != nil {
		return fmt.Errorf("promote Temporal runtime: %w", err)
	}
	return nil
}

func writeJSONFile(path string, value any, mode os.FileMode) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return writeFileAtomic(path, body, mode)
}

func writeFileAtomic(path string, body []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func command(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func commandQuiet(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	return cmd.Run()
}

func output(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	body, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(body), nil
}
