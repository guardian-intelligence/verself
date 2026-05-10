package zfs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

const Timeout = 30 * time.Second

type PrivZFS interface {
	ZFSClone(ctx context.Context, snapshot, target, operationID string) error
	ZFSSnapshot(ctx context.Context, dataset, snapshotName string, properties map[string]string) error
	ZFSDestroy(ctx context.Context, dataset string) error
	ZFSDestroyRecursive(ctx context.Context, dataset string) error
	ZFSEnsureFilesystem(ctx context.Context, dataset string) error
	ZFSSnapshotExists(ctx context.Context, snapshot string) (bool, error)
	ZFSDatasetExists(ctx context.Context, dataset string) (bool, error)
	ZFSSetProperty(ctx context.Context, dataset, key, value string) error
	ZFSGetProperty(ctx context.Context, target, key string) (string, error)
	ZFSCreateVolume(ctx context.Context, dataset string, sizeBytes uint64, volblocksize string) error
	ZFSWriteVolumeFromFile(ctx context.Context, devicePath, sourcePath string) (uint64, error)
	ZFSMkfs(ctx context.Context, devicePath, fsType, label string) error
	ZFSEnsureVolumeSizeExt4(ctx context.Context, dataset string, sizeBytes uint64) error
	ZFSRename(ctx context.Context, from, to string) error
	ZFSPromote(ctx context.Context, dataset string) error
	ZFSListChildren(ctx context.Context, dataset string) ([]string, error)
	ZFSUsed(ctx context.Context, dataset string) (uint64, error)
	ZFSWritten(ctx context.Context, dataset string) (uint64, error)
	UnmountStaleZvolMounts(ctx context.Context, pool string) (int, error)
}

type VolumeLifecycle struct {
	roots  Roots
	ops    PrivZFS
	logger *slog.Logger
}

func NewVolumeLifecycle(roots Roots, ops PrivZFS, logger *slog.Logger) *VolumeLifecycle {
	if logger == nil {
		logger = slog.Default()
	}
	return &VolumeLifecycle{roots: roots.normalized(), ops: ops, logger: logger}
}

func (l *VolumeLifecycle) EnsureRoots(ctx context.Context) error {
	for _, dataset := range []string{l.roots.imageRoot(), l.roots.workloadRoot(), l.roots.goldenRoot()} {
		if err := l.ops.ZFSEnsureFilesystem(ctx, dataset); err != nil {
			return err
		}
	}
	return nil
}

func (l *VolumeLifecycle) PrepareSubstrateClone(ctx context.Context, lease Lease, image Image) error {
	if err := validateSameRoots(l.roots, lease.roots, image.roots); err != nil {
		return err
	}
	if err := l.ops.ZFSEnsureFilesystem(ctx, lease.Dataset()); err != nil {
		return err
	}
	return l.ops.ZFSClone(ctx, image.Snapshot().String(), lease.RootDataset(), lease.ID())
}

func (l *VolumeLifecycle) ResizeLeaseRootExt4(ctx context.Context, lease Lease, sizeBytes uint64) error {
	return l.ops.ZFSEnsureVolumeSizeExt4(ctx, lease.RootDataset(), sizeBytes)
}

func (l *VolumeLifecycle) DestroyLeaseRoot(ctx context.Context, lease Lease) error {
	_, _ = l.ops.UnmountStaleZvolMounts(ctx, l.roots.Pool)
	return l.ops.ZFSDestroyRecursive(ctx, lease.Dataset())
}

func (l *VolumeLifecycle) PrepareMount(ctx context.Context, lease Lease, image Image, index int, name, operationID string) (MountClone, error) {
	return l.PrepareMountFromSnapshot(ctx, lease, image.Snapshot(), index, name, operationID)
}

func (l *VolumeLifecycle) PrepareMountFromSnapshot(ctx context.Context, lease Lease, snapshot Snapshot, index int, name, operationID string) (MountClone, error) {
	target, err := lease.MountDataset(index, name)
	if err != nil {
		return MountClone{}, err
	}
	if snapshot.String() == "" {
		return MountClone{}, fmt.Errorf("source snapshot is required")
	}
	if err := l.ops.ZFSEnsureFilesystem(ctx, filepath.ToSlash(filepath.Dir(target))); err != nil {
		return MountClone{}, err
	}
	if err := l.ops.ZFSClone(ctx, snapshot.String(), target, firstNonEmpty(operationID, lease.ID())); err != nil {
		return MountClone{}, err
	}
	return MountClone{lease: lease, dataset: target, name: name}, nil
}

