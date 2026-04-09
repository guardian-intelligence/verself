import { decodeJwt, createRemoteJWKSet, jwtVerify, type JWTPayload } from "jose";
import { useSession, getRequestUrl } from "@tanstack/react-start/server";
import postgres from "postgres";

export interface AuthUser {
  sub: string;
  email: string | null;
  name: string | null;
  preferredUsername: string | null;
  orgID: string | null;
  roles: string[];
  claims: Record<string, unknown>;
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

interface TokenResponse {
  access_token: string;
  token_type: string;
  expires_in: number;
  refresh_token?: string;
  id_token?: string;
  scope?: string;
}

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
    sessionPassword: requiredNonEmpty(
      config.sessionPassword,
      `${config.appName} sessionPassword`,
    ),
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
    metadataPromise = fetch(
      new URL("/.well-known/openid-configuration", issuerURL).toString(),
      {
        headers: { Accept: "application/json" },
      },
    ).then(async (response) => {
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
  return new URL(pathname, baseURL).toString();
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
  const response = await fetch(metadata.token_endpoint, {
    method: "POST",
    headers: {
      Accept: "application/json",
      "Content-Type": "application/x-www-form-urlencoded",
    },
    body: params.toString(),
  });
  if (!response.ok) {
    throw new Error(
      `OIDC token exchange failed: ${response.status} ${await response.text()}`,
    );
  }
  return response.json() as Promise<TokenResponse>;
}

function extractRoles(payload: JWTPayload): string[] {
  const candidate = payload["urn:zitadel:iam:org:project:roles"];
  if (!candidate || typeof candidate !== "object") {
    return [];
  }
  return Object.keys(candidate as Record<string, unknown>).sort();
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
      : preferredUsername ?? email ?? idTokenClaims.sub ?? null;
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
      claims: row.user_claims ?? {},
    },
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
): Promise<AuthSession | null> {
  if (!stored.refreshToken) {
    return null;
  }

  const metadata = await getProviderMetadata(config.issuerURL);
  const refreshed = await exchangeToken(
    metadata,
    config,
    new URLSearchParams({
      grant_type: "refresh_token",
      refresh_token: stored.refreshToken,
    }),
  ).catch(() => null);

  if (!refreshed?.id_token) {
    return null;
  }

  const verifiedIDToken = await jwtVerify(refreshed.id_token, getJWKS(metadata), {
    issuer: metadata.issuer,
    audience: config.clientID,
  });
  const accessTokenClaims = decodeJwt(refreshed.access_token);
  const user = buildUserSnapshot(verifiedIDToken.payload, accessTokenClaims);
  await writeStoredSession(config, stored.sessionID, refreshed, user);
  return readStoredSession(config, stored.sessionID);
}

export async function beginLogin(
  config: AuthConfig,
  requestedRedirectTo?: string | null,
): Promise<Response> {
  const metadata = await getProviderMetadata(config.issuerURL);
  const session = await getSessionManager(config);
  const state = randomToken();
  const nonce = randomToken();
  const codeVerifier = randomToken(48);
  const redirectTo = sanitizeRedirectTarget(
    requestedRedirectTo,
    config.defaultRedirectPath,
  );
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

  return Response.redirect(authorizeURL.toString(), 302);
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
    redirectTo: pending.redirectTo,
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
    if (!refreshed) {
      await deleteStoredSession(config, stored.sessionID);
      await session.clear();
      return null;
    }
    return refreshed;
  }

  return stored;
}

export async function getAuthUser(config: AuthConfig): Promise<AuthUser | null> {
  const session = await getAuthSession(config);
  return session?.user ?? null;
}

export async function requireAccessToken(config: AuthConfig): Promise<string> {
  const session = await getAuthSession(config);
  if (!session) {
    throw new Error("Authentication required");
  }
  return session.accessToken;
}

export async function logout(config: AuthConfig): Promise<Response> {
  const sessionManager = await getSessionManager(config);
  const sessionID = sessionManager.data.sessionID;
  const stored = sessionID ? await readStoredSession(config, sessionID) : null;
  if (sessionID) {
    await deleteStoredSession(config, sessionID);
  }
  await sessionManager.clear();

  const postLogoutRedirect = getAbsoluteURL(config.postLogoutRedirectPath);
  if (!stored?.idToken) {
    return Response.redirect(postLogoutRedirect, 302);
  }

  const metadata = await getProviderMetadata(config.issuerURL);
  if (!metadata.end_session_endpoint) {
    return Response.redirect(postLogoutRedirect, 302);
  }

  const logoutURL = new URL(metadata.end_session_endpoint);
  logoutURL.searchParams.set("id_token_hint", stored.idToken);
  logoutURL.searchParams.set("post_logout_redirect_uri", postLogoutRedirect);
  return Response.redirect(logoutURL.toString(), 302);
}

export const authSessionSchemaSQL = `
CREATE TABLE IF NOT EXISTS auth_sessions (
  session_id TEXT PRIMARY KEY,
  app_name TEXT NOT NULL,
  subject TEXT NOT NULL,
  email TEXT,
  display_name TEXT,
  preferred_username TEXT,
  org_id TEXT,
  roles JSONB NOT NULL DEFAULT '[]'::jsonb,
  user_claims JSONB NOT NULL DEFAULT '{}'::jsonb,
  id_token TEXT,
  access_token TEXT NOT NULL,
  refresh_token TEXT,
  token_scope TEXT,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS auth_sessions_app_subject_idx
  ON auth_sessions (app_name, subject);
`;
