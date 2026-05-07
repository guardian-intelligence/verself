import * as v from "valibot";
import { createClient, type Client } from "./__generated/governance-api/client/index.js";
import {
  createDataExport as createGeneratedDataExport,
  downloadDataExport as downloadGeneratedDataExport,
  getDataExport as getGeneratedDataExport,
  listAuditEvents as listGeneratedAuditEvents,
  listDataExports as listGeneratedDataExports,
} from "./__generated/governance-api/index.js";
import type {
  GovernanceCreateExportRequestWritable,
  ListAuditEventsData,
} from "./__generated/governance-api/types.gen.js";
import {
  vGovernanceAuditEvent,
  vGovernanceAuditEvents,
  vGovernanceExportJob,
  vGovernanceExportJobs,
} from "./__generated/governance-api/valibot.gen.js";
import type { BearerClientOptions } from "./service-api";
import {
  ServiceApiError,
  createBearerJSONHeaders,
  idempotencyHeaders,
  throwGeneratedServiceError,
} from "./service-api";

export type GovernanceClientOptions = BearerClientOptions;

export type GovernanceMutationOptions = {
  idempotencyKey?: string | undefined;
};

export class GovernanceApiError extends ServiceApiError {
  constructor(status: number, path: string, body: string) {
    super("Governance API", status, path, body);
    this.name = "GovernanceApiError";
  }
}

export function isGovernanceApiError(error: unknown): error is GovernanceApiError {
  return error instanceof GovernanceApiError;
}

function createGovernanceClient(options: GovernanceClientOptions): Client {
  return createClient({
    baseUrl: options.baseUrl,
    headers: createBearerJSONHeaders(options.accessToken, options.traceparent),
    ...(options.fetch ? { fetch: options.fetch } : {}),
  });
}

function throwGovernanceError(path: string, response: Response | undefined, error: unknown): never {
  throwGeneratedServiceError(GovernanceApiError, path, response, error);
}

const governanceExportScopes = ["identity", "billing", "sandbox", "audit"] as const;

export const governanceCreateExportRequestSchema = v.strictObject({
  include_logs: v.optional(v.boolean(), false),
  scopes: v.optional(v.array(v.picklist(governanceExportScopes)), [...governanceExportScopes]),
});

export const governanceAuditEventsQuerySchema = v.strictObject({
  actor_id: v.optional(v.string()),
  audit_event: v.optional(v.string()),
  credential_id: v.optional(v.string()),
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
  order: v.optional(v.picklist(["asc", "desc"])),
  result: v.optional(v.picklist(["allowed", "denied", "error"])),
  risk_level: v.optional(v.picklist(["low", "medium", "high", "critical"])),
  service_name: v.optional(v.string()),
  source_product_area: v.optional(v.string()),
  target_id: v.optional(v.string()),
  target_kind: v.optional(v.string()),
});

export type GovernanceCreateExportRequest = v.InferOutput<
  typeof governanceCreateExportRequestSchema
>;
export type GovernanceCreateExportRequestInput = v.InferInput<
  typeof governanceCreateExportRequestSchema
>;
export type GovernanceAuditEventsQuery = v.InferOutput<typeof governanceAuditEventsQuerySchema>;
export type GovernanceAuditEventsQueryInput = v.InferInput<typeof governanceAuditEventsQuerySchema>;

function removeUndefined<T extends Record<string, unknown>>(input: T): Record<string, unknown> {
  return Object.fromEntries(Object.entries(input).filter(([, value]) => value !== undefined));
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

export type GovernanceExportArtifact = {
  content_type: string;
  data: Uint8Array;
  file_name: string;
};

function fileNameFromContentDisposition(value: string | null, exportId: string): string {
  const fallback = `verself-export-${exportId}.tar.gz`;
  if (value) {
    const quoted = /filename="([^"]+)"/.exec(value)?.[1];
    if (quoted !== undefined && quoted.trim() !== "") {
      return safeFileName(quoted, fallback);
    }
    const bare = /filename=([^;]+)/.exec(value)?.[1]?.trim();
    if (bare !== undefined && bare !== "") {
      return safeFileName(bare, fallback);
    }
  }
  return fallback;
}

