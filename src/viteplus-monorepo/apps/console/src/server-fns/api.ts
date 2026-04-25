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
  auditEventsQuerySchema,
  createDataExport as createDataExportRequest,
  createExportRequestSchema,
  isGovernanceApiError,
  listAuditEvents as listAuditEventsRequest,
  listDataExports as listDataExportsRequest,
} from "~/lib/governance-api";
import {
  ProfileApiError,
  getProfile as getProfileRequest,
  isProfileApiError,
  putProfilePreferences as putProfilePreferencesRequest,
  putProfilePreferencesRequestSchema,
  updateProfileIdentity as updateProfileIdentityRequest,
  updateProfileIdentityRequestSchema,
} from "~/lib/profile-api";
import {
  NotificationsApiError,
  clearNotifications as clearNotificationsRequest,
  dismissNotification as dismissNotificationRequest,
  dismissNotificationRequestSchema,
  getNotificationSummary as getNotificationSummaryRequest,
  isNotificationsApiError,
  listNotifications as listNotificationsRequest,
  markNotificationRead as markNotificationReadRequest,
  markNotificationReadByID as markNotificationReadByIDRequest,
  markNotificationReadRequestSchema,
  notificationsListQuerySchema,
  publishTestNotification as publishTestNotificationRequest,
  publishTestNotificationRequestSchema,
  putNotificationPreferences as putNotificationPreferencesRequest,
  putNotificationPreferencesRequestSchema,
} from "~/lib/notifications-api";
import {
  SourceCodeHostingApiError,
  createCheckoutGrant as createSourceCheckoutGrantRequest,
  createCheckoutGrantRequestSchema as createSourceCheckoutGrantRequestSchema,
  createIntegration as createSourceIntegrationRequest,
  createIntegrationRequestSchema as createSourceIntegrationRequestSchema,
  createRepository as createSourceRepositoryRequest,
  createRepositoryRequestSchema as createSourceRepositoryRequestSchema,
  getBlob as getSourceBlobRequest,
  getRepository as getSourceRepositoryRequest,
  getTree as getSourceTreeRequest,
  isSourceCodeHostingApiError,
  listRefs as listSourceRefsRequest,
  listRepositories as listSourceRepositoriesRequest,
  listWorkflowRuns as listSourceWorkflowRunsRequest,
} from "~/lib/source-code-hosting-api";
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
import type {
  ProfileSnapshot,
  PutProfilePreferencesRequest,
  UpdateProfileIdentityRequest,
} from "~/lib/profile-api";
import type {
  DismissNotificationRequest,
  MarkNotificationReadRequest,
  Notification,
  NotificationAccepted,
  NotificationList,
  NotificationSummary,
  NotificationsListQuery,
  PublishTestNotificationRequest,
  PutNotificationPreferencesRequest,
} from "~/lib/notifications-api";
import type {
  CreateCheckoutGrantRequest as CreateSourceCheckoutGrantRequest,
  CreateIntegrationRequest as CreateSourceIntegrationRequest,
  CreateRepositoryRequest as CreateSourceRepositoryRequest,
  SourceBlob,
  SourceCheckoutGrant,
  SourceIntegration,
  SourceRefs,
  SourceRepository,
  SourceRepositoryList,
  SourceTree,
  SourceWorkflowRunList,
} from "~/lib/source-code-hosting-api";
import {
  cancelContract as cancelContractRequest,
  cancelContractRequestSchema,
  createExecutionSchedule as createExecutionScheduleRequest,
  createCheckoutSession as createCheckoutSessionRequest,
  createContractChangeSession as createContractChangeSessionRequest,
  createContractSession as createContractSessionRequest,
  createPortalSession as createPortalSessionRequest,
  executionIdInputSchema,
  executionScheduleIdInputSchema,
  executionScheduleRequestSchema,
  getEntitlements as getEntitlementsRequest,
  getExecution as getExecutionRequest,
  getExecutionSchedule as getExecutionScheduleRequest,
  getPlans as getPlansRequest,
  getStatement as getStatementRequest,
  getContracts as getContractsRequest,
  listExecutionSchedules as listExecutionSchedulesRequest,
  pauseSchedule as pauseScheduleRequest,
  resumeSchedule as resumeScheduleRequest,
  statementQuerySchema,
  isSandboxRentalApiError,
  isSandboxRentalNotFound,
  SandboxRentalApiError,
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
  ExecutionSchedule,
  ExecutionScheduleIdInput,
  ExecutionScheduleRequest,
  ExecutionSchedules,
  PlansResponse,
  Statement,
  StatementQuery,
  PortalRequest,
  ContractRequest,
  ContractsResponse,
} from "~/lib/sandbox-rental-api";
import type { AuthSession } from "@forge-metal/auth-web/server";
import { getAccessTokenForAudience, getAuthSession } from "@forge-metal/auth-web/server";
import { getAuthConfig } from "../server/auth";
import { consoleAuthMiddleware } from "./auth";

