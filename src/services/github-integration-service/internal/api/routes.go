package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/verself/github-integration-service/internal/githubintegration"
	"github.com/verself/github-integration-service/internal/routecatalog"
	runtimeiam "github.com/verself/service-runtime/iam"
)

type startSetupInput struct {
	Body struct {
		IdempotencyKey string  `json:"idempotency_key" required:"true"`
		CallbackURL    *string `json:"callback_url,omitempty"`
	}
}

type startSetupOutput struct {
	Body struct {
		Session setupSessionDTO `json:"session"`
	}
}

type getSetupInput struct {
	SetupSessionID string `path:"setup_session_id"`
}

type getSetupOutput = startSetupOutput

type completeSetupInput struct {
	SetupSessionID string `path:"setup_session_id"`
	Body           struct {
		IdempotencyKey         string `json:"idempotency_key" required:"true"`
		State                  string `json:"state" required:"true"`
		ProviderInstallationID int64  `json:"provider_installation_id" required:"true"`
		UserAuthorizationID    string `json:"user_authorization_id" required:"true"`
	}
}

type completeSetupOutput struct {
	Body struct {
		Session      setupSessionDTO          `json:"session"`
		Installation installationBindingDTO   `json:"installation"`
		Repositories []repositoryCandidateDTO `json:"repositories,omitempty"`
	}
}

type startUserAuthorizationInput = startSetupInput

type startUserAuthorizationOutput struct {
	Body struct {
		OAuthSessionID   string `json:"oauth_session_id"`
		AuthorizationURL string `json:"authorization_url"`
		ExpiresAt        string `json:"expires_at"`
	}
}

type completeUserAuthorizationInput struct {
	Body struct {
		IdempotencyKey string `json:"idempotency_key" required:"true"`
		OAuthSessionID string `json:"oauth_session_id" required:"true"`
		State          string `json:"state" required:"true"`
		Code           string `json:"code" required:"true"`
	}
}

type completeUserAuthorizationOutput struct {
	Body struct {
		Authorization userAuthorizationDTO `json:"authorization"`
	}
}

type listInstallationsInput struct {
	PageSize  int    `query:"page_size,omitempty"`
	PageToken string `query:"page_token,omitempty"`
}

type listInstallationsOutput struct {
	Body struct {
		Installations []installationBindingDTO `json:"installations"`
		NextPageToken string                   `json:"nextPageToken,omitempty"`
	}
}

type installationIDInput struct {
	InstallationBindingID string `path:"installation_binding_id"`
}

type getInstallationOutput struct {
	Body struct {
		Installation installationBindingDTO `json:"installation"`
	}
}

type syncInstallationInput struct {
	InstallationBindingID string `path:"installation_binding_id"`
	Body                  struct {
		IdempotencyKey string `json:"idempotency_key" required:"true"`
	}
}

type syncInstallationOutput struct {
	Body struct {
		Installation installationBindingDTO   `json:"installation"`
		Repositories []repositoryCandidateDTO `json:"repositories,omitempty"`
	}
}

type disconnectInstallationInput struct {
	InstallationBindingID string `path:"installation_binding_id"`
	IdempotencyKey        string `header:"Idempotency-Key"`
}

type listRepositoriesInput struct {
	InstallationBindingID string `query:"installation_binding_id,omitempty"`
	PageSize              int    `query:"page_size,omitempty"`
	PageToken             string `query:"page_token,omitempty"`
}

type listRepositoriesOutput struct {
	Body struct {
		Repositories  []repositoryCandidateDTO `json:"repositories"`
		NextPageToken string                   `json:"nextPageToken,omitempty"`
	}
}

type repositoryIDInput struct {
	RepositoryBindingID string `path:"repository_binding_id"`
}

type getRepositoryOutput struct {
	Body struct {
		Repository repositoryBindingDTO `json:"repository"`
	}
}

type enableRepositoryInput struct {
	Body struct {
		IdempotencyKey        string `json:"idempotency_key" required:"true"`
		InstallationBindingID string `json:"installation_binding_id" required:"true"`
		ProviderRepositoryID  int64  `json:"provider_repository_id" required:"true"`
	}
}

