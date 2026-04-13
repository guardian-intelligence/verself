package billing

import (
	"testing"
	"time"
)

func TestSelfServeContractPhaseIDIncludesRequestedAt(t *testing.T) {
	t.Parallel()

	firstAt := time.Date(2026, 4, 13, 18, 45, 5, 1, time.UTC)
	secondAt := firstAt.Add(time.Nanosecond)

	first := newSelfServeContractPhaseID("contract_01", "sandbox-hobby", firstAt)
	again := newSelfServeContractPhaseID("contract_01", "sandbox-hobby", firstAt)
	second := newSelfServeContractPhaseID("contract_01", "sandbox-hobby", secondAt)

	if first != again {
		t.Fatalf("phase id = %q, want deterministic %q", again, first)
	}
	if first == second {
		t.Fatalf("phase id must differ across non-contiguous plan changes: %q", first)
	}
}

func TestNormalizePhaseChangeEffectiveAtKeepsHalfOpenPhaseInterval(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 4, 13, 18, 45, 5, 0, time.UTC)
	got := normalizePhaseChangeEffectiveAt(start, &start)
	want := start.Add(time.Nanosecond)

	if !got.Equal(want) {
		t.Fatalf("effective_at = %s, want %s", got, want)
	}

	later := start.Add(time.Second)
	if got := normalizePhaseChangeEffectiveAt(later, &start); !got.Equal(later) {
		t.Fatalf("later effective_at = %s, want %s", got, later)
	}
}
