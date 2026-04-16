package vmorchestrator

import (
	"strings"
	"time"

	"github.com/forge-metal/apiwire"
	vmrpc "github.com/forge-metal/vm-orchestrator/proto/v1"
)

func leaseSpecFromProto(spec *vmrpc.LeaseSpec, cfg Config) (LeaseSpec, error) {
	if spec == nil {
		return normalizeLeaseSpec(LeaseSpec{}, cfg)
	}
	networkMode := "nat"
	if spec.GetNetwork() != nil && spec.GetNetwork().GetMode() == vmrpc.NetworkAttachMode_NETWORK_ATTACH_MODE_NONE {
		networkMode = "none"
	}
	return normalizeLeaseSpec(LeaseSpec{
		Resources:               vmResourcesFromProto(spec.GetResources()),
		FromCheckpointRef:       spec.GetFromCheckpointRef(),
		TTLSeconds:              spec.GetTtlSeconds(),
		TrustClass:              spec.GetTrustClass(),
		CheckpointSaveAllowlist: append([]string(nil), spec.GetCheckpointSaveAllowlist()...),
		NetworkMode:             networkMode,
		FilesystemMounts:        filesystemMountsFromProto(spec.GetFilesystemMounts()),
	}, cfg)
}

func filesystemMountsFromProto(mounts []*vmrpc.FilesystemMount) []FilesystemMount {
	if len(mounts) == 0 {
		return nil
	}
	out := make([]FilesystemMount, 0, len(mounts))
	for _, mount := range mounts {
		if mount == nil {
			continue
		}
		out = append(out, FilesystemMount{
			Name:      mount.GetName(),
			SourceRef: mount.GetSourceRef(),
			MountPath: mount.GetMountPath(),
			FSType:    mount.GetFsType(),
			ReadOnly:  mount.GetReadOnly(),
		})
	}
	return out
}

func filesystemMountsToProto(mounts []FilesystemMount) []*vmrpc.FilesystemMount {
	if len(mounts) == 0 {
		return nil
	}
	out := make([]*vmrpc.FilesystemMount, 0, len(mounts))
	for _, mount := range mounts {
		out = append(out, &vmrpc.FilesystemMount{
			Name:      mount.Name,
			SourceRef: mount.SourceRef,
			MountPath: mount.MountPath,
			FsType:    mount.FSType,
			ReadOnly:  mount.ReadOnly,
		})
	}
	return out
}

func vmResourcesFromProto(r *vmrpc.VMResources) apiwire.VMResources {
	if r == nil {
		return apiwire.VMResources{}
	}
	return apiwire.VMResources{
		VCPUs:       r.GetVcpus(),
		MemoryMiB:   r.GetMemoryMib(),
		RootDiskGiB: r.GetRootDiskGib(),
		KernelImage: apiwire.KernelImageRef(r.GetKernelImage()),
	}
}

func vmResourcesToProto(r apiwire.VMResources) *vmrpc.VMResources {
	return &vmrpc.VMResources{
		Vcpus:       r.VCPUs,
		MemoryMib:   r.MemoryMiB,
		RootDiskGib: r.RootDiskGiB,
		KernelImage: string(r.KernelImage),
	}
}

func execSpecFromProto(spec *vmrpc.ExecSpec) ExecSpec {
	if spec == nil {
		return ExecSpec{}
	}
	return ExecSpec{
		Argv:           append([]string(nil), spec.GetArgv()...),
		WorkingDir:     spec.GetWorkingDir(),
		Env:            cloneStringMap(spec.GetEnv()),
		MaxWallSeconds: spec.GetMaxWallSeconds(),
	}
}

func acquireLeaseResponseFromRecord(record LeaseRecord) *vmrpc.AcquireLeaseResponse {
	return &vmrpc.AcquireLeaseResponse{
		LeaseId:          record.LeaseID,
		State:            leaseStateToProto(record.State),
		AcquiredAtUnixNs: uint64(record.AcquiredAt.UnixNano()),
		ExpiresAtUnixNs:  uint64(record.ExpiresAt.UnixNano()),
		VmIp:             record.VMIP,
		Resources:        vmResourcesToProto(record.Resources),
	}
}

func leaseSnapshotToProto(snap leaseSnapshot) *vmrpc.LeaseRecord {
	return &vmrpc.LeaseRecord{
		LeaseId:          snap.LeaseID,
		State:            leaseStateToProto(snap.State),
		AcquiredAtUnixNs: uint64(snap.AcquiredAt.UnixNano()),
		ReadyAtUnixNs:    uint64(snap.ReadyAt.UnixNano()),
		ExpiresAtUnixNs:  uint64(snap.ExpiresAt.UnixNano()),
		TerminalAtUnixNs: uint64(snap.TerminalAt.UnixNano()),
		TerminalReason:   snap.TerminalReason,
		VmIp:             snap.VMIP,
		Resources:        vmResourcesToProto(snap.Spec.Resources),
		TrustClass:       snap.TrustClass,
	}
}

