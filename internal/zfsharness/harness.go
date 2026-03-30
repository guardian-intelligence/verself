package zfsharness

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// Config holds settings for the ZFS harness.
// Maps to the [zfs] section in forge-metal.toml.
type Config struct {
	Pool           string        // ZFS pool name, e.g. "benchpool"
	GoldenDataset  string        // dataset under pool for golden images, e.g. "golden"
	CIDataset      string        // dataset under pool for CI clones, e.g. "ci"
	CommandTimeout time.Duration // 0 means DefaultTimeout (30s)
}

// CloneInfo holds metadata about an active CI clone.
type CloneInfo struct {
	Dataset   string // full ZFS dataset path
	JobID     string // forge:job_id user property
	CreatedAt string // forge:created_at user property
	Written   uint64 // bytes written since clone
	HasDone   bool   // whether @done snapshot exists
}

// Harness manages ZFS-backed VM allocation from golden images.
type Harness struct {
	pool    string
	golden  string
	ci      string
	timeout time.Duration
	exec    *executor
}

// New creates a Harness from configuration.
func New(cfg Config) *Harness {
	timeout := cfg.CommandTimeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	return &Harness{
		pool:    cfg.Pool,
		golden:  cfg.GoldenDataset,
		ci:      cfg.CIDataset,
		timeout: timeout,
		exec:    &executor{timeout: timeout},
	}
}

// PoolName returns the ZFS pool name.
func (h *Harness) PoolName() string { return h.pool }

// GoldenSnapshot returns the full path to the golden @ready snapshot,
// e.g. "benchpool/golden@ready".
func (h *Harness) GoldenSnapshot() string {
	return fmt.Sprintf("%s/%s@ready", h.pool, h.golden)
}

// GoldenMountpoint returns the mount path of the golden dataset.
func (h *Harness) GoldenMountpoint() string {
	return "/" + h.pool + "/" + h.golden
}

// ciDatasetPath returns the full CI dataset path, e.g. "benchpool/ci".
func (h *Harness) ciDatasetPath() string {
	return fmt.Sprintf("%s/%s", h.pool, h.ci)
}

// clonePath returns the full dataset path for a CI clone.
func (h *Harness) clonePath(jobID string) string {
	return fmt.Sprintf("%s/%s/%s", h.pool, h.ci, jobID)
}

// GoldenReady checks whether the golden @ready snapshot exists.
func (h *Harness) GoldenReady(ctx context.Context) (bool, error) {
	return h.exec.exists(ctx, h.GoldenSnapshot())
}

// Allocate creates a new Clone for the given job ID from the golden image.
// This is the hot path — ZFS clone is O(1), ~5.7ms total.
func (h *Harness) Allocate(ctx context.Context, jobID string) (*Clone, error) {
	golden := h.GoldenSnapshot()

	exists, err := h.exec.exists(ctx, golden)
	if err != nil {
		return nil, fmt.Errorf("check golden snapshot: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("golden snapshot %s does not exist — run golden image build first", golden)
	}

	dataset := h.clonePath(jobID)

	start := time.Now()
	props := map[string]string{
		"forge:job_id":     jobID,
		"forge:created_at": time.Now().UTC().Format(time.RFC3339),
	}
	if err := h.exec.cloneWithProps(ctx, golden, dataset, props); err != nil {
		return nil, fmt.Errorf("clone %s → %s: %w", golden, dataset, err)
	}

	c := &Clone{
		harness:       h,
		jobID:         jobID,
		dataset:       dataset,
		AllocDuration: time.Since(start),
	}

	// Register cleanup (LIFO — this runs last).
	c.pushCleanup(func() error {
		return h.exec.destroy(context.Background(), dataset, true)
	})

	return c, nil
}

// ListClones returns metadata about all active CI clones.
func (h *Harness) ListClones(ctx context.Context) ([]CloneInfo, error) {
	children, err := h.exec.listChildren(ctx, h.ciDatasetPath())
	if err != nil {
		return nil, fmt.Errorf("list CI clones: %w", err)
	}

	ciDS := h.ciDatasetPath()
	var clones []CloneInfo
	for _, child := range children {
		if child.Name == ciDS {
			continue
		}

		jobID, _ := h.exec.getProperty(ctx, child.Name, "forge:job_id")
		createdAt, _ := h.exec.getProperty(ctx, child.Name, "forge:created_at")
		written, _ := h.exec.written(ctx, child.Name)
		hasDone, _ := h.exec.exists(ctx, child.Name+"@done")

		clones = append(clones, CloneInfo{
			Dataset:   child.Name,
			JobID:     jobID,
			CreatedAt: createdAt,
			Written:   written,
			HasDone:   hasDone,
		})
	}
	return clones, nil
}

// GoldenDatasetInfo returns metadata about the golden dataset.
func (h *Harness) GoldenDatasetInfo(ctx context.Context) (*DatasetInfo, error) {
	return h.exec.getDataset(ctx, fmt.Sprintf("%s/%s", h.pool, h.golden))
}

// EnsureGoldenDataset creates the golden dataset as a ZFS filesystem if it
// doesn't already exist. Idempotent (uses zfs create -p).
func (h *Harness) EnsureGoldenDataset(ctx context.Context) error {
	ds := fmt.Sprintf("%s/%s", h.pool, h.golden)
	return h.exec.createFilesystem(ctx, ds)
}

// SnapshotGoldenReady creates (or replaces) the golden @ready snapshot.
// Any existing @ready snapshot is destroyed first.
func (h *Harness) SnapshotGoldenReady(ctx context.Context) error {
	snap := h.GoldenSnapshot()
	exists, err := h.exec.exists(ctx, snap)
	if err != nil {
		return fmt.Errorf("check existing snapshot: %w", err)
	}
	if exists {
		if err := h.exec.destroy(ctx, snap, false); err != nil {
			return fmt.Errorf("destroy old snapshot: %w", err)
		}
	}
	return h.exec.snapshot(ctx, snap)
}

// GoldenAge returns the age of the golden @ready snapshot.
// Uses the ZFS 'creation' property which is a Unix epoch timestamp with -p flag.
func (h *Harness) GoldenAge(ctx context.Context) (time.Duration, error) {
	snap := h.GoldenSnapshot()
	val, err := h.exec.getProperty(ctx, snap, "creation")
	if err != nil {
		return 0, fmt.Errorf("get creation time: %w", err)
	}
	epoch, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse creation epoch %q: %w", val, err)
	}
	return time.Since(time.Unix(epoch, 0)), nil
}
