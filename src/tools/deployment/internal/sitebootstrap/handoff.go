package sitebootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type RootHandoffOptions struct {
	Site               string
	Host               string
	Port               int
	RootPasswordFile   string
	PublicKeyFile      string
	PrivateKeyFile     string
	BootstrapUser      string
	HostAlias          string
	InventoryPath      string
	HostKeySHA256      string
	TrustFirstUse      bool
	Timeout            time.Duration
	WriteInventoryFile bool
	ForceInventory     bool
}

func RunRootHandoff(ctx context.Context, opts RootHandoffOptions) error {
	opts = normalizeRootHandoffOptions(opts)
	if opts.Host == "" {
		return errors.New("host is required")
	}
	if opts.RootPasswordFile == "" {
		return errors.New("root password file is required")
	}
	if opts.PublicKeyFile == "" {
		return errors.New("public key file is required")
	}
	if opts.PrivateKeyFile == "" {
		return errors.New("private key file is required")
	}
	if !isSystemName(opts.BootstrapUser) {
		return fmt.Errorf("bootstrap user %q must be a simple system account name", opts.BootstrapUser)
	}
	if opts.HostKeySHA256 == "" && !opts.TrustFirstUse {
		return errors.New("host key SHA256 fingerprint is required unless --trust-first-use is passed")
	}
	password, err := readTrimmed(opts.RootPasswordFile)
	if err != nil {
		return err
	}
	if password == "" {
		return fmt.Errorf("%s is empty", opts.RootPasswordFile)
	}
	authorizedKey, err := readAuthorizedPublicKey(opts.PublicKeyFile)
	if err != nil {
		return err
	}
	signer, err := readPrivateKeySigner(opts.PrivateKeyFile)
	if err != nil {
		return err
	}
	addr := net.JoinHostPort(opts.Host, fmt.Sprintf("%d", opts.Port))
	rootClient, err := dialSSH(ctx, addr, "root", []ssh.AuthMethod{ssh.Password(password)}, opts)
	if err != nil {
		return fmt.Errorf("connect root@%s: %w", addr, err)
	}
	defer func() { _ = rootClient.Close() }()

	for _, command := range rootHandoffCommands(opts.BootstrapUser, authorizedKey) {
		if err := runSSHCommand(rootClient, command); err != nil {
			return err
		}
	}

	userClient, err := dialSSH(ctx, addr, opts.BootstrapUser, []ssh.AuthMethod{ssh.PublicKeys(signer)}, opts)
	if err != nil {
		return fmt.Errorf("verify %s key login before disabling passwords: %w", opts.BootstrapUser, err)
	}
	if err := runSSHCommand(userClient, "sudo -n true"); err != nil {
		_ = userClient.Close()
		return fmt.Errorf("verify passwordless sudo for %s: %w", opts.BootstrapUser, err)
	}
	_ = userClient.Close()

	for _, command := range rootPasswordDisableCommands() {
		if err := runSSHCommand(rootClient, command); err != nil {
			return err
		}
	}
	if opts.WriteInventoryFile {
		if err := WriteInventory(InventoryOptions{
			Site:         opts.Site,
			Host:         opts.Host,
			HostAlias:    opts.HostAlias,
			User:         opts.BootstrapUser,
			OutputPath:   opts.InventoryPath,
			RecoveryHost: opts.Host,
			RecoveryPort: opts.Port,
			ForceWrite:   opts.ForceInventory,
		}); err != nil {
			return err
		}
	}
	return nil
}

func normalizeRootHandoffOptions(opts RootHandoffOptions) RootHandoffOptions {
	opts.Site = strings.TrimSpace(opts.Site)
	opts.Host = strings.TrimSpace(opts.Host)
	opts.RootPasswordFile = strings.TrimSpace(opts.RootPasswordFile)
	opts.PublicKeyFile = strings.TrimSpace(opts.PublicKeyFile)
	opts.PrivateKeyFile = strings.TrimSpace(opts.PrivateKeyFile)
	opts.BootstrapUser = strings.TrimSpace(opts.BootstrapUser)
	opts.HostAlias = strings.TrimSpace(opts.HostAlias)
	opts.InventoryPath = strings.TrimSpace(opts.InventoryPath)
	opts.HostKeySHA256 = normalizeSHA256Fingerprint(opts.HostKeySHA256)
	if opts.Port == 0 {
		opts.Port = 22
	}
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Second
	}
	if opts.BootstrapUser == "" {
		opts.BootstrapUser = "ubuntu"
	}
	if opts.HostAlias == "" && opts.Site != "" {
		opts.HostAlias = "vs-" + opts.Site + "-w0"
	}
	return opts
}

