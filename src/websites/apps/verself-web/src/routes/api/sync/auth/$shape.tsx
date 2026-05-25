import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { createFileRoute } from "@tanstack/react-router";
import { requireURLFromEnv } from "@verself/web-env";
import { forwardRequestMetadata } from "~/server-fns/request-metadata.server";

type AuthShape = "client" | "accounts" | "sessions";

type AuthShapeConfig = {
  readonly table: string;
  readonly columns: readonly string[];
  readonly where: string;
};

type AuthShapeProblemCode =
  | "auth_shape_authentication_required"
  | "auth_shape_forbidden"
  | "auth_shape_authorization_failed"
  | "auth_shape_iam_unavailable"
  | "auth_shape_electric_unavailable";

const authCookieName = "verself_client";
const browserAuthClientPrefix = "bc_";
const browserHandleDomain = "verself browser handle v1";
const identityAuthSessionURL = new URL(
  "/api/v1/auth/sync-session",
  requireURLFromEnv("IAM_SERVICE_BASE_URL"),
).toString();
const electricShapeURL = new URL(
  "/v1/shape",
  requireURLFromEnv("ELECTRIC_IAM_BASE_URL", {
    ...process.env,
    ELECTRIC_IAM_BASE_URL: process.env.ELECTRIC_IAM_BASE_URL ?? process.env.ELECTRIC_BASE_URL,
  }),
).toString();

const electricProtocolQueryParams = new Set([
  "live",
  "live_sse",
  "experimental_live_sse",
  "handle",
  "offset",
  "cursor",
  "expired_handle",
  "log",
  "subset__where",
  "subset__limit",
  "subset__offset",
  "subset__order_by",
  "subset__params",
  "subset__where_expr",
  "subset__order_by_expr",
  "cache-buster",
]);

const authShapes: Record<AuthShape, AuthShapeConfig> = {
  client: {
    table: "iam_web_auth_clients",
    columns: [
      "client_handle",
      "auth_state",
      "active_account_handle",
      "subject",
      "email",
      "display_name",
      "preferred_username",
      "org_id",
      "home_org_id",
      "selected_org_id",
      "available_org_contexts",
      "client_cache_partition",
      "auth_revision",
      "last_auth_event",
      "client_expires_at",
      "account_expires_at",
      "updated_at",
    ],
    where: `"client_handle" = $1`,
  },
  accounts: {
    table: "iam_web_auth_accounts",
    columns: [
      "client_handle",
      "account_handle",
      "is_current",
      "subject",
      "email",
      "display_name",
      "preferred_username",
      "home_org_id",
      "selected_org_id",
      "available_org_contexts",
      "created_at",
      "last_seen_at",
      "expires_at",
      "updated_at",
    ],
    where: `"client_handle" = $1`,
  },
  sessions: {
    table: "iam_web_auth_account_sessions",
    columns: [
      "client_handle",
      "account_handle",
      "session_handle",
      "is_current",
      "device_label",
      "device_kind",
      "browser_name",
      "os_name",
      "location_country_code",
      "location_region",
      "location_city",
      "client_ip",
      "client_ip_source",
      "user_agent",
      "created_at",
      "last_seen_at",
      "expires_at",
      "updated_at",
    ],
    where: `"client_handle" = $1`,
  },
};

let electricSecret: string | undefined;

export const Route = createFileRoute("/api/sync/auth/$shape")({
  server: {
    handlers: {
      GET: ({ request, params }) => proxyAuthShape(request, params.shape, "GET"),
      POST: ({ request, params }) => proxyAuthShape(request, params.shape, "POST"),
    },
  },
});

async function proxyAuthShape(
  request: Request,
  shapeParam: string,
  method: "GET" | "POST",
): Promise<Response> {
  const auth = await authenticateAuthShapeRequest(request);
  if (auth instanceof Response) {
    return auth;
  }

  const shape = parseAuthShape(shapeParam);
  if (!shape) {
    return authShapeProblem(request, 403, "auth_shape_forbidden", "auth shape is not allowed");
  }

  let upstreamURL: string;
  try {
    upstreamURL = upstreamElectricShapeURL(
      new URL(request.url),
      shape,
      auth.clientHandle,
      readElectricSecret(),
    );
  } catch {
    return authShapeProblem(
      request,
      503,
      "auth_shape_electric_unavailable",
      "electric unavailable",
    );
  }

  const init: RequestInit = { method, headers: electricRequestHeaders(request.headers) };
  if (method === "POST") {
    (init.headers as Headers).set("content-type", "application/json");
    init.body = await request.text();
  }

  let upstream: Response;
  try {
    upstream = await fetch(upstreamURL, init);
  } catch {
    return authShapeProblem(
      request,
      503,
      "auth_shape_electric_unavailable",
      "electric unavailable",
    );
  }

  return streamElectricResponse(upstream);
}

function parseAuthShape(value: string): AuthShape | null {
  return value === "client" || value === "accounts" || value === "sessions" ? value : null;
}

