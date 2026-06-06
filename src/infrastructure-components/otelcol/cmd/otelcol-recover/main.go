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
	apiVersion      = "otelcol.guardianintelligence.org/v1alpha1"
	kind            = "OtelCollector"
	defaultRepoRoot = "/home/ubuntu/.local/state/guardian/repo/current"
	defaultResource = "otelcol"
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
	RuntimeArtifact     string   `json:"runtimeArtifact"`
	ConfigArtifact      string   `json:"configArtifact"`
	RuntimeRoot         string   `json:"runtimeRoot"`
	ConfigRoot          string   `json:"configRoot"`
	ConfigPath          string   `json:"configPath"`
	HelperConfigPath    string   `json:"helperConfigPath"`
	DataDir             string   `json:"dataDir"`
	StorageDir          string   `json:"storageDir"`
	ClickHouseSPIFFEDir string   `json:"clickhouseSpiffeDir"`
	ReportPath          string   `json:"reportPath"`
	User                string   `json:"user"`
	Group               string   `json:"group"`
	SupplementaryGroups []string `json:"supplementaryGroups"`
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
	ConfigArtifactDigest  string      `json:"configArtifactDigest,omitempty"`
	Conditions            []condition `json:"conditions"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "otelcol-recover: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "recover" {
		args = args[1:]
	}
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(opts)
	if err != nil {
		return err
	}
	if err := validateConfig(cfg); err != nil {
		return err
	}
	if os.Geteuid() != 0 {
		return errors.New("otelcol-recover must run as root")
	}
	if err := ensureAccount(cfg); err != nil {
		return err
	}
	if err := prepareDirectories(cfg); err != nil {
		return err
	}
	runtimeDigest, err := installArtifact(opts.repoRoot, cfg.RuntimeArtifact, cfg.RuntimeRoot, []string{"bin/otelcol-contrib", "bin/spiffe-helper"})
	if err != nil {
		return err
	}
	configDigest, err := installArtifact(opts.repoRoot, cfg.ConfigArtifact, cfg.ConfigRoot, []string{
		"config/config.yaml",
		"config/clickhouse-spiffe-helper.conf",
	})
	if err != nil {
		return err
	}
	if err := ownTree(cfg.RuntimeRoot, cfg); err != nil {
		return err
	}
	if err := ownTree(cfg.ConfigRoot, cfg); err != nil {
		return err
	}
	rep := report{
		Component:             "otelcol",
		ResourceName:          opts.resourceName,
		RuntimeArtifactDigest: runtimeDigest,
		ConfigArtifactDigest:  configDigest,
		Conditions: []condition{
			conditionTrue("OtelCollectorRuntimeInstalled", "RuntimeReady", "repo-built OTel Collector runtime is installed"),
			conditionTrue("OtelCollectorConfigInstalled", "ConfigReady", "repo-built OTel Collector config is installed"),
			conditionTrue("OtelCollectorRecoveryComplete", "Recovered", "OTel Collector is ready for Nomad to start"),
		},
	}
	return writeReport(cfg.ReportPath, rep, cfg)
}

func parseOptions(args []string) (options, error) {
	opts := options{
		repoRoot:     defaultRepoRoot,
		resourceName: defaultResource,
	}
	fs := flag.NewFlagSet("otelcol-recover", flag.ContinueOnError)
	fs.StringVar(&opts.repoRoot, "repo-root", opts.repoRoot, "Boarded repo root.")
	fs.StringVar(&opts.resourceGraph, "resource-graph", "", "Guardian resource graph document path.")
	fs.StringVar(&opts.resourceName, "resource-name", opts.resourceName, "OtelCollector resource name.")
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
		return config{}, fmt.Errorf("decode OtelCollector spec: %w", err)
	}
	return cfg, nil
}

func validateConfig(cfg config) error {
	for name, value := range map[string]string{
		"runtimeArtifact":     cfg.RuntimeArtifact,
		"configArtifact":      cfg.ConfigArtifact,
		"runtimeRoot":         cfg.RuntimeRoot,
		"configRoot":          cfg.ConfigRoot,
		"configPath":          cfg.ConfigPath,
		"helperConfigPath":    cfg.HelperConfigPath,
		"dataDir":             cfg.DataDir,
		"storageDir":          cfg.StorageDir,
		"clickhouseSpiffeDir": cfg.ClickHouseSPIFFEDir,
		"reportPath":          cfg.ReportPath,
		"user":                cfg.User,
		"group":               cfg.Group,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("OtelCollector.spec.%s is required", name)
		}
	}
	for name, value := range map[string]string{
		"runtimeArtifact": cfg.RuntimeArtifact,
		"configArtifact":  cfg.ConfigArtifact,
	} {
		if filepath.IsAbs(value) || containsParent(value) {
			return fmt.Errorf("OtelCollector.spec.%s must be repo-relative", name)
		}
	}
	for name, value := range map[string]string{
		"runtimeRoot":         cfg.RuntimeRoot,
		"configRoot":          cfg.ConfigRoot,
		"configPath":          cfg.ConfigPath,
		"helperConfigPath":    cfg.HelperConfigPath,
		"dataDir":             cfg.DataDir,
		"storageDir":          cfg.StorageDir,
		"clickhouseSpiffeDir": cfg.ClickHouseSPIFFEDir,
		"reportPath":          cfg.ReportPath,
	} {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("OtelCollector.spec.%s must be an absolute path", name)
		}
	}
	return nil
}

func containsParent(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func ensureAccount(cfg config) error {
	if _, err := user.LookupGroup(cfg.Group); err != nil {
		if _, ok := err.(user.UnknownGroupError); !ok {
			return fmt.Errorf("lookup OTel Collector group: %w", err)
		}
		if err := command("/usr/sbin/groupadd", "--system", cfg.Group); err != nil {
			return err
		}
	}
	for _, group := range cfg.SupplementaryGroups {
		if _, err := user.LookupGroup(group); err != nil {
			return fmt.Errorf("lookup OTel Collector supplementary group %q: %w", group, err)
		}
	}
	groups := strings.Join(cfg.SupplementaryGroups, ",")
	if _, err := user.Lookup(cfg.User); err != nil {
		if _, ok := err.(user.UnknownUserError); !ok {
			return fmt.Errorf("lookup OTel Collector user: %w", err)
		}
		args := []string{"--system", "--gid", cfg.Group, "--home-dir", cfg.DataDir, "--shell", "/usr/sbin/nologin", "--create-home"}
		if groups != "" {
			args = append(args, "--groups", groups)
		}
		args = append(args, cfg.User)
		return command("/usr/sbin/useradd", args...)
	}
	args := []string{"--gid", cfg.Group}
	if groups != "" {
		args = append(args, "-a", "-G", groups)
	}
	args = append(args, cfg.User)
	return command("/usr/sbin/usermod", args...)
}

func prepareDirectories(cfg config) error {
	uid, gid, err := ids(cfg.User, cfg.Group)
	if err != nil {
		return err
	}
	rootDirs := []string{
		cfg.RuntimeRoot,
		cfg.ConfigRoot,
		filepath.Dir(cfg.ReportPath),
	}
	for _, dir := range rootDirs {
		if err := mkdirOwned(dir, 0, gid, 0o750); err != nil {
			return err
		}
	}
	userDirs := []string{
		cfg.DataDir,
		cfg.StorageDir,
		cfg.ClickHouseSPIFFEDir,
	}
	for _, dir := range userDirs {
		if err := mkdirOwned(dir, uid, gid, 0o750); err != nil {
			return err
		}
	}
	if err := os.Chmod(cfg.ClickHouseSPIFFEDir, 0o700); err != nil {
		return fmt.Errorf("chmod %s: %w", cfg.ClickHouseSPIFFEDir, err)
	}
	return nil
}

func mkdirOwned(path string, uid int, gid int, mode os.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

func installArtifact(repoRoot string, artifactPath string, root string, requiredFiles []string) (string, error) {
	artifact := filepath.Join(repoRoot, filepath.FromSlash(artifactPath))
	digest, err := fileSHA256(artifact)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create artifact root: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(root, ".install.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return "", fmt.Errorf("open artifact install lock: %w", err)
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return "", fmt.Errorf("lock artifact install: %w", err)
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	release := filepath.Join(root, "releases", strings.ReplaceAll(digest, ":", "-"))
	if !requiredInstalled(release, requiredFiles) {
		if err := extractArtifact(artifact, release, requiredFiles); err != nil {
			return "", err
		}
	}
	if err := promote(root, release); err != nil {
		return "", err
	}
	return digest, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open artifact: %w", err)
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("hash artifact: %w", err)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func requiredInstalled(root string, requiredFiles []string) bool {
	for _, required := range requiredFiles {
		stat, err := os.Stat(filepath.Join(root, filepath.FromSlash(required)))
		if err != nil || !stat.Mode().IsRegular() {
			return false
		}
	}
	return true
}

func extractArtifact(artifact string, release string, requiredFiles []string) error {
	if err := os.MkdirAll(filepath.Dir(release), 0o755); err != nil {
		return fmt.Errorf("create release parent: %w", err)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(release), "."+filepath.Base(release)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create artifact staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := extractTar(artifact, tmp); err != nil {
		return err
	}
	if !requiredInstalled(tmp, requiredFiles) {
		return fmt.Errorf("artifact missing required files: %s", strings.Join(requiredFiles, ", "))
	}
	if err := os.RemoveAll(release); err != nil {
		return fmt.Errorf("remove stale release: %w", err)
	}
	if err := os.Rename(tmp, release); err != nil {
		return fmt.Errorf("publish release: %w", err)
	}
	return nil
}

func extractTar(artifact string, dest string) error {
	file, err := os.Open(artifact)
	if err != nil {
		return fmt.Errorf("open artifact: %w", err)
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
			return fmt.Errorf("read artifact tar: %w", err)
		}
		target, err := safeTarTarget(destAbs, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, modeOrDefault(header.Mode, 0o755)); err != nil {
				return fmt.Errorf("create directory %s: %w", header.Name, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create parent %s: %w", header.Name, err)
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, modeOrDefault(header.Mode, 0o644))
			if err != nil {
				return fmt.Errorf("create file %s: %w", header.Name, err)
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return fmt.Errorf("write file %s: %w", header.Name, err)
			}
			if err := out.Close(); err != nil {
				return fmt.Errorf("close file %s: %w", header.Name, err)
			}
		case tar.TypeSymlink:
			if err := validateSymlink(header.Linkname); err != nil {
				return fmt.Errorf("unsafe symlink %s: %w", header.Name, err)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create symlink parent %s: %w", header.Name, err)
			}
			_ = os.Remove(target)
			if err := os.Symlink(header.Linkname, target); err != nil {
				return fmt.Errorf("create symlink %s: %w", header.Name, err)
			}
		default:
			return fmt.Errorf("unsupported artifact tar entry %s type %d", header.Name, header.Typeflag)
		}
	}
}

func safeTarTarget(destAbs string, name string) (string, error) {
	if filepath.IsAbs(name) || containsParent(name) {
		return "", fmt.Errorf("unsafe artifact tar entry %s", name)
	}
	target := filepath.Join(destAbs, name)
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if targetAbs != destAbs && !strings.HasPrefix(targetAbs, destAbs+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe artifact tar entry %s", name)
	}
	return targetAbs, nil
}

func validateSymlink(linkName string) error {
	if strings.TrimSpace(linkName) == "" {
		return errors.New("empty link target")
	}
	if filepath.IsAbs(linkName) || containsParent(linkName) {
		return fmt.Errorf("link target %q leaves artifact", linkName)
	}
	return nil
}

func modeOrDefault(mode int64, fallback os.FileMode) os.FileMode {
	if mode == 0 {
		return fallback
	}
	return os.FileMode(mode).Perm()
}

func promote(root string, release string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create artifact root: %w", err)
	}
	next := filepath.Join(root, "current.next")
	current := filepath.Join(root, "current")
	if err := os.Remove(next); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale current symlink: %w", err)
	}
	if err := os.Symlink(release, next); err != nil {
		return fmt.Errorf("create current symlink: %w", err)
	}
	if stat, err := os.Lstat(current); err == nil && stat.Mode()&os.ModeSymlink == 0 {
		if err := os.RemoveAll(current); err != nil {
			return fmt.Errorf("remove non-symlink current path: %w", err)
		}
	}
	if err := os.Rename(next, current); err != nil {
		return fmt.Errorf("publish current symlink: %w", err)
	}
	return nil
}

func ownTree(root string, cfg config) error {
	_, gid, err := ids(cfg.User, cfg.Group)
	if err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := os.Lchown(path, 0, gid); err != nil {
			return fmt.Errorf("chown %s: %w", path, err)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return os.Chmod(path, 0o755)
		}
		return nil
	})
}

func writeReport(path string, rep report, cfg config) error {
	body, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("encode OTel Collector report: %w", err)
	}
	body = append(body, '\n')
	_, gid, err := ids(cfg.User, cfg.Group)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp report: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write report: %w", err)
	}
	if err := tmp.Chmod(0o640); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod report: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close report: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("publish report: %w", err)
	}
	if err := os.Chown(path, 0, gid); err != nil {
		return fmt.Errorf("chown report: %w", err)
	}
	return nil
}

func ids(userName string, groupName string) (int, int, error) {
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
		return 0, 0, fmt.Errorf("parse uid %s: %w", u.Uid, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse gid %s: %w", g.Gid, err)
	}
	return uid, gid, nil
}

func conditionTrue(t string, reason string, message string) condition {
	return condition{Type: t, Status: "True", Reason: reason, Resource: "otelcol", Message: message}
}

func command(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
