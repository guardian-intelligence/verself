package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"sort"
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
	"gopkg.in/yaml.v3"
)

const (
	durableWorkspaceMountName     = "github-workspace"
	durableWorkspaceComponentKind = "github_workspace"
	durableCacheComponentKind     = "cache_volume"
	durableTrustProtectedBranch   = "protected_branch"
	durablePlatformImage          = "ubuntu-2404-actions-runner"
	durableGuestArch              = "x86_64"
	durableToolchainImage         = "gh-actions-runner"
	durableDogfoodBranch          = "main"
	durableScopeKindBranch        = "branch"
	durableCacheManifestPath      = ".verself/cache.yml"
	durableCacheActionRef         = "guardian-intelligence/verself-cache@v0"
	durableCacheMountRoot         = "/verself/.mounts"
	durableDefaultCacheBytes      = uint64(1 << 30)
	durableMaxCacheBytes          = uint64(1 << 40)
	durableMaxVolumesPerJob       = 99
)

var ErrCacheDeclarationInvalid = errors.New("sandbox-rental: cache declaration invalid")

type durableVolumePlan struct {
	Enabled    bool
	Identity   RunnerExecutionIdentity
	Operations []durableVolumeOperation
}

type durableVolumeOperation struct {
	OperationID           uuid.UUID
	DurableScopeID        uuid.UUID
	SourceGenerationID    *uuid.UUID
	SourceSnapshotRef     string
	CandidateGenerationID uuid.UUID
	MountName             string
	MountPath             string
	BindPaths             []string
	SizeBytes             uint64
	TrustClass            string
	ComponentKind         string
	ComponentName         string
	PromotionEligible     bool
	Required              bool
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

type cacheDeclaration struct {
	SourceKind       string            `json:"-"`
	SourcePath       string            `json:"-"`
	SourceSHA        string            `json:"-"`
	WorkflowIdentity string            `json:"-"`
	JobIdentity      string            `json:"-"`
	StepIdentity     string            `json:"-"`
	Version          int               `json:"version"`
	Volumes          []cacheVolumeDecl `json:"cache"`
}

type cacheVolumeDecl struct {
	Name      string   `json:"name" yaml:"name"`
	Size      string   `json:"size,omitempty" yaml:"size"`
	SizeBytes uint64   `json:"size_bytes" yaml:"-"`
	Paths     []string `json:"paths" yaml:"paths"`
}

type cacheManifestFile struct {
	Version int               `yaml:"version"`
	Cache   []cacheVolumeDecl `yaml:"cache"`
}

type durableEvent struct {
	OperationID           *uuid.UUID
	ScopeID               *uuid.UUID
	GenerationID          *uuid.UUID
	SourceGenerationID    *uuid.UUID
	CandidateGenerationID *uuid.UUID
	CurrentGenerationID   *uuid.UUID
	ExecutionID           *uuid.UUID
	AttemptID             *uuid.UUID
	Identity              RunnerExecutionIdentity
	ComponentKind         string
	ComponentName         string
	Name                  string
	Result                string
	Reason                string
	MountName             string
	ZFSSnapshotRef        string
	UsedBytes             uint64
	WrittenBytes          uint64
}

type durableEventRow struct {
	ObservedAt            time.Time `ch:"observed_at"`
	OrgID                 uint64    `ch:"org_id"`
	RepositoryID          uint64    `ch:"repository_id"`
	Provider              string    `ch:"provider"`
	ProviderRepositoryID  uint64    `ch:"provider_repository_id"`
	ProviderRunID         uint64    `ch:"provider_run_id"`
	ProviderRunAttempt    uint64    `ch:"provider_run_attempt"`
	ProviderJobID         uint64    `ch:"provider_job_id"`
	ExecutionID           uuid.UUID `ch:"execution_id"`
	AttemptID             uuid.UUID `ch:"attempt_id"`
	OperationID           uuid.UUID `ch:"operation_id"`
	DurableScopeID        uuid.UUID `ch:"durable_scope_id"`
	DurableGenerationID   uuid.UUID `ch:"durable_generation_id"`
	ComponentKind         string    `ch:"component_kind"`
	ComponentName         string    `ch:"component_name"`
	EventName             string    `ch:"event_name"`
	Result                string    `ch:"result"`
	Reason                string    `ch:"reason"`
	MountName             string    `ch:"mount_name"`
	SourceGenerationID    uuid.UUID `ch:"source_generation_id"`
	CandidateGenerationID uuid.UUID `ch:"candidate_generation_id"`
	CurrentGenerationID   uuid.UUID `ch:"current_generation_id"`
	ZFSSnapshotRef        string    `ch:"zfs_snapshot_ref"`
	UsedBytes             uint64    `ch:"used_bytes"`
	WrittenBytes          uint64    `ch:"written_bytes"`
	TraceID               string    `ch:"trace_id"`
	SpanID                string    `ch:"span_id"`
}

func (p durableVolumePlan) filesystemMounts() []vmorchestrator.FilesystemMount {
	if !p.Enabled {
		return nil
	}
	mounts := make([]vmorchestrator.FilesystemMount, 0, len(p.Operations))
	for _, op := range p.Operations {
		mounts = append(mounts, vmorchestrator.FilesystemMount{
			Name:        op.MountName,
			OperationID: op.OperationID.String(),
			SourceRef:   op.SourceSnapshotRef,
			MountPath:   op.MountPath,
			BindPaths:   append([]string(nil), op.BindPaths...),
			FSType:      "ext4",
			ReadOnly:    false,
			Required:    op.Required,
			SizeBytes:   op.SizeBytes,
		})
	}
	return mounts
}

func (s *Service) prepareDurableVolumes(ctx context.Context, item executionWorkItem) (durableVolumePlan, error) {
	if item.WorkloadKind != WorkloadKindRunner || item.ExternalProvider != RunnerProviderGitHub {
		return durableVolumePlan{}, nil
	}
	ctx, span := tracer.Start(ctx, "durable.volume.select", trace.WithAttributes(
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
		return durableVolumePlan{}, err
	}
	if identity.Provider != RunnerProviderGitHub {
		return durableVolumePlan{}, nil
	}
	identity, err = s.hydrateGitHubRunIdentity(ctx, identity)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return durableVolumePlan{}, err
	}

	decl, err := s.resolveCacheDeclaration(ctx, identity)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return durableVolumePlan{}, err
	}
	if len(decl.Volumes)+1 > durableMaxVolumesPerJob {
		return durableVolumePlan{}, fmt.Errorf("%w: requested %d durable volumes, maximum is %d", ErrCacheDeclarationInvalid, len(decl.Volumes)+1, durableMaxVolumesPerJob)
	}

	normalizedJSON, declarationHash, err := normalizedDeclarationJSON(decl)
	if err != nil {
		return durableVolumePlan{}, err
	}
	now := time.Now().UTC()
	declarationID := stableUUID("cache-declaration", strconv.FormatInt(identity.ProviderRepositoryID, 10), declarationHash, decl.SourceKind, decl.SourcePath, decl.WorkflowIdentity, decl.JobIdentity, decl.StepIdentity)
	declarationSHA := stableHex(string(normalizedJSON))
	cacheDeclarationID, err := s.storeQueries().UpsertCacheDeclaration(ctx, store.UpsertCacheDeclarationParams{
		CacheDeclarationID: declarationID,
		RepositoryID:       identity.ProviderRepositoryID,
		SourceKind:         decl.SourceKind,
		SourceRef:          durableScopeRef(identity),
		SourceSha:          firstNonEmpty(decl.SourceSHA, identity.HeadSHA, identity.RunHeadSHA),
		SourcePath:         decl.SourcePath,
		WorkflowIdentity:   decl.WorkflowIdentity,
		JobIdentity:        decl.JobIdentity,
		StepIdentity:       decl.StepIdentity,
		DeclarationSha256:  declarationSHA,
		DeclarationHash:    declarationHash,
		NormalizedJson:     normalizedJSON,
		ParsedAt:           pgTime(now),
	})
	if err != nil {
		return durableVolumePlan{}, fmt.Errorf("upsert cache declaration: %w", err)
	}
	for _, volume := range decl.Volumes {
		pathsJSON, pathHash, err := normalizedPathsJSON(volume.Paths)
		if err != nil {
			return durableVolumePlan{}, err
		}
		if err := s.storeQueries().InsertCacheVolumeSpec(ctx, store.InsertCacheVolumeSpecParams{
			CacheVolumeSpecID:   stableUUID("cache-volume-spec", cacheDeclarationID.String(), volume.Name),
			CacheDeclarationID:  cacheDeclarationID,
			Name:                volume.Name,
			SizeBytes:           mustInt64FromUint64(volume.SizeBytes, "cache volume size"),
			PathSetHash:         pathHash,
			MountPolicyHash:     stableHex("bind", "noatime", "nodev", "nosuid"),
			NormalizedPathsJson: pathsJSON,
			CreatedAt:           pgTime(now),
		}); err != nil {
			return durableVolumePlan{}, fmt.Errorf("insert cache volume spec %s: %w", volume.Name, err)
		}
	}

	workflowIdentity := firstNonEmpty(identity.WorkflowName, "github-actions")
	jobIdentity := firstNonEmpty(identity.JobName, identity.RunnerClass, "github-job")
	matrixKey := githubMatrixKey(identity)
	workspacePolicyHash := stableHex("workspace", "v0", githubRunnerDurableWorkDir, "preserve-untracked")
	jobShapeID := stableUUID("durable-job-shape", strconv.FormatUint(identity.OrgID, 10), identity.Provider, strconv.FormatInt(identity.ProviderRepositoryID, 10), workflowIdentity, jobIdentity, matrixKey, identity.RunnerClass, durablePlatformImage, durableGuestArch, workspacePolicyHash, declarationHash)
	shape, err := s.storeQueries().UpsertJobShape(ctx, store.UpsertJobShapeParams{
		JobShapeID:             jobShapeID,
		RepositoryID:           identity.ProviderRepositoryID,
		Provider:               identity.Provider,
		WorkflowIdentity:       workflowIdentity,
		CalledWorkflowIdentity: "",
		JobIdentity:            jobIdentity,
		MatrixKey:              matrixKey,
		RunnerClass:            identity.RunnerClass,
		GuestArch:              durableGuestArch,
		PlatformImageID:        durablePlatformImage,
		KernelImageID:          "default",
		RunnerToolchainImageID: durableToolchainImage,
		WorkspacePolicyHash:    workspacePolicyHash,
		CacheDeclarationHash:   declarationHash,
		CreatedAt:              pgTime(now),
	})
	if err != nil {
		return durableVolumePlan{}, fmt.Errorf("upsert durable job shape: %w", err)
	}

	scopeRef := durableScopeRef(identity)
	promotionEligible := durablePromotionCandidate(identity)
	plan := durableVolumePlan{Enabled: true, Identity: identity}
	workspace, err := s.insertDurableOperation(ctx, item, identity, durableOperationSpec{
		JobShapeID:        shape,
		ScopeRef:          scopeRef,
		ComponentKind:     durableWorkspaceComponentKind,
		ComponentName:     "repo-workspace",
		MountName:         durableWorkspaceMountName,
		MountPath:         githubRunnerDurableWorkDir,
		BindPaths:         nil,
		SizeBytes:         100 * 1024 * 1024 * 1024,
		TrustClass:        durableTrustProtectedBranch,
		PromotionEligible: promotionEligible,
		Required:          true,
		Now:               now,
	})
	if err != nil {
		return durableVolumePlan{}, err
	}
	plan.Operations = append(plan.Operations, workspace)
	for _, volume := range decl.Volumes {
		op, err := s.insertDurableOperation(ctx, item, identity, durableOperationSpec{
			JobShapeID:        shape,
			ScopeRef:          scopeRef,
			ComponentKind:     durableCacheComponentKind,
			ComponentName:     volume.Name,
			MountName:         "cache-" + sanitizeMountName(volume.Name),
			MountPath:         path.Join(durableCacheMountRoot, volume.Name),
			BindPaths:         volume.Paths,
			SizeBytes:         volume.SizeBytes,
			TrustClass:        durableTrustProtectedBranch,
			PromotionEligible: promotionEligible,
			Required:          false,
			Now:               now,
		})
		if err != nil {
			return durableVolumePlan{}, err
		}
		plan.Operations = append(plan.Operations, op)
	}
	span.SetAttributes(
		attribute.Int("durable.volume_count", len(plan.Operations)),
		attribute.String("durable.cache_declaration_hash", declarationHash),
		attribute.Bool("durable.promotion_candidate", promotionEligible),
		attribute.String("github.repository", identity.RepositoryFullName),
		attribute.String("github.scope_ref", scopeRef),
	)
	for _, op := range plan.Operations {
		_ = s.appendDurableEvent(ctx, durableEvent{OperationID: &op.OperationID, ScopeID: &op.DurableScopeID, SourceGenerationID: op.SourceGenerationID, CandidateGenerationID: &op.CandidateGenerationID, ExecutionID: &item.ExecutionID, AttemptID: &item.AttemptID, Identity: identity, ComponentKind: op.ComponentKind, ComponentName: op.ComponentName, Name: "durable.volume.select", Result: boolResult(op.SourceSnapshotRef != "", "hit", "miss"), MountName: op.MountName})
	}
	return plan, nil
}

