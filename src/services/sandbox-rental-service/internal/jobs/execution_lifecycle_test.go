package jobs

import (
	"testing"
	"time"
)

func TestLeaseTTLTracksExecutionWallClockBudget(t *testing.T) {
	item := executionWorkItem{MaxWallSeconds: uint64((3 * time.Hour).Seconds())}
	got := leaseTTLSeconds(item, 2*time.Hour)
	want := uint64((3 * time.Hour).Seconds()) + leaseTTLGraceSeconds
	if got != want {
		t.Fatalf("lease TTL = %d, want %d", got, want)
	}
}

func TestLeaseTTLUsesConfiguredDefaultWhenRequestOmitsMaxWall(t *testing.T) {
	got := leaseTTLSeconds(executionWorkItem{}, 45*time.Minute)
	want := uint64((45 * time.Minute).Seconds()) + leaseTTLGraceSeconds
	if got != want {
		t.Fatalf("lease TTL = %d, want %d", got, want)
	}
}
