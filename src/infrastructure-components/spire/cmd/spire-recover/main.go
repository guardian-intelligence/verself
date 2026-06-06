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
	apiVersion      = "spire.guardianintelligence.org/v1alpha1"
	kind            = "SPIRECluster"
	defaultRepoRoot = "/home/ubuntu/.local/state/guardian/repo/current"
	defaultResource = "spire"
)

type options struct {
	repoRoot      string
	resourceGraph string
	resourceName  string
	loop          bool
}

type execOptions struct {
	user    string
	command []string
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
	RuntimeArtifact          string `json:"runtimeArtifact"`
	IdentityRegistryArtifact string `json:"identityRegistryArtifact"`
	RuntimeRoot              string `json:"runtimeRoot"`
	ServerConfigPath         string `json:"serverConfigPath"`
	AgentConfigPath          string `json:"agentConfigPath"`
	ServerDataDir            string `json:"serverDataDir"`
	AgentDataDir             string `json:"agentDataDir"`
	ServerSocketPath         string `json:"serverSocketPath"`
	AgentSocketPath          string `json:"agentSocketPath"`
	JoinTokenPath            string `json:"joinTokenPath"`
	ReportPath               string `json:"reportPath"`
	TrustDomain              string `json:"trustDomain"`
	AgentSPIFFEID            string `json:"agentSpiffeID"`
	ServerAddress            string `json:"serverAddress"`
	ServerPort               int    `json:"serverPort"`
	ServerUser               string `json:"serverUser"`
	ServerGroup              string `json:"serverGroup"`
	WorkloadGroup            string `json:"workloadGroup"`
	RegistrarIntervalSeconds int    `json:"registrarIntervalSeconds"`
}

type identityRegistry struct {
	SchemaVersion int        `json:"schema_version"`
	Identities    []identity `json:"identities"`
}

type identity struct {
	EntryID            string    `json:"entry_id"`
	Group              string    `json:"group"`
	Key                string    `json:"key"`
	Path               string    `json:"path"`
	Selector           string    `json:"selector"`
	UIDPolicy          uidPolicy `json:"uid_policy"`
	User               string    `json:"user"`
	X509SVIDTTLSeconds int       `json:"x509_svid_ttl_seconds"`
}

