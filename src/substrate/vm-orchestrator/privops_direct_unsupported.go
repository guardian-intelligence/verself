//go:build !linux

package vmorchestrator

import (
	"context"
	"fmt"
)

type DirectPrivOps struct{}

func unsupportedHostMutation(operation string) error {
	return fmt.Errorf("%s requires the Linux vm-orchestrator host runtime", operation)
}

func (DirectPrivOps) ZFSClone(context.Context, string, string, string) error {
	return unsupportedHostMutation("zfs clone")
}

func (DirectPrivOps) ZFSSnapshot(context.Context, string, string, map[string]string) error {
	return unsupportedHostMutation("zfs snapshot")
}

func (DirectPrivOps) ZFSDestroy(context.Context, string) error {
	return unsupportedHostMutation("zfs destroy")
}

func (DirectPrivOps) ZFSDestroyRecursive(context.Context, string) error {
	return unsupportedHostMutation("zfs destroy recursive")
}

func (DirectPrivOps) ZFSEnsureFilesystem(context.Context, string) error {
	return unsupportedHostMutation("zfs ensure filesystem")
}

func (DirectPrivOps) ZFSSendReceive(context.Context, string, string) error {
	return unsupportedHostMutation("zfs send receive")
}

func (DirectPrivOps) ZFSSnapshotExists(context.Context, string) (bool, error) {
	return false, unsupportedHostMutation("zfs snapshot exists")
}

func (DirectPrivOps) ZFSDatasetExists(context.Context, string) (bool, error) {
	return false, unsupportedHostMutation("zfs dataset exists")
}

func (DirectPrivOps) ZFSSetProperty(context.Context, string, string, string) error {
	return unsupportedHostMutation("zfs set property")
}

func (DirectPrivOps) ZFSGetProperty(context.Context, string, string) (string, error) {
	return "", unsupportedHostMutation("zfs get property")
}

func (DirectPrivOps) ZFSCreateVolume(context.Context, string, uint64, string) error {
	return unsupportedHostMutation("zfs create volume")
}

func (DirectPrivOps) ZFSWriteVolumeFromFile(context.Context, string, string) (uint64, error) {
	return 0, unsupportedHostMutation("zfs write volume from file")
}

func (DirectPrivOps) ZFSMkfs(context.Context, string, string, string) error {
	return unsupportedHostMutation("zfs mkfs")
}

func (DirectPrivOps) ZFSRename(context.Context, string, string) error {
	return unsupportedHostMutation("zfs rename")
}

func (DirectPrivOps) ZFSListChildren(context.Context, string) ([]string, error) {
	return nil, unsupportedHostMutation("zfs list children")
}

func (DirectPrivOps) UnmountStaleZvolMounts(context.Context, string) (int, error) {
	return 0, unsupportedHostMutation("unmount stale zvol mounts")
}

func (DirectPrivOps) FlushBlockDevice(context.Context, string) error {
	return unsupportedHostMutation("flush block device")
}

func (DirectPrivOps) TapCreate(context.Context, string, string, int, int) error {
	return unsupportedHostMutation("tap create")
}

func (DirectPrivOps) TapUp(context.Context, string) error {
	return unsupportedHostMutation("tap up")
}

func (DirectPrivOps) TapDelete(context.Context, string) error {
	return unsupportedHostMutation("tap delete")
}

func (DirectPrivOps) SetupJail(context.Context, string, string, int, int, []JailBlockDevice) error {
	return unsupportedHostMutation("setup jail")
}

func (DirectPrivOps) StartJailer(context.Context, string, JailerConfig) (*JailerProcess, error) {
	return nil, unsupportedHostMutation("start jailer")
}

func (DirectPrivOps) Chmod(context.Context, string, uint32) error {
	return unsupportedHostMutation("chmod")
}
