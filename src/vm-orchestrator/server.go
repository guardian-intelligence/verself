package vmorchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	vmrpc "github.com/forge-metal/vm-orchestrator/proto/v1"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type APIServer struct {
	vmrpc.UnimplementedVMServiceServer

	cfg    Config
	logger *slog.Logger
	state  *hostStateStore

	mu   sync.RWMutex
	runs map[string]*managedRun
}

type managedRun struct {
	id string

	mu             sync.RWMutex
	state          RunState
	cancel         context.CancelFunc
	result         *RunResult
	err            error
	billablePhases map[string]struct{}
}

type runObserver struct {
	server *APIServer
	run    *managedRun
}

func NewAPIServer(cfg Config, logger *slog.Logger) (*APIServer, error) {
	base := DefaultConfig()
	if cfg.Pool != "" {
		base = cfg
	}
	if logger == nil {
		logger = slog.Default()
	}

	state, err := openHostStateStore(base.StateDBPath, logger)
	if err != nil {
		return nil, fmt.Errorf("open durable host state ledger %s: %w", base.StateDBPath, err)
	}

	return &APIServer{
		cfg:    base,
		logger: logger,
		state:  state,
		runs:   make(map[string]*managedRun),
	}, nil
}

func (s *APIServer) Close() error {
	if s == nil || s.state == nil {
		return nil
	}
	return s.state.close()
}

func (s *APIServer) EnsureRun(ctx context.Context, req *vmrpc.EnsureRunRequest) (*vmrpc.EnsureRunResponse, error) {
	ctx, span := tracer.Start(ctx, "rpc.EnsureRun")
	defer span.End()

	spec := req.GetSpec()
	if spec == nil {
		return nil, status.Error(codes.InvalidArgument, "run spec is required")
	}

	runSpec := hostRunSpecFromProto(spec)
	runSpec.RunID = ensureRunID(runSpec.RunID)
	if len(runSpec.RunCommand) == 0 {
		return nil, status.Error(codes.InvalidArgument, "run_command is required")
	}
	span.SetAttributes(attribute.String("run.id", runSpec.RunID))

	record, created, existingState, err := s.ensureRunRecord(ctx, runSpec)
	if err != nil {
		return nil, err
	}

	if created {
		run := runSpecFromHostRunSpec(runSpec)
		go s.runManagedRun(record, func(runCtx context.Context, observer RunObserver) (RunResult, error) {
			orch := New(s.cfg, s.logger)
			return orch.RunObserved(runCtx, run, observer)
		})
		return &vmrpc.EnsureRunResponse{
			RunId:   runSpec.RunID,
			State:   vmrpc.RunState_RUN_STATE_PENDING,
			Created: true,
		}, nil
	}

	return &vmrpc.EnsureRunResponse{
		RunId:   runSpec.RunID,
		State:   runStateToProto(existingState),
		Created: false,
	}, nil
}

func (s *APIServer) GetRun(ctx context.Context, req *vmrpc.GetRunRequest) (*vmrpc.GetRunResponse, error) {
	_, span := tracer.Start(ctx, "rpc.GetRun")
	defer span.End()

	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}

	snapshot, err := s.state.getRunSnapshot(ctx, runID)
	if err != nil {
		if errors.Is(err, errHostRunNotFound) {
			return nil, status.Errorf(codes.NotFound, "run %s not found", runID)
		}
		return nil, status.Errorf(codes.Internal, "load run %s from host state ledger: %v", runID, err)
	}

	resp := &vmrpc.GetRunResponse{
		RunId:             snapshot.RunID,
		State:             runStateToProto(snapshot.State),
		Terminal:          snapshot.State.Terminal(),
		TerminalReason:    snapshot.TerminalReason,
		UpdatedAtUnixNano: uint64(snapshot.UpdatedAt.UnixNano()),
	}
	if snapshot.Result != nil {
		resp.Result = runResultToProto(*snapshot.Result, req.GetIncludeOutput())
	}
	return resp, nil
}

