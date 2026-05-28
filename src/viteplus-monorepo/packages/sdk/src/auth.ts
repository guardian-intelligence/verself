import * as v from "valibot";

export const AUTH_ERROR_TAG = {
  requestMethodNotAllowed: "urn:verself:problem:request:method_not_allowed",
  requestBodyTooLarge: "urn:verself:problem:request:body_too_large",
  requestValidationFailed: "urn:verself:problem:request:validation_failed",
  permissionDenied: "urn:verself:problem:auth:permission_denied",
  invalidCredentials: "urn:verself:problem:auth:invalid_credentials",
  loginConstraintFailed: "urn:verself:problem:auth:login_constraint_failed",
  originNotAllowed: "urn:verself:problem:auth:origin_not_allowed",
  providerSessionRequired: "urn:verself:problem:auth:provider_session_required",
  unauthenticated: "urn:verself:problem:auth:unauthenticated",
  reauthenticationRequired: "urn:verself:problem:auth:reauthentication_required",
  accountLinkRequired: "urn:verself:problem:auth:account_link_required",
  providerEmailUnverified: "urn:verself:problem:auth:provider_email_unverified",
  providerSubjectAlreadyLinked: "urn:verself:problem:auth:provider_subject_already_linked",
  sessionRevoked: "urn:verself:problem:auth:session_revoked",
  noAccessibleOrganization: "urn:verself:problem:auth:no_accessible_organization",
  orgSelectionRequired: "urn:verself:problem:auth:org_selection_required",
  accountSelectionRequired: "urn:verself:problem:auth:account_selection_required",
  insufficientAssurance: "urn:verself:problem:auth:insufficient_assurance",
  deviceSessionRequired: "urn:verself:problem:auth:device_session_required",
  organizationUnavailable: "urn:verself:problem:auth:organization_unavailable",
  accountUnavailable: "urn:verself:problem:auth:account_unavailable",
  sessionUnavailable: "urn:verself:problem:auth:session_unavailable",
  oidcCallbackFailed: "urn:verself:problem:auth:oidc_callback_failed",
  deviceLoginExpired: "urn:verself:problem:iam:device_login_expired",
  deviceLoginNotFound: "urn:verself:problem:iam:device_login_not_found",
  deviceLoginNotPending: "urn:verself:problem:iam:device_login_not_pending",
  organizationSlugUnavailable: "urn:verself:problem:iam:organization_slug_unavailable",
  passwordBreached: "urn:verself:problem:iam:password_breached",
  passwordRejected: "urn:verself:problem:iam:password_rejected",
  passwordTooLong: "urn:verself:problem:iam:password_too_long",
  passwordTooShort: "urn:verself:problem:iam:password_too_short",
  passwordResetTokenInvalid: "urn:verself:problem:iam:password_reset_token_invalid",
  signupAccountExists: "urn:verself:problem:iam:signup_account_exists",
  signupMaterializing: "urn:verself:problem:iam:signup_materializing",
  signupStateConflict: "urn:verself:problem:iam:signup_state_conflict",
  signupVerificationAlreadyUsed: "urn:verself:problem:iam:signup_verification_already_used",
  signupVerificationExpired: "urn:verself:problem:iam:signup_verification_expired",
  signupVerificationInvalid: "urn:verself:problem:iam:signup_verification_invalid",
  conflictState: "urn:verself:problem:conflict:state",
  idempotencyPayloadMismatch: "urn:verself:problem:conflict:idempotency_payload_mismatch",
  paymentRequired: "urn:verself:problem:billing:payment_required",
  rateLimited: "urn:verself:problem:quota:rate_limited",
  resourceNotFound: "urn:verself:problem:resource:not_found",
  serviceUnavailable: "urn:verself:problem:service:unavailable",
  unknown: "urn:verself:problem:auth:unknown",
} as const;

