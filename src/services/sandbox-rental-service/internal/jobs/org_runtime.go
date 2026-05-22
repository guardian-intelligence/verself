package jobs

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	billingclient "github.com/verself/billing-service/client"
	"github.com/verself/sandbox-rental-service/internal/store"
	vmorchestrator "github.com/verself/vm-orchestrator"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const orgRuntimeEnsureTimeout = 45 * time.Second

func (s *Service) ensureOrgRuntimeForRunnerClass(ctx context.Context, orgID, productID, runnerClass string) error {
	mounts, err := s.runnerClassFilesystemMountsRead(ctx, runnerClass)
	if err != nil {
		return err
	}
	quotaBytes, err := s.durableStorageQuotaBytesForOrgProduct(ctx, orgID, productID)
	if err != nil {
		return err
	}
	_, err = s.ensureOrgRuntime(ctx, orgID, productID, quotaBytes, mounts)
	return err
}

func (s *Service) ensureOrgRuntime(ctx context.Context, orgID, productID string, quotaBytes uint64, mounts []vmorchestrator.FilesystemMount) (status vmorchestrator.OrgRuntimeStatus, err error) {
	ctx, span := tracer.Start(ctx, "sandbox.org_runtime.ensure",
		trace.WithAttributes(
			attribute.String("org.id", orgID),
			attribute.String("billing.product_id", productID),
			attribute.String("storage.quota_bytes", strconv.FormatUint(quotaBytes, 10)),
			attribute.Int("filesystem.mount_count", len(mounts)),
		),
	)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	if s.Orchestrator == nil {
		return vmorchestrator.OrgRuntimeStatus{}, ErrRunnerUnavailable
	}
	imageRefs := imageRefsFromFilesystemMounts(mounts)
	span.SetAttributes(attribute.Int("zfs.image_ref_count", len(imageRefs)))
	ensureCtx, cancel := context.WithTimeout(ctx, orgRuntimeEnsureTimeout)
	defer cancel()
	status, err = s.Orchestrator.EnsureOrgRuntime(ensureCtx, vmorchestrator.OrgRuntimeShape{
		StorageNamespace: vmorchestrator.StorageNamespace{
			OrgID:      orgID,
			QuotaBytes: quotaBytes,
		},
		ImageRefs: imageRefs,
	})
	if err != nil {
		return vmorchestrator.OrgRuntimeStatus{}, fmt.Errorf("ensure org runtime: %w", err)
	}
	span.SetAttributes(
		attribute.String("org_runtime.ready_at", status.ReadyAt.Format(time.RFC3339Nano)),
		attribute.String("org_runtime.verified_at", status.VerifiedAt.Format(time.RFC3339Nano)),
	)
	return status, nil
}

func (s *Service) durableStorageQuotaBytesForOrgProduct(ctx context.Context, orgID, productID string) (uint64, error) {
	entitlement, err := s.durableStorageEntitlementForOrgProduct(ctx, orgID, productID)
	if err != nil {
		return 0, err
	}
	quotaBytes, err := billingSafeUint64(entitlement.DurableStorageQuotaBytes, "durable storage quota bytes")
	if err != nil {
		return 0, err
	}
	if quotaBytes == 0 {
		return 0, fmt.Errorf("durable storage quota bytes is required")
	}
	return quotaBytes, nil
}

func (s *Service) durableStorageEntitlementForOrgProduct(ctx context.Context, orgID, productID string) (billingclient.BillingStorageEntitlement, error) {
	if s.Billing == nil {
		return billingclient.BillingStorageEntitlement{}, fmt.Errorf("billing client is required for durable storage entitlement")
	}
	resp, err := s.Billing.GetStorageEntitlement(ctx, billingclient.GetStorageEntitlementRequest{
		Body: billingclient.GetStorageEntitlementInputBody{
			OrgID:     billingclient.OrgId(orgID),
			ProductID: billingclient.ProductId(productID),
		},
	})
	if err != nil {
		return billingclient.BillingStorageEntitlement{}, fmt.Errorf("get durable storage entitlement: %w", err)
	}
	if resp.StatusCode != http.StatusOK || resp.Result == nil {
		return billingclient.BillingStorageEntitlement{}, newBillingStatusError("get durable storage entitlement", resp.StatusCode, resp.Problem, nil)
	}
	return resp.Result.Entitlement, nil
}

func (s *Service) runnerClassFilesystemMountsRead(ctx context.Context, runnerClass string) ([]vmorchestrator.FilesystemMount, error) {
	return s.runnerClassFilesystemMountsFromQueries(ctx, s.storeQueries(), runnerClass)
}

func (s *Service) runnerClassFilesystemMountsFromQueries(ctx context.Context, q *store.Queries, runnerClass string) ([]vmorchestrator.FilesystemMount, error) {
	rows, err := q.ListRunnerClassFilesystemMounts(ctx, store.ListRunnerClassFilesystemMountsParams{RunnerClass: runnerClass})
	if err != nil {
		return nil, fmt.Errorf("load runner class filesystem mounts: %w", err)
	}
	out := make([]vmorchestrator.FilesystemMount, 0, len(rows))
	for _, row := range rows {
		out = append(out, vmorchestrator.FilesystemMount{
			Name:      row.MountName,
			SourceRef: row.SourceRef,
			MountPath: row.MountPath,
			FSType:    row.FsType,
			ReadOnly:  row.ReadOnly,
		})
	}
	return out, nil
}

func imageRefsFromFilesystemMounts(mounts []vmorchestrator.FilesystemMount) []string {
	seen := map[string]struct{}{}
	if substrateRef := strings.TrimSpace(vmorchestrator.DefaultConfig().DefaultSubstrateRef); substrateRef != "" {
		seen[substrateRef] = struct{}{}
	}
	for _, mount := range mounts {
		sourceRef := strings.TrimSpace(mount.SourceRef)
		if sourceRef == "" || strings.Contains(sourceRef, "@") {
			continue
		}
		seen[sourceRef] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for ref := range seen {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}
