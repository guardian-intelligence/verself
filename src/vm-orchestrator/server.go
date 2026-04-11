package vmorchestrator

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

	mu            sync.RWMutex
	jobs          map[string]*managedJob
	telemetrySubs map[uint64]chan TelemetryEvent
	nextSubID     uint64
}

const managedJobLogTruncatedMarker = "\n[vm-orchestrator] log buffer truncated\n"

type managedJob struct {
	id string

	mu             sync.RWMutex
	state          JobState
	cancel         context.CancelFunc
	result         *JobResult
	err            error
	logSeq         uint64
	logBytes       int
	logTruncated   bool
	logChunks      []jobLogChunk
	eventSeq       uint64
	events         []jobGuestEvent
	billablePhases map[string]struct{}
	hello          *TelemetryHello
	sample         *TelemetrySample
	lastUpdate     time.Time
}

type jobLogChunk struct {
	Seq   uint64
	Chunk string
}

type jobGuestEvent struct {
	Seq   uint64
	Kind  string
	Attrs map[string]string
}

type jobObserver struct {
	server *APIServer
	job    *managedJob
}

func NewAPIServer(cfg Config, logger *slog.Logger) *APIServer {
	base := DefaultConfig()
	if cfg.Pool != "" {
		base = cfg
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &APIServer{
		cfg:           base,
		logger:        logger,
		jobs:          make(map[string]*managedJob),
		telemetrySubs: make(map[uint64]chan TelemetryEvent),
	}
}

func (s *APIServer) CreateJob(ctx context.Context, req *vmrpc.CreateJobRequest) (*vmrpc.CreateJobResponse, error) {
	ctx, span := tracer.Start(ctx, "rpc.CreateJob")
	defer span.End()

	switch spec := req.GetSpec().(type) {
	case *vmrpc.CreateJobRequest_DirectJob:
		job := jobConfigFromProto(spec.DirectJob.GetJob())
		job.JobID = ensureJobID(job.JobID)
		record, err := s.createJobRecord(job.JobID, job.BillablePhases)
		if err != nil {
			return nil, err
		}
		span.SetAttributes(attribute.String("job.id", job.JobID), attribute.String("job.kind", "direct"))
		go s.runManagedJob(record, func(runCtx context.Context, observer RunObserver) (JobResult, error) {
			orch := New(s.cfg, s.logger)
			return orch.RunObserved(runCtx, job, observer)
		})
		return &vmrpc.CreateJobResponse{
			JobId: job.JobID,
			State: vmrpc.JobState_JOB_STATE_PENDING,
		}, nil
	default:
		return nil, status.Error(codes.InvalidArgument, "create job spec is required")
	}
}

func (s *APIServer) GetJobStatus(ctx context.Context, req *vmrpc.GetJobStatusRequest) (*vmrpc.GetJobStatusResponse, error) {
	_, span := tracer.Start(ctx, "rpc.GetJobStatus")
	defer span.End()

	record, err := s.lookupJob(req.GetJobId())
	if err != nil {
		return nil, err
	}
	return record.protoStatus(req.GetIncludeOutput()), nil
}

func (s *APIServer) CancelJob(ctx context.Context, req *vmrpc.CancelJobRequest) (*vmrpc.CancelJobResponse, error) {
	_, span := tracer.Start(ctx, "rpc.CancelJob")
	defer span.End()

	record, err := s.lookupJob(req.GetJobId())
	if err != nil {
		return nil, err
	}
	return &vmrpc.CancelJobResponse{Canceled: record.cancelJob()}, nil
}

func (s *APIServer) StreamJobLogs(req *vmrpc.StreamJobLogsRequest, stream vmrpc.VMService_StreamJobLogsServer) error {
	ctx, span := tracer.Start(stream.Context(), "rpc.StreamJobLogs")
	defer span.End()

	record, err := s.lookupJob(req.GetJobId())
	if err != nil {
		return err
	}

	var sent int
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		chunks, terminal := record.logSnapshot(sent)
		for _, chunk := range chunks {
			if err := stream.Send(&vmrpc.JobLogChunk{
				Seq:   chunk.Seq,
				Chunk: chunk.Chunk,
			}); err != nil {
				return err
			}
			sent++
		}

		if terminal {
			return stream.Send(&vmrpc.JobLogChunk{Terminal: true})
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

func (s *APIServer) StreamGuestEvents(req *vmrpc.StreamGuestEventsRequest, stream vmrpc.VMService_StreamGuestEventsServer) error {
	ctx, span := tracer.Start(stream.Context(), "rpc.StreamGuestEvents")
	defer span.End()

	record, err := s.lookupJob(req.GetJobId())
	if err != nil {
		return err
	}

	var sent int
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		events, terminal := record.guestEventSnapshot(sent)
		for _, event := range events {
			if err := stream.Send(&vmrpc.JobGuestEvent{
				Seq:   event.Seq,
				JobId: req.GetJobId(),
				Kind:  event.Kind,
				Attrs: cloneStringMap(event.Attrs),
			}); err != nil {
				return err
			}
			sent++
		}

		if terminal {
			return stream.Send(&vmrpc.JobGuestEvent{
				JobId:    req.GetJobId(),
				Terminal: true,
			})
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

func (s *APIServer) StreamTelemetry(req *vmrpc.StreamTelemetryRequest, stream vmrpc.VMService_StreamTelemetryServer) error {
	ctx, span := tracer.Start(stream.Context(), "rpc.StreamTelemetry")
	defer span.End()

	subID, ch := s.addTelemetrySubscriber()
	defer s.removeTelemetrySubscriber(subID)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event := <-ch:
			if req.GetJobId() != "" && req.GetJobId() != event.JobID {
				continue
			}

			out := &vmrpc.TelemetryEvent{
				JobId:              event.JobID,
				ReceivedAtUnixNano: uint64(event.ReceivedAtUnix.UnixNano()),
			}
			switch {
			case event.Hello != nil:
				out.Kind = vmrpc.TelemetryFrameKind_TELEMETRY_FRAME_KIND_HELLO
				out.Frame = &vmrpc.TelemetryEvent_Hello{Hello: telemetryHelloToProto(*event.Hello)}
			case event.Sample != nil:
				out.Kind = vmrpc.TelemetryFrameKind_TELEMETRY_FRAME_KIND_SAMPLE
				out.Frame = &vmrpc.TelemetryEvent_Sample{Sample: telemetrySampleToProto(*event.Sample)}
			default:
				continue
			}

			if err := stream.Send(out); err != nil {
				return err
			}
		}
	}
}

func (s *APIServer) GetFleetSnapshot(ctx context.Context, _ *vmrpc.GetFleetSnapshotRequest) (*vmrpc.GetFleetSnapshotResponse, error) {
	_, span := tracer.Start(ctx, "rpc.GetFleetSnapshot")
	defer span.End()

	s.mu.RLock()
	defer s.mu.RUnlock()

	resp := &vmrpc.GetFleetSnapshotResponse{}
	for _, job := range s.jobs {
		if vm, ok := job.fleetVM(); ok {
			out := &vmrpc.FleetVM{
				JobId:              vm.JobID,
				State:              jobStateToProto(vm.State),
				LastUpdateUnixNano: uint64(vm.LastUpdateAt.UnixNano()),
				Hello:              telemetryHelloToProtoValue(vm.Hello),
				LatestSample:       telemetrySampleToProtoValue(vm.LatestSample),
			}
			resp.Vms = append(resp.Vms, out)
		}
	}
	return resp, nil
}

func (s *APIServer) GetCapacity(ctx context.Context, _ *vmrpc.GetCapacityRequest) (*vmrpc.GetCapacityResponse, error) {
	_, span := tracer.Start(ctx, "rpc.GetCapacity")
	defer span.End()

	activeJobs := uint32(s.activeJobCount())
	totalSlots := uint32(totalGuestSlots(s.cfg.GuestPoolCIDR))
	available := uint32(0)
	if totalSlots > activeJobs {
		available = totalSlots - activeJobs
	}

	return &vmrpc.GetCapacityResponse{
		GuestPoolCidr:  s.cfg.GuestPoolCIDR,
		TotalSlots:     totalSlots,
		ActiveJobs:     activeJobs,
		AvailableSlots: available,
		VcpusPerVm:     uint32(s.cfg.VCPUs),
		MemoryMibPerVm: uint32(s.cfg.MemoryMiB),
	}, nil
}

func (s *APIServer) createJobRecord(jobID string, billablePhases []string) (*managedJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.jobs[jobID]; exists {
		return nil, status.Errorf(codes.AlreadyExists, "job %s already exists", jobID)
	}

	record := &managedJob{
		id:             jobID,
		state:          JobStatePending,
		billablePhases: normalizeBillablePhaseSet(billablePhases),
	}
	s.jobs[jobID] = record
	return record, nil
}

func (s *APIServer) lookupJob(jobID string) (*managedJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	record, ok := s.jobs[jobID]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "job %s not found", jobID)
	}
	return record, nil
}

func (s *APIServer) runManagedJob(record *managedJob, runner func(context.Context, RunObserver) (JobResult, error)) {
	ctx, cancel := context.WithCancel(context.Background())
	record.setRunning(cancel)

	observer := &jobObserver{server: s, job: record}
	result, err := runner(ctx, observer)
	record.finish(finalJobState(err, result.ExitCode), &result, err)
}

func (s *APIServer) addTelemetrySubscriber() (uint64, chan TelemetryEvent) {
	id := atomic.AddUint64(&s.nextSubID, 1)
	ch := make(chan TelemetryEvent, 256)
	s.mu.Lock()
	s.telemetrySubs[id] = ch
	s.mu.Unlock()
	return id, ch
}

func (s *APIServer) removeTelemetrySubscriber(id uint64) {
	s.mu.Lock()
	if ch, ok := s.telemetrySubs[id]; ok {
		delete(s.telemetrySubs, id)
		close(ch)
	}
	s.mu.Unlock()
}

func (s *APIServer) broadcastTelemetry(event TelemetryEvent) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, ch := range s.telemetrySubs {
		select {
		case ch <- event:
		default:
		}
	}
}

