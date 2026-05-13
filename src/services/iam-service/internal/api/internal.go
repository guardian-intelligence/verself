package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/iam-service/internal/authz"
	"github.com/verself/iam-service/internal/identity"
	"github.com/verself/iam-service/internal/internalcontractapi"
	auth "github.com/verself/service-runtime/auth"
	workloadauth "github.com/verself/service-runtime/workload"
)

var internalAPITracer = otel.Tracer("iam-service/internal/api/internal")

func RegisterInternalRoutes(api huma.API, svc *identity.Service, authzSvc *authz.Service) {
	runtime := internalRuntime{}
	handlers := internalHandlers{service: svc, authz: authzSvc}
	registerInternalOperation(api, runtime, internalcontractapi.AuthorizeOperation, handlers.AuthorizeOperation, "Authorize operation")
	registerInternalOperation(api, runtime, internalcontractapi.AuthorizeResource, handlers.AuthorizeResource, "Authorize resource")
	registerInternalOperation(api, runtime, internalcontractapi.WriteResourceParentEdge, handlers.WriteResourceParentEdge, "Write resource parent edge")
	registerInternalOperation(api, runtime, internalcontractapi.ResolveOrganization, handlers.ResolveOrganization, "Resolve organization")
	registerInternalOperation(api, runtime, internalcontractapi.UpdateHumanProfile, handlers.UpdateHumanProfile, "Update human profile")
}

func registerInternalOperation[Input any, Output any](
	api huma.API,
	runtime internalRuntime,
	operation internalcontractapi.Operation[Input, Output],
	handler internalcontractapi.Handler[Input, Output],
	summary string,
) {
	desc := operation.Descriptor
	op := runtime.PrepareOperation(desc, huma.Operation{
		OperationID:   desc.OperationID,
		Method:        desc.Method,
		Path:          desc.Path,
		Summary:       summary,
		DefaultStatus: desc.DefaultStatus,
	})
	huma.Register(api, op, func(ctx context.Context, input *Input) (*Output, error) {
		identity, err := runtime.BeforeOperation(ctx, desc, input)
		if err != nil {
			runtime.AfterOperation(ctx, desc, identity, input, nil, err)
			return nil, err
		}
		output, err := handler(ctx, input)
		runtime.AfterOperation(ctx, desc, identity, input, output, err)
		return output, err
	})
}

type internalRuntime struct{}

type internalHandlers struct {
	service *identity.Service
	authz   *authz.Service
}

func (r internalRuntime) PrepareOperation(desc internalcontractapi.OperationDescriptor, op huma.Operation) huma.Operation {
	if err := validateGeneratedInternalOperation(desc); err != nil {
		panic(err)
	}
	if desc.RequestBodyMaxBytes > 0 {
		op.MaxBodyBytes = desc.RequestBodyMaxBytes
	}
	op.Errors = internalContractProblemStatuses(desc.Problems)
	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}
	op.Extensions["x-verself-contract"] = internalContractExtension(desc)
	switch desc.Identity.Mode {
	case "spiffe_mtls_bearer":
		op.Security = []map[string][]string{{"mutualTLS": {}, "bearerAuth": {}}}
	case "spiffe_mtls":
		op.Security = []map[string][]string{{"mutualTLS": {}}}
	default:
		panic(fmt.Errorf("%s: unsupported internal identity mode %q", desc.OperationID, desc.Identity.Mode))
	}
	op.Middlewares = append(op.Middlewares, operationRequestMiddleware)
	return op
}

func (r internalRuntime) BeforeOperation(context.Context, internalcontractapi.OperationDescriptor, any) (any, error) {
	return nil, nil
}

func (r internalRuntime) AfterOperation(context.Context, internalcontractapi.OperationDescriptor, any, any, any, error) {
}

