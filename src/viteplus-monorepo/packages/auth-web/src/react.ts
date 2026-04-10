import { createContext, createElement, type ReactNode, useContext } from "react";
import type { Auth, AuthSnapshot, AuthenticatedAuth, ClientUser, SessionInfo } from "./shared.ts";

export type {
  AnonymousAuth,
  Auth,
  AuthSnapshot,
  AuthenticatedAuth,
  AuthRoleAssignment,
  ClientUser,
  SessionInfo,
} from "./shared.ts";
export { authCollectionId, authQueryKey, loginRedirect, requireAuth } from "./shared.ts";

export interface AuthNavigationClient {
  getLoginRedirectURL?: (input: {
    data: {
      redirectTo?: string | null;
    };
  }) => Promise<string>;
  getLogoutRedirectURL?: () => Promise<string>;
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

export interface UseUserReturn {
  isLoaded: true;
  isSignedIn: boolean;
  user: ClientUser | null;
}

export interface UseSessionReturn {
  isLoaded: true;
  isSignedIn: boolean;
  session: SessionInfo | null;
}

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
  return createElement(
    AuthContext.Provider,
    { value: { client: client ?? null, snapshot } },
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

export function useAuthenticatedAuth(): AuthenticatedAuth {
  const auth = useAuth();
  if (!auth.isAuthenticated) {
    throw new Error("useAuthenticatedAuth() requires an authenticated auth snapshot");
  }
  return auth;
}

export function useUser(): UseUserReturn {
  const {
    snapshot: { auth, user },
  } = useAuthContextValue();
  return {
    isLoaded: true,
    isSignedIn: auth.isAuthenticated,
    user,
  };
}

export function useSession(): UseSessionReturn {
  const {
    snapshot: { auth, session },
  } = useAuthContextValue();
  return {
    isLoaded: true,
    isSignedIn: auth.isAuthenticated,
    session,
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
    throw new Error("useAuthClient() can only be used in the browser");
  }

  return location;
}

export function useAuthClient() {
  const { client } = useAuthContextValue();

  return {
    redirectToSignIn: async (options?: { redirectTo?: string }) => {
      if (!client?.getLoginRedirectURL) {
        throw new Error("AuthProvider is missing getLoginRedirectURL()");
      }

      const location = getBrowserLocation();
      const redirectTo = options?.redirectTo ?? location.href;
      const href = await client.getLoginRedirectURL({
        data: redirectTo ? { redirectTo } : {},
      });
      location.assign(href);
    },
    redirectToSignOut: async () => {
      if (!client?.getLogoutRedirectURL) {
        throw new Error("AuthProvider is missing getLogoutRedirectURL()");
      }

      const location = getBrowserLocation();
      const href = await client.getLogoutRedirectURL();
      location.assign(href);
    },
  };
}
