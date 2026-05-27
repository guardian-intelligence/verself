import { createHash } from "node:crypto";
import { setResponseHeader } from "@tanstack/react-start/server";
import * as v from "valibot";
import { requireURLFromEnv } from "@verself/web-env";
import {
  parseAuthSnapshot,
  type AuthenticatedAuthSnapshot,
  type AuthSnapshot,
} from "@verself/auth-web/isomorphic";
import {
  passwordLoginErrorCodeFromProblem,
  type PasswordLoginResult,
} from "../features/auth/auth-errors";
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
  loginIntent: v.optional(
    v.object({
      loginUrl: v.pipe(v.string(), v.nonEmpty()),
      purpose: v.pipe(v.string(), v.nonEmpty()),
      requiredSubject: v.pipe(v.string(), v.nonEmpty()),
      requiredEmail: v.pipe(v.string(), v.nonEmpty()),
      requiredOrgId: v.pipe(v.string(), v.nonEmpty()),
      redirectTo: v.optional(v.pipe(v.string(), v.nonEmpty())),
    }),
  ),
});

export type SignupVerificationResult = v.InferOutput<typeof signupVerificationResultSchema>;

const signupStartResultSchema = v.object({
  message: v.pipe(v.string(), v.nonEmpty()),
  status: v.pipe(v.string(), v.nonEmpty()),
  verificationExpiresAt: v.pipe(v.string(), v.nonEmpty()),
});

export type SignupStartResult = v.InferOutput<typeof signupStartResultSchema>;

const organizationSlugAvailabilitySchema = v.object({
  slug: v.pipe(v.string(), v.nonEmpty()),
  available: v.boolean(),
});

export type OrganizationSlugAvailability = v.InferOutput<typeof organizationSlugAvailabilitySchema>;

const memberInviteAcceptanceResultSchema = v.object({
  orgId: v.pipe(v.string(), v.nonEmpty()),
  memberId: v.pipe(v.string(), v.nonEmpty()),
  resourceName: v.pipe(v.string(), v.nonEmpty()),
  loginUrl: v.pipe(v.string(), v.nonEmpty()),
  loginIntent: v.optional(
    v.object({
      loginUrl: v.pipe(v.string(), v.nonEmpty()),
      purpose: v.pipe(v.string(), v.nonEmpty()),
      requiredSubject: v.pipe(v.string(), v.nonEmpty()),
      requiredEmail: v.pipe(v.string(), v.nonEmpty()),
      requiredOrgId: v.pipe(v.string(), v.nonEmpty()),
      redirectTo: v.optional(v.pipe(v.string(), v.nonEmpty())),
    }),
  ),
});

export type MemberInviteAcceptanceResult = v.InferOutput<typeof memberInviteAcceptanceResultSchema>;

const passwordLoginSuccessResultSchema = v.object({
  callbackUrl: v.pipe(v.string(), v.nonEmpty()),
});

export type { PasswordLoginResult };

const nullableStringSchema = v.union([v.null_(), v.string()]);

const browserAccountOrganizationSchema = v.object({
  orgID: v.pipe(v.string(), v.nonEmpty()),
  identityProviderOrgID: v.string(),
});

const browserAccountSummarySchema = v.object({
  accountHandle: v.pipe(v.string(), v.nonEmpty()),
  isCurrent: v.boolean(),
  subject: v.pipe(v.string(), v.nonEmpty()),
  email: nullableStringSchema,
  displayName: nullableStringSchema,
  preferredUsername: nullableStringSchema,
  homeOrgID: nullableStringSchema,
  selectedOrgID: nullableStringSchema,
  availableOrganizations: v.array(browserAccountOrganizationSchema),
});

const browserAccountsResponseSchema = v.object({
  accounts: v.array(browserAccountSummarySchema),
});

export type BrowserAccountSummary = v.InferOutput<typeof browserAccountSummarySchema>;

const passwordResetResultSchema = v.object({
  status: v.pipe(v.string(), v.nonEmpty()),
  message: v.pipe(v.string(), v.nonEmpty()),
});

export type PasswordResetResult = v.InferOutput<typeof passwordResetResultSchema>;

