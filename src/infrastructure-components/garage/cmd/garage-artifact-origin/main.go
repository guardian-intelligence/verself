package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	rpcSecretPath  = "/etc/garage/rpc-secret"
	adminTokenPath = "/etc/garage/admin-token"
)

type garageNode struct {
	Instance  int
	S3Port    int
	RPCPort   int
	AdminPort int
}

type options struct {
	GarageBin          string
	Nodes              []garageNode
	CapacityBytes      int64
	ZoneRedundancy     int
	Bucket             string
	ReaderEnv          string
	ReaderAccessEnv    string
	ReaderSecretEnv    string
	PublisherEnv       string
	PublisherAccessEnv string
	PublisherSecretEnv string
	OriginHostname     string
	CABundlePath       string
	Listen             string
	Target             string
}

type nodeInfo struct {
	ID   string
	Addr string
	Full string
	Node garageNode
}

var envValueRE = regexp.MustCompile(`^[A-Za-z0-9/+=._:-]+$`)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "garage-artifact-origin: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	opts, err := parseOptions()
	if err != nil {
		return err
	}
	adminToken, err := readTrimmed(adminTokenPath)
	if err != nil {
		return err
	}
	client := &garageAdminClient{
		BaseURL: fmt.Sprintf("http://127.0.0.1:%d", opts.Nodes[0].AdminPort),
		Token:   adminToken,
		Client:  &http.Client{Timeout: 5 * time.Second},
	}
	if err := waitForGarageAdmin(ctx, client, 90*time.Second); err != nil {
		return err
	}
	infos, err := collectNodeInfo(opts)
	if err != nil {
		return err
	}
	if err := convergeClusterLayout(ctx, opts, client, infos); err != nil {
		return err
	}
	creds, err := convergeArtifactCredentials(client, opts.Bucket)
	if err != nil {
		return err
	}
	if err := writeArtifactEnvFiles(opts, creds); err != nil {
		return err
	}
	if err := convergeTLS(opts); err != nil {
		return err
	}
	if err := convergeHostsLine(opts.OriginHostname); err != nil {
		return err
	}
	return serveProxy(ctx, opts)
}

func parseOptions() (options, error) {
	nodesRaw := flag.String("nodes", "0:3900:3901:3903,1:3910:3911:3913,2:3920:3921:3923", "Comma-separated instance:s3:rpc:admin node list.")
	opts := options{}
	flag.StringVar(&opts.GarageBin, "garage-bin", "", "Garage binary path.")
	flag.Int64Var(&opts.CapacityBytes, "capacity-bytes", 50000000000, "Garage capacity bytes per local node.")
	flag.IntVar(&opts.ZoneRedundancy, "zone-redundancy", 3, "Garage zone redundancy target.")
	flag.StringVar(&opts.Bucket, "bucket", "nomad-artifacts", "Nomad artifact bucket.")
	flag.StringVar(&opts.ReaderEnv, "reader-env", "/etc/nomad/nomad-artifacts.env", "Nomad artifact reader environment file.")
	flag.StringVar(&opts.ReaderAccessEnv, "reader-access-env", "AWS_ACCESS_KEY_ID", "Reader access-key env var.")
	flag.StringVar(&opts.ReaderSecretEnv, "reader-secret-env", "AWS_SECRET_ACCESS_KEY", "Reader secret-key env var.")
	flag.StringVar(&opts.PublisherEnv, "publisher-env", "/etc/garage/nomad-artifacts/publisher.env", "Controller publisher environment file.")
	flag.StringVar(&opts.PublisherAccessEnv, "publisher-access-env", "VERSELF_NOMAD_ARTIFACTS_S3_ACCESS_KEY_ID", "Publisher access-key env var.")
	flag.StringVar(&opts.PublisherSecretEnv, "publisher-secret-env", "VERSELF_NOMAD_ARTIFACTS_S3_SECRET_ACCESS_KEY", "Publisher secret-key env var.")
	flag.StringVar(&opts.OriginHostname, "origin-hostname", "", "Private artifact origin hostname.")
	flag.StringVar(&opts.CABundlePath, "ca-bundle", "/etc/verself/pki/nomad-artifacts-ca.pem", "Artifact origin CA bundle path.")
	flag.StringVar(&opts.Listen, "listen", "127.0.0.1:9443", "HTTPS proxy listen address.")
	flag.StringVar(&opts.Target, "target", "http://127.0.0.1:3900", "Garage S3 target URL.")
	flag.Parse()
	if opts.GarageBin == "" {
		return options{}, errors.New("--garage-bin is required")
	}
	if opts.OriginHostname == "" {
		return options{}, errors.New("--origin-hostname is required")
	}
	nodes, err := parseNodes(*nodesRaw)
	if err != nil {
		return options{}, err
	}
	opts.Nodes = nodes
	if opts.CapacityBytes <= 0 {
		return options{}, errors.New("--capacity-bytes must be positive")
	}
	if opts.ZoneRedundancy <= 0 {
		return options{}, errors.New("--zone-redundancy must be positive")
	}
	return opts, nil
}

