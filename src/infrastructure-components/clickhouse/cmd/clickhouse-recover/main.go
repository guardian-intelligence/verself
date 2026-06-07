package main

import (
	"archive/tar"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	apiVersion      = "clickhouse.guardianintelligence.org/v1alpha1"
	kind            = "ClickHouseCluster"
	defaultRepoRoot = "/home/ubuntu/.local/state/guardian/repo"
	defaultResource = "clickhouse"

	monitorProbeTimeout  = 45 * time.Second
	monitorProbeInterval = time.Second
	monitorLoopInterval  = 10 * time.Second
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
	DataDir         string `json:"dataDir"`
	BackupDir       string `json:"backupDir"`
	BackupDiskName  string `json:"backupDiskName"`
	LogDir          string `json:"logDir"`
	Host            string `json:"host"`
	Port            int    `json:"port"`

	ConfigPath string `json:"configPath"`
	TLSDir     string `json:"tlsDir"`
	PIDPath    string `json:"pidPath"`

	ServerUser  string `json:"serverUser"`
	ServerGroup string `json:"serverGroup"`

	OperatorUser             string `json:"operatorUser"`
	OperatorGroup            string `json:"operatorGroup"`
	OperatorDatabaseUser     string `json:"operatorDatabaseUser"`
	OperatorClientConfigPath string `json:"operatorClientConfigPath"`
	OperatorCAPath           string `json:"operatorCAPath"`

	SPIFFE struct {
		TrustDomain          string `json:"trustDomain"`
		ServicePrefix        string `json:"servicePrefix"`
		AgentSocket          string `json:"agentSocket"`
		HelperPath           string `json:"helperPath"`
		ServerID             string `json:"serverID"`
		OperatorID           string `json:"operatorID"`
		ServerDir            string `json:"serverDir"`
		OperatorDir          string `json:"operatorDir"`
		SPIREWorkloadGroup   string `json:"spireWorkloadGroup"`
		ServerHelperConfig   string `json:"serverHelperConfig"`
		OperatorHelperConfig string `json:"operatorHelperConfig"`
		BundleReloadState    string `json:"bundleReloadState"`
	} `json:"spiffe"`

	Systemd struct {
		ServerServicePath         string `json:"serverServicePath"`
		ServerHelperServicePath   string `json:"serverHelperServicePath"`
		OperatorHelperServicePath string `json:"operatorHelperServicePath"`
		BundleReloadServicePath   string `json:"bundleReloadServicePath"`
		BundleReloadPathUnitPath  string `json:"bundleReloadPathUnitPath"`
	} `json:"systemd"`

	Migrations struct {
		BootstrapSchemaPath string `json:"bootstrapSchemaPath"`
		DeltaDir            string `json:"deltaDir"`
		StateDir            string `json:"stateDir"`
	} `json:"migrations"`

	ClientCAProjections []caProjection `json:"clientCAProjections"`
	ReportPath          string         `json:"reportPath"`
}

type caProjection struct {
	Path          string `json:"path"`
	Group         string `json:"group"`
	Mode          string `json:"mode"`
	DirectoryMode string `json:"directoryMode"`
}

type report struct {
	Component             string      `json:"component"`
	ResourceName          string      `json:"resourceName"`
	CheckedAt             string      `json:"checkedAt"`
	RuntimeArtifactDigest string      `json:"runtimeArtifactDigest,omitempty"`
	BootstrapFingerprint  string      `json:"bootstrapFingerprint,omitempty"`
	AppliedMigrations     []string    `json:"appliedMigrations,omitempty"`
	Conditions            []condition `json:"conditions"`
}

