package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("source-code-hosting-service/source")

type Service struct {
	Store         Store
	Forgejo       ForgejoClient
	Credentials   GitCredentialIssuer
	Runner        RunnerRepositoryRegistrar
	Projects      ProjectResolver
	CheckoutTTL   time.Duration
	ForgejoPrefix string
}

func (s *Service) Ready(ctx context.Context) error {
	if err := s.Store.Ready(ctx); err != nil {
		return err
	}
	return s.Forgejo.Ready(ctx)
}

func (s *Service) CreateRepository(ctx context.Context, principal Principal, input CreateRepositoryRequest) (Repository, error) {
	ctx, span := tracer.Start(ctx, "source.repo.create")
	defer span.End()
	if err := ValidatePrincipal(principal); err != nil {
		return Repository{}, err
	}
	input, err := NormalizeCreate(input)
	if err != nil {
		return Repository{}, err
	}
	if err := s.resolveProject(ctx, principal.OrgID, input.ProjectID); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Repository{}, err
	}
	repoName := s.forgejoRepoName(principal.OrgID, input.Name)
	span.SetAttributes(
		attribute.Int64("verself.org_id", int64(principal.OrgID)),
		attribute.String("verself.project_id", input.ProjectID.String()),
		attribute.String("source.forgejo_repo", repoName),
	)
	forgejoRepo, err := s.createOrGetForgejoRepository(ctx, repoName, input.Description, input.DefaultBranch)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Repository{}, err
	}
	repo, err := s.Store.CreateRepository(ctx, principal, input, s.Forgejo.Owner, forgejoRepo)
	if err != nil {
		if errors.Is(err, ErrConflict) {
			existing, loadErr := s.Store.GetRepositoryBySlug(ctx, principal.OrgID, NormalizeSlug(input.Name))
			if loadErr == nil {
				if existing.ProjectID != input.ProjectID {
					return Repository{}, err
				}
				if registerErr := s.registerRunnerRepository(ctx, span, existing); registerErr != nil {
					return Repository{}, registerErr
				}
				span.SetAttributes(attribute.String("source.repo_id", existing.RepoID.String()))
				return existing, nil
			}
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Repository{}, err
	}
	if err := s.registerRunnerRepository(ctx, span, repo); err != nil {
		return Repository{}, err
	}
	span.SetAttributes(attribute.String("source.repo_id", repo.RepoID.String()))
	return repo, nil
}

func (s *Service) CreateGitCredential(ctx context.Context, principal Principal, input CreateGitCredentialRequest) (GitCredential, error) {
	ctx, span := tracer.Start(ctx, "source.git_credential.create")
	defer span.End()
	if s.Credentials == nil {
		return GitCredential{}, ErrStoreUnavailable
	}
	credential, err := s.Credentials.CreateSourceGitCredential(ctx, principal, input)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return GitCredential{}, err
	}
	credential, err = s.Store.CreateGitCredential(ctx, principal, credential)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return GitCredential{}, err
	}
	span.SetAttributes(attribute.String("source.git_credential_id", credential.CredentialID.String()))
	return credential, nil
}

func (s *Service) AuthenticateGitCredential(ctx context.Context, username string, token string, orgPath string, requiredScopes []string) (GitPrincipal, error) {
	ctx, span := tracer.Start(ctx, "source.git.auth")
	defer span.End()
	username = strings.TrimSpace(username)
	token = strings.TrimSpace(token)
	if username != GitCredentialUsername || token == "" {
		return GitPrincipal{}, ErrUnauthorized
	}
	orgID, err := OrgIDFromPath(orgPath)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return GitPrincipal{}, ErrUnauthorized
	}
	if s.Credentials == nil {
		return GitPrincipal{}, ErrStoreUnavailable
	}
	verified, active, err := s.Credentials.VerifySourceGitCredential(ctx, orgID, "", token, requiredScopes)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return GitPrincipal{}, err
	}
	if !active {
		return GitPrincipal{}, ErrUnauthorized
	}
	principal, err := s.Store.MarkGitCredentialUsed(ctx, verified.CredentialID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return GitPrincipal{}, err
	}
	span.SetAttributes(
		attribute.String("source.git_credential_id", principal.CredentialID.String()),
		attribute.Int64("verself.org_id", int64(principal.OrgID)),
	)
	return principal, nil
}

