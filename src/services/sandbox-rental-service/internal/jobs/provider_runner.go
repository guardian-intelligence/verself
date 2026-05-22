package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/verself/sandbox-rental-service/internal/store"
	"go.opentelemetry.io/otel/attribute"
)

type ProviderRunnerJobObservation struct {
	Provider               string
	ProviderInstallationID int64
	ProviderRepositoryID   int64
	RepositoryFullName     string
	ProviderRunID          int64
	ProviderRunAttempt     int64
	ProviderJobID          int64
	JobName                string
	HeadSHA                string
	HeadBranch             string
	WorkflowName           string
	Status                 string
	Conclusion             string
	Labels                 []string
	RunnerID               int64
	RunnerName             string
	StartedAt              time.Time
	CompletedAt            time.Time
	DeliveryID             string
}

type ProviderWorkflowRunObservation struct {
	Provider               string
	ProviderInstallationID int64
	ProviderRepositoryID   int64
	ProviderRunID          int64
	ProviderRunAttempt     int64
	RepositoryFullName     string
	EventName              string
	HeadSHA                string
	HeadBranch             string
	HeadRepositoryFullName string
	BaseSHA                string
	BaseBranch             string
	WorkflowPath           string
	PullRequestNumber      int64
	CommitCount            int64
}

type ProviderCacheManifest struct {
	SourceKind string
	SourcePath string
	SourceSHA  string
	Content    []byte
}

type ProviderRunnerJobSubmission struct {
	Observation      ProviderRunnerJobObservation
	WorkflowRun      *ProviderWorkflowRunObservation
	CacheManifest    *ProviderCacheManifest
	RunnerName       string
	RunnerID         int64
	BootstrapKind    string
	BootstrapPayload string
}

type ProviderRunnerJobSubmissionResult struct {
	AllocationID uuid.UUID
	ExecutionID  uuid.UUID
	AttemptID    uuid.UUID
	RunnerName   string
	RunnerID     int64
	Created      bool
}

type ProviderRunnerJobObservationResult struct {
	State        string
	AllocationID uuid.UUID
	ExecutionID  uuid.UUID
	AttemptID    uuid.UUID
}

