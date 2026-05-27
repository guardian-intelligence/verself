$version: "2"

namespace verself.iam.v1

use aws.protocols#restJson1

use smithy.api#auth
use smithy.api#enumValue
use smithy.api#error
use smithy.api#http
use smithy.api#httpBearerAuth
use smithy.api#httpError
use smithy.api#httpHeader
use smithy.api#httpLabel
use smithy.api#httpPayload
use smithy.api#httpQuery
use smithy.api#idempotencyToken
use smithy.api#idempotent
use smithy.api#input
use smithy.api#length
use smithy.api#mixin
use smithy.api#notProperty
use smithy.api#nestedProperties
use smithy.api#output
use smithy.api#paginated
use smithy.api#pattern
use smithy.api#range
use smithy.api#readonly
use smithy.api#required
use smithy.api#resourceIdentifier
use smithy.api#sensitive
use verself.common.v1#PermissionName
use verself.common.v1#ConflictError
use verself.common.v1#DisplayName
use verself.common.v1#IdempotencyKey
use verself.common.v1#IdempotencyPayloadMismatchError
use verself.common.v1#PageRequest
use verself.common.v1#PageResponse
use verself.common.v1#PermissionDeniedError
use verself.common.v1#ProblemDetails
use verself.common.v1#RateLimitedError
use verself.common.v1#ResourceNotFoundError
use verself.common.v1#ServiceUnavailableError
use verself.common.v1#UnauthenticatedError
use verself.common.v1#ValidationFailedError
use verself.common.v1#audit
use verself.common.v1#auditEvent
use verself.common.v1#authz
use verself.common.v1#identity
use verself.common.v1#operationSemantics
use verself.common.v1#permission
use verself.common.v1#problem
use verself.common.v1#protoField
use verself.common.v1#rateLimit
use verself.common.v1#requestBudget
use verself.common.v1#sdk
use verself.common.v1#serviceRuntime
use verself.common.v1#DateTime

