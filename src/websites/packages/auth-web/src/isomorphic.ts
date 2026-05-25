import { redirect } from "@tanstack/react-router";
import { electricStringifiedBooleanSchema } from "@verself/web-env";
import * as v from "valibot";

export const authOrganizationContextSchema = v.object({
  orgID: v.string(),
  identityProviderOrgID: v.string(),
});

export type AuthOrganizationContext = v.InferOutput<typeof authOrganizationContextSchema>;

const organizationContextsSchema = v.union([
  v.array(authOrganizationContextSchema),
  v.pipe(
    v.string(),
    v.transform((value) => JSON.parse(value) as unknown),
    v.array(authOrganizationContextSchema),
  ),
]);

const nullableStringSchema = v.union([
  v.null_(),
  v.pipe(
    v.string(),
    v.transform((value) => {
      const trimmed = value.trim();
      return trimmed === "" ? null : trimmed;
    }),
  ),
]);

const dateSchema = v.union([
  v.date(),
  v.pipe(
    v.string(),
    v.isoTimestamp(),
    v.transform((value) => new Date(value)),
  ),
]);

const revisionSchema = v.union([
  v.pipe(
    v.bigint(),
    v.transform((value) => value.toString()),
  ),
  v.pipe(
    v.number(),
    v.integer(),
    v.transform((value) => value.toString()),
  ),
  v.pipe(v.string(), v.regex(/^-?\d+$/)),
]);

export const clientUserSchema = v.object({
  sub: v.string(),
  email: nullableStringSchema,
  name: nullableStringSchema,
  preferredUsername: nullableStringSchema,
  homeOrgID: nullableStringSchema,
  selectedOrgID: nullableStringSchema,
  orgID: nullableStringSchema,
  availableOrganizations: v.array(authOrganizationContextSchema),
});

export type ClientUser = v.InferOutput<typeof clientUserSchema>;

export const anonymousAuthSchema = v.object({
  isAuthenticated: v.literal(false),
  userId: v.null_(),
  orgId: v.null_(),
  selectedOrgId: v.null_(),
  cachePartition: nullableStringSchema,
  clientHandle: nullableStringSchema,
  accountHandle: v.null_(),
});

export type AnonymousAuth = v.InferOutput<typeof anonymousAuthSchema>;

export const authenticatedAuthSchema = v.object({
  isAuthenticated: v.literal(true),
  userId: v.string(),
  orgId: nullableStringSchema,
  selectedOrgId: nullableStringSchema,
  cachePartition: v.string(),
  clientHandle: v.string(),
  accountHandle: v.string(),
});

export type AuthenticatedAuth = v.InferOutput<typeof authenticatedAuthSchema>;

export const authSchema = v.variant("isAuthenticated", [
  anonymousAuthSchema,
  authenticatedAuthSchema,
]);

export type Auth = v.InferOutput<typeof authSchema>;

export const browserDeviceSchema = v.object({
  label: v.string(),
  kind: v.string(),
  browserName: v.string(),
  osName: v.string(),
});

export type BrowserDevice = v.InferOutput<typeof browserDeviceSchema>;

export const browserLocationSchema = v.object({
  countryCode: v.string(),
  region: v.string(),
  city: v.string(),
});

export type BrowserLocation = v.InferOutput<typeof browserLocationSchema>;

export const sessionInfoSchema = v.object({
  sessionHandle: v.string(),
  clientHandle: v.string(),
  accountHandle: v.string(),
  createdAt: dateSchema,
  lastSeenAt: dateSchema,
  expiresAt: dateSchema,
  clientIP: v.string(),
  clientIPSource: v.string(),
  userAgent: v.string(),
  device: browserDeviceSchema,
  location: browserLocationSchema,
});

export type SessionInfo = v.InferOutput<typeof sessionInfoSchema>;

export const webAuthClientSchema = v.object({
  client_handle: v.string(),
  auth_state: v.picklist(["client", "account"]),
  active_account_handle: v.string(),
  subject: v.string(),
  email: v.string(),
  display_name: v.string(),
  preferred_username: v.string(),
  org_id: v.string(),
  home_org_id: v.string(),
  selected_org_id: v.string(),
  available_org_contexts: organizationContextsSchema,
  client_cache_partition: v.string(),
  auth_revision: revisionSchema,
  last_auth_event: v.string(),
  client_expires_at: dateSchema,
  account_expires_at: dateSchema,
  updated_at: dateSchema,
});

