package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
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
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	clickhouseUser  = "clickhouse"
	clickhouseGroup = "clickhouse"
	operatorUser    = "clickhouse_operator"
	operatorGroup   = "clickhouse_operator"
	spireGroup      = "spire_workload"

	verselfBin = "/opt/verself/profile/bin"

	clickhouseDataDir        = "/var/lib/clickhouse"
	clickhouseLogDir         = "/var/log/clickhouse-server"
	clickhouseServerSpiffe   = "/var/lib/clickhouse/spiffe"
	clickhouseOperatorHome   = "/var/lib/clickhouse-operator"
	clickhouseOperatorSpiffe = "/var/lib/clickhouse-operator/spiffe"

	serverConfigDir   = "/etc/clickhouse-server/config.d"
	serverTLSDir      = "/etc/clickhouse-server/tls"
	operatorConfigDir = "/etc/clickhouse-client"

	serverConfigPath         = "/etc/clickhouse-server/config.d/verself.xml"
	operatorConfigPath       = "/etc/clickhouse-client/operator.xml"
	operatorCAPath           = "/etc/clickhouse-client/server-ca.pem"
	serverHelperConfigPath   = "/etc/clickhouse-server/server-spiffe-helper.conf"
	operatorHelperConfigPath = "/etc/clickhouse-client/operator-spiffe-helper.conf"
	bundleReloadStatePath    = "/var/lib/clickhouse/spiffe/.bundle-reload.sha256"
)

var trustDomainRE = regexp.MustCompile(`(?m)^\s*trust_domain\s*=\s*"([^"]+)"\s*$`)

type options struct {
	ArtifactOpt string
}

type ids struct {
	clickhouseUID int
	clickhouseGID int
	operatorUID   int
	operatorGID   int
	rootGID       int
}

