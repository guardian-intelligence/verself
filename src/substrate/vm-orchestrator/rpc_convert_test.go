package vmorchestrator

import (
	"testing"
	"time"
)

func TestUnixNsTreatsAbsentAndPreEpochTimesAsZero(t *testing.T) {
	if got := unixNs(time.Time{}); got != 0 {
		t.Fatalf("zero time unix ns = %d, want 0", got)
	}
	if got := unixNs(time.Unix(-1, 0)); got != 0 {
		t.Fatalf("pre-epoch unix ns = %d, want 0", got)
	}
}

func TestTimeFromUnixNsTreatsWrappedLegacyZeroAsAbsent(t *testing.T) {
	const wrappedZeroUnixNano = uint64(11651379494838206464)
	if got := timeFromUnixNs(wrappedZeroUnixNano); !got.IsZero() {
		t.Fatalf("wrapped legacy zero = %s, want zero time", got)
	}
}

func TestUnixNsRoundTrip(t *testing.T) {
	now := time.Unix(1778395973, 123).UTC()
	got := timeFromUnixNs(unixNs(now))
	if !got.Equal(now) {
		t.Fatalf("round trip = %s, want %s", got, now)
	}
}
