import { createMiddleware, createServerFn } from "@tanstack/react-start";
import type { AuthSession } from "@forge-metal/auth-web";
import {
  beginLettersLogin,
  finishLettersLogin,
  getLettersAuthSession,
  getLettersAuthUser,
  logoutLetters,
} from "../server/auth";

export interface LettersAuthContext {
  auth: AuthSession;
}

export const lettersAuthMiddleware = createMiddleware({ type: "function" }).server(
  async ({ next }) => {
    const auth = await getLettersAuthSession();
    if (!auth) {
      throw new Error("Authentication required");
    }
    return next({
      context: {
        auth,
      } satisfies LettersAuthContext,
    });
  },
);

export const getViewer = createServerFn({ method: "GET" }).handler(async () => {
  const user = await getLettersAuthUser();
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
    return beginLettersLogin(data.redirectTo);
  });

export const getCallbackRedirectURL = createServerFn({ method: "GET" }).handler(async () => {
  const { redirectTo } = await finishLettersLogin();
  return redirectTo;
});

export const getLogoutRedirectURL = createServerFn({ method: "GET" }).handler(async () => {
  return logoutLetters();
});