export type WebAuthClient = v.InferOutput<typeof webAuthClientSchema>;

export const webAuthAccountSchema = v.object({
  client_handle: v.string(),
  account_handle: v.string(),
  is_current: electricStringifiedBooleanSchema,
  subject: v.string(),
  email: v.string(),
  display_name: v.string(),
  preferred_username: v.string(),
  home_org_id: v.string(),
  selected_org_id: v.string(),
  available_org_contexts: organizationContextsSchema,
  created_at: dateSchema,
  last_seen_at: dateSchema,
  expires_at: dateSchema,
  updated_at: dateSchema,
});

export type WebAuthAccount = v.InferOutput<typeof webAuthAccountSchema>;

export const webAuthSessionSchema = v.object({
  client_handle: v.string(),
  account_handle: v.string(),
  session_handle: v.string(),
  is_current: electricStringifiedBooleanSchema,
  device_label: v.string(),
  device_kind: v.string(),
  browser_name: v.string(),
  os_name: v.string(),
  location_country_code: v.string(),
  location_region: v.string(),
  location_city: v.string(),
  client_ip: v.string(),
  client_ip_source: v.string(),
  user_agent: v.string(),
  created_at: dateSchema,
  last_seen_at: dateSchema,
  expires_at: dateSchema,
  updated_at: dateSchema,
});

export type WebAuthSession = v.InferOutput<typeof webAuthSessionSchema>;

export const anonymousAuthSnapshotSchema = v.object({
  isSignedIn: v.literal(false),
  auth: anonymousAuthSchema,
  user: v.null_(),
  session: v.null_(),
});

export type AnonymousAuthSnapshot = v.InferOutput<typeof anonymousAuthSnapshotSchema>;

export const authenticatedAuthSnapshotSchema = v.object({
  isSignedIn: v.literal(true),
  auth: authenticatedAuthSchema,
  user: clientUserSchema,
  session: sessionInfoSchema,
});

export type AuthenticatedAuthSnapshot = v.InferOutput<typeof authenticatedAuthSnapshotSchema>;

export const authSnapshotSchema = v.variant("isSignedIn", [
  anonymousAuthSnapshotSchema,
  authenticatedAuthSnapshotSchema,
]);

export type AuthSnapshot = v.InferOutput<typeof authSnapshotSchema>;

export const anonymousAuth: AnonymousAuth = {
  isAuthenticated: false,
  userId: null,
  orgId: null,
  selectedOrgId: null,
  cachePartition: null,
  clientHandle: null,
  accountHandle: null,
};

export const anonymousSnapshot: AuthSnapshot = {
  isSignedIn: false,
  auth: anonymousAuth,
  user: null,
  session: null,
};

export function parseAuthSnapshot(input: unknown): AuthSnapshot {
  const snapshot = v.parse(authSnapshotSchema, normalizeServerSnapshot(input));
  if (!snapshot.isSignedIn) {
    return snapshot;
  }
  if (snapshot.auth.userId !== snapshot.user.sub) {
    throw new Error("Auth snapshot user does not match cache partition owner");
  }
  if (snapshot.auth.orgId !== snapshot.user.selectedOrgID) {
    throw new Error("Auth snapshot organization does not match user organization");
  }
  if (snapshot.auth.selectedOrgId !== snapshot.user.selectedOrgID) {
    throw new Error("Auth snapshot selected organization does not match user organization");
  }
  return snapshot;
}

