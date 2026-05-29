import { createMiddleware, createServerFn, createServerOnlyFn } from "@tanstack/react-start";
import * as v from "valibot";
import type { AuthenticatedAuthSnapshot } from "@verself/auth-web/isomorphic";
import { MIN_PASSWORD_LENGTH, PASSWORD_LENGTH_MESSAGE } from "../features/auth/password-policy";

const selectOrganizationInputSchema = v.object({
  orgID: v.pipe(v.string(), v.nonEmpty()),
});

const revokeDeviceInputSchema = v.object({
  sessionHandle: v.pipe(v.string(), v.nonEmpty()),
});

const selectAccountInputSchema = v.object({
  accountHandle: v.pipe(v.string(), v.nonEmpty()),
});

const removeAccountInputSchema = v.object({
  accountHandle: v.pipe(v.string(), v.nonEmpty()),
});

const acceptMemberInviteInputSchema = v.object({
  token: v.pipe(v.string(), v.nonEmpty()),
});

const passwordLoginInputSchema = v.object({
  email: v.pipe(v.string(), v.trim(), v.email()),
  password: v.pipe(v.string(), v.nonEmpty()),
  authRequestId: v.optional(v.string()),
  redirectTo: v.optional(v.string()),
  purpose: v.optional(v.string()),
  loginHint: v.optional(v.string()),
  requiredSubject: v.optional(v.string()),
  requiredEmail: v.optional(v.string()),
  requiredOrgId: v.optional(v.string()),
  prompt: v.optional(v.picklist(["login", "select_account"])),
});

const githubLoginStartInputSchema = v.object({
  redirectTo: v.optional(v.string()),
  purpose: v.optional(v.string()),
});

const passwordResetStartInputSchema = v.object({
  email: v.pipe(v.string(), v.trim(), v.email()),
});

const passwordResetCompleteInputSchema = v.object({
  userId: v.pipe(v.string(), v.trim(), v.nonEmpty()),
  verificationCode: v.pipe(v.string(), v.trim(), v.nonEmpty()),
  password: v.pipe(
    v.string(),
    v.nonEmpty(),
    v.minLength(MIN_PASSWORD_LENGTH, PASSWORD_LENGTH_MESSAGE),
  ),
});

const deviceLoginLookupInputSchema = v.object({
  userCode: v.pipe(v.string(), v.trim(), v.nonEmpty()),
});

const deviceLoginDecisionInputSchema = v.object({
  deviceLoginHandle: v.pipe(v.string(), v.trim(), v.nonEmpty()),
});

const verifySignupInputSchema = v.object({
  signupIntentId: v.pipe(v.string(), v.nonEmpty()),
  verificationToken: v.pipe(v.string(), v.nonEmpty()),
  initialPassword: v.pipe(
    v.string(),
    v.nonEmpty(),
    v.minLength(MIN_PASSWORD_LENGTH, PASSWORD_LENGTH_MESSAGE),
  ),
  organizationDisplayName: v.pipe(v.string(), v.trim(), v.minLength(1), v.maxLength(120)),
  organizationSlug: v.pipe(
    v.string(),
    v.trim(),
    v.minLength(1),
    v.maxLength(80),
    v.regex(/^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/),
  ),
});

const startSignupInputSchema = v.object({
  email: v.pipe(v.string(), v.trim(), v.email()),
  organizationDisplayName: v.pipe(v.string(), v.trim(), v.minLength(1), v.maxLength(120)),
  organizationSlug: v.optional(
    v.pipe(
      v.string(),
      v.trim(),
      v.minLength(1),
      v.maxLength(80),
      v.regex(/^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/),
    ),
  ),
  givenName: v.optional(v.pipe(v.string(), v.trim(), v.maxLength(100))),
  familyName: v.optional(v.pipe(v.string(), v.trim(), v.maxLength(100))),
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
  .inputValidator(selectAccountInputSchema)
  .handler(async ({ data }) => {
    const { selectIdentityBrowserAccount } = await import("./auth.server");
    return selectIdentityBrowserAccount(data.accountHandle);
  });

export const listBrowserAccounts = createServerFn({ method: "GET" }).handler(async () => {
  const { listIdentityBrowserAccounts } = await import("./auth.server");
  return listIdentityBrowserAccounts();
});

export const removeBrowserAccount = createServerFn({ method: "POST" })
  .inputValidator(removeAccountInputSchema)
  .handler(async ({ data }) => {
    const { removeIdentityBrowserAccount } = await import("./auth.server");
    await removeIdentityBrowserAccount(data.accountHandle);
    return { removed: true };
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

export const passwordLogin = createServerFn({ method: "POST" })
  .inputValidator(passwordLoginInputSchema)
  .handler(async ({ data }) => {
    const { completeIdentityPasswordLogin } = await import("./auth.server");
    return completeIdentityPasswordLogin(data);
  });

export const startGithubLogin = createServerFn({ method: "POST" })
  .inputValidator(githubLoginStartInputSchema)
  .handler(async ({ data }) => {
    const { startIdentityGithubLogin } = await import("./auth.server");
    return startIdentityGithubLogin(data);
  });

export const startPasswordReset = createServerFn({ method: "POST" })
  .inputValidator(passwordResetStartInputSchema)
  .handler(async ({ data }) => {
    const { startIdentityPasswordReset } = await import("./auth.server");
    return startIdentityPasswordReset(data.email);
  });

export const completePasswordReset = createServerFn({ method: "POST" })
  .inputValidator(passwordResetCompleteInputSchema)
  .handler(async ({ data }) => {
    const { completeIdentityPasswordReset } = await import("./auth.server");
    return completeIdentityPasswordReset(data);
  });

export const lookupDeviceLogin = createServerFn({ method: "POST" })
  .inputValidator(deviceLoginLookupInputSchema)
  .handler(async ({ data }) => {
    const { lookupIdentityDeviceLogin } = await import("./auth.server");
    return lookupIdentityDeviceLogin(data.userCode);
  });

export const approveDeviceLogin = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(deviceLoginDecisionInputSchema)
  .handler(async ({ data }) => {
    const { approveIdentityDeviceLogin } = await import("./auth.server");
    return approveIdentityDeviceLogin(data.deviceLoginHandle);
  });

export const denyDeviceLogin = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(deviceLoginDecisionInputSchema)
  .handler(async ({ data }) => {
    const { denyIdentityDeviceLogin } = await import("./auth.server");
    return denyIdentityDeviceLogin(data.deviceLoginHandle);
  });

export const verifySignup = createServerFn({ method: "POST" })
  .inputValidator(verifySignupInputSchema)
  .handler(async ({ data }) => {
    const { verifyIdentitySignup } = await import("./auth.server");
    return verifyIdentitySignup(data);
  });

export const startSignup = createServerFn({ method: "POST" })
  .inputValidator(startSignupInputSchema)
  .handler(async ({ data }) => {
    const { startIdentitySignup } = await import("./auth.server");
    return startIdentitySignup(data);
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
