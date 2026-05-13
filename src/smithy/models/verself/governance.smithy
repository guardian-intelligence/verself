$version: "2"
namespace verself.governance.v1
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
@serviceRuntime(serviceName: "governance-service", publicAudience: "governance-service", internalAudience: "governance-service")
service Governance {
    version: "2026-05-13"
    operations: [
        ListAuditEvents,
        ListDataExports,
        CreateDataExport,
        GetDataExport,
        DownloadDataExport
    ]
    resources: [
        AuditLog,
        DataExport
    ]
}
@serviceRuntime(serviceName: "governance-service", publicAudience: "governance-service", internalAudience: "governance-service")
service GovernanceInternal {
    version: "2026-05-13"
    operations: [AppendAuditEvent]
    resources: [AuditLog]
}
@length(min: 1, max: 128)
string GovernanceOrgId
@pattern("^[0-9a-fA-F-]{36}$")
string AuditEventId
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
string AuditActorType
@length(min: 1, max: 512)
string AuditActorId
@length(max: 512)
string AuditCredentialId
@length(min: 1, max: 128)
@pattern("^[a-z][a-z0-9_]*$")
string AuditTargetType
@length(max: 512)
string AuditTargetId
@length(min: 1, max: 128)
string AuditEventSource
@length(min: 1, max: 128)
string AuditEventOperationName
@length(min: 1, max: 255)
string GovernanceAuditEventName
@length(min: 1, max: 255)
string GovernancePermissionName
@length(min: 1, max: 128)
string AuditSchemaVersion
@length(max: 128)
string AuditHMACKeyId
@length(max: 128)
string AuditErrorCode
@pattern("^[0-9a-f]{32}$")
string TraceId
@length(min: 1, max: 4096)
@sensitive
string AuditCursor
@length(max: 4096)
@sensitive
string ExportDownloadURL
@length(min: 1, max: 512)
string ExportArtifactPath
@length(min: 1, max: 128)
string ExportScope
@length(min: 1, max: 128)
string ExportFormat
@length(min: 1, max: 128)
string ExportState
@length(max: 128)
string ExportErrorCode
@length(max: 2048)
string ExportErrorMessage
@length(min: 1, max: 255)
string MediaType
@length(min: 1, max: 255)
string ContentDisposition
@mediaType("application/gzip")
blob DataExportArchive
@range(min: 1, max: 200)
integer AuditEventsLimit
enum AuditListOrder {
    ASC = "asc"
    DESC = "desc"
}
enum AuditOutcome {
    ALLOWED = "allowed"
    DENIED = "denied"
    ERROR = "error"
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
list AuditEvents {
    member: AuditEvent
}
@permission(name: "governance:audit_log:read")
string AuditLogReadPermission
@permission(name: "governance:audit_log:read_detail")
string AuditLogReadDetailPermission
@permission(name: "governance:audit_log:export")
string AuditLogExportPermission
@permission(name: "governance:audit_log:append")
string AuditLogAppendPermission
@auditEvent(name: "governance.audit_log.list")
string AuditLogListAuditEvent
@auditEvent(name: "governance.audit_log.export.list")
string DataExportListAuditEvent
@auditEvent(name: "governance.audit_log.export.create")
string DataExportCreateAuditEvent
@auditEvent(name: "governance.audit_log.export.read")
string DataExportReadAuditEvent
@auditEvent(name: "governance.audit_log.export.download")
string DataExportDownloadAuditEvent
@auditEvent(name: "governance.audit_log.append")
string AuditLogAppendAuditEvent
resource AuditLog {}
resource DataExport {}
structure AuditEvent {
    @required
    @protoField(number: 1)
    event_id: AuditEventId
    @required
    @protoField(number: 2)
    recorded_at: Timestamp
    @required
    @protoField(number: 3)
    org_id: GovernanceOrgId
    @required
    @protoField(number: 4)
    sequence: DecimalUint64
    @required
    @protoField(number: 5)
    event_source: AuditEventSource
    @required
    @protoField(number: 6)
    event_name: AuditEventOperationName
    @required
    @protoField(number: 7)
    audit_event: GovernanceAuditEventName
    @required
    @protoField(number: 8)
    actor_type: AuditActorType
    @required
    @protoField(number: 9)
    actor_id: AuditActorId
    @protoField(number: 10)
    credential_id: AuditCredentialId
    @required
    @protoField(number: 11)
    target_type: AuditTargetType
    @protoField(number: 12)
    target_id: AuditTargetId
    @protoField(number: 13)
    targetResourceName: ResourceName
    @required
    @protoField(number: 14)
    permission: GovernancePermissionName
    @required
    @protoField(number: 15)
    outcome: AuditOutcome
    @protoField(number: 16)
    error_code: AuditErrorCode
    @protoField(number: 17)
    trace_id: TraceId
    @required
    @protoField(number: 18)
    detail_sha256: SHA256Hex
    @required
    @protoField(number: 19)
    prev_hmac: HMACHex
    @required
    @protoField(number: 20)
    row_hmac: HMACHex
    @protoField(number: 21)
    hmac_key_id: AuditHMACKeyId
}
structure AuditFilters {
    @protoField(number: 1)
    actor_id: AuditActorId
    @protoField(number: 2)
    audit_event: GovernanceAuditEventName
    @protoField(number: 3)
    credential_id: AuditCredentialId
    @protoField(number: 4)
    event_name: AuditEventOperationName
    @protoField(number: 5)
    event_source: AuditEventSource
    @protoField(number: 6)
    outcome: AuditOutcome
    @protoField(number: 7)
    target_id: AuditTargetId
    @protoField(number: 8)
    target_type: AuditTargetType
    @protoField(number: 9)
    targetResourceName: ResourceName
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
    requested_by: AuditActorId
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
    created_at: Timestamp
    @required
    @protoField(number: 14)
    updated_at: Timestamp
    @protoField(number: 15)
    completed_at: Timestamp
    @required
    @protoField(number: 16)
    expires_at: Timestamp
    @protoField(number: 17)
    error_code: ExportErrorCode
    @protoField(number: 18)
    error_message: ExportErrorMessage
}
structure AuditRecord {
    @protoField(number: 1)
    schema_version: AuditSchemaVersion
    @required
    @protoField(number: 2)
    org_id: GovernanceOrgId
    @required
    @protoField(number: 3)
    event_source: AuditEventSource
    @required
    @protoField(number: 4)
    event_name: AuditEventOperationName
    @required
    @protoField(number: 5)
    audit_event: GovernanceAuditEventName
    @required
    @protoField(number: 6)
    actor_type: AuditActorType
    @required
    @protoField(number: 7)
    actor_id: AuditActorId
    @protoField(number: 8)
    credential_id: AuditCredentialId
    @required
    @protoField(number: 9)
    target_type: AuditTargetType
    @protoField(number: 10)
    target_id: AuditTargetId
    @protoField(number: 11)
    targetResourceName: ResourceName
    @required
    @protoField(number: 12)
    permission: GovernancePermissionName
    @required
    @protoField(number: 13)
    outcome: AuditOutcome
    @protoField(number: 14)
    error_code: AuditErrorCode
    @protoField(number: 15)
    trace_id: TraceId
    @protoField(number: 16)
    hmac_key_id: AuditHMACKeyId
    @protoField(number: 17)
    recorded_at: Timestamp
    detail: Document
}
structure AppendAuditEventAccepted {
    @required
    @protoField(number: 1)
    event_id: AuditEventId
    @required
    @protoField(number: 2)
    sequence: DecimalUint64
    @required
    @protoField(number: 3)
    row_hmac: HMACHex
}
@readonly
@http(method: "GET", uri: "/api/v1/governance/audit/events")
@paginated(inputToken: "cursor", outputToken: "next_cursor", pageSize: "limit", items: "events")
@identity(mode: "bearer", audience: "governance-service", principals: ["browser", "cli", "workload"])
@authz(permission: AuditLogReadPermission, organization: {source: "token_org_id"})
@audit(event: AuditLogListAuditEvent, resource: AuditLog, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "governance.audit", method: "listEvents", paginated: true, retryable: true)
operation ListAuditEvents {
    input: ListAuditEventsInput
    output: ListAuditEventsOutput
    errors: [
        UnauthenticatedError
        PermissionDeniedError
        RateLimitedError
        ServiceUnavailableError
    ]
}
structure ListAuditEventsInput {
    @httpQuery("limit")
    @protoField(number: 1)
    limit: AuditEventsLimit
    @httpQuery("cursor")
    @protoField(number: 2)
    cursor: AuditCursor
    @httpQuery("order")
    @protoField(number: 3)
    order: AuditListOrder
    @httpQuery("actor_id")
    @protoField(number: 4)
    actor_id: AuditActorId
    @httpQuery("audit_event")
    @protoField(number: 5)
    audit_event: GovernanceAuditEventName
    @httpQuery("credential_id")
    @protoField(number: 6)
    credential_id: AuditCredentialId
    @httpQuery("event_name")
    @protoField(number: 7)
    event_name: AuditEventOperationName
    @httpQuery("event_source")
    @protoField(number: 8)
    event_source: AuditEventSource
    @httpQuery("outcome")
    @protoField(number: 9)
    outcome: AuditOutcome
    @httpQuery("target_id")
    @protoField(number: 10)
    target_id: AuditTargetId
    @httpQuery("target_type")
    @protoField(number: 11)
    target_type: AuditTargetType
    @httpQuery("targetResourceName")
    @protoField(number: 12)
    targetResourceName: ResourceName
}
structure ListAuditEventsOutput {
    @required
    @protoField(number: 1)
    events: AuditEvents
    @protoField(number: 2)
    next_cursor: AuditCursor
    @required
    @protoField(number: 3)
    limit: AuditEventsLimit
    @required
    @protoField(number: 4)
    filters: AuditFilters
}
@readonly
@http(method: "GET", uri: "/api/v1/governance/exports")
@identity(mode: "bearer", audience: "governance-service", principals: ["browser", "cli", "workload"])
@authz(permission: AuditLogExportPermission, organization: {source: "token_org_id"})
@audit(event: DataExportListAuditEvent, resource: DataExport, action: "list")
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
@identity(mode: "bearer", audience: "governance-service", principals: ["browser", "cli"])
@authz(permission: AuditLogExportPermission, organization: {source: "token_org_id"})
@audit(event: DataExportCreateAuditEvent, resource: DataExport, action: "create")
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
@identity(mode: "bearer", audience: "governance-service", principals: ["browser", "cli", "workload"])
@authz(permission: AuditLogExportPermission, organization: {source: "token_org_id"})
@audit(event: DataExportReadAuditEvent, resource: DataExport, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "governance.exports", method: "get", paginated: false, retryable: true)
operation GetDataExport {
    input: GetDataExportInput
    output: GetDataExportOutput
    errors: [
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
    export_id: DataExportId}
structure GetDataExportOutput {
    @required
    @protoField(number: 1)
    export: DataExportJob
}
@readonly
@http(method: "GET", uri: "/api/v1/governance/exports/{export_id}/download")
@identity(mode: "bearer", audience: "governance-service", principals: ["browser", "cli"])
@authz(permission: AuditLogExportPermission, organization: {source: "token_org_id"})
@audit(event: DataExportDownloadAuditEvent, resource: DataExport, action: "download")
@rateLimit(bucket: "export_download")
@requestBudget(maxBytes: 0)
@sdk(module: "governance.exports", method: "download", paginated: false, retryable: true)
operation DownloadDataExport {
    input: DownloadDataExportInput
    output: DownloadDataExportOutput
    errors: [
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
    export_id: DataExportId}
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
@http(method: "POST", uri: "/internal/v1/audit/events", code: 202)
@identity(mode: "spiffe_mtls", audience: "governance-service", principals: ["workload"])
@authz(permission: AuditLogAppendPermission, organization: {source: "body_org_id", member: "org_id"})
@audit(event: AuditLogAppendAuditEvent, resource: AuditLog, action: "write")
@rateLimit(bucket: "internal_mutation")
@requestBudget(maxBytes: 32768)
@sdk(module: "governanceInternal.audit", method: "append", paginated: false, retryable: false)
operation AppendAuditEvent {
    input: AppendAuditEventInput
    output: AppendAuditEventOutput
    errors: [
        ValidationFailedError
        PermissionDeniedError
        ServiceUnavailableError
    ]
}
structure AppendAuditEventInput {
    @required
    @httpPayload
    @protoField(number: 1)
    record: AuditRecord
}
structure AppendAuditEventOutput {
    @required
    @protoField(number: 1)
    accepted: AppendAuditEventAccepted
}
