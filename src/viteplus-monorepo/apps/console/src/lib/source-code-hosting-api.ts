import * as v from "valibot";
import { createClient, type Client } from "../__generated/source-code-hosting-api/client/index.js";
import {
  type CreateCheckoutGrantRequestWritable,
  type CreateGitCredentialRequestWritable,
  type CreateRepositoryRequestWritable,
  type ListSourceRepositoriesData,
  type GetSourceBlobData,
  type GetSourceTreeData,
  type ListSourceWorkflowRunsData,
  createSourceCheckoutGrant,
  createSourceGitCredential,
  createSourceRepository,
  getSourceBlob,
  getSourceRepository,
  getSourceTree,
  listSourceWorkflowRuns,
  listSourceRefs,
  listSourceRepositories,
} from "../__generated/source-code-hosting-api/index.js";
import {
  vBlob,
  vCheckoutGrant,
  vCreateCheckoutGrantRequest,
  vCreateGitCredentialRequest,
  vCreateRepositoryRequest,
  vGetSourceBlobQuery,
  vGetSourceBlobPath,
  vGetSourceRepositoryPath,
  vGetSourceTreeQuery,
  vGetSourceTreePath,
  vListSourceRepositoriesQuery,
  vListSourceRefsPath,
  vListSourceWorkflowRunsPath,
  vGitCredential,
  vRefList,
  vRepository,
  vRepositoryList,
  vTree,
  vWorkflowRun,
  vWorkflowRunList,
} from "../__generated/source-code-hosting-api/valibot.gen.js";
import type { BearerClientOptions } from "./service-api";
import {
  ServiceApiError,
  createBearerJSONHeaders,
  idempotencyHeaders,
  throwGeneratedServiceError,
} from "./service-api";

export type SourceCodeHostingClientOptions = BearerClientOptions;

export class SourceCodeHostingApiError extends ServiceApiError {
  constructor(status: number, path: string, body: string) {
    super("Source code hosting API", status, path, body);
    this.name = "SourceCodeHostingApiError";
  }
}

export function isSourceCodeHostingApiError(error: unknown): error is SourceCodeHostingApiError {
  return error instanceof SourceCodeHostingApiError;
}

function createSourceClient(options: SourceCodeHostingClientOptions): Client {
  return createClient({
    baseUrl: options.baseUrl,
    headers: createBearerJSONHeaders(options.accessToken),
    ...(options.fetch ? { fetch: options.fetch } : {}),
  });
}

function throwSourceError(path: string, response: Response | undefined, error: unknown): never {
  throwGeneratedServiceError(SourceCodeHostingApiError, path, response, error);
}

function parseRepository(input: unknown) {
  return v.parse(vRepository, input);
}

export type SourceRepository = ReturnType<typeof parseRepository>;

function parseRepositoryList(input: unknown) {
  const parsed = v.parse(vRepositoryList, input);
  return {
    repositories: parsed.repositories?.map((repo) => parseRepository(repo)) ?? [],
  };
}

export type SourceRepositoryList = ReturnType<typeof parseRepositoryList>;

function parseRefs(input: unknown) {
  const parsed = v.parse(vRefList, input);
  return {
    refs: parsed.refs ?? [],
  };
}

export type SourceRefs = ReturnType<typeof parseRefs>;

function parseWorkflowRun(input: unknown) {
  return v.parse(vWorkflowRun, input);
}

export type SourceWorkflowRun = ReturnType<typeof parseWorkflowRun>;

function parseWorkflowRunList(input: unknown) {
  const parsed = v.parse(vWorkflowRunList, input);
  return {
    workflow_runs: parsed.workflow_runs?.map((run) => parseWorkflowRun(run)) ?? [],
  };
}

export type SourceWorkflowRunList = ReturnType<typeof parseWorkflowRunList>;

