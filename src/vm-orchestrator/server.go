package vmorchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"time"

	vmrpc "github.com/verself/vm-orchestrator/proto/v1"
	"github.com/verself/vm-orchestrator/vmproto"
	"github.com/oklog/ulid/v2"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type APIServer struct {
	vmrpc.UnimplementedVMServiceServer

	cfg    Config
	logger *slog.Logger
	state  *hostStateStore

	mu     sync.RWMutex
	actors map[string]*vmActor
}

type vmActor struct {
	leaseID string
	spec    LeaseSpec

	server  *APIServer
	mailbox chan vmCommand
	done    chan struct{}

	runtime *LeaseRuntime
	state   LeaseState
	expires time.Time
	active  *activeExec
}

type activeExec struct {
	execID string
	cancel context.CancelFunc
}

type vmCommand interface{}

// acquireCmd carries the caller's context so the lease boot path can inherit
// its SpanContext. detachedTraceContext strips cancellation but preserves the
// trace/baggage, so the lease goroutine outlives the RPC while staying joined
// to the same trace.
type acquireCmd struct {
	ctx   context.Context
	reply chan acquireReply
}
type acquireReply struct {
	record LeaseRecord
	err    error
}
type renewCmd struct {
	expiresAt time.Time
	allowlist []string
	reply     chan error
}
type releaseCmd struct {
	reason string
	state  LeaseState
	event  LeaseEventType
	reply  chan error
}
type startExecCmd struct {
	ctx    context.Context
	execID string
	spec   ExecSpec
	reply  chan startExecReply
}
type startExecReply struct {
	startedAt time.Time
	err       error
}
type commitFilesystemMountCmd struct {
	ctx             context.Context
	mountName       string
	targetSourceRef string
	reply           chan commitFilesystemMountReply
}
type commitFilesystemMountReply struct {
	result FilesystemCommitResult
	err    error
}
type cancelExecCmd struct {
	execID string
	reason string
	reply  chan bool
}
type execDoneCmd struct {
	execID string
	result ExecResult
	err    error
}
type checkpointSavedCmd struct {
	event CheckpointEvent
}
type telemetryCmd struct {
	event TelemetryEvent
}

func NewAPIServer(cfg Config, logger *slog.Logger) (*APIServer, error) {
	base := DefaultConfig()
	if cfg.Pool != "" {
		base = cfg
	}
	if base.ImageDataset == "" {
		base.ImageDataset = "images"
	}
	if logger == nil {
		logger = slog.Default()
	}
	if _, err := telemetryFaultProfileFromConfig(base); err != nil {
		return nil, err
	}
	state, err := openHostStateStore(base.StateDBPath, logger)
	if err != nil {
		return nil, fmt.Errorf("open host state ledger %s: %w", base.StateDBPath, err)
	}
	server := &APIServer{
		cfg:    base,
		logger: logger,
		state:  state,
		actors: map[string]*vmActor{},
	}
	if err := server.recoverNetworkState(context.Background()); err != nil {
		_ = state.close()
		return nil, err
	}
	if err := server.markUnownedActiveLeasesCrashed(context.Background()); err != nil {
		_ = state.close()
		return nil, err
	}
	return server, nil
}

func (s *APIServer) recoverNetworkState(ctx context.Context) error {
	cfg := NetworkPoolConfig{
		PoolCIDR:      s.cfg.GuestPoolCIDR,
		StateDBPath:   s.cfg.StateDBPath,
		HostInterface: s.cfg.HostInterface,
		TapOwnerUID:   s.cfg.JailerUID,
		TapOwnerGID:   s.cfg.JailerGID,
	}
	recoverCtx, endRecoverSpan := startStepSpan(ctx, "vmorchestrator.network.startup_recover",
		attribute.String("network.pool_cidr", cfg.PoolCIDR),
	)
	err := NewAllocator(cfg).Recover(recoverCtx, DirectPrivOps{})
	endRecoverSpan(err)
	if err != nil {
		return fmt.Errorf("recover host network state: %w", err)
	}
	return nil
}

func (s *APIServer) Close() error {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	actors := make([]*vmActor, 0, len(s.actors))
	for _, actor := range s.actors {
		actors = append(actors, actor)
	}
	s.mu.RUnlock()
	for _, actor := range actors {
		reply := make(chan error, 1)
		actor.send(releaseCmd{reason: "server_shutdown", state: LeaseStateReleased, event: LeaseEventLeaseReleased, reply: reply})
		<-reply
	}
	if s.state != nil {
		return s.state.close()
	}
	return nil
}

