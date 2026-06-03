$version: "2"
namespace verself.cloudflare.v1

use aws.protocols#restJson1
use smithy.api#http
use smithy.api#httpHeader
use smithy.api#httpLabel
use smithy.api#httpQuery
use smithy.api#idempotent
use smithy.api#idempotencyToken
use smithy.api#length
use smithy.api#pattern
use smithy.api#range
use smithy.api#readonly
use smithy.api#required
use smithy.api#sensitive
use verself.common.v1#ConflictError
use verself.common.v1#DateTime
use verself.common.v1#IdempotencyKey
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

@serviceRuntime(
    serviceName: "cloudflare-integration-service",
    publicAudience: "cloudflare-integration-service",
    internalAudience: "cloudflare-integration-service"
)
@restJson1
service CloudflareIntegration {
    version: "2026-06-03"
    operations: [
        ImportCloudflareAuthority,
        VerifyCloudflareAuthority,
        RotateCloudflareAuthority,
        GetCloudflareOperation,
        ReconcileCloudflareSite,
        ReconcileCloudflareDNS,
        CreateDNSChallenge,
        DeleteDNSChallenge,
        ReconcileR2Capability,
        RotateR2CapabilityCredential,
        CreateR2TemporaryCredentials,
        CreatePresignedObjectURL,
        ReconcileEmailRouting
    ]
    resources: [
        CloudflareAuthority,
        CloudflareOperation,
        CloudflareDNSRecordSet,
        CloudflareDNSChallenge,
        CloudflareR2Capability,
        CloudflareEmailRouting
    ]
}

@length(min: 1, max: 64)
@pattern("^[a-z][a-z0-9_-]*$")
string SiteName

@length(min: 32, max: 32)
@pattern("^[0-9a-f]{32}$")
string CloudflareAccountId

@length(min: 1, max: 253)
@pattern("^[A-Za-z0-9*._-]+$")
string DNSName

@length(min: 1, max: 128)
string DNSZoneName

@length(min: 1, max: 4096)
string DNSRecordContent

@length(min: 1, max: 128)
string CloudflareProviderId

@length(min: 1, max: 128)
string CloudflareFingerprint

@length(min: 1, max: 4096)
@sensitive
string CloudflareApiToken

@length(min: 1, max: 128)
@pattern("^cfop_[0-9A-HJKMNP-TV-Z]{26}$")
string CloudflareOperationId

@length(min: 1, max: 128)
@pattern("^cfdns_[0-9A-HJKMNP-TV-Z]{26}$")
string CloudflareDNSChallengeId

@length(min: 1, max: 128)
@pattern("^cfr2cred_[0-9A-HJKMNP-TV-Z]{26}$")
string CloudflareR2CredentialId

@length(min: 3, max: 63)
@pattern("^[a-z0-9][a-z0-9.-]*[a-z0-9]$")
string R2BucketName

@length(min: 1, max: 2048)
string R2ObjectKey

@length(min: 0, max: 1024)
string R2ObjectPrefix

@length(min: 64, max: 64)
@pattern("^[0-9a-f]{64}$")
string SHA256Hex

@length(min: 1, max: 4096)
@sensitive
string PresignedURL

@length(min: 1, max: 255)
string AccessKeyId

@length(min: 1, max: 4096)
@sensitive
string SecretAccessKey

@length(min: 1, max: 4096)
@sensitive
string SessionToken

@length(min: 1, max: 4096)
@sensitive
string ProviderRequestId

@length(min: 0, max: 4096)
string ProviderDetail

@range(min: 60, max: 604800)
integer CredentialTTLSeconds

@range(min: 60, max: 86400)
integer PresignedURLTTLSeconds

@range(min: 0, max: 86400)
integer DNSTTLSeconds

@range(min: 0, max: 2147483647)
integer DNSPriority

enum CloudflareAuthoritySlot {
    A = "a"
    B = "b"
}

enum CloudflareCapability {
    DEPLOYMENT_ARTIFACTS = "deployment_artifacts"
    OBJECT_STORAGE_ADMIN = "object_storage_admin"
    OBJECT_STORAGE_PROXY = "object_storage_proxy"
    RECOVERY = "recovery"
    DNS = "dns"
    ACME_DNS_CHALLENGE = "acme_dns_challenge"
    EMAIL_ROUTING = "email_routing"
}