const IDENTITY_SERVICE_BASE_URL = requireURLFromEnv("IDENTITY_SERVICE_BASE_URL");
const GOVERNANCE_SERVICE_BASE_URL = requireURLFromEnv("GOVERNANCE_SERVICE_BASE_URL");
const PROFILE_SERVICE_BASE_URL = requireURLFromEnv("PROFILE_SERVICE_BASE_URL");
const NOTIFICATIONS_SERVICE_BASE_URL = requireURLFromEnv("NOTIFICATIONS_SERVICE_BASE_URL");
const SOURCE_CODE_HOSTING_SERVICE_BASE_URL = requireURLFromEnv(
  "SOURCE_CODE_HOSTING_SERVICE_BASE_URL",
);
const SANDBOX_RENTAL_SERVICE_BASE_URL = requireURLFromEnv("SANDBOX_RENTAL_SERVICE_BASE_URL");
const IDENTITY_SERVICE_AUTH_AUDIENCE = process.env.IDENTITY_SERVICE_AUTH_AUDIENCE?.trim();
const PROFILE_SERVICE_AUTH_AUDIENCE =
  process.env.PROFILE_SERVICE_AUTH_AUDIENCE?.trim() || IDENTITY_SERVICE_AUTH_AUDIENCE;
const NOTIFICATIONS_SERVICE_AUTH_AUDIENCE =
  process.env.NOTIFICATIONS_SERVICE_AUTH_AUDIENCE?.trim() || IDENTITY_SERVICE_AUTH_AUDIENCE;
const SOURCE_CODE_HOSTING_SERVICE_AUTH_AUDIENCE =
  process.env.SOURCE_CODE_HOSTING_SERVICE_AUTH_AUDIENCE?.trim() || IDENTITY_SERVICE_AUTH_AUDIENCE;

