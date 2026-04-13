import { createMiddleware } from "@tanstack/react-start";
import { decodeJwt, createRemoteJWKSet, jwtVerify, type JWTPayload } from "jose";
import * as v from "valibot";
import { anonymousAuth, parseAuthSnapshot } from "./isomorphic.ts";
import { resolveAuthConfig } from "./config.ts";
import type {
  AnonymousAuth,
  Auth,
  AuthRoleAssignment,
  AuthSnapshot,
  ClientUser,
  SessionInfo,
} from "./isomorphic.ts";
import type { AuthConfig, AuthConfigSource } from "./config.ts";

export { createAuthConfig } from "./config.ts";
export type { AuthConfig, AuthConfigSource } from "./config.ts";

export {
  anonymousAuth,
  authCacheKey,
  authCollectionId,
  authQueryKey,
  loginRedirect,
  parseAuthSnapshot,
  requireAuth,
  syncAuthPartitionedCache,
} from "./isomorphic.ts";
export type {
  AnonymousAuth,
  Auth,
  AuthRoleAssignment,
  AuthenticatedAuth,
  AuthSnapshot,
  ClientUser,
  SessionInfo,
} from "./isomorphic.ts";

export interface CurrentUser {
  sub: string;
  email: string | null;
  name: string | null;
  preferredUsername: string | null;
  // Current runtime still assumes one active org per user even though Zitadel
  // can issue multi-org assignments.
  orgID: string | null;
  roles: string[];
  roleAssignments: AuthRoleAssignment[];
  claims: Record<string, unknown>;
}

export interface AuthSession {
  sessionID: string;
  clientCachePartition: string;
  accessToken: string;
  refreshToken: string | null;
  idToken: string | null;
  scope: string | null;
  expiresAt: Date;
  user: CurrentUser;
  createdAt: Date;
  updatedAt: Date;
}

interface ProviderMetadata {
  issuer: string;
  authorization_endpoint: string;
  token_endpoint: string;
  jwks_uri: string;
  userinfo_endpoint?: string;
  end_session_endpoint?: string;
}

const pendingLoginStateSchema = v.object({
  state: v.pipe(v.string(), v.nonEmpty()),
  nonce: v.pipe(v.string(), v.nonEmpty()),
  codeVerifier: v.pipe(v.string(), v.nonEmpty()),
  redirectTo: v.string(),
  createdAt: v.pipe(v.number(), v.finite()),
});

type PendingLoginState = v.InferOutput<typeof pendingLoginStateSchema>;

const authCookieDataSchema = v.object({
  sessionID: v.optional(v.string()),
  login: v.optional(pendingLoginStateSchema),
  loginTransactions: v.optional(
    v.record(v.pipe(v.string(), v.nonEmpty()), pendingLoginStateSchema),
  ),
});

type AuthCookieData = v.InferOutput<typeof authCookieDataSchema>;

interface StoredAuthSessionRow {
  session_id: string;
  app_name: string;
  client_cache_partition: string;
  subject: string;
  email: string | null;
  display_name: string | null;
  preferred_username: string | null;
  org_id: string | null;
  roles: string[] | null;
  user_claims: Record<string, unknown> | null;
  id_token: string | null;
  access_token: string;
  refresh_token: string | null;
  token_scope: string | null;
  expires_at: string | Date;
  created_at: string | Date;
  updated_at: string | Date;
}

interface StoredResourceTokenRow {
  access_token: string;
  token_scope: string | null;
  expires_at: string | Date;
}

type ResolvedAuthSnapshot = AuthSnapshot & {
  currentUser: CurrentUser | null;
};

interface TokenResponse {
  access_token: string;
  token_type: string;
  expires_in: number;
  refresh_token?: string;
  id_token?: string;
  scope?: string;
}

interface OAuthErrorBody {
  error?: string;
  error_description?: string;
}

class OIDCExchangeError extends Error {
  status: number | null;
  oauthError: string | null;
  oauthDescription: string | null;
  body: string | null;
  isNetworkError: boolean;

  constructor(
    message: string,
    options: {
      status?: number | null;
      oauthError?: string | null;
      oauthDescription?: string | null;
      body?: string | null;
      isNetworkError?: boolean;
    } = {},
  ) {
    super(message);
    this.name = "OIDCExchangeError";
    this.status = options.status ?? null;
    this.oauthError = options.oauthError ?? null;
    this.oauthDescription = options.oauthDescription ?? null;
    this.body = options.body ?? null;
    this.isNetworkError = options.isNetworkError ?? false;
  }
}

interface RefreshResult {
  session: AuthSession | null;
  revoked: boolean;
}

