package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/governance-service/internal/governance"
	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
)

type permission = runtimeiam.Permission

const (
	permissionAuditRead    permission = "governance:audit:read"
	permissionExportRead   permission = "governance:export:read"
	permissionExportCreate permission = "governance:export:create"

	idempotencyHeaderKey        = runtimeiam.IdempotencyHeaderKey
	maxIdempotencyKeyLength     = 128
	rateLimiterMaxWindowEntries = 10000

	bodyLimitNoBody    int64 = 1024
	bodyLimitSmallJSON int64 = 16 << 10
)

type securedOperation struct {
	Operation huma.Operation
	Policy    runtimeiam.OperationPolicy
}

func secured(op huma.Operation, policy runtimeiam.OperationPolicy) securedOperation {
	return securedOperation{Operation: op, Policy: policy}
}

func registerSecured[I, O any](api huma.API, svc *governance.Service, authorizer runtimeiam.OperationAuthorizer, securedOp securedOperation, handler func(context.Context, governance.Principal, *I) (*O, error)) {
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
		principal, err := enforceOperationPolicy(ctx, authorizer, policy)
		if err != nil {
			auditOperation(ctx, svc, op, policy, principal, input, nil, "denied", err)
			return nil, err
		}
		output, err := handler(ctx, principal, input)
		if err != nil {
			mapped := mapError(ctx, err)
			auditOperation(ctx, svc, op, policy, principal, input, nil, "error", mapped)
			return nil, mapped
		}
		auditOperation(ctx, svc, op, policy, principal, input, output, "allowed", nil)
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
	case "":
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

func enforceOperationPolicy(ctx context.Context, authorizer runtimeiam.OperationAuthorizer, policy runtimeiam.OperationPolicy) (governance.Principal, error) {
	authIdentity, err := requireIdentity(ctx)
	if err != nil {
		return governance.Principal{}, err
	}
	principal := principalFromIdentity(authIdentity)
	if authorizer == nil {
		return principal, problem(ctx, http.StatusServiceUnavailable, "iam-authorizer-unavailable", "IAM authorizer unavailable", runtimeiam.ErrAuthorizerUnavailable)
	}
	decision, err := authorizer.AuthorizeOperation(ctx, authIdentity, policy)
	if err != nil {
		return principal, problem(ctx, http.StatusServiceUnavailable, "iam-authorizer-unavailable", "IAM authorization check failed", err)
	}
	if !decision.Allowed {
		return principal, forbidden(ctx, "permission-denied", fmt.Sprintf("missing required permission %q", policy.Permission))
	}
	if err := requireOperationIdempotency(ctx, policy); err != nil {
		return principal, err
	}
	if decision := apiOperationRateLimiter.allow(string(policy.RateLimitClass), operationRateLimitKey(ctx, authIdentity, policy), time.Now()); !decision.Allowed {
		return principal, rateLimitExceeded(ctx, decision.RetryAfter)
	}
	return principal, nil
}

func principalFromIdentity(identity *auth.Identity) governance.Principal {
	if identity == nil {
		return governance.Principal{}
	}
	principalType := "user"
	credentialID := claimString(identity.Raw, "verself:credential_id")
	serviceAccountID := claimString(identity.Raw, "verself:service_account_id")
	if credentialID != "" {
		principalType = "service_account"
	}
	return governance.Principal{
		OrgID:                 strings.TrimSpace(identity.OrgID),
		Subject:               firstNonEmpty(serviceAccountID, identity.Subject),
		Email:                 strings.TrimSpace(identity.Email),
		Type:                  principalType,
		CredentialID:          credentialID,
		CredentialName:        claimString(identity.Raw, "verself:credential_name"),
		CredentialFingerprint: claimString(identity.Raw, "verself:credential_fingerprint"),
		ActorOwnerID:          claimString(identity.Raw, "verself:credential_owner_id"),
		ActorOwnerDisplay:     claimString(identity.Raw, "verself:credential_owner_display"),
		AuthMethod:            claimString(identity.Raw, "verself:credential_auth_method"),
	}
}

func claimString(claims map[string]any, key string) string {
	if claims == nil {
		return ""
	}
	value, _ := claims[key].(string)
	return strings.TrimSpace(value)
}

type operationRequestInfoKey struct{}

type operationRequestInfo struct {
	ClientIP       string
	IPChain        string
	TrustedHops    uint8
	UserAgent      string
	IdempotencyKey string
	RefererOrigin  string
	Origin         string
	Host           string
	RequestID      string
}

func operationRequestMiddleware(ctx huma.Context, next func(huma.Context)) {
	info := operationRequestInfo{
		ClientIP:       clientIPFromHuma(ctx),
		IPChain:        ipChainFromHuma(ctx),
		TrustedHops:    1,
		UserAgent:      strings.TrimSpace(ctx.Header("User-Agent")),
		IdempotencyKey: strings.TrimSpace(ctx.Header("Idempotency-Key")),
		RefererOrigin:  originFromURL(ctx.Header("Referer")),
		Origin:         strings.TrimSpace(ctx.Header("Origin")),
		Host:           strings.TrimSpace(ctx.Header("Host")),
		RequestID:      firstHeader(ctx, "X-Request-ID", "X-Correlation-ID", "Fly-Request-Id", "Cf-Ray"),
	}
	next(huma.WithValue(ctx, operationRequestInfoKey{}, info))
}

func requireOperationIdempotency(ctx context.Context, policy runtimeiam.OperationPolicy) error {
	if policy.Idempotency == runtimeiam.IdempotencyNone {
		return nil
	}
	value := operationRequestInfoFromContext(ctx).IdempotencyKey
	if strings.TrimSpace(value) == "" {
		return problem(ctx, http.StatusBadRequest, "idempotency-key-required", "Idempotency-Key is required for this operation", nil)
	}
	if len(value) > maxIdempotencyKeyLength || strings.ContainsAny(value, "\x00\r\n\t") {
		return problem(ctx, http.StatusBadRequest, "idempotency-key-invalid", "Idempotency-Key is invalid", nil)
	}
	return nil
}

func operationRequestInfoFromContext(ctx context.Context) operationRequestInfo {
	info, _ := ctx.Value(operationRequestInfoKey{}).(operationRequestInfo)
	return info
}

func operationRateLimitKey(ctx context.Context, identity *auth.Identity, policy runtimeiam.OperationPolicy) string {
	info := operationRequestInfoFromContext(ctx)
	return strings.Join([]string{string(policy.RateLimitClass), string(policy.Permission), identity.OrgID, identity.Subject, info.ClientIP}, "\x00")
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

func ipChainFromHuma(ctx huma.Context) string {
	if value := strings.TrimSpace(ctx.Header("X-Forwarded-For")); value != "" {
		return value
	}
	return clientIPFromHuma(ctx)
}

func firstHeader(ctx huma.Context, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(ctx.Header(name)); value != "" {
			return value
		}
	}
	return ""
}

func originFromURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func auditOperation(ctx context.Context, svc *governance.Service, op huma.Operation, policy runtimeiam.OperationPolicy, principal governance.Principal, input any, output any, outcome string, err error) {
	if svc == nil || principal.OrgID == "" {
		return
	}
	info := operationRequestInfoFromContext(ctx)
	targetID, _ := targetFromBoundary(input, output)
	record := governance.AuditRecord{
		OrgID:        principal.OrgID,
		EventSource:  "governance-service",
		EventName:    op.OperationID,
		AuditEvent:   string(policy.AuditEvent),
		ActorType:    principal.Type,
		ActorID:      principal.Subject,
		CredentialID: principal.CredentialID,
		Permission:   string(policy.Permission),
		TargetType:   string(policy.Resource),
		TargetID:     targetID,
		Outcome:      outcome,
		Detail: compactAuditDetail(map[string]any{
			"idempotency_key_hash":   governanceHashForAPI(info.IdempotencyKey),
			"credential_fingerprint": principal.CredentialFingerprint,
		}),
	}
	if err != nil {
		record.ErrorCode = problemCode(err)
		if outcome == "denied" {
			record.Detail["denial_reason"] = record.ErrorCode
		}
	}
	if _, auditErr := svc.RecordAuditEvent(ctx, record); auditErr != nil {
		slog.Default().ErrorContext(ctx, "governance audit write failed", "error", auditErr, "audit_event", policy.AuditEvent, "org_id", principal.OrgID)
	}
}

func compactAuditDetail(values map[string]any) map[string]any {
	detail := make(map[string]any, len(values))
	for key, value := range values {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				detail[key] = typed
			}
		case uint16:
			if typed != 0 {
				detail[key] = typed
			}
		case nil:
		default:
			detail[key] = value
		}
	}
	return detail
}

