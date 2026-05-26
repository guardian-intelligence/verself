package identity

import runtimeiam "github.com/verself/service-runtime/iam"

func smithyIAMServiceOperations() ServiceOperations {
	return ServiceOperations{Service: "iam-service", Operations: []Operation{
		{OperationID: "create-device-session", Permission: runtimeiam.Permission("iam:device_session:create"), Resource: runtimeiam.ResourceKind("device_session"), Action: runtimeiam.Action("create"), OrgScope: runtimeiam.OrgScope("request_subject")},
		{OperationID: "create-organization", Permission: runtimeiam.Permission("iam:organization:create"), Resource: runtimeiam.ResourceKind("organization"), Action: runtimeiam.Action("create"), OrgScope: runtimeiam.OrgScope("request_subject_id")},
		{OperationID: "check-organization-slug-availability", Permission: runtimeiam.Permission("iam:organization_slug:check"), Resource: runtimeiam.ResourceKind("organization"), Action: runtimeiam.Action("read"), OrgScope: runtimeiam.OrgScope("installation")},
		{OperationID: "get-auth-context", Permission: runtimeiam.Permission("iam:account:read"), Resource: runtimeiam.ResourceKind("account"), Action: runtimeiam.Action("read"), OrgScope: runtimeiam.OrgScope("request_subject")},
		{OperationID: "get-member", Permission: runtimeiam.Permission("iam:member:read"), Resource: runtimeiam.ResourceKind("member"), Action: runtimeiam.Action("read"), OrgScope: runtimeiam.OrgScope("path_org_id")},
		{OperationID: "get-organization", Permission: runtimeiam.Permission("iam:organization:read"), Resource: runtimeiam.ResourceKind("organization"), Action: runtimeiam.Action("read"), OrgScope: runtimeiam.OrgScope("path_org_id")},
		{OperationID: "get-iam-policy", Permission: runtimeiam.Permission("iam:policy:get"), Resource: runtimeiam.ResourceKind("organization"), Action: runtimeiam.Action("read"), OrgScope: runtimeiam.OrgScope("path_org_id")},
		{OperationID: "invite-member", Permission: runtimeiam.Permission("iam:member:invite"), Resource: runtimeiam.ResourceKind("member"), Action: runtimeiam.Action("invite"), OrgScope: runtimeiam.OrgScope("path_org_id")},
		{OperationID: "list-account-connections", Permission: runtimeiam.Permission("iam:account_connection:read"), Resource: runtimeiam.ResourceKind("account_connection"), Action: runtimeiam.Action("read"), OrgScope: runtimeiam.OrgScope("request_subject")},
		{OperationID: "list-device-sessions", Permission: runtimeiam.Permission("iam:device_session:read"), Resource: runtimeiam.ResourceKind("device_session"), Action: runtimeiam.Action("read"), OrgScope: runtimeiam.OrgScope("request_subject")},
		{OperationID: "list-members", Permission: runtimeiam.Permission("iam:member:list"), Resource: runtimeiam.ResourceKind("member"), Action: runtimeiam.Action("list"), OrgScope: runtimeiam.OrgScope("path_org_id")},
		{OperationID: "list-organizations", Permission: runtimeiam.Permission("iam:organization:list"), Resource: runtimeiam.ResourceKind("organization"), Action: runtimeiam.Action("list"), OrgScope: runtimeiam.OrgScope("request_subject")},
		{OperationID: "remove-account-connection", Permission: runtimeiam.Permission("iam:account_connection:delete"), Resource: runtimeiam.ResourceKind("account_connection"), Action: runtimeiam.Action("delete"), OrgScope: runtimeiam.OrgScope("request_subject")},
		{OperationID: "revoke-device-session", Permission: runtimeiam.Permission("iam:device_session:delete"), Resource: runtimeiam.ResourceKind("device_session"), Action: runtimeiam.Action("delete"), OrgScope: runtimeiam.OrgScope("request_subject")},
		{OperationID: "set-iam-policy", Permission: runtimeiam.Permission("iam:policy:set"), Resource: runtimeiam.ResourceKind("organization"), Action: runtimeiam.Action("set"), OrgScope: runtimeiam.OrgScope("path_org_id")},
		{OperationID: "start-signup", Permission: runtimeiam.Permission("iam:signup_intent:create"), Resource: runtimeiam.ResourceKind("signup_intent"), Action: runtimeiam.Action("create"), OrgScope: runtimeiam.OrgScope("installation")},
		{OperationID: "test-iam-permissions", Permission: runtimeiam.Permission("iam:policy:test"), Resource: runtimeiam.ResourceKind("organization"), Action: runtimeiam.Action("test"), OrgScope: runtimeiam.OrgScope("path_org_id")},
		{OperationID: "update-organization", Permission: runtimeiam.Permission("iam:organization:update"), Resource: runtimeiam.ResourceKind("organization"), Action: runtimeiam.Action("update"), OrgScope: runtimeiam.OrgScope("path_org_id")},
		{OperationID: "verify-signup", Permission: runtimeiam.Permission("iam:signup_intent:verify"), Resource: runtimeiam.ResourceKind("signup_intent"), Action: runtimeiam.Action("verify"), OrgScope: runtimeiam.OrgScope("installation")},
	}}
}
