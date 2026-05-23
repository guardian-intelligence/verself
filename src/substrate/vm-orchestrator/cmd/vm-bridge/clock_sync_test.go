package main

import (
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
	t.Cleanup(func() {
		wallClockNow = oldNow
		setRealtimeUnixNano = oldSetRealtime
	})

	hostTime := time.Unix(0, 1_800_000_000_000)
	nowCalls := 0
	wallClockNow = func() time.Time {
		nowCalls++
		switch nowCalls {
		case 1:
			return hostTime.Add(-25 * time.Minute)
		default:
			return hostTime.Add(25 * time.Microsecond)
		}
	}
	var steppedTo int64
	setRealtimeUnixNano = func(unixNano int64) error {
		steppedTo = unixNano
		return nil
	}

	result := vmproto.ClockSyncResult{Status: "synchronized"}
	if err := verifyRestoredWallClock(&result, hostTime.UnixNano()); err != nil {
		t.Fatalf("verify restored wall clock: %v", err)
	}
	if steppedTo != hostTime.UnixNano() {
		t.Fatalf("stepped to %d, want %d", steppedTo, hostTime.UnixNano())
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

func TestVerifyRestoredWallClockRejectsPostStepBeyondSLA(t *testing.T) {
	oldNow := wallClockNow
	oldSetRealtime := setRealtimeUnixNano
	t.Cleanup(func() {
		wallClockNow = oldNow
		setRealtimeUnixNano = oldSetRealtime
	})

	hostTime := time.Unix(0, 1_800_000_000_000)
	nowCalls := 0
	wallClockNow = func() time.Time {
		nowCalls++
		switch nowCalls {
		case 1:
			return hostTime.Add(-25 * time.Minute)
		default:
			return hostTime.Add(2 * time.Millisecond)
		}
	}
	setRealtimeUnixNano = func(int64) error {
		return nil
	}

	result := vmproto.ClockSyncResult{Status: "synchronized"}
	err := verifyRestoredWallClock(&result, hostTime.UnixNano())
	if err == nil {
		t.Fatal("verify restored wall clock returned nil error")
	}
	if result.Status != "host_step_offset_exceeded" {
		t.Fatalf("status = %q, want host_step_offset_exceeded", result.Status)
	}
	if result.PostStepWallOffsetNS != int64(2*time.Millisecond) {
		t.Fatalf("post-step offset = %d, want %d", result.PostStepWallOffsetNS, int64(2*time.Millisecond))
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
