package api

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/verself/sandbox-rental-service/internal/internalcontractapi"
	"github.com/verself/sandbox-rental-service/internal/jobs"
	workloadauth "github.com/verself/service-runtime/workload"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var internalAPITracer = otel.Tracer("sandbox-rental-service/internal/api")

const maxRunnerCacheManifestBytes = 64 << 10

func RegisterInternalRoutes(api huma.API, svc *jobs.Service) {
	registerInternalSandboxContractRoute(api, internalcontractapi.InternalRegisterRunnerRepository, "Register a repository with the runner product", internalRegisterRunnerRepository(svc))
	registerInternalSandboxContractRoute(api, internalcontractapi.InternalSubmitRunnerJob, "Submit a provider runner job", internalSubmitRunnerJob(svc))
	registerInternalSandboxContractRoute(api, internalcontractapi.InternalGetRunnerAllocation, "Get runner allocation status", internalGetRunnerAllocation(svc))
	registerInternalSandboxContractRoute(api, internalcontractapi.InternalObserveRunnerJob, "Observe a provider runner job", internalObserveRunnerJob(svc))
	registerInternalSandboxContractRoute(api, internalcontractapi.InternalObserveRunnerWorkflowRun, "Observe a provider workflow run", internalObserveRunnerWorkflowRun(svc))
	registerInternalSandboxContractRoute(api, internalcontractapi.InternalRequestGoldenSnapshotBarrier, "Request a golden snapshot barrier", internalRequestGoldenSnapshotBarrier(svc))
}

func internalRegisterRunnerRepository(svc *jobs.Service) func(context.Context, *internalcontractapi.InternalRegisterRunnerRepositoryInput) (*internalcontractapi.InternalRegisterRunnerRepositoryOutput, error) {
	return func(ctx context.Context, input *internalcontractapi.InternalRegisterRunnerRepositoryInput) (_ *internalcontractapi.InternalRegisterRunnerRepositoryOutput, err error) {
		ctx, span := internalAPITracer.Start(ctx, "sandbox-rental.runner_repository.register")
		defer func() {
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
			span.End()
		}()
		peerID, ok := workloadauth.PeerIDFromContext(ctx)
		if !ok {
			return nil, unauthorized(ctx)
		}
		if svc == nil {
			return nil, serviceUnavailable(ctx, "sandbox-service-unavailable", "sandbox job service is unavailable", jobs.ErrRunnerUnavailable)
		}
		req := input.Body
		orgID := strings.TrimSpace(string(req.OrgID))
		if orgID == "" {
			return nil, badRequest(ctx, "invalid-org-id", "org_id is required", nil)
		}
		providerRepoID, err := strconv.ParseInt(strings.TrimSpace(string(req.ProviderRepositoryID)), 10, 64)
		if err != nil || providerRepoID <= 0 {
			return nil, badRequest(ctx, "invalid-provider-repository-id", "provider_repository_id must be a positive decimal int64 string", err)
		}
		projectID, err := uuid.Parse(string(req.ProjectID))
		if err != nil {
			return nil, badRequest(ctx, "invalid-project-id", "project_id must be a UUID", err)
		}
		var sourceRepositoryID uuid.UUID
		if req.SourceRepositoryID != nil {
			sourceRepositoryID, err = uuid.Parse(string(*req.SourceRepositoryID))
			if err != nil {
				return nil, badRequest(ctx, "invalid-source-repository-id", "source_repository_id must be a UUID", err)
			}
		}
		registration := jobs.RunnerRepositoryRegistration{
			Provider:             strings.TrimSpace(string(req.Provider)),
			OrgID:                orgID,
			ProjectID:            projectID,
			SourceRepositoryID:   sourceRepositoryID,
			ProviderOwner:        strings.TrimSpace(string(req.ProviderOwner)),
			ProviderRepo:         strings.TrimSpace(string(req.ProviderRepo)),
			ProviderRepositoryID: providerRepoID,
			RepositoryFullName:   strings.TrimSpace(stringFromPtr(req.RepositoryFullName)),
		}
		span.SetAttributes(
			attribute.String("spiffe.peer_id", peerID.String()),
			attribute.String("verself.org_id", orgID),
			attribute.String("verself.project_id", projectID.String()),
			attribute.String("runner.provider", registration.Provider),
			attribute.Int64("runner.provider_repository_id", providerRepoID),
		)
		if err := svc.RegisterRunnerRepository(ctx, registration); err != nil {
			return nil, runnerRepositoryRegistrationError(ctx, err)
		}
		var sourceRepositoryIDOut *internalcontractapi.SourceRepositoryID
		if registration.SourceRepositoryID != uuid.Nil {
			value := internalcontractapi.SourceRepositoryID(registration.SourceRepositoryID.String())
			sourceRepositoryIDOut = &value
		}
		return &internalcontractapi.InternalRegisterRunnerRepositoryOutput{Body: internalcontractapi.InternalRegisterRunnerRepositoryOutputBody{
			Registration: internalcontractapi.RunnerRepositoryRegistration{
				Provider:             internalcontractapi.Provider(registration.Provider),
				ProviderRepositoryID: internalcontractapi.ProviderRepositoryID(strconv.FormatInt(providerRepoID, 10)),
				ProjectID:            internalcontractapi.ProjectID(registration.ProjectID.String()),
				SourceRepositoryID:   sourceRepositoryIDOut,
				State:                "registered",
			},
		}}, nil
	}
}

