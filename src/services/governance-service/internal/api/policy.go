package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/governance-service/internal/governance"
	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
	"github.com/verself/service-runtime/requestmeta"
)

type permission = runtimeiam.Permission

const (
	permissionAPIActivityRead   permission = "governance:api_activity:read"
	permissionAPIActivityExport permission = "governance:api_activity:export"

	idempotencyHeaderKey    = runtimeiam.IdempotencyHeaderKey
	maxIdempotencyKeyLength = 128

	apiActivityResourceType = "api_activity"
	orgResourceType         = "org"
	parentOrgRelation       = "parent_org"
)

type securedOperation struct {
	Operation huma.Operation
	Policy    runtimeiam.OperationPolicy
}

func secured(op huma.Operation, policy runtimeiam.OperationPolicy) securedOperation {
	return securedOperation{Operation: op, Policy: policy}
}

func registerSecured[I, O any](api huma.API, svc *governance.Service, authorizer runtimeiam.ResourceAuthorizer, securedOp securedOperation, handler func(context.Context, governance.Principal, *I) (*O, error)) {
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
			recordAPIActivity(ctx, svc, op, policy, principal, input, nil, "denied", err)
			return nil, err
		}
		output, err := handler(ctx, principal, input)
		if err != nil {
			mapped := mapError(ctx, err)
			recordAPIActivity(ctx, svc, op, policy, principal, input, nil, "error", mapped)
			return nil, mapped
		}
		recordAPIActivity(ctx, svc, op, policy, principal, input, output, "allowed", nil)
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

func enforceOperationPolicy(ctx context.Context, authorizer runtimeiam.ResourceAuthorizer, policy runtimeiam.OperationPolicy) (governance.Principal, error) {
	authIdentity, err := requireIdentity(ctx)
	if err != nil {
		return governance.Principal{}, err
	}
	principal := principalFromIdentity(authIdentity)
	if authorizer == nil {
		return principal, problem(ctx, http.StatusServiceUnavailable, "service.unavailable", "IAM authorizer unavailable", runtimeiam.ErrAuthorizerUnavailable)
	}
	resourcePermission, err := apiActivityResourcePermission(policy.Permission)
	if err != nil {
		return principal, problem(ctx, http.StatusServiceUnavailable, "service.unavailable", "governance IAM policy is invalid", err)
	}
	orgID := strings.TrimSpace(authIdentity.OrgID)
	apiActivityResource := runtimeiam.ResourceRef{Type: apiActivityResourceType, ID: orgID}
	_, err = authorizer.EnsureResourceParent(ctx, runtimeiam.ResourceParentEdgeRequest{
		OrgID:     orgID,
		Resource:  apiActivityResource,
		Relation:  parentOrgRelation,
		Parent:    runtimeiam.ResourceRef{Type: orgResourceType, ID: orgID},
		Operation: string(policy.Permission),
	})
	if err != nil {
		return principal, problem(ctx, http.StatusServiceUnavailable, "service.unavailable", "IAM API activity resource write failed", err)
	}
	decision, err := authorizer.AuthorizeResource(ctx, authIdentity, runtimeiam.ResourceAuthorizationRequest{
		OrgID:               orgID,
		OperationPermission: policy.Permission,
		Resource:            apiActivityResource,
		ResourcePermission:  resourcePermission,
	})
	if err != nil {
		return principal, problem(ctx, http.StatusServiceUnavailable, "service.unavailable", "IAM authorization check failed", err)
	}
	if !decision.Allowed {
		return principal, forbidden(ctx, "auth.permission_denied", fmt.Sprintf("missing required permission %q", policy.Permission))
	}
	if err := requireOperationIdempotency(ctx, policy); err != nil {
		return principal, err
	}
	if decision := apiOperationRateLimiter.Allow(policy.RateLimitClass, operationRateLimitKey(ctx, authIdentity, policy), time.Now()); !decision.Allowed {
		return principal, rateLimitExceeded(ctx, decision.RetryAfter)
	}
	return principal, nil
}

func apiActivityResourcePermission(productPermission permission) (string, error) {
	switch productPermission {
	case permissionAPIActivityRead:
		return "read", nil
	case permissionAPIActivityExport:
		return "export", nil
	default:
		return "", fmt.Errorf("unsupported governance permission %q", productPermission)
	}
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
	Attribution    requestmeta.Attribution
}