type durableOperationSpec struct {
	JobShapeID        uuid.UUID
	ScopeRef          string
	ComponentKind     string
	ComponentName     string
	MountName         string
	MountPath         string
	BindPaths         []string
	SizeBytes         uint64
	TrustClass        string
	PromotionEligible bool
	Required          bool
	Now               time.Time
}

func (s *Service) insertDurableOperation(ctx context.Context, item executionWorkItem, identity RunnerExecutionIdentity, spec durableOperationSpec) (durableVolumeOperation, error) {
	scopeID := stableUUID("durable-scope", strconv.FormatUint(identity.OrgID, 10), identity.Provider, strconv.FormatInt(identity.ProviderRepositoryID, 10), spec.ScopeRef, spec.JobShapeID.String(), spec.ComponentKind, spec.ComponentName, spec.TrustClass)
	scope, err := s.storeQueries().UpsertDurableScope(ctx, store.UpsertDurableScopeParams{
		DurableScopeID:       scopeID,
		RepositoryID:         identity.ProviderRepositoryID,
		Provider:             identity.Provider,
		ProviderRepositoryID: identity.ProviderRepositoryID,
		ScopeKind:            durableScopeKindBranch,
		ScopeRef:             spec.ScopeRef,
		JobShapeID:           spec.JobShapeID,
		ComponentName:        spec.ComponentName,
		ComponentKind:        spec.ComponentKind,
		TrustClass:           spec.TrustClass,
		CreatedAt:            pgTime(spec.Now),
	})
	if err != nil {
		return durableVolumeOperation{}, fmt.Errorf("upsert durable scope %s: %w", spec.ComponentName, err)
	}
	if err := s.storeQueries().EnsureDurableCurrentPointer(ctx, store.EnsureDurableCurrentPointerParams{DurableScopeID: scope, PromotedAt: pgTime(spec.Now)}); err != nil {
		return durableVolumeOperation{}, fmt.Errorf("ensure durable current pointer %s: %w", spec.ComponentName, err)
	}
	var sourceGenerationID *uuid.UUID
	sourceSnapshotRef := ""
	current, err := s.storeQueries().GetCurrentDurableGeneration(ctx, store.GetCurrentDurableGenerationParams{DurableScopeID: scope})
	if err == nil {
		sourceGenerationID = &current.DurableGenerationID
		sourceSnapshotRef = current.ZfsSnapshotRef
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return durableVolumeOperation{}, fmt.Errorf("select current durable generation %s: %w", spec.ComponentName, err)
	}
	bindPathsJSON, err := json.Marshal(spec.BindPaths)
	if err != nil {
		return durableVolumeOperation{}, err
	}
	operationID := uuid.New()
	candidateGenerationID := uuid.New()
	op, err := s.storeQueries().InsertDurableOperation(ctx, store.InsertDurableOperationParams{
		OperationID:           operationID,
		ExecutionID:           item.ExecutionID,
		AttemptID:             item.AttemptID,
		AllocationID:          &identity.AllocationID,
		DurableScopeID:        scope,
		SourceGenerationID:    sourceGenerationID,
		SourceSnapshotRef:     sourceSnapshotRef,
		CandidateGenerationID: candidateGenerationID,
		MountName:             spec.MountName,
		InternalMountPath:     spec.MountPath,
		BindPathsJson:         bindPathsJSON,
		SizeBytes:             mustInt64FromUint64(spec.SizeBytes, "durable operation size"),
		TrustClass:            spec.TrustClass,
		RequestedAt:           pgTime(spec.Now),
	})
	if err != nil {
		return durableVolumeOperation{}, fmt.Errorf("insert durable operation %s: %w", spec.ComponentName, err)
	}
	return durableVolumeOperation{
		OperationID:           op.OperationID,
		DurableScopeID:        op.DurableScopeID,
		SourceGenerationID:    op.SourceGenerationID,
		SourceSnapshotRef:     op.SourceSnapshotRef,
		CandidateGenerationID: op.CandidateGenerationID,
		MountName:             op.MountName,
		MountPath:             op.InternalMountPath,
		BindPaths:             append([]string(nil), spec.BindPaths...),
		SizeBytes:             uint64FromInt64(op.SizeBytes, "durable operation size"),
		TrustClass:            op.TrustClass,
		ComponentKind:         spec.ComponentKind,
		ComponentName:         spec.ComponentName,
		PromotionEligible:     spec.PromotionEligible,
		Required:              spec.Required,
	}, nil
}

