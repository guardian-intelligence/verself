package identity

import "sort"

const (
	PermissionOrganizationRead       = "identity:organization:read"
	PermissionMemberRead             = "identity:member:read"
	PermissionMemberInvite           = "identity:member:invite"
	PermissionMemberRolesWrite       = "identity:member:roles:write"
	PermissionPolicyRead             = "identity:policy:read"
	PermissionPolicyWrite            = "identity:policy:write"
	PermissionOperationsRead         = "identity:operations:read"
	PermissionAPICredentialsRead     = "identity:api_credentials:read"
	PermissionAPICredentialsCreate   = "identity:api_credentials:create"
	PermissionAPICredentialsRoll     = "identity:api_credentials:roll"
	PermissionAPICredentialsRevoke   = "identity:api_credentials:revoke"
	PermissionSandboxRepoRead        = "sandbox:repo:read"
	PermissionSandboxRepoWrite       = "sandbox:repo:write"
	PermissionSandboxWebhookRead     = "sandbox:webhook_endpoint:read"
	PermissionSandboxWebhookWrite    = "sandbox:webhook_endpoint:write"
	PermissionSandboxExecutionSubmit = "sandbox:execution:submit"
	PermissionSandboxExecutionRead   = "sandbox:execution:read"
	PermissionSandboxLogsRead        = "sandbox:logs:read"
	PermissionBillingRead            = "billing:read"
	PermissionBillingCheckout        = "billing:checkout"
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
				{OperationID: "list-api-credentials", Permission: PermissionAPICredentialsRead, Resource: "api_credential", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "get-api-credential", Permission: PermissionAPICredentialsRead, Resource: "api_credential", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "create-api-credential", Permission: PermissionAPICredentialsCreate, Resource: "api_credential", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "roll-api-credential", Permission: PermissionAPICredentialsRoll, Resource: "api_credential", Action: "roll", OrgScope: "token_org_id"},
				{OperationID: "revoke-api-credential", Permission: PermissionAPICredentialsRevoke, Resource: "api_credential", Action: "revoke", OrgScope: "token_org_id"},
			},
		},
		{
			Service: "sandbox-rental-service",
			Operations: []Operation{
				{OperationID: "import-repo", Permission: PermissionSandboxRepoWrite, Resource: "repo", Action: "import", OrgScope: "token_org_id"},
				{OperationID: "list-repos", Permission: PermissionSandboxRepoRead, Resource: "repo", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "get-repo", Permission: PermissionSandboxRepoRead, Resource: "repo", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "rescan-repo", Permission: PermissionSandboxRepoWrite, Resource: "repo", Action: "rescan", OrgScope: "token_org_id"},
				{OperationID: "create-webhook-endpoint", Permission: PermissionSandboxWebhookWrite, Resource: "webhook_endpoint", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "list-webhook-endpoints", Permission: PermissionSandboxWebhookRead, Resource: "webhook_endpoint", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "rotate-webhook-endpoint-secret", Permission: PermissionSandboxWebhookWrite, Resource: "webhook_endpoint_secret", Action: "rotate", OrgScope: "token_org_id"},
				{OperationID: "delete-webhook-endpoint", Permission: PermissionSandboxWebhookWrite, Resource: "webhook_endpoint", Action: "delete", OrgScope: "token_org_id"},
				{OperationID: "submit-execution", Permission: PermissionSandboxExecutionSubmit, Resource: "execution", Action: "submit", OrgScope: "token_org_id"},
				{OperationID: "get-execution", Permission: PermissionSandboxExecutionRead, Resource: "execution", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "get-execution-logs", Permission: PermissionSandboxLogsRead, Resource: "execution_logs", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "get-billing-balance", Permission: PermissionBillingRead, Resource: "billing_balance", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "list-billing-subscriptions", Permission: PermissionBillingRead, Resource: "billing_subscription", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "list-billing-grants", Permission: PermissionBillingRead, Resource: "billing_grant", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "get-billing-statement", Permission: PermissionBillingRead, Resource: "billing_statement", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "create-billing-checkout", Permission: PermissionBillingCheckout, Resource: "billing_checkout", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "create-billing-subscription", Permission: PermissionBillingCheckout, Resource: "billing_subscription_checkout", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "create-billing-portal", Permission: PermissionBillingCheckout, Resource: "billing_portal", Action: "create", OrgScope: "token_org_id"},
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
			PermissionAPICredentialsRead,
			PermissionAPICredentialsCreate,
			PermissionAPICredentialsRoll,
			PermissionAPICredentialsRevoke,
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
