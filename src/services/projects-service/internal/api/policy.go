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

	"github.com/verself/projects-service/internal/projects"
	auth "github.com/verself/service-runtime/auth"
	workloadauth "github.com/verself/service-runtime/workload"
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
	orgScopeTokenOrgID      = "token_org_id"
	orgScopeRequestOrgID    = "request_org_id"
	maxIdempotencyKeyLength = 128
	maxOriginSubjectLength  = 256
	maxOriginEmailLength    = 320
	bodyLimitSmallJSON      = 64 << 10

	originOrgIDHeader   = "X-Verself-Origin-Org-ID"
	originSubjectHeader = "X-Verself-Origin-Subject"
	originEmailHeader   = "X-Verself-Origin-Email"
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
	InternalPeers      []string
}

type operationRequestInfoKey struct{}

type operationRequestInfo struct {
	IdempotencyKey string
	OriginOrgID    string
	OriginSubject  string
	OriginEmail    string
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
		span.SetAttributes(attribute.Int64("verself.org_id", int64FromUint64(principal.OrgID, "org id")))
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
	if policy.Internal && len(policy.InternalPeers) == 0 {
		panic("missing internal peer allowlist for " + op.OperationID)
	}
	if policy.BodyLimitBytes > 0 {
		op.MaxBodyBytes = policy.BodyLimitBytes
	}
	if policy.Idempotency == idempotencyHeaderKey {
		op.Parameters = appendIdempotencyKeyHeaderParameter(op.Parameters)
	}
	if policy.Internal && policy.OrgScope == orgScopeTokenOrgID {
		op.Parameters = appendOriginHeaderParameters(op.Parameters)
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
	if policy.Idempotency != "" {
		op.Extensions["x-verself-iam"].(map[string]any)["idempotency"] = policy.Idempotency
	}
	if policy.BodyLimitBytes > 0 {
		op.Extensions["x-verself-iam"].(map[string]any)["request_body_max_bytes"] = policy.BodyLimitBytes
	}
	if policy.Internal && policy.OrgScope == orgScopeTokenOrgID {
		op.Extensions["x-verself-origin"] = map[string]any{
			"org_id_header":  originOrgIDHeader,
			"subject_header": originSubjectHeader,
			"email_header":   originEmailHeader,
		}
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

func appendOriginHeaderParameters(parameters []*huma.Param) []*huma.Param {
	parameters = appendRequiredStringHeader(parameters, originOrgIDHeader, "Origin organization ID asserted by the SPIFFE-authenticated caller.", 1, 32)
	parameters = appendRequiredStringHeader(parameters, originSubjectHeader, "Origin subject ID asserted by the SPIFFE-authenticated caller.", 1, maxOriginSubjectLength)
	return appendOptionalStringHeader(parameters, originEmailHeader, "Origin subject email asserted by the SPIFFE-authenticated caller.", maxOriginEmailLength)
}

func appendRequiredStringHeader(parameters []*huma.Param, name, description string, minLength, maxLength int) []*huma.Param {
	for _, param := range parameters {
		if param != nil && strings.EqualFold(param.Name, name) && param.In == "header" {
			param.Required = true
			return parameters
		}
	}
	return append(parameters, &huma.Param{
		Name:        name,
		In:          "header",
		Description: description,
		Required:    true,
		Schema:      &huma.Schema{Type: "string", MinLength: &minLength, MaxLength: &maxLength},
	})
}

func appendOptionalStringHeader(parameters []*huma.Param, name, description string, maxLength int) []*huma.Param {
	for _, param := range parameters {
		if param != nil && strings.EqualFold(param.Name, name) && param.In == "header" {
			return parameters
		}
	}
	return append(parameters, &huma.Param{
		Name:        name,
		In:          "header",
		Description: description,
		Required:    false,
		Schema:      &huma.Schema{Type: "string", MaxLength: &maxLength},
	})
}

func enforceOperationPolicy(ctx context.Context, policy operationPolicy) (projects.Principal, error) {
	if policy.Internal {
		peerID, ok := workloadauth.PeerIDFromContext(ctx)
		if !ok {
			return projects.Principal{}, unauthorized(ctx)
		}
		if !internalPeerAllowed(peerID.String(), policy.InternalPeers) {
			return projects.Principal{Subject: peerID.String()}, forbidden(ctx, "internal-peer-denied", "SPIFFE peer is not allowed to call this projects operation")
		}
		if policy.OrgScope == orgScopeTokenOrgID {
			principal, err := principalFromInternalOrigin(ctx)
			if err != nil {
				return principal, err
			}
			if err := requireOperationIdempotency(ctx, policy); err != nil {
				return principal, err
			}
			return principal, nil
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

func principalFromInternalOrigin(ctx context.Context) (projects.Principal, error) {
	info := operationRequestInfoFromContext(ctx)
	orgID, err := strconv.ParseUint(strings.TrimSpace(info.OriginOrgID), 10, 64)
	if err != nil || orgID == 0 {
		return projects.Principal{}, badRequest(ctx, "origin-organization-required", originOrgIDHeader+" must be a non-zero decimal organization ID", err)
	}
	principal := projects.Principal{
		Subject: strings.TrimSpace(info.OriginSubject),
		OrgID:   orgID,
		Email:   strings.TrimSpace(info.OriginEmail),
	}
	if principal.Subject == "" {
		return principal, badRequest(ctx, "origin-subject-required", originSubjectHeader+" is required for SPIFFE-authenticated org-scoped operations", nil)
	}
	if len(principal.Subject) > maxOriginSubjectLength {
		return principal, badRequest(ctx, "origin-subject-too-long", originSubjectHeader+" is too long", nil)
	}
	if len(principal.Email) > maxOriginEmailLength {
		return principal, badRequest(ctx, "origin-email-too-long", originEmailHeader+" is too long", nil)
	}
	if err := projects.ValidatePrincipal(principal); err != nil {
		return principal, forbidden(ctx, "projects-origin-principal-required", "SPIFFE-authenticated org-scoped operations require an origin subject")
	}
	return principal, nil
}

func internalPeerAllowed(peerID string, allowedServices []string) bool {
	if len(allowedServices) == 0 {
		return false
	}
	for _, service := range allowedServices {
		if strings.HasSuffix(peerID, "/svc/"+service) {
			return true
		}
	}
	return false
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
	info := operationRequestInfo{
		IdempotencyKey: ctx.Header("Idempotency-Key"),
		OriginOrgID:    ctx.Header(originOrgIDHeader),
		OriginSubject:  ctx.Header(originSubjectHeader),
		OriginEmail:    ctx.Header(originEmailHeader),
	}
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