export const authErrorTags = [
  AUTH_ERROR_TAG.requestMethodNotAllowed,
  AUTH_ERROR_TAG.requestBodyTooLarge,
  AUTH_ERROR_TAG.requestValidationFailed,
  AUTH_ERROR_TAG.permissionDenied,
  AUTH_ERROR_TAG.invalidCredentials,
  AUTH_ERROR_TAG.loginConstraintFailed,
  AUTH_ERROR_TAG.originNotAllowed,
  AUTH_ERROR_TAG.providerSessionRequired,
  AUTH_ERROR_TAG.unauthenticated,
  AUTH_ERROR_TAG.reauthenticationRequired,
  AUTH_ERROR_TAG.accountLinkRequired,
  AUTH_ERROR_TAG.providerEmailUnverified,
  AUTH_ERROR_TAG.providerSubjectAlreadyLinked,
  AUTH_ERROR_TAG.sessionRevoked,
  AUTH_ERROR_TAG.noAccessibleOrganization,
  AUTH_ERROR_TAG.orgSelectionRequired,
  AUTH_ERROR_TAG.accountSelectionRequired,
  AUTH_ERROR_TAG.insufficientAssurance,
  AUTH_ERROR_TAG.deviceSessionRequired,
  AUTH_ERROR_TAG.organizationUnavailable,
  AUTH_ERROR_TAG.accountUnavailable,
  AUTH_ERROR_TAG.sessionUnavailable,
  AUTH_ERROR_TAG.oidcCallbackFailed,
  AUTH_ERROR_TAG.deviceLoginExpired,
  AUTH_ERROR_TAG.deviceLoginNotFound,
  AUTH_ERROR_TAG.deviceLoginNotPending,
  AUTH_ERROR_TAG.organizationSlugUnavailable,
  AUTH_ERROR_TAG.passwordBreached,
  AUTH_ERROR_TAG.passwordRejected,
  AUTH_ERROR_TAG.passwordTooLong,
  AUTH_ERROR_TAG.passwordTooShort,
  AUTH_ERROR_TAG.passwordResetTokenInvalid,
  AUTH_ERROR_TAG.signupAccountExists,
  AUTH_ERROR_TAG.signupMaterializing,
  AUTH_ERROR_TAG.signupStateConflict,
  AUTH_ERROR_TAG.signupVerificationAlreadyUsed,
  AUTH_ERROR_TAG.signupVerificationExpired,
  AUTH_ERROR_TAG.signupVerificationInvalid,
  AUTH_ERROR_TAG.conflictState,
  AUTH_ERROR_TAG.idempotencyPayloadMismatch,
  AUTH_ERROR_TAG.paymentRequired,
  AUTH_ERROR_TAG.rateLimited,
  AUTH_ERROR_TAG.resourceNotFound,
  AUTH_ERROR_TAG.serviceUnavailable,
] as const;

export type KnownAuthErrorTag = (typeof authErrorTags)[number];

const authErrorTagSet = new Set<string>(authErrorTags);

export function isKnownAuthErrorTag(tag: string): tag is KnownAuthErrorTag {
  return authErrorTagSet.has(tag);
}

export const knownAuthErrorSchema = v.object({
  _tag: v.picklist(authErrorTags),
  type: v.picklist(authErrorTags),
  code: v.pipe(v.string(), v.nonEmpty()),
  status: v.pipe(v.number(), v.integer()),
  title: v.pipe(v.string(), v.nonEmpty()),
  detail: v.string(),
  message: v.pipe(v.string(), v.nonEmpty()),
  retryable: v.boolean(),
  retryAfterSeconds: v.optional(v.pipe(v.number(), v.integer(), v.minValue(1))),
  requestId: v.optional(v.pipe(v.string(), v.nonEmpty())),
  traceparent: v.optional(v.pipe(v.string(), v.nonEmpty())),
});

export type KnownAuthError = v.InferOutput<typeof knownAuthErrorSchema>;

export type UnknownAuthError = {
  readonly _tag: typeof AUTH_ERROR_TAG.unknown;
  readonly type: typeof AUTH_ERROR_TAG.unknown;
  readonly code: string;
  readonly status: number;
  readonly title: string;
  readonly detail: string;
  readonly message: string;
  readonly retryable: boolean;
  readonly retryAfterSeconds?: number;
  readonly requestId?: string;
  readonly traceparent?: string;
  readonly rawTag?: string;
  readonly rawType?: string;
  readonly rawCode?: string;
};

export type AuthError = KnownAuthError | UnknownAuthError;

