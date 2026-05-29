package nomadclient

import (
	"strings"
	"testing"

	"github.com/hashicorp/nomad/api"
)

func TestSubtractReservedResourcesRejectsOverflow(t *testing.T) {
	capacity := NodeCapacity{CPU: 1000, MemoryMB: 1000, DiskMB: 1000}
	err := subtractReservedResources(&capacity, &api.NodeReservedResources{
		Cpu: api.NodeReservedCpuResources{CpuShares: maxInt64AsUint64 + 1},
	})
	if err == nil {
		t.Fatal("subtractReservedResources() error = nil, want overflow error")
	}
	if !strings.Contains(err.Error(), "reserved cpu shares exceeds int64 range") {
		t.Fatalf("subtractReservedResources() error = %q, want reserved cpu overflow", err)
	}
}
