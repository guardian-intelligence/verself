package vmorchestrator

import (
	"time"

	vmrpc "github.com/forge-metal/vm-orchestrator/proto/v1"
)

func hostRunSpecToProto(spec HostRunSpec) *vmrpc.HostRunSpec {
	return &vmrpc.HostRunSpec{
		RunId:              spec.RunID,
		WorkloadKind:       spec.WorkloadKind,
		RunnerClass:        spec.RunnerClass,
		RunCommand:         cloneStringSlice(spec.RunCommand),
		RunWorkDir:         spec.RunWorkDir,
		Env:                cloneStringMap(spec.Env),
		WorkflowYaml:       spec.WorkflowYAML,
		WorkflowEnv:        cloneStringMap(spec.WorkflowEnv),
		WorkflowSecrets:    cloneStringMap(spec.WorkflowSecrets),
		WorkflowEventName:  spec.WorkflowEventName,
		WorkflowInputs:     cloneStringMap(spec.WorkflowInputs),
		GithubJitConfig:    spec.GitHubJITConfig,
		BillablePhases:     cloneStringSlice(spec.BillablePhases),
		CheckpointSaveRefs: cloneStringSlice(spec.CheckpointSaveRefs),
		AttemptId:          spec.AttemptID,
		SegmentId:          spec.SegmentID,
	}
}

func hostRunSpecFromProto(spec *vmrpc.HostRunSpec) HostRunSpec {
	if spec == nil {
		return HostRunSpec{}
	}
	return HostRunSpec{
		RunID:              spec.GetRunId(),
		WorkloadKind:       spec.GetWorkloadKind(),
		RunnerClass:        spec.GetRunnerClass(),
		RunCommand:         cloneStringSlice(spec.GetRunCommand()),
		RunWorkDir:         spec.GetRunWorkDir(),
		Env:                cloneStringMap(spec.GetEnv()),
		WorkflowYAML:       spec.GetWorkflowYaml(),
		WorkflowEnv:        cloneStringMap(spec.GetWorkflowEnv()),
		WorkflowSecrets:    cloneStringMap(spec.GetWorkflowSecrets()),
		WorkflowEventName:  spec.GetWorkflowEventName(),
		WorkflowInputs:     cloneStringMap(spec.GetWorkflowInputs()),
		GitHubJITConfig:    spec.GetGithubJitConfig(),
		BillablePhases:     cloneStringSlice(spec.GetBillablePhases()),
		CheckpointSaveRefs: cloneStringSlice(spec.GetCheckpointSaveRefs()),
		AttemptID:          spec.GetAttemptId(),
		SegmentID:          spec.GetSegmentId(),
	}
}

func runSpecFromHostRunSpec(spec HostRunSpec) RunSpec {
	return RunSpec{
		RunID:              spec.RunID,
		WorkloadKind:       spec.WorkloadKind,
		RunnerClass:        spec.RunnerClass,
		RunCommand:         cloneStringSlice(spec.RunCommand),
		RunWorkDir:         spec.RunWorkDir,
		Env:                cloneStringMap(spec.Env),
		WorkflowYAML:       spec.WorkflowYAML,
		WorkflowEnv:        cloneStringMap(spec.WorkflowEnv),
		WorkflowSecrets:    cloneStringMap(spec.WorkflowSecrets),
		WorkflowEventName:  spec.WorkflowEventName,
		WorkflowInputs:     cloneStringMap(spec.WorkflowInputs),
		GitHubJITConfig:    spec.GitHubJITConfig,
		BillablePhases:     cloneStringSlice(spec.BillablePhases),
		CheckpointSaveRefs: cloneStringSlice(spec.CheckpointSaveRefs),
	}
}

