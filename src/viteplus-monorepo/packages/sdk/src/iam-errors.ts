import * as v from "valibot";

export const UNKNOWN_IAM_ERROR_STATUS = 500;

export const IAM_ERROR_TAG = {
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
  unknown: "urn:verself:problem:iam:unknown",
} as const;

export const IAM_SUCCESS_TAG = {
  memberInviteAccepted: "iam.member_invite.accepted",
  passwordLoginCompleted: "iam.password_login.completed",
  githubLoginStarted: "iam.github_login.started",
  passwordResetStarted: "iam.password_reset.started",
  passwordResetCompleted: "iam.password_reset.completed",
  deviceLoginLoaded: "iam.device_login.loaded",
  deviceLoginApproved: "iam.device_login.approved",
  deviceLoginDenied: "iam.device_login.denied",
  signupVerified: "iam.signup.verified",
  signupStarted: "iam.signup.started",
} as const;

export const iamErrorTags = [
  IAM_ERROR_TAG.requestMethodNotAllowed,
  IAM_ERROR_TAG.requestBodyTooLarge,
  IAM_ERROR_TAG.requestValidationFailed,
  IAM_ERROR_TAG.permissionDenied,
  IAM_ERROR_TAG.invalidCredentials,
  IAM_ERROR_TAG.loginConstraintFailed,
  IAM_ERROR_TAG.originNotAllowed,
  IAM_ERROR_TAG.providerSessionRequired,
  IAM_ERROR_TAG.unauthenticated,
  IAM_ERROR_TAG.reauthenticationRequired,
  IAM_ERROR_TAG.accountLinkRequired,
  IAM_ERROR_TAG.providerEmailUnverified,
  IAM_ERROR_TAG.providerSubjectAlreadyLinked,
  IAM_ERROR_TAG.sessionRevoked,
  IAM_ERROR_TAG.noAccessibleOrganization,
  IAM_ERROR_TAG.orgSelectionRequired,
  IAM_ERROR_TAG.accountSelectionRequired,
  IAM_ERROR_TAG.insufficientAssurance,
  IAM_ERROR_TAG.deviceSessionRequired,
  IAM_ERROR_TAG.organizationUnavailable,
  IAM_ERROR_TAG.accountUnavailable,
  IAM_ERROR_TAG.sessionUnavailable,
  IAM_ERROR_TAG.oidcCallbackFailed,
  IAM_ERROR_TAG.deviceLoginExpired,
  IAM_ERROR_TAG.deviceLoginNotFound,
  IAM_ERROR_TAG.deviceLoginNotPending,
  IAM_ERROR_TAG.organizationSlugUnavailable,
  IAM_ERROR_TAG.passwordBreached,
  IAM_ERROR_TAG.passwordRejected,
  IAM_ERROR_TAG.passwordTooLong,
  IAM_ERROR_TAG.passwordTooShort,
  IAM_ERROR_TAG.passwordResetTokenInvalid,
  IAM_ERROR_TAG.signupAccountExists,
  IAM_ERROR_TAG.signupMaterializing,
  IAM_ERROR_TAG.signupStateConflict,
  IAM_ERROR_TAG.signupVerificationAlreadyUsed,
  IAM_ERROR_TAG.signupVerificationExpired,
  IAM_ERROR_TAG.signupVerificationInvalid,
  IAM_ERROR_TAG.conflictState,
  IAM_ERROR_TAG.idempotencyPayloadMismatch,
  IAM_ERROR_TAG.paymentRequired,
  IAM_ERROR_TAG.rateLimited,
  IAM_ERROR_TAG.resourceNotFound,
  IAM_ERROR_TAG.serviceUnavailable,
] as const;

export type KnownIamErrorTag = (typeof iamErrorTags)[number];
export type IamErrorTag = KnownIamErrorTag | typeof IAM_ERROR_TAG.unknown;

const iamErrorTagSet = new Set<string>(iamErrorTags);
const iamTerminalTagSet = new Set<string>([...iamErrorTags, IAM_ERROR_TAG.unknown]);

export function isKnownIamErrorTag(tag: string): tag is KnownIamErrorTag {
  return iamErrorTagSet.has(tag);
}

export function isIamErrorTag(tag: string): tag is IamErrorTag {
  return iamTerminalTagSet.has(tag);
}

