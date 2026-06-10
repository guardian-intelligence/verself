-- name: Ping :one
SELECT 1::int;

-- name: CreateIdempotencyKey :one
INSERT INTO distribution_idempotency_keys (
    scope,
    idempotency_key,
    payload_sha256,
    resource_kind,
    resource_id,
    created_at
) VALUES (
    @scope,
    @idempotency_key,
    @payload_sha256,
    @resource_kind,
    @resource_id,
    @created_at
)
ON CONFLICT (scope, idempotency_key) DO NOTHING
RETURNING *;

-- name: GetIdempotencyKey :one
SELECT *
FROM distribution_idempotency_keys
WHERE scope = @scope AND idempotency_key = @idempotency_key;

-- name: CreateArtifact :one
INSERT INTO distribution_artifacts (
    artifact_id,
    package_name,
    package_version,
    channel_name,
    platform_os,
    platform_arch,
    flavor,
    origin_registry_url,
    public_registry_url,
    oci_repository,
    oci_digest,
    oci_media_type,
    oci_size_bytes,
    public_oci_reference,
    builder_id,
    signer_identity,
    source_repository,
    source_commit,
    source_ref,
    policy_ref,
    state,
    verification_decision,
    verification_reason,
    retention_class,
    replication_state,
    submitted_by,
    admitted_at,
    available_at,
    created_at,
    updated_at
) VALUES (
    @artifact_id,
    @package_name,
    @package_version,
    @channel_name,
    @platform_os,
    @platform_arch,
    @flavor,
    @origin_registry_url,
    @public_registry_url,
    @oci_repository,
    @oci_digest,
    @oci_media_type,
    @oci_size_bytes,
    @public_oci_reference,
    @builder_id,
    @signer_identity,
    @source_repository,
    @source_commit,
    @source_ref,
    @policy_ref,
    @state,
    @verification_decision,
    @verification_reason,
    @retention_class,
    @replication_state,
    @submitted_by,
    @admitted_at,
    @available_at,
    @created_at,
    @updated_at
)
RETURNING *;

-- name: CreateEvidence :one
INSERT INTO distribution_artifact_evidence (
    evidence_id,
    artifact_id,
    evidence_kind,
    predicate_type,
    subject_digest,
    document_digest,
    oci_referrer_digest,
    signing_key_id,
    created_at
) VALUES (
    @evidence_id,
    @artifact_id,
    @evidence_kind,
    @predicate_type,
    @subject_digest,
    @document_digest,
    @oci_referrer_digest,
    @signing_key_id,
    @created_at
)
ON CONFLICT (artifact_id, evidence_kind, document_digest) DO UPDATE
SET oci_referrer_digest = EXCLUDED.oci_referrer_digest,
    signing_key_id = EXCLUDED.signing_key_id
RETURNING *;

-- name: GetArtifactByDigest :one
SELECT *
FROM distribution_artifacts
WHERE oci_digest = @oci_digest
ORDER BY available_at DESC, artifact_id DESC
LIMIT 1;

-- name: GetArtifactByRepositoryDigest :one
SELECT *
FROM distribution_artifacts
WHERE oci_repository = @oci_repository AND oci_digest = @oci_digest
ORDER BY available_at DESC, artifact_id DESC
LIMIT 1;

-- name: GetArtifactByCoordinateDigest :one
SELECT *
FROM distribution_artifacts
WHERE package_name = @package_name
  AND package_version = @package_version
  AND channel_name = @channel_name
  AND platform_os = @platform_os
  AND platform_arch = @platform_arch
  AND flavor = @flavor
  AND oci_digest = @oci_digest;

-- name: ListEvidenceForArtifact :many
SELECT *
FROM distribution_artifact_evidence
WHERE artifact_id = @artifact_id
ORDER BY evidence_kind, evidence_id;

-- name: MarkArtifactQuarantined :one
UPDATE distribution_artifacts
SET
    state = 'quarantined',
    quarantine_reason = @quarantine_reason,
    quarantined_at = @quarantined_at,
    updated_at = @updated_at
WHERE oci_digest = @oci_digest
  AND state <> 'deleted'
RETURNING *;

-- name: EnsureArtifactReplication :one
UPDATE distribution_artifacts
SET
    replication_state = 'available',
    updated_at = @updated_at
WHERE oci_digest = @oci_digest
  AND state NOT IN ('quarantined', 'deleted')
RETURNING *;

-- name: GetCurrentTarget :one
SELECT *
FROM distribution_channel_targets
WHERE package_name = @package_name
  AND channel_name = @channel_name
  AND platform_os = @platform_os
  AND platform_arch = @platform_arch
  AND flavor = @flavor
  AND state IN ('published', 'rollback_target_published')
ORDER BY published_at DESC, target_id DESC
LIMIT 1;

-- name: SupersedeCurrentTargets :exec
UPDATE distribution_channel_targets
SET
    state = 'superseded',
    superseded_at = @superseded_at,
    superseded_by_digest = @superseded_by_digest,
    updated_at = @updated_at
WHERE package_name = @package_name
  AND channel_name = @channel_name
  AND platform_os = @platform_os
  AND platform_arch = @platform_arch
  AND flavor = @flavor
  AND state IN ('published', 'rollback_target_published');

-- name: UpsertPublishedTarget :one
INSERT INTO distribution_channel_targets (
    target_id,
    package_name,
    channel_name,
    platform_os,
    platform_arch,
    flavor,
    artifact_id,
    artifact_digest,
    package_version,
    state,
    public_oci_reference,
    download_url,
    policy_ref,
    promoted_by,
    reason,
    published_at,
    created_at,
    updated_at
) VALUES (
    @target_id,
    @package_name,
    @channel_name,
    @platform_os,
    @platform_arch,
    @flavor,
    @artifact_id,
    @artifact_digest,
    @package_version,
    @state,
    @public_oci_reference,
    @download_url,
    @policy_ref,
    @promoted_by,
    @reason,
    @published_at,
    @created_at,
    @updated_at
)
ON CONFLICT (package_name, channel_name, platform_os, platform_arch, flavor, package_version, artifact_digest) DO UPDATE
SET
    state = EXCLUDED.state,
    public_oci_reference = EXCLUDED.public_oci_reference,
    download_url = EXCLUDED.download_url,
    policy_ref = EXCLUDED.policy_ref,
    promoted_by = EXCLUDED.promoted_by,
    reason = EXCLUDED.reason,
    published_at = EXCLUDED.published_at,
    superseded_at = NULL,
    superseded_by_digest = '',
    updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: ListChannelTargets :many
SELECT *
FROM distribution_channel_targets
WHERE package_name = @package_name
  AND channel_name = @channel_name
  AND (@platform_os::text = '' OR platform_os = @platform_os)
  AND (@platform_arch::text = '' OR platform_arch = @platform_arch)
  AND (@flavor::text = '' OR flavor = @flavor)
ORDER BY published_at DESC, target_id DESC
LIMIT @limit_count;