enum CloudflareOperationKind {
    IMPORT_AUTHORITY = "import_authority"
    VERIFY_AUTHORITY = "verify_authority"
    ROTATE_AUTHORITY = "rotate_authority"
    RECONCILE_SITE = "reconcile_site"
    RECONCILE_DNS = "reconcile_dns"
    CREATE_DNS_CHALLENGE = "create_dns_challenge"
    DELETE_DNS_CHALLENGE = "delete_dns_challenge"
    RECONCILE_R2_CAPABILITY = "reconcile_r2_capability"
    ROTATE_R2_CAPABILITY_CREDENTIAL = "rotate_r2_capability_credential"
    RECONCILE_EMAIL_ROUTING = "reconcile_email_routing"
}

enum CloudflareOperationState {
    QUEUED = "queued"
    RUNNING = "running"
    VERIFYING = "verifying"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    CANCELED = "canceled"
}

enum DNSRecordType {
    A = "A"
    AAAA = "AAAA"
    CNAME = "CNAME"
    TXT = "TXT"
    MX = "MX"
    CAA = "CAA"
}

enum DNSChangeAction {
    CREATE = "create"
    UPDATE = "update"
    DELETE = "delete"
    NOOP = "noop"
}

enum R2Permission {
    OBJECT_READ_ONLY = "object-read-only"
    OBJECT_READ_WRITE = "object-read-write"
    ADMIN_READ_ONLY = "admin-read-only"
    ADMIN_READ_WRITE = "admin-read-write"
}

enum PresignedObjectMethod {
    GET = "GET"
    PUT = "PUT"
    HEAD = "HEAD"
}

@permission(name: "cloudflare:authority:write")
string CloudflareAuthorityWritePermission

@permission(name: "cloudflare:authority:read")
string CloudflareAuthorityReadPermission

@permission(name: "cloudflare:dns:write")
string CloudflareDNSWritePermission

@permission(name: "cloudflare:r2:write")
string CloudflareR2WritePermission

@permission(name: "cloudflare:email_routing:write")
string CloudflareEmailRoutingWritePermission

@permission(name: "cloudflare:operation:read")
string CloudflareOperationReadPermission

@auditEvent(name: "cloudflare.authority.import")
string CloudflareAuthorityImportAuditEvent

@auditEvent(name: "cloudflare.authority.verify")
string CloudflareAuthorityVerifyAuditEvent

@auditEvent(name: "cloudflare.authority.rotate")
string CloudflareAuthorityRotateAuditEvent

@auditEvent(name: "cloudflare.operation.read")
string CloudflareOperationReadAuditEvent

@auditEvent(name: "cloudflare.site.reconcile")
string CloudflareSiteReconcileAuditEvent

@auditEvent(name: "cloudflare.dns.reconcile")
string CloudflareDNSReconcileAuditEvent

@auditEvent(name: "cloudflare.dns_challenge.create")
string CloudflareDNSChallengeCreateAuditEvent

@auditEvent(name: "cloudflare.dns_challenge.delete")
string CloudflareDNSChallengeDeleteAuditEvent

@auditEvent(name: "cloudflare.r2_capability.reconcile")
string CloudflareR2CapabilityReconcileAuditEvent

@auditEvent(name: "cloudflare.r2_capability_credential.rotate")
string CloudflareR2CapabilityCredentialRotateAuditEvent

@auditEvent(name: "cloudflare.r2_temporary_credentials.create")
string CloudflareR2TemporaryCredentialsCreateAuditEvent

@auditEvent(name: "cloudflare.r2_presigned_url.create")
string CloudflareR2PresignedURLCreateAuditEvent

@auditEvent(name: "cloudflare.email_routing.reconcile")
string CloudflareEmailRoutingReconcileAuditEvent

resource CloudflareAuthority {}
resource CloudflareOperation {}
resource CloudflareDNSRecordSet {}
resource CloudflareDNSChallenge {}
resource CloudflareR2Capability {}
resource CloudflareEmailRouting {}

