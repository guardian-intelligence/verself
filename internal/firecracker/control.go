package firecracker

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

	"github.com/forge-metal/forge-metal/internal/vmproto"
)

const maxBufferedGuestLogs = 10 * 1024 * 1024

type guestControlResult struct {
	hello  vmproto.Hello
	result vmproto.Result
	logs   string
}

type guestControl struct {
	conn   net.Conn
	reader io.Reader
	codec  *vmproto.Codec

	mu  sync.Mutex
	seq uint64
}

func connectGuestControl(ctx context.Context, udsPath string, port int) (*guestControl, error) {
	for {
		conn, err := net.DialTimeout("unix", udsPath, 250*time.Millisecond)
		if err != nil {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("dial guest control %s: %w", udsPath, ctx.Err())
			case <-time.After(50 * time.Millisecond):
				continue
			}
		}

		reader := bufio.NewReader(conn)
		if _, err := io.WriteString(conn, fmt.Sprintf("CONNECT %d\n", port)); err != nil {
			conn.Close()
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("write guest control connect command: %w", ctx.Err())
			case <-time.After(50 * time.Millisecond):
				continue
			}
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			conn.Close()
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("read guest control ack: %w", ctx.Err())
			case <-time.After(50 * time.Millisecond):
				continue
			}
		}
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "OK ") {
			conn.Close()
			return nil, fmt.Errorf("unexpected guest control ack %q", line)
		}

		return &guestControl{
			conn:   conn,
			reader: reader,
			codec:  vmproto.NewCodec(reader, conn),
		}, nil
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

func (c *guestControl) run(job JobConfig, lease NetworkLease, logger *slog.Logger) (guestControlResult, error) {
	var (
		logBuf   strings.Builder
		hello    vmproto.Hello
		gotHello bool
	)

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
		PrepareCommand:      cloneStringSlice(job.PrepareCommand),
		PrepareWorkDir:      job.PrepareWorkDir,
		RunCommand:          cloneStringSlice(job.RunCommand),
		RunWorkDir:          job.RunWorkDir,
		Services:            cloneStringSlice(job.Services),
		Env:                 cloneStringMap(job.Env),
		Network:             lease.GuestNetworkConfig(),
		HostWallclockUnixNS: time.Now().UnixNano(),
		ProtocolVersion:     vmproto.ProtocolVersion,
	}); err != nil {
		return guestControlResult{}, fmt.Errorf("send run request: %w", err)
	}

	for {
		env, err := c.recv()
		if err != nil {
			if gotHello {
				return guestControlResult{
					hello: hello,
					logs:  logBuf.String(),
				}, fmt.Errorf("read guest control stream: %w", err)
			}
			return guestControlResult{}, fmt.Errorf("read guest control stream: %w", err)
		}

		switch env.Type {
		case vmproto.TypeLogChunk:
			msg, err := vmproto.DecodePayload[vmproto.LogChunk](env)
			if err != nil {
				return guestControlResult{hello: hello, logs: logBuf.String()}, err
			}
			appendLogChunk(&logBuf, msg.Data)
		case vmproto.TypeHeartbeat:
		case vmproto.TypePhaseStart:
			if logger != nil {
				if msg, err := vmproto.DecodePayload[vmproto.PhaseStart](env); err == nil {
					logger.Info("guest phase start", "phase", msg.Name, "job_id", job.JobID)
				}
			}
		case vmproto.TypePhaseEnd:
			if logger != nil {
				if msg, err := vmproto.DecodePayload[vmproto.PhaseEnd](env); err == nil {
					logger.Info("guest phase end", "phase", msg.Name, "exit_code", msg.ExitCode, "duration_ms", msg.DurationMS, "job_id", job.JobID)
				}
			}
		case vmproto.TypeFatal:
			msg, decodeErr := vmproto.DecodePayload[vmproto.Fatal](env)
			if decodeErr != nil {
				return guestControlResult{hello: hello, logs: logBuf.String()}, decodeErr
			}
			return guestControlResult{hello: hello, logs: logBuf.String()}, fmt.Errorf("guest fatal: %s", strings.TrimSpace(msg.Message))
		case vmproto.TypeResult:
			msg, err := vmproto.DecodePayload[vmproto.Result](env)
			if err != nil {
				return guestControlResult{hello: hello, logs: logBuf.String()}, err
			}
			if err := c.send(vmproto.TypeAck, vmproto.Ack{ForType: vmproto.TypeResult, ForSeq: env.Seq}); err != nil {
				return guestControlResult{hello: hello, logs: logBuf.String(), result: msg}, fmt.Errorf("ack guest result: %w", err)
			}
			if logger != nil {
				logger.Info("guest result received; requesting graceful guest reboot", "job_id", job.JobID, "exit_code", msg.ExitCode)
			}
			if err := c.send(vmproto.TypeShutdown, vmproto.Shutdown{}); err != nil {
				return guestControlResult{hello: hello, logs: logBuf.String(), result: msg}, fmt.Errorf("send guest shutdown: %w", err)
			}
			return guestControlResult{
				hello:  hello,
				result: msg,
				logs:   logBuf.String(),
			}, nil
		default:
			return guestControlResult{hello: hello, logs: logBuf.String()}, fmt.Errorf("unexpected guest message type %s", env.Type)
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
