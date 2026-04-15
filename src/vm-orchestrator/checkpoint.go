package vmorchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/forge-metal/vm-orchestrator/vmproto"
)

const checkpointSnapshotPrefix = "ckpt-"

var zfsSnapshotNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func (o *Orchestrator) checkpointHandler(ctx context.Context, leaseID string, dataset string, allowedSaveRefs map[string]struct{}, observer LeaseObserver, logger *slog.Logger) checkpointHandler {
	return func(req vmproto.CheckpointRequest) vmproto.CheckpointResponse {
		resp := o.handleCheckpointRequest(ctx, leaseID, dataset, allowedSaveRefs, req, logger)
		observer = normalizeLeaseObserver(observer)
		observer.OnGuestCheckpoint(leaseID, CheckpointEvent{
			RequestID: resp.RequestID,
			Operation: resp.Operation,
			Ref:       resp.Ref,
			Accepted:  resp.Accepted,
			VersionID: resp.VersionID,
			Error:     resp.Error,
		})
		return resp
	}
}

func (o *Orchestrator) handleCheckpointRequest(ctx context.Context, leaseID string, dataset string, allowedSaveRefs map[string]struct{}, req vmproto.CheckpointRequest, logger *slog.Logger) vmproto.CheckpointResponse {
	resp := vmproto.CheckpointResponse{
		RequestID: req.RequestID,
		Operation: req.Operation,
		Ref:       strings.TrimSpace(req.Ref),
	}
	if strings.TrimSpace(resp.RequestID) == "" {
		resp.RequestID = uuid.NewString()
	}
	if err := vmproto.ValidateCheckpointRequest(req); err != nil {
		resp.Error = err.Error()
		return resp
	}
	if req.Operation != vmproto.CheckpointOperationSave {
		resp.Error = fmt.Sprintf("unsupported checkpoint operation %q", req.Operation)
		return resp
	}
	if _, ok := allowedSaveRefs[resp.Ref]; !ok {
		resp.Error = fmt.Sprintf("checkpoint save ref %q is not authorized for this lease", resp.Ref)
		return resp
	}
	if dataset == "" {
		resp.Error = "active dataset is not available"
		return resp
	}

	versionID := uuid.NewString()
	snapshotName := checkpointSnapshotPrefix + strings.ReplaceAll(versionID, "-", "")
	if err := validateZFSSnapshotName(snapshotName); err != nil {
		resp.Error = err.Error()
		return resp
	}

	snapshotCtx, cancel := context.WithTimeout(detachedTraceContext(ctx), zfsTimeout)
	defer cancel()
	snapshotCtx, endSnapshotSpan := startStepSpan(snapshotCtx, "vmorchestrator.checkpoint.snapshot")
	props := map[string]string{
		"forge:lease_id":             leaseID,
		"forge:checkpoint_ref":       resp.Ref,
		"forge:checkpoint_version":   versionID,
		"forge:checkpoint_created":   time.Now().UTC().Format(time.RFC3339Nano),
		"forge:checkpoint_operation": req.Operation,
	}
	if err := o.ops.ZFSSnapshot(snapshotCtx, dataset, snapshotName, props); err != nil {
		endSnapshotSpan(err)
		resp.Error = err.Error()
		if logger != nil {
			logger.WarnContext(ctx, "checkpoint snapshot failed", "lease_id", leaseID, "ref", resp.Ref, "request_id", resp.RequestID, "error", err)
		}
		return resp
	}
	endSnapshotSpan(nil)

	resp.Accepted = true
	resp.VersionID = versionID
	if logger != nil {
		logger.InfoContext(ctx, "checkpoint snapshot saved", "lease_id", leaseID, "ref", resp.Ref, "version_id", versionID)
	}
	return resp
}

func normalizeCheckpointRefSet(refs []string) map[string]struct{} {
	out := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if err := vmproto.ValidateCheckpointRef(ref); err != nil {
			continue
		}
		out[ref] = struct{}{}
	}
	return out
}

func validateZFSSnapshotName(snapshotName string) error {
	if strings.TrimSpace(snapshotName) != snapshotName || snapshotName == "" {
		return fmt.Errorf("zfs snapshot name is required")
	}
	if strings.ContainsAny(snapshotName, "@/") {
		return fmt.Errorf("zfs snapshot name must not contain '@' or '/'")
	}
	if !zfsSnapshotNamePattern.MatchString(snapshotName) {
		return fmt.Errorf("invalid zfs snapshot name %q", snapshotName)
	}
	return nil
}
