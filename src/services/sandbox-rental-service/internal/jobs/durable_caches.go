package jobs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/bits"
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
	"github.com/verself/vm-orchestrator/vmproto"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/yaml.v3"
)

const (
	durableWorkspaceCacheName        = "workspace"
	durableTrustProtectedBranch      = "protected_branch"
	durablePlatformImage             = "ubuntu-2404-actions-runner"
	durableGuestArch                 = "x86_64"
	durableToolchainImage            = "gh-actions-runner"
	durableDogfoodBranch             = "main"
	durableScopeKindBranch           = "branch"
	durableCacheManifestPath         = ".verself/cache.yml"
	durableCacheMountRoot            = "/verself/.mounts"
	durableMaxCachesPerJob           = 99
	durableRetainedGenerationTTL     = 7 * 24 * time.Hour
	durablePruneBatchSize            = 32
	durableEvictionMaxBatches        = 8
	durablePoolLowWatermarkPermille  = 700
	durablePoolHardWatermarkPermille = 850
)

const (
	durableEventDeclarationResolve  = "durable.declaration.resolve"
	durableEventPrepare             = "durable.cache.prepare"
	durableEventSelect              = "durable.cache.select"
	durableEventMount               = "durable.cache.mount"
	durableEventBind                = "durable.cache.bind"
	durableEventSeal                = "durable.cache.seal"
	durableEventCommit              = "durable.cache.commit"
	durableEventPromote             = "durable.cache.promote"
	durableEventRetain              = "durable.cache.retain"
	durableEventPrune               = "durable.cache.prune"
	durableEventEvict               = "durable.cache.evict"
	durableEventReconcile           = "durable.cache.reconcile"
	durableEventPoolObserve         = "durable.pool.observe"
	goldenVMEventLookup             = "golden.vm.lookup"
	goldenVMEventRestore            = "golden.vm.restore"
	goldenVMEventCreateEnqueue      = "golden.vm.create.enqueue"
	goldenVMEventCreateStart        = "golden.vm.create.start"
	goldenVMEventCreateReleaseLease = "golden.vm.create.release_lease"
	goldenVMEventBeforeSnapshot     = "golden.vm.before_snapshot"
	goldenVMEventCheckpoint         = "golden.vm.checkpoint"
	goldenVMEventPublish            = "golden.vm.publish"
	goldenVMEventPromote            = "golden.vm.promote"
	goldenVMEventPrune              = "golden.vm.prune"
)

var ErrCacheDeclarationInvalid = errors.New("sandbox-rental: cache declaration invalid")

type durableCachePlan struct {
	Enabled    bool
	Identity   RunnerExecutionIdentity
	Operations []durableCacheOperation
	GoldenVM   goldenVMPlan
}

type durableCacheOperation struct {
	OperationID           uuid.UUID
	DurableScopeID        uuid.UUID
	JobShapeID            uuid.UUID
	SourceGenerationID    *uuid.UUID
	SourceSnapshotRef     string
	SourceSnapshotGUID    string
	CandidateGenerationID uuid.UUID
	MountName             string
	MountPath             string
	BindPaths             []string
	TrustClass            string
	CacheName             string
	PromotionEligible     bool
	Required              bool
	SelectMissReason      string
	Mounted               bool
	MountSkipped          bool
	MountSkipReason       string
}

type goldenVMPlan struct {
	Enabled                 bool
	OperationID             uuid.UUID
	CandidateSnapshotID     uuid.UUID
	JobShapeID              uuid.UUID
	ScopeRef                string
	TrustClass              string
	SourceGenerationSetHash string
	Activation              vmorchestrator.GoldenVMActivation
	PromotionEligible       bool
	LookupResult            string
	LookupReason            string
}

func (p goldenVMPlan) event(executionID, attemptID uuid.UUID, identity RunnerExecutionIdentity, name, result, reason string) goldenVMEvent {
	activationMode := ""
	if p.Activation.Requested() {
		activationMode = string(vmorchestrator.ActivationModeSnapshotRestore)
	}
	return goldenVMEvent{
		OperationID:             &p.OperationID,
		SnapshotID:              &p.CandidateSnapshotID,
		JobShapeID:              &p.JobShapeID,
		ExecutionID:             &executionID,
		AttemptID:               &attemptID,
		Identity:                identity,
		Name:                    name,
		Result:                  result,
		Reason:                  reason,
		GenerationSetHash:       p.SourceGenerationSetHash,
		SourceGenerationSetHash: p.SourceGenerationSetHash,
		SnapshotKey:             p.Activation.SnapshotKey,
		ActivationMode:          activationMode,
		VMStateArtifactRef:      p.Activation.VMStateArtifactRef,
		MemoryArtifactRef:       p.Activation.MemoryArtifactRef,
		RootSnapshotRef:         p.Activation.RootSnapshotRef,
		RootSnapshotGUID:        p.Activation.RootSnapshotGUID,
	}
}

func (op durableCacheOperation) event(executionID, attemptID uuid.UUID, identity RunnerExecutionIdentity, name, result, reason, zfsSnapshotRef string) durableEvent {
	return durableEvent{
		OperationID:           &op.OperationID,
		ScopeID:               &op.DurableScopeID,
		SourceGenerationID:    op.SourceGenerationID,
		CandidateGenerationID: &op.CandidateGenerationID,
		ExecutionID:           &executionID,
		AttemptID:             &attemptID,
		Identity:              identity,
		CacheName:             op.CacheName,
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

type githubWorkflowRunJobsState struct {
	Total            int
	Completed        int
	Succeeded        int
	Failed           int
	SuccessfulJobIDs []int64
	JobStatuses      map[int64]string
	JobConclusions   map[int64]string
}

func (s githubWorkflowRunJobsState) promotionReady() (bool, string) {
	if s.Total == 0 {
		return false, "github workflow run has no observed jobs"
	}
	if s.Completed != s.Total {
		return false, "github workflow run is not complete"
	}
	if s.Failed > 0 {
		return false, "github workflow run has non-success jobs"
	}
	return true, ""
}

type cacheDeclaration struct {
	SourceKind       string      `json:"-"`
	SourcePath       string      `json:"-"`
	SourceSHA        string      `json:"-"`
	WorkflowIdentity string      `json:"-"`
	JobIdentity      string      `json:"-"`
	StepIdentity     string      `json:"-"`
	Version          int         `json:"version"`
	Caches           []cacheDecl `json:"cache"`
}

type cacheDecl struct {
	Name  string   `json:"name" yaml:"name"`
	Paths []string `json:"paths" yaml:"paths"`
}

type cacheManifestFile struct {
	Version int         `yaml:"version"`
	Cache   []cacheDecl `yaml:"cache"`
}

type durableCacheDefinition struct {
	Name            string
	VisiblePaths    []string
	MountName       string
	MountPath       string
	BindPaths       []string
	ReconcilePolicy string
	Required        bool
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
	CacheName             string
	Name                  string
	Result                string
	Reason                string
	MountName             string
	ZFSSnapshotRef        string
	UsedBytes             uint64
	WrittenBytes          uint64
	PoolSizeBytes         uint64
	PoolAllocatedBytes    uint64
	PoolFreeBytes         uint64
	OrgQuotaBytes         uint64
	OrgUsedBytes          uint64
	OrgAvailableBytes     uint64
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
	CacheName             string    `ch:"cache_name"`
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
	PoolSizeBytes         uint64    `ch:"pool_size_bytes"`
	PoolAllocatedBytes    uint64    `ch:"pool_allocated_bytes"`
	PoolFreeBytes         uint64    `ch:"pool_free_bytes"`
	OrgQuotaBytes         uint64    `ch:"org_quota_bytes"`
	OrgUsedBytes          uint64    `ch:"org_used_bytes"`
	OrgAvailableBytes     uint64    `ch:"org_available_bytes"`
	TraceID               string    `ch:"trace_id"`
	SpanID                string    `ch:"span_id"`
}

type goldenVMEvent struct {
	OperationID             *uuid.UUID
	SnapshotID              *uuid.UUID
	JobShapeID              *uuid.UUID
	ExecutionID             *uuid.UUID
	AttemptID               *uuid.UUID
	Identity                RunnerExecutionIdentity
	Name                    string
	Result                  string
	Reason                  string
	FromState               string
	ToState                 string
	LeaseID                 string
	ExecID                  string
	RiverJobID              int64
	GenerationSetHash       string
	SourceGenerationSetHash string
	SnapshotKey             string
	ActivationMode          string
	VMStateArtifactRef      string
	MemoryArtifactRef       string
	RootSnapshotRef         string
	RootSnapshotGUID        string
	StateBytes              uint64
	MemoryBytes             uint64
}

type goldenVMEventRow struct {
	ObservedAt              time.Time `ch:"observed_at"`
	OrgID                   string    `ch:"org_id"`
	RepositoryID            uint64    `ch:"repository_id"`
	Provider                string    `ch:"provider"`
	ProviderRepositoryID    uint64    `ch:"provider_repository_id"`
	ProviderRunID           uint64    `ch:"provider_run_id"`
	ProviderRunAttempt      uint64    `ch:"provider_run_attempt"`
	ProviderJobID           uint64    `ch:"provider_job_id"`
	ExecutionID             uuid.UUID `ch:"execution_id"`
	AttemptID               uuid.UUID `ch:"attempt_id"`
	OperationID             uuid.UUID `ch:"operation_id"`
	GoldenVMSnapshotID      uuid.UUID `ch:"golden_vm_snapshot_id"`
	JobShapeID              uuid.UUID `ch:"job_shape_id"`
	EventName               string    `ch:"event_name"`
	Result                  string    `ch:"result"`
	Reason                  string    `ch:"reason"`
	FromState               string    `ch:"from_state"`
	ToState                 string    `ch:"to_state"`
	LeaseID                 string    `ch:"lease_id"`
	ExecID                  string    `ch:"exec_id"`
	RiverJobID              int64     `ch:"river_job_id"`
	GenerationSetHash       string    `ch:"generation_set_hash"`
	SourceGenerationSetHash string    `ch:"source_generation_set_hash"`
	SnapshotKey             string    `ch:"snapshot_key"`
	ActivationMode          string    `ch:"activation_mode"`
	VMStateArtifactRef      string    `ch:"vmstate_artifact_ref"`
	MemoryArtifactRef       string    `ch:"memory_artifact_ref"`
	RootSnapshotRef         string    `ch:"root_snapshot_ref"`
	RootSnapshotGUID        string    `ch:"root_snapshot_guid"`
	StateBytes              uint64    `ch:"state_bytes"`
	MemoryBytes             uint64    `ch:"memory_bytes"`
	TraceID                 string    `ch:"trace_id"`
	SpanID                  string    `ch:"span_id"`
}

func (p durableCachePlan) filesystemMounts() []vmorchestrator.FilesystemMount {
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
		})
	}
	return mounts
}

func durableCacheDefinitions(decl cacheDeclaration) []durableCacheDefinition {
	caches := make([]durableCacheDefinition, 0, len(decl.Caches)+1)
	caches = append(caches, durableCacheDefinition{
		Name:            durableWorkspaceCacheName,
		VisiblePaths:    []string{githubRunnerDurableWorkDir},
		MountName:       durableWorkspaceCacheName,
		MountPath:       githubRunnerDurableWorkDir,
		BindPaths:       nil,
		ReconcilePolicy: "git_checkout",
		Required:        true,
	})
	for _, cache := range decl.Caches {
		caches = append(caches, durableCacheDefinition{
			Name:            cache.Name,
			VisiblePaths:    append([]string(nil), cache.Paths...),
			MountName:       "cache-" + sanitizeMountName(cache.Name),
			MountPath:       path.Join(durableCacheMountRoot, cache.Name),
			BindPaths:       append([]string(nil), cache.Paths...),
			ReconcilePolicy: "none",
			Required:        false,
		})
	}
	return caches
}

