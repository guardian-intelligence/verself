$version: "2"
namespace verself.governance.v1

use aws.protocols#restJson1
use smithy.api#http
use smithy.api#httpHeader
use smithy.api#httpLabel
use smithy.api#httpPayload
use smithy.api#httpQuery
use smithy.api#idempotencyToken
use smithy.api#idempotent
use smithy.api#length
use smithy.api#mediaType
use smithy.api#paginated
use smithy.api#pattern
use smithy.api#range
use smithy.api#readonly
use smithy.api#required
use smithy.api#sensitive
use verself.common.v1#ConflictError
use verself.common.v1#DateTime
use verself.common.v1#IdempotencyKey
use verself.common.v1#IdempotencyPayloadMismatchError
use verself.common.v1#PermissionDeniedError
use verself.common.v1#RateLimitedError
use verself.common.v1#ResourceName
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

@serviceRuntime(serviceName: "governance-service", publicAudience: "verself-api", internalAudience: "governance-service")
@restJson1
service Governance {
    version: "2026-05-13"
    operations: [
        ListAPIActivities,
        ListDataExports,
        CreateDataExport,
        GetDataExport,
        DownloadDataExport
    ]
    resources: [
        APIActivity,
        DataExport
    ]
}

@serviceRuntime(serviceName: "governance-service", publicAudience: "verself-api", internalAudience: "governance-service")
@restJson1
service GovernanceInternal {
    version: "2026-05-13"
    operations: [AppendAPIActivity]
    resources: [APIActivity]
}

@length(min: 1, max: 128)
@pattern("^org_[0-9A-HJKMNP-TV-Z]{26}$")
string GovernanceOrgId

@pattern("^[0-9a-fA-F-]{36}$")
string OCSFMetadataUid

@pattern("^[0-9a-fA-F-]{36}$")
string DataExportId

@pattern("^[0-9]+$")
string DecimalUint64

@pattern("^[0-9a-f]{64}$")
string SHA256Hex

@pattern("^[0-9a-f]{64}$")
string HMACHex

@length(min: 1, max: 128)
@pattern("^[a-z][a-z0-9_.-]*$")
string ActorType

@length(min: 1, max: 512)
string ActorUid

@length(max: 512)
string ActorName

@length(max: 512)
@sensitive
string ActorEmail

@length(max: 512)
string CredentialUid

@length(min: 1, max: 128)
@pattern("^[a-z][a-z0-9_-]*$")
string ResourceType

@length(max: 64)
string ResourceRole

@range(min: 1, max: 4)
integer ResourceRoleId

@length(max: 512)
string ResourceUid

@length(max: 512)
string ResourceDisplayName

@length(min: 1, max: 128)
@pattern("^[a-z][a-z0-9-]*-service$")
string APIActivityService

@length(min: 1, max: 128)
@pattern("^[a-z][a-z0-9_.-]*$")
string APIActivityOperation

@length(min: 1, max: 255)
@pattern("^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)+$")
string APIEventCode

@length(min: 1, max: 32)
@pattern("^[a-z][a-z0-9_]*$")
string APIActivityAction

@length(min: 1, max: 64)
string APIVersion

@length(min: 1, max: 255)
string GovernancePermissionName

@length(min: 1, max: 128)
string HMACKeyId

@length(min: 1, max: 128)
string ProblemStatusCode

@length(max: 4096)
string ProblemStatusDetail

@pattern("^[0-9a-f]{32}$")
string TraceId

@pattern("^[0-9a-f]{16}$")
string SpanId

@length(min: 1, max: 4096)
@sensitive
string APIActivityCursor

@length(max: 8192)
@sensitive
string ExportDownloadURL

@length(min: 1, max: 4096)
string ExportArtifactPath

@length(min: 1, max: 128)
string ExportScope

@length(min: 1, max: 128)
string ExportFormat

@length(min: 1, max: 128)
string ExportState

@length(max: 128)
string ExportErrorCode

@length(max: 4096)
string ExportErrorMessage

@length(min: 1, max: 255)
string MediaType

@length(min: 1, max: 255)
string ContentDisposition

@length(min: 1, max: 32)
string HTTPMethod

@length(min: 1, max: 4096)
string HTTPRoute

@length(max: 8192)
@sensitive
string HTTPSafeArgs

@length(max: 512)
@sensitive
string HTTPUserAgent

@length(max: 128)
string HTTPRequestUid

@length(max: 1024)
@sensitive
string HTTPXForwardedFor

@length(max: 1024)
@sensitive
string HTTPReferrer

@length(max: 255)
string HTTPHost

@length(max: 16)
string HTTPScheme

