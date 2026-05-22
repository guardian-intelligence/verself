package jobs

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/verself/sandbox-rental-service/internal/scheduler"
	"github.com/verself/sandbox-rental-service/internal/store"
	secretsclient "github.com/verself/secrets-service/client"
	"go.opentelemetry.io/otel/attribute"
)

type RunnerRepositoryRegistration struct {
	Provider             string
	OrgID                string
	ProjectID            uuid.UUID
	SourceRepositoryID   uuid.UUID
	ProviderOwner        string
	ProviderRepo         string
	ProviderRepositoryID int64
	RepositoryFullName   string
}

const (
	runnerBootstrapPath       = "/internal/sandbox/v1/runner-bootstrap"
	runnerBootstrapSecretTTL  = 15 * time.Minute
	runnerBootstrapSecretWait = 5 * time.Second
	githubCheckoutPath        = "/internal/sandbox/v1/github-checkout"
	githubBazelTelemetryPath  = "/internal/sandbox/v1/bazel-telemetry/invocations"
)

func (s *Service) BindRunnerJob(ctx context.Context, provider string, providerJobID int64) error {
	switch strings.TrimSpace(provider) {
	case RunnerProviderGitHub:
		_, err := s.bindProviderRunnerJob(ctx, RunnerProviderGitHub, providerJobID)
		return err
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
		now := time.Now().UTC()
		if err := s.storeQueries().MarkRunnerAllocationCleaned(ctx, store.MarkRunnerAllocationCleanedParams{
			CleanupBy:    pgTime(now),
			AllocationID: allocationID,
		}); err != nil {
			return err
		}
		return s.deleteRunnerBootstrapConfig(ctx, allocationID)
	default:
		return fmt.Errorf("%w: unsupported runner provider %q", ErrRunnerUnavailable, provider)
	}
}