func (s *Service) SubmitProviderRunnerJob(ctx context.Context, req ProviderRunnerJobSubmission) (ProviderRunnerJobSubmissionResult, error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.runner_job.submit")
	defer span.End()
	if s == nil || s.PGX == nil || s.Scheduler == nil {
		return ProviderRunnerJobSubmissionResult{}, ErrRunnerUnavailable
	}
	req.Observation.Provider = strings.TrimSpace(req.Observation.Provider)
	req.RunnerName = strings.TrimSpace(req.RunnerName)
	if req.Observation.Provider == "" || req.Observation.ProviderJobID <= 0 || req.Observation.ProviderRepositoryID <= 0 {
		return ProviderRunnerJobSubmissionResult{}, fmt.Errorf("%w: runner job provider identity is required", ErrRunnerUnavailable)
	}
	if strings.TrimSpace(req.BootstrapKind) == "" || strings.TrimSpace(req.BootstrapPayload) == "" {
		return ProviderRunnerJobSubmissionResult{}, fmt.Errorf("%w: runner bootstrap payload is required", ErrRunnerUnavailable)
	}
	if req.RunnerName == "" {
		return ProviderRunnerJobSubmissionResult{}, fmt.Errorf("%w: runner name is required", ErrRunnerUnavailable)
	}
	if err := s.upsertProviderRunnerJob(ctx, req.Observation); err != nil {
		return ProviderRunnerJobSubmissionResult{}, err
	}
	if req.CacheManifest != nil {
		if err := s.upsertProviderCacheManifest(ctx, req.Observation.Provider, req.Observation.ProviderJobID, *req.CacheManifest); err != nil {
			return ProviderRunnerJobSubmissionResult{}, err
		}
	}
	if req.WorkflowRun != nil {
		if err := s.ObserveProviderWorkflowRun(ctx, *req.WorkflowRun); err != nil {
			return ProviderRunnerJobSubmissionResult{}, err
		}
	}
	existing, err := s.activeAllocationForJob(ctx, req.Observation.Provider, req.Observation.ProviderJobID)
	if err != nil {
		return ProviderRunnerJobSubmissionResult{}, err
	}
	if existing != uuid.Nil {
		row, err := s.storeQueries().GetRunnerAllocationSubmission(ctx, store.GetRunnerAllocationSubmissionParams{AllocationID: existing})
		if err != nil {
			return ProviderRunnerJobSubmissionResult{}, err
		}
		if row.ExecutionID == nil || *row.ExecutionID == uuid.Nil || row.AttemptID == nil || *row.AttemptID == uuid.Nil {
			return ProviderRunnerJobSubmissionResult{}, fmt.Errorf("%w: runner allocation %s is not submitted yet", ErrRunnerUnavailable, existing)
		}
		return ProviderRunnerJobSubmissionResult{AllocationID: existing, ExecutionID: *row.ExecutionID, AttemptID: *row.AttemptID, RunnerName: row.RunnerName, RunnerID: row.ProviderRunnerID, Created: false}, nil
	}

	job, err := s.storeQueries().GetProviderQueuedJobContext(ctx, store.GetProviderQueuedJobContextParams{
		Provider:      req.Observation.Provider,
		ProviderJobID: req.Observation.ProviderJobID,
		Status:        req.Observation.Status,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProviderRunnerJobSubmissionResult{}, fmt.Errorf("%w: queued runner job not found", ErrRunnerUnavailable)
		}
		return ProviderRunnerJobSubmissionResult{}, err
	}
	var labels []string
	_ = json.Unmarshal(job.LabelsJson, &labels)
	runnerClass, resources, productID, err := s.runnerClassForLabels(ctx, labels)
	if err != nil {
		return ProviderRunnerJobSubmissionResult{}, err
	}
	if runnerClass == "" {
		return ProviderRunnerJobSubmissionResult{}, ErrRunnerClassMissing
	}
	if err := s.ensureOrgRuntimeForRunnerClass(ctx, orgIDFromDB(job.OrgID), productID, runnerClass); err != nil {
		return ProviderRunnerJobSubmissionResult{}, err
	}

	allocationID := uuid.New()
	now := time.Now().UTC()
	rows, err := s.storeQueries().InsertProviderRunnerAllocation(ctx, store.InsertProviderRunnerAllocationParams{
		AllocationID:              allocationID,
		Provider:                  req.Observation.Provider,
		ProviderInstallationID:    job.ProviderInstallationID,
		ProviderRepositoryID:      job.ProviderRepositoryID,
		RunnerClass:               runnerClass,
		RunnerName:                req.RunnerName,
		ProviderRunnerID:          req.RunnerID,
		State:                     "jit_created",
		RequestedForProviderJobID: job.ProviderJobID,
		AllocateBy:                pgTime(now.Add(runnerAllocateDeadline)),
		JitBy:                     pgTime(now.Add(runnerBootstrapDeadline)),
		VmSubmittedBy:             pgTime(now.Add(runnerSubmitDeadline)),
		RunnerListeningBy:         pgTime(now.Add(runnerListeningDeadline)),
		AssignmentBy:              pgTime(now.Add(runnerAssignmentDeadline)),
		VmExitBy:                  pgTime(now.Add(3 * time.Hour)),
		CleanupBy:                 pgTime(now.Add(3 * time.Hour)),
		CreatedAt:                 pgTime(now),
	})
	if err != nil {
		return ProviderRunnerJobSubmissionResult{}, err
	}
	if rows == 0 {
		return ProviderRunnerJobSubmissionResult{}, fmt.Errorf("%w: runner allocation insert conflicted", ErrRunnerUnavailable)
	}
	executionID, attemptID, err := s.Submit(WithCorrelationID(ctx, CorrelationIDFromContext(ctx)), orgIDFromDB(job.OrgID), req.Observation.Provider+"-integration:"+strconv.FormatInt(req.Observation.ProviderInstallationID, 10), SubmitRequest{
		Kind:                   KindDirect,
		SourceKind:             sourceKindForProvider(req.Observation.Provider),
		WorkloadKind:           WorkloadKindRunner,
		SourceRef:              job.RepositoryFullName,
		RunnerClass:            runnerClass,
		ExternalProvider:       req.Observation.Provider,
		ExternalTaskID:         strconv.FormatInt(req.Observation.ProviderJobID, 10),
		Provider:               req.Observation.Provider,
		ProductID:              productID,
		IdempotencyKey:         req.Observation.Provider + "-runner:" + allocationID.String(),
		RunCommand:             runnerCommandForProvider(req.Observation.Provider),
		MaxWallSeconds:         uint64((3 * time.Hour).Seconds()),
		Resources:              resources,
		RunnerAllocationID:     allocationID,
		RunnerBootstrapKind:    req.BootstrapKind,
		RunnerBootstrapPayload: req.BootstrapPayload,
	})
	if err != nil {
		_ = s.storeQueries().SetRunnerAllocationState(ctx, store.SetRunnerAllocationStateParams{
			State:         "failed",
			FailureReason: "execution_submit_failed",
			UpdatedAt:     pgTime(time.Now().UTC()),
			Provider:      req.Observation.Provider,
			AllocationID:  allocationID,
		})
		return ProviderRunnerJobSubmissionResult{}, err
	}
	span.SetAttributes(
		attribute.String("runner.provider", req.Observation.Provider),
		attribute.Int64("runner.provider_job_id", req.Observation.ProviderJobID),
		attribute.String("runner.allocation_id", allocationID.String()),
		attribute.String("execution.id", executionID.String()),
		attribute.String("attempt.id", attemptID.String()),
	)
	return ProviderRunnerJobSubmissionResult{AllocationID: allocationID, ExecutionID: executionID, AttemptID: attemptID, RunnerName: req.RunnerName, RunnerID: req.RunnerID, Created: true}, nil
}

