package main

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	apiVersion      = "verdaccio.guardianintelligence.org/v1alpha1"
	kind            = "VerdaccioRegistry"
	defaultRepoRoot = "/home/ubuntu/.local/state/guardian/repo"
	defaultResource = "verdaccio"
)

type options struct {
	repoRoot      string
	resourceGraph string
	resourceName  string
}

type document struct {
	Resources []resource `json:"resources"`
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
	RuntimeArtifact string              `json:"runtimeArtifact"`
	RuntimeRoot     string              `json:"runtimeRoot"`
	ConfigPath      string              `json:"configPath"`
	StorageDir      string              `json:"storageDir"`
	HtpasswdPath    string              `json:"htpasswdPath"`
	ReportPath      string              `json:"reportPath"`
	User            string              `json:"user"`
	Group           string              `json:"group"`
	Host            string              `json:"host"`
	Port            int                 `json:"port"`
	MaxBodySize     string              `json:"maxBodySize"`
	Uplink          uplinkConfig        `json:"uplink"`
	PackageFilter   packageFilterConfig `json:"packageFilter"`
	Log             logConfig           `json:"log"`
}

type uplinkConfig struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Cache     bool   `json:"cache"`
	MaxAge    string `json:"maxAge"`
	StrictSSL bool   `json:"strictSSL"`
}

type packageFilterConfig struct {
	MinAgeDays int `json:"minAgeDays"`
}