func (cache durableCacheDefinition) specHash(mountPolicyHash string) string {
	parts := make([]string, 0, len(cache.VisiblePaths)+4)
	parts = append(parts, "durable-cache", cache.Name, mountPolicyHash, cache.ReconcilePolicy)
	parts = append(parts, cache.VisiblePaths...)
	return stableHex(parts...)
}

func (s *Service) prepareDurableCaches(ctx context.Context, item executionWorkItem) (durableCachePlan, error) {
	if item.WorkloadKind != WorkloadKindRunner || item.ExternalProvider != RunnerProviderGitHub {
		return durableCachePlan{}, nil
	}
	ctx, span := tracer.Start(ctx, durableEventPrepare, trace.WithAttributes(
		attribute.String("execution.id", item.ExecutionID.String()),
		attribute.String("attempt.id", item.AttemptID.String()),
	))
	defer span.End()

	identityStarted := time.Now().UTC()
	identity, err := s.runnerExecutionIdentity(ctx, item.ExecutionID, item.AttemptID)
	identityCompleted := time.Now().UTC()
	result, reason := sandboxPhaseResult(err)
	s.recordExecutionPhase(ctx, item, "sandbox-rental", "sandbox.durable.prepare.identity", result, reason, identityStarted, identityCompleted, nil)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = fmt.Errorf("%w: runner execution identity missing", ErrRunnerUnavailable)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return durableCachePlan{}, err
	}
	if identity.Provider != RunnerProviderGitHub {
		return durableCachePlan{}, nil
	}
	hydrateStarted := time.Now().UTC()
	identity, err = s.hydrateGitHubRunIdentity(ctx, identity)
	hydrateCompleted := time.Now().UTC()
	result, reason = sandboxPhaseResult(err)
	s.recordRunnerIdentityPhase(ctx, identity, item, "sandbox-rental", "sandbox.durable.prepare.github_run_hydrate", result, reason, hydrateStarted, hydrateCompleted, nil)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return durableCachePlan{}, err
	}

	declarationStarted := time.Now().UTC()
	decl, err := s.resolveCacheDeclaration(ctx, identity)
	declarationCompleted := time.Now().UTC()
	result, reason = sandboxPhaseResult(err)
	s.recordRunnerIdentityPhase(ctx, identity, item, "sandbox-rental", "sandbox.durable.prepare.declaration", result, reason, declarationStarted, declarationCompleted, nil)
	if err != nil {
		_ = s.appendDurableEvent(ctx, durableEvent{
			ExecutionID: &item.ExecutionID,
			AttemptID:   &item.AttemptID,
			Identity:    identity,
			CacheName:   "declaration",
			Name:        durableEventDeclarationResolve,
			Result:      "failed",
			Reason:      err.Error(),
		})
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return durableCachePlan{}, err
	}
	caches := durableCacheDefinitions(decl)
	if len(caches) > durableMaxCachesPerJob {
		return durableCachePlan{}, fmt.Errorf("%w: requested %d durable caches, maximum is %d", ErrCacheDeclarationInvalid, len(caches), durableMaxCachesPerJob)
	}

	declarationHash, err := normalizedDeclarationHash(decl)
	if err != nil {
		return durableCachePlan{}, err
	}
	now := time.Now().UTC()
	_ = s.appendDurableEvent(ctx, durableEvent{
		ExecutionID: &item.ExecutionID,
		AttemptID:   &item.AttemptID,
		Identity:    identity,
		CacheName:   "declaration",
		Name:        durableEventDeclarationResolve,
		Result:      "succeeded",
		Reason:      firstNonEmpty(decl.SourcePath, decl.SourceSHA),
	})
	mountPolicyHash := stableHex("bind", "noatime", "nodev", "nosuid")
	workflowIdentity := firstNonEmpty(identity.WorkflowName, "github-actions")
	jobIdentity := githubJobIdentity(identity)
	matrixKey := githubMatrixKey(identity)
	upsertJobShape := func(cacheSpecHash string) (uuid.UUID, error) {
		jobShapeID := stableUUID("durable-job-shape", identity.OrgID, identity.Provider, strconv.FormatInt(identity.ProviderRepositoryID, 10), workflowIdentity, jobIdentity, matrixKey, identity.RunnerClass, durablePlatformImage, durableGuestArch, cacheSpecHash)
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
			CacheSpecHash:          cacheSpecHash,
			CreatedAt:              pgTime(now),
		})
		if err != nil {
			return uuid.Nil, fmt.Errorf("upsert durable job shape: %w", err)
		}
		return shape, nil
	}
	scopeRef := durableScopeRef(identity)
	promotionEligible := durablePromotionCandidate(identity)
	storageStarted := time.Now().UTC()
	storage, storageKnown, err := s.durableStorageSnapshot(ctx)
	storageCompleted := time.Now().UTC()
	result, reason = sandboxPhaseResult(err)
	s.recordRunnerIdentityPhase(ctx, identity, item, "sandbox-rental", "sandbox.durable.prepare.storage_capacity", result, reason, storageStarted, storageCompleted, sandboxPhaseAttrs{
		"storage_known": strconv.FormatBool(storageKnown),
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return durableCachePlan{}, err
	}
	warmSourceSkipReason := ""
	if storageKnown && storage.AllocatedPermille >= durablePoolHardWatermarkPermille {
		warmSourceSkipReason = "zpool_hard_watermark_exceeded"
	}
	planStarted := time.Now().UTC()
	plan := durableCachePlan{Enabled: true, Identity: identity}
	for idx, cache := range caches {
		cacheShape, err := upsertJobShape(cache.specHash(mountPolicyHash))
		if err != nil {
			return durableCachePlan{}, err
		}
		op, err := s.insertDurableOperation(ctx, item, identity, durableOperationSpec{
			JobShapeID:        cacheShape,
			ScopeRef:          scopeRef,
			CacheName:         cache.Name,
			MountName:         cache.MountName,
			MountPath:         cache.MountPath,
			BindPaths:         cache.BindPaths,
			TrustClass:        durableTrustProtectedBranch,
			PromotionEligible: promotionEligible,
			Required:          cache.Required,
			SortOrder:         idx,
			SourceSkipReason:  warmSourceSkipReason,
			Now:               now,
		})
		if err != nil {
			return durableCachePlan{}, err
		}
		plan.Operations = append(plan.Operations, op)
	}
	goldenJobShape, err := upsertJobShape(stableHex("golden-vm", declarationHash))
	if err != nil {
		return durableCachePlan{}, err
	}
	s.recordRunnerIdentityPhase(ctx, identity, item, "sandbox-rental", "sandbox.durable.prepare.plan", "succeeded", "", planStarted, time.Now().UTC(), sandboxPhaseAttrs{
		"durable.cache_count": strconv.Itoa(len(plan.Operations)),
	})
	plan.GoldenVM, err = s.prepareGoldenVMPlan(ctx, item, identity, plan.Operations, goldenVMPlanSpec{
		JobShapeID:        goldenJobShape,
		ScopeRef:          scopeRef,
		TrustClass:        durableTrustProtectedBranch,
		PromotionEligible: promotionEligible,
		Now:               now,
	})
	if err != nil {
		return durableCachePlan{}, err
	}
	span.SetAttributes(
		attribute.Int("durable.cache_count", len(plan.Operations)),
		attribute.String("golden_vm.source_generation_set_hash", plan.GoldenVM.SourceGenerationSetHash),
		attribute.String("golden_vm.lookup_result", plan.GoldenVM.LookupResult),
		attribute.String("durable.declaration_hash", declarationHash),
		attribute.Bool("durable.promotion_candidate", promotionEligible),
		attribute.Bool("durable.storage_known", storageKnown),
		attribute.Int64("durable.pool_allocated_permille", mustInt64FromUint64(storage.AllocatedPermille, "pool allocated permille")),
		attribute.String("durable.warm_source_skip_reason", warmSourceSkipReason),
		attribute.String("github.repository", identity.RepositoryFullName),
		attribute.String("github.scope_ref", scopeRef),
	)
	for _, op := range plan.Operations {
		_ = s.appendDurableEvent(ctx, op.event(item.ExecutionID, item.AttemptID, identity, durableEventPrepare, "succeeded", "", ""))
		selectEvent := op.event(item.ExecutionID, item.AttemptID, identity, durableEventSelect, boolResult(op.SourceSnapshotRef != "", "hit", "miss"), "", op.SourceSnapshotRef)
		if op.SourceSnapshotRef == "" {
			selectEvent.Reason = firstNonEmpty(op.SelectMissReason, "current_generation_missing")
		}
		if storageKnown {
			selectEvent = selectEvent.withStorageCapacity(storage.Capacity, identity.OrgID)
		}
		_ = s.appendDurableEvent(ctx, selectEvent)
	}
	return plan, nil
}

type goldenVMPlanSpec struct {
	JobShapeID        uuid.UUID
	ScopeRef          string
	TrustClass        string
	PromotionEligible bool
	Now               time.Time
}

func (s *Service) prepareGoldenVMPlan(ctx context.Context, item executionWorkItem, identity RunnerExecutionIdentity, ops []durableCacheOperation, spec goldenVMPlanSpec) (goldenVMPlan, error) {
	sourceHash := goldenVMSourceGenerationSetHash(nonDurableFilesystemMounts(item.FilesystemMounts), ops)
	operationID := uuid.New()
	candidateSnapshotID := uuid.New()
	inserted, err := s.storeQueries().InsertGoldenVMOperation(ctx, store.InsertGoldenVMOperationParams{
		OperationID:                 operationID,
		ExecutionID:                 item.ExecutionID,
		AttemptID:                   item.AttemptID,
		AllocationID:                &identity.AllocationID,
		OrgID:                       identity.OrgID,
		RepositoryID:                identity.ProviderRepositoryID,
		Provider:                    identity.Provider,
		ProviderRepositoryID:        identity.ProviderRepositoryID,
		ScopeKind:                   durableScopeKindBranch,
		ScopeRef:                    spec.ScopeRef,
		JobShapeID:                  spec.JobShapeID,
		TrustClass:                  spec.TrustClass,
		SourceGenerationSetHash:     sourceHash,
		CandidateGoldenVmSnapshotID: candidateSnapshotID,
		PromotionEligible:           spec.PromotionEligible,
		ProviderRunID:               identity.ProviderRunID,
		ProviderRunAttempt:          identity.ProviderRunAttempt,
		ProviderJobID:               identity.ProviderJobID,
		HeadSha:                     firstNonEmpty(identity.HeadSHA, identity.RunHeadSHA),
		RequestedAt:                 pgTime(spec.Now),
	})
	if err != nil {
		return goldenVMPlan{}, fmt.Errorf("insert golden VM operation: %w", err)
	}
	plan := goldenVMPlan{
		Enabled:                 true,
		OperationID:             inserted.OperationID,
		CandidateSnapshotID:     inserted.CandidateGoldenVmSnapshotID,
		JobShapeID:              spec.JobShapeID,
		ScopeRef:                spec.ScopeRef,
		TrustClass:              spec.TrustClass,
		SourceGenerationSetHash: inserted.SourceGenerationSetHash,
		PromotionEligible:       spec.PromotionEligible,
		LookupResult:            "miss",
		LookupReason:            "current_snapshot_missing",
	}
	current, err := s.storeQueries().GetCurrentGoldenVMActivation(ctx, store.GetCurrentGoldenVMActivationParams{
		OrgID:                identity.OrgID,
		RepositoryID:         identity.ProviderRepositoryID,
		Provider:             identity.Provider,
		ProviderRepositoryID: identity.ProviderRepositoryID,
		ScopeKind:            durableScopeKindBranch,
		ScopeRef:             spec.ScopeRef,
		JobShapeID:           spec.JobShapeID,
		TrustClass:           spec.TrustClass,
		GenerationSetHash:    sourceHash,
	})
	if err == nil {
		plan.LookupResult = "hit"
		plan.LookupReason = ""
		plan.Activation = vmorchestrator.GoldenVMActivation{
			SnapshotID:         current.GoldenVmSnapshotID.String(),
			GenerationSetHash:  current.GenerationSetHash,
			RootSnapshotRef:    current.RootSnapshotRef,
			RootSnapshotGUID:   current.RootSnapshotGuid,
			SnapshotKey:        current.SnapshotKey,
			VMStateArtifactRef: current.VmstateArtifactRef,
			MemoryArtifactRef:  current.MemoryArtifactRef,
		}
		_ = s.storeQueries().TouchGoldenVMSnapshotLastUsed(ctx, store.TouchGoldenVMSnapshotLastUsedParams{
			GoldenVmSnapshotID: current.GoldenVmSnapshotID,
			LastUsedAt:         pgTime(spec.Now),
		})
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return goldenVMPlan{}, fmt.Errorf("select current golden VM snapshot: %w", err)
	}
	_ = s.appendGoldenVMEvent(ctx, plan.event(item.ExecutionID, item.AttemptID, identity, goldenVMEventLookup, plan.LookupResult, plan.LookupReason))
	return plan, nil
}

