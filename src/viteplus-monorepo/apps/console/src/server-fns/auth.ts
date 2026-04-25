import { createMiddleware, createServerFn } from "@tanstack/react-start";
import * as v from "valibot";
import {
  beginLogin,
  finishLogin,
  getAuthSession,
  getClientAuthSnapshot as readClientAuthSnapshot,
  logout,
  selectOrganization,
  type AuthSession,
} from "@forge-metal/auth-web/server";
import { getAuthConfig } from "../server/auth";

const loginRedirectInputSchema = v.object({
  redirectTo: v.optional(v.nullable(v.string())),
});

const selectOrganizationInputSchema = v.object({
  orgID: v.pipe(v.string(), v.nonEmpty()),
});

function getConsoleAuthConfig() {
  if (!import.meta.env.SSR) {
    throw new Error("auth config is server-only");
  }
  return getAuthConfig();
}

// TanStack Start resolves server functions by top-level export name; factories hide those exports from the generated resolver.
export const consoleAuthMiddleware = createMiddleware({ type: "function" }).server(
  async ({ next }) => {
    const auth = await getAuthSession(getConsoleAuthConfig());
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
  return readClientAuthSnapshot(getConsoleAuthConfig);
});

export const getSignInRedirectURL = createServerFn({ method: "GET" })
  .inputValidator(loginRedirectInputSchema)
  .handler(async ({ data }) => {
    return beginLogin(getConsoleAuthConfig(), data.redirectTo);
  });

export const getSignInCallbackRedirectURL = createServerFn({ method: "GET" }).handler(async () => {
  const { redirectTo } = await finishLogin(getConsoleAuthConfig());
  return redirectTo;
});

export const getSignOutRedirectURL = createServerFn({ method: "GET" }).handler(async () => {
  return logout(getConsoleAuthConfig());
});

export const selectActiveOrganization = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(selectOrganizationInputSchema)
  .handler(async ({ data }) => {
    return selectOrganization(getConsoleAuthConfig(), data.orgID);
  });
