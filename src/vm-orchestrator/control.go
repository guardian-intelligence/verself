package vmorchestrator

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/forge-metal/vm-orchestrator/vmproto"
	"go.opentelemetry.io/otel/attribute"
)

type guestControl struct {
	conn   net.Conn
	reader io.Reader
	codec  *vmproto.Codec

	mu  sync.Mutex
	seq uint64

	activeExecID string
}

type checkpointHandler func(vmproto.CheckpointRequest) vmproto.CheckpointResponse

func connectGuestControl(ctx context.Context, udsPath string, port int) (*guestControl, error) {
	conn, reader, err := connectGuestBridge(ctx, udsPath, port)
	if err != nil {
		return nil, err
	}
	return &guestControl{conn: conn, reader: reader, codec: vmproto.NewCodec(reader, conn)}, nil
}

func connectGuestBridge(ctx context.Context, udsPath string, port int) (net.Conn, *bufio.Reader, error) {
	for {
		conn, err := net.DialTimeout("unix", udsPath, 250*time.Millisecond)
		if err != nil {
			select {
			case <-ctx.Done():
				return nil, nil, fmt.Errorf("dial guest bridge %s: %w", udsPath, ctx.Err())
			case <-time.After(50 * time.Millisecond):
				continue
			}
		}
		reader := bufio.NewReader(conn)
		if _, err := io.WriteString(conn, fmt.Sprintf("CONNECT %d\n", port)); err != nil {
			conn.Close()
			select {
			case <-ctx.Done():
				return nil, nil, fmt.Errorf("write guest bridge connect command: %w", ctx.Err())
			case <-time.After(50 * time.Millisecond):
				continue
			}
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			conn.Close()
			select {
			case <-ctx.Done():
				return nil, nil, fmt.Errorf("read guest bridge ack: %w", ctx.Err())
			case <-time.After(50 * time.Millisecond):
				continue
			}
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "OK ") {
			conn.Close()
			return nil, nil, fmt.Errorf("unexpected guest bridge ack %q", strings.TrimSpace(line))
		}
		return conn, reader, nil
	}
}

func (c *guestControl) close() error {
	return c.conn.Close()
}

func (c *guestControl) send(msgType vmproto.MessageType, payload any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	env, err := vmproto.NewEnvelope(msgType, c.seq, time.Now().UnixNano(), payload)
	if err != nil {
		return err
	}
	return c.codec.WriteEnvelope(env)
}

func (c *guestControl) recv() (vmproto.Envelope, error) {
	if err := c.conn.SetReadDeadline(time.Now().Add(vmproto.HeartbeatTimeout)); err != nil {
		return vmproto.Envelope{}, err
	}
	return c.codec.ReadEnvelope()
}

func (c *guestControl) awaitHello(ctx context.Context) error {
	env, err := c.recv()
	if err != nil {
		return fmt.Errorf("read guest hello: %w", err)
	}
	if env.Type != vmproto.TypeHello {
		return unexpectedGuestControlFrame("await_guest_hello", env.Type, vmproto.TypeHello)
	}
	if _, err := vmproto.DecodePayload[vmproto.Hello](env); err != nil {
		return guestProtocolError("await_guest_hello", "decode hello payload: %v", err)
	}
	return nil
}

func (c *guestControl) initLease(ctx context.Context, leaseID string, network vmproto.NetworkConfig) error {
	_, endSpan := startStepSpan(ctx, "vmorchestrator.guest.lease_init",
		attribute.String("lease.id", leaseID),
	)
	err := c.send(vmproto.TypeLeaseInit, vmproto.LeaseInit{
		LeaseID:             leaseID,
		Network:             network,
		HostWallclockUnixNS: time.Now().UnixNano(),
		ProtocolVersion:     vmproto.ProtocolVersion,
	})
	endSpan(err)
	if err != nil {
		return fmt.Errorf("send lease init: %w", err)
	}
	return nil
}

