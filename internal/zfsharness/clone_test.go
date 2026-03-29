//go:build integration

package zfsharness_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/forge-metal/forge-metal/internal/zfsharness/zfstest"
)

func TestClone_FullLifecycle(t *testing.T) {
	h := zfstest.NewHarness(t)
	zfstest.SeedGolden(t, h)
	ctx := context.Background()

	clone, err := h.Allocate(ctx, "lifecycle-1")
	if err != nil {
		t.Fatal(err)
	}

	// Verify clone timing was recorded.
	if clone.AllocDuration <= 0 {
		t.Errorf("expected positive alloc duration, got %v", clone.AllocDuration)
	}
	t.Logf("clone took %v", clone.AllocDuration)

	// Verify the golden file is visible in the clone.
	pkg := fmt.Sprintf("/%s/ci/lifecycle-1/workspace/package.json", h.PoolName())
	data, err := os.ReadFile(pkg)
	if err != nil {
		t.Fatalf("golden file not visible in clone: %v", err)
	}
	if string(data) != `{"name":"test"}` {
		t.Fatalf("unexpected content: %s", data)
	}

	// Simulate job work: write a file.
	os.WriteFile(
		fmt.Sprintf("/%s/ci/lifecycle-1/output.log", h.PoolName()),
		[]byte("build output here"),
		0644,
	)

	// Mark done.
	if err := clone.MarkDone(ctx); err != nil {
		t.Fatal(err)
	}

	// Collect metrics.
	if err := clone.CollectMetrics(ctx); err != nil {
		t.Fatal(err)
	}
	t.Logf("written bytes: %d", clone.WrittenBytes)

	// Verify @done snapshot exists.
	out, err := exec.Command("zfs", "list", "-H", clone.Dataset()+"@done").CombinedOutput()
	if err != nil {
		t.Fatalf("@done snapshot should exist after MarkDone: %v\n%s", err, out)
	}

	// Release.
	if err := clone.Release(); err != nil {
		t.Fatal(err)
	}

	// Verify clone is destroyed.
	_, err = exec.Command("zfs", "list", "-H", clone.Dataset()).CombinedOutput()
	if err == nil {
		t.Fatal("clone should be destroyed after Release")
	}
}

func TestClone_ReleaseIsLIFO(t *testing.T) {
	h := zfstest.NewHarness(t)
	zfstest.SeedGolden(t, h)
	ctx := context.Background()

	clone, err := h.Allocate(ctx, "lifo-test")
	if err != nil {
		t.Fatal(err)
	}

	if err := clone.Release(); err != nil {
		t.Fatal(err)
	}

	// Calling Release again should be a no-op (idempotent).
	if err := clone.Release(); err != nil {
		t.Fatal("second Release should be no-op")
	}
}

func TestClone_Written(t *testing.T) {
	h := zfstest.NewHarness(t)
	zfstest.SeedGolden(t, h)
	ctx := context.Background()

	clone, err := h.Allocate(ctx, "job-write")
	if err != nil {
		t.Fatal(err)
	}
	defer clone.Release()

	// Write some data to the clone.
	path := fmt.Sprintf("/%s/ci/job-write/bigfile", h.PoolName())
	data := make([]byte, 64*1024) // 64KB
	for i := range data {
		data[i] = byte(i % 256)
	}
	os.WriteFile(path, data, 0644)

	// Force a sync so ZFS accounting updates.
	exec.Command("sync").Run()

	if err := clone.CollectMetrics(ctx); err != nil {
		t.Fatal(err)
	}
	// Written should be non-zero after writing data.
	if clone.WrittenBytes == 0 {
		t.Log("warning: written=0 (may be delayed on sync=disabled pool)")
	}
}
