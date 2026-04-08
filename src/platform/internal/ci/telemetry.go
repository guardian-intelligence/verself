package ci

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	ch "github.com/forge-metal/forge-metal/internal/clickhouse"
	"github.com/forge-metal/forge-metal/internal/config"
	vmorchestrator "github.com/forge-metal/vm-orchestrator"
	"github.com/google/uuid"
)

type emitExecTelemetryInput struct {
	FirecrackerConfig vmorchestrator.Config
	Request           ExecRequest
	RunID             string
	Manifest          *Manifest
	Toolchain         *Toolchain
	InstallNeeded     bool
	GoldenSnapshot    string
	Job               vmorchestrator.JobConfig
	JobResult         vmorchestrator.JobResult
	CloneDuration     time.Duration
	CreatedAt         time.Time
	StartedAt         time.Time
	CompletedAt       time.Time
	CommitSHA         string
	PRNumber          uint32
	RunErr            error
}

type emitWarmTelemetryInput struct {
	FirecrackerConfig         vmorchestrator.Config
	Request                   WarmRequest
	RunID                     string
	ParentRunID               string
	Manifest                  *Manifest
	Toolchain                 *Toolchain
	TargetDataset             string
	PreviousDataset           string
	Job                       vmorchestrator.JobConfig
	JobResult                 vmorchestrator.JobResult
	CloneDuration             time.Duration
	FilesystemCheckDuration   time.Duration
	SnapshotPromotionDuration time.Duration
	PreviousDestroyDuration   time.Duration
	FilesystemCheckOK         bool
	Promoted                  bool
	CreatedAt                 time.Time
	StartedAt                 time.Time
	CompletedAt               time.Time
	CommitSHA                 string
	RunErr                    error
}

