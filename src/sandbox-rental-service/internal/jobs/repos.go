package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	RepoStateImporting      = "importing"
	RepoStateActionRequired = "action_required"
	RepoStateReady          = "ready"
	RepoStateFailed         = "failed"
	RepoStateArchived       = "archived"

	CompatibilityStatusCompatible     = "compatible"
	CompatibilityStatusActionRequired = "action_required"
)

var ErrRepoMissing = errors.New("sandbox-rental: repo not found")

type RepoRecord struct {
	RepoID               uuid.UUID       `json:"repo_id"`
	OrgID                uint64          `json:"org_id"`
	IntegrationID        *uuid.UUID      `json:"integration_id,omitempty"`
	Provider             string          `json:"provider"`
	ProviderHost         string          `json:"provider_host"`
	ProviderRepoID       string          `json:"provider_repo_id"`
	Owner                string          `json:"owner"`
	Name                 string          `json:"name"`
	FullName             string          `json:"full_name"`
	CloneURL             string          `json:"clone_url"`
	DefaultBranch        string          `json:"default_branch"`
	State                string          `json:"state"`
	CompatibilityStatus  string          `json:"compatibility_status"`
	CompatibilitySummary json.RawMessage `json:"compatibility_summary,omitempty"`
	LastScannedSHA       string          `json:"last_scanned_sha,omitempty"`
	LastError            string          `json:"last_error,omitempty"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
	ArchivedAt           *time.Time      `json:"archived_at,omitempty"`
}

type CreateRepoRequest struct {
	OrgID                uint64          `json:"org_id"`
	IntegrationID        *uuid.UUID      `json:"integration_id,omitempty"`
	Provider             string          `json:"provider"`
	ProviderHost         string          `json:"provider_host"`
	ProviderRepoID       string          `json:"provider_repo_id"`
	Owner                string          `json:"owner"`
	Name                 string          `json:"name"`
	FullName             string          `json:"full_name,omitempty"`
	CloneURL             string          `json:"clone_url"`
	DefaultBranch        string          `json:"default_branch,omitempty"`
	State                string          `json:"state,omitempty"`
	CompatibilityStatus  string          `json:"compatibility_status,omitempty"`
	CompatibilitySummary json.RawMessage `json:"compatibility_summary,omitempty"`
	LastScannedSHA       string          `json:"last_scanned_sha,omitempty"`
}

type RepoCompatibilityResult struct {
	Compatible           bool            `json:"compatible"`
	CompatibilityStatus  string          `json:"compatibility_status,omitempty"`
	CompatibilitySummary json.RawMessage `json:"compatibility_summary,omitempty"`
	LastScannedSHA       string          `json:"last_scanned_sha,omitempty"`
}

func (s *Service) CreateRepo(ctx context.Context, req CreateRepoRequest) (*RepoRecord, error) {
	req, err := normalizeCreateRepoRequest(req)
	if err != nil {
		return nil, err
	}

	repoID := uuid.New()
	now := time.Now().UTC()
	summary := normalizedSummary(req.CompatibilitySummary)

	if _, err := s.PG.ExecContext(ctx, `
			INSERT INTO repos (
				repo_id, org_id, integration_id, provider, provider_host, provider_repo_id, owner, name, full_name,
				clone_url, default_branch, state,
				compatibility_status, compatibility_summary, last_scanned_sha,
				created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9,
				$10, $11, $12,
				$13, $14::jsonb, $15,
				$16, $16
			)
		`, repoID, int64(req.OrgID), req.IntegrationID, req.Provider, req.ProviderHost, req.ProviderRepoID, req.Owner, req.Name, req.FullName,
		req.CloneURL, req.DefaultBranch, req.State,
		req.CompatibilityStatus, string(summary), req.LastScannedSHA, now); err != nil {
		return nil, fmt.Errorf("insert repo: %w", err)
	}

	return s.GetRepo(ctx, req.OrgID, repoID)
}

func (s *Service) GetRepo(ctx context.Context, orgID uint64, repoID uuid.UUID) (*RepoRecord, error) {
	row := s.PG.QueryRowContext(ctx, `
		SELECT
			repo_id,
			org_id,
			COALESCE(integration_id::text, ''),
			provider,
			provider_host,
			provider_repo_id,
			owner,
			name,
			full_name,
			clone_url,
			default_branch,
			state,
			compatibility_status,
			compatibility_summary,
			last_scanned_sha,
			last_error,
			created_at,
			updated_at,
			archived_at
		FROM repos
		WHERE repo_id = $1 AND org_id = $2
	`, repoID, int64(orgID))

	record, err := scanRepoRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRepoMissing
		}
		return nil, fmt.Errorf("scan repo: %w", err)
	}
	return record, nil
}

func (s *Service) ListRepos(ctx context.Context, orgID uint64) ([]RepoRecord, error) {
	rows, err := s.PG.QueryContext(ctx, `
		SELECT
			repo_id,
			org_id,
			COALESCE(integration_id::text, ''),
			provider,
			provider_host,
			provider_repo_id,
			owner,
			name,
			full_name,
			clone_url,
			default_branch,
			state,
			compatibility_status,
			compatibility_summary,
			last_scanned_sha,
			last_error,
			created_at,
			updated_at,
			archived_at
		FROM repos
		WHERE org_id = $1
		ORDER BY updated_at DESC, created_at DESC
	`, int64(orgID))
	if err != nil {
		return nil, fmt.Errorf("query repos: %w", err)
	}
	defer rows.Close()

	var repos []RepoRecord
	for rows.Next() {
		record, err := scanRepoRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan repo row: %w", err)
		}
		repos = append(repos, *record)
	}
	return repos, rows.Err()
}

func (s *Service) RecordRepoCompatibility(ctx context.Context, repoID uuid.UUID, result RepoCompatibilityResult) (*RepoRecord, error) {
	result = normalizeRepoCompatibilityResult(result)
	now := time.Now().UTC()
	summary := normalizedSummary(result.CompatibilitySummary)
	tx, err := s.PG.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		currentState string
		currentError string
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT state, last_error
		FROM repos
		WHERE repo_id = $1
		FOR UPDATE
	`, repoID).Scan(&currentState, &currentError); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRepoMissing
		}
		return nil, fmt.Errorf("lock repo for compatibility update: %w", err)
	}

	targetState := nextRepoStateForCompatibility(currentState, result.Compatible)
	lastError := repoLastErrorAfterCompatibility(currentState, currentError, targetState, result.Compatible)

	res, err := tx.ExecContext(ctx, `
		UPDATE repos
		SET compatibility_status = $2,
		    compatibility_summary = $3::jsonb,
		    last_scanned_sha = $4,
		    state = $5,
		    last_error = $6,
		    updated_at = $7
		WHERE repo_id = $1
	`, repoID, result.CompatibilityStatus, string(summary), strings.TrimSpace(result.LastScannedSHA), targetState, lastError, now)
	if err != nil {
		return nil, fmt.Errorf("update repo compatibility: %w", err)
	}
	if err := ensureRowsAffected(res, ErrRepoMissing); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return s.getRepoByID(ctx, repoID)
}

