import * as v from "valibot";
import { createClient, type Client } from "../__generated/governance-api/client/index.js";
import {
  createDataExport as createGeneratedDataExport,
  getDataExport as getGeneratedDataExport,
  listAuditEvents as listGeneratedAuditEvents,
  listDataExports as listGeneratedDataExports,
} from "../__generated/governance-api/index.js";
import type {
  GovernanceCreateExportRequestWritable,
  ListAuditEventsData,
} from "../__generated/governance-api/types.gen.js";
import {
  vGovernanceAuditEvent,
  vGovernanceAuditEvents,
  vGovernanceExportJob,
  vGovernanceExportJobs,
} from "../__generated/governance-api/valibot.gen.js";
import {
  type BearerClientOptions,
  ServiceApiError,
  createBearerJSONHeaders,
  idempotencyHeaders,
  throwGeneratedServiceError,
} from "./service-api";

export interface GovernanceClientOptions extends BearerClientOptions {}

export class GovernanceApiError extends ServiceApiError {
  constructor(status: number, path: string, body: string) {
    super("Governance API", status, path, body);
    this.name = "GovernanceApiError";
  }
}

export function isGovernanceApiError(error: unknown): error is GovernanceApiError {
  return error instanceof GovernanceApiError;
}

function throwGovernanceError(path: string, response: Response | undefined, error: unknown): never {
  throwGeneratedServiceError(GovernanceApiError, path, response, error);
}

function createGovernanceClient(options: GovernanceClientOptions): Client {
  return createClient({
    baseUrl: options.baseUrl,
    headers: createBearerJSONHeaders(options.accessToken),
    ...(options.fetch ? { fetch: options.fetch } : {}),
  });
}

function parseAuditEvent(input: unknown) {
  return v.parse(vGovernanceAuditEvent, input);
}

export type GovernanceAuditEvent = ReturnType<typeof parseAuditEvent>;

function parseAuditEvents(input: unknown) {
  const parsed = v.parse(vGovernanceAuditEvents, input);
  return {
    events: parsed.events.map((event) => parseAuditEvent(event)),
    filters: parsed.filters,
    limit: Number(parsed.limit),
    next_cursor: parsed.next_cursor ?? "",
  };
}

export type GovernanceAuditEvents = ReturnType<typeof parseAuditEvents>;

function parseExportJob(input: unknown) {
  const parsed = v.parse(vGovernanceExportJob, input);
  return {
    ...parsed,
    files: parsed.files ?? [],
    scopes: parsed.scopes ?? [],
  };
}

export type GovernanceExportJob = ReturnType<typeof parseExportJob>;

function parseExportJobs(input: unknown): Array<GovernanceExportJob> {
  const parsed = v.parse(vGovernanceExportJobs, input);
  return parsed.exports.map((job) => parseExportJob(job));
}

const exportScopes = ["identity", "billing", "sandbox", "audit"] as const;

export const createExportRequestSchema = v.strictObject({
  include_logs: v.optional(v.boolean(), false),
  scopes: v.optional(v.array(v.picklist(exportScopes)), [...exportScopes]),
});

export type CreateExportRequest = v.InferInput<typeof createExportRequestSchema>;

export const auditEventsQuerySchema = v.strictObject({
  actor_id: v.optional(v.string()),
  audit_event: v.optional(v.string()),
  cursor: v.optional(v.string()),
  high_risk: v.optional(v.boolean()),
  limit: v.optional(v.pipe(v.number(), v.integer(), v.minValue(1), v.maxValue(200))),
  operation_id: v.optional(v.string()),
  operation_type: v.optional(
    v.picklist([
      "read",
      "write",
      "delete",
      "authn",
      "authz",
      "billing",
      "export",
      "system",
      "unknown",
    ]),
  ),
  result: v.optional(v.picklist(["allowed", "denied", "error"])),
  risk_level: v.optional(v.picklist(["low", "medium", "high", "critical"])),
  service_name: v.optional(v.string()),
  source_product_area: v.optional(v.string()),
  target_id: v.optional(v.string()),
  target_kind: v.optional(v.string()),
});

