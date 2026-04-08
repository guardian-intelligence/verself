package vmorchestrator

import (
	"time"

	vmrpc "github.com/forge-metal/vm-orchestrator/proto/v1"
)

func jobConfigToProto(job JobConfig) *vmrpc.JobConfig {
	return &vmrpc.JobConfig{
		JobId:          job.JobID,
		PrepareCommand: cloneStringSlice(job.PrepareCommand),
		PrepareWorkDir: job.PrepareWorkDir,
		RunCommand:     cloneStringSlice(job.RunCommand),
		RunWorkDir:     job.RunWorkDir,
		Services:       cloneStringSlice(job.Services),
		Env:            cloneStringMap(job.Env),
	}
}

func jobConfigFromProto(job *vmrpc.JobConfig) JobConfig {
	if job == nil {
		return JobConfig{}
	}
	return JobConfig{
		JobID:          job.GetJobId(),
		PrepareCommand: cloneStringSlice(job.GetPrepareCommand()),
		PrepareWorkDir: job.GetPrepareWorkDir(),
		RunCommand:     cloneStringSlice(job.GetRunCommand()),
		RunWorkDir:     job.GetRunWorkDir(),
		Services:       cloneStringSlice(job.GetServices()),
		Env:            cloneStringMap(job.GetEnv()),
	}
}

func jobResultToProto(result JobResult, includeOutput bool) *vmrpc.JobResult {
	out := &vmrpc.JobResult{
		ExitCode:               int32(result.ExitCode),
		DurationMs:             result.Duration.Milliseconds(),
		CloneTimeMs:            result.CloneTime.Milliseconds(),
		JailSetupTimeMs:        result.JailSetupTime.Milliseconds(),
		VmBootTimeMs:           result.VMBootTime.Milliseconds(),
		BootToReadyDurationMs:  result.BootToReadyDuration.Milliseconds(),
		PrepareDurationMs:      result.PrepareDuration.Milliseconds(),
		RunDurationMs:          result.RunDuration.Milliseconds(),
		ServiceStartDurationMs: result.ServiceStartDuration.Milliseconds(),
		VmExitWaitDurationMs:   result.VMExitWaitDuration.Milliseconds(),
		CleanupTimeMs:          result.CleanupTime.Milliseconds(),
		ZfsWritten:             result.ZFSWritten,
		StdoutBytes:            result.StdoutBytes,
		StderrBytes:            result.StderrBytes,
		DroppedLogBytes:        result.DroppedLogBytes,
		ForcedShutdown:         result.ForcedShutdown,
		FailurePhase:           result.FailurePhase,
	}
	if includeOutput {
		out.Logs = result.Logs
		out.SerialLogs = result.SerialLogs
	}
	if result.Metrics != nil {
		out.Metrics = &vmrpc.VMMetrics{
			BootTimeUs:      result.Metrics.BootTimeUs,
			BlockReadBytes:  result.Metrics.BlockReadBytes,
			BlockWriteBytes: result.Metrics.BlockWriteBytes,
			BlockReadCount:  result.Metrics.BlockReadCount,
			BlockWriteCount: result.Metrics.BlockWriteCount,
			NetRxBytes:      result.Metrics.NetRxBytes,
			NetTxBytes:      result.Metrics.NetTxBytes,
			VcpuExitCount:   result.Metrics.VCPUExitCount,
		}
	}
	if len(result.PhaseResults) > 0 {
		out.PhaseResults = make([]*vmrpc.PhaseResult, 0, len(result.PhaseResults))
		for _, phase := range result.PhaseResults {
			out.PhaseResults = append(out.PhaseResults, &vmrpc.PhaseResult{
				Name:       phase.Name,
				ExitCode:   int32(phase.ExitCode),
				DurationMs: phase.DurationMS,
			})
		}
	}
	return out
}