func (s *Service) markDurableVolumesMounted(ctx context.Context, plan durableVolumePlan) {
	if !plan.Enabled {
		return
	}
	now := time.Now().UTC()
	for _, op := range plan.Operations {
		if err := s.storeQueries().MarkDurableOperationMounted(ctx, store.MarkDurableOperationMountedParams{MountedAt: pgTime(now), OperationID: op.OperationID}); err != nil && s.Logger != nil {
			s.Logger.WarnContext(ctx, "mark durable volume mounted failed", "operation_id", op.OperationID, "error", err)
		}
		_ = s.appendDurableEvent(ctx, durableEvent{OperationID: &op.OperationID, ScopeID: &op.DurableScopeID, SourceGenerationID: op.SourceGenerationID, CandidateGenerationID: &op.CandidateGenerationID, ExecutionID: &plan.Identity.ExecutionID, AttemptID: &plan.Identity.AttemptID, Identity: plan.Identity, ComponentKind: op.ComponentKind, ComponentName: op.ComponentName, Name: "durable.volume.mount", Result: "mounted", MountName: op.MountName})
	}
}

func (s *Service) failDurableVolumes(ctx context.Context, plan durableVolumePlan, reason string, cause error) {
	if !plan.Enabled {
		return
	}
	failureReason := durableFailureReason(reason, cause)
	now := time.Now().UTC()
	for _, op := range plan.Operations {
		if err := s.storeQueries().MarkDurableOperationFailed(ctx, store.MarkDurableOperationFailedParams{FailureReason: failureReason, RecordedAt: pgTime(now), OperationID: op.OperationID}); err != nil && s.Logger != nil {
			s.Logger.WarnContext(ctx, "mark durable volume failed", "operation_id", op.OperationID, "error", err)
		}
		_ = s.appendDurableEvent(ctx, durableEvent{OperationID: &op.OperationID, ScopeID: &op.DurableScopeID, SourceGenerationID: op.SourceGenerationID, CandidateGenerationID: &op.CandidateGenerationID, ExecutionID: &plan.Identity.ExecutionID, AttemptID: &plan.Identity.AttemptID, Identity: plan.Identity, ComponentKind: op.ComponentKind, ComponentName: op.ComponentName, Name: "durable.volume.prepare", Result: "failed", Reason: failureReason, MountName: op.MountName})
	}
}

func (s *Service) failOpenDurableOperationsForAttempt(ctx context.Context, item executionWorkItem, reason string, cause error) {
	failureReason := durableFailureReason(reason, cause)
	rows, err := s.storeQueries().MarkOpenDurableOperationsFailedByAttempt(ctx, store.MarkOpenDurableOperationsFailedByAttemptParams{FailureReason: failureReason, RecordedAt: pgTime(time.Now().UTC()), AttemptID: item.AttemptID})
	if err != nil {
		if s.Logger != nil {
			s.Logger.WarnContext(ctx, "mark open durable operations failed", "attempt_id", item.AttemptID, "error", err)
		}
		return
	}
	for _, row := range rows {
		_ = s.appendDurableEvent(ctx, durableEvent{OperationID: &row.OperationID, ScopeID: &row.DurableScopeID, ExecutionID: &row.ExecutionID, AttemptID: &row.AttemptID, Name: "durable.volume.reconcile", Result: "failed", Reason: failureReason})
	}
}

