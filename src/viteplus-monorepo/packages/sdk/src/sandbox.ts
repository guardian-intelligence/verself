import * as v from "valibot";
import { createClient, type Client } from "./__generated/sandbox-rental-api/client/index.js";
import {
  type GetCostsAnalyticsData,
  type GetJobsAnalyticsData,
  type GetRunnerSizingAnalyticsData,
  type ListExecutionSchedulesData,
  type ListRunsData,
  type SearchRunLogsData,
  createExecutionSchedule as createGeneratedExecutionSchedule,
  deleteCacheGeneration as deleteGeneratedCacheGeneration,
  deleteCachePath as deleteGeneratedCachePath,
  getCostsAnalytics as getGeneratedCostsAnalytics,
  getExecution as getGeneratedExecution,
  getExecutionLogs as getGeneratedExecutionLogs,
  getExecutionSchedule as getGeneratedExecutionSchedule,
  getJobsAnalytics as getGeneratedJobsAnalytics,
  getRun as getGeneratedRun,
  getRunnerSizingAnalytics as getGeneratedRunnerSizingAnalytics,
  listCacheGenerations as listGeneratedCacheGenerations,
  listCaches as listGeneratedCaches,
  listExecutionSchedules as listGeneratedExecutionSchedules,
  listRuns as listGeneratedRuns,
  pauseExecutionSchedule as pauseGeneratedExecutionSchedule,
  resumeExecutionSchedule as resumeGeneratedExecutionSchedule,
  searchRunLogs as searchGeneratedRunLogs,
} from "./__generated/sandbox-rental-api/index.js";
import {
  vCreateExecutionScheduleBody,
  vCreateExecutionScheduleResponse,
  vDeleteCacheGenerationHeaders,
  vDeleteCacheGenerationPath,
  vDeleteCacheGenerationResponse,
  vDeleteCachePathBody,
  vDeleteCachePathHeaders,
  vDeleteCachePathPath,
  vDeleteCachePathResponse,
  vGetCostsAnalyticsQuery,
  vGetCostsAnalyticsResponse,
  vGetExecutionPath,
  vGetExecutionResponse,
  vGetExecutionLogsPath,
  vGetExecutionLogsResponse,
  vGetExecutionSchedulePath,
  vGetExecutionScheduleResponse,
  vGetJobsAnalyticsQuery,
  vGetJobsAnalyticsResponse,
  vGetRunPath,
  vGetRunResponse,
  vGetRunnerSizingAnalyticsQuery,
  vGetRunnerSizingAnalyticsResponse,
  vListCacheGenerationsPath,
  vListCacheGenerationsResponse,
  vListCachesResponse,
  vListExecutionSchedulesQuery,
  vListExecutionSchedulesResponse,
  vListRunsQuery,
  vListRunsResponse,
  vPauseExecutionScheduleHeaders,
  vPauseExecutionSchedulePath,
  vPauseExecutionScheduleResponse,
  vResumeExecutionScheduleHeaders,
  vResumeExecutionSchedulePath,
  vResumeExecutionScheduleResponse,
  vSearchRunLogsQuery,
  vSearchRunLogsResponse,
  vSandboxCache,
  vSandboxAttemptRecord,
  vSandboxBillingWindow,
  vSandboxCacheGeneration,
  vSandboxExecutionRecord,
  vSandboxExecutionScheduleDispatchRecord,
  vSandboxExecutionScheduleRecord,
} from "./__generated/sandbox-rental-api/valibot.gen.js";
import {
  type BearerClientOptions,
  ServiceApiError,
  createBearerJSONHeaders,
  createIdempotencyKey,
  idempotencyHeaders,
  throwGeneratedServiceError,
} from "./service-api";

const maxSafeInteger = BigInt(Number.MAX_SAFE_INTEGER);

export type SandboxRentalClientOptions = BearerClientOptions;

export class SandboxRental {
  readonly #options: SandboxRentalClientOptions;

  constructor(options: SandboxRentalClientOptions) {
    this.#options = options;
  }