func jobResultFromProto(result *vmrpc.JobResult) *JobResult {
	if result == nil {
		return nil
	}
	out := &JobResult{
		ExitCode:             int(result.GetExitCode()),
		Logs:                 result.GetLogs(),
		SerialLogs:           result.GetSerialLogs(),
		Duration:             time.Duration(result.GetDurationMs()) * time.Millisecond,
		CloneTime:            time.Duration(result.GetCloneTimeMs()) * time.Millisecond,
		JailSetupTime:        time.Duration(result.GetJailSetupTimeMs()) * time.Millisecond,
		VMBootTime:           time.Duration(result.GetVmBootTimeMs()) * time.Millisecond,
		BootToReadyDuration:  time.Duration(result.GetBootToReadyDurationMs()) * time.Millisecond,
		PrepareDuration:      time.Duration(result.GetPrepareDurationMs()) * time.Millisecond,
		RunDuration:          time.Duration(result.GetRunDurationMs()) * time.Millisecond,
		ServiceStartDuration: time.Duration(result.GetServiceStartDurationMs()) * time.Millisecond,
		VMExitWaitDuration:   time.Duration(result.GetVmExitWaitDurationMs()) * time.Millisecond,
		CleanupTime:          time.Duration(result.GetCleanupTimeMs()) * time.Millisecond,
		ZFSWritten:           result.GetZfsWritten(),
		StdoutBytes:          result.GetStdoutBytes(),
		StderrBytes:          result.GetStderrBytes(),
		DroppedLogBytes:      result.GetDroppedLogBytes(),
		ForcedShutdown:       result.GetForcedShutdown(),
		FailurePhase:         result.GetFailurePhase(),
	}
	if metrics := result.GetMetrics(); metrics != nil {
		out.Metrics = &VMMetrics{
			BootTimeUs:      metrics.GetBootTimeUs(),
			BlockReadBytes:  metrics.GetBlockReadBytes(),
			BlockWriteBytes: metrics.GetBlockWriteBytes(),
			BlockReadCount:  metrics.GetBlockReadCount(),
			BlockWriteCount: metrics.GetBlockWriteCount(),
			NetRxBytes:      metrics.GetNetRxBytes(),
			NetTxBytes:      metrics.GetNetTxBytes(),
			VCPUExitCount:   metrics.GetVcpuExitCount(),
		}
	}
	if phases := result.GetPhaseResults(); len(phases) > 0 {
		out.PhaseResults = make([]PhaseResult, 0, len(phases))
		for _, phase := range phases {
			out.PhaseResults = append(out.PhaseResults, PhaseResult{
				Name:       phase.GetName(),
				ExitCode:   int(phase.GetExitCode()),
				DurationMS: phase.GetDurationMs(),
			})
		}
	}
	return out
}

func jobStateToProto(state JobState) vmrpc.JobState {
	switch state {
	case JobStatePending:
		return vmrpc.JobState_JOB_STATE_PENDING
	case JobStateRunning:
		return vmrpc.JobState_JOB_STATE_RUNNING
	case JobStateSucceeded:
		return vmrpc.JobState_JOB_STATE_SUCCEEDED
	case JobStateFailed:
		return vmrpc.JobState_JOB_STATE_FAILED
	case JobStateCanceled:
		return vmrpc.JobState_JOB_STATE_CANCELED
	default:
		return vmrpc.JobState_JOB_STATE_UNSPECIFIED
	}
}

func jobStateFromProto(state vmrpc.JobState) JobState {
	switch state {
	case vmrpc.JobState_JOB_STATE_PENDING:
		return JobStatePending
	case vmrpc.JobState_JOB_STATE_RUNNING:
		return JobStateRunning
	case vmrpc.JobState_JOB_STATE_SUCCEEDED:
		return JobStateSucceeded
	case vmrpc.JobState_JOB_STATE_FAILED:
		return JobStateFailed
	case vmrpc.JobState_JOB_STATE_CANCELED:
		return JobStateCanceled
	default:
		return JobStateUnspecified
	}
}

func jobStatusFromProto(resp *vmrpc.GetJobStatusResponse) JobStatus {
	status := JobStatus{
		JobID:        resp.GetJobId(),
		State:        jobStateFromProto(resp.GetState()),
		Terminal:     resp.GetTerminal(),
		ErrorMessage: resp.GetErrorMessage(),
		Result:       jobResultFromProto(resp.GetResult()),
	}
	if repo := resp.GetRepoExec(); repo != nil {
		status.RepoExec = &RepoExecMetadata{
			Repo:           repo.GetRepo(),
			RepoURL:        repo.GetRepoUrl(),
			Ref:            repo.GetRef(),
			GoldenSnapshot: repo.GetGoldenSnapshot(),
			CloneDuration:  time.Duration(repo.GetCloneDurationMs()) * time.Millisecond,
			InstallNeeded:  repo.GetInstallNeeded(),
			CommitSHA:      repo.GetCommitSha(),
		}
	}
	return status
}

