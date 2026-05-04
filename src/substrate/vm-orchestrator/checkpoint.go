package vmorchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/verself/vm-orchestrator/vmproto"
	"github.com/verself/vm-orchestrator/zfs"
)

func (o *Orchestrator) checkpointHandler(ctx context.Context, lease zfs.Lease, allowedSaveRefs map[string]struct{}, observer LeaseObserver, logger *slog.Logger) checkpointHandler {
	return func(req vmproto.CheckpointRequest) vmproto.CheckpointResponse {
		resp := o.handleCheckpointRequest(ctx, lease, allowedSaveRefs, req, logger)
		observer = normalizeLeaseObserver(observer)
		observer.OnGuestCheckpoint(lease.ID(), CheckpointEvent{
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

func (o *Orchestrator) handleCheckpointRequest(ctx context.Context, lease zfs.Lease, allowedSaveRefs map[string]struct{}, req vmproto.CheckpointRequest, logger *slog.Logger) vmproto.CheckpointResponse {
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

	result, err := o.volumes.Checkpoint(detachedTraceContext(ctx), lease, resp.Ref)
	if err != nil {
		resp.Error = err.Error()
		if logger != nil {
			logger.WarnContext(ctx, "checkpoint snapshot failed",
				"lease_id", lease.ID(),
				"ref", resp.Ref,
				"request_id", resp.RequestID,
				"error", err,
			)
		}
		return resp
	}
	resp.Accepted = true
	resp.VersionID = result.VersionID
	if logger != nil {
		logger.InfoContext(ctx, "checkpoint snapshot saved",
			"lease_id", lease.ID(),
			"ref", resp.Ref,
			"version_id", result.VersionID,
		)
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