structure CloudflareOperationView {
    @required
    @protoField(number: 1)
    operation_id: CloudflareOperationId

    @required
    @protoField(number: 2)
    resourceName: ResourceName

    @required
    @protoField(number: 3)
    kind: CloudflareOperationKind

    @required
    @protoField(number: 4)
    state: CloudflareOperationState

    @protoField(number: 5)
    site: SiteName

    @protoField(number: 6)
    capability: CloudflareCapability

    @protoField(number: 7)
    error_code: String

    @protoField(number: 8)
    error_detail: ProviderDetail

    @required
    @protoField(number: 9)
    created_at: DateTime

    @required
    @protoField(number: 10)
    updated_at: DateTime

    @protoField(number: 11)
    finished_at: DateTime
}

structure ProviderEvidence {
    @protoField(number: 1)
    provider_request_id: ProviderRequestId

    @protoField(number: 2)
    token_id_fingerprint: CloudflareFingerprint

    @protoField(number: 3)
    value_fingerprint: CloudflareFingerprint

    @protoField(number: 4)
    provider_status: String

    @protoField(number: 5)
    detail: ProviderDetail
}

structure DNSRecordSpec {
    @required
    @protoField(number: 1)
    zone: DNSZoneName

    @required
    @protoField(number: 2)
    name: DNSName

    @required
    @protoField(number: 3)
    type: DNSRecordType

    @required
    @protoField(number: 4)
    content: DNSRecordContent

    @required
    @protoField(number: 5)
    ttl_seconds: DNSTTLSeconds

    @required
    @protoField(number: 6)
    proxied: Boolean

    @protoField(number: 7)
    priority: DNSPriority
}

list DNSRecordSpecs {
    member: DNSRecordSpec
}

structure DNSChange {
    @required
    @protoField(number: 1)
    action: DNSChangeAction

    @required
    @protoField(number: 2)
    record: DNSRecordSpec

    @protoField(number: 3)
    provider_id_fingerprint: CloudflareFingerprint
}

list DNSChanges {
    member: DNSChange
}

structure R2ObjectScope {
    @required
    @protoField(number: 1)
    bucket: R2BucketName

    @required
    @protoField(number: 2)
    key: R2ObjectKey
}

list R2ObjectScopes {
    member: R2ObjectScope
}

list R2ObjectPrefixes {
    member: R2ObjectPrefix
}

map PresignedHeaders {
    key: String
    value: String
}

structure R2TemporaryCredentials {
    @required
    @protoField(number: 1)
    access_key_id: AccessKeyId

    @required
    @protoField(number: 2)
    secret_access_key: SecretAccessKey

    @protoField(number: 3)
    session_token: SessionToken

    @required
    @protoField(number: 4)
    endpoint: PresignedURL

    @required
    @protoField(number: 5)
    region: String

    @required
    @protoField(number: 6)
    expires_at: DateTime
}

structure EmailRoutingRuleSpec {
    @required
    @protoField(number: 1)
    zone: DNSZoneName

    @required
    @protoField(number: 2)
    match: DNSName

    @required
    @protoField(number: 3)
    forward_to: String
}

list EmailRoutingRuleSpecs {
    member: EmailRoutingRuleSpec
}

structure OperationOutput {
    @required
    @protoField(number: 1)
    operation: CloudflareOperationView
}

structure ImportCloudflareAuthorityInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    @protoField(number: 1)
    account_id: CloudflareAccountId

    @required
    @protoField(number: 2)
    slot: CloudflareAuthoritySlot

    @required
    @protoField(number: 3)
    api_token: CloudflareApiToken

    @protoField(number: 4)
    token_id: CloudflareProviderId

    @protoField(number: 5)
    expires_at: DateTime
}

