// Package otelagent supervises a controller-side otelcol-contrib
// agent for the lifetime of a verself-deploy invocation.
//
// The agent's role is narrow: receive OTLP traces/logs/metrics on a
// fixed loopback port (14317), buffer them in a file_storage-backed
// queue, and forward to the bare-metal otelcol over an SSH-forwarded
// channel. Buffering decouples the agent's drain from process exit so
// Ansible's BSP atexit flush isn't racing the SSH tunnel teardown
// (the bug that the prior bash supervisor papered over with sleep
// timing).
//
// Boundary: this package owns the agent process, not the SSH
// channel. The caller passes the local listen address of an
// already-open OTLP forward (typically obtained from
// internal/sshtun.Client.Forward(ctx, "otlp", 4317)). The agent
// supervisor never opens an SSH connection itself.
package otelagent

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// LocalReceiverEndpoint is the fixed loopback address the agent binds
// for its OTLP gRPC receiver. The OTel SDK in this binary points
// OTEL_EXPORTER_OTLP_ENDPOINT at this address; child processes
// (ansible-playbook with the verself_otel callback) inherit the same
// pointer through the environment.
const LocalReceiverEndpoint = "127.0.0.1:14317"

// drainBudget bounds the SIGTERM-to-exit window the supervisor gives
// the agent. Matches the prior bash budget; the file-storage queue
// preserves anything still in flight on SIGKILL, so this is the
// "would have been visible on the current run" deadline rather than
// a correctness requirement.
const drainBudget = 90 * time.Second

//go:embed otelcol.yaml
var defaultConfig []byte

// Config configures the supervised agent.
type Config struct {
	// ForwardEndpoint is the address the agent's upstream OTLP
	// exporter dials — typically the 127.0.0.1:<port> ListenAddr of
	// an SSH local-port forward to the bare-metal otelcol's :4317.
	ForwardEndpoint string
	// Site namespaces the file-storage queue directory so concurrent
	// per-site deploys (when we get there) don't share a queue.
	Site string
	// Binary is the otelcol-contrib executable on $PATH or as an
	// absolute path. Empty defaults to "otelcol-contrib".
	Binary string
}

// Agent represents a running otelcol-contrib supervised by this
// process. Methods are safe to call from a single goroutine; Shutdown
// is the only one designed to be called twice (idempotent).
type Agent struct {
	cfg        Config
	cmd        *exec.Cmd
	configPath string
	logPath    string
	dataDir    string

	mu       sync.Mutex
	stopped  bool
	exitErr  error
	doneCh   chan struct{}
}

// Start launches otelcol-contrib with the embedded config, waits for
// the receiver to bind, and returns a live agent. Errors here mean
// the agent never came up — callers should not Shutdown a nil Agent.
func Start(ctx context.Context, cfg Config) (*Agent, error) {
	if cfg.ForwardEndpoint == "" {
		return nil, errors.New("otelagent: ForwardEndpoint is required")
	}
	binary := cfg.Binary
	if binary == "" {
		binary = "otelcol-contrib"
	}
	if _, err := exec.LookPath(binary); err != nil {
		return nil, fmt.Errorf("otelagent: %s not on PATH: %w", binary, err)
	}

	// Refuse to start if the local receiver port is already taken —
	// a parallel verself-deploy on the same controller would race on
	// the file-storage queue. Surface the collision rather than
	// silently corrupting the queue.
	if c, err := net.DialTimeout("tcp", LocalReceiverEndpoint, 250*time.Millisecond); err == nil {
		_ = c.Close()
		return nil, fmt.Errorf("otelagent: %s is already bound (parallel verself-deploy in flight)", LocalReceiverEndpoint)
	}

	dataDir, err := agentDataDir(cfg.Site)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("otelagent: mkdir queue dir: %w", err)
	}

	configPath := filepath.Join(dataDir, "otelcol.yaml")
	if err := os.WriteFile(configPath, defaultConfig, 0o644); err != nil {
		return nil, fmt.Errorf("otelagent: write config: %w", err)
	}
	logPath := filepath.Join(dataDir, "agent.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("otelagent: open agent log: %w", err)
	}

	cmd := exec.Command(binary, "--config", configPath)
	// The embedded config references VERSELF_AGENT_FORWARD_ENDPOINT
	// and VERSELF_AGENT_DATA_DIR via ${env:...}; setting them on the
	// child env (rather than the parent's) keeps Go-side telemetry
	// pointed at the local receiver while the agent points at the
	// upstream tunnel.
	cmd.Env = append(os.Environ(),
		"VERSELF_AGENT_FORWARD_ENDPOINT="+cfg.ForwardEndpoint,
		"VERSELF_AGENT_DATA_DIR="+dataDir,
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	// Detach the child into its own pgid so a parent SIGINT doesn't
	// race the supervisor's controlled drain.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("otelagent: start otelcol-contrib: %w", err)
	}

	a := &Agent{
		cfg:        cfg,
		cmd:        cmd,
		configPath: configPath,
		logPath:    logPath,
		dataDir:    dataDir,
		doneCh:     make(chan struct{}),
	}
	go func() {
		err := cmd.Wait()
		a.mu.Lock()
		a.exitErr = err
		a.mu.Unlock()
		_ = logFile.Close()
		close(a.doneCh)
	}()

	if err := waitReady(ctx, LocalReceiverEndpoint, 10*time.Second); err != nil {
		_ = a.Shutdown(context.Background())
		return nil, fmt.Errorf("otelagent: %w (see %s)", err, logPath)
	}
	return a, nil
}

