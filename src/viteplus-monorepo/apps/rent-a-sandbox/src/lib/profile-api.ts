import * as v from "valibot";
import { createClient, type Client } from "../__generated/profile-api/client/index.js";
import {
  getProfile as getGeneratedProfile,
  patchProfileIdentity as patchGeneratedProfileIdentity,
  putProfilePreferences as putGeneratedProfilePreferences,
} from "../__generated/profile-api/index.js";
import type {
  ProfilePutPreferencesRequestWritable,
  ProfileUpdateIdentityRequestWritable,
} from "../__generated/profile-api/types.gen.js";
import {
  vProfilePutPreferencesRequestWritable,
  vProfileSnapshot,
  vProfileUpdateIdentityRequestWritable,
} from "../__generated/profile-api/valibot.gen.js";
import {
  type BearerClientOptions,
  ServiceApiError,
  createBearerJSONHeaders,
  idempotencyHeaders,
  throwGeneratedServiceError,
} from "./service-api";

export interface ProfileClientOptions extends BearerClientOptions {}

export class ProfileApiError extends ServiceApiError {
  constructor(status: number, path: string, body: string) {
    super("Profile API", status, path, body);
    this.name = "ProfileApiError";
  }
}

export function isProfileApiError(error: unknown): error is ProfileApiError {
  return error instanceof ProfileApiError;
}

function throwProfileError(path: string, response: Response | undefined, error: unknown): never {
  throwGeneratedServiceError(ProfileApiError, path, response, error);
}

function createProfileClient(options: ProfileClientOptions): Client {
  return createClient({
    baseUrl: options.baseUrl,
    headers: createBearerJSONHeaders(options.accessToken),
    ...(options.fetch ? { fetch: options.fetch } : {}),
  });
}

const profileTextSchema = (maxLength: number) =>
  v.pipe(v.string(), v.trim(), v.minLength(1), v.maxLength(maxLength));

export const updateProfileIdentityRequestSchema = v.strictObject({
  display_name: v.optional(v.pipe(v.string(), v.trim(), v.maxLength(200))),
  family_name: profileTextSchema(100),
  given_name: profileTextSchema(100),
  version: v.pipe(v.number(), v.integer(), v.minValue(0), v.maxValue(2147483647)),
});

export type UpdateProfileIdentityRequest = v.InferInput<typeof updateProfileIdentityRequestSchema>;

export const putProfilePreferencesRequestSchema = v.strictObject({
  default_surface: v.optional(v.pipe(v.string(), v.trim(), v.maxLength(80))),
  locale: v.pipe(v.string(), v.trim(), v.minLength(1), v.maxLength(35)),
  theme: v.picklist(["system", "light", "dark"]),
  time_display: v.picklist(["utc", "local"]),
  timezone: v.pipe(v.string(), v.trim(), v.minLength(1), v.maxLength(64)),
  version: v.pipe(v.number(), v.integer(), v.minValue(0), v.maxValue(2147483647)),
});

export type PutProfilePreferencesRequest = v.InferInput<typeof putProfilePreferencesRequestSchema>;

function parseProfileSnapshot(input: unknown) {
  const { $schema: _schema, ...snapshot } = v.parse(vProfileSnapshot, input);
  return snapshot;
}

export type ProfileSnapshot = ReturnType<typeof parseProfileSnapshot>;

export async function getProfile(options: ProfileClientOptions): Promise<ProfileSnapshot> {
  const client = createProfileClient(options);
  const path = "/api/v1/profile";
  const result = await getGeneratedProfile({
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwProfileError(path, result.response, result.error);
  }

  return parseProfileSnapshot(result.data);
}

export async function updateProfileIdentity(
  options: ProfileClientOptions & { body: UpdateProfileIdentityRequest },
): Promise<ProfileSnapshot> {
  const client = createProfileClient(options);
  const input = v.parse(updateProfileIdentityRequestSchema, options.body);
  const bodySource: Record<string, unknown> = {
    family_name: input.family_name,
    given_name: input.given_name,
    version: input.version,
  };
  if (input.display_name !== undefined) {
    bodySource.display_name = input.display_name;
  }
  const body = v.parse(
    vProfileUpdateIdentityRequestWritable,
    bodySource,
  ) as ProfileUpdateIdentityRequestWritable;
  const path = "/api/v1/profile/identity";
  const result = await patchGeneratedProfileIdentity({
    body,
    client,
    headers: idempotencyHeaders("profile-identity"),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwProfileError(path, result.response, result.error);
  }

  return parseProfileSnapshot(result.data);
}

export async function putProfilePreferences(
  options: ProfileClientOptions & { body: PutProfilePreferencesRequest },
): Promise<ProfileSnapshot> {
  const client = createProfileClient(options);
  const input = v.parse(putProfilePreferencesRequestSchema, options.body);
  const bodySource: Record<string, unknown> = {
    locale: input.locale,
    theme: input.theme,
    time_display: input.time_display,
    timezone: input.timezone,
    version: input.version,
  };
  if (input.default_surface !== undefined) {
    bodySource.default_surface = input.default_surface;
  }
  const body = v.parse(
    vProfilePutPreferencesRequestWritable,
    bodySource,
  ) as ProfilePutPreferencesRequestWritable;
  const path = "/api/v1/profile/preferences";
  const result = await putGeneratedProfilePreferences({
    body,
    client,
    headers: idempotencyHeaders("profile-preferences"),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwProfileError(path, result.response, result.error);
  }

  return parseProfileSnapshot(result.data);
}
