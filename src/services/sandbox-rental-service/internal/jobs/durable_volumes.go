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
	durableDeclarationKind        = "cache_declaration"
	durableTrustProtectedBranch   = "protected_branch"
	durablePlatformImage          = "ubuntu-2404-actions-runner"
	durableGuestArch              = "x86_64"
	durableToolchainImage         = "gh-actions-runner"
	durableDogfoodBranch          = "main"
	durableScopeKindBranch        = "branch"
	durableCacheManifestPath      = ".verself/cache.yml"
	durableCacheMountRoot         = "/verself/.mounts"
	durableDefaultCacheBytes      = uint64(1 << 30)
	durableMaxCacheBytes          = uint64(1 << 40)
	durableMaxVolumesPerJob       = 99
	durableRetainedGenerationTTL  = 7 * 24 * time.Hour
	durablePruneBatchSize         = 32
)

const (
	durableEventDeclarationResolve = "durable.declaration.resolve"
	durableEventPrepare            = "durable.volume.prepare"
	durableEventSelect             = "durable.volume.select"
	durableEventMount              = "durable.volume.mount"
	durableEventBind               = "durable.volume.bind"
	durableEventSeal               = "durable.volume.seal"
	durableEventCommit             = "durable.volume.commit"
	durableEventPromote            = "durable.volume.promote"
	durableEventRetain             = "durable.volume.retain"
	durableEventPrune              = "durable.volume.prune"
	durableEventReconcile          = "durable.volume.reconcile"
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
	Mounted               bool
	MountSkipped          bool
	MountSkipReason       string
}

func (op durableVolumeOperation) event(executionID, attemptID uuid.UUID, identity RunnerExecutionIdentity, name, result, reason, zfsSnapshotRef string) durableEvent {
	return durableEvent{
		OperationID:           &op.OperationID,
		ScopeID:               &op.DurableScopeID,
		SourceGenerationID:    op.SourceGenerationID,
		CandidateGenerationID: &op.CandidateGenerationID,
		ExecutionID:           &executionID,
		AttemptID:             &attemptID,
		Identity:              identity,
		ComponentKind:         op.ComponentKind,
		ComponentName:         op.ComponentName,
		Name:                  name,
		Result:                result,
		Reason:                reason,
		MountName:             op.MountName,
		ZFSSnapshotRef:        zfsSnapshotRef,
	}
}

