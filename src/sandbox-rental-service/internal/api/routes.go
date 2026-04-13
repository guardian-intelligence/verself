// Package api registers sandbox-rental-service HTTP routes on a Huma API.
package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/forge-metal/apiwire"
	auth "github.com/forge-metal/auth-middleware"
	billingclient "github.com/forge-metal/billing-service/client"

	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
)

// RegisterRoutes wires all sandbox-rental-service endpoints onto the Huma API.
func RegisterRoutes(api huma.API, svc *jobs.Service, billing *billingclient.ServiceClient, publicConfig PublicAPIConfig) {
	registerSecured(api, secured(huma.Operation{
		OperationID:   "import-repo",
		Method:        http.MethodPost,
		Path:          "/api/v1/repos",
		Summary:       "Import or rescan repo metadata",
		DefaultStatus: 201,
	}, operationPolicy{
		Permission:     permissionRepoWrite,
		Resource:       "repo",
		Action:         "import",
		OrgScope:       "token_org_id",
		RateLimitClass: "repo_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "sandbox.repo.import",
		BodyLimitBytes: bodyLimitRepoImport,
	}), importRepo(svc))

	registerSecured(api, secured(huma.Operation{
		OperationID: "list-repos",
		Method:      http.MethodGet,
		Path:        "/api/v1/repos",
		Summary:     "List imported repos for the current org",
	}, operationPolicy{
		Permission:     permissionRepoRead,
		Resource:       "repo",
		Action:         "list",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "sandbox.repo.list",
	}), listRepos(svc))

	registerSecured(api, secured(huma.Operation{
		OperationID: "get-repo",
		Method:      http.MethodGet,
		Path:        "/api/v1/repos/{repo_id}",
		Summary:     "Get repo state and compatibility details",
	}, operationPolicy{
		Permission:     permissionRepoRead,
		Resource:       "repo",
		Action:         "read",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "sandbox.repo.read",
	}), getRepo(svc))

	registerSecured(api, secured(huma.Operation{
		OperationID:   "rescan-repo",
		Method:        http.MethodPost,
		Path:          "/api/v1/repos/{repo_id}/rescan",
		Summary:       "Rescan repo metadata",
		DefaultStatus: 200,
	}, operationPolicy{
		Permission:     permissionRepoWrite,
		Resource:       "repo",
		Action:         "rescan",
		OrgScope:       "token_org_id",
		RateLimitClass: "repo_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "sandbox.repo.rescan",
		BodyLimitBytes: bodyLimitNoBody,
	}), rescanRepo(svc))

	registerSecured(api, secured(huma.Operation{
		OperationID:   "create-webhook-endpoint",
		Method:        http.MethodPost,
		Path:          "/api/v1/webhook-endpoints",
		Summary:       "Create a git webhook endpoint",
		DefaultStatus: 201,
	}, operationPolicy{
		Permission:     permissionWebhookWrite,
		Resource:       "webhook_endpoint",
		Action:         "create",
		OrgScope:       "token_org_id",
		RateLimitClass: "webhook_endpoint_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "sandbox.webhook_endpoint.create",
		BodyLimitBytes: bodyLimitSmallJSON,
	}), createWebhookEndpoint(svc, publicConfig.PublicBaseURL))

	registerSecured(api, secured(huma.Operation{
		OperationID: "list-webhook-endpoints",
		Method:      http.MethodGet,
		Path:        "/api/v1/webhook-endpoints",
		Summary:     "List git webhook endpoints for the current org",
	}, operationPolicy{
		Permission:     permissionWebhookRead,
		Resource:       "webhook_endpoint",
		Action:         "list",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "sandbox.webhook_endpoint.list",
	}), listWebhookEndpoints(svc))

	registerSecured(api, secured(huma.Operation{
		OperationID:   "rotate-webhook-endpoint-secret",
		Method:        http.MethodPost,
		Path:          "/api/v1/webhook-endpoints/{endpoint_id}/rotate",
		Summary:       "Rotate a git webhook endpoint secret",
		DefaultStatus: 200,
	}, operationPolicy{
		Permission:     permissionWebhookWrite,
		Resource:       "webhook_endpoint_secret",
		Action:         "rotate",
		OrgScope:       "token_org_id",
		RateLimitClass: "webhook_endpoint_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "sandbox.webhook_endpoint.secret.rotate",
		BodyLimitBytes: bodyLimitNoBody,
	}), rotateWebhookEndpointSecret(svc))

	registerSecured(api, secured(huma.Operation{
		OperationID:   "delete-webhook-endpoint",
		Method:        http.MethodDelete,
		Path:          "/api/v1/webhook-endpoints/{endpoint_id}",
		Summary:       "Deactivate a git webhook endpoint",
		DefaultStatus: 204,
	}, operationPolicy{
		Permission:     permissionWebhookWrite,
		Resource:       "webhook_endpoint",
		Action:         "delete",
		OrgScope:       "token_org_id",
		RateLimitClass: "webhook_endpoint_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "sandbox.webhook_endpoint.delete",
		BodyLimitBytes: bodyLimitNoBody,
	}), deleteWebhookEndpoint(svc))

	registerSecured(api, secured(huma.Operation{
		OperationID:   "submit-execution",
		Method:        http.MethodPost,
		Path:          "/api/v1/executions",
		Summary:       "Submit a new execution",
		DefaultStatus: 201,
	}, operationPolicy{
		Permission:     permissionExecutionSubmit,
		Resource:       "execution",
		Action:         "submit",
		OrgScope:       "token_org_id",
		RateLimitClass: "execution_submit",
		Idempotency:    idempotencyRequestBodyKey,
		AuditEvent:     "sandbox.execution.submit",
		BodyLimitBytes: bodyLimitExecutionPost,
	}), submitExecution(svc))

	registerSecured(api, secured(huma.Operation{
		OperationID: "get-execution",
		Method:      http.MethodGet,
		Path:        "/api/v1/executions/{execution_id}",
		Summary:     "Get execution status and latest attempt",
	}, operationPolicy{
		Permission:     permissionExecutionRead,
		Resource:       "execution",
		Action:         "read",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "sandbox.execution.read",
	}), getExecution(svc))

	registerSecured(api, secured(huma.Operation{
		OperationID: "get-execution-logs",
		Method:      http.MethodGet,
		Path:        "/api/v1/executions/{execution_id}/logs",
		Summary:     "Get latest execution attempt log output",
	}, operationPolicy{
		Permission:     permissionLogsRead,
		Resource:       "execution_logs",
		Action:         "read",
		OrgScope:       "token_org_id",
		RateLimitClass: "logs_read",
		AuditEvent:     "sandbox.execution.logs.read",
	}), getExecutionLogs(svc))

	registerSecured(api, secured(huma.Operation{
		OperationID:   "create-scheduler-probe",
		Method:        http.MethodPost,
		Path:          "/api/v1/scheduler/probes",
		Summary:       "Enqueue a scheduler runtime probe",
		DefaultStatus: 202,
		Hidden:        true,
	}, operationPolicy{
		Permission:     permissionExecutionSubmit,
		Resource:       "scheduler_probe",
		Action:         "create",
		OrgScope:       "token_org_id",
		RateLimitClass: "scheduler_probe",
		AuditEvent:     "sandbox.scheduler.probe.create",
		BodyLimitBytes: bodyLimitSmallJSON,
	}), createSchedulerProbe(svc))

	// Billing proxy — frontend calls these; we enforce org_id from JWT
	// and forward to the billing-service on loopback.
	registerSecured(api, secured(huma.Operation{
		OperationID: "get-billing-entitlements",
		Method:      http.MethodGet,
		Path:        "/api/v1/billing/entitlements",
		Summary:     "Get org entitlements view",
	}, operationPolicy{
		Permission:     permissionBillingRead,
		Resource:       "billing_entitlements",
		Action:         "read",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "billing.entitlements.read",
	}), getBillingEntitlements(billing))

	registerSecured(api, secured(huma.Operation{
		OperationID: "list-billing-contracts",
		Method:      http.MethodGet,
		Path:        "/api/v1/billing/contracts",
		Summary:     "List org billing contracts",
	}, operationPolicy{
		Permission:     permissionBillingRead,
		Resource:       "billing_contract",
		Action:         "list",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "billing.contract.list",
	}), listBillingContracts(billing))

	registerSecured(api, secured(huma.Operation{
		OperationID: "list-billing-plans",
		Method:      http.MethodGet,
		Path:        "/api/v1/billing/plans",
		Summary:     "List contract plans",
	}, operationPolicy{
		Permission:     permissionBillingRead,
		Resource:       "billing_plan",
		Action:         "list",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "billing.plan.list",
	}), listBillingPlans(billing))

	registerSecured(api, secured(huma.Operation{
		OperationID: "get-billing-statement",
		Method:      http.MethodGet,
		Path:        "/api/v1/billing/statement",
		Summary:     "Get current billing statement",
	}, operationPolicy{
		Permission:     permissionBillingRead,
		Resource:       "billing_statement",
		Action:         "read",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "billing.statement.read",
	}), getBillingStatement(billing))

	registerSecured(api, secured(huma.Operation{
		OperationID:   "create-billing-checkout",
		Method:        http.MethodPost,
		Path:          "/api/v1/billing/checkout",
		Summary:       "Create Stripe checkout session for credit purchase",
		DefaultStatus: 200,
	}, operationPolicy{
		Permission:     permissionBillingCheckout,
		Resource:       "billing_checkout",
		Action:         "create",
		OrgScope:       "token_org_id",
		RateLimitClass: "billing_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "billing.checkout.create",
		BodyLimitBytes: bodyLimitSmallJSON,
	}), createBillingCheckout(billing, publicConfig.BillingReturnOrigins))

	registerSecured(api, secured(huma.Operation{
		OperationID:   "create-billing-contract",
		Method:        http.MethodPost,
		Path:          "/api/v1/billing/contracts",
		Summary:       "Create self-serve contract checkout",
		DefaultStatus: 200,
	}, operationPolicy{
		Permission:     permissionBillingCheckout,
		Resource:       "billing_contract_checkout",
		Action:         "create",
		OrgScope:       "token_org_id",
		RateLimitClass: "billing_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "billing.contract_checkout.create",
		BodyLimitBytes: bodyLimitSmallJSON,
	}), createBillingContract(billing, publicConfig.BillingReturnOrigins))

	registerSecured(api, secured(huma.Operation{
		OperationID:   "create-billing-contract-change",
		Method:        http.MethodPost,
		Path:          "/api/v1/billing/contracts/{contract_id}/changes",
		Summary:       "Create invoice-backed contract change",
		DefaultStatus: 200,
	}, operationPolicy{
		Permission:     permissionBillingCheckout,
		Resource:       "billing_contract_change",
		Action:         "create",
		OrgScope:       "token_org_id",
		RateLimitClass: "billing_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "billing.contract_change.create",
		BodyLimitBytes: bodyLimitSmallJSON,
	}), createBillingContractChange(billing, publicConfig.BillingReturnOrigins))

	registerSecured(api, secured(huma.Operation{
		OperationID:   "cancel-billing-contract",
		Method:        http.MethodPost,
		Path:          "/api/v1/billing/contracts/{contract_id}/cancel",
		Summary:       "Schedule contract cancellation",
		DefaultStatus: 200,
	}, operationPolicy{
		Permission:     permissionBillingCheckout,
		Resource:       "billing_contract",
		Action:         "cancel",
		OrgScope:       "token_org_id",
		RateLimitClass: "billing_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "billing.contract.cancel",
		BodyLimitBytes: bodyLimitNoBody,
	}), cancelBillingContract(billing))

	registerSecured(api, secured(huma.Operation{
		OperationID:   "create-billing-portal",
		Method:        http.MethodPost,
		Path:          "/api/v1/billing/portal",
		Summary:       "Create Stripe billing portal session",
		DefaultStatus: 200,
	}, operationPolicy{
		Permission:     permissionBillingCheckout,
		Resource:       "billing_portal",
		Action:         "create",
		OrgScope:       "token_org_id",
		RateLimitClass: "billing_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "billing.portal.create",
		BodyLimitBytes: bodyLimitSmallJSON,
	}), createBillingPortal(billing, publicConfig.BillingReturnOrigins))
}

