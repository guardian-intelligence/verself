// Wire-format DTO mirrors. Each consuming app's generated/parsed identity
// types satisfy these structurally — components only read these fields, so
// the package stays decoupled from any specific generated client.

export type OrganizationRoleKey = "owner" | "admin" | "member";

export interface Member {
  readonly user_id: string;
  readonly email: string;
  readonly display_name: string;
  readonly state: string;
  readonly role_keys: ReadonlyArray<OrganizationRoleKey>;
}

export interface Organization {
  readonly org_id: string;
  readonly display_name: string;
  readonly slug: string;
  readonly version: number;
  readonly org_acl_version: number;
  readonly caller: Member;
  readonly permissions: ReadonlyArray<string>;
}

export interface OrganizationMetadata {
  readonly org_id: string;
  readonly display_name: string;
  readonly slug: string;
}

export interface UpdateMemberRolesRequest {
  userId: string;
  roleKeys: Array<OrganizationRoleKey>;
  expectedRoleKeys: Array<OrganizationRoleKey>;
  expectedOrgAclVersion: number;
}

export interface UpdateOrganizationRequest {
  version: number;
  display_name?: string;
  slug?: string;
}
