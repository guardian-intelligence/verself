// Package sshtun is the deploy's single SSH session: one *ssh.Client
// dialled at process start, multiplexed across role-typed local-port
// forwards (artifact, nomad) plus on-demand remote-command execution
// for `sudo cat`-style controller-to-host secret reads.
//
// Bash equivalents replaced:
//   - per-tunnel `ssh -N -L` invocations (one per role today)
//   - ad-hoc `ssh "${HOST}" "sudo cat ..."` reads
//   - `BatchMode=yes ExitOnForwardFailure=yes ControlMaster=no
//     ControlPath=none` flag-shake against persistent multiplexing
//
// Auth follows the configured operator SSH access plane. We do not shell out
// to the system ssh, so missing or locked key material surfaces as Go errors
// rather than hanging on tty input.
package sshtun

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

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

const tracerName = "github.com/verself/deployment-tools/internal/sshtun"

// Client is a deploy-scoped SSH connection. Open it once with Dial,
// register one local-port forward per role with Forward, run remote
// commands with Exec, and Close at end of life.
type Client struct {
	host      string
	user      string
	conn      *ssh.Client
	tracer    trace.Tracer
	agentConn net.Conn
	mu        sync.Mutex
	closers   []io.Closer
}

// Dial opens an SSH connection authenticated by the operator's SSH
// agent. Strict host-key checking is disabled to match the existing
// bash behaviour (the controller is on a private wireguard mesh; the
// hardening of host-key pinning is a Phase 4 follow-up).
func Dial(ctx context.Context, host, user string) (*Client, error) {
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "verself_deploy.ssh.connect",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("ssh.host", host),
			attribute.String("ssh.user", user),
		),
	)
	defer span.End()

	if host == "" || user == "" {
		err := errors.New("sshtun: host and user are required")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	authMethods, agentConn, authSpanAttr, err := buildAuthMethods()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetAttributes(attribute.String("ssh.auth_method", authSpanAttr))

	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	dialer := net.Dialer{Timeout: cfg.Timeout}
	tcpConn, err := dialer.DialContext(ctx, "tcp", host+":22")
	if err != nil {
		closeAgentConn(agentConn)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("ssh tcp dial: %w", err)
	}
	cc, chans, reqs, err := ssh.NewClientConn(tcpConn, host+":22", cfg)
	if err != nil {
		_ = tcpConn.Close()
		closeAgentConn(agentConn)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("ssh handshake: %w", err)
	}
	span.SetStatus(codes.Ok, "")
	c := &Client{
		host:      host,
		user:      user,
		conn:      ssh.NewClient(cc, chans, reqs),
		tracer:    tracer,
		agentConn: agentConn,
	}
	return c, nil
}

func closeAgentConn(conn net.Conn) {
	if conn != nil {
		_ = conn.Close()
	}
}

// Forward is one role-tagged local-port forward. ListenAddr is the
// 127.0.0.1:<port> the caller dials; the forward proxies each
// accepted connection to the same port on the remote loopback.
type Forward struct {
	Role       string
	ListenAddr string
	listener   net.Listener
	cancel     context.CancelFunc
	once       sync.Once
	closeErr   error
}

// Forward starts a local TCP listener on 127.0.0.1:0 (kernel picks
// the port) and forwards every accepted connection through the SSH
// session to the remote 127.0.0.1:remotePort. The role label is
// surfaced as a span attribute and is part of the
// ssh.channel.open span tree.
func (c *Client) Forward(ctx context.Context, role string, remotePort int) (*Forward, error) {
	return c.forward(ctx, role, "127.0.0.1:0", remotePort)
}

func (c *Client) ForwardLocalPort(ctx context.Context, role string, localPort, remotePort int) (*Forward, error) {
	if localPort <= 0 {
		return nil, fmt.Errorf("local port must be positive: %d", localPort)
	}
	return c.forward(ctx, role, fmt.Sprintf("127.0.0.1:%d", localPort), remotePort)
}

