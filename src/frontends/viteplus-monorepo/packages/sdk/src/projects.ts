import * as v from "valibot";
import { createClient, type Client } from "./__generated/projects-api/client/index.js";
import {
  type CreateProjectRequestWritable,
  type ListProjectsData,
  createProject,
  getProject,
  listProjects,
} from "./__generated/projects-api/index.js";
import {
  vCreateProjectBody,
  vCreateProjectRequest,
  vGetProjectPath,
  vListProjectsQuery,
  vProject,
  vProjectList,
} from "./__generated/projects-api/valibot.gen.js";
import type { BearerClientOptions } from "./service-api";
import {
  ServiceApiError,
  createBearerJSONHeaders,
  idempotencyHeaders,
  throwGeneratedServiceError,
} from "./service-api";

export type ProjectsClientOptions = BearerClientOptions;

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
  return v.parse(vProject, input);
}

export type Project = ReturnType<typeof parseProject>;

function parseProjectList(input: unknown) {
  const parsed = v.parse(vProjectList, input);
  return {
    next_cursor: parsed.next_cursor ?? "",
    projects: parsed.projects?.map((project) => parseProject(project)) ?? [],
  };
}

export type ProjectList = ReturnType<typeof parseProjectList>;

export const createProjectRequestSchema = vCreateProjectRequest;

export type CreateProjectRequest = v.InferOutput<typeof createProjectRequestSchema>;

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

  async create(body: CreateProjectRequest): Promise<Project> {
    const client = createProjectsClient(this.#options);
    const parsedBody = removeUndefined(
      v.parse(vCreateProjectBody, body),
    ) as CreateProjectRequestWritable;
    const path = "/api/v1/projects";
    const result = await createProject({
      client,
      body: parsedBody,
      headers: idempotencyHeaders("project"),
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
}