func (s *Service) UpdateRepoImportMetadata(ctx context.Context, repoID uuid.UUID, req ImportRepoRequest) error {
	req, err := normalizeImportRepoRequest(req)
	if err != nil {
		return err
	}
	res, err := s.PG.ExecContext(ctx, `
			UPDATE repos
			SET integration_id = COALESCE($2, integration_id),
			    provider = $3,
			    provider_host = $4,
			    provider_repo_id = $5,
			    owner = $6,
			    name = $7,
			    full_name = $8,
			    clone_url = $9,
			    default_branch = $10,
			    updated_at = $11
			WHERE repo_id = $1
		`, repoID, req.IntegrationID, req.Provider, req.ProviderHost, req.ProviderRepoID, req.Owner, req.Name, req.FullName, req.CloneURL, req.DefaultBranch, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("update repo import metadata: %w", err)
	}
	return ensureRowsAffected(res, ErrRepoMissing)
}

func (s *Service) getRepoByID(ctx context.Context, repoID uuid.UUID) (*RepoRecord, error) {
	row := s.PG.QueryRowContext(ctx, `
		SELECT
			repo_id,
			org_id,
			COALESCE(integration_id::text, ''),
			provider,
			provider_host,
			provider_repo_id,
			owner,
			name,
			full_name,
			clone_url,
			default_branch,
			state,
			compatibility_status,
			compatibility_summary,
			last_scanned_sha,
			last_error,
			created_at,
			updated_at,
			archived_at
		FROM repos
		WHERE repo_id = $1
	`, repoID)
	record, err := scanRepoRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRepoMissing
		}
		return nil, err
	}
	return record, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRepoRow(scanner rowScanner) (*RepoRecord, error) {
	var (
		record        RepoRecord
		integrationID string
		archivedAt    sql.NullTime
		summaryBytes  []byte
	)
	if err := scanner.Scan(
		&record.RepoID,
		&record.OrgID,
		&integrationID,
		&record.Provider,
		&record.ProviderHost,
		&record.ProviderRepoID,
		&record.Owner,
		&record.Name,
		&record.FullName,
		&record.CloneURL,
		&record.DefaultBranch,
		&record.State,
		&record.CompatibilityStatus,
		&summaryBytes,
		&record.LastScannedSHA,
		&record.LastError,
		&record.CreatedAt,
		&record.UpdatedAt,
		&archivedAt,
	); err != nil {
		return nil, err
	}
	record.CompatibilitySummary = append(json.RawMessage(nil), summaryBytes...)
	record.IntegrationID = uuidPointer(integrationID)
	if archivedAt.Valid {
		record.ArchivedAt = &archivedAt.Time
	}
	return &record, nil
}