func (c *Client) forward(ctx context.Context, role, localAddr string, remotePort int) (*Forward, error) {
	_, span := c.tracer.Start(ctx, "verself_deploy.ssh.channel.open",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("channel.role", role),
			attribute.String("channel.local_addr", localAddr),
			attribute.Int("channel.remote_port", remotePort),
		),
	)
	defer span.End()

	listener, err := net.Listen("tcp", localAddr)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("local listen: %w", err)
	}
	span.SetAttributes(attribute.String("channel.listen_addr", listener.Addr().String()))

	forwardCtx, cancel := context.WithCancel(context.Background())
	forward := &Forward{
		Role:       role,
		ListenAddr: listener.Addr().String(),
		listener:   listener,
		cancel:     cancel,
	}
	go c.acceptLoop(forwardCtx, listener, remotePort)

	c.mu.Lock()
	c.closers = append(c.closers, forward)
	c.mu.Unlock()

	span.SetStatus(codes.Ok, "")
	return forward, nil
}

// Close tears down the listener backing this forward. The parent SSH
// client also calls this during Close; idempotence lets owner packages
// release role-specific tunnels earlier without racing final cleanup.
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

func (c *Client) acceptLoop(ctx context.Context, listener net.Listener, remotePort int) {
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
		go c.proxy(local, remotePort)
	}
}

func (c *Client) proxy(local net.Conn, remotePort int) {
	defer func() { _ = local.Close() }()
	remote, err := c.conn.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", remotePort))
	if err != nil {
		return
	}
	defer func() { _ = remote.Close() }()
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(remote, local); done <- struct{}{} }()
	go func() { _, _ = io.Copy(local, remote); done <- struct{}{} }()
	<-done
}

// Exec runs a single remote command and returns its stdout. Errors
// surface stderr in the message so an operator looking at a span
// failure has the underlying remote diagnostic without re-SSHing.
func (c *Client) Exec(ctx context.Context, command string) ([]byte, error) {
	_, span := c.tracer.Start(ctx, "verself_deploy.ssh.exec",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("ssh.command", command)),
	)
	defer span.End()

	session, err := c.conn.NewSession()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("ssh new session: %w", err)
	}
	defer func() { _ = session.Close() }()

	var stderr bytes.Buffer
	session.Stderr = &stderr
	out, err := session.Output(command)
	if err != nil {
		// Surface remote stderr in the wrapped error so a span failure
		// shows the actual diagnostic from the controller — sudo's
		// "command not allowed" or `cat: foo: No such file or
		// directory` is the load-bearing line.
		err = fmt.Errorf("ssh exec %q: %w (stderr: %s)", command, err, strings.TrimSpace(stderr.String()))
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetAttributes(attribute.Int("ssh.bytes_received", len(out)))
	span.SetStatus(codes.Ok, "")
	return out, nil
}

// Close tears down every registered forward, the SSH connection, and
// the SSH-agent handle. Idempotent; safe to defer.
func (c *Client) Close() error {
	c.mu.Lock()
	closers := c.closers
	c.closers = nil
	c.mu.Unlock()
	for _, fw := range closers {
		_ = fw.Close()
	}
	var errAll error
	if c.conn != nil {
		errAll = c.conn.Close()
	}
	if c.agentConn != nil {
		_ = c.agentConn.Close()
	}
	return errAll
}