type SubmitExecutionInput struct {
	Body apiwire.SandboxSubmitRequest
}

type ImportRepoInput struct {
	Body apiwire.SandboxImportRepoRequest
}

type RepoIDPath struct {
	RepoID string `path:"repo_id" doc:"Repo UUID"`
}

type RepoOutput struct {
	Body apiwire.SandboxRepoRecord
}

type ListReposOutput struct {
	Body []apiwire.SandboxRepoRecord
}

type SubmitExecutionOutput struct {
	Body apiwire.SandboxSubmitExecutionResult
}

type ExecutionIDPath struct {
	ExecutionID string `path:"execution_id" doc:"Execution UUID"`
}

type GetExecutionOutput struct {
	Body apiwire.SandboxExecutionRecord
}

type GetExecutionLogsOutput struct {
	Body apiwire.SandboxExecutionLogs
}

type EmptyInput struct{}

type SchedulerProbeInput struct {
	Body SchedulerProbeRequest
}

type SchedulerProbeRequest struct {
	Message string `json:"message,omitempty" maxLength:"512" doc:"Optional probe marker for verification runs."`
}

type SchedulerProbeOutput struct {
	Body SchedulerProbeResponse
}

type SchedulerProbeResponse struct {
	JobID  string `json:"job_id" doc:"River job ID encoded as a decimal string for JavaScript-safe transport."`
	Kind   string `json:"kind"`
	Queue  string `json:"queue"`
	Status string `json:"status"`
}

