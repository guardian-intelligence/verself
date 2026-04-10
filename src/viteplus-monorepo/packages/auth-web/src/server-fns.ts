import { createMiddleware, createServerFn } from "@tanstack/react-start";
import * as v from "valibot";
import { resolveAuthConfig, type AuthConfigSource } from "./config.ts";
import type { AuthSession } from "./index.ts";

const loginRedirectInputSchema = v.object({
  redirectTo: v.optional(v.nullable(v.string())),
});

async function loadAuthServer() {
  return import("./index.ts");
}

export function createAuthServerFns(config: AuthConfigSource) {
  const authMiddleware = createMiddleware({ type: "function" }).server(async ({ next }) => {
    const { getAuthSession } = await loadAuthServer();
    const auth = await getAuthSession(await resolveAuthConfig(config));
    if (!auth) {
      throw new Error("Authentication required");
    }
    return next({
      context: {
        auth,
      } satisfies { auth: AuthSession },
    });
  });

  const getClientAuthSnapshot = createServerFn({ method: "GET" }).handler(async () => {
    const { getClientAuthSnapshot } = await loadAuthServer();
    return getClientAuthSnapshot(config);
  });

  const getSignInRedirectURL = createServerFn({ method: "GET" })
    .inputValidator(loginRedirectInputSchema)
    .handler(async ({ data }) => {
      const { beginLogin } = await loadAuthServer();
      return beginLogin(await resolveAuthConfig(config), data.redirectTo);
    });

  const getSignInCallbackRedirectURL = createServerFn({ method: "GET" }).handler(async () => {
    const { finishLogin } = await loadAuthServer();
    const { redirectTo } = await finishLogin(await resolveAuthConfig(config));
    return redirectTo;
  });

  const getSignOutRedirectURL = createServerFn({ method: "GET" }).handler(async () => {
    const { logout } = await loadAuthServer();
    return logout(await resolveAuthConfig(config));
  });

  return {
    authMiddleware,
    getClientAuthSnapshot,
    getSignInCallbackRedirectURL,
    getSignInRedirectURL,
    getSignOutRedirectURL,
  };
}
