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
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	apiVersion      = "tigerbeetle.guardianintelligence.org/v1alpha1"
	kind            = "TigerBeetleCluster"
	defaultRepoRoot = "/home/ubuntu/.local/state/guardian/repo/current"
	defaultResource = "tigerbeetle"
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
	RuntimeArtifact string   `json:"runtimeArtifact"`
	RuntimeRoot     string   `json:"runtimeRoot"`
	DataFile        string   `json:"dataFile"`
	ReportPath      string   `json:"reportPath"`
	User            string   `json:"user"`
	Group           string   `json:"group"`
	ClusterID       int      `json:"clusterID"`
	Replica         int      `json:"replica"`
	ReplicaCount    int      `json:"replicaCount"`
	Addresses       []string `json:"addresses"`
	LogLevel        string   `json:"logLevel"`
	StatsdAddress   string   `json:"statsdAddress"`
	Experimental    bool     `json:"experimental"`
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
		fmt.Fprintln(os.Stderr, "tigerbeetle-recover: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: tigerbeetle-recover <recover|server> [flags]")
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
		if err := ensureDataFile(cfg); err != nil {
			return err
		}
		if err := grantMemoryLock(cfg); err != nil {
			return err
		}
		rep := report{
			Component:             "tigerbeetle",
			ResourceName:          opts.resourceName,
			RuntimeArtifactDigest: digest,
			Conditions: []condition{
				conditionTrue("TigerBeetleRuntimeInstalled", "RuntimeReady", "repo-built TigerBeetle runtime is installed"),
				conditionTrue("TigerBeetleDataFileReady", "DataFileReady", "TigerBeetle data file exists"),
				conditionTrue("TigerBeetleRecoveryComplete", "Recovered", "TigerBeetle is ready for Nomad to start"),
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
	fs := flag.NewFlagSet("tigerbeetle-recover", flag.ContinueOnError)
	fs.StringVar(&opts.repoRoot, "repo-root", opts.repoRoot, "Boarded repo root.")
	fs.StringVar(&opts.resourceGraph, "resource-graph", "", "Guardian resource graph document path.")
	fs.StringVar(&opts.resourceName, "resource-name", opts.resourceName, "TigerBeetleCluster resource name.")
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
		return config{}, fmt.Errorf("decode TigerBeetleCluster spec: %w", err)
	}
	return cfg, nil
}

func validateConfig(cfg config) error {
	for name, value := range map[string]string{
		"runtimeArtifact": cfg.RuntimeArtifact,
		"runtimeRoot":     cfg.RuntimeRoot,
		"dataFile":        cfg.DataFile,
		"reportPath":      cfg.ReportPath,
		"user":            cfg.User,
		"group":           cfg.Group,
		"logLevel":        cfg.LogLevel,
		"statsdAddress":   cfg.StatsdAddress,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("TigerBeetleCluster.spec.%s is required", name)
		}
	}
	if filepath.IsAbs(cfg.RuntimeArtifact) || strings.Contains(filepath.ToSlash(cfg.RuntimeArtifact), "../") {
		return errors.New("TigerBeetleCluster.spec.runtimeArtifact must be repo-relative")
	}
	for name, value := range map[string]string{
		"runtimeRoot": cfg.RuntimeRoot,
		"dataFile":    cfg.DataFile,
		"reportPath":  cfg.ReportPath,
	} {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("TigerBeetleCluster.spec.%s must be an absolute path", name)
		}
	}
	if cfg.Replica < 0 || cfg.ReplicaCount <= 0 || cfg.Replica >= cfg.ReplicaCount {
		return errors.New("TigerBeetleCluster replica must be in range [0, replicaCount)")
	}
	if len(cfg.Addresses) == 0 {
		return errors.New("TigerBeetleCluster.spec.addresses requires at least one address")
	}
	for i, address := range cfg.Addresses {
		if err := validateHostPort(address); err != nil {
			return fmt.Errorf("TigerBeetleCluster.spec.addresses[%d]: %w", i, err)
		}
	}
	if err := validateHostPort(cfg.StatsdAddress); err != nil {
		return fmt.Errorf("TigerBeetleCluster.spec.statsdAddress: %w", err)
	}
	return nil
}