func (s *Service) upsertProviderCacheManifest(ctx context.Context, provider string, providerJobID int64, manifest ProviderCacheManifest) error {
	provider = strings.TrimSpace(provider)
	manifest.SourceKind = strings.TrimSpace(manifest.SourceKind)
	manifest.SourcePath = strings.TrimSpace(manifest.SourcePath)
	manifest.SourceSHA = strings.TrimSpace(manifest.SourceSHA)
	if provider == "" || providerJobID <= 0 || manifest.SourceKind == "" || manifest.SourcePath == "" || manifest.SourceSHA == "" || len(manifest.Content) == 0 {
		return fmt.Errorf("%w: cache manifest identity is required", ErrRunnerUnavailable)
	}
	sum := sha256.Sum256(manifest.Content)
	return s.storeQueries().UpsertRunnerJobCacheManifest(ctx, store.UpsertRunnerJobCacheManifestParams{
		Provider:      provider,
		ProviderJobID: providerJobID,
		SourceKind:    manifest.SourceKind,
		SourcePath:    manifest.SourcePath,
		SourceSha:     manifest.SourceSHA,
		ContentSha256: hex.EncodeToString(sum[:]),
		ContentBytes:  manifest.Content,
		UpdatedAt:     pgTime(time.Now().UTC()),
	})
}

func (s *Service) ObserveProviderRunnerJob(ctx context.Context, obs ProviderRunnerJobObservation, workflow *ProviderWorkflowRunObservation) (ProviderRunnerJobObservationResult, error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.runner_job.observe")
	defer span.End()
	if s == nil || s.PGX == nil {
		return ProviderRunnerJobObservationResult{}, ErrRunnerUnavailable
	}
	if err := s.upsertProviderRunnerJob(ctx, obs); err != nil {
		return ProviderRunnerJobObservationResult{}, err
	}
	if workflow != nil {
		if err := s.ObserveProviderWorkflowRun(ctx, *workflow); err != nil {
			return ProviderRunnerJobObservationResult{}, err
		}
	}
	result, err := s.bindProviderRunnerJob(ctx, obs.Provider, obs.ProviderJobID)
	if err != nil {
		return ProviderRunnerJobObservationResult{}, err
	}
	return result, nil
}

