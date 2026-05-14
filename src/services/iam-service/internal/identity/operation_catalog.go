package identity

import runtimeiam "github.com/verself/service-runtime/iam"

func smithyIAMServiceOperations() ServiceOperations {
	return ServiceOperations{Service: "iam-service", Operations: []Operation{
		{OperationID: "get-member", Permission: runtimeiam.Permission("iam:member:read"), Resource: runtimeiam.ResourceKind("member"), Action: runtimeiam.Action("read"), OrgScope: runtimeiam.OrgScope("path_org_id")},
		{OperationID: "get-organization", Permission: runtimeiam.Permission("iam:organization:read"), Resource: runtimeiam.ResourceKind("organization"), Action: runtimeiam.Action("read"), OrgScope: runtimeiam.OrgScope("path_org_id")},
		{OperationID: "get-iam-policy", Permission: runtimeiam.Permission("iam:policy:get"), Resource: runtimeiam.ResourceKind("organization"), Action: runtimeiam.Action("read"), OrgScope: runtimeiam.OrgScope("path_org_id")},
		{OperationID: "list-members", Permission: runtimeiam.Permission("iam:member:list"), Resource: runtimeiam.ResourceKind("member"), Action: runtimeiam.Action("list"), OrgScope: runtimeiam.OrgScope("path_org_id")},
		{OperationID: "list-organizations", Permission: runtimeiam.Permission("iam:organization:list"), Resource: runtimeiam.ResourceKind("organization"), Action: runtimeiam.Action("list"), OrgScope: runtimeiam.OrgScope("request_subject")},
		{OperationID: "set-iam-policy", Permission: runtimeiam.Permission("iam:policy:set"), Resource: runtimeiam.ResourceKind("organization"), Action: runtimeiam.Action("set"), OrgScope: runtimeiam.OrgScope("path_org_id")},
		{OperationID: "test-iam-permissions", Permission: runtimeiam.Permission("iam:policy:test"), Resource: runtimeiam.ResourceKind("organization"), Action: runtimeiam.Action("test"), OrgScope: runtimeiam.OrgScope("path_org_id")},
		{OperationID: "update-organization", Permission: runtimeiam.Permission("iam:organization:update"), Resource: runtimeiam.ResourceKind("organization"), Action: runtimeiam.Action("update"), OrgScope: runtimeiam.OrgScope("path_org_id")},
	}}
}
