CREATE TABLE distribution_artifacts (
    artifact_id UUID PRIMARY KEY,
    package_name TEXT NOT NULL CHECK (package_name ~ '^[a-z0-9][a-z0-9._-]*[a-z0-9]$'),
    package_version TEXT NOT NULL CHECK (length(btrim(package_version)) > 0),
    channel_name TEXT NOT NULL CHECK (channel_name ~ '^[a-z][a-z0-9._-]*$'),
    platform_os TEXT NOT NULL CHECK (length(btrim(platform_os)) > 0),
    platform_arch TEXT NOT NULL CHECK (length(btrim(platform_arch)) > 0),
    origin_registry_url TEXT NOT NULL CHECK (length(btrim(origin_registry_url)) > 0),
    public_registry_url TEXT NOT NULL CHECK (length(btrim(public_registry_url)) > 0),
    oci_repository TEXT NOT NULL CHECK (length(btrim(oci_repository)) > 0),
    oci_digest TEXT NOT NULL CHECK (oci_digest ~ '^sha256:[a-f0-9]{64}$'),
    oci_media_type TEXT NOT NULL CHECK (length(btrim(oci_media_type)) > 0),
    oci_size_bytes BIGINT NOT NULL CHECK (oci_size_bytes >= 0),
    public_oci_reference TEXT NOT NULL CHECK (length(btrim(public_oci_reference)) > 0),
    builder_id TEXT NOT NULL CHECK (length(btrim(builder_id)) > 0),
    signer_identity TEXT NOT NULL CHECK (length(btrim(signer_identity)) > 0),
    source_repository TEXT NOT NULL CHECK (length(btrim(source_repository)) > 0),
    source_commit TEXT NOT NULL CHECK (length(btrim(source_commit)) > 0),
    source_ref TEXT NOT NULL CHECK (length(btrim(source_ref)) > 0),
    bazel_target TEXT NOT NULL CHECK (length(btrim(bazel_target)) > 0),
    policy_ref TEXT NOT NULL CHECK (length(btrim(policy_ref)) > 0),
    state TEXT NOT NULL CHECK (state IN ('zot_pushed', 'submitted', 'verifying', 'admitted', 'available', 'rejected', 'superseded', 'quarantined', 'retained', 'expired', 'gc_eligible', 'deleted')),
    verification_decision TEXT NOT NULL CHECK (verification_decision IN ('pending', 'allowed', 'denied')),
    verification_reason TEXT NOT NULL CHECK (length(btrim(verification_reason)) > 0),
    retention_class TEXT NOT NULL CHECK (retention_class IN ('canary', 'nightly', 'release', 'stable', 'security_retained')),
    replication_state TEXT NOT NULL CHECK (replication_state IN ('pending', 'available', 'repairing', 'failed')),
    submitted_by TEXT NOT NULL CHECK (length(btrim(submitted_by)) > 0),
    admitted_at TIMESTAMPTZ NOT NULL,
    available_at TIMESTAMPTZ NOT NULL,
    quarantined_at TIMESTAMPTZ,
    quarantine_reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (oci_repository, oci_digest),
    UNIQUE (package_name, platform_os, platform_arch, oci_digest)
);

CREATE INDEX distribution_artifacts_package_platform_idx
    ON distribution_artifacts (package_name, platform_os, platform_arch, state, updated_at DESC);

CREATE TABLE distribution_artifact_evidence (
    evidence_id UUID PRIMARY KEY,
    artifact_id UUID NOT NULL REFERENCES distribution_artifacts (artifact_id) ON DELETE CASCADE,
    evidence_kind TEXT NOT NULL CHECK (length(btrim(evidence_kind)) > 0),
    predicate_type TEXT NOT NULL CHECK (length(btrim(predicate_type)) > 0),
    subject_digest TEXT NOT NULL CHECK (subject_digest ~ '^sha256:[a-f0-9]{64}$'),
    document_digest TEXT NOT NULL CHECK (length(btrim(document_digest)) > 0),
    oci_referrer_digest TEXT NOT NULL CHECK (oci_referrer_digest ~ '^sha256:[a-f0-9]{64}$'),
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (artifact_id, evidence_kind, document_digest)
);