func operationRequestMiddleware(ctx huma.Context, next func(huma.Context)) {
	attribution := requestmeta.FromValues(ctx.Header, ctx.RemoteAddr())
	trustedHops := uint8(0)
	if attribution.Trusted {
		trustedHops = 1
	}
	info := operationRequestInfo{
		ClientIP:       attribution.ClientIP,
		IPChain:        attribution.ClientIP,
		TrustedHops:    trustedHops,
		UserAgent:      strings.TrimSpace(ctx.Header("User-Agent")),
		IdempotencyKey: strings.TrimSpace(ctx.Header("Idempotency-Key")),
		RefererOrigin:  originFromURL(ctx.Header("Referer")),
		Origin:         strings.TrimSpace(ctx.Header("Origin")),
		Host:           strings.TrimSpace(ctx.Header("Host")),
		RequestID:      firstHeader(ctx, "X-Request-ID", "X-Correlation-ID", "Fly-Request-Id", "Cf-Ray"),
		Attribution:    attribution,
	}
	nextCtx := requestmeta.WithAttribution(ctx.Context(), attribution)
	nextCtx = context.WithValue(nextCtx, operationRequestInfoKey{}, info)
	requestmeta.AnnotateSpan(nextCtx, attribution)
	next(huma.WithContext(ctx, nextCtx))
}

