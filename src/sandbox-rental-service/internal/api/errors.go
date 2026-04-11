package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel/trace"
)

const problemTypePrefix = "urn:forge-metal:problem:sandbox-rental:"

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

func paymentRequired(ctx context.Context, detail string) error {
	return problem(ctx, http.StatusPaymentRequired, "payment-required", detail, nil)
}

func unprocessableEntity(ctx context.Context, code, detail string, cause error) error {
	return problem(ctx, http.StatusUnprocessableEntity, code, detail, cause)
}

func notFound(ctx context.Context, code, detail string) error {
	return problem(ctx, http.StatusNotFound, code, detail, nil)
}

func conflict(ctx context.Context, code, detail string) error {
	return problem(ctx, http.StatusConflict, code, detail, nil)
}

func tooManyRequests(ctx context.Context, code, detail string) error {
	return problem(ctx, http.StatusTooManyRequests, code, detail, nil)
}

func internalFailure(ctx context.Context, code, detail string, cause error) error {
	return problem(ctx, http.StatusInternalServerError, code, detail, cause)
}

func upstreamFailure(ctx context.Context, code, detail string, cause error) error {
	return problem(ctx, http.StatusBadGateway, code, detail, cause)
}