func (l *VolumeLifecycle) PrepareEmptyMount(ctx context.Context, lease Lease, index int, name string, sizeBytes uint64, operationID string) (MountClone, error) {
	target, err := lease.MountDataset(index, name)
	if err != nil {
		return MountClone{}, err
	}
	if sizeBytes == 0 {
		return MountClone{}, fmt.Errorf("empty mount size is required")
	}
	if err := l.ops.ZFSEnsureFilesystem(ctx, filepath.ToSlash(filepath.Dir(target))); err != nil {
		return MountClone{}, err
	}
	if err := l.ops.ZFSCreateVolume(ctx, target, sizeBytes, "16K"); err != nil {
		return MountClone{}, err
	}
	if err := l.ops.ZFSSetProperty(ctx, target, "vs:operation_id", firstNonEmpty(operationID, lease.ID())); err != nil {
		_ = l.ops.ZFSDestroyRecursive(context.Background(), target)
		return MountClone{}, err
	}
	if err := l.ops.ZFSMkfs(ctx, zvolDevicePath(target), "ext4", safeExt4Label(name)); err != nil {
		_ = l.ops.ZFSDestroyRecursive(context.Background(), target)
		return MountClone{}, err
	}
	return MountClone{lease: lease, dataset: target, name: name}, nil
}

func (l *VolumeLifecycle) DestroyMount(ctx context.Context, clone MountClone) error {
	_, _ = l.ops.UnmountStaleZvolMounts(ctx, l.roots.Pool)
	return l.ops.ZFSDestroyRecursive(ctx, clone.Dataset())
}

type CommitResult struct {
	NewGeneration Generation
	UsedBytes     uint64
	WrittenBytes  uint64
	CommittedAt   time.Time
}

func (l *VolumeLifecycle) Commit(ctx context.Context, clone MountClone, volume Volume, parent *Generation, generationName, operationID string) (CommitResult, error) {
	if err := validateSameRoots(l.roots, clone.lease.roots, volume.roots); err != nil {
		return CommitResult{}, err
	}
	generationName = strings.TrimSpace(generationName)
	if generationName == "" {
		generationName = "gen-" + strings.ToLower(ulid.Make().String())
	}
	if err := ValidateSnapshotName(generationName); err != nil {
		return CommitResult{}, err
	}
	if parent != nil && parent.Volume().Dataset() != volume.Dataset() {
		return CommitResult{}, fmt.Errorf("parent generation belongs to %s, not %s", parent.Volume().Dataset(), volume.Dataset())
	}
	genDataset, err := volume.GenerationDataset(generationName)
	if err != nil {
		return CommitResult{}, err
	}
	if err := l.ops.ZFSEnsureFilesystem(ctx, filepath.ToSlash(filepath.Dir(genDataset))); err != nil {
		return CommitResult{}, err
	}

	workSnapshot := "work-" + generationName
	if err := l.ops.ZFSSnapshot(ctx, clone.Dataset(), workSnapshot, map[string]string{
		"vs:operation_id": firstNonEmpty(operationID, clone.Lease().ID()),
		"vs:generation":   generationName,
	}); err != nil {
		return CommitResult{}, err
	}
	if err := l.ops.ZFSClone(ctx, clone.Dataset()+"@"+workSnapshot, genDataset, firstNonEmpty(operationID, clone.Lease().ID())); err != nil {
		return CommitResult{}, err
	}
	if err := l.ops.ZFSPromote(ctx, genDataset); err != nil {
		_ = l.ops.ZFSDestroyRecursive(context.Background(), genDataset)
		return CommitResult{}, err
	}
	if err := l.ops.ZFSSnapshot(ctx, genDataset, sealedSnapshot, map[string]string{
		"vs:operation_id": firstNonEmpty(operationID, clone.Lease().ID()),
		"vs:generation":   generationName,
	}); err != nil {
		_ = l.ops.ZFSDestroyRecursive(context.Background(), genDataset)
		return CommitResult{}, err
	}
	now := time.Now().UTC()
	snap := Snapshot{dataset: genDataset, name: sealedSnapshot}
	used, err := l.ops.ZFSUsed(ctx, genDataset)
	if err != nil {
		return CommitResult{}, err
	}
	written, err := l.ops.ZFSWritten(ctx, genDataset)
	if err != nil {
		return CommitResult{}, err
	}
	return CommitResult{
		NewGeneration: Generation{volume: volume, snap: snap},
		UsedBytes:     used,
		WrittenBytes:  written,
		CommittedAt:   now,
	}, nil
}

func (l *VolumeLifecycle) DestroyGeneration(ctx context.Context, generation Generation) error {
	return l.ops.ZFSDestroyRecursive(ctx, generation.Snapshot().Dataset())
}

func (l *VolumeLifecycle) DestroyVolume(ctx context.Context, volume Volume) error {
	return l.ops.ZFSDestroyRecursive(ctx, volume.Dataset())
}

type SeedStrategy string

const (
	SeedStrategyDDFromFile SeedStrategy = "dd_from_file"
	SeedStrategyMkfsExt4   SeedStrategy = "mkfs_ext4"
)

type SeedOutcome string

const (
	SeedOutcomeRefreshed SeedOutcome = "refreshed"
	SeedOutcomeUpToDate  SeedOutcome = "up_to_date"
)

