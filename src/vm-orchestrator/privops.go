package vmorchestrator

import (
	"context"
	"io"
)

// PrivOps abstracts privileged operations that require root.
//
// The current implementation is DirectPrivOps only. vm-orchestrator is the
// long-lived privileged process on the host, so it performs these operations
// directly instead of delegating them through a second socket layer.
type PrivOps interface {
	ZFSClone(ctx context.Context, snapshot, target, runID string) error
	ZFSSnapshot(ctx context.Context, dataset, snapshotName string, properties map[string]string) error
	ZFSDestroy(ctx context.Context, dataset string) error
	TapCreate(ctx context.Context, tapName, hostCIDR string) error
	TapUp(ctx context.Context, tapName string) error
	TapDelete(ctx context.Context, tapName string) error
	SetupJail(ctx context.Context, jailRoot, zvolDev, kernelSrc string, uid, gid int) error
	StartJailer(ctx context.Context, runID string, cfg JailerConfig) (*JailerProcess, error)
	Chmod(ctx context.Context, path string, mode uint32) error
}

// JailerConfig holds the parameters needed to start a Firecracker jailer.
type JailerConfig struct {
	FirecrackerBin string
	JailerBin      string
	ChrootBaseDir  string
	UID            int
	GID            int
}

// JailerProcess represents a running Firecracker jailer process.
type JailerProcess struct {
	Pid    int
	Stdout io.ReadCloser
	Stderr io.ReadCloser

	waitFn func() error
	killFn func() error
}

// Wait blocks until the jailer process exits.
func (j *JailerProcess) Wait() error { return j.waitFn() }

// Kill sends SIGKILL to the jailer process.
func (j *JailerProcess) Kill() error { return j.killFn() }

// Option configures an Orchestrator.
type Option func(*Orchestrator)

// WithPrivOps sets the privilege operations backend. The default is
// DirectPrivOps{}, which requires root.
func WithPrivOps(ops PrivOps) Option {
	return func(o *Orchestrator) {
		o.ops = ops
	}
}
