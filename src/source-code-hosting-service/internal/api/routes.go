package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/source-code-hosting-service/internal/source"
)

type Config struct {
	Service       *source.Service
	PublicBaseURL string
	WebhookSecret string
}

type repositoryPath struct {
	RepoID string `path:"repo_id" format:"uuid"`
}

type treeInput struct {
	RepoID string `path:"repo_id" format:"uuid"`
	Ref    string `query:"ref,omitempty" maxLength:"255"`
	Path   string `query:"path,omitempty" maxLength:"2048"`
}

type blobInput struct {
	RepoID string `path:"repo_id" format:"uuid"`
	Ref    string `query:"ref,omitempty" maxLength:"255"`
	Path   string `query:"path" required:"true" minLength:"1" maxLength:"2048"`
}

type createRepositoryInput struct {
	Body CreateRepositoryRequest
}

type listRepositoriesInput struct {
	ProjectID string `query:"project_id,omitempty" format:"uuid"`
}

type createGitCredentialInput struct {
	Body CreateGitCredentialRequest
}

type createCheckoutGrantInput struct {
	RepoID string `path:"repo_id" format:"uuid"`
	Body   CreateCheckoutGrantRequest
}

type createWorkflowRunInput struct {
	RepoID string `path:"repo_id" format:"uuid"`
	Body   CreateWorkflowRunRequest
}

type workflowRunPath struct {
	WorkflowRunID string `path:"workflow_run_id" format:"uuid"`
}

type internalCreateWorkflowRunInput struct {
	Body InternalCreateWorkflowRunRequest
}

type internalResolveRepositoryInput struct {
	Body InternalResolveRepositoryRequest
}

type checkoutArchiveInput struct {
	GrantID string `path:"grant_id" format:"uuid"`
	Token   string `header:"X-Verself-Checkout-Token" required:"true"`
}

type repositoryOutput struct {
	Body Repository
}

type repositoryListOutput struct {
	Body RepositoryList
}

type gitCredentialOutput struct {
	Body GitCredential
}

type refListOutput struct {
	Body RefList
}

type treeOutput struct {
	Body Tree
}

type blobOutput struct {
	Body Blob
}

type checkoutGrantOutput struct {
	Body CheckoutGrant
}

type workflowRunOutput struct {
	Body WorkflowRun
}

type workflowRunListOutput struct {
	Body WorkflowRunList
}

type archiveOutput struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	Body               []byte
}

