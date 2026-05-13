$version: "2"
namespace verself.objectstorage.v1
use smithy.api#http
use smithy.api#httpHeader
use smithy.api#httpLabel
use smithy.api#idempotencyToken
use smithy.api#idempotent
use smithy.api#length
use smithy.api#pattern
use smithy.api#readonly
use smithy.api#required
use smithy.api#sensitive
use verself.common.v1#ConflictError
use verself.common.v1#DisplayName
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
@serviceRuntime(serviceName: "object-storage-service", publicAudience: "object-storage-service", internalAudience: "object-storage-service")
service ObjectStorage {
    version: "2026-05-13"
    operations: [
        ListObjectStorageBuckets,
        CreateObjectStorageBucket,
        GetObjectStorageBucket,
        UpdateObjectStorageBucket,
        DeleteObjectStorageBucket,
        CreateObjectStorageBucketAlias,
        ListObjectStorageBucketAliases,
        DeleteObjectStorageBucketAlias,
        ListObjectStorageCredentials,
        CreateObjectStorageAccessKey,
        RollObjectStorageAccessKey,
        DeleteObjectStorageAccessKey,
        CreateObjectStorageMTLSPrincipal,
        DeleteObjectStorageMTLSPrincipal
    ]
    resources: [
        Bucket,
        BucketAlias,
        ObjectStorageCredential,
        ObjectStorageAccessKey,
        ObjectStorageMTLSPrincipal
    ]
}
@pattern("^[0-9a-fA-F-]{36}$")
string BucketId
@pattern("^[0-9a-fA-F-]{36}$")
string CredentialId
@length(min: 1, max: 128)
string OrgId
@length(min: 3, max: 63)
@pattern("^[a-z0-9][a-z0-9.-]*[a-z0-9]$")
string BucketName
@length(min: 1, max: 255)
string BucketAliasName
@length(max: 1024)
string ObjectPrefix
@length(max: 65536)
string LifecycleJSON
@pattern("^[0-9]+$")
string DecimalUint64
@length(min: 1, max: 255)
string AccessKeyId
@length(min: 1, max: 255)
@sensitive
string SecretAccessKey
@length(min: 1, max: 255)
string CredentialFingerprint
@length(min: 1, max: 512)
string SPIFFESubject
@length(min: 1, max: 64)
string CredentialStatus
@length(min: 1, max: 64)
string AuthMode
@length(max: 128)
string ServiceTag
list Buckets {
    member: BucketView
}
list BucketAliases {
    member: BucketAliasView
}
list ObjectStorageCredentials {
    member: ObjectStorageCredentialView
}
@permission(name: "object-storage:bucket:read")
string BucketReadPermission
@permission(name: "object-storage:bucket:write")
string BucketWritePermission
@permission(name: "object-storage:access-key:read")
string AccessKeyReadPermission
@permission(name: "object-storage:access-key:write")
string AccessKeyWritePermission
@auditEvent(name: "object_storage.bucket.list")
string BucketListAuditEvent
@auditEvent(name: "object_storage.bucket.create")
string BucketCreateAuditEvent
@auditEvent(name: "object_storage.bucket.read")
string BucketReadAuditEvent
@auditEvent(name: "object_storage.bucket.update")
string BucketUpdateAuditEvent
@auditEvent(name: "object_storage.bucket.delete")
string BucketDeleteAuditEvent
@auditEvent(name: "object_storage.bucket_alias.create")
string BucketAliasCreateAuditEvent
@auditEvent(name: "object_storage.bucket_alias.list")
string BucketAliasListAuditEvent
@auditEvent(name: "object_storage.bucket_alias.delete")
string BucketAliasDeleteAuditEvent
@auditEvent(name: "object_storage.credential.list")
string CredentialListAuditEvent
@auditEvent(name: "object_storage.access_key.create")
string AccessKeyCreateAuditEvent
@auditEvent(name: "object_storage.access_key.roll")
string AccessKeyRollAuditEvent
@auditEvent(name: "object_storage.access_key.delete")
string AccessKeyDeleteAuditEvent
@auditEvent(name: "object_storage.mtls_principal.create")
string MTLSPrincipalCreateAuditEvent
@auditEvent(name: "object_storage.mtls_principal.delete")
string MTLSPrincipalDeleteAuditEvent
resource Bucket {}
resource BucketAlias {}
resource ObjectStorageCredential {}
resource ObjectStorageAccessKey {}
resource ObjectStorageMTLSPrincipal {}
structure BucketView {
    @required
    @protoField(number: 1)
    bucket_id: BucketId
    @required
    @protoField(number: 2)
    resourceName: ResourceName
    @required
    @protoField(number: 3)
    org_id: OrgId
    @required
    @protoField(number: 4)
    bucket_name: BucketName
    @required
    @protoField(number: 5)
    quota_bytes: DecimalUint64
    @required
    @protoField(number: 6)
    quota_objects: DecimalUint64
    @protoField(number: 7)
    lifecycle: LifecycleJSON
    @required
    @protoField(number: 8)
    created_at: Timestamp
    @required
    @protoField(number: 9)
    updated_at: Timestamp
}
structure BucketAliasView {
    @required
    @protoField(number: 1)
    bucket_id: BucketId
    @required
    @protoField(number: 2)
    alias: BucketAliasName
    @protoField(number: 3)
    prefix: ObjectPrefix
    @protoField(number: 4)
    service_tag: ServiceTag
    @required
    @protoField(number: 5)
    created_at: Timestamp
}
structure ObjectStorageCredentialView {
    @required
    @protoField(number: 1)
    credential_id: CredentialId
    @required
    @protoField(number: 2)
    bucket_id: BucketId
    @required
    @protoField(number: 3)
    auth_mode: AuthMode
    @required
    @protoField(number: 4)
    display_name: DisplayName
    @protoField(number: 5)
    access_key_id: AccessKeyId
    @protoField(number: 6)
    spiffe_subject: SPIFFESubject
    @protoField(number: 7)
    credential_fingerprint: CredentialFingerprint
    @required
    @protoField(number: 8)
    status: CredentialStatus
    @protoField(number: 9)
    expires_at: Timestamp
    @required
    @protoField(number: 10)
    created_at: Timestamp
    @protoField(number: 11)
    revoked_at: Timestamp
}
structure ObjectStorageAccessKeySecret {
    @required
    @protoField(number: 1)
    access_key_id: AccessKeyId
    @required
    @protoField(number: 2)
    secret_access_key: SecretAccessKey
    @required
    @protoField(number: 3)
    fingerprint: CredentialFingerprint
    @required
    @protoField(number: 4)
    credential: ObjectStorageCredentialView
}
structure EmptyInput {}
structure BucketPathInput {
    @required
    @httpLabel
    bucket_id: BucketId
}
structure BucketIdempotentInput {
    @required
    @httpLabel
    bucket_id: BucketId
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
}
structure BucketOutput {
    @required
    bucket: BucketView
}
structure EmptyOutput {}
@readonly
@http(method: "GET", uri: "/api/v1/buckets")
@identity(mode: "bearer", audience: "object-storage-service", principals: ["browser", "cli", "workload"])
@authz(permission: BucketReadPermission, organization: {source: "token_org_id"})
@audit(event: BucketListAuditEvent, resource: Bucket, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "objectStorage.buckets", method: "list", paginated: false, retryable: true)
operation ListObjectStorageBuckets {
    input: EmptyInput
    output: ListObjectStorageBucketsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}
