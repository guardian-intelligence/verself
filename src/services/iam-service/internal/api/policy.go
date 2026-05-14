package api

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	governanceinternalclient "github.com/verself/governance-service/internalclient"
	"github.com/verself/iam-service/internal/authz"
	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
)

const (
	idempotencyHeaderKey    = runtimeiam.IdempotencyHeaderKey
	maxIdempotencyKeyLength = 128
)

const (
	rateLimitRead        runtimeiam.RateLimitClass = "read"
	rateLimitIAMMutation runtimeiam.RateLimitClass = "iam_mutation"
)

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

func authorizedOrgIDs(ctx context.Context, authzSvc *authz.Service, authIdentity *auth.Identity, orgIDs []string, required runtimeiam.Permission) ([]string, error) {
	if required == "" {
		return nil, nil
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

func operationRequestInfoFromContext(ctx context.Context) operationRequestInfo {
	info, _ := ctx.Value(operationRequestInfoKey{}).(operationRequestInfo)
	return info
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
	decision, status := apiActivityResult(outcome)
	record := governanceAPIActivity{
		OrgID:                 identity.OrgID,
		APIService:            "iam-service",
		APIOperation:          op.OperationID,
		APIEventCode:          string(policy.AuditEvent),
		APIAction:             string(policy.Action),
		ActorType:             principalType,
		ActorUID:              firstNonEmpty(serviceAccountID, identity.Subject),
		ActorName:             firstNonEmpty(identity.Email, identity.Subject),
		ActorEmail:            identity.Email,
		CredentialUID:         credentialID,
		Permission:            string(policy.Permission),
		ResourceType:          string(policy.Resource),
		ResourceUID:           targetID,
		ResourceName:          targetDisplay,
		HTTPMethod:            op.Method,
		HTTPRoute:             op.Path,
		HTTPSafeParams:        "",
		HTTPStatus:            httpStatusFromOperationResult(outcome, err),
		AuthorizationDecision: decision,
		Status:                status,
		StatusCode:            firstNonEmpty(problemCodeOrEmpty(err), strconv.Itoa(int(httpStatusFromOperationResult(outcome, err)))),
		Unmapped: compactAPIActivityUnmapped(map[string]any{
			"verself.idempotency_key_hash": hashTextForAPIActivity(info.IdempotencyKey),
		}),
	}
	if err != nil {
		record.StatusDetail = problemCode(err)
	}
	sendGovernanceAPIActivity(ctx, record)
}

func apiActivityResult(outcome string) (governanceinternalclient.AuthorizationDecision, governanceinternalclient.APIActivityStatus) {
	switch strings.TrimSpace(outcome) {
	case "denied":
		return governanceinternalclient.AuthorizationDecisionDenied, governanceinternalclient.APIActivityStatusFailure
	case "error":
		return governanceinternalclient.AuthorizationDecisionAllowed, governanceinternalclient.APIActivityStatusFailure
	default:
		return governanceinternalclient.AuthorizationDecisionAllowed, governanceinternalclient.APIActivityStatusSuccess
	}
}

func httpStatusFromOperationResult(outcome string, err error) uint16 {
	if err != nil {
		var statusErr huma.StatusError
		if errors.As(err, &statusErr) {
			status := statusErr.GetStatus()
			if status >= http.StatusContinue && status <= 599 {
				return uint16(status)
			}
		}
	}
	if outcome == "denied" {
		return http.StatusForbidden
	}
	if outcome == "error" {
		return http.StatusInternalServerError
	}
	return http.StatusOK
}

func problemCodeOrEmpty(err error) string {
	if err == nil {
		return ""
	}
	return problemCode(err)
}

func targetFromBoundary(input any, output any) (string, string) {
	if targetID, targetDisplay := targetFromValue(output); targetID != "" || targetDisplay != "" {
		return targetID, targetDisplay
	}
	return targetFromValue(input)
}

func compactAPIActivityUnmapped(values map[string]any) map[string]any {
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
	for _, fieldName := range []string{"MemberID", "CredentialID", "UserID", "OrgID", "ID"} {
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
		for _, fieldName := range []string{"MemberID", "CredentialID", "UserID", "OrgID", "ID"} {
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

var apiOperationRateLimiter = runtimeiam.NewFixedWindowOperationRateLimiter(map[runtimeiam.RateLimitClass]runtimeiam.RateLimitRule{
	rateLimitRead:        {Limit: 6000, Window: time.Minute},
	rateLimitIAMMutation: {Limit: 600, Window: time.Minute},
})

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
