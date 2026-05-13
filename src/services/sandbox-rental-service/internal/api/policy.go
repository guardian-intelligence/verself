package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
)

type permission = runtimeiam.Permission

const (
	permissionGitHubRead    permission = "sandbox:github_installation:read"
	permissionGitHubWrite   permission = "sandbox:github_installation:write"
	permissionExecutionRead permission = "sandbox:execution:read"
	permissionScheduleRead  permission = "sandbox:execution_schedule:read"
	permissionScheduleWrite permission = "sandbox:execution_schedule:write"
	permissionLogsRead      permission = "sandbox:logs:read"
	permissionAnalyticsRead permission = "sandbox:analytics:read"

	idempotencyRequestBodyKey   = runtimeiam.IdempotencyRequestBodyKey
	idempotencyHeaderKey        = runtimeiam.IdempotencyHeaderKey
	maxIdempotencyKeyLength     = 128
	rateLimiterMaxWindowEntries = 10000
)

type securedOperation struct {
	Operation huma.Operation
	Policy    runtimeiam.OperationPolicy
}

func secured(op huma.Operation, policy runtimeiam.OperationPolicy) securedOperation {
	return securedOperation{Operation: op, Policy: policy}
}

func registerSecured[I, O any](api huma.API, authorizer runtimeiam.OperationAuthorizer, securedOp securedOperation, handler func(context.Context, *I) (*O, error)) {
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
		identity, err := enforceOperationPolicy(ctx, authorizer, policy, input)
		if err != nil {
			auditOperation(ctx, op.OperationID, policy, identity, input, nil, "denied", err)
			return nil, err
		}
		output, err := handler(ctx, input)
		if err != nil {
			auditOperation(ctx, op.OperationID, policy, identity, input, nil, "error", err)
			return nil, err
		}
		auditOperation(ctx, op.OperationID, policy, identity, input, output, "allowed", nil)
		return output, nil
	})
}

func withOperationPolicy(op huma.Operation, policy runtimeiam.OperationPolicy) huma.Operation {
	if err := policy.ValidateHTTPOperation(op.Method, op.OperationID); err != nil {
		panic(err)
	}
	if policy.BodyLimitBytes > 0 {
		op.MaxBodyBytes = policy.BodyLimitBytes
	}
	switch policy.Idempotency {
	case "", idempotencyRequestBodyKey:
	case idempotencyHeaderKey:
		op.Parameters = appendIdempotencyKeyHeaderParameter(op.Parameters)
	default:
		panic("unsupported idempotency policy for operation " + op.OperationID + ": " + string(policy.Idempotency))
	}
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	op.Extensions["x-verself-iam"] = policy.OpenAPIExtension()
	op.Security = []map[string][]string{{"bearerAuth": {}}}
	return op
}

