package source

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	sandboxclient "github.com/verself/sandbox-rental-service/internalclient"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var runnerTracer = otel.Tracer("source-code-hosting-service/runner")

type RunnerRepositoryClient struct {
	Client *sandboxclient.ClientWithResponses
}

func NewRunnerRepositoryClient(baseURL string, httpClient sandboxclient.HttpRequestDoer) (RunnerRepositoryClient, error) {
	client, err := sandboxclient.NewClientWithResponses(strings.TrimRight(baseURL, "/"), sandboxclient.WithHTTPClient(httpClient))
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
		attribute.Int64("verself.org_id", int64(repo.OrgID)),
		attribute.String("source.repo_id", repo.RepoID.String()),
		attribute.Int64("runner.provider_repository_id", providerRepoID),
		attribute.String("runner.provider", BackendForgejo),
	)
	sourceRepositoryID := repo.RepoID.String()
	resp, err := c.Client.InternalRegisterRunnerRepositoryWithResponse(ctx, sandboxclient.InternalRegisterRunnerRepositoryJSONRequestBody{
		Provider:             BackendForgejo,
		OrgId:                fmt.Sprintf("%d", repo.OrgID),
		ProjectId:            repo.ProjectID.String(),
		SourceRepositoryId:   &sourceRepositoryID,
		ProviderOwner:        repo.Backend.BackendOwner,
		ProviderRepo:         repo.Backend.BackendRepo,
		ProviderRepositoryId: strconv.FormatInt(providerRepoID, 10),
		RepositoryFullName:   &repositoryFullName,
	})
	if err != nil {
		return fmt.Errorf("%w: register runner repository: %v", ErrRunner, err)
	}
	if resp.JSON201 == nil {
		status := 0
		body := ""
		if resp.HTTPResponse != nil {
			status = resp.HTTPResponse.StatusCode
			body = strings.TrimSpace(string(resp.Body))
		}
		if status == http.StatusConflict {
			return fmt.Errorf("%w: runner repository conflict: %s", ErrRunner, body)
		}
		return fmt.Errorf("%w: runner repository unexpected status %d: %s", ErrRunner, status, body)
	}
	return nil
}
