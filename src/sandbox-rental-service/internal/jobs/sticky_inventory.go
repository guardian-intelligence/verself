package jobs

import (
	"context"
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

var ErrStickyDiskCursorInvalid = errors.New("sandbox-rental: sticky disk cursor invalid")

type StickyDiskListFilters struct {
	Limit      int
	Cursor     string
	Repository string
}

type StickyDiskPage struct {
	Disks      []StickyDiskRecord
	NextCursor string
	Limit      int
}

type StickyDiskRecord struct {
	InstallationID     int64
	RepositoryID       int64
	RepositoryFullName string
	KeyHash            string
	Key                string
	CurrentGeneration  int64
	CurrentSourceRef   string
	LastUsedAt         *time.Time
	LastCompletedAt    *time.Time
	LastSaveState      string
	LastExecutionID    *uuid.UUID
	LastAttemptID      *uuid.UUID
	LastRunnerClass    string
	LastWorkflowName   string
	LastJobName        string
	LastMountPath      string
	UpdatedAt          time.Time
}

type StickyDiskResetResult struct {
	InstallationID   int64
	RepositoryID     int64
	KeyHash          string
	DeletedSourceRef string
	ResetAt          time.Time
}

type stickyDiskCursor struct {
	UpdatedAt      time.Time
	InstallationID int64
	RepositoryID   int64
	KeyHash        string
}

func makeStickyDiskCursor(updatedAt time.Time, installationID, repositoryID int64, keyHash string) string {
	return fmt.Sprintf("%s.%d.%d.%s", updatedAt.UTC().Format(time.RFC3339Nano), installationID, repositoryID, keyHash)
}

func parseStickyDiskCursor(value string) (stickyDiskCursor, error) {
	lastDot := strings.LastIndex(value, ".")
	if lastDot <= 0 || lastDot == len(value)-1 {
		return stickyDiskCursor{}, ErrStickyDiskCursorInvalid
	}
	keyHash := value[lastDot+1:]
	prefix := value[:lastDot]
	secondDot := strings.LastIndex(prefix, ".")
	if secondDot <= 0 || secondDot == len(prefix)-1 {
		return stickyDiskCursor{}, ErrStickyDiskCursorInvalid
	}
	repositoryID, err := strconv.ParseInt(prefix[secondDot+1:], 10, 64)
	if err != nil {
		return stickyDiskCursor{}, ErrStickyDiskCursorInvalid
	}
	prefix = prefix[:secondDot]
	thirdDot := strings.LastIndex(prefix, ".")
	if thirdDot <= 0 || thirdDot == len(prefix)-1 {
		return stickyDiskCursor{}, ErrStickyDiskCursorInvalid
	}
	installationID, err := strconv.ParseInt(prefix[thirdDot+1:], 10, 64)
	if err != nil {
		return stickyDiskCursor{}, ErrStickyDiskCursorInvalid
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, prefix[:thirdDot])
	if err != nil {
		return stickyDiskCursor{}, ErrStickyDiskCursorInvalid
	}
	return stickyDiskCursor{UpdatedAt: updatedAt.UTC(), InstallationID: installationID, RepositoryID: repositoryID, KeyHash: keyHash}, nil
}

func (s *Service) ListStickyDisks(ctx context.Context, orgID uint64, filters StickyDiskListFilters) (StickyDiskPage, error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.stickydisks.list")
	defer span.End()
	limit := filters.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	cursor := stickyDiskCursor{UpdatedAt: time.Unix(0, 0).UTC()}
	cursorEnabled := false
	if strings.TrimSpace(filters.Cursor) != "" {
		parsed, err := parseStickyDiskCursor(filters.Cursor)
		if err != nil {
			return StickyDiskPage{}, err
		}
		cursor = parsed
		cursorEnabled = true
	}

	rows, err := s.storeQueries().ListStickyDisks(ctx, store.ListStickyDisksParams{
		OrgID:                dbOrgID(orgID),
		Repository:           strings.TrimSpace(filters.Repository),
		CursorEnabled:        cursorEnabled,
		CursorUpdatedAt:      pgTime(cursor.UpdatedAt),
		CursorInstallationID: cursor.InstallationID,
		CursorRepositoryID:   cursor.RepositoryID,
		CursorKeyHash:        cursor.KeyHash,
		LimitCount:           int32(limit + 1),
	})
	if err != nil {
		return StickyDiskPage{}, fmt.Errorf("list sticky disks: %w", err)
	}

	disks := make([]StickyDiskRecord, 0, limit)
	for _, row := range rows {
		disks = append(disks, StickyDiskRecord{
			InstallationID:     row.ProviderInstallationID,
			RepositoryID:       row.ProviderRepositoryID,
			RepositoryFullName: row.RepositoryFullName,
			KeyHash:            row.KeyHash,
			Key:                row.Key,
			CurrentGeneration:  row.CurrentGeneration,
			CurrentSourceRef:   row.CurrentSourceRef,
			LastUsedAt:         timePtrFromPG(row.LastUsedAt),
			LastCompletedAt:    timePtrFromPG(row.CompletedAt),
			LastSaveState:      row.SaveState,
			LastExecutionID:    uuidPtrFromZero(row.ExecutionID),
			LastAttemptID:      uuidPtrFromZero(row.AttemptID),
			LastRunnerClass:    row.RunnerClass,
			LastWorkflowName:   row.WorkflowName,
			LastJobName:        row.JobName,
			LastMountPath:      row.MountPath,
			UpdatedAt:          timeFromPG(row.UpdatedAt),
		})
	}

	nextCursor := ""
	if len(disks) > limit {
		last := disks[limit-1]
		nextCursor = makeStickyDiskCursor(last.UpdatedAt, last.InstallationID, last.RepositoryID, last.KeyHash)
		disks = disks[:limit]
	}
	span.SetAttributes(traceOrgID(orgID), attribute.Int("sandbox.sticky_disk_count", len(disks)))
	return StickyDiskPage{Disks: disks, NextCursor: nextCursor, Limit: limit}, nil
}

func (s *Service) ResetStickyDisk(ctx context.Context, orgID uint64, installationID, repositoryID int64, keyHash string) (StickyDiskResetResult, error) {
	ctx, span := tracer.Start(ctx, "sandbox-rental.stickydisks.reset")
	defer span.End()
	keyHash = strings.TrimSpace(keyHash)
	if keyHash == "" {
		return StickyDiskResetResult{}, ErrStickyDiskInvalid
	}
	now := time.Now().UTC()
	deletedSourceRef, err := s.storeQueries().DeleteStickyDiskGenerationForOrg(ctx, store.DeleteStickyDiskGenerationForOrgParams{
		ProviderInstallationID: installationID,
		ProviderRepositoryID:   repositoryID,
		KeyHash:                keyHash,
		OrgID:                  dbOrgID(orgID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StickyDiskResetResult{}, ErrStickyDiskMissing
		}
		return StickyDiskResetResult{}, fmt.Errorf("reset sticky disk: %w", err)
	}
	span.SetAttributes(
		traceOrgID(orgID),
		attribute.Int64("github.installation_id", installationID),
		attribute.Int64("github.repository_id", repositoryID),
		attribute.String("github.stickydisk.key_hash", keyHash),
	)
	return StickyDiskResetResult{
		InstallationID:   installationID,
		RepositoryID:     repositoryID,
		KeyHash:          keyHash,
		DeletedSourceRef: deletedSourceRef,
		ResetAt:          now,
	}, nil
}