export { IdentityApiError, isIdentityApiError };
export { GovernanceApiError, isGovernanceApiError };
export { ProfileApiError, isProfileApiError };
export { NotificationsApiError, isNotificationsApiError };
export { SourceCodeHostingApiError, isSourceCodeHostingApiError };
export { SandboxRentalApiError, isSandboxRentalApiError, isSandboxRentalNotFound };
export type {
  CreateExportRequest,
  GovernanceAuditEvent,
  GovernanceAuditEvents,
  GovernanceExportJob,
};
export type { ProfileSnapshot, PutProfilePreferencesRequest, UpdateProfileIdentityRequest };
export type {
  DismissNotificationRequest,
  MarkNotificationReadRequest,
  Notification,
  NotificationAccepted,
  NotificationList,
  NotificationSummary,
  NotificationsListQuery,
  PublishTestNotificationRequest,
  PutNotificationPreferencesRequest,
};
export type {
  CreateSourceCheckoutGrantRequest,
  CreateSourceIntegrationRequest,
  CreateSourceRepositoryRequest,
  SourceBlob,
  SourceCheckoutGrant,
  SourceIntegration,
  SourceRefs,
  SourceRepository,
  SourceRepositoryList,
  SourceTree,
  SourceWorkflowRunList,
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
  ExecutionSchedule,
  ExecutionScheduleIdInput,
  ExecutionScheduleRequest,
  ExecutionSchedules,
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
  const accessToken = IDENTITY_SERVICE_AUTH_AUDIENCE
    ? await getAccessTokenForAudience(getAuthConfig(), auth, IDENTITY_SERVICE_AUTH_AUDIENCE)
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

async function profileClientOptions(context: { auth?: AuthSession } | undefined) {
  const auth = await resolveAuthContext(context);
  const accessToken =
    PROFILE_SERVICE_AUTH_AUDIENCE &&
    PROFILE_SERVICE_AUTH_AUDIENCE !== IDENTITY_SERVICE_AUTH_AUDIENCE
      ? await getAccessTokenForAudience(getAuthConfig(), auth, PROFILE_SERVICE_AUTH_AUDIENCE)
      : auth.accessToken;
  return {
    accessToken,
    baseUrl: PROFILE_SERVICE_BASE_URL,
  };
}

async function notificationsClientOptions(context: { auth?: AuthSession } | undefined) {
  const auth = await resolveAuthContext(context);
  const accessToken =
    NOTIFICATIONS_SERVICE_AUTH_AUDIENCE &&
    NOTIFICATIONS_SERVICE_AUTH_AUDIENCE !== IDENTITY_SERVICE_AUTH_AUDIENCE
      ? await getAccessTokenForAudience(getAuthConfig(), auth, NOTIFICATIONS_SERVICE_AUTH_AUDIENCE)
      : auth.accessToken;
  return {
    accessToken,
    baseUrl: NOTIFICATIONS_SERVICE_BASE_URL,
  };
}

async function sourceCodeHostingClientOptions(context: { auth?: AuthSession } | undefined) {
  const auth = await resolveAuthContext(context);
  const accessToken =
    SOURCE_CODE_HOSTING_SERVICE_AUTH_AUDIENCE &&
    SOURCE_CODE_HOSTING_SERVICE_AUTH_AUDIENCE !== IDENTITY_SERVICE_AUTH_AUDIENCE
      ? await getAccessTokenForAudience(
          getAuthConfig(),
          auth,
          SOURCE_CODE_HOSTING_SERVICE_AUTH_AUDIENCE,
        )
      : auth.accessToken;
  return {
    accessToken,
    baseUrl: SOURCE_CODE_HOSTING_SERVICE_BASE_URL,
  };
}

export const getOrganization = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return getOrganizationRequest(await identityClientOptions(context));
  });

export const getMembers = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return getMembersRequest(await identityClientOptions(context));
  });

export const inviteMember = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(inviteMemberRequestSchema)
  .handler(async ({ context, data }) => {
    return inviteMemberRequest({
      ...(await identityClientOptions(context)),
      body: data,
    });
  });

export const updateMemberRoles = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(updateMemberRolesRequestSchema)
  .handler(async ({ context, data }) => {
    return updateMemberRolesRequest({
      ...(await identityClientOptions(context)),
      body: data,
    });
  });

export const getMemberCapabilities = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return getMemberCapabilitiesRequest(await identityClientOptions(context));
  });

export const putMemberCapabilities = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(putMemberCapabilitiesRequestSchema)
  .handler(async ({ context, data }) => {
    return putMemberCapabilitiesRequest({
      ...(await identityClientOptions(context)),
      body: data,
    });
  });

export const getProfile = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return getProfileRequest(await profileClientOptions(context));
  });

export const updateProfileIdentity = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(updateProfileIdentityRequestSchema)
  .handler(async ({ context, data }) => {
    return updateProfileIdentityRequest({
      ...(await profileClientOptions(context)),
      body: data,
    });
  });

export const putProfilePreferences = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(putProfilePreferencesRequestSchema)
  .handler(async ({ context, data }) => {
    return putProfilePreferencesRequest({
      ...(await profileClientOptions(context)),
      body: data,
    });
  });

export const listNotifications = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(notificationsListQuerySchema)
  .handler(async ({ context, data }) => {
    return listNotificationsRequest({
      ...(await notificationsClientOptions(context)),
      query: data,
    });
  });

