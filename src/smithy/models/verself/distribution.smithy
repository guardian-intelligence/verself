$version: "2"
namespace verself.distribution.v1

use aws.protocols#restJson1
use smithy.api#error
use smithy.api#http
use smithy.api#httpError
use smithy.api#httpHeader
use smithy.api#httpLabel
use smithy.api#httpPayload
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
use verself.common.v1#DateTime
use verself.common.v1#IdempotencyKey
use verself.common.v1#IdempotencyPayloadMismatchError
use verself.common.v1#PageSize
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
use verself.common.v1#problem
use verself.common.v1#protoField
use verself.common.v1#rateLimit
use verself.common.v1#requestBudget
use verself.common.v1#sdk
use verself.common.v1#serviceRuntime

@serviceRuntime(serviceName: "distribution-service", publicAudience: "verself-api", internalAudience: "distribution-service")
@restJson1
service Distribution {
    version: "2026-05-26"
    operations: [
        CheckDistributionUpdate,
        RecordDistributionUpgradeVerification,
        ResolveDistributionTarget,
        GetDistributionArtifact,
        ListDistributionChannelTargets
    ]
    resources: [
        DistributionArtifact,
        DistributionChannel,
        DistributionTarget,
        DistributionVerification,
        DistributionReplication,
        DistributionQuarantine
    ]
}

@serviceRuntime(serviceName: "distribution-service", publicAudience: "verself-api", internalAudience: "distribution-service")
@restJson1
service DistributionInternal {
    version: "2026-05-26"
    operations: [
        AdmitDistributionArtifact,
        PromoteDistributionTarget,
        QuarantineDistributionArtifact,
        EnsureDistributionReplication,
        GetDistributionArtifactInternal
    ]
    resources: [
        DistributionArtifact,
        DistributionChannel,
        DistributionTarget,
        DistributionVerification,
        DistributionReplication,
        DistributionQuarantine
    ]
}

@length(min: 1, max: 128)
@pattern("^[a-z0-9][a-z0-9._-]*[a-z0-9]$")
string PackageName

@length(min: 1, max: 64)
@pattern("^[a-z][a-z0-9._-]*$")
string ChannelName

@length(min: 1, max: 128)
string PackageVersion

@length(min: 1, max: 128)
string PlatformOS

@length(min: 1, max: 128)
string PlatformArch

@length(min: 1, max: 128)
@pattern("^[a-z0-9][a-z0-9._-]*[a-z0-9]$")
string ArtifactFlavor

@length(min: 1, max: 4096)
string OCIRepository

@length(min: 1, max: 4096)
@pattern("^sha256:[a-f0-9]{64}$")
string OCIDigest

@length(min: 1, max: 80)
@pattern("^sha256-[a-f0-9]{64}$")
string ArtifactDigestRef

@length(min: 1, max: 4096)
string OCIMediaType

@range(min: 0, max: 9223372036854775807)
long OCISizeBytes

@length(min: 1, max: 4096)
string OCIReference

@length(min: 1, max: 4096)
string RegistryURL

@length(min: 1, max: 4096)
string DownloadURL

@length(min: 1, max: 4096)
string SourceRepository

@length(min: 1, max: 128)
string SourceCommit

@length(min: 1, max: 4096)
string SourceRef

@length(min: 1, max: 4096)
string BuilderID

@length(min: 1, max: 4096)
string SignerIdentity

@length(min: 1, max: 4096)
string PredicateType

@length(min: 1, max: 4096)
string DocumentDigest

@length(min: 1, max: 4096)
string PolicyRef

@length(min: 1, max: 4096)
string DecisionReason

@length(min: 1, max: 64)
@pattern("^[a-z][a-z0-9_]*$")
string ArtifactState

@length(min: 1, max: 64)
@pattern("^[a-z][a-z0-9_]*$")
string ChannelTargetState

@length(min: 1, max: 64)
@pattern("^[a-z][a-z0-9_]*$")
string VerificationDecision

@length(min: 1, max: 64)
@pattern("^[a-z][a-z0-9_]*$")
string RetentionClass

