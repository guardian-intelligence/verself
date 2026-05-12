import * as v from "valibot";
import { createClient, type Client } from "./__generated/iam-api/client/index.js";
import {
  getOrganization as getGeneratedOrganization,
  listMembers as listGeneratedMembers,
  listOrganizations as listGeneratedOrganizations,
  updateMemberRole as updateGeneratedMemberRole,
  updateOrganization as updateGeneratedOrganization,
} from "./__generated/iam-api/index.js";
import type {
  MemberSummary,
  UpdateMemberRoleInputBodyWritable,
  UpdateOrganizationInputBodyWritable,
} from "./__generated/iam-api/types.gen.js";
import {
  vListMembersOutputBody,
  vListOrganizationsOutputBody,
  vMemberSummary,
  vOrganizationSummary,
  vUpdateMemberRoleInputBodyWritable,
  vUpdateOrganizationInputBodyWritable,
} from "./__generated/iam-api/valibot.gen.js";
import type { BearerClientOptions } from "./service-api";
import {
  ServiceApiError,
  createBearerJSONHeaders,
  idempotencyHeaders,
  throwGeneratedServiceError,
} from "./service-api";

export type IAMClientOptions = BearerClientOptions;
export type IAMMutationOptions = {
  idempotencyKey?: string | undefined;
};

export class IAMApiError extends ServiceApiError {
  constructor(status: number, path: string, body: string) {
    super("IAM API", status, path, body);
    this.name = "IAMApiError";
  }
}

export function isIAMApiError(error: unknown): error is IAMApiError {
  return error instanceof IAMApiError;
}

function createIAMClient(options: IAMClientOptions): Client {
  return createClient({
    baseUrl: options.baseUrl,
    headers: createBearerJSONHeaders(options.accessToken, options.traceparent),
    ...(options.fetch ? { fetch: options.fetch } : {}),
  });
}

function throwIAMError(path: string, response: Response | undefined, error: unknown): never {
  throwGeneratedServiceError(IAMApiError, path, response, error);
}

function removeUndefined<T extends Record<string, unknown>>(input: T): Record<string, unknown> {
  return Object.fromEntries(Object.entries(input).filter(([, value]) => value !== undefined));
}

const organizationSlugSchema = v.pipe(
  v.string(),
  v.trim(),
  v.toLowerCase(),
  v.regex(/^[a-z0-9]+(?:-[a-z0-9]+)*$/),
  v.maxLength(80),
);

const organizationRoleSchema = v.picklist(["owner", "admin", "member"]);

function parseRole(value: string): "owner" | "admin" | "member" {
  return v.parse(organizationRoleSchema, value);
}

function rolePermissions(role: string): Array<string> {
  switch (parseRole(role)) {
    case "owner":
    case "admin":
      return [
        "iam:organization:list",
        "iam:organization:read",
        "iam:organization:update",
        "iam:member:list",
        "iam:member:read",
        "iam:member:update_role",
      ];
    case "member":
      return [
        "iam:organization:list",
        "iam:organization:read",
        "iam:member:list",
        "iam:member:read",
      ];
  }
}

function bigintToNumber(value: number | bigint): number {
  if (typeof value === "number") {
    return value;
  }
  const out = Number(value);
  if (!Number.isSafeInteger(out)) {
    throw new Error(`IAM integer exceeds JavaScript safe range: ${value.toString()}`);
  }
  return out;
}

export interface Member {
  readonly user_id: string;
  readonly email: string;
  readonly display_name: string;
  readonly state: string;
  readonly role_keys: ReadonlyArray<OrganizationRoleKey>;
}

export type OrganizationRoleKey = "owner" | "admin" | "member";

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

function parseMember(input: unknown): Member {
  const member = v.parse(vMemberSummary, input) as MemberSummary;
  const role = parseRole(member.role);
  return {
    user_id: member.memberId,
    email: member.email,
    display_name: member.displayName,
    state: "active",
    role_keys: [role],
  };
}

function parseOrganization(input: unknown): Organization {
  const organization = v.parse(vOrganizationSummary, input);
  const callerRole = parseRole(organization.callerRole);
  return {
    org_id: organization.orgId,
    display_name: organization.displayName,
    slug: organization.slug ?? "",
    version: bigintToNumber(organization.version),
    org_acl_version: bigintToNumber(organization.orgAclVersion),
    caller: {
      user_id: "",
      email: "",
      display_name: "",
      state: "active",
      role_keys: [callerRole],
    },
    permissions: rolePermissions(callerRole),
  };
}

function parseOrganizationMetadata(input: unknown): OrganizationMetadata {
  const organization = parseOrganization(input);
  return {
    org_id: organization.org_id,
    display_name: organization.display_name,
    slug: organization.slug,
  };
}

export const updateOrganizationRequestSchema = v.strictObject({
  display_name: v.optional(v.pipe(v.string(), v.trim(), v.minLength(1), v.maxLength(120))),
  slug: v.optional(organizationSlugSchema),
  version: v.pipe(v.number(), v.integer(), v.minValue(1), v.maxValue(2147483647)),
});

