package vmorchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/forge-metal/vm-orchestrator/vmproto"
)

func TestCheckpointRequestRequiresAuthorizedRef(t *testing.T) {
	t.Parallel()

	ops := &checkpointTestPrivOps{}
	orch := New(DefaultConfig(), nil, WithPrivOps(ops))

	resp := orch.handleCheckpointRequest(RunSpec{RunID: "job-1"}, "pool/workloads/job-1", nil, vmproto.CheckpointRequest{
		RequestID: "req-1",
		Operation: vmproto.CheckpointOperationSave,
		Ref:       "pg-demo",
	}, nil)

	if resp.Accepted {
		t.Fatal("expected checkpoint request to be rejected")
	}
	if !strings.Contains(resp.Error, "not authorized") {
		t.Fatalf("expected authorization error, got %q", resp.Error)
	}
	if len(ops.snapshots) != 0 {
		t.Fatalf("expected no snapshot calls, got %d", len(ops.snapshots))
	}
}

func TestCheckpointRequestSnapshotsActiveDatasetOnly(t *testing.T) {
	t.Parallel()

	ops := &checkpointTestPrivOps{}
	orch := New(DefaultConfig(), nil, WithPrivOps(ops))

	resp := orch.handleCheckpointRequest(RunSpec{
		RunID:              "job-1",
		CheckpointSaveRefs: []string{"pg-demo"},
	}, "pool/workloads/job-1", normalizeCheckpointRefSet([]string{"pg-demo"}), vmproto.CheckpointRequest{
		RequestID: "req-1",
		Operation: vmproto.CheckpointOperationSave,
		Ref:       "pg-demo",
	}, nil)

	if !resp.Accepted {
		t.Fatalf("expected checkpoint request to be accepted: %s", resp.Error)
	}
	if resp.VersionID == "" {
		t.Fatal("expected checkpoint version id")
	}
	if len(ops.snapshots) != 1 {
		t.Fatalf("expected one snapshot call, got %d", len(ops.snapshots))
	}
	call := ops.snapshots[0]
	if call.dataset != "pool/workloads/job-1" {
		t.Fatalf("snapshot dataset: got %q", call.dataset)
	}
	if strings.Contains(call.snapshotName, "pg-demo") {
		t.Fatalf("snapshot name should be host generated and not include guest ref: %q", call.snapshotName)
	}
	if call.properties["forge:checkpoint_ref"] != "pg-demo" {
		t.Fatalf("checkpoint ref property: got %q", call.properties["forge:checkpoint_ref"])
	}
	if call.properties["forge:checkpoint_version"] != resp.VersionID {
		t.Fatalf("checkpoint version property: got %q want %q", call.properties["forge:checkpoint_version"], resp.VersionID)
	}
}

func TestCleanupRetainsCheckpointedWorkloadDataset(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Pool = "pool"
	cfg.WorkloadDataset = "workloads"
	ops := &checkpointTestPrivOps{}
	orch := New(cfg, nil, WithPrivOps(ops))

	retained, err := orch.destroyDisposableWorkloadDataset(context.Background(), "pool/workloads/job-1", true)
	if err != nil {
		t.Fatalf("cleanup returned error: %v", err)
	}
	if !retained {
		t.Fatal("expected checkpointed workload dataset to be retained")
	}
	if len(ops.destroys) != 0 {
		t.Fatalf("expected no destroy calls, got %v", ops.destroys)
	}
}

func TestCleanupDestroysUncheckpointedWorkloadDataset(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Pool = "pool"
	cfg.WorkloadDataset = "workloads"
	ops := &checkpointTestPrivOps{}
	orch := New(cfg, nil, WithPrivOps(ops))

	retained, err := orch.destroyDisposableWorkloadDataset(context.Background(), "pool/workloads/job-1", false)
	if err != nil {
		t.Fatalf("cleanup returned error: %v", err)
	}
	if retained {
		t.Fatal("uncheckpointed workload dataset should not be retained")
	}
	if len(ops.destroys) != 1 || ops.destroys[0] != "pool/workloads/job-1" {
		t.Fatalf("destroy calls: got %v", ops.destroys)
	}
}

type checkpointTestPrivOps struct {
	snapshots []checkpointSnapshotCall
	destroys  []string
}

type checkpointSnapshotCall struct {
	dataset      string
	snapshotName string
	properties   map[string]string
}

func (p *checkpointTestPrivOps) ZFSClone(context.Context, string, string, string) error {
	return nil
}

func (p *checkpointTestPrivOps) ZFSSnapshot(_ context.Context, dataset, snapshotName string, properties map[string]string) error {
	props := make(map[string]string, len(properties))
	for key, value := range properties {
		props[key] = value
	}
	p.snapshots = append(p.snapshots, checkpointSnapshotCall{
		dataset:      dataset,
		snapshotName: snapshotName,
		properties:   props,
	})
	return nil
}

func (p *checkpointTestPrivOps) ZFSDestroy(_ context.Context, dataset string) error {
	p.destroys = append(p.destroys, dataset)
	return nil
}

func (p *checkpointTestPrivOps) TapCreate(context.Context, string, string) error {
	return nil
}

func (p *checkpointTestPrivOps) TapUp(context.Context, string) error {
	return nil
}

func (p *checkpointTestPrivOps) TapDelete(context.Context, string) error {
	return nil
}

func (p *checkpointTestPrivOps) SetupJail(context.Context, string, string, string, int, int) error {
	return nil
}

func (p *checkpointTestPrivOps) StartJailer(context.Context, string, JailerConfig) (*JailerProcess, error) {
	return nil, nil
}

func (p *checkpointTestPrivOps) Chmod(context.Context, string, uint32) error {
	return nil
}
