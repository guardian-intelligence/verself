import * as v from "valibot";
import { createClient, type Client } from "../__generated/identity-api/client/index.js";
import {
  getOrganization as getGeneratedOrganization,
  getOrganizationMemberCapabilities as getGeneratedOrganizationMemberCapabilities,
  inviteOrganizationMember as inviteGeneratedOrganizationMember,
  listOrganizationMembers as listGeneratedOrganizationMembers,
  putOrganizationMemberCapabilities as putGeneratedOrganizationMemberCapabilities,
  updateOrganizationMemberRoles as updateGeneratedOrganizationMemberRoles,
} from "../__generated/identity-api/index.js";
import type {
  IdentityInviteMemberRequestWritable,
  IdentityPutMemberCapabilitiesRequestWritable,
} from "../__generated/identity-api/types.gen.js";
import {
  vIdentityInviteMemberRequestWritable,
  vIdentityInviteMemberResponse,
  vIdentityMember,
  vIdentityMemberCapabilities,
  vIdentityMemberCapabilitiesDocument,
  vIdentityMemberCapability,
  vIdentityMembers,
  vIdentityOrganization,
  vIdentityPutMemberCapabilitiesRequestWritable,
  vIdentityUpdateMemberRolesRequestWritable,
} from "../__generated/identity-api/valibot.gen.js";
import {
  type BearerClientOptions,
  ServiceApiError,
  createBearerJSONHeaders,
  idempotencyHeaders,
  throwGeneratedServiceError,
} from "./service-api";

export interface IdentityClientOptions extends BearerClientOptions {}

export class IdentityApiError extends ServiceApiError {
  constructor(status: number, path: string, body: string) {
    super("Identity API", status, path, body);
    this.name = "IdentityApiError";
  }
}

export function isIdentityApiError(error: unknown): error is IdentityApiError {
  return error instanceof IdentityApiError;
}

function throwIdentityError(path: string, response: Response | undefined, error: unknown): never {
  throwGeneratedServiceError(IdentityApiError, path, response, error);
}

function createIdentityClient(options: IdentityClientOptions): Client {
  return createClient({
    baseUrl: options.baseUrl,
    headers: createBearerJSONHeaders(options.accessToken),
    ...(options.fetch ? { fetch: options.fetch } : {}),
  });
}

const roleKeySchema = v.pipe(v.string(), v.trim(), v.minLength(1), v.maxLength(128));
const roleKeysSchema = v.pipe(
  v.array(roleKeySchema),
  v.minLength(1),
  v.transform((roleKeys) => Array.from(new Set(roleKeys)).sort()),
);

const capabilityKeySchema = v.pipe(v.string(), v.trim(), v.minLength(1), v.maxLength(64));
const capabilityKeysSchema = v.pipe(
  v.array(capabilityKeySchema),
  v.transform((keys) => Array.from(new Set(keys)).sort()),
);

function omitEmptyText(value: string | undefined): string | undefined {
  const trimmed = value?.trim();
  return trimmed ? trimmed : undefined;
}

function parseMember(input: unknown) {
  const { $schema: _schema, role_keys, ...member } = v.parse(vIdentityMember, input);
  return {
    ...member,
    role_keys: role_keys ?? [],
  };
}

export type Member = ReturnType<typeof parseMember>;

function parseMemberCapabilitiesDocument(input: unknown) {
  const { enabled_keys, ...doc } = v.parse(vIdentityMemberCapabilitiesDocument, input);
  return {
    ...doc,
    enabled_keys: enabled_keys ?? [],
  };
}

export type MemberCapabilitiesDocument = ReturnType<typeof parseMemberCapabilitiesDocument>;

function parseMemberCapability(input: unknown) {
  const capability = v.parse(vIdentityMemberCapability, input);
  return {
    ...capability,
    permissions: capability.permissions ?? [],
  };
}

export type MemberCapability = ReturnType<typeof parseMemberCapability>;

function parseMemberCapabilities(input: unknown) {
  const { $schema: _schema, document, catalog } = v.parse(vIdentityMemberCapabilities, input);
  return {
    document: parseMemberCapabilitiesDocument(document),
    catalog: catalog?.map((capability) => parseMemberCapability(capability)) ?? [],
  };
}

export type MemberCapabilities = ReturnType<typeof parseMemberCapabilities>;

function parseOrganization(input: unknown) {
  const {
    $schema: _schema,
    caller,
    permissions,
    member_capabilities,
    ...organization
  } = v.parse(vIdentityOrganization, input);
  return {
    ...organization,
    caller: parseMember(caller),
    permissions: permissions ?? [],
    member_capabilities: parseMemberCapabilitiesDocument(member_capabilities),
  };
}

export type Organization = ReturnType<typeof parseOrganization>;

function parseMembers(input: unknown): Array<Member> {
  const { $schema: _schema, members } = v.parse(vIdentityMembers, input);
  return members?.map((member) => parseMember(member)) ?? [];
}

export const inviteMemberRequestSchema = v.strictObject({
  email: v.pipe(v.string(), v.trim(), v.email()),
  familyName: v.optional(v.pipe(v.string(), v.maxLength(100))),
  givenName: v.optional(v.pipe(v.string(), v.maxLength(100))),
  roleKeys: roleKeysSchema,
});

