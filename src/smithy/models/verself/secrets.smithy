$version: "2"
namespace verself.secrets.v1

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
use verself.common.v1#DateTime

@serviceRuntime(serviceName: "secrets-service", publicAudience: "verself-api", internalAudience: "secrets-service")
@restJson1
service Secrets {
    version: "2026-05-13"
    operations: [
        PutSecret,
        ReadSecret,
        ListSecrets,
        ResolveSecrets,
        DeleteSecret,
        PutVariable,
        ReadVariable,
        ListVariables,
        DeleteVariable,
        CreateOpaqueCredential,
        GetOpaqueCredential,
        ListOpaqueCredentials,
        RollOpaqueCredential,
        RevokeOpaqueCredential,
        CreateTransitKey,
        RotateTransitKey,
        EncryptWithTransitKey,
        DecryptWithTransitKey,
        SignWithTransitKey,
        VerifyWithTransitKey
    ]
    resources: [
        Secret,
        Variable,
        OpaqueCredential,
        TransitKey
    ]
}

@serviceRuntime(serviceName: "secrets-service", publicAudience: "verself-api", internalAudience: "secrets-service")
@restJson1
service SecretsInternal {
    version: "2026-05-13"
    operations: [
        ResolveInjection,
        CreateInternalOpaqueCredential,
        VerifyInternalOpaqueCredential
    ]
    resources: [
        Secret,
        OpaqueCredential
    ]
}

@length(min: 1, max: 255)
string SecretName

@pattern("^[0-9a-fA-F-]{36}$")
string CredentialId

@length(min: 1, max: 255)
string KeyName

@length(min: 1, max: 128)
@pattern("^org_[0-9A-HJKMNP-TV-Z]{26}$")
string OrgId

@length(min: 1, max: 128)
string SubjectId

@length(min: 1, max: 128)
string ScopeLevel

@length(max: 255)
string SourceId

@length(max: 255)
string EnvId

@length(max: 255)
string Branch

@pattern("^[0-9]+$")
string SecretVersion

@length(min: 1, max: 65536)
@sensitive
string SecretValue

@length(min: 1, max: 128)
string CredentialKind

@length(min: 1, max: 128)
string CredentialStatus

@length(min: 1, max: 128)
string CredentialScope

@length(max: 128)
string CredentialDisplayName

@length(max: 255)
string CredentialSubject

@length(min: 1, max: 64)
list CredentialScopes {
    member: CredentialScope
}

map CredentialMetadata {
    key: String
    value: String
}

@range(min: 60, max: 7776000)
long CredentialExpiresInSeconds

@length(min: 1, max: 128)
string TransitAlgorithm

@length(min: 1, max: 262144)
@sensitive
string PlaintextBase64

@length(min: 1, max: 262144)
@sensitive
string MessageBase64

@length(min: 1, max: 262144)
@sensitive
string Ciphertext

@length(min: 1, max: 262144)
@sensitive
string Signature

@range(min: 1, max: 200)
integer SecretListLimit

@length(min: 1, max: 255)
string InjectionEnvName

@length(min: 1, max: 255)
string InjectionExecutionId

@length(min: 1, max: 255)
string InjectionAttemptId

@length(min: 1, max: 255)
string InjectionGrantId

@length(min: 1, max: 200)
list SecretNames {
    member: SecretName
}

list SecretDTOs {
    member: SecretDTO
}

list SecretValueDTOs {
    member: SecretValueDTO
}

list VariableDTOs {
    member: VariableDTO
}

list OpaqueCredentialDTOs {
    member: OpaqueCredentialDTO
}

list InjectionSecretRequests {
    member: InjectionSecretRequest
}

list InjectionEnvValues {
    member: InjectionEnvValue
}

@permission(name: "secrets:secret:write")
string SecretWritePermission

@permission(name: "secrets:secret:read")
string SecretReadPermission

@permission(name: "secrets:secret:list")
string SecretListPermission

@permission(name: "secrets:secret:delete")
string SecretDeletePermission

@permission(name: "secrets:variable:write")
string VariableWritePermission

