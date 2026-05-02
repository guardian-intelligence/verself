package zfs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

// SeedStrategy names how a seed run materializes bytes onto the staging zvol.
// The daemon owns the privileged shell-out for each strategy; callers only
// pass typed enums.
type SeedStrategy string

const (
	SeedStrategyDDFromFile SeedStrategy = "dd_from_file"
	SeedStrategyMkfsExt4   SeedStrategy = "mkfs_ext4"
)

// Validate reports whether s is a recognized strategy.
func (s SeedStrategy) Validate() error {
	switch s {
	case SeedStrategyDDFromFile, SeedStrategyMkfsExt4:
		return nil
	}
	return fmt.Errorf("invalid seed strategy %q", s)
}

// SeedOutcome describes whether the seed run materially changed on-disk
// state. UpToDate runs are observable in traces but emit no destructive ops.
type SeedOutcome string

const (
	SeedOutcomeRefreshed SeedOutcome = "refreshed"
	SeedOutcomeUpToDate  SeedOutcome = "up_to_date"
)

// SeedSpec is the privileged input to VolumeLifecycle.Seed. All fields are
// host-level facts; the typed Image carries the dataset/snapshot identity.
type SeedSpec struct {
	Strategy SeedStrategy

	// SizeBytes is the zvol size in bytes. Required for both strategies.
	SizeBytes uint64

	// VolBlockSize is passed to `zfs create -o volblocksize=`. Defaults to
	// "16K" when empty.
	VolBlockSize string

	// SourcePath is the host-local path to the artifact whose bytes get
	// dd-ed onto the staging zvol. Required when Strategy=DDFromFile.
	SourcePath string

	// FilesystemLabel is passed to mkfs -L. Required when Strategy=MkfsExt4.
	FilesystemLabel string

	// AllowDestroyingActiveClones lets Seed destroy any clones in the
	// workload subtree before tearing down the old image dataset. Used at
	// deploy time when workload datasets are guaranteed orphaned by
	// markUnownedActiveLeasesCrashed; must be false in any context where
	// live leases may still hold valid clones.
	AllowDestroyingActiveClones bool
}

// SeedResult describes a successful seed run. SourceDigest is the stable
// identifier (sha256:<hex> or mkfs-ext4:size=...:label=...) recorded as the
// vs:source_digest user property on the @ready snapshot; subsequent Seed
// runs short-circuit when their computed digest matches the recorded one.
type SeedResult struct {
	Image           Image
	Outcome         SeedOutcome
	Snapshot        Snapshot
	SourceDigest    string
	SeededBytes     uint64
	DependentsTorn  int
	StagingDestroys int
	SeededAt        time.Time
}

const (
	stagingSuffix    = "-staging"
	defaultVolBlock  = "16K"
	deviceWaitWindow = 10 * time.Second
)

