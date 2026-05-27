export const passwordLoginErrorCodes = [
  "invalid_credentials",
  "rate_limited",
  "unavailable",
] as const;

export type PasswordLoginErrorCode = (typeof passwordLoginErrorCodes)[number];

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

function problemCode(body: string): string {
  try {
    const problem = JSON.parse(body) as { code?: unknown };
    return typeof problem.code === "string" ? problem.code : "";
  } catch {
    return "";
  }
}
