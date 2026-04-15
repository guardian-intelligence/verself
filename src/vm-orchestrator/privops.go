package vmorchestrator

import (
	"context"
	"io"
)

type PrivOps interface {
	ZFSClone(ctx context.Context, snapshot, target, leaseID string) error
	ZFSSnapshot(ctx context.Context, dataset, snapshotName string, properties map[string]string) error
	ZFSDestroy(ctx context.Context, dataset string) error
	TapCreate(ctx context.Context, tapName, hostCIDR string, ownerUID, ownerGID int) error
	TapUp(ctx context.Context, tapName string) error
	TapDelete(ctx context.Context, tapName string) error
	SetupJail(ctx context.Context, jailRoot, zvolDev, kernelSrc string, uid, gid int) error
	StartJailer(ctx context.Context, leaseID string, cfg JailerConfig) (*JailerProcess, error)
	Chmod(ctx context.Context, path string, mode uint32) error
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