type durableOperationSpec struct {
	JobShapeID        uuid.UUID
	ScopeRef          string
	CacheName         string
	MountName         string
	MountPath         string
	BindPaths         []string
	TrustClass        string
	PromotionEligible bool
	Required          bool
	SortOrder         int
	SourceSkipReason  string
	Now               time.Time
}

func (s *Service) insertDurableOperation(ctx context.Context, item executionWorkItem, identity RunnerExecutionIdentity, spec durableOperationSpec) (durableCacheOperation, error) {
	scopeID := stableUUID("durable-scope", identity.OrgID, identity.Provider, strconv.FormatInt(identity.ProviderRepositoryID, 10), spec.ScopeRef, spec.JobShapeID.String(), spec.CacheName, spec.TrustClass)
	scope, err := s.storeQueries().UpsertDurableScope(ctx, store.UpsertDurableScopeParams{
		DurableScopeID:       scopeID,
		OrgID:                identity.OrgID,
		RepositoryID:         identity.ProviderRepositoryID,
		Provider:             identity.Provider,
		ProviderRepositoryID: identity.ProviderRepositoryID,
		ScopeKind:            durableScopeKindBranch,
		ScopeRef:             spec.ScopeRef,
		JobShapeID:           spec.JobShapeID,
		CacheName:            spec.CacheName,
		TrustClass:           spec.TrustClass,
		CreatedAt:            pgTime(spec.Now),
	})
	if err != nil {
		return durableCacheOperation{}, fmt.Errorf("upsert durable scope %s: %w", spec.CacheName, err)
	}
	if err := s.storeQueries().EnsureDurableCurrentPointer(ctx, store.EnsureDurableCurrentPointerParams{DurableScopeID: scope, PromotedAt: pgTime(spec.Now)}); err != nil {
		return durableCacheOperation{}, fmt.Errorf("ensure durable current pointer %s: %w", spec.CacheName, err)
	}
	var sourceGenerationID *uuid.UUID
	sourceSnapshotRef := ""
	sourceSnapshotGUID := ""
	sourceSkipReason := ""
	current, err := s.storeQueries().GetCurrentDurableGeneration(ctx, store.GetCurrentDurableGenerationParams{DurableScopeID: scope})
	if err == nil {
		if spec.SourceSkipReason == "" {
			sourceGenerationID = &current.DurableGenerationID
			sourceSnapshotRef = current.ZfsSnapshotRef
			sourceSnapshotGUID = current.ZfsSnapshotGuid
			_ = s.storeQueries().TouchDurableGenerationLastUsed(ctx, store.TouchDurableGenerationLastUsedParams{
				DurableGenerationID: current.DurableGenerationID,
				LastUsedAt:          pgTime(spec.Now),
			})
		} else {
			sourceSkipReason = spec.SourceSkipReason
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return durableCacheOperation{}, fmt.Errorf("select current durable generation %s: %w", spec.CacheName, err)
	}
	bindPathsJSON, err := json.Marshal(spec.BindPaths)
	if err != nil {
		return durableCacheOperation{}, err
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
		SourceSkipReason:      sourceSkipReason,
		CandidateGenerationID: candidateGenerationID,
		MountName:             spec.MountName,
		InternalMountPath:     spec.MountPath,
		BindPathsJson:         bindPathsJSON,
		TrustClass:            spec.TrustClass,
		PromotionEligible:     spec.PromotionEligible,
		Required:              spec.Required,
		SortOrder:             int32FromInt(spec.SortOrder, "durable operation sort order"),
		RequestedAt:           pgTime(spec.Now),
	})
	if err != nil {
		return durableCacheOperation{}, fmt.Errorf("insert durable operation %s: %w", spec.CacheName, err)
	}
	return durableCacheOperation{
		OperationID:           op.OperationID,
		DurableScopeID:        op.DurableScopeID,
		JobShapeID:            spec.JobShapeID,
		SourceGenerationID:    op.SourceGenerationID,
		SourceSnapshotRef:     op.SourceSnapshotRef,
		SourceSnapshotGUID:    sourceSnapshotGUID,
		CandidateGenerationID: op.CandidateGenerationID,
		MountName:             op.MountName,
		MountPath:             op.InternalMountPath,
		BindPaths:             append([]string(nil), spec.BindPaths...),
		TrustClass:            op.TrustClass,
		CacheName:             spec.CacheName,
		PromotionEligible:     op.PromotionEligible,
		Required:              op.Required,
		SelectMissReason:      op.SourceSkipReason,
	}, nil
}