const pendingLoginTTL = 5 * 60 * 1000;
const maxPendingLoginTransactions = 5;
const metadataCache = new Map<string, Promise<ProviderMetadata>>();
const jwksCache = new Map<string, ReturnType<typeof createRemoteJWKSet>>();
type SQLClient = import("postgres").Sql<Record<string, unknown>>;
const sqlCache = new Map<string, SQLClient>();

async function getSQL(databaseURL: string): Promise<SQLClient> {
  let sql = sqlCache.get(databaseURL);
  if (!sql) {
    const { default: postgres } = await import("postgres");
    sql = postgres(databaseURL, {
      max: 5,
      idle_timeout: 20,
      prepare: true,
    });
    sqlCache.set(databaseURL, sql);
  }
  return sql;
}

async function getProviderMetadata(issuerURL: string): Promise<ProviderMetadata> {
  let metadataPromise = metadataCache.get(issuerURL);
  if (!metadataPromise) {
    metadataPromise = fetch(new URL("/.well-known/openid-configuration", issuerURL).toString(), {
      headers: { Accept: "application/json" },
    }).then(async (response) => {
      if (!response.ok) {
        throw new Error(
          `OIDC discovery failed for ${issuerURL}: ${response.status} ${await response.text()}`,
        );
      }
      return response.json() as Promise<ProviderMetadata>;
    });
    metadataCache.set(issuerURL, metadataPromise);
  }
  return metadataPromise;
}

function getJWKS(metadata: ProviderMetadata) {
  let jwks = jwksCache.get(metadata.jwks_uri);
  if (!jwks) {
    jwks = createRemoteJWKSet(new URL(metadata.jwks_uri));
    jwksCache.set(metadata.jwks_uri, jwks);
  }
  return jwks;
}

async function verifyAccessTokenExpiresAt(
  metadata: ProviderMetadata,
  accessToken: string,
  audience: string,
  fallbackExpiresIn?: number,
): Promise<Date> {
  const verified = await jwtVerify(accessToken, getJWKS(metadata), {
    issuer: metadata.issuer,
    audience,
  });
  if (typeof verified.payload.exp === "number") {
    return new Date(verified.payload.exp * 1000);
  }
  if (typeof fallbackExpiresIn === "number") {
    return new Date(Date.now() + fallbackExpiresIn * 1000);
  }
  throw new Error("OIDC access token is missing exp");
}

async function getStartServerModule() {
  return import("@tanstack/react-start/server");
}

async function getBaseURL(): Promise<URL> {
  const { getRequestUrl } = await getStartServerModule();
  return new URL(getRequestUrl({ xForwardedHost: true, xForwardedProto: true }).toString());
}

async function getAbsoluteURL(pathname: string): Promise<string> {
  const baseURL = await getBaseURL();
  const resolvedURL = new URL(pathname, baseURL);
  // Zitadel compares post_logout_redirect_uri literally, so keep "/" as the bare origin.
  if (resolvedURL.pathname === "/" && !resolvedURL.search && !resolvedURL.hash) {
    return resolvedURL.origin;
  }
  return resolvedURL.toString();
}

function toDate(value: string | Date): Date {
  return value instanceof Date ? value : new Date(value);
}

function base64UrlEncode(bytes: Uint8Array): string {
  // This module is imported by both server and client bundles, so avoid Node Buffer.
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
  let output = "";
  for (let index = 0; index < bytes.length; index += 3) {
    const a = bytes[index] ?? 0;
    const b = bytes[index + 1] ?? 0;
    const c = bytes[index + 2] ?? 0;
    const chunk = (a << 16) | (b << 8) | c;

    output += alphabet[(chunk >> 18) & 0x3f];
    output += alphabet[(chunk >> 12) & 0x3f];
    if (index + 1 < bytes.length) {
      output += alphabet[(chunk >> 6) & 0x3f];
    }
    if (index + 2 < bytes.length) {
      output += alphabet[chunk & 0x3f];
    }
  }
  return output;
}

function randomToken(bytes = 32): string {
  return base64UrlEncode(crypto.getRandomValues(new Uint8Array(bytes)));
}

function parseAuthCookieData(value: unknown): AuthCookieData {
  const result = v.safeParse(authCookieDataSchema, value);
  return result.success ? result.output : {};
}

function parsePendingLoginState(value: unknown): PendingLoginState | null {
  const result = v.safeParse(pendingLoginStateSchema, value);
  return result.success ? result.output : null;
}

function isPendingLoginExpired(pending: PendingLoginState, now: number): boolean {
  return !Number.isFinite(pending.createdAt) || now - pending.createdAt > pendingLoginTTL;
}

