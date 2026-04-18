-- Durable volume inventory and billing sweep state.

CREATE TABLE volumes (
    volume_id                    UUID        PRIMARY KEY,
    org_id                       BIGINT      NOT NULL CHECK (org_id > 0),
    actor_id                     TEXT        NOT NULL DEFAULT '',
    product_id                   TEXT        NOT NULL DEFAULT 'sandbox',
    idempotency_key              TEXT        NOT NULL CHECK (idempotency_key <> ''),
    display_name                 TEXT        NOT NULL DEFAULT '',
    state                        TEXT        NOT NULL CHECK (state IN ('active', 'read_only', 'write_blocked', 'retention_only', 'deleted')),
    storage_node_id              TEXT        NOT NULL CHECK (storage_node_id <> ''),
    pool_id                      TEXT        NOT NULL CHECK (pool_id <> ''),
    dataset_ref                  TEXT        NOT NULL CHECK (dataset_ref <> '' AND dataset_ref !~ '^/'),
    current_generation_id        UUID,
    used_bytes                   BIGINT      NOT NULL DEFAULT 0 CHECK (used_bytes >= 0),
    usedbysnapshots_bytes        BIGINT      NOT NULL DEFAULT 0 CHECK (usedbysnapshots_bytes >= 0),
    billable_live_bytes          BIGINT      NOT NULL DEFAULT 0 CHECK (billable_live_bytes >= 0),
    billable_retained_bytes      BIGINT      NOT NULL DEFAULT 0 CHECK (billable_retained_bytes >= 0),
    written_bytes                BIGINT      NOT NULL DEFAULT 0 CHECK (written_bytes >= 0),
    provisioned_bytes            BIGINT      NOT NULL DEFAULT 0 CHECK (provisioned_bytes >= 0),
    last_metered_at              TIMESTAMPTZ,
    created_at                   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, idempotency_key),
    UNIQUE (org_id, dataset_ref)
);

CREATE INDEX idx_volumes_org_state
    ON volumes (org_id, state, updated_at DESC);

CREATE TABLE volume_generations (
    volume_generation_id UUID        PRIMARY KEY,
    volume_id            UUID        NOT NULL REFERENCES volumes(volume_id) ON DELETE CASCADE,
    org_id               BIGINT      NOT NULL CHECK (org_id > 0),
    generation_seq       INTEGER     NOT NULL CHECK (generation_seq > 0),
    source_ref           TEXT        NOT NULL DEFAULT '',
    state                TEXT        NOT NULL CHECK (state IN ('current', 'retained', 'deleted')),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    retained_at          TIMESTAMPTZ,
    deleted_at           TIMESTAMPTZ,
    UNIQUE (volume_id, generation_seq)
);

CREATE INDEX idx_volume_generations_volume_state
    ON volume_generations (volume_id, state, generation_seq DESC);

ALTER TABLE volumes
    ADD CONSTRAINT volumes_current_generation_fk
    FOREIGN KEY (current_generation_id) REFERENCES volume_generations(volume_generation_id) ON DELETE SET NULL;

CREATE TABLE volume_events (
    event_seq   BIGSERIAL   PRIMARY KEY,
    volume_id   UUID        NOT NULL REFERENCES volumes(volume_id) ON DELETE CASCADE,
    org_id      BIGINT      NOT NULL CHECK (org_id > 0),
    event_type  TEXT        NOT NULL CHECK (event_type <> ''),
    trace_id    TEXT        NOT NULL DEFAULT '',
    payload     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_volume_events_volume_created
    ON volume_events (volume_id, created_at, event_seq);

CREATE TABLE volume_meter_ticks (
    meter_tick_id              UUID        PRIMARY KEY,
    volume_id                  UUID        NOT NULL REFERENCES volumes(volume_id) ON DELETE CASCADE,
    org_id                     BIGINT      NOT NULL CHECK (org_id > 0),
    actor_id                   TEXT        NOT NULL DEFAULT '',
    product_id                 TEXT        NOT NULL DEFAULT 'sandbox',
    idempotency_key            TEXT        NOT NULL CHECK (idempotency_key <> ''),
    source_type                TEXT        NOT NULL DEFAULT 'volume_meter_tick',
    source_ref                 TEXT        NOT NULL CHECK (source_ref <> ''),
    window_seq                 INTEGER     NOT NULL DEFAULT 1 CHECK (window_seq >= 0),
    window_millis              INTEGER     NOT NULL CHECK (window_millis >= 30000),
    state                      TEXT        NOT NULL CHECK (state IN ('queued', 'billing_reserving', 'billing_settled', 'billing_failed')),
    observed_at                TIMESTAMPTZ NOT NULL,
    window_start               TIMESTAMPTZ NOT NULL,
    window_end                 TIMESTAMPTZ NOT NULL,
    used_bytes                 BIGINT      NOT NULL CHECK (used_bytes >= 0),
    usedbysnapshots_bytes      BIGINT      NOT NULL CHECK (usedbysnapshots_bytes >= 0),
    billable_live_bytes        BIGINT      NOT NULL CHECK (billable_live_bytes >= 0),
    billable_retained_bytes    BIGINT      NOT NULL CHECK (billable_retained_bytes >= 0),
    written_bytes              BIGINT      NOT NULL CHECK (written_bytes >= 0),
    provisioned_bytes          BIGINT      NOT NULL CHECK (provisioned_bytes >= 0),
    allocation_jsonb           JSONB       NOT NULL DEFAULT '{}'::jsonb,
    billing_window_id          TEXT        NOT NULL DEFAULT '',
    billed_charge_units        BIGINT      NOT NULL DEFAULT 0 CHECK (billed_charge_units >= 0),
    billing_failure_reason     TEXT        NOT NULL DEFAULT '',
    clickhouse_projected_at    TIMESTAMPTZ,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (window_end > window_start),
    UNIQUE (org_id, idempotency_key),
    UNIQUE (source_type, source_ref, window_seq)
);

CREATE INDEX idx_volume_meter_ticks_volume_window
    ON volume_meter_ticks (volume_id, window_start DESC, meter_tick_id);

CREATE INDEX idx_volume_meter_ticks_project
    ON volume_meter_ticks (state, clickhouse_projected_at, updated_at, meter_tick_id)
    WHERE clickhouse_projected_at IS NULL;
