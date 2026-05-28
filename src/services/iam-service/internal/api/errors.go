package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/iam-service/internal/authz"
	"github.com/verself/iam-service/internal/identity"
)

const (
	problemTypePrefix = "urn:verself:problem:"
)

type verselfProblem struct {
	huma.ErrorModel
	Tag         string `json:"_tag"`
	Code        string `json:"code"`
	Message     string `json:"message"`
	Retryable   bool   `json:"retryable"`
	RequestID   string `json:"requestId,omitempty"`
	Traceparent string `json:"traceparent,omitempty"`
}

func problem(ctx context.Context, status int, code, detail string, cause error) error {
	if cause != nil {
		trace.SpanFromContext(ctx).RecordError(cause)
	}
	instance := ""
	traceparent := ""
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.HasTraceID() {
		instance = "urn:verself:trace:" + spanContext.TraceID().String()
		if spanContext.HasSpanID() {
			traceparent = "00-" + spanContext.TraceID().String() + "-" + spanContext.SpanID().String() + "-" + spanContext.TraceFlags().String()
		}
	}
	requestID := operationRequestInfoFromContext(ctx).RequestID
	if requestID == "" && trace.SpanContextFromContext(ctx).HasTraceID() {
		requestID = trace.SpanContextFromContext(ctx).TraceID().String()
	}
	problemType := problemType(code)
	return &verselfProblem{
		ErrorModel: huma.ErrorModel{
			Type:     problemType,
			Title:    problemTitle(code, status),
			Status:   status,
			Detail:   detail,
			Instance: instance,
		},
		Tag:         problemType,
		Code:        code,
		Message:     authErrorMessageForCode(code, detail, status),
		Retryable:   authProblemRetryableForCode(code, status),
		RequestID:   requestID,
		Traceparent: traceparent,
	}
}

func problemType(code string) string {
	code = strings.TrimSpace(code)
	switch {
	case strings.HasPrefix(code, "request."):
		return problemTypePrefix + "request:" + strings.ReplaceAll(strings.TrimPrefix(code, "request."), ".", "_")
	case strings.HasPrefix(code, "auth."):
		return problemTypePrefix + "auth:" + strings.ReplaceAll(strings.TrimPrefix(code, "auth."), ".", "_")
	case strings.HasPrefix(code, "conflict."):
		return problemTypePrefix + "conflict:" + strings.ReplaceAll(strings.TrimPrefix(code, "conflict."), ".", "_")
	case strings.HasPrefix(code, "quota."):
		return problemTypePrefix + "quota:" + strings.ReplaceAll(strings.TrimPrefix(code, "quota."), ".", "_")
	case strings.HasPrefix(code, "resource."):
		return problemTypePrefix + "resource:" + strings.ReplaceAll(strings.TrimPrefix(code, "resource."), ".", "_")
	case strings.HasPrefix(code, "service."):
		return problemTypePrefix + "service:" + strings.ReplaceAll(strings.TrimPrefix(code, "service."), ".", "_")
	case strings.HasPrefix(code, "iam."):
		return problemTypePrefix + "iam:" + strings.ReplaceAll(strings.TrimPrefix(code, "iam."), ".", "_")
	default:
		return problemTypePrefix + "iam:" + strings.ReplaceAll(strings.ReplaceAll(code, "-", "_"), ".", "_")
	}
}

func problemTitle(code string, status int) string {
	if definition, ok := authProblemCatalog[authProblemCode(code)]; ok {
		return definition.Title
	}
	if title := publicAuthProblemTitle(code); title != "" {
		return title
	}
	return http.StatusText(status)
}

func authErrorMessageForCode(code, detail string, status int) string {
	if definition, ok := authProblemCatalog[authProblemCode(code)]; ok {
		if definition.Message != "" {
			return definition.Message
		}
		return definition.Detail
	}
	switch code {
	case "iam.password.breached":
		return "This password has appeared in a public data breach. Please use a different one."
	case "iam.signup.verification.expired":
		return "This signup link has expired. Request a new signup email."
	case "iam.signup.verification.invalid":
		return "This signup link is no longer valid."
	case "iam.signup.verification.already_used":
		return "This signup is complete. Sign in with the account that completed signup."
	case "iam.signup.materializing":
		return "Signup is still being finalized. Wait a moment and try again."
	case "iam.signup.account_exists":
		return "This email already has a Verself account. Sign in instead."
	case "iam.signup.state_conflict":
		return "Signup could not be completed because the request state changed."
	case "iam.organization_slug.unavailable":
		return "That organization URL is already taken."
	case "conflict.idempotency_payload_mismatch":
		return "This request was already submitted with different data."
	}
	detail = strings.TrimSpace(detail)
	if detail != "" && status < 500 {
		return detail
	}
	if status == http.StatusTooManyRequests {
		return "Too many attempts. Wait a moment and try again."
	}
	if status >= 500 {
		return "Authentication is unavailable right now."
	}
	return "Authentication request failed."
}

func authProblemRetryableForCode(code string, status int) bool {
	if definition, ok := authProblemCatalog[authProblemCode(code)]; ok {
		return definition.Retryable
	}
	switch {
	case code == "quota.rate_limited":
		return true
	case code == "service.unavailable":
		return true
	case status == http.StatusTooManyRequests:
		return true
	default:
		return false
	}
}