func (s *Service) recordDurableLeaseMountResults(ctx context.Context, plan durableCachePlan, results []vmorchestrator.FilesystemMountResult) (durableCachePlan, error) {
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
				s.Logger.WarnContext(ctx, "mark durable cache mounted failed", "operation_id", op.OperationID, "error", err)
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
			s.Logger.WarnContext(ctx, "mark durable cache skipped failed", "operation_id", op.OperationID, "error", err)
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

func (s *Service) failDurableCaches(ctx context.Context, plan durableCachePlan, reason string, cause error) {
	if !plan.Enabled {
		return
	}
	failureReason := durableFailureReason(reason, cause)
	now := time.Now().UTC()
	for _, op := range plan.Operations {
		if err := s.storeQueries().MarkDurableOperationFailed(ctx, store.MarkDurableOperationFailedParams{FailureReason: failureReason, RecordedAt: pgTime(now), OperationID: op.OperationID}); err != nil && s.Logger != nil {
			s.Logger.WarnContext(ctx, "mark durable cache failed", "operation_id", op.OperationID, "error", err)
		}
		_ = s.appendDurableEvent(ctx, op.event(plan.Identity.ExecutionID, plan.Identity.AttemptID, plan.Identity, durableEventPrepare, "failed", failureReason, ""))
	}
	if plan.GoldenVM.Enabled {
		if err := s.storeQueries().MarkGoldenVMOperationFailed(ctx, store.MarkGoldenVMOperationFailedParams{FailureReason: failureReason, RecordedAt: pgTime(now), OperationID: plan.GoldenVM.OperationID}); err != nil && s.Logger != nil {
			s.Logger.WarnContext(ctx, "mark golden VM operation failed", "operation_id", plan.GoldenVM.OperationID, "error", err)
		}
		_ = s.appendGoldenVMEvent(ctx, plan.GoldenVM.event(plan.Identity.ExecutionID, plan.Identity.AttemptID, plan.Identity, goldenVMEventCheckpoint, "failed", failureReason))
	}
}

func (s *Service) failOpenDurableOperationsForAttempt(ctx context.Context, item executionWorkItem, reason string, cause error) {
	failureReason := durableFailureReason(reason, cause)
	recordedAt := pgTime(time.Now().UTC())
	rows, err := s.storeQueries().MarkOpenDurableOperationsFailedByAttempt(ctx, store.MarkOpenDurableOperationsFailedByAttemptParams{FailureReason: failureReason, RecordedAt: recordedAt, AttemptID: item.AttemptID})
	if err != nil {
		if s.Logger != nil {
			s.Logger.WarnContext(ctx, "mark open durable operations failed", "attempt_id", item.AttemptID, "error", err)
		}
	} else {
		for _, row := range rows {
			_ = s.appendDurableEvent(ctx, durableEvent{OperationID: &row.OperationID, ScopeID: &row.DurableScopeID, ExecutionID: &row.ExecutionID, AttemptID: &row.AttemptID, CacheName: row.CacheName, Name: durableEventReconcile, Result: "failed", Reason: failureReason})
		}
	}
	goldenRows, err := s.storeQueries().MarkOpenGoldenVMOperationsFailedByAttempt(ctx, store.MarkOpenGoldenVMOperationsFailedByAttemptParams{FailureReason: failureReason, RecordedAt: recordedAt, AttemptID: item.AttemptID})
	if err != nil {
		if s.Logger != nil {
			s.Logger.WarnContext(ctx, "mark open golden VM operations failed", "attempt_id", item.AttemptID, "error", err)
		}
		return
	}
	for _, row := range goldenRows {
		identity := RunnerExecutionIdentity{OrgID: row.OrgID, Provider: row.Provider, ProviderRepositoryID: row.ProviderRepositoryID, ProviderRunID: row.ProviderRunID, ProviderRunAttempt: row.ProviderRunAttempt, ProviderJobID: row.ProviderJobID}
		_ = s.appendGoldenVMEvent(ctx, goldenVMEvent{OperationID: &row.OperationID, SnapshotID: &row.CandidateGoldenVmSnapshotID, JobShapeID: &row.JobShapeID, ExecutionID: &row.ExecutionID, AttemptID: &row.AttemptID, Identity: identity, Name: goldenVMEventCheckpoint, Result: "failed", Reason: failureReason, SourceGenerationSetHash: row.SourceGenerationSetHash})
	}
}

type goldenVMSnapshotGeneration struct {
	DurableScopeID      uuid.UUID
	DurableGenerationID uuid.UUID
	CacheName           string
	ZFSSnapshotRef      string
	ZFSSnapshotGUID     string
	DriveID             string
	MountPath           string
	BindPaths           []string
	FSType              string
	ReadOnly            bool
	Required            bool
	SortOrder           int
}

func (s *Service) finalizeDurableCaches(ctx context.Context, item executionWorkItem, leaseID string, plan durableCachePlan, sealDecision durableSealDecision) error {
	if !plan.Enabled {
		return nil
	}
	phaseStarted := time.Now().UTC()
	var retErr error
	commitSkipReason := durableCommitSkipReason(plan, sealDecision)
	defer func() {
		result, reason := sandboxPhaseResult(retErr)
		s.recordRunnerIdentityPhase(ctx, plan.Identity, item, "sandbox-rental", "sandbox.durable.seal", result, reason, phaseStarted, time.Now().UTC(), sandboxPhaseAttrs{
			"commit_allowed":     strconv.FormatBool(commitSkipReason == ""),
			"commit_skip_reason": commitSkipReason,
		})
	}()
	ctx, span := tracer.Start(ctx, durableEventSeal, trace.WithAttributes(
		attribute.String("lease.id", leaseID),
		attribute.Int("durable.cache_count", len(plan.Operations)),
	))
	defer span.End()
	var errs []error
	anyCandidate := false
	span.SetAttributes(
		attribute.Bool("durable.commit_allowed", commitSkipReason == ""),
		attribute.String("durable.commit_skip_reason", commitSkipReason),
	)
	checkpoint, checkpointErr := s.checkpointGoldenVMForDurable(ctx, item, leaseID, plan, sealDecision)
	if checkpointErr != nil {
		errs = append(errs, checkpointErr)
	}
	if commitSkipReason != "" {
		now := time.Now().UTC()
		for _, op := range plan.Operations {
			_ = s.storeQueries().MarkDurableOperationResultRecorded(ctx, store.MarkDurableOperationResultRecordedParams{FinalState: "skipped", SealedAt: pgTime(now), RecordedAt: pgTime(now), FailureReason: commitSkipReason, OperationID: op.OperationID})
			_ = s.appendDurableEvent(ctx, op.event(item.ExecutionID, item.AttemptID, plan.Identity, durableEventSeal, "skipped", commitSkipReason, ""))
		}
		if len(errs) > 0 {
			retErr = errors.Join(errs...)
			return retErr
		}
		return nil
	}
	goldenGenerations := make([]goldenVMSnapshotGeneration, 0, len(plan.Operations))
	for idx, op := range plan.Operations {
		if !op.Mounted {
			skipReason := firstNonEmpty(op.MountSkipReason, "mount_not_available")
			_ = s.appendDurableEvent(ctx, op.event(item.ExecutionID, item.AttemptID, plan.Identity, durableEventSeal, "skipped", skipReason, ""))
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
			errs = append(errs, fmt.Errorf("commit durable cache %s: %w", op.CacheName, err))
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
			ZfsSnapshotGuid:     commit.SnapshotGUID,
			UsedBytes:           usedBytes,
			WrittenBytes:        writtenBytes,
			SealedAt:            pgTime(commit.CommittedAt),
			CommittedAt:         pgTime(now),
			LastUsedAt:          pgTime(now),
			ExpiresAt:           pgOptionalTime(durableGenerationExpiresAt(op, now)),
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("insert durable generation %s: %w", op.CacheName, err))
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
		if checkpoint != nil && op.PromotionEligible {
			goldenGenerations = append(goldenGenerations, goldenVMSnapshotGeneration{
				DurableScopeID:      op.DurableScopeID,
				DurableGenerationID: generationID,
				CacheName:           op.CacheName,
				ZFSSnapshotRef:      commit.Snapshot,
				ZFSSnapshotGUID:     commit.SnapshotGUID,
				DriveID:             op.MountName,
				MountPath:           op.MountPath,
				BindPaths:           append([]string(nil), op.BindPaths...),
				FSType:              "ext4",
				ReadOnly:            false,
				Required:            op.Required,
				SortOrder:           idx,
			})
		}
	}
	goldenPublished := false
	if checkpoint != nil {
		if _, err := s.storeQueries().MarkGoldenVMOperationPublishing(ctx, store.MarkGoldenVMOperationPublishingParams{RecordedAt: pgTime(time.Now().UTC()), OperationID: plan.GoldenVM.OperationID}); err != nil {
			errs = append(errs, fmt.Errorf("mark golden VM publishing: %w", err))
		}
		if err := s.publishGoldenVMSnapshot(ctx, item, plan, *checkpoint, goldenGenerations); err != nil {
			errs = append(errs, err)
		} else {
			goldenPublished = true
		}
	}
	if anyCandidate {
		if _, err := s.promoteDurableWorkflowRun(ctx, goldenRunRefFromRunnerIdentity(plan.Identity)); err != nil {
			errs = append(errs, err)
		}
	}
	if goldenPublished {
		if _, err := s.promoteGoldenVMWorkflowRun(ctx, goldenRunRefFromRunnerIdentity(plan.Identity)); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		retErr = errors.Join(errs...)
		return retErr
	}
	return nil
}

func (s *Service) checkpointGoldenVMForDurable(ctx context.Context, item executionWorkItem, leaseID string, plan durableCachePlan, sealDecision durableSealDecision) (*vmorchestrator.GoldenVMCheckpointRecord, error) {
	if !plan.GoldenVM.Enabled {
		return nil, nil
	}
	skipReason := goldenVMCheckpointSkipReason(plan, sealDecision)
	if skipReason != "" {
		now := time.Now().UTC()
		if err := s.storeQueries().MarkGoldenVMOperationSkipped(ctx, store.MarkGoldenVMOperationSkippedParams{RecordedAt: pgTime(now), FailureReason: skipReason, OperationID: plan.GoldenVM.OperationID}); err != nil && s.Logger != nil {
			s.Logger.WarnContext(ctx, "mark golden VM operation skipped failed", "operation_id", plan.GoldenVM.OperationID, "error", err)
		}
		_ = s.appendGoldenVMEvent(ctx, plan.GoldenVM.event(item.ExecutionID, item.AttemptID, plan.Identity, goldenVMEventCheckpoint, "skipped", skipReason))
		return nil, nil
	}
	if _, err := s.storeQueries().MarkGoldenVMOperationCreating(ctx, store.MarkGoldenVMOperationCreatingParams{RecordedAt: pgTime(time.Now().UTC()), OperationID: plan.GoldenVM.OperationID}); err != nil {
		_ = s.appendGoldenVMEvent(ctx, plan.GoldenVM.event(item.ExecutionID, item.AttemptID, plan.Identity, goldenVMEventCheckpoint, "failed", err.Error()))
		return nil, fmt.Errorf("mark golden VM create started: %w", err)
	}
	checkpoint, err := s.Orchestrator.CheckpointGoldenVM(ctx, leaseID, plan.GoldenVM.OperationID.String()+":checkpoint", plan.GoldenVM.OperationID.String(), plan.GoldenVM.CandidateSnapshotID.String())
	if err != nil {
		reason := durableFailureReason("checkpoint_failed", err)
		if markErr := s.storeQueries().MarkGoldenVMOperationFailed(ctx, store.MarkGoldenVMOperationFailedParams{RecordedAt: pgTime(time.Now().UTC()), FailureReason: reason, OperationID: plan.GoldenVM.OperationID}); markErr != nil && s.Logger != nil {
			s.Logger.WarnContext(ctx, "mark golden VM operation failed failed", "operation_id", plan.GoldenVM.OperationID, "error", markErr)
		}
		_ = s.appendGoldenVMEvent(ctx, plan.GoldenVM.event(item.ExecutionID, item.AttemptID, plan.Identity, goldenVMEventCheckpoint, "failed", reason))
		return nil, fmt.Errorf("checkpoint golden VM: %w", err)
	}
	mountSnapshotsJSON, err := goldenVMMountSnapshotsJSON(checkpoint.MountSnapshots)
	if err != nil {
		_ = s.appendGoldenVMEvent(ctx, plan.GoldenVM.event(item.ExecutionID, item.AttemptID, plan.Identity, goldenVMEventCheckpoint, "failed", err.Error()))
		return nil, err
	}
	stateBytes, err := int64FromUint64("golden VM state bytes", checkpoint.StateBytes)
	if err != nil {
		return nil, err
	}
	memoryBytes, err := int64FromUint64("golden VM memory bytes", checkpoint.MemoryBytes)
	if err != nil {
		return nil, err
	}
	if _, err := s.storeQueries().MarkGoldenVMOperationCreated(ctx, store.MarkGoldenVMOperationCreatedParams{
		SnapshotKey:        checkpoint.SnapshotKey,
		RootSnapshotRef:    checkpoint.RootSnapshotRef,
		RootSnapshotGuid:   checkpoint.RootSnapshotGUID,
		VmstateArtifactRef: checkpoint.VMStateArtifactRef,
		MemoryArtifactRef:  checkpoint.MemoryArtifactRef,
		MountSnapshotsJson: mountSnapshotsJSON,
		StateBytes:         stateBytes,
		MemoryBytes:        memoryBytes,
		RecordedAt:         pgTime(checkpoint.CheckpointedAt),
		OperationID:        plan.GoldenVM.OperationID,
	}); err != nil {
		_ = s.appendGoldenVMEvent(ctx, plan.GoldenVM.event(item.ExecutionID, item.AttemptID, plan.Identity, goldenVMEventCheckpoint, "failed", err.Error()))
		return nil, fmt.Errorf("mark golden VM created: %w", err)
	}
	beforeEvent := plan.GoldenVM.event(item.ExecutionID, item.AttemptID, plan.Identity, goldenVMEventBeforeSnapshot, "succeeded", "")
	beforeEvent.SnapshotKey = checkpoint.SnapshotKey
	beforeEvent.VMStateArtifactRef = checkpoint.VMStateArtifactRef
	beforeEvent.MemoryArtifactRef = checkpoint.MemoryArtifactRef
	beforeEvent.RootSnapshotRef = checkpoint.RootSnapshotRef
	beforeEvent.RootSnapshotGUID = checkpoint.RootSnapshotGUID
	_ = s.appendGoldenVMEvent(ctx, beforeEvent)
	checkpointEvent := beforeEvent
	checkpointEvent.Name = goldenVMEventCheckpoint
	checkpointEvent.StateBytes = checkpoint.StateBytes
	checkpointEvent.MemoryBytes = checkpoint.MemoryBytes
	_ = s.appendGoldenVMEvent(ctx, checkpointEvent)
	return &checkpoint, nil
}

func goldenVMCheckpointSkipReason(plan durableCachePlan, sealDecision durableSealDecision) string {
	if !sealDecision.Commit {
		return firstNonEmpty(sealDecision.SkipReason, "exec_not_success")
	}
	if !plan.GoldenVM.PromotionEligible {
		return "non_promotable_scope"
	}
	for _, op := range plan.Operations {
		if !op.Mounted {
			return "mount_not_available"
		}
	}
	return ""
}

func (s *Service) publishGoldenVMSnapshot(ctx context.Context, item executionWorkItem, plan durableCachePlan, checkpoint vmorchestrator.GoldenVMCheckpointRecord, generations []goldenVMSnapshotGeneration) error {
	if !plan.GoldenVM.Enabled {
		return nil
	}
	if len(generations) != len(plan.Operations) {
		err := fmt.Errorf("golden VM generation manifest incomplete: got %d generations for %d mounts", len(generations), len(plan.Operations))
		reason := durableFailureReason("publish_failed", err)
		_ = s.storeQueries().MarkGoldenVMOperationFailed(ctx, store.MarkGoldenVMOperationFailedParams{RecordedAt: pgTime(time.Now().UTC()), FailureReason: reason, OperationID: plan.GoldenVM.OperationID})
		_ = s.appendGoldenVMEvent(ctx, plan.GoldenVM.event(item.ExecutionID, item.AttemptID, plan.Identity, goldenVMEventPublish, "failed", reason))
		return err
	}
	sort.Slice(generations, func(i, j int) bool {
		if generations[i].SortOrder != generations[j].SortOrder {
			return generations[i].SortOrder < generations[j].SortOrder
		}
		return generations[i].DurableScopeID.String() < generations[j].DurableScopeID.String()
	})
	generationSetHash := goldenVMCandidateGenerationSetHash(nonDurableFilesystemMounts(item.FilesystemMounts), generations)
	now := time.Now().UTC()
	stateBytes, err := int64FromUint64("golden VM state bytes", checkpoint.StateBytes)
	if err != nil {
		return err
	}
	memoryBytes, err := int64FromUint64("golden VM memory bytes", checkpoint.MemoryBytes)
	if err != nil {
		return err
	}
	_, err = s.storeQueries().InsertGoldenVMSnapshot(ctx, store.InsertGoldenVMSnapshotParams{
		GoldenVmSnapshotID:        plan.GoldenVM.CandidateSnapshotID,
		OperationID:               plan.GoldenVM.OperationID,
		OrgID:                     plan.Identity.OrgID,
		RepositoryID:              plan.Identity.ProviderRepositoryID,
		Provider:                  plan.Identity.Provider,
		ProviderRepositoryID:      plan.Identity.ProviderRepositoryID,
		ScopeKind:                 durableScopeKindBranch,
		ScopeRef:                  plan.GoldenVM.ScopeRef,
		JobShapeID:                plan.GoldenVM.JobShapeID,
		TrustClass:                plan.GoldenVM.TrustClass,
		GenerationSetHash:         generationSetHash,
		RootSnapshotRef:           checkpoint.RootSnapshotRef,
		RootSnapshotGuid:          checkpoint.RootSnapshotGUID,
		SnapshotKey:               checkpoint.SnapshotKey,
		VmstateArtifactRef:        checkpoint.VMStateArtifactRef,
		MemoryArtifactRef:         checkpoint.MemoryArtifactRef,
		StateBytes:                stateBytes,
		MemoryBytes:               memoryBytes,
		DriveManifestHash:         goldenVMDriveManifestHash(checkpoint),
		MountManifestHash:         generationSetHash,
		FirecrackerAbiHash:        "firecracker-v1.15.0",
		HostAbiHash:               durableGuestArch,
		NetworkModelHash:          stableHex("nat", durableGuestArch, "host-service-plane-v1"),
		VsockModelHash:            stableHex("vm-bridge", strconv.Itoa(vmproto.ProtocolVersion), "single-control-vsock"),
		ClockModelHash:            stableHex("clock-realtime", "after-restore-settime-v1"),
		VmprotoVersion:            int32(vmproto.ProtocolVersion),
		AfterRestoreHookVersion:   "after_restore.v1",
		BeforeSnapshotHookVersion: "before_golden_snapshot.v1",
		WarmProfileHash:           plan.GoldenVM.SourceGenerationSetHash,
		Vcpus:                     int32FromUint32(item.Resources.VCPUs, "golden VM vcpus"),
		MemoryMib:                 int32FromUint32(item.Resources.MemoryMiB, "golden VM memory mib"),
		ProviderRunID:             plan.Identity.ProviderRunID,
		ProviderRunAttempt:        plan.Identity.ProviderRunAttempt,
		ProviderJobID:             plan.Identity.ProviderJobID,
		HeadSha:                   firstNonEmpty(plan.Identity.HeadSHA, plan.Identity.RunHeadSHA),
		TreeHash:                  "",
		State:                     "candidate",
		CreatedAt:                 pgTime(now),
		LastUsedAt:                pgTime(now),
		ExpiresAt:                 pgTime(now.Add(durableRetainedGenerationTTL)),
	})
	if err != nil {
		reason := durableFailureReason("publish_failed", err)
		_ = s.storeQueries().MarkGoldenVMOperationFailed(ctx, store.MarkGoldenVMOperationFailedParams{RecordedAt: pgTime(now), FailureReason: reason, OperationID: plan.GoldenVM.OperationID})
		_ = s.appendGoldenVMEvent(ctx, plan.GoldenVM.event(item.ExecutionID, item.AttemptID, plan.Identity, goldenVMEventPublish, "failed", reason))
		return fmt.Errorf("insert golden VM snapshot: %w", err)
	}
	for _, gen := range generations {
		bindPathsJSON, err := json.Marshal(gen.BindPaths)
		if err != nil {
			return err
		}
		if err := s.storeQueries().InsertGoldenVMSnapshotGeneration(ctx, store.InsertGoldenVMSnapshotGenerationParams{
			GoldenVmSnapshotID:  plan.GoldenVM.CandidateSnapshotID,
			DurableScopeID:      gen.DurableScopeID,
			DurableGenerationID: gen.DurableGenerationID,
			CacheName:           gen.CacheName,
			ZfsSnapshotRef:      gen.ZFSSnapshotRef,
			ZfsSnapshotGuid:     gen.ZFSSnapshotGUID,
			DriveID:             gen.DriveID,
			MountPath:           gen.MountPath,
			BindPathsJson:       bindPathsJSON,
			FsType:              gen.FSType,
			ReadOnly:            gen.ReadOnly,
			Required:            gen.Required,
			SortOrder:           int32FromInt(gen.SortOrder, "golden VM snapshot generation sort order"),
		}); err != nil {
			reason := durableFailureReason("publish_failed", err)
			_ = s.storeQueries().MarkGoldenVMOperationFailed(ctx, store.MarkGoldenVMOperationFailedParams{RecordedAt: pgTime(now), FailureReason: reason, OperationID: plan.GoldenVM.OperationID})
			_ = s.appendGoldenVMEvent(ctx, plan.GoldenVM.event(item.ExecutionID, item.AttemptID, plan.Identity, goldenVMEventPublish, "failed", reason))
			return fmt.Errorf("insert golden VM snapshot generation %s: %w", gen.CacheName, err)
		}
	}
	if _, err := s.storeQueries().MarkGoldenVMOperationPublished(ctx, store.MarkGoldenVMOperationPublishedParams{GenerationSetHash: generationSetHash, RecordedAt: pgTime(now), OperationID: plan.GoldenVM.OperationID}); err != nil {
		return fmt.Errorf("mark golden VM operation published: %w", err)
	}
	publishEvent := plan.GoldenVM.event(item.ExecutionID, item.AttemptID, plan.Identity, goldenVMEventPublish, "succeeded", "")
	publishEvent.GenerationSetHash = generationSetHash
	publishEvent.SnapshotKey = checkpoint.SnapshotKey
	publishEvent.VMStateArtifactRef = checkpoint.VMStateArtifactRef
	publishEvent.MemoryArtifactRef = checkpoint.MemoryArtifactRef
	publishEvent.RootSnapshotRef = checkpoint.RootSnapshotRef
	publishEvent.RootSnapshotGUID = checkpoint.RootSnapshotGUID
	publishEvent.StateBytes = checkpoint.StateBytes
	publishEvent.MemoryBytes = checkpoint.MemoryBytes
	_ = s.appendGoldenVMEvent(ctx, publishEvent)
	return nil
}

type durableSealDecision struct {
	Commit     bool
	SkipReason string
}

func durableCommitSkipReason(plan durableCachePlan, sealDecision durableSealDecision) string {
	if !sealDecision.Commit {
		return firstNonEmpty(sealDecision.SkipReason, "exec_not_success")
	}
	if !durablePlanPromotionEligible(plan) {
		return "non_promotable_scope"
	}
	return ""
}

func durablePlanPromotionEligible(plan durableCachePlan) bool {
	if plan.GoldenVM.Enabled && plan.GoldenVM.PromotionEligible {
		return true
	}
	for _, op := range plan.Operations {
		if op.PromotionEligible {
			return true
		}
	}
	return false
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

type durableStorageSnapshot struct {
	Capacity          vmorchestrator.Capacity
	AllocatedPermille uint64
}

func (s *Service) durableStorageSnapshot(ctx context.Context) (durableStorageSnapshot, bool, error) {
	if s.Orchestrator == nil {
		return durableStorageSnapshot{}, false, nil
	}
	capacity, err := s.Orchestrator.GetCapacity(ctx)
	if err != nil {
		return durableStorageSnapshot{}, false, fmt.Errorf("query durable storage capacity: %w", err)
	}
	return durableStorageSnapshot{
		Capacity:          capacity,
		AllocatedPermille: poolAllocatedPermille(capacity),
	}, true, nil
}

func poolAllocatedPermille(capacity vmorchestrator.Capacity) uint64 {
	if capacity.ZpoolSizeBytes == 0 || capacity.ZpoolAllocatedBytes == 0 {
		return 0
	}
	if capacity.ZpoolAllocatedBytes >= capacity.ZpoolSizeBytes {
		return 1000
	}
	hi, lo := bits.Mul64(capacity.ZpoolAllocatedBytes, 1000)
	if hi >= capacity.ZpoolSizeBytes {
		return 1000
	}
	permille, _ := bits.Div64(hi, lo, capacity.ZpoolSizeBytes)
	return permille
}

func (event durableEvent) withStorageCapacity(capacity vmorchestrator.Capacity, orgID string) durableEvent {
	event.PoolSizeBytes = capacity.ZpoolSizeBytes
	event.PoolAllocatedBytes = capacity.ZpoolAllocatedBytes
	event.PoolFreeBytes = capacity.ZpoolFreeBytes
	for _, org := range capacity.OrgStorage {
		if org.OrgID != orgID {
			continue
		}
		event.OrgQuotaBytes = org.QuotaBytes
		event.OrgUsedBytes = org.UsedBytes
		event.OrgAvailableBytes = org.AvailableBytes
		break
	}
	return event
}

func durableCommitFailureEvents(op durableCacheOperation, executionID, attemptID uuid.UUID, identity RunnerExecutionIdentity, cause error) []durableEvent {
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

func durableGenerationInitialState(op durableCacheOperation) string {
	if op.PromotionEligible {
		return "committed"
	}
	return "retained"
}

func durableGenerationExpiresAt(op durableCacheOperation, now time.Time) time.Time {
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
	ref := goldenWorkflowRunRef{OrgID: orgID, Provider: RunnerProviderGitHub, ProviderInstallationID: installationID, ProviderRepositoryID: req.ProviderRepositoryID, ProviderRunID: req.ProviderRunID, ProviderRunAttempt: req.ProviderRunAttempt, RepositoryFullName: repositoryFullName, HeadSHA: req.HeadSHA}
	if _, err := s.promoteDurableWorkflowRun(ctx, ref); err != nil {
		return err
	}
	_, err = s.promoteGoldenVMWorkflowRun(ctx, ref)
	return err
}

func (s *Service) observeDurableStorage(ctx context.Context) error {
	storage, storageKnown, err := s.durableStorageSnapshot(ctx)
	if err != nil {
		_ = s.appendDurableEvent(ctx, durableEvent{Name: durableEventPoolObserve, Result: "failed", Reason: err.Error()})
		return err
	}
	if !storageKnown {
		return nil
	}
	if len(storage.Capacity.OrgStorage) == 0 {
		_ = s.appendDurableEvent(ctx, durableEvent{Name: durableEventPoolObserve, Result: "succeeded"}.withStorageCapacity(storage.Capacity, ""))
		return nil
	}
	for _, org := range storage.Capacity.OrgStorage {
		event := durableEvent{
			Identity: RunnerExecutionIdentity{OrgID: org.OrgID},
			Name:     durableEventPoolObserve,
			Result:   "succeeded",
		}
		_ = s.appendDurableEvent(ctx, event.withStorageCapacity(storage.Capacity, org.OrgID))
	}
	return nil
}

func (s *Service) pruneDurableGenerations(ctx context.Context) error {
	if s.Orchestrator == nil {
		return nil
	}
	now := time.Now().UTC()
	storage, storageKnown, err := s.durableStorageSnapshot(ctx)
	if err != nil {
		return err
	}
	if storageKnown && storage.AllocatedPermille >= durablePoolLowWatermarkPermille {
		var errs []error
		for batch := 0; batch < durableEvictionMaxBatches; batch++ {
			rows, err := s.storeQueries().ListEvictableDurableGenerations(ctx, store.ListEvictableDurableGenerationsParams{
				LimitCount: durablePruneBatchSize,
			})
			if err != nil {
				return fmt.Errorf("query evictable durable generations: %w", err)
			}
			pruned, err := s.pruneDurableGenerationCandidates(ctx, now, durableEventEvict, "zpool_low_watermark_exceeded", storage, evictableDurablePruneCandidates(rows))
			if err != nil {
				errs = append(errs, err)
			}
			if pruned == 0 {
				break
			}
			storage, storageKnown, err = s.durableStorageSnapshot(ctx)
			if err != nil {
				errs = append(errs, err)
				break
			}
			if !storageKnown || storage.AllocatedPermille < durablePoolLowWatermarkPermille {
				break
			}
		}
		return errors.Join(errs...)
	}
	rows, err := s.storeQueries().ListPrunableDurableGenerations(ctx, store.ListPrunableDurableGenerationsParams{
		NowAt:      pgTime(now),
		LimitCount: durablePruneBatchSize,
	})
	if err != nil {
		return fmt.Errorf("query prunable durable generations: %w", err)
	}
	_, err = s.pruneDurableGenerationCandidates(ctx, now, durableEventPrune, "retention_ttl_expired", storage, prunableDurablePruneCandidates(rows))
	return err
}

func (s *Service) pruneGoldenVMSnapshots(ctx context.Context) error {
	if s.Orchestrator == nil {
		return nil
	}
	now := time.Now().UTC()
	rows, err := s.storeQueries().ListPrunableGoldenVMSnapshots(ctx, store.ListPrunableGoldenVMSnapshotsParams{
		NowAt:      pgTime(now),
		LimitCount: durablePruneBatchSize,
	})
	if err != nil {
		return fmt.Errorf("query prunable golden VM snapshots: %w", err)
	}
	var errs []error
	for _, row := range rows {
		changed, err := s.storeQueries().MarkGoldenVMSnapshotPruning(ctx, store.MarkGoldenVMSnapshotPruningParams{GoldenVmSnapshotID: row.GoldenVmSnapshotID})
		if err != nil {
			errs = append(errs, fmt.Errorf("mark golden VM snapshot pruning %s: %w", row.GoldenVmSnapshotID, err))
			continue
		}
		if changed == 0 {
			continue
		}
		identity := RunnerExecutionIdentity{
			OrgID:                row.OrgID,
			Provider:             row.Provider,
			ProviderRepositoryID: row.ProviderRepositoryID,
			ProviderRunID:        row.ProviderRunID,
			ProviderRunAttempt:   row.ProviderRunAttempt,
			ProviderJobID:        row.ProviderJobID,
		}
		_, err = s.Orchestrator.PruneGoldenVMSnapshot(ctx, row.GoldenVmSnapshotID.String()+":prune", row.OperationID.String(), row.GoldenVmSnapshotID.String(), row.SnapshotKey, row.RootSnapshotRef, row.OrgID)
		if err != nil {
			event := goldenVMEvent{OperationID: &row.OperationID, SnapshotID: &row.GoldenVmSnapshotID, JobShapeID: &row.JobShapeID, Identity: identity, Name: goldenVMEventPrune, Result: "failed", Reason: err.Error(), GenerationSetHash: row.GenerationSetHash, SourceGenerationSetHash: row.SourceGenerationSetHash, SnapshotKey: row.SnapshotKey, VMStateArtifactRef: row.VmstateArtifactRef, MemoryArtifactRef: row.MemoryArtifactRef, RootSnapshotRef: row.RootSnapshotRef, RootSnapshotGUID: row.RootSnapshotGuid, StateBytes: uint64FromInt64(row.StateBytes, "golden VM state bytes"), MemoryBytes: uint64FromInt64(row.MemoryBytes, "golden VM memory bytes")}
			_ = s.appendGoldenVMEvent(ctx, event)
			errs = append(errs, fmt.Errorf("prune golden VM snapshot %s: %w", row.GoldenVmSnapshotID, err))
			continue
		}
		if err := s.storeQueries().MarkGoldenVMSnapshotPruned(ctx, store.MarkGoldenVMSnapshotPrunedParams{PrunedAt: pgTime(time.Now().UTC()), GoldenVmSnapshotID: row.GoldenVmSnapshotID}); err != nil {
			errs = append(errs, fmt.Errorf("mark golden VM snapshot pruned %s: %w", row.GoldenVmSnapshotID, err))
			continue
		}
		event := goldenVMEvent{OperationID: &row.OperationID, SnapshotID: &row.GoldenVmSnapshotID, JobShapeID: &row.JobShapeID, Identity: identity, Name: goldenVMEventPrune, Result: "succeeded", Reason: "retention_ttl_expired", GenerationSetHash: row.GenerationSetHash, SourceGenerationSetHash: row.SourceGenerationSetHash, SnapshotKey: row.SnapshotKey, VMStateArtifactRef: row.VmstateArtifactRef, MemoryArtifactRef: row.MemoryArtifactRef, RootSnapshotRef: row.RootSnapshotRef, RootSnapshotGUID: row.RootSnapshotGuid, StateBytes: uint64FromInt64(row.StateBytes, "golden VM state bytes"), MemoryBytes: uint64FromInt64(row.MemoryBytes, "golden VM memory bytes")}
		_ = s.appendGoldenVMEvent(ctx, event)
	}
	return errors.Join(errs...)
}

type durablePruneCandidate struct {
	DurableGenerationID  uuid.UUID
	DurableScopeID       uuid.UUID
	OperationID          uuid.UUID
	ZFSSnapshotRef       string
	UsedBytes            int64
	WrittenBytes         int64
	ProviderRunID        int64
	ProviderRunAttempt   int64
	ProviderJobID        int64
	OrgID                string
	Provider             string
	ProviderRepositoryID int64
	CacheName            string
	ExecutionID          uuid.UUID
	AttemptID            uuid.UUID
}

func prunableDurablePruneCandidates(rows []store.ListPrunableDurableGenerationsRow) []durablePruneCandidate {
	out := make([]durablePruneCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, durablePruneCandidate{
			DurableGenerationID:  row.DurableGenerationID,
			DurableScopeID:       row.DurableScopeID,
			OperationID:          row.OperationID,
			ZFSSnapshotRef:       row.ZfsSnapshotRef,
			UsedBytes:            row.UsedBytes,
			WrittenBytes:         row.WrittenBytes,
			ProviderRunID:        row.ProviderRunID,
			ProviderRunAttempt:   row.ProviderRunAttempt,
			ProviderJobID:        row.ProviderJobID,
			OrgID:                row.OrgID,
			Provider:             row.Provider,
			ProviderRepositoryID: row.ProviderRepositoryID,
			CacheName:            row.CacheName,
			ExecutionID:          row.ExecutionID,
			AttemptID:            row.AttemptID,
		})
	}
	return out
}

func evictableDurablePruneCandidates(rows []store.ListEvictableDurableGenerationsRow) []durablePruneCandidate {
	out := make([]durablePruneCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, durablePruneCandidate{
			DurableGenerationID:  row.DurableGenerationID,
			DurableScopeID:       row.DurableScopeID,
			OperationID:          row.OperationID,
			ZFSSnapshotRef:       row.ZfsSnapshotRef,
			UsedBytes:            row.UsedBytes,
			WrittenBytes:         row.WrittenBytes,
			ProviderRunID:        row.ProviderRunID,
			ProviderRunAttempt:   row.ProviderRunAttempt,
			ProviderJobID:        row.ProviderJobID,
			OrgID:                row.OrgID,
			Provider:             row.Provider,
			ProviderRepositoryID: row.ProviderRepositoryID,
			CacheName:            row.CacheName,
			ExecutionID:          row.ExecutionID,
			AttemptID:            row.AttemptID,
		})
	}
	return out
}

func (s *Service) pruneDurableGenerationCandidates(ctx context.Context, now time.Time, eventName, reason string, storage durableStorageSnapshot, rows []durablePruneCandidate) (int, error) {
	var errs []error
	pruned := 0
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
			OrgID:                row.OrgID,
			Provider:             row.Provider,
			ProviderRepositoryID: row.ProviderRepositoryID,
			ProviderRunID:        row.ProviderRunID,
			ProviderRunAttempt:   row.ProviderRunAttempt,
			ProviderJobID:        row.ProviderJobID,
		}
		_, err = s.Orchestrator.PruneFilesystemGeneration(ctx, row.DurableGenerationID.String()+":prune", row.OperationID.String(), row.DurableGenerationID.String(), row.DurableScopeID.String(), row.ZFSSnapshotRef, row.OrgID)
		if err != nil {
			event := durableEvent{OperationID: &row.OperationID, ScopeID: &row.DurableScopeID, GenerationID: &row.DurableGenerationID, ExecutionID: &row.ExecutionID, AttemptID: &row.AttemptID, Identity: identity, CacheName: row.CacheName, Name: eventName, Result: "failed", Reason: err.Error(), ZFSSnapshotRef: row.ZFSSnapshotRef, UsedBytes: uint64FromInt64(row.UsedBytes, "durable used bytes"), WrittenBytes: uint64FromInt64(row.WrittenBytes, "durable written bytes")}
			_ = s.appendDurableEvent(ctx, event.withStorageCapacity(storage.Capacity, row.OrgID))
			errs = append(errs, fmt.Errorf("prune durable generation %s: %w", row.DurableGenerationID, err))
			continue
		}
		if err := s.storeQueries().MarkDurableGenerationPruned(ctx, store.MarkDurableGenerationPrunedParams{PrunedAt: pgTime(time.Now().UTC()), DurableGenerationID: row.DurableGenerationID, DurableScopeID: row.DurableScopeID}); err != nil {
			errs = append(errs, fmt.Errorf("mark durable generation pruned %s: %w", row.DurableGenerationID, err))
			continue
		}
		event := durableEvent{OperationID: &row.OperationID, ScopeID: &row.DurableScopeID, GenerationID: &row.DurableGenerationID, ExecutionID: &row.ExecutionID, AttemptID: &row.AttemptID, Identity: identity, CacheName: row.CacheName, Name: eventName, Result: "succeeded", Reason: reason, ZFSSnapshotRef: row.ZFSSnapshotRef, UsedBytes: uint64FromInt64(row.UsedBytes, "durable used bytes"), WrittenBytes: uint64FromInt64(row.WrittenBytes, "durable written bytes")}
		_ = s.appendDurableEvent(ctx, event.withStorageCapacity(storage.Capacity, row.OrgID))
		pruned++
	}
	return pruned, errors.Join(errs...)
}

