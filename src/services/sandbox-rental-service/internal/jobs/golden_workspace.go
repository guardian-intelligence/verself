package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/verself/sandbox-rental-service/internal/scheduler"
	"github.com/verself/sandbox-rental-service/internal/store"
	vmorchestrator "github.com/verself/vm-orchestrator"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	goldenWorkspaceMountName       = "github-workspace"
	goldenWorkspaceComponentKind   = "github_workspace"
	goldenWorkspaceComponentKey    = "repo-workspace"
	goldenWorkspaceTrustSecretless = "secretless"
	goldenWorkspaceTaintPolicy     = "secretless_only"
	goldenWorkspacePlatformImage   = "ubuntu-2404-actions-runner"
	goldenWorkspaceDogfoodBranch   = "main"
)

type goldenWorkspacePlan struct {
	Enabled               bool
	Identity              RunnerExecutionIdentity
	OperationID           uuid.UUID
	GoldenScopeID         uuid.UUID
	JobShapeID            uuid.UUID
	SourceGenerationID    *uuid.UUID
	SourceSnapshotRef     string
	CandidateGenerationID uuid.UUID
	MountName             string
	MountPath             string
	TrustClass            string
	TaintPolicy           string
	PromotionEligible     bool
}

type goldenWorkflowRunRef struct {
	OrgID                  uint64
	Provider               string
	ProviderInstallationID int64
	ProviderRepositoryID   int64
	ProviderRunID          int64
	ProviderRunAttempt     int64
	RepositoryFullName     string
	HeadSHA                string
}

func (p goldenWorkspacePlan) filesystemMount() vmorchestrator.FilesystemMount {
	return vmorchestrator.FilesystemMount{
		Name:        p.MountName,
		OperationID: p.OperationID.String(),
		SourceRef:   p.SourceSnapshotRef,
		MountPath:   p.MountPath,
		FSType:      "ext4",
		ReadOnly:    false,
	}
}