func validateGeneratedInternalOperation(desc internalcontractapi.OperationDescriptor) error {
	if strings.TrimSpace(desc.OperationID) == "" || strings.TrimSpace(desc.Method) == "" || strings.TrimSpace(desc.Path) == "" {
		return fmt.Errorf("generated internal operation missing id, method, or path: %#v", desc)
	}
	switch desc.Identity.Mode {
	case "spiffe_mtls", "spiffe_mtls_bearer":
	default:
		return fmt.Errorf("%s: unsupported internal identity mode %q", desc.OperationID, desc.Identity.Mode)
	}
	if desc.Identity.Audience != "iam-service" {
		return fmt.Errorf("%s: unexpected audience %q", desc.OperationID, desc.Identity.Audience)
	}
	if desc.Authorization.Permission == "" || desc.Audit.Event == "" || desc.Audit.Resource == "" || desc.Audit.Action == "" {
		return fmt.Errorf("%s: incomplete generated IAM metadata", desc.OperationID)
	}
	if desc.RequestBodyMaxBytes <= 0 {
		return fmt.Errorf("%s: internal operation missing request body budget", desc.OperationID)
	}
	return nil
}

func internalContractExtension(desc internalcontractapi.OperationDescriptor) map[string]any {
	return map[string]any{
		"shape_id":               desc.ShapeID,
		"operation_id":           desc.OperationID,
		"identity":               desc.Identity.Mode,
		"audience":               desc.Identity.Audience,
		"permission":             desc.Authorization.Permission,
		"organization_source":    desc.Authorization.OrganizationSource,
		"organization_member":    desc.Authorization.OrganizationMember,
		"audit_event":            desc.Audit.Event,
		"resource":               desc.Audit.Resource,
		"action":                 desc.Audit.Action,
		"rate_limit_bucket":      desc.RateLimitBucket,
		"request_body_max_bytes": desc.RequestBodyMaxBytes,
		"idempotency":            desc.Idempotency.Policy,
	}
}

func internalContractProblemStatuses(problems []internalcontractapi.ProblemDescriptor) []int {
	statuses := make([]int, 0, len(problems))
	for _, problem := range problems {
		if problem.Status > 0 {
			statuses = append(statuses, problem.Status)
		}
	}
	return uniqueSortedStatusCodes(statuses)
}

func (h internalHandlers) UpdateHumanProfile(ctx context.Context, input *internalcontractapi.UpdateHumanProfileInput) (*internalcontractapi.UpdateHumanProfileOutput, error) {
	return updateHumanProfile(h.service)(ctx, input)
}

func (h internalHandlers) ResolveOrganization(ctx context.Context, input *internalcontractapi.ResolveOrganizationInput) (*internalcontractapi.ResolveOrganizationOutput, error) {
	return resolveOrganization(h.service)(ctx, input)
}

func (h internalHandlers) AuthorizeOperation(ctx context.Context, input *internalcontractapi.AuthorizeOperationInput) (*internalcontractapi.AuthorizeOperationOutput, error) {
	return authorizeOperation(h.service, h.authz)(ctx, input)
}

func (h internalHandlers) AuthorizeResource(ctx context.Context, input *internalcontractapi.AuthorizeResourceInput) (*internalcontractapi.AuthorizeResourceOutput, error) {
	return authorizeResource(h.service, h.authz)(ctx, input)
}

func (h internalHandlers) WriteResourceParentEdge(ctx context.Context, input *internalcontractapi.WriteResourceParentEdgeInput) (*internalcontractapi.WriteResourceParentEdgeOutput, error) {
	return writeResourceParentEdge(h.service, h.authz)(ctx, input)
}

