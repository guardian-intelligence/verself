$version: "2"

namespace verself.common.v1

use smithy.api#error
use smithy.api#httpError
use smithy.api#httpQuery
use smithy.api#idRef
use smithy.api#length
use smithy.api#mixin
use smithy.api#notProperty
use smithy.api#pattern
use smithy.api#range
use smithy.api#required
use smithy.api#sensitive
use smithy.api#trait
use smithy.api#timestampFormat

/// Opaque installation identifier generated during first bootstrap.
@pattern("^inst_[0-9A-HJKMNP-TV-Z]{26}$")
string InstallationId

/// Opaque, type-prefixed UUIDv7-compatible resource identifier.
@pattern("^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$")
string ResourceId

/// Customer or SDK supplied retry key for idempotent mutations.
@length(min: 8, max: 128)
@sensitive
string IdempotencyKey

/// W3C traceparent header value.
@length(min: 55, max: 255)
string TraceParent

/// Stable request identifier echoed in successful responses and problem details.
@length(min: 8, max: 128)
string RequestId

/// Opaque cursor token. Services reauthorize every request that presents one.
@length(min: 1, max: 4096)
@sensitive
string PageToken

/// RFC 3339 timestamp used by public JSON APIs.
@timestampFormat("date-time")
timestamp DateTime

/// Cursor page size accepted by public list operations.
@range(min: 1, max: 500)
integer PageSize

/// Human-facing label. It is not unique and must not be used as identity.
@length(min: 1, max: 120)
string DisplayName

/// Stable product permission name checked through iam-service.
@pattern("^[a-z][a-z0-9_-]*(?::[a-z][a-z0-9_-]*)+$")
string PermissionName

/// Stable product resource kind used for contract metadata and audit.
@pattern("^[a-z][a-z0-9_]*$")
string ResourceKind

/// Globally unique Verself resource name.
@length(min: 1, max: 4096)
@pattern("^urn:verself:.+$")
string ResourceName

/// Stable dotted audit event name.
@pattern("^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)+$")
string AuditEventName

/// OCSF class UID emitted by governance-service for this operation.
@range(min: 1, max: 65535)
integer OCSFClassUid

/// OCSF class name emitted by governance-service for this operation.
@length(min: 1, max: 120)
string OCSFClassName

/// Service-owned rate-limit bucket name.
@pattern("^[a-z][a-z0-9_]*$")
string RateLimitBucket

/// Expected token audience, SPIFFE service name, or checkout grant audience.
@length(min: 1, max: 160)
@pattern("^([a-z][a-z0-9-]*-(service|api)|https://[A-Za-z0-9.-]+|spiffe://.+)$")
string Audience

/// Repository-owned service binary name.
@length(min: 3, max: 80)
@pattern("^[a-z][a-z0-9-]*-service$")
string ServiceName

/// Stable Verself problem code used by SDK error normalization.
@pattern("^[a-z][a-z0-9_]*(\\.[a-z][a-z0-9_]*)*$")
string ProblemCode

/// RFC 9457 problem type URI.
@pattern("^(https://.+|urn:verself:problem:.+)$")
string ProblemType

/// Optional human-readable problem detail.
@length(max: 4096)
string ProblemDetail

/// Marks a string shape as the declaration site for a product permission.
@trait(selector: "string")
structure permission {
    @required
    name: PermissionName
}

/// Marks a string shape as the declaration site for a governance audit event.
@trait(selector: "string")
structure auditEvent {
    @required
    name: AuditEventName
}

/// Marks a concrete error as a stable Verself RFC 9457 problem.
@trait(selector: "structure[trait|smithy.api#error]")
structure problem {
    @required
    type: ProblemType

    @required
    code: ProblemCode
}

@idRef(failWhenMissing: true, selector: "string[trait|verself.common.v1#permission]")
string PermissionShape

@idRef(failWhenMissing: true, selector: "string[trait|verself.common.v1#auditEvent]")
string AuditEventShape

@idRef(failWhenMissing: true, selector: "resource")
string ResourceShape

/// Runtime service metadata required by deployment, auth, and projections.
@trait(selector: "service")
structure serviceRuntime {
    @required
    serviceName: ServiceName

    @required
    publicAudience: Audience

    internalAudience: Audience
}

enum AuthMode {
    BEARER = "bearer"
    BROWSER_SESSION = "browser_session"
    SPIFFE_MTLS = "spiffe_mtls"
    SPIFFE_MTLS_BEARER = "spiffe_mtls_bearer"
    CHECKOUT_GRANT = "checkout_grant"
    PROVIDER_WEBHOOK = "provider_webhook"
    PUBLIC = "public"
}

enum PrincipalKind {
    BROWSER = "browser"
    CLI = "cli"
    WORKLOAD = "workload"
    GITHUB_ACTION = "github_action"
    PROVIDER = "provider"
    ANONYMOUS = "anonymous"
}

list PrincipalKinds {
    member: PrincipalKind
}

/// Authentication expectations beyond the native Smithy auth scheme.
@trait(selector: "operation")
structure identity {
    @required
    mode: AuthMode

    @required
    audience: Audience

    @required
    principals: PrincipalKinds
}

enum OrganizationScopeSource {
    INSTALLATION = "installation"
    TOKEN_ORG_ID = "token_org_id"
    INPUT_MEMBER = "input_member"
    REQUEST_SUBJECT = "request_subject"
    BODY_ORG_ID = "body_org_id"
    REQUEST_ORG_ID = "request_org_id"
    REQUEST_SUBJECT_ID = "request_subject_id"
    REQUEST_ID = "request_id"
    CHECKOUT_GRANT = "checkout_grant"
}

