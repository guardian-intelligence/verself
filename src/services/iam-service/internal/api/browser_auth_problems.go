package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

type authProblemCode string

const (
	problemRequestMethodNotAllowed  authProblemCode = "request.method_not_allowed"
	problemRequestBodyTooLarge      authProblemCode = "request.body_too_large"
	problemRequestValidationFailed  authProblemCode = "request.validation_failed"
	problemAuthOriginNotAllowed     authProblemCode = "auth.origin_not_allowed"
	problemAuthInvalidCredentials   authProblemCode = "auth.invalid_credentials"
	problemAuthConstraintFailed     authProblemCode = "auth.login_constraint_failed"
	problemAuthUnauthenticated      authProblemCode = "auth.unauthenticated"
	problemAuthProviderSession      authProblemCode = "auth.provider_session_required"
	problemAuthOrganizationMissing  authProblemCode = "auth.organization_unavailable"
	problemAuthAccountMissing       authProblemCode = "auth.account_unavailable"
	problemAuthSessionMissing       authProblemCode = "auth.session_unavailable"
	problemIAMPasswordTooShort      authProblemCode = "iam.password.too_short"
	problemIAMPasswordTooLong       authProblemCode = "iam.password.too_long"
	problemIAMPasswordRejected      authProblemCode = "iam.password.rejected"
	problemIAMPasswordResetInvalid  authProblemCode = "iam.password_reset.token_invalid"
	problemIAMDeviceLoginNotFound   authProblemCode = "iam.device_login.not_found"
	problemIAMDeviceLoginExpired    authProblemCode = "iam.device_login.expired"
	problemIAMDeviceLoginNotPending authProblemCode = "iam.device_login.not_pending"
	problemOIDCCallbackFailed       authProblemCode = "auth.oidc_callback_failed"
	problemQuotaRateLimited         authProblemCode = "quota.rate_limited"
	problemResourceNotFound         authProblemCode = "resource.not_found"
	problemServiceUnavailable       authProblemCode = "service.unavailable"
)

type authProblemDefinition struct {
	Status    int
	Title     string
	Detail    string
	Message   string
	Retryable bool
}

