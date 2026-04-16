package jobs

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	vmorchestrator "github.com/forge-metal/vm-orchestrator"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const (
	stickyDiskMaxKeyBytes        = 512
	stickyDiskMaxPathBytes       = 4096
	stickyDiskEmptySourceRef     = "sticky-empty"
	stickyDiskStateNotRequested  = "not_requested"
	stickyDiskStateRequested     = "requested"
	stickyDiskStateRunning       = "running"
	stickyDiskStateCommitted     = "committed"
	stickyDiskStateFailed        = "failed"
	stickyDiskStateSkipped       = "skipped"
	stickyDiskWorkspaceRoot      = "/workspace"
	stickyDiskRunnerHome         = "/home/runner"
	stickyDiskTargetRefHashBytes = 16
)

var (
	ErrStickyDiskUnauthorized = errors.New("sticky disk request is not authorized")
	ErrStickyDiskMissing      = errors.New("sticky disk generation is missing")
	ErrStickyDiskInvalid      = errors.New("sticky disk request is invalid")
)

type StickyDiskIdentity struct {
	ExecutionID        uuid.UUID
	AttemptID          uuid.UUID
	AllocationID       uuid.UUID
	Installation       int64
	RepositoryID       int64
	RepositoryFullName string
	GitHubJobID        int64
	RunnerName         string
}

// StickyDiskSave is the guest-visible fast path. The action asks the control
// plane to persist a pre-mounted zvol after the runner exits; no archive bytes
// cross the guest boundary.
type StickyDiskSave struct {
	Identity    StickyDiskIdentity
	CommitID    uuid.UUID
	Key         string
	MountPath   string
	RequestedAt time.Time
}

type StickyDiskMountSpec struct {
	MountID         uuid.UUID
	AllocationID    uuid.UUID
	MountName       string
	Key             string
	KeyHash         string
	MountPath       string
	BaseGeneration  int64
	SourceRef       string
	TargetSourceRef string
}

type stickyDiskPendingCommit struct {
	MountID         uuid.UUID
	Identity        StickyDiskIdentity
	MountName       string
	Key             string
	KeyHash         string
	MountPath       string
	BaseGeneration  int64
	TargetSourceRef string
}

func (r *GitHubRunner) AuthenticateStickyDisk(ctx context.Context, executionID, attemptID, bearer string) (StickyDiskIdentity, error) {
	executionID = strings.TrimSpace(executionID)
	attemptID = strings.TrimSpace(attemptID)
	bearer = strings.TrimSpace(strings.TrimPrefix(bearer, "Bearer "))
	execUUID, err := uuid.Parse(executionID)
	if err != nil {
		return StickyDiskIdentity{}, fmt.Errorf("%w: invalid execution id", ErrStickyDiskUnauthorized)
	}
	attemptUUID, err := uuid.Parse(attemptID)
	if err != nil {
		return StickyDiskIdentity{}, fmt.Errorf("%w: invalid attempt id", ErrStickyDiskUnauthorized)
	}
	return r.authenticateGitHubExecution(ctx, execUUID, attemptUUID, bearer, r.deriveStickyDiskToken(execUUID, attemptUUID), ErrStickyDiskUnauthorized)
}

func (r *GitHubRunner) AuthenticateCheckout(ctx context.Context, executionID, attemptID, bearer string) (StickyDiskIdentity, error) {
	executionID = strings.TrimSpace(executionID)
	attemptID = strings.TrimSpace(attemptID)
	bearer = strings.TrimSpace(strings.TrimPrefix(bearer, "Bearer "))
	execUUID, err := uuid.Parse(executionID)
	if err != nil {
		return StickyDiskIdentity{}, fmt.Errorf("%w: invalid execution id", ErrCheckoutUnauthorized)
	}
	attemptUUID, err := uuid.Parse(attemptID)
	if err != nil {
		return StickyDiskIdentity{}, fmt.Errorf("%w: invalid attempt id", ErrCheckoutUnauthorized)
	}
	return r.authenticateGitHubExecution(ctx, execUUID, attemptUUID, bearer, r.deriveCheckoutToken(execUUID, attemptUUID), ErrCheckoutUnauthorized)
}