func scanRepoRows(rows *sql.Rows) (*RepoRecord, error) {
	return scanRepoRow(rows)
}

func normalizeCreateRepoRequest(req CreateRepoRequest) (CreateRepoRequest, error) {
	req.Provider = strings.TrimSpace(req.Provider)
	req.ProviderHost = strings.TrimSpace(strings.ToLower(req.ProviderHost))
	req.ProviderRepoID = strings.TrimSpace(req.ProviderRepoID)
	req.Owner = strings.TrimSpace(req.Owner)
	req.Name = strings.TrimSpace(req.Name)
	req.FullName = strings.TrimSpace(req.FullName)
	req.CloneURL = strings.TrimSpace(req.CloneURL)
	req.DefaultBranch = strings.TrimSpace(req.DefaultBranch)
	req.State = strings.TrimSpace(req.State)
	req.CompatibilityStatus = strings.TrimSpace(req.CompatibilityStatus)
	req.LastScannedSHA = strings.TrimSpace(req.LastScannedSHA)

	if req.OrgID == 0 {
		return CreateRepoRequest{}, fmt.Errorf("org_id is required")
	}
	if req.Provider == "" {
		return CreateRepoRequest{}, fmt.Errorf("provider is required")
	}
	cloneProviderHost := providerHostFromCloneURL(req.CloneURL)
	if req.ProviderHost == "" {
		req.ProviderHost = cloneProviderHost
	}
	if req.ProviderHost == "" {
		return CreateRepoRequest{}, fmt.Errorf("provider_host is required")
	}
	if cloneProviderHost != "" && req.ProviderHost != cloneProviderHost {
		return CreateRepoRequest{}, fmt.Errorf("provider_host %q must match clone_url host %q", req.ProviderHost, cloneProviderHost)
	}
	if req.ProviderRepoID == "" {
		return CreateRepoRequest{}, fmt.Errorf("provider_repo_id is required")
	}
	if req.Owner == "" {
		return CreateRepoRequest{}, fmt.Errorf("owner is required")
	}
	if req.Name == "" {
		return CreateRepoRequest{}, fmt.Errorf("name is required")
	}
	if req.FullName == "" {
		req.FullName = req.Owner + "/" + req.Name
	}
	if req.CloneURL == "" {
		return CreateRepoRequest{}, fmt.Errorf("clone_url is required")
	}
	if err := validateGitCloneURLField("clone_url", req.CloneURL); err != nil {
		return CreateRepoRequest{}, err
	}
	if req.DefaultBranch == "" {
		req.DefaultBranch = defaultBranchName
	}
	if req.State == "" {
		req.State = RepoStateImporting
	}
	switch req.State {
	case RepoStateImporting, RepoStateActionRequired, RepoStateReady, RepoStateFailed, RepoStateArchived:
	default:
		return CreateRepoRequest{}, fmt.Errorf("unsupported repo state %q", req.State)
	}
	return req, nil
}

func normalizeRepoCompatibilityResult(result RepoCompatibilityResult) RepoCompatibilityResult {
	result.CompatibilityStatus = strings.TrimSpace(result.CompatibilityStatus)
	result.LastScannedSHA = strings.TrimSpace(result.LastScannedSHA)
	if result.Compatible {
		if result.CompatibilityStatus == "" {
			result.CompatibilityStatus = CompatibilityStatusCompatible
		}
		return result
	}
	if result.CompatibilityStatus == "" {
		result.CompatibilityStatus = CompatibilityStatusActionRequired
	}
	return result
}

func normalizedSummary(summary json.RawMessage) json.RawMessage {
	summary = json.RawMessage(strings.TrimSpace(string(summary)))
	if len(summary) == 0 {
		return json.RawMessage(`{}`)
	}
	if !json.Valid(summary) {
		return json.RawMessage(`{}`)
	}
	return summary
}

func uuidPointer(value string) *uuid.UUID {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := uuid.Parse(value)
	if err != nil {
		return nil
	}
	return &parsed
}

func ensureRowsAffected(res sql.Result, notFound error) error {
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return notFound
	}
	return nil
}

func nextRepoStateForCompatibility(currentState string, compatible bool) string {
	if currentState == RepoStateArchived {
		return RepoStateArchived
	}
	if !compatible {
		return RepoStateActionRequired
	}
	return RepoStateReady
}

func repoLastErrorAfterCompatibility(_ string, _ string, _ string, compatible bool) string {
	if compatible {
		return ""
	}
	return ""
}
