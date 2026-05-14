$version: "2"
namespace verself.notifications.v1

use aws.protocols#restJson1

use smithy.api#http
use smithy.api#httpHeader
use smithy.api#httpLabel
use smithy.api#httpPayload
use smithy.api#nestedProperties
use smithy.api#httpQuery
use smithy.api#idempotencyToken
use smithy.api#idempotent
use smithy.api#length
use smithy.api#paginated
use smithy.api#pattern
use smithy.api#range
use smithy.api#readonly
use smithy.api#required
use verself.common.v1#ConflictError
use verself.common.v1#IdempotencyKey
use verself.common.v1#IdempotencyPayloadMismatchError
use verself.common.v1#PageToken
use verself.common.v1#PermissionDeniedError
use verself.common.v1#RateLimitedError
use verself.common.v1#ResourceName
use verself.common.v1#ResourceNotFoundError
use verself.common.v1#ServiceUnavailableError
use verself.common.v1#TraceParent
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

@serviceRuntime(serviceName: "notifications-service", publicAudience: "verself-api", internalAudience: "notifications-service")
@restJson1
service Notifications {
    version: "2026-05-13"
    operations: [
        ListNotifications,
        GetNotificationSummary,
        PutNotificationPreferences,
        AdvanceNotificationReadCursor,
        DismissNotification,
        MarkNotificationRead,
        ClearNotifications,
        PublishTestNotification
    ]
    resources: [
        NotificationSubject,
        NotificationPreferences,
        Notification
    ]
}

@serviceRuntime(serviceName: "notifications-service", publicAudience: "verself-api", internalAudience: "notifications-service")
@restJson1
service NotificationsInternal {
    version: "2026-05-13"
    operations: [
        TriggerNotificationWorkflow,
        ReceiveGrafanaAlertWebhook
    ]
    resources: [
        NotificationWorkflow,
        NotificationIntegration
    ]
}

@pattern("^[0-9a-fA-F-]{36}$")
string NotificationId

@pattern("^[0-9a-fA-F-]{36}$")
string WorkflowRunId

@length(min: 1, max: 128)
@pattern("^org_[0-9A-HJKMNP-TV-Z]{26}$")
string OrgId

@length(min: 1, max: 512)
string SubjectId

@length(max: 320)
string EmailAddress

@length(min: 1, max: 128)
@pattern("^[a-z0-9._-]+$")
string WorkflowKey

@length(max: 240)
string NotificationTitle

@length(max: 4096)
string NotificationBody

@length(min: 1, max: 4096)
string RequiredNotificationBody

@length(max: 8192)
string ActionURL

@length(min: 1, max: 128)
string NotificationKind

@pattern("^[0-9]+$")
string DecimalInt64

@range(min: 0, max: 999)
integer UnreadCount

@range(min: 0, max: 2147483647)
integer PreferenceVersion

@range(min: 1, max: 500)
integer NotificationPageSize

@range(min: 0, max: 200)
integer NotificationSmallCount

@length(min: 1, max: 255)
string GrafanaString

@length(max: 8192)
string GrafanaURL

@length(min: 1, max: 128)
string GrafanaLabelKey

@length(max: 512)
string GrafanaLabelValue

enum NotificationPriority {
    LOW = "low"
    NORMAL = "normal"
    HIGH = "high"
}

list NotificationsList {
    member: NotificationRecord
}

@length(min: 1, max: 100)
list WorkflowRecipients {
    member: WorkflowRecipient
}

list GrafanaAlerts {
    member: GrafanaAlert
}

map GrafanaLabelMap {
    key: GrafanaLabelKey
    value: GrafanaLabelValue
}

@permission(name: "notifications:self:read")
string NotificationsReadPermission

@permission(name: "notifications:self:write")
string NotificationsWritePermission

@permission(name: "notifications:self:preferences:write")
string NotificationPreferencesWritePermission

@permission(name: "notifications:self:test")
string NotificationsTestPermission

@permission(name: "notifications:workflow:trigger")
string NotificationWorkflowTriggerPermission

@permission(name: "notifications:integration:receive")
string NotificationIntegrationReceivePermission

@auditEvent(name: "notifications.list")
string NotificationListAuditEvent

@auditEvent(name: "notifications.summary.read")
string NotificationSummaryReadAuditEvent

@auditEvent(name: "notifications.preferences.write")
string NotificationPreferencesWriteAuditEvent

