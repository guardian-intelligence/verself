package zfsharness

import (
	"context"
	"fmt"
	"time"
)

// Clone represents an allocated ZFS clone for a single CI job.
//
// Lifecycle: Allocate → (job runs) → MarkDone → CollectMetrics → Release
//
// Follows Velo's LIFO cleanup pattern: if any step fails, Release
// unwinds all completed steps. OBuilder's @done snapshot pattern
// provides crash recovery: clones without @done on restart are orphans.
type Clone struct {
	harness *Harness
	jobID   string // unique ID (job_id or UUID)
	dataset string // full path, e.g. "benchpool/ci/abc123"

	// Metrics collected during lifecycle.
	AllocDuration time.Duration // time to create the clone
	WrittenBytes  uint64        // data written during job

	// LIFO cleanup stack (Velo's Rollback pattern).
	cleanups []func() error
}

// Dataset returns the full ZFS dataset path (e.g. "benchpool/ci/abc123").
func (c *Clone) Dataset() string { return c.dataset }

// Mountpoint returns the filesystem mount path (e.g. "/benchpool/ci/abc123").
func (c *Clone) Mountpoint() string { return "/" + c.dataset }

// MarkDone creates the @done snapshot, signaling successful completion.
// Used for crash recovery: clones without @done on restart are orphans.
func (c *Clone) MarkDone(ctx context.Context) error {
	return c.harness.exec.snapshot(ctx, c.dataset+"@done")
}

// CollectMetrics reads ZFS accounting data after the job completes.
func (c *Clone) CollectMetrics(ctx context.Context) error {
	written, err := c.harness.exec.written(ctx, c.dataset)
	if err != nil {
		return fmt.Errorf("read written bytes: %w", err)
	}
	c.WrittenBytes = written
	return nil
}

// Release runs all cleanup functions in LIFO order, destroying the clone.
// Safe to call multiple times (idempotent after first call).
func (c *Clone) Release() error {
	var firstErr error
	for i := len(c.cleanups) - 1; i >= 0; i-- {
		if err := c.cleanups[i](); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	c.cleanups = nil
	return firstErr
}

func (c *Clone) pushCleanup(fn func() error) {
	c.cleanups = append(c.cleanups, fn)
}