func parseNodes(raw string) ([]garageNode, error) {
	parts := strings.Split(raw, ",")
	nodes := make([]garageNode, 0, len(parts))
	seen := map[int]bool{}
	for _, part := range parts {
		fields := strings.Split(strings.TrimSpace(part), ":")
		if len(fields) != 4 {
			return nil, fmt.Errorf("node %q must be instance:s3:rpc:admin", part)
		}
		values := [4]int{}
		for i, field := range fields {
			value, err := strconv.Atoi(field)
			if err != nil || value < 0 {
				return nil, fmt.Errorf("node %q contains invalid integer %q", part, field)
			}
			values[i] = value
		}
		if seen[values[0]] {
			return nil, fmt.Errorf("duplicate garage instance %d", values[0])
		}
		seen[values[0]] = true
		nodes = append(nodes, garageNode{Instance: values[0], S3Port: values[1], RPCPort: values[2], AdminPort: values[3]})
	}
	if len(nodes) == 0 {
		return nil, errors.New("at least one garage node is required")
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Instance < nodes[j].Instance })
	return nodes, nil
}

func waitForGarageAdmin(ctx context.Context, client *garageAdminClient, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	var lastErr error
	for time.Now().Before(deadline) {
		_, err := client.Get("GetClusterStatus", nil, false)
		if err == nil {
			return nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("garage admin did not become ready: %w", lastErr)
}

func collectNodeInfo(opts options) ([]nodeInfo, error) {
	infos := make([]nodeInfo, 0, len(opts.Nodes))
	for _, node := range opts.Nodes {
		out, err := runOutput(opts.GarageBin, "-c", fmt.Sprintf("/etc/garage/garage-%d.toml", node.Instance), "node", "id", "-q")
		if err != nil {
			return nil, err
		}
		full := strings.TrimSpace(out)
		id, addr, ok := strings.Cut(full, "@")
		if !ok || id == "" || addr == "" {
			return nil, fmt.Errorf("unexpected garage node id output for instance %d: %q", node.Instance, full)
		}
		infos = append(infos, nodeInfo{ID: id, Addr: addr, Full: full, Node: node})
	}
	return infos, nil
}

func convergeClusterLayout(ctx context.Context, opts options, client *garageAdminClient, infos []nodeInfo) error {
	status, err := client.Get("GetClusterStatus", nil, false)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, raw := range sliceValue(status["nodes"]) {
		node := mapValue(raw)
		known[stringValue(node["addr"])] = true
	}
	rpcHost := infos[0].Full
	for _, info := range infos[1:] {
		if known[info.Addr] {
			continue
		}
		if _, err := runOutput(opts.GarageBin, "-h", rpcHost, "--rpc-secret-file", rpcSecretPath, "node", "connect", info.Full); err != nil {
			return err
		}
	}
	if err := waitForClusterUp(ctx, client, infos, 30*time.Second); err != nil {
		return err
	}
	layout, err := client.Get("GetClusterLayout", nil, false)
	if err != nil {
		return err
	}
	desired := desiredRoles(opts, infos)
	if layoutMatches(layout, desired, opts.ZoneRedundancy) {
		return nil
	}
	_, _ = runOutput(opts.GarageBin, "-h", rpcHost, "--rpc-secret-file", rpcSecretPath, "layout", "revert", "--yes")
	version := intValue(layout["version"])
	if _, err := runOutput(opts.GarageBin, "-h", rpcHost, "--rpc-secret-file", rpcSecretPath, "layout", "config", "--redundancy", strconv.Itoa(opts.ZoneRedundancy)); err != nil {
		return err
	}
	for _, role := range desired {
		args := []string{"-h", rpcHost, "--rpc-secret-file", rpcSecretPath, "layout", "assign", role.ID, "--zone", role.Zone, "--capacity", strconv.FormatInt(role.Capacity, 10)}
		for _, tag := range role.Tags {
			args = append(args, "--tag", tag)
		}
		if _, err := runOutput(opts.GarageBin, args...); err != nil {
			return err
		}
	}
	if _, err := runOutput(opts.GarageBin, "-h", rpcHost, "--rpc-secret-file", rpcSecretPath, "layout", "apply", "--version", strconv.Itoa(version+1)); err != nil {
		return err
	}
	return waitForLayout(ctx, client, desired, opts.ZoneRedundancy, 30*time.Second)
}

func waitForClusterUp(ctx context.Context, client *garageAdminClient, infos []nodeInfo, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		status, err := client.Get("GetClusterStatus", nil, false)
		if err == nil {
			up := map[string]bool{}
			for _, raw := range sliceValue(status["nodes"]) {
				node := mapValue(raw)
				if boolValue(node["isUp"]) {
					up[stringValue(node["addr"])] = true
				}
			}
			all := true
			for _, info := range infos {
				all = all && up[info.Addr]
			}
			if all {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return errors.New("garage cluster nodes did not converge")
}

type desiredRole struct {
	ID       string
	Zone     string
	Capacity int64
	Tags     []string
}

func desiredRoles(opts options, infos []nodeInfo) []desiredRole {
	roles := make([]desiredRole, 0, len(infos))
	for _, info := range infos {
		roles = append(roles, desiredRole{
			ID:       info.ID,
			Zone:     fmt.Sprintf("garage-%d", info.Node.Instance),
			Capacity: opts.CapacityBytes,
			Tags:     []string{fmt.Sprintf("garage-%d", info.Node.Instance), "single-node"},
		})
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].ID < roles[j].ID })
	return roles
}

func layoutMatches(layout map[string]any, desired []desiredRole, zoneRedundancy int) bool {
	if len(sliceValue(layout["stagedRoleChanges"])) > 0 || len(sliceValue(layout["stagedParameters"])) > 0 {
		return false
	}
	current := currentRoles(layout)
	parameters := mapValue(layout["parameters"])
	zone := mapValue(parameters["zoneRedundancy"])
	return reflect.DeepEqual(current, desired) && intValue(zone["atLeast"]) == zoneRedundancy
}

func currentRoles(layout map[string]any) []desiredRole {
	roles := []desiredRole{}
	for _, raw := range sliceValue(layout["roles"]) {
		role := mapValue(raw)
		tags := []string{}
		for _, tag := range sliceValue(role["tags"]) {
			tags = append(tags, stringValue(tag))
		}
		sort.Strings(tags)
		roles = append(roles, desiredRole{
			ID:       stringValue(role["id"]),
			Zone:     stringValue(role["zone"]),
			Capacity: int64Value(role["capacity"]),
			Tags:     tags,
		})
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].ID < roles[j].ID })
	return roles
}