function pendingLoginEntries(data: AuthCookieData): PendingLoginState[] {
  const byState = new Map<string, PendingLoginState>();
  const parsedData = parseAuthCookieData(data);
  const transactions = parsedData.loginTransactions;
  if (transactions && typeof transactions === "object" && !Array.isArray(transactions)) {
    for (const [state, rawPending] of Object.entries(transactions)) {
      const pending = parsePendingLoginState(rawPending);
      if (pending && state === pending.state) {
        byState.set(state, pending);
      }
    }
  }
  // Legacy single-transaction cookies may still be in browsers during deploys.
  const legacyPending = parsePendingLoginState(parsedData.login);
  if (legacyPending) {
    byState.set(legacyPending.state, legacyPending);
  }
  return [...byState.values()].sort((left, right) => right.createdAt - left.createdAt);
}

function activePendingLoginEntries(data: AuthCookieData, now: number): PendingLoginState[] {
  return pendingLoginEntries(data).filter((pending) => !isPendingLoginExpired(pending, now));
}

function pendingLoginTransactionStore(
  data: AuthCookieData,
  pending: PendingLoginState,
  now: number,
): Record<string, PendingLoginState> {
  const byState = new Map<string, PendingLoginState>();
  for (const entry of activePendingLoginEntries(data, now)) {
    byState.set(entry.state, entry);
  }
  byState.set(pending.state, pending);
  return Object.fromEntries(
    [...byState.values()]
      .sort((left, right) => right.createdAt - left.createdAt)
      .slice(0, maxPendingLoginTransactions)
      .map((entry) => [entry.state, entry]),
  );
}

function findPendingLoginTransaction(
  data: AuthCookieData,
  state: string,
): { hasAny: boolean; pending: PendingLoginState | null } {
  const entries = pendingLoginEntries(data);
  return {
    hasAny: entries.length > 0,
    pending: entries.find((entry) => entry.state === state) ?? null,
  };
}

async function codeChallenge(verifier: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(verifier));
  return base64UrlEncode(new Uint8Array(digest));
}

async function sanitizeRedirectTarget(
  redirectTo: string | null | undefined,
  fallback: string,
): Promise<string> {
  if (!redirectTo) return fallback;
  try {
    const baseURL = await getBaseURL();
    const parsed = new URL(redirectTo, baseURL);
    if (parsed.origin !== baseURL.origin) {
      return fallback;
    }
    return `${parsed.pathname}${parsed.search}${parsed.hash}`;
  } catch {
    return fallback;
  }
}

async function getSessionManager(config: AuthConfig) {
  const { useSession } = await getStartServerModule();
  return useSession<AuthCookieData>({
    password: config.sessionPassword,
    name: config.sessionCookieName ?? `${config.appName}-session`,
    maxAge: config.sessionMaxAgeSeconds ?? 60 * 60 * 24 * 30,
    cookie: {
      httpOnly: true,
      path: "/",
      sameSite: "lax",
      secure: process.env.NODE_ENV === "production",
    },
  });
}

async function fetchUserInfo(
  metadata: ProviderMetadata,
  accessToken: string,
): Promise<Record<string, unknown>> {
  if (!metadata.userinfo_endpoint) {
    throw new Error("OIDC provider metadata is missing userinfo_endpoint");
  }
  // Zitadel puts profile and role claims on userinfo even when the access token
  // itself only carries transport-level claims.
  const response = await fetch(metadata.userinfo_endpoint, {
    headers: {
      Accept: "application/json",
      Authorization: `Bearer ${accessToken}`,
    },
  });
  if (!response.ok) {
    throw new Error(`OIDC userinfo request failed: ${response.status} ${await response.text()}`);
  }
  const payload = await response.json();
  if (!payload || typeof payload !== "object" || Array.isArray(payload)) {
    throw new Error("OIDC userinfo payload must be an object");
  }
  return payload as Record<string, unknown>;
}

async function exchangeToken(
  metadata: ProviderMetadata,
  config: AuthConfig,
  params: URLSearchParams,
): Promise<TokenResponse> {
  params.set("client_id", config.clientID);
  if (config.clientSecret) {
    params.set("client_secret", config.clientSecret);
  }
  let response: Response;
  try {
    response = await fetch(metadata.token_endpoint, {
      method: "POST",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/x-www-form-urlencoded",
      },
      body: params.toString(),
    });
  } catch (error) {
    throw new OIDCExchangeError("OIDC token exchange failed: network error", {
      isNetworkError: true,
      body: error instanceof Error ? error.message : String(error),
    });
  }
  if (!response.ok) {
    const body = await response.text();
    let oauthBody: OAuthErrorBody | null = null;
    try {
      oauthBody = JSON.parse(body) as OAuthErrorBody;
    } catch {
      oauthBody = null;
    }
    const messageDetail =
      oauthBody?.error_description ?? oauthBody?.error ?? (body || response.statusText);
    throw new OIDCExchangeError(`OIDC token exchange failed: ${response.status} ${messageDetail}`, {
      status: response.status,
      oauthError: oauthBody?.error ?? null,
      oauthDescription: oauthBody?.error_description ?? null,
      body,
    });
  }
  return response.json() as Promise<TokenResponse>;
}

