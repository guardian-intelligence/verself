$version: "2"
namespace verself.release.v1

use aws.protocols#restJson1
use smithy.api#http
use smithy.api#httpHeader
use smithy.api#httpLabel
use smithy.api#httpPayload
use smithy.api#idempotencyToken
use smithy.api#idempotent
use smithy.api#length
use smithy.api#pattern
use smithy.api#range
use smithy.api#readonly
use smithy.api#required
use verself.common.v1#ConflictError
use verself.common.v1#DateTime
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

@serviceRuntime(serviceName: "release-service", publicAudience: "verself-api", internalAudience: "release-service")
@restJson1
service ReleaseInternal {
    version: "2026-05-26"
    operations: [
        RegisterReleasePackage,
        CreateReleaseVersion,
        GetReleaseVersion,
        AttachReleaseArtifact,
        AttachReleaseEvidence,
        VerifyRelease,
        ApproveRelease,
        PromoteReleaseChannel,
        ListReleaseInstallOptions,
        CreateReleasePublication,
        DispatchReleasePublication,
        RecordReleasePublicationReceipt,
        ReconcileReleasePublication
    ]
    resources: [
        ReleasePackage,
        ReleaseVersion,
        ReleaseArtifact,
        ReleaseEvidence,
        ReleaseChannel,
        ReleasePublication,
        ReleaseInstallOption
    ]
}

@pattern("^[0-9a-fA-F-]{36}$")
string ReleasePackageId

@pattern("^[0-9a-fA-F-]{36}$")
string ReleaseVersionId

@pattern("^[0-9a-fA-F-]{36}$")
string ReleaseArtifactId

@pattern("^[0-9a-fA-F-]{36}$")
string ReleaseEvidenceId

@pattern("^[0-9a-fA-F-]{36}$")
string ReleaseChannelId

@pattern("^[0-9a-fA-F-]{36}$")
string ReleasePublicationId

@pattern("^[0-9a-fA-F-]{36}$")
string ReleaseReceiptId

@pattern("^[0-9a-fA-F-]{36}$")
string ReleaseInstallOptionId

@length(min: 1, max: 128)
string OrgId

@length(min: 1, max: 128)
@pattern("^[a-z0-9][a-z0-9._-]*[a-z0-9]$")
string PackageName

@length(min: 1, max: 64)
@pattern("^[a-z][a-z0-9_]*$")
string ReleaseKind

@length(min: 1, max: 128)
string ReleaseVersionText

@length(min: 1, max: 64)
@pattern("^[a-z][a-z0-9_]*$")
string ReleaseState

@length(min: 1, max: 64)
@pattern("^[a-z][a-z0-9_]*$")
string LifecycleStatus

@length(min: 1, max: 64)
@pattern("^[a-z][a-z0-9_]*$")
string VersionScheme

@length(min: 1, max: 4096)
string RepoPath

@length(min: 1, max: 4096)
string BazelTarget

@length(min: 1, max: 4096)
string SourceRepository

@length(min: 1, max: 128)
string SourceCommit

@length(min: 1, max: 4096)
string SourceRef

@length(min: 1, max: 128)
string OwnerTeam

@length(min: 1, max: 128)
string Visibility

@length(min: 1, max: 4096)
string PolicyRef

@range(min: 0, max: 9223372036854775807)
long ReleaseSequence

@length(min: 1, max: 128)
string PlatformOS

@length(min: 1, max: 128)
string PlatformArch

@length(min: 1, max: 4096)
string OCIRepository

@length(min: 1, max: 4096)
@pattern("^sha256:[a-f0-9]{64}$")
string OCIDigest

@length(min: 1, max: 4096)
string OCIMediaType

@range(min: 0, max: 9223372036854775807)
long OCISizeBytes

@length(min: 1, max: 4096)
string RegistryURL

@length(min: 1, max: 128)
string EvidenceKind

@length(min: 1, max: 4096)
string PredicateType

@length(min: 1, max: 4096)
string DocumentDigest

@length(min: 1, max: 4096)
string ChannelName

@length(min: 1, max: 128)
string ProviderKind

@length(min: 1, max: 4096)
string ProviderPackage

