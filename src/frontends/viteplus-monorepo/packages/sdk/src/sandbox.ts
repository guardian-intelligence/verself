import * as v from "valibot";
import { createClient, type Client } from "./__generated/sandbox-rental-api/client/index.js";
import {
  type ListRunsData,
  createExecutionSchedule as createGeneratedExecutionSchedule,
  getExecution as getGeneratedExecution,
  getExecutionSchedule as getGeneratedExecutionSchedule,
  listExecutionSchedules as listGeneratedExecutionSchedules,
  listRuns as listGeneratedRuns,
  pauseExecutionSchedule as pauseGeneratedExecutionSchedule,
  resumeExecutionSchedule as resumeGeneratedExecutionSchedule,
} from "./__generated/sandbox-rental-api/index.js";
import {
  vCreateExecutionScheduleBody,
  vCreateExecutionScheduleResponse,
  vGetExecutionPath,
  vGetExecutionSchedulePath,
  vGetExecutionScheduleResponse,
  vListExecutionSchedulesResponse,
  vListRunsQuery,
  vListRunsResponse,
  vPauseExecutionScheduleHeaders,
  vPauseExecutionSchedulePath,
  vPauseExecutionScheduleResponse,
  vResumeExecutionScheduleHeaders,
  vResumeExecutionSchedulePath,
  vResumeExecutionScheduleResponse,
  vSandboxAttemptRecord,
  vSandboxBillingWindow,
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

  listRuns(query?: RunListQueryInput): Promise<RunsPage> {
    return listRuns({ ...this.#options, ...(query === undefined ? {} : { query }) });
  }

  listExecutionSchedules(): Promise<ExecutionSchedules> {
    return listExecutionSchedules(this.#options);
  }

  createExecutionSchedule(body: ExecutionScheduleRequest): Promise<ExecutionSchedule> {
    return createExecutionSchedule({ ...this.#options, body });
  }

  getExecutionSchedule(scheduleId: string): Promise<ExecutionSchedule> {
    return getExecutionSchedule({ ...this.#options, scheduleId });
  }

  pauseSchedule(scheduleId: string): Promise<ExecutionSchedule> {
    return pauseSchedule({ ...this.#options, scheduleId });
  }

  resumeSchedule(scheduleId: string): Promise<ExecutionSchedule> {
    return resumeSchedule({ ...this.#options, scheduleId });
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

function toSafeNumber(value: bigint, label: string): number {
  if (value > maxSafeInteger || value < -maxSafeInteger) {
    throw new RangeError(`${label} exceeds Number.MAX_SAFE_INTEGER`);
  }
  return Number(value);
}

function toOptionalSafeNumber(value: bigint | undefined, label: string): number | undefined {
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
    zfs_written: toOptionalSafeNumber(input.zfs_written, "zfs_written"),
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
  const {
    $schema: _schema,
    billing_windows,
    latest_attempt,
    sticky_disk_mounts,
    ...execution
  } = v.parse(vSandboxExecutionRecord, input);
  return {
    ...execution,
    billing_windows:
      billing_windows?.map((billingWindow) => normalizeBillingWindow(billingWindow)) ?? [],
    latest_attempt: normalizeAttempt(latest_attempt),
    sticky_disk_mounts: sticky_disk_mounts ?? [],
  };
}

export type Execution = ReturnType<typeof parseExecution>;

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
  const { $schema: _schema, filters, limit, runs, next_cursor } = v.parse(vListRunsResponse, input);
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
    cursor: v.optional(v.pipe(v.string(), v.maxLength(128))),
    sourceKind: v.optional(v.pipe(v.string(), v.maxLength(64))),
    status: v.optional(v.pipe(v.string(), v.maxLength(64))),
    repository: v.optional(v.pipe(v.string(), v.maxLength(255))),
    workflow: v.optional(v.pipe(v.string(), v.maxLength(255))),
    branch: v.optional(v.pipe(v.string(), v.maxLength(255))),
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
    ref: v.optional(v.pipe(v.string(), v.trim(), v.maxLength(255))),
    source_repository_id: v.pipe(v.string(), v.uuid()),
    workflow_path: v.pipe(v.string(), v.trim(), v.minLength(1), v.maxLength(512)),
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
  return (parsed ?? []).map((schedule) => parseExecutionSchedule(schedule));
}

export type ExecutionSchedules = ReturnType<typeof parseExecutionSchedules>;

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

  return parseExecution(result.data);
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

export async function listExecutionSchedules(
  options: SandboxRentalClientOptions,
): Promise<ExecutionSchedules> {
  const client = createSandboxRentalClient(options);
  const path = "/api/v1/execution-schedules";
  const result = await listGeneratedExecutionSchedules({
    client,
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
  options: SandboxRentalClientOptions & { scheduleId: string },
): Promise<ExecutionSchedule> {
  const client = createSandboxRentalClient(options);
  const { scheduleId } = v.parse(executionScheduleIdInputSchema, {
    scheduleId: options.scheduleId,
  });
  const headers = v.parse(
    vPauseExecutionScheduleHeaders,
    idempotencyHeaders("execution-schedule-pause"),
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
  options: SandboxRentalClientOptions & { scheduleId: string },
): Promise<ExecutionSchedule> {
  const client = createSandboxRentalClient(options);
  const { scheduleId } = v.parse(executionScheduleIdInputSchema, {
    scheduleId: options.scheduleId,
  });
  const headers = v.parse(
    vResumeExecutionScheduleHeaders,
    idempotencyHeaders("execution-schedule-resume"),
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