@length(min: 1, max: 64)
@pattern("^[a-z][a-z0-9_]*$")
string ReplicationState

@length(min: 1, max: 4096)
string ActorId

@range(min: 1, max: 9223372036854775807)
long ByteLength

@length(min: 1, max: 4096)
string EvidenceKind

list DistributionEvidence {
    member: DistributionEvidenceRecord
}

list DistributionTargetList {
    member: DistributionTargetRecord
}

@permission(name: "distribution:artifact:read")
string DistributionArtifactReadPermission

@permission(name: "distribution:artifact:write")
string DistributionArtifactWritePermission

@permission(name: "distribution:target:read")
string DistributionTargetReadPermission

@permission(name: "distribution:target:write")
string DistributionTargetWritePermission

@permission(name: "distribution:update:check")
string DistributionUpdateCheckPermission

@permission(name: "distribution:replication:write")
string DistributionReplicationWritePermission

@auditEvent(name: "distribution.updates.check")
string DistributionUpdateCheckAuditEvent

@auditEvent(name: "distribution.targets.resolve")
string DistributionTargetResolveAuditEvent

@auditEvent(name: "distribution.artifacts.get")
string DistributionArtifactGetAuditEvent

@auditEvent(name: "distribution.channels.list_targets")
string DistributionChannelListTargetsAuditEvent

@auditEvent(name: "distribution.upgrade.download_verified")
string DistributionUpgradeDownloadVerifiedAuditEvent

@auditEvent(name: "distribution.artifact.admit")
string DistributionArtifactAdmitAuditEvent

@auditEvent(name: "distribution.target.promote")
string DistributionTargetPromoteAuditEvent

@auditEvent(name: "distribution.artifact.quarantine")
string DistributionArtifactQuarantineAuditEvent

@auditEvent(name: "distribution.replication.ensure")
string DistributionReplicationEnsureAuditEvent

resource DistributionArtifact {}
resource DistributionChannel {}
resource DistributionTarget {}
resource DistributionVerification {}
resource DistributionReplication {}
resource DistributionQuarantine {}

structure DistributionEvidenceRecord {
    @required
    @protoField(number: 1)
    evidence_kind: EvidenceKind

    @required
    @protoField(number: 2)
    predicate_type: PredicateType

    @required
    @protoField(number: 3)
    subject_digest: OCIDigest

    @required
    @protoField(number: 4)
    document_digest: DocumentDigest

    @required
    @protoField(number: 5)
    oci_referrer_digest: OCIDigest
}

structure DistributionArtifactRecord {
    @required
    @protoField(number: 1)
    artifact_digest: OCIDigest

    @required
    @protoField(number: 2)
    artifact_digest_ref: ArtifactDigestRef

    @required
    @protoField(number: 3)
    resourceName: ResourceName

    @required
    @protoField(number: 4)
    package_name: PackageName

    @required
    @protoField(number: 5)
    package_version: PackageVersion

    @required
    @protoField(number: 6)
    platform_os: PlatformOS

    @required
    @protoField(number: 7)
    platform_arch: PlatformArch

    @required
    @protoField(number: 20)
    flavor: ArtifactFlavor

    @required
    @protoField(number: 8)
    oci_repository: OCIRepository

    @required
    @protoField(number: 9)
    public_oci_reference: OCIReference

    @required
    @protoField(number: 10)
    oci_media_type: OCIMediaType

    @required
    @protoField(number: 11)
    oci_size_bytes: OCISizeBytes

    @required
    @protoField(number: 12)
    state: ArtifactState

    @required
    @protoField(number: 13)
    retention_class: RetentionClass

    @required
    @protoField(number: 14)
    verification: DistributionVerificationRecord

    @required
    @protoField(number: 15)
    replication: DistributionReplicationRecord

    @required
    @protoField(number: 16)
    created_at: DateTime

    @required
    @protoField(number: 17)
    updated_at: DateTime

    @protoField(number: 18)
    quarantined_at: DateTime

    @protoField(number: 19)
    quarantine_reason: DecisionReason
}

