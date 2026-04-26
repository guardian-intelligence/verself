package source

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	sourcestore "github.com/verself/source-code-hosting-service/internal/store"
)

var storeTracer = otel.Tracer("source-code-hosting-service/store")

const maxPostgresBigint = uint64(1<<63 - 1)

type Store struct {
	PG  *pgxpool.Pool
	Now func() time.Time
}

func (s Store) q() *sourcestore.Queries {
	return sourcestore.New(s.PG)
}

func (s Store) Ready(ctx context.Context) error {
	if s.PG == nil {
		return ErrStoreUnavailable
	}
	if _, err := s.q().Ping(ctx); err != nil {
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
	repo, err := s.createRepository(ctx, principal, req, owner, forgejoRepo, "source.repo.created", "api")
	if err != nil {
		return Repository{}, err
	}
	span.SetAttributes(
		attribute.String("source.repo_id", repo.RepoID.String()),
		attribute.String("source.backend_repo_id", repo.Backend.BackendRepoID),
	)
	return repo, nil
}

func (s Store) createRepository(ctx context.Context, principal Principal, req CreateRepositoryRequest, owner string, forgejoRepo forgejoRepo, eventType string, origin string) (Repository, error) {
	orgID, err := pgOrgID(principal.OrgID)
	if err != nil {
		return Repository{}, err
	}
	now := s.now()
	repo := Repository{
		RepoID:        uuid.New(),
		OrgID:         principal.OrgID,
		ProjectID:     req.ProjectID,
		CreatedBy:     principal.Subject,
		Name:          req.Name,
		Slug:          NormalizeSlug(req.Name),
		Description:   req.Description,
		DefaultBranch: req.DefaultBranch,
		Visibility:    "private",
		State:         "active",
		Version:       1,
		CreatedAt:     now,
		UpdatedAt:     now,
		Backend: RepositoryBackend{
			BackendID:     uuid.New(),
			Backend:       BackendForgejo,
			BackendOwner:  strings.TrimSpace(owner),
			BackendRepo:   forgejoRepo.Name,
			BackendRepoID: fmt.Sprintf("%d", forgejoRepo.ID),
			State:         "active",
			CreatedAt:     now,
			UpdatedAt:     now,
		},
	}
	repo.Backend.RepoID = repo.RepoID

	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Repository{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	q := sourcestore.New(tx)
	if err := q.InsertRepository(ctx, sourcestore.InsertRepositoryParams{
		RepoID:        repo.RepoID,
		OrgID:         orgID,
		ProjectID:     repo.ProjectID,
		CreatedBy:     repo.CreatedBy,
		Name:          repo.Name,
		Slug:          repo.Slug,
		Description:   repo.Description,
		DefaultBranch: repo.DefaultBranch,
		Visibility:    repo.Visibility,
		State:         repo.State,
		Version:       repo.Version,
		CreatedAt:     timestamptz(repo.CreatedAt),
	}); err != nil {
		return Repository{}, storeWriteError(err)
	}
	if err := q.InsertRepositoryBackend(ctx, sourcestore.InsertRepositoryBackendParams{
		BackendID:     repo.Backend.BackendID,
		RepoID:        repo.Backend.RepoID,
		Backend:       repo.Backend.Backend,
		BackendOwner:  repo.Backend.BackendOwner,
		BackendRepo:   repo.Backend.BackendRepo,
		BackendRepoID: repo.Backend.BackendRepoID,
		State:         repo.Backend.State,
		CreatedAt:     timestamptz(repo.Backend.CreatedAt),
	}); err != nil {
		return Repository{}, storeWriteError(err)
	}
	if err := s.insertEventTx(ctx, q, principal.OrgID, principal.Subject, repo.RepoID, eventType, "allowed", map[string]any{
		"name":         repo.Name,
		"slug":         repo.Slug,
		"project_id":   repo.ProjectID.String(),
		"origin":       origin,
		"backend":      repo.Backend.Backend,
		"backend_repo": repo.Backend.BackendRepo,
	}); err != nil {
		return Repository{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Repository{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return repo, nil
}

func (s Store) ListRepositories(ctx context.Context, orgID uint64, projectID uuid.UUID) ([]Repository, error) {
	ctx, span := storeTracer.Start(ctx, "source.pg.repo.list")
	defer span.End()
	span.SetAttributes(attribute.Int64("verself.org_id", int64(orgID)))

	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return nil, err
	}
	q := s.q()
	if projectID != uuid.Nil {
		span.SetAttributes(attribute.String("verself.project_id", projectID.String()))
		rows, err := q.ListRepositoriesByProject(ctx, sourcestore.ListRepositoriesByProjectParams{
			OrgID:     pgOrg,
			ProjectID: projectID,
			Backend:   BackendForgejo,
		})
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
		}
		repos, err := repositoriesFromProjectRows(rows)
		if err != nil {
			return nil, err
		}
		span.SetAttributes(attribute.Int("source.repo_count", len(repos)))
		return repos, nil
	}
	rows, err := q.ListRepositories(ctx, sourcestore.ListRepositoriesParams{OrgID: pgOrg, Backend: BackendForgejo})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	repos, err := repositoriesFromListRows(rows)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(attribute.Int("source.repo_count", len(repos)))
	return repos, nil
}

func (s Store) GetRepository(ctx context.Context, orgID uint64, repoID uuid.UUID) (Repository, error) {
	ctx, span := storeTracer.Start(ctx, "source.pg.repo.get")
	defer span.End()
	span.SetAttributes(attribute.String("source.repo_id", repoID.String()), attribute.Int64("verself.org_id", int64(orgID)))

	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return Repository{}, err
	}
	row, err := s.q().GetRepository(ctx, sourcestore.GetRepositoryParams{OrgID: pgOrg, RepoID: repoID, Backend: BackendForgejo})
	if err != nil {
		return Repository{}, notFoundOrStoreError(err)
	}
	return repositoryFromRow(repositoryRow(row))
}

func (s Store) GetRepositoryByProject(ctx context.Context, orgID uint64, projectID uuid.UUID) (Repository, error) {
	ctx, span := storeTracer.Start(ctx, "source.pg.repo.get_by_project")
	defer span.End()
	span.SetAttributes(attribute.String("verself.project_id", projectID.String()), attribute.Int64("verself.org_id", int64(orgID)))
	if orgID == 0 || projectID == uuid.Nil {
		return Repository{}, ErrInvalid
	}
	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return Repository{}, err
	}
	row, err := s.q().GetRepositoryByProject(ctx, sourcestore.GetRepositoryByProjectParams{OrgID: pgOrg, ProjectID: projectID, Backend: BackendForgejo})
	if err != nil {
		return Repository{}, notFoundOrStoreError(err)
	}
	return repositoryFromRow(repositoryRow(row))
}

func (s Store) FindRepositoryByBackend(ctx context.Context, backend, backendOwner, backendRepo, backendRepoID string) (Repository, error) {
	ctx, span := storeTracer.Start(ctx, "source.pg.repo.resolve_backend")
	defer span.End()
	backend = strings.TrimSpace(backend)
	backendOwner = strings.TrimSpace(backendOwner)
	backendRepo = strings.TrimSpace(backendRepo)
	backendRepoID = strings.TrimSpace(backendRepoID)
	span.SetAttributes(
		attribute.String("source.backend", backend),
		attribute.String("source.backend_repo_id", backendRepoID),
	)
	if backend == "" || (backendRepoID == "" && (backendOwner == "" || backendRepo == "")) {
		return Repository{}, ErrInvalid
	}
	row, err := s.q().FindRepositoryByBackend(ctx, sourcestore.FindRepositoryByBackendParams{
		Backend:       backend,
		BackendOwner:  backendOwner,
		BackendRepo:   backendRepo,
		BackendRepoID: backendRepoID,
	})
	if err != nil {
		return Repository{}, notFoundOrStoreError(err)
	}
	repo, err := repositoryFromRow(repositoryRow(row))
	if err != nil {
		return Repository{}, err
	}
	span.SetAttributes(attribute.String("source.repo_id", repo.RepoID.String()), attribute.Int64("verself.org_id", int64(repo.OrgID)))
	return repo, nil
}

func (s Store) CreateGitCredential(ctx context.Context, principal Principal, credential GitCredential) (GitCredential, error) {
	ctx, span := storeTracer.Start(ctx, "source.pg.git_credential.create")
	defer span.End()
	if err := ValidatePrincipal(principal); err != nil {
		return GitCredential{}, err
	}
	orgID, err := pgOrgID(principal.OrgID)
	if err != nil {
		return GitCredential{}, err
	}
	now := s.now()
	if credential.CredentialID == uuid.Nil || credential.OrgID != principal.OrgID || credential.ActorID == "" || credential.TokenPrefix == "" || credential.ExpiresAt.IsZero() {
		return GitCredential{}, ErrInvalid
	}
	if credential.Username == "" {
		credential.Username = GitCredentialUsername
	}
	if credential.State == "" {
		credential.State = "active"
	}
	if credential.CreatedAt.IsZero() {
		credential.CreatedAt = now
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return GitCredential{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	q := sourcestore.New(tx)
	if err := q.InsertGitCredential(ctx, sourcestore.InsertGitCredentialParams{
		CredentialID: credential.CredentialID,
		OrgID:        orgID,
		ActorID:      credential.ActorID,
		Label:        credential.Label,
		Username:     credential.Username,
		TokenPrefix:  credential.TokenPrefix,
		Scopes:       credential.Scopes,
		State:        credential.State,
		ExpiresAt:    timestamptz(credential.ExpiresAt),
		CreatedAt:    timestamptz(credential.CreatedAt),
	}); err != nil {
		return GitCredential{}, storeWriteError(err)
	}
	if err := s.insertEventTx(ctx, q, principal.OrgID, principal.Subject, uuid.Nil, "source.git_credential.created", "allowed", map[string]any{
		"credential_id": credential.CredentialID.String(),
		"token_prefix":  credential.TokenPrefix,
		"expires_at":    credential.ExpiresAt.Format(time.RFC3339),
	}); err != nil {
		return GitCredential{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return GitCredential{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	span.SetAttributes(attribute.String("source.git_credential_id", credential.CredentialID.String()), attribute.Int64("verself.org_id", int64(principal.OrgID)))
	return credential, nil
}

func (s Store) MarkGitCredentialUsed(ctx context.Context, credentialID uuid.UUID) (GitPrincipal, error) {
	ctx, span := storeTracer.Start(ctx, "source.pg.git_credential.mark_used")
	defer span.End()
	if credentialID == uuid.Nil {
		return GitPrincipal{}, ErrUnauthorized
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return GitPrincipal{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	q := sourcestore.New(tx)
	now := s.now()
	row, err := q.LockActiveGitCredentialForUse(ctx, sourcestore.LockActiveGitCredentialForUseParams{
		CredentialID: credentialID,
		ExpiresAt:    timestamptz(now),
	})
	if err != nil {
		return GitPrincipal{}, unauthorizedOrStoreError(err)
	}
	principal, err := gitPrincipalFromRow(row)
	if err != nil {
		return GitPrincipal{}, err
	}
	if err := q.MarkGitCredentialUsed(ctx, sourcestore.MarkGitCredentialUsedParams{
		CredentialID: principal.CredentialID,
		LastUsedAt:   timestamptz(now),
	}); err != nil {
		return GitPrincipal{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if err := s.insertEventTx(ctx, q, principal.OrgID, principal.ActorID, uuid.Nil, "source.git_credential.used", "allowed", map[string]any{
		"credential_id": principal.CredentialID.String(),
	}); err != nil {
		return GitPrincipal{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return GitPrincipal{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	span.SetAttributes(attribute.String("source.git_credential_id", principal.CredentialID.String()), attribute.Int64("verself.org_id", int64(principal.OrgID)))
	return principal, nil
}

func (s Store) ReplaceRefs(ctx context.Context, actorID string, repo Repository, refs []Ref) error {
	ctx, span := storeTracer.Start(ctx, "source.pg.refs.replace")
	defer span.End()
	orgID, err := pgOrgID(repo.OrgID)
	if err != nil {
		return err
	}
	now := s.now()
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	q := sourcestore.New(tx)
	if err := q.DeleteRefsForRepository(ctx, sourcestore.DeleteRefsForRepositoryParams{RepoID: repo.RepoID}); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	for _, ref := range refs {
		ref.Name = strings.TrimSpace(ref.Name)
		ref.Commit = strings.TrimSpace(ref.Commit)
		if ref.Name == "" || ref.Commit == "" {
			continue
		}
		if err := q.InsertRefHead(ctx, sourcestore.InsertRefHeadParams{
			RepoID:    repo.RepoID,
			OrgID:     orgID,
			RefName:   ref.Name,
			CommitSha: ref.Commit,
			IsDefault: ref.Name == repo.DefaultBranch,
			PushedAt:  timestamptz(now),
		}); err != nil {
			return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
		}
	}
	if err := q.MarkRepositoryRefsPushed(ctx, sourcestore.MarkRepositoryRefsPushedParams{RepoID: repo.RepoID, LastPushedAt: timestamptz(now)}); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if err := s.insertEventTx(ctx, q, repo.OrgID, actorID, repo.RepoID, "source.git.refs_refreshed", "allowed", map[string]any{
		"ref_count": len(refs),
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	span.SetAttributes(attribute.String("source.repo_id", repo.RepoID.String()), attribute.Int("source.ref_count", len(refs)))
	return nil
}

func (s Store) ListRefs(ctx context.Context, orgID uint64, repoID uuid.UUID) ([]Ref, error) {
	ctx, span := storeTracer.Start(ctx, "source.pg.refs.list_cached")
	defer span.End()
	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q().ListRefs(ctx, sourcestore.ListRefsParams{OrgID: pgOrg, RepoID: repoID})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	refs := make([]Ref, 0, len(rows))
	for _, row := range rows {
		refs = append(refs, Ref{Name: row.RefName, Commit: row.CommitSha})
	}
	span.SetAttributes(attribute.String("source.repo_id", repoID.String()), attribute.Int("source.ref_count", len(refs)))
	return refs, nil
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
	orgID, err := pgOrgID(principal.OrgID)
	if err != nil {
		return CheckoutGrant{}, err
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
	q := sourcestore.New(tx)
	if err := q.InsertCheckoutGrant(ctx, sourcestore.InsertCheckoutGrantParams{
		GrantID:    grant.GrantID,
		RepoID:     grant.RepoID,
		OrgID:      orgID,
		ActorID:    grant.ActorID,
		Ref:        grant.Ref,
		PathPrefix: grant.PathPrefix,
		TokenHash:  tokenHash,
		ExpiresAt:  timestamptz(grant.ExpiresAt),
		CreatedAt:  timestamptz(grant.CreatedAt),
	}); err != nil {
		return CheckoutGrant{}, storeWriteError(err)
	}
	if err := s.insertEventTx(ctx, q, principal.OrgID, principal.Subject, repo.RepoID, "source.checkout_grant.created", "allowed", map[string]any{
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
	q := sourcestore.New(tx)
	now := s.now()
	row, err := q.LockCheckoutGrantForConsume(ctx, sourcestore.LockCheckoutGrantForConsumeParams{
		GrantID:   grantID,
		TokenHash: tokenHash,
		ExpiresAt: timestamptz(now),
		Backend:   BackendForgejo,
	})
	if err != nil {
		return CheckoutGrant{}, Repository{}, unauthorizedOrStoreError(err)
	}
	grant, err := checkoutGrantFromLockedRow(row)
	if err != nil {
		return CheckoutGrant{}, Repository{}, err
	}
	repo, err := repositoryFromLockedGrantRow(row)
	if err != nil {
		return CheckoutGrant{}, Repository{}, err
	}
	if err := q.ConsumeCheckoutGrant(ctx, sourcestore.ConsumeCheckoutGrantParams{GrantID: grant.GrantID, ConsumedAt: timestamptz(now)}); err != nil {
		return CheckoutGrant{}, Repository{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if err := s.insertEventTx(ctx, q, grant.OrgID, grant.ActorID, grant.RepoID, "source.checkout_grant.consumed", "allowed", map[string]any{
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

func (s Store) CreateWorkflowRun(ctx context.Context, principal Principal, repo Repository, req WorkflowDispatchRequest) (WorkflowRun, bool, error) {
	ctx, span := storeTracer.Start(ctx, "source.pg.workflow_run.create")
	defer span.End()
	orgID, err := pgOrgID(principal.OrgID)
	if err != nil {
		return WorkflowRun{}, false, err
	}
	inputs, err := workflowInputsJSON(req.Inputs)
	if err != nil {
		return WorkflowRun{}, false, err
	}
	traceID := traceIDFromContext(ctx)
	now := s.now()
	run := WorkflowRun{
		WorkflowRunID:  uuid.New(),
		OrgID:          principal.OrgID,
		ProjectID:      repo.ProjectID,
		RepoID:         repo.RepoID,
		ActorID:        principal.Subject,
		IdempotencyKey: req.IdempotencyKey,
		Backend:        repo.Backend.Backend,
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
	q := sourcestore.New(tx)
	_, err = q.InsertWorkflowRun(ctx, sourcestore.InsertWorkflowRunParams{
		WorkflowRunID:  run.WorkflowRunID,
		OrgID:          orgID,
		ProjectID:      run.ProjectID,
		RepoID:         run.RepoID,
		ActorID:        run.ActorID,
		IdempotencyKey: run.IdempotencyKey,
		Backend:        run.Backend,
		WorkflowPath:   run.WorkflowPath,
		Ref:            run.Ref,
		InputsJson:     inputs,
		State:          run.State,
		TraceID:        run.TraceID,
		CreatedAt:      timestamptz(now),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			existing, loadErr := workflowRunByIdempotencyKey(ctx, q, orgID, req.IdempotencyKey)
			if loadErr == nil && !workflowRunMatchesRequest(existing, principal, repo, req) {
				return WorkflowRun{}, false, fmt.Errorf("%w: idempotency key reused with different workflow dispatch", ErrConflict)
			}
			return existing, false, loadErr
		}
		return WorkflowRun{}, false, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if err := s.insertEventTx(ctx, q, principal.OrgID, principal.Subject, repo.RepoID, "source.workflow.dispatch.requested", "allowed", map[string]any{
		"workflow_run_id": run.WorkflowRunID.String(),
		"project_id":      run.ProjectID.String(),
		"workflow_path":   run.WorkflowPath,
		"ref":             run.Ref,
		"backend":         run.Backend,
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
	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return WorkflowRun{}, err
	}
	return workflowRunByIdempotencyKey(ctx, s.q(), pgOrg, idempotencyKey)
}

func (s Store) GetWorkflowRun(ctx context.Context, orgID uint64, workflowRunID uuid.UUID) (WorkflowRun, error) {
	ctx, span := storeTracer.Start(ctx, "source.pg.workflow_run.get")
	defer span.End()
	span.SetAttributes(attribute.String("source.workflow_run_id", workflowRunID.String()), attribute.Int64("verself.org_id", int64(orgID)))
	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return WorkflowRun{}, err
	}
	row, err := s.q().GetWorkflowRun(ctx, sourcestore.GetWorkflowRunParams{OrgID: pgOrg, WorkflowRunID: workflowRunID})
	if err != nil {
		return WorkflowRun{}, notFoundOrStoreError(err)
	}
	return workflowRunFromRow(row)
}

func (s Store) ListWorkflowRuns(ctx context.Context, orgID uint64, repoID uuid.UUID) ([]WorkflowRun, error) {
	ctx, span := storeTracer.Start(ctx, "source.pg.workflow_run.list")
	defer span.End()
	span.SetAttributes(attribute.String("source.repo_id", repoID.String()), attribute.Int64("verself.org_id", int64(orgID)))
	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q().ListWorkflowRuns(ctx, sourcestore.ListWorkflowRunsParams{OrgID: pgOrg, RepoID: repoID})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	out := make([]WorkflowRun, 0, len(rows))
	for _, row := range rows {
		run, err := workflowRunFromRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, nil
}

func (s Store) MarkWorkflowRunDispatched(ctx context.Context, run WorkflowRun, backendDispatchID string) (WorkflowRun, error) {
	now := s.now()
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return WorkflowRun{}, fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	q := sourcestore.New(tx)
	row, err := q.MarkWorkflowRunDispatched(ctx, sourcestore.MarkWorkflowRunDispatchedParams{
		WorkflowRunID:     run.WorkflowRunID,
		BackendDispatchID: strings.TrimSpace(backendDispatchID),
		DispatchedAt:      timestamptz(now),
	})
	if err != nil {
		return WorkflowRun{}, notFoundOrStoreError(err)
	}
	updated, err := workflowRunFromRow(row)
	if err != nil {
		return WorkflowRun{}, err
	}
	if err := s.insertEventTx(ctx, q, updated.OrgID, updated.ActorID, updated.RepoID, "source.workflow.dispatched", "allowed", map[string]any{
		"workflow_run_id": updated.WorkflowRunID.String(),
		"project_id":      updated.ProjectID.String(),
		"workflow_path":   updated.WorkflowPath,
		"ref":             updated.Ref,
		"backend":         updated.Backend,
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
	q := sourcestore.New(tx)
	if err := q.MarkWorkflowRunFailed(ctx, sourcestore.MarkWorkflowRunFailedParams{
		WorkflowRunID: run.WorkflowRunID,
		FailureReason: strings.TrimSpace(failureReason),
		UpdatedAt:     timestamptz(s.now()),
	}); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if err := s.insertEventTx(ctx, q, run.OrgID, run.ActorID, run.RepoID, "source.workflow.dispatch_failed", "error", map[string]any{
		"workflow_run_id": run.WorkflowRunID.String(),
		"project_id":      run.ProjectID.String(),
		"workflow_path":   run.WorkflowPath,
		"ref":             run.Ref,
		"backend":         run.Backend,
		"failure_reason":  strings.TrimSpace(failureReason),
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s Store) InsertStorageEvent(ctx context.Context, orgID uint64, repoID uuid.UUID, backend, objectKind, eventType string, byteCount int64, details map[string]any) error {
	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(details)
	if err != nil {
		return err
	}
	if err := s.q().InsertStorageEvent(ctx, sourcestore.InsertStorageEventParams{
		StorageEventID:    uuid.New(),
		OrgID:             pgOrg,
		RepoID:            nullableUUID(repoID),
		Backend:           strings.TrimSpace(backend),
		StorageObjectKind: strings.TrimSpace(objectKind),
		EventType:         strings.TrimSpace(eventType),
		ByteCount:         byteCount,
		TraceID:           traceIDFromContext(ctx),
		Details:           data,
		MeasuredAt:        timestamptz(s.now()),
	}); err != nil {
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
	orgValue, err := nullableOrgID(delivery.ResolvedOrgID)
	if err != nil {
		return err
	}
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	q := sourcestore.New(tx)
	if err := q.UpsertWebhookDelivery(ctx, sourcestore.UpsertWebhookDeliveryParams{
		WebhookDeliveryID: delivery.WebhookDeliveryID,
		Backend:           delivery.Backend,
		DeliveryID:        delivery.DeliveryID,
		EventType:         delivery.EventType,
		SignatureValid:    delivery.SignatureValid,
		Result:            delivery.Result,
		ResolvedOrgID:     orgValue,
		ResolvedProjectID: nullableUUID(delivery.ResolvedProjectID),
		ResolvedRepoID:    nullableUUID(delivery.ResolvedRepoID),
		TraceID:           delivery.TraceID,
		Details:           data,
		CreatedAt:         timestamptz(s.now()),
	}); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	if delivery.ResolvedOrgID != 0 && delivery.ResolvedRepoID != uuid.Nil {
		result := "allowed"
		if delivery.Result != "accepted" {
			result = "error"
		}
		if err := s.insertEventTx(ctx, q, delivery.ResolvedOrgID, "system:webhook", delivery.ResolvedRepoID, "source.webhook."+delivery.EventType, result, map[string]any{
			"backend":  delivery.Backend,
			"delivery": delivery.DeliveryID,
			"result":   delivery.Result,
		}); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s Store) InsertEvent(ctx context.Context, orgID uint64, actorID string, repoID uuid.UUID, eventType, result string, details map[string]any) error {
	tx, err := s.PG.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	defer rollback(ctx, tx)
	q := sourcestore.New(tx)
	if err := s.insertEventTx(ctx, q, orgID, actorID, repoID, eventType, result, details); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

func (s Store) insertEventTx(ctx context.Context, q *sourcestore.Queries, orgID uint64, actorID string, repoID uuid.UUID, eventType, result string, details map[string]any) error {
	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(details)
	if err != nil {
		return err
	}
	if err := q.InsertSourceEvent(ctx, sourcestore.InsertSourceEventParams{
		EventID:   uuid.New(),
		OrgID:     pgOrg,
		ActorID:   actorID,
		RepoID:    nullableUUID(repoID),
		EventType: eventType,
		Result:    result,
		TraceID:   traceIDFromContext(ctx),
		Details:   data,
		CreatedAt: timestamptz(s.now()),
	}); err != nil {
		return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
	}
	return nil
}

type repositoryRow struct {
	RepoID              uuid.UUID
	OrgID               int64
	ProjectID           uuid.UUID
	CreatedBy           string
	Name                string
	Slug                string
	Description         string
	DefaultBranch       string
	Visibility          string
	State               string
	Version             int64
	LastPushedAt        pgtype.Timestamptz
	CreatedAt           pgtype.Timestamptz
	UpdatedAt           pgtype.Timestamptz
	BackendID           uuid.UUID
	BackendSourceRepoID uuid.UUID
	Backend             string
	BackendOwner        string
	BackendRepo         string
	BackendRepoID       string
	BackendState        string
	BackendCreatedAt    pgtype.Timestamptz
	BackendUpdatedAt    pgtype.Timestamptz
}

func repositoriesFromListRows(rows []sourcestore.ListRepositoriesRow) ([]Repository, error) {
	repos := make([]Repository, 0, len(rows))
	for _, row := range rows {
		repo, err := repositoryFromRow(repositoryRow(row))
		if err != nil {
			return nil, err
		}
		repos = append(repos, repo)
	}
	return repos, nil
}

func repositoriesFromProjectRows(rows []sourcestore.ListRepositoriesByProjectRow) ([]Repository, error) {
	repos := make([]Repository, 0, len(rows))
	for _, row := range rows {
		repo, err := repositoryFromRow(repositoryRow(row))
		if err != nil {
			return nil, err
		}
		repos = append(repos, repo)
	}
	return repos, nil
}

func repositoryFromRow(row repositoryRow) (Repository, error) {
	orgID, err := domainOrgID(row.OrgID)
	if err != nil {
		return Repository{}, err
	}
	createdAt, err := requiredTime(row.CreatedAt)
	if err != nil {
		return Repository{}, err
	}
	updatedAt, err := requiredTime(row.UpdatedAt)
	if err != nil {
		return Repository{}, err
	}
	backendCreatedAt, err := requiredTime(row.BackendCreatedAt)
	if err != nil {
		return Repository{}, err
	}
	backendUpdatedAt, err := requiredTime(row.BackendUpdatedAt)
	if err != nil {
		return Repository{}, err
	}
	return Repository{
		RepoID:        row.RepoID,
		OrgID:         orgID,
		ProjectID:     row.ProjectID,
		CreatedBy:     row.CreatedBy,
		Name:          row.Name,
		Slug:          row.Slug,
		Description:   row.Description,
		DefaultBranch: row.DefaultBranch,
		Visibility:    row.Visibility,
		State:         row.State,
		Version:       row.Version,
		LastPushedAt:  timePtr(row.LastPushedAt),
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
		Backend: RepositoryBackend{
			BackendID:     row.BackendID,
			RepoID:        row.BackendSourceRepoID,
			Backend:       row.Backend,
			BackendOwner:  row.BackendOwner,
			BackendRepo:   row.BackendRepo,
			BackendRepoID: row.BackendRepoID,
			State:         row.BackendState,
			CreatedAt:     backendCreatedAt,
			UpdatedAt:     backendUpdatedAt,
		},
	}, nil
}

func repositoryFromLockedGrantRow(row sourcestore.LockCheckoutGrantForConsumeRow) (Repository, error) {
	return repositoryFromRow(repositoryRow{
		RepoID:              row.RepoID,
		OrgID:               row.OrgID,
		ProjectID:           row.ProjectID,
		CreatedBy:           row.CreatedBy,
		Name:                row.Name,
		Slug:                row.Slug,
		Description:         row.Description,
		DefaultBranch:       row.DefaultBranch,
		Visibility:          row.Visibility,
		State:               row.State,
		Version:             row.Version,
		LastPushedAt:        row.LastPushedAt,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		BackendID:           row.BackendID,
		BackendSourceRepoID: row.BackendSourceRepoID,
		Backend:             row.Backend,
		BackendOwner:        row.BackendOwner,
		BackendRepo:         row.BackendRepo,
		BackendRepoID:       row.BackendRepoID,
		BackendState:        row.BackendState,
		BackendCreatedAt:    row.BackendCreatedAt,
		BackendUpdatedAt:    row.BackendUpdatedAt,
	})
}

func checkoutGrantFromLockedRow(row sourcestore.LockCheckoutGrantForConsumeRow) (CheckoutGrant, error) {
	orgID, err := domainOrgID(row.GrantOrgID)
	if err != nil {
		return CheckoutGrant{}, err
	}
	expiresAt, err := requiredTime(row.GrantExpiresAt)
	if err != nil {
		return CheckoutGrant{}, err
	}
	createdAt, err := requiredTime(row.GrantCreatedAt)
	if err != nil {
		return CheckoutGrant{}, err
	}
	return CheckoutGrant{
		GrantID:    row.GrantID,
		RepoID:     row.GrantRepoID,
		OrgID:      orgID,
		ActorID:    row.GrantActorID,
		Ref:        row.Ref,
		PathPrefix: row.PathPrefix,
		ExpiresAt:  expiresAt,
		CreatedAt:  createdAt,
	}, nil
}

func gitPrincipalFromRow(row sourcestore.LockActiveGitCredentialForUseRow) (GitPrincipal, error) {
	orgID, err := domainOrgID(row.OrgID)
	if err != nil {
		return GitPrincipal{}, err
	}
	return GitPrincipal{
		CredentialID: row.CredentialID,
		OrgID:        orgID,
		ActorID:      row.ActorID,
		Username:     row.Username,
		Scopes:       row.Scopes,
	}, nil
}

func workflowRunByIdempotencyKey(ctx context.Context, q *sourcestore.Queries, orgID int64, idempotencyKey string) (WorkflowRun, error) {
	row, err := q.GetWorkflowRunByIdempotencyKey(ctx, sourcestore.GetWorkflowRunByIdempotencyKeyParams{
		OrgID:          orgID,
		IdempotencyKey: strings.TrimSpace(idempotencyKey),
	})
	if err != nil {
		return WorkflowRun{}, notFoundOrStoreError(err)
	}
	return workflowRunFromRow(row)
}

func workflowRunFromRow(row sourcestore.SourceWorkflowRun) (WorkflowRun, error) {
	orgID, err := domainOrgID(row.OrgID)
	if err != nil {
		return WorkflowRun{}, err
	}
	createdAt, err := requiredTime(row.CreatedAt)
	if err != nil {
		return WorkflowRun{}, err
	}
	updatedAt, err := requiredTime(row.UpdatedAt)
	if err != nil {
		return WorkflowRun{}, err
	}
	run := WorkflowRun{
		WorkflowRunID:     row.WorkflowRunID,
		OrgID:             orgID,
		ProjectID:         row.ProjectID,
		RepoID:            row.RepoID,
		ActorID:           row.ActorID,
		IdempotencyKey:    row.IdempotencyKey,
		Backend:           row.Backend,
		WorkflowPath:      row.WorkflowPath,
		Ref:               row.Ref,
		State:             row.State,
		BackendDispatchID: row.BackendDispatchID,
		FailureReason:     row.FailureReason,
		TraceID:           row.TraceID,
		DispatchedAt:      timePtr(row.DispatchedAt),
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
	}
	if len(row.InputsJson) == 0 {
		run.Inputs = map[string]string{}
	} else if err := json.Unmarshal(row.InputsJson, &run.Inputs); err != nil {
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
		run.ProjectID == repo.ProjectID &&
		run.RepoID == repo.RepoID &&
		run.Backend == repo.Backend.Backend &&
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

func pgOrgID(orgID uint64) (int64, error) {
	if orgID == 0 || orgID > maxPostgresBigint {
		return 0, ErrInvalid
	}
	return int64(orgID), nil
}

func nullableOrgID(orgID uint64) (pgtype.Int8, error) {
	if orgID == 0 {
		return pgtype.Int8{}, nil
	}
	pgOrg, err := pgOrgID(orgID)
	if err != nil {
		return pgtype.Int8{}, err
	}
	return pgtype.Int8{Int64: pgOrg, Valid: true}, nil
}

func domainOrgID(orgID int64) (uint64, error) {
	if orgID <= 0 {
		return 0, fmt.Errorf("%w: invalid org_id %d", ErrStoreUnavailable, orgID)
	}
	return uint64(orgID), nil
}

func timestamptz(value time.Time) pgtype.Timestamptz {
	if value.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func requiredTime(value pgtype.Timestamptz) (time.Time, error) {
	if !value.Valid {
		return time.Time{}, fmt.Errorf("%w: missing timestamp", ErrStoreUnavailable)
	}
	return value.Time.UTC(), nil
}

func timePtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time.UTC()
	return &t
}

func nullableUUID(value uuid.UUID) pgtype.UUID {
	if value == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: value, Valid: true}
}

func notFoundOrStoreError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
}

func unauthorizedOrStoreError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnauthorized
	}
	return fmt.Errorf("%w: %v", ErrStoreUnavailable, err)
}

func storeWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
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