func requireOperationIdempotency(ctx context.Context, policy runtimeiam.OperationPolicy) error {
	if policy.Idempotency == runtimeiam.IdempotencyNone {
		return nil
	}
	value := operationRequestInfoFromContext(ctx).IdempotencyKey
	if strings.TrimSpace(value) == "" {
		return problem(ctx, http.StatusBadRequest, "request.validation_failed", "Idempotency-Key is required for this operation", nil)
	}
	if len(value) > maxIdempotencyKeyLength || strings.ContainsAny(value, "\x00\r\n\t") {
		return problem(ctx, http.StatusBadRequest, "request.validation_failed", "Idempotency-Key is invalid", nil)
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

func recordAPIActivity(ctx context.Context, svc *governance.Service, op huma.Operation, policy runtimeiam.OperationPolicy, principal governance.Principal, input any, output any, outcome string, err error) {
	if svc == nil || principal.OrgID == "" {
		return
	}
	info := operationRequestInfoFromContext(ctx)
	targetID, targetDisplay := targetFromBoundary(input, output)
	httpStatus := httpStatusForActivity(op, outcome, err)
	httpResponseCode, conversionErr := httpResponseCodeUint16(httpStatus)
	if conversionErr != nil {
		slog.Default().ErrorContext(ctx, "governance API activity response code conversion failed", "error", conversionErr, "api_event_code", policy.AuditEvent)
		return
	}
	decision, status := apiActivityResult(outcome)
	record := governance.APIActivityRecord{
		OrgID:            principal.OrgID,
		APIService:       "governance-service",
		APIOperation:     op.OperationID,
		APIEventCode:     string(policy.AuditEvent),
		APIAction:        string(policy.Action),
		ActorType:        principal.Type,
		ActorID:          principal.Subject,
		ActorDisplayName: firstNonEmpty(principal.Email, principal.Subject),
		ActorEmail:       principal.Email,
		CredentialID:     principal.CredentialID,
		Permission:       string(policy.Permission),
		Resources: []governance.APIActivityResource{{
			Type:     string(policy.Resource),
			UID:      targetID,
			Name:     targetDisplay,
			FullName: governanceResourceName(svc.InstallationID, principal.OrgID, string(policy.Resource), targetID),
		}},
		HTTPRequest: governance.APIActivityHTTPRequest{
			UID:           info.RequestID,
			Method:        op.Method,
			Route:         op.Path,
			SafeParams:    safeParamsFromInput(input),
			UserAgent:     info.UserAgent,
			XForwardedFor: info.IPChain,
			Referrer:      info.RefererOrigin,
			Host:          info.Host,
			ClientIP:      info.ClientIP,
		},
		HTTPResponse: governance.APIActivityHTTPResponse{
			Code:    httpResponseCode,
			Message: http.StatusText(httpStatus),
			Status:  http.StatusText(httpStatus),
		},
		AuthorizationDecision: decision,
		Status:                status,
		StatusCode:            firstNonEmpty(problemCodeOrEmpty(err), strconv.Itoa(httpStatus)),
		Unmapped: compactUnmappedDetail(map[string]any{
			"verself.idempotency_key_hash":   governanceHashForAPI(info.IdempotencyKey),
			"verself.credential_fingerprint": principal.CredentialFingerprint,
			"verself.trusted_proxy_hops":     info.TrustedHops,
			"verself.client_ip_source":       info.Attribution.ClientIPSource,
			"verself.client_ip_trusted":      info.Attribution.Trusted,
			"verself.edge_peer_ip":           info.Attribution.EdgePeerIP,
			"verself.rejected_ip_headers":    strings.Join(info.Attribution.RejectedHeaders, ","),
			"verself.origin":                 info.Origin,
		}),
	}
	if err != nil {
		if outcome == "denied" {
			record.StatusDetail = problemCode(err)
		}
	}
	if _, recordErr := svc.RecordAPIActivity(ctx, record); recordErr != nil {
		slog.Default().ErrorContext(ctx, "governance API activity write failed", "error", recordErr, "api_event_code", policy.AuditEvent, "org_id", principal.OrgID)
	}
}

func governanceResourceName(installationID, orgID, resource, targetID string) string {
	if installationID == "" || orgID == "" {
		return ""
	}
	if resource == "api_activity" && targetID != "" {
		return governance.ResourceNameAPIActivity(installationID, orgID, targetID).String()
	}
	if resource == "data_export" && targetID != "" {
		return governance.ResourceNameDataExport(installationID, orgID, targetID).String()
	}
	return governance.ResourceNameOrg(installationID, orgID).String()
}

func httpResponseCodeUint16(status int) (uint16, error) {
	if status < 0 || status > 65535 {
		return 0, fmt.Errorf("http status out of UInt16 range: %d", status)
	}
	return uint16(status), nil
}

func apiActivityResult(result string) (governance.AuthorizationDecision, governance.Status) {
	switch strings.TrimSpace(result) {
	case "denied":
		return governance.AuthorizationDenied, governance.StatusFailure
	case "error":
		return governance.AuthorizationAllowed, governance.StatusFailure
	default:
		return governance.AuthorizationAllowed, governance.StatusSuccess
	}
}

func httpStatusForActivity(op huma.Operation, result string, err error) int {
	if err != nil {
		var statusErr huma.StatusError
		if errors.As(err, &statusErr) && statusErr.GetStatus() > 0 {
			return statusErr.GetStatus()
		}
	}
	if result == "denied" {
		return http.StatusForbidden
	}
	if op.DefaultStatus > 0 {
		return op.DefaultStatus
	}
	return http.StatusOK
}

func problemCodeOrEmpty(err error) string {
	if err == nil {
		return ""
	}
	return problemCode(err)
}

func compactUnmappedDetail(values map[string]any) map[string]any {
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

func safeParamsFromInput(input any) string {
	values := url.Values{}
	addSafeParams(values, reflectValue(input))
	encoded := values.Encode()
	if len(encoded) > 1024 {
		return encoded[:1024]
	}
	return encoded
}

func addSafeParams(values url.Values, value reflect.Value) {
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return
	}
	valueType := value.Type()
	for index := 0; index < value.NumField(); index++ {
		field := valueType.Field(index)
		if field.PkgPath != "" {
			continue
		}
		name := firstNonEmpty(field.Tag.Get("query"), field.Tag.Get("path"))
		if name == "" || name == "-" {
			continue
		}
		item := reflectValue(value.Field(index).Interface())
		if !item.IsValid() {
			continue
		}
		switch item.Kind() {
		case reflect.String:
			if text := strings.TrimSpace(item.String()); text != "" {
				values.Set(name, text)
			}
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if item.Int() != 0 {
				values.Set(name, strconv.FormatInt(item.Int(), 10))
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if item.Uint() != 0 {
				values.Set(name, strconv.FormatUint(item.Uint(), 10))
			}
		}
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

func problemCode(err error) string {
	var verselfErr *verselfProblem
	if errors.As(err, &verselfErr) && verselfErr.Code != "" {
		return verselfErr.Code
	}
	var model *huma.ErrorModel
	if !errors.As(err, &model) {
		return "service.unavailable"
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
	return "service.unavailable"
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
	"read":              {Limit: 6000, Window: time.Minute},
	"export_create":     {Limit: 12, Window: time.Hour},
	"export_download":   {Limit: 60, Window: time.Minute},
	"internal_mutation": {Limit: 600, Window: time.Minute},
})

func rateLimitExceeded(ctx context.Context, retryAfter time.Duration) error {
	err := problem(ctx, http.StatusTooManyRequests, "quota.rate_limited", "rate limit exceeded", nil)
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
		Description:  "Zitadel-issued bearer token scoped to the Verself product API audience.",
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