@permission(name: "secrets:variable:read")
string VariableReadPermission

@permission(name: "secrets:variable:list")
string VariableListPermission

@permission(name: "secrets:variable:delete")
string VariableDeletePermission

@permission(name: "secrets:credential:create")
string CredentialCreatePermission

@permission(name: "secrets:credential:read")
string CredentialReadPermission

@permission(name: "secrets:credential:list")
string CredentialListPermission

@permission(name: "secrets:credential:roll")
string CredentialRollPermission

@permission(name: "secrets:credential:revoke")
string CredentialRevokePermission

@permission(name: "secrets:transit_key:create")
string TransitKeyCreatePermission

@permission(name: "secrets:transit_key:rotate")
string TransitKeyRotatePermission

@permission(name: "secrets:transit:encrypt")
string TransitEncryptPermission

@permission(name: "secrets:transit:decrypt")
string TransitDecryptPermission

@permission(name: "secrets:transit:sign")
string TransitSignPermission

@permission(name: "secrets:transit:verify")
string TransitVerifyPermission

@auditEvent(name: "secrets.secret.write")
string SecretWriteAuditEvent

@auditEvent(name: "secrets.secret.read")
string SecretReadAuditEvent

@auditEvent(name: "secrets.secret.list")
string SecretListAuditEvent

@auditEvent(name: "secrets.secret.resolve")
string SecretResolveAuditEvent

@auditEvent(name: "secrets.secret.delete")
string SecretDeleteAuditEvent

@auditEvent(name: "secrets.variable.write")
string VariableWriteAuditEvent

@auditEvent(name: "secrets.variable.read")
string VariableReadAuditEvent

@auditEvent(name: "secrets.variable.list")
string VariableListAuditEvent

@auditEvent(name: "secrets.variable.delete")
string VariableDeleteAuditEvent

@auditEvent(name: "secrets.credential.create")
string CredentialCreateAuditEvent

@auditEvent(name: "secrets.credential.read")
string CredentialReadAuditEvent

@auditEvent(name: "secrets.credential.list")
string CredentialListAuditEvent

@auditEvent(name: "secrets.credential.roll")
string CredentialRollAuditEvent

@auditEvent(name: "secrets.credential.revoke")
string CredentialRevokeAuditEvent

@auditEvent(name: "secrets.transit_key.create")
string TransitKeyCreateAuditEvent

@auditEvent(name: "secrets.transit_key.rotate")
string TransitKeyRotateAuditEvent

@auditEvent(name: "secrets.transit_key.encrypt")
string TransitEncryptAuditEvent

@auditEvent(name: "secrets.transit_key.decrypt")
string TransitDecryptAuditEvent

@auditEvent(name: "secrets.transit_key.sign")
string TransitSignAuditEvent

@auditEvent(name: "secrets.transit_key.verify")
string TransitVerifyAuditEvent

@auditEvent(name: "secrets.injection.resolve")
string ResolveInjectionAuditEvent

resource Secret {}
resource Variable {}
resource OpaqueCredential {}
resource TransitKey {}

structure EmptyInput {}
structure EmptyOutput {}

structure SecretDTO {
    @required
    @protoField(number: 1)
    secret_id: String

    @protoField(number: 2)
    resourceName: ResourceName

    @required
    @protoField(number: 3)
    kind: String

    @required
    @protoField(number: 4)
    name: SecretName

    @required
    @protoField(number: 5)
    scope_level: ScopeLevel

    @protoField(number: 6)
    source_id: SourceId

    @protoField(number: 7)
    env_id: EnvId

    @protoField(number: 8)
    branch: Branch

    @required
    @protoField(number: 9)
    current_version: SecretVersion

    @required
    @protoField(number: 10)
    created_at: DateTime

    @required
    @protoField(number: 11)
    updated_at: DateTime
}

