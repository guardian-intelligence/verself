import * as v from "valibot";
import { createClient, type Client } from "./__generated/governance-api/client/index.js";
import {
  createDataExport as createGeneratedDataExport,
  downloadDataExport as downloadGeneratedDataExport,
  getDataExport as getGeneratedDataExport,
  listDataExports as listGeneratedDataExports,
} from "./__generated/governance-api/index.js";
import type { CreateDataExportData } from "./__generated/governance-api/types.gen.js";
import {
  vCreateDataExportResponse,
  vDataExportJob,
  vGetDataExportResponse,
  vListDataExportsResponse,
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

const governanceExportScopes = ["identity", "billing", "sandbox", "api_activity"] as const;

export const governanceCreateExportRequestSchema = v.strictObject({
  include_logs: v.optional(v.boolean(), false),
  scopes: v.optional(v.array(v.picklist(governanceExportScopes)), [...governanceExportScopes]),
});

export const governanceAPIActivitiesQuerySchema = v.strictObject({
  activity_id: v.optional(v.pipe(v.number(), v.integer(), v.minValue(1), v.maxValue(99))),
  actor_type: v.optional(v.string()),
  actor_uid: v.optional(v.string()),
  api_operation: v.optional(v.string()),
  api_service: v.optional(v.string()),
  credential_uid: v.optional(v.string()),
  cursor: v.optional(v.string()),
  limit: v.optional(v.pipe(v.number(), v.integer(), v.minValue(1), v.maxValue(200))),
  order: v.optional(v.picklist(["asc", "desc"])),
  resource_type: v.optional(v.string()),
  resource_uid: v.optional(v.string()),
  status_code: v.optional(v.string()),
  status_id: v.optional(v.pipe(v.number(), v.integer(), v.minValue(1), v.maxValue(99))),
  trace_uid: v.optional(v.string()),
});

const vAPIActivity = v.object({
  action: v.string(),
  action_id: v.number(),
  activity_id: v.number(),
  activity_name: v.string(),
  actor_name: v.optional(v.string()),
  actor_type: v.string(),
  actor_uid: v.string(),
  api_operation: v.string(),
  api_service: v.string(),
  api_version: v.optional(v.string()),
  category_name: v.string(),
  category_uid: v.number(),
  class_name: v.string(),
  class_uid: v.number(),
  credential_uid: v.optional(v.string()),
  hmac_key_id: v.optional(v.string()),
  http_args: v.optional(v.string()),
  http_method: v.string(),
  http_request_uid: v.optional(v.string()),
  http_response_code: v.number(),
  http_route: v.string(),
  http_user_agent: v.optional(v.string()),
  metadata_uid: v.string(),
  ocsf_sha256: v.string(),
  ocsf_version: v.string(),
  org_id: v.string(),
  permission: v.string(),
  prev_hmac: v.string(),
  primary_resource_full_name: v.optional(v.string()),
  primary_resource_name: v.optional(v.string()),
  primary_resource_type: v.string(),
  primary_resource_uid: v.optional(v.string()),
  row_hmac: v.string(),
  sequence: v.string(),
  severity: v.string(),
  severity_id: v.number(),
  span_uid: v.optional(v.string()),
  src_endpoint_ip: v.optional(v.string()),
  src_endpoint_name: v.optional(v.string()),
  status: v.string(),
  status_code: v.string(),
  status_id: v.number(),
  time: v.string(),
  trace_uid: v.optional(v.string()),
  type_uid: v.number(),
});

const vAPIActivityFilters = v.object({
  activity_id: v.optional(v.number()),
  actor_type: v.optional(v.string()),
  actor_uid: v.optional(v.string()),
  api_operation: v.optional(v.string()),
  api_service: v.optional(v.string()),
  credential_uid: v.optional(v.string()),
  resource_type: v.optional(v.string()),
  resource_uid: v.optional(v.string()),
  status_code: v.optional(v.string()),
  status_id: v.optional(v.number()),
  trace_uid: v.optional(v.string()),
});

const vListAPIActivitiesResponse = v.object({
  api_activities: v.array(vAPIActivity),
  filters: v.optional(vAPIActivityFilters, {}),
  limit: v.number(),
  next_cursor: v.optional(v.string()),
});

export type GovernanceCreateExportRequest = v.InferOutput<
  typeof governanceCreateExportRequestSchema
>;
export type GovernanceCreateExportRequestInput = v.InferInput<
  typeof governanceCreateExportRequestSchema
>;
export type GovernanceAPIActivitiesQuery = v.InferOutput<typeof governanceAPIActivitiesQuerySchema>;
export type GovernanceAPIActivitiesQueryInput = v.InferInput<
  typeof governanceAPIActivitiesQuerySchema
>;

function removeUndefined<T extends Record<string, unknown>>(input: T): Record<string, unknown> {
  return Object.fromEntries(Object.entries(input).filter(([, value]) => value !== undefined));
}

function parseAPIActivity(input: unknown) {
  return v.parse(vAPIActivity, input);
}

export type GovernanceAPIActivity = ReturnType<typeof parseAPIActivity>;

function parseAPIActivities(input: unknown) {
  const parsed = v.parse(vListAPIActivitiesResponse, input);
  return {
    api_activities: parsed.api_activities.map((event) => parseAPIActivity(event)),
    filters: parsed.filters,
    limit: Number(parsed.limit),
    next_cursor: parsed.next_cursor ?? "",
  };
}

export type GovernanceAPIActivities = ReturnType<typeof parseAPIActivities>;

function parseExportJob(input: unknown) {
  const parsed = v.parse(vDataExportJob, input);
  return {
    ...parsed,
    files: parsed.files ?? [],
    scopes: parsed.scopes ?? [],
  };
}

export type GovernanceExportJob = ReturnType<typeof parseExportJob>;

function parseExportJobs(input: unknown): Array<GovernanceExportJob> {
  const parsed = v.parse(vListDataExportsResponse, input);
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

  async listAPIActivities(
    input: GovernanceAPIActivitiesQueryInput = {},
  ): Promise<GovernanceAPIActivities> {
    const parsedQuery = removeUndefined(
      v.parse(governanceAPIActivitiesQuerySchema, input),
    ) as Record<string, string | number>;
    const path = "/api/v1/governance/ocsf/api-activities";
    const url = new URL(`${this.#options.baseUrl.replace(/\/+$/, "")}${path}`);
    for (const [key, value] of Object.entries(parsedQuery)) {
      url.searchParams.set(key, String(value));
    }
    const fetcher = this.#options.fetch ?? fetch;
    const response = await fetcher(url, {
      headers: createBearerJSONHeaders(this.#options.accessToken, this.#options.traceparent),
      method: "GET",
    });
    const text = await response.text();
    if (!response.ok) {
      throw new GovernanceApiError(response.status, path, text);
    }

    return parseAPIActivities(text === "" ? {} : JSON.parse(text));
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
    const body: NonNullable<CreateDataExportData["body"]> = {
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

    return parseExportJob(v.parse(vCreateDataExportResponse, result.data).export);
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

    return parseExportJob(v.parse(vGetDataExportResponse, result.data).export);
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
      data: await artifactData(result.data as unknown as Blob | ArrayBuffer | Uint8Array),
      file_name: fileNameFromContentDisposition(
        result.response.headers.get("content-disposition"),
        parsedExportId,
      ),
    };
  }
}
