import { createElement, type ReactNode } from "react";
import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vite-plus/test";
import {
  AuthProvider,
  useAuth,
  useAuthClient,
  useAuthenticatedAuth,
  useSession,
  useUser,
  type UseAuthReturn,
  type UseSessionReturn,
  type UseUserReturn,
} from "./react.ts";
import {
  authCollectionId,
  authQueryKey,
  requireAuth,
  type Auth,
  type AuthSnapshot,
} from "./shared.ts";

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
  auth: anonymousAuth,
  session: null,
  user: null,
};

const authenticatedSnapshot: AuthSnapshot = {
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
      useAuthenticatedAuth();
      return null;
    }

    expect(() => renderWithAuth(anonymousSnapshot, createElement(Probe))).toThrow(
      "useAuthenticatedAuth() requires an authenticated auth snapshot",
    );
  });
});

describe("auth-web helpers", () => {
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
    const clients: ReturnType<typeof useAuthClient>[] = [];

    function Probe() {
      clients.push(useAuthClient());
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
              getLoginRedirectURL: async ({ data }) =>
                `/login/redirect?to=${encodeURIComponent(data.redirectTo ?? "")}`,
              getLogoutRedirectURL: async () => "/logout/redirect",
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
