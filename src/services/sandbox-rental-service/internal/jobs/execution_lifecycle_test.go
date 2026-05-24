package jobs

import (
	"errors"
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

func TestGoldenVMRootSnapshotMissingClassifiesZFSCloneFailure(t *testing.T) {
	err := errors.New("lease 01K reached terminal state before ready: zfs clone vspool/orgs/org_a/goldens/vmroot-snap/generations/root@sealed -> vspool/orgs/org_a/workloads/lease/root: cannot open 'vspool/orgs/org_a/goldens/vmroot-snap/generations/root@sealed': dataset does not exist: zfs source snapshot not found: exit status 1")
	if !goldenVMRootSnapshotMissing(err) {
		t.Fatalf("goldenVMRootSnapshotMissing(%q) = false, want true", err)
	}
}

func TestGoldenVMRootSnapshotMissingIgnoresUnrelatedFailure(t *testing.T) {
	err := errors.New("lease 01K reached terminal state before ready: guest failed health check")
	if goldenVMRootSnapshotMissing(err) {
		t.Fatalf("goldenVMRootSnapshotMissing(%q) = true, want false", err)
	}
}
