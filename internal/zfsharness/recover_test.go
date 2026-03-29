//go:build integration

package zfsharness_test

import (
	"context"
	"testing"

	"github.com/forge-metal/forge-metal/internal/zfsharness/zfstest"
)

func TestRecoverOrphans(t *testing.T) {
	h := zfstest.NewHarness(t)
	zfstest.SeedGolden(t, h)
	ctx := context.Background()

	// Create a "completed" clone (has @done).
	completed, err := h.Allocate(ctx, "completed-job")
	if err != nil {
		t.Fatal(err)
	}
	if err := completed.MarkDone(ctx); err != nil {
		t.Fatal(err)
	}

	// Create an "orphaned" clone (no @done — simulates crash).
	orphaned, err := h.Allocate(ctx, "orphaned-job")
	if err != nil {
		t.Fatal(err)
	}
	// Intentionally do NOT call MarkDone — this simulates a crash.

	// Run recovery.
	destroyed, err := h.RecoverOrphans(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Orphaned clone should be destroyed.
	if len(destroyed) != 1 {
		t.Fatalf("expected 1 orphan destroyed, got %d: %v", len(destroyed), destroyed)
	}
	if destroyed[0] != orphaned.Dataset() {
		t.Fatalf("expected %s destroyed, got %s", orphaned.Dataset(), destroyed[0])
	}

	// Completed clone should still be listed.
	clones, err := h.ListClones(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range clones {
		if c.Dataset == completed.Dataset() {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("completed clone should survive recovery")
	}

	// Clean up the completed clone.
	completed.Release()
}
