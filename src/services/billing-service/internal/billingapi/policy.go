package billingapi

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

	"github.com/verself/billing-service/internal/billing"
	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
)

type permission string

const (
	permissionBillingRead     permission = "billing:read"
	permissionBillingCheckout permission = "billing:checkout"

	idempotencyHeaderKey    = "idempotency_key_header"
	maxIdempotencyKeyLength = 128
	bodyLimitSmallJSON      = 64 << 10
)

var apiTracer = otel.Tracer("billing-service/internal/billingapi")

type operationPolicy struct {
	Permission     permission
	Resource       string
	Action         string
	RateLimitClass string
	Idempotency    string
	AuditEvent     string
	BodyLimitBytes int64
}

type operationRequestInfoKey struct{}

type operationRequestInfo struct {
	IdempotencyKey string
}

func readPolicy(resource, action, auditEvent string) operationPolicy {
	return operationPolicy{
		Permission:     permissionBillingRead,
		Resource:       resource,
		Action:         action,
		RateLimitClass: "read",
		AuditEvent:     auditEvent,
		BodyLimitBytes: 0,
	}
}

func checkoutPolicy(resource, action, auditEvent string) operationPolicy {
	return operationPolicy{
		Permission:     permissionBillingCheckout,
		Resource:       resource,
		Action:         action,
		RateLimitClass: "billing_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     auditEvent,
		BodyLimitBytes: bodyLimitSmallJSON,
	}
}

func registerPublicBillingRoute[I, O any](api huma.API, authorizer runtimeiam.OperationAuthorizer, op huma.Operation, policy operationPolicy, handler func(context.Context, billing.OrgID, *I) (*O, error)) {
	if op.OperationID == "" {
		panic("missing operation ID for billing API route")
	}
	op = withOperationPolicy(op, policy)
	op.Middlewares = append(op.Middlewares, operationRequestMiddleware)
	huma.Register(api, op, func(ctx context.Context, input *I) (*O, error) {
		ctx, span := startOperationSpan(ctx, op.OperationID, policy)
		defer span.End()
		orgID, err := enforceOperationPolicy(ctx, authorizer, policy)
		if err != nil {
			finishOperationSpan(span, orgID, policy, "denied", err)
			return nil, err
		}
		output, err := handler(ctx, orgID, input)
		if err != nil {
			finishOperationSpan(span, orgID, policy, "error", err)
			return nil, err
		}
		finishOperationSpan(span, orgID, policy, "allowed", nil)
		return output, nil
	})
}

func startOperationSpan(ctx context.Context, operationID string, policy operationPolicy) (context.Context, trace.Span) {
	return apiTracer.Start(ctx, policy.AuditEvent, trace.WithAttributes(
		attribute.String("billing.operation_id", operationID),
		attribute.String("billing.permission", string(policy.Permission)),
		attribute.String("billing.resource", policy.Resource),
		attribute.String("billing.action", policy.Action),
		attribute.String("billing.audit_event", policy.AuditEvent),
	))
}

func finishOperationSpan(span trace.Span, orgID billing.OrgID, policy operationPolicy, outcome string, err error) {
	if span == nil {
		return
	}
	if orgID != 0 {
		span.SetAttributes(attribute.String("verself.org_id", strconv.FormatUint(uint64(orgID), 10)))
	}
	span.SetAttributes(
		attribute.String("billing.outcome", outcome),
		attribute.String("billing.rate_limit_class", policy.RateLimitClass),
	)
	if err != nil {
		span.RecordError(err)
		if outcome != "denied" {
			span.SetStatus(codes.Error, err.Error())
		}
	}
}

func withOperationPolicy(op huma.Operation, policy operationPolicy) huma.Operation {
	if policy.Permission == "" || policy.Resource == "" || policy.Action == "" || policy.RateLimitClass == "" || policy.AuditEvent == "" {
		panic("incomplete billing operation policy for " + op.OperationID)
	}
	if operationRequiresBodyBudget(op) && policy.BodyLimitBytes <= 0 {
		panic("missing body limit for mutating operation " + op.OperationID)
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
	op.Extensions["x-verself-iam"] = map[string]any{
		"permission":       string(policy.Permission),
		"resource":         policy.Resource,
		"action":           policy.Action,
		"org_scope":        "token_org_id",
		"rate_limit_class": policy.RateLimitClass,
		"audit_event":      policy.AuditEvent,
	}
	if policy.Idempotency != "" {
		op.Extensions["x-verself-iam"].(map[string]any)["idempotency"] = policy.Idempotency
	}
	if policy.BodyLimitBytes > 0 {
		op.Extensions["x-verself-iam"].(map[string]any)["request_body_max_bytes"] = policy.BodyLimitBytes
	}
	op.Security = []map[string][]string{{"bearerAuth": {}}}
	return op
}

func operationRequiresBodyBudget(op huma.Operation) bool {
	switch op.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
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

func enforceOperationPolicy(ctx context.Context, authorizer runtimeiam.OperationAuthorizer, policy operationPolicy) (billing.OrgID, error) {
	identity := auth.FromContext(ctx)
	if identity == nil {
		return 0, problem(ctx, http.StatusUnauthorized, "unauthorized", "authentication required", nil)
	}
	orgID, err := strconv.ParseUint(strings.TrimSpace(identity.OrgID), 10, 64)
	if err != nil || orgID == 0 {
		return 0, problem(ctx, http.StatusForbidden, "organization-required", "billing routes require an organization-scoped token", err)
	}
	if authorizer == nil {
		return billing.OrgID(orgID), problem(ctx, http.StatusServiceUnavailable, "iam-authorizer-unavailable", "IAM authorizer unavailable", runtimeiam.ErrAuthorizerUnavailable)
	}
	decision, err := authorizer.AuthorizeOperation(ctx, identity, string(policy.Permission))
	if err != nil {
		return billing.OrgID(orgID), problem(ctx, http.StatusServiceUnavailable, "iam-authorizer-unavailable", "IAM authorization check failed", err)
	}
	if !decision.Allowed {
		return billing.OrgID(orgID), problem(ctx, http.StatusForbidden, "permission-denied", "missing required billing permission", nil)
	}
	if err := requireOperationIdempotency(ctx, policy); err != nil {
		return billing.OrgID(orgID), err
	}
	return billing.OrgID(orgID), nil
}

func requireOperationIdempotency(ctx context.Context, policy operationPolicy) error {
	if policy.Idempotency == "" {
		return nil
	}
	value := strings.TrimSpace(operationRequestInfoFromContext(ctx).IdempotencyKey)
	if value == "" {
		return problem(ctx, http.StatusBadRequest, "idempotency-key-required", "Idempotency-Key is required for this operation", nil)
	}
	if len(value) > maxIdempotencyKeyLength {
		return problem(ctx, http.StatusBadRequest, "idempotency-key-too-long", "Idempotency-Key is too long", nil)
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
