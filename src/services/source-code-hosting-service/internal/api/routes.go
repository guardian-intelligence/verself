package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	runtimeiam "github.com/verself/service-runtime/iam"
	"github.com/verself/source-code-hosting-service/internal/contractapi"
	"github.com/verself/source-code-hosting-service/internal/internalcontractapi"
	"github.com/verself/source-code-hosting-service/internal/source"
)

type Config struct {
	Service        *source.Service
	PublicBaseURL  string
	WebhookSecret  string
	Authorizer     runtimeiam.OperationAuthorizer
	InstallationID string
}

func RegisterRoutes(api huma.API, cfg Config) {
	svc := cfg.Service
	registerPublicSourceRoute(api, cfg.Authorizer, contractapi.CreateSourceRepository.Descriptor, "Create a source repository", createRepository(svc, cfg))
	registerPublicSourceRoute(api, cfg.Authorizer, contractapi.CreateSourceGitCredential.Descriptor, "Create a scoped HTTPS Git credential", createGitCredential(svc, cfg))
	registerPublicSourceRoute(api, cfg.Authorizer, contractapi.ListSourceRepositories.Descriptor, "List source repositories", listRepositories(svc, cfg))
	registerPublicSourceRoute(api, cfg.Authorizer, contractapi.GetSourceRepository.Descriptor, "Get a source repository", getRepository(svc, cfg))
	registerPublicSourceRoute(api, cfg.Authorizer, contractapi.ListSourceRefs.Descriptor, "List repository refs", listRefs(svc))
	registerPublicSourceRoute(api, cfg.Authorizer, contractapi.GetSourceTree.Descriptor, "Get a repository tree path", getTree(svc))
	registerPublicSourceRoute(api, cfg.Authorizer, contractapi.GetSourceBlob.Descriptor, "Get a repository blob", getBlob(svc))
	registerPublicSourceRoute(api, cfg.Authorizer, contractapi.CreateSourceCheckoutGrant.Descriptor, "Create a short-lived checkout grant", createCheckoutGrant(svc, cfg))
	registerPublicSourceRoute(api, cfg.Authorizer, contractapi.CreateSourceWorkflowRun.Descriptor, "Dispatch a repository workflow", createWorkflowRun(svc, cfg))
	registerPublicSourceRoute(api, cfg.Authorizer, contractapi.ListSourceWorkflowRuns.Descriptor, "List repository workflow runs", listWorkflowRuns(svc, cfg))
	registerPublicSourceRoute(api, cfg.Authorizer, contractapi.GetSourceWorkflowRun.Descriptor, "Get a workflow run", getWorkflowRun(svc, cfg))
}

func RegisterInternalRoutes(api huma.API, cfg Config) {
	svc := cfg.Service
	registerInternalSourceRoute(api, cfg.Authorizer, internalcontractapi.InternalCreateSourceWorkflowRun.Descriptor, "Dispatch a source workflow on behalf of a service-owned schedule", internalCreateWorkflowRun(svc, cfg))
	registerInternalSourceRoute(api, cfg.Authorizer, internalcontractapi.DownloadSourceCheckoutArchive.Descriptor, "Download an archive using a short-lived checkout grant", downloadArchive(svc))
}

func createRepository(svc *source.Service, cfg Config) func(context.Context, source.Principal, *contractapi.CreateSourceRepositoryInput) (*contractapi.RepositoryOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *contractapi.CreateSourceRepositoryInput) (*contractapi.RepositoryOutput, error) {
		projectID, err := parseUUID(ctx, string(input.Body.ProjectID), "invalid-project-id", "project_id must be a UUID")
		if err != nil {
			return nil, err
		}
		repo, err := svc.CreateRepository(ctx, principal, source.CreateRepositoryRequest{
			ProjectID:     projectID,
			Description:   stringFromPointer(input.Body.Description),
			DefaultBranch: stringFromPointer(input.Body.DefaultBranch),
		})
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		view, err := repositoryResponse(ctx, svc, cfg, repo)
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &contractapi.RepositoryOutput{Body: view}, nil
	}
}