func (s *Service) ObserveProviderWorkflowRun(ctx context.Context, obs ProviderWorkflowRunObservation) error {
	if s == nil || s.PGX == nil {
		return ErrRunnerUnavailable
	}
	obs.Provider = strings.TrimSpace(obs.Provider)
	if obs.Provider != RunnerProviderGitHub {
		return nil
	}
	if obs.ProviderRepositoryID <= 0 || obs.ProviderRunID <= 0 {
		return fmt.Errorf("%w: workflow run provider identity is required", ErrRunnerUnavailable)
	}
	return s.storeQueries().UpsertProviderWorkflowRun(ctx, store.UpsertProviderWorkflowRunParams{
		Provider:               obs.Provider,
		ProviderInstallationID: obs.ProviderInstallationID,
		ProviderRepositoryID:   obs.ProviderRepositoryID,
		ProviderRunID:          obs.ProviderRunID,
		ProviderRunAttempt:     obs.ProviderRunAttempt,
		RepositoryFullName:     obs.RepositoryFullName,
		EventName:              obs.EventName,
		HeadSha:                obs.HeadSHA,
		HeadBranch:             obs.HeadBranch,
		HeadRepositoryFullName: obs.HeadRepositoryFullName,
		BaseSha:                obs.BaseSHA,
		BaseBranch:             obs.BaseBranch,
		PullRequestNumber:      obs.PullRequestNumber,
		WorkflowPath:           obs.WorkflowPath,
		CommitCount:            obs.CommitCount,
		UpdatedAt:              pgTime(time.Now().UTC()),
	})
}

func (s *Service) upsertProviderRunnerJob(ctx context.Context, obs ProviderRunnerJobObservation) error {
	labels, err := json.Marshal(obs.Labels)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := s.storeQueries().UpsertProviderRunnerJob(ctx, store.UpsertProviderRunnerJobParams{
		Provider:               strings.TrimSpace(obs.Provider),
		ProviderJobID:          obs.ProviderJobID,
		ProviderInstallationID: obs.ProviderInstallationID,
		ProviderRepositoryID:   obs.ProviderRepositoryID,
		RepositoryFullName:     strings.TrimSpace(obs.RepositoryFullName),
		ProviderRunID:          obs.ProviderRunID,
		ProviderRunAttempt:     obs.ProviderRunAttempt,
		JobName:                strings.TrimSpace(obs.JobName),
		HeadSha:                strings.TrimSpace(obs.HeadSHA),
		HeadBranch:             strings.TrimSpace(obs.HeadBranch),
		WorkflowName:           strings.TrimSpace(obs.WorkflowName),
		Status:                 strings.TrimSpace(obs.Status),
		Conclusion:             strings.TrimSpace(obs.Conclusion),
		LabelsJson:             labels,
		RunnerID:               obs.RunnerID,
		RunnerName:             strings.TrimSpace(obs.RunnerName),
		StartedAt:              pgOptionalTime(obs.StartedAt),
		CompletedAt:            pgOptionalTime(obs.CompletedAt),
		LastWebhookDelivery:    strings.TrimSpace(obs.DeliveryID),
		UpdatedAt:              pgTime(now),
	}); err != nil {
		return err
	}
	if obs.Provider == RunnerProviderGitHub && isActiveFlightStatus(obs.Status) {
		orgID, err := projectFlightJob(ctx, s.storeQueries(), obs, now)
		if err != nil {
			return err
		}
		if orgID != "" {
			go s.refreshFlightBaseline(context.WithoutCancel(ctx), obs.ProviderJobID, orgID, obs.RepositoryFullName, obs.WorkflowName, obs.JobName)
		}
	}
	return nil
}

