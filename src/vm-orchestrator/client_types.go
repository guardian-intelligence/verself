package vmorchestrator

import "time"

const (
	DefaultSocketPath = "/run/vm-orchestrator/api.sock"
)

type RunState int

const (
	RunStateUnspecified RunState = iota
	RunStatePending
	RunStateRunning
	RunStateSucceeded
	RunStateFailed
	RunStateCanceled
)

func (s RunState) Terminal() bool {
	switch s {
	case RunStateSucceeded, RunStateFailed, RunStateCanceled:
		return true
	default:
		return false
	}
}

type HostRunSpec struct {
	RunID              string
	RunCommand         []string
	RunWorkDir         string
	Env                map[string]string
	BillablePhases     []string
	CheckpointSaveRefs []string
	AttemptID          string
	SegmentID          string
}

type HostRunSnapshot struct {
	RunID          string
	State          RunState
	Terminal       bool
	TerminalReason string
	Result         *RunResult
	UpdatedAt      time.Time
}

type HostRunEvent struct {
	Seq       uint64
	RunID     string
	EventType string
	Attrs     map[string]string
	CreatedAt time.Time
}

type CheckpointEvent struct {
	RequestID string
	Operation string
	Ref       string
	Accepted  bool
	VersionID string
	Error     string
}

type TelemetryHello struct {
	Seq        uint32
	Flags      uint32
	MonoNS     uint64
	WallNS     uint64
	BootID     string
	MemTotalKB uint64
}

type TelemetrySample struct {
	Seq            uint32
	Flags          uint32
	MonoNS         uint64
	WallNS         uint64
	CPUUserTicks   uint64
	CPUSystemTicks uint64
	CPUIdleTicks   uint64
	Load1Centis    uint32
	Load5Centis    uint32
	Load15Centis   uint32
	ProcsRunning   uint16
	ProcsBlocked   uint16
	MemAvailableKB uint64
	IOReadBytes    uint64
	IOWriteBytes   uint64
	NetRXBytes     uint64
	NetTXBytes     uint64
	PSICPUPct100   uint16
	PSIMemPct100   uint16
	PSIIOPct100    uint16
}

type TelemetryDiagnosticKind string

const (
	TelemetryDiagnosticKindGap        TelemetryDiagnosticKind = "gap"
	TelemetryDiagnosticKindRegression TelemetryDiagnosticKind = "regression"
)

type TelemetryDiagnostic struct {
	Kind           TelemetryDiagnosticKind
	ExpectedSeq    uint32
	ObservedSeq    uint32
	MissingSamples uint32
}

type TelemetryEvent struct {
	RunID          string
	ReceivedAtUnix time.Time
	Hello          *TelemetryHello
	Sample         *TelemetrySample
	Diagnostic     *TelemetryDiagnostic
}

type Capacity struct {
	GuestPoolCIDR          string
	TotalSlots             uint32
	ActiveRuns             uint32
	AvailableSlots         uint32
	VCPUsPerVM             uint32
	MemoryMiBPerVM         uint32
	RootfsProvisionedBytes uint64
}
