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
)

var tracer = otel.Tracer("source-code-hosting-service/source")

type Service struct {
	Store         Store
	Forgejo       ForgejoClient
	CheckoutTTL   time.Duration
	ForgejoPrefix string
}

func (s *Service) Ready(ctx context.Context) error {
	if err := s.Store.Ready(ctx); err != nil {
		return err
	}
	if err := s.Forgejo.Ready(ctx); err != nil {
		return err
	}
	return s.Store.UpsertInstallation(ctx, ProviderForgejo, s.Forgejo.BaseURL, s.Forgejo.Owner)
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
	repoName := s.forgejoRepoName(principal.OrgID, input.Name)
	span.SetAttributes(attribute.Int64("forge_metal.org_id", int64(principal.OrgID)), attribute.String("source.forgejo_repo", repoName))
	forgejoRepo, err := s.Forgejo.CreateRepository(ctx, repoName, input.Description, input.DefaultBranch)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Repository{}, err
	}
	repo, err := s.Store.CreateRepository(ctx, principal, input, s.Forgejo.Owner, forgejoRepo)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Repository{}, err
	}
	span.SetAttributes(attribute.String("source.repo_id", repo.RepoID.String()))
	return repo, nil
}

func (s *Service) ListRepositories(ctx context.Context, principal Principal) ([]Repository, error) {
	if err := ValidatePrincipal(principal); err != nil {
		return nil, err
	}
	return s.Store.ListRepositories(ctx, principal.OrgID)
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
	refs, err := s.Forgejo.ListBranches(ctx, repo)
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
	if err := s.Store.InsertStorageEvent(ctx, repo.OrgID, repo.RepoID, repo.Provider, "repository_archive", "source.repository.archive_served", int64(len(data)), map[string]any{
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

func (s *Service) CreateExternalIntegration(ctx context.Context, principal Principal, provider, externalRepo, credentialRef string) (ExternalIntegration, error) {
	ctx, span := tracer.Start(ctx, "source.integration.create")
	defer span.End()
	integration, err := s.Store.CreateExternalIntegration(ctx, principal, provider, externalRepo, credentialRef)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return integration, err
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
	input, err = NormalizeWorkflowDispatch(input, repo.DefaultBranch)
	if err != nil {
		return WorkflowRun{}, err
	}
	span.SetAttributes(
		attribute.String("source.repo_id", repo.RepoID.String()),
		attribute.String("source.workflow_path", input.WorkflowPath),
		attribute.String("source.ref", input.Ref),
		attribute.String("source.provider", repo.Provider),
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
	run, err = s.Store.MarkWorkflowRunDispatched(ctx, run, dispatch.ProviderDispatchID)
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
		WorkflowPath:   input.WorkflowPath,
		Ref:            input.Ref,
		Inputs:         input.Inputs,
		IdempotencyKey: input.IdempotencyKey,
	})
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

func (s *Service) RecordWebhook(ctx context.Context, provider, event, delivery string, valid bool, body []byte) error {
	ctx, span := tracer.Start(ctx, "source.webhook.apply")
	defer span.End()
	span.SetAttributes(attribute.String("source.webhook_provider", provider), attribute.String("source.webhook_event", event), attribute.Bool("source.webhook_valid", valid))
	result := "denied"
	var (
		resolvedOrgID  uint64
		resolvedRepoID uuid.UUID
		details        = map[string]any{"provider": provider, "delivery": delivery}
	)
	if valid {
		result = "unresolved"
		if repo, err := s.ResolveWebhookRepository(ctx, provider, body); err == nil {
			result = "accepted"
			resolvedOrgID = repo.OrgID
			resolvedRepoID = repo.RepoID
			details["repo_id"] = repo.RepoID.String()
		} else if !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrInvalid) {
			result = "error"
			span.RecordError(err)
		}
	}
	return s.Store.RecordWebhookDelivery(ctx, WebhookDelivery{
		Provider:       provider,
		DeliveryID:     strings.TrimSpace(delivery),
		EventType:      strings.TrimSpace(event),
		SignatureValid: valid,
		Result:         result,
		ResolvedOrgID:  resolvedOrgID,
		ResolvedRepoID: resolvedRepoID,
		Details:        details,
	})
}

func (s *Service) ResolveWebhookRepository(ctx context.Context, provider string, body []byte) (Repository, error) {
	switch strings.TrimSpace(provider) {
	case ProviderForgejo:
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
	providerRepoID := ""
	if payload.Repository.ID > 0 {
		providerRepoID = fmt.Sprintf("%d", payload.Repository.ID)
	}
	return s.Store.FindRepositoryByProvider(ctx, ProviderForgejo, owner, repoName, providerRepoID)
}

func (s *Service) forgejoRepoName(orgID uint64, name string) string {
	prefix := strings.Trim(strings.TrimSpace(s.ForgejoPrefix), "-")
	if prefix == "" {
		prefix = "fm"
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