func updateHumanProfile(svc *identity.Service) func(context.Context, *internalcontractapi.UpdateHumanProfileInput) (*internalcontractapi.UpdateHumanProfileOutput, error) {
	return func(ctx context.Context, input *internalcontractapi.UpdateHumanProfileInput) (*internalcontractapi.UpdateHumanProfileOutput, error) {
		ctx, span := internalAPITracer.Start(ctx, "iam.human_profile.write")
		defer span.End()
		subjectID := string(input.SubjectID)
		span.SetAttributes(
			attribute.String("iam.operation_id", "update-human-profile"),
			attribute.String("iam.subject_id", strings.TrimSpace(subjectID)),
		)
		authIdentity, err := requireInternalHumanIAM(ctx, subjectID)
		if err != nil {
			finishInternalProfileSpan(span, authIdentity, "denied", err)
			auditInternalProfileUpdate(ctx, subjectID, authIdentity, "denied", err)
			return nil, err
		}
		setInternalProfileIAMAttributes(span, authIdentity)
		profile, err := svc.UpdateHumanProfile(ctx, subjectID, identity.HumanProfileUpdate{
			GivenName:   string(input.Body.GivenName),
			FamilyName:  string(input.Body.FamilyName),
			DisplayName: optionalStringPointer(input.Body.DisplayName),
		})
		if err != nil {
			mapped := identityError(ctx, err)
			finishInternalProfileSpan(span, authIdentity, "error", mapped)
			auditInternalProfileUpdate(ctx, subjectID, authIdentity, "error", mapped)
			return nil, mapped
		}
		finishInternalProfileSpan(span, authIdentity, "allowed", nil)
		auditInternalProfileUpdate(ctx, subjectID, authIdentity, "allowed", nil)
		return &internalcontractapi.UpdateHumanProfileOutput{Body: humanProfileSummary(profile)}, nil
	}
}

func resolveOrganization(svc *identity.Service) func(context.Context, *internalcontractapi.ResolveOrganizationInput) (*internalcontractapi.ResolveOrganizationOutput, error) {
	return func(ctx context.Context, input *internalcontractapi.ResolveOrganizationInput) (*internalcontractapi.ResolveOrganizationOutput, error) {
		ctx, span := internalAPITracer.Start(ctx, "iam.organization.resolve")
		defer span.End()
		peerID, ok := workloadauth.PeerIDFromContext(ctx)
		if !ok {
			err := problem(ctx, http.StatusUnauthorized, "missing-workload-identity", "missing SPIFFE peer identity", nil)
			span.RecordError(err)
			span.SetStatus(codes.Error, "missing SPIFFE peer identity")
			return nil, err
		}
		orgID := contractString(input.Body.OrgID)
		slug := contractString(input.Body.Slug)
		span.SetAttributes(
			attribute.String("spiffe.peer_id", peerID.String()),
			attribute.String("verself.org_id", orgID),
			attribute.String("iam.org_slug.requested", strings.TrimSpace(slug)),
		)
		profile, err := svc.ResolveOrganization(ctx, identity.ResolveOrganizationRequest{
			IdentityProviderOrgID: orgID,
			Slug:                  slug,
			RequireActive:         input.Body.RequireActive,
		})
		if err != nil {
			mapped := identityError(ctx, err)
			span.RecordError(mapped)
			span.SetStatus(codes.Error, problemCode(mapped))
			return nil, mapped
		}
		span.SetAttributes(
			attribute.String("verself.org_id", profile.OrgID),
			attribute.String("iam.org_slug", profile.Slug),
			attribute.String("iam.org_slug.redirected_from", profile.RedirectedFrom),
		)
		return &internalcontractapi.ResolveOrganizationOutput{
			Body: internalcontractapi.ResolveOrganizationOutputBody{Organization: organizationProfile(profile)},
		}, nil
	}
}

