import { createMiddleware, createServerFn } from "@tanstack/react-start";
import * as v from "valibot";
import type { AuthSession } from "@forge-metal/auth-web/shared";
import { getAuthConfig } from "../server/auth";

const loginRedirectInputSchema = v.object({
  redirectTo: v.optional(v.nullable(v.string())),
});

async function loadAuthServer() {
  return import("@forge-metal/auth-web/server");
}

// TanStack Start transforms top-level createServerFn declarations, but the
// server-only auth module must stay behind dynamic imports for the browser graph.
export const webmailAuthMiddleware = createMiddleware({ type: "function" }).server(
  async ({ next }) => {
    const { getAuthSession } = await loadAuthServer();
    const auth = await getAuthSession(getAuthConfig());
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
export const getLoginRedirectURL = createServerFn({ method: "GET" })
  .inputValidator(loginRedirectInputSchema)
  .handler(async ({ data }) => {
    const { beginLogin } = await loadAuthServer();
    return beginLogin(getAuthConfig(), data.redirectTo);
  });
export const getCallbackRedirectURL = createServerFn({ method: "GET" }).handler(async () => {
  const { finishLogin } = await loadAuthServer();
  const { redirectTo } = await finishLogin(getAuthConfig());
  return redirectTo;
});
export const getLogoutRedirectURL = createServerFn({ method: "GET" }).handler(async () => {
  const { logout } = await loadAuthServer();
  return logout(getAuthConfig());
});
export const getViewer = createServerFn({ method: "GET" }).handler(async () => {
  const { getAuthSnapshot } = await loadAuthServer();
  const snapshot = await getAuthSnapshot(getAuthConfig);
  return snapshot.user;
});