func (s *APIServer) CancelRun(ctx context.Context, req *vmrpc.CancelRunRequest) (*vmrpc.CancelRunResponse, error) {
	_, span := tracer.Start(ctx, "rpc.CancelRun")
	defer span.End()

	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	reason := strings.TrimSpace(req.GetReason())
	if reason == "" {
		reason = "requested_by_client"
	}

	record, ok := s.lookupLiveRun(runID)
	if ok {
		accepted := record.cancelRun()
		s.appendRunEvent(runID, "cancel_requested", map[string]string{
			"reason":                  reason,
			"accepted":                strconv.FormatBool(accepted),
			"host_received_unix_nano": strconv.FormatInt(time.Now().UTC().UnixNano(), 10),
		})
		return &vmrpc.CancelRunResponse{Accepted: accepted}, nil
	}

	snapshot, err := s.state.getRunSnapshot(ctx, runID)
	if err != nil {
		if errors.Is(err, errHostRunNotFound) {
			return nil, status.Errorf(codes.NotFound, "run %s not found", runID)
		}
		return nil, status.Errorf(codes.Internal, "load run %s from host state ledger: %v", runID, err)
	}
	if snapshot.State.Terminal() {
		return &vmrpc.CancelRunResponse{Accepted: false}, nil
	}

	s.appendRunEvent(runID, "cancel_requested", map[string]string{
		"reason":                  reason,
		"accepted":                "false",
		"host_received_unix_nano": strconv.FormatInt(time.Now().UTC().UnixNano(), 10),
		"detail":                  "run_not_live",
	})
	return &vmrpc.CancelRunResponse{Accepted: false}, nil
}

