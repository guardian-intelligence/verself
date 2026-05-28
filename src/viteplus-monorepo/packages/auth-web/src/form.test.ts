import { describe, expect, it } from "vite-plus/test";
import { IAM_ERROR_TAG, type IamError, type KnownIamErrorTag } from "@verself/sdk/iam-errors";
import { iamTaggedFormValidationError } from "./form.ts";

type LoginFields = {
  readonly email: string;
  readonly password: string;
};

describe("IAM form validation", () => {
  it("projects tagged IAM errors into TanStack form and field errors", () => {
    const validation = iamTaggedFormValidationError<LoginFields>(
      knownIamError(IAM_ERROR_TAG.invalidCredentials, "Email or password is incorrect."),
      {
        message: (error) => error.message,
        fields: {
          [IAM_ERROR_TAG.invalidCredentials]: ["password"],
        },
      },
    );

    expect(validation.form).toBe("Email or password is incorrect.");
    expect(validation.fields.password).toBe("Email or password is incorrect.");
    expect(validation.fields.email).toBeUndefined();
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
