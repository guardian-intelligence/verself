import * as v from "valibot";
import { createClient, type Client } from "./__generated/iam-api/client/index.js";
import {
  createApiCredential as createGeneratedApiCredential,
  getApiCredential as getGeneratedApiCredential,
  getOrganization as getGeneratedOrganization,
  getOrganizationMemberCapabilities as getGeneratedOrganizationMemberCapabilities,
  inviteOrganizationMember as inviteGeneratedOrganizationMember,
  listApiCredentials as listGeneratedApiCredentials,
  listMyOrganizations as listGeneratedMyOrganizations,
  listOrganizationMembers as listGeneratedOrganizationMembers,
  patchOrganization as patchGeneratedOrganization,
  putOrganizationMemberCapabilities as putGeneratedOrganizationMemberCapabilities,
  revokeApiCredential as revokeGeneratedApiCredential,
  rollApiCredential as rollGeneratedApiCredential,
  updateOrganizationMemberRoles as updateGeneratedOrganizationMemberRoles,
} from "./__generated/iam-api/index.js";
import type {
  IamCreateApiCredentialRequestWritable,
  IamInviteMemberRequestWritable,
  IamPutMemberCapabilitiesRequestWritable,
  IamRollApiCredentialRequestWritable,
  IamUpdateOrganizationRequestWritable,
} from "./__generated/iam-api/types.gen.js";
import {
  vCreateApiCredentialBody,
  vGetApiCredentialPath,
  vIamCreateApiCredentialResponse,
  vIamInviteMemberRequestWritable,
  vIamInviteMemberResponse,
  vIamMember,
  vIamMemberCapabilities,
  vIamMemberCapabilitiesDocument,
  vIamMemberCapability,
  vIamMembers,
  vIamOrganization,
  vIamOrganizationMetadata,
  vIamPutMemberCapabilitiesRequestWritable,
  vIamRollApiCredentialResponse,
  vIamRollApiCredentialRequestWritable,
  vIamUpdateMemberRolesRequestWritable,
  vIamUpdateOrganizationRequestWritable,
  vIamapiCredential,
  vIamapiCredentialIssuedMaterial,
  vIamapiCredentials,
  vRevokeApiCredentialPath,
  vRollApiCredentialPath,
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

const permissionSchema = v.pipe(v.string(), v.trim(), v.minLength(1), v.maxLength(256));
const permissionsSchema = v.pipe(
  v.array(permissionSchema),
  v.minLength(1),
  v.maxLength(256),
  v.transform((permissions) => Array.from(new Set(permissions)).sort()),
);

const organizationSlugSchema = v.pipe(
  v.string(),
  v.trim(),
  v.toLowerCase(),
  v.regex(/^[a-z0-9]+(?:-[a-z0-9]+)*$/),
  v.maxLength(80),
);

function omitEmptyText(value: string | undefined): string | undefined {
  const trimmed = value?.trim();
  return trimmed ? trimmed : undefined;
}

function parseMember(input: unknown) {
  const { $schema: _schema, role_keys, ...member } = v.parse(vIamMember, input);
  return {
    ...member,
    role_keys: role_keys ?? [],
  };
}

export type Member = ReturnType<typeof parseMember>;

function parseMemberCapabilitiesDocument(input: unknown) {
  const { enabled_keys, ...doc } = v.parse(vIamMemberCapabilitiesDocument, input);
  return {
    ...doc,
    enabled_keys: enabled_keys ?? [],
  };
}

export type MemberCapabilitiesDocument = ReturnType<typeof parseMemberCapabilitiesDocument>;

function parseMemberCapability(input: unknown) {
  const capability = v.parse(vIamMemberCapability, input);
  return {
    ...capability,
    permissions: capability.permissions ?? [],
  };
}

export type MemberCapability = ReturnType<typeof parseMemberCapability>;

function parseMemberCapabilities(input: unknown) {
  const { $schema: _schema, document, catalog } = v.parse(vIamMemberCapabilities, input);
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
  } = v.parse(vIamOrganization, input);
  return {
    ...organization,
    caller: parseMember(caller),
    permissions: permissions ?? [],
    member_capabilities: parseMemberCapabilitiesDocument(member_capabilities),
  };
}

export type Organization = ReturnType<typeof parseOrganization>;

function parseOrganizationMetadata(input: unknown) {
  return v.parse(vIamOrganizationMetadata, input);
}

export type OrganizationMetadata = ReturnType<typeof parseOrganizationMetadata>;

function parseMembers(input: unknown): Array<Member> {
  const { $schema: _schema, members } = v.parse(vIamMembers, input);
  return members?.map((member) => parseMember(member)) ?? [];
}

function parseAPICredential(input: unknown) {
  const { $schema: _schema, permissions, ...credential } = v.parse(vIamapiCredential, input);
  return {
    ...credential,
    permissions: permissions ?? [],
  };
}

export type APICredential = ReturnType<typeof parseAPICredential>;

