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
	ProtocolVersion = 2
	GuestPort       = 10789
	GuestCID        = 52

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
	TypeHello              MessageType = "hello"
	TypeRunRequest         MessageType = "run_request"
	TypePhaseStart         MessageType = "phase_start"
	TypePhaseEnd           MessageType = "phase_end"
	TypeLogChunk           MessageType = "log_chunk"
	TypeHeartbeat          MessageType = "heartbeat"
	TypeResult             MessageType = "result"
	TypeCheckpointRequest  MessageType = "checkpoint_request"
	TypeCheckpointResponse MessageType = "checkpoint_response"
	TypeFatal              MessageType = "fatal"
	TypeCancel             MessageType = "cancel"
	TypeAck                MessageType = "ack"
	TypeShutdown           MessageType = "shutdown"
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
	BootToReadyMS int64 `json:"boot_to_ready_ms"`
}

type NetworkConfig struct {
	AddressCIDR     string   `json:"address_cidr"`
	Gateway         string   `json:"gateway"`
	LinkName        string   `json:"link_name"`
	DNS             []string `json:"dns,omitempty"`
	HostServiceIP   string   `json:"host_service_ip,omitempty"`
	HostServicePort int      `json:"host_service_port,omitempty"`
}

type RunRequest struct {
	RunID               string            `json:"run_id"`
	WorkloadKind        string            `json:"workload_kind,omitempty"`
	RunnerClass         string            `json:"runner_class,omitempty"`
	RunCommand          []string          `json:"run_command"`
	RunWorkDir          string            `json:"run_work_dir,omitempty"`
	Env                 map[string]string `json:"env,omitempty"`
	WorkflowYAML        string            `json:"workflow_yaml,omitempty"`
	WorkflowEnv         map[string]string `json:"workflow_env,omitempty"`
	WorkflowSecrets     map[string]string `json:"workflow_secrets,omitempty"`
	WorkflowEventName   string            `json:"workflow_event_name,omitempty"`
	WorkflowInputs      map[string]string `json:"workflow_inputs,omitempty"`
	GitHubJITConfig     string            `json:"github_jit_config,omitempty"`
	Network             NetworkConfig     `json:"network"`
	HostWallclockUnixNS int64             `json:"host_wallclock_unix_ns"`
	ProtocolVersion     int               `json:"protocol_version"`
}

const (
	WorkloadKindDirect          = "direct"
	WorkloadKindForgejoWorkflow = "forgejo_workflow"
	WorkloadKindGitHubRunner    = "github_runner"
)

func NormalizeWorkloadKind(kind string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return WorkloadKindDirect
	}
	return kind
}

func ValidateWorkloadKind(kind string) error {
	switch NormalizeWorkloadKind(kind) {
	case WorkloadKindDirect, WorkloadKindForgejoWorkflow, WorkloadKindGitHubRunner:
		return nil
	default:
		return fmt.Errorf("unsupported workload_kind %q", strings.TrimSpace(kind))
	}
}

type PhaseStart struct {
	Name string `json:"name"`
}

type PhaseEnd struct {
	Name       string `json:"name"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
}

type LogChunk struct {
	Stream string `json:"stream"`
	Data   []byte `json:"data"`
}

type Heartbeat struct{}

type Result struct {
	ExitCode        int    `json:"exit_code"`
	RunDurationMS   int64  `json:"run_duration_ms"`
	BootToReadyMS   int64  `json:"boot_to_ready_ms"`
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

type Fatal struct {
	Message string `json:"message"`
}

type Cancel struct {
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