function logAuthEvent(level: "warn" | "error", event: string, fields: Record<string, unknown>) {
  const logger = level === "error" ? console.error : console.warn;
  logger("[auth-web]", {
    event,
    ...fields,
  });
}

function extractRoles(payload: JWTPayload): string[] {
  const roles = new Set<string>();
  for (const [claim, value] of Object.entries(payload)) {
    if (
      claim !== "urn:zitadel:iam:org:project:roles" &&
      !/^urn:zitadel:iam:org:project:[^:]+:roles$/.test(claim)
    ) {
      continue;
    }
    if (!value || typeof value !== "object" || Array.isArray(value)) {
      continue;
    }
    for (const role of Object.keys(value as Record<string, unknown>)) {
      roles.add(role);
    }
  }
  return [...roles].sort();
}

function extractRoleAssignments(payload: JWTPayload): AuthRoleAssignment[] {
  const assignments: AuthRoleAssignment[] = [];

  for (const [key, value] of Object.entries(payload)) {
    let projectID: string | null = null;
    if (key === "urn:zitadel:iam:org:project:roles") {
      projectID = null;
    } else if (key.startsWith("urn:zitadel:iam:org:project:") && key.endsWith(":roles")) {
      projectID = key.slice("urn:zitadel:iam:org:project:".length, -":roles".length);
    } else {
      continue;
    }

    if (!value || typeof value !== "object" || Array.isArray(value)) {
      continue;
    }

    for (const [role, organizations] of Object.entries(value as Record<string, unknown>)) {
      if (!organizations || typeof organizations !== "object" || Array.isArray(organizations)) {
        continue;
      }
      for (const [orgID, orgName] of Object.entries(organizations as Record<string, unknown>)) {
        assignments.push({
          projectID,
          orgID,
          orgName: typeof orgName === "string" ? orgName : null,
          role,
        });
      }
    }
  }

  return assignments.sort((left, right) => {
    const projectOrder = (left.projectID ?? "").localeCompare(right.projectID ?? "");
    if (projectOrder !== 0) {
      return projectOrder;
    }
    const orgOrder = left.orgID.localeCompare(right.orgID);
    if (orgOrder !== 0) {
      return orgOrder;
    }
    return left.role.localeCompare(right.role);
  });
}

function buildUserSnapshot(
  idTokenClaims: JWTPayload,
  accessTokenClaims: JWTPayload,
  userInfoClaims: Record<string, unknown>,
): CurrentUser {
  const mergedClaims: JWTPayload = {
    ...accessTokenClaims,
    ...idTokenClaims,
    ...userInfoClaims,
  };
  const email =
    typeof mergedClaims.email === "string"
      ? mergedClaims.email
      : typeof idTokenClaims.email === "string"
        ? idTokenClaims.email
        : typeof accessTokenClaims.email === "string"
          ? accessTokenClaims.email
          : null;
  const preferredUsername =
    typeof mergedClaims.preferred_username === "string"
      ? mergedClaims.preferred_username
      : typeof idTokenClaims.preferred_username === "string"
        ? idTokenClaims.preferred_username
        : typeof accessTokenClaims.preferred_username === "string"
          ? accessTokenClaims.preferred_username
          : null;
  const name =
    typeof mergedClaims.name === "string"
      ? mergedClaims.name
      : (preferredUsername ?? email ?? idTokenClaims.sub ?? null);
  const orgID =
    typeof mergedClaims["urn:zitadel:iam:user:resourceowner:id"] === "string"
      ? (mergedClaims["urn:zitadel:iam:user:resourceowner:id"] as string)
      : null;

  return {
    sub: idTokenClaims.sub ?? "",
    email,
    name,
    preferredUsername,
    orgID,
    roles: extractRoles(mergedClaims),
    roleAssignments: extractRoleAssignments(mergedClaims),
    claims: mergedClaims,
  };
}

