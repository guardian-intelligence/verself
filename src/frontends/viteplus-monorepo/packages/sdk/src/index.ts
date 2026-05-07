import { Billing } from "./billing";
import { Governance } from "./governance";
import { IAM } from "./iam";
import { Notifications } from "./notifications";
import { Projects } from "./projects";
import { SandboxRental } from "./sandbox";
import { Secrets } from "./secrets";
import { Source } from "./source";

export const DEFAULT_SERVER_URL = "https://verself.sh";
export const DEFAULT_IAM_URL = "https://iam.api.verself.sh";
export const DEFAULT_PROJECTS_URL = "https://projects.api.verself.sh";
export const DEFAULT_NOTIFICATIONS_URL = "https://notifications.api.verself.sh";
export const DEFAULT_BILLING_URL = "https://billing.api.verself.sh";
export const DEFAULT_GOVERNANCE_URL = "https://governance.api.verself.sh";
export const DEFAULT_SANDBOX_URL = "https://sandbox.api.verself.sh";
export const DEFAULT_SECRETS_URL = "https://secrets.api.verself.sh";
export const DEFAULT_SOURCE_URL = "https://source.api.verself.sh";

export type VerselfOptions = {
  bearerToken: string;
  serverURL?: string | undefined;
  iamURL?: string | undefined;
  projectsURL?: string | undefined;
  notificationsURL?: string | undefined;
  billingURL?: string | undefined;
  governanceURL?: string | undefined;
  sandboxURL?: string | undefined;
  secretsURL?: string | undefined;
  sourceURL?: string | undefined;
  fetch?: typeof fetch | undefined;
  traceparent?: string | undefined;
};

export class Verself {
  readonly iam: IAM;
  readonly projects: Projects;
  readonly notifications: Notifications;
  readonly billing: Billing;
  readonly governance: Governance;
  readonly sandbox: SandboxRental;
  readonly secrets: Secrets;
  readonly source: Source;

  constructor(options: VerselfOptions) {
    const token = options.bearerToken.trim();
    if (token === "") {
      throw new Error("Verself SDK requires bearerToken");
    }
    this.iam = new IAM({
      accessToken: token,
      baseUrl: resolveIAMURL(options),
      ...(options.fetch ? { fetch: options.fetch } : {}),
      ...(options.traceparent ? { traceparent: options.traceparent } : {}),
    });
    this.projects = new Projects({
      accessToken: token,
      baseUrl: resolveProjectsURL(options),
      ...(options.fetch ? { fetch: options.fetch } : {}),
      ...(options.traceparent ? { traceparent: options.traceparent } : {}),
    });
    this.notifications = new Notifications({
      accessToken: token,
      baseUrl: resolveNotificationsURL(options),
      ...(options.fetch ? { fetch: options.fetch } : {}),
      ...(options.traceparent ? { traceparent: options.traceparent } : {}),
    });
    this.billing = new Billing({
      accessToken: token,
      baseUrl: resolveBillingURL(options),
      ...(options.fetch ? { fetch: options.fetch } : {}),
      ...(options.traceparent ? { traceparent: options.traceparent } : {}),
    });
    this.governance = new Governance({
      accessToken: token,
      baseUrl: resolveGovernanceURL(options),
      ...(options.fetch ? { fetch: options.fetch } : {}),
      ...(options.traceparent ? { traceparent: options.traceparent } : {}),
    });
    this.sandbox = new SandboxRental({
      accessToken: token,
      baseUrl: resolveSandboxURL(options),
      ...(options.fetch ? { fetch: options.fetch } : {}),
      ...(options.traceparent ? { traceparent: options.traceparent } : {}),
    });
    this.secrets = new Secrets({
      accessToken: token,
      baseUrl: resolveSecretsURL(options),
      ...(options.fetch ? { fetch: options.fetch } : {}),
      ...(options.traceparent ? { traceparent: options.traceparent } : {}),
    });
    this.source = new Source({
      accessToken: token,
      baseUrl: resolveSourceURL(options),
      ...(options.fetch ? { fetch: options.fetch } : {}),
      ...(options.traceparent ? { traceparent: options.traceparent } : {}),
    });
  }
}

function resolveIAMURL(options: VerselfOptions): string {
  return resolveServiceURL("iam", options.iamURL, options.serverURL, DEFAULT_IAM_URL);
}

