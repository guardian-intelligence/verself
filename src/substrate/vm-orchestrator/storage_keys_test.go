package vmorchestrator

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
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

func TestStorageKeyReleaseZeroesMaterialAndClearsRef(t *testing.T) {
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
	captured := hold
	if !hold.Release(context.Background()) {
		t.Fatal("release did not report the last storage key material ref")
	}
	if captured.Release(context.Background()) {
		t.Fatal("second release reported a live ref")
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

func TestStorageKeyReleaseTracksConcurrentMaterialRefs(t *testing.T) {
	key := make([]byte, storageKeyBytes)
	for i := range key {
		key[i] = byte(i + 1)
	}
	manager := NewStorageKeyManager(staticStorageKeyProvider{material: StorageKeyMaterial{Key: key, Version: "test-key"}}, nil)
	first, err := manager.Acquire(context.Background(), "org_a", "lease-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Acquire(context.Background(), "org_a", "lease-b")
	if err != nil {
		t.Fatal(err)
	}
	if first.Release(context.Background()) {
		t.Fatal("first release reported last ref while second ref was live")
	}
	if !second.Release(context.Background()) {
		t.Fatal("second release did not report last ref")
	}
}

type staticStorageKeyProvider struct {
	material StorageKeyMaterial
}

func (p staticStorageKeyProvider) GetOrCreateStorageKey(context.Context, string) (StorageKeyMaterial, error) {
	return StorageKeyMaterial{Key: append([]byte(nil), p.material.Key...), Version: p.material.Version}, nil
}
