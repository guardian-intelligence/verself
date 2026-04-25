package api

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	auth "github.com/verself/auth-middleware"
	workloadauth "github.com/verself/auth-middleware/workload"
	"github.com/verself/projects-service/internal/projects"
)

type permission string

const (
	permissionProjectRead      permission = "projects:project:read"
	permissionProjectWrite     permission = "projects:project:write"
	permissionEnvironmentRead  permission = "projects:environment:read"
	permissionEnvironmentWrite permission = "projects:environment:write"
	permissionProjectEventRead permission = "projects:event:read"
	permissionProjectResolve   permission = "projects:resolve"

	roleOwner  = "owner"
	roleAdmin  = "admin"
	roleMember = "member"

	idempotencyHeaderKey    = "idempotency_key_header"
	maxIdempotencyKeyLength = 128
	bodyLimitSmallJSON      = 64 << 10
)

var apiTracer = otel.Tracer("projects-service/internal/api")

var fullRolePermissions = []permission{
	permissionProjectRead,
	permissionProjectWrite,
	permissionEnvironmentRead,
	permissionEnvironmentWrite,
	permissionProjectEventRead,
}

var rolePermissionBundles = map[string][]permission{
	roleOwner:  fullRolePermissions,
	roleAdmin:  fullRolePermissions,
	roleMember: {permissionProjectRead, permissionEnvironmentRead},
}

type operationPolicy struct {
	Permission         permission
	Resource           string
	Action             string
	OrgScope           string
	RateLimitClass     string
	Idempotency        string
	AuditEvent         string
	OperationDisplay   string
	OperationType      string
	EventCategory      string
	RiskLevel          string
	DataClassification string
	BodyLimitBytes     int64
	Internal           bool
}

type operationRequestInfoKey struct{}

type operationRequestInfo struct {
	IdempotencyKey string
}