func (s *APIServer) StreamRunEvents(req *vmrpc.StreamRunEventsRequest, stream vmrpc.VMService_StreamRunEventsServer) error {
	ctx, span := tracer.Start(stream.Context(), "rpc.StreamRunEvents")
	defer span.End()

	runID := strings.TrimSpace(req.GetRunId())
	if runID == "" {
		return status.Error(codes.InvalidArgument, "run_id is required")
	}

	if _, err := s.state.getRunSnapshot(ctx, runID); err != nil {
		if errors.Is(err, errHostRunNotFound) {
			return status.Errorf(codes.NotFound, "run %s not found", runID)
		}
		return status.Errorf(codes.Internal, "load run %s from host state ledger: %v", runID, err)
	}

	fromSeq := req.GetFromSeq()
	limit := int(req.GetBatchSize())
	if limit <= 0 || limit > 1000 {
		limit = 256
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		events, err := s.state.listRunEvents(ctx, runID, fromSeq, limit)
		if err != nil {
			return status.Errorf(codes.Internal, "list run events for %s: %v", runID, err)
		}
		for _, event := range events {
			if err := stream.Send(&vmrpc.HostRunEvent{
				RunId:             runID,
				EventSeq:          event.Seq,
				EventType:         event.EventType,
				Attrs:             cloneStringMap(event.Attrs),
				CreatedAtUnixNano: uint64(event.CreatedAt.UnixNano()),
			}); err != nil {
				return err
			}
			fromSeq = event.Seq
		}

		snapshot, err := s.state.getRunSnapshot(ctx, runID)
		if err != nil {
			if errors.Is(err, errHostRunNotFound) {
				return status.Errorf(codes.NotFound, "run %s not found", runID)
			}
			return status.Errorf(codes.Internal, "load run %s terminal state: %v", runID, err)
		}
		if snapshot.State.Terminal() {
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

func (s *APIServer) GetCapacity(ctx context.Context, _ *vmrpc.GetCapacityRequest) (*vmrpc.GetCapacityResponse, error) {
	_, span := tracer.Start(ctx, "rpc.GetCapacity")
	defer span.End()

	activeRuns := uint32(s.activeRunCount(ctx))
	totalSlots := uint32(totalGuestSlots(s.cfg.GuestPoolCIDR))
	available := uint32(0)
	if totalSlots > activeRuns {
		available = totalSlots - activeRuns
	}

	rootfsProvisionedBytes, err := zfsVolsize(ctx, s.cfg.Pool+"/"+s.cfg.GoldenZvol)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get golden zvol volsize: %v", err)
	}

	return &vmrpc.GetCapacityResponse{
		GuestPoolCidr:          s.cfg.GuestPoolCIDR,
		TotalSlots:             totalSlots,
		ActiveRuns:             activeRuns,
		AvailableSlots:         available,
		VcpusPerVm:             uint32(s.cfg.VCPUs),
		MemoryMibPerVm:         uint32(s.cfg.MemoryMiB),
		RootfsProvisionedBytes: rootfsProvisionedBytes,
	}, nil
}

func (s *APIServer) ensureRunRecord(ctx context.Context, spec HostRunSpec) (*managedRun, bool, RunState, error) {
	runAttrs := map[string]string{}
	normalizedPhases := normalizeBillablePhaseSet(spec.BillablePhases)
	if len(normalizedPhases) > 0 {
		runAttrs["billable_phases"] = strings.Join(sortedPhaseNames(normalizedPhases), ",")
	}
	if spec.AttemptID != "" {
		runAttrs["attempt_id"] = spec.AttemptID
	}
	if spec.SegmentID != "" {
		runAttrs["segment_id"] = spec.SegmentID
	}

	if err := s.state.createRun(ctx, spec.RunID, RunStatePending, runAttrs); err != nil {
		switch {
		case errors.Is(err, errHostRunExists):
			snapshot, snapshotErr := s.state.getRunSnapshot(ctx, spec.RunID)
			if snapshotErr != nil {
				if errors.Is(snapshotErr, errHostRunNotFound) {
					return nil, false, RunStateUnspecified, status.Errorf(codes.NotFound, "run %s not found", spec.RunID)
				}
				return nil, false, RunStateUnspecified, status.Errorf(codes.Internal, "load run %s from host state ledger: %v", spec.RunID, snapshotErr)
			}
			return nil, false, snapshot.State, nil
		default:
			return nil, false, RunStateUnspecified, status.Errorf(codes.Internal, "persist run %s in host state ledger: %v", spec.RunID, err)
		}
	}

	s.logger.Info(
		"host run state transition",
		"run_id", spec.RunID,
		"from_state", "",
		"to_state", runStateName(RunStatePending),
		"reason", "accepted",
	)

	record := &managedRun{
		id:             spec.RunID,
		state:          RunStatePending,
		billablePhases: normalizedPhases,
	}
	s.rememberRun(record)
	return record, true, RunStatePending, nil
}

func (s *APIServer) rememberRun(record *managedRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[record.id] = record
}

func (s *APIServer) forgetRun(runID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.runs, runID)
}

func (s *APIServer) lookupLiveRun(runID string) (*managedRun, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.runs[runID]
	return record, ok
}

func (s *APIServer) runManagedRun(record *managedRun, runner func(context.Context, RunObserver) (RunResult, error)) {
	defer s.forgetRun(record.id)

	ctx, cancel := context.WithCancel(context.Background())
	record.setRunning(cancel)
	if err := s.state.transitionRunState(
		context.Background(),
		record.id,
		[]RunState{RunStatePending},
		RunStateRunning,
		"run_started",
		map[string]string{"run_id": record.id},
		"",
		nil,
	); err != nil {
		record.finish(RunStateFailed, nil, fmt.Errorf("transition run %s to running: %w", record.id, err))
		return
	}
	s.logger.Info(
		"host run state transition",
		"run_id", record.id,
		"from_state", runStateName(RunStatePending),
		"to_state", runStateName(RunStateRunning),
		"reason", "runner_started",
	)

	observer := &runObserver{server: s, run: record}
	result, err := runner(ctx, observer)
	finalState := finalRunState(err, result.ExitCode)
	record.finish(finalState, &result, err)

	terminalReason := errorString(err)
	terminalAttrs := map[string]string{
		"run_id":    record.id,
		"to_state":  runStateName(finalState),
		"exit_code": strconv.Itoa(result.ExitCode),
	}
	if terminalReason != "" {
		terminalAttrs["reason"] = terminalReason
	}
	if result.FailurePhase != "" {
		terminalAttrs["failure_phase"] = result.FailurePhase
	}

	if stateErr := s.state.transitionRunState(
		context.Background(),
		record.id,
		[]RunState{RunStatePending, RunStateRunning},
		finalState,
		"run_finished",
		terminalAttrs,
		terminalReason,
		&result,
	); stateErr != nil {
		s.logger.Error("persist terminal host run state failed", "run_id", record.id, "err", stateErr)
	} else {
		s.logger.Info(
			"host run state transition",
			"run_id", record.id,
			"from_state", runStateName(RunStateRunning),
			"to_state", runStateName(finalState),
			"reason", terminalReason,
		)
	}
}

func (s *APIServer) appendRunEvent(runID, eventType string, attrs map[string]string) {
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(eventType) == "" {
		return
	}
	if err := s.state.appendRunEvent(context.Background(), runID, eventType, attrs); err != nil {
		s.logger.Warn("append run event to host state ledger failed", "run_id", runID, "event_type", eventType, "err", err)
	}
}

func (s *APIServer) activeRunCount(ctx context.Context) int {
	count, err := s.state.countActiveRuns(ctx)
	if err == nil {
		return count
	}
	s.logger.Warn("active run count fell back to in-memory state", "err", err)

	s.mu.RLock()
	defer s.mu.RUnlock()
	var inMemory int
	for _, run := range s.runs {
		if state := run.currentState(); state == RunStatePending || state == RunStateRunning {
			inMemory++
		}
	}
	return inMemory
}

func (o *runObserver) OnGuestLogChunk(_ string, _ string) {
	// Logs are persisted in HostRunResult.Logs; live log streaming was removed in the run-event cutover.
}

func (o *runObserver) OnGuestPhaseStart(_ string, phase string) {
	phase = strings.TrimSpace(phase)
	if phase == "" {
		return
	}
	attrs := map[string]string{
		"phase":                   phase,
		"billable":                strconv.FormatBool(o.run.isBillablePhase(phase)),
		"host_received_unix_nano": strconv.FormatInt(time.Now().UTC().UnixNano(), 10),
	}
	if o.server != nil {
		o.server.appendRunEvent(o.run.id, "phase_started", attrs)
	}
}

func (o *runObserver) OnGuestPhaseEnd(_ string, result PhaseResult) {
	result.Name = strings.TrimSpace(result.Name)
	if result.Name == "" {
		return
	}
	attrs := map[string]string{
		"exit_code":               strconv.Itoa(result.ExitCode),
		"duration_ms":             strconv.FormatInt(result.DurationMS, 10),
		"phase":                   result.Name,
		"billable":                strconv.FormatBool(o.run.isBillablePhase(result.Name)),
		"host_received_unix_nano": strconv.FormatInt(time.Now().UTC().UnixNano(), 10),
	}
	if o.server != nil {
		o.server.appendRunEvent(o.run.id, "phase_ended", attrs)
	}
}

func (o *runObserver) OnGuestCheckpoint(_ string, event CheckpointEvent) {
	attrs := map[string]string{
		"request_id":              event.RequestID,
		"operation":               event.Operation,
		"ref":                     event.Ref,
		"accepted":                strconv.FormatBool(event.Accepted),
		"version_id":              event.VersionID,
		"error":                   event.Error,
		"host_received_unix_nano": strconv.FormatInt(time.Now().UTC().UnixNano(), 10),
	}
	if o.server != nil {
		o.server.appendRunEvent(o.run.id, "checkpoint_request", attrs)
	}
}

func (o *runObserver) OnTelemetryEvent(TelemetryEvent) {
	// Telemetry fanout API was removed in the run-event cutover.
}

func (r *managedRun) setRunning(cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state = RunStateRunning
	r.cancel = cancel
}

func (r *managedRun) finish(state RunState, result *RunResult, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state = state
	r.result = result
	r.err = err
	r.cancel = nil
}

func (r *managedRun) cancelRun() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel == nil || r.state.Terminal() {
		return false
	}
	r.cancel()
	return true
}

