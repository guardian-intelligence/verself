package api

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	auth "github.com/verself/auth-middleware"
	"github.com/verself/domain-transfer-objects"
	"github.com/verself/iam-service/internal/authz"
	"github.com/verself/iam-service/internal/identity"
)

func RegisterRoutes(api huma.API, svc *identity.Service, authzSvc *authz.Service) {
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
		AuditEvent:     "iam.organization.read",
	}), getOrganization(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID: "list-my-organizations",
		Method:      http.MethodGet,
		Path:        "/api/v1/me/organizations",
		Summary:     "List organizations available to the caller",
	}, operationPolicy{
		Permission:     permissionOrganizationRead,
		Resource:       "organization",
		Action:         "list",
		OrgScope:       orgScopeTokenRoleAssignmentOrgIDs,
		RateLimitClass: "read",
		AuditEvent:     "iam.organization.membership.list",
	}), listMyOrganizations(svc))

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
		AuditEvent:     "iam.organization.update",
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
		AuditEvent:     "iam.organization.member.list",
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
		AuditEvent:     "iam.organization.member.invite",
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
		AuditEvent:     "iam.organization.member.roles.write",
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
		AuditEvent:     "iam.organization.member_capabilities.read",
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
		AuditEvent:     "iam.organization.member_capabilities.write",
		BodyLimitBytes: bodyLimitSmallJSON,
	}), putMemberCapabilities(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID: "get-organization-iam-policy",
		Method:      http.MethodGet,
		Path:        "/api/v1/organizations/{org_id}/iamPolicy",
		Summary:     "Get organization IAM policy",
	}, operationPolicy{
		Permission:     permissionIAMPolicyRead,
		Resource:       "organization_iam_policy",
		Action:         "read",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "iam.organization.policy.read",
	}), getOrganizationIAMPolicy(authzSvc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID:   "set-organization-iam-policy",
		Method:        http.MethodPut,
		Path:          "/api/v1/organizations/{org_id}/iamPolicy",
		Summary:       "Replace organization IAM policy",
		DefaultStatus: http.StatusOK,
	}, operationPolicy{
		Permission:     permissionIAMPolicySet,
		Resource:       "organization_iam_policy",
		Action:         "write",
		OrgScope:       "token_org_id",
		RateLimitClass: "iam_policy_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "iam.organization.policy.write",
		BodyLimitBytes: bodyLimitSmallJSON,
	}), setOrganizationIAMPolicy(authzSvc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID:   "test-organization-iam-permissions",
		Method:        http.MethodPost,
		Path:          "/api/v1/organizations/{org_id}/iamPolicy:testPermissions",
		Summary:       "Test caller permissions on an organization",
		DefaultStatus: http.StatusOK,
	}, operationPolicy{
		Permission:     permissionIAMPolicyTest,
		Resource:       "organization_iam_policy",
		Action:         "test",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "iam.organization.policy.test_permissions",
		OperationType:  "read",
		BodyLimitBytes: bodyLimitSmallJSON,
	}), testOrganizationIAMPermissions(authzSvc))

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
		AuditEvent:     "iam.api_credential.list",
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
		AuditEvent:     "iam.api_credential.read",
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
		AuditEvent:     "iam.api_credential.create",
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
		AuditEvent:     "iam.api_credential.roll",
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
		AuditEvent:     "iam.api_credential.revoke",
		BodyLimitBytes: bodyLimitNoBody,
	}), revokeAPICredential(svc))
}

type emptyInput struct{}

type organizationOutput struct {
	Body dto.IAMOrganization
}

type accessibleOrganizationsOutput struct {
	Body []dto.IAMOrganizationMetadata
}

type updateOrganizationInput struct {
	Body dto.IAMUpdateOrganizationRequest
}

type membersOutput struct {
	Body dto.IAMMembers
}

type inviteMemberInput struct {
	Body dto.IAMInviteMemberRequest
}

type inviteMemberOutput struct {
	Body dto.IAMInviteMemberResponse
}

type memberRolesPath struct {
	UserID string `path:"user_id" doc:"Zitadel user ID"`
	Body   dto.IAMUpdateMemberRolesRequest
}

type memberOutput struct {
	Body dto.IAMMember
}

type memberCapabilitiesOutput struct {
	Body dto.IAMMemberCapabilities
}