func (s *APIServer) activeJobCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	for _, job := range s.jobs {
		if state := job.currentState(); state == JobStatePending || state == JobStateRunning {
			count++
		}
	}
	return count
}

func (o *jobObserver) OnGuestLogChunk(_ string, chunk string) {
	o.job.appendLogChunk(chunk)
}

func (o *jobObserver) OnGuestPhaseStart(_ string, phase string) {
	o.job.appendPhaseEvent("phase_started", phase, nil)
}

func (o *jobObserver) OnGuestPhaseEnd(_ string, result PhaseResult) {
	attrs := map[string]string{
		"exit_code":   strconv.Itoa(result.ExitCode),
		"duration_ms": strconv.FormatInt(result.DurationMS, 10),
	}
	o.job.appendPhaseEvent("phase_ended", result.Name, attrs)
}

func (o *jobObserver) OnTelemetryEvent(event TelemetryEvent) {
	o.job.recordTelemetry(event)
	o.server.broadcastTelemetry(event)
}

func (j *managedJob) setRunning(cancel context.CancelFunc) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.state = JobStateRunning
	j.cancel = cancel
}

func (j *managedJob) finish(state JobState, result *JobResult, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.state = state
	j.result = result
	j.err = err
	j.cancel = nil
}

func (j *managedJob) cancelJob() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.cancel == nil || j.state.Terminal() {
		return false
	}
	j.cancel()
	return true
}

