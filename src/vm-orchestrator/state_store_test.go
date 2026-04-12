package vmorchestrator

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHostStateStoreLifecycle(t *testing.T) {
	t.Parallel()

	store := mustOpenStateStoreForTest(t)
	defer store.close()

	runID := "run-1"
	if err := store.createRun(context.Background(), runID, RunStatePending, map[string]string{"source": "test"}); err != nil {
		t.Fatalf("createRun: %v", err)
	}
	if err := store.appendRunEvent(context.Background(), runID, "phase_started", map[string]string{"phase": "run"}); err != nil {
		t.Fatalf("appendRunEvent: %v", err)
	}

	if err := store.transitionRunState(
		context.Background(),
		runID,
		[]RunState{RunStatePending},
		RunStateRunning,
		"run_started",
		map[string]string{"phase": "run"},
		"",
		nil,
	); err != nil {
		t.Fatalf("transitionRunState pending->running: %v", err)
	}

	finishedResult := &RunResult{
		ExitCode:    0,
		Duration:    2 * time.Second,
		RunDuration: 1500 * time.Millisecond,
	}
	if err := store.transitionRunState(
		context.Background(),
		runID,
		[]RunState{RunStateRunning},
		RunStateSucceeded,
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
	if snapshot.State != RunStateSucceeded {
		t.Fatalf("snapshot state = %v, want %v", snapshot.State, RunStateSucceeded)
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

	if err := store.createRun(context.Background(), "run-pending", RunStatePending, nil); err != nil {
		t.Fatalf("create run-pending: %v", err)
	}
	if err := store.createRun(context.Background(), "run-running", RunStatePending, nil); err != nil {
		t.Fatalf("create run-running: %v", err)
	}
	if err := store.transitionRunState(
		context.Background(),
		"run-running",
		[]RunState{RunStatePending},
		RunStateRunning,
		"run_started",
		nil,
		"",
		nil,
	); err != nil {
		t.Fatalf("transition run-running: %v", err)
	}
	if err := store.createRun(context.Background(), "run-terminal", RunStatePending, nil); err != nil {
		t.Fatalf("create run-terminal: %v", err)
	}
	if err := store.transitionRunState(
		context.Background(),
		"run-terminal",
		[]RunState{RunStatePending},
		RunStateFailed,
		"run_finished",
		nil,
		"boom",
		&RunResult{ExitCode: 1},
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

func TestHostStateStorePersistsCheckpointOpsAuditRows(t *testing.T) {
	t.Parallel()

	store := mustOpenStateStoreForTest(t)
	defer store.close()

	ctx := context.Background()
	runID := "run-checkpoint-audit"
	if err := store.createRun(ctx, runID, RunStatePending, nil); err != nil {
		t.Fatalf("createRun: %v", err)
	}

	first := map[string]string{
		"request_id": "req-1",
		"operation":  "save",
		"ref":        "checkpoints/default",
		"accepted":   "true",
		"version_id": "v-1",
		"error":      "",
	}
	second := map[string]string{
		"request_id": "req-2",
		"operation":  "save",
		"ref":        "checkpoints/default",
		"accepted":   "false",
		"version_id": "",
		"error":      "not authorized",
	}
	if err := store.appendRunEvent(ctx, runID, "checkpoint_request", first); err != nil {
		t.Fatalf("appendRunEvent first checkpoint request: %v", err)
	}
	if err := store.appendRunEvent(ctx, runID, "checkpoint_request", second); err != nil {
		t.Fatalf("appendRunEvent second checkpoint request: %v", err)
	}

	type checkpointOpRow struct {
		seq       int64
		requestID string
		opType    string
		ref       string
		accepted  int64
		versionID string
		errorText string
	}

	rows, err := store.db.QueryContext(
		ctx,
		`SELECT op_seq, request_id, op_type, ref, accepted, version_id, error_text
		 FROM checkpoint_ops
		 WHERE run_id = ?
		 ORDER BY op_seq ASC`,
		runID,
	)
	if err != nil {
		t.Fatalf("query checkpoint_ops: %v", err)
	}
	defer rows.Close()

	var got []checkpointOpRow
	for rows.Next() {
		var row checkpointOpRow
		if err := rows.Scan(&row.seq, &row.requestID, &row.opType, &row.ref, &row.accepted, &row.versionID, &row.errorText); err != nil {
			t.Fatalf("scan checkpoint_ops row: %v", err)
		}
		got = append(got, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate checkpoint_ops rows: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("checkpoint_ops rows = %d, want 2", len(got))
	}
	if got[0].seq != 1 || got[0].requestID != "req-1" || got[0].accepted != 1 || got[0].versionID != "v-1" {
		t.Fatalf("checkpoint_ops row[0] = %#v", got[0])
	}
	if got[1].seq != 2 || got[1].requestID != "req-2" || got[1].accepted != 0 || got[1].errorText != "not authorized" {
		t.Fatalf("checkpoint_ops row[1] = %#v", got[1])
	}
}

func TestHostStateStoreRejectsInvalidCheckpointAcceptedValue(t *testing.T) {
	t.Parallel()

	store := mustOpenStateStoreForTest(t)
	defer store.close()

	ctx := context.Background()
	runID := "run-checkpoint-invalid-accepted"
	if err := store.createRun(ctx, runID, RunStatePending, nil); err != nil {
		t.Fatalf("createRun: %v", err)
	}

	err := store.appendRunEvent(ctx, runID, "checkpoint_request", map[string]string{
		"request_id": "req-invalid",
		"operation":  "save",
		"ref":        "checkpoints/default",
		"accepted":   "not-a-bool",
	})
	if err == nil {
		t.Fatal("expected appendRunEvent to fail on invalid accepted value")
	}
	if !strings.Contains(err.Error(), "invalid accepted value") {
		t.Fatalf("expected invalid accepted error, got %v", err)
	}

	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM checkpoint_ops WHERE run_id = ?`, runID).Scan(&count); err != nil {
		t.Fatalf("count checkpoint_ops rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("checkpoint_ops row count = %d, want 0", count)
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
