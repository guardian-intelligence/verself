$version: "2"
namespace verself.mailbox.v1
use smithy.api#http
use smithy.api#httpHeader
use smithy.api#httpLabel
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
@serviceRuntime(serviceName: "mailbox-service", publicAudience: "mailbox-service", internalAudience: "mailbox-service")
service Mailbox {
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
        MailboxEmail,
        MailboxAccount,
        MailboxSyncStatus
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
@length(min: 1, max: 1024)
string MailSubject
@length(max: 1048576)
string MailBodyText
@length(min: 1, max: 128)
string MailSyncState
@permission(name: "mailbox:account:read")
string MailboxAccountReadPermission
@permission(name: "mailbox:mail:read")
string MailboxMailReadPermission
@permission(name: "mailbox:mail:write")
string MailboxMailWritePermission
@permission(name: "mailbox:sync_status:read")
string MailboxSyncStatusReadPermission
@auditEvent(name: "mailbox.mail_mark_read")
string MailMarkReadAuditEvent
@auditEvent(name: "mailbox.mail_mark_unread")
string MailMarkUnreadAuditEvent
@auditEvent(name: "mailbox.mail_flag")
string MailFlagAuditEvent
@auditEvent(name: "mailbox.mail_unflag")
string MailUnflagAuditEvent
@auditEvent(name: "mailbox.mail_move")
string MailMoveAuditEvent
@auditEvent(name: "mailbox.mail_trash")
string MailTrashAuditEvent
@auditEvent(name: "mailbox.mail_body")
string MailBodyAuditEvent
@auditEvent(name: "mailbox.mail_account")
string MailAccountAuditEvent
@auditEvent(name: "mailbox.mail_sync_status")
string MailSyncStatusAuditEvent
resource MailboxEmail {}
resource MailboxAccount {}
resource MailboxSyncStatus {}
structure MailMutationResult {
    @required
    @protoField(number: 1)
    email_id: EmailId
    @required
    @protoField(number: 2)
    changed: Boolean
}
structure MailBodyResult {
    @required
    @protoField(number: 1)
    email_id: EmailId
    @required
    @protoField(number: 2)
    subject: MailSubject
    @required
    @protoField(number: 3)
    body: MailBodyText
}
structure MailboxAccountView {
    @required
    @protoField(number: 1)
    account_id: AccountId
    @required
    @protoField(number: 2)
    email: EmailAddress
}
structure MailboxSyncStatusView {
    @required
    @protoField(number: 1)
    state: MailSyncState
    @required
    @protoField(number: 2)
    synced_at: Timestamp
}
structure MailEmailPathInput {
    @required
    @httpLabel
    email_id: EmailId
}
structure MailEmailIdempotentInput {
    @required
    @httpLabel
    email_id: EmailId
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
}
structure MailMoveInput {
    @required
    @httpLabel
    email_id: EmailId
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
    @required
    mailbox_id: MailboxId
}
structure EmptyInput {}
structure MailMutationOutput {
    @required
    result: MailMutationResult
}
@idempotent
@http(method: "POST", uri: "/api/v1/mail/emails/{email_id}/read")
@identity(mode: "bearer", audience: "mailbox-service", principals: ["browser", "cli"])
@authz(permission: MailboxMailWritePermission, organization: {source: "request_subject"})
@audit(event: MailMarkReadAuditEvent, resource: MailboxEmail, action: "write")
@rateLimit(bucket: "mail_mutation")
@requestBudget(maxBytes: 1)
@sdk(module: "mailbox.mail", method: "markRead", paginated: false, retryable: false)
operation MailMarkRead {
    input: MailEmailIdempotentInput
    output: MailMutationOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
@idempotent
@http(method: "POST", uri: "/api/v1/mail/emails/{email_id}/unread")
@identity(mode: "bearer", audience: "mailbox-service", principals: ["browser", "cli"])
@authz(permission: MailboxMailWritePermission, organization: {source: "request_subject"})
@audit(event: MailMarkUnreadAuditEvent, resource: MailboxEmail, action: "write")
@rateLimit(bucket: "mail_mutation")
@requestBudget(maxBytes: 1)
@sdk(module: "mailbox.mail", method: "markUnread", paginated: false, retryable: false)
operation MailMarkUnread {
    input: MailEmailIdempotentInput
    output: MailMutationOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
@idempotent
@http(method: "POST", uri: "/api/v1/mail/emails/{email_id}/flag")
@identity(mode: "bearer", audience: "mailbox-service", principals: ["browser", "cli"])
@authz(permission: MailboxMailWritePermission, organization: {source: "request_subject"})
@audit(event: MailFlagAuditEvent, resource: MailboxEmail, action: "write")
@rateLimit(bucket: "mail_mutation")
@requestBudget(maxBytes: 1)
@sdk(module: "mailbox.mail", method: "flag", paginated: false, retryable: false)
operation MailFlag {
    input: MailEmailIdempotentInput
    output: MailMutationOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
@idempotent
@http(method: "POST", uri: "/api/v1/mail/emails/{email_id}/unflag")
@identity(mode: "bearer", audience: "mailbox-service", principals: ["browser", "cli"])
@authz(permission: MailboxMailWritePermission, organization: {source: "request_subject"})
@audit(event: MailUnflagAuditEvent, resource: MailboxEmail, action: "write")
@rateLimit(bucket: "mail_mutation")
@requestBudget(maxBytes: 1)
@sdk(module: "mailbox.mail", method: "unflag", paginated: false, retryable: false)
operation MailUnflag {
    input: MailEmailIdempotentInput
    output: MailMutationOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
@idempotent
@http(method: "POST", uri: "/api/v1/mail/emails/{email_id}/move")
@identity(mode: "bearer", audience: "mailbox-service", principals: ["browser", "cli"])
@authz(permission: MailboxMailWritePermission, organization: {source: "request_subject"})
@audit(event: MailMoveAuditEvent, resource: MailboxEmail, action: "write")
@rateLimit(bucket: "mail_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "mailbox.mail", method: "move", paginated: false, retryable: false)
operation MailMove {
    input: MailMoveInput
    output: MailMutationOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
@idempotent
@http(method: "POST", uri: "/api/v1/mail/emails/{email_id}/trash")
@identity(mode: "bearer", audience: "mailbox-service", principals: ["browser", "cli"])
@authz(permission: MailboxMailWritePermission, organization: {source: "request_subject"})
@audit(event: MailTrashAuditEvent, resource: MailboxEmail, action: "write")
@rateLimit(bucket: "mail_mutation")
@requestBudget(maxBytes: 1)
@sdk(module: "mailbox.mail", method: "trash", paginated: false, retryable: false)
operation MailTrash {
    input: MailEmailIdempotentInput
    output: MailMutationOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
@readonly
@http(method: "GET", uri: "/api/v1/mail/emails/{email_id}/body")
@identity(mode: "bearer", audience: "mailbox-service", principals: ["browser", "cli"])
@authz(permission: MailboxMailReadPermission, organization: {source: "request_subject"})
@audit(event: MailBodyAuditEvent, resource: MailboxEmail, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "mailbox.mail", method: "body", paginated: false, retryable: true)
operation MailBody {
    input: MailEmailPathInput
    output: MailBodyOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}
structure MailBodyOutput {
    @required
    body: MailBodyResult
}
@readonly
@http(method: "GET", uri: "/api/v1/mail/account")
@identity(mode: "bearer", audience: "mailbox-service", principals: ["browser", "cli"])
@authz(permission: MailboxAccountReadPermission, organization: {source: "request_subject"})
@audit(event: MailAccountAuditEvent, resource: MailboxAccount, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "mailbox.account", method: "get", paginated: false, retryable: true)
operation MailAccount {
    input: EmptyInput
    output: MailAccountOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}
structure MailAccountOutput {
    @required
    account: MailboxAccountView
}
@readonly
@http(method: "GET", uri: "/api/v1/mail/sync/status")
@identity(mode: "bearer", audience: "mailbox-service", principals: ["browser", "cli"])
@authz(permission: MailboxSyncStatusReadPermission, organization: {source: "request_subject"})
@audit(event: MailSyncStatusAuditEvent, resource: MailboxSyncStatus, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "mailbox.sync", method: "status", paginated: false, retryable: true)
operation MailSyncStatus {
    input: EmptyInput
    output: MailSyncStatusOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}
structure MailSyncStatusOutput {
    @required
    status: MailboxSyncStatusView
}
