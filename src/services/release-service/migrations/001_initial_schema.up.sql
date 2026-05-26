CREATE TABLE release_packages (
    package_id UUID PRIMARY KEY,
    org_id TEXT NOT NULL CHECK (length(btrim(org_id)) > 0),
    package_name TEXT NOT NULL CHECK (package_name ~ '^[a-z0-9][a-z0-9._-]*[a-z0-9]$'),
    package_kind TEXT NOT NULL CHECK (package_kind IN ('cli', 'npm', 'container', 'library', 'website', 'image')),
    repo_path TEXT NOT NULL CHECK (length(btrim(repo_path)) > 0),
    bazel_target TEXT NOT NULL CHECK (length(btrim(bazel_target)) > 0),
    owner_team TEXT NOT NULL CHECK (length(btrim(owner_team)) > 0),
    default_version_scheme TEXT NOT NULL CHECK (default_version_scheme IN ('semver', 'pep440', 'debian', 'calendar', 'opaque')),
    visibility TEXT NOT NULL CHECK (visibility IN ('internal', 'private', 'public')),
    policy_ref TEXT NOT NULL CHECK (length(btrim(policy_ref)) > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (org_id, package_name)
);

CREATE INDEX release_packages_org_idx ON release_packages (org_id, package_name);

CREATE TABLE release_versions (
    release_version_id UUID PRIMARY KEY,
    package_id UUID NOT NULL REFERENCES release_packages (package_id) ON DELETE CASCADE,
    canonical_version TEXT NOT NULL CHECK (length(btrim(canonical_version)) > 0),
    version_scheme TEXT NOT NULL CHECK (version_scheme IN ('semver', 'pep440', 'debian', 'calendar', 'opaque')),
    version_kind TEXT NOT NULL CHECK (version_kind IN ('stable', 'rc', 'nightly', 'canary')),
    release_sequence BIGINT NOT NULL CHECK (release_sequence >= 0),
    source_repository TEXT NOT NULL CHECK (length(btrim(source_repository)) > 0),
    source_commit TEXT NOT NULL CHECK (length(btrim(source_commit)) > 0),
    source_ref TEXT NOT NULL CHECK (length(btrim(source_ref)) > 0),
    state TEXT NOT NULL CHECK (state IN ('draft', 'submitted', 'verifying', 'verified', 'approval_required', 'approved', 'publishing', 'published', 'rejected', 'denied', 'terminal_failed', 'terminal')),
    lifecycle_status TEXT NOT NULL CHECK (lifecycle_status IN ('active', 'superseded', 'deprecated', 'yanked', 'quarantined', 'revoked')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (package_id, canonical_version),
    UNIQUE (package_id, version_kind, release_sequence)
);

CREATE INDEX release_versions_package_state_idx ON release_versions (package_id, state, updated_at DESC);

CREATE TABLE release_provider_versions (
    provider_version_id UUID PRIMARY KEY,
    release_version_id UUID NOT NULL REFERENCES release_versions (release_version_id) ON DELETE CASCADE,
    provider_kind TEXT NOT NULL CHECK (provider_kind IN ('zot', 'oci', 'npm', 'pypi', 'crates', 'homebrew', 'apt', 'github')),
    provider_package TEXT NOT NULL CHECK (length(btrim(provider_package)) > 0),
    provider_version TEXT NOT NULL CHECK (length(btrim(provider_version)) > 0),
    provider_channel TEXT NOT NULL CHECK (length(btrim(provider_channel)) > 0),
    version_scheme TEXT NOT NULL CHECK (length(btrim(version_scheme)) > 0),
    projection_policy TEXT NOT NULL CHECK (length(btrim(projection_policy)) > 0),
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (provider_kind, provider_package, provider_version)
);

CREATE TABLE release_channels (
    channel_id UUID PRIMARY KEY,
    package_id UUID NOT NULL REFERENCES release_packages (package_id) ON DELETE CASCADE,
    channel_name TEXT NOT NULL CHECK (channel_name ~ '^[a-z0-9][a-z0-9._-]*$'),
    channel_kind TEXT NOT NULL CHECK (channel_kind IN ('stable', 'rc', 'nightly', 'canary')),
    visibility TEXT NOT NULL CHECK (visibility IN ('internal', 'private', 'public')),
    current_release_version_id UUID NOT NULL REFERENCES release_versions (release_version_id),
    policy_ref TEXT NOT NULL CHECK (length(btrim(policy_ref)) > 0),
    updated_by TEXT NOT NULL CHECK (length(btrim(updated_by)) > 0),
    updated_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (package_id, channel_name)
);

CREATE TABLE release_channel_events (
    channel_event_id UUID PRIMARY KEY,
    channel_id UUID NOT NULL REFERENCES release_channels (channel_id) ON DELETE CASCADE,
    from_release_version_id UUID NOT NULL REFERENCES release_versions (release_version_id),
    to_release_version_id UUID NOT NULL REFERENCES release_versions (release_version_id),
    event_kind TEXT NOT NULL CHECK (event_kind IN ('promoted', 'rolled_back', 'disabled')),
    reason_code TEXT NOT NULL CHECK (length(btrim(reason_code)) > 0),
    policy_check_id UUID NOT NULL,
    actor TEXT NOT NULL CHECK (length(btrim(actor)) > 0),
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE release_artifacts (
    artifact_id UUID PRIMARY KEY,
    release_version_id UUID NOT NULL REFERENCES release_versions (release_version_id) ON DELETE CASCADE,
    artifact_role TEXT NOT NULL CHECK (artifact_role IN ('binary', 'archive', 'container', 'sbom', 'metadata')),
    platform_os TEXT NOT NULL CHECK (length(btrim(platform_os)) > 0),
    platform_arch TEXT NOT NULL CHECK (length(btrim(platform_arch)) > 0),
    oci_repository TEXT NOT NULL CHECK (length(btrim(oci_repository)) > 0),
    oci_digest TEXT NOT NULL CHECK (oci_digest ~ '^sha256:[a-f0-9]{64}$'),
    oci_media_type TEXT NOT NULL CHECK (length(btrim(oci_media_type)) > 0),
    oci_size_bytes BIGINT NOT NULL CHECK (oci_size_bytes >= 0),
    registry_url TEXT NOT NULL CHECK (length(btrim(registry_url)) > 0),
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (oci_repository, oci_digest)
);

CREATE INDEX release_artifacts_release_idx ON release_artifacts (release_version_id, artifact_role, platform_os, platform_arch);

CREATE TABLE release_evidence (
    evidence_id UUID PRIMARY KEY,
    release_version_id UUID NOT NULL REFERENCES release_versions (release_version_id) ON DELETE CASCADE,
    artifact_id UUID NOT NULL REFERENCES release_artifacts (artifact_id) ON DELETE CASCADE,
    evidence_kind TEXT NOT NULL CHECK (evidence_kind IN ('cosign_signature', 'slsa_provenance', 'sbom', 'test', 'vsa', 'receipt', 'vex')),
    predicate_type TEXT NOT NULL CHECK (length(btrim(predicate_type)) > 0),
    subject_digest TEXT NOT NULL CHECK (subject_digest ~ '^sha256:[a-f0-9]{64}$'),
    document_digest TEXT NOT NULL CHECK (length(btrim(document_digest)) > 0),
    oci_referrer_digest TEXT NOT NULL CHECK (oci_referrer_digest ~ '^sha256:[a-f0-9]{64}$'),
    verification_state TEXT NOT NULL CHECK (verification_state IN ('pending', 'verified', 'rejected')),
    verified_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (release_version_id, evidence_kind, document_digest)
);

CREATE INDEX release_evidence_release_kind_idx ON release_evidence (release_version_id, evidence_kind, verification_state);

CREATE TABLE release_policy_checks (
    policy_check_id UUID PRIMARY KEY,
    release_version_id UUID NOT NULL REFERENCES release_versions (release_version_id) ON DELETE CASCADE,
    policy_ref TEXT NOT NULL CHECK (length(btrim(policy_ref)) > 0),
    policy_digest TEXT NOT NULL CHECK (length(btrim(policy_digest)) > 0),
    decision TEXT NOT NULL CHECK (decision IN ('allowed', 'denied', 'needs_approval')),
    decision_reason TEXT NOT NULL CHECK (length(btrim(decision_reason)) > 0),
    builder_id TEXT NOT NULL CHECK (length(btrim(builder_id)) > 0),
    signer_identity TEXT NOT NULL CHECK (length(btrim(signer_identity)) > 0),
    source_commit TEXT NOT NULL CHECK (length(btrim(source_commit)) > 0),
    checked_at TIMESTAMPTZ NOT NULL,
    checked_by TEXT NOT NULL CHECK (length(btrim(checked_by)) > 0),
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX release_policy_checks_release_idx ON release_policy_checks (release_version_id, checked_at DESC);

CREATE TABLE release_install_options (
    install_option_id UUID PRIMARY KEY,
    release_version_id UUID NOT NULL REFERENCES release_versions (release_version_id) ON DELETE CASCADE,
    option_kind TEXT NOT NULL CHECK (option_kind IN ('source', 'oci', 'third_party')),
    status TEXT NOT NULL CHECK (status IN ('active', 'deferred', 'disabled')),
    display_order INTEGER NOT NULL CHECK (display_order > 0),
    command_template TEXT NOT NULL CHECK (length(btrim(command_template)) > 0),
    verification_template TEXT NOT NULL CHECK (length(btrim(verification_template)) > 0),
    oci_repository TEXT NOT NULL DEFAULT '',
    oci_digest TEXT NOT NULL DEFAULT '',
    provider_kind TEXT NOT NULL DEFAULT '',
    provider_ref TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (release_version_id, option_kind, provider_kind)
);

CREATE INDEX release_install_options_release_idx ON release_install_options (release_version_id, display_order);

CREATE TABLE release_publications (
    publication_id UUID PRIMARY KEY,
    release_version_id UUID NOT NULL REFERENCES release_versions (release_version_id) ON DELETE CASCADE,
    provider_kind TEXT NOT NULL CHECK (provider_kind IN ('zot', 'oci', 'npm', 'pypi', 'crates', 'homebrew', 'apt', 'github', 'newsroom')),
    provider_package TEXT NOT NULL CHECK (length(btrim(provider_package)) > 0),
    provider_channel TEXT NOT NULL CHECK (length(btrim(provider_channel)) > 0),
    requested_by TEXT NOT NULL CHECK (length(btrim(requested_by)) > 0),
    state TEXT NOT NULL CHECK (state IN ('requested', 'policy_verified', 'approval_required', 'approved', 'dispatching', 'provider_pending', 'provider_verified', 'complete', 'denied', 'cancelled', 'retryable_failed', 'terminal_failed')),
    idempotency_key TEXT NOT NULL CHECK (length(btrim(idempotency_key)) > 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (provider_kind, provider_package, idempotency_key)
);

CREATE INDEX release_publications_due_idx ON release_publications (state, updated_at, publication_id);

CREATE TABLE release_publication_receipts (
    receipt_id UUID PRIMARY KEY,
    publication_id UUID NOT NULL REFERENCES release_publications (publication_id) ON DELETE CASCADE,
    provider_kind TEXT NOT NULL CHECK (length(btrim(provider_kind)) > 0),
    provider_resource_url TEXT NOT NULL CHECK (length(btrim(provider_resource_url)) > 0),
    provider_version TEXT NOT NULL CHECK (length(btrim(provider_version)) > 0),
    provider_digest TEXT NOT NULL CHECK (length(btrim(provider_digest)) > 0),
    receipt_digest TEXT NOT NULL CHECK (length(btrim(receipt_digest)) > 0),
    reconciled_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (publication_id, receipt_digest)
);

-- SQL derived from River v0.34.0 riverdriver/riverpgxv5/migration/main
-- (MPL-2.0). Keep in lockstep with go.mod's River pin.

CREATE TABLE river_migration (
    line       TEXT        NOT NULL,
    version    BIGINT      NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT line_length CHECK (char_length(line) > 0 AND char_length(line) < 128),
    CONSTRAINT version_gte_1 CHECK (version >= 1),
    PRIMARY KEY (line, version)
);

CREATE TYPE river_job_state AS ENUM (
    'available',
    'cancelled',
    'completed',
    'discarded',
    'pending',
    'retryable',
    'running',
    'scheduled'
);

CREATE TABLE river_job (
    id            BIGSERIAL       PRIMARY KEY,
    state         river_job_state NOT NULL DEFAULT 'available',
    attempt       SMALLINT        NOT NULL DEFAULT 0,
    max_attempts  SMALLINT        NOT NULL,
    attempted_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    finalized_at  TIMESTAMPTZ,
    scheduled_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    priority      SMALLINT        NOT NULL DEFAULT 1,
    args          JSONB           NOT NULL,
    attempted_by  TEXT[],
    errors        JSONB[],
    kind          TEXT            NOT NULL,
    metadata      JSONB           NOT NULL DEFAULT '{}',
    queue         TEXT            NOT NULL DEFAULT 'default',
    tags          VARCHAR(255)[]  NOT NULL DEFAULT '{}',
    unique_key    BYTEA,
    unique_states BIT(8),
    CONSTRAINT finalized_or_finalized_at_null CHECK (
        (finalized_at IS NULL AND state NOT IN ('cancelled', 'completed', 'discarded')) OR
        (finalized_at IS NOT NULL AND state IN ('cancelled', 'completed', 'discarded'))
    ),
    CONSTRAINT max_attempts_is_positive CHECK (max_attempts > 0),
    CONSTRAINT priority_in_range CHECK (priority >= 1 AND priority <= 4),
    CONSTRAINT queue_length CHECK (char_length(queue) > 0 AND char_length(queue) < 128),
    CONSTRAINT kind_length CHECK (char_length(kind) > 0 AND char_length(kind) < 128)
);

CREATE INDEX river_job_kind ON river_job USING btree (kind);
CREATE INDEX river_job_state_and_finalized_at_index
    ON river_job USING btree (state, finalized_at) WHERE finalized_at IS NOT NULL;
CREATE INDEX river_job_prioritized_fetching_index
    ON river_job USING btree (state, queue, priority, scheduled_at, id);
CREATE INDEX river_job_args_index ON river_job USING GIN (args);
CREATE INDEX river_job_metadata_index ON river_job USING GIN (metadata);

CREATE OR REPLACE FUNCTION river_job_state_in_bitmask(bitmask BIT(8), state river_job_state)
RETURNS boolean
LANGUAGE SQL
IMMUTABLE
AS $$
    SELECT CASE state
        WHEN 'available' THEN get_bit(bitmask, 7)
        WHEN 'cancelled' THEN get_bit(bitmask, 6)
        WHEN 'completed' THEN get_bit(bitmask, 5)
        WHEN 'discarded' THEN get_bit(bitmask, 4)
        WHEN 'pending'   THEN get_bit(bitmask, 3)
        WHEN 'retryable' THEN get_bit(bitmask, 2)
        WHEN 'running'   THEN get_bit(bitmask, 1)
        WHEN 'scheduled' THEN get_bit(bitmask, 0)
        ELSE 0
    END = 1;
$$;

CREATE UNIQUE INDEX river_job_unique_idx ON river_job (unique_key)
    WHERE unique_key IS NOT NULL
      AND unique_states IS NOT NULL
      AND river_job_state_in_bitmask(unique_states, state);

CREATE UNLOGGED TABLE river_leader (
    elected_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    leader_id  TEXT        NOT NULL,
    name       TEXT        PRIMARY KEY NOT NULL DEFAULT 'default',
    CONSTRAINT name_length CHECK (name = 'default'),
    CONSTRAINT leader_id_length CHECK (char_length(leader_id) > 0 AND char_length(leader_id) < 128)
);

CREATE TABLE river_queue (
    name       TEXT        PRIMARY KEY NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    metadata   JSONB       NOT NULL DEFAULT '{}'::jsonb,
    paused_at  TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE UNLOGGED TABLE river_client (
    id         TEXT        PRIMARY KEY NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    metadata   JSONB       NOT NULL DEFAULT '{}',
    paused_at  TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT name_length CHECK (char_length(id) > 0 AND char_length(id) < 128)
);

CREATE UNLOGGED TABLE river_client_queue (
    river_client_id    TEXT        NOT NULL REFERENCES river_client (id) ON DELETE CASCADE,
    name               TEXT        NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    max_workers        BIGINT      NOT NULL DEFAULT 0,
    metadata           JSONB       NOT NULL DEFAULT '{}',
    num_jobs_completed BIGINT      NOT NULL DEFAULT 0,
    num_jobs_running   BIGINT      NOT NULL DEFAULT 0,
    updated_at         TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (river_client_id, name),
    CONSTRAINT name_length CHECK (char_length(name) > 0 AND char_length(name) < 128),
    CONSTRAINT num_jobs_completed_zero_or_positive CHECK (num_jobs_completed >= 0),
    CONSTRAINT num_jobs_running_zero_or_positive CHECK (num_jobs_running >= 0)
);

INSERT INTO river_migration (line, version) VALUES
    ('main', 1),
    ('main', 2),
    ('main', 3),
    ('main', 4),
    ('main', 5),
    ('main', 6);
