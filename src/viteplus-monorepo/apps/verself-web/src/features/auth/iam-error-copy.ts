import { IAM_ERROR_TAG, type IamError } from "@verself/sdk/iam-errors";
import {
  BREACHED_PASSWORD_MESSAGE,
  PASSWORD_LENGTH_MESSAGE,
  PASSWORD_REJECTED_MESSAGE,
  PASSWORD_TOO_LONG_MESSAGE,
} from "./password-policy";

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

function rateLimitMessage(retryAfterSeconds: number | undefined): string {
  if (retryAfterSeconds !== undefined && retryAfterSeconds > 0) {
    return `Too many attempts. Try again in ${Math.ceil(retryAfterSeconds)} seconds.`;
  }
  return "Too many attempts. Wait a moment and try again.";
}
