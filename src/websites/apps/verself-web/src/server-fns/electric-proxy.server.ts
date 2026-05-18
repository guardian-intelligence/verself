import { readFile } from "node:fs/promises";
import { requireElectricOpaqueID, requireEnv, requireURLFromEnv } from "@verself/web-env";
import { type AuthenticatedAuthSnapshot } from "@verself/auth-web/isomorphic";
import { readAuthSnapshotFromCookie } from "./auth.server";

// Read-path-only Electric gateway. The browser only ever reaches a same-origin
// `/api/sync/<name>` route; the shape definition (table, columns, where) is
// fixed server-side and the org scope is taken from the authenticated cookie
// snapshot, never from client input. There is no write path: the flights
// collection is created without mutation handlers, so this proxy is a pure
// CDC tap on the console_flight_jobs read-model.

const ELECTRIC_BASE_URL = requireURLFromEnv("ELECTRIC_BASE_URL");

// Electric protocol params the client legitimately drives (long-poll / SSE
// cursor bookkeeping). Everything that defines *what* a shape is stays here.
const ELECTRIC_PROTOCOL_QUERY_PARAMS = new Set([
  "cache-buster",
  "cursor",
  "expired_handle",
  "experimental_live_sse",
  "offset",
  "handle",
  "live",
  "live_sse",
  "replica",
  "log",
  "subset__limit",
  "subset__offset",
  "subset__order_by",
  "subset__order_by_expr",
  "subset__params",
  "subset__where",
  "subset__where_expr",
]);
const ELECTRIC_SHAPE_DEFINITION_PARAMS = new Set([
  "table",
  "columns",
  "where",
  "secret",
  "api_secret",
]);
const ELECTRIC_POST_SHAPE_DEFINITION_KEYS = new Set(["table", "columns", "secret", "api_secret"]);

type ElectricShapeDefinition = {
  readonly table: string;
  readonly columns: readonly string[];
  readonly where: string;
  readonly params: readonly string[];
};

let secretPromise: Promise<string> | undefined;

export const electricShapeDefinitions = {
  // One card per in-flight workflow run; the active-status set is fixed in the
  // where clause (server-side, never a client param) and must stay in lockstep
  // with flightActiveStatuses in sandbox-rental-service.
  flights: (snapshot: AuthenticatedAuthSnapshot): ElectricShapeDefinition => {
    const orgID = requireElectricOpaqueID(requireSelectedOrgID(snapshot), "org_id");
    return {
      table: "console_flight_jobs",
      columns: [
        "provider",
        "provider_job_id",
        "org_id",
        "provider_run_id",
        "provider_run_attempt",
        "repository_full_name",
        "workflow_name",
        "job_name",
        "head_branch",
        "head_sha",
        "pr_number",
        "base_branch",
        "actor_login",
        "commit_count",
        "status",
        "predicted_baseline_ms",
        "created_at",
        "started_at",
      ],
      where: "org_id = $1 AND status IN ('queued', 'in_progress', 'waiting')",
      params: [orgID],
    };
  },
} as const;

export async function proxyElectricShape(
  request: Request,
  defineShape: (snapshot: AuthenticatedAuthSnapshot) => ElectricShapeDefinition,
): Promise<Response> {
  if (request.method !== "GET" && request.method !== "POST") {
    return plainResponse("method not allowed", 405);
  }

  let snapshot;
  try {
    snapshot = await readAuthSnapshotFromCookie(
      request.headers.get("cookie") ?? undefined,
      request.headers,
    );
  } catch (error) {
    return plainResponse(errorMessage("identity session lookup failed", error), 502);
  }

  if (!snapshot.isSignedIn) {
    return plainResponse("authentication required", 401);
  }

  let shape;
  try {
    shape = defineShape(snapshot);
  } catch (error) {
    return plainResponse(errorMessage("invalid Electric shape scope", error), 403);
  }

  const upstreamURL = new URL("/v1/shape", ELECTRIC_BASE_URL);
  const queryValidation = copyProtocolQueryParams(request, upstreamURL);
  if (queryValidation) return queryValidation;

  upstreamURL.searchParams.set("table", shape.table);
  upstreamURL.searchParams.set("columns", shape.columns.join(","));
  upstreamURL.searchParams.set("where", shape.where);
  shape.params.forEach((value, index) => {
    upstreamURL.searchParams.set(`params[${index + 1}]`, value);
  });
  try {
    upstreamURL.searchParams.set("secret", await electricSecret());
  } catch (error) {
    return plainResponse(errorMessage("Electric secret unavailable", error), 500);
  }

  const bodyResult = await electricRequestBody(request);
  if (bodyResult instanceof Response) return bodyResult;

  let upstream;
  try {
    const upstreamInit: RequestInit = {
      method: request.method,
      headers: electricRequestHeaders(request, bodyResult),
    };
    if (bodyResult !== undefined) {
      upstreamInit.body = bodyResult;
    }
    upstream = await fetch(upstreamURL, upstreamInit);
  } catch (error) {
    return plainResponse(errorMessage("Electric upstream unavailable", error), 502);
  }

  const headers = new Headers(upstream.headers);
  headers.delete("content-encoding");
  headers.delete("content-length");
  headers.set("cache-control", "private, no-store");
  setVaryCookie(headers);

  return new Response(upstream.body, {
    status: upstream.status,
    statusText: upstream.statusText,
    headers,
  });
}

