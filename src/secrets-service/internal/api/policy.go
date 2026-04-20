package api

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/forge-metal/secrets-service/internal/secrets"
)

type permission string

const (
	permissionSecretRead    permission = "secrets:secret:read"
	permissionSecretWrite   permission = "secrets:secret:write"
	permissionSecretDelete  permission = "secrets:secret:delete"
	permissionTransitRead   permission = "secrets:transit:read"
	permissionTransitWrite  permission = "secrets:transit:write"
	permissionTransitUse    permission = "secrets:transit:use"
	permissionTransitRotate permission = "secrets:transit:rotate"

	idempotencyHeaderKey              = "idempotency_key_header"
	maxIdempotencyKeyLength           = 128
	rateLimiterMaxWindowEntries       = 10000
	bodyLimitSmallJSON          int64 = 64 << 10
	bodyLimitCryptoJSON         int64 = 256 << 10
)

type operationPolicy struct {
	Permission         permission
	TargetKind         string
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
	SecretOperation    string
}

type securedOperation struct {
	Operation huma.Operation
	Policy    operationPolicy
}

func secured(op huma.Operation, policy operationPolicy) securedOperation {
	return securedOperation{Operation: op, Policy: normalizeOperationPolicy(op.OperationID, policy)}
}

func registerSecured[I, O any](api huma.API, svc *secrets.Service, securedOp securedOperation, handler func(context.Context, secrets.Principal, *I) (*O, error)) {
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
		principal, identity, err := enforceOperationPolicy(ctx, policy)
		if err != nil {
			auditOperation(ctx, op.OperationID, policy, identity, input, nil, "denied", err)
			return nil, err
		}
		output, err := handler(ctx, principal, input)
		if err != nil {
			mapped := mapError(ctx, err)
			auditOperation(ctx, op.OperationID, policy, identity, input, nil, "error", mapped)
			return nil, mapped
		}
		auditOperation(ctx, op.OperationID, policy, identity, input, output, "allowed", nil)
		return output, nil
	})
}

func withOperationPolicy(op huma.Operation, policy operationPolicy) huma.Operation {
	if policy.Permission == "" || policy.TargetKind == "" || policy.Action == "" || policy.OrgScope == "" || policy.RateLimitClass == "" || policy.AuditEvent == "" ||
		policy.OperationDisplay == "" || policy.OperationType == "" || policy.EventCategory == "" || policy.RiskLevel == "" {
		panic("incomplete IAM policy for operation: " + op.OperationID)
	}
	if operationRequiresBodyBudget(op) && policy.BodyLimitBytes <= 0 {
		panic("empty request body limit for mutating operation: " + op.OperationID)
	}
	if policy.BodyLimitBytes > 0 {
		op.MaxBodyBytes = policy.BodyLimitBytes
	}
	if policy.Idempotency == idempotencyHeaderKey {
		op.Parameters = appendIdempotencyKeyHeaderParameter(op.Parameters)
	} else if policy.Idempotency != "" {
		panic("unsupported idempotency policy for operation " + op.OperationID + ": " + policy.Idempotency)
	}
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	op.Extensions["x-forge-metal-iam"] = map[string]any{
		"permission":          string(policy.Permission),
		"resource":            policy.TargetKind,
		"action":              policy.Action,
		"org_scope":           policy.OrgScope,
		"rate_limit_class":    policy.RateLimitClass,
		"audit_event":         policy.AuditEvent,
		"source_product_area": "Secrets",
		"operation_display":   policy.OperationDisplay,
		"operation_type":      policy.OperationType,
		"event_category":      policy.EventCategory,
		"risk_level":          policy.RiskLevel,
		"data_classification": policy.DataClassification,
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
		Schema: &huma.Schema{
			Type:      "string",
			MinLength: &minLength,
			MaxLength: &maxLength,
		},
	})
}

func enforceOperationPolicy(ctx context.Context, policy operationPolicy) (secrets.Principal, *auth.Identity, error) {
	identity, err := requireIdentity(ctx)
	if err != nil {
		return secrets.Principal{}, nil, err
	}
	principal := principalFromIdentity(identity)
	if !identityHasPermission(identity, policy.Permission) {
		return principal, identity, forbidden(ctx, "permission-denied", fmt.Sprintf("missing required permission %q", policy.Permission))
	}
	if err := requireOperationIdempotency(ctx, policy); err != nil {
		return principal, identity, err
	}
	if decision := apiOperationRateLimiter.allow(policy.RateLimitClass, operationRateLimitKey(ctx, identity, policy), time.Now()); !decision.Allowed {
		return principal, identity, rateLimitExceeded(ctx, decision.RetryAfter)
	}
	return principal, identity, nil
}

func requireIdentity(ctx context.Context) (*auth.Identity, error) {
	identity := auth.FromContext(ctx)
	if identity == nil || strings.TrimSpace(identity.Subject) == "" {
		return nil, unauthorized(ctx, "missing-identity", "missing authenticated identity")
	}
	if strings.TrimSpace(identity.OrgID) == "" {
		return nil, forbidden(ctx, "missing-org", "authenticated identity is missing organization scope")
	}
	return identity, nil
}