@length(min: 1, max: 4096)
string ProviderVersion

@length(min: 1, max: 4096)
string ProviderChannel

@length(min: 1, max: 4096)
string ProviderResourceURL

@length(min: 1, max: 64)
string InstallOptionKind

@length(min: 1, max: 64)
string InstallOptionStatus

@length(min: 1, max: 8192)
string CommandTemplate

@length(min: 1, max: 8192)
string VerificationTemplate

@length(min: 1, max: 4096)
string ActorId

@length(min: 1, max: 4096)
string DecisionReason

@permission(name: "release:package:write")
string ReleasePackageWritePermission

@permission(name: "release:package:read")
string ReleasePackageReadPermission

@permission(name: "release:version:write")
string ReleaseVersionWritePermission

@permission(name: "release:version:read")
string ReleaseVersionReadPermission

@permission(name: "release:publication:write")
string ReleasePublicationWritePermission

@auditEvent(name: "release.package.register")
string ReleasePackageRegisterAuditEvent

@auditEvent(name: "release.version.create")
string ReleaseVersionCreateAuditEvent

@auditEvent(name: "release.version.read")
string ReleaseVersionReadAuditEvent

@auditEvent(name: "release.artifact.attach")
string ReleaseArtifactAttachAuditEvent

@auditEvent(name: "release.evidence.attach")
string ReleaseEvidenceAttachAuditEvent

@auditEvent(name: "release.version.verify")
string ReleaseVerifyAuditEvent

@auditEvent(name: "release.version.approve")
string ReleaseApproveAuditEvent

@auditEvent(name: "release.channel.promote")
string ReleaseChannelPromoteAuditEvent

@auditEvent(name: "release.install_options.list")
string ReleaseInstallOptionsListAuditEvent

@auditEvent(name: "release.publication.create")
string ReleasePublicationCreateAuditEvent

@auditEvent(name: "release.publication.dispatch")
string ReleasePublicationDispatchAuditEvent

@auditEvent(name: "release.publication.receipt.record")
string ReleasePublicationReceiptRecordAuditEvent

@auditEvent(name: "release.publication.reconcile")
string ReleasePublicationReconcileAuditEvent

resource ReleasePackage {}
resource ReleaseVersion {}
resource ReleaseArtifact {}
resource ReleaseEvidence {}
resource ReleaseChannel {}
resource ReleasePublication {}
resource ReleaseInstallOption {}

structure RegisterReleasePackageBody {
    @required
    @protoField(number: 1)
    org_id: OrgId

    @required
    @protoField(number: 2)
    package_name: PackageName

    @required
    @protoField(number: 3)
    package_kind: ReleaseKind

    @required
    @protoField(number: 4)
    repo_path: RepoPath

    @required
    @protoField(number: 5)
    bazel_target: BazelTarget

    @required
    @protoField(number: 6)
    owner_team: OwnerTeam

    @required
    @protoField(number: 7)
    default_version_scheme: VersionScheme

    @required
    @protoField(number: 8)
    visibility: Visibility

    @required
    @protoField(number: 9)
    policy_ref: PolicyRef
}

structure CreateReleaseVersionBody {
    @required
    @protoField(number: 1)
    canonical_version: ReleaseVersionText

    @required
    @protoField(number: 2)
    version_scheme: VersionScheme

    @required
    @protoField(number: 3)
    version_kind: ReleaseKind

    @required
    @protoField(number: 4)
    release_sequence: ReleaseSequence

    @required
    @protoField(number: 5)
    source_repository: SourceRepository

    @required
    @protoField(number: 6)
    source_commit: SourceCommit

    @required
    @protoField(number: 7)
    source_ref: SourceRef
}

structure AttachReleaseArtifactBody {
    @required
    @protoField(number: 1)
    artifact_role: ReleaseKind

    @required
    @protoField(number: 2)
    platform_os: PlatformOS

    @required
    @protoField(number: 3)
    platform_arch: PlatformArch

    @required
    @protoField(number: 4)
    oci_repository: OCIRepository

    @required
    @protoField(number: 5)
    oci_digest: OCIDigest

    @required
    @protoField(number: 6)
    oci_media_type: OCIMediaType

    @required
    @protoField(number: 7)
    oci_size_bytes: OCISizeBytes

    @required
    @protoField(number: 8)
    registry_url: RegistryURL
}