type EntitlementsOutput struct {
	Body apiwire.BillingEntitlementsView
}

type ContractsOutput struct {
	Body apiwire.BillingContracts
}

type PlansOutput struct {
	Body apiwire.BillingPlans
}

type GrantsInput struct {
	ProductID string `query:"product_id,omitempty" doc:"Filter by product"`
	Active    bool   `query:"active,omitempty" doc:"Only active grants"`
}

type GrantsOutput struct {
	Body apiwire.BillingGrants
}

type StatementInput struct {
	ProductID string `query:"product_id" required:"true" minLength:"1" maxLength:"255" doc:"Product to preview"`
}

type StatementOutput struct {
	Body apiwire.BillingStatement
}

type CheckoutInput struct {
	Body apiwire.SandboxBillingCheckoutRequest
}

type URLOutput struct {
	Body apiwire.BillingURLResponse
}

type ContractInput struct {
	Body apiwire.SandboxBillingContractRequest
}

type ContractChangeInput struct {
	ContractID string `path:"contract_id" minLength:"1" maxLength:"255"`
	Body       apiwire.SandboxBillingContractChangeRequest
}

type ContractIDPath struct {
	ContractID string `path:"contract_id" minLength:"1" maxLength:"255"`
}

type CancelContractOutput struct {
	Body apiwire.BillingCancelContractResponse
}

