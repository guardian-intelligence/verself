import { describe, expect, it } from "vite-plus/test";
import { IAM_ERROR_TAG, type IamError, type KnownIamErrorTag } from "@verself/sdk/iam-errors";
import {
  loginIamFormError,
  resetPasswordIamFormError,
  signupStartIamFormError,
  signupVerificationIamFormError,
} from "./iam-error-copy.ts";
import { BREACHED_PASSWORD_MESSAGE, PASSWORD_REJECTED_MESSAGE } from "./password-policy.ts";

describe("auth IAM error copy", () => {
  it("maps incorrect email or password to login form state", () => {
    const validation = loginIamFormError(
      knownIamError(IAM_ERROR_TAG.invalidCredentials, "Email or password is incorrect."),
    );

    expect(validation.form).toBe("Email or password is incorrect.");
    expect(validation.fields.password).toBe("Email or password is incorrect.");
  });

  it("maps organization and password policy tags to their editable fields", () => {
    expect(
      signupStartIamFormError(
        knownIamError(IAM_ERROR_TAG.organizationSlugUnavailable, "Organization URL is taken."),
      ).fields.organizationSlug,
    ).toBe("Organization URL is taken.");

    expect(
      resetPasswordIamFormError(knownIamError(IAM_ERROR_TAG.passwordBreached, "provider detail"))
        .fields.password,
    ).toBe(BREACHED_PASSWORD_MESSAGE);

    expect(
      signupVerificationIamFormError(
        knownIamError(IAM_ERROR_TAG.passwordRejected, "provider detail"),
      ).fields.initialPassword,
    ).toBe(PASSWORD_REJECTED_MESSAGE);
  });
});

function knownIamError(tag: KnownIamErrorTag, message: string): IamError {
  return {
    _tag: tag,
    type: tag,
    code: "test.error",
    status: 400,
    title: "Test error",
    detail: message,
    message,
    retryable: false,
  } as IamError;
}
