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

	rows, err := s.PGX.Query(ctx, `
		SELECT
			g.installation_id,
			g.repository_id,
			COALESCE(last_mount.repository_full_name, ''),
			g.key_hash,
			g.key,
			g.current_generation,
			g.current_source_ref,
			COALESCE(last_mount.last_used_at, NULL),
			last_mount.completed_at,
			COALESCE(last_mount.save_state, ''),
			last_mount.execution_id,
			last_mount.attempt_id,
			COALESCE(last_mount.runner_class, ''),
			COALESCE(last_mount.workflow_name, ''),
			COALESCE(last_mount.job_name, ''),
			COALESCE(last_mount.mount_path, ''),
			g.updated_at
		FROM github_sticky_disk_generations g
		JOIN github_installations i ON i.installation_id = g.installation_id
		LEFT JOIN LATERAL (
			SELECT
				COALESCE(j.repository_full_name, '') AS repository_full_name,
				COALESCE(m.completed_at, m.requested_at, m.updated_at, m.created_at) AS last_used_at,
				m.completed_at,
				m.save_state,
				m.execution_id,
				m.attempt_id,
				a.runner_class,
				COALESCE(j.workflow_name, '') AS workflow_name,
				COALESCE(j.job_name, '') AS job_name,
				m.mount_path
			FROM execution_sticky_disk_mounts m
			JOIN github_runner_allocations a ON a.allocation_id = m.allocation_id
			LEFT JOIN github_runner_job_bindings b ON b.allocation_id = a.allocation_id
			LEFT JOIN github_workflow_jobs j ON j.github_job_id = COALESCE(b.github_job_id, a.requested_for_github_job_id)
			WHERE a.installation_id = g.installation_id
			  AND a.repository_id = g.repository_id
			  AND m.key_hash = g.key_hash
			ORDER BY COALESCE(m.completed_at, m.requested_at, m.updated_at, m.created_at) DESC, m.mount_id DESC
			LIMIT 1
		) last_mount ON true
		WHERE i.org_id = $1
		  AND i.active
		  AND ($2 = '' OR COALESCE(last_mount.repository_full_name, '') = $2)
		  AND ($3 = false OR (g.updated_at, g.installation_id, g.repository_id, g.key_hash) < ($4, $5, $6, $7))
		ORDER BY g.updated_at DESC, g.installation_id DESC, g.repository_id DESC, g.key_hash DESC
		LIMIT $8
	`, orgID, strings.TrimSpace(filters.Repository), cursorEnabled, cursor.UpdatedAt, cursor.InstallationID, cursor.RepositoryID, cursor.KeyHash, limit+1)
	if err != nil {
		return StickyDiskPage{}, fmt.Errorf("list sticky disks: %w", err)
	}
	defer rows.Close()

	disks := make([]StickyDiskRecord, 0, limit)
	for rows.Next() {
		var record StickyDiskRecord
		if err := rows.Scan(
			&record.InstallationID,
			&record.RepositoryID,
			&record.RepositoryFullName,
			&record.KeyHash,
			&record.Key,
			&record.CurrentGeneration,
			&record.CurrentSourceRef,
			&record.LastUsedAt,
			&record.LastCompletedAt,
			&record.LastSaveState,
			&record.LastExecutionID,
			&record.LastAttemptID,
			&record.LastRunnerClass,
			&record.LastWorkflowName,
			&record.LastJobName,
			&record.LastMountPath,
			&record.UpdatedAt,
		); err != nil {
			return StickyDiskPage{}, fmt.Errorf("scan sticky disk: %w", err)
		}
		disks = append(disks, record)
	}
	if err := rows.Err(); err != nil {
		return StickyDiskPage{}, fmt.Errorf("iterate sticky disks: %w", err)
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
	var deletedSourceRef string
	err := s.PGX.QueryRow(ctx, `
		DELETE FROM github_sticky_disk_generations g
		USING github_installations i
		WHERE g.installation_id = $1
		  AND g.repository_id = $2
		  AND g.key_hash = $3
		  AND i.installation_id = g.installation_id
		  AND i.org_id = $4
		  AND i.active
		RETURNING g.current_source_ref
	`, installationID, repositoryID, keyHash, orgID).Scan(&deletedSourceRef)
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