func (s *Service) hydrateGitHubRunIdentity(ctx context.Context, identity RunnerExecutionIdentity) (RunnerExecutionIdentity, error) {
	if identity.Provider != RunnerProviderGitHub || identity.ProviderRunID <= 0 {
		return identity, nil
	}
	invocation, err := s.githubWorkflowRunInvocationForRef(ctx, goldenRunRefFromRunnerIdentity(identity))
	if err != nil {
		return RunnerExecutionIdentity{}, fmt.Errorf("load github workflow run invocation: %w", err)
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
	_ = s.appendDurableEvent(ctx, durableEvent{OperationID: &candidate.OperationID, ScopeID: &candidate.DurableScopeID, GenerationID: &candidate.DurableGenerationID, SourceGenerationID: candidate.SourceGenerationID, CurrentGenerationID: &currentGenerationID, ExecutionID: &candidate.ExecutionID, AttemptID: &candidate.AttemptID, Identity: identity, CacheName: candidate.CacheName, Name: durableEventPromote, Result: result, ZFSSnapshotRef: candidate.ZfsSnapshotRef, UsedBytes: uint64FromInt64(candidate.UsedBytes, "durable used bytes"), WrittenBytes: uint64FromInt64(candidate.WrittenBytes, "durable written bytes")})
	return promoted, nil
}

func (s *Service) promoteGoldenVMWorkflowRun(ctx context.Context, ref goldenWorkflowRunRef) (bool, error) {
	ctx, span := tracer.Start(ctx, "golden_vm.workflow_run.promote", trace.WithAttributes(
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
		span.SetAttributes(attribute.Bool("golden_vm.promoted", false), attribute.String("golden_vm.promotion_deferred_reason", reason))
		return false, nil
	}
	state, err := s.githubWorkflowRunPromotionStateForRef(ctx, ref)
	if err != nil {
		return false, err
	}
	promotionReady, reason := state.promotionReady()
	if !promotionReady {
		span.SetAttributes(attribute.Bool("golden_vm.promoted", false), attribute.String("golden_vm.promotion_deferred_reason", reason))
		return false, nil
	}
	candidates, err := s.storeQueries().ListGoldenVMSnapshotCandidatesForRun(ctx, store.ListGoldenVMSnapshotCandidatesForRunParams{ProviderRunID: ref.ProviderRunID, ProviderRunAttempt: ref.ProviderRunAttempt, HeadSha: firstNonEmpty(invocation.HeadSHA, ref.HeadSHA)})
	if err != nil {
		return false, err
	}
	span.SetAttributes(attribute.Int("golden_vm.candidate_count", len(candidates)))
	anyPromoted := false
	for _, candidate := range candidates {
		promoted, err := s.promoteGoldenVMCandidate(ctx, candidate, ref)
		if err != nil {
			return anyPromoted, err
		}
		anyPromoted = anyPromoted || promoted
	}
	span.SetAttributes(attribute.Bool("golden_vm.promoted", anyPromoted))
	return anyPromoted, nil
}

func (s *Service) promoteGoldenVMCandidate(ctx context.Context, candidate store.ListGoldenVMSnapshotCandidatesForRunRow, ref goldenWorkflowRunRef) (bool, error) {
	ctx, span := tracer.Start(ctx, goldenVMEventPromote, trace.WithAttributes(
		attribute.String("golden_vm.operation_id", candidate.OperationID.String()),
		attribute.String("golden_vm.snapshot_id", candidate.GoldenVmSnapshotID.String()),
		attribute.String("golden_vm.snapshot_key", candidate.SnapshotKey),
		attribute.String("golden_vm.generation_set_hash", candidate.GenerationSetHash),
		attribute.String("golden_vm.source_generation_set_hash", candidate.SourceGenerationSetHash),
	))
	defer span.End()
	now := time.Now().UTC()
	candidateID := candidate.GoldenVmSnapshotID
	operationID := candidate.OperationID
	if _, err := s.storeQueries().MarkGoldenVMOperationPromoting(ctx, store.MarkGoldenVMOperationPromotingParams{RecordedAt: pgTime(now), OperationID: operationID}); err != nil && s.Logger != nil {
		s.Logger.WarnContext(ctx, "mark golden VM operation promoting failed", "operation_id", operationID, "error", err)
	}
	row, err := s.storeQueries().PromoteGoldenVMSnapshotCAS(ctx, store.PromoteGoldenVMSnapshotCASParams{
		OrgID:                       candidate.OrgID,
		RepositoryID:                candidate.RepositoryID,
		Provider:                    candidate.Provider,
		ProviderRepositoryID:        candidate.ProviderRepositoryID,
		ScopeKind:                   candidate.ScopeKind,
		ScopeRef:                    candidate.ScopeRef,
		JobShapeID:                  candidate.JobShapeID,
		TrustClass:                  candidate.TrustClass,
		PromotedAt:                  pgTime(now),
		CandidateGoldenVmSnapshotID: candidateID,
		OperationID:                 &operationID,
		SourceGenerationSetHash:     candidate.SourceGenerationSetHash,
		ExpiresAt:                   pgTime(now.Add(durableRetainedGenerationTTL)),
	})
	if err != nil {
		return false, err
	}
	result := "conflicted"
	if row.Promoted {
		result = "succeeded"
	}
	if err := s.storeQueries().MarkGoldenVMOperationCommitted(ctx, store.MarkGoldenVMOperationCommittedParams{RecordedAt: pgTime(now), OperationID: operationID}); err != nil && s.Logger != nil {
		s.Logger.WarnContext(ctx, "mark golden VM operation committed failed", "operation_id", operationID, "error", err)
	}
	span.SetAttributes(attribute.Bool("golden_vm.promoted", row.Promoted), attribute.String("golden_vm.promotion_result", result))
	identity := RunnerExecutionIdentity{OrgID: ref.OrgID, Provider: ref.Provider, ProviderRepositoryID: ref.ProviderRepositoryID, ProviderRunID: ref.ProviderRunID, ProviderRunAttempt: ref.ProviderRunAttempt, ProviderJobID: candidate.ProviderJobID, RepositoryFullName: ref.RepositoryFullName}
	_ = s.appendGoldenVMEvent(ctx, goldenVMEvent{OperationID: &candidate.OperationID, SnapshotID: &candidate.GoldenVmSnapshotID, JobShapeID: &candidate.JobShapeID, Identity: identity, Name: goldenVMEventPromote, Result: result, GenerationSetHash: candidate.GenerationSetHash, SourceGenerationSetHash: candidate.SourceGenerationSetHash, SnapshotKey: candidate.SnapshotKey})
	return row.Promoted, nil
}

func (s *Service) githubWorkflowRunPromotionStateForRef(ctx context.Context, ref goldenWorkflowRunRef) (githubWorkflowRunJobsState, error) {
	if ref.Provider != RunnerProviderGitHub {
		return githubWorkflowRunJobsState{Total: 1, Completed: 1, Succeeded: 1}, nil
	}
	rows, err := s.storeQueries().ListWorkflowRunJobResults(ctx, store.ListWorkflowRunJobResultsParams{
		Provider:             ref.Provider,
		ProviderRepositoryID: ref.ProviderRepositoryID,
		ProviderRunID:        ref.ProviderRunID,
		ProviderRunAttempt:   ref.ProviderRunAttempt,
	})
	if err != nil {
		return githubWorkflowRunJobsState{}, err
	}
	state := githubWorkflowRunJobsState{
		Total:          len(rows),
		JobStatuses:    make(map[int64]string, len(rows)),
		JobConclusions: make(map[int64]string, len(rows)),
	}
	for _, row := range rows {
		state.JobStatuses[row.ProviderJobID] = row.Status
		state.JobConclusions[row.ProviderJobID] = row.Conclusion
		if row.Status != "completed" {
			continue
		}
		state.Completed++
		switch row.Conclusion {
		case "success", "skipped":
			state.Succeeded++
			state.SuccessfulJobIDs = append(state.SuccessfulJobIDs, row.ProviderJobID)
		default:
			state.Failed++
		}
	}
	return state, nil
}

func (s *Service) githubWorkflowRunPromotionGate(ctx context.Context, ref goldenWorkflowRunRef) (githubWorkflowInvocation, bool, string, error) {
	if ref.Provider != RunnerProviderGitHub {
		return githubWorkflowInvocation{HeadSHA: ref.HeadSHA}, true, "", nil
	}
	invocation, err := s.githubWorkflowRunInvocationForRef(ctx, ref)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return githubWorkflowInvocation{}, false, "github workflow run invocation is not observed", nil
		}
		return githubWorkflowInvocation{}, false, "", err
	}
	ok, reason := invocation.dogfoodMainPromotion(ref)
	return invocation, ok, reason, nil
}

