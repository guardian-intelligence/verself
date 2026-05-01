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
//
// Pre-onboarding state — no operator-config dir, no Vault token — is
// not an error: the binary exits 0 with a notice so `aspect deploy`
// pre-flight on a fresh controller (or in CI) doesn't trip on a
// missing prerequisite the deploy itself is about to provide.
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
	sshDir := filepath.Join(cfg, "ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return err
	}

	dev := *device
	if dev == "" {
		dev, err = inferDeviceName(cfg)
		if errors.Is(err, errNoOnboardedDevice) {
			fmt.Fprintf(os.Stderr, "aspect-operator: no devices onboarded on this machine; skipping refresh\n")
			return nil
		}
		if err != nil {
			return err
		}
	}

	sshKeyPath := filepath.Join(sshDir, dev)
	sshPubPath := sshKeyPath + ".pub"
	sshCertPath := sshKeyPath + "-cert.pub"

	if _, err := os.Stat(sshPubPath); err != nil {
		fmt.Fprintf(os.Stderr,
			"aspect-operator: device %q is not onboarded on this machine (%s missing); skipping refresh. Run `aspect operator onboard --device=%s` to onboard.\n",
			dev, sshPubPath, dev)
		return nil
	}

	tokenPath := vaultTokenPath()
	tokenBytes, err := os.ReadFile(tokenPath)
	if err != nil || strings.TrimSpace(string(tokenBytes)) == "" {
		fmt.Fprintf(os.Stderr,
			"aspect-operator: no cached Vault token at %s; skipping refresh (run `aspect operator onboard --device=%s --refresh-oidc` to OIDC-re-auth)\n",
			tokenPath, dev)
		return nil
	}

	anchors, err := fetchTrustAnchors(*domain, cfg)
	if err != nil {
		return err
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

// errNoOnboardedDevice is returned by inferDeviceName when the
// operator-config dir contains no priv keys. Callers (refresh) treat
// this as a soft no-op so a fresh controller's deploy pre-flight isn't
// tripped on a chicken-and-egg.
var errNoOnboardedDevice = errors.New("no onboarded devices on this machine")

// inferDeviceName picks a device name when --device is omitted: it
// returns the only entry under ~/.config/verself/ssh/ (excluding -pub
// / -cert files) when there is exactly one, else an error pointing at
// --device. This matches the common single-device case (one laptop)
// without surprising operators with auto-selection on multi-device
// machines.
func inferDeviceName(cfg string) (string, error) {
	entries, err := os.ReadDir(filepath.Join(cfg, "ssh"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", errNoOnboardedDevice
		}
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
		return "", errNoOnboardedDevice
	}
	if len(devices) > 1 {
		return "", fmt.Errorf("multiple onboarded devices on this machine (%s); pass --device=<name>", strings.Join(devices, ", "))
	}
	return devices[0], nil
}
