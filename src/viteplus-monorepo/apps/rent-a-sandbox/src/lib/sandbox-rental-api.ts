import * as v from "valibot";
import { createClient, type Client } from "../__generated/sandbox-rental-api/client/index.js";
import {
  cancelBillingSubscription,
  createBillingCheckout,
  createBillingPortal,
  createBillingSubscription,
  getBillingEntitlements,
  getBillingStatement,
  getExecution as getGeneratedExecution,
  getRepo as getGeneratedRepo,
  importRepo as importGeneratedRepo,
  listBillingPlans,
  listBillingSubscriptions,
  listRepos,
  rescanRepo as rescanGeneratedRepo,
  submitExecution,
} from "../__generated/sandbox-rental-api/index.js";
import {
  vBillingCancelSubscriptionResponse,
  vBillingEntitlementBucketSection,
  vBillingEntitlementProductSection,
  vBillingEntitlementSlot,
  vBillingEntitlementSourceTotal,
  vBillingEntitlementsView,
  vBillingPlan,
  vBillingStatement,
  vBillingSubscription,
  vCancelBillingSubscriptionPath,
  vCreateBillingCheckoutBody,
  vCreateBillingCheckoutResponse,
  vCreateBillingPortalBody,
  vCreateBillingPortalResponse,
  vCreateBillingSubscriptionBody,
  vCreateBillingSubscriptionResponse,
  vGetBillingStatementQuery,
  vGetExecutionPath,
  vGetRepoPath,
  vImportRepoBody,
  vListBillingPlansResponse,
  vListBillingSubscriptionsResponse,
  vListReposResponse,
  vSandboxAttemptRecord,
  vSandboxBillingWindow,
  vSandboxExecutionRecord,
  vSandboxRepoRecord,
  vSubmitExecutionBody,
  vSubmitExecutionResponse,
} from "../__generated/sandbox-rental-api/valibot.gen.js";
import {
  type BearerClientOptions,
  ServiceApiError,
  createBearerJSONHeaders,
  createIdempotencyKey,
  idempotencyHeaders,
  throwGeneratedServiceError,
} from "./service-api";

const verificationRunHeader = "X-Forge-Metal-Verification-Run";
const maxSafeInteger = BigInt(Number.MAX_SAFE_INTEGER);