@length(max: 128)
string EndpointIP

@length(max: 255)
string EndpointName

@length(max: 255)
string HTTPResponseMessage

@length(max: 255)
string HTTPResponseStatus

@mediaType("application/gzip")
blob DataExportArchive

@range(min: 1, max: 200)
integer APIActivityEventsLimit

@range(min: 1, max: 65535)
integer OCSFClassUid

@range(min: 1, max: 4294967295)
long OCSFTypeUid

@range(min: 1, max: 99)
integer APIActivityId

@range(min: 0, max: 255)
integer OCSFSmallId

@range(min: 100, max: 599)
integer HTTPStatusCode

enum APIActivityListOrder {
    ASC = "asc"
    DESC = "desc"
}

enum APIActivityStatus {
    SUCCESS = "Success"
    FAILURE = "Failure"
    OTHER = "Other"
}

enum AuthorizationDecision {
    ALLOWED = "Allowed"
    DENIED = "Denied"
}

list APIActivityEvents {
    member: APIActivityEvent
}

list APIActivityResources {
    member: APIActivityResource
}

list ExportScopes {
    member: ExportScope
}

list DataExportFiles {
    member: DataExportFile
}

list DataExportJobs {
    member: DataExportJob
}

@permission(name: "governance:api_activity:read")
string APIActivityReadPermission

@permission(name: "governance:api_activity:export")
string APIActivityExportPermission

@permission(name: "governance:api_activity:append")
string APIActivityAppendPermission

@auditEvent(name: "governance.ocsf_api_activity.list")
string APIActivityListOperationEvent

@auditEvent(name: "governance.ocsf_api_activity.export.list")
string DataExportListOperationEvent

@auditEvent(name: "governance.ocsf_api_activity.export.create")
string DataExportCreateOperationEvent

@auditEvent(name: "governance.ocsf_api_activity.export.read")
string DataExportReadOperationEvent

@auditEvent(name: "governance.ocsf_api_activity.export.download")
string DataExportDownloadOperationEvent

@auditEvent(name: "governance.ocsf_api_activity.append")
string APIActivityAppendOperationEvent

resource APIActivity {}
resource DataExport {}

structure APIActivityEvent {
    @required
    @protoField(number: 1)
    metadata_uid: OCSFMetadataUid

    @required
    @protoField(number: 2)
    time: DateTime

    @required
    @protoField(number: 3)
    org_id: GovernanceOrgId

    @required
    @protoField(number: 4)
    sequence: DecimalUint64

    @required
    @protoField(number: 5)
    ocsf_version: String

    @required
    @protoField(number: 6)
    category_uid: OCSFSmallId

    @required
    @protoField(number: 7)
    category_name: String

    @required
    @protoField(number: 8)
    class_uid: OCSFClassUid

    @required
    @protoField(number: 9)
    class_name: String

    @required
    @protoField(number: 10)
    type_uid: OCSFTypeUid

    @required
    @protoField(number: 11)
    activity_id: APIActivityId

    @required
    @protoField(number: 12)
    activity_name: String

    @required
    @protoField(number: 13)
    action_id: OCSFSmallId

    @required
    @protoField(number: 14)
    action: String

    @required
    @protoField(number: 15)
    status_id: OCSFSmallId

    @required
    @protoField(number: 16)
    status: APIActivityStatus

    @required
    @protoField(number: 17)
    status_code: ProblemStatusCode

    @required
    @protoField(number: 18)
    severity_id: OCSFSmallId

    @required
    @protoField(number: 19)
    severity: String

    @required
    @protoField(number: 20)
    api_service: APIActivityService

    @required
    @protoField(number: 21)
    api_operation: APIActivityOperation

    @protoField(number: 22)
    api_version: APIVersion

    @required
    @protoField(number: 23)
    actor_type: ActorType

    @required
    @protoField(number: 24)
    actor_uid: ActorUid

    @protoField(number: 25)
    actor_name: ActorName

    @protoField(number: 26)
    credential_uid: CredentialUid

    @required
    @protoField(number: 27)
    primary_resource_type: ResourceType

    @protoField(number: 28)
    primary_resource_uid: ResourceUid

    @protoField(number: 29)
    primary_resource_name: ResourceDisplayName

    @protoField(number: 30)
    primary_resource_full_name: ResourceName

    @required
    @protoField(number: 31)
    permission: GovernancePermissionName

    @protoField(number: 32)
    http_request_uid: HTTPRequestUid

    @required
    @protoField(number: 33)
    http_method: HTTPMethod

    @required
    @protoField(number: 34)
    http_route: HTTPRoute

    @protoField(number: 35)
    http_args: HTTPSafeArgs

    @protoField(number: 36)
    http_user_agent: HTTPUserAgent

    @protoField(number: 37)
    src_endpoint_ip: EndpointIP

    @protoField(number: 38)
    src_endpoint_name: EndpointName

    @required
    @protoField(number: 39)
    http_response_code: HTTPStatusCode

    @protoField(number: 40)
    trace_uid: TraceId

    @protoField(number: 41)
    span_uid: SpanId

    @required
    @protoField(number: 42)
    ocsf_sha256: SHA256Hex

    @required
    @protoField(number: 43)
    prev_hmac: HMACHex

    @required
    @protoField(number: 44)
    row_hmac: HMACHex

    @protoField(number: 45)
    hmac_key_id: HMACKeyId
}

