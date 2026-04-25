package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/forge-metal/apiwire"
	workloadauth "github.com/forge-metal/auth-middleware/workload"
	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
)

var internalAPITracer = otel.Tracer("sandbox-rental-service/internal/api")

type InternalSubmitSourceCiRunInput struct {
	Body InternalSubmitSourceCiRunRequest
}

type InternalSubmitSourceCiRunRequest struct {
	OrgID          string    `json:"org_id" required:"true"`
	ActorID        string    `json:"actor_id" required:"true" minLength:"1" maxLength:"255"`
	RepoID         uuid.UUID `json:"repo_id" required:"true"`
	CIRunID        uuid.UUID `json:"ci_run_id" required:"true"`
	RefName        string    `json:"ref_name" required:"true" minLength:"1" maxLength:"255"`
	CommitSHA      string    `json:"commit_sha" required:"true" minLength:"1" maxLength:"255"`
	RunnerClass    string    `json:"runner_class,omitempty" maxLength:"255"`
	RunCommand     string    `json:"run_command,omitempty" maxLength:"4096"`
	IdempotencyKey string    `json:"idempotency_key" required:"true" minLength:"1" maxLength:"128"`
}

type InternalSubmitSourceCiRunOutput struct {
	Body InternalSourceCIRunSubmission
}

type InternalSourceCIRunSubmission struct {
	ExecutionID uuid.UUID `json:"execution_id"`
	AttemptID   uuid.UUID `json:"attempt_id"`
	State       string    `json:"state"`
}

func RegisterInternalRoutes(api huma.API, svc *jobs.Service) {
	huma.Register(api, huma.Operation{
		OperationID:   "internal-submit-source-ci-run",
		Method:        http.MethodPost,
		Path:          "/internal/v1/source-ci-runs",
		Summary:       "Submit a source-code-hosting CI run to the sandbox execution pipeline",
		DefaultStatus: http.StatusCreated,
		MaxBodyBytes:  bodyLimitSmallJSON,
		Security:      []map[string][]string{{"mutualTLS": {}}},
		Extensions: map[string]any{
			"x-forge-metal-iam": map[string]any{
				"permission":          "sandbox:source_ci:submit",
				"resource":            "source_ci_run",
				"action":              "submit",
				"org_scope":           "body_org_id",
				"rate_limit_class":    "internal_mutation",
				"audit_event":         "sandbox.source_ci.submit",
				"source_product_area": "Sandbox",
				"operation_display":   "submit source ci run",
				"operation_type":      "write",
				"event_category":      "sandbox",
				"risk_level":          "high",
				"data_classification": "restricted",
			},
		},
	}, internalSubmitSourceCIRun(svc))
}

func internalSubmitSourceCIRun(svc *jobs.Service) func(context.Context, *InternalSubmitSourceCiRunInput) (*InternalSubmitSourceCiRunOutput, error) {
	return func(ctx context.Context, input *InternalSubmitSourceCiRunInput) (_ *InternalSubmitSourceCiRunOutput, err error) {
		ctx, span := internalAPITracer.Start(ctx, "sandbox-rental.source_ci.submit")
		defer func() {
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
			span.End()
		}()
		peerID, ok := workloadauth.PeerIDFromContext(ctx)
		if !ok {
			return nil, unauthorized(ctx)
		}
		if svc == nil {
			return nil, serviceUnavailable(ctx, "sandbox-service-unavailable", "sandbox job service is unavailable", jobs.ErrRunnerUnavailable)
		}
		req := input.Body
		orgID, err := apiwire.ParseUint64(strings.TrimSpace(req.OrgID))
		if err != nil || orgID == 0 {
			return nil, badRequest(ctx, "invalid-org-id", "org_id must be a positive decimal uint64 string", err)
		}
		actorID := strings.TrimSpace(req.ActorID)
		if actorID == "" {
			return nil, badRequest(ctx, "actor-id-required", "actor_id is required", nil)
		}
		if req.RepoID == uuid.Nil {
			return nil, badRequest(ctx, "repo-id-required", "repo_id is required", nil)
		}
		if req.CIRunID == uuid.Nil {
			return nil, badRequest(ctx, "ci-run-id-required", "ci_run_id is required", nil)
		}
		if strings.TrimSpace(req.RefName) == "" {
			return nil, badRequest(ctx, "ref-name-required", "ref_name is required", nil)
		}
		if strings.TrimSpace(req.CommitSHA) == "" {
			return nil, badRequest(ctx, "commit-sha-required", "commit_sha is required", nil)
		}
		if err := validateIdempotencyValue(ctx, "idempotency_key", req.IdempotencyKey); err != nil {
			return nil, err
		}
		span.SetAttributes(
			attribute.String("spiffe.peer_id", peerID.String()),
			attribute.Int64("forge_metal.org_id", int64(orgID)),
			attribute.String("forge_metal.subject_id", actorID),
			attribute.String("source.repo_id", req.RepoID.String()),
			attribute.String("source.ci_run_id", req.CIRunID.String()),
			attribute.String("source.ref_name", strings.TrimSpace(req.RefName)),
		)

		executionID, attemptID, err := svc.Submit(ctx, orgID, actorID, jobs.SubmitRequest{
			Kind:             jobs.KindDirect,
			RunnerClass:      strings.TrimSpace(req.RunnerClass),
			IdempotencyKey:   strings.TrimSpace(req.IdempotencyKey),
			SourceKind:       jobs.SourceKindSourceHosting,
			WorkloadKind:     jobs.WorkloadKindDirect,
			SourceRef:        sourceCIRunRef(req),
			ExternalProvider: "source-code-hosting-service",
			ExternalTaskID:   req.CIRunID.String(),
			RunCommand:       strings.TrimSpace(req.RunCommand),
		})
		if err != nil {
			return nil, sandboxSubmitError(ctx, err)
		}
		span.SetAttributes(
			attribute.String("execution.id", executionID.String()),
			attribute.String("attempt.id", attemptID.String()),
		)
		return &InternalSubmitSourceCiRunOutput{Body: InternalSourceCIRunSubmission{
			ExecutionID: executionID,
			AttemptID:   attemptID,
			State:       jobs.StateQueued,
		}}, nil
	}
}

func sourceCIRunRef(req InternalSubmitSourceCiRunRequest) string {
	return fmt.Sprintf("source-code-hosting://repos/%s/refs/%s@%s", req.RepoID, strings.TrimSpace(req.RefName), strings.TrimSpace(req.CommitSHA))
}

func sandboxSubmitError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, jobs.ErrRunnerUnavailable):
		return serviceUnavailable(ctx, "runner-unavailable", "sandbox runner is unavailable", err)
	case errors.Is(err, jobs.ErrRunnerClassMissing):
		return badRequest(ctx, "runner-class-missing", "runner_class is not configured", err)
	case errors.Is(err, jobs.ErrQuotaExceeded):
		return paymentRequired(ctx, "sandbox quota is exhausted")
	case errors.Is(err, jobs.ErrBillingPaymentRequired):
		return paymentRequired(ctx, "billing payment is required")
	case errors.Is(err, jobs.ErrBillingForbidden):
		return forbidden(ctx, "billing-forbidden", "billing account is not allowed to submit executions")
	default:
		return internalFailure(ctx, "source-ci-submit-failed", "submit source ci run failed", err)
	}
}