func authorizeOperation(svc *identity.Service, authzSvc *authz.Service) func(context.Context, *internalcontractapi.AuthorizeOperationInput) (*internalcontractapi.AuthorizeOperationOutput, error) {
	return func(ctx context.Context, input *internalcontractapi.AuthorizeOperationInput) (*internalcontractapi.AuthorizeOperationOutput, error) {
		ctx, span := internalAPITracer.Start(ctx, "iam.authorization.check")
		defer span.End()
		peerID, ok := workloadauth.PeerIDFromContext(ctx)
		if !ok {
			err := problem(ctx, http.StatusUnauthorized, "missing-workload-identity", "missing SPIFFE peer identity", nil)
			span.RecordError(err)
			span.SetStatus(codes.Error, "missing SPIFFE peer identity")
			return nil, err
		}
		if authzSvc == nil {
			err := internalFailure(ctx, "iam-authz-unavailable", "authorization graph unavailable", authz.ErrUnavailable)
			span.RecordError(err)
			span.SetStatus(codes.Error, problemCode(err))
			return nil, err
		}
		orgID, err := publicOrgIDForProvider(ctx, svc, string(input.Body.OrgID))
		if err != nil {
			return nil, identityError(ctx, err)
		}
		subject, err := authorizationSubjectFromContract(input.Body.Subject)
		if err != nil {
			return nil, badRequest(ctx, "invalid-authorization-subject", "authorization subject is invalid", err)
		}
		permissions := permissionStrings(input.Body.Permissions)
		span.SetAttributes(
			attribute.String("spiffe.peer_id", peerID.String()),
			attribute.String("verself.org_id", orgID),
			attribute.String("iam.subject_type", string(subject.Kind)),
			attribute.String("iam.subject_id", subject.ID),
			attribute.StringSlice("iam.permissions.requested", compactStrings(permissions)),
		)
		allowed, zedToken, err := authzSvc.TestOrganizationPermissions(ctx, orgID, subject, permissions, contractString(input.Body.MinZedToken))
		if err != nil {
			mapped := authzError(ctx, err)
			span.RecordError(mapped)
			span.SetStatus(codes.Error, problemCode(mapped))
			return nil, mapped
		}
		span.SetAttributes(
			attribute.StringSlice("iam.permissions.allowed", allowed),
			attribute.String("iam.zed_token", zedToken),
		)
		return &internalcontractapi.AuthorizeOperationOutput{Body: internalcontractapi.AuthorizeOperationResult{
			OrgID:       input.Body.OrgID,
			Subject:     input.Body.Subject,
			Permissions: contractPermissions(allowed),
			ZedToken:    optionalZedToken(zedToken),
		}}, nil
	}
}

func authorizeResource(svc *identity.Service, authzSvc *authz.Service) func(context.Context, *internalcontractapi.AuthorizeResourceInput) (*internalcontractapi.AuthorizeResourceOutput, error) {
	return func(ctx context.Context, input *internalcontractapi.AuthorizeResourceInput) (*internalcontractapi.AuthorizeResourceOutput, error) {
		ctx, span := internalAPITracer.Start(ctx, "iam.authorization.resource_check")
		defer span.End()
		peerID, ok := workloadauth.PeerIDFromContext(ctx)
		if !ok {
			err := problem(ctx, http.StatusUnauthorized, "missing-workload-identity", "missing SPIFFE peer identity", nil)
			span.RecordError(err)
			span.SetStatus(codes.Error, "missing SPIFFE peer identity")
			return nil, err
		}
		if authzSvc == nil {
			err := internalFailure(ctx, "iam-authz-unavailable", "authorization graph unavailable", authz.ErrUnavailable)
			span.RecordError(err)
			span.SetStatus(codes.Error, problemCode(err))
			return nil, err
		}
		orgID, err := publicOrgIDForProvider(ctx, svc, string(input.Body.OrgID))
		if err != nil {
			return nil, identityError(ctx, err)
		}
		subject, err := authorizationSubjectFromContract(input.Body.Subject)
		if err != nil {
			return nil, badRequest(ctx, "invalid-authorization-subject", "authorization subject is invalid", err)
		}
		resource := resourceRefFromContract(input.Body.Resource)
		operationPermission := contractString(input.Body.OperationPermission)
		minZedToken := contractString(input.Body.MinZedToken)
		span.SetAttributes(
			attribute.String("spiffe.peer_id", peerID.String()),
			attribute.String("verself.org_id", orgID),
			attribute.String("iam.subject_type", string(subject.Kind)),
			attribute.String("iam.subject_id", subject.ID),
			attribute.String("iam.operation_permission", operationPermission),
			attribute.String("iam.resource_type", resource.Type),
			attribute.String("iam.resource_id", resource.ID),
			attribute.String("iam.resource_permission", strings.TrimSpace(string(input.Body.ResourcePermission))),
		)
		decision, err := authzSvc.CheckResourcePermission(ctx, orgID, subject, resource, string(input.Body.ResourcePermission), operationPermission, minZedToken)
		if err != nil {
			mapped := authzError(ctx, err)
			span.RecordError(mapped)
			span.SetStatus(codes.Error, problemCode(mapped))
			return nil, mapped
		}
		span.SetAttributes(
			attribute.Bool("iam.allowed", decision.Allowed),
			attribute.String("iam.zed_token", decision.ZedToken),
		)
		return &internalcontractapi.AuthorizeResourceOutput{Body: internalcontractapi.AuthorizeResourceResult{
			OrgID:               input.Body.OrgID,
			Subject:             authorizationSubjectContract(decision.Subject),
			OperationPermission: optionalPermissionName(decision.OperationPermission),
			Resource:            resourceRefContract(decision.Resource),
			ResourcePermission:  internalcontractapi.ResourcePermissionName(decision.ResourcePermission),
			Allowed:             decision.Allowed,
			ZedToken:            optionalZedToken(decision.ZedToken),
		}}, nil
	}
}

