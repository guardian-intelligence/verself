// Wire-format DTO mirrors. Each consuming app's generated/parsed identity
// types satisfy these structurally, so components stay decoupled from any
// specific generated client.

export interface Member {
  readonly user_id: string;
  readonly email: string;
  readonly display_name: string;
  readonly state: string;
}

export interface MemberInvitation {
  readonly org_id: string;
  readonly member_id: string;
  readonly email: string;
  readonly status: string;
  readonly roles: ReadonlyArray<string>;
}

export interface Organization {
  readonly org_id: string;
  readonly display_name: string;
  readonly slug: string;
  readonly version: number;
  readonly permissions: ReadonlyArray<string>;
}

export interface OrganizationMetadata {
  readonly org_id: string;
  readonly display_name: string;
  readonly slug: string;
}

export interface UpdateOrganizationRequest {
  version: number;
  display_name?: string;
  slug?: string;
}

export interface CreateOrganizationRequest {
  display_name: string;
  slug?: string;
}

export interface InviteMemberRequest {
  email: string;
  given_name?: string;
  family_name?: string;
  roles?: ReadonlyArray<string>;
}
