package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel/trace"

	"github.com/forge-metal/profile-service/internal/profile"
)

const problemTypePrefix = "urn:forge-metal:problem:profile:"

func problem(ctx context.Context, status int, code, detail string, cause error) error {
	if cause != nil {
		trace.SpanFromContext(ctx).RecordError(cause)
	}
	instance := ""
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.HasTraceID() {
		instance = "urn:forge-metal:trace:" + spanContext.TraceID().String()
	}
	return &huma.ErrorModel{
		Type:     problemTypePrefix + code,
		Title:    http.StatusText(status),
		Status:   status,
		Detail:   detail,
		Instance: instance,
	}
}

func badRequest(ctx context.Context, code, detail string, cause error) error {
	return problem(ctx, http.StatusBadRequest, code, detail, cause)
}

func unauthorized(ctx context.Context) error {
	return problem(ctx, http.StatusUnauthorized, "unauthorized", "authentication required", nil)
}

func forbidden(ctx context.Context, code, detail string) error {
	return problem(ctx, http.StatusForbidden, code, detail, nil)
}

func notFound(ctx context.Context, code, detail string) error {
	return problem(ctx, http.StatusNotFound, code, detail, nil)
}

func conflict(ctx context.Context, code, detail string, cause error) error {
	return problem(ctx, http.StatusConflict, code, detail, cause)
}

func upstreamFailure(ctx context.Context, code, detail string, cause error) error {
	return problem(ctx, http.StatusBadGateway, code, detail, cause)
}

func internalFailure(ctx context.Context, code, detail string, cause error) error {
	return problem(ctx, http.StatusInternalServerError, code, detail, cause)
}

func profileError(ctx context.Context, err error) error {
	switch {
	case profile.IsInvalid(err):
		return badRequest(ctx, "invalid-request", "invalid profile request", err)
	case errors.Is(err, profile.ErrConflict):
		return conflict(ctx, "profile-version-conflict", "profile resource version conflict", err)
	case errors.Is(err, profile.ErrNotFound):
		return notFound(ctx, "profile-request-not-found", "profile request not found")
	case errors.Is(err, profile.ErrIdentityUnavailable):
		return upstreamFailure(ctx, "identity-service-unavailable", "identity service unavailable", err)
	case errors.Is(err, profile.ErrStoreUnavailable):
		return internalFailure(ctx, "profile-store-unavailable", "profile store unavailable", err)
	default:
		return internalFailure(ctx, "profile-operation-failed", "profile operation failed", err)
	}
}