func (s *Service) RegisterRunnerRepository(ctx context.Context, req RunnerRepositoryRegistration) error {
	switch strings.TrimSpace(req.Provider) {
	case RunnerProviderGitHub:
		fullName := strings.TrimSpace(req.RepositoryFullName)
		owner := strings.TrimSpace(req.ProviderOwner)
		repo := strings.TrimSpace(req.ProviderRepo)
		if fullName == "" && owner != "" && repo != "" {
			fullName = owner + "/" + repo
		}
		if owner == "" {
			owner, _, _ = strings.Cut(fullName, "/")
		}
		if repo == "" {
			_, repo, _ = strings.Cut(fullName, "/")
		}
		if owner == "" || repo == "" || fullName == "" || req.ProviderRepositoryID <= 0 {
			return fmt.Errorf("%w: github repository registration is invalid", ErrRunnerUnavailable)
		}
		rows, err := s.storeQueries().UpsertProviderRunnerRepository(ctx, store.UpsertProviderRunnerRepositoryParams{
			Provider:             RunnerProviderGitHub,
			ProviderRepositoryID: req.ProviderRepositoryID,
			OrgID:                dbOrgID(req.OrgID),
			ProjectID:            uuidPtrFromZero(req.ProjectID),
			SourceRepositoryID:   uuidPtrFromZero(req.SourceRepositoryID),
			ProviderOwner:        owner,
			ProviderRepo:         repo,
			RepositoryFullName:   fullName,
			UpdatedAt:            pgTime(time.Now().UTC()),
		})
		if err != nil {
			return err
		}
		if rows != 1 {
			return fmt.Errorf("%w: github repository %s is already registered to another org", ErrRunnerUnavailable, fullName)
		}
		return nil
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

func (s *Service) attachRunnerAllocationExecutionTx(ctx context.Context, tx pgx.Tx, allocationID, executionID, attemptID uuid.UUID, bootstrapKind, bootstrapPayload string) (string, error) {
	bootstrapKind = strings.TrimSpace(bootstrapKind)
	if bootstrapKind == "" || strings.TrimSpace(bootstrapPayload) == "" {
		return "", fmt.Errorf("%w: runner bootstrap payload is required", ErrRunnerUnavailable)
	}
	provider, err := s.runnerAllocationProviderTx(ctx, tx, allocationID)
	if err != nil {
		return "", err
	}
	token, err := s.deriveRunnerBootstrapToken(provider, allocationID, attemptID)
	if err != nil {
		return "", err
	}
	secretName := runnerBootstrapSecretName(provider, allocationID, attemptID)
	if err := s.putRunnerBootstrapSecret(ctx, secretName, bootstrapPayload, "runner-bootstrap:"+attemptID.String()); err != nil {
		return "", err
	}
	keepSecret := false
	defer func() {
		if !keepSecret {
			_ = s.deleteRunnerBootstrapSecret(context.Background(), secretName, "attach-failed:"+attemptID.String())
		}
	}()
	now := time.Now().UTC()
	rows, err := store.New(tx).AttachRunnerAllocationExecution(ctx, store.AttachRunnerAllocationExecutionParams{
		ExecutionID:  &executionID,
		AttemptID:    &attemptID,
		UpdatedAt:    pgTime(now),
		AllocationID: allocationID,
	})
	if err != nil {
		return "", err
	}
	if rows != 1 {
		return "", fmt.Errorf("runner allocation %s is not attachable", allocationID)
	}
	if err := store.New(tx).UpsertRunnerBootstrapConfig(ctx, store.UpsertRunnerBootstrapConfigParams{
		AllocationID:        allocationID,
		AttemptID:           attemptID,
		FetchTokenHash:      hashToken(token),
		BootstrapKind:       bootstrapKind,
		BootstrapSecretName: secretName,
		ExpiresAt:           pgTime(now.Add(runnerBootstrapSecretTTL)),
		CreatedAt:           pgTime(now),
	}); err != nil {
		return "", err
	}
	keepSecret = true
	return secretName, nil
}

func (s *Service) runnerExecEnv(ctx context.Context, executionID, attemptID uuid.UUID) map[string]string {
	row, err := s.storeQueries().GetRunnerAllocationByExecution(ctx, store.GetRunnerAllocationByExecutionParams{ExecutionID: &executionID})
	if err != nil {
		return nil
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
	if row.Provider == RunnerProviderGitHub {
		env["VERSELF_CHECKOUT_TOKEN"] = s.deriveScopedExecutionToken("verself-checkout", executionID, attemptID)
		env["VERSELF_CHECKOUT_PATH"] = githubCheckoutPath
		env["VERSELF_BAZEL_TELEMETRY_TOKEN"] = s.deriveScopedExecutionToken("verself-bazel-telemetry", executionID, attemptID)
		env["VERSELF_BAZEL_TELEMETRY_PATH"] = githubBazelTelemetryPath
	}
	return env
}

func runnerBootstrapSecretName(provider string, allocationID, attemptID uuid.UUID) string {
	return secretsclient.SandboxRunnerBootstrapSecretPrefix + strings.TrimSpace(provider) + "." + allocationID.String() + "." + attemptID.String()
}

func (s *Service) putRunnerBootstrapSecret(ctx context.Context, secretName string, payload string, idempotencyKey string) error {
	if s.Secrets == nil {
		return fmt.Errorf("%w: secrets client is required for runner bootstrap", ErrRunnerUnavailable)
	}
	secretName = strings.TrimSpace(secretName)
	if secretName == "" || strings.TrimSpace(payload) == "" {
		return fmt.Errorf("%w: runner bootstrap secret is required", ErrRunnerUnavailable)
	}
	secretCtx, cancel := context.WithTimeout(ctx, runnerBootstrapSecretWait)
	defer cancel()
	resp, err := s.Secrets.PutSecret(secretCtx, secretsclient.PutSecretRequest{
		IdempotencyKey: secretsclient.IdempotencyKey(idempotencyKey),
		Name:           secretsclient.SecretName(secretName),
		Body: secretsclient.SecretMutationInputBody{
			Value: secretsclient.SecretValue(payload),
		},
	})
	if err != nil {
		return fmt.Errorf("%w: put runner bootstrap secret: %v", ErrRunnerUnavailable, err)
	}
	if resp == nil || resp.Result == nil {
		status := 0
		body := ""
		if resp != nil {
			status = resp.StatusCode
			body = strings.TrimSpace(string(resp.Body))
		}
		return fmt.Errorf("%w: put runner bootstrap secret status %d: %s", ErrRunnerUnavailable, status, body)
	}
	return nil
}

func (s *Service) readRunnerBootstrapSecret(ctx context.Context, secretName string) (string, error) {
	if s.Secrets == nil {
		return "", fmt.Errorf("%w: secrets client is required for runner bootstrap", ErrRunnerUnavailable)
	}
	secretName = strings.TrimSpace(secretName)
	if secretName == "" {
		return "", fmt.Errorf("%w: runner bootstrap secret name is required", ErrRunnerUnavailable)
	}
	secretCtx, cancel := context.WithTimeout(ctx, runnerBootstrapSecretWait)
	defer cancel()
	resp, err := s.Secrets.ReadSecret(secretCtx, secretsclient.ReadSecretRequest{Name: secretsclient.SecretName(secretName)})
	if err != nil {
		return "", fmt.Errorf("%w: read runner bootstrap secret: %v", ErrRunnerUnavailable, err)
	}
	if resp == nil || resp.Result == nil {
		status := 0
		body := ""
		if resp != nil {
			status = resp.StatusCode
			body = strings.TrimSpace(string(resp.Body))
		}
		return "", fmt.Errorf("%w: read runner bootstrap secret status %d: %s", ErrRunnerUnavailable, status, body)
	}
	payload := string(resp.Result.Value)
	if strings.TrimSpace(payload) == "" {
		return "", fmt.Errorf("%w: runner bootstrap secret payload is empty", ErrRunnerUnavailable)
	}
	return payload, nil
}

func (s *Service) deleteRunnerBootstrapSecret(ctx context.Context, secretName string, idempotencyKey string) error {
	if s.Secrets == nil {
		return fmt.Errorf("%w: secrets client is required for runner bootstrap", ErrRunnerUnavailable)
	}
	secretName = strings.TrimSpace(secretName)
	if secretName == "" {
		return nil
	}
	secretCtx, cancel := context.WithTimeout(ctx, runnerBootstrapSecretWait)
	defer cancel()
	resp, err := s.Secrets.DeleteSecret(secretCtx, secretsclient.DeleteSecretRequest{
		IdempotencyKey: secretsclient.IdempotencyKey(idempotencyKey),
		Name:           secretsclient.SecretName(secretName),
	})
	if err != nil {
		return fmt.Errorf("%w: delete runner bootstrap secret: %v", ErrRunnerUnavailable, err)
	}
	if resp == nil || resp.Result == nil {
		status := 0
		body := ""
		if resp != nil {
			status = resp.StatusCode
			body = strings.TrimSpace(string(resp.Body))
		}
		return fmt.Errorf("%w: delete runner bootstrap secret status %d: %s", ErrRunnerUnavailable, status, body)
	}
	return nil
}

func (s *Service) deleteRunnerBootstrapConfig(ctx context.Context, allocationID uuid.UUID) error {
	secretName, err := s.storeQueries().GetRunnerBootstrapSecretNameByAllocation(ctx, store.GetRunnerBootstrapSecretNameByAllocationParams{AllocationID: allocationID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil && strings.TrimSpace(secretName) != "" {
		_ = s.deleteRunnerBootstrapSecret(ctx, secretName, "runner-bootstrap-cleanup:"+allocationID.String())
	}
	return s.storeQueries().DeleteRunnerBootstrapConfig(ctx, store.DeleteRunnerBootstrapConfigParams{AllocationID: allocationID})
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
		secretName   string
		expiresAt    time.Time
		consumedAt   *time.Time
	)
	row, err := store.New(tx).LockRunnerBootstrapConfigByTokenHash(ctx, store.LockRunnerBootstrapConfigByTokenHashParams{FetchTokenHash: hashToken(token)})
	if err != nil {
		return "", ErrRunnerUnavailable
	}
	allocationID = row.AllocationID
	kind = row.BootstrapKind
	secretName = row.BootstrapSecretName
	expiresAt = timeFromPG(row.ExpiresAt)
	consumedAt = timePtrFromPG(row.ConsumedAt)
	if expectedKind != "" && kind != expectedKind {
		return "", ErrRunnerUnavailable
	}
	if consumedAt != nil || time.Now().UTC().After(expiresAt) {
		return "", ErrRunnerUnavailable
	}
	payload, err := s.readRunnerBootstrapSecret(ctx, secretName)
	if err != nil {
		return "", ErrRunnerUnavailable
	}
	// Delete before returning so a consumed bootstrap payload is not recoverable.
	if err := s.deleteRunnerBootstrapSecret(ctx, secretName, "runner-bootstrap-consume:"+allocationID.String()); err != nil {
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
		return s.deriveScopedAllocationToken("verself-runner-bootstrap:"+provider, allocationID, attemptID)
	default:
		return "", fmt.Errorf("%w: unsupported runner provider %q", ErrRunnerUnavailable, provider)
	}
}

func (s *Service) deriveScopedAllocationToken(scope string, allocationID, attemptID uuid.UUID) (string, error) {
	secret := strings.TrimSpace(s.RunnerBootstrapSecret)
	if secret == "" {
		return "", fmt.Errorf("%w: runner bootstrap secret is not configured", ErrRunnerUnavailable)
	}
	mac := hmac.New(sha256.New, []byte(scope+":"+secret))
	mac.Write([]byte(allocationID.String()))
	mac.Write([]byte(":"))
	mac.Write([]byte(attemptID.String()))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *Service) deriveScopedExecutionToken(scope string, executionID, attemptID uuid.UUID) string {
	secret := strings.TrimSpace(s.RunnerBootstrapSecret)
	if secret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(scope+":"+secret))
	mac.Write([]byte(executionID.String()))
	mac.Write([]byte(":"))
	mac.Write([]byte(attemptID.String()))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}
