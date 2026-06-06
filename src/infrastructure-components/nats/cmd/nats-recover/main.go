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
	"time"
)

const (
	apiVersion      = "nats.guardianintelligence.org/v1alpha1"
	kind            = "NATSCluster"
	defaultRepoRoot = "/home/ubuntu/.local/state/guardian/repo/current"
	defaultResource = "nats"
)

type options struct {
	repoRoot      string
	resourceGraph string
	resourceName  string
}

type execOptions struct {
	user        string
	waitFiles   stringList
	waitTimeout time.Duration
	command     []string
}

type stringList []string

func (l *stringList) String() string {
	return strings.Join(*l, ",")
}

func (l *stringList) Set(value string) error {
	value = strings.TrimSpace(value)
	if value != "" {
		*l = append(*l, value)
	}
	return nil
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
	RuntimeArtifact string `json:"runtimeArtifact"`
	RuntimeRoot     string `json:"runtimeRoot"`
	ConfigPath      string `json:"configPath"`
	HelperConfig    string `json:"helperConfigPath"`
	DataDir         string `json:"dataDir"`
	JetStreamDir    string `json:"jetStreamDir"`
	PIDPath         string `json:"pidPath"`
	ReportPath      string `json:"reportPath"`
	User            string `json:"user"`
	Group           string `json:"group"`
	WorkloadGroup   string `json:"workloadGroup"`
	ServerName      string `json:"serverName"`
	Host            string `json:"host"`
	ClientPort      int    `json:"clientPort"`
	MonitoringPort  int    `json:"monitoringPort"`
	JetStream       struct {
		MaxMemStore  string `json:"maxMemStore"`
		MaxFileStore string `json:"maxFileStore"`
	} `json:"jetstream"`
	SPIFFE struct {
		AgentSocket string `json:"agentSocket"`
		CertDir     string `json:"certDir"`
	} `json:"spiffe"`
	Authorization struct {
		Users []authUser `json:"users"`
	} `json:"authorization"`
}

type authUser struct {
	SPIFFEID       string   `json:"spiffeID"`
	PublishAllow   []string `json:"publishAllow"`
	SubscribeAllow []string `json:"subscribeAllow"`
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

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "nats-recover: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "exec" {
		return runExec(args[1:])
	}
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
	if os.Geteuid() == 0 {
		if err := ensureAccount(cfg); err != nil {
			return err
		}
	}
	digest, err := installRuntime(opts.repoRoot, cfg)
	if err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		if err := prepareDirectories(cfg); err != nil {
			return err
		}
	}
	if err := writeNATSConfig(cfg); err != nil {
		return err
	}
	if err := writeHelperConfig(cfg); err != nil {
		return err
	}
	rep := report{
		Component:             "nats",
		ResourceName:          opts.resourceName,
		RuntimeArtifactDigest: digest,
		Conditions: []condition{
			conditionTrue("NATSRuntimeInstalled", "RuntimeReady", "repo-built NATS runtime is installed"),
			conditionTrue("NATSConfigWritten", "ConfigReady", "NATS config and SPIFFE helper config are written"),
			conditionTrue("NATSRecoveryComplete", "Recovered", "NATS is ready for Nomad to start"),
		},
	}
	return writeReport(cfg.ReportPath, rep)
}

func runExec(args []string) error {
	opts, err := parseExecOptions(args)
	if err != nil {
		return err
	}
	if err := waitForFiles(opts.waitFiles, opts.waitTimeout); err != nil {
		return err
	}
	return execAsUser(opts.user, opts.command)
}

func parseExecOptions(args []string) (execOptions, error) {
	opts := execOptions{waitTimeout: 60 * time.Second}
	fs := flag.NewFlagSet("nats-recover exec", flag.ContinueOnError)
	fs.StringVar(&opts.user, "user", "", "User to drop to before exec.")
	fs.Var(&opts.waitFiles, "wait-file", "File that must exist before exec; repeatable.")
	fs.DurationVar(&opts.waitTimeout, "wait-timeout", opts.waitTimeout, "Maximum time to wait for required files.")
	if err := fs.Parse(args); err != nil {
		return execOptions{}, err
	}
	if strings.TrimSpace(opts.user) == "" {
		return execOptions{}, errors.New("nats-recover exec requires --user")
	}
	if fs.NArg() == 0 {
		return execOptions{}, errors.New("nats-recover exec requires a command")
	}
	opts.command = fs.Args()
	return opts, nil
}