structure ListObjectStorageBucketsOutput {
    @required
    buckets: Buckets
}
@idempotent
@http(method: "POST", uri: "/api/v1/buckets")
@identity(mode: "bearer", audience: "object-storage-service", principals: ["browser", "cli", "workload"])
@authz(permission: BucketWritePermission, organization: {source: "token_org_id"})
@audit(event: BucketCreateAuditEvent, resource: Bucket, action: "create")
@rateLimit(bucket: "object_storage_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "objectStorage.buckets", method: "create", paginated: false, retryable: false)
operation CreateObjectStorageBucket {
    input: CreateObjectStorageBucketInput
    output: BucketOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
structure CreateObjectStorageBucketInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
    @required
    bucket_name: BucketName
    @required
    quota_bytes: DecimalUint64
    @required
    quota_objects: DecimalUint64
    lifecycle: LifecycleJSON
}
@readonly
@http(method: "GET", uri: "/api/v1/buckets/{bucket_id}")
@identity(mode: "bearer", audience: "object-storage-service", principals: ["browser", "cli", "workload"])
@authz(permission: BucketReadPermission, organization: {source: "token_org_id"})
@audit(event: BucketReadAuditEvent, resource: Bucket, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "objectStorage.buckets", method: "get", paginated: false, retryable: true)
operation GetObjectStorageBucket {
    input: BucketPathInput
    output: BucketOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}
@idempotent
@http(method: "PUT", uri: "/api/v1/buckets/{bucket_id}")
@identity(mode: "bearer", audience: "object-storage-service", principals: ["browser", "cli", "workload"])
@authz(permission: BucketWritePermission, organization: {source: "token_org_id"})
@audit(event: BucketUpdateAuditEvent, resource: Bucket, action: "update")
@rateLimit(bucket: "object_storage_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "objectStorage.buckets", method: "update", paginated: false, retryable: false)
operation UpdateObjectStorageBucket {
    input: UpdateObjectStorageBucketInput
    output: BucketOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
structure UpdateObjectStorageBucketInput {
    @required
    @httpLabel
    bucket_id: BucketId
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
    @required
    quota_bytes: DecimalUint64
    @required
    quota_objects: DecimalUint64
    lifecycle: LifecycleJSON
}
@idempotent
@http(method: "DELETE", uri: "/api/v1/buckets/{bucket_id}")
@identity(mode: "bearer", audience: "object-storage-service", principals: ["browser", "cli", "workload"])
@authz(permission: BucketWritePermission, organization: {source: "token_org_id"})
@audit(event: BucketDeleteAuditEvent, resource: Bucket, action: "delete")
@rateLimit(bucket: "object_storage_mutation")
@requestBudget(maxBytes: 1)
@sdk(module: "objectStorage.buckets", method: "delete", paginated: false, retryable: false)
operation DeleteObjectStorageBucket {
    input: BucketIdempotentInput
    output: EmptyOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
@idempotent
@http(method: "POST", uri: "/api/v1/buckets/{bucket_id}/aliases")
@identity(mode: "bearer", audience: "object-storage-service", principals: ["browser", "cli", "workload"])
@authz(permission: BucketWritePermission, organization: {source: "token_org_id"})
@audit(event: BucketAliasCreateAuditEvent, resource: BucketAlias, action: "create")
@rateLimit(bucket: "object_storage_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "objectStorage.aliases", method: "create", paginated: false, retryable: false)
operation CreateObjectStorageBucketAlias {
    input: CreateObjectStorageBucketAliasInput
    output: BucketAliasOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
structure CreateObjectStorageBucketAliasInput {
    @required
    @httpLabel
    bucket_id: BucketId
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
    @required
    alias: BucketAliasName
    prefix: ObjectPrefix
    service_tag: ServiceTag
}
structure BucketAliasOutput {
    @required
    alias: BucketAliasView
}
@readonly
@http(method: "GET", uri: "/api/v1/buckets/{bucket_id}/aliases")
@identity(mode: "bearer", audience: "object-storage-service", principals: ["browser", "cli", "workload"])
@authz(permission: BucketReadPermission, organization: {source: "token_org_id"})
@audit(event: BucketAliasListAuditEvent, resource: BucketAlias, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "objectStorage.aliases", method: "list", paginated: false, retryable: true)
operation ListObjectStorageBucketAliases {
    input: BucketPathInput
    output: ListObjectStorageBucketAliasesOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}
structure ListObjectStorageBucketAliasesOutput {
    @required
    aliases: BucketAliases
}
@idempotent
@http(method: "DELETE", uri: "/api/v1/buckets/{bucket_id}/aliases/{alias}")
@identity(mode: "bearer", audience: "object-storage-service", principals: ["browser", "cli", "workload"])
@authz(permission: BucketWritePermission, organization: {source: "token_org_id"})
@audit(event: BucketAliasDeleteAuditEvent, resource: BucketAlias, action: "delete")
@rateLimit(bucket: "object_storage_mutation")
@requestBudget(maxBytes: 1)
@sdk(module: "objectStorage.aliases", method: "delete", paginated: false, retryable: false)
operation DeleteObjectStorageBucketAlias {
    input: DeleteObjectStorageBucketAliasInput
    output: EmptyOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
structure DeleteObjectStorageBucketAliasInput {
    @required
    @httpLabel
    bucket_id: BucketId
    @required
    @httpLabel
    alias: BucketAliasName
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
}
@readonly
@http(method: "GET", uri: "/api/v1/buckets/{bucket_id}/credentials")
@identity(mode: "bearer", audience: "object-storage-service", principals: ["browser", "cli", "workload"])
@authz(permission: AccessKeyReadPermission, organization: {source: "token_org_id"})
@audit(event: CredentialListAuditEvent, resource: ObjectStorageCredential, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "objectStorage.credentials", method: "list", paginated: false, retryable: true)
operation ListObjectStorageCredentials {
    input: BucketPathInput
    output: ListObjectStorageCredentialsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}
structure ListObjectStorageCredentialsOutput {
    @required
    credentials: ObjectStorageCredentials
}
@idempotent
@http(method: "POST", uri: "/api/v1/buckets/{bucket_id}/access-keys")
@identity(mode: "bearer", audience: "object-storage-service", principals: ["browser", "cli", "workload"])
@authz(permission: AccessKeyWritePermission, organization: {source: "token_org_id"})
@audit(event: AccessKeyCreateAuditEvent, resource: ObjectStorageAccessKey, action: "create")
@rateLimit(bucket: "object_storage_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "objectStorage.accessKeys", method: "create", paginated: false, retryable: false)
operation CreateObjectStorageAccessKey {
    input: CreateObjectStorageAccessKeyInput
    output: ObjectStorageAccessKeySecretOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
structure CreateObjectStorageAccessKeyInput {
    @required
    @httpLabel
    bucket_id: BucketId
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
    @required
    display_name: DisplayName
    expires_at: Timestamp
}
structure ObjectStorageAccessKeySecretOutput {
    @required
    access_key: ObjectStorageAccessKeySecret
}
@idempotent
@http(method: "POST", uri: "/api/v1/access-keys/{access_key_id}/roll")
@identity(mode: "bearer", audience: "object-storage-service", principals: ["browser", "cli", "workload"])
@authz(permission: AccessKeyWritePermission, organization: {source: "token_org_id"})
@audit(event: AccessKeyRollAuditEvent, resource: ObjectStorageAccessKey, action: "roll")
@rateLimit(bucket: "object_storage_mutation")
@requestBudget(maxBytes: 1)
@sdk(module: "objectStorage.accessKeys", method: "roll", paginated: false, retryable: false)
operation RollObjectStorageAccessKey {
    input: AccessKeyIdempotentInput
    output: ObjectStorageAccessKeySecretOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
@idempotent
@http(method: "DELETE", uri: "/api/v1/access-keys/{access_key_id}")
@identity(mode: "bearer", audience: "object-storage-service", principals: ["browser", "cli", "workload"])
@authz(permission: AccessKeyWritePermission, organization: {source: "token_org_id"})
@audit(event: AccessKeyDeleteAuditEvent, resource: ObjectStorageAccessKey, action: "delete")
@rateLimit(bucket: "object_storage_mutation")
@requestBudget(maxBytes: 1)
@sdk(module: "objectStorage.accessKeys", method: "delete", paginated: false, retryable: false)
operation DeleteObjectStorageAccessKey {
    input: AccessKeyIdempotentInput
    output: EmptyOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
structure AccessKeyIdempotentInput {
    @required
    @httpLabel
    access_key_id: AccessKeyId
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
}
@idempotent
@http(method: "POST", uri: "/api/v1/buckets/{bucket_id}/mtls-principals")
@identity(mode: "bearer", audience: "object-storage-service", principals: ["browser", "cli", "workload"])
@authz(permission: AccessKeyWritePermission, organization: {source: "token_org_id"})
@audit(event: MTLSPrincipalCreateAuditEvent, resource: ObjectStorageMTLSPrincipal, action: "create")
@rateLimit(bucket: "object_storage_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "objectStorage.mtlsPrincipals", method: "create", paginated: false, retryable: false)
operation CreateObjectStorageMTLSPrincipal {
    input: CreateObjectStorageMTLSPrincipalInput
    output: BucketOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
structure CreateObjectStorageMTLSPrincipalInput {
    @required
    @httpLabel
    bucket_id: BucketId
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
    @required
    display_name: DisplayName
    @required
    spiffe_subject: SPIFFESubject
}
@idempotent
@http(method: "DELETE", uri: "/api/v1/buckets/{bucket_id}/mtls-principals/{credential_id}")
@identity(mode: "bearer", audience: "object-storage-service", principals: ["browser", "cli", "workload"])
@authz(permission: AccessKeyWritePermission, organization: {source: "token_org_id"})
@audit(event: MTLSPrincipalDeleteAuditEvent, resource: ObjectStorageMTLSPrincipal, action: "delete")
@rateLimit(bucket: "object_storage_mutation")
@requestBudget(maxBytes: 1)
@sdk(module: "objectStorage.mtlsPrincipals", method: "delete", paginated: false, retryable: false)
operation DeleteObjectStorageMTLSPrincipal {
    input: DeleteObjectStorageMTLSPrincipalInput
    output: EmptyOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}
structure DeleteObjectStorageMTLSPrincipalInput {
    @required
    @httpLabel
    bucket_id: BucketId
    @required
    @httpLabel
    credential_id: CredentialId
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
}
