import { createAuthServerFns } from "@forge-metal/auth-web";
import { authConfig } from "../server/auth";

const authServerFns = createAuthServerFns(authConfig);

export const lettersAuthMiddleware = authServerFns.authMiddleware;
export const { getViewer, getLoginRedirectURL, getCallbackRedirectURL, getLogoutRedirectURL } =
  authServerFns;