function parseAPICredentials(input: unknown): Array<APICredential> {
  const { $schema: _schema, credentials } = v.parse(vIamapiCredentials, input);
  return credentials?.map((credential) => parseAPICredential(credential)) ?? [];
}

function parseIssuedMaterial(input: unknown) {
  return v.parse(vIamapiCredentialIssuedMaterial, input);
}

export type APICredentialIssuedMaterial = ReturnType<typeof parseIssuedMaterial>;

function parseCreateAPICredentialResponse(input: unknown) {
  const {
    $schema: _schema,
    credential,
    issued_material,
  } = v.parse(vIamCreateApiCredentialResponse, input);
  return {
    credential: parseAPICredential(credential),
    issued_material: parseIssuedMaterial(issued_material),
  };
}

export type CreateAPICredentialResponse = ReturnType<typeof parseCreateAPICredentialResponse>;

function parseRollAPICredentialResponse(input: unknown) {
  const {
    $schema: _schema,
    credential,
    issued_material,
  } = v.parse(vIamRollApiCredentialResponse, input);
  return {
    credential: parseAPICredential(credential),
    issued_material: parseIssuedMaterial(issued_material),
  };
}

export type RollAPICredentialResponse = ReturnType<typeof parseRollAPICredentialResponse>;

export const updateOrganizationRequestSchema = v.strictObject({
  display_name: v.optional(v.pipe(v.string(), v.trim(), v.minLength(1), v.maxLength(120))),
  slug: v.optional(organizationSlugSchema),
  version: v.pipe(v.number(), v.integer(), v.minValue(0), v.maxValue(2147483647)),
});

export type UpdateOrganizationRequest = v.InferInput<typeof updateOrganizationRequestSchema>;

export const inviteMemberRequestSchema = v.strictObject({
  email: v.pipe(v.string(), v.trim(), v.email()),
  familyName: v.optional(v.pipe(v.string(), v.maxLength(100))),
  givenName: v.optional(v.pipe(v.string(), v.maxLength(100))),
  roleKeys: roleKeysSchema,
});

export type InviteMemberRequest = v.InferInput<typeof inviteMemberRequestSchema>;
export type InviteMemberResponse = v.InferOutput<typeof vIamInviteMemberResponse>;

