package identity

const (
	PermissionOrganizationList              = "iam:organization:list"
	PermissionOrganizationRead              = "iam:organization:read"
	PermissionOrganizationUpdate            = "iam:organization:update"
	PermissionMemberList                    = "iam:member:list"
	PermissionMemberRead                    = "iam:member:read"
	PermissionMemberInvite                  = "iam:member:invite"
	PermissionIAMPolicyRead                 = "iam:policy:get"
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
	PermissionAnalyticsDatasetRead          = "analytics:dataset:read"
	PermissionAnalyticsDatasetReadRaw       = "analytics:dataset:read_raw"
	PermissionAnalyticsDatasetIngest        = "analytics:dataset:ingest"
	PermissionAnalyticsDatasetManage        = "analytics:dataset:manage"
	PermissionGovernanceAPIActivityRead     = "governance:api_activity:read"
	PermissionGovernanceAPIActivityExport   = "governance:api_activity:export"
	PermissionProfileRead                   = "profile:self:read"
	PermissionProfileIdentityWrite          = "profile:self:identity:write"
	PermissionProfilePreferencesWrite       = "profile:self:preferences:write"
	PermissionNotificationsRead             = "notifications:self:read"
	PermissionNotificationsWrite            = "notifications:self:write"
	PermissionNotificationsPreferencesWrite = "notifications:self:preferences:write"
	PermissionNotificationsTest             = "notifications:self:test"
	PermissionMailboxAccountRead            = "mailbox:account:read"
	PermissionMailboxMailRead               = "mailbox:mail:read"
	PermissionMailboxMailWrite              = "mailbox:mail:write"
	PermissionMailboxSyncStatusRead         = "mailbox:sync_status:read"
	PermissionObjectStorageBucketRead       = "object-storage:bucket:read"
	PermissionObjectStorageBucketWrite      = "object-storage:bucket:write"
	PermissionObjectStorageAccessKeyRead    = "object-storage:access-key:read"
	PermissionObjectStorageAccessKeyWrite   = "object-storage:access-key:write"
)

