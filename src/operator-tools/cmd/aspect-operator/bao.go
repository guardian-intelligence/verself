package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// baoClient is a thin HTTP wrapper around the OpenBao API. The TLS
// config trusts only the pinned ca.pem fetched from /.well-known/ —
// the system trust store is deliberately not consulted, so a rogue
// certificate signed by any system CA can't intercept token issuance.
type baoClient struct {
	addr  string // https://<wg-ops>:8200
	token string // X-Vault-Token; empty if unauthenticated
	http  *http.Client
}

func newBaoClient(addr, caPath, token string) (*baoClient, error) {
	caBytes, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("read OpenBao CA %s: %w", caPath, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caBytes) {
		return nil, fmt.Errorf("parse OpenBao CA %s: no PEM blocks", caPath)
	}
	return &baoClient{
		addr:  strings.TrimRight(addr, "/"),
		token: token,
		http: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					RootCAs:    pool,
					MinVersion: tls.VersionTLS13,
				},
			},
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
		body = bytes.NewReader(buf)
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
	defer resp.Body.Close()
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
			return resp.StatusCode, fmt.Errorf("decode %s %s response: %w",
				method, path, err)
		}
	}
	return resp.StatusCode, nil
}

// signSSHCert calls /v1/<mount>/sign/<role> with the operator's
// public key bytes and a stamped key_id. Returns the cert bytes
// (OpenSSH single-line format).
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
		return "", fmt.Errorf("OpenBao sign %s/%s: empty signed_key in response", mount, role)
	}
	return out.Data.SignedKey, nil
}

// renewSelf calls /v1/auth/token/renew-self. Returns the new lease TTL
// in seconds and an error pointing at the recovery action when the
// token has exceeded its explicit_max_ttl. The renew endpoint returns
// 403 when the token is unrecoverable.
func (b *baoClient) renewSelf() (int, error) {
	var out struct {
		Auth struct {
			LeaseDuration int `json:"lease_duration"`
		} `json:"auth"`
	}
	status, err := b.do("POST", "/v1/auth/token/renew-self", map[string]any{}, &out)
	if err != nil {
		if status == http.StatusForbidden {
			return 0, fmt.Errorf(
				"vault token can no longer renew (HTTP 403). The token has exceeded its explicit_max_ttl (~30d). " +
					"Run `aspect operator onboard --device=<your-device> --refresh-oidc` to OIDC-re-auth.")
		}
		return 0, err
	}
	return out.Auth.LeaseDuration, nil
}

// lookupSelf returns the cached token's TTL information for callers
// that want to decide whether a renewSelf is worth attempting before
// the next sign call.
func (b *baoClient) lookupSelf() (struct {
	TTL         int    `json:"ttl"`
	ExplicitMax int    `json:"explicit_max_ttl"`
	Period      int    `json:"period"`
	DisplayName string `json:"display_name"`
}, error) {
	var out struct {
		Data struct {
			TTL         int    `json:"ttl"`
			ExplicitMax int    `json:"explicit_max_ttl"`
			Period      int    `json:"period"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if _, err := b.do("GET", "/v1/auth/token/lookup-self", nil, &out); err != nil {
		return struct {
			TTL         int    `json:"ttl"`
			ExplicitMax int    `json:"explicit_max_ttl"`
			Period      int    `json:"period"`
			DisplayName string `json:"display_name"`
		}{}, err
	}
	return out.Data, nil
}