func principalFromIdentity(identity *auth.Identity) secrets.Principal {
	principalType := "user"
	credentialID := claimString(identity.Raw, "forge_metal:credential_id")
	if credentialID != "" {
		principalType = "api_credential"
	}
	return secrets.Principal{
		OrgID:                 strings.TrimSpace(identity.OrgID),
		Subject:               strings.TrimSpace(identity.Subject),
		Email:                 strings.TrimSpace(identity.Email),
		Type:                  principalType,
		CredentialID:          credentialID,
		CredentialName:        claimString(identity.Raw, "forge_metal:credential_name"),
		CredentialFingerprint: claimString(identity.Raw, "forge_metal:credential_fingerprint"),
		ActorOwnerID:          claimString(identity.Raw, "forge_metal:credential_owner_id"),
		ActorOwnerDisplay:     claimString(identity.Raw, "forge_metal:credential_owner_display"),
		AuthMethod:            claimString(identity.Raw, "forge_metal:credential_auth_method"),
	}
}

func identityHasPermission(identity *auth.Identity, required permission) bool {
	if identity == nil {
		return false
	}
	if identityHasDirectPermission(identity, required) {
		return true
	}
	if identity.OrgID == "" || len(identity.RoleAssignments) == 0 {
		return false
	}
	for _, assignment := range identity.RoleAssignments {
		if assignment.ProjectID != identity.ProjectID || assignment.OrganizationID != identity.OrgID {
			continue
		}
		switch assignment.Role {
		case "owner", "admin":
			return true
		case "member":
			if required == permissionSecretRead || required == permissionTransitRead || required == permissionTransitUse {
				return true
			}
		}
	}
	return false
}

func identityHasDirectPermission(identity *auth.Identity, required permission) bool {
	credentialID := claimString(identity.Raw, "forge_metal:credential_id")
	if credentialID == "" || strings.TrimSpace(identity.OrgID) == "" {
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
	if strings.ContainsAny(value, "\x00\r\n\t") {
		return problem(ctx, http.StatusBadRequest, "idempotency-key-invalid", "Idempotency-Key contains unsupported characters", nil)
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

func auditOperation(ctx context.Context, operationID string, policy operationPolicy, identity *auth.Identity, input any, output any, outcome string, err error) {
	if identity == nil {
		return
	}
	info := operationRequestInfoFromContext(ctx)
	targetID, targetScope, targetPathHash, secretVersion, keyID := auditTarget(identity.OrgID, input, output)
	record := governanceAuditRecord{
		OrgID:                 identity.OrgID,
		SourceProductArea:     "Secrets",
		ServiceName:           "secrets-service",
		OperationID:           operationID,
		AuditEvent:            policy.AuditEvent,
		OperationDisplay:      policy.OperationDisplay,
		OperationType:         policy.OperationType,
		EventCategory:         policy.EventCategory,
		RiskLevel:             policy.RiskLevel,
		DataClassification:    policy.DataClassification,
		ActorType:             principalFromIdentity(identity).Type,
		ActorID:               identity.Subject,
		ActorDisplay:          identity.Email,
		ActorOwnerID:          claimString(identity.Raw, "forge_metal:credential_owner_id"),
		ActorOwnerDisplay:     claimString(identity.Raw, "forge_metal:credential_owner_display"),
		CredentialID:          claimString(identity.Raw, "forge_metal:credential_id"),
		CredentialName:        claimString(identity.Raw, "forge_metal:credential_name"),
		CredentialFingerprint: claimString(identity.Raw, "forge_metal:credential_fingerprint"),
		AuthMethod:            claimString(identity.Raw, "forge_metal:credential_auth_method"),
		Permission:            string(policy.Permission),
		TargetKind:            policy.TargetKind,
		TargetID:              targetID,
		TargetDisplay:         targetID,
		TargetScope:           targetScope,
		TargetPathHash:        targetPathHash,
		Action:                policy.Action,
		OrgScope:              policy.OrgScope,
		RateLimitClass:        policy.RateLimitClass,
		Decision:              outcomeDecision(outcome),
		Result:                outcome,
		ClientIP:              info.ClientIP,
		IPChain:               info.ClientIP,
		IPChainTrustedHops:    1,
		UserAgentRaw:          info.UserAgent,
		IdempotencyKeyHash:    hashTextForAudit(info.IdempotencyKey),
		TrustClass:            "standard",
		SecretMount:           "local-envelope",
		SecretPathHash:        targetPathHash,
		SecretVersion:         secretVersion,
		SecretOperation:       policy.SecretOperation,
		KeyID:                 keyID,
		ContentSHA256:         contentHashFromBoundary(input),
	}
	if err != nil {
		record.ErrorCode = problemCode(err)
		record.ErrorClass = "application"
		record.ErrorMessage = err.Error()
		if outcome == "denied" {
			record.DenialReason = record.ErrorCode
		}
	}
	sendGovernanceAudit(ctx, record)
}

func auditTarget(orgID string, input any, output any) (string, string, string, uint64, string) {
	if id, scope, pathHash, version, keyID := auditTargetFromValue(orgID, output); id != "" || pathHash != "" || keyID != "" {
		return id, scope, pathHash, version, keyID
	}
	return auditTargetFromValue(orgID, input)
}

func auditTargetFromValue(orgID string, input any) (string, string, string, uint64, string) {
	value := reflectValue(input)
	body := bodyValue(value)
	keyName := stringField(body, "KeyName")
	if keyName == "" {
		keyName = stringField(value, "KeyName")
	}
	keyID := stringField(body, "KeyID")
	if keyID == "" {
		keyID = stringField(value, "KeyID")
	}
	if keyName != "" || keyID != "" {
		return firstNonEmpty(keyID, keyName), "org", "", 0, keyID
	}
	name := stringField(body, "Name")
	if name == "" {
		name = stringField(value, "Name")
	}
	kind := stringField(body, "Kind")
	scope := scopeFromValue(body)
	if scope.Level == "" {
		scope = scopeFromValue(value)
	}
	if name != "" {
		pathHash := secrets.SecretPathHash(orgID, kind, name, scope)
		version := uint64Field(body, "CurrentVersion")
		if version == 0 {
			version = uint64Field(value, "CurrentVersion")
		}
		return "", scope.Level, pathHash, version, ""
	}
	return "", "", "", 0, ""
}

func scopeFromValue(value reflect.Value) secrets.Scope {
	if !value.IsValid() {
		return secrets.Scope{}
	}
	scopeValue := value.FieldByName("Scope")
	if scopeValue.IsValid() {
		scopeValue = reflectValue(scopeValue.Interface())
		return secrets.Scope{
			Level:    stringField(scopeValue, "Level"),
			SourceID: stringField(scopeValue, "SourceID"),
			EnvID:    stringField(scopeValue, "EnvID"),
			Branch:   stringField(scopeValue, "Branch"),
		}
	}
	return secrets.Scope{
		Level:    stringField(value, "ScopeLevel"),
		SourceID: stringField(value, "SourceID"),
		EnvID:    stringField(value, "EnvID"),
		Branch:   stringField(value, "Branch"),
	}
}

func contentHashFromBoundary(input any) string {
	value := bodyValue(reflectValue(input))
	for _, name := range []string{"Value", "PlaintextBase64", "Ciphertext", "MessageBase64", "Signature"} {
		if text := stringField(value, name); text != "" {
			return hashTextForAudit("redacted:" + name + ":" + text)
		}
	}
	return ""
}

func bodyValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return reflect.Value{}
	}
	value = reflectValue(value.Interface())
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return value
	}
	body := value.FieldByName("Body")
	if body.IsValid() {
		return reflectValue(body.Interface())
	}
	return value
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

func uint64Field(value reflect.Value, name string) uint64 {
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return 0
	}
	field := value.FieldByName(name)
	if !field.IsValid() {
		return 0
	}
	switch field.Kind() {
	case reflect.Uint64, reflect.Uint, reflect.Uint32, reflect.Uint16, reflect.Uint8:
		return field.Uint()
	case reflect.String:
		value, err := strconv.ParseUint(strings.TrimSpace(field.String()), 10, 64)
		if err == nil {
			return value
		}
	default:
		return 0
	}
	return 0
}

