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

func (s Store) UpsertInstallation(ctx context.Context, baseURL, owner string) error {
	ctx, span := storeTracer.Start(ctx, "source.pg.installation.upsert")
	defer span.End()

	_, err := s.PG.Exec(ctx, `
INSERT INTO forgejo_installations (installation_id, base_url, owner_username, created_at, updated_at)
VALUES ($1, $2, $3, $4, $4)
ON CONFLICT (installation_id)
DO UPDATE SET base_url = EXCLUDED.base_url, owner_username = EXCLUDED.owner_username, updated_at = EXCLUDED.updated_at`,
		uuid.MustParse("00000000-0000-0000-0000-000000000001"), baseURL, owner, s.now())
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
		RepoID:        uuid.New(),
		OrgID:         principal.OrgID,
		CreatedBy:     principal.Subject,
		Name:          req.Name,
		Slug:          NormalizeSlug(req.Name),
		Description:   req.Description,
		DefaultBranch: req.DefaultBranch,
		Visibility:    "private",
		ForgejoOwner:  owner,
		ForgejoRepo:   forgejoRepo.Name,
		ForgejoRepoID: forgejoRepo.ID,
		State:         "active",
		Version:       1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Repository{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	_, err = tx.Exec(ctx, `
INSERT INTO source_repositories (
    repo_id, org_id, created_by, name, slug, description, default_branch, visibility,
    forgejo_owner, forgejo_repo, forgejo_repo_id, state, version, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		repo.RepoID, repo.OrgID, repo.CreatedBy, repo.Name, repo.Slug, repo.Description, repo.DefaultBranch, repo.Visibility,
		repo.ForgejoOwner, repo.ForgejoRepo, repo.ForgejoRepoID, repo.State, repo.Version, repo.CreatedAt, repo.UpdatedAt)
	if err != nil {
		return Repository{}, storeWriteError(err)
	}
	if err := s.insertEventTx(ctx, tx, principal.OrgID, principal.Subject, repo.RepoID, "source.repo.created", "allowed", map[string]any{
		"name":          repo.Name,
		"slug":          repo.Slug,
		"forgejo_owner": repo.ForgejoOwner,
		"forgejo_repo":  repo.ForgejoRepo,
	}); err != nil {
		return Repository{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Repository{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	span.SetAttributes(attribute.String("source.repo_id", repo.RepoID.String()), attribute.Int64("source.forgejo_repo_id", repo.ForgejoRepoID))
	return repo, nil
}

func (s Store) ListRepositories(ctx context.Context, orgID uint64) ([]Repository, error) {
	ctx, span := storeTracer.Start(ctx, "source.pg.repo.list")
	defer span.End()
	span.SetAttributes(attribute.Int64("forge_metal.org_id", int64(orgID)))

	rows, err := s.PG.Query(ctx, `
SELECT repo_id, org_id, created_by, name, slug, description, default_branch, visibility,
       forgejo_owner, forgejo_repo, forgejo_repo_id, state, version, created_at, updated_at
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
       forgejo_owner, forgejo_repo, forgejo_repo_id, state, version, created_at, updated_at
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
       r.forgejo_owner, r.forgejo_repo, r.forgejo_repo_id, r.state, r.version, r.created_at, r.updated_at
FROM source_checkout_grants g
JOIN source_repositories r ON r.repo_id = g.repo_id
WHERE g.grant_id = $1 AND g.token_hash = $2 AND g.consumed_at IS NULL AND g.expires_at > $3 AND r.state = 'active'
FOR UPDATE OF g`, grantID, tokenHash, s.now())
	var grant CheckoutGrant
	var repo Repository
	if err := row.Scan(&grant.GrantID, &grant.RepoID, &grant.OrgID, &grant.ActorID, &grant.Ref, &grant.PathPrefix, &grant.ExpiresAt, &grant.CreatedAt,
		&repo.RepoID, &repo.OrgID, &repo.CreatedBy, &repo.Name, &repo.Slug, &repo.Description, &repo.DefaultBranch, &repo.Visibility,
		&repo.ForgejoOwner, &repo.ForgejoRepo, &repo.ForgejoRepoID, &repo.State, &repo.Version, &repo.CreatedAt, &repo.UpdatedAt); err != nil {
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

func (s Store) RecordWebhook(ctx context.Context, provider, event, delivery string, valid bool) error {
	result := "allowed"
	if !valid {
		result = "denied"
	}
	return s.InsertEvent(ctx, 1, "system:webhook", uuid.Nil, "source.webhook."+event, result, map[string]any{
		"provider": provider,
		"delivery": delivery,
	})
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
	traceID := ""
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.HasTraceID() {
		traceID = spanContext.TraceID().String()
	}
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
		&repo.ForgejoOwner, &repo.ForgejoRepo, &repo.ForgejoRepoID, &repo.State, &repo.Version, &repo.CreatedAt, &repo.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return Repository{}, ErrNotFound
		}
		return Repository{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return repo, nil
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
