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

func TestHarness_GoldenReady(t *testing.T) {
	h := zfstest.NewHarness(t)
	ctx := context.Background()

	// Before seeding, golden should not be ready.
	ready, err := h.GoldenReady(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("golden should not be ready before seeding")
	}

	zfstest.SeedGolden(t, h)

	ready, err = h.GoldenReady(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Fatal("golden should be ready after seeding")
	}
}

func TestHarness_Allocate(t *testing.T) {
	h := zfstest.NewHarness(t)
	zfstest.SeedGolden(t, h)
	ctx := context.Background()

	clone, err := h.Allocate(ctx, "job-1")
	if err != nil {
		t.Fatal(err)
	}
	defer clone.Release()

	// Verify clone timing was recorded.
	if clone.AllocDuration <= 0 {
		t.Errorf("expected positive alloc duration, got %v", clone.AllocDuration)
	}
	t.Logf("clone took %v", clone.AllocDuration)

	// Verify the golden file is visible in the clone.
	pkg := fmt.Sprintf("/%s/ci/job-1/workspace/package.json", h.PoolName())
	data, err := os.ReadFile(pkg)
	if err != nil {
		t.Fatalf("golden file not visible in clone: %v", err)
	}
	if string(data) != `{"name":"test"}` {
		t.Fatalf("unexpected content: %s", data)
	}
}

func TestHarness_AllocateWithProps(t *testing.T) {
	h := zfstest.NewHarness(t)
	zfstest.SeedGolden(t, h)
	ctx := context.Background()

	clone, err := h.Allocate(ctx, "job-props")
	if err != nil {
		t.Fatal(err)
	}
	defer clone.Release()

	// Verify user property was set.
	out, err := exec.Command("zfs", "get", "-H", "-p", "-o", "value",
		"forge:job_id", clone.Dataset()).CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	val := string(out)
	if len(val) > 0 && val[len(val)-1] == '\n' {
		val = val[:len(val)-1]
	}
	if val != "job-props" {
		t.Fatalf("expected forge:job_id=job-props, got %q", val)
	}
}

func TestHarness_AllocateNoGolden(t *testing.T) {
	h := zfstest.NewHarness(t)
	ctx := context.Background()

	// Don't seed golden — no @ready snapshot.
	_, err := h.Allocate(ctx, "no-golden")
	if err == nil {
		t.Fatal("expected error when golden snapshot missing")
	}
}

func TestHarness_ListClones(t *testing.T) {
	h := zfstest.NewHarness(t)
	zfstest.SeedGolden(t, h)
	ctx := context.Background()

	// Allocate two clones.
	for _, id := range []string{"job-a", "job-b"} {
		c, err := h.Allocate(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Release()
	}

	clones, err := h.ListClones(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(clones) != 2 {
		t.Fatalf("expected 2 clones, got %d", len(clones))
	}
}

func TestHarness_GoldenDatasetInfo(t *testing.T) {
	h := zfstest.NewHarness(t)
	ctx := context.Background()

	ds, err := h.GoldenDatasetInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	expected := h.PoolName() + "/golden"
	if ds.Name != expected {
		t.Fatalf("expected %s, got %s", expected, ds.Name)
	}
	if ds.Type != "filesystem" {
		t.Fatalf("expected filesystem, got %s", ds.Type)
	}
}
