package api

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/apiwire"
	auth "github.com/verself/auth-middleware"
	"github.com/verself/identity-service/internal/identity"
)

func RegisterRoutes(api huma.API, svc *identity.Service) {
	registerSecured(api, svc, secured(huma.Operation{
		OperationID: "get-organization",
		Method:      http.MethodGet,
		Path:        "/api/v1/organization",
		Summary:     "Get organization identity state",
	}, operationPolicy{
		Permission:     permissionOrganizationRead,
		Resource:       "organization",
		Action:         "read",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "identity.organization.read",
	}), getOrganization(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID:   "patch-organization",
		Method:        http.MethodPatch,
		Path:          "/api/v1/organization",
		Summary:       "Update organization profile",
		DefaultStatus: http.StatusOK,
	}, operationPolicy{
		Permission:     permissionOrganizationWrite,
		Resource:       "organization",
		Action:         "update",
		OrgScope:       "token_org_id",
		RateLimitClass: "organization_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "identity.organization.update",
		BodyLimitBytes: bodyLimitSmallJSON,
	}), updateOrganization(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID: "list-organization-members",
		Method:      http.MethodGet,
		Path:        "/api/v1/organization/members",
		Summary:     "List organization members",
	}, operationPolicy{
		Permission:     permissionMemberRead,
		Resource:       "organization_member",
		Action:         "list",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "identity.organization.member.list",
	}), listMembers(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID:   "invite-organization-member",
		Method:        http.MethodPost,
		Path:          "/api/v1/organization/members",
		Summary:       "Invite an organization member",
		DefaultStatus: 201,
	}, operationPolicy{
		Permission:     permissionMemberInvite,
		Resource:       "organization_member",
		Action:         "invite",
		OrgScope:       "token_org_id",
		RateLimitClass: "member_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "identity.organization.member.invite",
		BodyLimitBytes: bodyLimitSmallJSON,
	}), inviteMember(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID:   "update-organization-member-roles",
		Method:        http.MethodPut,
		Path:          "/api/v1/organization/members/{user_id}/roles",
		Summary:       "Update organization member roles",
		DefaultStatus: 200,
	}, operationPolicy{
		Permission:     permissionMemberRolesWrite,
		Resource:       "organization_member_roles",
		Action:         "write",
		OrgScope:       "token_org_id",
		RateLimitClass: "member_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "identity.organization.member.roles.write",
		BodyLimitBytes: bodyLimitSmallJSON,
	}), updateMemberRoles(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID: "get-organization-member-capabilities",
		Method:      http.MethodGet,
		Path:        "/api/v1/organization/member-capabilities",
		Summary:     "Get organization member capabilities and the static capability catalog",
	}, operationPolicy{
		Permission:     permissionMemberCapabilitiesRead,
		Resource:       "organization_member_capabilities",
		Action:         "read",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "identity.organization.member_capabilities.read",
	}), getMemberCapabilities(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID:   "put-organization-member-capabilities",
		Method:        http.MethodPut,
		Path:          "/api/v1/organization/member-capabilities",
		Summary:       "Replace the organization's enabled member capability set",
		DefaultStatus: 200,
	}, operationPolicy{
		Permission:     permissionMemberCapabilitiesWrite,
		Resource:       "organization_member_capabilities",
		Action:         "write",
		OrgScope:       "token_org_id",
		RateLimitClass: "member_capabilities_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "identity.organization.member_capabilities.write",
		BodyLimitBytes: bodyLimitSmallJSON,
	}), putMemberCapabilities(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID: "list-api-credentials",
		Method:      http.MethodGet,
		Path:        "/api/v1/organization/api-credentials",
		Summary:     "List organization API credentials",
	}, operationPolicy{
		Permission:     permissionAPICredentialsRead,
		Resource:       "api_credential",
		Action:         "list",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "identity.api_credential.list",
	}), listAPICredentials(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID: "get-api-credential",
		Method:      http.MethodGet,
		Path:        "/api/v1/organization/api-credentials/{credential_id}",
		Summary:     "Get API credential metadata",
	}, operationPolicy{
		Permission:     permissionAPICredentialsRead,
		Resource:       "api_credential",
		Action:         "read",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "identity.api_credential.read",
	}), getAPICredential(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID:   "create-api-credential",
		Method:        http.MethodPost,
		Path:          "/api/v1/organization/api-credentials",
		Summary:       "Create an API credential",
		DefaultStatus: 201,
	}, operationPolicy{
		Permission:     permissionAPICredentialsCreate,
		Resource:       "api_credential",
		Action:         "create",
		OrgScope:       "token_org_id",
		RateLimitClass: "api_credential_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "identity.api_credential.create",
		BodyLimitBytes: bodyLimitSmallJSON,
	}), createAPICredential(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID:   "roll-api-credential",
		Method:        http.MethodPost,
		Path:          "/api/v1/organization/api-credentials/{credential_id}/roll",
		Summary:       "Roll API credential secret material",
		DefaultStatus: 200,
	}, operationPolicy{
		Permission:     permissionAPICredentialsRoll,
		Resource:       "api_credential",
		Action:         "roll",
		OrgScope:       "token_org_id",
		RateLimitClass: "api_credential_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "identity.api_credential.roll",
		BodyLimitBytes: bodyLimitSmallJSON,
	}), rollAPICredential(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID:   "revoke-api-credential",
		Method:        http.MethodDelete,
		Path:          "/api/v1/organization/api-credentials/{credential_id}",
		Summary:       "Revoke an API credential",
		DefaultStatus: 200,
	}, operationPolicy{
		Permission:     permissionAPICredentialsRevoke,
		Resource:       "api_credential",
		Action:         "revoke",
		OrgScope:       "token_org_id",
		RateLimitClass: "api_credential_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "identity.api_credential.revoke",
		BodyLimitBytes: bodyLimitNoBody,
	}), revokeAPICredential(svc))
}