type PortalInput struct {
	Body apiwire.SandboxBillingPortalRequest
}

func requireIdentity(ctx context.Context) (*auth.Identity, error) {
	identity := auth.FromContext(ctx)
	if identity == nil {
		return nil, unauthorized(ctx)
	}
	return identity, nil
}

func requireOrgID(ctx context.Context) (uint64, error) {
	identity, err := requireIdentity(ctx)
	if err != nil {
		return 0, err
	}
	orgID, err := apiwire.ParseUint64(identity.OrgID)
	if err != nil {
		return 0, badRequest(ctx, "invalid-token-org", "token org_id must be an unsigned integer", err)
	}
	return orgID, nil
}

func importRepo(svc *jobs.Service) func(context.Context, *ImportRepoInput) (*RepoOutput, error) {
	return func(ctx context.Context, input *ImportRepoInput) (*RepoOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		repo, err := svc.ImportRepo(ctx, orgID, importRepoRequest(input.Body))
		if err != nil {
			if errors.Is(err, jobs.ErrRepoScanCapacity) {
				return nil, tooManyRequests(ctx, "repo-scan-capacity-exceeded", "repo scan capacity exceeded")
			}
			return nil, internalFailure(ctx, "import-repo-failed", "import repo failed", err)
		}
		return &RepoOutput{Body: repoRecord(*repo)}, nil
	}
}

