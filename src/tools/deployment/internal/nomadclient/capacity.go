package nomadclient

import (
	"context"
	"fmt"

	"github.com/hashicorp/nomad/api"
)

type NodeCapacity struct {
	NodeID   string
	Name     string
	Class    string
	CPU      int64
	MemoryMB int64
	DiskMB   int64
}

func (c *Client) NodeCapacities(ctx context.Context) ([]NodeCapacity, error) {
	nodes, _, err := c.api.Nodes().List((&api.QueryOptions{
		Params: map[string]string{"resources": "true"},
	}).WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("list nomad nodes: %w", err)
	}
	out := make([]NodeCapacity, 0, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}
		if node.Status != api.NodeStatusReady || node.SchedulingEligibility != api.NodeSchedulingEligible || node.Drain {
			continue
		}
		if node.NodeResources == nil {
			continue
		}
		capacity := NodeCapacity{
			NodeID: node.ID,
			Name:   node.Name,
			Class:  node.NodeClass,
		}
		capacity.CPU = node.NodeResources.Cpu.CpuShares
		capacity.MemoryMB = node.NodeResources.Memory.MemoryMB
		capacity.DiskMB = node.NodeResources.Disk.DiskMB
		if node.ReservedResources != nil {
			if err := subtractReservedResources(&capacity, node.ReservedResources); err != nil {
				return nil, fmt.Errorf("parse reserved resources for nomad node %q: %w", node.ID, err)
			}
		}
		capacity.CPU = maxZero(capacity.CPU)
		capacity.MemoryMB = maxZero(capacity.MemoryMB)
		capacity.DiskMB = maxZero(capacity.DiskMB)
		out = append(out, capacity)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("list nomad nodes: no ready eligible nodes returned resource capacity")
	}
	return out, nil
}

func maxZero(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func subtractReservedResources(capacity *NodeCapacity, reserved *api.NodeReservedResources) error {
	reservedCPU, err := checkedInt64FromUint64(reserved.Cpu.CpuShares, "reserved cpu shares")
	if err != nil {
		return err
	}
	reservedMemoryMB, err := checkedInt64FromUint64(reserved.Memory.MemoryMB, "reserved memory mb")
	if err != nil {
		return err
	}
	reservedDiskMB, err := checkedInt64FromUint64(reserved.Disk.DiskMB, "reserved disk mb")
	if err != nil {
		return err
	}
	capacity.CPU -= reservedCPU
	capacity.MemoryMB -= reservedMemoryMB
	capacity.DiskMB -= reservedDiskMB
	return nil
}