func validateHostPort(value string) error {
	host, port, err := net.SplitHostPort(value)
	if err != nil {
		return fmt.Errorf("must be host:port: %w", err)
	}
	if strings.TrimSpace(host) == "" {
		return errors.New("host is required")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n <= 0 || n > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	return nil
}

func ensureAccount(cfg config) error {
	if err := ensureGroup(cfg.Group); err != nil {
		return err
	}
	if _, err := user.Lookup(cfg.User); err != nil {
		if _, ok := err.(user.UnknownUserError); !ok {
			return fmt.Errorf("lookup TigerBeetle user: %w", err)
		}
		if err := command("/usr/sbin/useradd", "--system", "--gid", cfg.Group, "--home-dir", filepath.Dir(cfg.DataFile), "--shell", "/usr/sbin/nologin", "--no-create-home", cfg.User); err != nil {
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

func ensureDataFile(cfg config) error {
	u, err := user.Lookup(cfg.User)
	if err != nil {
		return fmt.Errorf("lookup TigerBeetle user: %w", err)
	}
	g, err := user.LookupGroup(cfg.Group)
	if err != nil {
		return fmt.Errorf("lookup TigerBeetle group: %w", err)
	}
	uid, err := parseID(u.Uid, "uid")
	if err != nil {
		return err
	}
	gid, err := parseID(g.Gid, "gid")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DataFile), 0o700); err != nil {
		return fmt.Errorf("create TigerBeetle data directory: %w", err)
	}
	if err := os.Chown(filepath.Dir(cfg.DataFile), uid, gid); err != nil {
		return fmt.Errorf("chown TigerBeetle data directory: %w", err)
	}
	if stat, err := os.Stat(cfg.DataFile); err == nil {
		if !stat.Mode().IsRegular() {
			return fmt.Errorf("TigerBeetle data file %s is not a regular file", cfg.DataFile)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat TigerBeetle data file: %w", err)
	}
	args := []string{
		filepath.Join(cfg.RuntimeRoot, "current/bin/tigerbeetle"),
		"format",
		"--cluster=" + strconv.Itoa(cfg.ClusterID),
		"--replica=" + strconv.Itoa(cfg.Replica),
		"--replica-count=" + strconv.Itoa(cfg.ReplicaCount),
		cfg.DataFile,
	}
	if err := runAsUserWithEnv(cfg.User, args, nil); err != nil {
		return fmt.Errorf("format TigerBeetle data file: %w", err)
	}
	if err := os.Chown(cfg.DataFile, uid, gid); err != nil {
		return fmt.Errorf("chown TigerBeetle data file: %w", err)
	}
	if err := os.Chmod(cfg.DataFile, 0o600); err != nil {
		return fmt.Errorf("chmod TigerBeetle data file: %w", err)
	}
	return nil
}

func grantMemoryLock(cfg config) error {
	return command("/usr/sbin/setcap", "cap_ipc_lock+ep", filepath.Join(cfg.RuntimeRoot, "current/bin/tigerbeetle"))
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
		return "", fmt.Errorf("chmod TigerBeetle runtime release: %w", err)
	}
	if err := promoteRuntime(cfg.RuntimeRoot, release); err != nil {
		return "", err
	}
	return digest, nil
}

func runtimeInstalled(release string) bool {
	for _, rel := range []string{"bin/tigerbeetle", "bin/tigerbeetle-recover"} {
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
		return "", fmt.Errorf("open TigerBeetle runtime artifact: %w", err)
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("hash TigerBeetle runtime artifact: %w", err)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func extractRuntimeTar(artifact string, release string) error {
	if err := os.MkdirAll(filepath.Dir(release), 0o755); err != nil {
		return fmt.Errorf("create TigerBeetle runtime release parent: %w", err)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(release), "."+filepath.Base(release)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create TigerBeetle runtime staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := extractTar(artifact, tmp); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return fmt.Errorf("chmod TigerBeetle runtime staging directory: %w", err)
	}
	if !runtimeInstalled(tmp) {
		return errors.New("TigerBeetle runtime artifact missing tigerbeetle or tigerbeetle-recover")
	}
	if err := os.RemoveAll(release); err != nil {
		return fmt.Errorf("remove stale TigerBeetle runtime release: %w", err)
	}
	if err := os.Rename(tmp, release); err != nil {
		return fmt.Errorf("publish TigerBeetle runtime release: %w", err)
	}
	return nil
}

func extractTar(artifact string, dest string) error {
	file, err := os.Open(artifact)
	if err != nil {
		return fmt.Errorf("open TigerBeetle runtime artifact: %w", err)
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
			return fmt.Errorf("read TigerBeetle runtime tar: %w", err)
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
	if clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
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

func modeOrDefault(raw int64, fallback os.FileMode) os.FileMode {
	if raw <= 0 {
		return fallback
	}
	return os.FileMode(raw & 0o7777)
}

func promoteRuntime(root string, release string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create TigerBeetle runtime root: %w", err)
	}
	next := filepath.Join(root, "current.next")
	current := filepath.Join(root, "current")
	_ = os.Remove(next)
	if err := os.Symlink(release, next); err != nil {
		return fmt.Errorf("create TigerBeetle runtime symlink: %w", err)
	}
	if err := os.Rename(next, current); err != nil {
		return fmt.Errorf("promote TigerBeetle runtime symlink: %w", err)
	}
	return nil
}

func execServer(cfg config) error {
	args := []string{
		filepath.Join(cfg.RuntimeRoot, "current/bin/tigerbeetle"),
		"start",
		"--addresses=" + strings.Join(cfg.Addresses, ","),
		"--statsd=" + cfg.StatsdAddress,
	}
	if cfg.Experimental {
		args = append(args, "--experimental")
	}
	args = append(args, cfg.DataFile)
	env := []string{
		"TB_LOG=" + cfg.LogLevel,
		"TB_STATSD=" + cfg.StatsdAddress,
	}
	return execAsUserWithEnv(cfg.User, args, env)
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

func runAsUserWithEnv(userName string, command []string, extraEnv []string) error {
	if len(command) == 0 {
		return errors.New("command is required")
	}
	creds, err := lookupUserCredentials(userName)
	if err != nil {
		return err
	}
	groups := make([]uint32, 0, len(creds.groups))
	for _, group := range creds.groups {
		groups = append(groups, uint32(group))
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid:    uint32(creds.uid),
			Gid:    uint32(creds.gid),
			Groups: groups,
		},
	}
	return cmd.Run()
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create TigerBeetle report directory: %w", err)
	}
	body, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("encode TigerBeetle report: %w", err)
	}
	body = append(body, '\n')
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("write TigerBeetle report: %w", err)
	}
	return nil
}

func conditionTrue(t string, reason string, message string) condition {
	return condition{Type: t, Status: "True", Reason: reason, Resource: defaultResource, Message: message}
}