func waitForLayout(ctx context.Context, client *garageAdminClient, desired []desiredRole, zoneRedundancy int, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		layout, err := client.Get("GetClusterLayout", nil, false)
		if err == nil && layoutMatches(layout, desired, zoneRedundancy) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return errors.New("garage layout did not settle")
}

type artifactCreds struct {
	Reader    map[string]any
	Publisher map[string]any
}

func convergeArtifactCredentials(client *garageAdminClient, bucketName string) (artifactCreds, error) {
	bucket, err := getOrCreateBucket(client, bucketName)
	if err != nil {
		return artifactCreds{}, err
	}
	reader, err := getOrCreateKey(client, "verself-nomad-artifacts-reader")
	if err != nil {
		return artifactCreds{}, err
	}
	publisher, err := getOrCreateKey(client, "verself-nomad-artifacts-publisher")
	if err != nil {
		return artifactCreds{}, err
	}
	if err := ensureEnvSafe(reader); err != nil {
		return artifactCreds{}, err
	}
	if err := ensureEnvSafe(publisher); err != nil {
		return artifactCreds{}, err
	}
	bucket, err = ensureKeyPermission(client, bucket, reader, true, false, false)
	if err != nil {
		return artifactCreds{}, err
	}
	if _, err := ensureKeyPermission(client, bucket, publisher, true, true, false); err != nil {
		return artifactCreds{}, err
	}
	return artifactCreds{Reader: reader, Publisher: publisher}, nil
}

func getOrCreateBucket(client *garageAdminClient, name string) (map[string]any, error) {
	found, err := client.Get("GetBucketInfo", url.Values{"globalAlias": []string{name}}, true)
	if err != nil {
		return nil, err
	}
	if found != nil {
		return found, nil
	}
	return client.Post("CreateBucket", map[string]any{"globalAlias": name})
}

func getOrCreateKey(client *garageAdminClient, name string) (map[string]any, error) {
	found, err := client.Get("GetKeyInfo", url.Values{"search": []string{name}, "showSecretKey": []string{"true"}}, true)
	if err != nil {
		return nil, err
	}
	if found != nil {
		return found, nil
	}
	return client.Post("CreateKey", map[string]any{"name": name, "neverExpires": true})
}

