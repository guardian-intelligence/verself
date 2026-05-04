package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/iam-service/internal/authz"
	"github.com/verself/iam-service/internal/identity"
)

const problemTypePrefix = "urn:verself:problem:iam:"

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

func upstreamFailure(ctx context.Context, code, detail string, cause error) error {
	return problem(ctx, http.StatusBadGateway, code, detail, cause)
}

func internalFailure(ctx context.Context, code, detail string, cause error) error {
	return problem(ctx, http.StatusInternalServerError, code, detail, cause)
}

func identityError(ctx context.Context, err error) error {
	switch {
	case identity.IsInvalid(err):
		return badRequest(ctx, "invalid-request", "invalid identity request", err)
	case errors.Is(err, identity.ErrMemberMissing):
		return notFound(ctx, "member-not-found", "organization member not found")
	case errors.Is(err, identity.ErrCapabilitiesConflict):
		return conflict(ctx, "member-capabilities-version-conflict", "organization member capabilities version conflict", err)
	case errors.Is(err, identity.ErrOrgACLConflict):
		return conflict(ctx, "organization-acl-version-conflict", "organization ACL changed before this update was applied", err)
	case errors.Is(err, identity.ErrOrganizationConflict):
		return conflict(ctx, "organization-profile-version-conflict", "organization profile changed before this update was applied", err)
	case errors.Is(err, identity.ErrOrganizationMissing):
		return notFound(ctx, "organization-not-found", "organization not found")
	case errors.Is(err, identity.ErrAPICredentialMissing):
		return notFound(ctx, "api-credential-not-found", "API credential not found")
	case errors.Is(err, identity.ErrZitadelUnavailable):
		return upstreamFailure(ctx, "zitadel-unavailable", "identity provider unavailable", err)
	case errors.Is(err, identity.ErrStoreUnavailable):
		return internalFailure(ctx, "iam-store-unavailable", "iam store unavailable", err)
	default:
		return internalFailure(ctx, "iam-operation-failed", "IAM operation failed", err)
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