structure DistributionVerificationRecord {
    @required
    @protoField(number: 1)
    decision: VerificationDecision

    @required
    @protoField(number: 2)
    reason: DecisionReason

    @required
    @protoField(number: 3)
    builder_id: BuilderID

    @required
    @protoField(number: 4)
    signer_identity: SignerIdentity

    @required
    @protoField(number: 5)
    source_repository: SourceRepository

    @required
    @protoField(number: 6)
    source_commit: SourceCommit

    @required
    @protoField(number: 7)
    source_ref: SourceRef

    @required
    @protoField(number: 9)
    evidence: DistributionEvidence

    @required
    @protoField(number: 10)
    checked_at: DateTime
}

structure DistributionReplicationRecord {
    @required
    @protoField(number: 1)
    state: ReplicationState

    @required
    @protoField(number: 2)
    origin_registry_url: RegistryURL

    @required
    @protoField(number: 3)
    public_registry_url: RegistryURL

    @required
    @protoField(number: 4)
    checked_at: DateTime
}

structure DistributionTargetRecord {
    @required
    @protoField(number: 1)
    resourceName: ResourceName

    @required
    @protoField(number: 2)
    package_name: PackageName

    @required
    @protoField(number: 3)
    channel_name: ChannelName

    @required
    @protoField(number: 4)
    platform_os: PlatformOS

    @required
    @protoField(number: 5)
    platform_arch: PlatformArch

    @required
    @protoField(number: 15)
    flavor: ArtifactFlavor

    @required
    @protoField(number: 6)
    artifact_digest: OCIDigest

    @required
    @protoField(number: 7)
    artifact_digest_ref: ArtifactDigestRef

    @required
    @protoField(number: 8)
    package_version: PackageVersion

    @required
    @protoField(number: 9)
    state: ChannelTargetState

    @required
    @protoField(number: 10)
    public_oci_reference: OCIReference

    @required
    @protoField(number: 11)
    download_url: DownloadURL

    @required
    @protoField(number: 12)
    published_at: DateTime

    @protoField(number: 13)
    superseded_at: DateTime

    @protoField(number: 14)
    superseded_by_digest: OCIDigest
}

structure CheckDistributionUpdateBody {
    @required
    @protoField(number: 1)
    package_name: PackageName

    @required
    @protoField(number: 2)
    channel_name: ChannelName

    @required
    @protoField(number: 3)
    platform_os: PlatformOS

    @required
    @protoField(number: 4)
    platform_arch: PlatformArch

    @required
    @protoField(number: 5)
    flavor: ArtifactFlavor

    @protoField(number: 6)
    installed_digest: OCIDigest

    @protoField(number: 7)
    installed_version: PackageVersion
}

structure CheckDistributionUpdateInput {
    @required
    @httpPayload
    body: CheckDistributionUpdateBody
}

structure CheckDistributionUpdateOutputBody {
    @required
    @protoField(number: 1)
    update_available: Boolean

    @required
    @protoField(number: 2)
    target: DistributionTargetRecord

    @required
    @protoField(number: 3)
    traceparent: TraceParent
}

structure CheckDistributionUpdateOutput {
    @required
    @httpPayload
    body: CheckDistributionUpdateOutputBody
}

structure RecordDistributionUpgradeVerificationBody {
    @required
    @protoField(number: 1)
    package_name: PackageName

    @required
    @protoField(number: 2)
    channel_name: ChannelName

    @required
    @protoField(number: 3)
    platform_os: PlatformOS

    @required
    @protoField(number: 4)
    platform_arch: PlatformArch

    @required
    @protoField(number: 5)
    flavor: ArtifactFlavor

    @required
    @protoField(number: 6)
    artifact_digest: OCIDigest

    @required
    @protoField(number: 7)
    layer_digest: OCIDigest

    @protoField(number: 8)
    installed_version: PackageVersion
}

structure RecordDistributionUpgradeVerificationInput {
    @required
    @httpPayload
    body: RecordDistributionUpgradeVerificationBody
}

