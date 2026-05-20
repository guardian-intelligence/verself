-- Golden VM manifests couple Firecracker snapshot artifacts to exact root and
-- durable zvol generations. The migration is idempotent so early pre-release
-- sites can be wiped or advanced without hand-editing bootstrap state.

ALTER TABLE durable_generation
    ADD COLUMN IF NOT EXISTS zfs_snapshot_guid TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS golden_vm_operation (
    operation_id                    UUID        PRIMARY KEY,
    execution_id                    UUID        NOT NULL REFERENCES executions(execution_id) ON DELETE CASCADE,
    attempt_id                      UUID        NOT NULL REFERENCES execution_attempts(attempt_id) ON DELETE CASCADE,
    allocation_id                   UUID        REFERENCES runner_allocations(allocation_id) ON DELETE SET NULL,
    org_id                          TEXT        NOT NULL CHECK (org_id <> ''),
    repository_id                   BIGINT      NOT NULL CHECK (repository_id > 0),
    provider                        TEXT        NOT NULL,
    provider_repository_id          BIGINT      NOT NULL CHECK (provider_repository_id > 0),
    scope_kind                      TEXT        NOT NULL,
    scope_ref                       TEXT        NOT NULL,
    job_shape_id                    UUID        NOT NULL REFERENCES job_shape(job_shape_id) ON DELETE RESTRICT,
    trust_class                     TEXT        NOT NULL,
    source_generation_set_hash      TEXT        NOT NULL DEFAULT '',
    candidate_golden_vm_snapshot_id UUID        NOT NULL,
    provider_run_id                 BIGINT      NOT NULL DEFAULT 0,
    provider_run_attempt            BIGINT      NOT NULL DEFAULT 0,
    provider_job_id                 BIGINT      NOT NULL DEFAULT 0,
    head_sha                        TEXT        NOT NULL DEFAULT '',
    requested_at                    TIMESTAMPTZ NOT NULL DEFAULT now(),
    checkpoint_started_at           TIMESTAMPTZ,
    checkpointed_at                 TIMESTAMPTZ,
    result_recorded_at              TIMESTAMPTZ,
    final_state                     TEXT        NOT NULL CHECK (final_state IN ('requested', 'checkpointed', 'committed', 'skipped', 'failed')),
    failure_reason                  TEXT        NOT NULL DEFAULT '',
    UNIQUE (candidate_golden_vm_snapshot_id)
);

ALTER TABLE golden_vm_operation
    ADD COLUMN IF NOT EXISTS provider_run_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS provider_run_attempt BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS provider_job_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS head_sha TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_golden_vm_operation_attempt
    ON golden_vm_operation (attempt_id, final_state, requested_at DESC);
CREATE INDEX IF NOT EXISTS idx_golden_vm_operation_scope
    ON golden_vm_operation (org_id, provider_repository_id, scope_kind, scope_ref, job_shape_id, trust_class, requested_at DESC);

CREATE TABLE IF NOT EXISTS golden_vm_snapshot (
    golden_vm_snapshot_id       UUID        PRIMARY KEY,
    operation_id                UUID        NOT NULL REFERENCES golden_vm_operation(operation_id) ON DELETE RESTRICT,
    org_id                      TEXT        NOT NULL CHECK (org_id <> ''),
    repository_id               BIGINT      NOT NULL CHECK (repository_id > 0),
    provider                    TEXT        NOT NULL,
    provider_repository_id      BIGINT      NOT NULL CHECK (provider_repository_id > 0),
    scope_kind                  TEXT        NOT NULL,
    scope_ref                   TEXT        NOT NULL,
    job_shape_id                UUID        NOT NULL REFERENCES job_shape(job_shape_id) ON DELETE RESTRICT,
    trust_class                 TEXT        NOT NULL,
    generation_set_hash         TEXT        NOT NULL,
    root_snapshot_ref           TEXT        NOT NULL,
    root_snapshot_guid          TEXT        NOT NULL DEFAULT '',
    snapshot_key                TEXT        NOT NULL,
    vmstate_artifact_ref        TEXT        NOT NULL,
    memory_artifact_ref         TEXT        NOT NULL,
    state_bytes                 BIGINT      NOT NULL DEFAULT 0,
    memory_bytes                BIGINT      NOT NULL DEFAULT 0,
    drive_manifest_hash         TEXT        NOT NULL DEFAULT '',
    mount_manifest_hash         TEXT        NOT NULL DEFAULT '',
    firecracker_abi_hash        TEXT        NOT NULL DEFAULT '',
    host_abi_hash               TEXT        NOT NULL DEFAULT '',
    network_model_hash          TEXT        NOT NULL DEFAULT '',
    vsock_model_hash            TEXT        NOT NULL DEFAULT '',
    clock_model_hash            TEXT        NOT NULL DEFAULT '',
    vmproto_version             INTEGER     NOT NULL DEFAULT 0,
    after_restore_hook_version  TEXT        NOT NULL DEFAULT '',
    before_snapshot_hook_version TEXT       NOT NULL DEFAULT '',
    warm_profile_hash           TEXT        NOT NULL DEFAULT '',
    vcpus                       INTEGER     NOT NULL DEFAULT 0,
    memory_mib                  INTEGER     NOT NULL DEFAULT 0,
    provider_run_id             BIGINT      NOT NULL DEFAULT 0,
    provider_run_attempt        BIGINT      NOT NULL DEFAULT 0,
    provider_job_id             BIGINT      NOT NULL DEFAULT 0,
    head_sha                    TEXT        NOT NULL DEFAULT '',
    tree_hash                   TEXT        NOT NULL DEFAULT '',
    state                       TEXT        NOT NULL CHECK (state IN ('candidate', 'current', 'retained', 'invalidated', 'prunable', 'pruned')),
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at                  TIMESTAMPTZ,
    pruned_at                   TIMESTAMPTZ,
    UNIQUE (snapshot_key)
);

