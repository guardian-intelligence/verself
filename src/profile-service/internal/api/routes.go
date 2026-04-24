package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/forge-metal/apiwire"
	"github.com/forge-metal/profile-service/internal/profile"
)

type emptyInput struct{}

type profileOutput struct {
	Body apiwire.ProfileSnapshot
}

type updateIdentityInput struct {
	Body apiwire.ProfileUpdateIdentityRequest
}

type putPreferencesInput struct {
	Body apiwire.ProfilePutPreferencesRequest
}

type mutationOutput struct {
	Body          apiwire.ProfileSnapshot
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

func RegisterRoutes(api huma.API, svc *profile.Service) {
	registerProfileRoute(api, huma.Operation{
		OperationID: "get-profile",
		Method:      http.MethodGet,
		Path:        "/api/v1/profile",
		Summary:     "Get the current human profile snapshot",
	}, operationPolicy{
		Permission:       permissionProfileRead,
		Resource:         "profile_subject",
		Action:           "read",
		OrgScope:         "token_org_id",
		RateLimitClass:   "read",
		AuditEvent:       "profile.subject.read",
		OperationDisplay: "get profile",
		OperationType:    "read",
		RiskLevel:        "low",
	}, getProfile(svc))

	registerProfileRoute(api, huma.Operation{
		OperationID:   "patch-profile-identity",
		Method:        http.MethodPatch,
		Path:          "/api/v1/profile/identity",
		Summary:       "Update the current human's identity profile fields",
		DefaultStatus: http.StatusOK,
	}, operationPolicy{
		Permission:       permissionProfileIdentity,
		Resource:         "profile_identity",
		Action:           "write",
		OrgScope:         "token_subject",
		RateLimitClass:   "profile_mutation",
		Idempotency:      idempotencyHeaderKey,
		AuditEvent:       "profile.subject.identity.write",
		OperationDisplay: "update profile identity",
		OperationType:    "write",
		RiskLevel:        "medium",
		BodyLimitBytes:   bodyLimitSmallJSON,
	}, updateIdentity(svc))

	registerProfileRoute(api, huma.Operation{
		OperationID:   "put-profile-preferences",
		Method:        http.MethodPut,
		Path:          "/api/v1/profile/preferences",
		Summary:       "Replace the current human's profile preferences",
		DefaultStatus: http.StatusOK,
	}, operationPolicy{
		Permission:       permissionProfilePreferences,
		Resource:         "profile_preferences",
		Action:           "write",
		OrgScope:         "token_subject",
		RateLimitClass:   "profile_mutation",
		Idempotency:      idempotencyHeaderKey,
		AuditEvent:       "profile.preferences.write",
		OperationDisplay: "update profile preferences",
		OperationType:    "write",
		RiskLevel:        "low",
		BodyLimitBytes:   bodyLimitSmallJSON,
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

func snapshotDTO(snapshot profile.Snapshot) apiwire.ProfileSnapshot {
	return apiwire.ProfileSnapshot{
		SubjectID:   snapshot.SubjectID,
		OrgID:       snapshot.OrgID,
		Identity:    identityDTO(snapshot.Identity),
		Preferences: preferencesDTO(snapshot.Preferences),
	}
}

func identityDTO(identity profile.IdentitySummary) apiwire.ProfileIdentity {
	return apiwire.ProfileIdentity{
		Version:     identity.Version,
		Email:       identity.Email,
		GivenName:   identity.GivenName,
		FamilyName:  identity.FamilyName,
		DisplayName: identity.DisplayName,
		SyncedAt:    identity.SyncedAt,
	}
}

func preferencesDTO(preferences profile.Preferences) apiwire.ProfilePreferences {
	return apiwire.ProfilePreferences{
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

func artifactBytes(artifacts []apiwire.ProfileDataRightsArtifact) uint64 {
	var total uint64
	for _, artifact := range artifacts {
		bytes, err := strconv.ParseUint(artifact.Bytes, 10, 64)
		if err == nil {
			total += bytes
		}
	}
	return total
}
