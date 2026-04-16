-- Sandbox rental service control-plane schema.
-- Database: sandbox_rental (one database per service).

CREATE SEQUENCE execution_billing_job_seq AS BIGINT;

CREATE TABLE runner_classes (
    runner_class  TEXT        PRIMARY KEY,
    product_id    TEXT        NOT NULL DEFAULT 'sandbox',
    display_name  TEXT        NOT NULL,
    os_family     TEXT        NOT NULL,
    os_version    TEXT        NOT NULL,
    arch          TEXT        NOT NULL DEFAULT 'x86_64',
    vcpus         INTEGER     NOT NULL CHECK (vcpus > 0),
    memory_mib    INTEGER     NOT NULL CHECK (memory_mib > 0),
    rootfs_gib    INTEGER     NOT NULL CHECK (rootfs_gib > 0),
    runtime_image TEXT        NOT NULL,
    active        BOOLEAN     NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO runner_classes (
    runner_class,
    product_id,
    display_name,
    os_family,
    os_version,
    arch,
    vcpus,
    memory_mib,
    rootfs_gib,
    runtime_image
) VALUES (
    'metal-4vcpu-ubuntu-2404',
    'sandbox',
    'Metal 4 vCPU Ubuntu 24.04',
    'ubuntu',
    '24.04',
    'x86_64',
    4,
    4096,
    8,
    'ubuntu-2404-actions-runner'
);

CREATE TABLE executions (
    execution_id      UUID        PRIMARY KEY,
    org_id            BIGINT      NOT NULL CHECK (org_id > 0),
    actor_id          TEXT        NOT NULL,
    kind              TEXT        NOT NULL,
    source_kind       TEXT        NOT NULL DEFAULT 'api',
    workload_kind     TEXT        NOT NULL DEFAULT 'direct',
    source_ref        TEXT        NOT NULL DEFAULT '',
    runner_class      TEXT        NOT NULL REFERENCES runner_classes(runner_class),
    external_provider TEXT        NOT NULL DEFAULT '',
    external_task_id  TEXT        NOT NULL DEFAULT '',
    provider          TEXT        NOT NULL DEFAULT '',
    product_id        TEXT        NOT NULL DEFAULT 'sandbox',
    state             TEXT        NOT NULL,
    correlation_id    TEXT        NOT NULL DEFAULT '',
    idempotency_key   TEXT        NOT NULL DEFAULT '',
    run_command       TEXT        NOT NULL DEFAULT '',
    max_wall_seconds  BIGINT      NOT NULL DEFAULT 0 CHECK (max_wall_seconds >= 0),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_executions_org_idempotency_key
    ON executions (org_id, idempotency_key);
CREATE INDEX idx_executions_org_updated_at ON executions (org_id, updated_at DESC);
CREATE INDEX idx_executions_state_updated ON executions (state, updated_at);
CREATE INDEX idx_executions_source_workload_updated
    ON executions (source_kind, workload_kind, updated_at DESC);
CREATE INDEX idx_executions_external_task
    ON executions (external_provider, external_task_id)
    WHERE external_provider <> '' AND external_task_id <> '';
CREATE INDEX idx_executions_correlation_id
    ON executions (correlation_id)
    WHERE correlation_id <> '';

CREATE TABLE execution_attempts (
    attempt_id     UUID        PRIMARY KEY,
    execution_id   UUID        NOT NULL REFERENCES executions(execution_id) ON DELETE CASCADE,
    attempt_seq    INTEGER     NOT NULL CHECK (attempt_seq > 0),
    state          TEXT        NOT NULL,
    lease_id       TEXT,
    exec_id        TEXT,
    billing_job_id BIGINT,
    failure_reason TEXT        NOT NULL DEFAULT '',
    exit_code      INTEGER     NOT NULL DEFAULT 0,
    duration_ms    BIGINT      NOT NULL DEFAULT 0,
    zfs_written    BIGINT      NOT NULL DEFAULT 0,
    stdout_bytes   BIGINT      NOT NULL DEFAULT 0,
    stderr_bytes   BIGINT      NOT NULL DEFAULT 0,
    trace_id       TEXT        NOT NULL DEFAULT '',
    started_at     TIMESTAMPTZ,
    completed_at   TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (execution_id, attempt_seq)
);

CREATE INDEX idx_execution_attempts_execution_id
    ON execution_attempts (execution_id, attempt_seq DESC);
CREATE INDEX idx_execution_attempts_state_updated_at
    ON execution_attempts (state, updated_at);
CREATE INDEX idx_execution_attempts_lease
    ON execution_attempts (lease_id)
    WHERE lease_id IS NOT NULL;
CREATE INDEX idx_execution_attempts_exec
    ON execution_attempts (exec_id)
    WHERE exec_id IS NOT NULL;

CREATE TABLE execution_events (
    event_seq    BIGSERIAL   PRIMARY KEY,
    execution_id UUID        NOT NULL REFERENCES executions(execution_id) ON DELETE CASCADE,
    attempt_id   UUID        NOT NULL REFERENCES execution_attempts(attempt_id) ON DELETE CASCADE,
    from_state   TEXT        NOT NULL DEFAULT '',
    to_state     TEXT        NOT NULL,
    reason       TEXT        NOT NULL DEFAULT '',
    trace_id     TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_execution_events_execution_created
    ON execution_events (execution_id, created_at, event_seq);
CREATE INDEX idx_execution_events_attempt_created
    ON execution_events (attempt_id, created_at, event_seq);
CREATE INDEX idx_execution_events_trace_id
    ON execution_events (trace_id)
    WHERE trace_id <> '';

CREATE TABLE execution_billing_windows (
    attempt_id        UUID        NOT NULL REFERENCES execution_attempts(attempt_id) ON DELETE CASCADE,
    billing_window_id TEXT        NOT NULL,
    window_seq        INTEGER     NOT NULL,
    reservation_shape TEXT        NOT NULL DEFAULT 'time',
    reserved_quantity INTEGER     NOT NULL DEFAULT 0,
    actual_quantity   INTEGER     NOT NULL DEFAULT 0,
    pricing_phase     TEXT        NOT NULL DEFAULT '',
    state             TEXT        NOT NULL,
    window_start      TIMESTAMPTZ NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    settled_at        TIMESTAMPTZ,
    reservation_jsonb JSONB       NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (attempt_id, window_seq),
    UNIQUE (billing_window_id)
);

CREATE INDEX idx_execution_billing_windows_state_attempt
    ON execution_billing_windows (state, attempt_id);

CREATE TABLE execution_logs (
    execution_id UUID        NOT NULL REFERENCES executions(execution_id) ON DELETE CASCADE,
    attempt_id   UUID        NOT NULL REFERENCES execution_attempts(attempt_id) ON DELETE CASCADE,
    seq          INTEGER     NOT NULL,
    stream       TEXT        NOT NULL,
    chunk        TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (attempt_id, seq)
);

CREATE TABLE github_installations (
    installation_id BIGINT      PRIMARY KEY,
    org_id          BIGINT      NOT NULL CHECK (org_id > 0),
    account_login   TEXT        NOT NULL,
    account_type    TEXT        NOT NULL DEFAULT '',
    active          BOOLEAN     NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_github_installations_org
    ON github_installations (org_id, active, updated_at DESC);

CREATE TABLE github_installation_states (
    state        TEXT        PRIMARY KEY,
    org_id       BIGINT      NOT NULL CHECK (org_id > 0),
    actor_id     TEXT        NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_github_installation_states_expires
    ON github_installation_states (expires_at);

CREATE TABLE github_workflow_jobs (
    github_job_id          BIGINT      PRIMARY KEY,
    installation_id        BIGINT      NOT NULL,
    repository_id          BIGINT      NOT NULL,
    repository_full_name   TEXT        NOT NULL,
    run_id                 BIGINT      NOT NULL,
    job_name               TEXT        NOT NULL DEFAULT '',
    head_sha               TEXT        NOT NULL DEFAULT '',
    head_branch            TEXT        NOT NULL DEFAULT '',
    workflow_name          TEXT        NOT NULL DEFAULT '',
    status                 TEXT        NOT NULL,
    conclusion             TEXT        NOT NULL DEFAULT '',
    labels_json            JSONB       NOT NULL DEFAULT '[]'::jsonb,
    runner_id              BIGINT      NOT NULL DEFAULT 0,
    runner_name            TEXT        NOT NULL DEFAULT '',
    started_at             TIMESTAMPTZ,
    completed_at           TIMESTAMPTZ,
    last_webhook_delivery  TEXT        NOT NULL DEFAULT '',
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_github_workflow_jobs_installation_status
    ON github_workflow_jobs (installation_id, status, updated_at DESC);
CREATE INDEX idx_github_workflow_jobs_runner
    ON github_workflow_jobs (runner_id, runner_name)
    WHERE runner_id <> 0 OR runner_name <> '';

CREATE TABLE github_runner_allocations (
    allocation_id                  UUID        PRIMARY KEY,
    installation_id                BIGINT      NOT NULL,
    repository_id                  BIGINT      NOT NULL DEFAULT 0,
    runner_class                   TEXT        NOT NULL REFERENCES runner_classes(runner_class),
    runner_name                    TEXT        NOT NULL,
    github_runner_id               BIGINT      NOT NULL DEFAULT 0,
    execution_id                   UUID        REFERENCES executions(execution_id) ON DELETE SET NULL,
    attempt_id                     UUID        REFERENCES execution_attempts(attempt_id) ON DELETE SET NULL,
    state                          TEXT        NOT NULL,
    requested_for_github_job_id    BIGINT      NOT NULL DEFAULT 0,
    allocate_by                    TIMESTAMPTZ,
    jit_by                         TIMESTAMPTZ,
    vm_submitted_by                TIMESTAMPTZ,
    runner_listening_by            TIMESTAMPTZ,
    assignment_by                  TIMESTAMPTZ,
    vm_exit_by                     TIMESTAMPTZ,
    cleanup_by                     TIMESTAMPTZ,
    failure_reason                 TEXT        NOT NULL DEFAULT '',
    created_at                     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_github_runner_allocations_state
    ON github_runner_allocations (state, updated_at);
CREATE UNIQUE INDEX idx_github_runner_allocations_execution
    ON github_runner_allocations (execution_id)
    WHERE execution_id IS NOT NULL;
CREATE UNIQUE INDEX idx_github_runner_allocations_runner_name
    ON github_runner_allocations (runner_name)
    WHERE runner_name <> '';

CREATE TABLE github_runner_jit_configs (
    allocation_id      UUID        PRIMARY KEY REFERENCES github_runner_allocations(allocation_id) ON DELETE CASCADE,
    attempt_id         UUID        NOT NULL REFERENCES execution_attempts(attempt_id) ON DELETE CASCADE,
    fetch_token_hash   TEXT        NOT NULL UNIQUE CHECK (fetch_token_hash <> ''),
    encoded_jit_config TEXT        NOT NULL CHECK (encoded_jit_config <> ''),
    expires_at         TIMESTAMPTZ NOT NULL,
    consumed_at        TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_github_runner_jit_configs_attempt
    ON github_runner_jit_configs (attempt_id);
CREATE INDEX idx_github_runner_jit_configs_expires
    ON github_runner_jit_configs (expires_at)
    WHERE consumed_at IS NULL;

CREATE TABLE github_runner_job_bindings (
    binding_id       UUID        PRIMARY KEY,
    allocation_id    UUID        NOT NULL REFERENCES github_runner_allocations(allocation_id) ON DELETE CASCADE,
    github_job_id    BIGINT      NOT NULL REFERENCES github_workflow_jobs(github_job_id) ON DELETE CASCADE,
    github_runner_id BIGINT      NOT NULL DEFAULT 0,
    runner_name      TEXT        NOT NULL DEFAULT '',
    bound_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (allocation_id),
    UNIQUE (github_job_id)
);

CREATE UNIQUE INDEX idx_github_runner_job_bindings_runner
    ON github_runner_job_bindings (github_runner_id, runner_name)
    WHERE github_runner_id <> 0 OR runner_name <> '';
