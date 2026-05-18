package vmorchestrator

import (
	"context"
	"os"
	"path/filepath"
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
