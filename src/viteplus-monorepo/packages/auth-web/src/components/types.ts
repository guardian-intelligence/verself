// Wire-format DTO mirrors. Each consuming app's generated/parsed identity
// types satisfy these structurally — components only read these fields, so
// the package stays decoupled from any specific generated client.

export interface PolicyRole {
  readonly role_key: string;
  readonly display_name: string;
  readonly permissions: ReadonlyArray<string>;
}

export interface PolicyDocument {
  readonly org_id: string;
  readonly version: number;
  readonly roles: ReadonlyArray<PolicyRole>;
  readonly updated_at: string;
  readonly updated_by: string;
}

export interface Operation {
  readonly operation_id: string;
  readonly permission: string;
  readonly resource: string;
  readonly action: string;
  readonly org_scope: string;
}

export interface ServiceOperations {
  readonly service: string;
  readonly operations: ReadonlyArray<Operation>;
}

export interface Operations {
  readonly services: ReadonlyArray<ServiceOperations>;
}

export interface Member {
  readonly user_id: string;
  readonly email: string;
  readonly display_name: string;
  readonly state: string;
  readonly role_keys: ReadonlyArray<string>;
}

export interface Organization {
  readonly org_id: string;
  readonly name: string;
  readonly caller: Member;
  readonly permissions: ReadonlyArray<string>;
  readonly policy: PolicyDocument;
}

// Request DTOs use mutable arrays so consuming-app validators (which produce
// `string[]` after parsing) can satisfy the shape without a `[...arr]` copy
// at every call site. Response DTOs stay readonly.
export interface InviteMemberRequest {
  email: string;
  familyName?: string;
  givenName?: string;
  roleKeys: Array<string>;
}

export interface InviteMemberResponse {
  readonly user_id: string;
  readonly email: string;
}

export interface UpdateMemberRolesRequest {
  userId: string;
  roleKeys: Array<string>;
}

export interface PutPolicyRequestRole {
  role_key: string;
  display_name: string;
  permissions: Array<string>;
}

export interface PutPolicyRequest {
  version: number;
  roles: Array<PutPolicyRequestRole>;
}