func (s *APIServer) AcquireLease(ctx context.Context, req *vmrpc.AcquireLeaseRequest) (*vmrpc.AcquireLeaseResponse, error) {
	ctx, span := tracer.Start(ctx, "rpc.AcquireLease")
	defer span.End()
	key := strings.TrimSpace(req.GetIdempotencyKey())
	if key == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency_key is required")
	}
	if prior, ok, err := s.state.getIdempotency(ctx, "acquire_lease", key); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	} else if ok {
		resp := &vmrpc.AcquireLeaseResponse{}
		if err := json.Unmarshal([]byte(prior), resp); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return resp, nil
	}

	spec, specErr := leaseSpecFromProto(req.GetSpec(), s.cfg)
	if specErr != nil {
		return nil, status.Error(codes.InvalidArgument, specErr.Error())
	}
	leaseID := newHostID()
	actor := &vmActor{
		leaseID: leaseID,
		spec:    spec,
		server:  s,
		mailbox: make(chan vmCommand, 16),
		done:    make(chan struct{}),
		state:   LeaseStateAcquiring,
		expires: time.Now().UTC().Add(time.Duration(spec.TTLSeconds) * time.Second),
	}
	s.rememberActor(actor)
	go actor.run()

	reply := make(chan acquireReply, 1)
	if !actor.send(acquireCmd{ctx: ctx, reply: reply}) {
		return nil, status.Error(codes.Unavailable, "lease actor is not accepting commands")
	}
	var out acquireReply
	select {
	case out = <-reply:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if out.err != nil {
		s.forgetActor(leaseID)
		span.RecordError(out.err)
		span.SetStatus(otelcodes.Error, out.err.Error())
		return nil, status.Error(codes.Internal, out.err.Error())
	}
	resp := acquireLeaseResponseFromRecord(out.record)
	data, _ := json.Marshal(resp)
	if err := s.state.putIdempotency(context.Background(), "acquire_lease", key, string(data)); err != nil {
		s.logger.WarnContext(ctx, "store acquire lease idempotency failed", "error", err)
	}
	span.SetAttributes(attribute.String("lease.id", leaseID))
	return resp, nil
}

func (s *APIServer) RenewLease(ctx context.Context, req *vmrpc.RenewLeaseRequest) (*vmrpc.RenewLeaseResponse, error) {
	_, span := tracer.Start(ctx, "rpc.RenewLease")
	defer span.End()
	leaseID := strings.TrimSpace(req.GetLeaseId())
	if leaseID == "" {
		return nil, status.Error(codes.InvalidArgument, "lease_id is required")
	}
	key := strings.TrimSpace(req.GetIdempotencyKey())
	if key == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency_key is required")
	}
	scope := "renew_lease:" + leaseID
	if prior, ok, err := s.state.getIdempotency(ctx, scope, key); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	} else if ok {
		resp := &vmrpc.RenewLeaseResponse{}
		if err := json.Unmarshal([]byte(prior), resp); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return resp, nil
	}
	actor, ok := s.lookupActor(leaseID)
	if !ok {
		return nil, status.Error(codes.NotFound, "lease not live")
	}
	extend := req.GetExtendSeconds()
	if extend == 0 {
		return nil, status.Error(codes.InvalidArgument, "extend_seconds is required")
	}
	snapshot, err := s.state.getLease(ctx, leaseID)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	expiresAt := snapshot.ExpiresAt.Add(time.Duration(extend) * time.Second)
	if max := time.Now().UTC().Add(24 * time.Hour); expiresAt.After(max) {
		expiresAt = max
	}
	allowlist := snapshot.Allowlist
	if len(req.GetCheckpointSaveAllowlist()) > 0 {
		allowlist = req.GetCheckpointSaveAllowlist()
	}
	reply := make(chan error, 1)
	actor.send(renewCmd{expiresAt: expiresAt, allowlist: allowlist, reply: reply})
	if err := <-reply; err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	resp := &vmrpc.RenewLeaseResponse{LeaseId: leaseID, ExpiresAtUnixNs: uint64(expiresAt.UnixNano())}
	data, _ := json.Marshal(resp)
	_ = s.state.putIdempotency(context.Background(), scope, key, string(data))
	return resp, nil
}

