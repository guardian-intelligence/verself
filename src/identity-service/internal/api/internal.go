package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/apiwire"
	auth "github.com/verself/auth-middleware"
	workloadauth "github.com/verself/auth-middleware/workload"
	"github.com/verself/identity-service/internal/identity"
)

var internalAPITracer = otel.Tracer("identity-service/internal/api/internal")

type updateHumanProfileInput struct {
	SubjectID string `path:"subject_id" doc:"Zitadel human subject ID"`
	Body      apiwire.IdentityUpdateHumanProfileRequest
}

type updateHumanProfileOutput struct {
	Body apiwire.IdentityUpdateHumanProfileResponse
}

type resolveOrganizationInput struct {
	Body apiwire.IdentityResolveOrganizationRequest
}

type resolveOrganizationOutput struct {
	Body apiwire.IdentityResolveOrganizationResponse
}

func RegisterInternalRoutes(api huma.API, svc *identity.Service) {
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
}

func updateHumanProfile(svc *identity.Service) func(context.Context, *updateHumanProfileInput) (*updateHumanProfileOutput, error) {
	return func(ctx context.Context, input *updateHumanProfileInput) (*updateHumanProfileOutput, error) {
		ctx, span := internalAPITracer.Start(ctx, "identity.human_profile.write")
		defer span.End()
		span.SetAttributes(
			attribute.String("identity.operation_id", "update-human-profile"),
			attribute.String("identity.subject_id", strings.TrimSpace(input.SubjectID)),
		)
		authIdentity, err := requireInternalHumanIdentity(ctx, input.SubjectID)
		if err != nil {
			finishInternalProfileSpan(span, authIdentity, "denied", err)
			auditInternalProfileUpdate(ctx, input.SubjectID, authIdentity, "denied", err)
			return nil, err
		}
		setInternalProfileIdentityAttributes(span, authIdentity)
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
		ctx, span := internalAPITracer.Start(ctx, "identity.organization.resolve")
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
			attribute.String("identity.org_slug.requested", strings.TrimSpace(input.Body.Slug)),
		)
		profile, err := svc.ResolveOrganization(ctx, identity.ResolveOrganizationRequest{
			OrgID:         orgID,
			Slug:          input.Body.Slug,
			RequireActive: input.Body.RequireActive,
		})
		if err != nil {
			mapped := identityError(ctx, err)
			span.RecordError(mapped)
			span.SetStatus(codes.Error, problemCode(mapped))
			return nil, mapped
		}
		span.SetAttributes(
			attribute.String("verself.org_id", profile.OrgID),
			attribute.String("identity.org_slug", profile.Slug),
			attribute.String("identity.org_slug.redirected_from", profile.RedirectedFrom),
		)
		return &resolveOrganizationOutput{Body: apiwire.IdentityResolveOrganizationResponse{Organization: organizationProfileDTO(profile)}}, nil
	}
}

func setInternalProfileIdentityAttributes(span trace.Span, identity *auth.Identity) {
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
	setInternalProfileIdentityAttributes(span, identity)
	span.SetAttributes(attribute.String("identity.outcome", outcome))
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String("identity.error_code", problemCode(err)))
		if outcome != "denied" {
			span.SetStatus(codes.Error, problemCode(err))
		}
	}
}

func requireInternalHumanIdentity(ctx context.Context, subjectID string) (*auth.Identity, error) {
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

func humanProfileDTO(profile identity.HumanProfile) apiwire.IdentityUpdateHumanProfileResponse {
	return apiwire.IdentityUpdateHumanProfileResponse{
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
		"audit_event", "identity.human_profile.write",
		"operation_id", "update-human-profile",
		"operation_resource", "human_profile",
		"operation_action", "write",
		"operation_type", "write",
		"risk_level", "medium",
		"outcome", outcome,
	}
	if authIdentity != nil {
		args = append(args, "subject", authIdentity.Subject, "org_id", authIdentity.OrgID)
	}
	if err != nil {
		args = append(args, "error", problemCode(err))
	}
	slog.Default().InfoContext(ctx, "identity internal api operation", args...)
	if authIdentity == nil {
		return
	}
	info := operationRequestInfoFromContext(ctx)
	record := governanceAuditRecord{
		OrgID:              authIdentity.OrgID,
		SourceProductArea:  "Identity",
		ServiceName:        "identity-service",
		OperationID:        "update-human-profile",
		AuditEvent:         "identity.human_profile.write",
		OperationDisplay:   "update human profile",
		OperationType:      "write",
		EventCategory:      "identity",
		RiskLevel:          "medium",
		DataClassification: "restricted",
		ActorType:          "user",
		ActorID:            authIdentity.Subject,
		ActorDisplay:       authIdentity.Email,
		AuthMethod:         "bearer_forwarded_over_spiffe_mtls",
		Permission:         "identity:human_profile:write",
		TargetKind:         "human_profile",
		TargetID:           strings.TrimSpace(subjectID),
		TargetDisplay:      strings.TrimSpace(subjectID),
		TargetScope:        "token_subject",
		Action:             "write",
		OrgScope:           "token_org_id",
		RateLimitClass:     "internal_profile_mutation",
		Decision:           outcomeDecision(outcome),
		Result:             outcome,
		ClientIP:           info.ClientIP,
		IPChain:            info.ClientIP,
		IPChainTrustedHops: 1,
		UserAgentRaw:       info.UserAgent,
	}
	if err != nil {
		record.ErrorCode = problemCode(err)
		record.ErrorClass = "application"
		record.ErrorMessage = problemCode(err)
		if outcome == "denied" {
			record.DenialReason = record.ErrorCode
		}
	}
	sendGovernanceAudit(ctx, record)
}
