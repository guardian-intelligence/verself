package api

import (
	"context"
	"errors"
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

	auth "github.com/verself/auth-middleware"
	"github.com/verself/iam-service/internal/identity"
)

type permission string

// LogValue renders named-string values as plain strings for the otelslog
// bridge. Without this, the bridge emits `"unhandled: (permission) v"`.
func (p permission) LogValue() slog.Value { return slog.StringValue(string(p)) }

const (
	permissionOrganizationRead        permission = identity.PermissionOrganizationRead
	permissionOrganizationWrite       permission = identity.PermissionOrganizationWrite
	permissionMemberRead              permission = identity.PermissionMemberRead
	permissionMemberInvite            permission = identity.PermissionMemberInvite
	permissionMemberRolesWrite        permission = identity.PermissionMemberRolesWrite
	permissionMemberCapabilitiesRead  permission = identity.PermissionMemberCapabilitiesRead
	permissionMemberCapabilitiesWrite permission = identity.PermissionMemberCapabilitiesWrite
	permissionIAMPolicyRead           permission = identity.PermissionIAMPolicyRead
	permissionIAMPolicySet            permission = identity.PermissionIAMPolicySet
	permissionIAMPolicyTest           permission = identity.PermissionIAMPolicyTest
	permissionAPICredentialsRead      permission = identity.PermissionAPICredentialsRead
	permissionAPICredentialsCreate    permission = identity.PermissionAPICredentialsCreate
	permissionAPICredentialsRoll      permission = identity.PermissionAPICredentialsRoll
	permissionAPICredentialsRevoke    permission = identity.PermissionAPICredentialsRevoke

	idempotencyHeaderKey        = "idempotency_key_header"
	maxIdempotencyKeyLength     = 128
	rateLimiterMaxWindowEntries = 10000

	bodyLimitNoBody    int64 = 1024
	bodyLimitSmallJSON int64 = 16 << 10

	orgScopeTokenOrgID                = "token_org_id"
	orgScopeTokenRoleAssignmentOrgIDs = "token_role_assignment_org_ids"
)