func (s *Service) bindProviderRunnerJob(ctx context.Context, provider string, providerJobID int64) (ProviderRunnerJobObservationResult, error) {
	job, err := s.storeQueries().GetRunnerJobForBinding(ctx, store.GetRunnerJobForBindingParams{Provider: provider, ProviderJobID: providerJobID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProviderRunnerJobObservationResult{State: "unmatched"}, nil
		}
		return ProviderRunnerJobObservationResult{}, err
	}
	if job.RunnerID == 0 && strings.TrimSpace(job.RunnerName) == "" {
		return ProviderRunnerJobObservationResult{State: "no_runner_identity"}, nil
	}
	bindingAllocation, err := s.storeQueries().FindAllocationForRunner(ctx, store.FindAllocationForRunnerParams{
		Provider:         provider,
		ProviderRunnerID: job.RunnerID,
		RunnerName:       job.RunnerName,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ProviderRunnerJobObservationResult{State: "unmatched"}, nil
		}
		return ProviderRunnerJobObservationResult{}, err
	}
	allocationID := bindingAllocation.AllocationID
	now := time.Now().UTC()
	tx, err := s.PGX.Begin(ctx)
	if err != nil {
		return ProviderRunnerJobObservationResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := store.New(tx)
	_, err = qtx.InsertRunnerJobBinding(ctx, store.InsertRunnerJobBindingParams{
		BindingID:        uuid.New(),
		AllocationID:     allocationID,
		Provider:         provider,
		ProviderJobID:    providerJobID,
		ProviderRunnerID: job.RunnerID,
		RunnerName:       job.RunnerName,
		BoundAt:          pgTime(now),
	})
	if err != nil {
		return ProviderRunnerJobObservationResult{}, err
	}
	state := "assigned"
	if job.Status == "completed" {
		state = "job_completed"
	}
	if err := qtx.UpdateRunnerAllocationAssignment(ctx, store.UpdateRunnerAllocationAssignmentParams{
		State:        state,
		UpdatedAt:    pgTime(now),
		CleanupBy:    pgTime(now.Add(30 * time.Minute)),
		Provider:     provider,
		AllocationID: allocationID,
	}); err != nil {
		return ProviderRunnerJobObservationResult{}, err
	}
	if err := qtx.UpdateRunnerExecutionExternalTaskID(ctx, store.UpdateRunnerExecutionExternalTaskIDParams{
		ExternalTaskID: strconv.FormatInt(providerJobID, 10),
		UpdatedAt:      pgTime(now),
		AllocationID:   allocationID,
	}); err != nil {
		return ProviderRunnerJobObservationResult{}, err
	}
	submission, err := qtx.GetRunnerAllocationSubmission(ctx, store.GetRunnerAllocationSubmissionParams{AllocationID: allocationID})
	if err != nil {
		return ProviderRunnerJobObservationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProviderRunnerJobObservationResult{}, err
	}
	if job.Status == "completed" && s.Scheduler != nil {
		_, _ = s.Scheduler.EnqueueRunnerCleanup(ctx, schedulerCleanupRequest(ctx, allocationID))
	}
	return ProviderRunnerJobObservationResult{
		State:        state,
		AllocationID: allocationID,
		ExecutionID:  uuidFromPtr(submission.ExecutionID),
		AttemptID:    uuidFromPtr(submission.AttemptID),
	}, nil
}

func uuidFromPtr(value *uuid.UUID) uuid.UUID {
	if value == nil {
		return uuid.Nil
	}
	return *value
}

func (s *Service) runnerClassForLabels(ctx context.Context, labels []string) (string, VMResources, string, error) {
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		classRec, ok, err := s.runnerClassResources(ctx, label)
		if err != nil {
			return "", VMResources{}, "", err
		}
		if !ok {
			continue
		}
		return label, classRec.Resources, classRec.ProductID, nil
	}
	return "", VMResources{}, "", nil
}

func (s *Service) activeAllocationForJob(ctx context.Context, provider string, providerJobID int64) (uuid.UUID, error) {
	allocationID, err := s.storeQueries().GetActiveAllocationForRunnerJob(ctx, store.GetActiveAllocationForRunnerJobParams{
		Provider:      provider,
		ProviderJobID: providerJobID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return allocationID, err
}

func sourceKindForProvider(provider string) string {
	switch provider {
	case RunnerProviderGitHub:
		return SourceKindGitHubAction
	default:
		return "provider_actions"
	}
}

func runnerCommandForProvider(provider string) string {
	switch provider {
	case RunnerProviderGitHub:
		return githubRunnerCommand()
	default:
		return defaultRunCommand
	}
}
