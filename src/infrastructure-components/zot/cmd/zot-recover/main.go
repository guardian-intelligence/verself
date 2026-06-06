package main

import (
	"archive/tar"
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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

	"golang.org/x/crypto/bcrypt"
)

const (
	apiVersion      = "zot.guardianintelligence.org/v1alpha1"
	kind            = "ZotRegistry"
	defaultRepoRoot = "/home/ubuntu/.local/state/guardian/repo/current"
	defaultResource = "zot"
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
	RuntimeArtifact string `json:"runtimeArtifact"`
	RuntimeRoot     string `json:"runtimeRoot"`
	ConfigPath      string `json:"configPath"`
	StorageDir      string `json:"storageDir"`
	HtpasswdPath    string `json:"htpasswdPath"`
	ReportPath      string `json:"reportPath"`
	User            string `json:"user"`
	Group           string `json:"group"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Realm           string `json:"realm"`
	LogLevel        string `json:"logLevel"`
	PublisherUser   string `json:"publisherUser"`
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
		fmt.Fprintln(os.Stderr, "zot-recover: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: zot-recover <recover|server> [flags]")
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
		if err := verifyConfig(cfg); err != nil {
			return err
		}
		if err := reclaimStaleServer(cfg); err != nil {
			return err
		}
		rep := report{
			Component:             "zot",
			ResourceName:          opts.resourceName,
			RuntimeArtifactDigest: digest,
			Conditions: []condition{
				conditionTrue("ZotRuntimeInstalled", "RuntimeReady", "repo-built Zot runtime is installed"),
				conditionTrue("ZotConfigWritten", "ConfigReady", "Zot config and htpasswd are written"),
				conditionTrue("ZotStaleProcessReclaimed", "PortAvailable", "Zot's configured port is available for Nomad"),
				conditionTrue("ZotRecoveryComplete", "Recovered", "Zot is ready for Nomad to start"),
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
	fs := flag.NewFlagSet("zot-recover", flag.ContinueOnError)
	fs.StringVar(&opts.repoRoot, "repo-root", opts.repoRoot, "Boarded repo root.")
	fs.StringVar(&opts.resourceGraph, "resource-graph", "", "Guardian resource graph document path.")
	fs.StringVar(&opts.resourceName, "resource-name", opts.resourceName, "ZotRegistry resource name.")
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
		return config{}, fmt.Errorf("decode ZotRegistry spec: %w", err)
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
		"realm":           cfg.Realm,
		"logLevel":        cfg.LogLevel,
		"publisherUser":   cfg.PublisherUser,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("ZotRegistry.spec.%s is required", name)
		}
	}
	if filepath.IsAbs(cfg.RuntimeArtifact) || strings.Contains(filepath.ToSlash(cfg.RuntimeArtifact), "../") {
		return errors.New("ZotRegistry.spec.runtimeArtifact must be repo-relative")
	}
	for name, value := range map[string]string{
		"runtimeRoot":  cfg.RuntimeRoot,
		"configPath":   cfg.ConfigPath,
		"storageDir":   cfg.StorageDir,
		"htpasswdPath": cfg.HtpasswdPath,
		"reportPath":   cfg.ReportPath,
	} {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("ZotRegistry.spec.%s must be an absolute path", name)
		}
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return errors.New("ZotRegistry.spec.port must be between 1 and 65535")
	}
	if strings.ContainsAny(cfg.PublisherUser, ":\n\r") {
		return errors.New("ZotRegistry.spec.publisherUser cannot contain ':', CR, or LF")
	}
	return nil
}

func ensureAccount(cfg config) error {
	if err := ensureGroup(cfg.Group); err != nil {
		return err
	}
	if _, err := user.Lookup(cfg.User); err != nil {
		if _, ok := err.(user.UnknownUserError); !ok {
			return fmt.Errorf("lookup Zot user: %w", err)
		}
		if err := command("/usr/sbin/useradd", "--system", "--gid", cfg.Group, "--home-dir", cfg.StorageDir, "--shell", "/usr/sbin/nologin", "--no-create-home", cfg.User); err != nil {
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
		return fmt.Errorf("lookup Zot user: %w", err)
	}
	serviceGroup, err := user.LookupGroup(cfg.Group)
	if err != nil {
		return fmt.Errorf("lookup Zot group: %w", err)
	}
	uid, err := parseID(serviceUser.Uid, "uid")
	if err != nil {
		return err
	}
	gid, err := parseID(serviceGroup.Gid, "gid")
	if err != nil {
		return err
	}
	if err := mkdirOwned(filepath.Dir(cfg.ConfigPath), 0, gid, 0o750); err != nil {
		return err
	}
	if err := mkdirOwned(cfg.StorageDir, uid, gid, 0o750); err != nil {
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
	if stat, err := os.Stat(cfg.HtpasswdPath); err == nil && stat.Mode().IsRegular() && stat.Size() > 0 {
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat Zot htpasswd: %w", err)
	}
	password := make([]byte, 32)
	if _, err := rand.Read(password); err != nil {
		return fmt.Errorf("generate Zot publisher password: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(password)
	hash, err := bcrypt.GenerateFromPassword([]byte(encoded), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash Zot publisher password: %w", err)
	}
	body := []byte(cfg.PublisherUser + ":" + string(hash) + "\n")
	gid, err := lookupGroupID(cfg.Group)
	if err != nil {
		return err
	}
	if err := writeOwnedFile(cfg.HtpasswdPath, body, 0, gid, 0o640); err != nil {
		return err
	}
	return nil
}

func writeConfig(cfg config) error {
	gid, err := lookupGroupID(cfg.Group)
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(zotConfig{
		DistSpecVersion: "1.1.1",
		Storage: zotStorageConfig{
			RootDirectory: cfg.StorageDir,
			Commit:        true,
			Dedupe:        true,
			GC:            true,
			GCDelay:       "1h",
			GCInterval:    "24h",
		},
		HTTP: zotHTTPConfig{
			Address: cfg.Host,
			Port:    cfg.Port,
			Realm:   cfg.Realm,
			Auth: zotAuthConfig{
				Htpasswd:  zotHtpasswdConfig{Path: cfg.HtpasswdPath},
				FailDelay: 1,
			},
			AccessControl: zotAccessControl{
				Repositories: map[string]zotRepositoryPolicy{
					"verself/**": {
						AnonymousPolicy: []string{"read"},
						Policies: []zotUserPolicy{{
							Users:   []string{cfg.PublisherUser},
							Actions: []string{"read", "create", "update", "delete"},
						}},
					},
					"**": {
						AnonymousPolicy: []string{"read"},
					},
				},
			},
		},
		Log: zotLogConfig{Level: cfg.LogLevel},
		Extensions: zotExtensions{
			Scrub: zotScrubConfig{Enable: true, Interval: "24h"},
		},
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Zot config: %w", err)
	}
	body = append(body, '\n')
	return writeOwnedFile(cfg.ConfigPath, body, 0, gid, 0o640)
}

type zotConfig struct {
	DistSpecVersion string           `json:"distSpecVersion"`
	Storage         zotStorageConfig `json:"storage"`
	HTTP            zotHTTPConfig    `json:"http"`
	Log             zotLogConfig     `json:"log"`
	Extensions      zotExtensions    `json:"extensions"`
}

type zotStorageConfig struct {
	RootDirectory string `json:"rootDirectory"`
	Commit        bool   `json:"commit"`
	Dedupe        bool   `json:"dedupe"`
	GC            bool   `json:"gc"`
	GCDelay       string `json:"gcDelay"`
	GCInterval    string `json:"gcInterval"`
}

type zotHTTPConfig struct {
	Address       string           `json:"address"`
	Port          int              `json:"port"`
	Realm         string           `json:"realm"`
	Auth          zotAuthConfig    `json:"auth"`
	AccessControl zotAccessControl `json:"accessControl"`
}

type zotAuthConfig struct {
	Htpasswd  zotHtpasswdConfig `json:"htpasswd"`
	FailDelay int               `json:"failDelay"`
}

type zotHtpasswdConfig struct {
	Path string `json:"path"`
}

type zotAccessControl struct {
	Repositories map[string]zotRepositoryPolicy `json:"repositories"`
}

type zotRepositoryPolicy struct {
	AnonymousPolicy []string        `json:"anonymousPolicy"`
	Policies        []zotUserPolicy `json:"policies,omitempty"`
}

type zotUserPolicy struct {
	Users   []string `json:"users"`
	Actions []string `json:"actions"`
}

type zotLogConfig struct {
	Level string `json:"level"`
}

type zotExtensions struct {
	Scrub zotScrubConfig `json:"scrub"`
}

type zotScrubConfig struct {
	Enable   bool   `json:"enable"`
	Interval string `json:"interval"`
}

func verifyConfig(cfg config) error {
	return command(filepath.Join(cfg.RuntimeRoot, "current/bin/zot"), "verify", cfg.ConfigPath)
}

func reclaimStaleServer(cfg config) error {
	pids, err := pidsListeningOnPort(cfg.Port)
	if err != nil {
		return err
	}
	for _, pid := range pids {
		cmdline, err := readCmdline(pid)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read process %d command line: %w", pid, err)
		}
		if !isZotProcess(cmdline) {
			return fmt.Errorf("configured Zot port %d is occupied by non-Zot process %d: %s", cfg.Port, pid, cmdline)
		}
		if err := terminateProcess(pid, 5*time.Second); err != nil {
			return fmt.Errorf("terminate stale Zot process %d: %w", pid, err)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		pids, err := pidsListeningOnPort(cfg.Port)
		if err != nil {
			return err
		}
		if len(pids) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("configured Zot port %d is still occupied by pids %v", cfg.Port, pids)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func pidsListeningOnPort(port int) ([]int, error) {
	inodes, err := socketInodesListeningOnPort(port)
	if err != nil {
		return nil, err
	}
	if len(inodes) == 0 {
		return nil, nil
	}
	return pidsForSocketInodes(inodes)
}

func socketInodesListeningOnPort(port int) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		file, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", path, err)
		}
		inodes, parseErr := listenerInodesFromTCPTable(file, port)
		closeErr := file.Close()
		if parseErr != nil {
			return nil, fmt.Errorf("parse %s: %w", path, parseErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close %s: %w", path, closeErr)
		}
		for inode := range inodes {
			out[inode] = struct{}{}
		}
	}
	return out, nil
}

func listenerInodesFromTCPTable(r io.Reader, port int) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 || fields[0] == "sl" {
			continue
		}
		if fields[3] != "0A" {
			continue
		}
		_, rawPort, ok := strings.Cut(fields[1], ":")
		if !ok {
			continue
		}
		parsed, err := strconv.ParseInt(rawPort, 16, 32)
		if err != nil {
			return nil, err
		}
		if int(parsed) == port {
			out[fields[9]] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func pidsForSocketInodes(inodes map[string]struct{}) ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("read /proc: %w", err)
	}
	seen := map[int]struct{}{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		fds, err := os.ReadDir(filepath.Join("/proc", entry.Name(), "fd"))
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read /proc/%d/fd: %w", pid, err)
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join("/proc", entry.Name(), "fd", fd.Name()))
			if err != nil {
				continue
			}
			if !strings.HasPrefix(target, "socket:[") || !strings.HasSuffix(target, "]") {
				continue
			}
			inode := strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")
			if _, ok := inodes[inode]; ok {
				seen[pid] = struct{}{}
			}
		}
	}
	pids := make([]int, 0, len(seen))
	for pid := range seen {
		pids = append(pids, pid)
	}
	return pids, nil
}

func readCmdline(pid int) (string, error) {
	body, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.ReplaceAll(string(body), "\x00", " ")), nil
}

func isZotProcess(cmdline string) bool {
	fields := strings.Fields(cmdline)
	return len(fields) > 0 && strings.Contains(filepath.Base(fields[0]), "zot")
}

func terminateProcess(pid int, timeout time.Duration) error {
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	deadline := time.Now().Add(timeout)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if !processAlive(pid) {
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	killDeadline := time.Now().Add(time.Second)
	for processAlive(pid) && time.Now().Before(killDeadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if processAlive(pid) {
		return errors.New("process survived SIGKILL")
	}
	return nil
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err != nil && !errors.Is(err, syscall.EPERM) {
		return false
	}
	body, statErr := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if errors.Is(statErr, os.ErrNotExist) {
		return false
	}
	if statErr != nil {
		return true
	}
	state, ok := processStateFromStat(string(body))
	return !ok || (state != "Z" && state != "X")
}

func processStateFromStat(body string) (string, bool) {
	_, tail, ok := strings.Cut(body, ") ")
	if !ok {
		return "", false
	}
	fields := strings.Fields(tail)
	if len(fields) == 0 {
		return "", false
	}
	return fields[0], true
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
	if err := os.MkdirAll(cfg.RuntimeRoot, 0o755); err != nil {
		return "", fmt.Errorf("create Zot runtime root: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(cfg.RuntimeRoot, "install.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return "", fmt.Errorf("open Zot runtime install lock: %w", err)
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return "", fmt.Errorf("lock Zot runtime install: %w", err)
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	release := filepath.Join(cfg.RuntimeRoot, "releases", strings.ReplaceAll(digest, ":", "-"))
	if !runtimeInstalled(release) {
		if err := extractRuntimeTar(artifact, release); err != nil {
			return "", err
		}
	}
	if err := os.Chmod(release, 0o755); err != nil {
		return "", fmt.Errorf("chmod Zot runtime release: %w", err)
	}
	if err := promoteRuntime(cfg.RuntimeRoot, release); err != nil {
		return "", err
	}
	return digest, nil
}

func runtimeInstalled(release string) bool {
	for _, rel := range []string{"bin/zot", "bin/zot-htpasswd", "bin/zot-recover"} {
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
		return "", fmt.Errorf("open Zot runtime artifact: %w", err)
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("hash Zot runtime artifact: %w", err)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func extractRuntimeTar(artifact string, release string) error {
	if err := os.MkdirAll(filepath.Dir(release), 0o755); err != nil {
		return fmt.Errorf("create Zot runtime release parent: %w", err)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(release), "."+filepath.Base(release)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create Zot runtime staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := extractTar(artifact, tmp); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return fmt.Errorf("chmod Zot runtime staging directory: %w", err)
	}
	if !runtimeInstalled(tmp) {
		return errors.New("Zot runtime artifact missing zot, zot-htpasswd, or zot-recover")
	}
	if err := os.RemoveAll(release); err != nil {
		return fmt.Errorf("remove stale Zot runtime release: %w", err)
	}
	if err := os.Rename(tmp, release); err != nil {
		return fmt.Errorf("publish Zot runtime release: %w", err)
	}
	return nil
}

func extractTar(artifact string, dest string) error {
	file, err := os.Open(artifact)
	if err != nil {
		return fmt.Errorf("open Zot runtime artifact: %w", err)
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
			return fmt.Errorf("read Zot runtime tar: %w", err)
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
		return fmt.Errorf("create Zot runtime root: %w", err)
	}
	next := filepath.Join(root, "current.next")
	current := filepath.Join(root, "current")
	_ = os.Remove(next)
	if err := os.Symlink(release, next); err != nil {
		return fmt.Errorf("create Zot runtime symlink: %w", err)
	}
	if err := os.Rename(next, current); err != nil {
		return fmt.Errorf("promote Zot runtime symlink: %w", err)
	}
	return nil
}

func execServer(cfg config) error {
	return execAsUserWithEnv(cfg.User, []string{
		filepath.Join(cfg.RuntimeRoot, "current/bin/zot"),
		"serve",
		cfg.ConfigPath,
	}, nil)
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
		return fmt.Errorf("encode Zot report: %w", err)
	}
	body = append(body, '\n')
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("write Zot report: %w", err)
	}
	return nil
}

func conditionTrue(t string, reason string, message string) condition {
	return condition{Type: t, Status: "True", Reason: reason, Resource: defaultResource, Message: message}
}
