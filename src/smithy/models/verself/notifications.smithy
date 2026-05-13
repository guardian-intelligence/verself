$version: "2"
namespace verself.notifications.v1
use smithy.api#http
use smithy.api#httpHeader
use smithy.api#httpLabel
use smithy.api#httpQuery
use smithy.api#idempotencyToken
use smithy.api#idempotent
use smithy.api#length
use smithy.api#paginated
use smithy.api#pattern
use smithy.api#readonly
use smithy.api#required
use verself.common.v1#ConflictError
use verself.common.v1#IdempotencyKey
use verself.common.v1#IdempotencyPayloadMismatchError
use verself.common.v1#PageSize
use verself.common.v1#PageToken
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
@serviceRuntime(serviceName: "notifications-service", publicAudience: "notifications-service", internalAudience: "notifications-service")
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
@serviceRuntime(serviceName: "notifications-service", publicAudience: "notifications-service", internalAudience: "notifications-service")
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
@length(min: 1, max: 128)
string OrgId
@length(min: 1, max: 512)
string SubjectId
@length(min: 1, max: 320)
string EmailAddress
@length(min: 1, max: 128)
string WorkflowKey
@length(min: 1, max: 255)
string NotificationTitle
@length(max: 4096)
string NotificationBody
@length(max: 2048)
string ActionURL
@length(min: 1, max: 64)
string Priority
@length(min: 1, max: 128)
string NotificationType
@length(min: 1, max: 128)
string NotificationState
@length(min: 1, max: 128)
string DeliveryChannel
@length(min: 1, max: 255)
string AlertFingerprint
@length(min: 1, max: 128)
string LabelKey
@length(max: 512)
string LabelValue
list NotificationsList {
    member: NotificationRecord
}
list WorkflowRecipients {
    member: WorkflowRecipient
}
list NotificationLabels {
    member: NotificationLabel
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
    subject_id: SubjectId
    @required
    @protoField(number: 3)
    type: NotificationType
    @required
    @protoField(number: 4)
    title: NotificationTitle
    @protoField(number: 5)
    body: NotificationBody
    @protoField(number: 6)
    action_url: ActionURL
    @required
    @protoField(number: 7)
    state: NotificationState
    @required
    @protoField(number: 8)
    created_at: Timestamp
    @protoField(number: 9)
    read_at: Timestamp
    @protoField(number: 10)
    dismissed_at: Timestamp
}
structure NotificationSummary {
    @required
    @protoField(number: 1)
    unread_count: Integer
    @required
    @protoField(number: 2)
    latest_at: Timestamp
}
structure NotificationPreferencesView {
    @required
    @protoField(number: 1)
    enabled: Boolean
    @required
    @protoField(number: 2)
    email_enabled: Boolean
    @required
    @protoField(number: 3)
    in_app_enabled: Boolean
}
structure NotificationAccepted {
    @required
    @protoField(number: 1)
    accepted: Boolean
}
structure WorkflowRecipient {
    @required
    @protoField(number: 1)
    subject_id: SubjectId
    @protoField(number: 2)
    email: EmailAddress
}
structure NotificationLabel {
    @required
    @protoField(number: 1)
    key: LabelKey
    @protoField(number: 2)
    value: LabelValue
}
structure EmptyInput {}
@readonly
@http(method: "GET", uri: "/api/v1/notifications")
@paginated(inputToken: "cursor", outputToken: "next_cursor", pageSize: "limit", items: "notifications")
@identity(mode: "bearer", audience: "notifications-service", principals: ["browser", "cli"])
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
    limit: PageSize
    @httpQuery("cursor")
    cursor: PageToken
}
structure ListNotificationsOutput {
    @required
    notifications: NotificationsList
    next_cursor: PageToken
}
@readonly
@http(method: "GET", uri: "/api/v1/notifications/summary")
@identity(mode: "bearer", audience: "notifications-service", principals: ["browser", "cli"])
@authz(permission: NotificationsReadPermission, organization: {source: "request_subject"})
@audit(event: NotificationSummaryReadAuditEvent, resource: NotificationSubject, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "notifications", method: "summary", paginated: false, retryable: true)
operation GetNotificationSummary {
    input: EmptyInput
    output: GetNotificationSummaryOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}
