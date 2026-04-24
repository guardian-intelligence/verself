package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel/trace"

	"github.com/forge-metal/notifications-service/internal/notifications"
)

const problemTypePrefix = "urn:forge-metal:problem:notifications:"

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

func internalFailure(ctx context.Context, code, detail string, cause error) error {
	return problem(ctx, http.StatusInternalServerError, code, detail, cause)
}

func notificationError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, notifications.ErrInvalidInput):
		return badRequest(ctx, "invalid-request", "invalid notification request", err)
	case errors.Is(err, notifications.ErrConflict):
		return conflict(ctx, "notification-version-conflict", "notification resource version conflict", err)
	case errors.Is(err, notifications.ErrNotFound):
		return notFound(ctx, "notification-not-found", "notification not found")
	case errors.Is(err, notifications.ErrStoreUnavailable):
		return internalFailure(ctx, "notification-store-unavailable", "notification store unavailable", err)
	default:
		return internalFailure(ctx, "notification-operation-failed", "notification operation failed", err)
	}
}
