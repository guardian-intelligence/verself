package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	auth "github.com/forge-metal/auth-middleware"
	"github.com/forge-metal/profile-service/internal/profile"
)

type permission string

const (
	permissionProfileRead        permission = "profile:self:read"
	permissionProfileIdentity    permission = "profile:self:identity:write"
	permissionProfilePreferences permission = "profile:self:preferences:write"
	permissionProfileDataRights  permission = "profile:data_rights:write"

	idempotencyHeaderKey    = "idempotency_key_header"
	maxIdempotencyKeyLength = 128
	bodyLimitSmallJSON      = 16 << 10
	bodyLimitDataRightsJSON = 64 << 10
)

var apiTracer = otel.Tracer("profile-service/internal/api")

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
	Internal           bool
}

type operationRequestInfoKey struct{}

type operationRequestInfo struct {
	ClientIP       string
	UserAgent      string
	IdempotencyKey string
}

func registerProfileRoute[I, O any](api huma.API, op huma.Operation, policy operationPolicy, handler func(context.Context, *I) (*O, error)) {
	if op.OperationID == "" {
		panic("missing operation ID for profile API route")
	}
	policy = normalizeOperationPolicy(op.OperationID, policy)
	op = withOperationPolicy(op, policy)
	op.Middlewares = append(op.Middlewares, operationRequestMiddleware)
	huma.Register(api, op, func(ctx context.Context, input *I) (*O, error) {
		ctx, span := startOperationSpan(ctx, op.OperationID, policy)
		defer span.End()
		authIdentity, err := enforceOperationPolicy(ctx, policy)
		if err != nil {
			finishOperationSpan(span, authIdentity, policy, "denied", err)
			auditOperation(ctx, op.OperationID, policy, authIdentity, input, nil, "denied", err)
			return nil, err
		}
		setIdentitySpanAttributes(span, authIdentity)
		output, err := handler(ctx, input)
		if err != nil {
			finishOperationSpan(span, authIdentity, policy, "error", err)
			auditOperation(ctx, op.OperationID, policy, authIdentity, input, nil, "error", err)
			return nil, err
		}
		finishOperationSpan(span, authIdentity, policy, "allowed", nil)
		auditOperation(ctx, op.OperationID, policy, authIdentity, input, output, "allowed", nil)
		return output, nil
	})
}

func startOperationSpan(ctx context.Context, operationID string, policy operationPolicy) (context.Context, trace.Span) {
	return apiTracer.Start(ctx, policy.AuditEvent, trace.WithAttributes(
		attribute.String("profile.operation_id", operationID),
		attribute.String("profile.permission", string(policy.Permission)),
		attribute.String("profile.resource", policy.Resource),
		attribute.String("profile.action", policy.Action),
		attribute.String("profile.audit_event", policy.AuditEvent),
		attribute.Bool("profile.internal", policy.Internal),
	))
}

func setIdentitySpanAttributes(span trace.Span, identity *auth.Identity) {
	if span == nil || identity == nil {
		return
	}
	span.SetAttributes(
		attribute.String("forge_metal.org_id", identity.OrgID),
		attribute.String("forge_metal.subject_id", identity.Subject),
	)
}

func finishOperationSpan(span trace.Span, identity *auth.Identity, policy operationPolicy, outcome string, err error) {
	if span == nil {
		return
	}
	setIdentitySpanAttributes(span, identity)
	span.SetAttributes(
		attribute.String("profile.outcome", outcome),
		attribute.String("profile.rate_limit_class", policy.RateLimitClass),
	)
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String("profile.error_code", stableErrorCode(err)))
		if outcome != "denied" {
			span.SetStatus(codes.Error, stableErrorCode(err))
		}
	}
}

func normalizeOperationPolicy(operationID string, policy operationPolicy) operationPolicy {
	if policy.SourceProductArea == "" {
		policy.SourceProductArea = "Profile"
	}
	if policy.OperationDisplay == "" {
		policy.OperationDisplay = operationID
	}
	if policy.OperationType == "" {
		policy.OperationType = "write"
	}
	if policy.EventCategory == "" {
		policy.EventCategory = "profile"
	}
	if policy.RiskLevel == "" {
		policy.RiskLevel = "medium"
	}
	if policy.DataClassification == "" {
		policy.DataClassification = "controller_personal_data"
	}
	return policy
}

