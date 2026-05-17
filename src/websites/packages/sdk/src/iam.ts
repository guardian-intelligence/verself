import * as v from "valibot";
import { createClient, type Client } from "./__generated/iam-api/client/index.js";
import type {
  IamPolicy as GeneratedIamPolicy,
  CreateOrganizationData,
  CreateOrganizationRequestContent as CreateOrganizationInputBody,
  InviteMemberData,
  InviteMemberRequestContent as InviteMemberInputBody,
  ListMembersResponseContent as ListMembersOutputBody,
  ListOrganizationsResponseContent as ListOrganizationsOutputBody,
  MemberInvitationSummary,
  MemberSummary,
  OrganizationSummary,
  SetIamPolicyData,
  SetIamPolicyRequestContent,
  TestIamPermissionsData,
  TestIamPermissionsRequestContent,
  TestIamPermissionsResponseContent,
  UpdateOrganizationData,
  UpdateOrganizationRequestContent as UpdateOrganizationInputBody,
} from "./__generated/iam-api/index.js";
import {
  createOrganization as createOrganizationOperation,
  getIamPolicy,
  getOrganization,
  inviteMember as inviteMemberOperation,
  listMembers,
  listOrganizations,
  setIamPolicy,
  testIamPermissions,
  updateOrganization,
} from "./__generated/iam-api/index.js";
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

function unwrapIAMResult<T>(
  path: string,
  result: {
    readonly response: Response;
    readonly data?: T;
    readonly error?: unknown;
  },
): NonNullable<T> {
  if (result.error !== undefined || result.data === undefined) {
    throwIAMError(path, result.response, result.error ?? "empty response body");
  }
  return result.data as NonNullable<T>;
}

const organizationSlugSchema = v.pipe(
  v.string(),
  v.trim(),
  v.toLowerCase(),
  v.regex(/^[a-z0-9]+(?:-[a-z0-9]+)*$/),
  v.maxLength(80),
);

const permissionSchema = v.pipe(
  v.string(),
  v.trim(),
  v.regex(/^[a-z][a-z0-9_-]*(?::[a-z][a-z0-9_-]*)+$/),
);

const iamMemberSchema = v.pipe(
  v.string(),
  v.trim(),
  v.regex(
    /^(user|serviceAccount|workload):.+$|^principalSet:\/\/iam\.verself\.sh\/organizations\/org_[0-9A-HJKMNP-TV-Z]{26}\/roles\/[A-Za-z][A-Za-z0-9.]*$/,
  ),
);

const iamRoleSchema = v.pipe(v.string(), v.trim(), v.regex(/^roles\/[A-Za-z][A-Za-z0-9.]*$/));

const displayNameSchema = v.pipe(v.string(), v.trim(), v.minLength(1), v.maxLength(120));

const emailSchema = v.pipe(
  v.string(),
  v.trim(),
  v.regex(/^[^\s@]+@[^\s@]+\.[^\s@]+$/),
  v.maxLength(320),
);

const iamPolicyBindingSchema = v.strictObject({
  role: iamRoleSchema,
  members: v.array(iamMemberSchema),
});

export const iamPolicySchema = v.strictObject({
  version: v.pipe(v.number(), v.integer(), v.minValue(1), v.maxValue(2147483647)),
  bindings: v.array(iamPolicyBindingSchema),
  etag: v.optional(v.pipe(v.string(), v.trim(), v.minLength(1), v.maxLength(256))),
});

const organizationSummarySchema = v.strictObject({
  orgId: v.string(),
  resourceName: v.string(),
  slug: v.optional(v.string()),
  displayName: v.string(),
  version: v.number(),
});
const memberSummarySchema = v.strictObject({
  orgId: v.string(),
  memberId: v.string(),
  resourceName: v.string(),
  email: v.string(),
  displayName: v.string(),
});
const memberInvitationSummarySchema = v.strictObject({
  orgId: v.string(),
  memberId: v.string(),
  resourceName: v.string(),
  email: v.string(),
  status: v.string(),
  roles: v.array(iamRoleSchema),
});
const listOrganizationsOutputBodySchema = v.strictObject({
  organizations: v.array(organizationSummarySchema),
  nextPageToken: v.optional(v.string()),
});
const listMembersOutputBodySchema = v.strictObject({
  members: v.array(memberSummarySchema),
  nextPageToken: v.optional(v.string()),
});

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

export interface IAMPolicy {
  readonly version: number;
  readonly bindings: ReadonlyArray<IAMPolicyBinding>;
  readonly etag?: string;
}

export interface IAMPolicyBinding {
  readonly role: string;
  readonly members: ReadonlyArray<string>;
}

