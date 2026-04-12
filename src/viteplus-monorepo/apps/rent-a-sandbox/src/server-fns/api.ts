import { createServerFn } from "@tanstack/react-start";
import { requireURLFromEnv } from "@forge-metal/web-env";
import {
  IdentityApiError,
  getMembers as getMembersRequest,
  getMemberCapabilities as getMemberCapabilitiesRequest,
  getOperations as getOperationsRequest,
  getOrganization as getOrganizationRequest,
  inviteMember as inviteMemberRequest,
  inviteMemberRequestSchema,
  isIdentityApiError,
  putMemberCapabilities as putMemberCapabilitiesRequest,
  putMemberCapabilitiesRequestSchema,
  updateMemberRoles as updateMemberRolesRequest,
  updateMemberRolesRequestSchema,
} from "~/lib/identity-api";
import type {
  InviteMemberRequest,
  InviteMemberResponse,
  Member,
  MemberCapabilities,
  MemberCapabilitiesDocument,
  MemberCapability,
  Operations,
  Organization,
  PutMemberCapabilitiesRequest,
  UpdateMemberRolesRequest,
} from "~/lib/identity-api";
import {
  createCheckoutSession as createCheckoutSessionRequest,
  createPortalSession as createPortalSessionRequest,
  createSubscriptionSession as createSubscriptionSessionRequest,
  executionIdInputSchema,
  getBalance as getBalanceRequest,
  getExecution as getExecutionRequest,
  getGrants as getGrantsRequest,
  getStatement as getStatementRequest,
  getRepo as getRepoRequest,
  getRepos as getReposRequest,
  getSubscriptions as getSubscriptionsRequest,
  grantsQuerySchema,
  statementQuerySchema,
  importRepo as importRepoRequest,
  importRepoRequestSchema,
  isSandboxRentalApiError,
  isSandboxRentalNotFound,
  repoIdInputSchema,
  rescanRepo as rescanRepoRequest,
  SandboxRentalApiError,
  submitDirectExecution as submitDirectExecutionRequest,
  executionRequestSchema,
  portalRequestSchema,
  subscribeRequestSchema,
  checkoutRequestSchema,
} from "~/lib/sandbox-rental-api";
import type {
  Balance,
  CheckoutRequest,
  Execution,
  GrantsResponse,
  Statement,
  StatementQuery,
  ImportRepoRequest,
  PortalRequest,
  Repo,
  RepoCompatibilitySummary,
  ExecutionRequest,
  SubscribeRequest,
  SubscriptionsResponse,
} from "~/lib/sandbox-rental-api";
import type { AuthSession } from "@forge-metal/auth-web/server";
import { getAccessTokenForAudience } from "@forge-metal/auth-web/server";
import { rentASandboxAuthMiddleware } from "./auth";

const IDENTITY_SERVICE_BASE_URL = requireURLFromEnv("IDENTITY_SERVICE_BASE_URL");
const SANDBOX_RENTAL_SERVICE_BASE_URL = requireURLFromEnv("SANDBOX_RENTAL_SERVICE_BASE_URL");
const IDENTITY_SERVICE_AUTH_PROJECT_ID = process.env.IDENTITY_SERVICE_AUTH_PROJECT_ID?.trim();
const verificationRunHeader = "X-Forge-Metal-Verification-Run";

export { IdentityApiError, isIdentityApiError };
export { SandboxRentalApiError, isSandboxRentalApiError, isSandboxRentalNotFound };
export type {
  Balance,
  CheckoutRequest,
  Execution,
  ExecutionRequest,
  GrantsResponse,
  Statement,
  StatementQuery,
  ImportRepoRequest,
  PortalRequest,
  Repo,
  RepoCompatibilitySummary,
  SubscribeRequest,
  SubscriptionsResponse,
};
export type {
  InviteMemberRequest,
  InviteMemberResponse,
  Member,
  MemberCapabilities,
  MemberCapabilitiesDocument,
  MemberCapability,
  Operations,
  Organization,
  PutMemberCapabilitiesRequest,
  UpdateMemberRolesRequest,
};