func ensureKeyPermission(client *garageAdminClient, bucket, key map[string]any, read, write, owner bool) (map[string]any, error) {
	accessKeyID := stringValue(key["accessKeyId"])
	desired := map[string]bool{"read": read, "write": write, "owner": owner}
	for _, raw := range sliceValue(bucket["keys"]) {
		entry := mapValue(raw)
		if stringValue(entry["accessKeyId"]) != accessKeyID {
			continue
		}
		current := mapValue(entry["permissions"])
		matches := true
		deny := map[string]bool{}
		for name, value := range desired {
			if boolValue(current[name]) != value {
				matches = false
			}
			if !value && boolValue(current[name]) {
				deny[name] = true
			}
		}
		if matches {
			return bucket, nil
		}
		if len(deny) > 0 {
			next, err := client.Post("DenyBucketKey", map[string]any{"bucketId": stringValue(bucket["id"]), "accessKeyId": accessKeyID, "permissions": deny})
			if err != nil {
				return nil, err
			}
			bucket = next
		}
		break
	}
	return client.Post("AllowBucketKey", map[string]any{
		"bucketId":    stringValue(bucket["id"]),
		"accessKeyId": accessKeyID,
		"permissions": desired,
	})
}

func ensureEnvSafe(key map[string]any) error {
	for _, field := range []string{"accessKeyId", "secretAccessKey"} {
		value := stringValue(key[field])
		if !envValueRE.MatchString(value) {
			return fmt.Errorf("garage generated unsafe %s for environment file serialization", field)
		}
	}
	return nil
}

func writeArtifactEnvFiles(opts options, creds artifactCreds) error {
	if err := writeEnvFile(opts.ReaderEnv, []string{
		fmt.Sprintf("%s=%s", opts.ReaderAccessEnv, stringValue(creds.Reader["accessKeyId"])),
		fmt.Sprintf("%s=%s", opts.ReaderSecretEnv, stringValue(creds.Reader["secretAccessKey"])),
		"AWS_REGION=garage",
		"AWS_EC2_METADATA_DISABLED=true",
	}); err != nil {
		return err
	}
	return writeEnvFile(opts.PublisherEnv, []string{
		fmt.Sprintf("%s=%s", opts.PublisherAccessEnv, stringValue(creds.Publisher["accessKeyId"])),
		fmt.Sprintf("%s=%s", opts.PublisherSecretEnv, stringValue(creds.Publisher["secretAccessKey"])),
	})
}

func writeEnvFile(path string, lines []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func convergeTLS(opts options) error {
	caKeyPath := "/etc/verself/pki/private/nomad-artifacts-ca-key.pem"
	serverKeyPath := "/etc/verself/pki/private/nomad-artifacts-server-key.pem"
	serverCertPath := "/etc/verself/pki/nomad-artifacts-server.pem"
	if err := os.MkdirAll("/etc/verself/pki/private", 0o750); err != nil {
		return fmt.Errorf("mkdir pki private: %w", err)
	}
	if !fileExists(opts.CABundlePath) || !fileExists(caKeyPath) {
		if err := generateCA(opts.CABundlePath, caKeyPath); err != nil {
			return err
		}
	}
	if !serverTLSReady(opts, serverCertPath, serverKeyPath) {
		if err := generateServerCert(opts, serverCertPath, serverKeyPath, opts.CABundlePath, caKeyPath); err != nil {
			return err
		}
	}
	trustPath := "/usr/local/share/ca-certificates/verself-nomad-artifacts.crt"
	if err := copyFile(opts.CABundlePath, trustPath, 0o644); err != nil {
		return err
	}
	cmd := exec.Command("update-ca-certificates")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("update-ca-certificates: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func serverTLSReady(opts options, certPath, keyPath string) bool {
	cert, err := readCertificate(certPath)
	if err != nil {
		return false
	}
	key, err := readPrivateKey(keyPath)
	if err != nil {
		return false
	}
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok || pub.E != key.PublicKey.E || pub.N.Cmp(key.PublicKey.N) != 0 {
		return false
	}
	now := time.Now()
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return false
	}
	if err := cert.VerifyHostname(opts.OriginHostname); err != nil {
		return false
	}
	return cert.VerifyHostname("127.0.0.1") == nil
}

func generateCA(certPath, keyPath string) error {
	key, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return fmt.Errorf("generate artifact CA key: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serialNumber(),
		Subject:               pkix.Name{CommonName: "Verself Nomad artifact local CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("create artifact CA certificate: %w", err)
	}
	if err := writePEM(keyPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key), 0o600); err != nil {
		return err
	}
	return writePEM(certPath, "CERTIFICATE", certDER, 0o644)
}

func generateServerCert(opts options, certPath, keyPath, caCertPath, caKeyPath string) error {
	caCert, err := readCertificate(caCertPath)
	if err != nil {
		return err
	}
	caKey, err := readPrivateKey(caKeyPath)
	if err != nil {
		return err
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate artifact server key: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serialNumber(),
		Subject:      pkix.Name{CommonName: opts.OriginHostname},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{opts.OriginHostname},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create artifact server certificate: %w", err)
	}
	if err := writePEM(keyPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key), 0o600); err != nil {
		return err
	}
	return writePEM(certPath, "CERTIFICATE", certDER, 0o644)
}

