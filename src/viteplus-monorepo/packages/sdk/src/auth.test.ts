import { describe, expect, it } from "vite-plus/test";
import * as v from "valibot";
import {
  AUTH_ERROR_TAG,
  authErrorFromResponse,
  authErrorMessage,
  authResultFromResponse,
  isKnownAuthErrorTag,
  passwordPolicyErrorMessage,
  unwrapAuthResult,
} from "./auth.ts";

describe("auth tagged error decoding", () => {
  it("preserves structured incorrect password errors", () => {
    const error = authErrorFromResponse(
      401,
      JSON.stringify({
        _tag: AUTH_ERROR_TAG.invalidCredentials,
        type: AUTH_ERROR_TAG.invalidCredentials,
        title: "Invalid credentials",
        status: 401,
        detail: "Email or password is incorrect.",
        message: "Email or password is incorrect.",
        code: "auth.invalid_credentials",
        retryable: false,
      }),
    );

    expect(error._tag).toBe(AUTH_ERROR_TAG.invalidCredentials);
    expect(error.status).toBe(401);
    expect(authErrorMessage(error)).toBe("Email or password is incorrect.");
  });

  it("falls back to a base unknown error when the tag is not known", () => {
    const error = authErrorFromResponse(
      503,
      JSON.stringify({
        _tag: "urn:verself:problem:auth:future_error",
        type: "urn:verself:problem:auth:future_error",
        code: "auth.future_error",
        message: "future failure",
      }),
    );

    expect(error._tag).toBe(AUTH_ERROR_TAG.unknown);
    if (error._tag !== AUTH_ERROR_TAG.unknown) {
      throw new Error("expected unknown auth error");
    }
    expect(error.rawTag).toBe("urn:verself:problem:auth:future_error");
    expect(authErrorMessage(error)).toBe("Authentication is unavailable right now.");
  });

  it("recognizes IAM contract auth tags by their full URN", () => {
    expect(isKnownAuthErrorTag(AUTH_ERROR_TAG.sessionRevoked)).toBe(true);
    const error = authErrorFromResponse(
      401,
      JSON.stringify({
        _tag: AUTH_ERROR_TAG.sessionRevoked,
        type: AUTH_ERROR_TAG.sessionRevoked,
        code: "auth.session_revoked",
        status: 401,
        title: "Session revoked",
        detail: "This session has ended.",
        message: "This session has ended.",
        retryable: false,
      }),
    );

    expect(error._tag).toBe(AUTH_ERROR_TAG.sessionRevoked);
    expect(authErrorMessage(error)).toBe("This session has ended.");
  });

  it("keeps rate limit and availability messages distinct", () => {
    const rateLimited = authErrorFromResponse(
      429,
      JSON.stringify({
        _tag: AUTH_ERROR_TAG.rateLimited,
        type: AUTH_ERROR_TAG.rateLimited,
        code: "quota.rate_limited",
        status: 429,
        title: "Rate limited",
        detail: "Rate limit exceeded.",
        message: "Rate limit exceeded.",
        retryable: true,
      }),
      12,
    );
    expect(authErrorMessage(rateLimited)).toBe("Too many attempts. Try again in 12 seconds.");

    const unavailable = authErrorFromResponse(503, "not json");
    expect(unavailable._tag).toBe(AUTH_ERROR_TAG.unknown);
    expect(authErrorMessage(unavailable)).toBe("Authentication is unavailable right now.");
  });

  it("terminalizes auth HTTP responses into tagged results", async () => {
    const success = await authResultFromResponse(
      new Response(JSON.stringify({ callbackUrl: "/home" }), { status: 200 }),
      v.object({ callbackUrl: v.string() }),
    );
    expect(success).toEqual({ _tag: "Ok", value: { callbackUrl: "/home" } });

    const failure = await authResultFromResponse(
      new Response(
        JSON.stringify({
          _tag: AUTH_ERROR_TAG.rateLimited,
          type: AUTH_ERROR_TAG.rateLimited,
          code: "quota.rate_limited",
          status: 429,
          title: "Rate limited",
          detail: "Rate limit exceeded.",
          message: "Rate limit exceeded.",
          retryable: true,
        }),
        { status: 429, headers: { "retry-after": "7" } },
      ),
      v.object({ callbackUrl: v.string() }),
    );

    expect(failure._tag).toBe("Err");
    if (failure._tag !== "Err") {
      throw new Error("expected auth failure");
    }
    expect(authErrorMessage(failure.error)).toBe("Too many attempts. Try again in 7 seconds.");
  });

  it("maps password policy errors without leaking provider detail into copy", () => {
    const breached = authErrorFromResponse(
      400,
      JSON.stringify({
        _tag: AUTH_ERROR_TAG.passwordBreached,
        type: AUTH_ERROR_TAG.passwordBreached,
        code: "iam.password.breached",
        status: 400,
        title: "Password breached",
        detail: "provider detail",
        message: "provider detail",
        retryable: false,
      }),
    );
    expect(passwordPolicyErrorMessage(breached)).toBe(
      "This password has appeared in a public data breach. Please use a different one.",
    );

    const tooShort = authErrorFromResponse(
      400,
      JSON.stringify({
        _tag: AUTH_ERROR_TAG.passwordTooShort,
        type: AUTH_ERROR_TAG.passwordTooShort,
        code: "iam.password.too_short",
        status: 400,
        title: "Password too short",
        detail: "Password is too short.",
        message: "Use at least 8 characters.",
        retryable: false,
      }),
    );
    expect(passwordPolicyErrorMessage(tooShort)).toBe("Use at least 8 characters.");
  });

  it("throws a typed error when an auth result is terminal", () => {
    const error = authErrorFromResponse(
      403,
      JSON.stringify({
        _tag: AUTH_ERROR_TAG.loginConstraintFailed,
        type: AUTH_ERROR_TAG.loginConstraintFailed,
        code: "auth.login_constraint_failed",
        status: 403,
        title: "Required account mismatch",
        detail: "Sign in with the required account to continue.",
        message: "Sign in with the required account to continue.",
        retryable: false,
      }),
    );

    expect(() => unwrapAuthResult({ _tag: "Err", error })).toThrow(
      "Sign in with the required account to continue.",
    );
  });
});