structure RecordDistributionUpgradeVerificationOutputBody {
    @required
    @protoField(number: 1)
    traceparent: TraceParent
}

structure RecordDistributionUpgradeVerificationOutput {
    @required
    @httpPayload
    body: RecordDistributionUpgradeVerificationOutputBody
}

structure ResolveDistributionTargetInput {
    @required
    @httpLabel
    package_name: PackageName

    @required
    @httpLabel
    channel_name: ChannelName

    @required
    @httpQuery("platform_os")
    platform_os: PlatformOS

    @required
    @httpQuery("platform_arch")
    platform_arch: PlatformArch

    @required
    @httpQuery("flavor")
    flavor: ArtifactFlavor
}

structure ResolveDistributionTargetOutputBody {
    @required
    @protoField(number: 1)
    target: DistributionTargetRecord

    @required
    @protoField(number: 2)
    traceparent: TraceParent
}

structure ResolveDistributionTargetOutput {
    @required
    @httpPayload
    body: ResolveDistributionTargetOutputBody
}

structure DistributionArtifactPathInput {
    @required
    @httpLabel
    artifact_digest_ref: ArtifactDigestRef
}

structure DistributionArtifactOutputBody {
    @required
    @protoField(number: 1)
    artifact: DistributionArtifactRecord

    @required
    @protoField(number: 2)
    traceparent: TraceParent
}

structure DistributionArtifactOutput {
    @required
    @httpPayload
    body: DistributionArtifactOutputBody
}

structure ListDistributionChannelTargetsInput {
    @required
    @httpLabel
    package_name: PackageName

    @required
    @httpLabel
    channel_name: ChannelName

    @httpQuery("platform_os")
    platform_os: PlatformOS

    @httpQuery("platform_arch")
    platform_arch: PlatformArch

    @httpQuery("flavor")
    flavor: ArtifactFlavor

    @httpQuery("page_size")
    page_size: PageSize

    @httpQuery("page_token")
    page_token: PageToken
}

structure ListDistributionChannelTargetsOutputBody {
    @required
    @protoField(number: 1)
    targets: DistributionTargetList

    @protoField(number: 2)
    next_page_token: PageToken

    @required
    @protoField(number: 3)
    traceparent: TraceParent
}

structure ListDistributionChannelTargetsOutput {
    @required
    @protoField(number: 1)
    targets: DistributionTargetList

    @protoField(number: 2)
    next_page_token: PageToken

    @required
    @protoField(number: 3)
    traceparent: TraceParent
}

structure AdmitDistributionArtifactBody {
    @required
    @protoField(number: 1)
    package_name: PackageName

    @required
    @protoField(number: 2)
    package_version: PackageVersion

    @required
    @protoField(number: 3)
    channel_name: ChannelName

    @required
    @protoField(number: 4)
    platform_os: PlatformOS

    @required
    @protoField(number: 5)
    platform_arch: PlatformArch

    @required
    @protoField(number: 6)
    flavor: ArtifactFlavor

    @required
    @protoField(number: 7)
    origin_registry_url: RegistryURL

    @required
    @protoField(number: 8)
    public_registry_url: RegistryURL

    @required
    @protoField(number: 9)
    oci_repository: OCIRepository

    @required
    @protoField(number: 10)
    oci_digest: OCIDigest

    @required
    @protoField(number: 11)
    oci_media_type: OCIMediaType

    @required
    @protoField(number: 12)
    oci_size_bytes: OCISizeBytes

    @required
    @protoField(number: 13)
    builder_id: BuilderID

    @required
    @protoField(number: 14)
    signer_identity: SignerIdentity

    @required
    @protoField(number: 15)
    source_repository: SourceRepository

    @required
    @protoField(number: 16)
    source_commit: SourceCommit

    @required
    @protoField(number: 17)
    source_ref: SourceRef

    @required
    @protoField(number: 18)
    policy_ref: PolicyRef

    @required
    @protoField(number: 19)
    evidence: DistributionEvidence

    @required
    @protoField(number: 20)
    submitted_by: ActorId
}

