package vmorchestrator

import (
	"context"
	"testing"
)

func TestWorkspaceJournalPersistsCommitPhases(t *testing.T) {
	ctx := context.Background()
	store, err := openHostStateStore(":memory:", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.close() }()

	entries := []workspaceJournalEntry{
		{
			OperationID: "op-a",
			LeaseID:     "lease-a",
			MountName:   "github-workspace",
			Phase:       "accepted",
		},
		{
			OperationID:       "op-a",
			LeaseID:           "lease-a",
			MountName:         "github-workspace",
			Phase:             "zfs_commit_started",
			SourceDatasetRef:  "pool/goldens/scope/generations/base@sealed",
			WorkingDatasetRef: "pool/workloads/lease-a/mounts/00-github-workspace",
		},
		{
			OperationID:      "op-a",
			LeaseID:          "lease-a",
			MountName:        "github-workspace",
			Phase:            "committed",
			SealedDatasetRef: "pool/goldens/scope/generations/new@sealed",
		},
	}
	for _, entry := range entries {
		if err := store.appendWorkspaceJournal(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := store.db.QueryContext(ctx, `SELECT phase FROM host_workspace_journal WHERE operation_id = ? ORDER BY journal_seq`, "op-a")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var phases []string
	for rows.Next() {
		var phase string
		if err := rows.Scan(&phase); err != nil {
			t.Fatal(err)
		}
		phases = append(phases, phase)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{"accepted", "zfs_commit_started", "committed"}
	if len(phases) != len(want) {
		t.Fatalf("phases = %v, want %v", phases, want)
	}
	for i := range want {
		if phases[i] != want[i] {
			t.Fatalf("phases = %v, want %v", phases, want)
		}
	}
}
