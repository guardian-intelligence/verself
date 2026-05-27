import { describe, expect, it } from "vite-plus/test";
import {
  passwordLoginErrorCodeFromProblem,
  passwordLoginErrorMessage,
  passwordPolicyErrorCodeFromProblem,
  passwordPolicyErrorMessage,
} from "./auth-errors.ts";

describe("auth error mapping", () => {
  it("discriminates incorrect password responses", () => {
    expect(
      passwordLoginErrorCodeFromProblem(
        401,
        JSON.stringify({
          code: "invalid-credentials",
        }),
      ),
    ).toBe("invalid_credentials");
    expect(passwordLoginErrorMessage("invalid_credentials")).toBe(
      "Email or password is incorrect.",
    );
  });

  it("keeps rate limit and availability messages distinct", () => {
    expect(passwordLoginErrorCodeFromProblem(429, "{}")).toBe("rate_limited");
    expect(passwordLoginErrorMessage("rate_limited")).toBe(
      "Too many attempts. Wait a moment and try again.",
    );
    expect(passwordLoginErrorCodeFromProblem(503, "not json")).toBe("unavailable");
  });

  it("maps password policy problems without leaking provider detail into copy", () => {
    expect(
      passwordPolicyErrorCodeFromProblem(
        400,
        JSON.stringify({
          code: "iam.password.breached",
        }),
      ),
    ).toBe("breached");
    expect(passwordPolicyErrorMessage("breached")).toBe(
      "This password has appeared in a public data breach. Please use a different one.",
    );
    expect(
      passwordPolicyErrorCodeFromProblem(
        400,
        JSON.stringify({
          code: "iam.password.too_short",
        }),
      ),
    ).toBe("too_short");
    expect(passwordPolicyErrorMessage("too_short")).toBe("Use at least 8 characters.");
  });
});
