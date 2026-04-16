package jobs

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const (
	defaultStickyDiskDir      = "/var/lib/forge-metal/sandbox-rental/stickydisks"
	stickyDiskMaxKeyBytes     = 512
	stickyDiskArchiveFilename = "archive.tgz"
	stickyDiskMetaFilename    = "meta.json"
)

var (
	ErrStickyDiskUnauthorized = errors.New("sticky disk request is not authorized")
	ErrStickyDiskMissing      = errors.New("sticky disk generation is missing")
	ErrStickyDiskInvalid      = errors.New("sticky disk request is invalid")
)

type StickyDiskIdentity struct {
	ExecutionID  uuid.UUID
	AttemptID    uuid.UUID
	AllocationID uuid.UUID
	Installation int64
	RepositoryID int64
	GitHubJobID  int64
	RunnerName   string
}

type StickyDiskRestore struct {
	Identity    StickyDiskIdentity
	Generation  int64
	ArchivePath string
	SizeBytes   int64
	SHA256      string
}

type StickyDiskCommit struct {
	Identity   StickyDiskIdentity
	Generation int64
	SizeBytes  int64
	SHA256     string
}

type stickyDiskMetadata struct {
	Key          string    `json:"key"`
	Generation   int64     `json:"generation"`
	SizeBytes    int64     `json:"size_bytes"`
	SHA256       string    `json:"sha256"`
	ExecutionID  string    `json:"execution_id"`
	AttemptID    string    `json:"attempt_id"`
	AllocationID string    `json:"allocation_id"`
	GitHubJobID  int64     `json:"github_job_id"`
	RunnerName   string    `json:"runner_name"`
	CommittedAt  time.Time `json:"committed_at"`
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
	expected := r.deriveStickyDiskToken(execUUID, attemptUUID)
	if bearer == "" || !hmac.Equal([]byte(bearer), []byte(expected)) {
		return StickyDiskIdentity{}, ErrStickyDiskUnauthorized
	}

	var identity StickyDiskIdentity
	err = r.service.PGX.QueryRow(ctx, `SELECT
		a.allocation_id,
		a.installation_id,
		a.repository_id,
		COALESCE(b.github_job_id, a.requested_for_github_job_id),
		a.runner_name
		FROM github_runner_allocations a
		LEFT JOIN github_runner_job_bindings b ON b.allocation_id = a.allocation_id
		WHERE a.execution_id = $1 AND a.attempt_id = $2`,
		execUUID, attemptUUID).Scan(&identity.AllocationID, &identity.Installation, &identity.RepositoryID, &identity.GitHubJobID, &identity.RunnerName)
	if errors.Is(err, pgx.ErrNoRows) {
		return StickyDiskIdentity{}, ErrStickyDiskUnauthorized
	}
	if err != nil {
		return StickyDiskIdentity{}, err
	}
	identity.ExecutionID = execUUID
	identity.AttemptID = attemptUUID
	return identity, nil
}

func (r *GitHubRunner) RestoreStickyDisk(ctx context.Context, identity StickyDiskIdentity, key string) (StickyDiskRestore, error) {
	ctx, span := tracer.Start(ctx, "github.stickydisk.restore")
	defer span.End()
	key, err := normalizeStickyDiskKey(key)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return StickyDiskRestore{}, err
	}
	span.SetAttributes(stickyDiskAttributes(identity, key)...)

	meta, archivePath, err := r.readStickyDiskMetadata(identity, key)
	if errors.Is(err, os.ErrNotExist) {
		span.SetAttributes(attribute.Bool("github.stickydisk.hit", false))
		return StickyDiskRestore{}, ErrStickyDiskMissing
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return StickyDiskRestore{}, err
	}
	span.SetAttributes(
		attribute.Bool("github.stickydisk.hit", true),
		attribute.Int64("github.stickydisk.generation", meta.Generation),
		attribute.Int64("github.stickydisk.size_bytes", meta.SizeBytes),
		attribute.String("github.stickydisk.sha256", meta.SHA256),
	)
	return StickyDiskRestore{
		Identity:    identity,
		Generation:  meta.Generation,
		ArchivePath: archivePath,
		SizeBytes:   meta.SizeBytes,
		SHA256:      meta.SHA256,
	}, nil
}

