import {
  BREACHED_PASSWORD_MESSAGE,
  PASSWORD_LENGTH_MESSAGE,
  PASSWORD_REJECTED_MESSAGE,
  PASSWORD_TOO_LONG_MESSAGE,
} from "./password-policy";

export const passwordLoginErrorCodes = [
  "invalid_credentials",
  "rate_limited",
  "unavailable",
] as const;

export type PasswordLoginErrorCode = (typeof passwordLoginErrorCodes)[number];

export const passwordPolicyErrorCodes = ["too_short", "too_long", "breached", "rejected"] as const;

export type PasswordPolicyErrorCode = (typeof passwordPolicyErrorCodes)[number];

export type PasswordLoginResult =
  | {
      readonly ok: true;
      readonly callbackUrl: string;
    }
  | {
      readonly ok: false;
      readonly code: PasswordLoginErrorCode;
    };

export function passwordLoginErrorMessage(code: PasswordLoginErrorCode): string {
  switch (code) {
    case "invalid_credentials":
      return "Email or password is incorrect.";
    case "rate_limited":
      return "Too many attempts. Wait a moment and try again.";
    case "unavailable":
      return "Sign in is unavailable right now.";
  }
}

export function passwordLoginErrorCodeFromProblem(
  status: number,
  body: string,
): PasswordLoginErrorCode {
  const code = problemCode(body);
  if (status === 401 || code === "invalid-credentials") {
    return "invalid_credentials";
  }
  if (status === 429) {
    return "rate_limited";
  }
  return "unavailable";
}

export function passwordPolicyErrorMessage(code: PasswordPolicyErrorCode): string {
  switch (code) {
    case "too_short":
      return PASSWORD_LENGTH_MESSAGE;
    case "too_long":
      return PASSWORD_TOO_LONG_MESSAGE;
    case "breached":
      return BREACHED_PASSWORD_MESSAGE;
    case "rejected":
      return PASSWORD_REJECTED_MESSAGE;
  }
}

export function passwordPolicyErrorCodeFromProblem(
  _status: number,
  body: string,
): PasswordPolicyErrorCode {
  const code = problemCode(body);
  switch (code) {
    case "iam.password.too_short":
      return "too_short";
    case "iam.password.too_long":
      return "too_long";
    case "iam.password.breached":
      return "breached";
    default:
      return "rejected";
  }
}

function problemCode(body: string): string {
  try {
    const problem = JSON.parse(body) as { code?: unknown };
    return typeof problem.code === "string" ? problem.code : "";
  } catch {
    return "";
  }
}