export type AuditEventsQuery = v.InferInput<typeof auditEventsQuerySchema>;

export async function listAuditEvents(
  options: GovernanceClientOptions & { query?: AuditEventsQuery },
): Promise<GovernanceAuditEvents> {
  const client = createGovernanceClient(options);
  const parsedQuery = v.parse(auditEventsQuerySchema, options.query ?? {});
  const query: NonNullable<ListAuditEventsData["query"]> = {
    ...(parsedQuery.actor_id !== undefined ? { actor_id: parsedQuery.actor_id } : {}),
    ...(parsedQuery.audit_event !== undefined ? { audit_event: parsedQuery.audit_event } : {}),
    ...(parsedQuery.cursor !== undefined ? { cursor: parsedQuery.cursor } : {}),
    ...(parsedQuery.high_risk !== undefined ? { high_risk: parsedQuery.high_risk } : {}),
    ...(parsedQuery.limit !== undefined ? { limit: parsedQuery.limit } : {}),
    ...(parsedQuery.operation_id !== undefined ? { operation_id: parsedQuery.operation_id } : {}),
    ...(parsedQuery.operation_type !== undefined
      ? { operation_type: parsedQuery.operation_type }
      : {}),
    ...(parsedQuery.result !== undefined ? { result: parsedQuery.result } : {}),
    ...(parsedQuery.risk_level !== undefined ? { risk_level: parsedQuery.risk_level } : {}),
    ...(parsedQuery.service_name !== undefined ? { service_name: parsedQuery.service_name } : {}),
    ...(parsedQuery.source_product_area !== undefined
      ? { source_product_area: parsedQuery.source_product_area }
      : {}),
    ...(parsedQuery.target_id !== undefined ? { target_id: parsedQuery.target_id } : {}),
    ...(parsedQuery.target_kind !== undefined ? { target_kind: parsedQuery.target_kind } : {}),
  };
  const path = "/api/v1/governance/audit/events";
  const result = await listGeneratedAuditEvents({
    client,
    query,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwGovernanceError(path, result.response, result.error);
  }

  return parseAuditEvents(result.data);
}

export async function listDataExports(
  options: GovernanceClientOptions,
): Promise<Array<GovernanceExportJob>> {
  const client = createGovernanceClient(options);
  const path = "/api/v1/governance/exports";
  const result = await listGeneratedDataExports({
    client,
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwGovernanceError(path, result.response, result.error);
  }

  return parseExportJobs(result.data);
}

export async function createDataExport(
  options: GovernanceClientOptions & { body: CreateExportRequest },
): Promise<GovernanceExportJob> {
  const client = createGovernanceClient(options);
  const input = v.parse(createExportRequestSchema, options.body);
  const body: GovernanceCreateExportRequestWritable = {
    include_logs: input.include_logs ?? false,
    scopes: [...(input.scopes ?? exportScopes)],
  };
  const path = "/api/v1/governance/exports";
  const result = await createGeneratedDataExport({
    body,
    client,
    headers: idempotencyHeaders("governance-export-create"),
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwGovernanceError(path, result.response, result.error);
  }

  return parseExportJob(result.data);
}

export async function getDataExport(
  options: GovernanceClientOptions & { exportId: string },
): Promise<GovernanceExportJob> {
  const client = createGovernanceClient(options);
  const path = `/api/v1/governance/exports/${options.exportId}`;
  const result = await getGeneratedDataExport({
    client,
    path: { export_id: options.exportId },
    responseStyle: "fields",
    throwOnError: false,
  });

  if (result.error !== undefined) {
    throwGovernanceError(path, result.response, result.error);
  }

  return parseExportJob(result.data);
}