func RegisterRoutes(api huma.API, cfg Config) {
	svc := cfg.Service
	registerSourceRoute(api, huma.Operation{
		OperationID:   "create-source-repository",
		Method:        http.MethodPost,
		Path:          "/api/v1/repos",
		Summary:       "Create a source repository",
		DefaultStatus: http.StatusCreated,
	}, operationPolicy{
		Permission:       permissionRepoWrite,
		Resource:         "source_repository",
		Action:           "create",
		OrgScope:         "token_org_id",
		RateLimitClass:   "source_mutation",
		Idempotency:      idempotencyHeaderKey,
		AuditEvent:       "source.repo.create",
		OperationDisplay: "create source repository",
		OperationType:    "write",
		RiskLevel:        "medium",
		BodyLimitBytes:   bodyLimitSmallJSON,
	}, createRepository(svc))

	registerSourceRoute(api, huma.Operation{
		OperationID:   "create-source-git-credential",
		Method:        http.MethodPost,
		Path:          "/api/v1/git-credentials",
		Summary:       "Create a scoped HTTPS Git credential",
		DefaultStatus: http.StatusCreated,
	}, operationPolicy{
		Permission:       permissionGitCredentialWrite,
		Resource:         "source_git_credential",
		Action:           "create",
		OrgScope:         "token_org_id",
		RateLimitClass:   "source_mutation",
		Idempotency:      idempotencyHeaderKey,
		AuditEvent:       "source.git_credential.create",
		OperationDisplay: "create source Git credential",
		OperationType:    "write",
		RiskLevel:        "high",
		BodyLimitBytes:   bodyLimitSmallJSON,
	}, createGitCredential(svc))

	registerSourceRoute(api, huma.Operation{
		OperationID: "list-source-repositories",
		Method:      http.MethodGet,
		Path:        "/api/v1/repos",
		Summary:     "List source repositories",
	}, operationPolicy{
		Permission:       permissionRepoRead,
		Resource:         "source_repository",
		Action:           "list",
		OrgScope:         "token_org_id",
		RateLimitClass:   "read",
		AuditEvent:       "source.repo.list",
		OperationDisplay: "list source repositories",
		OperationType:    "read",
		RiskLevel:        "low",
	}, listRepositories(svc))

	registerSourceRoute(api, huma.Operation{
		OperationID: "get-source-repository",
		Method:      http.MethodGet,
		Path:        "/api/v1/repos/{repo_id}",
		Summary:     "Get a source repository",
	}, operationPolicy{
		Permission:       permissionRepoRead,
		Resource:         "source_repository",
		Action:           "read",
		OrgScope:         "token_org_id",
		RateLimitClass:   "read",
		AuditEvent:       "source.repo.read",
		OperationDisplay: "read source repository",
		OperationType:    "read",
		RiskLevel:        "low",
	}, getRepository(svc))

	registerSourceRoute(api, huma.Operation{
		OperationID: "list-source-refs",
		Method:      http.MethodGet,
		Path:        "/api/v1/repos/{repo_id}/refs",
		Summary:     "List repository refs",
	}, operationPolicy{
		Permission:       permissionRepoRead,
		Resource:         "source_ref",
		Action:           "list",
		OrgScope:         "token_org_id",
		RateLimitClass:   "read",
		AuditEvent:       "source.refs.list",
		OperationDisplay: "list source refs",
		OperationType:    "read",
		RiskLevel:        "low",
	}, listRefs(svc))

	registerSourceRoute(api, huma.Operation{
		OperationID: "get-source-tree",
		Method:      http.MethodGet,
		Path:        "/api/v1/repos/{repo_id}/tree",
		Summary:     "Get a repository tree path",
	}, operationPolicy{
		Permission:       permissionRepoRead,
		Resource:         "source_tree",
		Action:           "read",
		OrgScope:         "token_org_id",
		RateLimitClass:   "read",
		AuditEvent:       "source.tree.get",
		OperationDisplay: "read source tree",
		OperationType:    "read",
		RiskLevel:        "low",
	}, getTree(svc))

	registerSourceRoute(api, huma.Operation{
		OperationID: "get-source-blob",
		Method:      http.MethodGet,
		Path:        "/api/v1/repos/{repo_id}/blob",
		Summary:     "Get a repository blob",
	}, operationPolicy{
		Permission:       permissionRepoRead,
		Resource:         "source_blob",
		Action:           "read",
		OrgScope:         "token_org_id",
		RateLimitClass:   "read",
		AuditEvent:       "source.blob.get",
		OperationDisplay: "read source blob",
		OperationType:    "read",
		RiskLevel:        "medium",
	}, getBlob(svc))

	registerSourceRoute(api, huma.Operation{
		OperationID:   "create-source-checkout-grant",
		Method:        http.MethodPost,
		Path:          "/api/v1/repos/{repo_id}/checkout-grants",
		Summary:       "Create a short-lived checkout grant",
		DefaultStatus: http.StatusCreated,
	}, operationPolicy{
		Permission:       permissionCheckoutWrite,
		Resource:         "source_checkout_grant",
		Action:           "create",
		OrgScope:         "token_org_id",
		RateLimitClass:   "source_mutation",
		Idempotency:      idempotencyHeaderKey,
		AuditEvent:       "source.checkout_grant.create",
		OperationDisplay: "create source checkout grant",
		OperationType:    "write",
		RiskLevel:        "high",
		BodyLimitBytes:   bodyLimitSmallJSON,
	}, createCheckoutGrant(svc))

	registerSourceRoute(api, huma.Operation{
		OperationID:   "create-source-workflow-run",
		Method:        http.MethodPost,
		Path:          "/api/v1/repos/{repo_id}/workflow-runs",
		Summary:       "Dispatch a repository workflow",
		DefaultStatus: http.StatusCreated,
	}, operationPolicy{
		Permission:       permissionWorkflowWrite,
		Resource:         "source_workflow_run",
		Action:           "create",
		OrgScope:         "token_org_id",
		RateLimitClass:   "source_mutation",
		Idempotency:      idempotencyHeaderKey,
		AuditEvent:       "source.workflow.dispatch",
		OperationDisplay: "dispatch source workflow",
		OperationType:    "write",
		RiskLevel:        "high",
		BodyLimitBytes:   bodyLimitSmallJSON,
	}, createWorkflowRun(svc))

	registerSourceRoute(api, huma.Operation{
		OperationID: "list-source-workflow-runs",
		Method:      http.MethodGet,
		Path:        "/api/v1/repos/{repo_id}/workflow-runs",
		Summary:     "List repository workflow runs",
	}, operationPolicy{
		Permission:       permissionWorkflowRead,
		Resource:         "source_workflow_run",
		Action:           "list",
		OrgScope:         "token_org_id",
		RateLimitClass:   "read",
		AuditEvent:       "source.workflow_run.list",
		OperationDisplay: "list source workflow runs",
		OperationType:    "read",
		RiskLevel:        "low",
	}, listWorkflowRuns(svc))

	registerSourceRoute(api, huma.Operation{
		OperationID: "get-source-workflow-run",
		Method:      http.MethodGet,
		Path:        "/api/v1/workflow-runs/{workflow_run_id}",
		Summary:     "Get a workflow run",
	}, operationPolicy{
		Permission:       permissionWorkflowRead,
		Resource:         "source_workflow_run",
		Action:           "read",
		OrgScope:         "token_org_id",
		RateLimitClass:   "read",
		AuditEvent:       "source.workflow_run.read",
		OperationDisplay: "read source workflow run",
		OperationType:    "read",
		RiskLevel:        "low",
	}, getWorkflowRun(svc))

}