type operationPolicy struct {
	Permission         permission
	Resource           string
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

func registerSecured[I, O any](api huma.API, svc *identity.Service, securedOp securedOperation, handler func(context.Context, *I) (*O, error)) {
	op := securedOp.Operation
	policy := securedOp.Policy
	if op.OperationID == "" {
		panic("missing operation ID for secured public API route")
	}
	if !strings.HasPrefix(op.Path, "/api/") {
		panic("secured public API route must live under /api/: " + op.OperationID)
	}
	policy = normalizeOperationPolicy(op.OperationID, policy)
	op = withOperationPolicy(op, policy)
	op.Middlewares = append(op.Middlewares, operationRequestMiddleware)
	huma.Register(api, op, func(ctx context.Context, input *I) (*O, error) {
		identity, err := enforceOperationPolicy(ctx, svc, policy)
		if err != nil {
			auditOperation(ctx, op, policy, identity, input, nil, "denied", err)
			return nil, err
		}
		output, err := handler(ctx, input)
		if err != nil {
			auditOperation(ctx, op, policy, identity, input, nil, "error", err)
			return nil, err
		}
		auditOperation(ctx, op, policy, identity, input, output, "allowed", nil)
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
	if policy.SourceProductArea == "" || policy.OperationDisplay == "" || policy.OperationType == "" || policy.EventCategory == "" || policy.RiskLevel == "" || policy.DataClassification == "" {
		panic("empty audit classification for operation: " + op.OperationID)
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
		"resource":            policy.Resource,
		"action":              policy.Action,
		"org_scope":           policy.OrgScope,
		"rate_limit_class":    policy.RateLimitClass,
		"audit_event":         policy.AuditEvent,
		"source_product_area": policy.SourceProductArea,
		"operation_display":   policy.OperationDisplay,
		"operation_type":      policy.OperationType,
		"event_category":      policy.EventCategory,
		"risk_level":          policy.RiskLevel,
		"data_classification": policy.DataClassification,
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

func enforceOperationPolicy(ctx context.Context, svc *identity.Service, policy operationPolicy) (*auth.Identity, error) {
	identity, err := requireIdentity(ctx)
	if err != nil {
		return nil, err
	}
	allowed, err := identityHasPermission(ctx, svc, identity, policy.Permission, policy.OrgScope)
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

func identityHasPermission(ctx context.Context, svc *identity.Service, authIdentity *auth.Identity, required permission, orgScope string) (bool, error) {
	if authIdentity == nil || required == "" {
		return false, nil
	}
	if orgScope == orgScopeTokenRoleAssignmentOrgIDs {
		orgIDs, err := authorizedRoleAssignmentOrgIDs(ctx, svc, authIdentity, required)
		return len(orgIDs) > 0, err
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
	capabilities, err := svc.MemberCapabilities(ctx, principal)
	if err != nil {
		return false, err
	}
	for _, granted := range identity.PermissionsForRoles(capabilities, principal.Roles) {
		if granted == string(required) {
			return true, nil
		}
	}
	return false, nil
}

func authorizedRoleAssignmentOrgIDs(ctx context.Context, svc *identity.Service, authIdentity *auth.Identity, required permission) ([]string, error) {
	if required == "" {
		return nil, nil
	}
	if svc == nil {
		return nil, identity.ErrStoreUnavailable
	}
	orgIDs, err := roleAssignmentOrgIDs(ctx, authIdentity)
	if err != nil {
		return nil, err
	}
	authorized := make([]string, 0, len(orgIDs))
	for _, orgID := range orgIDs {
		principal := identity.Principal{
			Subject: authIdentity.Subject,
			OrgID:   orgID,
			Roles:   rolesForOrg(authIdentity, orgID),
			Email:   authIdentity.Email,
		}
		if strings.TrimSpace(principal.Subject) == "" {
			return nil, fmt.Errorf("%w: subject is required", identity.ErrInvalidInput)
		}
		capabilities, err := svc.MemberCapabilities(ctx, principal)
		if err != nil {
			return nil, err
		}
		for _, granted := range identity.PermissionsForRoles(capabilities, principal.Roles) {
			if granted == string(required) {
				authorized = append(authorized, orgID)
				break
			}
		}
	}
	return authorized, nil
}

func rolesForOrg(authIdentity *auth.Identity, orgID string) []string {
	if authIdentity == nil || orgID == "" {
		return nil
	}
	seen := map[string]struct{}{}
	roles := make([]string, 0, len(authIdentity.RoleAssignments))
	for _, assignment := range authIdentity.RoleAssignments {
		if assignment.OrganizationID != orgID || assignment.Role == "" {
			continue
		}
		if _, ok := seen[assignment.Role]; ok {
			continue
		}
		seen[assignment.Role] = struct{}{}
		roles = append(roles, assignment.Role)
	}
	sort.Strings(roles)
	return roles
}

type operationRequestInfoKey struct{}

type operationRequestInfo struct {
	ClientIP       string
	UserAgent      string
	IdempotencyKey string
	StartedAt      time.Time
}

func operationRequestMiddleware(ctx huma.Context, next func(huma.Context)) {
	info := operationRequestInfo{
		ClientIP:       clientIPFromHuma(ctx),
		UserAgent:      strings.TrimSpace(ctx.Header("User-Agent")),
		IdempotencyKey: strings.TrimSpace(ctx.Header("Idempotency-Key")),
		StartedAt:      time.Now(),
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

func auditOperation(ctx context.Context, op huma.Operation, policy operationPolicy, identity *auth.Identity, input any, output any, outcome string, err error) {
	info := operationRequestInfoFromContext(ctx)
	args := []any{
		"audit_event", policy.AuditEvent,
		"operation_id", op.OperationID,
		"operation_permission", policy.Permission,
		"operation_resource", policy.Resource,
		"operation_action", policy.Action,
		"operation_type", policy.OperationType,
		"risk_level", policy.RiskLevel,
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
	if identity == nil {
		return
	}
	principalType := "user"
	credentialID := claimString(identity.Raw, "verself:credential_id")
	if credentialID != "" {
		principalType = "api_credential"
	}
	targetID, targetDisplay := targetFromBoundary(input, output)
	record := governanceAuditRecord{
		OrgID:                 identity.OrgID,
		SourceProductArea:     policy.SourceProductArea,
		ServiceName:           "iam-service",
		OperationID:           op.OperationID,
		AuditEvent:            policy.AuditEvent,
		OperationDisplay:      policy.OperationDisplay,
		OperationType:         policy.OperationType,
		EventCategory:         policy.EventCategory,
		RiskLevel:             policy.RiskLevel,
		DataClassification:    policy.DataClassification,
		ActorType:             principalType,
		ActorID:               identity.Subject,
		ActorDisplay:          identity.Email,
		ActorOwnerID:          claimString(identity.Raw, "verself:credential_owner_id"),
		ActorOwnerDisplay:     claimString(identity.Raw, "verself:credential_owner_display"),
		CredentialID:          credentialID,
		CredentialName:        claimString(identity.Raw, "verself:credential_name"),
		CredentialFingerprint: claimString(identity.Raw, "verself:credential_fingerprint"),
		AuthMethod:            claimString(identity.Raw, "verself:credential_auth_method"),
		Permission:            string(policy.Permission),
		TargetKind:            policy.Resource,
		TargetID:              targetID,
		TargetDisplay:         targetDisplay,
		TargetScope:           policy.OrgScope,
		Action:                policy.Action,
		OrgScope:              policy.OrgScope,
		RateLimitClass:        policy.RateLimitClass,
		Decision:              outcomeDecision(outcome),
		Result:                outcome,
		DurationMS:            durationMillis(info.StartedAt),
		ClientIP:              info.ClientIP,
		IPChain:               info.ClientIP,
		IPChainTrustedHops:    1,
		UserAgentRaw:          info.UserAgent,
		IdempotencyKeyHash:    hashTextForAudit(info.IdempotencyKey),
		RouteTemplate:         op.Path,
		HTTPMethod:            op.Method,
		HTTPStatus:            uint16FromInt(statusForOutcome(outcome, err, op.DefaultStatus), "audit http status"),
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

func targetFromBoundary(input any, output any) (string, string) {
	if targetID, targetDisplay := targetFromValue(output); targetID != "" || targetDisplay != "" {
		return targetID, targetDisplay
	}
	return targetFromValue(input)
}

func targetFromValue(input any) (string, string) {
	value := reflectValue(input)
	for _, fieldName := range []string{"CredentialID", "UserID", "OrgID", "ID"} {
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
		for _, fieldName := range []string{"CredentialID", "UserID", "OrgID", "ID"} {
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

func claimString(claims map[string]any, key string) string {
	if claims == nil {
		return ""
	}
	value, _ := claims[key].(string)
	return strings.TrimSpace(value)
}

func normalizeOperationPolicy(operationID string, policy operationPolicy) operationPolicy {
	policy.SourceProductArea = firstNonEmpty(policy.SourceProductArea, "IAM")
	policy.OperationDisplay = firstNonEmpty(policy.OperationDisplay, operationDisplay(operationID))
	policy.OperationType = firstNonEmpty(policy.OperationType, operationType(policy))
	policy.EventCategory = firstNonEmpty(policy.EventCategory, "iam")
	policy.RiskLevel = firstNonEmpty(policy.RiskLevel, riskLevel(policy))
	policy.DataClassification = firstNonEmpty(policy.DataClassification, "restricted")
	return policy
}

func operationDisplay(operationID string) string {
	return strings.ReplaceAll(strings.TrimSpace(operationID), "-", " ")
}

func operationType(policy operationPolicy) string {
	switch policy.Action {
	case "read", "list":
		return "read"
	case "delete", "revoke":
		return "delete"
	default:
		return "write"
	}
}

func riskLevel(policy operationPolicy) string {
	event := policy.AuditEvent
	switch {
	case strings.Contains(event, "api_credential") || strings.Contains(event, "member_capabilities") || strings.Contains(event, "roles.write") || strings.Contains(event, "member.invite"):
		return "high"
	case policy.Action == "read" || policy.Action == "list":
		return "medium"
	default:
		return "medium"
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

func durationMillis(startedAt time.Time) float64 {
	if startedAt.IsZero() {
		return 0
	}
	return float64(time.Since(startedAt)) / float64(time.Millisecond)
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
	"read":                         {Limit: 600, Window: time.Minute},
	"organization_mutation":        {Limit: 30, Window: time.Minute},
	"member_mutation":              {Limit: 60, Window: time.Minute},
	"member_capabilities_mutation": {Limit: 30, Window: time.Minute},
	"api_credential_mutation":      {Limit: 30, Window: time.Minute},
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
	if len(identity.RoleAssignments) == 0 || identity.OrgID == "" {
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
		Description:  "Zitadel-issued bearer token scoped to the iam-service API audience.",
	}
}
