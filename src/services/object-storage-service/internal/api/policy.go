package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
)

var apiTracer = otel.Tracer("object-storage-service/internal/api")

type operationPrincipal struct {
	OrgID string
	Actor string
}

func registerAdminRoute[I, O any](api huma.API, authorizer runtimeiam.OperationAuthorizer, op huma.Operation, policy runtimeiam.OperationPolicy, handler func(context.Context, operationPrincipal, *I) (*O, error)) {
	if op.OperationID == "" {
		panic("missing operation ID for object-storage route")
	}
	op = withOperationPolicy(op, policy)
	huma.Register(api, op, func(ctx context.Context, input *I) (*O, error) {
		ctx, span := startOperationSpan(ctx, op.OperationID, policy)
		defer span.End()
		principal, err := enforceOperationPolicy(ctx, authorizer, policy)
		if err != nil {
			finishOperationSpan(span, principal, "denied", err)
			return nil, err
		}
		output, err := handler(ctx, principal, input)
		if err != nil {
			finishOperationSpan(span, principal, "error", err)
			return nil, err
		}
		finishOperationSpan(span, principal, "allowed", nil)
		return output, nil
	})
}

func startOperationSpan(ctx context.Context, operationID string, policy runtimeiam.OperationPolicy) (context.Context, trace.Span) {
	return apiTracer.Start(ctx, policy.AuditEvent.String(), trace.WithAttributes(
		attribute.String("object_storage.operation_id", operationID),
		attribute.String("object_storage.permission", string(policy.Permission)),
		attribute.String("object_storage.resource", string(policy.Resource)),
		attribute.String("object_storage.action", string(policy.Action)),
		attribute.String("object_storage.audit_event", string(policy.AuditEvent)),
	))
}

func finishOperationSpan(span trace.Span, principal operationPrincipal, outcome string, err error) {
	if span == nil {
		return
	}
	if principal.OrgID != "" {
		span.SetAttributes(attribute.String("verself.org_id", principal.OrgID))
	}
	if principal.Actor != "" {
		span.SetAttributes(attribute.String("object_storage.actor", principal.Actor))
	}
	span.SetAttributes(attribute.String("object_storage.outcome", outcome))
	if err != nil {
		span.RecordError(err)
		if outcome != "denied" {
			span.SetStatus(codes.Error, err.Error())
		}
	}
}

func withOperationPolicy(op huma.Operation, policy runtimeiam.OperationPolicy) huma.Operation {
	if err := policy.ValidateHTTPOperation(op.Method, op.OperationID); err != nil {
		panic(err)
	}
	if policy.BodyLimitBytes > 0 {
		op.MaxBodyBytes = policy.BodyLimitBytes
	}
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	op.Extensions["x-verself-iam"] = policy.OpenAPIExtension()
	op.Security = []map[string][]string{{"bearerAuth": {}}}
	return op
}

func enforceOperationPolicy(ctx context.Context, authorizer runtimeiam.OperationAuthorizer, policy runtimeiam.OperationPolicy) (operationPrincipal, error) {
	identity := auth.FromContext(ctx)
	if identity == nil {
		return operationPrincipal{}, problem(ctx, http.StatusUnauthorized, "unauthorized", "authentication required", nil)
	}
	orgID := strings.TrimSpace(identity.OrgID)
	if orgID == "" {
		return operationPrincipal{}, problem(ctx, http.StatusForbidden, "organization-required", "object-storage routes require an organization-scoped token", nil)
	}
	principal := operationPrincipal{OrgID: orgID, Actor: actorFromIdentity(identity)}
	if authorizer == nil {
		return principal, problem(ctx, http.StatusServiceUnavailable, "iam-authorizer-unavailable", "IAM authorizer unavailable", runtimeiam.ErrAuthorizerUnavailable)
	}
	decision, err := authorizer.AuthorizeOperation(ctx, identity, policy)
	if err != nil {
		return principal, problem(ctx, http.StatusServiceUnavailable, "iam-authorizer-unavailable", "IAM authorization check failed", err)
	}
	if !decision.Allowed {
		return principal, problem(ctx, http.StatusForbidden, "permission-denied", "missing required object-storage permission", nil)
	}
	return principal, nil
}

func actorFromIdentity(identity *auth.Identity) string {
	if identity == nil {
		return ""
	}
	if serviceAccountID, _ := identity.Raw["verself:service_account_id"].(string); strings.TrimSpace(serviceAccountID) != "" {
		return "service_account:" + strings.TrimSpace(serviceAccountID)
	}
	subject := strings.TrimSpace(identity.Subject)
	if subject == "" {
		return ""
	}
	return "user:" + subject
}

func applyPublicSecurityScheme(api huma.API) {
	components := api.OpenAPI().Components
	if components.SecuritySchemes == nil {
		components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	components.SecuritySchemes["bearerAuth"] = &huma.SecurityScheme{
		Type:         "http",
		Scheme:       "bearer",
		BearerFormat: "JWT",
		Description:  "Zitadel-issued bearer token scoped to the Verself product API audience.",
	}
}