@idempotent
@http(method: "POST", uri: "/internal/v1/cloudflare/authority:import", code: 202)
@identity(mode: "spiffe_mtls", audience: "cloudflare-integration-service", principals: ["cli", "workload"])
@authz(permission: CloudflareAuthorityWritePermission, organization: {source: "installation"})
@audit(event: CloudflareAuthorityImportAuditEvent, resource: CloudflareAuthority, action: "create")
@rateLimit(bucket: "cloudflare_authority_write")
@requestBudget(maxBytes: 65536)
@sdk(module: "cloudflare.authority", method: "import", paginated: false, retryable: false)
operation ImportCloudflareAuthority {
    input: ImportCloudflareAuthorityInput
    output: OperationOutput
    errors: [
        ValidationFailedError,
        UnauthenticatedError,
        PermissionDeniedError,
        ConflictError,
        RateLimitedError,
        ServiceUnavailableError
    ]
}

structure VerifyCloudflareAuthorityInput {
    @httpQuery("account_id")
    account_id: CloudflareAccountId
}

@http(method: "POST", uri: "/internal/v1/cloudflare/authority:verify", code: 202)
@identity(mode: "spiffe_mtls", audience: "cloudflare-integration-service", principals: ["cli", "workload"])
@authz(permission: CloudflareAuthorityReadPermission, organization: {source: "installation"})
@audit(event: CloudflareAuthorityVerifyAuditEvent, resource: CloudflareAuthority, action: "verify")
@rateLimit(bucket: "cloudflare_authority_read")
@requestBudget(maxBytes: 1024)
@sdk(module: "cloudflare.authority", method: "verify", paginated: false, retryable: true)
operation VerifyCloudflareAuthority {
    input: VerifyCloudflareAuthorityInput
    output: OperationOutput
    errors: [
        ValidationFailedError,
        UnauthenticatedError,
        PermissionDeniedError,
        RateLimitedError,
        ServiceUnavailableError
    ]
}

structure RotateCloudflareAuthorityInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @httpQuery("account_id")
    account_id: CloudflareAccountId
}

@idempotent
@http(method: "POST", uri: "/internal/v1/cloudflare/authority:rotate", code: 202)
@identity(mode: "spiffe_mtls", audience: "cloudflare-integration-service", principals: ["cli", "workload"])
@authz(permission: CloudflareAuthorityWritePermission, organization: {source: "installation"})
@audit(event: CloudflareAuthorityRotateAuditEvent, resource: CloudflareAuthority, action: "rotate")
@rateLimit(bucket: "cloudflare_authority_write")
@requestBudget(maxBytes: 1024)
@sdk(module: "cloudflare.authority", method: "rotate", paginated: false, retryable: false)
operation RotateCloudflareAuthority {
    input: RotateCloudflareAuthorityInput
    output: OperationOutput
    errors: [
        ValidationFailedError,
        UnauthenticatedError,
        PermissionDeniedError,
        ConflictError,
        RateLimitedError,
        ServiceUnavailableError
    ]
}

structure GetCloudflareOperationInput {
    @required
    @httpLabel
    operation_id: CloudflareOperationId
}

@readonly
@http(method: "GET", uri: "/internal/v1/cloudflare/operations/{operation_id}")
@identity(mode: "spiffe_mtls", audience: "cloudflare-integration-service", principals: ["cli", "workload"])
@authz(permission: CloudflareOperationReadPermission, organization: {source: "installation"})
@audit(event: CloudflareOperationReadAuditEvent, resource: CloudflareOperation, action: "read")
@rateLimit(bucket: "cloudflare_read")
@requestBudget(maxBytes: 0)
@sdk(module: "cloudflare.operations", method: "get", paginated: false, retryable: true)
operation GetCloudflareOperation {
    input: GetCloudflareOperationInput
    output: OperationOutput
    errors: [
        ValidationFailedError,
        UnauthenticatedError,
        PermissionDeniedError,
        ResourceNotFoundError,
        RateLimitedError,
        ServiceUnavailableError
    ]
}

structure ReconcileCloudflareSiteInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    @httpLabel
    site: SiteName

    @required
    @protoField(number: 1)
    dns_records: DNSRecordSpecs

    @required
    @protoField(number: 2)
    r2_capabilities: CloudflareCapabilities

    @required
    @protoField(number: 3)
    email_routing_rules: EmailRoutingRuleSpecs

    @required
    @protoField(number: 4)
    dry_run: Boolean
}