func publicAuthProblemTitle(code string) string {
	switch code {
	case "iam.password.breached":
		return "Password breached"
	case "iam.signup.verification.expired":
		return "Signup verification expired"
	case "iam.signup.verification.invalid":
		return "Signup verification invalid"
	case "iam.signup.verification.already_used":
		return "Signup verification already used"
	case "iam.signup.materializing":
		return "Signup materializing"
	case "iam.signup.account_exists":
		return "Signup account exists"
	case "iam.signup.state_conflict":
		return "Signup state conflict"
	case "iam.organization_slug.unavailable":
		return "Organization slug unavailable"
	case "conflict.idempotency_payload_mismatch":
		return "Idempotency conflict"
	default:
		return ""
	}
}

func badRequest(ctx context.Context, code, detail string, cause error) error {
	return problem(ctx, http.StatusBadRequest, code, detail, cause)
}

func unauthorized(ctx context.Context) error {
	return problem(ctx, http.StatusUnauthorized, "auth.unauthenticated", "authentication required", nil)
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

func identityError(ctx context.Context, err error) error {
	switch {
	case identity.IsInvalid(err):
		return badRequest(ctx, "request.validation_failed", "invalid identity request", err)
	case errors.Is(err, identity.ErrPasswordTooShort):
		return badRequest(ctx, "iam.password.too_short", fmt.Sprintf("password must be at least %d characters", identity.PasswordMinRunes), err)
	case errors.Is(err, identity.ErrPasswordTooLong):
		return badRequest(ctx, "iam.password.too_long", "password is too long", err)
	case errors.Is(err, identity.ErrPasswordBreached):
		return badRequest(ctx, "iam.password.breached", "password appears in known breach data", err)
	case errors.Is(err, identity.ErrPasswordRejected):
		return badRequest(ctx, "iam.password.rejected", "password does not meet the current password policy", err)
	case errors.Is(err, identity.ErrIdempotencyConflict):
		return conflict(ctx, "conflict.idempotency_payload_mismatch", "idempotency key was already used with a different request", err)
	case errors.Is(err, identity.ErrSignupVerificationAlreadyUsed):
		return conflict(ctx, "iam.signup.verification.already_used", "signup verification was already used", err)
	case errors.Is(err, identity.ErrSignupIntentExpired):
		return badRequest(ctx, "iam.signup.verification.expired", "signup verification expired", err)
	case errors.Is(err, identity.ErrSignupIntentMissing):
		return badRequest(ctx, "iam.signup.verification.invalid", "signup verification is invalid", err)
	case errors.Is(err, identity.ErrSignupMaterializing):
		return conflict(ctx, "iam.signup.materializing", "signup verification is already being materialized", err)
	case errors.Is(err, identity.ErrSignupAccountExists):
		return conflict(ctx, "iam.signup.account_exists", "signup email already belongs to an account", err)
	case errors.Is(err, identity.ErrSignupIntentConflict):
		return conflict(ctx, "iam.signup.state_conflict", "signup state cannot accept this operation", err)
	case errors.Is(err, identity.ErrOrganizationSlugUnavailable):
		return conflict(ctx, "iam.organization_slug.unavailable", "organization slug is unavailable", err)
	case errors.Is(err, identity.ErrMemberMissing):
		return notFound(ctx, "resource.not_found", "organization member not found")
	case errors.Is(err, identity.ErrOrganizationConflict):
		return conflict(ctx, "conflict.state", "organization profile changed before this update was applied", err)
	case errors.Is(err, identity.ErrOrganizationMissing):
		return notFound(ctx, "resource.not_found", "organization not found")
	case errors.Is(err, identity.ErrAPICredentialMissing):
		return notFound(ctx, "resource.not_found", "API credential not found")
	case errors.Is(err, identity.ErrZitadelUnavailable):
		return upstreamFailure(ctx, "service.unavailable", "identity provider unavailable", err)
	case errors.Is(err, identity.ErrBillingUnavailable):
		return upstreamFailure(ctx, "service.unavailable", "billing service unavailable", err)
	case errors.Is(err, identity.ErrAuthzUnavailable):
		return internalFailure(ctx, "service.unavailable", "authorization graph unavailable", err)
	case errors.Is(err, identity.ErrConfiguration):
		return internalFailure(ctx, "service.unavailable", "IAM configuration is unavailable", err)
	case errors.Is(err, identity.ErrStoreUnavailable):
		return internalFailure(ctx, "service.unavailable", "iam store unavailable", err)
	default:
		return internalFailure(ctx, "service.unavailable", "IAM operation failed", err)
	}
}

func authzError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, authz.ErrInvalid):
		return badRequest(ctx, "invalid-iam-policy", "invalid IAM policy request", err)
	case errors.Is(err, authz.ErrConflict):
		return conflict(ctx, "iam-policy-etag-conflict", "IAM policy changed before this update was applied", err)
	case errors.Is(err, authz.ErrUnavailable):
		return internalFailure(ctx, "iam-authz-unavailable", "authorization graph unavailable", err)
	default:
		return internalFailure(ctx, "iam-authz-operation-failed", "authorization graph operation failed", err)
	}
}