func operationRequiresBodyBudget(op huma.Operation) bool {
	return runtimeiam.OperationRequiresBodyBudget(op.Method)
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

func enforceOperationPolicy(ctx context.Context, authorizer runtimeiam.OperationAuthorizer, policy runtimeiam.OperationPolicy, input any) (*auth.Identity, error) {
	identity, err := requireIdentity(ctx)
	if err != nil {
		return nil, err
	}
	if authorizer == nil {
		return identity, serviceUnavailable(ctx, "iam-authorizer-unavailable", "IAM authorizer unavailable", runtimeiam.ErrAuthorizerUnavailable)
	}
	decision, err := authorizer.AuthorizeOperation(ctx, identity, policy)
	if err != nil {
		return identity, serviceUnavailable(ctx, "iam-authorizer-unavailable", "IAM authorization check failed", err)
	}
	if !decision.Allowed {
		return identity, forbidden(ctx, "permission-denied", fmt.Sprintf("missing required permission %q", policy.Permission))
	}
	if err := requireOperationIdempotency(ctx, policy, input); err != nil {
		return identity, err
	}
	if decision := apiOperationRateLimiter.allow(string(policy.RateLimitClass), operationRateLimitKey(ctx, identity, policy), time.Now()); !decision.Allowed {
		return identity, rateLimitExceeded(ctx, decision.RetryAfter)
	}
	return identity, nil
}

type operationRequestInfoKey struct{}

type operationRequestInfo struct {
	ClientIP       string
	UserAgent      string
	IdempotencyKey string
}

func operationRequestMiddleware(ctx huma.Context, next func(huma.Context)) {
	info := operationRequestInfo{
		ClientIP:       clientIPFromHuma(ctx),
		UserAgent:      strings.TrimSpace(ctx.Header("User-Agent")),
		IdempotencyKey: strings.TrimSpace(ctx.Header("Idempotency-Key")),
	}
	next(huma.WithValue(ctx, operationRequestInfoKey{}, info))
}

func requireOperationIdempotency(ctx context.Context, policy runtimeiam.OperationPolicy, input any) error {
	switch policy.Idempotency {
	case "":
		return nil
	case idempotencyRequestBodyKey:
		return validateIdempotencyValue(ctx, "idempotency_key", nestedStringField(input, "Body", "IdempotencyKey"))
	case idempotencyHeaderKey:
		return validateIdempotencyValue(ctx, "Idempotency-Key", operationRequestInfoFromContext(ctx).IdempotencyKey)
	default:
		panic("unsupported idempotency policy: " + string(policy.Idempotency))
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

func operationRateLimitKey(ctx context.Context, identity *auth.Identity, policy runtimeiam.OperationPolicy) string {
	info := operationRequestInfoFromContext(ctx)
	parts := []string{
		string(policy.RateLimitClass),
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

func auditOperation(ctx context.Context, operationID string, policy runtimeiam.OperationPolicy, identity *auth.Identity, input any, output any, outcome string, err error) {
	args := []any{
		"audit_event", policy.AuditEvent,
		"operation_id", operationID,
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
	if identity == nil {
		return
	}
	principalType := "user"
	credentialID := claimString(identity.Raw, "verself:credential_id")
	serviceAccountID := claimString(identity.Raw, "verself:service_account_id")
	if credentialID != "" {
		principalType = "service_account"
	}
	targetID, _ := auditTargetFromBoundary(input, output)
	record := governanceAuditRecord{
		OrgID:        identity.OrgID,
		EventSource:  "sandbox-rental-service",
		EventName:    operationID,
		AuditEvent:   string(policy.AuditEvent),
		ActorType:    principalType,
		ActorID:      firstNonEmpty(serviceAccountID, identity.Subject),
		CredentialID: credentialID,
		Permission:   string(policy.Permission),
		TargetType:   string(policy.Resource),
		TargetID:     targetID,
		Outcome:      outcome,
	}
	if err != nil {
		record.ErrorCode = problemCode(err)
	}
	sendGovernanceAudit(ctx, record)
}

func auditTargetFromBoundary(input any, output any) (string, string) {
	if targetID, targetDisplay := auditTargetFromValue(output); targetID != "" || targetDisplay != "" {
		return targetID, targetDisplay
	}
	return auditTargetFromValue(input)
}

func auditTargetFromValue(input any) (string, string) {
	value := reflectValue(input)
	for _, fieldName := range []string{"ExecutionID", "AttemptID", "InstallationID", "VolumeID", "MeterTickID", "ID"} {
		if text := stringField(value, fieldName); text != "" {
			return text, text
		}
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return "", ""
	}
	body := value.FieldByName("Body")
	if body.IsValid() {
		body = reflectValue(body.Interface())
		for _, fieldName := range []string{"ExecutionID", "VolumeID", "ProductID", "IdempotencyKey"} {
			if text := stringField(body, fieldName); text != "" {
				return text, text
			}
		}
	}
	return "", ""
}

func stringField(value reflect.Value, name string) string {
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return ""
	}
	field := value.FieldByName(name)
	if !field.IsValid() || field.Kind() != reflect.String {
		return ""
	}
	return strings.TrimSpace(field.String())
}

func claimString(claims map[string]any, key string) string {
	if claims == nil {
		return ""
	}
	value, _ := claims[key].(string)
	return strings.TrimSpace(value)
}

func problemCode(err error) string {
	var model *huma.ErrorModel
	if errors.As(err, &model) {
		if model.Type != "" {
			if index := strings.LastIndex(model.Type, ":"); index >= 0 && index+1 < len(model.Type) {
				return model.Type[index+1:]
			}
			return model.Type
		}
	}
	return "operation-failed"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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
	// Browser SSR plus TanStack refetches can legitimately issue many read
	// calls during billing return polling; mutation limits carry abuse control.
	"read":                         {Limit: 3000, Window: time.Minute},
	"logs_read":                    {Limit: 600, Window: time.Minute},
	"execution_schedule_mutation":  {Limit: 120, Window: time.Minute},
	"github_installation_mutation": {Limit: 30, Window: time.Minute},
	"billing_mutation":             {Limit: 60, Window: time.Minute},
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

func applyPublicAPISecurityScheme(api huma.API) {
	openapi := api.OpenAPI()
	if openapi.Components == nil {
		openapi.Components = &huma.Components{}
	}
	openapi.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearerAuth": {
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "JWT",
			Description:  "Zitadel-issued bearer token scoped to the sandbox-rental API audience.",
		},
	}
}

func applyInternalAPISecurityScheme(api huma.API) {
	openapi := api.OpenAPI()
	if openapi.Components == nil {
		openapi.Components = &huma.Components{}
	}
	openapi.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"mutualTLS": {
			Type:        "mutualTLS",
			Description: "SPIFFE X.509-SVID mutual TLS on the sandbox-rental-service internal listener.",
		},
	}
}