func registerProjectsRoute[I, O any](api huma.API, op huma.Operation, policy operationPolicy, handler func(context.Context, projects.Principal, *I) (*O, error)) {
	if op.OperationID == "" {
		panic("missing operation ID for projects API route")
	}
	policy = normalizeOperationPolicy(op.OperationID, policy)
	op = withOperationPolicy(op, policy)
	op.Middlewares = append(op.Middlewares, operationRequestMiddleware)
	huma.Register(api, op, func(ctx context.Context, input *I) (*O, error) {
		ctx, span := startOperationSpan(ctx, op.OperationID, policy)
		defer span.End()
		principal, err := enforceOperationPolicy(ctx, policy)
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

func startOperationSpan(ctx context.Context, operationID string, policy operationPolicy) (context.Context, trace.Span) {
	return apiTracer.Start(ctx, policy.AuditEvent, trace.WithAttributes(
		attribute.String("projects.operation_id", operationID),
		attribute.String("projects.permission", string(policy.Permission)),
		attribute.String("projects.resource", policy.Resource),
		attribute.String("projects.action", policy.Action),
		attribute.String("projects.audit_event", policy.AuditEvent),
		attribute.Bool("projects.internal", policy.Internal),
	))
}

func finishOperationSpan(span trace.Span, principal projects.Principal, policy operationPolicy, outcome string, err error) {
	if span == nil {
		return
	}
	if principal.OrgID != 0 {
		span.SetAttributes(attribute.Int64("verself.org_id", int64(principal.OrgID)))
	}
	if principal.Subject != "" {
		span.SetAttributes(attribute.String("verself.subject_id", principal.Subject))
	}
	span.SetAttributes(
		attribute.String("projects.outcome", outcome),
		attribute.String("projects.rate_limit_class", policy.RateLimitClass),
	)
	if err != nil {
		span.RecordError(err)
		if outcome != "denied" {
			span.SetStatus(codes.Error, err.Error())
		}
	}
}

func normalizeOperationPolicy(operationID string, policy operationPolicy) operationPolicy {
	if policy.OperationDisplay == "" {
		policy.OperationDisplay = operationID
	}
	if policy.OperationType == "" {
		policy.OperationType = "write"
	}
	if policy.EventCategory == "" {
		policy.EventCategory = "projects"
	}
	if policy.RiskLevel == "" {
		policy.RiskLevel = "medium"
	}
	if policy.DataClassification == "" {
		policy.DataClassification = "customer_project_metadata"
	}
	return policy
}

func withOperationPolicy(op huma.Operation, policy operationPolicy) huma.Operation {
	if policy.Permission == "" || policy.Resource == "" || policy.Action == "" || policy.OrgScope == "" || policy.RateLimitClass == "" || policy.AuditEvent == "" {
		panic("incomplete projects operation policy for " + op.OperationID)
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
		"permission":          string(policy.Permission),
		"resource":            policy.Resource,
		"action":              policy.Action,
		"org_scope":           policy.OrgScope,
		"rate_limit_class":    policy.RateLimitClass,
		"audit_event":         policy.AuditEvent,
		"operation_display":   policy.OperationDisplay,
		"operation_type":      policy.OperationType,
		"event_category":      policy.EventCategory,
		"risk_level":          policy.RiskLevel,
		"data_classification": policy.DataClassification,
	}
	if policy.Internal {
		op.Security = []map[string][]string{{"mutualTLS": {}}}
	} else {
		op.Security = []map[string][]string{{"bearerAuth": {}}}
	}
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

func enforceOperationPolicy(ctx context.Context, policy operationPolicy) (projects.Principal, error) {
	if policy.Internal {
		peerID, ok := workloadauth.PeerIDFromContext(ctx)
		if !ok {
			return projects.Principal{}, unauthorized(ctx)
		}
		return projects.Principal{Subject: peerID.String()}, requireOperationIdempotency(ctx, policy)
	}
	identity := auth.FromContext(ctx)
	if identity == nil {
		return projects.Principal{}, unauthorized(ctx)
	}
	orgID, err := strconv.ParseUint(strings.TrimSpace(identity.OrgID), 10, 64)
	if err != nil || orgID == 0 {
		return projects.Principal{}, forbidden(ctx, "organization-required", "projects routes require an organization-scoped token")
	}
	principal := projects.Principal{Subject: identity.Subject, OrgID: orgID, Email: identity.Email}
	if err := projects.ValidatePrincipal(principal); err != nil {
		return principal, forbidden(ctx, "projects-principal-required", "projects routes require a subject token")
	}
	if !identityHasPermission(identity, policy.Permission) {
		return principal, forbidden(ctx, "permission-denied", "missing required projects permission")
	}
	if err := requireOperationIdempotency(ctx, policy); err != nil {
		return principal, err
	}
	return principal, nil
}

func requireOperationIdempotency(ctx context.Context, policy operationPolicy) error {
	if policy.Idempotency == "" {
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

func identityHasPermission(identity *auth.Identity, required permission) bool {
	if identity == nil || required == "" {
		return false
	}
	if identityHasDirectPermission(identity, required) {
		return true
	}
	for _, role := range identityRolesForCurrentOrg(identity) {
		for _, granted := range rolePermissionBundles[role] {
			if granted == required {
				return true
			}
		}
	}
	return false
}

func identityHasDirectPermission(identity *auth.Identity, required permission) bool {
	if identity == nil || strings.TrimSpace(identity.OrgID) == "" {
		return false
	}
	credentialID, _ := identity.Raw["verself:credential_id"].(string)
	if strings.TrimSpace(credentialID) == "" {
		return false
	}
	requiredText := string(required)
	for _, claimKey := range []string{"permissions", "permission"} {
		for _, value := range stringClaimList(identity.Raw[claimKey]) {
			if value == requiredText {
				return true
			}
		}
	}
	return false
}

func identityRolesForCurrentOrg(identity *auth.Identity) []string {
	if identity == nil || identity.OrgID == "" || len(identity.RoleAssignments) == 0 {
		return nil
	}
	roles := make([]string, 0, len(identity.RoleAssignments))
	for _, assignment := range identity.RoleAssignments {
		if assignment.OrganizationID == identity.OrgID && assignment.Role != "" {
			roles = append(roles, assignment.Role)
		}
	}
	sort.Strings(roles)
	return compactStrings(roles)
}

func stringClaimList(value any) []string {
	switch typed := value.(type) {
	case string:
		return strings.Fields(typed)
	case []string:
		out := append([]string(nil), typed...)
		sort.Strings(out)
		return compactStrings(out)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, stringClaimList(item)...)
		}
		sort.Strings(out)
		return compactStrings(out)
	default:
		return nil
	}
}

func compactStrings(values []string) []string {
	out := values[:0]
	var previous string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || value == previous {
			continue
		}
		out = append(out, value)
		previous = value
	}
	return out
}
