package vmorchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/verself/vm-orchestrator/zfs"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type OrgRuntimeManager struct {
	cfg     Config
	roots   zfs.Roots
	logger  *slog.Logger
	ops     PrivOps
	volumes *zfs.VolumeLifecycle
	keys    *StorageKeyManager
	state   *hostStateStore

	mu    sync.RWMutex
	byOrg map[string]orgRuntimeSnapshot
	locks sync.Map
}

func NewOrgRuntimeManager(cfg Config, roots zfs.Roots, ops PrivOps, keys *StorageKeyManager, state *hostStateStore, logger *slog.Logger) *OrgRuntimeManager {
	if logger == nil {
		logger = slog.Default()
	}
	if ops == nil {
		ops = DirectPrivOps{}
	}
	if cfg.DefaultSubstrateRef == "" {
		cfg.DefaultSubstrateRef = DefaultConfig().DefaultSubstrateRef
	}
	return &OrgRuntimeManager{
		cfg:     cfg,
		roots:   roots,
		logger:  logger,
		ops:     ops,
		volumes: zfs.NewVolumeLifecycle(roots, ops, logger),
		keys:    keys,
		state:   state,
		byOrg:   map[string]orgRuntimeSnapshot{},
	}
}

func (m *OrgRuntimeManager) Ensure(ctx context.Context, shape OrgRuntimeShape) (status OrgRuntimeStatus, err error) {
	if m == nil {
		return OrgRuntimeStatus{}, fmt.Errorf("org runtime manager is required")
	}
	shape, err = normalizeOrgRuntimeShape(m.cfg.DefaultSubstrateRef, shape)
	if err != nil {
		return OrgRuntimeStatus{}, err
	}
	ctx, span := tracer.Start(ctx, "vmorchestrator.org_runtime.ensure",
		trace.WithAttributes(
			attribute.String("org.id", shape.StorageNamespace.OrgID),
			uint64TraceAttribute("storage.quota_bytes", shape.StorageNamespace.QuotaBytes),
			attribute.Int("zfs.image_ref_count", len(shape.ImageRefs)),
		),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	if status, ok := m.readyStatus(shape); ok {
		span.SetAttributes(attribute.Bool("org_runtime.memory_ready", true))
		return status, nil
	}
	lock := m.orgLock(shape.StorageNamespace.OrgID)
	lock.Lock()
	defer lock.Unlock()
	if status, ok := m.readyStatus(shape); ok {
		span.SetAttributes(attribute.Bool("org_runtime.memory_ready", true))
		return status, nil
	}
	span.SetAttributes(attribute.Bool("org_runtime.memory_ready", false))

	namespaceStatus, err := m.ensureStorageNamespace(ctx, shape.StorageNamespace)
	if err != nil {
		return OrgRuntimeStatus{}, err
	}
	span.SetAttributes(
		attribute.String("zfs.namespace_dataset", namespaceStatus.Dataset),
		attribute.String("zfs.keystatus", namespaceStatus.KeyStatus),
	)

	existing := m.currentSnapshot(shape.StorageNamespace.OrgID)
	images := existing.Images
	for _, imageRef := range shape.ImageRefs {
		if imageReady(shape.StorageNamespace.OrgID, imageRef, images, m.roots) {
			continue
		}
		image, imageErr := zfs.NewImage(m.roots, imageRef)
		if imageErr != nil {
			return OrgRuntimeStatus{}, imageErr
		}
		imageStatus, imageErr := m.volumes.EnsureOrgImageStatus(ctx, shape.StorageNamespace.OrgID, image)
		if imageErr != nil {
			return OrgRuntimeStatus{}, fmt.Errorf("ensure org image %s: %w", imageRef, imageErr)
		}
		images = mergeOrgRuntimeImageState(images, []orgRuntimeImageState{{
			ImageRef:       imageStatus.ImageRef,
			SourceSnapshot: imageStatus.SourceSnapshot.String(),
			SourceDigest:   imageStatus.SourceDigest,
			OrgSnapshot:    imageStatus.Snapshot.String(),
		}})
	}
	now := time.Now().UTC()
	readyAt := now
	if existing.OrgID == shape.StorageNamespace.OrgID && existing.QuotaBytes == shape.StorageNamespace.QuotaBytes {
		readyAt = firstNonZeroTime(existing.ReadyAt, now)
	}
	snapshot := orgRuntimeSnapshot{
		OrgID:      shape.StorageNamespace.OrgID,
		QuotaBytes: shape.StorageNamespace.QuotaBytes,
		Images:     images,
		ReadyAt:    readyAt,
		VerifiedAt: now,
	}
	if !snapshotSatisfiesShape(snapshot, shape, m.roots) {
		return OrgRuntimeStatus{}, fmt.Errorf("org runtime %s did not converge", shape.StorageNamespace.OrgID)
	}
	if m.state != nil {
		if err := m.state.upsertOrgRuntime(ctx, snapshot); err != nil {
			return OrgRuntimeStatus{}, err
		}
	}
	m.setSnapshot(snapshot)
	return orgRuntimeStatusFromSnapshot(snapshot), nil
}

func (m *OrgRuntimeManager) RequireReady(ctx context.Context, shape OrgRuntimeShape) (plan OrgRuntimeLeasePlan, err error) {
	if m == nil {
		return OrgRuntimeLeasePlan{}, fmt.Errorf("org runtime manager is required")
	}
	shape, err = normalizeOrgRuntimeShape(m.cfg.DefaultSubstrateRef, shape)
	if err != nil {
		return OrgRuntimeLeasePlan{}, err
	}
	_, span := tracer.Start(ctx, "vmorchestrator.org_runtime.require_ready",
		trace.WithAttributes(
			attribute.String("org.id", shape.StorageNamespace.OrgID),
			uint64TraceAttribute("storage.quota_bytes", shape.StorageNamespace.QuotaBytes),
			attribute.Int("zfs.image_ref_count", len(shape.ImageRefs)),
		),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	snapshot := m.currentSnapshot(shape.StorageNamespace.OrgID)
	if !snapshotSatisfiesShape(snapshot, shape, m.roots) {
		return OrgRuntimeLeasePlan{}, fmt.Errorf("%w: org %s", errOrgRuntimeNotReady, shape.StorageNamespace.OrgID)
	}
	plan = OrgRuntimeLeasePlan{ImageSnapshots: map[string]zfs.Snapshot{}}
	byRef := make(map[string]orgRuntimeImageState, len(snapshot.Images))
	for _, image := range snapshot.Images {
		byRef[image.ImageRef] = image
	}
	for _, imageRef := range shape.ImageRefs {
		imageState := byRef[imageRef]
		snapshotRef, parseErr := zfs.OrgImageSnapshot(m.roots, shape.StorageNamespace.OrgID, imageRef, imageState.SourceDigest)
		if parseErr != nil {
			return OrgRuntimeLeasePlan{}, parseErr
		}
		plan.ImageSnapshots[imageRef] = snapshotRef
	}
	verifiedAt := time.Now().UTC()
	span.SetAttributes(
		attribute.String("org_runtime.ready_at", snapshot.ReadyAt.Format(time.RFC3339Nano)),
		attribute.String("org_runtime.verified_at", verifiedAt.Format(time.RFC3339Nano)),
	)
	return plan, nil
}

func (m *OrgRuntimeManager) InvalidateOrg(orgID, reason string) {
	if m == nil {
		return
	}
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return
	}
	m.mu.Lock()
	delete(m.byOrg, orgID)
	m.mu.Unlock()
	m.logger.Info("org runtime invalidated", "org_id", orgID, "reason", reason)
}

func (m *OrgRuntimeManager) InvalidateImage(imageRef, reason string) {
	if m == nil {
		return
	}
	imageRef = strings.TrimSpace(imageRef)
	if imageRef == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for orgID, snapshot := range m.byOrg {
		filtered := snapshot.Images[:0]
		removed := false
		for _, image := range snapshot.Images {
			if image.ImageRef == imageRef {
				removed = true
				continue
			}
			filtered = append(filtered, image)
		}
		if removed {
			snapshot.Images = append([]orgRuntimeImageState(nil), filtered...)
			m.byOrg[orgID] = snapshot
			m.logger.Info("org runtime image invalidated", "org_id", orgID, "image_ref", imageRef, "reason", reason)
		}
	}
}

func (m *OrgRuntimeManager) ensureStorageNamespace(ctx context.Context, namespace StorageNamespace) (zfs.StorageNamespaceStatus, error) {
	if m.keys == nil {
		return zfs.StorageNamespaceStatus{}, fmt.Errorf("storage key manager is required")
	}
	keyHold, err := m.keys.Acquire(ctx, namespace.OrgID, "org-runtime")
	if err != nil {
		return zfs.StorageNamespaceStatus{}, fmt.Errorf("acquire storage key: %w", err)
	}
	defer m.releaseStorageKeyMaterial(context.Background(), namespace.OrgID, "org-runtime", keyHold)
	rawKey := keyHold.Key()
	status, ensureErr := m.volumes.EnsureStorageNamespace(ctx, zfs.StorageNamespace{
		OrgID:      namespace.OrgID,
		QuotaBytes: namespace.QuotaBytes,
	}, rawKey)
	for i := range rawKey {
		rawKey[i] = 0
	}
	if ensureErr != nil {
		return zfs.StorageNamespaceStatus{}, fmt.Errorf("ensure org storage namespace: %w", ensureErr)
	}
	return status, nil
}

func (m *OrgRuntimeManager) releaseStorageKeyMaterial(ctx context.Context, orgID, leaseID string, hold *StorageKeyHold) {
	if hold == nil {
		return
	}
	lastRef := hold.Release(ctx)
	if !lastRef {
		return
	}
	m.logger.InfoContext(ctx, "storage key material idle", "org_id", orgID, "lease_id", leaseID)
}

func (m *OrgRuntimeManager) readyStatus(shape OrgRuntimeShape) (OrgRuntimeStatus, bool) {
	snapshot := m.currentSnapshot(shape.StorageNamespace.OrgID)
	if !snapshotSatisfiesShape(snapshot, shape, m.roots) {
		return OrgRuntimeStatus{}, false
	}
	snapshot.VerifiedAt = time.Now().UTC()
	return orgRuntimeStatusFromSnapshot(snapshot), true
}

func (m *OrgRuntimeManager) currentSnapshot(orgID string) orgRuntimeSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneOrgRuntimeSnapshot(m.byOrg[orgID])
}

