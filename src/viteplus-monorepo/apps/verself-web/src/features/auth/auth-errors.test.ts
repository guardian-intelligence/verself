import { describe, expect, it } from "vite-plus/test";
import { passwordLoginErrorCodeFromProblem, passwordLoginErrorMessage } from "./auth-errors.ts";

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
});
