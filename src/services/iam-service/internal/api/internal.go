package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/domain-transfer-objects"
	"github.com/verself/iam-service/internal/authz"
	"github.com/verself/iam-service/internal/identity"
	auth "github.com/verself/service-runtime/auth"
	workloadauth "github.com/verself/service-runtime/workload"
)

var internalAPITracer = otel.Tracer("iam-service/internal/api/internal")

type updateHumanProfileInput struct {
	SubjectID string `path:"subject_id" doc:"Zitadel human subject ID"`
	Body      dto.IAMUpdateHumanProfileRequest
}

type updateHumanProfileOutput struct {
	Body dto.IAMUpdateHumanProfileResponse
}

type resolveOrganizationInput struct {
	Body dto.IAMResolveOrganizationRequest
}

type resolveOrganizationOutput struct {
	Body dto.IAMResolveOrganizationResponse
}

type authorizeOperationInput struct {
	Body dto.IAMAuthorizeRequest
}

type authorizeOperationOutput struct {
	Body dto.IAMAuthorizeResponse
}

type authorizeResourceInput struct {
	Body dto.IAMAuthorizeResourceRequest
}

type authorizeResourceOutput struct {
	Body dto.IAMAuthorizeResourceResponse
}

type writeResourceParentEdgeInput struct {
	Body dto.IAMWriteResourceParentEdgeRequest
}

type writeResourceParentEdgeOutput struct {
	Body dto.IAMWriteResourceParentEdgeResponse
}

func RegisterInternalRoutes(api huma.API, svc *identity.Service, authzSvc *authz.Service) {
	op := huma.Operation{
		OperationID:   "update-human-profile",
		Method:        http.MethodPatch,
		Path:          "/internal/v1/subjects/{subject_id}/human-profile",
		Summary:       "Update a human profile",
		Description:   "SPIFFE-mTLS internal endpoint for profile-service to update the forwarded human subject's Zitadel profile fields.",
		Security:      []map[string][]string{{"mutualTLS": {}, "bearerAuth": {}}},
		DefaultStatus: http.StatusOK,
		MaxBodyBytes:  bodyLimitSmallJSON,
	}
	op.Middlewares = append(op.Middlewares, operationRequestMiddleware)
	huma.Register(api, op, updateHumanProfile(svc))

	resolveOp := huma.Operation{
		OperationID:   "resolve-organization",
		Method:        http.MethodPost,
		Path:          "/internal/v1/organizations/resolve",
		Summary:       "Resolve an organization profile",
		Description:   "SPIFFE-mTLS internal endpoint for repo-owned services to resolve canonical and redirected organization slugs.",
		Security:      []map[string][]string{{"mutualTLS": {}}},
		DefaultStatus: http.StatusOK,
		MaxBodyBytes:  bodyLimitSmallJSON,
	}
	resolveOp.Middlewares = append(resolveOp.Middlewares, operationRequestMiddleware)
	huma.Register(api, resolveOp, resolveOrganization(svc))

	authorizeOp := huma.Operation{
		OperationID:   "authorize-operation",
		Method:        http.MethodPost,
		Path:          "/internal/v1/authorization/authorize",
		Summary:       "Authorize a product operation",
		Description:   "SPIFFE-mTLS internal endpoint for product services to check an authenticated user, service account, or workload against IAM.",
		Security:      []map[string][]string{{"mutualTLS": {}}},
		DefaultStatus: http.StatusOK,
		MaxBodyBytes:  bodyLimitSmallJSON,
	}
	authorizeOp.Middlewares = append(authorizeOp.Middlewares, operationRequestMiddleware)
	huma.Register(api, authorizeOp, authorizeOperation(svc, authzSvc))

	authorizeResourceOp := huma.Operation{
		OperationID:   "authorize-resource",
		Method:        http.MethodPost,
		Path:          "/internal/v1/authorization/resources/authorize",
		Summary:       "Authorize a product operation against a Zanzibar resource",
		Description:   "SPIFFE-mTLS internal endpoint for product services to check a user, service account, or workload against a concrete IAM resource.",
		Security:      []map[string][]string{{"mutualTLS": {}}},
		DefaultStatus: http.StatusOK,
		MaxBodyBytes:  bodyLimitSmallJSON,
	}
	authorizeResourceOp.Middlewares = append(authorizeResourceOp.Middlewares, operationRequestMiddleware)
	huma.Register(api, authorizeResourceOp, authorizeResource(svc, authzSvc))

	parentEdgeOp := huma.Operation{
		OperationID:   "write-resource-parent-edge",
		Method:        http.MethodPost,
		Path:          "/internal/v1/authorization/resources/parent-edge",
		Summary:       "Write an IAM resource parent edge",
		Description:   "SPIFFE-mTLS internal endpoint for product services to materialize idempotent Zanzibar parent edges for IAM-owned resources.",
		Security:      []map[string][]string{{"mutualTLS": {}}},
		DefaultStatus: http.StatusOK,
		MaxBodyBytes:  bodyLimitSmallJSON,
	}
	parentEdgeOp.Middlewares = append(parentEdgeOp.Middlewares, operationRequestMiddleware)
	huma.Register(api, parentEdgeOp, writeResourceParentEdge(svc, authzSvc))
}