func emitExecTelemetry(logger *slog.Logger, input emitExecTelemetryInput) error {
	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	client, err := ch.New(cfg.ClickHouse)
	if err != nil {
		return err
	}
	defer client.Close()

	jobUUID, err := uuid.Parse(input.Job.JobID)
	if err != nil {
		return fmt.Errorf("parse job id %s: %w", input.Job.JobID, err)
	}

	artifactManifestPath := guestArtifactManifestPath(input.FirecrackerConfig)
	guestArtifactManifest, guestArtifactErr := loadGuestArtifactManifest(artifactManifestPath)
	if guestArtifactErr != nil && logger != nil {
		logger.Warn("load guest artifact manifest failed", "path", artifactManifestPath, "err", guestArtifactErr)
	}

	jobConfigJSON, err := buildExecJobConfigJSON(input, artifactManifestPath, guestArtifactManifest, guestArtifactErr)
	if err != nil {
		return err
	}

	event := &ch.CIEvent{
		JobID:           jobUUID,
		RunID:           input.RunID,
		EventKind:       "exec",
		NodeID:          hostNodeID(),
		Region:          cfg.Latitude.Region,
		Plan:            cfg.Latitude.Plan,
		Repo:            input.Request.Repo,
		Branch:          input.Request.Ref,
		CommitSHA:       normalizeCommitSHA(input.CommitSHA),
		PRNumber:        input.PRNumber,
		BaseBranch:      "",
		ZFSCloneNs:      input.CloneDuration.Nanoseconds(),
		DepsInstallNs:   input.JobResult.PrepareDuration.Nanoseconds(),
		TestNs:          input.JobResult.RunDuration.Nanoseconds(),
		TotalCINs:       input.JobResult.Duration.Nanoseconds(),
		TotalE2ENs:      endToEndDuration(input).Nanoseconds(),
		CleanupNs:       input.JobResult.CleanupTime.Nanoseconds(),
		TestExit:        clampInt8(execExitCode(input)),
		ZFSWrittenBytes: input.JobResult.ZFSWritten,
		NPMCacheHit:     0,
		NextCacheHit:    0,
		TSCCacheHit:     0,
		LockfileChanged: boolToUint8(input.InstallNeeded),
		Cores:           uint16(input.FirecrackerConfig.VCPUs),
		MemoryMB:        uint32(input.FirecrackerConfig.MemoryMiB),
		DiskType:        "zfs-zvol",
		GoldenSnapshot:  input.GoldenSnapshot,
		GoldenAgeHours:  0,
		NodeVersion:     input.Toolchain.NodeVersion,
		NPMVersion:      input.Toolchain.PackageManagerVersion,
		CreatedAt:       input.CreatedAt.UTC(),
		StartedAt:       input.StartedAt.UTC(),
		CompletedAt:     input.CompletedAt.UTC(),
		VMExitCode:      int32(execExitCode(input)),
		JobConfigJSON:   jobConfigJSON,
		BootToReadyNs:   input.JobResult.BootToReadyDuration.Nanoseconds(),
		ServiceStartNs:  input.JobResult.ServiceStartDuration.Nanoseconds(),
		VMExitWaitNs:    input.JobResult.VMExitWaitDuration.Nanoseconds(),
		VMExitForced:    boolToUint8(input.JobResult.ForcedShutdown),
		StdoutBytes:     input.JobResult.StdoutBytes,
		StderrBytes:     input.JobResult.StderrBytes,
		DroppedLogBytes: input.JobResult.DroppedLogBytes,
	}
	if guestArtifactManifest != nil {
		event.GuestRootfsTreeBytes = guestArtifactManifest.RootfsTreeBytes
		event.GuestRootfsAllocatedBytes = guestArtifactManifest.RootfsAllocatedBytes
		event.GuestRootfsFilesystemBytes = guestArtifactManifest.RootfsFilesystemBytes
		event.GuestRootfsUsedBytes = guestArtifactManifest.RootfsUsedBytes
		event.GuestKernelBytes = guestArtifactManifest.KernelBytes
		event.GuestPackageCount = guestArtifactManifest.PackageCount
	}

	if input.JobResult.Metrics != nil {
		event.VMBootTimeUs = input.JobResult.Metrics.BootTimeUs
		event.BlockReadBytes = input.JobResult.Metrics.BlockReadBytes
		event.BlockWriteBytes = input.JobResult.Metrics.BlockWriteBytes
		event.BlockReadCount = input.JobResult.Metrics.BlockReadCount
		event.BlockWriteCount = input.JobResult.Metrics.BlockWriteCount
		event.NetRxBytes = input.JobResult.Metrics.NetRxBytes
		event.NetTxBytes = input.JobResult.Metrics.NetTxBytes
		event.VCPUExitCount = input.JobResult.Metrics.VCPUExitCount
	}

	insertCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.InsertEvent(insertCtx, event); err != nil {
		return fmt.Errorf("insert ci event: %w", err)
	}
	if logger != nil {
		logger.Info("ci exec telemetry inserted", "repo", input.Request.Repo, "run_id", input.RunID, "job_id", input.Job.JobID)
	}
	return nil
}