structure SecretValueDTO {
    @required
    secret_id: String
    resourceName: ResourceName
    @required
    kind: String
    @required
    name: SecretName
    @required
    scope_level: ScopeLevel
    source_id: SourceId
    env_id: EnvId
    branch: Branch
    @required
    current_version: SecretVersion
    @required
    created_at: DateTime
    @required
    updated_at: DateTime
    @required
    value: SecretValue
}

structure VariableDTO {
    @required
    variable_id: String
    resourceName: ResourceName
    @required
    kind: String
    @required
    name: SecretName
    @required
    scope_level: ScopeLevel
    source_id: SourceId
    env_id: EnvId
    branch: Branch
    @required
    current_version: SecretVersion
    @required
    created_at: DateTime
    @required
    updated_at: DateTime
}

structure VariableValueDTO {
    @required
    variable_id: String
    resourceName: ResourceName
    @required
    kind: String
    @required
    name: SecretName
    @required
    scope_level: ScopeLevel
    source_id: SourceId
    env_id: EnvId
    branch: Branch
    @required
    current_version: SecretVersion
    @required
    created_at: DateTime
    @required
    updated_at: DateTime
    @required
    value: SecretValue
}

structure SecretsDTO {
    @required
    secrets: SecretDTOs
}

structure ResolvedSecretsDTO {
    @required
    values: SecretValueDTOs
}

structure VariablesDTO {
    @required
    variables: VariableDTOs
}

structure OpaqueCredentialDTO {
    @required
    credential_id: CredentialId
    resourceName: ResourceName
    @required
    org_id: OrgId
    @required
    kind: CredentialKind
    @required
    subject: String
    @required
    display_name: String
    @required
    status: CredentialStatus
    @required
    token_prefix: String
    @required
    scopes: CredentialScopes
    @required
    metadata: CredentialMetadata
    @required
    current_version: SecretVersion
    @required
    expires_at: DateTime
    last_used_at: DateTime
    @required
    created_at: DateTime
    @required
    updated_at: DateTime
    revoked_at: DateTime
}

structure OpaqueCredentialMaterialDTO {
    @required
    credential: OpaqueCredentialDTO

    @required
    token: SecretValue
}

structure OpaqueCredentialsDTO {
    @required
    credentials: OpaqueCredentialDTOs
}

structure TransitKeyDTO {
    @required
    key_id: String
    resourceName: ResourceName
    @required
    name: KeyName
    @required
    current_version: SecretVersion
    @required
    public_key: String
    @required
    created_at: DateTime
    @required
    updated_at: DateTime
}

structure SecretPathInput {
    @required
    @httpLabel
    name: SecretName

    @httpQuery("scope_level")
    scope_level: ScopeLevel

    @httpQuery("source_id")
    source_id: SourceId

    @httpQuery("env_id")
    env_id: EnvId

    @httpQuery("branch")
    branch: Branch
}

structure SecretMutationInput {
    @required
    @httpLabel
    name: SecretName

    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    scope_level: ScopeLevel
    source_id: SourceId
    env_id: EnvId
    branch: Branch

    @required
    value: SecretValue
}

structure SecretDeleteInput {
    @required
    @httpLabel
    name: SecretName

    @httpQuery("scope_level")
    scope_level: ScopeLevel

    @httpQuery("source_id")
    source_id: SourceId

    @httpQuery("env_id")
    env_id: EnvId

    @httpQuery("branch")
    branch: Branch

    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
}

structure SecretListInput {
    @httpQuery("limit")
    limit: SecretListLimit
}

structure ResolveSecretsInput {
    scope_level: ScopeLevel
    source_id: SourceId
    env_id: EnvId
    branch: Branch
    names: SecretNames
    limit: SecretListLimit
}

structure SecretOutput {
    @required
    @httpPayload
    @nestedProperties
    secret: SecretDTO
}

structure SecretValueOutput {
    @required
    @httpPayload
    @nestedProperties
    secret: SecretValueDTO
}

structure SecretsOutput {
    @required
    @httpPayload
    @nestedProperties
    secrets: SecretsDTO
}

structure ResolvedSecretsOutput {
    @required
    @httpPayload
    @nestedProperties
    resolved: ResolvedSecretsDTO
}