structure GetNotificationSummaryOutput {
    @required
    summary: NotificationSummary
}
@idempotent
@http(method: "PUT", uri: "/api/v1/notifications/preferences")
@identity(mode: "bearer", audience: "notifications-service", principals: ["browser", "cli"])
@authz(permission: NotificationPreferencesWritePermission, organization: {source: "request_subject"})
@audit(event: NotificationPreferencesWriteAuditEvent, resource: NotificationPreferences, action: "write")
@rateLimit(bucket: "notification_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "notifications.preferences", method: "put", paginated: false, retryable: false)
operation PutNotificationPreferences {
    input: PutNotificationPreferencesInput
    output: NotificationPreferencesOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
structure PutNotificationPreferencesInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
    @required
    preferences: NotificationPreferencesView
}
structure NotificationPreferencesOutput {
    @required
    preferences: NotificationPreferencesView
}
@idempotent
@http(method: "POST", uri: "/api/v1/notifications/read-cursor")
@identity(mode: "bearer", audience: "notifications-service", principals: ["browser", "cli"])
@authz(permission: NotificationsWritePermission, organization: {source: "request_subject"})
@audit(event: NotificationReadCursorAdvanceAuditEvent, resource: NotificationSubject, action: "write")
@rateLimit(bucket: "notification_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "notifications", method: "advanceReadCursor", paginated: false, retryable: false)
operation AdvanceNotificationReadCursor {
    input: NotificationIdempotentInput
    output: NotificationAcceptedOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
@idempotent
@http(method: "POST", uri: "/api/v1/notifications/{notification_id}/dismiss")
@identity(mode: "bearer", audience: "notifications-service", principals: ["browser", "cli"])
@authz(permission: NotificationsWritePermission, organization: {source: "request_subject"})
@audit(event: NotificationDismissAuditEvent, resource: NotificationSubject, action: "write")
@rateLimit(bucket: "notification_mutation")
@requestBudget(maxBytes: 1)
@sdk(module: "notifications", method: "dismiss", paginated: false, retryable: false)
operation DismissNotification {
    input: NotificationPathIdempotentInput
    output: NotificationAcceptedOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
@idempotent
@http(method: "POST", uri: "/api/v1/notifications/{notification_id}/read")
@identity(mode: "bearer", audience: "notifications-service", principals: ["browser", "cli"])
@authz(permission: NotificationsWritePermission, organization: {source: "request_subject"})
@audit(event: NotificationMarkReadAuditEvent, resource: NotificationSubject, action: "write")
@rateLimit(bucket: "notification_mutation")
@requestBudget(maxBytes: 1)
@sdk(module: "notifications", method: "markRead", paginated: false, retryable: false)
operation MarkNotificationRead {
    input: NotificationPathIdempotentInput
    output: NotificationAcceptedOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
@idempotent
@http(method: "POST", uri: "/api/v1/notifications/clear")
@identity(mode: "bearer", audience: "notifications-service", principals: ["browser", "cli"])
@authz(permission: NotificationsWritePermission, organization: {source: "request_subject"})
@audit(event: NotificationsClearAuditEvent, resource: NotificationSubject, action: "write")
@rateLimit(bucket: "notification_mutation")
@requestBudget(maxBytes: 1)
@sdk(module: "notifications", method: "clear", paginated: false, retryable: false)
operation ClearNotifications {
    input: NotificationIdempotentInput
    output: NotificationAcceptedOutput
    errors: [UnauthenticatedError, PermissionDeniedError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
@idempotent
@http(method: "POST", uri: "/api/v1/notifications/test", code: 202)
@identity(mode: "bearer", audience: "notifications-service", principals: ["browser", "cli"])
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
    @required
    title: NotificationTitle
    body: NotificationBody
    action_url: ActionURL
}
structure NotificationAcceptedOutput {
    @required
    accepted: NotificationAccepted
}
@idempotent
@http(method: "POST", uri: "/internal/v1/workflows/{workflow_key}/trigger", code: 202)
@identity(mode: "spiffe_mtls", audience: "notifications-service", principals: ["workload"])
@authz(permission: NotificationWorkflowTriggerPermission, organization: {source: "body_org_id", member: "org_id"})
@audit(event: NotificationWorkflowTriggerAuditEvent, resource: NotificationWorkflow, action: "create")
@rateLimit(bucket: "internal_workflow")
@requestBudget(maxBytes: 262144)
@sdk(module: "notificationsInternal.workflows", method: "trigger", paginated: false, retryable: false)
operation TriggerNotificationWorkflow {
    input: TriggerNotificationWorkflowInput
    output: NotificationAcceptedOutput
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
    @required
    title: NotificationTitle
    body: NotificationBody
    action_url: ActionURL
    priority: Priority
    targetResourceName: ResourceName
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
    output: NotificationAcceptedOutput
    errors: [ValidationFailedError, PermissionDeniedError, ServiceUnavailableError]
}
structure ReceiveGrafanaAlertWebhookInput {
    @required
    fingerprint: AlertFingerprint
    @required
    labels: NotificationLabels
}