list CloudflareCapabilities {
    member: CloudflareCapability
}

@idempotent
@http(method: "POST", uri: "/internal/v1/cloudflare/sites/{site}/reconcile", code: 202)
@identity(mode: "spiffe_mtls", audience: "cloudflare-integration-service", principals: ["cli", "workload"])
@authz(permission: CloudflareAuthorityWritePermission, organization: {source: "installation"})
@audit(event: CloudflareSiteReconcileAuditEvent, resource: CloudflareAuthority, action: "sync")
@rateLimit(bucket: "cloudflare_reconcile")
@requestBudget(maxBytes: 262144)
@sdk(module: "cloudflare.sites", method: "reconcile", paginated: false, retryable: false)
operation ReconcileCloudflareSite {
    input: ReconcileCloudflareSiteInput
    output: OperationOutput
    errors: [
        ValidationFailedError,
        UnauthenticatedError,
        PermissionDeniedError,
        ConflictError,
        RateLimitedError,
        ServiceUnavailableError
    ]
}

structure ReconcileCloudflareDNSInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    @httpLabel
    site: SiteName

    @required
    @protoField(number: 1)
    records: DNSRecordSpecs

    @required
    @protoField(number: 2)
    dry_run: Boolean
}

structure ReconcileCloudflareDNSOutput {
    @required
    @protoField(number: 1)
    operation: CloudflareOperationView

    @required
    @protoField(number: 2)
    changes: DNSChanges
}

@idempotent
@http(method: "POST", uri: "/internal/v1/cloudflare/sites/{site}/dns:reconcile", code: 202)
@identity(mode: "spiffe_mtls", audience: "cloudflare-integration-service", principals: ["cli", "workload"])
@authz(permission: CloudflareDNSWritePermission, organization: {source: "installation"})
@audit(event: CloudflareDNSReconcileAuditEvent, resource: CloudflareDNSRecordSet, action: "sync")
@rateLimit(bucket: "cloudflare_dns_write")
@requestBudget(maxBytes: 262144)
@sdk(module: "cloudflare.dns", method: "reconcile", paginated: false, retryable: false)
operation ReconcileCloudflareDNS {
    input: ReconcileCloudflareDNSInput
    output: ReconcileCloudflareDNSOutput
    errors: [
        ValidationFailedError,
        UnauthenticatedError,
        PermissionDeniedError,
        ConflictError,
        RateLimitedError,
        ServiceUnavailableError
    ]
}

structure CreateDNSChallengeInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    @protoField(number: 1)
    zone: DNSZoneName

    @required
    @protoField(number: 2)
    name: DNSName

    @required
    @protoField(number: 3)
    value: DNSRecordContent

    @required
    @protoField(number: 4)
    ttl_seconds: DNSTTLSeconds
}

structure DNSChallengeOutput {
    @required
    @protoField(number: 1)
    challenge_id: CloudflareDNSChallengeId

    @required
    @protoField(number: 2)
    record: DNSRecordSpec

    @required
    @protoField(number: 3)
    evidence: ProviderEvidence

    @required
    @protoField(number: 4)
    created_at: DateTime
}

@idempotent
@http(method: "POST", uri: "/internal/v1/cloudflare/dns-challenges", code: 201)
@identity(mode: "spiffe_mtls", audience: "cloudflare-integration-service", principals: ["cli", "workload"])
@authz(permission: CloudflareDNSWritePermission, organization: {source: "installation"})
@audit(event: CloudflareDNSChallengeCreateAuditEvent, resource: CloudflareDNSChallenge, action: "create")
@rateLimit(bucket: "cloudflare_dns_write")
@requestBudget(maxBytes: 65536)
@sdk(module: "cloudflare.dnsChallenges", method: "create", paginated: false, retryable: false)
operation CreateDNSChallenge {
    input: CreateDNSChallengeInput
    output: DNSChallengeOutput
    errors: [
        ValidationFailedError,
        UnauthenticatedError,
        PermissionDeniedError,
        ConflictError,
        RateLimitedError,
        ServiceUnavailableError
    ]
}

