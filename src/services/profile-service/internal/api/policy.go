package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/profile-service/internal/profile"
	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
	workloadauth "github.com/verself/service-runtime/workload"
)

const (
	idempotencyHeaderKey    = runtimeiam.IdempotencyHeaderKey
	maxIdempotencyKeyLength = 128
)

var apiTracer = otel.Tracer("profile-service/internal/api")

var apiOperationRateLimiter = runtimeiam.NewFixedWindowOperationRateLimiter(map[runtimeiam.RateLimitClass]runtimeiam.RateLimitRule{
	"read":                 {Limit: 6000, Window: time.Minute},
	"profile_mutation":     {Limit: 600, Window: time.Minute},
	"internal_data_rights": {Limit: 600, Window: time.Minute},
})

type profileOperationPolicy struct {
	runtimeiam.OperationPolicy
	Internal bool
}

type operationRequestInfoKey struct{}

type operationRequestInfo struct {
	ClientIP       string
	UserAgent      string
	IdempotencyKey string
}

func registerProfileRoute[I, O any](api huma.API, authorizer runtimeiam.OperationAuthorizer, op huma.Operation, policy profileOperationPolicy, handler func(context.Context, *I) (*O, error)) {
	if op.OperationID == "" {
		panic("missing operation ID for profile API route")
	}
	op = withOperationPolicy(op, policy)
	op.Middlewares = append(op.Middlewares, operationRequestMiddleware)
	huma.Register(api, op, func(ctx context.Context, input *I) (*O, error) {
		ctx, span := startOperationSpan(ctx, op.OperationID, policy)
		defer span.End()
		authIdentity, err := enforceOperationPolicy(ctx, authorizer, policy)
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

func startOperationSpan(ctx context.Context, operationID string, policy profileOperationPolicy) (context.Context, trace.Span) {
	return apiTracer.Start(ctx, policy.AuditEvent.String(), trace.WithAttributes(
		attribute.String("profile.operation_id", operationID),
		attribute.String("profile.permission", string(policy.Permission)),
		attribute.String("profile.resource", string(policy.Resource)),
		attribute.String("profile.action", string(policy.Action)),
		attribute.String("profile.audit_event", string(policy.AuditEvent)),
		attribute.Bool("profile.internal", policy.Internal),
	))
}

func setIdentitySpanAttributes(span trace.Span, identity *auth.Identity) {
	if span == nil || identity == nil {
		return
	}
	span.SetAttributes(
		attribute.String("verself.org_id", identity.OrgID),
		attribute.String("verself.subject_id", identity.Subject),
	)
}

func finishOperationSpan(span trace.Span, identity *auth.Identity, policy profileOperationPolicy, outcome string, err error) {
	if span == nil {
		return
	}
	setIdentitySpanAttributes(span, identity)
	span.SetAttributes(
		attribute.String("profile.outcome", outcome),
		attribute.String("profile.rate_limit_class", string(policy.RateLimitClass)),
	)
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String("profile.error_code", stableErrorCode(err)))
		if outcome != "denied" {
			span.SetStatus(codes.Error, stableErrorCode(err))
		}
	}
}

func withOperationPolicy(op huma.Operation, policy profileOperationPolicy) huma.Operation {
	if err := policy.ValidateHTTPOperation(op.Method, op.OperationID); err != nil {
		panic(err)
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
	iam := policy.OpenAPIExtension()
	op.Extensions["x-verself-iam"] = iam
	if policy.Internal {
		op.Security = []map[string][]string{{"mutualTLS": {}}}
	} else {
		op.Security = []map[string][]string{{"bearerAuth": {}}}
	}
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

func enforceOperationPolicy(ctx context.Context, authorizer runtimeiam.OperationAuthorizer, policy profileOperationPolicy) (*auth.Identity, error) {
	if policy.Internal {
		peerID, ok := workloadauth.PeerIDFromContext(ctx)
		if !ok {
			return nil, unauthorized(ctx)
		}
		if err := requireOperationIdempotency(ctx, policy); err != nil {
			return nil, err
		}
		if decision := apiOperationRateLimiter.Allow(policy.RateLimitClass, operationRateLimitKey(nil, peerID.String()), time.Now()); !decision.Allowed {
			return nil, rateLimitExceeded(ctx, decision.RetryAfter)
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
	if authorizer == nil {
		return identity, problem(ctx, http.StatusServiceUnavailable, "iam-authorizer-unavailable", "IAM authorizer unavailable", runtimeiam.ErrAuthorizerUnavailable)
	}
	decision, err := authorizer.AuthorizeOperation(ctx, identity, policy.OperationPolicy)
	if err != nil {
		return identity, problem(ctx, http.StatusServiceUnavailable, "iam-authorizer-unavailable", "IAM authorization check failed", err)
	}
	if !decision.Allowed {
		return identity, forbidden(ctx, "permission-denied", "missing required profile permission")
	}
	if err := requireOperationIdempotency(ctx, policy); err != nil {
		return identity, err
	}
	if decision := apiOperationRateLimiter.Allow(policy.RateLimitClass, operationRateLimitKey(identity, ""), time.Now()); !decision.Allowed {
		return identity, rateLimitExceeded(ctx, decision.RetryAfter)
	}
	return identity, nil
}

func requireOperationIdempotency(ctx context.Context, policy profileOperationPolicy) error {
	if policy.Idempotency == runtimeiam.IdempotencyNone {
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

func operationRateLimitKey(identity *auth.Identity, internalSubject string) string {
	if identity == nil {
		subject := strings.TrimSpace(internalSubject)
		if subject == "" {
			return "anonymous"
		}
		return subject
	}
	key := strings.TrimSpace(identity.OrgID) + "\x00" + strings.TrimSpace(identity.Subject)
	if strings.Trim(key, "\x00") == "" {
		return "anonymous"
	}
	return key
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

func auditOperation(ctx context.Context, operationID string, policy profileOperationPolicy, identity *auth.Identity, input any, output any, outcome string, err error) {
	orgID := ""
	actorID := ""
	actorType := "service"
	if identity != nil {
		orgID = identity.OrgID
		actorID = identity.Subject
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
	info := operationRequestInfoFromContext(ctx)
	details := auditDetailsFromOutput(output)
	sendGovernanceAudit(ctx, governanceAuditRecord{
		OrgID:       orgID,
		EventSource: "profile-service",
		EventName:   operationID,
		AuditEvent:  string(policy.AuditEvent),
		ActorType:   actorType,
		ActorID:     actorID,
		Permission:  string(policy.Permission),
		TargetType:  string(policy.Resource),
		TargetID:    targetIDFromInput(input, identity),
		Outcome:     outcome,
		ErrorCode:   stableErrorCode(err),
		Detail: compactAuditDetail(map[string]any{
			"idempotency_key_hash": hashTextForAudit(info.IdempotencyKey),
			"changed_fields":       strings.Join(details.changedFields, ","),
			"before_hash":          details.beforeHash,
			"after_hash":           details.afterHash,
			"artifact_sha256":      details.artifactSHA256,
			"artifact_bytes":       details.artifactBytes,
		}),
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

func compactAuditDetail(values map[string]any) map[string]any {
	detail := make(map[string]any, len(values))
	for key, value := range values {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				detail[key] = typed
			}
		case uint64:
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
		return contractString(typed.Body.OrgID)
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
		return string(typed.Body.RequestedBy)
	}
	return ""
}

func targetIDFromInput(input any, identity *auth.Identity) string {
	if identity != nil {
		return identity.Subject
	}
	if typed, ok := any(input).(*dataRightsInput); ok {
		if subjectID := contractString(typed.Body.SubjectID); subjectID != "" {
			return subjectID
		}
		return contractString(typed.Body.OrgID)
	}
	if typed, ok := any(input).(*dataRightsStatusInput); ok {
		return string(typed.RequestID)
	}
	return ""
}

func sortedChangedFields(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func versionHash(resource string, version int32) string {
	return hashTextForAudit(fmt.Sprintf("%s:%d", resource, version))
}