func execSnapshotToProto(snap execSnapshot, includeOutput bool) *vmrpc.ExecRecord {
	output := ""
	if includeOutput {
		output = snap.Output
	}
	return &vmrpc.ExecRecord{
		LeaseId:                snap.LeaseID,
		ExecId:                 snap.ExecID,
		State:                  execStateToProto(snap.State),
		ExitCode:               int32(snap.ExitCode),
		TerminalReason:         snap.TerminalReason,
		QueuedAtUnixNs:         unixNs(snap.QueuedAt),
		StartedAtUnixNs:        unixNs(snap.StartedAt),
		FirstByteAtUnixNs:      unixNs(snap.FirstByteAt),
		ExitedAtUnixNs:         unixNs(snap.ExitedAt),
		StdoutBytes:            snap.StdoutBytes,
		StderrBytes:            snap.StderrBytes,
		DroppedLogBytes:        snap.DroppedLogBytes,
		Output:                 output,
		Metrics:                vmMetricsToProto(snap.Metrics),
		ZfsWritten:             snap.ZFSWritten,
		RootfsProvisionedBytes: snap.RootfsProvisionedBytes,
	}
}

func leaseEventToProto(leaseID string, event leaseEventRecord) *vmrpc.LeaseEvent {
	return &vmrpc.LeaseEvent{
		LeaseId:         leaseID,
		EventSeq:        event.Seq,
		EventType:       leaseEventTypeToProto(event.Type),
		ExecId:          event.ExecID,
		Attrs:           cloneStringMap(event.Attrs),
		CreatedAtUnixNs: uint64(event.CreatedAt.UnixNano()),
	}
}

func leaseStateToProto(state LeaseState) vmrpc.LeaseState {
	switch state {
	case LeaseStateAcquiring:
		return vmrpc.LeaseState_LEASE_STATE_ACQUIRING
	case LeaseStateReady:
		return vmrpc.LeaseState_LEASE_STATE_READY
	case LeaseStateDraining:
		return vmrpc.LeaseState_LEASE_STATE_DRAINING
	case LeaseStateReleased:
		return vmrpc.LeaseState_LEASE_STATE_RELEASED
	case LeaseStateExpired:
		return vmrpc.LeaseState_LEASE_STATE_EXPIRED
	case LeaseStateCrashed:
		return vmrpc.LeaseState_LEASE_STATE_CRASHED
	default:
		return vmrpc.LeaseState_LEASE_STATE_UNSPECIFIED
	}
}

func leaseStateFromProto(state vmrpc.LeaseState) LeaseState {
	switch state {
	case vmrpc.LeaseState_LEASE_STATE_ACQUIRING:
		return LeaseStateAcquiring
	case vmrpc.LeaseState_LEASE_STATE_READY:
		return LeaseStateReady
	case vmrpc.LeaseState_LEASE_STATE_DRAINING:
		return LeaseStateDraining
	case vmrpc.LeaseState_LEASE_STATE_RELEASED:
		return LeaseStateReleased
	case vmrpc.LeaseState_LEASE_STATE_EXPIRED:
		return LeaseStateExpired
	case vmrpc.LeaseState_LEASE_STATE_CRASHED:
		return LeaseStateCrashed
	default:
		return LeaseStateUnspecified
	}
}

func execStateToProto(state ExecState) vmrpc.ExecState {
	switch state {
	case ExecStatePending:
		return vmrpc.ExecState_EXEC_STATE_PENDING
	case ExecStateRunning:
		return vmrpc.ExecState_EXEC_STATE_RUNNING
	case ExecStateExited:
		return vmrpc.ExecState_EXEC_STATE_EXITED
	case ExecStateFailed:
		return vmrpc.ExecState_EXEC_STATE_FAILED
	case ExecStateCanceled:
		return vmrpc.ExecState_EXEC_STATE_CANCELED
	case ExecStateKilledByLeaseExpiry:
		return vmrpc.ExecState_EXEC_STATE_KILLED_BY_LEASE_EXPIRY
	default:
		return vmrpc.ExecState_EXEC_STATE_UNSPECIFIED
	}
}

func execStateFromProto(state vmrpc.ExecState) ExecState {
	switch state {
	case vmrpc.ExecState_EXEC_STATE_PENDING:
		return ExecStatePending
	case vmrpc.ExecState_EXEC_STATE_RUNNING:
		return ExecStateRunning
	case vmrpc.ExecState_EXEC_STATE_EXITED:
		return ExecStateExited
	case vmrpc.ExecState_EXEC_STATE_FAILED:
		return ExecStateFailed
	case vmrpc.ExecState_EXEC_STATE_CANCELED:
		return ExecStateCanceled
	case vmrpc.ExecState_EXEC_STATE_KILLED_BY_LEASE_EXPIRY:
		return ExecStateKilledByLeaseExpiry
	default:
		return ExecStateUnspecified
	}
}