type emptyInput struct{}

type organizationOutput struct {
	Body apiwire.IdentityOrganization
}

type updateOrganizationInput struct {
	Body apiwire.IdentityUpdateOrganizationRequest
}

type membersOutput struct {
	Body apiwire.IdentityMembers
}

type inviteMemberInput struct {
	Body apiwire.IdentityInviteMemberRequest
}

type inviteMemberOutput struct {
	Body apiwire.IdentityInviteMemberResponse
}

type memberRolesPath struct {
	UserID string `path:"user_id" doc:"Zitadel user ID"`
	Body   apiwire.IdentityUpdateMemberRolesRequest
}

type memberOutput struct {
	Body apiwire.IdentityMember
}

type memberCapabilitiesOutput struct {
	Body apiwire.IdentityMemberCapabilities
}

type putMemberCapabilitiesInput struct {
	Body apiwire.IdentityPutMemberCapabilitiesRequest
}

type apiCredentialPath struct {
	CredentialID string `path:"credential_id" doc:"Verself API credential ID"`
}

type apiCredentialsOutput struct {
	Body apiwire.IdentityAPICredentials
}

type apiCredentialOutput struct {
	Body apiwire.IdentityAPICredential
}

type createAPICredentialInput struct {
	Body apiwire.IdentityCreateAPICredentialRequest
}

type createAPICredentialOutput struct {
	Body apiwire.IdentityCreateAPICredentialResponse
}

type rollAPICredentialInput struct {
	CredentialID string `path:"credential_id" doc:"Verself API credential ID"`
	Body         apiwire.IdentityRollAPICredentialRequest
}

type rollAPICredentialOutput struct {
	Body apiwire.IdentityRollAPICredentialResponse
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
	if _, err := apiwire.ParseUint64(authIdentity.OrgID); err != nil {
		return identity.Principal{}, badRequest(ctx, "invalid-token-org", "token org_id must be an unsigned integer", err)
	}
	return identity.Principal{
		Subject:           authIdentity.Subject,
		OrgID:             authIdentity.OrgID,
		OrgDisplayName:    organizationDisplayNameForTokenOrg(authIdentity),
		Roles:             identityRolesForCurrentOrg(authIdentity),
		DirectPermissions: directPermissionsFromAuthIdentity(authIdentity),
		Email:             authIdentity.Email,
	}, nil
}

func getOrganization(svc *identity.Service) func(context.Context, *emptyInput) (*organizationOutput, error) {
	return func(ctx context.Context, _ *emptyInput) (*organizationOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		org, err := svc.Organization(ctx, principal)
		if err != nil {
			return nil, identityError(ctx, err)
		}
		return &organizationOutput{Body: organizationDTO(org)}, nil
	}
}