/// Organization boundary derivation used before Zanzibar authorization.
structure OrganizationBinding {
    @required
    source: OrganizationScopeSource

    /// Required when source is input_member.
    member: String
}

/// Zanzibar authorization metadata for a product operation.
@trait(selector: "operation")
structure authz {
    @required
    permission: PermissionShape

    @required
    organization: OrganizationBinding
}

enum Action {
    ARCHIVE = "archive"
    CANCEL = "cancel"
    CONNECT = "connect"
    CREATE = "create"
    DECRYPT = "decrypt"
    DELETE = "delete"
    DISABLE = "disable"
    DOWNLOAD = "download"
    ERASE = "erase"
    ENCRYPT = "encrypt"
    EXPORT = "export"
    INGEST = "ingest"
    INVITE = "invite"
    LIST = "list"
    MANAGE = "manage"
    PAUSE = "pause"
    QUERY = "query"
    READ = "read"
    REGISTER = "register"
    RESOLVE = "resolve"
    RESUME = "resume"
    RESTORE = "restore"
    REVOKE = "revoke"
    ROLL = "roll"
    ROTATE = "rotate"
    SEARCH = "search"
    SET = "set"
    SIGN = "sign"
    SYNC = "sync"
    TEST = "test"
    UPDATE = "update"
    VERIFY = "verify"
    WRITE = "write"
}

/// Audit metadata projected into governance-service and OCSF builders.
@trait(selector: "operation")
structure audit {
    @required
    event: AuditEventShape

    @required
    resource: ResourceShape

    @required
    action: Action

    ocsf_class_uid: OCSFClassUid

    ocsf_class_name: OCSFClassName
}

/// Service-owned request throttle bucket.
@trait(selector: "operation")
structure rateLimit {
    @required
    bucket: RateLimitBucket
}

/// Maximum accepted request body bytes. Zero is valid only for bodyless operations.
@trait(selector: "operation")
structure requestBudget {
    @required
    maxBytes: Long
}

/// SDK operation placement and transport behavior.
@trait(selector: "operation")
structure sdk {
    @required
    module: String

    @required
    method: String

    @required
    paginated: Boolean

    @required
    retryable: Boolean
}

enum OperationEffect {
    READ = "read"
    WRITE = "write"
}

/// Product-side operation effect when HTTP method semantics are too coarse.
@trait(selector: "operation")
structure operationSemantics {
    @required
    effect: OperationEffect
}

@range(min: 1, max: 536870911)
integer ProtoFieldNumber

@length(min: 1, max: 80)
@pattern("^[a-z][a-z0-9_]*$")
string ProtoFieldName

/// Stable protobuf field number for the future Buf projection.
@trait(selector: "structure > member")
structure protoField {
    @required
    number: ProtoFieldNumber

    name: ProtoFieldName
}

/// Marks pagination and transport-only members as unrelated to resource state.
@notProperty
@trait(selector: "structure > member")
structure notResourceProperty {}

/// Common cursor request members for growing collections.
@mixin
structure PageRequest {
    @notResourceProperty
    @httpQuery("page_size")
    @protoField(number: 100)
    pageSize: PageSize

    @notResourceProperty
    @httpQuery("page_token")
    @protoField(number: 101)
    pageToken: PageToken
}

/// Common cursor response members for growing collections.
@mixin
structure PageResponse {
    @protoField(number: 100)
    nextPageToken: PageToken
}

/// RFC 9457 problem document members shared by concrete error shapes.
@mixin
structure ProblemDetails {
    @required
    @protoField(number: 1)
    type: ProblemType

    @required
    @protoField(number: 2)
    title: String

    @required
    @protoField(number: 3)
    status: Integer

    @protoField(number: 4)
    detail: ProblemDetail

    @protoField(number: 5)
    instance: String

    @required
    @protoField(number: 6)
    code: ProblemCode

    @protoField(number: 7)
    requestId: RequestId

    @protoField(number: 8)
    traceparent: TraceParent
}

@error("client")
@httpError(400)
@problem(type: "urn:verself:problem:request:validation_failed", code: "request.validation_failed")
structure ValidationFailedError with [ProblemDetails] {}

@error("client")
@httpError(401)
@problem(type: "urn:verself:problem:auth:unauthenticated", code: "auth.unauthenticated")
structure UnauthenticatedError with [ProblemDetails] {}

@error("client")
@httpError(403)
@problem(type: "urn:verself:problem:auth:permission_denied", code: "auth.permission_denied")
structure PermissionDeniedError with [ProblemDetails] {}

@error("client")
@httpError(404)
@problem(type: "urn:verself:problem:resource:not_found", code: "resource.not_found")
structure ResourceNotFoundError with [ProblemDetails] {}

@error("client")
@httpError(409)
@problem(type: "urn:verself:problem:conflict:state", code: "conflict.state")
structure ConflictError with [ProblemDetails] {}

@error("client")
@httpError(409)
@problem(type: "urn:verself:problem:conflict:idempotency_payload_mismatch", code: "conflict.idempotency_payload_mismatch")
structure IdempotencyPayloadMismatchError with [ProblemDetails] {}

@error("client")
@httpError(402)
@problem(type: "urn:verself:problem:billing:payment_required", code: "billing.payment_required")
structure PaymentRequiredError with [ProblemDetails] {}

@error("client")
@httpError(429)
@problem(type: "urn:verself:problem:quota:rate_limited", code: "quota.rate_limited")
structure RateLimitedError with [ProblemDetails] {}

@error("server")
@httpError(503)
@problem(type: "urn:verself:problem:service:unavailable", code: "service.unavailable")
structure ServiceUnavailableError with [ProblemDetails] {}
