package vmorchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/verself/vm-orchestrator/zfs"
)

func TestOrgRuntimeManagerRequireReadyUsesResidentState(t *testing.T) {
	ctx := context.Background()
	roots := zfs.Roots{Pool: "pool", ImageDataset: "images", GoldenDataset: "goldens", WorkloadDataset: "workloads"}
	shape := OrgRuntimeShape{
		StorageNamespace: StorageNamespace{OrgID: "org_a", QuotaBytes: 1024},
		ImageRefs:        []string{"substrate"},
	}
	orgSnapshot, err := zfs.OrgImageSnapshot(roots, "org_a", "substrate", "digest-a")
	if err != nil {
		t.Fatal(err)
	}
	manager := NewOrgRuntimeManager(Config{DefaultSubstrateRef: "substrate"}, roots, nil, nil, nil, nil)
	manager.setSnapshot(orgRuntimeSnapshot{
		OrgID:      "org_a",
		QuotaBytes: 1024,
		Images: []orgRuntimeImageState{{
			ImageRef:       "substrate",
			SourceSnapshot: "pool/images/substrate@ready",
			SourceDigest:   "digest-a",
			OrgSnapshot:    orgSnapshot.String(),
		}},
		ReadyAt:    time.Now().UTC(),
		VerifiedAt: time.Now().UTC(),
	})

	status, err := manager.Ensure(ctx, shape)
	if err != nil {
		t.Fatalf("ensure resident runtime: %v", err)
	}
	if len(status.Images) != 1 || status.Images[0].OrgSnapshot != orgSnapshot.String() {
		t.Fatalf("status images = %+v, want %s", status.Images, orgSnapshot.String())
	}
	plan, err := manager.RequireReady(ctx, shape)
	if err != nil {
		t.Fatalf("require ready: %v", err)
	}
	if got := plan.ImageSnapshots["substrate"].String(); got != orgSnapshot.String() {
		t.Fatalf("substrate snapshot = %s, want %s", got, orgSnapshot.String())
	}
}

func TestOrgRuntimeManagerRequireReadyMissesWithoutResidentImage(t *testing.T) {
	ctx := context.Background()
	roots := zfs.Roots{Pool: "pool", ImageDataset: "images", GoldenDataset: "goldens", WorkloadDataset: "workloads"}
	orgSnapshot, err := zfs.OrgImageSnapshot(roots, "org_a", "substrate", "digest-a")
	if err != nil {
		t.Fatal(err)
	}
	manager := NewOrgRuntimeManager(Config{DefaultSubstrateRef: "substrate"}, roots, nil, nil, nil, nil)
	manager.setSnapshot(orgRuntimeSnapshot{
		OrgID:      "org_a",
		QuotaBytes: 1024,
		Images: []orgRuntimeImageState{{
			ImageRef:       "substrate",
			SourceSnapshot: "pool/images/substrate@ready",
			SourceDigest:   "digest-a",
			OrgSnapshot:    orgSnapshot.String(),
		}},
		ReadyAt:    time.Now().UTC(),
		VerifiedAt: time.Now().UTC(),
	})

	_, err = manager.RequireReady(ctx, OrgRuntimeShape{
		StorageNamespace: StorageNamespace{OrgID: "org_a", QuotaBytes: 1024},
		ImageRefs:        []string{"node-22"},
	})
	if !errors.Is(err, errOrgRuntimeNotReady) {
		t.Fatalf("require missing image error = %v, want %v", err, errOrgRuntimeNotReady)
	}
}