function parseTree(input: unknown) {
  const parsed = v.parse(vTree, input);
  return {
    entries: parsed.entries ?? [],
  };
}

export type SourceTree = ReturnType<typeof parseTree>;

function parseBlob(input: unknown) {
  return v.parse(vBlob, input);
}

export type SourceBlob = ReturnType<typeof parseBlob>;

function parseCheckoutGrant(input: unknown) {
  return v.parse(vCheckoutGrant, input);
}

export type SourceCheckoutGrant = ReturnType<typeof parseCheckoutGrant>;

function parseGitCredential(input: unknown) {
  return v.parse(vGitCredential, input);
}

export type SourceGitCredential = ReturnType<typeof parseGitCredential>;

export const createRepositoryRequestSchema = vCreateRepositoryRequest;
export const createCheckoutGrantRequestSchema = vCreateCheckoutGrantRequest;
export const createGitCredentialRequestSchema = vCreateGitCredentialRequest;

export type CreateRepositoryRequest = v.InferOutput<typeof createRepositoryRequestSchema>;
export type CreateCheckoutGrantRequest = v.InferOutput<typeof createCheckoutGrantRequestSchema>;
export type CreateGitCredentialRequest = v.InferOutput<typeof createGitCredentialRequestSchema>;

export async function listRepositories(
  options: SourceCodeHostingClientOptions & { projectId?: string },
): Promise<SourceRepositoryList> {
  const client = createSourceClient(options);
  const query =
    options.projectId === undefined
      ? undefined
      : v.parse(vListSourceRepositoriesQuery, { project_id: options.projectId });
  const path = "/api/v1/repos";
  const result = await listSourceRepositories({
    client,
    ...(query ? { query: query as NonNullable<ListSourceRepositoriesData["query"]> } : {}),
    responseStyle: "fields",
    throwOnError: false,
  });
  if (result.error !== undefined) {
    throwSourceError(path, result.response, result.error);
  }
  return parseRepositoryList(result.data);
}

function removeUndefined<T extends Record<string, unknown>>(input: T): Record<string, unknown> {
  return Object.fromEntries(Object.entries(input).filter(([, value]) => value !== undefined));
}

export async function createRepository(
  options: SourceCodeHostingClientOptions & { body: CreateRepositoryRequest },
): Promise<SourceRepository> {
  const client = createSourceClient(options);
  const body = removeUndefined(
    v.parse(vCreateRepositoryRequest, options.body),
  ) as CreateRepositoryRequestWritable;
  const path = "/api/v1/repos";
  const result = await createSourceRepository({
    client,
    body,
    headers: idempotencyHeaders("source-repo"),
    responseStyle: "fields",
    throwOnError: false,
  });
  if (result.error !== undefined) {
    throwSourceError(path, result.response, result.error);
  }
  return parseRepository(result.data);
}

export async function createGitCredential(
  options: SourceCodeHostingClientOptions & { body: CreateGitCredentialRequest },
): Promise<SourceGitCredential> {
  const client = createSourceClient(options);
  const body = removeUndefined(
    v.parse(vCreateGitCredentialRequest, options.body),
  ) as CreateGitCredentialRequestWritable;
  const path = "/api/v1/git-credentials";
  const result = await createSourceGitCredential({
    client,
    body,
    headers: idempotencyHeaders("source-git-credential"),
    responseStyle: "fields",
    throwOnError: false,
  });
  if (result.error !== undefined) {
    throwSourceError(path, result.response, result.error);
  }
  return parseGitCredential(result.data);
}

export async function getRepository(
  options: SourceCodeHostingClientOptions & { repoId: string },
): Promise<SourceRepository> {
  const client = createSourceClient(options);
  const pathParams = v.parse(vGetSourceRepositoryPath, { repo_id: options.repoId });
  const path = `/api/v1/repos/${options.repoId}`;
  const result = await getSourceRepository({
    client,
    path: pathParams,
    responseStyle: "fields",
    throwOnError: false,
  });
  if (result.error !== undefined) {
    throwSourceError(path, result.response, result.error);
  }
  return parseRepository(result.data);
}

