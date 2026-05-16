package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/verself/sandbox-rental-service/internal/store"
)

const (
	durableCacheManagementListLimit = 200
)

type DurableCacheRecord struct {
	CacheID              uuid.UUID
	OrgID                string
	Provider             string
	ProviderRepositoryID int64
	RepositoryFullName   string
	ScopeRef             string
	Name                 string
	CreatedAt            time.Time
	CurrentGenerationID  *uuid.UUID
	GenerationCount      uint64
	UsedBytes            uint64
	WrittenBytes         uint64
	LastUsedAt           time.Time
}

type DurableCacheGenerationRecord struct {
	CacheGenerationID    uuid.UUID
	CacheID              uuid.UUID
	SourceGenerationID   *uuid.UUID
	OrgID                string
	Provider             string
	ProviderRepositoryID int64
	RepositoryFullName   string
	ScopeRef             string
	CacheName            string
	BindPaths            []string
	ProviderRunID        int64
	ProviderRunAttempt   int64
	ProviderJobID        int64
	HeadSHA              string
	TreeHash             string
	Result               string
	State                string
	ZFSSnapshotRef       string
	UsedBytes            uint64
	WrittenBytes         uint64
	SealedAt             time.Time
	CommittedAt          time.Time
	LastUsedAt           time.Time
	ExpiresAt            *time.Time
	IsCurrent            bool
}

type DurableCacheDeleteResult struct {
	DeletedGeneration DurableCacheGenerationRecord
}

type DurableCachePathDeleteResult struct {
	CacheID            uuid.UUID
	Path               string
	DeletedGenerations []DurableCacheGenerationRecord
}

func (s *Service) ListDurableCaches(ctx context.Context, orgID string) ([]DurableCacheRecord, error) {
	rows, err := s.storeQueries().ListDurableCaches(ctx, store.ListDurableCachesParams{
		OrgID:      dbOrgID(orgID),
		LimitCount: durableCacheManagementListLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list durable caches: %w", err)
	}
	out := make([]DurableCacheRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, DurableCacheRecord{
			CacheID:              row.DurableScopeID,
			OrgID:                orgIDFromDB(row.OrgID),
			Provider:             row.Provider,
			ProviderRepositoryID: row.ProviderRepositoryID,
			RepositoryFullName:   row.RepositoryFullName,
			ScopeRef:             row.ScopeRef,
			Name:                 row.CacheName,
			CreatedAt:            timeFromPG(row.CreatedAt),
			CurrentGenerationID:  copyUUIDPtr(row.CurrentGenerationID),
			GenerationCount:      uint64FromInt64(row.GenerationCount, "durable cache generation count"),
			UsedBytes:            uint64FromInt64(row.UsedBytes, "durable cache used bytes"),
			WrittenBytes:         uint64FromInt64(row.WrittenBytes, "durable cache written bytes"),
			LastUsedAt:           timeFromPG(row.LastUsedAt),
		})
	}
	return out, nil
}

