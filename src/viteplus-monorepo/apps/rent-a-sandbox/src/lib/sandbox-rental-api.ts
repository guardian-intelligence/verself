import * as v from "valibot";
import { createClient, type Client } from "../__generated/sandbox-rental-api/client/index.js";
import {
  createBillingCheckout,
  createBillingSubscription,
  getBillingBalance,
  getExecution as getGeneratedExecution,
  getRepo as getGeneratedRepo,
  importRepo as importGeneratedRepo,
  listBillingGrants,
  listBillingSubscriptions,
  listRepos,
  rescanRepo as rescanGeneratedRepo,
  submitExecution,
} from "../__generated/sandbox-rental-api/index.js";
import {
  vBillingBalance,
  vBillingGrant,
  vBillingGrants,
  vBillingSubscription,
  vCreateBillingCheckoutBody,
  vCreateBillingCheckoutResponse,
  vCreateBillingSubscriptionBody,
  vCreateBillingSubscriptionResponse,
  vGetExecutionPath,
  vGetRepoPath,
  vImportRepoBody,
  vListBillingSubscriptionsResponse,
  vListReposResponse,
  vSandboxAttemptRecord,
  vSandboxBillingWindow,
  vSandboxExecutionRecord,
  vSandboxRepoRecord,
  vSubmitExecutionBody,
  vSubmitExecutionResponse,
} from "../__generated/sandbox-rental-api/valibot.gen.js";

const verificationRunHeader = "X-Forge-Metal-Verification-Run";
const idempotencyKeyMaxLength = 128;
const maxSafeInteger = BigInt(Number.MAX_SAFE_INTEGER);
type IdempotencyHeaders = { "Idempotency-Key": string };

export interface SandboxRentalClientOptions {
  accessToken: string;
  baseUrl: string;
  fetch?: typeof fetch;
  verificationRunId?: string;
}

