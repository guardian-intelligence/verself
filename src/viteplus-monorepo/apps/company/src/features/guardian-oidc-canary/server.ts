import {
  GUARDIAN_OIDC_CANARY_CALLBACK_PATH,
  GUARDIAN_OIDC_CANARY_COOKIE,
  GUARDIAN_OIDC_CANARY_COOKIE_TTL_SECONDS,
  GUARDIAN_OIDC_CANARY_ISSUER,
  GUARDIAN_OIDC_CANARY_PATH,
  type GuardianOidcCanaryStatus,
} from "./constants";

type CanarySession = {
  readonly state: string;
  readonly nonce: string;
  readonly verifier: string;
  readonly issuedAt: number;
};

const base64URLAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";

export async function createGuardianAuthorizationRedirect(request: Request): Promise<Response> {
  const clientID = process.env.GUARDIAN_OIDC_CLIENT_ID?.trim();
  if (!clientID) {
    return canaryStatusRedirect("missing_client_registration");
  }

  const issuer = absoluteURL(
    process.env.GUARDIAN_OIDC_ISSUER?.trim() || GUARDIAN_OIDC_CANARY_ISSUER,
    "GUARDIAN_OIDC_ISSUER",
  );
  const redirectURI = new URL(
    GUARDIAN_OIDC_CANARY_CALLBACK_PATH,
    explicitOrigin(process.env.GUARDIAN_OIDC_REDIRECT_ORIGIN) ?? requestOrigin(request),
  );
  const session: CanarySession = {
    state: randomHex(32),
    nonce: randomHex(32),
    verifier: randomHex(32),
    issuedAt: Date.now(),
  };
  const challenge = await pkceChallenge(session.verifier);
  const authorizationURL = new URL("/oauth/v2/authorize", issuer);

  authorizationURL.searchParams.set("client_id", clientID);
  authorizationURL.searchParams.set("redirect_uri", redirectURI.toString());
  authorizationURL.searchParams.set("response_type", "code");
  authorizationURL.searchParams.set("scope", "openid profile email");
  authorizationURL.searchParams.set("state", session.state);
  authorizationURL.searchParams.set("nonce", session.nonce);
  authorizationURL.searchParams.set("code_challenge", challenge);
  authorizationURL.searchParams.set("code_challenge_method", "S256");

  return new Response(null, {
    status: 303,
    headers: {
      location: authorizationURL.toString(),
      "set-cookie": sessionCookie(session, redirectURI.protocol === "https:"),
    },
  });
}

export function handleGuardianAuthorizationCallback(request: Request): Response {
  const url = new URL(request.url);
  const providerError = normalizedSearchParam(url, "error");
  if (providerError) {
    return canaryStatusRedirect("provider_error", providerError, clearSessionCookie(request));
  }

  const code = normalizedSearchParam(url, "code");
  const state = normalizedSearchParam(url, "state");
  if (!code || !state) {
    return canaryStatusRedirect("missing_response", undefined, clearSessionCookie(request));
  }

  const session = readSessionCookie(request);
  if (!session) {
    return canaryStatusRedirect("invalid_canary_cookie", undefined, clearSessionCookie(request));
  }
  if (session.state !== state) {
    return canaryStatusRedirect("state_mismatch", undefined, clearSessionCookie(request));
  }

  return canaryStatusRedirect("authorization_response", undefined, clearSessionCookie(request));
}

function canaryStatusRedirect(
  status: GuardianOidcCanaryStatus,
  reason?: string,
  cookie?: string,
): Response {
  const location = new URL(GUARDIAN_OIDC_CANARY_PATH, "https://guardianintelligence.org");
  location.searchParams.set("status", status);
  if (reason) {
    location.searchParams.set("reason", reason.slice(0, 80));
  }

  const headers: Record<string, string> = {
    location: `${location.pathname}${location.search}`,
  };
  if (cookie) {
    headers["set-cookie"] = cookie;
  }

  return new Response(null, {
    status: 303,
    headers,
  });
}

