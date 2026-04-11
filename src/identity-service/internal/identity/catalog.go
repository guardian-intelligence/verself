package identity

import "sort"

const (
	PermissionOrganizationRead = "identity:organization:read"
	PermissionMemberRead       = "identity:member:read"
	PermissionMemberInvite     = "identity:member:invite"
	PermissionMemberRolesWrite = "identity:member:roles:write"
	PermissionPolicyRead       = "identity:policy:read"
	PermissionPolicyWrite      = "identity:policy:write"
	PermissionOperationsRead   = "identity:operations:read"
)

var defaultOperations = Operations{
	Services: []ServiceOperations{
		{
			Service: "identity-service",
			Operations: []Operation{
				{OperationID: "get-organization", Permission: PermissionOrganizationRead, Resource: "organization", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "list-organization-members", Permission: PermissionMemberRead, Resource: "organization_member", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "invite-organization-member", Permission: PermissionMemberInvite, Resource: "organization_member", Action: "invite", OrgScope: "token_org_id"},
				{OperationID: "update-organization-member-roles", Permission: PermissionMemberRolesWrite, Resource: "organization_member_roles", Action: "write", OrgScope: "token_org_id"},
				{OperationID: "get-organization-policy", Permission: PermissionPolicyRead, Resource: "organization_policy", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "put-organization-policy", Permission: PermissionPolicyWrite, Resource: "organization_policy", Action: "write", OrgScope: "token_org_id"},
				{OperationID: "list-organization-operations", Permission: PermissionOperationsRead, Resource: "service_operation", Action: "list", OrgScope: "token_org_id"},
			},
		},
	},
}

var defaultRoleBundles = []PolicyRole{
	{
		RoleKey:     RoleOrgAdmin,
		DisplayName: "Organization Admin",
		Permissions: []string{
			PermissionOrganizationRead,
			PermissionMemberRead,
			PermissionMemberInvite,
			PermissionMemberRolesWrite,
			PermissionPolicyRead,
			PermissionPolicyWrite,
			PermissionOperationsRead,
		},
	},
	{
		RoleKey:     RoleOrgMember,
		DisplayName: "Organization Member",
		Permissions: []string{
			PermissionOrganizationRead,
			PermissionMemberRead,
			PermissionPolicyRead,
			PermissionOperationsRead,
		},
	},
}

func KnownRoleKeys() map[string]struct{} {
	known := make(map[string]struct{}, len(defaultRoleBundles))
	for _, role := range defaultRoleBundles {
		known[role.RoleKey] = struct{}{}
	}
	return known
}

func ReservedRoleKeys() map[string]struct{} {
	return map[string]struct{}{
		RoleForgeOrgOwner: {},
	}
}

func DefaultOperations() Operations {
	// This catalog is identity-service-only until policy documents become service-scoped bundles.
	out := Operations{Services: make([]ServiceOperations, 0, len(defaultOperations.Services))}
	for _, service := range defaultOperations.Services {
		copied := ServiceOperations{
			Service:    service.Service,
			Operations: append([]Operation(nil), service.Operations...),
		}
		out.Services = append(out.Services, copied)
	}
	return out
}

func DefaultPolicy(orgID, actor string) PolicyDocument {
	return PolicyDocument{
		OrgID:     orgID,
		Version:   0,
		Roles:     clonePolicyRoles(defaultRoleBundles),
		UpdatedBy: actor,
	}
}

func KnownPermissions() map[string]struct{} {
	known := map[string]struct{}{}
	for _, service := range defaultOperations.Services {
		for _, operation := range service.Operations {
			known[operation.Permission] = struct{}{}
		}
	}
	return known
}

func PermissionsForRoles(policy PolicyDocument, roleKeys []string) []string {
	roleSet := map[string]struct{}{}
	for _, role := range roleKeys {
		roleSet[role] = struct{}{}
	}
	if _, ok := roleSet[RoleForgeOrgOwner]; ok {
		return sortedKeys(KnownPermissions())
	}
	permissionSet := map[string]struct{}{}
	for _, role := range policy.Roles {
		if _, ok := roleSet[role.RoleKey]; !ok {
			continue
		}
		for _, permission := range role.Permissions {
			permissionSet[permission] = struct{}{}
		}
	}
	permissions := make([]string, 0, len(permissionSet))
	for permission := range permissionSet {
		permissions = append(permissions, permission)
	}
	sort.Strings(permissions)
	return permissions
}

func clonePolicyRoles(roles []PolicyRole) []PolicyRole {
	out := make([]PolicyRole, 0, len(roles))
	for _, role := range roles {
		role.Permissions = append([]string(nil), role.Permissions...)
		out = append(out, role)
	}
	return out
}