func (s *APIServer) ReleaseLease(ctx context.Context, req *vmrpc.ReleaseLeaseRequest) (*vmrpc.ReleaseLeaseResponse, error) {
	_, span := tracer.Start(ctx, "rpc.ReleaseLease")
	defer span.End()
	leaseID := strings.TrimSpace(req.GetLeaseId())
	if leaseID == "" {
		return nil, status.Error(codes.InvalidArgument, "lease_id is required")
	}
	actor, ok := s.lookupActor(leaseID)
	if !ok {
		snap, err := s.state.getLease(ctx, leaseID)
		if err != nil {
			return nil, status.Error(codes.NotFound, "lease not found")
		}
		return &vmrpc.ReleaseLeaseResponse{LeaseId: leaseID, State: leaseStateToProto(snap.State), ReleasedAtUnixNs: uint64(snap.TerminalAt.UnixNano())}, nil
	}
	reply := make(chan error, 1)
	actor.send(releaseCmd{reason: "released_by_client", state: LeaseStateReleased, event: LeaseEventLeaseReleased, reply: reply})
	if err := <-reply; err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	s.forgetActor(leaseID)
	return &vmrpc.ReleaseLeaseResponse{LeaseId: leaseID, State: vmrpc.LeaseState_LEASE_STATE_RELEASED, ReleasedAtUnixNs: uint64(time.Now().UTC().UnixNano())}, nil
}

func (s *APIServer) GetLease(ctx context.Context, req *vmrpc.GetLeaseRequest) (*vmrpc.GetLeaseResponse, error) {
	_, span := tracer.Start(ctx, "rpc.GetLease")
	defer span.End()
	snap, err := s.state.getLease(ctx, strings.TrimSpace(req.GetLeaseId()))
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return &vmrpc.GetLeaseResponse{Lease: leaseSnapshotToProto(snap)}, nil
}

func (s *APIServer) ListLeases(ctx context.Context, req *vmrpc.ListLeasesRequest) (*vmrpc.ListLeasesResponse, error) {
	_, span := tracer.Start(ctx, "rpc.ListLeases")
	defer span.End()
	leases, err := s.state.listLeases(ctx, req.GetIncludeTerminal(), int(req.GetLimit()))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	resp := &vmrpc.ListLeasesResponse{Leases: make([]*vmrpc.LeaseRecord, 0, len(leases))}
	for _, lease := range leases {
		resp.Leases = append(resp.Leases, leaseSnapshotToProto(lease))
	}
	return resp, nil
}

func (s *APIServer) StreamLeaseEvents(req *vmrpc.StreamLeaseEventsRequest, stream vmrpc.VMService_StreamLeaseEventsServer) error {
	ctx, span := tracer.Start(stream.Context(), "rpc.StreamLeaseEvents")
	defer span.End()
	leaseID := strings.TrimSpace(req.GetLeaseId())
	if leaseID == "" {
		return status.Error(codes.InvalidArgument, "lease_id is required")
	}
	fromSeq := req.GetFromSeq()
	limit := int(req.GetBatchSize())
	if limit <= 0 || limit > 1000 {
		limit = 256
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		events, err := s.state.listLeaseEvents(ctx, leaseID, fromSeq, limit)
		if err != nil {
			return status.Error(codes.Internal, err.Error())
		}
		for _, event := range events {
			if err := stream.Send(leaseEventToProto(leaseID, event)); err != nil {
				return err
			}
			fromSeq = event.Seq
		}
		snap, err := s.state.getLease(ctx, leaseID)
		if err != nil {
			return status.Error(codes.NotFound, err.Error())
		}
		if snap.State.Terminal() {
			if !req.GetFollow() || len(events) == 0 {
				return nil
			}
			continue
		}
		if !req.GetFollow() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *APIServer) StartExec(ctx context.Context, req *vmrpc.StartExecRequest) (*vmrpc.StartExecResponse, error) {
	ctx, span := tracer.Start(ctx, "rpc.StartExec")
	defer span.End()
	leaseID := strings.TrimSpace(req.GetLeaseId())
	key := strings.TrimSpace(req.GetIdempotencyKey())
	if leaseID == "" || key == "" {
		return nil, status.Error(codes.InvalidArgument, "lease_id and idempotency_key are required")
	}
	scope := "start_exec:" + leaseID
	if prior, ok, err := s.state.getIdempotency(ctx, scope, key); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	} else if ok {
		resp := &vmrpc.StartExecResponse{}
		if err := json.Unmarshal([]byte(prior), resp); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return resp, nil
	}
	actor, ok := s.lookupActor(leaseID)
	if !ok {
		return nil, status.Error(codes.NotFound, "lease not live")
	}
	execID := newHostID()
	spec := execSpecFromProto(req.GetSpec())
	spec = normalizeExecSpec(spec)
	if spec.Env == nil {
		spec.Env = map[string]string{}
	}
	spec.Env["VERSELF_LEASE_ID"] = leaseID
	spec.Env["VERSELF_EXEC_ID"] = execID
	if err := validateExecSpec(spec); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	persistedSpec := redactExecSpecForPersistence(ctx, spec)
	if err := s.state.createExec(ctx, execSnapshot{LeaseID: leaseID, ExecID: execID, Spec: persistedSpec}); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	reply := make(chan startExecReply, 1)
	actor.send(startExecCmd{ctx: ctx, execID: execID, spec: spec, reply: reply})
	out := <-reply
	if out.err != nil {
		return nil, status.Error(codes.FailedPrecondition, out.err.Error())
	}
	resp := &vmrpc.StartExecResponse{LeaseId: leaseID, ExecId: execID, State: vmrpc.ExecState_EXEC_STATE_RUNNING, StartedAtUnixNs: uint64(out.startedAt.UnixNano())}
	data, _ := json.Marshal(resp)
	_ = s.state.putIdempotency(context.Background(), scope, key, string(data))
	return resp, nil
}

