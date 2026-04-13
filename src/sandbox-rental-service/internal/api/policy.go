package api

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	auth "github.com/forge-metal/auth-middleware"
)

type permission string

const (
	permissionRepoRead        permission = "sandbox:repo:read"
	permissionRepoWrite       permission = "sandbox:repo:write"
	permissionWebhookRead     permission = "sandbox:webhook_endpoint:read"
	permissionWebhookWrite    permission = "sandbox:webhook_endpoint:write"
	permissionGitHubRead      permission = "sandbox:github_installation:read"
	permissionGitHubWrite     permission = "sandbox:github_installation:write"
	permissionExecutionSubmit permission = "sandbox:execution:submit"
	permissionExecutionRead   permission = "sandbox:execution:read"
	permissionLogsRead        permission = "sandbox:logs:read"
	permissionBillingRead     permission = "billing:read"
	permissionBillingCheckout permission = "billing:checkout"

	roleSandboxOrgAdmin  = "sandbox_org_admin"
	roleSandboxOrgMember = "sandbox_org_member"

	idempotencyRequestBodyKey   = "request_body_idempotency_key"
	idempotencyHeaderKey        = "idempotency_key_header"
	maxIdempotencyKeyLength     = 128
	rateLimiterMaxWindowEntries = 10000

	bodyLimitNoBody        int64 = 1024
	bodyLimitSmallJSON     int64 = 8 << 10
	bodyLimitRepoImport    int64 = 32 << 10
	bodyLimitExecutionPost int64 = 64 << 10
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

var rolePermissionBundles = map[string][]permission{
	roleSandboxOrgAdmin: {
		permissionRepoRead,
		permissionRepoWrite,
		permissionWebhookRead,
		permissionWebhookWrite,
		permissionGitHubRead,
		permissionGitHubWrite,
		permissionExecutionSubmit,
		permissionExecutionRead,
		permissionLogsRead,
		permissionBillingRead,
		permissionBillingCheckout,
	},
	roleSandboxOrgMember: {
		permissionRepoRead,
		permissionRepoWrite,
		permissionExecutionSubmit,
		permissionExecutionRead,
		permissionLogsRead,
	},
}

func registerSecured[I, O any](api huma.API, securedOp securedOperation, handler func(context.Context, *I) (*O, error)) {
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
		identity, err := enforceOperationPolicy(ctx, policy, input)
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
	case "", idempotencyRequestBodyKey:
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
		if param == nil {
			continue
		}
		if strings.EqualFold(param.Name, "Idempotency-Key") && param.In == "header" {
			param.Required = true
			return parameters
		}
	}
	return append(parameters, idempotencyKeyHeaderParameter())
}

func idempotencyKeyHeaderParameter() *huma.Param {
	minLength := 1
	maxLength := maxIdempotencyKeyLength
	return &huma.Param{
		Name:        "Idempotency-Key",
		In:          "header",
		Description: "Stable caller-provided key used to make this mutation retry-safe.",
		Required:    true,
		Schema: &huma.Schema{
			Type:      "string",
			MinLength: &minLength,
			MaxLength: &maxLength,
		},
	}
}

func enforceOperationPolicy(ctx context.Context, policy operationPolicy, input any) (*auth.Identity, error) {
	identity, err := requireIdentity(ctx)
	if err != nil {
		return nil, err
	}
	if !identityHasPermission(identity, policy.Permission) {
		return identity, forbidden(ctx, "permission-denied", fmt.Sprintf("missing required permission %q", policy.Permission))
	}
	if err := requireOperationIdempotency(ctx, policy, input); err != nil {
		return identity, err
	}
	if decision := apiOperationRateLimiter.allow(policy.RateLimitClass, operationRateLimitKey(ctx, identity, policy), time.Now()); !decision.Allowed {
		return identity, rateLimitExceeded(ctx, decision.RetryAfter)
	}
	return identity, nil
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

func requireOperationIdempotency(ctx context.Context, policy operationPolicy, input any) error {
	switch policy.Idempotency {
	case "":
		return nil
	case idempotencyRequestBodyKey:
		return validateIdempotencyValue(ctx, "idempotency_key", nestedStringField(input, "Body", "IdempotencyKey"))
	case idempotencyHeaderKey:
		return validateIdempotencyValue(ctx, "Idempotency-Key", operationRequestInfoFromContext(ctx).IdempotencyKey)
	default:
		panic("unsupported idempotency policy: " + policy.Idempotency)
	}
}

func validateIdempotencyValue(ctx context.Context, field, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return badRequest(ctx, "idempotency-key-required", field+" is required for this operation", nil)
	}
	if len(value) > maxIdempotencyKeyLength {
		return badRequest(ctx, "idempotency-key-too-long", field+" is too long", nil)
	}
	if strings.ContainsAny(value, "\x00\r\n\t") {
		return badRequest(ctx, "idempotency-key-invalid", field+" contains unsupported characters", nil)
	}
	return nil
}