func internalGetRunnerAllocation(svc *jobs.Service) func(context.Context, *internalcontractapi.InternalGetRunnerAllocationInput) (*internalcontractapi.InternalGetRunnerAllocationOutput, error) {
	return func(ctx context.Context, input *internalcontractapi.InternalGetRunnerAllocationInput) (*internalcontractapi.InternalGetRunnerAllocationOutput, error) {
		if _, ok := workloadauth.PeerIDFromContext(ctx); !ok {
			return nil, unauthorized(ctx)
		}
		if svc == nil {
			return nil, serviceUnavailable(ctx, "sandbox-service-unavailable", "sandbox job service is unavailable", jobs.ErrRunnerUnavailable)
		}
		allocationID, err := uuid.Parse(string(input.AllocationID))
		if err != nil {
			return nil, badRequest(ctx, "invalid-allocation-id", "allocation_id must be a UUID", err)
		}
		status, err := svc.GetProviderRunnerAllocationStatus(ctx, allocationID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, notFound(ctx, "runner-allocation-not-found", "runner allocation was not found")
			}
			return nil, runnerJobMutationError(ctx, err)
		}
		return &internalcontractapi.InternalGetRunnerAllocationOutput{Body: internalcontractapi.InternalGetRunnerAllocationOutputBody{
			Allocation: runnerAllocationStatusOutput(status),
		}}, nil
	}
}

