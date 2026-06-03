package jobs

import (
	"errors"
	"testing"
	"time"

	vmorchestrator "github.com/verself/vm-orchestrator"
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

func TestGoldenVMRestoreFailureReasonClassifiesReadyWaitFailures(t *testing.T) {
	for _, reason := range []string{"lease_ready_failed", "lease_ready_timeout"} {
		if !goldenVMRestoreFailureReason(reason) {
			t.Fatalf("goldenVMRestoreFailureReason(%q) = false, want true", reason)
		}
	}
	for _, reason := range []string{"lease_acquire_failed", "lease_acquire_timeout", "durable_cache_prepare_failed"} {
		if goldenVMRestoreFailureReason(reason) {
			t.Fatalf("goldenVMRestoreFailureReason(%q) = true, want false", reason)
		}
	}
}

func TestRunnerSnapshotRestoreProblemClassifiesGuestTimeSyncPrecisionGate(t *testing.T) {
	for _, err := range []error{
		errors.New("lease 01KT632XZKAJAGVMHEJ45WHGRY reached terminal state before ready: guest control protocol violation in await_after_restore_result: restored wall clock offset 3813174ns exceeds 1000000ns after host step"),
		errors.New("lease 01K reached terminal state before ready: guest control protocol violation in await_after_restore_result: restored chrony sync: chrony sync wait: exit status 1"),
		errors.New("lease 01K reached terminal state before ready: guest control protocol violation in await_after_restore_result: restored chrony sync: chrony offset 1200000ns exceeds 1000000ns"),
	} {
		problem := runnerSnapshotRestoreProblemFromError(err)
		if problem.Code != runnerAllocationGuestTimeSyncPrecisionGateFailedCode {
			t.Fatalf("problem code for %q = %q, want %q", err, problem.Code, runnerAllocationGuestTimeSyncPrecisionGateFailedCode)
		}
		if problem.Title != runnerAllocationGuestTimeSyncPrecisionGateFailedTitle {
			t.Fatalf("problem title = %q, want %q", problem.Title, runnerAllocationGuestTimeSyncPrecisionGateFailedTitle)
		}
		if problem.Phase != runnerAllocationGuestTimeSyncPrecisionGateFailedPhase {
			t.Fatalf("problem phase = %q, want %q", problem.Phase, runnerAllocationGuestTimeSyncPrecisionGateFailedPhase)
		}
	}
}

func TestRunnerSnapshotRestoreProblemKeepsGenericRestoreFailure(t *testing.T) {
	err := errors.New("lease 01K reached terminal state before ready: guest control protocol violation in await_after_restore_result: read restored PTP time: open /dev/ptp0: no such file or directory")
	problem := runnerSnapshotRestoreProblemFromError(err)
	if problem.Code != runnerAllocationSnapshotRestoreFailedCode {
		t.Fatalf("problem code = %q, want %q", problem.Code, runnerAllocationSnapshotRestoreFailedCode)
	}
	if problem.Phase != runnerAllocationSnapshotRestoreFailedPhase {
		t.Fatalf("problem phase = %q, want %q", problem.Phase, runnerAllocationSnapshotRestoreFailedPhase)
	}
}

func TestGoldenVMHookVersionsComeFromOrchestratorProtocol(t *testing.T) {
	if goldenVMAfterRestoreHookVersion != vmorchestrator.GoldenVMRestoreHookVersion {
		t.Fatalf("after restore hook version = %q, want %q", goldenVMAfterRestoreHookVersion, vmorchestrator.GoldenVMRestoreHookVersion)
	}
	if goldenVMBeforeSnapshotHookVersion != vmorchestrator.GoldenVMBeforeSnapshotHookVersion {
		t.Fatalf("before snapshot hook version = %q, want %q", goldenVMBeforeSnapshotHookVersion, vmorchestrator.GoldenVMBeforeSnapshotHookVersion)
	}
}
