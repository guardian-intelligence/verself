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
	"github.com/forge-metal/forge-metal/internal/firecracker"
	"github.com/google/uuid"
)

type emitExecTelemetryInput struct {
	FirecrackerConfig firecracker.Config
	Request           ExecRequest
	RunID             string
	Manifest          *Manifest
	Toolchain         *Toolchain
	InstallNeeded     bool
	GoldenSnapshot    string
	Job               firecracker.JobConfig
	JobResult         firecracker.JobResult
	CloneDuration     time.Duration
	CreatedAt         time.Time
	StartedAt         time.Time
	CompletedAt       time.Time
	CommitSHA         string
	PRNumber          uint32
	RunErr            error
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

	jobConfigJSON, err := buildExecJobConfigJSON(input)
	if err != nil {
		return err
	}

	event := &ch.CIEvent{
		JobID:           jobUUID,
		RunID:           input.RunID,
		NodeID:          hostNodeID(),
		Region:          cfg.Latitude.Region,
		Plan:            cfg.Latitude.Plan,
		Repo:            input.Request.Repo,
		Branch:          input.Request.Ref,
		CommitSHA:       normalizeCommitSHA(input.CommitSHA),
		PRNumber:        input.PRNumber,
		BaseBranch:      "",
		ZFSCloneNs:      input.CloneDuration.Nanoseconds(),
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

func buildExecJobConfigJSON(input emitExecTelemetryInput) (string, error) {
	payload := map[string]any{
		"run_id":                  input.RunID,
		"repo":                    input.Request.Repo,
		"ref":                     input.Request.Ref,
		"commit_sha":              input.CommitSHA,
		"pr_number":               input.PRNumber,
		"manifest_workdir":        input.Manifest.WorkDir,
		"manifest_services":       input.Manifest.Services,
		"manifest_env":            input.Manifest.Env,
		"manifest_profile":        string(input.Manifest.Profile),
		"resolved_profile":        string(resolvedProfile(input.Manifest)),
		"package_manager":         string(input.Toolchain.PackageManager),
		"package_manager_version": input.Toolchain.PackageManagerVersion,
		"node_version":            input.Toolchain.NodeVersion,
		"install_needed":          input.InstallNeeded,
		"job_prepare_command":     input.Job.PrepareCommand,
		"job_prepare_workdir":     input.Job.PrepareWorkDir,
		"job_run_command":         input.Job.RunCommand,
		"job_run_workdir":         input.Job.RunWorkDir,
		"job_transport":           strings.TrimSpace(input.JobResult.ConfigTransport),
		"job_services":            input.Job.Services,
		"job_env_names":           sortedEnvKeys(input.Job.Env),
	}
	if input.RunErr != nil {
		payload["run_error"] = input.RunErr.Error()
	}
	data, err := jsonMarshalIndent(payload)
	if err != nil {
		return "", fmt.Errorf("marshal exec telemetry payload: %w", err)
	}
	return string(data), nil
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

func endToEndDuration(input emitExecTelemetryInput) time.Duration {
	if input.CompletedAt.Before(input.CreatedAt) {
		return 0
	}
	return input.CompletedAt.Sub(input.CreatedAt)
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
