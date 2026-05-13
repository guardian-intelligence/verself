package api

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/profile-service/internal/contractapi"
	"github.com/verself/profile-service/internal/profile"
	runtimeiam "github.com/verself/service-runtime/iam"
)

type mutationOutput struct {
	Body          contractapi.ProfileOutputBody
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
	op, policy := profileContract(contractapi.GetProfile.Descriptor, "Get the current human profile snapshot")
	registerProfileRoute(api, authorizer, op, policy, getProfile(svc))
	op, policy = profileContract(contractapi.PatchProfileIdentity.Descriptor, "Update the current human's identity profile fields")
	registerProfileRoute(api, authorizer, op, policy, updateIdentity(svc))
	op, policy = profileContract(contractapi.PutProfilePreferences.Descriptor, "Replace the current human's profile preferences")
	registerProfileRoute(api, authorizer, op, policy, putPreferences(svc))
}

func profileContract(desc contractapi.OperationDescriptor, summary string) (huma.Operation, profileOperationPolicy) {
	op := huma.Operation{
		OperationID:   desc.OperationID,
		Method:        desc.Method,
		Path:          desc.Path,
		Summary:       summary,
		DefaultStatus: desc.DefaultStatus,
		Errors:        contractProblemStatuses(desc.Problems),
		Extensions:    map[string]any{"x-verself-contract": contractExtension(desc)},
	}
	return op, profileOperationPolicy{OperationPolicy: operationPolicyFromContract(desc)}
}

func getProfile(svc *profile.Service) func(context.Context, *contractapi.EmptyInput) (*contractapi.ProfileOutput, error) {
	return func(ctx context.Context, _ *contractapi.EmptyInput) (*contractapi.ProfileOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		snapshot, err := svc.Snapshot(ctx, principal)
		if err != nil {
			return nil, profileError(ctx, err)
		}
		return &contractapi.ProfileOutput{Body: profileOutputBody(snapshot)}, nil
	}
}

func updateIdentity(svc *profile.Service) func(context.Context, *contractapi.PatchProfileIdentityInput) (*mutationOutput, error) {
	return func(ctx context.Context, input *contractapi.PatchProfileIdentityInput) (*mutationOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		rawToken, ok := rawBearerTokenFromContext(ctx)
		if !ok {
			return nil, unauthorized(ctx)
		}
		version, err := int32FromDecimalInt64(string(input.Body.Version), "identity version")
		if err != nil {
			return nil, badRequest(ctx, "identity-version-out-of-range", "identity version is outside the supported range", err)
		}
		before := versionHash("identity", version)
		snapshot, changed, err := svc.UpdateIdentity(ctx, principal, profile.UpdateIdentityRequest{
			Version:     version,
			GivenName:   string(input.Body.GivenName),
			FamilyName:  string(input.Body.FamilyName),
			DisplayName: contractString(input.Body.DisplayName),
		}, rawToken.value)
		if err != nil {
			return nil, profileError(ctx, err)
		}
		return &mutationOutput{
			Body:          profileOutputBody(snapshot),
			changedFields: changed,
			beforeHash:    before,
			afterHash:     versionHash("identity", snapshot.Identity.Version),
		}, nil
	}
}

func putPreferences(svc *profile.Service) func(context.Context, *contractapi.PutProfilePreferencesInput) (*mutationOutput, error) {
	return func(ctx context.Context, input *contractapi.PutProfilePreferencesInput) (*mutationOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		version, err := int32FromDecimalInt64(string(input.Body.Version), "preferences version")
		if err != nil {
			return nil, badRequest(ctx, "preferences-version-out-of-range", "preferences version is outside the supported range", err)
		}
		before := versionHash("preferences", version)
		snapshot, changed, err := svc.PutPreferences(ctx, principal, profile.PutPreferencesRequest{
			Version:        version,
			Locale:         contractString(input.Body.Locale),
			Timezone:       contractString(input.Body.Timezone),
			TimeDisplay:    contractString(input.Body.TimeDisplay),
			Theme:          contractString(input.Body.Theme),
			DefaultSurface: contractString(input.Body.DefaultSurface),
		})
		if err != nil {
			return nil, profileError(ctx, err)
		}
		return &mutationOutput{
			Body:          profileOutputBody(snapshot),
			changedFields: changed,
			beforeHash:    before,
			afterHash:     versionHash("preferences", snapshot.Preferences.Version),
		}, nil
	}
}

func profileOutputBody(snapshot profile.Snapshot) contractapi.ProfileOutputBody {
	return contractapi.ProfileOutputBody{Profile: profileSnapshotContract(snapshot)}
}

func profileSnapshotContract(snapshot profile.Snapshot) contractapi.ProfileSnapshot {
	return contractapi.ProfileSnapshot{
		SubjectID:   contractapi.SubjectID(snapshot.SubjectID),
		OrgID:       contractapi.OrgID(snapshot.OrgID),
		Identity:    identityContract(snapshot.Identity),
		Preferences: preferencesContract(snapshot.Preferences),
	}
}

func identityContract(identity profile.IdentitySummary) contractapi.ProfileIdentityView {
	return contractapi.ProfileIdentityView{
		Version:     contractapi.DecimalInt64(strconv.FormatInt(int64(identity.Version), 10)),
		Email:       optionalContractString[contractapi.EmailAddress](identity.Email),
		GivenName:   optionalContractString[contractapi.GivenName](identity.GivenName),
		FamilyName:  optionalContractString[contractapi.FamilyName](identity.FamilyName),
		DisplayName: optionalContractString[contractapi.DisplayName](identity.DisplayName),
		SyncedAt:    optionalContractTime(identity.SyncedAt),
	}
}

func preferencesContract(preferences profile.Preferences) contractapi.ProfilePreferencesView {
	return contractapi.ProfilePreferencesView{
		Version:        contractapi.DecimalInt64(strconv.FormatInt(int64(preferences.Version), 10)),
		Locale:         optionalContractString[contractapi.Locale](preferences.Locale),
		Timezone:       optionalContractString[contractapi.Timezone](preferences.Timezone),
		TimeDisplay:    optionalContractString[contractapi.ProfilePreferenceValue](preferences.TimeDisplay),
		Theme:          optionalContractString[contractapi.ProfilePreferenceValue](preferences.Theme),
		DefaultSurface: optionalContractString[contractapi.ProfilePreferenceValue](preferences.DefaultSurface),
		UpdatedAt:      formatContractTime(preferences.UpdatedAt),
		UpdatedBy:      optionalContractString[contractapi.SubjectID](preferences.UpdatedBy),
	}
}

func contractString[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func optionalContractString[T ~string](value string) *T {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	out := T(value)
	return &out
}

func optionalContractTime(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	out := formatContractTime(*value)
	return &out
}

func formatContractTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func int32FromDecimalInt64(value string, field string) (int32, error) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0, err
	}
	if parsed < minInt32AsInt64 || parsed > maxInt32AsInt64 {
		return 0, strconv.ErrRange
	}
	return int32(parsed), nil // #nosec G115 -- parsed is range-checked above.
}
