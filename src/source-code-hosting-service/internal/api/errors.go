package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel/trace"

	"github.com/forge-metal/source-code-hosting-service/internal/source"
)

const problemTypePrefix = "urn:forge-metal:problem:source:"

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

func sourceError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, source.ErrInvalid):
		return badRequest(ctx, "invalid-request", "invalid source request", err)
	case errors.Is(err, source.ErrUnauthorized):
		return unauthorized(ctx)
	case errors.Is(err, source.ErrNotFound):
		return notFound(ctx, "repository-not-found", "repository not found")
	case errors.Is(err, source.ErrConflict):
		return conflict(ctx, "source-conflict", "source resource conflict", err)
	case errors.Is(err, source.ErrForgejo):
		return upstreamFailure(ctx, "forgejo-unavailable", "forgejo backing service unavailable", err)
	case errors.Is(err, source.ErrStoreUnavailable):
		return internalFailure(ctx, "source-store-unavailable", "source store unavailable", err)
	default:
		return internalFailure(ctx, "source-operation-failed", "source operation failed", err)
	}
}