func createGitCredential(svc *source.Service, cfg Config) func(context.Context, source.Principal, *contractapi.CreateSourceGitCredentialInput) (*contractapi.GitCredentialOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *contractapi.CreateSourceGitCredentialInput) (*contractapi.GitCredentialOutput, error) {
		credential, err := svc.CreateGitCredential(ctx, principal, source.CreateGitCredentialRequest{
			Label:            stringFromPointer(input.Body.Label),
			ExpiresInSeconds: int64FromPointer(input.Body.ExpiresInSeconds),
			Scopes:           gitScopesFromContract(input.Body.Scopes),
		})
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &contractapi.GitCredentialOutput{Body: gitCredentialView(credential, cfg.InstallationID)}, nil
	}
}

func listRepositories(svc *source.Service, cfg Config) func(context.Context, source.Principal, *contractapi.ListSourceRepositoriesInput) (*contractapi.ListSourceRepositoriesOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *contractapi.ListSourceRepositoriesInput) (*contractapi.ListSourceRepositoriesOutput, error) {
		var projectID uuid.UUID
		if input.ProjectID != "" {
			parsed, err := parseUUID(ctx, string(input.ProjectID), "invalid-project-id", "project_id must be a UUID")
			if err != nil {
				return nil, err
			}
			projectID = parsed
		}
		repos, err := svc.ListRepositories(ctx, principal, projectID)
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		views, err := repositoryListResponse(ctx, svc, cfg, repos)
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &contractapi.ListSourceRepositoriesOutput{Body: contractapi.ListSourceRepositoriesOutputBody{Repositories: views}}, nil
	}
}

func getRepository(svc *source.Service, cfg Config) func(context.Context, source.Principal, *contractapi.RepositoryPathInput) (*contractapi.RepositoryOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *contractapi.RepositoryPathInput) (*contractapi.RepositoryOutput, error) {
		repoID, err := parseUUID(ctx, string(input.RepoID), "invalid-repo-id", "repo_id must be a UUID")
		if err != nil {
			return nil, err
		}
		repo, err := svc.GetRepository(ctx, principal, repoID)
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		view, err := repositoryResponse(ctx, svc, cfg, repo)
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &contractapi.RepositoryOutput{Body: view}, nil
	}
}

func repositoryResponse(ctx context.Context, svc *source.Service, cfg Config, repo source.Repository) (contractapi.RepositorySummary, error) {
	if svc == nil || svc.Organizations == nil || svc.Projects == nil {
		return contractapi.RepositorySummary{}, source.ErrStoreUnavailable
	}
	org, err := svc.Organizations.ResolveSourceOrganizationID(ctx, repo.OrgID)
	if err != nil {
		return contractapi.RepositorySummary{}, err
	}
	project, err := svc.Projects.ResolveSourceProject(ctx, repo.OrgID, repo.ProjectID)
	if err != nil {
		return contractapi.RepositorySummary{}, err
	}
	return repositoryView(repo, org, project, cfg.PublicBaseURL, cfg.InstallationID), nil
}

func repositoryListResponse(ctx context.Context, svc *source.Service, cfg Config, repos []source.Repository) (contractapi.Repositories, error) {
	if len(repos) == 0 {
		return contractapi.Repositories{}, nil
	}
	if svc == nil || svc.Organizations == nil || svc.Projects == nil {
		return nil, source.ErrStoreUnavailable
	}
	org, err := svc.Organizations.ResolveSourceOrganizationID(ctx, repos[0].OrgID)
	if err != nil {
		return nil, err
	}
	projects := make(map[uuid.UUID]source.ProjectReference, len(repos))
	for _, repo := range repos {
		if _, ok := projects[repo.ProjectID]; ok {
			continue
		}
		project, err := svc.Projects.ResolveSourceProject(ctx, repo.OrgID, repo.ProjectID)
		if err != nil {
			return nil, err
		}
		projects[repo.ProjectID] = project
	}
	return repositoryViews(repos, org, projects, cfg.PublicBaseURL, cfg.InstallationID), nil
}

func listRefs(svc *source.Service) func(context.Context, source.Principal, *contractapi.RepositoryPathInput) (*contractapi.ListSourceRefsOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *contractapi.RepositoryPathInput) (*contractapi.ListSourceRefsOutput, error) {
		repoID, err := parseUUID(ctx, string(input.RepoID), "invalid-repo-id", "repo_id must be a UUID")
		if err != nil {
			return nil, err
		}
		refs, err := svc.ListRefs(ctx, principal, repoID)
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &contractapi.ListSourceRefsOutput{Body: contractapi.ListSourceRefsOutputBody{Refs: refViews(refs)}}, nil
	}
}

