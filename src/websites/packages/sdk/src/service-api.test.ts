import { describe, expect, it } from "vite-plus/test";
import { createBearerJSONHeaders, idempotencyHeaders } from "./service-api.ts";

describe("service API headers", () => {
  it("rejects non-string access tokens before building bearer headers", () => {
    expect(() => createBearerJSONHeaders({} as string)).toThrow("accessToken must be a string");
  });

  it("rejects non-string traceparent values", () => {
    expect(() => createBearerJSONHeaders("token", {} as string)).toThrow(
      "traceparent must be a string",
    );
  });

  it("rejects non-string idempotency keys", () => {
    expect(() => idempotencyHeaders("iam-organization", {} as string)).toThrow(
      "Idempotency-Key must be a string",
    );
  });
});
