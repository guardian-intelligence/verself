import { createServerFn } from "@tanstack/react-start";
import * as v from "valibot";
import { requireURLFromEnv } from "@forge-metal/web-env";
import {
  IdentityApiError,
  getMembers as getMembersRequest,
  getMemberCapabilities as getMemberCapabilitiesRequest,
  getOrganization as getOrganizationRequest,
  inviteMember as inviteMemberRequest,
  inviteMemberRequestSchema,
  isIdentityApiError,
  putMemberCapabilities as putMemberCapabilitiesRequest,
  putMemberCapabilitiesRequestSchema,
  updateMemberRoles as updateMemberRolesRequest,
  updateMemberRolesRequestSchema,
} from "~/lib/identity-api";
import {
  GovernanceApiError,
  createDataExport as createDataExportRequest,
  createExportRequestSchema,
  isGovernanceApiError,
  listAuditEvents as listAuditEventsRequest,
  listDataExports as listDataExportsRequest,
} from "~/lib/governance-api";
import type {
  InviteMemberRequest,
  InviteMemberResponse,
  Member,
  MemberCapabilities,
  MemberCapabilitiesDocument,
  MemberCapability,
  Organization,
  PutMemberCapabilitiesRequest,
  UpdateMemberRolesRequest,
} from "~/lib/identity-api";
import type {
  CreateExportRequest,
  GovernanceAuditEvent,
  GovernanceAuditEvents,
  GovernanceExportJob,
} from "~/lib/governance-api";
import {
  cancelContract as cancelContractRequest,
  cancelContractRequestSchema,
  createCheckoutSession as createCheckoutSessionRequest,
  createContractChangeSession as createContractChangeSessionRequest,
  createContractSession as createContractSessionRequest,
  createPortalSession as createPortalSessionRequest,
  executionIdInputSchema,
  getEntitlements as getEntitlementsRequest,
  getExecution as getExecutionRequest,
  getPlans as getPlansRequest,
  getStatement as getStatementRequest,
  getContracts as getContractsRequest,
  statementQuerySchema,
  isSandboxRentalApiError,
  isSandboxRentalNotFound,
  SandboxRentalApiError,
  submitDirectExecution as submitDirectExecutionRequest,
  executionRequestSchema,
  portalRequestSchema,
  contractChangeRequestSchema,
  contractRequestSchema,
  checkoutRequestSchema,
} from "~/lib/sandbox-rental-api";
import type {
  CheckoutRequest,
  CancelContractRequest,
  ContractChangeRequest,
  EntitlementBucketSection,
  EntitlementProductSection,
  EntitlementSlot,
  EntitlementSourceTotal,
  EntitlementsView,
  Execution,
  PlansResponse,
  Statement,
  StatementQuery,
  PortalRequest,
  ExecutionRequest,
  ContractRequest,
  ContractsResponse,
} from "~/lib/sandbox-rental-api";
import type { AuthSession } from "@forge-metal/auth-web/server";
import { getAccessTokenForAudience, getAuthSession } from "@forge-metal/auth-web/server";
import { getAuthConfig } from "../server/auth";
import { rentASandboxAuthMiddleware } from "./auth";

const IDENTITY_SERVICE_BASE_URL = requireURLFromEnv("IDENTITY_SERVICE_BASE_URL");
const GOVERNANCE_SERVICE_BASE_URL = requireURLFromEnv("GOVERNANCE_SERVICE_BASE_URL");
const SANDBOX_RENTAL_SERVICE_BASE_URL = requireURLFromEnv("SANDBOX_RENTAL_SERVICE_BASE_URL");
const IDENTITY_SERVICE_AUTH_PROJECT_ID = process.env.IDENTITY_SERVICE_AUTH_PROJECT_ID?.trim();

export { IdentityApiError, isIdentityApiError };
export { GovernanceApiError, isGovernanceApiError };
export { SandboxRentalApiError, isSandboxRentalApiError, isSandboxRentalNotFound };
export type {
  CreateExportRequest,
  GovernanceAuditEvent,
  GovernanceAuditEvents,
  GovernanceExportJob,
};
export type {
  CheckoutRequest,
  CancelContractRequest,
  EntitlementBucketSection,
  EntitlementProductSection,
  EntitlementSlot,
  EntitlementSourceTotal,
  EntitlementsView,
  Execution,
  ExecutionRequest,
  Statement,
  StatementQuery,
  PortalRequest,
  PlansResponse,
  ContractRequest,
  ContractChangeRequest,
  ContractsResponse,
};
export type {
  InviteMemberRequest,
  InviteMemberResponse,
  Member,
  MemberCapabilities,
  MemberCapabilitiesDocument,
  MemberCapability,
  Organization,
  PutMemberCapabilitiesRequest,
  UpdateMemberRolesRequest,
};

