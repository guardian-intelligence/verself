CREATE TABLE volumes (
    volume_id                UUID        PRIMARY KEY,
    org_id                   BIGINT      NOT NULL CHECK (org_id > 0),
    provider                 TEXT        NOT NULL DEFAULT '',
    provider_installation_id BIGINT      NOT NULL DEFAULT 0,
    provider_repository_id   BIGINT      NOT NULL DEFAULT 0,
    repository_full_name     TEXT        NOT NULL DEFAULT '',
    scope_kind               TEXT        NOT NULL CHECK (scope_kind <> ''),
    scope_ref                TEXT        NOT NULL CHECK (scope_ref <> ''),
    default_scope_ref        TEXT        NOT NULL DEFAULT '',
    product_kind             TEXT        NOT NULL CHECK (product_kind <> ''),
    component_kind           TEXT        NOT NULL CHECK (component_kind <> ''),
    key_hash                 TEXT        NOT NULL CHECK (key_hash <> ''),
    key                      TEXT        NOT NULL DEFAULT '',
    display_name             TEXT        NOT NULL DEFAULT '',
    retention_policy         TEXT        NOT NULL DEFAULT 'short_7d',
    state                    TEXT        NOT NULL DEFAULT 'active',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (
        org_id, provider, provider_repository_id, scope_kind, scope_ref,
        product_kind, component_kind, key_hash
    )
);

CREATE INDEX idx_volumes_org_repository
    ON volumes (org_id, provider, provider_repository_id, updated_at DESC);

CREATE TABLE volume_generations (
    volume_generation_id      UUID        PRIMARY KEY,
    volume_id                 UUID        NOT NULL REFERENCES volumes(volume_id) ON DELETE CASCADE,
    org_id                    BIGINT      NOT NULL CHECK (org_id > 0),
    generation                INTEGER     NOT NULL CHECK (generation > 0),
    parent_generation_id      UUID        REFERENCES volume_generations(volume_generation_id) ON DELETE SET NULL,
    trust_class               TEXT        NOT NULL CHECK (trust_class <> ''),
    zfs_source_ref            TEXT        NOT NULL CHECK (zfs_source_ref <> ''),
    zfs_snapshot_ref          TEXT        NOT NULL DEFAULT '',
    used_bytes                BIGINT      NOT NULL DEFAULT 0 CHECK (used_bytes >= 0),
    written_bytes             BIGINT      NOT NULL DEFAULT 0 CHECK (written_bytes >= 0),
    state                     TEXT        NOT NULL DEFAULT 'committed',
    created_by_execution_id   UUID        REFERENCES executions(execution_id) ON DELETE SET NULL,
    created_by_attempt_id     UUID        REFERENCES execution_attempts(attempt_id) ON DELETE SET NULL,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at                TIMESTAMPTZ,
    UNIQUE (volume_id, generation),
    UNIQUE (zfs_source_ref)
);

CREATE INDEX idx_volume_generations_volume_generation
    ON volume_generations (volume_id, generation DESC);

CREATE TABLE volume_current_generation (
    org_id                    BIGINT      NOT NULL CHECK (org_id > 0),
    volume_id                 UUID        NOT NULL REFERENCES volumes(volume_id) ON DELETE CASCADE,
    trust_class               TEXT        NOT NULL CHECK (trust_class <> ''),
    current_generation        INTEGER     NOT NULL CHECK (current_generation > 0),
    volume_generation_id      UUID        NOT NULL REFERENCES volume_generations(volume_generation_id) ON DELETE CASCADE,
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, volume_id, trust_class)
);

