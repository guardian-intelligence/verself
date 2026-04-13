package billing

import (
	"testing"
	"time"
)

func TestSuccessorBillingCycleIsDeterministicAndConsecutive(t *testing.T) {
	t.Parallel()

	anchor := time.Date(2026, 1, 31, 12, 0, 0, 123, time.UTC)
	predecessor := BillingCycle{
		CycleID:     "cycle_0",
		OrgID:       42,
		ProductID:   "sandbox",
		CadenceKind: "anniversary_monthly",
		AnchorAt:    anchor,
		CycleSeq:    0,
		StartsAt:    anchor,
		EndsAt:      addMonthsClampedUTC(anchor, 1),
		Status:      "closed_for_usage",
	}

	successor, err := successorBillingCycle(predecessor)
	if err != nil {
		t.Fatalf("successor: %v", err)
	}
	if successor.PredecessorCycleID != predecessor.CycleID {
		t.Fatalf("predecessor id = %q, want %q", successor.PredecessorCycleID, predecessor.CycleID)
	}
	if successor.CycleSeq != 1 {
		t.Fatalf("cycle seq = %d, want 1", successor.CycleSeq)
	}
	if !successor.StartsAt.Equal(predecessor.EndsAt) {
		t.Fatalf("successor starts_at = %s, want predecessor ends_at %s", successor.StartsAt, predecessor.EndsAt)
	}
	wantEnd := addMonthsClampedUTC(anchor, 2)
	if !successor.EndsAt.Equal(wantEnd) {
		t.Fatalf("successor ends_at = %s, want %s", successor.EndsAt, wantEnd)
	}
	again, err := successorBillingCycle(predecessor)
	if err != nil {
		t.Fatalf("successor again: %v", err)
	}
	if again.CycleID != successor.CycleID {
		t.Fatalf("successor id = %q, want deterministic %q", again.CycleID, successor.CycleID)
	}
}