structure AttachReleaseEvidenceBody {
    @required
    @protoField(number: 1)
    artifact_id: ReleaseArtifactId

    @required
    @protoField(number: 2)
    evidence_kind: EvidenceKind

    @required
    @protoField(number: 3)
    predicate_type: PredicateType

    @required
    @protoField(number: 4)
    subject_digest: OCIDigest

    @required
    @protoField(number: 5)
    document_digest: DocumentDigest

    @required
    @protoField(number: 6)
    oci_referrer_digest: OCIDigest
}

structure VerifyReleaseBody {
    @required
    @protoField(number: 1)
    policy_ref: PolicyRef

    @required
    @protoField(number: 2)
    policy_digest: DocumentDigest

    @required
    @protoField(number: 3)
    builder_id: SourceRef

    @required
    @protoField(number: 4)
    signer_identity: SourceRef
}

structure ApproveReleaseBody {
    @required
    @protoField(number: 1)
    actor_id: ActorId

    @required
    @protoField(number: 2)
    reason: DecisionReason
}

structure PromoteReleaseChannelBody {
    @required
    @protoField(number: 1)
    release_version_id: ReleaseVersionId

    @required
    @protoField(number: 2)
    channel_kind: ReleaseKind

    @required
    @protoField(number: 3)
    visibility: Visibility

    @required
    @protoField(number: 4)
    policy_ref: PolicyRef

    @required
    @protoField(number: 5)
    actor_id: ActorId

    @required
    @protoField(number: 6)
    reason: DecisionReason
}

structure CreateReleasePublicationBody {
    @required
    @protoField(number: 1)
    provider_kind: ProviderKind

    @required
    @protoField(number: 2)
    provider_package: ProviderPackage

    @required
    @protoField(number: 3)
    provider_channel: ProviderChannel

    @required
    @protoField(number: 4)
    requested_by: ActorId
}

structure RecordPublicationReceiptRequest {
    @required
    @protoField(number: 1)
    provider_resource_url: ProviderResourceURL

    @required
    @protoField(number: 2)
    provider_version: ProviderVersion

    @required
    @protoField(number: 3)
    provider_digest: DocumentDigest

    @required
    @protoField(number: 4)
    receipt_digest: DocumentDigest
}

structure ReleasePackageRecord {
    @required
    @protoField(number: 1)
    package_id: ReleasePackageId

    @required
    @protoField(number: 2)
    resourceName: ResourceName

    @required
    @protoField(number: 3)
    org_id: OrgId

    @required
    @protoField(number: 4)
    package_name: PackageName

    @required
    @protoField(number: 5)
    package_kind: ReleaseKind

    @required
    @protoField(number: 6)
    repo_path: RepoPath

    @required
    @protoField(number: 7)
    bazel_target: BazelTarget

    @required
    @protoField(number: 8)
    owner_team: OwnerTeam

    @required
    @protoField(number: 9)
    default_version_scheme: VersionScheme

    @required
    @protoField(number: 10)
    visibility: Visibility

    @required
    @protoField(number: 11)
    policy_ref: PolicyRef

    @required
    @protoField(number: 12)
    created_at: DateTime

    @required
    @protoField(number: 13)
    updated_at: DateTime
}

structure ReleaseVersionRecord {
    @required
    @protoField(number: 1)
    release_version_id: ReleaseVersionId

    @required
    @protoField(number: 2)
    resourceName: ResourceName

    @required
    @protoField(number: 3)
    package_id: ReleasePackageId

    @required
    @protoField(number: 4)
    canonical_version: ReleaseVersionText

    @required
    @protoField(number: 5)
    version_scheme: VersionScheme

    @required
    @protoField(number: 6)
    version_kind: ReleaseKind

    @required
    @protoField(number: 7)
    release_sequence: ReleaseSequence

    @required
    @protoField(number: 8)
    source_repository: SourceRepository

    @required
    @protoField(number: 9)
    source_commit: SourceCommit

    @required
    @protoField(number: 10)
    source_ref: SourceRef

    @required
    @protoField(number: 11)
    state: ReleaseState

    @required
    @protoField(number: 12)
    lifecycle_status: LifecycleStatus

    @required
    @protoField(number: 13)
    created_at: DateTime

    @required
    @protoField(number: 14)
    updated_at: DateTime
}

