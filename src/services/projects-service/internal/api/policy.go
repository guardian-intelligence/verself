package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/projects-service/internal/projects"
	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
	workloadauth "github.com/verself/service-runtime/workload"
)

const (
	idempotencyHeaderKey    = runtimeiam.IdempotencyHeaderKey
	orgScopeTokenOrgID      = runtimeiam.OrgScopeTokenOrgID
	orgScopeRequestOrgID    = runtimeiam.OrgScopeRequestOrgID
	maxIdempotencyKeyLength = 128
	maxOriginSubjectLength  = 256
	maxOriginEmailLength    = 320
	bodyLimitSmallJSON      = 64 << 10

	originOrgIDHeader   = "X-Verself-Origin-Org-ID"
	originSubjectHeader = "X-Verself-Origin-Subject"
	originEmailHeader   = "X-Verself-Origin-Email"
)

var apiTracer = otel.Tracer("projects-service/internal/api")

type projectsOperationPolicy struct {
	runtimeiam.OperationPolicy
	Service      bool
	ServicePeers []string
}

type operationRequestInfoKey struct{}

type operationRequestInfo struct {
	IdempotencyKey string
	OriginOrgID    string
	OriginSubject  string
	OriginEmail    string
}

func registerProjectsRoute[I, O any](api huma.API, authorizer runtimeiam.OperationAuthorizer, op huma.Operation, policy projectsOperationPolicy, handler func(context.Context, projects.Principal, *I) (*O, error)) {
	if op.OperationID == "" {
		panic("missing operation ID for projects API route")
	}
	op = withOperationPolicy(op, policy)
	op.Middlewares = append(op.Middlewares, operationRequestMiddleware)
	huma.Register(api, op, func(ctx context.Context, input *I) (*O, error) {
		ctx, span := startOperationSpan(ctx, op.OperationID, policy)
		defer span.End()
		principal, err := enforceOperationPolicy(ctx, authorizer, policy)
		if err != nil {
			finishOperationSpan(span, principal, policy, "denied", err)
			return nil, err
		}
		output, err := handler(ctx, principal, input)
		if err != nil {
			finishOperationSpan(span, principal, policy, "error", err)
			return nil, err
		}
		finishOperationSpan(span, principal, policy, "allowed", nil)
		return output, nil
	})
}

func startOperationSpan(ctx context.Context, operationID string, policy projectsOperationPolicy) (context.Context, trace.Span) {
	return apiTracer.Start(ctx, policy.AuditEvent.String(), trace.WithAttributes(
		attribute.String("projects.operation_id", operationID),
		attribute.String("projects.permission", string(policy.Permission)),
		attribute.String("projects.resource", string(policy.Resource)),
		attribute.String("projects.action", string(policy.Action)),
		attribute.String("projects.audit_event", string(policy.AuditEvent)),
		attribute.Bool("projects.service_projection", policy.Service),
	))
}

func finishOperationSpan(span trace.Span, principal projects.Principal, policy projectsOperationPolicy, outcome string, err error) {
	if span == nil {
		return
	}
	if principal.OrgID != 0 {
		span.SetAttributes(attribute.Int64("verself.org_id", int64FromUint64(principal.OrgID, "org id")))
	}
	if principal.Subject != "" {
		span.SetAttributes(attribute.String("verself.subject_id", principal.Subject))
	}
	span.SetAttributes(
		attribute.String("projects.outcome", outcome),
		attribute.String("projects.rate_limit_class", string(policy.RateLimitClass)),
	)
	if err != nil {
		span.RecordError(err)
		if outcome != "denied" {
			span.SetStatus(codes.Error, err.Error())
		}
	}
}

func withOperationPolicy(op huma.Operation, policy projectsOperationPolicy) huma.Operation {
	if err := policy.ValidateHTTPOperation(op.Method, op.OperationID); err != nil {
		panic(err)
	}
	if policy.Service && len(policy.ServicePeers) == 0 {
		panic("missing service peer allowlist for " + op.OperationID)
	}
	if policy.BodyLimitBytes > 0 {
		op.MaxBodyBytes = policy.BodyLimitBytes
	}
	if policy.Idempotency == idempotencyHeaderKey {
		op.Parameters = appendIdempotencyKeyHeaderParameter(op.Parameters)
	}
	if policy.Service && policy.OrgScope == orgScopeTokenOrgID {
		op.Parameters = appendOriginHeaderParameters(op.Parameters)
	}
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	op.Extensions["x-verself-iam"] = policy.OpenAPIExtension()
	if policy.Service && policy.OrgScope == orgScopeTokenOrgID {
		op.Extensions["x-verself-origin"] = map[string]any{
			"org_id_header":  originOrgIDHeader,
			"subject_header": originSubjectHeader,
			"email_header":   originEmailHeader,
		}
	}
	if policy.Service {
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
		Schema:      &huma.Schema{Type: "string", MinLength: &minLength, MaxLength: &maxLength},
	})
}

func appendOriginHeaderParameters(parameters []*huma.Param) []*huma.Param {
	parameters = appendRequiredStringHeader(parameters, originOrgIDHeader, "Origin organization ID asserted by the SPIFFE-authenticated caller.", 1, 32)
	parameters = appendRequiredStringHeader(parameters, originSubjectHeader, "Origin subject ID asserted by the SPIFFE-authenticated caller.", 1, maxOriginSubjectLength)
	return appendOptionalStringHeader(parameters, originEmailHeader, "Origin subject email asserted by the SPIFFE-authenticated caller.", maxOriginEmailLength)
}

