package vmorchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	vmrpc "github.com/forge-metal/vm-orchestrator/proto/v1"
	"github.com/forge-metal/vm-orchestrator/vmproto"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type APIServer struct {
	vmrpc.UnimplementedVMServiceServer

	cfg                Config
	repoGoldenStateDir string
	logger             *slog.Logger

	mu            sync.RWMutex
	jobs          map[string]*managedJob
	telemetrySubs map[uint64]chan TelemetryEvent
	nextSubID     uint64
}

type managedJob struct {
	id string

	mu         sync.RWMutex
	state      JobState
	cancel     context.CancelFunc
	result     *JobResult
	err        error
	logSeq     uint64
	logChunks  []jobLogChunk
	eventSeq   uint64
	events     []jobGuestEvent
	hello      *TelemetryHello
	sample     *TelemetrySample
	lastUpdate time.Time
	repoExec   *RepoExecMetadata
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

func NewAPIServer(cfg Config, repoGoldenStateDir string, logger *slog.Logger) *APIServer {
	base := DefaultConfig()
	if cfg.Pool != "" {
		base = cfg
	}
	if repoGoldenStateDir == "" {
		repoGoldenStateDir = DefaultRepoGoldenStateDir
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &APIServer{
		cfg:                base,
		repoGoldenStateDir: repoGoldenStateDir,
		logger:             logger,
		jobs:               make(map[string]*managedJob),
		telemetrySubs:      make(map[uint64]chan TelemetryEvent),
	}
}

func (s *APIServer) CreateJob(ctx context.Context, req *vmrpc.CreateJobRequest) (*vmrpc.CreateJobResponse, error) {
	ctx, span := tracer.Start(ctx, "rpc.CreateJob")
	defer span.End()

	switch spec := req.GetSpec().(type) {
	case *vmrpc.CreateJobRequest_DirectJob:
		cfg := configFromProto(s.cfg, spec.DirectJob.GetRuntimeConfig())
		job := jobConfigFromProto(spec.DirectJob.GetJob())
		job.JobID = ensureJobID(job.JobID)
		record, err := s.createJobRecord(job.JobID)
		if err != nil {
			return nil, err
		}
		span.SetAttributes(attribute.String("job.id", job.JobID), attribute.String("job.kind", "direct"))
		go s.runManagedJob(record, cfg, func(runCtx context.Context, observer RunObserver) (JobResult, error) {
			orch := New(cfg, s.logger)
			return orch.RunObserved(runCtx, job, observer)
		})
		return &vmrpc.CreateJobResponse{
			JobId: job.JobID,
			State: vmrpc.JobState_JOB_STATE_PENDING,
		}, nil
	case *vmrpc.CreateJobRequest_RepoExec:
		cfg := configFromProto(s.cfg, spec.RepoExec.GetRuntimeConfig())
		job := jobConfigFromProto(spec.RepoExec.GetJobTemplate())
		job.JobID = ensureJobID(job.JobID)
		record, err := s.createJobRecord(job.JobID)
		if err != nil {
			return nil, err
		}
		span.SetAttributes(attribute.String("job.id", job.JobID), attribute.String("job.kind", "repo_exec"), attribute.String("repo", spec.RepoExec.GetRepo()))
		go s.runManagedRepoExec(record, cfg, spec.RepoExec)
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

func (s *APIServer) WarmGolden(ctx context.Context, req *vmrpc.WarmGoldenRequest) (*vmrpc.WarmGoldenResponse, error) {
	ctx, span := tracer.Start(ctx, "rpc.WarmGolden")
	defer span.End()

	cfg := configFromProto(s.cfg, req.GetRuntimeConfig())
	job := jobConfigFromProto(req.GetJob())
	job.JobID = ensureJobID(job.JobID)
	record, err := s.createJobRecord(job.JobID)
	if err != nil {
		return nil, err
	}

	span.SetAttributes(attribute.String("job.id", job.JobID), attribute.String("repo", req.GetRepo()))

	record.setRunning(func() {})
	result, runErr := s.runWarmGolden(ctx, record, cfg, req)
	if runErr != nil {
		record.finish(finalJobState(runErr, result.JobResult.ExitCode), &result.JobResult, runErr)
		return &vmrpc.WarmGoldenResponse{
			TargetDataset:               result.TargetDataset,
			PreviousDataset:             result.PreviousDataset,
			Promoted:                    result.Promoted,
			CloneDurationMs:             result.CloneDuration.Milliseconds(),
			SnapshotPromotionDurationMs: result.SnapshotPromotionDuration.Milliseconds(),
			PreviousDestroyDurationMs:   result.PreviousDestroyDuration.Milliseconds(),
			CommitSha:                   result.CommitSHA,
			JobResult:                   jobResultToProto(result.JobResult, true),
			ErrorMessage:                runErr.Error(),
		}, nil
	}
	record.finish(finalJobState(nil, result.JobResult.ExitCode), &result.JobResult, nil)

	return &vmrpc.WarmGoldenResponse{
		TargetDataset:               result.TargetDataset,
		PreviousDataset:             result.PreviousDataset,
		Promoted:                    result.Promoted,
		CloneDurationMs:             result.CloneDuration.Milliseconds(),
		SnapshotPromotionDurationMs: result.SnapshotPromotionDuration.Milliseconds(),
		PreviousDestroyDurationMs:   result.PreviousDestroyDuration.Milliseconds(),
		CommitSha:                   result.CommitSHA,
		JobResult:                   jobResultToProto(result.JobResult, true),
		ErrorMessage:                "",
	}, nil
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

func (s *APIServer) createJobRecord(jobID string) (*managedJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.jobs[jobID]; exists {
		return nil, status.Errorf(codes.AlreadyExists, "job %s already exists", jobID)
	}

	record := &managedJob{
		id:    jobID,
		state: JobStatePending,
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

func (s *APIServer) runManagedJob(record *managedJob, cfg Config, runner func(context.Context, RunObserver) (JobResult, error)) {
	ctx, cancel := context.WithCancel(context.Background())
	record.setRunning(cancel)

	observer := &jobObserver{server: s, job: record}
	result, err := runner(ctx, observer)
	record.finish(finalJobState(err, result.ExitCode), &result, err)
}

func (s *APIServer) runManagedRepoExec(record *managedJob, cfg Config, spec *vmrpc.RepoExecSpec) {
	ctx, cancel := context.WithCancel(context.Background())
	record.setRunning(cancel)

	result, meta, err := s.runRepoExec(ctx, record, cfg, spec)
	record.setRepoExec(meta)
	record.finish(finalJobState(err, result.ExitCode), &result, err)
}

func (s *APIServer) runRepoExec(ctx context.Context, record *managedJob, cfg Config, spec *vmrpc.RepoExecSpec) (JobResult, *RepoExecMetadata, error) {
	repoKey := sanitizeRepoKey(spec.GetRepo())
	repoDataset, err := activeRepoGoldenDataset(s.repoGoldenStateDir, repoKey)
	if err != nil {
		return JobResult{}, nil, err
	}
	if repoDataset == "" {
		return JobResult{}, nil, fmt.Errorf("repo golden for %s does not exist; run warm first", spec.GetRepo())
	}

	snapshot := repoDataset + "@ready"
	exists, err := zfsSnapshotExists(ctx, snapshot)
	if err != nil {
		return JobResult{}, nil, err
	}
	if !exists {
		return JobResult{}, nil, fmt.Errorf("repo golden %s does not exist; run warm first", snapshot)
	}

	job := jobConfigFromProto(spec.GetJobTemplate())
	job.JobID = ensureJobID(job.JobID)
	repoJob, err := buildInVMRepoExecJob(job, spec.GetRepoUrl(), spec.GetRef(), spec.GetLockfileRelPath(), cfg.HostServiceIP, cfg.HostServicePort)
	if err != nil {
		return JobResult{}, nil, err
	}

	jobDataset := fmt.Sprintf("%s/%s/%s", cfg.Pool, cfg.CIDataset, job.JobID)
	cloneStart := time.Now()
	if err := (DirectPrivOps{}).ZFSClone(ctx, snapshot, jobDataset, job.JobID); err != nil {
		return JobResult{}, nil, err
	}
	cloneDuration := time.Since(cloneStart)

	observer := &jobObserver{server: s, job: record}
	orch := New(cfg, s.logger)
	result, err := orch.RunDatasetObserved(ctx, repoJob, jobDataset, true, observer)
	manifest := result.RepoManifest
	if err == nil {
		err = validateRepoExecManifest(spec, manifest)
	}
	commitSHA := ""
	installNeeded := true
	if manifest != nil {
		commitSHA = manifest.ResolvedCommitSHA
		installNeeded = manifest.InstallNeeded
	}
	meta := &RepoExecMetadata{
		Repo:           spec.GetRepo(),
		RepoURL:        spec.GetRepoUrl(),
		Ref:            spec.GetRef(),
		GoldenSnapshot: snapshot,
		CloneDuration:  cloneDuration,
		InstallNeeded:  installNeeded,
		CommitSHA:      commitSHA,
	}
	return result, meta, err
}

func (s *APIServer) runWarmGolden(ctx context.Context, record *managedJob, cfg Config, req *vmrpc.WarmGoldenRequest) (WarmGoldenResult, error) {
	repoKey := sanitizeRepoKey(req.GetRepo())
	targetDataset := nextRepoGoldenDataset(cfg, repoKey, time.Now())
	previousDataset, err := activeRepoGoldenDataset(s.repoGoldenStateDir, repoKey)
	if err != nil {
		return WarmGoldenResult{}, err
	}
	if err := ensureDataset(ctx, repoGoldensRootDataset(cfg)); err != nil {
		return WarmGoldenResult{}, err
	}

	job := jobConfigFromProto(req.GetJob())
	job.JobID = ensureJobID(job.JobID)

	cloneStart := time.Now()
	if err := (DirectPrivOps{}).ZFSClone(ctx, baseGoldenSnapshot(cfg), targetDataset, job.JobID); err != nil {
		return WarmGoldenResult{}, err
	}
	cloneDuration := time.Since(cloneStart)

	cleanupTargetDataset := true
	defer func() {
		if !cleanupTargetDataset {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), zfsTimeout)
		defer cancel()
		_ = destroyDatasetRecursive(cleanupCtx, targetDataset)
	}()

	// The warm process runs entirely inside the Firecracker VM: git fetch,
	// dependency install, and lockfile hash write all happen in the guest.
	// The host only snapshots blocks after vm-init returns a typed manifest.
	warmJob, err := buildInVMWarmJob(job, req.GetRepoUrl(), req.GetDefaultBranch(), req.GetLockfileRelPath(), cfg.HostServiceIP, cfg.HostServicePort)
	if err != nil {
		return WarmGoldenResult{}, err
	}
	s.logger.Info("warm golden: in-VM warm (guest manifest gate)",
		"repo", req.GetRepo(),
		"dataset", targetDataset,
	)

	observer := &jobObserver{server: s, job: record}
	orch := New(cfg, s.logger)
	startedResult, runErr := orch.RunDatasetObserved(ctx, warmJob, targetDataset, false, observer)
	manifest := startedResult.RepoManifest
	commitSHA := ""
	if manifest != nil {
		commitSHA = manifest.ResolvedCommitSHA
	}
	if runErr != nil {
		return WarmGoldenResult{
			TargetDataset:   targetDataset,
			PreviousDataset: previousDataset,
			CloneDuration:   cloneDuration,
			CommitSHA:       commitSHA,
			JobResult:       startedResult,
		}, runErr
	}
	if startedResult.ExitCode != 0 {
		return WarmGoldenResult{
			TargetDataset:   targetDataset,
			PreviousDataset: previousDataset,
			CloneDuration:   cloneDuration,
			CommitSHA:       commitSHA,
			JobResult:       startedResult,
		}, fmt.Errorf("warm run exited with code %d", startedResult.ExitCode)
	}
	if err := validateWarmPromotionManifest(req, startedResult); err != nil {
		return WarmGoldenResult{
			TargetDataset:   targetDataset,
			PreviousDataset: previousDataset,
			CloneDuration:   cloneDuration,
			CommitSHA:       commitSHA,
			JobResult:       startedResult,
		}, err
	}

	snapshotPromotionStart := time.Now()
	if err := replaceReadySnapshot(ctx, targetDataset); err != nil {
		return WarmGoldenResult{
			TargetDataset:             targetDataset,
			PreviousDataset:           previousDataset,
			CloneDuration:             cloneDuration,
			SnapshotPromotionDuration: time.Since(snapshotPromotionStart),
			CommitSHA:                 commitSHA,
			JobResult:                 startedResult,
		}, err
	}
	snapshotPromotionDuration := time.Since(snapshotPromotionStart)
	if err := writeActiveRepoGoldenDataset(s.repoGoldenStateDir, repoKey, targetDataset); err != nil {
		return WarmGoldenResult{}, err
	}
	cleanupTargetDataset = false

	var previousDestroyDuration time.Duration
	if previousDataset != "" && previousDataset != targetDataset {
		start := time.Now()
		if err := destroyDatasetRecursive(ctx, previousDataset); err == nil {
			previousDestroyDuration = time.Since(start)
		} else {
			s.logger.Warn("destroy previous repo golden failed", "repo", req.GetRepo(), "dataset", previousDataset, "err", err)
		}
	}

	return WarmGoldenResult{
		TargetDataset:             targetDataset,
		PreviousDataset:           previousDataset,
		Promoted:                  true,
		CloneDuration:             cloneDuration,
		SnapshotPromotionDuration: snapshotPromotionDuration,
		PreviousDestroyDuration:   previousDestroyDuration,
		CommitSHA:                 commitSHA,
		JobResult:                 startedResult,
	}, nil
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

func (o *jobObserver) OnGuestEvent(_ string, event vmproto.GuestEvent) {
	o.job.appendGuestEvent(event)
}

func (o *jobObserver) OnGuestPhaseStart(_ string, _ string) {}

func (o *jobObserver) OnGuestPhaseEnd(_ string, _ PhaseResult) {}

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

func (j *managedJob) setRepoExec(meta *RepoExecMetadata) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.repoExec = meta
}

func validateWarmPromotionManifest(req *vmrpc.WarmGoldenRequest, result JobResult) error {
	if result.ForcedShutdown {
		return fmt.Errorf("warm promotion blocked: VM required forced shutdown")
	}
	manifest := result.RepoManifest
	if manifest == nil {
		return fmt.Errorf("warm promotion blocked: missing repo supervisor manifest")
	}
	if manifest.Kind != vmproto.RepoOperationWarm {
		return fmt.Errorf("warm promotion blocked: manifest kind %q is not %q", manifest.Kind, vmproto.RepoOperationWarm)
	}
	if ref := strings.TrimSpace(req.GetDefaultBranch()); ref != "" && strings.TrimSpace(manifest.RequestedRef) != ref {
		return fmt.Errorf("warm promotion blocked: manifest ref %q is not requested ref %q", manifest.RequestedRef, ref)
	}
	if !looksLikeGitObjectID(manifest.ResolvedCommitSHA) {
		return fmt.Errorf("warm promotion blocked: invalid resolved commit %q", manifest.ResolvedCommitSHA)
	}
	if lockfileRelPath := strings.TrimSpace(req.GetLockfileRelPath()); lockfileRelPath != "" {
		if strings.TrimSpace(manifest.LockfileRelPath) != lockfileRelPath {
			return fmt.Errorf("warm promotion blocked: manifest lockfile %q is not requested lockfile %q", manifest.LockfileRelPath, lockfileRelPath)
		}
		lockfileSHA := strings.TrimSpace(manifest.LockfileSHA256)
		if lockfileSHA == "" {
			return fmt.Errorf("warm promotion blocked: missing lockfile hash for %s", lockfileRelPath)
		}
		if !looksLikeSHA256Hex(lockfileSHA) {
			return fmt.Errorf("warm promotion blocked: invalid lockfile hash for %s", lockfileRelPath)
		}
	}
	return nil
}

func validateRepoExecManifest(spec *vmrpc.RepoExecSpec, manifest *RepoManifest) error {
	if spec == nil {
		spec = &vmrpc.RepoExecSpec{}
	}
	if manifest == nil {
		return fmt.Errorf("repo exec completed without supervisor manifest")
	}
	if manifest.Kind != vmproto.RepoOperationExec {
		return fmt.Errorf("repo exec supervisor manifest kind %q is not %q", manifest.Kind, vmproto.RepoOperationExec)
	}
	if ref := strings.TrimSpace(spec.GetRef()); ref != "" && strings.TrimSpace(manifest.RequestedRef) != ref {
		return fmt.Errorf("repo exec supervisor manifest ref %q is not requested ref %q", manifest.RequestedRef, ref)
	}
	if !looksLikeGitObjectID(manifest.ResolvedCommitSHA) {
		return fmt.Errorf("repo exec supervisor manifest invalid resolved commit %q", manifest.ResolvedCommitSHA)
	}
	if lockfileRelPath := strings.TrimSpace(spec.GetLockfileRelPath()); lockfileRelPath != "" {
		if strings.TrimSpace(manifest.LockfileRelPath) != lockfileRelPath {
			return fmt.Errorf("repo exec supervisor manifest lockfile %q is not requested lockfile %q", manifest.LockfileRelPath, lockfileRelPath)
		}
	}
	if strings.TrimSpace(manifest.LockfileSHA256) != "" && !looksLikeSHA256Hex(manifest.LockfileSHA256) {
		return fmt.Errorf("repo exec supervisor manifest invalid lockfile hash")
	}
	if strings.TrimSpace(manifest.PreviousLockfileSHA256) != "" && !looksLikeSHA256Hex(manifest.PreviousLockfileSHA256) {
		return fmt.Errorf("repo exec supervisor manifest invalid previous lockfile hash")
	}
	return nil
}

func looksLikeGitObjectID(value string) bool {
	value = strings.TrimSpace(value)
	switch len(value) {
	case 40, 64:
	default:
		return false
	}
	for _, ch := range value {
		switch {
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'f':
		case ch >= 'A' && ch <= 'F':
		default:
			return false
		}
	}
	return true
}

func looksLikeSHA256Hex(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 64 {
		return false
	}
	for _, ch := range value {
		switch {
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'f':
		case ch >= 'A' && ch <= 'F':
		default:
			return false
		}
	}
	return true
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
	j.logSeq++
	j.logChunks = append(j.logChunks, jobLogChunk{
		Seq:   j.logSeq,
		Chunk: chunk,
	})
}

func (j *managedJob) appendGuestEvent(event vmproto.GuestEvent) {
	if strings.TrimSpace(event.Kind) == "" {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.eventSeq++
	j.events = append(j.events, jobGuestEvent{
		Seq:   j.eventSeq,
		Kind:  strings.TrimSpace(event.Kind),
		Attrs: cloneStringMap(event.Attrs),
	})
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
	if j.repoExec != nil {
		resp.RepoExec = &vmrpc.RepoExecMetadata{
			Repo:            j.repoExec.Repo,
			RepoUrl:         j.repoExec.RepoURL,
			Ref:             j.repoExec.Ref,
			GoldenSnapshot:  j.repoExec.GoldenSnapshot,
			CloneDurationMs: j.repoExec.CloneDuration.Milliseconds(),
			InstallNeeded:   j.repoExec.InstallNeeded,
			CommitSha:       j.repoExec.CommitSHA,
		}
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

func totalGuestSlots(poolCIDR string) int {
	cfg := normalizeNetworkPoolConfig(NetworkPoolConfig{PoolCIDR: poolCIDR})
	allocator := NewAllocator(cfg)
	_, slots, err := allocator.pool()
	if err != nil {
		return 0
	}
	return slots
}