func updateHumanProfile(svc *identity.Service) func(context.Context, *updateHumanProfileInput) (*updateHumanProfileOutput, error) {
	return func(ctx context.Context, input *updateHumanProfileInput) (*updateHumanProfileOutput, error) {
		ctx, span := internalAPITracer.Start(ctx, "iam.human_profile.write")
		defer span.End()
		span.SetAttributes(
			attribute.String("iam.operation_id", "update-human-profile"),
			attribute.String("iam.subject_id", strings.TrimSpace(input.SubjectID)),
		)
		authIdentity, err := requireInternalHumanIAM(ctx, input.SubjectID)
		if err != nil {
			finishInternalProfileSpan(span, authIdentity, "denied", err)
			auditInternalProfileUpdate(ctx, input.SubjectID, authIdentity, "denied", err)
			return nil, err
		}
		setInternalProfileIAMAttributes(span, authIdentity)
		profile, err := svc.UpdateHumanProfile(ctx, input.SubjectID, identity.HumanProfileUpdate{
			GivenName:   input.Body.GivenName,
			FamilyName:  input.Body.FamilyName,
			DisplayName: input.Body.DisplayName,
		})
		if err != nil {
			mapped := identityError(ctx, err)
			finishInternalProfileSpan(span, authIdentity, "error", mapped)
			auditInternalProfileUpdate(ctx, input.SubjectID, authIdentity, "error", mapped)
			return nil, mapped
		}
		finishInternalProfileSpan(span, authIdentity, "allowed", nil)
		auditInternalProfileUpdate(ctx, input.SubjectID, authIdentity, "allowed", nil)
		return &updateHumanProfileOutput{Body: humanProfileDTO(profile)}, nil
	}
}

