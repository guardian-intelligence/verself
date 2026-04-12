package api

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/forge-metal/apiwire"
	auth "github.com/forge-metal/auth-middleware"
	"github.com/forge-metal/identity-service/internal/identity"
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
		OperationID: "get-organization-policy",
		Method:      http.MethodGet,
		Path:        "/api/v1/organization/policy",
		Summary:     "Get organization policy document",
	}, operationPolicy{
		Permission:     permissionPolicyRead,
		Resource:       "organization_policy",
		Action:         "read",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "identity.organization.policy.read",
	}), getPolicy(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID:   "put-organization-policy",
		Method:        http.MethodPut,
		Path:          "/api/v1/organization/policy",
		Summary:       "Replace organization policy document",
		DefaultStatus: 200,
	}, operationPolicy{
		Permission:     permissionPolicyWrite,
		Resource:       "organization_policy",
		Action:         "write",
		OrgScope:       "token_org_id",
		RateLimitClass: "policy_mutation",
		Idempotency:    idempotencyHeaderKey,
		AuditEvent:     "identity.organization.policy.write",
		BodyLimitBytes: bodyLimitSmallJSON,
	}), putPolicy(svc))

	registerSecured(api, svc, secured(huma.Operation{
		OperationID: "list-organization-operations",
		Method:      http.MethodGet,
		Path:        "/api/v1/organization/operations",
		Summary:     "List service-declared operations available to policy documents",
	}, operationPolicy{
		Permission:     permissionOperationsRead,
		Resource:       "service_operation",
		Action:         "list",
		OrgScope:       "token_org_id",
		RateLimitClass: "read",
		AuditEvent:     "identity.organization.operations.list",
	}), listOperations(svc))

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

type policyOutput struct {
	Body apiwire.IdentityPolicyDocument
}

type putPolicyInput struct {
	Body apiwire.IdentityPutPolicyRequest
}

type operationsOutput struct {
	Body apiwire.IdentityOperations
}

type apiCredentialPath struct {
	CredentialID string `path:"credential_id" doc:"Forge Metal API credential ID"`
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
	CredentialID string `path:"credential_id" doc:"Forge Metal API credential ID"`
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
		member, err := svc.UpdateMemberRoles(ctx, principal, input.UserID, input.Body.RoleKeys)
		if err != nil {
			return nil, identityError(ctx, err)
		}
		return &memberOutput{Body: memberDTO(member)}, nil
	}
}

func getPolicy(svc *identity.Service) func(context.Context, *emptyInput) (*policyOutput, error) {
	return func(ctx context.Context, _ *emptyInput) (*policyOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		policy, err := svc.Policy(ctx, principal)
		if err != nil {
			return nil, identityError(ctx, err)
		}
		return &policyOutput{Body: policyDTO(policy)}, nil
	}
}

func putPolicy(svc *identity.Service) func(context.Context, *putPolicyInput) (*policyOutput, error) {
	return func(ctx context.Context, input *putPolicyInput) (*policyOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		policy, err := svc.PutPolicy(ctx, principal, policyFromPutDTO(input.Body))
		if err != nil {
			return nil, identityError(ctx, err)
		}
		return &policyOutput{Body: policyDTO(policy)}, nil
	}
}

func listOperations(svc *identity.Service) func(context.Context, *emptyInput) (*operationsOutput, error) {
	return func(ctx context.Context, _ *emptyInput) (*operationsOutput, error) {
		principal, err := principalFromContext(ctx)
		if err != nil {
			return nil, err
		}
		return &operationsOutput{Body: operationsDTO(svc.Operations(ctx, principal))}, nil
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
		OrgID:       orgID(org.OrgID),
		Name:        org.Name,
		Caller:      memberDTO(org.Caller),
		Policy:      policyDTO(org.Policy),
		Permissions: append([]string(nil), org.Permissions...),
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

func policyDTO(policy identity.PolicyDocument) apiwire.IdentityPolicyDocument {
	roles := make([]apiwire.IdentityPolicyRole, 0, len(policy.Roles))
	for _, role := range policy.Roles {
		roles = append(roles, apiwire.IdentityPolicyRole{
			RoleKey:     role.RoleKey,
			DisplayName: role.DisplayName,
			Permissions: append([]string(nil), role.Permissions...),
		})
	}
	return apiwire.IdentityPolicyDocument{
		OrgID:     orgID(policy.OrgID),
		Version:   policy.Version,
		Roles:     roles,
		UpdatedAt: policy.UpdatedAt,
		UpdatedBy: policy.UpdatedBy,
	}
}

func policyFromPutDTO(policy apiwire.IdentityPutPolicyRequest) identity.PolicyDocument {
	roles := make([]identity.PolicyRole, 0, len(policy.Roles))
	for _, role := range policy.Roles {
		roles = append(roles, identity.PolicyRole{
			RoleKey:     role.RoleKey,
			DisplayName: role.DisplayName,
			Permissions: append([]string(nil), role.Permissions...),
		})
	}
	return identity.PolicyDocument{
		Version: policy.Version,
		Roles:   roles,
	}
}

func operationsDTO(operations identity.Operations) apiwire.IdentityOperations {
	services := make([]apiwire.IdentityServiceOperations, 0, len(operations.Services))
	for _, service := range operations.Services {
		serviceDTO := apiwire.IdentityServiceOperations{
			Service:    service.Service,
			Operations: make([]apiwire.IdentityOperation, 0, len(service.Operations)),
		}
		for _, operation := range service.Operations {
			serviceDTO.Operations = append(serviceDTO.Operations, apiwire.IdentityOperation{
				OperationID: operation.OperationID,
				Permission:  operation.Permission,
				Resource:    operation.Resource,
				Action:      operation.Action,
				OrgScope:    operation.OrgScope,
			})
		}
		services = append(services, serviceDTO)
	}
	return apiwire.IdentityOperations{Services: services}
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
	credentialID, _ := authIdentity.Raw["forge_metal:credential_id"].(string)
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