function resolveProjectsURL(options: VerselfOptions): string {
  return resolveServiceURL(
    "projects",
    options.projectsURL,
    options.serverURL,
    DEFAULT_PROJECTS_URL,
  );
}

function resolveNotificationsURL(options: VerselfOptions): string {
  return resolveServiceURL(
    "notifications",
    options.notificationsURL,
    options.serverURL,
    DEFAULT_NOTIFICATIONS_URL,
  );
}

function resolveBillingURL(options: VerselfOptions): string {
  return resolveServiceURL("billing", options.billingURL, options.serverURL, DEFAULT_BILLING_URL);
}

function resolveGovernanceURL(options: VerselfOptions): string {
  return resolveServiceURL(
    "governance",
    options.governanceURL,
    options.serverURL,
    DEFAULT_GOVERNANCE_URL,
  );
}

function resolveSandboxURL(options: VerselfOptions): string {
  return resolveServiceURL("sandbox", options.sandboxURL, options.serverURL, DEFAULT_SANDBOX_URL);
}

function resolveSecretsURL(options: VerselfOptions): string {
  return resolveServiceURL("secrets", options.secretsURL, options.serverURL, DEFAULT_SECRETS_URL);
}

function resolveSourceURL(options: VerselfOptions): string {
  return resolveServiceURL("source", options.sourceURL, options.serverURL, DEFAULT_SOURCE_URL);
}

function resolveServiceURL(
  service:
    | "iam"
    | "projects"
    | "notifications"
    | "billing"
    | "governance"
    | "sandbox"
    | "secrets"
    | "source",
  serviceURL: string | undefined,
  serverURL: string | undefined,
  defaultURL: string,
): string {
  if (serviceURL !== undefined) {
    return normalizeURL(new URL(serviceURL));
  }
  if (serverURL === undefined) {
    return defaultURL;
  }
  const url = new URL(serverURL);
  const expectedPrefix = `${service}.api.`;
  if (url.hostname.startsWith(expectedPrefix)) {
    url.pathname = "";
    url.search = "";
    url.hash = "";
    return normalizeURL(url);
  }
  if (url.hostname.includes(".api.")) {
    throw new Error(
      "Verself SDK serverURL must be the installation apex; pass the service URL override for service API hosts",
    );
  }
  url.hostname = `${expectedPrefix}${url.hostname}`;
  url.pathname = "";
  url.search = "";
  url.hash = "";
  return normalizeURL(url);
}

function normalizeURL(url: URL): string {
  return url.toString().replace(/\/$/, "");
}

export {
  Billing,
  BillingApiError,
  billingDocumentsQuerySchema,
  billingGrantsQuerySchema,
  billingProductQuerySchema,
  cancelContractRequestSchema as cancelBillingContractRequestSchema,
  checkoutRequestSchema as billingCheckoutRequestSchema,
  contractChangeRequestSchema as billingContractChangeRequestSchema,
  contractRequestSchema as billingContractRequestSchema,
  isBillingApiError,
  portalRequestSchema as billingPortalRequestSchema,
  statementQuerySchema as billingStatementQuerySchema,
  type BillingClientOptions,
  type BillingDocumentsQuery,
  type BillingDocumentsQueryInput,
  type BillingGrantsQuery,
  type BillingGrantsQueryInput,
  type BillingProductQuery,
  type BillingProductQueryInput,
  type BillingPlan,
  type CancelContractRequest as CancelBillingContractRequest,
  type CheckoutRequest as BillingCheckoutRequest,
  type CheckoutSession as BillingCheckoutSession,
  type Contract,
  type ContractChangeRequest as BillingContractChangeRequest,
  type ContractChangeSession as BillingContractChangeSession,
  type ContractRequest as BillingContractRequest,
  type ContractSession as BillingContractSession,
  type ContractsResponse as BillingContractsResponse,
  type DocumentsResponse as BillingDocumentsResponse,
  type EntitlementBucketSection as BillingEntitlementBucketSection,
  type EntitlementProductSection as BillingEntitlementProductSection,
  type EntitlementSlot as BillingEntitlementSlot,
  type EntitlementSourceTotal as BillingEntitlementSourceTotal,
  type EntitlementsView as BillingEntitlementsView,
  type Grant as BillingGrant,
  type GrantsResponse as BillingGrantsResponse,
  type PlansResponse as BillingPlansResponse,
  type PortalRequest as BillingPortalRequest,
  type PortalSession as BillingPortalSession,
  type Statement as BillingStatement,
  type StatementQuery as BillingStatementQuery,
  type StatementQueryInput as BillingStatementQueryInput,
} from "./billing";

