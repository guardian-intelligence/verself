package vmproto

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

const (
	ProtocolVersion = 1
	GuestPort       = 10789
	GuestCID        = 52

	ControlQueueCapacity = 32
	LogQueueCapacity     = 256

	HeartbeatInterval = 5 * time.Second
	HeartbeatTimeout  = 30 * time.Second
	CancelGracePeriod = 5 * time.Second
)

type MessageType string

const (
	TypeHello      MessageType = "hello"
	TypeRunRequest MessageType = "run_request"
	TypePhaseStart MessageType = "phase_start"
	TypePhaseEnd   MessageType = "phase_end"
	TypeLogChunk   MessageType = "log_chunk"
	TypeHeartbeat  MessageType = "heartbeat"
	TypeResult     MessageType = "result"
	TypeFatal      MessageType = "fatal"
	TypeCancel     MessageType = "cancel"
	TypeAck        MessageType = "ack"
	TypeShutdown   MessageType = "shutdown"
)

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
	AddressCIDR string   `json:"address_cidr"`
	Gateway     string   `json:"gateway"`
	LinkName    string   `json:"link_name"`
	DNS         []string `json:"dns,omitempty"`
}

type RunRequest struct {
	JobID               string            `json:"job_id"`
	PrepareCommand      []string          `json:"prepare_command,omitempty"`
	PrepareWorkDir      string            `json:"prepare_work_dir,omitempty"`
	RunCommand          []string          `json:"run_command"`
	RunWorkDir          string            `json:"run_work_dir,omitempty"`
	Services            []string          `json:"services,omitempty"`
	Env                 map[string]string `json:"env,omitempty"`
	Network             NetworkConfig     `json:"network"`
	HostWallclockUnixNS int64             `json:"host_wallclock_unix_ns"`
	TemplateGeneration  string            `json:"template_generation,omitempty"`
	ProtocolVersion     int               `json:"protocol_version"`
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
	ExitCode               int    `json:"exit_code"`
	PrepareDurationMS      int64  `json:"prepare_duration_ms"`
	RunDurationMS          int64  `json:"run_duration_ms"`
	ServiceStartDurationMS int64  `json:"service_start_duration_ms"`
	BootToReadyMS          int64  `json:"boot_to_ready_ms"`
	StdoutBytes            uint64 `json:"stdout_bytes"`
	StderrBytes            uint64 `json:"stderr_bytes"`
	DroppedLogBytes        uint64 `json:"dropped_log_bytes"`
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
