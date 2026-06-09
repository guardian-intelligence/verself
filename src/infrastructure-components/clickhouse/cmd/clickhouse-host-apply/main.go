package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	managedHeader        = "<!-- Managed by Verself clickhouse-host-apply. -->"
	clickhouseUser       = "clickhouse"
	clickhouseGroup      = "clickhouse"
	operatorUser         = "clickhouse_operator"
	operatorGroup        = "clickhouse_operator"
	clickhouseDBUser     = "clickhouse_operator"
	migrationInstallPath = "/opt/verself/migrations/001_initial_schema.up.sql"
	migrationStatePath   = "/opt/verself/migrations/.bootstrap-applied-hash"
)

type config struct {
	artifactRoot          string
	destRoot              string
	spiffeServicePrefix   string
	spireAgentSocket      string
	spireWorkloadGroup    string
	clickhouseHost        string
	clickhousePort        string
	dataPath              string
	serverSpiffeDir       string
	operatorSpiffeDir     string
	systemctlBin          string
	manageSystemd         bool
	clickhouseBin         string
	bundleReloadBin       string
	serverHelperConfig    string
	operatorHelperConfig  string
	operatorClientConfig  string
	operatorCAPath        string
	serverConfig          string
	serverPIDPath         string
	bundleReloadStatePath string
}

type ownerMode struct {
	user  string
	group string
	mode  fs.FileMode
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "clickhouse-host-apply: %v\n", err)
		os.Exit(1)
	}
}

func defaultConfig() config {
	return config{
		artifactRoot:          os.Getenv("VERSELF_CLICKHOUSE_HOST_INSTALL"),
		destRoot:              "/",
		spiffeServicePrefix:   os.Getenv("VERSELF_SPIFFE_SERVICE_PREFIX"),
		spireAgentSocket:      "/run/spire-agent/sockets/agent.sock",
		spireWorkloadGroup:    "spire_workload",
		clickhouseHost:        "127.0.0.1",
		clickhousePort:        "9440",
		dataPath:              "/var/lib/clickhouse",
		serverSpiffeDir:       "/var/lib/clickhouse/spiffe",
		operatorSpiffeDir:     "/var/lib/clickhouse-operator/spiffe",
		systemctlBin:          "systemctl",
		manageSystemd:         true,
		clickhouseBin:         "/opt/verself/profile/bin/clickhouse",
		bundleReloadBin:       "/opt/verself/profile/bin/clickhouse-spiffe-bundle-reload",
		serverHelperConfig:    "/etc/clickhouse-server/server-spiffe-helper.conf",
		operatorHelperConfig:  "/etc/clickhouse-client/operator-spiffe-helper.conf",
		operatorClientConfig:  "/etc/clickhouse-client/operator.xml",
		operatorCAPath:        "/etc/clickhouse-client/server-ca.pem",
		serverConfig:          "/etc/clickhouse-server/config.d/verself.xml",
		serverPIDPath:         "/run/clickhouse-server/clickhouse-server.pid",
		bundleReloadStatePath: "/var/lib/clickhouse/spiffe/.bundle-reload.sha256",
	}
}

func run(ctx context.Context, args []string) error {
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	if err := cfg.validate(); err != nil {
		return err
	}
	if err := copyProfileRuntime(cfg); err != nil {
		return err
	}
	if cfg.destRoot == "/" {
		if err := ensureHostUsers(ctx); err != nil {
			return err
		}
	}
	if err := ensureDirectories(cfg); err != nil {
		return err
	}
	if cfg.destRoot == "/" {
		if err := chownRuntimeTrees(ctx); err != nil {
			return err
		}
	}
	if err := ensureTLSMaterial(cfg); err != nil {
		return err
	}
	if err := writeStaticConfig(cfg); err != nil {
		return err
	}
	if cfg.destRoot == "/" && cfg.manageSystemd {
		if err := convergeSystemd(ctx, cfg); err != nil {
			return err
		}
		if err := convergeBootstrapSchema(ctx, cfg); err != nil {
			return err
		}
	}
	fmt.Println("clickhouse-host-apply: converged")
	return nil
}

