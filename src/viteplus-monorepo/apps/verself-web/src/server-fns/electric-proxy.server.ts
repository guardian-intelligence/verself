import { readFile } from "node:fs/promises";
import {
  requireDecimalID,
  requireElectricOpaqueID,
  requireEnv,
  requireURLFromEnv,
  requireUUID,
} from "@verself/web-env";
import { type AuthenticatedAuthSnapshot } from "@verself/auth-web/isomorphic";
import { readAuthSnapshotFromCookie } from "./auth.server";

const ELECTRIC_BASE_URL = requireURLFromEnv("ELECTRIC_BASE_URL");
const ELECTRIC_NOTIFICATIONS_BASE_URL = requireURLFromEnv("ELECTRIC_NOTIFICATIONS_BASE_URL");

const ELECTRIC_PROTOCOL_QUERY_PARAMS = new Set([
  "offset",
  "handle",
  "live",
  "live_sse",
  "replica",
  "log",
]);
const ELECTRIC_SHAPE_DEFINITION_PARAMS = new Set([
  "table",
  "columns",
  "where",
  "secret",
  "api_secret",
]);
const ELECTRIC_POST_SHAPE_DEFINITION_KEYS = new Set(["table", "columns", "secret", "api_secret"]);

type ElectricStream = "sandbox" | "notifications";

type ElectricShapeDefinition = {
  readonly stream: ElectricStream;
  readonly table: string;
  readonly columns: readonly string[];
  readonly where: string;
  readonly params: readonly string[];
};

const secretCache = new Map<string, Promise<string>>();

export const electricShapeDefinitions = {
  executions: (snapshot: AuthenticatedAuthSnapshot): ElectricShapeDefinition => {
    const orgID = requireDecimalID(requireSelectedOrgID(snapshot), "org_id");
    return {
      stream: "sandbox",
      table: "executions",
      columns: ["execution_id", "source_ref", "state", "created_at"],
      where: "org_id = $1",
      params: [orgID],
    };
  },
  runnerProviderRepositories: (snapshot: AuthenticatedAuthSnapshot): ElectricShapeDefinition => {
    const orgID = requireDecimalID(requireSelectedOrgID(snapshot), "org_id");
    return {
      stream: "sandbox",
      table: "runner_provider_repositories",
      columns: [
        "provider",
        "provider_repository_id",
        "source_repository_id",
        "repository_full_name",
        "active",
        "updated_at",
      ],
      where: "org_id = $1",
      params: [orgID],
    };
  },
  executionLogs: (
    snapshot: AuthenticatedAuthSnapshot,
    attemptID: string,
  ): ElectricShapeDefinition => {
    const orgID = requireDecimalID(requireSelectedOrgID(snapshot), "org_id");
    const validatedAttemptID = requireUUID(attemptID, "attempt_id");
    return {
      stream: "sandbox",
      table: "execution_logs",
      columns: ["attempt_id", "seq", "chunk"],
      where: "org_id = $1 AND attempt_id = $2",
      params: [orgID, validatedAttemptID],
    };
  },
  notificationInboxState: (snapshot: AuthenticatedAuthSnapshot): ElectricShapeDefinition => {
    const orgID = requireElectricOpaqueID(requireSelectedOrgID(snapshot), "org_id");
    const subjectID = requireElectricOpaqueID(snapshot.auth.userId, "recipient_subject_id");
    return {
      stream: "notifications",
      table: "notification_inbox_state",
      columns: ["inbox_state_id", "next_sequence", "read_up_to_sequence", "updated_at"],
      where: "org_id = $1 AND recipient_subject_id = $2",
      params: [orgID, subjectID],
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
    snapshot = await readAuthSnapshotFromCookie(request.headers.get("cookie") ?? undefined);
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

  const upstreamURL = new URL("/v1/shape", upstreamBaseURL(shape.stream));
  const queryValidation = copyProtocolQueryParams(request, upstreamURL);
  if (queryValidation) return queryValidation;

  upstreamURL.searchParams.set("table", shape.table);
  upstreamURL.searchParams.set("columns", shape.columns.join(","));
  upstreamURL.searchParams.set("where", shape.where);
  shape.params.forEach((value, index) => {
    upstreamURL.searchParams.set(`params[${index + 1}]`, value);
  });
  try {
    upstreamURL.searchParams.set("secret", await electricSecret(shape.stream));
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

function upstreamBaseURL(stream: ElectricStream): string {
  return stream === "notifications" ? ELECTRIC_NOTIFICATIONS_BASE_URL : ELECTRIC_BASE_URL;
}

function electricSecret(stream: ElectricStream): Promise<string> {
  const valueEnv =
    stream === "notifications" ? "ELECTRIC_NOTIFICATIONS_API_SECRET" : "ELECTRIC_API_SECRET";
  const pathEnv =
    stream === "notifications"
      ? "VERSELF_CRED_ELECTRIC_NOTIFICATIONS_API_SECRET"
      : "VERSELF_CRED_ELECTRIC_API_SECRET";
  const cacheKey = `${valueEnv}:${pathEnv}`;
  const cached = secretCache.get(cacheKey);
  if (cached) return cached;
  const loaded = readSecret(valueEnv, pathEnv);
  secretCache.set(cacheKey, loaded);
  return loaded;
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