func runResultToProto(result RunResult, includeOutput bool) *vmrpc.HostRunResult {
	out := &vmrpc.HostRunResult{
		ExitCode:               int32(result.ExitCode),
		DurationMs:             result.Duration.Milliseconds(),
		CloneTimeMs:            result.CloneTime.Milliseconds(),
		JailSetupTimeMs:        result.JailSetupTime.Milliseconds(),
		VmBootTimeMs:           result.VMBootTime.Milliseconds(),
		BootToReadyDurationMs:  result.BootToReadyDuration.Milliseconds(),
		RunDurationMs:          result.RunDuration.Milliseconds(),
		VmExitWaitDurationMs:   result.VMExitWaitDuration.Milliseconds(),
		CleanupTimeMs:          result.CleanupTime.Milliseconds(),
		ZfsWritten:             result.ZFSWritten,
		RootfsProvisionedBytes: result.RootfsProvisionedBytes,
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

func runResultFromProto(result *vmrpc.HostRunResult) *RunResult {
	if result == nil {
		return nil
	}
	out := &RunResult{
		ExitCode:               int(result.GetExitCode()),
		Logs:                   result.GetLogs(),
		SerialLogs:             result.GetSerialLogs(),
		Duration:               time.Duration(result.GetDurationMs()) * time.Millisecond,
		CloneTime:              time.Duration(result.GetCloneTimeMs()) * time.Millisecond,
		JailSetupTime:          time.Duration(result.GetJailSetupTimeMs()) * time.Millisecond,
		VMBootTime:             time.Duration(result.GetVmBootTimeMs()) * time.Millisecond,
		BootToReadyDuration:    time.Duration(result.GetBootToReadyDurationMs()) * time.Millisecond,
		RunDuration:            time.Duration(result.GetRunDurationMs()) * time.Millisecond,
		VMExitWaitDuration:     time.Duration(result.GetVmExitWaitDurationMs()) * time.Millisecond,
		CleanupTime:            time.Duration(result.GetCleanupTimeMs()) * time.Millisecond,
		ZFSWritten:             result.GetZfsWritten(),
		RootfsProvisionedBytes: result.GetRootfsProvisionedBytes(),
		StdoutBytes:            result.GetStdoutBytes(),
		StderrBytes:            result.GetStderrBytes(),
		DroppedLogBytes:        result.GetDroppedLogBytes(),
		ForcedShutdown:         result.GetForcedShutdown(),
		FailurePhase:           result.GetFailurePhase(),
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

func runStateToProto(state RunState) vmrpc.RunState {
	switch state {
	case RunStatePending:
		return vmrpc.RunState_RUN_STATE_PENDING
	case RunStateRunning:
		return vmrpc.RunState_RUN_STATE_RUNNING
	case RunStateSucceeded:
		return vmrpc.RunState_RUN_STATE_SUCCEEDED
	case RunStateFailed:
		return vmrpc.RunState_RUN_STATE_FAILED
	case RunStateCanceled:
		return vmrpc.RunState_RUN_STATE_CANCELED
	default:
		return vmrpc.RunState_RUN_STATE_UNSPECIFIED
	}
}

func runStateFromProto(state vmrpc.RunState) RunState {
	switch state {
	case vmrpc.RunState_RUN_STATE_PENDING:
		return RunStatePending
	case vmrpc.RunState_RUN_STATE_RUNNING:
		return RunStateRunning
	case vmrpc.RunState_RUN_STATE_SUCCEEDED:
		return RunStateSucceeded
	case vmrpc.RunState_RUN_STATE_FAILED:
		return RunStateFailed
	case vmrpc.RunState_RUN_STATE_CANCELED:
		return RunStateCanceled
	default:
		return RunStateUnspecified
	}
}

func hostRunSnapshotFromProto(resp *vmrpc.GetRunResponse) HostRunSnapshot {
	if resp == nil {
		return HostRunSnapshot{}
	}
	return HostRunSnapshot{
		RunID:          resp.GetRunId(),
		State:          runStateFromProto(resp.GetState()),
		Terminal:       resp.GetTerminal(),
		TerminalReason: resp.GetTerminalReason(),
		Result:         runResultFromProto(resp.GetResult()),
		UpdatedAt:      time.Unix(0, int64(resp.GetUpdatedAtUnixNano())).UTC(),
	}
}

func hostRunEventFromProto(event *vmrpc.HostRunEvent) HostRunEvent {
	if event == nil {
		return HostRunEvent{}
	}
	return HostRunEvent{
		Seq:       event.GetEventSeq(),
		RunID:     event.GetRunId(),
		EventType: event.GetEventType(),
		Attrs:     cloneStringMap(event.GetAttrs()),
		CreatedAt: time.Unix(0, int64(event.GetCreatedAtUnixNano())).UTC(),
	}
}