func parseConfig(args []string) (config, error) {
	cfg := defaultConfig()
	fs := flag.NewFlagSet("clickhouse-host-apply", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&cfg.artifactRoot, "artifact-root", cfg.artifactRoot, "Extracted clickhouse-host-install artifact root.")
	fs.StringVar(&cfg.destRoot, "dest-root", cfg.destRoot, "Destination root for tests.")
	fs.StringVar(&cfg.spiffeServicePrefix, "spiffe-service-prefix", cfg.spiffeServicePrefix, "Site SPIFFE service prefix, e.g. spiffe://gamma.verself.sh/svc.")
	fs.StringVar(&cfg.spireAgentSocket, "spire-agent-socket", cfg.spireAgentSocket, "SPIRE agent socket path.")
	fs.StringVar(&cfg.spireWorkloadGroup, "spire-workload-group", cfg.spireWorkloadGroup, "Group allowed to use the SPIRE agent socket.")
	fs.BoolVar(&cfg.manageSystemd, "manage-systemd", cfg.manageSystemd, "Install, enable, and restart component-owned systemd units.")
	fs.StringVar(&cfg.systemctlBin, "systemctl-bin", cfg.systemctlBin, "systemctl executable.")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if fs.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	return cfg, nil
}

func (cfg *config) validate() error {
	if cfg.artifactRoot == "" {
		return errors.New("--artifact-root is required")
	}
	artifactRoot, err := filepath.Abs(cfg.artifactRoot)
	if err != nil {
		return fmt.Errorf("resolve --artifact-root: %w", err)
	}
	cfg.artifactRoot = artifactRoot
	if cfg.destRoot == "" {
		return errors.New("--dest-root is required")
	}
	destRoot, err := filepath.Abs(cfg.destRoot)
	if err != nil {
		return fmt.Errorf("resolve --dest-root: %w", err)
	}
	cfg.destRoot = destRoot
	cfg.spiffeServicePrefix = strings.TrimRight(strings.TrimSpace(cfg.spiffeServicePrefix), "/")
	if !strings.HasPrefix(cfg.spiffeServicePrefix, "spiffe://") || !strings.HasSuffix(cfg.spiffeServicePrefix, "/svc") {
		return fmt.Errorf("--spiffe-service-prefix must look like spiffe://<trust-domain>/svc, got %q", cfg.spiffeServicePrefix)
	}
	if cfg.spireAgentSocket == "" || !filepath.IsAbs(cfg.spireAgentSocket) {
		return errors.New("--spire-agent-socket must be absolute")
	}
	if cfg.spireWorkloadGroup == "" || strings.ContainsAny(cfg.spireWorkloadGroup, " \t\r\n:/") {
		return fmt.Errorf("--spire-workload-group has unsupported value %q", cfg.spireWorkloadGroup)
	}
	return nil
}

func copyProfileRuntime(cfg config) error {
	src := filepath.Join(cfg.artifactRoot, "opt", "verself", "profile", "bin")
	for _, name := range []string{"clickhouse", "clickhouse-spiffe-bundle-reload"} {
		if err := requireExecutable(filepath.Join(src, name)); err != nil {
			return err
		}
	}
	return copyTree(filepath.Join(cfg.artifactRoot, "opt"), destPath(cfg.destRoot, "/opt"))
}

func requireExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat executable %s: %w", path, err)
	}
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("%s is not executable", path)
	}
	return nil
}