func internalSubmitRunnerJob(svc *jobs.Service) func(context.Context, *internalcontractapi.InternalSubmitRunnerJobInput) (*internalcontractapi.InternalSubmitRunnerJobOutput, error) {
	return func(ctx context.Context, input *internalcontractapi.InternalSubmitRunnerJobInput) (*internalcontractapi.InternalSubmitRunnerJobOutput, error) {
		if _, ok := workloadauth.PeerIDFromContext(ctx); !ok {
			return nil, unauthorized(ctx)
		}
		if svc == nil {
			return nil, serviceUnavailable(ctx, "sandbox-service-unavailable", "sandbox job service is unavailable", jobs.ErrRunnerUnavailable)
		}
		req := input.Body
		obs, err := runnerJobObservationFromContract(req.Observation)
		if err != nil {
			return nil, badRequest(ctx, "invalid-runner-job-observation", err.Error(), err)
		}
		var workflow *jobs.ProviderWorkflowRunObservation
		if req.WorkflowRun != nil {
			value, err := workflowRunObservationFromContract(*req.WorkflowRun)
			if err != nil {
				return nil, badRequest(ctx, "invalid-runner-workflow-run-observation", err.Error(), err)
			}
			workflow = &value
		}
		var cacheManifest *jobs.ProviderCacheManifest
		if req.CacheManifest != nil {
			value, err := cacheManifestFromContract(*req.CacheManifest)
			if err != nil {
				return nil, badRequest(ctx, "invalid-runner-cache-manifest", err.Error(), err)
			}
			cacheManifest = &value
		}
		runnerID := int64(0)
		if req.RunnerID != nil {
			runnerID, err = parsePositiveInt64(string(*req.RunnerID), "runner_id")
			if err != nil {
				return nil, badRequest(ctx, "invalid-runner-id", err.Error(), err)
			}
		}
		result, err := svc.SubmitProviderRunnerJob(ctx, jobs.ProviderRunnerJobSubmission{
			Observation:      obs,
			WorkflowRun:      workflow,
			CacheManifest:    cacheManifest,
			RunnerName:       string(req.RunnerName),
			RunnerID:         runnerID,
			BootstrapKind:    string(req.BootstrapKind),
			BootstrapPayload: string(req.BootstrapPayload),
		})
		if err != nil {
			return nil, runnerJobMutationError(ctx, err)
		}
		return &internalcontractapi.InternalSubmitRunnerJobOutput{Body: internalcontractapi.InternalSubmitRunnerJobOutputBody{
			AllocationID: internalcontractapi.AttemptID(result.AllocationID.String()),
			ExecutionID:  internalcontractapi.ExecutionID(result.ExecutionID.String()),
			AttemptID:    internalcontractapi.AttemptID(result.AttemptID.String()),
			RunnerName:   internalcontractapi.RunnerName(result.RunnerName),
			RunnerID:     decimalPtr(result.RunnerID),
			Created:      result.Created,
		}}, nil
	}
}

func cacheManifestFromContract(input internalcontractapi.RunnerCacheManifest) (jobs.ProviderCacheManifest, error) {
	content, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(input.ContentBase64)))
	if err != nil {
		return jobs.ProviderCacheManifest{}, fmt.Errorf("content_base64 must be standard base64: %w", err)
	}
	if len(content) > maxRunnerCacheManifestBytes {
		return jobs.ProviderCacheManifest{}, fmt.Errorf("content_base64 decodes to %d bytes, maximum is %d", len(content), maxRunnerCacheManifestBytes)
	}
	return jobs.ProviderCacheManifest{
		SourceKind: strings.TrimSpace(input.SourceKind),
		SourcePath: strings.TrimSpace(input.SourcePath),
		SourceSHA:  strings.TrimSpace(input.SourceSHA),
		Content:    content,
	}, nil
}

func decimalPtr(value int64) *internalcontractapi.DecimalUint64 {
	if value <= 0 {
		return nil
	}
	out := internalcontractapi.DecimalUint64(strconv.FormatInt(value, 10))
	return &out
}

func internalObserveRunnerJob(svc *jobs.Service) func(context.Context, *internalcontractapi.InternalObserveRunnerJobInput) (*internalcontractapi.InternalObserveRunnerJobOutput, error) {
	return func(ctx context.Context, input *internalcontractapi.InternalObserveRunnerJobInput) (*internalcontractapi.InternalObserveRunnerJobOutput, error) {
		if _, ok := workloadauth.PeerIDFromContext(ctx); !ok {
			return nil, unauthorized(ctx)
		}
		if svc == nil {
			return nil, serviceUnavailable(ctx, "sandbox-service-unavailable", "sandbox job service is unavailable", jobs.ErrRunnerUnavailable)
		}
		obs, err := runnerJobObservationFromContract(input.Body.Observation)
		if err != nil {
			return nil, badRequest(ctx, "invalid-runner-job-observation", err.Error(), err)
		}
		var workflow *jobs.ProviderWorkflowRunObservation
		if input.Body.WorkflowRun != nil {
			value, err := workflowRunObservationFromContract(*input.Body.WorkflowRun)
			if err != nil {
				return nil, badRequest(ctx, "invalid-runner-workflow-run-observation", err.Error(), err)
			}
			workflow = &value
		}
		result, err := svc.ObserveProviderRunnerJob(ctx, obs, workflow)
		if err != nil {
			return nil, runnerJobMutationError(ctx, err)
		}
		return &internalcontractapi.InternalObserveRunnerJobOutput{Body: runnerJobObservationOutput(result)}, nil
	}
}

