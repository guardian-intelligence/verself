package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// cmdOnboard runs the interactive onboarding flow for a fresh operator
// device. It is idempotent — re-running on an already-onboarded device
// refreshes the cert and exits.
func cmdOnboard(args []string) error {
	fs := flagSet("onboard")
	device := fs.String("device", "", "Device name (lowercase kebab; matches the cert KeyID suffix). Inferred from ~/.config/verself/ssh/ when exactly one device is onboarded; required on first run.")
	wgAddress := fs.String("wg-address", "", "WireGuard address inside the wg-ops /24 (e.g. 10.66.66.5). Must be unused. Operator addresses occupy .2..99; .100..163 are reserved for the workload pool.")
	domain := fs.String("domain", "verself.sh", "Public domain that serves /.well-known/verself-*")
	hostAlias := fs.String("host-alias", "fm-dev-w0", "ssh_config alias to map to the wg-ops listener")
	refreshOIDC := fs.Bool("refresh-oidc", false, "Force a fresh OIDC login even if a Vault token is cached")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := operatorConfigDir()
	if err != nil {
		return err
	}
	if *device == "" {
		inferred, err := inferDeviceName(cfg)
		if errors.Is(err, errNoOnboardedDevice) {
			return errors.New("--device is required for first-time onboarding (no devices present under ~/.config/verself/ssh/)")
		}
		if err != nil {
			return err
		}
		*device = inferred
		fmt.Fprintf(os.Stderr, "auto-detected --device=%s from ~/.config/verself/ssh/\n", inferred)
	}
	if !validDeviceName(*device) {
		return fmt.Errorf("invalid --device=%q: must match ^[a-z][a-z0-9-]*$", *device)
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

	// `--wg-address` is mandatory for a fresh onboard, but we also
	// support the case where wg-ops is brought up out-of-band (the
	// controller this binary was first deployed on, for instance,
	// runs wg-quick@wg-ops as a system unit). In that case we
	// auto-adopt the address and skip the per-user `verself`
	// interface — adding a second interface bound to the same
	// address would just collide.
	existingWGAddress, externallyManagedWG := detectExistingWGOps()
	if *wgAddress == "" {
		if externallyManagedWG {
			*wgAddress = existingWGAddress
			fmt.Fprintf(os.Stderr, "wg-ops already up at %s (system-managed); skipping per-user interface\n", existingWGAddress)
		} else {
			return errors.New(
				"--wg-address is required for fresh onboarding. Pick an unused IPv4 in the wg-ops /24 " +
					"(operators: 10.66.66.2..99; workload-pool slots: 10.66.66.100..163). " +
					"Existing operator addresses live in src/cue-renderer/instances/prod/operators.cue.")
		}
	}
	if err := validateOperatorWGAddress(*wgAddress); err != nil {
		return err
	}

	// 1. Local keypairs. ssh-keygen + wg are operator-side dev tools
	//    laid down by `aspect dev install`. Skipped when wg-ops
	//    is externally managed (no per-user wg keypair to mint).
	if err := ensureSSHKeypair(sshKeyPath, *device); err != nil {
		return err
	}
	if !externallyManagedWG {
		if err := ensureWGKeypair(wgKeyPath, wgPubPath); err != nil {
			return err
		}
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

	fmt.Fprintf(os.Stderr, "ssh pubkey for %s: %s\n", *device, strings.TrimSpace(string(sshPub)))
	fmt.Fprintf(os.Stderr, "wg-ops endpoint: %s:%d (server pubkey %s)\n",
		anchors.Wireguard.EndpointHost, anchors.Wireguard.EndpointPort, anchors.Wireguard.ServerPubkey)

	// 3. Print the CUE diff for the trusted operator to PR. Skipped
	//    when adopting an existing wg-ops interface — its pubkey is
	//    already registered in CUE by definition (we'd be unable to
	//    handshake otherwise).
	if !externallyManagedWG {
		wgPub, err := os.ReadFile(wgPubPath)
		if err != nil {
			return err
		}
		emitCUEDiff(*device, strings.TrimSpace(string(wgPub)), *wgAddress)
	}

	// 4. Local wg-ops bring-up. Skipped when the system already runs
	//    a wg-ops interface (typically wg-quick@wg-ops); spinning up
	//    a parallel `verself` interface would either collide on the
	//    same address or accept traffic on an unregistered pubkey.
	if !externallyManagedWG {
		wgConfPath, err := writeWGConfig(wgKeyPath, *wgAddress, anchors)
		if err != nil {
			return err
		}
		if err := wgQuickUp(wgConfPath); err != nil {
			return err
		}
	}

	// 5. Generated SSH config drop-in. Maps the inventory's host alias
	//    onto the wg-ops listener so `aspect deploy` and `ssh fm-dev-w0`
	//    work without per-device hand-edits to ~/.ssh/config.
	if err := writeSSHConfigDropIn(*hostAlias, anchors.Wireguard.HostAddress, sshKeyPath, sshCertPath); err != nil {
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

// validateOperatorWGAddress refuses obvious mistakes: malformed IPv4,
// the wg-ops gateway (.1), the workload-pool range (.100..163), or
// anything outside the 10.66.66.0/24 mesh. Collision with an existing
// operator device is detected at PR-review time — the binary cannot
// enumerate operators.cue from a fresh device.
func validateOperatorWGAddress(addr string) error {
	parts := strings.Split(addr, ".")
	if len(parts) != 4 {
		return fmt.Errorf("invalid --wg-address=%q: must be IPv4 dotted-quad", addr)
	}
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 {
			return fmt.Errorf("invalid --wg-address=%q: octet %q is not 0..255", addr, p)
		}
	}
	if !strings.HasPrefix(addr, "10.66.66.") {
		return fmt.Errorf("invalid --wg-address=%q: must lie in the wg-ops mesh 10.66.66.0/24", addr)
	}
	last, _ := strconv.Atoi(parts[3])
	switch {
	case last < 2:
		return fmt.Errorf("invalid --wg-address=%q: .%d is reserved (.0 network, .1 wg-ops gateway)", addr, last)
	case last >= 100 && last <= 163:
		return fmt.Errorf("invalid --wg-address=%q: .%d falls in the workload-pool range (.100..163). Operator devices use .2..99", addr, last)
	case last > 254:
		return fmt.Errorf("invalid --wg-address=%q: .%d is the broadcast address", addr, last)
	}
	return nil
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

func writeWGConfig(privPath, address string, anchors fetchedAnchors) (string, error) {
	confDir := filepath.Dir(privPath)
	confPath := filepath.Join(confDir, "verself.conf")
	priv, err := os.ReadFile(privPath)
	if err != nil {
		return "", err
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
	if err := os.WriteFile(confPath, []byte(conf), 0o600); err != nil {
		return "", err
	}
	return confPath, nil
}

// wgQuickUp brings up the wg interface described by the config at
// confPath. wg-quick derives the interface name from the file's
// basename (e.g. ~/.config/verself/wg/verself.conf → interface
// "verself"). The macOS invocation is identical to Linux's; the
// operator is responsible for installing wireguard-tools (Linux: apt;
// macOS: brew install wireguard-tools).
func wgQuickUp(confPath string) error {
	bin, err := exec.LookPath("wg-quick")
	if err != nil {
		return fmt.Errorf("wg-quick not found in PATH; install wireguard-tools (Linux: apt; macOS: brew install wireguard-tools)")
	}
	args := []string{bin}
	if needsSudo() {
		args = append([]string{"sudo"}, args...)
	}
	// Bring the tunnel down before bringing it up: wg-quick refuses to
	// touch an interface it didn't create, and a stale config file
	// from an aborted prior run will land us there.
	_ = exec.Command(args[0], append(args[1:], "down", confPath)...).Run()
	cmd := exec.Command(args[0], append(args[1:], "up", confPath)...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func needsSudo() bool { return os.Geteuid() != 0 }

func writeSSHConfigDropIn(alias, hostAddress, keyPath, certPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	cfgDir := filepath.Join(home, ".ssh", "config.d")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		return err
	}
	// Host-key trust is TOFU on first contact (StrictHostKeyChecking
	// accept-new) and strict thereafter — matches OpenSSH defaults
	// rather than overriding UserKnownHostsFile, which prior versions
	// of this drop-in incorrectly pointed at the user-cert CA pubkey
	// (a different file shape that SSH refuses to parse as known_hosts).
	// Publishing the host's SSH host key under /.well-known/ so this
	// becomes pinned-on-first-fetch is a documented future hardening.
	dropIn := fmt.Sprintf(`# Managed by aspect-operator; safe to overwrite. Source of truth:
# src/cue-renderer/instances/prod/{config,operators}.cue.
Host %s %s
    HostName %s
    User ubuntu
    IdentityFile %s
    CertificateFile %s
    IdentitiesOnly yes
    StrictHostKeyChecking accept-new
    ControlMaster auto
    ControlPersist 1h
`, alias, hostAddress, hostAddress, keyPath, certPath)
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
		// skip_browser=true makes bao print the auth URL and wait
		// without trying to launch xdg-open / open / start. The
		// localhost:8250 callback still has to be reachable from
		// whichever browser the operator uses; on a headless
		// controller this means an SSH local-forward from the laptop
		// SSH session: `ssh -L 8250:localhost:8250 ubuntu@<host>`.
		//
		// callbackmode=device would eliminate the localhost callback
		// entirely — Zitadel advertises the device endpoint and
		// grant_types_supported lists urn:ietf:params:oauth:grant-
		// type:device_code, and the verself-ssh-ca app carries
		// OIDC_GRANT_TYPE_DEVICE_CODE. But OpenBao 2.5.2's OIDC
		// plugin returns "no state returned in device callback mode"
		// when the polling response lands; tracked for re-enable
		// once OpenBao ships a fix. The Zitadel grant stays in place
		// so the flip becomes a one-line change here.
		"skip_browser=true",
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

// detectExistingWGOps returns the operator's wg-ops IPv4 when the
// system already has a wg-ops interface up — typically wg-quick@wg-ops
// configured outside this binary's scope. The binary skips its own
// per-user `verself` tunnel in that case; adding a second interface
// bound to the same address would only collide. Uses `ip` rather
// than `wg show` because the latter requires CAP_NET_ADMIN.
func detectExistingWGOps() (string, bool) {
	out, err := exec.Command("ip", "-4", "-o", "addr", "show", "wg-ops").Output()
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		for i, f := range fields {
			if f == "inet" && i+1 < len(fields) {
				return strings.SplitN(fields[i+1], "/", 2)[0], true
			}
		}
	}
	return "", false
}
