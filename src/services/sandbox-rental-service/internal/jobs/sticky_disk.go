package jobs

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/verself/sandbox-rental-service/internal/store"
	vmorchestrator "github.com/verself/vm-orchestrator"
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
	stickyDiskRunnerWorkRoot     = "/workspace"
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
	OrgID              uint64
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

	row, err := r.service.storeQueries().GetGitHubExecutionIdentity(ctx, store.GetGitHubExecutionIdentityParams{
		ExecutionID: &executionID,
		AttemptID:   &attemptID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return StickyDiskIdentity{}, unauthorizedErr
	}
	if err != nil {
		return StickyDiskIdentity{}, err
	}
	return StickyDiskIdentity{
		ExecutionID:        executionID,
		AttemptID:          attemptID,
		AllocationID:       row.AllocationID,
		OrgID:              orgIDFromDB(row.OrgID),
		Installation:       row.ProviderInstallationID,
		RepositoryID:       row.ProviderRepositoryID,
		RepositoryFullName: row.RepositoryFullName,
		GitHubJobID:        row.ProviderJobID,
		RunnerName:         row.RunnerName,
	}, nil
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
	row, err := r.service.storeQueries().RequestStickyDiskCommit(ctx, store.RequestStickyDiskCommitParams{
		SaveState:   stickyDiskStateRequested,
		RequestedAt: pgTime(now),
		AttemptID:   identity.AttemptID,
		KeyHash:     keyHash,
		MountPath:   mountPath,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		err = fmt.Errorf("%w: sticky disk %s at %s was not provisioned before VM boot", ErrStickyDiskInvalid, keyHash, mountPath)
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return StickyDiskSave{}, err
	}
	save := StickyDiskSave{
		Identity:    identity,
		CommitID:    row.MountID,
		Key:         key,
		MountPath:   mountPath,
		RequestedAt: timeFromPG(row.RequestedAt),
	}
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
	rows, err := r.service.storeQueries().ListPendingStickyDiskCommits(ctx, store.ListPendingStickyDiskCommitsParams{
		AttemptID: attemptID,
		SaveState: stickyDiskStateRequested,
	})
	if err != nil {
		return nil, err
	}
	out := make([]stickyDiskPendingCommit, 0, len(rows))
	for _, row := range rows {
		out = append(out, stickyDiskPendingCommit{
			MountID:         row.MountID,
			MountName:       row.MountName,
			Key:             row.Key,
			KeyHash:         row.KeyHash,
			MountPath:       row.MountPath,
			BaseGeneration:  row.BaseGeneration,
			TargetSourceRef: row.TargetSourceRef,
			Identity: StickyDiskIdentity{
				ExecutionID:        row.ExecutionID,
				AttemptID:          row.AttemptID,
				AllocationID:       row.AllocationID,
				OrgID:              orgIDFromDB(row.OrgID),
				Installation:       row.ProviderInstallationID,
				RepositoryID:       row.ProviderRepositoryID,
				RepositoryFullName: row.RepositoryFullName,
				GitHubJobID:        row.ProviderJobID,
				RunnerName:         row.RunnerName,
			},
		})
	}
	return out, nil
}

func (r *GitHubRunner) commitStickyDiskMount(ctx context.Context, item executionWorkItem, leaseID string, commit stickyDiskPendingCommit) error {
	ctx, span := tracer.Start(ctx, "github.stickydisk.commit_zfs")
	defer span.End()
	attrs := append(stickyDiskAttributes(commit.Identity, commit.Key),
		attribute.String("github.stickydisk.mount_id", commit.MountID.String()),
		attribute.String("github.stickydisk.mount_name", commit.MountName),
		attribute.String("github.stickydisk.mount_path", commit.MountPath),
		attribute.String("github.stickydisk.target_source_ref", commit.TargetSourceRef),
		attribute.String("lease.id", leaseID),
	)
	span.SetAttributes(attrs...)
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
	rows, err := r.service.storeQueries().MarkStickyDiskCommitRunning(ctx, store.MarkStickyDiskCommitRunningParams{
		ToState:   stickyDiskStateRunning,
		StartedAt: pgTime(time.Now().UTC()),
		MountID:   mountID,
		FromState: stickyDiskStateRequested,
	})
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

func (r *GitHubRunner) markStickyDiskCommitFinished(ctx context.Context, mountID uuid.UUID, state, reason string, generation int64, snapshot string) error {
	if len(reason) > 2048 {
		reason = reason[:2048]
	}
	return r.service.storeQueries().MarkStickyDiskCommitFinished(ctx, store.MarkStickyDiskCommitFinishedParams{
		SaveState:           state,
		FailureReason:       reason,
		CommittedGeneration: generation,
		CommittedSnapshot:   snapshot,
		CompletedAt:         pgTime(time.Now().UTC()),
		MountID:             mountID,
	})
}

func (r *GitHubRunner) promoteStickyDiskGeneration(ctx context.Context, commit stickyDiskPendingCommit, generation int64) error {
	tx, err := r.service.PGX.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := store.New(tx)
	current, err := qtx.LockStickyDiskGeneration(ctx, store.LockStickyDiskGenerationParams{
		ProviderInstallationID: commit.Identity.Installation,
		ProviderRepositoryID:   commit.Identity.RepositoryID,
		KeyHash:                commit.KeyHash,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		current = 0
	} else if err != nil {
		return err
	}
	if current != commit.BaseGeneration {
		return fmt.Errorf("sticky disk generation moved from %d to %d while execution was running", commit.BaseGeneration, current)
	}
	now := time.Now().UTC()
	if err := qtx.UpsertStickyDiskGeneration(ctx, store.UpsertStickyDiskGenerationParams{
		ProviderInstallationID: commit.Identity.Installation,
		ProviderRepositoryID:   commit.Identity.RepositoryID,
		KeyHash:                commit.KeyHash,
		Key:                    commit.Key,
		CurrentGeneration:      generation,
		CurrentSourceRef:       commit.TargetSourceRef,
		UpdatedAt:              pgTime(now),
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *GitHubRunner) currentStickyDiskGeneration(ctx context.Context, installationID, repositoryID int64, key, keyHash string) (int64, string, error) {
	row, err := r.service.storeQueries().GetCurrentStickyDiskGeneration(ctx, store.GetCurrentStickyDiskGenerationParams{
		ProviderInstallationID: installationID,
		ProviderRepositoryID:   repositoryID,
		KeyHash:                keyHash,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, stickyDiskEmptySourceRef, nil
	}
	if err != nil {
		return 0, "", err
	}
	generation := row.CurrentGeneration
	sourceRef := row.CurrentSourceRef
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

func resolveStickyDiskPath(raw, repositoryFullName string) (string, error) {
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
		workspaceRoot, err := githubActionsWorkspaceRoot(repositoryFullName)
		if err != nil {
			return "", err
		}
		return normalizeStickyDiskPath(filepath.Join(workspaceRoot, raw))
	}
}

func githubActionsWorkspaceRoot(repositoryFullName string) (string, error) {
	_, repo, ok := strings.Cut(strings.TrimSpace(repositoryFullName), "/")
	repo = strings.TrimSpace(repo)
	if !ok || repo == "" || repo == "." || repo == ".." || strings.ContainsAny(repo, `/\`) {
		return "", fmt.Errorf("%w: repository full name is required for relative sticky disk paths", ErrStickyDiskInvalid)
	}
	// The runner starts in /opt/actions-runner with _work -> /workspace, so GitHub sets
	// GITHUB_WORKSPACE to /opt/actions-runner/_work/<repo>/<repo>.
	return filepath.Join(stickyDiskRunnerWorkRoot, repo, repo), nil
}

func stickyDiskAttributes(identity StickyDiskIdentity, key string) []attribute.KeyValue {
	return []attribute.KeyValue{
		traceOrgID(identity.OrgID),
		attribute.String("github.stickydisk.key_hash", stickyDiskKeyHash(key)),
		attribute.String("github.allocation_id", identity.AllocationID.String()),
		attribute.String("execution.id", identity.ExecutionID.String()),
		attribute.String("attempt.id", identity.AttemptID.String()),
		attribute.Int64("github.installation_id", identity.Installation),
		attribute.Int64("github.repository_id", identity.RepositoryID),
		attribute.String("github.repository", identity.RepositoryFullName),
		attribute.Int64("github.job_id", identity.GitHubJobID),
		attribute.String("github.runner_name", identity.RunnerName),
	}
}

func stickyDiskKeyHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
