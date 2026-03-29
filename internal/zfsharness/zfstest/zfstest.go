//go:build integration

// Package zfstest provides test helpers for ZFS-backed tests.
//
// Tests using this package require the "integration" build tag:
//
//	sudo env PATH="$PATH" go test -tags integration ./...
//
// NewHarness creates a file-backed ZFS pool per test, matching
// the pattern from tests/testbed/testbed.sh: each test gets a
// fresh, isolated filesystem via ZFS clone.
package zfstest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/forge-metal/forge-metal/internal/zfsharness"
)

// NewHarness creates a file-backed 256MB ZFS pool with golden and CI
// datasets. The pool is destroyed when the test completes.
//
// Requires root and zfs in PATH. Fails loudly if either is missing —
// use `go test -run TestZFS` to select these tests explicitly, or run
// the full suite with `sudo env PATH="$PATH" go test ./...`.
func NewHarness(t *testing.T) *zfsharness.Harness {
	t.Helper()

	if os.Getuid() != 0 {
		t.Fatal("ZFS tests require root — run with: sudo env PATH=\"$PATH\" go test ./...")
	}

	if _, err := exec.LookPath("zfs"); err != nil {
		t.Fatal("zfs not in PATH — install zfsutils-linux or enter nix develop")
	}

	poolName := fmt.Sprintf("fmtest_%d", time.Now().UnixNano()%100000)
	imgDir := t.TempDir()
	imgPath := filepath.Join(imgDir, "zfs.img")

	// Create sparse file (256MB is enough for tests).
	if err := exec.Command("truncate", "-s", "256M", imgPath).Run(); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	// Create pool.
	// Incus pattern: sync=disabled for file-backed pools to avoid double-buffering.
	out, err := exec.Command("zpool", "create",
		"-o", "feature@encryption=disabled",
		"-O", "sync=disabled",
		"-O", "compression=lz4",
		"-O", "atime=off",
		poolName, imgPath,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("zpool create: %v\n%s", err, out)
	}

	t.Cleanup(func() {
		exec.Command("zpool", "destroy", "-f", poolName).Run()
		os.Remove(imgPath)
	})

	cfg := zfsharness.Config{
		Pool:           poolName,
		GoldenDataset:  "golden",
		CIDataset:      "ci",
		CommandTimeout: 10 * time.Second,
	}
	h := zfsharness.New(cfg)

	// Create the golden and ci datasets.
	ctx := context.Background()
	for _, ds := range []string{poolName + "/golden", poolName + "/ci"} {
		cmd := exec.Command("zfs", "create", ds)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("zfs create %s: %v\n%s", ds, err, out)
		}
	}

	_ = ctx
	return h
}

// SeedGolden writes a test file into the golden dataset and creates
// the @ready snapshot.
func SeedGolden(t *testing.T, h *zfsharness.Harness) {
	t.Helper()

	workDir := filepath.Join(h.GoldenMountpoint(), "workspace")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("mkdir golden workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "package.json"), []byte(`{"name":"test"}`), 0644); err != nil {
		t.Fatalf("write golden test file: %v", err)
	}

	snap := h.GoldenSnapshot()
	if out, err := exec.Command("zfs", "snapshot", snap).CombinedOutput(); err != nil {
		t.Fatalf("zfs snapshot %s: %v\n%s", snap, err, out)
	}
}
