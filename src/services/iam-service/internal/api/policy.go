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

	"github.com/verself/iam-service/internal/authz"
	"github.com/verself/iam-service/internal/identity"
	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
)

type permission = runtimeiam.Permission

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

	idempotencyHeaderKey        = runtimeiam.IdempotencyHeaderKey
	maxIdempotencyKeyLength     = 128
	rateLimiterMaxWindowEntries = 10000

	bodyLimitNoBody    int64 = 1024
	bodyLimitSmallJSON int64 = 16 << 10

	orgScopeTokenOrgID                = runtimeiam.OrgScopeTokenOrgID
	orgScopeTokenRoleAssignmentOrgIDs = runtimeiam.OrgScopeTokenRoleAssignmentOrgIDs
)

const (
	resourceOrganization                runtimeiam.ResourceKind   = "organization"
	resourceOrganizationMember          runtimeiam.ResourceKind   = "organization_member"
	resourceOrganizationMemberRoles     runtimeiam.ResourceKind   = "organization_member_roles"
	resourceOrganizationCapabilities    runtimeiam.ResourceKind   = "organization_member_capabilities"
	resourceOrganizationIAMPolicy       runtimeiam.ResourceKind   = "organization_iam_policy"
	resourceServiceAccount              runtimeiam.ResourceKind   = "service_account"
	resourceAPICredential               runtimeiam.ResourceKind   = "api_credential"
	rateLimitRead                       runtimeiam.RateLimitClass = "read"
	rateLimitOrganizationMutation       runtimeiam.RateLimitClass = "organization_mutation"
	rateLimitMemberMutation             runtimeiam.RateLimitClass = "member_mutation"
	rateLimitMemberCapabilitiesMutation runtimeiam.RateLimitClass = "member_capabilities_mutation"
	rateLimitIAMPolicyMutation          runtimeiam.RateLimitClass = "iam_policy_mutation"
	rateLimitAPICredentialMutation      runtimeiam.RateLimitClass = "api_credential_mutation"
	auditOrganizationRead               runtimeiam.AuditEvent     = "iam.organization.read"
	auditOrganizationMembershipList     runtimeiam.AuditEvent     = "iam.organization.membership.list"
	auditOrganizationUpdate             runtimeiam.AuditEvent     = "iam.organization.update"
	auditOrganizationMemberList         runtimeiam.AuditEvent     = "iam.organization.member.list"
	auditOrganizationMemberInvite       runtimeiam.AuditEvent     = "iam.organization.member.invite"
	auditOrganizationMemberRolesWrite   runtimeiam.AuditEvent     = "iam.organization.member.roles.write"
	auditOrganizationCapabilitiesRead   runtimeiam.AuditEvent     = "iam.organization.member_capabilities.read"
	auditOrganizationCapabilitiesWrite  runtimeiam.AuditEvent     = "iam.organization.member_capabilities.write"
	auditOrganizationPolicyRead         runtimeiam.AuditEvent     = "iam.organization.policy.read"
	auditOrganizationPolicyWrite        runtimeiam.AuditEvent     = "iam.organization.policy.write"
	auditOrganizationPolicyTest         runtimeiam.AuditEvent     = "iam.organization.policy.test_permissions"
	auditServiceAccountList             runtimeiam.AuditEvent     = "iam.service_account.list"
	auditServiceAccountRead             runtimeiam.AuditEvent     = "iam.service_account.read"
	auditServiceAccountDisable          runtimeiam.AuditEvent     = "iam.service_account.disable"
	auditAPICredentialList              runtimeiam.AuditEvent     = "iam.api_credential.list"
	auditAPICredentialRead              runtimeiam.AuditEvent     = "iam.api_credential.read"
	auditAPICredentialCreate            runtimeiam.AuditEvent     = "iam.api_credential.create"
	auditAPICredentialRoll              runtimeiam.AuditEvent     = "iam.api_credential.roll"
	auditAPICredentialRevoke            runtimeiam.AuditEvent     = "iam.api_credential.revoke"
)

type securedOperation struct {
	Operation huma.Operation
	Policy    runtimeiam.OperationPolicy
}

func secured(op huma.Operation, policy runtimeiam.OperationPolicy) securedOperation {
	return securedOperation{Operation: op, Policy: policy}
}

