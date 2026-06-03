package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/verself/vm-orchestrator/vmproto"
)

func TestParseChronyTracking(t *testing.T) {
	const tracking = `Reference ID    : 4B564D00 (KVM)
Stratum         : 1
Ref time (UTC)  : Sat May 23 20:15:00 2026
System time     : 0.000006523 seconds slow of NTP time
Last offset     : -0.000006747 seconds
RMS offset      : 0.000035822 seconds
Frequency       : 3.225 ppm slow
Residual freq   : -0.000 ppm
Skew            : 0.129 ppm
Root delay      : 0.000000001 seconds
Root dispersion : 0.000001100 seconds
Update interval : 1.0 seconds
Leap status     : Normal
`

	result, err := parseChronyTracking(tracking)
	if err != nil {
		t.Fatalf("parse tracking: %v", err)
	}
	if result.Source != "KVM" {
		t.Fatalf("source = %q, want KVM", result.Source)
	}
	if result.OffsetNS != -6523 {
		t.Fatalf("offset ns = %d, want -6523", result.OffsetNS)
	}
	if result.SkewPPM != 0.129 {
		t.Fatalf("skew ppm = %f, want 0.129", result.SkewPPM)
	}
	if result.LeapStatus != "Normal" {
		t.Fatalf("leap status = %q, want Normal", result.LeapStatus)
	}
}

func TestParseChronyTrackingRejectsMalformedSystemTime(t *testing.T) {
	_, err := parseChronyTracking("System time     : not-a-number seconds slow of NTP time\n")
	if err == nil {
		t.Fatal("parse tracking returned nil error")
	}
}

func TestParseChronyTrackingRequiresGateFields(t *testing.T) {
	_, err := parseChronyTracking(`Reference ID    : 4B564D00 (KVM)
System time     : 0.000006523 seconds slow of NTP time
Leap status     : Normal
`)
	if err == nil {
		t.Fatal("parse tracking returned nil error")
	}
	if !strings.Contains(err.Error(), "Skew") {
		t.Fatalf("parse error = %q, want missing Skew", err.Error())
	}
}

func TestChronyReferenceNameTreatsLocalFallbackAsUnsynchronized(t *testing.T) {
	if got := chronyReferenceName("7F7F0101 ()"); got != "" {
		t.Fatalf("source = %q, want empty", got)
	}
}

func TestChronycArgsUseLocalCommandSocket(t *testing.T) {
	got := chronycArgs("tracking")
	want := []string{"-h", chronySocketPath, "-n", "tracking"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chronyc args = %#v, want %#v", got, want)
	}
}

func TestVerifyRestoredWallClockStepsStaleClock(t *testing.T) {
	oldNow := wallClockNow
	oldSetRealtime := setRealtimeUnixNano
	oldPTPClock := ptpClockUnixNano
	t.Cleanup(func() {
		wallClockNow = oldNow
		setRealtimeUnixNano = oldSetRealtime
		ptpClockUnixNano = oldPTPClock
	})

	hostControlTime := time.Unix(0, 1_799_999_990_000)
	ptpTime := time.Unix(0, 1_800_000_000_000)
	nowCalls := 0
	wallClockNow = func() time.Time {
		nowCalls++
		switch nowCalls {
		case 1:
			return ptpTime.Add(-25 * time.Minute)
		default:
			return ptpTime.Add(25 * time.Microsecond)
		}
	}
	ptpClockUnixNano = func() (int64, error) {
		return ptpTime.UnixNano(), nil
	}
	var steppedTo int64
	setRealtimeUnixNano = func(unixNano int64) error {
		steppedTo = unixNano
		return nil
	}

	result := vmproto.ClockSyncResult{Status: "synchronized"}
	if err := verifyRestoredWallClock(&result, hostControlTime.UnixNano()); err != nil {
		t.Fatalf("verify restored wall clock: %v", err)
	}
	if steppedTo != ptpTime.UnixNano() {
		t.Fatalf("stepped to %d, want %d", steppedTo, ptpTime.UnixNano())
	}
	if result.HostUnixNano != ptpTime.UnixNano() {
		t.Fatalf("host unix nano = %d, want PTP time %d", result.HostUnixNano, ptpTime.UnixNano())
	}
	if !result.HostStepApplied {
		t.Fatal("host step was not recorded")
	}
	if result.PreStepWallOffsetNS != int64(-25*time.Minute) {
		t.Fatalf("pre-step offset = %d, want %d", result.PreStepWallOffsetNS, int64(-25*time.Minute))
	}
	if result.PostStepWallOffsetNS != int64(25*time.Microsecond) {
		t.Fatalf("post-step offset = %d, want %d", result.PostStepWallOffsetNS, int64(25*time.Microsecond))
	}
	if result.WallOffsetNS != int64(25*time.Microsecond) {
		t.Fatalf("wall offset = %d, want %d", result.WallOffsetNS, int64(25*time.Microsecond))
	}
}