func redactExecSpecForPersistence(ctx context.Context, spec ExecSpec) ExecSpec {
	_, span := tracer.Start(ctx, "vmorchestrator.exec.spec.redact")
	defer span.End()
	redacted := spec
	redacted.Env = nil
	if len(spec.Env) > 0 {
		redacted.Env = make(map[string]string, len(spec.Env))
		for key := range spec.Env {
			redacted.Env[key] = "[redacted]"
		}
	}
	span.SetAttributes(
		attribute.Int("vmorchestrator.exec.env_key_count", len(spec.Env)),
		attribute.Bool("vmorchestrator.exec.env_values_redacted", true),
	)
	return redacted
}

func (s *APIServer) CancelExec(ctx context.Context, req *vmrpc.CancelExecRequest) (*vmrpc.CancelExecResponse, error) {
	_, span := tracer.Start(ctx, "rpc.CancelExec")
	defer span.End()
	leaseID := strings.TrimSpace(req.GetLeaseId())
	execID := strings.TrimSpace(req.GetExecId())
	actor, ok := s.lookupActor(leaseID)
	if !ok {
		return nil, status.Error(codes.NotFound, "lease not live")
	}
	reply := make(chan bool, 1)
	actor.send(cancelExecCmd{execID: execID, reason: req.GetReason(), reply: reply})
	accepted := <-reply
	state := vmrpc.ExecState_EXEC_STATE_CANCELED
	if !accepted {
		state = vmrpc.ExecState_EXEC_STATE_UNSPECIFIED
	}
	return &vmrpc.CancelExecResponse{LeaseId: leaseID, ExecId: execID, State: state, Accepted: accepted}, nil
}

func (s *APIServer) GetExec(ctx context.Context, req *vmrpc.GetExecRequest) (*vmrpc.GetExecResponse, error) {
	_, span := tracer.Start(ctx, "rpc.GetExec")
	defer span.End()
	snap, err := s.state.getExec(ctx, strings.TrimSpace(req.GetLeaseId()), strings.TrimSpace(req.GetExecId()))
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return &vmrpc.GetExecResponse{Exec: execSnapshotToProto(snap, req.GetIncludeOutput())}, nil
}

