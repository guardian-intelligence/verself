import { describe, expect, it } from "vite-plus/test";
import * as v from "valibot";
import {
  IAM_ERROR_TAG,
  IAM_SUCCESS_TAG,
  UNKNOWN_IAM_ERROR_STATUS,
  iamErrorFromResponse,
  iamErrorFromUnknown,
  iamOutcomeFromResponse,
  isIamError,
  isKnownIamErrorTag,
} from "./iam-errors.ts";

describe("IAM tagged error decoding", () => {
  it("preserves structured incorrect password errors", async () => {
    const error = await iamErrorFromResponse(
      new Response(
        JSON.stringify({
          _tag: IAM_ERROR_TAG.invalidCredentials,
          type: IAM_ERROR_TAG.invalidCredentials,
          title: "Invalid credentials",
          status: 401,
          detail: "Email or password is incorrect.",
          message: "Email or password is incorrect.",
          code: "auth.invalid_credentials",
          retryable: false,
        }),
        { status: 401 },
      ),
    );

    expect(error._tag).toBe(IAM_ERROR_TAG.invalidCredentials);
    expect(error.status).toBe(401);
    expect(error.message).toBe("Email or password is incorrect.");
  });

  it("falls back to the base unknown IAM error when the tag is not known", async () => {
    const error = await iamErrorFromResponse(
      new Response(
        JSON.stringify({
          _tag: "urn:verself:problem:iam:future_error",
          type: "urn:verself:problem:iam:future_error",
          code: "iam.future_error",
          message: "future failure",
        }),
        { status: 503 },
      ),
    );

    expect(error._tag).toBe(IAM_ERROR_TAG.unknown);
    if (error._tag !== IAM_ERROR_TAG.unknown) {
      throw new Error("expected unknown IAM error");
    }
    expect(error.status).toBe(UNKNOWN_IAM_ERROR_STATUS);
    expect(error.rawTag).toBe("urn:verself:problem:iam:future_error");
    expect(error.cause).toContain("sourceStatus");
    expect(error.message).toBe("IAM request failed.");
  });

  it("recognizes IAM service problem tags by their full URN", async () => {
    expect(isKnownIamErrorTag(IAM_ERROR_TAG.sessionRevoked)).toBe(true);
    const error = await iamErrorFromResponse(
      new Response(
        JSON.stringify({
          _tag: IAM_ERROR_TAG.sessionRevoked,
          type: IAM_ERROR_TAG.sessionRevoked,
          code: "auth.session_revoked",
          status: 401,
          title: "Session revoked",
          detail: "This session has ended.",
          message: "This session has ended.",
          retryable: false,
        }),
        { status: 401 },
      ),
    );

    expect(error._tag).toBe(IAM_ERROR_TAG.sessionRevoked);
    expect(error.message).toBe("This session has ended.");
  });

  it("keeps rate limit and availability messages distinct", async () => {
    const rateLimited = await iamErrorFromResponse(
      new Response(
        JSON.stringify({
          _tag: IAM_ERROR_TAG.rateLimited,
          type: IAM_ERROR_TAG.rateLimited,
          code: "quota.rate_limited",
          status: 429,
          title: "Rate limited",
          detail: "Rate limit exceeded.",
          message: "Rate limit exceeded.",
          retryable: true,
        }),
        { status: 429, headers: { "retry-after": "7" } },
      ),
    );
    expect(rateLimited._tag).toBe(IAM_ERROR_TAG.rateLimited);
    expect(rateLimited.retryAfterSeconds).toBe(7);

    const unavailable = await iamErrorFromResponse(new Response("not json", { status: 503 }));
    expect(unavailable._tag).toBe(IAM_ERROR_TAG.unknown);
    expect(unavailable.status).toBe(UNKNOWN_IAM_ERROR_STATUS);
    expect(unavailable.message).toBe("IAM request failed.");
  });

  it("terminalizes IAM HTTP responses into direct tagged outcomes", async () => {
    const success = await iamOutcomeFromResponse(
      new Response(JSON.stringify({ callbackUrl: "/home" }), { status: 200 }),
      v.object({ callbackUrl: v.string() }),
      IAM_SUCCESS_TAG.passwordLoginCompleted,
    );
    expect(success).toEqual({
      _tag: IAM_SUCCESS_TAG.passwordLoginCompleted,
      callbackUrl: "/home",
    });

    const failure = await iamOutcomeFromResponse(
      new Response(
        JSON.stringify({
          _tag: IAM_ERROR_TAG.rateLimited,
          type: IAM_ERROR_TAG.rateLimited,
          code: "quota.rate_limited",
          status: 429,
          title: "Rate limited",
          detail: "Rate limit exceeded.",
          message: "Rate limit exceeded.",
          retryable: true,
        }),
        { status: 429, headers: { "retry-after": "9" } },
      ),
      v.object({ callbackUrl: v.string() }),
      IAM_SUCCESS_TAG.passwordLoginCompleted,
    );

    expect(isIamError(failure)).toBe(true);
    if (!isIamError(failure)) {
      throw new Error("expected IAM error");
    }
    expect(failure._tag).toBe(IAM_ERROR_TAG.rateLimited);
    expect(failure.retryAfterSeconds).toBe(9);
  });

  it("terminalizes schema and thrown failures as unknown IAM errors with string causes", async () => {
    const schemaFailure = await iamOutcomeFromResponse(
      new Response(JSON.stringify({ callbackUrl: 3 }), { status: 200 }),
      v.object({ callbackUrl: v.string() }),
      IAM_SUCCESS_TAG.passwordLoginCompleted,
    );

    expect(schemaFailure._tag).toBe(IAM_ERROR_TAG.unknown);
    if (schemaFailure._tag !== IAM_ERROR_TAG.unknown) {
      throw new Error("expected unknown IAM error");
    }
    expect(schemaFailure.status).toBe(UNKNOWN_IAM_ERROR_STATUS);
    expect(typeof schemaFailure.cause).toBe("string");
    expect(schemaFailure.cause).toContain("expected schema");

    const thrown = iamErrorFromUnknown(new Error("network down"));
    expect(thrown._tag).toBe(IAM_ERROR_TAG.unknown);
    if (thrown._tag !== IAM_ERROR_TAG.unknown) {
      throw new Error("expected unknown IAM error");
    }
    expect(thrown.status).toBe(UNKNOWN_IAM_ERROR_STATUS);
    expect(thrown.cause).toContain("network down");
  });

  it("preserves password policy tags as data", async () => {
    const breached = await iamErrorFromResponse(
      new Response(
        JSON.stringify({
          _tag: IAM_ERROR_TAG.passwordBreached,
          type: IAM_ERROR_TAG.passwordBreached,
          code: "iam.password.breached",
          status: 400,
          title: "Password breached",
          detail: "provider detail",
          message: "provider detail",
          retryable: false,
        }),
        { status: 400 },
      ),
    );
    expect(breached._tag).toBe(IAM_ERROR_TAG.passwordBreached);
    expect(breached.message).toBe("provider detail");

    const tooShort = await iamErrorFromResponse(
      new Response(
        JSON.stringify({
          _tag: IAM_ERROR_TAG.passwordTooShort,
          type: IAM_ERROR_TAG.passwordTooShort,
          code: "iam.password.too_short",
          status: 400,
          title: "Password too short",
          detail: "Password is too short.",
          message: "Use at least 8 characters.",
          retryable: false,
        }),
        { status: 400 },
      ),
    );
    expect(tooShort._tag).toBe(IAM_ERROR_TAG.passwordTooShort);
    expect(tooShort.message).toBe("Use at least 8 characters.");
  });
});
