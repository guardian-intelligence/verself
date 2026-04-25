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
	"go.opentelemetry.io/otel/trace"

	"github.com/forge-metal/source-code-hosting-service/internal/source"
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

type createCheckoutGrantInput struct {
	RepoID string `path:"repo_id" format:"uuid"`
	Body   CreateCheckoutGrantRequest
}

type checkoutArchiveInput struct {
	GrantID string `path:"grant_id" format:"uuid"`
	Token   string `header:"X-Forge-Metal-Checkout-Token" required:"true"`
}

type createIntegrationInput struct {
	Body CreateIntegrationRequest
}

type repositoryOutput struct {
	Body Repository
}

type repositoryListOutput struct {
	Body RepositoryList
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

type archiveOutput struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	Body               []byte
}

type integrationOutput struct {
	Body ExternalIntegration
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
		OperationID:   "create-source-integration",
		Method:        http.MethodPost,
		Path:          "/api/v1/integrations",
		Summary:       "Register an external source integration",
		DefaultStatus: http.StatusCreated,
	}, operationPolicy{
		Permission:       permissionIntegrationWrite,
		Resource:         "source_integration",
		Action:           "create",
		OrgScope:         "token_org_id",
		RateLimitClass:   "source_mutation",
		Idempotency:      idempotencyHeaderKey,
		AuditEvent:       "source.integration.create",
		OperationDisplay: "create source integration",
		OperationType:    "write",
		RiskLevel:        "high",
		BodyLimitBytes:   bodyLimitSmallJSON,
	}, createIntegration(svc))
}

func RegisterInternalRoutes(api huma.API, cfg Config) {
	svc := cfg.Service
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

func createRepository(svc *source.Service) func(context.Context, source.Principal, *createRepositoryInput) (*repositoryOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *createRepositoryInput) (*repositoryOutput, error) {
		repo, err := svc.CreateRepository(ctx, principal, source.CreateRepositoryRequest{
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

func listRepositories(svc *source.Service) func(context.Context, source.Principal, *struct{}) (*repositoryListOutput, error) {
	return func(ctx context.Context, principal source.Principal, _ *struct{}) (*repositoryListOutput, error) {
		repos, err := svc.ListRepositories(ctx, principal)
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

func createIntegration(svc *source.Service) func(context.Context, source.Principal, *createIntegrationInput) (*integrationOutput, error) {
	return func(ctx context.Context, principal source.Principal, input *createIntegrationInput) (*integrationOutput, error) {
		integration, err := svc.CreateExternalIntegration(ctx, principal, input.Body.Provider, input.Body.ExternalRepo, input.Body.CredentialRef)
		if err != nil {
			return nil, sourceError(ctx, err)
		}
		return &integrationOutput{Body: integrationDTO(integration)}, nil
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
			_ = svc.RecordWebhook(ctx, "forgejo", event, delivery, false)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if err := svc.RecordWebhook(ctx, "forgejo", event, delivery, true); err != nil {
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
