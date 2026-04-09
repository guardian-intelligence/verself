import { createMiddleware, createServerFn } from "@tanstack/react-start";
import type { AuthSession } from "@forge-metal/auth-web";
import {
  beginRentASandboxLogin,
  finishRentASandboxLogin,
  getRentASandboxAuthSession,
  getRentASandboxAuthUser,
  logoutRentASandbox,
} from "../server/auth";

const verificationRunHeader = "X-Forge-Metal-Verification-Run";

export interface RentASandboxAuthContext {
  verificationRunID?: string;
  auth: AuthSession;
}

export const verificationRunMiddleware = createMiddleware({ type: "request" }).server(
  async ({ next, request }) => {
    const verificationRunID = request.headers.get(verificationRunHeader)?.trim() || undefined;
    return next({
      context: verificationRunID ? { verificationRunID } : {},
    });
  },
);

export const rentASandboxAuthMiddleware = createMiddleware({ type: "function" }).server(
  async ({ next }) => {
    const auth = await getRentASandboxAuthSession();
    if (!auth) {
      throw new Error("Authentication required");
    }
    return next({
      context: {
        auth,
      } satisfies Pick<RentASandboxAuthContext, "auth">,
    });
  },
);

export const getViewer = createServerFn({ method: "GET" }).handler(async () => {
  const user = await getRentASandboxAuthUser();
  if (!user) {
    return null;
  }
  return {
    sub: user.sub,
    email: user.email,
    name: user.name,
    preferredUsername: user.preferredUsername,
    orgID: user.orgID,
    roles: user.roles,
  };
});

export const getLoginRedirectURL = createServerFn({ method: "GET" })
  .inputValidator((data: { redirectTo?: string | null }) => data)
  .handler(async ({ data }) => {
    return beginRentASandboxLogin(data.redirectTo);
  });

export const getCallbackRedirectURL = createServerFn({ method: "GET" }).handler(async () => {
  const { redirectTo } = await finishRentASandboxLogin();
  return redirectTo;
});

export const getLogoutRedirectURL = createServerFn({ method: "GET" }).handler(async () => {
  return logoutRentASandbox();
});
