package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/domain-transfer-objects"
	"github.com/verself/profile-service/internal/profile"
	runtimeiam "github.com/verself/service-runtime/iam"
)

type emptyInput struct{}

type profileOutput struct {
	Body dto.ProfileSnapshot
}

type updateIdentityInput struct {
	Body dto.ProfileUpdateIdentityRequest
}

type putPreferencesInput struct {
	Body dto.ProfilePutPreferencesRequest
}

type mutationOutput struct {
	Body          dto.ProfileSnapshot
	changedFields []string
	beforeHash    string
	afterHash     string
}

func (o *mutationOutput) auditDetails() auditDetails {
	return auditDetails{
		changedFields: sortedChangedFields(o.changedFields),
		beforeHash:    o.beforeHash,
		afterHash:     o.afterHash,
	}
}

func RegisterRoutes(api huma.API, svc *profile.Service, authorizer runtimeiam.OperationAuthorizer) {
	registerProfileRoute(api, authorizer, huma.Operation{
		OperationID: "get-profile",
		Method:      http.MethodGet,
		Path:        "/api/v1/profile",
		Summary:     "Get the current human profile snapshot",
	}, profileOperationPolicy{
		OperationPolicy: runtimeiam.OperationPolicy{
			Permission:     permissionProfileRead,
			Resource:       "profile_subject",
			Action:         runtimeiam.ActionRead,
			OrgScope:       runtimeiam.OrgScopeTokenOrgID,
			RateLimitClass: "read",
			AuditEvent:     "profile.subject.read",
		},
	}, getProfile(svc))

	registerProfileRoute(api, authorizer, huma.Operation{
		OperationID:   "patch-profile-identity",
		Method:        http.MethodPatch,
		Path:          "/api/v1/profile/identity",
		Summary:       "Update the current human's identity profile fields",
		DefaultStatus: http.StatusOK,
	}, profileOperationPolicy{
		OperationPolicy: runtimeiam.OperationPolicy{
			Permission:     permissionProfileIdentity,
			Resource:       "profile_identity",
			Action:         runtimeiam.ActionWrite,
			OrgScope:       runtimeiam.OrgScopeTokenSubject,
			RateLimitClass: "profile_mutation",
			Idempotency:    idempotencyHeaderKey,
			AuditEvent:     "profile.subject.identity.write",
			BodyLimitBytes: bodyLimitSmallJSON,
		},
	}, updateIdentity(svc))

	registerProfileRoute(api, authorizer, huma.Operation{
		OperationID:   "put-profile-preferences",
		Method:        http.MethodPut,
		Path:          "/api/v1/profile/preferences",
		Summary:       "Replace the current human's profile preferences",
		DefaultStatus: http.StatusOK,
	}, profileOperationPolicy{
		OperationPolicy: runtimeiam.OperationPolicy{
			Permission:     permissionProfilePreferences,
			Resource:       "profile_preferences",
			Action:         runtimeiam.ActionWrite,
			OrgScope:       runtimeiam.OrgScopeTokenSubject,
			RateLimitClass: "profile_mutation",
			Idempotency:    idempotencyHeaderKey,
			AuditEvent:     "profile.preferences.write",
			BodyLimitBytes: bodyLimitSmallJSON,
		},
	}, putPreferences(svc))
}

func getProfile(svc *profile.Service) func(context.Context, *emptyInput) (*profileOutput, error) {
	return func(ctx context.Context, _ *emptyInput) (*profileOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		snapshot, err := svc.Snapshot(ctx, principal)
		if err != nil {
			return nil, profileError(ctx, err)
		}
		return &profileOutput{Body: snapshotDTO(snapshot)}, nil
	}
}

func updateIdentity(svc *profile.Service) func(context.Context, *updateIdentityInput) (*mutationOutput, error) {
	return func(ctx context.Context, input *updateIdentityInput) (*mutationOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		rawToken, ok := rawBearerTokenFromContext(ctx)
		if !ok {
			return nil, unauthorized(ctx)
		}
		before := versionHash("identity", input.Body.Version)
		snapshot, changed, err := svc.UpdateIdentity(ctx, principal, profile.UpdateIdentityRequest{
			Version:     input.Body.Version,
			GivenName:   input.Body.GivenName,
			FamilyName:  input.Body.FamilyName,
			DisplayName: input.Body.DisplayName,
		}, rawToken.value)
		if err != nil {
			return nil, profileError(ctx, err)
		}
		return &mutationOutput{
			Body:          snapshotDTO(snapshot),
			changedFields: changed,
			beforeHash:    before,
			afterHash:     versionHash("identity", snapshot.Identity.Version),
		}, nil
	}
}

func putPreferences(svc *profile.Service) func(context.Context, *putPreferencesInput) (*mutationOutput, error) {
	return func(ctx context.Context, input *putPreferencesInput) (*mutationOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		before := versionHash("preferences", input.Body.Version)
		snapshot, changed, err := svc.PutPreferences(ctx, principal, profile.PutPreferencesRequest{
			Version:        input.Body.Version,
			Locale:         input.Body.Locale,
			Timezone:       input.Body.Timezone,
			TimeDisplay:    input.Body.TimeDisplay,
			Theme:          input.Body.Theme,
			DefaultSurface: input.Body.DefaultSurface,
		})
		if err != nil {
			return nil, profileError(ctx, err)
		}
		return &mutationOutput{
			Body:          snapshotDTO(snapshot),
			changedFields: changed,
			beforeHash:    before,
			afterHash:     versionHash("preferences", snapshot.Preferences.Version),
		}, nil
	}
}

func snapshotDTO(snapshot profile.Snapshot) dto.ProfileSnapshot {
	return dto.ProfileSnapshot{
		SubjectID:   snapshot.SubjectID,
		OrgID:       snapshot.OrgID,
		Identity:    identityDTO(snapshot.Identity),
		Preferences: preferencesDTO(snapshot.Preferences),
	}
}

func identityDTO(identity profile.IdentitySummary) dto.ProfileIdentity {
	return dto.ProfileIdentity{
		Version:     identity.Version,
		Email:       identity.Email,
		GivenName:   identity.GivenName,
		FamilyName:  identity.FamilyName,
		DisplayName: identity.DisplayName,
		SyncedAt:    identity.SyncedAt,
	}
}

func preferencesDTO(preferences profile.Preferences) dto.ProfilePreferences {
	return dto.ProfilePreferences{
		Version:        preferences.Version,
		Locale:         preferences.Locale,
		Timezone:       preferences.Timezone,
		TimeDisplay:    preferences.TimeDisplay,
		Theme:          preferences.Theme,
		DefaultSurface: preferences.DefaultSurface,
		UpdatedAt:      preferences.UpdatedAt,
		UpdatedBy:      preferences.UpdatedBy,
	}
}

func artifactBytes(artifacts []dto.ProfileDataRightsArtifact) uint64 {
	var total uint64
	for _, artifact := range artifacts {
		bytes, err := strconv.ParseUint(artifact.Bytes, 10, 64)
		if err == nil {
			total += bytes
		}
	}
	return total
}