@auditEvent(name: "notifications.read_cursor.advance")
string NotificationReadCursorAdvanceAuditEvent

@auditEvent(name: "notifications.dismiss")
string NotificationDismissAuditEvent

@auditEvent(name: "notifications.mark_read")
string NotificationMarkReadAuditEvent

@auditEvent(name: "notifications.clear")
string NotificationsClearAuditEvent

@auditEvent(name: "notifications.test.publish")
string NotificationTestPublishAuditEvent

@auditEvent(name: "notifications.workflow.trigger")
string NotificationWorkflowTriggerAuditEvent

@auditEvent(name: "notifications.integration.grafana.alert")
string GrafanaAlertWebhookAuditEvent

resource NotificationSubject {}
resource NotificationPreferences {}
resource Notification {}
resource NotificationWorkflow {}
resource NotificationIntegration {}

structure NotificationRecord {
    @required
    @protoField(number: 1)
    notification_id: NotificationId

    @required
    @protoField(number: 2)
    resourceName: ResourceName

    @required
    @protoField(number: 3)
    org_id: OrgId

    @required
    @protoField(number: 4)
    recipient_subject_id: SubjectId

    @required
    @protoField(number: 5)
    recipient_sequence: DecimalInt64

    @required
    @protoField(number: 6)
    kind: NotificationKind

    @required
    @protoField(number: 7)
    priority: NotificationPriority

    @required
    @protoField(number: 8)
    title: NotificationTitle

    @required
    @protoField(number: 9)
    body: RequiredNotificationBody

    @protoField(number: 10)
    action_url: ActionURL

    @protoField(number: 11)
    targetResourceName: ResourceName

    @required
    @protoField(number: 12)
    created_at: DateTime

    @protoField(number: 13)
    expires_at: DateTime

    @protoField(number: 14)
    read_at: DateTime

    @protoField(number: 15)
    dismissed_at: DateTime
}

structure NotificationSummary {
    @required
    @protoField(number: 1)
    org_id: OrgId

    @required
    @protoField(number: 2)
    subject_id: SubjectId

    @required
    @protoField(number: 3)
    unread_count: UnreadCount

    @required
    @protoField(number: 4)
    latest_sequence: DecimalInt64

    @required
    @protoField(number: 5)
    read_up_to_sequence: DecimalInt64

    @required
    @protoField(number: 6)
    preferences: NotificationPreferencesView

    @protoField(number: 7)
    latest_notification: NotificationRecord
}

structure NotificationPreferencesView {
    @required
    @protoField(number: 1)
    enabled: Boolean

    @required
    @protoField(number: 2)
    web_enabled: Boolean

    @required
    @protoField(number: 3)
    email_enabled: Boolean

    @required
    @protoField(number: 4)
    push_enabled: Boolean

    @required
    @protoField(number: 5)
    sms_enabled: Boolean

    @required
    @protoField(number: 6)
    version: PreferenceVersion

    @required
    @protoField(number: 7)
    updated_at: DateTime

    @protoField(number: 8)
    updated_by: SubjectId
}

structure NotificationAccepted {
    @required
    @protoField(number: 1)
    event_id: NotificationId

    @protoField(number: 2)
    traceparent: TraceParent
}

structure WorkflowRecipient {
    @protoField(number: 1)
    subject_id: SubjectId

    @protoField(number: 2)
    email: EmailAddress
}

structure NotificationWorkflowTriggerResult {
    @required
    @protoField(number: 1)
    workflow_run_id: WorkflowRunId

    @required
    @protoField(number: 2)
    workflow_key: WorkflowKey

    @required
    @protoField(number: 3)
    org_id: OrgId

    @required
    @protoField(number: 4)
    accepted_at: DateTime

    @required
    @protoField(number: 5)
    recipient_count: NotificationSmallCount

    @required
    @protoField(number: 6)
    web_notifications_accepted: NotificationSmallCount

    @required
    @protoField(number: 7)
    email_deliveries_queued: NotificationSmallCount

    @required
    @protoField(number: 8)
    suppressed_count: NotificationSmallCount

    @protoField(number: 9)
    traceparent: TraceParent
}

structure GrafanaWebhookPayload {
    @protoField(number: 1)
    receiver: GrafanaString

    @protoField(number: 2)
    status: GrafanaString

    @protoField(number: 3)
    orgId: OrgId

    @protoField(number: 4)
    groupKey: GrafanaString

    @protoField(number: 5)
    externalURL: GrafanaURL

    @protoField(number: 6)
    title: NotificationTitle

    @protoField(number: 7)
    message: NotificationBody

    @protoField(number: 8)
    commonLabels: GrafanaLabelMap

    @protoField(number: 9)
    commonAnnotations: GrafanaLabelMap

    @protoField(number: 10)
    alerts: GrafanaAlerts
}