structure APIActivityFilters {
    @protoField(number: 1)
    actor_uid: ActorUid

    @protoField(number: 2)
    actor_type: ActorType

    @protoField(number: 3)
    api_service: APIActivityService

    @protoField(number: 4)
    api_operation: APIActivityOperation

    @protoField(number: 5)
    activity_id: APIActivityId

    @protoField(number: 6)
    credential_uid: CredentialUid

    @protoField(number: 7)
    resource_uid: ResourceUid

    @protoField(number: 8)
    resource_type: ResourceType

    @protoField(number: 9)
    status_id: OCSFSmallId

    @protoField(number: 10)
    status_code: ProblemStatusCode

    @protoField(number: 11)
    trace_uid: TraceId
}

structure APIActivityResource {
    @required
    @protoField(number: 1)
    type: ResourceType

    @protoField(number: 2)
    uid: ResourceUid

    @protoField(number: 3)
    name: ResourceDisplayName

    @protoField(number: 4)
    full_name: ResourceName

    @protoField(number: 5)
    role: ResourceRole

    @protoField(number: 6)
    role_id: ResourceRoleId
}

structure APIActivityHTTPRequest {
    @required
    @protoField(number: 1)
    method: HTTPMethod

    @required
    @protoField(number: 2)
    route: HTTPRoute

    @protoField(number: 3)
    request_uid: HTTPRequestUid

    @protoField(number: 4)
    args: HTTPSafeArgs

    @protoField(number: 5)
    user_agent: HTTPUserAgent

    @protoField(number: 6)
    src_endpoint_ip: EndpointIP

    @protoField(number: 7)
    src_endpoint_name: EndpointName

    @protoField(number: 8)
    x_forwarded_for: HTTPXForwardedFor

    @protoField(number: 9)
    referrer: HTTPReferrer

    @protoField(number: 10)
    host: HTTPHost

    @protoField(number: 11)
    scheme: HTTPScheme
}

structure APIActivityHTTPResponse {
    @required
    @protoField(number: 1)
    code: HTTPStatusCode

    @protoField(number: 2)
    message: HTTPResponseMessage

    @protoField(number: 3)
    status: HTTPResponseStatus
}

structure APIActivityRecord {
    @required
    @protoField(number: 1)
    org_id: GovernanceOrgId

    @protoField(number: 2)
    observed_at: DateTime

    @required
    @protoField(number: 3)
    api_service: APIActivityService

    @required
    @protoField(number: 4)
    api_operation: APIActivityOperation

    @required
    @protoField(number: 5)
    api_event_code: APIEventCode

    @required
    @protoField(number: 6)
    api_action: APIActivityAction

    @protoField(number: 7)
    api_version: APIVersion

    @required
    @protoField(number: 8)
    actor_type: ActorType

    @required
    @protoField(number: 9)
    actor_uid: ActorUid

    @protoField(number: 10)
    actor_name: ActorName

    @protoField(number: 11)
    credential_uid: CredentialUid

    @required
    @protoField(number: 12)
    permission: GovernancePermissionName

    @required
    @protoField(number: 13)
    resources: APIActivityResources

    @required
    @protoField(number: 14)
    http_request: APIActivityHTTPRequest

    @required
    @protoField(number: 15)
    http_response: APIActivityHTTPResponse

    @required
    @protoField(number: 16)
    authorization_decision: AuthorizationDecision

    @required
    @protoField(number: 17)
    status: APIActivityStatus

    @required
    @protoField(number: 18)
    status_code: ProblemStatusCode

    @protoField(number: 19)
    status_detail: ProblemStatusDetail

    @protoField(number: 20)
    trace_uid: TraceId

    @protoField(number: 21)
    span_uid: SpanId

    @protoField(number: 22)
    unmapped: Document

    @protoField(number: 23)
    actor_email: ActorEmail
}