const deviceLoginResultSchema = v.object({
  deviceLoginHandle: v.pipe(v.string(), v.nonEmpty()),
  userCodeSuffix: v.string(),
  clientId: v.string(),
  appName: v.string(),
  projectName: v.string(),
  scopes: v.array(v.string()),
  state: v.pipe(v.string(), v.nonEmpty()),
  approvedAt: v.string(),
  deniedAt: v.string(),
  expiresAt: v.pipe(v.string(), v.nonEmpty()),
  updatedAt: v.pipe(v.string(), v.nonEmpty()),
});

export type DeviceLoginResult = v.InferOutput<typeof deviceLoginResultSchema>;

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

export async function selectIdentityBrowserAccount(accountHandle: string): Promise<AuthSnapshot> {
  const response = await identityAuthFetch("accounts", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ accountHandle }),
  });
  if (!response.ok) {
    throw new Error(`identity account switch failed: ${response.status} ${await response.text()}`);
  }
  return parseAuthSnapshot(await response.json());
}

export async function listIdentityBrowserAccounts(): Promise<Array<BrowserAccountSummary>> {
  const response = await identityAuthFetch("accounts");
  if (!response.ok) {
    if (response.status === 401) {
      return [];
    }
    throw new Error(`identity account list failed: ${response.status} ${await response.text()}`);
  }
  return v.parse(browserAccountsResponseSchema, await response.json()).accounts;
}

export async function removeIdentityBrowserAccount(accountHandle: string): Promise<void> {
  const response = await identityAuthFetch(`accounts/${encodeURIComponent(accountHandle)}`, {
    method: "DELETE",
  });
  if (!response.ok) {
    throw new Error(`identity account remove failed: ${response.status} ${await response.text()}`);
  }
}

export async function revokeIdentityBrowserDevice(sessionHandle: string): Promise<void> {
  const response = await identityAuthFetch(`sessions/${encodeURIComponent(sessionHandle)}`, {
    method: "DELETE",
  });
  if (!response.ok) {
    throw new Error(`identity device revoke failed: ${response.status} ${await response.text()}`);
  }
}

export async function acceptIdentityMemberInvite(data: {
  token: string;
}): Promise<MemberInviteAcceptanceResult> {
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
  return v.parse(memberInviteAcceptanceResultSchema, await response.json());
}

export async function completeIdentityPasswordLogin(data: {
  email: string;
  password: string;
  authRequestId?: string | undefined;
  redirectTo?: string | undefined;
  purpose?: string | undefined;
  loginHint?: string | undefined;
  requiredSubject?: string | undefined;
  requiredEmail?: string | undefined;
  requiredOrgId?: string | undefined;
  prompt?: "login" | "select_account" | undefined;
}): Promise<PasswordLoginResult> {
  const response = await identityAuthFetch("password-login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      email: data.email,
      password: data.password,
      authRequestId: data.authRequestId ?? "",
      redirectTo: data.redirectTo ?? "",
      purpose: data.purpose ?? "",
      loginHint: data.loginHint ?? "",
      requiredSubject: data.requiredSubject ?? "",
      requiredEmail: data.requiredEmail ?? "",
      requiredOrgId: data.requiredOrgId ?? "",
      prompt: data.prompt ?? "",
    }),
  });
  if (!response.ok) {
    return {
      ok: false,
      code: passwordLoginErrorCodeFromProblem(response.status, await response.text()),
    };
  }
  return { ok: true, ...v.parse(passwordLoginSuccessResultSchema, await response.json()) };
}

export async function startIdentityPasswordReset(email: string): Promise<PasswordResetResult> {
  const response = await identityAuthFetch("password-reset", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email }),
  });
  if (!response.ok) {
    throw new Error(passwordResetProblemMessage(response.status, await response.text()));
  }
  return v.parse(passwordResetResultSchema, await response.json());
}

export async function completeIdentityPasswordReset(data: {
  userId: string;
  verificationCode: string;
  password: string;
}): Promise<PasswordResetResult> {
  const response = await identityAuthFetch("password-reset/complete", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(data),
  });
  if (!response.ok) {
    throw new Error(passwordResetProblemMessage(response.status, await response.text()));
  }
  return v.parse(passwordResetResultSchema, await response.json());
}

