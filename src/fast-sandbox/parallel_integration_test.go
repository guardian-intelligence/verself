//go:build integration

package fastsandbox

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestParallelFirecrackerVMs(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Fatal("integration test requires root; run via sudo")
	}

	cfg := DefaultConfig()
	cfg.VCPUs = 1
	cfg.MemoryMiB = 512

	requireFirecrackerIntegrationPrereqs(t, cfg)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	orch := New(cfg, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	jobIDs := []string{uuid.NewString(), uuid.NewString()}
	results := make(chan runOutcome, len(jobIDs))

	for _, jobID := range jobIDs {
		job := JobConfig{
			JobID:      jobID,
			RunCommand: []string{"sh", "-lc", "echo parallel-start && sleep 3 && echo parallel-done"},
			RunWorkDir: "/workspace",
			Env: map[string]string{
				"CI": "true",
			},
		}

		go func(job JobConfig) {
			result, err := orch.Run(ctx, job)
			results <- runOutcome{jobID: job.JobID, result: result, err: err}
		}(job)
	}

	leases, err := waitForRunningJobLeases(ctx, cfg.NetworkLeaseDir, jobIDs, len(jobIDs))
	if err != nil {
		cancel()
		t.Fatalf("wait for concurrent running VMs: %v", err)
	}

	seenSlots := make(map[int]struct{}, len(leases))
	for _, lease := range leases {
		if _, exists := seenSlots[lease.SlotIndex]; exists {
			t.Fatalf("duplicate slot allocation detected: %d", lease.SlotIndex)
		}
		seenSlots[lease.SlotIndex] = struct{}{}
		if lease.PID <= 0 || !processExists(lease.PID) {
			t.Fatalf("expected live jailer PID for job %s, got %d", lease.JobID, lease.PID)
		}
		if !tapExists(lease.TapName) {
			t.Fatalf("expected TAP %s for job %s to exist while VM is running", lease.TapName, lease.JobID)
		}
	}

	for range jobIDs {
		outcome := <-results
		if outcome.err != nil {
			t.Fatalf("job %s failed: %v", outcome.jobID, outcome.err)
		}
		if outcome.result.ExitCode != 0 {
			t.Fatalf("job %s exited with %d\nlogs:\n%s", outcome.jobID, outcome.result.ExitCode, outcome.result.Logs)
		}
	}

	if err := waitForLeaseCleanup(ctx, cfg.NetworkLeaseDir, jobIDs); err != nil {
		t.Fatalf("wait for lease cleanup: %v", err)
	}

	for _, lease := range leases {
		if tapExists(lease.TapName) {
			t.Fatalf("expected TAP %s to be cleaned up", lease.TapName)
		}
	}
}

type runOutcome struct {
	jobID  string
	result JobResult
	err    error
}

func requireFirecrackerIntegrationPrereqs(t *testing.T, cfg Config) {
	t.Helper()

	for _, path := range []string{"/dev/kvm", cfg.KernelPath, cfg.FirecrackerBin, cfg.JailerBin} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing prerequisite %s: %v", path, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	goldenSnapshot := cfg.Pool + "/" + cfg.GoldenZvol + "@ready"
	exists, err := zfsSnapshotExists(ctx, goldenSnapshot)
	if err != nil {
		t.Fatalf("cannot inspect golden snapshot %s: %v", goldenSnapshot, err)
	}
	if !exists {
		t.Fatalf("golden snapshot %s does not exist", goldenSnapshot)
	}
}

func waitForRunningJobLeases(ctx context.Context, leaseDir string, jobIDs []string, want int) ([]NetworkLease, error) {
	jobSet := make(map[string]struct{}, len(jobIDs))
	for _, jobID := range jobIDs {
		jobSet[jobID] = struct{}{}
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		leases, err := currentJobLeases(leaseDir, jobSet)
		if err != nil {
			return nil, err
		}
		live := runningLeases(leases)
		if len(live) == want {
			return live, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func runningLeases(leases []NetworkLease) []NetworkLease {
	live := make([]NetworkLease, 0, len(leases))
	for _, lease := range leases {
		if lease.PID <= 0 || !processExists(lease.PID) {
			continue
		}
		if !tapExists(lease.TapName) {
			continue
		}
		live = append(live, lease)
	}
	return live
}

func waitForLeaseCleanup(ctx context.Context, leaseDir string, jobIDs []string) error {
	jobSet := make(map[string]struct{}, len(jobIDs))
	for _, jobID := range jobIDs {
		jobSet[jobID] = struct{}{}
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		leases, err := currentJobLeases(leaseDir, jobSet)
		if err != nil {
			return err
		}
		if len(leases) == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func currentJobLeases(leaseDir string, jobSet map[string]struct{}) ([]NetworkLease, error) {
	entries, err := os.ReadDir(leaseDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	leases := make([]NetworkLease, 0, len(jobSet))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		lease, err := readLeaseFile(filepath.Join(leaseDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		if _, ok := jobSet[lease.JobID]; ok {
			leases = append(leases, lease)
		}
	}
	return leases, nil
}
