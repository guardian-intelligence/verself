package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/verself/sandbox-rental-service/internal/store"
	vmorchestrator "github.com/verself/vm-orchestrator"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	checkpointSlotCount       = 5
	checkpointDefaultBytes    = 5 * 1024 * 1024 * 1024
	checkpointProductKind     = "ci_checkpoint"
	checkpointComponentKind   = "checkpoint"
	checkpointRetentionPolicy = "short_7d"
	checkpointTrustClass      = "branch"
	checkpointScopeKind       = "branch"
	checkpointFilesystemType  = "ext4"
	checkpointPath            = "/internal/sandbox/v1/checkpoints"
)

var (
	ErrCheckpointInvalid      = errors.New("checkpoint request is invalid")
	ErrCheckpointUnauthorized = errors.New("checkpoint request is not authorized")
	ErrCheckpointUnavailable  = errors.New("checkpoint service is unavailable")
	checkpointKeyPattern      = regexp.MustCompile(`\A[[:print:]]{1,512}\z`)
)

type CheckpointMountRequest struct {
	Key       string
	Path      string
	SizeBytes uint64
}

type CheckpointMount struct {
	MountID          uuid.UUID
	MountName        string
	DevicePath       string
	MountPath        string
	FSType           string
	CacheHit         bool
	SourceGeneration int32
	SourceRef        string
}

type checkpointEvent struct {
	EventID                uuid.UUID
	EventName              string
	ObservedAt             time.Time
	ExecutionID            uuid.UUID
	AttemptID              uuid.UUID
	MountID                uuid.UUID
	VolumeID               uuid.UUID
	SourceGenerationID     uuid.UUID
	CommittedGenerationID  uuid.UUID
	OrgID                  uint64
	Provider               string
	ProviderInstallationID int64
	ProviderRepositoryID   int64
	ProviderRunID          int64
	ProviderJobID          int64
	RepositoryFullName     string
	RunnerClass            string
	ScopeKind              string
	ScopeRef               string
	TrustClass             string
	KeyHash                string
	MountPathHash          string
	FromMountState         string
	ToMountState           string
	FromSaveState          string
	ToSaveState            string
	Operation              string
	Result                 string
	ReasonCode             string
	UsedBytes              uint64
	WrittenBytes           uint64
	DurationMs             uint64
}

type checkpointOperationRow struct {
	OperationID            uuid.UUID `ch:"operation_id"`
	ExecutionID            uuid.UUID `ch:"execution_id"`
	AttemptID              uuid.UUID `ch:"attempt_id"`
	MountID                uuid.UUID `ch:"mount_id"`
	VolumeID               uuid.UUID `ch:"volume_id"`
	SourceGenerationID     string    `ch:"source_generation_id"`
	CommittedGenerationID  string    `ch:"committed_generation_id"`
	OrgID                  uint64    `ch:"org_id"`
	Provider               string    `ch:"provider"`
	ProviderInstallationID uint64    `ch:"provider_installation_id"`
	ProviderRepositoryID   uint64    `ch:"provider_repository_id"`
	ProviderRunID          uint64    `ch:"provider_run_id"`
	ProviderJobID          uint64    `ch:"provider_job_id"`
	RepositoryFullName     string    `ch:"repository_full_name"`
	RunnerClass            string    `ch:"runner_class"`
	ScopeKind              string    `ch:"scope_kind"`
	ScopeRef               string    `ch:"scope_ref"`
	KeyHash                string    `ch:"key_hash"`
	MountPathHash          string    `ch:"mount_path_hash"`
	EventName              string    `ch:"event_name"`
	Operation              string    `ch:"operation"`
	Result                 string    `ch:"result"`
	MountState             string    `ch:"mount_state"`
	SaveState              string    `ch:"save_state"`
	TrustClass             string    `ch:"trust_class"`
	UsedBytes              uint64    `ch:"used_bytes"`
	WrittenBytes           uint64    `ch:"written_bytes"`
	DurationMs             uint64    `ch:"duration_ms"`
	ErrorClass             string    `ch:"error_class"`
	ObservedAt             time.Time `ch:"observed_at"`
	TraceID                string    `ch:"trace_id"`
	SpanID                 string    `ch:"span_id"`
}

type checkpointPreparedCommit struct {
	GenerationID    uuid.UUID
	Generation      int32
	TargetSourceRef string
	MountState      string
	SaveState       string
}

