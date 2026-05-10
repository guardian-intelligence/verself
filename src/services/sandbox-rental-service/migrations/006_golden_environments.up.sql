-- Golden Environments for GitHub runner acceleration. The first durable
-- component is the GitHub Actions _work tree.

CREATE TABLE job_shapes (
    job_shape_id          UUID        PRIMARY KEY,
    org_id                BIGINT      NOT NULL CHECK (org_id > 0),
    provider              TEXT        NOT NULL CHECK (provider <> ''),
    provider_repository_id BIGINT     NOT NULL CHECK (provider_repository_id > 0),
    workflow_identity     TEXT        NOT NULL DEFAULT '',
    called_workflow_identity TEXT     NOT NULL DEFAULT '',
    job_identity          TEXT        NOT NULL DEFAULT '',
    matrix_key            TEXT        NOT NULL DEFAULT '',
    runner_class          TEXT        NOT NULL CHECK (runner_class <> ''),
    platform_image_id     TEXT        NOT NULL CHECK (platform_image_id <> ''),
    durable_manifest_hash TEXT        NOT NULL CHECK (durable_manifest_hash <> ''),
    checkout_policy_hash  TEXT        NOT NULL CHECK (checkout_policy_hash <> ''),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (
        org_id, provider, provider_repository_id, workflow_identity,
        called_workflow_identity, job_identity, matrix_key, runner_class,
        platform_image_id, durable_manifest_hash, checkout_policy_hash
    )
);

CREATE TABLE golden_scopes (
    golden_scope_id       UUID        PRIMARY KEY,
    org_id                BIGINT      NOT NULL CHECK (org_id > 0),
    provider              TEXT        NOT NULL CHECK (provider <> ''),
    provider_repository_id BIGINT     NOT NULL CHECK (provider_repository_id > 0),
    scope_kind            TEXT        NOT NULL CHECK (scope_kind <> ''),
    scope_ref             TEXT        NOT NULL CHECK (scope_ref <> ''),
    job_shape_id          UUID        NOT NULL REFERENCES job_shapes(job_shape_id) ON DELETE RESTRICT,
    trust_class           TEXT        NOT NULL CHECK (trust_class <> ''),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (
        org_id, provider, provider_repository_id, scope_kind, scope_ref,
        job_shape_id, trust_class
    )
);

CREATE TABLE workspace_operations (
    operation_id          UUID        PRIMARY KEY,
    execution_id          UUID        NOT NULL REFERENCES executions(execution_id) ON DELETE CASCADE,
    attempt_id            UUID        NOT NULL REFERENCES execution_attempts(attempt_id) ON DELETE CASCADE,
    allocation_id         UUID        REFERENCES runner_allocations(allocation_id) ON DELETE SET NULL,
    golden_scope_id       UUID        NOT NULL REFERENCES golden_scopes(golden_scope_id) ON DELETE RESTRICT,
    job_shape_id          UUID        NOT NULL REFERENCES job_shapes(job_shape_id) ON DELETE RESTRICT,
    source_generation_id  UUID,
    source_snapshot_ref   TEXT        NOT NULL DEFAULT '',
    candidate_generation_id UUID      NOT NULL,
    mount_name            TEXT        NOT NULL CHECK (mount_name <> ''),
    mount_path            TEXT        NOT NULL CHECK (mount_path LIKE '/%' AND mount_path <> '/'),
    trust_class           TEXT        NOT NULL CHECK (trust_class <> ''),
    taint_policy          TEXT        NOT NULL CHECK (taint_policy <> ''),
    final_state           TEXT        NOT NULL CHECK (final_state <> ''),
    requested_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    host_accepted_at      TIMESTAMPTZ,
    mounted_at            TIMESTAMPTZ,
    sealed_at             TIMESTAMPTZ,
    result_recorded_at    TIMESTAMPTZ,
    failure_reason        TEXT        NOT NULL DEFAULT '',
    UNIQUE (attempt_id)
);