export const knownIamErrorSchema = v.object({
  _tag: v.picklist(iamErrorTags),
  type: v.picklist(iamErrorTags),
  code: v.pipe(v.string(), v.nonEmpty()),
  status: v.pipe(v.number(), v.integer(), v.minValue(400), v.maxValue(599)),
  title: v.pipe(v.string(), v.nonEmpty()),
  detail: v.pipe(v.string(), v.nonEmpty()),
  message: v.pipe(v.string(), v.nonEmpty()),
  retryable: v.boolean(),
  retryAfterSeconds: v.optional(v.pipe(v.number(), v.integer(), v.minValue(1))),
  requestId: v.optional(v.pipe(v.string(), v.nonEmpty())),
  traceparent: v.optional(v.pipe(v.string(), v.nonEmpty())),
});

export const unknownIamErrorSchema = v.object({
  _tag: v.literal(IAM_ERROR_TAG.unknown),
  type: v.literal(IAM_ERROR_TAG.unknown),
  code: v.literal("iam.unknown"),
  status: v.literal(UNKNOWN_IAM_ERROR_STATUS),
  title: v.literal("IAM request failed"),
  detail: v.literal("IAM request failed."),
  message: v.literal("IAM request failed."),
  cause: v.pipe(v.string(), v.nonEmpty()),
  retryable: v.literal(true),
  retryAfterSeconds: v.optional(v.pipe(v.number(), v.integer(), v.minValue(1))),
  requestId: v.optional(v.pipe(v.string(), v.nonEmpty())),
  traceparent: v.optional(v.pipe(v.string(), v.nonEmpty())),
  rawTag: v.optional(v.pipe(v.string(), v.nonEmpty())),
  rawType: v.optional(v.pipe(v.string(), v.nonEmpty())),
  rawCode: v.optional(v.pipe(v.string(), v.nonEmpty())),
});

export const iamErrorSchema = v.union([knownIamErrorSchema, unknownIamErrorSchema]);

export type IamKnownErrorBase<TTag extends KnownIamErrorTag> = Omit<
  v.InferOutput<typeof knownIamErrorSchema>,
  "_tag" | "type"
> & {
  readonly _tag: TTag;
  readonly type: TTag;
};

type IamKnownErrorByTag = {
  readonly [TTag in KnownIamErrorTag]: IamKnownErrorBase<TTag>;
};

export type KnownIamError = IamKnownErrorByTag[KnownIamErrorTag];

export type UnknownIamError = {
  readonly _tag: typeof IAM_ERROR_TAG.unknown;
  readonly type: typeof IAM_ERROR_TAG.unknown;
  readonly code: "iam.unknown";
  readonly status: typeof UNKNOWN_IAM_ERROR_STATUS;
  readonly title: "IAM request failed";
  readonly detail: "IAM request failed.";
  readonly message: "IAM request failed.";
  readonly cause: string;
  readonly retryable: true;
  readonly retryAfterSeconds?: number;
  readonly requestId?: string;
  readonly traceparent?: string;
  readonly rawTag?: string;
  readonly rawType?: string;
  readonly rawCode?: string;
};

export type IamError = KnownIamError | UnknownIamError;

export type IamSuccess<TTag extends string, TValue> = TValue & {
  readonly _tag: TTag;
};

export type IamOutcome<TTag extends string, TValue> = IamSuccess<TTag, TValue> | IamError;

export function isIamError(input: unknown): input is IamError {
  return parseIamError(input) !== undefined;
}

export async function iamErrorFromResponse(response: Response): Promise<IamError> {
  const body = await response.text();
  const retryAfter = retryAfterSeconds(response);
  return iamErrorFromResponseBody(
    response.status,
    body,
    retryAfter === undefined ? {} : { retryAfterSeconds: retryAfter },
  );
}

export async function iamOutcomeFromResponse<TTag extends string, TSchema extends v.GenericSchema>(
  response: Response,
  schema: TSchema,
  successTag: TTag,
): Promise<IamOutcome<TTag, v.InferOutput<TSchema>>> {
  if (!response.ok) {
    return iamErrorFromResponse(response);
  }
  try {
    const value = v.parse(schema, await response.json()) as v.InferOutput<TSchema>;
    const fields = value && typeof value === "object" ? value : { value };
    return {
      ...fields,
      _tag: successTag,
    } as IamSuccess<TTag, v.InferOutput<TSchema>>;
  } catch (cause) {
    return unknownIamError({
      cause: {
        reason: "IAM success response did not match the expected schema",
        cause,
      },
    });
  }
}