func (s *Service) MountCheckpoint(ctx context.Context, identity RunnerExecutionIdentity, req CheckpointMountRequest) (CheckpointMount, error) {
	if s == nil {
		return CheckpointMount{}, ErrCheckpointUnavailable
	}
	ctx, span := tracer.Start(ctx, "runner.checkpoint.mount")
	defer span.End()
	mount, err := s.mountCheckpoint(ctx, identity, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return mount, err
}

func (s *Service) MarkCheckpointMounted(ctx context.Context, identity RunnerExecutionIdentity, mountID uuid.UUID) error {
	if s == nil {
		return ErrCheckpointUnavailable
	}
	ctx, span := tracer.Start(ctx, "runner.checkpoint.mounted")
	defer span.End()
	err := s.markCheckpointMounted(ctx, identity, mountID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

func (s *Service) RequestCheckpointSave(ctx context.Context, identity RunnerExecutionIdentity, mountID uuid.UUID) error {
	if s == nil {
		return ErrCheckpointUnavailable
	}
	ctx, span := tracer.Start(ctx, "runner.checkpoint.save")
	defer span.End()
	err := s.requestCheckpointSave(ctx, identity, mountID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return err
}

func (s *Service) mountCheckpoint(ctx context.Context, identity RunnerExecutionIdentity, req CheckpointMountRequest) (CheckpointMount, error) {
	if s.Orchestrator == nil {
		return CheckpointMount{}, ErrCheckpointUnavailable
	}
	key, err := normalizeCheckpointKey(req.Key)
	if err != nil {
		return CheckpointMount{}, err
	}
	mountPath, err := normalizeCheckpointMountPath(req.Path)
	if err != nil {
		return CheckpointMount{}, err
	}
	item, err := s.loadWorkItem(ctx, identity.ExecutionID, identity.AttemptID)
	if err != nil {
		return CheckpointMount{}, err
	}
	if item.LeaseID == "" {
		return CheckpointMount{}, fmt.Errorf("%w: execution has no active lease", ErrCheckpointUnavailable)
	}
	scopeRef := checkpointScopeRef(identity)
	keyHash := hexSHA256(key)
	mountPathHash := hexSHA256(mountPath)
	now := time.Now().UTC()
	q := s.storeQueries()
	volume, err := q.UpsertVolume(ctx, store.UpsertVolumeParams{
		VolumeID:               uuid.New(),
		OrgID:                  dbOrgID(identity.OrgID),
		Provider:               identity.Provider,
		ProviderInstallationID: identity.ProviderInstallationID,
		ProviderRepositoryID:   identity.ProviderRepositoryID,
		RepositoryFullName:     identity.RepositoryFullName,
		ScopeKind:              checkpointScopeKind,
		ScopeRef:               scopeRef,
		DefaultScopeRef:        "main",
		ProductKind:            checkpointProductKind,
		ComponentKind:          checkpointComponentKind,
		KeyHash:                keyHash,
		Key:                    key,
		DisplayName:            key,
		RetentionPolicy:        checkpointRetentionPolicy,
		Now:                    pgTime(now),
	})
	if err != nil {
		return CheckpointMount{}, fmt.Errorf("upsert checkpoint volume: %w", err)
	}
	current, cacheHit, err := s.currentCheckpointGeneration(ctx, volume.VolumeID, identity.OrgID)
	if err != nil {
		return CheckpointMount{}, err
	}
	mountID := uuid.New()
	mountName := checkpointMountName(mountID)
	sourceGeneration := pgtype.Int4{}
	var sourceGenerationID *uuid.UUID
	sourceRef := ""
	if cacheHit {
		sourceGeneration.Valid = true
		sourceGeneration.Int32 = current.Generation
		sourceGenerationID = &current.VolumeGenerationID
		sourceRef = current.ZfsSourceRef
	}
	record, err := q.InsertExecutionVolumeMount(ctx, store.InsertExecutionVolumeMountParams{
		MountID:            mountID,
		ExecutionID:        identity.ExecutionID,
		AttemptID:          identity.AttemptID,
		AllocationID:       &identity.AllocationID,
		VolumeID:           volume.VolumeID,
		MountName:          mountName,
		MountPath:          mountPath,
		MountPathHash:      mountPathHash,
		KeyHash:            keyHash,
		SourceGenerationID: sourceGenerationID,
		SourceGeneration:   sourceGeneration,
		SourceRef:          sourceRef,
		Now:                pgTime(now),
	})
	if err != nil {
		return CheckpointMount{}, fmt.Errorf("insert checkpoint execution mount: %w", err)
	}
	if record.MountID != mountID {
		if record.GuestDevicePath == "" {
			return CheckpointMount{}, fmt.Errorf("%w: checkpoint mount is already being prepared", ErrCheckpointInvalid)
		}
		return checkpointMountFromRecord(record, cacheHit), nil
	}
	baseEvent := checkpointEventForIdentity(identity, record, checkpointScopeKind, scopeRef)
	if err := s.appendCheckpointEvent(ctx, baseEvent.withTransition("checkpoint.mount.requested", "", "requested", "", "none", "mount_request", "accepted", "")); err != nil {
		return CheckpointMount{}, err
	}
	resolved, err := q.UpdateExecutionVolumeMountResolving(ctx, store.UpdateExecutionVolumeMountResolvingParams{Now: pgTime(time.Now().UTC()), MountID: record.MountID})
	if err != nil {
		return CheckpointMount{}, err
	}
	if err := s.appendCheckpointEvent(ctx, baseEvent.withTransition("checkpoint.volume.resolving", "requested", resolved.MountState, "none", resolved.SaveState, "volume_resolve", "started", "")); err != nil {
		return CheckpointMount{}, err
	}
	volumeEvent := "checkpoint.volume.found"
	volumeResult := "found"
	if volume.Created {
		volumeEvent = "checkpoint.volume.created"
		volumeResult = "created"
	}
	if err := s.appendCheckpointEvent(ctx, baseEvent.withTransition(volumeEvent, resolved.MountState, resolved.MountState, resolved.SaveState, resolved.SaveState, "volume_resolve", volumeResult, "")); err != nil {
		return CheckpointMount{}, err
	}
	sourceEvent := "checkpoint.source.missed"
	sourceResult := "miss"
	if cacheHit {
		sourceEvent = "checkpoint.source.hit"
		sourceResult = "hit"
		baseEvent.SourceGenerationID = current.VolumeGenerationID
	}
	if err := s.appendCheckpointEvent(ctx, baseEvent.withTransition(sourceEvent, resolved.MountState, resolved.MountState, resolved.SaveState, resolved.SaveState, "source_select", sourceResult, "")); err != nil {
		return CheckpointMount{}, err
	}
	preparing, err := q.UpdateExecutionVolumeMountPreparing(ctx, store.UpdateExecutionVolumeMountPreparingParams{Now: pgTime(time.Now().UTC()), MountID: record.MountID})
	if err != nil {
		return CheckpointMount{}, err
	}
	if err := s.appendCheckpointEvent(ctx, baseEvent.withTransition("checkpoint.host.prepare_started", resolved.MountState, preparing.MountState, resolved.SaveState, preparing.SaveState, "host_prepare", "started", "")); err != nil {
		return CheckpointMount{}, err
	}
	attachStarted := time.Now().UTC()
	attached, err := s.Orchestrator.AttachFilesystemMount(ctx, item.LeaseID, record.MountID.String()+":attach", vmorchestrator.FilesystemMount{
		Name:      record.MountName,
		SourceRef: sourceRef,
		MountPath: mountPath,
		FSType:    checkpointFilesystemType,
		ReadOnly:  false,
	}, firstNonZero(req.SizeBytes, checkpointDefaultBytes))
	if err != nil {
		_ = q.MarkExecutionVolumeMountFailed(context.Background(), store.MarkExecutionVolumeMountFailedParams{
			FailureReason: err.Error(),
			Now:           pgTime(time.Now().UTC()),
			MountID:       record.MountID,
		})
		failEvent := baseEvent.withTransition("checkpoint.host.prepare_failed", preparing.MountState, "mount_failed", preparing.SaveState, preparing.SaveState, "host_prepare", "failed", classifyCheckpointError(err))
		failEvent.DurationMs = uint64(time.Since(attachStarted).Milliseconds())
		_ = s.appendCheckpointEvent(context.Background(), failEvent)
		return CheckpointMount{}, fmt.Errorf("attach checkpoint filesystem mount: %w", err)
	}
	attachedState, err := q.UpdateExecutionVolumeMountAttached(ctx, store.UpdateExecutionVolumeMountAttachedParams{
		GuestDevicePath: attached.GuestDevicePath,
		Now:             pgTime(time.Now().UTC()),
		MountID:         record.MountID,
	})
	if err != nil {
		return CheckpointMount{}, err
	}
	successEvent := baseEvent.withTransition("checkpoint.host.prepare_succeeded", preparing.MountState, attachedState.MountState, preparing.SaveState, attachedState.SaveState, "host_prepare", "succeeded", "")
	successEvent.DurationMs = uint64(time.Since(attachStarted).Milliseconds())
	if err := s.appendCheckpointEvent(ctx, successEvent); err != nil {
		return CheckpointMount{}, err
	}
	record.GuestDevicePath = attached.GuestDevicePath
	record.MountState = attachedState.MountState
	record.SaveState = attachedState.SaveState
	return checkpointMountFromRecord(record, cacheHit), nil
}

func (s *Service) currentCheckpointGeneration(ctx context.Context, volumeID uuid.UUID, orgID uint64) (store.GetCurrentVolumeGenerationRow, bool, error) {
	current, err := s.storeQueries().GetCurrentVolumeGeneration(ctx, store.GetCurrentVolumeGenerationParams{
		OrgID:      dbOrgID(orgID),
		VolumeID:   volumeID,
		TrustClass: checkpointTrustClass,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.GetCurrentVolumeGenerationRow{}, false, nil
	}
	if err != nil {
		return store.GetCurrentVolumeGenerationRow{}, false, fmt.Errorf("resolve checkpoint current generation: %w", err)
	}
	return current, true, nil
}

func (s *Service) markCheckpointMounted(ctx context.Context, identity RunnerExecutionIdentity, mountID uuid.UUID) error {
	if mountID == uuid.Nil {
		return fmt.Errorf("%w: mount_id is required", ErrCheckpointInvalid)
	}
	record, err := s.storeQueries().MarkExecutionVolumeMountReady(ctx, store.MarkExecutionVolumeMountReadyParams{
		Now:         pgTime(time.Now().UTC()),
		MountID:     mountID,
		ExecutionID: identity.ExecutionID,
		AttemptID:   identity.AttemptID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: checkpoint mount is not attachable", ErrCheckpointInvalid)
	}
	if err != nil {
		return err
	}
	event := checkpointEventForIdentity(identity, record, checkpointScopeKind, checkpointScopeRef(identity))
	return s.appendCheckpointEvent(ctx, event.withTransition("checkpoint.mount.ready", "attaching", record.MountState, "none", record.SaveState, "guest_mount", "mounted", ""))
}

func (s *Service) requestCheckpointSave(ctx context.Context, identity RunnerExecutionIdentity, mountID uuid.UUID) error {
	if mountID == uuid.Nil {
		return fmt.Errorf("%w: mount_id is required", ErrCheckpointInvalid)
	}
	record, err := s.storeQueries().RequestExecutionVolumeMountSave(ctx, store.RequestExecutionVolumeMountSaveParams{
		Now:         pgTime(time.Now().UTC()),
		MountID:     mountID,
		ExecutionID: identity.ExecutionID,
		AttemptID:   identity.AttemptID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: checkpoint mount is not mounted", ErrCheckpointInvalid)
	}
	if err != nil {
		return err
	}
	event := checkpointEventForIdentity(identity, record, checkpointScopeKind, checkpointScopeRef(identity))
	return s.appendCheckpointEvent(ctx, event.withTransition("checkpoint.save.requested", record.MountState, record.MountState, "none", record.SaveState, "save_request", "accepted", ""))
}

func (s *Service) FinalizeCheckpoints(ctx context.Context, item executionWorkItem, leaseID string) error {
	if s.Orchestrator == nil || leaseID == "" {
		return nil
	}
	ctx, span := tracer.Start(ctx, "sandbox-rental.checkpoint.finalize", trace.WithAttributes(
		attribute.String("execution.id", item.ExecutionID.String()),
		attribute.String("attempt.id", item.AttemptID.String()),
		attribute.String("lease.id", leaseID),
	))
	defer span.End()
	rows, err := s.storeQueries().ListSaveRequestedExecutionVolumeMounts(ctx, store.ListSaveRequestedExecutionVolumeMountsParams{AttemptID: item.AttemptID})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	for _, row := range rows {
		if err := s.finalizeCheckpointMount(ctx, item, leaseID, row); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
	}
	return nil
}

func (s *Service) PruneCheckpointGenerations(ctx context.Context, item executionWorkItem) error {
	if s.Orchestrator == nil {
		return nil
	}
	rows, err := s.storeQueries().ListPrunableVolumeGenerationsForAttempt(ctx, store.ListPrunableVolumeGenerationsForAttemptParams{AttemptID: item.AttemptID})
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.ZfsSourceRef == "" {
			continue
		}
		event := checkpointEventForPrunedGeneration(item, row)
		started := time.Now().UTC()
		if err := s.appendCheckpointEvent(ctx, event.withTransition("checkpoint.generation.prune_started", row.MountState, row.MountState, row.SaveState, row.SaveState, "retention_prune", "started", "")); err != nil {
			return err
		}
		key := item.AttemptID.String() + ":prune:" + row.VolumeGenerationID.String()
		if _, err := s.Orchestrator.DeleteFilesystemSource(ctx, key, row.ZfsSourceRef); err != nil {
			failEvent := event.withTransition("checkpoint.generation.prune_failed", row.MountState, row.MountState, row.SaveState, row.SaveState, "retention_prune", "failed", classifyCheckpointError(err))
			failEvent.DurationMs = uint64(time.Since(started).Milliseconds())
			_ = s.appendCheckpointEvent(context.Background(), failEvent)
			return fmt.Errorf("prune checkpoint generation %s (%s): %w", row.VolumeGenerationID, row.ZfsSourceRef, err)
		}
		if err := s.storeQueries().MarkVolumeGenerationPruned(ctx, store.MarkVolumeGenerationPrunedParams{
			Now:                pgTime(time.Now().UTC()),
			VolumeGenerationID: row.VolumeGenerationID,
		}); err != nil {
			failEvent := event.withTransition("checkpoint.generation.prune_failed", row.MountState, row.MountState, row.SaveState, row.SaveState, "retention_prune", "failed", classifyCheckpointError(err))
			failEvent.DurationMs = uint64(time.Since(started).Milliseconds())
			_ = s.appendCheckpointEvent(context.Background(), failEvent)
			return err
		}
		prunedEvent := event.withTransition("checkpoint.generation.pruned", row.MountState, row.MountState, row.SaveState, row.SaveState, "retention_prune", "succeeded", "")
		prunedEvent.DurationMs = uint64(time.Since(started).Milliseconds())
		if err := s.appendCheckpointEvent(ctx, prunedEvent); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) finalizeCheckpointMount(ctx context.Context, item executionWorkItem, leaseID string, row store.ListSaveRequestedExecutionVolumeMountsRow) error {
	event := checkpointEventForFinalizer(item, row)
	q := s.storeQueries()
	finalizing, err := q.MarkExecutionVolumeMountFinalizing(ctx, store.MarkExecutionVolumeMountFinalizingParams{
		Now:     pgTime(time.Now().UTC()),
		MountID: row.MountID,
	})
	if err != nil {
		return err
	}
	if err := s.appendCheckpointEvent(ctx, event.withTransition("checkpoint.finalization.started", row.MountState, finalizing.MountState, row.SaveState, finalizing.SaveState, "finalize", "started", "")); err != nil {
		return err
	}
	prepared, err := s.prepareCheckpointCommit(ctx, row)
	if err != nil {
		return err
	}
	if err := s.appendCheckpointEvent(ctx, event.withTransition("checkpoint.commit.started", finalizing.MountState, prepared.MountState, finalizing.SaveState, prepared.SaveState, "commit", "started", "")); err != nil {
		return err
	}
	commitStarted := time.Now().UTC()
	commit, err := s.Orchestrator.CommitFilesystemMount(ctx, leaseID, row.MountID.String()+":commit", row.MountName, prepared.TargetSourceRef)
	if err != nil {
		_ = q.MarkVolumeGenerationFailed(context.Background(), store.MarkVolumeGenerationFailedParams{
			Now:                pgTime(time.Now().UTC()),
			VolumeGenerationID: prepared.GenerationID,
		})
		failed, markErr := q.MarkExecutionVolumeMountCommitFailed(context.Background(), store.MarkExecutionVolumeMountCommitFailedParams{
			FailureReason: err.Error(),
			Now:           pgTime(time.Now().UTC()),
			MountID:       row.MountID,
		})
		if markErr == nil {
			failEvent := event.withTransition("checkpoint.commit.failed", prepared.MountState, failed.MountState, prepared.SaveState, failed.SaveState, "commit", "failed", classifyCheckpointError(err))
			failEvent.DurationMs = uint64(time.Since(commitStarted).Milliseconds())
			_ = s.appendCheckpointEvent(context.Background(), failEvent)
		}
		return fmt.Errorf("commit checkpoint mount %s: %w", row.MountName, err)
	}
	promoted, saveState, err := s.recordCheckpointGeneration(ctx, row, prepared.GenerationID, commit.Snapshot)
	if err != nil {
		return err
	}
	event.CommittedGenerationID = prepared.GenerationID
	commitEvent := event.withTransition("checkpoint.commit.succeeded", prepared.MountState, prepared.MountState, prepared.SaveState, "committed", "commit", "succeeded", "")
	commitEvent.DurationMs = uint64(time.Since(commitStarted).Milliseconds())
	if err := s.appendCheckpointEvent(ctx, commitEvent); err != nil {
		return err
	}
	promotion, err := q.MarkExecutionVolumeMountPromotion(ctx, store.MarkExecutionVolumeMountPromotionParams{
		SaveState: saveState,
		Now:       pgTime(time.Now().UTC()),
		MountID:   row.MountID,
	})
	if err != nil {
		return err
	}
	promotionEvent := "checkpoint.promotion.conflicted"
	promotionResult := "conflicted"
	if promoted {
		promotionEvent = "checkpoint.promotion.succeeded"
		promotionResult = "succeeded"
	}
	if err := s.appendCheckpointEvent(ctx, event.withTransition(promotionEvent, prepared.MountState, promotion.MountState, "committed", promotion.SaveState, "promotion", promotionResult, "")); err != nil {
		return err
	}
	return s.appendCheckpointEvent(ctx, event.withTransition("checkpoint.finalization.completed", finalizing.MountState, promotion.MountState, saveState, promotion.SaveState, "finalize", "succeeded", ""))
}

func (s *Service) prepareCheckpointCommit(ctx context.Context, row store.ListSaveRequestedExecutionVolumeMountsRow) (checkpointPreparedCommit, error) {
	tx, err := s.PGX.Begin(ctx)
	if err != nil {
		return checkpointPreparedCommit{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := store.New(tx)
	if err := qtx.LockVolumeForGeneration(ctx, store.LockVolumeForGenerationParams{VolumeID: row.VolumeID}); err != nil {
		return checkpointPreparedCommit{}, err
	}
	generation, err := qtx.NextVolumeGeneration(ctx, store.NextVolumeGenerationParams{VolumeID: row.VolumeID})
	if err != nil {
		return checkpointPreparedCommit{}, err
	}
	targetSourceRef := checkpointTargetSourceRef(row.VolumeID, generation)
	generationID := uuid.New()
	if _, err := qtx.InsertPendingVolumeGeneration(ctx, store.InsertPendingVolumeGenerationParams{
		VolumeGenerationID:   generationID,
		VolumeID:             row.VolumeID,
		OrgID:                row.OrgID,
		Generation:           generation,
		ParentGenerationID:   row.SourceGenerationID,
		TrustClass:           checkpointTrustClass,
		ZfsSourceRef:         targetSourceRef,
		CreatedByExecutionID: &row.ExecutionID,
		CreatedByAttemptID:   &row.AttemptID,
		Now:                  pgTime(time.Now().UTC()),
	}); err != nil {
		return checkpointPreparedCommit{}, err
	}
	committing, err := qtx.MarkExecutionVolumeMountCommitting(ctx, store.MarkExecutionVolumeMountCommittingParams{
		TargetSourceRef: targetSourceRef,
		Now:             pgTime(time.Now().UTC()),
		MountID:         row.MountID,
	})
	if err != nil {
		return checkpointPreparedCommit{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return checkpointPreparedCommit{}, err
	}
	return checkpointPreparedCommit{
		GenerationID:    generationID,
		Generation:      generation,
		TargetSourceRef: targetSourceRef,
		MountState:      committing.MountState,
		SaveState:       committing.SaveState,
	}, nil
}

func (s *Service) recordCheckpointGeneration(ctx context.Context, row store.ListSaveRequestedExecutionVolumeMountsRow, generationID uuid.UUID, snapshot string) (bool, string, error) {
	tx, err := s.PGX.Begin(ctx)
	if err != nil {
		return false, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := store.New(tx)
	if err := qtx.LockVolumeForGeneration(ctx, store.LockVolumeForGenerationParams{VolumeID: row.VolumeID}); err != nil {
		return false, "", err
	}
	committed, err := qtx.MarkVolumeGenerationCommitted(ctx, store.MarkVolumeGenerationCommittedParams{
		ZfsSnapshotRef:     snapshot,
		UsedBytes:          0,
		WrittenBytes:       0,
		Now:                pgTime(time.Now().UTC()),
		VolumeGenerationID: generationID,
	})
	if err != nil {
		return false, "", err
	}
	if _, err := qtx.MarkExecutionVolumeMountCommitted(ctx, store.MarkExecutionVolumeMountCommittedParams{
		CommittedGenerationID: &generationID,
		Now:                   pgTime(time.Now().UTC()),
		MountID:               row.MountID,
	}); err != nil {
		return false, "", err
	}
	var affected int64
	if row.SourceGeneration.Valid {
		affected, err = qtx.UpdateVolumeCurrentGenerationCAS(ctx, store.UpdateVolumeCurrentGenerationCASParams{
			CurrentGeneration:  committed.Generation,
			VolumeGenerationID: generationID,
			Now:                pgTime(time.Now().UTC()),
			OrgID:              row.OrgID,
			VolumeID:           row.VolumeID,
			TrustClass:         checkpointTrustClass,
			ExpectedGeneration: row.SourceGeneration.Int32,
		})
	} else {
		affected, err = qtx.InsertVolumeCurrentGeneration(ctx, store.InsertVolumeCurrentGenerationParams{
			OrgID:              row.OrgID,
			VolumeID:           row.VolumeID,
			TrustClass:         checkpointTrustClass,
			CurrentGeneration:  committed.Generation,
			VolumeGenerationID: generationID,
			Now:                pgTime(time.Now().UTC()),
		})
	}
	if err != nil {
		return false, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, "", err
	}
	if affected == 1 {
		return true, "promotion_succeeded", nil
	}
	return false, "promotion_conflict", nil
}

func (s *Service) appendCheckpointEvent(ctx context.Context, event checkpointEvent) error {
	if event.EventID == uuid.Nil {
		event.EventID = uuid.New()
	}
	if event.ObservedAt.IsZero() {
		event.ObservedAt = time.Now().UTC()
	}
	sc := trace.SpanContextFromContext(ctx)
	traceID := ""
	spanID := ""
	if sc.HasTraceID() {
		traceID = sc.TraceID().String()
	}
	if sc.HasSpanID() {
		spanID = sc.SpanID().String()
	}
	if err := s.storeQueries().InsertCheckpointLifecycleEvent(ctx, store.InsertCheckpointLifecycleEventParams{
		EventID:                event.EventID,
		EventName:              event.EventName,
		ObservedAt:             pgTime(event.ObservedAt),
		ExecutionID:            event.ExecutionID,
		AttemptID:              event.AttemptID,
		MountID:                uuidPtrFromZero(event.MountID),
		VolumeID:               uuidPtrFromZero(event.VolumeID),
		SourceGenerationID:     uuidPtrFromZero(event.SourceGenerationID),
		CommittedGenerationID:  uuidPtrFromZero(event.CommittedGenerationID),
		OrgID:                  dbOrgID(event.OrgID),
		Provider:               event.Provider,
		ProviderInstallationID: event.ProviderInstallationID,
		ProviderRepositoryID:   event.ProviderRepositoryID,
		ProviderRunID:          event.ProviderRunID,
		ProviderJobID:          event.ProviderJobID,
		RepositoryFullName:     event.RepositoryFullName,
		RunnerClass:            event.RunnerClass,
		ScopeKind:              event.ScopeKind,
		ScopeRef:               event.ScopeRef,
		TrustClass:             event.TrustClass,
		KeyHash:                event.KeyHash,
		MountPathHash:          event.MountPathHash,
		FromMountState:         event.FromMountState,
		ToMountState:           event.ToMountState,
		FromSaveState:          event.FromSaveState,
		ToSaveState:            event.ToSaveState,
		Operation:              event.Operation,
		Result:                 event.Result,
		ReasonCode:             event.ReasonCode,
		UsedBytes:              mustInt64FromUint64(event.UsedBytes, "checkpoint used bytes"),
		WrittenBytes:           mustInt64FromUint64(event.WrittenBytes, "checkpoint written bytes"),
		DurationMs:             mustInt64FromUint64(event.DurationMs, "checkpoint duration ms"),
		TraceID:                traceID,
		SpanID:                 spanID,
	}); err != nil {
		return err
	}
	if s.CH == nil {
		return nil
	}
	batch, err := s.CH.PrepareBatch(ctx, "INSERT INTO "+s.CHDatabase+".checkpoint_operations")
	if err != nil {
		return err
	}
	row := checkpointOperationRow{
		OperationID:            event.EventID,
		ExecutionID:            event.ExecutionID,
		AttemptID:              event.AttemptID,
		MountID:                event.MountID,
		VolumeID:               event.VolumeID,
		SourceGenerationID:     uuidStringFromZero(event.SourceGenerationID),
		CommittedGenerationID:  uuidStringFromZero(event.CommittedGenerationID),
		OrgID:                  event.OrgID,
		Provider:               event.Provider,
		ProviderInstallationID: uint64FromSigned(event.ProviderInstallationID),
		ProviderRepositoryID:   uint64FromSigned(event.ProviderRepositoryID),
		ProviderRunID:          uint64FromSigned(event.ProviderRunID),
		ProviderJobID:          uint64FromSigned(event.ProviderJobID),
		RepositoryFullName:     event.RepositoryFullName,
		RunnerClass:            event.RunnerClass,
		ScopeKind:              event.ScopeKind,
		ScopeRef:               event.ScopeRef,
		KeyHash:                event.KeyHash,
		MountPathHash:          event.MountPathHash,
		EventName:              event.EventName,
		Operation:              event.Operation,
		Result:                 event.Result,
		MountState:             event.ToMountState,
		SaveState:              event.ToSaveState,
		TrustClass:             event.TrustClass,
		UsedBytes:              event.UsedBytes,
		WrittenBytes:           event.WrittenBytes,
		DurationMs:             event.DurationMs,
		ErrorClass:             event.ReasonCode,
		ObservedAt:             event.ObservedAt,
		TraceID:                traceID,
		SpanID:                 spanID,
	}
	if err := batch.AppendStruct(&row); err != nil {
		return err
	}
	return batch.Send()
}

func checkpointEventForIdentity(identity RunnerExecutionIdentity, record store.ExecutionVolumeMount, scopeKind, scopeRef string) checkpointEvent {
	event := checkpointEvent{
		ExecutionID:            identity.ExecutionID,
		AttemptID:              identity.AttemptID,
		MountID:                record.MountID,
		VolumeID:               record.VolumeID,
		OrgID:                  identity.OrgID,
		Provider:               identity.Provider,
		ProviderInstallationID: identity.ProviderInstallationID,
		ProviderRepositoryID:   identity.ProviderRepositoryID,
		ProviderRunID:          identity.ProviderRunID,
		ProviderJobID:          identity.ProviderJobID,
		RepositoryFullName:     identity.RepositoryFullName,
		RunnerClass:            identity.RunnerClass,
		ScopeKind:              scopeKind,
		ScopeRef:               scopeRef,
		TrustClass:             checkpointTrustClass,
		KeyHash:                record.KeyHash,
		MountPathHash:          record.MountPathHash,
	}
	if record.SourceGenerationID != nil {
		event.SourceGenerationID = *record.SourceGenerationID
	}
	if record.CommittedGenerationID != nil {
		event.CommittedGenerationID = *record.CommittedGenerationID
	}
	return event
}

func checkpointEventForFinalizer(item executionWorkItem, row store.ListSaveRequestedExecutionVolumeMountsRow) checkpointEvent {
	event := checkpointEvent{
		ExecutionID:            row.ExecutionID,
		AttemptID:              row.AttemptID,
		MountID:                row.MountID,
		VolumeID:               row.VolumeID,
		OrgID:                  orgIDFromDB(row.OrgID),
		Provider:               row.Provider,
		ProviderInstallationID: row.ProviderInstallationID,
		ProviderRepositoryID:   row.ProviderRepositoryID,
		ProviderJobID:          parseProviderID(item.ExternalTaskID),
		RepositoryFullName:     row.RepositoryFullName,
		RunnerClass:            item.RunnerClass,
		ScopeKind:              row.ScopeKind,
		ScopeRef:               row.ScopeRef,
		TrustClass:             checkpointTrustClass,
		KeyHash:                row.KeyHash,
		MountPathHash:          row.MountPathHash,
	}
	if row.SourceGenerationID != nil {
		event.SourceGenerationID = *row.SourceGenerationID
	}
	if row.CommittedGenerationID != nil {
		event.CommittedGenerationID = *row.CommittedGenerationID
	}
	return event
}

func checkpointEventForPrunedGeneration(item executionWorkItem, row store.ListPrunableVolumeGenerationsForAttemptRow) checkpointEvent {
	return checkpointEvent{
		ExecutionID:            row.ExecutionID,
		AttemptID:              row.AttemptID,
		MountID:                row.MountID,
		VolumeID:               row.VolumeID,
		CommittedGenerationID:  row.VolumeGenerationID,
		OrgID:                  orgIDFromDB(row.OrgID),
		Provider:               row.Provider,
		ProviderInstallationID: row.ProviderInstallationID,
		ProviderRepositoryID:   row.ProviderRepositoryID,
		ProviderJobID:          parseProviderID(item.ExternalTaskID),
		RepositoryFullName:     row.RepositoryFullName,
		RunnerClass:            item.RunnerClass,
		ScopeKind:              row.ScopeKind,
		ScopeRef:               row.ScopeRef,
		TrustClass:             row.TrustClass,
		KeyHash:                row.KeyHash,
		MountPathHash:          row.MountPathHash,
	}
}

func (event checkpointEvent) withTransition(eventName, fromMount, toMount, fromSave, toSave, operation, result, reason string) checkpointEvent {
	event.EventID = uuid.New()
	event.EventName = eventName
	event.ObservedAt = time.Now().UTC()
	event.FromMountState = fromMount
	event.ToMountState = toMount
	event.FromSaveState = fromSave
	event.ToSaveState = toSave
	event.Operation = operation
	event.Result = result
	event.ReasonCode = reason
	return event
}

func checkpointMountFromRecord(record store.ExecutionVolumeMount, cacheHit bool) CheckpointMount {
	sourceGeneration := int32(0)
	if record.SourceGeneration.Valid {
		sourceGeneration = record.SourceGeneration.Int32
	}
	return CheckpointMount{
		MountID:          record.MountID,
		MountName:        record.MountName,
		DevicePath:       record.GuestDevicePath,
		MountPath:        record.MountPath,
		FSType:           checkpointFilesystemType,
		CacheHit:         cacheHit,
		SourceGeneration: sourceGeneration,
		SourceRef:        record.SourceRef,
	}
}

func normalizeCheckpointKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || !checkpointKeyPattern.MatchString(value) {
		return "", fmt.Errorf("%w: key must be printable UTF-8 and at most 512 bytes", ErrCheckpointInvalid)
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", fmt.Errorf("%w: key must not contain control characters", ErrCheckpointInvalid)
	}
	return value, nil
}

func normalizeCheckpointMountPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("%w: path must be absolute", ErrCheckpointInvalid)
	}
	clean := path.Clean(value)
	if clean == "/" {
		return "", fmt.Errorf("%w: path must not be filesystem root", ErrCheckpointInvalid)
	}
	for _, reserved := range []string{"/dev", "/proc", "/sys", "/run", "/boot"} {
		if clean == reserved || strings.HasPrefix(clean, reserved+"/") {
			return "", fmt.Errorf("%w: path must not target %s", ErrCheckpointInvalid, reserved)
		}
	}
	if strings.ContainsRune(clean, '\x00') {
		return "", fmt.Errorf("%w: path must not contain NUL", ErrCheckpointInvalid)
	}
	return clean, nil
}

func checkpointScopeRef(identity RunnerExecutionIdentity) string {
	value := strings.TrimSpace(identity.HeadBranch)
	if value == "" {
		return "default"
	}
	return value
}

func checkpointMountName(id uuid.UUID) string {
	return "ckpt-" + strings.ReplaceAll(id.String(), "-", "")[:24]
}

func checkpointTargetSourceRef(volumeID uuid.UUID, generation int32) string {
	return "ckpt-" + strings.ReplaceAll(volumeID.String(), "-", "")[:24] + "-g" + strconv.Itoa(int(generation))
}

func hexSHA256(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func uuidStringFromZero(value uuid.UUID) string {
	if value == uuid.Nil {
		return ""
	}
	return value.String()
}

func uint64FromSigned(value int64) uint64 {
	if value <= 0 {
		return 0
	}
	return uint64(value)
}

func parseProviderID(value string) int64 {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed < 0 {
		return 0
	}
	return parsed
}

func classifyCheckpointError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return "operation_failed"
}

func firstNonZero(values ...uint64) uint64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