func listRepos(svc *jobs.Service) func(context.Context, *EmptyInput) (*ListReposOutput, error) {
	return func(ctx context.Context, _ *EmptyInput) (*ListReposOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		repos, err := svc.ListRepos(ctx, orgID)
		if err != nil {
			return nil, internalFailure(ctx, "list-repos-failed", "list repos failed", err)
		}
		return &ListReposOutput{Body: repoRecords(repos)}, nil
	}
}

func getRepo(svc *jobs.Service) func(context.Context, *RepoIDPath) (*RepoOutput, error) {
	return func(ctx context.Context, input *RepoIDPath) (*RepoOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		repoID, err := uuid.Parse(input.RepoID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-repo-id", "repo_id must be a UUID", err)
		}
		repo, err := svc.GetRepo(ctx, orgID, repoID)
		if err != nil {
			if errors.Is(err, jobs.ErrRepoMissing) {
				return nil, notFound(ctx, "repo-not-found", "repo not found")
			}
			return nil, internalFailure(ctx, "get-repo-failed", "get repo failed", err)
		}
		return &RepoOutput{Body: repoRecord(*repo)}, nil
	}
}

func rescanRepo(svc *jobs.Service) func(context.Context, *RepoIDPath) (*RepoOutput, error) {
	return func(ctx context.Context, input *RepoIDPath) (*RepoOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		repoID, err := uuid.Parse(input.RepoID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-repo-id", "repo_id must be a UUID", err)
		}
		repo, err := svc.RescanRepo(ctx, orgID, repoID)
		if err != nil {
			if errors.Is(err, jobs.ErrRepoMissing) {
				return nil, notFound(ctx, "repo-not-found", "repo not found")
			}
			if errors.Is(err, jobs.ErrRepoScanCapacity) {
				return nil, tooManyRequests(ctx, "repo-scan-capacity-exceeded", "repo scan capacity exceeded")
			}
			return nil, internalFailure(ctx, "rescan-repo-failed", "rescan repo failed", err)
		}
		return &RepoOutput{Body: repoRecord(*repo)}, nil
	}
}

func submitExecution(svc *jobs.Service) func(context.Context, *SubmitExecutionInput) (*SubmitExecutionOutput, error) {
	return func(ctx context.Context, input *SubmitExecutionInput) (*SubmitExecutionOutput, error) {
		identity, err := requireIdentity(ctx)
		if err != nil {
			return nil, err
		}

		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}

		executionID, attemptID, err := svc.Submit(ctx, orgID, identity.Subject, submitRequest(input.Body))
		if err != nil {
			switch {
			case errors.Is(err, jobs.ErrQuotaExceeded):
				return nil, tooManyRequests(ctx, "quota-exceeded", "quota exceeded")
			case errors.Is(err, jobs.ErrRepoNotReady):
				return nil, conflict(ctx, "repo-not-ready", "repo is not ready")
			case errors.Is(err, billingclient.ErrPaymentRequired):
				return nil, paymentRequired(ctx, "insufficient balance")
			default:
				return nil, internalFailure(ctx, "submit-execution-failed", "submit execution failed", err)
			}
		}

		out := &SubmitExecutionOutput{}
		out.Body = apiwire.SandboxSubmitExecutionResult{
			ExecutionID: executionID.String(),
			AttemptID:   attemptID.String(),
			Status:      jobs.StateQueued,
		}
		return out, nil
	}
}

func createSchedulerProbe(svc *jobs.Service) func(context.Context, *SchedulerProbeInput) (*SchedulerProbeOutput, error) {
	return func(ctx context.Context, input *SchedulerProbeInput) (*SchedulerProbeOutput, error) {
		identity, err := requireIdentity(ctx)
		if err != nil {
			return nil, err
		}
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		result, err := svc.EnqueueSchedulerProbe(ctx, jobs.SchedulerProbeRequest{
			Message: input.Body.Message,
			OrgID:   orgID,
			ActorID: identity.Subject,
		})
		if err != nil {
			return nil, internalFailure(ctx, "scheduler-probe-enqueue-failed", "scheduler probe enqueue failed", err)
		}
		return &SchedulerProbeOutput{Body: SchedulerProbeResponse{
			JobID:  strconv.FormatInt(result.JobID, 10),
			Kind:   result.Kind,
			Queue:  result.Queue,
			Status: result.Status,
		}}, nil
	}
}