func nestedStringField(value any, path ...string) string {
	current := reflectValue(value)
	for _, name := range path {
		if !current.IsValid() || current.Kind() != reflect.Struct {
			return ""
		}
		field := current.FieldByName(name)
		if !field.IsValid() || !field.CanInterface() {
			return ""
		}
		current = reflectValue(field.Interface())
	}
	if !current.IsValid() || current.Kind() != reflect.String {
		return ""
	}
	return current.String()
}

func reflectValue(value any) reflect.Value {
	current := reflect.ValueOf(value)
	for current.IsValid() && (current.Kind() == reflect.Pointer || current.Kind() == reflect.Interface) {
		if current.IsNil() {
			return reflect.Value{}
		}
		current = current.Elem()
	}
	return current
}

func operationRequestInfoFromContext(ctx context.Context) operationRequestInfo {
	info, _ := ctx.Value(operationRequestInfoKey{}).(operationRequestInfo)
	return info
}

func operationRateLimitKey(ctx context.Context, identity *auth.Identity, policy operationPolicy) string {
	info := operationRequestInfoFromContext(ctx)
	parts := []string{
		policy.RateLimitClass,
		string(policy.Permission),
		identity.OrgID,
		identity.Subject,
		info.ClientIP,
	}
	return strings.Join(parts, "\x00")
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
		args = append(args,
			"subject", identity.Subject,
			"org_id", identity.OrgID,
		)
	}
	if err != nil {
		args = append(args, "error", err.Error())
	}
	slog.Default().InfoContext(ctx, "sandbox-rental api operation", args...)
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
	"read":                      {Limit: 600, Window: time.Minute},
	"logs_read":                 {Limit: 120, Window: time.Minute},
	"repo_mutation":             {Limit: 120, Window: time.Minute},
	"execution_submit":          {Limit: 120, Window: time.Minute},
	"billing_mutation":          {Limit: 60, Window: time.Minute},
	"scheduler_probe":           {Limit: 30, Window: time.Minute},
	"webhook_endpoint_mutation": {Limit: 30, Window: time.Minute},
})

func newFixedWindowOperationRateLimiter(rules map[string]rateLimitRule) *fixedWindowOperationRateLimiter {
	copied := make(map[string]rateLimitRule, len(rules))
	for class, rule := range rules {
		copied[class] = rule
	}
	return &fixedWindowOperationRateLimiter{
		rules:   copied,
		windows: map[string]rateLimitWindow{},
	}
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
		l.windows[key] = rateLimitWindow{
			ResetAt: now.Add(rule.Window),
			Count:   1,
		}
		return rateLimitDecision{Allowed: true}
	}
	if window.Count >= rule.Limit {
		return rateLimitDecision{
			Allowed:    false,
			RetryAfter: window.ResetAt.Sub(now).Round(time.Second),
		}
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
	err := tooManyRequests(ctx, "rate-limit-exceeded", "rate limit exceeded")
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
	if len(identity.RoleAssignments) == 0 || identity.OrgID == "" || identity.ProjectID == "" {
		return nil
	}
	roles := make([]string, 0, len(identity.RoleAssignments))
	for _, assignment := range identity.RoleAssignments {
		if assignment.ProjectID == identity.ProjectID &&
			assignment.OrganizationID == identity.OrgID &&
			assignment.Role != "" {
			roles = append(roles, assignment.Role)
		}
	}
	sort.Strings(roles)
	return compactStrings(roles)
}

func identityHasDirectPermission(identity *auth.Identity, required permission) bool {
	credentialID, _ := identity.Raw["forge_metal:credential_id"].(string)
	if strings.TrimSpace(credentialID) == "" || strings.TrimSpace(identity.OrgID) == "" {
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
		Description:  "Zitadel-issued bearer token scoped to the sandbox-rental API audience.",
	}
}