func (s *APIServer) WaitExec(ctx context.Context, req *vmrpc.WaitExecRequest) (*vmrpc.WaitExecResponse, error) {
	_, span := tracer.Start(ctx, "rpc.WaitExec")
	defer span.End()
	leaseID := strings.TrimSpace(req.GetLeaseId())
	execID := strings.TrimSpace(req.GetExecId())
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		snap, err := s.state.getExec(ctx, leaseID, execID)
		if err != nil {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if snap.State.Terminal() {
			return &vmrpc.WaitExecResponse{Exec: execSnapshotToProto(snap, req.GetIncludeOutput())}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *APIServer) CommitFilesystemMount(ctx context.Context, req *vmrpc.CommitFilesystemMountRequest) (*vmrpc.CommitFilesystemMountResponse, error) {
	ctx, span := tracer.Start(ctx, "rpc.CommitFilesystemMount")
	defer span.End()
	leaseID := strings.TrimSpace(req.GetLeaseId())
	mountName := strings.TrimSpace(req.GetMountName())
	targetSourceRef := strings.TrimSpace(req.GetTargetSourceRef())
	key := strings.TrimSpace(req.GetIdempotencyKey())
	if leaseID == "" || mountName == "" || targetSourceRef == "" || key == "" {
		return nil, status.Error(codes.InvalidArgument, "lease_id, mount_name, target_source_ref, and idempotency_key are required")
	}
	if !filesystemRefPattern.MatchString(targetSourceRef) {
		return nil, status.Error(codes.InvalidArgument, "target_source_ref is invalid")
	}
	scope := "commit_filesystem_mount:" + leaseID
	if prior, ok, err := s.state.getIdempotency(ctx, scope, key); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	} else if ok {
		resp := &vmrpc.CommitFilesystemMountResponse{}
		if err := json.Unmarshal([]byte(prior), resp); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return resp, nil
	}
	actor, ok := s.lookupActor(leaseID)
	if !ok {
		return nil, status.Error(codes.NotFound, "lease not live")
	}
	reply := make(chan commitFilesystemMountReply, 1)
	actor.send(commitFilesystemMountCmd{ctx: ctx, mountName: mountName, targetSourceRef: targetSourceRef, reply: reply})
	out := <-reply
	if out.err != nil {
		span.RecordError(out.err)
		span.SetStatus(otelcodes.Error, out.err.Error())
		return nil, status.Error(codes.FailedPrecondition, out.err.Error())
	}
	resp := &vmrpc.CommitFilesystemMountResponse{
		LeaseId:           out.result.LeaseID,
		MountName:         out.result.MountName,
		TargetSourceRef:   out.result.TargetSourceRef,
		Snapshot:          out.result.Snapshot,
		CommittedAtUnixNs: uint64(out.result.CommittedAt.UnixNano()),
	}
	data, _ := json.Marshal(resp)
	_ = s.state.putIdempotency(context.Background(), scope, key, string(data))
	span.SetAttributes(attribute.String("lease.id", leaseID), attribute.String("filesystem.name", mountName), attribute.String("filesystem.target_source_ref", targetSourceRef))
	return resp, nil
}

func (s *APIServer) SaveCheckpoint(ctx context.Context, req *vmrpc.SaveCheckpointRequest) (*vmrpc.SaveCheckpointResponse, error) {
	_, span := tracer.Start(ctx, "rpc.SaveCheckpoint")
	defer span.End()
	leaseID := strings.TrimSpace(req.GetLeaseId())
	ref := strings.TrimSpace(req.GetRef())
	actor, ok := s.lookupActor(leaseID)
	if !ok || actor.runtime == nil {
		return nil, status.Error(codes.NotFound, "lease not live")
	}
	snap, err := s.state.getLease(ctx, leaseID)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	orch := New(s.cfg, s.logger)
	resp := orch.handleCheckpointRequest(ctx, leaseID, actor.runtime.Dataset, normalizeCheckpointRefSet(snap.Allowlist), vmproto.CheckpointRequest{
		RequestID: req.GetIdempotencyKey(),
		Operation: vmproto.CheckpointOperationSave,
		Ref:       ref,
	}, s.logger)
	if !resp.Accepted {
		return nil, status.Error(codes.FailedPrecondition, resp.Error)
	}
	now := time.Now().UTC()
	_ = s.state.appendLeaseEvent(context.Background(), leaseID, LeaseEventCheckpointSaved, "", map[string]string{"ref": ref, "version_id": resp.VersionID})
	return &vmrpc.SaveCheckpointResponse{LeaseId: leaseID, Ref: ref, VersionId: resp.VersionID, SavedAtUnixNs: uint64(now.UnixNano())}, nil
}

func (s *APIServer) GetCapacity(ctx context.Context, _ *vmrpc.GetCapacityRequest) (*vmrpc.GetCapacityResponse, error) {
	_, span := tracer.Start(ctx, "rpc.GetCapacity")
	defer span.End()
	held, err := s.state.countActiveLeases(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	total := uint32(totalGuestSlots(s.cfg.GuestPoolCIDR))
	leasesHeld := uint32(held)
	available := uint32(0)
	if total > leasesHeld {
		available = total - leasesHeld
	}
	rootfsBytes, err := zfsVolsize(ctx, s.cfg.Pool+"/"+s.cfg.GoldenZvol)
	if err != nil {
		rootfsBytes = 0
	}
	return &vmrpc.GetCapacityResponse{
		GuestPoolCidr: s.cfg.GuestPoolCIDR,
		Pool: &vmrpc.VMPoolCapacity{
			TotalSlots:             total,
			LeasesHeld:             leasesHeld,
			LeasesAvailable:        available,
			MaxVcpusPerLease:       s.cfg.Bounds.MaxVCPUs,
			MaxMemoryMibPerLease:   s.cfg.Bounds.MaxMemoryMiB,
			MaxRootDiskGibPerLease: s.cfg.Bounds.MaxRootDiskGiB,
			RootfsProvisionedBytes: rootfsBytes,
		},
	}, nil
}

func (a *vmActor) run() {
	defer close(a.done)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case cmd := <-a.mailbox:
			switch msg := cmd.(type) {
			case acquireCmd:
				msg.reply <- a.handleAcquire(msg.ctx)
			case renewCmd:
				msg.reply <- a.handleRenew(msg.expiresAt, msg.allowlist)
			case releaseCmd:
				msg.reply <- a.handleRelease(msg.reason, msg.state, msg.event)
				return
			case startExecCmd:
				msg.reply <- a.handleStartExec(msg.ctx, msg.execID, msg.spec)
			case commitFilesystemMountCmd:
				msg.reply <- a.handleCommitFilesystemMount(msg.ctx, msg.mountName, msg.targetSourceRef)
			case cancelExecCmd:
				msg.reply <- a.handleCancelExec(msg.execID, msg.reason)
			case execDoneCmd:
				a.handleExecDone(msg)
			case checkpointSavedCmd:
				a.handleCheckpointSaved(msg.event)
			case telemetryCmd:
				a.handleTelemetry(msg.event)
			}
		case <-ticker.C:
			if !a.expires.IsZero() && time.Now().UTC().After(a.expires) {
				_ = a.handleRelease("lease_deadline_reached", LeaseStateExpired, LeaseEventLeaseExpired)
				a.server.forgetActor(a.leaseID)
				return
			}
		}
	}
}

