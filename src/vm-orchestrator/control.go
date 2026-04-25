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

	"github.com/verself/vm-orchestrator/vmproto"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	guestBridgeDialTimeout = 250 * time.Millisecond
	guestBridgeAckTimeout  = 500 * time.Millisecond
	guestBridgeRetryDelay  = 50 * time.Millisecond
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

func connectGuestControl(ctx context.Context, udsPath string, port int, leaseID string) (*guestControl, error) {
	conn, reader, err := connectGuestBridge(ctx, udsPath, port, leaseID)
	if err != nil {
		return nil, err
	}
	return &guestControl{conn: conn, reader: reader, codec: vmproto.NewCodec(reader, conn)}, nil
}

func connectGuestBridge(ctx context.Context, udsPath string, port int, leaseID string) (net.Conn, *bufio.Reader, error) {
	connectCtx, connectSpan := tracer.Start(ctx, "vmorchestrator.guest.vsock_proxy_connect",
		trace.WithAttributes(
			attribute.String("lease.id", leaseID),
			attribute.String("socket.path", udsPath),
			attribute.Int("guest.port", port),
		),
	)
	attempts := 0
	var lastErr error
	var lastRetryErr error
	defer func() {
		connectSpan.SetAttributes(attribute.Int("guest.vsock.attempts", attempts))
		if lastRetryErr != nil {
			connectSpan.SetAttributes(attribute.String("guest.vsock.last_retry_error", lastRetryErr.Error()))
		}
		if lastErr != nil {
			connectSpan.SetAttributes(attribute.String("guest.vsock.last_error", lastErr.Error()))
		}
		connectSpan.End()
	}()

	for {
		attempts++
		attemptCtx, attemptSpan := tracer.Start(connectCtx, "vmorchestrator.guest.vsock_proxy_connect_attempt",
			trace.WithAttributes(
				attribute.String("socket.path", udsPath),
				attribute.String("lease.id", leaseID),
				attribute.Int("guest.port", port),
				attribute.Int("guest.vsock.attempt", attempts),
			),
		)

		_, dialSpan := tracer.Start(attemptCtx, "vmorchestrator.guest.vsock_proxy_unix_dial",
			trace.WithAttributes(
				attribute.String("socket.path", udsPath),
				attribute.String("lease.id", leaseID),
				attribute.Int("guest.port", port),
				attribute.Int("guest.vsock.attempt", attempts),
				attribute.Int64("guest.vsock.dial_timeout_ms", guestBridgeDialTimeout.Milliseconds()),
			),
		)
		conn, err := net.DialTimeout("unix", udsPath, guestBridgeDialTimeout)
		endGuestBridgeSpan(dialSpan, err)
		if err != nil {
			lastErr = err
			lastRetryErr = err
			attemptSpan.SetAttributes(
				attribute.String("guest.vsock.attempt_result", "retry"),
				attribute.String("guest.vsock.retry_stage", "unix_dial"),
				attribute.String("guest.vsock.retry_error", err.Error()),
			)
			attemptSpan.End()
			select {
			case <-ctx.Done():
				lastErr = ctx.Err()
				connectSpan.SetStatus(codes.Error, ctx.Err().Error())
				return nil, nil, fmt.Errorf("dial guest bridge %s: %w", udsPath, ctx.Err())
			default:
			}
			if err := traceGuestBridgeRetrySleep(connectCtx, leaseID, attempts, port, "unix_dial"); err != nil {
				lastErr = err
				connectSpan.SetStatus(codes.Error, err.Error())
				return nil, nil, fmt.Errorf("dial guest bridge %s: %w", udsPath, err)
			}
			continue
		}
		_, writeSpan := tracer.Start(attemptCtx, "vmorchestrator.guest.vsock_proxy_connect_command_write",
			trace.WithAttributes(
				attribute.String("socket.path", udsPath),
				attribute.String("lease.id", leaseID),
				attribute.Int("guest.port", port),
				attribute.Int("guest.vsock.attempt", attempts),
			),
		)
		if _, err := io.WriteString(conn, fmt.Sprintf("CONNECT %d\n", port)); err != nil {
			endGuestBridgeSpan(writeSpan, err)
			conn.Close()
			lastErr = err
			lastRetryErr = err
			attemptSpan.SetAttributes(
				attribute.String("guest.vsock.attempt_result", "retry"),
				attribute.String("guest.vsock.retry_stage", "connect_command_write"),
				attribute.String("guest.vsock.retry_error", err.Error()),
			)
			attemptSpan.End()
			select {
			case <-ctx.Done():
				lastErr = ctx.Err()
				connectSpan.SetStatus(codes.Error, ctx.Err().Error())
				return nil, nil, fmt.Errorf("write guest bridge connect command: %w", ctx.Err())
			default:
			}
			if err := traceGuestBridgeRetrySleep(connectCtx, leaseID, attempts, port, "connect_command_write"); err != nil {
				lastErr = err
				connectSpan.SetStatus(codes.Error, err.Error())
				return nil, nil, fmt.Errorf("write guest bridge connect command: %w", err)
			}
			continue
		}
		endGuestBridgeSpan(writeSpan, nil)
		reader := bufio.NewReader(conn)
		_, ackSpan := tracer.Start(attemptCtx, "vmorchestrator.guest.vsock_proxy_ack_read",
			trace.WithAttributes(
				attribute.String("socket.path", udsPath),
				attribute.String("lease.id", leaseID),
				attribute.Int("guest.port", port),
				attribute.Int("guest.vsock.attempt", attempts),
				attribute.Int64("guest.vsock.ack_timeout_ms", guestBridgeAckTimeout.Milliseconds()),
			),
		)
		// The vsock proxy can accept the Unix socket before the guest port is listening, so ACK reads must be bounded.
		if err := conn.SetReadDeadline(time.Now().Add(guestBridgeAckTimeout)); err != nil {
			endGuestBridgeSpan(ackSpan, err)
			conn.Close()
			lastErr = err
			lastRetryErr = err
			attemptSpan.SetAttributes(
				attribute.String("guest.vsock.attempt_result", "retry"),
				attribute.String("guest.vsock.retry_stage", "ack_deadline_set"),
				attribute.String("guest.vsock.retry_error", err.Error()),
			)
			attemptSpan.End()
			select {
			case <-ctx.Done():
				lastErr = ctx.Err()
				connectSpan.SetStatus(codes.Error, ctx.Err().Error())
				return nil, nil, fmt.Errorf("set guest bridge ack deadline: %w", ctx.Err())
			default:
			}
			if err := traceGuestBridgeRetrySleep(connectCtx, leaseID, attempts, port, "ack_deadline_set"); err != nil {
				lastErr = err
				connectSpan.SetStatus(codes.Error, err.Error())
				return nil, nil, fmt.Errorf("set guest bridge ack deadline: %w", err)
			}
			continue
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			endGuestBridgeSpan(ackSpan, err)
			conn.Close()
			lastErr = err
			lastRetryErr = err
			attemptSpan.SetAttributes(
				attribute.String("guest.vsock.attempt_result", "retry"),
				attribute.String("guest.vsock.retry_stage", "ack_read"),
				attribute.String("guest.vsock.retry_error", err.Error()),
			)
			attemptSpan.End()
			select {
			case <-ctx.Done():
				lastErr = ctx.Err()
				connectSpan.SetStatus(codes.Error, ctx.Err().Error())
				return nil, nil, fmt.Errorf("read guest bridge ack: %w", ctx.Err())
			default:
			}
			if err := traceGuestBridgeRetrySleep(connectCtx, leaseID, attempts, port, "ack_read"); err != nil {
				lastErr = err
				connectSpan.SetStatus(codes.Error, err.Error())
				return nil, nil, fmt.Errorf("read guest bridge ack: %w", err)
			}
			continue
		}
		endGuestBridgeSpan(ackSpan, nil)
		if err := conn.SetReadDeadline(time.Time{}); err != nil {
			conn.Close()
			lastErr = err
			lastRetryErr = err
			attemptSpan.SetAttributes(
				attribute.String("guest.vsock.attempt_result", "retry"),
				attribute.String("guest.vsock.retry_stage", "ack_deadline_clear"),
				attribute.String("guest.vsock.retry_error", err.Error()),
			)
			attemptSpan.End()
			select {
			case <-ctx.Done():
				lastErr = ctx.Err()
				connectSpan.SetStatus(codes.Error, ctx.Err().Error())
				return nil, nil, fmt.Errorf("clear guest bridge ack deadline: %w", ctx.Err())
			default:
			}
			if err := traceGuestBridgeRetrySleep(connectCtx, leaseID, attempts, port, "ack_deadline_clear"); err != nil {
				lastErr = err
				connectSpan.SetStatus(codes.Error, err.Error())
				return nil, nil, fmt.Errorf("clear guest bridge ack deadline: %w", err)
			}
			continue
		}
		ack := strings.TrimSpace(line)
		if !strings.HasPrefix(ack, "OK ") {
			conn.Close()
			err := fmt.Errorf("unexpected guest bridge ack %q", ack)
			lastErr = err
			attemptSpan.SetAttributes(
				attribute.String("guest.vsock.attempt_result", "terminal_error"),
				attribute.String("guest.vsock.ack", ack),
			)
			attemptSpan.RecordError(err)
			attemptSpan.SetStatus(codes.Error, err.Error())
			attemptSpan.End()
			connectSpan.SetStatus(codes.Error, err.Error())
			return nil, nil, err
		}
		attemptSpan.SetAttributes(
			attribute.String("guest.vsock.attempt_result", "success"),
			attribute.String("guest.vsock.ack", ack),
		)
		attemptSpan.End()
		lastErr = nil
		connectSpan.SetAttributes(attribute.Bool("guest.vsock.connected", true))
		return conn, reader, nil
	}
}