func (s *Service) EnsureGitRepository(ctx context.Context, principal GitPrincipal, slug string) (Repository, bool, error) {
	ctx, span := tracer.Start(ctx, "source.git.repository.ensure")
	defer span.End()
	slug = NormalizeSlug(slug)
	if slug == "" || principal.OrgID == 0 {
		return Repository{}, false, ErrInvalid
	}
	repo, err := s.Store.GetRepositoryBySlug(ctx, principal.OrgID, slug)
	if err == nil {
		if err := s.registerRunnerRepository(ctx, span, repo); err != nil {
			return Repository{}, false, err
		}
		span.SetAttributes(attribute.String("source.repo_id", repo.RepoID.String()), attribute.Bool("source.repo_created", false))
		return repo, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Repository{}, false, err
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return Repository{}, false, err
}

func (s *Service) GetGitRepository(ctx context.Context, principal GitPrincipal, slug string) (Repository, error) {
	ctx, span := tracer.Start(ctx, "source.git.repository.get")
	defer span.End()
	slug = NormalizeSlug(slug)
	if slug == "" || principal.OrgID == 0 {
		return Repository{}, ErrInvalid
	}
	repo, err := s.Store.GetRepositoryBySlug(ctx, principal.OrgID, slug)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Repository{}, err
	}
	span.SetAttributes(attribute.String("source.repo_id", repo.RepoID.String()), attribute.String("source.slug", slug))
	return repo, nil
}

func (s *Service) AfterGitReceive(ctx context.Context, principal GitPrincipal, repo Repository) error {
	ctx, span := tracer.Start(ctx, "source.git.receive.apply")
	defer span.End()
	if err := s.registerRunnerRepository(ctx, span, repo); err != nil {
		return err
	}
	refs, err := s.listRefsAfterReceive(ctx, repo)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if err := s.Store.ReplaceRefs(ctx, principal.ActorID, repo, refs); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetAttributes(
		attribute.String("source.repo_id", repo.RepoID.String()),
		attribute.Int("source.ref_count", len(refs)),
	)
	return nil
}