type SeedSpec struct {
	Strategy                    SeedStrategy
	SizeBytes                   uint64
	VolBlockSize                string
	SourcePath                  string
	FilesystemLabel             string
	AllowDestroyingActiveClones bool
}

type SeedResult struct {
	Image          Image
	Snapshot       Snapshot
	Outcome        SeedOutcome
	SourceDigest   string
	SeededBytes    uint64
	DependentsTorn int
	SeededAt       time.Time
}

func (l *VolumeLifecycle) Seed(ctx context.Context, image Image, spec SeedSpec) (SeedResult, error) {
	if err := validateSameRoots(l.roots, image.roots); err != nil {
		return SeedResult{}, err
	}
	if spec.SizeBytes == 0 {
		return SeedResult{}, fmt.Errorf("seed size is required")
	}
	volBlockSize := strings.TrimSpace(spec.VolBlockSize)
	if volBlockSize == "" {
		volBlockSize = "16K"
	}
	sourceDigest, err := seedDigest(spec)
	if err != nil {
		return SeedResult{}, err
	}
	currentDigest, _ := l.ops.ZFSGetProperty(ctx, image.Snapshot().String(), "vs:source_digest")
	if currentDigest != "" && currentDigest == sourceDigest {
		return SeedResult{Image: image, Snapshot: image.Snapshot(), Outcome: SeedOutcomeUpToDate, SourceDigest: sourceDigest, SeededAt: time.Now().UTC()}, nil
	}
	staging := image.Dataset() + "-staging-" + strings.ToLower(ulid.Make().String())
	if err := l.ops.ZFSCreateVolume(ctx, staging, spec.SizeBytes, volBlockSize); err != nil {
		return SeedResult{}, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = l.ops.ZFSDestroyRecursive(context.Background(), staging)
		}
	}()
	var seededBytes uint64
	switch spec.Strategy {
	case SeedStrategyDDFromFile:
		if strings.TrimSpace(spec.SourcePath) == "" {
			return SeedResult{}, fmt.Errorf("seed source path is required")
		}
		seededBytes, err = l.ops.ZFSWriteVolumeFromFile(ctx, zvolDevicePath(staging), spec.SourcePath)
	case SeedStrategyMkfsExt4:
		err = l.ops.ZFSMkfs(ctx, zvolDevicePath(staging), "ext4", spec.FilesystemLabel)
	default:
		err = fmt.Errorf("unsupported seed strategy %q", spec.Strategy)
	}
	if err != nil {
		return SeedResult{}, err
	}
	if err := l.ops.ZFSSnapshot(ctx, staging, readySnapshot, map[string]string{"vs:source_digest": sourceDigest}); err != nil {
		return SeedResult{}, err
	}

	dependents := 0
	if exists, _ := l.ops.ZFSDatasetExists(ctx, image.Dataset()); exists {
		if !spec.AllowDestroyingActiveClones {
			return SeedResult{}, fmt.Errorf("image %s already exists; allow_destroying_active_clones is required to replace it", image.Ref())
		}
		children, _ := l.ops.ZFSListChildren(ctx, l.roots.workloadRoot())
		for _, child := range children {
			if strings.Contains(child, image.Ref()) {
				_ = l.ops.ZFSDestroyRecursive(ctx, child)
				dependents++
			}
		}
		if err := l.ops.ZFSDestroyRecursive(ctx, image.Dataset()); err != nil {
			return SeedResult{}, err
		}
	}
	if err := l.ops.ZFSRename(ctx, staging, image.Dataset()); err != nil {
		return SeedResult{}, err
	}
	cleanup = false
	return SeedResult{
		Image:          image,
		Snapshot:       image.Snapshot(),
		Outcome:        SeedOutcomeRefreshed,
		SourceDigest:   sourceDigest,
		SeededBytes:    seededBytes,
		DependentsTorn: dependents,
		SeededAt:       time.Now().UTC(),
	}, nil
}

func seedDigest(spec SeedSpec) (string, error) {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\x00%d\x00%s\x00%s\x00", spec.Strategy, spec.SizeBytes, spec.VolBlockSize, spec.FilesystemLabel)
	if spec.Strategy == SeedStrategyDDFromFile {
		info, err := os.Stat(spec.SourcePath)
		if err != nil {
			return "", err
		}
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00%d", spec.SourcePath, info.Size(), info.ModTime().UnixNano())
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func safeExt4Label(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, value)
	if len(value) > 16 {
		value = value[:16]
	}
	return value
}

func validateSameRoots(expected Roots, roots ...Roots) error {
	expected = expected.normalized()
	for _, root := range roots {
		if root.normalized() != expected {
			return fmt.Errorf("zfs roots mismatch")
		}
	}
	return nil
}

func zvolDevicePath(dataset string) string {
	return "/dev/zvol/" + strings.Trim(dataset, "/")
}
