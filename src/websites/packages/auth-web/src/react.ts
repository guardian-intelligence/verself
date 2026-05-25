import { useQueryClient } from "@tanstack/react-query";
import { useHydrated } from "@tanstack/react-router";
import { useLiveQuery, useLiveQueryEffect } from "@tanstack/react-db";
import { Fragment, createContext, createElement, type ReactNode, useContext, useMemo } from "react";
import {
  clearLiveAuthCollections,
  createLiveAuthCollections,
  type LiveAuthCollections,
} from "./live.ts";
import {
  anonymousSnapshot,
  parseAuthSnapshot,
  snapshotFromLiveAuth,
  syncAuthPartitionedCache,
} from "./isomorphic.ts";
import type {
  Auth,
  AuthSnapshot,
  AuthenticatedAuth,
  ClientUser,
  SessionInfo,
  WebAuthClient,
} from "./isomorphic.ts";

export type {
  AnonymousAuth,
  Auth,
  AuthSnapshot,
  AuthenticatedAuth,
  AuthOrganizationContext,
  BrowserDevice,
  BrowserLocation,
  ClientUser,
  SessionInfo,
  WebAuthAccount,
  WebAuthClient,
  WebAuthSession,
} from "./isomorphic.ts";
export {
  anonymousSnapshot,
  authCacheKey,
  authCollectionId,
  authQueryKey,
  loginRedirect,
  parseAuthSnapshot,
  requireAuth,
  syncAuthPartitionedCache,
} from "./isomorphic.ts";

export interface AuthNavigationClient {
  getSignInRedirectURL?: (input: {
    data: {
      promptLogin?: boolean | null;
      promptSelectAccount?: boolean | null;
      redirectTo?: string | null;
      purpose?: string | null;
      loginHint?: string | null;
      requiredSubject?: string | null;
      requiredEmail?: string | null;
      requiredOrgId?: string | null;
    };
  }) => Promise<string>;
  getSignOutRedirectURL?: () => Promise<string>;
}

interface AuthContextValue {
  client: AuthNavigationClient | null;
  collections: LiveAuthCollections | null;
  snapshot: AuthSnapshot;
}

export interface UseAuthSignedOut extends Extract<Auth, { isAuthenticated: false }> {
  has: () => boolean;
  isLoaded: true;
  isSignedIn: false;
}

export interface UseAuthSignedIn extends AuthenticatedAuth {
  has: () => boolean;
  isLoaded: true;
  isSignedIn: true;
}

export type UseAuthReturn = UseAuthSignedOut | UseAuthSignedIn;

export type UseUserReturn =
  | {
      isLoaded: true;
      isSignedIn: false;
      user: null;
    }
  | {
      isLoaded: true;
      isSignedIn: true;
      user: ClientUser;
    };

export type UseSessionReturn =
  | {
      isLoaded: true;
      isSignedIn: false;
      session: null;
    }
  | {
      isLoaded: true;
      isSignedIn: true;
      session: SessionInfo;
    };

export interface AuthProviderProps {
  children?: ReactNode;
  client?: AuthNavigationClient;
  live?: boolean;
  snapshot: AuthSnapshot;
}

const AuthContext = createContext<AuthContextValue | null>(null);

type AuthProviderInnerProps = {
  children?: ReactNode;
  client?: AuthNavigationClient | undefined;
  snapshot: AuthSnapshot;
};

function authHas(auth: Auth): boolean {
  return auth.isAuthenticated;
}

function useAuthContextValue(): AuthContextValue {
  const value = useContext(AuthContext);
  if (!value) {
    throw new Error("AuthProvider is required");
  }
  return value;
}

export function AuthProvider({ children, client, live = false, snapshot }: AuthProviderProps) {
  const parsedSnapshot = parseAuthSnapshot(snapshot);
  if (!live) {
    return createElement(StaticAuthProvider, {
      children,
      client,
      snapshot: parsedSnapshot,
    });
  }
  return createElement(LiveAuthProvider, {
    children,
    client,
    snapshot: parsedSnapshot,
  });
}

function StaticAuthProvider({ children, client, snapshot }: AuthProviderInnerProps) {
  return createElement(
    AuthContext.Provider,
    { value: { client: client ?? null, collections: null, snapshot } },
    children,
  );
}

function LiveAuthProvider({ children, client, snapshot }: AuthProviderInnerProps) {
  const hydrated = useHydrated();
  if (!hydrated) {
    return createElement(StaticAuthProvider, { children, client, snapshot });
  }
  return createElement(HydratedLiveAuthProvider, { children, client, snapshot });
}

