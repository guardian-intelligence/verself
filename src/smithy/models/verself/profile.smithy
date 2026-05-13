$version: "2"
namespace verself.profile.v1
use smithy.api#http
use smithy.api#httpHeader
use smithy.api#httpLabel
use smithy.api#idempotencyToken
use smithy.api#idempotent
use smithy.api#length
use smithy.api#pattern
use smithy.api#readonly
use smithy.api#required
use verself.common.v1#ConflictError
use verself.common.v1#DisplayName
use verself.common.v1#IdempotencyKey
use verself.common.v1#IdempotencyPayloadMismatchError
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
@serviceRuntime(serviceName: "profile-service", publicAudience: "profile-service", internalAudience: "profile-service")
service Profile {
    version: "2026-05-13"
    operations: [
        GetProfile,
        PatchProfileIdentity,
        PutProfilePreferences
    ]
    resources: [
        ProfileSubject,
        ProfileIdentity,
        ProfilePreferences
    ]
}
@serviceRuntime(serviceName: "profile-service", publicAudience: "profile-service", internalAudience: "profile-service")
service ProfileInternal {
    version: "2026-05-13"
    operations: [
        ProfileOrgExport,
        ProfileSubjectExport,
        ProfileSubjectErasure,
        ProfileDataRightsStatus
    ]
    resources: [ProfileDataRights]
}
@length(min: 1, max: 128)
string OrgId
@length(min: 1, max: 512)
string SubjectId
@length(min: 1, max: 128)
string DataRightsRequestId
@length(min: 1, max: 320)
string EmailAddress
@length(max: 100)
string GivenName
@length(max: 100)
string FamilyName
@length(min: 1, max: 80)
string Locale
@length(min: 1, max: 80)
string Timezone
@length(min: 1, max: 64)
string ProfilePreferenceValue
@pattern("^-?[0-9]+$")
string DecimalInt64
@pattern("^[0-9]+$")
string DecimalUint64
@pattern("^[0-9a-f]{64}$")
string SHA256Hex
@length(min: 1, max: 2048)
string ArtifactURI
list ProfileArtifacts {
    member: ProfileArtifact
}
@permission(name: "profile:self:read")
string ProfileReadPermission
@permission(name: "profile:self:identity:write")
string ProfileIdentityWritePermission
@permission(name: "profile:self:preferences:write")
string ProfilePreferencesWritePermission
@permission(name: "profile:data_rights:write")
string ProfileDataRightsPermission
@auditEvent(name: "profile.subject.read")
string ProfileSubjectReadAuditEvent
@auditEvent(name: "profile.subject.identity.write")
string ProfileIdentityWriteAuditEvent
@auditEvent(name: "profile.preferences.write")
string ProfilePreferencesWriteAuditEvent
@auditEvent(name: "profile.data_rights.org_export")
string ProfileOrgExportAuditEvent
@auditEvent(name: "profile.data_rights.subject_export")
string ProfileSubjectExportAuditEvent
@auditEvent(name: "profile.data_rights.subject_erasure")
string ProfileSubjectErasureAuditEvent
@auditEvent(name: "profile.data_rights.status")
string ProfileDataRightsStatusAuditEvent
resource ProfileSubject {}
resource ProfileIdentity {}
resource ProfilePreferences {}
resource ProfileDataRights {}
structure ProfileIdentityView {
    @required
    @protoField(number: 1)
    version: DecimalInt64
    @required
    @protoField(number: 2)
    email: EmailAddress
    @protoField(number: 3)
    given_name: GivenName
    @protoField(number: 4)
    family_name: FamilyName
    @protoField(number: 5)
    display_name: DisplayName
    @required
    @protoField(number: 6)
    synced_at: Timestamp
}
structure ProfilePreferencesView {
    @required
    @protoField(number: 1)
    version: DecimalInt64
    @protoField(number: 2)
    locale: Locale
    @protoField(number: 3)
    timezone: Timezone
    @protoField(number: 4)
    time_display: ProfilePreferenceValue
    @protoField(number: 5)
    theme: ProfilePreferenceValue
    @protoField(number: 6)
    default_surface: ProfilePreferenceValue
    @required
    @protoField(number: 7)
    updated_at: Timestamp
    @protoField(number: 8)
    updated_by: SubjectId
}
structure ProfileSnapshot {
    @required
    @protoField(number: 1)
    subject_id: SubjectId
    @required
    @protoField(number: 2)
    org_id: OrgId
    @required
    @protoField(number: 3)
    identity: ProfileIdentityView
    @required
    @protoField(number: 4)
    preferences: ProfilePreferencesView
}
structure ProfileArtifact {
    @required
    @protoField(number: 1)
    uri: ArtifactURI
    @required
    @protoField(number: 2)
    bytes: DecimalUint64
    @required
    @protoField(number: 3)
    sha256: SHA256Hex
}
structure ProfileDataRightsManifest {
    @required
    @protoField(number: 1)
    request_id: DataRightsRequestId
    @required
    @protoField(number: 2)
    org_id: OrgId
    @protoField(number: 3)
    subject_id: SubjectId
    @required
    @protoField(number: 4)
    status: String
    @required
    @protoField(number: 5)
    artifacts: ProfileArtifacts
}
structure EmptyInput {}
structure ProfileOutput {
    @required
    @protoField(number: 1)
    profile: ProfileSnapshot
}
@readonly
@http(method: "GET", uri: "/api/v1/profile")
@identity(mode: "bearer", audience: "profile-service", principals: ["browser", "cli"])
@authz(permission: ProfileReadPermission, organization: {source: "request_subject"})
@audit(event: ProfileSubjectReadAuditEvent, resource: ProfileSubject, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "profile", method: "get", paginated: false, retryable: true)
operation GetProfile {
    input: EmptyInput
    output: ProfileOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}
