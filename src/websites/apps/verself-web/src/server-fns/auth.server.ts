import { createHash } from "node:crypto";
import { setResponseHeader } from "@tanstack/react-start/server";
import * as v from "valibot";
import { requireURLFromEnv } from "@verself/web-env";
import {
  browserSessionsResponseSchema,
  parseAuthSnapshot,
  type AuthenticatedAuthSnapshot,
  type AuthSnapshot,
  type BrowserSessionsResponse,
} from "@verself/auth-web/isomorphic";
import type { ConsoleAuthContext } from "./auth";
import {
  currentCookieHeader,
  currentRequestHeaders,
  forwardRequestMetadata,
} from "./request-metadata.server";

const IAM_SERVICE_BASE_URL = requireURLFromEnv("IAM_SERVICE_BASE_URL");

const resourceTokenResponseSchema = v.object({
  accessToken: v.pipe(v.string(), v.nonEmpty()),
});

const organizationSummarySchema = v.object({
  orgId: v.pipe(v.string(), v.nonEmpty()),
  resourceName: v.pipe(v.string(), v.nonEmpty()),
  slug: v.optional(v.pipe(v.string(), v.nonEmpty())),
  displayName: v.pipe(v.string(), v.nonEmpty()),
  version: v.number(),
});

const signupVerificationResultSchema = v.object({
  organization: organizationSummarySchema,
  loginUrl: v.pipe(v.string(), v.nonEmpty()),
});

export type SignupVerificationResult = v.InferOutput<typeof signupVerificationResultSchema>;

function identityAuthURL(path: string): string {
  return new URL(`/api/v1/auth/${path}`, IAM_SERVICE_BASE_URL).toString();
}

function identityAPIURL(path: string): string {
  const normalized = path.startsWith("/") ? path : `/api/v1/${path}`;
  return new URL(normalized, IAM_SERVICE_BASE_URL).toString();
}

function forwardSetCookie(headers: Headers): void {
  const getSetCookie = (headers as Headers & { getSetCookie?: () => Array<string> }).getSetCookie;
  const cookies =
    typeof getSetCookie === "function" ? getSetCookie.call(headers) : [headers.get("set-cookie")];
  const resolved = cookies.filter((cookie): cookie is string => Boolean(cookie));
  if (resolved.length > 0) {
    setResponseHeader("set-cookie", resolved);
  }
}

async function identityFetch(
  url: string,
  init: RequestInit = {},
  options: {
    cookieHeader?: string | undefined;
    forwardCookies?: boolean;
    sourceHeaders?: Headers | undefined;
  } = {},
): Promise<Response> {
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  const hasExplicitCookie = Object.prototype.hasOwnProperty.call(options, "cookieHeader");
  const sourceHeaders =
    options.sourceHeaders ?? (hasExplicitCookie ? undefined : currentRequestHeaders());
  forwardRequestMetadata(headers, sourceHeaders);
  const cookie = hasExplicitCookie ? options.cookieHeader : currentCookieHeader(sourceHeaders);
  if (cookie) {
    headers.set("Cookie", cookie);
  }
  const response = await fetch(url, {
    ...init,
    headers,
  });
  if (options.forwardCookies !== false) {
    forwardSetCookie(response.headers);
  }
  return response;
}

async function identityAuthFetch(
  path: string,
  init: RequestInit = {},
  options: {
    cookieHeader?: string | undefined;
    forwardCookies?: boolean;
    sourceHeaders?: Headers | undefined;
  } = {},
): Promise<Response> {
  return identityFetch(identityAuthURL(path), init, options);
}

async function identityAPIFetch(
  path: string,
  init: RequestInit = {},
  options: {
    cookieHeader?: string | undefined;
    forwardCookies?: boolean;
    sourceHeaders?: Headers | undefined;
  } = {},
): Promise<Response> {
  return identityFetch(identityAPIURL(path), init, options);
}

export async function readAuthSnapshot(): Promise<AuthSnapshot> {
  const response = await identityAuthFetch("session");
  if (!response.ok) {
    throw new Error(`identity auth session failed: ${response.status} ${await response.text()}`);
  }
  return parseAuthSnapshot(await response.json());
}