func registerSecured[I, O any](api huma.API, svc *identity.Service, authzSvc *authz.Service, securedOp securedOperation, handler func(context.Context, *I) (*O, error)) {
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
		identity, err := enforceOperationPolicy(ctx, svc, authzSvc, policy)
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

func withOperationPolicy(op huma.Operation, policy runtimeiam.OperationPolicy) huma.Operation {
	if err := policy.ValidateHTTPOperation(op.Method, op.OperationID); err != nil {
		panic(err)
	}
	if policy.BodyLimitBytes > 0 {
		op.MaxBodyBytes = policy.BodyLimitBytes
	}
	switch policy.Idempotency {
	case runtimeiam.IdempotencyNone:
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

func enforceOperationPolicy(ctx context.Context, svc *identity.Service, authzSvc *authz.Service, policy runtimeiam.OperationPolicy) (*auth.Identity, error) {
	identity, err := requireIdentity(ctx)
	if err != nil {
		return nil, err
	}
	if err := synchronizeAuthorizationGraph(ctx, svc, identity, policy); err != nil {
		if errors.Is(err, authz.ErrInvalid) || errors.Is(err, authz.ErrUnavailable) {
			return identity, authzError(ctx, err)
		}
		return identity, identityError(ctx, err)
	}
	allowed, err := identityHasPermission(ctx, authzSvc, identity, policy.Permission, policy.OrgScope)
	if err != nil {
		if errors.Is(err, authz.ErrInvalid) || errors.Is(err, authz.ErrUnavailable) {
			return identity, authzError(ctx, err)
		}
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

func synchronizeAuthorizationGraph(ctx context.Context, svc *identity.Service, authIdentity *auth.Identity, policy runtimeiam.OperationPolicy) error {
	if svc == nil || authIdentity == nil {
		return identity.ErrStoreUnavailable
	}
	actor := authorizationActor(authIdentity)
	switch policy.OrgScope {
	case orgScopeTokenOrgID:
		return svc.ReconcileOrganizationAuthorization(ctx, authIdentity.OrgID, actor, "authorize-"+string(policy.Permission))
	case orgScopeTokenRoleAssignmentOrgIDs:
		orgIDs, err := roleAssignmentOrgIDs(ctx, authIdentity)
		if err != nil {
			return err
		}
		for _, orgID := range orgIDs {
			if err := svc.ReconcileOrganizationAuthorization(ctx, orgID, actor, "authorize-"+string(policy.Permission)); err != nil {
				return err
			}
		}
	}
	return nil
}

func authorizationActor(authIdentity *auth.Identity) string {
	subject := authzSubjectFromIdentity(authIdentity)
	if strings.TrimSpace(subject.ID) != "" {
		return strings.TrimSpace(subject.ID)
	}
	if authIdentity == nil {
		return ""
	}
	return strings.TrimSpace(authIdentity.Subject)
}

func identityHasPermission(ctx context.Context, authzSvc *authz.Service, authIdentity *auth.Identity, required permission, orgScope runtimeiam.OrgScope) (bool, error) {
	if authIdentity == nil || required == "" {
		return false, nil
	}
	if authzSvc == nil {
		return false, authz.ErrUnavailable
	}
	if orgScope == orgScopeTokenRoleAssignmentOrgIDs {
		orgIDs, err := authorizedRoleAssignmentOrgIDs(ctx, authzSvc, authIdentity, required)
		return len(orgIDs) > 0, err
	}
	orgID := strings.TrimSpace(authIdentity.OrgID)
	if orgID == "" {
		return false, fmt.Errorf("%w: org_id is required", identity.ErrInvalidInput)
	}
	allowed, _, err := authzSvc.TestOrganizationPermissions(ctx, orgID, authzSubjectFromIdentity(authIdentity), []string{string(required)}, "")
	if err != nil {
		return false, err
	}
	return stringSliceContains(allowed, string(required)), nil
}

func authorizedRoleAssignmentOrgIDs(ctx context.Context, authzSvc *authz.Service, authIdentity *auth.Identity, required permission) ([]string, error) {
	if required == "" {
		return nil, nil
	}
	orgIDs, err := roleAssignmentOrgIDs(ctx, authIdentity)
	if err != nil {
		return nil, err
	}
	authorized := make([]string, 0, len(orgIDs))
	for _, orgID := range orgIDs {
		allowed, _, err := authzSvc.TestOrganizationPermissions(ctx, orgID, authzSubjectFromIdentity(authIdentity), []string{string(required)}, "")
		if err != nil {
			return nil, err
		}
		if stringSliceContains(allowed, string(required)) {
			authorized = append(authorized, orgID)
		}
	}
	return authorized, nil
}

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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

func requireOperationIdempotency(ctx context.Context, policy runtimeiam.OperationPolicy) error {
	if policy.Idempotency == runtimeiam.IdempotencyNone {
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

func operationRateLimitKey(ctx context.Context, identity *auth.Identity, policy runtimeiam.OperationPolicy) string {
	info := operationRequestInfoFromContext(ctx)
	return strings.Join([]string{
		string(policy.RateLimitClass),
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

func auditOperation(ctx context.Context, op huma.Operation, policy runtimeiam.OperationPolicy, identity *auth.Identity, input any, output any, outcome string, err error) {
	info := operationRequestInfoFromContext(ctx)
	args := []any{
		"audit_event", policy.AuditEvent,
		"operation_id", op.OperationID,
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
	if identity == nil {
		return
	}
	principalType := "user"
	credentialID := claimString(identity.Raw, "verself:credential_id")
	serviceAccountID := claimString(identity.Raw, "verself:service_account_id")
	if credentialID != "" {
		principalType = "service_account"
	}
	targetID, targetDisplay := targetFromBoundary(input, output)
	record := governanceAuditRecord{
		OrgID:        identity.OrgID,
		EventSource:  "iam-service",
		EventName:    op.OperationID,
		AuditEvent:   string(policy.AuditEvent),
		ActorType:    principalType,
		ActorID:      firstNonEmpty(serviceAccountID, identity.Subject),
		CredentialID: credentialID,
		Permission:   string(policy.Permission),
		TargetType:   string(policy.Resource),
		TargetID:     targetID,
		Outcome:      outcome,
		Detail: compactAuditDetail(map[string]any{
			"idempotency_key_hash": hashTextForAudit(info.IdempotencyKey),
			"target_display":       targetDisplay,
		}),
	}
	if err != nil {
		record.ErrorCode = problemCode(err)
	}
	sendGovernanceAudit(ctx, record)
}

func targetFromBoundary(input any, output any) (string, string) {
	if targetID, targetDisplay := targetFromValue(output); targetID != "" || targetDisplay != "" {
		return targetID, targetDisplay
	}
	return targetFromValue(input)
}

func compactAuditDetail(values map[string]any) map[string]any {
	detail := make(map[string]any, len(values))
	for key, value := range values {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				detail[key] = typed
			}
		case nil:
		default:
			detail[key] = value
		}
	}
	return detail
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
	rules   map[runtimeiam.RateLimitClass]rateLimitRule
	windows map[string]rateLimitWindow
}

var apiOperationRateLimiter = newFixedWindowOperationRateLimiter(map[runtimeiam.RateLimitClass]rateLimitRule{
	rateLimitRead:                       {Limit: 600, Window: time.Minute},
	rateLimitOrganizationMutation:       {Limit: 30, Window: time.Minute},
	rateLimitMemberMutation:             {Limit: 60, Window: time.Minute},
	rateLimitMemberCapabilitiesMutation: {Limit: 30, Window: time.Minute},
	rateLimitIAMPolicyMutation:          {Limit: 30, Window: time.Minute},
	rateLimitAPICredentialMutation:      {Limit: 30, Window: time.Minute},
})

func newFixedWindowOperationRateLimiter(rules map[runtimeiam.RateLimitClass]rateLimitRule) *fixedWindowOperationRateLimiter {
	copied := make(map[runtimeiam.RateLimitClass]rateLimitRule, len(rules))
	for class, rule := range rules {
		copied[class] = rule
	}
	return &fixedWindowOperationRateLimiter{rules: copied, windows: map[string]rateLimitWindow{}}
}

func (l *fixedWindowOperationRateLimiter) allow(class runtimeiam.RateLimitClass, key string, now time.Time) rateLimitDecision {
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
	key = string(class) + "\x00" + key
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
