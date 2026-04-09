import { createMiddleware, createServerFn } from "@tanstack/react-start";
import type { AuthSession } from "@forge-metal/auth-web";
import {
  beginWebmailLogin,
  finishWebmailLogin,
  getWebmailAuthSession,
  getWebmailAuthUser,
  logoutWebmail,
} from "../server/auth";

export interface WebmailAuthContext {
  auth: AuthSession;
}

export const webmailAuthMiddleware = createMiddleware({ type: "function" }).server(
  async ({ next }) => {
    const auth = await getWebmailAuthSession();
    if (!auth) {
      throw new Error("Authentication required");
    }
    return next({
      context: {
        auth,
      } satisfies WebmailAuthContext,
    });
  },
);

export const getViewer = createServerFn({ method: "GET" }).handler(async () => {
  const user = await getWebmailAuthUser();
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
    return beginWebmailLogin(data.redirectTo);
  });

export const getCallbackRedirectURL = createServerFn({ method: "GET" }).handler(async () => {
  const { redirectTo } = await finishWebmailLogin();
  return redirectTo;
});

export const getLogoutRedirectURL = createServerFn({ method: "GET" }).handler(async () => {
  return logoutWebmail();
});