func (s *Service) prepareGoldenWorkspace(ctx context.Context, item executionWorkItem) (goldenWorkspacePlan, error) {
	if item.WorkloadKind != WorkloadKindRunner || item.ExternalProvider != RunnerProviderGitHub {
		return goldenWorkspacePlan{}, nil
	}
	ctx, span := tracer.Start(ctx, "github.workspace.select", trace.WithAttributes(
		attribute.String("execution.id", item.ExecutionID.String()),
		attribute.String("attempt.id", item.AttemptID.String()),
	))
	defer span.End()

	identity, err := s.runnerExecutionIdentity(ctx, item.ExecutionID, item.AttemptID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("%w: runner execution identity missing", ErrRunnerUnavailable)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return goldenWorkspacePlan{}, err
	}
	if identity.Provider != RunnerProviderGitHub {
		return goldenWorkspacePlan{}, nil
	}
	scopeRef := "refs/heads/" + goldenWorkspaceDogfoodBranch
	trustClass := "protected_branch"
	workflowIdentity := firstNonEmpty(identity.WorkflowName, "github-actions")
	jobIdentity := firstNonEmpty(identity.JobName, identity.RunnerClass, "github-job")
	matrixKey := githubMatrixKey(identity)
	durableHash := stableHex("durable", goldenWorkspaceComponentKind, githubRunnerDurableWorkDir)
	checkoutHash := stableHex("checkout", "v0", "preserve-untracked")
	now := time.Now().UTC()

	jobShapeID := stableUUID("job-shape", strconv.FormatUint(identity.OrgID, 10), identity.Provider, strconv.FormatInt(identity.ProviderRepositoryID, 10), workflowIdentity, jobIdentity, matrixKey, identity.RunnerClass, goldenWorkspacePlatformImage, durableHash, checkoutHash)
	scopeID := stableUUID("golden-scope", strconv.FormatUint(identity.OrgID, 10), identity.Provider, strconv.FormatInt(identity.ProviderRepositoryID, 10), scopeRef, jobShapeID.String(), trustClass)
	operationID := uuid.New()
	candidateGenerationID := uuid.New()
	promotionEligible := goldenWorkspacePromotionCandidate(identity)

	shape, err := s.storeQueries().UpsertJobShape(ctx, store.UpsertJobShapeParams{
		JobShapeID:             jobShapeID,
		OrgID:                  dbOrgID(identity.OrgID),
		Provider:               identity.Provider,
		ProviderRepositoryID:   identity.ProviderRepositoryID,
		WorkflowIdentity:       workflowIdentity,
		CalledWorkflowIdentity: "",
		JobIdentity:            jobIdentity,
		MatrixKey:              matrixKey,
		RunnerClass:            identity.RunnerClass,
		PlatformImageID:        goldenWorkspacePlatformImage,
		DurableManifestHash:    durableHash,
		CheckoutPolicyHash:     checkoutHash,
		CreatedAt:              pgTime(now),
	})
	if err != nil {
		return goldenWorkspacePlan{}, fmt.Errorf("upsert golden job shape: %w", err)
	}
	scope, err := s.storeQueries().UpsertGoldenScope(ctx, store.UpsertGoldenScopeParams{
		GoldenScopeID:        scopeID,
		OrgID:                dbOrgID(identity.OrgID),
		Provider:             identity.Provider,
		ProviderRepositoryID: identity.ProviderRepositoryID,
		ScopeKind:            "branch",
		ScopeRef:             scopeRef,
		JobShapeID:           shape,
		TrustClass:           trustClass,
		CreatedAt:            pgTime(now),
	})
	if err != nil {
		return goldenWorkspacePlan{}, fmt.Errorf("upsert golden scope: %w", err)
	}

	var sourceGenerationID *uuid.UUID
	sourceSnapshotRef := ""
	current, err := s.storeQueries().GetCurrentGoldenGeneration(ctx, store.GetCurrentGoldenGenerationParams{GoldenScopeID: scope})
	if err == nil {
		sourceGenerationID = &current.GoldenGenerationID
		sourceSnapshotRef = current.ZfsSnapshotRef
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return goldenWorkspacePlan{}, fmt.Errorf("select current golden generation: %w", err)
	}
	op, err := s.storeQueries().InsertWorkspaceOperation(ctx, store.InsertWorkspaceOperationParams{
		OperationID:           operationID,
		ExecutionID:           item.ExecutionID,
		AttemptID:             item.AttemptID,
		AllocationID:          &identity.AllocationID,
		GoldenScopeID:         scope,
		JobShapeID:            shape,
		SourceGenerationID:    sourceGenerationID,
		SourceSnapshotRef:     sourceSnapshotRef,
		CandidateGenerationID: candidateGenerationID,
		MountName:             goldenWorkspaceMountName,
		MountPath:             githubRunnerDurableWorkDir,
		TrustClass:            trustClass,
		TaintPolicy:           goldenWorkspaceTaintPolicy,
		RequestedAt:           pgTime(now),
	})
	if err != nil {
		return goldenWorkspacePlan{}, fmt.Errorf("insert workspace operation: %w", err)
	}
	span.SetAttributes(
		attribute.String("golden.operation_id", op.OperationID.String()),
		attribute.String("golden.scope_id", scope.String()),
		attribute.String("golden.source_generation_id", uuidPtrString(sourceGenerationID)),
		attribute.Bool("github.workspace.cache_hit", sourceSnapshotRef != ""),
		attribute.String("github.repository", identity.RepositoryFullName),
		attribute.String("github.workspace.scope_ref", scopeRef),
		attribute.String("github.workflow.job_matrix_key", matrixKey),
		attribute.Bool("golden.promotion_candidate", promotionEligible),
	)
	_ = s.appendGoldenEvent(ctx, goldenEvent{
		OperationID: &op.OperationID,
		ScopeID:     &scope,
		ExecutionID: &item.ExecutionID,
		AttemptID:   &item.AttemptID,
		Name:        "github.workspace.select",
		Result:      boolResult(sourceSnapshotRef != "", "hit", "miss"),
	})
	return goldenWorkspacePlan{
		Enabled:               true,
		Identity:              identity,
		OperationID:           op.OperationID,
		GoldenScopeID:         scope,
		JobShapeID:            shape,
		SourceGenerationID:    op.SourceGenerationID,
		SourceSnapshotRef:     op.SourceSnapshotRef,
		CandidateGenerationID: op.CandidateGenerationID,
		MountName:             op.MountName,
		MountPath:             op.MountPath,
		TrustClass:            op.TrustClass,
		TaintPolicy:           op.TaintPolicy,
		PromotionEligible:     promotionEligible,
	}, nil
}