@httpBearerAuth
@auth([smithy.api#httpBearerAuth])
@serviceRuntime(serviceName: "iam-service", publicAudience: "verself-api")
@restJson1
service Iam {
    version: "2026-05-12"
    operations: [
        GetAuthContext
        CreateDeviceSession
        ListDeviceSessions
        RevokeDeviceSession
        ListAccountConnections
        RemoveAccountConnection
        CompletePasswordLogin
        StartPasswordReset
        CompletePasswordReset
        LookupDeviceLogin
        ApproveDeviceLogin
        DenyDeviceLogin
        BrowserAuthCallback
        GetBrowserSession
        SelectBrowserOrganization
        CreateBrowserResourceToken
        LogoutBrowserSession
        CheckOrganizationSlugAvailability
        StartSignup
        VerifySignup
        AcceptMemberInvite
        InviteMember
    ]
    resources: [
        Account
        DeviceSession
        AccountConnection
        BrowserAccount
        BrowserSession
        DeviceLogin
        SignupIntent
        Organization
    ]
}

@serviceRuntime(serviceName: "iam-service", publicAudience: "verself-api", internalAudience: "iam-service")
@restJson1
service IamInternal {
    version: "2026-05-12"
    operations: [
        UpdateHumanProfile
        ResolveOrganization
        AuthorizeOperation
        AuthorizeResource
        WriteResourceParentEdge
    ]
    resources: [
        HumanProfile
        Authorization
    ]
}

@pattern("^org_[0-9A-HJKMNP-TV-Z]{26}$")
string OrgId

@pattern("^member_[0-9A-HJKMNP-TV-Z]{26}$")
string MemberId

@pattern("^signup_[0-9A-HJKMNP-TV-Z]{26}$")
string SignupIntentId

@pattern("^urn:verself:inst_[0-9A-HJKMNP-TV-Z]{26}:orgs/org_[0-9A-HJKMNP-TV-Z]{26}$")
string OrganizationResourceName

@pattern("^urn:verself:inst_[0-9A-HJKMNP-TV-Z]{26}:orgs/org_[0-9A-HJKMNP-TV-Z]{26}/members/member_[0-9A-HJKMNP-TV-Z]{26}$")
string MemberResourceName

@length(min: 1, max: 80)
@pattern("^[a-z0-9]([a-z0-9-]*[a-z0-9])?$")
string OrgSlug

@length(min: 3, max: 320)
string EmailAddress

@length(min: 32, max: 512)
@sensitive
string SignupVerificationToken

@length(min: 32, max: 512)
@sensitive
string MemberInviteAcceptanceToken

@length(min: 1, max: 2048)
string LoginURL

@length(min: 1, max: 512)
string OIDCAuthRequestId

@length(min: 1, max: 64)
@pattern("^[a-z][a-z0-9_]*$")
string BrowserLoginPurpose

@length(min: 1, max: 32)
@pattern("^(login|select_account)$")
string BrowserLoginPrompt

@length(min: 1, max: 128)
@pattern("^[a-z][a-z0-9_.]*$")
string AuthWarningCode

structure AuthWarning {
    @required
    @protoField(number: 1)
    code: AuthWarningCode

    @required
    @protoField(number: 2)
    message: String
}

list AuthWarnings {
    member: AuthWarning
}

@length(min: 1, max: 64)
@pattern("^[0-9]+$")
string IdentityProviderOrgId

@range(min: 1, max: 2147483647)
integer OrganizationVersion

@length(min: 1, max: 1024)
string PolicyEtag

@range(min: 1, max: 3)
integer PolicyVersion

@length(min: 1, max: 256)
@pattern("^roles/[A-Za-z][A-Za-z0-9.]*$")
string IAMRoleName

@length(min: 1, max: 1024)
string IAMMemberName

@length(min: 1, max: 512)
string SubjectId

@length(min: 31, max: 31)
@pattern("^acct_[0-9A-HJKMNP-TV-Z]{26}$")
string AccountId

@length(min: 31, max: 31)
@pattern("^sess_[0-9A-HJKMNP-TV-Z]{26}$")
string DeviceSessionId

@length(min: 31, max: 31)
@pattern("^conn_[0-9A-HJKMNP-TV-Z]{26}$")
string AccountConnectionId

@length(min: 1, max: 2048)
string IdentityIssuer

@length(min: 1, max: 128)
@pattern("^(browser|cli|sdk|mobile|workload)$")
string AuthChannel

@length(min: 1, max: 128)
@pattern("^[A-Za-z0-9._:-]+$")
string AuthMethod

list AuthMethods {
    member: AuthMethod
}

@length(min: 1, max: 80)
@pattern("^(active|revoked|expired)$")
string DeviceSessionState

@length(min: 1, max: 80)
@pattern("^(linked|removed)$")
string AccountConnectionState

@length(min: 35, max: 35)
@pattern("^ba_[A-Za-z0-9_-]{32}$")
string BrowserSessionHandle

@length(min: 35, max: 35)
@pattern("^ba_[A-Za-z0-9_-]{32}$")
string BrowserAccountHandle

@length(min: 35, max: 35)
@pattern("^bc_[A-Za-z0-9_-]{32}$")
string BrowserClientHandle

@length(min: 1, max: 2048)
string BrowserRedirectPath

@length(min: 1, max: 2048)
string BrowserRedirectURL

@length(min: 8, max: 4096)
@sensitive
string Password

@length(min: 1, max: 512)
@sensitive
string PasswordResetVerificationCode

@length(min: 1, max: 128)
@pattern("^[A-Za-z0-9-]+$")
string DeviceUserCode

@length(min: 28, max: 68)
@pattern("^dlr_[A-Za-z0-9_-]{24,64}$")
string DeviceLoginHandle

@length(min: 1, max: 80)
@pattern("^(pending|approved|denied|expired)$")
string DeviceLoginState

@length(min: 1, max: 512)
string OAuthScope

list OAuthScopes {
    member: OAuthScope
}

@length(min: 1, max: 2048)
@sensitive
string OAuthAuthorizationCode

@length(min: 1, max: 512)
@sensitive
string OAuthState

@length(min: 1, max: 4096)
@sensitive
string OAuthErrorDescription

@length(min: 1, max: 4096)
@sensitive
string BrowserAccessToken

@length(min: 1, max: 128)
string ClientIPAddress

@length(min: 1, max: 128)
string ClientIPSource

@length(min: 1, max: 512)
string UserAgent

@length(min: 1, max: 160)
string DeviceLabel

@length(min: 1, max: 80)
string DeviceKind

@length(min: 1, max: 80)
string BrowserName

@length(min: 1, max: 80)
string OperatingSystemName

@length(min: 1, max: 2)
string CountryCode

@length(min: 1, max: 120)
string RegionName

@length(min: 1, max: 120)
string CityName

@length(min: 1, max: 100)
string GivenName

@length(min: 1, max: 100)
string FamilyName

@length(max: 1024)
string ZedToken

@length(min: 1, max: 128)
@pattern("^[a-z][a-z0-9_]*$")
string AuthorizationResourceType

@length(min: 1, max: 512)
string AuthorizationResourceId

@length(min: 1, max: 128)
@pattern("^[a-z][a-z0-9_]*$")
string AuthorizationRelation

@length(min: 1, max: 128)
string ResourcePermissionName

list Permissions {
    member: PermissionName
}

enum IAMAuthorizationSubjectType {
    @enumValue("user")
    USER

    @enumValue("service_account")
    SERVICE_ACCOUNT

    @enumValue("workload")
    WORKLOAD
}

enum SignupStartStatus {
    @enumValue("accepted")
    ACCEPTED
}

@error("client")
@httpError(400)
@problem(type: "urn:verself:problem:iam:signup_verification_invalid", code: "iam.signup.verification.invalid")
structure SignupVerificationInvalidError with [ProblemDetails] {}

@error("client")
@httpError(400)
@problem(type: "urn:verself:problem:iam:signup_verification_expired", code: "iam.signup.verification.expired")
structure SignupVerificationExpiredError with [ProblemDetails] {}

@error("client")
@httpError(409)
@problem(type: "urn:verself:problem:iam:signup_verification_already_used", code: "iam.signup.verification.already_used")
structure SignupVerificationAlreadyUsedError with [ProblemDetails] {}

@error("client")
@httpError(409)
@problem(type: "urn:verself:problem:iam:signup_materializing", code: "iam.signup.materializing")
structure SignupMaterializingError with [ProblemDetails] {}

@error("client")
@httpError(409)
@problem(type: "urn:verself:problem:iam:signup_state_conflict", code: "iam.signup.state_conflict")
structure SignupStateConflictError with [ProblemDetails] {}

@error("client")
@httpError(409)
@problem(type: "urn:verself:problem:iam:signup_account_exists", code: "iam.signup.account_exists")
structure SignupAccountExistsError with [ProblemDetails] {}

@error("client")
@httpError(409)
@problem(type: "urn:verself:problem:iam:organization_slug_unavailable", code: "iam.organization_slug.unavailable")
structure OrganizationSlugUnavailableError with [ProblemDetails] {}

@error("client")
@httpError(400)
@problem(type: "urn:verself:problem:iam:password_too_short", code: "iam.password.too_short")
structure PasswordTooShortError with [ProblemDetails] {}

@error("client")
@httpError(400)
@problem(type: "urn:verself:problem:iam:password_too_long", code: "iam.password.too_long")
structure PasswordTooLongError with [ProblemDetails] {}

@error("client")
@httpError(400)
@problem(type: "urn:verself:problem:iam:password_breached", code: "iam.password.breached")
structure PasswordBreachedError with [ProblemDetails] {}

@error("client")
@httpError(401)
@problem(type: "https://verself.sh/docs/reference/iam/errors#auth-reauthentication-required", code: "auth.reauthentication_required")
structure ReauthenticationRequiredError with [ProblemDetails] {}

@error("client")
@httpError(409)
@problem(type: "https://verself.sh/docs/reference/iam/errors#auth-account-link-required", code: "auth.account_link_required")
structure AccountLinkRequiredError with [ProblemDetails] {}

@error("client")
@httpError(403)
@problem(type: "https://verself.sh/docs/reference/iam/errors#auth-provider-email-unverified", code: "auth.provider_email_unverified")
structure ProviderEmailUnverifiedError with [ProblemDetails] {}

@error("client")
@httpError(409)
@problem(type: "https://verself.sh/docs/reference/iam/errors#auth-provider-subject-already-linked", code: "auth.provider_subject_already_linked")
structure ProviderSubjectAlreadyLinkedError with [ProblemDetails] {}

@error("client")
@httpError(401)
@problem(type: "https://verself.sh/docs/reference/iam/errors#auth-session-revoked", code: "auth.session_revoked")
structure SessionRevokedError with [ProblemDetails] {}

@error("client")
@httpError(403)
@problem(type: "https://verself.sh/docs/reference/iam/errors#auth-no-accessible-organization", code: "auth.no_accessible_organization")
structure NoAccessibleOrganizationError with [ProblemDetails] {}

@error("client")
@httpError(409)
@problem(type: "https://verself.sh/docs/reference/iam/errors#auth-org-selection-required", code: "auth.org_selection_required")
structure OrgSelectionRequiredError with [ProblemDetails] {}

@error("client")
@httpError(409)
@problem(type: "https://verself.sh/docs/reference/iam/errors#auth-account-selection-required", code: "auth.account_selection_required")
structure AccountSelectionRequiredError with [ProblemDetails] {}

@error("client")
@httpError(403)
@problem(type: "https://verself.sh/docs/reference/iam/errors#auth-insufficient-assurance", code: "auth.insufficient_assurance")
structure InsufficientAssuranceError with [ProblemDetails] {}

@error("client")
@httpError(401)
@problem(type: "https://verself.sh/docs/reference/iam/errors#auth-device-session-required", code: "auth.device_session_required")
structure DeviceSessionRequiredError with [ProblemDetails] {}

@permission(name: "iam:signup_intent:create")
string SignupIntentCreatePermission

@permission(name: "iam:signup_intent:verify")
string SignupIntentVerifyPermission

@permission(name: "iam:organization_slug:check")
string OrganizationSlugCheckPermission

@permission(name: "iam:member_invite:accept")
string MemberInviteAcceptPermission

@permission(name: "iam:account:read")
string AccountReadPermission

@permission(name: "iam:device_session:create")
string DeviceSessionCreatePermission

@permission(name: "iam:device_session:read")
string DeviceSessionReadPermission

@permission(name: "iam:device_session:delete")
string DeviceSessionDeletePermission

@permission(name: "iam:account_connection:read")
string AccountConnectionReadPermission

@permission(name: "iam:account_connection:delete")
string AccountConnectionDeletePermission

@permission(name: "iam:browser_account:create")
string BrowserSessionCreatePermission

@permission(name: "iam:password_reset:create")
string PasswordResetCreatePermission

@permission(name: "iam:password_reset:complete")
string PasswordResetCompletePermission

@permission(name: "iam:browser_account:read")
string BrowserSessionReadPermission

@permission(name: "iam:browser_account:update")
string BrowserSessionUpdatePermission

@permission(name: "iam:browser_account:delete")
string BrowserSessionDeletePermission

@permission(name: "iam:browser_resource_token:create")
string BrowserResourceTokenCreatePermission

@permission(name: "iam:device_login:read")
string DeviceLoginReadPermission

@permission(name: "iam:device_login:approve")
string DeviceLoginApprovePermission

@permission(name: "iam:device_login:deny")
string DeviceLoginDenyPermission

@permission(name: "iam:organization:list")
string OrganizationListPermission

@permission(name: "iam:organization:create")
string OrganizationCreatePermission

@permission(name: "iam:organization:read")
string OrganizationReadPermission

@permission(name: "iam:organization:update")
string OrganizationUpdatePermission

@permission(name: "iam:member:list")
string MemberListPermission

@permission(name: "iam:member:read")
string MemberReadPermission

@permission(name: "iam:member:invite")
string MemberInvitePermission

@permission(name: "iam:policy:get")
string IAMPolicyGetPermission

@permission(name: "iam:policy:set")
string IAMPolicySetPermission

@permission(name: "iam:policy:test")
string IAMPolicyTestPermission

@permission(name: "iam:organization:resolve")
string OrganizationResolvePermission

@permission(name: "iam:human_profile:write")
string HumanProfileWritePermission

@permission(name: "iam:authorization:check")
string AuthorizationCheckPermission

@permission(name: "iam:authorization:resource_check")
string AuthorizationResourceCheckPermission

@permission(name: "iam:authorization:parent_edge_write")
string AuthorizationParentEdgeWritePermission

@auditEvent(name: "iam.signup_intent.create")
string SignupIntentCreateAuditEvent

@auditEvent(name: "iam.signup_intent.verify")
string SignupIntentVerifyAuditEvent

@auditEvent(name: "iam.organization_slug.check")
string OrganizationSlugCheckAuditEvent

@auditEvent(name: "iam.member_invite.accept")
string MemberInviteAcceptAuditEvent

@auditEvent(name: "iam.account_context.read")
string AccountContextReadAuditEvent

@auditEvent(name: "iam.device_session.create")
string DeviceSessionCreateAuditEvent

@auditEvent(name: "iam.device_session.read")
string DeviceSessionReadAuditEvent

@auditEvent(name: "iam.device_session.delete")
string DeviceSessionDeleteAuditEvent

@auditEvent(name: "iam.account_connection.read")
string AccountConnectionReadAuditEvent

@auditEvent(name: "iam.account_connection.delete")
string AccountConnectionDeleteAuditEvent

@auditEvent(name: "iam.browser_account.create")
string BrowserSessionCreateAuditEvent

@auditEvent(name: "iam.password_reset.create")
string PasswordResetCreateAuditEvent

@auditEvent(name: "iam.password_reset.complete")
string PasswordResetCompleteAuditEvent

@auditEvent(name: "iam.browser_account.read")
string BrowserSessionReadAuditEvent

@auditEvent(name: "iam.browser_account.update")
string BrowserSessionUpdateAuditEvent

@auditEvent(name: "iam.browser_account.delete")
string BrowserSessionDeleteAuditEvent

@auditEvent(name: "iam.browser_resource_token.create")
string BrowserResourceTokenCreateAuditEvent

@auditEvent(name: "iam.device_login.read")
string DeviceLoginReadAuditEvent

@auditEvent(name: "iam.device_login.approve")
string DeviceLoginApproveAuditEvent

@auditEvent(name: "iam.device_login.deny")
string DeviceLoginDenyAuditEvent

@auditEvent(name: "iam.organization.list")
string OrganizationListAuditEvent

@auditEvent(name: "iam.organization.create")
string OrganizationCreateAuditEvent

@auditEvent(name: "iam.organization.read")
string OrganizationReadAuditEvent

@auditEvent(name: "iam.organization.update")
string OrganizationUpdateAuditEvent

@auditEvent(name: "iam.member.list")
string MemberListAuditEvent

@auditEvent(name: "iam.member.read")
string MemberReadAuditEvent

@auditEvent(name: "iam.member.invite")
string MemberInviteAuditEvent

@auditEvent(name: "iam.policy.get")
string IAMPolicyGetAuditEvent

@auditEvent(name: "iam.policy.set")
string IAMPolicySetAuditEvent

@auditEvent(name: "iam.policy.test")
string IAMPolicyTestAuditEvent

@auditEvent(name: "iam.organization.resolve")
string OrganizationResolveAuditEvent

@auditEvent(name: "iam.human_profile.write")
string HumanProfileWriteAuditEvent

@auditEvent(name: "iam.authorization.check")
string AuthorizationCheckAuditEvent

@auditEvent(name: "iam.authorization.resource_check")
string AuthorizationResourceCheckAuditEvent

@auditEvent(name: "iam.authorization.resource_parent_edge.write")
string AuthorizationParentEdgeWriteAuditEvent

resource BrowserSession {
    identifiers: {
        sessionHandle: BrowserSessionHandle
    }
    list: ListBrowserSessions
    delete: RevokeBrowserSession
}

resource BrowserAccount {
    identifiers: {
        accountHandle: BrowserAccountHandle
    }
    list: ListBrowserAccounts
    delete: RemoveBrowserAccount
    operations: [
        SelectBrowserAccount
    ]
}

resource DeviceLogin {
    identifiers: {
        deviceLoginHandle: DeviceLoginHandle
    }
}

resource Organization {
    identifiers: {
        orgId: OrgId
    }
    properties: {
        resourceName: OrganizationResourceName
        slug: OrgSlug
        displayName: DisplayName
        version: OrganizationVersion
    }
    create: CreateOrganization
    list: ListOrganizations
    read: GetOrganization
    update: UpdateOrganization
    operations: [
        GetIamPolicy
        SetIamPolicy
        TestIamPermissions
    ]
    resources: [Member]
}

resource SignupIntent {}

resource Member {
    identifiers: {
        orgId: OrgId
        memberId: MemberId
    }
    properties: {
        resourceName: MemberResourceName
        email: EmailAddress
        displayName: DisplayName
    }
    list: ListMembers
    read: GetMember
}

resource HumanProfile {
}

resource Authorization {
}

resource Account {
    identifiers: {
        accountId: AccountId
    }
}

resource DeviceSession {
    identifiers: {
        sessionId: DeviceSessionId
    }
}

resource AccountConnection {
    identifiers: {
        connectionId: AccountConnectionId
    }
}

structure OrganizationSummary for Organization {
    @required
    @resourceIdentifier("orgId")
    @protoField(number: 1)
    $orgId

    @required
    @protoField(number: 2)
    $resourceName

    @protoField(number: 3)
    $slug

    @required
    @protoField(number: 4)
    $displayName

    @required
    @protoField(number: 5)
    $version
}

structure MemberSummary for Member {
    @required
    @resourceIdentifier("orgId")
    @protoField(number: 1)
    $orgId

    @required
    @resourceIdentifier("memberId")
    @protoField(number: 2)
    $memberId

    @required
    @protoField(number: 3)
    $resourceName

    @required
    @protoField(number: 4)
    $email

    @required
    @protoField(number: 5)
    $displayName

}

structure AccountSummary for Account {
    @required
    @resourceIdentifier("accountId")
    @protoField(number: 1)
    @suppress(["MemberShouldReferenceResource"])
    accountId: AccountId

    @required
    @protoField(number: 2)
    issuer: IdentityIssuer

    @required
    @protoField(number: 3)
    subject: SubjectId

    @required
    @protoField(number: 4)
    email: EmailAddress

    @required
    @protoField(number: 5)
    emailVerified: Boolean

    @required
    @protoField(number: 6)
    displayName: DisplayName

    @required
    @protoField(number: 7)
    createdAt: DateTime
}

structure AuthOrganizationContext {
    @required
    @resourceIdentifier("orgId")
    @protoField(number: 1)
    @suppress(["MemberShouldReferenceResource"])
    orgId: OrgId

    @required
    @protoField(number: 2)
    displayName: DisplayName

    @protoField(number: 3)
    slug: OrgSlug

    @required
    @protoField(number: 4)
    selected: Boolean
}

list AuthOrganizationContexts {
    member: AuthOrganizationContext
}

structure DeviceSessionSummary for DeviceSession {
    @required
    @resourceIdentifier("sessionId")
    @protoField(number: 1)
    @suppress(["MemberShouldReferenceResource"])
    sessionId: DeviceSessionId

    @required
    @resourceIdentifier("accountId")
    @protoField(number: 2)
    @suppress(["MemberShouldReferenceResource"])
    accountId: AccountId

    @required
    @protoField(number: 3)
    channel: AuthChannel

    @required
    @protoField(number: 4)
    state: DeviceSessionState

    @required
    @protoField(number: 5)
    current: Boolean

    @required
    @protoField(number: 6)
    deviceLabel: DeviceLabel

    @required
    @protoField(number: 7)
    createdAt: DateTime

    @required
    @protoField(number: 8)
    lastSeenAt: DateTime

    @required
    @protoField(number: 9)
    expiresAt: DateTime
}

list DeviceSessionSummaries {
    member: DeviceSessionSummary
}

structure AccountConnectionSummary for AccountConnection {
    @required
    @resourceIdentifier("connectionId")
    @protoField(number: 1)
    @suppress(["MemberShouldReferenceResource"])
    connectionId: AccountConnectionId

    @required
    @resourceIdentifier("accountId")
    @protoField(number: 2)
    @suppress(["MemberShouldReferenceResource"])
    accountId: AccountId

    @required
    @protoField(number: 3)
    issuer: IdentityIssuer

    @required
    @protoField(number: 4)
    subject: SubjectId

    @required
    @protoField(number: 5)
    state: AccountConnectionState

    @required
    @protoField(number: 6)
    email: EmailAddress

    @required
    @protoField(number: 7)
    emailVerified: Boolean

    @required
    @protoField(number: 8)
    createdAt: DateTime

    @required
    @protoField(number: 9)
    lastSeenAt: DateTime
}

list AccountConnectionSummaries {
    member: AccountConnectionSummary
}

structure AuthContext {
    @required
    @protoField(number: 1)
    account: AccountSummary

    @required
    @protoField(number: 2)
    session: DeviceSessionSummary

    @required
    @protoField(number: 3)
    organizations: AuthOrganizationContexts

    @protoField(number: 4)
    selectedOrgId: OrgId

    @required
    @protoField(number: 5)
    authTime: DateTime

    @required
    @protoField(number: 6)
    authMethods: AuthMethods
}

@readonly
@http(method: "GET", uri: "/api/v1/auth-context")
@identity(mode: "bearer", audience: "verself-api", principals: ["cli", "browser", "workload"])
@authz(permission: AccountReadPermission, organization: {source: "request_subject"})
@audit(event: AccountContextReadAuditEvent, resource: Account, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "auth", method: "getContext", paginated: false, retryable: true)
operation GetAuthContext {
    input: GetAuthContextInput
    output: GetAuthContextOutput
    errors: [
        UnauthenticatedError
        DeviceSessionRequiredError
        SessionRevokedError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure GetAuthContextInput {
    @httpHeader("X-Verself-Device-Session")
    @protoField(number: 1)
    @suppress(["MemberShouldReferenceResource"])
    sessionId: DeviceSessionId
}

@output
structure GetAuthContextOutput {
    @required
    @httpPayload
    @nestedProperties
    @notProperty
    @protoField(number: 1)
    context: AuthContext
}

@idempotent
@http(method: "POST", uri: "/api/v1/device-sessions", code: 201)
@identity(mode: "bearer", audience: "verself-api", principals: ["cli", "browser", "workload"])
@authz(permission: DeviceSessionCreatePermission, organization: {source: "request_subject"})
@audit(event: DeviceSessionCreateAuditEvent, resource: DeviceSession, action: "create")
@rateLimit(bucket: "iam_mutation")
@requestBudget(maxBytes: 4096)
@sdk(module: "auth", method: "createDeviceSession", paginated: false, retryable: false)
operation CreateDeviceSession {
    input: CreateDeviceSessionInput
    output: CreateDeviceSessionOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        AccountLinkRequiredError
        ProviderEmailUnverifiedError
        ProviderSubjectAlreadyLinkedError
        IdempotencyPayloadMismatchError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure CreateDeviceSessionInput {
    @required
    @notProperty
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    @protoField(number: 100)
    idempotencyKey: IdempotencyKey

    @required
    @protoField(number: 1)
    channel: AuthChannel

    @required
    @protoField(number: 2)
    deviceLabel: DeviceLabel
}

@output
structure CreateDeviceSessionOutput {
    @required
    @httpPayload
    @nestedProperties
    @notProperty
    @protoField(number: 1)
    context: AuthContext
}

@readonly
@http(method: "GET", uri: "/api/v1/device-sessions")
@identity(mode: "bearer", audience: "verself-api", principals: ["cli", "browser", "workload"])
@authz(permission: DeviceSessionReadPermission, organization: {source: "request_subject"})
@audit(event: DeviceSessionReadAuditEvent, resource: DeviceSession, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "auth", method: "listDeviceSessions", paginated: false, retryable: true)
operation ListDeviceSessions {
    input: ListDeviceSessionsInput
    output: ListDeviceSessionsOutput
    errors: [
        UnauthenticatedError
        DeviceSessionRequiredError
        SessionRevokedError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure ListDeviceSessionsInput {
    @httpHeader("X-Verself-Device-Session")
    @protoField(number: 1)
    @suppress(["MemberShouldReferenceResource"])
    sessionId: DeviceSessionId
}

@output
structure ListDeviceSessionsOutput {
    @required
    @protoField(number: 1)
    sessions: DeviceSessionSummaries
}

@http(method: "DELETE", uri: "/api/v1/device-sessions/{sessionId}", code: 204)
@idempotent
@identity(mode: "bearer", audience: "verself-api", principals: ["cli", "browser", "workload"])
@authz(permission: DeviceSessionDeletePermission, organization: {source: "request_subject"})
@audit(event: DeviceSessionDeleteAuditEvent, resource: DeviceSession, action: "delete")
@rateLimit(bucket: "iam_mutation")
@requestBudget(maxBytes: 1)
@sdk(module: "auth", method: "revokeDeviceSession", paginated: false, retryable: false)
@suppress(["ServiceBoundResourceOperation"])
operation RevokeDeviceSession {
    input: RevokeDeviceSessionInput
    output: RevokeDeviceSessionOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        PermissionDeniedError
        DeviceSessionRequiredError
        SessionRevokedError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure RevokeDeviceSessionInput {
    @required
    @httpLabel
    @protoField(number: 1)
    @suppress(["MemberShouldReferenceResource"])
    sessionId: DeviceSessionId

    @required
    @httpHeader("X-Verself-Device-Session")
    @protoField(number: 2)
    currentSessionId: DeviceSessionId
}

@output
structure RevokeDeviceSessionOutput {}

@readonly
@http(method: "GET", uri: "/api/v1/account-connections")
@identity(mode: "bearer", audience: "verself-api", principals: ["cli", "browser", "workload"])
@authz(permission: AccountConnectionReadPermission, organization: {source: "request_subject"})
@audit(event: AccountConnectionReadAuditEvent, resource: AccountConnection, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "auth", method: "listAccountConnections", paginated: false, retryable: true)
operation ListAccountConnections {
    input: ListAccountConnectionsInput
    output: ListAccountConnectionsOutput
    errors: [
        UnauthenticatedError
        DeviceSessionRequiredError
        SessionRevokedError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure ListAccountConnectionsInput {
    @httpHeader("X-Verself-Device-Session")
    @protoField(number: 1)
    @suppress(["MemberShouldReferenceResource"])
    sessionId: DeviceSessionId
}

@output
structure ListAccountConnectionsOutput {
    @required
    @protoField(number: 1)
    connections: AccountConnectionSummaries
}

@http(method: "DELETE", uri: "/api/v1/account-connections/{connectionId}", code: 204)
@idempotent
@identity(mode: "bearer", audience: "verself-api", principals: ["cli", "browser", "workload"])
@authz(permission: AccountConnectionDeletePermission, organization: {source: "request_subject"})
@audit(event: AccountConnectionDeleteAuditEvent, resource: AccountConnection, action: "delete")
@rateLimit(bucket: "iam_mutation")
@requestBudget(maxBytes: 1)
@sdk(module: "auth", method: "removeAccountConnection", paginated: false, retryable: false)
@suppress(["ServiceBoundResourceOperation"])
operation RemoveAccountConnection {
    input: RemoveAccountConnectionInput
    output: RemoveAccountConnectionOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        PermissionDeniedError
        DeviceSessionRequiredError
        SessionRevokedError
        ReauthenticationRequiredError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure RemoveAccountConnectionInput {
    @required
    @httpLabel
    @protoField(number: 1)
    @suppress(["MemberShouldReferenceResource"])
    connectionId: AccountConnectionId

    @required
    @httpHeader("X-Verself-Device-Session")
    @protoField(number: 2)
    currentSessionId: DeviceSessionId
}

@output
structure RemoveAccountConnectionOutput {}

@readonly
@http(method: "GET", uri: "/api/v1/organization-slugs/{slug}/availability", code: 200)
@identity(mode: "public", audience: "verself-api", principals: ["browser", "cli", "anonymous"])
@authz(permission: OrganizationSlugCheckPermission, organization: {source: "installation"})
@audit(event: OrganizationSlugCheckAuditEvent, resource: Organization, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "orgs", method: "checkSlugAvailability", paginated: false, retryable: true)
operation CheckOrganizationSlugAvailability {
    input: CheckOrganizationSlugAvailabilityInput
    output: CheckOrganizationSlugAvailabilityOutput
    errors: [
        ValidationFailedError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure CheckOrganizationSlugAvailabilityInput {
    @required
    @httpLabel
    @protoField(number: 1)
    slug: OrgSlug
}

@output
structure CheckOrganizationSlugAvailabilityOutput {
    @required
    @httpPayload
    @notProperty
    @protoField(number: 1)
    result: OrganizationSlugAvailability
}

structure OrganizationSlugAvailability {
    @required
    @protoField(number: 1)
    slug: OrgSlug

    @required
    @protoField(number: 2)
    available: Boolean
}

@idempotent
@http(method: "POST", uri: "/api/v1/signup-intents", code: 202)
@identity(mode: "public", audience: "verself-api", principals: ["browser", "cli", "anonymous"])
@authz(permission: SignupIntentCreatePermission, organization: {source: "installation"})
@audit(event: SignupIntentCreateAuditEvent, resource: SignupIntent, action: "create")
@rateLimit(bucket: "signup_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "signup", method: "start", paginated: false, retryable: false)
operation StartSignup {
    input: StartSignupInput
    output: StartSignupOutput
    errors: [
        ValidationFailedError
        IdempotencyPayloadMismatchError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure StartSignupInput {
    @required
    @protoField(number: 1)
    email: EmailAddress

    @required
    @protoField(number: 2)
    organizationDisplayName: DisplayName

    @protoField(number: 3)
    organizationSlug: OrgSlug

    @protoField(number: 4)
    @notProperty
    givenName: GivenName

    @protoField(number: 5)
    @notProperty
    familyName: FamilyName

    @required
    @notProperty
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    @protoField(number: 100)
    idempotencyKey: IdempotencyKey
}

@output
structure StartSignupOutput {
    @required
    @httpPayload
    @notProperty
    @protoField(number: 1)
    result: SignupStartResult
}

structure SignupStartResult {
    @required
    @protoField(number: 1)
    message: String

    @required
    @protoField(number: 2)
    status: SignupStartStatus

    @required
    @protoField(number: 3)
    verificationExpiresAt: DateTime
}

@idempotent
@http(method: "POST", uri: "/api/v1/signup-intents/{signupIntentId}/verification", code: 201)
@identity(mode: "public", audience: "verself-api", principals: ["browser", "cli", "anonymous"])
@authz(permission: SignupIntentVerifyPermission, organization: {source: "installation"})
@audit(event: SignupIntentVerifyAuditEvent, resource: SignupIntent, action: "verify")
@rateLimit(bucket: "signup_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "signup", method: "verify", paginated: false, retryable: false)
operation VerifySignup {
    input: VerifySignupInput
    output: VerifySignupOutput
    errors: [
        ValidationFailedError
        SignupVerificationInvalidError
        SignupVerificationExpiredError
        SignupVerificationAlreadyUsedError
        SignupMaterializingError
        SignupStateConflictError
        SignupAccountExistsError
        OrganizationSlugUnavailableError
        PasswordTooShortError
        PasswordTooLongError
        PasswordBreachedError
        IdempotencyPayloadMismatchError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure VerifySignupInput {
    @required
    @httpLabel
    @protoField(number: 1)
    signupIntentId: SignupIntentId

    @required
    @protoField(number: 2)
    @notProperty
    verificationToken: SignupVerificationToken

    @required
    @protoField(number: 3)
    @notProperty
    initialPassword: Password

    @protoField(number: 4)
    organizationDisplayName: DisplayName

    @protoField(number: 5)
    organizationSlug: OrgSlug

    @required
    @notProperty
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    @protoField(number: 100)
    idempotencyKey: IdempotencyKey
}

@output
structure VerifySignupOutput {
    @required
    @httpPayload
    @notProperty
    @protoField(number: 1)
    result: VerifySignupResult
}

structure VerifySignupResult {
    @required
    @protoField(number: 1)
    organization: OrganizationSummary

    @required
    @protoField(number: 2)
    loginUrl: LoginURL

    @protoField(number: 3)
    loginIntent: RequiredAccountLoginIntent

    @protoField(number: 4)
    warnings: AuthWarnings
}

structure RequiredAccountLoginIntent {
    @required
    @protoField(number: 1)
    loginUrl: LoginURL

    @required
    @protoField(number: 2)
    purpose: BrowserLoginPurpose

    @required
    @protoField(number: 3)
    requiredSubject: SubjectId

    @required
    @protoField(number: 4)
    requiredEmail: EmailAddress

    @required
    @protoField(number: 5)
    requiredOrgId: OrgId

    @protoField(number: 6)
    redirectTo: BrowserRedirectPath
}

structure BrowserDevice {
    @required
    @protoField(number: 1)
    label: DeviceLabel

    @required
    @protoField(number: 2)
    kind: DeviceKind

    @protoField(number: 3)
    browserName: BrowserName

    @protoField(number: 4)
    osName: OperatingSystemName
}

structure BrowserLocation {
    @protoField(number: 1)
    countryCode: CountryCode

    @protoField(number: 2)
    region: RegionName

    @protoField(number: 3)
    city: CityName
}

list BrowserAuthMethods {
    member: String
}

structure BrowserOrganizationContext {
    @required
    @protoField(number: 1)
    @suppress(["MemberShouldReferenceResource"])
    orgId: OrgId

    @required
    @protoField(number: 2)
    identityProviderOrgId: IdentityProviderOrgId
}

list BrowserOrganizationContexts {
    member: BrowserOrganizationContext
}

structure BrowserUser {
    @required
    @protoField(number: 1)
    sub: SubjectId

    @protoField(number: 2)
    email: EmailAddress

    @protoField(number: 3)
    name: DisplayName

    @protoField(number: 4)
    preferredUsername: DisplayName

    @protoField(number: 5)
    homeOrgId: OrgId

    @protoField(number: 6)
    selectedOrgId: OrgId

    @protoField(number: 7)
    @suppress(["MemberShouldReferenceResource"])
    orgId: OrgId

    @required
    @protoField(number: 8)
    availableOrganizations: BrowserOrganizationContexts
}

@references([
    {
        resource: BrowserSession
        ids: {
            sessionHandle: "sessionHandle"
        }
    }
    {
        resource: BrowserAccount
        ids: {
            accountHandle: "accountHandle"
        }
    }
])
structure BrowserSessionInfo {
    @required
    @protoField(number: 1)
    sessionHandle: BrowserSessionHandle

    @required
    @protoField(number: 2)
    clientHandle: BrowserClientHandle

    @required
    @protoField(number: 3)
    accountHandle: BrowserAccountHandle

    @required
    @protoField(number: 4)
    createdAt: DateTime

    @required
    @protoField(number: 5)
    lastSeenAt: DateTime

    @required
    @protoField(number: 6)
    expiresAt: DateTime

    @required
    @protoField(number: 7)
    authMethods: BrowserAuthMethods

    @protoField(number: 8)
    clientIP: ClientIPAddress

    @required
    @protoField(number: 9)
    clientIPTrusted: Boolean

    @protoField(number: 10)
    clientIPSource: ClientIPSource

    @protoField(number: 11)
    edgePeerIP: ClientIPAddress

    @protoField(number: 12)
    userAgent: UserAgent

    @required
    @protoField(number: 13)
    device: BrowserDevice

    @required
    @protoField(number: 14)
    location: BrowserLocation
}

@references([
    {
        resource: BrowserSession
        ids: {
            sessionHandle: "sessionHandle"
        }
    }
])
structure BrowserSessionSummary {
    @required
    @protoField(number: 1)
    sessionHandle: BrowserSessionHandle

    @required
    @protoField(number: 2)
    isCurrent: Boolean

    @required
    @protoField(number: 3)
    createdAt: DateTime

    @required
    @protoField(number: 4)
    lastSeenAt: DateTime

    @required
    @protoField(number: 5)
    expiresAt: DateTime

    @protoField(number: 6)
    createdClientIP: ClientIPAddress

    @required
    @protoField(number: 7)
    createdClientIPTrusted: Boolean

    @protoField(number: 8)
    createdClientIPSource: ClientIPSource

    @protoField(number: 9)
    createdEdgePeerIP: ClientIPAddress

    @protoField(number: 10)
    createdUserAgent: UserAgent

    @required
    @protoField(number: 11)
    createdDevice: BrowserDevice

    @required
    @protoField(number: 12)
    createdLocation: BrowserLocation

    @protoField(number: 13)
    clientIP: ClientIPAddress

    @required
    @protoField(number: 14)
    clientIPTrusted: Boolean

    @protoField(number: 15)
    clientIPSource: ClientIPSource

    @protoField(number: 16)
    edgePeerIP: ClientIPAddress

    @protoField(number: 17)
    userAgent: UserAgent

    @required
    @protoField(number: 18)
    device: BrowserDevice

    @required
    @protoField(number: 19)
    location: BrowserLocation
}

list BrowserSessionSummaries {
    member: BrowserSessionSummary
}

@references([
    {
        resource: BrowserAccount
        ids: {
            accountHandle: "accountHandle"
        }
    }
])
structure BrowserAuthState {
    @required
    @protoField(number: 1)
    isAuthenticated: Boolean

    @protoField(number: 2)
    userId: SubjectId

    @protoField(number: 3)
    @suppress(["MemberShouldReferenceResource"])
    orgId: OrgId

    @protoField(number: 4)
    selectedOrgId: OrgId

    @protoField(number: 5)
    cachePartition: String

    @protoField(number: 6)
    clientHandle: BrowserClientHandle

    @protoField(number: 7)
    accountHandle: BrowserAccountHandle
}

structure BrowserAuthSnapshot {
    @required
    @protoField(number: 1)
    isSignedIn: Boolean

    @required
    @protoField(number: 2)
    auth: BrowserAuthState

    @protoField(number: 3)
    user: BrowserUser

    @protoField(number: 4)
    session: BrowserSessionInfo
}

@references([
    {
        resource: BrowserAccount
        ids: {
            accountHandle: "accountHandle"
        }
    }
])
structure BrowserAccountSummary {
    @required
    @protoField(number: 1)
    accountHandle: BrowserAccountHandle

    @required
    @protoField(number: 2)
    isCurrent: Boolean

    @required
    @protoField(number: 3)
    subject: SubjectId

    @protoField(number: 4)
    email: EmailAddress

    @protoField(number: 5)
    displayName: DisplayName

    @protoField(number: 6)
    preferredUsername: String

    @protoField(number: 7)
    homeOrgID: OrgId

    @protoField(number: 8)
    selectedOrgID: OrgId

    @required
    @protoField(number: 9)
    availableOrganizations: BrowserOrganizationContexts

    @required
    @protoField(number: 10)
    createdAt: DateTime

    @required
    @protoField(number: 11)
    lastSeenAt: DateTime

    @required
    @protoField(number: 12)
    expiresAt: DateTime

    @protoField(number: 13)
    createdClientIP: ClientIPAddress

    @required
    @protoField(number: 14)
    createdClientIPTrusted: Boolean

    @protoField(number: 15)
    createdClientIPSource: ClientIPSource

    @protoField(number: 16)
    createdEdgePeerIP: ClientIPAddress

    @protoField(number: 17)
    createdUserAgent: UserAgent

    @required
    @protoField(number: 18)
    createdDevice: BrowserDevice

    @required
    @protoField(number: 19)
    createdLocation: BrowserLocation

    @protoField(number: 20)
    lastSeenClientIP: ClientIPAddress

    @required
    @protoField(number: 21)
    lastSeenClientIPTrusted: Boolean

    @protoField(number: 22)
    lastSeenClientIPSource: ClientIPSource

    @protoField(number: 23)
    lastSeenEdgePeerIP: ClientIPAddress

    @protoField(number: 24)
    lastSeenUserAgent: UserAgent

    @required
    @protoField(number: 25)
    lastSeenDevice: BrowserDevice

    @required
    @protoField(number: 26)
    lastSeenLocation: BrowserLocation
}

list BrowserAccountSummaries {
    member: BrowserAccountSummary
}

@http(method: "POST", uri: "/api/v1/auth/password-login")
@identity(mode: "public", audience: "verself-api", principals: ["browser", "anonymous"])
@authz(permission: BrowserSessionCreatePermission, organization: {source: "installation"})
@audit(event: BrowserSessionCreateAuditEvent, resource: BrowserSession, action: "create")
@rateLimit(bucket: "signup_mutation")
@requestBudget(maxBytes: 4096)
@sdk(module: "auth", method: "completePasswordLogin", paginated: false, retryable: false)
operation CompletePasswordLogin {
    input: CompletePasswordLoginInput
    output: CompletePasswordLoginOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure CompletePasswordLoginInput {
    @required
    @protoField(number: 1)
    email: EmailAddress

    @required
    @protoField(number: 2)
    password: Password

    @protoField(number: 3)
    redirectTo: BrowserRedirectPath

    @protoField(number: 4)
    purpose: BrowserLoginPurpose

    @protoField(number: 5)
    loginHint: EmailAddress

    @protoField(number: 6)
    requiredSubject: SubjectId

    @protoField(number: 7)
    requiredEmail: EmailAddress

    @protoField(number: 8)
    requiredOrgId: OrgId

    @protoField(number: 9)
    prompt: BrowserLoginPrompt

    @protoField(number: 10)
    authRequestId: OIDCAuthRequestId
}

@output
structure CompletePasswordLoginOutput {
    @required
    @protoField(number: 1)
    callbackUrl: BrowserRedirectURL
}

@http(method: "POST", uri: "/api/v1/auth/password-reset")
@identity(mode: "public", audience: "verself-api", principals: ["browser", "anonymous"])
@authz(permission: PasswordResetCreatePermission, organization: {source: "installation"})
@audit(event: PasswordResetCreateAuditEvent, resource: BrowserAccount, action: "create")
@rateLimit(bucket: "signup_mutation")
@requestBudget(maxBytes: 4096)
@sdk(module: "auth", method: "startPasswordReset", paginated: false, retryable: false)
operation StartPasswordReset {
    input: StartPasswordResetInput
    output: StartPasswordResetOutput
    errors: [
        ValidationFailedError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure StartPasswordResetInput {
    @required
    @protoField(number: 1)
    email: EmailAddress
}

@http(method: "POST", uri: "/api/v1/auth/password-reset/complete")
@identity(mode: "public", audience: "verself-api", principals: ["browser", "anonymous"])
@authz(permission: PasswordResetCompletePermission, organization: {source: "installation"})
@audit(event: PasswordResetCompleteAuditEvent, resource: BrowserAccount, action: "update")
@rateLimit(bucket: "signup_mutation")
@requestBudget(maxBytes: 4096)
@sdk(module: "auth", method: "completePasswordReset", paginated: false, retryable: false)
operation CompletePasswordReset {
    input: CompletePasswordResetInput
    output: CompletePasswordResetOutput
    errors: [
        ValidationFailedError
        PasswordTooShortError
        PasswordTooLongError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure CompletePasswordResetInput {
    @required
    @protoField(number: 1)
    userId: SubjectId

    @required
    @protoField(number: 2)
    @notProperty
    verificationCode: PasswordResetVerificationCode

    @required
    @protoField(number: 3)
    @notProperty
    password: Password
}

@output
structure StartPasswordResetOutput {
    @required
    @protoField(number: 1)
    status: String

    @required
    @protoField(number: 2)
    message: String
}

@output
structure CompletePasswordResetOutput {
    @required
    @protoField(number: 1)
    status: String

    @required
    @protoField(number: 2)
    message: String
}

@http(method: "POST", uri: "/api/v1/auth/device-logins/lookup")
@identity(mode: "public", audience: "verself-api", principals: ["browser", "anonymous"])
@authz(permission: DeviceLoginReadPermission, organization: {source: "installation"})
@audit(event: DeviceLoginReadAuditEvent, resource: DeviceLogin, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 4096)
@sdk(module: "auth", method: "lookupDeviceLogin", paginated: false, retryable: true)
@suppress(["ServiceBoundResourceOperation"])
operation LookupDeviceLogin {
    input: LookupDeviceLoginInput
    output: LookupDeviceLoginOutput
    errors: [
        ValidationFailedError
        ResourceNotFoundError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure LookupDeviceLoginInput {
    @required
    @protoField(number: 1)
    userCode: DeviceUserCode
}

@http(method: "POST", uri: "/api/v1/auth/device-logins/{deviceLoginHandle}/approval")
@identity(mode: "browser_session", audience: "verself-api", principals: ["browser"])
@authz(permission: DeviceLoginApprovePermission, organization: {source: "request_subject"})
@audit(event: DeviceLoginApproveAuditEvent, resource: DeviceLogin, action: "update")
@rateLimit(bucket: "iam_mutation")
@requestBudget(maxBytes: 4096)
@sdk(module: "auth", method: "approveDeviceLogin", paginated: false, retryable: false)
@suppress(["ServiceBoundResourceOperation"])
operation ApproveDeviceLogin {
    input: ApproveDeviceLoginInput
    output: ApproveDeviceLoginOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        ConflictError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@http(method: "POST", uri: "/api/v1/auth/device-logins/{deviceLoginHandle}/denial")
@identity(mode: "browser_session", audience: "verself-api", principals: ["browser"])
@authz(permission: DeviceLoginDenyPermission, organization: {source: "request_subject"})
@audit(event: DeviceLoginDenyAuditEvent, resource: DeviceLogin, action: "update")
@rateLimit(bucket: "iam_mutation")
@requestBudget(maxBytes: 4096)
@sdk(module: "auth", method: "denyDeviceLogin", paginated: false, retryable: false)
@suppress(["ServiceBoundResourceOperation"])
operation DenyDeviceLogin {
    input: DenyDeviceLoginInput
    output: DenyDeviceLoginOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        ConflictError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure ApproveDeviceLoginInput {
    @required
    @httpLabel
    @protoField(number: 1)
    @suppress(["MemberShouldReferenceResource"])
    deviceLoginHandle: DeviceLoginHandle
}

@output
structure ApproveDeviceLoginOutput with [DeviceLoginOutputFields] {}

@input
structure DenyDeviceLoginInput {
    @required
    @httpLabel
    @protoField(number: 1)
    @suppress(["MemberShouldReferenceResource"])
    deviceLoginHandle: DeviceLoginHandle
}

@output
structure DenyDeviceLoginOutput with [DeviceLoginOutputFields] {}

@output
structure LookupDeviceLoginOutput with [DeviceLoginOutputFields] {}

@mixin
structure DeviceLoginOutputFields {
    @required
    @protoField(number: 1)
    @suppress(["MemberShouldReferenceResource"])
    deviceLoginHandle: DeviceLoginHandle

    @required
    @protoField(number: 2)
    userCodeSuffix: String

    @required
    @protoField(number: 3)
    clientId: String

    @required
    @protoField(number: 4)
    appName: String

    @required
    @protoField(number: 5)
    projectName: String

    @required
    @protoField(number: 6)
    scopes: OAuthScopes

    @required
    @protoField(number: 7)
    state: DeviceLoginState

    @required
    @protoField(number: 8)
    approvedAt: String

    @required
    @protoField(number: 9)
    deniedAt: String

    @required
    @protoField(number: 10)
    expiresAt: DateTime

    @required
    @protoField(number: 11)
    updatedAt: DateTime
}

@readonly
@http(method: "GET", uri: "/api/v1/auth/callback", code: 303)
@suppress(["HttpResponseCodeSemantics"])
@identity(mode: "public", audience: "verself-api", principals: ["browser", "anonymous"])
@authz(permission: BrowserSessionCreatePermission, organization: {source: "installation"})
@audit(event: BrowserSessionCreateAuditEvent, resource: BrowserSession, action: "create")
@rateLimit(bucket: "signup_mutation")
@requestBudget(maxBytes: 0)
@sdk(module: "auth", method: "callback", paginated: false, retryable: false)
operation BrowserAuthCallback {
    input: BrowserAuthCallbackInput
    output: BrowserAuthCallbackOutput
    errors: [
        ValidationFailedError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure BrowserAuthCallbackInput {
    @httpQuery("code")
    @protoField(number: 1)
    code: OAuthAuthorizationCode

    @httpQuery("state")
    @protoField(number: 2)
    state: OAuthState

    @httpQuery("error")
    @protoField(number: 3)
    error: String

    @httpQuery("error_description")
    @protoField(number: 4)
    errorDescription: OAuthErrorDescription
}

@output
structure BrowserAuthCallbackOutput {
    @httpHeader("Location")
    @protoField(number: 1)
    location: BrowserRedirectURL
}

@readonly
@http(method: "GET", uri: "/api/v1/auth/session")
@identity(mode: "browser_session", audience: "verself-api", principals: ["browser", "anonymous"])
@authz(permission: BrowserSessionReadPermission, organization: {source: "installation"})
@audit(event: BrowserSessionReadAuditEvent, resource: BrowserSession, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "auth", method: "getSession", paginated: false, retryable: true)
operation GetBrowserSession {
    input: GetBrowserSessionInput
    output: GetBrowserSessionOutput
    errors: [
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure GetBrowserSessionInput {}

@output
structure GetBrowserSessionOutput {
    @required
    @protoField(number: 1)
    snapshot: BrowserAuthSnapshot
}

@idempotent
@http(method: "POST", uri: "/api/v1/auth/organization")
@suppress(["ServiceBoundResourceOperation"])
@identity(mode: "browser_session", audience: "verself-api", principals: ["browser"])
@authz(permission: BrowserSessionUpdatePermission, organization: {source: "body_org_id", member: "orgId"})
@audit(event: BrowserSessionUpdateAuditEvent, resource: BrowserSession, action: "update")
@rateLimit(bucket: "iam_mutation")
@requestBudget(maxBytes: 4096)
@sdk(module: "auth", method: "selectOrganization", paginated: false, retryable: false)
operation SelectBrowserOrganization {
    input: SelectBrowserOrganizationInput
    output: SelectBrowserOrganizationOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        PermissionDeniedError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure SelectBrowserOrganizationInput {
    @required
    @protoField(number: 1)
    @suppress(["MemberShouldReferenceResource"])
    orgId: OrgId
}

@output
structure SelectBrowserOrganizationOutput {
    @required
    @protoField(number: 1)
    snapshot: BrowserAuthSnapshot
}

@http(method: "POST", uri: "/api/v1/auth/resource-token")
@identity(mode: "browser_session", audience: "verself-api", principals: ["browser"])
@authz(permission: BrowserResourceTokenCreatePermission, organization: {source: "request_subject"})
@audit(event: BrowserResourceTokenCreateAuditEvent, resource: BrowserSession, action: "create")
@rateLimit(bucket: "iam_mutation")
@requestBudget(maxBytes: 0)
@sdk(module: "auth", method: "createResourceToken", paginated: false, retryable: false)
operation CreateBrowserResourceToken {
    input: CreateBrowserResourceTokenInput
    output: CreateBrowserResourceTokenOutput
    errors: [
        UnauthenticatedError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure CreateBrowserResourceTokenInput {}

@output
structure CreateBrowserResourceTokenOutput {
    @required
    @protoField(number: 1)
    accessToken: BrowserAccessToken
}

@readonly
@http(method: "GET", uri: "/api/v1/auth/logout", code: 303)
@suppress(["HttpResponseCodeSemantics"])
@identity(mode: "browser_session", audience: "verself-api", principals: ["browser", "anonymous"])
@authz(permission: BrowserSessionDeletePermission, organization: {source: "installation"})
@audit(event: BrowserSessionDeleteAuditEvent, resource: BrowserSession, action: "delete")
@rateLimit(bucket: "iam_mutation")
@requestBudget(maxBytes: 0)
@sdk(module: "auth", method: "logout", paginated: false, retryable: false)
operation LogoutBrowserSession {
    input: LogoutBrowserSessionInput
    output: LogoutBrowserSessionOutput
    errors: [
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure LogoutBrowserSessionInput {}

@output
structure LogoutBrowserSessionOutput {
    @httpHeader("Location")
    @protoField(number: 1)
    location: BrowserRedirectURL
}

@readonly
@http(method: "GET", uri: "/api/v1/auth/accounts")
@identity(mode: "browser_session", audience: "verself-api", principals: ["browser"])
@authz(permission: BrowserSessionReadPermission, organization: {source: "request_subject"})
@audit(event: BrowserSessionReadAuditEvent, resource: BrowserAccount, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "auth", method: "listAccounts", paginated: false, retryable: true)
operation ListBrowserAccounts {
    input: ListBrowserAccountsInput
    output: ListBrowserAccountsOutput
    errors: [
        UnauthenticatedError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure ListBrowserAccountsInput {}

@output
structure ListBrowserAccountsOutput {
    @required
    @protoField(number: 1)
    accounts: BrowserAccountSummaries
}

@idempotent
@http(method: "POST", uri: "/api/v1/auth/accounts")
@identity(mode: "browser_session", audience: "verself-api", principals: ["browser"])
@authz(permission: BrowserSessionUpdatePermission, organization: {source: "request_subject"})
@audit(event: BrowserSessionUpdateAuditEvent, resource: BrowserAccount, action: "update")
@rateLimit(bucket: "iam_mutation")
@requestBudget(maxBytes: 4096)
@sdk(module: "auth", method: "selectAccount", paginated: false, retryable: false)
operation SelectBrowserAccount {
    input: SelectBrowserAccountInput
    output: SelectBrowserAccountOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        PermissionDeniedError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
@references([
    {
        resource: BrowserAccount
        ids: {
            accountHandle: "accountHandle"
        }
    }
])
structure SelectBrowserAccountInput {
    @required
    @protoField(number: 1)
    accountHandle: BrowserAccountHandle
}

@output
structure SelectBrowserAccountOutput {
    @required
    @protoField(number: 1)
    snapshot: BrowserAuthSnapshot
}

@http(method: "DELETE", uri: "/api/v1/auth/accounts/{accountHandle}", code: 204)
@idempotent
@identity(mode: "browser_session", audience: "verself-api", principals: ["browser"])
@authz(permission: BrowserSessionDeletePermission, organization: {source: "request_subject"})
@audit(event: BrowserSessionDeleteAuditEvent, resource: BrowserAccount, action: "delete")
@rateLimit(bucket: "iam_mutation")
@requestBudget(maxBytes: 0)
@sdk(module: "auth", method: "removeAccount", paginated: false, retryable: false)
operation RemoveBrowserAccount {
    input: RemoveBrowserAccountInput
    output: RemoveBrowserAccountOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
@references([
    {
        resource: BrowserAccount
        ids: {
            accountHandle: "accountHandle"
        }
    }
])
structure RemoveBrowserAccountInput {
    @required
    @httpLabel
    @protoField(number: 1)
    accountHandle: BrowserAccountHandle
}

@output
structure RemoveBrowserAccountOutput {}

@readonly
@http(method: "GET", uri: "/api/v1/auth/sessions")
@identity(mode: "browser_session", audience: "verself-api", principals: ["browser"])
@authz(permission: BrowserSessionReadPermission, organization: {source: "request_subject"})
@audit(event: BrowserSessionReadAuditEvent, resource: BrowserSession, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "auth", method: "listSessions", paginated: false, retryable: true)
operation ListBrowserSessions {
    input: ListBrowserSessionsInput
    output: ListBrowserSessionsOutput
    errors: [
        UnauthenticatedError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure ListBrowserSessionsInput {}

@output
structure ListBrowserSessionsOutput {
    @required
    @protoField(number: 1)
    sessions: BrowserSessionSummaries
}

@http(method: "DELETE", uri: "/api/v1/auth/sessions/{sessionHandle}", code: 204)
@idempotent
@identity(mode: "browser_session", audience: "verself-api", principals: ["browser"])
@authz(permission: BrowserSessionDeletePermission, organization: {source: "request_subject"})
@audit(event: BrowserSessionDeleteAuditEvent, resource: BrowserSession, action: "delete")
@rateLimit(bucket: "iam_mutation")
@requestBudget(maxBytes: 0)
@sdk(module: "auth", method: "revokeSession", paginated: false, retryable: false)
operation RevokeBrowserSession {
    input: RevokeBrowserSessionInput
    output: RevokeBrowserSessionOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure RevokeBrowserSessionInput {
    @required
    @httpLabel
    @protoField(number: 1)
    @suppress(["MemberShouldReferenceResource"])
    sessionHandle: BrowserSessionHandle
}

@output
structure RevokeBrowserSessionOutput {}

@idempotent
@http(method: "POST", uri: "/api/v1/auth/invites/accept")
@identity(mode: "public", audience: "verself-api", principals: ["browser", "cli", "anonymous"])
@authz(permission: MemberInviteAcceptPermission, organization: {source: "installation"})
@audit(event: MemberInviteAcceptAuditEvent, resource: Member, action: "verify")
@rateLimit(bucket: "signup_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "auth", method: "acceptMemberInvite", paginated: false, retryable: false)
operation AcceptMemberInvite {
    input: AcceptMemberInviteInput
    output: AcceptMemberInviteOutput
    errors: [
        ValidationFailedError
        ConflictError
        IdempotencyPayloadMismatchError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure AcceptMemberInviteInput {
    @required
    @protoField(number: 1)
    acceptanceToken: MemberInviteAcceptanceToken

    @required
    @notProperty
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    @protoField(number: 100)
    idempotencyKey: IdempotencyKey
}

@output
structure AcceptMemberInviteOutput {
    @required
    @httpPayload
    @nestedProperties
    @notProperty
    @protoField(number: 1)
    acceptance: MemberInviteAcceptanceSummary
}

structure MemberInviteAcceptanceSummary {
    @required
    @resourceIdentifier("orgId")
    @protoField(number: 1)
    @suppress(["MemberShouldReferenceResource"])
    orgId: OrgId

    @required
    @resourceIdentifier("memberId")
    @protoField(number: 2)
    @suppress(["MemberShouldReferenceResource"])
    memberId: MemberId

    @required
    @protoField(number: 3)
    resourceName: MemberResourceName

    @required
    @protoField(number: 4)
    loginUrl: LoginURL

    @protoField(number: 5)
    loginIntent: RequiredAccountLoginIntent
}

@readonly
@http(method: "GET", uri: "/api/v1/orgs")
@paginated(inputToken: "pageToken", outputToken: "nextPageToken", pageSize: "pageSize", items: "organizations")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: OrganizationListPermission, organization: {source: "request_subject"})
@audit(event: OrganizationListAuditEvent, resource: Organization, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "orgs", method: "list", paginated: true, retryable: true)
operation ListOrganizations {
    input: ListOrganizationsInput
    output: ListOrganizationsOutput
    errors: [
        UnauthenticatedError
        PermissionDeniedError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure ListOrganizationsInput with [PageRequest] {}

@output
structure ListOrganizationsOutput with [PageResponse] {
    @required
    @protoField(number: 1)
    organizations: Organizations
}

list Organizations {
    member: OrganizationSummary
}

@idempotent
@http(method: "POST", uri: "/api/v1/orgs", code: 201)
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: OrganizationCreatePermission, organization: {source: "request_subject_id"})
@audit(event: OrganizationCreateAuditEvent, resource: Organization, action: "create")
@rateLimit(bucket: "signup_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "orgs", method: "create", paginated: false, retryable: false)
operation CreateOrganization {
    input: CreateOrganizationInput
    output: CreateOrganizationOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        PermissionDeniedError
        ConflictError
        IdempotencyPayloadMismatchError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure CreateOrganizationInput for Organization {
    @required
    @protoField(number: 1)
    $displayName

    @protoField(number: 2)
    $slug

    @required
    @notProperty
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    @protoField(number: 100)
    idempotencyKey: IdempotencyKey
}

@output
structure CreateOrganizationOutput {
    @required
    @httpPayload
    @nestedProperties
    @notProperty
    @protoField(number: 1)
    organization: OrganizationSummary
}

@readonly
@http(method: "GET", uri: "/api/v1/orgs/{orgId}")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: OrganizationReadPermission, organization: {source: "input_member", member: "orgId"})
@audit(event: OrganizationReadAuditEvent, resource: Organization, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "orgs", method: "get", paginated: false, retryable: true)
operation GetOrganization {
    input: GetOrganizationInput
    output: GetOrganizationOutput
    errors: [
        UnauthenticatedError
        PermissionDeniedError
        ResourceNotFoundError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure GetOrganizationInput for Organization {
    @required
    @httpLabel
    @protoField(number: 1)
    $orgId
}

@output
structure GetOrganizationOutput {
    @required
    @httpPayload
    @nestedProperties
    @notProperty
    @protoField(number: 1)
    organization: OrganizationSummary
}

@idempotent
@http(method: "PATCH", uri: "/api/v1/orgs/{orgId}")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: OrganizationUpdatePermission, organization: {source: "input_member", member: "orgId"})
@audit(event: OrganizationUpdateAuditEvent, resource: Organization, action: "update")
@rateLimit(bucket: "iam_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "orgs", method: "update", paginated: false, retryable: false)
operation UpdateOrganization {
    input: UpdateOrganizationInput
    output: UpdateOrganizationOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        PermissionDeniedError
        ResourceNotFoundError
        ConflictError
        IdempotencyPayloadMismatchError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure UpdateOrganizationInput for Organization {
    @required
    @httpLabel
    @protoField(number: 1)
    $orgId

    @protoField(number: 2)
    $slug

    @protoField(number: 3)
    $displayName

    @required
    @protoField(number: 4)
    $version

    @required
    @notProperty
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    @protoField(number: 100)
    idempotencyKey: IdempotencyKey
}

@output
structure UpdateOrganizationOutput {
    @required
    @httpPayload
    @nestedProperties
    @notProperty
    @protoField(number: 1)
    organization: OrganizationSummary
}

@readonly
@http(method: "GET", uri: "/api/v1/orgs/{orgId}/members")
@paginated(inputToken: "pageToken", outputToken: "nextPageToken", pageSize: "pageSize", items: "members")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: MemberListPermission, organization: {source: "input_member", member: "orgId"})
@audit(event: MemberListAuditEvent, resource: Member, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "members", method: "list", paginated: true, retryable: true)
operation ListMembers {
    input: ListMembersInput
    output: ListMembersOutput
    errors: [
        UnauthenticatedError
        PermissionDeniedError
        ResourceNotFoundError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure ListMembersInput for Member with [PageRequest] {
    @required
    @httpLabel
    @protoField(number: 1)
    $orgId
}

@output
structure ListMembersOutput with [PageResponse] {
    @required
    @protoField(number: 1)
    members: Members
}

list Members {
    member: MemberSummary
}

@idempotent
@http(method: "POST", uri: "/api/v1/orgs/{orgId}/members:invite", code: 202)
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: MemberInvitePermission, organization: {source: "input_member", member: "orgId"})
@audit(event: MemberInviteAuditEvent, resource: Member, action: "invite")
@rateLimit(bucket: "iam_invite")
@requestBudget(maxBytes: 16384)
@sdk(module: "members", method: "invite", paginated: false, retryable: false)
@suppress(["ServiceBoundResourceOperation"])
operation InviteMember {
    input: InviteMemberInput
    output: InviteMemberOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        PermissionDeniedError
        ResourceNotFoundError
        ConflictError
        IdempotencyPayloadMismatchError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure InviteMemberInput {
    @required
    @httpLabel
    @protoField(number: 1)
    @suppress(["MemberShouldReferenceResource"])
    orgId: OrgId

    @required
    @protoField(number: 2)
    email: EmailAddress

    @notProperty
    @protoField(number: 3)
    givenName: GivenName

    @notProperty
    @protoField(number: 4)
    familyName: FamilyName

    @notProperty
    @protoField(number: 5)
    roles: InviteMemberRoles

    @required
    @notProperty
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    @protoField(number: 100)
    idempotencyKey: IdempotencyKey
}

@output
structure InviteMemberOutput {
    @required
    @httpPayload
    @nestedProperties
    @notProperty
    @protoField(number: 1)
    invitation: MemberInvitationSummary
}

@length(min: 1, max: 8)
list InviteMemberRoles {
    member: IAMRoleName
}

structure MemberInvitationSummary {
    @required
    @resourceIdentifier("orgId")
    @protoField(number: 1)
    @suppress(["MemberShouldReferenceResource"])
    orgId: OrgId

    @required
    @resourceIdentifier("memberId")
    @protoField(number: 2)
    @suppress(["MemberShouldReferenceResource"])
    memberId: MemberId

    @required
    @protoField(number: 3)
    resourceName: MemberResourceName

    @required
    @protoField(number: 4)
    email: EmailAddress

    @required
    @protoField(number: 5)
    status: MemberInvitationStatus

    @required
    @protoField(number: 6)
    roles: InviteMemberRoles
}

@length(min: 1, max: 32)
string MemberInvitationStatus

@readonly
@http(method: "GET", uri: "/api/v1/orgs/{orgId}/members/{memberId}")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: MemberReadPermission, organization: {source: "input_member", member: "orgId"})
@audit(event: MemberReadAuditEvent, resource: Member, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "members", method: "get", paginated: false, retryable: true)
operation GetMember {
    input: GetMemberInput
    output: GetMemberOutput
    errors: [
        UnauthenticatedError
        PermissionDeniedError
        ResourceNotFoundError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure GetMemberInput for Member {
    @required
    @httpLabel
    @protoField(number: 1)
    $orgId

    @required
    @httpLabel
    @protoField(number: 2)
    $memberId
}

@output
structure GetMemberOutput {
    @required
    @httpPayload
    @nestedProperties
    @notProperty
    @protoField(number: 1)
    member: MemberSummary
}

structure IAMPolicy {
    @required
    @protoField(number: 1)
    version: PolicyVersion

    @required
    @protoField(number: 2)
    bindings: IAMPolicyBindings

    @protoField(number: 3)
    etag: PolicyEtag
}

list IAMPolicyBindings {
    member: IAMPolicyBinding
}

structure IAMPolicyBinding {
    @required
    @protoField(number: 1)
    role: IAMRoleName

    @required
    @protoField(number: 2)
    members: IAMMembers
}

list IAMMembers {
    member: IAMMemberName
}

@readonly
@http(method: "GET", uri: "/api/v1/orgs/{orgId}/iamPolicy")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: IAMPolicyGetPermission, organization: {source: "input_member", member: "orgId"})
@audit(event: IAMPolicyGetAuditEvent, resource: Organization, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 8192)
@sdk(module: "iamPolicies", method: "get", paginated: false, retryable: true)
operation GetIamPolicy {
    input: GetIamPolicyInput
    output: GetIamPolicyOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        PermissionDeniedError
        ResourceNotFoundError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure GetIamPolicyInput for Organization {
    @required
    @httpLabel
    @protoField(number: 1)
    $orgId
}

@output
structure GetIamPolicyOutput {
    @required
    @httpPayload
    @notProperty
    @protoField(number: 1)
    policy: IAMPolicy
}

@idempotent
@http(method: "POST", uri: "/api/v1/orgs/{orgId}/iamPolicy:set")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: IAMPolicySetPermission, organization: {source: "input_member", member: "orgId"})
@audit(event: IAMPolicySetAuditEvent, resource: Organization, action: "set")
@rateLimit(bucket: "iam_mutation")
@requestBudget(maxBytes: 32768)
@sdk(module: "iamPolicies", method: "set", paginated: false, retryable: false)
operation SetIamPolicy {
    input: SetIamPolicyInput
    output: SetIamPolicyOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        PermissionDeniedError
        ResourceNotFoundError
        ConflictError
        IdempotencyPayloadMismatchError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure SetIamPolicyInput for Organization {
    @required
    @httpLabel
    @protoField(number: 1)
    $orgId

    @required
    @notProperty
    @protoField(number: 2)
    policy: IAMPolicy

    @required
    @notProperty
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    @protoField(number: 100)
    idempotencyKey: IdempotencyKey
}

@output
structure SetIamPolicyOutput {
    @required
    @httpPayload
    @notProperty
    @protoField(number: 1)
    policy: IAMPolicy
}

@http(method: "POST", uri: "/api/v1/orgs/{orgId}/iamPolicy:testPermissions")
@operationSemantics(effect: "read")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: IAMPolicyTestPermission, organization: {source: "input_member", member: "orgId"})
@audit(event: IAMPolicyTestAuditEvent, resource: Organization, action: "test")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 8192)
@sdk(module: "iamPolicies", method: "testPermissions", paginated: false, retryable: true)
operation TestIamPermissions {
    input: TestIamPermissionsInput
    output: TestIamPermissionsOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        PermissionDeniedError
        ResourceNotFoundError
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure TestIamPermissionsInput for Organization {
    @required
    @httpLabel
    @protoField(number: 1)
    $orgId

    @required
    @notProperty
    @protoField(number: 2)
    permissions: Permissions
}

@output
structure TestIamPermissionsOutput {
    @required
    @notProperty
    @protoField(number: 1)
    permissions: Permissions
}

structure IAMAuthorizationSubject {
    @required
    @protoField(number: 1)
    type: IAMAuthorizationSubjectType

    @required
    @protoField(number: 2)
    id: SubjectId
}

structure IAMResourceRef {
    @required
    @protoField(number: 1)
    type: AuthorizationResourceType

    @required
    @protoField(number: 2)
    id: AuthorizationResourceId
}

structure OrganizationProfile {
    @required
    @protoField(number: 1)
    identity_provider_org_id: IdentityProviderOrgId

    @required
    @protoField(number: 2)
    slug: OrgSlug

    @required
    @protoField(number: 3)
    display_name: DisplayName

    @required
    @protoField(number: 4)
    state: String

    @required
    @protoField(number: 5)
    version: OrganizationVersion

    @required
    @protoField(number: 6)
    updated_at: DateTime

    @protoField(number: 7)
    redirected_from: OrgSlug

    @protoField(number: 8)
    resourceName: OrganizationResourceName
}

structure HumanProfileSummary {
    @required
    @resourceIdentifier("subjectId")
    @protoField(number: 1)
    subject_id: SubjectId

    @required
    @protoField(number: 2)
    email: EmailAddress

    @required
    @protoField(number: 3)
    given_name: GivenName

    @required
    @protoField(number: 4)
    family_name: FamilyName

    @required
    @protoField(number: 5)
    display_name: DisplayName

    @required
    @protoField(number: 6)
    synced_at: DateTime
}

structure AuthorizeOperationResult {
    @required
    @protoField(number: 1)
    org_id: OrgId

    @required
    @protoField(number: 2)
    subject: IAMAuthorizationSubject

    @required
    @protoField(number: 3)
    permissions: Permissions

    @protoField(number: 4)
    zed_token: ZedToken
}

structure AuthorizeResourceResult {
    @required
    @protoField(number: 1)
    org_id: OrgId

    @required
    @protoField(number: 2)
    subject: IAMAuthorizationSubject

    @protoField(number: 3)
    operation_permission: PermissionName

    @required
    @protoField(number: 4)
    resource: IAMResourceRef

    @required
    @protoField(number: 5)
    resource_permission: ResourcePermissionName

    @required
    @protoField(number: 6)
    allowed: Boolean

    @protoField(number: 7)
    zed_token: ZedToken
}

structure WriteResourceParentEdgeResult {
    @required
    @protoField(number: 1)
    resource: IAMResourceRef

    @required
    @protoField(number: 2)
    relation: AuthorizationRelation

    @required
    @protoField(number: 3)
    parent: IAMResourceRef

    @protoField(number: 4)
    zed_token: ZedToken

    @protoField(number: 5)
    operation: PermissionName
}

@http(method: "POST", uri: "/internal/v1/organizations/resolve")
@identity(mode: "spiffe_mtls", audience: "iam-service", principals: ["workload"])
@authz(permission: OrganizationResolvePermission, organization: {source: "request_subject"})
@audit(event: OrganizationResolveAuditEvent, resource: Organization, action: "read")
@rateLimit(bucket: "internal")
@requestBudget(maxBytes: 8192)
@sdk(module: "iamInternal", method: "resolveOrganization", paginated: false, retryable: true)
operation ResolveOrganization {
    input: ResolveOrganizationInput
    output: ResolveOrganizationOutput
    errors: [
        ValidationFailedError
        PermissionDeniedError
        ResourceNotFoundError
        ServiceUnavailableError
    ]
}

@input
structure ResolveOrganizationInput {
    org_id: OrgId

    identity_provider_org_id: IdentityProviderOrgId

    slug: OrgSlug

    @required
    require_active: Boolean
}

@output
structure ResolveOrganizationOutput {
    @required
    @protoField(number: 1)
    organization: OrganizationProfile
}

@http(method: "PATCH", uri: "/internal/v1/subjects/{subject_id}/human-profile")
@identity(mode: "spiffe_mtls_bearer", audience: "iam-service", principals: ["workload"])
@authz(permission: HumanProfileWritePermission, organization: {source: "request_subject"})
@audit(event: HumanProfileWriteAuditEvent, resource: HumanProfile, action: "write")
@rateLimit(bucket: "internal_mutation")
@requestBudget(maxBytes: 8192)
@sdk(module: "iamInternal", method: "updateHumanProfile", paginated: false, retryable: false)
operation UpdateHumanProfile {
    input: UpdateHumanProfileInput
    output: UpdateHumanProfileOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        PermissionDeniedError
        ConflictError
        ServiceUnavailableError
    ]
}

@input
structure UpdateHumanProfileInput {
    @required
    @httpLabel
    @protoField(number: 1)
    subject_id: SubjectId

    @required
    @protoField(number: 2)
    given_name: GivenName

    @required
    @protoField(number: 3)
    family_name: FamilyName

    @protoField(number: 4)
    display_name: DisplayName
}

@output
structure UpdateHumanProfileOutput {
    @required
    @httpPayload
    @nestedProperties
    @notProperty
    @protoField(number: 1)
    profile: HumanProfileSummary
}

@http(method: "POST", uri: "/internal/v1/authorization/authorize")
@identity(mode: "spiffe_mtls", audience: "iam-service", principals: ["workload"])
@authz(permission: AuthorizationCheckPermission, organization: {source: "input_member", member: "org_id"})
@audit(event: AuthorizationCheckAuditEvent, resource: Authorization, action: "test")
@rateLimit(bucket: "internal")
@requestBudget(maxBytes: 8192)
@sdk(module: "iamInternal", method: "authorizeOperation", paginated: false, retryable: true)
operation AuthorizeOperation {
    input: AuthorizeOperationInput
    output: AuthorizeOperationOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        PermissionDeniedError
        ServiceUnavailableError
    ]
}

@input
structure AuthorizeOperationInput {
    @required
    @protoField(number: 1)
    org_id: OrgId

    @required
    @protoField(number: 2)
    subject: IAMAuthorizationSubject

    @required
    @protoField(number: 3)
    permissions: Permissions

    @protoField(number: 4)
    min_zed_token: ZedToken
}

@output
structure AuthorizeOperationOutput {
    @required
    @httpPayload
    @nestedProperties
    @notProperty
    @protoField(number: 1)
    authorization: AuthorizeOperationResult
}

@http(method: "POST", uri: "/internal/v1/authorization/resources/authorize")
@identity(mode: "spiffe_mtls", audience: "iam-service", principals: ["workload"])
@authz(permission: AuthorizationResourceCheckPermission, organization: {source: "input_member", member: "org_id"})
@audit(event: AuthorizationResourceCheckAuditEvent, resource: Authorization, action: "test")
@rateLimit(bucket: "internal")
@requestBudget(maxBytes: 8192)
@sdk(module: "iamInternal", method: "authorizeResource", paginated: false, retryable: true)
operation AuthorizeResource {
    input: AuthorizeResourceInput
    output: AuthorizeResourceOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        PermissionDeniedError
        ServiceUnavailableError
    ]
}

@input
structure AuthorizeResourceInput {
    @required
    @protoField(number: 1)
    org_id: OrgId

    @required
    @protoField(number: 2)
    subject: IAMAuthorizationSubject

    @protoField(number: 3)
    operation_permission: PermissionName

    @required
    @protoField(number: 4)
    resource: IAMResourceRef

    @required
    @protoField(number: 5)
    resource_permission: ResourcePermissionName

    @protoField(number: 6)
    min_zed_token: ZedToken
}

@output
structure AuthorizeResourceOutput {
    @required
    @httpPayload
    @nestedProperties
    @notProperty
    @protoField(number: 1)
    authorization: AuthorizeResourceResult
}

@http(method: "POST", uri: "/internal/v1/authorization/resources/parent-edge")
@identity(mode: "spiffe_mtls", audience: "iam-service", principals: ["workload"])
@authz(permission: AuthorizationParentEdgeWritePermission, organization: {source: "input_member", member: "org_id"})
@audit(event: AuthorizationParentEdgeWriteAuditEvent, resource: Authorization, action: "write")
@rateLimit(bucket: "internal_mutation")
@requestBudget(maxBytes: 8192)
@sdk(module: "iamInternal", method: "writeResourceParentEdge", paginated: false, retryable: false)
operation WriteResourceParentEdge {
    input: WriteResourceParentEdgeInput
    output: WriteResourceParentEdgeOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        PermissionDeniedError
        ConflictError
        ServiceUnavailableError
    ]
}

@input
structure WriteResourceParentEdgeInput {
    @required
    @protoField(number: 1)
    org_id: OrgId

    @required
    @protoField(number: 2)
    resource: IAMResourceRef

    @required
    @protoField(number: 3)
    relation: AuthorizationRelation

    @required
    @protoField(number: 4)
    parent: IAMResourceRef

    @protoField(number: 5)
    operation: PermissionName
}

@output
structure WriteResourceParentEdgeOutput {
    @required
    @httpPayload
    @nestedProperties
    @notProperty
    @protoField(number: 1)
    edge: WriteResourceParentEdgeResult
}