export {
  Governance,
  GovernanceApiError,
  governanceAuditEventsQuerySchema,
  governanceCreateExportRequestSchema,
  isGovernanceApiError,
  type GovernanceAuditEvent,
  type GovernanceAuditEvents,
  type GovernanceAuditEventsQuery,
  type GovernanceAuditEventsQueryInput,
  type GovernanceClientOptions,
  type GovernanceCreateExportRequest,
  type GovernanceCreateExportRequestInput,
  type GovernanceExportArtifact,
  type GovernanceExportJob,
  type GovernanceMutationOptions,
} from "./governance";

export {
  IAM,
  IAMApiError,
  createAPICredentialRequestSchema,
  inviteMemberRequestSchema,
  isIAMApiError,
  putMemberCapabilitiesRequestSchema,
  rollAPICredentialRequestSchema,
  updateMemberRolesRequestSchema,
  updateOrganizationRequestSchema,
  type APICredential,
  type APICredentialIssuedMaterial,
  type CreateAPICredentialRequest,
  type CreateAPICredentialResponse,
  type IAMClientOptions,
  type IAMMutationOptions,
  type InviteMemberRequest,
  type InviteMemberResponse,
  type Member,
  type MemberCapabilities,
  type MemberCapabilitiesDocument,
  type MemberCapability,
  type Organization,
  type OrganizationMetadata,
  type PutMemberCapabilitiesRequest,
  type RollAPICredentialRequest,
  type RollAPICredentialResponse,
  type UpdateMemberRolesRequest,
  type UpdateOrganizationRequest,
} from "./iam";

export {
  Projects,
  ProjectsApiError,
  createProjectEnvironmentRequestSchema,
  createProjectRequestSchema,
  isProjectsApiError,
  projectLifecycleRequestSchema,
  updateProjectEnvironmentRequestSchema,
  updateProjectRequestSchema,
  type CreateProjectRequest,
  type CreateProjectEnvironmentRequest,
  type ListProjectsInput,
  type Project,
  type ProjectEnvironment,
  type ProjectEnvironmentList,
  type ProjectLifecycleRequest,
  type ProjectList,
  type ProjectsClientOptions,
  type ProjectsMutationOptions,
  type UpdateProjectEnvironmentRequest,
  type UpdateProjectRequest,
} from "./projects";

export {
  Notifications,
  NotificationsApiError,
  dismissNotificationRequestSchema,
  isNotificationsApiError,
  markNotificationReadRequestSchema,
  notificationsListQuerySchema,
  publishTestNotificationRequestSchema,
  putNotificationPreferencesRequestSchema,
  type DismissNotificationRequest,
  type MarkNotificationReadRequest,
  type Notification,
  type NotificationAccepted,
  type NotificationList,
  type NotificationSummary,
  type NotificationsClientOptions,
  type NotificationsListQuery,
  type NotificationsMutationOptions,
  type PublishTestNotificationRequest,
  type PutNotificationPreferencesRequest,
} from "./notifications";

export * from "./sandbox";

export {
  Secrets,
  SecretsApiError,
  isSecretsApiError,
  putSecretRequestSchema,
  resolveSecretsRequestSchema,
  type DeleteSecretScope,
  type ListSecretsInput,
  type PutSecretRequest,
  type ResolveSecretsRequest,
  type ResolvedSecrets,
  type Secret,
  type SecretList,
  type SecretValue,
  type SecretsClientOptions,
  type SecretsMutationOptions,
} from "./secrets";

export {
  Source,
  SourceApiError,
  createCheckoutGrantRequestSchema,
  createGitCredentialRequestSchema,
  createRepositoryRequestSchema,
  createWorkflowRunRequestSchema,
  isSourceApiError,
  type CreateCheckoutGrantRequest,
  type CreateGitCredentialRequest,
  type CreateRepositoryRequest,
  type CreateWorkflowRunRequest,
  type ListRepositoriesInput,
  type SourceBlob,
  type SourceBlobInput,
  type SourceCheckoutGrant,
  type SourceClientOptions,
  type SourceGitCredential,
  type SourceMutationOptions,
  type SourceRefs,
  type SourceRepository,
  type SourceRepositoryList,
  type SourceTree,
  type SourceTreeInput,
  type SourceWorkflowRun,
  type SourceWorkflowRunList,
} from "./source";
