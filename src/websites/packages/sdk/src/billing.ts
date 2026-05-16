import * as v from "valibot";
import { createClient, type Client } from "./__generated/billing-api/client/index.js";
import {
  type GetBillingStatementData,
  type ListBillingDocumentsData,
  type ListBillingGrantsData,
  type ListBillingPlansData,
  cancelBillingContract,
  createBillingCheckout,
  createBillingContract,
  createBillingContractChange,
  createBillingPortal,
  getBillingEntitlements,
  getBillingStatement,
  listBillingContracts,
  listBillingDocuments,
  listBillingGrants,
  listBillingPlans,
} from "./__generated/billing-api/index.js";
import {
  vBillingContract,
  vBillingDocument,
  vBillingEntitlementBucketSection,
  vBillingEntitlementProductSection,
  vBillingEntitlementSlot,
  vBillingEntitlementSourceTotal,
  vBillingEntitlementsView,
  vBillingGrant,
  vBillingPlan,
  vBillingStatement,
  vCancelBillingContractResponse,
  vCancelBillingContractPath,
  vCreateBillingCheckoutBody,
  vCreateBillingCheckoutResponse,
  vCreateBillingContractBody,
  vCreateBillingContractChangeBody,
  vCreateBillingContractChangePath,
  vCreateBillingContractChangeResponse,
  vCreateBillingContractResponse,
  vCreateBillingPortalBody,
  vCreateBillingPortalResponse,
  vGetBillingStatementQuery,
  vListBillingContractsResponse,
  vListBillingDocumentsQuery,
  vListBillingDocumentsResponse,
  vListBillingGrantsQuery,
  vListBillingGrantsResponse,
  vListBillingPlansQuery,
  vListBillingPlansResponse,
} from "./__generated/billing-api/valibot.gen.js";
import {
  type BearerClientOptions,
  ServiceApiError,
  createBearerJSONHeaders,
  idempotencyHeaders,
  throwGeneratedServiceError,
} from "./service-api";

const maxSafeInteger = BigInt(Number.MAX_SAFE_INTEGER);

export type BillingClientOptions = BearerClientOptions;

export class Billing {
  readonly #options: BillingClientOptions;

  constructor(options: BillingClientOptions) {
    this.#options = options;
  }

