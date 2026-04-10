import { createAuthServerFns } from "@forge-metal/auth-web";
import { getAuthConfig } from "../server/auth";

const auth = createAuthServerFns(getAuthConfig);

export const rentASandboxAuthMiddleware = auth.authMiddleware;
export const {
  getAuthState,
  getViewer,
  getLoginRedirectURL,
  getCallbackRedirectURL,
  getLogoutRedirectURL,
} = auth;
