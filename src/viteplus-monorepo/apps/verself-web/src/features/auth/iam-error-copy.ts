import {
  iamTaggedFormValidationError,
  type IamFormErrorFieldMap,
  type IamFormValidationError,
} from "@verself/auth-web/form";
import { IAM_ERROR_TAG, type IamError } from "@verself/sdk/iam-errors";
import {
  BREACHED_PASSWORD_MESSAGE,
  PASSWORD_LENGTH_MESSAGE,
  PASSWORD_REJECTED_MESSAGE,
  PASSWORD_TOO_LONG_MESSAGE,
} from "./password-policy";
import type {
  ForgotPasswordFormValues,
  LoginFormValues,
  ResetPasswordFormValues,
  SignupStartFormValues,
  SignupVerificationFormValues,
} from "./form-schemas";

export function iamErrorMessage(error: IamError): string {
  switch (error._tag) {
    case IAM_ERROR_TAG.rateLimited:
      return rateLimitMessage(error.retryAfterSeconds);
    case IAM_ERROR_TAG.passwordTooShort:
      return PASSWORD_LENGTH_MESSAGE;
    case IAM_ERROR_TAG.passwordTooLong:
      return PASSWORD_TOO_LONG_MESSAGE;
    case IAM_ERROR_TAG.passwordBreached:
      return BREACHED_PASSWORD_MESSAGE;
    case IAM_ERROR_TAG.passwordRejected:
      return PASSWORD_REJECTED_MESSAGE;
    case IAM_ERROR_TAG.serviceUnavailable:
    case IAM_ERROR_TAG.unknown:
      return "Authentication is unavailable right now.";
    default:
      return error.message;
  }
}

export function loginIamFormError(error: IamError): IamFormValidationError<LoginFormValues> {
  return iamFormError(error, loginErrorFields);
}

export function signupStartIamFormError(
  error: IamError,
): IamFormValidationError<SignupStartFormValues> {
  return iamFormError(error, signupStartErrorFields);
}

export function forgotPasswordIamFormError(
  error: IamError,
): IamFormValidationError<ForgotPasswordFormValues> {
  return iamFormError(error, forgotPasswordErrorFields);
}

export function resetPasswordIamFormError(
  error: IamError,
): IamFormValidationError<ResetPasswordFormValues> {
  return iamFormError(error, resetPasswordErrorFields);
}

export function signupVerificationIamFormError(
  error: IamError,
): IamFormValidationError<SignupVerificationFormValues> {
  return iamFormError(error, signupVerificationErrorFields);
}

export function genericIamFormError<TFormData>(error: IamError): IamFormValidationError<TFormData> {
  return iamFormError(error, {});
}

function rateLimitMessage(retryAfterSeconds: number | undefined): string {
  if (retryAfterSeconds !== undefined && retryAfterSeconds > 0) {
    return `Too many attempts. Try again in ${Math.ceil(retryAfterSeconds)} seconds.`;
  }
  return "Too many attempts. Wait a moment and try again.";
}

function iamFormError<TFormData>(
  error: IamError,
  fields: IamFormErrorFieldMap<TFormData>,
): IamFormValidationError<TFormData> {
  return iamTaggedFormValidationError(error, {
    message: iamErrorMessage,
    fields,
  });
}

const loginErrorFields = {
  [IAM_ERROR_TAG.invalidCredentials]: ["password"],
  [IAM_ERROR_TAG.loginConstraintFailed]: ["email"],
  [IAM_ERROR_TAG.rateLimited]: ["password"],
} satisfies IamFormErrorFieldMap<LoginFormValues>;

const signupStartErrorFields = {
  [IAM_ERROR_TAG.signupAccountExists]: ["email"],
  [IAM_ERROR_TAG.organizationSlugUnavailable]: ["organizationSlug"],
  [IAM_ERROR_TAG.rateLimited]: ["email"],
} satisfies IamFormErrorFieldMap<SignupStartFormValues>;

const forgotPasswordErrorFields = {
  [IAM_ERROR_TAG.rateLimited]: ["email"],
} satisfies IamFormErrorFieldMap<ForgotPasswordFormValues>;

const resetPasswordErrorFields = {
  [IAM_ERROR_TAG.passwordBreached]: ["password"],
  [IAM_ERROR_TAG.passwordRejected]: ["password"],
  [IAM_ERROR_TAG.passwordTooLong]: ["password"],
  [IAM_ERROR_TAG.passwordTooShort]: ["password"],
  [IAM_ERROR_TAG.rateLimited]: ["password"],
} satisfies IamFormErrorFieldMap<ResetPasswordFormValues>;

const signupVerificationErrorFields = {
  [IAM_ERROR_TAG.invalidCredentials]: ["initialPassword"],
  [IAM_ERROR_TAG.organizationSlugUnavailable]: ["organizationSlug"],
  [IAM_ERROR_TAG.passwordBreached]: ["initialPassword"],
  [IAM_ERROR_TAG.passwordRejected]: ["initialPassword"],
  [IAM_ERROR_TAG.passwordTooLong]: ["initialPassword"],
  [IAM_ERROR_TAG.passwordTooShort]: ["initialPassword"],
  [IAM_ERROR_TAG.rateLimited]: ["initialPassword"],
} satisfies IamFormErrorFieldMap<SignupVerificationFormValues>;