structure AppendAPIActivityAccepted {
    @required
    @protoField(number: 1)
    metadata_uid: OCSFMetadataUid

    @required
    @protoField(number: 2)
    sequence: DecimalUint64

    @required
    @protoField(number: 3)
    row_hmac: HMACHex
}

structure DataExportFile {
    @required
    @protoField(number: 1)
    path: ExportArtifactPath

    @required
    @protoField(number: 2)
    content_type: MediaType

    @required
    @protoField(number: 3)
    rows: DecimalUint64

    @required
    @protoField(number: 4)
    bytes: DecimalUint64

    @required
    @protoField(number: 5)
    sha256: SHA256Hex
}

structure DataExportJob {
    @required
    @protoField(number: 1)
    export_id: DataExportId

    @required
    @protoField(number: 2)
    resourceName: ResourceName

    @required
    @protoField(number: 3)
    org_id: GovernanceOrgId

    @required
    @protoField(number: 4)
    requested_by: ActorUid

    @required
    @protoField(number: 5)
    scopes: ExportScopes

    @required
    @protoField(number: 6)
    include_logs: Boolean

    @required
    @protoField(number: 7)
    format: ExportFormat

    @required
    @protoField(number: 8)
    state: ExportState

    @protoField(number: 9)
    artifact_sha256: SHA256Hex

    @required
    @protoField(number: 10)
    artifact_bytes: DecimalUint64

    @protoField(number: 11)
    download_url: ExportDownloadURL

    @required
    @protoField(number: 12)
    files: DataExportFiles

    @required
    @protoField(number: 13)
    created_at: DateTime

    @required
    @protoField(number: 14)
    updated_at: DateTime

    @protoField(number: 15)
    completed_at: DateTime

    @required
    @protoField(number: 16)
    expires_at: DateTime

    @protoField(number: 17)
    error_code: ExportErrorCode

    @protoField(number: 18)
    error_message: ExportErrorMessage
}

@readonly
@http(method: "GET", uri: "/api/v1/governance/ocsf/api-activities")
@paginated(inputToken: "cursor", outputToken: "next_cursor", pageSize: "limit", items: "api_activities")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: APIActivityReadPermission, organization: {source: "token_org_id"})
@audit(event: APIActivityListOperationEvent, resource: APIActivity, action: "list", ocsf_class_uid: 6003, ocsf_class_name: "API Activity")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "governance.apiActivity", method: "list", paginated: true, retryable: true)
operation ListAPIActivities {
    input: ListAPIActivitiesInput
    output: ListAPIActivitiesOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        PermissionDeniedError
        RateLimitedError
        ServiceUnavailableError
    ]
}

structure ListAPIActivitiesInput {
    @httpQuery("limit")
    @protoField(number: 1)
    limit: APIActivityEventsLimit

    @httpQuery("cursor")
    @protoField(number: 2)
    cursor: APIActivityCursor

    @httpQuery("order")
    @protoField(number: 3)
    order: APIActivityListOrder

    @httpQuery("actor_uid")
    @protoField(number: 4)
    actor_uid: ActorUid

    @httpQuery("actor_type")
    @protoField(number: 5)
    actor_type: ActorType

    @httpQuery("api_service")
    @protoField(number: 6)
    api_service: APIActivityService

    @httpQuery("api_operation")
    @protoField(number: 7)
    api_operation: APIActivityOperation

    @httpQuery("activity_id")
    @protoField(number: 8)
    activity_id: APIActivityId

    @httpQuery("credential_uid")
    @protoField(number: 9)
    credential_uid: CredentialUid

    @httpQuery("resource_uid")
    @protoField(number: 10)
    resource_uid: ResourceUid

    @httpQuery("resource_type")
    @protoField(number: 11)
    resource_type: ResourceType

    @httpQuery("status_id")
    @protoField(number: 12)
    status_id: OCSFSmallId

    @httpQuery("status_code")
    @protoField(number: 13)
    status_code: ProblemStatusCode

    @httpQuery("trace_uid")
    @protoField(number: 14)
    trace_uid: TraceId
}

structure ListAPIActivitiesOutput {
    @required
    @protoField(number: 1)
    api_activities: APIActivityEvents

    @protoField(number: 2)
    next_cursor: APIActivityCursor

    @required
    @protoField(number: 3)
    limit: APIActivityEventsLimit

    @required
    @protoField(number: 4)
    filters: APIActivityFilters
}