function rowToAuthSession(row: StoredAuthSessionRow): AuthSession {
  return {
    sessionID: row.session_id,
    clientCachePartition: row.client_cache_partition,
    accessToken: row.access_token,
    refreshToken: row.refresh_token,
    idToken: row.id_token,
    scope: row.token_scope,
    expiresAt: toDate(row.expires_at),
    createdAt: toDate(row.created_at),
    updatedAt: toDate(row.updated_at),
    user: {
      sub: row.subject,
      email: row.email,
      name: row.display_name,
      preferredUsername: row.preferred_username,
      orgID: row.org_id,
      roles: row.roles ?? [],
      roleAssignments: extractRoleAssignments((row.user_claims ?? {}) as JWTPayload),
      claims: row.user_claims ?? {},
    },
  };
}

function toClientUser(user: CurrentUser): ClientUser {
  return {
    sub: user.sub,
    email: user.email,
    name: user.name,
    preferredUsername: user.preferredUsername,
    orgID: user.orgID,
    roles: user.roles,
    roleAssignments: user.roleAssignments,
  };
}

async function readStoredSession(
  config: AuthConfig,
  sessionID: string,
): Promise<AuthSession | null> {
  const sql = await getSQL(config.sessionDatabaseURL);
  const [row] = await sql<StoredAuthSessionRow[]>`
    SELECT
      session_id,
      app_name,
      client_cache_partition,
      subject,
      email,
      display_name,
      preferred_username,
      org_id,
      roles,
      user_claims,
      id_token,
      access_token,
      refresh_token,
      token_scope,
      expires_at,
      created_at,
      updated_at
    FROM auth_sessions
    WHERE app_name = ${config.appName}
      AND session_id = ${sessionID}
  `;
  if (!row) {
    return null;
  }
  return rowToAuthSession(row);
}

async function writeStoredSession(
  config: AuthConfig,
  sessionID: string,
  clientCachePartition: string,
  tokens: TokenResponse,
  user: CurrentUser,
): Promise<void> {
  const sql = await getSQL(config.sessionDatabaseURL);
  const expiresAt = new Date(Date.now() + tokens.expires_in * 1000);
  await sql`
    INSERT INTO auth_sessions (
      session_id,
      app_name,
      client_cache_partition,
      subject,
      email,
      display_name,
      preferred_username,
      org_id,
      roles,
      user_claims,
      id_token,
      access_token,
      refresh_token,
      token_scope,
      expires_at
    ) VALUES (
      ${sessionID},
      ${config.appName},
      ${clientCachePartition},
      ${user.sub},
      ${user.email},
      ${user.name},
      ${user.preferredUsername},
      ${user.orgID},
      ${user.roles},
      ${user.claims},
      ${tokens.id_token ?? null},
      ${tokens.access_token},
      ${tokens.refresh_token ?? null},
      ${tokens.scope ?? null},
      ${expiresAt.toISOString()}
    )
    ON CONFLICT (session_id) DO UPDATE SET
      app_name = EXCLUDED.app_name,
      client_cache_partition = EXCLUDED.client_cache_partition,
      subject = EXCLUDED.subject,
      email = EXCLUDED.email,
      display_name = EXCLUDED.display_name,
      preferred_username = EXCLUDED.preferred_username,
      org_id = EXCLUDED.org_id,
      roles = EXCLUDED.roles,
      user_claims = EXCLUDED.user_claims,
      id_token = EXCLUDED.id_token,
      access_token = EXCLUDED.access_token,
      refresh_token = EXCLUDED.refresh_token,
      token_scope = EXCLUDED.token_scope,
      expires_at = EXCLUDED.expires_at,
      updated_at = now()
  `;
}

async function deleteStoredSession(config: AuthConfig, sessionID: string): Promise<void> {
  const sql = await getSQL(config.sessionDatabaseURL);
  await sql`
    DELETE FROM auth_sessions
    WHERE app_name = ${config.appName}
      AND session_id = ${sessionID}
  `;
}

async function readStoredResourceToken(
  config: AuthConfig,
  sessionID: string,
  audience: string,
): Promise<StoredResourceTokenRow | null> {
  const sql = await getSQL(config.sessionDatabaseURL);
  const [row] = await sql<StoredResourceTokenRow[]>`
    SELECT access_token, token_scope, expires_at
    FROM auth_resource_tokens
    WHERE session_id = ${sessionID}
      AND audience = ${audience}
  `;
  return row ?? null;
}

async function writeStoredResourceToken(
  config: AuthConfig,
  sessionID: string,
  audience: string,
  tokens: TokenResponse,
  expiresAt: Date,
): Promise<void> {
  const sql = await getSQL(config.sessionDatabaseURL);
  await sql`
    INSERT INTO auth_resource_tokens (
      session_id,
      audience,
      access_token,
      token_scope,
      expires_at
    ) VALUES (
      ${sessionID},
      ${audience},
      ${tokens.access_token},
      ${tokens.scope ?? null},
      ${expiresAt.toISOString()}
    )
    ON CONFLICT (session_id, audience) DO UPDATE SET
      access_token = EXCLUDED.access_token,
      token_scope = EXCLUDED.token_scope,
      expires_at = EXCLUDED.expires_at,
      updated_at = now()
  `;
}

