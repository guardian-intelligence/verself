package vmorchestrator

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"

	"github.com/verself/vm-orchestrator/zfs"
)

type leaseBootPlan struct {
	LeaseID            string
	Spec               LeaseSpec
	Lease              zfs.Lease
	Runtime            OrgRuntimeLeasePlan
	RootSourceSnapshot zfs.Snapshot
	RootSourceKind     string
}

type preparedLeaseRoot struct {
	Dataset string
}

func (o *Orchestrator) planLeaseBoot(ctx context.Context, leaseID string, spec LeaseSpec) (leaseBootPlan, error) {
	runtimePlanCtx, endRuntimePlanSpan := startStepSpan(ctx, "vmorchestrator.org_runtime.require_ready_check",
		attribute.String("lease.id", leaseID),
		attribute.String("org.id", spec.StorageNamespace.OrgID),
		uint64TraceAttribute("storage.quota_bytes", spec.StorageNamespace.QuotaBytes),
	)
	runtimePlan, runtimePlanErr := o.runtime.RequireReady(runtimePlanCtx, orgRuntimeShapeForLease(o.cfg.DefaultSubstrateRef, spec))
	endRuntimePlanSpan(runtimePlanErr)
	if runtimePlanErr != nil {
		return leaseBootPlan{}, fmt.Errorf("org runtime is not ready: %w", runtimePlanErr)
	}
	lease, leaseRefErr := zfs.NewLease(o.roots, spec.StorageNamespace.OrgID, leaseID)
	if leaseRefErr != nil {
		return leaseBootPlan{}, fmt.Errorf("lease ref: %w", leaseRefErr)
	}
	substrateSnapshot, ok := runtimePlan.ImageSnapshots[o.cfg.DefaultSubstrateRef]
	if !ok && strings.TrimSpace(spec.GoldenVM.RootSnapshotRef) == "" {
		return leaseBootPlan{}, fmt.Errorf("org runtime substrate image %s is not ready", o.cfg.DefaultSubstrateRef)
	}
	rootSourceSnapshot := substrateSnapshot
	rootSourceKind := "substrate"
	if ref := strings.TrimSpace(spec.GoldenVM.RootSnapshotRef); ref != "" {
		parsed, parseErr := zfs.ParseSnapshot(ref)
		if parseErr != nil {
			return leaseBootPlan{}, fmt.Errorf("golden VM root snapshot ref: %w", parseErr)
		}
		rootSourceSnapshot = parsed
		rootSourceKind = "golden_vm"
	}
	return leaseBootPlan{
		LeaseID:            leaseID,
		Spec:               spec,
		Lease:              lease,
		Runtime:            runtimePlan,
		RootSourceSnapshot: rootSourceSnapshot,
		RootSourceKind:     rootSourceKind,
	}, nil
}

func (o *Orchestrator) prepareLeaseRoot(ctx context.Context, plan leaseBootPlan) (preparedLeaseRoot, error) {
	rootCloneCtx, endRootCloneSpan := startStepSpan(ctx, "vmorchestrator.zfs.root_clone",
		attribute.String("lease.id", plan.LeaseID),
		attribute.String("org.id", plan.Spec.StorageNamespace.OrgID),
		attribute.String("zfs.dataset", plan.Lease.RootDataset()),
		attribute.String("zfs.image_ref", o.cfg.DefaultSubstrateRef),
		attribute.String("zfs.source_snapshot", plan.RootSourceSnapshot.String()),
		attribute.String("zfs.source_kind", plan.RootSourceKind),
	)
	prepErr := o.volumes.PrepareSubstrateCloneFromSnapshot(rootCloneCtx, plan.Lease, plan.RootSourceSnapshot, plan.Lease.ID())
	endRootCloneSpan(prepErr)
	if prepErr != nil {
		return preparedLeaseRoot{}, prepErr
	}

	rootBytes := uint64(plan.Spec.Resources.RootDiskGiB) * 1024 * 1024 * 1024
	rootResizeCtx, endRootResizeSpan := startStepSpan(ctx, "vmorchestrator.zfs.root_resize_ext4",
		attribute.String("lease.id", plan.LeaseID),
		attribute.String("org.id", plan.Spec.StorageNamespace.OrgID),
		attribute.String("zfs.dataset", plan.Lease.RootDataset()),
		uint64TraceAttribute("zfs.size_bytes", rootBytes),
		attribute.Int("vmresources.root_disk_gib", int(plan.Spec.Resources.RootDiskGiB)),
	)
	resizeErr := o.volumes.ResizeLeaseRootExt4(rootResizeCtx, plan.Lease, rootBytes)
	endRootResizeSpan(resizeErr)
	if resizeErr != nil {
		_ = o.volumes.DestroyLeaseRoot(context.Background(), plan.Lease)
		return preparedLeaseRoot{}, resizeErr
	}
	return preparedLeaseRoot{Dataset: plan.Lease.RootDataset()}, nil
}

func (o *Orchestrator) prepareLeaseFilesystems(ctx context.Context, plan leaseBootPlan) ([]preparedFilesystemMount, error) {
	mountsCtx, endMountsSpan := startStepSpan(ctx, "vmorchestrator.zfs.mounts_prepare",
		attribute.String("lease.id", plan.LeaseID),
		attribute.String("org.id", plan.Spec.StorageNamespace.OrgID),
		attribute.Int("filesystem.mount_count", len(plan.Spec.FilesystemMounts)),
	)
	mounts, mountErr := o.prepareFilesystemMounts(mountsCtx, plan.Lease, plan.Spec.StorageNamespace.QuotaBytes, plan.Spec.FilesystemMounts, plan.Runtime.ImageSnapshots)
	endMountsSpan(mountErr)
	return mounts, mountErr
}

func (o *Orchestrator) activateLeaseRuntime(ctx context.Context, plan leaseBootPlan, root preparedLeaseRoot, mounts []preparedFilesystemMount, observer LeaseObserver) (*LeaseRuntime, error) {
	return o.bootDataset(ctx, plan.Lease, plan.Spec, root.Dataset, mounts, observer)
}