func main() {
	var opts options
	flag.StringVar(&opts.ArtifactOpt, "artifact-opt", "", "Path to the artifact opt/ directory.")
	flag.Parse()
	if err := run(context.Background(), opts); err != nil {
		fmt.Fprintf(os.Stderr, "clickhouse-host-install: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, opts options) error {
	if opts.ArtifactOpt == "" {
		return errors.New("--artifact-opt is required")
	}
	if err := installBinaries(opts.ArtifactOpt); err != nil {
		return err
	}
	if err := ensureGroupsAndUsers(ctx); err != nil {
		return err
	}
	ids, err := lookupIDs()
	if err != nil {
		return err
	}
	if err := convergeDirectories(ids); err != nil {
		return err
	}
	if err := convergeTLS(ids); err != nil {
		return err
	}
	trustDomain, err := readTrustDomain("/etc/spire/agent/agent.conf")
	if err != nil {
		return err
	}
	if err := writeFile("/etc/verself/spiffe-trust-domain", []byte(trustDomain+"\n"), 0644, 0, ids.rootGID); err != nil {
		return err
	}
	if err := convergeConfigs(trustDomain, ids); err != nil {
		return err
	}
	if err := convergeSystemd(ctx); err != nil {
		return err
	}
	return nil
}

func installBinaries(artifactOpt string) error {
	src := filepath.Join(artifactOpt, "verself", "profile", "bin")
	for _, name := range []string{"clickhouse", "clickhouse-spiffe-bundle-reload", "spiffe-helper"} {
		if err := copyExecutable(filepath.Join(src, name), filepath.Join(verselfBin, name)); err != nil {
			return err
		}
	}
	for _, name := range []string{"clickhouse-benchmark", "clickhouse-client", "clickhouse-keeper", "clickhouse-local", "clickhouse-server"} {
		link := filepath.Join(verselfBin, name)
		_ = os.Remove(link)
		if err := os.Symlink(filepath.Join(verselfBin, "clickhouse"), link); err != nil {
			return fmt.Errorf("symlink %s: %w", name, err)
		}
	}
	return nil
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy %s: %w", src, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("install %s: %w", dst, err)
	}
	return nil
}

func ensureGroupsAndUsers(ctx context.Context) error {
	for _, group := range []string{clickhouseGroup, operatorGroup} {
		if err := ensureGroup(ctx, group); err != nil {
			return err
		}
	}
	if err := ensureUser(ctx, clickhouseUser, clickhouseGroup, clickhouseDataDir); err != nil {
		return err
	}
	if err := ensureUser(ctx, operatorUser, operatorGroup, clickhouseOperatorHome); err != nil {
		return err
	}
	for _, account := range []string{clickhouseUser, operatorUser} {
		if err := runCommand(ctx, "usermod", "-a", "-G", spireGroup, account); err != nil {
			return fmt.Errorf("add %s to %s: %w", account, spireGroup, err)
		}
	}
	return nil
}

func ensureGroup(ctx context.Context, name string) error {
	if err := runCommand(ctx, "getent", "group", name); err == nil {
		return nil
	}
	return runCommand(ctx, "groupadd", "--system", name)
}

func ensureUser(ctx context.Context, name, group, home string) error {
	if err := runCommand(ctx, "id", "-u", name); err == nil {
		return nil
	}
	return runCommand(ctx, "useradd", "--system", "--gid", group, "--home-dir", home, "--shell", "/usr/sbin/nologin", "--no-create-home", name)
}

func lookupIDs() (ids, error) {
	clickhouseUID, err := lookupUserID(clickhouseUser)
	if err != nil {
		return ids{}, err
	}
	clickhouseGID, err := lookupGroupID(clickhouseGroup)
	if err != nil {
		return ids{}, err
	}
	operatorUID, err := lookupUserID(operatorUser)
	if err != nil {
		return ids{}, err
	}
	operatorGID, err := lookupGroupID(operatorGroup)
	if err != nil {
		return ids{}, err
	}
	rootGID, err := lookupGroupID("root")
	if err != nil {
		return ids{}, err
	}
	return ids{clickhouseUID: clickhouseUID, clickhouseGID: clickhouseGID, operatorUID: operatorUID, operatorGID: operatorGID, rootGID: rootGID}, nil
}

func lookupUserID(name string) (int, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, fmt.Errorf("lookup user %s: %w", name, err)
	}
	id, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, fmt.Errorf("parse uid for %s: %w", name, err)
	}
	return id, nil
}

func lookupGroupID(name string) (int, error) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, fmt.Errorf("lookup group %s: %w", name, err)
	}
	id, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, fmt.Errorf("parse gid for %s: %w", name, err)
	}
	return id, nil
}

func convergeDirectories(ids ids) error {
	dirs := []struct {
		path string
		uid  int
		gid  int
		mode os.FileMode
	}{
		{serverConfigDir, 0, ids.rootGID, 0755},
		{serverTLSDir, 0, ids.clickhouseGID, 0750},
		{clickhouseDataDir, ids.clickhouseUID, ids.clickhouseGID, 0750},
		{clickhouseLogDir, ids.clickhouseUID, ids.clickhouseGID, 0750},
		{operatorConfigDir, 0, ids.operatorGID, 0750},
		{clickhouseOperatorHome, ids.operatorUID, ids.operatorGID, 0750},
		{clickhouseServerSpiffe, ids.clickhouseUID, ids.clickhouseGID, 0700},
		{clickhouseOperatorSpiffe, ids.operatorUID, ids.operatorGID, 0700},
	}
	for _, dir := range dirs {
		if err := ensureDir(dir.path, dir.uid, dir.gid, dir.mode); err != nil {
			return err
		}
	}
	return nil
}