async function refreshStoredSession(
  config: AuthConfig,
  stored: AuthSession,
): Promise<RefreshResult> {
  if (!stored.refreshToken) {
    return { session: null, revoked: true };
  }

  const metadata = await getProviderMetadata(config.issuerURL);
  let refreshed: TokenResponse;
  try {
    refreshed = await exchangeToken(
      metadata,
      config,
      new URLSearchParams({
        grant_type: "refresh_token",
        refresh_token: stored.refreshToken,
      }),
    );
  } catch (error) {
    const oidcError =
      error instanceof OIDCExchangeError
        ? error
        : new OIDCExchangeError("OIDC token refresh failed", {
            body: error instanceof Error ? error.message : String(error),
          });
    const revoked =
      oidcError.oauthError === "invalid_grant" ||
      oidcError.oauthError === "invalid_token" ||
      oidcError.status === 400 ||
      oidcError.status === 401;

    logAuthEvent(revoked ? "warn" : "error", "token_refresh_failed", {
      app_name: config.appName,
      session_id: stored.sessionID,
      subject: stored.user.sub,
      status: oidcError.status,
      oauth_error: oidcError.oauthError,
      oauth_error_description: oidcError.oauthDescription,
      failure_type: revoked
        ? "revoked_or_invalid"
        : oidcError.isNetworkError
          ? "network"
          : "upstream",
      token_expires_at: stored.expiresAt.toISOString(),
      body: oidcError.body,
    });
    if (!revoked && stored.expiresAt.getTime() > Date.now()) {
      return { session: stored, revoked: false };
    }
    return { session: null, revoked };
  }

  if (!refreshed.id_token) {
    logAuthEvent("warn", "token_refresh_missing_id_token", {
      app_name: config.appName,
      session_id: stored.sessionID,
      subject: stored.user.sub,
    });
    return { session: null, revoked: true };
  }

  const verifiedIDToken = await jwtVerify(refreshed.id_token, getJWKS(metadata), {
    issuer: metadata.issuer,
    audience: config.clientID,
  });
  const accessTokenClaims = decodeJwt(refreshed.access_token);
  const userInfoClaims = await fetchUserInfo(metadata, refreshed.access_token);
  const user = buildUserSnapshot(verifiedIDToken.payload, accessTokenClaims, userInfoClaims);
  await writeStoredSession(config, stored.sessionID, stored.clientCachePartition, refreshed, user);
  return {
    session: await readStoredSession(config, stored.sessionID),
    revoked: false,
  };
}

export async function beginLogin(
  config: AuthConfig,
  requestedRedirectTo?: string | null,
): Promise<string> {
  const metadata = await getProviderMetadata(config.issuerURL);
  const session = await getSessionManager(config);
  const now = Date.now();
  const state = randomToken();
  const nonce = randomToken();
  const codeVerifier = randomToken(48);
  const redirectTo = await sanitizeRedirectTarget(requestedRedirectTo, config.defaultRedirectPath);
  const pending = {
    state,
    nonce,
    codeVerifier,
    redirectTo,
    createdAt: now,
  };
  const authorizeURL = new URL(metadata.authorization_endpoint);
  authorizeURL.searchParams.set("client_id", config.clientID);
  authorizeURL.searchParams.set("redirect_uri", await getAbsoluteURL(config.callbackPath));
  authorizeURL.searchParams.set("response_type", "code");
  authorizeURL.searchParams.set("scope", config.scopes.join(" "));
  authorizeURL.searchParams.set("state", state);
  authorizeURL.searchParams.set("nonce", nonce);
  authorizeURL.searchParams.set("code_challenge", await codeChallenge(codeVerifier));
  authorizeURL.searchParams.set("code_challenge_method", "S256");

  await session.update({
    // The browser can trigger overlapping sign-in starts through an SSR route
    // redirect and a hydrated server function. Keep all fresh states so the
    // provider callback is not invalidated by the later transaction.
    login: pending,
    loginTransactions: pendingLoginTransactionStore(session.data, pending, now),
  });

  return authorizeURL.toString();
}

