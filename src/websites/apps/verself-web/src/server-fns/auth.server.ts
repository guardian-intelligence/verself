import { getRequest, setResponseHeader } from "@tanstack/react-start/server";
import * as v from "valibot";
import { requireURLFromEnv } from "@verself/web-env";
import {
  parseAuthSnapshot,
  type AuthenticatedAuthSnapshot,
  type AuthSnapshot,
} from "@verself/auth-web/isomorphic";
import type { ConsoleAuthContext } from "./auth";

const IAM_SERVICE_BASE_URL = requireURLFromEnv("IAM_SERVICE_BASE_URL");

const resourceTokenResponseSchema = v.object({
  accessToken: v.pipe(v.string(), v.nonEmpty()),
});

function identityAuthURL(path: string): string {
  return new URL(`/api/v1/auth/${path}`, IAM_SERVICE_BASE_URL).toString();
}

function currentCookieHeader(): string | undefined {
  return getRequest().headers.get("cookie") ?? undefined;
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

async function identityAuthFetch(
  path: string,
  init: RequestInit = {},
  options: { cookieHeader?: string | undefined; forwardCookies?: boolean } = {},
): Promise<Response> {
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  const cookie = Object.prototype.hasOwnProperty.call(options, "cookieHeader")
    ? options.cookieHeader
    : currentCookieHeader();
  if (cookie) {
    headers.set("Cookie", cookie);
  }
  const response = await fetch(identityAuthURL(path), {
    ...init,
    headers,
  });
  if (options.forwardCookies !== false) {
    forwardSetCookie(response.headers);
  }
  return response;
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
): Promise<AuthSnapshot> {
  const response = await identityAuthFetch("session", {}, { cookieHeader, forwardCookies: false });
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

export async function getIdentityAccessTokenForAudience(
  context: ConsoleAuthContext | undefined,
  audience: string,
  options: { roleAssignmentScope?: "selected_org" | "all_granted_orgs" } = {},
): Promise<string> {
  await requireAuthSnapshot(context);
  const response = await identityAuthFetch("resource-token", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      audience,
      ...(options.roleAssignmentScope ? { roleAssignmentScope: options.roleAssignmentScope } : {}),
    }),
  });
  if (!response.ok) {
    throw new Error(`identity resource token failed: ${response.status} ${await response.text()}`);
  }
  return v.parse(resourceTokenResponseSchema, await response.json()).accessToken;
}
