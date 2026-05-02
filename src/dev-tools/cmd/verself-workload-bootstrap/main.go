// Command verself-workload-bootstrap brings a Devin / Cursor / CI VM
// onto wg-ops, signs an SSH cert via the OpenBao workload AppRole, and
// exits. It runs once at workload boot; the cert is valid for the
// AppRole's token_max_ttl (24h by default). Extending past that
// requires a fresh `aspect operator enroll-workload` from the operator.
//
// Required environment (set by `aspect operator enroll-workload`):
//
//	VERSELF_DOMAIN              public domain serving /.well-known/verself-*
//	VERSELF_BOOTSTRAP_ROLE_ID   AppRole role_id
//	VERSELF_BOOTSTRAP_SECRET_ID AppRole secret_id (single-use, 15-min TTL)
//	VERSELF_SLOT                workload-pool slot index (string)
//	VERSELF_DEVICE              workload tag stamped into the cert KeyID
//	VERSELF_WG_PRIVATE_KEY      slot's wg priv key
//	VERSELF_WG_ADDRESS          slot's wg-ops IP
//	VERSELF_WG_ENDPOINT         <host>:<port> for wg-ops
//	VERSELF_WG_HOST_ADDRESS     wg-ops host address (10.66.66.1)
//	VERSELF_WG_NETWORK          wg-ops CIDR (10.66.66.0/24)
//	VERSELF_WG_SERVER_PUBKEY    wg-ops server pubkey
//
// Optional:
//
//	VERSELF_INTERFACE   wg interface name (default: verself-workload)
//	VERSELF_SSH_KEY     path to existing ed25519 priv key; default
//	                    /etc/verself/ssh/id (generated if missing)
//	VERSELF_SSH_CERT    path to write the signed cert (default
//	                    /etc/verself/ssh/id-cert.pub)
package main

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "verself-workload-bootstrap: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	domain := mustEnv("VERSELF_DOMAIN")
	roleID := mustEnv("VERSELF_BOOTSTRAP_ROLE_ID")
	secretID := mustEnv("VERSELF_BOOTSTRAP_SECRET_ID")
	slot := mustEnv("VERSELF_SLOT")
	device := mustEnv("VERSELF_DEVICE")
	wgPriv := mustEnv("VERSELF_WG_PRIVATE_KEY")
	wgAddr := mustEnv("VERSELF_WG_ADDRESS")
	wgEndpoint := mustEnv("VERSELF_WG_ENDPOINT")
	wgHost := mustEnv("VERSELF_WG_HOST_ADDRESS")
	wgNetwork := mustEnv("VERSELF_WG_NETWORK")
	wgServerPub := mustEnv("VERSELF_WG_SERVER_PUBKEY")

	iface := envOr("VERSELF_INTERFACE", "verself-workload")
	sshKey := envOr("VERSELF_SSH_KEY", "/etc/verself/ssh/id")
	sshCert := envOr("VERSELF_SSH_CERT", "/etc/verself/ssh/id-cert.pub")

	// 1. Fetch + pin the OpenBao TLS CA. Uses public TLS to the
	//    operator-facing apex; sha256 verification means a rogue CA
	//    can't substitute without the secret-id (which only the
	//    operator side ever holds).
	caBytes, err := fetchAndPin(
		fmt.Sprintf("https://%s/.well-known/verself-openbao-ca.pem", domain),
		"/etc/verself/trust-anchors/verself-openbao-ca.pem",
	)
	if err != nil {
		return err
	}

	// 2. Bring up wg-ops. wg-quick is required on the workload image;
	//    the bootstrap binary itself does not vendor a userspace
	//    implementation.
	if err := writeWGConfig(iface, wgPriv, wgAddr, wgServerPub, wgEndpoint, wgNetwork); err != nil {
		return err
	}
	if err := wgQuickUp(iface); err != nil {
		return err
	}

	// 3. AppRole login. Returns a Vault token bound to ssh-ca-workload
	//    policy with token_ttl=24h; the secret-id is consumed by the
	//    server (single-use), so a stolen env block can't be replayed
	//    after this call lands.
	bao, err := newBaoClient(fmt.Sprintf("https://%s:8200", wgHost), caBytes, "")
	if err != nil {
		return err
	}
	type loginOut struct {
		Auth struct {
			ClientToken   string `json:"client_token"`
			LeaseDuration int    `json:"lease_duration"`
		} `json:"auth"`
	}
	var login loginOut
	if _, err := bao.do("POST", "/v1/auth/approle/login", map[string]any{
		"role_id":   roleID,
		"secret_id": secretID,
	}, &login); err != nil {
		return fmt.Errorf("workload AppRole login: %w", err)
	}
	if login.Auth.ClientToken == "" {
		return fmt.Errorf("workload AppRole login returned empty client_token")
	}
	bao.token = login.Auth.ClientToken

	// 4. Ensure SSH keypair, then sign.
	if err := os.MkdirAll(filepath.Dir(sshKey), 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(sshKey); os.IsNotExist(err) {
		cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-f", sshKey, "-N", "",
			"-C", fmt.Sprintf("verself-workload-%s", device))
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("ssh-keygen: %w", err)
		}
	}
	pub, err := os.ReadFile(sshKey + ".pub")
	if err != nil {
		return err
	}
	keyID := fmt.Sprintf("verself-workload-slot-%s", slot)
	signed, err := bao.signSSHCert("ssh-ca", "workload", string(pub), "workload", keyID)
	if err != nil {
		return err
	}
	if err := os.WriteFile(sshCert, []byte(signed), 0o600); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "verself-workload-bootstrap: cert %s\n  key_id=%s, valid for ~%dh\n",
		sshCert, keyID, login.Auth.LeaseDuration/3600)
	return nil
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		fmt.Fprintf(os.Stderr, "verself-workload-bootstrap: %s is required\n", k)
		os.Exit(1)
	}
	return v
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func fetchAndPin(url, pinPath string) ([]byte, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	gotSum := sha256.Sum256(body)
	gotHex := hex.EncodeToString(gotSum[:])
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o700); err != nil {
		return nil, err
	}
	pinFile := pinPath + ".sha256"
	if existing, err := os.ReadFile(pinFile); err == nil {
		if strings.TrimSpace(string(existing)) != gotHex {
			return nil, fmt.Errorf("trust-anchor mismatch for %s; pinned=%s got=%s", url,
				strings.TrimSpace(string(existing)), gotHex)
		}
	} else if os.IsNotExist(err) {
		if err := os.WriteFile(pinFile, []byte(gotHex+"\n"), 0o600); err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}
	if err := os.WriteFile(pinPath, body, 0o600); err != nil {
		return nil, err
	}
	return body, nil
}

