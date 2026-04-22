// Package api registers sandbox-rental-service HTTP routes on a Huma API.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/forge-metal/apiwire"
	auth "github.com/forge-metal/auth-middleware"
	billingclient "github.com/forge-metal/billing-service/client"

	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
	"github.com/forge-metal/sandbox-rental-service/internal/recurring"
)

const billingNoStripeCustomerProblemType = "urn:forge-metal:problem:billing:no-stripe-customer"

// RegisterRoutes wires all sandbox-rental-service endpoints onto the Huma API.
func RegisterRoutes(api huma.API, svc *jobs.Service, recurringSvc *recurring.Service, billing *billingclient.ClientWithResponses, publicConfig PublicAPIConfig) {
	registerSecured(api, secured(huma.Operation{
		OperationID:   "begin-github-installation",
		Method:        http.MethodPost,
		Path:          "/api/v1/github/installations/connect",
		Summary:       "Start GitHub App installation for the current org",
		DefaultStatus: 201,
	}, operationPolicy{
		Permission:     permissionGitHubWrite,
		Resource:       "github_installation",
		Action:         "connect",
		OrgScope:       "token_org_id",
		RateLimitClass: "github_installation_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "sandbox.github_installation.connect",
		BodyLimitBytes: bodyLimitNoBody,
	}), beginGitHubInstallation(svc))

	registerSecured(api, secured(huma.Operation{
		OperationID: "list-github-installations",
		Method:      http.MethodGet,
		Path:        "/api/v1/github/installations",
		Summary:     "List GitHub App installations for the current org",
	}, operationPolicy{
		Permission:     permissionGitHubRead,
		Resource:       "github_installation",
		Action:         "list",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "sandbox.github_installation.list",
	}), listGitHubInstallations(svc))

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
		OperationID:   "create-execution-schedule",
		Method:        http.MethodPost,
		Path:          "/api/v1/execution-schedules",
		Summary:       "Create a recurring execution schedule",
		DefaultStatus: 201,
	}, operationPolicy{
		Permission:     permissionScheduleWrite,
		Resource:       "execution_schedule",
		Action:         "create",
		OrgScope:       "token_org_id",
		RateLimitClass: "execution_schedule_mutation",
		Idempotency:    idempotencyRequestBodyKey,
		AuditEvent:     "sandbox.execution_schedule.create",
		BodyLimitBytes: bodyLimitSmallJSON,
	}), createExecutionSchedule(recurringSvc))

	registerSecured(api, secured(huma.Operation{
		OperationID: "list-execution-schedules",
		Method:      http.MethodGet,
		Path:        "/api/v1/execution-schedules",
		Summary:     "List recurring execution schedules",
	}, operationPolicy{
		Permission:     permissionScheduleRead,
		Resource:       "execution_schedule",
		Action:         "list",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "sandbox.execution_schedule.list",
	}), listExecutionSchedules(recurringSvc))

	registerSecured(api, secured(huma.Operation{
		OperationID: "get-execution-schedule",
		Method:      http.MethodGet,
		Path:        "/api/v1/execution-schedules/{schedule_id}",
		Summary:     "Get a recurring execution schedule",
	}, operationPolicy{
		Permission:     permissionScheduleRead,
		Resource:       "execution_schedule",
		Action:         "read",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "sandbox.execution_schedule.read",
	}), getExecutionSchedule(recurringSvc))

	registerSecured(api, secured(huma.Operation{
		OperationID:   "pause-execution-schedule",
		Method:        http.MethodPost,
		Path:          "/api/v1/execution-schedules/{schedule_id}/pause",
		Summary:       "Pause a recurring execution schedule",
		DefaultStatus: 200,
	}, operationPolicy{
		Permission:     permissionScheduleWrite,
		Resource:       "execution_schedule",
		Action:         "pause",
		OrgScope:       "token_org_id",
		RateLimitClass: "execution_schedule_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "sandbox.execution_schedule.pause",
		BodyLimitBytes: bodyLimitNoBody,
	}), pauseExecutionSchedule(recurringSvc))

	registerSecured(api, secured(huma.Operation{
		OperationID:   "resume-execution-schedule",
		Method:        http.MethodPost,
		Path:          "/api/v1/execution-schedules/{schedule_id}/resume",
		Summary:       "Resume a recurring execution schedule",
		DefaultStatus: 200,
	}, operationPolicy{
		Permission:     permissionScheduleWrite,
		Resource:       "execution_schedule",
		Action:         "resume",
		OrgScope:       "token_org_id",
		RateLimitClass: "execution_schedule_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "sandbox.execution_schedule.resume",
		BodyLimitBytes: bodyLimitNoBody,
	}), resumeExecutionSchedule(recurringSvc))

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

	registerSecured(api, secured(huma.Operation{
		OperationID:   "create-volume",
		Method:        http.MethodPost,
		Path:          "/api/v1/volumes",
		Summary:       "Create a durable volume",
		DefaultStatus: 201,
	}, operationPolicy{
		Permission:     permissionVolumeWrite,
		Resource:       "volume",
		Action:         "create",
		OrgScope:       "token_org_id",
		RateLimitClass: "volume_mutation",
		Idempotency:    idempotencyRequestBodyKey,
		AuditEvent:     "sandbox.volume.create",
		BodyLimitBytes: bodyLimitSmallJSON,
	}), createVolume(svc))

	registerSecured(api, secured(huma.Operation{
		OperationID: "list-volumes",
		Method:      http.MethodGet,
		Path:        "/api/v1/volumes",
		Summary:     "List durable volumes",
	}, operationPolicy{
		Permission:     permissionVolumeRead,
		Resource:       "volume",
		Action:         "list",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "sandbox.volume.list",
	}), listVolumes(svc))

	registerSecured(api, secured(huma.Operation{
		OperationID: "get-volume",
		Method:      http.MethodGet,
		Path:        "/api/v1/volumes/{volume_id}",
		Summary:     "Get durable volume current state",
	}, operationPolicy{
		Permission:     permissionVolumeRead,
		Resource:       "volume",
		Action:         "read",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "sandbox.volume.read",
	}), getVolume(svc))

	registerSecured(api, secured(huma.Operation{
		OperationID:   "create-volume-meter-tick",
		Method:        http.MethodPost,
		Path:          "/api/v1/volumes/{volume_id}/meter-ticks",
		Summary:       "Queue a durable volume meter tick",
		DefaultStatus: 202,
		Hidden:        true,
	}, operationPolicy{
		Permission:     permissionVolumeWrite,
		Resource:       "volume_meter_tick",
		Action:         "create",
		OrgScope:       "token_org_id",
		RateLimitClass: "volume_mutation",
		Idempotency:    idempotencyRequestBodyKey,
		AuditEvent:     "sandbox.volume.meter_tick.create",
		BodyLimitBytes: bodyLimitSmallJSON,
	}), createVolumeMeterTick(svc))

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