func updateOrganization(svc *identity.Service) func(context.Context, *updateOrganizationInput) (*organizationOutput, error) {
	return func(ctx context.Context, input *updateOrganizationInput) (*organizationOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		org, err := svc.UpdateOrganization(ctx, principal, identity.UpdateOrganizationRequest{
			Version:     input.Body.Version,
			DisplayName: input.Body.DisplayName,
			Slug:        input.Body.Slug,
		})
		if err != nil {
			return nil, identityError(ctx, err)
		}
		return &organizationOutput{Body: organizationDTO(org)}, nil
	}
}

func listMembers(svc *identity.Service) func(context.Context, *emptyInput) (*membersOutput, error) {
	return func(ctx context.Context, _ *emptyInput) (*membersOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		members, err := svc.Members(ctx, principal)
		if err != nil {
			return nil, identityError(ctx, err)
		}
		return &membersOutput{Body: apiwire.IdentityMembers{Members: memberDTOs(members)}}, nil
	}
}

func inviteMember(svc *identity.Service) func(context.Context, *inviteMemberInput) (*inviteMemberOutput, error) {
	return func(ctx context.Context, input *inviteMemberInput) (*inviteMemberOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		result, err := svc.InviteMember(ctx, principal, identity.InviteMemberRequest{
			Email:      input.Body.Email,
			GivenName:  input.Body.GivenName,
			FamilyName: input.Body.FamilyName,
			RoleKeys:   input.Body.RoleKeys,
		})
		if err != nil {
			return nil, identityError(ctx, err)
		}
		return &inviteMemberOutput{Body: apiwire.IdentityInviteMemberResponse{
			UserID:   result.UserID,
			Email:    result.Email,
			RoleKeys: result.RoleKeys,
			Status:   result.Status,
		}}, nil
	}
}

func updateMemberRoles(svc *identity.Service) func(context.Context, *memberRolesPath) (*memberOutput, error) {
	return func(ctx context.Context, input *memberRolesPath) (*memberOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		result, err := svc.UpdateMemberRoles(ctx, principal, identity.UpdateMemberRolesCommand{
			UserID:                input.UserID,
			RoleKeys:              input.Body.RoleKeys,
			ExpectedRoleKeys:      input.Body.ExpectedRoleKeys,
			ExpectedOrgACLVersion: input.Body.ExpectedOrgACLVersion,
			OperationID:           "update-organization-member-roles",
			IdempotencyKey:        operationRequestInfoFromContext(ctx).IdempotencyKey,
		})
		if err != nil {
			return nil, identityError(ctx, err)
		}
		return &memberOutput{Body: memberDTO(result.Member)}, nil
	}
}

func getMemberCapabilities(svc *identity.Service) func(context.Context, *emptyInput) (*memberCapabilitiesOutput, error) {
	return func(ctx context.Context, _ *emptyInput) (*memberCapabilitiesOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		doc, err := svc.MemberCapabilities(ctx, principal)
		if err != nil {
			return nil, identityError(ctx, err)
		}
		return &memberCapabilitiesOutput{Body: memberCapabilitiesDTO(doc)}, nil
	}
}

func putMemberCapabilities(svc *identity.Service) func(context.Context, *putMemberCapabilitiesInput) (*memberCapabilitiesOutput, error) {
	return func(ctx context.Context, input *putMemberCapabilitiesInput) (*memberCapabilitiesOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		doc, err := svc.PutMemberCapabilities(ctx, principal, identity.MemberCapabilitiesDocument{
			Version:     input.Body.Version,
			EnabledKeys: append([]string(nil), input.Body.EnabledKeys...),
		})
		if err != nil {
			return nil, identityError(ctx, err)
		}
		return &memberCapabilitiesOutput{Body: memberCapabilitiesDTO(doc)}, nil
	}
}

func listAPICredentials(svc *identity.Service) func(context.Context, *emptyInput) (*apiCredentialsOutput, error) {
	return func(ctx context.Context, _ *emptyInput) (*apiCredentialsOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		credentials, err := svc.ListAPICredentials(ctx, principal)
		if err != nil {
			return nil, identityError(ctx, err)
		}
		return &apiCredentialsOutput{Body: apiwire.IdentityAPICredentials{Credentials: apiCredentialDTOs(credentials)}}, nil
	}
}