structure DeleteDNSChallengeInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    @httpLabel
    challenge_id: CloudflareDNSChallengeId
}

@idempotent
@http(method: "POST", uri: "/internal/v1/cloudflare/dns-challenges/{challenge_id}/delete", code: 202)
@identity(mode: "spiffe_mtls", audience: "cloudflare-integration-service", principals: ["cli", "workload"])
@authz(permission: CloudflareDNSWritePermission, organization: {source: "installation"})
@audit(event: CloudflareDNSChallengeDeleteAuditEvent, resource: CloudflareDNSChallenge, action: "delete")
@rateLimit(bucket: "cloudflare_dns_write")
@requestBudget(maxBytes: 1024)
@sdk(module: "cloudflare.dnsChallenges", method: "delete", paginated: false, retryable: false)
operation DeleteDNSChallenge {
    input: DeleteDNSChallengeInput
    output: OperationOutput
    errors: [
        ValidationFailedError,
        UnauthenticatedError,
        PermissionDeniedError,
        ResourceNotFoundError,
        ConflictError,
        RateLimitedError,
        ServiceUnavailableError
    ]
}

structure ReconcileR2CapabilityInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    @httpLabel
    site: SiteName

    @required
    @httpLabel
    capability: CloudflareCapability

    @required
    @protoField(number: 1)
    bucket: R2BucketName

    @required
    @protoField(number: 2)
    prefixes: R2ObjectPrefixes
}

@idempotent
@http(method: "POST", uri: "/internal/v1/cloudflare/sites/{site}/r2/capabilities/{capability}/reconcile", code: 202)
@identity(mode: "spiffe_mtls", audience: "cloudflare-integration-service", principals: ["cli", "workload"])
@authz(permission: CloudflareR2WritePermission, organization: {source: "installation"})
@audit(event: CloudflareR2CapabilityReconcileAuditEvent, resource: CloudflareR2Capability, action: "sync")
@rateLimit(bucket: "cloudflare_r2_write")
@requestBudget(maxBytes: 65536)
@sdk(module: "cloudflare.r2.capabilities", method: "reconcile", paginated: false, retryable: false)
operation ReconcileR2Capability {
    input: ReconcileR2CapabilityInput
    output: OperationOutput
    errors: [
        ValidationFailedError,
        UnauthenticatedError,
        PermissionDeniedError,
        ConflictError,
        RateLimitedError,
        ServiceUnavailableError
    ]
}

structure RotateR2CapabilityCredentialInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    @httpLabel
    site: SiteName

    @required
    @httpLabel
    capability: CloudflareCapability

    @required
    @protoField(number: 1)
    ttl_seconds: CredentialTTLSeconds
}

@idempotent
@http(method: "POST", uri: "/internal/v1/cloudflare/sites/{site}/r2/capabilities/{capability}/credentials:rotate", code: 202)
@identity(mode: "spiffe_mtls", audience: "cloudflare-integration-service", principals: ["cli", "workload"])
@authz(permission: CloudflareR2WritePermission, organization: {source: "installation"})
@audit(event: CloudflareR2CapabilityCredentialRotateAuditEvent, resource: CloudflareR2Capability, action: "rotate")
@rateLimit(bucket: "cloudflare_r2_write")
@requestBudget(maxBytes: 65536)
@sdk(module: "cloudflare.r2.credentials", method: "rotate", paginated: false, retryable: false)
operation RotateR2CapabilityCredential {
    input: RotateR2CapabilityCredentialInput
    output: OperationOutput
    errors: [
        ValidationFailedError,
        UnauthenticatedError,
        PermissionDeniedError,
        ConflictError,
        RateLimitedError,
        ServiceUnavailableError
    ]
}

structure CreateR2TemporaryCredentialsInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    @httpLabel
    site: SiteName

    @required
    @protoField(number: 1)
    capability: CloudflareCapability

    @required
    @protoField(number: 2)
    permission: R2Permission

    @required
    @protoField(number: 3)
    objects: R2ObjectScopes

    @required
    @protoField(number: 4)
    prefixes: R2ObjectPrefixes

    @required
    @protoField(number: 5)
    ttl_seconds: CredentialTTLSeconds
}