func emitWarmTelemetry(logger *slog.Logger, input emitWarmTelemetryInput) error {
	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	client, err := ch.New(cfg.ClickHouse)
	if err != nil {
		return err
	}
	defer client.Close()

	jobUUID, err := uuid.Parse(strings.TrimSpace(input.Job.JobID))
	if err != nil {
		jobUUID = uuid.New()
	}

	artifactManifestPath := guestArtifactManifestPath(input.FirecrackerConfig)
	guestArtifactManifest, guestArtifactErr := loadGuestArtifactManifest(artifactManifestPath)
	if guestArtifactErr != nil && logger != nil {
		logger.Warn("load guest artifact manifest failed", "path", artifactManifestPath, "err", guestArtifactErr)
	}

	jobConfigJSON, err := buildWarmJobConfigJSON(input, artifactManifestPath, guestArtifactManifest, guestArtifactErr)
	if err != nil {
		return err
	}

	branch := defaultBranchRef(input.Request.DefaultBranch)
	event := &ch.CIEvent{
		JobID:                   jobUUID,
		RunID:                   input.RunID,
		EventKind:               "warm",
		NodeID:                  hostNodeID(),
		Region:                  cfg.Latitude.Region,
		Plan:                    cfg.Latitude.Plan,
		Repo:                    input.Request.Repo,
		Branch:                  branch,
		CommitSHA:               normalizeCommitSHA(input.CommitSHA),
		PRNumber:                0,
		BaseBranch:              input.Request.DefaultBranch,
		ZFSCloneNs:              input.CloneDuration.Nanoseconds(),
		DepsInstallNs:           input.JobResult.PrepareDuration.Nanoseconds(),
		TestNs:                  input.JobResult.RunDuration.Nanoseconds(),
		TotalCINs:               input.JobResult.Duration.Nanoseconds(),
		TotalE2ENs:              warmEndToEndDuration(input).Nanoseconds(),
		CleanupNs:               input.JobResult.CleanupTime.Nanoseconds(),
		TestExit:                clampInt8(warmExitCode(input)),
		ZFSWrittenBytes:         input.JobResult.ZFSWritten,
		NPMCacheHit:             0,
		NextCacheHit:            0,
		TSCCacheHit:             0,
		LockfileChanged:         0,
		Cores:                   uint16(input.FirecrackerConfig.VCPUs),
		MemoryMB:                uint32(input.FirecrackerConfig.MemoryMiB),
		DiskType:                "zfs-zvol",
		GoldenSnapshot:          input.TargetDataset + "@ready",
		GoldenAgeHours:          0,
		NodeVersion:             toolchainNodeVersion(input.Toolchain),
		NPMVersion:              toolchainPackageManagerVersion(input.Toolchain),
		CreatedAt:               input.CreatedAt.UTC(),
		StartedAt:               input.StartedAt.UTC(),
		CompletedAt:             input.CompletedAt.UTC(),
		VMExitCode:              int32(warmExitCode(input)),
		JobConfigJSON:           jobConfigJSON,
		BootToReadyNs:           input.JobResult.BootToReadyDuration.Nanoseconds(),
		ServiceStartNs:          input.JobResult.ServiceStartDuration.Nanoseconds(),
		VMExitWaitNs:            input.JobResult.VMExitWaitDuration.Nanoseconds(),
		VMExitForced:            boolToUint8(input.JobResult.ForcedShutdown),
		WarmFilesystemCheckNs:   input.FilesystemCheckDuration.Nanoseconds(),
		WarmSnapshotPromotionNs: input.SnapshotPromotionDuration.Nanoseconds(),
		WarmPreviousDestroyNs:   input.PreviousDestroyDuration.Nanoseconds(),
		WarmFilesystemCheckOK:   boolToUint8(input.FilesystemCheckOK),
		StdoutBytes:             input.JobResult.StdoutBytes,
		StderrBytes:             input.JobResult.StderrBytes,
		DroppedLogBytes:         input.JobResult.DroppedLogBytes,
	}
	if guestArtifactManifest != nil {
		event.GuestRootfsTreeBytes = guestArtifactManifest.RootfsTreeBytes
		event.GuestRootfsAllocatedBytes = guestArtifactManifest.RootfsAllocatedBytes
		event.GuestRootfsFilesystemBytes = guestArtifactManifest.RootfsFilesystemBytes
		event.GuestRootfsUsedBytes = guestArtifactManifest.RootfsUsedBytes
		event.GuestKernelBytes = guestArtifactManifest.KernelBytes
		event.GuestPackageCount = guestArtifactManifest.PackageCount
	}
	if input.JobResult.Metrics != nil {
		event.VMBootTimeUs = input.JobResult.Metrics.BootTimeUs
		event.BlockReadBytes = input.JobResult.Metrics.BlockReadBytes
		event.BlockWriteBytes = input.JobResult.Metrics.BlockWriteBytes
		event.BlockReadCount = input.JobResult.Metrics.BlockReadCount
		event.BlockWriteCount = input.JobResult.Metrics.BlockWriteCount
		event.NetRxBytes = input.JobResult.Metrics.NetRxBytes
		event.NetTxBytes = input.JobResult.Metrics.NetTxBytes
		event.VCPUExitCount = input.JobResult.Metrics.VCPUExitCount
	}

	insertCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.InsertEvent(insertCtx, event); err != nil {
		return fmt.Errorf("insert ci warm event: %w", err)
	}
	if logger != nil {
		logger.Info("ci warm telemetry inserted", "repo", input.Request.Repo, "run_id", input.RunID, "job_id", input.Job.JobID)
	}
	return nil
}

