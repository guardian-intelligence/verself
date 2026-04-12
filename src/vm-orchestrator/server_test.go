package vmorchestrator

import (
	"strings"
	"testing"
	"time"
)

func TestJobObserverEmitsBillablePhaseEvents(t *testing.T) {
	job := &managedJob{
		id:             "job-1",
		state:          JobStateRunning,
		billablePhases: map[string]struct{}{"run": {}},
	}
	observer := &jobObserver{job: job}

	observer.OnGuestPhaseStart("job-1", "run")
	observer.OnGuestPhaseEnd("job-1", PhaseResult{Name: "run", ExitCode: 0, DurationMS: 1234})

	events, terminal := job.guestEventSnapshot(0)
	if terminal {
		t.Fatal("running job reported terminal guest event stream")
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 phase events, got %d", len(events))
	}

	start := events[0]
	if start.Kind != "phase_started" {
		t.Fatalf("expected first event kind=phase_started, got %q", start.Kind)
	}
	if start.Attrs["phase"] != "run" {
		t.Fatalf("expected phase attr run, got %q", start.Attrs["phase"])
	}
	if start.Attrs["billable"] != "true" {
		t.Fatalf("expected billable attr true, got %q", start.Attrs["billable"])
	}
	if start.Attrs["host_received_unix_nano"] == "" {
		t.Fatal("expected host_received_unix_nano on phase start")
	}

	end := events[1]
	if end.Kind != "phase_ended" {
		t.Fatalf("expected second event kind=phase_ended, got %q", end.Kind)
	}
	if end.Attrs["phase"] != "run" {
		t.Fatalf("expected phase attr run, got %q", end.Attrs["phase"])
	}
	if end.Attrs["billable"] != "true" {
		t.Fatalf("expected billable attr true, got %q", end.Attrs["billable"])
	}
	if end.Attrs["exit_code"] != "0" {
		t.Fatalf("expected exit_code attr 0, got %q", end.Attrs["exit_code"])
	}
	if end.Attrs["duration_ms"] != "1234" {
		t.Fatalf("expected duration_ms attr 1234, got %q", end.Attrs["duration_ms"])
	}
	if end.Attrs["host_received_unix_nano"] == "" {
		t.Fatal("expected host_received_unix_nano on phase end")
	}
}

func TestManagedJobCapsBufferedLogs(t *testing.T) {
	t.Parallel()

	job := &managedJob{id: "job-1", state: JobStateRunning}
	job.appendLogChunk(strings.Repeat("x", maxBufferedGuestLogs+1024))

	chunks, terminal := job.logSnapshot(0)
	if terminal {
		t.Fatal("running job reported terminal log stream")
	}
	if len(chunks) == 0 {
		t.Fatal("expected retained log chunks")
	}

	var total int
	for _, chunk := range chunks {
		total += len(chunk.Chunk)
	}
	if total > maxBufferedGuestLogs {
		t.Fatalf("buffered logs exceeded cap: got %d want <= %d", total, maxBufferedGuestLogs)
	}
	if !strings.Contains(chunks[len(chunks)-1].Chunk, "truncated") {
		t.Fatalf("expected truncation marker in final chunk, got %q", chunks[len(chunks)-1].Chunk)
	}
}

func TestJobResultProtoRoundTripIncludesRootfsProvisionedBytes(t *testing.T) {
	t.Parallel()

	original := JobResult{
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

	proto := jobResultToProto(original, false)
	if got := proto.GetRootfsProvisionedBytes(); got != original.RootfsProvisionedBytes {
		t.Fatalf("rootfs_provisioned_bytes = %d, want %d", got, original.RootfsProvisionedBytes)
	}
	if got := proto.GetZfsWritten(); got != original.ZFSWritten {
		t.Fatalf("zfs_written = %d, want %d", got, original.ZFSWritten)
	}

	roundTrip := jobResultFromProto(proto)
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