func waitForFiles(paths []string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		var missing []string
		for _, path := range paths {
			stat, err := os.Stat(path)
			if err != nil || !stat.Mode().IsRegular() || stat.Size() == 0 {
				missing = append(missing, path)
			}
		}
		if len(missing) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s", strings.Join(missing, ", "))
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func execAsUser(userName string, command []string) error {
	if os.Geteuid() != 0 {
		return errors.New("nats-recover exec must run as root")
	}
	u, err := user.Lookup(userName)
	if err != nil {
		return fmt.Errorf("lookup user %s: %w", userName, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return fmt.Errorf("parse uid %s: %w", u.Uid, err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return fmt.Errorf("parse gid %s: %w", u.Gid, err)
	}
	groupIDs, err := u.GroupIds()
	if err != nil {
		return fmt.Errorf("load supplementary groups for %s: %w", userName, err)
	}
	groups := make([]int, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		group, err := strconv.Atoi(groupID)
		if err != nil {
			return fmt.Errorf("parse supplementary gid %s: %w", groupID, err)
		}
		groups = append(groups, group)
	}
	if err := syscall.Setgroups(groups); err != nil {
		return fmt.Errorf("set supplementary groups for %s: %w", userName, err)
	}
	if err := syscall.Setgid(gid); err != nil {
		return fmt.Errorf("set gid for %s: %w", userName, err)
	}
	if err := syscall.Setuid(uid); err != nil {
		return fmt.Errorf("set uid for %s: %w", userName, err)
	}
	return syscall.Exec(command[0], command, os.Environ())
}

func parseOptions(args []string) (options, error) {
	opts := options{
		repoRoot:     defaultRepoRoot,
		resourceName: defaultResource,
	}
	fs := flag.NewFlagSet("nats-recover", flag.ContinueOnError)
	fs.StringVar(&opts.repoRoot, "repo-root", opts.repoRoot, "Boarded repo root.")
	fs.StringVar(&opts.resourceGraph, "resource-graph", "", "Guardian resource graph document path.")
	fs.StringVar(&opts.resourceName, "resource-name", opts.resourceName, "NATSCluster resource name.")
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
		return config{}, fmt.Errorf("decode NATSCluster spec: %w", err)
	}
	return cfg, nil
}

func validateConfig(cfg config) error {
	for name, value := range map[string]string{
		"runtimeArtifact":        cfg.RuntimeArtifact,
		"runtimeRoot":            cfg.RuntimeRoot,
		"configPath":             cfg.ConfigPath,
		"helperConfigPath":       cfg.HelperConfig,
		"dataDir":                cfg.DataDir,
		"jetStreamDir":           cfg.JetStreamDir,
		"pidPath":                cfg.PIDPath,
		"reportPath":             cfg.ReportPath,
		"user":                   cfg.User,
		"group":                  cfg.Group,
		"workloadGroup":          cfg.WorkloadGroup,
		"serverName":             cfg.ServerName,
		"host":                   cfg.Host,
		"jetstream.maxMemStore":  cfg.JetStream.MaxMemStore,
		"jetstream.maxFileStore": cfg.JetStream.MaxFileStore,
		"spiffe.agentSocket":     cfg.SPIFFE.AgentSocket,
		"spiffe.certDir":         cfg.SPIFFE.CertDir,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("NATSCluster.spec.%s is required", name)
		}
	}
	if filepath.IsAbs(cfg.RuntimeArtifact) || strings.Contains(filepath.ToSlash(cfg.RuntimeArtifact), "../") {
		return errors.New("NATSCluster.spec.runtimeArtifact must be repo-relative")
	}
	for name, value := range map[string]string{
		"runtimeRoot":        cfg.RuntimeRoot,
		"configPath":         cfg.ConfigPath,
		"helperConfigPath":   cfg.HelperConfig,
		"dataDir":            cfg.DataDir,
		"jetStreamDir":       cfg.JetStreamDir,
		"pidPath":            cfg.PIDPath,
		"reportPath":         cfg.ReportPath,
		"spiffe.agentSocket": cfg.SPIFFE.AgentSocket,
		"spiffe.certDir":     cfg.SPIFFE.CertDir,
	} {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("NATSCluster.spec.%s must be an absolute path", name)
		}
	}
	if cfg.ClientPort <= 0 || cfg.ClientPort > 65535 || cfg.MonitoringPort <= 0 || cfg.MonitoringPort > 65535 {
		return errors.New("NATSCluster client and monitoring ports must be valid TCP ports")
	}
	if len(cfg.Authorization.Users) == 0 {
		return errors.New("NATSCluster authorization.users requires at least one mapped SPIFFE identity")
	}
	for i, user := range cfg.Authorization.Users {
		if strings.TrimSpace(user.SPIFFEID) == "" {
			return fmt.Errorf("NATSCluster authorization.users[%d].spiffeID is required", i)
		}
	}
	return nil
}