structure AdmitDistributionArtifactInput {
    @required
    @idempotencyToken
    @httpHeader("Idempotency-Key")
    idempotency_key: IdempotencyKey

    @required
    @httpPayload
    body: AdmitDistributionArtifactBody
}

structure PromoteDistributionTargetBody {
    @required
    @protoField(number: 1)
    artifact_digest: OCIDigest

    @required
    @protoField(number: 2)
    platform_os: PlatformOS

    @required
    @protoField(number: 3)
    platform_arch: PlatformArch

    @required
    @protoField(number: 4)
    flavor: ArtifactFlavor

    @required
    @protoField(number: 5)
    policy_ref: PolicyRef

    @required
    @protoField(number: 6)
    promoted_by: ActorId

    @required
    @protoField(number: 7)
    reason: DecisionReason
}

structure PromoteDistributionTargetInput {
    @required
    @httpLabel
    package_name: PackageName

    @required
    @httpLabel
    channel_name: ChannelName

    @required
    @idempotencyToken
    @httpHeader("Idempotency-Key")
    idempotency_key: IdempotencyKey

    @required
    @httpPayload
    body: PromoteDistributionTargetBody
}

structure QuarantineDistributionArtifactBody {
    @required
    @protoField(number: 1)
    actor_id: ActorId

    @required
    @protoField(number: 2)
    reason: DecisionReason
}

structure QuarantineDistributionArtifactInput {
    @required
    @httpLabel
    artifact_digest_ref: ArtifactDigestRef

    @required
    @idempotencyToken
    @httpHeader("Idempotency-Key")
    idempotency_key: IdempotencyKey

    @required
    @httpPayload
    body: QuarantineDistributionArtifactBody
}

structure EnsureDistributionReplicationBody {
    @required
    @protoField(number: 1)
    actor_id: ActorId

    @required
    @protoField(number: 2)
    reason: DecisionReason
}

structure EnsureDistributionReplicationInput {
    @required
    @httpLabel
    artifact_digest_ref: ArtifactDigestRef

    @required
    @idempotencyToken
    @httpHeader("Idempotency-Key")
    idempotency_key: IdempotencyKey

    @required
    @httpPayload
    body: EnsureDistributionReplicationBody
}

@error("client")
@httpError(409)
@problem(type: "urn:verself:problem:distribution:missing_referrers", code: "distribution.missing_referrers")
structure MissingOCIReferrersError {}

@error("client")
@httpError(409)
@problem(type: "urn:verself:problem:distribution:digest_mismatch", code: "distribution.digest_mismatch")
structure DistributionDigestMismatchError {}

@error("client")
@httpError(403)
@problem(type: "urn:verself:problem:distribution:untrusted_builder", code: "distribution.untrusted_builder")
structure UntrustedBuilderError {}

@error("client")
@httpError(403)
@problem(type: "urn:verself:problem:distribution:untrusted_signer", code: "distribution.untrusted_signer")
structure UntrustedSignerError {}

@error("client")
@httpError(403)
@problem(type: "urn:verself:problem:distribution:source_policy_failure", code: "distribution.source_policy_failure")
structure SourcePolicyFailureError {}

@error("client")
@httpError(403)
@problem(type: "urn:verself:problem:distribution:channel_policy_failure", code: "distribution.channel_policy_failure")
structure ChannelPolicyFailureError {}

@error("server")
@httpError(503)
@problem(type: "urn:verself:problem:distribution:replication_failure", code: "distribution.replication_failure")
structure DistributionReplicationFailureError {}

@error("client")
@httpError(403)
@problem(type: "urn:verself:problem:distribution:artifact_quarantined", code: "distribution.artifact_quarantined")
structure QuarantinedArtifactError {}

