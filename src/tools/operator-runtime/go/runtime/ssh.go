package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

const sshDialTimeout = 5 * time.Second

var remoteStagingPrefixRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

type SSHOptions struct {
	User           string
	Host           string
	PortCandidates []int
}

type SSHClient struct {
	host       string
	port       int
	user       string
	authMethod string
	client     *ssh.Client
	closers    []io.Closer
	mu         sync.Mutex
}

func DialSSH(ctx context.Context, opts SSHOptions) (*SSHClient, error) {
	if opts.User == "" {
		return nil, errors.New("operator ssh: User is required")
	}
	if opts.Host == "" {
		return nil, errors.New("operator ssh: Host is required")
	}
	authMethods, authClosers, authMethod, err := operatorSSHAuthMethods()
	if err != nil {
		return nil, err
	}
	hostKeyCallback, err := acceptNewKnownHostsCallback()
	if err != nil {
		for _, closer := range authClosers {
			_ = closer.Close()
		}
		return nil, err
	}
	config := &ssh.ClientConfig{
		User:              opts.User,
		Auth:              authMethods,
		HostKeyCallback:   hostKeyCallback,
		HostKeyAlgorithms: []string{ssh.KeyAlgoED25519},
		Timeout:           sshDialTimeout,
	}
	ports := normalizeSSHPortCandidates(opts.PortCandidates)
	dialer := net.Dialer{Timeout: sshDialTimeout}
	var connectErrs []error
	for _, port := range ports {
		addr := net.JoinHostPort(opts.Host, strconv.Itoa(port))
		tcpConn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			connectErrs = append(connectErrs, fmt.Errorf("%s tcp dial: %w", addr, err))
			continue
		}
		conn, chans, reqs, err := ssh.NewClientConn(tcpConn, addr, config)
		if err != nil {
			_ = tcpConn.Close()
			connectErrs = append(connectErrs, fmt.Errorf("%s handshake: %w", addr, err))
			continue
		}
		return &SSHClient{
			host:       opts.Host,
			port:       port,
			user:       opts.User,
			authMethod: authMethod,
			client:     ssh.NewClient(conn, chans, reqs),
			closers:    authClosers,
		}, nil
	}
	for _, closer := range authClosers {
		_ = closer.Close()
	}
	return nil, fmt.Errorf("operator ssh connect %s ports %v: %w", opts.Host, ports, errors.Join(connectErrs...))
}

func normalizeSSHPortCandidates(candidates []int) []int {
	if len(candidates) == 0 {
		return []int{22}
	}
	ports := make([]int, 0, len(candidates))
	for _, port := range candidates {
		if port <= 0 || port > 65535 {
			continue
		}
		seen := false
		for _, existing := range ports {
			if existing == port {
				seen = true
				break
			}
		}
		if !seen {
			ports = append(ports, port)
		}
	}
	if len(ports) == 0 {
		return []int{22}
	}
	return ports
}

func (c *SSHClient) Port() int {
	if c == nil || c.port == 0 {
		return 22
	}
	return c.port
}

func (c *SSHClient) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	type result struct {
		conn net.Conn
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		conn, err := c.client.Dial(network, address)
		ch <- result{conn: conn, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.err != nil && strings.TrimSpace(res.err.Error()) == "" {
			return nil, errors.New("ssh dial failed")
		}
		return res.conn, res.err
	}
}

func (c *SSHClient) Exec(ctx context.Context, command string) ([]byte, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh new session: %w", err)
	}
	defer func() { _ = session.Close() }()
	var stderr bytes.Buffer
	session.Stderr = &stderr
	out, err := session.Output(command)
	if err != nil {
		return nil, fmt.Errorf("ssh exec %q: %w (stderr: %s)", command, err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func (c *SSHClient) Run(ctx context.Context, command string, stdin io.Reader, stdout, stderr io.Writer) error {
	session, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh new session: %w", err)
	}
	defer func() { _ = session.Close() }()
	session.Stdin = stdin
	session.Stdout = stdout
	session.Stderr = stderr
	done := make(chan error, 1)
	go func() {
		done <- session.Run(command)
	}()
	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGTERM)
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return fmt.Errorf("ssh run %q: %w", command, err)
		}
		return nil
	}
}