func ensureDir(path string, uid, gid int, mode os.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("mkdir %s: %w", path, err)
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

func convergeTLS(ids ids) error {
	caKeyPath := filepath.Join(serverTLSDir, "server-ca-key.pem")
	caCertPath := filepath.Join(serverTLSDir, "server-ca.pem")
	serverKeyPath := filepath.Join(serverTLSDir, "server-key.pem")
	serverCertPath := filepath.Join(serverTLSDir, "server-cert.pem")
	if allExist(caKeyPath, caCertPath, serverKeyPath, serverCertPath) {
		return convergeTLSModes(ids)
	}
	caKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return fmt.Errorf("generate ClickHouse CA key: %w", err)
	}
	now := time.Now().UTC()
	caTemplate := &x509.Certificate{
		SerialNumber:          serialNumber(),
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
		SerialNumber: serialNumber(),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create ClickHouse server certificate: %w", err)
	}
	if err := writePEM(caKeyPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(caKey), 0600, 0, ids.rootGID); err != nil {
		return err
	}
	if err := writePEM(caCertPath, "CERTIFICATE", caDER, 0640, 0, ids.clickhouseGID); err != nil {
		return err
	}
	if err := writePEM(serverKeyPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(serverKey), 0640, 0, ids.clickhouseGID); err != nil {
		return err
	}
	if err := writePEM(serverCertPath, "CERTIFICATE", serverDER, 0640, 0, ids.clickhouseGID); err != nil {
		return err
	}
	return convergeTLSModes(ids)
}

func allExist(paths ...string) bool {
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			return false
		}
	}
	return true
}

func convergeTLSModes(ids ids) error {
	files := []struct {
		path string
		uid  int
		gid  int
		mode os.FileMode
	}{
		{filepath.Join(serverTLSDir, "server-ca-key.pem"), 0, ids.rootGID, 0600},
		{filepath.Join(serverTLSDir, "server-ca.pem"), 0, ids.clickhouseGID, 0640},
		{filepath.Join(serverTLSDir, "server-key.pem"), 0, ids.clickhouseGID, 0640},
		{filepath.Join(serverTLSDir, "server-cert.pem"), 0, ids.clickhouseGID, 0640},
	}
	for _, file := range files {
		if err := os.Chown(file.path, file.uid, file.gid); err != nil {
			return fmt.Errorf("chown %s: %w", file.path, err)
		}
		if err := os.Chmod(file.path, file.mode); err != nil {
			return fmt.Errorf("chmod %s: %w", file.path, err)
		}
	}
	return copyFile(filepath.Join(serverTLSDir, "server-ca.pem"), operatorCAPath, 0640, 0, ids.operatorGID)
}

func serialNumber() *big.Int {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		panic(err)
	}
	return n
}

func writePEM(path, blockType string, der []byte, mode os.FileMode, uid, gid int) error {
	var buf bytes.Buffer
	if err := pem.Encode(&buf, &pem.Block{Type: blockType, Bytes: der}); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return writeFile(path, buf.Bytes(), mode, uid, gid)
}

func readTrustDomain(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read SPIRE agent config: %w", err)
	}
	match := trustDomainRE.FindSubmatch(body)
	if len(match) != 2 {
		return "", fmt.Errorf("trust_domain not found in %s", path)
	}
	return string(match[1]), nil
}