structure ReleaseArtifactRecord {
    @required
    @protoField(number: 1)
    artifact_id: ReleaseArtifactId

    @required
    @protoField(number: 2)
    release_version_id: ReleaseVersionId

    @required
    @protoField(number: 3)
    artifact_role: ReleaseKind

    @required
    @protoField(number: 4)
    platform_os: PlatformOS

    @required
    @protoField(number: 5)
    platform_arch: PlatformArch

    @required
    @protoField(number: 6)
    oci_repository: OCIRepository

    @required
    @protoField(number: 7)
    oci_digest: OCIDigest

    @required
    @protoField(number: 8)
    oci_media_type: OCIMediaType

    @required
    @protoField(number: 9)
    oci_size_bytes: OCISizeBytes

    @required
    @protoField(number: 10)
    registry_url: RegistryURL

    @required
    @protoField(number: 11)
    created_at: DateTime
}

structure ReleaseEvidenceRecord {
    @required
    @protoField(number: 1)
    evidence_id: ReleaseEvidenceId

    @required
    @protoField(number: 2)
    release_version_id: ReleaseVersionId

    @required
    @protoField(number: 3)
    artifact_id: ReleaseArtifactId

    @required
    @protoField(number: 4)
    evidence_kind: EvidenceKind

    @required
    @protoField(number: 5)
    predicate_type: PredicateType

    @required
    @protoField(number: 6)
    subject_digest: OCIDigest

    @required
    @protoField(number: 7)
    document_digest: DocumentDigest

    @required
    @protoField(number: 8)
    oci_referrer_digest: OCIDigest

    @required
    @protoField(number: 9)
    verification_state: ReleaseState

    @required
    @protoField(number: 10)
    created_at: DateTime
}

structure ReleaseInstallOptionRecord {
    @required
    @protoField(number: 1)
    install_option_id: ReleaseInstallOptionId

    @required
    @protoField(number: 2)
    release_version_id: ReleaseVersionId

    @required
    @protoField(number: 3)
    option_kind: InstallOptionKind

    @required
    @protoField(number: 4)
    status: InstallOptionStatus

    @required
    @protoField(number: 5)
    display_order: Integer

    @required
    @protoField(number: 6)
    command_template: CommandTemplate

    @required
    @protoField(number: 7)
    verification_template: VerificationTemplate

    @required
    @protoField(number: 8)
    oci_repository: String

    @required
    @protoField(number: 9)
    oci_digest: String

    @required
    @protoField(number: 10)
    provider_kind: String

    @required
    @protoField(number: 11)
    provider_ref: String
}

list ReleaseInstallOptions {
    member: ReleaseInstallOptionRecord
}

structure ReleasePublicationRecord {
    @required
    @protoField(number: 1)
    publication_id: ReleasePublicationId

    @required
    @protoField(number: 2)
    release_version_id: ReleaseVersionId

    @required
    @protoField(number: 3)
    provider_kind: ProviderKind

    @required
    @protoField(number: 4)
    provider_package: ProviderPackage

    @required
    @protoField(number: 5)
    provider_channel: ProviderChannel

    @required
    @protoField(number: 6)
    requested_by: ActorId

    @required
    @protoField(number: 7)
    state: ReleaseState

    @required
    @protoField(number: 8)
    idempotency_key: String

    @required
    @protoField(number: 9)
    created_at: DateTime

    @required
    @protoField(number: 10)
    updated_at: DateTime
}

structure ReleasePackageOutput {
    @required
    @protoField(number: 1)
    package: ReleasePackageRecord
}

structure ReleaseVersionOutput {
    @required
    @protoField(number: 1)
    release: ReleaseVersionRecord
}

