package vmorchestrator

import (
	"time"
)

const DefaultSocketPath = "/run/vm-orchestrator/api.sock"

type LeaseRecord struct {
	LeaseID          string
	State            LeaseState
	AcquiredAt       time.Time
	ReadyAt          time.Time
	ExpiresAt        time.Time
	TerminalAt       time.Time
	TerminalReason   string
	VMIP             string
	Resources        VMResources
	TrustClass       string
	FilesystemMounts []FilesystemMountResult
	Activation       ActivationResult
}

type FilesystemMountResult struct {
	Name        string
	MountPath   string
	OperationID string
	Mounted     bool
	Required    bool
	Error       string
}

type FilesystemCommitRecord struct {
	LeaseID       string
	MountName     string
	VolumeDataset string
	Snapshot      string
	SnapshotGUID  string
	UsedBytes     uint64
	WrittenBytes  uint64
	CommittedAt   time.Time
}

type GoldenVMMountCheckpoint struct {
	MountName    string
	SnapshotRef  string
	SnapshotGUID string
}

type GoldenVMCheckpointRecord struct {
	LeaseID            string
	OperationID        string
	CheckpointID       string
	SnapshotKey        string
	RootSnapshotRef    string
	RootSnapshotGUID   string
	VMStateArtifactRef string
	MemoryArtifactRef  string
	StateBytes         uint64
	MemoryBytes        uint64
	CheckpointedAt     time.Time
	MountSnapshots     []GoldenVMMountCheckpoint
}

type SnapshotDestroyRecord struct {
	OperationID         string
	VolumeID            string
	SnapshotRef         string
	SnapshotArtifactKey string
	DestroyedAt         time.Time
}

type OrgRuntimeShape struct {
	StorageNamespace StorageNamespace
	ImageRefs        []string
}

type OrgRuntimeStatus struct {
	StorageNamespace StorageNamespace
	Images           []OrgRuntimeImage
	ReadyAt          time.Time
	VerifiedAt       time.Time
}

type OrgRuntimeImage struct {
	ImageRef       string
	SourceSnapshot string
	SourceDigest   string
	OrgSnapshot    string
}

type ExecRecord struct {
	LeaseID                string
	ExecID                 string
	State                  ExecState
	ExitCode               int
	TerminalReason         string
	QueuedAt               time.Time
	StartedAt              time.Time
	FirstByteAt            time.Time
	ExitedAt               time.Time
	StdoutBytes            uint64
	StderrBytes            uint64
	DroppedLogBytes        uint64
	Output                 string
	Metrics                *VMMetrics
	ZFSWritten             uint64
	RootfsProvisionedBytes uint64
}

type LeaseEvent struct {
	LeaseID   string
	Seq       uint64
	Type      LeaseEventType
	ExecID    string
	Attrs     map[string]string
	CreatedAt time.Time
}

type Capacity struct {
	GuestPoolCIDR          string
	TotalSlots             uint32
	LeasesHeld             uint32
	LeasesAvailable        uint32
	MaxVCPUsPerLease       uint32
	MaxMemoryMiBPerLease   uint32
	MaxRootDiskGiBPerLease uint32
	RootfsProvisionedBytes uint64
	ZpoolSizeBytes         uint64
	ZpoolAllocatedBytes    uint64
	ZpoolFreeBytes         uint64
	OrgStorage             []OrgStorageCapacity
}

type OrgStorageCapacity struct {
	OrgID          string
	UsedBytes      uint64
	QuotaBytes     uint64
	AvailableBytes uint64
}

type TelemetryEvent struct {
	LeaseID        string
	ReceivedAtUnix time.Time
	Hello          *TelemetryHello
	Sample         *TelemetrySample
	Diagnostic     *TelemetryDiagnostic
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
