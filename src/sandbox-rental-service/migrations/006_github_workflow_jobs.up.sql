-- GitHub App workflow_job correlation. GitHub remains a provider adapter:
-- terminalization, billing, logs, and VM cleanup stay in executions.

CREATE TABLE github_app_installations (
    installation_id BIGINT      PRIMARY KEY,
    org_id          BIGINT      NOT NULL CHECK (org_id > 0),
    account_login   TEXT        NOT NULL,
    account_type    TEXT        NOT NULL DEFAULT '',
    active          BOOLEAN     NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE github_app_installation_states (
    state           TEXT        PRIMARY KEY,
    org_id          BIGINT      NOT NULL CHECK (org_id > 0),
    actor_id        TEXT        NOT NULL,
    installation_id BIGINT,
    expires_at      TIMESTAMPTZ NOT NULL,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_github_app_installation_states_open
    ON github_app_installation_states (expires_at)
    WHERE completed_at IS NULL;

CREATE TABLE github_workflow_job_executions (
    github_job_id       BIGINT      PRIMARY KEY,
    installation_id     BIGINT      NOT NULL REFERENCES github_app_installations(installation_id),
    org_id              BIGINT      NOT NULL CHECK (org_id > 0),
    org_login           TEXT        NOT NULL,
    repo_id             BIGINT      NOT NULL,
    repo_full_name      TEXT        NOT NULL,
    runner_class        TEXT        NOT NULL REFERENCES runner_classes(runner_class),
    delivery_id         TEXT        NOT NULL,
    action              TEXT        NOT NULL,
    execution_id        UUID        REFERENCES executions(execution_id) ON DELETE SET NULL,
    state               TEXT        NOT NULL,
    queued_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    submitted_at        TIMESTAMPTZ,
    finalized_at        TIMESTAMPTZ,
    last_error          TEXT        NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_github_workflow_job_executions_execution
    ON github_workflow_job_executions (execution_id)
    WHERE execution_id IS NOT NULL;

CREATE INDEX idx_github_workflow_job_executions_state_updated
    ON github_workflow_job_executions (state, updated_at);
