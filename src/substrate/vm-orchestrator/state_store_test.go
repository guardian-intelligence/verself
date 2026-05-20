package vmorchestrator

import (
	"context"
	"testing"
	"time"
)

func TestDurableJournalPersistsCommitPhases(t *testing.T) {
	ctx := context.Background()
	store, err := openHostStateStore(":memory:", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.close() }()

	entries := []durableJournalEntry{
		{
			OperationID: "op-a",
			LeaseID:     "lease-a",
			MountName:   "workspace",
			Phase:       "accepted",
		},
		{
			OperationID:       "op-a",
			LeaseID:           "lease-a",
			MountName:         "workspace",
			Phase:             "zfs_commit_started",
			SourceDatasetRef:  "pool/goldens/scope/generations/base@sealed",
			WorkingDatasetRef: "pool/workloads/lease-a/mounts/00-workspace",
		},
		{
			OperationID:      "op-a",
			LeaseID:          "lease-a",
			MountName:        "workspace",
			Phase:            "committed",
			SealedDatasetRef: "pool/goldens/scope/generations/new@sealed",
		},
	}
	for _, entry := range entries {
		if err := store.appendDurableJournal(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := store.db.QueryContext(ctx, `SELECT phase FROM host_durable_journal WHERE operation_id = ? ORDER BY journal_seq`, "op-a")
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

func TestLeaseReadyPersistsFilesystemMountResults(t *testing.T) {
	ctx := context.Background()
	store, err := openHostStateStore(":memory:", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.close() }()

	acquiredAt := time.Now().UTC()
	expiresAt := acquiredAt.Add(time.Hour)
	if err := store.createLease(ctx, leaseSnapshot{
		LeaseID:    "lease-mounts",
		State:      LeaseStateAcquiring,
		Spec:       LeaseSpec{Resources: VMResources{VCPUs: 2, MemoryMiB: 2048, RootDiskGiB: 8}},
		TrustClass: "trusted",
		AcquiredAt: acquiredAt,
		ExpiresAt:  expiresAt,
	}); err != nil {
		t.Fatal(err)
	}
	mounts := []FilesystemMountResult{{
		Name:        "workspace",
		MountPath:   "/workspace",
		OperationID: "op-a",
		Mounted:     true,
		Required:    true,
	}}
	if err := store.setLeaseReady(ctx, "lease-mounts", "172.30.0.2", time.Now().UTC(), mounts); err != nil {
		t.Fatal(err)
	}

	got, err := store.getLease(ctx, "lease-mounts")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != LeaseStateReady {
		t.Fatalf("state = %v, want %v", got.State, LeaseStateReady)
	}
	if len(got.FilesystemMounts) != 1 {
		t.Fatalf("filesystem mounts = %v, want one mount", got.FilesystemMounts)
	}
	if got.FilesystemMounts[0] != mounts[0] {
		t.Fatalf("filesystem mount = %+v, want %+v", got.FilesystemMounts[0], mounts[0])
	}
}
