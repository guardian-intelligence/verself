import * as v from "valibot";
import { createClient, type Client } from "../__generated/sandbox-rental-api/client/index.js";
import {
  cancelBillingContract,
  createBillingCheckout,
  createBillingContract,
  createBillingContractChange,
  createBillingPortal,
  getBillingEntitlements,
  getBillingStatement,
  getExecution as getGeneratedExecution,
  listBillingPlans,
  listBillingContracts,
  submitExecution,
} from "../__generated/sandbox-rental-api/index.js";
import {
  vBillingCancelContractResponse,
  vBillingEntitlementBucketSection,
  vBillingEntitlementProductSection,
  vBillingEntitlementSlot,
  vBillingEntitlementSourceTotal,
  vBillingEntitlementsView,
  vBillingPlan,
  vBillingStatement,
  vBillingContract,
  vCancelBillingContractPath,
  vCreateBillingCheckoutBody,
  vCreateBillingCheckoutResponse,
  vCreateBillingContractChangeBody,
  vCreateBillingContractChangePath,
  vCreateBillingContractChangeResponse,
  vCreateBillingPortalBody,
  vCreateBillingPortalResponse,
  vCreateBillingContractBody,
  vCreateBillingContractResponse,
  vGetBillingStatementQuery,
  vGetExecutionPath,
  vListBillingPlansResponse,
  vListBillingContractsResponse,
  vSandboxAttemptRecord,
  vSandboxBillingWindow,
  vSandboxExecutionRecord,
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

const maxSafeInteger = BigInt(Number.MAX_SAFE_INTEGER);

export type SandboxRentalClientOptions = BearerClientOptions;

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
  return createClient({
    baseUrl: options.baseUrl,
    headers: createBearerJSONHeaders(options.accessToken),
    ...(options.fetch ? { fetch: options.fetch } : {}),
  });
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
    pending_units: decimalStringToSafeNumber(input.pending_units, "sources.pending_units"),
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

function parseContract(input: unknown) {
  return v.parse(vBillingContract, input);
}

export type Contract = ReturnType<typeof parseContract>;

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

function parseContractsResponse(input: unknown) {
  const { $schema: _schema, contracts } = v.parse(vListBillingContractsResponse, input);
  return {
    contracts: contracts?.map((contract) => parseContract(contract)) ?? null,
  };
}

export type ContractsResponse = ReturnType<typeof parseContractsResponse>;

type RawStatement = v.InferOutput<typeof vBillingStatement>;
type RawStatementLineItem = NonNullable<RawStatement["line_items"]>[number];
type RawStatementGrantSummary = NonNullable<RawStatement["grant_summaries"]>[number];

function parseStatementLineItem(input: RawStatementLineItem) {
  return {
    ...input,
    charge_units: decimalStringToSafeNumber(input.charge_units, "line_items.charge_units"),
    unit_rate: decimalStringToSafeNumber(input.unit_rate, "line_items.unit_rate"),
    free_tier_units: decimalStringToSafeNumber(input.free_tier_units, "line_items.free_tier_units"),
    contract_units: decimalStringToSafeNumber(input.contract_units, "line_items.contract_units"),
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
    contract_units: decimalStringToSafeNumber(input.contract_units, "totals.contract_units"),
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

export const contractRequestSchema = vCreateBillingContractBody;

export type ContractRequest = v.InferOutput<typeof contractRequestSchema>;

export const contractChangeRequestSchema = v.strictObject({
  cancel_url: v.string(),
  contract_id: v.string(),
  success_url: v.string(),
  target_plan_id: v.string(),
});

export type ContractChangeRequest = v.InferOutput<typeof contractChangeRequestSchema>;

export const portalRequestSchema = vCreateBillingPortalBody;

export type PortalRequest = v.InferOutput<typeof portalRequestSchema>;

export const cancelContractRequestSchema = v.strictObject({
  contractId: v.string(),
});

export type CancelContractRequest = v.InferInput<typeof cancelContractRequestSchema>;

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

export type SubmitExecutionResponse = v.InferOutput<typeof vSubmitExecutionResponse>;
export type CheckoutSession = v.InferOutput<typeof vCreateBillingCheckoutResponse>;
export type ContractSession = v.InferOutput<typeof vCreateBillingContractResponse>;
export type ContractChangeSession = v.InferOutput<typeof vCreateBillingContractChangeResponse>;
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

export async function getContracts(
  options: SandboxRentalClientOptions,
): Promise<ContractsResponse> {
  const client = createSandboxRentalClient(options);
  const path = "/api/v1/billing/contracts";
  const result = await listBillingContracts({
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return parseContractsResponse(result.data);
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

export async function createContractSession(
  options: SandboxRentalClientOptions & { body: ContractRequest },
): Promise<ContractSession> {
  const client = createSandboxRentalClient(options);
  const body = v.parse(contractRequestSchema, options.body);
  const requestBody = {
    cancel_url: body.cancel_url,
    plan_id: body.plan_id,
    success_url: body.success_url,
    ...(body.cadence !== undefined ? { cadence: body.cadence } : {}),
  };
  const path = "/api/v1/billing/contracts";
  const result = await createBillingContract({
    body: requestBody,
    client,
    headers: idempotencyHeaders("billing-contract"),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return v.parse(vCreateBillingContractResponse, result.data);
}

export async function createContractChangeSession(
  options: SandboxRentalClientOptions & { body: ContractChangeRequest },
): Promise<ContractChangeSession> {
  const client = createSandboxRentalClient(options);
  const body = v.parse(contractChangeRequestSchema, options.body);
  const requestBody = v.parse(vCreateBillingContractChangeBody, {
    cancel_url: body.cancel_url,
    success_url: body.success_url,
    target_plan_id: body.target_plan_id,
  });
  const pathParams = v.parse(vCreateBillingContractChangePath, {
    contract_id: body.contract_id,
  });
  const path = "/api/v1/billing/contracts/{contract_id}/changes";
  const result = await createBillingContractChange({
    body: requestBody,
    client,
    headers: idempotencyHeaders("billing-contract-change"),
    path: pathParams,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  return v.parse(vCreateBillingContractChangeResponse, result.data);
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

export async function cancelContract(
  options: SandboxRentalClientOptions & { body: CancelContractRequest },
): Promise<{ contract: Contract }> {
  const client = createSandboxRentalClient(options);
  const body = v.parse(cancelContractRequestSchema, options.body);
  const pathParams = v.parse(vCancelBillingContractPath, {
    contract_id: body.contractId,
  });
  const path = "/api/v1/billing/contracts/{contract_id}/cancel";
  const result = await cancelBillingContract({
    client,
    headers: idempotencyHeaders("billing-contract-cancel"),
    path: pathParams,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwSandboxRentalError(path, result.response, result.error);
  }

  const parsed = v.parse(vBillingCancelContractResponse, result.data);
  return { contract: parseContract(parsed.contract) };
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