func (a *vmActor) send(cmd vmCommand) bool {
	select {
	case a.mailbox <- cmd:
		return true
	case <-a.done:
		return false
	case <-time.After(2 * time.Second):
		return false
	}
}

func (a *vmActor) handleAcquire(callerCtx context.Context) acquireReply {
	// Inherit the caller's SpanContext + baggage so lease.boot reparents under
	// rpc.AcquireLease. detachedTraceContext drops cancellation (the lease
	// must outlive the RPC), but the trace/baggage ride through.
	ctx := detachedTraceContext(callerCtx)
	spec, normErr := normalizeLeaseSpec(a.spec, a.server.cfg)
	if normErr != nil {
		return acquireReply{err: normErr}
	}
	a.spec = spec
	acquiredAt := time.Now().UTC()
	a.expires = acquiredAt.Add(time.Duration(spec.TTLSeconds) * time.Second)
	if err := a.server.state.createLease(ctx, leaseSnapshot{
		LeaseID:    a.leaseID,
		State:      LeaseStateAcquiring,
		Spec:       spec,
		TrustClass: spec.TrustClass,
		Allowlist:  spec.CheckpointSaveAllowlist,
		AcquiredAt: acquiredAt,
		ExpiresAt:  a.expires,
	}); err != nil {
		return acquireReply{err: err}
	}
	_ = a.server.state.appendLeaseEvent(ctx, a.leaseID, LeaseEventVMBooting, "", nil)
	observer := &leaseObserver{actor: a}
	runtime, err := New(a.server.cfg, a.server.logger).BootLease(ctx, a.leaseID, spec, observer)
	if err != nil {
		_ = a.server.state.finishLease(ctx, a.leaseID, LeaseStateCrashed, err.Error(), LeaseEventLeaseCrashed)
		return acquireReply{err: err}
	}
	a.runtime = runtime
	a.state = LeaseStateReady
	readyAt := time.Now().UTC()
	if err := a.server.state.setLeaseReady(ctx, a.leaseID, runtime.Network.GuestIP, readyAt); err != nil {
		return acquireReply{err: err}
	}
	a.server.logger.InfoContext(ctx, "lease ready", "lease_id", a.leaseID, "vm_ip", runtime.Network.GuestIP)
	return acquireReply{record: LeaseRecord{
		LeaseID:    a.leaseID,
		State:      LeaseStateReady,
		AcquiredAt: acquiredAt,
		ReadyAt:    readyAt,
		ExpiresAt:  a.expires,
		VMIP:       runtime.Network.GuestIP,
		Resources:  spec.Resources,
		TrustClass: spec.TrustClass,
	}}
}

