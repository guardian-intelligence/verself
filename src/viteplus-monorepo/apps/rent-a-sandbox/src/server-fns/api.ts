import { createServerFn } from "@tanstack/react-start";
import { requireURLFromEnv } from "@forge-metal/web-env";
import {
  createCheckoutSession as createCheckoutSessionRequest,
  createSubscriptionSession as createSubscriptionSessionRequest,
  executionIdInputSchema,
  getBalance as getBalanceRequest,
  getExecution as getExecutionRequest,
  getGrants as getGrantsRequest,
  getRepo as getRepoRequest,
  getRepos as getReposRequest,
  getSubscriptions as getSubscriptionsRequest,
  grantsQuerySchema,
  importRepo as importRepoRequest,
  importRepoRequestSchema,
  isSandboxRentalApiError,
  isSandboxRentalNotFound,
  repoIdInputSchema,
  rescanRepo as rescanRepoRequest,
  SandboxRentalApiError,
  submitDirectExecution as submitDirectExecutionRequest,
  executionRequestSchema,
  subscribeRequestSchema,
  checkoutRequestSchema,
} from "~/lib/sandbox-rental-api";
import type {
  Balance,
  CheckoutRequest,
  Execution,
  GrantsResponse,
  ImportRepoRequest,
  Repo,
  RepoCompatibilitySummary,
  ExecutionRequest,
  SubscribeRequest,
  SubscriptionsResponse,
} from "~/lib/sandbox-rental-api";
import type { AuthSession } from "@forge-metal/auth-web/server";
import { rentASandboxAuthMiddleware } from "./auth";

const SANDBOX_RENTAL_SERVICE_BASE_URL = requireURLFromEnv("SANDBOX_RENTAL_SERVICE_BASE_URL");
const verificationRunHeader = "X-Forge-Metal-Verification-Run";

export { SandboxRentalApiError, isSandboxRentalApiError, isSandboxRentalNotFound };
export type {
  Balance,
  CheckoutRequest,
  Execution,
  ExecutionRequest,
  GrantsResponse,
  ImportRepoRequest,
  Repo,
  RepoCompatibilitySummary,
  SubscribeRequest,
  SubscriptionsResponse,
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