structure VariableOutput {
    @required
    @httpPayload
    @nestedProperties
    variable: VariableDTO
}

structure VariableValueOutput {
    @required
    @httpPayload
    @nestedProperties
    variable: VariableValueDTO
}

structure VariablesOutput {
    @required
    @httpPayload
    @nestedProperties
    variables: VariablesDTO
}

structure OpaqueCredentialOutput {
    @required
    @httpPayload
    @nestedProperties
    credential: OpaqueCredentialDTO
}

structure OpaqueCredentialMaterialOutput {
    @required
    @httpPayload
    @nestedProperties
    material: OpaqueCredentialMaterialDTO
}

structure OpaqueCredentialsOutput {
    @required
    @httpPayload
    @nestedProperties
    credentials: OpaqueCredentialsDTO
}

structure TransitKeyOutput {
    @required
    @httpPayload
    @nestedProperties
    key: TransitKeyDTO
}

@idempotent
@http(method: "PUT", uri: "/api/v1/secrets/{name}")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: SecretWritePermission, organization: {source: "token_org_id"})
@audit(event: SecretWriteAuditEvent, resource: Secret, action: "write")
@rateLimit(bucket: "secret_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "secrets", method: "put", paginated: false, retryable: false)
operation PutSecret {
    input: SecretMutationInput
    output: SecretOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

@readonly
@http(method: "GET", uri: "/api/v1/secrets/{name}")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: SecretReadPermission, organization: {source: "token_org_id"})
@audit(event: SecretReadAuditEvent, resource: Secret, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "secrets", method: "read", paginated: false, retryable: true)
operation ReadSecret {
    input: SecretPathInput
    output: SecretValueOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}

@readonly
@http(method: "GET", uri: "/api/v1/secrets")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: SecretListPermission, organization: {source: "token_org_id"})
@audit(event: SecretListAuditEvent, resource: Secret, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "secrets", method: "list", paginated: false, retryable: true)
operation ListSecrets {
    input: SecretListInput
    output: SecretsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

@http(method: "POST", uri: "/api/v1/secrets:resolve")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: SecretReadPermission, organization: {source: "token_org_id"})
@audit(event: SecretResolveAuditEvent, resource: Secret, action: "resolve")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 65536)
@sdk(module: "secrets", method: "resolve", paginated: false, retryable: true)
operation ResolveSecrets {
    input: ResolveSecretsInput
    output: ResolvedSecretsOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

@idempotent
@http(method: "DELETE", uri: "/api/v1/secrets/{name}")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: SecretDeletePermission, organization: {source: "token_org_id"})
@audit(event: SecretDeleteAuditEvent, resource: Secret, action: "delete")
@rateLimit(bucket: "secret_mutation")
@requestBudget(maxBytes: 1024)
@sdk(module: "secrets", method: "delete", paginated: false, retryable: false)
operation DeleteSecret {
    input: SecretDeleteInput
    output: SecretOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

@idempotent
@http(method: "PUT", uri: "/api/v1/variables/{name}")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: VariableWritePermission, organization: {source: "token_org_id"})
@audit(event: VariableWriteAuditEvent, resource: Variable, action: "write")
@rateLimit(bucket: "secret_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "variables", method: "put", paginated: false, retryable: false)
operation PutVariable {
    input: SecretMutationInput
    output: VariableOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

@readonly
@http(method: "GET", uri: "/api/v1/variables/{name}")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: VariableReadPermission, organization: {source: "token_org_id"})
@audit(event: VariableReadAuditEvent, resource: Variable, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "variables", method: "read", paginated: false, retryable: true)
operation ReadVariable {
    input: SecretPathInput
    output: VariableValueOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}

@readonly
@http(method: "GET", uri: "/api/v1/variables")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: VariableListPermission, organization: {source: "token_org_id"})
@audit(event: VariableListAuditEvent, resource: Variable, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "variables", method: "list", paginated: false, retryable: true)
operation ListVariables {
    input: SecretListInput
    output: VariablesOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

@idempotent
@http(method: "DELETE", uri: "/api/v1/variables/{name}")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: VariableDeletePermission, organization: {source: "token_org_id"})
@audit(event: VariableDeleteAuditEvent, resource: Variable, action: "delete")
@rateLimit(bucket: "secret_mutation")
@requestBudget(maxBytes: 1024)
@sdk(module: "variables", method: "delete", paginated: false, retryable: false)
operation DeleteVariable {
    input: SecretDeleteInput
    output: VariableOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure CreateOpaqueCredentialInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    kind: CredentialKind

    display_name: CredentialDisplayName
    subject: CredentialSubject

    @required
    scopes: CredentialScopes

    metadata: CredentialMetadata
    expires_in_seconds: CredentialExpiresInSeconds
}