@idempotent
@http(method: "PATCH", uri: "/api/v1/profile/identity")
@identity(mode: "bearer", audience: "profile-service", principals: ["browser", "cli"])
@authz(permission: ProfileIdentityWritePermission, organization: {source: "request_subject"})
@audit(event: ProfileIdentityWriteAuditEvent, resource: ProfileIdentity, action: "write")
@rateLimit(bucket: "profile_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "profile.identity", method: "update", paginated: false, retryable: false)
operation PatchProfileIdentity {
    input: PatchProfileIdentityInput
    output: ProfileOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
structure PatchProfileIdentityInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
    @required
    version: DecimalInt64
    given_name: GivenName
    family_name: FamilyName
    display_name: DisplayName
}
@idempotent
@http(method: "PUT", uri: "/api/v1/profile/preferences")
@identity(mode: "bearer", audience: "profile-service", principals: ["browser", "cli"])
@authz(permission: ProfilePreferencesWritePermission, organization: {source: "request_subject"})
@audit(event: ProfilePreferencesWriteAuditEvent, resource: ProfilePreferences, action: "write")
@rateLimit(bucket: "profile_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "profile.preferences", method: "put", paginated: false, retryable: false)
operation PutProfilePreferences {
    input: PutProfilePreferencesInput
    output: ProfileOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
structure PutProfilePreferencesInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
    @required
    version: DecimalInt64
    locale: Locale
    timezone: Timezone
    time_display: ProfilePreferenceValue
    theme: ProfilePreferenceValue
    default_surface: ProfilePreferenceValue
}
structure ProfileDataRightsInput {
    @required
    org_id: OrgId
    subject_id: SubjectId
    @required
    request_id: DataRightsRequestId
}
structure ProfileDataRightsOutput {
    @required
    manifest: ProfileDataRightsManifest
}
@http(method: "POST", uri: "/internal/v1/data-rights/org-export")
@identity(mode: "spiffe_mtls", audience: "profile-service", principals: ["workload"])
@authz(permission: ProfileDataRightsPermission, organization: {source: "request_org_id"})
@audit(event: ProfileOrgExportAuditEvent, resource: ProfileDataRights, action: "export")
@rateLimit(bucket: "internal_data_rights")
@requestBudget(maxBytes: 65536)
@sdk(module: "profileInternal.dataRights", method: "orgExport", paginated: false, retryable: false)
operation ProfileOrgExport {
    input: ProfileDataRightsInput
    output: ProfileDataRightsOutput
    errors: [ValidationFailedError, PermissionDeniedError, ServiceUnavailableError]
}
@http(method: "POST", uri: "/internal/v1/data-rights/subject-export")
@identity(mode: "spiffe_mtls", audience: "profile-service", principals: ["workload"])
@authz(permission: ProfileDataRightsPermission, organization: {source: "request_subject_id"})
@audit(event: ProfileSubjectExportAuditEvent, resource: ProfileDataRights, action: "export")
@rateLimit(bucket: "internal_data_rights")
@requestBudget(maxBytes: 65536)
@sdk(module: "profileInternal.dataRights", method: "subjectExport", paginated: false, retryable: false)
operation ProfileSubjectExport {
    input: ProfileDataRightsInput
    output: ProfileDataRightsOutput
    errors: [ValidationFailedError, PermissionDeniedError, ServiceUnavailableError]
}
@http(method: "POST", uri: "/internal/v1/data-rights/subject-erasure")
@identity(mode: "spiffe_mtls", audience: "profile-service", principals: ["workload"])
@authz(permission: ProfileDataRightsPermission, organization: {source: "request_subject_id"})
@audit(event: ProfileSubjectErasureAuditEvent, resource: ProfileDataRights, action: "erase")
@rateLimit(bucket: "internal_data_rights")
@requestBudget(maxBytes: 65536)
@sdk(module: "profileInternal.dataRights", method: "subjectErasure", paginated: false, retryable: false)
operation ProfileSubjectErasure {
    input: ProfileDataRightsInput
    output: ProfileDataRightsOutput
    errors: [ValidationFailedError, PermissionDeniedError, ServiceUnavailableError]
}
@readonly
@http(method: "GET", uri: "/internal/v1/data-rights/requests/{request_id}")
@identity(mode: "spiffe_mtls", audience: "profile-service", principals: ["workload"])
@authz(permission: ProfileDataRightsPermission, organization: {source: "request_id"})
@audit(event: ProfileDataRightsStatusAuditEvent, resource: ProfileDataRights, action: "read")
@rateLimit(bucket: "internal_data_rights")
@requestBudget(maxBytes: 0)
@sdk(module: "profileInternal.dataRights", method: "status", paginated: false, retryable: true)
operation ProfileDataRightsStatus {
    input: ProfileDataRightsStatusInput
    output: ProfileDataRightsOutput
    errors: [PermissionDeniedError, ResourceNotFoundError, ServiceUnavailableError]
}
structure ProfileDataRightsStatusInput {
    @required
    @httpLabel
    request_id: DataRightsRequestId
}