func targetFromBoundary(input any, output any) (string, string) {
	if targetID, targetDisplay := targetFromValue(output); targetID != "" || targetDisplay != "" {
		return targetID, targetDisplay
	}
	return targetFromValue(input)
}

func targetFromValue(input any) (string, string) {
	value := reflectValue(input)
	for _, fieldName := range []string{"ExportID", "CredentialID", "UserID", "ExecutionID", "VolumeID", "ID"} {
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
		for _, fieldName := range []string{"ExportID", "CredentialID", "UserID", "ExecutionID", "VolumeID", "ID"} {
			if text := stringField(body, fieldName); text != "" {
				return text, text
			}
		}
	}
	return "", ""
}

func reflectValue(input any) reflect.Value {
	value := reflect.ValueOf(input)
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			return reflect.Value{}
		}
		value = value.Elem()
	}
	return value
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

func problemCode(err error) string {
	var model *huma.ErrorModel
	if !errors.As(err, &model) {
		return "operation-failed"
	}
	if len(model.Errors) > 0 && model.Errors[0] != nil {
		if code := fmt.Sprint(model.Errors[0].Value); code != "" {
			return code
		}
	}
	if model.Type != "" {
		if index := strings.LastIndex(model.Type, ":"); index >= 0 && index+1 < len(model.Type) {
			return model.Type[index+1:]
		}
		return model.Type
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
	"read":            {Limit: 600, Window: time.Minute},
	"export_create":   {Limit: 12, Window: time.Hour},
	"export_download": {Limit: 60, Window: time.Minute},
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
		Description:  "Zitadel-issued bearer token scoped to the governance-service API audience.",
	}
}

func governanceHashForAPI(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