export type InviteMemberRequest = v.InferInput<typeof inviteMemberRequestSchema>;
export type InviteMemberResponse = v.InferOutput<typeof vIdentityInviteMemberResponse>;

export const updateMemberRolesRequestSchema = v.pipe(
  v.strictObject({
    expectedOrgAclVersion: v.pipe(v.number(), v.integer(), v.minValue(1), v.maxValue(2147483647)),
    expectedRoleKeys: roleKeysSchema,
    roleKeys: roleKeysSchema,
    userId: v.pipe(v.string(), v.trim(), v.minLength(1)),
  }),
  v.transform((input) => ({
    expectedOrgAclVersion: input.expectedOrgAclVersion,
    expectedRoleKeys: input.expectedRoleKeys,
    roleKeys: input.roleKeys,
    userId: input.userId,
  })),
);

export type UpdateMemberRolesRequest = v.InferInput<typeof updateMemberRolesRequestSchema>;

export const putMemberCapabilitiesRequestSchema = v.strictObject({
  enabled_keys: capabilityKeysSchema,
  version: v.pipe(v.number(), v.integer(), v.minValue(0), v.maxValue(2147483647)),
});

export type PutMemberCapabilitiesRequest = v.InferInput<typeof putMemberCapabilitiesRequestSchema>;

export async function getOrganization(options: IdentityClientOptions): Promise<Organization> {
  const client = createIdentityClient(options);
  const path = "/api/v1/organization";
  const result = await getGeneratedOrganization({
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwIdentityError(path, result.response, result.error);
  }

  return parseOrganization(result.data);
}

export async function getMembers(options: IdentityClientOptions): Promise<Array<Member>> {
  const client = createIdentityClient(options);
  const path = "/api/v1/organization/members";
  const result = await listGeneratedOrganizationMembers({
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwIdentityError(path, result.response, result.error);
  }

  return parseMembers(result.data);
}

export async function inviteMember(
  options: IdentityClientOptions & { body: InviteMemberRequest },
): Promise<InviteMemberResponse> {
  const client = createIdentityClient(options);
  const input = v.parse(inviteMemberRequestSchema, options.body);
  const bodyInput: {
    email: string;
    family_name?: string;
    given_name?: string;
    role_keys: Array<string>;
  } = {
    email: input.email,
    role_keys: input.roleKeys,
  };
  const familyName = omitEmptyText(input.familyName);
  if (familyName) {
    bodyInput.family_name = familyName;
  }
  const givenName = omitEmptyText(input.givenName);
  if (givenName) {
    bodyInput.given_name = givenName;
  }
  const parsedBody = v.parse(vIdentityInviteMemberRequestWritable, bodyInput);
  const body: IdentityInviteMemberRequestWritable = {
    email: parsedBody.email,
    role_keys: parsedBody.role_keys,
    ...(parsedBody.family_name ? { family_name: parsedBody.family_name } : {}),
    ...(parsedBody.given_name ? { given_name: parsedBody.given_name } : {}),
  };
  const path = "/api/v1/organization/members";
  const result = await inviteGeneratedOrganizationMember({
    body,
    client,
    headers: idempotencyHeaders("identity-member-invite"),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwIdentityError(path, result.response, result.error);
  }

  return v.parse(vIdentityInviteMemberResponse, result.data);
}

export async function updateMemberRoles(
  options: IdentityClientOptions & { body: UpdateMemberRolesRequest },
): Promise<Member> {
  const client = createIdentityClient(options);
  const input = v.parse(updateMemberRolesRequestSchema, options.body);
  const body = v.parse(vIdentityUpdateMemberRolesRequestWritable, {
    expected_org_acl_version: input.expectedOrgAclVersion,
    expected_role_keys: input.expectedRoleKeys,
    role_keys: input.roleKeys,
  });
  const path = `/api/v1/organization/members/${input.userId}/roles`;
  const result = await updateGeneratedOrganizationMemberRoles({
    body,
    client,
    headers: idempotencyHeaders("identity-member-roles"),
    path: { user_id: input.userId },
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwIdentityError(path, result.response, result.error);
  }

  return parseMember(result.data);
}

export async function getMemberCapabilities(
  options: IdentityClientOptions,
): Promise<MemberCapabilities> {
  const client = createIdentityClient(options);
  const path = "/api/v1/organization/member-capabilities";
  const result = await getGeneratedOrganizationMemberCapabilities({
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwIdentityError(path, result.response, result.error);
  }

  return parseMemberCapabilities(result.data);
}

export async function putMemberCapabilities(
  options: IdentityClientOptions & { body: PutMemberCapabilitiesRequest },
): Promise<MemberCapabilities> {
  const client = createIdentityClient(options);
  const input = v.parse(putMemberCapabilitiesRequestSchema, options.body);
  const body: IdentityPutMemberCapabilitiesRequestWritable = v.parse(
    vIdentityPutMemberCapabilitiesRequestWritable,
    {
      enabled_keys: input.enabled_keys,
      version: input.version,
    },
  );
  const path = "/api/v1/organization/member-capabilities";
  const result = await putGeneratedOrganizationMemberCapabilities({
    body,
    client,
    headers: idempotencyHeaders("identity-member-capabilities"),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwIdentityError(path, result.response, result.error);
  }

  return parseMemberCapabilities(result.data);
}