type enableRepositoryOutput = getRepositoryOutput

type disableRepositoryInput struct {
	RepositoryBindingID string `path:"repository_binding_id"`
	Body                struct {
		IdempotencyKey string `json:"idempotency_key" required:"true"`
	}
}

type disableRepositoryOutput = getRepositoryOutput

func RegisterRoutes(api huma.API, svc *githubintegration.Service, authorizer runtimeiam.OperationAuthorizer) {
	registerSecured(api, svc, authorizer, routecatalog.Public.MustOperation("start-github-app-setup"), "Start a GitHub App setup session", startGithubSetup(svc))
	registerSecured(api, svc, authorizer, routecatalog.Public.MustOperation("get-github-setup-session"), "Get a GitHub App setup session", getGithubSetup(svc))
	registerSecured(api, svc, authorizer, routecatalog.Public.MustOperation("complete-github-app-setup"), "Complete a GitHub App setup session", completeGithubSetup(svc))
	registerSecured(api, svc, authorizer, routecatalog.Public.MustOperation("start-github-user-authorization"), "Start GitHub user authorization", startGithubUserAuthorization(svc))
	registerSecured(api, svc, authorizer, routecatalog.Public.MustOperation("complete-github-user-authorization"), "Complete GitHub user authorization", completeGithubUserAuthorization(svc))
	registerSecured(api, svc, authorizer, routecatalog.Public.MustOperation("list-github-installations"), "List GitHub installations", listGithubInstallations(svc))
	registerSecured(api, svc, authorizer, routecatalog.Public.MustOperation("get-github-installation"), "Get a GitHub installation", getGithubInstallation(svc))
	registerSecured(api, svc, authorizer, routecatalog.Public.MustOperation("sync-github-installation"), "Sync a GitHub installation", syncGithubInstallation(svc))
	registerSecured(api, svc, authorizer, routecatalog.Public.MustOperation("disconnect-github-installation"), "Disconnect a GitHub installation", disconnectGithubInstallation(svc))
	registerSecured(api, svc, authorizer, routecatalog.Public.MustOperation("list-github-repositories"), "List GitHub repositories", listGithubRepositories(svc))
	registerSecured(api, svc, authorizer, routecatalog.Public.MustOperation("get-github-repository"), "Get a GitHub repository", getGithubRepository(svc))
	registerSecured(api, svc, authorizer, routecatalog.Public.MustOperation("enable-github-repository"), "Enable a GitHub repository", enableGithubRepository(svc))
	registerSecured(api, svc, authorizer, routecatalog.Public.MustOperation("disable-github-repository"), "Disable a GitHub repository", disableGithubRepository(svc))
}

func startGithubSetup(svc *githubintegration.Service) func(context.Context, githubintegration.Principal, *startSetupInput) (*startSetupOutput, error) {
	return func(ctx context.Context, principal githubintegration.Principal, input *startSetupInput) (*startSetupOutput, error) {
		callbackURL := ""
		if input.Body.CallbackURL != nil {
			callbackURL = *input.Body.CallbackURL
		}
		result, err := svc.StartSetupSession(ctx, principal, input.Body.IdempotencyKey, callbackURL)
		if err != nil {
			return nil, err
		}
		out := &startSetupOutput{}
		out.Body.Session = setupSessionDTOFromDomain(result.Session)
		return out, nil
	}
}

func getGithubSetup(svc *githubintegration.Service) func(context.Context, githubintegration.Principal, *getSetupInput) (*getSetupOutput, error) {
	return func(ctx context.Context, principal githubintegration.Principal, input *getSetupInput) (*getSetupOutput, error) {
		id, err := parseUUID(input.SetupSessionID)
		if err != nil {
			return nil, err
		}
		session, err := svc.GetSetupSession(ctx, principal, id)
		if err != nil {
			return nil, err
		}
		out := &getSetupOutput{}
		out.Body.Session = setupSessionDTOFromDomain(session)
		return out, nil
	}
}