// Seed materializes Image at pool/images/<ref>@ready according to spec. It is
// idempotent: a re-run with the same SourceDigest emits SeedOutcomeUpToDate
// and zero destructive ops. On refresh it stages a fresh zvol, writes bytes,
// snapshots, then rename-promotes the staging dataset over the previous one
// (after recursively destroying the previous image and any dependent clones
// when AllowDestroyingActiveClones is set).
func (vl *VolumeLifecycle) Seed(ctx context.Context, image Image, spec SeedSpec) (result SeedResult, retErr error) {
	if err := spec.Strategy.Validate(); err != nil {
		return SeedResult{}, err
	}
	if spec.SizeBytes == 0 {
		return SeedResult{}, fmt.Errorf("seed size_bytes is required")
	}
	volBlock := strings.TrimSpace(spec.VolBlockSize)
	if volBlock == "" {
		volBlock = defaultVolBlock
	}
	switch spec.Strategy {
	case SeedStrategyDDFromFile:
		if strings.TrimSpace(spec.SourcePath) == "" {
			return SeedResult{}, fmt.Errorf("seed source_path is required for dd_from_file")
		}
	case SeedStrategyMkfsExt4:
		if strings.TrimSpace(spec.FilesystemLabel) == "" {
			return SeedResult{}, fmt.Errorf("seed filesystem_label is required for mkfs_ext4")
		}
	}

	digest, err := computeSourceDigest(spec)
	if err != nil {
		return SeedResult{}, fmt.Errorf("compute source digest: %w", err)
	}

	targetDataset := image.Dataset()
	stagingDataset := targetDataset + stagingSuffix
	readySnap, err := NewSnapshot(targetDataset, "ready")
	if err != nil {
		return SeedResult{}, fmt.Errorf("ready snapshot ref: %w", err)
	}

	// Idempotency: if the @ready snapshot already exists with a matching
	// vs:source_digest property, skip the refresh entirely.
	checkCtx, endCheck := startSpan(ctx, "vmorchestrator.zfs.seed_idempotency_check",
		attribute.String("image.ref", image.SourceRef()),
		attribute.String("zfs.snapshot", readySnap.String()),
	)
	exists, existsErr := vl.ops.ZFSSnapshotExists(checkCtx, readySnap.String())
	endCheck(existsErr)
	if existsErr != nil {
		return SeedResult{}, fmt.Errorf("check %s: %w", readySnap, existsErr)
	}
	if exists {
		readCtx, endRead := startSpan(ctx, "vmorchestrator.zfs.seed_digest_read",
			attribute.String("image.ref", image.SourceRef()),
			attribute.String("zfs.snapshot", readySnap.String()),
		)
		recorded, readErr := vl.ops.ZFSGetProperty(readCtx, readySnap.String(), PropSourceDigest)
		endRead(readErr)
		if readErr == nil && recorded == digest {
			return SeedResult{
				Image:        image,
				Outcome:      SeedOutcomeUpToDate,
				Snapshot:     readySnap,
				SourceDigest: digest,
				SeededAt:     time.Now().UTC(),
			}, nil
		}
		// digest mismatch or read error → fall through to refresh
	}

	if ensureErr := vl.ops.ZFSEnsureFilesystem(ctx, ImageDatasetRoot(vl.roots)); ensureErr != nil {
		return SeedResult{}, fmt.Errorf("ensure image dataset root: %w", ensureErr)
	}

	// Stage the new image first; only after the staging snapshot is good
	// do we destroy dependents and the old target dataset. A crash before
	// the staging snapshot leaves the previous (working) image untouched.
	stagingDestroys, err := vl.cleanupStaging(ctx, stagingDataset)
	if err != nil {
		return SeedResult{}, err
	}
	volsizeBytes, volsizeErr := int64FromUint64(spec.SizeBytes, "seed size_bytes")
	if volsizeErr != nil {
		return SeedResult{}, volsizeErr
	}

	createCtx, endCreate := startSpan(ctx, "vmorchestrator.zfs.seed_staging_create",
		attribute.String("image.ref", image.SourceRef()),
		attribute.String("zfs.dataset", stagingDataset),
		attribute.Int64("zfs.volsize_bytes", volsizeBytes),
		attribute.String("zfs.volblocksize", volBlock),
	)
	createErr := vl.ops.ZFSCreateVolume(createCtx, stagingDataset, spec.SizeBytes, volBlock)
	endCreate(createErr)
	if createErr != nil {
		return SeedResult{}, fmt.Errorf("create staging zvol %s: %w", stagingDataset, createErr)
	}
	defer func() {
		if retErr == nil {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), Timeout)
		defer cancel()
		if destroyErr := vl.ops.ZFSDestroyRecursive(cleanupCtx, stagingDataset); destroyErr != nil {
			vl.logger.WarnContext(context.Background(), "seed staging cleanup failed",
				"image_ref", image.SourceRef(),
				"staging_dataset", stagingDataset,
				"error", destroyErr,
			)
		}
	}()

	devicePath := DevicePath(stagingDataset)
	waitCtx, endWait := startSpan(ctx, "vmorchestrator.zfs.seed_staging_devnode_wait",
		attribute.String("device.path", devicePath),
	)
	waitErr := waitForDevice(waitCtx, devicePath)
	endWait(waitErr)
	if waitErr != nil {
		return SeedResult{}, fmt.Errorf("wait for staging device %s: %w", devicePath, waitErr)
	}

	var seededBytes uint64
	switch spec.Strategy {
	case SeedStrategyDDFromFile:
		ddCtx, endDD := startSpan(ctx, "vmorchestrator.zfs.seed_dd",
			attribute.String("image.ref", image.SourceRef()),
			attribute.String("device.path", devicePath),
			attribute.String("source.path", spec.SourcePath),
		)
		bytes, ddErr := vl.ops.ZFSWriteVolumeFromFile(ddCtx, devicePath, spec.SourcePath)
		endDD(ddErr)
		if ddErr != nil {
			return SeedResult{}, fmt.Errorf("dd %s -> %s: %w", spec.SourcePath, devicePath, ddErr)
		}
		seededBytes = bytes
	case SeedStrategyMkfsExt4:
		mkfsCtx, endMkfs := startSpan(ctx, "vmorchestrator.zfs.seed_mkfs",
			attribute.String("image.ref", image.SourceRef()),
			attribute.String("device.path", devicePath),
			attribute.String("fs.type", "ext4"),
			attribute.String("fs.label", spec.FilesystemLabel),
		)
		mkfsErr := vl.ops.ZFSMkfs(mkfsCtx, devicePath, "ext4", spec.FilesystemLabel)
		endMkfs(mkfsErr)
		if mkfsErr != nil {
			return SeedResult{}, fmt.Errorf("mkfs %s: %w", devicePath, mkfsErr)
		}
		seededBytes = spec.SizeBytes
	}

	stagingReady, err := NewSnapshot(stagingDataset, "ready")
	if err != nil {
		return SeedResult{}, fmt.Errorf("staging ready snapshot ref: %w", err)
	}
	seededAt := time.Now().UTC()
	snapshotProps := map[string]string{
		PropImageRef:       image.SourceRef(),
		PropSourceDigest:   digest,
		PropSeededAt:       seededAt.Format(time.RFC3339Nano),
		PropSeedStrategy:   string(spec.Strategy),
		PropSeedSizeBytes:  fmt.Sprintf("%d", spec.SizeBytes),
		PropSeedSeededFrom: spec.SourcePath,
	}
	snapCtx, endSnap := startSpan(ctx, "vmorchestrator.zfs.seed_staging_snapshot",
		attribute.String("image.ref", image.SourceRef()),
		attribute.String("zfs.dataset", stagingDataset),
		attribute.String("zfs.snapshot", stagingReady.String()),
	)
	snapErr := vl.ops.ZFSSnapshot(snapCtx, stagingDataset, "ready", snapshotProps)
	endSnap(snapErr)
	if snapErr != nil {
		return SeedResult{}, fmt.Errorf("snapshot staging %s: %w", stagingReady, snapErr)
	}

	// Staging is proven good — now tear down dependents of the previous
	// image (when authorized) and destroy the previous target so the
	// rename can claim the path.
	dependents := 0
	if spec.AllowDestroyingActiveClones {
		torn, tearErr := vl.tearDownDependentClones(ctx, image)
		if tearErr != nil {
			return SeedResult{}, tearErr
		}
		dependents = torn
	}
	if existsErr := destroyIfExists(ctx, vl.ops, targetDataset, true); existsErr != nil {
		return SeedResult{}, fmt.Errorf("destroy old image dataset %s: %w", targetDataset, existsErr)
	}

	renameCtx, endRename := startSpan(ctx, "vmorchestrator.zfs.seed_promote_rename",
		attribute.String("image.ref", image.SourceRef()),
		attribute.String("zfs.from", stagingDataset),
		attribute.String("zfs.to", targetDataset),
	)
	renameErr := vl.ops.ZFSRename(renameCtx, stagingDataset, targetDataset)
	endRename(renameErr)
	if renameErr != nil {
		return SeedResult{}, fmt.Errorf("promote staging %s -> %s: %w", stagingDataset, targetDataset, renameErr)
	}

	return SeedResult{
		Image:           image,
		Outcome:         SeedOutcomeRefreshed,
		Snapshot:        readySnap,
		SourceDigest:    digest,
		SeededBytes:     seededBytes,
		DependentsTorn:  dependents,
		StagingDestroys: stagingDestroys,
		SeededAt:        seededAt,
	}, nil
}

