-- name: Ping :one
SELECT 1::int;

-- name: CreatePackage :one
INSERT INTO release_packages (
    package_id,
    org_id,
    package_name,
    package_kind,
    repo_path,
    bazel_target,
    owner_team,
    default_version_scheme,
    visibility,
    policy_ref,
    created_at,
    updated_at
) VALUES (
    @package_id,
    @org_id,
    @package_name,
    @package_kind,
    @repo_path,
    @bazel_target,
    @owner_team,
    @default_version_scheme,
    @visibility,
    @policy_ref,
    @created_at,
    @updated_at
)
RETURNING *;

-- name: GetPackage :one
SELECT *
FROM release_packages
WHERE package_id = @package_id;

-- name: GetPackageByName :one
SELECT *
FROM release_packages
WHERE org_id = @org_id AND package_name = @package_name;

-- name: CreateReleaseVersion :one
INSERT INTO release_versions (
    release_version_id,
    package_id,
    canonical_version,
    version_scheme,
    version_kind,
    release_sequence,
    source_repository,
    source_commit,
    source_ref,
    state,
    lifecycle_status,
    created_at,
    updated_at
) VALUES (
    @release_version_id,
    @package_id,
    @canonical_version,
    @version_scheme,
    @version_kind,
    @release_sequence,
    @source_repository,
    @source_commit,
    @source_ref,
    @state,
    @lifecycle_status,
    @created_at,
    @updated_at
)
RETURNING *;

-- name: GetReleaseVersion :one
SELECT *
FROM release_versions
WHERE release_version_id = @release_version_id;

-- name: GetReleaseContext :one
SELECT
    rp.package_id,
    rp.org_id,
    rp.package_name,
    rp.package_kind,
    rp.repo_path,
    rp.bazel_target,
    rp.owner_team,
    rp.default_version_scheme,
    rp.visibility,
    rp.policy_ref,
    rp.created_at AS package_created_at,
    rp.updated_at AS package_updated_at,
    rv.release_version_id,
    rv.canonical_version,
    rv.version_scheme,
    rv.version_kind,
    rv.release_sequence,
    rv.source_repository,
    rv.source_commit,
    rv.source_ref,
    rv.state,
    rv.lifecycle_status,
    rv.created_at AS release_created_at,
    rv.updated_at AS release_updated_at
FROM release_versions rv
JOIN release_packages rp ON rp.package_id = rv.package_id
WHERE rv.release_version_id = @release_version_id;

-- name: UpdateReleaseState :one
UPDATE release_versions
SET state = @state, updated_at = @updated_at
WHERE release_version_id = @release_version_id
RETURNING *;

-- name: CreateProviderVersion :one
INSERT INTO release_provider_versions (
    provider_version_id,
    release_version_id,
    provider_kind,
    provider_package,
    provider_version,
    provider_channel,
    version_scheme,
    projection_policy,
    created_at
) VALUES (
    @provider_version_id,
    @release_version_id,
    @provider_kind,
    @provider_package,
    @provider_version,
    @provider_channel,
    @version_scheme,
    @projection_policy,
    @created_at
)
RETURNING *;

-- name: CreateArtifact :one
INSERT INTO release_artifacts (
    artifact_id,
    release_version_id,
    artifact_role,
    platform_os,
    platform_arch,
    oci_repository,
    oci_digest,
    oci_media_type,
    oci_size_bytes,
    registry_url,
    created_at
) VALUES (
    @artifact_id,
    @release_version_id,
    @artifact_role,
    @platform_os,
    @platform_arch,
    @oci_repository,
    @oci_digest,
    @oci_media_type,
    @oci_size_bytes,
    @registry_url,
    @created_at
)
RETURNING *;

-- name: GetArtifact :one
SELECT *
FROM release_artifacts
WHERE artifact_id = @artifact_id;

-- name: ListArtifactsForRelease :many
SELECT *
FROM release_artifacts
WHERE release_version_id = @release_version_id
ORDER BY artifact_role, platform_os, platform_arch, artifact_id;

-- name: GetPrimaryArtifactForRelease :one
SELECT *
FROM release_artifacts
WHERE release_version_id = @release_version_id
ORDER BY
    CASE artifact_role
        WHEN 'archive' THEN 0
        WHEN 'binary' THEN 1
        WHEN 'container' THEN 2
        ELSE 3
    END,
    platform_os,
    platform_arch,
    artifact_id
LIMIT 1;

-- name: CreateEvidence :one
INSERT INTO release_evidence (
    evidence_id,
    release_version_id,
    artifact_id,
    evidence_kind,
    predicate_type,
    subject_digest,
    document_digest,
    oci_referrer_digest,
    verification_state,
    verified_at,
    created_at
) VALUES (
    @evidence_id,
    @release_version_id,
    @artifact_id,
    @evidence_kind,
    @predicate_type,
    @subject_digest,
    @document_digest,
    @oci_referrer_digest,
    @verification_state,
    @verified_at,
    @created_at
)
RETURNING *;

-- name: CountVerifiedEvidenceByKind :one
SELECT count(*)::bigint
FROM release_evidence
WHERE release_version_id = @release_version_id
  AND evidence_kind = @evidence_kind
  AND verification_state = 'verified';

-- name: CreatePolicyCheck :one
INSERT INTO release_policy_checks (
    policy_check_id,
    release_version_id,
    policy_ref,
    policy_digest,
    decision,
    decision_reason,
    builder_id,
    signer_identity,
    source_commit,
    checked_at,
    checked_by,
    created_at
) VALUES (
    @policy_check_id,
    @release_version_id,
    @policy_ref,
    @policy_digest,
    @decision,
    @decision_reason,
    @builder_id,
    @signer_identity,
    @source_commit,
    @checked_at,
    @checked_by,
    @created_at
)
RETURNING *;