export function iamErrorFromUnknown(cause: unknown): IamError {
  if (isIamError(cause)) {
    return cause;
  }
  return unknownIamError({ cause });
}

export function unknownIamError(input: {
  readonly cause: unknown;
  readonly retryAfterSeconds?: number;
  readonly requestId?: string;
  readonly traceparent?: string;
  readonly rawTag?: string;
  readonly rawType?: string;
  readonly rawCode?: string;
}): UnknownIamError {
  return {
    _tag: IAM_ERROR_TAG.unknown,
    type: IAM_ERROR_TAG.unknown,
    code: "iam.unknown",
    status: UNKNOWN_IAM_ERROR_STATUS,
    title: "IAM request failed",
    detail: "IAM request failed.",
    message: "IAM request failed.",
    cause: safeStringifyCause(input.cause),
    retryable: true,
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

export function safeStringifyCause(cause: unknown): string {
  const value = errorToJSON(cause);
  try {
    const seen = new WeakSet<object>();
    const serialized = JSON.stringify(value, (_key, item: unknown) => {
      if (typeof item === "bigint") {
        return item.toString();
      }
      if (item && typeof item === "object") {
        if (seen.has(item)) {
          return "[Circular]";
        }
        seen.add(item);
      }
      return item;
    });
    return truncateCause(serialized ?? String(value));
  } catch {
    return truncateCause(String(value));
  }
}

function iamErrorFromResponseBody(
  status: number,
  body: string,
  options: { readonly retryAfterSeconds?: number } = {},
): IamError {
  const parsed = parseJSONRecord(body);
  const tag = stringField(parsed, "_tag");
  const type = stringField(parsed, "type");
  const code = stringField(parsed, "code");
  const requestId = optionalStringField(parsed, "requestId");
  const traceparent = optionalStringField(parsed, "traceparent");
  const retryAfter =
    options.retryAfterSeconds ?? optionalPositiveIntegerField(parsed, "retryAfterSeconds");
  const candidate = {
    _tag: tag,
    type,
    code,
    status: numberField(parsed, "status"),
    title: stringField(parsed, "title"),
    detail: stringField(parsed, "detail"),
    message: stringField(parsed, "message"),
    retryable: booleanField(parsed, "retryable"),
    ...(retryAfter !== undefined ? { retryAfterSeconds: retryAfter } : {}),
    ...(requestId ? { requestId } : {}),
    ...(traceparent ? { traceparent } : {}),
  };
  if (tag === type && isKnownIamErrorTag(tag)) {
    const decoded = v.safeParse(knownIamErrorSchema, candidate);
    if (decoded.success) {
      return decoded.output as KnownIamError;
    }
  }
  return unknownIamError({
    cause: {
      reason: "IAM error response did not match the tagged error contract",
      sourceStatus: status,
      body: truncateCause(body),
    },
    ...(retryAfter !== undefined ? { retryAfterSeconds: retryAfter } : {}),
    ...(requestId ? { requestId } : {}),
    ...(traceparent ? { traceparent } : {}),
    ...(tag ? { rawTag: tag } : {}),
    ...(type ? { rawType: type } : {}),
    ...(code ? { rawCode: code } : {}),
  });
}

function parseIamError(input: unknown): IamError | undefined {
  const decoded = v.safeParse(iamErrorSchema, input);
  if (!decoded.success || decoded.output._tag !== decoded.output.type) {
    return undefined;
  }
  return decoded.output as IamError;
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
  return typeof value === "number" && Number.isInteger(value) ? value : 0;
}

function optionalPositiveIntegerField(
  input: Record<string, unknown>,
  key: string,
): number | undefined {
  const value = numberField(input, key);
  return value > 0 ? value : undefined;
}

function booleanField(input: Record<string, unknown>, key: string): boolean | undefined {
  return typeof input[key] === "boolean" ? input[key] : undefined;
}

function retryAfterSeconds(response: Response): number | undefined {
  const value = Number(response.headers.get("retry-after"));
  return Number.isInteger(value) && value > 0 ? value : undefined;
}

function errorToJSON(input: unknown): unknown {
  if (input instanceof Error) {
    return {
      name: input.name,
      message: input.message,
    };
  }
  return input;
}

function truncateCause(value: string): string {
  return value.length > 2048 ? `${value.slice(0, 2045)}...` : value;
}