func (r *GitHubRunner) authenticateGitHubExecution(ctx context.Context, executionID, attemptID uuid.UUID, bearer, expected string, unauthorizedErr error) (StickyDiskIdentity, error) {
	if bearer == "" || !hmac.Equal([]byte(bearer), []byte(expected)) {
		return StickyDiskIdentity{}, unauthorizedErr
	}

	var identity StickyDiskIdentity
	err := r.service.PGX.QueryRow(ctx, `SELECT
			a.allocation_id,
			a.installation_id,
			a.repository_id,
			COALESCE(j.repository_full_name, ''),
			COALESCE(b.github_job_id, a.requested_for_github_job_id),
			a.runner_name
			FROM github_runner_allocations a
			LEFT JOIN github_runner_job_bindings b ON b.allocation_id = a.allocation_id
			LEFT JOIN github_workflow_jobs j ON j.github_job_id = COALESCE(b.github_job_id, a.requested_for_github_job_id)
			WHERE a.execution_id = $1 AND a.attempt_id = $2`,
		executionID, attemptID).Scan(&identity.AllocationID, &identity.Installation, &identity.RepositoryID, &identity.RepositoryFullName, &identity.GitHubJobID, &identity.RunnerName)
	if errors.Is(err, pgx.ErrNoRows) {
		return StickyDiskIdentity{}, unauthorizedErr
	}
	if err != nil {
		return StickyDiskIdentity{}, err
	}
	identity.ExecutionID = executionID
	identity.AttemptID = attemptID
	return identity, nil
}

func (r *GitHubRunner) RequestStickyDiskCommit(ctx context.Context, identity StickyDiskIdentity, key, mountPath string) (StickyDiskSave, error) {
	ctx, span := tracer.Start(ctx, "github.stickydisk.save_request")
	defer span.End()
	key, err := normalizeStickyDiskKey(key)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return StickyDiskSave{}, err
	}
	mountPath, err = normalizeStickyDiskPath(mountPath)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return StickyDiskSave{}, err
	}
	span.SetAttributes(append(stickyDiskAttributes(identity, key), attribute.String("github.stickydisk.mount_path", mountPath))...)
	if r.service == nil || r.service.PGX == nil {
		return StickyDiskSave{}, fmt.Errorf("%w: database unavailable", ErrStickyDiskInvalid)
	}

	now := time.Now().UTC()
	keyHash := stickyDiskKeyHash(key)
	var save StickyDiskSave
	err = r.service.PGX.QueryRow(ctx, `UPDATE execution_sticky_disk_mounts
		SET save_requested = true,
		    save_state = $1,
		    requested_at = $2,
		    started_at = NULL,
		    completed_at = NULL,
		    failure_reason = '',
		    updated_at = $2
		WHERE attempt_id = $3
		  AND key_hash = $4
		  AND mount_path = $5
		RETURNING mount_id, requested_at`, stickyDiskStateRequested, now, identity.AttemptID, keyHash, mountPath).Scan(&save.CommitID, &save.RequestedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = fmt.Errorf("%w: sticky disk %s at %s was not provisioned before VM boot", ErrStickyDiskInvalid, keyHash, mountPath)
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return StickyDiskSave{}, err
	}
	save.Identity = identity
	save.Key = key
	save.MountPath = mountPath
	span.SetAttributes(attribute.String("github.stickydisk.mount_id", save.CommitID.String()))
	return save, nil
}

