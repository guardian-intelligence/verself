import { createAuthServerFns } from "@forge-metal/auth-web/server-fns";

const authServerFns = createAuthServerFns(async () => {
  if (!import.meta.env.SSR) {
    throw new Error("auth config is server-only");
  }
  const { getAuthConfig } = await import("../server/auth");
  return getAuthConfig();
});

export const rentASandboxAuthMiddleware = authServerFns.authMiddleware;
export const {
  getAuthSnapshot,
  getLoginRedirectURL,
  getCallbackRedirectURL,
  getLogoutRedirectURL,
} = authServerFns;