func internalObserveRunnerWorkflowRun(svc *jobs.Service) func(context.Context, *internalcontractapi.InternalObserveRunnerWorkflowRunInput) (*internalcontractapi.InternalObserveRunnerWorkflowRunOutput, error) {
	return func(ctx context.Context, input *internalcontractapi.InternalObserveRunnerWorkflowRunInput) (*internalcontractapi.InternalObserveRunnerWorkflowRunOutput, error) {
		if _, ok := workloadauth.PeerIDFromContext(ctx); !ok {
			return nil, unauthorized(ctx)
		}
		if svc == nil {
			return nil, serviceUnavailable(ctx, "sandbox-service-unavailable", "sandbox job service is unavailable", jobs.ErrRunnerUnavailable)
		}
		workflow, err := workflowRunObservationFromContract(input.Body.WorkflowRun)
		if err != nil {
			return nil, badRequest(ctx, "invalid-runner-workflow-run-observation", err.Error(), err)
		}
		if err := svc.ObserveProviderWorkflowRun(ctx, workflow); err != nil {
			return nil, runnerJobMutationError(ctx, err)
		}
		return &internalcontractapi.InternalObserveRunnerWorkflowRunOutput{Body: internalcontractapi.InternalObserveRunnerWorkflowRunOutputBody{State: "observed"}}, nil
	}
}

func internalRequestGoldenSnapshotBarrier(svc *jobs.Service) func(context.Context, *internalcontractapi.InternalRequestGoldenSnapshotBarrierInput) (*internalcontractapi.InternalRequestGoldenSnapshotBarrierOutput, error) {
	return func(ctx context.Context, input *internalcontractapi.InternalRequestGoldenSnapshotBarrierInput) (*internalcontractapi.InternalRequestGoldenSnapshotBarrierOutput, error) {
		if _, ok := workloadauth.PeerIDFromContext(ctx); !ok {
			return nil, unauthorized(ctx)
		}
		if svc == nil {
			return nil, serviceUnavailable(ctx, "sandbox-service-unavailable", "sandbox job service is unavailable", jobs.ErrRunnerUnavailable)
		}
		evidence, err := goldenSnapshotBarrierEvidenceFromContract(input.Body.Evidence)
		if err != nil {
			return nil, badRequest(ctx, "invalid-golden-snapshot-barrier-evidence", err.Error(), err)
		}
		result, err := svc.RequestGoldenSnapshotBarrier(ctx, evidence)
		if err != nil {
			return nil, runnerJobMutationError(ctx, err)
		}
		var reason *string
		if result.Reason != "" {
			reason = &result.Reason
		}
		return &internalcontractapi.InternalRequestGoldenSnapshotBarrierOutput{Body: internalcontractapi.InternalRequestGoldenSnapshotBarrierOutputBody{State: result.State, Reason: reason}}, nil
	}
}

func runnerRepositoryRegistrationError(ctx context.Context, err error) error {
	switch {
	case strings.Contains(err.Error(), "unsupported runner provider"):
		return badRequest(ctx, "unsupported-runner-provider", "runner provider is not supported", err)
	default:
		return internalFailure(ctx, "runner-repository-registration-failed", "register runner repository failed", err)
	}
}

func runnerJobMutationError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, jobs.ErrRunnerClassMissing):
		return badRequest(ctx, "runner-class-missing", "runner job labels do not include a configured runner class", err)
	case errors.Is(err, jobs.ErrRunnerUnavailable):
		return serviceUnavailable(ctx, "runner-unavailable", "runner job cannot be submitted", err)
	default:
		return internalFailure(ctx, "runner-job-mutation-failed", "runner job mutation failed", err)
	}
}

