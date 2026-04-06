package clickhouse

import (
	"time"

	"github.com/google/uuid"
)

// CIEvent is the wide event struct: one row per CI execution event.
// Every dimension is denormalized into a single flat row.
type CIEvent struct {
	// Identity
	JobID     uuid.UUID `ch:"job_id"`
	RunID     string    `ch:"run_id"`
	EventKind string    `ch:"event_kind"`
	NodeID    string    `ch:"node_id"`
	Region    string    `ch:"region"`
	Plan      string    `ch:"plan"`

	// Git metadata
	Repo             string `ch:"repo"`
	Branch           string `ch:"branch"`
	CommitSHA        string `ch:"commit_sha"`
	PRNumber         uint32 `ch:"pr_number"`
	PRAuthor         string `ch:"pr_author"`
	BaseBranch       string `ch:"base_branch"`
	DiffFilesChanged uint16 `ch:"diff_files_changed"`
	DiffLinesAdded   uint32 `ch:"diff_lines_added"`
	DiffLinesDeleted uint32 `ch:"diff_lines_deleted"`

	// Timing (nanoseconds)
	ZFSCloneNs       int64 `ch:"zfs_clone_ns"`
	GVisorSetupNs    int64 `ch:"gvisor_setup_ns"`
	DepsInstallNs    int64 `ch:"deps_install_ns"`
	LintNs           int64 `ch:"lint_ns"`
	TypecheckNs      int64 `ch:"typecheck_ns"`
	BuildNs          int64 `ch:"build_ns"`
	TestNs           int64 `ch:"test_ns"`
	TotalCINs        int64 `ch:"total_ci_ns"`
	TotalE2ENs       int64 `ch:"total_e2e_ns"`
	CleanupNs        int64 `ch:"cleanup_ns"`
	GVisorTeardownNs int64 `ch:"gvisor_teardown_ns"`

	// Exit codes
	LintExit      int8 `ch:"lint_exit"`
	TypecheckExit int8 `ch:"typecheck_exit"`
	BuildExit     int8 `ch:"build_exit"`
	TestExit      int8 `ch:"test_exit"`

	// Resource usage (peak, from cgroup stats)
	CPUUserMs       uint64 `ch:"cpu_user_ms"`
	CPUSystemMs     uint64 `ch:"cpu_system_ms"`
	MemoryPeakBytes uint64 `ch:"memory_peak_bytes"`
	IOReadBytes     uint64 `ch:"io_read_bytes"`
	IOWriteBytes    uint64 `ch:"io_write_bytes"`
	ZFSWrittenBytes uint64 `ch:"zfs_written_bytes"`

	// Firecracker VM metrics
	VMBootTimeUs               uint64 `ch:"vm_boot_time_us"`
	BlockReadBytes             uint64 `ch:"block_read_bytes"`
	BlockWriteBytes            uint64 `ch:"block_write_bytes"`
	BlockReadCount             uint64 `ch:"block_read_count"`
	BlockWriteCount            uint64 `ch:"block_write_count"`
	NetRxBytes                 uint64 `ch:"net_rx_bytes"`
	NetTxBytes                 uint64 `ch:"net_tx_bytes"`
	VCPUExitCount              uint64 `ch:"vcpu_exit_count"`
	VMExitCode                 int32  `ch:"vm_exit_code"`
	JobConfigJSON              string `ch:"job_config_json"`
	BootToReadyNs              int64  `ch:"boot_to_ready_ns"`
	ServiceStartNs             int64  `ch:"service_start_ns"`
	VMExitWaitNs               int64  `ch:"vm_exit_wait_ns"`
	VMExitForced               uint8  `ch:"vm_exit_forced"`
	WarmFilesystemCheckNs      int64  `ch:"warm_filesystem_check_ns"`
	WarmSnapshotPromotionNs    int64  `ch:"warm_snapshot_promotion_ns"`
	WarmPreviousDestroyNs      int64  `ch:"warm_previous_destroy_ns"`
	WarmFilesystemCheckOK      uint8  `ch:"warm_filesystem_check_ok"`
	StdoutBytes                uint64 `ch:"stdout_bytes"`
	StderrBytes                uint64 `ch:"stderr_bytes"`
	DroppedLogBytes            uint64 `ch:"dropped_log_bytes"`
	GuestRootfsTreeBytes       uint64 `ch:"guest_rootfs_tree_bytes"`
	GuestRootfsAllocatedBytes  uint64 `ch:"guest_rootfs_allocated_bytes"`
	GuestRootfsFilesystemBytes uint64 `ch:"guest_rootfs_filesystem_bytes"`
	GuestRootfsUsedBytes       uint64 `ch:"guest_rootfs_used_bytes"`
	GuestKernelBytes           uint64 `ch:"guest_kernel_bytes"`
	GuestPackageCount          uint32 `ch:"guest_package_count"`

	// Supply chain scanning (event_kind = "supply-chain-scan")
	SupplyChainScanNs    int64  `ch:"supply_chain_scan_ns"`
	SupplyChainScanOK    uint8  `ch:"supply_chain_scan_ok"`
	ScanAgeFindings      uint16 `ch:"scan_age_findings"`
	ScanGuardDogFindings uint16 `ch:"scan_guarddog_findings"`
	ScanJSXRayFindings   uint16 `ch:"scan_jsxray_findings"`
	ScanOSVFindings      uint16 `ch:"scan_osv_findings"`
	ScanTarballsCount    uint32 `ch:"scan_tarballs_count"`

	// Cache effectiveness
	NPMCacheHit     uint8 `ch:"npm_cache_hit"`
	NextCacheHit    uint8 `ch:"next_cache_hit"`
	TSCCacheHit     uint8 `ch:"tsc_cache_hit"`
	LockfileChanged uint8 `ch:"lockfile_changed"`

	// Hardware
	CPUModel string `ch:"cpu_model"`
	Cores    uint16 `ch:"cores"`
	MemoryMB uint32 `ch:"memory_mb"`
	DiskType string `ch:"disk_type"`

	// Environment
	GoldenSnapshot string  `ch:"golden_snapshot"`
	GoldenAgeHours float32 `ch:"golden_age_hours"`
	NodeVersion    string  `ch:"node_version"`
	NPMVersion     string  `ch:"npm_version"`

	// Timestamps
	CreatedAt   time.Time `ch:"created_at"`
	StartedAt   time.Time `ch:"started_at"`
	CompletedAt time.Time `ch:"completed_at"`
}