func (j *managedJob) appendLogChunk(chunk string) {
	if chunk == "" {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.logTruncated {
		return
	}
	remaining := maxBufferedGuestLogs - j.logBytes
	if remaining <= 0 {
		j.logTruncated = true
		return
	}
	if len(chunk) > remaining {
		j.logTruncated = true
		if remaining <= len(managedJobLogTruncatedMarker) {
			chunk = managedJobLogTruncatedMarker[:remaining]
		} else {
			chunk = chunk[:remaining-len(managedJobLogTruncatedMarker)] + managedJobLogTruncatedMarker
		}
	}
	j.logSeq++
	j.logBytes += len(chunk)
	j.logChunks = append(j.logChunks, jobLogChunk{
		Seq:   j.logSeq,
		Chunk: chunk,
	})
}

func (j *managedJob) appendPhaseEvent(kind, phase string, extra map[string]string) {
	phase = strings.TrimSpace(phase)
	if phase == "" {
		return
	}
	attrs := map[string]string{
		"phase":                   phase,
		"billable":                strconv.FormatBool(j.isBillablePhase(phase)),
		"host_received_unix_nano": strconv.FormatInt(time.Now().UTC().UnixNano(), 10),
	}
	for key, value := range extra {
		attrs[key] = value
	}
	j.appendEvent(kind, attrs)
}

func (j *managedJob) appendEvent(kind string, attrs map[string]string) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.eventSeq++
	j.events = append(j.events, jobGuestEvent{
		Seq:   j.eventSeq,
		Kind:  kind,
		Attrs: cloneStringMap(attrs),
	})
}