export const getNotificationSummary = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return getNotificationSummaryRequest(await notificationsClientOptions(context));
  });

export const putNotificationPreferences = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(putNotificationPreferencesRequestSchema)
  .handler(async ({ context, data }) => {
    return putNotificationPreferencesRequest({
      ...(await notificationsClientOptions(context)),
      body: data,
    });
  });

export const markNotificationRead = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(markNotificationReadRequestSchema)
  .handler(async ({ context, data }) => {
    return markNotificationReadRequest({
      ...(await notificationsClientOptions(context)),
      body: data,
    });
  });

export const markNotificationReadByID = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(dismissNotificationRequestSchema)
  .handler(async ({ context, data }) => {
    return markNotificationReadByIDRequest({
      ...(await notificationsClientOptions(context)),
      body: data,
    });
  });

export const dismissNotification = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(dismissNotificationRequestSchema)
  .handler(async ({ context, data }) => {
    return dismissNotificationRequest({
      ...(await notificationsClientOptions(context)),
      body: data,
    });
  });

export const clearNotifications = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return clearNotificationsRequest(await notificationsClientOptions(context));
  });

export const publishTestNotification = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(publishTestNotificationRequestSchema)
  .handler(async ({ context, data }) => {
    return publishTestNotificationRequest({
      ...(await notificationsClientOptions(context)),
      body: data,
    });
  });

const sourceRepositoryIDInputSchema = v.strictObject({
  repoId: v.pipe(v.string(), v.uuid()),
});

const sourceTreeInputSchema = v.strictObject({
  repoId: v.pipe(v.string(), v.uuid()),
  ref: v.optional(v.string()),
  path: v.optional(v.string()),
});

const sourceBlobInputSchema = v.strictObject({
  repoId: v.pipe(v.string(), v.uuid()),
  ref: v.optional(v.string()),
  path: v.pipe(v.string(), v.minLength(1)),
});

const sourceCheckoutGrantInputSchema = v.strictObject({
  repoId: v.pipe(v.string(), v.uuid()),
  body: createSourceCheckoutGrantRequestSchema,
});

export const listSourceRepositories = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return listSourceRepositoriesRequest(await sourceCodeHostingClientOptions(context));
  });

export const createSourceRepository = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(createSourceRepositoryRequestSchema)
  .handler(async ({ context, data }) => {
    return createSourceRepositoryRequest({
      ...(await sourceCodeHostingClientOptions(context)),
      body: data,
    });
  });

export const getSourceRepository = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(sourceRepositoryIDInputSchema)
  .handler(async ({ context, data }) => {
    return getSourceRepositoryRequest({
      ...(await sourceCodeHostingClientOptions(context)),
      repoId: data.repoId,
    });
  });

export const listSourceRefs = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(sourceRepositoryIDInputSchema)
  .handler(async ({ context, data }) => {
    return listSourceRefsRequest({
      ...(await sourceCodeHostingClientOptions(context)),
      repoId: data.repoId,
    });
  });

export const listSourceWorkflowRuns = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(sourceRepositoryIDInputSchema)
  .handler(async ({ context, data }) => {
    return listSourceWorkflowRunsRequest({
      ...(await sourceCodeHostingClientOptions(context)),
      repoId: data.repoId,
    });
  });

export const getSourceTree = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(sourceTreeInputSchema)
  .handler(async ({ context, data }) => {
    const treeInput = {
      repoId: data.repoId,
      ...(data.ref !== undefined ? { ref: data.ref } : {}),
      ...(data.path !== undefined ? { path: data.path } : {}),
    };
    return getSourceTreeRequest({
      ...(await sourceCodeHostingClientOptions(context)),
      ...treeInput,
    });
  });

export const getSourceBlob = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(sourceBlobInputSchema)
  .handler(async ({ context, data }) => {
    const blobInput = {
      repoId: data.repoId,
      path: data.path,
      ...(data.ref !== undefined ? { ref: data.ref } : {}),
    };
    return getSourceBlobRequest({
      ...(await sourceCodeHostingClientOptions(context)),
      ...blobInput,
    });
  });