type logConfig struct {
	Level string `json:"level"`
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
		fmt.Fprintln(os.Stderr, "verdaccio-recover: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: verdaccio-recover <recover|server> [flags]")
	}
	switch args[0] {
	case "recover":
		opts, cfg, err := loadValidated(args[1:])
		if err != nil {
			return err
		}
		if os.Geteuid() != 0 {
			return errors.New("recover must run as root")
		}
		digest, err := installRuntime(opts.repoRoot, cfg)
		if err != nil {
			return err
		}
		if err := ensureAccount(cfg); err != nil {
			return err
		}
		if err := prepareDirectories(cfg); err != nil {
			return err
		}
		if err := ensureHtpasswd(cfg); err != nil {
			return err
		}
		if err := writeConfig(cfg); err != nil {
			return err
		}
		rep := report{
			Component:             "verdaccio",
			ResourceName:          opts.resourceName,
			RuntimeArtifactDigest: digest,
			Conditions: []condition{
				conditionTrue(opts.resourceName, "VerdaccioRuntimeInstalled", "RuntimeReady", "repo-built Verdaccio runtime is installed"),
				conditionTrue(opts.resourceName, "VerdaccioConfigWritten", "ConfigReady", "Verdaccio config and htpasswd are written"),
				conditionTrue(opts.resourceName, "VerdaccioRecoveryComplete", "Recovered", "Verdaccio is ready for Nomad to start"),
			},
		}
		return writeReport(cfg.ReportPath, rep)
	case "server":
		_, cfg, err := loadValidated(args[1:])
		if err != nil {
			return err
		}
		if os.Geteuid() != 0 {
			return errors.New("server must run as root")
		}
		return execServer(cfg)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func loadValidated(args []string) (options, config, error) {
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
	opts := options{
		repoRoot:     defaultRepoRoot,
		resourceName: defaultResource,
	}
	fs := flag.NewFlagSet("verdaccio-recover", flag.ContinueOnError)
	fs.StringVar(&opts.repoRoot, "repo-root", opts.repoRoot, "Boarded repo root.")
	fs.StringVar(&opts.resourceGraph, "resource-graph", "", "Guardian resource graph document path.")
	fs.StringVar(&opts.resourceName, "resource-name", opts.resourceName, "VerdaccioRegistry resource name.")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	opts.repoRoot = strings.TrimSpace(opts.repoRoot)
	opts.resourceGraph = strings.TrimSpace(opts.resourceGraph)
	opts.resourceName = strings.TrimSpace(opts.resourceName)
	if opts.resourceGraph == "" {
		opts.resourceGraph = filepath.Join(opts.repoRoot, "workspace/.guardian/fly/document.json")
	}
	if opts.repoRoot == "" || opts.resourceName == "" {
		return options{}, errors.New("--repo-root and --resource-name are required")
	}
	repoRoot, err := filepath.Abs(opts.repoRoot)
	if err != nil {
		return options{}, fmt.Errorf("resolve repo root: %w", err)
	}
	opts.repoRoot = repoRoot
	return opts, nil
}

func loadConfig(opts options) (config, error) {
	body, err := os.ReadFile(opts.resourceGraph)
	if err != nil {
		return config{}, fmt.Errorf("read Guardian resource graph: %w", err)
	}
	var doc document
	if err := json.Unmarshal(body, &doc); err != nil {
		return config{}, fmt.Errorf("decode Guardian resource graph: %w", err)
	}
	var matches []resource
	for _, resource := range doc.Resources {
		if resource.APIVersion == apiVersion && resource.Kind == kind && resource.Metadata.Name == opts.resourceName {
			matches = append(matches, resource)
		}
	}
	if len(matches) != 1 {
		return config{}, fmt.Errorf("expected exactly one %s resource named %q, found %d", kind, opts.resourceName, len(matches))
	}
	var cfg config
	if err := json.Unmarshal(matches[0].Spec, &cfg); err != nil {
		return config{}, fmt.Errorf("decode VerdaccioRegistry spec: %w", err)
	}
	return cfg, nil
}

func validateConfig(cfg config) error {
	for name, value := range map[string]string{
		"runtimeArtifact": cfg.RuntimeArtifact,
		"runtimeRoot":     cfg.RuntimeRoot,
		"configPath":      cfg.ConfigPath,
		"storageDir":      cfg.StorageDir,
		"htpasswdPath":    cfg.HtpasswdPath,
		"reportPath":      cfg.ReportPath,
		"user":            cfg.User,
		"group":           cfg.Group,
		"host":            cfg.Host,
		"maxBodySize":     cfg.MaxBodySize,
		"uplink.name":     cfg.Uplink.Name,
		"uplink.url":      cfg.Uplink.URL,
		"uplink.maxAge":   cfg.Uplink.MaxAge,
		"log.level":       cfg.Log.Level,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("VerdaccioRegistry.spec.%s is required", name)
		}
	}
	if filepath.IsAbs(cfg.RuntimeArtifact) || strings.Contains(filepath.ToSlash(cfg.RuntimeArtifact), "../") {
		return errors.New("VerdaccioRegistry.spec.runtimeArtifact must be repo-relative")
	}
	for name, value := range map[string]string{
		"runtimeRoot":  cfg.RuntimeRoot,
		"configPath":   cfg.ConfigPath,
		"storageDir":   cfg.StorageDir,
		"htpasswdPath": cfg.HtpasswdPath,
		"reportPath":   cfg.ReportPath,
	} {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("VerdaccioRegistry.spec.%s must be an absolute path", name)
		}
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return errors.New("VerdaccioRegistry.spec.port must be between 1 and 65535")
	}
	if !strings.HasPrefix(cfg.Uplink.URL, "https://") {
		return errors.New("VerdaccioRegistry.spec.uplink.url must use https")
	}
	if cfg.PackageFilter.MinAgeDays < 0 {
		return errors.New("VerdaccioRegistry.spec.packageFilter.minAgeDays must be non-negative")
	}
	return nil
}

