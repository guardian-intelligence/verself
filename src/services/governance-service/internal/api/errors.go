package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/governance-service/internal/governance"
)

type verselfProblem struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Status      int    `json:"status"`
	Detail      string `json:"detail,omitempty"`
	Instance    string `json:"instance,omitempty"`
	Code        string `json:"code"`
	RequestID   string `json:"requestId,omitempty"`
	Traceparent string `json:"traceparent,omitempty"`
}

func init() {
	huma.NewError = func(status int, msg string, errs ...error) huma.StatusError {
		return newProblem(context.Background(), status, problemCodeForStatus(status), msg, errs...)
	}
	huma.NewErrorWithContext = func(ctx huma.Context, status int, msg string, errs ...error) huma.StatusError {
		return newProblem(ctx.Context(), status, problemCodeForStatus(status), msg, errs...)
	}
}

func problem(ctx context.Context, status int, code, detail string, cause error) error {
	if cause != nil {
		trace.SpanFromContext(ctx).RecordError(cause)
	}
	return newProblem(ctx, status, code, detail)
}

func newProblem(ctx context.Context, status int, code, detail string, errs ...error) *verselfProblem {
	if status == http.StatusUnprocessableEntity {
		status = http.StatusBadRequest
	}
	code = problemCodeForCodeOrStatus(code, status)
	instance := ""
	traceparent := ""
	spanContext := trace.SpanContextFromContext(ctx)
	if spanContext.HasTraceID() {
		instance = "urn:verself:trace:" + spanContext.TraceID().String()
		if spanContext.HasSpanID() {
			traceparent = "00-" + spanContext.TraceID().String() + "-" + spanContext.SpanID().String() + "-01"
		}
	}
	requestID := operationRequestInfoFromContext(ctx).RequestID
	return &verselfProblem{
		Type:        problemTypeForCode(code),
		Title:       http.StatusText(status),
		Status:      status,
		Detail:      problemDetail(detail, errs),
		Instance:    instance,
		Code:        code,
		RequestID:   requestID,
		Traceparent: traceparent,
	}
}

func problemDetail(detail string, errs []error) string {
	detail = strings.TrimSpace(detail)
	for _, err := range errs {
		if err == nil {
			continue
		}
		errText := strings.TrimSpace(err.Error())
		if errText == "" {
			continue
		}
		if detail == "" {
			return truncateProblemDetail(errText)
		}
		return truncateProblemDetail(detail + ": " + errText)
	}
	return truncateProblemDetail(detail)
}

func truncateProblemDetail(detail string) string {
	if len(detail) <= 4096 {
		return detail
	}
	return detail[:4096]
}

func (p *verselfProblem) Error() string {
	return p.Detail
}

func (p *verselfProblem) GetStatus() int {
	return p.Status
}

func (p *verselfProblem) ContentType(contentType string) string {
	if contentType == "application/json" {
		return "application/problem+json"
	}
	return contentType
}

func mapError(ctx context.Context, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, governance.ErrInvalidArgument):
		return problem(ctx, http.StatusBadRequest, "request.validation_failed", err.Error(), err)
	case errors.Is(err, governance.ErrForbidden):
		return problem(ctx, http.StatusForbidden, "auth.permission_denied", "permission denied", err)
	case errors.Is(err, governance.ErrNotFound):
		return problem(ctx, http.StatusNotFound, "resource.not_found", "resource not found", err)
	case errors.Is(err, governance.ErrStore):
		return problem(ctx, http.StatusServiceUnavailable, "service.unavailable", "service unavailable", err)
	default:
		return problem(ctx, http.StatusServiceUnavailable, "service.unavailable", "service unavailable", err)
	}
}

func unauthorized(ctx context.Context, code, detail string) error {
	return problem(ctx, http.StatusUnauthorized, code, detail, nil)
}

func forbidden(ctx context.Context, code, detail string) error {
	return problem(ctx, http.StatusForbidden, code, detail, nil)
}

func problemCodeForCodeOrStatus(code string, status int) string {
	code = strings.TrimSpace(code)
	if code != "" {
		switch code {
		case "missing-identity", "missing-workload-identity", "unauthorized":
			return "auth.unauthenticated"
		case "missing-org", "permission-denied":
			return "auth.permission_denied"
		case "not-found":
			return "resource.not_found"
		case "rate-limit-exceeded":
			return "quota.rate_limited"
		case "idempotency-key-required", "idempotency-key-invalid", "invalid-api-activity", "invalid-request":
			return "request.validation_failed"
		case "iam-authorizer-unavailable", "iam-policy-invalid", "internal-error":
			return "service.unavailable"
		default:
			if strings.Contains(code, ".") {
				return code
			}
		}
	}
	return problemCodeForStatus(status)
}

func problemCodeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return "request.validation_failed"
	case http.StatusUnauthorized:
		return "auth.unauthenticated"
	case http.StatusForbidden:
		return "auth.permission_denied"
	case http.StatusNotFound:
		return "resource.not_found"
	case http.StatusConflict:
		return "conflict.state"
	case http.StatusTooManyRequests:
		return "quota.rate_limited"
	case http.StatusServiceUnavailable:
		return "service.unavailable"
	default:
		return "service.unavailable"
	}
}

func problemTypeForCode(code string) string {
	switch code {
	case "request.validation_failed":
		return "urn:verself:problem:request:validation_failed"
	case "auth.unauthenticated":
		return "urn:verself:problem:auth:unauthenticated"
	case "auth.permission_denied":
		return "urn:verself:problem:auth:permission_denied"
	case "resource.not_found":
		return "urn:verself:problem:resource:not_found"
	case "conflict.idempotency_payload_mismatch":
		return "urn:verself:problem:conflict:idempotency_payload_mismatch"
	case "conflict.state":
		return "urn:verself:problem:conflict:state"
	case "quota.rate_limited":
		return "urn:verself:problem:quota:rate_limited"
	case "service.unavailable":
		return "urn:verself:problem:service:unavailable"
	default:
		return "urn:verself:problem:service:unavailable"
	}
}
