$version: "2"

namespace verself.iam.v1

use smithy.api#auth
use smithy.api#http
use smithy.api#httpBearerAuth
use smithy.api#httpHeader
use smithy.api#httpLabel
use smithy.api#idempotencyToken
use smithy.api#idempotent
use smithy.api#input
use smithy.api#length
use smithy.api#nestedProperties
use smithy.api#notProperty
use smithy.api#output
use smithy.api#paginated
use smithy.api#pattern
use smithy.api#range
use smithy.api#readonly
use smithy.api#required
use smithy.api#resourceIdentifier
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
use verself.common.v1#conformance
use verself.common.v1#identity
use verself.common.v1#permission
use verself.common.v1#protoField
use verself.common.v1#rateLimit
use verself.common.v1#requestBudget
use verself.common.v1#sdk
use verself.common.v1#serviceRuntime

@httpBearerAuth
@auth([smithy.api#httpBearerAuth])
@serviceRuntime(serviceName: "iam-service", publicAudience: "iam-service")
service Iam {
    version: "2026-05-12"
    resources: [Organization]
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
@conformance(cases: ["http_serialization", "response_parsing", "pagination", "auth", "retry", "trace_context"])
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
@conformance(cases: ["http_serialization", "response_parsing", "auth", "retry", "wrong_org", "trace_context"])
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
    @nestedProperties
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
@conformance(cases: ["http_serialization", "response_parsing", "problem_parsing", "idempotency", "auth", "wrong_org", "trace_context"])
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
    @nestedProperties
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
@conformance(cases: ["http_serialization", "response_parsing", "pagination", "auth", "retry", "wrong_org", "trace_context"])
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
@conformance(cases: ["http_serialization", "response_parsing", "auth", "retry", "wrong_org", "trace_context"])
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
    @nestedProperties
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
@conformance(cases: ["http_serialization", "response_parsing", "problem_parsing", "idempotency", "auth", "wrong_org", "trace_context"])
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
    @nestedProperties
    @protoField(number: 1)
    member: MemberSummary
}