async function resolveAuthContext(
  context: { auth?: AuthSession } | undefined,
): Promise<AuthSession> {
  if (context?.auth) {
    return context.auth;
  }
  // Start server functions invoked during SSR can miss middleware context; re-read the server-owned session before crossing the service boundary.
  const auth = await getAuthSession(getAuthConfig());
  if (!auth) {
    throw new Error("Authentication required");
  }
  return auth;
}

async function sandboxRentalClientOptions(context: { auth?: AuthSession } | undefined) {
  const auth = await resolveAuthContext(context);
  return {
    accessToken: auth.accessToken,
    baseUrl: SANDBOX_RENTAL_SERVICE_BASE_URL,
  };
}

async function identityClientOptions(context: { auth?: AuthSession } | undefined) {
  const auth = await resolveAuthContext(context);
  const accessToken = IDENTITY_SERVICE_AUTH_PROJECT_ID
    ? await getAccessTokenForAudience(getAuthConfig(), auth, IDENTITY_SERVICE_AUTH_PROJECT_ID)
    : auth.accessToken;
  return {
    accessToken,
    baseUrl: IDENTITY_SERVICE_BASE_URL,
  };
}

async function governanceClientOptions(context: { auth?: AuthSession } | undefined) {
  const identityOptions = await identityClientOptions(context);
  return {
    ...identityOptions,
    baseUrl: GOVERNANCE_SERVICE_BASE_URL,
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

export const listGovernanceAuditEvents = createServerFn({ method: "GET" })
  .middleware([rentASandboxAuthMiddleware])
  .handler(async ({ context }) => {
    return listAuditEventsRequest({
      ...(await governanceClientOptions(context)),
      query: { limit: 50 },
    });
  });

export const listGovernanceDataExports = createServerFn({ method: "GET" })
  .middleware([rentASandboxAuthMiddleware])
  .handler(async ({ context }) => {
    return listDataExportsRequest(await governanceClientOptions(context));
  });

export const createGovernanceDataExport = createServerFn({ method: "POST" })
  .middleware([rentASandboxAuthMiddleware])
  .inputValidator(createExportRequestSchema)
  .handler(async ({ context, data }) => {
    return createDataExportRequest({
      ...(await governanceClientOptions(context)),
      body: data,
    });
  });

const governanceDownloadRequestSchema = v.strictObject({
  export_id: v.pipe(v.string(), v.uuid()),
});

export const downloadGovernanceDataExport = createServerFn({ method: "POST" })
  .middleware([rentASandboxAuthMiddleware])
  .inputValidator(governanceDownloadRequestSchema)
  .handler(async ({ context, data }) => {
    const options = await governanceClientOptions(context);
    const response = await fetch(
      `${options.baseUrl}/api/v1/governance/exports/${data.export_id}/download`,
      {
        headers: {
          Accept: "application/gzip",
          Authorization: `Bearer ${options.accessToken}`,
        },
      },
    );
    if (!response.ok) {
      throw new GovernanceApiError(
        response.status,
        `/api/v1/governance/exports/${data.export_id}/download`,
        await response.text(),
      );
    }
    const contentDisposition = response.headers.get("content-disposition") ?? "";
    const fileName =
      /filename="([^"]+)"/.exec(contentDisposition)?.[1] ??
      `forge-metal-export-${data.export_id}.tar.gz`;
    const bytes = Buffer.from(await response.arrayBuffer());
    return {
      content_type: response.headers.get("content-type") ?? "application/gzip",
      data_base64: bytes.toString("base64"),
      file_name: fileName,
    };
  });

export const getEntitlements = createServerFn({ method: "GET" })
  .middleware([rentASandboxAuthMiddleware])
  .handler(async ({ context }) => {
    return getEntitlementsRequest(await sandboxRentalClientOptions(context));
  });

export const getContracts = createServerFn({ method: "GET" })
  .middleware([rentASandboxAuthMiddleware])
  .handler(async ({ context }) => {
    return getContractsRequest(await sandboxRentalClientOptions(context));
  });

export const getPlans = createServerFn({ method: "GET" })
  .middleware([rentASandboxAuthMiddleware])
  .handler(async ({ context }) => {
    return getPlansRequest(await sandboxRentalClientOptions(context));
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

export const createContractSession = createServerFn({ method: "POST" })
  .middleware([rentASandboxAuthMiddleware])
  .inputValidator(contractRequestSchema)
  .handler(async ({ context, data }) => {
    return createContractSessionRequest({
      ...(await sandboxRentalClientOptions(context)),
      body: data,
    });
  });

export const createContractChangeSession = createServerFn({ method: "POST" })
  .middleware([rentASandboxAuthMiddleware])
  .inputValidator(contractChangeRequestSchema)
  .handler(async ({ context, data }) => {
    return createContractChangeSessionRequest({
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

export const cancelContract = createServerFn({ method: "POST" })
  .middleware([rentASandboxAuthMiddleware])
  .inputValidator(cancelContractRequestSchema)
  .handler(async ({ context, data }) => {
    return cancelContractRequest({
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
