package identity

const (
	PermissionOrganizationRead          = "identity:organization:read"
	PermissionMemberRead                = "identity:member:read"
	PermissionMemberInvite              = "identity:member:invite"
	PermissionMemberRolesWrite          = "identity:member:roles:write"
	PermissionMemberCapabilitiesRead    = "identity:member_capabilities:read"
	PermissionMemberCapabilitiesWrite   = "identity:member_capabilities:write"
	PermissionOperationsRead            = "identity:operations:read"
	PermissionAPICredentialsRead        = "identity:api_credentials:read"
	PermissionAPICredentialsCreate      = "identity:api_credentials:create"
	PermissionAPICredentialsRoll        = "identity:api_credentials:roll"
	PermissionAPICredentialsRevoke      = "identity:api_credentials:revoke"
	PermissionSandboxRepoRead           = "sandbox:repo:read"
	PermissionSandboxRepoWrite          = "sandbox:repo:write"
	PermissionSandboxWebhookRead        = "sandbox:webhook_endpoint:read"
	PermissionSandboxWebhookWrite       = "sandbox:webhook_endpoint:write"
	PermissionSandboxExecutionSubmit    = "sandbox:execution:submit"
	PermissionSandboxExecutionRead      = "sandbox:execution:read"
	PermissionSandboxLogsRead           = "sandbox:logs:read"
	PermissionBillingRead               = "billing:read"
	PermissionBillingCheckout           = "billing:checkout"
)

// member_eligible: true marks a permission as one that can ever appear in a
// member-role caller's effective set, either as a baseline or via a Capability
// bundle. The init() check in capabilities.go enforces that no permission
// outside this set leaks into the member resolution path.
var defaultOperations = Operations{
	Services: []ServiceOperations{
		{
			Service: "identity-service",
			Operations: []Operation{
				{OperationID: "get-organization", Permission: PermissionOrganizationRead, Resource: "organization", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "list-organization-members", Permission: PermissionMemberRead, Resource: "organization_member", Action: "list", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "invite-organization-member", Permission: PermissionMemberInvite, Resource: "organization_member", Action: "invite", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "update-organization-member-roles", Permission: PermissionMemberRolesWrite, Resource: "organization_member_roles", Action: "write", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "get-organization-member-capabilities", Permission: PermissionMemberCapabilitiesRead, Resource: "organization_member_capabilities", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "put-organization-member-capabilities", Permission: PermissionMemberCapabilitiesWrite, Resource: "organization_member_capabilities", Action: "write", OrgScope: "token_org_id"},
				{OperationID: "list-organization-operations", Permission: PermissionOperationsRead, Resource: "service_operation", Action: "list", OrgScope: "token_org_id", MemberEligible: true},
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
				{OperationID: "import-repo", Permission: PermissionSandboxRepoWrite, Resource: "repo", Action: "import", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "list-repos", Permission: PermissionSandboxRepoRead, Resource: "repo", Action: "list", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "get-repo", Permission: PermissionSandboxRepoRead, Resource: "repo", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "rescan-repo", Permission: PermissionSandboxRepoWrite, Resource: "repo", Action: "rescan", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "create-webhook-endpoint", Permission: PermissionSandboxWebhookWrite, Resource: "webhook_endpoint", Action: "create", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "list-webhook-endpoints", Permission: PermissionSandboxWebhookRead, Resource: "webhook_endpoint", Action: "list", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "rotate-webhook-endpoint-secret", Permission: PermissionSandboxWebhookWrite, Resource: "webhook_endpoint_secret", Action: "rotate", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "delete-webhook-endpoint", Permission: PermissionSandboxWebhookWrite, Resource: "webhook_endpoint", Action: "delete", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "submit-execution", Permission: PermissionSandboxExecutionSubmit, Resource: "execution", Action: "submit", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "get-execution", Permission: PermissionSandboxExecutionRead, Resource: "execution", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "get-execution-logs", Permission: PermissionSandboxLogsRead, Resource: "execution_logs", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "get-billing-balance", Permission: PermissionBillingRead, Resource: "billing_balance", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "list-billing-subscriptions", Permission: PermissionBillingRead, Resource: "billing_subscription", Action: "list", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "list-billing-grants", Permission: PermissionBillingRead, Resource: "billing_grant", Action: "list", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "get-billing-statement", Permission: PermissionBillingRead, Resource: "billing_statement", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "create-billing-checkout", Permission: PermissionBillingCheckout, Resource: "billing_checkout", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "create-billing-subscription", Permission: PermissionBillingCheckout, Resource: "billing_subscription_checkout", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "create-billing-portal", Permission: PermissionBillingCheckout, Resource: "billing_portal", Action: "create", OrgScope: "token_org_id"},
			},
		},
	},
}

func KnownRoleKeys() map[string]struct{} {
	return map[string]struct{}{
		RoleOwner:  {},
		RoleAdmin:  {},
		RoleMember: {},
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

func KnownPermissions() map[string]struct{} {
	known := map[string]struct{}{}
	for _, service := range defaultOperations.Services {
		for _, operation := range service.Operations {
			known[operation.Permission] = struct{}{}
		}
	}
	return known
}

func memberEligiblePermissions() map[string]struct{} {
	eligible := map[string]struct{}{}
	for _, service := range defaultOperations.Services {
		for _, operation := range service.Operations {
			if operation.MemberEligible {
				eligible[operation.Permission] = struct{}{}
			}
		}
	}
	return eligible
}

// PermissionsForRoles resolves a role-keyed principal's effective product
// permissions against the org's enabled member capabilities. owner and admin
// always receive the full known permission set; member receives the union of
// baseline member permissions and the permissions bundled into each enabled
// capability key.
func PermissionsForRoles(capabilities MemberCapabilitiesDocument, roleKeys []string) []string {
	roleSet := map[string]struct{}{}
	for _, role := range roleKeys {
		roleSet[role] = struct{}{}
	}
	if _, ok := roleSet[RoleOwner]; ok {
		return sortedKeys(KnownPermissions())
	}
	if _, ok := roleSet[RoleAdmin]; ok {
		return sortedKeys(KnownPermissions())
	}
	if _, ok := roleSet[RoleMember]; ok {
		return ResolveMemberPermissions(capabilities.EnabledKeys)
	}
	return nil
}