type putMemberCapabilitiesInput struct {
	Body dto.IAMPutMemberCapabilitiesRequest
}

type organizationIAMPolicyPath struct {
	OrgID string `path:"org_id" doc:"Zitadel organization ID"`
}

type organizationIAMPolicyOutput struct {
	Body dto.IAMPolicy
}

type setOrganizationIAMPolicyInput struct {
	OrgID string `path:"org_id" doc:"Zitadel organization ID"`
	Body  dto.IAMSetPolicyRequest
}

type testOrganizationIAMPermissionsInput struct {
	OrgID string `path:"org_id" doc:"Zitadel organization ID"`
	Body  dto.IAMTestPermissionsRequest
}

type testOrganizationIAMPermissionsOutput struct {
	Body dto.IAMTestPermissionsResponse
}

type apiCredentialPath struct {
	CredentialID string `path:"credential_id" doc:"Verself API credential ID"`
}

type apiCredentialsOutput struct {
	Body dto.IAMAPICredentials
}

type apiCredentialOutput struct {
	Body dto.IAMAPICredential
}

type createAPICredentialInput struct {
	Body dto.IAMCreateAPICredentialRequest
}

type createAPICredentialOutput struct {
	Body dto.IAMCreateAPICredentialResponse
}

type rollAPICredentialInput struct {
	CredentialID string `path:"credential_id" doc:"Verself API credential ID"`
	Body         dto.IAMRollAPICredentialRequest
}

