package identity

const (
	PermissionOrganizationRead              = "iam:organization:read"
	PermissionOrganizationWrite             = "iam:organization:write"
	PermissionMemberRead                    = "iam:member:read"
	PermissionMemberInvite                  = "iam:member:invite"
	PermissionMemberRolesWrite              = "iam:member:roles:write"
	PermissionMemberCapabilitiesRead        = "iam:member_capabilities:read"
	PermissionMemberCapabilitiesWrite       = "iam:member_capabilities:write"
	PermissionIAMPolicyRead                 = "iam:policy:read"
	PermissionIAMPolicySet                  = "iam:policy:set"
	PermissionIAMPolicyTest                 = "iam:policy:test"
	PermissionAPICredentialsRead            = "iam:api_credentials:read"
	PermissionAPICredentialsCreate          = "iam:api_credentials:create"
	PermissionAPICredentialsRoll            = "iam:api_credentials:roll"
	PermissionAPICredentialsRevoke          = "iam:api_credentials:revoke"
	PermissionSandboxGitHubRead             = "sandbox:github_installation:read"
	PermissionSandboxGitHubWrite            = "sandbox:github_installation:write"
	PermissionSandboxExecutionRead          = "sandbox:execution:read"
	PermissionSandboxExecutionScheduleRead  = "sandbox:execution_schedule:read"
	PermissionSandboxExecutionScheduleWrite = "sandbox:execution_schedule:write"
	PermissionSandboxLogsRead               = "sandbox:logs:read"
	PermissionSandboxAnalyticsRead          = "sandbox:analytics:read"
	PermissionSandboxStickyDiskRead         = "sandbox:sticky_disk:read"
	PermissionSandboxStickyDiskWrite        = "sandbox:sticky_disk:write"
	PermissionBillingRead                   = "billing:read"
	PermissionBillingCheckout               = "billing:checkout"
	PermissionProjectRead                   = "projects:project:read"
	PermissionProjectWrite                  = "projects:project:write"
	PermissionProjectEnvironmentRead        = "projects:environment:read"
	PermissionProjectEnvironmentWrite       = "projects:environment:write"
	PermissionProjectEventRead              = "projects:event:read"
	PermissionProjectResolve                = "projects:resolve"
	PermissionSourceRepoRead                = "source:repo:read"
	PermissionSourceRepoWrite               = "source:repo:write"
	PermissionSourceCheckoutWrite           = "source:checkout:write"
	PermissionSourceIntegrationWrite        = "source:integration:write"
	PermissionSourceGitCredentialWrite      = "source:git_credential:write"
	PermissionSourceWorkflowRead            = "source:workflow:read"
	PermissionSourceWorkflowWrite           = "source:workflow:write"
	PermissionSecretWrite                   = "secrets:secret:write"
	PermissionSecretRead                    = "secrets:secret:read"
	PermissionSecretList                    = "secrets:secret:list"
	PermissionSecretDelete                  = "secrets:secret:delete"
	PermissionVariableWrite                 = "secrets:variable:write"
	PermissionVariableRead                  = "secrets:variable:read"
	PermissionVariableList                  = "secrets:variable:list"
	PermissionVariableDelete                = "secrets:variable:delete"
	PermissionCredentialCreate              = "secrets:credential:create"
	PermissionCredentialRead                = "secrets:credential:read"
	PermissionCredentialList                = "secrets:credential:list"
	PermissionCredentialRoll                = "secrets:credential:roll"
	PermissionCredentialRevoke              = "secrets:credential:revoke"
	PermissionTransitKeyCreate              = "secrets:transit_key:create"
	PermissionTransitKeyRotate              = "secrets:transit_key:rotate"
	PermissionTransitEncrypt                = "secrets:transit:encrypt"
	PermissionTransitDecrypt                = "secrets:transit:decrypt"
	PermissionTransitSign                   = "secrets:transit:sign"
	PermissionTransitVerify                 = "secrets:transit:verify"
	PermissionGovernanceAuditRead           = "governance:audit:read"
	PermissionGovernanceExportRead          = "governance:export:read"
	PermissionGovernanceExportCreate        = "governance:export:create"
)

