import { redirect } from "@tanstack/react-router";
import * as v from "valibot";

export const authOrganizationContextSchema = v.object({
  orgID: v.string(),
  identityProviderOrgID: v.string(),
});

export type AuthOrganizationContext = v.InferOutput<typeof authOrganizationContextSchema>;

export const clientUserSchema = v.object({
  sub: v.string(),
  email: v.nullable(v.string()),
  name: v.nullable(v.string()),
  preferredUsername: v.nullable(v.string()),
  homeOrgID: v.nullable(v.string()),
  selectedOrgID: v.nullable(v.string()),
  orgID: v.nullable(v.string()),
  availableOrganizations: v.array(authOrganizationContextSchema),
});

export type ClientUser = v.InferOutput<typeof clientUserSchema>;

export const anonymousAuthSchema = v.object({
  isAuthenticated: v.literal(false),
  userId: v.null_(),
  orgId: v.null_(),
  selectedOrgId: v.null_(),
  cachePartition: v.null_(),
});

export type AnonymousAuth = v.InferOutput<typeof anonymousAuthSchema>;

export const authenticatedAuthSchema = v.object({
  isAuthenticated: v.literal(true),
  userId: v.string(),
  orgId: v.nullable(v.string()),
  selectedOrgId: v.nullable(v.string()),
  cachePartition: v.string(),
});

export type AuthenticatedAuth = v.InferOutput<typeof authenticatedAuthSchema>;

export const authSchema = v.variant("isAuthenticated", [
  anonymousAuthSchema,
  authenticatedAuthSchema,
]);

export type Auth = v.InferOutput<typeof authSchema>;

const sessionDateSchema = v.union([
  v.date(),
  v.pipe(
    v.string(),
    v.isoTimestamp(),
    v.transform((value) => new Date(value)),
  ),
]);

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
  createdAt: sessionDateSchema,
  lastSeenAt: sessionDateSchema,
  expiresAt: sessionDateSchema,
  authMethods: v.array(v.string()),
  clientIP: v.string(),
  clientIPTrusted: v.boolean(),
  clientIPSource: v.string(),
  edgePeerIP: v.string(),
  userAgent: v.string(),
  device: browserDeviceSchema,
  location: browserLocationSchema,
});

export type SessionInfo = v.InferOutput<typeof sessionInfoSchema>;

export const browserSessionSummarySchema = v.object({
  sessionHandle: v.string(),
  isCurrent: v.boolean(),
  createdAt: sessionDateSchema,
  lastSeenAt: sessionDateSchema,
  expiresAt: sessionDateSchema,
  createdClientIP: v.string(),
  createdClientIPTrusted: v.boolean(),
  createdClientIPSource: v.string(),
  createdEdgePeerIP: v.string(),
  createdUserAgent: v.string(),
  createdDevice: browserDeviceSchema,
  createdLocation: browserLocationSchema,
  clientIP: v.string(),
  clientIPTrusted: v.boolean(),
  clientIPSource: v.string(),
  edgePeerIP: v.string(),
  userAgent: v.string(),
  device: browserDeviceSchema,
  location: browserLocationSchema,
});

export type BrowserSessionSummary = v.InferOutput<typeof browserSessionSummarySchema>;

export const browserSessionsResponseSchema = v.object({
  sessions: v.array(browserSessionSummarySchema),
});

export type BrowserSessionsResponse = v.InferOutput<typeof browserSessionsResponseSchema>;

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
};

export function parseAuthSnapshot(input: unknown): AuthSnapshot {
  const snapshot = v.parse(authSnapshotSchema, input);

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