func (r *managedRun) isBillablePhase(phase string) bool {
	_, ok := r.billablePhases[strings.TrimSpace(phase)]
	return ok
}

func (r *managedRun) currentState() RunState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.state
}

func ensureRunID(runID string) string {
	if _, err := uuid.Parse(runID); err == nil {
		return runID
	}
	return uuid.NewString()
}

func finalRunState(err error, exitCode int) RunState {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return RunStateCanceled
	case err != nil:
		return RunStateFailed
	case exitCode != 0:
		return RunStateFailed
	default:
		return RunStateSucceeded
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func normalizeBillablePhaseSet(phases []string) map[string]struct{} {
	out := make(map[string]struct{}, len(phases))
	for _, phase := range phases {
		phase = strings.TrimSpace(phase)
		if phase == "" {
			continue
		}
		out[phase] = struct{}{}
	}
	return out
}

func sortedPhaseNames(phases map[string]struct{}) []string {
	if len(phases) == 0 {
		return nil
	}
	out := make([]string, 0, len(phases))
	for phase := range phases {
		out = append(out, phase)
	}
	slices.Sort(out)
	return out
}

func totalGuestSlots(poolCIDR string) int {
	cfg := normalizeNetworkPoolConfig(NetworkPoolConfig{PoolCIDR: poolCIDR})
	allocator := NewAllocator(cfg)
	_, slots, err := allocator.pool()
	if err != nil {
		return 0
	}
	return slots
}