func ensureAccount(cfg config) error {
	if err := ensureGroup(cfg.Group); err != nil {
		return err
	}
	if cfg.WorkloadGroup != "" {
		if err := ensureGroup(cfg.WorkloadGroup); err != nil {
			return err
		}
	}
	if _, err := user.Lookup(cfg.User); err != nil {
		if _, ok := err.(user.UnknownUserError); !ok {
			return fmt.Errorf("lookup NATS user: %w", err)
		}
		if err := command("/usr/sbin/useradd", "--system", "--gid", cfg.Group, "--groups", cfg.WorkloadGroup, "--home-dir", cfg.DataDir, "--shell", "/usr/sbin/nologin", "--create-home", cfg.User); err != nil {
			return err
		}
		return nil
	}
	if cfg.WorkloadGroup != "" {
		if err := command("/usr/sbin/usermod", "-a", "-G", cfg.WorkloadGroup, cfg.User); err != nil {
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
		return "", fmt.Errorf("chmod NATS runtime release: %w", err)
	}
	if err := promoteRuntime(cfg.RuntimeRoot, release); err != nil {
		return "", err
	}
	return digest, nil
}

func runtimeInstalled(release string) bool {
	for _, rel := range []string{"bin/nats-server", "bin/spiffe-helper", "bin/nats-recover"} {
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
		return "", fmt.Errorf("open NATS runtime artifact: %w", err)
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("hash NATS runtime artifact: %w", err)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func extractRuntimeTar(artifact string, release string) error {
	if err := os.MkdirAll(filepath.Dir(release), 0o755); err != nil {
		return fmt.Errorf("create NATS runtime release parent: %w", err)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(release), "."+filepath.Base(release)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create NATS runtime staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := extractTar(artifact, tmp); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return fmt.Errorf("chmod NATS runtime staging directory: %w", err)
	}
	if !runtimeInstalled(tmp) {
		return errors.New("NATS runtime artifact missing nats-server, spiffe-helper, or nats-recover")
	}
	if err := os.RemoveAll(release); err != nil {
		return fmt.Errorf("remove stale NATS runtime release: %w", err)
	}
	if err := os.Rename(tmp, release); err != nil {
		return fmt.Errorf("publish NATS runtime release: %w", err)
	}
	return nil
}

func extractTar(artifact string, dest string) error {
	file, err := os.Open(artifact)
	if err != nil {
		return fmt.Errorf("open NATS runtime artifact: %w", err)
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
			return fmt.Errorf("read NATS runtime tar: %w", err)
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
			return fmt.Errorf("unsupported NATS runtime tar entry %s type %d", header.Name, header.Typeflag)
		}
	}
}

func safeTarTarget(destAbs string, name string) (string, error) {
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("unsafe NATS runtime tar entry %s", name)
	}
	target := filepath.Join(destAbs, name)
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if targetAbs != destAbs && !strings.HasPrefix(targetAbs, destAbs+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe NATS runtime tar entry %s", name)
	}
	return targetAbs, nil
}

func modeOrDefault(mode int64, fallback os.FileMode) os.FileMode {
	if mode == 0 {
		return fallback
	}
	return os.FileMode(mode).Perm()
}

func promoteRuntime(runtimeRoot string, release string) error {
	if err := os.MkdirAll(runtimeRoot, 0o755); err != nil {
		return fmt.Errorf("create NATS runtime root: %w", err)
	}
	next := filepath.Join(runtimeRoot, "current.next")
	current := filepath.Join(runtimeRoot, "current")
	if err := os.Remove(next); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale NATS current symlink: %w", err)
	}
	if err := os.Symlink(release, next); err != nil {
		return fmt.Errorf("create NATS current symlink: %w", err)
	}
	if stat, err := os.Lstat(current); err == nil && stat.Mode()&os.ModeSymlink == 0 {
		if err := os.RemoveAll(current); err != nil {
			return fmt.Errorf("remove non-symlink NATS current runtime: %w", err)
		}
	}
	if err := os.Rename(next, current); err != nil {
		return fmt.Errorf("publish NATS current runtime: %w", err)
	}
	return nil
}