structure CreateR2TemporaryCredentialsOutput {
    @required
    @protoField(number: 1)
    credentials: R2TemporaryCredentials
}

@idempotent
@http(method: "POST", uri: "/internal/v1/cloudflare/sites/{site}/r2/temporary-credentials", code: 201)
@identity(mode: "spiffe_mtls", audience: "cloudflare-integration-service", principals: ["workload"])
@authz(permission: CloudflareR2WritePermission, organization: {source: "installation"})
@audit(event: CloudflareR2TemporaryCredentialsCreateAuditEvent, resource: CloudflareR2Capability, action: "create")
@rateLimit(bucket: "cloudflare_r2_write")
@requestBudget(maxBytes: 65536)
@sdk(module: "cloudflare.r2.temporaryCredentials", method: "create", paginated: false, retryable: false)
operation CreateR2TemporaryCredentials {
    input: CreateR2TemporaryCredentialsInput
    output: CreateR2TemporaryCredentialsOutput
    errors: [
        ValidationFailedError,
        UnauthenticatedError,
        PermissionDeniedError,
        ConflictError,
        RateLimitedError,
        ServiceUnavailableError
    ]
}

structure CreatePresignedObjectURLInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    @httpLabel
    site: SiteName

    @required
    @protoField(number: 1)
    capability: CloudflareCapability

    @required
    @protoField(number: 2)
    method: PresignedObjectMethod

    @required
    @protoField(number: 3)
    object: R2ObjectScope

    @protoField(number: 4)
    sha256: SHA256Hex

    @required
    @protoField(number: 5)
    ttl_seconds: PresignedURLTTLSeconds
}

structure CreatePresignedObjectURLOutput {
    @required
    @protoField(number: 1)
    url: PresignedURL

    @required
    @protoField(number: 2)
    headers: PresignedHeaders

    @required
    @protoField(number: 3)
    expires_at: DateTime
}

@idempotent
@http(method: "POST", uri: "/internal/v1/cloudflare/sites/{site}/r2/presigned-urls", code: 201)
@identity(mode: "spiffe_mtls", audience: "cloudflare-integration-service", principals: ["workload"])
@authz(permission: CloudflareR2WritePermission, organization: {source: "installation"})
@audit(event: CloudflareR2PresignedURLCreateAuditEvent, resource: CloudflareR2Capability, action: "create")
@rateLimit(bucket: "cloudflare_r2_write")
@requestBudget(maxBytes: 65536)
@sdk(module: "cloudflare.r2.presignedUrls", method: "create", paginated: false, retryable: false)
operation CreatePresignedObjectURL {
    input: CreatePresignedObjectURLInput
    output: CreatePresignedObjectURLOutput
    errors: [
        ValidationFailedError,
        UnauthenticatedError,
        PermissionDeniedError,
        ConflictError,
        RateLimitedError,
        ServiceUnavailableError
    ]
}

structure ReconcileEmailRoutingInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    @httpLabel
    site: SiteName

    @required
    @protoField(number: 1)
    rules: EmailRoutingRuleSpecs

    @required
    @protoField(number: 2)
    dry_run: Boolean
}

@idempotent
@http(method: "POST", uri: "/internal/v1/cloudflare/sites/{site}/email-routing:reconcile", code: 202)
@identity(mode: "spiffe_mtls", audience: "cloudflare-integration-service", principals: ["cli", "workload"])
@authz(permission: CloudflareEmailRoutingWritePermission, organization: {source: "installation"})
@audit(event: CloudflareEmailRoutingReconcileAuditEvent, resource: CloudflareEmailRouting, action: "sync")
@rateLimit(bucket: "cloudflare_email_routing_write")
@requestBudget(maxBytes: 262144)
@sdk(module: "cloudflare.emailRouting", method: "reconcile", paginated: false, retryable: false)
operation ReconcileEmailRouting {
    input: ReconcileEmailRoutingInput
    output: OperationOutput
    errors: [
        ValidationFailedError,
        UnauthenticatedError,
        PermissionDeniedError,
        ConflictError,
        RateLimitedError,
        ServiceUnavailableError
    ]
}