@idempotent
@http(method: "POST", uri: "/api/v1/credentials", code: 201)
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: CredentialCreatePermission, organization: {source: "token_org_id"})
@audit(event: CredentialCreateAuditEvent, resource: OpaqueCredential, action: "create")
@rateLimit(bucket: "credential_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "secrets.credentials", method: "create", paginated: false, retryable: false)
operation CreateOpaqueCredential {
    input: CreateOpaqueCredentialInput
    output: OpaqueCredentialMaterialOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure OpaqueCredentialPathInput {
    @required
    @httpLabel
    credential_id: CredentialId
}

@readonly
@http(method: "GET", uri: "/api/v1/credentials/{credential_id}")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: CredentialReadPermission, organization: {source: "token_org_id"})
@audit(event: CredentialReadAuditEvent, resource: OpaqueCredential, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "secrets.credentials", method: "get", paginated: false, retryable: true)
operation GetOpaqueCredential {
    input: OpaqueCredentialPathInput
    output: OpaqueCredentialOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}

structure ListOpaqueCredentialsInput {
    @httpQuery("kind")
    kind: CredentialKind

    @httpQuery("limit")
    limit: SecretListLimit
}

@readonly
@http(method: "GET", uri: "/api/v1/credentials")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: CredentialListPermission, organization: {source: "token_org_id"})
@audit(event: CredentialListAuditEvent, resource: OpaqueCredential, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "secrets.credentials", method: "list", paginated: false, retryable: true)
operation ListOpaqueCredentials {
    input: ListOpaqueCredentialsInput
    output: OpaqueCredentialsOutput
    errors: [UnauthenticatedError, PermissionDeniedError, RateLimitedError, ServiceUnavailableError]
}

structure RollOpaqueCredentialInput {
    @required
    @httpLabel
    credential_id: CredentialId

    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    expires_in_seconds: CredentialExpiresInSeconds
}