func convergeConfigs(trustDomain string, ids ids) error {
	operatorID := "spiffe://" + trustDomain + "/svc/clickhouse-operator"
	if err := writeFile(serverConfigPath, []byte(serverConfig(operatorID)), 0640, 0, ids.clickhouseGID); err != nil {
		return err
	}
	if err := writeFile(operatorConfigPath, []byte(operatorConfig()), 0640, 0, ids.operatorGID); err != nil {
		return err
	}
	if err := writeFile(serverHelperConfigPath, []byte(spiffeHelperConfig(clickhouseServerSpiffe, "/run/clickhouse-server/clickhouse-server.pid")), 0640, 0, ids.clickhouseGID); err != nil {
		return err
	}
	if err := writeFile(operatorHelperConfigPath, []byte(spiffeHelperConfig(clickhouseOperatorSpiffe, "")), 0640, 0, ids.operatorGID); err != nil {
		return err
	}
	units := map[string]string{
		"/etc/systemd/system/clickhouse-server-spiffe-helper.service":        spiffeHelperUnit("ClickHouse server SPIFFE helper", clickhouseUser, clickhouseGroup, serverHelperConfigPath, clickhouseServerSpiffe),
		"/etc/systemd/system/clickhouse-operator-spiffe-helper.service":      spiffeHelperUnit("ClickHouse operator SPIFFE helper", operatorUser, operatorGroup, operatorHelperConfigPath, clickhouseOperatorSpiffe),
		"/etc/systemd/system/clickhouse-server.service":                      clickhouseServerUnit(),
		"/etc/systemd/system/clickhouse-server-spiffe-bundle-reload.service": bundleReloadUnit(),
		"/etc/systemd/system/clickhouse-server-spiffe-bundle-reload.path":    bundleReloadPathUnit(),
	}
	for path, content := range units {
		if err := writeFile(path, []byte(content), 0644, 0, ids.rootGID); err != nil {
			return err
		}
	}
	return nil
}

func convergeSystemd(ctx context.Context) error {
	if err := runCommand(ctx, "systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := runCommand(ctx, "systemctl", "enable", "--now", "clickhouse-server-spiffe-helper.service"); err != nil {
		return err
	}
	if err := waitForPath(filepath.Join(clickhouseServerSpiffe, "bundle.pem"), 30*time.Second); err != nil {
		return err
	}
	if err := runCommand(ctx, "systemctl", "enable", "--now", "clickhouse-operator-spiffe-helper.service"); err != nil {
		return err
	}
	if err := waitForPath(filepath.Join(clickhouseOperatorSpiffe, "svid.pem"), 30*time.Second); err != nil {
		return err
	}
	if err := runCommand(ctx, "systemctl", "enable", "--now", "clickhouse-server.service"); err != nil {
		return err
	}
	if err := waitForClickHouse(ctx, 45*time.Second); err != nil {
		return err
	}
	if err := runCommand(ctx, filepath.Join(verselfBin, "clickhouse-spiffe-bundle-reload"), "--bundle", filepath.Join(clickhouseServerSpiffe, "bundle.pem"), "--state-path", bundleReloadStatePath, "--unit", "clickhouse-server.service"); err != nil {
		return err
	}
	if err := waitForClickHouse(ctx, 45*time.Second); err != nil {
		return err
	}
	return runCommand(ctx, "systemctl", "enable", "--now", "clickhouse-server-spiffe-bundle-reload.path")
}

func waitForPath(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("%s did not appear within %s", path, timeout)
}

func waitForClickHouse(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		err := runCommand(ctx, filepath.Join(verselfBin, "clickhouse-client"), "--config-file", operatorConfigPath, "--user", operatorUser, "--query", "SELECT 1")
		if err == nil {
			return nil
		}
		lastErr = err
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("ClickHouse did not become ready within %s: %w", timeout, lastErr)
}

func writeFile(path string, body []byte, mode os.FileMode, uid, gid int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, mode); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Chown(tmp, uid, gid); err != nil {
		return fmt.Errorf("chown %s: %w", tmp, err)
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("install %s: %w", path, err)
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode, uid, gid int) error {
	body, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	return writeFile(dst, body, mode, uid, gid)
}

func runCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, msg)
		}
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func serverConfig(operatorID string) string {
	return fmt.Sprintf(`<clickhouse>
    <listen_host>127.0.0.1</listen_host>
    <tcp_port_secure>9440</tcp_port_secure>
    <path>/var/lib/clickhouse/</path>
    <tmp_path>/var/lib/clickhouse/tmp/</tmp_path>
    <user_files_path>/var/lib/clickhouse/user_files/</user_files_path>
    <format_schema_path>/var/lib/clickhouse/format_schemas/</format_schema_path>
    <access_control_path>/var/lib/clickhouse/access/</access_control_path>
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
            <caConfig>/var/lib/clickhouse/spiffe/bundle.pem</caConfig>
            <cacheSessions>true</cacheSessions>
            <disableProtocols>sslv2,sslv3</disableProtocols>
            <preferServerCiphers>true</preferServerCiphers>
        </server>
    </openSSL>
    <profiles><default/></profiles>
    <users>
        <clickhouse_operator>
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
        </clickhouse_operator>
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
`, operatorID)
}

