import { createServerFn } from "@tanstack/react-start";
import * as v from "valibot";
import { requireURLFromEnv } from "@verself/web-env";
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
  BillingApiError,
  GovernanceApiError,
  IAMApiError,
  NotificationsApiError,
  ProjectsApiError,
  SandboxRentalApiError,
  SourceApiError as SourceCodeHostingApiError,
  billingCheckoutRequestSchema as checkoutRequestSchema,
  billingContractChangeRequestSchema as contractChangeRequestSchema,
  billingContractRequestSchema as contractRequestSchema,
  billingPortalRequestSchema as portalRequestSchema,
  billingStatementQuerySchema as statementQuerySchema,
  cancelBillingContractRequestSchema as cancelContractRequestSchema,
  cacheGenerationIdInputSchema,
  cachePathDeleteRequestSchema,
  cacheIdInputSchema,
  createProjectRequestSchema,
  createCheckoutGrantRequestSchema as createSourceCheckoutGrantRequestSchema,
  createGitCredentialRequestSchema as createSourceGitCredentialRequestSchema,
  createRepositoryRequestSchema as createSourceRepositoryRequestSchema,
  dismissNotificationRequestSchema,
  executionIdInputSchema,
  executionScheduleListQuerySchema,
  executionScheduleIdInputSchema,
  executionScheduleRequestSchema,
  governanceAPIActivitiesQuerySchema as apiActivitiesQuerySchema,
  governanceCreateExportRequestSchema as createExportRequestSchema,
  isGovernanceApiError,
  isNotificationsApiError,
  runIdInputSchema,
  isBillingApiError,
  isIAMApiError,
  isProjectsApiError,
  isSandboxRentalApiError,
  isSandboxRentalNotFound,
  isSourceApiError as isSourceCodeHostingApiError,
  markNotificationReadRequestSchema,
  notificationsListQuerySchema,
  publishTestNotificationRequestSchema,
  putNotificationPreferencesRequestSchema,
  runListQuerySchema,
  runLogSearchQuerySchema,
  sandboxAnalyticsQuerySchema,
  updateOrganizationRequestSchema,
  type BillingCheckoutRequest as CheckoutRequest,
  type BillingContractChangeRequest as ContractChangeRequest,
  type BillingContractRequest as ContractRequest,
  type BillingContractsResponse as ContractsResponse,
  type BillingEntitlementBucketSection as EntitlementBucketSection,
  type BillingEntitlementProductSection as EntitlementProductSection,
  type BillingEntitlementSlot as EntitlementSlot,
  type BillingEntitlementSourceTotal as EntitlementSourceTotal,
  type BillingEntitlementsView as EntitlementsView,
  type BillingPlansResponse as PlansResponse,
  type BillingPortalRequest as PortalRequest,
  type BillingStatement as Statement,
  type BillingStatementQuery as StatementQuery,
  type CancelBillingContractRequest as CancelContractRequest,
  type CacheGeneration,
  type CacheGenerationIdInput,
  type CachePathDeleteRequest,
  type CachePathDeleteResult,
  type Cache,
  type CacheIdInput,
  type CreateCheckoutGrantRequest as CreateSourceCheckoutGrantRequest,
  type CreateGitCredentialRequest as CreateSourceGitCredentialRequest,
  type CreateProjectRequest,
  type CreateRepositoryRequest as CreateSourceRepositoryRequest,
  type CostsAnalytics,
  type DismissNotificationRequest,
  type Execution,
  type ExecutionLogs,
  type ExecutionSchedule,
  type ExecutionScheduleIdInput,
  type ExecutionScheduleListQueryInput,
  type ExecutionScheduleRequest,
  type ExecutionSchedules,
  type Member,
  type MarkNotificationReadRequest,
  type Notification,
  type NotificationAccepted,
  type NotificationList,
  type NotificationSummary,
  type NotificationsListQuery,
  type Organization,
  type OrganizationMetadata,
  type PublishTestNotificationRequest,
  type PutNotificationPreferencesRequest,
  type Project,
  type ProjectList,
  type GitHubInstallation,
  type GitHubInstallationConnect,
  type GovernanceAPIActivities,
  type GovernanceAPIActivity,
  type GovernanceCreateExportRequest as CreateExportRequest,
  type GovernanceExportJob,
  type JobsAnalytics,
  type RunListQuery,
  type RunListQueryInput,
  type RunLogSearchPage,
  type RunnerSizingAnalytics,
  type RunsPage,
  type SourceBlob,
  type SourceCheckoutGrant,
  type SourceGitCredential,
  type SourceRefs,
  type SourceRepository,
  type SourceRepositoryList,
  type SourceTree,
  type SourceWorkflowRunList,
  type UpdateOrganizationRequest,
  Verself,
} from "@verself/sdk";
import type {
  ProfileSnapshot,
  PutProfilePreferencesRequest,
  UpdateProfileIdentityRequest,
} from "~/lib/profile-api";
import { consoleAuthMiddleware, getProductAccessToken, type ConsoleAuthContext } from "./auth";

