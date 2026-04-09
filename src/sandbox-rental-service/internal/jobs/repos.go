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
	RunnerProfileForgeMetal = "forge-metal"

	RepoStateImporting           = "importing"
	RepoStateActionRequired      = "action_required"
	RepoStateWaitingForBootstrap = "waiting_for_bootstrap"
	RepoStatePreparing           = "preparing"
	RepoStateReady               = "ready"
	RepoStateDegraded            = "degraded"
	RepoStateFailed              = "failed"
	RepoStateArchived            = "archived"

	GenerationStateQueued     = "queued"
	GenerationStateBuilding   = "building"
	GenerationStateSanitizing = "sanitizing"
	GenerationStateReady      = "ready"
	GenerationStateFailed     = "failed"
	GenerationStateSuperseded = "superseded"

	CompatibilityStatusCompatible     = "compatible"
	CompatibilityStatusActionRequired = "action_required"

	GenerationTriggerBootstrap         = "bootstrap"
	GenerationTriggerDefaultBranchPush = "default_branch_push"
	GenerationTriggerManualRefresh     = "manual_refresh"
)

var (
	ErrRepoMissing             = errors.New("sandbox-rental: repo not found")
	ErrGoldenGenerationMissing = errors.New("sandbox-rental: golden generation not found")
)

type RepoRecord struct {
	RepoID                   uuid.UUID       `json:"repo_id"`
	OrgID                    uint64          `json:"org_id"`
	Provider                 string          `json:"provider"`
	ProviderRepoID           string          `json:"provider_repo_id"`
	Owner                    string          `json:"owner"`
	Name                     string          `json:"name"`
	FullName                 string          `json:"full_name"`
	CloneURL                 string          `json:"clone_url"`
	DefaultBranch            string          `json:"default_branch"`
	RunnerProfileSlug        string          `json:"runner_profile_slug"`
	State                    string          `json:"state"`
	CompatibilityStatus      string          `json:"compatibility_status"`
	CompatibilitySummary     json.RawMessage `json:"compatibility_summary,omitempty"`
	LastScannedSHA           string          `json:"last_scanned_sha,omitempty"`
	ActiveGoldenGenerationID *uuid.UUID      `json:"active_golden_generation_id,omitempty"`
	LastReadySHA             string          `json:"last_ready_sha,omitempty"`
	LastError                string          `json:"last_error,omitempty"`
	CreatedAt                time.Time       `json:"created_at"`
	UpdatedAt                time.Time       `json:"updated_at"`
	ArchivedAt               *time.Time      `json:"archived_at,omitempty"`
}