func TestVerifyRestoredWallClockLeavesPostStepGateToChrony(t *testing.T) {
	oldNow := wallClockNow
	oldSetRealtime := setRealtimeUnixNano
	oldPTPClock := ptpClockUnixNano
	t.Cleanup(func() {
		wallClockNow = oldNow
		setRealtimeUnixNano = oldSetRealtime
		ptpClockUnixNano = oldPTPClock
	})

	hostControlTime := time.Unix(0, 1_799_999_990_000)
	ptpTime := time.Unix(0, 1_800_000_000_000)
	nowCalls := 0
	wallClockNow = func() time.Time {
		nowCalls++
		switch nowCalls {
		case 1:
			return ptpTime.Add(-25 * time.Minute)
		default:
			return ptpTime.Add(2 * time.Millisecond)
		}
	}
	ptpClockUnixNano = func() (int64, error) {
		return ptpTime.UnixNano(), nil
	}
	setRealtimeUnixNano = func(int64) error {
		return nil
	}

	result := vmproto.ClockSyncResult{Status: "synchronized"}
	if err := verifyRestoredWallClock(&result, hostControlTime.UnixNano()); err != nil {
		t.Fatalf("verify restored wall clock: %v", err)
	}
	if result.PostStepWallOffsetNS != int64(2*time.Millisecond) {
		t.Fatalf("post-step offset = %d, want %d", result.PostStepWallOffsetNS, int64(2*time.Millisecond))
	}
	if result.Status != "synchronized" {
		t.Fatalf("status = %q, want synchronized", result.Status)
	}
}

func TestVerifyRestoredWallClockRequiresPTPTime(t *testing.T) {
	oldPTPClock := ptpClockUnixNano
	t.Cleanup(func() { ptpClockUnixNano = oldPTPClock })

	ptpClockUnixNano = func() (int64, error) {
		return 0, errors.New("ptp missing")
	}

	result := vmproto.ClockSyncResult{Status: "synchronized"}
	err := verifyRestoredWallClock(&result, time.Now().UnixNano())
	if err == nil {
		t.Fatal("verify restored wall clock returned nil error")
	}
	if result.Status != "ptp_time_read_failed" {
		t.Fatalf("status = %q, want ptp_time_read_failed", result.Status)
	}
}

func TestVerifyRestoredWallClockRequiresHostTime(t *testing.T) {
	result := vmproto.ClockSyncResult{Status: "synchronized"}
	err := verifyRestoredWallClock(&result, 0)
	if err == nil {
		t.Fatal("verify restored wall clock returned nil error")
	}
	if result.Status != "host_time_missing" {
		t.Fatalf("status = %q, want host_time_missing", result.Status)
	}
}