function passwordResetProblemMessage(status: number, body: string): string {
  let code = "";
  try {
    const problem = JSON.parse(body) as { code?: unknown };
    code = typeof problem.code === "string" ? problem.code : "";
  } catch {
    code = "";
  }
  if (code === "password-rejected") {
    return "Use a longer passphrase that has not appeared in a breach.";
  }
  if (code === "reset-token-invalid") {
    return "This reset link is invalid or expired.";
  }
  if (status === 429) {
    return "Too many attempts. Wait a moment and try again.";
  }
  return "Password reset is unavailable right now.";
}

export async function lookupIdentityDeviceLogin(userCode: string): Promise<DeviceLoginResult> {
  const response = await identityAuthFetch(
    "device-logins/lookup",
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ userCode }),
    },
    { forwardCookies: false },
  );
  if (!response.ok) {
    throw new Error(deviceLoginProblemMessage(response.status, await response.text()));
  }
  return v.parse(deviceLoginResultSchema, await response.json());
}

export async function approveIdentityDeviceLogin(
  deviceLoginHandle: string,
): Promise<DeviceLoginResult> {
  const response = await identityAuthFetch(
    `device-logins/${encodeURIComponent(deviceLoginHandle)}/approval`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: "{}",
    },
  );
  if (!response.ok) {
    throw new Error(deviceLoginProblemMessage(response.status, await response.text()));
  }
  return v.parse(deviceLoginResultSchema, await response.json());
}

export async function denyIdentityDeviceLogin(
  deviceLoginHandle: string,
): Promise<DeviceLoginResult> {
  const response = await identityAuthFetch(
    `device-logins/${encodeURIComponent(deviceLoginHandle)}/denial`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: "{}",
    },
  );
  if (!response.ok) {
    throw new Error(deviceLoginProblemMessage(response.status, await response.text()));
  }
  return v.parse(deviceLoginResultSchema, await response.json());
}

function deviceLoginProblemMessage(status: number, body: string): string {
  let code = "";
  try {
    const problem = JSON.parse(body) as { code?: unknown };
    code = typeof problem.code === "string" ? problem.code : "";
  } catch {
    code = "";
  }
  switch (code) {
    case "device-login-expired":
      return "This device code has expired.";
    case "provider-session-required":
      return "Sign in again before approving this device.";
    case "device-login-not-found":
      return "This device code is not valid.";
    default:
      if (status === 409) return "This device login is no longer pending.";
      return "Device login is unavailable right now.";
  }
}

function inviteAcceptanceIdempotencyKey(token: string): string {
  const digest = createHash("sha256").update(token).digest("base64url").slice(0, 48);
  return `invite-${digest}`;
}

export async function verifyIdentitySignup(data: {
  signupIntentId: string;
  verificationToken: string;
  initialPassword: string;
  organizationDisplayName: string;
  organizationSlug: string;
}): Promise<SignupVerificationResult> {
  const organizationDisplayName = normalizeHumanText(data.organizationDisplayName);
  const organizationSlug = normalizeSlug(data.organizationSlug);
  const response = await identityAPIFetch(
    `signup-intents/${encodeURIComponent(data.signupIntentId)}/verification`,
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Idempotency-Key": signupVerificationIdempotencyKey({
          ...data,
          organizationDisplayName,
          organizationSlug,
        }),
      },
      body: JSON.stringify({
        verificationToken: data.verificationToken,
        initialPassword: data.initialPassword,
        organizationDisplayName,
        organizationSlug,
      }),
    },
    { cookieHeader: undefined, forwardCookies: false },
  );
  if (!response.ok) {
    throw new Error(signupVerificationProblemMessage(response.status, await response.text()));
  }
  return v.parse(signupVerificationResultSchema, await response.json());
}