func copyTree(srcRoot, destRoot string) error {
	return filepath.WalkDir(srcRoot, func(src string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcRoot, src)
		if err != nil {
			return err
		}
		dest := filepath.Join(destRoot, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dest, info.Mode().Perm())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(src)
			if err != nil {
				return fmt.Errorf("readlink %s: %w", src, err)
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return fmt.Errorf("create %s: %w", filepath.Dir(dest), err)
			}
			_ = os.Remove(dest)
			if err := os.Symlink(target, dest); err != nil {
				return fmt.Errorf("symlink %s -> %s: %w", dest, target, err)
			}
			return nil
		}
		if entry.IsDir() {
			if err := os.MkdirAll(dest, info.Mode().Perm()); err != nil {
				return fmt.Errorf("create %s: %w", dest, err)
			}
			return os.Chmod(dest, info.Mode().Perm())
		}
		return copyFile(src, dest, info.Mode().Perm())
	})
}

func copyFile(src, dest string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(dest), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", dest, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("copy %s to %s: %w", src, dest, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, dest, err)
	}
	return nil
}

func ensureHostUsers(ctx context.Context) error {
	if err := ensureSystemGroup(ctx, clickhouseGroup); err != nil {
		return err
	}
	if err := ensureSystemUser(ctx, clickhouseUser, clickhouseGroup, "/var/lib/clickhouse"); err != nil {
		return err
	}
	if err := ensureSystemGroup(ctx, operatorGroup); err != nil {
		return err
	}
	return ensureSystemUser(ctx, operatorUser, operatorGroup, "/var/lib/clickhouse-operator")
}

func ensureSystemGroup(ctx context.Context, name string) error {
	if err := command(ctx, 5*time.Second, "", "/usr/bin/getent", "group", name); err == nil {
		return nil
	}
	return command(ctx, 10*time.Second, "", "/usr/sbin/groupadd", "--system", name)
}

func ensureSystemUser(ctx context.Context, name, group, home string) error {
	if err := command(ctx, 5*time.Second, "", "/usr/bin/id", "-u", name); err == nil {
		return nil
	}
	return command(ctx, 10*time.Second, "", "/usr/sbin/useradd",
		"--system",
		"--gid", group,
		"--home-dir", home,
		"--shell", "/usr/sbin/nologin",
		"--no-create-home",
		name,
	)
}

func ensureDirectories(cfg config) error {
	dirs := map[string]ownerMode{
		"/etc/clickhouse-server/config.d":     {user: "root", group: "root", mode: 0o755},
		"/etc/clickhouse-server/tls":          {user: "root", group: "root", mode: 0o755},
		"/var/lib/clickhouse":                 {user: clickhouseUser, group: clickhouseGroup, mode: 0o750},
		"/var/log/clickhouse-server":          {user: clickhouseUser, group: clickhouseGroup, mode: 0o750},
		"/var/lib/clickhouse/spiffe":          {user: clickhouseUser, group: clickhouseGroup, mode: 0o700},
		"/etc/clickhouse-client":              {user: "root", group: operatorGroup, mode: 0o750},
		"/var/lib/clickhouse-operator":        {user: operatorUser, group: operatorGroup, mode: 0o750},
		"/var/lib/clickhouse-operator/spiffe": {user: operatorUser, group: operatorGroup, mode: 0o700},
		"/etc/verself/clickhouse":             {user: "root", group: "root", mode: 0o755},
		"/opt/verself/migrations":             {user: "root", group: "root", mode: 0o755},
	}
	for path, mode := range dirs {
		if err := ensureDir(destPath(cfg.destRoot, path), cfg.destRoot, mode); err != nil {
			return err
		}
	}
	return nil
}

func ensureDir(path, destRoot string, mode ownerMode) error {
	if err := os.MkdirAll(path, mode.mode); err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if err := os.Chmod(path, mode.mode); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return chownPath(path, destRoot, mode.user, mode.group)
}

func chownRuntimeTrees(ctx context.Context) error {
	for _, spec := range [][]string{
		{clickhouseUser + ":" + clickhouseGroup, "/var/lib/clickhouse", "/var/log/clickhouse-server"},
		{operatorUser + ":" + operatorGroup, "/var/lib/clickhouse-operator"},
	} {
		args := append([]string{"-R", spec[0]}, spec[1:]...)
		if err := command(ctx, 60*time.Second, "", "/usr/bin/chown", args...); err != nil {
			return err
		}
	}
	return nil
}