  getExecution(executionId: string): Promise<Execution> {
    return getExecution({ ...this.#options, executionId });
  }

  getExecutionLogs(executionId: string): Promise<ExecutionLogs> {
    return getExecutionLogs({ ...this.#options, executionId });
  }

  getRun(runId: string): Promise<Execution> {
    return getRun({ ...this.#options, runId });
  }

  listRuns(query?: RunListQueryInput): Promise<RunsPage> {
    return listRuns({ ...this.#options, ...(query === undefined ? {} : { query }) });
  }

  searchRunLogs(query?: RunLogSearchQueryInput): Promise<RunLogSearchPage> {
    return searchRunLogs({ ...this.#options, ...(query === undefined ? {} : { query }) });
  }

  getJobsAnalytics(query?: SandboxAnalyticsQueryInput): Promise<JobsAnalytics> {
    return getJobsAnalytics({ ...this.#options, ...(query === undefined ? {} : { query }) });
  }

  getCostsAnalytics(query?: SandboxAnalyticsQueryInput): Promise<CostsAnalytics> {
    return getCostsAnalytics({ ...this.#options, ...(query === undefined ? {} : { query }) });
  }

  getRunnerSizingAnalytics(query?: SandboxAnalyticsQueryInput): Promise<RunnerSizingAnalytics> {
    return getRunnerSizingAnalytics({
      ...this.#options,
      ...(query === undefined ? {} : { query }),
    });
  }

  listCaches(): Promise<Cache[]> {
    return listCaches(this.#options);
  }

  listCacheGenerations(cacheId: string): Promise<CacheGeneration[]> {
    return listCacheGenerations({ ...this.#options, cacheId });
  }

  deleteCacheGeneration(
    cacheGenerationId: string,
    options?: SandboxMutationOptions,
  ): Promise<CacheGeneration> {
    return deleteCacheGeneration({
      ...this.#options,
      cacheGenerationId,
      ...(options === undefined ? {} : { options }),
    });
  }

  deleteCachePath(
    cacheId: string,
    path: string,
    options?: SandboxMutationOptions,
  ): Promise<CachePathDeleteResult> {
    return deleteCachePath({
      ...this.#options,
      cacheId,
      path,
      ...(options === undefined ? {} : { options }),
    });
  }

  listExecutionSchedules(query?: ExecutionScheduleListQueryInput): Promise<ExecutionSchedules> {
    return listExecutionSchedules({
      ...this.#options,
      ...(query === undefined ? {} : { query }),
    });
  }

  createExecutionSchedule(body: ExecutionScheduleRequest): Promise<ExecutionSchedule> {
    return createExecutionSchedule({ ...this.#options, body });
  }

  getExecutionSchedule(scheduleId: string): Promise<ExecutionSchedule> {
    return getExecutionSchedule({ ...this.#options, scheduleId });
  }

  pauseSchedule(scheduleId: string, options?: SandboxMutationOptions): Promise<ExecutionSchedule> {
    return pauseSchedule({
      ...this.#options,
      scheduleId,
      ...(options === undefined ? {} : { options }),
    });
  }

  resumeSchedule(scheduleId: string, options?: SandboxMutationOptions): Promise<ExecutionSchedule> {
    return resumeSchedule({
      ...this.#options,
      scheduleId,
      ...(options === undefined ? {} : { options }),
    });
  }
}

export class SandboxRentalApiError extends ServiceApiError {
  constructor(status: number, path: string, body: string) {
    super("Sandbox rental API", status, path, body);
    this.name = "SandboxRentalApiError";
  }
}

export function isSandboxRentalApiError(error: unknown): error is SandboxRentalApiError {
  return error instanceof SandboxRentalApiError;
}

export function isSandboxRentalNotFound(error: unknown): error is SandboxRentalApiError {
  return error instanceof SandboxRentalApiError && error.status === 404;
}

function toSafeNumber(value: bigint | number, label: string): number {
  if (typeof value === "number") {
    if (!Number.isSafeInteger(value)) {
      throw new RangeError(`${label} exceeds Number.MAX_SAFE_INTEGER`);
    }
    return value;
  }
  if (value > maxSafeInteger || value < -maxSafeInteger) {
    throw new RangeError(`${label} exceeds Number.MAX_SAFE_INTEGER`);
  }
  return Number(value);
}

function toOptionalSafeNumber(
  value: bigint | number | undefined,
  label: string,
): number | undefined {
  return value === undefined ? undefined : toSafeNumber(value, label);
}

function throwSandboxRentalError(
  path: string,
  response: Response | undefined,
  error: unknown,
): never {
  throwGeneratedServiceError(SandboxRentalApiError, path, response, error);
}

function createSandboxRentalClient(options: SandboxRentalClientOptions): Client {
  return createClient({
    baseUrl: options.baseUrl,
    headers: createBearerJSONHeaders(options.accessToken, options.traceparent),
    ...(options.fetch ? { fetch: options.fetch } : {}),
  });
}

function removeUndefined<T extends Record<string, unknown>>(input: T): Record<string, unknown> {
  return Object.fromEntries(Object.entries(input).filter(([, value]) => value !== undefined));
}

function normalizeAttempt(input: v.InferOutput<typeof vSandboxAttemptRecord>) {
  return {
    ...input,
    attempt_seq: toSafeNumber(input.attempt_seq, "attempt_seq"),
    billing_job_id: toOptionalSafeNumber(input.billing_job_id, "billing_job_id"),
    duration_ms: toOptionalSafeNumber(input.duration_ms, "duration_ms"),
    exit_code: toOptionalSafeNumber(input.exit_code, "exit_code"),
    stderr_bytes: toOptionalSafeNumber(input.stderr_bytes, "stderr_bytes"),
    stdout_bytes: toOptionalSafeNumber(input.stdout_bytes, "stdout_bytes"),
    block_write_bytes: toOptionalSafeNumber(input.block_write_bytes, "block_write_bytes"),
  };
}

export type Attempt = ReturnType<typeof normalizeAttempt>;

function normalizeBillingWindow(input: v.InferOutput<typeof vSandboxBillingWindow>) {
  return {
    ...input,
    actual_quantity: toOptionalSafeNumber(input.actual_quantity, "actual_quantity"),
    reserved_quantity: toSafeNumber(input.reserved_quantity, "reserved_quantity"),
    window_seq: toSafeNumber(input.window_seq, "window_seq"),
  };
}

export type BillingWindow = ReturnType<typeof normalizeBillingWindow>;

function parseExecution(input: unknown) {
  const { billing_windows, latest_attempt, ...execution } = v.parse(vSandboxExecutionRecord, input);
  return {
    ...execution,
    billing_windows:
      billing_windows?.map((billingWindow) => normalizeBillingWindow(billingWindow)) ?? [],
    latest_attempt: normalizeAttempt(latest_attempt),
  };
}

export type Execution = ReturnType<typeof parseExecution>;

function parseExecutionLogs(input: unknown) {
  return v.parse(vGetExecutionLogsResponse, input);
}

export type ExecutionLogs = ReturnType<typeof parseExecutionLogs>;

function parseRunLogSearchPage(input: unknown) {
  const parsed = v.parse(vSearchRunLogsResponse, input);
  return {
    ...parsed,
    nextCursor: parsed.next_cursor ?? "",
    results: parsed.results ?? [],
  };
}

export type RunLogSearchPage = ReturnType<typeof parseRunLogSearchPage>;

function parseJobsAnalytics(input: unknown) {
  const parsed = v.parse(vGetJobsAnalyticsResponse, input);
  return {
    ...parsed,
    by_runner_class: parsed.by_runner_class ?? [],
    by_source: parsed.by_source ?? [],
    slowest_runs:
      parsed.slowest_runs?.map((run) => ({
        ...run,
        duration_ms: toSafeNumber(run.duration_ms, "duration_ms"),
      })) ?? [],
  };
}

export type JobsAnalytics = ReturnType<typeof parseJobsAnalytics>;

function parseCostsAnalytics(input: unknown) {
  const parsed = v.parse(vGetCostsAnalyticsResponse, input);
  return {
    ...parsed,
    by_repository: parsed.by_repository ?? [],
    by_runner_class: parsed.by_runner_class ?? [],
    by_source: parsed.by_source ?? [],
  };
}

export type CostsAnalytics = ReturnType<typeof parseCostsAnalytics>;

function parseRunnerSizingAnalytics(input: unknown) {
  const parsed = v.parse(vGetRunnerSizingAnalyticsResponse, input);
  return {
    ...parsed,
    by_runner_class: parsed.by_runner_class ?? [],
  };
}

export type RunnerSizingAnalytics = ReturnType<typeof parseRunnerSizingAnalytics>;

function parseCache(input: unknown) {
  const parsed = v.parse(vSandboxCache, input);
  return {
    ...parsed,
    generation_count: toSafeNumber(parsed.generation_count, "cache.generation_count"),
    used_bytes: toSafeNumber(parsed.used_bytes, "cache.used_bytes"),
    written_bytes: toSafeNumber(parsed.written_bytes, "cache.written_bytes"),
  };
}

export type Cache = ReturnType<typeof parseCache>;

function parseCacheGeneration(input: unknown) {
  const parsed = v.parse(vSandboxCacheGeneration, input);
  return {
    ...parsed,
    bind_paths: parsed.bind_paths ?? [],
    used_bytes: toSafeNumber(parsed.used_bytes, "cache_generation.used_bytes"),
    written_bytes: toSafeNumber(parsed.written_bytes, "cache_generation.written_bytes"),
  };
}

export type CacheGeneration = ReturnType<typeof parseCacheGeneration>;

function parseCaches(input: unknown) {
  return v.parse(vListCachesResponse, input).caches?.map((cache) => parseCache(cache)) ?? [];
}

function parseCacheGenerations(input: unknown) {
  return (
    v
      .parse(vListCacheGenerationsResponse, input)
      .generations?.map((generation) => parseCacheGeneration(generation)) ?? []
  );
}

function parseCachePathDeleteResult(input: unknown) {
  const parsed = v.parse(vDeleteCachePathResponse, input);
  return {
    ...parsed,
    generations: parsed.generations?.map((generation) => parseCacheGeneration(generation)) ?? [],
  };
}

export type CachePathDeleteResult = ReturnType<typeof parseCachePathDeleteResult>;

type RawRunsPage = v.InferOutput<typeof vListRunsResponse>;

function parseRunsFilters(input: RawRunsPage["filters"]) {
  return {
    branch: input.branch ?? "",
    repository: input.repository ?? "",
    runnerClass: input.runner_class ?? "",
    sourceKind: input.source_kind ?? "",
    status: input.status ?? "",
    workflow: input.workflow ?? "",
  };
}

function parseRunsPage(input: unknown) {
  const { filters, limit, runs, next_cursor } = v.parse(vListRunsResponse, input);
  return {
    filters: parseRunsFilters(filters),
    limit,
    nextCursor: next_cursor ?? "",
    runs: runs?.map((run) => parseExecution(run)) ?? [],
  };
}

export type RunsPage = ReturnType<typeof parseRunsPage>;

function normalizeRunListFilter(value: string | undefined): string | undefined {
  const trimmed = value?.trim();
  return trimmed ? trimmed : undefined;
}

export const runListQuerySchema = v.pipe(
  v.strictObject({
    limit: v.optional(v.pipe(v.number(), v.integer(), v.minValue(1), v.maxValue(200))),
    cursor: v.optional(v.pipe(v.string(), v.maxLength(4096))),
    sourceKind: v.optional(v.pipe(v.string(), v.maxLength(64))),
    status: v.optional(v.pipe(v.string(), v.maxLength(64))),
    repository: v.optional(v.pipe(v.string(), v.maxLength(1024))),
    workflow: v.optional(v.pipe(v.string(), v.maxLength(1024))),
    branch: v.optional(v.pipe(v.string(), v.maxLength(1024))),
    runnerClass: v.optional(v.pipe(v.string(), v.maxLength(255))),
  }),
  v.transform((query) => {
    const parsed = v.parse(vListRunsQuery, {
      limit: query.limit,
      cursor: normalizeRunListFilter(query.cursor),
      source_kind: normalizeRunListFilter(query.sourceKind),
      status: normalizeRunListFilter(query.status),
      repository: normalizeRunListFilter(query.repository),
      workflow: normalizeRunListFilter(query.workflow),
      branch: normalizeRunListFilter(query.branch),
      runner_class: normalizeRunListFilter(query.runnerClass),
    });
    return {
      ...(parsed.limit === undefined ? {} : { limit: toSafeNumber(parsed.limit, "runs.limit") }),
      ...(parsed.cursor === undefined ? {} : { cursor: parsed.cursor }),
      ...(parsed.source_kind === undefined ? {} : { sourceKind: parsed.source_kind }),
      ...(parsed.status === undefined ? {} : { status: parsed.status }),
      ...(parsed.repository === undefined ? {} : { repository: parsed.repository }),
      ...(parsed.workflow === undefined ? {} : { workflow: parsed.workflow }),
      ...(parsed.branch === undefined ? {} : { branch: parsed.branch }),
      ...(parsed.runner_class === undefined ? {} : { runnerClass: parsed.runner_class }),
    };
  }),
);

export type RunListQueryInput = v.InferInput<typeof runListQuerySchema>;
export type RunListQuery = v.InferOutput<typeof runListQuerySchema>;

export const runLogSearchQuerySchema = v.pipe(
  v.strictObject({
    limit: v.optional(v.pipe(v.number(), v.integer(), v.minValue(1), v.maxValue(500))),
    cursor: v.optional(v.pipe(v.string(), v.maxLength(4096))),
    query: v.optional(v.pipe(v.string(), v.maxLength(4096))),
    runId: v.optional(v.pipe(v.string(), v.uuid())),
    attemptId: v.optional(v.pipe(v.string(), v.uuid())),
    sourceKind: v.optional(v.pipe(v.string(), v.maxLength(64))),
    repository: v.optional(v.pipe(v.string(), v.maxLength(1024))),
    workflow: v.optional(v.pipe(v.string(), v.maxLength(1024))),
    branch: v.optional(v.pipe(v.string(), v.maxLength(1024))),
    runnerClass: v.optional(v.pipe(v.string(), v.maxLength(255))),
  }),
  v.transform((query) => {
    const parsed = v.parse(vSearchRunLogsQuery, {
      limit: query.limit,
      cursor: normalizeRunListFilter(query.cursor),
      query: normalizeRunListFilter(query.query),
      run_id: normalizeRunListFilter(query.runId),
      attempt_id: normalizeRunListFilter(query.attemptId),
      source_kind: normalizeRunListFilter(query.sourceKind),
      repository: normalizeRunListFilter(query.repository),
      workflow: normalizeRunListFilter(query.workflow),
      branch: normalizeRunListFilter(query.branch),
      runner_class: normalizeRunListFilter(query.runnerClass),
    });
    return {
      ...(parsed.limit === undefined ? {} : { limit: toSafeNumber(parsed.limit, "logs.limit") }),
      ...(parsed.cursor === undefined ? {} : { cursor: parsed.cursor }),
      ...(parsed.query === undefined ? {} : { query: parsed.query }),
      ...(parsed.run_id === undefined ? {} : { runId: parsed.run_id }),
      ...(parsed.attempt_id === undefined ? {} : { attemptId: parsed.attempt_id }),
      ...(parsed.source_kind === undefined ? {} : { sourceKind: parsed.source_kind }),
      ...(parsed.repository === undefined ? {} : { repository: parsed.repository }),
      ...(parsed.workflow === undefined ? {} : { workflow: parsed.workflow }),
      ...(parsed.branch === undefined ? {} : { branch: parsed.branch }),
      ...(parsed.runner_class === undefined ? {} : { runnerClass: parsed.runner_class }),
    };
  }),
);

export type RunLogSearchQueryInput = v.InferInput<typeof runLogSearchQuerySchema>;
export type RunLogSearchQuery = v.InferOutput<typeof runLogSearchQuerySchema>;

export const sandboxAnalyticsQuerySchema = v.strictObject({
  start: v.optional(v.pipe(v.string(), v.isoTimestamp())),
  end: v.optional(v.pipe(v.string(), v.isoTimestamp())),
});

export type SandboxAnalyticsQueryInput = v.InferInput<typeof sandboxAnalyticsQuerySchema>;

export type SandboxMutationOptions = {
  idempotencyKey?: string | undefined;
};

function toGeneratedRunListQuery(query: RunListQueryInput | undefined) {
  if (query === undefined) return undefined;
  const parsed = v.parse(runListQuerySchema, query);
  return removeUndefined({
    limit: parsed.limit,
    cursor: parsed.cursor,
    source_kind: parsed.sourceKind,
    status: parsed.status,
    repository: parsed.repository,
    workflow: parsed.workflow,
    branch: parsed.branch,
    runner_class: parsed.runnerClass,
  }) as NonNullable<ListRunsData["query"]>;
}

function toGeneratedExecutionScheduleListQuery(query: ExecutionScheduleListQueryInput | undefined) {
  if (query === undefined) return undefined;
  const parsed = v.parse(executionScheduleListQuerySchema, query);
  return removeUndefined({
    limit: parsed.limit,
    cursor: parsed.cursor,
  }) as NonNullable<ListExecutionSchedulesData["query"]>;
}

function toGeneratedRunLogSearchQuery(query: RunLogSearchQueryInput | undefined) {
  if (query === undefined) return undefined;
  const parsed = v.parse(runLogSearchQuerySchema, query);
  return removeUndefined({
    limit: parsed.limit,
    cursor: parsed.cursor,
    query: parsed.query,
    run_id: parsed.runId,
    attempt_id: parsed.attemptId,
    source_kind: parsed.sourceKind,
    repository: parsed.repository,
    workflow: parsed.workflow,
    branch: parsed.branch,
    runner_class: parsed.runnerClass,
  }) as NonNullable<SearchRunLogsData["query"]>;
}

function toGeneratedJobsAnalyticsQuery(query: SandboxAnalyticsQueryInput | undefined) {
  if (query === undefined) return undefined;
  return v.parse(
    vGetJobsAnalyticsQuery,
    v.parse(sandboxAnalyticsQuerySchema, query),
  ) as NonNullable<GetJobsAnalyticsData["query"]>;
}

function toGeneratedCostsAnalyticsQuery(query: SandboxAnalyticsQueryInput | undefined) {
  if (query === undefined) return undefined;
  return v.parse(
    vGetCostsAnalyticsQuery,
    v.parse(sandboxAnalyticsQuerySchema, query),
  ) as NonNullable<GetCostsAnalyticsData["query"]>;
}

function toGeneratedRunnerSizingAnalyticsQuery(query: SandboxAnalyticsQueryInput | undefined) {
  if (query === undefined) return undefined;
  return v.parse(
    vGetRunnerSizingAnalyticsQuery,
    v.parse(sandboxAnalyticsQuerySchema, query),
  ) as NonNullable<GetRunnerSizingAnalyticsData["query"]>;
}

type ExecutionScheduleRequestBody = {
  idempotency_key: string;
  display_name?: string;
  inputs?: Record<string, string>;
  interval_seconds: number;
  paused?: boolean;
  project_id: string;
  ref?: string;
  source_repository_id: string;
  workflow_path: string;
};

export const executionScheduleRequestSchema = v.pipe(
  v.strictObject({
    display_name: v.optional(v.pipe(v.string(), v.trim(), v.maxLength(255))),
    idempotency_key: v.optional(v.string()),
    inputs: v.optional(v.record(v.string(), v.string())),
    interval_seconds: v.pipe(v.number(), v.integer(), v.minValue(15)),
    paused: v.optional(v.boolean()),
    project_id: v.pipe(v.string(), v.uuid()),
    ref: v.optional(v.pipe(v.string(), v.trim(), v.maxLength(1024))),
    source_repository_id: v.pipe(v.string(), v.uuid()),
    workflow_path: v.pipe(v.string(), v.trim(), v.minLength(1), v.maxLength(4096)),
  }),
  v.transform((body) => {
    const providedIdempotencyKey = body.idempotency_key?.trim();
    const requestBody: ExecutionScheduleRequestBody = {
      idempotency_key: providedIdempotencyKey || createIdempotencyKey("execution-schedule"),
      interval_seconds: body.interval_seconds,
      project_id: body.project_id,
      source_repository_id: body.source_repository_id,
      workflow_path: body.workflow_path,
      ...(body.display_name ? { display_name: body.display_name } : {}),
      ...(body.inputs && Object.keys(body.inputs).length > 0 ? { inputs: body.inputs } : {}),
      ...(body.paused !== undefined ? { paused: body.paused } : {}),
      ...(body.ref ? { ref: body.ref } : {}),
    };
    v.parse(vCreateExecutionScheduleBody, requestBody);
    return requestBody;
  }),
);

export type ExecutionScheduleRequest = v.InferInput<typeof executionScheduleRequestSchema>;

export const executionIdInputSchema = v.pipe(
  v.strictObject({
    executionId: v.string(),
  }),
  v.transform(({ executionId }) => ({
    executionId: v.parse(vGetExecutionPath, { execution_id: executionId }).execution_id,
  })),
);

export type ExecutionIdInput = v.InferOutput<typeof executionIdInputSchema>;

export const runIdInputSchema = v.pipe(
  v.strictObject({
    runId: v.string(),
  }),
  v.transform(({ runId }) => ({
    runId: v.parse(vGetRunPath, { run_id: runId }).run_id,
  })),
);

export type RunIdInput = v.InferOutput<typeof runIdInputSchema>;

export const cacheIdInputSchema = v.pipe(
  v.strictObject({
    cacheId: v.string(),
  }),
  v.transform(({ cacheId }) => ({
    cacheId: v.parse(vListCacheGenerationsPath, {
      cache_id: cacheId,
    }).cache_id,
  })),
);

export type CacheIdInput = v.InferOutput<typeof cacheIdInputSchema>;

export const cacheGenerationIdInputSchema = v.pipe(
  v.strictObject({
    cacheGenerationId: v.string(),
  }),
  v.transform(({ cacheGenerationId }) => ({
    cacheGenerationId: v.parse(vDeleteCacheGenerationPath, {
      cache_generation_id: cacheGenerationId,
    }).cache_generation_id,
  })),
);

export type CacheGenerationIdInput = v.InferOutput<typeof cacheGenerationIdInputSchema>;

export const cachePathDeleteRequestSchema = v.pipe(
  v.strictObject({
    cacheId: v.string(),
    path: v.pipe(v.string(), v.trim(), v.minLength(1), v.maxLength(255)),
  }),
  v.transform(({ cacheId, path }) => ({
    cacheId: v.parse(vDeleteCachePathPath, { cache_id: cacheId }).cache_id,
    path: v.parse(vDeleteCachePathBody, { path }).path,
  })),
);

export type CachePathDeleteRequest = v.InferInput<typeof cachePathDeleteRequestSchema>;

export const executionScheduleIdInputSchema = v.pipe(
  v.strictObject({
    scheduleId: v.string(),
  }),
  v.transform(({ scheduleId }) => ({
    scheduleId: v.parse(vGetExecutionSchedulePath, { schedule_id: scheduleId }).schedule_id,
  })),
);

export type ExecutionScheduleIdInput = v.InferOutput<typeof executionScheduleIdInputSchema>;

function parseExecutionScheduleDispatch(
  input: v.InferOutput<typeof vSandboxExecutionScheduleDispatchRecord>,
) {
  return {
    ...input,
    failure_reason: input.failure_reason ?? null,
    source_workflow_run_id: input.source_workflow_run_id ?? null,
    submitted_at: input.submitted_at ?? null,
    workflow_state: input.workflow_state ?? null,
  };
}

export type ExecutionScheduleDispatch = ReturnType<typeof parseExecutionScheduleDispatch>;

function parseExecutionSchedule(input: unknown) {
  const parsed = v.parse(vSandboxExecutionScheduleRecord, input);
  return {
    ...parsed,
    display_name: parsed.display_name ?? "",
    dispatches:
      parsed.dispatches?.map((dispatch) => parseExecutionScheduleDispatch(dispatch)) ?? [],
    idempotency_key: parsed.idempotency_key ?? "",
    inputs: parsed.inputs ?? {},
    ref: parsed.ref ?? "",
  };
}

export type ExecutionSchedule = ReturnType<typeof parseExecutionSchedule>;

function parseExecutionSchedules(input: unknown) {
  const parsed = v.parse(vListExecutionSchedulesResponse, input);
  return {
    limit: parsed.limit,
    nextCursor: parsed.next_cursor ?? "",
    schedules: (parsed.schedules ?? []).map((schedule) => parseExecutionSchedule(schedule)),
  };
}

export type ExecutionSchedules = ReturnType<typeof parseExecutionSchedules>;

export const executionScheduleListQuerySchema = v.pipe(
  v.strictObject({
    limit: v.optional(v.pipe(v.number(), v.integer(), v.minValue(1), v.maxValue(500))),
    cursor: v.optional(v.pipe(v.string(), v.maxLength(4096))),
  }),
  v.transform((query) => {
    const parsed = v.parse(vListExecutionSchedulesQuery, {
      limit: query.limit,
      cursor: normalizeRunListFilter(query.cursor),
    });
    return {
      ...(parsed.limit === undefined
        ? {}
        : { limit: toSafeNumber(parsed.limit, "execution_schedules.limit") }),
      ...(parsed.cursor === undefined ? {} : { cursor: parsed.cursor }),
    };
  }),
);

export type ExecutionScheduleListQueryInput = v.InferInput<typeof executionScheduleListQuerySchema>;
export type ExecutionScheduleListQuery = v.InferOutput<typeof executionScheduleListQuerySchema>;

export async function getExecution(
  options: SandboxRentalClientOptions & { executionId: string },
): Promise<Execution> {
  const client = createSandboxRentalClient(options);
  const { executionId } = v.parse(executionIdInputSchema, { executionId: options.executionId });
  const path = `/api/v1/executions/${executionId}`;
  const result = await getGeneratedExecution({
    client,
    path: { execution_id: executionId },
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseExecution(v.parse(vGetExecutionResponse, result.data));
}

export async function getExecutionLogs(
  options: SandboxRentalClientOptions & { executionId: string },
): Promise<ExecutionLogs> {
  const client = createSandboxRentalClient(options);
  const { executionId } = v.parse(executionIdInputSchema, { executionId: options.executionId });
  const path = `/api/v1/executions/${executionId}/logs`;
  const result = await getGeneratedExecutionLogs({
    client,
    path: v.parse(vGetExecutionLogsPath, { execution_id: executionId }),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseExecutionLogs(result.data);
}

export async function getRun(
  options: SandboxRentalClientOptions & { runId: string },
): Promise<Execution> {
  const client = createSandboxRentalClient(options);
  const { runId } = v.parse(runIdInputSchema, { runId: options.runId });
  const path = `/api/v1/runs/${runId}`;
  const result = await getGeneratedRun({
    client,
    path: { run_id: runId },
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseExecution(v.parse(vGetRunResponse, result.data));
}

export async function listRuns(
  options: SandboxRentalClientOptions & { query?: RunListQueryInput },
): Promise<RunsPage> {
  const client = createSandboxRentalClient(options);
  const query = toGeneratedRunListQuery(options.query);
  const path = "/api/v1/runs";
  const result = await listGeneratedRuns({
    client,
    ...(query === undefined ? {} : { query }),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseRunsPage(result.data);
}

export async function searchRunLogs(
  options: SandboxRentalClientOptions & { query?: RunLogSearchQueryInput },
): Promise<RunLogSearchPage> {
  const client = createSandboxRentalClient(options);
  const query = toGeneratedRunLogSearchQuery(options.query);
  const path = "/api/v1/run-logs/search";
  const result = await searchGeneratedRunLogs({
    client,
    ...(query === undefined ? {} : { query }),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseRunLogSearchPage(result.data);
}

export async function getJobsAnalytics(
  options: SandboxRentalClientOptions & { query?: SandboxAnalyticsQueryInput },
): Promise<JobsAnalytics> {
  const client = createSandboxRentalClient(options);
  const query = toGeneratedJobsAnalyticsQuery(options.query);
  const path = "/api/v1/run-analytics/jobs";
  const result = await getGeneratedJobsAnalytics({
    client,
    ...(query === undefined ? {} : { query }),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseJobsAnalytics(result.data);
}

export async function getCostsAnalytics(
  options: SandboxRentalClientOptions & { query?: SandboxAnalyticsQueryInput },
): Promise<CostsAnalytics> {
  const client = createSandboxRentalClient(options);
  const query = toGeneratedCostsAnalyticsQuery(options.query);
  const path = "/api/v1/run-analytics/costs";
  const result = await getGeneratedCostsAnalytics({
    client,
    ...(query === undefined ? {} : { query }),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseCostsAnalytics(result.data);
}

export async function getRunnerSizingAnalytics(
  options: SandboxRentalClientOptions & { query?: SandboxAnalyticsQueryInput },
): Promise<RunnerSizingAnalytics> {
  const client = createSandboxRentalClient(options);
  const query = toGeneratedRunnerSizingAnalyticsQuery(options.query);
  const path = "/api/v1/run-analytics/runner-sizing";
  const result = await getGeneratedRunnerSizingAnalytics({
    client,
    ...(query === undefined ? {} : { query }),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseRunnerSizingAnalytics(result.data);
}

export async function listCaches(options: SandboxRentalClientOptions): Promise<Cache[]> {
  const client = createSandboxRentalClient(options);
  const path = "/api/v1/caches";
  const result = await listGeneratedCaches({
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseCaches(result.data);
}

export async function listCacheGenerations(
  options: SandboxRentalClientOptions & { cacheId: string },
): Promise<CacheGeneration[]> {
  const client = createSandboxRentalClient(options);
  const { cacheId } = v.parse(cacheIdInputSchema, {
    cacheId: options.cacheId,
  });
  const path = `/api/v1/caches/${cacheId}/generations`;
  const result = await listGeneratedCacheGenerations({
    client,
    path: v.parse(vListCacheGenerationsPath, { cache_id: cacheId }),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseCacheGenerations(result.data);
}

export async function deleteCacheGeneration(
  options: SandboxRentalClientOptions & {
    cacheGenerationId: string;
    options?: SandboxMutationOptions;
  },
): Promise<CacheGeneration> {
  const client = createSandboxRentalClient(options);
  const { cacheGenerationId } = v.parse(cacheGenerationIdInputSchema, {
    cacheGenerationId: options.cacheGenerationId,
  });
  const headers = v.parse(
    vDeleteCacheGenerationHeaders,
    idempotencyHeaders("cache-generation-delete", options.options?.idempotencyKey),
  );
  const path = `/api/v1/cache/generations/${cacheGenerationId}/delete`;
  const result = await deleteGeneratedCacheGeneration({
    client,
    headers,
    path: v.parse(vDeleteCacheGenerationPath, { cache_generation_id: cacheGenerationId }),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseCacheGeneration(v.parse(vDeleteCacheGenerationResponse, result.data).generation);
}

export async function deleteCachePath(
  options: SandboxRentalClientOptions & {
    cacheId: string;
    path: string;
    options?: SandboxMutationOptions;
  },
): Promise<CachePathDeleteResult> {
  const client = createSandboxRentalClient(options);
  const request = v.parse(cachePathDeleteRequestSchema, {
    cacheId: options.cacheId,
    path: options.path,
  });
  const headers = v.parse(
    vDeleteCachePathHeaders,
    idempotencyHeaders("cache-path-delete", options.options?.idempotencyKey),
  );
  const path = `/api/v1/caches/${request.cacheId}/paths/delete`;
  const result = await deleteGeneratedCachePath({
    body: { path: request.path },
    client,
    headers,
    path: v.parse(vDeleteCachePathPath, { cache_id: request.cacheId }),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseCachePathDeleteResult(result.data);
}

export async function listExecutionSchedules(
  options: SandboxRentalClientOptions & { query?: ExecutionScheduleListQueryInput },
): Promise<ExecutionSchedules> {
  const client = createSandboxRentalClient(options);
  const query = toGeneratedExecutionScheduleListQuery(options.query);
  const path = "/api/v1/execution-schedules";
  const result = await listGeneratedExecutionSchedules({
    client,
    ...(query === undefined ? {} : { query }),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseExecutionSchedules(result.data);
}

export async function createExecutionSchedule(
  options: SandboxRentalClientOptions & { body: ExecutionScheduleRequest },
): Promise<ExecutionSchedule> {
  const client = createSandboxRentalClient(options);
  const body = v.parse(executionScheduleRequestSchema, options.body);
  const path = "/api/v1/execution-schedules";
  const result = await createGeneratedExecutionSchedule({
    body,
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseExecutionSchedule(v.parse(vCreateExecutionScheduleResponse, result.data));
}

export async function getExecutionSchedule(
  options: SandboxRentalClientOptions & { scheduleId: string },
): Promise<ExecutionSchedule> {
  const client = createSandboxRentalClient(options);
  const { scheduleId } = v.parse(executionScheduleIdInputSchema, {
    scheduleId: options.scheduleId,
  });
  const path = `/api/v1/execution-schedules/${scheduleId}`;
  const result = await getGeneratedExecutionSchedule({
    client,
    path: { schedule_id: scheduleId },
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseExecutionSchedule(v.parse(vGetExecutionScheduleResponse, result.data));
}

export async function pauseSchedule(
  options: SandboxRentalClientOptions & {
    scheduleId: string;
    options?: SandboxMutationOptions;
  },
): Promise<ExecutionSchedule> {
  const client = createSandboxRentalClient(options);
  const { scheduleId } = v.parse(executionScheduleIdInputSchema, {
    scheduleId: options.scheduleId,
  });
  const headers = v.parse(
    vPauseExecutionScheduleHeaders,
    idempotencyHeaders("execution-schedule-pause", options.options?.idempotencyKey),
  );
  const path = `/api/v1/execution-schedules/${scheduleId}/pause`;
  const result = await pauseGeneratedExecutionSchedule({
    client,
    headers,
    path: v.parse(vPauseExecutionSchedulePath, { schedule_id: scheduleId }),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseExecutionSchedule(v.parse(vPauseExecutionScheduleResponse, result.data));
}

export async function resumeSchedule(
  options: SandboxRentalClientOptions & {
    scheduleId: string;
    options?: SandboxMutationOptions;
  },
): Promise<ExecutionSchedule> {
  const client = createSandboxRentalClient(options);
  const { scheduleId } = v.parse(executionScheduleIdInputSchema, {
    scheduleId: options.scheduleId,
  });
  const headers = v.parse(
    vResumeExecutionScheduleHeaders,
    idempotencyHeaders("execution-schedule-resume", options.options?.idempotencyKey),
  );
  const path = `/api/v1/execution-schedules/${scheduleId}/resume`;
  const result = await resumeGeneratedExecutionSchedule({
    client,
    headers,
    path: v.parse(vResumeExecutionSchedulePath, { schedule_id: scheduleId }),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseExecutionSchedule(v.parse(vResumeExecutionScheduleResponse, result.data));
}