func writeResourceParentEdge(svc *identity.Service, authzSvc *authz.Service) func(context.Context, *internalcontractapi.WriteResourceParentEdgeInput) (*internalcontractapi.WriteResourceParentEdgeOutput, error) {
	return func(ctx context.Context, input *internalcontractapi.WriteResourceParentEdgeInput) (*internalcontractapi.WriteResourceParentEdgeOutput, error) {
		ctx, span := internalAPITracer.Start(ctx, "iam.authorization.resource_parent_edge.write")
		defer span.End()
		peerID, ok := workloadauth.PeerIDFromContext(ctx)
		if !ok {
			err := problem(ctx, http.StatusUnauthorized, "missing-workload-identity", "missing SPIFFE peer identity", nil)
			span.RecordError(err)
			span.SetStatus(codes.Error, "missing SPIFFE peer identity")
			return nil, err
		}
		if authzSvc == nil {
			err := internalFailure(ctx, "iam-authz-unavailable", "authorization graph unavailable", authz.ErrUnavailable)
			span.RecordError(err)
			span.SetStatus(codes.Error, problemCode(err))
			return nil, err
		}
		orgID, err := publicOrgIDForProvider(ctx, svc, string(input.Body.OrgID))
		if err != nil {
			return nil, identityError(ctx, err)
		}
		resource := resourceRefFromContract(input.Body.Resource)
		parent := resourceRefFromContract(input.Body.Parent)
		if parent.Type == "org" && parent.ID == string(input.Body.OrgID) {
			parent.ID = orgID
		}
		span.SetAttributes(
			attribute.String("spiffe.peer_id", peerID.String()),
			attribute.String("verself.org_id", orgID),
			attribute.String("iam.resource_type", resource.Type),
			attribute.String("iam.resource_id", resource.ID),
			attribute.String("iam.parent_relation", strings.TrimSpace(string(input.Body.Relation))),
			attribute.String("iam.parent_type", parent.Type),
			attribute.String("iam.parent_id", parent.ID),
		)
		edge, err := authzSvc.WriteResourceParentEdge(ctx, orgID, resource, string(input.Body.Relation), parent, contractString(input.Body.Operation))
		if err != nil {
			mapped := authzError(ctx, err)
			span.RecordError(mapped)
			span.SetStatus(codes.Error, problemCode(mapped))
			return nil, mapped
		}
		span.SetAttributes(attribute.String("iam.zed_token", edge.ZedToken))
		return &internalcontractapi.WriteResourceParentEdgeOutput{Body: internalcontractapi.WriteResourceParentEdgeResult{
			Resource:  resourceRefContract(edge.Resource),
			Relation:  internalcontractapi.AuthorizationRelation(edge.Relation),
			Parent:    resourceRefContract(edge.Parent),
			ZedToken:  optionalZedToken(edge.ZedToken),
			Operation: optionalPermissionName(edge.Operation),
		}}, nil
	}
}