const IAM_SERVICE_BASE_URL = requireURLFromEnv("IAM_SERVICE_BASE_URL");
const GOVERNANCE_SERVICE_BASE_URL = requireURLFromEnv("GOVERNANCE_SERVICE_BASE_URL");
const PROFILE_SERVICE_BASE_URL = requireURLFromEnv("PROFILE_SERVICE_BASE_URL");
const NOTIFICATIONS_SERVICE_BASE_URL = requireURLFromEnv("NOTIFICATIONS_SERVICE_BASE_URL");
const PROJECTS_SERVICE_BASE_URL = requireURLFromEnv("PROJECTS_SERVICE_BASE_URL");
const BILLING_SERVICE_BASE_URL = requireURLFromEnv("BILLING_SERVICE_BASE_URL");
const SOURCE_CODE_HOSTING_SERVICE_BASE_URL = requireURLFromEnv(
  "SOURCE_CODE_HOSTING_SERVICE_BASE_URL",
);
const SANDBOX_RENTAL_SERVICE_BASE_URL = requireURLFromEnv("SANDBOX_RENTAL_SERVICE_BASE_URL");

export { IAMApiError, isIAMApiError };
export { GovernanceApiError, isGovernanceApiError };
export { ProfileApiError, isProfileApiError };
export { NotificationsApiError, isNotificationsApiError };
export { ProjectsApiError, isProjectsApiError };
export { BillingApiError, isBillingApiError };
export { SourceCodeHostingApiError, isSourceCodeHostingApiError };
export { SandboxRentalApiError, isSandboxRentalApiError, isSandboxRentalNotFound };
export type {
  CreateExportRequest,
  GovernanceAPIActivities,
  GovernanceAPIActivity,
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
  ExecutionScheduleListQueryInput,
  ExecutionScheduleRequest,
  ExecutionSchedules,
  Statement,
  StatementQuery,
  PortalRequest,
  PlansResponse,
  ContractRequest,
  ContractChangeRequest,
  ContractsResponse,
  CostsAnalytics,
  RunListQuery,
  RunListQueryInput,
  RunLogSearchPage,
  RunsPage,
  ExecutionLogs,
  CacheGeneration,
  CacheGenerationIdInput,
  CachePathDeleteRequest,
  CachePathDeleteResult,
  Cache,
  CacheIdInput,
  GitHubInstallation,
  GitHubInstallationConnect,
  JobsAnalytics,
  RunnerSizingAnalytics,
};
export type { Member, Organization, OrganizationMetadata, UpdateOrganizationRequest };

async function iamSDK(context: ConsoleAuthContext | undefined) {
  const accessToken = await getProductAccessToken(context);
  return new Verself({
    bearerToken: accessToken,
    iamURL: IAM_SERVICE_BASE_URL,
    projectsURL: PROJECTS_SERVICE_BASE_URL,
  });
}