async function authenticateAuthShapeRequest(
  request: Request,
): Promise<{ readonly clientHandle: string } | Response> {
  const cookieHeader = request.headers.get("cookie") ?? "";
  const clientSecret = readCookie(cookieHeader, authCookieName);
  if (!clientSecret) {
    return authShapeProblem(
      request,
      401,
      "auth_shape_authentication_required",
      "authentication required",
    );
  }

  let upstream: Response;
  try {
    const headers = new Headers({ accept: "application/json", cookie: cookieHeader });
    forwardRequestMetadata(headers, request.headers);
    upstream = await fetch(identityAuthSessionURL, { headers });
  } catch {
    return authShapeProblem(request, 503, "auth_shape_iam_unavailable", "iam unavailable");
  }

  if (upstream.ok) {
    return { clientHandle: browserClientHandle(hashToken(clientSecret)) };
  }

  if (upstream.status === 401) {
    return authShapeProblem(
      request,
      401,
      "auth_shape_authentication_required",
      "authentication required",
      upstream.headers,
      true,
    );
  }
  if (upstream.status === 403) {
    return authShapeProblem(
      request,
      403,
      "auth_shape_forbidden",
      "auth shape is not allowed",
      upstream.headers,
    );
  }
  if (upstream.status >= 500) {
    return authShapeProblem(
      request,
      503,
      "auth_shape_iam_unavailable",
      "iam unavailable",
      upstream.headers,
    );
  }

  return authShapeProblem(
    request,
    upstream.status,
    "auth_shape_authorization_failed",
    "auth shape authorization failed",
    upstream.headers,
  );
}

function electricRequestHeaders(requestHeaders: Headers): Headers {
  const headers = new Headers();
  const accept = requestHeaders.get("accept");
  const ifNoneMatch = requestHeaders.get("if-none-match");
  if (accept) {
    headers.set("accept", accept);
  }
  if (ifNoneMatch) {
    headers.set("if-none-match", ifNoneMatch);
  }
  return headers;
}

function upstreamElectricShapeURL(
  requestURL: URL,
  shape: AuthShape,
  clientHandle: string,
  secret: string,
): string {
  const config = authShapes[shape];
  const upstream = new URL(electricShapeURL);
  for (const [key, value] of requestURL.searchParams) {
    if (electricProtocolQueryParams.has(key)) {
      upstream.searchParams.append(key, value);
    }
  }
  upstream.searchParams.set("table", config.table);
  upstream.searchParams.set("columns", config.columns.join(","));
  upstream.searchParams.set("where", config.where);
  upstream.searchParams.set("params[1]", clientHandle);
  upstream.searchParams.set("secret", secret);
  return upstream.toString();
}

function streamElectricResponse(response: Response): Response {
  const headers = new Headers(response.headers);
  headers.delete("content-encoding");
  headers.delete("content-length");
  headers.set("Vary", "Cookie");
  return new Response(response.body, {
    status: response.status,
    statusText: response.statusText,
    headers,
  });
}

function authShapeProblem(
  request: Request,
  status: number,
  code: AuthShapeProblemCode,
  detail: string,
  upstreamHeaders?: Headers,
  clearAuthCookie = false,
): Response {
  const headers = new Headers({
    "cache-control": "no-store",
    "content-type": "application/problem+json",
    Vary: "Cookie",
  });
  for (const cookie of setCookieHeaders(upstreamHeaders)) {
    headers.append("set-cookie", cookie);
  }
  if (clearAuthCookie && status === 401 && !headers.has("set-cookie")) {
    headers.append("set-cookie", clearClientCookie());
  }
  const requestURL = new URL(request.url);
  const traceparent = request.headers.get("traceparent") ?? "";
  const problem = {
    type: `urn:verself:problem:web-auth-sync:${code}`,
    title: authShapeProblemTitle(status),
    status,
    detail,
    code,
    trace: { traceparent },
    request: {
      method: request.method,
      path: requestURL.pathname,
    },
  };
  return new Response(`${JSON.stringify(problem)}\n`, { status, headers });
}

function authShapeProblemTitle(status: number): string {
  switch (status) {
    case 401:
      return "Authentication required";
    case 403:
      return "Forbidden";
    case 503:
      return "Service unavailable";
    default:
      return "Auth sync failed";
  }
}

function setCookieHeaders(headers: Headers | undefined): string[] {
  if (!headers) {
    return [];
  }
  const getSetCookie = (headers as Headers & { getSetCookie?: () => string[] }).getSetCookie;
  if (typeof getSetCookie === "function") {
    return getSetCookie.call(headers);
  }
  const header = headers.get("set-cookie");
  return header ? [header] : [];
}

function clearClientCookie(): string {
  return `${authCookieName}=; Path=/; Expires=Thu, 01 Jan 1970 00:00:00 GMT; Max-Age=0; HttpOnly; Secure; SameSite=Lax`;
}

function readCookie(cookieHeader: string, name: string): string {
  for (const part of cookieHeader.split(";")) {
    const [rawKey, ...rawValue] = part.split("=");
    if (rawKey?.trim() === name) {
      return decodeURIComponent(rawValue.join("=").trim());
    }
  }
  return "";
}

function hashToken(value: string): string {
  return createHash("sha256").update(value).digest("base64url");
}

function browserClientHandle(clientHash: string): string {
  const hash = createHash("sha256");
  hash.update(browserHandleDomain);
  hash.update("\x00");
  hash.update(clientHash);
  return `${browserAuthClientPrefix}${hash.digest("base64url").slice(0, 32)}`;
}

function readElectricSecret(): string {
  if (electricSecret !== undefined) {
    return electricSecret;
  }
  const path =
    process.env.VERSELF_CRED_ELECTRIC_IAM_API_SECRET ??
    process.env.VERSELF_CRED_ELECTRIC_API_SECRET;
  if (!path) {
    throw new Error("VERSELF_CRED_ELECTRIC_IAM_API_SECRET is required");
  }
  electricSecret = readFileSync(path, "utf8").trim();
  if (!electricSecret) {
    throw new Error("Electric IAM API secret is empty");
  }
  return electricSecret;
}