type CreateRepoRequest struct {
	OrgID                uint64          `json:"org_id"`
	Provider             string          `json:"provider"`
	ProviderRepoID       string          `json:"provider_repo_id"`
	Owner                string          `json:"owner"`
	Name                 string          `json:"name"`
	FullName             string          `json:"full_name,omitempty"`
	CloneURL             string          `json:"clone_url"`
	DefaultBranch        string          `json:"default_branch,omitempty"`
	RunnerProfileSlug    string          `json:"runner_profile_slug,omitempty"`
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

type GoldenGenerationRecord struct {
	GoldenGenerationID uuid.UUID  `json:"golden_generation_id"`
	RepoID             uuid.UUID  `json:"repo_id"`
	RunnerProfileSlug  string     `json:"runner_profile_slug"`
	SourceRef          string     `json:"source_ref"`
	SourceSHA          string     `json:"source_sha"`
	State              string     `json:"state"`
	TriggerReason      string     `json:"trigger_reason"`
	ExecutionID        *uuid.UUID `json:"execution_id,omitempty"`
	AttemptID          *uuid.UUID `json:"attempt_id,omitempty"`
	OrchestratorJobID  string     `json:"orchestrator_job_id,omitempty"`
	SnapshotRef        string     `json:"snapshot_ref,omitempty"`
	ActivatedAt        *time.Time `json:"activated_at,omitempty"`
	SupersededAt       *time.Time `json:"superseded_at,omitempty"`
	FailureReason      string     `json:"failure_reason,omitempty"`
	FailureDetail      string     `json:"failure_detail,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type CreateGoldenGenerationRequest struct {
	RunnerProfileSlug string `json:"runner_profile_slug,omitempty"`
	SourceRef         string `json:"source_ref"`
	SourceSHA         string `json:"source_sha"`
	TriggerReason     string `json:"trigger_reason,omitempty"`
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
			repo_id, org_id, provider, provider_repo_id, owner, name, full_name,
			clone_url, default_branch, runner_profile_slug, state,
			compatibility_status, compatibility_summary, last_scanned_sha,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11,
			$12, $13::jsonb, $14,
			$15, $15
		)
	`, repoID, int64(req.OrgID), req.Provider, req.ProviderRepoID, req.Owner, req.Name, req.FullName,
		req.CloneURL, req.DefaultBranch, req.RunnerProfileSlug, req.State,
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
			provider,
			provider_repo_id,
			owner,
			name,
			full_name,
			clone_url,
			default_branch,
			runner_profile_slug,
			state,
			compatibility_status,
			compatibility_summary,
			last_scanned_sha,
			COALESCE(active_golden_generation_id::text, ''),
			last_ready_sha,
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
			provider,
			provider_repo_id,
			owner,
			name,
			full_name,
			clone_url,
			default_branch,
			runner_profile_slug,
			state,
			compatibility_status,
			compatibility_summary,
			last_scanned_sha,
			COALESCE(active_golden_generation_id::text, ''),
			last_ready_sha,
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
	targetState := RepoStateActionRequired
	if result.Compatible {
		targetState = RepoStateWaitingForBootstrap
	}

	res, err := s.PG.ExecContext(ctx, `
		UPDATE repos
		SET compatibility_status = $2,
		    compatibility_summary = $3::jsonb,
		    last_scanned_sha = $4,
		    state = $5,
		    updated_at = $6
		WHERE repo_id = $1
	`, repoID, result.CompatibilityStatus, string(summary), strings.TrimSpace(result.LastScannedSHA), targetState, now)
	if err != nil {
		return nil, fmt.Errorf("update repo compatibility: %w", err)
	}
	if err := ensureRowsAffected(res, ErrRepoMissing); err != nil {
		return nil, err
	}

	return s.getRepoByID(ctx, repoID)
}

func (s *Service) CreateGoldenGeneration(ctx context.Context, repoID uuid.UUID, req CreateGoldenGenerationRequest) (*GoldenGenerationRecord, error) {
	req, err := normalizeCreateGoldenGenerationRequest(req)
	if err != nil {
		return nil, err
	}

	generationID := uuid.New()
	now := time.Now().UTC()
	tx, err := s.PG.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE repos
		SET state = $2,
		    updated_at = $3
		WHERE repo_id = $1
	`, repoID, RepoStatePreparing, now)
	if err != nil {
		return nil, fmt.Errorf("mark repo preparing: %w", err)
	}
	if err := ensureRowsAffected(res, ErrRepoMissing); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO golden_generations (
			golden_generation_id, repo_id, runner_profile_slug, source_ref, source_sha,
			state, trigger_reason, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $8
		)
	`, generationID, repoID, req.RunnerProfileSlug, req.SourceRef, req.SourceSHA,
		GenerationStateQueued, req.TriggerReason, now); err != nil {
		return nil, fmt.Errorf("insert golden generation: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return s.GetGoldenGeneration(ctx, repoID, generationID)
}

func (s *Service) GetGoldenGeneration(ctx context.Context, repoID, generationID uuid.UUID) (*GoldenGenerationRecord, error) {
	row := s.PG.QueryRowContext(ctx, `
		SELECT
			golden_generation_id,
			repo_id,
			runner_profile_slug,
			source_ref,
			source_sha,
			state,
			trigger_reason,
			COALESCE(execution_id::text, ''),
			COALESCE(attempt_id::text, ''),
			orchestrator_job_id,
			snapshot_ref,
			activated_at,
			superseded_at,
			failure_reason,
			failure_detail,
			created_at,
			updated_at
		FROM golden_generations
		WHERE repo_id = $1 AND golden_generation_id = $2
	`, repoID, generationID)

	record, err := scanGoldenGenerationRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrGoldenGenerationMissing
		}
		return nil, fmt.Errorf("scan golden generation: %w", err)
	}
	return record, nil
}