func completeGithubSetup(svc *githubintegration.Service) func(context.Context, githubintegration.Principal, *completeSetupInput) (*completeSetupOutput, error) {
	return func(ctx context.Context, principal githubintegration.Principal, input *completeSetupInput) (*completeSetupOutput, error) {
		setupID, err := parseUUID(input.SetupSessionID)
		if err != nil {
			return nil, err
		}
		authID, err := parseUUID(input.Body.UserAuthorizationID)
		if err != nil {
			return nil, err
		}
		session, installation, repositories, err := svc.CompleteSetupSession(ctx, principal, setupID, input.Body.State, input.Body.ProviderInstallationID, authID)
		if err != nil {
			return nil, err
		}
		out := &completeSetupOutput{}
		out.Body.Session = setupSessionDTOFromDomain(session)
		out.Body.Installation = installationDTOFromDomain(installation)
		out.Body.Repositories = repositoryCandidateDTOs(repositories)
		return out, nil
	}
}

func startGithubUserAuthorization(svc *githubintegration.Service) func(context.Context, githubintegration.Principal, *startUserAuthorizationInput) (*startUserAuthorizationOutput, error) {
	return func(ctx context.Context, principal githubintegration.Principal, input *startUserAuthorizationInput) (*startUserAuthorizationOutput, error) {
		callbackURL := ""
		if input.Body.CallbackURL != nil {
			callbackURL = *input.Body.CallbackURL
		}
		result, err := svc.StartUserAuthorization(ctx, principal, callbackURL)
		if err != nil {
			return nil, err
		}
		out := &startUserAuthorizationOutput{}
		out.Body.OAuthSessionID = result.OAuthSessionID.String()
		out.Body.AuthorizationURL = result.AuthorizationURL
		out.Body.ExpiresAt = formatTime(result.ExpiresAt)
		return out, nil
	}
}

func completeGithubUserAuthorization(svc *githubintegration.Service) func(context.Context, githubintegration.Principal, *completeUserAuthorizationInput) (*completeUserAuthorizationOutput, error) {
	return func(ctx context.Context, principal githubintegration.Principal, input *completeUserAuthorizationInput) (*completeUserAuthorizationOutput, error) {
		id, err := parseUUID(input.Body.OAuthSessionID)
		if err != nil {
			return nil, err
		}
		authz, err := svc.CompleteUserAuthorization(ctx, principal, id, input.Body.State, input.Body.Code)
		if err != nil {
			return nil, err
		}
		out := &completeUserAuthorizationOutput{}
		out.Body.Authorization = userAuthorizationDTOFromDomain(authz)
		return out, nil
	}
}

func listGithubInstallations(svc *githubintegration.Service) func(context.Context, githubintegration.Principal, *listInstallationsInput) (*listInstallationsOutput, error) {
	return func(ctx context.Context, principal githubintegration.Principal, input *listInstallationsInput) (*listInstallationsOutput, error) {
		page, err := svc.ListInstallations(ctx, principal, input.PageSize, input.PageToken)
		if err != nil {
			return nil, err
		}
		out := &listInstallationsOutput{}
		out.Body.Installations = installationDTOs(page.Installations)
		out.Body.NextPageToken = page.NextPageToken
		return out, nil
	}
}

func getGithubInstallation(svc *githubintegration.Service) func(context.Context, githubintegration.Principal, *installationIDInput) (*getInstallationOutput, error) {
	return func(ctx context.Context, principal githubintegration.Principal, input *installationIDInput) (*getInstallationOutput, error) {
		id, err := parseUUID(input.InstallationBindingID)
		if err != nil {
			return nil, err
		}
		row, err := svc.GetInstallation(ctx, principal, id)
		if err != nil {
			return nil, err
		}
		out := &getInstallationOutput{}
		out.Body.Installation = installationDTOFromDomain(row)
		return out, nil
	}
}

