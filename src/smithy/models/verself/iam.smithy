$version: "2"

namespace verself.iam.v1

use aws.protocols#restJson1

use smithy.api#auth
use smithy.api#enumValue
use smithy.api#http
use smithy.api#httpBearerAuth
use smithy.api#httpHeader
use smithy.api#httpLabel
use smithy.api#httpPayload
use smithy.api#httpQuery
use smithy.api#idempotencyToken
use smithy.api#idempotent
use smithy.api#input
use smithy.api#length
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
        BrowserLogin
        BrowserAuthCallback
        GetBrowserSession
        SelectBrowserOrganization
        CreateBrowserResourceToken
        LogoutBrowserSession
        ListBrowserSessions
        RevokeBrowserSession
        StartSignup
        VerifySignup
        AcceptMemberInvite
        InviteMember
    ]
    resources: [
        BrowserSession
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

@pattern("^urn:verself:inst_[0-9A-HJKMNP-TV-Z]{26}:signup-intents/signup_[0-9A-HJKMNP-TV-Z]{26}$")
string SignupIntentResourceName

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

@length(min: 27, max: 27)
@pattern("^bs_[A-Za-z0-9_-]{24}$")
string BrowserSessionHandle

@length(min: 1, max: 2048)
string BrowserRedirectPath

@length(min: 1, max: 2048)
string BrowserRedirectURL

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

enum SignupIntentStatus {
    @enumValue("verification_pending")
    VERIFICATION_PENDING

    @enumValue("verified")
    VERIFIED

    @enumValue("expired")
    EXPIRED
}

@permission(name: "iam:signup_intent:create")
string SignupIntentCreatePermission

@permission(name: "iam:signup_intent:verify")
string SignupIntentVerifyPermission

@permission(name: "iam:member_invite:accept")
string MemberInviteAcceptPermission

@permission(name: "iam:browser_session:create")
string BrowserSessionCreatePermission

@permission(name: "iam:browser_session:read")
string BrowserSessionReadPermission

@permission(name: "iam:browser_session:update")
string BrowserSessionUpdatePermission

@permission(name: "iam:browser_session:delete")
string BrowserSessionDeletePermission

@permission(name: "iam:browser_resource_token:create")
string BrowserResourceTokenCreatePermission

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

@auditEvent(name: "iam.member_invite.accept")
string MemberInviteAcceptAuditEvent

@auditEvent(name: "iam.browser_session.create")
string BrowserSessionCreateAuditEvent

@auditEvent(name: "iam.browser_session.read")
string BrowserSessionReadAuditEvent

@auditEvent(name: "iam.browser_session.update")
string BrowserSessionUpdateAuditEvent

@auditEvent(name: "iam.browser_session.delete")
string BrowserSessionDeleteAuditEvent

@auditEvent(name: "iam.browser_resource_token.create")
string BrowserResourceTokenCreateAuditEvent

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

structure SignupIntentSummary {
    @required
    @protoField(number: 1)
    signupIntentId: SignupIntentId

    @required
    @protoField(number: 2)
    resourceName: SignupIntentResourceName

    @required
    @protoField(number: 3)
    organizationDisplayName: DisplayName

    @protoField(number: 4)
    organizationSlug: OrgSlug

    @required
    @protoField(number: 5)
    status: SignupIntentStatus

    @required
    @protoField(number: 6)
    verificationExpiresAt: DateTime
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
        ConflictError
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
    @nestedProperties
    @notProperty
    @protoField(number: 1)
    signupIntent: SignupIntentSummary
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
        ConflictError
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

    @protoField(number: 4)
    organizationDisplayName: DisplayName

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
}

structure BrowserSessionInfo {
    @required
    @protoField(number: 1)
    @suppress(["MemberShouldReferenceResource"])
    sessionHandle: BrowserSessionHandle

    @required
    @protoField(number: 2)
    createdAt: DateTime

    @required
    @protoField(number: 3)
    lastSeenAt: DateTime

    @required
    @protoField(number: 4)
    expiresAt: DateTime

    @required
    @protoField(number: 5)
    authMethods: BrowserAuthMethods

    @protoField(number: 6)
    clientIP: ClientIPAddress

    @required
    @protoField(number: 7)
    clientIPTrusted: Boolean

    @protoField(number: 8)
    clientIPSource: ClientIPSource

    @protoField(number: 9)
    edgePeerIP: ClientIPAddress

    @protoField(number: 10)
    userAgent: UserAgent

    @required
    @protoField(number: 11)
    device: BrowserDevice

    @required
    @protoField(number: 12)
    location: BrowserLocation
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

structure BrowserSessionSummary {
    @required
    @protoField(number: 1)
    @suppress(["MemberShouldReferenceResource"])
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

@readonly
@http(method: "GET", uri: "/api/v1/auth/login", code: 303)
@suppress(["HttpResponseCodeSemantics"])
@identity(mode: "public", audience: "verself-api", principals: ["browser", "anonymous"])
@authz(permission: BrowserSessionCreatePermission, organization: {source: "installation"})
@audit(event: BrowserSessionCreateAuditEvent, resource: BrowserSession, action: "create")
@rateLimit(bucket: "signup_mutation")
@requestBudget(maxBytes: 0)
@sdk(module: "auth", method: "login", paginated: false, retryable: false)
operation BrowserLogin {
    input: BrowserLoginInput
    output: BrowserLoginOutput
    errors: [
        RateLimitedError
        ServiceUnavailableError
    ]
}

@input
structure BrowserLoginInput {
    @httpQuery("redirect_to")
    @protoField(number: 1)
    redirectTo: BrowserRedirectPath
}

@output
structure BrowserLoginOutput {
    @httpHeader("Location")
    @protoField(number: 1)
    location: BrowserRedirectURL
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
@suppress(["ServiceBoundResourceOperation"])
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