func (s *Service) registerRunnerRepository(ctx context.Context, span trace.Span, repo Repository) error {
	if s.Runner == nil {
		return nil
	}
	if err := s.Runner.RegisterRunnerRepository(ctx, repo); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

func (s *Service) listRefsAfterReceive(ctx context.Context, repo Repository) ([]Ref, error) {
	backoff := []time.Duration{0, 25 * time.Millisecond, 75 * time.Millisecond, 150 * time.Millisecond, 300 * time.Millisecond}
	var refs []Ref
	var err error
	for attempt, delay := range backoff {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		refs, err = s.Forgejo.ListBranches(ctx, repo)
		if err != nil {
			return nil, err
		}
		if len(refs) > 0 || attempt == len(backoff)-1 {
			return refs, nil
		}
	}
	return refs, err
}

func (s *Service) ListRepositories(ctx context.Context, principal Principal, projectID uuid.UUID) ([]Repository, error) {
	if err := ValidatePrincipal(principal); err != nil {
		return nil, err
	}
	return s.Store.ListRepositories(ctx, principal.OrgID, projectID)
}

func (s *Service) GetRepository(ctx context.Context, principal Principal, repoID uuid.UUID) (Repository, error) {
	if err := ValidatePrincipal(principal); err != nil {
		return Repository{}, err
	}
	return s.Store.GetRepository(ctx, principal.OrgID, repoID)
}

func (s *Service) ListRefs(ctx context.Context, principal Principal, repoID uuid.UUID) ([]Ref, error) {
	ctx, span := tracer.Start(ctx, "source.refs.list")
	defer span.End()
	repo, err := s.GetRepository(ctx, principal, repoID)
	if err != nil {
		return nil, err
	}
	refs, err := s.Store.ListRefs(ctx, principal.OrgID, repo.RepoID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	return refs, nil
}

func (s *Service) Tree(ctx context.Context, principal Principal, repoID uuid.UUID, ref, path string) ([]TreeEntry, error) {
	ctx, span := tracer.Start(ctx, "source.tree.get")
	defer span.End()
	repo, err := s.GetRepository(ctx, principal, repoID)
	if err != nil {
		return nil, err
	}
	entries, blob, err := s.Forgejo.Contents(ctx, repo, firstNonEmpty(ref, repo.DefaultBranch), path)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	if blob != nil {
		return nil, ErrInvalid
	}
	return entries, nil
}

func (s *Service) Blob(ctx context.Context, principal Principal, repoID uuid.UUID, ref, path string) (Blob, error) {
	ctx, span := tracer.Start(ctx, "source.blob.get")
	defer span.End()
	repo, err := s.GetRepository(ctx, principal, repoID)
	if err != nil {
		return Blob{}, err
	}
	_, blob, err := s.Forgejo.Contents(ctx, repo, firstNonEmpty(ref, repo.DefaultBranch), path)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Blob{}, err
	}
	if blob == nil {
		return Blob{}, ErrInvalid
	}
	return *blob, nil
}

func (s *Service) CreateCheckoutGrant(ctx context.Context, principal Principal, repoID uuid.UUID, ref, pathPrefix string) (CheckoutGrant, error) {
	ctx, span := tracer.Start(ctx, "source.checkout_grant.create")
	defer span.End()
	// Path-scoped archive extraction is not implemented yet, so reject prefixes instead of issuing an overbroad grant.
	if strings.Trim(strings.TrimSpace(pathPrefix), "/") != "" {
		return CheckoutGrant{}, ErrInvalid
	}
	repo, err := s.GetRepository(ctx, principal, repoID)
	if err != nil {
		return CheckoutGrant{}, err
	}
	grant, err := s.Store.CreateCheckoutGrant(ctx, principal, repo, ref, pathPrefix, firstDuration(s.CheckoutTTL, 5*time.Minute))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return CheckoutGrant{}, err
	}
	return grant, nil
}

func (s *Service) ConsumeArchive(ctx context.Context, grantID uuid.UUID, token string) ([]byte, string, CheckoutGrant, Repository, error) {
	ctx, span := tracer.Start(ctx, "source.archive.stream")
	defer span.End()
	grant, repo, err := s.Store.ConsumeCheckoutGrant(ctx, grantID, token)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, "", CheckoutGrant{}, Repository{}, err
	}
	data, contentType, err := s.Forgejo.Archive(ctx, repo, grant.Ref)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, "", CheckoutGrant{}, Repository{}, err
	}
	if err := s.Store.InsertStorageEvent(ctx, repo.OrgID, repo.RepoID, repo.Backend.Backend, "repository_archive", "source.repository.archive_served", int64(len(data)), map[string]any{
		"grant_id": grant.GrantID.String(),
		"ref":      grant.Ref,
	}); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, "", CheckoutGrant{}, Repository{}, err
	}
	span.SetAttributes(attribute.String("source.repo_id", repo.RepoID.String()), attribute.String("source.checkout_grant_id", grant.GrantID.String()))
	return data, contentType, grant, repo, nil
}

