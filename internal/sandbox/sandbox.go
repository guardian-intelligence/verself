package sandbox

import (
	"context"
	"fmt"
)

// Sandbox manages gVisor container lifecycle for a single CI job.
//
// Flow:
//  1. ZFS clone golden snapshot -> job workspace
//  2. Create OCI bundle from ZFS clone as rootfs
//  3. runsc create + start (gVisor sandbox)
//  4. CI phases execute inside sandbox
//  5. Collect metrics from cgroup stats
//  6. runsc delete + ZFS destroy
type Sandbox struct {
	jobID      string
	zfsPool    string
	goldenSnap string
}

// New creates a Sandbox for the given job.
func New(jobID, zfsPool, goldenSnap string) *Sandbox {
	return &Sandbox{
		jobID:      jobID,
		zfsPool:    zfsPool,
		goldenSnap: goldenSnap,
	}
}

// Run executes the CI workload inside a gVisor sandbox.
func (s *Sandbox) Run(ctx context.Context) error {
	// TODO: implement ZFS clone, OCI spec generation, runsc lifecycle, metrics collection
	return fmt.Errorf("sandbox: not yet implemented")
}

// Cleanup destroys the ZFS clone and gVisor container.
func (s *Sandbox) Cleanup() error {
	// TODO: runsc delete + zfs destroy
	return fmt.Errorf("sandbox cleanup: not yet implemented")
}