// tearDownDependentClones unmounts any VFS mounts on /dev/zvol/<pool>/* and
// recursively destroys every direct child of the workload dataset. This
// matches the lifecycle assumption that markUnownedActiveLeasesCrashed has
// already invalidated every workload dataset on disk before seeding runs.
func (vl *VolumeLifecycle) tearDownDependentClones(ctx context.Context, image Image) (int, error) {
	workloadRoot := strings.TrimSuffix(WorkloadPrefix(vl.roots), "/")
	exists, err := vl.ops.ZFSDatasetExists(ctx, workloadRoot)
	if err != nil {
		return 0, fmt.Errorf("probe workload dataset: %w", err)
	}
	if !exists {
		return 0, nil
	}
	unmountCtx, endUnmount := startSpan(ctx, "vmorchestrator.zfs.seed_unmount_stale_zvols",
		attribute.String("image.ref", image.SourceRef()),
		attribute.String("pool", vl.roots.Pool),
	)
	unmounted, unmountErr := vl.ops.UnmountStaleZvolMounts(unmountCtx, vl.roots.Pool)
	endUnmount(unmountErr)
	if unmountErr != nil {
		return 0, fmt.Errorf("unmount stale zvol mounts: %w", unmountErr)
	}
	listCtx, endList := startSpan(ctx, "vmorchestrator.zfs.seed_list_workload_clones",
		attribute.String("zfs.dataset", workloadRoot),
	)
	clones, listErr := vl.ops.ZFSListChildren(listCtx, workloadRoot)
	endList(listErr)
	if listErr != nil {
		return unmounted, fmt.Errorf("list workload clones: %w", listErr)
	}
	pruneCtx, endPrune := startSpan(ctx, "vmorchestrator.zfs.seed_dependents_prune",
		attribute.String("image.ref", image.SourceRef()),
		attribute.Int("dependents.count", len(clones)),
	)
	for _, dep := range clones {
		if err := vl.ops.ZFSDestroyRecursive(pruneCtx, dep); err != nil {
			endPrune(err)
			return unmounted, fmt.Errorf("destroy clone %s: %w", dep, err)
		}
	}
	endPrune(nil)
	return len(clones), nil
}