func writeWGConfig(iface, priv, address, serverPub, endpoint, network string) error {
	conf := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s/24

[Peer]
PublicKey = %s
Endpoint = %s
AllowedIPs = %s
PersistentKeepalive = 25
`, priv, address, serverPub, endpoint, network)
	path := fmt.Sprintf("/etc/wireguard/%s.conf", iface)
	if err := os.MkdirAll("/etc/wireguard", 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(conf), 0o600)
}

func wgQuickUp(name string) error {
	bin, err := exec.LookPath("wg-quick")
	if err != nil {
		return fmt.Errorf("wg-quick missing — install wireguard-tools in the workload image")
	}
	_ = exec.Command(bin, "down", name).Run()
	cmd := exec.Command(bin, "up", name)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Compact baoClient mirroring the aspect-operator side. Kept duplicated
// rather than shared so verself-workload-bootstrap is a self-contained
// static binary with the smallest possible deploy footprint into the
// workload VM.
type baoClient struct {
	addr  string
	token string
	http  *http.Client
}

func newBaoClient(addr string, caBytes []byte, token string) (*baoClient, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caBytes) {
		return nil, fmt.Errorf("OpenBao CA bytes contain no PEM blocks")
	}
	return &baoClient{
		addr:  strings.TrimRight(addr, "/"),
		token: token,
		http: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				MinVersion: tls.VersionTLS13,
			}},
		},
	}, nil
}

func (b *baoClient) do(method, path string, in any, out any) (int, error) {
	var body io.Reader
	if in != nil {
		buf, err := json.Marshal(in)
		if err != nil {
			return 0, err
		}
		body = strings.NewReader(string(buf))
	}
	req, err := http.NewRequest(method, b.addr+path, body)
	if err != nil {
		return 0, err
	}
	if b.token != "" {
		req.Header.Set("X-Vault-Token", b.token)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := b.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode >= 400 {
		return resp.StatusCode, fmt.Errorf("OpenBao %s %s: HTTP %d: %s",
			method, path, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

func (b *baoClient) signSSHCert(mount, role, pubkey, principal, keyID string) (string, error) {
	in := map[string]any{
		"public_key":       pubkey,
		"valid_principals": principal,
		"key_id":           keyID,
		"cert_type":        "user",
	}
	var out struct {
		Data struct {
			SignedKey string `json:"signed_key"`
		} `json:"data"`
	}
	if _, err := b.do("POST", "/v1/"+mount+"/sign/"+role, in, &out); err != nil {
		return "", err
	}
	if out.Data.SignedKey == "" {
		return "", fmt.Errorf("empty signed_key from %s/sign/%s", mount, role)
	}
	return out.Data.SignedKey, nil
}