func ensureTLSMaterial(cfg config) error {
	caKeyPath := destPath(cfg.destRoot, "/etc/clickhouse-server/tls/server-ca-key.pem")
	caCertPath := destPath(cfg.destRoot, "/etc/clickhouse-server/tls/server-ca.pem")
	serverKeyPath := destPath(cfg.destRoot, "/etc/clickhouse-server/tls/server-key.pem")
	serverCertPath := destPath(cfg.destRoot, "/etc/clickhouse-server/tls/server-cert.pem")

	caKey, caCert, caChanged, err := ensureCA(cfg, caKeyPath, caCertPath)
	if err != nil {
		return err
	}
	if caChanged || !fileExists(serverKeyPath) || !fileExists(serverCertPath) {
		if err := writeServerCertificate(cfg, caKey, caCert, serverKeyPath, serverCertPath); err != nil {
			return err
		}
	}

	modes := map[string]ownerMode{
		"/etc/clickhouse-server/tls/server-ca.pem":     {user: "root", group: clickhouseGroup, mode: 0o640},
		"/etc/clickhouse-server/tls/server-ca-key.pem": {user: "root", group: "root", mode: 0o600},
		"/etc/clickhouse-server/tls/server-cert.pem":   {user: "root", group: clickhouseGroup, mode: 0o640},
		"/etc/clickhouse-server/tls/server-key.pem":    {user: "root", group: clickhouseGroup, mode: 0o640},
	}
	for path, mode := range modes {
		if err := chmodChown(destPath(cfg.destRoot, path), cfg.destRoot, mode); err != nil {
			return err
		}
	}
	ca, err := os.ReadFile(caCertPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", caCertPath, err)
	}
	if err := writeFile(destPath(cfg.destRoot, cfg.operatorCAPath), cfg.destRoot, ca, ownerMode{user: "root", group: operatorGroup, mode: 0o640}); err != nil {
		return err
	}
	return writeFile(destPath(cfg.destRoot, "/etc/verself/clickhouse/server-ca.pem"), cfg.destRoot, ca, ownerMode{user: "root", group: "root", mode: 0o644})
}

func ensureCA(cfg config, keyPath, certPath string) (*rsa.PrivateKey, *x509.Certificate, bool, error) {
	if fileExists(keyPath) && fileExists(certPath) {
		key, cert, err := readCA(keyPath, certPath)
		return key, cert, false, err
	}
	key, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, nil, false, fmt.Errorf("generate ClickHouse CA key: %w", err)
	}
	now := time.Now().UTC()
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, false, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Verself ClickHouse local CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, false, fmt.Errorf("create ClickHouse CA cert: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, false, fmt.Errorf("parse generated ClickHouse CA cert: %w", err)
	}
	if err := writePEM(keyPath, cfg.destRoot, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key), ownerMode{user: "root", group: "root", mode: 0o600}); err != nil {
		return nil, nil, false, err
	}
	if err := writePEM(certPath, cfg.destRoot, "CERTIFICATE", der, ownerMode{user: "root", group: clickhouseGroup, mode: 0o640}); err != nil {
		return nil, nil, false, err
	}
	return key, cert, true, nil
}

func readCA(keyPath, certPath string) (*rsa.PrivateKey, *x509.Certificate, error) {
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", keyPath, err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, nil, fmt.Errorf("%s does not contain a PEM block", keyPath)
	}
	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", keyPath, err)
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", certPath, err)
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, nil, fmt.Errorf("%s does not contain a PEM block", certPath)
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", certPath, err)
	}
	return key, cert, nil
}

