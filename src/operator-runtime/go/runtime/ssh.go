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
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const sshDialTimeout = 5 * time.Second

type SSHOptions struct {
	ConfigDir string
	Device    string
	User      string
	Host      string
}

type SSHClient struct {
	host    string
	user    string
	client  *ssh.Client
	closers []io.Closer
	mu      sync.Mutex
}

func DialSSH(ctx context.Context, opts SSHOptions) (*SSHClient, error) {
	if opts.ConfigDir == "" {
		return nil, errors.New("operator ssh: ConfigDir is required")
	}
	if opts.Device == "" {
		return nil, errors.New("operator ssh: Device is required")
	}
	if opts.User == "" {
		return nil, errors.New("operator ssh: User is required")
	}
	if opts.Host == "" {
		return nil, errors.New("operator ssh: Host is required")
	}
	signer, err := loadOperatorSigner(opts.ConfigDir, opts.Device)
	if err != nil {
		return nil, err
	}
	hostKeyCallback, err := acceptNewKnownHostsCallback()
	if err != nil {
		return nil, err
	}
	config := &ssh.ClientConfig{
		User:              opts.User,
		Auth:              []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback:   hostKeyCallback,
		HostKeyAlgorithms: []string{ssh.KeyAlgoED25519},
		Timeout:           sshDialTimeout,
	}
	addr := net.JoinHostPort(opts.Host, "22")
	dialer := net.Dialer{Timeout: sshDialTimeout}
	tcpConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("ssh tcp dial %s: %w", addr, err)
	}
	conn, chans, reqs, err := ssh.NewClientConn(tcpConn, addr, config)
	if err != nil {
		_ = tcpConn.Close()
		return nil, fmt.Errorf("ssh handshake %s: %w", addr, err)
	}
	return &SSHClient{
		host:   opts.Host,
		user:   opts.User,
		client: ssh.NewClient(conn, chans, reqs),
	}, nil
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
	if c == nil {
		return nil, errors.New("operator ssh: client is nil")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
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

func ShellWord(s string) (string, error) {
	if strings.ContainsRune(s, 0) {
		return "", errors.New("remote shell argument contains NUL")
	}
	// The SSH server runs command strings through a login shell before target
	// argv exists, so single quotes protect variables and metacharacters.
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'", nil
}

func loadOperatorSigner(cfgDir, device string) (ssh.Signer, error) {
	keyPath := filepath.Join(cfgDir, "ssh", device)
	certPath := keyPath + "-cert.pub"

	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("operator SSH key %s is missing; run `aspect operator onboard --device=%s`", keyPath, device)
		}
		return nil, err
	}
	keySigner, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("parse operator SSH key %s: %w", keyPath, err)
	}
	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("operator SSH cert %s is missing; run `aspect operator refresh --device=%s`", certPath, device)
		}
		return nil, err
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey(certBytes)
	if err != nil {
		return nil, fmt.Errorf("parse operator SSH cert %s: %w", certPath, err)
	}
	cert, ok := pub.(*ssh.Certificate)
	if !ok {
		return nil, fmt.Errorf("operator SSH cert %s did not contain an OpenSSH certificate", certPath)
	}
	certSigner, err := ssh.NewCertSigner(cert, keySigner)
	if err != nil {
		return nil, fmt.Errorf("pair operator SSH cert with private key: %w", err)
	}
	return certSigner, nil
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
