import { createMiddleware, createServerFn } from "@tanstack/react-start";
import * as v from "valibot";
import {
  beginLogin,
  finishLogin,
  getAuthSession,
  getClientAuthSnapshot as readClientAuthSnapshot,
  logout,
  type AuthSession,
} from "@forge-metal/auth-web/server";
import { getAuthConfig } from "../server/auth";

const loginRedirectInputSchema = v.object({
  redirectTo: v.optional(v.nullable(v.string())),
});

function getWebmailAuthConfig() {
  if (!import.meta.env.SSR) {
    throw new Error("auth config is server-only");
  }
  return getAuthConfig();
}

// TanStack Start resolves server functions by top-level export name; factories hide those exports from the generated resolver.
export const webmailAuthMiddleware = createMiddleware({ type: "function" }).server(
  async ({ next }) => {
    const auth = await getAuthSession(getWebmailAuthConfig());
    if (!auth) {
      throw new Error("Authentication required");
    }
    return next({
      context: {
        auth,
      } satisfies { auth: AuthSession },
    });
  },
);

export const getClientAuthSnapshot = createServerFn({ method: "GET" }).handler(async () => {
  return readClientAuthSnapshot(getWebmailAuthConfig);
});

export const getSignInRedirectURL = createServerFn({ method: "GET" })
  .inputValidator(loginRedirectInputSchema)
  .handler(async ({ data }) => {
    return beginLogin(getWebmailAuthConfig(), data.redirectTo);
  });

export const getSignInCallbackRedirectURL = createServerFn({ method: "GET" }).handler(async () => {
  const { redirectTo } = await finishLogin(getWebmailAuthConfig());
  return redirectTo;
});

export const getSignOutRedirectURL = createServerFn({ method: "GET" }).handler(async () => {
  return logout(getWebmailAuthConfig());
});
