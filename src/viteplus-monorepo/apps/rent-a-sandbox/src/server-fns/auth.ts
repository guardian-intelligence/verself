import { createAuthServerFns } from "@forge-metal/auth-web/server-fns";

async function getRentASandboxAuthConfig() {
  if (!import.meta.env.SSR) {
    throw new Error("auth config is server-only");
  }
  const { getAuthConfig } = await import("../server/auth");
  return getAuthConfig();
}

export const {
  authMiddleware: rentASandboxAuthMiddleware,
  getClientAuthSnapshot,
  getSignInCallbackRedirectURL,
  getSignInRedirectURL,
  getSignOutRedirectURL,
} = createAuthServerFns(getRentASandboxAuthConfig);