export type UpdateOrganizationRequest = v.InferInput<typeof updateOrganizationRequestSchema>;

export const updateMemberRolesRequestSchema = v.pipe(
  v.strictObject({
    expectedOrgAclVersion: v.pipe(v.number(), v.integer(), v.minValue(1), v.maxValue(2147483647)),
    expectedRoleKeys: v.pipe(v.array(organizationRoleSchema), v.minLength(1)),
    roleKeys: v.pipe(v.array(organizationRoleSchema), v.minLength(1)),
    userId: v.pipe(v.string(), v.trim(), v.regex(/^member_[0-9A-HJKMNP-TV-Z]{26}$/)),
  }),
);

export type UpdateMemberRolesRequest = v.InferInput<typeof updateMemberRolesRequestSchema>;

export class IAM {
  readonly #options: IAMClientOptions;

  constructor(options: IAMClientOptions) {
    this.#options = options;
  }

  async getOrganization(): Promise<Organization> {
    const client = createIAMClient(this.#options);
    const org = await this.currentOrganization(client);
    const path = `/api/v1/orgs/${org.org_id}`;
    const result = await getGeneratedOrganization({
      client,
      path: { orgId: org.org_id },
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwIAMError(path, result.response, result.error);
    }
    return parseOrganization(result.data);
  }

  async listMyOrganizations(): Promise<Array<OrganizationMetadata>> {
    const client = createIAMClient(this.#options);
    const path = "/api/v1/orgs";
    const result = await listGeneratedOrganizations({
      client,
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwIAMError(path, result.response, result.error);
    }
    const body = v.parse(vListOrganizationsOutputBody, result.data);
    return body.organizations?.map((organization) => parseOrganizationMetadata(organization)) ?? [];
  }

  async updateOrganization(
    body: UpdateOrganizationRequest,
    options: IAMMutationOptions = {},
  ): Promise<Organization> {
    const client = createIAMClient(this.#options);
    const org = await this.currentOrganization(client);
    const generatedBody = v.parse(vUpdateOrganizationInputBodyWritable, {
      displayName: body.display_name,
      slug: body.slug,
      version: body.version,
    });
    const parsedBody = removeUndefined({
      displayName: generatedBody.displayName,
      slug: generatedBody.slug,
      version: bigintToNumber(generatedBody.version),
    }) as UpdateOrganizationInputBodyWritable;
    const path = `/api/v1/orgs/${org.org_id}`;
    const result = await updateGeneratedOrganization({
      body: parsedBody,
      client,
      headers: idempotencyHeaders("iam-organization", options.idempotencyKey),
      path: { orgId: org.org_id },
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwIAMError(path, result.response, result.error);
    }
    return parseOrganization(result.data);
  }

  async listMembers(): Promise<Array<Member>> {
    const client = createIAMClient(this.#options);
    const org = await this.currentOrganization(client);
    const path = `/api/v1/orgs/${org.org_id}/members`;
    const result = await listGeneratedMembers({
      client,
      path: { orgId: org.org_id },
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwIAMError(path, result.response, result.error);
    }
    const body = v.parse(vListMembersOutputBody, result.data);
    return body.members?.map((member) => parseMember(member)) ?? [];
  }

  async updateMemberRoles(
    body: UpdateMemberRolesRequest,
    options: IAMMutationOptions = {},
  ): Promise<Member> {
    const client = createIAMClient(this.#options);
    const org = await this.currentOrganization(client);
    const input = v.parse(updateMemberRolesRequestSchema, body);
    const generatedBody = v.parse(vUpdateMemberRoleInputBodyWritable, {
      expectedOrgAclVersion: input.expectedOrgAclVersion,
      expectedRole: input.expectedRoleKeys[0],
      role: input.roleKeys[0],
    });
    const parsedBody: UpdateMemberRoleInputBodyWritable = {
      expectedOrgAclVersion: bigintToNumber(generatedBody.expectedOrgAclVersion),
      expectedRole: generatedBody.expectedRole,
      role: generatedBody.role,
    };
    const path = `/api/v1/orgs/${org.org_id}/members/${input.userId}/role`;
    const result = await updateGeneratedMemberRole({
      body: parsedBody,
      client,
      headers: idempotencyHeaders("iam-member-role", options.idempotencyKey),
      path: { orgId: org.org_id, memberId: input.userId },
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwIAMError(path, result.response, result.error);
    }
    return parseMember(result.data);
  }

  async currentOrganization(client = createIAMClient(this.#options)): Promise<Organization> {
    const path = "/api/v1/orgs";
    const result = await listGeneratedOrganizations({
      client,
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwIAMError(path, result.response, result.error);
    }
    const body = v.parse(vListOrganizationsOutputBody, result.data);
    const organizations = body.organizations ?? [];
    if (organizations.length !== 1) {
      throw new Error(`IAM selected-organization token returned ${organizations.length} organizations`);
    }
    return parseOrganization(organizations[0]);
  }
}
