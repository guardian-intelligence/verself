package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/verself/sandbox-rental-service/internal/scheduler"
	"github.com/verself/sandbox-rental-service/internal/store"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type RunnerRepositoryRegistration struct {
	Provider             string
	OrgID                uint64
	ProjectID            uuid.UUID
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
	allocationIDs, err := s.storeQueries().MarkRunnerExecutionExited(ctx, store.MarkRunnerExecutionExitedParams{
		UpdatedAt:   pgTime(time.Now().UTC()),
		ExecutionID: &executionID,
	})
	if err != nil {
		return
	}
	for _, allocationID := range allocationIDs {
		if s.Scheduler != nil {
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
	rows, err := store.New(tx).AttachRunnerAllocationExecution(ctx, store.AttachRunnerAllocationExecutionParams{
		ExecutionID:  &executionID,
		AttemptID:    &attemptID,
		UpdatedAt:    pgTime(now),
		AllocationID: allocationID,
	})
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("runner allocation %s is not attachable", allocationID)
	}
	return store.New(tx).UpsertRunnerBootstrapConfig(ctx, store.UpsertRunnerBootstrapConfigParams{
		AllocationID:     allocationID,
		AttemptID:        attemptID,
		FetchTokenHash:   hashToken(token),
		BootstrapKind:    bootstrapKind,
		BootstrapPayload: bootstrapPayload,
		ExpiresAt:        pgTime(now.Add(15 * time.Minute)),
		CreatedAt:        pgTime(now),
	})
}

func (s *Service) runnerExecEnv(ctx context.Context, executionID, attemptID uuid.UUID) map[string]string {
	row, err := s.storeQueries().GetRunnerAllocationByExecution(ctx, store.GetRunnerAllocationByExecutionParams{ExecutionID: &executionID})
	if err != nil {
		return nil
	}
	if row.Provider == RunnerProviderGitHub && s.GitHubRunner != nil {
		return s.GitHubRunner.execEnv(ctx, executionID, attemptID)
	}
	token, err := s.deriveRunnerBootstrapToken(row.Provider, row.AllocationID, attemptID)
	if err != nil {
		return nil
	}
	env := map[string]string{
		"VERSELF_RUNNER_BOOTSTRAP_TOKEN": token,
		"VERSELF_RUNNER_BOOTSTRAP_PATH":  runnerBootstrapPath,
	}
	if parent := traceParent(ctx); parent != "" {
		env["VERSELF_TRACEPARENT"] = parent
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
	row, err := store.New(tx).LockRunnerBootstrapConfigByTokenHash(ctx, store.LockRunnerBootstrapConfigByTokenHashParams{FetchTokenHash: hashToken(token)})
	if err != nil {
		return "", ErrRunnerUnavailable
	}
	allocationID = row.AllocationID
	kind = row.BootstrapKind
	payload = row.BootstrapPayload
	expiresAt = timeFromPG(row.ExpiresAt)
	consumedAt = timePtrFromPG(row.ConsumedAt)
	if expectedKind != "" && kind != expectedKind {
		return "", ErrRunnerUnavailable
	}
	if consumedAt != nil || time.Now().UTC().After(expiresAt) {
		return "", ErrRunnerUnavailable
	}
	now := time.Now().UTC()
	qtx := store.New(tx)
	if err := qtx.MarkRunnerBootstrapConsumed(ctx, store.MarkRunnerBootstrapConsumedParams{ConsumedAt: pgTime(now), AllocationID: allocationID}); err != nil {
		return "", err
	}
	if err := qtx.MarkRunnerAllocationConfigFetched(ctx, store.MarkRunnerAllocationConfigFetchedParams{UpdatedAt: pgTime(now), AllocationID: allocationID}); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	span.SetAttributes(attribute.String("runner.allocation_id", allocationID.String()), attribute.String("runner.bootstrap_kind", kind))
	return payload, nil
}

func (s *Service) runnerAllocationProvider(ctx context.Context, allocationID uuid.UUID) (string, error) {
	provider, err := s.storeQueries().GetRunnerAllocationProvider(ctx, store.GetRunnerAllocationProviderParams{AllocationID: allocationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrRunnerUnavailable
	}
	return provider, err
}

func (s *Service) runnerAllocationProviderTx(ctx context.Context, tx pgx.Tx, allocationID uuid.UUID) (string, error) {
	provider, err := store.New(tx).LockRunnerAllocationProvider(ctx, store.LockRunnerAllocationProviderParams{AllocationID: allocationID})
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