@http(method: "POST", uri: "/api/v1/distribution/updates:check", code: 200)
@identity(mode: "public", audience: "verself-api", principals: ["browser", "cli", "workload", "anonymous"])
@authz(permission: DistributionUpdateCheckPermission, organization: {source: "installation"})
@audit(event: DistributionUpdateCheckAuditEvent, resource: DistributionTarget, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 16384)
@sdk(module: "distribution.updates", method: "check", paginated: false, retryable: true)
operation CheckDistributionUpdate {
    input: CheckDistributionUpdateInput
    output: CheckDistributionUpdateOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, QuarantinedArtifactError, RateLimitedError, ServiceUnavailableError, UnauthenticatedError]
}

@idempotent
@http(method: "POST", uri: "/api/v1/distribution/upgrades:recordVerifiedDownload", code: 202)
@identity(mode: "public", audience: "verself-api", principals: ["browser", "cli", "workload", "anonymous"])
@authz(permission: DistributionUpdateCheckPermission, organization: {source: "installation"})
@audit(event: DistributionUpgradeDownloadVerifiedAuditEvent, resource: DistributionTarget, action: "read")
@rateLimit(bucket: "write")
@requestBudget(maxBytes: 16384)
@sdk(module: "distribution.upgrades", method: "recordVerifiedDownload", paginated: false, retryable: true)
operation RecordDistributionUpgradeVerification {
    input: RecordDistributionUpgradeVerificationInput
    output: RecordDistributionUpgradeVerificationOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, QuarantinedArtifactError, RateLimitedError, ServiceUnavailableError, UnauthenticatedError]
}

@readonly
@http(method: "GET", uri: "/api/v1/distribution/packages/{package_name}/channels/{channel_name}/resolve", code: 200)
@identity(mode: "public", audience: "verself-api", principals: ["browser", "cli", "workload", "anonymous"])
@authz(permission: DistributionTargetReadPermission, organization: {source: "installation"})
@audit(event: DistributionTargetResolveAuditEvent, resource: DistributionTarget, action: "resolve")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "distribution.targets", method: "resolve", paginated: false, retryable: true)
operation ResolveDistributionTarget {
    input: ResolveDistributionTargetInput
    output: ResolveDistributionTargetOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, QuarantinedArtifactError, RateLimitedError, ServiceUnavailableError, UnauthenticatedError]
}

@readonly
@http(method: "GET", uri: "/api/v1/distribution/artifacts/{artifact_digest_ref}", code: 200)
@identity(mode: "public", audience: "verself-api", principals: ["browser", "cli", "workload", "anonymous"])
@authz(permission: DistributionArtifactReadPermission, organization: {source: "installation"})
@audit(event: DistributionArtifactGetAuditEvent, resource: DistributionArtifact, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "distribution.artifacts", method: "get", paginated: false, retryable: true)
operation GetDistributionArtifact {
    input: DistributionArtifactPathInput
    output: DistributionArtifactOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError, UnauthenticatedError]
}

@readonly
@http(method: "GET", uri: "/api/v1/distribution/packages/{package_name}/channels/{channel_name}/targets", code: 200)
@paginated(inputToken: "page_token", outputToken: "next_page_token", pageSize: "page_size", items: "targets")
@identity(mode: "public", audience: "verself-api", principals: ["browser", "cli", "workload", "anonymous"])
@authz(permission: DistributionTargetReadPermission, organization: {source: "installation"})
@audit(event: DistributionChannelListTargetsAuditEvent, resource: DistributionChannel, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "distribution.channels", method: "listTargets", paginated: true, retryable: true)
operation ListDistributionChannelTargets {
    input: ListDistributionChannelTargetsInput
    output: ListDistributionChannelTargetsOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError, UnauthenticatedError]
}

