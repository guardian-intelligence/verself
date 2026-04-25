package source

import (
	"context"
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
	return s.Store.UpsertInstallation(ctx, s.Forgejo.BaseURL, s.Forgejo.Owner)
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

func (s *Service) RecordWebhook(ctx context.Context, provider, event, delivery string, valid bool) error {
	ctx, span := tracer.Start(ctx, "source.webhook.apply")
	defer span.End()
	span.SetAttributes(attribute.String("source.webhook_provider", provider), attribute.String("source.webhook_event", event), attribute.Bool("source.webhook_valid", valid))
	return s.Store.RecordWebhook(ctx, provider, event, delivery, valid)
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