structure ReleaseArtifactOutput {
    @required
    @protoField(number: 1)
    artifact: ReleaseArtifactRecord
}

structure ReleaseEvidenceOutput {
    @required
    @protoField(number: 1)
    evidence: ReleaseEvidenceRecord
}

structure ReleaseVerificationOutput {
    @required
    @protoField(number: 1)
    release: ReleaseVersionRecord

    @required
    @protoField(number: 2)
    decision: ReleaseState

    @required
    @protoField(number: 3)
    reason: DecisionReason
}

structure ReleaseChannelOutput {
    @required
    @protoField(number: 1)
    channel_id: ReleaseChannelId

    @required
    @protoField(number: 2)
    current_release_version_id: ReleaseVersionId

    @required
    @protoField(number: 3)
    channel_name: ChannelName
}

structure ReleaseInstallOptionsOutput {
    @required
    @protoField(number: 1)
    install_options: ReleaseInstallOptions
}

structure ReleasePublicationOutput {
    @required
    @protoField(number: 1)
    publication: ReleasePublicationRecord
}

structure RegisterReleasePackageInput {
    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    @httpPayload
    body: RegisterReleasePackageBody
}

structure CreateReleaseVersionInput {
    @required
    @httpLabel
    package_id: ReleasePackageId

    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    @httpPayload
    body: CreateReleaseVersionBody
}

structure ReleaseVersionPathInput {
    @required
    @httpLabel
    release_version_id: ReleaseVersionId
}

structure AttachReleaseArtifactInput {
    @required
    @httpLabel
    release_version_id: ReleaseVersionId

    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    @httpPayload
    body: AttachReleaseArtifactBody
}

structure AttachReleaseEvidenceInput {
    @required
    @httpLabel
    release_version_id: ReleaseVersionId

    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    @httpPayload
    body: AttachReleaseEvidenceBody
}

structure VerifyReleaseInput {
    @required
    @httpLabel
    release_version_id: ReleaseVersionId

    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    @httpPayload
    body: VerifyReleaseBody
}

structure ApproveReleaseInput {
    @required
    @httpLabel
    release_version_id: ReleaseVersionId

    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    @httpPayload
    body: ApproveReleaseBody
}

structure PromoteReleaseChannelInput {
    @required
    @httpLabel
    package_id: ReleasePackageId

    @required
    @httpLabel
    channel_name: ChannelName

    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    @httpPayload
    body: PromoteReleaseChannelBody
}

structure ListReleaseInstallOptionsInput {
    @required
    @httpLabel
    release_version_id: ReleaseVersionId
}

structure CreateReleasePublicationInput {
    @required
    @httpLabel
    release_version_id: ReleaseVersionId

    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    @httpPayload
    body: CreateReleasePublicationBody
}

structure ReleasePublicationPathInput {
    @required
    @httpLabel
    publication_id: ReleasePublicationId

    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey
}

structure RecordPublicationReceiptInput {
    @required
    @httpLabel
    publication_id: ReleasePublicationId

    @required
    @httpHeader("Idempotency-Key")
    @idempotencyToken
    idempotencyKey: IdempotencyKey

    @required
    @httpPayload
    body: RecordPublicationReceiptRequest
}

