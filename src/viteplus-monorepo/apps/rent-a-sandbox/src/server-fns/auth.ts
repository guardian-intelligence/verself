import { createMiddleware } from "@tanstack/react-start";
import { createAuthServerFns, type AuthSession } from "@forge-metal/auth-web";
import { authConfig } from "../server/auth";

const verificationRunHeader = "X-Forge-Metal-Verification-Run";

export interface RentASandboxAuthContext {
  verificationRunID?: string;
  auth: AuthSession;
}

const authServerFns = createAuthServerFns(authConfig);

export const verificationRunMiddleware = createMiddleware({
  type: "request",
}).server(async ({ next, request }) => {
  const verificationRunID = request.headers.get(verificationRunHeader)?.trim() || undefined;
  return next({
    context: verificationRunID ? { verificationRunID } : {},
  });
});

export const rentASandboxAuthMiddleware = authServerFns.authMiddleware;
export const { getViewer, getLoginRedirectURL, getCallbackRedirectURL, getLogoutRedirectURL } =
  authServerFns;
