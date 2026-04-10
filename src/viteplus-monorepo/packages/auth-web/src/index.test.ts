import { createElement, type ReactNode } from "react";
import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vite-plus/test";
import {
  AuthProvider,
  useAuth,
  useClerk,
  useSession,
  useSignedInAuth,
  useUser,
  type UseAuthReturn,
  type UseSessionReturn,
  type UseUserReturn,
} from "./react.ts";
import {
  authCacheKey,
  authCollectionId,
  authQueryKey,
  parseAuthSnapshot,
  requireAuth,
  syncAuthPartitionedCache,
  type Auth,
  type AuthSnapshot,
} from "./isomorphic.ts";
import { createAuthConfig, resolveAuthConfig, type AuthConfig } from "./config.ts";

const anonymousAuth: Auth = {
  cachePartition: null,
  isAuthenticated: false,
  orgId: null,
  roleAssignments: [],
  roles: [],
  userId: null,
};

const authenticatedAuth = {
  cachePartition: "partition-123",
  isAuthenticated: true,
  orgId: "org-1",
  roleAssignments: [],
  roles: ["sandbox_operator"],
  userId: "user-1",
} satisfies Auth;

const anonymousSnapshot: AuthSnapshot = {
  isSignedIn: false,
  auth: anonymousAuth,
  session: null,
  user: null,
};

const authenticatedSnapshot: AuthSnapshot = {
  isSignedIn: true,
  auth: authenticatedAuth,
  session: {
    createdAt: new Date("2026-04-10T12:00:00.000Z"),
    expiresAt: new Date("2026-04-10T13:00:00.000Z"),
  },
  user: {
    email: "user@example.com",
    name: "User Example",
    orgID: "org-1",
    preferredUsername: "user",
    roleAssignments: [],
    roles: ["sandbox_operator"],
    sub: "user-1",
  },
};

const testAuthConfig: AuthConfig = createAuthConfig({
  appName: "test-auth",
  issuerURL: "https://auth.example.test",
  clientID: "test-client",
  sessionDatabaseURL: "postgres://auth.example.test/auth",
  sessionPassword: "x".repeat(32),
  scopes: ["openid", "profile"],
  callbackPath: "/callback",
  defaultRedirectPath: "/",
  postLogoutRedirectPath: "/",
});

function renderWithAuth(snapshot: AuthSnapshot, child: ReactNode) {
  return renderToString(createElement(AuthProvider, { snapshot }, child));
}

function one<T>(values: T[]): T {
  const value = values[0];
  if (!value) {
    throw new Error("Expected test probe to capture a value");
  }
  return value;
}

describe("auth-web React hooks", () => {
  it("exposes anonymous auth without a cache partition", () => {
    const results: UseAuthReturn[] = [];

    function Probe() {
      results.push(useAuth());
      return null;
    }

    renderWithAuth(anonymousSnapshot, createElement(Probe));

    const result = one(results);
    expect(result).toMatchObject({
      cachePartition: null,
      isLoaded: true,
      isSignedIn: false,
      userId: null,
    });
    expect(result.has({ role: "sandbox_operator" })).toBe(false);
  });

  it("exposes authenticated auth and role checks", () => {
    const results: UseAuthReturn[] = [];

    function Probe() {
      results.push(useAuth());
      return null;
    }

    renderWithAuth(authenticatedSnapshot, createElement(Probe));

    const result = one(results);
    expect(result).toMatchObject({
      cachePartition: "partition-123",
      isLoaded: true,
      isSignedIn: true,
      orgId: "org-1",
      userId: "user-1",
    });
    expect(result.has({ role: "sandbox_operator" })).toBe(true);
    expect(result.has({ role: "billing_admin" })).toBe(false);
  });

  it("exposes client-safe user and session metadata", () => {
    const userResults: UseUserReturn[] = [];
    const sessionResults: UseSessionReturn[] = [];

    function Probe() {
      userResults.push(useUser());
      sessionResults.push(useSession());
      return null;
    }

    renderWithAuth(authenticatedSnapshot, createElement(Probe));

    expect(one(userResults).user?.email).toBe("user@example.com");
    expect(one(userResults).isSignedIn).toBe(true);
    expect(one(sessionResults).session?.expiresAt.toISOString()).toBe("2026-04-10T13:00:00.000Z");
  });

  it("throws when authenticated auth is requested from an anonymous snapshot", () => {
    function Probe() {
      useSignedInAuth();
      return null;
    }

    expect(() => renderWithAuth(anonymousSnapshot, createElement(Probe))).toThrow(
      "useSignedInAuth() requires a signed-in auth snapshot",
    );
  });

  it("rejects mismatched auth snapshots at the provider boundary", () => {
    const mismatchedSnapshot = {
      ...authenticatedSnapshot,
      user: {
        ...authenticatedSnapshot.user,
        sub: "other-user",
      },
    } satisfies AuthSnapshot;

    expect(() => renderWithAuth(mismatchedSnapshot, null)).toThrow(
      "Auth snapshot user does not match cache partition owner",
    );
  });
});

