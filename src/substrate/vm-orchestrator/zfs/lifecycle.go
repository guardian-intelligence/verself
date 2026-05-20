package zfs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

const (
	Timeout                   = 30 * time.Second
	DurableVolumeNominalBytes = uint64(1 << 40)
	sourceDigestProperty      = "vs:source_digest"
)

type StorageNamespace struct {
	OrgID      string
	QuotaBytes uint64
}

type PrivZFS interface {
	ZFSClone(ctx context.Context, snapshot, target, operationID string) error
	ZFSSnapshot(ctx context.Context, dataset, snapshotName string, properties map[string]string) error
	ZFSDestroy(ctx context.Context, dataset string) error
	ZFSDestroyRecursive(ctx context.Context, dataset string) error
	ZFSCreateEncryptedFilesystem(ctx context.Context, dataset string, rawKey []byte) error
	ZFSEnsureFilesystem(ctx context.Context, dataset string) error
	ZFSLoadKey(ctx context.Context, dataset string, rawKey []byte) error
	ZFSUnloadKey(ctx context.Context, dataset string) error
	ZFSSnapshotExists(ctx context.Context, snapshot string) (bool, error)
	ZFSDatasetExists(ctx context.Context, dataset string) (bool, error)
	ZFSSetProperty(ctx context.Context, dataset, key, value string) error
	ZFSGetProperty(ctx context.Context, target, key string) (string, error)
	ZFSCreateVolume(ctx context.Context, dataset string, sizeBytes uint64, volblocksize string) error
	ZFSCreateSparseVolume(ctx context.Context, dataset string, sizeBytes uint64, volblocksize string) error
	ZFSReceiveSnapshot(ctx context.Context, sourceSnapshot, targetDataset string) error
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

var orgImageMaterializationLocks sync.Map

func NewVolumeLifecycle(roots Roots, ops PrivZFS, logger *slog.Logger) *VolumeLifecycle {
	if logger == nil {
		logger = slog.Default()
	}
	return &VolumeLifecycle{roots: roots.normalized(), ops: ops, logger: logger}
}

func (l *VolumeLifecycle) EnsureRoots(ctx context.Context) error {
	for _, dataset := range []string{l.roots.imageRoot(), l.roots.orgsRoot()} {
		if err := l.ops.ZFSEnsureFilesystem(ctx, dataset); err != nil {
			return err
		}
	}
	return nil
}

func (l *VolumeLifecycle) EnsureEncryptedStorageNamespace(ctx context.Context, namespace StorageNamespace, rawKey []byte) error {
	namespace.OrgID = strings.TrimSpace(namespace.OrgID)
	if !IsValidRef(namespace.OrgID) {
		return fmt.Errorf("storage namespace org id is invalid: %s", namespace.OrgID)
	}
	if namespace.QuotaBytes == 0 {
		return fmt.Errorf("storage namespace quota is required")
	}
	if len(rawKey) != 32 {
		return fmt.Errorf("storage namespace raw key must be 32 bytes")
	}
	orgRoot := l.roots.orgRoot(namespace.OrgID)
	exists, err := l.ops.ZFSDatasetExists(ctx, orgRoot)
	if err != nil {
		return err
	}
	if exists {
		encryption, err := l.ops.ZFSGetProperty(ctx, orgRoot, "encryption")
		if err != nil {
			return fmt.Errorf("read storage namespace encryption %s: %w", orgRoot, err)
		}
		if encryption == "" || encryption == "off" {
			l.logger.WarnContext(ctx, "unencrypted storage namespace replaced", "org_id", namespace.OrgID, "dataset", orgRoot)
			if err := l.ops.ZFSDestroyRecursive(ctx, orgRoot); err != nil {
				return fmt.Errorf("destroy unencrypted storage namespace %s: %w", orgRoot, err)
			}
			exists = false
		}
	}
	if !exists {
		if err := l.ops.ZFSCreateEncryptedFilesystem(ctx, orgRoot, rawKey); err != nil {
			return fmt.Errorf("create encrypted storage namespace %s: %w", orgRoot, err)
		}
	}
	if err := l.ops.ZFSLoadKey(ctx, orgRoot, rawKey); err != nil {
		return fmt.Errorf("load storage namespace key %s: %w", orgRoot, err)
	}
	if err := l.ensureStorageNamespace(ctx, namespace); err != nil {
		return err
	}
	for _, dataset := range []string{orgRoot, l.roots.workloadRoot(namespace.OrgID), l.roots.goldenRoot(namespace.OrgID), l.roots.orgImageRoot(namespace.OrgID)} {
		if err := l.AssertEncryptedDataset(ctx, dataset, orgRoot); err != nil {
			return err
		}
	}
	return nil
}

func (l *VolumeLifecycle) ensureStorageNamespace(ctx context.Context, namespace StorageNamespace) error {
	namespace.OrgID = strings.TrimSpace(namespace.OrgID)
	if !IsValidRef(namespace.OrgID) {
		return fmt.Errorf("storage namespace org id is invalid: %s", namespace.OrgID)
	}
	if namespace.QuotaBytes == 0 {
		return fmt.Errorf("storage namespace quota is required")
	}
	orgRoot := l.roots.orgRoot(namespace.OrgID)
	for _, dataset := range []string{orgRoot, l.roots.workloadRoot(namespace.OrgID), l.roots.goldenRoot(namespace.OrgID), l.roots.orgImageRoot(namespace.OrgID)} {
		if err := l.ops.ZFSEnsureFilesystem(ctx, dataset); err != nil {
			return err
		}
		if err := l.ops.ZFSSetProperty(ctx, dataset, "mountpoint", "none"); err != nil {
			return fmt.Errorf("set storage namespace mountpoint on %s: %w", dataset, err)
		}
		if err := l.ops.ZFSSetProperty(ctx, dataset, "canmount", "off"); err != nil {
			return fmt.Errorf("set storage namespace canmount on %s: %w", dataset, err)
		}
	}
	// quota, not refquota: the org boundary must include child zvols.
	if err := l.ops.ZFSSetProperty(ctx, orgRoot, "quota", fmt.Sprintf("%d", namespace.QuotaBytes)); err != nil {
		return fmt.Errorf("set storage namespace quota: %w", err)
	}
	return nil
}

func (l *VolumeLifecycle) UnloadStorageNamespaceKey(ctx context.Context, orgID string) error {
	orgID = strings.TrimSpace(orgID)
	if !IsValidRef(orgID) {
		return fmt.Errorf("storage namespace org id is invalid: %s", orgID)
	}
	return l.ops.ZFSUnloadKey(ctx, l.roots.orgRoot(orgID))
}

func (l *VolumeLifecycle) AssertEncryptedDataset(ctx context.Context, dataset, expectedRoot string) error {
	if strings.TrimSpace(dataset) == "" || strings.Contains(dataset, "@") {
		return fmt.Errorf("encrypted dataset assertion target is invalid: %s", dataset)
	}
	encryption, err := l.ops.ZFSGetProperty(ctx, dataset, "encryption")
	if err != nil {
		return fmt.Errorf("read zfs encryption for %s: %w", dataset, err)
	}
	if encryption == "" || encryption == "off" {
		return fmt.Errorf("zfs dataset %s is not encrypted", dataset)
	}
	encryptionRoot, err := l.ops.ZFSGetProperty(ctx, dataset, "encryptionroot")
	if err != nil {
		return fmt.Errorf("read zfs encryptionroot for %s: %w", dataset, err)
	}
	if encryptionRoot != expectedRoot {
		return fmt.Errorf("zfs dataset %s encryptionroot = %s, want %s", dataset, encryptionRoot, expectedRoot)
	}
	keyStatus, err := l.ops.ZFSGetProperty(ctx, dataset, "keystatus")
	if err != nil {
		return fmt.Errorf("read zfs keystatus for %s: %w", dataset, err)
	}
	if keyStatus != "available" {
		return fmt.Errorf("zfs dataset %s key status = %s, want available", dataset, keyStatus)
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
	source, err := l.EnsureOrgImage(ctx, lease.OrgID(), image)
	if err != nil {
		return err
	}
	if err := l.ops.ZFSClone(ctx, source.String(), lease.RootDataset(), lease.ID()); err != nil {
		return err
	}
	if err := l.AssertEncryptedDataset(ctx, lease.RootDataset(), l.roots.orgRoot(lease.OrgID())); err != nil {
		_ = l.ops.ZFSDestroyRecursive(context.Background(), lease.RootDataset())
		return err
	}
	return nil
}

func (l *VolumeLifecycle) ResizeLeaseRootExt4(ctx context.Context, lease Lease, sizeBytes uint64) error {
	return l.ops.ZFSEnsureVolumeSizeExt4(ctx, lease.RootDataset(), sizeBytes)
}

func (l *VolumeLifecycle) DestroyLeaseRoot(ctx context.Context, lease Lease) error {
	_, _ = l.ops.UnmountStaleZvolMounts(ctx, l.roots.Pool)
	return l.ops.ZFSDestroyRecursive(ctx, lease.Dataset())
}

func (l *VolumeLifecycle) PrepareMount(ctx context.Context, lease Lease, image Image, index int, name, operationID string) (MountClone, error) {
	if err := validateSameRoots(l.roots, lease.roots, image.roots); err != nil {
		return MountClone{}, err
	}
	source, err := l.EnsureOrgImage(ctx, lease.OrgID(), image)
	if err != nil {
		return MountClone{}, err
	}
	return l.PrepareMountFromSnapshot(ctx, lease, source, index, name, operationID)
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
	if err := l.AssertEncryptedDataset(ctx, target, l.roots.orgRoot(lease.OrgID())); err != nil {
		_ = l.ops.ZFSDestroyRecursive(context.Background(), target)
		return MountClone{}, err
	}
	return MountClone{lease: lease, dataset: target, name: name}, nil
}

func (l *VolumeLifecycle) PrepareEmptyMount(ctx context.Context, lease Lease, index int, name string, operationID string) (MountClone, error) {
	target, err := lease.MountDataset(index, name)
	if err != nil {
		return MountClone{}, err
	}
	if err := l.ops.ZFSEnsureFilesystem(ctx, filepath.ToSlash(filepath.Dir(target))); err != nil {
		return MountClone{}, err
	}
	if err := l.ops.ZFSCreateSparseVolume(ctx, target, DurableVolumeNominalBytes, "16K"); err != nil {
		return MountClone{}, err
	}
	if err := l.ops.ZFSSetProperty(ctx, target, "vs:operation_id", firstNonEmpty(operationID, lease.ID())); err != nil {
		_ = l.ops.ZFSDestroyRecursive(context.Background(), target)
		return MountClone{}, err
	}
	if err := l.AssertEncryptedDataset(ctx, target, l.roots.orgRoot(lease.OrgID())); err != nil {
		_ = l.ops.ZFSDestroyRecursive(context.Background(), target)
		return MountClone{}, err
	}
	if err := l.ops.ZFSMkfs(ctx, zvolDevicePath(target), "ext4", safeExt4Label(name)); err != nil {
		_ = l.ops.ZFSDestroyRecursive(context.Background(), target)
		return MountClone{}, err
	}
	return MountClone{lease: lease, dataset: target, name: name}, nil
}

func (l *VolumeLifecycle) EnsureOrgImage(ctx context.Context, orgID string, image Image) (Snapshot, error) {
	orgID = strings.TrimSpace(orgID)
	if !IsValidRef(orgID) {
		return Snapshot{}, fmt.Errorf("org image org id is invalid: %s", orgID)
	}
	if err := validateSameRoots(l.roots, image.roots); err != nil {
		return Snapshot{}, err
	}
	source := image.Snapshot()
	if source.String() == "" {
		return Snapshot{}, fmt.Errorf("image snapshot is required")
	}
	sourceExists, err := l.ops.ZFSSnapshotExists(ctx, source.String())
	if err != nil {
		return Snapshot{}, err
	}
	if !sourceExists {
		return Snapshot{}, fmt.Errorf("image snapshot %s does not exist", source.String())
	}
	sourceDigest, err := l.ops.ZFSGetProperty(ctx, source.String(), sourceDigestProperty)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read source image digest %s: %w", source.String(), err)
	}
	if sourceDigest == "" {
		return Snapshot{}, fmt.Errorf("image snapshot %s is missing %s", source.String(), sourceDigestProperty)
	}
	orgImageName, err := orgImageDatasetName(image.Ref(), sourceDigest)
	if err != nil {
		return Snapshot{}, err
	}
	dataset := filepath.ToSlash(filepath.Join(l.roots.orgImageRoot(orgID), orgImageName))
	target := Snapshot{dataset: dataset, name: readySnapshot}
	lock := orgImageLock(orgID, image.Ref())
	lock.Lock()
	defer lock.Unlock()

	if exists, err := l.ops.ZFSSnapshotExists(ctx, target.String()); err != nil {
		return Snapshot{}, err
	} else if exists {
		targetDigest, err := l.ops.ZFSGetProperty(ctx, target.String(), sourceDigestProperty)
		if err != nil {
			return Snapshot{}, fmt.Errorf("read org image digest %s: %w", target.String(), err)
		}
		if targetDigest == sourceDigest {
			if err := l.AssertEncryptedDataset(ctx, dataset, l.roots.orgRoot(orgID)); err != nil {
				return Snapshot{}, err
			}
			return target, nil
		}
		if err := l.ops.ZFSDestroyRecursive(ctx, dataset); err != nil {
			return Snapshot{}, fmt.Errorf("destroy stale org image %s: %w", dataset, err)
		}
	} else if exists, err := l.ops.ZFSDatasetExists(ctx, dataset); err != nil {
		return Snapshot{}, err
	} else if exists {
		if err := l.ops.ZFSDestroyRecursive(ctx, dataset); err != nil {
			return Snapshot{}, fmt.Errorf("destroy partial org image %s: %w", dataset, err)
		}
	}
	if err := l.ops.ZFSEnsureFilesystem(ctx, l.roots.orgImageRoot(orgID)); err != nil {
		return Snapshot{}, err
	}
	if err := l.ops.ZFSReceiveSnapshot(ctx, source.String(), dataset); err != nil {
		_ = l.ops.ZFSDestroyRecursive(context.Background(), dataset)
		return Snapshot{}, err
	}
	if err := l.ops.ZFSSetProperty(ctx, target.String(), "vs:image_ref", image.Ref()); err != nil {
		_ = l.ops.ZFSDestroyRecursive(context.Background(), dataset)
		return Snapshot{}, err
	}
	if err := l.ops.ZFSSetProperty(ctx, target.String(), sourceDigestProperty, sourceDigest); err != nil {
		_ = l.ops.ZFSDestroyRecursive(context.Background(), dataset)
		return Snapshot{}, err
	}
	if err := l.AssertEncryptedDataset(ctx, dataset, l.roots.orgRoot(orgID)); err != nil {
		_ = l.ops.ZFSDestroyRecursive(context.Background(), dataset)
		return Snapshot{}, err
	}
	l.logger.InfoContext(ctx, "org image materialized",
		"org_id", orgID,
		"image_ref", image.Ref(),
		"source_snapshot", source.String(),
		"target_snapshot", target.String(),
	)
	return target, nil
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
	expectedEncryptionRoot := l.roots.orgRoot(volume.OrgID())
	if err := l.AssertEncryptedDataset(ctx, clone.Dataset(), expectedEncryptionRoot); err != nil {
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
	if err := l.AssertEncryptedDataset(ctx, genDataset, expectedEncryptionRoot); err != nil {
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
	currentDigest, _ := l.ops.ZFSGetProperty(ctx, image.Snapshot().String(), sourceDigestProperty)
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
	if err := l.ops.ZFSSnapshot(ctx, staging, readySnapshot, map[string]string{sourceDigestProperty: sourceDigest}); err != nil {
		return SeedResult{}, err
	}

	dependents := 0
	if exists, _ := l.ops.ZFSDatasetExists(ctx, image.Dataset()); exists {
		if !spec.AllowDestroyingActiveClones {
			return SeedResult{}, fmt.Errorf("image %s already exists; allow_destroying_active_clones is required to replace it", image.Ref())
		}
		orgRoots, _ := l.ops.ZFSListChildren(ctx, l.roots.orgsRoot())
		for _, orgRoot := range orgRoots {
			orgID := strings.TrimPrefix(orgRoot, l.roots.orgsRoot()+"/")
			children, _ := l.ops.ZFSListChildren(ctx, l.roots.workloadRoot(orgID))
			for _, child := range children {
				if strings.Contains(child, image.Ref()) {
					_ = l.ops.ZFSDestroyRecursive(ctx, child)
					dependents++
				}
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
		sourcePath := strings.TrimSpace(spec.SourcePath)
		if sourcePath == "" {
			return "", fmt.Errorf("seed source path is required")
		}
		source, err := os.Open(sourcePath)
		if err != nil {
			return "", err
		}
		defer func() { _ = source.Close() }()
		info, err := source.Stat()
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("seed source path is not a regular file: %s", sourcePath)
		}
		_, _ = fmt.Fprintf(h, "source-content\x00%d\x00", info.Size())
		if _, err := io.Copy(h, source); err != nil {
			return "", err
		}
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

func orgImageLock(orgID, imageRef string) *sync.Mutex {
	key := orgID + "\x00" + imageRef
	lock, _ := orgImageMaterializationLocks.LoadOrStore(key, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func zvolDevicePath(dataset string) string {
	return "/dev/zvol/" + strings.Trim(dataset, "/")
}

func orgImageDatasetName(imageRef, sourceDigest string) (string, error) {
	imageRef = strings.TrimSpace(imageRef)
	sourceDigest = strings.ToLower(strings.TrimSpace(sourceDigest))
	name := imageRef + "-" + sourceDigest
	if len(name) > 128 {
		sum := sha256.Sum256([]byte(imageRef + "\x00" + sourceDigest))
		digest := hex.EncodeToString(sum[:16])
		maxPrefix := 128 - 1 - len(digest)
		if len(imageRef) > maxPrefix {
			imageRef = imageRef[:maxPrefix]
		}
		name = imageRef + "-" + digest
	}
	if !IsValidRef(name) {
		return "", fmt.Errorf("org image dataset name is invalid: %s", name)
	}
	return name, nil
}