func getAPICredential(svc *identity.Service) func(context.Context, *apiCredentialPath) (*apiCredentialOutput, error) {
	return func(ctx context.Context, input *apiCredentialPath) (*apiCredentialOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		credential, err := svc.GetAPICredential(ctx, principal, input.CredentialID)
		if err != nil {
			return nil, identityError(ctx, err)
		}
		return &apiCredentialOutput{Body: apiCredentialDTO(credential)}, nil
	}
}

func createAPICredential(svc *identity.Service) func(context.Context, *createAPICredentialInput) (*createAPICredentialOutput, error) {
	return func(ctx context.Context, input *createAPICredentialInput) (*createAPICredentialOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		result, err := svc.CreateAPICredential(ctx, principal, identity.CreateAPICredentialRequest{
			DisplayName: input.Body.DisplayName,
			AuthMethod:  identity.APICredentialAuthMethod(input.Body.AuthMethod),
			Permissions: input.Body.Permissions,
			ExpiresAt:   input.Body.ExpiresAt,
		})
		if err != nil {
			return nil, identityError(ctx, err)
		}
		return &createAPICredentialOutput{Body: apiwire.IdentityCreateAPICredentialResponse{
			Credential:     apiCredentialDTO(result.Credential),
			IssuedMaterial: issuedMaterialDTO(result.IssuedMaterial),
		}}, nil
	}
}

func rollAPICredential(svc *identity.Service) func(context.Context, *rollAPICredentialInput) (*rollAPICredentialOutput, error) {
	return func(ctx context.Context, input *rollAPICredentialInput) (*rollAPICredentialOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		result, err := svc.RollAPICredential(ctx, principal, input.CredentialID, identity.RollAPICredentialRequest{
			AuthMethod: identity.APICredentialAuthMethod(input.Body.AuthMethod),
		})
		if err != nil {
			return nil, identityError(ctx, err)
		}
		return &rollAPICredentialOutput{Body: apiwire.IdentityRollAPICredentialResponse{
			Credential:     apiCredentialDTO(result.Credential),
			IssuedMaterial: issuedMaterialDTO(result.IssuedMaterial),
		}}, nil
	}
}

func revokeAPICredential(svc *identity.Service) func(context.Context, *apiCredentialPath) (*apiCredentialOutput, error) {
	return func(ctx context.Context, input *apiCredentialPath) (*apiCredentialOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		credential, err := svc.RevokeAPICredential(ctx, principal, input.CredentialID)
		if err != nil {
			return nil, identityError(ctx, err)
		}
		return &apiCredentialOutput{Body: apiCredentialDTO(credential)}, nil
	}
}

func organizationDTO(org identity.Organization) apiwire.IdentityOrganization {
	return apiwire.IdentityOrganization{
		OrgID:              orgID(org.OrgID),
		DisplayName:        org.DisplayName,
		Slug:               org.Slug,
		Version:            org.Version,
		OrgACLVersion:      org.OrgACLVersion,
		Caller:             memberDTO(org.Caller),
		MemberCapabilities: memberCapabilitiesDocumentDTO(org.MemberCapabilities),
		Permissions:        append([]string(nil), org.Permissions...),
	}
}

func organizationProfileDTO(profile identity.OrganizationProfile) apiwire.IdentityOrganizationProfile {
	return apiwire.IdentityOrganizationProfile{
		OrgID:          orgID(profile.OrgID),
		DisplayName:    profile.DisplayName,
		Slug:           profile.Slug,
		State:          string(profile.State),
		Version:        profile.Version,
		UpdatedAt:      profile.UpdatedAt,
		RedirectedFrom: profile.RedirectedFrom,
	}
}

func memberDTOs(members []identity.Member) []apiwire.IdentityMember {
	out := make([]apiwire.IdentityMember, 0, len(members))
	for _, member := range members {
		out = append(out, memberDTO(member))
	}
	return out
}

func memberDTO(member identity.Member) apiwire.IdentityMember {
	return apiwire.IdentityMember{
		UserID:      member.UserID,
		Email:       member.Email,
		LoginName:   member.LoginName,
		DisplayName: member.DisplayName,
		State:       member.State,
		RoleKeys:    append([]string(nil), member.RoleKeys...),
	}
}

