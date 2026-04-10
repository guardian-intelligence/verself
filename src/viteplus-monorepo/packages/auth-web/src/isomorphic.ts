import { redirect } from "@tanstack/react-router";
import * as v from "valibot";

export const authRoleAssignmentSchema = v.object({
  projectID: v.nullable(v.string()),
  orgID: v.string(),
  orgName: v.nullable(v.string()),
  role: v.string(),
});

export type AuthRoleAssignment = v.InferOutput<typeof authRoleAssignmentSchema>;

export const clientUserSchema = v.object({
  sub: v.string(),
  email: v.nullable(v.string()),
  name: v.nullable(v.string()),
  preferredUsername: v.nullable(v.string()),
  orgID: v.nullable(v.string()),
  roles: v.array(v.string()),
  roleAssignments: v.array(authRoleAssignmentSchema),
});

export type ClientUser = v.InferOutput<typeof clientUserSchema>;

export const anonymousAuthSchema = v.object({
  isAuthenticated: v.literal(false),
  userId: v.null_(),
  orgId: v.null_(),
  roles: v.array(v.string()),
  roleAssignments: v.array(authRoleAssignmentSchema),
  cachePartition: v.null_(),
});

export type AnonymousAuth = v.InferOutput<typeof anonymousAuthSchema>;

export const authenticatedAuthSchema = v.object({
  isAuthenticated: v.literal(true),
  userId: v.string(),
  orgId: v.nullable(v.string()),
  roles: v.array(v.string()),
  roleAssignments: v.array(authRoleAssignmentSchema),
  cachePartition: v.string(),
});

export type AuthenticatedAuth = v.InferOutput<typeof authenticatedAuthSchema>;

export const authSchema = v.variant("isAuthenticated", [anonymousAuthSchema, authenticatedAuthSchema]);

export type Auth = v.InferOutput<typeof authSchema>;

const sessionDateSchema = v.union([
  v.date(),
  v.pipe(
    v.string(),
    v.isoTimestamp(),
    v.transform((value) => new Date(value)),
  ),
]);

export const sessionInfoSchema = v.object({
  createdAt: sessionDateSchema,
  expiresAt: sessionDateSchema,
});

export type SessionInfo = v.InferOutput<typeof sessionInfoSchema>;

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
  roles: [],
  roleAssignments: [],
  cachePartition: null,
};

export function parseAuthSnapshot(input: unknown): AuthSnapshot {
  const snapshot = v.parse(authSnapshotSchema, input);

  if (!snapshot.isSignedIn) {
    if (snapshot.auth.roles.length > 0 || snapshot.auth.roleAssignments.length > 0) {
      throw new Error("Anonymous auth snapshot cannot carry roles");
    }
    return snapshot;
  }

  if (snapshot.auth.userId !== snapshot.user.sub) {
    throw new Error("Auth snapshot user does not match cache partition owner");
  }

  if (snapshot.auth.orgId !== snapshot.user.orgID) {
    throw new Error("Auth snapshot organization does not match user organization");
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
  return ["auth", authState.cachePartition, ...parts] as const;
}

export function authCollectionId(authState: AuthenticatedAuth, baseId: string): string {
  return `auth:${authState.cachePartition}:${baseId}`;
}

export interface AuthPartitionedCache {
  clear: () => void;
}

const authPartitionsByCache = new WeakMap<AuthPartitionedCache, string | null>();

export function authCacheKey(snapshot: AuthSnapshot): string {
  return `auth:${snapshot.auth.cachePartition ?? "anonymous"}`;
}

export function syncAuthPartitionedCache(cache: AuthPartitionedCache, snapshot: AuthSnapshot): void {
  const cachePartition = snapshot.auth.cachePartition;
  const previousPartition = authPartitionsByCache.get(cache);
  if (previousPartition !== undefined && previousPartition !== cachePartition) {
    cache.clear();
  }
  authPartitionsByCache.set(cache, cachePartition);
}
