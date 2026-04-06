package fastsandbox

import (
	"context"
	"io"
)

// PrivOps abstracts privileged operations that require root.
//
// Two implementations exist:
//   - DirectPrivOps: shells out to zfs/ip/mknod directly (requires root).
//   - SocketPrivOps: delegates to homestead-smelter-host via AF_UNIX SEQPACKET.
//
// The Orchestrator calls PrivOps methods at nine sites during the VM lifecycle.
// Callers that run as root (forge-metal CLI) use DirectPrivOps. Services that
// run unprivileged (sandbox-rental-service) use SocketPrivOps.
type PrivOps interface {
	ZFSClone(ctx context.Context, snapshot, target, jobID string) error
	ZFSDestroy(ctx context.Context, dataset string) error
	TapCreate(ctx context.Context, tapName, hostCIDR string) error
	TapUp(ctx context.Context, tapName string) error
	TapDelete(ctx context.Context, tapName string) error
	SetupJail(ctx context.Context, jailRoot, zvolDev, kernelSrc string, uid, gid int) error
	StartJailer(ctx context.Context, jobID string, cfg JailerConfig) (*JailerProcess, error)
}

// JailerConfig holds the parameters needed to start a Firecracker jailer.
type JailerConfig struct {
	FirecrackerBin string
	JailerBin      string
	ChrootBaseDir  string
	UID            int
	GID            int
}

// JailerProcess represents a running Firecracker jailer process. DirectPrivOps
// populates Stdout/Stderr for serial log capture; SocketPrivOps leaves them nil
// (the smelter host daemon owns the jailer's stdio).
type JailerProcess struct {
	Pid    int
	Stdout io.ReadCloser // nil when started via socket
	Stderr io.ReadCloser // nil when started via socket

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