func serialNumber() *big.Int {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		panic(err)
	}
	return n
}

func writePEM(path, typ string, der []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	var b bytes.Buffer
	if err := pem.Encode(&b, &pem.Block{Type: typ, Bytes: der}); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := os.WriteFile(path, b.Bytes(), mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func readCertificate(path string) (*x509.Certificate, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	block, _ := pem.Decode(body)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("%s does not contain a certificate PEM", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return cert, nil
}

func readPrivateKey(path string) (*rsa.PrivateKey, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	block, _ := pem.Decode(body)
	if block == nil {
		return nil, fmt.Errorf("%s does not contain a PEM block", path)
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse %s as PKCS#1 or PKCS#8 RSA private key: %w", path, err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("parse %s: PEM block is %T, not RSA private key", path, parsed)
	}
	return key, nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	body, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}
	if err := os.WriteFile(dst, body, mode); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return nil
}

func convergeHostsLine(hostname string) error {
	body, err := os.ReadFile("/etc/hosts")
	if err != nil {
		return fmt.Errorf("read /etc/hosts: %w", err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		for _, field := range fields[1:] {
			if field == hostname {
				return nil
			}
		}
	}
	f, err := os.OpenFile("/etc/hosts", os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open /etc/hosts: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := fmt.Fprintf(f, "127.0.0.1 %s\n", hostname); err != nil {
		return fmt.Errorf("append /etc/hosts: %w", err)
	}
	return nil
}

func serveProxy(ctx context.Context, opts options) error {
	target, err := url.Parse(opts.Target)
	if err != nil {
		return fmt.Errorf("parse target: %w", err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	serverCertPath := "/etc/verself/pki/nomad-artifacts-server.pem"
	serverKeyPath := "/etc/verself/pki/private/nomad-artifacts-server-key.pem"
	server := &http.Server{
		Addr:              opts.Listen,
		Handler:           proxy,
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServeTLS(serverCertPath, serverKeyPath)
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

type garageAdminClient struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

func (c *garageAdminClient) Get(endpoint string, query url.Values, missingOK bool) (map[string]any, error) {
	return c.do(http.MethodGet, endpoint, nil, query, missingOK)
}

func (c *garageAdminClient) Post(endpoint string, payload any) (map[string]any, error) {
	return c.do(http.MethodPost, endpoint, payload, nil, false)
}

func (c *garageAdminClient) do(method, endpoint string, payload any, query url.Values, missingOK bool) (map[string]any, error) {
	u := strings.TrimRight(c.BaseURL, "/") + "/v2/" + endpoint
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal %s payload: %w", endpoint, err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if missingOK && resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s returned HTTP %d: %s", method, endpoint, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if len(strings.TrimSpace(string(respBody))) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", endpoint, err)
	}
	return out, nil
}

func runOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func readTrimmed(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	value := strings.TrimSpace(string(body))
	if value == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return value, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func mapValue(v any) map[string]any {
	if out, ok := v.(map[string]any); ok {
		return out
	}
	return map[string]any{}
}

func sliceValue(v any) []any {
	if out, ok := v.([]any); ok {
		return out
	}
	return nil
}

func stringValue(v any) string {
	if out, ok := v.(string); ok {
		return out
	}
	return ""
}

func boolValue(v any) bool {
	if out, ok := v.(bool); ok {
		return out
	}
	return false
}

func intValue(v any) int {
	return int(int64Value(v))
}

func int64Value(v any) int64 {
	switch value := v.(type) {
	case float64:
		return int64(value)
	case int:
		return int64(value)
	case int64:
		return value
	case json.Number:
		out, _ := value.Int64()
		return out
	default:
		return 0
	}
}