function parseMember(input: MemberSummary): Member {
  const member = v.parse(memberSummarySchema, input) as MemberSummary;
  return {
    user_id: member.memberId,
    email: member.email,
    display_name: member.displayName,
    state: "active",
  };
}

function parseMemberInvitation(input: MemberInvitationSummary): MemberInvitation {
  const invitation = v.parse(memberInvitationSummarySchema, input) as MemberInvitationSummary;
  return {
    org_id: invitation.orgId,
    member_id: invitation.memberId,
    email: invitation.email,
    status: invitation.status,
    roles: [...invitation.roles],
  };
}

function parseOrganization(
  input: OrganizationSummary,
  permissions: ReadonlyArray<string> = [],
): Organization {
  const organization = v.parse(organizationSummarySchema, input) as OrganizationSummary;
  return {
    org_id: organization.orgId,
    display_name: organization.displayName,
    slug: organization.slug ?? "",
    version: bigintToNumber(organization.version),
    permissions,
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

function parsePolicy(input: GeneratedIamPolicy): IAMPolicy {
  const policy = v.parse(iamPolicySchema, input) as IAMPolicy;
  return {
    version: policy.version,
    bindings: policy.bindings.map((binding) => ({
      role: binding.role,
      members: [...binding.members],
    })),
    ...(policy.etag ? { etag: policy.etag } : {}),
  };
}

export const createOrganizationRequestSchema = v.strictObject({
  display_name: displayNameSchema,
  slug: v.optional(organizationSlugSchema),
});

export type CreateOrganizationRequest = v.InferInput<typeof createOrganizationRequestSchema>;

export const updateOrganizationRequestSchema = v.strictObject({
  display_name: v.optional(displayNameSchema),
  slug: v.optional(organizationSlugSchema),
  version: v.pipe(v.number(), v.integer(), v.minValue(1), v.maxValue(2147483647)),
});

export type UpdateOrganizationRequest = v.InferInput<typeof updateOrganizationRequestSchema>;

export const setIamPolicyRequestSchema = v.strictObject({
  policy: iamPolicySchema,
});

export type SetIamPolicyRequest = v.InferInput<typeof setIamPolicyRequestSchema>;

export const testIamPermissionsRequestSchema = v.strictObject({
  permissions: v.array(permissionSchema),
});

export type TestIamPermissionsRequest = v.InferInput<typeof testIamPermissionsRequestSchema>;

export const inviteMemberRequestSchema = v.strictObject({
  email: emailSchema,
  given_name: v.optional(displayNameSchema),
  family_name: v.optional(displayNameSchema),
  roles: v.optional(v.array(iamRoleSchema)),
});

export type InviteMemberRequest = v.InferInput<typeof inviteMemberRequestSchema>;

const ORGANIZATION_PAGE_PERMISSIONS = [
  "iam:organization:update",
  "iam:member:invite",
  "iam:member:list",
  "iam:policy:get",
] as const;

export class IAM {
  readonly #options: IAMClientOptions;

  constructor(options: IAMClientOptions) {
    this.#options = options;
  }

  async createOrganization(
    body: CreateOrganizationRequest,
    options: IAMMutationOptions = {},
  ): Promise<Organization> {
    const client = createIAMClient(this.#options);
    const input = v.parse(createOrganizationRequestSchema, body);
    const parsedBody: CreateOrganizationInputBody = {
      displayName: input.display_name,
      ...(input.slug !== undefined ? { slug: input.slug } : {}),
    };
    const path = "/api/v1/orgs";
    const result = await createOrganizationOperation({
      client,
      body: parsedBody as CreateOrganizationData["body"],
      headers: idempotencyHeaders("iam-organization", options.idempotencyKey),
      responseStyle: "fields",
      throwOnError: false,
    });
    return parseOrganization(unwrapIAMResult(path, result));
  }

  async getOrganization(): Promise<Organization> {
    const client = createIAMClient(this.#options);
    const org = await this.currentOrganization(client);
    const path = `/api/v1/orgs/${org.org_id}`;
    const result = await getOrganization({
      client,
      path: { orgId: org.org_id },
      responseStyle: "fields",
      throwOnError: false,
    });
    const organization = parseOrganization(unwrapIAMResult(path, result));
    const permissions = await this.testPermissionsForOrg(
      client,
      organization.org_id,
      ORGANIZATION_PAGE_PERMISSIONS,
    );
    return { ...organization, permissions };
  }

  async listMyOrganizations(): Promise<Array<OrganizationMetadata>> {
    const client = createIAMClient(this.#options);
    const path = "/api/v1/orgs";
    const result = await listOrganizations({
      client,
      responseStyle: "fields",
      throwOnError: false,
    });
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
    const result = await updateOrganization({
      client,
      body: parsedBody as UpdateOrganizationData["body"],
      headers: idempotencyHeaders("iam-organization", options.idempotencyKey),
      path: { orgId: org.org_id },
      responseStyle: "fields",
      throwOnError: false,
    });
    const organization = parseOrganization(unwrapIAMResult(path, result));
    const permissions = await this.testPermissionsForOrg(
      client,
      organization.org_id,
      ORGANIZATION_PAGE_PERMISSIONS,
    );
    return { ...organization, permissions };
  }

  async listMembers(): Promise<Array<Member>> {
    const client = createIAMClient(this.#options);
    const org = await this.currentOrganization(client);
    const path = `/api/v1/orgs/${org.org_id}/members`;
    const result = await listMembers({
      client,
      path: { orgId: org.org_id },
      responseStyle: "fields",
      throwOnError: false,
    });
    const body = v.parse(
      listMembersOutputBodySchema,
      unwrapIAMResult(path, result),
    ) as ListMembersOutputBody;
    return body.members.map((member) => parseMember(member));
  }

  async inviteMember(
    body: InviteMemberRequest,
    options: IAMMutationOptions = {},
  ): Promise<MemberInvitation> {
    const client = createIAMClient(this.#options);
    const org = await this.currentOrganization(client);
    const input = v.parse(inviteMemberRequestSchema, body);
    const parsedBody: InviteMemberInputBody = {
      email: input.email,
      ...(input.given_name !== undefined ? { givenName: input.given_name } : {}),
      ...(input.family_name !== undefined ? { familyName: input.family_name } : {}),
      ...(input.roles !== undefined ? { roles: [...input.roles] } : {}),
    };
    const path = `/api/v1/orgs/${org.org_id}/members:invite`;
    const result = await inviteMemberOperation({
      client,
      body: parsedBody as InviteMemberData["body"],
      headers: idempotencyHeaders("iam-member-invite", options.idempotencyKey),
      path: { orgId: org.org_id },
      responseStyle: "fields",
      throwOnError: false,
    });
    return parseMemberInvitation(unwrapIAMResult(path, result));
  }

  async getIamPolicy(): Promise<IAMPolicy> {
    const client = createIAMClient(this.#options);
    const org = await this.currentOrganization(client);
    const path = `/api/v1/orgs/${org.org_id}/iamPolicy:get`;
    const result = await getIamPolicy({
      client,
      path: { orgId: org.org_id },
      responseStyle: "fields",
      throwOnError: false,
    });
    return parsePolicy(unwrapIAMResult(path, result));
  }

  async setIamPolicy(
    body: SetIamPolicyRequest,
    options: IAMMutationOptions = {},
  ): Promise<IAMPolicy> {
    const client = createIAMClient(this.#options);
    const org = await this.currentOrganization(client);
    const input = v.parse(setIamPolicyRequestSchema, body);
    const policy: GeneratedIamPolicy = {
      version: input.policy.version,
      bindings: input.policy.bindings.map((binding) => ({
        role: binding.role,
        members: [...binding.members],
      })),
      ...(input.policy.etag !== undefined ? { etag: input.policy.etag } : {}),
    };
    const parsedBody: SetIamPolicyRequestContent = {
      policy,
    };
    const path = `/api/v1/orgs/${org.org_id}/iamPolicy:set`;
    const result = await setIamPolicy({
      client,
      body: parsedBody as SetIamPolicyData["body"],
      headers: idempotencyHeaders("iam-policy", options.idempotencyKey),
      path: { orgId: org.org_id },
      responseStyle: "fields",
      throwOnError: false,
    });
    return parsePolicy(unwrapIAMResult(path, result));
  }

  async testIamPermissions(body: TestIamPermissionsRequest): Promise<Array<string>> {
    const client = createIAMClient(this.#options);
    const org = await this.currentOrganization(client);
    return this.testPermissionsForOrg(client, org.org_id, body.permissions);
  }

  async currentOrganization(client = createIAMClient(this.#options)): Promise<Organization> {
    const path = "/api/v1/orgs";
    const result = await listOrganizations({
      client,
      responseStyle: "fields",
      throwOnError: false,
    });
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

  async testPermissionsForOrg(
    client: Client,
    orgID: string,
    permissions: ReadonlyArray<string>,
  ): Promise<Array<string>> {
    const input = v.parse(testIamPermissionsRequestSchema, {
      permissions: [...permissions],
    });
    const body: TestIamPermissionsRequestContent = {
      permissions: input.permissions,
    };
    const path = `/api/v1/orgs/${orgID}/iamPolicy:testPermissions`;
    const result = await testIamPermissions({
      client,
      body: body as TestIamPermissionsData["body"],
      path: { orgId: orgID },
      responseStyle: "fields",
      throwOnError: false,
    });
    const output = unwrapIAMResult(path, result) as TestIamPermissionsResponseContent;
    return output.permissions;
  }
}
