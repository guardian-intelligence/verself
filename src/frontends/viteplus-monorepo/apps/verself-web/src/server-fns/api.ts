import { createServerFn } from "@tanstack/react-start";
import * as v from "valibot";
import { readFileSync } from "node:fs";
import { requireURLFromEnv } from "@verself/web-env";
import {
  IAMApiError,
  getMembers as getMembersRequest,
  getMemberCapabilities as getMemberCapabilitiesRequest,
  getOrganization as getOrganizationRequest,
  inviteMember as inviteMemberRequest,
  inviteMemberRequestSchema,
  isIAMApiError,
  listMyOrganizations as listMyOrganizationsRequest,
  putMemberCapabilities as putMemberCapabilitiesRequest,
  putMemberCapabilitiesRequestSchema,
  updateOrganization as updateOrganizationRequest,
  updateOrganizationRequestSchema,
  updateMemberRoles as updateMemberRolesRequest,
  updateMemberRolesRequestSchema,
} from "~/lib/iam-api";
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
  ProjectsApiError,
  createProjectRequestSchema,
  isProjectsApiError,
  type CreateProjectRequest,
  type Project,
  type ProjectList,
  Verself,
} from "@verself/sdk";
import {
  SourceCodeHostingApiError,
  createCheckoutGrant as createSourceCheckoutGrantRequest,
  createCheckoutGrantRequestSchema as createSourceCheckoutGrantRequestSchema,
  createGitCredential as createSourceGitCredentialRequest,
  createGitCredentialRequestSchema as createSourceGitCredentialRequestSchema,
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
  OrganizationMetadata,
  PutMemberCapabilitiesRequest,
  UpdateOrganizationRequest,
  UpdateMemberRolesRequest,
} from "~/lib/iam-api";
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
  CreateGitCredentialRequest as CreateSourceGitCredentialRequest,
  CreateRepositoryRequest as CreateSourceRepositoryRequest,
  SourceBlob,
  SourceCheckoutGrant,
  SourceGitCredential,
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
  listRuns as listRunsRequest,
  listExecutionSchedules as listExecutionSchedulesRequest,
  pauseSchedule as pauseScheduleRequest,
  resumeSchedule as resumeScheduleRequest,
  runListQuerySchema,
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
  RunListQuery,
  RunListQueryInput,
  RunsPage,
} from "~/lib/sandbox-rental-api";
import { consoleAuthMiddleware, getAccessTokenForAudience, type ConsoleAuthContext } from "./auth";

const IAM_SERVICE_BASE_URL = requireURLFromEnv("IAM_SERVICE_BASE_URL");
const GOVERNANCE_SERVICE_BASE_URL = requireURLFromEnv("GOVERNANCE_SERVICE_BASE_URL");
const PROFILE_SERVICE_BASE_URL = requireURLFromEnv("PROFILE_SERVICE_BASE_URL");
const NOTIFICATIONS_SERVICE_BASE_URL = requireURLFromEnv("NOTIFICATIONS_SERVICE_BASE_URL");
const PROJECTS_SERVICE_BASE_URL = requireURLFromEnv("PROJECTS_SERVICE_BASE_URL");
const SOURCE_CODE_HOSTING_SERVICE_BASE_URL = requireURLFromEnv(
  "SOURCE_CODE_HOSTING_SERVICE_BASE_URL",
);
const SANDBOX_RENTAL_SERVICE_BASE_URL = requireURLFromEnv("SANDBOX_RENTAL_SERVICE_BASE_URL");
const IAM_SERVICE_AUTH_AUDIENCE = requireCredentialEnv("IAM_SERVICE_AUTH_AUDIENCE");
const SANDBOX_RENTAL_SERVICE_AUTH_AUDIENCE = requireCredentialEnv(
  "SANDBOX_RENTAL_SERVICE_AUTH_AUDIENCE",
);
const PROFILE_SERVICE_AUTH_AUDIENCE = requireCredentialEnv("PROFILE_SERVICE_AUTH_AUDIENCE");
const NOTIFICATIONS_SERVICE_AUTH_AUDIENCE = requireCredentialEnv(
  "NOTIFICATIONS_SERVICE_AUTH_AUDIENCE",
);
const PROJECTS_SERVICE_AUTH_AUDIENCE = requireCredentialEnv("PROJECTS_SERVICE_AUTH_AUDIENCE");
const SOURCE_CODE_HOSTING_SERVICE_AUTH_AUDIENCE = requireCredentialEnv(
  "SOURCE_CODE_HOSTING_SERVICE_AUTH_AUDIENCE",
);

function requireCredentialEnv(name: string): string {
  const inlineValue = process.env[name]?.trim();
  if (inlineValue) {
    return inlineValue;
  }
  const pathEnvName = `VERSELF_CRED_${name}`;
  const path = process.env[pathEnvName]?.trim();
  if (!path) {
    throw new Error(`${name} or ${pathEnvName} is required`);
  }
  const value = readFileSync(path, "utf8").trim();
  if (!value) {
    throw new Error(`${pathEnvName} points to an empty credential`);
  }
  return value;
}

export { IAMApiError, isIAMApiError };
export { GovernanceApiError, isGovernanceApiError };
export { ProfileApiError, isProfileApiError };
export { NotificationsApiError, isNotificationsApiError };
export { ProjectsApiError, isProjectsApiError };
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
export type { CreateProjectRequest, Project, ProjectList };
export type {
  CreateSourceCheckoutGrantRequest,
  CreateSourceGitCredentialRequest,
  CreateSourceRepositoryRequest,
  SourceBlob,
  SourceCheckoutGrant,
  SourceGitCredential,
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
  RunListQuery,
  RunListQueryInput,
  RunsPage,
};
export type {
  InviteMemberRequest,
  InviteMemberResponse,
  Member,
  MemberCapabilities,
  MemberCapabilitiesDocument,
  MemberCapability,
  Organization,
  OrganizationMetadata,
  PutMemberCapabilitiesRequest,
  UpdateOrganizationRequest,
  UpdateMemberRolesRequest,
};

