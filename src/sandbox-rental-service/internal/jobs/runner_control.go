package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/forge-metal/sandbox-rental-service/internal/scheduler"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type RunnerRepositoryRegistration struct {
	Provider             string
	OrgID                uint64
	SourceRepositoryID   uuid.UUID
	ProviderOwner        string
	ProviderRepo         string
	ProviderRepositoryID int64
	RepositoryFullName   string
}

const runnerBootstrapPath = "/internal/sandbox/v1/runner-bootstrap"

func (s *Service) ReconcileRunnerCapacity(ctx context.Context, provider string, providerJobID int64) error {
	switch strings.TrimSpace(provider) {
	case RunnerProviderGitHub:
		if s.GitHubRunner == nil {
			return ErrGitHubRunnerNotConfigured
		}
		return s.GitHubRunner.ReconcileCapacity(ctx, providerJobID)
	case RunnerProviderForgejo:
		if s.ForgejoRunner == nil {
			return ErrForgejoRunnerNotConfigured
		}
		return s.ForgejoRunner.ReconcileCapacity(ctx, providerJobID)
	default:
		return fmt.Errorf("%w: unsupported runner provider %q", ErrRunnerUnavailable, provider)
	}
}

func (s *Service) AllocateRunner(ctx context.Context, allocationID uuid.UUID) error {
	provider, err := s.runnerAllocationProvider(ctx, allocationID)
	if err != nil {
		return err
	}
	switch provider {
	case RunnerProviderGitHub:
		if s.GitHubRunner == nil {
			return ErrGitHubRunnerNotConfigured
		}
		return s.GitHubRunner.AllocateRunner(ctx, allocationID)
	case RunnerProviderForgejo:
		if s.ForgejoRunner == nil {
			return ErrForgejoRunnerNotConfigured
		}
		return s.ForgejoRunner.AllocateRunner(ctx, allocationID)
	default:
		return fmt.Errorf("%w: unsupported runner provider %q", ErrRunnerUnavailable, provider)
	}
}

func (s *Service) BindRunnerJob(ctx context.Context, provider string, providerJobID int64) error {
	switch strings.TrimSpace(provider) {
	case RunnerProviderGitHub:
		if s.GitHubRunner == nil {
			return ErrGitHubRunnerNotConfigured
		}
		return s.GitHubRunner.BindJob(ctx, providerJobID)
	case RunnerProviderForgejo:
		if s.ForgejoRunner == nil {
			return ErrForgejoRunnerNotConfigured
		}
		return s.ForgejoRunner.BindJob(ctx, providerJobID)
	default:
		return fmt.Errorf("%w: unsupported runner provider %q", ErrRunnerUnavailable, provider)
	}
}

func (s *Service) CleanupRunner(ctx context.Context, allocationID uuid.UUID) error {
	provider, err := s.runnerAllocationProvider(ctx, allocationID)
	if err != nil {
		return err
	}
	switch provider {
	case RunnerProviderGitHub:
		if s.GitHubRunner == nil {
			return ErrGitHubRunnerNotConfigured
		}
		return s.GitHubRunner.CleanupRunner(ctx, allocationID)
	case RunnerProviderForgejo:
		if s.ForgejoRunner == nil {
			return ErrForgejoRunnerNotConfigured
		}
		return s.ForgejoRunner.CleanupRunner(ctx, allocationID)
	default:
		return fmt.Errorf("%w: unsupported runner provider %q", ErrRunnerUnavailable, provider)
	}
}

func (s *Service) SyncRunnerRepository(ctx context.Context, provider string, providerRepositoryID int64) error {
	switch strings.TrimSpace(provider) {
	case RunnerProviderForgejo:
		if s.ForgejoRunner == nil {
			return ErrForgejoRunnerNotConfigured
		}
		return s.ForgejoRunner.SyncRepositoryJobs(ctx, providerRepositoryID)
	default:
		return nil
	}
}