function readSessionCookie(request: Request): CanarySession | undefined {
  const cookieHeader = request.headers.get("cookie") ?? "";
  const raw = cookieHeader
    .split(";")
    .map((part) => part.trim())
    .find((part) => part.startsWith(`${GUARDIAN_OIDC_CANARY_COOKIE}=`))
    ?.slice(GUARDIAN_OIDC_CANARY_COOKIE.length + 1);
  if (!raw) return undefined;

  try {
    const parsed = JSON.parse(decodeURIComponent(raw)) as Partial<CanarySession>;
    if (
      typeof parsed.state !== "string" ||
      typeof parsed.nonce !== "string" ||
      typeof parsed.verifier !== "string" ||
      typeof parsed.issuedAt !== "number"
    ) {
      return undefined;
    }
    if (Date.now() - parsed.issuedAt > GUARDIAN_OIDC_CANARY_COOKIE_TTL_SECONDS * 1000) {
      return undefined;
    }
    return {
      state: parsed.state,
      nonce: parsed.nonce,
      verifier: parsed.verifier,
      issuedAt: parsed.issuedAt,
    };
  } catch {
    return undefined;
  }
}

function sessionCookie(session: CanarySession, secure: boolean): string {
  const encoded = encodeURIComponent(JSON.stringify(session));
  return [
    `${GUARDIAN_OIDC_CANARY_COOKIE}=${encoded}`,
    `Path=${GUARDIAN_OIDC_CANARY_PATH}`,
    `Max-Age=${GUARDIAN_OIDC_CANARY_COOKIE_TTL_SECONDS}`,
    "HttpOnly",
    secure ? "Secure" : undefined,
    "SameSite=Lax",
  ]
    .filter(Boolean)
    .join("; ");
}

function clearSessionCookie(request: Request): string {
  return [
    `${GUARDIAN_OIDC_CANARY_COOKIE}=`,
    `Path=${GUARDIAN_OIDC_CANARY_PATH}`,
    "Expires=Thu, 01 Jan 1970 00:00:00 GMT",
    "Max-Age=0",
    "HttpOnly",
    requestOrigin(request).protocol === "https:" ? "Secure" : undefined,
    "SameSite=Lax",
  ]
    .filter(Boolean)
    .join("; ");
}

function normalizedSearchParam(url: URL, key: string): string | undefined {
  const value = url.searchParams.get(key)?.trim();
  return value ? value : undefined;
}

function requestOrigin(request: Request): URL {
  const forwardedHost = firstHeaderValue(request.headers.get("x-forwarded-host"));
  const forwardedProto = firstHeaderValue(request.headers.get("x-forwarded-proto"));
  if (forwardedHost && forwardedProto) {
    return absoluteURL(`${forwardedProto}://${forwardedHost}`, "forwarded origin");
  }
  const url = new URL(request.url);
  return absoluteURL(url.origin, "request origin");
}

function firstHeaderValue(value: string | null): string | undefined {
  return value
    ?.split(",")
    .map((part) => part.trim())
    .find(Boolean);
}

function explicitOrigin(value: string | undefined): URL | undefined {
  const trimmed = value?.trim();
  return trimmed ? absoluteURL(trimmed, "GUARDIAN_OIDC_REDIRECT_ORIGIN") : undefined;
}

function absoluteURL(value: string, label: string): URL {
  try {
    const url = new URL(value);
    if (!url.protocol || !url.hostname) {
      throw new Error("missing protocol or hostname");
    }
    return url;
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    throw new Error(`${label} must be an absolute URL: ${message}`);
  }
}

function randomHex(bytes: number): string {
  const values = new Uint8Array(bytes);
  globalThis.crypto.getRandomValues(values);
  return Array.from(values, (value) => value.toString(16).padStart(2, "0")).join("");
}

async function pkceChallenge(verifier: string): Promise<string> {
  const digest = await globalThis.crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(verifier),
  );
  return base64URL(new Uint8Array(digest));
}

function base64URL(bytes: Uint8Array): string {
  let out = "";
  for (let i = 0; i < bytes.length; i += 3) {
    const a = bytes[i] ?? 0;
    const b = bytes[i + 1] ?? 0;
    const c = bytes[i + 2] ?? 0;
    const chunk = (a << 16) | (b << 8) | c;
    out += base64URLAlphabet[(chunk >> 18) & 63];
    out += base64URLAlphabet[(chunk >> 12) & 63];
    if (i + 1 < bytes.length) {
      out += base64URLAlphabet[(chunk >> 6) & 63];
    }
    if (i + 2 < bytes.length) {
      out += base64URLAlphabet[chunk & 63];
    }
  }
  return out;
}
