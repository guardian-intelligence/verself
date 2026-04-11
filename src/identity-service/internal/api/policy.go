package api

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	auth "github.com/forge-metal/auth-middleware"
	"github.com/forge-metal/identity-service/internal/identity"
)

type permission string

const (
	permissionOrganizationRead permission = identity.PermissionOrganizationRead
	permissionMemberRead       permission = identity.PermissionMemberRead
	permissionMemberInvite     permission = identity.PermissionMemberInvite
	permissionMemberRolesWrite permission = identity.PermissionMemberRolesWrite
	permissionPolicyRead       permission = identity.PermissionPolicyRead
	permissionPolicyWrite      permission = identity.PermissionPolicyWrite
	permissionOperationsRead   permission = identity.PermissionOperationsRead

	idempotencyHeaderKey        = "idempotency_key_header"
	maxIdempotencyKeyLength     = 128
	rateLimiterMaxWindowEntries = 10000

	bodyLimitNoBody    int64 = 1024
	bodyLimitSmallJSON int64 = 16 << 10
)

type operationPolicy struct {
	Permission     permission
	Resource       string
	Action         string
	OrgScope       string
	RateLimitClass string
	Idempotency    string
	AuditEvent     string
	BodyLimitBytes int64
}

type securedOperation struct {
	Operation huma.Operation
	Policy    operationPolicy
}

func secured(op huma.Operation, policy operationPolicy) securedOperation {
	return securedOperation{Operation: op, Policy: policy}
}

func registerSecured[I, O any](api huma.API, svc *identity.Service, securedOp securedOperation, handler func(context.Context, *I) (*O, error)) {
	op := securedOp.Operation
	policy := securedOp.Policy
	if op.OperationID == "" {
		panic("missing operation ID for secured public API route")
	}
	if !strings.HasPrefix(op.Path, "/api/") {
		panic("secured public API route must live under /api/: " + op.OperationID)
	}
	op = withOperationPolicy(op, policy)
	op.Middlewares = append(op.Middlewares, operationRequestMiddleware)
	huma.Register(api, op, func(ctx context.Context, input *I) (*O, error) {
		identity, err := enforceOperationPolicy(ctx, svc, policy)
		if err != nil {
			auditOperation(ctx, policy, identity, "denied", err)
			return nil, err
		}
		output, err := handler(ctx, input)
		if err != nil {
			auditOperation(ctx, policy, identity, "error", err)
			return nil, err
		}
		auditOperation(ctx, policy, identity, "allowed", nil)
		return output, nil
	})
}

func withOperationPolicy(op huma.Operation, policy operationPolicy) huma.Operation {
	if policy.Permission == "" {
		panic("empty IAM permission for operation: " + op.OperationID)
	}
	if policy.Resource == "" {
		panic("empty IAM resource for operation: " + op.OperationID)
	}
	if policy.Action == "" {
		panic("empty IAM action for operation: " + op.OperationID)
	}
	if policy.OrgScope == "" {
		panic("empty IAM org scope for operation: " + op.OperationID)
	}
	if policy.RateLimitClass == "" {
		panic("empty rate limit class for operation: " + op.OperationID)
	}
	if policy.AuditEvent == "" {
		panic("empty audit event for operation: " + op.OperationID)
	}
	if operationRequiresBodyBudget(op) && policy.BodyLimitBytes <= 0 {
		panic("empty request body limit for mutating operation: " + op.OperationID)
	}
	if policy.BodyLimitBytes > 0 {
		op.MaxBodyBytes = policy.BodyLimitBytes
	}
	switch policy.Idempotency {
	case "":
	case idempotencyHeaderKey:
		op.Parameters = appendIdempotencyKeyHeaderParameter(op.Parameters)
	default:
		panic("unsupported idempotency policy for operation " + op.OperationID + ": " + policy.Idempotency)
	}
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	iam := map[string]any{
		"permission":       string(policy.Permission),
		"resource":         policy.Resource,
		"action":           policy.Action,
		"org_scope":        policy.OrgScope,
		"rate_limit_class": policy.RateLimitClass,
		"audit_event":      policy.AuditEvent,
	}
	if policy.Idempotency != "" {
		iam["idempotency"] = policy.Idempotency
	}
	if policy.BodyLimitBytes > 0 {
		iam["request_body_max_bytes"] = policy.BodyLimitBytes
	}
	op.Extensions["x-forge-metal-iam"] = iam
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
		Schema: &huma.Schema{
			Type:      "string",
			MinLength: &minLength,
			MaxLength: &maxLength,
		},
	})
}