@idempotent
@http(method: "POST", uri: "/internal/v1/release/packages", code: 201)
@identity(mode: "spiffe_mtls", audience: "release-service", principals: ["workload"])
@authz(permission: ReleasePackageWritePermission, organization: {source: "body_org_id", member: "org_id"})
@audit(event: ReleasePackageRegisterAuditEvent, resource: ReleasePackage, action: "create")
@rateLimit(bucket: "release_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "release.internal", method: "registerPackage", paginated: false, retryable: true)
operation RegisterReleasePackage {
    input: RegisterReleasePackageInput
    output: ReleasePackageOutput
    errors: [ValidationFailedError, PermissionDeniedError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError, UnauthenticatedError]
}

@idempotent
@http(method: "POST", uri: "/internal/v1/release/packages/{package_id}/versions", code: 201)
@identity(mode: "spiffe_mtls", audience: "release-service", principals: ["workload"])
@authz(permission: ReleaseVersionWritePermission, organization: {source: "request_id"})
@audit(event: ReleaseVersionCreateAuditEvent, resource: ReleaseVersion, action: "create")
@rateLimit(bucket: "release_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "release.internal", method: "createVersion", paginated: false, retryable: true)
operation CreateReleaseVersion {
    input: CreateReleaseVersionInput
    output: ReleaseVersionOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError, UnauthenticatedError]
}

@readonly
@http(method: "GET", uri: "/internal/v1/release/versions/{release_version_id}", code: 200)
@identity(mode: "spiffe_mtls", audience: "release-service", principals: ["workload"])
@authz(permission: ReleaseVersionReadPermission, organization: {source: "request_id"})
@audit(event: ReleaseVersionReadAuditEvent, resource: ReleaseVersion, action: "read")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "release.internal", method: "getVersion", paginated: false, retryable: true)
operation GetReleaseVersion {
    input: ReleaseVersionPathInput
    output: ReleaseVersionOutput
    errors: [PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError, UnauthenticatedError]
}

@idempotent
@http(method: "POST", uri: "/internal/v1/release/versions/{release_version_id}/artifacts", code: 201)
@identity(mode: "spiffe_mtls", audience: "release-service", principals: ["workload"])
@authz(permission: ReleaseVersionWritePermission, organization: {source: "request_id"})
@audit(event: ReleaseArtifactAttachAuditEvent, resource: ReleaseArtifact, action: "write")
@rateLimit(bucket: "release_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "release.internal", method: "attachArtifact", paginated: false, retryable: true)
operation AttachReleaseArtifact {
    input: AttachReleaseArtifactInput
    output: ReleaseArtifactOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError, UnauthenticatedError]
}

@idempotent
@http(method: "POST", uri: "/internal/v1/release/versions/{release_version_id}/evidence", code: 201)
@identity(mode: "spiffe_mtls", audience: "release-service", principals: ["workload"])
@authz(permission: ReleaseVersionWritePermission, organization: {source: "request_id"})
@audit(event: ReleaseEvidenceAttachAuditEvent, resource: ReleaseEvidence, action: "write")
@rateLimit(bucket: "release_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "release.internal", method: "attachEvidence", paginated: false, retryable: true)
operation AttachReleaseEvidence {
    input: AttachReleaseEvidenceInput
    output: ReleaseEvidenceOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError, UnauthenticatedError]
}

@idempotent
@http(method: "POST", uri: "/internal/v1/release/versions/{release_version_id}/verify", code: 200)
@identity(mode: "spiffe_mtls", audience: "release-service", principals: ["workload"])
@authz(permission: ReleaseVersionWritePermission, organization: {source: "request_id"})
@audit(event: ReleaseVerifyAuditEvent, resource: ReleaseVersion, action: "verify")
@rateLimit(bucket: "release_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "release.internal", method: "verify", paginated: false, retryable: true)
operation VerifyRelease {
    input: VerifyReleaseInput
    output: ReleaseVerificationOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError, UnauthenticatedError]
}

@idempotent
@http(method: "POST", uri: "/internal/v1/release/versions/{release_version_id}/approve", code: 200)
@identity(mode: "spiffe_mtls", audience: "release-service", principals: ["workload"])
@authz(permission: ReleaseVersionWritePermission, organization: {source: "request_id"})
@audit(event: ReleaseApproveAuditEvent, resource: ReleaseVersion, action: "set")
@rateLimit(bucket: "release_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "release.internal", method: "approve", paginated: false, retryable: true)
operation ApproveRelease {
    input: ApproveReleaseInput
    output: ReleaseVersionOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError, UnauthenticatedError]
}

@idempotent
@http(method: "POST", uri: "/internal/v1/release/packages/{package_id}/channels/{channel_name}/promote", code: 200)
@identity(mode: "spiffe_mtls", audience: "release-service", principals: ["workload"])
@authz(permission: ReleaseVersionWritePermission, organization: {source: "request_id"})
@audit(event: ReleaseChannelPromoteAuditEvent, resource: ReleaseChannel, action: "set")
@rateLimit(bucket: "release_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "release.internal", method: "promoteChannel", paginated: false, retryable: true)
operation PromoteReleaseChannel {
    input: PromoteReleaseChannelInput
    output: ReleaseChannelOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError, UnauthenticatedError]
}