export async function listRefs(
  options: SourceCodeHostingClientOptions & { repoId: string },
): Promise<SourceRefs> {
  const client = createSourceClient(options);
  const pathParams = v.parse(vListSourceRefsPath, { repo_id: options.repoId });
  const path = `/api/v1/repos/${options.repoId}/refs`;
  const result = await listSourceRefs({
    client,
    path: pathParams,
    responseStyle: "fields",
    throwOnError: false,
  });
  if (result.error !== undefined) {
    throwSourceError(path, result.response, result.error);
  }
  return parseRefs(result.data);
}

export async function listWorkflowRuns(
  options: SourceCodeHostingClientOptions & { repoId: string },
): Promise<SourceWorkflowRunList> {
  const client = createSourceClient(options);
  const pathParams = v.parse(vListSourceWorkflowRunsPath, { repo_id: options.repoId });
  const path = `/api/v1/repos/${options.repoId}/workflow-runs`;
  const result = await listSourceWorkflowRuns({
    client,
    path: pathParams as NonNullable<ListSourceWorkflowRunsData["path"]>,
    responseStyle: "fields",
    throwOnError: false,
  });
  if (result.error !== undefined) {
    throwSourceError(path, result.response, result.error);
  }
  return parseWorkflowRunList(result.data);
}

export async function getTree(
  options: SourceCodeHostingClientOptions & { repoId: string; ref?: string; path?: string },
): Promise<SourceTree> {
  const client = createSourceClient(options);
  const pathParams = v.parse(vGetSourceTreePath, { repo_id: options.repoId });
  const query = removeUndefined(
    v.parse(vGetSourceTreeQuery, { ref: options.ref, path: options.path }),
  ) as NonNullable<GetSourceTreeData["query"]>;
  const path = `/api/v1/repos/${options.repoId}/tree`;
  const result = await getSourceTree({
    client,
    path: pathParams,
    query,
    responseStyle: "fields",
    throwOnError: false,
  });
  if (result.error !== undefined) {
    throwSourceError(path, result.response, result.error);
  }
  return parseTree(result.data);
}

export async function getBlob(
  options: SourceCodeHostingClientOptions & { repoId: string; ref?: string; path: string },
): Promise<SourceBlob> {
  const client = createSourceClient(options);
  const pathParams = v.parse(vGetSourceBlobPath, { repo_id: options.repoId });
  const query = removeUndefined(
    v.parse(vGetSourceBlobQuery, { ref: options.ref, path: options.path }),
  ) as NonNullable<GetSourceBlobData["query"]>;
  const path = `/api/v1/repos/${options.repoId}/blob`;
  const result = await getSourceBlob({
    client,
    path: pathParams,
    query,
    responseStyle: "fields",
    throwOnError: false,
  });
  if (result.error !== undefined) {
    throwSourceError(path, result.response, result.error);
  }
  return parseBlob(result.data);
}

export async function createCheckoutGrant(
  options: SourceCodeHostingClientOptions & { repoId: string; body: CreateCheckoutGrantRequest },
): Promise<SourceCheckoutGrant> {
  const client = createSourceClient(options);
  const body = removeUndefined(
    v.parse(vCreateCheckoutGrantRequest, options.body),
  ) as CreateCheckoutGrantRequestWritable;
  const path = `/api/v1/repos/${options.repoId}/checkout-grants`;
  const result = await createSourceCheckoutGrant({
    client,
    path: { repo_id: options.repoId },
    body,
    headers: idempotencyHeaders("source-checkout"),
    responseStyle: "fields",
    throwOnError: false,
  });
  if (result.error !== undefined) {
    throwSourceError(path, result.response, result.error);
  }
  return parseCheckoutGrant(result.data);
}