func (s *Service) RegisterRunnerRepository(ctx context.Context, req RunnerRepositoryRegistration) error {
	switch strings.TrimSpace(req.Provider) {
	case RunnerProviderForgejo:
		if s.ForgejoRunner == nil {
			return ErrForgejoRunnerNotConfigured
		}
		return s.ForgejoRunner.RegisterRepository(ctx, req)
	default:
		return fmt.Errorf("%w: unsupported runner provider %q", ErrRunnerUnavailable, req.Provider)
	}
}

func (s *Service) MarkRunnerExecutionExited(ctx context.Context, executionID uuid.UUID) {
	if s == nil || s.PGX == nil || executionID == uuid.Nil {
		return
	}
	rows, err := s.PGX.Query(ctx, `UPDATE runner_allocations
		SET state = CASE WHEN state = 'cleaned' THEN state ELSE 'vm_exited' END,
		    vm_exit_by = $1,
		    updated_at = $1
		WHERE execution_id = $2
		RETURNING allocation_id`, time.Now().UTC(), executionID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var allocationID uuid.UUID
		if err := rows.Scan(&allocationID); err == nil && s.Scheduler != nil {
			_, _ = s.Scheduler.EnqueueRunnerCleanup(ctx, schedulerCleanupRequest(ctx, allocationID))
		}
	}
}

func schedulerCleanupRequest(ctx context.Context, allocationID uuid.UUID) scheduler.RunnerCleanupRequest {
	return scheduler.RunnerCleanupRequest{
		AllocationID:  allocationID.String(),
		CorrelationID: CorrelationIDFromContext(ctx),
		TraceParent:   traceParent(ctx),
	}
}