function HydratedLiveAuthProvider({ children, client, snapshot }: AuthProviderInnerProps) {
  const queryClient = useQueryClient();
  const collections = useMemo(
    () =>
      createLiveAuthCollections(() => {
        queryClient.clear();
      }),
    [queryClient],
  );

  const clientQuery = useLiveQuery(() => collections.client, [collections]);
  const sessionsQuery = useLiveQuery(() => collections.sessions, [collections]);

  const liveClient =
    clientQuery.isReady && !clientQuery.isError ? clientQuery.data?.[0] : undefined;
  const currentSession =
    sessionsQuery.isReady && !sessionsQuery.isError
      ? sessionsQuery.data?.find((session) => session.is_current)
      : undefined;

  const liveSnapshot =
    clientQuery.isError || sessionsQuery.isError
      ? anonymousSnapshot
      : clientQuery.isReady
        ? snapshotFromLiveAuth(liveClient, currentSession)
        : snapshot;

  return createElement(
    AuthContext.Provider,
    { value: { client: client ?? null, collections, snapshot: liveSnapshot } },
    createElement(AuthPartitionSync, { collections }, children),
  );
}

function AuthPartitionSync({
  children,
  collections,
}: {
  children?: ReactNode;
  collections: LiveAuthCollections;
}) {
  const queryClient = useQueryClient();
  useLiveQueryEffect<WebAuthClient, string>(
    {
      query: (q) => q.from({ client: collections.client }),
      skipInitial: false,
      onEnter: ({ value }) =>
        syncAuthPartitionedCache(queryClient, snapshotFromLiveAuth(value, undefined)),
      onUpdate: ({ value }) =>
        syncAuthPartitionedCache(queryClient, snapshotFromLiveAuth(value, undefined)),
      onExit: () => syncAuthPartitionedCache(queryClient, anonymousSnapshot),
      onSourceError: () => {
        void clearLiveAuthCollections(collections);
        queryClient.clear();
      },
    },
    [collections, queryClient],
  );
  return createElement(Fragment, null, children);
}

export function useAuth(): UseAuthReturn {
  const {
    snapshot: { auth },
  } = useAuthContextValue();
  const has = () => authHas(auth);

  if (!auth.isAuthenticated) {
    return {
      ...auth,
      has,
      isLoaded: true,
      isSignedIn: false,
    };
  }

  return {
    ...auth,
    has,
    isLoaded: true,
    isSignedIn: true,
  };
}

export function useSignedInAuth(): AuthenticatedAuth {
  const auth = useAuth();
  if (!auth.isAuthenticated) {
    throw new Error("useSignedInAuth() requires a signed-in auth snapshot");
  }
  return auth;
}

export function useUser(): UseUserReturn {
  const { snapshot } = useAuthContextValue();
  if (!snapshot.isSignedIn) {
    return {
      isLoaded: true,
      isSignedIn: false,
      user: null,
    };
  }
  return {
    isLoaded: true,
    isSignedIn: true,
    user: snapshot.user,
  };
}

export function useSession(): UseSessionReturn {
  const { snapshot } = useAuthContextValue();
  if (!snapshot.isSignedIn) {
    return {
      isLoaded: true,
      isSignedIn: false,
      session: null,
    };
  }
  return {
    isLoaded: true,
    isSignedIn: true,
    session: snapshot.session,
  };
}

export function useAuthCollections(): LiveAuthCollections {
  const { collections } = useAuthContextValue();
  if (!collections) {
    throw new Error("Live auth collections are not enabled");
  }
  return collections;
}

function getBrowserLocation() {
  const location = (
    globalThis as {
      location?: {
        assign: (url: string) => void;
        href: string;
      };
    }
  ).location;

  if (!location) {
    throw new Error("useClerk() can only be used in the browser");
  }

  return location;
}

export function useClerk() {
  const { client } = useAuthContextValue();

  return {
    redirectToSignIn: async (options?: {
      promptLogin?: boolean;
      promptSelectAccount?: boolean;
      redirectTo?: string;
      purpose?: string;
      loginHint?: string;
      requiredSubject?: string;
      requiredEmail?: string;
      requiredOrgId?: string;
    }) => {
      if (!client?.getSignInRedirectURL) {
        throw new Error("AuthProvider is missing getSignInRedirectURL()");
      }

      const location = getBrowserLocation();
      const redirectTo = options?.redirectTo ?? location.href;
      const href = await client.getSignInRedirectURL({
        data: {
          ...(options?.promptLogin ? { promptLogin: true } : {}),
          ...(options?.promptSelectAccount ? { promptSelectAccount: true } : {}),
          ...(redirectTo ? { redirectTo } : {}),
          ...(options?.purpose ? { purpose: options.purpose } : {}),
          ...(options?.loginHint ? { loginHint: options.loginHint } : {}),
          ...(options?.requiredSubject ? { requiredSubject: options.requiredSubject } : {}),
          ...(options?.requiredEmail ? { requiredEmail: options.requiredEmail } : {}),
          ...(options?.requiredOrgId ? { requiredOrgId: options.requiredOrgId } : {}),
        },
      });
      location.assign(href);
    },
    redirectToSignOut: async () => {
      if (!client?.getSignOutRedirectURL) {
        throw new Error("AuthProvider is missing getSignOutRedirectURL()");
      }

      const location = getBrowserLocation();
      const href = await client.getSignOutRedirectURL();
      location.assign(href);
    },
  };
}
