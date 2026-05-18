//go:build linux

package vmorchestrator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/verself/vm-orchestrator/zfs"
)

func TestEnsureJailDirectoryRestoresModeAfterRestrictiveUmask(t *testing.T) {
	oldUmask := syscall.Umask(0o077)
	defer syscall.Umask(oldUmask)

	dir := filepath.Join(t.TempDir(), "root", "drives")
	if err := ensureJailDirectory(dir); err != nil {
		t.Fatalf("ensureJailDirectory returned error: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat jail directory: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o755); got != want {
		t.Fatalf("jail directory mode = %o, want %o", got, want)
	}
}

func TestFinishZFSEnsureFilesystemCreateTreatsConcurrentCreateAsSuccess(t *testing.T) {
	createErr := errors.New("exit status 1")
	err := finishZFSEnsureFilesystemCreate(context.Background(), "pool/goldens/scope/generations", []byte("dataset already exists"), createErr, func(context.Context, string) (bool, error) {
		return true, nil
	})
	if err != nil {
		t.Fatalf("finishZFSEnsureFilesystemCreate returned error: %v", err)
	}
}

func TestFinishZFSEnsureFilesystemCreateReturnsOriginalFailure(t *testing.T) {
	createErr := errors.New("exit status 1")
	err := finishZFSEnsureFilesystemCreate(context.Background(), "pool/goldens/scope/generations", []byte("permission denied"), createErr, func(context.Context, string) (bool, error) {
		return false, nil
	})
	if err == nil {
		t.Fatal("finishZFSEnsureFilesystemCreate returned nil")
	}
	if !errors.Is(err, createErr) {
		t.Fatalf("error does not wrap create error: %v", err)
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("error = %q, want command output", err.Error())
	}
}

func TestZFSCloneErrorClassifiesMissingSourceSnapshot(t *testing.T) {
	source := "pool/orgs/org_a/goldens/scope/generations/gen-a@sealed"
	target := "pool/orgs/org_a/workloads/lease-a/mounts/00-workspace"
	err := zfsCloneError(source, target, []byte("cannot open 'pool/orgs/org_a/goldens/scope/generations/gen-a@sealed': dataset does not exist\n"), errors.New("exit status 1"))
	if !errors.Is(err, zfs.ErrSnapshotNotFound) {
		t.Fatalf("zfsCloneError = %v, want ErrSnapshotNotFound", err)
	}
}

func TestZFSCloneErrorDoesNotClassifyTargetParentFailure(t *testing.T) {
	source := "pool/orgs/org_a/goldens/scope/generations/gen-a@sealed"
	target := "pool/orgs/org_a/workloads/lease-a/mounts/00-workspace"
	err := zfsCloneError(source, target, []byte("cannot create 'pool/orgs/org_a/workloads/lease-a/mounts/00-workspace': parent does not exist\n"), errors.New("exit status 1"))
	if errors.Is(err, zfs.ErrSnapshotNotFound) {
		t.Fatalf("zfsCloneError = %v, did not want ErrSnapshotNotFound", err)
	}
}