var defaultOperations = Operations{
	Services: []ServiceOperations{
		smithyIAMServiceOperations(),
		{
			Service: "sandbox-rental-service",
			Operations: []Operation{
				{OperationID: "begin-github-installation", Permission: PermissionSandboxGitHubWrite, Resource: "github_installation", Action: "connect", OrgScope: "token_org_id"},
				{OperationID: "list-github-installations", Permission: PermissionSandboxGitHubRead, Resource: "github_installation", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "get-execution", Permission: PermissionSandboxExecutionRead, Resource: "execution", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "get-execution-logs", Permission: PermissionSandboxLogsRead, Resource: "execution_logs", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "list-runs", Permission: PermissionSandboxExecutionRead, Resource: "run", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "get-run", Permission: PermissionSandboxExecutionRead, Resource: "run", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "search-run-logs", Permission: PermissionSandboxLogsRead, Resource: "run_logs", Action: "search", OrgScope: "token_org_id"},
				{OperationID: "get-jobs-analytics", Permission: PermissionSandboxAnalyticsRead, Resource: "run_analytics_jobs", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "get-costs-analytics", Permission: PermissionSandboxAnalyticsRead, Resource: "run_analytics_costs", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "get-caches-analytics", Permission: PermissionSandboxAnalyticsRead, Resource: "run_analytics_caches", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "get-runner-sizing-analytics", Permission: PermissionSandboxAnalyticsRead, Resource: "run_analytics_runner_sizing", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "create-execution-schedule", Permission: PermissionSandboxExecutionScheduleWrite, Resource: "execution_schedule", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "list-execution-schedules", Permission: PermissionSandboxExecutionScheduleRead, Resource: "execution_schedule", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "get-execution-schedule", Permission: PermissionSandboxExecutionScheduleRead, Resource: "execution_schedule", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "pause-execution-schedule", Permission: PermissionSandboxExecutionScheduleWrite, Resource: "execution_schedule", Action: "pause", OrgScope: "token_org_id"},
				{OperationID: "resume-execution-schedule", Permission: PermissionSandboxExecutionScheduleWrite, Resource: "execution_schedule", Action: "resume", OrgScope: "token_org_id"},
			},
		},
		{
			Service: "billing-service",
			Operations: []Operation{
				{OperationID: "get-billing-entitlements", Permission: PermissionBillingRead, Resource: "billing_entitlements", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "list-billing-grants", Permission: PermissionBillingRead, Resource: "billing_grant", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "list-billing-documents", Permission: PermissionBillingRead, Resource: "billing_document", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "list-billing-contracts", Permission: PermissionBillingRead, Resource: "billing_contract", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "list-billing-plans", Permission: PermissionBillingRead, Resource: "billing_plan", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "get-billing-statement", Permission: PermissionBillingRead, Resource: "billing_statement", Action: "read", OrgScope: "token_org_id"},
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
				{OperationID: "list-projects", Permission: PermissionProjectRead, Resource: "project", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "get-project", Permission: PermissionProjectRead, Resource: "project", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "patch-project", Permission: PermissionProjectWrite, Resource: "project", Action: "update", OrgScope: "token_org_id"},
				{OperationID: "archive-project", Permission: PermissionProjectWrite, Resource: "project", Action: "archive", OrgScope: "token_org_id"},
				{OperationID: "restore-project", Permission: PermissionProjectWrite, Resource: "project", Action: "restore", OrgScope: "token_org_id"},
				{OperationID: "list-project-environments", Permission: PermissionProjectEnvironmentRead, Resource: "project_environment", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "create-project-environment", Permission: PermissionProjectEnvironmentWrite, Resource: "project_environment", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "patch-project-environment", Permission: PermissionProjectEnvironmentWrite, Resource: "project_environment", Action: "update", OrgScope: "token_org_id"},
				{OperationID: "archive-project-environment", Permission: PermissionProjectEnvironmentWrite, Resource: "project_environment", Action: "archive", OrgScope: "token_org_id"},
				{OperationID: "list-project-events", Permission: PermissionProjectEventRead, Resource: "project_event", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "resolve-project", Permission: PermissionProjectResolve, Resource: "project", Action: "resolve", OrgScope: "token_org_id"},
			},
		},
		{
			Service: "source-code-hosting-service",
			Operations: []Operation{
				{OperationID: "create-source-repository", Permission: PermissionSourceRepoWrite, Resource: "source_repository", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "create-source-git-credential", Permission: PermissionSourceGitCredentialWrite, Resource: "source_git_credential", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "list-source-repositories", Permission: PermissionSourceRepoRead, Resource: "source_repository", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "get-source-repository", Permission: PermissionSourceRepoRead, Resource: "source_repository", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "list-source-refs", Permission: PermissionSourceRepoRead, Resource: "source_ref", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "get-source-tree", Permission: PermissionSourceRepoRead, Resource: "source_tree", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "get-source-blob", Permission: PermissionSourceRepoRead, Resource: "source_blob", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "create-source-checkout-grant", Permission: PermissionSourceCheckoutWrite, Resource: "source_checkout_grant", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "create-source-integration", Permission: PermissionSourceIntegrationWrite, Resource: "source_integration", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "create-source-workflow-run", Permission: PermissionSourceWorkflowWrite, Resource: "source_workflow_run", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "list-source-workflow-runs", Permission: PermissionSourceWorkflowRead, Resource: "source_workflow_run", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "get-source-workflow-run", Permission: PermissionSourceWorkflowRead, Resource: "source_workflow_run", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "download-source-checkout-archive", Permission: PermissionSourceCheckoutWrite, Resource: "source_checkout_archive", Action: "download", OrgScope: "token_org_id"},
			},
		},
		{
			Service: "secrets-service",
			Operations: []Operation{
				{OperationID: "put-secret", Permission: PermissionSecretWrite, Resource: "secret", Action: "write", OrgScope: "token_org_id"},
				{OperationID: "read-secret", Permission: PermissionSecretRead, Resource: "secret", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "list-secrets", Permission: PermissionSecretList, Resource: "secret", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "resolve-secrets", Permission: PermissionSecretRead, Resource: "secret", Action: "resolve", OrgScope: "token_org_id"},
				{OperationID: "delete-secret", Permission: PermissionSecretDelete, Resource: "secret", Action: "delete", OrgScope: "token_org_id"},
				{OperationID: "put-variable", Permission: PermissionVariableWrite, Resource: "variable", Action: "write", OrgScope: "token_org_id"},
				{OperationID: "read-variable", Permission: PermissionVariableRead, Resource: "variable", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "list-variables", Permission: PermissionVariableList, Resource: "variable", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "delete-variable", Permission: PermissionVariableDelete, Resource: "variable", Action: "delete", OrgScope: "token_org_id"},
				{OperationID: "create-opaque-credential", Permission: PermissionCredentialCreate, Resource: "opaque_credential", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "get-opaque-credential", Permission: PermissionCredentialRead, Resource: "opaque_credential", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "list-opaque-credentials", Permission: PermissionCredentialList, Resource: "opaque_credential", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "roll-opaque-credential", Permission: PermissionCredentialRoll, Resource: "opaque_credential", Action: "roll", OrgScope: "token_org_id"},
				{OperationID: "revoke-opaque-credential", Permission: PermissionCredentialRevoke, Resource: "opaque_credential", Action: "revoke", OrgScope: "token_org_id"},
				{OperationID: "create-transit-key", Permission: PermissionTransitKeyCreate, Resource: "transit_key", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "rotate-transit-key", Permission: PermissionTransitKeyRotate, Resource: "transit_key", Action: "rotate", OrgScope: "token_org_id"},
				{OperationID: "encrypt-with-transit-key", Permission: PermissionTransitEncrypt, Resource: "transit_key", Action: "encrypt", OrgScope: "token_org_id"},
				{OperationID: "decrypt-with-transit-key", Permission: PermissionTransitDecrypt, Resource: "transit_key", Action: "decrypt", OrgScope: "token_org_id"},
				{OperationID: "sign-with-transit-key", Permission: PermissionTransitSign, Resource: "transit_key", Action: "sign", OrgScope: "token_org_id"},
				{OperationID: "verify-with-transit-key", Permission: PermissionTransitVerify, Resource: "transit_key", Action: "verify", OrgScope: "token_org_id"},
			},
		},
		{
			Service: "governance-service",
			Operations: []Operation{
				{OperationID: "list-api-activities", Permission: PermissionGovernanceAPIActivityRead, Resource: "api_activity", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "list-data-exports", Permission: PermissionGovernanceAPIActivityExport, Resource: "api_activity", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "create-data-export", Permission: PermissionGovernanceAPIActivityExport, Resource: "api_activity", Action: "export", OrgScope: "token_org_id"},
				{OperationID: "get-data-export", Permission: PermissionGovernanceAPIActivityExport, Resource: "api_activity", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "download-data-export", Permission: PermissionGovernanceAPIActivityExport, Resource: "api_activity", Action: "download", OrgScope: "token_org_id"},
			},
		},
		{
			Service: "analytics-service",
			Operations: []Operation{
				{OperationID: "ingest-analytics-logs", Permission: PermissionAnalyticsDatasetIngest, Resource: "analytics_dataset", Action: "ingest", OrgScope: "token_org_id"},
				{OperationID: "query-analytics-events", Permission: PermissionAnalyticsDatasetRead, Resource: "analytics_dataset", Action: "query", OrgScope: "token_org_id"},
				{OperationID: "query-raw-analytics-events", Permission: PermissionAnalyticsDatasetReadRaw, Resource: "analytics_dataset", Action: "query", OrgScope: "token_org_id"},
				{OperationID: "manage-analytics-dataset", Permission: PermissionAnalyticsDatasetManage, Resource: "analytics_dataset", Action: "manage", OrgScope: "token_org_id"},
			},
		},
		{
			Service: "object-storage-service",
			Operations: []Operation{
				{OperationID: "list-object-storage-buckets", Permission: PermissionObjectStorageBucketRead, Resource: "bucket", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "create-object-storage-bucket", Permission: PermissionObjectStorageBucketWrite, Resource: "bucket", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "get-object-storage-bucket", Permission: PermissionObjectStorageBucketRead, Resource: "bucket", Action: "read", OrgScope: "token_org_id"},
				{OperationID: "update-object-storage-bucket", Permission: PermissionObjectStorageBucketWrite, Resource: "bucket", Action: "update", OrgScope: "token_org_id"},
				{OperationID: "delete-object-storage-bucket", Permission: PermissionObjectStorageBucketWrite, Resource: "bucket", Action: "delete", OrgScope: "token_org_id"},
				{OperationID: "create-object-storage-bucket-alias", Permission: PermissionObjectStorageBucketWrite, Resource: "bucket_alias", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "list-object-storage-bucket-aliases", Permission: PermissionObjectStorageBucketRead, Resource: "bucket_alias", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "delete-object-storage-bucket-alias", Permission: PermissionObjectStorageBucketWrite, Resource: "bucket_alias", Action: "delete", OrgScope: "token_org_id"},
				{OperationID: "list-object-storage-credentials", Permission: PermissionObjectStorageAccessKeyRead, Resource: "object_storage_credential", Action: "list", OrgScope: "token_org_id"},
				{OperationID: "create-object-storage-access-key", Permission: PermissionObjectStorageAccessKeyWrite, Resource: "object_storage_access_key", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "roll-object-storage-access-key", Permission: PermissionObjectStorageAccessKeyWrite, Resource: "object_storage_access_key", Action: "roll", OrgScope: "token_org_id"},
				{OperationID: "delete-object-storage-access-key", Permission: PermissionObjectStorageAccessKeyWrite, Resource: "object_storage_access_key", Action: "delete", OrgScope: "token_org_id"},
				{OperationID: "create-object-storage-mtls-principal", Permission: PermissionObjectStorageAccessKeyWrite, Resource: "object_storage_mtls_principal", Action: "create", OrgScope: "token_org_id"},
				{OperationID: "delete-object-storage-mtls-principal", Permission: PermissionObjectStorageAccessKeyWrite, Resource: "object_storage_mtls_principal", Action: "delete", OrgScope: "token_org_id"},
			},
		},
		{
			Service: "profile-service",
			Operations: []Operation{
				{OperationID: "get-profile", Permission: PermissionProfileRead, Resource: "profile_subject", Action: "read", OrgScope: "token_subject"},
				{OperationID: "patch-profile-identity", Permission: PermissionProfileIdentityWrite, Resource: "profile_identity", Action: "write", OrgScope: "token_subject"},
				{OperationID: "put-profile-preferences", Permission: PermissionProfilePreferencesWrite, Resource: "profile_preferences", Action: "write", OrgScope: "token_subject"},
			},
		},
		{
			Service: "notifications-service",
			Operations: []Operation{
				{OperationID: "list-notifications", Permission: PermissionNotificationsRead, Resource: "notification_subject", Action: "list", OrgScope: "token_subject"},
				{OperationID: "get-notification-summary", Permission: PermissionNotificationsRead, Resource: "notification_subject", Action: "read", OrgScope: "token_subject"},
				{OperationID: "put-notification-preferences", Permission: PermissionNotificationsPreferencesWrite, Resource: "notification_preferences", Action: "write", OrgScope: "token_subject"},
				{OperationID: "advance-notification-read-cursor", Permission: PermissionNotificationsWrite, Resource: "notification_subject", Action: "write", OrgScope: "token_subject"},
				{OperationID: "dismiss-notification", Permission: PermissionNotificationsWrite, Resource: "notification_subject", Action: "write", OrgScope: "token_subject"},
				{OperationID: "mark-notification-read", Permission: PermissionNotificationsWrite, Resource: "notification_subject", Action: "write", OrgScope: "token_subject"},
				{OperationID: "clear-notifications", Permission: PermissionNotificationsWrite, Resource: "notification_subject", Action: "write", OrgScope: "token_subject"},
				{OperationID: "publish-test-notification", Permission: PermissionNotificationsTest, Resource: "notification_subject", Action: "test", OrgScope: "token_subject"},
			},
		},
		{
			Service: "mailbox-service",
			Operations: []Operation{
				{OperationID: "mail-mark-read", Permission: PermissionMailboxMailWrite, Resource: "mailbox_email", Action: "write", OrgScope: "token_subject"},
				{OperationID: "mail-mark-unread", Permission: PermissionMailboxMailWrite, Resource: "mailbox_email", Action: "write", OrgScope: "token_subject"},
				{OperationID: "mail-flag", Permission: PermissionMailboxMailWrite, Resource: "mailbox_email", Action: "write", OrgScope: "token_subject"},
				{OperationID: "mail-unflag", Permission: PermissionMailboxMailWrite, Resource: "mailbox_email", Action: "write", OrgScope: "token_subject"},
				{OperationID: "mail-move", Permission: PermissionMailboxMailWrite, Resource: "mailbox_email", Action: "write", OrgScope: "token_subject"},
				{OperationID: "mail-trash", Permission: PermissionMailboxMailWrite, Resource: "mailbox_email", Action: "write", OrgScope: "token_subject"},
				{OperationID: "mail-body", Permission: PermissionMailboxMailRead, Resource: "mailbox_email", Action: "read", OrgScope: "token_subject"},
				{OperationID: "mail-account", Permission: PermissionMailboxAccountRead, Resource: "mailbox_account", Action: "read", OrgScope: "token_subject"},
				{OperationID: "mail-sync-status", Permission: PermissionMailboxSyncStatusRead, Resource: "mailbox_sync_status", Action: "read", OrgScope: "token_subject"},
			},
		},
	},
}

func KnownPermissions() map[string]struct{} {
	known := map[string]struct{}{}
	for _, service := range defaultOperations.Services {
		for _, operation := range service.Operations {
			known[string(operation.Permission)] = struct{}{}
		}
	}
	return known
}

// DefaultOperations returns a copy of the static operation catalog used by IAM
// policy validation and operator inspection.
func DefaultOperations() Operations {
	services := make([]ServiceOperations, len(defaultOperations.Services))
	for i, service := range defaultOperations.Services {
		service.Operations = append([]Operation(nil), service.Operations...)
		services[i] = service
	}
	return Operations{Services: services}
}