func RegisterInternalRoutes(api huma.API, cfg Config) {
	svc := cfg.Service
	registerSourceRoute(api, huma.Operation{
		OperationID:   "internal-resolve-source-repository",
		Method:        http.MethodPost,
		Path:          "/internal/v1/repos/resolve",
		Summary:       "Resolve a source repository on behalf of a repo-owned service",
		DefaultStatus: http.StatusOK,
	}, operationPolicy{
		Permission:       permissionRepoRead,
		Resource:         "source_repository",
		Action:           "resolve",
		OrgScope:         "body_org_id",
		RateLimitClass:   "internal_read",
		AuditEvent:       "source.repo.resolve.internal",
		OperationDisplay: "resolve source repository internally",
		OperationType:    "read",
		RiskLevel:        "medium",
		BodyLimitBytes:   bodyLimitSmallJSON,
		Internal:         true,
	}, internalResolveRepository(svc))

	registerSourceRoute(api, huma.Operation{
		OperationID:   "internal-create-source-workflow-run",
		Method:        http.MethodPost,
		Path:          "/internal/v1/workflow-runs",
		Summary:       "Dispatch a source workflow on behalf of a service-owned schedule",
		DefaultStatus: http.StatusCreated,
	}, operationPolicy{
		Permission:       permissionWorkflowWrite,
		Resource:         "source_workflow_run",
		Action:           "create",
		OrgScope:         "body_org_id",
		RateLimitClass:   "internal_mutation",
		AuditEvent:       "source.workflow.dispatch.internal",
		OperationDisplay: "dispatch source workflow internally",
		OperationType:    "write",
		RiskLevel:        "high",
		BodyLimitBytes:   bodyLimitSmallJSON,
		Internal:         true,
	}, internalCreateWorkflowRun(svc))

	registerSourceRoute(api, huma.Operation{
		OperationID: "download-source-checkout-archive",
		Method:      http.MethodGet,
		Path:        "/internal/v1/checkouts/{grant_id}/archive",
		Summary:     "Download an archive using a short-lived checkout grant",
		Responses: map[string]*huma.Response{
			"200": {
				Description: "tar.gz source archive",
				Content: map[string]*huma.MediaType{
					"application/gzip": {Schema: &huma.Schema{Type: "string", Format: "binary"}},
				},
			},
		},
	}, operationPolicy{
		Permission:       permissionCheckoutWrite,
		Resource:         "source_checkout_grant",
		Action:           "consume",
		OrgScope:         "checkout_grant",
		RateLimitClass:   "checkout_download",
		AuditEvent:       "source.archive.stream",
		OperationDisplay: "download source archive",
		OperationType:    "read",
		RiskLevel:        "high",
		Internal:         true,
	}, downloadArchive(svc))
}