func memberCapabilitiesDocumentDTO(doc identity.MemberCapabilitiesDocument) apiwire.IdentityMemberCapabilitiesDocument {
	return apiwire.IdentityMemberCapabilitiesDocument{
		OrgID:       orgID(doc.OrgID),
		Version:     doc.Version,
		EnabledKeys: append([]string(nil), doc.EnabledKeys...),
		UpdatedAt:   doc.UpdatedAt,
		UpdatedBy:   doc.UpdatedBy,
	}
}

func memberCapabilitiesDTO(doc identity.MemberCapabilitiesDocument) apiwire.IdentityMemberCapabilities {
	catalog := identity.DefaultCapabilities()
	dtoCatalog := make([]apiwire.IdentityMemberCapability, 0, len(catalog))
	for _, capability := range catalog {
		dtoCatalog = append(dtoCatalog, apiwire.IdentityMemberCapability{
			Key:            capability.Key,
			Label:          capability.Label,
			Description:    capability.Description,
			DefaultEnabled: capability.DefaultEnabled,
			Permissions:    append([]string(nil), capability.Permissions...),
		})
	}
	return apiwire.IdentityMemberCapabilities{
		Document: memberCapabilitiesDocumentDTO(doc),
		Catalog:  dtoCatalog,
	}
}

func apiCredentialDTOs(credentials []identity.APICredential) []apiwire.IdentityAPICredential {
	out := make([]apiwire.IdentityAPICredential, 0, len(credentials))
	for _, credential := range credentials {
		out = append(out, apiCredentialDTO(credential))
	}
	return out
}

func apiCredentialDTO(credential identity.APICredential) apiwire.IdentityAPICredential {
	return apiwire.IdentityAPICredential{
		CredentialID:         credential.CredentialID,
		OrgID:                orgID(credential.OrgID),
		SubjectID:            credential.SubjectID,
		ClientID:             credential.ClientID,
		DisplayName:          credential.DisplayName,
		Status:               string(credential.Status),
		AuthMethod:           string(credential.AuthMethod),
		Fingerprint:          credential.Fingerprint,
		Permissions:          append([]string(nil), credential.Permissions...),
		PolicyVersionAtIssue: credential.PolicyVersionAtIssue,
		CreatedAt:            credential.CreatedAt,
		CreatedBy:            credential.CreatedBy,
		UpdatedAt:            credential.UpdatedAt,
		ExpiresAt:            credential.ExpiresAt,
		RevokedAt:            credential.RevokedAt,
		RevokedBy:            credential.RevokedBy,
		LastUsedAt:           credential.LastUsedAt,
	}
}

func issuedMaterialDTO(material identity.APICredentialIssuedMaterial) apiwire.IdentityAPICredentialIssuedMaterial {
	return apiwire.IdentityAPICredentialIssuedMaterial{
		AuthMethod:   string(material.AuthMethod),
		ClientID:     material.ClientID,
		TokenURL:     material.TokenURL,
		KeyID:        material.KeyID,
		KeyContent:   material.KeyContent,
		ClientSecret: material.ClientSecret,
		Fingerprint:  material.Fingerprint,
	}
}

func directPermissionsFromAuthIdentity(authIdentity *auth.Identity) []string {
	if authIdentity == nil {
		return nil
	}
	credentialID, _ := authIdentity.Raw["verself:credential_id"].(string)
	if strings.TrimSpace(credentialID) == "" {
		return nil
	}
	out := []string{}
	for _, claimKey := range []string{"permissions", "permission"} {
		out = append(out, stringClaimList(authIdentity.Raw[claimKey])...)
	}
	sort.Strings(out)
	return compactStrings(out)
}

func orgID(value string) apiwire.OrgID {
	parsed, err := apiwire.ParseUint64(value)
	if err != nil {
		return apiwire.Uint64(0)
	}
	return apiwire.Uint64(parsed)
}

func organizationDisplayNameForTokenOrg(authIdentity *auth.Identity) string {
	if authIdentity == nil {
		return ""
	}
	for _, assignment := range authIdentity.RoleAssignments {
		if assignment.OrganizationID == authIdentity.OrgID && strings.TrimSpace(assignment.OrganizationName) != "" {
			return strings.TrimSpace(assignment.OrganizationName)
		}
	}
	return ""
}