describe("auth-web helpers", () => {
  it("resolves object, sync factory, and async factory auth config sources", async () => {
    await expect(resolveAuthConfig(testAuthConfig)).resolves.toBe(testAuthConfig);
    await expect(resolveAuthConfig(() => testAuthConfig)).resolves.toBe(testAuthConfig);
    await expect(resolveAuthConfig(async () => testAuthConfig)).resolves.toBe(testAuthConfig);
  });

  it("partitions query keys and Electric collection IDs by auth scope", () => {
    expect(authQueryKey(authenticatedAuth, "billing", "balance")).toEqual([
      "auth",
      "partition-123",
      "billing",
      "balance",
    ]);
    expect(authCollectionId(authenticatedAuth, "sync-executions-org-1")).toBe(
      "auth:partition-123:sync-executions-org-1",
    );
    expect(authCacheKey(authenticatedSnapshot)).toBe("auth:partition-123");
    expect(authCacheKey(anonymousSnapshot)).toBe("auth:anonymous");
  });

  it("normalizes serialized client auth snapshot session dates", () => {
    const snapshot = parseAuthSnapshot({
      ...authenticatedSnapshot,
      session: {
        createdAt: "2026-04-10T12:00:00.000Z",
        expiresAt: "2026-04-10T13:00:00.000Z",
      },
    });

    if (!snapshot.isSignedIn) {
      throw new Error("Expected a signed-in auth snapshot");
    }
    expect(snapshot.session.expiresAt.toISOString()).toBe("2026-04-10T13:00:00.000Z");
  });

  it("clears partitioned caches when the auth partition changes", () => {
    const cache = {
      clears: 0,
      clear() {
        this.clears += 1;
      },
    };
    const secondSnapshot: AuthSnapshot = {
      ...authenticatedSnapshot,
      auth: {
        ...authenticatedSnapshot.auth,
        cachePartition: "partition-456",
      },
    };

    syncAuthPartitionedCache(cache, anonymousSnapshot);
    syncAuthPartitionedCache(cache, anonymousSnapshot);
    syncAuthPartitionedCache(cache, authenticatedSnapshot);
    syncAuthPartitionedCache(cache, secondSnapshot);

    expect(cache.clears).toBe(2);
  });

  it("returns authenticated auth from requireAuth", () => {
    expect(requireAuth(authenticatedAuth, "/jobs")).toBe(authenticatedAuth);
  });

  it("uses provider-supplied login and logout redirect functions", async () => {
    const assignedURLs: string[] = [];
    const previousLocation = (
      globalThis as {
        location?: {
          assign: (url: string) => void;
          href: string;
        };
      }
    ).location;
    const clients: ReturnType<typeof useClerk>[] = [];

    function Probe() {
      clients.push(useClerk());
      return null;
    }

    Object.defineProperty(globalThis, "location", {
      configurable: true,
      value: {
        assign: (url: string) => assignedURLs.push(url),
        href: "https://rent.example.com/jobs",
      },
    });

    try {
      renderToString(
        createElement(
          AuthProvider,
          {
            client: {
              getSignInRedirectURL: async ({ data }) =>
                `/login/redirect?to=${encodeURIComponent(data.redirectTo ?? "")}`,
              getSignOutRedirectURL: async () => "/logout/redirect",
            },
            snapshot: authenticatedSnapshot,
          },
          createElement(Probe),
        ),
      );

      const client = one(clients);
      await client.redirectToSignIn();
      await client.redirectToSignOut();

      expect(assignedURLs).toEqual([
        "/login/redirect?to=https%3A%2F%2Frent.example.com%2Fjobs",
        "/logout/redirect",
      ]);
    } finally {
      if (previousLocation) {
        Object.defineProperty(globalThis, "location", {
          configurable: true,
          value: previousLocation,
        });
      } else {
        delete (globalThis as { location?: unknown }).location;
      }
    }
  });
});