func getExecution(svc *jobs.Service) func(context.Context, *ExecutionIDPath) (*GetExecutionOutput, error) {
	return func(ctx context.Context, input *ExecutionIDPath) (*GetExecutionOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		executionID, err := uuid.Parse(input.ExecutionID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-execution-id", "execution_id must be a UUID", err)
		}

		execution, err := svc.GetExecution(ctx, orgID, executionID)
		if err != nil {
			if errors.Is(err, jobs.ErrExecutionMissing) {
				return nil, notFound(ctx, "execution-not-found", "execution not found")
			}
			return nil, internalFailure(ctx, "get-execution-failed", "get execution failed", err)
		}

		out := &GetExecutionOutput{}
		out.Body = executionRecord(*execution)
		return out, nil
	}
}

func getExecutionLogs(svc *jobs.Service) func(context.Context, *ExecutionIDPath) (*GetExecutionLogsOutput, error) {
	return func(ctx context.Context, input *ExecutionIDPath) (*GetExecutionLogsOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		executionID, err := uuid.Parse(input.ExecutionID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-execution-id", "execution_id must be a UUID", err)
		}

		attemptID, logs, err := svc.GetExecutionLogs(ctx, orgID, executionID)
		if err != nil {
			if errors.Is(err, jobs.ErrExecutionMissing) {
				return nil, notFound(ctx, "execution-not-found", "execution not found")
			}
			return nil, internalFailure(ctx, "get-execution-logs-failed", "get execution logs failed", err)
		}

		out := &GetExecutionLogsOutput{}
		out.Body = apiwire.SandboxExecutionLogs{
			ExecutionID: executionID.String(),
			AttemptID:   attemptID.String(),
			Logs:        logs,
		}
		return out, nil
	}
}

func getBillingEntitlements(billing *billingclient.ServiceClient) func(context.Context, *EmptyInput) (*EntitlementsOutput, error) {
	return func(ctx context.Context, _ *EmptyInput) (*EntitlementsOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		view, err := billing.GetEntitlements(ctx, orgID)
		if err != nil {
			return nil, billingProxyError(ctx, err)
		}
		return &EntitlementsOutput{Body: view}, nil
	}
}

func listBillingContracts(billing *billingclient.ServiceClient) func(context.Context, *EmptyInput) (*ContractsOutput, error) {
	return func(ctx context.Context, _ *EmptyInput) (*ContractsOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		contracts, err := billing.ListContracts(ctx, orgID)
		if err != nil {
			return nil, billingProxyError(ctx, err)
		}
		return &ContractsOutput{Body: contracts}, nil
	}
}

func listBillingPlans(billing *billingclient.ServiceClient) func(context.Context, *EmptyInput) (*PlansOutput, error) {
	return func(ctx context.Context, _ *EmptyInput) (*PlansOutput, error) {
		if _, err := requireOrgID(ctx); err != nil {
			return nil, err
		}
		plans, err := billing.ListPlans(ctx, "sandbox")
		if err != nil {
			return nil, billingProxyError(ctx, err)
		}
		return &PlansOutput{Body: plans}, nil
	}
}

func getBillingStatement(billing *billingclient.ServiceClient) func(context.Context, *StatementInput) (*StatementOutput, error) {
	return func(ctx context.Context, input *StatementInput) (*StatementOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		statement, err := billing.GetStatement(ctx, orgID, input.ProductID)
		if err != nil {
			return nil, billingProxyError(ctx, err)
		}
		return &StatementOutput{Body: statement}, nil
	}
}

func createBillingCheckout(billing *billingclient.ServiceClient, billingReturnOrigins []string) func(context.Context, *CheckoutInput) (*URLOutput, error) {
	return func(ctx context.Context, input *CheckoutInput) (*URLOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		if err := validateBillingReturnURLs(ctx, billingReturnOrigins,
			billingReturnURLField{Name: "success_url", URL: input.Body.SuccessURL},
			billingReturnURLField{Name: "cancel_url", URL: input.Body.CancelURL},
		); err != nil {
			return nil, err
		}
		url, err := billing.CreateCheckout(ctx, orgID, input.Body.ProductID, input.Body.AmountCents, input.Body.SuccessURL, input.Body.CancelURL)
		if err != nil {
			return nil, billingProxyError(ctx, err)
		}
		out := &URLOutput{}
		out.Body = apiwire.BillingURLResponse{URL: url}
		return out, nil
	}
}