func (s *Service) markGoldenWorkspaceMounted(ctx context.Context, plan goldenWorkspacePlan) {
	if !plan.Enabled {
		return
	}
	now := time.Now().UTC()
	if err := s.storeQueries().MarkWorkspaceOperationMounted(ctx, store.MarkWorkspaceOperationMountedParams{
		Now:         pgTime(now),
		OperationID: plan.OperationID,
	}); err != nil && s.Logger != nil {
		s.Logger.WarnContext(ctx, "mark golden workspace mounted failed", "operation_id", plan.OperationID, "error", err)
	}
	_ = s.appendGoldenEvent(ctx, goldenEvent{
		OperationID: &plan.OperationID,
		ScopeID:     &plan.GoldenScopeID,
		ExecutionID: &plan.Identity.ExecutionID,
		AttemptID:   &plan.Identity.AttemptID,
		Name:        "github.workspace.prepare",
		Result:      "mounted",
	})
}

func (s *Service) failGoldenWorkspace(ctx context.Context, plan goldenWorkspacePlan, reason string, cause error) {
	if !plan.Enabled {
		return
	}
	failureReason := goldenFailureReason(reason, cause)
	now := time.Now().UTC()
	if err := s.storeQueries().MarkWorkspaceOperationFailed(ctx, store.MarkWorkspaceOperationFailedParams{
		FailureReason: failureReason,
		Now:           pgTime(now),
		OperationID:   plan.OperationID,
	}); err != nil && s.Logger != nil {
		s.Logger.WarnContext(ctx, "mark golden workspace failed", "operation_id", plan.OperationID, "error", err)
	}
	_ = s.appendGoldenEvent(ctx, goldenEvent{
		OperationID: &plan.OperationID,
		ScopeID:     &plan.GoldenScopeID,
		ExecutionID: &plan.Identity.ExecutionID,
		AttemptID:   &plan.Identity.AttemptID,
		Name:        "github.workspace.prepare",
		Result:      "failed",
		Reason:      failureReason,
	})
}

func (s *Service) failOpenGoldenWorkspaceForAttempt(ctx context.Context, item executionWorkItem, reason string, cause error) {
	failureReason := goldenFailureReason(reason, cause)
	rows, err := s.storeQueries().MarkOpenWorkspaceOperationsFailedByAttempt(ctx, store.MarkOpenWorkspaceOperationsFailedByAttemptParams{
		FailureReason: failureReason,
		Now:           pgTime(time.Now().UTC()),
		AttemptID:     item.AttemptID,
	})
	if err != nil {
		if s.Logger != nil {
			s.Logger.WarnContext(ctx, "mark open golden workspace failed", "attempt_id", item.AttemptID, "error", err)
		}
		return
	}
	for _, row := range rows {
		_ = s.appendGoldenEvent(ctx, goldenEvent{
			OperationID: &row.OperationID,
			ScopeID:     &row.GoldenScopeID,
			ExecutionID: &row.ExecutionID,
			AttemptID:   &row.AttemptID,
			Name:        "github.workspace.reconcile",
			Result:      "failed",
			Reason:      failureReason,
		})
	}
}

func goldenFailureReason(reason string, cause error) string {
	failureReason := strings.TrimSpace(reason)
	if cause != nil {
		causeText := strings.TrimSpace(cause.Error())
		if causeText != "" {
			if failureReason == "" {
				return causeText
			}
			return failureReason + ": " + causeText
		}
	}
	if failureReason == "" {
		return "unknown"
	}
	return failureReason
}

