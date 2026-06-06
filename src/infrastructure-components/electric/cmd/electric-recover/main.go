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
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	apiVersion      = "electric.guardianintelligence.org/v1alpha1"
	kind            = "ElectricDeployment"
	defaultRepoRoot = "/home/ubuntu/.local/state/guardian/repo/current"
	defaultResource = "electric"
	postgresMajor   = "16"
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type options struct {
	repoRoot         string
	resourceGraph    string
	resourceName     string
	instanceName     string
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
	RuntimeArtifact string             `json:"runtimeArtifact"`
	RuntimeRoot     string             `json:"runtimeRoot"`
	Image           string             `json:"image"`
	Containerd      containerdConfig   `json:"containerd"`
	Postgres        postgresConfig     `json:"postgres"`
	OpenBao         openBaoConfig      `json:"openBao"`
	Instances       []electricInstance `json:"instances"`
}

type containerdConfig struct {
	SocketPath string `json:"socketPath"`
	StateDir   string `json:"stateDir"`
	RootDir    string `json:"rootDir"`
}

type postgresConfig struct {
	RuntimeRoot string `json:"runtimeRoot"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
}

type openBaoConfig struct {
	Address string `json:"address"`
	CACert  string `json:"caCert"`
}

type electricInstance struct {
	Name                string    `json:"name"`
	ServiceName         string    `json:"serviceName"`
	StorageDir          string    `json:"storageDir"`
	Database            string    `json:"database"`
	DatabaseUser        string    `json:"databaseUser"`
	ConnectionLimit     int       `json:"connectionLimit"`
	ReplicationStreamID string    `json:"replicationStreamID"`
	DBPoolSize          int       `json:"dbPoolSize"`
	PGPasswordRef       objectRef `json:"pgPasswordRef"`
	APISecretRef        objectRef `json:"apiSecretRef"`
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
	cfg       config
	instances map[string]loadedInstance
}

type loadedInstance struct {
	spec             electricInstance
	pgPasswordSecret secretPathSpec
	apiSecret        secretPathSpec
}

type runtimeSecrets struct {
	pgPassword string
	apiSecret  string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "electric-recover: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: electric-recover <containerd|image|run> [flags]")
	}
	switch args[0] {
	case "containerd":
		opts, loaded, err := loadValidated(args[1:], false, false)
		if err != nil {
			return err
		}
		if os.Geteuid() != 0 {
			return errors.New("containerd must run as root")
		}
		if _, err := installRuntime(opts.repoRoot, loaded.cfg); err != nil {
			return err
		}
		return execContainerd(loaded.cfg)
	case "image":
		opts, loaded, err := loadValidated(args[1:], true, false)
		if err != nil {
			return err
		}
		if os.Geteuid() != 0 {
			return errors.New("image must run as root")
		}
		if _, err := installRuntime(opts.repoRoot, loaded.cfg); err != nil {
			return err
		}
		return ensureImage(loaded.cfg)
	case "run":
		opts, loaded, err := loadValidated(args[1:], true, true)
		if err != nil {
			return err
		}
		if os.Geteuid() != 0 {
			return errors.New("run must run as root")
		}
		if _, err := installRuntime(opts.repoRoot, loaded.cfg); err != nil {
			return err
		}
		instance := loaded.instances[opts.instanceName]
		secrets, err := readSecretsWithRetry(opts, loaded.cfg, instance)
		if err != nil {
			return err
		}
		if err := reconcilePostgresInstance(loaded.cfg, instance.spec, secrets); err != nil {
			return err
		}
		envFile, err := writeInstanceEnvFile(instance.spec, secrets, loaded.cfg.Postgres)
		if err != nil {
			return err
		}
		return execRunner(loaded.cfg, instance.spec, envFile)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func loadValidated(args []string, requireInstance bool, requireOpenBao bool) (options, loadedConfig, error) {
	opts, err := parseOptions(args)
	if err != nil {
		return options{}, loadedConfig{}, err
	}
	if requireInstance && opts.instanceName == "" {
		return options{}, loadedConfig{}, errors.New("--instance is required")
	}
	if requireOpenBao && opts.openBaoTokenFile == "" {
		return options{}, loadedConfig{}, errors.New("--openbao-token-file is required")
	}
	loaded, err := loadConfig(opts)
	if err != nil {
		return options{}, loadedConfig{}, err
	}
	if err := validateConfig(loaded.cfg); err != nil {
		return options{}, loadedConfig{}, err
	}
	if requireInstance {
		if _, ok := loaded.instances[opts.instanceName]; !ok {
			return options{}, loadedConfig{}, fmt.Errorf("ElectricDeployment missing instance %q", opts.instanceName)
		}
	}
	return opts, loaded, nil
}

func parseOptions(args []string) (options, error) {
	opts := options{repoRoot: defaultRepoRoot, resourceName: defaultResource}
	fs := flag.NewFlagSet("electric-recover", flag.ContinueOnError)
	fs.StringVar(&opts.repoRoot, "repo-root", opts.repoRoot, "Boarded repo root.")
	fs.StringVar(&opts.resourceGraph, "resource-graph", "", "Guardian resource graph document path.")
	fs.StringVar(&opts.resourceName, "resource-name", opts.resourceName, "ElectricDeployment resource name.")
	fs.StringVar(&opts.instanceName, "instance", "", "Electric instance name.")
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
	opts.instanceName = strings.TrimSpace(opts.instanceName)
	opts.openBaoTokenFile = strings.TrimSpace(opts.openBaoTokenFile)
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
	deployment, ok := resources[resourceKey(apiVersion, kind, opts.resourceName)]
	if !ok {
		return loadedConfig{}, fmt.Errorf("Guardian resource graph missing %s %q", kind, opts.resourceName)
	}
	var cfg config
	deploymentDecoder := json.NewDecoder(bytes.NewReader(deployment.Spec))
	deploymentDecoder.DisallowUnknownFields()
	if err := deploymentDecoder.Decode(&cfg); err != nil {
		return loadedConfig{}, fmt.Errorf("decode ElectricDeployment spec: %w", err)
	}
	instances := make(map[string]loadedInstance, len(cfg.Instances))
	for _, instance := range cfg.Instances {
		if strings.TrimSpace(instance.Name) == "" {
			return loadedConfig{}, errors.New("ElectricDeployment.spec.instances[].name is required")
		}
		if _, exists := instances[instance.Name]; exists {
			return loadedConfig{}, fmt.Errorf("duplicate Electric instance %q", instance.Name)
		}
		pgPassword, err := loadSecretRef(resources, instance.PGPasswordRef, instance.Name+".pgPasswordRef")
		if err != nil {
			return loadedConfig{}, err
		}
		apiSecret, err := loadSecretRef(resources, instance.APISecretRef, instance.Name+".apiSecretRef")
		if err != nil {
			return loadedConfig{}, err
		}
		instances[instance.Name] = loadedInstance{spec: instance, pgPasswordSecret: pgPassword, apiSecret: apiSecret}
	}
	return loadedConfig{cfg: cfg, instances: instances}, nil
}

func loadSecretRef(resources map[string]resource, ref objectRef, field string) (secretPathSpec, error) {
	resource, ok := resources[resourceKey(ref.APIVersion, ref.Kind, ref.Name)]
	if !ok {
		return secretPathSpec{}, fmt.Errorf("Guardian resource graph missing Electric %s %s/%s/%s", field, ref.APIVersion, ref.Kind, ref.Name)
	}
	if resource.APIVersion != "openbao.guardianintelligence.org/v1alpha1" || resource.Kind != "SecretPath" {
		return secretPathSpec{}, fmt.Errorf("ElectricDeployment.spec.instances[].%s must target openbao.guardianintelligence.org/v1alpha1/SecretPath", field)
	}
	var secret secretPathSpec
	decoder := json.NewDecoder(bytes.NewReader(resource.Spec))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&secret); err != nil {
		return secretPathSpec{}, fmt.Errorf("decode Electric SecretPath %q: %w", resource.Metadata.Name, err)
	}
	if err := validateSecretPath(secret); err != nil {
		return secretPathSpec{}, fmt.Errorf("Electric SecretPath %q: %w", resource.Metadata.Name, err)
	}
	return secret, nil
}

func validateConfig(cfg config) error {
	for name, value := range map[string]string{
		"runtimeArtifact":       cfg.RuntimeArtifact,
		"runtimeRoot":           cfg.RuntimeRoot,
		"image":                 cfg.Image,
		"containerd.socketPath": cfg.Containerd.SocketPath,
		"containerd.stateDir":   cfg.Containerd.StateDir,
		"containerd.rootDir":    cfg.Containerd.RootDir,
		"postgres.runtimeRoot":  cfg.Postgres.RuntimeRoot,
		"postgres.host":         cfg.Postgres.Host,
		"openBao.address":       cfg.OpenBao.Address,
		"openBao.caCert":        cfg.OpenBao.CACert,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("ElectricDeployment.spec.%s is required", name)
		}
	}
	if filepath.IsAbs(cfg.RuntimeArtifact) || strings.Contains(filepath.ToSlash(cfg.RuntimeArtifact), "../") {
		return errors.New("ElectricDeployment.spec.runtimeArtifact must be repo-relative")
	}
	for name, value := range map[string]string{
		"runtimeRoot":           cfg.RuntimeRoot,
		"containerd.socketPath": cfg.Containerd.SocketPath,
		"containerd.stateDir":   cfg.Containerd.StateDir,
		"containerd.rootDir":    cfg.Containerd.RootDir,
		"postgres.runtimeRoot":  cfg.Postgres.RuntimeRoot,
		"openBao.caCert":        cfg.OpenBao.CACert,
	} {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("ElectricDeployment.spec.%s must be an absolute path", name)
		}
	}
	if cfg.Postgres.Port <= 0 || cfg.Postgres.Port > 65535 {
		return errors.New("ElectricDeployment.spec.postgres.port must be between 1 and 65535")
	}
	if !strings.HasPrefix(cfg.OpenBao.Address, "http://") && !strings.HasPrefix(cfg.OpenBao.Address, "https://") {
		return errors.New("ElectricDeployment.spec.openBao.address must be an HTTP URL")
	}
	if len(cfg.Instances) == 0 {
		return errors.New("ElectricDeployment.spec.instances must not be empty")
	}
	for _, instance := range cfg.Instances {
		if err := validateInstance(instance); err != nil {
			return err
		}
	}
	return nil
}

func validateInstance(instance electricInstance) error {
	for name, value := range map[string]string{
		"name":                instance.Name,
		"serviceName":         instance.ServiceName,
		"storageDir":          instance.StorageDir,
		"database":            instance.Database,
		"databaseUser":        instance.DatabaseUser,
		"replicationStreamID": instance.ReplicationStreamID,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("ElectricDeployment.spec.instances[].%s is required", name)
		}
	}
	if !filepath.IsAbs(instance.StorageDir) {
		return fmt.Errorf("ElectricDeployment.spec.instances[%s].storageDir must be an absolute path", instance.Name)
	}
	if !identifierPattern.MatchString(instance.Database) || !identifierPattern.MatchString(instance.DatabaseUser) {
		return fmt.Errorf("ElectricDeployment.spec.instances[%s] database and databaseUser must be PostgreSQL identifiers", instance.Name)
	}
	if instance.ConnectionLimit <= 0 || instance.DBPoolSize <= 0 {
		return fmt.Errorf("ElectricDeployment.spec.instances[%s] connectionLimit and dbPoolSize must be positive", instance.Name)
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
	if err := os.MkdirAll(cfg.RuntimeRoot, 0o755); err != nil {
		return "", fmt.Errorf("create Electric runtime root: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(cfg.RuntimeRoot, ".install.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return "", fmt.Errorf("open Electric runtime install lock: %w", err)
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return "", fmt.Errorf("lock Electric runtime install: %w", err)
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	release := filepath.Join(cfg.RuntimeRoot, "releases", strings.ReplaceAll(digest, ":", "-"))
	if !runtimeInstalled(release) {
		if err := extractRuntimeTar(artifact, release); err != nil {
			return "", err
		}
	}
	if err := os.Chmod(release, 0o755); err != nil {
		return "", fmt.Errorf("chmod Electric runtime release: %w", err)
	}
	if err := promoteRuntime(cfg.RuntimeRoot, release); err != nil {
		return "", err
	}
	return digest, nil
}

func runtimeInstalled(release string) bool {
	for _, rel := range []string{"bin/containerd", "bin/containerd-shim-runc-v2", "bin/ctr", "bin/electric-nomad-runner", "bin/electric-recover", "bin/runc"} {
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
		return "", fmt.Errorf("open Electric runtime artifact: %w", err)
	}
	defer func() { _ = file.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("hash Electric runtime artifact: %w", err)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func extractRuntimeTar(artifact string, release string) error {
	if err := os.MkdirAll(filepath.Dir(release), 0o755); err != nil {
		return fmt.Errorf("create Electric runtime release parent: %w", err)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(release), "."+filepath.Base(release)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create Electric runtime staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := extractTar(artifact, tmp); err != nil {
		return err
	}
	if !runtimeInstalled(tmp) {
		return errors.New("Electric runtime artifact missing runtime binaries")
	}
	if err := os.RemoveAll(release); err != nil {
		return fmt.Errorf("remove stale Electric runtime release: %w", err)
	}
	if err := os.Rename(tmp, release); err != nil {
		return fmt.Errorf("publish Electric runtime release: %w", err)
	}
	return nil
}

func extractTar(artifact string, dest string) error {
	file, err := os.Open(artifact)
	if err != nil {
		return fmt.Errorf("open Electric runtime artifact: %w", err)
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
			return fmt.Errorf("read Electric runtime tar: %w", err)
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
		return fmt.Errorf("create Electric runtime root: %w", err)
	}
	next := filepath.Join(root, "current.next")
	current := filepath.Join(root, "current")
	_ = os.Remove(next)
	if err := os.Symlink(release, next); err != nil {
		return fmt.Errorf("create Electric runtime symlink: %w", err)
	}
	if info, err := os.Lstat(current); err == nil && info.Mode()&os.ModeSymlink == 0 {
		if err := os.RemoveAll(current); err != nil {
			_ = os.Remove(next)
			return fmt.Errorf("remove old Electric runtime current path: %w", err)
		}
	}
	if err := os.Rename(next, current); err != nil {
		_ = os.Remove(next)
		return fmt.Errorf("promote Electric runtime symlink: %w", err)
	}
	return nil
}

func execContainerd(cfg config) error {
	if err := reclaimStaleContainerd(cfg); err != nil {
		return err
	}
	_ = os.Remove(cfg.Containerd.SocketPath)
	_ = os.Remove(cfg.Containerd.SocketPath + ".ttrpc")
	for _, dir := range []string{filepath.Dir(cfg.Containerd.SocketPath), cfg.Containerd.StateDir, cfg.Containerd.RootDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create Electric containerd directory %s: %w", dir, err)
		}
	}
	binDir := filepath.Join(cfg.RuntimeRoot, "current/bin")
	command := filepath.Join(binDir, "containerd")
	argv := []string{
		command,
		"--address", cfg.Containerd.SocketPath,
		"--root", cfg.Containerd.RootDir,
		"--state", cfg.Containerd.StateDir,
	}
	env := append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"))
	return syscall.Exec(command, argv, env)
}

func reclaimStaleContainerd(cfg config) error {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return fmt.Errorf("read /proc: %w", err)
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		cmdline, err := readCmdline(pid)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
				continue
			}
			return fmt.Errorf("read process %d command line: %w", pid, err)
		}
		if !isElectricContainerdProcess(cmdline, cfg.Containerd.SocketPath, cfg.Containerd.RootDir) {
			continue
		}
		if err := terminateProcess(pid); err != nil {
			return fmt.Errorf("terminate stale Electric containerd process %d: %w", pid, err)
		}
	}
	return nil
}

func readCmdline(pid int) (string, error) {
	body, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.ReplaceAll(string(body), "\x00", " ")), nil
}

func isElectricContainerdProcess(cmdline string, socketPath string, rootDir string) bool {
	fields := strings.Fields(cmdline)
	if len(fields) == 0 {
		return false
	}
	switch filepath.Base(fields[0]) {
	case "containerd":
		return commandHasFlagValue(fields, "--address", socketPath) && commandHasFlagValue(fields, "--root", rootDir)
	case "containerd-shim-runc-v2":
		return commandHasFlagValue(fields, "-address", socketPath) || commandHasFlagValue(fields, "--address", socketPath)
	default:
		return false
	}
}

func commandHasFlagValue(fields []string, flag string, value string) bool {
	for i, field := range fields {
		if field == flag && i+1 < len(fields) && fields[i+1] == value {
			return true
		}
		if strings.HasPrefix(field, flag+"=") && strings.TrimPrefix(field, flag+"=") == value {
			return true
		}
	}
	return false
}

func terminateProcess(pid int) error {
	if !processAlive(pid) {
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	deadline := time.Now().Add(3 * time.Second)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if !processAlive(pid) {
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	killDeadline := time.Now().Add(3 * time.Second)
	for processAlive(pid) && time.Now().Before(killDeadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if processAlive(pid) {
		return errors.New("process survived SIGKILL")
	}
	return nil
}

func processAlive(pid int) bool {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return false
	}
	body, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return true
	}
	state, ok := processStateFromStat(string(body))
	return !ok || state != "Z"
}

func processStateFromStat(body string) (string, bool) {
	end := strings.LastIndex(body, ")")
	if end < 0 || end+2 >= len(body) {
		return "", false
	}
	rest := strings.TrimSpace(body[end+1:])
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", false
	}
	return fields[0], true
}

func ensureImage(cfg config) error {
	if err := os.MkdirAll(cfg.Containerd.StateDir, 0o755); err != nil {
		return fmt.Errorf("create Electric containerd state dir: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(cfg.Containerd.StateDir, "image-pull.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open Electric image pull lock: %w", err)
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock Electric image pull: %w", err)
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	if err := waitForContainerd(cfg); err != nil {
		return err
	}
	if err := ctr(cfg, "images", "inspect", cfg.Image); err == nil {
		return nil
	}
	return ctr(cfg, "images", "pull", "--platform", "linux/amd64", cfg.Image)
}

func waitForContainerd(cfg config) error {
	return retry("Electric containerd", 120*time.Second, func() error {
		return ctr(cfg, "version")
	})
}

func ctr(cfg config, args ...string) error {
	allArgs := append([]string{"--address", cfg.Containerd.SocketPath}, args...)
	cmd := exec.Command(filepath.Join(cfg.RuntimeRoot, "current/bin/ctr"), allArgs...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		if len(output) > 0 {
			_, _ = os.Stdout.Write(output)
		}
		return nil
	}
	if len(output) > 0 {
		_, _ = os.Stderr.Write(output)
	}
	return fmt.Errorf("ctr %s: %w", strings.Join(args, " "), err)
}

func readSecretsWithRetry(opts options, cfg config, instance loadedInstance) (runtimeSecrets, error) {
	return retryValue("Electric OpenBao secrets", 120*time.Second, func() (runtimeSecrets, error) {
		token, err := readTokenFile(opts.openBaoTokenFile)
		if err != nil {
			return runtimeSecrets{}, err
		}
		client, err := newOpenBaoClient(cfg.OpenBao)
		if err != nil {
			return runtimeSecrets{}, err
		}
		pgPassword, err := client.readOrCreateSecret(token, instance.pgPasswordSecret)
		if err != nil {
			return runtimeSecrets{}, err
		}
		apiSecret, err := client.readOrCreateSecret(token, instance.apiSecret)
		if err != nil {
			return runtimeSecrets{}, err
		}
		return runtimeSecrets{pgPassword: pgPassword, apiSecret: apiSecret}, nil
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
	body, err := os.ReadFile(cfg.CACert)
	if err != nil {
		return nil, fmt.Errorf("read OpenBao CA cert: %w", err)
	}
	if !pool.AppendCertsFromPEM(body) {
		return nil, fmt.Errorf("OpenBao CA cert %s did not contain a PEM certificate", cfg.CACert)
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

func reconcilePostgresInstance(cfg config, instance electricInstance, secrets runtimeSecrets) error {
	if err := waitForPostgres(cfg); err != nil {
		return err
	}
	if !identifierPattern.MatchString(instance.DatabaseUser) || !identifierPattern.MatchString(instance.Database) {
		return fmt.Errorf("invalid Electric Postgres identifiers for %s", instance.Name)
	}
	if !roleExists(cfg, instance.DatabaseUser) {
		if err := psqlQuery(cfg, "create role "+quoteIdent(instance.DatabaseUser)+" login replication connection limit "+strconv.Itoa(instance.ConnectionLimit)+" password "+quoteLiteral(secrets.pgPassword)+";"); err != nil {
			return err
		}
	} else {
		if err := psqlQuery(cfg, "alter role "+quoteIdent(instance.DatabaseUser)+" with login replication connection limit "+strconv.Itoa(instance.ConnectionLimit)+" password "+quoteLiteral(secrets.pgPassword)+";"); err != nil {
			return err
		}
	}
	if !databaseExists(cfg, instance.Database) {
		if err := psqlQuery(cfg, "create database "+quoteIdent(instance.Database)+" owner "+quoteIdent(instance.DatabaseUser)+";"); err != nil {
			return err
		}
	}
	return nil
}

func waitForPostgres(cfg config) error {
	return retry("PostgreSQL", 120*time.Second, func() error {
		return psqlQuery(cfg, "select 1;")
	})
}

func roleExists(cfg config, name string) bool {
	result, err := psqlQueryOutput(cfg, []string{"-A", "-t"}, "select 1 from pg_roles where rolname = "+quoteLiteral(name)+";")
	return err == nil && strings.TrimSpace(result) == "1"
}

func databaseExists(cfg config, name string) bool {
	result, err := psqlQueryOutput(cfg, []string{"-A", "-t"}, "select 1 from pg_database where datname = "+quoteLiteral(name)+";")
	return err == nil && strings.TrimSpace(result) == "1"
}

func psqlQuery(cfg config, query string) error {
	_, err := psqlQueryOutput(cfg, nil, query)
	return err
}

func psqlQueryOutput(cfg config, extraArgs []string, query string) (string, error) {
	psqlPath := filepath.Join(cfg.Postgres.RuntimeRoot, "current/usr/lib/postgresql", postgresMajor, "bin/psql")
	psqlArgs := []string{
		psqlPath,
		"-h", cfg.Postgres.Host,
		"-p", strconv.Itoa(cfg.Postgres.Port),
		"-d", "postgres",
		"-v", "ON_ERROR_STOP=1",
	}
	psqlArgs = append(psqlArgs, extraArgs...)
	psqlArgs = append(psqlArgs, "-f", "-")
	command := []string{
		"-u", "postgres",
		"--",
		"/usr/bin/env",
		"HOME=/var/lib/postgresql",
		"LD_LIBRARY_PATH=" + postgresLDLibraryPath(cfg.Postgres.RuntimeRoot),
	}
	command = append(command, psqlArgs...)
	cmd := exec.Command("/usr/sbin/runuser", command...)
	cmd.Env = os.Environ()
	cmd.Stdin = strings.NewReader(query)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("psql failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func postgresLDLibraryPath(runtimeRoot string) string {
	current := filepath.Join(runtimeRoot, "current")
	return filepath.Join(current, "usr/lib/x86_64-linux-gnu") + ":" + filepath.Join(current, "usr/lib/postgresql", postgresMajor, "lib")
}

func quoteIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func quoteLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}

func writeInstanceEnvFile(instance electricInstance, secrets runtimeSecrets, postgres postgresConfig) (string, error) {
	secretDir := strings.TrimSpace(os.Getenv("NOMAD_SECRETS_DIR"))
	if secretDir == "" {
		return "", errors.New("NOMAD_SECRETS_DIR is required")
	}
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		return "", fmt.Errorf("create Nomad secrets directory: %w", err)
	}
	path := filepath.Join(secretDir, instance.Name+".env")
	dsn := "postgresql://" + url.QueryEscape(instance.DatabaseUser) + ":" + url.QueryEscape(secrets.pgPassword) + "@" + netHost(postgres.Host) + ":" + strconv.Itoa(postgres.Port) + "/" + url.PathEscape(instance.Database)
	body := []byte("DATABASE_URL=" + dsn + "\nELECTRIC_SECRET=" + secrets.apiSecret + "\n")
	tmp, err := os.CreateTemp(secretDir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("create Electric env file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write Electric env file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close Electric env file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return "", fmt.Errorf("chmod Electric env file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return "", fmt.Errorf("promote Electric env file: %w", err)
	}
	return path, nil
}

func netHost(host string) string {
	if strings.HasPrefix(host, "/") {
		return "127.0.0.1"
	}
	return host
}

func execRunner(cfg config, instance electricInstance, envFile string) error {
	if err := os.MkdirAll(instance.StorageDir, 0o750); err != nil {
		return fmt.Errorf("create Electric storage dir: %w", err)
	}
	binDir := filepath.Join(cfg.RuntimeRoot, "current/bin")
	command := filepath.Join(binDir, "electric-nomad-runner")
	argv := []string{
		command,
		"--ctr=" + filepath.Join(binDir, "ctr"),
		"--containerd-address=" + cfg.Containerd.SocketPath,
		"--image=" + cfg.Image,
		"--env-file=" + envFile,
		"--storage-dir=" + instance.StorageDir,
		"--instance-id=" + instance.Name,
		"--replication-stream-id=" + instance.ReplicationStreamID,
		"--db-pool-size=" + strconv.Itoa(instance.DBPoolSize),
		"--runc-binary=" + filepath.Join(binDir, "runc"),
	}
	env := append(os.Environ(),
		"OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4317",
		"OTEL_RESOURCE_ATTRIBUTES=verself.supervisor=nomad",
		"OTEL_SERVICE_NAME="+instance.ServiceName,
		"VERSELF_SUPERVISOR=nomad",
	)
	return syscall.Exec(command, argv, env)
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

func resourceKey(apiVersion string, kind string, name string) string {
	return apiVersion + "/" + kind + "/" + name
}