func createBillingContract(billing *billingclient.ServiceClient, billingReturnOrigins []string) func(context.Context, *ContractInput) (*URLOutput, error) {
	return func(ctx context.Context, input *ContractInput) (*URLOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		if err := validateBillingReturnURLs(ctx, billingReturnOrigins,
			billingReturnURLField{Name: "success_url", URL: input.Body.SuccessURL},
			billingReturnURLField{Name: "cancel_url", URL: input.Body.CancelURL},
		); err != nil {
			return nil, err
		}
		url, err := billing.CreateContract(ctx, orgID, input.Body.PlanID, input.Body.Cadence, input.Body.SuccessURL, input.Body.CancelURL)
		if err != nil {
			return nil, billingProxyError(ctx, err)
		}
		out := &URLOutput{}
		out.Body = apiwire.BillingURLResponse{URL: url}
		return out, nil
	}
}

func createBillingContractChange(billing *billingclient.ServiceClient, billingReturnOrigins []string) func(context.Context, *ContractChangeInput) (*URLOutput, error) {
	return func(ctx context.Context, input *ContractChangeInput) (*URLOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		if input.ContractID == "" {
			return nil, badRequest(ctx, "invalid-contract-id", "contract_id is required", nil)
		}
		if err := validateBillingReturnURLs(ctx, billingReturnOrigins,
			billingReturnURLField{Name: "success_url", URL: input.Body.SuccessURL},
			billingReturnURLField{Name: "cancel_url", URL: input.Body.CancelURL},
		); err != nil {
			return nil, err
		}
		result, err := billing.CreateContractChange(ctx, orgID, input.ContractID, input.Body.TargetPlanID, input.Body.SuccessURL, input.Body.CancelURL)
		if err != nil {
			return nil, billingProxyError(ctx, err)
		}
		out := &URLOutput{}
		out.Body = apiwire.BillingURLResponse{URL: result.URL}
		return out, nil
	}
}

func cancelBillingContract(billing *billingclient.ServiceClient) func(context.Context, *ContractIDPath) (*CancelContractOutput, error) {
	return func(ctx context.Context, input *ContractIDPath) (*CancelContractOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		if input.ContractID == "" {
			return nil, badRequest(ctx, "invalid-contract-id", "contract_id is required", nil)
		}
		contract, err := billing.CancelContract(ctx, orgID, input.ContractID)
		if err != nil {
			return nil, billingProxyError(ctx, err)
		}
		return &CancelContractOutput{Body: contract}, nil
	}
}

func createBillingPortal(billing *billingclient.ServiceClient, billingReturnOrigins []string) func(context.Context, *PortalInput) (*URLOutput, error) {
	return func(ctx context.Context, input *PortalInput) (*URLOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		if err := validateBillingReturnURLs(ctx, billingReturnOrigins,
			billingReturnURLField{Name: "return_url", URL: input.Body.ReturnURL},
		); err != nil {
			return nil, err
		}
		url, err := billing.CreatePortalSession(ctx, orgID, input.Body.ReturnURL)
		if err != nil {
			return nil, billingProxyError(ctx, err)
		}
		out := &URLOutput{}
		out.Body = apiwire.BillingURLResponse{URL: url}
		return out, nil
	}
}

func billingProxyError(ctx context.Context, err error) error {
	if errors.Is(err, billingclient.ErrNoStripeCustomer) {
		return unprocessableEntity(ctx, "billing-no-stripe-customer", "billing portal requires an existing Stripe customer", err)
	}
	if errors.Is(err, billingclient.ErrContractNotFound) {
		return notFound(ctx, "billing-contract-not-found", "billing contract not found")
	}
	return upstreamFailure(ctx, "billing-service-unavailable", "billing service unavailable", err)
}