func (j *managedJob) isBillablePhase(phase string) bool {
	_, ok := j.billablePhases[strings.TrimSpace(phase)]
	return ok
}

func (j *managedJob) logSnapshot(offset int) ([]jobLogChunk, bool) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	if offset >= len(j.logChunks) {
		return nil, j.state.Terminal()
	}
	chunks := append([]jobLogChunk(nil), j.logChunks[offset:]...)
	return chunks, j.state.Terminal()
}

func (j *managedJob) guestEventSnapshot(offset int) ([]jobGuestEvent, bool) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	if offset >= len(j.events) {
		return nil, j.state.Terminal()
	}
	events := append([]jobGuestEvent(nil), j.events[offset:]...)
	return events, j.state.Terminal()
}

func (j *managedJob) protoStatus(includeOutput bool) *vmrpc.GetJobStatusResponse {
	j.mu.RLock()
	defer j.mu.RUnlock()

	resp := &vmrpc.GetJobStatusResponse{
		JobId:        j.id,
		State:        jobStateToProto(j.state),
		Terminal:     j.state.Terminal(),
		ErrorMessage: errorString(j.err),
	}
	if j.result != nil {
		resp.Result = jobResultToProto(*j.result, includeOutput)
	}
	return resp
}

func (j *managedJob) recordTelemetry(event TelemetryEvent) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.lastUpdate = event.ReceivedAtUnix
	if event.Hello != nil {
		hello := *event.Hello
		j.hello = &hello
	}
	if event.Sample != nil {
		sample := *event.Sample
		j.sample = &sample
	}
}

func (j *managedJob) fleetVM() (FleetVM, bool) {
	j.mu.RLock()
	defer j.mu.RUnlock()

	if j.hello == nil && j.sample == nil {
		return FleetVM{}, false
	}

	var hello *TelemetryHello
	if j.hello != nil {
		value := *j.hello
		hello = &value
	}
	var sample *TelemetrySample
	if j.sample != nil {
		value := *j.sample
		sample = &value
	}

	return FleetVM{
		JobID:        j.id,
		State:        j.state,
		LastUpdateAt: j.lastUpdate,
		Hello:        hello,
		LatestSample: sample,
	}, true
}

func (j *managedJob) currentState() JobState {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.state
}

func telemetryHelloToProtoValue(frame *TelemetryHello) *vmrpc.TelemetryHello {
	if frame == nil {
		return nil
	}
	return telemetryHelloToProto(*frame)
}

func telemetrySampleToProtoValue(frame *TelemetrySample) *vmrpc.TelemetrySample {
	if frame == nil {
		return nil
	}
	return telemetrySampleToProto(*frame)
}

func ensureJobID(jobID string) string {
	if _, err := uuid.Parse(jobID); err == nil {
		return jobID
	}
	return uuid.NewString()
}

func finalJobState(err error, exitCode int) JobState {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return JobStateCanceled
	case err != nil:
		return JobStateFailed
	case exitCode != 0:
		return JobStateFailed
	default:
		return JobStateSucceeded
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

func totalGuestSlots(poolCIDR string) int {
	cfg := normalizeNetworkPoolConfig(NetworkPoolConfig{PoolCIDR: poolCIDR})
	allocator := NewAllocator(cfg)
	_, slots, err := allocator.pool()
	if err != nil {
		return 0
	}
	return slots
}