func traceGuestBridgeRetrySleep(ctx context.Context, leaseID string, attempt, port int, stage string) error {
	_, sleepSpan := tracer.Start(ctx, "vmorchestrator.guest.vsock_proxy_retry_sleep",
		trace.WithAttributes(
			attribute.String("lease.id", leaseID),
			attribute.Int("guest.port", port),
			attribute.Int("guest.vsock.attempt", attempt),
			attribute.String("guest.vsock.retry_stage", stage),
			attribute.Int64("guest.vsock.retry_sleep_ms", guestBridgeRetryDelay.Milliseconds()),
		),
	)
	defer sleepSpan.End()
	select {
	case <-ctx.Done():
		sleepSpan.SetAttributes(attribute.Bool("guest.vsock.retry_sleep_interrupted", true))
		return ctx.Err()
	case <-time.After(guestBridgeRetryDelay):
		return nil
	}
}

func endGuestBridgeSpan(span trace.Span, err error) {
	if err != nil {
		span.SetAttributes(attribute.String("guest.vsock.error", err.Error()))
	}
	span.End()
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

func (c *guestControl) awaitHello(ctx context.Context) (vmproto.Hello, error) {
	env, err := c.recv()
	if err != nil {
		return vmproto.Hello{}, fmt.Errorf("read guest hello: %w", err)
	}
	if env.Type != vmproto.TypeHello {
		return vmproto.Hello{}, unexpectedGuestControlFrame("await_guest_hello", env.Type, vmproto.TypeHello)
	}
	hello, err := vmproto.DecodePayload[vmproto.Hello](env)
	if err != nil {
		return vmproto.Hello{}, guestProtocolError("await_guest_hello", "decode hello payload: %v", err)
	}
	return hello, nil
}

func (c *guestControl) initLease(ctx context.Context, leaseID string, network vmproto.NetworkConfig, filesystems []vmproto.FilesystemMount) error {
	_, endSpan := startStepSpan(ctx, "vmorchestrator.guest.lease_init",
		attribute.String("lease.id", leaseID),
		attribute.Int("filesystem.mount_count", len(filesystems)),
	)
	err := c.send(vmproto.TypeLeaseInit, vmproto.LeaseInit{
		LeaseID:             leaseID,
		Network:             network,
		Filesystems:         filesystems,
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
	execID := strings.TrimSpace(spec.Env["VERSELF_EXEC_ID"])
	if execID == "" {
		return ExecResult{}, fmt.Errorf("VERSELF_EXEC_ID is required")
	}
	var logBuf strings.Builder
	var firstByteAt time.Time
	startedAt := time.Time{}

	c.activeExecID = execID
	defer func() { c.activeExecID = "" }()

	// exec_dispatch measures the host-side send of the ExecRequest envelope.
	// It's typically sub-millisecond; the interesting latency lives in
	// exec_workload (customer code) and exec_teardown (result drain + ack).
	dispatchCtx, endExecDispatchSpan := startStepSpan(ctx, "vmorchestrator.guest.exec_dispatch",
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
		endExecDispatchSpan(err)
		return ExecResult{}, fmt.Errorf("send exec request: %w", err)
	}
	endExecDispatchSpan(nil)
	_ = dispatchCtx

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
			resultReceivedAt := time.Now().UTC()
			// exec_teardown wraps the ack send + result drain + return. Covered
			// as a real span (not synthetic) because the ack write is on the
			// critical path and we want failures visible in-trace.
			teardownCtx, endExecTeardownSpan := startStepSpan(ctx, "vmorchestrator.guest.exec_teardown",
				attribute.String("lease.id", leaseID),
				attribute.String("exec.id", execID),
			)
			_ = teardownCtx
			if err := c.send(vmproto.TypeAck, vmproto.Ack{ForType: vmproto.TypeExecResult, ForSeq: env.Seq}); err != nil {
				endExecTeardownSpan(err)
				return ExecResult{StartedAt: startedAt, FirstByteAt: firstByteAt, Output: logBuf.String()}, fmt.Errorf("ack guest exec result: %w", err)
			}
			if startedAt.IsZero() {
				startedAt = resultReceivedAt.Add(-time.Duration(msg.DurationMS) * time.Millisecond)
			}
			exitedAt := time.Now().UTC()
			// exec_workload is the customer-code window. Anchored from the
			// guest-reported startedAt (via ExecStarted) and ending at
			// resultReceivedAt (when ExecResult hit the host). Dashboards
			// plot this phase as "customer workload time" — independent of
			// host-side orchestration overhead.
			workloadEnd := resultReceivedAt
			if !startedAt.IsZero() && startedAt.Before(workloadEnd) {
				recordObservedIntervalSpan(ctx, "vmorchestrator.guest.exec_workload", startedAt, workloadEnd,
					attribute.String("lease.id", leaseID),
					attribute.String("exec.id", execID),
					attribute.Int64("exec.duration_ms", msg.DurationMS),
					attribute.Int("exec.exit_code", msg.ExitCode),
				)
			}
			endExecTeardownSpan(nil)
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

func (c *guestControl) sealFilesystem(ctx context.Context, leaseID, mountName, mountPath string) error {
	if c == nil {
		return fmt.Errorf("guest control is not available")
	}
	if err := c.send(vmproto.TypeFilesystemSealRequest, vmproto.FilesystemSealRequest{
		LeaseID:         leaseID,
		Name:            mountName,
		ProtocolVersion: vmproto.ProtocolVersion,
	}); err != nil {
		return fmt.Errorf("send filesystem seal request: %w", err)
	}
	for {
		env, err := c.recv()
		if err != nil {
			return fmt.Errorf("read filesystem seal result: %w", err)
		}
		switch env.Type {
		case vmproto.TypeFilesystemSealResult:
			msg, err := vmproto.DecodePayload[vmproto.FilesystemSealResult](env)
			if err != nil {
				return err
			}
			if msg.LeaseID != leaseID || msg.Name != mountName {
				return guestProtocolError("await_filesystem_seal_result", "result mismatch lease=%s mount=%s", msg.LeaseID, msg.Name)
			}
			if !msg.Sealed {
				if strings.TrimSpace(msg.Error) == "" {
					return fmt.Errorf("guest failed to seal filesystem %s at %s", mountName, mountPath)
				}
				return fmt.Errorf("guest failed to seal filesystem %s at %s: %s", mountName, mountPath, strings.TrimSpace(msg.Error))
			}
			return nil
		case vmproto.TypeHeartbeat:
			continue
		case vmproto.TypeFatal:
			msg, decodeErr := vmproto.DecodePayload[vmproto.Fatal](env)
			if decodeErr != nil {
				return decodeErr
			}
			return fmt.Errorf("guest fatal: %s", strings.TrimSpace(msg.Message))
		default:
			return unexpectedGuestControlFrame("await_filesystem_seal_result", env.Type, vmproto.TypeFilesystemSealResult, vmproto.TypeHeartbeat, vmproto.TypeFatal)
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