func (s *Service) DispatchWorkflow(ctx context.Context, principal Principal, input WorkflowDispatchRequest) (WorkflowRun, error) {
	ctx, span := tracer.Start(ctx, "source.workflow.dispatch")
	defer span.End()
	if err := ValidatePrincipal(principal); err != nil {
		return WorkflowRun{}, err
	}
	repo, err := s.GetRepository(ctx, principal, input.RepoID)
	if err != nil {
		return WorkflowRun{}, err
	}
	if input.ProjectID != repo.ProjectID {
		return WorkflowRun{}, ErrInvalid
	}
	if err := s.resolveProject(ctx, principal.OrgID, repo.ProjectID); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return WorkflowRun{}, err
	}
	input, err = NormalizeWorkflowDispatch(input, repo.DefaultBranch)
	if err != nil {
		return WorkflowRun{}, err
	}
	span.SetAttributes(
		attribute.String("source.repo_id", repo.RepoID.String()),
		attribute.String("verself.project_id", repo.ProjectID.String()),
		attribute.String("source.workflow_path", input.WorkflowPath),
		attribute.String("source.ref", input.Ref),
		attribute.String("source.backend", repo.Backend.Backend),
	)
	run, inserted, err := s.Store.CreateWorkflowRun(ctx, principal, repo, input)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return WorkflowRun{}, err
	}
	if !inserted || run.State == WorkflowRunStateDispatched {
		span.SetAttributes(attribute.String("source.workflow_run_id", run.WorkflowRunID.String()), attribute.Bool("source.workflow_idempotent_replay", true))
		return run, nil
	}
	dispatch, err := s.Forgejo.DispatchWorkflow(ctx, repo, run.WorkflowRunID, input.WorkflowPath, input.Ref, input.Inputs)
	if err != nil {
		if markErr := s.Store.MarkWorkflowRunFailed(ctx, run, err.Error()); markErr != nil {
			span.RecordError(markErr)
			span.SetStatus(codes.Error, markErr.Error())
			return WorkflowRun{}, errors.Join(err, markErr)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return WorkflowRun{}, err
	}
	run, err = s.Store.MarkWorkflowRunDispatched(ctx, run, dispatch.BackendDispatchID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return WorkflowRun{}, err
	}
	span.SetAttributes(attribute.String("source.workflow_run_id", run.WorkflowRunID.String()), attribute.String("source.workflow_state", run.State))
	return run, nil
}

func (s *Service) DispatchWorkflowInternal(ctx context.Context, input InternalWorkflowDispatchRequest) (WorkflowRun, error) {
	principal := Principal{Subject: strings.TrimSpace(input.ActorID), OrgID: input.OrgID}
	return s.DispatchWorkflow(ctx, principal, WorkflowDispatchRequest{
		RepoID:         input.RepoID,
		ProjectID:      input.ProjectID,
		WorkflowPath:   input.WorkflowPath,
		Ref:            input.Ref,
		Inputs:         input.Inputs,
		IdempotencyKey: input.IdempotencyKey,
	})
}

func (s *Service) resolveProject(ctx context.Context, orgID uint64, projectID uuid.UUID) error {
	if projectID == uuid.Nil {
		return ErrInvalid
	}
	if s.Projects == nil {
		return ErrStoreUnavailable
	}
	return s.Projects.ResolveSourceProject(ctx, orgID, projectID)
}

func (s *Service) ListWorkflowRuns(ctx context.Context, principal Principal, repoID uuid.UUID) ([]WorkflowRun, error) {
	ctx, span := tracer.Start(ctx, "source.workflow_runs.list")
	defer span.End()
	if err := ValidatePrincipal(principal); err != nil {
		return nil, err
	}
	if _, err := s.GetRepository(ctx, principal, repoID); err != nil {
		return nil, err
	}
	return s.Store.ListWorkflowRuns(ctx, principal.OrgID, repoID)
}

func (s *Service) GetWorkflowRun(ctx context.Context, principal Principal, runID uuid.UUID) (WorkflowRun, error) {
	ctx, span := tracer.Start(ctx, "source.workflow_run.read")
	defer span.End()
	if err := ValidatePrincipal(principal); err != nil {
		return WorkflowRun{}, err
	}
	run, err := s.Store.GetWorkflowRun(ctx, principal.OrgID, runID)
	if err != nil {
		return WorkflowRun{}, err
	}
	span.SetAttributes(attribute.String("source.workflow_run_id", run.WorkflowRunID.String()), attribute.String("source.repo_id", run.RepoID.String()))
	return run, nil
}