export async function finishLogin(config: AuthConfig): Promise<{
  redirectTo: string;
  session: AuthSession;
}> {
  const requestURL = await getBaseURL();
  const error = requestURL.searchParams.get("error");
  if (error) {
    const description = requestURL.searchParams.get("error_description");
    throw new Error(description ? `${error}: ${description}` : error);
  }

  const code = requestURL.searchParams.get("code");
  const state = requestURL.searchParams.get("state");
  if (!code || !state) {
    throw new Error("OIDC callback is missing code or state");
  }

  const session = await getSessionManager(config);
  const { hasAny, pending } = findPendingLoginTransaction(session.data, state);
  if (!pending) {
    if (hasAny) {
      throw new Error("OIDC callback state mismatch");
    }
    throw new Error("OIDC callback is missing login transaction state");
  }
  if (isPendingLoginExpired(pending, Date.now())) {
    await session.update({ loginTransactions: {} });
    throw new Error("OIDC callback login transaction expired");
  }

  const metadata = await getProviderMetadata(config.issuerURL);
  const tokens = await exchangeToken(
    metadata,
    config,
    new URLSearchParams({
      grant_type: "authorization_code",
      code,
      redirect_uri: await getAbsoluteURL(config.callbackPath),
      code_verifier: pending.codeVerifier,
    }),
  );

  if (!tokens.id_token) {
    throw new Error("OIDC callback returned no id_token");
  }

  const verifiedIDToken = await jwtVerify(tokens.id_token, getJWKS(metadata), {
    issuer: metadata.issuer,
    audience: config.clientID,
  });
  if (verifiedIDToken.payload.nonce !== pending.nonce) {
    throw new Error("OIDC callback nonce mismatch");
  }
  const accessTokenClaims = decodeJwt(tokens.access_token);
  const userInfoClaims = await fetchUserInfo(metadata, tokens.access_token);
  const user = buildUserSnapshot(verifiedIDToken.payload, accessTokenClaims, userInfoClaims);
  const sessionID = crypto.randomUUID();
  const clientCachePartition = randomToken(24);

  await writeStoredSession(config, sessionID, clientCachePartition, tokens, user);
  await session.clear();
  await session.update({ sessionID });

  const storedSession = await readStoredSession(config, sessionID);
  if (!storedSession) {
    throw new Error("Auth session was not persisted");
  }

  return {
    redirectTo: new URL(pending.redirectTo, await getBaseURL()).toString(),
    session: storedSession,
  };
}

export async function getAuthSession(config: AuthConfig): Promise<AuthSession | null> {
  const session = await getSessionManager(config);
  const sessionID = session.data.sessionID;
  if (!sessionID) {
    return null;
  }

  const stored = await readStoredSession(config, sessionID);
  if (!stored) {
    await session.clear();
    return null;
  }

  const refreshLeeway = (config.refreshLeewaySeconds ?? 60) * 1000;
  if (stored.expiresAt.getTime() - Date.now() <= refreshLeeway) {
    const refreshed = await refreshStoredSession(config, stored);
    if (!refreshed.session) {
      await deleteStoredSession(config, stored.sessionID);
      await session.clear();
      return null;
    }
    return refreshed.session;
  }

  return stored;
}

export async function getAccessTokenForAudience(
  config: AuthConfig,
  session: AuthSession,
  audience: string,
): Promise<string> {
  const trimmedAudience = audience.trim();
  if (!trimmedAudience) {
    throw new Error("Resource audience is required");
  }

  const refreshLeewayMs = (config.refreshLeewaySeconds ?? 60) * 1000;
  const metadata = await getProviderMetadata(config.issuerURL);
  const cached = await readStoredResourceToken(config, session.sessionID, trimmedAudience);
  if (cached && toDate(cached.expires_at).getTime() - Date.now() > refreshLeewayMs) {
    const cachedExpiresAt = await verifyAccessTokenExpiresAt(
      metadata,
      cached.access_token,
      trimmedAudience,
    ).catch(() => null);
    if (cachedExpiresAt && cachedExpiresAt.getTime() - Date.now() > refreshLeewayMs) {
      return cached.access_token;
    }
  }

  const tokens = await exchangeToken(
    metadata,
    config,
    new URLSearchParams({
      grant_type: "urn:ietf:params:oauth:grant-type:token-exchange",
      subject_token: session.accessToken,
      subject_token_type: "urn:ietf:params:oauth:token-type:access_token",
      requested_token_type: "urn:ietf:params:oauth:token-type:jwt",
      audience: trimmedAudience,
      scope: [
        "openid",
        "profile",
        "email",
        "urn:zitadel:iam:user:resourceowner",
        `urn:zitadel:iam:org:project:id:${trimmedAudience}:aud`,
        "urn:zitadel:iam:org:projects:roles",
      ].join(" "),
    }),
  );
  if (!tokens.access_token || tokens.token_type.toLowerCase() !== "bearer") {
    throw new Error("OIDC token exchange did not return a bearer access token");
  }

  const expiresAt = await verifyAccessTokenExpiresAt(
    metadata,
    tokens.access_token,
    trimmedAudience,
    tokens.expires_in,
  );

  await writeStoredResourceToken(config, session.sessionID, trimmedAudience, tokens, expiresAt);
  return tokens.access_token;
}