func runnerJobObservationFromContract(input internalcontractapi.RunnerJobObservation) (jobs.ProviderRunnerJobObservation, error) {
	providerJobID, err := parsePositiveInt64(string(input.ProviderJobID), "provider_job_id")
	if err != nil {
		return jobs.ProviderRunnerJobObservation{}, err
	}
	providerRepoID, err := parsePositiveInt64(string(input.ProviderRepositoryID), "provider_repository_id")
	if err != nil {
		return jobs.ProviderRunnerJobObservation{}, err
	}
	obs := jobs.ProviderRunnerJobObservation{
		Provider:             strings.TrimSpace(string(input.Provider)),
		ProviderJobID:        providerJobID,
		ProviderRepositoryID: providerRepoID,
		Status:               strings.TrimSpace(input.Status),
		Labels:               []string(input.Labels),
	}
	if input.ProviderInstallationID != nil {
		obs.ProviderInstallationID, err = parseNonNegativeInt64(string(*input.ProviderInstallationID), "provider_installation_id")
		if err != nil {
			return jobs.ProviderRunnerJobObservation{}, err
		}
	}
	if input.ProviderRunID != nil {
		obs.ProviderRunID, err = parseNonNegativeInt64(string(*input.ProviderRunID), "provider_run_id")
		if err != nil {
			return jobs.ProviderRunnerJobObservation{}, err
		}
	}
	if input.ProviderRunAttempt != nil {
		obs.ProviderRunAttempt, err = parseNonNegativeInt64(string(*input.ProviderRunAttempt), "provider_run_attempt")
		if err != nil {
			return jobs.ProviderRunnerJobObservation{}, err
		}
	}
	if input.RunnerID != nil {
		obs.RunnerID, err = parseNonNegativeInt64(string(*input.RunnerID), "runner_id")
		if err != nil {
			return jobs.ProviderRunnerJobObservation{}, err
		}
	}
	obs.RepositoryFullName = stringFromPtr(input.RepositoryFullName)
	obs.JobName = stringFromPtr(input.JobName)
	obs.HeadSHA = stringFromPtr(input.HeadSHA)
	obs.HeadBranch = stringFromPtr(input.HeadBranch)
	obs.WorkflowName = stringFromPtr(input.WorkflowName)
	obs.Conclusion = stringFromPtr(input.Conclusion)
	obs.RunnerName = stringFromPtr(input.RunnerName)
	obs.DeliveryID = stringFromPtr(input.DeliveryID)
	if input.StartedAt != nil {
		obs.StartedAt, err = parseRFC3339(string(*input.StartedAt), "started_at")
		if err != nil {
			return jobs.ProviderRunnerJobObservation{}, err
		}
	}
	if input.CompletedAt != nil {
		obs.CompletedAt, err = parseRFC3339(string(*input.CompletedAt), "completed_at")
		if err != nil {
			return jobs.ProviderRunnerJobObservation{}, err
		}
	}
	return obs, nil
}

func workflowRunObservationFromContract(input internalcontractapi.RunnerWorkflowRunObservation) (jobs.ProviderWorkflowRunObservation, error) {
	providerRepoID, err := parsePositiveInt64(string(input.ProviderRepositoryID), "provider_repository_id")
	if err != nil {
		return jobs.ProviderWorkflowRunObservation{}, err
	}
	obs := jobs.ProviderWorkflowRunObservation{
		Provider:               strings.TrimSpace(string(input.Provider)),
		ProviderRepositoryID:   providerRepoID,
		RepositoryFullName:     stringFromPtr(input.RepositoryFullName),
		EventName:              stringFromPtr(input.EventName),
		HeadSHA:                stringFromPtr(input.HeadSHA),
		HeadBranch:             stringFromPtr(input.HeadBranch),
		HeadRepositoryFullName: stringFromPtr(input.HeadRepositoryFullName),
		BaseSHA:                stringFromPtr(input.BaseSHA),
		BaseBranch:             stringFromPtr(input.BaseBranch),
		WorkflowPath:           stringFromPtr(input.WorkflowPath),
	}
	if input.ProviderInstallationID != nil {
		obs.ProviderInstallationID, err = parseNonNegativeInt64(string(*input.ProviderInstallationID), "provider_installation_id")
		if err != nil {
			return jobs.ProviderWorkflowRunObservation{}, err
		}
	}
	if input.ProviderRunID != nil {
		obs.ProviderRunID, err = parseNonNegativeInt64(string(*input.ProviderRunID), "provider_run_id")
		if err != nil {
			return jobs.ProviderWorkflowRunObservation{}, err
		}
	}
	if input.ProviderRunAttempt != nil {
		obs.ProviderRunAttempt, err = parseNonNegativeInt64(string(*input.ProviderRunAttempt), "provider_run_attempt")
		if err != nil {
			return jobs.ProviderWorkflowRunObservation{}, err
		}
	}
	if input.PullRequestNumber != nil {
		obs.PullRequestNumber, err = parseNonNegativeInt64(string(*input.PullRequestNumber), "pull_request_number")
		if err != nil {
			return jobs.ProviderWorkflowRunObservation{}, err
		}
	}
	if input.CommitCount != nil {
		obs.CommitCount, err = parseNonNegativeInt64(string(*input.CommitCount), "commit_count")
		if err != nil {
			return jobs.ProviderWorkflowRunObservation{}, err
		}
	}
	return obs, nil
}