@readonly
@http(method: "GET", uri: "/api/v1/governance/exports")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: APIActivityExportPermission, organization: {source: "token_org_id"})
@audit(event: DataExportListOperationEvent, resource: DataExport, action: "list", ocsf_class_uid: 6003, ocsf_class_name: "API Activity")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "governance.exports", method: "list", paginated: false, retryable: true)
operation ListDataExports {
    input: ListDataExportsInput
    output: ListDataExportsOutput
    errors: [
        UnauthenticatedError
        PermissionDeniedError
        RateLimitedError
        ServiceUnavailableError
    ]
}

structure ListDataExportsInput {}

structure ListDataExportsOutput {
    @required
    @protoField(number: 1)
    exports: DataExportJobs
}

@idempotent
@http(method: "POST", uri: "/api/v1/governance/exports", code: 201)
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: APIActivityExportPermission, organization: {source: "token_org_id"})
@audit(event: DataExportCreateOperationEvent, resource: DataExport, action: "create", ocsf_class_uid: 6003, ocsf_class_name: "API Activity")
@rateLimit(bucket: "export_create")
@requestBudget(maxBytes: 16384)
@sdk(module: "governance.exports", method: "create", paginated: false, retryable: false)
operation CreateDataExport {
    input: CreateDataExportInput
    output: CreateDataExportOutput
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

structure CreateDataExportInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    @protoField(number: 100)
    idempotencyKey: IdempotencyKey

    @protoField(number: 1)
    scopes: ExportScopes

    @protoField(number: 2)
    include_logs: Boolean
}

structure CreateDataExportOutput {
    @required
    @protoField(number: 1)
    export: DataExportJob
}

@readonly
@http(method: "GET", uri: "/api/v1/governance/exports/{export_id}")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: APIActivityExportPermission, organization: {source: "token_org_id"})
@audit(event: DataExportReadOperationEvent, resource: DataExport, action: "read", ocsf_class_uid: 6003, ocsf_class_name: "API Activity")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "governance.exports", method: "get", paginated: false, retryable: true)
operation GetDataExport {
    input: GetDataExportInput
    output: GetDataExportOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        PermissionDeniedError
        ResourceNotFoundError
        RateLimitedError
        ServiceUnavailableError
    ]
}

structure GetDataExportInput {
    @required
    @httpLabel
    @protoField(number: 1)
    export_id: DataExportId
}

structure GetDataExportOutput {
    @required
    @protoField(number: 1)
    export: DataExportJob
}

@readonly
@http(method: "GET", uri: "/api/v1/governance/exports/{export_id}/download")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: APIActivityExportPermission, organization: {source: "token_org_id"})
@audit(event: DataExportDownloadOperationEvent, resource: DataExport, action: "download", ocsf_class_uid: 6003, ocsf_class_name: "API Activity")
@rateLimit(bucket: "export_download")
@requestBudget(maxBytes: 0)
@sdk(module: "governance.exports", method: "download", paginated: false, retryable: true)
operation DownloadDataExport {
    input: DownloadDataExportInput
    output: DownloadDataExportOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        PermissionDeniedError
        ResourceNotFoundError
        RateLimitedError
        ServiceUnavailableError
    ]
}

structure DownloadDataExportInput {
    @required
    @httpLabel
    @protoField(number: 1)
    export_id: DataExportId
}

structure DownloadDataExportOutput {
    @required
    @httpHeader("Content-Type")
    @protoField(number: 1)
    contentType: MediaType

    @required
    @httpHeader("Content-Disposition")
    @protoField(number: 2)
    contentDisposition: ContentDisposition

    @required
    @httpPayload
    @protoField(number: 3)
    body: DataExportArchive
}

@http(method: "POST", uri: "/internal/v1/ocsf/api-activities", code: 202)
@identity(mode: "spiffe_mtls", audience: "governance-service", principals: ["workload"])
@authz(permission: APIActivityAppendPermission, organization: {source: "body_org_id", member: "org_id"})
@audit(event: APIActivityAppendOperationEvent, resource: APIActivity, action: "write", ocsf_class_uid: 6003, ocsf_class_name: "API Activity")
@rateLimit(bucket: "internal_mutation")
@requestBudget(maxBytes: 32768)
@sdk(module: "governanceInternal.apiActivity", method: "append", paginated: false, retryable: false)
operation AppendAPIActivity {
    input: AppendAPIActivityInput
    output: AppendAPIActivityOutput
    errors: [
        ValidationFailedError
        UnauthenticatedError
        PermissionDeniedError
        ServiceUnavailableError
    ]
}

structure AppendAPIActivityInput {
    @required
    @httpPayload
    @protoField(number: 1)
    record: APIActivityRecord
}

structure AppendAPIActivityOutput {
    @required
    @protoField(number: 1)
    accepted: AppendAPIActivityAccepted
}