export const createSourceCheckoutGrant = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(sourceCheckoutGrantInputSchema)
  .handler(async ({ context, data }) => {
    return createSourceCheckoutGrantRequest({
      ...(await sourceCodeHostingClientOptions(context)),
      repoId: data.repoId,
      body: data.body,
    });
  });

export const createSourceIntegration = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(createSourceIntegrationRequestSchema)
  .handler(async ({ context, data }) => {
    return createSourceIntegrationRequest({
      ...(await sourceCodeHostingClientOptions(context)),
      body: data,
    });
  });

export const listGovernanceAuditEvents = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(auditEventsQuerySchema)
  .handler(async ({ context, data }) => {
    return listAuditEventsRequest({
      ...(await governanceClientOptions(context)),
      query: data,
    });
  });

export const listGovernanceDataExports = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return listDataExportsRequest(await governanceClientOptions(context));
  });

export const createGovernanceDataExport = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
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
  .middleware([consoleAuthMiddleware])
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
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return getEntitlementsRequest(await sandboxRentalClientOptions(context));
  });

export const getContracts = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return getContractsRequest(await sandboxRentalClientOptions(context));
  });

export const getPlans = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return getPlansRequest(await sandboxRentalClientOptions(context));
  });

export const getStatement = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(statementQuerySchema)
  .handler(async ({ context, data }) => {
    return getStatementRequest({
      ...(await sandboxRentalClientOptions(context)),
      query: data,
    });
  });

export const createCheckoutSession = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(checkoutRequestSchema)
  .handler(async ({ context, data }) => {
    return createCheckoutSessionRequest({
      ...(await sandboxRentalClientOptions(context)),
      body: data,
    });
  });

export const createContractSession = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(contractRequestSchema)
  .handler(async ({ context, data }) => {
    return createContractSessionRequest({
      ...(await sandboxRentalClientOptions(context)),
      body: data,
    });
  });

export const createContractChangeSession = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(contractChangeRequestSchema)
  .handler(async ({ context, data }) => {
    return createContractChangeSessionRequest({
      ...(await sandboxRentalClientOptions(context)),
      body: data,
    });
  });

export const createPortalSession = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(portalRequestSchema)
  .handler(async ({ context, data }) => {
    return createPortalSessionRequest({
      ...(await sandboxRentalClientOptions(context)),
      body: data,
    });
  });

export const cancelContract = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(cancelContractRequestSchema)
  .handler(async ({ context, data }) => {
    return cancelContractRequest({
      ...(await sandboxRentalClientOptions(context)),
      body: data,
    });
  });

export const getExecution = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(executionIdInputSchema)
  .handler(async ({ context, data }) => {
    return getExecutionRequest({
      ...(await sandboxRentalClientOptions(context)),
      executionId: data.executionId,
    });
  });

export const listExecutionSchedules = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return listExecutionSchedulesRequest(await sandboxRentalClientOptions(context));
  });

export const createExecutionSchedule = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(executionScheduleRequestSchema)
  .handler(async ({ context, data }) => {
    return createExecutionScheduleRequest({
      ...(await sandboxRentalClientOptions(context)),
      body: data,
    });
  });

export const getExecutionSchedule = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(executionScheduleIdInputSchema)
  .handler(async ({ context, data }) => {
    return getExecutionScheduleRequest({
      ...(await sandboxRentalClientOptions(context)),
      scheduleId: data.scheduleId,
    });
  });

export const pauseExecutionSchedule = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(executionScheduleIdInputSchema)
  .handler(async ({ context, data }) => {
    return pauseScheduleRequest({
      ...(await sandboxRentalClientOptions(context)),
      scheduleId: data.scheduleId,
    });
  });

export const resumeExecutionSchedule = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(executionScheduleIdInputSchema)
  .handler(async ({ context, data }) => {
    return resumeScheduleRequest({
      ...(await sandboxRentalClientOptions(context)),
      scheduleId: data.scheduleId,
    });
  });
