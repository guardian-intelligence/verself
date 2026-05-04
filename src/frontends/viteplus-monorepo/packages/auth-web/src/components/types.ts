// Wire-format DTO mirrors. Each consuming app's generated/parsed identity
// types satisfy these structurally — components only read these fields, so
// the package stays decoupled from any specific generated client.

export interface MemberCapability {
  readonly key: string;
  readonly label: string;
  readonly description: string;
  readonly default_enabled: boolean;
  readonly permissions: ReadonlyArray<string>;
}

export interface MemberCapabilitiesDocument {
  readonly org_id: string;
  readonly version: number;
  readonly enabled_keys: ReadonlyArray<string>;
  readonly updated_at: string;
  readonly updated_by: string;
}

export interface MemberCapabilities {
  readonly document: MemberCapabilitiesDocument;
  readonly catalog: ReadonlyArray<MemberCapability>;
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
  readonly display_name: string;
  readonly slug: string;
  readonly version: number;
  readonly org_acl_version: number;
  readonly caller: Member;
  readonly permissions: ReadonlyArray<string>;
  readonly member_capabilities: MemberCapabilitiesDocument;
}

export interface OrganizationMetadata {
  readonly org_id: string;
  readonly display_name: string;
  readonly slug: string;
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
  expectedRoleKeys: Array<string>;
  expectedOrgAclVersion: number;
}

export interface UpdateOrganizationRequest {
  version: number;
  display_name?: string;
  slug?: string;
}

export interface PutMemberCapabilitiesRequest {
  version: number;
  enabled_keys: Array<string>;
}