type goldenWorkflowRunRef struct {
	OrgID                  string
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
	OrgID                 string    `ch:"org_id"`
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
	ctx, span := tracer.Start(ctx, durableEventPrepare, trace.WithAttributes(
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
		_ = s.appendDurableEvent(ctx, durableEvent{
			ExecutionID:   &item.ExecutionID,
			AttemptID:     &item.AttemptID,
			Identity:      identity,
			ComponentKind: durableDeclarationKind,
			ComponentName: "unknown",
			Name:          durableEventDeclarationResolve,
			Result:        "failed",
			Reason:        err.Error(),
		})
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
	_ = s.appendDurableEvent(ctx, durableEvent{
		ExecutionID:   &item.ExecutionID,
		AttemptID:     &item.AttemptID,
		Identity:      identity,
		ComponentKind: durableDeclarationKind,
		ComponentName: decl.SourceKind,
		Name:          durableEventDeclarationResolve,
		Result:        "succeeded",
		Reason:        firstNonEmpty(decl.SourcePath, decl.SourceSHA),
	})
	mountPolicyHash := stableHex("bind", "noatime", "nodev", "nosuid")
	cacheVolumeCompatibilityHashes := make(map[string]string, len(decl.Volumes))
	for _, volume := range decl.Volumes {
		pathsJSON, pathHash, err := normalizedPathsJSON(volume.Paths)
		if err != nil {
			return durableVolumePlan{}, err
		}
		cacheVolumeCompatibilityHashes[volume.Name] = stableHex("cache-volume", volume.Name, strconv.FormatUint(volume.SizeBytes, 10), pathHash, mountPolicyHash)
		if err := s.storeQueries().InsertCacheVolumeSpec(ctx, store.InsertCacheVolumeSpecParams{
			CacheVolumeSpecID:   stableUUID("cache-volume-spec", cacheDeclarationID.String(), volume.Name),
			CacheDeclarationID:  cacheDeclarationID,
			Name:                volume.Name,
			SizeBytes:           mustInt64FromUint64(volume.SizeBytes, "cache volume size"),
			PathSetHash:         pathHash,
			MountPolicyHash:     mountPolicyHash,
			NormalizedPathsJson: pathsJSON,
			CreatedAt:           pgTime(now),
		}); err != nil {
			return durableVolumePlan{}, fmt.Errorf("insert cache volume spec %s: %w", volume.Name, err)
		}
	}

	workflowIdentity := firstNonEmpty(identity.WorkflowName, "github-actions")
	jobIdentity := githubJobIdentity(identity)
	matrixKey := githubMatrixKey(identity)
	workspacePolicyHash := stableHex("workspace", "v0", githubRunnerDurableWorkDir, "preserve-untracked")
	upsertJobShape := func(componentCompatibilityHash string) (uuid.UUID, error) {
		jobShapeID := stableUUID("durable-job-shape", identity.OrgID, identity.Provider, strconv.FormatInt(identity.ProviderRepositoryID, 10), workflowIdentity, jobIdentity, matrixKey, identity.RunnerClass, durablePlatformImage, durableGuestArch, workspacePolicyHash, componentCompatibilityHash)
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
			CacheDeclarationHash:   componentCompatibilityHash,
			CreatedAt:              pgTime(now),
		})
		if err != nil {
			return uuid.Nil, fmt.Errorf("upsert durable job shape: %w", err)
		}
		return shape, nil
	}
	workspaceShape, err := upsertJobShape(stableHex("workspace-volume", workspacePolicyHash))
	if err != nil {
		return durableVolumePlan{}, err
	}

	scopeRef := durableScopeRef(identity)
	promotionEligible := durablePromotionCandidate(identity)
	plan := durableVolumePlan{Enabled: true, Identity: identity}
	workspace, err := s.insertDurableOperation(ctx, item, identity, durableOperationSpec{
		JobShapeID:        workspaceShape,
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
		cacheShape, err := upsertJobShape(cacheVolumeCompatibilityHashes[volume.Name])
		if err != nil {
			return durableVolumePlan{}, err
		}
		op, err := s.insertDurableOperation(ctx, item, identity, durableOperationSpec{
			JobShapeID:        cacheShape,
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
		_ = s.appendDurableEvent(ctx, op.event(item.ExecutionID, item.AttemptID, identity, durableEventPrepare, "succeeded", "", ""))
		selectEvent := op.event(item.ExecutionID, item.AttemptID, identity, durableEventSelect, boolResult(op.SourceSnapshotRef != "", "hit", "miss"), "", op.SourceSnapshotRef)
		if op.SourceSnapshotRef == "" {
			selectEvent.Reason = "current_generation_missing"
		}
		_ = s.appendDurableEvent(ctx, selectEvent)
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
	scopeID := stableUUID("durable-scope", identity.OrgID, identity.Provider, strconv.FormatInt(identity.ProviderRepositoryID, 10), spec.ScopeRef, spec.JobShapeID.String(), spec.ComponentKind, spec.ComponentName, spec.TrustClass)
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

func (s *Service) recordDurableLeaseMountResults(ctx context.Context, plan durableVolumePlan, results []vmorchestrator.FilesystemMountResult) (durableVolumePlan, error) {
	if !plan.Enabled {
		return plan, nil
	}
	now := time.Now().UTC()
	byOperationID := make(map[uuid.UUID]vmorchestrator.FilesystemMountResult, len(results))
	byMountName := make(map[string]vmorchestrator.FilesystemMountResult, len(results))
	for _, result := range results {
		if id, err := uuid.Parse(strings.TrimSpace(result.OperationID)); err == nil {
			byOperationID[id] = result
		}
		byMountName[result.Name] = result
	}
	var errs []error
	for i := range plan.Operations {
		op := &plan.Operations[i]
		result, ok := byOperationID[op.OperationID]
		if !ok {
			result, ok = byMountName[op.MountName]
		}
		if !ok {
			reason := "mount_result_missing"
			if op.Required {
				errs = append(errs, fmt.Errorf("required durable mount %s has no lease mount result", op.MountName))
			}
			op.MountSkipped = true
			op.MountSkipReason = reason
			_ = s.storeQueries().MarkDurableOperationSkipped(ctx, store.MarkDurableOperationSkippedParams{RecordedAt: pgTime(now), FailureReason: reason, OperationID: op.OperationID})
			_ = s.appendDurableEvent(ctx, op.event(plan.Identity.ExecutionID, plan.Identity.AttemptID, plan.Identity, durableEventMount, "skipped", reason, ""))
			if len(op.BindPaths) > 0 {
				_ = s.appendDurableEvent(ctx, op.event(plan.Identity.ExecutionID, plan.Identity.AttemptID, plan.Identity, durableEventBind, "skipped", reason, ""))
			}
			continue
		}
		if result.Mounted {
			op.Mounted = true
			if err := s.storeQueries().MarkDurableOperationMounted(ctx, store.MarkDurableOperationMountedParams{MountedAt: pgTime(now), OperationID: op.OperationID}); err != nil && s.Logger != nil {
				s.Logger.WarnContext(ctx, "mark durable volume mounted failed", "operation_id", op.OperationID, "error", err)
			}
			_ = s.appendDurableEvent(ctx, op.event(plan.Identity.ExecutionID, plan.Identity.AttemptID, plan.Identity, durableEventMount, "mounted", "", ""))
			if len(op.BindPaths) > 0 {
				_ = s.appendDurableEvent(ctx, op.event(plan.Identity.ExecutionID, plan.Identity.AttemptID, plan.Identity, durableEventBind, "mounted", "", ""))
			}
			continue
		}
		reason := strings.TrimSpace(result.Error)
		if reason == "" {
			reason = "guest_mount_failed"
		}
		if op.Required {
			errs = append(errs, fmt.Errorf("required durable mount %s failed: %s", op.MountName, reason))
		}
		op.MountSkipped = true
		op.MountSkipReason = reason
		if err := s.storeQueries().MarkDurableOperationSkipped(ctx, store.MarkDurableOperationSkippedParams{RecordedAt: pgTime(now), FailureReason: reason, OperationID: op.OperationID}); err != nil && s.Logger != nil {
			s.Logger.WarnContext(ctx, "mark durable volume skipped failed", "operation_id", op.OperationID, "error", err)
		}
		_ = s.appendDurableEvent(ctx, op.event(plan.Identity.ExecutionID, plan.Identity.AttemptID, plan.Identity, durableEventMount, "skipped", reason, ""))
		if len(op.BindPaths) > 0 {
			_ = s.appendDurableEvent(ctx, op.event(plan.Identity.ExecutionID, plan.Identity.AttemptID, plan.Identity, durableEventBind, "skipped", reason, ""))
		}
	}
	if len(errs) > 0 {
		return plan, errors.Join(errs...)
	}
	return plan, nil
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
		_ = s.appendDurableEvent(ctx, op.event(plan.Identity.ExecutionID, plan.Identity.AttemptID, plan.Identity, durableEventPrepare, "failed", failureReason, ""))
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
		_ = s.appendDurableEvent(ctx, durableEvent{OperationID: &row.OperationID, ScopeID: &row.DurableScopeID, ExecutionID: &row.ExecutionID, AttemptID: &row.AttemptID, Name: durableEventReconcile, Result: "failed", Reason: failureReason})
	}
}

func (s *Service) finalizeDurableVolumes(ctx context.Context, item executionWorkItem, leaseID string, plan durableVolumePlan, sealDecision durableSealDecision) error {
	if !plan.Enabled {
		return nil
	}
	ctx, span := tracer.Start(ctx, durableEventSeal, trace.WithAttributes(
		attribute.String("lease.id", leaseID),
		attribute.Int("durable.volume_count", len(plan.Operations)),
	))
	defer span.End()
	var errs []error
	anyCandidate := false
	span.SetAttributes(
		attribute.Bool("durable.commit_allowed", sealDecision.Commit),
		attribute.String("durable.commit_skip_reason", sealDecision.SkipReason),
	)
	for _, op := range plan.Operations {
		if !op.Mounted {
			skipReason := firstNonEmpty(op.MountSkipReason, "mount_not_available")
			_ = s.appendDurableEvent(ctx, op.event(item.ExecutionID, item.AttemptID, plan.Identity, durableEventSeal, "skipped", skipReason, ""))
			continue
		}
		if !sealDecision.Commit {
			now := time.Now().UTC()
			_ = s.storeQueries().MarkDurableOperationResultRecorded(ctx, store.MarkDurableOperationResultRecordedParams{FinalState: "skipped", SealedAt: pgTime(now), RecordedAt: pgTime(now), FailureReason: sealDecision.SkipReason, OperationID: op.OperationID})
			_ = s.appendDurableEvent(ctx, op.event(item.ExecutionID, item.AttemptID, plan.Identity, durableEventSeal, "skipped", sealDecision.SkipReason, ""))
			continue
		}
		if err := s.storeQueries().MarkDurableOperationSealStarted(ctx, store.MarkDurableOperationSealStartedParams{SealStartedAt: pgTime(time.Now().UTC()), OperationID: op.OperationID}); err != nil {
			_ = s.appendDurableEvent(ctx, op.event(item.ExecutionID, item.AttemptID, plan.Identity, durableEventSeal, "failed", err.Error(), ""))
			errs = append(errs, err)
			continue
		}
		commit, err := s.Orchestrator.CommitFilesystemMount(ctx, leaseID, op.OperationID.String()+":commit", op.OperationID.String(), op.MountName, op.DurableScopeID.String(), op.SourceSnapshotRef, op.CandidateGenerationID.String())
		if err != nil {
			_ = s.storeQueries().MarkDurableOperationFailed(ctx, store.MarkDurableOperationFailedParams{FailureReason: err.Error(), RecordedAt: pgTime(time.Now().UTC()), OperationID: op.OperationID})
			for _, event := range durableCommitFailureEvents(op, item.ExecutionID, item.AttemptID, plan.Identity, err) {
				_ = s.appendDurableEvent(ctx, event)
			}
			errs = append(errs, fmt.Errorf("commit durable volume %s: %w", op.ComponentName, err))
			continue
		}
		sealEvent := op.event(item.ExecutionID, item.AttemptID, plan.Identity, durableEventSeal, "succeeded", "", commit.Snapshot)
		sealEvent.UsedBytes = commit.UsedBytes
		sealEvent.WrittenBytes = commit.WrittenBytes
		_ = s.appendDurableEvent(ctx, sealEvent)
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
			State:               durableGenerationInitialState(op),
			ZfsSnapshotRef:      commit.Snapshot,
			UsedBytes:           usedBytes,
			WrittenBytes:        writtenBytes,
			SealedAt:            pgTime(commit.CommittedAt),
			CommittedAt:         pgTime(now),
			LastUsedAt:          pgTime(now),
			ExpiresAt:           pgOptionalTime(durableGenerationExpiresAt(op, now)),
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("insert durable generation %s: %w", op.ComponentName, err))
			continue
		}
		if err := s.storeQueries().MarkDurableOperationResultRecorded(ctx, store.MarkDurableOperationResultRecordedParams{FinalState: "committed", SealedAt: pgTime(commit.CommittedAt), RecordedAt: pgTime(now), FailureReason: "", OperationID: op.OperationID}); err != nil {
			errs = append(errs, err)
			continue
		}
		commitEvent := op.event(item.ExecutionID, item.AttemptID, plan.Identity, durableEventCommit, "succeeded", "", commit.Snapshot)
		commitEvent.GenerationID = &generationID
		commitEvent.UsedBytes = commit.UsedBytes
		commitEvent.WrittenBytes = commit.WrittenBytes
		_ = s.appendDurableEvent(ctx, commitEvent)
		if !op.PromotionEligible {
			retainEvent := op.event(item.ExecutionID, item.AttemptID, plan.Identity, durableEventRetain, "succeeded", "non_promotable_scope", commit.Snapshot)
			retainEvent.GenerationID = &generationID
			retainEvent.UsedBytes = commit.UsedBytes
			retainEvent.WrittenBytes = commit.WrittenBytes
			_ = s.appendDurableEvent(ctx, retainEvent)
		}
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

type durableSealDecision struct {
	Commit     bool
	SkipReason string
}

func durableSealDecisionForExec(finalExec vmorchestrator.ExecRecord) durableSealDecision {
	switch finalExec.State {
	case vmorchestrator.ExecStateExited:
		if finalExec.ExitCode == 0 {
			return durableSealDecision{Commit: true}
		}
		return durableSealDecision{SkipReason: durableExecFailedReason(finalExec)}
	case vmorchestrator.ExecStateCanceled:
		return durableSealDecision{SkipReason: "exec_canceled"}
	case vmorchestrator.ExecStateKilledByLeaseExpiry:
		return durableSealDecision{SkipReason: "exec_killed_by_lease_expiry"}
	case vmorchestrator.ExecStateFailed:
		return durableSealDecision{SkipReason: durableExecFailedReason(finalExec)}
	}
	return durableSealDecision{SkipReason: "exec_not_success"}
}

func durableExecFailedReason(finalExec vmorchestrator.ExecRecord) string {
	reason := "exec_failed"
	if strings.TrimSpace(finalExec.TerminalReason) != "" {
		reason += ": " + strings.TrimSpace(finalExec.TerminalReason)
	}
	return reason
}

func durableCommitFailureEvents(op durableVolumeOperation, executionID, attemptID uuid.UUID, identity RunnerExecutionIdentity, cause error) []durableEvent {
	reason := durableFailureReason("commit_failed", cause)
	if durableCommitFailureReachedGuestSeal(cause) {
		sealEvent := op.event(executionID, attemptID, identity, durableEventSeal, "succeeded", "", "")
		commitEvent := op.event(executionID, attemptID, identity, durableEventCommit, "failed", reason, "")
		return []durableEvent{sealEvent, commitEvent}
	}
	if durableCommitFailureBeforeGuestSeal(cause) {
		commitEvent := op.event(executionID, attemptID, identity, durableEventCommit, "failed", reason, "")
		return []durableEvent{commitEvent}
	}
	sealEvent := op.event(executionID, attemptID, identity, durableEventSeal, "failed", reason, "")
	return []durableEvent{sealEvent}
}

func durableCommitFailureReachedGuestSeal(cause error) bool {
	if cause == nil {
		return false
	}
	reason := strings.TrimSpace(cause.Error())
	return strings.HasPrefix(reason, "flush filesystem mount device ") ||
		strings.HasPrefix(reason, "clone ") ||
		strings.HasPrefix(reason, "snapshot ") ||
		strings.HasPrefix(reason, "promote ") ||
		strings.HasPrefix(reason, "seal generation ") ||
		strings.Contains(reason, " zfs ")
}

func durableCommitFailureBeforeGuestSeal(cause error) bool {
	if cause == nil {
		return false
	}
	reason := strings.TrimSpace(cause.Error())
	return strings.HasPrefix(reason, "unknown filesystem mount ") ||
		strings.Contains(reason, " is read-only") ||
		strings.Contains(reason, " belongs to operation ") ||
		strings.Contains(reason, "guest control is not available")
}

func durableGenerationInitialState(op durableVolumeOperation) string {
	if op.PromotionEligible {
		return "committed"
	}
	return "retained"
}

func durableGenerationExpiresAt(op durableVolumeOperation, now time.Time) time.Time {
	if op.PromotionEligible {
		return time.Time{}
	}
	return now.Add(durableRetainedGenerationTTL)
}

func (s *Service) PromoteGoldenRun(ctx context.Context, req scheduler.GoldenRunPromoteArgs) error {
	if strings.TrimSpace(req.Provider) != RunnerProviderGitHub {
		return fmt.Errorf("durable run promotion unsupported provider %q", req.Provider)
	}
	repositoryFullName := strings.TrimSpace(req.RepositoryFullName)
	installationID := req.ProviderInstallationID
	var orgID string
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

func (s *Service) pruneDurableGenerations(ctx context.Context) error {
	if s.Orchestrator == nil {
		return nil
	}
	now := time.Now().UTC()
	rows, err := s.storeQueries().ListPrunableDurableGenerations(ctx, store.ListPrunableDurableGenerationsParams{
		NowAt:      pgTime(now),
		LimitCount: durablePruneBatchSize,
	})
	if err != nil {
		return fmt.Errorf("query prunable durable generations: %w", err)
	}
	var errs []error
	for _, row := range rows {
		rowsChanged, err := s.storeQueries().MarkDurableGenerationPruning(ctx, store.MarkDurableGenerationPruningParams{
			PruningAt:           pgTime(now),
			DurableGenerationID: row.DurableGenerationID,
			DurableScopeID:      row.DurableScopeID,
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("mark durable generation pruning %s: %w", row.DurableGenerationID, err))
			continue
		}
		if rowsChanged == 0 {
			continue
		}
		identity := RunnerExecutionIdentity{
			Provider:             row.Provider,
			ProviderRepositoryID: row.ProviderRepositoryID,
			ProviderRunID:        row.ProviderRunID,
			ProviderRunAttempt:   row.ProviderRunAttempt,
			ProviderJobID:        row.ProviderJobID,
		}
		_, err = s.Orchestrator.PruneFilesystemGeneration(ctx, row.DurableGenerationID.String()+":prune", row.OperationID.String(), row.DurableGenerationID.String(), row.DurableScopeID.String(), row.ZfsSnapshotRef)
		if err != nil {
			_ = s.appendDurableEvent(ctx, durableEvent{OperationID: &row.OperationID, ScopeID: &row.DurableScopeID, GenerationID: &row.DurableGenerationID, ExecutionID: &row.ExecutionID, AttemptID: &row.AttemptID, Identity: identity, ComponentKind: row.ComponentKind, ComponentName: row.ComponentName, Name: durableEventPrune, Result: "failed", Reason: err.Error(), ZFSSnapshotRef: row.ZfsSnapshotRef, UsedBytes: uint64FromInt64(row.UsedBytes, "durable used bytes"), WrittenBytes: uint64FromInt64(row.WrittenBytes, "durable written bytes")})
			errs = append(errs, fmt.Errorf("prune durable generation %s: %w", row.DurableGenerationID, err))
			continue
		}
		if err := s.storeQueries().MarkDurableGenerationPruned(ctx, store.MarkDurableGenerationPrunedParams{PrunedAt: pgTime(time.Now().UTC()), DurableGenerationID: row.DurableGenerationID, DurableScopeID: row.DurableScopeID}); err != nil {
			errs = append(errs, fmt.Errorf("mark durable generation pruned %s: %w", row.DurableGenerationID, err))
			continue
		}
		_ = s.appendDurableEvent(ctx, durableEvent{OperationID: &row.OperationID, ScopeID: &row.DurableScopeID, GenerationID: &row.DurableGenerationID, ExecutionID: &row.ExecutionID, AttemptID: &row.AttemptID, Identity: identity, ComponentKind: row.ComponentKind, ComponentName: row.ComponentName, Name: durableEventPrune, Result: "succeeded", ZFSSnapshotRef: row.ZfsSnapshotRef, UsedBytes: uint64FromInt64(row.UsedBytes, "durable used bytes"), WrittenBytes: uint64FromInt64(row.WrittenBytes, "durable written bytes")})
	}
	return errors.Join(errs...)
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
	ctx, span := tracer.Start(ctx, durableEventPromote, trace.WithAttributes(
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
	currentGenerationID := uuid.Nil
	if rows > 0 {
		result = "succeeded"
		currentGenerationID = candidateID
	} else {
		current, err := s.storeQueries().GetCurrentDurableGeneration(ctx, store.GetCurrentDurableGenerationParams{DurableScopeID: candidate.DurableScopeID})
		if err == nil && current.DurableGenerationID == candidateID {
			result = "already_current"
			promoted = true
			currentGenerationID = current.DurableGenerationID
		} else if err == nil {
			currentGenerationID = current.DurableGenerationID
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return false, err
		} else if err := s.storeQueries().RetainDurableGeneration(ctx, store.RetainDurableGenerationParams{RetainedAt: pgTime(now), ExpiresAt: pgTime(now.Add(durableRetainedGenerationTTL)), DurableGenerationID: candidate.DurableGenerationID, DurableScopeID: candidate.DurableScopeID}); err != nil {
			return false, err
		}
	}
	span.SetAttributes(
		attribute.Bool("durable.promoted", promoted),
		attribute.String("durable.promotion_result", result),
		attribute.String("durable.current_generation_id", currentGenerationID.String()),
	)
	identity := RunnerExecutionIdentity{OrgID: ref.OrgID, Provider: ref.Provider, ProviderRepositoryID: ref.ProviderRepositoryID, ProviderRunID: ref.ProviderRunID, ProviderRunAttempt: ref.ProviderRunAttempt, ProviderJobID: candidate.ProviderJobID, RepositoryFullName: ref.RepositoryFullName}
	_ = s.appendDurableEvent(ctx, durableEvent{OperationID: &candidate.OperationID, ScopeID: &candidate.DurableScopeID, GenerationID: &candidate.DurableGenerationID, SourceGenerationID: candidate.SourceGenerationID, CurrentGenerationID: &currentGenerationID, ExecutionID: &candidate.ExecutionID, AttemptID: &candidate.AttemptID, Identity: identity, ComponentKind: candidate.ComponentKind, ComponentName: candidate.ComponentName, Name: durableEventPromote, Result: result, ZFSSnapshotRef: candidate.ZfsSnapshotRef, UsedBytes: uint64FromInt64(candidate.UsedBytes, "durable used bytes"), WrittenBytes: uint64FromInt64(candidate.WrittenBytes, "durable written bytes")})
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
	ctx, span := tracer.Start(ctx, durableEventDeclarationResolve, trace.WithAttributes(
		attribute.String("runner.provider", identity.Provider),
		attribute.Int64("runner.provider_repository_id", identity.ProviderRepositoryID),
		attribute.Int64("runner.provider_run_id", identity.ProviderRunID),
		attribute.String("github.repository", identity.RepositoryFullName),
	))
	defer span.End()
	manifest, manifestPresent, err := s.fetchManifestCacheDeclaration(ctx, identity)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return cacheDeclaration{}, err
	}
	if manifestPresent {
		span.SetAttributes(
			attribute.String("cache.source_kind", manifest.SourceKind),
			attribute.String("cache.source_path", manifest.SourcePath),
			attribute.String("cache.source_ref", manifest.SourceSHA),
			attribute.Int("cache.volume_count", len(manifest.Volumes)),
		)
		return manifest, nil
	}
	decl := cacheDeclaration{SourceKind: "none", SourceSHA: cacheDeclarationRef(identity), Version: 1, Volumes: []cacheVolumeDecl{}}
	span.SetAttributes(
		attribute.String("cache.source_kind", decl.SourceKind),
		attribute.String("cache.source_ref", decl.SourceSHA),
		attribute.Int("cache.volume_count", 0),
	)
	return decl, nil
}

func (s *Service) fetchManifestCacheDeclaration(ctx context.Context, identity RunnerExecutionIdentity) (cacheDeclaration, bool, error) {
	if s.GitHubRunner == nil {
		return cacheDeclaration{}, false, nil
	}
	ref := cacheDeclarationRef(identity)
	content, ok, err := s.GitHubRunner.fetchRepositoryFile(ctx, identity, durableCacheManifestPath, ref)
	if err != nil || !ok {
		return cacheDeclaration{}, ok, err
	}
	decl, err := parseCacheManifest(content, "manifest", durableCacheManifestPath, ref, "", "", "")
	return decl, true, err
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
		{"TIB", 1 << 40},
		{"GIB", 1 << 30},
		{"MIB", 1 << 20},
		{"KIB", 1 << 10},
		{"TB", 1000 * 1000 * 1000 * 1000},
		{"GB", 1000 * 1000 * 1000},
		{"MB", 1000 * 1000},
		{"KB", 1000},
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
			return "", fmt.Errorf("path %q contains parent directory reference", raw)
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

func cacheDeclarationRef(identity RunnerExecutionIdentity) string {
	if identity.PullRequestNumber != 0 {
		if ref := strings.TrimSpace(identity.RunBaseSHA); ref != "" {
			return ref
		}
		if ref := strings.TrimSpace(identity.RunBaseBranch); ref != "" {
			return ref
		}
	}
	return firstNonEmpty(identity.HeadSHA, identity.RunHeadSHA, identity.RunHeadBranch, identity.HeadBranch)
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

func githubJobIdentity(identity RunnerExecutionIdentity) string {
	job := strings.TrimSpace(identity.JobName)
	if job == "" {
		return firstNonEmpty(identity.RunnerClass, "github-job")
	}
	open := strings.LastIndex(job, "(")
	if open >= 0 && strings.HasSuffix(job, ")") && open < len(job)-2 {
		base := strings.TrimSpace(job[:open])
		if base != "" {
			return base
		}
	}
	return job
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
	span := trace.SpanFromContext(ctx)
	span.AddEvent(event.Name, trace.WithAttributes(
		attribute.String("durable.event_name", event.Name),
		attribute.String("durable.result", event.Result),
		attribute.String("durable.reason", event.Reason),
		attribute.String("durable.component_kind", event.ComponentKind),
		attribute.String("durable.component_name", event.ComponentName),
		attribute.String("durable.mount_name", event.MountName),
		attribute.String("durable.operation_id", uuidValue(event.OperationID).String()),
		attribute.String("durable.scope_id", uuidValue(event.ScopeID).String()),
		attribute.String("durable.generation_id", uuidValue(event.GenerationID).String()),
		attribute.String("durable.source_generation_id", uuidValue(event.SourceGenerationID).String()),
		attribute.String("durable.candidate_generation_id", uuidValue(event.CandidateGenerationID).String()),
		attribute.String("durable.current_generation_id", uuidValue(event.CurrentGenerationID).String()),
		attribute.String("zfs.snapshot_ref", event.ZFSSnapshotRef),
		attribute.String("zfs.used_bytes", strconv.FormatUint(event.UsedBytes, 10)),
		attribute.String("zfs.written_bytes", strconv.FormatUint(event.WrittenBytes, 10)),
	))
	if s.CH == nil {
		return nil
	}
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
