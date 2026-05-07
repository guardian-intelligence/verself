import * as v from "valibot";
import { createClient, type Client } from "./__generated/source-api/client/index.js";
import {
  type CreateCheckoutGrantRequestWritable,
  type CreateGitCredentialRequestWritable,
  type CreateRepositoryRequestWritable,
  type CreateSourceWorkflowRunData,
  type CreateWorkflowRunRequestWritable,
  type GetSourceBlobData,
  type GetSourceTreeData,
  type ListSourceRepositoriesData,
  type ListSourceWorkflowRunsData,
  createSourceCheckoutGrant,
  createSourceGitCredential,
  createSourceRepository,
  createSourceWorkflowRun,
  getSourceBlob,
  getSourceRepository,
  getSourceTree,
  getSourceWorkflowRun,
  listSourceRefs,
  listSourceRepositories,
  listSourceWorkflowRuns,
} from "./__generated/source-api/index.js";
import {
  vBlob,
  vCheckoutGrant,
  vCreateCheckoutGrantRequest,
  vCreateGitCredentialRequest,
  vCreateRepositoryRequest,
  vCreateSourceCheckoutGrantPath,
  vCreateSourceWorkflowRunPath,
  vCreateWorkflowRunRequest,
  vGetSourceBlobPath,
  vGetSourceBlobQuery,
  vGetSourceRepositoryPath,
  vGetSourceTreePath,
  vGetSourceTreeQuery,
  vGetSourceWorkflowRunPath,
  vGitCredential,
  vListSourceRefsPath,
  vListSourceRepositoriesQuery,
  vListSourceWorkflowRunsPath,
  vRefList,
  vRepository,
  vRepositoryList,
  vTree,
  vWorkflowRun,
  vWorkflowRunList,
} from "./__generated/source-api/valibot.gen.js";
import type { BearerClientOptions } from "./service-api";
import {
  ServiceApiError,
  createBearerJSONHeaders,
  idempotencyHeaders,
  throwGeneratedServiceError,
} from "./service-api";

export type SourceClientOptions = BearerClientOptions;

export type SourceMutationOptions = {
  idempotencyKey?: string | undefined;
};

export class SourceApiError extends ServiceApiError {
  constructor(status: number, path: string, body: string) {
    super("Source API", status, path, body);
    this.name = "SourceApiError";
  }
}

export function isSourceApiError(error: unknown): error is SourceApiError {
  return error instanceof SourceApiError;
}

function createSourceClient(options: SourceClientOptions): Client {
  return createClient({
    baseUrl: options.baseUrl,
    headers: createBearerJSONHeaders(options.accessToken, options.traceparent),
    ...(options.fetch ? { fetch: options.fetch } : {}),
  });
}

function throwSourceError(path: string, response: Response | undefined, error: unknown): never {
  throwGeneratedServiceError(SourceApiError, path, response, error);
}

