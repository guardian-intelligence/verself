package source

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"maps"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var storeTracer = otel.Tracer("source-code-hosting-service/store")

type Store struct {
	PG  *pgxpool.Pool
	Now func() time.Time
}

func (s Store) Ready(ctx context.Context) error {
	if s.PG == nil {
		return ErrStoreUnavailable
	}
	var one int
	if err := s.PG.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s Store) UpsertInstallation(ctx context.Context, provider, baseURL, owner string) error {
	ctx, span := storeTracer.Start(ctx, "source.pg.installation.upsert")
	defer span.End()

	_, err := s.PG.Exec(ctx, `
INSERT INTO source_provider_installations (installation_id, provider, provider_installation_id, base_url, owner_username, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $6)
ON CONFLICT (installation_id)
DO UPDATE SET provider = EXCLUDED.provider, base_url = EXCLUDED.base_url, owner_username = EXCLUDED.owner_username, updated_at = EXCLUDED.updated_at`,
		uuid.MustParse("00000000-0000-0000-0000-000000000001"), provider, "system:"+provider, baseURL, owner, s.now())
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s Store) CreateRepository(ctx context.Context, principal Principal, req CreateRepositoryRequest, owner string, forgejoRepo forgejoRepo) (Repository, error) {
	ctx, span := storeTracer.Start(ctx, "source.pg.repo.create")
	defer span.End()

	if err := ValidatePrincipal(principal); err != nil {
		return Repository{}, err
	}
	req, err := NormalizeCreate(req)
	if err != nil {
		return Repository{}, err
	}
	now := s.now()
	repo := Repository{
		RepoID:         uuid.New(),
		OrgID:          principal.OrgID,
		CreatedBy:      principal.Subject,
		Name:           req.Name,
		Slug:           NormalizeSlug(req.Name),
		Description:    req.Description,
		DefaultBranch:  req.DefaultBranch,
		Visibility:     "private",
		Provider:       ProviderForgejo,
		ProviderOwner:  owner,
		ProviderRepo:   forgejoRepo.Name,
		ProviderRepoID: fmt.Sprintf("%d", forgejoRepo.ID),
		State:          "active",
		Version:        1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Repository{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	_, err = tx.Exec(ctx, `
INSERT INTO source_repositories (
    repo_id, org_id, created_by, name, slug, description, default_branch, visibility,
    provider, provider_owner, provider_repo, provider_repo_id, state, version, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		repo.RepoID, repo.OrgID, repo.CreatedBy, repo.Name, repo.Slug, repo.Description, repo.DefaultBranch, repo.Visibility,
		repo.Provider, repo.ProviderOwner, repo.ProviderRepo, repo.ProviderRepoID, repo.State, repo.Version, repo.CreatedAt, repo.UpdatedAt)
	if err != nil {
		return Repository{}, storeWriteError(err)
	}
	if err := s.insertEventTx(ctx, tx, principal.OrgID, principal.Subject, repo.RepoID, "source.repo.created", "allowed", map[string]any{
		"name":           repo.Name,
		"slug":           repo.Slug,
		"provider":       repo.Provider,
		"provider_owner": repo.ProviderOwner,
		"provider_repo":  repo.ProviderRepo,
	}); err != nil {
		return Repository{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Repository{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	span.SetAttributes(attribute.String("source.repo_id", repo.RepoID.String()), attribute.String("source.provider_repo_id", repo.ProviderRepoID))
	return repo, nil
}

func (s Store) ListRepositories(ctx context.Context, orgID uint64) ([]Repository, error) {
	ctx, span := storeTracer.Start(ctx, "source.pg.repo.list")
	defer span.End()
	span.SetAttributes(attribute.Int64("forge_metal.org_id", int64(orgID)))

	rows, err := s.PG.Query(ctx, `
SELECT repo_id, org_id, created_by, name, slug, description, default_branch, visibility,
       provider, provider_owner, provider_repo, provider_repo_id, state, version, created_at, updated_at
FROM source_repositories
WHERE org_id = $1 AND state = 'active'
ORDER BY updated_at DESC, repo_id DESC`, orgID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rows.Close()
	repos := []Repository{}
	for rows.Next() {
		repo, err := scanRepository(rows)
		if err != nil {
			return nil, err
		}
		repos = append(repos, repo)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	span.SetAttributes(attribute.Int("source.repo_count", len(repos)))
	return repos, nil
}

func (s Store) GetRepository(ctx context.Context, orgID uint64, repoID uuid.UUID) (Repository, error) {
	ctx, span := storeTracer.Start(ctx, "source.pg.repo.get")
	defer span.End()
	span.SetAttributes(attribute.String("source.repo_id", repoID.String()), attribute.Int64("forge_metal.org_id", int64(orgID)))

	row := s.PG.QueryRow(ctx, `
SELECT repo_id, org_id, created_by, name, slug, description, default_branch, visibility,
       provider, provider_owner, provider_repo, provider_repo_id, state, version, created_at, updated_at
FROM source_repositories
WHERE org_id = $1 AND repo_id = $2 AND state = 'active'`, orgID, repoID)
	repo, err := scanRepository(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Repository{}, ErrNotFound
		}
		return Repository{}, err
	}
	return repo, nil
}

func (s Store) FindRepositoryByProvider(ctx context.Context, provider, providerOwner, providerRepo, providerRepoID string) (Repository, error) {
	ctx, span := storeTracer.Start(ctx, "source.pg.repo.resolve_provider")
	defer span.End()
	provider = strings.TrimSpace(provider)
	providerOwner = strings.TrimSpace(providerOwner)
	providerRepo = strings.TrimSpace(providerRepo)
	providerRepoID = strings.TrimSpace(providerRepoID)
	span.SetAttributes(
		attribute.String("source.provider", provider),
		attribute.String("source.provider_repo_id", providerRepoID),
	)
	if provider == "" || (providerRepoID == "" && (providerOwner == "" || providerRepo == "")) {
		return Repository{}, ErrInvalid
	}
	query := `
SELECT repo_id, org_id, created_by, name, slug, description, default_branch, visibility,
       provider, provider_owner, provider_repo, provider_repo_id, state, version, created_at, updated_at
FROM source_repositories
WHERE provider = $1
  AND state = 'active'
  AND (
    ($2 <> '' AND provider_repo_id = $2)
    OR ($2 = '' AND provider_owner = $3 AND provider_repo = $4)
  )
LIMIT 1`
	repo, err := scanRepository(s.PG.QueryRow(ctx, query, provider, providerRepoID, providerOwner, providerRepo))
	if err != nil {
		return Repository{}, err
	}
	span.SetAttributes(attribute.String("source.repo_id", repo.RepoID.String()), attribute.Int64("forge_metal.org_id", int64(repo.OrgID)))
	return repo, nil
}

func (s Store) CreateCheckoutGrant(ctx context.Context, principal Principal, repo Repository, ref, pathPrefix string, ttl time.Duration) (CheckoutGrant, error) {
	ctx, span := storeTracer.Start(ctx, "source.pg.checkout_grant.create")
	defer span.End()

	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = repo.DefaultBranch
	}
	if ttl <= 0 || ttl > 10*time.Minute {
		return CheckoutGrant{}, ErrInvalid
	}
	token, tokenHash, err := newGrantToken()
	if err != nil {
		return CheckoutGrant{}, err
	}
	now := s.now()
	grant := CheckoutGrant{
		GrantID:    uuid.New(),
		RepoID:     repo.RepoID,
		OrgID:      principal.OrgID,
		ActorID:    principal.Subject,
		Ref:        ref,
		PathPrefix: strings.Trim(strings.TrimSpace(pathPrefix), "/"),
		Token:      token,
		ExpiresAt:  now.Add(ttl),
		CreatedAt:  now,
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CheckoutGrant{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	_, err = tx.Exec(ctx, `
INSERT INTO source_checkout_grants (
    grant_id, repo_id, org_id, actor_id, ref, path_prefix, token_hash, expires_at, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		grant.GrantID, grant.RepoID, grant.OrgID, grant.ActorID, grant.Ref, grant.PathPrefix, tokenHash, grant.ExpiresAt, grant.CreatedAt)
	if err != nil {
		return CheckoutGrant{}, storeWriteError(err)
	}
	if err := s.insertEventTx(ctx, tx, principal.OrgID, principal.Subject, repo.RepoID, "source.checkout_grant.created", "allowed", map[string]any{
		"grant_id": grant.GrantID.String(),
		"ref":      grant.Ref,
	}); err != nil {
		return CheckoutGrant{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CheckoutGrant{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	span.SetAttributes(attribute.String("source.checkout_grant_id", grant.GrantID.String()), attribute.String("source.repo_id", repo.RepoID.String()))
	return grant, nil
}

func (s Store) ConsumeCheckoutGrant(ctx context.Context, grantID uuid.UUID, token string) (CheckoutGrant, Repository, error) {
	ctx, span := storeTracer.Start(ctx, "source.pg.checkout_grant.consume")
	defer span.End()

	tokenHash := hashToken(token)
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CheckoutGrant{}, Repository{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	row := tx.QueryRow(ctx, `
SELECT g.grant_id, g.repo_id, g.org_id, g.actor_id, g.ref, g.path_prefix, g.expires_at, g.created_at,
       r.repo_id, r.org_id, r.created_by, r.name, r.slug, r.description, r.default_branch, r.visibility,
       r.provider, r.provider_owner, r.provider_repo, r.provider_repo_id, r.state, r.version, r.created_at, r.updated_at
FROM source_checkout_grants g
JOIN source_repositories r ON r.repo_id = g.repo_id
WHERE g.grant_id = $1 AND g.token_hash = $2 AND g.consumed_at IS NULL AND g.expires_at > $3 AND r.state = 'active'
FOR UPDATE OF g`, grantID, tokenHash, s.now())
	var grant CheckoutGrant
	var repo Repository
	if err := row.Scan(&grant.GrantID, &grant.RepoID, &grant.OrgID, &grant.ActorID, &grant.Ref, &grant.PathPrefix, &grant.ExpiresAt, &grant.CreatedAt,
		&repo.RepoID, &repo.OrgID, &repo.CreatedBy, &repo.Name, &repo.Slug, &repo.Description, &repo.DefaultBranch, &repo.Visibility,
		&repo.Provider, &repo.ProviderOwner, &repo.ProviderRepo, &repo.ProviderRepoID, &repo.State, &repo.Version, &repo.CreatedAt, &repo.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return CheckoutGrant{}, Repository{}, ErrUnauthorized
		}
		return CheckoutGrant{}, Repository{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if _, err := tx.Exec(ctx, `UPDATE source_checkout_grants SET consumed_at = $2 WHERE grant_id = $1`, grant.GrantID, s.now()); err != nil {
		return CheckoutGrant{}, Repository{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if err := s.insertEventTx(ctx, tx, grant.OrgID, grant.ActorID, grant.RepoID, "source.checkout_grant.consumed", "allowed", map[string]any{
		"grant_id": grant.GrantID.String(),
		"ref":      grant.Ref,
	}); err != nil {
		return CheckoutGrant{}, Repository{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CheckoutGrant{}, Repository{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	span.SetAttributes(attribute.String("source.checkout_grant_id", grant.GrantID.String()), attribute.String("source.repo_id", repo.RepoID.String()))
	return grant, repo, nil
}

func (s Store) CreateExternalIntegration(ctx context.Context, principal Principal, provider, externalRepo, credentialRef string) (ExternalIntegration, error) {
	ctx, span := storeTracer.Start(ctx, "source.pg.integration.create")
	defer span.End()

	provider = strings.TrimSpace(provider)
	externalRepo = strings.TrimSpace(externalRepo)
	if provider == "" || externalRepo == "" || len(provider) > 64 || len(externalRepo) > 512 {
		return ExternalIntegration{}, ErrInvalid
	}
	now := s.now()
	integration := ExternalIntegration{
		IntegrationID: uuid.New(),
		OrgID:         principal.OrgID,
		CreatedBy:     principal.Subject,
		Provider:      provider,
		ExternalRepo:  externalRepo,
		CredentialRef: strings.TrimSpace(credentialRef),
		State:         "active",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ExternalIntegration{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	_, err = tx.Exec(ctx, `
INSERT INTO source_external_integrations (
    integration_id, org_id, created_by, provider, external_repo, credential_ref, state, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		integration.IntegrationID, integration.OrgID, integration.CreatedBy, integration.Provider, integration.ExternalRepo, integration.CredentialRef, integration.State, integration.CreatedAt, integration.UpdatedAt)
	if err != nil {
		return ExternalIntegration{}, storeWriteError(err)
	}
	if err := s.insertEventTx(ctx, tx, principal.OrgID, principal.Subject, uuid.Nil, "source.integration.created", "allowed", map[string]any{
		"integration_id": integration.IntegrationID.String(),
		"provider":       integration.Provider,
		"external_repo":  integration.ExternalRepo,
	}); err != nil {
		return ExternalIntegration{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ExternalIntegration{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	span.SetAttributes(attribute.String("source.integration_id", integration.IntegrationID.String()))
	return integration, nil
}

func (s Store) CreateWorkflowRun(ctx context.Context, principal Principal, repo Repository, req WorkflowDispatchRequest) (WorkflowRun, bool, error) {
	ctx, span := storeTracer.Start(ctx, "source.pg.workflow_run.create")
	defer span.End()
	inputs, err := workflowInputsJSON(req.Inputs)
	if err != nil {
		return WorkflowRun{}, false, err
	}
	traceID := traceIDFromContext(ctx)
	now := s.now()
	run := WorkflowRun{
		WorkflowRunID:  uuid.New(),
		OrgID:          principal.OrgID,
		RepoID:         repo.RepoID,
		ActorID:        principal.Subject,
		IdempotencyKey: req.IdempotencyKey,
		Provider:       repo.Provider,
		WorkflowPath:   req.WorkflowPath,
		Ref:            req.Ref,
		Inputs:         req.Inputs,
		State:          WorkflowRunStateDispatching,
		TraceID:        traceID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return WorkflowRun{}, false, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	row := tx.QueryRow(ctx, `
INSERT INTO source_workflow_runs (
    workflow_run_id, org_id, repo_id, actor_id, idempotency_key, provider,
    workflow_path, ref, inputs_json, state, trace_id, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12)
ON CONFLICT (org_id, idempotency_key) DO NOTHING
RETURNING workflow_run_id`,
		run.WorkflowRunID, run.OrgID, run.RepoID, run.ActorID, run.IdempotencyKey, run.Provider,
		run.WorkflowPath, run.Ref, inputs, run.State, run.TraceID, now)
	var inserted uuid.UUID
	if err := row.Scan(&inserted); err != nil {
		if err == pgx.ErrNoRows {
			existing, loadErr := s.GetWorkflowRunByIdempotencyKey(ctx, principal.OrgID, req.IdempotencyKey)
			if loadErr == nil && !workflowRunMatchesRequest(existing, principal, repo, req) {
				return WorkflowRun{}, false, fmt.Errorf("%w: idempotency key reused with different workflow dispatch", ErrConflict)
			}
			return existing, false, loadErr
		}
		return WorkflowRun{}, false, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if err := s.insertEventTx(ctx, tx, principal.OrgID, principal.Subject, repo.RepoID, "source.workflow.dispatch.requested", "allowed", map[string]any{
		"workflow_run_id": run.WorkflowRunID.String(),
		"workflow_path":   run.WorkflowPath,
		"ref":             run.Ref,
		"provider":        run.Provider,
	}); err != nil {
		return WorkflowRun{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return WorkflowRun{}, false, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	span.SetAttributes(attribute.String("source.workflow_run_id", run.WorkflowRunID.String()), attribute.String("source.repo_id", repo.RepoID.String()))
	return run, true, nil
}

func (s Store) GetWorkflowRunByIdempotencyKey(ctx context.Context, orgID uint64, idempotencyKey string) (WorkflowRun, error) {
	row := s.PG.QueryRow(ctx, `
SELECT workflow_run_id, org_id, repo_id, actor_id, idempotency_key, provider, workflow_path, ref,
       inputs_json, state, provider_dispatch_id, failure_reason, trace_id, dispatched_at, created_at, updated_at
FROM source_workflow_runs
WHERE org_id = $1 AND idempotency_key = $2`, orgID, strings.TrimSpace(idempotencyKey))
	return scanWorkflowRun(row)
}

func (s Store) GetWorkflowRun(ctx context.Context, orgID uint64, workflowRunID uuid.UUID) (WorkflowRun, error) {
	ctx, span := storeTracer.Start(ctx, "source.pg.workflow_run.get")
	defer span.End()
	span.SetAttributes(attribute.String("source.workflow_run_id", workflowRunID.String()), attribute.Int64("forge_metal.org_id", int64(orgID)))
	row := s.PG.QueryRow(ctx, `
SELECT workflow_run_id, org_id, repo_id, actor_id, idempotency_key, provider, workflow_path, ref,
       inputs_json, state, provider_dispatch_id, failure_reason, trace_id, dispatched_at, created_at, updated_at
FROM source_workflow_runs
WHERE org_id = $1 AND workflow_run_id = $2`, orgID, workflowRunID)
	return scanWorkflowRun(row)
}

func (s Store) ListWorkflowRuns(ctx context.Context, orgID uint64, repoID uuid.UUID) ([]WorkflowRun, error) {
	ctx, span := storeTracer.Start(ctx, "source.pg.workflow_run.list")
	defer span.End()
	span.SetAttributes(attribute.String("source.repo_id", repoID.String()), attribute.Int64("forge_metal.org_id", int64(orgID)))
	rows, err := s.PG.Query(ctx, `
SELECT workflow_run_id, org_id, repo_id, actor_id, idempotency_key, provider, workflow_path, ref,
       inputs_json, state, provider_dispatch_id, failure_reason, trace_id, dispatched_at, created_at, updated_at
FROM source_workflow_runs
WHERE org_id = $1 AND repo_id = $2
ORDER BY created_at DESC, workflow_run_id DESC
LIMIT 100`, orgID, repoID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rows.Close()
	out := []WorkflowRun{}
	for rows.Next() {
		run, err := scanWorkflowRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return out, nil
}

func (s Store) MarkWorkflowRunDispatched(ctx context.Context, run WorkflowRun, providerDispatchID string) (WorkflowRun, error) {
	now := s.now()
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return WorkflowRun{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	row := tx.QueryRow(ctx, `
UPDATE source_workflow_runs
SET state = 'dispatched', provider_dispatch_id = $2, dispatched_at = $3, updated_at = $3
WHERE workflow_run_id = $1
RETURNING workflow_run_id, org_id, repo_id, actor_id, idempotency_key, provider, workflow_path, ref,
       inputs_json, state, provider_dispatch_id, failure_reason, trace_id, dispatched_at, created_at, updated_at`,
		run.WorkflowRunID, strings.TrimSpace(providerDispatchID), now)
	updated, err := scanWorkflowRun(row)
	if err != nil {
		return WorkflowRun{}, err
	}
	if err := s.insertEventTx(ctx, tx, updated.OrgID, updated.ActorID, updated.RepoID, "source.workflow.dispatched", "allowed", map[string]any{
		"workflow_run_id": updated.WorkflowRunID.String(),
		"workflow_path":   updated.WorkflowPath,
		"ref":             updated.Ref,
		"provider":        updated.Provider,
	}); err != nil {
		return WorkflowRun{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return WorkflowRun{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return updated, nil
}

func (s Store) MarkWorkflowRunFailed(ctx context.Context, run WorkflowRun, failureReason string) error {
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	_, err = tx.Exec(ctx, `
UPDATE source_workflow_runs
SET state = 'failed', failure_reason = $2, updated_at = $3
WHERE workflow_run_id = $1`, run.WorkflowRunID, strings.TrimSpace(failureReason), s.now())
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if err := s.insertEventTx(ctx, tx, run.OrgID, run.ActorID, run.RepoID, "source.workflow.dispatch_failed", "error", map[string]any{
		"workflow_run_id": run.WorkflowRunID.String(),
		"workflow_path":   run.WorkflowPath,
		"ref":             run.Ref,
		"provider":        run.Provider,
		"failure_reason":  strings.TrimSpace(failureReason),
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s Store) InsertStorageEvent(ctx context.Context, orgID uint64, repoID uuid.UUID, provider, objectKind, eventType string, byteCount int64, details map[string]any) error {
	data, err := json.Marshal(details)
	if err != nil {
		return err
	}
	var repoValue any
	if repoID != uuid.Nil {
		repoValue = repoID
	}
	_, err = s.PG.Exec(ctx, `
INSERT INTO source_storage_events (
    storage_event_id, org_id, repo_id, provider, storage_object_kind, event_type, byte_count, trace_id, details, measured_at, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10)`,
		uuid.New(), orgID, repoValue, strings.TrimSpace(provider), strings.TrimSpace(objectKind), strings.TrimSpace(eventType), byteCount, traceIDFromContext(ctx), data, s.now())
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s Store) RecordWebhookDelivery(ctx context.Context, delivery WebhookDelivery) error {
	if delivery.WebhookDeliveryID == uuid.Nil {
		delivery.WebhookDeliveryID = uuid.New()
	}
	if delivery.TraceID == "" {
		delivery.TraceID = traceIDFromContext(ctx)
	}
	data, err := json.Marshal(delivery.Details)
	if err != nil {
		return err
	}
	var orgValue any
	if delivery.ResolvedOrgID != 0 {
		orgValue = delivery.ResolvedOrgID
	}
	var repoValue any
	if delivery.ResolvedRepoID != uuid.Nil {
		repoValue = delivery.ResolvedRepoID
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	_, err = tx.Exec(ctx, `
INSERT INTO source_webhook_deliveries (
    webhook_delivery_id, provider, delivery_id, event_type, signature_valid, result,
    resolved_org_id, resolved_repo_id, trace_id, details, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (provider, delivery_id) DO UPDATE SET
    event_type = EXCLUDED.event_type,
    signature_valid = EXCLUDED.signature_valid,
    result = EXCLUDED.result,
    resolved_org_id = EXCLUDED.resolved_org_id,
    resolved_repo_id = EXCLUDED.resolved_repo_id,
    trace_id = EXCLUDED.trace_id,
    details = EXCLUDED.details`,
		delivery.WebhookDeliveryID, delivery.Provider, delivery.DeliveryID, delivery.EventType, delivery.SignatureValid, delivery.Result,
		orgValue, repoValue, delivery.TraceID, data, s.now())
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if delivery.ResolvedOrgID != 0 && delivery.ResolvedRepoID != uuid.Nil {
		result := "allowed"
		if delivery.Result != "accepted" {
			result = "error"
		}
		if err := s.insertEventTx(ctx, tx, delivery.ResolvedOrgID, "system:webhook", delivery.ResolvedRepoID, "source.webhook."+delivery.EventType, result, map[string]any{
			"provider": delivery.Provider,
			"delivery": delivery.DeliveryID,
			"result":   delivery.Result,
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s Store) InsertEvent(ctx context.Context, orgID uint64, actorID string, repoID uuid.UUID, eventType, result string, details map[string]any) error {
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	if err := s.insertEventTx(ctx, tx, orgID, actorID, repoID, eventType, result, details); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Store) insertEventTx(ctx context.Context, tx pgx.Tx, orgID uint64, actorID string, repoID uuid.UUID, eventType, result string, details map[string]any) error {
	traceID := traceIDFromContext(ctx)
	data, err := json.Marshal(details)
	if err != nil {
		return err
	}
	var repoValue any
	if repoID != uuid.Nil {
		repoValue = repoID
	}
	_, err = tx.Exec(ctx, `
INSERT INTO source_events (event_id, org_id, actor_id, repo_id, event_type, result, trace_id, details, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, uuid.New(), orgID, actorID, repoValue, eventType, result, traceID, data, s.now())
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

type repositoryScanner interface {
	Scan(dest ...any) error
}

func scanRepository(row repositoryScanner) (Repository, error) {
	var repo Repository
	if err := row.Scan(&repo.RepoID, &repo.OrgID, &repo.CreatedBy, &repo.Name, &repo.Slug, &repo.Description, &repo.DefaultBranch, &repo.Visibility,
		&repo.Provider, &repo.ProviderOwner, &repo.ProviderRepo, &repo.ProviderRepoID, &repo.State, &repo.Version, &repo.CreatedAt, &repo.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return Repository{}, ErrNotFound
		}
		return Repository{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return repo, nil
}

type workflowRunScanner interface {
	Scan(dest ...any) error
}

func scanWorkflowRun(row workflowRunScanner) (WorkflowRun, error) {
	var (
		run        WorkflowRun
		inputsJSON []byte
	)
	if err := row.Scan(
		&run.WorkflowRunID,
		&run.OrgID,
		&run.RepoID,
		&run.ActorID,
		&run.IdempotencyKey,
		&run.Provider,
		&run.WorkflowPath,
		&run.Ref,
		&inputsJSON,
		&run.State,
		&run.ProviderDispatchID,
		&run.FailureReason,
		&run.TraceID,
		&run.DispatchedAt,
		&run.CreatedAt,
		&run.UpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return WorkflowRun{}, ErrNotFound
		}
		return WorkflowRun{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if len(inputsJSON) == 0 {
		run.Inputs = map[string]string{}
	} else if err := json.Unmarshal(inputsJSON, &run.Inputs); err != nil {
		return WorkflowRun{}, fmt.Errorf("%w: decode workflow inputs: %v", ErrStoreUnavailable, err)
	}
	if run.Inputs == nil {
		run.Inputs = map[string]string{}
	}
	return run, nil
}

func workflowRunMatchesRequest(run WorkflowRun, principal Principal, repo Repository, req WorkflowDispatchRequest) bool {
	return run.OrgID == principal.OrgID &&
		run.ActorID == principal.Subject &&
		run.RepoID == repo.RepoID &&
		run.Provider == repo.Provider &&
		run.WorkflowPath == req.WorkflowPath &&
		run.Ref == req.Ref &&
		maps.Equal(run.Inputs, req.Inputs)
}

func traceIDFromContext(ctx context.Context) string {
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.HasTraceID() {
		return spanContext.TraceID().String()
	}
	return ""
}

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func storeWriteError(err error) error {
	if pgErr, ok := err.(*pgconn.PgError); ok && pgErr.Code == "23505" {
		return fmt.Errorf("%w: %v", ErrConflict, err)
	}
	return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
}

func newGrantToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(raw)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
