import { describe, expect, it } from "vite-plus/test";
import * as v from "valibot";
import {
  MIN_PASSWORD_LENGTH,
  deviceCodeFormSchema,
  loginFormSchema,
  normalizeDeviceCode,
  normalizeEmail,
  resetPasswordFormSchema,
  signupStartFormSchema,
  slugify,
} from "./form-schemas.ts";

function issues(schema: v.GenericSchema, input: unknown): string[] {
  const result = v.safeParse(schema, input);
  return result.success ? [] : result.issues.map((issue) => issue.message);
}

describe("auth form schemas", () => {
  it("validates login without composition rules", () => {
    expect(normalizeEmail(" Founder@Example.TEST ")).toBe("founder@example.test");
    expect(issues(loginFormSchema, { email: "", password: "" })).toContain("Email is required.");
    expect(issues(loginFormSchema, { email: "not-an-email", password: "" })).toContain(
      "Enter a valid email address.",
    );
    expect(
      v.safeParse(loginFormSchema, { email: "founder@example.test", password: "short" }).success,
    ).toBe(true);
  });

  it("validates password setup with passphrase guidance and confirmation forwarding", () => {
    expect(
      issues(resetPasswordFormSchema, {
        password: "1234567",
        confirmPassword: "1234567",
      }),
    ).toContain(`Use at least ${MIN_PASSWORD_LENGTH} characters.`);
    expect(
      issues(resetPasswordFormSchema, {
        password: "correct horse battery staple",
        confirmPassword: "wrong",
      }),
    ).toContain("Passwords do not match.");
  });

  it("normalizes and validates organization identity", () => {
    expect(slugify("  Guardian Intelligence!!  ")).toBe("guardian-intelligence");
    expect(
      v.safeParse(signupStartFormSchema, {
        email: "founder@example.test",
        organizationDisplayName: "Guardian Intelligence",
        organizationSlug: "guardian-intelligence",
        givenName: "",
        familyName: "",
      }).success,
    ).toBe(true);
    expect(
      issues(signupStartFormSchema, {
        email: "founder@example.test",
        organizationDisplayName: "",
        organizationSlug: "",
        givenName: "",
        familyName: "",
      }),
    ).toContain("Organization name is required.");
  });

  it("normalizes and validates device codes from URLs and email bodies", () => {
    expect(normalizeDeviceCode(" vers elf1 ")).toBe("VERS-ELF1");
    expect(issues(deviceCodeFormSchema, { userCode: "" })).toContain("Device code is required.");
    expect(v.safeParse(deviceCodeFormSchema, { userCode: "VERS-ELF1" }).success).toBe(true);
  });
});