func writeServerCertificate(cfg config, caKey *rsa.PrivateKey, caCert *x509.Certificate, keyPath, certPath string) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate ClickHouse server key: %w", err)
	}
	now := time.Now().UTC()
	serial, err := randomSerial()
	if err != nil {
		return fmt.Errorf("generate ClickHouse server cert serial: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create ClickHouse server cert: %w", err)
	}
	if err := writePEM(keyPath, cfg.destRoot, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key), ownerMode{user: "root", group: clickhouseGroup, mode: 0o640}); err != nil {
		return err
	}
	return writePEM(certPath, cfg.destRoot, "CERTIFICATE", der, ownerMode{user: "root", group: clickhouseGroup, mode: 0o640})
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, err
	}
	return serial, nil
}

func writePEM(path, destRoot, kind string, der []byte, mode ownerMode) error {
	var out bytes.Buffer
	if err := pem.Encode(&out, &pem.Block{Type: kind, Bytes: der}); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return writeFile(path, destRoot, out.Bytes(), mode)
}

func writeStaticConfig(cfg config) error {
	files := map[string]struct {
		body string
		mode ownerMode
	}{
		cfg.serverConfig:         {body: renderServerConfig(cfg), mode: ownerMode{user: "root", group: clickhouseGroup, mode: 0o640}},
		cfg.operatorClientConfig: {body: renderOperatorClientConfig(cfg), mode: ownerMode{user: "root", group: operatorGroup, mode: 0o640}},
		cfg.serverHelperConfig:   {body: renderServerHelperConfig(cfg), mode: ownerMode{user: "root", group: clickhouseGroup, mode: 0o640}},
		cfg.operatorHelperConfig: {body: renderOperatorHelperConfig(cfg), mode: ownerMode{user: "root", group: operatorGroup, mode: 0o640}},
		"/etc/systemd/system/clickhouse-server-spiffe-helper.service":        {body: renderServerHelperUnit(cfg), mode: ownerMode{user: "root", group: "root", mode: 0o644}},
		"/etc/systemd/system/clickhouse-operator-spiffe-helper.service":      {body: renderOperatorHelperUnit(cfg), mode: ownerMode{user: "root", group: "root", mode: 0o644}},
		"/etc/systemd/system/clickhouse-server.service":                      {body: renderClickHouseUnit(cfg), mode: ownerMode{user: "root", group: "root", mode: 0o644}},
		"/etc/systemd/system/clickhouse-server-spiffe-bundle-reload.service": {body: renderBundleReloadService(cfg), mode: ownerMode{user: "root", group: "root", mode: 0o644}},
		"/etc/systemd/system/clickhouse-server-spiffe-bundle-reload.path":    {body: renderBundleReloadPath(cfg), mode: ownerMode{user: "root", group: "root", mode: 0o644}},
	}
	for path, file := range files {
		if err := writeFile(destPath(cfg.destRoot, path), cfg.destRoot, []byte(file.body), file.mode); err != nil {
			return err
		}
	}
	return nil
}

func renderServerConfig(cfg config) string {
	return fmt.Sprintf(`%s
<clickhouse>
    <listen_host>%s</listen_host>
    <tcp_port_secure>%s</tcp_port_secure>

    <path>%s/</path>
    <tmp_path>%s/tmp/</tmp_path>
    <user_files_path>%s/user_files/</user_files_path>
    <format_schema_path>%s/format_schemas/</format_schema_path>
    <access_control_path>%s/access/</access_control_path>

    <logger>
        <level>information</level>
        <log>/var/log/clickhouse-server/clickhouse-server.log</log>
        <errorlog>/var/log/clickhouse-server/clickhouse-server.err.log</errorlog>
        <size>100M</size>
        <count>3</count>
    </logger>

    <openSSL>
        <server>
            <certificateFile>/etc/clickhouse-server/tls/server-cert.pem</certificateFile>
            <privateKeyFile>/etc/clickhouse-server/tls/server-key.pem</privateKeyFile>
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
        <%s>
            <ssl_certificates>
                <subject_alt_name>URI:%s/clickhouse-operator</subject_alt_name>
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
`, managedHeader, cfg.clickhouseHost, cfg.clickhousePort, cfg.dataPath, cfg.dataPath, cfg.dataPath, cfg.dataPath, cfg.dataPath, cfg.serverSpiffeDir, clickhouseDBUser, cfg.spiffeServicePrefix, clickhouseDBUser)
}