function removeUndefined<T extends Record<string, unknown>>(input: T): Record<string, unknown> {
  return Object.fromEntries(Object.entries(input).filter(([, value]) => value !== undefined));
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
export const createWorkflowRunRequestSchema = vCreateWorkflowRunRequest;

export type CreateRepositoryRequest = v.InferOutput<typeof createRepositoryRequestSchema>;
export type CreateCheckoutGrantRequest = v.InferOutput<typeof createCheckoutGrantRequestSchema>;
export type CreateGitCredentialRequest = v.InferOutput<typeof createGitCredentialRequestSchema>;
export type CreateWorkflowRunRequest = v.InferOutput<typeof createWorkflowRunRequestSchema>;

export type ListRepositoriesInput = {
  projectId?: string | undefined;
};

export type SourceTreeInput = {
  repoId: string;
  ref?: string | undefined;
  path?: string | undefined;
};

export type SourceBlobInput = {
  repoId: string;
  ref?: string | undefined;
  path: string;
};

export class Source {
  readonly #options: SourceClientOptions;

  constructor(options: SourceClientOptions) {
    this.#options = options;
  }

  async listRepositories(input: ListRepositoriesInput = {}): Promise<SourceRepositoryList> {
    const client = createSourceClient(this.#options);
    const query =
      input.projectId === undefined
        ? undefined
        : v.parse(vListSourceRepositoriesQuery, { project_id: input.projectId });
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

  async createRepository(
    body: CreateRepositoryRequest,
    options: SourceMutationOptions = {},
  ): Promise<SourceRepository> {
    const client = createSourceClient(this.#options);
    const parsedBody = removeUndefined(
      v.parse(vCreateRepositoryRequest, body),
    ) as CreateRepositoryRequestWritable;
    const path = "/api/v1/repos";
    const result = await createSourceRepository({
      client,
      body: parsedBody,
      headers: idempotencyHeaders("source-repository", options.idempotencyKey),
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwSourceError(path, result.response, result.error);
    }
    return parseRepository(result.data);
  }

  async createGitCredential(
    body: CreateGitCredentialRequest,
    options: SourceMutationOptions = {},
  ): Promise<SourceGitCredential> {
    const client = createSourceClient(this.#options);
    const parsedBody = removeUndefined(
      v.parse(vCreateGitCredentialRequest, body),
    ) as CreateGitCredentialRequestWritable;
    const path = "/api/v1/git-credentials";
    const result = await createSourceGitCredential({
      client,
      body: parsedBody,
      headers: idempotencyHeaders("source-git-credential", options.idempotencyKey),
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwSourceError(path, result.response, result.error);
    }
    return parseGitCredential(result.data);
  }

  async getRepository(repoId: string): Promise<SourceRepository> {
    const client = createSourceClient(this.#options);
    const pathParams = v.parse(vGetSourceRepositoryPath, { repo_id: repoId });
    const path = `/api/v1/repos/${repoId}`;
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

  async listRefs(repoId: string): Promise<SourceRefs> {
    const client = createSourceClient(this.#options);
    const pathParams = v.parse(vListSourceRefsPath, { repo_id: repoId });
    const path = `/api/v1/repos/${repoId}/refs`;
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

  async getTree(input: SourceTreeInput): Promise<SourceTree> {
    const client = createSourceClient(this.#options);
    const pathParams = v.parse(vGetSourceTreePath, { repo_id: input.repoId });
    const query = removeUndefined(
      v.parse(vGetSourceTreeQuery, { ref: input.ref, path: input.path }),
    ) as NonNullable<GetSourceTreeData["query"]>;
    const path = `/api/v1/repos/${input.repoId}/tree`;
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

  async getBlob(input: SourceBlobInput): Promise<SourceBlob> {
    const client = createSourceClient(this.#options);
    const pathParams = v.parse(vGetSourceBlobPath, { repo_id: input.repoId });
    const query = removeUndefined(
      v.parse(vGetSourceBlobQuery, { ref: input.ref, path: input.path }),
    ) as NonNullable<GetSourceBlobData["query"]>;
    const path = `/api/v1/repos/${input.repoId}/blob`;
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

  async createCheckoutGrant(
    repoId: string,
    body: CreateCheckoutGrantRequest,
    options: SourceMutationOptions = {},
  ): Promise<SourceCheckoutGrant> {
    const client = createSourceClient(this.#options);
    const pathParams = v.parse(vCreateSourceCheckoutGrantPath, { repo_id: repoId });
    const parsedBody = removeUndefined(
      v.parse(vCreateCheckoutGrantRequest, body),
    ) as CreateCheckoutGrantRequestWritable;
    const path = `/api/v1/repos/${repoId}/checkout-grants`;
    const result = await createSourceCheckoutGrant({
      client,
      path: pathParams,
      body: parsedBody,
      headers: idempotencyHeaders("source-checkout", options.idempotencyKey),
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwSourceError(path, result.response, result.error);
    }
    return parseCheckoutGrant(result.data);
  }

  async listWorkflowRuns(repoId: string): Promise<SourceWorkflowRunList> {
    const client = createSourceClient(this.#options);
    const pathParams = v.parse(vListSourceWorkflowRunsPath, { repo_id: repoId });
    const path = `/api/v1/repos/${repoId}/workflow-runs`;
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

  async createWorkflowRun(
    repoId: string,
    body: CreateWorkflowRunRequest,
    options: SourceMutationOptions = {},
  ): Promise<SourceWorkflowRun> {
    const client = createSourceClient(this.#options);
    const pathParams = v.parse(vCreateSourceWorkflowRunPath, { repo_id: repoId });
    const parsedBody = removeUndefined(
      v.parse(vCreateWorkflowRunRequest, body),
    ) as CreateWorkflowRunRequestWritable;
    const path = `/api/v1/repos/${repoId}/workflow-runs`;
    const result = await createSourceWorkflowRun({
      client,
      path: pathParams as NonNullable<CreateSourceWorkflowRunData["path"]>,
      body: parsedBody,
      headers: idempotencyHeaders("source-workflow", options.idempotencyKey),
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwSourceError(path, result.response, result.error);
    }
    return parseWorkflowRun(result.data);
  }

  async getWorkflowRun(workflowRunId: string): Promise<SourceWorkflowRun> {
    const client = createSourceClient(this.#options);
    const pathParams = v.parse(vGetSourceWorkflowRunPath, { workflow_run_id: workflowRunId });
    const path = `/api/v1/workflow-runs/${workflowRunId}`;
    const result = await getSourceWorkflowRun({
      client,
      path: pathParams,
      responseStyle: "fields",
      throwOnError: false,
    });
    if (result.error !== undefined) {
      throwSourceError(path, result.response, result.error);
    }
    return parseWorkflowRun(result.data);
  }
}