async function getServerVerificationRunID(): Promise<string | undefined> {
  // This module is imported by client query code, so keep Start's server helpers
  // behind a dynamic import or Vite will pull server-only modules into the browser graph.
  const { getRequestHeader } = await import("@tanstack/react-start/server");
  return getRequestHeader(verificationRunHeader)?.trim() || undefined;
}

async function resolveAuthContext(
  context: { auth?: AuthSession } | undefined,
): Promise<AuthSession> {
  if (context?.auth) {
    return context.auth;
  }
  // Start server functions invoked during SSR can miss middleware context; re-read the server-owned session before crossing the service boundary.
  const [{ getAuthSession }, { getAuthConfig }] = await Promise.all([
    import("@forge-metal/auth-web/server"),
    import("../server/auth"),
  ]);
  const auth = await getAuthSession(getAuthConfig());
  if (!auth) {
    throw new Error("Authentication required");
  }
  return auth;
}

async function sandboxRentalClientOptions(context: { auth?: AuthSession } | undefined) {
  const auth = await resolveAuthContext(context);
  const options = {
    accessToken: auth.accessToken,
    baseUrl: SANDBOX_RENTAL_SERVICE_BASE_URL,
  };
  const verificationRunID = await getServerVerificationRunID();
  return verificationRunID ? { ...options, verificationRunId: verificationRunID } : options;
}

async function identityClientOptions(context: { auth?: AuthSession } | undefined) {
  const auth = await resolveAuthContext(context);
  const { getAuthConfig } = await import("../server/auth");
  const accessToken = IDENTITY_SERVICE_AUTH_PROJECT_ID
    ? await getAccessTokenForAudience(getAuthConfig(), auth, IDENTITY_SERVICE_AUTH_PROJECT_ID)
    : auth.accessToken;
  return {
    accessToken,
    baseUrl: IDENTITY_SERVICE_BASE_URL,
  };
}

export const getOrganization = createServerFn({ method: "GET" })
  .middleware([rentASandboxAuthMiddleware])
  .handler(async ({ context }) => {
    return getOrganizationRequest(await identityClientOptions(context));
  });

export const getMembers = createServerFn({ method: "GET" })
  .middleware([rentASandboxAuthMiddleware])
  .handler(async ({ context }) => {
    return getMembersRequest(await identityClientOptions(context));
  });

export const inviteMember = createServerFn({ method: "POST" })
  .middleware([rentASandboxAuthMiddleware])
  .inputValidator(inviteMemberRequestSchema)
  .handler(async ({ context, data }) => {
    return inviteMemberRequest({
      ...(await identityClientOptions(context)),
      body: data,
    });
  });

export const updateMemberRoles = createServerFn({ method: "POST" })
  .middleware([rentASandboxAuthMiddleware])
  .inputValidator(updateMemberRolesRequestSchema)
  .handler(async ({ context, data }) => {
    return updateMemberRolesRequest({
      ...(await identityClientOptions(context)),
      body: data,
    });
  });

export const getOperations = createServerFn({ method: "GET" })
  .middleware([rentASandboxAuthMiddleware])
  .handler(async ({ context }) => {
    return getOperationsRequest(await identityClientOptions(context));
  });

export const getMemberCapabilities = createServerFn({ method: "GET" })
  .middleware([rentASandboxAuthMiddleware])
  .handler(async ({ context }) => {
    return getMemberCapabilitiesRequest(await identityClientOptions(context));
  });

export const putMemberCapabilities = createServerFn({ method: "POST" })
  .middleware([rentASandboxAuthMiddleware])
  .inputValidator(putMemberCapabilitiesRequestSchema)
  .handler(async ({ context, data }) => {
    return putMemberCapabilitiesRequest({
      ...(await identityClientOptions(context)),
      body: data,
    });
  });