structure GrafanaAlert {
    @protoField(number: 1)
    status: GrafanaString

    @protoField(number: 2)
    labels: GrafanaLabelMap

    @protoField(number: 3)
    annotations: GrafanaLabelMap

    @protoField(number: 4)
    startsAt: DateTime

    @protoField(number: 5)
    endsAt: DateTime

    @protoField(number: 6)
    generatorURL: GrafanaURL

    @protoField(number: 7)
    dashboardURL: GrafanaURL

    @protoField(number: 8)
    panelURL: GrafanaURL

    @protoField(number: 9)
    silenceURL: GrafanaURL

    @protoField(number: 10)
    fingerprint: GrafanaString
}

structure EmptyInput {}

@readonly
@http(method: "GET", uri: "/api/v1/notifications")
@paginated(inputToken: "cursor", outputToken: "next_cursor", pageSize: "limit", items: "notifications")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: NotificationsReadPermission, organization: {source: "request_subject"})
@audit(event: NotificationListAuditEvent, resource: NotificationSubject, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "notifications", method: "list", paginated: true, retryable: true)
operation ListNotifications {
    input: ListNotificationsInput
    output: ListNotificationsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

structure ListNotificationsInput {
    @httpQuery("limit")
    limit: NotificationPageSize

    @httpQuery("cursor")
    cursor: PageToken
}

structure ListNotificationsOutput {
    @required
    @protoField(number: 1)
    summary: NotificationSummary

    @required
    @protoField(number: 2)
    notifications: NotificationsList

    @protoField(number: 100)
    next_cursor: PageToken
}

@readonly
@http(method: "GET", uri: "/api/v1/notifications/summary")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: NotificationsReadPermission, organization: {source: "request_subject"})
@audit(event: NotificationSummaryReadAuditEvent, resource: NotificationSubject, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "notifications", method: "summary", paginated: false, retryable: true)
operation GetNotificationSummary {
    input: EmptyInput
    output: NotificationSummaryOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

structure NotificationSummaryOutput {
    @required
    @httpPayload
    @nestedProperties
    @protoField(number: 1)
    summary: NotificationSummary
}

@idempotent
@http(method: "PUT", uri: "/api/v1/notifications/preferences")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: NotificationPreferencesWritePermission, organization: {source: "request_subject"})
@audit(event: NotificationPreferencesWriteAuditEvent, resource: NotificationPreferences, action: "write")
@rateLimit(bucket: "notification_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "notifications.preferences", method: "put", paginated: false, retryable: false)
operation PutNotificationPreferences {
    input: PutNotificationPreferencesInput
    output: NotificationSummaryOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure PutNotificationPreferencesInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    version: PreferenceVersion

    @required
    enabled: Boolean

    web_enabled: Boolean

    email_enabled: Boolean

    push_enabled: Boolean

    sms_enabled: Boolean
}

@idempotent
@http(method: "POST", uri: "/api/v1/notifications/read-cursor")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: NotificationsWritePermission, organization: {source: "request_subject"})
@audit(event: NotificationReadCursorAdvanceAuditEvent, resource: NotificationSubject, action: "write")
@rateLimit(bucket: "notification_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "notifications", method: "advanceReadCursor", paginated: false, retryable: false)
operation AdvanceNotificationReadCursor {
    input: AdvanceNotificationReadCursorInput
    output: NotificationSummaryOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure AdvanceNotificationReadCursorInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    read_up_to_sequence: DecimalInt64
}

@idempotent
@http(method: "POST", uri: "/api/v1/notifications/{notification_id}/dismiss")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: NotificationsWritePermission, organization: {source: "request_subject"})
@audit(event: NotificationDismissAuditEvent, resource: NotificationSubject, action: "write")
@rateLimit(bucket: "notification_mutation")
@requestBudget(maxBytes: 1024)
@sdk(module: "notifications", method: "dismiss", paginated: false, retryable: false)
operation DismissNotification {
    input: NotificationPathIdempotentInput
    output: NotificationSummaryOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

@idempotent
@http(method: "POST", uri: "/api/v1/notifications/{notification_id}/read")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: NotificationsWritePermission, organization: {source: "request_subject"})
@audit(event: NotificationMarkReadAuditEvent, resource: NotificationSubject, action: "write")
@rateLimit(bucket: "notification_mutation")
@requestBudget(maxBytes: 1024)
@sdk(module: "notifications", method: "markRead", paginated: false, retryable: false)
operation MarkNotificationRead {
    input: NotificationPathIdempotentInput
    output: NotificationSummaryOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

@idempotent
@http(method: "POST", uri: "/api/v1/notifications/clear")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: NotificationsWritePermission, organization: {source: "request_subject"})
@audit(event: NotificationsClearAuditEvent, resource: NotificationSubject, action: "write")
@rateLimit(bucket: "notification_mutation")
@requestBudget(maxBytes: 1024)
@sdk(module: "notifications", method: "clear", paginated: false, retryable: false)
operation ClearNotifications {
    input: NotificationIdempotentInput
    output: NotificationSummaryOutput
    errors: [UnauthenticatedError, PermissionDeniedError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

@idempotent
@http(method: "POST", uri: "/api/v1/notifications/test", code: 202)
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: NotificationsTestPermission, organization: {source: "request_subject"})
@audit(event: NotificationTestPublishAuditEvent, resource: NotificationSubject, action: "test")
@rateLimit(bucket: "notification_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "notifications", method: "publishTest", paginated: false, retryable: false)
operation PublishTestNotification {
    input: PublishTestNotificationInput
    output: NotificationAcceptedOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure NotificationIdempotentInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
}

structure NotificationPathIdempotentInput {
    @required
    @httpLabel
    notification_id: NotificationId

    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
}

structure PublishTestNotificationInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    title: NotificationTitle

    body: NotificationBody

    action_url: ActionURL
}

structure NotificationAcceptedOutput {
    @required
    @httpPayload
    @nestedProperties
    @protoField(number: 1)
    accepted: NotificationAccepted
}

@idempotent
@http(method: "POST", uri: "/internal/v1/workflows/{workflow_key}/trigger", code: 202)
@identity(mode: "spiffe_mtls", audience: "notifications-service", principals: ["workload"])
@authz(permission: NotificationWorkflowTriggerPermission, organization: {source: "body_org_id", member: "org_id"})
@audit(event: NotificationWorkflowTriggerAuditEvent, resource: NotificationWorkflow, action: "create")
@rateLimit(bucket: "internal_workflow")
@requestBudget(maxBytes: 65536)
@sdk(module: "notificationsInternal.workflows", method: "trigger", paginated: false, retryable: false)
operation TriggerNotificationWorkflow {
    input: TriggerNotificationWorkflowInput
    output: NotificationWorkflowTriggerOutput
    errors: [ValidationFailedError, PermissionDeniedError, IdempotencyPayloadMismatchError, ServiceUnavailableError]
}

structure TriggerNotificationWorkflowInput {
    @required
    @httpLabel
    workflow_key: WorkflowKey

    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    org_id: OrgId

    @required
    recipients: WorkflowRecipients

    title: NotificationTitle

    @required
    body: RequiredNotificationBody

    action_url: ActionURL

    priority: NotificationPriority

    targetResourceName: ResourceName

    data: Document

    traceparent: TraceParent
}

structure NotificationWorkflowTriggerOutput {
    @required
    @httpPayload
    @nestedProperties
    @protoField(number: 1)
    result: NotificationWorkflowTriggerResult
}

@http(method: "POST", uri: "/internal/v1/integrations/grafana/alerts", code: 202)
@identity(mode: "spiffe_mtls", audience: "notifications-service", principals: ["workload"])
@authz(permission: NotificationIntegrationReceivePermission, organization: {source: "request_id"})
@audit(event: GrafanaAlertWebhookAuditEvent, resource: NotificationIntegration, action: "ingest")
@rateLimit(bucket: "provider_webhook")
@requestBudget(maxBytes: 262144)
@sdk(module: "notificationsInternal.integrations", method: "receiveGrafanaAlerts", paginated: false, retryable: false)
operation ReceiveGrafanaAlertWebhook {
    input: ReceiveGrafanaAlertWebhookInput
    output: NotificationWorkflowTriggerOutput
    errors: [ValidationFailedError, PermissionDeniedError, ServiceUnavailableError]
}

structure ReceiveGrafanaAlertWebhookInput {
    @required
    @httpPayload
    payload: GrafanaWebhookPayload
}