func (s *Service) attachRunnerAllocationExecutionTx(ctx context.Context, tx pgx.Tx, allocationID, executionID, attemptID uuid.UUID, bootstrapKind, bootstrapPayload string) error {
	bootstrapKind = strings.TrimSpace(bootstrapKind)
	bootstrapPayload = strings.TrimSpace(bootstrapPayload)
	if bootstrapKind == "" || bootstrapPayload == "" {
		return fmt.Errorf("%w: runner bootstrap payload is required", ErrRunnerUnavailable)
	}
	provider, err := s.runnerAllocationProviderTx(ctx, tx, allocationID)
	if err != nil {
		return err
	}
	token, err := s.deriveRunnerBootstrapToken(provider, allocationID, attemptID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	tag, err := tx.Exec(ctx, `UPDATE runner_allocations
		SET execution_id = $1, attempt_id = $2, state = 'vm_submitted', vm_submitted_by = $3, updated_at = $3
		WHERE allocation_id = $4 AND state IN ('jit_created', 'pending', 'jit_creating', 'bootstrap_created', 'bootstrap_creating')`, executionID, attemptID, now, allocationID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("runner allocation %s is not attachable", allocationID)
	}
	_, err = tx.Exec(ctx, `INSERT INTO runner_bootstrap_configs (
		allocation_id, attempt_id, fetch_token_hash, bootstrap_kind, bootstrap_payload, expires_at, created_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7)
	ON CONFLICT (allocation_id) DO UPDATE SET
		attempt_id = EXCLUDED.attempt_id,
		fetch_token_hash = EXCLUDED.fetch_token_hash,
		bootstrap_kind = EXCLUDED.bootstrap_kind,
		bootstrap_payload = EXCLUDED.bootstrap_payload,
		expires_at = EXCLUDED.expires_at,
		consumed_at = NULL`,
		allocationID, attemptID, hashToken(token), bootstrapKind, bootstrapPayload, now.Add(15*time.Minute), now)
	return err
}

func (s *Service) runnerExecEnv(ctx context.Context, executionID, attemptID uuid.UUID) map[string]string {
	var (
		allocationID uuid.UUID
		provider     string
	)
	if err := s.PGX.QueryRow(ctx, `SELECT allocation_id, provider FROM runner_allocations WHERE execution_id = $1`, executionID).Scan(&allocationID, &provider); err != nil {
		return nil
	}
	if provider == RunnerProviderGitHub && s.GitHubRunner != nil {
		return s.GitHubRunner.execEnv(ctx, executionID, attemptID)
	}
	token, err := s.deriveRunnerBootstrapToken(provider, allocationID, attemptID)
	if err != nil {
		return nil
	}
	env := map[string]string{
		"FORGE_METAL_RUNNER_BOOTSTRAP_TOKEN": token,
		"FORGE_METAL_RUNNER_BOOTSTRAP_PATH":  runnerBootstrapPath,
	}
	if parent := traceParent(ctx); parent != "" {
		env["FORGE_METAL_TRACEPARENT"] = parent
	}
	return env
}

func (s *Service) ConsumeRunnerBootstrapConfig(ctx context.Context, token, expectedKind string) (string, error) {
	token = strings.TrimSpace(token)
	expectedKind = strings.TrimSpace(expectedKind)
	if token == "" {
		return "", ErrRunnerUnavailable
	}
	ctx, span := tracer.Start(ctx, "runner.bootstrap.consume")
	defer span.End()
	tx, err := s.PGX.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var (
		allocationID uuid.UUID
		kind         string
		payload      string
		expiresAt    time.Time
		consumedAt   *time.Time
	)
	err = tx.QueryRow(ctx, `SELECT allocation_id, bootstrap_kind, bootstrap_payload, expires_at, consumed_at
		FROM runner_bootstrap_configs
		WHERE fetch_token_hash = $1
		FOR UPDATE`, hashToken(token)).Scan(&allocationID, &kind, &payload, &expiresAt, &consumedAt)
	if err != nil {
		return "", ErrRunnerUnavailable
	}
	if expectedKind != "" && kind != expectedKind {
		return "", ErrRunnerUnavailable
	}
	if consumedAt != nil || time.Now().UTC().After(expiresAt) {
		return "", ErrRunnerUnavailable
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `UPDATE runner_bootstrap_configs SET consumed_at = $1 WHERE allocation_id = $2`, now, allocationID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE runner_allocations SET state = CASE WHEN state = 'vm_submitted' THEN 'runner_config_fetched' ELSE state END, updated_at = $1 WHERE allocation_id = $2`, now, allocationID); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	span.SetAttributes(attribute.String("runner.allocation_id", allocationID.String()), attribute.String("runner.bootstrap_kind", kind))
	return payload, nil
}

func (s *Service) runnerAllocationProvider(ctx context.Context, allocationID uuid.UUID) (string, error) {
	var provider string
	err := s.PGX.QueryRow(ctx, `SELECT provider FROM runner_allocations WHERE allocation_id = $1`, allocationID).Scan(&provider)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrRunnerUnavailable
	}
	return provider, err
}

func (s *Service) runnerAllocationProviderTx(ctx context.Context, tx pgx.Tx, allocationID uuid.UUID) (string, error) {
	var provider string
	err := tx.QueryRow(ctx, `SELECT provider FROM runner_allocations WHERE allocation_id = $1 FOR UPDATE`, allocationID).Scan(&provider)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrRunnerUnavailable
	}
	return provider, err
}

func (s *Service) deriveRunnerBootstrapToken(provider string, allocationID, attemptID uuid.UUID) (string, error) {
	switch strings.TrimSpace(provider) {
	case RunnerProviderGitHub:
		if s.GitHubRunner == nil {
			return "", ErrGitHubRunnerNotConfigured
		}
		return s.GitHubRunner.deriveJITFetchToken(allocationID, attemptID), nil
	case RunnerProviderForgejo:
		if s.ForgejoRunner == nil {
			return "", ErrForgejoRunnerNotConfigured
		}
		return s.ForgejoRunner.deriveBootstrapFetchToken(allocationID, attemptID), nil
	default:
		return "", fmt.Errorf("%w: unsupported runner provider %q", ErrRunnerUnavailable, provider)
	}
}

func recordRunnerError(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