func leaseEventTypeToProto(eventType LeaseEventType) vmrpc.LeaseEventType {
	switch eventType {
	case LeaseEventLeaseAcquired:
		return vmrpc.LeaseEventType_LEASE_EVENT_TYPE_LEASE_ACQUIRED
	case LeaseEventVMBooting:
		return vmrpc.LeaseEventType_LEASE_EVENT_TYPE_VM_BOOTING
	case LeaseEventVMReady:
		return vmrpc.LeaseEventType_LEASE_EVENT_TYPE_VM_READY
	case LeaseEventLeaseRenewed:
		return vmrpc.LeaseEventType_LEASE_EVENT_TYPE_LEASE_RENEWED
	case LeaseEventExecStarted:
		return vmrpc.LeaseEventType_LEASE_EVENT_TYPE_EXEC_STARTED
	case LeaseEventExecFinished:
		return vmrpc.LeaseEventType_LEASE_EVENT_TYPE_EXEC_FINISHED
	case LeaseEventExecCanceled:
		return vmrpc.LeaseEventType_LEASE_EVENT_TYPE_EXEC_CANCELED
	case LeaseEventCheckpointSaved:
		return vmrpc.LeaseEventType_LEASE_EVENT_TYPE_CHECKPOINT_SAVED
	case LeaseEventVMShutdown:
		return vmrpc.LeaseEventType_LEASE_EVENT_TYPE_VM_SHUTDOWN
	case LeaseEventLeaseExpired:
		return vmrpc.LeaseEventType_LEASE_EVENT_TYPE_LEASE_EXPIRED
	case LeaseEventLeaseReleased:
		return vmrpc.LeaseEventType_LEASE_EVENT_TYPE_LEASE_RELEASED
	case LeaseEventLeaseCrashed:
		return vmrpc.LeaseEventType_LEASE_EVENT_TYPE_LEASE_CRASHED
	case LeaseEventTelemetryDiagnostic:
		return vmrpc.LeaseEventType_LEASE_EVENT_TYPE_TELEMETRY_DIAGNOSTIC
	default:
		return vmrpc.LeaseEventType_LEASE_EVENT_TYPE_UNSPECIFIED
	}
}

func leaseEventTypeFromProto(eventType vmrpc.LeaseEventType) LeaseEventType {
	switch eventType {
	case vmrpc.LeaseEventType_LEASE_EVENT_TYPE_LEASE_ACQUIRED:
		return LeaseEventLeaseAcquired
	case vmrpc.LeaseEventType_LEASE_EVENT_TYPE_VM_BOOTING:
		return LeaseEventVMBooting
	case vmrpc.LeaseEventType_LEASE_EVENT_TYPE_VM_READY:
		return LeaseEventVMReady
	case vmrpc.LeaseEventType_LEASE_EVENT_TYPE_LEASE_RENEWED:
		return LeaseEventLeaseRenewed
	case vmrpc.LeaseEventType_LEASE_EVENT_TYPE_EXEC_STARTED:
		return LeaseEventExecStarted
	case vmrpc.LeaseEventType_LEASE_EVENT_TYPE_EXEC_FINISHED:
		return LeaseEventExecFinished
	case vmrpc.LeaseEventType_LEASE_EVENT_TYPE_EXEC_CANCELED:
		return LeaseEventExecCanceled
	case vmrpc.LeaseEventType_LEASE_EVENT_TYPE_CHECKPOINT_SAVED:
		return LeaseEventCheckpointSaved
	case vmrpc.LeaseEventType_LEASE_EVENT_TYPE_VM_SHUTDOWN:
		return LeaseEventVMShutdown
	case vmrpc.LeaseEventType_LEASE_EVENT_TYPE_LEASE_EXPIRED:
		return LeaseEventLeaseExpired
	case vmrpc.LeaseEventType_LEASE_EVENT_TYPE_LEASE_RELEASED:
		return LeaseEventLeaseReleased
	case vmrpc.LeaseEventType_LEASE_EVENT_TYPE_LEASE_CRASHED:
		return LeaseEventLeaseCrashed
	case vmrpc.LeaseEventType_LEASE_EVENT_TYPE_TELEMETRY_DIAGNOSTIC:
		return LeaseEventTelemetryDiagnostic
	default:
		return ""
	}
}

func vmMetricsToProto(metrics *VMMetrics) *vmrpc.VMMetrics {
	if metrics == nil {
		return nil
	}
	return &vmrpc.VMMetrics{
		BootTimeUs:      metrics.BootTimeUs,
		BlockReadBytes:  metrics.BlockReadBytes,
		BlockWriteBytes: metrics.BlockWriteBytes,
		BlockReadCount:  metrics.BlockReadCount,
		BlockWriteCount: metrics.BlockWriteCount,
		NetRxBytes:      metrics.NetRxBytes,
		NetTxBytes:      metrics.NetTxBytes,
		VcpuExitCount:   metrics.VCPUExitCount,
	}
}

func vmMetricsFromProto(metrics *vmrpc.VMMetrics) *VMMetrics {
	if metrics == nil {
		return nil
	}
	return &VMMetrics{
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

func unixNs(t time.Time) uint64 {
	if t.IsZero() {
		return 0
	}
	return uint64(t.UTC().UnixNano())
}

func timeFromUnixNs(value uint64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.Unix(0, int64(value)).UTC()
}

func trimStringMap(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = value
	}
	return out
}