type uidPolicy struct {
	Kind  string `json:"kind"`
	Value int    `json:"value"`
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
	RegisteredIdentities  int         `json:"registeredIdentities,omitempty"`
	Conditions            []condition `json:"conditions"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "spire-recover: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		args = []string{"recover"}
	}
	commandName := args[0]
	args = args[1:]
	switch commandName {
	case "recover":
		return recover(args)
	case "agent":
		return runAgent(args)
	case "reconcile":
		return reconcile(args)
	case "exec":
		return runExec(args)
	default:
		return fmt.Errorf("unknown command %q", commandName)
	}
}

func recover(args []string) error {
	opts, cfg, err := load(args, false)
	if err != nil {
		return err
	}
	digest, err := installRuntime(opts.repoRoot, cfg)
	if err != nil {
		return err
	}
	if err := prepareAccounts(cfg); err != nil {
		return err
	}
	if err := prepareDirectories(cfg); err != nil {
		return err
	}
	if err := writeServerConfig(cfg); err != nil {
		return err
	}
	if err := writeAgentConfig(cfg); err != nil {
		return err
	}
	rep := report{
		Component:             "spire",
		ResourceName:          opts.resourceName,
		RuntimeArtifactDigest: digest,
		Conditions: []condition{
			conditionTrue("SPIRERuntimeInstalled", "RuntimeReady", "repo-built SPIRE runtime is installed"),
			conditionTrue("SPIREConfigWritten", "ConfigReady", "SPIRE server and agent config are written"),
		},
	}
	return writeReport(cfg.ReportPath, rep)
}

func runAgent(args []string) error {
	_, cfg, err := load(args, false)
	if err != nil {
		return err
	}
	if err := waitForFile(cfg.JoinTokenPath, 120*time.Second); err != nil {
		return err
	}
	agent := filepath.Join(cfg.RuntimeRoot, "current/bin/spire-agent")
	return syscall.Exec(agent, []string{
		agent,
		"run",
		"-config",
		cfg.AgentConfigPath,
		"-joinTokenFile",
		cfg.JoinTokenPath,
	}, os.Environ())
}

func reconcile(args []string) error {
	opts, cfg, err := load(args, true)
	if err != nil {
		return err
	}
	if opts.loop {
		for {
			if err := reconcileOnce(opts.repoRoot, opts.resourceName, cfg); err != nil {
				_ = writeReport(cfg.ReportPath, report{
					Component:    "spire",
					ResourceName: opts.resourceName,
					Conditions: []condition{
						conditionFalse("SPIRERecoveryComplete", "ReconcileFailed", err.Error()),
					},
				})
				fmt.Fprintln(os.Stderr, "spire-recover: reconcile failed: "+err.Error())
			}
			time.Sleep(time.Duration(cfg.RegistrarIntervalSeconds) * time.Second)
		}
	}
	return reconcileOnce(opts.repoRoot, opts.resourceName, cfg)
}

func reconcileOnce(repoRoot string, resourceName string, cfg config) error {
	if err := waitForCommand(90*time.Second, filepath.Join(cfg.RuntimeRoot, "current/bin/spire-server"), "healthcheck", "-socketPath", cfg.ServerSocketPath); err != nil {
		return err
	}
	if err := issueJoinToken(cfg); err != nil {
		return err
	}
	if err := waitForCommand(90*time.Second, filepath.Join(cfg.RuntimeRoot, "current/bin/spire-agent"), "healthcheck", "-socketPath", cfg.AgentSocketPath); err != nil {
		return err
	}
	if err := repairAgentSocketPermissions(cfg); err != nil {
		return err
	}
	registered, err := reconcileIdentityRegistry(repoRoot, cfg)
	if err != nil {
		return err
	}
	rep := report{
		Component:            "spire",
		ResourceName:         resourceName,
		RegisteredIdentities: registered,
		Conditions: []condition{
			conditionTrue("SPIREServerHealthy", "ServerReady", "SPIRE server API is healthy"),
			conditionTrue("SPIREJoinTokenIssued", "JoinTokenReady", "agent join token was issued"),
			conditionTrue("SPIREAgentHealthy", "AgentReady", "SPIRE agent API is healthy"),
			conditionTrue("SPIREWorkloadSocketReady", "SocketReady", "SPIRE workload API socket is accessible to workload identities"),
			conditionTrue("SPIREIdentityRegistryReconciled", "IdentityRegistryReady", "component-owned SPIFFE identities are registered"),
			conditionTrue("SPIRERecoveryComplete", "Recovered", "SPIRE server, agent, and identity registry are converged"),
		},
	}
	return writeReport(cfg.ReportPath, rep)
}

func runExec(args []string) error {
	opts, err := parseExecOptions(args)
	if err != nil {
		return err
	}
	return execAsUser(opts.user, opts.command)
}

func parseExecOptions(args []string) (execOptions, error) {
	opts := execOptions{}
	fs := flag.NewFlagSet("spire-recover exec", flag.ContinueOnError)
	fs.StringVar(&opts.user, "user", "", "User to drop to before exec.")
	if err := fs.Parse(args); err != nil {
		return execOptions{}, err
	}
	if strings.TrimSpace(opts.user) == "" {
		return execOptions{}, errors.New("spire-recover exec requires --user")
	}
	if fs.NArg() == 0 {
		return execOptions{}, errors.New("spire-recover exec requires a command")
	}
	opts.command = fs.Args()
	return opts, nil
}

func load(args []string, allowLoop bool) (options, config, error) {
	opts, err := parseOptions(args, allowLoop)
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

func parseOptions(args []string, allowLoop bool) (options, error) {
	opts := options{
		repoRoot:     defaultRepoRoot,
		resourceName: defaultResource,
	}
	fs := flag.NewFlagSet("spire-recover", flag.ContinueOnError)
	fs.StringVar(&opts.repoRoot, "repo-root", opts.repoRoot, "Boarded repo root.")
	fs.StringVar(&opts.resourceGraph, "resource-graph", "", "Guardian resource graph document path.")
	fs.StringVar(&opts.resourceName, "resource-name", opts.resourceName, "SPIRECluster resource name.")
	if allowLoop {
		fs.BoolVar(&opts.loop, "loop", false, "Continuously reconcile the SPIRE identity registry.")
	}
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
		return config{}, fmt.Errorf("decode SPIRECluster spec: %w", err)
	}
	return cfg, nil
}

func validateConfig(cfg config) error {
	for name, value := range map[string]string{
		"runtimeArtifact":          cfg.RuntimeArtifact,
		"identityRegistryArtifact": cfg.IdentityRegistryArtifact,
		"runtimeRoot":              cfg.RuntimeRoot,
		"serverConfigPath":         cfg.ServerConfigPath,
		"agentConfigPath":          cfg.AgentConfigPath,
		"serverDataDir":            cfg.ServerDataDir,
		"agentDataDir":             cfg.AgentDataDir,
		"serverSocketPath":         cfg.ServerSocketPath,
		"agentSocketPath":          cfg.AgentSocketPath,
		"joinTokenPath":            cfg.JoinTokenPath,
		"reportPath":               cfg.ReportPath,
		"trustDomain":              cfg.TrustDomain,
		"agentSpiffeID":            cfg.AgentSPIFFEID,
		"serverAddress":            cfg.ServerAddress,
		"serverUser":               cfg.ServerUser,
		"serverGroup":              cfg.ServerGroup,
		"workloadGroup":            cfg.WorkloadGroup,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("SPIRECluster.spec.%s is required", name)
		}
	}
	for name, value := range map[string]string{
		"runtimeArtifact":          cfg.RuntimeArtifact,
		"identityRegistryArtifact": cfg.IdentityRegistryArtifact,
	} {
		if filepath.IsAbs(value) || strings.Contains(filepath.ToSlash(value), "../") {
			return fmt.Errorf("SPIRECluster.spec.%s must be repo-relative", name)
		}
	}
	for name, value := range map[string]string{
		"runtimeRoot":      cfg.RuntimeRoot,
		"serverConfigPath": cfg.ServerConfigPath,
		"agentConfigPath":  cfg.AgentConfigPath,
		"serverDataDir":    cfg.ServerDataDir,
		"agentDataDir":     cfg.AgentDataDir,
		"serverSocketPath": cfg.ServerSocketPath,
		"agentSocketPath":  cfg.AgentSocketPath,
		"joinTokenPath":    cfg.JoinTokenPath,
		"reportPath":       cfg.ReportPath,
	} {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("SPIRECluster.spec.%s must be an absolute path", name)
		}
	}
	if cfg.ServerPort <= 0 || cfg.ServerPort > 65535 {
		return errors.New("SPIRECluster.spec.serverPort must be a valid TCP port")
	}
	if cfg.RegistrarIntervalSeconds <= 0 {
		return errors.New("SPIRECluster.spec.registrarIntervalSeconds must be positive")
	}
	return nil
}

func prepareAccounts(cfg config) error {
	if err := ensureGroup(cfg.ServerGroup); err != nil {
		return err
	}
	if err := ensureGroup(cfg.WorkloadGroup); err != nil {
		return err
	}
	return ensureUser(systemUser{
		User:  cfg.ServerUser,
		Group: cfg.ServerGroup,
	})
}

type systemUser struct {
	User       string
	Group      string
	FixedUID   int
	ExtraGroup string
}

func ensureIdentityAccount(id identity, workloadGroup string) error {
	if err := ensureGroup(id.Group); err != nil {
		return err
	}
	fixedUID := 0
	if id.UIDPolicy.Kind == "fixed" {
		fixedUID = id.UIDPolicy.Value
	}
	return ensureUser(systemUser{
		User:       id.User,
		Group:      id.Group,
		FixedUID:   fixedUID,
		ExtraGroup: workloadGroup,
	})
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

func ensureUser(spec systemUser) error {
	u, err := user.Lookup(spec.User)
	if err != nil {
		if _, ok := err.(user.UnknownUserError); !ok {
			return fmt.Errorf("lookup user %s: %w", spec.User, err)
		}
		args := []string{"--system", "--gid", spec.Group, "--home-dir", "/nonexistent", "--shell", "/usr/sbin/nologin", "--no-create-home"}
		if spec.FixedUID > 0 {
			args = append(args, "--uid", strconv.Itoa(spec.FixedUID))
		}
		if spec.ExtraGroup != "" {
			args = append(args, "--groups", spec.ExtraGroup)
		}
		args = append(args, spec.User)
		return command("/usr/sbin/useradd", args...)
	}
	if spec.FixedUID > 0 && u.Uid != strconv.Itoa(spec.FixedUID) {
		return fmt.Errorf("user %s has uid %s, expected fixed uid %d", spec.User, u.Uid, spec.FixedUID)
	}
	if spec.ExtraGroup != "" {
		if err := command("/usr/sbin/usermod", "-a", "-G", spec.ExtraGroup, spec.User); err != nil {
			return err
		}
	}
	return nil
}

func prepareDirectories(cfg config) error {
	serverUID, serverGID, err := ids(cfg.ServerUser, cfg.ServerGroup)
	if err != nil {
		return err
	}
	for _, dir := range []string{
		cfg.RuntimeRoot,
		cfg.ServerDataDir,
		filepath.Dir(filepath.Dir(cfg.ServerSocketPath)),
		filepath.Dir(cfg.ServerSocketPath),
	} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		if err := os.Chown(dir, serverUID, serverGID); err != nil {
			return fmt.Errorf("chown %s: %w", dir, err)
		}
	}
	workloadGID, err := groupID(cfg.WorkloadGroup)
	if err != nil {
		return err
	}
	for _, dir := range []string{
		cfg.AgentDataDir,
		filepath.Dir(filepath.Dir(cfg.AgentSocketPath)),
		filepath.Dir(cfg.AgentSocketPath),
		filepath.Dir(cfg.JoinTokenPath),
		filepath.Dir(cfg.ReportPath),
	} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		if err := os.Chown(dir, 0, workloadGID); err != nil {
			return fmt.Errorf("chown %s: %w", dir, err)
		}
	}
	for _, dir := range uniqueStrings([]string{
		filepath.Dir(cfg.ServerConfigPath),
		filepath.Dir(cfg.AgentConfigPath),
	}) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		if err := os.Chmod(dir, 0o755); err != nil {
			return fmt.Errorf("chmod %s: %w", dir, err)
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
	if err := promoteRuntime(cfg.RuntimeRoot, release); err != nil {
		return "", err
	}
	return digest, nil
}

func runtimeInstalled(release string) bool {
	for _, rel := range []string{"bin/spire-agent", "bin/spire-recover", "bin/spire-server"} {
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
		return "", fmt.Errorf("open SPIRE artifact: %w", err)
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("hash SPIRE artifact: %w", err)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func extractRuntimeTar(artifact string, release string) error {
	if err := os.MkdirAll(filepath.Dir(release), 0o755); err != nil {
		return fmt.Errorf("create SPIRE runtime release parent: %w", err)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(release), "."+filepath.Base(release)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create SPIRE runtime staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := extractTar(artifact, tmp); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return fmt.Errorf("chmod SPIRE runtime staging directory: %w", err)
	}
	if !runtimeInstalled(tmp) {
		return errors.New("SPIRE runtime artifact missing spire-agent, spire-recover, or spire-server")
	}
	if err := os.RemoveAll(release); err != nil {
		return fmt.Errorf("remove stale SPIRE runtime release: %w", err)
	}
	if err := os.Rename(tmp, release); err != nil {
		return fmt.Errorf("publish SPIRE runtime release: %w", err)
	}
	return nil
}

func extractTar(artifact string, dest string) error {
	file, err := os.Open(artifact)
	if err != nil {
		return fmt.Errorf("open SPIRE runtime artifact: %w", err)
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
			return fmt.Errorf("read SPIRE runtime tar: %w", err)
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
			return fmt.Errorf("unsupported SPIRE runtime tar entry %s type %d", header.Name, header.Typeflag)
		}
	}
}

func safeTarTarget(destAbs string, name string) (string, error) {
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("unsafe SPIRE runtime tar entry %s", name)
	}
	target := filepath.Join(destAbs, name)
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if targetAbs != destAbs && !strings.HasPrefix(targetAbs, destAbs+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe SPIRE runtime tar entry %s", name)
	}
	return targetAbs, nil
}

func promoteRuntime(runtimeRoot string, release string) error {
	if err := os.MkdirAll(runtimeRoot, 0o755); err != nil {
		return fmt.Errorf("create SPIRE runtime root: %w", err)
	}
	next := filepath.Join(runtimeRoot, "current.next")
	current := filepath.Join(runtimeRoot, "current")
	if err := os.Remove(next); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale SPIRE current symlink: %w", err)
	}
	if err := os.Symlink(release, next); err != nil {
		return fmt.Errorf("create SPIRE current symlink: %w", err)
	}
	if stat, err := os.Lstat(current); err == nil && stat.Mode()&os.ModeSymlink == 0 {
		if err := os.RemoveAll(current); err != nil {
			return fmt.Errorf("remove non-symlink SPIRE current runtime: %w", err)
		}
	}
	if err := os.Rename(next, current); err != nil {
		return fmt.Errorf("publish SPIRE current runtime: %w", err)
	}
	return nil
}

func writeServerConfig(cfg config) error {
	body := fmt.Sprintf(`server {
  bind_address = %s
  bind_port = %q
  socket_path = %s
  trust_domain = %s
  data_dir = %s
  log_level = "INFO"
  ca_key_type = "ec-p256"
  default_x509_svid_ttl = "1h"
  default_jwt_svid_ttl = "5m"
  ca_ttl = "24h"
  ca_subject {
    country = ["US"]
    organization = ["Guardian Intelligence"]
    common_name = "Guardian SPIRE CA"
  }
}

plugins {
  DataStore "sql" {
    plugin_data {
      database_type = "sqlite3"
      connection_string = %s
    }
  }
  NodeAttestor "join_token" {
    plugin_data {}
  }
  KeyManager "disk" {
    plugin_data {
      keys_path = %s
    }
  }
}
`, quote(cfg.ServerAddress), strconv.Itoa(cfg.ServerPort), quote(cfg.ServerSocketPath), quote(cfg.TrustDomain), quote(cfg.ServerDataDir), quote(filepath.Join(cfg.ServerDataDir, "datastore.sqlite3")), quote(filepath.Join(cfg.ServerDataDir, "keys.json")))
	if err := writePrivateFile(cfg.ServerConfigPath, []byte(body), 0o640); err != nil {
		return err
	}
	uid, gid, err := ids(cfg.ServerUser, cfg.ServerGroup)
	if err != nil {
		return err
	}
	if err := os.Chown(cfg.ServerConfigPath, uid, gid); err != nil {
		return fmt.Errorf("chown SPIRE server config: %w", err)
	}
	return nil
}

func writeAgentConfig(cfg config) error {
	body := fmt.Sprintf(`agent {
  trust_domain = %s
  data_dir = %s
  log_level = "INFO"
  server_address = %s
  server_port = %q
  socket_path = %s
  insecure_bootstrap = true
}

plugins {
  NodeAttestor "join_token" {
    plugin_data {}
  }
  KeyManager "disk" {
    plugin_data {
      directory = %s
    }
  }
  WorkloadAttestor "unix" {
    plugin_data {}
  }
}
`, quote(cfg.TrustDomain), quote(cfg.AgentDataDir), quote(cfg.ServerAddress), strconv.Itoa(cfg.ServerPort), quote(cfg.AgentSocketPath), quote(cfg.AgentDataDir))
	return writePrivateFile(cfg.AgentConfigPath, []byte(body), 0o640)
}

func issueJoinToken(cfg config) error {
	tokenPath := filepath.Join(cfg.RuntimeRoot, "current/bin/spire-server")
	stdout, stderr, err := commandOutput(tokenPath, "token", "generate", "-socketPath", cfg.ServerSocketPath, "-spiffeID", cfg.AgentSPIFFEID, "-ttl", "600", "-output", "json")
	if err != nil {
		return fmt.Errorf("generate SPIRE join token: %w: %s", err, strings.TrimSpace(stderr))
	}
	var token struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(stdout), &token); err != nil {
		return fmt.Errorf("decode SPIRE join token: %w", err)
	}
	if strings.TrimSpace(token.Value) == "" {
		return errors.New("SPIRE server generated an empty join token")
	}
	return writePrivateFile(cfg.JoinTokenPath, []byte(token.Value+"\n"), 0o600)
}

func reconcileIdentityRegistry(repoRoot string, cfg config) (int, error) {
	registry, err := readIdentityRegistry(filepath.Join(repoRoot, filepath.FromSlash(cfg.IdentityRegistryArtifact)))
	if err != nil {
		return 0, err
	}
	for _, id := range registry.Identities {
		if err := validateIdentity(id); err != nil {
			return 0, err
		}
		if err := ensureIdentityAccount(id, cfg.WorkloadGroup); err != nil {
			return 0, err
		}
		selector, err := identitySelector(id)
		if err != nil {
			return 0, err
		}
		spiffeID := "spiffe://" + cfg.TrustDomain + id.Path
		if err := upsertEntry(cfg, id, spiffeID, selector); err != nil {
			return 0, err
		}
	}
	return len(registry.Identities), nil
}

func readIdentityRegistry(path string) (identityRegistry, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return identityRegistry{}, fmt.Errorf("read SPIRE identity registry: %w", err)
	}
	var registry identityRegistry
	if err := json.Unmarshal(body, &registry); err != nil {
		return identityRegistry{}, fmt.Errorf("decode SPIRE identity registry: %w", err)
	}
	if registry.SchemaVersion != 1 {
		return identityRegistry{}, fmt.Errorf("unsupported SPIRE identity registry schema version %d", registry.SchemaVersion)
	}
	return registry, nil
}

func validateIdentity(id identity) error {
	for name, value := range map[string]string{
		"entry_id": id.EntryID,
		"group":    id.Group,
		"key":      id.Key,
		"path":     id.Path,
		"selector": id.Selector,
		"user":     id.User,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("SPIRE identity %s is required", name)
		}
	}
	if !strings.HasPrefix(id.Path, "/") {
		return fmt.Errorf("SPIRE identity %s path must start with /", id.Key)
	}
	if id.X509SVIDTTLSeconds <= 0 {
		return fmt.Errorf("SPIRE identity %s x509_svid_ttl_seconds must be positive", id.Key)
	}
	if id.UIDPolicy.Kind != "allocated" && id.UIDPolicy.Kind != "fixed" {
		return fmt.Errorf("SPIRE identity %s has unsupported uid policy %q", id.Key, id.UIDPolicy.Kind)
	}
	if id.UIDPolicy.Kind == "fixed" && id.UIDPolicy.Value <= 0 {
		return fmt.Errorf("SPIRE identity %s fixed uid must be positive", id.Key)
	}
	return nil
}

func identitySelector(id identity) (string, error) {
	if id.Selector != "unix:uid" && id.Selector != "unix:gid" && id.Selector != "unix:user" && id.Selector != "unix:group" {
		return "", fmt.Errorf("SPIRE identity %s selector %q is unsupported", id.Key, id.Selector)
	}
	u, err := user.Lookup(id.User)
	if err != nil {
		return "", fmt.Errorf("lookup identity user %s: %w", id.User, err)
	}
	g, err := user.LookupGroup(id.Group)
	if err != nil {
		return "", fmt.Errorf("lookup identity group %s: %w", id.Group, err)
	}
	switch id.Selector {
	case "unix:uid":
		return "unix:uid:" + u.Uid, nil
	case "unix:gid":
		return "unix:gid:" + g.Gid, nil
	case "unix:user":
		return "unix:user:" + id.User, nil
	case "unix:group":
		return "unix:group:" + id.Group, nil
	default:
		panic("unreachable selector validation")
	}
}

func upsertEntry(cfg config, id identity, spiffeID string, selector string) error {
	server := filepath.Join(cfg.RuntimeRoot, "current/bin/spire-server")
	args := []string{
		"entry", "create",
		"-socketPath", cfg.ServerSocketPath,
		"-entryID", id.EntryID,
		"-parentID", cfg.AgentSPIFFEID,
		"-spiffeID", spiffeID,
		"-selector", selector,
		"-x509SVIDTTL", strconv.Itoa(id.X509SVIDTTLSeconds),
	}
	if _, _, err := commandOutput(server, args...); err == nil {
		return nil
	}
	args[1] = "update"
	if _, stderr, err := commandOutput(server, args...); err != nil {
		return fmt.Errorf("upsert SPIRE entry %s: %w: %s", id.EntryID, err, strings.TrimSpace(stderr))
	}
	return nil
}

func repairAgentSocketPermissions(cfg config) error {
	gid, err := groupID(cfg.WorkloadGroup)
	if err != nil {
		return err
	}
	socketDir := filepath.Dir(cfg.AgentSocketPath)
	if err := os.Chown(socketDir, 0, gid); err != nil {
		return fmt.Errorf("chown SPIRE agent socket dir: %w", err)
	}
	if err := os.Chmod(socketDir, 0o750); err != nil {
		return fmt.Errorf("chmod SPIRE agent socket dir: %w", err)
	}
	if err := os.Chown(cfg.AgentSocketPath, 0, gid); err != nil {
		return fmt.Errorf("chown SPIRE agent socket: %w", err)
	}
	if err := os.Chmod(cfg.AgentSocketPath, 0o770); err != nil {
		return fmt.Errorf("chmod SPIRE agent socket: %w", err)
	}
	return nil
}

func waitForCommand(timeout time.Duration, name string, args ...string) error {
	deadline := time.Now().Add(timeout)
	var last string
	for {
		_, stderr, err := commandOutput(name, args...)
		if err == nil {
			return nil
		}
		last = strings.TrimSpace(stderr)
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s %s: %s", name, strings.Join(args, " "), last)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func waitForFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		stat, err := os.Stat(path)
		if err == nil && stat.Mode().IsRegular() && stat.Size() > 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s", path)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func execAsUser(userName string, command []string) error {
	if os.Geteuid() != 0 {
		return errors.New("spire-recover exec must run as root")
	}
	u, err := user.Lookup(userName)
	if err != nil {
		return fmt.Errorf("lookup user %s: %w", userName, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return fmt.Errorf("parse uid %s: %w", u.Uid)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return fmt.Errorf("parse gid %s: %w", u.Gid)
	}
	groupIDs, err := u.GroupIds()
	if err != nil {
		return fmt.Errorf("load supplementary groups for %s: %w", userName, err)
	}
	groups := make([]int, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		group, err := strconv.Atoi(groupID)
		if err != nil {
			return fmt.Errorf("parse supplementary gid %s: %w", groupID)
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
		return 0, 0, fmt.Errorf("parse uid %s: %w", u.Uid)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse gid %s: %w", g.Gid)
	}
	return uid, gid, nil
}

func groupID(groupName string) (int, error) {
	g, err := user.LookupGroup(groupName)
	if err != nil {
		return 0, fmt.Errorf("lookup group %s: %w", groupName, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, fmt.Errorf("parse gid %s: %w", g.Gid)
	}
	return gid, nil
}

func writeReport(path string, rep report) error {
	body, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("encode SPIRE report: %w", err)
	}
	body = append(body, '\n')
	return writePrivateFile(path, body, 0o640)
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

func modeOrDefault(mode int64, fallback os.FileMode) os.FileMode {
	if mode == 0 {
		return fallback
	}
	return os.FileMode(mode).Perm()
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	var out []string
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func quote(value string) string {
	return strconv.Quote(value)
}

func conditionTrue(t string, reason string, message string) condition {
	return condition{Type: t, Status: "True", Reason: reason, Resource: "spire", Message: message}
}

func conditionFalse(t string, reason string, message string) condition {
	return condition{Type: t, Status: "False", Reason: reason, Resource: "spire", Message: message}
}

func command(name string, args ...string) error {
	_, stderr, err := commandOutput(name, args...)
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr))
	}
	return nil
}

func commandOutput(name string, args ...string) (string, string, error) {
	cmd := exec.Command(name, args...)
	var stdout strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}
