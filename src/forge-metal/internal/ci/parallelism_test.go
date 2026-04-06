package ci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func TestSampleLeases_IgnoresPendingLeaseWithoutPID(t *testing.T) {
	leaseDir := t.TempDir()
	writeLeaseFixture(t, leaseDir, "000000.json", leaseFile{
		JobID:   "pending-job",
		TapName: "fc-tap-pending",
	})

	active, jobIDs, err := sampleLeases(leaseDir, time.Now().UTC().Add(-time.Minute))
	if err != nil {
		t.Fatalf("sampleLeases: %v", err)
	}
	if active != 0 {
		t.Fatalf("active leases: got %d want 0", active)
	}
	if len(jobIDs) != 0 {
		t.Fatalf("jobIDs: got %v want empty", jobIDs)
	}
}

func TestSampleLeases_CountsLiveProcess(t *testing.T) {
	leaseDir := t.TempDir()
	tapDir := t.TempDir()
	origSysClassNet := sysClassNetPath
	sysClassNetPath = tapDir
	defer func() { sysClassNetPath = origSysClassNet }()

	tapName := "fc-tap-live"
	if err := os.Mkdir(filepath.Join(tapDir, tapName), 0o755); err != nil {
		t.Fatalf("mkdir tap fixture: %v", err)
	}

	writeLeaseFixture(t, leaseDir, "000000.json", leaseFile{
		JobID:        "live-job",
		PID:          os.Getpid(),
		TapName:      tapName,
		CreatedAtUTC: time.Now().UTC(),
	})

	active, jobIDs, err := sampleLeases(leaseDir, time.Now().UTC().Add(-time.Minute))
	if err != nil {
		t.Fatalf("sampleLeases: %v", err)
	}
	if active != 1 {
		t.Fatalf("active leases: got %d want 1", active)
	}
	if len(jobIDs) != 1 || jobIDs[0] != "live-job" {
		t.Fatalf("jobIDs: got %v want [live-job]", jobIDs)
	}
}

func TestSampleLeases_IgnoresLeaseOlderThanWitness(t *testing.T) {
	leaseDir := t.TempDir()
	tapDir := t.TempDir()
	origSysClassNet := sysClassNetPath
	sysClassNetPath = tapDir
	defer func() { sysClassNetPath = origSysClassNet }()

	tapName := "fc-tap-old"
	if err := os.Mkdir(filepath.Join(tapDir, tapName), 0o755); err != nil {
		t.Fatalf("mkdir tap fixture: %v", err)
	}

	startedAt := time.Now().UTC()
	writeLeaseFixture(t, leaseDir, "000000.json", leaseFile{
		JobID:        "old-job",
		PID:          os.Getpid(),
		TapName:      tapName,
		CreatedAtUTC: startedAt.Add(-time.Second),
	})

	active, jobIDs, err := sampleLeases(leaseDir, startedAt)
	if err != nil {
		t.Fatalf("sampleLeases: %v", err)
	}
	if active != 0 {
		t.Fatalf("active leases: got %d want 0", active)
	}
	if len(jobIDs) != 0 {
		t.Fatalf("jobIDs: got %v want empty", jobIDs)
	}
}

func writeLeaseFixture(t *testing.T, leaseDir, name string, lease leaseFile) {
	t.Helper()
	path := filepath.Join(leaseDir, name)
	data, err := json.Marshal(lease)
	if err != nil {
		t.Fatalf("marshal lease: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write lease fixture: %v", err)
	}
}

func TestSortedKeys(t *testing.T) {
	t.Parallel()

	input := map[string]struct{}{
		"b": {},
		"a": {},
	}
	got := sortedKeys(input)
	want := []string{"a", "b"}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("sortedKeys returned unsorted slice: %v", got)
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("sortedKeys: got %v want %v", got, want)
	}
}