async function sandboxRentalClientOptions(context: ConsoleAuthContext | undefined) {
  return {
    accessToken: await getAccessTokenForAudience(context, SANDBOX_RENTAL_SERVICE_AUTH_AUDIENCE),
    baseUrl: SANDBOX_RENTAL_SERVICE_BASE_URL,
  };
}

async function iamClientOptions(
  context: ConsoleAuthContext | undefined,
  options: { roleAssignmentScope?: "selected_org" | "all_granted_orgs" } = {},
) {
  const accessToken = await getAccessTokenForAudience(context, IAM_SERVICE_AUTH_AUDIENCE, options);
  return {
    accessToken,
    baseUrl: IAM_SERVICE_BASE_URL,
  };
}

async function governanceClientOptions(context: ConsoleAuthContext | undefined) {
  const iamOptions = await iamClientOptions(context);
  return {
    ...iamOptions,
    baseUrl: GOVERNANCE_SERVICE_BASE_URL,
  };
}

async function profileClientOptions(context: ConsoleAuthContext | undefined) {
  const accessToken = await getAccessTokenForAudience(context, PROFILE_SERVICE_AUTH_AUDIENCE);
  return {
    accessToken,
    baseUrl: PROFILE_SERVICE_BASE_URL,
  };
}

async function notificationsClientOptions(context: ConsoleAuthContext | undefined) {
  const accessToken = await getAccessTokenForAudience(context, NOTIFICATIONS_SERVICE_AUTH_AUDIENCE);
  return {
    accessToken,
    baseUrl: NOTIFICATIONS_SERVICE_BASE_URL,
  };
}

async function projectsSDK(context: ConsoleAuthContext | undefined) {
  const accessToken = await getAccessTokenForAudience(context, PROJECTS_SERVICE_AUTH_AUDIENCE);
  return new Verself({ bearerToken: accessToken, projectsURL: PROJECTS_SERVICE_BASE_URL });
}

async function sourceCodeHostingClientOptions(context: ConsoleAuthContext | undefined) {
  const accessToken = await getAccessTokenForAudience(
    context,
    SOURCE_CODE_HOSTING_SERVICE_AUTH_AUDIENCE,
  );
  return {
    accessToken,
    baseUrl: SOURCE_CODE_HOSTING_SERVICE_BASE_URL,
  };
}

export const getOrganization = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return getOrganizationRequest(await iamClientOptions(context));
  });

export const listMyOrganizations = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return listMyOrganizationsRequest(
      await iamClientOptions(context, { roleAssignmentScope: "all_granted_orgs" }),
    );
  });

export const updateOrganization = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(updateOrganizationRequestSchema)
  .handler(async ({ context, data }) => {
    return updateOrganizationRequest({
      ...(await iamClientOptions(context)),
      body: data,
    });
  });

export const getMembers = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return getMembersRequest(await iamClientOptions(context));
  });

export const inviteMember = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(inviteMemberRequestSchema)
  .handler(async ({ context, data }) => {
    return inviteMemberRequest({
      ...(await iamClientOptions(context)),
      body: data,
    });
  });

export const updateMemberRoles = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(updateMemberRolesRequestSchema)
  .handler(async ({ context, data }) => {
    return updateMemberRolesRequest({
      ...(await iamClientOptions(context)),
      body: data,
    });
  });

export const getMemberCapabilities = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return getMemberCapabilitiesRequest(await iamClientOptions(context));
  });

export const putMemberCapabilities = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(putMemberCapabilitiesRequestSchema)
  .handler(async ({ context, data }) => {
    return putMemberCapabilitiesRequest({
      ...(await iamClientOptions(context)),
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

export const listProjects = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return (await projectsSDK(context)).projects.list();
  });

export const createProject = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(createProjectRequestSchema)
  .handler(async ({ context, data }) => {
    return (await projectsSDK(context)).projects.create(data);
  });

const projectIDInputSchema = v.strictObject({
  projectId: v.pipe(v.string(), v.uuid()),
});

export const getProject = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(projectIDInputSchema)
  .handler(async ({ context, data }) => {
    return (await projectsSDK(context)).projects.get(data.projectId);
  });

const sourceRepositoryIDInputSchema = v.strictObject({
  repoId: v.pipe(v.string(), v.uuid()),
});

const sourceRepositoryListInputSchema = v.optional(
  v.strictObject({
    projectId: v.optional(v.pipe(v.string(), v.uuid())),
  }),
);

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

const sourceGitCredentialInputSchema = createSourceGitCredentialRequestSchema;

export const listSourceRepositories = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(sourceRepositoryListInputSchema)
  .handler(async ({ context, data }) => {
    return listSourceRepositoriesRequest({
      ...(await sourceCodeHostingClientOptions(context)),
      ...(data?.projectId ? { projectId: data.projectId } : {}),
    });
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

export const createSourceGitCredential = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(sourceGitCredentialInputSchema)
  .handler(async ({ context, data }) => {
    return createSourceGitCredentialRequest({
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
      `verself-export-${data.export_id}.tar.gz`;
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

export const listRuns = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(v.optional(runListQuerySchema))
  .handler(async ({ context, data }) => {
    return listRunsRequest({
      ...(await sandboxRentalClientOptions(context)),
      ...(data === undefined ? {} : { query: data }),
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