func (s *Service) ListGoldenGenerations(ctx context.Context, repoID uuid.UUID) ([]GoldenGenerationRecord, error) {
	rows, err := s.PG.QueryContext(ctx, `
		SELECT
			golden_generation_id,
			repo_id,
			runner_profile_slug,
			source_ref,
			source_sha,
			state,
			trigger_reason,
			COALESCE(execution_id::text, ''),
			COALESCE(attempt_id::text, ''),
			orchestrator_job_id,
			snapshot_ref,
			activated_at,
			superseded_at,
			failure_reason,
			failure_detail,
			created_at,
			updated_at
		FROM golden_generations
		WHERE repo_id = $1
		ORDER BY created_at DESC, updated_at DESC
	`, repoID)
	if err != nil {
		return nil, fmt.Errorf("query golden generations: %w", err)
	}
	defer rows.Close()

	var generations []GoldenGenerationRecord
	for rows.Next() {
		record, err := scanGoldenGenerationRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan golden generation row: %w", err)
		}
		generations = append(generations, *record)
	}
	return generations, rows.Err()
}

func (s *Service) AttachGoldenGenerationExecution(ctx context.Context, generationID, executionID, attemptID uuid.UUID, orchestratorJobID string) error {
	res, err := s.PG.ExecContext(ctx, `
		UPDATE golden_generations
		SET execution_id = $2,
		    attempt_id = $3,
		    orchestrator_job_id = $4,
		    updated_at = $5
		WHERE golden_generation_id = $1
	`, generationID, executionID, attemptID, strings.TrimSpace(orchestratorJobID), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("attach golden generation execution: %w", err)
	}
	return ensureRowsAffected(res, ErrGoldenGenerationMissing)
}

func (s *Service) SetGoldenGenerationState(ctx context.Context, generationID uuid.UUID, state, failureReason, failureDetail string) error {
	state = strings.TrimSpace(state)
	switch state {
	case GenerationStateBuilding, GenerationStateSanitizing:
		return s.markGoldenGenerationInProgress(ctx, generationID, state)
	case GenerationStateFailed:
		return s.markGoldenGenerationFailed(ctx, generationID, failureReason, failureDetail)
	default:
		return fmt.Errorf("unsupported golden generation state transition target %q", state)
	}
}

