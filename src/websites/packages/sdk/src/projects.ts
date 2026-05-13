import * as v from "valibot";
import { createClient, type Client } from "./__generated/projects-api/client/index.js";
import {
  type ArchiveProjectData,
  type ArchiveProjectEnvironmentData,
  type CreateProjectData,
  type CreateProjectEnvironmentData,
  type ListProjectsData,
  type PatchProjectData,
  type PatchProjectEnvironmentData,
  type RestoreProjectData,
  archiveProject,
  archiveProjectEnvironment,
  createProject,
  createProjectEnvironment,
  getProject,
  listProjectEnvironments,
  listProjects,
  patchProject,
  patchProjectEnvironment,
  restoreProject,
} from "./__generated/projects-api/index.js";
import {
  vArchiveProjectBody,
  vArchiveProjectEnvironmentBody,
  vArchiveProjectEnvironmentPath,
  vArchiveProjectPath,
  vCreateProjectEnvironmentBody,
  vCreateProjectEnvironmentPath,
  vCreateProjectBody,
  vGetProjectPath,
  vListProjectEnvironmentsPath,
  vListProjectEnvironmentsResponse,
  vListProjectsQuery,
  vListProjectsResponse,
  vPatchProjectBody,
  vPatchProjectEnvironmentBody,
  vPatchProjectEnvironmentPath,
  vPatchProjectPath,
  vProjectEnvironmentSummary,
  vProjectSummary,
  vRestoreProjectBody,
  vRestoreProjectPath,
} from "./__generated/projects-api/valibot.gen.js";
import type { BearerClientOptions } from "./service-api";
import {
  ServiceApiError,
  createBearerJSONHeaders,
  idempotencyHeaders,
  throwGeneratedServiceError,
} from "./service-api";

export type ProjectsClientOptions = BearerClientOptions;

export type ProjectsMutationOptions = {
  idempotencyKey?: string | undefined;
};

export class ProjectsApiError extends ServiceApiError {
  constructor(status: number, path: string, body: string) {
    super("Projects API", status, path, body);
    this.name = "ProjectsApiError";
  }
}

export function isProjectsApiError(error: unknown): error is ProjectsApiError {
  return error instanceof ProjectsApiError;
}

function createProjectsClient(options: ProjectsClientOptions): Client {
  return createClient({
    baseUrl: options.baseUrl,
    headers: createBearerJSONHeaders(options.accessToken, options.traceparent),
    ...(options.fetch ? { fetch: options.fetch } : {}),
  });
}

function throwProjectsError(path: string, response: Response | undefined, error: unknown): never {
  throwGeneratedServiceError(ProjectsApiError, path, response, error);
}

function removeUndefined<T extends Record<string, unknown>>(input: T): Record<string, unknown> {
  return Object.fromEntries(Object.entries(input).filter(([, value]) => value !== undefined));
}

function parseProject(input: unknown) {
  return v.parse(vProjectSummary, input);
}

export type Project = ReturnType<typeof parseProject>;

function parseProjectEnvironment(input: unknown) {
  return v.parse(vProjectEnvironmentSummary, input);
}

export type ProjectEnvironment = ReturnType<typeof parseProjectEnvironment>;

function parseProjectList(input: unknown) {
  const parsed = v.parse(vListProjectsResponse, input);
  return {
    next_cursor: parsed.next_cursor ?? "",
    projects: parsed.projects.map((project) => parseProject(project)),
  };
}

export type ProjectList = ReturnType<typeof parseProjectList>;

function parseProjectEnvironmentList(input: unknown) {
  const parsed = v.parse(vListProjectEnvironmentsResponse, input);
  return {
    next_cursor: "",
    environments: parsed.environments.map((environment) => parseProjectEnvironment(environment)),
  };
}

export type ProjectEnvironmentList = ReturnType<typeof parseProjectEnvironmentList>;

