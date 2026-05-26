package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/release-service/internal/release"
)

func releaseError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, release.ErrInvalid):
		return problem(ctx, http.StatusBadRequest, "request.validation_failed", "Invalid release request", err)
	case errors.Is(err, release.ErrNotFound):
		return problem(ctx, http.StatusNotFound, "resource.not_found", "Release resource not found", err)
	case errors.Is(err, release.ErrConflict), errors.Is(err, release.ErrStateConflict), errors.Is(err, release.ErrPolicyDenied):
		return problem(ctx, http.StatusConflict, "conflict.state", "Release state conflict", err)
	case errors.Is(err, release.ErrRegistry), errors.Is(err, release.ErrStoreUnavailable):
		return problem(ctx, http.StatusServiceUnavailable, "service.unavailable", "Release service unavailable", err)
	default:
		return problem(ctx, http.StatusInternalServerError, "service.unavailable", "Release service failed", err)
	}
}

func unauthorized(ctx context.Context) error {
	return problem(ctx, http.StatusUnauthorized, "auth.unauthenticated", "Unauthenticated", nil)
}

func problem(ctx context.Context, status int, code string, title string, err error) error {
	if err != nil {
		trace.SpanFromContext(ctx).RecordError(err)
	}
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	instance := ""
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.HasTraceID() {
		instance = "urn:verself:trace:" + spanContext.TraceID().String()
	}
	return &huma.ErrorModel{
		Type:     "urn:verself:problem:release:" + code,
		Title:    title,
		Status:   status,
		Detail:   detail,
		Instance: instance,
	}
}
