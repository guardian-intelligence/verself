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

func TestOrgRuntimeStateRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, err := openHostStateStore(":memory:", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.close() }()

	readyAt := time.Now().UTC().Truncate(time.Nanosecond)
	verifiedAt := readyAt.Add(time.Second)
	want := orgRuntimeSnapshot{
		OrgID:      "org_a",
		QuotaBytes: 1024,
		Images: []orgRuntimeImageState{{
			ImageRef:       "substrate",
			SourceSnapshot: "pool/images/substrate@ready",
			SourceDigest:   "digest-a",
			OrgSnapshot:    "pool/orgs/org_a/images/substrate-digest-a@ready",
		}},
		ReadyAt:    readyAt,
		VerifiedAt: verifiedAt,
	}
	if err := store.upsertOrgRuntime(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := store.getOrgRuntime(ctx, "org_a")
	if err != nil {
		t.Fatal(err)
	}
	if got.OrgID != want.OrgID || got.QuotaBytes != want.QuotaBytes || !got.ReadyAt.Equal(want.ReadyAt) || !got.VerifiedAt.Equal(want.VerifiedAt) {
		t.Fatalf("runtime snapshot = %+v, want %+v", got, want)
	}
	if len(got.Images) != 1 || got.Images[0] != want.Images[0] {
		t.Fatalf("runtime images = %+v, want %+v", got.Images, want.Images)
	}
}

func TestOrgRuntimeStateMergesImageRefs(t *testing.T) {
	ctx := context.Background()
	store, err := openHostStateStore(":memory:", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.close() }()

	first := orgRuntimeSnapshot{
		OrgID:      "org_a",
		QuotaBytes: 1024,
		Images: []orgRuntimeImageState{{
			ImageRef:       "substrate",
			SourceSnapshot: "pool/images/substrate@ready",
			SourceDigest:   "digest-a",
			OrgSnapshot:    "pool/orgs/org_a/images/substrate-digest-a@ready",
		}},
	}
	if err := store.upsertOrgRuntime(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := orgRuntimeSnapshot{
		OrgID:      "org_a",
		QuotaBytes: 2048,
		Images: []orgRuntimeImageState{{
			ImageRef:       "node-22",
			SourceSnapshot: "pool/images/node-22@ready",
			SourceDigest:   "digest-b",
			OrgSnapshot:    "pool/orgs/org_a/images/node-22-digest-b@ready",
		}},
	}
	if err := store.upsertOrgRuntime(ctx, second); err != nil {
		t.Fatal(err)
	}
	got, err := store.getOrgRuntime(ctx, "org_a")
	if err != nil {
		t.Fatal(err)
	}
	if got.QuotaBytes != second.QuotaBytes {
		t.Fatalf("quota bytes = %d, want %d", got.QuotaBytes, second.QuotaBytes)
	}
	if len(got.Images) != 2 {
		t.Fatalf("runtime images = %+v, want two image refs", got.Images)
	}
	if got.Images[0].ImageRef != "node-22" || got.Images[1].ImageRef != "substrate" {
		t.Fatalf("runtime images = %+v, want stable sorted union", got.Images)
	}
}