func internalResolveRepository(svc *source.Service) func(context.Context, source.Principal, *internalResolveRepositoryInput) (*repositoryOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *internalResolveRepositoryInput) (*repositoryOutput, error) {
		orgID, err := strconv.ParseUint(strings.TrimSpace(input.Body.OrgID), 10, 64)
		if err != nil || orgID == 0 {
			return nil, badRequest(ctx, "invalid-org-id", "org_id must be a non-zero unsigned integer", err)
		}
		trace.SpanFromContext(ctx).SetAttributes(attribute.Int64("verself.org_id", int64(orgID)))
		repo, err := svc.GetRepository(ctx, source.Principal{Subject: principal.Subject, OrgID: orgID}, input.Body.RepoID)
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		trace.SpanFromContext(ctx).SetAttributes(
			attribute.String("source.repo_id", repo.RepoID.String()),
			attribute.String("verself.project_id", repo.ProjectID.String()),
		)
		return &repositoryOutput{Body: repositoryDTO(repo)}, nil
	}
}

func createRepository(svc *source.Service) func(context.Context, source.Principal, *createRepositoryInput) (*repositoryOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *createRepositoryInput) (*repositoryOutput, error) {
		repo, err := svc.CreateRepository(ctx, principal, source.CreateRepositoryRequest{
			ProjectID:     input.Body.ProjectID,
			Name:          input.Body.Name,
			Description:   input.Body.Description,
			DefaultBranch: input.Body.DefaultBranch,
		})
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &repositoryOutput{Body: repositoryDTO(repo)}, nil
	}
}

func createGitCredential(svc *source.Service) func(context.Context, source.Principal, *createGitCredentialInput) (*gitCredentialOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *createGitCredentialInput) (*gitCredentialOutput, error) {
		credential, err := svc.CreateGitCredential(ctx, principal, source.CreateGitCredentialRequest{
			Label:            input.Body.Label,
			ExpiresInSeconds: input.Body.ExpiresInSeconds,
		})
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &gitCredentialOutput{Body: gitCredentialDTO(credential)}, nil
	}
}

func listRepositories(svc *source.Service) func(context.Context, source.Principal, *listRepositoriesInput) (*repositoryListOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *listRepositoriesInput) (*repositoryListOutput, error) {
		var projectID uuid.UUID
		if input.ProjectID != "" {
			parsed, err := uuid.Parse(input.ProjectID)
			if err != nil {
				return nil, badRequest(ctx, "invalid-project-id", "project_id must be a UUID", err)
			}
			projectID = parsed
		}
		repos, err := svc.ListRepositories(ctx, principal, projectID)
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &repositoryListOutput{Body: RepositoryList{Repositories: repositoryDTOs(repos)}}, nil
	}
}

func getRepository(svc *source.Service) func(context.Context, source.Principal, *repositoryPath) (*repositoryOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *repositoryPath) (*repositoryOutput, error) {
		repoID, err := uuid.Parse(input.RepoID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-repo-id", "repo_id must be a UUID", err)
		}
		repo, err := svc.GetRepository(ctx, principal, repoID)
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &repositoryOutput{Body: repositoryDTO(repo)}, nil
	}
}

func listRefs(svc *source.Service) func(context.Context, source.Principal, *repositoryPath) (*refListOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *repositoryPath) (*refListOutput, error) {
		repoID, err := uuid.Parse(input.RepoID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-repo-id", "repo_id must be a UUID", err)
		}
		refs, err := svc.ListRefs(ctx, principal, repoID)
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &refListOutput{Body: RefList{Refs: refDTOs(refs)}}, nil
	}
}

func getTree(svc *source.Service) func(context.Context, source.Principal, *treeInput) (*treeOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *treeInput) (*treeOutput, error) {
		repoID, err := uuid.Parse(input.RepoID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-repo-id", "repo_id must be a UUID", err)
		}
		entries, err := svc.Tree(ctx, principal, repoID, input.Ref, input.Path)
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &treeOutput{Body: Tree{Entries: treeEntryDTOs(entries)}}, nil
	}
}

func getBlob(svc *source.Service) func(context.Context, source.Principal, *blobInput) (*blobOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *blobInput) (*blobOutput, error) {
		repoID, err := uuid.Parse(input.RepoID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-repo-id", "repo_id must be a UUID", err)
		}
		blob, err := svc.Blob(ctx, principal, repoID, input.Ref, input.Path)
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &blobOutput{Body: blobDTO(blob)}, nil
	}
}

