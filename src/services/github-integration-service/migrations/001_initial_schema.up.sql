CREATE TABLE github_webhook_deliveries (
    delivery_id              TEXT        PRIMARY KEY CHECK (delivery_id <> ''),
    event_name               TEXT        NOT NULL CHECK (event_name <> ''),
    action                   TEXT        NOT NULL DEFAULT '',
    state                    TEXT        NOT NULL CHECK (state <> ''),
    primary_problem_type     TEXT        NOT NULL DEFAULT '',
    primary_problem_code     TEXT        NOT NULL DEFAULT '',
    primary_problem_status   INTEGER     NOT NULL DEFAULT 0 CHECK (primary_problem_status BETWEEN 0 AND 599),
    primary_problem_title    TEXT        NOT NULL DEFAULT '',
    primary_problem_detail   TEXT        NOT NULL DEFAULT '',
    problem_count            INTEGER     NOT NULL DEFAULT 0 CHECK (problem_count >= 0),
    payload_sha256           TEXT        NOT NULL CHECK (payload_sha256 <> ''),
    payload_json             JSONB       NOT NULL,
    attempt_count            INTEGER     NOT NULL DEFAULT 0,
    provider_installation_id BIGINT      NOT NULL DEFAULT 0,
    provider_repository_id   BIGINT      NOT NULL DEFAULT 0,
    repository_full_name     TEXT        NOT NULL DEFAULT '',
    provider_run_id          BIGINT      NOT NULL DEFAULT 0,
    provider_run_attempt     BIGINT      NOT NULL DEFAULT 0,
    provider_job_id          BIGINT      NOT NULL DEFAULT 0,
    received_at              TIMESTAMPTZ NOT NULL,
    verified_at              TIMESTAMPTZ NOT NULL,
    next_attempt_at          TIMESTAMPTZ,
    processing_started_at    TIMESTAMPTZ,
    processed_at             TIMESTAMPTZ,
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE github_webhook_delivery_problems (
    delivery_id              TEXT        NOT NULL REFERENCES github_webhook_deliveries(delivery_id) ON DELETE CASCADE,
    problem_seq              INTEGER     NOT NULL CHECK (problem_seq > 0),
    phase                    TEXT        NOT NULL CHECK (phase <> ''),
    problem_type             TEXT        NOT NULL CHECK (problem_type <> ''),
    problem_code             TEXT        NOT NULL CHECK (problem_code <> ''),
    title                    TEXT        NOT NULL CHECK (title <> ''),
    detail                   TEXT        NOT NULL DEFAULT '',
    status                   INTEGER     NOT NULL DEFAULT 0 CHECK (status BETWEEN 0 AND 599),
    retryable                BOOLEAN     NOT NULL DEFAULT false,
    pointer                  TEXT        NOT NULL DEFAULT '',
    observed_at              TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (delivery_id, problem_seq)
);

CREATE INDEX idx_github_webhook_delivery_problems_delivery
    ON github_webhook_delivery_problems (delivery_id, observed_at);

CREATE TABLE github_accounts (
    provider_account_id      BIGINT      PRIMARY KEY CHECK (provider_account_id > 0),
    login                    TEXT        NOT NULL CHECK (login <> ''),
    account_type             TEXT        NOT NULL CHECK (account_type <> ''),
    avatar_url               TEXT        NOT NULL DEFAULT '',
    html_url                 TEXT        NOT NULL DEFAULT '',
    state                    TEXT        NOT NULL CHECK (state <> ''),
    last_event_delivery_id   TEXT        NOT NULL DEFAULT '',
    observed_from_api_at     TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE github_installations (
    provider_installation_id BIGINT      PRIMARY KEY CHECK (provider_installation_id > 0),
    provider_account_id      BIGINT      NOT NULL DEFAULT 0,
    account_login            TEXT        NOT NULL DEFAULT '',
    account_type             TEXT        NOT NULL DEFAULT '',
    app_slug                 TEXT        NOT NULL DEFAULT '',
    target_type              TEXT        NOT NULL DEFAULT '',
    repository_selection     TEXT        NOT NULL DEFAULT '',
    configuration_url        TEXT        NOT NULL DEFAULT '',
    permissions_json         JSONB       NOT NULL DEFAULT '{}'::jsonb,
    suspended_by             TEXT        NOT NULL DEFAULT '',
    suspended_at             TIMESTAMPTZ,
    state                    TEXT        NOT NULL CHECK (state <> ''),
    last_event_delivery_id   TEXT        NOT NULL DEFAULT '',
    observed_from_api_at     TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_github_installations_account
    ON github_installations (provider_account_id, state, updated_at DESC);

CREATE TABLE github_setup_sessions (
    setup_session_id         UUID        PRIMARY KEY,
    org_id                   TEXT        NOT NULL CHECK (org_id <> ''),
    actor_id                 TEXT        NOT NULL CHECK (actor_id <> ''),
    state_hash               TEXT        NOT NULL UNIQUE CHECK (state_hash <> ''),
    session_state            TEXT        NOT NULL CHECK (session_state <> ''),
    installation_url         TEXT        NOT NULL DEFAULT '',
    callback_url             TEXT        NOT NULL DEFAULT '',
    provider_installation_id BIGINT      NOT NULL DEFAULT 0,
    github_user_authorization_id UUID,
    installation_binding_id  UUID,
    failure_reason           TEXT        NOT NULL DEFAULT '',
    expires_at               TIMESTAMPTZ NOT NULL,
    completed_at             TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_github_setup_sessions_org_state
    ON github_setup_sessions (org_id, session_state, updated_at DESC);

CREATE TABLE github_oauth_sessions (
    oauth_session_id         UUID        PRIMARY KEY,
    org_id                   TEXT        NOT NULL CHECK (org_id <> ''),
    actor_id                 TEXT        NOT NULL CHECK (actor_id <> ''),
    state_hash               TEXT        NOT NULL UNIQUE CHECK (state_hash <> ''),
    session_state            TEXT        NOT NULL CHECK (session_state <> ''),
    authorization_url        TEXT        NOT NULL DEFAULT '',
    callback_url             TEXT        NOT NULL DEFAULT '',
    github_user_authorization_id UUID,
    failure_reason           TEXT        NOT NULL DEFAULT '',
    expires_at               TIMESTAMPTZ NOT NULL,
    completed_at             TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_github_oauth_sessions_org_state
    ON github_oauth_sessions (org_id, session_state, updated_at DESC);

CREATE TABLE github_idempotency_records (
    org_id                   TEXT        NOT NULL CHECK (org_id <> ''),
    actor_id                 TEXT        NOT NULL CHECK (actor_id <> ''),
    operation                TEXT        NOT NULL CHECK (operation <> ''),
    key_hash                 TEXT        NOT NULL CHECK (key_hash ~ '^[a-f0-9]{64}$'),
    request_hash             TEXT        NOT NULL CHECK (request_hash ~ '^[a-f0-9]{64}$'),
    result_payload           JSONB       NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, actor_id, operation, key_hash)
);

CREATE TABLE github_user_authorizations (
    github_user_authorization_id UUID    PRIMARY KEY,
    org_id                   TEXT        NOT NULL CHECK (org_id <> ''),
    actor_id                 TEXT        NOT NULL CHECK (actor_id <> ''),
    provider_user_id         BIGINT      NOT NULL CHECK (provider_user_id > 0),
    github_login             TEXT        NOT NULL CHECK (github_login <> ''),
    scopes_json              JSONB       NOT NULL DEFAULT '[]'::jsonb,
    credential_ref           TEXT        NOT NULL CHECK (credential_ref <> ''),
    state                    TEXT        NOT NULL CHECK (state <> ''),
    authorized_at            TIMESTAMPTZ NOT NULL,
    last_verified_at         TIMESTAMPTZ,
    revoked_at               TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_github_user_authorizations_active
    ON github_user_authorizations (org_id, actor_id, provider_user_id)
    WHERE state = 'active';

CREATE TABLE github_installation_bindings (
    installation_binding_id  UUID        PRIMARY KEY,
    org_id                   TEXT        NOT NULL CHECK (org_id <> ''),
    provider_installation_id BIGINT      NOT NULL REFERENCES github_installations(provider_installation_id) ON DELETE CASCADE,
    provider_account_id      BIGINT      NOT NULL DEFAULT 0,
    setup_session_id         UUID        REFERENCES github_setup_sessions(setup_session_id) ON DELETE SET NULL,
    connected_by_actor_id    TEXT        NOT NULL CHECK (connected_by_actor_id <> ''),
    disconnected_by_actor_id TEXT        NOT NULL DEFAULT '',
    state                    TEXT        NOT NULL CHECK (state <> ''),
    connected_at             TIMESTAMPTZ NOT NULL,
    disconnected_at          TIMESTAMPTZ,
    last_synced_at           TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_github_installation_bindings_active_installation
    ON github_installation_bindings (provider_installation_id)
    WHERE state = 'active';

CREATE INDEX idx_github_installation_bindings_org
    ON github_installation_bindings (org_id, state, updated_at DESC);

CREATE TABLE github_repositories (
    provider_repository_id   BIGINT      PRIMARY KEY CHECK (provider_repository_id > 0),
    provider_account_id      BIGINT      NOT NULL DEFAULT 0,
    owner_login              TEXT        NOT NULL DEFAULT '',
    repository_name          TEXT        NOT NULL DEFAULT '',
    repository_full_name     TEXT        NOT NULL CHECK (repository_full_name <> ''),
    default_branch           TEXT        NOT NULL DEFAULT '',
    private                  BOOLEAN     NOT NULL DEFAULT false,
    archived                 BOOLEAN     NOT NULL DEFAULT false,
    visibility               TEXT        NOT NULL DEFAULT '',
    html_url                 TEXT        NOT NULL DEFAULT '',
    state                    TEXT        NOT NULL CHECK (state <> ''),
    last_event_delivery_id   TEXT        NOT NULL DEFAULT '',
    observed_from_api_at     TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_github_repositories_owner
    ON github_repositories (owner_login, repository_name);

CREATE TABLE github_installation_repositories (
    provider_installation_id BIGINT      NOT NULL REFERENCES github_installations(provider_installation_id) ON DELETE CASCADE,
    provider_repository_id   BIGINT      NOT NULL REFERENCES github_repositories(provider_repository_id) ON DELETE CASCADE,
    state                    TEXT        NOT NULL CHECK (state <> ''),
    last_event_delivery_id   TEXT        NOT NULL DEFAULT '',
    observed_from_api_at     TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider_installation_id, provider_repository_id)
);

CREATE INDEX idx_github_installation_repositories_installation
    ON github_installation_repositories (provider_installation_id, state, updated_at DESC);

CREATE TABLE github_repository_bindings (
    repository_binding_id    UUID        PRIMARY KEY,
    org_id                   TEXT        NOT NULL CHECK (org_id <> ''),
    installation_binding_id  UUID        NOT NULL REFERENCES github_installation_bindings(installation_binding_id) ON DELETE CASCADE,
    provider_installation_id BIGINT      NOT NULL,
    provider_repository_id   BIGINT      NOT NULL REFERENCES github_repositories(provider_repository_id) ON DELETE CASCADE,
    state                    TEXT        NOT NULL CHECK (state <> ''),
    enabled_by_actor_id      TEXT        NOT NULL DEFAULT '',
    disabled_by_actor_id     TEXT        NOT NULL DEFAULT '',
    enabled_at               TIMESTAMPTZ,
    disabled_at              TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_github_repository_bindings_enabled_repo
    ON github_repository_bindings (org_id, provider_repository_id)
    WHERE state = 'enabled';

CREATE UNIQUE INDEX idx_github_repository_bindings_installation_repo
    ON github_repository_bindings (installation_binding_id, provider_repository_id);

CREATE INDEX idx_github_repository_bindings_org
    ON github_repository_bindings (org_id, state, updated_at DESC);

CREATE TABLE github_provider_reconciliations (
    reconciliation_id        UUID        PRIMARY KEY,
    org_id                   TEXT        NOT NULL DEFAULT '',
    installation_binding_id  UUID,
    provider_installation_id BIGINT      NOT NULL DEFAULT 0,
    reason                   TEXT        NOT NULL CHECK (reason <> ''),
    state                    TEXT        NOT NULL CHECK (state <> ''),
    attempt_count            INTEGER     NOT NULL DEFAULT 0,
    failure_reason           TEXT        NOT NULL DEFAULT '',
    started_at               TIMESTAMPTZ,
    completed_at             TIMESTAMPTZ,
    next_attempt_at          TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_github_provider_reconciliations_ready
    ON github_provider_reconciliations (next_attempt_at NULLS FIRST, created_at)
    WHERE state IN ('pending', 'retryable');

CREATE INDEX idx_github_webhook_deliveries_pending
    ON github_webhook_deliveries (next_attempt_at NULLS FIRST, received_at)
    WHERE state IN ('accepted', 'retryable');

CREATE TABLE github_workflow_jobs (
    provider_job_id          BIGINT      PRIMARY KEY,
    org_id                   TEXT        NOT NULL DEFAULT '',
    installation_binding_id  UUID,
    repository_binding_id    UUID,
    provider_installation_id BIGINT      NOT NULL DEFAULT 0,
    provider_repository_id   BIGINT      NOT NULL DEFAULT 0,
    repository_full_name     TEXT        NOT NULL DEFAULT '',
    provider_run_id          BIGINT      NOT NULL DEFAULT 0,
    provider_run_attempt     BIGINT      NOT NULL DEFAULT 0,
    job_name                 TEXT        NOT NULL DEFAULT '',
    head_sha                 TEXT        NOT NULL DEFAULT '',
    head_branch              TEXT        NOT NULL DEFAULT '',
    workflow_name            TEXT        NOT NULL DEFAULT '',
    status                   TEXT        NOT NULL DEFAULT '',
    conclusion               TEXT        NOT NULL DEFAULT '',
    labels_json              JSONB       NOT NULL DEFAULT '[]'::jsonb,
    runner_id                BIGINT      NOT NULL DEFAULT 0,
    runner_name              TEXT        NOT NULL DEFAULT '',
    started_at               TIMESTAMPTZ,
    completed_at             TIMESTAMPTZ,
    last_delivery_id         TEXT        NOT NULL DEFAULT '',
    observed_from_api_at     TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_github_workflow_jobs_run
    ON github_workflow_jobs (provider_repository_id, provider_run_id, provider_run_attempt, provider_job_id);

CREATE TABLE github_job_shapes (
    job_shape_id             TEXT        PRIMARY KEY CHECK (job_shape_id <> ''),
    org_id                   TEXT        NOT NULL DEFAULT '',
    installation_binding_id  UUID,
    repository_binding_id    UUID,
    provider_installation_id BIGINT      NOT NULL DEFAULT 0,
    provider_repository_id   BIGINT      NOT NULL DEFAULT 0,
    repository_full_name     TEXT        NOT NULL DEFAULT '',
    workflow_path            TEXT        NOT NULL DEFAULT '',
    workflow_name            TEXT        NOT NULL DEFAULT '',
    job_name                 TEXT        NOT NULL DEFAULT '',
    matrix_key               TEXT        NOT NULL DEFAULT '',
    runner_class             TEXT        NOT NULL DEFAULT '',
    runner_labels_json       JSONB       NOT NULL DEFAULT '[]'::jsonb,
    cache_manifest_sha256    TEXT        NOT NULL DEFAULT '',
    trust_class              TEXT        NOT NULL DEFAULT '',
    canonical_json           JSONB       NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_github_job_shapes_repository
    ON github_job_shapes (provider_repository_id, runner_class, updated_at DESC);

CREATE TABLE github_workflow_runs (
    org_id                       TEXT        NOT NULL DEFAULT '',
    installation_binding_id      UUID,
    repository_binding_id        UUID,
    provider_installation_id     BIGINT      NOT NULL DEFAULT 0,
    provider_repository_id       BIGINT      NOT NULL,
    provider_run_id              BIGINT      NOT NULL,
    provider_run_attempt         BIGINT      NOT NULL DEFAULT 0,
    repository_full_name         TEXT        NOT NULL DEFAULT '',
    event_name                   TEXT        NOT NULL DEFAULT '',
    head_sha                     TEXT        NOT NULL DEFAULT '',
    head_branch                  TEXT        NOT NULL DEFAULT '',
    head_repository_full_name    TEXT        NOT NULL DEFAULT '',
    base_sha                     TEXT        NOT NULL DEFAULT '',
    base_branch                  TEXT        NOT NULL DEFAULT '',
    workflow_path                TEXT        NOT NULL DEFAULT '',
    pull_request_number          BIGINT      NOT NULL DEFAULT 0,
    commit_count                 BIGINT      NOT NULL DEFAULT 0,
    last_delivery_id             TEXT        NOT NULL DEFAULT '',
    observed_from_api_at         TIMESTAMPTZ,
    created_at                   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider_installation_id, provider_repository_id, provider_run_id, provider_run_attempt)
);

CREATE TABLE github_provider_demands (
    demand_id                UUID        PRIMARY KEY,
    provider_job_id          BIGINT      NOT NULL UNIQUE REFERENCES github_workflow_jobs(provider_job_id) ON DELETE CASCADE,
    org_id                   TEXT        NOT NULL DEFAULT '',
    installation_binding_id  UUID,
    repository_binding_id    UUID,
    provider_installation_id BIGINT      NOT NULL DEFAULT 0,
    provider_repository_id   BIGINT      NOT NULL DEFAULT 0,
    repository_full_name     TEXT        NOT NULL DEFAULT '',
    provider_run_id          BIGINT      NOT NULL DEFAULT 0,
    provider_run_attempt     BIGINT      NOT NULL DEFAULT 0,
    job_shape_id             TEXT        NOT NULL DEFAULT '',
    trust_class              TEXT        NOT NULL DEFAULT '',
    runner_class             TEXT        NOT NULL DEFAULT '',
    state                    TEXT        NOT NULL CHECK (state <> ''),
    primary_problem_type     TEXT        NOT NULL DEFAULT '',
    primary_problem_code     TEXT        NOT NULL DEFAULT '',
    primary_problem_status   INTEGER     NOT NULL DEFAULT 0 CHECK (primary_problem_status BETWEEN 0 AND 599),
    primary_problem_title    TEXT        NOT NULL DEFAULT '',
    primary_problem_detail   TEXT        NOT NULL DEFAULT '',
    problem_count            INTEGER     NOT NULL DEFAULT 0 CHECK (problem_count >= 0),
    last_delivery_id         TEXT        NOT NULL DEFAULT '',
    claimed_at               TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_github_provider_demands_state
    ON github_provider_demands (state, updated_at);

CREATE TABLE github_provider_demand_problems (
    provider_job_id          BIGINT      NOT NULL REFERENCES github_provider_demands(provider_job_id) ON DELETE CASCADE,
    problem_seq              INTEGER     NOT NULL CHECK (problem_seq > 0),
    phase                    TEXT        NOT NULL CHECK (phase <> ''),
    problem_type             TEXT        NOT NULL CHECK (problem_type <> ''),
    problem_code             TEXT        NOT NULL CHECK (problem_code <> ''),
    title                    TEXT        NOT NULL CHECK (title <> ''),
    detail                   TEXT        NOT NULL DEFAULT '',
    status                   INTEGER     NOT NULL DEFAULT 0 CHECK (status BETWEEN 0 AND 599),
    retryable                BOOLEAN     NOT NULL DEFAULT false,
    pointer                  TEXT        NOT NULL DEFAULT '',
    observed_at              TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (provider_job_id, problem_seq)
);

CREATE INDEX idx_github_provider_demand_problems_demand
    ON github_provider_demand_problems (provider_job_id, observed_at);

CREATE TABLE github_provider_outbox (
    outbox_id                UUID        PRIMARY KEY,
    command_kind             TEXT        NOT NULL CHECK (command_kind <> ''),
    org_id                   TEXT        NOT NULL DEFAULT '',
    installation_binding_id  UUID,
    repository_binding_id    UUID,
    provider_job_id          BIGINT      NOT NULL DEFAULT 0,
    provider_run_id          BIGINT      NOT NULL DEFAULT 0,
    provider_run_attempt     BIGINT      NOT NULL DEFAULT 0,
    provider_installation_id BIGINT      NOT NULL DEFAULT 0,
    provider_repository_id   BIGINT      NOT NULL DEFAULT 0,
    sandbox_execution_id     UUID,
    sandbox_attempt_id       UUID,
    state                    TEXT        NOT NULL CHECK (state <> ''),
    command_sha256           TEXT        NOT NULL CHECK (command_sha256 <> ''),
    payload_json             JSONB       NOT NULL,
    attempt_count            INTEGER     NOT NULL DEFAULT 0,
    next_attempt_at          TIMESTAMPTZ,
    processed_at             TIMESTAMPTZ,
    failure_reason           TEXT        NOT NULL DEFAULT '',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (command_kind, command_sha256)
);

CREATE INDEX idx_github_provider_outbox_ready
    ON github_provider_outbox (next_attempt_at NULLS FIRST, created_at)
    WHERE state IN ('pending', 'retryable');

CREATE TABLE github_provider_surface_commands (
    surface_id               UUID        PRIMARY KEY,
    command_key              TEXT        NOT NULL UNIQUE CHECK (command_key <> ''),
    command_kind             TEXT        NOT NULL CHECK (command_kind <> ''),
    org_id                   TEXT        NOT NULL DEFAULT '',
    installation_binding_id  UUID,
    repository_binding_id    UUID,
    provider_installation_id BIGINT      NOT NULL DEFAULT 0,
    provider_repository_id   BIGINT      NOT NULL DEFAULT 0,
    repository_full_name     TEXT        NOT NULL DEFAULT '',
    provider_run_id          BIGINT      NOT NULL DEFAULT 0,
    provider_run_attempt     BIGINT      NOT NULL DEFAULT 0,
    provider_job_id          BIGINT      NOT NULL DEFAULT 0,
    runner_id                BIGINT      NOT NULL DEFAULT 0,
    runner_name              TEXT        NOT NULL DEFAULT '',
    runner_class             TEXT        NOT NULL DEFAULT '',
    state                    TEXT        NOT NULL CHECK (state <> ''),
    primary_problem_type     TEXT        NOT NULL DEFAULT '',
    primary_problem_code     TEXT        NOT NULL DEFAULT '',
    primary_problem_status   INTEGER     NOT NULL DEFAULT 0 CHECK (primary_problem_status BETWEEN 0 AND 599),
    primary_problem_title    TEXT        NOT NULL DEFAULT '',
    primary_problem_detail   TEXT        NOT NULL DEFAULT '',
    problem_count            INTEGER     NOT NULL DEFAULT 0 CHECK (problem_count >= 0),
    attempt_count            INTEGER     NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at          TIMESTAMPTZ,
    locked_at                TIMESTAMPTZ,
    completed_at             TIMESTAMPTZ,
    failed_at                TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_github_provider_surface_commands_ready
    ON github_provider_surface_commands (next_attempt_at NULLS FIRST, created_at)
    WHERE state IN ('pending', 'retryable');
CREATE INDEX idx_github_provider_surface_commands_running
    ON github_provider_surface_commands (locked_at, updated_at)
    WHERE state = 'running';
CREATE INDEX idx_github_provider_surface_commands_job
    ON github_provider_surface_commands (provider_job_id, updated_at)
    WHERE provider_job_id <> 0;

CREATE TABLE github_provider_surface_problems (
    surface_id               UUID        NOT NULL REFERENCES github_provider_surface_commands(surface_id) ON DELETE CASCADE,
    problem_seq              INTEGER     NOT NULL CHECK (problem_seq > 0),
    phase                    TEXT        NOT NULL CHECK (phase <> ''),
    problem_type             TEXT        NOT NULL CHECK (problem_type <> ''),
    problem_code             TEXT        NOT NULL CHECK (problem_code <> ''),
    title                    TEXT        NOT NULL CHECK (title <> ''),
    detail                   TEXT        NOT NULL DEFAULT '',
    status                   INTEGER     NOT NULL DEFAULT 0 CHECK (status BETWEEN 0 AND 599),
    retryable                BOOLEAN     NOT NULL DEFAULT false,
    pointer                  TEXT        NOT NULL DEFAULT '',
    observed_at              TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (surface_id, problem_seq)
);

CREATE INDEX idx_github_provider_surface_problems_surface
    ON github_provider_surface_problems (surface_id, observed_at);

CREATE TABLE github_runner_instances (
    runner_name              TEXT        PRIMARY KEY CHECK (runner_name <> ''),
    origin_provider_job_id   BIGINT      NOT NULL REFERENCES github_workflow_jobs(provider_job_id) ON DELETE CASCADE,
    origin_demand_id         UUID        REFERENCES github_provider_demands(demand_id) ON DELETE SET NULL,
    org_id                   TEXT        NOT NULL DEFAULT '',
    installation_binding_id  UUID,
    repository_binding_id    UUID,
    provider_installation_id BIGINT      NOT NULL DEFAULT 0,
    provider_repository_id   BIGINT      NOT NULL DEFAULT 0,
    runner_id                BIGINT      NOT NULL DEFAULT 0,
    runner_class             TEXT        NOT NULL DEFAULT '',
    jit_config_sha256        TEXT        NOT NULL DEFAULT '',
    sandbox_allocation_id    UUID,
    sandbox_execution_id     UUID,
    sandbox_attempt_id       UUID,
    assignment_deadline_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    state                    TEXT        NOT NULL CHECK (state <> ''),
    primary_problem_type     TEXT        NOT NULL DEFAULT '',
    primary_problem_code     TEXT        NOT NULL DEFAULT '',
    primary_problem_status   INTEGER     NOT NULL DEFAULT 0 CHECK (primary_problem_status BETWEEN 0 AND 599),
    primary_problem_title    TEXT        NOT NULL DEFAULT '',
    primary_problem_detail   TEXT        NOT NULL DEFAULT '',
    problem_count            INTEGER     NOT NULL DEFAULT 0 CHECK (problem_count >= 0),
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_github_runner_instances_runner_id
    ON github_runner_instances (provider_installation_id, provider_repository_id, runner_id)
    WHERE runner_id <> 0;
CREATE INDEX idx_github_runner_instances_origin
    ON github_runner_instances (origin_provider_job_id, updated_at);
CREATE INDEX idx_github_runner_instances_capacity
    ON github_runner_instances (provider_repository_id, runner_class, state, assignment_deadline_at, updated_at);

CREATE TABLE github_runner_instance_problems (
    runner_name              TEXT        NOT NULL REFERENCES github_runner_instances(runner_name) ON DELETE CASCADE,
    problem_seq              INTEGER     NOT NULL CHECK (problem_seq > 0),
    phase                    TEXT        NOT NULL CHECK (phase <> ''),
    problem_type             TEXT        NOT NULL CHECK (problem_type <> ''),
    problem_code             TEXT        NOT NULL CHECK (problem_code <> ''),
    title                    TEXT        NOT NULL CHECK (title <> ''),
    detail                   TEXT        NOT NULL DEFAULT '',
    status                   INTEGER     NOT NULL DEFAULT 0 CHECK (status BETWEEN 0 AND 599),
    retryable                BOOLEAN     NOT NULL DEFAULT false,
    pointer                  TEXT        NOT NULL DEFAULT '',
    observed_at              TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (runner_name, problem_seq)
);

CREATE INDEX idx_github_runner_instance_problems_runner
    ON github_runner_instance_problems (runner_name, observed_at);

CREATE TABLE github_job_assignments (
    provider_job_id          BIGINT      PRIMARY KEY REFERENCES github_workflow_jobs(provider_job_id) ON DELETE CASCADE,
    runner_name              TEXT        NOT NULL CHECK (runner_name <> ''),
    runner_id                BIGINT      NOT NULL DEFAULT 0,
    observed_from            TEXT        NOT NULL CHECK (observed_from <> ''),
    delivery_id              TEXT        NOT NULL DEFAULT '',
    observed_at              TIMESTAMPTZ NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_github_job_assignments_runner_name
    ON github_job_assignments (runner_name);
CREATE INDEX idx_github_job_assignments_runner_id
    ON github_job_assignments (runner_id)
    WHERE runner_id <> 0;

CREATE TABLE github_terminal_job_evidence (
    terminal_evidence_id     UUID        PRIMARY KEY,
    provider_job_id          BIGINT      NOT NULL UNIQUE REFERENCES github_workflow_jobs(provider_job_id) ON DELETE CASCADE,
    org_id                   TEXT        NOT NULL DEFAULT '',
    installation_binding_id  UUID,
    repository_binding_id    UUID,
    provider_installation_id BIGINT      NOT NULL DEFAULT 0,
    provider_repository_id   BIGINT      NOT NULL DEFAULT 0,
    provider_run_id          BIGINT      NOT NULL DEFAULT 0,
    provider_run_attempt     BIGINT      NOT NULL DEFAULT 0,
    sandbox_allocation_id    UUID,
    sandbox_execution_id     UUID,
    sandbox_attempt_id       UUID,
    runner_id                BIGINT      NOT NULL DEFAULT 0,
    runner_name              TEXT        NOT NULL DEFAULT '',
    job_shape_id             TEXT        NOT NULL DEFAULT '',
    trust_class              TEXT        NOT NULL DEFAULT '',
    status                   TEXT        NOT NULL DEFAULT '',
    conclusion               TEXT        NOT NULL DEFAULT '',
    source                   TEXT        NOT NULL CHECK (source <> ''),
    delivery_id              TEXT        NOT NULL DEFAULT '',
    observed_at              TIMESTAMPTZ NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE github_golden_snapshot_barriers (
    barrier_id               UUID        PRIMARY KEY,
    terminal_evidence_id     UUID        NOT NULL REFERENCES github_terminal_job_evidence(terminal_evidence_id) ON DELETE CASCADE,
    provider_job_id          BIGINT      NOT NULL UNIQUE,
    org_id                   TEXT        NOT NULL DEFAULT '',
    installation_binding_id  UUID,
    repository_binding_id    UUID,
    provider_run_id          BIGINT      NOT NULL DEFAULT 0,
    provider_run_attempt     BIGINT      NOT NULL DEFAULT 0,
    sandbox_execution_id     UUID,
    sandbox_attempt_id       UUID,
    job_shape_id             TEXT        NOT NULL DEFAULT '',
    trust_class              TEXT        NOT NULL DEFAULT '',
    state                    TEXT        NOT NULL CHECK (state <> ''),
    failure_reason           TEXT        NOT NULL DEFAULT '',
    requested_at             TIMESTAMPTZ NOT NULL,
    completed_at             TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);