func operatorConfig() string {
	return `<config>
    <secure>1</secure>
    <host>127.0.0.1</host>
    <port>9440</port>
    <openSSL>
        <client>
            <certificateFile>/var/lib/clickhouse-operator/spiffe/svid.pem</certificateFile>
            <privateKeyFile>/var/lib/clickhouse-operator/spiffe/svid_key.pem</privateKeyFile>
            <caConfig>/etc/clickhouse-client/server-ca.pem</caConfig>
            <cacheSessions>true</cacheSessions>
            <disableProtocols>sslv2,sslv3</disableProtocols>
            <preferServerCiphers>true</preferServerCiphers>
            <invalidCertificateHandler>
                <name>RejectCertificateHandler</name>
            </invalidCertificateHandler>
        </client>
    </openSSL>
</config>
`
}

func spiffeHelperConfig(certDir, pidFile string) string {
	pid := ""
	if pidFile != "" {
		pid = fmt.Sprintf("pid_file_name = %q\nrenew_signal = \"SIGHUP\"\n", pidFile)
	}
	return fmt.Sprintf(`agent_address = "/run/spire-agent/sockets/agent.sock"
cert_dir = %q
daemon_mode = true
%ssvid_file_name = "svid.pem"
svid_key_file_name = "svid_key.pem"
svid_bundle_file_name = "bundle.pem"
cert_file_mode = 0640
key_file_mode = 0600
`, certDir, pid)
}

func spiffeHelperUnit(description, runUser, runGroup, configPath, writePath string) string {
	return fmt.Sprintf(`[Unit]
Description=%s
After=network.target spire-agent.service
Wants=spire-agent.service

[Service]
Type=simple
User=%s
Group=%s
SupplementaryGroups=%s
ExecStart=%s/spiffe-helper -config %s
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=%s

[Install]
WantedBy=multi-user.target
`, description, runUser, runGroup, spireGroup, verselfBin, configPath, writePath)
}

func clickhouseServerUnit() string {
	return fmt.Sprintf(`[Unit]
Description=ClickHouse Server
After=network.target clickhouse-server-spiffe-helper.service
Wants=clickhouse-server-spiffe-helper.service

[Service]
Type=simple
User=%s
Group=%s
ExecStart=%s/clickhouse-server --config-file %s --pid-file /run/clickhouse-server/clickhouse-server.pid
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5
LimitNOFILE=500000
RuntimeDirectory=clickhouse-server
RuntimeDirectoryMode=0750

[Install]
WantedBy=multi-user.target
`, clickhouseUser, clickhouseGroup, verselfBin, serverConfigPath)
}

func bundleReloadUnit() string {
	return fmt.Sprintf(`[Unit]
Description=Reload ClickHouse when SPIFFE server bundle changes

[Service]
Type=oneshot
ExecStart=%s/clickhouse-spiffe-bundle-reload --bundle %s --state-path %s --unit clickhouse-server.service
`, verselfBin, filepath.Join(clickhouseServerSpiffe, "bundle.pem"), bundleReloadStatePath)
}

func bundleReloadPathUnit() string {
	return fmt.Sprintf(`[Unit]
Description=Watch ClickHouse SPIFFE bundle

[Path]
PathChanged=%s
Unit=clickhouse-server-spiffe-bundle-reload.service

[Install]
WantedBy=multi-user.target
`, filepath.Join(clickhouseServerSpiffe, "bundle.pem"))
}
