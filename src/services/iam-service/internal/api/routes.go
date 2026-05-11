package api

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/verself/domain-transfer-objects"
	"github.com/verself/iam-service/internal/authz"
	"github.com/verself/iam-service/internal/identity"
	auth "github.com/verself/service-runtime/auth"
	runtimeiam "github.com/verself/service-runtime/iam"
)

func RegisterRoutes(api huma.API, svc *identity.Service, authzSvc *authz.Service) {
	registerSecured(api, svc, authzSvc, secured(huma.Operation{
		OperationID: "get-organization",
		Method:      http.MethodGet,
		Path:        "/api/v1/organization",
		Summary:     "Get organization identity state",
	}, runtimeiam.OperationPolicy{
		Permission:     permissionOrganizationRead,
		Resource:       resourceOrganization,
		Action:         runtimeiam.ActionRead,
		OrgScope:       orgScopeTokenOrgID,
		RateLimitClass: rateLimitRead,
		AuditEvent:     auditOrganizationRead,
	}), getOrganization(svc))

	registerSecured(api, svc, authzSvc, secured(huma.Operation{
		OperationID: "list-my-organizations",
		Method:      http.MethodGet,
		Path:        "/api/v1/me/organizations",
		Summary:     "List organizations available to the caller",
	}, runtimeiam.OperationPolicy{
		Permission:     permissionOrganizationRead,
		Resource:       resourceOrganization,
		Action:         runtimeiam.ActionList,
		OrgScope:       orgScopeTokenRoleAssignmentOrgIDs,
		RateLimitClass: rateLimitRead,
		AuditEvent:     auditOrganizationMembershipList,
	}), listMyOrganizations(svc, authzSvc))

	registerSecured(api, svc, authzSvc, secured(huma.Operation{
		OperationID:   "patch-organization",
		Method:        http.MethodPatch,
		Path:          "/api/v1/organization",
		Summary:       "Update organization profile",
		DefaultStatus: http.StatusOK,
	}, runtimeiam.OperationPolicy{
		Permission:     permissionOrganizationWrite,
		Resource:       resourceOrganization,
		Action:         runtimeiam.ActionUpdate,
		OrgScope:       orgScopeTokenOrgID,
		RateLimitClass: rateLimitOrganizationMutation,
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     auditOrganizationUpdate,
		BodyLimitBytes: bodyLimitSmallJSON,
	}), updateOrganization(svc))

	registerSecured(api, svc, authzSvc, secured(huma.Operation{
		OperationID: "list-organization-members",
		Method:      http.MethodGet,
		Path:        "/api/v1/organization/members",
		Summary:     "List organization members",
	}, runtimeiam.OperationPolicy{
		Permission:     permissionMemberRead,
		Resource:       resourceOrganizationMember,
		Action:         runtimeiam.ActionList,
		OrgScope:       orgScopeTokenOrgID,
		RateLimitClass: rateLimitRead,
		AuditEvent:     auditOrganizationMemberList,
	}), listMembers(svc))

	registerSecured(api, svc, authzSvc, secured(huma.Operation{
		OperationID:   "invite-organization-member",
		Method:        http.MethodPost,
		Path:          "/api/v1/organization/members",
		Summary:       "Invite an organization member",
		DefaultStatus: 201,
	}, runtimeiam.OperationPolicy{
		Permission:     permissionMemberInvite,
		Resource:       resourceOrganizationMember,
		Action:         runtimeiam.ActionInvite,
		OrgScope:       orgScopeTokenOrgID,
		RateLimitClass: rateLimitMemberMutation,
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     auditOrganizationMemberInvite,
		BodyLimitBytes: bodyLimitSmallJSON,
	}), inviteMember(svc))

	registerSecured(api, svc, authzSvc, secured(huma.Operation{
		OperationID:   "update-organization-member-roles",
		Method:        http.MethodPut,
		Path:          "/api/v1/organization/members/{user_id}/roles",
		Summary:       "Update organization member roles",
		DefaultStatus: 200,
	}, runtimeiam.OperationPolicy{
		Permission:     permissionMemberRolesWrite,
		Resource:       resourceOrganizationMemberRoles,
		Action:         runtimeiam.ActionWrite,
		OrgScope:       orgScopeTokenOrgID,
		RateLimitClass: rateLimitMemberMutation,
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     auditOrganizationMemberRolesWrite,
		BodyLimitBytes: bodyLimitSmallJSON,
	}), updateMemberRoles(svc))

	registerSecured(api, svc, authzSvc, secured(huma.Operation{
		OperationID: "get-organization-member-capabilities",
		Method:      http.MethodGet,
		Path:        "/api/v1/organization/member-capabilities",
		Summary:     "Get organization member capabilities and the static capability catalog",
	}, runtimeiam.OperationPolicy{
		Permission:     permissionMemberCapabilitiesRead,
		Resource:       resourceOrganizationCapabilities,
		Action:         runtimeiam.ActionRead,
		OrgScope:       orgScopeTokenOrgID,
		RateLimitClass: rateLimitRead,
		AuditEvent:     auditOrganizationCapabilitiesRead,
	}), getMemberCapabilities(svc))

	registerSecured(api, svc, authzSvc, secured(huma.Operation{
		OperationID:   "put-organization-member-capabilities",
		Method:        http.MethodPut,
		Path:          "/api/v1/organization/member-capabilities",
		Summary:       "Replace the organization's enabled member capability set",
		DefaultStatus: 200,
	}, runtimeiam.OperationPolicy{
		Permission:     permissionMemberCapabilitiesWrite,
		Resource:       resourceOrganizationCapabilities,
		Action:         runtimeiam.ActionWrite,
		OrgScope:       orgScopeTokenOrgID,
		RateLimitClass: rateLimitMemberCapabilitiesMutation,
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     auditOrganizationCapabilitiesWrite,
		BodyLimitBytes: bodyLimitSmallJSON,
	}), putMemberCapabilities(svc))

	registerSecured(api, svc, authzSvc, secured(huma.Operation{
		OperationID: "get-organization-iam-policy",
		Method:      http.MethodGet,
		Path:        "/api/v1/organizations/{org_id}/iamPolicy",
		Summary:     "Get organization IAM policy",
	}, runtimeiam.OperationPolicy{
		Permission:     permissionIAMPolicyRead,
		Resource:       resourceOrganizationIAMPolicy,
		Action:         runtimeiam.ActionRead,
		OrgScope:       orgScopeTokenOrgID,
		RateLimitClass: rateLimitRead,
		AuditEvent:     auditOrganizationPolicyRead,
	}), getOrganizationIAMPolicy(authzSvc))

	registerSecured(api, svc, authzSvc, secured(huma.Operation{
		OperationID:   "set-organization-iam-policy",
		Method:        http.MethodPut,
		Path:          "/api/v1/organizations/{org_id}/iamPolicy",
		Summary:       "Replace organization IAM policy",
		DefaultStatus: http.StatusOK,
	}, runtimeiam.OperationPolicy{
		Permission:     permissionIAMPolicySet,
		Resource:       resourceOrganizationIAMPolicy,
		Action:         runtimeiam.ActionWrite,
		OrgScope:       orgScopeTokenOrgID,
		RateLimitClass: rateLimitIAMPolicyMutation,
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     auditOrganizationPolicyWrite,
		BodyLimitBytes: bodyLimitSmallJSON,
	}), setOrganizationIAMPolicy(authzSvc))

	registerSecured(api, svc, authzSvc, secured(huma.Operation{
		OperationID:   "test-organization-iam-permissions",
		Method:        http.MethodPost,
		Path:          "/api/v1/organizations/{org_id}/iamPolicy:testPermissions",
		Summary:       "Test caller permissions on an organization",
		DefaultStatus: http.StatusOK,
	}, runtimeiam.OperationPolicy{
		Permission:     permissionIAMPolicyTest,
		Resource:       resourceOrganizationIAMPolicy,
		Action:         runtimeiam.ActionTest,
		OrgScope:       orgScopeTokenOrgID,
		RateLimitClass: rateLimitRead,
		AuditEvent:     auditOrganizationPolicyTest,
		BodyLimitBytes: bodyLimitSmallJSON,
	}), testOrganizationIAMPermissions(authzSvc))

	registerSecured(api, svc, authzSvc, secured(huma.Operation{
		OperationID: "list-service-accounts",
		Method:      http.MethodGet,
		Path:        "/api/v1/organization/service-accounts",
		Summary:     "List organization service accounts",
	}, runtimeiam.OperationPolicy{
		Permission:     permissionAPICredentialsRead,
		Resource:       resourceServiceAccount,
		Action:         runtimeiam.ActionList,
		OrgScope:       orgScopeTokenOrgID,
		RateLimitClass: rateLimitRead,
		AuditEvent:     auditServiceAccountList,
	}), listServiceAccounts(svc))

	registerSecured(api, svc, authzSvc, secured(huma.Operation{
		OperationID: "get-service-account",
		Method:      http.MethodGet,
		Path:        "/api/v1/organization/service-accounts/{service_account_id}",
		Summary:     "Get service account metadata",
	}, runtimeiam.OperationPolicy{
		Permission:     permissionAPICredentialsRead,
		Resource:       resourceServiceAccount,
		Action:         runtimeiam.ActionRead,
		OrgScope:       orgScopeTokenOrgID,
		RateLimitClass: rateLimitRead,
		AuditEvent:     auditServiceAccountRead,
	}), getServiceAccount(svc))

	registerSecured(api, svc, authzSvc, secured(huma.Operation{
		OperationID:   "disable-service-account",
		Method:        http.MethodPost,
		Path:          "/api/v1/organization/service-accounts/{service_account_id}/disable",
		Summary:       "Disable a service account and revoke its API credentials",
		DefaultStatus: 200,
	}, runtimeiam.OperationPolicy{
		Permission:     permissionAPICredentialsRevoke,
		Resource:       resourceServiceAccount,
		Action:         runtimeiam.ActionDisable,
		OrgScope:       orgScopeTokenOrgID,
		RateLimitClass: rateLimitAPICredentialMutation,
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     auditServiceAccountDisable,
		BodyLimitBytes: bodyLimitNoBody,
	}), disableServiceAccount(svc))

	registerSecured(api, svc, authzSvc, secured(huma.Operation{
		OperationID: "list-api-credentials",
		Method:      http.MethodGet,
		Path:        "/api/v1/organization/api-credentials",
		Summary:     "List organization API credentials",
	}, runtimeiam.OperationPolicy{
		Permission:     permissionAPICredentialsRead,
		Resource:       resourceAPICredential,
		Action:         runtimeiam.ActionList,
		OrgScope:       orgScopeTokenOrgID,
		RateLimitClass: rateLimitRead,
		AuditEvent:     auditAPICredentialList,
	}), listAPICredentials(svc))

	registerSecured(api, svc, authzSvc, secured(huma.Operation{
		OperationID: "get-api-credential",
		Method:      http.MethodGet,
		Path:        "/api/v1/organization/api-credentials/{credential_id}",
		Summary:     "Get API credential metadata",
	}, runtimeiam.OperationPolicy{
		Permission:     permissionAPICredentialsRead,
		Resource:       resourceAPICredential,
		Action:         runtimeiam.ActionRead,
		OrgScope:       orgScopeTokenOrgID,
		RateLimitClass: rateLimitRead,
		AuditEvent:     auditAPICredentialRead,
	}), getAPICredential(svc))

	registerSecured(api, svc, authzSvc, secured(huma.Operation{
		OperationID:   "create-api-credential",
		Method:        http.MethodPost,
		Path:          "/api/v1/organization/api-credentials",
		Summary:       "Create an API credential",
		DefaultStatus: 201,
	}, runtimeiam.OperationPolicy{
		Permission:     permissionAPICredentialsCreate,
		Resource:       resourceAPICredential,
		Action:         runtimeiam.ActionCreate,
		OrgScope:       orgScopeTokenOrgID,
		RateLimitClass: rateLimitAPICredentialMutation,
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     auditAPICredentialCreate,
		BodyLimitBytes: bodyLimitSmallJSON,
	}), createAPICredential(svc))

	registerSecured(api, svc, authzSvc, secured(huma.Operation{
		OperationID:   "roll-api-credential",
		Method:        http.MethodPost,
		Path:          "/api/v1/organization/api-credentials/{credential_id}/roll",
		Summary:       "Roll API credential secret material",
		DefaultStatus: 200,
	}, runtimeiam.OperationPolicy{
		Permission:     permissionAPICredentialsRoll,
		Resource:       resourceAPICredential,
		Action:         runtimeiam.ActionRoll,
		OrgScope:       orgScopeTokenOrgID,
		RateLimitClass: rateLimitAPICredentialMutation,
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     auditAPICredentialRoll,
		BodyLimitBytes: bodyLimitSmallJSON,
	}), rollAPICredential(svc))

	registerSecured(api, svc, authzSvc, secured(huma.Operation{
		OperationID:   "revoke-api-credential",
		Method:        http.MethodDelete,
		Path:          "/api/v1/organization/api-credentials/{credential_id}",
		Summary:       "Revoke an API credential",
		DefaultStatus: 200,
	}, runtimeiam.OperationPolicy{
		Permission:     permissionAPICredentialsRevoke,
		Resource:       resourceAPICredential,
		Action:         runtimeiam.ActionRevoke,
		OrgScope:       orgScopeTokenOrgID,
		RateLimitClass: rateLimitAPICredentialMutation,
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     auditAPICredentialRevoke,
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

type serviceAccountPath struct {
	ServiceAccountID string `path:"service_account_id" doc:"Verself service account ID"`
}

type serviceAccountsOutput struct {
	Body dto.IAMServiceAccounts
}

type serviceAccountOutput struct {
	Body dto.IAMServiceAccount
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
	subject := authIdentity.Subject
	subjectKind := identity.AuthorizationSubjectKindUser
	if serviceAccountID, _ := authIdentity.Raw["verself:service_account_id"].(string); strings.TrimSpace(serviceAccountID) != "" {
		subject = strings.TrimSpace(serviceAccountID)
		subjectKind = identity.AuthorizationSubjectKindServiceAccount
	}
	return identity.Principal{
		Subject:     subject,
		SubjectKind: subjectKind,
		OrgID:       authIdentity.OrgID,
		Roles:       identityRolesForCurrentOrg(authIdentity),
		Email:       authIdentity.Email,
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

func listMyOrganizations(svc *identity.Service, authzSvc *authz.Service) func(context.Context, *emptyInput) (*accessibleOrganizationsOutput, error) {
	return func(ctx context.Context, _ *emptyInput) (*accessibleOrganizationsOutput, error) {
		authIdentity, err := requireIdentity(ctx)
		if err != nil {
			return nil, err
		}
		orgIDs, err := authorizedRoleAssignmentOrgIDs(ctx, authzSvc, authIdentity, permissionOrganizationRead)
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

func listServiceAccounts(svc *identity.Service) func(context.Context, *emptyInput) (*serviceAccountsOutput, error) {
	return func(ctx context.Context, _ *emptyInput) (*serviceAccountsOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		accounts, err := svc.ListServiceAccounts(ctx, principal)
		if err != nil {
			return nil, identityError(ctx, err)
		}
		return &serviceAccountsOutput{Body: dto.IAMServiceAccounts{ServiceAccounts: serviceAccountDTOs(accounts)}}, nil
	}
}

func getServiceAccount(svc *identity.Service) func(context.Context, *serviceAccountPath) (*serviceAccountOutput, error) {
	return func(ctx context.Context, input *serviceAccountPath) (*serviceAccountOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		account, err := svc.GetServiceAccount(ctx, principal, input.ServiceAccountID)
		if err != nil {
			return nil, identityError(ctx, err)
		}
		return &serviceAccountOutput{Body: serviceAccountDTO(account)}, nil
	}
}

func disableServiceAccount(svc *identity.Service) func(context.Context, *serviceAccountPath) (*serviceAccountOutput, error) {
	return func(ctx context.Context, input *serviceAccountPath) (*serviceAccountOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		result, err := svc.DisableServiceAccount(ctx, principal, input.ServiceAccountID)
		if err != nil {
			return nil, identityError(ctx, err)
		}
		return &serviceAccountOutput{Body: serviceAccountDTO(result.ServiceAccount)}, nil
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
			ServiceAccountID: input.Body.ServiceAccountID,
			DisplayName:      input.Body.DisplayName,
			Description:      input.Body.Description,
			AuthMethod:       identity.APICredentialAuthMethod(input.Body.AuthMethod),
			Permissions:      input.Body.Permissions,
			ExpiresAt:        input.Body.ExpiresAt,
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

func serviceAccountDTOs(accounts []identity.ServiceAccount) []dto.IAMServiceAccount {
	out := make([]dto.IAMServiceAccount, 0, len(accounts))
	for _, account := range accounts {
		out = append(out, serviceAccountDTO(account))
	}
	return out
}

func serviceAccountDTO(account identity.ServiceAccount) dto.IAMServiceAccount {
	return dto.IAMServiceAccount{
		ServiceAccountID: account.ServiceAccountID,
		OrgID:            orgID(account.OrgID),
		SubjectID:        account.SubjectID,
		ClientID:         account.ClientID,
		DisplayName:      account.DisplayName,
		Description:      account.Description,
		Status:           string(account.Status),
		Permissions:      append([]string(nil), account.Permissions...),
		CreatedAt:        account.CreatedAt,
		CreatedBy:        account.CreatedBy,
		UpdatedAt:        account.UpdatedAt,
		DisabledAt:       account.DisabledAt,
		DisabledBy:       account.DisabledBy,
		LastUsedAt:       account.LastUsedAt,
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
		ServiceAccountID:     credential.ServiceAccountID,
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