export const getBalance = createServerFn({ method: "GET" })
  .middleware([rentASandboxAuthMiddleware])
  .handler(async ({ context }) => {
    return getBalanceRequest(await sandboxRentalClientOptions(context));
  });

export const getSubscriptions = createServerFn({ method: "GET" })
  .middleware([rentASandboxAuthMiddleware])
  .handler(async ({ context }) => {
    return getSubscriptionsRequest(await sandboxRentalClientOptions(context));
  });

export const getGrants = createServerFn({ method: "GET" })
  .middleware([rentASandboxAuthMiddleware])
  .inputValidator(grantsQuerySchema)
  .handler(async ({ context, data }) => {
    return getGrantsRequest({
      ...(await sandboxRentalClientOptions(context)),
      query: data,
    });
  });

export const getStatement = createServerFn({ method: "GET" })
  .middleware([rentASandboxAuthMiddleware])
  .inputValidator(statementQuerySchema)
  .handler(async ({ context, data }) => {
    return getStatementRequest({
      ...(await sandboxRentalClientOptions(context)),
      query: data,
    });
  });

export const createCheckoutSession = createServerFn({ method: "POST" })
  .middleware([rentASandboxAuthMiddleware])
  .inputValidator(checkoutRequestSchema)
  .handler(async ({ context, data }) => {
    return createCheckoutSessionRequest({
      ...(await sandboxRentalClientOptions(context)),
      body: data,
    });
  });

export const createSubscriptionSession = createServerFn({ method: "POST" })
  .middleware([rentASandboxAuthMiddleware])
  .inputValidator(subscribeRequestSchema)
  .handler(async ({ context, data }) => {
    return createSubscriptionSessionRequest({
      ...(await sandboxRentalClientOptions(context)),
      body: data,
    });
  });

export const createPortalSession = createServerFn({ method: "POST" })
  .middleware([rentASandboxAuthMiddleware])
  .inputValidator(portalRequestSchema)
  .handler(async ({ context, data }) => {
    return createPortalSessionRequest({
      ...(await sandboxRentalClientOptions(context)),
      body: data,
    });
  });

export const submitDirectExecution = createServerFn({ method: "POST" })
  .middleware([rentASandboxAuthMiddleware])
  .inputValidator(executionRequestSchema)
  .handler(async ({ context, data }) => {
    return submitDirectExecutionRequest({
      ...(await sandboxRentalClientOptions(context)),
      body: data,
    });
  });

export const getExecution = createServerFn({ method: "GET" })
  .middleware([rentASandboxAuthMiddleware])
  .inputValidator(executionIdInputSchema)
  .handler(async ({ context, data }) => {
    return getExecutionRequest({
      ...(await sandboxRentalClientOptions(context)),
      executionId: data.executionId,
    });
  });

export const importRepo = createServerFn({ method: "POST" })
  .middleware([rentASandboxAuthMiddleware])
  .inputValidator(importRepoRequestSchema)
  .handler(async ({ context, data }) => {
    return importRepoRequest({
      ...(await sandboxRentalClientOptions(context)),
      body: data,
    });
  });

export const getRepos = createServerFn({ method: "GET" })
  .middleware([rentASandboxAuthMiddleware])
  .handler(async ({ context }) => {
    return getReposRequest(await sandboxRentalClientOptions(context));
  });

export const getRepo = createServerFn({ method: "GET" })
  .middleware([rentASandboxAuthMiddleware])
  .inputValidator(repoIdInputSchema)
  .handler(async ({ context, data }) => {
    return getRepoRequest({
      ...(await sandboxRentalClientOptions(context)),
      repoId: data.repoId,
    });
  });

export const rescanRepo = createServerFn({ method: "POST" })
  .middleware([rentASandboxAuthMiddleware])
  .inputValidator(repoIdInputSchema)
  .handler(async ({ context, data }) => {
    return rescanRepoRequest({
      ...(await sandboxRentalClientOptions(context)),
      repoId: data.repoId,
    });
  });