CREATE TABLE execution_volume_mounts (
    mount_id                  UUID        PRIMARY KEY,
    execution_id              UUID        NOT NULL REFERENCES executions(execution_id) ON DELETE CASCADE,
    attempt_id                UUID        NOT NULL REFERENCES execution_attempts(attempt_id) ON DELETE CASCADE,
    allocation_id             UUID        REFERENCES runner_allocations(allocation_id) ON DELETE SET NULL,
    volume_id                 UUID        NOT NULL REFERENCES volumes(volume_id) ON DELETE CASCADE,
    mount_name                TEXT        NOT NULL CHECK (mount_name <> ''),
    mount_path                TEXT        NOT NULL CHECK (mount_path LIKE '/%' AND mount_path <> '/'),
    mount_path_hash           TEXT        NOT NULL CHECK (mount_path_hash <> ''),
    key_hash                  TEXT        NOT NULL CHECK (key_hash <> ''),
    source_generation_id      UUID        REFERENCES volume_generations(volume_generation_id) ON DELETE SET NULL,
    source_generation         INTEGER,
    source_ref                TEXT        NOT NULL DEFAULT '',
    target_source_ref         TEXT        NOT NULL DEFAULT '',
    guest_device_path         TEXT        NOT NULL DEFAULT '',
    mount_state               TEXT        NOT NULL DEFAULT 'requested',
    save_requested            BOOLEAN     NOT NULL DEFAULT false,
    save_state                TEXT        NOT NULL DEFAULT 'none',
    committed_generation_id   UUID        REFERENCES volume_generations(volume_generation_id) ON DELETE SET NULL,
    failure_reason            TEXT        NOT NULL DEFAULT '',
    requested_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    mounted_at                TIMESTAMPTZ,
    completed_at              TIMESTAMPTZ,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (attempt_id, key_hash, mount_path_hash),
    UNIQUE (attempt_id, mount_path_hash)
);

CREATE INDEX idx_execution_volume_mounts_attempt
    ON execution_volume_mounts (attempt_id, mount_state, save_state);

CREATE TABLE checkpoint_lifecycle_events (
    event_id                  UUID        PRIMARY KEY,
    event_name                TEXT        NOT NULL CHECK (event_name <> ''),
    observed_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    execution_id              UUID        NOT NULL REFERENCES executions(execution_id) ON DELETE CASCADE,
    attempt_id                UUID        NOT NULL REFERENCES execution_attempts(attempt_id) ON DELETE CASCADE,
    mount_id                  UUID        REFERENCES execution_volume_mounts(mount_id) ON DELETE SET NULL,
    volume_id                 UUID        REFERENCES volumes(volume_id) ON DELETE SET NULL,
    source_generation_id      UUID        REFERENCES volume_generations(volume_generation_id) ON DELETE SET NULL,
    committed_generation_id   UUID        REFERENCES volume_generations(volume_generation_id) ON DELETE SET NULL,
    org_id                    BIGINT      NOT NULL CHECK (org_id > 0),
    provider                  TEXT        NOT NULL DEFAULT '',
    provider_installation_id  BIGINT      NOT NULL DEFAULT 0,
    provider_repository_id    BIGINT      NOT NULL DEFAULT 0,
    provider_run_id           BIGINT      NOT NULL DEFAULT 0,
    provider_job_id           BIGINT      NOT NULL DEFAULT 0,
    repository_full_name      TEXT        NOT NULL DEFAULT '',
    runner_class              TEXT        NOT NULL DEFAULT '',
    scope_kind                TEXT        NOT NULL DEFAULT '',
    scope_ref                 TEXT        NOT NULL DEFAULT '',
    trust_class               TEXT        NOT NULL DEFAULT '',
    key_hash                  TEXT        NOT NULL DEFAULT '',
    mount_path_hash           TEXT        NOT NULL DEFAULT '',
    from_mount_state          TEXT        NOT NULL DEFAULT '',
    to_mount_state            TEXT        NOT NULL DEFAULT '',
    from_save_state           TEXT        NOT NULL DEFAULT '',
    to_save_state             TEXT        NOT NULL DEFAULT '',
    operation                 TEXT        NOT NULL DEFAULT '',
    result                    TEXT        NOT NULL DEFAULT '',
    reason_code               TEXT        NOT NULL DEFAULT '',
    used_bytes                BIGINT      NOT NULL DEFAULT 0 CHECK (used_bytes >= 0),
    written_bytes             BIGINT      NOT NULL DEFAULT 0 CHECK (written_bytes >= 0),
    duration_ms               BIGINT      NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    trace_id                  TEXT        NOT NULL DEFAULT '',
    span_id                   TEXT        NOT NULL DEFAULT ''
);

CREATE INDEX idx_checkpoint_lifecycle_events_attempt
    ON checkpoint_lifecycle_events (attempt_id, observed_at);

CREATE INDEX idx_checkpoint_lifecycle_events_volume
    ON checkpoint_lifecycle_events (volume_id, observed_at DESC);
