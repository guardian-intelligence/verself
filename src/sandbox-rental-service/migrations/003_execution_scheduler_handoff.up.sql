-- Durable execution handoff state shared by API submissions, runner adapters,
-- schedules, and VM session control-plane work.

ALTER TABLE executions
  ADD COLUMN source_kind TEXT NOT NULL DEFAULT 'api',
  ADD COLUMN workload_kind TEXT NOT NULL DEFAULT 'direct',
  ADD COLUMN source_ref TEXT NOT NULL DEFAULT '',
  ADD COLUMN verification_run_id TEXT NOT NULL DEFAULT '';

ALTER TABLE execution_attempts
  ADD COLUMN submit_trace_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN submit_trace_context TEXT NOT NULL DEFAULT '';

ALTER TABLE execution_billing_windows
  ADD COLUMN reservation_jsonb JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE execution_events (
    execution_id UUID        NOT NULL REFERENCES executions(execution_id) ON DELETE CASCADE,
    attempt_id   UUID        NOT NULL REFERENCES execution_attempts(attempt_id) ON DELETE CASCADE,
    event_seq    BIGINT      NOT NULL,
    from_state   TEXT        NOT NULL DEFAULT '',
    to_state     TEXT        NOT NULL,
    reason       TEXT        NOT NULL DEFAULT '',
    trace_id     TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (attempt_id, event_seq)
);

CREATE INDEX idx_execution_events_execution_created
    ON execution_events (execution_id, created_at, event_seq);
CREATE INDEX idx_execution_events_trace_id
    ON execution_events (trace_id)
    WHERE trace_id <> '';

CREATE TABLE execution_workload_specs (
    execution_id      UUID        PRIMARY KEY REFERENCES executions(execution_id) ON DELETE CASCADE,
    workload_kind     TEXT        NOT NULL,
    spec_jsonb        JSONB       NOT NULL,
    secret_refs_jsonb JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_execution_attempts_state_updated_at
    ON execution_attempts (state, updated_at);
CREATE INDEX idx_execution_billing_windows_state_attempt
    ON execution_billing_windows (state, attempt_id);
CREATE INDEX idx_executions_source_workload_updated
    ON executions (source_kind, workload_kind, updated_at DESC);
