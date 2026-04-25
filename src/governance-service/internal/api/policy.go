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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	auth "github.com/verself/auth-middleware"
	"github.com/verself/governance-service/internal/governance"
)

type permission string

func (p permission) LogValue() slog.Value { return slog.StringValue(string(p)) }

const (
	permissionAuditRead    permission = "governance:audit:read"
	permissionExportRead   permission = "governance:export:read"
	permissionExportCreate permission = "governance:export:create"

	idempotencyHeaderKey        = "idempotency_key_header"
	maxIdempotencyKeyLength     = 128
	rateLimiterMaxWindowEntries = 10000

	bodyLimitNoBody    int64 = 1024
	bodyLimitSmallJSON int64 = 16 << 10
)

type operationPolicy struct {
	Permission         permission
	TargetKind         string
	Action             string
	OrgScope           string
	RateLimitClass     string
	Idempotency        string
	AuditEvent         string
	SourceProductArea  string
	OperationDisplay   string
	OperationType      string
	EventCategory      string
	RiskLevel          string
	DataClassification string
	BodyLimitBytes     int64
}

type securedOperation struct {
	Operation huma.Operation
	Policy    operationPolicy
}

func secured(op huma.Operation, policy operationPolicy) securedOperation {
	return securedOperation{Operation: op, Policy: policy}
}