async function governanceSDK(context: ConsoleAuthContext | undefined) {
  const accessToken = await getProductAccessToken(context);
  return new Verself({ bearerToken: accessToken, governanceURL: GOVERNANCE_SERVICE_BASE_URL });
}

async function profileClientOptions(context: ConsoleAuthContext | undefined) {
  const accessToken = await getProductAccessToken(context);
  return {
    accessToken,
    baseUrl: PROFILE_SERVICE_BASE_URL,
  };
}

async function notificationsSDK(context: ConsoleAuthContext | undefined) {
  const accessToken = await getProductAccessToken(context);
  return new Verself({
    bearerToken: accessToken,
    notificationsURL: NOTIFICATIONS_SERVICE_BASE_URL,
  });
}

async function projectsSDK(context: ConsoleAuthContext | undefined) {
  const accessToken = await getProductAccessToken(context);
  return new Verself({ bearerToken: accessToken, projectsURL: PROJECTS_SERVICE_BASE_URL });
}

async function billingSDK(context: ConsoleAuthContext | undefined) {
  const accessToken = await getProductAccessToken(context);
  return new Verself({ bearerToken: accessToken, billingURL: BILLING_SERVICE_BASE_URL });
}

async function sourceCodeHostingSDK(context: ConsoleAuthContext | undefined) {
  const accessToken = await getProductAccessToken(context);
  return new Verself({ bearerToken: accessToken, sourceURL: SOURCE_CODE_HOSTING_SERVICE_BASE_URL });
}

async function sandboxRentalSDK(context: ConsoleAuthContext | undefined) {
  const accessToken = await getProductAccessToken(context);
  return new Verself({
    bearerToken: accessToken,
    sandboxURL: SANDBOX_RENTAL_SERVICE_BASE_URL,
  });
}

export const getOrganization = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return (await iamSDK(context)).iam.getOrganization();
  });

export const listMyOrganizations = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return (await iamSDK(context)).iam.listMyOrganizations();
  });

export const updateOrganization = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(updateOrganizationRequestSchema)
  .handler(async ({ context, data }) => {
    return (await iamSDK(context)).iam.updateOrganization(data);
  });

export const getMembers = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return (await iamSDK(context)).iam.listMembers();
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
    return (await notificationsSDK(context)).notifications.list(data);
  });

export const getNotificationSummary = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return (await notificationsSDK(context)).notifications.summary();
  });

export const putNotificationPreferences = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(putNotificationPreferencesRequestSchema)
  .handler(async ({ context, data }) => {
    return (await notificationsSDK(context)).notifications.putPreferences(data);
  });

export const markNotificationRead = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(markNotificationReadRequestSchema)
  .handler(async ({ context, data }) => {
    return (await notificationsSDK(context)).notifications.markRead(data);
  });

export const markNotificationReadByID = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(dismissNotificationRequestSchema)
  .handler(async ({ context, data }) => {
    return (await notificationsSDK(context)).notifications.markNotificationRead(
      data.notification_id,
    );
  });

export const dismissNotification = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(dismissNotificationRequestSchema)
  .handler(async ({ context, data }) => {
    return (await notificationsSDK(context)).notifications.dismiss(data.notification_id);
  });

export const clearNotifications = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return (await notificationsSDK(context)).notifications.clear();
  });

export const publishTestNotification = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(publishTestNotificationRequestSchema)
  .handler(async ({ context, data }) => {
    return (await notificationsSDK(context)).notifications.publishTest(data);
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
    return (await sourceCodeHostingSDK(context)).source.listRepositories(
      data?.projectId ? { projectId: data.projectId } : {},
    );
  });

export const createSourceRepository = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(createSourceRepositoryRequestSchema)
  .handler(async ({ context, data }) => {
    return (await sourceCodeHostingSDK(context)).source.createRepository(data);
  });