@idempotent
@http(method: "POST", uri: "/internal/v1/distribution/artifacts:admit", code: 201)
@identity(mode: "spiffe_mtls", audience: "distribution-service", principals: ["workload"])
@authz(permission: DistributionArtifactWritePermission, organization: {source: "request_id"})
@audit(event: DistributionArtifactAdmitAuditEvent, resource: DistributionArtifact, action: "create")
@rateLimit(bucket: "distribution_mutation")
@requestBudget(maxBytes: 65536)
@sdk(module: "distributionInternal.artifacts", method: "admit", paginated: false, retryable: true)
operation AdmitDistributionArtifact {
    input: AdmitDistributionArtifactInput
    output: DistributionArtifactOutput
    errors: [
        ValidationFailedError,
        PermissionDeniedError,
        ConflictError,
        IdempotencyPayloadMismatchError,
        MissingOCIReferrersError,
        DistributionDigestMismatchError,
        UntrustedBuilderError,
        UntrustedSignerError,
        SourcePolicyFailureError,
        QuarantinedArtifactError,
        RateLimitedError,
        ServiceUnavailableError,
        UnauthenticatedError
    ]
}

@idempotent
@http(method: "POST", uri: "/internal/v1/distribution/packages/{package_name}/channels/{channel_name}/targets/promote", code: 200)
@identity(mode: "spiffe_mtls", audience: "distribution-service", principals: ["workload"])
@authz(permission: DistributionTargetWritePermission, organization: {source: "request_id"})
@audit(event: DistributionTargetPromoteAuditEvent, resource: DistributionTarget, action: "set")
@rateLimit(bucket: "distribution_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "distributionInternal.targets", method: "promote", paginated: false, retryable: true)
operation PromoteDistributionTarget {
    input: PromoteDistributionTargetInput
    output: ResolveDistributionTargetOutput
    errors: [
        ValidationFailedError,
        PermissionDeniedError,
        ResourceNotFoundError,
        ConflictError,
        IdempotencyPayloadMismatchError,
        ChannelPolicyFailureError,
        QuarantinedArtifactError,
        RateLimitedError,
        ServiceUnavailableError,
        UnauthenticatedError
    ]
}

@idempotent
@http(method: "POST", uri: "/internal/v1/distribution/artifacts/{artifact_digest_ref}/quarantine", code: 200)
@identity(mode: "spiffe_mtls", audience: "distribution-service", principals: ["workload"])
@authz(permission: DistributionArtifactWritePermission, organization: {source: "request_id"})
@audit(event: DistributionArtifactQuarantineAuditEvent, resource: DistributionQuarantine, action: "set")
@rateLimit(bucket: "distribution_mutation")
@requestBudget(maxBytes: 8192)
@sdk(module: "distributionInternal.artifacts", method: "quarantine", paginated: false, retryable: true)
operation QuarantineDistributionArtifact {
    input: QuarantineDistributionArtifactInput
    output: DistributionArtifactOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError, UnauthenticatedError]
}

@idempotent
@http(method: "POST", uri: "/internal/v1/distribution/artifacts/{artifact_digest_ref}/replicate", code: 200)
@identity(mode: "spiffe_mtls", audience: "distribution-service", principals: ["workload"])
@authz(permission: DistributionReplicationWritePermission, organization: {source: "request_id"})
@audit(event: DistributionReplicationEnsureAuditEvent, resource: DistributionReplication, action: "sync")
@rateLimit(bucket: "distribution_mutation")
@requestBudget(maxBytes: 8192)
@sdk(module: "distributionInternal.replication", method: "ensure", paginated: false, retryable: true)
operation EnsureDistributionReplication {
    input: EnsureDistributionReplicationInput
    output: DistributionArtifactOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, DistributionReplicationFailureError, RateLimitedError, ServiceUnavailableError, UnauthenticatedError]
}

@readonly
@http(method: "GET", uri: "/internal/v1/distribution/artifacts/{artifact_digest_ref}", code: 200)
@identity(mode: "spiffe_mtls", audience: "distribution-service", principals: ["workload"])
@authz(permission: DistributionArtifactReadPermission, organization: {source: "request_id"})
@audit(event: DistributionArtifactGetAuditEvent, resource: DistributionArtifact, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "distributionInternal.artifacts", method: "get", paginated: false, retryable: true)
operation GetDistributionArtifactInternal {
    input: DistributionArtifactPathInput
    output: DistributionArtifactOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError, UnauthenticatedError]
}
