package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel/trace"

	"github.com/forge-metal/secrets-service/internal/secrets"
)

func problem(ctx context.Context, status int, code, detail string, cause error) error {
	if cause != nil {
		trace.SpanFromContext(ctx).RecordError(cause)
	}
	instance := ""
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.HasTraceID() {
		instance = "urn:forge-metal:trace:" + spanContext.TraceID().String()
	}
	return &huma.ErrorModel{
		Status:   status,
		Type:     "urn:forge-metal:problem:" + code,
		Title:    http.StatusText(status),
		Detail:   detail,
		Instance: instance,
		Errors: []*huma.ErrorDetail{{
			Message:  code,
			Location: "code",
			Value:    code,
		}},
	}
}

func mapError(ctx context.Context, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, secrets.ErrInvalidArgument):
		return problem(ctx, http.StatusBadRequest, "invalid-request", err.Error(), err)
	case errors.Is(err, secrets.ErrForbidden):
		return problem(ctx, http.StatusForbidden, "permission-denied", "permission denied", err)
	case errors.Is(err, secrets.ErrNotFound):
		return problem(ctx, http.StatusNotFound, "not-found", "resource not found", err)
	case errors.Is(err, secrets.ErrConflict):
		return problem(ctx, http.StatusConflict, "conflict", err.Error(), err)
	case errors.Is(err, secrets.ErrCrypto):
		return problem(ctx, http.StatusInternalServerError, "crypto-error", "cryptographic operation failed", err)
	default:
		return problem(ctx, http.StatusInternalServerError, "internal-error", "internal error", err)
	}
}

func unauthorized(ctx context.Context, code, detail string) error {
	return problem(ctx, http.StatusUnauthorized, code, detail, nil)
}

func forbidden(ctx context.Context, code, detail string) error {
	return problem(ctx, http.StatusForbidden, code, detail, nil)
}