func appendRequiredStringHeader(parameters []*huma.Param, name, description string, minLength, maxLength int) []*huma.Param {
	for _, param := range parameters {
		if param != nil && strings.EqualFold(param.Name, name) && param.In == "header" {
			param.Required = true
			return parameters
		}
	}
	return append(parameters, &huma.Param{
		Name:        name,
		In:          "header",
		Description: description,
		Required:    true,
		Schema:      &huma.Schema{Type: "string", MinLength: &minLength, MaxLength: &maxLength},
	})
}

func appendOptionalStringHeader(parameters []*huma.Param, name, description string, maxLength int) []*huma.Param {
	for _, param := range parameters {
		if param != nil && strings.EqualFold(param.Name, name) && param.In == "header" {
			return parameters
		}
	}
	return append(parameters, &huma.Param{
		Name:        name,
		In:          "header",
		Description: description,
		Required:    false,
		Schema:      &huma.Schema{Type: "string", MaxLength: &maxLength},
	})
}

func enforceOperationPolicy(ctx context.Context, authorizer runtimeiam.OperationAuthorizer, policy projectsOperationPolicy) (projects.Principal, error) {
	if policy.Service {
		peerID, ok := workloadauth.PeerIDFromContext(ctx)
		if !ok {
			return projects.Principal{}, unauthorized(ctx)
		}
		if !servicePeerAllowed(peerID.String(), policy.ServicePeers) {
			return projects.Principal{Subject: peerID.String()}, forbidden(ctx, "service-peer-denied", "SPIFFE peer is not allowed to call this projects operation")
		}
		if policy.OrgScope == orgScopeTokenOrgID {
			principal, err := principalFromServiceOrigin(ctx)
			if err != nil {
				return principal, err
			}
			if err := requireOperationIdempotency(ctx, policy); err != nil {
				return principal, err
			}
			return principal, nil
		}
		return projects.Principal{Subject: peerID.String()}, requireOperationIdempotency(ctx, policy)
	}
	identity := auth.FromContext(ctx)
	if identity == nil {
		return projects.Principal{}, unauthorized(ctx)
	}
	orgID, err := strconv.ParseUint(strings.TrimSpace(identity.OrgID), 10, 64)
	if err != nil || orgID == 0 {
		return projects.Principal{}, forbidden(ctx, "organization-required", "projects routes require an organization-scoped token")
	}
	principal := projects.Principal{Subject: identity.Subject, OrgID: orgID, Email: identity.Email}
	if err := projects.ValidatePrincipal(principal); err != nil {
		return principal, forbidden(ctx, "projects-principal-required", "projects routes require a subject token")
	}
	if authorizer == nil {
		return principal, problem(ctx, http.StatusServiceUnavailable, "iam-authorizer-unavailable", "IAM authorizer unavailable", runtimeiam.ErrAuthorizerUnavailable)
	}
	decision, err := authorizer.AuthorizeOperation(ctx, identity, policy.OperationPolicy)
	if err != nil {
		return principal, problem(ctx, http.StatusServiceUnavailable, "iam-authorizer-unavailable", "IAM authorization check failed", err)
	}
	if !decision.Allowed {
		return principal, forbidden(ctx, "permission-denied", "missing required projects permission")
	}
	if err := requireOperationIdempotency(ctx, policy); err != nil {
		return principal, err
	}
	return principal, nil
}

func principalFromServiceOrigin(ctx context.Context) (projects.Principal, error) {
	info := operationRequestInfoFromContext(ctx)
	orgID, err := strconv.ParseUint(strings.TrimSpace(info.OriginOrgID), 10, 64)
	if err != nil || orgID == 0 {
		return projects.Principal{}, badRequest(ctx, "origin-organization-required", originOrgIDHeader+" must be a non-zero decimal organization ID", err)
	}
	principal := projects.Principal{
		Subject: strings.TrimSpace(info.OriginSubject),
		OrgID:   orgID,
		Email:   strings.TrimSpace(info.OriginEmail),
	}
	if principal.Subject == "" {
		return principal, badRequest(ctx, "origin-subject-required", originSubjectHeader+" is required for SPIFFE-authenticated org-scoped operations", nil)
	}
	if len(principal.Subject) > maxOriginSubjectLength {
		return principal, badRequest(ctx, "origin-subject-too-long", originSubjectHeader+" is too long", nil)
	}
	if len(principal.Email) > maxOriginEmailLength {
		return principal, badRequest(ctx, "origin-email-too-long", originEmailHeader+" is too long", nil)
	}
	if err := projects.ValidatePrincipal(principal); err != nil {
		return principal, forbidden(ctx, "projects-origin-principal-required", "SPIFFE-authenticated org-scoped operations require an origin subject")
	}
	return principal, nil
}

func servicePeerAllowed(peerID string, allowedServices []string) bool {
	if len(allowedServices) == 0 {
		return false
	}
	for _, service := range allowedServices {
		if strings.HasSuffix(peerID, "/svc/"+service) {
			return true
		}
	}
	return false
}

func requireOperationIdempotency(ctx context.Context, policy projectsOperationPolicy) error {
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
	return nil
}

func operationRequestMiddleware(ctx huma.Context, next func(huma.Context)) {
	info := operationRequestInfo{
		IdempotencyKey: ctx.Header("Idempotency-Key"),
		OriginOrgID:    ctx.Header(originOrgIDHeader),
		OriginSubject:  ctx.Header(originSubjectHeader),
		OriginEmail:    ctx.Header(originEmailHeader),
	}
	next(huma.WithValue(ctx, operationRequestInfoKey{}, info))
}

func operationRequestInfoFromContext(ctx context.Context) operationRequestInfo {
	info, _ := ctx.Value(operationRequestInfoKey{}).(operationRequestInfo)
	return info
}