export async function startIdentitySignup(data: {
  email: string;
  organizationDisplayName: string;
  organizationSlug?: string | undefined;
  givenName?: string | undefined;
  familyName?: string | undefined;
}): Promise<SignupStartResult> {
  const organizationDisplayName = normalizeHumanText(data.organizationDisplayName);
  const organizationSlug = data.organizationSlug ? normalizeSlug(data.organizationSlug) : "";
  const response = await identityAPIFetch(
    "signup-intents",
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Idempotency-Key": signupStartIdempotencyKey({
          email: data.email,
          organizationDisplayName,
          organizationSlug,
          givenName: data.givenName ?? "",
          familyName: data.familyName ?? "",
        }),
      },
      body: JSON.stringify({
        email: data.email,
        organizationDisplayName,
        ...(organizationSlug ? { organizationSlug } : {}),
        ...(data.givenName ? { givenName: normalizeHumanText(data.givenName) } : {}),
        ...(data.familyName ? { familyName: normalizeHumanText(data.familyName) } : {}),
      }),
    },
    { cookieHeader: undefined, forwardCookies: false },
  );
  if (!response.ok) {
    throw new Error(signupStartProblemMessage(response.status, await response.text()));
  }
  return v.parse(signupStartResultSchema, await response.json());
}

function signupStartProblemMessage(status: number, body: string): string {
  let code = "";
  try {
    const problem = JSON.parse(body) as { code?: unknown };
    code = typeof problem.code === "string" ? problem.code : "";
  } catch {
    code = "";
  }
  if (status === 429) {
    return "Too many signup attempts. Wait a moment and try again.";
  }
  if (code === "iam.organization_slug.unavailable") {
    return "That organization URL is already taken.";
  }
  return "Signup is unavailable right now.";
}

function signupStartIdempotencyKey(data: {
  email: string;
  organizationDisplayName: string;
  organizationSlug: string;
  givenName: string;
  familyName: string;
}): string {
  const digest = createHash("sha256")
    .update(data.email.trim().toLowerCase())
    .update("\x00")
    .update(data.organizationDisplayName)
    .update("\x00")
    .update(data.organizationSlug)
    .update("\x00")
    .update(data.givenName)
    .update("\x00")
    .update(data.familyName)
    .digest("base64url")
    .slice(0, 48);
  return `signup-start-${digest}`;
}

function signupVerificationProblemMessage(status: number, body: string): string {
  let code = "";
  try {
    const problem = JSON.parse(body) as { code?: unknown };
    code = typeof problem.code === "string" ? problem.code : "";
  } catch {
    code = "";
  }
  switch (code) {
    case "iam.signup.verification.expired":
      return "This signup link has expired. Request a new signup email.";
    case "iam.signup.verification.invalid":
      return "This signup link is no longer valid. Use the newest signup email.";
    case "iam.signup.verification.already_used":
      return "This signup link has already been used. Sign in with the account that completed signup.";
    case "iam.signup.materializing":
      return "Signup is still being finalized. Wait a moment and try again.";
    case "iam.signup.account_exists":
      return "This email already has a Verself account. Sign in instead.";
    case "iam.organization_slug.unavailable":
      return "That organization URL is already taken.";
    case "iam.password.rejected":
      return "Use a longer passphrase that has not appeared in a breach.";
    default:
      if (status === 409) {
        return "Signup could not be completed because the request state changed. Use the newest signup email and try again.";
      }
      return "Signup could not be completed. Use the newest signup email and try again.";
  }
}

function signupVerificationIdempotencyKey(data: {
  signupIntentId: string;
  verificationToken: string;
  organizationDisplayName: string;
  organizationSlug: string;
}): string {
  const digest = createHash("sha256")
    .update(data.signupIntentId)
    .update("\x00")
    .update(data.verificationToken)
    .update("\x00")
    .update(data.organizationDisplayName)
    .update("\x00")
    .update(data.organizationSlug)
    .digest("base64url")
    .slice(0, 48);
  return `signup-${digest}`;
}

export async function checkIdentityOrganizationSlugAvailability(
  slug: string,
): Promise<OrganizationSlugAvailability> {
  const normalized = normalizeSlug(slug);
  const response = await identityAPIFetch(
    `organization-slugs/${encodeURIComponent(normalized)}/availability`,
    {},
    { cookieHeader: undefined, forwardCookies: false },
  );
  if (!response.ok) {
    throw new Error(`identity organization slug check failed: ${response.status}`);
  }
  return v.parse(organizationSlugAvailabilitySchema, await response.json());
}

function normalizeHumanText(value: string): string {
  return value.trim().replace(/\s+/g, " ");
}

function normalizeSlug(value: string): string {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .replace(/-{2,}/g, "-");
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
