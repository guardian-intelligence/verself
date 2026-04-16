package vmproto

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

const (
	ProtocolVersion = 5
	GuestPort       = 10789

	ControlQueueCapacity = 32
	LogQueueCapacity     = 256

	HeartbeatInterval = 5 * time.Second
	HeartbeatTimeout  = 30 * time.Second
	CancelGracePeriod = 5 * time.Second
	MaxFrameSize      = 1024 * 1024
)

var ErrFrameTooLarge = errors.New("frame too large")

type MessageType string

const (
	TypeHello                 MessageType = "hello"
	TypeLeaseInit             MessageType = "lease_init"
	TypeExecRequest           MessageType = "exec_request"
	TypeExecStarted           MessageType = "exec_started"
	TypeLogChunk              MessageType = "log_chunk"
	TypeHeartbeat             MessageType = "heartbeat"
	TypeExecResult            MessageType = "exec_result"
	TypeCheckpointRequest     MessageType = "checkpoint_request"
	TypeCheckpointResponse    MessageType = "checkpoint_response"
	TypeFilesystemSealRequest MessageType = "filesystem_seal_request"
	TypeFilesystemSealResult  MessageType = "filesystem_seal_result"
	TypeFatal                 MessageType = "fatal"
	TypeCancel                MessageType = "cancel"
	TypeAck                   MessageType = "ack"
	TypeShutdown              MessageType = "shutdown"
)

const (
	CheckpointOperationSave = "save"
	MaxCheckpointRefLen     = 128
)

var checkpointRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type Envelope struct {
	Version int             `json:"v"`
	Type    MessageType     `json:"type"`
	Seq     uint64          `json:"seq,omitempty"`
	MonoNS  int64           `json:"mono_ns,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Hello struct {
	BootToReadyMS int64             `json:"boot_to_ready_ms"`
	BootTimings   *GuestBootTimings `json:"boot_timings,omitempty"`
}

type GuestBootTimings struct {
	KernelBootToInitStartMS         int64 `json:"kernel_boot_to_init_start_ms,omitempty"`
	KernelBootToVSockListenDoneMS   int64 `json:"kernel_boot_to_vsock_listen_done_ms,omitempty"`
	KernelBootToVSockAcceptDoneMS   int64 `json:"kernel_boot_to_vsock_accept_done_ms,omitempty"`
	KernelBootToHelloEnqueueStartMS int64 `json:"kernel_boot_to_hello_enqueue_start_ms,omitempty"`
	InitStartMS                     int64 `json:"init_start_ms"`
	MountVirtualFilesystemsStartMS  int64 `json:"mount_virtual_filesystems_start_ms"`
	MountVirtualFilesystemsDoneMS   int64 `json:"mount_virtual_filesystems_done_ms"`
	ConfigureLoopbackStartMS        int64 `json:"configure_loopback_start_ms"`
	ConfigureLoopbackDoneMS         int64 `json:"configure_loopback_done_ms"`
	SetSubreaperStartMS             int64 `json:"set_subreaper_start_ms"`
	SetSubreaperDoneMS              int64 `json:"set_subreaper_done_ms"`
	StartTelemetryStartMS           int64 `json:"start_telemetry_start_ms"`
	StartTelemetryDoneMS            int64 `json:"start_telemetry_done_ms"`
	SignalNotifyStartMS             int64 `json:"signal_notify_start_ms"`
	SignalNotifyDoneMS              int64 `json:"signal_notify_done_ms"`
	VSockListenStartMS              int64 `json:"vsock_listen_start_ms"`
	VSockListenDoneMS               int64 `json:"vsock_listen_done_ms"`
	VSockAcceptStartMS              int64 `json:"vsock_accept_start_ms"`
	VSockAcceptDoneMS               int64 `json:"vsock_accept_done_ms"`
	AgentStartMS                    int64 `json:"agent_start_ms"`
	AgentSessionReadyMS             int64 `json:"agent_session_ready_ms"`
	AgentIOLoopsStartedMS           int64 `json:"agent_io_loops_started_ms"`
	HelloEnqueueStartMS             int64 `json:"hello_enqueue_start_ms"`
	HelloEnqueueDoneMS              int64 `json:"hello_enqueue_done_ms"`
}

type NetworkConfig struct {
	AddressCIDR     string   `json:"address_cidr"`
	Gateway         string   `json:"gateway"`
	LinkName        string   `json:"link_name"`
	DNS             []string `json:"dns,omitempty"`
	HostServiceIP   string   `json:"host_service_ip,omitempty"`
	HostServicePort int      `json:"host_service_port,omitempty"`
}

type LeaseInit struct {
	LeaseID             string            `json:"lease_id"`
	Network             NetworkConfig     `json:"network"`
	Filesystems         []FilesystemMount `json:"filesystems,omitempty"`
	HostWallclockUnixNS int64             `json:"host_wallclock_unix_ns"`
	ProtocolVersion     int               `json:"protocol_version"`
}

type FilesystemMount struct {
	Name       string `json:"name"`
	DriveID    string `json:"drive_id"`
	DevicePath string `json:"device_path"`
	MountPath  string `json:"mount_path"`
	FSType     string `json:"fs_type"`
	ReadOnly   bool   `json:"read_only,omitempty"`
}

type ExecRequest struct {
	LeaseID         string            `json:"lease_id"`
	ExecID          string            `json:"exec_id"`
	Argv            []string          `json:"argv"`
	WorkingDir      string            `json:"working_dir,omitempty"`
	Env             map[string]string `json:"env,omitempty"`
	MaxWallSeconds  uint64            `json:"max_wall_seconds,omitempty"`
	ProtocolVersion int               `json:"protocol_version"`
}

type ExecStarted struct {
	LeaseID         string `json:"lease_id"`
	ExecID          string `json:"exec_id"`
	StartedUnixNS   int64  `json:"started_unix_ns"`
	ProtocolVersion int    `json:"protocol_version"`
}

type LogChunk struct {
	ExecID string `json:"exec_id,omitempty"`
	Stream string `json:"stream"`
	Data   []byte `json:"data"`
}

type Heartbeat struct{}

type ExecResult struct {
	LeaseID         string `json:"lease_id"`
	ExecID          string `json:"exec_id"`
	ExitCode        int    `json:"exit_code"`
	DurationMS      int64  `json:"duration_ms"`
	StdoutBytes     uint64 `json:"stdout_bytes"`
	StderrBytes     uint64 `json:"stderr_bytes"`
	DroppedLogBytes uint64 `json:"dropped_log_bytes"`
}

type CheckpointRequest struct {
	RequestID string `json:"request_id"`
	Operation string `json:"operation"`
	Ref       string `json:"ref"`
}

type CheckpointResponse struct {
	RequestID string `json:"request_id"`
	Operation string `json:"operation"`
	Ref       string `json:"ref"`
	Accepted  bool   `json:"accepted"`
	VersionID string `json:"version_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

type FilesystemSealRequest struct {
	LeaseID         string `json:"lease_id"`
	Name            string `json:"name"`
	ProtocolVersion int    `json:"protocol_version"`
}

type FilesystemSealResult struct {
	LeaseID string `json:"lease_id"`
	Name    string `json:"name"`
	Sealed  bool   `json:"sealed"`
	Error   string `json:"error,omitempty"`
}

type Fatal struct {
	Message string `json:"message"`
}

type Cancel struct {
	ExecID string `json:"exec_id,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type Ack struct {
	ForType MessageType `json:"for_type"`
	ForSeq  uint64      `json:"for_seq,omitempty"`
}

type Shutdown struct{}

func ValidateCheckpointRef(ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return errors.New("checkpoint ref is required")
	}
	if len(ref) > MaxCheckpointRefLen {
		return fmt.Errorf("checkpoint ref exceeds %d bytes", MaxCheckpointRefLen)
	}
	if !checkpointRefPattern.MatchString(ref) {
		return errors.New("checkpoint ref must start with an ASCII letter or digit and contain only letters, digits, '.', '_', ':', or '-'")
	}
	return nil
}

func ValidateCheckpointRequest(req CheckpointRequest) error {
	switch strings.TrimSpace(req.Operation) {
	case CheckpointOperationSave:
	default:
		return fmt.Errorf("unsupported checkpoint operation %q", req.Operation)
	}
	return ValidateCheckpointRef(req.Ref)
}

type Codec struct {
	reader io.Reader
	writer io.Writer
}

func NewCodec(reader io.Reader, writer io.Writer) *Codec {
	return &Codec{reader: reader, writer: writer}
}

func NewEnvelope(msgType MessageType, seq uint64, monoNS int64, payload any) (Envelope, error) {
	env := Envelope{
		Version: ProtocolVersion,
		Type:    msgType,
		Seq:     seq,
		MonoNS:  monoNS,
	}
	if payload == nil {
		return env, nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("marshal %s payload: %w", msgType, err)
	}
	env.Payload = data
	return env, nil
}

func DecodePayload[T any](env Envelope) (T, error) {
	var payload T
	if len(env.Payload) == 0 {
		return payload, nil
	}
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return payload, fmt.Errorf("decode %s payload: %w", env.Type, err)
	}
	return payload, nil
}

func (c *Codec) WriteEnvelope(env Envelope) error {
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	if len(data) > MaxFrameSize {
		return fmt.Errorf("%w: %d > %d", ErrFrameTooLarge, len(data), MaxFrameSize)
	}
	if len(data) > int(^uint32(0)) {
		return fmt.Errorf("frame too large: %d", len(data))
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(data)))
	if _, err := c.writer.Write(header[:]); err != nil {
		return fmt.Errorf("write frame header: %w", err)
	}
	if _, err := c.writer.Write(data); err != nil {
		return fmt.Errorf("write frame body: %w", err)
	}
	return nil
}

func (c *Codec) ReadEnvelope() (Envelope, error) {
	var header [4]byte
	if _, err := io.ReadFull(c.reader, header[:]); err != nil {
		return Envelope{}, err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size > MaxFrameSize {
		return Envelope{}, fmt.Errorf("%w: %d > %d", ErrFrameTooLarge, size, MaxFrameSize)
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(c.reader, body); err != nil {
		return Envelope{}, err
	}
	var env Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return Envelope{}, fmt.Errorf("decode envelope: %w", err)
	}
	if env.Version != ProtocolVersion {
		return Envelope{}, fmt.Errorf("unsupported protocol version %d", env.Version)
	}
	return env, nil
}