export type AuthSuccess<T> = {
  readonly _tag: "Ok";
  readonly value: T;
};

export type AuthFailure = {
  readonly _tag: "Err";
  readonly error: AuthError;
};

export type AuthResult<T> = AuthSuccess<T> | AuthFailure;

export class AuthErrorException extends Error {
  readonly authError: AuthError;

  constructor(authError: AuthError) {
    super(authErrorMessage(authError));
    this.name = "AuthErrorException";
    this.authError = authError;
  }
}

export function authSuccess<T>(value: T): AuthSuccess<T> {
  return { _tag: "Ok", value };
}

export function authFailure(error: AuthError): AuthFailure {
  return { _tag: "Err", error };
}

export function authFailureFromUnknown(error: unknown): AuthFailure {
  if (isAuthErrorException(error)) {
    return authFailure(error.authError);
  }
  return authFailure(unknownAuthError({ status: 503, message: errorMessage(error) }));
}

export async function authFailureFromResponse(response: Response): Promise<AuthFailure> {
  return authFailure(
    authErrorFromResponse(response.status, await response.text(), retryAfterSeconds(response)),
  );
}

export async function authResultFromResponse<TSchema extends v.GenericSchema>(
  response: Response,
  schema: TSchema,
): Promise<AuthResult<v.InferOutput<TSchema>>> {
  if (!response.ok) {
    return authFailureFromResponse(response);
  }
  try {
    return authSuccess(v.parse(schema, await response.json()));
  } catch (error) {
    return authFailureFromUnknown(error);
  }
}

export function unwrapAuthResult<T>(result: AuthResult<T>): T {
  if (result._tag === "Ok") {
    return result.value;
  }
  throw new AuthErrorException(result.error);
}

export function isAuthErrorException(error: unknown): error is AuthErrorException {
  return error instanceof AuthErrorException;
}

export function authErrorFromResponse(
  status: number,
  body: string,
  retryAfterSeconds?: number,
): AuthError {
  const parsed = parseJSONRecord(body);
  const tag = stringField(parsed, "_tag");
  const type = stringField(parsed, "type");
  const resolvedTag = tag || type;
  const code = stringField(parsed, "code");
  const requestId = optionalStringField(parsed, "requestId");
  const traceparent = optionalStringField(parsed, "traceparent");
  const problem = {
    _tag: resolvedTag,
    type,
    code,
    status: numberField(parsed, "status") || status,
    title: stringField(parsed, "title"),
    detail: stringField(parsed, "detail"),
    message: stringField(parsed, "message"),
    retryable:
      booleanField(parsed, "retryable") ?? (status === 429 || status === 502 || status === 503),
    ...(retryAfterSeconds !== undefined ? { retryAfterSeconds } : {}),
    ...(requestId ? { requestId } : {}),
    ...(traceparent ? { traceparent } : {}),
  };
  if (problem._tag === problem.type && isKnownAuthErrorTag(problem._tag)) {
    const decoded = v.safeParse(knownAuthErrorSchema, {
      ...problem,
      title: problem.title || fallbackTitle(status),
      detail: problem.detail || fallbackDetail(status),
      message: problem.message || problem.detail || fallbackMessage(status),
      code: problem.code || "auth.unknown",
    });
    if (decoded.success) {
      return decoded.output;
    }
  }
  return unknownAuthError({
    status,
    message: problem.message || problem.detail || fallbackMessage(status),
    ...(retryAfterSeconds !== undefined ? { retryAfterSeconds } : {}),
    ...(requestId ? { requestId } : {}),
    ...(traceparent ? { traceparent } : {}),
    ...(tag ? { rawTag: tag } : {}),
    ...(type ? { rawType: type } : {}),
    ...(code ? { rawCode: code } : {}),
  });
}

