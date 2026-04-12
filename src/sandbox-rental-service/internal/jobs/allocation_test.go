package jobs

import (
	"context"
	"errors"
	"strings"
	"testing"

	vmorchestrator "github.com/forge-metal/vm-orchestrator"
)

type capacityRunner struct {
	capacity vmorchestrator.Capacity
	err      error
}

func (r capacityRunner) GetCapacity(context.Context) (vmorchestrator.Capacity, error) {
	return r.capacity, r.err
}

func (capacityRunner) EnsureRun(context.Context, vmorchestrator.HostRunSpec) (string, bool, error) {
	panic("unexpected EnsureRun")
}

func (capacityRunner) StreamRunEvents(context.Context, string, uint64, bool, func(vmorchestrator.HostRunEvent) error) error {
	panic("unexpected StreamRunEvents")
}

func (capacityRunner) WaitRun(context.Context, string, bool) (vmorchestrator.HostRunSnapshot, error) {
	panic("unexpected WaitRun")
}

func (capacityRunner) CancelRun(context.Context, string, string) (bool, error) {
	panic("unexpected CancelRun")
}

func TestBuildBillingAllocationUsesFinalSKUIDs(t *testing.T) {
	t.Parallel()

	alloc := buildBillingAllocation(2, 2048, 1073741824)

	if got := alloc[billingSKUCompute]; got != 2 {
		t.Fatalf("compute allocation = %v, want 2", got)
	}
	if got := alloc[billingSKUMemory]; got != 2 {
		t.Fatalf("memory allocation = %v, want 2", got)
	}
	if got := alloc[billingSKUBlockStorage]; got != 1 {
		t.Fatalf("block storage allocation = %v, want 1", got)
	}
	if len(alloc) != 3 {
		t.Fatalf("allocation size = %d, want 3", len(alloc))
	}
}

func TestCurrentBillingAllocationUsesLiveCapacity(t *testing.T) {
	t.Parallel()

	svc := &Service{Orchestrator: capacityRunner{capacity: vmorchestrator.Capacity{
		VCPUsPerVM:             4,
		MemoryMiBPerVM:         8192,
		RootfsProvisionedBytes: 2 * 1024 * 1024 * 1024,
	}}}

	alloc, err := svc.currentBillingAllocation(context.Background())
	if err != nil {
		t.Fatalf("currentBillingAllocation returned error: %v", err)
	}
	if got := alloc[billingSKUCompute]; got != 4 {
		t.Fatalf("compute allocation = %v, want 4", got)
	}
	if got := alloc[billingSKUMemory]; got != 8 {
		t.Fatalf("memory allocation = %v, want 8", got)
	}
	if got := alloc[billingSKUBlockStorage]; got != 2 {
		t.Fatalf("block storage allocation = %v, want 2", got)
	}
}

func TestCurrentBillingAllocationFailsWithoutRootfsSize(t *testing.T) {
	t.Parallel()

	svc := &Service{Orchestrator: capacityRunner{capacity: vmorchestrator.Capacity{
		VCPUsPerVM:     2,
		MemoryMiBPerVM: 2048,
	}}}

	_, err := svc.currentBillingAllocation(context.Background())
	if err == nil {
		t.Fatal("currentBillingAllocation succeeded without rootfs_provisioned_bytes")
	}
	if !strings.Contains(err.Error(), "rootfs_provisioned_bytes") {
		t.Fatalf("error = %v, want rootfs_provisioned_bytes", err)
	}
}

func TestCurrentBillingAllocationPropagatesCapacityErrors(t *testing.T) {
	t.Parallel()

	capacityErr := errors.New("capacity down")
	svc := &Service{Orchestrator: capacityRunner{err: capacityErr}}

	_, err := svc.currentBillingAllocation(context.Background())
	if !errors.Is(err, capacityErr) {
		t.Fatalf("error = %v, want wrapped capacity error", err)
	}
}

func TestUsageSummaryForOutcomeIncludesRootfsProvisionedBytes(t *testing.T) {
	t.Parallel()

	summary := usageSummaryForOutcome(executionOutcome{
		RootfsProvisionedBytes: 4096,
		ZFSWritten:             123,
		StdoutBytes:            456,
		StderrBytes:            789,
		Metrics: &vmorchestrator.VMMetrics{
			BootTimeUs:      11,
			BlockReadBytes:  22,
			BlockWriteBytes: 33,
			BlockReadCount:  44,
			BlockWriteCount: 55,
			NetRxBytes:      66,
			NetTxBytes:      77,
			VCPUExitCount:   88,
		},
	})

	if got := summary["rootfs_provisioned_bytes"]; got != uint64(4096) {
		t.Fatalf("rootfs_provisioned_bytes = %#v, want 4096", got)
	}
	if got := summary["zfs_written_bytes"]; got != uint64(123) {
		t.Fatalf("zfs_written_bytes = %#v, want 123", got)
	}
	if got := summary["block_read_bytes"]; got != uint64(22) {
		t.Fatalf("block_read_bytes = %#v, want 22", got)
	}
	if got := summary["vcpu_exit_count"]; got != uint64(88) {
		t.Fatalf("vcpu_exit_count = %#v, want 88", got)
	}
}