export function snapshotFromLiveAuth(
  client: WebAuthClient | undefined,
  currentSession: WebAuthSession | undefined,
): AuthSnapshot {
  if (!client) {
    return anonymousSnapshot;
  }

  if (client.auth_state !== "account" || !client.active_account_handle || !client.subject) {
    return {
      isSignedIn: false,
      auth: {
        ...anonymousAuth,
        cachePartition: client.client_cache_partition,
        clientHandle: client.client_handle,
      },
      user: null,
      session: null,
    };
  }

  const selectedOrgID = emptyToNull(client.selected_org_id);
  return {
    isSignedIn: true,
    auth: {
      isAuthenticated: true,
      userId: client.subject,
      orgId: selectedOrgID,
      selectedOrgId: selectedOrgID,
      cachePartition: client.client_cache_partition,
      clientHandle: client.client_handle,
      accountHandle: client.active_account_handle,
    },
    user: {
      sub: client.subject,
      email: emptyToNull(client.email),
      name: emptyToNull(client.display_name),
      preferredUsername: emptyToNull(client.preferred_username),
      homeOrgID: emptyToNull(client.home_org_id),
      selectedOrgID,
      orgID: selectedOrgID,
      availableOrganizations: client.available_org_contexts,
    },
    session: sessionInfoFromLiveAuth(client, currentSession),
  };
}

function sessionInfoFromLiveAuth(
  client: WebAuthClient,
  session: WebAuthSession | undefined,
): SessionInfo {
  return {
    sessionHandle: session?.session_handle ?? client.active_account_handle,
    clientHandle: client.client_handle,
    accountHandle: session?.account_handle ?? client.active_account_handle,
    createdAt: session?.created_at ?? client.updated_at,
    lastSeenAt: session?.last_seen_at ?? client.updated_at,
    expiresAt: session?.expires_at ?? client.account_expires_at,
    clientIP: session?.client_ip ?? "",
    clientIPSource: session?.client_ip_source ?? "",
    userAgent: session?.user_agent ?? "",
    device: {
      label: session?.device_label ?? "",
      kind: session?.device_kind ?? "",
      browserName: session?.browser_name ?? "",
      osName: session?.os_name ?? "",
    },
    location: {
      countryCode: session?.location_country_code ?? "",
      region: session?.location_region ?? "",
      city: session?.location_city ?? "",
    },
  };
}

function normalizeServerSnapshot(input: unknown): unknown {
  if (!input || typeof input !== "object") {
    return input;
  }
  const snapshot = input as Record<string, unknown>;
  if (snapshot.isSignedIn !== true || !snapshot.session || typeof snapshot.session !== "object") {
    return input;
  }
  const session = snapshot.session as Record<string, unknown>;
  return {
    ...snapshot,
    session: {
      ...session,
      clientIPSource: session.clientIPSource ?? "",
    },
  };
}

function emptyToNull(value: string): string | null {
  const trimmed = value.trim();
  return trimmed === "" ? null : trimmed;
}

export function loginRedirect(locationHref: string) {
  return redirect({
    to: "/login",
    search: { redirect: locationHref },
  });
}

export function requireAuth(authState: Auth, locationHref: string): AuthenticatedAuth {
  if (!authState.isAuthenticated) {
    throw loginRedirect(locationHref);
  }
  return authState;
}

export function authQueryKey<TParts extends readonly unknown[]>(
  authState: AuthenticatedAuth,
  ...parts: TParts
) {
  return ["auth", authState.cachePartition, authState.selectedOrgId, ...parts] as const;
}

export function authCollectionId(authState: AuthenticatedAuth, baseId: string): string {
  return `auth:${authState.cachePartition}:${authState.selectedOrgId ?? "no-org"}:${baseId}`;
}

export interface AuthPartitionedCache {
  clear: () => void;
}

const authPartitionsByCache = new WeakMap<AuthPartitionedCache, string | null>();

export function authCacheKey(snapshot: AuthSnapshot): string {
  return `auth:${snapshot.auth.cachePartition ?? "anonymous"}:${snapshot.auth.selectedOrgId ?? "no-org"}`;
}

export function syncAuthPartitionedCache(
  cache: AuthPartitionedCache,
  snapshot: AuthSnapshot,
): void {
  const cachePartition = snapshot.auth.cachePartition;
  const previousPartition = authPartitionsByCache.get(cache);
  if (previousPartition !== undefined && previousPartition !== cachePartition) {
    cache.clear();
  }
  authPartitionsByCache.set(cache, cachePartition);
}