export class SandboxRentalApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly path: string,
    public readonly body: string,
  ) {
    super(`Sandbox rental API ${status}: ${body}`);
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

function decimalStringToSafeNumber(value: string, label: string): number {
  return toSafeNumber(BigInt(value), label);
}

function stringifyErrorBody(error: unknown): string {
  if (typeof error === "string") return error;
  if (error instanceof Error) return error.message;
  if (error && typeof error === "object") {
    const detail = "detail" in error ? error.detail : undefined;
    if (typeof detail === "string" && detail) return detail;
    const title = "title" in error ? error.title : undefined;
    if (typeof title === "string" && title) return title;
    return JSON.stringify(error);
  }
  return String(error);
}

function throwSandboxRentalError(
  path: string,
  response: Response | undefined,
  error: unknown,
): never {
  if (!response) {
    throw error instanceof Error ? error : new Error(stringifyErrorBody(error));
  }
  throw new SandboxRentalApiError(response.status, path, stringifyErrorBody(error));
}

function createSandboxRentalClient(options: SandboxRentalClientOptions): Client {
  const headers = new Headers();
  headers.set("Accept", "application/json");
  headers.set("Authorization", `Bearer ${options.accessToken}`);
  if (options.verificationRunId) {
    headers.set(verificationRunHeader, options.verificationRunId);
  }

  return createClient({
    baseUrl: options.baseUrl,
    headers,
    ...(options.fetch ? { fetch: options.fetch } : {}),
  });
}

function createIdempotencyKey(namespace: string): string {
  const suffix =
    globalThis.crypto?.randomUUID?.() ??
    `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
  return `${namespace}:${suffix}`.slice(0, idempotencyKeyMaxLength);
}

function idempotencyHeaders(namespace: string): IdempotencyHeaders {
  return { "Idempotency-Key": createIdempotencyKey(namespace) };
}

const repoScanIssueSchema = v.strictObject({
  path: v.string(),
  job_id: v.optional(v.string()),
  reason: v.string(),
  labels: v.optional(v.array(v.string())),
  details: v.optional(v.string()),
});

export type RepoScanIssue = v.InferOutput<typeof repoScanIssueSchema>;

export const repoCompatibilitySummarySchema = v.strictObject({
  mode: v.optional(v.string()),
  issues: v.optional(v.array(repoScanIssueSchema)),
});

export type RepoCompatibilitySummary = v.InferOutput<typeof repoCompatibilitySummarySchema>;

function parseCompatibilitySummary(input: unknown): RepoCompatibilitySummary | undefined {
  if (input === undefined || input === null) return undefined;
  return v.parse(repoCompatibilitySummarySchema, input);
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

function parseBalance(input: unknown) {
  const { $schema: _schema, ...balance } = v.parse(vBillingBalance, input);
  return {
    ...balance,
    credit_available: decimalStringToSafeNumber(balance.credit_available, "credit_available"),
    credit_pending: decimalStringToSafeNumber(balance.credit_pending, "credit_pending"),
    free_tier_available: decimalStringToSafeNumber(
      balance.free_tier_available,
      "free_tier_available",
    ),
    free_tier_pending: decimalStringToSafeNumber(balance.free_tier_pending, "free_tier_pending"),
    org_id: balance.org_id,
    total_available: decimalStringToSafeNumber(balance.total_available, "total_available"),
  };
}

export type Balance = ReturnType<typeof parseBalance>;

function parseSubscription(input: unknown) {
  return v.parse(vBillingSubscription, input);
}

export type Subscription = ReturnType<typeof parseSubscription>;

function parseSubscriptionsResponse(input: unknown) {
  const { $schema: _schema, subscriptions } = v.parse(vListBillingSubscriptionsResponse, input);
  return {
    subscriptions: subscriptions?.map((subscription) => parseSubscription(subscription)) ?? null,
  };
}

export type SubscriptionsResponse = ReturnType<typeof parseSubscriptionsResponse>;

function parseGrant(input: unknown) {
  const grant = v.parse(vBillingGrant, input);
  return {
    ...grant,
    available: decimalStringToSafeNumber(grant.available, "available"),
    pending: decimalStringToSafeNumber(grant.pending, "pending"),
  };
}

export type Grant = ReturnType<typeof parseGrant>;

function parseGrantsResponse(input: unknown) {
  const { $schema: _schema, grants } = v.parse(vBillingGrants, input);
  return {
    grants: grants?.map((grant) => parseGrant(grant)) ?? null,
  };
}

export type GrantsResponse = ReturnType<typeof parseGrantsResponse>;

function parseExecution(input: unknown) {
  const {
    $schema: _schema,
    billing_windows,
    latest_attempt,
    ...execution
  } = v.parse(vSandboxExecutionRecord, input);
  return {
    ...execution,
    billing_windows: billing_windows?.map((billingWindow) => normalizeBillingWindow(billingWindow)),
    latest_attempt: normalizeAttempt(latest_attempt),
  };
}

export type Execution = ReturnType<typeof parseExecution>;

function parseRepo(input: unknown) {
  const { $schema: _schema, compatibility_summary, ...repo } = v.parse(vSandboxRepoRecord, input);
  return {
    ...repo,
    compatibility_summary: parseCompatibilitySummary(compatibility_summary),
  };
}

export type Repo = ReturnType<typeof parseRepo>;

export const grantsQuerySchema = v.optional(
  v.object({
    active: v.optional(v.boolean()),
    productId: v.optional(v.string()),
  }),
  {},
);

export type GrantsQuery = v.InferOutput<typeof grantsQuerySchema>;

export const checkoutRequestSchema = v.pipe(
  v.strictObject({
    amount_cents: v.pipe(v.number(), v.minValue(1)),
    cancel_url: v.string(),
    product_id: v.string(),
    success_url: v.string(),
  }),
  v.transform((body) => {
    const parsed = v.parse(vCreateBillingCheckoutBody, body);
    return {
      ...body,
      amount_cents: toSafeNumber(parsed.amount_cents, "amount_cents"),
    };
  }),
);

export type CheckoutRequest = v.InferOutput<typeof checkoutRequestSchema>;

export const subscribeRequestSchema = vCreateBillingSubscriptionBody;

export type SubscribeRequest = v.InferOutput<typeof subscribeRequestSchema>;

type DirectExecutionRequestBody = {
  idempotency_key: string;
  kind: "direct";
  run_command: string;
};

export const executionRequestSchema = v.pipe(
  v.strictObject({
    idempotency_key: v.optional(v.string()),
    run_command: v.pipe(v.string(), v.trim(), v.minLength(1)),
  }),
  v.transform((body) => {
    const providedIdempotencyKey = body.idempotency_key?.trim();
    const requestBody: DirectExecutionRequestBody = {
      kind: "direct",
      idempotency_key: providedIdempotencyKey || createIdempotencyKey("execution"),
      run_command: body.run_command,
    };
    v.parse(vSubmitExecutionBody, requestBody);
    return requestBody;
  }),
);

export type ExecutionRequest = v.InferInput<typeof executionRequestSchema>;

export const executionIdInputSchema = v.pipe(
  v.strictObject({
    executionId: v.string(),
  }),
  v.transform(({ executionId }) => ({
    executionId: v.parse(vGetExecutionPath, { execution_id: executionId }).execution_id,
  })),
);

export type ExecutionIdInput = v.InferOutput<typeof executionIdInputSchema>;

export const repoIdInputSchema = v.pipe(
  v.strictObject({
    repoId: v.string(),
  }),
  v.transform(({ repoId }) => ({
    repoId: v.parse(vGetRepoPath, { repo_id: repoId }).repo_id,
  })),
);

export type RepoIdInput = v.InferOutput<typeof repoIdInputSchema>;

export const importRepoRequestSchema = vImportRepoBody;

export type ImportRepoRequest = v.InferOutput<typeof importRepoRequestSchema>;

export type SubmitExecutionResponse = v.InferOutput<typeof vSubmitExecutionResponse>;
export type CheckoutSession = v.InferOutput<typeof vCreateBillingCheckoutResponse>;
export type SubscriptionSession = v.InferOutput<typeof vCreateBillingSubscriptionResponse>;

export async function getBalance(options: SandboxRentalClientOptions): Promise<Balance> {
  const client = createSandboxRentalClient(options);
  const path = "/api/v1/billing/balance";
  const result = await getBillingBalance({
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseBalance(result.data);
}

export async function getSubscriptions(
  options: SandboxRentalClientOptions,
): Promise<SubscriptionsResponse> {
  const client = createSandboxRentalClient(options);
  const path = "/api/v1/billing/subscriptions";
  const result = await listBillingSubscriptions({
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseSubscriptionsResponse(result.data);
}

export async function getGrants(
  options: SandboxRentalClientOptions & { query?: GrantsQuery },
): Promise<GrantsResponse> {
  const client = createSandboxRentalClient(options);
  const query = v.parse(grantsQuerySchema, options.query);
  const path = "/api/v1/billing/grants";
  const result = await listBillingGrants(
    query === undefined
      ? {
          client,
          responseStyle: "fields",
          throwOnError: false,
        }
      : {
          client,
          query: {
            ...(query.active !== undefined ? { active: query.active } : {}),
            ...(query.productId !== undefined ? { product_id: query.productId } : {}),
          },
          responseStyle: "fields",
          throwOnError: false,
        },
  );

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseGrantsResponse(result.data);
}

export async function createCheckoutSession(
  options: SandboxRentalClientOptions & { body: CheckoutRequest },
): Promise<CheckoutSession> {
  const client = createSandboxRentalClient(options);
  const body = v.parse(checkoutRequestSchema, options.body);
  const path = "/api/v1/billing/checkout";
  const result = await createBillingCheckout({
    body,
    client,
    headers: idempotencyHeaders("billing-checkout"),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return v.parse(vCreateBillingCheckoutResponse, result.data);
}

export async function createSubscriptionSession(
  options: SandboxRentalClientOptions & { body: SubscribeRequest },
): Promise<SubscriptionSession> {
  const client = createSandboxRentalClient(options);
  const body = v.parse(subscribeRequestSchema, options.body);
  const requestBody = {
    cancel_url: body.cancel_url,
    plan_id: body.plan_id,
    success_url: body.success_url,
    ...(body.cadence !== undefined ? { cadence: body.cadence } : {}),
  };
  const path = "/api/v1/billing/subscribe";
  const result = await createBillingSubscription({
    body: requestBody,
    client,
    headers: idempotencyHeaders("billing-subscription"),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return v.parse(vCreateBillingSubscriptionResponse, result.data);
}

export async function submitDirectExecution(
  options: SandboxRentalClientOptions & { body: ExecutionRequest },
): Promise<SubmitExecutionResponse> {
  const client = createSandboxRentalClient(options);
  const body = v.parse(executionRequestSchema, options.body);
  const requestBody = {
    kind: "direct" as const,
    idempotency_key: body.idempotency_key,
    run_command: body.run_command,
  };
  const path = "/api/v1/executions";
  const result = await submitExecution({
    body: requestBody,
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return v.parse(vSubmitExecutionResponse, result.data);
}

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

export async function importRepo(
  options: SandboxRentalClientOptions & { body: ImportRepoRequest },
): Promise<Repo> {
  const client = createSandboxRentalClient(options);
  const body = v.parse(importRepoRequestSchema, options.body);
  const requestBody = {
    clone_url: body.clone_url,
    ...(body.default_branch !== undefined ? { default_branch: body.default_branch } : {}),
    ...(body.full_name !== undefined ? { full_name: body.full_name } : {}),
    ...(body.name !== undefined ? { name: body.name } : {}),
    ...(body.owner !== undefined ? { owner: body.owner } : {}),
    ...(body.provider !== undefined ? { provider: body.provider } : {}),
    ...(body.provider_repo_id !== undefined ? { provider_repo_id: body.provider_repo_id } : {}),
  };
  const path = "/api/v1/repos";
  const result = await importGeneratedRepo({
    body: requestBody,
    client,
    headers: idempotencyHeaders("repo-import"),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseRepo(result.data);
}

export async function getRepos(options: SandboxRentalClientOptions): Promise<Array<Repo>> {
  const client = createSandboxRentalClient(options);
  const path = "/api/v1/repos";
  const result = await listRepos({
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  const repos = v.parse(vListReposResponse, result.data);
  return repos?.map((repo) => parseRepo(repo)) ?? [];
}

export async function getRepo(
  options: SandboxRentalClientOptions & { repoId: string },
): Promise<Repo> {
  const client = createSandboxRentalClient(options);
  const { repoId } = v.parse(repoIdInputSchema, { repoId: options.repoId });
  const path = `/api/v1/repos/${repoId}`;
  const result = await getGeneratedRepo({
    client,
    path: { repo_id: repoId },
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseRepo(result.data);
}

export async function rescanRepo(
  options: SandboxRentalClientOptions & { repoId: string },
): Promise<Repo> {
  const client = createSandboxRentalClient(options);
  const { repoId } = v.parse(repoIdInputSchema, { repoId: options.repoId });
  const path = `/api/v1/repos/${repoId}/rescan`;
  const result = await rescanGeneratedRepo({
    client,
    headers: idempotencyHeaders("repo-rescan"),
    path: { repo_id: repoId },
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseRepo(result.data);
}