func (s *Service) ListDurableCacheGenerations(ctx context.Context, orgID string, cacheID uuid.UUID) ([]DurableCacheGenerationRecord, error) {
	rows, err := s.storeQueries().ListDurableCacheGenerations(ctx, store.ListDurableCacheGenerationsParams{
		OrgID:          dbOrgID(orgID),
		DurableScopeID: cacheID,
		LimitCount:     durableCacheManagementListLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list durable cache generations: %w", err)
	}
	if len(rows) == 0 {
		if _, err := s.storeQueries().GetDurableCache(ctx, store.GetDurableCacheParams{
			OrgID:          dbOrgID(orgID),
			DurableScopeID: cacheID,
		}); errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDurableCacheMissing
		} else if err != nil {
			return nil, fmt.Errorf("get durable cache: %w", err)
		}
		return []DurableCacheGenerationRecord{}, nil
	}
	out := make([]DurableCacheGenerationRecord, 0, len(rows))
	for _, row := range rows {
		record, err := durableCacheGenerationRecordFromListRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, nil
}

func (s *Service) DeleteDurableCacheGeneration(ctx context.Context, orgID string, generationID uuid.UUID) (DurableCacheDeleteResult, error) {
	row, err := s.storeQueries().GetDurableCacheGenerationForUserPrune(ctx, store.GetDurableCacheGenerationForUserPruneParams{
		OrgID:               dbOrgID(orgID),
		DurableGenerationID: generationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return DurableCacheDeleteResult{}, ErrDurableCacheMissing
	}
	if err != nil {
		return DurableCacheDeleteResult{}, fmt.Errorf("get durable cache generation: %w", err)
	}
	record, err := s.pruneUserDurableCacheGeneration(ctx, durableCachePruneCandidateFromGenerationRow(row), "user_requested")
	if err != nil {
		return DurableCacheDeleteResult{}, err
	}
	return DurableCacheDeleteResult{DeletedGeneration: record}, nil
}

func (s *Service) DeleteDurableCachePath(ctx context.Context, orgID string, cacheID uuid.UUID, bindPath string) (DurableCachePathDeleteResult, error) {
	bindPath = strings.TrimSpace(bindPath)
	if bindPath == "" || !strings.HasPrefix(bindPath, "/") || bindPath == "/" {
		return DurableCachePathDeleteResult{}, fmt.Errorf("%w: cache path must be an absolute non-root path", ErrDurableCacheInvalid)
	}
	rows, err := s.storeQueries().ListDurableCacheGenerationsForPathPrune(ctx, store.ListDurableCacheGenerationsForPathPruneParams{
		OrgID:          dbOrgID(orgID),
		DurableScopeID: cacheID,
		BindPath:       bindPath,
	})
	if err != nil {
		return DurableCachePathDeleteResult{}, fmt.Errorf("list durable cache generations for path: %w", err)
	}
	if len(rows) == 0 {
		return DurableCachePathDeleteResult{}, ErrDurableCacheMissing
	}
	out := DurableCachePathDeleteResult{CacheID: cacheID, Path: bindPath, DeletedGenerations: make([]DurableCacheGenerationRecord, 0, len(rows))}
	for _, row := range rows {
		record, err := s.pruneUserDurableCacheGeneration(ctx, durableCachePruneCandidateFromPathRow(row), "user_path_requested")
		if err != nil {
			return DurableCachePathDeleteResult{}, err
		}
		out.DeletedGenerations = append(out.DeletedGenerations, record)
	}
	return out, nil
}

type durableCacheUserPruneCandidate struct {
	DurableGenerationID  uuid.UUID
	DurableScopeID       uuid.UUID
	OperationID          uuid.UUID
	SourceGenerationID   *uuid.UUID
	HeadSHA              string
	TreeHash             string
	Result               string
	ZFSSnapshotRef       string
	UsedBytes            int64
	WrittenBytes         int64
	ProviderRunID        int64
	ProviderRunAttempt   int64
	ProviderJobID        int64
	State                string
	SealedAt             time.Time
	CommittedAt          time.Time
	LastUsedAt           time.Time
	ExpiresAt            *time.Time
	OrgID                string
	Provider             string
	ProviderRepositoryID int64
	RepositoryFullName   string
	ScopeRef             string
	CacheName            string
	ExecutionID          uuid.UUID
	AttemptID            uuid.UUID
	BindPathsJSON        []byte
	IsCurrent            bool
}

func durableCachePruneCandidateFromGenerationRow(row store.GetDurableCacheGenerationForUserPruneRow) durableCacheUserPruneCandidate {
	return durableCacheUserPruneCandidate{
		DurableGenerationID:  row.DurableGenerationID,
		DurableScopeID:       row.DurableScopeID,
		OperationID:          row.OperationID,
		SourceGenerationID:   copyUUIDPtr(row.SourceGenerationID),
		HeadSHA:              row.HeadSha,
		TreeHash:             row.TreeHash,
		Result:               row.Result,
		ZFSSnapshotRef:       row.ZfsSnapshotRef,
		UsedBytes:            row.UsedBytes,
		WrittenBytes:         row.WrittenBytes,
		ProviderRunID:        row.ProviderRunID,
		ProviderRunAttempt:   row.ProviderRunAttempt,
		ProviderJobID:        row.ProviderJobID,
		State:                row.State,
		SealedAt:             timeFromPG(row.SealedAt),
		CommittedAt:          timeFromPG(row.CommittedAt),
		LastUsedAt:           timeFromPG(row.LastUsedAt),
		ExpiresAt:            timePtrFromPG(row.ExpiresAt),
		OrgID:                row.OrgID,
		Provider:             row.Provider,
		ProviderRepositoryID: row.ProviderRepositoryID,
		RepositoryFullName:   row.RepositoryFullName,
		ScopeRef:             row.ScopeRef,
		CacheName:            row.CacheName,
		ExecutionID:          row.ExecutionID,
		AttemptID:            row.AttemptID,
		BindPathsJSON:        row.BindPathsJson,
		IsCurrent:            row.IsCurrent,
	}
}

func durableCachePruneCandidateFromPathRow(row store.ListDurableCacheGenerationsForPathPruneRow) durableCacheUserPruneCandidate {
	return durableCacheUserPruneCandidate{
		DurableGenerationID:  row.DurableGenerationID,
		DurableScopeID:       row.DurableScopeID,
		OperationID:          row.OperationID,
		SourceGenerationID:   copyUUIDPtr(row.SourceGenerationID),
		HeadSHA:              row.HeadSha,
		TreeHash:             row.TreeHash,
		Result:               row.Result,
		ZFSSnapshotRef:       row.ZfsSnapshotRef,
		UsedBytes:            row.UsedBytes,
		WrittenBytes:         row.WrittenBytes,
		ProviderRunID:        row.ProviderRunID,
		ProviderRunAttempt:   row.ProviderRunAttempt,
		ProviderJobID:        row.ProviderJobID,
		State:                row.State,
		SealedAt:             timeFromPG(row.SealedAt),
		CommittedAt:          timeFromPG(row.CommittedAt),
		LastUsedAt:           timeFromPG(row.LastUsedAt),
		ExpiresAt:            timePtrFromPG(row.ExpiresAt),
		OrgID:                row.OrgID,
		Provider:             row.Provider,
		ProviderRepositoryID: row.ProviderRepositoryID,
		RepositoryFullName:   row.RepositoryFullName,
		ScopeRef:             row.ScopeRef,
		CacheName:            row.CacheName,
		ExecutionID:          row.ExecutionID,
		AttemptID:            row.AttemptID,
		BindPathsJSON:        row.BindPathsJson,
		IsCurrent:            row.IsCurrent,
	}
}

func (s *Service) pruneUserDurableCacheGeneration(ctx context.Context, candidate durableCacheUserPruneCandidate, reason string) (DurableCacheGenerationRecord, error) {
	if s.Orchestrator == nil {
		return DurableCacheGenerationRecord{}, ErrRunnerUnavailable
	}
	bindPaths, err := decodeBindPaths(candidate.BindPathsJSON)
	if err != nil {
		return DurableCacheGenerationRecord{}, err
	}
	now := time.Now().UTC()
	tx, err := s.PGX.Begin(ctx)
	if err != nil {
		return DurableCacheGenerationRecord{}, fmt.Errorf("begin durable cache prune: %w", err)
	}
	qtx := s.storeQueries().WithTx(tx)
	rowsChanged, err := qtx.MarkDurableGenerationUserPruning(ctx, store.MarkDurableGenerationUserPruningParams{
		PruningAt:           pgTime(now),
		DurableGenerationID: candidate.DurableGenerationID,
		DurableScopeID:      candidate.DurableScopeID,
		OrgID:               candidate.OrgID,
	})
	if err != nil {
		_ = tx.Rollback(ctx)
		return DurableCacheGenerationRecord{}, fmt.Errorf("mark durable cache generation pruning: %w", err)
	}
	if rowsChanged == 0 {
		_ = tx.Rollback(ctx)
		return DurableCacheGenerationRecord{}, ErrDurableCacheBusy
	}
	generationID := candidate.DurableGenerationID
	if candidate.IsCurrent {
		if err := qtx.ClearDurableCurrentPointerForGeneration(ctx, store.ClearDurableCurrentPointerForGenerationParams{
			ClearedAt:           pgTime(now),
			DurableScopeID:      candidate.DurableScopeID,
			DurableGenerationID: &generationID,
		}); err != nil {
			_ = tx.Rollback(ctx)
			return DurableCacheGenerationRecord{}, fmt.Errorf("clear durable cache current pointer: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return DurableCacheGenerationRecord{}, fmt.Errorf("commit durable cache prune state: %w", err)
	}

	storage, storageKnown, storageErr := s.durableStorageSnapshot(ctx)
	if storageErr != nil && s.Logger != nil {
		s.Logger.WarnContext(ctx, "durable cache prune capacity snapshot failed", "error", storageErr)
	}
	identity := RunnerExecutionIdentity{
		OrgID:                candidate.OrgID,
		Provider:             candidate.Provider,
		ProviderRepositoryID: candidate.ProviderRepositoryID,
		ProviderRunID:        candidate.ProviderRunID,
		ProviderRunAttempt:   candidate.ProviderRunAttempt,
		ProviderJobID:        candidate.ProviderJobID,
	}
	_, err = s.Orchestrator.PruneFilesystemGeneration(ctx, candidate.DurableGenerationID.String()+":user-prune", candidate.OperationID.String(), candidate.DurableGenerationID.String(), candidate.DurableScopeID.String(), candidate.ZFSSnapshotRef, candidate.OrgID)
	if err != nil {
		event := durableEvent{OperationID: &candidate.OperationID, ScopeID: &candidate.DurableScopeID, GenerationID: &candidate.DurableGenerationID, ExecutionID: &candidate.ExecutionID, AttemptID: &candidate.AttemptID, Identity: identity, CacheName: candidate.CacheName, Name: durableEventPrune, Result: "failed", Reason: err.Error(), ZFSSnapshotRef: candidate.ZFSSnapshotRef, UsedBytes: uint64FromInt64(candidate.UsedBytes, "durable used bytes"), WrittenBytes: uint64FromInt64(candidate.WrittenBytes, "durable written bytes")}
		if storageKnown {
			event = event.withStorageCapacity(storage.Capacity, candidate.OrgID)
		}
		_ = s.appendDurableEvent(ctx, event)
		return DurableCacheGenerationRecord{}, fmt.Errorf("prune durable cache generation: %w", err)
	}
	if err := s.storeQueries().MarkDurableGenerationPruned(ctx, store.MarkDurableGenerationPrunedParams{
		PrunedAt:            pgTime(time.Now().UTC()),
		DurableGenerationID: candidate.DurableGenerationID,
		DurableScopeID:      candidate.DurableScopeID,
	}); err != nil {
		return DurableCacheGenerationRecord{}, fmt.Errorf("mark durable cache generation pruned: %w", err)
	}
	event := durableEvent{OperationID: &candidate.OperationID, ScopeID: &candidate.DurableScopeID, GenerationID: &candidate.DurableGenerationID, ExecutionID: &candidate.ExecutionID, AttemptID: &candidate.AttemptID, Identity: identity, CacheName: candidate.CacheName, Name: durableEventPrune, Result: "succeeded", Reason: reason, ZFSSnapshotRef: candidate.ZFSSnapshotRef, UsedBytes: uint64FromInt64(candidate.UsedBytes, "durable used bytes"), WrittenBytes: uint64FromInt64(candidate.WrittenBytes, "durable written bytes")}
	if storageKnown {
		event = event.withStorageCapacity(storage.Capacity, candidate.OrgID)
	}
	_ = s.appendDurableEvent(ctx, event)
	return DurableCacheGenerationRecord{
		CacheGenerationID:    candidate.DurableGenerationID,
		CacheID:              candidate.DurableScopeID,
		SourceGenerationID:   copyUUIDPtr(candidate.SourceGenerationID),
		OrgID:                orgIDFromDB(candidate.OrgID),
		Provider:             candidate.Provider,
		ProviderRepositoryID: candidate.ProviderRepositoryID,
		RepositoryFullName:   candidate.RepositoryFullName,
		ScopeRef:             candidate.ScopeRef,
		CacheName:            candidate.CacheName,
		BindPaths:            bindPaths,
		ProviderRunID:        candidate.ProviderRunID,
		ProviderRunAttempt:   candidate.ProviderRunAttempt,
		ProviderJobID:        candidate.ProviderJobID,
		HeadSHA:              candidate.HeadSHA,
		TreeHash:             candidate.TreeHash,
		Result:               candidate.Result,
		State:                "pruned",
		ZFSSnapshotRef:       candidate.ZFSSnapshotRef,
		UsedBytes:            uint64FromInt64(candidate.UsedBytes, "durable used bytes"),
		WrittenBytes:         uint64FromInt64(candidate.WrittenBytes, "durable written bytes"),
		SealedAt:             candidate.SealedAt,
		CommittedAt:          candidate.CommittedAt,
		LastUsedAt:           candidate.LastUsedAt,
		ExpiresAt:            candidate.ExpiresAt,
		IsCurrent:            false,
	}, nil
}

func durableCacheGenerationRecordFromListRow(row store.ListDurableCacheGenerationsRow) (DurableCacheGenerationRecord, error) {
	bindPaths, err := decodeBindPaths(row.BindPathsJson)
	if err != nil {
		return DurableCacheGenerationRecord{}, err
	}
	return DurableCacheGenerationRecord{
		CacheGenerationID:    row.DurableGenerationID,
		CacheID:              row.DurableScopeID,
		SourceGenerationID:   copyUUIDPtr(row.SourceGenerationID),
		OrgID:                orgIDFromDB(row.OrgID),
		Provider:             row.Provider,
		ProviderRepositoryID: row.ProviderRepositoryID,
		RepositoryFullName:   row.RepositoryFullName,
		ScopeRef:             row.ScopeRef,
		CacheName:            row.CacheName,
		BindPaths:            bindPaths,
		ProviderRunID:        row.ProviderRunID,
		ProviderRunAttempt:   row.ProviderRunAttempt,
		ProviderJobID:        row.ProviderJobID,
		HeadSHA:              row.HeadSha,
		TreeHash:             row.TreeHash,
		Result:               row.Result,
		State:                row.State,
		ZFSSnapshotRef:       row.ZfsSnapshotRef,
		UsedBytes:            uint64FromInt64(row.UsedBytes, "durable used bytes"),
		WrittenBytes:         uint64FromInt64(row.WrittenBytes, "durable written bytes"),
		SealedAt:             timeFromPG(row.SealedAt),
		CommittedAt:          timeFromPG(row.CommittedAt),
		LastUsedAt:           timeFromPG(row.LastUsedAt),
		ExpiresAt:            timePtrFromPG(row.ExpiresAt),
		IsCurrent:            row.IsCurrent,
	}, nil
}

func decodeBindPaths(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var paths []string
	if err := json.Unmarshal(raw, &paths); err != nil {
		return nil, fmt.Errorf("decode durable cache bind paths: %w", err)
	}
	return paths, nil
}

func copyUUIDPtr(value *uuid.UUID) *uuid.UUID {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