func warmGoldenResultFromProto(resp *vmrpc.WarmGoldenResponse) WarmGoldenResult {
	var result JobResult
	if decoded := jobResultFromProto(resp.GetJobResult()); decoded != nil {
		result = *decoded
	}
	return WarmGoldenResult{
		TargetDataset:             resp.GetTargetDataset(),
		PreviousDataset:           resp.GetPreviousDataset(),
		Promoted:                  resp.GetPromoted(),
		FilesystemCheckOK:         resp.GetFilesystemCheckOk(),
		CloneDuration:             time.Duration(resp.GetCloneDurationMs()) * time.Millisecond,
		FilesystemCheckDuration:   time.Duration(resp.GetFilesystemCheckDurationMs()) * time.Millisecond,
		SnapshotPromotionDuration: time.Duration(resp.GetSnapshotPromotionDurationMs()) * time.Millisecond,
		PreviousDestroyDuration:   time.Duration(resp.GetPreviousDestroyDurationMs()) * time.Millisecond,
		CommitSHA:                 resp.GetCommitSha(),
		JobResult:                 result,
		ErrorMessage:              resp.GetErrorMessage(),
	}
}

func telemetryHelloToProto(frame TelemetryHello) *vmrpc.TelemetryHello {
	return &vmrpc.TelemetryHello{
		Seq:        frame.Seq,
		Flags:      frame.Flags,
		MonoNs:     frame.MonoNS,
		WallNs:     frame.WallNS,
		BootId:     frame.BootID,
		MemTotalKb: frame.MemTotalKB,
	}
}

func telemetrySampleToProto(frame TelemetrySample) *vmrpc.TelemetrySample {
	return &vmrpc.TelemetrySample{
		Seq:            frame.Seq,
		Flags:          frame.Flags,
		MonoNs:         frame.MonoNS,
		WallNs:         frame.WallNS,
		CpuUserTicks:   frame.CPUUserTicks,
		CpuSystemTicks: frame.CPUSystemTicks,
		CpuIdleTicks:   frame.CPUIdleTicks,
		Load1Centis:    frame.Load1Centis,
		Load5Centis:    frame.Load5Centis,
		Load15Centis:   frame.Load15Centis,
		ProcsRunning:   uint32(frame.ProcsRunning),
		ProcsBlocked:   uint32(frame.ProcsBlocked),
		MemAvailableKb: frame.MemAvailableKB,
		IoReadBytes:    frame.IOReadBytes,
		IoWriteBytes:   frame.IOWriteBytes,
		NetRxBytes:     frame.NetRXBytes,
		NetTxBytes:     frame.NetTXBytes,
		PsiCpuPct100:   uint32(frame.PSICPUPct100),
		PsiMemPct100:   uint32(frame.PSIMemPct100),
		PsiIoPct100:    uint32(frame.PSIIOPct100),
	}
}

func telemetryHelloFromProto(frame *vmrpc.TelemetryHello) *TelemetryHello {
	if frame == nil {
		return nil
	}
	return &TelemetryHello{
		Seq:        frame.GetSeq(),
		Flags:      frame.GetFlags(),
		MonoNS:     frame.GetMonoNs(),
		WallNS:     frame.GetWallNs(),
		BootID:     frame.GetBootId(),
		MemTotalKB: frame.GetMemTotalKb(),
	}
}

func telemetrySampleFromProto(frame *vmrpc.TelemetrySample) *TelemetrySample {
	if frame == nil {
		return nil
	}
	return &TelemetrySample{
		Seq:            frame.GetSeq(),
		Flags:          frame.GetFlags(),
		MonoNS:         frame.GetMonoNs(),
		WallNS:         frame.GetWallNs(),
		CPUUserTicks:   frame.GetCpuUserTicks(),
		CPUSystemTicks: frame.GetCpuSystemTicks(),
		CPUIdleTicks:   frame.GetCpuIdleTicks(),
		Load1Centis:    frame.GetLoad1Centis(),
		Load5Centis:    frame.GetLoad5Centis(),
		Load15Centis:   frame.GetLoad15Centis(),
		ProcsRunning:   uint16(frame.GetProcsRunning()),
		ProcsBlocked:   uint16(frame.GetProcsBlocked()),
		MemAvailableKB: frame.GetMemAvailableKb(),
		IOReadBytes:    frame.GetIoReadBytes(),
		IOWriteBytes:   frame.GetIoWriteBytes(),
		NetRXBytes:     frame.GetNetRxBytes(),
		NetTXBytes:     frame.GetNetTxBytes(),
		PSICPUPct100:   uint16(frame.GetPsiCpuPct100()),
		PSIMemPct100:   uint16(frame.GetPsiMemPct100()),
		PSIIOPct100:    uint16(frame.GetPsiIoPct100()),
	}
}

func fleetVMFromProto(vm *vmrpc.FleetVM) FleetVM {
	return FleetVM{
		JobID:        vm.GetJobId(),
		State:        jobStateFromProto(vm.GetState()),
		LastUpdateAt: time.Unix(0, int64(vm.GetLastUpdateUnixNano())).UTC(),
		Hello:        telemetryHelloFromProto(vm.GetHello()),
		LatestSample: telemetrySampleFromProto(vm.GetLatestSample()),
	}
}
