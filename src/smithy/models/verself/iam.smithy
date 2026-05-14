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
use verself.common.v1#permission
use verself.common.v1#protoField
use verself.common.v1#rateLimit
use verself.common.v1#requestBudget
use verself.common.v1#sdk
use verself.common.v1#serviceRuntime
use verself.common.v1#DateTime

@httpBearerAuth
@auth([smithy.api#httpBearerAuth])
@serviceRuntime(serviceName: "iam-service", publicAudience: "iam-service")
@restJson1
service Iam {
    version: "2026-05-12"
    resources: [Organization]
}

@serviceRuntime(serviceName: "iam-service", publicAudience: "iam-service", internalAudience: "iam-service")
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

@pattern("^urn:verself:inst_[0-9A-HJKMNP-TV-Z]{26}:orgs/org_[0-9A-HJKMNP-TV-Z]{26}$")
string OrganizationResourceName

@pattern("^urn:verself:inst_[0-9A-HJKMNP-TV-Z]{26}:orgs/org_[0-9A-HJKMNP-TV-Z]{26}/members/member_[0-9A-HJKMNP-TV-Z]{26}$")
string MemberResourceName

@length(min: 1, max: 80)
@pattern("^[a-z0-9]([a-z0-9-]*[a-z0-9])?$")
string OrgSlug

@length(min: 3, max: 320)
string EmailAddress

enum OrganizationRole {
    OWNER = "owner"
    ADMIN = "admin"
    MEMBER = "member"
}

@range(min: 1, max: 2147483647)
integer OrganizationVersion

@range(min: 1, max: 2147483647)
integer OrgAclVersion

@length(min: 1, max: 64)
@pattern("^[0-9]+$")
string ProviderOrgId

@length(min: 1, max: 512)
string SubjectId

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

@permission(name: "iam:organization:list")
string OrganizationListPermission

@permission(name: "iam:organization:read")
string OrganizationReadPermission

@permission(name: "iam:organization:update")
string OrganizationUpdatePermission

@permission(name: "iam:member:list")
string MemberListPermission

@permission(name: "iam:member:read")
string MemberReadPermission

@permission(name: "iam:member:update_role")
string MemberUpdateRolePermission

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

@auditEvent(name: "iam.organization.list")
string OrganizationListAuditEvent

@auditEvent(name: "iam.organization.read")
string OrganizationReadAuditEvent

@auditEvent(name: "iam.organization.update")
string OrganizationUpdateAuditEvent

@auditEvent(name: "iam.member.list")
string MemberListAuditEvent

@auditEvent(name: "iam.member.read")
string MemberReadAuditEvent

@auditEvent(name: "iam.member.update_role")
string MemberUpdateRoleAuditEvent

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

resource Organization {
    identifiers: {
        orgId: OrgId
    }
    properties: {
        resourceName: OrganizationResourceName
        slug: OrgSlug
        displayName: DisplayName
        callerRole: OrganizationRole
        version: OrganizationVersion
        orgAclVersion: OrgAclVersion
    }
    list: ListOrganizations
    read: GetOrganization
    update: UpdateOrganization
    resources: [Member]
}

resource Member {
    identifiers: {
        orgId: OrgId
        memberId: MemberId
    }
    properties: {
        resourceName: MemberResourceName
        email: EmailAddress
        displayName: DisplayName
        role: OrganizationRole
    }
    list: ListMembers
    read: GetMember
    operations: [UpdateMemberRole]
}

resource HumanProfile {
}

resource Authorization {
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
    $callerRole

    @required
    @protoField(number: 6)
    $version

    @required
    @protoField(number: 7)
    $orgAclVersion
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

    @required
    @protoField(number: 6)
    $role
}

@readonly
@http(method: "GET", uri: "/api/v1/orgs")
@paginated(inputToken: "pageToken", outputToken: "nextPageToken", pageSize: "pageSize", items: "organizations")
@identity(mode: "bearer", audience: "iam-service", principals: ["browser", "cli", "workload"])
@authz(permission: OrganizationListPermission, organization: {source: "token_role_assignments"})
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

@readonly
@http(method: "GET", uri: "/api/v1/orgs/{orgId}")
@identity(mode: "bearer", audience: "iam-service", principals: ["browser", "cli", "workload"])
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
@identity(mode: "bearer", audience: "iam-service", principals: ["browser", "cli"])
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
@identity(mode: "bearer", audience: "iam-service", principals: ["browser", "cli"])
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

@readonly
@http(method: "GET", uri: "/api/v1/orgs/{orgId}/members/{memberId}")
@identity(mode: "bearer", audience: "iam-service", principals: ["browser", "cli"])
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

@idempotent
@http(method: "PATCH", uri: "/api/v1/orgs/{orgId}/members/{memberId}/role")
@identity(mode: "bearer", audience: "iam-service", principals: ["browser", "cli"])
@authz(permission: MemberUpdateRolePermission, organization: {source: "input_member", member: "orgId"})
@audit(event: MemberUpdateRoleAuditEvent, resource: Member, action: "update")
@rateLimit(bucket: "iam_mutation")
@requestBudget(maxBytes: 8192)
@sdk(module: "members", method: "updateRole", paginated: false, retryable: false)
operation UpdateMemberRole {
    input: UpdateMemberRoleInput
    output: UpdateMemberRoleOutput
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
structure UpdateMemberRoleInput for Member {
    @required
    @httpLabel
    @protoField(number: 1)
    $orgId

    @required
    @httpLabel
    @protoField(number: 2)
    $memberId

    @required
    @protoField(number: 3)
    $role

    @required
    @notProperty
    @protoField(number: 4)
    expectedRole: OrganizationRole

    @required
    @notProperty
    @protoField(number: 5)
    expectedOrgAclVersion: OrgAclVersion

    @required
    @notProperty
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    @protoField(number: 100)
    idempotencyKey: IdempotencyKey
}

@output
structure UpdateMemberRoleOutput {
    @required
    @httpPayload
    @nestedProperties
    @notProperty
    @protoField(number: 1)
    member: MemberSummary
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
    org_id: ProviderOrgId

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
    org_id: ProviderOrgId

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
    org_id: ProviderOrgId

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
    org_id: ProviderOrgId

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
    org_id: ProviderOrgId

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
    org_id: ProviderOrgId

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
    org_id: ProviderOrgId

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