func (a *vmActor) handleRenew(expiresAt time.Time, allowlist []string) error {
	if a.state.Terminal() {
		return fmt.Errorf("lease is terminal")
	}
	if time.Now().UTC().After(a.expires) {
		return fmt.Errorf("lease deadline already passed")
	}
	a.expires = expiresAt
	a.spec.CheckpointSaveAllowlist = allowlist
	return a.server.state.renewLease(context.Background(), a.leaseID, expiresAt, allowlist)
}

func (a *vmActor) handleRelease(reason string, state LeaseState, event LeaseEventType) error {
	if a.active != nil {
		a.active.cancel()
		if a.runtime != nil {
			_ = a.runtime.CancelExec(a.active.execID, reason)
		}
	}
	if a.runtime != nil {
		a.runtime.Cleanup(reason)
		a.runtime = nil
	}
	a.state = state
	if err := a.server.state.finishLease(context.Background(), a.leaseID, state, reason, event); err != nil {
		return err
	}
	a.server.forgetActor(a.leaseID)
	return nil
}

func (a *vmActor) handleStartExec(callerCtx context.Context, execID string, spec ExecSpec) startExecReply {
	if a.state != LeaseStateReady || a.runtime == nil {
		return startExecReply{err: fmt.Errorf("lease is not ready")}
	}
	if a.active != nil {
		return startExecReply{err: fmt.Errorf("lease already has an active exec")}
	}
	// Reparent exec spans to the caller's trace (rpc.StartExec). The workload
	// outlives the RPC reply; detachedTraceContext keeps trace/baggage without
	// tying workload lifetime to the short RPC call.
	execCtx, cancel := context.WithCancel(detachedTraceContext(callerCtx))
	a.active = &activeExec{execID: execID, cancel: cancel}
	startedCh := make(chan time.Time, 1)
	doneCh := make(chan execDoneCmd, 1)
	go func() {
		handleCheckpoint := New(a.server.cfg, a.server.logger).checkpointHandler(execCtx, a.leaseID, a.runtime.Dataset, normalizeCheckpointRefSet(a.spec.CheckpointSaveAllowlist), &leaseObserver{actor: a}, a.server.logger)
		result, err := a.runtime.Exec(execCtx, spec, handleCheckpoint)
		doneCh <- execDoneCmd{execID: execID, result: result, err: err}
	}()
	go func() {
		done := <-doneCh
		a.send(done)
	}()
	// The bridge emits exec_started after the child has been spawned. The
	// current control path only exposes that once Exec returns; use host time
	// for the first V1 slice and tighten it in the next bridge callback pass.
	startedAt := time.Now().UTC()
	startedCh <- startedAt
	if err := a.server.state.markExecStarted(context.Background(), a.leaseID, execID, startedAt); err != nil {
		cancel()
		return startExecReply{err: err}
	}
	return startExecReply{startedAt: startedAt}
}

func (a *vmActor) handleCommitFilesystemMount(callerCtx context.Context, mountName, targetSourceRef string) commitFilesystemMountReply {
	if a.state != LeaseStateReady || a.runtime == nil {
		return commitFilesystemMountReply{err: fmt.Errorf("lease is not ready")}
	}
	if a.active != nil {
		return commitFilesystemMountReply{err: fmt.Errorf("lease has an active exec")}
	}
	commitCtx := detachedTraceContext(callerCtx)
	result, err := New(a.server.cfg, a.server.logger).CommitFilesystemMount(commitCtx, a.runtime, mountName, targetSourceRef)
	if err != nil {
		return commitFilesystemMountReply{err: err}
	}
	_ = a.server.state.appendLeaseEvent(context.Background(), a.leaseID, LeaseEventCheckpointSaved, "", map[string]string{
		"filesystem_mount":  result.MountName,
		"target_source_ref": result.TargetSourceRef,
		"snapshot":          result.Snapshot,
	})
	return commitFilesystemMountReply{result: result}
}

