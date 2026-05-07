package billingapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel/trace"
)

const problemTypePrefix = "urn:verself:problem:billing:"

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