func getTree(svc *source.Service) func(context.Context, source.Principal, *contractapi.GetSourceTreeInput) (*contractapi.GetSourceTreeOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *contractapi.GetSourceTreeInput) (*contractapi.GetSourceTreeOutput, error) {
		repoID, err := parseUUID(ctx, string(input.RepoID), "invalid-repo-id", "repo_id must be a UUID")
		if err != nil {
			return nil, err
		}
		entries, err := svc.Tree(ctx, principal, repoID, string(input.Ref), string(input.Path))
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &contractapi.GetSourceTreeOutput{Body: contractapi.GetSourceTreeOutputBody{Entries: treeEntryViews(entries)}}, nil
	}
}

func getBlob(svc *source.Service) func(context.Context, source.Principal, *contractapi.GetSourceBlobInput) (*contractapi.GetSourceBlobOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *contractapi.GetSourceBlobInput) (*contractapi.GetSourceBlobOutput, error) {
		repoID, err := parseUUID(ctx, string(input.RepoID), "invalid-repo-id", "repo_id must be a UUID")
		if err != nil {
			return nil, err
		}
		blob, err := svc.Blob(ctx, principal, repoID, string(input.Ref), string(input.Path))
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &contractapi.GetSourceBlobOutput{Body: blobView(blob)}, nil
	}
}

func createCheckoutGrant(svc *source.Service, cfg Config) func(context.Context, source.Principal, *contractapi.CreateSourceCheckoutGrantInput) (*contractapi.CheckoutGrantOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *contractapi.CreateSourceCheckoutGrantInput) (*contractapi.CheckoutGrantOutput, error) {
		repoID, err := parseUUID(ctx, string(input.RepoID), "invalid-repo-id", "repo_id must be a UUID")
		if err != nil {
			return nil, err
		}
		grant, err := svc.CreateCheckoutGrant(ctx, principal, repoID, stringFromPointer(input.Body.Ref), "")
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &contractapi.CheckoutGrantOutput{Body: checkoutGrantView(grant, cfg.InstallationID)}, nil
	}
}

func createWorkflowRun(svc *source.Service, cfg Config) func(context.Context, source.Principal, *contractapi.CreateSourceWorkflowRunInput) (*contractapi.WorkflowRunOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *contractapi.CreateSourceWorkflowRunInput) (*contractapi.WorkflowRunOutput, error) {
		repoID, err := parseUUID(ctx, string(input.RepoID), "invalid-repo-id", "repo_id must be a UUID")
		if err != nil {
			return nil, err
		}
		projectID, err := parseUUID(ctx, string(input.Body.ProjectID), "invalid-project-id", "project_id must be a UUID")
		if err != nil {
			return nil, err
		}
		run, err := svc.DispatchWorkflow(ctx, principal, source.WorkflowDispatchRequest{
			RepoID:         repoID,
			ProjectID:      projectID,
			WorkflowPath:   string(input.Body.WorkflowPath),
			Ref:            stringFromPointer(input.Body.Ref),
			Inputs:         workflowInputsFromContract(input.Body.Inputs),
			IdempotencyKey: operationRequestInfoFromContext(ctx).IdempotencyKey,
		})
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &contractapi.WorkflowRunOutput{Body: workflowRunView(run, cfg.InstallationID)}, nil
	}
}

func listWorkflowRuns(svc *source.Service, cfg Config) func(context.Context, source.Principal, *contractapi.RepositoryPathInput) (*contractapi.ListSourceWorkflowRunsOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *contractapi.RepositoryPathInput) (*contractapi.ListSourceWorkflowRunsOutput, error) {
		repoID, err := parseUUID(ctx, string(input.RepoID), "invalid-repo-id", "repo_id must be a UUID")
		if err != nil {
			return nil, err
		}
		runs, err := svc.ListWorkflowRuns(ctx, principal, repoID)
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &contractapi.ListSourceWorkflowRunsOutput{Body: contractapi.ListSourceWorkflowRunsOutputBody{WorkflowRuns: workflowRunViews(runs, cfg.InstallationID)}}, nil
	}
}

func getWorkflowRun(svc *source.Service, cfg Config) func(context.Context, source.Principal, *contractapi.WorkflowRunPathInput) (*contractapi.WorkflowRunOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *contractapi.WorkflowRunPathInput) (*contractapi.WorkflowRunOutput, error) {
		runID, err := parseUUID(ctx, string(input.WorkflowRunID), "invalid-workflow-run-id", "workflow_run_id must be a UUID")
		if err != nil {
			return nil, err
		}
		run, err := svc.GetWorkflowRun(ctx, principal, runID)
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &contractapi.WorkflowRunOutput{Body: workflowRunView(run, cfg.InstallationID)}, nil
	}
}