func renderOperatorClientConfig(cfg config) string {
	return fmt.Sprintf(`%s
<config>
    <secure>1</secure>
    <host>%s</host>
    <port>%s</port>
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
`, managedHeader, cfg.clickhouseHost, cfg.clickhousePort, cfg.operatorSpiffeDir, cfg.operatorSpiffeDir, cfg.operatorCAPath)
}

func renderServerHelperConfig(cfg config) string {
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
`, cfg.spireAgentSocket, cfg.serverSpiffeDir, cfg.serverPIDPath)
}

func renderOperatorHelperConfig(cfg config) string {
	return fmt.Sprintf(`agent_address = "%s"
cert_dir = "%s"
daemon_mode = true
svid_file_name = "svid.pem"
svid_key_file_name = "svid_key.pem"
svid_bundle_file_name = "bundle.pem"
cert_file_mode = 0640
key_file_mode = 0600
`, cfg.spireAgentSocket, cfg.operatorSpiffeDir)
}

func renderServerHelperUnit(cfg config) string {
	return fmt.Sprintf(`[Unit]
Description=ClickHouse server SPIFFE helper
After=network.target spire-agent.service
Wants=spire-agent.service

[Service]
Type=simple
User=%s
Group=%s
SupplementaryGroups=%s
ExecStart=/opt/verself/profile/bin/spiffe-helper -config %s
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=%s

[Install]
WantedBy=multi-user.target
`, clickhouseUser, clickhouseGroup, cfg.spireWorkloadGroup, cfg.serverHelperConfig, cfg.serverSpiffeDir)
}

func renderOperatorHelperUnit(cfg config) string {
	return fmt.Sprintf(`[Unit]
Description=ClickHouse operator SPIFFE helper
After=network.target spire-agent.service
Wants=spire-agent.service

[Service]
Type=simple
User=%s
Group=%s
SupplementaryGroups=%s
ExecStart=/opt/verself/profile/bin/spiffe-helper -config %s
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=%s

[Install]
WantedBy=multi-user.target
`, operatorUser, operatorGroup, cfg.spireWorkloadGroup, cfg.operatorHelperConfig, cfg.operatorSpiffeDir)
}

func renderClickHouseUnit(cfg config) string {
	return fmt.Sprintf(`[Unit]
Description=ClickHouse Server
After=verself-firewall.target network.target
Requires=verself-firewall.target

[Service]
Type=simple
User=%s
Group=%s
ExecStart=%s server --config-file %s --pid-file %s
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5
LimitNOFILE=500000
RuntimeDirectory=clickhouse-server
RuntimeDirectoryMode=0750

[Install]
WantedBy=multi-user.target
`, clickhouseUser, clickhouseGroup, cfg.clickhouseBin, cfg.serverConfig, cfg.serverPIDPath)
}

func renderBundleReloadService(cfg config) string {
	return fmt.Sprintf(`[Unit]
Description=Restart ClickHouse after SPIFFE trust bundle changes
After=clickhouse-server.service

[Service]
Type=oneshot
ExecStart=%s --bundle %s/bundle.pem --state-path %s --unit clickhouse-server.service
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=%s
`, cfg.bundleReloadBin, cfg.serverSpiffeDir, cfg.bundleReloadStatePath, cfg.serverSpiffeDir)
}

func renderBundleReloadPath(cfg config) string {
	return fmt.Sprintf(`[Unit]
Description=Watch ClickHouse SPIFFE bundle for trust-store reload
After=clickhouse-server-spiffe-helper.service
Requires=clickhouse-server-spiffe-helper.service

[Path]
PathChanged=%s/bundle.pem
Unit=clickhouse-server-spiffe-bundle-reload.service

[Install]
WantedBy=multi-user.target
`, cfg.serverSpiffeDir)
}

func convergeSystemd(ctx context.Context, cfg config) error {
	if err := command(ctx, 15*time.Second, "", cfg.systemctlBin, "daemon-reload"); err != nil {
		return err
	}
	for _, unit := range []string{"clickhouse-server-spiffe-helper.service", "clickhouse-operator-spiffe-helper.service", "clickhouse-server.service", "clickhouse-server-spiffe-bundle-reload.path"} {
		if err := command(ctx, 15*time.Second, "", cfg.systemctlBin, "enable", unit); err != nil {
			return err
		}
	}
	if err := command(ctx, 30*time.Second, "", cfg.systemctlBin, "restart", "clickhouse-server-spiffe-helper.service"); err != nil {
		return err
	}
	if err := waitForFile(destPath(cfg.destRoot, cfg.serverSpiffeDir, "bundle.pem"), 30*time.Second); err != nil {
		return err
	}
	if err := command(ctx, 45*time.Second, "", cfg.systemctlBin, "restart", "clickhouse-server.service"); err != nil {
		return err
	}
	if err := command(ctx, 30*time.Second, "", cfg.systemctlBin, "restart", "clickhouse-operator-spiffe-helper.service"); err != nil {
		return err
	}
	if err := waitForFile(destPath(cfg.destRoot, cfg.operatorSpiffeDir, "svid.pem"), 30*time.Second); err != nil {
		return err
	}
	if err := waitClickHouse(ctx, cfg); err != nil {
		return err
	}
	if err := command(ctx, 45*time.Second, "", cfg.bundleReloadBin, "--bundle", filepath.Join(cfg.serverSpiffeDir, "bundle.pem"), "--state-path", cfg.bundleReloadStatePath, "--unit", "clickhouse-server.service"); err != nil {
		return err
	}
	if err := waitClickHouse(ctx, cfg); err != nil {
		return err
	}
	if err := command(ctx, 15*time.Second, "", cfg.systemctlBin, "start", "clickhouse-server-spiffe-bundle-reload.path"); err != nil {
		return err
	}
	return command(ctx, 15*time.Second, "", cfg.clickhouseBin, "client", "--config-file", cfg.operatorClientConfig, "--user", clickhouseDBUser, "--query", "SYSTEM FLUSH LOGS")
}

func waitForFile(path string, deadline time.Duration) error {
	expires := time.Now().Add(deadline)
	for {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return nil
		}
		if time.Now().After(expires) {
			return fmt.Errorf("timed out waiting for %s", path)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func waitClickHouse(ctx context.Context, cfg config) error {
	var last error
	for i := 0; i < 15; i++ {
		last = command(ctx, 10*time.Second, "", cfg.clickhouseBin, "client", "--config-file", cfg.operatorClientConfig, "--user", clickhouseDBUser, "--query", "SELECT 1")
		if last == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("clickhouse did not become ready: %w", last)
}

func convergeBootstrapSchema(ctx context.Context, cfg config) error {
	source := filepath.Join(cfg.artifactRoot, "share", "verself", "clickhouse", "migrations", "001_initial_schema.up.sql")
	migration, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read bootstrap migration %s: %w", source, err)
	}
	fingerprint := bootstrapFingerprint(migration, cfg.spiffeServicePrefix)
	needed, err := bootstrapNeeded(ctx, cfg, fingerprint)
	if err != nil {
		return err
	}
	if !needed {
		return nil
	}
	rendered := bytes.ReplaceAll(migration, []byte("__VERSELF_SPIFFE_SERVICE_PREFIX__"), []byte(cfg.spiffeServicePrefix))
	if err := writeFile(destPath(cfg.destRoot, migrationInstallPath), cfg.destRoot, rendered, ownerMode{user: "root", group: "root", mode: 0o644}); err != nil {
		return err
	}
	if err := command(ctx, 15*time.Second, "", cfg.clickhouseBin, "client", "--config-file", cfg.operatorClientConfig, "--user", clickhouseDBUser, "--query", "CREATE DATABASE IF NOT EXISTS verself"); err != nil {
		return err
	}
	if err := command(ctx, 120*time.Second, "", cfg.clickhouseBin, "client", "--config-file", cfg.operatorClientConfig, "--user", clickhouseDBUser, "--database", "verself", "--multiquery", "--queries-file", migrationInstallPath); err != nil {
		return err
	}
	return writeFile(destPath(cfg.destRoot, migrationStatePath), cfg.destRoot, []byte(fingerprint+"\n"), ownerMode{user: "root", group: "root", mode: 0o644})
}

func bootstrapNeeded(ctx context.Context, cfg config, fingerprint string) (bool, error) {
	current, err := os.ReadFile(destPath(cfg.destRoot, migrationStatePath))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("read bootstrap state: %w", err)
		}
		return true, nil
	}
	if strings.TrimSpace(string(current)) != fingerprint {
		return true, nil
	}
	out, err := commandOutput(ctx, 15*time.Second, cfg.clickhouseBin, "client", "--config-file", cfg.operatorClientConfig, "--user", clickhouseDBUser, "--query", `
SELECT count()
FROM system.tables
WHERE (database = 'default' AND name IN ('otel_traces', 'otel_logs', 'otel_metrics_sum'))
   OR (database = 'verself' AND name = 'job_events')
`)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "4", nil
}

func bootstrapFingerprint(migration []byte, spiffeServicePrefix string) string {
	h := sha256.New()
	_, _ = h.Write(migration)
	_, _ = h.Write([]byte("spiffe_service_prefix=" + spiffeServicePrefix + "\n"))
	return hex.EncodeToString(h.Sum(nil))
}

func writeFile(path, destRoot string, body []byte, mode ownerMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
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
	if err := tmp.Chmod(mode.mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, path, err)
	}
	return chownPath(path, destRoot, mode.user, mode.group)
}

func chmodChown(path, destRoot string, mode ownerMode) error {
	if err := os.Chmod(path, mode.mode); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return chownPath(path, destRoot, mode.user, mode.group)
}

func chownPath(path, destRoot, userName, groupName string) error {
	if destRoot != "/" {
		return nil
	}
	uid, gid, err := lookupIDs(userName, groupName)
	if err != nil {
		return err
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("chown %s to %s:%s: %w", path, userName, groupName, err)
	}
	return nil
}

func lookupIDs(userName, groupName string) (int, int, error) {
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
		return 0, 0, fmt.Errorf("parse uid for %s: %w", userName, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse gid for %s: %w", groupName, err)
	}
	return uid, gid, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func destPath(destRoot string, parts ...string) string {
	joined := filepath.Join(parts...)
	if filepath.IsAbs(joined) {
		joined = strings.TrimPrefix(joined, string(os.PathSeparator))
	}
	return filepath.Join(destRoot, joined)
}

func command(ctx context.Context, timeout time.Duration, dir string, name string, args ...string) error {
	_, err := runCommand(ctx, timeout, dir, name, args...)
	return err
}

func commandOutput(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	out, err := runCommand(ctx, timeout, "", name, args...)
	return strings.TrimSpace(out), err
}

func runCommand(ctx context.Context, timeout time.Duration, dir string, name string, args ...string) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, name, args...)
	cmd.Dir = dir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if cmdCtx.Err() != nil {
			return "", fmt.Errorf("%s timed out: %w", commandString(name, args), cmdCtx.Err())
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("%s: %w: %s", commandString(name, args), err, detail)
	}
	return stdout.String(), nil
}

func commandString(name string, args []string) string {
	return strings.Join(append([]string{name}, args...), " ")
}
