package source

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	sandboxclient "github.com/verself/sandbox-rental-service/client"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var runnerTracer = otel.Tracer("source-code-hosting-service/runner")

type RunnerRepositoryClient struct {
	Client *sandboxclient.Client
}

func NewRunnerRepositoryClient(baseURL string, httpClient sandboxclient.HTTPRequestDoer) (RunnerRepositoryClient, error) {
	client, err := sandboxclient.NewClient(strings.TrimRight(baseURL, "/"), sandboxclient.WithHTTPClient(httpClient))
	if err != nil {
		return RunnerRepositoryClient{}, err
	}
	return RunnerRepositoryClient{Client: client}, nil
}

func (c RunnerRepositoryClient) RegisterRunnerRepository(ctx context.Context, repo Repository) (err error) {
	ctx, span := runnerTracer.Start(ctx, "source.runner.repository.register")
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	if c.Client == nil {
		return ErrRunner
	}
	if repo.Backend.Backend != BackendForgejo {
		return fmt.Errorf("%w: unsupported repository backend %q", ErrRunner, repo.Backend.Backend)
	}
	providerRepoID, err := strconv.ParseInt(strings.TrimSpace(repo.Backend.BackendRepoID), 10, 64)
	if err != nil || providerRepoID <= 0 {
		return fmt.Errorf("%w: invalid forgejo repository id %q", ErrRunner, repo.Backend.BackendRepoID)
	}
	repositoryFullName := strings.TrimSpace(repo.Backend.BackendOwner + "/" + repo.Backend.BackendRepo)
	span.SetAttributes(
		attribute.Int64("verself.org_id", int64FromUint64(repo.OrgID, "org id")),
		attribute.String("source.repo_id", repo.RepoID.String()),
		attribute.Int64("runner.provider_repository_id", providerRepoID),
		attribute.String("runner.provider", BackendForgejo),
	)
	sourceRepositoryID := repo.RepoID.String()
	fullName := sandboxclient.RepositoryFullName(repositoryFullName)
	sourceRepoID := sandboxclient.SourceRepositoryId(sourceRepositoryID)
	resp, err := c.Client.InternalRegisterRunnerRepository(ctx, sandboxclient.InternalRegisterRunnerRepositoryRequest{
		Body: sandboxclient.InternalRegisterRunnerRepositoryInputBody{
			Provider:             sandboxclient.Provider(BackendForgejo),
			OrgID:                sandboxclient.OrgId(fmt.Sprintf("%d", repo.OrgID)),
			ProjectID:            sandboxclient.ProjectId(repo.ProjectID.String()),
			SourceRepositoryID:   &sourceRepoID,
			ProviderOwner:        sandboxclient.ProviderOwner(repo.Backend.BackendOwner),
			ProviderRepo:         sandboxclient.ProviderRepo(repo.Backend.BackendRepo),
			ProviderRepositoryID: sandboxclient.ProviderRepositoryId(strconv.FormatInt(providerRepoID, 10)),
			RepositoryFullName:   &fullName,
		},
	})
	if err != nil {
		return fmt.Errorf("%w: register runner repository: %v", ErrRunner, err)
	}
	if resp.Result != nil {
		return nil
	}
	body := strings.TrimSpace(string(resp.Body))
	if resp.StatusCode == http.StatusConflict {
		return fmt.Errorf("%w: runner repository conflict: %s", ErrRunner, body)
	}
	return fmt.Errorf("%w: runner repository unexpected status %d: %s", ErrRunner, resp.StatusCode, body)
}