func (s *Service) ActivateGoldenGeneration(ctx context.Context, repoID, generationID uuid.UUID, snapshotRef string) error {
	now := time.Now().UTC()
	tx, err := s.PG.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		currentActiveID string
		sourceSHA       string
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(active_golden_generation_id::text, '')
		FROM repos
		WHERE repo_id = $1
		FOR UPDATE
	`, repoID).Scan(&currentActiveID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRepoMissing
		}
		return fmt.Errorf("lock repo for activation: %w", err)
	}

	if err := tx.QueryRowContext(ctx, `
		SELECT source_sha
		FROM golden_generations
		WHERE repo_id = $1 AND golden_generation_id = $2
	`, repoID, generationID).Scan(&sourceSHA); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrGoldenGenerationMissing
		}
		return fmt.Errorf("load activated generation: %w", err)
	}

	if currentActiveID != "" && currentActiveID != generationID.String() {
		if _, err := tx.ExecContext(ctx, `
			UPDATE golden_generations
			SET state = $2,
			    superseded_at = $3,
			    updated_at = $3
			WHERE golden_generation_id = $1
		`, currentActiveID, GenerationStateSuperseded, now); err != nil {
			return fmt.Errorf("supersede previous golden generation: %w", err)
		}
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE golden_generations
		SET state = $3,
		    snapshot_ref = CASE WHEN $4 <> '' THEN $4 ELSE snapshot_ref END,
		    activated_at = COALESCE(activated_at, $5),
		    superseded_at = NULL,
		    failure_reason = '',
		    failure_detail = '',
		    updated_at = $5
		WHERE repo_id = $1
		  AND golden_generation_id = $2
	`, repoID, generationID, GenerationStateReady, strings.TrimSpace(snapshotRef), now)
	if err != nil {
		return fmt.Errorf("mark golden generation ready: %w", err)
	}
	if err := ensureRowsAffected(res, ErrGoldenGenerationMissing); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE repos
		SET active_golden_generation_id = $2,
		    state = $3,
		    last_ready_sha = $4,
		    last_error = '',
		    updated_at = $5
		WHERE repo_id = $1
	`, repoID, generationID, RepoStateReady, sourceSHA, now); err != nil {
		return fmt.Errorf("update repo active golden generation: %w", err)
	}

	return tx.Commit()
}

func (s *Service) markGoldenGenerationInProgress(ctx context.Context, generationID uuid.UUID, state string) error {
	now := time.Now().UTC()
	tx, err := s.PG.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var repoID uuid.UUID
	if err := tx.QueryRowContext(ctx, `
		SELECT repo_id
		FROM golden_generations
		WHERE golden_generation_id = $1
		FOR UPDATE
	`, generationID).Scan(&repoID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrGoldenGenerationMissing
		}
		return fmt.Errorf("lock golden generation: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE golden_generations
		SET state = $2,
		    failure_reason = '',
		    failure_detail = '',
		    updated_at = $3
		WHERE golden_generation_id = $1
	`, generationID, state, now); err != nil {
		return fmt.Errorf("update golden generation state: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE repos
		SET state = $2,
		    last_error = '',
		    updated_at = $3
		WHERE repo_id = $1
	`, repoID, RepoStatePreparing, now); err != nil {
		return fmt.Errorf("update repo preparing state: %w", err)
	}

	return tx.Commit()
}

func (s *Service) markGoldenGenerationFailed(ctx context.Context, generationID uuid.UUID, failureReason, failureDetail string) error {
	now := time.Now().UTC()
	tx, err := s.PG.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		repoID         uuid.UUID
		activeIDString string
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT g.repo_id, COALESCE(r.active_golden_generation_id::text, '')
		FROM golden_generations g
		JOIN repos r ON r.repo_id = g.repo_id
		WHERE g.golden_generation_id = $1
		FOR UPDATE
	`, generationID).Scan(&repoID, &activeIDString); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrGoldenGenerationMissing
		}
		return fmt.Errorf("lock generation for failure: %w", err)
	}

	reason := firstNonEmpty(strings.TrimSpace(failureReason), "golden_generation_failed")
	detail := firstNonEmpty(strings.TrimSpace(failureDetail), reason)

	if _, err := tx.ExecContext(ctx, `
		UPDATE golden_generations
		SET state = $2,
		    failure_reason = $3,
		    failure_detail = $4,
		    updated_at = $5
		WHERE golden_generation_id = $1
	`, generationID, GenerationStateFailed, reason, detail, now); err != nil {
		return fmt.Errorf("mark golden generation failed: %w", err)
	}

	repoState := RepoStateFailed
	if activeIDString != "" {
		repoState = RepoStateDegraded
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE repos
		SET state = $2,
		    last_error = $3,
		    updated_at = $4
		WHERE repo_id = $1
	`, repoID, repoState, detail, now); err != nil {
		return fmt.Errorf("update repo failed state: %w", err)
	}

	return tx.Commit()
}

func (s *Service) getRepoByID(ctx context.Context, repoID uuid.UUID) (*RepoRecord, error) {
	row := s.PG.QueryRowContext(ctx, `
		SELECT
			repo_id,
			org_id,
			provider,
			provider_repo_id,
			owner,
			name,
			full_name,
			clone_url,
			default_branch,
			runner_profile_slug,
			state,
			compatibility_status,
			compatibility_summary,
			last_scanned_sha,
			COALESCE(active_golden_generation_id::text, ''),
			last_ready_sha,
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
		record       RepoRecord
		activeID     string
		archivedAt   sql.NullTime
		summaryBytes []byte
	)
	if err := scanner.Scan(
		&record.RepoID,
		&record.OrgID,
		&record.Provider,
		&record.ProviderRepoID,
		&record.Owner,
		&record.Name,
		&record.FullName,
		&record.CloneURL,
		&record.DefaultBranch,
		&record.RunnerProfileSlug,
		&record.State,
		&record.CompatibilityStatus,
		&summaryBytes,
		&record.LastScannedSHA,
		&activeID,
		&record.LastReadySHA,
		&record.LastError,
		&record.CreatedAt,
		&record.UpdatedAt,
		&archivedAt,
	); err != nil {
		return nil, err
	}
	record.CompatibilitySummary = append(json.RawMessage(nil), summaryBytes...)
	record.ActiveGoldenGenerationID = uuidPointer(activeID)
	if archivedAt.Valid {
		record.ArchivedAt = &archivedAt.Time
	}
	return &record, nil
}

