package api

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/verself/sandbox-rental-service/internal/internalcontractapi"
	"github.com/verself/sandbox-rental-service/internal/jobs"
	workloadauth "github.com/verself/service-runtime/workload"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var internalAPITracer = otel.Tracer("sandbox-rental-service/internal/api")

func RegisterInternalRoutes(api huma.API, svc *jobs.Service) {
	registerInternalSandboxContractRoute(api, internalcontractapi.InternalRegisterRunnerRepository, "Register a repository with the runner product", internalRegisterRunnerRepository(svc))
}

func internalRegisterRunnerRepository(svc *jobs.Service) func(context.Context, *internalcontractapi.InternalRegisterRunnerRepositoryInput) (*internalcontractapi.InternalRegisterRunnerRepositoryOutput, error) {
	return func(ctx context.Context, input *internalcontractapi.InternalRegisterRunnerRepositoryInput) (_ *internalcontractapi.InternalRegisterRunnerRepositoryOutput, err error) {
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
		orgID := strings.TrimSpace(string(req.OrgID))
		if orgID == "" {
			return nil, badRequest(ctx, "invalid-org-id", "org_id is required", nil)
		}
		providerRepoID, err := strconv.ParseInt(strings.TrimSpace(string(req.ProviderRepositoryID)), 10, 64)
		if err != nil || providerRepoID <= 0 {
			return nil, badRequest(ctx, "invalid-provider-repository-id", "provider_repository_id must be a positive decimal int64 string", err)
		}
		projectID, err := uuid.Parse(string(req.ProjectID))
		if err != nil {
			return nil, badRequest(ctx, "invalid-project-id", "project_id must be a UUID", err)
		}
		var sourceRepositoryID uuid.UUID
		if req.SourceRepositoryID != nil {
			sourceRepositoryID, err = uuid.Parse(string(*req.SourceRepositoryID))
			if err != nil {
				return nil, badRequest(ctx, "invalid-source-repository-id", "source_repository_id must be a UUID", err)
			}
		}
		registration := jobs.RunnerRepositoryRegistration{
			Provider:             strings.TrimSpace(string(req.Provider)),
			OrgID:                orgID,
			ProjectID:            projectID,
			SourceRepositoryID:   sourceRepositoryID,
			ProviderOwner:        strings.TrimSpace(string(req.ProviderOwner)),
			ProviderRepo:         strings.TrimSpace(string(req.ProviderRepo)),
			ProviderRepositoryID: providerRepoID,
			RepositoryFullName:   strings.TrimSpace(stringFromPtr(req.RepositoryFullName)),
		}
		span.SetAttributes(
			attribute.String("spiffe.peer_id", peerID.String()),
			attribute.String("verself.org_id", orgID),
			attribute.String("verself.project_id", projectID.String()),
			attribute.String("runner.provider", registration.Provider),
			attribute.Int64("runner.provider_repository_id", providerRepoID),
		)
		if err := svc.RegisterRunnerRepository(ctx, registration); err != nil {
			return nil, runnerRepositoryRegistrationError(ctx, err)
		}
		var sourceRepositoryIDOut *internalcontractapi.SourceRepositoryID
		if registration.SourceRepositoryID != uuid.Nil {
			value := internalcontractapi.SourceRepositoryID(registration.SourceRepositoryID.String())
			sourceRepositoryIDOut = &value
		}
		return &internalcontractapi.InternalRegisterRunnerRepositoryOutput{Body: internalcontractapi.InternalRegisterRunnerRepositoryOutputBody{
			Registration: internalcontractapi.RunnerRepositoryRegistration{
				Provider:             internalcontractapi.Provider(registration.Provider),
				ProviderRepositoryID: internalcontractapi.ProviderRepositoryID(strconv.FormatInt(providerRepoID, 10)),
				ProjectID:            internalcontractapi.ProjectID(registration.ProjectID.String()),
				SourceRepositoryID:   sourceRepositoryIDOut,
				State:                "registered",
			},
		}}, nil
	}
}

func runnerRepositoryRegistrationError(ctx context.Context, err error) error {
	switch {
	case strings.Contains(err.Error(), "unsupported runner provider"):
		return badRequest(ctx, "unsupported-runner-provider", "runner provider is not supported", err)
	case errors.Is(err, jobs.ErrForgejoRunnerNotConfigured):
		return serviceUnavailable(ctx, "forgejo-runner-not-configured", "forgejo runner is not configured", err)
	case errors.Is(err, jobs.ErrGitHubRunnerNotConfigured):
		return serviceUnavailable(ctx, "github-runner-not-configured", "github runner is not configured", err)
	case errors.Is(err, jobs.ErrGitHubInstallationInvalid):
		return badRequest(ctx, "github-installation-invalid", "github installation or repository is invalid", err)
	default:
		return internalFailure(ctx, "runner-repository-registration-failed", "register runner repository failed", err)
	}
}
