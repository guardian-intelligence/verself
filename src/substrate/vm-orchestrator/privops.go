package vmorchestrator

import (
	"context"
	"io"

	"github.com/verself/vm-orchestrator/zfs"
)

// PrivOps is the privileged host adapter the orchestrator depends on. The
// ZFS half is contributed by zfs.PrivZFS so the same contract describes
// what VolumeLifecycle needs without duplicating method signatures.
type PrivOps interface {
	zfs.PrivZFS
	FlushBlockDevice(ctx context.Context, path string) error
	TapCreate(ctx context.Context, tapName, hostCIDR string, ownerUID, ownerGID int) error
	TapUp(ctx context.Context, tapName string) error
	TapDelete(ctx context.Context, tapName string) error
	SetupJail(ctx context.Context, jailRoot, kernelSrc string, uid, gid int, devices []JailBlockDevice) error
	StartJailer(ctx context.Context, leaseID string, cfg JailerConfig) (*JailerProcess, error)
	Chmod(ctx context.Context, path string, mode uint32) error
}

type JailBlockDevice struct {
	HostPath string
	JailPath string
}

type JailerConfig struct {
	FirecrackerBin string
	JailerBin      string
	ChrootBaseDir  string
	UID            int
	GID            int
}

type JailerProcess struct {
	Pid    int
	Stdout io.ReadCloser
	Stderr io.ReadCloser

	waitFn func() error
	killFn func() error
}

func (j *JailerProcess) Wait() error { return j.waitFn() }
func (j *JailerProcess) Kill() error { return j.killFn() }
