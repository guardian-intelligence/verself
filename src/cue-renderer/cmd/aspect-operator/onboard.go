package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// cmdOnboard runs the interactive onboarding flow for a fresh operator
// device. It is idempotent — re-running on an already-onboarded device
// refreshes the cert and exits.
func cmdOnboard(args []string) error {
	fs := flagSet("onboard")
	device := fs.String("device", "", "Device name (lowercase kebab; matches the cert KeyID suffix)")
	domain := fs.String("domain", "verself.sh", "Public domain that serves /.well-known/verself-*")
	hostAlias := fs.String("host-alias", "fm-dev-w0", "ssh_config alias to map to the wg-ops listener")
	refreshOIDC := fs.Bool("refresh-oidc", false, "Force a fresh OIDC login even if a Vault token is cached")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *device == "" {
		return errors.New("--device is required")
	}
	if !validDeviceName(*device) {
		return fmt.Errorf("invalid --device=%q: must match ^[a-z][a-z0-9-]*$", *device)
	}

	cfg, err := operatorConfigDir()
	if err != nil {
		return err
	}
	sshDir := filepath.Join(cfg, "ssh")
	wgDir := filepath.Join(cfg, "wg")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(wgDir, 0o700); err != nil {
		return err
	}
	sshKeyPath := filepath.Join(sshDir, *device)
	sshPubPath := sshKeyPath + ".pub"
	sshCertPath := sshKeyPath + "-cert.pub"
	wgKeyPath := filepath.Join(wgDir, *device)
	wgPubPath := wgKeyPath + ".pub"

	// 1. Local keypairs. ssh-keygen + wg are operator-side dev tools
	//    laid down by `aspect platform setup-dev`.
	if err := ensureSSHKeypair(sshKeyPath, *device); err != nil {
		return err
	}
	if err := ensureWGKeypair(wgKeyPath, wgPubPath); err != nil {
		return err
	}
	wgPub, err := os.ReadFile(wgPubPath)
	if err != nil {
		return err
	}
	sshPub, err := os.ReadFile(sshPubPath)
	if err != nil {
		return err
	}

	// 2. Trust anchors. Pinned on first contact under
	//    ~/.config/verself/trust-anchors/. A drift error here aborts
	//    the flow with a clear recovery hint.
	anchors, err := fetchTrustAnchors(*domain, cfg)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "wg pubkey for %s: %s\n", *device, strings.TrimSpace(string(wgPub)))
	fmt.Fprintf(os.Stderr, "ssh pubkey for %s: %s\n", *device, strings.TrimSpace(string(sshPub)))
	fmt.Fprintf(os.Stderr, "wg-ops endpoint: %s:%d (server pubkey %s)\n",
		anchors.Wireguard.EndpointHost, anchors.Wireguard.EndpointPort, anchors.Wireguard.ServerPubkey)

	// 3. Print the CUE diff for the trusted operator to PR. Don't
	//    attempt to authenticate to Forgejo from a clean device — the
	//    PR-author surface lives on an already-onboarded device.
	wgAddress, err := chooseWGAddress(*device, anchors)
	if err != nil {
		return err
	}
	emitCUEDiff(*device, strings.TrimSpace(string(wgPub)), wgAddress)

	// 4. Local wg-ops bring-up. The wg-quick service is best-effort
	//    here: if the device's pubkey isn't yet in CUE, the handshake
	//    won't complete and we error out on step 6 (OIDC over wg-ops).
	if err := writeWGConfig(wgKeyPath, wgAddress, anchors); err != nil {
		return err
	}
	if err := wgQuickUp("verself"); err != nil {
		return err
	}

	// 5. Generated SSH config drop-in. Maps the inventory's host alias
	//    onto the wg-ops listener so `aspect deploy` and `ssh fm-dev-w0`
	//    work without per-device hand-edits to ~/.ssh/config.
	if err := writeSSHConfigDropIn(*hostAlias, anchors.Wireguard.HostAddress, sshKeyPath, sshCertPath, anchors.SSHCAPath); err != nil {
		return err
	}

	// 6. OIDC. The login persists a periodic Vault token at
	//    ~/.vault-token; subsequent refreshes renew it in place.
	tokenPath, err := ensureVaultLogin(*domain, anchors, *refreshOIDC)
	if err != nil {
		return err
	}

	// 7. Sign the first cert. Mount + role are CUE-side constants
	//    (config.ssh_ca.mount=ssh-ca, principal=operator).
	tokenBytes, err := os.ReadFile(tokenPath)
	if err != nil {
		return fmt.Errorf("read vault token at %s: %w", tokenPath, err)
	}
	bao, err := newBaoClient(
		fmt.Sprintf("https://%s:8200", anchors.Wireguard.HostAddress),
		anchors.OpenBaoCAPath,
		strings.TrimSpace(string(tokenBytes)),
	)
	if err != nil {
		return err
	}
	keyID := fmt.Sprintf("verself-operator-%s", *device)
	signed, err := bao.signSSHCert("ssh-ca", "operator", string(sshPub), "operator", keyID)
	if err != nil {
		return err
	}
	if err := os.WriteFile(sshCertPath, []byte(signed), 0o600); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "\nonboard complete. Cert written to %s.\n", sshCertPath)
	fmt.Fprintf(os.Stderr, "Smoke: ssh %s 'true'\n", *hostAlias)
	return nil
}