@idempotent
@http(method: "POST", uri: "/api/v1/credentials/{credential_id}/roll")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: CredentialRollPermission, organization: {source: "token_org_id"})
@audit(event: CredentialRollAuditEvent, resource: OpaqueCredential, action: "roll")
@rateLimit(bucket: "credential_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "secrets.credentials", method: "roll", paginated: false, retryable: false)
operation RollOpaqueCredential {
    input: RollOpaqueCredentialInput
    output: OpaqueCredentialMaterialOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure RevokeOpaqueCredentialInput {
    @required
    @httpLabel
    credential_id: CredentialId

    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
}

@idempotent
@http(method: "POST", uri: "/api/v1/credentials/{credential_id}/revoke")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: CredentialRevokePermission, organization: {source: "token_org_id"})
@audit(event: CredentialRevokeAuditEvent, resource: OpaqueCredential, action: "revoke")
@rateLimit(bucket: "credential_mutation")
@requestBudget(maxBytes: 1024)
@sdk(module: "secrets.credentials", method: "revoke", paginated: false, retryable: false)
operation RevokeOpaqueCredential {
    input: RevokeOpaqueCredentialInput
    output: OpaqueCredentialOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure CreateTransitKeyInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    name: KeyName
}

@idempotent
@http(method: "POST", uri: "/api/v1/transit/keys", code: 201)
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: TransitKeyCreatePermission, organization: {source: "token_org_id"})
@audit(event: TransitKeyCreateAuditEvent, resource: TransitKey, action: "create")
@rateLimit(bucket: "key_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "secrets.transitKeys", method: "create", paginated: false, retryable: false)
operation CreateTransitKey {
    input: CreateTransitKeyInput
    output: TransitKeyOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure TransitKeyIdempotentInput {
    @required
    @httpLabel
    key_name: KeyName

    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
}

@idempotent
@http(method: "POST", uri: "/api/v1/transit/keys/{key_name}/rotate")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: TransitKeyRotatePermission, organization: {source: "token_org_id"})
@audit(event: TransitKeyRotateAuditEvent, resource: TransitKey, action: "rotate")
@rateLimit(bucket: "key_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "secrets.transitKeys", method: "rotate", paginated: false, retryable: false)
operation RotateTransitKey {
    input: TransitKeyIdempotentInput
    output: TransitKeyOutput
    errors: [UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError]
}

structure TransitEncryptInput {
    @required
    @httpLabel
    key_name: KeyName

    @required
    plaintext_base64: PlaintextBase64
}

structure TransitCiphertextOutput {
    @required
    ciphertext: Ciphertext

    @required
    version: SecretVersion
}

@http(method: "POST", uri: "/api/v1/transit/keys/{key_name}/encrypt")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: TransitEncryptPermission, organization: {source: "token_org_id"})
@audit(event: TransitEncryptAuditEvent, resource: TransitKey, action: "encrypt")
@rateLimit(bucket: "crypto")
@requestBudget(maxBytes: 262144)
@sdk(module: "secrets.transit", method: "encrypt", paginated: false, retryable: false)
operation EncryptWithTransitKey {
    input: TransitEncryptInput
    output: TransitCiphertextOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}

structure TransitDecryptInput {
    @required
    @httpLabel
    key_name: KeyName

    @required
    ciphertext: Ciphertext
}

structure TransitPlaintextOutput {
    @required
    plaintext_base64: PlaintextBase64
}

@http(method: "POST", uri: "/api/v1/transit/keys/{key_name}/decrypt")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: TransitDecryptPermission, organization: {source: "token_org_id"})
@audit(event: TransitDecryptAuditEvent, resource: TransitKey, action: "decrypt")
@rateLimit(bucket: "crypto")
@requestBudget(maxBytes: 262144)
@sdk(module: "secrets.transit", method: "decrypt", paginated: false, retryable: false)
operation DecryptWithTransitKey {
    input: TransitDecryptInput
    output: TransitPlaintextOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}

structure TransitSignInput {
    @required
    @httpLabel
    key_name: KeyName

    @required
    message_base64: MessageBase64
}

structure TransitSignatureOutput {
    @required
    signature: Signature
}

@http(method: "POST", uri: "/api/v1/transit/keys/{key_name}/sign")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: TransitSignPermission, organization: {source: "token_org_id"})
@audit(event: TransitSignAuditEvent, resource: TransitKey, action: "sign")
@rateLimit(bucket: "crypto")
@requestBudget(maxBytes: 262144)
@sdk(module: "secrets.transit", method: "sign", paginated: false, retryable: false)
operation SignWithTransitKey {
    input: TransitSignInput
    output: TransitSignatureOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError]
}

structure TransitVerifyInput {
    @required
    @httpLabel
    key_name: KeyName

    @required
    message_base64: MessageBase64

    @required
    signature: Signature
}

structure TransitVerifyOutput {
    @required
    valid: Boolean
}

@http(method: "POST", uri: "/api/v1/transit/keys/{key_name}/verify")
@identity(mode: "bearer", audience: "verself-api", principals: ["browser", "cli", "workload"])
@authz(permission: TransitVerifyPermission, organization: {source: "token_org_id"})
@audit(event: TransitVerifyAuditEvent, resource: TransitKey, action: "verify")
@rateLimit(bucket: "crypto")
@requestBudget(maxBytes: 262144)
@sdk(module: "secrets.transit", method: "verify", paginated: false, retryable: false)
operation VerifyWithTransitKey {
    input: TransitVerifyInput
    output: TransitVerifyOutput
    errors: [ValidationFailedError, UnauthenticatedError, PermissionDeniedError, ResourceNotFoundError, ServiceUnavailableError]
}