func (r *GitHubRunner) CommitStickyDisk(ctx context.Context, identity StickyDiskIdentity, key string, archive io.Reader) (StickyDiskCommit, error) {
	ctx, span := tracer.Start(ctx, "github.stickydisk.commit")
	defer span.End()
	key, err := normalizeStickyDiskKey(key)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return StickyDiskCommit{}, err
	}
	span.SetAttributes(stickyDiskAttributes(identity, key)...)
	dir := r.stickyDiskDir(identity, key)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return StickyDiskCommit{}, err
	}

	prev, _, err := r.readStickyDiskMetadata(identity, key)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return StickyDiskCommit{}, err
	}
	generation := prev.Generation + 1
	tmp, err := os.CreateTemp(dir, ".archive-*.tmp")
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return StickyDiskCommit{}, err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	hash := sha256.New()
	written, copyErr := io.Copy(tmp, io.TeeReader(archive, hash))
	closeErr := tmp.Close()
	if copyErr != nil {
		span.RecordError(copyErr)
		span.SetStatus(codes.Error, copyErr.Error())
		return StickyDiskCommit{}, copyErr
	}
	if closeErr != nil {
		span.RecordError(closeErr)
		span.SetStatus(codes.Error, closeErr.Error())
		return StickyDiskCommit{}, closeErr
	}
	if written == 0 {
		err := fmt.Errorf("%w: empty archive", ErrStickyDiskInvalid)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return StickyDiskCommit{}, err
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	archivePath := filepath.Join(dir, stickyDiskArchiveFilename)
	if err := os.Rename(tmpPath, archivePath); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return StickyDiskCommit{}, err
	}
	meta := stickyDiskMetadata{
		Key:          key,
		Generation:   generation,
		SizeBytes:    written,
		SHA256:       sum,
		ExecutionID:  identity.ExecutionID.String(),
		AttemptID:    identity.AttemptID.String(),
		AllocationID: identity.AllocationID.String(),
		GitHubJobID:  identity.GitHubJobID,
		RunnerName:   identity.RunnerName,
		CommittedAt:  time.Now().UTC(),
	}
	if err := writeStickyDiskMetadata(filepath.Join(dir, stickyDiskMetaFilename), meta); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return StickyDiskCommit{}, err
	}
	span.SetAttributes(
		attribute.Int64("github.stickydisk.generation", generation),
		attribute.Int64("github.stickydisk.size_bytes", written),
		attribute.String("github.stickydisk.sha256", sum),
	)
	return StickyDiskCommit{
		Identity:   identity,
		Generation: generation,
		SizeBytes:  written,
		SHA256:     sum,
	}, nil
}

func (r *GitHubRunner) stickyDiskRoot() string {
	root := strings.TrimSpace(r.service.StickyDiskDir)
	if root == "" {
		return defaultStickyDiskDir
	}
	return root
}

func (r *GitHubRunner) stickyDiskDir(identity StickyDiskIdentity, key string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s", identity.Installation, identity.RepositoryID, key)))
	return filepath.Join(r.stickyDiskRoot(), hex.EncodeToString(sum[:]))
}

func (r *GitHubRunner) readStickyDiskMetadata(identity StickyDiskIdentity, key string) (stickyDiskMetadata, string, error) {
	dir := r.stickyDiskDir(identity, key)
	data, err := os.ReadFile(filepath.Join(dir, stickyDiskMetaFilename))
	if err != nil {
		return stickyDiskMetadata{}, "", err
	}
	var meta stickyDiskMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return stickyDiskMetadata{}, "", err
	}
	if meta.Key != key || meta.Generation <= 0 || meta.SHA256 == "" || meta.SizeBytes <= 0 {
		return stickyDiskMetadata{}, "", fmt.Errorf("%w: invalid metadata", ErrStickyDiskInvalid)
	}
	archivePath := filepath.Join(dir, stickyDiskArchiveFilename)
	if _, err := os.Stat(archivePath); err != nil {
		return stickyDiskMetadata{}, "", err
	}
	return meta, archivePath, nil
}

func writeStickyDiskMetadata(path string, meta stickyDiskMetadata) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".meta-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
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