ALTER TABLE golden_vm_snapshot
    ADD COLUMN IF NOT EXISTS pruned_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_golden_vm_snapshot_scope
    ON golden_vm_snapshot (org_id, provider_repository_id, scope_kind, scope_ref, job_shape_id, trust_class, state, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_golden_vm_snapshot_run
    ON golden_vm_snapshot (provider_run_id, provider_run_attempt, provider_job_id, head_sha);
CREATE INDEX IF NOT EXISTS idx_golden_vm_snapshot_retention
    ON golden_vm_snapshot (state, expires_at, created_at);

CREATE TABLE IF NOT EXISTS golden_vm_snapshot_generation (
    golden_vm_snapshot_id UUID    NOT NULL REFERENCES golden_vm_snapshot(golden_vm_snapshot_id) ON DELETE CASCADE,
    durable_scope_id      UUID    NOT NULL REFERENCES durable_scope(durable_scope_id) ON DELETE RESTRICT,
    durable_generation_id UUID    NOT NULL REFERENCES durable_generation(durable_generation_id) ON DELETE RESTRICT,
    cache_name            TEXT    NOT NULL CHECK (cache_name <> ''),
    zfs_snapshot_ref      TEXT    NOT NULL,
    zfs_snapshot_guid     TEXT    NOT NULL DEFAULT '',
    drive_id              TEXT    NOT NULL,
    mount_path            TEXT    NOT NULL,
    bind_paths_json       JSONB   NOT NULL DEFAULT '[]'::jsonb,
    fs_type               TEXT    NOT NULL DEFAULT 'ext4',
    read_only             BOOLEAN NOT NULL DEFAULT false,
    required              BOOLEAN NOT NULL DEFAULT false,
    sort_order            INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (golden_vm_snapshot_id, durable_scope_id)
);

CREATE INDEX IF NOT EXISTS idx_golden_vm_snapshot_generation_generation
    ON golden_vm_snapshot_generation (durable_generation_id);

CREATE TABLE IF NOT EXISTS golden_vm_current_pointer (
    org_id                        TEXT        NOT NULL CHECK (org_id <> ''),
    repository_id                 BIGINT      NOT NULL CHECK (repository_id > 0),
    provider                      TEXT        NOT NULL,
    provider_repository_id        BIGINT      NOT NULL CHECK (provider_repository_id > 0),
    scope_kind                    TEXT        NOT NULL,
    scope_ref                     TEXT        NOT NULL,
    job_shape_id                  UUID        NOT NULL REFERENCES job_shape(job_shape_id) ON DELETE RESTRICT,
    trust_class                   TEXT        NOT NULL,
    current_golden_vm_snapshot_id UUID        REFERENCES golden_vm_snapshot(golden_vm_snapshot_id) ON DELETE SET NULL,
    promoted_by_operation_id      UUID        REFERENCES golden_vm_operation(operation_id) ON DELETE SET NULL,
    promoted_at                   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, repository_id, provider, provider_repository_id, scope_kind, scope_ref, job_shape_id, trust_class)
);