// cleanupStaging removes any leftover *-staging dataset from a previous
// failed seed run and reports how many entries were torn down (0 or 1) so
// the SeedResult can carry the count for trace introspection.
func (vl *VolumeLifecycle) cleanupStaging(ctx context.Context, stagingDataset string) (int, error) {
	exists, err := vl.ops.ZFSDatasetExists(ctx, stagingDataset)
	if err != nil {
		return 0, fmt.Errorf("probe staging dataset: %w", err)
	}
	if !exists {
		return 0, nil
	}
	cleanupCtx, end := startSpan(ctx, "vmorchestrator.zfs.seed_staging_destroy",
		attribute.String("zfs.dataset", stagingDataset),
	)
	destroyErr := vl.ops.ZFSDestroyRecursive(cleanupCtx, stagingDataset)
	end(destroyErr)
	if destroyErr != nil {
		return 0, fmt.Errorf("destroy leftover staging %s: %w", stagingDataset, destroyErr)
	}
	return 1, nil
}

// destroyIfExists destroys dataset only if it exists. The recursive flag maps
// to `zfs destroy -R`; it is required when destroying an image dataset that
// previously had clones (those clones must be removed first by the caller).
func destroyIfExists(ctx context.Context, ops PrivZFS, dataset string, recursive bool) error {
	exists, err := ops.ZFSDatasetExists(ctx, dataset)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if recursive {
		return ops.ZFSDestroyRecursive(ctx, dataset)
	}
	return ops.ZFSDestroy(ctx, dataset)
}

// computeSourceDigest produces the stable identifier we record in
// vs:source_digest. For dd_from_file the digest is sha256:<hex> of the source
// artifact bytes. For mkfs_ext4 there is no source file, so the digest is a
// derived string of the spec parameters that change the resulting filesystem
// shape.
func computeSourceDigest(spec SeedSpec) (string, error) {
	switch spec.Strategy {
	case SeedStrategyDDFromFile:
		f, err := os.Open(spec.SourcePath)
		if err != nil {
			return "", err
		}
		defer func() { _ = f.Close() }()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return "", err
		}
		return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
	case SeedStrategyMkfsExt4:
		return fmt.Sprintf("mkfs-ext4:size=%d:label=%s", spec.SizeBytes, spec.FilesystemLabel), nil
	}
	return "", fmt.Errorf("unhandled strategy %q", spec.Strategy)
}

// waitForDevice polls for a /dev/zvol/* node to appear after `zfs create -V`.
// udev creates the symlink asynchronously, so the seed flow needs the same
// poll the lease boot path uses; bounded by deviceWaitWindow so a stuck pool
// surfaces fast rather than wedging the seed.
func waitForDevice(ctx context.Context, path string) error {
	deadline, cancel := context.WithTimeout(ctx, deviceWaitWindow)
	defer cancel()
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat %s: %w", path, err)
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("device %s did not appear: %w", path, deadline.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// DevicePath returns the udev-managed symlink for a zvol dataset.
func DevicePath(dataset string) string {
	return "/dev/zvol/" + dataset
}