func (s *Service) finalizeGoldenWorkspace(ctx context.Context, item executionWorkItem, leaseID string, plan goldenWorkspacePlan, finalExec vmorchestrator.ExecRecord) error {
	if !plan.Enabled {
		return nil
	}
	ctx, span := tracer.Start(ctx, "golden.component.seal", trace.WithAttributes(
		attribute.String("golden.operation_id", plan.OperationID.String()),
		attribute.String("golden.scope_id", plan.GoldenScopeID.String()),
		attribute.String("lease.id", leaseID),
		attribute.String("filesystem.name", plan.MountName),
	))
	defer span.End()
	allowed, skipReason, err := s.goldenWorkspaceSealAllowed(ctx, plan, finalExec)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if !allowed {
		now := time.Now().UTC()
		_ = s.storeQueries().MarkWorkspaceOperationResultRecorded(ctx, store.MarkWorkspaceOperationResultRecordedParams{
			FinalState:  "skipped",
			SealedAt:    pgTime(now),
			RecordedAt:  pgTime(now),
			OperationID: plan.OperationID,
		})
		_ = s.appendGoldenEvent(ctx, goldenEvent{OperationID: &plan.OperationID, ScopeID: &plan.GoldenScopeID, ExecutionID: &item.ExecutionID, AttemptID: &item.AttemptID, Name: "golden.component.seal", Result: "skipped", Reason: skipReason})
		span.SetAttributes(attribute.String("golden.seal_skipped_reason", skipReason))
		return nil
	}
	commit, err := s.Orchestrator.CommitFilesystemMount(ctx, leaseID, plan.OperationID.String()+":commit", plan.OperationID.String(), plan.MountName, plan.GoldenScopeID.String(), plan.SourceSnapshotRef, plan.CandidateGenerationID.String())
	if err != nil {
		_ = s.storeQueries().MarkWorkspaceOperationFailed(ctx, store.MarkWorkspaceOperationFailedParams{
			FailureReason: err.Error(),
			Now:           pgTime(time.Now().UTC()),
			OperationID:   plan.OperationID,
		})
		_ = s.appendGoldenEvent(ctx, goldenEvent{OperationID: &plan.OperationID, ScopeID: &plan.GoldenScopeID, ExecutionID: &item.ExecutionID, AttemptID: &item.AttemptID, Name: "golden.component.seal", Result: "failed", Reason: err.Error()})
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	usedBytes, err := int64FromUint64("golden used bytes", commit.UsedBytes)
	if err != nil {
		return err
	}
	writtenBytes, err := int64FromUint64("golden written bytes", commit.WrittenBytes)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	gen, err := s.storeQueries().InsertGoldenGeneration(ctx, store.InsertGoldenGenerationParams{
		GoldenGenerationID: plan.CandidateGenerationID,
		GoldenScopeID:      plan.GoldenScopeID,
		OperationID:        plan.OperationID,
		SourceGenerationID: plan.SourceGenerationID,
		HeadSha:            plan.Identity.HeadSHA,
		TreeHash:           "",
		ProviderRunID:      plan.Identity.ProviderRunID,
		ProviderJobID:      plan.Identity.ProviderJobID,
		Result:             "success",
		TaintClass:         goldenWorkspaceTrustSecretless,
		PromotionEligible:  plan.PromotionEligible,
		ZfsSnapshotRef:     commit.Snapshot,
		UsedBytes:          usedBytes,
		WrittenBytes:       writtenBytes,
		SealedAt:           pgTime(commit.CommittedAt),
		CommittedAt:        pgTime(now),
	})
	if err != nil {
		return fmt.Errorf("insert golden generation: %w", err)
	}
	if err := s.storeQueries().InsertDurableComponent(ctx, store.InsertDurableComponentParams{
		DurableComponentID:  uuid.New(),
		GoldenGenerationID:  gen.GoldenGenerationID,
		ComponentKind:       goldenWorkspaceComponentKind,
		ComponentKey:        goldenWorkspaceComponentKey,
		GuestMountPath:      plan.MountPath,
		HostDatasetRef:      commit.VolumeDataset,
		SealedSnapshotRef:   commit.Snapshot,
		FilesystemKind:      "ext4",
		SizeBytesLogical:    usedBytes,
		SizeBytesReferenced: writtenBytes,
		ScrubPolicyHash:     "",
		CreatedAt:           pgTime(now),
	}); err != nil {
		return fmt.Errorf("insert durable component: %w", err)
	}
	if err := s.storeQueries().MarkWorkspaceOperationResultRecorded(ctx, store.MarkWorkspaceOperationResultRecordedParams{
		FinalState:  "committed",
		SealedAt:    pgTime(commit.CommittedAt),
		RecordedAt:  pgTime(now),
		OperationID: plan.OperationID,
	}); err != nil {
		return err
	}
	_ = s.appendGoldenEvent(ctx, goldenEvent{OperationID: &plan.OperationID, ScopeID: &plan.GoldenScopeID, GenerationID: &gen.GoldenGenerationID, ExecutionID: &item.ExecutionID, AttemptID: &item.AttemptID, Name: "golden.generation.commit", Result: "succeeded"})
	promoted, err := s.promoteGoldenWorkflowRun(ctx, goldenRunRefFromRunnerIdentity(plan.Identity))
	if err != nil {
		return err
	}
	span.SetAttributes(
		attribute.String("golden.generation_id", gen.GoldenGenerationID.String()),
		attribute.String("golden.snapshot_ref", commit.Snapshot),
		attribute.Bool("golden.promoted", promoted),
		attribute.Int64("zfs.used_bytes", usedBytes),
		attribute.Int64("zfs.written_bytes", writtenBytes),
	)
	return nil
}

func (s *Service) goldenWorkspaceSealAllowed(ctx context.Context, plan goldenWorkspacePlan, finalExec vmorchestrator.ExecRecord) (bool, string, error) {
	if finalExec.ExitCode != 0 || finalExec.State == vmorchestrator.ExecStateFailed {
		reason := "exec_failed"
		if strings.TrimSpace(finalExec.TerminalReason) != "" {
			reason += ": " + strings.TrimSpace(finalExec.TerminalReason)
		}
		return false, reason, nil
	}
	if plan.Identity.Provider != RunnerProviderGitHub || plan.Identity.ProviderJobID == 0 {
		return true, "", nil
	}
	if s.GitHubRunner == nil {
		return false, "", fmt.Errorf("github runner is not configured")
	}
	ref := goldenRunRefFromRunnerIdentity(plan.Identity)
	var lastReason string
	for attempt := 0; attempt < 4; attempt++ {
		state, err := s.GitHubRunner.refreshWorkflowRunJobsForRun(ctx, ref)
		if err != nil {
			return false, "", err
		}
		allowed, reason, settled := githubJobAllowsGoldenSeal(state, plan.Identity.ProviderJobID)
		if settled {
			return allowed, reason, nil
		}
		lastReason = reason
		if attempt < 3 {
			time.Sleep(500 * time.Millisecond)
		}
	}
	return false, lastReason, nil
}

func githubJobAllowsGoldenSeal(state githubWorkflowRunJobsState, providerJobID int64) (allowed bool, reason string, settled bool) {
	status, conclusion, ok := state.jobResult(providerJobID)
	if !ok {
		return false, "github job was not observed", true
	}
	if status != "completed" {
		return false, "github job is not complete", false
	}
	if conclusion != "success" {
		return false, "github job concluded " + firstNonEmpty(conclusion, "without success"), true
	}
	return true, "", true
}

func (s *Service) githubWorkflowRunPromotionStateForRef(ctx context.Context, ref goldenWorkflowRunRef) (githubWorkflowRunJobsState, error) {
	if ref.Provider != RunnerProviderGitHub {
		return githubWorkflowRunJobsState{Total: 1, Completed: 1, Succeeded: 1}, nil
	}
	if s.GitHubRunner == nil {
		return githubWorkflowRunJobsState{}, nil
	}
	return s.GitHubRunner.refreshWorkflowRunJobsForRun(ctx, ref)
}

func (s *Service) githubWorkflowRunPromotionGate(ctx context.Context, ref goldenWorkflowRunRef) (githubWorkflowInvocation, bool, string, error) {
	if ref.Provider != RunnerProviderGitHub {
		return githubWorkflowInvocation{HeadSHA: ref.HeadSHA}, true, "", nil
	}
	if s.GitHubRunner == nil {
		return githubWorkflowInvocation{}, false, "github runner is not configured", nil
	}
	invocation, err := s.GitHubRunner.refreshWorkflowRunInvocationForRun(ctx, ref)
	if err != nil {
		return githubWorkflowInvocation{}, false, "", err
	}
	ok, reason := invocation.dogfoodMainPromotion(ref)
	return invocation, ok, reason, nil
}

func (s *Service) PromoteGoldenRun(ctx context.Context, req scheduler.GoldenRunPromoteArgs) error {
	if strings.TrimSpace(req.Provider) != RunnerProviderGitHub {
		return fmt.Errorf("golden run promotion unsupported provider %q", req.Provider)
	}
	repository, err := s.storeQueries().GetGoldenRunRepository(ctx, store.GetGoldenRunRepositoryParams{
		Provider:             strings.TrimSpace(req.Provider),
		ProviderRepositoryID: req.ProviderRepositoryID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("load golden run repository: %w", err)
	}
	_, err = s.promoteGoldenWorkflowRun(ctx, goldenWorkflowRunRef{
		OrgID:                  orgIDFromDB(repository.OrgID),
		Provider:               strings.TrimSpace(req.Provider),
		ProviderInstallationID: req.ProviderInstallationID,
		ProviderRepositoryID:   req.ProviderRepositoryID,
		ProviderRunID:          req.ProviderRunID,
		ProviderRunAttempt:     req.ProviderRunAttempt,
		RepositoryFullName:     firstNonEmpty(req.RepositoryFullName, repository.RepositoryFullName),
		HeadSHA:                req.HeadSHA,
	})
	return err
}

func (s *Service) promoteGoldenWorkflowRun(ctx context.Context, ref goldenWorkflowRunRef) (bool, error) {
	ctx, span := tracer.Start(ctx, "golden.workflow_run.promote", trace.WithAttributes(
		attribute.String("runner.provider", ref.Provider),
		attribute.Int64("runner.provider_installation_id", ref.ProviderInstallationID),
		attribute.Int64("runner.provider_repository_id", ref.ProviderRepositoryID),
		attribute.Int64("runner.provider_run_id", ref.ProviderRunID),
		attribute.String("git.commit.sha", ref.HeadSHA),
	))
	defer span.End()
	invocation, promotable, reason, err := s.githubWorkflowRunPromotionGate(ctx, ref)
	if err != nil {
		return false, err
	}
	if !promotable {
		span.SetAttributes(attribute.Bool("golden.promoted", false), attribute.String("golden.promotion_deferred_reason", reason))
		return false, nil
	}
	state, err := s.githubWorkflowRunPromotionStateForRef(ctx, ref)
	if err != nil {
		return false, err
	}
	promotionReady, reason := state.promotionReady()
	if !promotionReady {
		span.SetAttributes(attribute.Bool("golden.promoted", false), attribute.String("golden.promotion_deferred_reason", reason))
		return false, nil
	}
	candidates, err := s.storeQueries().ListGoldenPromotionCandidatesForRun(ctx, store.ListGoldenPromotionCandidatesForRunParams{
		ProviderRunID:        ref.ProviderRunID,
		HeadSha:              firstNonEmpty(invocation.HeadSHA, ref.HeadSHA),
		ProviderJobIds:       state.SuccessfulJobIDs,
		OrgID:                dbOrgID(ref.OrgID),
		Provider:             ref.Provider,
		ProviderRepositoryID: ref.ProviderRepositoryID,
		ScopeRef:             "refs/heads/" + goldenWorkspaceDogfoodBranch,
	})
	if err != nil {
		return false, err
	}
	span.SetAttributes(attribute.Int("golden.candidate_count", len(candidates)))
	anyPromoted := false
	for _, candidate := range candidates {
		promoted, err := s.promoteGoldenCandidate(ctx, candidate.GoldenScopeID, candidate.OperationID, candidate.GoldenGenerationID, candidate.SourceGenerationID, candidate.ExecutionID, candidate.AttemptID)
		if err != nil {
			return anyPromoted, err
		}
		anyPromoted = anyPromoted || promoted
	}
	span.SetAttributes(attribute.Bool("golden.promoted", anyPromoted))
	return anyPromoted, nil
}

func (s *Service) promoteGoldenCandidate(ctx context.Context, scopeID, operationID, generationID uuid.UUID, sourceGenerationID *uuid.UUID, executionID, attemptID uuid.UUID) (bool, error) {
	ctx, span := tracer.Start(ctx, "golden.generation.promote", trace.WithAttributes(
		attribute.String("golden.operation_id", operationID.String()),
		attribute.String("golden.scope_id", scopeID.String()),
		attribute.String("golden.generation_id", generationID.String()),
		attribute.String("golden.source_generation_id", uuidPtrString(sourceGenerationID)),
	))
	defer span.End()
	now := time.Now().UTC()
	current := generationID
	op := operationID
	if sourceGenerationID == nil {
		rows, err := s.storeQueries().InsertGoldenCurrentPointer(ctx, store.InsertGoldenCurrentPointerParams{
			GoldenScopeID:       scopeID,
			CurrentGenerationID: &current,
			OperationID:         &op,
			PromotedAt:          pgTime(now),
		})
		if err != nil {
			return false, err
		}
		if rows > 0 {
			_ = s.appendGoldenEvent(ctx, goldenEvent{OperationID: &operationID, ScopeID: &scopeID, GenerationID: &generationID, ExecutionID: &executionID, AttemptID: &attemptID, Name: "golden.generation.promote", Result: "succeeded"})
			return true, nil
		}
	}
	rows, err := s.storeQueries().PromoteGoldenGenerationCAS(ctx, store.PromoteGoldenGenerationCASParams{
		NewGenerationID:      &current,
		OperationID:          &op,
		PromotedAt:           pgTime(now),
		GoldenScopeID:        scopeID,
		ExpectedGenerationID: sourceGenerationID,
	})
	if err != nil {
		return false, err
	}
	result := "conflicted"
	if rows > 0 {
		result = "succeeded"
	}
	_ = s.appendGoldenEvent(ctx, goldenEvent{OperationID: &operationID, ScopeID: &scopeID, GenerationID: &generationID, ExecutionID: &executionID, AttemptID: &attemptID, Name: "golden.generation.promote", Result: result})
	return rows > 0, nil
}

func goldenRunRefFromRunnerIdentity(identity RunnerExecutionIdentity) goldenWorkflowRunRef {
	return goldenWorkflowRunRef{
		OrgID:                  identity.OrgID,
		Provider:               identity.Provider,
		ProviderInstallationID: identity.ProviderInstallationID,
		ProviderRepositoryID:   identity.ProviderRepositoryID,
		ProviderRunID:          identity.ProviderRunID,
		ProviderRunAttempt:     identity.ProviderRunAttempt,
		RepositoryFullName:     identity.RepositoryFullName,
		HeadSHA:                identity.HeadSHA,
	}
}

type goldenEvent struct {
	OperationID  *uuid.UUID
	ScopeID      *uuid.UUID
	GenerationID *uuid.UUID
	ExecutionID  *uuid.UUID
	AttemptID    *uuid.UUID
	Name         string
	Result       string
	Reason       string
}

func (s *Service) appendGoldenEvent(ctx context.Context, event goldenEvent) error {
	return s.storeQueries().InsertGoldenEvent(ctx, store.InsertGoldenEventParams{
		EventID:            uuid.New(),
		OperationID:        event.OperationID,
		GoldenScopeID:      event.ScopeID,
		GoldenGenerationID: event.GenerationID,
		ExecutionID:        event.ExecutionID,
		AttemptID:          event.AttemptID,
		EventName:          event.Name,
		Result:             event.Result,
		Reason:             event.Reason,
		ObservedAt:         pgTime(time.Now().UTC()),
	})
}

func stableUUID(parts ...string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.Join(parts, "\x00")))
}

func githubMatrixKey(identity RunnerExecutionIdentity) string {
	return firstNonEmpty(strings.TrimSpace(identity.JobName), "default")
}

func goldenWorkspacePromotionCandidate(identity RunnerExecutionIdentity) bool {
	return strings.TrimSpace(firstNonEmpty(identity.RunHeadBranch, identity.HeadBranch)) == goldenWorkspaceDogfoodBranch
}

func stableHex(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func uuidPtrString(value *uuid.UUID) string {
	if value == nil {
		return ""
	}
	return value.String()
}

func boolResult(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}