var openBaoRolesByPermission = map[string]string{
	PermissionSecretWrite:      "secrets-direct-put-secret",
	PermissionSecretRead:       "secrets-direct-read-secret",
	PermissionSecretList:       "secrets-direct-list-secrets",
	PermissionSecretDelete:     "secrets-direct-delete-secret",
	PermissionVariableWrite:    "secrets-direct-put-variable",
	PermissionVariableRead:     "secrets-direct-read-variable",
	PermissionVariableList:     "secrets-direct-list-variables",
	PermissionVariableDelete:   "secrets-direct-delete-variable",
	PermissionCredentialCreate: "secrets-direct-create-credential",
	PermissionCredentialRead:   "secrets-direct-read-credential",
	PermissionCredentialList:   "secrets-direct-list-credentials",
	PermissionCredentialRoll:   "secrets-direct-roll-credential",
	PermissionCredentialRevoke: "secrets-direct-revoke-credential",
	PermissionTransitKeyCreate: "secrets-direct-create-transit-key",
	PermissionTransitKeyRotate: "secrets-direct-rotate-transit-key",
	PermissionTransitEncrypt:   "secrets-direct-encrypt-with-transit-key",
	PermissionTransitDecrypt:   "secrets-direct-decrypt-with-transit-key",
	PermissionTransitSign:      "secrets-direct-sign-with-transit-key",
	PermissionTransitVerify:    "secrets-direct-verify-with-transit-key",
}