func (a *vmActor) handleCancelExec(execID, reason string) bool {
	if a.active == nil || a.active.execID != execID {
		return false
	}
	a.active.cancel()
	if a.runtime != nil {
		_ = a.runtime.CancelExec(execID, reason)
	}
	return true
}

func (a *vmActor) handleExecDone(done execDoneCmd) {
	if a.active != nil && a.active.execID == done.execID {
		a.active = nil
	}
	state := ExecStateExited
	reason := ""
	if done.err != nil {
		state = ExecStateFailed
		reason = done.err.Error()
	}
	if done.result.ExitCode != 0 && state == ExecStateExited {
		state = ExecStateFailed
	}
	_ = a.server.state.finishExec(context.Background(), execSnapshot{
		LeaseID:                a.leaseID,
		ExecID:                 done.execID,
		State:                  state,
		ExitCode:               done.result.ExitCode,
		TerminalReason:         reason,
		Output:                 done.result.Output,
		StartedAt:              done.result.StartedAt,
		FirstByteAt:            done.result.FirstByteAt,
		ExitedAt:               done.result.ExitedAt,
		StdoutBytes:            done.result.StdoutBytes,
		StderrBytes:            done.result.StderrBytes,
		DroppedLogBytes:        done.result.DroppedLogBytes,
		ZFSWritten:             done.result.ZFSWritten,
		RootfsProvisionedBytes: done.result.RootfsProvisionedBytes,
		Metrics:                done.result.Metrics,
	})
}

func (a *vmActor) handleCheckpointSaved(event CheckpointEvent) {
	_ = a.server.state.appendLeaseEvent(context.Background(), a.leaseID, LeaseEventCheckpointSaved, "", map[string]string{
		"request_id": event.RequestID,
		"operation":  event.Operation,
		"ref":        event.Ref,
		"accepted":   fmt.Sprintf("%t", event.Accepted),
		"version_id": event.VersionID,
		"error":      event.Error,
	})
}

func (a *vmActor) handleTelemetry(event TelemetryEvent) {
	if event.Diagnostic == nil {
		return
	}
	_ = a.server.state.appendLeaseEvent(context.Background(), a.leaseID, LeaseEventTelemetryDiagnostic, "", map[string]string{
		"kind":            string(event.Diagnostic.Kind),
		"expected_seq":    fmt.Sprintf("%d", event.Diagnostic.ExpectedSeq),
		"observed_seq":    fmt.Sprintf("%d", event.Diagnostic.ObservedSeq),
		"missing_samples": fmt.Sprintf("%d", event.Diagnostic.MissingSamples),
	})
}

type leaseObserver struct {
	actor *vmActor
}

func (o *leaseObserver) OnGuestCheckpoint(_ string, event CheckpointEvent) {
	if o != nil && o.actor != nil {
		o.actor.send(checkpointSavedCmd{event: event})
	}
}

func (o *leaseObserver) OnTelemetryEvent(event TelemetryEvent) {
	if o != nil && o.actor != nil {
		o.actor.send(telemetryCmd{event: event})
	}
}

func (s *APIServer) rememberActor(actor *vmActor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actors[actor.leaseID] = actor
}

func (s *APIServer) forgetActor(leaseID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.actors, leaseID)
}

func (s *APIServer) lookupActor(leaseID string) (*vmActor, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	actor, ok := s.actors[leaseID]
	return actor, ok
}

func (s *APIServer) markUnownedActiveLeasesCrashed(ctx context.Context) error {
	leases, err := s.state.listLeases(ctx, false, 1000)
	if err != nil {
		return err
	}
	for _, lease := range leases {
		if err := s.state.finishLease(ctx, lease.LeaseID, LeaseStateCrashed, "orchestrator_restarted", LeaseEventLeaseCrashed); err != nil {
			return err
		}
	}
	return nil
}

func newHostID() string {
	return ulid.MustNew(ulid.Timestamp(time.Now().UTC()), rand.New(rand.NewSource(time.Now().UnixNano()))).String()
}
