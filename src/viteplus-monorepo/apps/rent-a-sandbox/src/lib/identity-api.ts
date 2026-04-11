import * as v from "valibot";
import { createClient, type Client } from "../__generated/identity-api/client/index.js";
import {
  getOrganization as getGeneratedOrganization,
  getOrganizationPolicy as getGeneratedOrganizationPolicy,
  inviteOrganizationMember as inviteGeneratedOrganizationMember,
  listOrganizationMembers as listGeneratedOrganizationMembers,
  listOrganizationOperations as listGeneratedOrganizationOperations,
  putOrganizationPolicy as putGeneratedOrganizationPolicy,
  updateOrganizationMemberRoles as updateGeneratedOrganizationMemberRoles,
} from "../__generated/identity-api/index.js";
import type {
  IdentityInviteMemberRequestWritable,
  IdentityPutPolicyRequestWritable,
} from "../__generated/identity-api/types.gen.js";
import {
  vIdentityInviteMemberRequestWritable,
  vIdentityInviteMemberResponse,
  vIdentityMember,
  vIdentityMembers,
  vIdentityOperation,
  vIdentityOperations,
  vIdentityOrganization,
  vIdentityPolicyDocument,
  vIdentityPolicyRole,
  vIdentityPutPolicyRequestWritable,
  vIdentityServiceOperations,
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

function parsePolicyRole(input: unknown) {
  const role = v.parse(vIdentityPolicyRole, input);
  return {
    ...role,
    permissions: role.permissions ?? [],
  };
}

export type PolicyRole = ReturnType<typeof parsePolicyRole>;

function parsePolicyDocument(input: unknown) {
  const { $schema: _schema, roles, ...policy } = v.parse(vIdentityPolicyDocument, input);
  return {
    ...policy,
    roles: roles?.map((role) => parsePolicyRole(role)) ?? [],
  };
}

export type PolicyDocument = ReturnType<typeof parsePolicyDocument>;

function parseOperation(input: unknown) {
  return v.parse(vIdentityOperation, input);
}

export type Operation = ReturnType<typeof parseOperation>;

function parseServiceOperations(input: unknown) {
  const service = v.parse(vIdentityServiceOperations, input);
  return {
    ...service,
    operations: service.operations?.map((operation) => parseOperation(operation)) ?? [],
  };
}

export type ServiceOperations = ReturnType<typeof parseServiceOperations>;

function parseOperations(input: unknown) {
  const { $schema: _schema, services } = v.parse(vIdentityOperations, input);
  return {
    services: services?.map((service) => parseServiceOperations(service)) ?? [],
  };
}

export type Operations = ReturnType<typeof parseOperations>;

function parseOrganization(input: unknown) {
  const {
    $schema: _schema,
    caller,
    permissions,
    policy,
    ...organization
  } = v.parse(vIdentityOrganization, input);
  return {
    ...organization,
    caller: parseMember(caller),
    permissions: permissions ?? [],
    policy: parsePolicyDocument(policy),
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
    roleKeys: roleKeysSchema,
    userId: v.pipe(v.string(), v.trim(), v.minLength(1)),
  }),
  v.transform((input) => ({
    roleKeys: input.roleKeys,
    userId: input.userId,
  })),
);

export type UpdateMemberRolesRequest = v.InferInput<typeof updateMemberRolesRequestSchema>;

export const putPolicyRequestSchema = v.strictObject({
  roles: v.array(
    v.strictObject({
      display_name: v.pipe(v.string(), v.maxLength(200)),
      permissions: v.pipe(v.array(v.string()), v.minLength(1), v.maxLength(256)),
      role_key: v.pipe(v.string(), v.maxLength(100)),
    }),
  ),
  version: v.pipe(v.number(), v.integer(), v.minValue(0), v.maxValue(2147483647)),
});

export type PutPolicyRequest = v.InferInput<typeof putPolicyRequestSchema>;

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

export async function getOperations(options: IdentityClientOptions): Promise<Operations> {
  const client = createIdentityClient(options);
  const path = "/api/v1/organization/operations";
  const result = await listGeneratedOrganizationOperations({
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwIdentityError(path, result.response, result.error);
  }

  return parseOperations(result.data);
}

export async function getPolicy(options: IdentityClientOptions): Promise<PolicyDocument> {
  const client = createIdentityClient(options);
  const path = "/api/v1/organization/policy";
  const result = await getGeneratedOrganizationPolicy({
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwIdentityError(path, result.response, result.error);
  }

  return parsePolicyDocument(result.data);
}

export async function putPolicy(
  options: IdentityClientOptions & { body: PutPolicyRequest },
): Promise<PolicyDocument> {
  const client = createIdentityClient(options);
  const input = v.parse(putPolicyRequestSchema, options.body);
  const body: IdentityPutPolicyRequestWritable = v.parse(vIdentityPutPolicyRequestWritable, {
    roles: input.roles,
    version: input.version,
  });
  const path = "/api/v1/organization/policy";
  const result = await putGeneratedOrganizationPolicy({
    body,
    client,
    headers: idempotencyHeaders("identity-policy"),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwIdentityError(path, result.response, result.error);
  }

  return parsePolicyDocument(result.data);
}