func (r *GitHubRunner) CommitPendingStickyDisks(ctx context.Context, item executionWorkItem, leaseID string) error {
	ctx, span := tracer.Start(ctx, "github.stickydisk.commit_pending")
	defer span.End()
	span.SetAttributes(attribute.String("execution.id", item.ExecutionID.String()), attribute.String("attempt.id", item.AttemptID.String()))
	if strings.TrimSpace(leaseID) == "" || r == nil || r.service == nil || r.service.PGX == nil || r.service.Orchestrator == nil {
		span.SetAttributes(attribute.Bool("github.stickydisk.noop", true))
		return nil
	}
	pending, err := r.pendingStickyDiskCommits(ctx, item.AttemptID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(attribute.Int("github.stickydisk.pending_count", len(pending)))
	for _, commit := range pending {
		if err := r.commitStickyDiskMount(ctx, item, leaseID, commit); err != nil {
			if r.service.Logger != nil {
				r.service.Logger.WarnContext(ctx, "sticky disk zfs commit failed", "mount_id", commit.MountID, "attempt_id", item.AttemptID, "key_hash", commit.KeyHash, "error", err)
			}
		}
	}
	return nil
}

func (r *GitHubRunner) pendingStickyDiskCommits(ctx context.Context, attemptID uuid.UUID) ([]stickyDiskPendingCommit, error) {
	rows, err := r.service.PGX.Query(ctx, `SELECT
		m.mount_id, m.mount_name, m.key, m.key_hash, m.mount_path, m.base_generation, m.target_source_ref,
		m.execution_id, m.attempt_id, m.allocation_id,
		a.installation_id, a.repository_id, COALESCE(j.repository_full_name, ''),
		COALESCE(b.github_job_id, a.requested_for_github_job_id), a.runner_name
		FROM execution_sticky_disk_mounts m
		JOIN github_runner_allocations a ON a.allocation_id = m.allocation_id
		LEFT JOIN github_runner_job_bindings b ON b.allocation_id = a.allocation_id
		LEFT JOIN github_workflow_jobs j ON j.github_job_id = COALESCE(b.github_job_id, a.requested_for_github_job_id)
		WHERE m.attempt_id = $1 AND m.save_requested AND m.save_state = $2
		ORDER BY m.requested_at, m.mount_name`, attemptID, stickyDiskStateRequested)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []stickyDiskPendingCommit{}
	for rows.Next() {
		var commit stickyDiskPendingCommit
		if err := rows.Scan(&commit.MountID, &commit.MountName, &commit.Key, &commit.KeyHash, &commit.MountPath, &commit.BaseGeneration, &commit.TargetSourceRef,
			&commit.Identity.ExecutionID, &commit.Identity.AttemptID, &commit.Identity.AllocationID,
			&commit.Identity.Installation, &commit.Identity.RepositoryID, &commit.Identity.RepositoryFullName,
			&commit.Identity.GitHubJobID, &commit.Identity.RunnerName); err != nil {
			return nil, err
		}
		out = append(out, commit)
	}
	return out, rows.Err()
}

func (r *GitHubRunner) commitStickyDiskMount(ctx context.Context, item executionWorkItem, leaseID string, commit stickyDiskPendingCommit) error {
	ctx, span := tracer.Start(ctx, "github.stickydisk.commit_zfs")
	defer span.End()
	span.SetAttributes(
		attribute.String("github.stickydisk.mount_id", commit.MountID.String()),
		attribute.String("github.stickydisk.key_hash", commit.KeyHash),
		attribute.String("github.stickydisk.mount_name", commit.MountName),
		attribute.String("github.stickydisk.mount_path", commit.MountPath),
		attribute.String("github.stickydisk.target_source_ref", commit.TargetSourceRef),
		attribute.String("lease.id", leaseID),
	)
	if started, err := r.markStickyDiskCommitRunning(ctx, commit.MountID); err != nil || !started {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		span.SetAttributes(attribute.Bool("github.stickydisk.already_claimed", true))
		return nil
	}

	result, err := r.service.Orchestrator.CommitFilesystemMount(ctx, leaseID, item.AttemptID.String()+":stickydisk:"+commit.MountName, commit.MountName, commit.TargetSourceRef)
	if err != nil {
		_ = r.markStickyDiskCommitFinished(detachedContext(ctx), commit.MountID, stickyDiskStateFailed, "commit_filesystem_mount_failed: "+err.Error(), 0, "")
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	generation := commit.BaseGeneration + 1
	if err := r.promoteStickyDiskGeneration(ctx, commit, generation); err != nil {
		_ = r.markStickyDiskCommitFinished(detachedContext(ctx), commit.MountID, stickyDiskStateFailed, "promote_generation_failed: "+err.Error(), 0, result.Snapshot)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := r.markStickyDiskCommitFinished(ctx, commit.MountID, stickyDiskStateCommitted, "", generation, result.Snapshot); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(
		attribute.String("github.stickydisk.state", stickyDiskStateCommitted),
		attribute.Int64("github.stickydisk.generation", generation),
		attribute.String("github.stickydisk.snapshot", result.Snapshot),
	)
	return nil
}

func (r *GitHubRunner) markStickyDiskCommitRunning(ctx context.Context, mountID uuid.UUID) (bool, error) {
	tag, err := r.service.PGX.Exec(ctx, `UPDATE execution_sticky_disk_mounts
		SET save_state = $1, started_at = $2, updated_at = $2
		WHERE mount_id = $3 AND save_state = $4`, stickyDiskStateRunning, time.Now().UTC(), mountID, stickyDiskStateRequested)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *GitHubRunner) markStickyDiskCommitFinished(ctx context.Context, mountID uuid.UUID, state, reason string, generation int64, snapshot string) error {
	if len(reason) > 2048 {
		reason = reason[:2048]
	}
	_, err := r.service.PGX.Exec(ctx, `UPDATE execution_sticky_disk_mounts
		SET save_state = $1,
		    failure_reason = $2,
		    committed_generation = $3,
		    committed_snapshot = $4,
		    completed_at = $5,
		    updated_at = $5
		WHERE mount_id = $6`, state, reason, generation, snapshot, time.Now().UTC(), mountID)
	return err
}

func (r *GitHubRunner) promoteStickyDiskGeneration(ctx context.Context, commit stickyDiskPendingCommit, generation int64) error {
	tx, err := r.service.PGX.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var current int64
	err = tx.QueryRow(ctx, `SELECT current_generation
		FROM github_sticky_disk_generations
		WHERE installation_id = $1 AND repository_id = $2 AND key_hash = $3
		FOR UPDATE`, commit.Identity.Installation, commit.Identity.RepositoryID, commit.KeyHash).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		current = 0
	} else if err != nil {
		return err
	}
	if current != commit.BaseGeneration {
		return fmt.Errorf("sticky disk generation moved from %d to %d while execution was running", commit.BaseGeneration, current)
	}
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `INSERT INTO github_sticky_disk_generations (
		installation_id, repository_id, key_hash, key, current_generation, current_source_ref, created_at, updated_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$7)
	ON CONFLICT (installation_id, repository_id, key_hash) DO UPDATE SET
		key = EXCLUDED.key,
		current_generation = EXCLUDED.current_generation,
		current_source_ref = EXCLUDED.current_source_ref,
		updated_at = EXCLUDED.updated_at`, commit.Identity.Installation, commit.Identity.RepositoryID, commit.KeyHash, commit.Key, generation, commit.TargetSourceRef, now)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *GitHubRunner) currentStickyDiskGeneration(ctx context.Context, installationID, repositoryID int64, key, keyHash string) (int64, string, error) {
	var generation int64
	var sourceRef string
	err := r.service.PGX.QueryRow(ctx, `SELECT current_generation, current_source_ref
		FROM github_sticky_disk_generations
		WHERE installation_id = $1 AND repository_id = $2 AND key_hash = $3`, installationID, repositoryID, keyHash).Scan(&generation, &sourceRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, stickyDiskEmptySourceRef, nil
	}
	if err != nil {
		return 0, "", err
	}
	if strings.TrimSpace(sourceRef) == "" {
		sourceRef = stickyDiskEmptySourceRef
	}
	return generation, sourceRef, nil
}

func stickyDiskMountSpec(allocationID uuid.UUID, attemptID uuid.UUID, idx int, installationID, repositoryID int64, key, mountPath string, generation int64, sourceRef string) StickyDiskMountSpec {
	keyHash := stickyDiskKeyHash(key)
	nextGeneration := generation + 1
	shortHash := keyHash
	if len(shortHash) > stickyDiskTargetRefHashBytes {
		shortHash = shortHash[:stickyDiskTargetRefHashBytes]
	}
	attemptPart := strings.ReplaceAll(attemptID.String(), "-", "")
	if len(attemptPart) > 12 {
		attemptPart = attemptPart[:12]
	}
	_ = installationID
	_ = repositoryID
	return StickyDiskMountSpec{
		MountID:         uuid.New(),
		AllocationID:    allocationID,
		MountName:       fmt.Sprintf("sticky-%02d", idx),
		Key:             key,
		KeyHash:         keyHash,
		MountPath:       mountPath,
		BaseGeneration:  generation,
		SourceRef:       sourceRef,
		TargetSourceRef: fmt.Sprintf("sticky-%s-%s-g%d", shortHash, attemptPart, nextGeneration),
	}
}

func stickyDiskFilesystemMount(spec StickyDiskMountSpec) vmorchestrator.FilesystemMount {
	return vmorchestrator.FilesystemMount{
		Name:      spec.MountName,
		SourceRef: spec.SourceRef,
		MountPath: spec.MountPath,
		FSType:    "ext4",
		ReadOnly:  false,
	}
}

func normalizeStickyDiskKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("%w: key is required", ErrStickyDiskInvalid)
	}
	if len([]byte(key)) > stickyDiskMaxKeyBytes {
		return "", fmt.Errorf("%w: key exceeds %d bytes", ErrStickyDiskInvalid, stickyDiskMaxKeyBytes)
	}
	if strings.ContainsAny(key, "\x00\r\n") {
		return "", fmt.Errorf("%w: key contains control characters", ErrStickyDiskInvalid)
	}
	return key, nil
}

