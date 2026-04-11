package api

import (
	"context"
	"net/http"

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
		Subject: authIdentity.Subject,
		OrgID:   authIdentity.OrgID,
		Roles:   identityRolesForCurrentOrg(authIdentity),
		Email:   authIdentity.Email,
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

func orgID(value string) apiwire.OrgID {
	parsed, err := apiwire.ParseUint64(value)
	if err != nil {
		return apiwire.Uint64(0)
	}
	return apiwire.Uint64(parsed)
}
