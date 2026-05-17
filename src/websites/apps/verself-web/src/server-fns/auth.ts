import { createMiddleware, createServerFn, createServerOnlyFn } from "@tanstack/react-start";
import * as v from "valibot";
import type { AuthenticatedAuthSnapshot } from "@verself/auth-web/isomorphic";

const selectOrganizationInputSchema = v.object({
  orgID: v.pipe(v.string(), v.nonEmpty()),
});

const revokeSessionInputSchema = v.object({
  sessionHandle: v.pipe(v.string(), v.nonEmpty()),
});

export type ConsoleAuthContext = {
  auth?: AuthenticatedAuthSnapshot;
};

// TanStack Start resolves server functions by top-level export name; factories hide those exports from the generated resolver.
export const consoleAuthMiddleware = createMiddleware({ type: "function" }).server(
  async ({ next }) => {
    const { readAuthSnapshot } = await import("./auth.server");
    const snapshot = await readAuthSnapshot();
    if (!snapshot.isSignedIn) {
      throw new Error("Authentication required");
    }
    return next({
      context: {
        auth: snapshot,
      } satisfies ConsoleAuthContext,
    });
  },
);

export const getClientAuthSnapshot = createServerFn({ method: "GET" }).handler(async () => {
  const { readAuthSnapshot } = await import("./auth.server");
  return readAuthSnapshot();
});

export const selectActiveOrganization = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(selectOrganizationInputSchema)
  .handler(async ({ data }) => {
    const { selectIdentityOrganization } = await import("./auth.server");
    return selectIdentityOrganization(data);
  });

export const getClientAuthSessions = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async () => {
    const { listIdentityBrowserSessions } = await import("./auth.server");
    return listIdentityBrowserSessions();
  });

export const revokeClientAuthSession = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(revokeSessionInputSchema)
  .handler(async ({ data }) => {
    const { revokeIdentityBrowserSession } = await import("./auth.server");
    await revokeIdentityBrowserSession(data.sessionHandle);
    return { revoked: true };
  });

export const getProductAccessToken = createServerOnlyFn(async function getProductAccessToken(
  context: ConsoleAuthContext | undefined,
): Promise<string> {
  const { getIdentityProductAccessToken } = await import("./auth.server");
  return getIdentityProductAccessToken(context);
});