export const createProjectRequestSchema = vCreateProjectBody;
export const createProjectEnvironmentRequestSchema = vCreateProjectEnvironmentBody;
export const updateProjectRequestSchema = vPatchProjectBody;
export const updateProjectEnvironmentRequestSchema = vPatchProjectEnvironmentBody;
export const projectLifecycleRequestSchema = vArchiveProjectBody;

export type CreateProjectRequest = v.InferOutput<typeof createProjectRequestSchema>;
export type CreateProjectEnvironmentRequest = v.InferOutput<
  typeof createProjectEnvironmentRequestSchema
>;
export type UpdateProjectRequest = v.InferOutput<typeof updateProjectRequestSchema>;
export type UpdateProjectEnvironmentRequest = v.InferOutput<
  typeof updateProjectEnvironmentRequestSchema
>;
export type ProjectLifecycleRequest = v.InferOutput<typeof projectLifecycleRequestSchema>;

export type ListProjectsInput = {
  state?: "active" | "archived" | undefined;
  limit?: number | undefined;
  cursor?: string | undefined;
};

export class Projects {
  readonly #options: ProjectsClientOptions;

  constructor(options: ProjectsClientOptions) {
    this.#options = options;
  }

  async list(input: ListProjectsInput = {}): Promise<ProjectList> {
    const client = createProjectsClient(this.#options);
    const parsedQuery = v.parse(vListProjectsQuery, {
      state: input.state ?? "active",
      limit: input.limit ?? 100,
      cursor: input.cursor,
    });
    const query = removeUndefined({
      state: parsedQuery.state,
      limit: parsedQuery.limit === undefined ? undefined : Number(parsedQuery.limit),
      cursor: parsedQuery.cursor,
    }) as NonNullable<ListProjectsData["query"]>;
    const path = "/api/v1/projects";
    const result = await listProjects({
      client,
      query,
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwProjectsError(path, result.response, result.error);
    }
    return parseProjectList(result.data);
  }

  async create(
    body: CreateProjectRequest,
    options: ProjectsMutationOptions = {},
  ): Promise<Project> {
    const client = createProjectsClient(this.#options);
    const parsedBody = removeUndefined(
      v.parse(vCreateProjectBody, body),
    ) as CreateProjectData["body"];
    const path = "/api/v1/projects";
    const result = await createProject({
      client,
      body: parsedBody,
      headers: idempotencyHeaders("project", options.idempotencyKey),
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwProjectsError(path, result.response, result.error);
    }
    return parseProject(result.data);
  }

  async get(projectId: string): Promise<Project> {
    const client = createProjectsClient(this.#options);
    const pathParams = v.parse(vGetProjectPath, { project_id: projectId });
    const path = `/api/v1/projects/${projectId}`;
    const result = await getProject({
      client,
      path: pathParams,
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwProjectsError(path, result.response, result.error);
    }
    return parseProject(result.data);
  }

  async update(
    projectId: string,
    body: UpdateProjectRequest,
    options: ProjectsMutationOptions = {},
  ): Promise<Project> {
    const client = createProjectsClient(this.#options);
    const pathParams = v.parse(vPatchProjectPath, { project_id: projectId });
    const parsedBody = removeUndefined(
      v.parse(vPatchProjectBody, body),
    ) as PatchProjectData["body"];
    const path = `/api/v1/projects/${projectId}`;
    const result = await patchProject({
      client,
      path: pathParams,
      body: parsedBody,
      headers: idempotencyHeaders("project", options.idempotencyKey),
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwProjectsError(path, result.response, result.error);
    }
    return parseProject(result.data);
  }

  async archive(
    projectId: string,
    body: ProjectLifecycleRequest,
    options: ProjectsMutationOptions = {},
  ): Promise<Project> {
    const client = createProjectsClient(this.#options);
    const pathParams = v.parse(vArchiveProjectPath, { project_id: projectId });
    const parsedBody = v.parse(vArchiveProjectBody, body) as ArchiveProjectData["body"];
    const path = `/api/v1/projects/${projectId}/archive`;
    const result = await archiveProject({
      client,
      path: pathParams,
      body: parsedBody,
      headers: idempotencyHeaders("project", options.idempotencyKey),
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwProjectsError(path, result.response, result.error);
    }
    return parseProject(result.data);
  }

  async restore(
    projectId: string,
    body: ProjectLifecycleRequest,
    options: ProjectsMutationOptions = {},
  ): Promise<Project> {
    const client = createProjectsClient(this.#options);
    const pathParams = v.parse(vRestoreProjectPath, { project_id: projectId });
    const parsedBody = v.parse(vRestoreProjectBody, body) as RestoreProjectData["body"];
    const path = `/api/v1/projects/${projectId}/restore`;
    const result = await restoreProject({
      client,
      path: pathParams,
      body: parsedBody,
      headers: idempotencyHeaders("project", options.idempotencyKey),
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwProjectsError(path, result.response, result.error);
    }
    return parseProject(result.data);
  }

  async listEnvironments(projectId: string): Promise<ProjectEnvironmentList> {
    const client = createProjectsClient(this.#options);
    const pathParams = v.parse(vListProjectEnvironmentsPath, { project_id: projectId });
    const path = `/api/v1/projects/${projectId}/environments`;
    const result = await listProjectEnvironments({
      client,
      path: pathParams,
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwProjectsError(path, result.response, result.error);
    }
    return parseProjectEnvironmentList(result.data);
  }

  async createEnvironment(
    projectId: string,
    body: CreateProjectEnvironmentRequest,
    options: ProjectsMutationOptions = {},
  ): Promise<ProjectEnvironment> {
    const client = createProjectsClient(this.#options);
    const pathParams = v.parse(vCreateProjectEnvironmentPath, { project_id: projectId });
    const parsedBody = removeUndefined(
      v.parse(vCreateProjectEnvironmentBody, body),
    ) as CreateProjectEnvironmentData["body"];
    const path = `/api/v1/projects/${projectId}/environments`;
    const result = await createProjectEnvironment({
      client,
      path: pathParams,
      body: parsedBody,
      headers: idempotencyHeaders("project-environment", options.idempotencyKey),
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwProjectsError(path, result.response, result.error);
    }
    return parseProjectEnvironment(result.data);
  }

  async updateEnvironment(
    projectId: string,
    environmentId: string,
    body: UpdateProjectEnvironmentRequest,
    options: ProjectsMutationOptions = {},
  ): Promise<ProjectEnvironment> {
    const client = createProjectsClient(this.#options);
    const pathParams = v.parse(vPatchProjectEnvironmentPath, {
      project_id: projectId,
      environment_id: environmentId,
    });
    const parsedBody = removeUndefined(
      v.parse(vPatchProjectEnvironmentBody, body),
    ) as PatchProjectEnvironmentData["body"];
    const path = `/api/v1/projects/${projectId}/environments/${environmentId}`;
    const result = await patchProjectEnvironment({
      client,
      path: pathParams,
      body: parsedBody,
      headers: idempotencyHeaders("project-environment", options.idempotencyKey),
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwProjectsError(path, result.response, result.error);
    }
    return parseProjectEnvironment(result.data);
  }

  async archiveEnvironment(
    projectId: string,
    environmentId: string,
    body: ProjectLifecycleRequest,
    options: ProjectsMutationOptions = {},
  ): Promise<ProjectEnvironment> {
    const client = createProjectsClient(this.#options);
    const pathParams = v.parse(vArchiveProjectEnvironmentPath, {
      project_id: projectId,
      environment_id: environmentId,
    });
    const parsedBody = v.parse(
      vArchiveProjectEnvironmentBody,
      body,
    ) as ArchiveProjectEnvironmentData["body"];
    const path = `/api/v1/projects/${projectId}/environments/${environmentId}/archive`;
    const result = await archiveProjectEnvironment({
      client,
      path: pathParams,
      body: parsedBody,
      headers: idempotencyHeaders("project-environment", options.idempotencyKey),
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwProjectsError(path, result.response, result.error);
    }
    return parseProjectEnvironment(result.data);
  }
}