func scanRepoRows(rows *sql.Rows) (*RepoRecord, error) {
	return scanRepoRow(rows)
}

func scanGoldenGenerationRow(scanner rowScanner) (*GoldenGenerationRecord, error) {
	var (
		record       GoldenGenerationRecord
		executionID  string
		attemptID    string
		activatedAt  sql.NullTime
		supersededAt sql.NullTime
	)
	if err := scanner.Scan(
		&record.GoldenGenerationID,
		&record.RepoID,
		&record.RunnerProfileSlug,
		&record.SourceRef,
		&record.SourceSHA,
		&record.State,
		&record.TriggerReason,
		&executionID,
		&attemptID,
		&record.OrchestratorJobID,
		&record.SnapshotRef,
		&activatedAt,
		&supersededAt,
		&record.FailureReason,
		&record.FailureDetail,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, err
	}
	record.ExecutionID = uuidPointer(executionID)
	record.AttemptID = uuidPointer(attemptID)
	if activatedAt.Valid {
		record.ActivatedAt = &activatedAt.Time
	}
	if supersededAt.Valid {
		record.SupersededAt = &supersededAt.Time
	}
	return &record, nil
}

func scanGoldenGenerationRows(rows *sql.Rows) (*GoldenGenerationRecord, error) {
	return scanGoldenGenerationRow(rows)
}

func normalizeCreateRepoRequest(req CreateRepoRequest) (CreateRepoRequest, error) {
	req.Provider = strings.TrimSpace(req.Provider)
	req.ProviderRepoID = strings.TrimSpace(req.ProviderRepoID)
	req.Owner = strings.TrimSpace(req.Owner)
	req.Name = strings.TrimSpace(req.Name)
	req.FullName = strings.TrimSpace(req.FullName)
	req.CloneURL = strings.TrimSpace(req.CloneURL)
	req.DefaultBranch = strings.TrimSpace(req.DefaultBranch)
	req.RunnerProfileSlug = strings.TrimSpace(req.RunnerProfileSlug)
	req.State = strings.TrimSpace(req.State)
	req.CompatibilityStatus = strings.TrimSpace(req.CompatibilityStatus)
	req.LastScannedSHA = strings.TrimSpace(req.LastScannedSHA)

	if req.OrgID == 0 {
		return CreateRepoRequest{}, fmt.Errorf("org_id is required")
	}
	if req.Provider == "" {
		return CreateRepoRequest{}, fmt.Errorf("provider is required")
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
	if req.DefaultBranch == "" {
		req.DefaultBranch = defaultBranchName
	}
	if req.RunnerProfileSlug == "" {
		req.RunnerProfileSlug = RunnerProfileForgeMetal
	}
	if req.State == "" {
		req.State = RepoStateImporting
	}
	switch req.State {
	case RepoStateImporting, RepoStateActionRequired, RepoStateWaitingForBootstrap, RepoStatePreparing, RepoStateReady, RepoStateDegraded, RepoStateFailed, RepoStateArchived:
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

func normalizeCreateGoldenGenerationRequest(req CreateGoldenGenerationRequest) (CreateGoldenGenerationRequest, error) {
	req.RunnerProfileSlug = strings.TrimSpace(req.RunnerProfileSlug)
	req.SourceRef = strings.TrimSpace(req.SourceRef)
	req.SourceSHA = strings.TrimSpace(req.SourceSHA)
	req.TriggerReason = strings.TrimSpace(req.TriggerReason)

	if req.RunnerProfileSlug == "" {
		req.RunnerProfileSlug = RunnerProfileForgeMetal
	}
	if req.SourceRef == "" {
		return CreateGoldenGenerationRequest{}, fmt.Errorf("source_ref is required")
	}
	if req.SourceSHA == "" {
		return CreateGoldenGenerationRequest{}, fmt.Errorf("source_sha is required")
	}
	if req.TriggerReason == "" {
		req.TriggerReason = GenerationTriggerBootstrap
	}
	return req, nil
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