func rootHandoffCommands(user, authorizedKey string) []string {
	key := shellQuote(authorizedKey)
	quotedUser := shellQuote(user)
	home := "/home/" + user
	userGroup := shellQuote(user + ":" + user)
	return []string{
		"install -d -m 0700 -o root -g root /root/.ssh",
		"touch /root/.ssh/authorized_keys",
		"chmod 0600 /root/.ssh/authorized_keys",
		"grep -qxF -- " + key + " /root/.ssh/authorized_keys || printf '%s\\n' " + key + " >> /root/.ssh/authorized_keys",
		"id -u " + quotedUser + " >/dev/null 2>&1 || useradd --create-home --shell /bin/bash --groups sudo " + quotedUser,
		"usermod -aG sudo " + quotedUser,
		"install -d -m 0700 -o " + quotedUser + " -g " + quotedUser + " " + shellQuote(home+"/.ssh"),
		"touch " + shellQuote(home+"/.ssh/authorized_keys"),
		"chown " + userGroup + " " + shellQuote(home+"/.ssh/authorized_keys"),
		"chmod 0600 " + shellQuote(home+"/.ssh/authorized_keys"),
		"grep -qxF -- " + key + " " + shellQuote(home+"/.ssh/authorized_keys") + " || printf '%s\\n' " + key + " >> " + shellQuote(home+"/.ssh/authorized_keys"),
		"chown " + userGroup + " " + shellQuote(home+"/.ssh/authorized_keys"),
		"printf '%s\\n' '" + user + " ALL=(ALL) NOPASSWD:ALL' >/etc/sudoers.d/90-verself-bootstrap-user && chmod 0440 /etc/sudoers.d/90-verself-bootstrap-user && visudo -cf /etc/sudoers.d/90-verself-bootstrap-user >/dev/null",
	}
}

func rootPasswordDisableCommands() []string {
	body := strings.Join([]string{
		"PasswordAuthentication no",
		"KbdInteractiveAuthentication no",
		"PermitRootLogin prohibit-password",
		"",
	}, "\n")
	return []string{
		"install -d -m 0755 /etc/ssh/sshd_config.d",
		"printf '%s' " + shellQuote(body) + " >/etc/ssh/sshd_config.d/60-verself-root-handoff.conf",
		"/usr/sbin/sshd -t",
		"systemctl reload ssh || systemctl reload sshd",
	}
}

func dialSSH(ctx context.Context, addr, user string, auth []ssh.AuthMethod, opts RootHandoffOptions) (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback(opts.HostKeySHA256, opts.TrustFirstUse),
		Timeout:         opts.Timeout,
	}
	type result struct {
		client *ssh.Client
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		client, err := ssh.Dial("tcp", addr, config)
		ch <- result{client: client, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		return res.client, res.err
	}
}

func hostKeyCallback(expected string, trustFirstUse bool) ssh.HostKeyCallback {
	if expected == "" && trustFirstUse {
		// Fresh Latitude installs do not have a known_hosts entry yet; callers must
		// opt into TOFU explicitly.
		return ssh.InsecureIgnoreHostKey()
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		got := sshSHA256Fingerprint(key)
		if got != expected {
			return fmt.Errorf("host key fingerprint mismatch for %s: got %s, want %s", hostname, got, expected)
		}
		return nil
	}
}

func runSSHCommand(client *ssh.Client, command string) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("new ssh session: %w", err)
	}
	defer func() { _ = session.Close() }()
	out, err := session.CombinedOutput(command)
	if err != nil {
		return fmt.Errorf("remote command failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func readTrimmed(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return strings.TrimSpace(string(body)), nil
}

func readAuthorizedPublicKey(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read public key %s: %w", path, err)
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey(body)
	if err != nil {
		return "", fmt.Errorf("parse public key %s: %w", path, err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))), nil
}

func readPrivateKeySigner(path string) (ssh.Signer, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read private key %s: %w", path, err)
	}
	signer, err := ssh.ParsePrivateKey(body)
	if err != nil {
		return nil, fmt.Errorf("parse private key %s: %w", path, err)
	}
	return signer, nil
}

func sshSHA256Fingerprint(key ssh.PublicKey) string {
	digest := sha256.Sum256(key.Marshal())
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(digest[:])
}

func normalizeSHA256Fingerprint(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "SHA256:") {
		return value
	}
	return "SHA256:" + value
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func isSystemName(value string) bool {
	if value == "" || len(value) > 32 {
		return false
	}
	for i, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			if i == 0 && (r == '-' || (r >= '0' && r <= '9')) {
				return false
			}
			continue
		}
		return false
	}
	return true
}
