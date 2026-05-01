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
// Auth follows the operator's existing SSH agent contract; the agent
// socket discovered via SSH_AUTH_SOCK signs the handshake. We do not
// shell out to the system ssh, so any prompts from a missing/locked
// agent surface as Go errors rather than hanging on tty input.
package sshtun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
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

const tracerName = "github.com/verself/deployment-tooling/internal/sshtun"

// Client is a deploy-scoped SSH connection. Open it once with Dial,
// register one local-port forward per role with Forward, run remote
// commands with Exec, and Close at end of life.
type Client struct {
	host    string
	user    string
	conn    *ssh.Client
	tracer  trace.Tracer
	mu      sync.Mutex
	closers []io.Closer
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

	signers, err := agentSigners()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signers...)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	dialer := net.Dialer{Timeout: cfg.Timeout}
	tcpConn, err := dialer.DialContext(ctx, "tcp", host+":22")
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("ssh tcp dial: %w", err)
	}
	cc, chans, reqs, err := ssh.NewClientConn(tcpConn, host+":22", cfg)
	if err != nil {
		_ = tcpConn.Close()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("ssh handshake: %w", err)
	}
	span.SetStatus(codes.Ok, "")
	return &Client{
		host:   host,
		user:   user,
		conn:   ssh.NewClient(cc, chans, reqs),
		tracer: tracer,
	}, nil
}

// Forward is one role-tagged local-port forward. ListenAddr is the
// 127.0.0.1:<port> the caller dials; the forward proxies each
// accepted connection to the same port on the remote loopback.
type Forward struct {
	Role       string
	ListenAddr string
	listener   net.Listener
	cancel     context.CancelFunc
}

// Forward starts a local TCP listener on 127.0.0.1:0 (kernel picks
// the port) and forwards every accepted connection through the SSH
// session to the remote 127.0.0.1:remotePort. The role label is
// surfaced as a span attribute and is part of the
// ssh.channel.open span tree.
func (c *Client) Forward(ctx context.Context, role string, remotePort int) (*Forward, error) {
	_, span := c.tracer.Start(ctx, "verself_deploy.ssh.channel.open",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("channel.role", role),
			attribute.Int("channel.remote_port", remotePort),
		),
	)
	defer span.End()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("local listen: %w", err)
	}
	span.SetAttributes(attribute.String("channel.listen_addr", listener.Addr().String()))

	forwardCtx, cancel := context.WithCancel(context.Background())
	go c.acceptLoop(forwardCtx, listener, remotePort)

	c.mu.Lock()
	c.closers = append(c.closers, &listenerCloser{l: listener, cancel: cancel})
	c.mu.Unlock()

	span.SetStatus(codes.Ok, "")
	return &Forward{
		Role:       role,
		ListenAddr: listener.Addr().String(),
		listener:   listener,
		cancel:     cancel,
	}, nil
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
	defer local.Close()
	remote, err := c.conn.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", remotePort))
	if err != nil {
		return
	}
	defer remote.Close()
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
	defer session.Close()

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

// Close tears down every registered forward and the SSH connection
// itself. Idempotent; safe to defer.
func (c *Client) Close() error {
	c.mu.Lock()
	closers := c.closers
	c.closers = nil
	c.mu.Unlock()
	for _, c := range closers {
		_ = c.Close()
	}
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func agentSigners() ([]ssh.Signer, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, errors.New("sshtun: SSH_AUTH_SOCK is unset; verself-deploy requires an authenticated ssh-agent")
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, fmt.Errorf("ssh agent dial: %w", err)
	}
	a := agent.NewClient(conn)
	signers, err := a.Signers()
	if err != nil {
		return nil, fmt.Errorf("ssh agent signers: %w", err)
	}
	if len(signers) == 0 {
		return nil, errors.New("sshtun: ssh-agent has no identities loaded")
	}
	return signers, nil
}

type listenerCloser struct {
	l      net.Listener
	cancel context.CancelFunc
}

func (lc *listenerCloser) Close() error {
	lc.cancel()
	return lc.l.Close()
}
