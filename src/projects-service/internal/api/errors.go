package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/projects-service/internal/projects"
)

const problemTypePrefix = "urn:verself:problem:projects:"

func problem(ctx context.Context, status int, code, detail string, cause error) error {
	if cause != nil {
		trace.SpanFromContext(ctx).RecordError(cause)
	}
	instance := ""
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.HasTraceID() {
		instance = "urn:verself:trace:" + spanContext.TraceID().String()
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

func internalFailure(ctx context.Context, code, detail string, cause error) error {
	return problem(ctx, http.StatusInternalServerError, code, detail, cause)
}

func projectsError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, projects.ErrInvalid):
		return badRequest(ctx, "invalid-request", "invalid projects request", err)
	case errors.Is(err, projects.ErrUnauthorized):
		return unauthorized(ctx)
	case errors.Is(err, projects.ErrNotFound):
		return notFound(ctx, "project-not-found", "project resource not found")
	case errors.Is(err, projects.ErrConflict):
		return conflict(ctx, "project-conflict", "project resource conflict", err)
	case errors.Is(err, projects.ErrArchived):
		return conflict(ctx, "project-archived", "project resource is archived", err)
	case errors.Is(err, projects.ErrStoreUnavailable):
		return internalFailure(ctx, "projects-store-unavailable", "projects store unavailable", err)
	default:
		return internalFailure(ctx, "projects-operation-failed", "projects operation failed", err)
	}
}