CREATE TABLE golden_generations (
    golden_generation_id  UUID        PRIMARY KEY,
    golden_scope_id       UUID        NOT NULL REFERENCES golden_scopes(golden_scope_id) ON DELETE RESTRICT,
    operation_id          UUID        NOT NULL REFERENCES workspace_operations(operation_id) ON DELETE RESTRICT,
    source_generation_id  UUID,
    head_sha              TEXT        NOT NULL DEFAULT '',
    tree_hash             TEXT        NOT NULL DEFAULT '',
    provider_run_id       BIGINT      NOT NULL DEFAULT 0,
    provider_job_id       BIGINT      NOT NULL DEFAULT 0,
    result                TEXT        NOT NULL CHECK (result <> ''),
    taint_class           TEXT        NOT NULL CHECK (taint_class <> ''),
    promotion_eligible    BOOLEAN     NOT NULL DEFAULT false,
    state                 TEXT        NOT NULL CHECK (state <> ''),
    zfs_snapshot_ref      TEXT        NOT NULL CHECK (zfs_snapshot_ref <> ''),
    used_bytes            BIGINT      NOT NULL DEFAULT 0 CHECK (used_bytes >= 0),
    written_bytes         BIGINT      NOT NULL DEFAULT 0 CHECK (written_bytes >= 0),
    sealed_at             TIMESTAMPTZ NOT NULL,
    committed_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at            TIMESTAMPTZ,
    UNIQUE (operation_id),
    UNIQUE (zfs_snapshot_ref)
);

ALTER TABLE workspace_operations
    ADD CONSTRAINT workspace_operations_source_generation_fk
    FOREIGN KEY (source_generation_id) REFERENCES golden_generations(golden_generation_id) ON DELETE SET NULL;

ALTER TABLE golden_generations
    ADD CONSTRAINT golden_generations_source_generation_fk
    FOREIGN KEY (source_generation_id) REFERENCES golden_generations(golden_generation_id) ON DELETE SET NULL;

CREATE TABLE durable_components (
    durable_component_id  UUID        PRIMARY KEY,
    golden_generation_id  UUID        NOT NULL REFERENCES golden_generations(golden_generation_id) ON DELETE CASCADE,
    component_kind        TEXT        NOT NULL CHECK (component_kind <> ''),
    component_key         TEXT        NOT NULL CHECK (component_key <> ''),
    guest_mount_path      TEXT        NOT NULL CHECK (guest_mount_path LIKE '/%' AND guest_mount_path <> '/'),
    host_dataset_ref      TEXT        NOT NULL DEFAULT '',
    sealed_snapshot_ref   TEXT        NOT NULL CHECK (sealed_snapshot_ref <> ''),
    filesystem_kind       TEXT        NOT NULL CHECK (filesystem_kind <> ''),
    size_bytes_logical    BIGINT      NOT NULL DEFAULT 0 CHECK (size_bytes_logical >= 0),
    size_bytes_referenced BIGINT      NOT NULL DEFAULT 0 CHECK (size_bytes_referenced >= 0),
    scrub_policy_hash     TEXT        NOT NULL DEFAULT '',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (golden_generation_id, component_kind, component_key)
);

CREATE TABLE golden_current_pointer (
    golden_scope_id       UUID        PRIMARY KEY REFERENCES golden_scopes(golden_scope_id) ON DELETE CASCADE,
    current_generation_id UUID        REFERENCES golden_generations(golden_generation_id) ON DELETE SET NULL,
    promoted_by_operation_id UUID    REFERENCES workspace_operations(operation_id) ON DELETE SET NULL,
    promoted_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE golden_events (
    event_id              UUID        PRIMARY KEY,
    operation_id          UUID        REFERENCES workspace_operations(operation_id) ON DELETE SET NULL,
    golden_scope_id       UUID        REFERENCES golden_scopes(golden_scope_id) ON DELETE SET NULL,
    golden_generation_id  UUID        REFERENCES golden_generations(golden_generation_id) ON DELETE SET NULL,
    execution_id          UUID        REFERENCES executions(execution_id) ON DELETE CASCADE,
    attempt_id            UUID        REFERENCES execution_attempts(attempt_id) ON DELETE CASCADE,
    event_name            TEXT        NOT NULL CHECK (event_name <> ''),
    result                TEXT        NOT NULL DEFAULT '',
    reason                TEXT        NOT NULL DEFAULT '',
    observed_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_golden_scopes_repo
    ON golden_scopes (org_id, provider, provider_repository_id, scope_kind, scope_ref);

CREATE INDEX idx_workspace_operations_attempt
    ON workspace_operations (attempt_id, final_state, requested_at DESC);

CREATE INDEX idx_golden_generations_scope_committed
    ON golden_generations (golden_scope_id, committed_at DESC);