func withOperationPolicy(op huma.Operation, policy operationPolicy) huma.Operation {
	if policy.Permission == "" || policy.Resource == "" || policy.Action == "" || policy.OrgScope == "" || policy.RateLimitClass == "" || policy.AuditEvent == "" {
		panic("incomplete profile operation policy for " + op.OperationID)
	}
	if operationRequiresBodyBudget(op) && policy.BodyLimitBytes <= 0 {
		panic("missing body limit for mutating operation " + op.OperationID)
	}
	if policy.BodyLimitBytes > 0 {
		op.MaxBodyBytes = policy.BodyLimitBytes
	}
	if policy.Idempotency == idempotencyHeaderKey {
		op.Parameters = appendIdempotencyKeyHeaderParameter(op.Parameters)
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
	op.Extensions["x-forge-metal-iam"] = iam
	if policy.Internal {
		op.Security = []map[string][]string{{"mutualTLS": {}}}
	} else {
		op.Security = []map[string][]string{{"bearerAuth": {}}}
	}
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

func enforceOperationPolicy(ctx context.Context, policy operationPolicy) (*auth.Identity, error) {
	if policy.Internal {
		if err := requireOperationIdempotency(ctx, policy); err != nil {
			return nil, err
		}
		return nil, nil
	}
	identity := auth.FromContext(ctx)
	if identity == nil {
		return nil, unauthorized(ctx)
	}
	principal := profile.Principal{Subject: identity.Subject, OrgID: identity.OrgID, Email: identity.Email, Raw: identity.Raw}
	if err := profile.ValidatePrincipal(principal); err != nil {
		return identity, forbidden(ctx, "human-profile-required", "human profile routes require a human subject token")
	}
	if err := requireOperationIdempotency(ctx, policy); err != nil {
		return identity, err
	}
	return identity, nil
}

func requireOperationIdempotency(ctx context.Context, policy operationPolicy) error {
	if policy.Idempotency == "" {
		return nil
	}
	value := strings.TrimSpace(operationRequestInfoFromContext(ctx).IdempotencyKey)
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

func operationRequestMiddleware(ctx huma.Context, next func(huma.Context)) {
	info := operationRequestInfo{
		ClientIP:       clientIPFromHuma(ctx),
		UserAgent:      strings.TrimSpace(ctx.Header("User-Agent")),
		IdempotencyKey: strings.TrimSpace(ctx.Header("Idempotency-Key")),
	}
	next(huma.WithValue(ctx, operationRequestInfoKey{}, info))
}

func operationRequestInfoFromContext(ctx context.Context) operationRequestInfo {
	info, _ := ctx.Value(operationRequestInfoKey{}).(operationRequestInfo)
	return info
}

func auditOperation(ctx context.Context, operationID string, policy operationPolicy, identity *auth.Identity, input any, output any, outcome string, err error) {
	orgID := ""
	actorID := ""
	actorDisplay := ""
	actorType := "service"
	if identity != nil {
		orgID = identity.OrgID
		actorID = identity.Subject
		actorDisplay = identity.Email
		actorType = "user"
	}
	if orgID == "" {
		orgID = orgIDFromInput(input)
	}
	if actorID == "" {
		actorID = actorIDFromInput(input)
	}
	if actorID == "" {
		actorID = "profile-service"
	}
	result := outcome
	decision := "allow"
	denialReason := ""
	errorMessage := ""
	if outcome == "denied" {
		decision = "deny"
		denialReason = stableErrorCode(err)
	}
	if err != nil && outcome != "denied" {
		errorMessage = stableErrorCode(err)
	}
	info := operationRequestInfoFromContext(ctx)
	details := auditDetailsFromOutput(output)
	sendGovernanceAudit(ctx, governanceAuditRecord{
		OrgID:              orgID,
		SourceProductArea:  policy.SourceProductArea,
		ServiceName:        "profile-service",
		OperationID:        operationID,
		AuditEvent:         policy.AuditEvent,
		OperationDisplay:   policy.OperationDisplay,
		OperationType:      policy.OperationType,
		EventCategory:      policy.EventCategory,
		RiskLevel:          policy.RiskLevel,
		DataClassification: policy.DataClassification,
		ActorType:          actorType,
		ActorID:            actorID,
		ActorDisplay:       actorDisplay,
		AuthMethod:         "oidc",
		Permission:         string(policy.Permission),
		TargetKind:         policy.Resource,
		TargetID:           targetIDFromInput(input, identity),
		Action:             policy.Action,
		OrgScope:           policy.OrgScope,
		RateLimitClass:     policy.RateLimitClass,
		Decision:           decision,
		Result:             result,
		DenialReason:       denialReason,
		ErrorCode:          stableErrorCode(err),
		ErrorMessage:       errorMessage,
		ClientIP:           info.ClientIP,
		UserAgentRaw:       info.UserAgent,
		RouteTemplate:      routeTemplateFromOperationID(operationID),
		HTTPMethod:         methodFromOperationID(operationID),
		IdempotencyKeyHash: hashTextForAudit(info.IdempotencyKey),
		ChangedFields:      strings.Join(details.changedFields, ","),
		BeforeHash:         details.beforeHash,
		AfterHash:          details.afterHash,
		ArtifactSHA256:     details.artifactSHA256,
		ArtifactBytes:      details.artifactBytes,
	})
}

type auditDetails struct {
	changedFields  []string
	beforeHash     string
	afterHash      string
	artifactSHA256 string
	artifactBytes  uint64
}

type auditDetailer interface {
	auditDetails() auditDetails
}

func auditDetailsFromOutput(output any) auditDetails {
	if output == nil {
		return auditDetails{}
	}
	if detailer, ok := any(output).(auditDetailer); ok {
		return detailer.auditDetails()
	}
	value := reflect.ValueOf(output)
	if value.Kind() == reflect.Pointer && !value.IsNil() {
		if detailer, ok := value.Interface().(auditDetailer); ok {
			return detailer.auditDetails()
		}
	}
	return auditDetails{}
}

func stableErrorCode(err error) string {
	if err == nil {
		return ""
	}
	if model, ok := err.(*huma.ErrorModel); ok {
		return strings.TrimPrefix(model.Type, problemTypePrefix)
	}
	return reflect.TypeOf(err).String()
}

func principalFromContext(ctx context.Context) (profile.Principal, error) {
	identity := auth.FromContext(ctx)
	if identity == nil {
		return profile.Principal{}, unauthorized(ctx)
	}
	principal := profile.Principal{Subject: identity.Subject, OrgID: identity.OrgID, Email: identity.Email, Raw: identity.Raw}
	if err := profile.ValidatePrincipal(principal); err != nil {
		return profile.Principal{}, forbidden(ctx, "human-profile-required", "human profile routes require a human subject token")
	}
	return principal, nil
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

func orgIDFromInput(input any) string {
	if input == nil {
		return ""
	}
	switch typed := any(input).(type) {
	case *dataRightsInput:
		return typed.Body.OrgID
	case *dataRightsStatusInput:
		return ""
	default:
		return ""
	}
}

func actorIDFromInput(input any) string {
	if input == nil {
		return ""
	}
	if typed, ok := any(input).(*dataRightsInput); ok {
		return typed.Body.RequestedBy
	}
	return ""
}

func targetIDFromInput(input any, identity *auth.Identity) string {
	if identity != nil {
		return identity.Subject
	}
	if typed, ok := any(input).(*dataRightsInput); ok {
		if typed.Body.SubjectID != "" {
			return typed.Body.SubjectID
		}
		return typed.Body.OrgID
	}
	if typed, ok := any(input).(*dataRightsStatusInput); ok {
		return typed.RequestID
	}
	return ""
}

func routeTemplateFromOperationID(operationID string) string {
	switch operationID {
	case "get-profile":
		return "/api/v1/profile"
	case "patch-profile-identity":
		return "/api/v1/profile/identity"
	case "put-profile-preferences":
		return "/api/v1/profile/preferences"
	case "profile-org-export":
		return "/internal/v1/data-rights/org-export"
	case "profile-subject-export":
		return "/internal/v1/data-rights/subject-export"
	case "profile-subject-erasure":
		return "/internal/v1/data-rights/subject-erasure"
	case "profile-data-rights-status":
		return "/internal/v1/data-rights/requests/{request_id}"
	default:
		return ""
	}
}

func methodFromOperationID(operationID string) string {
	if strings.Contains(operationID, "get-") || strings.Contains(operationID, "status") {
		return http.MethodGet
	}
	if strings.Contains(operationID, "patch-") {
		return http.MethodPatch
	}
	if strings.Contains(operationID, "put-") {
		return http.MethodPut
	}
	return http.MethodPost
}

func sortedChangedFields(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func versionHash(resource string, version int32) string {
	return hashTextForAudit(fmt.Sprintf("%s:%d", resource, version))
}
