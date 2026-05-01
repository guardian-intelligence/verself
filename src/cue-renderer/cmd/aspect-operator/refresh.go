package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// cmdRefresh renews the cached operator Vault token (periodic, 14d
// renewal cadence) and re-signs the SSH cert. Invoked by `aspect deploy`
// pre-flight; no interactivity. Fails loudly with a clear recovery
// message when the token has exceeded explicit_max_ttl and OIDC
// re-auth is mandatory.
func cmdRefresh(args []string) error {
	fs := flagSet("refresh")
	device := fs.String("device", "", "Operator device whose cert to re-sign (defaults to the most-recently-onboarded device on this machine)")
	domain := fs.String("domain", "verself.sh", "Public domain that serves /.well-known/verself-*")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := operatorConfigDir()
	if err != nil {
		return err
	}

	dev := *device
	if dev == "" {
		dev, err = inferDeviceName(cfg)
		if err != nil {
			return err
		}
	}

	sshKeyPath := filepath.Join(cfg, "ssh", dev)
	sshPubPath := sshKeyPath + ".pub"
	sshCertPath := sshKeyPath + "-cert.pub"
	if _, err := os.Stat(sshPubPath); err != nil {
		return fmt.Errorf("device %q is not onboarded on this machine: %w (run `aspect operator onboard --device=%s`)", dev, err, dev)
	}

	anchors, err := fetchTrustAnchors(*domain, cfg)
	if err != nil {
		return err
	}

	tokenPath := vaultTokenPath()
	tokenBytes, err := os.ReadFile(tokenPath)
	if err != nil || strings.TrimSpace(string(tokenBytes)) == "" {
		return fmt.Errorf(
			"no cached Vault token at %s — run `aspect operator onboard --device=%s --refresh-oidc` to OIDC-re-auth",
			tokenPath, dev)
	}

	bao, err := newBaoClient(
		fmt.Sprintf("https://%s:8200", anchors.Wireguard.HostAddress),
		anchors.OpenBaoCAPath,
		strings.TrimSpace(string(tokenBytes)),
	)
	if err != nil {
		return err
	}

	// Renew first, then sign with the renewed token. renewSelf returns
	// the actionable error message itself when the token has exceeded
	// explicit_max_ttl, so we just propagate.
	if _, err := bao.renewSelf(); err != nil {
		return err
	}

	pub, err := os.ReadFile(sshPubPath)
	if err != nil {
		return err
	}
	keyID := fmt.Sprintf("verself-operator-%s", dev)
	signed, err := bao.signSSHCert("ssh-ca", "operator", string(pub), "operator", keyID)
	if err != nil {
		return err
	}
	if err := os.WriteFile(sshCertPath, []byte(signed), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "aspect-operator: refreshed token + cert for device %s\n", dev)
	return nil
}

// inferDeviceName picks a device name when --device is omitted: it
// returns the only entry under ~/.config/verself/ssh/ (excluding -pub
// / -cert files) when there is exactly one, else an error pointing at
// --device. This matches the common single-device case (one laptop)
// without surprising operators with auto-selection on multi-device
// machines.
func inferDeviceName(cfg string) (string, error) {
	entries, err := os.ReadDir(filepath.Join(cfg, "ssh"))
	if err != nil {
		return "", err
	}
	var devices []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".pub") || strings.HasSuffix(name, "-cert.pub") {
			continue
		}
		devices = append(devices, name)
	}
	if len(devices) == 0 {
		return "", errors.New("no onboarded devices found under ~/.config/verself/ssh/; run `aspect operator onboard --device=<name>` first")
	}
	if len(devices) > 1 {
		return "", fmt.Errorf("multiple onboarded devices on this machine (%s); pass --device=<name>", strings.Join(devices, ", "))
	}
	return devices[0], nil
}