// buildAuthMethods assembles every operator public-key source available to
// this process. Pomerium native SSH binds ordinary SSH keys to the OIDC user;
// the historical cert layout stays readable during the live host cutover.
func buildAuthMethods() ([]ssh.AuthMethod, net.Conn, string, error) {
	var (
		methods  []ssh.AuthMethod
		labels   []string
		conn     net.Conn
		agentErr error
	)
	// Some access proxies end auth negotiation after an invalid SSH cert,
	// so offer ordinary identities before the legacy certificate path.
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		agentConn, err := net.Dial("unix", sock)
		if err != nil {
			// VS Code can leave SSH_AUTH_SOCK pointing at a removed socket; fall back to file-backed operator keys.
			agentErr = fmt.Errorf("ssh agent dial: %w", err)
		} else {
			signers, err := agent.NewClient(agentConn).Signers()
			if err != nil {
				_ = agentConn.Close()
				agentErr = fmt.Errorf("ssh agent signers: %w", err)
			} else if len(signers) == 0 {
				_ = agentConn.Close()
			} else {
				conn = agentConn
				methods = append(methods, ssh.PublicKeys(signers...))
				labels = append(labels, "ssh-agent")
			}
		}
	}
	if signer, label, err := loadDefaultKeySigner(len(methods) > 0); err == nil {
		methods = append(methods, ssh.PublicKeys(signer))
		labels = append(labels, label)
	} else if !errors.Is(err, errNoDefaultKey) {
		if conn != nil {
			_ = conn.Close()
		}
		return nil, nil, "", err
	}
	if signer, err := loadVerselfCertSigner(); err == nil {
		methods = append(methods, ssh.PublicKeys(signer))
		labels = append(labels, "verself-cert")
	} else if !errors.Is(err, errNoVerselfCert) {
		if conn != nil {
			_ = conn.Close()
		}
		return nil, nil, "", err
	}
	if len(methods) > 0 {
		methods = append(methods, ssh.KeyboardInteractive(operatorKeyboardInteractiveChallenge))
		labels = append(labels, "keyboard-interactive")
	}
	if len(methods) == 0 {
		if agentErr != nil {
			return nil, nil, "", fmt.Errorf("%w; no fallback signer found", agentErr)
		}
		return nil, nil, "", errors.New("sshtun: no usable SSH signer found; run ssh-add ~/.ssh/id_ed25519 or create an unencrypted default key")
	}
	return methods, conn, strings.Join(labels, "+"), nil
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

var errNoDefaultKey = errors.New("no default ssh key found")

func loadDefaultKeySigner(skipEncrypted bool) (ssh.Signer, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, "", fmt.Errorf("home dir: %w", err)
	}
	for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		path := filepath.Join(home, ".ssh", name)
		keyBytes, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, "", fmt.Errorf("read identity %s: %w", path, err)
		}
		signer, err := ssh.ParsePrivateKey(keyBytes)
		if err != nil {
			var passphraseMissing *ssh.PassphraseMissingError
			// Passphrase-protected default keys are expected on laptops; use the agent signer instead.
			if skipEncrypted && errors.As(err, &passphraseMissing) {
				continue
			}
			return nil, "", fmt.Errorf("parse identity %s: %w", path, err)
		}
		return signer, name, nil
	}
	return nil, "", errNoDefaultKey
}

// errNoVerselfCert distinguishes "no operator cert provisioned"
// (which means we should fall back to the agent) from "found a cert
// but it's malformed" (which is fatal).
var errNoVerselfCert = errors.New("no verself ssh cert found")

// loadVerselfCertSigner walks ~/.config/verself/ssh/ for a pre-cutover
// (private-key, certificate) pair and returns a cert-bearing ssh.Signer.
// The pair lives at <name> (private key, no extension) + <name>-cert.pub
// (signed certificate); the public key file <name>.pub is not used.
func loadVerselfCertSigner() (ssh.Signer, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("home dir: %w", err)
	}
	dir := filepath.Join(home, ".config", "verself", "ssh")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, errNoVerselfCert
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasSuffix(name, ".pub") {
			continue
		}
		certPath := filepath.Join(dir, name+"-cert.pub")
		if _, err := os.Stat(certPath); err != nil {
			continue
		}
		keyBytes, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read identity %s: %w", name, err)
		}
		signer, err := ssh.ParsePrivateKey(keyBytes)
		if err != nil {
			return nil, fmt.Errorf("parse identity %s: %w", name, err)
		}
		certBytes, err := os.ReadFile(certPath)
		if err != nil {
			return nil, fmt.Errorf("read cert %s: %w", certPath, err)
		}
		pub, _, _, _, err := ssh.ParseAuthorizedKey(certBytes)
		if err != nil {
			return nil, fmt.Errorf("parse cert %s: %w", certPath, err)
		}
		cert, ok := pub.(*ssh.Certificate)
		if !ok {
			return nil, fmt.Errorf("%s is not an SSH certificate", certPath)
		}
		certSigner, err := ssh.NewCertSigner(cert, signer)
		if err != nil {
			return nil, fmt.Errorf("build cert signer for %s: %w", name, err)
		}
		return certSigner, nil
	}
	return nil, errNoVerselfCert
}