func validDeviceName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case i > 0 && r >= '0' && r <= '9':
		case i > 0 && r == '-':
		default:
			return false
		}
	}
	return true
}

// operatorConfigDir returns the directory under which all per-device
// state lives — keys, certs, trust anchors, and the bao-token symlink.
func operatorConfigDir() (string, error) {
	if v := os.Getenv("VERSELF_CONFIG_HOME"); v != "" {
		return v, nil
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "verself"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "verself"), nil
}

func ensureSSHKeypair(privPath, comment string) error {
	if _, err := os.Stat(privPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-f", privPath, "-N", "",
		"-C", "verself-operator-"+comment+"@"+hostnameOrUnknown())
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh-keygen: %w", err)
	}
	return nil
}

func ensureWGKeypair(privPath, pubPath string) error {
	if _, err := os.Stat(privPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	priv, err := exec.Command("wg", "genkey").Output()
	if err != nil {
		return fmt.Errorf("wg genkey: %w", err)
	}
	priv = []byte(strings.TrimSpace(string(priv)) + "\n")
	if err := os.WriteFile(privPath, priv, 0o600); err != nil {
		return err
	}
	pubCmd := exec.Command("wg", "pubkey")
	pubCmd.Stdin = strings.NewReader(string(priv))
	pub, err := pubCmd.Output()
	if err != nil {
		return fmt.Errorf("wg pubkey: %w", err)
	}
	return os.WriteFile(pubPath, []byte(strings.TrimSpace(string(pub))+"\n"), 0o600)
}

func hostnameOrUnknown() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "unknown"
	}
	return h
}

// chooseWGAddress derives a candidate wg-ops address for a fresh
// device by scanning the host_address octet upward from .2 until it
// finds one not in the published peer list. Operator-allocated
// addresses live in the lower half of the wg-ops /24; workload-pool
// slots live in the .100..107 range so the two ranges don't collide.
func chooseWGAddress(device string, anchors fetchedAnchors) (string, error) {
	host := anchors.Wireguard.HostAddress
	parts := strings.Split(host, ".")
	if len(parts) != 4 {
		return "", fmt.Errorf("unexpected host_address shape %q", host)
	}
	// Operator addresses occupy 10.66.66.2 .. 10.66.66.99. The PR
	// reviewer is the final authority on which one a new device gets;
	// this just suggests one that doesn't collide with operator-pool
	// slot space.
	return fmt.Sprintf("%s.%s.%s.%d", parts[0], parts[1], parts[2], 2), nil
}

// emitCUEDiff prints the operator-device CUE entry the PR reviewer
// needs to add. Stdout so it is easy to pipe into `pbcopy` /
// `xclip -selection clipboard`.
func emitCUEDiff(device, wgPubkey, wgAddress string) {
	fmt.Println("# Add the following entry under config.operators.<operator>.devices in")
	fmt.Println("# src/cue-renderer/instances/prod/operators.cue, then open a PR.")
	fmt.Printf("\"%s\": {\n", device)
	fmt.Printf("\tname:       %q\n", device)
	fmt.Printf("\twg_pubkey:  %q\n", wgPubkey)
	fmt.Printf("\twg_address: %q\n", wgAddress)
	fmt.Println("}")
	fmt.Println()
}