func TestSyncRestoredClockStepsPTPTimeBeforeRestartingChrony(t *testing.T) {
	oldNow := wallClockNow
	oldSetRealtime := setRealtimeUnixNano
	oldPTPClock := ptpClockUnixNano
	oldRestartChrony := restartChronyDaemonAfterRestore
	oldSyncRunningChrony := syncRunningChrony
	t.Cleanup(func() {
		wallClockNow = oldNow
		setRealtimeUnixNano = oldSetRealtime
		ptpClockUnixNano = oldPTPClock
		restartChronyDaemonAfterRestore = oldRestartChrony
		syncRunningChrony = oldSyncRunningChrony
	})

	hostControlTime := time.Unix(0, 1_799_999_990_000)
	ptpTime := time.Unix(0, 1_800_000_000_000)
	nowCalls := 0
	wallClockNow = func() time.Time {
		nowCalls++
		switch nowCalls {
		case 1:
			return ptpTime.Add(-25 * time.Minute)
		default:
			return ptpTime.Add(25 * time.Microsecond)
		}
	}
	ptpClockUnixNano = func() (int64, error) {
		return ptpTime.UnixNano(), nil
	}

	var events []string
	setRealtimeUnixNano = func(unixNano int64) error {
		events = append(events, "set_realtime")
		if unixNano != ptpTime.UnixNano() {
			t.Fatalf("set realtime unix nano = %d, want %d", unixNano, ptpTime.UnixNano())
		}
		return nil
	}
	restartChronyDaemonAfterRestore = func() error {
		events = append(events, "restart_chrony")
		return nil
	}
	syncRunningChrony = func() (vmproto.ClockSyncResult, error) {
		events = append(events, "waitsync")
		return vmproto.ClockSyncResult{
			Status:     "synchronized",
			Source:     "KVM",
			OffsetNS:   12,
			SkewPPM:    0.003,
			LeapStatus: "Normal",
		}, nil
	}

	result, err := syncRestoredClockWithChrony(hostControlTime.UnixNano())
	if err != nil {
		t.Fatalf("sync restored clock: %v", err)
	}
	wantEvents := []string{"set_realtime", "restart_chrony", "waitsync"}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", events, wantEvents)
	}
	if result.Status != "synchronized" {
		t.Fatalf("status = %q, want synchronized", result.Status)
	}
	if result.Source != "KVM" {
		t.Fatalf("source = %q, want KVM", result.Source)
	}
	if !result.HostStepApplied {
		t.Fatal("host step was not recorded")
	}
	if result.WallOffsetNS != int64(25*time.Microsecond) {
		t.Fatalf("wall offset = %d, want %d", result.WallOffsetNS, int64(25*time.Microsecond))
	}
}

func TestSyncRestoredClockDoesNotRestartChronyWhenHostTimeIsMissing(t *testing.T) {
	oldRestartChrony := restartChronyDaemonAfterRestore
	oldSyncRunningChrony := syncRunningChrony
	t.Cleanup(func() {
		restartChronyDaemonAfterRestore = oldRestartChrony
		syncRunningChrony = oldSyncRunningChrony
	})

	restartChronyDaemonAfterRestore = func() error {
		t.Fatal("restart chrony should not be called")
		return nil
	}
	syncRunningChrony = func() (vmproto.ClockSyncResult, error) {
		t.Fatal("waitsync should not be called")
		return vmproto.ClockSyncResult{}, nil
	}

	result, err := syncRestoredClockWithChrony(0)
	if err == nil {
		t.Fatal("sync restored clock returned nil error")
	}
	if result.Status != "host_time_missing" {
		t.Fatalf("status = %q, want host_time_missing", result.Status)
	}
}