function createAnonymousAuth(): AnonymousAuth {
  return anonymousAuth;
}

function toAuth(session: AuthSession | null): Auth {
  if (!session) {
    return createAnonymousAuth();
  }

  return {
    isAuthenticated: true,
    userId: session.user.sub,
    orgId: session.user.orgID,
    roles: session.user.roles,
    roleAssignments: session.user.roleAssignments,
    cachePartition: session.clientCachePartition,
  };
}

function toSessionInfo(session: AuthSession | null): SessionInfo | null {
  if (!session) {
    return null;
  }

  return {
    createdAt: session.createdAt,
    expiresAt: session.expiresAt,
  };
}

async function resolveAuthSnapshot(config: AuthConfigSource): Promise<ResolvedAuthSnapshot> {
  const session = await getAuthSession(await resolveAuthConfig(config));
  const authState = toAuth(session);
  const user = session ? toClientUser(session.user) : null;
  const snapshot = parseAuthSnapshot({
    isSignedIn: authState.isAuthenticated,
    auth: authState,
    user,
    session: toSessionInfo(session),
  });

  return {
    ...snapshot,
    currentUser: session?.user ?? null,
  };
}

// Server-side auth read for routing, authorization, and cache partitioning.
// This is a small projection over the persisted web session.
export async function auth(config: AuthConfigSource): Promise<Auth> {
  return (await resolveAuthSnapshot(config)).auth;
}

// Server-only authenticated user snapshot. This includes claims and should not
// be serialized wholesale to the client.
export async function currentUser(config: AuthConfigSource): Promise<CurrentUser | null> {
  return (await resolveAuthSnapshot(config)).currentUser;
}

// Client-safe session timing metadata derived from the persisted web session.
export async function currentSession(config: AuthConfigSource): Promise<SessionInfo | null> {
  return (await resolveAuthSnapshot(config)).session;
}

// Root-loader snapshot. Read once per navigation and seed hooks from the result
// instead of making multiple auth reads from components.
export async function getClientAuthSnapshot(config: AuthConfigSource): Promise<AuthSnapshot> {
  const snapshot = await resolveAuthSnapshot(config);
  if (!snapshot.isSignedIn) {
    return {
      isSignedIn: false,
      auth: snapshot.auth,
      user: null,
      session: null,
    };
  }

  return {
    isSignedIn: true,
    auth: snapshot.auth,
    user: snapshot.user,
    session: snapshot.session,
  };
}

export function createAuthMiddleware(config: AuthConfigSource) {
  // TanStack Start resolves server function handlers from app modules, so keep
  // auth config lazy and server-side to avoid bundling env lookups into the client.
  return createMiddleware({ type: "function" }).server(async ({ next }) => {
    const auth = await getAuthSession(await resolveAuthConfig(config));
    if (!auth) {
      throw new Error("Authentication required");
    }
    return next({
      context: {
        auth,
      } satisfies { auth: AuthSession },
    });
  });
}

export async function requireAccessToken(config: AuthConfigSource): Promise<string> {
  const session = await getAuthSession(await resolveAuthConfig(config));
  if (!session) {
    throw new Error("Authentication required");
  }
  return session.accessToken;
}

export async function logout(config: AuthConfigSource): Promise<string> {
  const resolvedConfig = await resolveAuthConfig(config);
  const sessionManager = await getSessionManager(resolvedConfig);
  const sessionID = sessionManager.data.sessionID;
  const stored = sessionID ? await readStoredSession(resolvedConfig, sessionID) : null;
  if (sessionID) {
    await deleteStoredSession(resolvedConfig, sessionID);
  }
  await sessionManager.clear();

  const postLogoutRedirect = await getAbsoluteURL(resolvedConfig.postLogoutRedirectPath);
  if (!stored?.idToken) {
    return postLogoutRedirect;
  }

  const metadata = await getProviderMetadata(resolvedConfig.issuerURL);
  if (!metadata.end_session_endpoint) {
    return postLogoutRedirect;
  }

  const logoutURL = new URL(metadata.end_session_endpoint);
  logoutURL.searchParams.set("id_token_hint", stored.idToken);
  logoutURL.searchParams.set("post_logout_redirect_uri", postLogoutRedirect);
  return logoutURL.toString();
}