func (s *Service) finalizeDurableVolumes(ctx context.Context, item executionWorkItem, leaseID string, plan durableVolumePlan, finalExec vmorchestrator.ExecRecord) error {
	if !plan.Enabled {
		return nil
	}
	ctx, span := tracer.Start(ctx, "durable.volume.seal", trace.WithAttributes(
		attribute.String("lease.id", leaseID),
		attribute.Int("durable.volume_count", len(plan.Operations)),
	))
	defer span.End()
	var errs []error
	anyCandidate := false
	for _, op := range plan.Operations {
		allowed, skipReason := durableSealAllowed(op, finalExec)
		if !allowed {
			now := time.Now().UTC()
			_ = s.storeQueries().MarkDurableOperationResultRecorded(ctx, store.MarkDurableOperationResultRecordedParams{FinalState: "skipped", SealedAt: pgTime(now), RecordedAt: pgTime(now), FailureReason: skipReason, OperationID: op.OperationID})
			_ = s.appendDurableEvent(ctx, durableEvent{OperationID: &op.OperationID, ScopeID: &op.DurableScopeID, SourceGenerationID: op.SourceGenerationID, CandidateGenerationID: &op.CandidateGenerationID, ExecutionID: &item.ExecutionID, AttemptID: &item.AttemptID, Identity: plan.Identity, ComponentKind: op.ComponentKind, ComponentName: op.ComponentName, Name: "durable.volume.seal", Result: "skipped", Reason: skipReason, MountName: op.MountName})
			continue
		}
		if err := s.storeQueries().MarkDurableOperationSealStarted(ctx, store.MarkDurableOperationSealStartedParams{SealStartedAt: pgTime(time.Now().UTC()), OperationID: op.OperationID}); err != nil {
			errs = append(errs, err)
			continue
		}
		commit, err := s.Orchestrator.CommitFilesystemMount(ctx, leaseID, op.OperationID.String()+":commit", op.OperationID.String(), op.MountName, op.DurableScopeID.String(), op.SourceSnapshotRef, op.CandidateGenerationID.String())
		if err != nil {
			_ = s.storeQueries().MarkDurableOperationFailed(ctx, store.MarkDurableOperationFailedParams{FailureReason: err.Error(), RecordedAt: pgTime(time.Now().UTC()), OperationID: op.OperationID})
			_ = s.appendDurableEvent(ctx, durableEvent{OperationID: &op.OperationID, ScopeID: &op.DurableScopeID, SourceGenerationID: op.SourceGenerationID, CandidateGenerationID: &op.CandidateGenerationID, ExecutionID: &item.ExecutionID, AttemptID: &item.AttemptID, Identity: plan.Identity, ComponentKind: op.ComponentKind, ComponentName: op.ComponentName, Name: "durable.volume.seal", Result: "failed", Reason: err.Error(), MountName: op.MountName})
			errs = append(errs, fmt.Errorf("commit durable volume %s: %w", op.ComponentName, err))
			continue
		}
		usedBytes, err := int64FromUint64("durable used bytes", commit.UsedBytes)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		writtenBytes, err := int64FromUint64("durable written bytes", commit.WrittenBytes)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		now := time.Now().UTC()
		generationID, err := s.storeQueries().InsertDurableGeneration(ctx, store.InsertDurableGenerationParams{
			DurableGenerationID: op.CandidateGenerationID,
			DurableScopeID:      op.DurableScopeID,
			OperationID:         op.OperationID,
			SourceGenerationID:  op.SourceGenerationID,
			HeadSha:             firstNonEmpty(plan.Identity.HeadSHA, plan.Identity.RunHeadSHA),
			TreeHash:            "",
			ProviderRunID:       plan.Identity.ProviderRunID,
			ProviderRunAttempt:  plan.Identity.ProviderRunAttempt,
			ProviderJobID:       plan.Identity.ProviderJobID,
			Result:              "success",
			PromotionEligible:   op.PromotionEligible,
			State:               "committed",
			ZfsSnapshotRef:      commit.Snapshot,
			UsedBytes:           usedBytes,
			WrittenBytes:        writtenBytes,
			SealedAt:            pgTime(commit.CommittedAt),
			CommittedAt:         pgTime(now),
			LastUsedAt:          pgTime(now),
			ExpiresAt:           pgOptionalTime(time.Time{}),
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("insert durable generation %s: %w", op.ComponentName, err))
			continue
		}
		if err := s.storeQueries().MarkDurableOperationResultRecorded(ctx, store.MarkDurableOperationResultRecordedParams{FinalState: "committed", SealedAt: pgTime(commit.CommittedAt), RecordedAt: pgTime(now), FailureReason: "", OperationID: op.OperationID}); err != nil {
			errs = append(errs, err)
			continue
		}
		_ = s.appendDurableEvent(ctx, durableEvent{OperationID: &op.OperationID, ScopeID: &op.DurableScopeID, GenerationID: &generationID, SourceGenerationID: op.SourceGenerationID, CandidateGenerationID: &op.CandidateGenerationID, ExecutionID: &item.ExecutionID, AttemptID: &item.AttemptID, Identity: plan.Identity, ComponentKind: op.ComponentKind, ComponentName: op.ComponentName, Name: "durable.volume.commit", Result: "succeeded", MountName: op.MountName, ZFSSnapshotRef: commit.Snapshot, UsedBytes: commit.UsedBytes, WrittenBytes: commit.WrittenBytes})
		anyCandidate = anyCandidate || op.PromotionEligible
	}
	if anyCandidate {
		if _, err := s.promoteDurableWorkflowRun(ctx, goldenRunRefFromRunnerIdentity(plan.Identity)); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func durableSealAllowed(op durableVolumeOperation, finalExec vmorchestrator.ExecRecord) (bool, string) {
	if finalExec.ExitCode != 0 || finalExec.State == vmorchestrator.ExecStateFailed {
		reason := "exec_failed"
		if strings.TrimSpace(finalExec.TerminalReason) != "" {
			reason += ": " + strings.TrimSpace(finalExec.TerminalReason)
		}
		return false, reason
	}
	if !op.PromotionEligible {
		return false, "non_promotable_scope"
	}
	return true, ""
}

func (s *Service) PromoteGoldenRun(ctx context.Context, req scheduler.GoldenRunPromoteArgs) error {
	if strings.TrimSpace(req.Provider) != RunnerProviderGitHub {
		return fmt.Errorf("durable run promotion unsupported provider %q", req.Provider)
	}
	repositoryFullName := strings.TrimSpace(req.RepositoryFullName)
	installationID := req.ProviderInstallationID
	var orgID uint64
	repository, err := s.storeQueries().GetDurableRunRepository(ctx, store.GetDurableRunRepositoryParams{ProviderRepositoryID: req.ProviderRepositoryID, ProviderRunID: req.ProviderRunID})
	if err == nil {
		repositoryFullName = firstNonEmpty(repositoryFullName, repository.RepositoryFullName)
		installationID = firstNonZero(installationID, repository.ProviderInstallationID)
		orgID = orgIDFromDB(repository.OrgID)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load durable run repository: %w", err)
	}
	if repositoryFullName == "" || installationID == 0 {
		return nil
	}
	_, err = s.promoteDurableWorkflowRun(ctx, goldenWorkflowRunRef{OrgID: orgID, Provider: RunnerProviderGitHub, ProviderInstallationID: installationID, ProviderRepositoryID: req.ProviderRepositoryID, ProviderRunID: req.ProviderRunID, ProviderRunAttempt: req.ProviderRunAttempt, RepositoryFullName: repositoryFullName, HeadSHA: req.HeadSHA})
	return err
}

func (s *Service) hydrateGitHubRunIdentity(ctx context.Context, identity RunnerExecutionIdentity) (RunnerExecutionIdentity, error) {
	if identity.Provider != RunnerProviderGitHub || s.GitHubRunner == nil || identity.ProviderRunID <= 0 {
		return identity, nil
	}
	invocation, err := s.GitHubRunner.refreshWorkflowRunInvocationForRun(ctx, goldenRunRefFromRunnerIdentity(identity))
	if err != nil {
		return RunnerExecutionIdentity{}, fmt.Errorf("refresh github workflow run invocation: %w", err)
	}
	identity.RepositoryFullName = firstNonEmpty(identity.RepositoryFullName, invocation.RepositoryFullName)
	identity.RunEventName = firstNonEmpty(invocation.EventName, identity.RunEventName)
	identity.RunHeadSHA = firstNonEmpty(invocation.HeadSHA, identity.RunHeadSHA)
	identity.RunHeadBranch = firstNonEmpty(invocation.HeadBranch, identity.RunHeadBranch)
	identity.RunHeadRepository = firstNonEmpty(invocation.HeadRepositoryFullName, identity.RunHeadRepository)
	identity.RunBaseSHA = firstNonEmpty(invocation.BaseSHA, identity.RunBaseSHA)
	identity.RunBaseBranch = firstNonEmpty(invocation.BaseBranch, identity.RunBaseBranch)
	identity.WorkflowPath = firstNonEmpty(invocation.WorkflowPath, identity.WorkflowPath)
	if invocation.PullRequestNumber != 0 {
		identity.PullRequestNumber = invocation.PullRequestNumber
	}
	if identity.HeadSHA == "" {
		identity.HeadSHA = identity.RunHeadSHA
	}
	if identity.HeadBranch == "" {
		identity.HeadBranch = identity.RunHeadBranch
	}
	return identity, nil
}

func (s *Service) promoteDurableWorkflowRun(ctx context.Context, ref goldenWorkflowRunRef) (bool, error) {
	ctx, span := tracer.Start(ctx, "durable.workflow_run.promote", trace.WithAttributes(
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
		span.SetAttributes(attribute.Bool("durable.promoted", false), attribute.String("durable.promotion_deferred_reason", reason))
		return false, nil
	}
	state, err := s.githubWorkflowRunPromotionStateForRef(ctx, ref)
	if err != nil {
		return false, err
	}
	promotionReady, reason := state.promotionReady()
	if !promotionReady {
		span.SetAttributes(attribute.Bool("durable.promoted", false), attribute.String("durable.promotion_deferred_reason", reason))
		return false, nil
	}
	candidates, err := s.storeQueries().ListDurablePromotionCandidatesForRun(ctx, store.ListDurablePromotionCandidatesForRunParams{ProviderRunID: ref.ProviderRunID, ProviderRunAttempt: ref.ProviderRunAttempt, HeadSha: firstNonEmpty(invocation.HeadSHA, ref.HeadSHA)})
	if err != nil {
		return false, err
	}
	span.SetAttributes(attribute.Int("durable.candidate_count", len(candidates)))
	anyPromoted := false
	for _, candidate := range candidates {
		promoted, err := s.promoteDurableCandidate(ctx, candidate, ref)
		if err != nil {
			return anyPromoted, err
		}
		anyPromoted = anyPromoted || promoted
	}
	span.SetAttributes(attribute.Bool("durable.promoted", anyPromoted))
	return anyPromoted, nil
}

func (s *Service) promoteDurableCandidate(ctx context.Context, candidate store.ListDurablePromotionCandidatesForRunRow, ref goldenWorkflowRunRef) (bool, error) {
	ctx, span := tracer.Start(ctx, "durable.volume.promote", trace.WithAttributes(
		attribute.String("durable.operation_id", candidate.OperationID.String()),
		attribute.String("durable.scope_id", candidate.DurableScopeID.String()),
		attribute.String("durable.generation_id", candidate.DurableGenerationID.String()),
		attribute.String("durable.source_generation_id", uuidPtrString(candidate.SourceGenerationID)),
	))
	defer span.End()
	now := time.Now().UTC()
	candidateID := candidate.DurableGenerationID
	operationID := candidate.OperationID
	rows, err := s.storeQueries().PromoteDurableGenerationCAS(ctx, store.PromoteDurableGenerationCASParams{CandidateGenerationID: &candidateID, OperationID: &operationID, PromotedAt: pgTime(now), DurableScopeID: candidate.DurableScopeID, SourceGenerationID: candidate.SourceGenerationID})
	if err != nil {
		return false, err
	}
	result := "conflicted"
	promoted := rows > 0
	if rows > 0 {
		result = "succeeded"
	} else {
		current, err := s.storeQueries().GetCurrentDurableGeneration(ctx, store.GetCurrentDurableGenerationParams{DurableScopeID: candidate.DurableScopeID})
		if err == nil && current.DurableGenerationID == candidateID {
			result = "already_current"
			promoted = true
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return false, err
		} else if err := s.storeQueries().RetainDurableGeneration(ctx, store.RetainDurableGenerationParams{RetainedAt: pgTime(now), DurableGenerationID: candidate.DurableGenerationID, DurableScopeID: candidate.DurableScopeID}); err != nil {
			return false, err
		}
	}
	identity := RunnerExecutionIdentity{OrgID: ref.OrgID, Provider: ref.Provider, ProviderRepositoryID: ref.ProviderRepositoryID, ProviderRunID: ref.ProviderRunID, ProviderRunAttempt: ref.ProviderRunAttempt, ProviderJobID: candidate.ProviderJobID, RepositoryFullName: ref.RepositoryFullName}
	_ = s.appendDurableEvent(ctx, durableEvent{OperationID: &candidate.OperationID, ScopeID: &candidate.DurableScopeID, GenerationID: &candidate.DurableGenerationID, SourceGenerationID: candidate.SourceGenerationID, CurrentGenerationID: &candidateID, ExecutionID: &candidate.ExecutionID, AttemptID: &candidate.AttemptID, Identity: identity, ComponentKind: candidate.ComponentKind, ComponentName: candidate.ComponentName, Name: "durable.volume.promote", Result: result, ZFSSnapshotRef: candidate.ZfsSnapshotRef, UsedBytes: uint64FromInt64(candidate.UsedBytes, "durable used bytes"), WrittenBytes: uint64FromInt64(candidate.WrittenBytes, "durable written bytes")})
	return promoted, nil
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

func (s *Service) resolveCacheDeclaration(ctx context.Context, identity RunnerExecutionIdentity) (cacheDeclaration, error) {
	manifest, manifestPresent, err := s.fetchManifestCacheDeclaration(ctx, identity)
	if err != nil {
		return cacheDeclaration{}, err
	}
	action, actionPresent, err := s.fetchActionCacheDeclaration(ctx, identity)
	if err != nil {
		return cacheDeclaration{}, err
	}
	if manifestPresent && actionPresent {
		_, manifestHash, err := normalizedDeclarationJSON(manifest)
		if err != nil {
			return cacheDeclaration{}, err
		}
		_, actionHash, err := normalizedDeclarationJSON(action)
		if err != nil {
			return cacheDeclaration{}, err
		}
		if manifestHash != actionHash {
			return cacheDeclaration{}, fmt.Errorf("%w: .verself/cache.yml conflicts with %s", ErrCacheDeclarationInvalid, durableCacheActionRef)
		}
		return manifest, nil
	}
	if manifestPresent {
		return manifest, nil
	}
	if actionPresent {
		return action, nil
	}
	return cacheDeclaration{SourceKind: "none", SourceSHA: firstNonEmpty(identity.HeadSHA, identity.RunHeadSHA), Version: 1, Volumes: []cacheVolumeDecl{}}, nil
}

func (s *Service) fetchManifestCacheDeclaration(ctx context.Context, identity RunnerExecutionIdentity) (cacheDeclaration, bool, error) {
	if s.GitHubRunner == nil {
		return cacheDeclaration{}, false, nil
	}
	content, ok, err := s.GitHubRunner.fetchRepositoryFile(ctx, identity, durableCacheManifestPath, firstNonEmpty(identity.HeadSHA, identity.RunHeadSHA))
	if err != nil || !ok {
		return cacheDeclaration{}, ok, err
	}
	decl, err := parseCacheManifest(content, "manifest", durableCacheManifestPath, firstNonEmpty(identity.HeadSHA, identity.RunHeadSHA), "", "", "")
	return decl, true, err
}

func (s *Service) fetchActionCacheDeclaration(ctx context.Context, identity RunnerExecutionIdentity) (cacheDeclaration, bool, error) {
	if s.GitHubRunner == nil || strings.TrimSpace(identity.WorkflowPath) == "" {
		return cacheDeclaration{}, false, nil
	}
	content, ok, err := s.GitHubRunner.fetchRepositoryFile(ctx, identity, identity.WorkflowPath, firstNonEmpty(identity.HeadSHA, identity.RunHeadSHA))
	if err != nil || !ok {
		return cacheDeclaration{}, ok, err
	}
	decl, ok, err := parseWorkflowActionCacheDeclaration(content, identity)
	if !ok || err != nil {
		return cacheDeclaration{}, ok, err
	}
	decl.SourceKind = "workflow_action"
	decl.SourcePath = identity.WorkflowPath
	decl.SourceSHA = firstNonEmpty(identity.HeadSHA, identity.RunHeadSHA)
	decl.WorkflowIdentity = firstNonEmpty(identity.WorkflowName, "github-actions")
	decl.JobIdentity = firstNonEmpty(identity.JobName, identity.RunnerClass, "github-job")
	return decl, true, nil
}

func parseCacheManifest(content []byte, sourceKind, sourcePath, sourceSHA, workflowIdentity, jobIdentity, stepIdentity string) (cacheDeclaration, error) {
	var manifest cacheManifestFile
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return cacheDeclaration{}, fmt.Errorf("%w: parse %s: %w", ErrCacheDeclarationInvalid, sourcePath, err)
	}
	decl := cacheDeclaration{SourceKind: sourceKind, SourcePath: sourcePath, SourceSHA: sourceSHA, WorkflowIdentity: workflowIdentity, JobIdentity: jobIdentity, StepIdentity: stepIdentity, Version: manifest.Version, Volumes: manifest.Cache}
	if err := normalizeCacheDeclaration(&decl); err != nil {
		return cacheDeclaration{}, err
	}
	return decl, nil
}

func parseWorkflowActionCacheDeclaration(content []byte, identity RunnerExecutionIdentity) (cacheDeclaration, bool, error) {
	var workflow map[string]any
	if err := yaml.Unmarshal(content, &workflow); err != nil {
		return cacheDeclaration{}, false, fmt.Errorf("%w: parse workflow %s: %w", ErrCacheDeclarationInvalid, identity.WorkflowPath, err)
	}
	jobsValue, ok := workflow["jobs"].(map[string]any)
	if !ok {
		return cacheDeclaration{}, false, nil
	}
	var volumes []cacheVolumeDecl
	for jobID, rawJob := range jobsValue {
		job, ok := rawJob.(map[string]any)
		if !ok || !workflowJobMatches(identity.JobName, jobID, stringValue(job["name"])) {
			continue
		}
		steps, _ := job["steps"].([]any)
		for i, rawStep := range steps {
			step, ok := rawStep.(map[string]any)
			if !ok || strings.TrimSpace(stringValue(step["uses"])) != durableCacheActionRef {
				continue
			}
			with, _ := step["with"].(map[string]any)
			volume, err := actionVolumeFromWith(with, fmt.Sprintf("%s.steps[%d]", jobID, i))
			if err != nil {
				return cacheDeclaration{}, true, err
			}
			volumes = append(volumes, volume)
		}
	}
	if len(volumes) == 0 {
		return cacheDeclaration{}, false, nil
	}
	decl := cacheDeclaration{SourceKind: "workflow_action", SourcePath: identity.WorkflowPath, SourceSHA: firstNonEmpty(identity.HeadSHA, identity.RunHeadSHA), WorkflowIdentity: firstNonEmpty(identity.WorkflowName, "github-actions"), JobIdentity: firstNonEmpty(identity.JobName, identity.RunnerClass, "github-job"), Version: 1, Volumes: volumes}
	if err := normalizeCacheDeclaration(&decl); err != nil {
		return cacheDeclaration{}, true, err
	}
	return decl, true, nil
}

func actionVolumeFromWith(with map[string]any, step string) (cacheVolumeDecl, error) {
	if with == nil {
		return cacheVolumeDecl{}, fmt.Errorf("%w: %s missing with", ErrCacheDeclarationInvalid, step)
	}
	name := stringValue(with["name"])
	size := stringValue(with["size"])
	pathsValue := with["paths"]
	var paths []string
	switch v := pathsValue.(type) {
	case string:
		for _, line := range strings.Split(v, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				paths = append(paths, line)
			}
		}
	case []any:
		for _, item := range v {
			paths = append(paths, stringValue(item))
		}
	default:
		return cacheVolumeDecl{}, fmt.Errorf("%w: %s paths must be a static string or list", ErrCacheDeclarationInvalid, step)
	}
	for _, value := range append([]string{name, size}, paths...) {
		if strings.Contains(value, "${{") {
			return cacheVolumeDecl{}, fmt.Errorf("%w: %s contains a GitHub expression", ErrCacheDeclarationInvalid, step)
		}
	}
	return cacheVolumeDecl{Name: name, Size: size, Paths: paths}, nil
}

func normalizeCacheDeclaration(decl *cacheDeclaration) error {
	if decl.Version != 1 {
		return fmt.Errorf("%w: version must be 1", ErrCacheDeclarationInvalid)
	}
	seen := make(map[string]struct{}, len(decl.Volumes))
	for i := range decl.Volumes {
		volume := &decl.Volumes[i]
		volume.Name = strings.TrimSpace(volume.Name)
		if volume.Name == "" {
			return fmt.Errorf("%w: cache[%d].name is required", ErrCacheDeclarationInvalid, i)
		}
		if sanitizeMountName(volume.Name) != volume.Name {
			return fmt.Errorf("%w: cache[%d].name %q must use lowercase letters, numbers, '-' or '_'", ErrCacheDeclarationInvalid, i, volume.Name)
		}
		if _, ok := seen[volume.Name]; ok {
			return fmt.Errorf("%w: duplicate cache volume %q", ErrCacheDeclarationInvalid, volume.Name)
		}
		seen[volume.Name] = struct{}{}
		size, err := parseCacheSize(volume.Size)
		if err != nil {
			return fmt.Errorf("%w: cache %s size: %w", ErrCacheDeclarationInvalid, volume.Name, err)
		}
		volume.SizeBytes = size
		volume.Size = ""
		paths, err := normalizeCachePaths(volume.Paths)
		if err != nil {
			return fmt.Errorf("%w: cache %s paths: %w", ErrCacheDeclarationInvalid, volume.Name, err)
		}
		volume.Paths = paths
	}
	sort.Slice(decl.Volumes, func(i, j int) bool { return decl.Volumes[i].Name < decl.Volumes[j].Name })
	return nil
}

func parseCacheSize(raw string) (uint64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return durableDefaultCacheBytes, nil
	}
	upper := strings.ToUpper(raw)
	units := []struct {
		suffix string
		mult   uint64
	}{
		{"TIB", 1 << 40}, {"GIB", 1 << 30}, {"MIB", 1 << 20}, {"KIB", 1 << 10},
		{"TB", 1000 * 1000 * 1000 * 1000}, {"GB", 1000 * 1000 * 1000}, {"MB", 1000 * 1000}, {"KB", 1000},
		{"B", 1},
	}
	mult := uint64(1)
	for _, unit := range units {
		if strings.HasSuffix(upper, unit.suffix) {
			mult = unit.mult
			upper = strings.TrimSpace(strings.TrimSuffix(upper, unit.suffix))
			break
		}
	}
	value, err := strconv.ParseUint(upper, 10, 64)
	if err != nil || value == 0 {
		return 0, fmt.Errorf("must be a positive byte count or IEC size")
	}
	if value > durableMaxCacheBytes/mult {
		return 0, fmt.Errorf("exceeds 1TiB")
	}
	return value * mult, nil
}

func normalizeCachePaths(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("at least one path is required")
	}
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(paths))
	for _, raw := range paths {
		p, err := normalizeCachePath(raw)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[p]; ok {
			return nil, fmt.Errorf("duplicate path %s", p)
		}
		seen[p] = struct{}{}
		normalized = append(normalized, p)
	}
	sort.Strings(normalized)
	for i, a := range normalized {
		for _, b := range normalized[i+1:] {
			if strings.HasPrefix(b, a+"/") {
				return nil, fmt.Errorf("nested path %s under %s", b, a)
			}
		}
	}
	return normalized, nil
}

