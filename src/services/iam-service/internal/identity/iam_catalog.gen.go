// Code generated from the Smithy API contract. DO NOT EDIT.

package identity

import runtimeiam "github.com/verself/service-runtime/iam"

func smithyIAMServiceOperations() ServiceOperations {
	return ServiceOperations{Service: "iam-service", Operations: []Operation{
		{OperationID: "get-member", Permission: runtimeiam.Permission("iam:member:read"), Resource: runtimeiam.ResourceKind("member"), Action: runtimeiam.Action("read"), OrgScope: runtimeiam.OrgScope("path_org_id"), MemberEligible: true},
		{OperationID: "get-organization", Permission: runtimeiam.Permission("iam:organization:read"), Resource: runtimeiam.ResourceKind("organization"), Action: runtimeiam.Action("read"), OrgScope: runtimeiam.OrgScope("path_org_id"), MemberEligible: true},
		{OperationID: "list-members", Permission: runtimeiam.Permission("iam:member:list"), Resource: runtimeiam.ResourceKind("member"), Action: runtimeiam.Action("list"), OrgScope: runtimeiam.OrgScope("path_org_id"), MemberEligible: true},
		{OperationID: "list-organizations", Permission: runtimeiam.Permission("iam:organization:list"), Resource: runtimeiam.ResourceKind("organization"), Action: runtimeiam.Action("list"), OrgScope: runtimeiam.OrgScope("token_role_assignment_org_ids"), MemberEligible: true},
		{OperationID: "update-member-role", Permission: runtimeiam.Permission("iam:member:update_role"), Resource: runtimeiam.ResourceKind("member"), Action: runtimeiam.Action("update"), OrgScope: runtimeiam.OrgScope("path_org_id"), MemberEligible: false},
		{OperationID: "update-organization", Permission: runtimeiam.Permission("iam:organization:update"), Resource: runtimeiam.ResourceKind("organization"), Action: runtimeiam.Action("update"), OrgScope: runtimeiam.OrgScope("path_org_id"), MemberEligible: false},
	}}
}