export const updateMemberRolesRequestSchema = v.pipe(
  v.strictObject({
    expectedOrgAclVersion: v.pipe(v.number(), v.integer(), v.minValue(0), v.maxValue(2147483647)),
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

export const createAPICredentialRequestSchema = v.strictObject({
  auth_method: v.optional(v.picklist(["private_key_jwt", "client_secret"])),
  display_name: v.pipe(v.string(), v.trim(), v.minLength(1), v.maxLength(200)),
  expires_at: v.optional(v.pipe(v.string(), v.isoTimestamp())),
  permissions: permissionsSchema,
});

export type CreateAPICredentialRequest = v.InferInput<typeof createAPICredentialRequestSchema>;

export const rollAPICredentialRequestSchema = v.strictObject({
  auth_method: v.optional(v.picklist(["private_key_jwt", "client_secret"])),
});

export type RollAPICredentialRequest = v.InferInput<typeof rollAPICredentialRequestSchema>;

export class IAM {
  readonly #options: IAMClientOptions;

  constructor(options: IAMClientOptions) {
    this.#options = options;
  }

  async getOrganization(): Promise<Organization> {
    const client = createIAMClient(this.#options);
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

  async listMyOrganizations(): Promise<Array<OrganizationMetadata>> {
    const client = createIAMClient(this.#options);
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

  async updateOrganization(
    body: UpdateOrganizationRequest,
    options: IAMMutationOptions = {},
  ): Promise<Organization> {
    const client = createIAMClient(this.#options);
    const parsedBody = removeUndefined(
      v.parse(
        vIamUpdateOrganizationRequestWritable,
        v.parse(updateOrganizationRequestSchema, body),
      ),
    ) as IamUpdateOrganizationRequestWritable;
    const path = "/api/v1/organization";
    const result = await patchGeneratedOrganization({
      body: parsedBody,
      client,
      headers: idempotencyHeaders("iam-organization", options.idempotencyKey),
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

  async inviteMember(
    body: InviteMemberRequest,
    options: IAMMutationOptions = {},
  ): Promise<InviteMemberResponse> {
    const client = createIAMClient(this.#options);
    const input = v.parse(inviteMemberRequestSchema, body);
    const bodyInput = removeUndefined({
      email: input.email,
      family_name: omitEmptyText(input.familyName),
      given_name: omitEmptyText(input.givenName),
      role_keys: input.roleKeys,
    }) as IamInviteMemberRequestWritable;
    const parsedBody = v.parse(
      vIamInviteMemberRequestWritable,
      bodyInput,
    ) as IamInviteMemberRequestWritable;
    const path = "/api/v1/organization/members";
    const result = await inviteGeneratedOrganizationMember({
      body: parsedBody,
      client,
      headers: idempotencyHeaders("iam-member-invite", options.idempotencyKey),
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwIAMError(path, result.response, result.error);
    }
    return v.parse(vIamInviteMemberResponse, result.data);
  }

  async updateMemberRoles(
    body: UpdateMemberRolesRequest,
    options: IAMMutationOptions = {},
  ): Promise<Member> {
    const client = createIAMClient(this.#options);
    const input = v.parse(updateMemberRolesRequestSchema, body);
    const parsedBody = v.parse(vIamUpdateMemberRolesRequestWritable, {
      expected_org_acl_version: input.expectedOrgAclVersion,
      expected_role_keys: input.expectedRoleKeys,
      role_keys: input.roleKeys,
    });
    const path = `/api/v1/organization/members/${input.userId}/roles`;
    const result = await updateGeneratedOrganizationMemberRoles({
      body: parsedBody,
      client,
      headers: idempotencyHeaders("iam-member-roles", options.idempotencyKey),
      path: { user_id: input.userId },
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwIAMError(path, result.response, result.error);
    }
    return parseMember(result.data);
  }

  async getMemberCapabilities(): Promise<MemberCapabilities> {
    const client = createIAMClient(this.#options);
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

  async putMemberCapabilities(
    body: PutMemberCapabilitiesRequest,
    options: IAMMutationOptions = {},
  ): Promise<MemberCapabilities> {
    const client = createIAMClient(this.#options);
    const input = v.parse(putMemberCapabilitiesRequestSchema, body);
    const parsedBody: IamPutMemberCapabilitiesRequestWritable = v.parse(
      vIamPutMemberCapabilitiesRequestWritable,
      {
        enabled_keys: input.enabled_keys,
        version: input.version,
      },
    );
    const path = "/api/v1/organization/member-capabilities";
    const result = await putGeneratedOrganizationMemberCapabilities({
      body: parsedBody,
      client,
      headers: idempotencyHeaders("iam-member-capabilities", options.idempotencyKey),
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwIAMError(path, result.response, result.error);
    }
    return parseMemberCapabilities(result.data);
  }

  async listAPICredentials(): Promise<Array<APICredential>> {
    const client = createIAMClient(this.#options);
    const path = "/api/v1/organization/api-credentials";
    const result = await listGeneratedApiCredentials({
      client,
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwIAMError(path, result.response, result.error);
    }
    return parseAPICredentials(result.data);
  }

  async createAPICredential(
    body: CreateAPICredentialRequest,
    options: IAMMutationOptions = {},
  ): Promise<CreateAPICredentialResponse> {
    const client = createIAMClient(this.#options);
    const parsedBody = removeUndefined(
      v.parse(vCreateApiCredentialBody, v.parse(createAPICredentialRequestSchema, body)),
    ) as IamCreateApiCredentialRequestWritable;
    const path = "/api/v1/organization/api-credentials";
    const result = await createGeneratedApiCredential({
      body: parsedBody,
      client,
      headers: idempotencyHeaders("iam-api-credential", options.idempotencyKey),
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwIAMError(path, result.response, result.error);
    }
    return parseCreateAPICredentialResponse(result.data);
  }

  async getAPICredential(credentialId: string): Promise<APICredential> {
    const client = createIAMClient(this.#options);
    const pathParams = v.parse(vGetApiCredentialPath, { credential_id: credentialId });
    const path = `/api/v1/organization/api-credentials/${credentialId}`;
    const result = await getGeneratedApiCredential({
      client,
      path: pathParams,
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwIAMError(path, result.response, result.error);
    }
    return parseAPICredential(result.data);
  }

  async rollAPICredential(
    credentialId: string,
    body: RollAPICredentialRequest = {},
    options: IAMMutationOptions = {},
  ): Promise<RollAPICredentialResponse> {
    const client = createIAMClient(this.#options);
    const pathParams = v.parse(vRollApiCredentialPath, { credential_id: credentialId });
    const parsedBody = removeUndefined(
      v.parse(vIamRollApiCredentialRequestWritable, v.parse(rollAPICredentialRequestSchema, body)),
    ) as IamRollApiCredentialRequestWritable;
    const path = `/api/v1/organization/api-credentials/${credentialId}/roll`;
    const result = await rollGeneratedApiCredential({
      body: parsedBody,
      client,
      headers: idempotencyHeaders("iam-api-credential-roll", options.idempotencyKey),
      path: pathParams,
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwIAMError(path, result.response, result.error);
    }
    return parseRollAPICredentialResponse(result.data);
  }

  async revokeAPICredential(
    credentialId: string,
    options: IAMMutationOptions = {},
  ): Promise<APICredential> {
    const client = createIAMClient(this.#options);
    const pathParams = v.parse(vRevokeApiCredentialPath, { credential_id: credentialId });
    const path = `/api/v1/organization/api-credentials/${credentialId}`;
    const result = await revokeGeneratedApiCredential({
      client,
      headers: idempotencyHeaders("iam-api-credential-revoke", options.idempotencyKey),
      path: pathParams,
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwIAMError(path, result.response, result.error);
    }
    return parseAPICredential(result.data);
  }
}