func authorizationSubjectFromContract(subject internalcontractapi.IAMAuthorizationSubject) (identity.AuthorizationSubject, error) {
	out := identity.AuthorizationSubject{ID: strings.TrimSpace(string(subject.ID))}
	switch strings.TrimSpace(string(subject.Type)) {
	case string(identity.AuthorizationSubjectKindUser):
		out.Kind = identity.AuthorizationSubjectKindUser
	case string(identity.AuthorizationSubjectKindServiceAccount):
		out.Kind = identity.AuthorizationSubjectKindServiceAccount
	case string(identity.AuthorizationSubjectKindWorkload):
		out.Kind = identity.AuthorizationSubjectKindWorkload
	default:
		return identity.AuthorizationSubject{}, fmt.Errorf("unsupported subject type %q", subject.Type)
	}
	if out.ID == "" {
		return identity.AuthorizationSubject{}, fmt.Errorf("subject id is required")
	}
	return out, nil
}

func authorizationSubjectContract(subject authz.Subject) internalcontractapi.IAMAuthorizationSubject {
	return internalcontractapi.IAMAuthorizationSubject{
		Type: internalcontractapi.IAMAuthorizationSubjectType(subject.Kind),
		ID:   internalcontractapi.SubjectID(subject.ID),
	}
}

func resourceRefFromContract(resource internalcontractapi.IAMResourceRef) authz.ResourceRef {
	return authz.ResourceRef{
		Type: strings.TrimSpace(string(resource.Type)),
		ID:   strings.TrimSpace(string(resource.ID)),
	}
}

func resourceRefContract(resource authz.ResourceRef) internalcontractapi.IAMResourceRef {
	return internalcontractapi.IAMResourceRef{
		Type: internalcontractapi.AuthorizationResourceType(strings.TrimSpace(resource.Type)),
		ID:   internalcontractapi.AuthorizationResourceID(strings.TrimSpace(resource.ID)),
	}
}

func publicOrgIDForProvider(ctx context.Context, svc *identity.Service, providerOrgID string) (string, error) {
	if svc == nil {
		return "", identity.ErrStoreUnavailable
	}
	providerOrgID = strings.TrimSpace(providerOrgID)
	if providerOrgID == "" || providerOrgID == "0" {
		return "", fmt.Errorf("%w: org_id is required", identity.ErrInvalidInput)
	}
	profile, err := svc.ResolveOrganization(ctx, identity.ResolveOrganizationRequest{
		IdentityProviderOrgID: providerOrgID,
		RequireActive:         true,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(profile.OrgID), nil
}

func setInternalProfileIAMAttributes(span trace.Span, identity *auth.Identity) {
	if span == nil || identity == nil {
		return
	}
	span.SetAttributes(
		attribute.String("verself.org_id", identity.OrgID),
		attribute.String("verself.subject_id", identity.Subject),
	)
}

func finishInternalProfileSpan(span trace.Span, identity *auth.Identity, outcome string, err error) {
	if span == nil {
		return
	}
	setInternalProfileIAMAttributes(span, identity)
	span.SetAttributes(attribute.String("iam.outcome", outcome))
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String("iam.error_code", problemCode(err)))
		if outcome != "denied" {
			span.SetStatus(codes.Error, problemCode(err))
		}
	}
}

func requireInternalHumanIAM(ctx context.Context, subjectID string) (*auth.Identity, error) {
	if _, ok := workloadauth.PeerIDFromContext(ctx); !ok {
		return nil, problem(ctx, http.StatusUnauthorized, "missing-workload-identity", "missing SPIFFE peer identity", nil)
	}
	authIdentity, err := requireIdentity(ctx)
	if err != nil {
		return authIdentity, err
	}
	if strings.TrimSpace(claimString(authIdentity.Raw, "verself:credential_id")) != "" {
		return authIdentity, forbidden(ctx, "api-credential-not-allowed", "forwarded token must be a human token")
	}
	if !hasHumanTokenMarker(authIdentity.Raw) {
		return authIdentity, forbidden(ctx, "human-token-required", "forwarded token must be a human browser token")
	}
	if strings.TrimSpace(authIdentity.Subject) != strings.TrimSpace(subjectID) {
		return authIdentity, forbidden(ctx, "subject-mismatch", "forwarded token subject must match path subject_id")
	}
	return authIdentity, nil
}