func writeWGConfig(privPath, address string, anchors fetchedAnchors) error {
	confDir := filepath.Dir(privPath)
	confPath := filepath.Join(confDir, "verself.conf")
	priv, err := os.ReadFile(privPath)
	if err != nil {
		return err
	}
	conf := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s/24

[Peer]
PublicKey = %s
Endpoint = %s:%d
AllowedIPs = %s
PersistentKeepalive = 25
`,
		strings.TrimSpace(string(priv)),
		address,
		anchors.Wireguard.ServerPubkey,
		anchors.Wireguard.EndpointHost,
		anchors.Wireguard.EndpointPort,
		anchors.Wireguard.Network,
	)
	return os.WriteFile(confPath, []byte(conf), 0o600)
}

// wgQuickUp brings the named wg interface up via wg-quick. The macOS
// invocation is identical to Linux's; the operator is responsible for
// installing wireguard-tools (Linux: apt; macOS: brew install
// wireguard-tools).
func wgQuickUp(name string) error {
	bin, err := exec.LookPath("wg-quick")
	if err != nil {
		return fmt.Errorf("wg-quick not found in PATH; install wireguard-tools (Linux: apt; macOS: brew install wireguard-tools)")
	}
	// Bring the tunnel down before bringing it up: wg-quick refuses to
	// touch an interface it didn't create, and a stale config file
	// from an aborted prior run will land us there.
	if needsSudo() {
		_ = exec.Command("sudo", bin, "down", name).Run()
		cmd := exec.Command("sudo", bin, "up", name)
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	_ = exec.Command(bin, "down", name).Run()
	cmd := exec.Command(bin, "up", name)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func needsSudo() bool { return os.Geteuid() != 0 }

func writeSSHConfigDropIn(alias, hostAddress, keyPath, certPath, caPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	cfgDir := filepath.Join(home, ".ssh", "config.d")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		return err
	}
	dropIn := fmt.Sprintf(`# Managed by aspect-operator; safe to overwrite. Source of truth:
# src/cue-renderer/instances/prod/{config,operators}.cue.
Host %s
    HostName %s
    User ubuntu
    IdentityFile %s
    CertificateFile %s
    IdentitiesOnly yes
    UserKnownHostsFile %s
    StrictHostKeyChecking yes
    ControlMaster auto
    ControlPersist 1h
`, alias, hostAddress, keyPath, certPath, caPath)
	if err := os.WriteFile(filepath.Join(cfgDir, "verself.conf"), []byte(dropIn), 0o600); err != nil {
		return err
	}
	return ensureSSHIncludesDropIn(home)
}

func ensureSSHIncludesDropIn(home string) error {
	cfgPath := filepath.Join(home, ".ssh", "config")
	want := "Include config.d/*\n"
	existing, err := os.ReadFile(cfgPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if strings.Contains(string(existing), "Include config.d/*") {
		return nil
	}
	combined := want + string(existing)
	return os.WriteFile(cfgPath, []byte(combined), 0o600)
}

// ensureVaultLogin runs `bao login -method=oidc` when no usable cached
// token exists (or `--refresh-oidc` is set). Returns the path to the
// token file.
func ensureVaultLogin(domain string, anchors fetchedAnchors, force bool) (string, error) {
	tokenPath := vaultTokenPath()
	if !force {
		if buf, err := os.ReadFile(tokenPath); err == nil && strings.TrimSpace(string(buf)) != "" {
			bao, err := newBaoClient(
				fmt.Sprintf("https://%s:8200", anchors.Wireguard.HostAddress),
				anchors.OpenBaoCAPath,
				strings.TrimSpace(string(buf)),
			)
			if err == nil {
				if _, err := bao.lookupSelf(); err == nil {
					return tokenPath, nil
				}
			}
		}
	}
	bao := exec.Command("bao", "login",
		"-method=oidc",
		"-path=oidc-ssh-ca",
		"-no-print",
		"role=operator",
	)
	bao.Env = append(os.Environ(),
		fmt.Sprintf("BAO_ADDR=https://%s:8200", anchors.Wireguard.HostAddress),
		fmt.Sprintf("BAO_CACERT=%s", anchors.OpenBaoCAPath),
		fmt.Sprintf("VAULT_ADDR=https://%s:8200", anchors.Wireguard.HostAddress),
		fmt.Sprintf("VAULT_CACERT=%s", anchors.OpenBaoCAPath),
	)
	bao.Stdout = os.Stderr
	bao.Stderr = os.Stderr
	bao.Stdin = os.Stdin
	if err := bao.Run(); err != nil {
		return "", fmt.Errorf("bao login -method=oidc: %w", err)
	}
	return tokenPath, nil
}

func vaultTokenPath() string {
	if v := os.Getenv("BAO_TOKEN_FILE"); v != "" {
		return v
	}
	if v := os.Getenv("VAULT_TOKEN_FILE"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".vault-token")
}