// member_eligible: true marks a permission as one that can ever appear in a
// member-role caller's effective set, either as a baseline or via a Capability
// bundle. The init() check in capabilities.go enforces that no permission
// outside this set leaks into the member resolution path.
var defaultOperations = Operations{
	Services: []ServiceOperations{
		{
			Service: "iam-service",
			Operations: []Operation{
				{OperationID: "get-organization", Permission: PermissionOrganizationRead, Resource: "organization", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "list-my-organizations", Permission: PermissionOrganizationRead, Resource: "organization", Action: "list", OrgScope: "token_role_assignment_org_ids", MemberEligible: true},
				{OperationID: "patch-organization", Permission: PermissionOrganizationWrite, Resource: "organization", Action: "update", OrgScope: "token_org_id"},
				{OperationID: "list-organization-members", Permission: PermissionMemberRead, Resource: "organization_member", Action: "list", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "invite-organization-member", Permission: PermissionMemberInvite, Resource: "organization_member", Action: "invite", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "update-organization-member-roles", Permission: PermissionMemberRolesWrite, Resource: "organization_member_roles", Action: "write", OrgScope: "token_org_id"},
				{OperationID: "get-organization-member-capabilities", Permission: PermissionMemberCapabilitiesRead, Resource: "organization_member_capabilities", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "put-organization-member-capabilities", Permission: PermissionMemberCapabilitiesWrite, Resource: "organization_member_capabilities", Action: "write", OrgScope: "token_org_id"},
				{OperationID: "get-organization-iam-policy", Permission: PermissionIAMPolicyRead, Resource: "organization_iam_policy", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "set-organization-iam-policy", Permission: PermissionIAMPolicySet, Resource: "organization_iam_policy", Action: "write", OrgScope: "token_org_id"},
				{OperationID: "test-organization-iam-permissions", Permission: PermissionIAMPolicyTest, Resource: "organization_iam_policy", Action: "test", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "list-service-accounts", Permission: PermissionAPICredentialsRead, Resource: "service_account", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "get-service-account", Permission: PermissionAPICredentialsRead, Resource: "service_account", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "disable-service-account", Permission: PermissionAPICredentialsRevoke, Resource: "service_account", Action: "disable", OrgScope: "token_org_id"},
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
				{OperationID: "begin-github-installation", Permission: PermissionSandboxGitHubWrite, Resource: "github_installation", Action: "connect", OrgScope: "token_org_id"},
				{OperationID: "list-github-installations", Permission: PermissionSandboxGitHubRead, Resource: "github_installation", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "get-execution", Permission: PermissionSandboxExecutionRead, Resource: "execution", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "get-execution-logs", Permission: PermissionSandboxLogsRead, Resource: "execution_logs", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "list-runs", Permission: PermissionSandboxExecutionRead, Resource: "run", Action: "list", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "get-run", Permission: PermissionSandboxExecutionRead, Resource: "run", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "search-run-logs", Permission: PermissionSandboxLogsRead, Resource: "run_logs", Action: "search", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "get-jobs-analytics", Permission: PermissionSandboxAnalyticsRead, Resource: "run_analytics_jobs", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "get-costs-analytics", Permission: PermissionSandboxAnalyticsRead, Resource: "run_analytics_costs", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "get-caches-analytics", Permission: PermissionSandboxAnalyticsRead, Resource: "run_analytics_caches", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "get-runner-sizing-analytics", Permission: PermissionSandboxAnalyticsRead, Resource: "run_analytics_runner_sizing", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "list-sticky-disks", Permission: PermissionSandboxStickyDiskRead, Resource: "sticky_disk", Action: "list", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "reset-sticky-disk", Permission: PermissionSandboxStickyDiskWrite, Resource: "sticky_disk", Action: "reset", OrgScope: "token_org_id"},
				{OperationID: "create-execution-schedule", Permission: PermissionSandboxExecutionScheduleWrite, Resource: "execution_schedule", Action: "create", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "list-execution-schedules", Permission: PermissionSandboxExecutionScheduleRead, Resource: "execution_schedule", Action: "list", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "get-execution-schedule", Permission: PermissionSandboxExecutionScheduleRead, Resource: "execution_schedule", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "pause-execution-schedule", Permission: PermissionSandboxExecutionScheduleWrite, Resource: "execution_schedule", Action: "pause", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "resume-execution-schedule", Permission: PermissionSandboxExecutionScheduleWrite, Resource: "execution_schedule", Action: "resume", OrgScope: "token_org_id", MemberEligible: true},
			},
		},
		{
			Service: "billing-service",
			Operations: []Operation{
				{OperationID: "get-billing-entitlements", Permission: PermissionBillingRead, Resource: "billing_entitlements", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "list-billing-grants", Permission: PermissionBillingRead, Resource: "billing_grant", Action: "list", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "list-billing-documents", Permission: PermissionBillingRead, Resource: "billing_document", Action: "list", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "list-billing-contracts", Permission: PermissionBillingRead, Resource: "billing_contract", Action: "list", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "list-billing-plans", Permission: PermissionBillingRead, Resource: "billing_plan", Action: "list", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "get-billing-statement", Permission: PermissionBillingRead, Resource: "billing_statement", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "create-billing-checkout", Permission: PermissionBillingCheckout, Resource: "billing_checkout", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "create-billing-contract", Permission: PermissionBillingCheckout, Resource: "billing_contract_checkout", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "create-billing-contract-change", Permission: PermissionBillingCheckout, Resource: "billing_contract_change", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "cancel-billing-contract", Permission: PermissionBillingCheckout, Resource: "billing_contract", Action: "cancel", OrgScope: "token_org_id"},
				{OperationID: "create-billing-portal", Permission: PermissionBillingCheckout, Resource: "billing_portal", Action: "create", OrgScope: "token_org_id"},
			},
		},
		{
			Service: "projects-service",
			Operations: []Operation{
				{OperationID: "create-project", Permission: PermissionProjectWrite, Resource: "project", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "list-projects", Permission: PermissionProjectRead, Resource: "project", Action: "list", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "get-project", Permission: PermissionProjectRead, Resource: "project", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "patch-project", Permission: PermissionProjectWrite, Resource: "project", Action: "update", OrgScope: "token_org_id"},
				{OperationID: "archive-project", Permission: PermissionProjectWrite, Resource: "project", Action: "archive", OrgScope: "token_org_id"},
				{OperationID: "restore-project", Permission: PermissionProjectWrite, Resource: "project", Action: "restore", OrgScope: "token_org_id"},
				{OperationID: "list-project-environments", Permission: PermissionProjectEnvironmentRead, Resource: "project_environment", Action: "list", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "create-project-environment", Permission: PermissionProjectEnvironmentWrite, Resource: "project_environment", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "patch-project-environment", Permission: PermissionProjectEnvironmentWrite, Resource: "project_environment", Action: "update", OrgScope: "token_org_id"},
				{OperationID: "archive-project-environment", Permission: PermissionProjectEnvironmentWrite, Resource: "project_environment", Action: "archive", OrgScope: "token_org_id"},
				{OperationID: "list-project-events", Permission: PermissionProjectEventRead, Resource: "project_event", Action: "list", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "resolve-project", Permission: PermissionProjectResolve, Resource: "project", Action: "resolve", OrgScope: "token_org_id", MemberEligible: true},
			},
		},
		{
			Service: "source-code-hosting-service",
			Operations: []Operation{
				{OperationID: "create-source-repository", Permission: PermissionSourceRepoWrite, Resource: "source_repository", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "create-source-git-credential", Permission: PermissionSourceGitCredentialWrite, Resource: "source_git_credential", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "list-source-repositories", Permission: PermissionSourceRepoRead, Resource: "source_repository", Action: "list", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "get-source-repository", Permission: PermissionSourceRepoRead, Resource: "source_repository", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "list-source-refs", Permission: PermissionSourceRepoRead, Resource: "source_ref", Action: "list", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "get-source-tree", Permission: PermissionSourceRepoRead, Resource: "source_tree", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "get-source-blob", Permission: PermissionSourceRepoRead, Resource: "source_blob", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "create-source-checkout-grant", Permission: PermissionSourceCheckoutWrite, Resource: "source_checkout_grant", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "create-source-integration", Permission: PermissionSourceIntegrationWrite, Resource: "source_integration", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "create-source-workflow-run", Permission: PermissionSourceWorkflowWrite, Resource: "source_workflow_run", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "list-source-workflow-runs", Permission: PermissionSourceWorkflowRead, Resource: "source_workflow_run", Action: "list", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "get-source-workflow-run", Permission: PermissionSourceWorkflowRead, Resource: "source_workflow_run", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "download-source-checkout-archive", Permission: PermissionSourceCheckoutWrite, Resource: "source_checkout_archive", Action: "download", OrgScope: "token_org_id"},
			},
		},
		{
			Service: "secrets-service",
			Operations: []Operation{
				{OperationID: "put-secret", Permission: PermissionSecretWrite, Resource: "secret", Action: "write", OrgScope: "token_org_id"},
				{OperationID: "read-secret", Permission: PermissionSecretRead, Resource: "secret", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "list-secrets", Permission: PermissionSecretList, Resource: "secret", Action: "list", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "resolve-secrets", Permission: PermissionSecretRead, Resource: "secret", Action: "resolve", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "delete-secret", Permission: PermissionSecretDelete, Resource: "secret", Action: "delete", OrgScope: "token_org_id"},
				{OperationID: "put-variable", Permission: PermissionVariableWrite, Resource: "variable", Action: "write", OrgScope: "token_org_id"},
				{OperationID: "read-variable", Permission: PermissionVariableRead, Resource: "variable", Action: "read", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "list-variables", Permission: PermissionVariableList, Resource: "variable", Action: "list", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "delete-variable", Permission: PermissionVariableDelete, Resource: "variable", Action: "delete", OrgScope: "token_org_id"},
				{OperationID: "create-opaque-credential", Permission: PermissionCredentialCreate, Resource: "opaque_credential", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "get-opaque-credential", Permission: PermissionCredentialRead, Resource: "opaque_credential", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "list-opaque-credentials", Permission: PermissionCredentialList, Resource: "opaque_credential", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "roll-opaque-credential", Permission: PermissionCredentialRoll, Resource: "opaque_credential", Action: "roll", OrgScope: "token_org_id"},
				{OperationID: "revoke-opaque-credential", Permission: PermissionCredentialRevoke, Resource: "opaque_credential", Action: "revoke", OrgScope: "token_org_id"},
				{OperationID: "create-transit-key", Permission: PermissionTransitKeyCreate, Resource: "transit_key", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "rotate-transit-key", Permission: PermissionTransitKeyRotate, Resource: "transit_key", Action: "rotate", OrgScope: "token_org_id"},
				{OperationID: "encrypt-with-transit-key", Permission: PermissionTransitEncrypt, Resource: "transit_key", Action: "encrypt", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "decrypt-with-transit-key", Permission: PermissionTransitDecrypt, Resource: "transit_key", Action: "decrypt", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "sign-with-transit-key", Permission: PermissionTransitSign, Resource: "transit_key", Action: "sign", OrgScope: "token_org_id", MemberEligible: true},
				{OperationID: "verify-with-transit-key", Permission: PermissionTransitVerify, Resource: "transit_key", Action: "verify", OrgScope: "token_org_id", MemberEligible: true},
			},
		},
		{
			Service: "governance-service",
			Operations: []Operation{
				{OperationID: "list-audit-events", Permission: PermissionGovernanceAuditRead, Resource: "audit_event", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "list-data-exports", Permission: PermissionGovernanceExportRead, Resource: "data_export", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "create-data-export", Permission: PermissionGovernanceExportCreate, Resource: "data_export", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "get-data-export", Permission: PermissionGovernanceExportRead, Resource: "data_export", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "download-data-export", Permission: PermissionGovernanceExportRead, Resource: "data_export", Action: "download", OrgScope: "token_org_id"},
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

func KnownPermissions() map[string]struct{} {
	known := map[string]struct{}{}
	for _, service := range defaultOperations.Services {
		for _, operation := range service.Operations {
			known[operation.Permission] = struct{}{}
		}
	}
	return known
}

func OpenBaoRolesForPermissions(permissions []string) []string {
	roles := map[string]struct{}{}
	for _, permission := range permissions {
		if role, ok := openBaoRolesByPermission[permission]; ok {
			roles[role] = struct{}{}
		}
	}
	return sortedKeys(roles)
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
