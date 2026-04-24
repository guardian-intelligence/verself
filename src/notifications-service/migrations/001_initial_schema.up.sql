CREATE TABLE notification_inbox_state (
    org_id TEXT NOT NULL,
    recipient_subject_id TEXT NOT NULL,
    next_sequence BIGINT NOT NULL DEFAULT 1,
    read_up_to_sequence BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (org_id, recipient_subject_id),
    CHECK (length(btrim(org_id)) > 0),
    CHECK (length(btrim(recipient_subject_id)) > 0),
    CHECK (next_sequence >= 1),
    CHECK (read_up_to_sequence >= 0)
);

CREATE TABLE notification_preferences (
    org_id TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    enabled BOOLEAN NOT NULL DEFAULT true,
    updated_at TIMESTAMPTZ NOT NULL,
    updated_by TEXT NOT NULL,
    PRIMARY KEY (org_id, subject_id),
    CHECK (version >= 0),
    CHECK (length(btrim(org_id)) > 0),
    CHECK (length(btrim(subject_id)) > 0)
);

CREATE TABLE notification_events (
    event_source TEXT NOT NULL,
    event_id UUID NOT NULL,
    subject TEXT NOT NULL,
    org_id TEXT NOT NULL,
    actor_subject_id TEXT NOT NULL DEFAULT '',
    recipient_subject_id TEXT NOT NULL,
    dedupe_key TEXT NOT NULL,
    kind TEXT NOT NULL,
    priority TEXT NOT NULL CHECK (priority IN ('low', 'normal', 'high')),
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    action_url TEXT NOT NULL DEFAULT '',
    resource_kind TEXT NOT NULL DEFAULT '',
    resource_id TEXT NOT NULL DEFAULT '',
    content_sha256 TEXT NOT NULL,
    payload JSONB NOT NULL,
    traceparent TEXT NOT NULL DEFAULT '',
    occurred_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    processed_at TIMESTAMPTZ,
    suppressed_at TIMESTAMPTZ,
    suppression_reason TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (event_source, event_id),
    CHECK (length(btrim(event_source)) > 0),
    CHECK (length(btrim(subject)) > 0),
    CHECK (length(btrim(org_id)) > 0),
    CHECK (length(btrim(recipient_subject_id)) > 0),
    CHECK (length(btrim(dedupe_key)) > 0),
    CHECK (length(btrim(kind)) > 0),
    CHECK (length(content_sha256) = 64)
);

CREATE UNIQUE INDEX notification_events_dedupe_key_idx ON notification_events (dedupe_key);
CREATE INDEX notification_events_pending_idx ON notification_events (received_at) WHERE processed_at IS NULL AND suppressed_at IS NULL;
CREATE INDEX notification_events_recipient_idx ON notification_events (org_id, recipient_subject_id, received_at DESC);

CREATE TABLE user_notifications (
    notification_id UUID PRIMARY KEY,
    org_id TEXT NOT NULL,
    recipient_subject_id TEXT NOT NULL,
    recipient_sequence BIGINT NOT NULL,
    dedupe_key TEXT NOT NULL,
    event_source TEXT NOT NULL,
    event_id UUID NOT NULL,
    kind TEXT NOT NULL,
    priority TEXT NOT NULL CHECK (priority IN ('low', 'normal', 'high')),
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    action_url TEXT NOT NULL DEFAULT '',
    resource_kind TEXT NOT NULL DEFAULT '',
    resource_id TEXT NOT NULL DEFAULT '',
    content_sha256 TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ,
    read_at TIMESTAMPTZ,
    dismissed_at TIMESTAMPTZ,
    CHECK (length(btrim(org_id)) > 0),
    CHECK (length(btrim(recipient_subject_id)) > 0),
    CHECK (recipient_sequence >= 1),
    CHECK (length(btrim(dedupe_key)) > 0),
    CHECK (length(content_sha256) = 64),
    UNIQUE (org_id, recipient_subject_id, recipient_sequence),
    UNIQUE (dedupe_key)
);

CREATE INDEX user_notifications_inbox_idx ON user_notifications (org_id, recipient_subject_id, recipient_sequence DESC);
CREATE INDEX user_notifications_unread_idx ON user_notifications (org_id, recipient_subject_id, recipient_sequence DESC)
    WHERE dismissed_at IS NULL AND read_at IS NULL;
CREATE INDEX user_notifications_event_idx ON user_notifications (event_source, event_id);

CREATE TABLE notification_projection_queue (
    ledger_event_id UUID PRIMARY KEY,
    event_type TEXT NOT NULL,
    org_id TEXT NOT NULL,
    recipient_subject_id TEXT NOT NULL,
    notification_id UUID,
    event_source TEXT NOT NULL DEFAULT '',
    event_id UUID,
    recipient_sequence BIGINT NOT NULL DEFAULT 0,
    source_subject TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL DEFAULT '',
    priority TEXT NOT NULL DEFAULT '',
    content_sha256 TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    traceparent TEXT NOT NULL DEFAULT '',
    occurred_at TIMESTAMPTZ NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    projected_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    CHECK (length(btrim(event_type)) > 0),
    CHECK (length(btrim(org_id)) > 0),
    CHECK (length(btrim(recipient_subject_id)) > 0),
    CHECK (recipient_sequence >= 0),
    CHECK (attempts >= 0)
);

CREATE INDEX notification_projection_pending_idx ON notification_projection_queue (next_attempt_at, occurred_at)
    WHERE projected_at IS NULL;

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
