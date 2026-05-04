import * as v from "valibot";
import { createClient, type Client } from "../__generated/iam-api/client/index.js";
import {
  getOrganization as getGeneratedOrganization,
  getOrganizationMemberCapabilities as getGeneratedOrganizationMemberCapabilities,
  inviteOrganizationMember as inviteGeneratedOrganizationMember,
  listMyOrganizations as listGeneratedMyOrganizations,
  listOrganizationMembers as listGeneratedOrganizationMembers,
  patchOrganization as patchGeneratedOrganization,
  putOrganizationMemberCapabilities as putGeneratedOrganizationMemberCapabilities,
  updateOrganizationMemberRoles as updateGeneratedOrganizationMemberRoles,
} from "../__generated/iam-api/index.js";
import type {
  IamInviteMemberRequestWritable as IAMInviteMemberRequestWritable,
  IamPutMemberCapabilitiesRequestWritable as IAMPutMemberCapabilitiesRequestWritable,
  IamUpdateOrganizationRequestWritable as IAMUpdateOrganizationRequestWritable,
} from "../__generated/iam-api/types.gen.js";
import {
  vIamInviteMemberRequestWritable as vIAMInviteMemberRequestWritable,
  vIamInviteMemberResponse as vIAMInviteMemberResponse,
  vIamMember as vIAMMember,
  vIamMemberCapabilities as vIAMMemberCapabilities,
  vIamMemberCapabilitiesDocument as vIAMMemberCapabilitiesDocument,
  vIamMemberCapability as vIAMMemberCapability,
  vIamMembers as vIAMMembers,
  vIamOrganization as vIAMOrganization,
  vIamOrganizationMetadata as vIAMOrganizationMetadata,
  vIamPutMemberCapabilitiesRequestWritable as vIAMPutMemberCapabilitiesRequestWritable,
  vIamUpdateMemberRolesRequestWritable as vIAMUpdateMemberRolesRequestWritable,
  vIamUpdateOrganizationRequestWritable as vIAMUpdateOrganizationRequestWritable,
} from "../__generated/iam-api/valibot.gen.js";
import {
  type BearerClientOptions,
  ServiceApiError,
  createBearerJSONHeaders,
  idempotencyHeaders,
  throwGeneratedServiceError,
} from "./service-api";

export interface IAMClientOptions extends BearerClientOptions {}

export class IAMApiError extends ServiceApiError {
  constructor(status: number, path: string, body: string) {
    super("IAM API", status, path, body);
    this.name = "IAMApiError";
  }
}

export function isIAMApiError(error: unknown): error is IAMApiError {
  return error instanceof IAMApiError;
}

function throwIAMError(path: string, response: Response | undefined, error: unknown): never {
  throwGeneratedServiceError(IAMApiError, path, response, error);
}

function createIAMClient(options: IAMClientOptions): Client {
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
  const { $schema: _schema, role_keys, ...member } = v.parse(vIAMMember, input);
  return {
    ...member,
    role_keys: role_keys ?? [],
  };
}

export type Member = ReturnType<typeof parseMember>;

function parseMemberCapabilitiesDocument(input: unknown) {
  const { enabled_keys, ...doc } = v.parse(vIAMMemberCapabilitiesDocument, input);
  return {
    ...doc,
    enabled_keys: enabled_keys ?? [],
  };
}

export type MemberCapabilitiesDocument = ReturnType<typeof parseMemberCapabilitiesDocument>;

function parseMemberCapability(input: unknown) {
  const capability = v.parse(vIAMMemberCapability, input);
  return {
    ...capability,
    permissions: capability.permissions ?? [],
  };
}

export type MemberCapability = ReturnType<typeof parseMemberCapability>;

function parseMemberCapabilities(input: unknown) {
  const { $schema: _schema, document, catalog } = v.parse(vIAMMemberCapabilities, input);
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
  } = v.parse(vIAMOrganization, input);
  return {
    ...organization,
    caller: parseMember(caller),
    permissions: permissions ?? [],
    member_capabilities: parseMemberCapabilitiesDocument(member_capabilities),
  };
}

export type Organization = ReturnType<typeof parseOrganization>;

function parseOrganizationMetadata(input: unknown) {
  return v.parse(vIAMOrganizationMetadata, input);
}

export type OrganizationMetadata = ReturnType<typeof parseOrganizationMetadata>;

const organizationSlugSchema = v.pipe(
  v.string(),
  v.trim(),
  v.toLowerCase(),
  v.regex(/^[a-z0-9]+(?:-[a-z0-9]+)*$/),
  v.maxLength(80),
);

export const updateOrganizationRequestSchema = v.strictObject({
  display_name: v.optional(v.pipe(v.string(), v.trim(), v.minLength(1), v.maxLength(120))),
  slug: v.optional(organizationSlugSchema),
  version: v.pipe(v.number(), v.integer(), v.minValue(1), v.maxValue(2147483647)),
});

export type UpdateOrganizationRequest = v.InferInput<typeof updateOrganizationRequestSchema>;

function parseMembers(input: unknown): Array<Member> {
  const { $schema: _schema, members } = v.parse(vIAMMembers, input);
  return members?.map((member) => parseMember(member)) ?? [];
}