func enforceOperationPolicy(ctx context.Context, svc *identity.Service, policy operationPolicy) (*auth.Identity, error) {
	identity, err := requireIdentity(ctx)
	if err != nil {
		return nil, err
	}
	allowed, err := identityHasPermission(ctx, svc, identity, policy.Permission)
	if err != nil {
		return identity, identityError(ctx, err)
	}
	if !allowed {
		return identity, forbidden(ctx, "permission-denied", fmt.Sprintf("missing required permission %q", policy.Permission))
	}
	if err := requireOperationIdempotency(ctx, policy); err != nil {
		return identity, err
	}
	if decision := apiOperationRateLimiter.allow(policy.RateLimitClass, operationRateLimitKey(ctx, identity, policy), time.Now()); !decision.Allowed {
		return identity, rateLimitExceeded(ctx, decision.RetryAfter)
	}
	return identity, nil
}

func identityHasPermission(ctx context.Context, svc *identity.Service, authIdentity *auth.Identity, required permission) (bool, error) {
	if authIdentity == nil || required == "" {
		return false, nil
	}
	if identityHasDirectPermission(authIdentity, required) {
		return true, nil
	}
	if svc == nil {
		return false, identity.ErrStoreUnavailable
	}
	principal, err := principalFromAuthIdentity(ctx, authIdentity)
	if err != nil {
		return false, err
	}
	policy, err := svc.Policy(ctx, principal)
	if err != nil {
		return false, err
	}
	roles, err := svc.EffectiveRoleKeys(ctx, principal)
	if err != nil {
		return false, err
	}
	for _, granted := range identity.PermissionsForRoles(policy, roles) {
		if granted == string(required) {
			return true, nil
		}
	}
	return false, nil
}

type operationRequestInfoKey struct{}

type operationRequestInfo struct {
	ClientIP       string
	IdempotencyKey string
}

func operationRequestMiddleware(ctx huma.Context, next func(huma.Context)) {
	info := operationRequestInfo{
		ClientIP:       clientIPFromHuma(ctx),
		IdempotencyKey: strings.TrimSpace(ctx.Header("Idempotency-Key")),
	}
	next(huma.WithValue(ctx, operationRequestInfoKey{}, info))
}

func requireOperationIdempotency(ctx context.Context, policy operationPolicy) error {
	if policy.Idempotency == "" {
		return nil
	}
	value := operationRequestInfoFromContext(ctx).IdempotencyKey
	value = strings.TrimSpace(value)
	if value == "" {
		return badRequest(ctx, "idempotency-key-required", "Idempotency-Key is required for this operation", nil)
	}
	if len(value) > maxIdempotencyKeyLength {
		return badRequest(ctx, "idempotency-key-too-long", "Idempotency-Key is too long", nil)
	}
	if strings.ContainsAny(value, "\x00\r\n\t") {
		return badRequest(ctx, "idempotency-key-invalid", "Idempotency-Key contains unsupported characters", nil)
	}
	return nil
}

func operationRequestInfoFromContext(ctx context.Context) operationRequestInfo {
	info, _ := ctx.Value(operationRequestInfoKey{}).(operationRequestInfo)
	return info
}

func operationRateLimitKey(ctx context.Context, identity *auth.Identity, policy operationPolicy) string {
	info := operationRequestInfoFromContext(ctx)
	return strings.Join([]string{
		policy.RateLimitClass,
		string(policy.Permission),
		identity.OrgID,
		identity.Subject,
		info.ClientIP,
	}, "\x00")
}

func clientIPFromHuma(ctx huma.Context) string {
	for _, header := range []string{"CF-Connecting-IP", "X-Real-IP", "X-Forwarded-For"} {
		value := strings.TrimSpace(ctx.Header(header))
		if value == "" {
			continue
		}
		if header == "X-Forwarded-For" {
			value = strings.TrimSpace(strings.Split(value, ",")[0])
		}
		if value != "" {
			return value
		}
	}
	remote := strings.TrimSpace(ctx.RemoteAddr())
	if host, _, err := net.SplitHostPort(remote); err == nil {
		return host
	}
	return remote
}

func auditOperation(ctx context.Context, policy operationPolicy, identity *auth.Identity, outcome string, err error) {
	args := []any{
		"audit_event", policy.AuditEvent,
		"operation_permission", policy.Permission,
		"operation_resource", policy.Resource,
		"operation_action", policy.Action,
		"rate_limit_class", policy.RateLimitClass,
		"outcome", outcome,
	}
	if identity != nil {
		args = append(args, "subject", identity.Subject, "org_id", identity.OrgID)
	}
	if err != nil {
		args = append(args, "error", err.Error())
	}
	slog.Default().InfoContext(ctx, "identity api operation", args...)
}