  getEntitlements(): Promise<EntitlementsView> {
    return getEntitlements(this.#options);
  }

  listGrants(query?: BillingGrantsQueryInput): Promise<GrantsResponse> {
    return listGrants({ ...this.#options, ...(query === undefined ? {} : { query }) });
  }

  listDocuments(query?: BillingDocumentsQueryInput): Promise<DocumentsResponse> {
    return listDocuments({ ...this.#options, ...(query === undefined ? {} : { query }) });
  }

  getContracts(): Promise<ContractsResponse> {
    return getContracts(this.#options);
  }

  getPlans(query: BillingProductQueryInput): Promise<PlansResponse> {
    return getPlans({ ...this.#options, query });
  }

  getStatement(query: StatementQueryInput): Promise<Statement> {
    return getStatement({ ...this.#options, query });
  }

  createCheckoutSession(body: CheckoutRequest): Promise<CheckoutSession> {
    return createCheckoutSession({ ...this.#options, body });
  }

  createContractSession(body: ContractRequest): Promise<ContractSession> {
    return createContractSession({ ...this.#options, body });
  }

  createContractChangeSession(body: ContractChangeRequest): Promise<ContractChangeSession> {
    return createContractChangeSession({ ...this.#options, body });
  }

  createPortalSession(body: PortalRequest): Promise<PortalSession> {
    return createPortalSession({ ...this.#options, body });
  }

  cancelContract(body: CancelContractRequest): Promise<{ contract: Contract }> {
    return cancelContract({ ...this.#options, body });
  }
}

export class BillingApiError extends ServiceApiError {
  constructor(status: number, path: string, body: string) {
    super("Billing API", status, path, body);
    this.name = "BillingApiError";
  }
}

export function isBillingApiError(error: unknown): error is BillingApiError {
  return error instanceof BillingApiError;
}

function toSafeNumber(value: bigint, label: string): number {
  if (value > maxSafeInteger || value < -maxSafeInteger) {
    throw new RangeError(`${label} exceeds Number.MAX_SAFE_INTEGER`);
  }
  return Number(value);
}

function decimalStringToSafeNumber(value: string, label: string): number {
  return toSafeNumber(BigInt(value), label);
}

function throwBillingError(path: string, response: Response | undefined, error: unknown): never {
  throwGeneratedServiceError(BillingApiError, path, response, error);
}

function createBillingClient(options: BillingClientOptions): Client {
  return createClient({
    baseUrl: options.baseUrl,
    headers: createBearerJSONHeaders(options.accessToken, options.traceparent),
    ...(options.fetch ? { fetch: options.fetch } : {}),
  });
}

function removeUndefined<T extends Record<string, unknown>>(input: T): Record<string, unknown> {
  return Object.fromEntries(Object.entries(input).filter(([, value]) => value !== undefined));
}

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
    durable_storage_quota_bytes: parsed.durable_storage_quota_bytes,
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

function parseGrant(input: unknown) {
  const grant = v.parse(vBillingGrant, input);
  return {
    ...grant,
    available: decimalStringToSafeNumber(grant.available, "grants.available"),
    pending: decimalStringToSafeNumber(grant.pending, "grants.pending"),
  };
}

export type Grant = ReturnType<typeof parseGrant>;

function parseGrantsResponse(input: unknown) {
  const { grants } = v.parse(vListBillingGrantsResponse, input);
  return {
    grants: grants?.map((grant) => parseGrant(grant)) ?? [],
  };
}

export type GrantsResponse = ReturnType<typeof parseGrantsResponse>;

function parseDocument(input: unknown) {
  const document = v.parse(vBillingDocument, input);
  return {
    ...document,
    adjustment_units: decimalStringToSafeNumber(document.adjustment_units, "adjustment_units"),
    subtotal_units: decimalStringToSafeNumber(document.subtotal_units, "subtotal_units"),
    tax_units: decimalStringToSafeNumber(document.tax_units, "tax_units"),
    total_due_units: decimalStringToSafeNumber(document.total_due_units, "total_due_units"),
  };
}

export type BillingDocument = ReturnType<typeof parseDocument>;

function parseDocumentsResponse(input: unknown) {
  const { documents } = v.parse(vListBillingDocumentsResponse, input);
  return {
    documents: documents?.map((document) => parseDocument(document)) ?? [],
  };
}

export type DocumentsResponse = ReturnType<typeof parseDocumentsResponse>;

function parseContract(input: unknown) {
  return v.parse(vBillingContract, input);
}

export type Contract = ReturnType<typeof parseContract>;

function parseContractsResponse(input: unknown) {
  const { contracts } = v.parse(vListBillingContractsResponse, input);
  return {
    contracts: contracts?.map((contract) => parseContract(contract)) ?? [],
  };
}

export type ContractsResponse = ReturnType<typeof parseContractsResponse>;

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
  const { plans } = v.parse(vListBillingPlansResponse, input);
  return {
    plans: plans?.map((plan) => parsePlan(plan)) ?? [],
  };
}

export type PlansResponse = ReturnType<typeof parsePlansResponse>;

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
  const { grant_summaries, line_items, totals, ...statement } = v.parse(vBillingStatement, input);
  return {
    ...statement,
    grant_summaries: grant_summaries?.map((grant) => parseStatementGrantSummary(grant)) ?? [],
    line_items: line_items?.map((lineItem) => parseStatementLineItem(lineItem)) ?? [],
    totals: parseStatementTotals(totals),
  };
}

export type Statement = ReturnType<typeof parseStatement>;

export const billingProductQuerySchema = v.pipe(
  v.strictObject({
    productId: v.string(),
  }),
  v.transform(({ productId }) => ({
    product_id: v.parse(vListBillingPlansQuery, { product_id: productId }).product_id,
  })),
);

export type BillingProductQueryInput = v.InferInput<typeof billingProductQuerySchema>;
export type BillingProductQuery = v.InferOutput<typeof billingProductQuerySchema>;

export const billingGrantsQuerySchema = v.pipe(
  v.strictObject({
    active: v.optional(v.boolean()),
    productId: v.optional(v.string()),
  }),
  v.transform((query) =>
    v.parse(
      vListBillingGrantsQuery,
      removeUndefined({
        active: query.active,
        product_id: query.productId,
      }),
    ),
  ),
);

export type BillingGrantsQueryInput = v.InferInput<typeof billingGrantsQuerySchema>;
export type BillingGrantsQuery = v.InferOutput<typeof billingGrantsQuerySchema>;

export const billingDocumentsQuerySchema = v.pipe(
  v.strictObject({
    productId: v.optional(v.string()),
  }),
  v.transform((query) =>
    v.parse(
      vListBillingDocumentsQuery,
      removeUndefined({
        product_id: query.productId,
      }),
    ),
  ),
);

export type BillingDocumentsQueryInput = v.InferInput<typeof billingDocumentsQuerySchema>;
export type BillingDocumentsQuery = v.InferOutput<typeof billingDocumentsQuerySchema>;

export const statementQuerySchema = v.pipe(
  v.strictObject({
    productId: v.string(),
  }),
  v.transform(({ productId }) => ({
    product_id: v.parse(vGetBillingStatementQuery, { product_id: productId }).product_id,
  })),
);

export type StatementQueryInput = v.InferInput<typeof statementQuerySchema>;
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

export type CheckoutSession = v.InferOutput<typeof vCreateBillingCheckoutResponse>;
export type ContractSession = v.InferOutput<typeof vCreateBillingContractResponse>;
export type ContractChangeSession = v.InferOutput<typeof vCreateBillingContractChangeResponse>;
export type PortalSession = v.InferOutput<typeof vCreateBillingPortalResponse>;

export async function getEntitlements(options: BillingClientOptions): Promise<EntitlementsView> {
  const client = createBillingClient(options);
  const path = "/api/v1/entitlements";
  const result = await getBillingEntitlements({
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwBillingError(path, result.response, result.error);
  }

  return parseEntitlementsView(result.data);
}

export async function listGrants(
  options: BillingClientOptions & { query?: BillingGrantsQueryInput | undefined },
): Promise<GrantsResponse> {
  const client = createBillingClient(options);
  const query =
    options.query === undefined
      ? undefined
      : (v.parse(billingGrantsQuerySchema, options.query) as NonNullable<
          ListBillingGrantsData["query"]
        >);
  const path = "/api/v1/grants";
  const result = await listBillingGrants({
    client,
    ...(query === undefined ? {} : { query }),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwBillingError(path, result.response, result.error);
  }

  return parseGrantsResponse(result.data);
}

export async function listDocuments(
  options: BillingClientOptions & { query?: BillingDocumentsQueryInput | undefined },
): Promise<DocumentsResponse> {
  const client = createBillingClient(options);
  const query =
    options.query === undefined
      ? undefined
      : (v.parse(billingDocumentsQuerySchema, options.query) as NonNullable<
          ListBillingDocumentsData["query"]
        >);
  const path = "/api/v1/billing-documents";
  const result = await listBillingDocuments({
    client,
    ...(query === undefined ? {} : { query }),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwBillingError(path, result.response, result.error);
  }

  return parseDocumentsResponse(result.data);
}

export async function getContracts(options: BillingClientOptions): Promise<ContractsResponse> {
  const client = createBillingClient(options);
  const path = "/api/v1/contracts";
  const result = await listBillingContracts({
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwBillingError(path, result.response, result.error);
  }

  return parseContractsResponse(result.data);
}

export async function getPlans(
  options: BillingClientOptions & { query: BillingProductQueryInput },
): Promise<PlansResponse> {
  const client = createBillingClient(options);
  const query = v.parse(billingProductQuerySchema, options.query) as NonNullable<
    ListBillingPlansData["query"]
  >;
  const path = "/api/v1/plans";
  const result = await listBillingPlans({
    client,
    query,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwBillingError(path, result.response, result.error);
  }

  return parsePlansResponse(result.data);
}

export async function getStatement(
  options: BillingClientOptions & { query: StatementQueryInput },
): Promise<Statement> {
  const client = createBillingClient(options);
  const query = v.parse(statementQuerySchema, options.query) as NonNullable<
    GetBillingStatementData["query"]
  >;
  const path = "/api/v1/statement";
  const result = await getBillingStatement({
    client,
    query,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwBillingError(path, result.response, result.error);
  }

  return parseStatement(result.data);
}

export async function createCheckoutSession(
  options: BillingClientOptions & { body: CheckoutRequest },
): Promise<CheckoutSession> {
  const client = createBillingClient(options);
  const body = v.parse(checkoutRequestSchema, options.body);
  const path = "/api/v1/checkout";
  const result = await createBillingCheckout({
    body,
    client,
    headers: idempotencyHeaders("billing-checkout"),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwBillingError(path, result.response, result.error);
  }

  return v.parse(vCreateBillingCheckoutResponse, result.data);
}

export async function createContractSession(
  options: BillingClientOptions & { body: ContractRequest },
): Promise<ContractSession> {
  const client = createBillingClient(options);
  const body = v.parse(contractRequestSchema, options.body);
  const requestBody = {
    cancel_url: body.cancel_url,
    plan_id: body.plan_id,
    success_url: body.success_url,
    ...(body.cadence !== undefined ? { cadence: body.cadence } : {}),
  };
  const path = "/api/v1/contracts";
  const result = await createBillingContract({
    body: requestBody,
    client,
    headers: idempotencyHeaders("billing-contract"),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwBillingError(path, result.response, result.error);
  }

  return v.parse(vCreateBillingContractResponse, result.data);
}

export async function createContractChangeSession(
  options: BillingClientOptions & { body: ContractChangeRequest },
): Promise<ContractChangeSession> {
  const client = createBillingClient(options);
  const body = v.parse(contractChangeRequestSchema, options.body);
  const requestBody = v.parse(vCreateBillingContractChangeBody, {
    cancel_url: body.cancel_url,
    success_url: body.success_url,
    target_plan_id: body.target_plan_id,
  });
  const pathParams = v.parse(vCreateBillingContractChangePath, {
    contract_id: body.contract_id,
  });
  const path = "/api/v1/contracts/{contract_id}/changes";
  const result = await createBillingContractChange({
    body: requestBody,
    client,
    headers: idempotencyHeaders("billing-contract-change"),
    path: pathParams,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwBillingError(path, result.response, result.error);
  }

  return v.parse(vCreateBillingContractChangeResponse, result.data);
}

export async function createPortalSession(
  options: BillingClientOptions & { body: PortalRequest },
): Promise<PortalSession> {
  const client = createBillingClient(options);
  const body = v.parse(portalRequestSchema, options.body);
  const path = "/api/v1/portal";
  const result = await createBillingPortal({
    body,
    client,
    headers: idempotencyHeaders("billing-portal"),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwBillingError(path, result.response, result.error);
  }

  return v.parse(vCreateBillingPortalResponse, result.data);
}

export async function cancelContract(
  options: BillingClientOptions & { body: CancelContractRequest },
): Promise<{ contract: Contract }> {
  const client = createBillingClient(options);
  const body = v.parse(cancelContractRequestSchema, options.body);
  const pathParams = v.parse(vCancelBillingContractPath, {
    contract_id: body.contractId,
  });
  const path = "/api/v1/contracts/{contract_id}/cancel";
  const result = await cancelBillingContract({
    client,
    headers: idempotencyHeaders("billing-contract-cancel"),
    path: pathParams,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwBillingError(path, result.response, result.error);
  }

  const parsed = v.parse(vCancelBillingContractResponse, result.data);
  return { contract: parseContract(parsed.contract) };
}