func registerSecured[I, O any](api huma.API, svc *governance.Service, securedOp securedOperation, handler func(context.Context, governance.Principal, *I) (*O, error)) {
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
		principal, err := enforceOperationPolicy(ctx, policy)
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

func withOperationPolicy(op huma.Operation, policy operationPolicy) huma.Operation {
	if policy.Permission == "" || policy.TargetKind == "" || policy.Action == "" || policy.OrgScope == "" || policy.RateLimitClass == "" || policy.AuditEvent == "" ||
		policy.SourceProductArea == "" || policy.OperationDisplay == "" || policy.OperationType == "" || policy.EventCategory == "" || policy.RiskLevel == "" {
		panic("incomplete IAM policy for operation: " + op.OperationID)
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
		"permission":          string(policy.Permission),
		"resource":            policy.TargetKind,
		"action":              policy.Action,
		"org_scope":           policy.OrgScope,
		"rate_limit_class":    policy.RateLimitClass,
		"audit_event":         policy.AuditEvent,
		"source_product_area": policy.SourceProductArea,
		"operation_display":   policy.OperationDisplay,
		"operation_type":      policy.OperationType,
		"event_category":      policy.EventCategory,
		"risk_level":          policy.RiskLevel,
	}
	if policy.DataClassification != "" {
		iam["data_classification"] = policy.DataClassification
	}
	if policy.Idempotency != "" {
		iam["idempotency"] = policy.Idempotency
	}
	if policy.BodyLimitBytes > 0 {
		iam["request_body_max_bytes"] = policy.BodyLimitBytes
	}
	op.Extensions["x-verself-iam"] = iam
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

func enforceOperationPolicy(ctx context.Context, policy operationPolicy) (governance.Principal, error) {
	authIdentity, err := requireIdentity(ctx)
	if err != nil {
		return governance.Principal{}, err
	}
	principal := principalFromIdentity(authIdentity)
	if !identityHasPermission(authIdentity, policy.Permission) {
		return principal, forbidden(ctx, "permission-denied", fmt.Sprintf("missing required permission %q", policy.Permission))
	}
	if err := requireOperationIdempotency(ctx, policy); err != nil {
		return principal, err
	}
	if decision := apiOperationRateLimiter.allow(policy.RateLimitClass, operationRateLimitKey(ctx, authIdentity, policy), time.Now()); !decision.Allowed {
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
	if credentialID != "" {
		principalType = "api_credential"
	}
	return governance.Principal{
		OrgID:                 strings.TrimSpace(identity.OrgID),
		Subject:               strings.TrimSpace(identity.Subject),
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

func identityHasPermission(identity *auth.Identity, required permission) bool {
	if identity == nil || required == "" {
		return false
	}
	if identityHasDirectPermission(identity, required) {
		return true
	}
	for _, role := range identityRolesForCurrentOrg(identity) {
		switch role {
		case "owner", "admin":
			return true
		}
	}
	return false
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

func requireOperationIdempotency(ctx context.Context, policy operationPolicy) error {
	if policy.Idempotency == "" {
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

func operationRateLimitKey(ctx context.Context, identity *auth.Identity, policy operationPolicy) string {
	info := operationRequestInfoFromContext(ctx)
	return strings.Join([]string{policy.RateLimitClass, string(policy.Permission), identity.OrgID, identity.Subject, info.ClientIP}, "\x00")
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

func auditOperation(ctx context.Context, svc *governance.Service, op huma.Operation, policy operationPolicy, principal governance.Principal, input any, output any, outcome string, err error) {
	if svc == nil || principal.OrgID == "" {
		return
	}
	info := operationRequestInfoFromContext(ctx)
	targetID, targetDisplay := targetFromBoundary(input, output)
	record := governance.AuditRecord{
		OrgID:                 principal.OrgID,
		SourceProductArea:     policy.SourceProductArea,
		ServiceName:           "governance-service",
		OperationID:           op.OperationID,
		AuditEvent:            policy.AuditEvent,
		OperationDisplay:      policy.OperationDisplay,
		OperationType:         policy.OperationType,
		EventCategory:         policy.EventCategory,
		RiskLevel:             policy.RiskLevel,
		DataClassification:    firstNonEmpty(policy.DataClassification, "restricted"),
		ActorType:             principal.Type,
		ActorID:               principal.Subject,
		ActorDisplay:          principal.Email,
		CredentialID:          principal.CredentialID,
		CredentialName:        principal.CredentialName,
		CredentialFingerprint: principal.CredentialFingerprint,
		ActorOwnerID:          principal.ActorOwnerID,
		ActorOwnerDisplay:     principal.ActorOwnerDisplay,
		AuthMethod:            principal.AuthMethod,
		Permission:            string(policy.Permission),
		TargetKind:            policy.TargetKind,
		TargetID:              targetID,
		TargetDisplay:         targetDisplay,
		TargetScope:           policy.OrgScope,
		Action:                policy.Action,
		OrgScope:              policy.OrgScope,
		RateLimitClass:        policy.RateLimitClass,
		Decision:              outcomeDecision(outcome),
		Result:                outcome,
		ClientIP:              info.ClientIP,
		IPChain:               info.IPChain,
		IPChainTrustedHops:    info.TrustedHops,
		UserAgentRaw:          info.UserAgent,
		RefererOrigin:         info.RefererOrigin,
		Origin:                info.Origin,
		Host:                  info.Host,
		IdempotencyKeyHash:    governanceHashForAPI(info.IdempotencyKey),
		RequestID:             info.RequestID,
		RouteTemplate:         op.Path,
		HTTPMethod:            op.Method,
		HTTPStatus:            uint16(statusForOutcome(outcome, err, op.DefaultStatus)),
	}
	if err != nil {
		record.ErrorCode = problemCode(err)
		record.ErrorClass = "application"
		record.ErrorMessage = err.Error()
		if outcome == "denied" {
			record.DenialReason = record.ErrorCode
		}
	}
	if _, auditErr := svc.RecordAuditEvent(ctx, record); auditErr != nil {
		slog.Default().ErrorContext(ctx, "governance audit write failed", "error", auditErr, "audit_event", policy.AuditEvent, "org_id", principal.OrgID)
	}
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

func outcomeDecision(outcome string) string {
	switch outcome {
	case "allowed":
		return "allow"
	case "denied":
		return "deny"
	case "error":
		return "error"
	default:
		return ""
	}
}

func statusForOutcome(outcome string, err error, defaultStatus int) int {
	var statusErr huma.StatusError
	if errors.As(err, &statusErr) && statusErr.GetStatus() > 0 {
		return statusErr.GetStatus()
	}
	if outcome == "allowed" {
		if defaultStatus > 0 {
			return defaultStatus
		}
		return http.StatusOK
	}
	if outcome == "denied" {
		return http.StatusForbidden
	}
	return http.StatusInternalServerError
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

func identityRolesForCurrentOrg(identity *auth.Identity) []string {
	if identity == nil || len(identity.RoleAssignments) == 0 || identity.OrgID == "" {
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

func identityHasDirectPermission(identity *auth.Identity, required permission) bool {
	credentialID, _ := identity.Raw["verself:credential_id"].(string)
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
