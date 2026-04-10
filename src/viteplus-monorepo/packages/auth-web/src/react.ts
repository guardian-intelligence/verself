import { createContext, createElement, type ReactNode, useContext } from "react";
import { parseAuthSnapshot } from "./isomorphic.ts";
import type {
  Auth,
  AuthSnapshot,
  AuthenticatedAuth,
  ClientUser,
  SessionInfo,
} from "./isomorphic.ts";

export type {
  AnonymousAuth,
  Auth,
  AuthSnapshot,
  AuthenticatedAuth,
  AuthRoleAssignment,
  ClientUser,
  SessionInfo,
} from "./isomorphic.ts";
export {
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
      redirectTo?: string | null;
    };
  }) => Promise<string>;
  getSignOutRedirectURL?: () => Promise<string>;
}

interface AuthContextValue {
  client: AuthNavigationClient | null;
  snapshot: AuthSnapshot;
}

export interface UseAuthSignedOut extends Extract<Auth, { isAuthenticated: false }> {
  has: (params: { role?: string }) => boolean;
  isLoaded: true;
  isSignedIn: false;
}

export interface UseAuthSignedIn extends AuthenticatedAuth {
  has: (params: { role?: string }) => boolean;
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
  snapshot: AuthSnapshot;
}

const AuthContext = createContext<AuthContextValue | null>(null);

function authHas(auth: Auth, params: { role?: string }): boolean {
  if (!auth.isAuthenticated) {
    return false;
  }
  return params.role ? auth.roles.includes(params.role) : true;
}

function useAuthContextValue(): AuthContextValue {
  const value = useContext(AuthContext);
  if (!value) {
    throw new Error("AuthProvider is required");
  }
  return value;
}

export function AuthProvider({ children, client, snapshot }: AuthProviderProps) {
  const parsedSnapshot = parseAuthSnapshot(snapshot);
  return createElement(
    AuthContext.Provider,
    { value: { client: client ?? null, snapshot: parsedSnapshot } },
    children,
  );
}

export function useAuth(): UseAuthReturn {
  const {
    snapshot: { auth },
  } = useAuthContextValue();
  const has = (params: { role?: string }) => authHas(auth, params);

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
    redirectToSignIn: async (options?: { redirectTo?: string }) => {
      if (!client?.getSignInRedirectURL) {
        throw new Error("AuthProvider is missing getSignInRedirectURL()");
      }

      const location = getBrowserLocation();
      const redirectTo = options?.redirectTo ?? location.href;
      const href = await client.getSignInRedirectURL({
        data: redirectTo ? { redirectTo } : {},
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