func hasHumanTokenMarker(claims map[string]any) bool {
	// ZITADEL access tokens here omit email, so the generic roles claim is the current human-token discriminator.
	value, ok := claims["urn:zitadel:iam:org:project:roles"]
	if !ok {
		return false
	}
	roles, ok := value.(map[string]any)
	return ok && len(roles) > 0
}

func organizationProfile(profile identity.OrganizationProfile) internalcontractapi.OrganizationProfile {
	return internalcontractapi.OrganizationProfile{
		OrgID:          internalcontractapi.ProviderOrgID(profile.IdentityProviderOrgID),
		DisplayName:    internalcontractapi.DisplayName(profile.DisplayName),
		Slug:           internalcontractapi.OrgSlug(profile.Slug),
		State:          string(profile.State),
		Version:        internalcontractapi.OrganizationVersion(profile.Version),
		UpdatedAt:      profile.UpdatedAt.Format(time.RFC3339Nano),
		RedirectedFrom: optionalOrgSlug(profile.RedirectedFrom),
	}
}

func humanProfileSummary(profile identity.HumanProfile) internalcontractapi.HumanProfileSummary {
	return internalcontractapi.HumanProfileSummary{
		SubjectID:   internalcontractapi.SubjectID(profile.SubjectID),
		Email:       internalcontractapi.EmailAddress(profile.Email),
		GivenName:   internalcontractapi.GivenName(profile.GivenName),
		FamilyName:  internalcontractapi.FamilyName(profile.FamilyName),
		DisplayName: internalcontractapi.DisplayName(profile.DisplayName),
		SyncedAt:    profile.SyncedAt.Format(time.RFC3339Nano),
	}
}

func contractString[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(string(*value))
}

func optionalStringPointer[T ~string](value *T) *string {
	if value == nil {
		return nil
	}
	valueString := strings.TrimSpace(string(*value))
	if valueString == "" {
		return nil
	}
	return &valueString
}

func optionalZedToken(value string) *internalcontractapi.ZedToken {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	out := internalcontractapi.ZedToken(value)
	return &out
}

func optionalPermissionName(value string) *internalcontractapi.PermissionName {
	return optionalContractValue[internalcontractapi.PermissionName](value)
}

func optionalOrgSlug(value string) *internalcontractapi.OrgSlug {
	return optionalContractValue[internalcontractapi.OrgSlug](value)
}

func optionalContractValue[T ~string](value string) *T {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	out := T(value)
	return &out
}

func permissionStrings(values internalcontractapi.Permissions) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = internalcontractapi.PermissionName(strings.TrimSpace(string(value))); value != "" {
			out = append(out, string(value))
		}
	}
	return out
}

func contractPermissions(values []string) internalcontractapi.Permissions {
	out := make(internalcontractapi.Permissions, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, internalcontractapi.PermissionName(value))
		}
	}
	return out
}

func auditInternalProfileUpdate(ctx context.Context, subjectID string, authIdentity *auth.Identity, outcome string, err error) {
	args := []any{
		"audit_event", "iam.human_profile.write",
		"operation_id", "update-human-profile",
		"operation_resource", "human_profile",
		"operation_action", "write",
		"outcome", outcome,
	}
	if authIdentity != nil {
		args = append(args, "subject", authIdentity.Subject, "org_id", authIdentity.OrgID)
	}
	if err != nil {
		args = append(args, "error", problemCode(err))
	}
	slog.Default().InfoContext(ctx, "iam internal api operation", args...)
	if authIdentity == nil {
		return
	}
	record := governanceAuditRecord{
		OrgID:       authIdentity.OrgID,
		EventSource: "iam-service",
		EventName:   "update-human-profile",
		AuditEvent:  "iam.human_profile.write",
		ActorType:   "user",
		ActorID:     authIdentity.Subject,
		Permission:  "iam:human_profile:write",
		TargetType:  "human_profile",
		TargetID:    strings.TrimSpace(subjectID),
		Outcome:     outcome,
	}
	if err != nil {
		record.ErrorCode = problemCode(err)
	}
	sendGovernanceAudit(ctx, record)
}
