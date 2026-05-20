package vmorchestrator

import (
	"fmt"
	"sort"
	"strings"

	"github.com/verself/vm-orchestrator/zfs"
)

type OrgRuntimeLeasePlan struct {
	ImageSnapshots map[string]zfs.Snapshot
}

func orgRuntimeShapeForLease(defaultSubstrateRef string, spec LeaseSpec) OrgRuntimeShape {
	return OrgRuntimeShape{
		StorageNamespace: spec.StorageNamespace,
		ImageRefs:        leaseImageRefs(defaultSubstrateRef, spec.FilesystemMounts),
	}
}

func normalizeOrgRuntimeShape(defaultSubstrateRef string, shape OrgRuntimeShape) (OrgRuntimeShape, error) {
	shape.StorageNamespace.OrgID = strings.TrimSpace(shape.StorageNamespace.OrgID)
	if !zfs.IsValidRef(shape.StorageNamespace.OrgID) {
		return OrgRuntimeShape{}, fmt.Errorf("storage namespace org id is invalid: %s", shape.StorageNamespace.OrgID)
	}
	if shape.StorageNamespace.QuotaBytes == 0 {
		return OrgRuntimeShape{}, fmt.Errorf("storage namespace quota is required")
	}
	imageRefs, err := normalizeOrgRuntimeImageRefs(defaultSubstrateRef, shape.ImageRefs)
	if err != nil {
		return OrgRuntimeShape{}, err
	}
	shape.ImageRefs = imageRefs
	return shape, nil
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

func orgRuntimeStatusFromSnapshot(snapshot orgRuntimeSnapshot) OrgRuntimeStatus {
	images := make([]OrgRuntimeImage, 0, len(snapshot.Images))
	for _, image := range snapshot.Images {
		images = append(images, OrgRuntimeImage(image))
	}
	return OrgRuntimeStatus{
		StorageNamespace: StorageNamespace{OrgID: snapshot.OrgID, QuotaBytes: snapshot.QuotaBytes},
		Images:           images,
		ReadyAt:          snapshot.ReadyAt,
		VerifiedAt:       snapshot.VerifiedAt,
	}
}