func ensureAccount(cfg config) error {
	if err := ensureGroup(cfg.Group); err != nil {
		return err
	}
	if _, err := user.Lookup(cfg.User); err != nil {
		if _, ok := err.(user.UnknownUserError); !ok {
			return fmt.Errorf("lookup Verdaccio user: %w", err)
		}
		if err := command("/usr/sbin/useradd", "--system", "--gid", cfg.Group, "--home-dir", filepath.Dir(cfg.StorageDir), "--shell", "/usr/sbin/nologin", "--no-create-home", cfg.User); err != nil {
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
		return fmt.Errorf("lookup Verdaccio user: %w", err)
	}
	serviceGroup, err := user.LookupGroup(cfg.Group)
	if err != nil {
		return fmt.Errorf("lookup Verdaccio group: %w", err)
	}
	uid, err := parseID(serviceUser.Uid, "uid")
	if err != nil {
		return err
	}
	gid, err := parseID(serviceGroup.Gid, "gid")
	if err != nil {
		return err
	}
	for _, path := range []string{filepath.Dir(cfg.StorageDir), cfg.StorageDir} {
		if err := mkdirOwned(path, uid, gid, 0o750); err != nil {
			return err
		}
	}
	if err := mkdirOwned(filepath.Dir(cfg.ConfigPath), 0, gid, 0o750); err != nil {
		return err
	}
	if err := mkdirOwned(filepath.Dir(cfg.ReportPath), 0, 0, 0o755); err != nil {
		return err
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

func ensureHtpasswd(cfg config) error {
	if stat, err := os.Stat(cfg.HtpasswdPath); err == nil && stat.Mode().IsRegular() {
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat Verdaccio htpasswd: %w", err)
	}
	gid, err := lookupGroupID(cfg.Group)
	if err != nil {
		return err
	}
	return writeOwnedFile(cfg.HtpasswdPath, nil, 0, gid, 0o640)
}

func writeConfig(cfg config) error {
	gid, err := lookupGroupID(cfg.Group)
	if err != nil {
		return err
	}
	body := []byte(fmt.Sprintf(`storage: %s

auth:
  htpasswd:
    file: %s
    algorithm: bcrypt
    rounds: 10
    max_users: 0

web:
  enable: false

uplinks:
  %s:
    url: %s
    cache: %t
    maxage: %s
    strict_ssl: %t

packages:
  '**':
    access: $all
    publish: $nobody
    unpublish: $nobody
    proxy: %s

max_body_size: %s
listen: %s:%d

filters:
  '@verdaccio/package-filter':
    minAgeDays: %d

log:
  type: stdout
  format: json
  level: %s
  redact:
    paths:
      - req.header.authorization
      - req.header.cookie
    censor: '<redacted>'
`, cfg.StorageDir, cfg.HtpasswdPath, cfg.Uplink.Name, cfg.Uplink.URL, cfg.Uplink.Cache, cfg.Uplink.MaxAge, cfg.Uplink.StrictSSL, cfg.Uplink.Name, cfg.MaxBodySize, cfg.Host, cfg.Port, cfg.PackageFilter.MinAgeDays, cfg.Log.Level))
	return writeOwnedFile(cfg.ConfigPath, body, 0, gid, 0o640)
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
		return "", fmt.Errorf("chmod Verdaccio runtime release: %w", err)
	}
	if err := promoteRuntime(cfg.RuntimeRoot, release); err != nil {
		return "", err
	}
	return digest, nil
}

func runtimeInstalled(release string) bool {
	for _, rel := range []string{"bin/node", "bin/verdaccio-recover", "lib/node_modules/verdaccio/bin/verdaccio"} {
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
		return "", fmt.Errorf("open Verdaccio runtime artifact: %w", err)
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("hash Verdaccio runtime artifact: %w", err)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func extractRuntimeTar(artifact string, release string) error {
	if err := os.MkdirAll(filepath.Dir(release), 0o755); err != nil {
		return fmt.Errorf("create Verdaccio runtime release parent: %w", err)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(release), "."+filepath.Base(release)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create Verdaccio runtime staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := extractTar(artifact, tmp); err != nil {
		return err
	}
	nodeModulesTar := filepath.Join(tmp, "lib/node_modules.tar")
	if err := extractTar(nodeModulesTar, filepath.Join(tmp, "lib")); err != nil {
		return fmt.Errorf("extract Verdaccio node_modules: %w", err)
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return fmt.Errorf("chmod Verdaccio runtime staging directory: %w", err)
	}
	if !runtimeInstalled(tmp) {
		return errors.New("Verdaccio runtime artifact missing node, node_modules, or verdaccio-recover")
	}
	if err := os.RemoveAll(release); err != nil {
		return fmt.Errorf("remove stale Verdaccio runtime release: %w", err)
	}
	if err := os.Rename(tmp, release); err != nil {
		return fmt.Errorf("publish Verdaccio runtime release: %w", err)
	}
	return nil
}

func extractTar(artifact string, dest string) error {
	file, err := os.Open(artifact)
	if err != nil {
		return fmt.Errorf("open Verdaccio runtime artifact: %w", err)
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
			return fmt.Errorf("read Verdaccio runtime tar: %w", err)
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
				return fmt.Errorf("create runtime file parent %s: %w", header.Name, err)
			}
			if err := writeRegularFile(target, tr, modeOrDefault(header.Mode, 0o644)); err != nil {
				return fmt.Errorf("extract runtime file %s: %w", header.Name, err)
			}
		case tar.TypeSymlink:
			if _, err := safeLinkTarget(destAbs, target, header.Linkname); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create runtime symlink parent %s: %w", header.Name, err)
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				return fmt.Errorf("extract runtime symlink %s: %w", header.Name, err)
			}
		case tar.TypeLink:
			linkTarget, err := safeTarTarget(destAbs, header.Linkname)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create runtime hardlink parent %s: %w", header.Name, err)
			}
			if err := os.Link(linkTarget, target); err != nil {
				return fmt.Errorf("extract runtime hardlink %s: %w", header.Name, err)
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

func safeLinkTarget(destAbs string, target string, linkName string) (string, error) {
	if filepath.IsAbs(linkName) {
		return "", fmt.Errorf("runtime symlink %s points to absolute target %s", target, linkName)
	}
	linkTarget := filepath.Join(filepath.Dir(target), filepath.FromSlash(linkName))
	linkTargetAbs, err := filepath.Abs(linkTarget)
	if err != nil {
		return "", err
	}
	if linkTargetAbs != destAbs && !strings.HasPrefix(linkTargetAbs, destAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("runtime symlink %s escapes destination", target)
	}
	return linkTargetAbs, nil
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

func modeOrDefault(raw int64, fallback os.FileMode) os.FileMode {
	if raw <= 0 {
		return fallback
	}
	return os.FileMode(raw & 0o7777)
}

func promoteRuntime(root string, release string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create Verdaccio runtime root: %w", err)
	}
	next := filepath.Join(root, "current.next")
	current := filepath.Join(root, "current")
	_ = os.Remove(next)
	if err := os.Symlink(release, next); err != nil {
		return fmt.Errorf("create Verdaccio runtime symlink: %w", err)
	}
	if err := os.Rename(next, current); err != nil {
		return fmt.Errorf("promote Verdaccio runtime symlink: %w", err)
	}
	return nil
}

func execServer(cfg config) error {
	env := []string{
		"HOME=" + filepath.Dir(cfg.StorageDir),
	}
	return execAsUserWithEnv(cfg.User, []string{
		filepath.Join(cfg.RuntimeRoot, "current/bin/node"),
		filepath.Join(cfg.RuntimeRoot, "current/lib/node_modules/verdaccio/bin/verdaccio"),
		"--config",
		cfg.ConfigPath,
	}, env)
}

func execAsUserWithEnv(userName string, command []string, extraEnv []string) error {
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
	env := append(os.Environ(), extraEnv...)
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

func command(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func writeReport(path string, rep report) error {
	body, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Verdaccio report: %w", err)
	}
	body = append(body, '\n')
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("write Verdaccio report: %w", err)
	}
	return nil
}

func conditionTrue(resourceName string, t string, reason string, message string) condition {
	return condition{Type: t, Status: "True", Reason: reason, Resource: resourceName, Message: message}
}