func internalCreateWorkflowRun(svc *source.Service, cfg Config) func(context.Context, source.Principal, *internalcontractapi.InternalCreateSourceWorkflowRunInput) (*internalcontractapi.WorkflowRunOutput, error) {
	return func(ctx context.Context, _ source.Principal, input *internalcontractapi.InternalCreateSourceWorkflowRunInput) (*internalcontractapi.WorkflowRunOutput, error) {
		orgID := strings.TrimSpace(string(input.Body.OrgID))
		if orgID == "" {
			return nil, badRequest(ctx, "invalid-org-id", "org_id is required", nil)
		}
		repoID, err := parseUUID(ctx, string(input.Body.RepoID), "invalid-repo-id", "repo_id must be a UUID")
		if err != nil {
			return nil, err
		}
		projectID, err := parseUUID(ctx, string(input.Body.ProjectID), "invalid-project-id", "project_id must be a UUID")
		if err != nil {
			return nil, err
		}
		run, err := svc.DispatchWorkflowInternal(ctx, source.InternalWorkflowDispatchRequest{
			OrgID:          orgID,
			ActorID:        string(input.Body.ActorID),
			RepoID:         repoID,
			ProjectID:      projectID,
			WorkflowPath:   string(input.Body.WorkflowPath),
			Ref:            stringFromPointer(input.Body.Ref),
			Inputs:         workflowInputsFromInternalContract(input.Body.Inputs),
			IdempotencyKey: string(input.Body.IdempotencyKey),
		})
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &internalcontractapi.WorkflowRunOutput{Body: internalWorkflowRunView(run, cfg.InstallationID)}, nil
	}
}

func downloadArchive(svc *source.Service) func(context.Context, source.Principal, *internalcontractapi.DownloadSourceCheckoutArchiveInput) (*internalcontractapi.DownloadSourceCheckoutArchiveOutput, error) {
	return func(ctx context.Context, _ source.Principal, input *internalcontractapi.DownloadSourceCheckoutArchiveInput) (*internalcontractapi.DownloadSourceCheckoutArchiveOutput, error) {
		grantID, err := parseUUID(ctx, string(input.GrantID), "invalid-grant-id", "grant_id must be a UUID")
		if err != nil {
			return nil, err
		}
		data, contentType, grant, _, err := svc.ConsumeArchive(ctx, grantID, string(input.Token))
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &internalcontractapi.DownloadSourceCheckoutArchiveOutput{
			ContentType:        internalcontractapi.MediaType(contentType),
			ContentDisposition: internalcontractapi.ContentDisposition(`attachment; filename="source-` + grant.GrantID.String() + `.tar.gz"`),
			Body:               data,
		}, nil
	}
}

func parseUUID(ctx context.Context, value string, code string, detail string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return uuid.UUID{}, badRequest(ctx, code, detail, err)
	}
	return parsed, nil
}

func stringFromPointer[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func int64FromPointer[T ~int64](value *T) int64 {
	if value == nil {
		return 0
	}
	return int64(*value)
}

func WebhookHandler(svc *source.Service, secret string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := apiTracer.Start(r.Context(), "source.webhook.receive")
		defer span.End()
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		event := r.Header.Get("X-Forgejo-Event")
		delivery := r.Header.Get("X-Forgejo-Delivery")
		valid := verifyWebhookSignature(secret, r.Header.Get("X-Forgejo-Signature"), body)
		span.SetAttributes(attribute.String("source.webhook_event", event), attribute.String("source.webhook_delivery", delivery), attribute.Bool("source.webhook_valid", valid))
		if !valid {
			span.SetStatus(codes.Error, "invalid signature")
			_ = svc.RecordWebhook(ctx, "forgejo", event, delivery, false, body)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if err := svc.RecordWebhook(ctx, "forgejo", event, delivery, true, body); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
}

func verifyWebhookSignature(secret, signature string, body []byte) bool {
	if strings.TrimSpace(secret) == "" {
		return false
	}
	signature = strings.TrimPrefix(strings.TrimSpace(signature), "sha256=")
	got, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}