export function authErrorMessage(error: AuthError): string {
  switch (error._tag) {
    case AUTH_ERROR_TAG.rateLimited:
      return rateLimitMessage(error.retryAfterSeconds);
    case AUTH_ERROR_TAG.passwordTooShort:
      return "Use at least 8 characters.";
    case AUTH_ERROR_TAG.passwordTooLong:
      return "Use a shorter password.";
    case AUTH_ERROR_TAG.passwordBreached:
      return "This password has appeared in a public data breach. Please use a different one.";
    case AUTH_ERROR_TAG.passwordRejected:
      return "Use a password that is at least 8 characters and has not appeared in a data breach.";
    case AUTH_ERROR_TAG.serviceUnavailable:
    case AUTH_ERROR_TAG.unknown:
      return "Authentication is unavailable right now.";
    default:
      return error.message || fallbackMessage(error.status);
  }
}

export function passwordPolicyErrorMessage(error: AuthError): string {
  switch (error._tag) {
    case AUTH_ERROR_TAG.passwordTooShort:
    case AUTH_ERROR_TAG.passwordTooLong:
    case AUTH_ERROR_TAG.passwordBreached:
    case AUTH_ERROR_TAG.passwordRejected:
      return authErrorMessage(error);
    default:
      return "Use a password that is at least 8 characters and has not appeared in a data breach.";
  }
}

function unknownAuthError(input: {
  readonly status: number;
  readonly message?: string;
  readonly retryAfterSeconds?: number;
  readonly requestId?: string;
  readonly traceparent?: string;
  readonly rawTag?: string;
  readonly rawType?: string;
  readonly rawCode?: string;
}): UnknownAuthError {
  return {
    _tag: AUTH_ERROR_TAG.unknown,
    type: AUTH_ERROR_TAG.unknown,
    code: input.rawCode || "auth.unknown",
    status: input.status,
    title: fallbackTitle(input.status),
    detail: fallbackDetail(input.status),
    message: input.message || fallbackMessage(input.status),
    retryable: input.status === 429 || input.status === 502 || input.status === 503,
    ...(input.retryAfterSeconds !== undefined
      ? { retryAfterSeconds: input.retryAfterSeconds }
      : {}),
    ...(input.requestId ? { requestId: input.requestId } : {}),
    ...(input.traceparent ? { traceparent: input.traceparent } : {}),
    ...(input.rawTag ? { rawTag: input.rawTag } : {}),
    ...(input.rawType ? { rawType: input.rawType } : {}),
    ...(input.rawCode ? { rawCode: input.rawCode } : {}),
  };
}

function parseJSONRecord(body: string): Record<string, unknown> {
  try {
    const parsed = JSON.parse(body) as unknown;
    return parsed && typeof parsed === "object" && !Array.isArray(parsed)
      ? (parsed as Record<string, unknown>)
      : {};
  } catch {
    return {};
  }
}

function stringField(input: Record<string, unknown>, key: string): string {
  const value = input[key];
  return typeof value === "string" && value.trim() ? value.trim() : "";
}

function optionalStringField(input: Record<string, unknown>, key: string): string | undefined {
  const value = stringField(input, key);
  return value || undefined;
}

function numberField(input: Record<string, unknown>, key: string): number {
  const value = input[key];
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function booleanField(input: Record<string, unknown>, key: string): boolean | undefined {
  return typeof input[key] === "boolean" ? input[key] : undefined;
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message;
  return "";
}

function fallbackTitle(status: number): string {
  if (status === 429) return "Rate limited";
  if (status === 404) return "Not found";
  if (status >= 500) return "Service unavailable";
  return "Authentication failed";
}

function fallbackDetail(status: number): string {
  if (status === 429) return "Rate limit exceeded.";
  if (status === 404) return "The requested auth resource was not found.";
  if (status >= 500) return "Authentication is unavailable right now.";
  return "Authentication request failed.";
}

function fallbackMessage(status: number): string {
  if (status === 429) return "Too many attempts. Wait a moment and try again.";
  if (status >= 500) return "Authentication is unavailable right now.";
  return "Authentication request failed.";
}

function rateLimitMessage(retryAfterSeconds: number | undefined): string {
  if (retryAfterSeconds !== undefined && retryAfterSeconds > 0) {
    return `Too many attempts. Try again in ${Math.ceil(retryAfterSeconds)} seconds.`;
  }
  return "Too many attempts. Wait a moment and try again.";
}

function retryAfterSeconds(response: Response): number | undefined {
  const value = Number(response.headers.get("retry-after"));
  return Number.isFinite(value) && value > 0 ? value : undefined;
}
