package vmorchestrator

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/verself/vm-orchestrator/zfs"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type orgRuntimePlan struct {
	ImageSnapshots map[string]zfs.Snapshot
}

func (o *Orchestrator) WarmOrgRuntime(ctx context.Context, spec OrgRuntimeWarmSpec) (status OrgRuntimeStatus, err error) {
	spec, err = o.normalizeOrgRuntimeWarmSpec(spec)
	if err != nil {
		return OrgRuntimeStatus{}, err
	}
	ctx, span := tracer.Start(ctx, "vmorchestrator.org_runtime.warm",
		trace.WithAttributes(
			attribute.String("org.id", spec.StorageNamespace.OrgID),
			uint64TraceAttribute("storage.quota_bytes", spec.StorageNamespace.QuotaBytes),
			attribute.Int("zfs.image_ref_count", len(spec.ImageRefs)),
		),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	keyHold, err := o.keys.Acquire(ctx, spec.StorageNamespace.OrgID, "org-runtime")
	if err != nil {
		return OrgRuntimeStatus{}, fmt.Errorf("acquire storage key: %w", err)
	}
	defer o.releaseStorageKeyMaterial(context.Background(), spec.StorageNamespace.OrgID, "org-runtime", keyHold)

	rawKey := keyHold.Key()
	namespaceStatus, ensureErr := o.volumes.EnsureStorageNamespace(ctx, zfs.StorageNamespace{
		OrgID:      spec.StorageNamespace.OrgID,
		QuotaBytes: spec.StorageNamespace.QuotaBytes,
	}, rawKey)
	for i := range rawKey {
		rawKey[i] = 0
	}
	if ensureErr != nil {
		return OrgRuntimeStatus{}, fmt.Errorf("ensure org storage namespace: %w", ensureErr)
	}
	span.SetAttributes(
		attribute.String("zfs.namespace_dataset", namespaceStatus.Dataset),
		attribute.String("zfs.keystatus", namespaceStatus.KeyStatus),
	)

	images := make([]WarmedImage, 0, len(spec.ImageRefs))
	for _, imageRef := range spec.ImageRefs {
		image, imageErr := zfs.NewImage(o.roots, imageRef)
		if imageErr != nil {
			return OrgRuntimeStatus{}, imageErr
		}
		imageStatus, imageErr := o.volumes.EnsureOrgImageStatus(ctx, spec.StorageNamespace.OrgID, image)
		if imageErr != nil {
			return OrgRuntimeStatus{}, fmt.Errorf("warm org image %s: %w", imageRef, imageErr)
		}
		images = append(images, WarmedImage{
			ImageRef:       imageStatus.ImageRef,
			SourceSnapshot: imageStatus.SourceSnapshot.String(),
			SourceDigest:   imageStatus.SourceDigest,
			OrgSnapshot:    imageStatus.Snapshot.String(),
		})
	}
	readyAt := time.Now().UTC()
	status = OrgRuntimeStatus{
		StorageNamespace: spec.StorageNamespace,
		Images:           images,
		ReadyAt:          readyAt,
		VerifiedAt:       readyAt,
	}
	if o.state != nil {
		if err := o.state.upsertOrgRuntime(ctx, orgRuntimeSnapshotFromStatus(status)); err != nil {
			return OrgRuntimeStatus{}, err
		}
	}
	return status, nil
}

func (o *Orchestrator) AssertOrgRuntimeReady(ctx context.Context, spec OrgRuntimeWarmSpec) (plan orgRuntimePlan, err error) {
	spec, err = o.normalizeOrgRuntimeWarmSpec(spec)
	if err != nil {
		return orgRuntimePlan{}, err
	}
	ctx, span := tracer.Start(ctx, "vmorchestrator.org_runtime.assert_ready",
		trace.WithAttributes(
			attribute.String("org.id", spec.StorageNamespace.OrgID),
			uint64TraceAttribute("storage.quota_bytes", spec.StorageNamespace.QuotaBytes),
			attribute.Int("zfs.image_ref_count", len(spec.ImageRefs)),
		),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	if o.state == nil {
		return orgRuntimePlan{}, fmt.Errorf("host org runtime state store is required")
	}
	stored, err := o.state.getOrgRuntime(ctx, spec.StorageNamespace.OrgID)
	if err != nil {
		if errors.Is(err, errOrgRuntimeNotWarm) {
			return orgRuntimePlan{}, fmt.Errorf("org runtime %s is not warm", spec.StorageNamespace.OrgID)
		}
		return orgRuntimePlan{}, err
	}
	if stored.QuotaBytes != spec.StorageNamespace.QuotaBytes {
		return orgRuntimePlan{}, fmt.Errorf("org runtime %s quota = %d, want %d", spec.StorageNamespace.OrgID, stored.QuotaBytes, spec.StorageNamespace.QuotaBytes)
	}
	if _, err := o.volumes.AssertStorageNamespaceReady(ctx, zfs.StorageNamespace{
		OrgID:      spec.StorageNamespace.OrgID,
		QuotaBytes: spec.StorageNamespace.QuotaBytes,
	}); err != nil {
		return orgRuntimePlan{}, err
	}

	byRef := make(map[string]orgRuntimeImageState, len(stored.Images))
	for _, image := range stored.Images {
		byRef[image.ImageRef] = image
	}
	imageSnapshots := make(map[string]zfs.Snapshot, len(spec.ImageRefs))
	for _, imageRef := range spec.ImageRefs {
		imageState, ok := byRef[imageRef]
		if !ok {
			return orgRuntimePlan{}, fmt.Errorf("org runtime %s image %s is not warm", spec.StorageNamespace.OrgID, imageRef)
		}
		if strings.TrimSpace(imageState.SourceDigest) == "" {
			return orgRuntimePlan{}, fmt.Errorf("org runtime %s image %s source digest is empty", spec.StorageNamespace.OrgID, imageRef)
		}
		snapshot, parseErr := zfs.OrgImageSnapshot(o.roots, spec.StorageNamespace.OrgID, imageRef, imageState.SourceDigest)
		if parseErr != nil {
			return orgRuntimePlan{}, fmt.Errorf("org runtime %s image %s snapshot: %w", spec.StorageNamespace.OrgID, imageRef, parseErr)
		}
		if imageState.OrgSnapshot != snapshot.String() {
			return orgRuntimePlan{}, fmt.Errorf("org runtime %s image %s snapshot = %s, want %s", spec.StorageNamespace.OrgID, imageRef, imageState.OrgSnapshot, snapshot.String())
		}
		exists, existsErr := o.ops.ZFSSnapshotExists(ctx, snapshot.String())
		if existsErr != nil {
			return orgRuntimePlan{}, fmt.Errorf("check org runtime image %s snapshot: %w", imageRef, existsErr)
		}
		if !exists {
			return orgRuntimePlan{}, fmt.Errorf("org runtime %s image %s snapshot %s is missing", spec.StorageNamespace.OrgID, imageRef, snapshot.String())
		}
		imageSnapshots[imageRef] = snapshot
	}
	status := orgRuntimeStatusFromSnapshot(stored)
	status.StorageNamespace = spec.StorageNamespace
	status.VerifiedAt = time.Now().UTC()
	span.SetAttributes(
		attribute.String("org_runtime.ready_at", status.ReadyAt.Format(time.RFC3339Nano)),
		attribute.String("org_runtime.verified_at", status.VerifiedAt.Format(time.RFC3339Nano)),
	)
	return orgRuntimePlan{ImageSnapshots: imageSnapshots}, nil
}

func (o *Orchestrator) orgRuntimeWarmSpecForLease(spec LeaseSpec) OrgRuntimeWarmSpec {
	return OrgRuntimeWarmSpec{
		StorageNamespace: spec.StorageNamespace,
		ImageRefs:        leaseImageRefs(o.cfg.DefaultSubstrateRef, spec.FilesystemMounts),
	}
}

func (o *Orchestrator) normalizeOrgRuntimeWarmSpec(spec OrgRuntimeWarmSpec) (OrgRuntimeWarmSpec, error) {
	spec.StorageNamespace.OrgID = strings.TrimSpace(spec.StorageNamespace.OrgID)
	if !zfs.IsValidRef(spec.StorageNamespace.OrgID) {
		return OrgRuntimeWarmSpec{}, fmt.Errorf("storage namespace org id is invalid: %s", spec.StorageNamespace.OrgID)
	}
	if spec.StorageNamespace.QuotaBytes == 0 {
		return OrgRuntimeWarmSpec{}, fmt.Errorf("storage namespace quota is required")
	}
	imageRefs, err := normalizeOrgRuntimeImageRefs(o.cfg.DefaultSubstrateRef, spec.ImageRefs)
	if err != nil {
		return OrgRuntimeWarmSpec{}, err
	}
	spec.ImageRefs = imageRefs
	return spec, nil
}

func leaseImageRefs(defaultSubstrateRef string, mounts []FilesystemMount) []string {
	refs := []string{defaultSubstrateRef}
	for _, mount := range mounts {
		sourceRef := strings.TrimSpace(mount.SourceRef)
		if sourceRef != "" && !strings.Contains(sourceRef, "@") {
			refs = append(refs, sourceRef)
		}
	}
	refs, _ = normalizeOrgRuntimeImageRefs(defaultSubstrateRef, refs)
	return refs
}

func normalizeOrgRuntimeImageRefs(defaultSubstrateRef string, refs []string) ([]string, error) {
	seen := map[string]struct{}{}
	add := func(ref string) error {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return nil
		}
		if strings.Contains(ref, "@") {
			return fmt.Errorf("image ref must not be a snapshot ref: %s", ref)
		}
		if !zfs.IsValidRef(ref) {
			return fmt.Errorf("image ref is invalid: %s", ref)
		}
		seen[ref] = struct{}{}
		return nil
	}
	if err := add(defaultSubstrateRef); err != nil {
		return nil, err
	}
	for _, ref := range refs {
		if err := add(ref); err != nil {
			return nil, err
		}
	}
	out := make([]string, 0, len(seen))
	for ref := range seen {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out, nil
}

func orgRuntimeSnapshotFromStatus(status OrgRuntimeStatus) orgRuntimeSnapshot {
	images := make([]orgRuntimeImageState, 0, len(status.Images))
	for _, image := range status.Images {
		images = append(images, orgRuntimeImageState(image))
	}
	return orgRuntimeSnapshot{
		OrgID:      status.StorageNamespace.OrgID,
		QuotaBytes: status.StorageNamespace.QuotaBytes,
		Images:     images,
		ReadyAt:    status.ReadyAt,
		VerifiedAt: status.VerifiedAt,
	}
}

func orgRuntimeStatusFromSnapshot(snapshot orgRuntimeSnapshot) OrgRuntimeStatus {
	images := make([]WarmedImage, 0, len(snapshot.Images))
	for _, image := range snapshot.Images {
		images = append(images, WarmedImage(image))
	}
	return OrgRuntimeStatus{
		StorageNamespace: StorageNamespace{OrgID: snapshot.OrgID, QuotaBytes: snapshot.QuotaBytes},
		Images:           images,
		ReadyAt:          snapshot.ReadyAt,
		VerifiedAt:       snapshot.VerifiedAt,
	}
}
