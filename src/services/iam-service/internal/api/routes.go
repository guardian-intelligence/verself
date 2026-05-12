package api

import (
	"context"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/domain-transfer-objects"
	"github.com/verself/iam-service/internal/authz"
	"github.com/verself/iam-service/internal/contractapi"
	"github.com/verself/iam-service/internal/identity"
	auth "github.com/verself/service-runtime/auth"
)

func RegisterRoutes(api huma.API, svc *identity.Service, authzSvc *authz.Service, installationID string) {
	contractapi.RegisterPublic(api, publicRuntime{service: svc, authz: authzSvc}, publicHandlers{
		service:        svc,
		authz:          authzSvc,
		installationID: installationID,
	})
}

func requireIdentity(ctx context.Context) (*auth.Identity, error) {
	identity := auth.FromContext(ctx)
	if identity == nil {
		return nil, unauthorized(ctx)
	}
	return identity, nil
}

func principalFromContext(ctx context.Context) (identity.Principal, error) {
	authIdentity, err := requireIdentity(ctx)
	if err != nil {
		return identity.Principal{}, err
	}
	return principalFromAuthIdentity(ctx, authIdentity)
}

func principalFromAuthIdentity(ctx context.Context, authIdentity *auth.Identity) (identity.Principal, error) {
	if authIdentity == nil {
		return identity.Principal{}, unauthorized(ctx)
	}
	return principalForAuthIdentityOrg(ctx, authIdentity, authIdentity.OrgID)
}

func principalForAuthIdentityOrg(ctx context.Context, authIdentity *auth.Identity, orgID string) (identity.Principal, error) {
	if authIdentity == nil {
		return identity.Principal{}, unauthorized(ctx)
	}
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return identity.Principal{}, badRequest(ctx, "invalid-token-org", "token org_id is required", nil)
	}
	subject := authIdentity.Subject
	subjectKind := identity.AuthorizationSubjectKindUser
	if serviceAccountID, _ := authIdentity.Raw["verself:service_account_id"].(string); strings.TrimSpace(serviceAccountID) != "" {
		subject = strings.TrimSpace(serviceAccountID)
		subjectKind = identity.AuthorizationSubjectKindServiceAccount
	}
	return identity.Principal{
		Subject:     subject,
		SubjectKind: subjectKind,
		OrgID:       orgID,
		Roles:       identityRolesForCurrentOrg(authIdentity),
		Email:       authIdentity.Email,
	}, nil
}

func organizationProfileDTO(profile identity.OrganizationProfile) dto.IAMOrganizationProfile {
	return dto.IAMOrganizationProfile{
		OrgID:          orgID(profile.IdentityProviderOrgID),
		DisplayName:    profile.DisplayName,
		Slug:           profile.Slug,
		State:          string(profile.State),
		Version:        profile.Version,
		UpdatedAt:      profile.UpdatedAt,
		RedirectedFrom: profile.RedirectedFrom,
	}
}

func orgID(value string) dto.OrgID {
	parsed, err := dto.ParseUint64(value)
	if err != nil {
		return dto.Uint64(0)
	}
	return dto.Uint64(parsed)
}

func authzSubjectFromIdentity(authIdentity *auth.Identity) identity.AuthorizationSubject {
	if authIdentity == nil {
		return identity.AuthorizationSubject{Kind: identity.AuthorizationSubjectKindUser}
	}
	if credentialID, _ := authIdentity.Raw["verself:credential_id"].(string); strings.TrimSpace(credentialID) != "" {
		serviceAccountID, _ := authIdentity.Raw["verself:service_account_id"].(string)
		return identity.AuthorizationSubject{Kind: identity.AuthorizationSubjectKindServiceAccount, ID: strings.TrimSpace(serviceAccountID)}
	}
	if serviceAccountID, _ := authIdentity.Raw["verself:service_account_id"].(string); strings.TrimSpace(serviceAccountID) != "" {
		return identity.AuthorizationSubject{Kind: identity.AuthorizationSubjectKindServiceAccount, ID: strings.TrimSpace(serviceAccountID)}
	}
	return identity.AuthorizationSubject{Kind: identity.AuthorizationSubjectKindUser, ID: authIdentity.Subject}
}

func roleAssignmentOrgIDs(ctx context.Context, authIdentity *auth.Identity) ([]string, error) {
	if authIdentity == nil {
		return nil, unauthorized(ctx)
	}
	seen := map[string]struct{}{}
	orgIDs := make([]string, 0, len(authIdentity.RoleAssignments))
	for _, assignment := range authIdentity.RoleAssignments {
		orgID := strings.TrimSpace(assignment.OrganizationID)
		if orgID == "" {
			continue
		}
		if _, ok := seen[orgID]; ok {
			continue
		}
		seen[orgID] = struct{}{}
		orgIDs = append(orgIDs, orgID)
	}
	if len(orgIDs) == 0 {
		return nil, forbidden(ctx, "organization-role-assignment-required", "token does not carry any organization role assignments")
	}
	sort.Strings(orgIDs)
	return orgIDs, nil
}