func goldenSnapshotBarrierEvidenceFromContract(input internalcontractapi.GoldenSnapshotBarrierEvidence) (jobs.GoldenSnapshotBarrierEvidence, error) {
	providerRepoID, err := parsePositiveInt64(string(input.ProviderRepositoryID), "provider_repository_id")
	if err != nil {
		return jobs.GoldenSnapshotBarrierEvidence{}, err
	}
	runID, err := parsePositiveInt64(string(input.ProviderRunID), "provider_run_id")
	if err != nil {
		return jobs.GoldenSnapshotBarrierEvidence{}, err
	}
	runAttempt, err := parsePositiveInt64(string(input.ProviderRunAttempt), "provider_run_attempt")
	if err != nil {
		return jobs.GoldenSnapshotBarrierEvidence{}, err
	}
	jobID, err := parsePositiveInt64(string(input.ProviderJobID), "provider_job_id")
	if err != nil {
		return jobs.GoldenSnapshotBarrierEvidence{}, err
	}
	executionID, err := uuid.Parse(string(input.ExecutionID))
	if err != nil {
		return jobs.GoldenSnapshotBarrierEvidence{}, fmt.Errorf("execution_id must be a UUID: %w", err)
	}
	attemptID, err := uuid.Parse(string(input.AttemptID))
	if err != nil {
		return jobs.GoldenSnapshotBarrierEvidence{}, fmt.Errorf("attempt_id must be a UUID: %w", err)
	}
	evidence := jobs.GoldenSnapshotBarrierEvidence{
		Provider:                 strings.TrimSpace(string(input.Provider)),
		ProviderRepositoryID:     providerRepoID,
		ProviderRunID:            runID,
		ProviderRunAttempt:       runAttempt,
		ProviderJobID:            jobID,
		RepositoryFullName:       stringFromPtr(input.RepositoryFullName),
		HeadSHA:                  stringFromPtr(input.HeadSHA),
		Conclusion:               strings.TrimSpace(stringFromPtr(input.Conclusion)),
		RunnerName:               stringFromPtr(input.RunnerName),
		ExecutionID:              executionID,
		AttemptID:                attemptID,
		LeaseID:                  stringFromPtr(input.LeaseID),
		JobShapeID:               stringFromPtr(input.JobShapeID),
		DurableGenerationSetHash: stringFromPtr(input.DurableGenerationSetHash),
		TrustClass:               stringFromPtr(input.TrustClass),
		PromotionPolicy:          stringFromPtr(input.PromotionPolicy),
	}
	if input.ProviderInstallationID != nil {
		evidence.ProviderInstallationID, err = parseNonNegativeInt64(string(*input.ProviderInstallationID), "provider_installation_id")
		if err != nil {
			return jobs.GoldenSnapshotBarrierEvidence{}, err
		}
	}
	if input.RunnerID != nil {
		evidence.RunnerID, err = parseNonNegativeInt64(string(*input.RunnerID), "runner_id")
		if err != nil {
			return jobs.GoldenSnapshotBarrierEvidence{}, err
		}
	}
	return evidence, nil
}