type SubmitExecutionOutput struct {
	Body apiwire.SandboxSubmitExecutionResult
}

type GitHubInstallationConnectOutput struct {
	Body apiwire.SandboxGitHubInstallationConnectResponse
}

type ListGitHubInstallationsOutput struct {
	Body []apiwire.SandboxGitHubInstallationRecord
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

type ExecutionScheduleIDPath struct {
	ScheduleID string `path:"schedule_id" doc:"Execution schedule UUID"`
}

type CreateExecutionScheduleInput struct {
	Body apiwire.SandboxExecutionScheduleCreateRequest
}

type ExecutionScheduleOutput struct {
	Body apiwire.SandboxExecutionScheduleRecord
}

type ListExecutionSchedulesOutput struct {
	Body []apiwire.SandboxExecutionScheduleRecord
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

type CreateVolumeInput struct {
	Body apiwire.SandboxVolumeCreateRequest
}

type VolumeIDPath struct {
	VolumeID string `path:"volume_id" doc:"Volume UUID"`
}

type VolumeOutput struct {
	Body apiwire.SandboxVolumeRecord
}

type ListVolumesOutput struct {
	Body []apiwire.SandboxVolumeRecord
}

type VolumeMeterTickInput struct {
	VolumeID string `path:"volume_id" doc:"Volume UUID"`
	Body     apiwire.SandboxVolumeMeterTickRequest
}

type VolumeMeterTickOutput struct {
	Body apiwire.SandboxVolumeMeterTickResult
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
			var vmErr *apiwire.ValidationError
			switch {
			case errors.As(err, &vmErr):
				return nil, badRequest(ctx, "vmresources-"+vmErr.Field+"-out-of-bounds", vmErr.Reason, err)
			case errors.Is(err, jobs.ErrQuotaExceeded):
				return nil, tooManyRequests(ctx, "quota-exceeded", "quota exceeded")
			case errors.Is(err, jobs.ErrRunnerClassMissing):
				return nil, badRequest(ctx, "runner-class-unavailable", "runner class is not available", err)
			case errors.Is(err, jobs.ErrInvalidSecretInjection):
				return nil, badRequest(ctx, "invalid-secret-injection", err.Error(), err)
			case errors.Is(err, jobs.ErrBillingPaymentRequired):
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

func beginGitHubInstallation(svc *jobs.Service) func(context.Context, *EmptyInput) (*GitHubInstallationConnectOutput, error) {
	return func(ctx context.Context, _ *EmptyInput) (*GitHubInstallationConnectOutput, error) {
		identity, err := requireIdentity(ctx)
		if err != nil {
			return nil, err
		}
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		if svc.GitHubRunner == nil || !svc.GitHubRunner.Configured() {
			return nil, serviceUnavailable(ctx, "github-runner-not-configured", "github runner is not configured", jobs.ErrGitHubRunnerNotConfigured)
		}
		connect, err := svc.GitHubRunner.BeginInstallation(ctx, orgID, identity.Subject)
		if err != nil {
			switch {
			case errors.Is(err, jobs.ErrGitHubRunnerNotConfigured):
				return nil, serviceUnavailable(ctx, "github-runner-not-configured", "github runner is not configured", err)
			case errors.Is(err, jobs.ErrGitHubInstallationInvalid):
				return nil, badRequest(ctx, "github-installation-invalid", "github installation must be an active organization installation", err)
			case errors.Is(err, jobs.ErrGitHubInstallationStateInvalid):
				return nil, badRequest(ctx, "github-installation-state-invalid", "github installation state is invalid", err)
			default:
				return nil, internalFailure(ctx, "github-installation-connect-failed", "start github installation failed", err)
			}
		}
		return &GitHubInstallationConnectOutput{Body: githubInstallationConnect(connect)}, nil
	}
}

func listGitHubInstallations(svc *jobs.Service) func(context.Context, *EmptyInput) (*ListGitHubInstallationsOutput, error) {
	return func(ctx context.Context, _ *EmptyInput) (*ListGitHubInstallationsOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		records, err := svc.ListGitHubInstallations(ctx, orgID)
		if err != nil {
			return nil, internalFailure(ctx, "github-installation-list-failed", "list github installations failed", err)
		}
		return &ListGitHubInstallationsOutput{Body: githubInstallationRecords(records)}, nil
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

func createVolume(svc *jobs.Service) func(context.Context, *CreateVolumeInput) (*VolumeOutput, error) {
	return func(ctx context.Context, input *CreateVolumeInput) (*VolumeOutput, error) {
		identity, err := requireIdentity(ctx)
		if err != nil {
			return nil, err
		}
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		record, err := svc.CreateVolume(ctx, orgID, identity.Subject, volumeCreateRequest(input.Body))
		if err != nil {
			return nil, internalFailure(ctx, "create-volume-failed", "create volume failed", err)
		}
		return &VolumeOutput{Body: volumeRecord(record)}, nil
	}
}

func listVolumes(svc *jobs.Service) func(context.Context, *EmptyInput) (*ListVolumesOutput, error) {
	return func(ctx context.Context, _ *EmptyInput) (*ListVolumesOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		records, err := svc.ListVolumes(ctx, orgID)
		if err != nil {
			return nil, internalFailure(ctx, "list-volumes-failed", "list volumes failed", err)
		}
		return &ListVolumesOutput{Body: volumeRecords(records)}, nil
	}
}

func getVolume(svc *jobs.Service) func(context.Context, *VolumeIDPath) (*VolumeOutput, error) {
	return func(ctx context.Context, input *VolumeIDPath) (*VolumeOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		volumeID, err := uuid.Parse(input.VolumeID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-volume-id", "volume_id must be a UUID", err)
		}
		record, err := svc.GetVolume(ctx, orgID, volumeID)
		if err != nil {
			if errors.Is(err, jobs.ErrVolumeMissing) {
				return nil, notFound(ctx, "volume-not-found", "volume not found")
			}
			return nil, internalFailure(ctx, "get-volume-failed", "get volume failed", err)
		}
		return &VolumeOutput{Body: volumeRecord(record)}, nil
	}
}

func createVolumeMeterTick(svc *jobs.Service) func(context.Context, *VolumeMeterTickInput) (*VolumeMeterTickOutput, error) {
	return func(ctx context.Context, input *VolumeMeterTickInput) (*VolumeMeterTickOutput, error) {
		identity, err := requireIdentity(ctx)
		if err != nil {
			return nil, err
		}
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		volumeID, err := uuid.Parse(input.VolumeID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-volume-id", "volume_id must be a UUID", err)
		}
		tick, job, err := svc.EnqueueVolumeMeterTick(ctx, orgID, identity.Subject, volumeID, volumeMeterTickRequest(input.Body))
		if err != nil {
			if errors.Is(err, jobs.ErrVolumeMissing) {
				return nil, notFound(ctx, "volume-not-found", "volume not found")
			}
			return nil, internalFailure(ctx, "create-volume-meter-tick-failed", "create volume meter tick failed", err)
		}
		return &VolumeMeterTickOutput{Body: apiwire.SandboxVolumeMeterTickResult{
			MeterTick: volumeMeterTickRecord(tick),
			JobID:     strconv.FormatInt(job.JobID, 10),
			Kind:      job.Kind,
			Queue:     job.Queue,
			Status:    job.Status,
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

func createExecutionSchedule(recurringSvc *recurring.Service) func(context.Context, *CreateExecutionScheduleInput) (*ExecutionScheduleOutput, error) {
	return func(ctx context.Context, input *CreateExecutionScheduleInput) (*ExecutionScheduleOutput, error) {
		identity, err := requireIdentity(ctx)
		if err != nil {
			return nil, err
		}
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		record, err := recurringSvc.CreateSchedule(ctx, orgID, identity.Subject, executionScheduleCreateRequest(input.Body))
		if err != nil {
			return nil, internalFailure(ctx, "create-execution-schedule-failed", "create execution schedule failed", err)
		}
		return &ExecutionScheduleOutput{Body: executionScheduleRecord(record)}, nil
	}
}

func listExecutionSchedules(recurringSvc *recurring.Service) func(context.Context, *EmptyInput) (*ListExecutionSchedulesOutput, error) {
	return func(ctx context.Context, _ *EmptyInput) (*ListExecutionSchedulesOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		records, err := recurringSvc.ListSchedules(ctx, orgID)
		if err != nil {
			return nil, internalFailure(ctx, "list-execution-schedules-failed", "list execution schedules failed", err)
		}
		out := make([]apiwire.SandboxExecutionScheduleRecord, 0, len(records))
		for _, record := range records {
			out = append(out, executionScheduleRecord(record))
		}
		return &ListExecutionSchedulesOutput{Body: out}, nil
	}
}

func getExecutionSchedule(recurringSvc *recurring.Service) func(context.Context, *ExecutionScheduleIDPath) (*ExecutionScheduleOutput, error) {
	return func(ctx context.Context, input *ExecutionScheduleIDPath) (*ExecutionScheduleOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		scheduleID, err := uuid.Parse(input.ScheduleID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-schedule-id", "schedule_id must be a UUID", err)
		}
		record, err := recurringSvc.GetSchedule(ctx, orgID, scheduleID)
		if err != nil {
			if errors.Is(err, recurring.ErrScheduleMissing) {
				return nil, notFound(ctx, "execution-schedule-not-found", "execution schedule not found")
			}
			return nil, internalFailure(ctx, "get-execution-schedule-failed", "get execution schedule failed", err)
		}
		return &ExecutionScheduleOutput{Body: executionScheduleRecord(*record)}, nil
	}
}

func pauseExecutionSchedule(recurringSvc *recurring.Service) func(context.Context, *ExecutionScheduleIDPath) (*ExecutionScheduleOutput, error) {
	return func(ctx context.Context, input *ExecutionScheduleIDPath) (*ExecutionScheduleOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		scheduleID, err := uuid.Parse(input.ScheduleID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-schedule-id", "schedule_id must be a UUID", err)
		}
		record, err := recurringSvc.PauseSchedule(ctx, orgID, scheduleID)
		if err != nil {
			if errors.Is(err, recurring.ErrScheduleMissing) {
				return nil, notFound(ctx, "execution-schedule-not-found", "execution schedule not found")
			}
			return nil, internalFailure(ctx, "pause-execution-schedule-failed", "pause execution schedule failed", err)
		}
		return &ExecutionScheduleOutput{Body: executionScheduleRecord(*record)}, nil
	}
}

func resumeExecutionSchedule(recurringSvc *recurring.Service) func(context.Context, *ExecutionScheduleIDPath) (*ExecutionScheduleOutput, error) {
	return func(ctx context.Context, input *ExecutionScheduleIDPath) (*ExecutionScheduleOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		scheduleID, err := uuid.Parse(input.ScheduleID)
		if err != nil {
			return nil, badRequest(ctx, "invalid-schedule-id", "schedule_id must be a UUID", err)
		}
		record, err := recurringSvc.ResumeSchedule(ctx, orgID, scheduleID)
		if err != nil {
			if errors.Is(err, recurring.ErrScheduleMissing) {
				return nil, notFound(ctx, "execution-schedule-not-found", "execution schedule not found")
			}
			return nil, internalFailure(ctx, "resume-execution-schedule-failed", "resume execution schedule failed", err)
		}
		return &ExecutionScheduleOutput{Body: executionScheduleRecord(*record)}, nil
	}
}

func getBillingEntitlements(billing *billingclient.ClientWithResponses) func(context.Context, *EmptyInput) (*EntitlementsOutput, error) {
	return func(ctx context.Context, _ *EmptyInput) (*EntitlementsOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		resp, err := billing.GetEntitlementsWithResponse(ctx, strconv.FormatUint(orgID, 10))
		if err != nil {
			return nil, billingProxyError(ctx, err)
		}
		if resp.StatusCode() != http.StatusOK {
			return nil, billingProxyResponseError(ctx, resp.StatusCode(), resp.Body)
		}
		view, err := decodeBillingProxyResponse[apiwire.BillingEntitlementsView](ctx, "decode billing entitlements", resp.Body)
		if err != nil {
			return nil, err
		}
		return &EntitlementsOutput{Body: view}, nil
	}
}

func listBillingContracts(billing *billingclient.ClientWithResponses) func(context.Context, *EmptyInput) (*ContractsOutput, error) {
	return func(ctx context.Context, _ *EmptyInput) (*ContractsOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		resp, err := billing.ListContractsWithResponse(ctx, strconv.FormatUint(orgID, 10))
		if err != nil {
			return nil, billingProxyError(ctx, err)
		}
		if resp.StatusCode() != http.StatusOK {
			return nil, billingProxyResponseError(ctx, resp.StatusCode(), resp.Body)
		}
		contracts, err := decodeBillingProxyResponse[apiwire.BillingContracts](ctx, "decode billing contracts", resp.Body)
		if err != nil {
			return nil, err
		}
		return &ContractsOutput{Body: contracts}, nil
	}
}

func listBillingPlans(billing *billingclient.ClientWithResponses) func(context.Context, *EmptyInput) (*PlansOutput, error) {
	return func(ctx context.Context, _ *EmptyInput) (*PlansOutput, error) {
		if _, err := requireOrgID(ctx); err != nil {
			return nil, err
		}
		resp, err := billing.ListPlansWithResponse(ctx, "sandbox")
		if err != nil {
			return nil, billingProxyError(ctx, err)
		}
		if resp.StatusCode() != http.StatusOK {
			return nil, billingProxyResponseError(ctx, resp.StatusCode(), resp.Body)
		}
		plans, err := decodeBillingProxyResponse[apiwire.BillingPlans](ctx, "decode billing plans", resp.Body)
		if err != nil {
			return nil, err
		}
		return &PlansOutput{Body: plans}, nil
	}
}

func getBillingStatement(billing *billingclient.ClientWithResponses) func(context.Context, *StatementInput) (*StatementOutput, error) {
	return func(ctx context.Context, input *StatementInput) (*StatementOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		resp, err := billing.GetStatementWithResponse(ctx, strconv.FormatUint(orgID, 10), &billingclient.GetStatementParams{ProductId: input.ProductID})
		if err != nil {
			return nil, billingProxyError(ctx, err)
		}
		if resp.StatusCode() != http.StatusOK {
			return nil, billingProxyResponseError(ctx, resp.StatusCode(), resp.Body)
		}
		statement, err := decodeBillingProxyResponse[apiwire.BillingStatement](ctx, "decode billing statement", resp.Body)
		if err != nil {
			return nil, err
		}
		return &StatementOutput{Body: statement}, nil
	}
}

func createBillingCheckout(billing *billingclient.ClientWithResponses, billingReturnOrigins []string) func(context.Context, *CheckoutInput) (*URLOutput, error) {
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
		resp, err := billing.CreateCheckoutWithResponse(ctx, billingclient.BillingCreateCheckoutRequest{
			AmountCents: input.Body.AmountCents,
			CancelUrl:   input.Body.CancelURL,
			OrgId:       strconv.FormatUint(orgID, 10),
			ProductId:   input.Body.ProductID,
			SuccessUrl:  input.Body.SuccessURL,
		})
		if err != nil {
			return nil, billingProxyError(ctx, err)
		}
		if resp.StatusCode() != http.StatusOK {
			return nil, billingProxyResponseError(ctx, resp.StatusCode(), resp.Body)
		}
		url, err := decodeBillingProxyResponse[apiwire.BillingURLResponse](ctx, "decode billing checkout response", resp.Body)
		if err != nil {
			return nil, err
		}
		out := &URLOutput{}
		out.Body = url
		return out, nil
	}
}

func createBillingContract(billing *billingclient.ClientWithResponses, billingReturnOrigins []string) func(context.Context, *ContractInput) (*URLOutput, error) {
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
		req := billingclient.BillingCreateContractRequest{
			CancelUrl:  input.Body.CancelURL,
			OrgId:      strconv.FormatUint(orgID, 10),
			PlanId:     input.Body.PlanID,
			SuccessUrl: input.Body.SuccessURL,
		}
		if input.Body.Cadence != "" {
			cadence := billingclient.BillingCreateContractRequestCadence(input.Body.Cadence)
			req.Cadence = &cadence
		}
		resp, err := billing.CreateContractWithResponse(ctx, req)
		if err != nil {
			return nil, billingProxyError(ctx, err)
		}
		if resp.StatusCode() != http.StatusOK {
			return nil, billingProxyResponseError(ctx, resp.StatusCode(), resp.Body)
		}
		url, err := decodeBillingProxyResponse[apiwire.BillingURLResponse](ctx, "decode billing contract response", resp.Body)
		if err != nil {
			return nil, err
		}
		out := &URLOutput{}
		out.Body = url
		return out, nil
	}
}

func createBillingContractChange(billing *billingclient.ClientWithResponses, billingReturnOrigins []string) func(context.Context, *ContractChangeInput) (*URLOutput, error) {
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
		resp, err := billing.CreateContractChangeWithResponse(ctx, input.ContractID, billingclient.BillingCreateContractChangeRequest{
			CancelUrl:    input.Body.CancelURL,
			OrgId:        strconv.FormatUint(orgID, 10),
			SuccessUrl:   input.Body.SuccessURL,
			TargetPlanId: input.Body.TargetPlanID,
		})
		if err != nil {
			return nil, billingProxyError(ctx, err)
		}
		if resp.StatusCode() != http.StatusOK {
			return nil, billingContractProxyResponseError(ctx, resp.StatusCode(), resp.Body)
		}
		result, err := decodeBillingProxyResponse[apiwire.BillingContractChangeResponse](ctx, "decode billing contract change response", resp.Body)
		if err != nil {
			return nil, err
		}
		out := &URLOutput{}
		out.Body = apiwire.BillingURLResponse{URL: result.URL}
		return out, nil
	}
}

func cancelBillingContract(billing *billingclient.ClientWithResponses) func(context.Context, *ContractIDPath) (*CancelContractOutput, error) {
	return func(ctx context.Context, input *ContractIDPath) (*CancelContractOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		if input.ContractID == "" {
			return nil, badRequest(ctx, "invalid-contract-id", "contract_id is required", nil)
		}
		resp, err := billing.CancelContractWithResponse(ctx, input.ContractID, billingclient.BillingCancelContractRequest{
			OrgId: strconv.FormatUint(orgID, 10),
		})
		if err != nil {
			return nil, billingProxyError(ctx, err)
		}
		if resp.StatusCode() != http.StatusOK {
			return nil, billingContractProxyResponseError(ctx, resp.StatusCode(), resp.Body)
		}
		contract, err := decodeBillingProxyResponse[apiwire.BillingCancelContractResponse](ctx, "decode billing cancel response", resp.Body)
		if err != nil {
			return nil, err
		}
		return &CancelContractOutput{Body: contract}, nil
	}
}

func createBillingPortal(billing *billingclient.ClientWithResponses, billingReturnOrigins []string) func(context.Context, *PortalInput) (*URLOutput, error) {
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
		resp, err := billing.CreatePortalWithResponse(ctx, billingclient.BillingCreatePortalSessionRequest{
			OrgId:     strconv.FormatUint(orgID, 10),
			ReturnUrl: input.Body.ReturnURL,
		})
		if err != nil {
			return nil, billingProxyError(ctx, err)
		}
		if resp.StatusCode() != http.StatusOK {
			return nil, billingPortalProxyResponseError(ctx, resp.StatusCode(), resp.Body)
		}
		url, err := decodeBillingProxyResponse[apiwire.BillingURLResponse](ctx, "decode billing portal response", resp.Body)
		if err != nil {
			return nil, err
		}
		out := &URLOutput{}
		out.Body = url
		return out, nil
	}
}

func billingProxyError(ctx context.Context, err error) error {
	return upstreamFailure(ctx, "billing-service-unavailable", "billing service unavailable", err)
}

func billingProxyResponseError(ctx context.Context, statusCode int, body []byte) error {
	return upstreamFailure(ctx, "billing-service-unavailable", "billing service unavailable", billingUnexpectedStatusError(statusCode, body))
}

func billingContractProxyResponseError(ctx context.Context, statusCode int, body []byte) error {
	if statusCode == http.StatusNotFound {
		return notFound(ctx, "billing-contract-not-found", "billing contract not found")
	}
	return billingProxyResponseError(ctx, statusCode, body)
}

func billingPortalProxyResponseError(ctx context.Context, statusCode int, body []byte) error {
	if statusCode == http.StatusUnprocessableEntity {
		problem := decodeBillingProblem(body)
		if problem != nil && problem.Type != nil && *problem.Type == billingNoStripeCustomerProblemType {
			return unprocessableEntity(ctx, "billing-no-stripe-customer", "billing portal requires an existing Stripe customer", nil)
		}
	}
	return billingProxyResponseError(ctx, statusCode, body)
}

func decodeBillingProxyResponse[T any](ctx context.Context, op string, body []byte) (T, error) {
	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		return out, upstreamFailure(ctx, "billing-service-unavailable", "billing service unavailable", fmt.Errorf("%s: %w", op, err))
	}
	return out, nil
}

func decodeBillingProblem(body []byte) *billingclient.ErrorModel {
	if len(body) == 0 {
		return nil
	}
	var problem billingclient.ErrorModel
	if err := json.Unmarshal(body, &problem); err != nil {
		return nil
	}
	return &problem
}

func billingUnexpectedStatusError(statusCode int, body []byte) error {
	problem := decodeBillingProblem(body)
	if problem != nil && problem.Detail != nil && *problem.Detail != "" {
		return errors.New(*problem.Detail)
	}
	return errors.New(http.StatusText(statusCode))
}