export const createSourceGitCredential = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(sourceGitCredentialInputSchema)
  .handler(async ({ context, data }) => {
    return (await sourceCodeHostingSDK(context)).source.createGitCredential(data);
  });

export const getSourceRepository = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(sourceRepositoryIDInputSchema)
  .handler(async ({ context, data }) => {
    return (await sourceCodeHostingSDK(context)).source.getRepository(data.repoId);
  });

export const listSourceRefs = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(sourceRepositoryIDInputSchema)
  .handler(async ({ context, data }) => {
    return (await sourceCodeHostingSDK(context)).source.listRefs(data.repoId);
  });

export const listSourceWorkflowRuns = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(sourceRepositoryIDInputSchema)
  .handler(async ({ context, data }) => {
    return (await sourceCodeHostingSDK(context)).source.listWorkflowRuns(data.repoId);
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
    return (await sourceCodeHostingSDK(context)).source.getTree(treeInput);
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
    return (await sourceCodeHostingSDK(context)).source.getBlob(blobInput);
  });

export const createSourceCheckoutGrant = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(sourceCheckoutGrantInputSchema)
  .handler(async ({ context, data }) => {
    return (await sourceCodeHostingSDK(context)).source.createCheckoutGrant(data.repoId, data.body);
  });

export const listGovernanceAPIActivities = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(apiActivitiesQuerySchema)
  .handler(async ({ context, data }) => {
    return (await governanceSDK(context)).governance.listAPIActivities(data);
  });

export const listGovernanceDataExports = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return (await governanceSDK(context)).governance.listDataExports();
  });

export const createGovernanceDataExport = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(createExportRequestSchema)
  .handler(async ({ context, data }) => {
    return (await governanceSDK(context)).governance.createDataExport(data);
  });

const governanceDownloadRequestSchema = v.strictObject({
  export_id: v.pipe(v.string(), v.uuid()),
});

export const downloadGovernanceDataExport = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(governanceDownloadRequestSchema)
  .handler(async ({ context, data }) => {
    const artifact = await (
      await governanceSDK(context)
    ).governance.downloadDataExport(data.export_id);
    const bytes = Buffer.from(artifact.data);
    return {
      content_type: artifact.content_type,
      data_base64: bytes.toString("base64"),
      file_name: artifact.file_name,
    };
  });

export const getEntitlements = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return (await billingSDK(context)).billing.getEntitlements();
  });

export const getContracts = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return (await billingSDK(context)).billing.getContracts();
  });

export const getPlans = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return (await billingSDK(context)).billing.getPlans({ productId: "sandbox" });
  });

export const getStatement = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(statementQuerySchema)
  .handler(async ({ context, data }) => {
    return (await billingSDK(context)).billing.getStatement({ productId: data.product_id });
  });

export const createCheckoutSession = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(checkoutRequestSchema)
  .handler(async ({ context, data }) => {
    return (await billingSDK(context)).billing.createCheckoutSession(data);
  });

export const createContractSession = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(contractRequestSchema)
  .handler(async ({ context, data }) => {
    return (await billingSDK(context)).billing.createContractSession(data);
  });

export const createContractChangeSession = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(contractChangeRequestSchema)
  .handler(async ({ context, data }) => {
    return (await billingSDK(context)).billing.createContractChangeSession(data);
  });

export const createPortalSession = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(portalRequestSchema)
  .handler(async ({ context, data }) => {
    return (await billingSDK(context)).billing.createPortalSession(data);
  });

export const cancelContract = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(cancelContractRequestSchema)
  .handler(async ({ context, data }) => {
    return (await billingSDK(context)).billing.cancelContract(data);
  });

export const getExecution = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(executionIdInputSchema)
  .handler(async ({ context, data }) => {
    return (await sandboxRentalSDK(context)).sandbox.getExecution(data.executionId);
  });