type rateLimitRule struct {
	Limit  int
	Window time.Duration
}

type rateLimitDecision struct {
	Allowed    bool
	RetryAfter time.Duration
}

type rateLimitWindow struct {
	ResetAt time.Time
	Count   int
}

type fixedWindowOperationRateLimiter struct {
	mu      sync.Mutex
	rules   map[string]rateLimitRule
	windows map[string]rateLimitWindow
}

var apiOperationRateLimiter = newFixedWindowOperationRateLimiter(map[string]rateLimitRule{
	"read":            {Limit: 600, Window: time.Minute},
	"member_mutation": {Limit: 60, Window: time.Minute},
	"policy_mutation": {Limit: 30, Window: time.Minute},
})

func newFixedWindowOperationRateLimiter(rules map[string]rateLimitRule) *fixedWindowOperationRateLimiter {
	copied := make(map[string]rateLimitRule, len(rules))
	for class, rule := range rules {
		copied[class] = rule
	}
	return &fixedWindowOperationRateLimiter{rules: copied, windows: map[string]rateLimitWindow{}}
}

func (l *fixedWindowOperationRateLimiter) allow(class, key string, now time.Time) rateLimitDecision {
	if l == nil || class == "" {
		return rateLimitDecision{Allowed: true}
	}
	rule, ok := l.rules[class]
	if !ok || rule.Limit <= 0 || rule.Window <= 0 {
		return rateLimitDecision{Allowed: true}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.windows) > rateLimiterMaxWindowEntries {
		l.pruneExpired(now)
	}
	key = class + "\x00" + key
	window := l.windows[key]
	if window.ResetAt.IsZero() || !now.Before(window.ResetAt) {
		l.windows[key] = rateLimitWindow{ResetAt: now.Add(rule.Window), Count: 1}
		return rateLimitDecision{Allowed: true}
	}
	if window.Count >= rule.Limit {
		return rateLimitDecision{Allowed: false, RetryAfter: window.ResetAt.Sub(now).Round(time.Second)}
	}
	window.Count++
	l.windows[key] = window
	return rateLimitDecision{Allowed: true}
}

func (l *fixedWindowOperationRateLimiter) pruneExpired(now time.Time) {
	for key, window := range l.windows {
		if !now.Before(window.ResetAt) {
			delete(l.windows, key)
		}
	}
}

func rateLimitExceeded(ctx context.Context, retryAfter time.Duration) error {
	err := problem(ctx, http.StatusTooManyRequests, "rate-limit-exceeded", "rate limit exceeded", nil)
	if retryAfter <= 0 {
		return err
	}
	headers := http.Header{}
	headers.Set("Retry-After", strconv.FormatInt(int64(retryAfter.Seconds()), 10))
	return huma.ErrorWithHeaders(err, headers)
}

func identityRolesForCurrentOrg(identity *auth.Identity) []string {
	if identity == nil {
		return nil
	}
	if len(identity.RoleAssignments) > 0 && identity.OrgID != "" {
		roles := make([]string, 0, len(identity.RoleAssignments))
		for _, assignment := range identity.RoleAssignments {
			if assignment.OrganizationID == identity.OrgID && assignment.Role != "" {
				roles = append(roles, assignment.Role)
			}
		}
		sort.Strings(roles)
		return compactStrings(roles)
	}
	roles := append([]string(nil), identity.Roles...)
	sort.Strings(roles)
	return compactStrings(roles)
}

func identityHasDirectPermission(identity *auth.Identity, required permission) bool {
	requiredText := string(required)
	for _, claimKey := range []string{"scope", "scp", "permissions", "permission"} {
		for _, value := range stringClaimList(identity.Raw[claimKey]) {
			if value == requiredText {
				return true
			}
		}
	}
	return false
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

func applyPublicAPISecurityScheme(api huma.API) {
	openapi := api.OpenAPI()
	if openapi.Components == nil {
		openapi.Components = &huma.Components{}
	}
	if openapi.Components.SecuritySchemes == nil {
		openapi.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	openapi.Components.SecuritySchemes["bearerAuth"] = &huma.SecurityScheme{
		Type:         "http",
		Scheme:       "bearer",
		BearerFormat: "JWT",
		Description:  "Zitadel-issued bearer token scoped to the identity-service API audience.",
	}
}