-- name: LatestAllowedPolicyCheck :one
SELECT *
FROM release_policy_checks
WHERE release_version_id = @release_version_id
  AND decision = 'allowed'
ORDER BY checked_at DESC, policy_check_id DESC
LIMIT 1;

-- name: CreateInstallOption :one
INSERT INTO release_install_options (
    install_option_id,
    release_version_id,
    option_kind,
    status,
    display_order,
    command_template,
    verification_template,
    oci_repository,
    oci_digest,
    provider_kind,
    provider_ref,
    created_at,
    updated_at
) VALUES (
    @install_option_id,
    @release_version_id,
    @option_kind,
    @status,
    @display_order,
    @command_template,
    @verification_template,
    @oci_repository,
    @oci_digest,
    @provider_kind,
    @provider_ref,
    @created_at,
    @updated_at
)
ON CONFLICT (release_version_id, option_kind, provider_kind) DO UPDATE
SET
    status = EXCLUDED.status,
    display_order = EXCLUDED.display_order,
    command_template = EXCLUDED.command_template,
    verification_template = EXCLUDED.verification_template,
    oci_repository = EXCLUDED.oci_repository,
    oci_digest = EXCLUDED.oci_digest,
    provider_ref = EXCLUDED.provider_ref,
    updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: ListInstallOptions :many
SELECT *
FROM release_install_options
WHERE release_version_id = @release_version_id
ORDER BY display_order, option_kind, provider_kind;

-- name: GetChannelByPackageAndName :one
SELECT *
FROM release_channels
WHERE package_id = @package_id AND channel_name = @channel_name;

-- name: UpsertChannel :one
INSERT INTO release_channels (
    channel_id,
    package_id,
    channel_name,
    channel_kind,
    visibility,
    current_release_version_id,
    policy_ref,
    updated_by,
    updated_at,
    created_at
) VALUES (
    @channel_id,
    @package_id,
    @channel_name,
    @channel_kind,
    @visibility,
    @current_release_version_id,
    @policy_ref,
    @updated_by,
    @updated_at,
    @created_at
)
ON CONFLICT (package_id, channel_name) DO UPDATE
SET
    channel_kind = EXCLUDED.channel_kind,
    visibility = EXCLUDED.visibility,
    current_release_version_id = EXCLUDED.current_release_version_id,
    policy_ref = EXCLUDED.policy_ref,
    updated_by = EXCLUDED.updated_by,
    updated_at = EXCLUDED.updated_at
RETURNING *;

-- name: CreateChannelEvent :one
INSERT INTO release_channel_events (
    channel_event_id,
    channel_id,
    from_release_version_id,
    to_release_version_id,
    event_kind,
    reason_code,
    policy_check_id,
    actor,
    created_at
) VALUES (
    @channel_event_id,
    @channel_id,
    @from_release_version_id,
    @to_release_version_id,
    @event_kind,
    @reason_code,
    @policy_check_id,
    @actor,
    @created_at
)
RETURNING *;

-- name: CreatePublication :one
WITH inserted AS (
    INSERT INTO release_publications (
        publication_id,
        release_version_id,
        provider_kind,
        provider_package,
        provider_channel,
        requested_by,
        state,
        idempotency_key,
        created_at,
        updated_at
    ) VALUES (
        @publication_id,
        @release_version_id,
        @provider_kind,
        @provider_package,
        @provider_channel,
        @requested_by,
        @state,
        @idempotency_key,
        @created_at,
        @updated_at
    )
    ON CONFLICT (provider_kind, provider_package, idempotency_key) DO NOTHING
    RETURNING *
)
SELECT * FROM inserted
UNION ALL
SELECT *
FROM release_publications
WHERE provider_kind = @provider_kind
  AND provider_package = @provider_package
  AND idempotency_key = @idempotency_key
LIMIT 1;

-- name: GetPublication :one
SELECT *
FROM release_publications
WHERE publication_id = @publication_id;

-- name: GetPublicationContext :one
SELECT
    p.publication_id,
    p.release_version_id,
    p.provider_kind,
    p.provider_package,
    p.provider_channel,
    p.requested_by,
    p.state AS publication_state,
    p.idempotency_key,
    p.created_at AS publication_created_at,
    p.updated_at AS publication_updated_at,
    rv.package_id,
    rv.canonical_version,
    rv.version_kind,
    rv.state AS release_state,
    rv.lifecycle_status,
    rp.org_id,
    rp.package_name,
    rp.repo_path,
    rp.bazel_target
FROM release_publications p
JOIN release_versions rv ON rv.release_version_id = p.release_version_id
JOIN release_packages rp ON rp.package_id = rv.package_id
WHERE p.publication_id = @publication_id;

-- name: UpdatePublicationState :one
UPDATE release_publications
SET state = @state, updated_at = @updated_at
WHERE publication_id = @publication_id
RETURNING *;

-- name: ListDuePublications :many
SELECT *
FROM release_publications
WHERE state IN ('approved', 'dispatching', 'provider_pending', 'retryable_failed')
ORDER BY updated_at, publication_id
LIMIT @limit_count;

-- name: CreatePublicationReceipt :one
INSERT INTO release_publication_receipts (
    receipt_id,
    publication_id,
    provider_kind,
    provider_resource_url,
    provider_version,
    provider_digest,
    receipt_digest,
    reconciled_at,
    created_at
) VALUES (
    @receipt_id,
    @publication_id,
    @provider_kind,
    @provider_resource_url,
    @provider_version,
    @provider_digest,
    @receipt_digest,
    @reconciled_at,
    @created_at
)
RETURNING *;

-- name: CountReceiptsForPublication :one
SELECT count(*)::bigint
FROM release_publication_receipts
WHERE publication_id = @publication_id;
