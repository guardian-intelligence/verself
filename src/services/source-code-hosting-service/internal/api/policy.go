package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
	workloadauth "github.com/verself/service-runtime/workload"
	"github.com/verself/source-code-hosting-service/internal/source"
)

type permission = runtimeiam.Permission

const (
	permissionRepoRead           permission = "source:repo:read"
	permissionRepoWrite          permission = "source:repo:write"
	permissionCheckoutWrite      permission = "source:checkout:write"
	permissionGitCredentialWrite permission = "source:git_credential:write"
	permissionWorkflowRead       permission = "source:workflow:read"
	permissionWorkflowWrite      permission = "source:workflow:write"

	idempotencyHeaderKey    = runtimeiam.IdempotencyHeaderKey
	maxIdempotencyKeyLength = 128
	bodyLimitSmallJSON      = 64 << 10
	bodyLimitNoBody         = 1
)

var apiTracer = otel.Tracer("source-code-hosting-service/internal/api")

type sourceOperationPolicy struct {
	runtimeiam.OperationPolicy
	Internal bool
}

type operationRequestInfoKey struct{}

type operationRequestInfo struct {
	IdempotencyKey string
}

func registerSourceRoute[I, O any](api huma.API, authorizer runtimeiam.OperationAuthorizer, op huma.Operation, policy sourceOperationPolicy, handler func(context.Context, source.Principal, *I) (*O, error)) {
	if op.OperationID == "" {
		panic("missing operation ID for source API route")
	}
	op = withOperationPolicy(op, policy)
	op.Middlewares = append(op.Middlewares, operationRequestMiddleware)
	huma.Register(api, op, func(ctx context.Context, input *I) (*O, error) {
		ctx, span := startOperationSpan(ctx, op.OperationID, policy)
		defer span.End()
		principal, err := enforceOperationPolicy(ctx, authorizer, policy)
		if err != nil {
			finishOperationSpan(span, principal, policy, "denied", err)
			return nil, err
		}
		output, err := handler(ctx, principal, input)
		if err != nil {
			finishOperationSpan(span, principal, policy, "error", err)
			return nil, err
		}
		finishOperationSpan(span, principal, policy, "allowed", nil)
		return output, nil
	})
}

func startOperationSpan(ctx context.Context, operationID string, policy sourceOperationPolicy) (context.Context, trace.Span) {
	return apiTracer.Start(ctx, policy.AuditEvent.String(), trace.WithAttributes(
		attribute.String("source.operation_id", operationID),
		attribute.String("source.permission", string(policy.Permission)),
		attribute.String("source.resource", string(policy.Resource)),
		attribute.String("source.action", string(policy.Action)),
		attribute.String("source.audit_event", string(policy.AuditEvent)),
		attribute.Bool("source.internal", policy.Internal),
	))
}

func finishOperationSpan(span trace.Span, principal source.Principal, policy sourceOperationPolicy, outcome string, err error) {
	if span == nil {
		return
	}
	if principal.OrgID != 0 {
		span.SetAttributes(attribute.Int64("verself.org_id", int64FromUint64(principal.OrgID, "org id")))
	}
	if principal.Subject != "" {
		span.SetAttributes(attribute.String("verself.subject_id", principal.Subject))
	}
	span.SetAttributes(
		attribute.String("source.outcome", outcome),
		attribute.String("source.rate_limit_class", string(policy.RateLimitClass)),
	)
	if err != nil {
		span.RecordError(err)
		if outcome != "denied" {
			span.SetStatus(codes.Error, err.Error())
		}
	}
}

func withOperationPolicy(op huma.Operation, policy sourceOperationPolicy) huma.Operation {
	if err := policy.ValidateHTTPOperation(op.Method, op.OperationID); err != nil {
		panic(err)
	}
	if policy.BodyLimitBytes > 0 {
		op.MaxBodyBytes = policy.BodyLimitBytes
	}
	if policy.Idempotency == idempotencyHeaderKey {
		op.Parameters = appendIdempotencyKeyHeaderParameter(op.Parameters)
	}
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	iam := policy.OpenAPIExtension()
	iam["internal"] = policy.Internal
	op.Extensions["x-verself-iam"] = iam
	if policy.Internal {
		op.Security = []map[string][]string{{"mutualTLS": {}}}
	} else {
		op.Security = []map[string][]string{{"bearerAuth": {}}}
	}
	return op
}