type rollAPICredentialOutput struct {
	Body dto.IAMRollAPICredentialResponse
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
	if _, err := dto.ParseUint64(authIdentity.OrgID); err != nil {
		return identity.Principal{}, badRequest(ctx, "invalid-token-org", "token org_id must be an unsigned integer", err)
	}
	return identity.Principal{
		Subject:           authIdentity.Subject,
		OrgID:             authIdentity.OrgID,
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

func listMyOrganizations(svc *identity.Service) func(context.Context, *emptyInput) (*accessibleOrganizationsOutput, error) {
	return func(ctx context.Context, _ *emptyInput) (*accessibleOrganizationsOutput, error) {
		authIdentity, err := requireIdentity(ctx)
		if err != nil {
			return nil, err
		}
		orgIDs, err := authorizedRoleAssignmentOrgIDs(ctx, svc, authIdentity, permissionOrganizationRead)
		if err != nil {
			return nil, err
		}
		organizations, err := svc.AccessibleOrganizationsBySubject(ctx, authIdentity.Subject, orgIDs)
		if err != nil {
			return nil, identityError(ctx, err)
		}
		return &accessibleOrganizationsOutput{Body: organizationMetadataDTOs(organizations)}, nil
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
		return &membersOutput{Body: dto.IAMMembers{Members: memberDTOs(members)}}, nil
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
		return &inviteMemberOutput{Body: dto.IAMInviteMemberResponse{
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

func getOrganizationIAMPolicy(authzSvc *authz.Service) func(context.Context, *organizationIAMPolicyPath) (*organizationIAMPolicyOutput, error) {
	return func(ctx context.Context, input *organizationIAMPolicyPath) (*organizationIAMPolicyOutput, error) {
		if err := requirePathOrgMatchesToken(ctx, input.OrgID); err != nil {
			return nil, err
		}
		if authzSvc == nil {
			return nil, internalFailure(ctx, "iam-authz-unavailable", "authorization graph unavailable", authz.ErrUnavailable)
		}
		policy, err := authzSvc.GetOrganizationPolicy(ctx, input.OrgID)
		if err != nil {
			return nil, authzError(ctx, err)
		}
		return &organizationIAMPolicyOutput{Body: policyDTO(policy)}, nil
	}
}

func setOrganizationIAMPolicy(authzSvc *authz.Service) func(context.Context, *setOrganizationIAMPolicyInput) (*organizationIAMPolicyOutput, error) {
	return func(ctx context.Context, input *setOrganizationIAMPolicyInput) (*organizationIAMPolicyOutput, error) {
		if err := requirePathOrgMatchesToken(ctx, input.OrgID); err != nil {
			return nil, err
		}
		if authzSvc == nil {
			return nil, internalFailure(ctx, "iam-authz-unavailable", "authorization graph unavailable", authz.ErrUnavailable)
		}
		policy, err := authzSvc.SetOrganizationPolicy(ctx, input.OrgID, policyFromDTO(input.Body.Policy), "set-organization-iam-policy")
		if err != nil {
			return nil, authzError(ctx, err)
		}
		return &organizationIAMPolicyOutput{Body: policyDTO(policy)}, nil
	}
}

func testOrganizationIAMPermissions(authzSvc *authz.Service) func(context.Context, *testOrganizationIAMPermissionsInput) (*testOrganizationIAMPermissionsOutput, error) {
	return func(ctx context.Context, input *testOrganizationIAMPermissionsInput) (*testOrganizationIAMPermissionsOutput, error) {
		authIdentity, err := requireIdentity(ctx)
		if err != nil {
			return nil, err
		}
		if err := requirePathOrgMatchesToken(ctx, input.OrgID); err != nil {
			return nil, err
		}
		if authzSvc == nil {
			return nil, internalFailure(ctx, "iam-authz-unavailable", "authorization graph unavailable", authz.ErrUnavailable)
		}
		allowed, zedToken, err := authzSvc.TestOrganizationPermissions(ctx, input.OrgID, authzSubjectFromIdentity(authIdentity), input.Body.Permissions, "")
		if err != nil {
			return nil, authzError(ctx, err)
		}
		return &testOrganizationIAMPermissionsOutput{Body: dto.IAMTestPermissionsResponse{
			Permissions: allowed,
			ZedToken:    zedToken,
		}}, nil
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
		return &apiCredentialsOutput{Body: dto.IAMAPICredentials{Credentials: apiCredentialDTOs(credentials)}}, nil
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
		return &createAPICredentialOutput{Body: dto.IAMCreateAPICredentialResponse{
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
		return &rollAPICredentialOutput{Body: dto.IAMRollAPICredentialResponse{
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

func organizationDTO(org identity.Organization) dto.IAMOrganization {
	return dto.IAMOrganization{
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

func organizationMetadataDTOs(organizations []identity.OrganizationMetadata) []dto.IAMOrganizationMetadata {
	out := make([]dto.IAMOrganizationMetadata, 0, len(organizations))
	for _, organization := range organizations {
		out = append(out, dto.IAMOrganizationMetadata{
			OrgID:       orgID(organization.OrgID),
			DisplayName: organization.DisplayName,
			Slug:        organization.Slug,
		})
	}
	return out
}

func organizationProfileDTO(profile identity.OrganizationProfile) dto.IAMOrganizationProfile {
	return dto.IAMOrganizationProfile{
		OrgID:          orgID(profile.OrgID),
		DisplayName:    profile.DisplayName,
		Slug:           profile.Slug,
		State:          string(profile.State),
		Version:        profile.Version,
		UpdatedAt:      profile.UpdatedAt,
		RedirectedFrom: profile.RedirectedFrom,
	}
}

func memberDTOs(members []identity.Member) []dto.IAMMember {
	out := make([]dto.IAMMember, 0, len(members))
	for _, member := range members {
		out = append(out, memberDTO(member))
	}
	return out
}

func memberDTO(member identity.Member) dto.IAMMember {
	return dto.IAMMember{
		UserID:      member.UserID,
		Email:       member.Email,
		LoginName:   member.LoginName,
		DisplayName: member.DisplayName,
		State:       member.State,
		RoleKeys:    append([]string(nil), member.RoleKeys...),
	}
}

func memberCapabilitiesDocumentDTO(doc identity.MemberCapabilitiesDocument) dto.IAMMemberCapabilitiesDocument {
	return dto.IAMMemberCapabilitiesDocument{
		OrgID:       orgID(doc.OrgID),
		Version:     doc.Version,
		EnabledKeys: append([]string(nil), doc.EnabledKeys...),
		UpdatedAt:   doc.UpdatedAt,
		UpdatedBy:   doc.UpdatedBy,
	}
}

func memberCapabilitiesDTO(doc identity.MemberCapabilitiesDocument) dto.IAMMemberCapabilities {
	catalog := identity.DefaultCapabilities()
	dtoCatalog := make([]dto.IAMMemberCapability, 0, len(catalog))
	for _, capability := range catalog {
		dtoCatalog = append(dtoCatalog, dto.IAMMemberCapability{
			Key:            capability.Key,
			Label:          capability.Label,
			Description:    capability.Description,
			DefaultEnabled: capability.DefaultEnabled,
			Permissions:    append([]string(nil), capability.Permissions...),
		})
	}
	return dto.IAMMemberCapabilities{
		Document: memberCapabilitiesDocumentDTO(doc),
		Catalog:  dtoCatalog,
	}
}

func apiCredentialDTOs(credentials []identity.APICredential) []dto.IAMAPICredential {
	out := make([]dto.IAMAPICredential, 0, len(credentials))
	for _, credential := range credentials {
		out = append(out, apiCredentialDTO(credential))
	}
	return out
}

func apiCredentialDTO(credential identity.APICredential) dto.IAMAPICredential {
	return dto.IAMAPICredential{
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

func issuedMaterialDTO(material identity.APICredentialIssuedMaterial) dto.IAMAPICredentialIssuedMaterial {
	return dto.IAMAPICredentialIssuedMaterial{
		AuthMethod:   string(material.AuthMethod),
		ClientID:     material.ClientID,
		TokenURL:     material.TokenURL,
		KeyID:        material.KeyID,
		KeyContent:   material.KeyContent,
		ClientSecret: material.ClientSecret,
		Fingerprint:  material.Fingerprint,
	}
}

func requirePathOrgMatchesToken(ctx context.Context, orgID string) error {
	authIdentity, err := requireIdentity(ctx)
	if err != nil {
		return err
	}
	orgID = strings.TrimSpace(orgID)
	if _, err := dto.ParseUint64(orgID); err != nil {
		return badRequest(ctx, "invalid-path-org", "path org_id must be an unsigned integer", err)
	}
	if orgID != strings.TrimSpace(authIdentity.OrgID) {
		return forbidden(ctx, "organization-scope-mismatch", "path org_id must match the selected organization")
	}
	return nil
}

func authzSubjectFromIdentity(authIdentity *auth.Identity) authz.Subject {
	if authIdentity == nil {
		return authz.UserSubject("")
	}
	if credentialID, _ := authIdentity.Raw["verself:credential_id"].(string); strings.TrimSpace(credentialID) != "" {
		return authz.ServiceAccountSubject(authIdentity.Subject)
	}
	return authz.UserSubject(authIdentity.Subject)
}

func policyDTO(policy authz.Policy) dto.IAMPolicy {
	return dto.IAMPolicy{
		Resource: policy.Resource,
		Version:  policy.Version,
		Etag:     policy.Etag,
		Bindings: policyBindingDTOs(policy.Bindings),
		ZedToken: policy.ZedToken,
	}
}

func policyBindingDTOs(bindings []authz.PolicyBinding) []dto.IAMPolicyBinding {
	out := make([]dto.IAMPolicyBinding, 0, len(bindings))
	for _, binding := range bindings {
		out = append(out, dto.IAMPolicyBinding{
			Role:    binding.Role,
			Members: append([]string(nil), binding.Members...),
		})
	}
	return out
}

func policyFromDTO(policy dto.IAMPolicy) authz.Policy {
	return authz.Policy{
		Resource: policy.Resource,
		Version:  policy.Version,
		Etag:     policy.Etag,
		Bindings: policyBindingsFromDTO(policy.Bindings),
		ZedToken: policy.ZedToken,
	}
}

func policyBindingsFromDTO(bindings []dto.IAMPolicyBinding) []authz.PolicyBinding {
	out := make([]authz.PolicyBinding, 0, len(bindings))
	for _, binding := range bindings {
		out = append(out, authz.PolicyBinding{
			Role:    binding.Role,
			Members: append([]string(nil), binding.Members...),
		})
	}
	return out
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

func orgID(value string) dto.OrgID {
	parsed, err := dto.ParseUint64(value)
	if err != nil {
		return dto.Uint64(0)
	}
	return dto.Uint64(parsed)
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
		if _, err := dto.ParseUint64(orgID); err != nil {
			return nil, badRequest(ctx, "invalid-role-assignment-org", "role assignment org_id must be an unsigned integer", err)
		}
		if _, ok := seen[orgID]; ok {
			continue
		}
		seen[orgID] = struct{}{}
		orgIDs = append(orgIDs, orgID)
	}
	sort.Strings(orgIDs)
	if len(orgIDs) == 0 {
		return nil, forbidden(ctx, "missing-organization-role-assignments", "token has no organization role assignments")
	}
	return orgIDs, nil
}