func buildExecJobConfigJSON(input emitExecTelemetryInput, manifestPath string, guestArtifacts *GuestArtifactManifest, guestArtifactErr error) (string, error) {
	failurePhase := strings.TrimSpace(input.JobResult.FailurePhase)
	payload := map[string]any{
		"event_kind":                      "exec",
		"run_id":                          input.RunID,
		"repo":                            input.Request.Repo,
		"ref":                             input.Request.Ref,
		"commit_sha":                      input.CommitSHA,
		"pr_number":                       input.PRNumber,
		"manifest_workdir":                input.Manifest.WorkDir,
		"manifest_services":               input.Manifest.Services,
		"manifest_env":                    input.Manifest.Env,
		"manifest_profile":                string(input.Manifest.Profile),
		"resolved_profile":                string(resolvedProfile(input.Manifest)),
		"package_manager":                 string(input.Toolchain.PackageManager),
		"package_manager_version":         input.Toolchain.PackageManagerVersion,
		"node_version":                    input.Toolchain.NodeVersion,
		"install_needed":                  input.InstallNeeded,
		"job_prepare_command":             input.Job.PrepareCommand,
		"job_prepare_workdir":             input.Job.PrepareWorkDir,
		"job_run_command":                 input.Job.RunCommand,
		"job_run_workdir":                 input.Job.RunWorkDir,
		"job_services":                    input.Job.Services,
		"job_env_names":                   sortedEnvKeys(input.Job.Env),
		"runtime_protocol":                "vsock-v1",
		"shutdown_mode":                   "guest_reboot_k",
		"vm_exit_wait_ns":                 input.JobResult.VMExitWaitDuration.Nanoseconds(),
		"vm_exit_forced":                  input.JobResult.ForcedShutdown,
		"boot_to_ready_ns":                input.JobResult.BootToReadyDuration.Nanoseconds(),
		"service_start_ns":                input.JobResult.ServiceStartDuration.Nanoseconds(),
		"prepare_ns":                      input.JobResult.PrepareDuration.Nanoseconds(),
		"run_ns":                          input.JobResult.RunDuration.Nanoseconds(),
		"stdout_bytes":                    input.JobResult.StdoutBytes,
		"stderr_bytes":                    input.JobResult.StderrBytes,
		"dropped_log_bytes":               input.JobResult.DroppedLogBytes,
		"phase_exit_codes":                phaseExitCodes(input.JobResult.PhaseResults),
		"failure_phase":                   failurePhase,
		"failure_exit_code":               phaseExitCode(input.JobResult.PhaseResults, failurePhase),
		"guest_log_tail":                  tailString(input.JobResult.Logs, 4096),
		"serial_log_tail":                 tailString(input.JobResult.SerialLogs, 2048),
		"guest_artifact_manifest_path":    manifestPath,
		"guest_artifact_manifest_present": guestArtifacts != nil,
	}
	addGuestArtifactPayload(payload, manifestPath, guestArtifacts, guestArtifactErr)
	if input.RunErr != nil {
		payload["run_error"] = input.RunErr.Error()
	}
	data, err := jsonMarshalIndent(payload)
	if err != nil {
		return "", fmt.Errorf("marshal exec telemetry payload: %w", err)
	}
	return string(data), nil
}

