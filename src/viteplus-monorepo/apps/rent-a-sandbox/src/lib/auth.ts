import { UserManager, WebStorageStateStore, type User } from "oidc-client-ts";

// Server-side process.env is available via Nitro; client reads from
// window.__ENV__ injected by the root layout's SSR script tag.
declare const process: { env: Record<string, string | undefined> } | undefined;

const AUTH_ISSUER_URL =
  typeof window !== "undefined"
    ? ((window as any).__ENV__?.AUTH_ISSUER_URL ?? "https://auth.anveio.com")
    : typeof process !== "undefined"
      ? (process.env.AUTH_ISSUER_URL ?? "https://auth.anveio.com")
      : "https://auth.anveio.com";

const AUTH_CLIENT_ID =
  typeof window !== "undefined"
    ? ((window as any).__ENV__?.AUTH_CLIENT_ID ?? "")
    : typeof process !== "undefined"
      ? (process.env.AUTH_CLIENT_ID ?? "")
      : "";

const REDIRECT_URI =
  typeof window !== "undefined"
    ? `${window.location.origin}/callback`
    : "https://rentasandbox.anveio.com/callback";

const SILENT_REDIRECT_URI =
  typeof window !== "undefined"
    ? `${window.location.origin}/auth/silent-callback`
    : "https://rentasandbox.anveio.com/auth/silent-callback";

const POST_LOGOUT_URI =
  typeof window !== "undefined" ? window.location.origin : "https://rentasandbox.anveio.com";

let _userManager: UserManager | null = null;
let _silentRenewInFlight: Promise<User | null> | null = null;

export class AuthenticationRequiredError extends Error {
  constructor(message = "Authentication required") {
    super(message);
    this.name = "AuthenticationRequiredError";
  }
}

export function getUserManager(): UserManager {
  if (_userManager) return _userManager;
  _userManager = new UserManager({
    authority: AUTH_ISSUER_URL,
    client_id: AUTH_CLIENT_ID,
    redirect_uri: REDIRECT_URI,
    silent_redirect_uri: SILENT_REDIRECT_URI,
    post_logout_redirect_uri: POST_LOGOUT_URI,
    response_type: "code",
    // Request refresh-capable sessions when the provider allows them. The
    // manager will still fall back to iframe silent renew if needed.
    scope: "openid profile email offline_access urn:zitadel:iam:user:resourceowner",
    automaticSilentRenew: true,
    userStore: new WebStorageStateStore({ store: sessionStorage }),
  });
  return _userManager;
}

async function renewUser(): Promise<User | null> {
  if (_silentRenewInFlight) {
    return _silentRenewInFlight;
  }

  const manager = getUserManager();
  _silentRenewInFlight = (async () => {
    try {
      const user = await manager.signinSilent();
      if (!user || user.expired) {
        await manager.removeUser();
        return null;
      }
      return user;
    } catch {
      await manager.removeUser();
      return null;
    } finally {
      _silentRenewInFlight = null;
    }
  })();
  return _silentRenewInFlight;
}

export async function getUser(): Promise<User | null> {
  if (typeof window === "undefined") return null;
  const manager = getUserManager();
  const user = await manager.getUser();
  if (!user) return null;
  if (!user.expired) return user;
  return renewUser();
}

export async function getAccessToken(): Promise<string | null> {
  const user = await getUser();
  if (!user || user.expired) return null;
  return user.access_token;
}

export async function clearUser(): Promise<void> {
  await getUserManager().removeUser();
}

export function signIn(): Promise<void> {
  return getUserManager().signinRedirect();
}

export function signOut(): Promise<void> {
  return getUserManager().signoutRedirect();
}

export async function handleCallback(): Promise<User> {
  const user = await getUserManager().signinCallback();
  if (!user) throw new Error("OIDC callback returned no user");
  return user;
}

export async function handleSilentCallback(): Promise<void> {
  await getUserManager().signinSilentCallback();
}

export type { User };