func (m *OrgRuntimeManager) setSnapshot(snapshot orgRuntimeSnapshot) {
	m.mu.Lock()
	m.byOrg[snapshot.OrgID] = cloneOrgRuntimeSnapshot(snapshot)
	m.mu.Unlock()
}

func (m *OrgRuntimeManager) orgLock(orgID string) *sync.Mutex {
	actual, _ := m.locks.LoadOrStore(orgID, &sync.Mutex{})
	return actual.(*sync.Mutex)
}

func snapshotSatisfiesShape(snapshot orgRuntimeSnapshot, shape OrgRuntimeShape, roots zfs.Roots) bool {
	if snapshot.OrgID != shape.StorageNamespace.OrgID {
		return false
	}
	if snapshot.QuotaBytes != shape.StorageNamespace.QuotaBytes {
		return false
	}
	if snapshot.ReadyAt.IsZero() {
		return false
	}
	for _, imageRef := range shape.ImageRefs {
		if !imageReady(shape.StorageNamespace.OrgID, imageRef, snapshot.Images, roots) {
			return false
		}
	}
	return true
}

func imageReady(orgID, imageRef string, images []orgRuntimeImageState, roots zfs.Roots) bool {
	for _, image := range images {
		if image.ImageRef != imageRef || strings.TrimSpace(image.SourceDigest) == "" {
			continue
		}
		snapshot, err := zfs.OrgImageSnapshot(roots, orgID, imageRef, image.SourceDigest)
		if err != nil {
			return false
		}
		if image.OrgSnapshot != snapshot.String() {
			return false
		}
		if strings.TrimSpace(image.SourceSnapshot) == "" {
			return false
		}
		return true
	}
	return false
}

func cloneOrgRuntimeSnapshot(snapshot orgRuntimeSnapshot) orgRuntimeSnapshot {
	snapshot.Images = append([]orgRuntimeImageState(nil), snapshot.Images...)
	return snapshot
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}