func buildWarmJobConfigJSON(input emitWarmTelemetryInput, manifestPath string, guestArtifacts *GuestArtifactManifest, guestArtifactErr error) (string, error) {
	payload := map[string]any{
		"event_kind":              "warm",
		"run_id":                  input.RunID,
		"parent_run_id":           input.ParentRunID,
		"repo":                    input.Request.Repo,
		"default_branch":          input.Request.DefaultBranch,
		"repo_url":                input.Request.RepoURL,
		"commit_sha":              input.CommitSHA,
		"target_dataset":          input.TargetDataset,
		"previous_dataset":        input.PreviousDataset,
		"promoted":                input.Promoted,
		"filesystem_check_ok":     input.FilesystemCheckOK,
		"filesystem_check_ns":     input.FilesystemCheckDuration.Nanoseconds(),
		"snapshot_promotion_ns":   input.SnapshotPromotionDuration.Nanoseconds(),
		"previous_destroy_ns":     input.PreviousDestroyDuration.Nanoseconds(),
		"warm_prepare_command":    input.Job.PrepareCommand,
		"warm_prepare_workdir":    input.Job.PrepareWorkDir,
		"warm_run_command":        input.Job.RunCommand,
		"warm_run_workdir":        input.Job.RunWorkDir,
		"warm_services":           input.Job.Services,
		"warm_env_names":          sortedEnvKeys(input.Job.Env),
		"resolved_profile":        manifestProfile(input.Manifest),
		"package_manager":         toolchainPackageManager(input.Toolchain),
		"package_manager_version": toolchainPackageManagerVersion(input.Toolchain),
		"node_version":            toolchainNodeVersion(input.Toolchain),
		"runtime_protocol":        "vsock-v1",
		"shutdown_mode":           "guest_reboot_k",
		"vm_exit_wait_ns":         input.JobResult.VMExitWaitDuration.Nanoseconds(),
		"vm_exit_forced":          input.JobResult.ForcedShutdown,
		"boot_to_ready_ns":        input.JobResult.BootToReadyDuration.Nanoseconds(),
		"service_start_ns":        input.JobResult.ServiceStartDuration.Nanoseconds(),
		"prepare_ns":              input.JobResult.PrepareDuration.Nanoseconds(),
		"run_ns":                  input.JobResult.RunDuration.Nanoseconds(),
		"stdout_bytes":            input.JobResult.StdoutBytes,
		"stderr_bytes":            input.JobResult.StderrBytes,
		"dropped_log_bytes":       input.JobResult.DroppedLogBytes,
	}
	addGuestArtifactPayload(payload, manifestPath, guestArtifacts, guestArtifactErr)
	if input.RunErr != nil {
		payload["run_error"] = input.RunErr.Error()
	}
	data, err := jsonMarshalIndent(payload)
	if err != nil {
		return "", fmt.Errorf("marshal warm telemetry payload: %w", err)
	}
	return string(data), nil
}

func phaseExitCodes(phases []vmorchestrator.PhaseResult) map[string]int {
	if len(phases) == 0 {
		return nil
	}
	codes := make(map[string]int, len(phases))
	for _, phase := range phases {
		codes[phase.Name] = phase.ExitCode
	}
	return codes
}

func phaseExitCode(phases []vmorchestrator.PhaseResult, phaseName string) int {
	phaseName = strings.TrimSpace(phaseName)
	if phaseName == "" {
		return 0
	}
	for _, phase := range phases {
		if phase.Name == phaseName {
			return phase.ExitCode
		}
	}
	return 0
}

func tailString(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}

