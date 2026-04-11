package api

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/forge-metal/apiwire"
	"github.com/forge-metal/sandbox-rental-service/internal/jobs"
)

type CreateWebhookEndpointInput struct {
	Body apiwire.SandboxCreateWebhookEndpointRequest
}

type CreateWebhookEndpointOutput struct {
	Body apiwire.SandboxCreateWebhookEndpointResponse
}

type ListWebhookEndpointsOutput struct {
	Body []apiwire.SandboxWebhookEndpointRecord
}

type WebhookEndpointIDPath struct {
	EndpointID string `path:"endpoint_id" doc:"Webhook endpoint UUID"`
}

type RotateWebhookEndpointSecretOutput struct {
	Body apiwire.SandboxRotateWebhookEndpointSecretResponse
}

type NoContentOutput struct{}

func createWebhookEndpoint(svc *jobs.Service, publicBaseURL string) func(context.Context, *CreateWebhookEndpointInput) (*CreateWebhookEndpointOutput, error) {
	return func(ctx context.Context, input *CreateWebhookEndpointInput) (*CreateWebhookEndpointOutput, error) {
		identity, err := requireIdentity(ctx)
		if err != nil {
			return nil, err
		}
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		result, err := svc.CreateWebhookEndpoint(ctx, jobs.CreateWebhookEndpointRequest{
			OrgID:        orgID,
			ActorID:      identity.Subject,
			Provider:     input.Body.Provider,
			ProviderHost: input.Body.ProviderHost,
			Label:        input.Body.Label,
		})
		if err != nil {
			return nil, webhookEndpointError(ctx, "create-webhook-endpoint-failed", "create webhook endpoint failed", err)
		}
		return &CreateWebhookEndpointOutput{
			Body: createWebhookEndpointResponse(result, webhookEndpointURL(publicBaseURL, result.Endpoint.EndpointID)),
		}, nil
	}
}

func listWebhookEndpoints(svc *jobs.Service) func(context.Context, *EmptyInput) (*ListWebhookEndpointsOutput, error) {
	return func(ctx context.Context, _ *EmptyInput) (*ListWebhookEndpointsOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		endpoints, err := svc.ListWebhookEndpoints(ctx, orgID)
		if err != nil {
			return nil, internalFailure(ctx, "list-webhook-endpoints-failed", "list webhook endpoints failed", err)
		}
		return &ListWebhookEndpointsOutput{Body: webhookEndpointRecords(endpoints)}, nil
	}
}

func rotateWebhookEndpointSecret(svc *jobs.Service) func(context.Context, *WebhookEndpointIDPath) (*RotateWebhookEndpointSecretOutput, error) {
	return func(ctx context.Context, input *WebhookEndpointIDPath) (*RotateWebhookEndpointSecretOutput, error) {
		identity, err := requireIdentity(ctx)
		if err != nil {
			return nil, err
		}
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		endpointID, err := uuid.Parse(strings.TrimSpace(input.EndpointID))
		if err != nil {
			return nil, badRequest(ctx, "invalid-webhook-endpoint-id", "endpoint_id must be a UUID", err)
		}
		result, err := svc.RotateWebhookEndpointSecret(ctx, orgID, endpointID, identity.Subject)
		if err != nil {
			return nil, webhookEndpointError(ctx, "rotate-webhook-endpoint-secret-failed", "rotate webhook endpoint secret failed", err)
		}
		return &RotateWebhookEndpointSecretOutput{Body: rotateWebhookEndpointSecretResponse(result)}, nil
	}
}

func deleteWebhookEndpoint(svc *jobs.Service) func(context.Context, *WebhookEndpointIDPath) (*NoContentOutput, error) {
	return func(ctx context.Context, input *WebhookEndpointIDPath) (*NoContentOutput, error) {
		orgID, err := requireOrgID(ctx)
		if err != nil {
			return nil, err
		}
		endpointID, err := uuid.Parse(strings.TrimSpace(input.EndpointID))
		if err != nil {
			return nil, badRequest(ctx, "invalid-webhook-endpoint-id", "endpoint_id must be a UUID", err)
		}
		if err := svc.DeactivateWebhookEndpoint(ctx, orgID, endpointID); err != nil {
			return nil, webhookEndpointError(ctx, "delete-webhook-endpoint-failed", "delete webhook endpoint failed", err)
		}
		return &NoContentOutput{}, nil
	}
}

func webhookEndpointURL(publicBaseURL string, endpointID uuid.UUID) string {
	path := webhookIngestPathPrefix + endpointID.String()
	base := strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	if base == "" {
		return path
	}
	if parsed, err := url.Parse(base); err == nil && parsed.IsAbs() {
		return base + path
	}
	return path
}

func webhookEndpointError(ctx context.Context, code, detail string, err error) error {
	switch {
	case errors.Is(err, jobs.ErrWebhookEndpointMissing):
		return notFound(ctx, "webhook-endpoint-not-found", "webhook endpoint not found")
	case errors.Is(err, jobs.ErrWebhookProviderUnsupported):
		return badRequest(ctx, "webhook-provider-unsupported", "webhook provider is not supported", err)
	case errors.Is(err, jobs.ErrWebhookEndpointInvalid):
		return badRequest(ctx, "webhook-endpoint-invalid", "webhook endpoint is invalid", err)
	default:
		return internalFailure(ctx, code, detail, err)
	}
}
