package fastsandbox

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
)

// SocketPrivOps delegates privileged operations to the homestead-smelter-host
// daemon via its AF_UNIX SEQPACKET ops socket. Each method encodes a request,
// sends it, and blocks for the synchronous response.
type SocketPrivOps struct {
	SocketPath string

	mu        sync.Mutex
	conn      net.Conn
	requestID atomic.Uint64
}

func (s *SocketPrivOps) connect() (net.Conn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.conn != nil {
		return s.conn, nil
	}

	conn, err := net.Dial("unixpacket", s.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("connect ops socket %s: %w", s.SocketPath, err)
	}
	s.conn = conn
	return conn, nil
}

func (s *SocketPrivOps) roundTrip(op opsOpCode, payload []byte) ([]byte, error) {
	reqID := s.requestID.Add(1)
	var sendBuf [opsMaxMessage]byte
	n := opsEncodeRequest(sendBuf[:], op, reqID, payload)

	for attempt := 0; attempt < 2; attempt++ {
		conn, err := s.connect()
		if err != nil {
			return nil, err
		}

		if _, err := conn.Write(sendBuf[:n]); err != nil {
			s.resetConn()
			if attempt == 0 {
				continue
			}
			return nil, fmt.Errorf("ops send: %w", err)
		}

		var recvBuf [opsMaxMessage]byte
		rn, err := conn.Read(recvBuf[:])
		if err != nil {
			s.resetConn()
			return nil, fmt.Errorf("ops recv: %w", err)
		}

		status, respID, respPayload, err := opsDecodeResponse(recvBuf[:rn])
		if err != nil {
			return nil, err
		}
		if respID != reqID {
			return nil, fmt.Errorf("ops response id mismatch: got %d, want %d", respID, reqID)
		}
		if status != opsStatusOK {
			return nil, fmt.Errorf("ops error (status %d): %s", status, string(respPayload))
		}

		// Return a copy so callers aren't aliasing the recv buffer.
		result := make([]byte, len(respPayload))
		copy(result, respPayload)
		return result, nil
	}

	return nil, fmt.Errorf("ops send: retries exhausted")
}

func (s *SocketPrivOps) resetConn() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
}

func (s *SocketPrivOps) ZFSClone(_ context.Context, snapshot, target, jobID string) error {
	var b opsPayloadBuilder
	b.writeString(snapshot)
	b.writeString(target)
	b.writeString(jobID)
	_, err := s.roundTrip(opZFSClone, b.bytes())
	return err
}

func (s *SocketPrivOps) ZFSDestroy(_ context.Context, dataset string) error {
	var b opsPayloadBuilder
	b.writeString(dataset)
	_, err := s.roundTrip(opZFSDestroy, b.bytes())
	return err
}

func (s *SocketPrivOps) TapCreate(_ context.Context, tapName, hostCIDR string) error {
	var b opsPayloadBuilder
	b.writeString(tapName)
	b.writeString(hostCIDR)
	_, err := s.roundTrip(opTapCreate, b.bytes())
	return err
}

func (s *SocketPrivOps) TapUp(_ context.Context, tapName string) error {
	var b opsPayloadBuilder
	b.writeString(tapName)
	_, err := s.roundTrip(opTapUp, b.bytes())
	return err
}

func (s *SocketPrivOps) TapDelete(_ context.Context, tapName string) error {
	var b opsPayloadBuilder
	b.writeString(tapName)
	_, err := s.roundTrip(opTapDelete, b.bytes())
	return err
}

func (s *SocketPrivOps) SetupJail(_ context.Context, jailRoot, zvolDev, kernelSrc string, uid, gid int) error {
	var b opsPayloadBuilder
	b.writeString(jailRoot)
	b.writeString(zvolDev)
	b.writeString(kernelSrc)
	b.writeU32(uint32(uid))
	b.writeU32(uint32(gid))
	_, err := s.roundTrip(opSetupJail, b.bytes())
	return err
}

func (s *SocketPrivOps) StartJailer(_ context.Context, jobID string, cfg JailerConfig) (*JailerProcess, error) {
	var b opsPayloadBuilder
	b.writeString(jobID)
	b.writeString(cfg.FirecrackerBin)
	b.writeString(cfg.JailerBin)
	b.writeString(cfg.ChrootBaseDir)
	b.writeU32(uint32(cfg.UID))
	b.writeU32(uint32(cfg.GID))

	resp, err := s.roundTrip(opStartJailer, b.bytes())
	if err != nil {
		return nil, err
	}
	if len(resp) < 4 {
		return nil, fmt.Errorf("ops start_jailer: response too short")
	}
	pid := int(binary.LittleEndian.Uint32(resp[:4]))

	proc, findErr := os.FindProcess(pid)
	if findErr != nil {
		return nil, fmt.Errorf("ops start_jailer: find process %d: %w", pid, findErr)
	}

	return &JailerProcess{
		Pid:    pid,
		Stdout: nil, // Serial logs not available via socket path.
		Stderr: nil,
		waitFn: func() error {
			// The jailer is a child of the smelter daemon, not this process,
			// so waitpid won't work. Poll with kill(0) until the process exits.
			// This is only used as a fallback — the orchestrator's primary
			// completion signal comes from the vsock control channel, not Wait().
			// NOTE: PID reuse could cause a false "still alive" — acceptable
			// because the orchestrator has a 15s timeout before force-kill.
			for {
				if err := proc.Signal(syscall.Signal(0)); err != nil {
					return nil
				}
				syscall.Nanosleep(&syscall.Timespec{Nsec: 200_000_000}, nil) // 200ms
			}
		},
		killFn: func() error {
			return proc.Signal(syscall.SIGKILL)
		},
	}, nil
}

func (s *SocketPrivOps) Chmod(_ context.Context, path string, mode uint32) error {
	var b opsPayloadBuilder
	b.writeString(path)
	b.writeU32(mode)
	_, err := s.roundTrip(opChmod, b.bytes())
	return err
}