func addGuestArtifactPayload(payload map[string]any, manifestPath string, guestArtifacts *GuestArtifactManifest, guestArtifactErr error) {
	payload["guest_artifact_manifest_path"] = manifestPath
	payload["guest_artifact_manifest_present"] = guestArtifacts != nil
	if guestArtifacts != nil {
		payload["guest_alpine_version"] = guestArtifacts.AlpineVersion
		payload["guest_firecracker_version"] = guestArtifacts.FirecrackerVersion
		payload["guest_kernel_version"] = guestArtifacts.GuestKernelVersion
		payload["guest_rootfs_sha256"] = guestArtifacts.RootfsSHA256
		payload["guest_rootfs_tree_bytes"] = guestArtifacts.RootfsTreeBytes
		payload["guest_rootfs_apparent_bytes"] = guestArtifacts.RootfsApparentBytes
		payload["guest_rootfs_allocated_bytes"] = guestArtifacts.RootfsAllocatedBytes
		payload["guest_rootfs_filesystem_bytes"] = guestArtifacts.RootfsFilesystemBytes
		payload["guest_rootfs_used_bytes"] = guestArtifacts.RootfsUsedBytes
		payload["guest_rootfs_free_bytes"] = guestArtifacts.RootfsFreeBytes
		payload["guest_kernel_sha256"] = guestArtifacts.KernelSHA256
		payload["guest_kernel_bytes"] = guestArtifacts.KernelBytes
		payload["guest_sbom_sha256"] = guestArtifacts.SBOMSHA256
		payload["guest_sbom_bytes"] = guestArtifacts.SBOMBytes
		payload["guest_package_count"] = guestArtifacts.PackageCount
		payload["guest_init_bytes"] = guestArtifacts.InitBytes
		payload["guest_artifacts_built_at_utc"] = guestArtifacts.BuiltAtUTC
	}
	if guestArtifactErr != nil {
		payload["guest_artifact_manifest_error"] = guestArtifactErr.Error()
	}
}

func execExitCode(input emitExecTelemetryInput) int {
	if input.RunErr != nil {
		if input.JobResult.ExitCode != 0 {
			return input.JobResult.ExitCode
		}
		return -1
	}
	return input.JobResult.ExitCode
}

func warmExitCode(input emitWarmTelemetryInput) int {
	if input.RunErr != nil {
		if input.JobResult.ExitCode != 0 {
			return input.JobResult.ExitCode
		}
		return -1
	}
	return input.JobResult.ExitCode
}

func endToEndDuration(input emitExecTelemetryInput) time.Duration {
	if input.CompletedAt.Before(input.CreatedAt) {
		return 0
	}
	return input.CompletedAt.Sub(input.CreatedAt)
}

func warmEndToEndDuration(input emitWarmTelemetryInput) time.Duration {
	if input.CompletedAt.Before(input.CreatedAt) {
		return 0
	}
	return input.CompletedAt.Sub(input.CreatedAt)
}

func warmRunIDs(parent string) (string, string) {
	parent = strings.TrimSpace(parent)
	if parent == "" {
		return "ci-warm-" + uuid.NewString(), ""
	}
	return parent + "-warm", parent
}

func defaultBranchRef(branch string) string {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = "main"
	}
	return "refs/heads/" + branch
}

func manifestProfile(manifest *Manifest) string {
	if manifest == nil {
		return ""
	}
	return string(resolvedProfile(manifest))
}

func toolchainPackageManager(toolchain *Toolchain) string {
	if toolchain == nil {
		return ""
	}
	return string(toolchain.PackageManager)
}

func toolchainPackageManagerVersion(toolchain *Toolchain) string {
	if toolchain == nil {
		return ""
	}
	return toolchain.PackageManagerVersion
}

func toolchainNodeVersion(toolchain *Toolchain) string {
	if toolchain == nil {
		return ""
	}
	return toolchain.NodeVersion
}

func prNumberFromRef(ref string) uint32 {
	const prefix = "refs/pull/"
	if !strings.HasPrefix(ref, prefix) {
		return 0
	}
	rest := strings.TrimPrefix(ref, prefix)
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		return 0
	}
	var value uint32
	for _, r := range parts[0] {
		if r < '0' || r > '9' {
			return 0
		}
		value = value*10 + uint32(r-'0')
	}
	return value
}

func normalizeCommitSHA(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 40 {
		return value[:40]
	}
	if value == "" {
		return strings.Repeat("0", 40)
	}
	return value + strings.Repeat("0", 40-len(value))
}

func clampInt8(value int) int8 {
	if value > 127 {
		return 127
	}
	if value < -128 {
		return -128
	}
	return int8(value)
}

func boolToUint8(value bool) uint8 {
	if value {
		return 1
	}
	return 0
}

func hostNodeID() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}
