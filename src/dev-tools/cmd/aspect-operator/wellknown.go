package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// trustAnchor identifies one /.well-known/verself-* artifact and the
// local cache path that pins its sha256 on first contact (TOFU).
type trustAnchor struct {
	URL       string
	CachePath string // absolute path under ~/.config/verself/trust-anchors/
}

// wireguardWellKnown is the shape HAProxy serves at
// /.well-known/verself-wireguard.json. Field names match what the
// wireguard role's tunnel.yml writes; if the two ever drift, fetch
// fails with a JSON decode error.
type wireguardWellKnown struct {
	EndpointHost string `json:"endpoint_host"`
	EndpointPort int    `json:"endpoint_port"`
	ServerPubkey string `json:"server_pubkey"`
	Network      string `json:"network"`
	HostAddress  string `json:"host_address"`
}

// fetchAndPin downloads the artifact at the given URL, returns its
// bytes, and verifies (or pins on first contact) the sha256 against the
// on-disk cache. Returns the raw bytes on success.
func fetchAndPin(client *http.Client, anchor trustAnchor) ([]byte, error) {
	req, err := http.NewRequest("GET", anchor.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "aspect-operator/1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", anchor.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d", anchor.URL, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", anchor.URL, err)
	}

	gotSum := sha256.Sum256(body)
	gotHex := hex.EncodeToString(gotSum[:])

	if err := os.MkdirAll(filepath.Dir(anchor.CachePath), 0o700); err != nil {
		return nil, err
	}
	pinPath := anchor.CachePath + ".sha256"
	if existing, err := os.ReadFile(pinPath); err == nil {
		want := strings.TrimSpace(string(existing))
		if want != gotHex {
			return nil, fmt.Errorf(
				"trust-anchor mismatch for %s\n"+
					"  pinned sha256: %s\n"+
					"  fetched sha256: %s\n"+
					"  pinned at:      %s\n"+
					"To accept the new value (only after out-of-band verification),\n"+
					"delete %s and re-run",
				anchor.URL, want, gotHex, pinPath, pinPath)
		}
	} else if os.IsNotExist(err) {
		// First-contact TOFU. Write the pin alongside the artifact.
		if err := os.WriteFile(pinPath, []byte(gotHex+"\n"), 0o600); err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}
	if err := os.WriteFile(anchor.CachePath, body, 0o600); err != nil {
		return nil, err
	}
	return body, nil
}

// fetchTrustAnchors downloads ssh-ca.pub, openbao-ca.pem, and
// wireguard.json under the apex /.well-known/ path and verifies them
// against pinned hashes. The ssh-ca.pub download is a TOFU integrity
// check (drift would surface as a pin mismatch on the next call); the
// signed cert is verified by sshd against /etc/ssh/verself-ssh-ca.pub
// on the host, so the operator binary does not need the path itself.
type fetchedAnchors struct {
	OpenBaoCAPath string
	Wireguard     wireguardWellKnown
	WireguardRaw  []byte
}

func fetchTrustAnchors(domain, configDir string) (fetchedAnchors, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	anchorsDir := filepath.Join(configDir, "trust-anchors")

	defs := []trustAnchor{
		{URL: fmt.Sprintf("https://%s/.well-known/verself-ssh-ca.pub", domain),
			CachePath: filepath.Join(anchorsDir, "verself-ssh-ca.pub")},
		{URL: fmt.Sprintf("https://%s/.well-known/verself-openbao-ca.pem", domain),
			CachePath: filepath.Join(anchorsDir, "verself-openbao-ca.pem")},
		{URL: fmt.Sprintf("https://%s/.well-known/verself-wireguard.json", domain),
			CachePath: filepath.Join(anchorsDir, "verself-wireguard.json")},
	}

	bodies := make([][]byte, len(defs))
	for i, anchor := range defs {
		body, err := fetchAndPin(client, anchor)
		if err != nil {
			return fetchedAnchors{}, err
		}
		bodies[i] = body
	}

	var wg wireguardWellKnown
	if err := json.Unmarshal(bodies[2], &wg); err != nil {
		return fetchedAnchors{}, fmt.Errorf("parse wireguard.json: %w", err)
	}

	return fetchedAnchors{
		OpenBaoCAPath: defs[1].CachePath,
		Wireguard:     wg,
		WireguardRaw:  bodies[2],
	}, nil
}