func normalizeCachePath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty path")
	}
	if strings.HasPrefix(raw, "~/") {
		raw = "/home/runner/" + strings.TrimPrefix(raw, "~/")
	}
	for _, segment := range strings.Split(raw, "/") {
		if segment == ".." {
			return "", fmt.Errorf("path %q contains ..", raw)
		}
	}
	if !strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("relative path %q", raw)
	}
	clean := path.Clean(raw)
	if clean == "/" || clean == "." || strings.Contains(clean, "/../") || strings.HasSuffix(clean, "/..") {
		return "", fmt.Errorf("invalid path %q", raw)
	}
	blocked := []string{"/bin", "/boot", "/dev", "/etc", "/lib", "/lib64", "/proc", "/run", "/sbin", "/sys", "/usr"}
	for _, root := range blocked {
		if clean == root || strings.HasPrefix(clean, root+"/") {
			return "", fmt.Errorf("path %s is under reserved root %s", clean, root)
		}
	}
	workspaceRoots := []string{githubRunnerDurableWorkDir, githubRunnerRuntimeDir, "/workspace"}
	for _, root := range workspaceRoots {
		root = path.Clean(root)
		if clean == root || strings.HasPrefix(clean, root+"/") {
			return "", fmt.Errorf("path %s is under GITHUB_WORKSPACE", clean)
		}
	}
	return clean, nil
}

