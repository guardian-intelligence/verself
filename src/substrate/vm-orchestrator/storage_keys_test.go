package vmorchestrator

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestFileStorageKeyProviderCreatesStableRootOnlyKey(t *testing.T) {
	dir := t.TempDir()
	provider := NewFileStorageKeyProvider(filepath.Join(dir, "keys"))
	first, err := provider.GetOrCreateStorageKey(context.Background(), "org_a")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Key) != storageKeyBytes {
		t.Fatalf("key length = %d, want %d", len(first.Key), storageKeyBytes)
	}
	second, err := provider.GetOrCreateStorageKey(context.Background(), "org_a")
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != second.Version {
		t.Fatalf("key version changed from %s to %s", first.Version, second.Version)
	}
	if string(first.Key) != string(second.Key) {
		t.Fatal("storage key material changed across reads")
	}
	info, err := os.Stat(filepath.Join(dir, "keys", "org_a.raw"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("storage key mode = %o, want %o", got, want)
	}
	dirInfo, err := os.Stat(filepath.Join(dir, "keys"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := dirInfo.Mode().Perm(), os.FileMode(0o700); got != want {
		t.Fatalf("storage key directory mode = %o, want %o", got, want)
	}
}

func TestFileStorageKeyProviderRejectsUnsafeOrgID(t *testing.T) {
	provider := NewFileStorageKeyProvider(t.TempDir())
	if _, err := provider.GetOrCreateStorageKey(context.Background(), "../org_a"); err == nil {
		t.Fatal("GetOrCreateStorageKey accepted unsafe org id")
	}
}

func TestStorageKeyCleanupCapturesHold(t *testing.T) {
	key := make([]byte, storageKeyBytes)
	for i := range key {
		key[i] = byte(i + 1)
	}
	manager := NewStorageKeyManager(staticStorageKeyProvider{material: StorageKeyMaterial{Key: key, Version: "test-key"}}, nil)
	hold, err := manager.Acquire(context.Background(), "org_a", "lease-a")
	if err != nil {
		t.Fatal(err)
	}
	if slices.Equal(hold.key, make([]byte, storageKeyBytes)) {
		t.Fatal("test setup produced an empty key hold")
	}
	o := New(Config{Pool: "pool", ImageDataset: "images", GoldenDataset: "goldens", WorkloadDataset: "workloads"}, nil, WithPrivOps(&filesystemMountTestPrivOps{}), WithStorageKeyManager(manager))
	runtime := &LeaseRuntime{}
	captured := hold
	o.prependStorageKeyCleanup(runtime, "org_a", "lease-a", hold)
	hold = nil
	if hold != nil {
		t.Fatal("test setup failed to clear outer hold reference")
	}
	for i := len(runtime.cleanups) - 1; i >= 0; i-- {
		runtime.cleanups[i]()
	}
	manager.mu.Lock()
	_, stillHeld := manager.refs["org_a"]
	manager.mu.Unlock()
	if stillHeld {
		t.Fatal("storage key hold was not released")
	}
	if !slices.Equal(captured.key, make([]byte, storageKeyBytes)) {
		t.Fatal("released storage key material was not zeroed")
	}
}

func TestStorageKeyReleaseSerializesIdleUnloadWithAcquire(t *testing.T) {
	key := make([]byte, storageKeyBytes)
	for i := range key {
		key[i] = byte(i + 1)
	}
	manager := NewStorageKeyManager(staticStorageKeyProvider{material: StorageKeyMaterial{Key: key, Version: "test-key"}}, nil)
	hold, err := manager.Acquire(context.Background(), "org_a", "lease-a")
	if err != nil {
		t.Fatal(err)
	}

	idleStarted := make(chan struct{})
	finishIdle := make(chan struct{})
	releaseDone := make(chan error, 1)
	go func() {
		_, releaseErr := hold.ReleaseWhenIdle(context.Background(), func(context.Context, string) error {
			close(idleStarted)
			<-finishIdle
			return nil
		})
		releaseDone <- releaseErr
	}()
	<-idleStarted

	acquireDone := make(chan error, 1)
	go func() {
		next, acquireErr := manager.Acquire(context.Background(), "org_a", "lease-b")
		if acquireErr == nil {
			_, acquireErr = next.Release(context.Background())
		}
		acquireDone <- acquireErr
	}()

	select {
	case err := <-acquireDone:
		t.Fatalf("acquire completed while idle unload was still running: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	close(finishIdle)
	if err := <-releaseDone; err != nil {
		t.Fatal(err)
	}
	if err := <-acquireDone; err != nil {
		t.Fatal(err)
	}
}

type staticStorageKeyProvider struct {
	material StorageKeyMaterial
}

func (p staticStorageKeyProvider) GetOrCreateStorageKey(context.Context, string) (StorageKeyMaterial, error) {
	return StorageKeyMaterial{Key: append([]byte(nil), p.material.Key...), Version: p.material.Version}, nil
}