function requireSelectedOrgID(snapshot: AuthenticatedAuthSnapshot): string {
  const orgID = snapshot.auth.selectedOrgId ?? snapshot.auth.orgId;
  if (!orgID) {
    throw new Error("selected organization is required");
  }
  return orgID;
}

function electricSecret(): Promise<string> {
  if (!secretPromise) {
    secretPromise = readSecret("ELECTRIC_API_SECRET", "VERSELF_CRED_ELECTRIC_API_SECRET");
  }
  return secretPromise;
}

async function readSecret(valueEnv: string, pathEnv: string): Promise<string> {
  const value = process.env[valueEnv]?.trim();
  if (value) return value;

  const path = requireEnv(pathEnv);
  const fileValue = (await readFile(path, "utf8")).trim();
  if (!fileValue) {
    throw new Error(`${pathEnv} points to an empty secret`);
  }
  return fileValue;
}

function copyProtocolQueryParams(request: Request, upstreamURL: URL): Response | undefined {
  const requestURL = new URL(request.url);
  for (const [key, value] of requestURL.searchParams) {
    if (ELECTRIC_SHAPE_DEFINITION_PARAMS.has(key) || key.startsWith("params[")) {
      return plainResponse(`client-controlled Electric shape parameter rejected: ${key}`, 400);
    }
    if (!ELECTRIC_PROTOCOL_QUERY_PARAMS.has(key)) {
      return plainResponse(`unsupported Electric protocol parameter: ${key}`, 400);
    }
    upstreamURL.searchParams.append(key, value);
  }
  return undefined;
}

async function electricRequestBody(request: Request): Promise<string | undefined | Response> {
  if (request.method !== "POST") return undefined;

  const text = await request.text();
  if (text.trim() === "") return undefined;

  const contentType = request.headers.get("content-type") ?? "";
  if (!contentType.toLowerCase().startsWith("application/json")) {
    return plainResponse("Electric subset requests must use application/json", 415);
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch {
    return plainResponse("Electric subset request body must be valid JSON", 400);
  }

  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    return plainResponse("Electric subset request body must be a JSON object", 400);
  }

  for (const key of Object.keys(parsed)) {
    if (ELECTRIC_POST_SHAPE_DEFINITION_KEYS.has(key)) {
      return plainResponse(`client-controlled Electric shape body key rejected: ${key}`, 400);
    }
  }

  return text;
}

function electricRequestHeaders(request: Request, body: string | undefined): Headers {
  const headers = new Headers();
  const accept = request.headers.get("accept");
  if (accept) headers.set("accept", accept);
  if (body !== undefined) headers.set("content-type", "application/json");
  return headers;
}

function setVaryCookie(headers: Headers): void {
  const vary = headers.get("vary");
  if (!vary) {
    headers.set("vary", "Cookie");
    return;
  }

  const values = vary.split(",").map((value) => value.trim().toLowerCase());
  if (!values.includes("cookie")) {
    headers.set("vary", `${vary}, Cookie`);
  }
}

function plainResponse(message: string, status: number): Response {
  return new Response(message, {
    status,
    headers: {
      "cache-control": "private, no-store",
      "content-type": "text/plain",
      vary: "Cookie",
    },
  });
}

function errorMessage(prefix: string, error: unknown): string {
  const message = error instanceof Error ? error.message : String(error);
  return `${prefix}: ${message}`;
}
