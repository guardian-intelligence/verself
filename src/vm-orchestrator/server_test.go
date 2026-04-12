package vmorchestrator

import (
	"testing"
	"time"
)

func TestHostRunSpecProtoRoundTrip(t *testing.T) {
	t.Parallel()

	original := HostRunSpec{
		RunID:              "5c80f2bf-b22c-4ec5-bf9a-61578a7f7158",
		RunCommand:         []string{"sh", "-c", "echo hello"},
		RunWorkDir:         "/workspace",
		Env:                map[string]string{"A": "1", "B": "2"},
		BillablePhases:     []string{"prepare", "run"},
		CheckpointSaveRefs: []string{"checkpoints/default"},
		AttemptID:          "attempt-1",
		SegmentID:          "segment-2",
	}

	proto := hostRunSpecToProto(original)
	roundTrip := hostRunSpecFromProto(proto)

	if roundTrip.RunID != original.RunID {
		t.Fatalf("run_id = %q, want %q", roundTrip.RunID, original.RunID)
	}
	if len(roundTrip.RunCommand) != len(original.RunCommand) {
		t.Fatalf("run_command length = %d, want %d", len(roundTrip.RunCommand), len(original.RunCommand))
	}
	if roundTrip.RunWorkDir != original.RunWorkDir {
		t.Fatalf("run_work_dir = %q, want %q", roundTrip.RunWorkDir, original.RunWorkDir)
	}
	if roundTrip.Env["A"] != "1" || roundTrip.Env["B"] != "2" {
		t.Fatalf("env round-trip mismatch: %#v", roundTrip.Env)
	}
	if roundTrip.AttemptID != original.AttemptID {
		t.Fatalf("attempt_id = %q, want %q", roundTrip.AttemptID, original.AttemptID)
	}
	if roundTrip.SegmentID != original.SegmentID {
		t.Fatalf("segment_id = %q, want %q", roundTrip.SegmentID, original.SegmentID)
	}
}

func TestHostRunResultProtoRoundTripIncludesRootfsProvisionedBytes(t *testing.T) {
	t.Parallel()

	original := RunResult{
		ExitCode:               7,
		Duration:               3 * time.Second,
		CloneTime:              100 * time.Millisecond,
		JailSetupTime:          200 * time.Millisecond,
		VMBootTime:             300 * time.Millisecond,
		BootToReadyDuration:    400 * time.Millisecond,
		RunDuration:            500 * time.Millisecond,
		VMExitWaitDuration:     600 * time.Millisecond,
		CleanupTime:            700 * time.Millisecond,
		ZFSWritten:             12345,
		RootfsProvisionedBytes: 67890,
		StdoutBytes:            11,
		StderrBytes:            22,
		DroppedLogBytes:        33,
		ForcedShutdown:         true,
		FailurePhase:           "run",
		Metrics: &VMMetrics{
			BootTimeUs:      44,
			BlockReadBytes:  55,
			BlockWriteBytes: 66,
			BlockReadCount:  77,
			BlockWriteCount: 88,
			NetRxBytes:      99,
			NetTxBytes:      111,
			VCPUExitCount:   222,
		},
	}

	proto := runResultToProto(original, false)
	if got := proto.GetRootfsProvisionedBytes(); got != original.RootfsProvisionedBytes {
		t.Fatalf("rootfs_provisioned_bytes = %d, want %d", got, original.RootfsProvisionedBytes)
	}
	if got := proto.GetZfsWritten(); got != original.ZFSWritten {
		t.Fatalf("zfs_written = %d, want %d", got, original.ZFSWritten)
	}

	roundTrip := runResultFromProto(proto)
	if roundTrip == nil {
		t.Fatal("expected round-trip result")
	}
	if got := roundTrip.RootfsProvisionedBytes; got != original.RootfsProvisionedBytes {
		t.Fatalf("round trip rootfs_provisioned_bytes = %d, want %d", got, original.RootfsProvisionedBytes)
	}
	if got := roundTrip.ZFSWritten; got != original.ZFSWritten {
		t.Fatalf("round trip zfs_written = %d, want %d", got, original.ZFSWritten)
	}
	if roundTrip.Metrics == nil || roundTrip.Metrics.BootTimeUs != original.Metrics.BootTimeUs {
		t.Fatalf("round trip metrics = %#v, want boot_time_us %d", roundTrip.Metrics, original.Metrics.BootTimeUs)
	}
}
