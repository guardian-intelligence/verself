$version: "2"
namespace verself.email.v1

use aws.protocols#restJson1
use smithy.api#http
use smithy.api#httpHeader
use smithy.api#httpLabel
use smithy.api#httpPayload
use smithy.api#nestedProperties
use smithy.api#idempotencyToken
use smithy.api#idempotent
use smithy.api#length
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
@serviceRuntime(serviceName: "email-service", publicAudience: "verself-api", internalAudience: "email-service")
@restJson1
service Email {
    version: "2026-05-13"
    operations: [
        MailMarkRead,
        MailMarkUnread,
        MailFlag,
        MailUnflag,
        MailMove,
        MailTrash,
        MailBody,
        MailAccount,
        MailSyncStatus
    ]
    resources: [
        EmailMessage,
        EmailAccount,
        EmailSyncStatus
    ]
}
@length(min: 1, max: 255)
string EmailId
@length(min: 1, max: 255)
string MailboxId
@length(min: 1, max: 255)
string AccountId
@length(min: 1, max: 320)
string EmailAddress
@length(max: 1048576)
string MailBodyText
@length(max: 1048576)
string MailBodyHTML
@length(max: 255)
string MailboxDisplayName
@length(max: 4096)
string MailboxStatusError
@length(max: 1024)
string EmailServiceURL
@length(min: 1, max: 64)
string EmailProviderName
@permission(name: "email:account:read")
string EmailAccountReadPermission
@permission(name: "email:mail:read")
string EmailMailReadPermission
@permission(name: "email:mail:write")
string EmailMailWritePermission
@permission(name: "email:sync_status:read")
string EmailSyncStatusReadPermission
@auditEvent(name: "email.mail_mark_read")
string MailMarkReadAuditEvent
@auditEvent(name: "email.mail_mark_unread")
string MailMarkUnreadAuditEvent
@auditEvent(name: "email.mail_flag")
string MailFlagAuditEvent
@auditEvent(name: "email.mail_unflag")
string MailUnflagAuditEvent
@auditEvent(name: "email.mail_move")
string MailMoveAuditEvent
@auditEvent(name: "email.mail_trash")
string MailTrashAuditEvent
@auditEvent(name: "email.mail_body")
string MailBodyAuditEvent
@auditEvent(name: "email.mail_account")
string MailAccountAuditEvent
@auditEvent(name: "email.mail_sync_status")
string MailSyncStatusAuditEvent
resource EmailMessage {}
resource EmailAccount {}
resource EmailSyncStatus {}
structure MailMutationResult {
    @required
    @protoField(number: 1)
    email_id: EmailId
    @required
    @protoField(number: 2)
    status: String
}
structure MailBodyResult {
    @required
    @protoField(number: 1)
    account_id: AccountId
    @required
    @protoField(number: 2)
    email_id: EmailId
    @required
    @protoField(number: 3)
    text_body: MailBodyText
    @required
    @protoField(number: 4)
    html_body: MailBodyHTML
    @required
    @protoField(number: 5)
    fetched_at: DateTime
}
structure MailboxAccountView {
    @required
    @protoField(number: 1)
    account_id: AccountId
    @required
    @protoField(number: 2)
    email_address: EmailAddress
    @required
    @protoField(number: 3)
    display_name: MailboxDisplayName
    @protoField(number: 4)
    default_mailbox_id: MailboxId
}
structure MailboxSyncAccountStatusView {
    @required
    @protoField(number: 1)
    account_id: AccountId
    @required
    @protoField(number: 2)
    running: Boolean
    @required
    @protoField(number: 3)
    connected: Boolean
    @protoField(number: 4)
    last_sync_at: DateTime
    @protoField(number: 5)
    last_event_at: DateTime
    @protoField(number: 6)
    last_connected_at: DateTime
    @protoField(number: 7)
    last_error: MailboxStatusError
}
map MailboxSyncAccounts {
    key: AccountId
    value: MailboxSyncAccountStatusView
}
structure MailboxSyncWorkerStatusView {
    @required
    @protoField(number: 1)
    running: Boolean
    @protoField(number: 2)
    last_discovery_at: DateTime
    @protoField(number: 3)
    last_error: MailboxStatusError
    @required
    @protoField(number: 4)
    accounts: MailboxSyncAccounts
}
structure MailboxSyncStatusView {
    @required
    @protoField(number: 1)
    started_at: DateTime
    @required
    @protoField(number: 2)
    stalwart_base_url: EmailServiceURL
    @required
    @protoField(number: 3)
    public_base_url: EmailServiceURL
    @required
    @protoField(number: 4)
    mailbox_sync: MailboxSyncWorkerStatusView
    @required
    @protoField(number: 5)
    sender_provider: EmailProviderName
}
structure MailEmailPathInput {
    @required
    @httpLabel
    @protoField(number: 1)
    email_id: EmailId
}
structure MailEmailIdempotentInput {
    @required
    @httpLabel
    @protoField(number: 1)
    email_id: EmailId
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    @protoField(number: 100)
    idempotencyKey: IdempotencyKey
}
structure MailMoveInput {
    @required
    @httpLabel
    @protoField(number: 1)
    email_id: EmailId
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    @protoField(number: 100)
    idempotencyKey: IdempotencyKey
    @required
    @protoField(number: 2)
    mailbox_id: MailboxId
}
structure EmptyInput {}
structure MailMutationOutput {
    @required
    @httpPayload
    @nestedProperties
    @protoField(number: 1)
    result: MailMutationResult
}
@idempotent
@http(method: "POST", uri: "/api/v1/email/emails/{email_id}/read")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: EmailMailWritePermission, organization: {source: "request_subject"})
@audit(event: MailMarkReadAuditEvent, resource: EmailMessage, action: "write")
@rateLimit(bucket: "mail_mutation")
@requestBudget(maxBytes: 1024)
@sdk(module: "email.mail", method: "markRead", paginated: false, retryable: false)
operation MailMarkRead {
    input: MailEmailIdempotentInput
    output: MailMutationOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
@idempotent
@http(method: "POST", uri: "/api/v1/email/emails/{email_id}/unread")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: EmailMailWritePermission, organization: {source: "request_subject"})
@audit(event: MailMarkUnreadAuditEvent, resource: EmailMessage, action: "write")
@rateLimit(bucket: "mail_mutation")
@requestBudget(maxBytes: 1024)
@sdk(module: "email.mail", method: "markUnread", paginated: false, retryable: false)
operation MailMarkUnread {
    input: MailEmailIdempotentInput
    output: MailMutationOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
@idempotent
@http(method: "POST", uri: "/api/v1/email/emails/{email_id}/flag")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: EmailMailWritePermission, organization: {source: "request_subject"})
@audit(event: MailFlagAuditEvent, resource: EmailMessage, action: "write")
@rateLimit(bucket: "mail_mutation")
@requestBudget(maxBytes: 1024)
@sdk(module: "email.mail", method: "flag", paginated: false, retryable: false)
operation MailFlag {
    input: MailEmailIdempotentInput
    output: MailMutationOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
@idempotent
@http(method: "POST", uri: "/api/v1/email/emails/{email_id}/unflag")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: EmailMailWritePermission, organization: {source: "request_subject"})
@audit(event: MailUnflagAuditEvent, resource: EmailMessage, action: "write")
@rateLimit(bucket: "mail_mutation")
@requestBudget(maxBytes: 1024)
@sdk(module: "email.mail", method: "unflag", paginated: false, retryable: false)
operation MailUnflag {
    input: MailEmailIdempotentInput
    output: MailMutationOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
@idempotent
@http(method: "POST", uri: "/api/v1/email/emails/{email_id}/move")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: EmailMailWritePermission, organization: {source: "request_subject"})
@audit(event: MailMoveAuditEvent, resource: EmailMessage, action: "write")
@rateLimit(bucket: "mail_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "email.mail", method: "move", paginated: false, retryable: false)
operation MailMove {
    input: MailMoveInput
    output: MailMutationOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
@idempotent
@http(method: "POST", uri: "/api/v1/email/emails/{email_id}/trash")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: EmailMailWritePermission, organization: {source: "request_subject"})
@audit(event: MailTrashAuditEvent, resource: EmailMessage, action: "write")
@rateLimit(bucket: "mail_mutation")
@requestBudget(maxBytes: 1024)
@sdk(module: "email.mail", method: "trash", paginated: false, retryable: false)
operation MailTrash {
    input: MailEmailIdempotentInput
    output: MailMutationOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
@readonly
@http(method: "GET", uri: "/api/v1/email/emails/{email_id}/body")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: EmailMailReadPermission, organization: {source: "request_subject"})
@audit(event: MailBodyAuditEvent, resource: EmailMessage, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "email.mail", method: "body", paginated: false, retryable: true)
operation MailBody {
    input: MailEmailPathInput
    output: MailBodyOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}
structure MailBodyOutput {
    @required
    @httpPayload
    @protoField(number: 1)
    body: MailBodyResult
}
@readonly
@http(method: "GET", uri: "/api/v1/email/account")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: EmailAccountReadPermission, organization: {source: "request_subject"})
@audit(event: MailAccountAuditEvent, resource: EmailAccount, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "email.account", method: "get", paginated: false, retryable: true)
operation MailAccount {
    input: EmptyInput
    output: MailAccountOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}
structure MailAccountOutput {
    @required
    @httpPayload
    @nestedProperties
    @protoField(number: 1)
    account: MailboxAccountView
}
@readonly
@http(method: "GET", uri: "/api/v1/email/sync/status")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli"])
@authz(permission: EmailSyncStatusReadPermission, organization: {source: "request_subject"})
@audit(event: MailSyncStatusAuditEvent, resource: EmailSyncStatus, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "email.sync", method: "status", paginated: false, retryable: true)
operation MailSyncStatus {
    input: EmptyInput
    output: MailSyncStatusOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}
structure MailSyncStatusOutput {
    @required
    @protoField(number: 1)
    status: MailboxSyncStatusView
}