// Endpoint is the address callers should set
// OTEL_EXPORTER_OTLP_ENDPOINT to. Constant for now; surfaced as a
// method so future agents on a kernel-picked port require no caller
// changes.
func (a *Agent) Endpoint() string { return LocalReceiverEndpoint }

// LogPath is where the agent's stderr/stdout was redirected. Useful
// when surfacing a startup failure in an error message.
func (a *Agent) LogPath() string { return a.logPath }

// QueueDir is the file-storage queue location. Visible so operators
// debugging a stuck queue have the path without re-deriving it.
func (a *Agent) QueueDir() string { return a.dataDir }

// Shutdown sends SIGTERM and waits up to drainBudget for graceful
// exit, then SIGKILL. Returns the agent's exit error (nil on a clean
// shutdown). Idempotent.
func (a *Agent) Shutdown(ctx context.Context) error {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		<-a.doneCh
		return a.exitErr
	}
	a.stopped = true
	a.mu.Unlock()

	if a.cmd.Process != nil {
		_ = a.cmd.Process.Signal(syscall.SIGTERM)
	}

	// Honor either drainBudget or the caller's context deadline,
	// whichever expires first. Operators tearing down a deploy with
	// Ctrl-C should not have to wait the full 90 seconds.
	timeout := drainBudget
	if dl, ok := ctx.Deadline(); ok {
		if remaining := time.Until(dl); remaining < timeout {
			timeout = remaining
		}
	}
	select {
	case <-a.doneCh:
	case <-time.After(timeout):
		if a.cmd.Process != nil {
			_ = a.cmd.Process.Signal(syscall.SIGKILL)
		}
		<-a.doneCh
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.exitErr
}

// waitReady polls the agent's receiver until it accepts a TCP
// connection or the timeout expires. The OTel SDK does its own retry
// on the first export, but blocking here means the BatchSpanProcessor
// doesn't surface "exporter unavailable" warnings on the first batch.
func waitReady(ctx context.Context, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("agent did not bind %s within %s", addr, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// agentDataDir resolves $XDG_CACHE_HOME (or ~/.cache) plus
// verself/otelcol-controller/<site>. Site defaults to "default" when
// empty so a one-shot dev invocation still has a stable queue dir.
func agentDataDir(site string) (string, error) {
	root := os.Getenv("XDG_CACHE_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("otelagent: home dir: %w", err)
		}
		root = filepath.Join(home, ".cache")
	}
	if site == "" {
		site = "default"
	}
	site = strings.ReplaceAll(site, "/", "_")
	return filepath.Join(root, "verself", "otelcol-controller", site), nil
}