func (s *Service) githubWorkflowRunInvocationForRef(ctx context.Context, ref goldenWorkflowRunRef) (githubWorkflowInvocation, error) {
	row, err := s.storeQueries().GetWorkflowRunInvocation(ctx, store.GetWorkflowRunInvocationParams{
		ProviderInstallationID: ref.ProviderInstallationID,
		ProviderRepositoryID:   ref.ProviderRepositoryID,
		ProviderRunID:          ref.ProviderRunID,
		ProviderRunAttempt:     ref.ProviderRunAttempt,
	})
	if err != nil {
		return githubWorkflowInvocation{}, err
	}
	return githubWorkflowInvocation{
		InstallationID:         row.ProviderInstallationID,
		RepositoryID:           row.ProviderRepositoryID,
		RunID:                  row.ProviderRunID,
		RunAttempt:             row.ProviderRunAttempt,
		RepositoryFullName:     row.RepositoryFullName,
		EventName:              row.EventName,
		HeadSHA:                row.HeadSha,
		HeadBranch:             row.HeadBranch,
		HeadRepositoryFullName: row.HeadRepositoryFullName,
		BaseSHA:                row.BaseSha,
		BaseBranch:             row.BaseBranch,
		WorkflowPath:           row.WorkflowPath,
		PullRequestNumber:      row.PullRequestNumber,
		CommitCount:            row.CommitCount,
	}, nil
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
			attribute.Int("cache.count", len(manifest.Caches)),
		)
		return manifest, nil
	}
	decl := cacheDeclaration{SourceKind: "none", SourceSHA: cacheDeclarationRef(identity), Version: 1, Caches: []cacheDecl{}}
	span.SetAttributes(
		attribute.String("cache.source_kind", decl.SourceKind),
		attribute.String("cache.source_ref", decl.SourceSHA),
		attribute.Int("cache.count", 0),
	)
	return decl, nil
}

