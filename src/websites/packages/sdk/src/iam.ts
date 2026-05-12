import * as v from "valibot";
import type {
  IAMTransport,
  ListMembersOutputBody,
  ListOrganizationsOutputBody,
  MemberSummary,
  OrganizationSummary,
  UpdateMemberRoleInputBody,
  UpdateOrganizationInputBody,
} from "./__generated/iam-transport/client.gen.js";
import { createIAMTransport } from "./__generated/iam-transport/client.gen.js";
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

function createIAMClient(options: IAMClientOptions): IAMTransport {
  return createIAMTransport({
    baseUrl: options.baseUrl,
    headers: createBearerJSONHeaders(options.accessToken, options.traceparent),
    ...(options.fetch ? { fetch: options.fetch } : {}),
  });
}

function throwIAMError(path: string, response: Response | undefined, error: unknown): never {
  throwGeneratedServiceError(IAMApiError, path, response, error);
}

function unwrapIAMResult<T>(
  path: string,
  result: {
    readonly response: Response;
    readonly bodyText: string;
    readonly data?: T;
    readonly error?: unknown;
  },
): T {
  if (result.error !== undefined || result.data === undefined) {
    throwIAMError(path, result.response, result.error ?? result.bodyText);
  }
  return result.data;
}

const organizationSlugSchema = v.pipe(
  v.string(),
  v.trim(),
  v.toLowerCase(),
  v.regex(/^[a-z0-9]+(?:-[a-z0-9]+)*$/),
  v.maxLength(80),
);

const organizationRoleSchema = v.picklist(["owner", "admin", "member"]);
const organizationSummarySchema = v.strictObject({
  orgId: v.string(),
  resourceName: v.string(),
  slug: v.optional(v.string()),
  displayName: v.string(),
  callerRole: organizationRoleSchema,
  version: v.number(),
  orgAclVersion: v.number(),
});
const memberSummarySchema = v.strictObject({
  orgId: v.string(),
  memberId: v.string(),
  resourceName: v.string(),
  email: v.string(),
  displayName: v.string(),
  role: organizationRoleSchema,
});
const listOrganizationsOutputBodySchema = v.strictObject({
  organizations: v.array(organizationSummarySchema),
  nextPageToken: v.optional(v.string()),
});
const listMembersOutputBodySchema = v.strictObject({
  members: v.array(memberSummarySchema),
  nextPageToken: v.optional(v.string()),
});

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

function parseMember(input: MemberSummary): Member {
  const member = v.parse(memberSummarySchema, input) as MemberSummary;
  const role = parseRole(member.role);
  return {
    user_id: member.memberId,
    email: member.email,
    display_name: member.displayName,
    state: "active",
    role_keys: [role],
  };
}

function parseOrganization(input: OrganizationSummary): Organization {
  const organization = v.parse(organizationSummarySchema, input) as OrganizationSummary;
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
  const organization = parseOrganization(input as OrganizationSummary);
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
    const result = await client.getOrganization({ orgId: org.org_id });
    return parseOrganization(unwrapIAMResult(path, result));
  }

  async listMyOrganizations(): Promise<Array<OrganizationMetadata>> {
    const client = createIAMClient(this.#options);
    const path = "/api/v1/orgs";
    const result = await client.listOrganizations();
    const body = v.parse(
      listOrganizationsOutputBodySchema,
      unwrapIAMResult(path, result),
    ) as ListOrganizationsOutputBody;
    return body.organizations.map((organization) => parseOrganizationMetadata(organization));
  }

  async updateOrganization(
    body: UpdateOrganizationRequest,
    options: IAMMutationOptions = {},
  ): Promise<Organization> {
    const client = createIAMClient(this.#options);
    const org = await this.currentOrganization(client);
    const input = v.parse(updateOrganizationRequestSchema, body);
    const parsedBody: UpdateOrganizationInputBody = {
      ...(input.display_name !== undefined ? { displayName: input.display_name } : {}),
      ...(input.slug !== undefined ? { slug: input.slug } : {}),
      version: input.version,
    };
    const path = `/api/v1/orgs/${org.org_id}`;
    const result = await client.updateOrganization({
      body: parsedBody,
      idempotencyKey: idempotencyHeaders("iam-organization", options.idempotencyKey)[
        "Idempotency-Key"
      ],
      orgId: org.org_id,
    });
    return parseOrganization(unwrapIAMResult(path, result));
  }

  async listMembers(): Promise<Array<Member>> {
    const client = createIAMClient(this.#options);
    const org = await this.currentOrganization(client);
    const path = `/api/v1/orgs/${org.org_id}/members`;
    const result = await client.listMembers({ orgId: org.org_id });
    const body = v.parse(
      listMembersOutputBodySchema,
      unwrapIAMResult(path, result),
    ) as ListMembersOutputBody;
    return body.members.map((member) => parseMember(member));
  }

  async updateMemberRoles(
    body: UpdateMemberRolesRequest,
    options: IAMMutationOptions = {},
  ): Promise<Member> {
    const client = createIAMClient(this.#options);
    const org = await this.currentOrganization(client);
    const input = v.parse(updateMemberRolesRequestSchema, body);
    const expectedRole = input.expectedRoleKeys[0];
    const role = input.roleKeys[0];
    if (expectedRole === undefined || role === undefined) {
      throw new Error("IAM member role update requires current and desired roles");
    }
    const parsedBody: UpdateMemberRoleInputBody = {
      expectedOrgAclVersion: input.expectedOrgAclVersion,
      expectedRole,
      role,
    };
    const path = `/api/v1/orgs/${org.org_id}/members/${input.userId}/role`;
    const result = await client.updateMemberRole({
      body: parsedBody,
      idempotencyKey: idempotencyHeaders("iam-member-role", options.idempotencyKey)[
        "Idempotency-Key"
      ],
      memberId: input.userId,
      orgId: org.org_id,
    });
    return parseMember(unwrapIAMResult(path, result));
  }

  async currentOrganization(client = createIAMClient(this.#options)): Promise<Organization> {
    const path = "/api/v1/orgs";
    const result = await client.listOrganizations();
    const body = v.parse(
      listOrganizationsOutputBodySchema,
      unwrapIAMResult(path, result),
    ) as ListOrganizationsOutputBody;
    const organizations = body.organizations;
    if (organizations.length !== 1) {
      throw new Error(
        `IAM selected-organization token returned ${organizations.length} organizations`,
      );
    }
    const organization = organizations[0];
    if (organization === undefined) {
      throw new Error("IAM selected-organization token returned no organization");
    }
    return parseOrganization(organization);
  }
}
