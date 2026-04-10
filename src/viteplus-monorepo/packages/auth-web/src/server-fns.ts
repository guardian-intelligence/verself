import { createMiddleware, createServerFn } from "@tanstack/react-start";
import * as v from "valibot";
import { resolveAuthConfigAsync } from "./config.ts";
import type { AsyncAuthConfigSource } from "./config.ts";
import type { AuthSession } from "./shared.ts";

const loginRedirectInputSchema = v.object({
  redirectTo: v.optional(v.nullable(v.string())),
});

export function createAuthServerFns(config: AsyncAuthConfigSource) {
  const authMiddleware = createMiddleware({ type: "function" }).server(async ({ next }) => {
    const { getAuthSession } = await import("./index.ts");
    const auth = await getAuthSession(await resolveAuthConfigAsync(config));
    if (!auth) {
      throw new Error("Authentication required");
    }
    return next({
      context: {
        auth,
      } satisfies { auth: AuthSession },
    });
  });

  const getAuthSnapshot = createServerFn({ method: "GET" }).handler(async () => {
    const { getAuthSnapshot } = await import("./index.ts");
    return getAuthSnapshot(await resolveAuthConfigAsync(config));
  });
  const getLoginRedirectURL = createServerFn({ method: "GET" })
    .inputValidator(loginRedirectInputSchema)
    .handler(async ({ data }) => {
      const { beginLogin } = await import("./index.ts");
      return beginLogin(await resolveAuthConfigAsync(config), data.redirectTo);
    });
  const getCallbackRedirectURL = createServerFn({ method: "GET" }).handler(async () => {
    const { finishLogin } = await import("./index.ts");
    const { redirectTo } = await finishLogin(await resolveAuthConfigAsync(config));
    return redirectTo;
  });
  const getLogoutRedirectURL = createServerFn({ method: "GET" }).handler(async () => {
    const { logout } = await import("./index.ts");
    return logout(await resolveAuthConfigAsync(config));
  });

  return {
    authMiddleware,
    getAuthSnapshot,
    getLoginRedirectURL,
    getCallbackRedirectURL,
    getLogoutRedirectURL,
  };
}