func (c *SSHClient) RunPTY(ctx context.Context, command string, stdin io.Reader, stdout, stderr io.Writer) error {
	session, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh new session: %w", err)
	}
	defer func() { _ = session.Close() }()
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm-256color", 40, 120, modes); err != nil {
		return fmt.Errorf("request ssh pty: %w", err)
	}
	session.Stdin = stdin
	session.Stdout = stdout
	session.Stderr = stderr
	done := make(chan error, 1)
	go func() {
		done <- session.Run(command)
	}()
	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGTERM)
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return fmt.Errorf("ssh run pty %q: %w", command, err)
		}
		return nil
	}
}

type Forward struct {
	Role       string
	ListenAddr string
	listener   net.Listener
	cancel     context.CancelFunc
	once       sync.Once
	closeErr   error
}

func (c *SSHClient) Forward(parent context.Context, role, remoteAddr string) (*Forward, error) {
	return c.ForwardLocal(parent, role, "127.0.0.1:0", remoteAddr)
}

func (c *SSHClient) ForwardLocal(parent context.Context, role, localAddr, remoteAddr string) (*Forward, error) {
	if c == nil {
		return nil, errors.New("operator ssh: client is nil")
	}
	if localAddr == "" {
		localAddr = "127.0.0.1:0"
	}
	listener, err := net.Listen("tcp", localAddr)
	if err != nil {
		return nil, fmt.Errorf("listen for local forward: %w", err)
	}
	ctx, cancel := context.WithCancel(parent)
	forward := &Forward{
		Role:       role,
		ListenAddr: listener.Addr().String(),
		listener:   listener,
		cancel:     cancel,
	}
	go c.acceptForward(ctx, listener, remoteAddr)
	c.mu.Lock()
	c.closers = append(c.closers, forward)
	c.mu.Unlock()
	return forward, nil
}

func (f *Forward) Close() error {
	if f == nil {
		return nil
	}
	f.once.Do(func() {
		f.cancel()
		f.closeErr = f.listener.Close()
	})
	return f.closeErr
}

func (c *SSHClient) acceptForward(ctx context.Context, listener net.Listener, remoteAddr string) {
	for {
		local, err := listener.Accept()
		if err != nil {
			return
		}
		select {
		case <-ctx.Done():
			_ = local.Close()
			return
		default:
		}
		go c.proxyForward(local, remoteAddr)
	}
}

func (c *SSHClient) proxyForward(local net.Conn, remoteAddr string) {
	defer func() { _ = local.Close() }()
	remote, err := c.client.Dial("tcp", remoteAddr)
	if err != nil {
		return
	}
	defer func() { _ = remote.Close() }()
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(remote, local)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(local, remote)
		done <- struct{}{}
	}()
	<-done
}

func (c *SSHClient) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	closers := c.closers
	c.closers = nil
	c.mu.Unlock()
	var closeErr error
	for _, closer := range closers {
		closeErr = errors.Join(closeErr, closer.Close())
	}
	if c.client != nil {
		closeErr = errors.Join(closeErr, c.client.Close())
		c.client = nil
	}
	return closeErr
}

func ReadRemoteFile(ctx context.Context, sshClient *SSHClient, path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("empty remote path")
	}
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("remote path must be absolute: %q", path)
	}
	if strings.ContainsRune(path, 0) {
		return nil, errors.New("remote path contains NUL")
	}
	pathWord, err := ShellWord(path)
	if err != nil {
		return nil, err
	}
	return sshClient.Exec(ctx, "sudo /bin/cat -- "+pathWord)
}