func TestSyncRestoredClockRecordsChronyRestartFailure(t *testing.T) {
	oldNow := wallClockNow
	oldPTPClock := ptpClockUnixNano
	oldRestartChrony := restartChronyDaemonAfterRestore
	oldSyncRunningChrony := syncRunningChrony
	t.Cleanup(func() {
		wallClockNow = oldNow
		ptpClockUnixNano = oldPTPClock
		restartChronyDaemonAfterRestore = oldRestartChrony
		syncRunningChrony = oldSyncRunningChrony
	})

	hostTime := time.Unix(0, 1_800_000_000_000)
	wallClockNow = func() time.Time { return hostTime }
	ptpClockUnixNano = func() (int64, error) {
		return hostTime.UnixNano(), nil
	}
	restartChronyDaemonAfterRestore = func() error {
		return errors.New("boom")
	}
	syncRunningChrony = func() (vmproto.ClockSyncResult, error) {
		t.Fatal("waitsync should not be called after chrony restart failure")
		return vmproto.ClockSyncResult{}, nil
	}

	result, err := syncRestoredClockWithChrony(hostTime.UnixNano())
	if err == nil {
		t.Fatal("sync restored clock returned nil error")
	}
	if result.Status != "restored_chrony_start_failed" {
		t.Fatalf("status = %q, want restored_chrony_start_failed", result.Status)
	}
}

func TestSyncRestoredClockPrefixesRestoredChronyGateFailure(t *testing.T) {
	oldNow := wallClockNow
	oldPTPClock := ptpClockUnixNano
	oldRestartChrony := restartChronyDaemonAfterRestore
	oldSyncRunningChrony := syncRunningChrony
	t.Cleanup(func() {
		wallClockNow = oldNow
		ptpClockUnixNano = oldPTPClock
		restartChronyDaemonAfterRestore = oldRestartChrony
		syncRunningChrony = oldSyncRunningChrony
	})

	hostTime := time.Unix(0, 1_800_000_000_000)
	wallClockNow = func() time.Time { return hostTime }
	ptpClockUnixNano = func() (int64, error) {
		return hostTime.UnixNano(), nil
	}
	restartChronyDaemonAfterRestore = func() error { return nil }
	syncRunningChrony = func() (vmproto.ClockSyncResult, error) {
		return vmproto.ClockSyncResult{
			Status:      "source_wait_failed",
			TrackingRaw: "refid: 00000000",
		}, errors.New("chrony source wait")
	}

	result, err := syncRestoredClockWithChrony(hostTime.UnixNano())
	if err == nil {
		t.Fatal("sync restored clock returned nil error")
	}
	if result.Status != "restored_chrony_source_wait_failed" {
		t.Fatalf("status = %q, want restored_chrony_source_wait_failed", result.Status)
	}
	if result.TrackingRaw != "refid: 00000000" {
		t.Fatalf("tracking raw = %q, want refid diagnostic", result.TrackingRaw)
	}
}

func TestPrepareBeforeGoldenSnapshotStopsChronyBeforeSync(t *testing.T) {
	oldStopChrony := stopChronyDaemonForSnapshot
	oldSyncFilesystems := syncFilesystems
	t.Cleanup(func() {
		stopChronyDaemonForSnapshot = oldStopChrony
		syncFilesystems = oldSyncFilesystems
	})

	var events []string
	stopChronyDaemonForSnapshot = func() error {
		events = append(events, "stop_chrony")
		return nil
	}
	syncFilesystems = func() {
		events = append(events, "sync")
	}

	if err := prepareBeforeGoldenSnapshotClock(); err != nil {
		t.Fatalf("prepare before golden snapshot: %v", err)
	}
	wantEvents := []string{"stop_chrony", "sync"}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", events, wantEvents)
	}
}

func TestPrepareBeforeGoldenSnapshotFailsWhenChronyStopFails(t *testing.T) {
	oldStopChrony := stopChronyDaemonForSnapshot
	oldSyncFilesystems := syncFilesystems
	t.Cleanup(func() {
		stopChronyDaemonForSnapshot = oldStopChrony
		syncFilesystems = oldSyncFilesystems
	})

	stopChronyDaemonForSnapshot = func() error {
		return errors.New("boom")
	}
	syncFilesystems = func() {
		t.Fatal("sync should not be called after chrony stop failure")
	}

	err := prepareBeforeGoldenSnapshotClock()
	if err == nil {
		t.Fatal("prepare before golden snapshot returned nil error")
	}
	if !strings.Contains(err.Error(), "snapshot_chrony_stop_failed") {
		t.Fatalf("error = %q, want snapshot_chrony_stop_failed", err.Error())
	}
}
