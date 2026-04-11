import { createMiddleware, createServerFn } from "@tanstack/react-start";
import * as v from "valibot";
import type { AuthSession } from "@forge-metal/auth-web/server";

const loginRedirectInputSchema = v.object({
  redirectTo: v.optional(v.nullable(v.string())),
});

async function loadAuthServer() {
  return import("@forge-metal/auth-web/server");
}

async function getRentASandboxAuthConfig() {
  if (!import.meta.env.SSR) {
    throw new Error("auth config is server-only");
  }
  const { getAuthConfig } = await import("../server/auth");
  return getAuthConfig();
}

// TanStack Start resolves server functions by top-level export name; factories hide those exports from the generated resolver.
export const rentASandboxAuthMiddleware = createMiddleware({ type: "function" }).server(
  async ({ next }) => {
    const { getAuthSession } = await loadAuthServer();
    const auth = await getAuthSession(await getRentASandboxAuthConfig());
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
  const { getClientAuthSnapshot } = await loadAuthServer();
  return getClientAuthSnapshot(getRentASandboxAuthConfig);
});

export const getSignInRedirectURL = createServerFn({ method: "GET" })
  .inputValidator(loginRedirectInputSchema)
  .handler(async ({ data }) => {
    const { beginLogin } = await loadAuthServer();
    return beginLogin(await getRentASandboxAuthConfig(), data.redirectTo);
  });

export const getSignInCallbackRedirectURL = createServerFn({ method: "GET" }).handler(async () => {
  const { finishLogin } = await loadAuthServer();
  const { redirectTo } = await finishLogin(await getRentASandboxAuthConfig());
  return redirectTo;
});

export const getSignOutRedirectURL = createServerFn({ method: "GET" }).handler(async () => {
  const { logout } = await loadAuthServer();
  return logout(await getRentASandboxAuthConfig());
});
