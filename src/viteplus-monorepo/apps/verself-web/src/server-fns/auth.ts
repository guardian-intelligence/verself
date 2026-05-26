import { createMiddleware, createServerFn, createServerOnlyFn } from "@tanstack/react-start";
import * as v from "valibot";
import type { AuthenticatedAuthSnapshot } from "@verself/auth-web/isomorphic";

const selectOrganizationInputSchema = v.object({
  orgID: v.pipe(v.string(), v.nonEmpty()),
});

const revokeDeviceInputSchema = v.object({
  sessionHandle: v.pipe(v.string(), v.nonEmpty()),
});

const selectAccountInputSchema = v.object({
  accountHandle: v.pipe(v.string(), v.nonEmpty()),
});

const acceptMemberInviteInputSchema = v.object({
  token: v.pipe(v.string(), v.nonEmpty()),
});

const verifySignupInputSchema = v.object({
  signupIntentId: v.pipe(v.string(), v.nonEmpty()),
  verificationToken: v.pipe(v.string(), v.nonEmpty()),
  organizationDisplayName: v.pipe(v.string(), v.trim(), v.minLength(1), v.maxLength(120)),
  organizationSlug: v.pipe(
    v.string(),
    v.trim(),
    v.minLength(1),
    v.maxLength(80),
    v.regex(/^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/),
  ),
});

const checkOrganizationSlugInputSchema = v.object({
  slug: v.pipe(
    v.string(),
    v.trim(),
    v.minLength(1),
    v.maxLength(80),
    v.regex(/^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/),
  ),
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

export const selectActiveAccount = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(selectAccountInputSchema)
  .handler(async ({ data }) => {
    const { selectIdentityBrowserAccount } = await import("./auth.server");
    return selectIdentityBrowserAccount(data.accountHandle);
  });

export const revokeClientAuthDevice = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(revokeDeviceInputSchema)
  .handler(async ({ data }) => {
    const { revokeIdentityBrowserDevice } = await import("./auth.server");
    await revokeIdentityBrowserDevice(data.sessionHandle);
    return { revoked: true };
  });

export const acceptMemberInvite = createServerFn({ method: "POST" })
  .inputValidator(acceptMemberInviteInputSchema)
  .handler(async ({ data }) => {
    const { acceptIdentityMemberInvite } = await import("./auth.server");
    return acceptIdentityMemberInvite(data);
  });

export const verifySignup = createServerFn({ method: "POST" })
  .inputValidator(verifySignupInputSchema)
  .handler(async ({ data }) => {
    const { verifyIdentitySignup } = await import("./auth.server");
    return verifyIdentitySignup(data);
  });

export const checkOrganizationSlug = createServerFn({ method: "GET" })
  .inputValidator(checkOrganizationSlugInputSchema)
  .handler(async ({ data }) => {
    const { checkIdentityOrganizationSlugAvailability } = await import("./auth.server");
    return checkIdentityOrganizationSlugAvailability(data.slug);
  });

export const getProductAccessToken = createServerOnlyFn(async function getProductAccessToken(
  context: ConsoleAuthContext | undefined,
): Promise<string> {
  const { getIdentityProductAccessToken } = await import("./auth.server");
  return getIdentityProductAccessToken(context);
});