CREATE INDEX distribution_artifact_evidence_artifact_idx
    ON distribution_artifact_evidence (artifact_id, evidence_kind);

CREATE TABLE distribution_channel_targets (
    target_id UUID PRIMARY KEY,
    package_name TEXT NOT NULL CHECK (package_name ~ '^[a-z0-9][a-z0-9._-]*[a-z0-9]$'),
    channel_name TEXT NOT NULL CHECK (channel_name ~ '^[a-z][a-z0-9._-]*$'),
    platform_os TEXT NOT NULL CHECK (length(btrim(platform_os)) > 0),
    platform_arch TEXT NOT NULL CHECK (length(btrim(platform_arch)) > 0),
    artifact_id UUID NOT NULL REFERENCES distribution_artifacts (artifact_id),
    artifact_digest TEXT NOT NULL CHECK (artifact_digest ~ '^sha256:[a-f0-9]{64}$'),
    package_version TEXT NOT NULL CHECK (length(btrim(package_version)) > 0),
    state TEXT NOT NULL CHECK (state IN ('draft_target', 'policy_checked', 'tuf_signed', 'published', 'superseded', 'denied', 'rollback_target_published')),
    public_oci_reference TEXT NOT NULL CHECK (length(btrim(public_oci_reference)) > 0),
    download_url TEXT NOT NULL CHECK (length(btrim(download_url)) > 0),
    tuf_metadata_url TEXT NOT NULL CHECK (length(btrim(tuf_metadata_url)) > 0),
    tuf_targets_version BIGINT NOT NULL CHECK (tuf_targets_version > 0),
    tuf_snapshot_version BIGINT NOT NULL CHECK (tuf_snapshot_version > 0),
    tuf_timestamp_version BIGINT NOT NULL CHECK (tuf_timestamp_version > 0),
    policy_ref TEXT NOT NULL CHECK (length(btrim(policy_ref)) > 0),
    promoted_by TEXT NOT NULL CHECK (length(btrim(promoted_by)) > 0),
    reason TEXT NOT NULL CHECK (length(btrim(reason)) > 0),
    published_at TIMESTAMPTZ NOT NULL,
    superseded_at TIMESTAMPTZ,
    superseded_by_digest TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (package_name, channel_name, platform_os, platform_arch, artifact_digest)
);

CREATE UNIQUE INDEX distribution_channel_targets_current_idx
    ON distribution_channel_targets (package_name, channel_name, platform_os, platform_arch)
    WHERE state IN ('published', 'rollback_target_published');

CREATE INDEX distribution_channel_targets_history_idx
    ON distribution_channel_targets (package_name, channel_name, platform_os, platform_arch, published_at DESC);

CREATE TABLE distribution_tuf_metadata (
    tuf_metadata_id UUID PRIMARY KEY,
    package_name TEXT NOT NULL CHECK (package_name ~ '^[a-z0-9][a-z0-9._-]*[a-z0-9]$'),
    role TEXT NOT NULL CHECK (role IN ('root', 'timestamp', 'snapshot', 'targets')),
    version BIGINT NOT NULL CHECK (version > 0),
    body BYTEA NOT NULL,
    body_sha256 TEXT NOT NULL CHECK (body_sha256 ~ '^sha256:[a-f0-9]{64}$'),
    body_length BIGINT NOT NULL CHECK (body_length > 0),
    signed_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    UNIQUE (package_name, role, version)
);

CREATE INDEX distribution_tuf_metadata_latest_idx
    ON distribution_tuf_metadata (package_name, role, version DESC);

CREATE TABLE distribution_idempotency_keys (
    scope TEXT NOT NULL CHECK (length(btrim(scope)) > 0),
    idempotency_key TEXT NOT NULL CHECK (length(btrim(idempotency_key)) > 0),
    payload_sha256 TEXT NOT NULL CHECK (payload_sha256 ~ '^sha256:[a-f0-9]{64}$'),
    resource_kind TEXT NOT NULL CHECK (length(btrim(resource_kind)) > 0),
    resource_id TEXT NOT NULL CHECK (length(btrim(resource_id)) > 0),
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope, idempotency_key)
);
