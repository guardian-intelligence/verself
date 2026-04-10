import { redirect } from "@tanstack/react-router";

export interface AuthRoleAssignment {
  projectID: string | null;
  orgID: string;
  orgName: string | null;
  role: string;
}

export interface CurrentUser {
  sub: string;
  email: string | null;
  name: string | null;
  preferredUsername: string | null;
  // Current runtime still assumes one active org per user even though Zitadel
  // can issue multi-org assignments.
  orgID: string | null;
  roles: string[];
  roleAssignments: AuthRoleAssignment[];
  claims: Record<string, unknown>;
}

export interface AuthSession {
  sessionID: string;
  clientCachePartition: string;
  accessToken: string;
  refreshToken: string | null;
  idToken: string | null;
  scope: string | null;
  expiresAt: Date;
  user: CurrentUser;
  createdAt: Date;
  updatedAt: Date;
}

export interface ClientUser {
  sub: string;
  email: string | null;
  name: string | null;
  preferredUsername: string | null;
  orgID: string | null;
  roles: string[];
  roleAssignments: AuthRoleAssignment[];
}

export interface AnonymousAuth {
  isAuthenticated: false;
  userId: null;
  orgId: null;
  roles: string[];
  roleAssignments: AuthRoleAssignment[];
  cachePartition: null;
}

export interface AuthenticatedAuth {
  isAuthenticated: true;
  userId: string;
  orgId: string | null;
  roles: string[];
  roleAssignments: AuthRoleAssignment[];
  cachePartition: string;
}

export type Auth = AnonymousAuth | AuthenticatedAuth;

export interface SessionInfo {
  createdAt: Date;
  expiresAt: Date;
}

export interface AuthSnapshot {
  auth: Auth;
  user: ClientUser | null;
  session: SessionInfo | null;
}

export const anonymousAuth: AnonymousAuth = {
  isAuthenticated: false,
  userId: null,
  orgId: null,
  roles: [],
  roleAssignments: [],
  cachePartition: null,
};

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
  authState: { cachePartition: string },
  ...parts: TParts
) {
  return ["auth", authState.cachePartition, ...parts] as const;
}

export function authCollectionId(authState: { cachePartition: string }, baseId: string): string {
  return `auth:${authState.cachePartition}:${baseId}`;
}