func (s *Service) fetchManifestCacheDeclaration(ctx context.Context, identity RunnerExecutionIdentity) (cacheDeclaration, bool, error) {
	row, err := s.storeQueries().GetRunnerJobCacheManifest(ctx, store.GetRunnerJobCacheManifestParams{
		Provider:      identity.Provider,
		ProviderJobID: identity.ProviderJobID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return cacheDeclaration{}, false, nil
	}
	if err != nil {
		return cacheDeclaration{}, false, err
	}
	decl, err := parseCacheManifest(row.ContentBytes, row.SourceKind, row.SourcePath, row.SourceSha, "", "", "")
	return decl, true, err
}

func parseCacheManifest(content []byte, sourceKind, sourcePath, sourceSHA, workflowIdentity, jobIdentity, stepIdentity string) (cacheDeclaration, error) {
	var manifest cacheManifestFile
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return cacheDeclaration{}, fmt.Errorf("%w: parse %s: %w", ErrCacheDeclarationInvalid, sourcePath, err)
	}
	decl := cacheDeclaration{SourceKind: sourceKind, SourcePath: sourcePath, SourceSHA: sourceSHA, WorkflowIdentity: workflowIdentity, JobIdentity: jobIdentity, StepIdentity: stepIdentity, Version: manifest.Version, Caches: manifest.Cache}
	if err := normalizeCacheDeclaration(&decl); err != nil {
		return cacheDeclaration{}, err
	}
	return decl, nil
}

