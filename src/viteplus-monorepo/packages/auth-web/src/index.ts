import { createMiddleware, createServerFn } from "@tanstack/react-start";
import { decodeJwt, createRemoteJWKSet, jwtVerify, type JWTPayload } from "jose";
import { useSession, getRequestUrl } from "@tanstack/react-start/server";
import postgres from "postgres";

export interface AuthUser {
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

export interface AuthRoleAssignment {
  projectID: string | null;
  orgID: string;
  orgName: string | null;
  role: string;
}

export interface AuthConfig {
  appName: string;
  issuerURL: string;
  clientID: string;
  clientSecret?: string;
  sessionCookieName?: string;
  sessionDatabaseURL: string;
  sessionPassword: string;
  sessionMaxAgeSeconds?: number;
  refreshLeewaySeconds?: number;
  scopes: string[];
  callbackPath: string;
  defaultRedirectPath: string;
  postLogoutRedirectPath: string;
}

interface ProviderMetadata {
  issuer: string;
  authorization_endpoint: string;
  token_endpoint: string;
  jwks_uri: string;
  end_session_endpoint?: string;
}

interface PendingLoginState {
  state: string;
  nonce: string;
  codeVerifier: string;
  redirectTo: string;
  createdAt: number;
}

interface AuthCookieData {
  sessionID?: string;
  login?: PendingLoginState;
}

interface StoredAuthSessionRow {
  session_id: string;
  app_name: string;
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

export interface AuthSession {
  sessionID: string;
  accessToken: string;
  refreshToken: string | null;
  idToken: string | null;
  scope: string | null;
  expiresAt: Date;
  user: AuthUser;
  createdAt: Date;
  updatedAt: Date;
}

export interface AuthViewer {
  sub: string;
  email: string | null;
  name: string | null;
  preferredUsername: string | null;
  orgID: string | null;
  roles: string[];
  roleAssignments: AuthRoleAssignment[];
}

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

const metadataCache = new Map<string, Promise<ProviderMetadata>>();
const jwksCache = new Map<string, ReturnType<typeof createRemoteJWKSet>>();
const sqlCache = new Map<string, postgres.Sql<Record<string, unknown>>>();

function requiredNonEmpty(value: string | undefined, label: string): string {
  const trimmed = value?.trim();
  if (!trimmed) {
    throw new Error(`${label} is required`);
  }
  return trimmed;
}

export function createAuthConfig(config: AuthConfig): AuthConfig {
  return {
    ...config,
    issuerURL: requiredNonEmpty(config.issuerURL, `${config.appName} issuerURL`),
    clientID: requiredNonEmpty(config.clientID, `${config.appName} clientID`),
    sessionDatabaseURL: requiredNonEmpty(
      config.sessionDatabaseURL,
      `${config.appName} sessionDatabaseURL`,
    ),
    sessionPassword: requiredNonEmpty(config.sessionPassword, `${config.appName} sessionPassword`),
    sessionCookieName: config.sessionCookieName ?? `${config.appName}-session`,
    sessionMaxAgeSeconds: config.sessionMaxAgeSeconds ?? 60 * 60 * 24 * 30,
    refreshLeewaySeconds: config.refreshLeewaySeconds ?? 60,
  };
}

function getSQL(databaseURL: string) {
  let sql = sqlCache.get(databaseURL);
  if (!sql) {
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

function getBaseURL(): URL {
  return new URL(getRequestUrl({ xForwardedHost: true, xForwardedProto: true }).toString());
}

function getAbsoluteURL(pathname: string): string {
  const baseURL = getBaseURL();
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

function randomToken(bytes = 32): string {
  return Buffer.from(crypto.getRandomValues(new Uint8Array(bytes))).toString("base64url");
}

async function codeChallenge(verifier: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(verifier));
  return Buffer.from(digest).toString("base64url");
}

function sanitizeRedirectTarget(redirectTo: string | null | undefined, fallback: string): string {
  if (!redirectTo) return fallback;
  try {
    const baseURL = getBaseURL();
    const parsed = new URL(redirectTo, baseURL);
    if (parsed.origin !== baseURL.origin) {
      return fallback;
    }
    return `${parsed.pathname}${parsed.search}${parsed.hash}`;
  } catch {
    return fallback;
  }
}

function getSessionManager(config: AuthConfig) {
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

function buildUserSnapshot(idTokenClaims: JWTPayload, accessTokenClaims: JWTPayload): AuthUser {
  const email =
    typeof idTokenClaims.email === "string"
      ? idTokenClaims.email
      : typeof accessTokenClaims.email === "string"
        ? accessTokenClaims.email
        : null;
  const preferredUsername =
    typeof idTokenClaims.preferred_username === "string"
      ? idTokenClaims.preferred_username
      : typeof accessTokenClaims.preferred_username === "string"
        ? accessTokenClaims.preferred_username
        : null;
  const name =
    typeof idTokenClaims.name === "string"
      ? idTokenClaims.name
      : (preferredUsername ?? email ?? idTokenClaims.sub ?? null);
  const orgID =
    typeof accessTokenClaims["urn:zitadel:iam:user:resourceowner:id"] === "string"
      ? (accessTokenClaims["urn:zitadel:iam:user:resourceowner:id"] as string)
      : null;

  return {
    sub: idTokenClaims.sub ?? "",
    email,
    name,
    preferredUsername,
    orgID,
    roles: extractRoles(accessTokenClaims),
    roleAssignments: extractRoleAssignments(accessTokenClaims),
    claims: {
      ...accessTokenClaims,
      ...idTokenClaims,
    },
  };
}

function rowToAuthSession(row: StoredAuthSessionRow): AuthSession {
  return {
    sessionID: row.session_id,
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

function toAuthViewer(user: AuthUser): AuthViewer {
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
  const sql = getSQL(config.sessionDatabaseURL);
  const [row] = await sql<StoredAuthSessionRow[]>`
    SELECT
      session_id,
      app_name,
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
  tokens: TokenResponse,
  user: AuthUser,
): Promise<void> {
  const sql = getSQL(config.sessionDatabaseURL);
  const expiresAt = new Date(Date.now() + tokens.expires_in * 1000);
  await sql`
    INSERT INTO auth_sessions (
      session_id,
      app_name,
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
      ${user.sub},
      ${user.email},
      ${user.name},
      ${user.preferredUsername},
      ${user.orgID},
      ${JSON.stringify(user.roles)},
      ${JSON.stringify(user.claims)},
      ${tokens.id_token ?? null},
      ${tokens.access_token},
      ${tokens.refresh_token ?? null},
      ${tokens.scope ?? null},
      ${expiresAt.toISOString()}
    )
    ON CONFLICT (session_id) DO UPDATE SET
      app_name = EXCLUDED.app_name,
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
  const sql = getSQL(config.sessionDatabaseURL);
  await sql`
    DELETE FROM auth_sessions
    WHERE app_name = ${config.appName}
      AND session_id = ${sessionID}
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
  const user = buildUserSnapshot(verifiedIDToken.payload, accessTokenClaims);
  await writeStoredSession(config, stored.sessionID, refreshed, user);
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
  const state = randomToken();
  const nonce = randomToken();
  const codeVerifier = randomToken(48);
  const redirectTo = sanitizeRedirectTarget(requestedRedirectTo, config.defaultRedirectPath);
  const authorizeURL = new URL(metadata.authorization_endpoint);
  authorizeURL.searchParams.set("client_id", config.clientID);
  authorizeURL.searchParams.set("redirect_uri", getAbsoluteURL(config.callbackPath));
  authorizeURL.searchParams.set("response_type", "code");
  authorizeURL.searchParams.set("scope", config.scopes.join(" "));
  authorizeURL.searchParams.set("state", state);
  authorizeURL.searchParams.set("nonce", nonce);
  authorizeURL.searchParams.set("code_challenge", await codeChallenge(codeVerifier));
  authorizeURL.searchParams.set("code_challenge_method", "S256");

  await session.clear();
  await session.update({
    login: {
      state,
      nonce,
      codeVerifier,
      redirectTo,
      createdAt: Date.now(),
    },
  });

  return authorizeURL.toString();
}

export async function finishLogin(config: AuthConfig): Promise<{
  redirectTo: string;
  session: AuthSession;
}> {
  const requestURL = getBaseURL();
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
  const pending = session.data.login;
  if (!pending) {
    throw new Error("OIDC callback is missing login transaction state");
  }
  if (!Number.isFinite(pending.createdAt) || Date.now() - pending.createdAt > pendingLoginTTL) {
    await session.clear();
    throw new Error("OIDC callback login transaction expired");
  }
  if (pending.state !== state) {
    throw new Error("OIDC callback state mismatch");
  }

  const metadata = await getProviderMetadata(config.issuerURL);
  const tokens = await exchangeToken(
    metadata,
    config,
    new URLSearchParams({
      grant_type: "authorization_code",
      code,
      redirect_uri: getAbsoluteURL(config.callbackPath),
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
  const user = buildUserSnapshot(verifiedIDToken.payload, accessTokenClaims);
  const sessionID = crypto.randomUUID();

  await writeStoredSession(config, sessionID, tokens, user);
  await session.clear();
  await session.update({ sessionID });

  const storedSession = await readStoredSession(config, sessionID);
  if (!storedSession) {
    throw new Error("Auth session was not persisted");
  }

  return {
    redirectTo: new URL(pending.redirectTo, getBaseURL()).toString(),
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

export async function getAuthUser(config: AuthConfig): Promise<AuthUser | null> {
  const session = await getAuthSession(config);
  return session?.user ?? null;
}

export function createAuthServerFns(config: AuthConfig) {
  const authMiddleware = createMiddleware({ type: "function" }).server(async ({ next }) => {
    const auth = await getAuthSession(config);
    if (!auth) {
      throw new Error("Authentication required");
    }
    return next({
      context: {
        auth,
      } satisfies { auth: AuthSession },
    });
  });

  const getViewer = createServerFn({ method: "GET" }).handler(async () => {
    const user = await getAuthUser(config);
    return user ? toAuthViewer(user) : null;
  });

  const getLoginRedirectURL = createServerFn({ method: "GET" })
    .inputValidator((data: { redirectTo?: string | null }) => data)
    .handler(async ({ data }) => beginLogin(config, data.redirectTo));

  const getCallbackRedirectURL = createServerFn({ method: "GET" }).handler(async () => {
    const { redirectTo } = await finishLogin(config);
    return redirectTo;
  });

  const getLogoutRedirectURL = createServerFn({ method: "GET" }).handler(async () => {
    return logout(config);
  });

  return {
    authMiddleware,
    getViewer,
    getLoginRedirectURL,
    getCallbackRedirectURL,
    getLogoutRedirectURL,
  };
}

export async function requireAccessToken(config: AuthConfig): Promise<string> {
  const session = await getAuthSession(config);
  if (!session) {
    throw new Error("Authentication required");
  }
  return session.accessToken;
}

export async function logout(config: AuthConfig): Promise<string> {
  const sessionManager = await getSessionManager(config);
  const sessionID = sessionManager.data.sessionID;
  const stored = sessionID ? await readStoredSession(config, sessionID) : null;
  if (sessionID) {
    await deleteStoredSession(config, sessionID);
  }
  await sessionManager.clear();

  const postLogoutRedirect = getAbsoluteURL(config.postLogoutRedirectPath);
  if (!stored?.idToken) {
    return postLogoutRedirect;
  }

  const metadata = await getProviderMetadata(config.issuerURL);
  if (!metadata.end_session_endpoint) {
    return postLogoutRedirect;
  }

  const logoutURL = new URL(metadata.end_session_endpoint);
  logoutURL.searchParams.set("id_token_hint", stored.idToken);
  logoutURL.searchParams.set("post_logout_redirect_uri", postLogoutRedirect);
  return logoutURL.toString();
}
