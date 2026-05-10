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
	branch := strings.TrimSpace(identity.HeadBranch)
	if branch == "" {
		branch = "unknown"
	}
	scopeRef := "refs/heads/" + branch
	trustClass := goldenTrustClass(identity)
	workflowIdentity := firstNonEmpty(identity.WorkflowName, "github-actions")
	jobIdentity := firstNonEmpty(identity.JobName, identity.RunnerClass, "github-job")
	durableHash := stableHex("durable", goldenWorkspaceComponentKind, githubRunnerDurableWorkDir)
	checkoutHash := stableHex("checkout", "v0", "preserve-untracked")
	now := time.Now().UTC()

	jobShapeID := stableUUID("job-shape", strconv.FormatUint(identity.OrgID, 10), identity.Provider, strconv.FormatInt(identity.ProviderRepositoryID, 10), workflowIdentity, jobIdentity, identity.RunnerClass, goldenWorkspacePlatformImage, durableHash, checkoutHash)
	scopeID := stableUUID("golden-scope", strconv.FormatUint(identity.OrgID, 10), identity.Provider, strconv.FormatInt(identity.ProviderRepositoryID, 10), scopeRef, jobShapeID.String(), trustClass)
	operationID := uuid.New()
	candidateGenerationID := uuid.New()

	shape, err := s.storeQueries().UpsertJobShape(ctx, store.UpsertJobShapeParams{
		JobShapeID:             jobShapeID,
		OrgID:                  dbOrgID(identity.OrgID),
		Provider:               identity.Provider,
		ProviderRepositoryID:   identity.ProviderRepositoryID,
		WorkflowIdentity:       workflowIdentity,
		CalledWorkflowIdentity: "",
		JobIdentity:            jobIdentity,
		MatrixKey:              "",
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
	if finalExec.ExitCode != 0 || finalExec.State == vmorchestrator.ExecStateFailed {
		_ = s.storeQueries().MarkWorkspaceOperationResultRecorded(ctx, store.MarkWorkspaceOperationResultRecordedParams{
			FinalState:  "skipped",
			SealedAt:    pgTime(time.Now().UTC()),
			RecordedAt:  pgTime(time.Now().UTC()),
			OperationID: plan.OperationID,
		})
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
		PromotionEligible:  true,
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
	promotionReady, reason, err := s.githubWorkflowRunPromotionReady(ctx, plan.Identity)
	if err != nil {
		return err
	}
	if !promotionReady {
		span.SetAttributes(attribute.Bool("golden.promoted", false), attribute.String("golden.promotion_deferred_reason", reason))
		_ = s.appendGoldenEvent(ctx, goldenEvent{OperationID: &plan.OperationID, ScopeID: &plan.GoldenScopeID, GenerationID: &gen.GoldenGenerationID, ExecutionID: &item.ExecutionID, AttemptID: &item.AttemptID, Name: "golden.generation.promote", Result: "deferred", Reason: reason})
		return nil
	}
	promoted, err := s.promoteGoldenRun(ctx, plan.Identity)
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

func (s *Service) githubWorkflowRunPromotionReady(ctx context.Context, identity RunnerExecutionIdentity) (bool, string, error) {
	if identity.Provider != RunnerProviderGitHub {
		return true, "", nil
	}
	if s.GitHubRunner == nil {
		return false, "github runner is not configured", nil
	}
	state, err := s.GitHubRunner.refreshWorkflowRunJobs(ctx, identity)
	if err != nil {
		return false, "", err
	}
	ready, reason := state.promotionReady()
	return ready, reason, nil
}

func (s *Service) promoteGoldenRun(ctx context.Context, identity RunnerExecutionIdentity) (bool, error) {
	candidates, err := s.storeQueries().ListGoldenPromotionCandidatesForRun(ctx, store.ListGoldenPromotionCandidatesForRunParams{
		ProviderRunID:        identity.ProviderRunID,
		HeadSha:              identity.HeadSHA,
		OrgID:                dbOrgID(identity.OrgID),
		Provider:             identity.Provider,
		ProviderRepositoryID: identity.ProviderRepositoryID,
	})
	if err != nil {
		return false, err
	}
	anyPromoted := false
	for _, candidate := range candidates {
		promoted, err := s.promoteGoldenCandidate(ctx, identity, candidate.GoldenScopeID, candidate.OperationID, candidate.GoldenGenerationID, candidate.SourceGenerationID)
		if err != nil {
			return anyPromoted, err
		}
		anyPromoted = anyPromoted || promoted
	}
	return anyPromoted, nil
}

func (s *Service) promoteGoldenCandidate(ctx context.Context, identity RunnerExecutionIdentity, scopeID, operationID, generationID uuid.UUID, sourceGenerationID *uuid.UUID) (bool, error) {
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
			_ = s.appendGoldenEvent(ctx, goldenEvent{OperationID: &operationID, ScopeID: &scopeID, GenerationID: &generationID, ExecutionID: &identity.ExecutionID, AttemptID: &identity.AttemptID, Name: "golden.generation.promote", Result: "succeeded"})
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
	_ = s.appendGoldenEvent(ctx, goldenEvent{OperationID: &operationID, ScopeID: &scopeID, GenerationID: &generationID, ExecutionID: &identity.ExecutionID, AttemptID: &identity.AttemptID, Name: "golden.generation.promote", Result: result})
	return rows > 0, nil
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

func goldenTrustClass(identity RunnerExecutionIdentity) string {
	switch strings.TrimSpace(identity.HeadBranch) {
	case "main", "master", "beta", "gamma":
		return "protected_branch"
	case "":
		return "unknown_branch"
	default:
		return "same_repo_branch"
	}
}

func stableUUID(parts ...string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.Join(parts, "\x00")))
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