@readonly
@http(method: "GET", uri: "/internal/v1/release/versions/{release_version_id}/install-options", code: 200)
@identity(mode: "spiffe_mtls", audience: "release-service", principals: ["workload"])
@authz(permission: ReleaseVersionReadPermission, organization: {source: "request_id"})
@audit(event: ReleaseInstallOptionsListAuditEvent, resource: ReleaseInstallOption, action: "list")
@rateLimit(bucket: "read")
@requestBudget(maxBytes: 0)
@sdk(module: "release.internal", method: "listInstallOptions", paginated: false, retryable: true)
operation ListReleaseInstallOptions {
    input: ListReleaseInstallOptionsInput
    output: ReleaseInstallOptionsOutput
    errors: [PermissionDeniedError, ResourceNotFoundError, RateLimitedError, ServiceUnavailableError, UnauthenticatedError]
}

@idempotent
@http(method: "POST", uri: "/internal/v1/release/versions/{release_version_id}/publications", code: 201)
@identity(mode: "spiffe_mtls", audience: "release-service", principals: ["workload"])
@authz(permission: ReleasePublicationWritePermission, organization: {source: "request_id"})
@audit(event: ReleasePublicationCreateAuditEvent, resource: ReleasePublication, action: "create")
@rateLimit(bucket: "release_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "release.internal", method: "createPublication", paginated: false, retryable: true)
operation CreateReleasePublication {
    input: CreateReleasePublicationInput
    output: ReleasePublicationOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError, UnauthenticatedError]
}

@idempotent
@http(method: "POST", uri: "/internal/v1/release/publications/{publication_id}/dispatch", code: 200)
@identity(mode: "spiffe_mtls", audience: "release-service", principals: ["workload"])
@authz(permission: ReleasePublicationWritePermission, organization: {source: "request_id"})
@audit(event: ReleasePublicationDispatchAuditEvent, resource: ReleasePublication, action: "sync")
@rateLimit(bucket: "release_mutation")
@requestBudget(maxBytes: 0)
@sdk(module: "release.internal", method: "dispatchPublication", paginated: false, retryable: true)
operation DispatchReleasePublication {
    input: ReleasePublicationPathInput
    output: ReleasePublicationOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError, UnauthenticatedError]
}

@idempotent
@http(method: "POST", uri: "/internal/v1/release/publications/{publication_id}/receipts", code: 201)
@identity(mode: "spiffe_mtls", audience: "release-service", principals: ["workload"])
@authz(permission: ReleasePublicationWritePermission, organization: {source: "request_id"})
@audit(event: ReleasePublicationReceiptRecordAuditEvent, resource: ReleasePublication, action: "write")
@rateLimit(bucket: "release_mutation")
@requestBudget(maxBytes: 16384)
@sdk(module: "release.internal", method: "recordPublicationReceipt", paginated: false, retryable: true)
operation RecordReleasePublicationReceipt {
    input: RecordPublicationReceiptInput
    output: ReleasePublicationOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError, UnauthenticatedError]
}

@idempotent
@http(method: "POST", uri: "/internal/v1/release/publications/{publication_id}/reconcile", code: 200)
@identity(mode: "spiffe_mtls", audience: "release-service", principals: ["workload"])
@authz(permission: ReleasePublicationWritePermission, organization: {source: "request_id"})
@audit(event: ReleasePublicationReconcileAuditEvent, resource: ReleasePublication, action: "sync")
@rateLimit(bucket: "release_mutation")
@requestBudget(maxBytes: 0)
@sdk(module: "release.internal", method: "reconcilePublication", paginated: false, retryable: true)
operation ReconcileReleasePublication {
    input: ReleasePublicationPathInput
    output: ReleasePublicationOutput
    errors: [ValidationFailedError, PermissionDeniedError, ResourceNotFoundError, ConflictError, IdempotencyPayloadMismatchError, RateLimitedError, ServiceUnavailableError, UnauthenticatedError]
}