func prepareDirectories(cfg config) error {
	uid, gid, err := ids(cfg.User, cfg.Group)
	if err != nil {
		return err
	}
	for _, dir := range []string{
		filepath.Dir(cfg.ConfigPath),
		cfg.DataDir,
		cfg.JetStreamDir,
		cfg.SPIFFE.CertDir,
		filepath.Dir(cfg.ReportPath),
	} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		if err := os.Chown(dir, uid, gid); err != nil {
			return fmt.Errorf("chown %s: %w", dir, err)
		}
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

func writeNATSConfig(cfg config) error {
	var b strings.Builder
	fmt.Fprintf(&b, "server_name: %s\n", quote(cfg.ServerName))
	fmt.Fprintf(&b, "pid_file: %s\n", quote(cfg.PIDPath))
	fmt.Fprintf(&b, "host: %s\n", cfg.Host)
	fmt.Fprintf(&b, "port: %d\n", cfg.ClientPort)
	fmt.Fprintf(&b, "http: %d\n\n", cfg.MonitoringPort)
	fmt.Fprintf(&b, "jetstream {\n  store_dir: %s\n  max_mem_store: %s\n  max_file_store: %s\n}\n\n", quote(cfg.JetStreamDir), cfg.JetStream.MaxMemStore, cfg.JetStream.MaxFileStore)
	fmt.Fprintf(&b, "tls {\n  cert_file: %s\n  key_file: %s\n  ca_file: %s\n  verify: true\n  verify_and_map: true\n  timeout: 2\n}\n\n", quote(filepath.Join(cfg.SPIFFE.CertDir, "svid.pem")), quote(filepath.Join(cfg.SPIFFE.CertDir, "svid_key.pem")), quote(filepath.Join(cfg.SPIFFE.CertDir, "bundle.pem")))
	fmt.Fprintf(&b, "authorization {\n  users = [\n")
	for _, user := range cfg.Authorization.Users {
		fmt.Fprintf(&b, "    {\n      user: %s\n      permissions: {\n        publish: { allow: %s }\n        subscribe: { allow: %s }\n      }\n    }\n", quote(user.SPIFFEID), quoteList(user.PublishAllow), quoteList(user.SubscribeAllow))
	}
	fmt.Fprintf(&b, "  ]\n}\n")
	return writeNATSOwnedFile(cfg, cfg.ConfigPath, []byte(b.String()), 0o640)
}

func writeHelperConfig(cfg config) error {
	body := fmt.Sprintf(`agent_address = %s
cert_dir = %s
daemon_mode = true
pid_file_name = %s
renew_signal = "SIGHUP"
svid_file_name = "svid.pem"
svid_key_file_name = "svid_key.pem"
svid_bundle_file_name = "bundle.pem"
cert_file_mode = 0640
key_file_mode = 0600
`, quote(cfg.SPIFFE.AgentSocket), quote(cfg.SPIFFE.CertDir), quote(cfg.PIDPath))
	return writeNATSOwnedFile(cfg, cfg.HelperConfig, []byte(body), 0o640)
}

func writeReport(path string, rep report) error {
	body, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("encode NATS report: %w", err)
	}
	body = append(body, '\n')
	return writePrivateFile(path, body, 0o640)
}

func writeNATSOwnedFile(cfg config, path string, body []byte, mode os.FileMode) error {
	if err := writePrivateFile(path, body, mode); err != nil {
		return err
	}
	if os.Geteuid() != 0 {
		return nil
	}
	_, gid, err := ids(cfg.User, cfg.Group)
	if err != nil {
		return err
	}
	if err := os.Chown(path, 0, gid); err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	return nil
}

func writePrivateFile(path string, body []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, path, err)
	}
	return nil
}

func quote(value string) string {
	return strconv.Quote(value)
}

func quoteList(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, quote(value))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func conditionTrue(t string, reason string, message string) condition {
	return condition{Type: t, Status: "True", Reason: reason, Resource: "nats", Message: message}
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