func normalizeCacheDeclaration(decl *cacheDeclaration) error {
	if decl.Version != 1 {
		return fmt.Errorf("%w: version must be 1", ErrCacheDeclarationInvalid)
	}
	seen := make(map[string]struct{}, len(decl.Caches))
	for i := range decl.Caches {
		cache := &decl.Caches[i]
		cache.Name = strings.TrimSpace(cache.Name)
		if cache.Name == "" {
			return fmt.Errorf("%w: cache[%d].name is required", ErrCacheDeclarationInvalid, i)
		}
		if sanitizeMountName(cache.Name) != cache.Name {
			return fmt.Errorf("%w: cache[%d].name %q must use lowercase letters, numbers, '-' or '_'", ErrCacheDeclarationInvalid, i, cache.Name)
		}
		if cache.Name == durableWorkspaceCacheName {
			return fmt.Errorf("%w: cache[%d].name %q is reserved", ErrCacheDeclarationInvalid, i, cache.Name)
		}
		if _, ok := seen[cache.Name]; ok {
			return fmt.Errorf("%w: duplicate cache %q", ErrCacheDeclarationInvalid, cache.Name)
		}
		seen[cache.Name] = struct{}{}
		paths, err := normalizeCachePaths(cache.Paths)
		if err != nil {
			return fmt.Errorf("%w: cache %s paths: %w", ErrCacheDeclarationInvalid, cache.Name, err)
		}
		cache.Paths = paths
	}
	sort.Slice(decl.Caches, func(i, j int) bool { return decl.Caches[i].Name < decl.Caches[j].Name })
	return nil
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

func normalizedDeclarationHash(decl cacheDeclaration) (string, error) {
	copyDecl := decl
	if copyDecl.Caches == nil {
		copyDecl.Caches = []cacheDecl{}
	}
	payload, err := json.Marshal(copyDecl)
	if err != nil {
		return "", err
	}
	return stableHex(string(payload)), nil
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
		attribute.String("durable.cache_name", event.CacheName),
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
		attribute.String("zfs.pool_size_bytes", strconv.FormatUint(event.PoolSizeBytes, 10)),
		attribute.String("zfs.pool_allocated_bytes", strconv.FormatUint(event.PoolAllocatedBytes, 10)),
		attribute.String("zfs.pool_free_bytes", strconv.FormatUint(event.PoolFreeBytes, 10)),
		attribute.String("zfs.org_quota_bytes", strconv.FormatUint(event.OrgQuotaBytes, 10)),
		attribute.String("zfs.org_used_bytes", strconv.FormatUint(event.OrgUsedBytes, 10)),
		attribute.String("zfs.org_available_bytes", strconv.FormatUint(event.OrgAvailableBytes, 10)),
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
		CacheName:             event.CacheName,
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
		PoolSizeBytes:         event.PoolSizeBytes,
		PoolAllocatedBytes:    event.PoolAllocatedBytes,
		PoolFreeBytes:         event.PoolFreeBytes,
		OrgQuotaBytes:         event.OrgQuotaBytes,
		OrgUsedBytes:          event.OrgUsedBytes,
		OrgAvailableBytes:     event.OrgAvailableBytes,
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
		"cache_name", event.CacheName,
		"mount_name", event.MountName,
		"operation_id", uuidValue(event.OperationID),
		"execution_id", uuidValue(event.ExecutionID),
		"attempt_id", uuidValue(event.AttemptID),
		"error", err,
	)
}

func (s *Service) appendGoldenVMEvent(ctx context.Context, event goldenVMEvent) error {
	span := trace.SpanFromContext(ctx)
	span.AddEvent(event.Name, trace.WithAttributes(
		attribute.String("golden_vm.event_name", event.Name),
		attribute.String("golden_vm.result", event.Result),
		attribute.String("golden_vm.reason", event.Reason),
		attribute.String("golden_vm.from_state", event.FromState),
		attribute.String("golden_vm.to_state", event.ToState),
		attribute.String("lease.id", event.LeaseID),
		attribute.String("vm.exec_id", event.ExecID),
		attribute.Int64("river.job_id", event.RiverJobID),
		attribute.String("golden_vm.operation_id", uuidValue(event.OperationID).String()),
		attribute.String("golden_vm.snapshot_id", uuidValue(event.SnapshotID).String()),
		attribute.String("golden_vm.snapshot_key", event.SnapshotKey),
		attribute.String("golden_vm.activation_mode", event.ActivationMode),
		attribute.String("golden_vm.generation_set_hash", event.GenerationSetHash),
		attribute.String("golden_vm.source_generation_set_hash", event.SourceGenerationSetHash),
		attribute.String("golden_vm.vmstate_artifact_ref", event.VMStateArtifactRef),
		attribute.String("golden_vm.memory_artifact_ref", event.MemoryArtifactRef),
		attribute.String("golden_vm.root_snapshot_ref", event.RootSnapshotRef),
	))
	if s.CH == nil {
		return nil
	}
	spanContext := span.SpanContext()
	row := goldenVMEventRow{
		ObservedAt:              time.Now().UTC(),
		OrgID:                   event.Identity.OrgID,
		RepositoryID:            uint64FromNonNegativeInt64(event.Identity.ProviderRepositoryID),
		Provider:                event.Identity.Provider,
		ProviderRepositoryID:    uint64FromNonNegativeInt64(event.Identity.ProviderRepositoryID),
		ProviderRunID:           uint64FromNonNegativeInt64(event.Identity.ProviderRunID),
		ProviderRunAttempt:      uint64FromNonNegativeInt64(event.Identity.ProviderRunAttempt),
		ProviderJobID:           uint64FromNonNegativeInt64(event.Identity.ProviderJobID),
		ExecutionID:             uuidValue(event.ExecutionID),
		AttemptID:               uuidValue(event.AttemptID),
		OperationID:             uuidValue(event.OperationID),
		GoldenVMSnapshotID:      uuidValue(event.SnapshotID),
		JobShapeID:              uuidValue(event.JobShapeID),
		EventName:               event.Name,
		Result:                  event.Result,
		Reason:                  event.Reason,
		FromState:               event.FromState,
		ToState:                 event.ToState,
		LeaseID:                 event.LeaseID,
		ExecID:                  event.ExecID,
		RiverJobID:              event.RiverJobID,
		GenerationSetHash:       event.GenerationSetHash,
		SourceGenerationSetHash: event.SourceGenerationSetHash,
		SnapshotKey:             event.SnapshotKey,
		ActivationMode:          event.ActivationMode,
		VMStateArtifactRef:      event.VMStateArtifactRef,
		MemoryArtifactRef:       event.MemoryArtifactRef,
		RootSnapshotRef:         event.RootSnapshotRef,
		RootSnapshotGUID:        event.RootSnapshotGUID,
		StateBytes:              event.StateBytes,
		MemoryBytes:             event.MemoryBytes,
		TraceID:                 spanContext.TraceID().String(),
		SpanID:                  spanContext.SpanID().String(),
	}
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO "+s.CHDatabase+".golden_vm_events")
	if err != nil {
		s.recordGoldenVMEventInsertFailure(ctx, event, err)
		span.RecordError(err)
		return err
	}
	if err := batch.AppendStruct(&row); err != nil {
		s.recordGoldenVMEventInsertFailure(ctx, event, err)
		span.RecordError(err)
		return err
	}
	if err := batch.Send(); err != nil {
		s.recordGoldenVMEventInsertFailure(ctx, event, err)
		span.RecordError(err)
		return err
	}
	return nil
}

func (s *Service) recordGoldenVMEventInsertFailure(ctx context.Context, event goldenVMEvent, err error) {
	if s.Logger == nil || err == nil {
		return
	}
	s.Logger.WarnContext(ctx, "golden VM event insert failed",
		"event_name", event.Name,
		"result", event.Result,
		"operation_id", uuidValue(event.OperationID),
		"snapshot_id", uuidValue(event.SnapshotID),
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

func goldenVMSourceGenerationSetHash(staticMounts []vmorchestrator.FilesystemMount, ops []durableCacheOperation) string {
	parts := []string{"golden-vm-generation-set-v2"}
	parts = appendGoldenVMStaticMountHashParts(parts, staticMounts)
	for idx, op := range ops {
		bindPaths := append([]string(nil), op.BindPaths...)
		sort.Strings(bindPaths)
		parts = append(parts,
			strconv.Itoa(idx),
			op.CacheName,
			op.DurableScopeID.String(),
			uuidPtrString(op.SourceGenerationID),
			op.SourceSnapshotRef,
			op.SourceSnapshotGUID,
			op.MountName,
			op.MountPath,
			strings.Join(bindPaths, "\n"),
			"ext4",
			goldenVMAccessMode(false),
			strconv.FormatBool(op.Required),
		)
	}
	return stableHex(parts...)
}

func goldenVMCandidateGenerationSetHash(staticMounts []vmorchestrator.FilesystemMount, gens []goldenVMSnapshotGeneration) string {
	parts := []string{"golden-vm-generation-set-v2"}
	parts = appendGoldenVMStaticMountHashParts(parts, staticMounts)
	for _, gen := range gens {
		bindPaths := append([]string(nil), gen.BindPaths...)
		sort.Strings(bindPaths)
		parts = append(parts,
			strconv.Itoa(gen.SortOrder),
			gen.CacheName,
			gen.DurableScopeID.String(),
			gen.DurableGenerationID.String(),
			gen.ZFSSnapshotRef,
			gen.ZFSSnapshotGUID,
			gen.DriveID,
			gen.MountPath,
			strings.Join(bindPaths, "\n"),
			gen.FSType,
			goldenVMAccessMode(gen.ReadOnly),
			strconv.FormatBool(gen.Required),
		)
	}
	return stableHex(parts...)
}

func nonDurableFilesystemMounts(mounts []vmorchestrator.FilesystemMount) []vmorchestrator.FilesystemMount {
	out := make([]vmorchestrator.FilesystemMount, 0, len(mounts))
	for _, mount := range mounts {
		if strings.TrimSpace(mount.OperationID) != "" {
			continue
		}
		out = append(out, mount)
	}
	return out
}

func appendGoldenVMStaticMountHashParts(parts []string, mounts []vmorchestrator.FilesystemMount) []string {
	for idx, mount := range mounts {
		bindPaths := append([]string(nil), mount.BindPaths...)
		sort.Strings(bindPaths)
		parts = append(parts,
			"static-mount",
			strconv.Itoa(idx),
			mount.Name,
			mount.SourceRef,
			mount.MountPath,
			strings.Join(bindPaths, "\n"),
			firstNonEmpty(mount.FSType, "ext4"),
			goldenVMAccessMode(mount.ReadOnly),
			strconv.FormatBool(mount.Required),
		)
	}
	return parts
}

func goldenVMAccessMode(readOnly bool) string {
	if readOnly {
		return "ro"
	}
	return "rw"
}

func goldenVMDriveManifestHash(checkpoint vmorchestrator.GoldenVMCheckpointRecord) string {
	parts := []string{"golden-vm-drive-manifest-v1", checkpoint.RootSnapshotRef, checkpoint.RootSnapshotGUID}
	mounts := append([]vmorchestrator.GoldenVMMountCheckpoint(nil), checkpoint.MountSnapshots...)
	sort.Slice(mounts, func(i, j int) bool {
		return mounts[i].MountName < mounts[j].MountName
	})
	for _, mount := range mounts {
		parts = append(parts, mount.MountName, mount.SnapshotRef, mount.SnapshotGUID)
	}
	return stableHex(parts...)
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