func runnerJobObservationOutput(result jobs.ProviderRunnerJobObservationResult) internalcontractapi.InternalObserveRunnerJobOutputBody {
	out := internalcontractapi.InternalObserveRunnerJobOutputBody{State: result.State}
	if result.AllocationID != uuid.Nil {
		value := internalcontractapi.AttemptID(result.AllocationID.String())
		out.AllocationID = &value
	}
	if result.ExecutionID != uuid.Nil {
		value := internalcontractapi.ExecutionID(result.ExecutionID.String())
		out.ExecutionID = &value
	}
	if result.AttemptID != uuid.Nil {
		value := internalcontractapi.AttemptID(result.AttemptID.String())
		out.AttemptID = &value
	}
	return out
}

func runnerAllocationStatusOutput(status jobs.ProviderRunnerAllocationStatus) internalcontractapi.RunnerAllocationStatus {
	out := internalcontractapi.RunnerAllocationStatus{
		AllocationID:         internalcontractapi.AttemptID(status.AllocationID.String()),
		Provider:             internalcontractapi.Provider(status.Provider),
		ProviderRepositoryID: internalcontractapi.ProviderRepositoryID(strconv.FormatInt(status.ProviderRepositoryID, 10)),
		State:                status.State,
	}
	if status.ProviderInstallationID != 0 {
		out.ProviderInstallationID = decimalPtr(status.ProviderInstallationID)
	}
	if status.RunnerClass != "" {
		value := status.RunnerClass
		out.RunnerClass = &value
	}
	if status.RunnerName != "" {
		value := internalcontractapi.RunnerName(status.RunnerName)
		out.RunnerName = &value
	}
	if status.RunnerID != 0 {
		out.RunnerID = decimalPtr(status.RunnerID)
	}
	if status.OriginProviderJobID != 0 {
		out.OriginProviderJobID = decimalPtr(status.OriginProviderJobID)
	}
	if status.AssignedProviderJobID != 0 {
		out.AssignedProviderJobID = decimalPtr(status.AssignedProviderJobID)
	}
	if status.ExecutionID != uuid.Nil {
		value := internalcontractapi.ExecutionID(status.ExecutionID.String())
		out.ExecutionID = &value
	}
	if status.AttemptID != uuid.Nil {
		value := internalcontractapi.AttemptID(status.AttemptID.String())
		out.AttemptID = &value
	}
	out.Problems = runnerProblemOccurrences(status.Problems)
	if status.ExecutionState != "" {
		out.ExecutionState = &status.ExecutionState
	}
	if status.AttemptState != "" {
		out.AttemptState = &status.AttemptState
	}
	return out
}

func runnerProblemOccurrences(problems []jobs.RunnerProblem) internalcontractapi.ProblemOccurrences {
	out := make(internalcontractapi.ProblemOccurrences, 0, len(problems))
	for _, problem := range problems {
		item := internalcontractapi.ProblemOccurrence{
			Type:  internalcontractapi.ProblemType(problem.Type),
			Code:  internalcontractapi.ProblemCode(problem.Code),
			Title: problem.Title,
		}
		if problem.Detail != "" {
			value := internalcontractapi.ProblemDetail(problem.Detail)
			item.Detail = &value
		}
		if problem.Status != 0 {
			value := int(problem.Status)
			item.Status = &value
		}
		if problem.Phase != "" {
			value := internalcontractapi.ProblemPhase(problem.Phase)
			item.Phase = &value
		}
		if problem.Retryable {
			item.Retryable = &problem.Retryable
		}
		if problem.Pointer != "" {
			value := internalcontractapi.ProblemPointer(problem.Pointer)
			item.Pointer = &value
		}
		if !problem.ObservedAt.IsZero() {
			value := timestamp(problem.ObservedAt)
			item.ObservedAt = &value
		}
		out = append(out, item)
	}
	return out
}

func parsePositiveInt64(value string, field string) (int64, error) {
	parsed, err := parseNonNegativeInt64(value, field)
	if err != nil {
		return 0, err
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be positive", field)
	}
	return parsed, nil
}

func parseNonNegativeInt64(value string, field string) (int64, error) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed < 0 {
		if err == nil {
			err = fmt.Errorf("negative value")
		}
		return 0, fmt.Errorf("%s must be a non-negative int64: %w", field, err)
	}
	return parsed, nil
}

func parseRFC3339(value string, field string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be RFC3339: %w", field, err)
	}
	return parsed.UTC(), nil
}
