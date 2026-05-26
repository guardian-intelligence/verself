export type IamAuthProblem = {
  readonly code: string;
  readonly title: string;
  readonly status: number | null;
  readonly phase: string;
  readonly retryable: boolean;
  readonly message: string;
};

export type IamAuthProblemGroup = {
  readonly id: string;
  readonly label: string;
  readonly description: string;
  readonly problems: readonly IamAuthProblem[];
};

const ERROR_BASE = "/docs/reference/iam/errors";

export function iamAuthProblemAnchor(code: string): string {
  return code.replaceAll(/[._]/g, "-");
}

export function iamAuthProblemPath(code: string): string {
  return `${ERROR_BASE}#${iamAuthProblemAnchor(code)}`;
}

const authProblem = (
  code: string,
  title: string,
  status: number | null,
  phase: string,
  retryable: boolean,
  message: string,
): IamAuthProblem => ({
  code,
  title,
  status,
  phase,
  retryable,
  message,
});

export const IAM_AUTH_PROBLEM_GROUPS: readonly IamAuthProblemGroup[] = [
  {
    id: "authentication",
    label: "Authentication",
    description:
      "Problems raised before Verself can trust bearer evidence, device-session evidence, or authentication freshness.",
    problems: [
      authProblem(
        "auth.unauthenticated",
        "Authentication required",
        401,
        "bearer_validation",
        false,
        "No valid Verself bearer credential was supplied.",
      ),
      authProblem(
        "auth.device_session_required",
        "Device session required",
        401,
        "device_session_validation",
        false,
        "The bearer credential is valid, but the request also needs a current Verself device-session id.",
      ),
      authProblem(
        "auth.session_revoked",
        "Session revoked",
        401,
        "device_session_validation",
        false,
        "The selected device session was revoked, expired, or invalidated by back-channel logout.",
      ),
      authProblem(
        "auth.reauthentication_required",
        "Reauthentication required",
        403,
        "freshness_validation",
        false,
        "The operation requires a recent authentication ceremony for the same Verself account.",
      ),
      authProblem(
        "auth.insufficient_assurance",
        "Insufficient assurance",
        403,
        "assurance_validation",
        false,
        "The actor authenticated, but the operation requires stronger proof such as passkey or MFA.",
      ),
    ],
  },
  {
    id: "account-linking",
    label: "Account Linking",
    description:
      "Problems raised while resolving a Zitadel-brokered external identity into the Verself product account graph.",
    problems: [
      authProblem(
        "auth.account_link_required",
        "Account link required",
        409,
        "identity_resolution",
        false,
        "A new provider subject returned a verified email already present on another Verself account. Verself never auto-merges this case.",
      ),
      authProblem(
        "auth.provider_email_unverified",
        "Provider email unverified",
        403,
        "identity_resolution",
        false,
        "The provider did not return a verified email address acceptable for product account creation or linking.",
      ),
      authProblem(
        "auth.provider_subject_already_linked",
        "Provider subject already linked",
        409,
        "identity_resolution",
        false,
        "The same issuer and subject are already linked to a different Verself account.",
      ),
      authProblem(
        "auth.account_selection_required",
        "Account selection required",
        409,
        "account_selection",
        false,
        "The device has multiple known accounts and the caller must choose one explicitly.",
      ),
    ],
  },
  {
    id: "organization-context",
    label: "Organization Context",
    description:
      "Problems raised after account resolution when the actor has no usable organization context.",
    problems: [
      authProblem(
        "auth.no_accessible_organization",
        "No accessible organization",
        403,
        "organization_resolution",
        false,
        "The account is valid, but it has no organization membership usable by the requested product surface.",
      ),
      authProblem(
        "auth.org_selection_required",
        "Organization selection required",
        409,
        "organization_selection",
        false,
        "The account can access multiple organizations and the caller must select the intended context.",
      ),
    ],
  },
  {
    id: "device-code",
    label: "Device Code",
    description:
      "Problems surfaced by CLI and SDK device-code login before a Verself device session exists.",
    problems: [
      authProblem(
        "auth.device_code_expired",
        "Device code expired",
        401,
        "device_authorization",
        false,
        "The OAuth device code expired before the user completed authorization in the browser.",
      ),
      authProblem(
        "auth.device_authorization_denied",
        "Device authorization denied",
        403,
        "device_authorization",
        false,
        "The user or identity policy denied the pending OAuth device authorization.",
      ),
      authProblem(
        "auth.device_authorization_slow_down",
        "Device authorization polling slowed",
        null,
        "device_authorization",
        true,
        "The authorization server asked the client to increase the polling interval.",
      ),
      authProblem(
        "auth.non_interactive_login_required",
        "Interactive login required",
        400,
        "device_authorization",
        false,
        "The CLI cannot start device-code login without a TTY. Use an existing credential source or workload identity for non-interactive automation.",
      ),
    ],
  },
];