export const getExecutionLogs = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(executionIdInputSchema)
  .handler(async ({ context, data }) => {
    return (await sandboxRentalSDK(context)).sandbox.getExecutionLogs(data.executionId);
  });

export const getRun = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(runIdInputSchema)
  .handler(async ({ context, data }) => {
    return (await sandboxRentalSDK(context)).sandbox.getRun(data.runId);
  });

export const listRuns = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(v.optional(runListQuerySchema))
  .handler(async ({ context, data }) => {
    return (await sandboxRentalSDK(context)).sandbox.listRuns(data);
  });

export const searchRunLogs = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(v.optional(runLogSearchQuerySchema))
  .handler(async ({ context, data }) => {
    return (await sandboxRentalSDK(context)).sandbox.searchRunLogs(data);
  });

export const getJobsAnalytics = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(v.optional(sandboxAnalyticsQuerySchema))
  .handler(async ({ context, data }) => {
    return (await sandboxRentalSDK(context)).sandbox.getJobsAnalytics(data);
  });

export const getCostsAnalytics = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(v.optional(sandboxAnalyticsQuerySchema))
  .handler(async ({ context, data }) => {
    return (await sandboxRentalSDK(context)).sandbox.getCostsAnalytics(data);
  });

export const getRunnerSizingAnalytics = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(v.optional(sandboxAnalyticsQuerySchema))
  .handler(async ({ context, data }) => {
    return (await sandboxRentalSDK(context)).sandbox.getRunnerSizingAnalytics(data);
  });

export const listCaches = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return (await sandboxRentalSDK(context)).sandbox.listCaches();
  });

export const listCacheGenerations = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(cacheIdInputSchema)
  .handler(async ({ context, data }) => {
    return (await sandboxRentalSDK(context)).sandbox.listCacheGenerations(data.cacheId);
  });

export const deleteCacheGeneration = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(cacheGenerationIdInputSchema)
  .handler(async ({ context, data }) => {
    return (await sandboxRentalSDK(context)).sandbox.deleteCacheGeneration(data.cacheGenerationId);
  });

export const deleteCachePath = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(cachePathDeleteRequestSchema)
  .handler(async ({ context, data }) => {
    return (await sandboxRentalSDK(context)).sandbox.deleteCachePath(data.cacheId, data.path);
  });

export const listGitHubInstallations = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return (await sandboxRentalSDK(context)).sandbox.listGitHubInstallations();
  });

export const beginGitHubInstallation = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .handler(async ({ context }) => {
    return (await sandboxRentalSDK(context)).sandbox.beginGitHubInstallation();
  });

export const listExecutionSchedules = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(v.optional(executionScheduleListQuerySchema))
  .handler(async ({ context, data }) => {
    return (await sandboxRentalSDK(context)).sandbox.listExecutionSchedules(data);
  });

export const createExecutionSchedule = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(executionScheduleRequestSchema)
  .handler(async ({ context, data }) => {
    return (await sandboxRentalSDK(context)).sandbox.createExecutionSchedule(data);
  });

export const getExecutionSchedule = createServerFn({ method: "GET" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(executionScheduleIdInputSchema)
  .handler(async ({ context, data }) => {
    return (await sandboxRentalSDK(context)).sandbox.getExecutionSchedule(data.scheduleId);
  });

export const pauseExecutionSchedule = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(executionScheduleIdInputSchema)
  .handler(async ({ context, data }) => {
    return (await sandboxRentalSDK(context)).sandbox.pauseSchedule(data.scheduleId);
  });

export const resumeExecutionSchedule = createServerFn({ method: "POST" })
  .middleware([consoleAuthMiddleware])
  .inputValidator(executionScheduleIdInputSchema)
  .handler(async ({ context, data }) => {
    return (await sandboxRentalSDK(context)).sandbox.resumeSchedule(data.scheduleId);
  });