func (c *SSHClient) UploadExecutable(ctx context.Context, localPath, prefix string) (string, error) {
	if c == nil {
		return "", errors.New("operator ssh: client is nil")
	}
	if localPath == "" {
		return "", errors.New("upload executable: local path is required")
	}
	remotePath, err := RemoteStagingPath(prefix)
	if err != nil {
		return "", err
	}
	local, err := os.Open(localPath)
	if err != nil {
		return "", fmt.Errorf("open executable %s: %w", localPath, err)
	}
	defer func() { _ = local.Close() }()
	if _, err := c.Exec(ctx, "sudo /usr/bin/install -d -m 0755 /opt/verself/staging"); err != nil {
		return "", fmt.Errorf("prepare remote staging directory: %w", err)
	}
	remoteWord, err := ShellWord(remotePath)
	if err != nil {
		return "", err
	}
	if err := c.Run(ctx, "sudo /usr/bin/tee "+remoteWord+" >/dev/null", local, io.Discard, os.Stderr); err != nil {
		return "", fmt.Errorf("upload executable to %s: %w", remotePath, err)
	}
	if _, err := c.Exec(ctx, "sudo /bin/chmod 0755 "+remoteWord); err != nil {
		_ = c.RemoveRemotePath(ctx, remotePath)
		return "", fmt.Errorf("chmod executable %s: %w", remotePath, err)
	}
	return remotePath, nil
}

func RemoteStagingPath(prefix string) (string, error) {
	if prefix == "" {
		return "", errors.New("remote staging prefix is required")
	}
	if !remoteStagingPrefixRE.MatchString(prefix) {
		return "", fmt.Errorf("remote staging prefix %q must match ^[A-Za-z0-9_.-]+$", prefix)
	}
	return "/opt/verself/staging/" + prefix + "." + uuid.NewString(), nil
}

func (c *SSHClient) RemoveRemotePath(ctx context.Context, path string) error {
	if c == nil {
		return nil
	}
	if path == "" {
		return nil
	}
	pathWord, err := ShellWord(path)
	if err != nil {
		return err
	}
	_, err = c.Exec(ctx, "sudo /bin/rm -f -- "+pathWord)
	return err
}

func (c *SSHClient) RunArgv(ctx context.Context, runAsUser string, argv []string, stdin io.Reader, stdout, stderr io.Writer) error {
	command, err := RemoteCommand(runAsUser, argv)
	if err != nil {
		return err
	}
	return c.Run(ctx, command, stdin, stdout, stderr)
}

func (c *SSHClient) RunArgvPTY(ctx context.Context, runAsUser string, argv []string, stdin io.Reader, stdout, stderr io.Writer) error {
	command, err := RemoteCommand(runAsUser, argv)
	if err != nil {
		return err
	}
	return c.RunPTY(ctx, command, stdin, stdout, stderr)
}

func RemoteCommand(runAsUser string, argv []string) (string, error) {
	if len(argv) == 0 {
		return "", errors.New("remote command argv is empty")
	}
	parts := make([]string, 0, len(argv)+4)
	if runAsUser != "" {
		if !remoteStagingPrefixRE.MatchString(runAsUser) {
			return "", fmt.Errorf("remote run-as user %q must match ^[A-Za-z0-9_.-]+$", runAsUser)
		}
		userWord, err := ShellWord(runAsUser)
		if err != nil {
			return "", err
		}
		parts = append(parts, "sudo", "-u", userWord, "--")
	}
	for _, arg := range argv {
		word, err := ShellWord(arg)
		if err != nil {
			return "", err
		}
		parts = append(parts, word)
	}
	return strings.Join(parts, " "), nil
}

func ShellWord(s string) (string, error) {
	if strings.ContainsRune(s, 0) {
		return "", errors.New("remote shell argument contains NUL")
	}
	// The SSH server runs command strings through a login shell before target
	// argv exists, so single quotes protect variables and metacharacters.
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'", nil
}

