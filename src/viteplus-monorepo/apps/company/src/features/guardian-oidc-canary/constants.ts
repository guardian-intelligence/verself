export const GUARDIAN_OIDC_CANARY_PATH = "/_canary/guardian-oidc";
export const GUARDIAN_OIDC_CANARY_START_PATH = `${GUARDIAN_OIDC_CANARY_PATH}/start`;
export const GUARDIAN_OIDC_CANARY_CALLBACK_PATH = `${GUARDIAN_OIDC_CANARY_PATH}/callback`;
export const GUARDIAN_OIDC_CANARY_ISSUER = "https://verself.sh";
export const GUARDIAN_OIDC_CANARY_COOKIE = "guardian_oidc_canary";
export const GUARDIAN_OIDC_CANARY_COOKIE_TTL_SECONDS = 5 * 60;

export type GuardianOidcCanaryStatus =
  | "authorization_response"
  | "invalid_canary_cookie"
  | "missing_client_registration"
  | "missing_response"
  | "provider_error"
  | "state_mismatch";

export function parseGuardianOidcCanaryStatus(
  value: unknown,
): GuardianOidcCanaryStatus | undefined {
  if (
    value === "authorization_response" ||
    value === "invalid_canary_cookie" ||
    value === "missing_client_registration" ||
    value === "missing_response" ||
    value === "provider_error" ||
    value === "state_mismatch"
  ) {
    return value;
  }
  return undefined;
}
