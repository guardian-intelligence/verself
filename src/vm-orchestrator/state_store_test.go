package vmorchestrator

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestHostStateStoreLifecycle(t *testing.T) {
	t.Parallel()

	store := mustOpenStateStoreForTest(t)
	defer store.close()

	runID := "run-1"
	if err := store.createRun(context.Background(), runID, JobStatePending, map[string]string{"source": "test"}); err != nil {
		t.Fatalf("createRun: %v", err)
	}
	if err := store.appendRunEvent(context.Background(), runID, "phase_started", map[string]string{"phase": "run"}); err != nil {
		t.Fatalf("appendRunEvent: %v", err)
	}

	if err := store.transitionRunState(
		context.Background(),
		runID,
		[]JobState{JobStatePending},
		JobStateRunning,
		"run_started",
		map[string]string{"phase": "run"},
		"",
		nil,
	); err != nil {
		t.Fatalf("transitionRunState pending->running: %v", err)
	}

	finishedResult := &JobResult{
		ExitCode:    0,
		Duration:    2 * time.Second,
		RunDuration: 1500 * time.Millisecond,
	}
	if err := store.transitionRunState(
		context.Background(),
		runID,
		[]JobState{JobStateRunning},
		JobStateSucceeded,
		"run_finished",
		map[string]string{"reason": "ok"},
		"",
		finishedResult,
	); err != nil {
		t.Fatalf("transitionRunState running->succeeded: %v", err)
	}

	snapshot, err := store.getRunSnapshot(context.Background(), runID)
	if err != nil {
		t.Fatalf("getRunSnapshot: %v", err)
	}
	if snapshot.State != JobStateSucceeded {
		t.Fatalf("snapshot state = %v, want %v", snapshot.State, JobStateSucceeded)
	}
	if snapshot.Result == nil || snapshot.Result.ExitCode != 0 {
		t.Fatalf("snapshot result = %#v", snapshot.Result)
	}
	if snapshot.UpdatedAt.IsZero() {
		t.Fatal("snapshot updated_at must be set")
	}

	events, err := store.listRunEvents(context.Background(), runID, 0, 100)
	if err != nil {
		t.Fatalf("listRunEvents: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("events length = %d, want 4", len(events))
	}
	for i, event := range events {
		if got, want := event.Seq, uint64(i+1); got != want {
			t.Fatalf("event seq[%d] = %d, want %d", i, got, want)
		}
	}
}

func TestHostStateStoreCountActiveRuns(t *testing.T) {
	t.Parallel()

	store := mustOpenStateStoreForTest(t)
	defer store.close()

	if err := store.createRun(context.Background(), "run-pending", JobStatePending, nil); err != nil {
		t.Fatalf("create run-pending: %v", err)
	}
	if err := store.createRun(context.Background(), "run-running", JobStatePending, nil); err != nil {
		t.Fatalf("create run-running: %v", err)
	}
	if err := store.transitionRunState(
		context.Background(),
		"run-running",
		[]JobState{JobStatePending},
		JobStateRunning,
		"run_started",
		nil,
		"",
		nil,
	); err != nil {
		t.Fatalf("transition run-running: %v", err)
	}
	if err := store.createRun(context.Background(), "run-terminal", JobStatePending, nil); err != nil {
		t.Fatalf("create run-terminal: %v", err)
	}
	if err := store.transitionRunState(
		context.Background(),
		"run-terminal",
		[]JobState{JobStatePending},
		JobStateFailed,
		"run_finished",
		nil,
		"boom",
		&JobResult{ExitCode: 1},
	); err != nil {
		t.Fatalf("transition run-terminal: %v", err)
	}

	count, err := store.countActiveRuns(context.Background())
	if err != nil {
		t.Fatalf("countActiveRuns: %v", err)
	}
	if count != 2 {
		t.Fatalf("active runs = %d, want 2", count)
	}
}

func mustOpenStateStoreForTest(t *testing.T) *hostStateStore {
	t.Helper()
	store, err := openHostStateStore(filepath.Join(t.TempDir(), "state.db"), nil)
	if err != nil {
		t.Fatalf("openHostStateStore: %v", err)
	}
	return store
}