func (c *guestControl) exec(ctx context.Context, leaseID string, spec ExecSpec, handleCheckpoint checkpointHandler, logger *slog.Logger) (ExecResult, error) {
	execID := strings.TrimSpace(spec.Env["FORGE_METAL_EXEC_ID"])
	if execID == "" {
		return ExecResult{}, fmt.Errorf("FORGE_METAL_EXEC_ID is required")
	}
	var logBuf strings.Builder
	var firstByteAt time.Time
	startedAt := time.Time{}

	c.activeExecID = execID
	defer func() { c.activeExecID = "" }()

	_, endExecRequestSpan := startStepSpan(ctx, "vmorchestrator.guest.exec_request",
		attribute.String("lease.id", leaseID),
		attribute.String("exec.id", execID),
	)
	if err := c.send(vmproto.TypeExecRequest, vmproto.ExecRequest{
		LeaseID:         leaseID,
		ExecID:          execID,
		Argv:            cloneStringSlice(spec.Argv),
		WorkingDir:      spec.WorkingDir,
		Env:             cloneStringMap(spec.Env),
		MaxWallSeconds:  spec.MaxWallSeconds,
		ProtocolVersion: vmproto.ProtocolVersion,
	}); err != nil {
		endExecRequestSpan(err)
		return ExecResult{}, fmt.Errorf("send exec request: %w", err)
	}
	endExecRequestSpan(nil)

	for {
		env, err := c.recv()
		if err != nil {
			return ExecResult{StartedAt: startedAt, FirstByteAt: firstByteAt, Output: logBuf.String()}, fmt.Errorf("read guest control stream: %w", err)
		}
		switch env.Type {
		case vmproto.TypeExecStarted:
			msg, err := vmproto.DecodePayload[vmproto.ExecStarted](env)
			if err != nil {
				return ExecResult{Output: logBuf.String()}, err
			}
			startedAt = time.Unix(0, msg.StartedUnixNS).UTC()
			if logger != nil {
				logger.InfoContext(ctx, "guest exec started", "lease_id", leaseID, "exec_id", execID)
			}
		case vmproto.TypeLogChunk:
			msg, err := vmproto.DecodePayload[vmproto.LogChunk](env)
			if err != nil {
				return ExecResult{StartedAt: startedAt, FirstByteAt: firstByteAt, Output: logBuf.String()}, err
			}
			if firstByteAt.IsZero() {
				firstByteAt = time.Now().UTC()
			}
			appendLogChunk(&logBuf, msg.Data)
		case vmproto.TypeHeartbeat:
		case vmproto.TypeCheckpointRequest:
			req, err := vmproto.DecodePayload[vmproto.CheckpointRequest](env)
			if err != nil {
				return ExecResult{StartedAt: startedAt, FirstByteAt: firstByteAt, Output: logBuf.String()}, err
			}
			resp := vmproto.CheckpointResponse{
				RequestID: req.RequestID,
				Operation: req.Operation,
				Ref:       req.Ref,
				Accepted:  false,
				Error:     "checkpoint requests are not enabled",
			}
			if handleCheckpoint != nil {
				resp = handleCheckpoint(req)
			}
			if err := c.send(vmproto.TypeCheckpointResponse, resp); err != nil {
				return ExecResult{StartedAt: startedAt, FirstByteAt: firstByteAt, Output: logBuf.String()}, fmt.Errorf("send checkpoint response: %w", err)
			}
		case vmproto.TypeFatal:
			msg, decodeErr := vmproto.DecodePayload[vmproto.Fatal](env)
			if decodeErr != nil {
				return ExecResult{StartedAt: startedAt, FirstByteAt: firstByteAt, Output: logBuf.String()}, decodeErr
			}
			return ExecResult{StartedAt: startedAt, FirstByteAt: firstByteAt, Output: logBuf.String()}, fmt.Errorf("guest fatal: %s", strings.TrimSpace(msg.Message))
		case vmproto.TypeExecResult:
			if env.Seq == 0 {
				return ExecResult{StartedAt: startedAt, FirstByteAt: firstByteAt, Output: logBuf.String()}, guestProtocolError("await_exec_result", "result seq must be non-zero")
			}
			msg, err := vmproto.DecodePayload[vmproto.ExecResult](env)
			if err != nil {
				return ExecResult{StartedAt: startedAt, FirstByteAt: firstByteAt, Output: logBuf.String()}, err
			}
			if err := c.send(vmproto.TypeAck, vmproto.Ack{ForType: vmproto.TypeExecResult, ForSeq: env.Seq}); err != nil {
				return ExecResult{StartedAt: startedAt, FirstByteAt: firstByteAt, Output: logBuf.String()}, fmt.Errorf("ack guest exec result: %w", err)
			}
			if startedAt.IsZero() {
				startedAt = time.Now().UTC().Add(-time.Duration(msg.DurationMS) * time.Millisecond)
			}
			exitedAt := time.Now().UTC()
			return ExecResult{
				ExitCode:        msg.ExitCode,
				Output:          logBuf.String(),
				Duration:        time.Duration(msg.DurationMS) * time.Millisecond,
				StartedAt:       startedAt,
				FirstByteAt:     firstByteAt,
				ExitedAt:        exitedAt,
				StdoutBytes:     msg.StdoutBytes,
				StderrBytes:     msg.StderrBytes,
				DroppedLogBytes: msg.DroppedLogBytes,
			}, nil
		default:
			return ExecResult{StartedAt: startedAt, FirstByteAt: firstByteAt, Output: logBuf.String()}, unexpectedGuestControlFrame(
				"await_guest_exec_events",
				env.Type,
				vmproto.TypeExecStarted,
				vmproto.TypeLogChunk,
				vmproto.TypeHeartbeat,
				vmproto.TypeCheckpointRequest,
				vmproto.TypeFatal,
				vmproto.TypeExecResult,
			)
		}
	}
}

func (c *guestControl) cancelExec(execID, reason string) error {
	return c.send(vmproto.TypeCancel, vmproto.Cancel{ExecID: execID, Reason: reason})
}

func (c *guestControl) shutdown() error {
	return c.send(vmproto.TypeShutdown, vmproto.Shutdown{})
}

func unexpectedGuestControlFrame(state string, got vmproto.MessageType, expected ...vmproto.MessageType) error {
	allowed := make([]string, 0, len(expected))
	for _, msgType := range expected {
		allowed = append(allowed, string(msgType))
	}
	return guestProtocolError(state, "unexpected guest message type %s (expected one of: %s)", got, strings.Join(allowed, ", "))
}

func guestProtocolError(state string, format string, args ...any) error {
	return fmt.Errorf("guest control protocol violation in %s: %s", state, fmt.Sprintf(format, args...))
}

func appendLogChunk(dst *strings.Builder, data []byte) {
	if len(data) == 0 {
		return
	}
	remaining := maxBufferedGuestLogs - dst.Len()
	if remaining <= 0 {
		return
	}
	if len(data) > remaining {
		data = data[:remaining]
	}
	dst.Write(data)
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