func operatorSSHAuthMethods() ([]ssh.AuthMethod, []io.Closer, string, error) {
	var (
		methods           []ssh.AuthMethod
		closers           []io.Closer
		labels            []string
		passphraseKeyPath string
	)

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, "", fmt.Errorf("home dir: %w", err)
	}
	for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		path := filepath.Join(home, ".ssh", name)
		keyBytes, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, nil, "", fmt.Errorf("read SSH identity %s: %w", path, err)
		}
		signer, err := ssh.ParsePrivateKey(keyBytes)
		if err != nil {
			var passphraseMissing *ssh.PassphraseMissingError
			// Passphrase-protected default keys are expected on laptops; use the agent signer instead.
			if errors.As(err, &passphraseMissing) {
				passphraseKeyPath = path
				continue
			}
			return nil, nil, "", fmt.Errorf("parse SSH identity %s: %w", path, err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
		labels = append(labels, name)
		break
	}

	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		conn, err := net.Dial("unix", sock)
		if err != nil {
			return nil, nil, "", fmt.Errorf("ssh agent dial: %w", err)
		}
		signers, err := agent.NewClient(conn).Signers()
		if err != nil {
			_ = conn.Close()
			return nil, nil, "", fmt.Errorf("ssh agent signers: %w", err)
		}
		if len(signers) == 0 {
			_ = conn.Close()
		} else {
			methods = append(methods, ssh.PublicKeys(signers...))
			closers = append(closers, conn)
			labels = append(labels, "ssh-agent")
		}
	}
	if len(methods) > 0 {
		methods = append(methods, ssh.KeyboardInteractive(operatorKeyboardInteractiveChallenge))
		labels = append(labels, "keyboard-interactive")
	}
	if len(methods) == 0 {
		if passphraseKeyPath != "" {
			return nil, nil, "", fmt.Errorf("operator ssh: default SSH identity %s is passphrase-protected; run ssh-add %s", passphraseKeyPath, passphraseKeyPath)
		}
		return nil, nil, "", errors.New("operator ssh: no usable SSH signer found; run ssh-add ~/.ssh/id_ed25519 or create an unencrypted default key")
	}
	return methods, closers, strings.Join(labels, "+"), nil
}

func operatorKeyboardInteractiveChallenge(name, instruction string, questions []string, _ []bool) ([]string, error) {
	for _, line := range []string{name, instruction} {
		if line = strings.TrimSpace(line); line != "" {
			_, _ = fmt.Fprintln(os.Stderr, line)
		}
	}
	for _, question := range questions {
		if question = strings.TrimSpace(question); question != "" {
			_, _ = fmt.Fprintln(os.Stderr, question)
		}
	}
	// Pomerium native SSH uses keyboard-interactive to show a browser/device
	// sign-in URL after public-key partial auth; answers are out-of-band.
	return make([]string, len(questions)), nil
}

func acceptNewKnownHostsCallback() (ssh.HostKeyCallback, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".ssh", "known_hosts")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0o600); err != nil {
		return nil, err
	} else {
		_ = f.Close()
	}
	callback, err := knownhosts.New(path)
	if err != nil {
		return nil, err
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := callback(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if !errors.As(err, &keyErr) || len(keyErr.Want) > 0 {
			return err
		}
		line := knownhosts.Line(uniqueKnownHostAddresses(hostname, remote), key)
		f, openErr := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
		if openErr != nil {
			return openErr
		}
		defer func() { _ = f.Close() }()
		if _, writeErr := f.WriteString(line + "\n"); writeErr != nil {
			return writeErr
		}
		return nil
	}, nil
}

func uniqueKnownHostAddresses(hostname string, remote net.Addr) []string {
	out := make([]string, 0, 2)
	seen := map[string]bool{}
	for _, raw := range []string{hostname, remote.String()} {
		normalized := knownhosts.Normalize(raw)
		if normalized == "" || seen[normalized] {
			continue
		}
		out = append(out, normalized)
		seen[normalized] = true
	}
	return out
}