type authProblemOccurrence struct {
	Tag         string `json:"_tag"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	Status      int    `json:"status"`
	Detail      string `json:"detail"`
	Message     string `json:"message"`
	Instance    string `json:"instance,omitempty"`
	Code        string `json:"code"`
	RequestID   string `json:"requestId,omitempty"`
	Traceparent string `json:"traceparent,omitempty"`
	Retryable   bool   `json:"retryable,omitempty"`
}

type authProblemOptions struct {
	status    int
	detail    string
	retryable *bool
	cause     error
}

type authProblemOption func(*authProblemOptions)

func withAuthProblemStatus(status int) authProblemOption {
	return func(options *authProblemOptions) {
		options.status = status
	}
}

func withAuthProblemDetail(detail string) authProblemOption {
	return func(options *authProblemOptions) {
		options.detail = strings.TrimSpace(detail)
	}
}

func withAuthProblemCause(cause error) authProblemOption {
	return func(options *authProblemOptions) {
		options.cause = cause
	}
}

func authProblemFromCatalog(r *http.Request, code authProblemCode, opts ...authProblemOption) authProblemOccurrence {
	options := authProblemOptions{}
	for _, opt := range opts {
		opt(&options)
	}
	definition, ok := authProblemCatalog[code]
	if !ok {
		definition = authProblemCatalog[problemServiceUnavailable]
		code = problemServiceUnavailable
	}
	status := definition.Status
	if options.status > 0 {
		status = options.status
	}
	detail := definition.Detail
	if options.detail != "" {
		detail = options.detail
	}
	message := definition.Message
	if message == "" {
		message = definition.Detail
	}
	retryable := definition.Retryable
	if options.retryable != nil {
		retryable = *options.retryable
	}
	problemType := problemType(string(code))
	instance := ""
	traceparent := ""
	requestID := ""
	if r != nil {
		if options.cause != nil {
			trace.SpanFromContext(r.Context()).RecordError(options.cause)
		}
		spanContext := trace.SpanContextFromContext(r.Context())
		if spanContext.HasTraceID() {
			instance = "urn:verself:trace:" + spanContext.TraceID().String()
			requestID = spanContext.TraceID().String()
			if spanContext.HasSpanID() {
				traceparent = "00-" + spanContext.TraceID().String() + "-" + spanContext.SpanID().String() + "-" + spanContext.TraceFlags().String()
			}
		}
	}
	return authProblemOccurrence{
		Tag:         problemType,
		Type:        problemType,
		Title:       definition.Title,
		Status:      status,
		Detail:      detail,
		Message:     message,
		Instance:    instance,
		Code:        string(code),
		RequestID:   requestID,
		Traceparent: traceparent,
		Retryable:   retryable,
	}
}

func (a *BrowserAuth) writeProblem(w http.ResponseWriter, r *http.Request, status int, code authProblemCode, detail string) {
	writeBrowserAuthProblem(w, authProblemFromCatalog(
		r,
		code,
		withAuthProblemStatus(status),
		withAuthProblemDetail(detail),
	))
}

func (a *BrowserAuth) writeCatalogProblem(w http.ResponseWriter, r *http.Request, code authProblemCode, opts ...authProblemOption) {
	writeBrowserAuthProblem(w, authProblemFromCatalog(r, code, opts...))
}

func writeBrowserAuthProblem(w http.ResponseWriter, problem authProblemOccurrence) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(problem.Status)
	_ = json.NewEncoder(w).Encode(problem)
}

var authProblemCatalog = map[authProblemCode]authProblemDefinition{
	problemRequestMethodNotAllowed: {
		Status: http.StatusMethodNotAllowed,
		Title:  "Method not allowed",
		Detail: "The auth endpoint does not allow this method.",
	},
	problemRequestBodyTooLarge: {
		Status: http.StatusRequestEntityTooLarge,
		Title:  "Request body too large",
		Detail: "The auth request body is too large.",
	},
	problemRequestValidationFailed: {
		Status:  http.StatusBadRequest,
		Title:   "Validation failed",
		Detail:  "The auth request is invalid.",
		Message: "Check the highlighted fields.",
	},
	problemAuthOriginNotAllowed: {
		Status: http.StatusForbidden,
		Title:  "Origin not allowed",
		Detail: "The auth request did not come from the Verself origin.",
	},
	problemAuthInvalidCredentials: {
		Status: http.StatusUnauthorized,
		Title:  "Invalid credentials",
		Detail: "Email or password is incorrect.",
	},
	problemAuthConstraintFailed: {
		Status: http.StatusForbidden,
		Title:  "Required account mismatch",
		Detail: "Sign in with the required account to continue.",
	},
	problemAuthUnauthenticated: {
		Status: http.StatusUnauthorized,
		Title:  "Authentication required",
		Detail: "Authentication required.",
	},
	problemAuthProviderSession: {
		Status: http.StatusConflict,
		Title:  "Provider session required",
		Detail: "Sign in again before approving this device.",
	},
	problemAuthOrganizationMissing: {
		Status: http.StatusForbidden,
		Title:  "Organization unavailable",
		Detail: "The selected organization is not available to this account.",
	},
	problemAuthAccountMissing: {
		Status: http.StatusForbidden,
		Title:  "Account unavailable",
		Detail: "The selected account is not available to this browser.",
	},
	problemAuthSessionMissing: {
		Status: http.StatusForbidden,
		Title:  "Session unavailable",
		Detail: "The selected session is not available to this browser.",
	},
	problemIAMPasswordTooShort: {
		Status:  http.StatusBadRequest,
		Title:   "Password too short",
		Detail:  "Password is too short.",
		Message: "Use at least 8 characters.",
	},
	problemIAMPasswordTooLong: {
		Status: http.StatusBadRequest,
		Title:  "Password too long",
		Detail: "Password is too long.",
	},
	problemIAMPasswordRejected: {
		Status: http.StatusBadRequest,
		Title:  "Password rejected",
		Detail: "Password does not meet the current password policy.",
	},
	problemIAMPasswordResetInvalid: {
		Status: http.StatusBadRequest,
		Title:  "Reset token invalid",
		Detail: "Reset link is invalid or expired.",
	},
	problemIAMDeviceLoginNotFound: {
		Status: http.StatusNotFound,
		Title:  "Device login not found",
		Detail: "This device code is not valid.",
	},
	problemIAMDeviceLoginExpired: {
		Status: http.StatusGone,
		Title:  "Device login expired",
		Detail: "This device code has expired.",
	},
	problemIAMDeviceLoginNotPending: {
		Status: http.StatusConflict,
		Title:  "Device login not pending",
		Detail: "This device login is no longer pending.",
	},
	problemOIDCCallbackFailed: {
		Status: http.StatusBadRequest,
		Title:  "OIDC callback failed",
		Detail: "The sign-in callback could not be completed.",
	},
	problemQuotaRateLimited: {
		Status:    http.StatusTooManyRequests,
		Title:     "Rate limited",
		Detail:    "Rate limit exceeded.",
		Retryable: true,
	},
	problemResourceNotFound: {
		Status: http.StatusNotFound,
		Title:  "Not found",
		Detail: "The auth route was not found.",
	},
	problemServiceUnavailable: {
		Status:    http.StatusServiceUnavailable,
		Title:     "Service unavailable",
		Detail:    "Authentication is unavailable right now.",
		Retryable: true,
	},
}