export async function readAuthSnapshotFromCookie(
  cookieHeader: string | undefined,
  sourceHeaders?: Headers,
): Promise<AuthSnapshot> {
  const response = await identityAuthFetch(
    "session",
    {},
    { cookieHeader, forwardCookies: false, sourceHeaders },
  );
  if (!response.ok) {
    throw new Error(`identity auth session failed: ${response.status} ${await response.text()}`);
  }
  return parseAuthSnapshot(await response.json());
}

async function requireAuthSnapshot(
  context: ConsoleAuthContext | undefined,
): Promise<AuthenticatedAuthSnapshot> {
  if (context?.auth) {
    return context.auth;
  }
  const snapshot = await readAuthSnapshot();
  if (!snapshot.isSignedIn) {
    throw new Error("Authentication required");
  }
  return snapshot;
}

export async function selectIdentityOrganization(data: { orgID: string }): Promise<AuthSnapshot> {
  const response = await identityAuthFetch("organization", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(data),
  });
  if (!response.ok) {
    throw new Error(
      `identity organization switch failed: ${response.status} ${await response.text()}`,
    );
  }
  return parseAuthSnapshot(await response.json());
}

export async function listIdentityBrowserSessions(): Promise<BrowserSessionsResponse> {
  const response = await identityAuthFetch("sessions");
  if (!response.ok) {
    throw new Error(`identity sessions failed: ${response.status} ${await response.text()}`);
  }
  return v.parse(browserSessionsResponseSchema, await response.json());
}

export async function revokeIdentityBrowserSession(sessionHandle: string): Promise<void> {
  const response = await identityAuthFetch(`sessions/${encodeURIComponent(sessionHandle)}`, {
    method: "DELETE",
  });
  if (!response.ok) {
    throw new Error(`identity session revoke failed: ${response.status} ${await response.text()}`);
  }
}

export async function acceptIdentityMemberInvite(data: { token: string }): Promise<void> {
  const response = await identityAuthFetch(
    "invites/accept",
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Idempotency-Key": inviteAcceptanceIdempotencyKey(data.token),
      },
      body: JSON.stringify({
        acceptanceToken: data.token,
      }),
    },
    { cookieHeader: undefined, forwardCookies: false },
  );
  if (!response.ok) {
    throw new Error(`identity invite acceptance failed: ${response.status}`);
  }
}

function inviteAcceptanceIdempotencyKey(token: string): string {
  const digest = createHash("sha256").update(token).digest("base64url").slice(0, 48);
  return `invite-${digest}`;
}

export async function verifyIdentitySignup(data: {
  signupIntentId: string;
  verificationToken: string;
  organizationDisplayName: string;
}): Promise<SignupVerificationResult> {
  const organizationDisplayName = normalizeHumanText(data.organizationDisplayName);
  const response = await identityAPIFetch(
    `signup-intents/${encodeURIComponent(data.signupIntentId)}/verification`,
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Idempotency-Key": signupVerificationIdempotencyKey({
          ...data,
          organizationDisplayName,
        }),
      },
      body: JSON.stringify({
        verificationToken: data.verificationToken,
        organizationDisplayName,
      }),
    },
    { cookieHeader: undefined, forwardCookies: false },
  );
  if (!response.ok) {
    throw new Error(
      `identity signup verification failed: ${response.status} ${await response.text()}`,
    );
  }
  return v.parse(signupVerificationResultSchema, await response.json());
}

function signupVerificationIdempotencyKey(data: {
  signupIntentId: string;
  verificationToken: string;
  organizationDisplayName: string;
}): string {
  const digest = createHash("sha256")
    .update(data.signupIntentId)
    .update("\x00")
    .update(data.verificationToken)
    .update("\x00")
    .update(data.organizationDisplayName)
    .digest("base64url")
    .slice(0, 48);
  return `signup-${digest}`;
}

function normalizeHumanText(value: string): string {
  return value.trim().replace(/\s+/g, " ");
}

export async function getIdentityProductAccessToken(
  context: ConsoleAuthContext | undefined,
): Promise<string> {
  await requireAuthSnapshot(context);
  const response = await identityAuthFetch("resource-token", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
  });
  if (!response.ok) {
    throw new Error(`identity resource token failed: ${response.status} ${await response.text()}`);
  }
  return v.parse(resourceTokenResponseSchema, await response.json()).accessToken;
}