func normalizeStickyDiskPath(mountPath string) (string, error) {
	mountPath = filepath.Clean(strings.TrimSpace(mountPath))
	if mountPath == "." || mountPath == "" {
		return "", fmt.Errorf("%w: path is required", ErrStickyDiskInvalid)
	}
	if !filepath.IsAbs(mountPath) {
		return "", fmt.Errorf("%w: path must be absolute", ErrStickyDiskInvalid)
	}
	if mountPath == "/" {
		return "", fmt.Errorf("%w: root path cannot be sticky", ErrStickyDiskInvalid)
	}
	if strings.HasPrefix(mountPath, "/proc") || strings.HasPrefix(mountPath, "/sys") || strings.HasPrefix(mountPath, "/dev") || strings.HasPrefix(mountPath, "/run") {
		return "", fmt.Errorf("%w: path is not mountable", ErrStickyDiskInvalid)
	}
	if len([]byte(mountPath)) > stickyDiskMaxPathBytes {
		return "", fmt.Errorf("%w: path exceeds %d bytes", ErrStickyDiskInvalid, stickyDiskMaxPathBytes)
	}
	if strings.ContainsAny(mountPath, "\x00\r\n") {
		return "", fmt.Errorf("%w: path contains control characters", ErrStickyDiskInvalid)
	}
	return mountPath, nil
}