export const inviteMemberRequestSchema = v.strictObject({
  email: v.pipe(v.string(), v.trim(), v.email()),
  familyName: v.optional(v.pipe(v.string(), v.maxLength(100))),
  givenName: v.optional(v.pipe(v.string(), v.maxLength(100))),
  roleKeys: roleKeysSchema,
});

export type InviteMemberRequest = v.InferInput<typeof inviteMemberRequestSchema>;
export type InviteMemberResponse = v.InferOutput<typeof vIAMInviteMemberResponse>;

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

export async function getOrganization(options: IAMClientOptions): Promise<Organization> {
  const client = createIAMClient(options);
  const path = "/api/v1/organization";
  const result = await getGeneratedOrganization({
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwIAMError(path, result.response, result.error);
  }

  return parseOrganization(result.data);
}

export async function listMyOrganizations(
  options: IAMClientOptions,
): Promise<Array<OrganizationMetadata>> {
  const client = createIAMClient(options);
  const path = "/api/v1/me/organizations";
  const result = await listGeneratedMyOrganizations({
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwIAMError(path, result.response, result.error);
  }

  return result.data?.map((organization) => parseOrganizationMetadata(organization)) ?? [];
}

export async function updateOrganization(
  options: IAMClientOptions & { body: UpdateOrganizationRequest },
): Promise<Organization> {
  const client = createIAMClient(options);
  const input = v.parse(updateOrganizationRequestSchema, options.body);
  const parsedBody = v.parse(vIAMUpdateOrganizationRequestWritable, input);
  const body: IAMUpdateOrganizationRequestWritable = {
    version: parsedBody.version,
    ...(parsedBody.display_name !== undefined ? { display_name: parsedBody.display_name } : {}),
    ...(parsedBody.slug !== undefined ? { slug: parsedBody.slug } : {}),
  };
  const path = "/api/v1/organization";
  const result = await patchGeneratedOrganization({
    body,
    client,
    headers: idempotencyHeaders("iam-organization"),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwIAMError(path, result.response, result.error);
  }

  return parseOrganization(result.data);
}

export async function getMembers(options: IAMClientOptions): Promise<Array<Member>> {
  const client = createIAMClient(options);
  const path = "/api/v1/organization/members";
  const result = await listGeneratedOrganizationMembers({
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwIAMError(path, result.response, result.error);
  }

  return parseMembers(result.data);
}

export async function inviteMember(
  options: IAMClientOptions & { body: InviteMemberRequest },
): Promise<InviteMemberResponse> {
  const client = createIAMClient(options);
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
  const parsedBody = v.parse(vIAMInviteMemberRequestWritable, bodyInput);
  const body: IAMInviteMemberRequestWritable = {
    email: parsedBody.email,
    role_keys: parsedBody.role_keys,
    ...(parsedBody.family_name ? { family_name: parsedBody.family_name } : {}),
    ...(parsedBody.given_name ? { given_name: parsedBody.given_name } : {}),
  };
  const path = "/api/v1/organization/members";
  const result = await inviteGeneratedOrganizationMember({
    body,
    client,
    headers: idempotencyHeaders("iam-member-invite"),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwIAMError(path, result.response, result.error);
  }

  return v.parse(vIAMInviteMemberResponse, result.data);
}

export async function updateMemberRoles(
  options: IAMClientOptions & { body: UpdateMemberRolesRequest },
): Promise<Member> {
  const client = createIAMClient(options);
  const input = v.parse(updateMemberRolesRequestSchema, options.body);
  const body = v.parse(vIAMUpdateMemberRolesRequestWritable, {
    expected_org_acl_version: input.expectedOrgAclVersion,
    expected_role_keys: input.expectedRoleKeys,
    role_keys: input.roleKeys,
  });
  const path = `/api/v1/organization/members/${input.userId}/roles`;
  const result = await updateGeneratedOrganizationMemberRoles({
    body,
    client,
    headers: idempotencyHeaders("iam-member-roles"),
    path: { user_id: input.userId },
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwIAMError(path, result.response, result.error);
  }

  return parseMember(result.data);
}

export async function getMemberCapabilities(
  options: IAMClientOptions,
): Promise<MemberCapabilities> {
  const client = createIAMClient(options);
  const path = "/api/v1/organization/member-capabilities";
  const result = await getGeneratedOrganizationMemberCapabilities({
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwIAMError(path, result.response, result.error);
  }

  return parseMemberCapabilities(result.data);
}

export async function putMemberCapabilities(
  options: IAMClientOptions & { body: PutMemberCapabilitiesRequest },
): Promise<MemberCapabilities> {
  const client = createIAMClient(options);
  const input = v.parse(putMemberCapabilitiesRequestSchema, options.body);
  const body: IAMPutMemberCapabilitiesRequestWritable = v.parse(
    vIAMPutMemberCapabilitiesRequestWritable,
    {
      enabled_keys: input.enabled_keys,
      version: input.version,
    },
  );
  const path = "/api/v1/organization/member-capabilities";
  const result = await putGeneratedOrganizationMemberCapabilities({
    body,
    client,
    headers: idempotencyHeaders("iam-member-capabilities"),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwIAMError(path, result.response, result.error);
  }

  return parseMemberCapabilities(result.data);
}