structure InjectionResolveInput {
    @required
    org_id: OrgId

    @required
    actor_id: SubjectId

    @required
    execution_id: InjectionExecutionId

    @required
    attempt_id: InjectionAttemptId

    @required
    secrets: InjectionSecretRequests
}

structure InjectionSecretRequest {
    @required
    env_name: InjectionEnvName

    kind: String

    @required
    secret_name: SecretName

    scope_level: ScopeLevel
    source_id: SourceId
    env_id: EnvId
    branch: Branch

    @required
    grant_id: InjectionGrantId
}

structure InjectionResolveOutput {
    @required
    env: InjectionEnvValues
}

structure InjectionEnvValue {
    @required
    name: InjectionEnvName

    @required
    value: SecretValue
}

@http(method: "POST", uri: "/internal/v1/injections/resolve")
@identity(mode: "spiffe_mtls", audience: "secrets-service", principals: ["workload"])
@authz(permission: SecretReadPermission, organization: {source: "body_org_id", member: "org_id"})
@audit(event: ResolveInjectionAuditEvent, resource: Secret, action: "resolve")
@rateLimit(bucket: "internal_read")
@requestBudget(maxBytes: 131072)
@sdk(module: "secretsInternal.injections", method: "resolve", paginated: false, retryable: false)
operation ResolveInjection {
    input: InjectionResolveInput
    output: InjectionResolveOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, ServiceUnavailableError]
}

structure InternalOpaqueCredentialInput {
    @required
    org_id: OrgId

    @required
    actor_id: SubjectId

    @required
    kind: CredentialKind

    display_name: CredentialDisplayName

    @required
    scopes: CredentialScopes

    metadata: CredentialMetadata

    expires_at: DateTime
}

structure InternalOpaqueCredentialOutput {
    @required
    credential: OpaqueCredentialDTO

    @required
    token: SecretValue
}

@http(method: "POST", uri: "/internal/v1/credentials", code: 201)
@identity(mode: "spiffe_mtls", audience: "secrets-service", principals: ["workload"])
@authz(permission: CredentialCreatePermission, organization: {source: "body_org_id", member: "org_id"})
@audit(event: CredentialCreateAuditEvent, resource: OpaqueCredential, action: "create")
@rateLimit(bucket: "internal_mutation")
@requestBudget(maxBytes: 131072)
@sdk(module: "secretsInternal.credentials", method: "create", paginated: false, retryable: false)
operation CreateInternalOpaqueCredential {
    input: InternalOpaqueCredentialInput
    output: InternalOpaqueCredentialOutput
    errors: [ValidationFailedError, PermissionDeniedError, ConflictError, ServiceUnavailableError]
}

structure VerifyInternalOpaqueCredentialInput {
    @required
    org_id: OrgId

    actor_id: SubjectId

    @required
    kind: CredentialKind

    @required
    token: SecretValue

    required_scopes: CredentialScopes
}

structure VerifyInternalOpaqueCredentialOutput {
    @required
    active: Boolean

    denial_reason: String
    credential: OpaqueCredentialDTO
}

@http(method: "POST", uri: "/internal/v1/credentials:verify")
@identity(mode: "spiffe_mtls", audience: "secrets-service", principals: ["workload"])
@authz(permission: CredentialReadPermission, organization: {source: "body_org_id", member: "org_id"})
@audit(event: CredentialReadAuditEvent, resource: OpaqueCredential, action: "verify")
@rateLimit(bucket: "internal_read")
@requestBudget(maxBytes: 131072)
@sdk(module: "secretsInternal.credentials", method: "verify", paginated: false, retryable: false)
operation VerifyInternalOpaqueCredential {
    input: VerifyInternalOpaqueCredentialInput
    output: VerifyInternalOpaqueCredentialOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, ServiceUnavailableError]
}
