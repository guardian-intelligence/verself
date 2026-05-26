import * as v from "valibot";
import { createClient, type Client } from "./__generated/source-api/client/index.js";
import {
  type CreateSourceCheckoutGrantData,
  type CreateSourceGitCredentialData,
  type CreateSourceRepositoryData,
  type CreateSourceWorkflowRunData,
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
import type {
  CheckoutGrantSummary,
  GitCredentialSummary,
  ListSourceRefsResponse,
  ListSourceRepositoriesResponse,
  ListSourceWorkflowRunsResponse,
  RepositorySummary,
  SourceBlobView,
  TreeEntry,
  WorkflowRunSummary,
} from "./__generated/source-api/types.gen.js";
import {
  vCheckoutGrantSummary,
  vCreateSourceCheckoutGrantBody,
  vCreateSourceCheckoutGrantPath,
  vCreateSourceCheckoutGrantResponse,
  vCreateSourceGitCredentialResponse,
  vCreateSourceRepositoryBody,
  vCreateSourceRepositoryResponse,
  vCreateSourceWorkflowRunPath,
  vCreateSourceWorkflowRunResponse,
  vGetSourceBlobPath,
  vGetSourceBlobQuery,
  vGetSourceBlobResponse,
  vGetSourceRepositoryPath,
  vGetSourceRepositoryResponse,
  vGetSourceTreePath,
  vGetSourceTreeQuery,
  vGetSourceTreeResponse,
  vGetSourceWorkflowRunPath,
  vGetSourceWorkflowRunResponse,
  vGitCredentialSummary,
  vListSourceRefsResponse,
  vListSourceRefsPath,
  vListSourceRepositoriesQuery,
  vListSourceRepositoriesResponse,
  vListSourceWorkflowRunsResponse,
  vListSourceWorkflowRunsPath,
  vRepositorySummary,
  vWorkflowRunSummary,
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

function safeInteger(value: bigint | number | string): number {
  const out = Number(value);
  if (!Number.isSafeInteger(out)) {
    throw new TypeError("Source API returned an integer outside JavaScript's safe range");
  }
  return out;
}

function parseRepository(input: unknown) {
  return v.parse(vRepositorySummary, input) as RepositorySummary;
}

export type SourceRepository = RepositorySummary;

function parseRepositoryList(input: unknown) {
  const parsed = v.parse(vListSourceRepositoriesResponse, input) as ListSourceRepositoriesResponse;
  return {
    repositories: parsed.repositories?.map((repo) => parseRepository(repo)) ?? [],
  };
}

export type SourceRepositoryList = ReturnType<typeof parseRepositoryList>;

function parseRefs(input: unknown) {
  const parsed = v.parse(vListSourceRefsResponse, input) as ListSourceRefsResponse;
  return {
    refs: parsed.refs ?? [],
  };
}

export type SourceRefs = ReturnType<typeof parseRefs>;

function parseWorkflowRun(input: unknown) {
  return v.parse(vWorkflowRunSummary, input) as WorkflowRunSummary;
}

export type SourceWorkflowRun = WorkflowRunSummary;

function parseWorkflowRunList(input: unknown) {
  const parsed = v.parse(vListSourceWorkflowRunsResponse, input) as ListSourceWorkflowRunsResponse;
  return {
    workflow_runs: parsed.workflow_runs?.map((run) => parseWorkflowRun(run)) ?? [],
  };
}

export type SourceWorkflowRunList = ReturnType<typeof parseWorkflowRunList>;

function parseTree(input: unknown) {
  const parsed = v.parse(vGetSourceTreeResponse, input) as unknown as {
    entries?: Array<Omit<TreeEntry, "size"> & { size: bigint | number | string }>;
  };
  return {
    entries: parsed.entries?.map((entry) => ({ ...entry, size: safeInteger(entry.size) })) ?? [],
  };
}

export type SourceTree = ReturnType<typeof parseTree>;

function parseBlob(input: unknown) {
  const parsed = v.parse(vGetSourceBlobResponse, input) as unknown as Omit<
    SourceBlobView,
    "size"
  > & {
    size: bigint | number | string;
  };
  return { ...parsed, size: safeInteger(parsed.size) };
}

export type SourceBlob = SourceBlobView;

function parseCheckoutGrant(input: unknown) {
  return v.parse(vCheckoutGrantSummary, input) as CheckoutGrantSummary;
}

export type SourceCheckoutGrant = CheckoutGrantSummary;

function parseGitCredential(input: unknown) {
  return v.parse(vGitCredentialSummary, input) as GitCredentialSummary;
}

export type SourceGitCredential = GitCredentialSummary;

const gitCredentialLabelSchema = v.pipe(v.string(), v.minLength(1), v.maxLength(128));
const gitRefSchema = v.pipe(v.string(), v.minLength(1), v.maxLength(1024));
const gitScopeSchema = v.pipe(v.string(), v.minLength(1), v.maxLength(128));
const projectIdSchema = v.pipe(v.string(), v.uuid());
const workflowPathSchema = v.pipe(v.string(), v.minLength(1), v.maxLength(4096));

const workflowInputsSchema = v.record(
  v.pipe(v.string(), v.minLength(1), v.maxLength(128)),
  v.pipe(v.string(), v.maxLength(4096)),
);

export const createRepositoryRequestSchema = vCreateSourceRepositoryBody;
export const createCheckoutGrantRequestSchema = vCreateSourceCheckoutGrantBody;
export const createGitCredentialRequestSchema = v.object({
  label: v.optional(gitCredentialLabelSchema),
  expires_in_seconds: v.optional(
    v.pipe(v.number(), v.integer(), v.minValue(60), v.maxValue(7776000)),
  ),
  scopes: v.array(gitScopeSchema),
});
export const createWorkflowRunRequestSchema = v.object({
  project_id: projectIdSchema,
  workflow_path: workflowPathSchema,
  ref: v.optional(gitRefSchema),
  inputs: v.optional(workflowInputsSchema),
});

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
      v.parse(createRepositoryRequestSchema, body),
    ) as CreateSourceRepositoryData["body"];
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
    return parseRepository(v.parse(vCreateSourceRepositoryResponse, result.data));
  }

  async createGitCredential(
    body: CreateGitCredentialRequest,
    options: SourceMutationOptions = {},
  ): Promise<SourceGitCredential> {
    const client = createSourceClient(this.#options);
    const parsedBody = removeUndefined(
      v.parse(createGitCredentialRequestSchema, body),
    ) as CreateSourceGitCredentialData["body"];
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
    return parseGitCredential(v.parse(vCreateSourceGitCredentialResponse, result.data));
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
    return parseRepository(v.parse(vGetSourceRepositoryResponse, result.data));
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
      v.parse(createCheckoutGrantRequestSchema, body),
    ) as NonNullable<CreateSourceCheckoutGrantData["body"]>;
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
    return parseCheckoutGrant(v.parse(vCreateSourceCheckoutGrantResponse, result.data));
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
      v.parse(createWorkflowRunRequestSchema, body),
    ) as CreateSourceWorkflowRunData["body"];
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
    return parseWorkflowRun(v.parse(vCreateSourceWorkflowRunResponse, result.data));
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
    return parseWorkflowRun(v.parse(vGetSourceWorkflowRunResponse, result.data));
  }
}
