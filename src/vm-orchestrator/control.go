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
)

const maxBufferedGuestLogs = 10 * 1024 * 1024

type guestControlResult struct {
	hello  vmproto.Hello
	result vmproto.Result
	logs   string
	phases []PhaseResult
}

type guestControl struct {
	conn   net.Conn
	reader io.Reader
	codec  *vmproto.Codec

	mu  sync.Mutex
	seq uint64
}

type checkpointHandler func(vmproto.CheckpointRequest) vmproto.CheckpointResponse

func connectGuestControl(ctx context.Context, udsPath string, port int) (*guestControl, error) {
	conn, reader, err := connectGuestBridge(ctx, udsPath, port)
	if err != nil {
		return nil, err
	}

	return &guestControl{
		conn:   conn,
		reader: reader,
		codec:  vmproto.NewCodec(reader, conn),
	}, nil
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
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "OK ") {
			conn.Close()
			return nil, nil, fmt.Errorf("unexpected guest bridge ack %q", line)
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

func (c *guestControl) run(job JobConfig, lease NetworkLease, hostServiceIP string, hostServicePort int, handleCheckpoint checkpointHandler, logger *slog.Logger, observer RunObserver) (guestControlResult, error) {
	var (
		logBuf       strings.Builder
		hello        vmproto.Hello
		gotHello     bool
		phaseResults []PhaseResult
	)
	observer = normalizeRunObserver(observer)

	resultWithLogs := func() guestControlResult {
		return guestControlResult{
			hello:  hello,
			logs:   logBuf.String(),
			phases: append([]PhaseResult(nil), phaseResults...),
		}
	}

	env, err := c.recv()
	if err != nil {
		return guestControlResult{}, fmt.Errorf("read guest hello: %w", err)
	}
	if env.Type != vmproto.TypeHello {
		return guestControlResult{}, fmt.Errorf("expected guest hello, got %s", env.Type)
	}
	hello, err = vmproto.DecodePayload[vmproto.Hello](env)
	if err != nil {
		return guestControlResult{}, err
	}
	gotHello = true

	if err := c.send(vmproto.TypeRunRequest, vmproto.RunRequest{
		JobID:               job.JobID,
		RunCommand:          cloneStringSlice(job.RunCommand),
		RunWorkDir:          job.RunWorkDir,
		Env:                 cloneStringMap(job.Env),
		Network:             lease.GuestNetworkConfig(hostServiceIP, hostServicePort),
		HostWallclockUnixNS: time.Now().UnixNano(),
		ProtocolVersion:     vmproto.ProtocolVersion,
	}); err != nil {
		return guestControlResult{}, fmt.Errorf("send run request: %w", err)
	}

	for {
		env, err := c.recv()
		if err != nil {
			if gotHello {
				return resultWithLogs(), fmt.Errorf("read guest control stream: %w", err)
			}
			return guestControlResult{}, fmt.Errorf("read guest control stream: %w", err)
		}

		switch env.Type {
		case vmproto.TypeLogChunk:
			msg, err := vmproto.DecodePayload[vmproto.LogChunk](env)
			if err != nil {
				return resultWithLogs(), err
			}
			appendLogChunk(&logBuf, msg.Data)
			observer.OnGuestLogChunk(job.JobID, string(msg.Data))
		case vmproto.TypeHeartbeat:
		case vmproto.TypeCheckpointRequest:
			req, err := vmproto.DecodePayload[vmproto.CheckpointRequest](env)
			if err != nil {
				return resultWithLogs(), err
			}
			var resp vmproto.CheckpointResponse
			if handleCheckpoint == nil {
				resp = vmproto.CheckpointResponse{
					RequestID: req.RequestID,
					Operation: req.Operation,
					Ref:       req.Ref,
					Accepted:  false,
					Error:     "checkpoint requests are not enabled for this VM",
				}
			} else {
				resp = handleCheckpoint(req)
			}
			if err := c.send(vmproto.TypeCheckpointResponse, resp); err != nil {
				return resultWithLogs(), fmt.Errorf("send checkpoint response: %w", err)
			}
		case vmproto.TypePhaseStart:
			if msg, err := vmproto.DecodePayload[vmproto.PhaseStart](env); err == nil {
				observer.OnGuestPhaseStart(job.JobID, msg.Name)
			}
			if logger != nil {
				if msg, err := vmproto.DecodePayload[vmproto.PhaseStart](env); err == nil {
					logger.Info("guest phase start", "phase", msg.Name, "job_id", job.JobID)
				}
			}
		case vmproto.TypePhaseEnd:
			msg, err := vmproto.DecodePayload[vmproto.PhaseEnd](env)
			if err != nil {
				return resultWithLogs(), err
			}
			phaseResults = append(phaseResults, PhaseResult{
				Name:       msg.Name,
				ExitCode:   msg.ExitCode,
				DurationMS: msg.DurationMS,
			})
			observer.OnGuestPhaseEnd(job.JobID, PhaseResult{
				Name:       msg.Name,
				ExitCode:   msg.ExitCode,
				DurationMS: msg.DurationMS,
			})
			if logger != nil {
				logger.Info("guest phase end", "phase", msg.Name, "exit_code", msg.ExitCode, "duration_ms", msg.DurationMS, "job_id", job.JobID)
			}
		case vmproto.TypeFatal:
			msg, decodeErr := vmproto.DecodePayload[vmproto.Fatal](env)
			if decodeErr != nil {
				return resultWithLogs(), decodeErr
			}
			return resultWithLogs(), fmt.Errorf("guest fatal: %s", strings.TrimSpace(msg.Message))
		case vmproto.TypeResult:
			msg, err := vmproto.DecodePayload[vmproto.Result](env)
			if err != nil {
				return resultWithLogs(), err
			}
			if err := c.send(vmproto.TypeAck, vmproto.Ack{ForType: vmproto.TypeResult, ForSeq: env.Seq}); err != nil {
				outcome := resultWithLogs()
				outcome.result = msg
				return outcome, fmt.Errorf("ack guest result: %w", err)
			}
			if logger != nil {
				logger.Info("guest result received; requesting graceful guest reboot", "job_id", job.JobID, "exit_code", msg.ExitCode)
			}
			if err := c.send(vmproto.TypeShutdown, vmproto.Shutdown{}); err != nil {
				outcome := resultWithLogs()
				outcome.result = msg
				return outcome, fmt.Errorf("send guest shutdown: %w", err)
			}
			outcome := resultWithLogs()
			outcome.result = msg
			return outcome, nil
		default:
			return resultWithLogs(), fmt.Errorf("unexpected guest message type %s", env.Type)
		}
	}
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