func operationRequiresBodyBudget(op huma.Operation) bool {
	return runtimeiam.OperationRequiresBodyBudget(op.Method)
}

func appendIdempotencyKeyHeaderParameter(parameters []*huma.Param) []*huma.Param {
	for _, param := range parameters {
		if param != nil && strings.EqualFold(param.Name, "Idempotency-Key") && param.In == "header" {
			param.Required = true
			return parameters
		}
	}
	minLength := 1
	maxLength := maxIdempotencyKeyLength
	return append(parameters, &huma.Param{
		Name:        "Idempotency-Key",
		In:          "header",
		Description: "Stable caller-provided key used to make this mutation retry-safe.",
		Required:    true,
		Schema:      &huma.Schema{Type: "string", MinLength: &minLength, MaxLength: &maxLength},
	})
}

func enforceOperationPolicy(ctx context.Context, authorizer runtimeiam.OperationAuthorizer, policy sourceOperationPolicy) (source.Principal, error) {
	if policy.Internal {
		peerID, ok := workloadauth.PeerIDFromContext(ctx)
		if !ok {
			return source.Principal{}, unauthorized(ctx)
		}
		return source.Principal{Subject: peerID.String()}, requireOperationIdempotency(ctx, policy)
	}
	identity := auth.FromContext(ctx)
	if identity == nil {
		return source.Principal{}, unauthorized(ctx)
	}
	orgID, err := strconv.ParseUint(strings.TrimSpace(identity.OrgID), 10, 64)
	if err != nil || orgID == 0 {
		return source.Principal{}, forbidden(ctx, "organization-required", "source routes require an organization-scoped human token")
	}
	principal := source.Principal{Subject: identity.Subject, OrgID: orgID, Email: identity.Email}
	if err := source.ValidatePrincipal(principal); err != nil {
		return principal, forbidden(ctx, "human-source-principal-required", "source routes require a human subject token")
	}
	if authorizer == nil {
		return principal, problem(ctx, http.StatusServiceUnavailable, "iam-authorizer-unavailable", "IAM authorizer unavailable", runtimeiam.ErrAuthorizerUnavailable)
	}
	decision, err := authorizer.AuthorizeOperation(ctx, identity, policy.OperationPolicy)
	if err != nil {
		return principal, problem(ctx, http.StatusServiceUnavailable, "iam-authorizer-unavailable", "IAM authorization check failed", err)
	}
	if !decision.Allowed {
		return principal, forbidden(ctx, "permission-denied", "missing required source permission")
	}
	if err := requireOperationIdempotency(ctx, policy); err != nil {
		return principal, err
	}
	return principal, nil
}

func requireOperationIdempotency(ctx context.Context, policy sourceOperationPolicy) error {
	if policy.Idempotency == runtimeiam.IdempotencyNone {
		return nil
	}
	value := strings.TrimSpace(operationRequestInfoFromContext(ctx).IdempotencyKey)
	if value == "" {
		return badRequest(ctx, "idempotency-key-required", "Idempotency-Key is required for this operation", nil)
	}
	if len(value) > maxIdempotencyKeyLength {
		return badRequest(ctx, "idempotency-key-too-long", "Idempotency-Key is too long", nil)
	}
	return nil
}

func operationRequestMiddleware(ctx huma.Context, next func(huma.Context)) {
	info := operationRequestInfo{IdempotencyKey: ctx.Header("Idempotency-Key")}
	next(huma.WithValue(ctx, operationRequestInfoKey{}, info))
}

func operationRequestInfoFromContext(ctx context.Context) operationRequestInfo {
	info, _ := ctx.Value(operationRequestInfoKey{}).(operationRequestInfo)
	return info
}
