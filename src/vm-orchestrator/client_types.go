package vmorchestrator

import "time"

const (
	DefaultSocketPath         = "/run/vm-orchestrator/api.sock"
	DefaultRepoGoldenStateDir = "/var/lib/ci/repo-goldens"
)

type JobState int

const (
	JobStateUnspecified JobState = iota
	JobStatePending
	JobStateRunning
	JobStateSucceeded
	JobStateFailed
	JobStateCanceled
)

func (s JobState) Terminal() bool {
	switch s {
	case JobStateSucceeded, JobStateFailed, JobStateCanceled:
		return true
	default:
		return false
	}
}

type RepoExecRequest struct {
	Config          Config
	Repo            string
	RepoURL         string
	Ref             string
	JobTemplate     JobConfig
	LockfileRelPath string
}

type RepoExecMetadata struct {
	Repo           string
	RepoURL        string
	Ref            string
	GoldenSnapshot string
	CloneDuration  time.Duration
	InstallNeeded  bool
	CommitSHA      string
}

type JobStatus struct {
	JobID        string
	State        JobState
	Terminal     bool
	ErrorMessage string
	Result       *JobResult
	RepoExec     *RepoExecMetadata
}

type JobGuestEvent struct {
	Seq      uint64
	JobID    string
	Kind     string
	Attrs    map[string]string
	Terminal bool
}

type WarmGoldenRequest struct {
	Config          Config
	Repo            string
	RepoURL         string
	DefaultBranch   string
	Job             JobConfig
	LockfileRelPath string
}

type WarmGoldenResult struct {
	TargetDataset             string
	PreviousDataset           string
	Promoted                  bool
	FilesystemCheckOK         bool
	CloneDuration             time.Duration
	FilesystemCheckDuration   time.Duration
	SnapshotPromotionDuration time.Duration
	PreviousDestroyDuration   time.Duration
	CommitSHA                 string
	JobResult                 JobResult
	ErrorMessage              string
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

type TelemetryEvent struct {
	JobID          string
	ReceivedAtUnix time.Time
	Hello          *TelemetryHello
	Sample         *TelemetrySample
}

type FleetVM struct {
	JobID        string
	State        JobState
	LastUpdateAt time.Time
	Hello        *TelemetryHello
	LatestSample *TelemetrySample
}

type Capacity struct {
	GuestPoolCIDR  string
	TotalSlots     uint32
	ActiveJobs     uint32
	AvailableSlots uint32
	VCPUsPerVM     uint32
	MemoryMiBPerVM uint32
}