func createCheckoutGrant(svc *source.Service) func(context.Context, source.Principal, *createCheckoutGrantInput) (*checkoutGrantOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *createCheckoutGrantInput) (*checkoutGrantOutput, error) {
		repoID, err := uuid.Parse(input.RepoID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-repo-id", "repo_id must be a UUID", err)
		}
		grant, err := svc.CreateCheckoutGrant(ctx, principal, repoID, input.Body.Ref, input.Body.PathPrefix)
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &checkoutGrantOutput{Body: checkoutGrantDTO(grant)}, nil
	}
}

func createWorkflowRun(svc *source.Service) func(context.Context, source.Principal, *createWorkflowRunInput) (*workflowRunOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *createWorkflowRunInput) (*workflowRunOutput, error) {
		repoID, err := uuid.Parse(input.RepoID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-repo-id", "repo_id must be a UUID", err)
		}
		run, err := svc.DispatchWorkflow(ctx, principal, source.WorkflowDispatchRequest{
			RepoID:         repoID,
			ProjectID:      input.Body.ProjectID,
			WorkflowPath:   input.Body.WorkflowPath,
			Ref:            input.Body.Ref,
			Inputs:         input.Body.Inputs,
			IdempotencyKey: operationRequestInfoFromContext(ctx).IdempotencyKey,
		})
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &workflowRunOutput{Body: workflowRunDTO(run)}, nil
	}
}

func listWorkflowRuns(svc *source.Service) func(context.Context, source.Principal, *repositoryPath) (*workflowRunListOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *repositoryPath) (*workflowRunListOutput, error) {
		repoID, err := uuid.Parse(input.RepoID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-repo-id", "repo_id must be a UUID", err)
		}
		runs, err := svc.ListWorkflowRuns(ctx, principal, repoID)
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &workflowRunListOutput{Body: WorkflowRunList{WorkflowRuns: workflowRunDTOs(runs)}}, nil
	}
}

func getWorkflowRun(svc *source.Service) func(context.Context, source.Principal, *workflowRunPath) (*workflowRunOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *workflowRunPath) (*workflowRunOutput, error) {
		runID, err := uuid.Parse(input.WorkflowRunID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-workflow-run-id", "workflow_run_id must be a UUID", err)
		}
		run, err := svc.GetWorkflowRun(ctx, principal, runID)
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &workflowRunOutput{Body: workflowRunDTO(run)}, nil
	}
}

func internalCreateWorkflowRun(svc *source.Service) func(context.Context, source.Principal, *internalCreateWorkflowRunInput) (*workflowRunOutput, error) {
	return func(ctx context.Context, _ source.Principal, input *internalCreateWorkflowRunInput) (*workflowRunOutput, error) {
		orgID, err := strconv.ParseUint(strings.TrimSpace(input.Body.OrgID), 10, 64)
		if err != nil || orgID == 0 {
			return nil, badRequest(ctx, "invalid-org-id", "org_id must be a non-zero unsigned integer", err)
		}
		run, err := svc.DispatchWorkflowInternal(ctx, source.InternalWorkflowDispatchRequest{
			OrgID:          orgID,
			ActorID:        input.Body.ActorID,
			RepoID:         input.Body.RepoID,
			ProjectID:      input.Body.ProjectID,
			WorkflowPath:   input.Body.WorkflowPath,
			Ref:            input.Body.Ref,
			Inputs:         input.Body.Inputs,
			IdempotencyKey: input.Body.IdempotencyKey,
		})
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &workflowRunOutput{Body: workflowRunDTO(run)}, nil
	}
}

func downloadArchive(svc *source.Service) func(context.Context, source.Principal, *checkoutArchiveInput) (*archiveOutput, error) {
	return func(ctx context.Context, _ source.Principal, input *checkoutArchiveInput) (*archiveOutput, error) {
		grantID, err := uuid.Parse(input.GrantID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-grant-id", "grant_id must be a UUID", err)
		}
		data, contentType, grant, _, err := svc.ConsumeArchive(ctx, grantID, input.Token)
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &archiveOutput{
			ContentType:        contentType,
			ContentDisposition: `attachment; filename="source-` + grant.GrantID.String() + `.tar.gz"`,
			Body:               data,
		}, nil
	}
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

func annotateRepoSpan(ctx context.Context, repoID uuid.UUID) {
	trace.SpanFromContext(ctx).SetAttributes(attribute.String("source.repo_id", repoID.String()))
}