func (s *Service) RecordWebhook(ctx context.Context, backend, event, delivery string, valid bool, body []byte) error {
	ctx, span := tracer.Start(ctx, "source.webhook.apply")
	defer span.End()
	span.SetAttributes(attribute.String("source.webhook_backend", backend), attribute.String("source.webhook_event", event), attribute.Bool("source.webhook_valid", valid))
	result := "denied"
	var (
		resolvedOrgID     uint64
		resolvedProjectID uuid.UUID
		resolvedRepoID    uuid.UUID
		details           = map[string]any{"backend": backend, "delivery": delivery}
	)
	if valid {
		result = "unresolved"
		if repo, err := s.ResolveWebhookRepository(ctx, backend, body); err == nil {
			result = "accepted"
			resolvedOrgID = repo.OrgID
			resolvedProjectID = repo.ProjectID
			resolvedRepoID = repo.RepoID
			details["project_id"] = repo.ProjectID.String()
			details["repo_id"] = repo.RepoID.String()
		} else if !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrInvalid) {
			result = "error"
			span.RecordError(err)
		}
	}
	return s.Store.RecordWebhookDelivery(ctx, WebhookDelivery{
		Backend:           strings.TrimSpace(backend),
		DeliveryID:        strings.TrimSpace(delivery),
		EventType:         strings.TrimSpace(event),
		SignatureValid:    valid,
		Result:            result,
		ResolvedOrgID:     resolvedOrgID,
		ResolvedProjectID: resolvedProjectID,
		ResolvedRepoID:    resolvedRepoID,
		Details:           details,
	})
}

func (s *Service) ResolveWebhookRepository(ctx context.Context, backend string, body []byte) (Repository, error) {
	switch strings.TrimSpace(backend) {
	case BackendForgejo:
		return s.resolveForgejoWebhookRepository(ctx, body)
	default:
		return Repository{}, ErrInvalid
	}
}

func (s *Service) resolveForgejoWebhookRepository(ctx context.Context, body []byte) (Repository, error) {
	var payload struct {
		Repository struct {
			ID       int64  `json:"id"`
			Name     string `json:"name"`
			FullName string `json:"full_name"`
			Owner    struct {
				UserName string `json:"username"`
				Login    string `json:"login"`
				Name     string `json:"name"`
			} `json:"owner"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Repository{}, ErrInvalid
	}
	owner := firstNonEmpty(payload.Repository.Owner.UserName, payload.Repository.Owner.Login, payload.Repository.Owner.Name)
	repoName := payload.Repository.Name
	if payload.Repository.FullName != "" {
		parts := strings.SplitN(payload.Repository.FullName, "/", 2)
		if len(parts) == 2 {
			owner = firstNonEmpty(owner, parts[0])
			repoName = firstNonEmpty(repoName, parts[1])
		}
	}
	backendRepoID := ""
	if payload.Repository.ID > 0 {
		backendRepoID = fmt.Sprintf("%d", payload.Repository.ID)
	}
	return s.Store.FindRepositoryByBackend(ctx, BackendForgejo, owner, repoName, backendRepoID)
}

func (s *Service) createOrGetForgejoRepository(ctx context.Context, repoName, description, defaultBranch string) (forgejoRepo, error) {
	repo, err := s.Forgejo.CreateRepository(ctx, repoName, description, defaultBranch)
	if err == nil {
		return repo, nil
	}
	existing, getErr := s.Forgejo.GetRepository(ctx, s.Forgejo.Owner, repoName)
	if getErr == nil {
		return existing, nil
	}
	return forgejoRepo{}, err
}

func (s *Service) forgejoRepoName(orgID uint64, name string) string {
	prefix := strings.Trim(strings.TrimSpace(s.ForgejoPrefix), "-")
	if prefix == "" {
		prefix = "verself"
	}
	return fmt.Sprintf("%s-%d-%s", prefix, orgID, NormalizeSlug(name))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstDuration(values ...time.Duration) time.Duration {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