func claimString(claims map[string]any, key string) string {
	if claims == nil {
		return ""
	}
	value, _ := claims[key].(string)
	return strings.TrimSpace(value)
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

func normalizeOperationPolicy(operationID string, policy operationPolicy) operationPolicy {
	policy.OperationDisplay = firstNonEmpty(policy.OperationDisplay, strings.ReplaceAll(strings.TrimSpace(operationID), "-", " "))
	policy.OperationType = firstNonEmpty(policy.OperationType, operationType(policy))
	policy.EventCategory = firstNonEmpty(policy.EventCategory, "secrets")
	policy.RiskLevel = firstNonEmpty(policy.RiskLevel, "high")
	policy.DataClassification = firstNonEmpty(policy.DataClassification, "secret")
	return policy
}

func operationType(policy operationPolicy) string {
	switch policy.Action {
	case "read", "list":
		return "read"
	case "delete", "destroy":
		return "delete"
	default:
		return "write"
	}
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

func problemCode(err error) string {
	var model *huma.ErrorModel
	if errors.As(err, &model) {
		if model.Type != "" {
			if index := strings.LastIndex(model.Type, ":"); index >= 0 && index+1 < len(model.Type) {
				return model.Type[index+1:]
			}
			return model.Type
		}
		for _, detail := range model.Errors {
			if detail == nil {
				continue
			}
			if value, ok := detail.Value.(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
			if strings.TrimSpace(detail.Message) != "" {
				return strings.TrimSpace(detail.Message)
			}
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
	"read":            {Limit: 600, Window: time.Minute},
	"secret_mutation": {Limit: 120, Window: time.Minute},
	"crypto":          {Limit: 1200, Window: time.Minute},
	"key_mutation":    {Limit: 60, Window: time.Minute},
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
		Description:  "Zitadel-issued bearer token scoped to the secrets-service API audience.",
	}
}