func normalizedDeclarationJSON(decl cacheDeclaration) ([]byte, string, error) {
	copyDecl := decl
	if copyDecl.Volumes == nil {
		copyDecl.Volumes = []cacheVolumeDecl{}
	}
	payload, err := json.Marshal(copyDecl)
	if err != nil {
		return nil, "", err
	}
	return payload, stableHex(string(payload)), nil
}

func normalizedPathsJSON(paths []string) ([]byte, string, error) {
	payload, err := json.Marshal(paths)
	if err != nil {
		return nil, "", err
	}
	return payload, stableHex(string(payload)), nil
}

func (r *GitHubRunner) fetchRepositoryFile(ctx context.Context, identity RunnerExecutionIdentity, filePath, ref string) ([]byte, bool, error) {
	repository := strings.TrimSpace(identity.RepositoryFullName)
	owner, repo, ok := strings.Cut(repository, "/")
	if !ok || owner == "" || repo == "" {
		return nil, false, fmt.Errorf("github repository must be owner/name")
	}
	token, err := r.installationToken(ctx, identity.ProviderInstallationID)
	if err != nil {
		return nil, false, err
	}
	var resp struct {
		Type     string `json:"type"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	requestPath := fmt.Sprintf("/repos/%s/%s/contents/%s", url.PathEscape(owner), url.PathEscape(repo), escapeGitHubContentPath(filePath))
	if strings.TrimSpace(ref) != "" {
		requestPath += "?ref=" + url.QueryEscape(ref)
	}
	if err := r.githubRequest(ctx, http.MethodGet, requestPath, token, nil, &resp, http.StatusOK); err != nil {
		if strings.Contains(err.Error(), "status 404") {
			return nil, false, nil
		}
		return nil, false, err
	}
	if resp.Type != "file" || resp.Encoding != "base64" {
		return nil, true, fmt.Errorf("%w: %s is not a base64 file", ErrCacheDeclarationInvalid, filePath)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(resp.Content, "\n", ""))
	if err != nil {
		return nil, true, fmt.Errorf("%w: decode %s: %w", ErrCacheDeclarationInvalid, filePath, err)
	}
	return decoded, true, nil
}

func escapeGitHubContentPath(filePath string) string {
	parts := strings.Split(strings.Trim(filePath, "/"), "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func durableScopeRef(identity RunnerExecutionIdentity) string {
	branch := strings.TrimSpace(identity.RunBaseBranch)
	if branch == "" && identity.PullRequestNumber == 0 {
		branch = strings.TrimSpace(firstNonEmpty(identity.RunHeadBranch, identity.HeadBranch))
	}
	if branch == "" {
		branch = durableDogfoodBranch
	}
	return "refs/heads/" + branch
}

func durablePromotionCandidate(identity RunnerExecutionIdentity) bool {
	return identity.Provider == RunnerProviderGitHub && identity.PullRequestNumber == 0 && strings.TrimSpace(identity.RunEventName) == "push" && strings.TrimSpace(firstNonEmpty(identity.RunHeadBranch, identity.HeadBranch)) == durableDogfoodBranch
}

func goldenRunRefFromRunnerIdentity(identity RunnerExecutionIdentity) goldenWorkflowRunRef {
	return goldenWorkflowRunRef{OrgID: identity.OrgID, Provider: identity.Provider, ProviderInstallationID: identity.ProviderInstallationID, ProviderRepositoryID: identity.ProviderRepositoryID, ProviderRunID: identity.ProviderRunID, ProviderRunAttempt: identity.ProviderRunAttempt, RepositoryFullName: identity.RepositoryFullName, HeadSHA: firstNonEmpty(identity.HeadSHA, identity.RunHeadSHA)}
}

func githubMatrixKey(identity RunnerExecutionIdentity) string {
	job := strings.TrimSpace(identity.JobName)
	open := strings.LastIndex(job, "(")
	if open >= 0 && strings.HasSuffix(job, ")") && open < len(job)-2 {
		return strings.TrimSpace(job[open+1 : len(job)-1])
	}
	return ""
}

func workflowJobMatches(githubJobName, jobID, configuredName string) bool {
	githubJobName = strings.TrimSpace(githubJobName)
	candidates := []string{strings.TrimSpace(configuredName), strings.TrimSpace(jobID)}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if githubJobName == candidate || strings.HasPrefix(githubJobName, candidate+" (") {
			return true
		}
	}
	return false
}

func stringValue(value any) string {
	s, _ := value.(string)
	return strings.TrimSpace(s)
}

func sanitizeMountName(name string) string {
	name = strings.TrimSpace(name)
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func durableFailureReason(reason string, cause error) string {
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

func (s *Service) appendDurableEvent(ctx context.Context, event durableEvent) error {
	if s.CH == nil {
		return nil
	}
	span := trace.SpanFromContext(ctx)
	spanContext := span.SpanContext()
	row := durableEventRow{
		ObservedAt:            time.Now().UTC(),
		OrgID:                 event.Identity.OrgID,
		RepositoryID:          uint64FromNonNegativeInt64(event.Identity.ProviderRepositoryID),
		Provider:              event.Identity.Provider,
		ProviderRepositoryID:  uint64FromNonNegativeInt64(event.Identity.ProviderRepositoryID),
		ProviderRunID:         uint64FromNonNegativeInt64(event.Identity.ProviderRunID),
		ProviderRunAttempt:    uint64FromNonNegativeInt64(event.Identity.ProviderRunAttempt),
		ProviderJobID:         uint64FromNonNegativeInt64(event.Identity.ProviderJobID),
		ExecutionID:           uuidValue(event.ExecutionID),
		AttemptID:             uuidValue(event.AttemptID),
		OperationID:           uuidValue(event.OperationID),
		DurableScopeID:        uuidValue(event.ScopeID),
		DurableGenerationID:   uuidValue(event.GenerationID),
		ComponentKind:         event.ComponentKind,
		ComponentName:         event.ComponentName,
		EventName:             event.Name,
		Result:                event.Result,
		Reason:                event.Reason,
		MountName:             event.MountName,
		SourceGenerationID:    uuidValue(event.SourceGenerationID),
		CandidateGenerationID: uuidValue(event.CandidateGenerationID),
		CurrentGenerationID:   uuidValue(event.CurrentGenerationID),
		ZFSSnapshotRef:        event.ZFSSnapshotRef,
		UsedBytes:             event.UsedBytes,
		WrittenBytes:          event.WrittenBytes,
		TraceID:               spanContext.TraceID().String(),
		SpanID:                spanContext.SpanID().String(),
	}
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO "+s.CHDatabase+".durable_events")
	if err != nil {
		s.recordDurableEventInsertFailure(ctx, event, err)
		span.RecordError(err)
		return err
	}
	if err := batch.AppendStruct(&row); err != nil {
		s.recordDurableEventInsertFailure(ctx, event, err)
		span.RecordError(err)
		return err
	}
	if err := batch.Send(); err != nil {
		s.recordDurableEventInsertFailure(ctx, event, err)
		span.RecordError(err)
		return err
	}
	return nil
}

func (s *Service) recordDurableEventInsertFailure(ctx context.Context, event durableEvent, err error) {
	if s.Logger == nil || err == nil {
		return
	}
	s.Logger.WarnContext(ctx, "durable event insert failed",
		"event_name", event.Name,
		"result", event.Result,
		"component_kind", event.ComponentKind,
		"component_name", event.ComponentName,
		"mount_name", event.MountName,
		"operation_id", uuidValue(event.OperationID),
		"execution_id", uuidValue(event.ExecutionID),
		"attempt_id", uuidValue(event.AttemptID),
		"error", err,
	)
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

func uuidValue(value *uuid.UUID) uuid.UUID {
	if value == nil {
		return uuid.Nil
	}
	return *value
}

func uint64FromNonNegativeInt64(value int64) uint64 {
	if value <= 0 {
		return 0
	}
	return uint64(value) // #nosec G115 -- negative values are mapped to zero above.
}

func boolResult(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}