type condition struct {
	Type     string `json:"type"`
	Status   string `json:"status"`
	Reason   string `json:"reason"`
	Message  string `json:"message,omitempty"`
	Resource string `json:"resource,omitempty"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "clickhouse-recover: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) < 1 {
		return errors.New("usage: clickhouse-recover <recover|monitor> [flags]")
	}
	switch args[0] {
	case "recover":
		opts, err := parseOptions("clickhouse-recover recover", args[1:])
		if err != nil {
			return err
		}
		return recover(opts, stdout)
	case "monitor":
		opts, err := parseOptions("clickhouse-recover monitor", args[1:])
		if err != nil {
			return err
		}
		return monitor(opts)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func parseOptions(name string, args []string) (options, error) {
	opts := options{repoRoot: defaultRepoRoot, resourceName: defaultResource}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.StringVar(&opts.repoRoot, "repo-root", opts.repoRoot, "boarded repo root")
	fs.StringVar(&opts.resourceGraph, "resource-graph", "", "Guardian resource graph JSON")
	fs.StringVar(&opts.resourceName, "resource-name", opts.resourceName, "ClickHouseCluster resource name")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	opts.repoRoot = strings.TrimSpace(opts.repoRoot)
	opts.resourceName = strings.TrimSpace(opts.resourceName)
	if opts.repoRoot == "" || !filepath.IsAbs(opts.repoRoot) {
		return options{}, errors.New("--repo-root must be an absolute path")
	}
	if opts.resourceGraph == "" {
		opts.resourceGraph = filepath.Join(opts.repoRoot, "workspace/.guardian/fly/document.json")
	}
	if !filepath.IsAbs(opts.resourceGraph) {
		return options{}, errors.New("--resource-graph must be an absolute path")
	}
	if opts.resourceName == "" {
		return options{}, errors.New("--resource-name is required")
	}
	return opts, nil
}

func recover(opts options, stdout io.Writer) error {
	cfg, err := loadConfig(opts)
	if err != nil {
		return err
	}
	rep := report{
		Component:    "clickhouse",
		ResourceName: opts.resourceName,
		CheckedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
	finish := func(err error) error {
		if err != nil {
			rep.Conditions = append(rep.Conditions, conditionFalse("ClickHouseRecoveryComplete", "RecoveryFailed", err.Error(), opts.resourceName))
		}
		if writeErr := writeReport(stdout, cfg.ReportPath, rep); writeErr != nil && err == nil {
			return writeErr
		}
		if err != nil {
			return err
		}
		return nil
	}
	if err := validateConfig(cfg); err != nil {
		return finish(err)
	}
	artifact, err := resolveRepoPath(opts.repoRoot, cfg.RuntimeArtifact)
	if err != nil {
		return finish(err)
	}
	digest, err := installRuntime(artifact, cfg.RuntimeRoot)
	if err != nil {
		return finish(err)
	}
	rep.RuntimeArtifactDigest = digest
	rep.Conditions = append(rep.Conditions, conditionTrue("ClickHouseRuntimeInstalled", "RuntimeReady", "repo-built ClickHouse runtime is installed", opts.resourceName))
	if err := convergeHost(cfg); err != nil {
		return finish(err)
	}
	rep.Conditions = append(rep.Conditions, conditionTrue("ClickHouseHostPrepared", "HostReady", "users, directories, TLS, SPIFFE helpers, and systemd units are prepared", opts.resourceName))
	if err := startServices(cfg); err != nil {
		return finish(err)
	}
	rep.Conditions = append(rep.Conditions, conditionTrue("ClickHouseServerAvailable", "QuerySucceeded", "ClickHouse accepted an operator query", opts.resourceName))
	bootstrapFingerprint, applied, err := applyMigrations(opts.repoRoot, cfg)
	if err != nil {
		return finish(err)
	}
	rep.BootstrapFingerprint = bootstrapFingerprint
	rep.AppliedMigrations = applied
	rep.Conditions = append(rep.Conditions, conditionTrue("ClickHouseSchemaReconciled", "MigrationsApplied", "ClickHouse schema has been reconciled from repo migrations", opts.resourceName))
	rep.Conditions = append(rep.Conditions, conditionTrue("ClickHouseRecoveryComplete", "Recovered", "ClickHouse is ready", opts.resourceName))
	return finish(nil)
}

func monitor(opts options) error {
	cfg, err := loadConfig(opts)
	if err != nil {
		return err
	}
	if err := validateConfig(cfg); err != nil {
		return err
	}
	for {
		if err := waitForMonitorProbe(monitorProbeTimeout, monitorProbeInterval, func() error {
			return clickhouseMonitorProbe(cfg)
		}); err != nil {
			cond := conditionFalse("ClickHouseMonitorHealthy", "ProbeFailed", err.Error(), opts.resourceName)
			if writeErr := updateMonitorReport(cfg.ReportPath, opts.resourceName, cond, conditionFalse("ClickHouseRecoveryComplete", "MonitorUnhealthy", "ClickHouse monitor probe failed", opts.resourceName)); writeErr != nil {
				fmt.Fprintf(os.Stderr, "clickhouse monitor report update failed: %v\n", writeErr)
			}
			fmt.Fprintf(os.Stderr, "clickhouse monitor probe failed: %v\n", err)
		} else {
			cond := conditionTrue("ClickHouseMonitorHealthy", "ProbeSucceeded", "ClickHouse accepted an operator query from the monitor", opts.resourceName)
			if writeErr := updateMonitorReport(cfg.ReportPath, opts.resourceName, cond, conditionTrue("ClickHouseRecoveryComplete", "Recovered", "ClickHouse is ready", opts.resourceName)); writeErr != nil {
				fmt.Fprintf(os.Stderr, "clickhouse monitor report update failed: %v\n", writeErr)
			}
		}
		time.Sleep(monitorLoopInterval)
	}
}

func clickhouseMonitorProbe(cfg config) error {
	operatorSVID := filepath.Join(cfg.SPIFFE.OperatorDir, "svid.pem")
	// SPIFFE helper rotations can briefly expose an incomplete PEM.
	valid, err := certificateValidBeyond(operatorSVID, time.Now().Add(5*time.Minute))
	if !valid {
		return fmt.Errorf("operator SVID unavailable: %w", err)
	}
	if _, err := clickhouseQuery(cfg, "SELECT 1"); err != nil {
		return fmt.Errorf("operator query failed: %w", err)
	}
	return nil
}

func waitForMonitorProbe(timeout, interval time.Duration, probe func() error) error {
	deadline := time.Now().Add(timeout)
	var last error
	for {
		if err := probe(); err == nil {
			return nil
		} else {
			last = err
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("timed out waiting for ClickHouse monitor probe: %w", last)
		}
		time.Sleep(interval)
	}
}

func loadConfig(opts options) (config, error) {
	body, err := os.ReadFile(opts.resourceGraph)
	if err != nil {
		return config{}, fmt.Errorf("read resource graph: %w", err)
	}
	var doc document
	if err := json.Unmarshal(body, &doc); err != nil {
		return config{}, fmt.Errorf("decode resource graph: %w", err)
	}
	var matches []resource
	for _, res := range doc.Resources {
		if res.APIVersion == apiVersion && res.Kind == kind && res.Metadata.Name == opts.resourceName {
			matches = append(matches, res)
		}
	}
	if len(matches) != 1 {
		return config{}, fmt.Errorf("expected exactly one %s/%s named %q, found %d", apiVersion, kind, opts.resourceName, len(matches))
	}
	var cfg config
	dec := json.NewDecoder(bytes.NewReader(matches[0].Spec))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return config{}, fmt.Errorf("decode ClickHouseCluster spec: %w", err)
	}
	return cfg, nil
}

func validateConfig(cfg config) error {
	requiredAbs := map[string]string{
		"runtimeRoot":                       cfg.RuntimeRoot,
		"dataDir":                           cfg.DataDir,
		"backupDir":                         cfg.BackupDir,
		"logDir":                            cfg.LogDir,
		"configPath":                        cfg.ConfigPath,
		"tlsDir":                            cfg.TLSDir,
		"pidPath":                           cfg.PIDPath,
		"operatorClientConfigPath":          cfg.OperatorClientConfigPath,
		"operatorCAPath":                    cfg.OperatorCAPath,
		"spiffe.agentSocket":                cfg.SPIFFE.AgentSocket,
		"spiffe.helperPath":                 cfg.SPIFFE.HelperPath,
		"spiffe.serverDir":                  cfg.SPIFFE.ServerDir,
		"spiffe.operatorDir":                cfg.SPIFFE.OperatorDir,
		"spiffe.serverHelperConfig":         cfg.SPIFFE.ServerHelperConfig,
		"spiffe.operatorHelperConfig":       cfg.SPIFFE.OperatorHelperConfig,
		"spiffe.bundleReloadState":          cfg.SPIFFE.BundleReloadState,
		"systemd.serverServicePath":         cfg.Systemd.ServerServicePath,
		"systemd.serverHelperServicePath":   cfg.Systemd.ServerHelperServicePath,
		"systemd.operatorHelperServicePath": cfg.Systemd.OperatorHelperServicePath,
		"systemd.bundleReloadServicePath":   cfg.Systemd.BundleReloadServicePath,
		"systemd.bundleReloadPathUnitPath":  cfg.Systemd.BundleReloadPathUnitPath,
		"migrations.stateDir":               cfg.Migrations.StateDir,
		"reportPath":                        cfg.ReportPath,
	}
	for field, value := range requiredAbs {
		if strings.TrimSpace(value) == "" || !filepath.IsAbs(value) {
			return fmt.Errorf("ClickHouseCluster.spec.%s must be an absolute path", field)
		}
	}
	required := map[string]string{
		"backupDiskName":                 cfg.BackupDiskName,
		"host":                           cfg.Host,
		"serverUser":                     cfg.ServerUser,
		"serverGroup":                    cfg.ServerGroup,
		"operatorUser":                   cfg.OperatorUser,
		"operatorGroup":                  cfg.OperatorGroup,
		"operatorDatabaseUser":           cfg.OperatorDatabaseUser,
		"spiffe.trustDomain":             cfg.SPIFFE.TrustDomain,
		"spiffe.servicePrefix":           cfg.SPIFFE.ServicePrefix,
		"spiffe.serverID":                cfg.SPIFFE.ServerID,
		"spiffe.operatorID":              cfg.SPIFFE.OperatorID,
		"spiffe.spireWorkloadGroup":      cfg.SPIFFE.SPIREWorkloadGroup,
		"migrations.bootstrapSchemaPath": cfg.Migrations.BootstrapSchemaPath,
		"migrations.deltaDir":            cfg.Migrations.DeltaDir,
	}
	for field, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("ClickHouseCluster.spec.%s is required", field)
		}
	}
	if cfg.RuntimeArtifact == "" || filepath.IsAbs(cfg.RuntimeArtifact) || hasDotDot(cfg.RuntimeArtifact) {
		return errors.New("ClickHouseCluster.spec.runtimeArtifact must be a repo-relative path")
	}
	if filepath.IsAbs(cfg.Migrations.BootstrapSchemaPath) || hasDotDot(cfg.Migrations.BootstrapSchemaPath) {
		return errors.New("ClickHouseCluster.spec.migrations.bootstrapSchemaPath must be a repo-relative path")
	}
	if filepath.IsAbs(cfg.Migrations.DeltaDir) || hasDotDot(cfg.Migrations.DeltaDir) {
		return errors.New("ClickHouseCluster.spec.migrations.deltaDir must be a repo-relative path")
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return errors.New("ClickHouseCluster.spec.port must be between 1 and 65535")
	}
	return nil
}

func hasDotDot(path string) bool {
	for _, part := range filepath.SplitList(path) {
		if part == ".." {
			return true
		}
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func installRuntime(artifact string, runtimeRoot string) (string, error) {
	sum, err := sha256File(artifact)
	if err != nil {
		return "", err
	}
	if err := mkdir(runtimeRoot, 0, 0, 0o755); err != nil {
		return "", err
	}
	releases := filepath.Join(runtimeRoot, "releases")
	tmpRoot := filepath.Join(runtimeRoot, "tmp")
	if err := mkdir(releases, 0, 0, 0o755); err != nil {
		return "", err
	}
	if err := mkdir(tmpRoot, 0, 0, 0o755); err != nil {
		return "", err
	}
	lock, err := os.OpenFile(filepath.Join(runtimeRoot, "install.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return "", fmt.Errorf("lock runtime install: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	release := filepath.Join(releases, strings.ReplaceAll(sum, ":", "-"))
	if _, err := os.Stat(filepath.Join(release, "bin/clickhouse")); err != nil {
		tmp := filepath.Join(tmpRoot, filepath.Base(release)+"."+strconv.Itoa(os.Getpid()))
		_ = os.RemoveAll(tmp)
		if err := mkdir(tmp, 0, 0, 0o755); err != nil {
			return "", err
		}
		if err := extractTar(artifact, tmp); err != nil {
			_ = os.RemoveAll(tmp)
			return "", err
		}
		if _, err := os.Stat(filepath.Join(tmp, "bin/clickhouse")); err != nil {
			_ = os.RemoveAll(tmp)
			return "", fmt.Errorf("ClickHouse runtime artifact missing bin/clickhouse: %w", err)
		}
		_ = os.RemoveAll(release)
		if err := os.Rename(tmp, release); err != nil {
			_ = os.RemoveAll(tmp)
			return "", fmt.Errorf("promote ClickHouse runtime: %w", err)
		}
	}
	next := filepath.Join(runtimeRoot, "current.next")
	current := filepath.Join(runtimeRoot, "current")
	_ = os.Remove(next)
	if err := os.Symlink(release, next); err != nil {
		return "", fmt.Errorf("create runtime symlink: %w", err)
	}
	if info, err := os.Lstat(current); err == nil && info.Mode()&os.ModeSymlink == 0 {
		return "", fmt.Errorf("%s exists and is not a symlink", current)
	}
	if err := os.Rename(next, current); err != nil {
		return "", fmt.Errorf("promote runtime symlink: %w", err)
	}
	return sum, nil
}

func convergeHost(cfg config) error {
	serverGroup, err := ensureGroup(cfg.ServerGroup)
	if err != nil {
		return err
	}
	operatorGroup, err := ensureGroup(cfg.OperatorGroup)
	if err != nil {
		return err
	}
	if _, err := lookupGroup(cfg.SPIFFE.SPIREWorkloadGroup); err != nil {
		return fmt.Errorf("lookup SPIRE workload group %q: %w", cfg.SPIFFE.SPIREWorkloadGroup, err)
	}
	serverUser, err := ensureUser(cfg.ServerUser, cfg.ServerGroup, cfg.DataDir)
	if err != nil {
		return err
	}
	operatorUser, err := ensureUser(cfg.OperatorUser, cfg.OperatorGroup, "/var/lib/clickhouse-operator")
	if err != nil {
		return err
	}
	for _, u := range []string{cfg.ServerUser, cfg.OperatorUser} {
		if err := command("/usr/sbin/usermod", "-a", "-G", cfg.SPIFFE.SPIREWorkloadGroup, u); err != nil {
			return fmt.Errorf("add %s to %s: %w", u, cfg.SPIFFE.SPIREWorkloadGroup, err)
		}
	}
	if err := mkdir(cfg.DataDir, serverUser.uid, serverGroup.gid, 0o750); err != nil {
		return err
	}
	if err := mkdir(cfg.BackupDir, serverUser.uid, serverGroup.gid, 0o750); err != nil {
		return err
	}
	if err := mkdir(cfg.LogDir, serverUser.uid, serverGroup.gid, 0o750); err != nil {
		return err
	}
	if err := mkdir(filepath.Dir(cfg.SPIFFE.ServerHelperConfig), 0, serverGroup.gid, 0o750); err != nil {
		return err
	}
	if err := mkdir(filepath.Dir(cfg.ConfigPath), 0, serverGroup.gid, 0o750); err != nil {
		return err
	}
	if err := mkdir(filepath.Dir(cfg.OperatorClientConfigPath), 0, operatorGroup.gid, 0o750); err != nil {
		return err
	}
	if err := mkdir("/var/lib/clickhouse-operator", operatorUser.uid, operatorGroup.gid, 0o750); err != nil {
		return err
	}
	if err := mkdir(cfg.SPIFFE.ServerDir, serverUser.uid, serverGroup.gid, 0o700); err != nil {
		return err
	}
	if err := mkdir(cfg.SPIFFE.OperatorDir, operatorUser.uid, operatorGroup.gid, 0o700); err != nil {
		return err
	}
	if err := ensureTLS(cfg, serverGroup.gid, operatorGroup.gid); err != nil {
		return err
	}
	if err := writeConfigs(cfg, serverGroup.gid, operatorGroup.gid); err != nil {
		return err
	}
	return nil
}

type ids struct {
	uid int
	gid int
}

func ensureGroup(name string) (ids, error) {
	group, err := lookupGroup(name)
	if err == nil {
		return ids{gid: group.gid}, nil
	}
	if err := command("/usr/sbin/groupadd", "--system", name); err != nil {
		return ids{}, fmt.Errorf("create group %s: %w", name, err)
	}
	group, err = lookupGroup(name)
	if err != nil {
		return ids{}, err
	}
	return ids{gid: group.gid}, nil
}

func ensureUser(name, group, home string) (ids, error) {
	u, err := lookupUser(name)
	if err == nil {
		return u, nil
	}
	if err := command(
		"/usr/sbin/useradd",
		"--system",
		"--gid", group,
		"--home-dir", home,
		"--shell", "/usr/sbin/nologin",
		"--no-create-home",
		name,
	); err != nil {
		return ids{}, fmt.Errorf("create user %s: %w", name, err)
	}
	return lookupUser(name)
}

func lookupUser(name string) (ids, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return ids{}, err
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return ids{}, err
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return ids{}, err
	}
	return ids{uid: uid, gid: gid}, nil
}

func lookupGroup(name string) (ids, error) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return ids{}, err
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return ids{}, err
	}
	return ids{gid: gid}, nil
}

func ensureTLS(cfg config, serverGID, operatorGID int) error {
	if err := mkdir(cfg.TLSDir, 0, serverGID, 0o750); err != nil {
		return err
	}
	caKeyPath := filepath.Join(cfg.TLSDir, "server-ca-key.pem")
	caCertPath := filepath.Join(cfg.TLSDir, "server-ca.pem")
	serverKeyPath := filepath.Join(cfg.TLSDir, "server-key.pem")
	serverCertPath := filepath.Join(cfg.TLSDir, "server-cert.pem")
	if allFilesExist(caKeyPath, caCertPath, serverKeyPath, serverCertPath) {
		return projectCA(cfg, caCertPath, operatorGID)
	}
	caKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return fmt.Errorf("generate ClickHouse CA key: %w", err)
	}
	now := time.Now().UTC()
	caTemplate := &x509.Certificate{
		SerialNumber:          randomSerial(),
		Subject:               pkix.Name{CommonName: "Verself ClickHouse local CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create ClickHouse CA certificate: %w", err)
	}
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate ClickHouse server key: %w", err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber:          randomSerial(),
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create ClickHouse server certificate: %w", err)
	}
	if err := writeAtomic(caKeyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(caKey)}), 0o600, 0, 0); err != nil {
		return err
	}
	if err := writeAtomic(caCertPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o640, 0, serverGID); err != nil {
		return err
	}
	if err := writeAtomic(serverKeyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverKey)}), 0o640, 0, serverGID); err != nil {
		return err
	}
	if err := writeAtomic(serverCertPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}), 0o640, 0, serverGID); err != nil {
		return err
	}
	return projectCA(cfg, caCertPath, operatorGID)
}

func randomSerial() *big.Int {
	serial := make([]byte, 20)
	if _, err := rand.Read(serial); err != nil {
		panic(err)
	}
	serial[0] &= 0x7f
	return new(big.Int).SetBytes(serial)
}

func allFilesExist(paths ...string) bool {
	for _, path := range paths {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			return false
		}
	}
	return true
}

func projectCA(cfg config, caCertPath string, operatorGID int) error {
	ca, err := os.ReadFile(caCertPath)
	if err != nil {
		return err
	}
	if err := writeAtomic(cfg.OperatorCAPath, ca, 0o640, 0, operatorGID); err != nil {
		return err
	}
	for _, projection := range cfg.ClientCAProjections {
		mode, err := parseMode(projection.Mode)
		if err != nil {
			return err
		}
		dirMode, err := parseMode(projection.DirectoryMode)
		if err != nil {
			return err
		}
		group, err := lookupGroup(projection.Group)
		if err != nil {
			return err
		}
		if err := mkdir(filepath.Dir(projection.Path), 0, group.gid, dirMode); err != nil {
			return err
		}
		if err := writeAtomic(projection.Path, ca, mode, 0, group.gid); err != nil {
			return err
		}
	}
	return nil
}

func writeConfigs(cfg config, serverGID, operatorGID int) error {
	files := []struct {
		path string
		body string
		mode os.FileMode
		uid  int
		gid  int
	}{
		{cfg.ConfigPath, serverXML(cfg), 0o640, 0, serverGID},
		{cfg.OperatorClientConfigPath, operatorXML(cfg), 0o640, 0, operatorGID},
		{cfg.SPIFFE.ServerHelperConfig, serverHelperConfig(cfg), 0o640, 0, serverGID},
		{cfg.SPIFFE.OperatorHelperConfig, operatorHelperConfig(cfg), 0o640, 0, operatorGID},
		{cfg.Systemd.ServerServicePath, serverService(cfg), 0o644, 0, 0},
		{cfg.Systemd.ServerHelperServicePath, serverHelperService(cfg), 0o644, 0, 0},
		{cfg.Systemd.OperatorHelperServicePath, operatorHelperService(cfg), 0o644, 0, 0},
		{cfg.Systemd.BundleReloadServicePath, bundleReloadService(cfg), 0o644, 0, 0},
		{cfg.Systemd.BundleReloadPathUnitPath, bundleReloadPathUnit(cfg), 0o644, 0, 0},
	}
	for _, f := range files {
		if err := writeAtomic(f.path, []byte(f.body), f.mode, f.uid, f.gid); err != nil {
			return err
		}
	}
	return nil
}

func startServices(cfg config) error {
	if err := command("/bin/systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := enableAndRestart("clickhouse-server-spiffe-helper.service"); err != nil {
		return err
	}
	if err := waitForValidCertificate(filepath.Join(cfg.SPIFFE.ServerDir, "svid.pem"), 30*time.Second); err != nil {
		return err
	}
	if err := waitForFile(filepath.Join(cfg.SPIFFE.ServerDir, "bundle.pem"), 30*time.Second); err != nil {
		return err
	}
	if err := enableAndRestart("clickhouse-server.service"); err != nil {
		return err
	}
	if err := enableAndRestart("clickhouse-operator-spiffe-helper.service"); err != nil {
		return err
	}
	if err := waitForValidCertificate(filepath.Join(cfg.SPIFFE.OperatorDir, "svid.pem"), 30*time.Second); err != nil {
		return err
	}
	if err := waitForQuery(cfg, 60*time.Second); err != nil {
		return err
	}
	if err := command(filepath.Join(cfg.RuntimeRoot, "current/bin/clickhouse-spiffe-bundle-reload"), "--bundle", filepath.Join(cfg.SPIFFE.ServerDir, "bundle.pem"), "--state-path", cfg.SPIFFE.BundleReloadState, "--unit", "clickhouse-server.service"); err != nil {
		return err
	}
	if err := waitForQuery(cfg, 60*time.Second); err != nil {
		return err
	}
	if err := command("/bin/systemctl", "enable", "--now", "clickhouse-server-spiffe-bundle-reload.path"); err != nil {
		return err
	}
	return nil
}

func enableAndRestart(unit string) error {
	if err := command("/bin/systemctl", "enable", unit); err != nil {
		return err
	}
	return command("/bin/systemctl", "restart", unit)
}

func waitForFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("timed out waiting for %s", path)
}

func waitForValidCertificate(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		valid, err := certificateValidBeyond(path, time.Now().Add(5*time.Minute))
		if valid {
			return nil
		}
		last = err
		time.Sleep(time.Second)
	}
	return fmt.Errorf("timed out waiting for valid certificate %s: %w", path, last)
}

func certificateValidBeyond(path string, minimumNotAfter time.Time) (bool, error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err != nil {
		return false, fmt.Errorf("read certificate %s: %w", path, err)
	}
	block, _ := pem.Decode(body)
	if block == nil || block.Type != "CERTIFICATE" {
		return false, fmt.Errorf("%s does not contain a PEM certificate", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false, fmt.Errorf("parse certificate %s: %w", path, err)
	}
	if !cert.NotAfter.After(minimumNotAfter) {
		return false, fmt.Errorf("certificate %s expires at %s", path, cert.NotAfter.Format(time.RFC3339))
	}
	return true, nil
}

func waitForQuery(cfg config, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		if _, err := clickhouseQuery(cfg, "SELECT 1"); err == nil {
			return nil
		} else {
			last = err
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("timed out waiting for ClickHouse query: %w", last)
}

func applyMigrations(repoRoot string, cfg config) (string, []string, error) {
	if err := mkdir(cfg.Migrations.StateDir, 0, 0, 0o755); err != nil {
		return "", nil, err
	}
	renderedDir := filepath.Join(cfg.Migrations.StateDir, "rendered")
	if err := mkdir(renderedDir, 0, 0, 0o755); err != nil {
		return "", nil, err
	}
	bootstrapPath, err := resolveRepoPath(repoRoot, cfg.Migrations.BootstrapSchemaPath)
	if err != nil {
		return "", nil, err
	}
	bootstrapBytes, err := os.ReadFile(bootstrapPath)
	if err != nil {
		return "", nil, err
	}
	fingerprint := migrationFingerprint(bootstrapBytes, cfg.SPIFFE.ServicePrefix)
	remoteFingerprintPath := filepath.Join(cfg.Migrations.StateDir, ".bootstrap-applied-hash")
	remoteFingerprint, _ := os.ReadFile(remoteFingerprintPath)
	needsBootstrap := strings.TrimSpace(string(remoteFingerprint)) != fingerprint
	if !needsBootstrap {
		healthy, err := bootstrapTablesPresent(cfg)
		if err != nil || !healthy {
			needsBootstrap = true
		}
	}
	var applied []string
	if needsBootstrap {
		if _, err := clickhouseQuery(cfg, "CREATE DATABASE IF NOT EXISTS verself"); err != nil {
			return "", nil, err
		}
		rendered := filepath.Join(renderedDir, "001_initial_schema.up.sql")
		if err := writeAtomic(rendered, renderMigration(bootstrapBytes, cfg.SPIFFE.ServicePrefix), 0o644, 0, 0); err != nil {
			return "", nil, err
		}
		if err := clickhouseQueriesFile(cfg, rendered); err != nil {
			return "", nil, err
		}
		if err := writeAtomic(remoteFingerprintPath, []byte(fingerprint+"\n"), 0o644, 0, 0); err != nil {
			return "", nil, err
		}
		applied = append(applied, "001_initial_schema.up.sql")
	}
	deltaDir, err := resolveRepoPath(repoRoot, cfg.Migrations.DeltaDir)
	if err != nil {
		return "", nil, err
	}
	deltas, err := filepath.Glob(filepath.Join(deltaDir, "[0-9][0-9][0-9]_*.up.sql"))
	if err != nil {
		return "", nil, err
	}
	sort.Strings(deltas)
	for _, migration := range deltas {
		if filepath.Base(migration) == "001_initial_schema.up.sql" {
			continue
		}
		body, err := os.ReadFile(migration)
		if err != nil {
			return "", nil, err
		}
		rendered := filepath.Join(renderedDir, filepath.Base(migration))
		if err := writeAtomic(rendered, renderMigration(body, cfg.SPIFFE.ServicePrefix), 0o644, 0, 0); err != nil {
			return "", nil, err
		}
		if err := clickhouseQueriesFile(cfg, rendered); err != nil {
			return "", nil, fmt.Errorf("apply %s: %w", filepath.Base(migration), err)
		}
		applied = append(applied, filepath.Base(migration))
	}
	_, _ = clickhouseQuery(cfg, "SYSTEM FLUSH LOGS")
	return fingerprint, applied, nil
}

func bootstrapTablesPresent(cfg config) (bool, error) {
	out, err := clickhouseQuery(cfg, `
SELECT count()
FROM system.tables
WHERE (database = 'default' AND name IN ('otel_traces', 'otel_logs', 'otel_metrics_sum'))
   OR (database = 'verself' AND name = 'job_events')`)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "4", nil
}

func migrationFingerprint(body []byte, servicePrefix string) string {
	h := sha256.New()
	h.Write(body)
	h.Write([]byte("\nspiffe_service_prefix="))
	h.Write([]byte(servicePrefix))
	h.Write([]byte("\n"))
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func renderMigration(body []byte, servicePrefix string) []byte {
	return bytes.ReplaceAll(body, []byte("__CLICKHOUSE_SPIFFE_SERVICE_PREFIX__"), []byte(servicePrefix))
}

func resolveRepoPath(repoRoot, rel string) (string, error) {
	candidates := []string{
		filepath.Join(repoRoot, filepath.FromSlash(rel)),
		filepath.Join(repoRoot, "workspace", filepath.FromSlash(rel)),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("repo path %q not found under %s or %s", rel, repoRoot, filepath.Join(repoRoot, "workspace"))
}

func clickhouseQueriesFile(cfg config, path string) error {
	_, err := clickhouse(cfg, "client", "--config-file", cfg.OperatorClientConfigPath, "--user", cfg.OperatorDatabaseUser, "--database", "verself", "--multiquery", "--queries-file", path)
	return err
}

func clickhouseQuery(cfg config, query string) ([]byte, error) {
	return clickhouse(cfg, "client", "--config-file", cfg.OperatorClientConfigPath, "--user", cfg.OperatorDatabaseUser, "--query", query)
}

func clickhouse(cfg config, args ...string) ([]byte, error) {
	return output(filepath.Join(cfg.RuntimeRoot, "current/bin/clickhouse"), args...)
}

func serverXML(cfg config) string {
	return fmt.Sprintf(`<clickhouse>
    <listen_host>%s</listen_host>
    <tcp_port_secure>%d</tcp_port_secure>

    <path>%s/</path>
    <tmp_path>%s/tmp/</tmp_path>
    <user_files_path>%s/user_files/</user_files_path>
    <format_schema_path>%s/format_schemas/</format_schema_path>
    <access_control_path>%s/access/</access_control_path>

    <storage_configuration>
        <disks>
            <%s>
                <type>local</type>
                <path>%s/</path>
            </%s>
        </disks>
    </storage_configuration>

    <backups>
        <allowed_disk>%s</allowed_disk>
        <remove_backup_files_after_failure>true</remove_backup_files_after_failure>
    </backups>

    <logger>
        <level>information</level>
        <log>%s/clickhouse-server.log</log>
        <errorlog>%s/clickhouse-server.err.log</errorlog>
        <size>100M</size>
        <count>3</count>
    </logger>

    <openSSL>
        <server>
            <certificateFile>%s/server-cert.pem</certificateFile>
            <privateKeyFile>%s/server-key.pem</privateKeyFile>
            <verificationMode>strict</verificationMode>
            <caConfig>%s/bundle.pem</caConfig>
            <cacheSessions>true</cacheSessions>
            <disableProtocols>sslv2,sslv3</disableProtocols>
            <preferServerCiphers>true</preferServerCiphers>
        </server>
    </openSSL>

    <profiles>
        <default/>
    </profiles>

    <users>
        <default>
            <no_password/>
            <access_management>0</access_management>
            <networks>
                <ip>127.0.0.1</ip>
                <ip>::1</ip>
            </networks>
            <profile>default</profile>
            <quota>default</quota>
        </default>
        <%s>
            <ssl_certificates>
                <subject_alt_name>URI:%s</subject_alt_name>
            </ssl_certificates>
            <networks>
                <ip>127.0.0.1</ip>
                <ip>::1</ip>
            </networks>
            <profile>default</profile>
            <quota>default</quota>
            <access_management>1</access_management>
            <named_collection_control>1</named_collection_control>
        </%s>
    </users>

    <query_log>
        <database>system</database>
        <table>query_log</table>
        <partition_by>toYYYYMM(event_time)</partition_by>
        <flush_interval_milliseconds>7500</flush_interval_milliseconds>
    </query_log>

    <quotas>
        <default>
            <interval>
                <duration>3600</duration>
                <queries>0</queries>
                <errors>0</errors>
                <result_rows>0</result_rows>
                <read_rows>0</read_rows>
                <execution_time>0</execution_time>
            </interval>
        </default>
    </quotas>
</clickhouse>
`, x(cfg.Host), cfg.Port, x(cfg.DataDir), x(cfg.DataDir), x(cfg.DataDir), x(cfg.DataDir), x(cfg.DataDir), x(cfg.BackupDiskName), x(cfg.BackupDir), x(cfg.BackupDiskName), x(cfg.BackupDiskName), x(cfg.LogDir), x(cfg.LogDir), x(cfg.TLSDir), x(cfg.TLSDir), x(cfg.SPIFFE.ServerDir), x(cfg.OperatorDatabaseUser), x(cfg.SPIFFE.OperatorID), x(cfg.OperatorDatabaseUser))
}

func operatorXML(cfg config) string {
	return fmt.Sprintf(`<config>
    <secure>1</secure>
    <host>%s</host>
    <port>%d</port>
    <openSSL>
        <client>
            <certificateFile>%s/svid.pem</certificateFile>
            <privateKeyFile>%s/svid_key.pem</privateKeyFile>
            <caConfig>%s</caConfig>
            <cacheSessions>true</cacheSessions>
            <disableProtocols>sslv2,sslv3</disableProtocols>
            <preferServerCiphers>true</preferServerCiphers>
            <invalidCertificateHandler>
                <name>RejectCertificateHandler</name>
            </invalidCertificateHandler>
        </client>
    </openSSL>
</config>
`, x(cfg.Host), cfg.Port, x(cfg.SPIFFE.OperatorDir), x(cfg.SPIFFE.OperatorDir), x(cfg.OperatorCAPath))
}

func serverHelperConfig(cfg config) string {
	return fmt.Sprintf(`agent_address = "%s"
cert_dir = "%s"
daemon_mode = true
pid_file_name = "%s"
renew_signal = "SIGHUP"
svid_file_name = "svid.pem"
svid_key_file_name = "svid_key.pem"
svid_bundle_file_name = "bundle.pem"
cert_file_mode = 0640
key_file_mode = 0600
`, cfg.SPIFFE.AgentSocket, cfg.SPIFFE.ServerDir, cfg.PIDPath)
}

func operatorHelperConfig(cfg config) string {
	return fmt.Sprintf(`agent_address = "%s"
cert_dir = "%s"
daemon_mode = true
svid_file_name = "svid.pem"
svid_key_file_name = "svid_key.pem"
svid_bundle_file_name = "bundle.pem"
cert_file_mode = 0640
key_file_mode = 0600
`, cfg.SPIFFE.AgentSocket, cfg.SPIFFE.OperatorDir)
}

func serverService(cfg config) string {
	return fmt.Sprintf(`[Unit]
Description=ClickHouse Server
After=network.target clickhouse-server-spiffe-helper.service
Requires=clickhouse-server-spiffe-helper.service

[Service]
Type=simple
User=%s
Group=%s
ExecStart=%s/current/bin/clickhouse server --config-file %s --pid-file %s
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5
LimitNOFILE=500000
RuntimeDirectory=clickhouse-server
RuntimeDirectoryMode=0750

[Install]
WantedBy=multi-user.target
`, cfg.ServerUser, cfg.ServerGroup, cfg.RuntimeRoot, cfg.ConfigPath, cfg.PIDPath)
}

func serverHelperService(cfg config) string {
	return fmt.Sprintf(`[Unit]
Description=ClickHouse server SPIFFE helper
After=network.target spire-agent.service
Wants=spire-agent.service

[Service]
Type=simple
User=%s
Group=%s
SupplementaryGroups=%s
ExecStart=%s -config %s
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=%s

[Install]
WantedBy=multi-user.target
`, cfg.ServerUser, cfg.ServerGroup, cfg.SPIFFE.SPIREWorkloadGroup, cfg.SPIFFE.HelperPath, cfg.SPIFFE.ServerHelperConfig, cfg.SPIFFE.ServerDir)
}

func operatorHelperService(cfg config) string {
	return fmt.Sprintf(`[Unit]
Description=ClickHouse operator SPIFFE helper
After=network.target spire-agent.service
Wants=spire-agent.service

[Service]
Type=simple
User=%s
Group=%s
SupplementaryGroups=%s
ExecStart=%s -config %s
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=%s

[Install]
WantedBy=multi-user.target
`, cfg.OperatorUser, cfg.OperatorGroup, cfg.SPIFFE.SPIREWorkloadGroup, cfg.SPIFFE.HelperPath, cfg.SPIFFE.OperatorHelperConfig, cfg.SPIFFE.OperatorDir)
}

func bundleReloadService(cfg config) string {
	return fmt.Sprintf(`[Unit]
Description=Restart ClickHouse after SPIFFE trust bundle changes
After=clickhouse-server.service

[Service]
Type=oneshot
ExecStart=%s/current/bin/clickhouse-spiffe-bundle-reload --bundle %s/bundle.pem --state-path %s --unit clickhouse-server.service
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=%s
`, cfg.RuntimeRoot, cfg.SPIFFE.ServerDir, cfg.SPIFFE.BundleReloadState, cfg.SPIFFE.ServerDir)
}

func bundleReloadPathUnit(cfg config) string {
	return fmt.Sprintf(`[Unit]
Description=Watch ClickHouse SPIFFE bundle for trust-store reload
After=clickhouse-server-spiffe-helper.service
Requires=clickhouse-server-spiffe-helper.service

[Path]
PathChanged=%s/bundle.pem
Unit=clickhouse-server-spiffe-bundle-reload.service

[Install]
WantedBy=multi-user.target
`, cfg.SPIFFE.ServerDir)
}

func x(value string) string {
	var b bytes.Buffer
	for _, r := range value {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func extractTar(path string, dest string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	tr := tar.NewReader(f)
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}
		name := filepath.Clean(header.Name)
		if name == "." || strings.HasPrefix(name, "../") || filepath.IsAbs(name) {
			return fmt.Errorf("unsafe tar member %q", header.Name)
		}
		target := filepath.Join(destAbs, name)
		if !strings.HasPrefix(target, destAbs+string(os.PathSeparator)) && target != destAbs {
			return fmt.Errorf("tar member escapes destination: %q", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)&0o777); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(header.Mode)&0o777)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, tr)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return fmt.Errorf("unsupported tar member type %d for %q", header.Typeflag, header.Name)
		}
	}
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func mkdir(path string, uid, gid int, mode os.FileMode) error {
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

func writeAtomic(path string, body []byte, mode os.FileMode, uid, gid int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + "." + strconv.Itoa(os.Getpid()) + ".tmp"
	if err := os.WriteFile(tmp, body, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chown(tmp, uid, gid); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("promote %s: %w", path, err)
	}
	return nil
}

func writeReport(stdout io.Writer, path string, rep report) error {
	body, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if _, err := stdout.Write(body); err != nil {
		return err
	}
	return writeAtomic(path, body, 0o644, 0, 0)
}

func updateMonitorReport(path, resourceName string, conditions ...condition) error {
	rep := report{
		Component:    "clickhouse",
		ResourceName: resourceName,
	}
	if body, err := os.ReadFile(path); err == nil {
		var existing report
		if err := json.Unmarshal(body, &existing); err == nil {
			rep = existing
			rep.Component = "clickhouse"
			rep.ResourceName = resourceName
		}
	}
	rep.CheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
	rep.Conditions = upsertConditions(rep.Conditions, conditions...)
	return writeReport(io.Discard, path, rep)
}

func upsertConditions(existing []condition, updates ...condition) []condition {
	positions := make(map[string]int, len(existing))
	for i, cond := range existing {
		positions[cond.Type] = i
	}
	for _, update := range updates {
		if i, ok := positions[update.Type]; ok {
			existing[i] = update
			continue
		}
		positions[update.Type] = len(existing)
		existing = append(existing, update)
	}
	return existing
}

func parseMode(mode string) (os.FileMode, error) {
	value, err := strconv.ParseUint(mode, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("parse file mode %q: %w", mode, err)
	}
	return os.FileMode(value), nil
}

func command(name string, args ...string) error {
	_, err := output(name, args...)
	return err
}

func output(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func conditionTrue(conditionType, reason, message, resource string) condition {
	return condition{Type: conditionType, Status: "True", Reason: reason, Message: message, Resource: resource}
}

func conditionFalse(conditionType, reason, message, resource string) condition {
	return condition{Type: conditionType, Status: "False", Reason: reason, Message: message, Resource: resource}
}