func resolveStickyDiskPath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%w: path is required", ErrStickyDiskInvalid)
	}
	switch {
	case raw == "~":
		return normalizeStickyDiskPath(stickyDiskRunnerHome)
	case strings.HasPrefix(raw, "~/"):
		return normalizeStickyDiskPath(filepath.Join(stickyDiskRunnerHome, strings.TrimPrefix(raw, "~/")))
	case filepath.IsAbs(raw):
		return normalizeStickyDiskPath(raw)
	default:
		return normalizeStickyDiskPath(filepath.Join(stickyDiskWorkspaceRoot, raw))
	}
}

func stickyDiskAttributes(identity StickyDiskIdentity, key string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("github.stickydisk.key_hash", stickyDiskKeyHash(key)),
		attribute.String("github.allocation_id", identity.AllocationID.String()),
		attribute.String("execution.id", identity.ExecutionID.String()),
		attribute.String("attempt.id", identity.AttemptID.String()),
		attribute.Int64("github.installation_id", identity.Installation),
		attribute.Int64("github.repository_id", identity.RepositoryID),
		attribute.Int64("github.job_id", identity.GitHubJobID),
		attribute.String("github.runner_name", identity.RunnerName),
	}
}

func stickyDiskKeyHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func parseStickyDiskGeneration(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}