func syncGithubInstallation(svc *githubintegration.Service) func(context.Context, githubintegration.Principal, *syncInstallationInput) (*syncInstallationOutput, error) {
	return func(ctx context.Context, principal githubintegration.Principal, input *syncInstallationInput) (*syncInstallationOutput, error) {
		id, err := parseUUID(input.InstallationBindingID)
		if err != nil {
			return nil, err
		}
		repositories, err := svc.SyncInstallationRepositories(ctx, principal, id)
		if err != nil {
			return nil, err
		}
		installation, err := svc.GetInstallation(ctx, principal, id)
		if err != nil {
			return nil, err
		}
		out := &syncInstallationOutput{}
		out.Body.Installation = installationDTOFromDomain(installation)
		out.Body.Repositories = repositoryCandidateDTOs(repositories)
		return out, nil
	}
}

func disconnectGithubInstallation(svc *githubintegration.Service) func(context.Context, githubintegration.Principal, *disconnectInstallationInput) (*getInstallationOutput, error) {
	return func(ctx context.Context, principal githubintegration.Principal, input *disconnectInstallationInput) (*getInstallationOutput, error) {
		id, err := parseUUID(input.InstallationBindingID)
		if err != nil {
			return nil, err
		}
		row, err := svc.DisconnectInstallation(ctx, principal, id)
		if err != nil {
			return nil, err
		}
		out := &getInstallationOutput{}
		out.Body.Installation = installationDTOFromDomain(row)
		return out, nil
	}
}

func listGithubRepositories(svc *githubintegration.Service) func(context.Context, githubintegration.Principal, *listRepositoriesInput) (*listRepositoriesOutput, error) {
	return func(ctx context.Context, principal githubintegration.Principal, input *listRepositoriesInput) (*listRepositoriesOutput, error) {
		var installationID uuid.UUID
		if input.InstallationBindingID != "" {
			id, err := parseUUID(input.InstallationBindingID)
			if err != nil {
				return nil, err
			}
			installationID = id
		}
		page, err := svc.ListRepositories(ctx, principal, installationID, input.PageSize, input.PageToken)
		if err != nil {
			return nil, err
		}
		out := &listRepositoriesOutput{}
		out.Body.Repositories = repositoryCandidateDTOs(page.Repositories)
		out.Body.NextPageToken = page.NextPageToken
		return out, nil
	}
}

func getGithubRepository(svc *githubintegration.Service) func(context.Context, githubintegration.Principal, *repositoryIDInput) (*getRepositoryOutput, error) {
	return func(ctx context.Context, principal githubintegration.Principal, input *repositoryIDInput) (*getRepositoryOutput, error) {
		id, err := parseUUID(input.RepositoryBindingID)
		if err != nil {
			return nil, err
		}
		row, err := svc.GetRepository(ctx, principal, id)
		if err != nil {
			return nil, err
		}
		out := &getRepositoryOutput{}
		out.Body.Repository = repositoryDTOFromDomain(row)
		return out, nil
	}
}

func enableGithubRepository(svc *githubintegration.Service) func(context.Context, githubintegration.Principal, *enableRepositoryInput) (*enableRepositoryOutput, error) {
	return func(ctx context.Context, principal githubintegration.Principal, input *enableRepositoryInput) (*enableRepositoryOutput, error) {
		installationID, err := parseUUID(input.Body.InstallationBindingID)
		if err != nil {
			return nil, err
		}
		row, err := svc.EnableRepository(ctx, principal, installationID, input.Body.ProviderRepositoryID)
		if err != nil {
			return nil, err
		}
		out := &enableRepositoryOutput{}
		out.Body.Repository = repositoryDTOFromDomain(row)
		return out, nil
	}
}

func disableGithubRepository(svc *githubintegration.Service) func(context.Context, githubintegration.Principal, *disableRepositoryInput) (*disableRepositoryOutput, error) {
	return func(ctx context.Context, principal githubintegration.Principal, input *disableRepositoryInput) (*disableRepositoryOutput, error) {
		id, err := parseUUID(input.RepositoryBindingID)
		if err != nil {
			return nil, err
		}
		row, err := svc.DisableRepository(ctx, principal, id)
		if err != nil {
			return nil, err
		}
		out := &disableRepositoryOutput{}
		out.Body.Repository = repositoryDTOFromDomain(row)
		return out, nil
	}
}

func parseUUID(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, huma.Error400BadRequest("invalid UUID path parameter", err)
	}
	return id, nil
}
