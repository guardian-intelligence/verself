import { IAM } from "./iam";
import { Projects } from "./projects";
import { SandboxRental } from "./sandbox";
import { Source } from "./source";

export const DEFAULT_SERVER_URL = "https://verself.sh";
export const DEFAULT_IAM_URL = "https://iam.api.verself.sh";
export const DEFAULT_PROJECTS_URL = "https://projects.api.verself.sh";
export const DEFAULT_SANDBOX_URL = "https://sandbox.api.verself.sh";
export const DEFAULT_SOURCE_URL = "https://source.api.verself.sh";

export type VerselfOptions = {
  bearerToken: string;
  serverURL?: string | undefined;
  iamURL?: string | undefined;
  projectsURL?: string | undefined;
  sandboxURL?: string | undefined;
  sourceURL?: string | undefined;
  fetch?: typeof fetch | undefined;
  traceparent?: string | undefined;
};

export class Verself {
  readonly iam: IAM;
  readonly projects: Projects;
  readonly sandbox: SandboxRental;
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
    this.sandbox = new SandboxRental({
      accessToken: token,
      baseUrl: resolveSandboxURL(options),
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

function resolveSandboxURL(options: VerselfOptions): string {
  return resolveServiceURL("sandbox", options.sandboxURL, options.serverURL, DEFAULT_SANDBOX_URL);
}

function resolveSourceURL(options: VerselfOptions): string {
  return resolveServiceURL("source", options.sourceURL, options.serverURL, DEFAULT_SOURCE_URL);
}

function resolveServiceURL(
  service: "iam" | "projects" | "sandbox" | "source",
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

export * from "./sandbox";

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