export interface SandboxRentalClientOptions extends BearerClientOptions {
  verificationRunId?: string;
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

function decimalStringToSafeNumber(value: string, label: string): number {
  return toSafeNumber(BigInt(value), label);
}

function throwSandboxRentalError(
  path: string,
  response: Response | undefined,
  error: unknown,
): never {
  throwGeneratedServiceError(SandboxRentalApiError, path, response, error);
}

function createSandboxRentalClient(options: SandboxRentalClientOptions): Client {
  const headers = createBearerJSONHeaders(options.accessToken);
  if (options.verificationRunId) {
    headers.set(verificationRunHeader, options.verificationRunId);
  }

  return createClient({
    baseUrl: options.baseUrl,
    headers,
    ...(options.fetch ? { fetch: options.fetch } : {}),
  });
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

type RawEntitlementSlot = v.InferOutput<typeof vBillingEntitlementSlot>;
type RawEntitlementSourceTotal = v.InferOutput<typeof vBillingEntitlementSourceTotal>;
type RawEntitlementBucketSection = v.InferOutput<typeof vBillingEntitlementBucketSection>;
type RawEntitlementProductSection = v.InferOutput<typeof vBillingEntitlementProductSection>;

function parseEntitlementSourceTotal(input: RawEntitlementSourceTotal) {
  return {
    source: input.source,
    plan_id: input.plan_id,
    label: input.label,
    period_start_units: decimalStringToSafeNumber(
      input.period_start_units,
      "sources.period_start_units",
    ),
    available_units: decimalStringToSafeNumber(input.available_units, "sources.available_units"),
    inline_expires_at: input.inline_expires_at ?? null,
  };
}

function parseEntitlementSlot(input: RawEntitlementSlot) {
  return {
    scope_type: input.scope_type,
    product_id: input.product_id,
    product_display: input.product_display,
    bucket_id: input.bucket_id,
    bucket_display: input.bucket_display,
    sku_id: input.sku_id,
    sku_display: input.sku_display,
    coverage_label: input.coverage_label,
    period_start_units: decimalStringToSafeNumber(
      input.period_start_units,
      "slot.period_start_units",
    ),
    spent_units: decimalStringToSafeNumber(input.spent_units, "slot.spent_units"),
    pending_units: decimalStringToSafeNumber(input.pending_units, "slot.pending_units"),
    available_units: decimalStringToSafeNumber(input.available_units, "slot.available_units"),
    sources: input.sources?.map((source) => parseEntitlementSourceTotal(source)) ?? [],
  };
}

function parseEntitlementBucketSection(input: RawEntitlementBucketSection) {
  return {
    bucket_id: input.bucket_id,
    display_name: input.display_name,
    bucket_slot: input.bucket_slot ? parseEntitlementSlot(input.bucket_slot) : null,
    sku_slots: input.sku_slots?.map((slot) => parseEntitlementSlot(slot)) ?? [],
  };
}

function parseEntitlementProductSection(input: RawEntitlementProductSection) {
  return {
    product_id: input.product_id,
    display_name: input.display_name,
    product_slot: input.product_slot ? parseEntitlementSlot(input.product_slot) : null,
    buckets: input.buckets?.map((bucket) => parseEntitlementBucketSection(bucket)) ?? [],
  };
}

function parseEntitlementsView(input: unknown) {
  const parsed = v.parse(vBillingEntitlementsView, input);
  return {
    org_id: parsed.org_id,
    universal: parseEntitlementSlot(parsed.universal),
    products: parsed.products?.map((product) => parseEntitlementProductSection(product)) ?? [],
  };
}

export type EntitlementSourceTotal = ReturnType<typeof parseEntitlementSourceTotal>;
export type EntitlementSlot = ReturnType<typeof parseEntitlementSlot>;
export type EntitlementBucketSection = ReturnType<typeof parseEntitlementBucketSection>;
export type EntitlementProductSection = ReturnType<typeof parseEntitlementProductSection>;
export type EntitlementsView = ReturnType<typeof parseEntitlementsView>;

function parseSubscription(input: unknown) {
  return v.parse(vBillingSubscription, input);
}

export type Subscription = ReturnType<typeof parseSubscription>;

function parsePlan(input: unknown) {
  const plan = v.parse(vBillingPlan, input);
  return {
    ...plan,
    annual_amount_cents: decimalStringToSafeNumber(plan.annual_amount_cents, "annual_amount_cents"),
    monthly_amount_cents: decimalStringToSafeNumber(
      plan.monthly_amount_cents,
      "monthly_amount_cents",
    ),
  };
}

export type BillingPlan = ReturnType<typeof parsePlan>;

function parsePlansResponse(input: unknown) {
  const { $schema: _schema, plans } = v.parse(vListBillingPlansResponse, input);
  return {
    plans: plans?.map((plan) => parsePlan(plan)) ?? null,
  };
}

export type PlansResponse = ReturnType<typeof parsePlansResponse>;

function parseSubscriptionsResponse(input: unknown) {
  const { $schema: _schema, subscriptions } = v.parse(vListBillingSubscriptionsResponse, input);
  return {
    subscriptions: subscriptions?.map((subscription) => parseSubscription(subscription)) ?? null,
  };
}

export type SubscriptionsResponse = ReturnType<typeof parseSubscriptionsResponse>;

type RawStatement = v.InferOutput<typeof vBillingStatement>;
type RawStatementLineItem = NonNullable<RawStatement["line_items"]>[number];
type RawStatementGrantSummary = NonNullable<RawStatement["grant_summaries"]>[number];

function parseStatementLineItem(input: RawStatementLineItem) {
  return {
    ...input,
    charge_units: decimalStringToSafeNumber(input.charge_units, "line_items.charge_units"),
    unit_rate: decimalStringToSafeNumber(input.unit_rate, "line_items.unit_rate"),
    free_tier_units: decimalStringToSafeNumber(
      input.free_tier_units,
      "line_items.free_tier_units",
    ),
    subscription_units: decimalStringToSafeNumber(
      input.subscription_units,
      "line_items.subscription_units",
    ),
    purchase_units: decimalStringToSafeNumber(input.purchase_units, "line_items.purchase_units"),
    promo_units: decimalStringToSafeNumber(input.promo_units, "line_items.promo_units"),
    refund_units: decimalStringToSafeNumber(input.refund_units, "line_items.refund_units"),
    receivable_units: decimalStringToSafeNumber(
      input.receivable_units,
      "line_items.receivable_units",
    ),
    reserved_units: decimalStringToSafeNumber(input.reserved_units, "line_items.reserved_units"),
  };
}

function parseStatementGrantSummary(input: RawStatementGrantSummary) {
  return {
    ...input,
    available: decimalStringToSafeNumber(input.available, "grant_summaries.available"),
    pending: decimalStringToSafeNumber(input.pending, "grant_summaries.pending"),
  };
}

function parseStatementTotals(input: RawStatement["totals"]) {
  return {
    charge_units: decimalStringToSafeNumber(input.charge_units, "totals.charge_units"),
    free_tier_units: decimalStringToSafeNumber(input.free_tier_units, "totals.free_tier_units"),
    subscription_units: decimalStringToSafeNumber(
      input.subscription_units,
      "totals.subscription_units",
    ),
    purchase_units: decimalStringToSafeNumber(input.purchase_units, "totals.purchase_units"),
    promo_units: decimalStringToSafeNumber(input.promo_units, "totals.promo_units"),
    refund_units: decimalStringToSafeNumber(input.refund_units, "totals.refund_units"),
    receivable_units: decimalStringToSafeNumber(input.receivable_units, "totals.receivable_units"),
    reserved_units: decimalStringToSafeNumber(input.reserved_units, "totals.reserved_units"),
    total_due_units: decimalStringToSafeNumber(input.total_due_units, "totals.total_due_units"),
  };
}

function parseStatement(input: unknown) {
  const {
    $schema: _schema,
    grant_summaries,
    line_items,
    totals,
    ...statement
  } = v.parse(vBillingStatement, input);
  return {
    ...statement,
    grant_summaries: grant_summaries?.map((grant) => parseStatementGrantSummary(grant)) ?? [],
    line_items: line_items?.map((lineItem) => parseStatementLineItem(lineItem)) ?? [],
    totals: parseStatementTotals(totals),
  };
}

export type Statement = ReturnType<typeof parseStatement>;

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

export const statementQuerySchema = v.pipe(
  v.strictObject({
    productId: v.string(),
  }),
  v.transform(({ productId }) => ({
    product_id: v.parse(vGetBillingStatementQuery, { product_id: productId }).product_id,
  })),
);

export type StatementQuery = v.InferOutput<typeof statementQuerySchema>;

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

export const portalRequestSchema = vCreateBillingPortalBody;

export type PortalRequest = v.InferOutput<typeof portalRequestSchema>;

export const cancelSubscriptionRequestSchema = v.strictObject({
  subscriptionId: v.string(),
});

export type CancelSubscriptionRequest = v.InferInput<typeof cancelSubscriptionRequestSchema>;

type DirectExecutionRequestBody = {
  idempotency_key: string;
  kind: "direct";
  run_command: string;
};

export const executionRequestSchema = v.pipe(
  v.strictObject({
    idempotency_key: v.optional(v.string()),
    kind: v.optional(v.literal("direct")),
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
export type PortalSession = v.InferOutput<typeof vCreateBillingPortalResponse>;

export async function getEntitlements(
  options: SandboxRentalClientOptions,
): Promise<EntitlementsView> {
  const client = createSandboxRentalClient(options);
  const path = "/api/v1/billing/entitlements";
  const result = await getBillingEntitlements({
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseEntitlementsView(result.data);
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

export async function getPlans(options: SandboxRentalClientOptions): Promise<PlansResponse> {
  const client = createSandboxRentalClient(options);
  const path = "/api/v1/billing/plans";
  const result = await listBillingPlans({
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parsePlansResponse(result.data);
}

export async function getStatement(
  options: SandboxRentalClientOptions & { query: StatementQuery },
): Promise<Statement> {
  const client = createSandboxRentalClient(options);
  const query = v.parse(vGetBillingStatementQuery, options.query);
  const path = "/api/v1/billing/statement";
  const result = await getBillingStatement({
    client,
    query,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseStatement(result.data);
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

export async function createPortalSession(
  options: SandboxRentalClientOptions & { body: PortalRequest },
): Promise<PortalSession> {
  const client = createSandboxRentalClient(options);
  const body = v.parse(portalRequestSchema, options.body);
  const path = "/api/v1/billing/portal";
  const result = await createBillingPortal({
    body,
    client,
    headers: idempotencyHeaders("billing-portal"),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return v.parse(vCreateBillingPortalResponse, result.data);
}

export async function cancelSubscription(
  options: SandboxRentalClientOptions & { body: CancelSubscriptionRequest },
): Promise<{ subscription: Subscription }> {
  const client = createSandboxRentalClient(options);
  const body = v.parse(cancelSubscriptionRequestSchema, options.body);
  const pathParams = v.parse(vCancelBillingSubscriptionPath, {
    subscription_id: body.subscriptionId,
  });
  const path = "/api/v1/billing/subscriptions/{subscription_id}/cancel";
  const result = await cancelBillingSubscription({
    client,
    headers: idempotencyHeaders("billing-subscription-cancel"),
    path: pathParams,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  const parsed = v.parse(vBillingCancelSubscriptionResponse, result.data);
  return { subscription: parseSubscription(parsed.subscription) };
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
