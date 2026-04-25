package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/forge-metal/apiwire"
	workloadauth "github.com/forge-metal/auth-middleware/workload"
	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var internalAPITracer = otel.Tracer("sandbox-rental-service/internal/api")

type InternalRegisterRunnerRepositoryInput struct {
	Body InternalRegisterRunnerRepositoryRequest
}

type InternalRegisterRunnerRepositoryRequest struct {
	Provider             string    `json:"provider" required:"true" enum:"forgejo"`
	OrgID                string    `json:"org_id" required:"true"`
	SourceRepositoryID   uuid.UUID `json:"source_repository_id,omitempty"`
	ProviderOwner        string    `json:"provider_owner" required:"true" minLength:"1" maxLength:"255"`
	ProviderRepo         string    `json:"provider_repo" required:"true" minLength:"1" maxLength:"255"`
	ProviderRepositoryID string    `json:"provider_repository_id" required:"true"`
	RepositoryFullName   string    `json:"repository_full_name,omitempty" maxLength:"512"`
}

type InternalRegisterRunnerRepositoryOutput struct {
	Body InternalRunnerRepositoryRegistration
}

type InternalRunnerRepositoryRegistration struct {
	Provider             string    `json:"provider"`
	ProviderRepositoryID string    `json:"provider_repository_id"`
	SourceRepositoryID   uuid.UUID `json:"source_repository_id,omitempty"`
	State                string    `json:"state"`
}

func RegisterInternalRoutes(api huma.API, svc *jobs.Service) {
	huma.Register(api, huma.Operation{
		OperationID:   "internal-register-runner-repository",
		Method:        http.MethodPost,
		Path:          "/internal/v1/runner/repositories",
		Summary:       "Register a repository with the runner product",
		DefaultStatus: http.StatusCreated,
		MaxBodyBytes:  bodyLimitSmallJSON,
		Security:      []map[string][]string{{"mutualTLS": {}}},
		Extensions: map[string]any{
			"x-forge-metal-iam": map[string]any{
				"permission":          "sandbox:runner_repository:register",
				"resource":            "runner_repository",
				"action":              "register",
				"org_scope":           "body_org_id",
				"rate_limit_class":    "internal_mutation",
				"audit_event":         "sandbox.runner_repository.register",
				"source_product_area": "Runner",
				"operation_display":   "register runner repository",
				"operation_type":      "write",
				"event_category":      "sandbox",
				"risk_level":          "high",
				"data_classification": "restricted",
			},
		},
	}, internalRegisterRunnerRepository(svc))
}

func internalRegisterRunnerRepository(svc *jobs.Service) func(context.Context, *InternalRegisterRunnerRepositoryInput) (*InternalRegisterRunnerRepositoryOutput, error) {
	return func(ctx context.Context, input *InternalRegisterRunnerRepositoryInput) (_ *InternalRegisterRunnerRepositoryOutput, err error) {
		ctx, span := internalAPITracer.Start(ctx, "sandbox-rental.runner_repository.register")
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
		providerRepoID, err := strconv.ParseInt(strings.TrimSpace(req.ProviderRepositoryID), 10, 64)
		if err != nil || providerRepoID <= 0 {
			return nil, badRequest(ctx, "invalid-provider-repository-id", "provider_repository_id must be a positive decimal int64 string", err)
		}
		registration := jobs.RunnerRepositoryRegistration{
			Provider:             strings.TrimSpace(req.Provider),
			OrgID:                orgID,
			SourceRepositoryID:   req.SourceRepositoryID,
			ProviderOwner:        strings.TrimSpace(req.ProviderOwner),
			ProviderRepo:         strings.TrimSpace(req.ProviderRepo),
			ProviderRepositoryID: providerRepoID,
			RepositoryFullName:   strings.TrimSpace(req.RepositoryFullName),
		}
		span.SetAttributes(
			attribute.String("spiffe.peer_id", peerID.String()),
			attribute.Int64("forge_metal.org_id", int64(orgID)),
			attribute.String("runner.provider", registration.Provider),
			attribute.Int64("runner.provider_repository_id", providerRepoID),
		)
		if err := svc.RegisterRunnerRepository(ctx, registration); err != nil {
			return nil, runnerRepositoryRegistrationError(ctx, err)
		}
		return &InternalRegisterRunnerRepositoryOutput{Body: InternalRunnerRepositoryRegistration{
			Provider:             registration.Provider,
			ProviderRepositoryID: strconv.FormatInt(providerRepoID, 10),
			SourceRepositoryID:   registration.SourceRepositoryID,
			State:                "registered",
		}}, nil
	}
}

func runnerRepositoryRegistrationError(ctx context.Context, err error) error {
	switch {
	case strings.Contains(err.Error(), "unsupported runner provider"):
		return badRequest(ctx, "unsupported-runner-provider", "runner provider is not supported", err)
	case errors.Is(err, jobs.ErrForgejoRunnerNotConfigured):
		return serviceUnavailable(ctx, "forgejo-runner-not-configured", "forgejo runner is not configured", err)
	default:
		return internalFailure(ctx, "runner-repository-registration-failed", "register runner repository failed", err)
	}
}