function safeFileName(value: string, fallback: string): string {
  const base = value.trim().split(/[\\/]/).at(-1)?.trim();
  return base && base !== "." ? base : fallback;
}

async function artifactData(input: Blob | ArrayBuffer | Uint8Array): Promise<Uint8Array> {
  if (input instanceof Uint8Array) {
    return input;
  }
  if (input instanceof ArrayBuffer) {
    return new Uint8Array(input);
  }
  return new Uint8Array(await input.arrayBuffer());
}

export class Governance {
  readonly #options: GovernanceClientOptions;

  constructor(options: GovernanceClientOptions) {
    this.#options = options;
  }

  async listAuditEvents(
    input: GovernanceAuditEventsQueryInput = {},
  ): Promise<GovernanceAuditEvents> {
    const client = createGovernanceClient(this.#options);
    const parsedQuery = removeUndefined(
      v.parse(governanceAuditEventsQuerySchema, input),
    ) as NonNullable<ListAuditEventsData["query"]>;
    const path = "/api/v1/governance/audit/events";
    const result = await listGeneratedAuditEvents({
      client,
      query: parsedQuery,
      responseStyle: "fields",
      throwOnError: false,
    });

    if (result.error !== undefined) {
      throwGovernanceError(path, result.response, result.error);
    }

    return parseAuditEvents(result.data);
  }

  async listDataExports(): Promise<Array<GovernanceExportJob>> {
    const client = createGovernanceClient(this.#options);
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

  async createDataExport(
    input: GovernanceCreateExportRequestInput,
    options: GovernanceMutationOptions = {},
  ): Promise<GovernanceExportJob> {
    const client = createGovernanceClient(this.#options);
    const parsedInput = v.parse(governanceCreateExportRequestSchema, input);
    const body: GovernanceCreateExportRequestWritable = {
      include_logs: parsedInput.include_logs,
      scopes: [...parsedInput.scopes],
    };
    const path = "/api/v1/governance/exports";
    const result = await createGeneratedDataExport({
      body,
      client,
      headers: idempotencyHeaders("governance-export-create", options.idempotencyKey),
      responseStyle: "fields",
      throwOnError: false,
    });

    if (result.error !== undefined) {
      throwGovernanceError(path, result.response, result.error);
    }

    return parseExportJob(result.data);
  }

  async getDataExport(exportId: string): Promise<GovernanceExportJob> {
    const parsedExportId = v.parse(v.pipe(v.string(), v.uuid()), exportId);
    const client = createGovernanceClient(this.#options);
    const path = `/api/v1/governance/exports/${parsedExportId}`;
    const result = await getGeneratedDataExport({
      client,
      path: { export_id: parsedExportId },
      responseStyle: "fields",
      throwOnError: false,
    });

    if (result.error !== undefined) {
      throwGovernanceError(path, result.response, result.error);
    }

    return parseExportJob(result.data);
  }

  async downloadDataExport(exportId: string): Promise<GovernanceExportArtifact> {
    const parsedExportId = v.parse(v.pipe(v.string(), v.uuid()), exportId);
    const client = createGovernanceClient(this.#options);
    const path = `/api/v1/governance/exports/${parsedExportId}/download`;
    const result = await downloadGeneratedDataExport({
      client,
      headers: { Accept: "application/gzip" },
      parseAs: "arrayBuffer",
      path: { export_id: parsedExportId },
      responseStyle: "fields",
      throwOnError: false,
    });

    if (result.error !== undefined) {
      throwGovernanceError(path, result.response, result.error);
    }

    return {
      content_type: result.response.headers.get("content-type") ?? "application/gzip",
      data: await artifactData(result.data as Blob | ArrayBuffer | Uint8Array),
      file_name: fileNameFromContentDisposition(
        result.response.headers.get("content-disposition"),
        parsedExportId,
      ),
    };
  }
}