func resolveOrganization(svc *identity.Service) func(context.Context, *resolveOrganizationInput) (*resolveOrganizationOutput, error) {
	return func(ctx context.Context, input *resolveOrganizationInput) (*resolveOrganizationOutput, error) {
		ctx, span := internalAPITracer.Start(ctx, "iam.organization.resolve")
		defer span.End()
		peerID, ok := workloadauth.PeerIDFromContext(ctx)
		if !ok {
			err := problem(ctx, http.StatusUnauthorized, "missing-workload-identity", "missing SPIFFE peer identity", nil)
			span.RecordError(err)
			span.SetStatus(codes.Error, "missing SPIFFE peer identity")
			return nil, err
		}
		orgID := ""
		if input.Body.OrgID.Uint64() != 0 {
			orgID = input.Body.OrgID.String()
		}
		span.SetAttributes(
			attribute.String("spiffe.peer_id", peerID.String()),
			attribute.String("verself.org_id", orgID),
			attribute.String("iam.org_slug.requested", strings.TrimSpace(input.Body.Slug)),
		)
		profile, err := svc.ResolveOrganization(ctx, identity.ResolveOrganizationRequest{
			IdentityProviderOrgID: orgID,
			Slug:                  input.Body.Slug,
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
		return &resolveOrganizationOutput{Body: dto.IAMResolveOrganizationResponse{Organization: organizationProfileDTO(profile)}}, nil
	}
}

func authorizeOperation(svc *identity.Service, authzSvc *authz.Service) func(context.Context, *authorizeOperationInput) (*authorizeOperationOutput, error) {
	return func(ctx context.Context, input *authorizeOperationInput) (*authorizeOperationOutput, error) {
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
		orgID, err := publicOrgIDForProviderDTO(ctx, svc, input.Body.OrgID)
		if err != nil {
			return nil, identityError(ctx, err)
		}
		subject, err := authorizationSubjectFromDTO(input.Body.Subject)
		if err != nil {
			return nil, badRequest(ctx, "invalid-authorization-subject", "authorization subject is invalid", err)
		}
		span.SetAttributes(
			attribute.String("spiffe.peer_id", peerID.String()),
			attribute.String("verself.org_id", orgID),
			attribute.String("iam.subject_type", string(subject.Kind)),
			attribute.String("iam.subject_id", subject.ID),
			attribute.StringSlice("iam.permissions.requested", compactStrings(input.Body.Permissions)),
		)
		allowed, zedToken, err := authzSvc.TestOrganizationPermissions(ctx, orgID, subject, input.Body.Permissions, strings.TrimSpace(input.Body.MinZedToken))
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
		return &authorizeOperationOutput{Body: dto.IAMAuthorizeResponse{
			OrgID:       input.Body.OrgID,
			Subject:     input.Body.Subject,
			Permissions: allowed,
			ZedToken:    zedToken,
		}}, nil
	}
}

func authorizeResource(svc *identity.Service, authzSvc *authz.Service) func(context.Context, *authorizeResourceInput) (*authorizeResourceOutput, error) {
	return func(ctx context.Context, input *authorizeResourceInput) (*authorizeResourceOutput, error) {
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
		orgID, err := publicOrgIDForProviderDTO(ctx, svc, input.Body.OrgID)
		if err != nil {
			return nil, identityError(ctx, err)
		}
		subject, err := authorizationSubjectFromDTO(input.Body.Subject)
		if err != nil {
			return nil, badRequest(ctx, "invalid-authorization-subject", "authorization subject is invalid", err)
		}
		resource := resourceRefFromDTO(input.Body.Resource)
		span.SetAttributes(
			attribute.String("spiffe.peer_id", peerID.String()),
			attribute.String("verself.org_id", orgID),
			attribute.String("iam.subject_type", string(subject.Kind)),
			attribute.String("iam.subject_id", subject.ID),
			attribute.String("iam.operation_permission", strings.TrimSpace(input.Body.OperationPermission)),
			attribute.String("iam.resource_type", resource.Type),
			attribute.String("iam.resource_id", resource.ID),
			attribute.String("iam.resource_permission", strings.TrimSpace(input.Body.ResourcePermission)),
		)
		decision, err := authzSvc.CheckResourcePermission(ctx, orgID, subject, resource, input.Body.ResourcePermission, input.Body.OperationPermission, input.Body.MinZedToken)
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
		return &authorizeResourceOutput{Body: dto.IAMAuthorizeResourceResponse{
			OrgID:               input.Body.OrgID,
			Subject:             authorizationSubjectDTO(decision.Subject),
			OperationPermission: decision.OperationPermission,
			Resource:            resourceRefDTO(decision.Resource),
			ResourcePermission:  decision.ResourcePermission,
			Allowed:             decision.Allowed,
			ZedToken:            decision.ZedToken,
		}}, nil
	}
}

func writeResourceParentEdge(svc *identity.Service, authzSvc *authz.Service) func(context.Context, *writeResourceParentEdgeInput) (*writeResourceParentEdgeOutput, error) {
	return func(ctx context.Context, input *writeResourceParentEdgeInput) (*writeResourceParentEdgeOutput, error) {
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
		orgID, err := publicOrgIDForProviderDTO(ctx, svc, input.Body.OrgID)
		if err != nil {
			return nil, identityError(ctx, err)
		}
		resource := resourceRefFromDTO(input.Body.Resource)
		parent := resourceRefFromDTO(input.Body.Parent)
		if parent.Type == "org" && parent.ID == input.Body.OrgID.String() {
			parent.ID = orgID
		}
		span.SetAttributes(
			attribute.String("spiffe.peer_id", peerID.String()),
			attribute.String("verself.org_id", orgID),
			attribute.String("iam.resource_type", resource.Type),
			attribute.String("iam.resource_id", resource.ID),
			attribute.String("iam.parent_relation", strings.TrimSpace(input.Body.Relation)),
			attribute.String("iam.parent_type", parent.Type),
			attribute.String("iam.parent_id", parent.ID),
		)
		edge, err := authzSvc.WriteResourceParentEdge(ctx, orgID, resource, input.Body.Relation, parent, input.Body.Operation)
		if err != nil {
			mapped := authzError(ctx, err)
			span.RecordError(mapped)
			span.SetStatus(codes.Error, problemCode(mapped))
			return nil, mapped
		}
		span.SetAttributes(attribute.String("iam.zed_token", edge.ZedToken))
		return &writeResourceParentEdgeOutput{Body: dto.IAMWriteResourceParentEdgeResponse{
			Resource:  resourceRefDTO(edge.Resource),
			Relation:  edge.Relation,
			Parent:    resourceRefDTO(edge.Parent),
			ZedToken:  edge.ZedToken,
			Operation: edge.Operation,
		}}, nil
	}
}

func authorizationSubjectFromDTO(subject dto.IAMAuthorizationSubject) (identity.AuthorizationSubject, error) {
	out := identity.AuthorizationSubject{ID: strings.TrimSpace(subject.ID)}
	switch strings.TrimSpace(subject.Type) {
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

func authorizationSubjectDTO(subject authz.Subject) dto.IAMAuthorizationSubject {
	return dto.IAMAuthorizationSubject{
		Type: string(subject.Kind),
		ID:   subject.ID,
	}
}

func resourceRefFromDTO(resource dto.IAMResourceRef) authz.ResourceRef {
	return authz.ResourceRef{
		Type: strings.TrimSpace(resource.Type),
		ID:   strings.TrimSpace(resource.ID),
	}
}

func resourceRefDTO(resource authz.ResourceRef) dto.IAMResourceRef {
	return dto.IAMResourceRef{
		Type: strings.TrimSpace(resource.Type),
		ID:   strings.TrimSpace(resource.ID),
	}
}

func publicOrgIDForProviderDTO(ctx context.Context, svc *identity.Service, providerOrgID dto.OrgID) (string, error) {
	if svc == nil {
		return "", identity.ErrStoreUnavailable
	}
	if providerOrgID.Uint64() == 0 {
		return "", fmt.Errorf("%w: org_id is required", identity.ErrInvalidInput)
	}
	profile, err := svc.ResolveOrganization(ctx, identity.ResolveOrganizationRequest{
		IdentityProviderOrgID: providerOrgID.String(),
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

func humanProfileDTO(profile identity.HumanProfile) dto.IAMUpdateHumanProfileResponse {
	return dto.IAMUpdateHumanProfileResponse{
		SubjectID:   profile.SubjectID,
		Email:       profile.Email,
		GivenName:   profile.GivenName,
		FamilyName:  profile.FamilyName,
		DisplayName: profile.DisplayName,
		SyncedAt:    profile.SyncedAt,
	}
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
