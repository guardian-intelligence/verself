$version: "2"
namespace verself.analytics.v1

use aws.protocols#restJson1
use smithy.api#http
use smithy.api#httpHeader
use smithy.api#idempotencyToken
use smithy.api#idempotent
use smithy.api#length
use smithy.api#pattern
use smithy.api#readonly
use smithy.api#required
use verself.common.v1#ConflictError
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
use verself.common.v1#DateTime
@serviceRuntime(serviceName: "analytics-service", publicAudience: "analytics-service", internalAudience: "analytics-service")
@restJson1
service Analytics {
    version: "2026-05-13"
    operations: [
        IngestAnalyticsLogs,
        QueryAnalyticsEvents,
        QueryRawAnalyticsEvents,
        ManageAnalyticsDataset
    ]
    resources: [AnalyticsDataset]
}
@length(min: 1, max: 128)
string OrgId
@length(min: 1, max: 128)
@pattern("^[a-z][a-z0-9_]*$")
string DatasetName
@length(min: 1, max: 1024)
string EventName
@length(min: 1, max: 8192)
string AnalyticsQuery
@pattern("^[0-9]+$")
string DecimalUint64
list AnalyticsEvents {
    member: AnalyticsEvent
}
@permission(name: "analytics:dataset:read")
string AnalyticsDatasetReadPermission
@permission(name: "analytics:dataset:read_raw")
string AnalyticsDatasetReadRawPermission
@permission(name: "analytics:dataset:ingest")
string AnalyticsDatasetIngestPermission
@permission(name: "analytics:dataset:manage")
string AnalyticsDatasetManagePermission
@auditEvent(name: "analytics.dataset.ingest")
string AnalyticsDatasetIngestAuditEvent
@auditEvent(name: "analytics.dataset.query")
string AnalyticsDatasetQueryAuditEvent
@auditEvent(name: "analytics.dataset.raw_query")
string AnalyticsDatasetRawQueryAuditEvent
@auditEvent(name: "analytics.dataset.manage")
string AnalyticsDatasetManageAuditEvent
resource AnalyticsDataset {}
structure AnalyticsEvent {
    @required
    @protoField(number: 1)
    name: EventName
    @required
    @protoField(number: 2)
    observed_at: DateTime
    @required
    @protoField(number: 3)
    body: String
}
structure AnalyticsIngestResult {
    @required
    @protoField(number: 1)
    accepted: DecimalUint64
}
structure AnalyticsQueryResult {
    @required
    @protoField(number: 1)
    rows: AnalyticsEvents
}
structure AnalyticsDatasetView {
    @required
    @protoField(number: 1)
    org_id: OrgId
    @required
    @protoField(number: 2)
    dataset: DatasetName
    @required
    @protoField(number: 3)
    enabled: Boolean
}
@idempotent
@http(method: "POST", uri: "/api/v1/analytics/logs:ingest")
@identity(mode: "bearer", audience: "analytics-service", principals: ["browser", "cli", "workload"])
@authz(permission: AnalyticsDatasetIngestPermission, organization: {source: "token_org_id"})
@audit(event: AnalyticsDatasetIngestAuditEvent, resource: AnalyticsDataset, action: "ingest")
@rateLimit(bucket: "analytics_ingest")
@requestBudget(maxBytes: 1048576)
@sdk(module: "analytics.logs", method: "ingest", paginated: false, retryable: false)
operation IngestAnalyticsLogs {
    input: IngestAnalyticsLogsInput
    output: IngestAnalyticsLogsOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
structure IngestAnalyticsLogsInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
    @required
    dataset: DatasetName
    @required
    events: AnalyticsEvents
}
structure IngestAnalyticsLogsOutput {
    @required
    result: AnalyticsIngestResult
}
@http(method: "POST", uri: "/api/v1/analytics/events:query")
@identity(mode: "bearer", audience: "analytics-service", principals: ["browser", "cli", "workload"])
@authz(permission: AnalyticsDatasetReadPermission, organization: {source: "token_org_id"})
@audit(event: AnalyticsDatasetQueryAuditEvent, resource: AnalyticsDataset, action: "query")
@rateLimit(bucket: "analytics_query")
@requestBudget(maxBytes: 65536)
@sdk(module: "analytics.events", method: "query", paginated: false, retryable: true)
operation QueryAnalyticsEvents {
    input: QueryAnalyticsEventsInput
    output: QueryAnalyticsEventsOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}
structure QueryAnalyticsEventsInput {
    @required
    dataset: DatasetName
    @required
    query: AnalyticsQuery
}
structure QueryAnalyticsEventsOutput {
    @required
    result: AnalyticsQueryResult
}
@http(method: "POST", uri: "/api/v1/analytics/events/raw:query")
@identity(mode: "bearer", audience: "analytics-service", principals: ["browser", "cli", "workload"])
@authz(permission: AnalyticsDatasetReadRawPermission, organization: {source: "token_org_id"})
@audit(event: AnalyticsDatasetRawQueryAuditEvent, resource: AnalyticsDataset, action: "query")
@rateLimit(bucket: "analytics_query")
@requestBudget(maxBytes: 65536)
@sdk(module: "analytics.events", method: "queryRaw", paginated: false, retryable: true)
operation QueryRawAnalyticsEvents {
    input: QueryAnalyticsEventsInput
    output: QueryAnalyticsEventsOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}
@idempotent
@http(method: "PUT", uri: "/api/v1/analytics/dataset")
@identity(mode: "bearer", audience: "analytics-service", principals: ["browser", "cli", "workload"])
@authz(permission: AnalyticsDatasetManagePermission, organization: {source: "token_org_id"})
@audit(event: AnalyticsDatasetManageAuditEvent, resource: AnalyticsDataset, action: "manage")
@rateLimit(bucket: "analytics_manage")
@requestBudget(maxBytes: 65536)
@sdk(module: "analytics.datasets", method: "manage", paginated: false, retryable: false)
operation ManageAnalyticsDataset {
    input: ManageAnalyticsDatasetInput
    output: ManageAnalyticsDatasetOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
structure ManageAnalyticsDatasetInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
    @required
    dataset: DatasetName
    @required
    enabled: Boolean
}
structure ManageAnalyticsDatasetOutput {
    @required
    dataset: AnalyticsDatasetView
}
