//go:build linux

package vmorchestrator

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
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
