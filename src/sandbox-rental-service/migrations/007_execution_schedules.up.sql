-- Recurring execution schedules backed by Temporal schedules.

CREATE TABLE execution_schedules (
    schedule_id           UUID        PRIMARY KEY,
    org_id                BIGINT      NOT NULL CHECK (org_id > 0),
    actor_id              TEXT        NOT NULL,
    display_name          TEXT        NOT NULL DEFAULT '',
    idempotency_key       TEXT        NOT NULL CHECK (idempotency_key <> ''),
    temporal_schedule_id  TEXT        NOT NULL CHECK (temporal_schedule_id <> ''),
    temporal_namespace    TEXT        NOT NULL CHECK (temporal_namespace <> ''),
    task_queue            TEXT        NOT NULL CHECK (task_queue <> ''),
    state                 TEXT        NOT NULL CHECK (state IN ('active', 'paused')),
    interval_seconds      INTEGER     NOT NULL CHECK (interval_seconds >= 15),
    run_command           TEXT        NOT NULL CHECK (run_command <> ''),
    max_wall_seconds      BIGINT      NOT NULL DEFAULT 0 CHECK (max_wall_seconds >= 0),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, idempotency_key),
    UNIQUE (temporal_schedule_id)
);

CREATE INDEX idx_execution_schedules_org_created
    ON execution_schedules (org_id, created_at DESC, schedule_id);

CREATE INDEX idx_execution_schedules_org_state
    ON execution_schedules (org_id, state, updated_at DESC, schedule_id);

CREATE TABLE execution_schedule_dispatches (
    dispatch_id           UUID        PRIMARY KEY,
    schedule_id           UUID        NOT NULL REFERENCES execution_schedules(schedule_id) ON DELETE CASCADE,
    temporal_workflow_id  TEXT        NOT NULL CHECK (temporal_workflow_id <> ''),
    temporal_run_id       TEXT        NOT NULL CHECK (temporal_run_id <> ''),
    execution_id          UUID        REFERENCES executions(execution_id) ON DELETE SET NULL,
    attempt_id            UUID        REFERENCES execution_attempts(attempt_id) ON DELETE SET NULL,
    state                 TEXT        NOT NULL CHECK (state IN ('pending', 'submitted', 'failed')),
    failure_reason        TEXT        NOT NULL DEFAULT '',
    scheduled_at          TIMESTAMPTZ NOT NULL,
    submitted_at          TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (schedule_id, temporal_workflow_id, temporal_run_id)
);

CREATE INDEX idx_execution_schedule_dispatches_schedule_created
    ON execution_schedule_dispatches (schedule_id, created_at DESC, dispatch_id);

CREATE INDEX idx_execution_schedule_dispatches_execution
    ON execution_schedule_dispatches (execution_id)
    WHERE execution_id IS NOT NULL;
