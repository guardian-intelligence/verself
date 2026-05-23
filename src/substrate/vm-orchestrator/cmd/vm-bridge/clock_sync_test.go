package main

import (
	"strings"
	"testing"
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
