-- Sandbox rental service control-plane schema.
-- Database: sandbox_rental (one database per service).

-- ─── Runner class catalog ───────────────────────────────────────────────────

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
    'verself-4vcpu-ubuntu-2404',
    'sandbox',
    'Verself 4 vCPU Ubuntu 24.04',
    'ubuntu',
    '24.04',
    'x86_64',
    4,
    16384,
    80,
    'ubuntu-2404-actions-runner'
), (
    'verself-2vcpu-ubuntu-2404',
    'sandbox',
    'Verself 2 vCPU Ubuntu 24.04',
    'ubuntu',
    '24.04',
    'x86_64',
    2,
    8192,
    80,
    'ubuntu-2404-actions-runner'
);

-- Per-org VM resource ceilings. Defaults mirror apiwire.DefaultBounds.
CREATE TABLE vm_resource_bounds (
    org_id             BIGINT      PRIMARY KEY CHECK (org_id > 0),
    min_vcpus          INT         NOT NULL CHECK (min_vcpus > 0),
    max_vcpus          INT         NOT NULL CHECK (max_vcpus >= min_vcpus),
    min_memory_mib     INT         NOT NULL CHECK (min_memory_mib > 0),
    max_memory_mib     INT         NOT NULL CHECK (max_memory_mib >= min_memory_mib),
    min_root_disk_gib  INT         NOT NULL CHECK (min_root_disk_gib > 0),
    max_root_disk_gib  INT         NOT NULL CHECK (max_root_disk_gib >= min_root_disk_gib),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ─── Executions ─────────────────────────────────────────────────────────────

CREATE TABLE executions (
    execution_id            UUID        PRIMARY KEY,
    org_id                  BIGINT      NOT NULL CHECK (org_id > 0),
    actor_id                TEXT        NOT NULL,
    kind                    TEXT        NOT NULL,
    source_kind             TEXT        NOT NULL DEFAULT 'api',
    workload_kind           TEXT        NOT NULL DEFAULT 'direct',
    source_ref              TEXT        NOT NULL DEFAULT '',
    runner_class            TEXT        NOT NULL REFERENCES runner_classes(runner_class),
    external_provider       TEXT        NOT NULL DEFAULT '',
    external_task_id        TEXT        NOT NULL DEFAULT '',
    provider                TEXT        NOT NULL DEFAULT '',
    product_id              TEXT        NOT NULL DEFAULT 'sandbox',
    state                   TEXT        NOT NULL,
    correlation_id          TEXT        NOT NULL DEFAULT '',
    idempotency_key         TEXT        NOT NULL DEFAULT '',
    run_command             TEXT        NOT NULL DEFAULT '',
    max_wall_seconds        BIGINT      NOT NULL DEFAULT 0 CHECK (max_wall_seconds >= 0),
    requested_vcpus         INT         NOT NULL DEFAULT 4     CHECK (requested_vcpus > 0),
    requested_memory_mib    INT         NOT NULL DEFAULT 16384 CHECK (requested_memory_mib > 0),
    requested_root_disk_gib INT         NOT NULL DEFAULT 80    CHECK (requested_root_disk_gib > 0),
    requested_kernel_image  TEXT        NOT NULL DEFAULT 'default',
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
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
    rootfs_provisioned_bytes BIGINT NOT NULL DEFAULT 0,
    boot_time_us             BIGINT NOT NULL DEFAULT 0,
    block_read_bytes         BIGINT NOT NULL DEFAULT 0,
    block_write_bytes        BIGINT NOT NULL DEFAULT 0,
    net_rx_bytes             BIGINT NOT NULL DEFAULT 0,
    net_tx_bytes             BIGINT NOT NULL DEFAULT 0,
    vcpu_exit_count          BIGINT NOT NULL DEFAULT 0,
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
    reserved_charge_units BIGINT  NOT NULL DEFAULT 0,
    billed_charge_units   BIGINT  NOT NULL DEFAULT 0,
    writeoff_charge_units BIGINT  NOT NULL DEFAULT 0,
    cost_per_unit         BIGINT  NOT NULL DEFAULT 0,
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

-- ─── Runner-class filesystem composition and sticky disks ──────────────────
-- runner_class_filesystem_mounts is the product/control-plane definition;
-- execution_filesystem_mounts is the compiled immutable manifest for an execution.

CREATE TABLE runner_class_filesystem_mounts (
    runner_class TEXT        NOT NULL REFERENCES runner_classes(runner_class) ON DELETE CASCADE,
    mount_name   TEXT        NOT NULL CHECK (mount_name <> ''),
    source_ref   TEXT        NOT NULL CHECK (source_ref <> ''),
    mount_path   TEXT        NOT NULL CHECK (mount_path LIKE '/%' AND mount_path <> '/'),
    fs_type      TEXT        NOT NULL DEFAULT 'ext4',
    read_only    BOOLEAN     NOT NULL DEFAULT true,
    sort_order   INTEGER     NOT NULL DEFAULT 0,
    active       BOOLEAN     NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (runner_class, mount_name)
);

CREATE INDEX idx_runner_class_filesystem_mounts_active
    ON runner_class_filesystem_mounts (runner_class, active, sort_order);

-- Runner-class baseline mounts: every verself runner class boots from
-- the substrate image and composes the gh-actions-runner toolchain
-- image read-only at /opt/actions-runner. source_ref values match the
-- composable image catalog in src/cue-renderer/instances/prod/config.cue:
-- firecracker.images, which the daemon resolves to ZFS snapshots at
-- lease boot. Sticky-disk mounts (caches, persistent workspace) are
-- per-execution and arrive via StartExecRequest, not this table.
INSERT INTO runner_class_filesystem_mounts (runner_class, mount_name, source_ref, mount_path, fs_type, read_only, sort_order)
VALUES
    ('verself-4vcpu-ubuntu-2404', 'gh-actions-runner', 'gh-actions-runner', '/opt/actions-runner', 'ext4', true, 10),
    ('verself-2vcpu-ubuntu-2404', 'gh-actions-runner', 'gh-actions-runner', '/opt/actions-runner', 'ext4', true, 10);

CREATE TABLE execution_filesystem_mounts (
    execution_id UUID        NOT NULL REFERENCES executions(execution_id) ON DELETE CASCADE,
    mount_name   TEXT        NOT NULL CHECK (mount_name <> ''),
    source_ref   TEXT        NOT NULL CHECK (source_ref <> ''),
    mount_path   TEXT        NOT NULL CHECK (mount_path LIKE '/%' AND mount_path <> '/'),
    fs_type      TEXT        NOT NULL DEFAULT 'ext4',
    read_only    BOOLEAN     NOT NULL DEFAULT true,
    sort_order   INTEGER     NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (execution_id, mount_name)
);

CREATE INDEX idx_execution_filesystem_mounts_execution
    ON execution_filesystem_mounts (execution_id, sort_order);

CREATE TABLE runner_sticky_disk_generations (
    provider            TEXT        NOT NULL CHECK (provider <> ''),
    provider_installation_id BIGINT  NOT NULL DEFAULT 0,
    provider_repository_id   BIGINT  NOT NULL,
    key_hash            TEXT        NOT NULL CHECK (key_hash <> ''),
    key                 TEXT        NOT NULL CHECK (key <> ''),
    current_generation  BIGINT      NOT NULL CHECK (current_generation >= 0),
    current_source_ref  TEXT        NOT NULL CHECK (current_source_ref <> ''),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, provider_installation_id, provider_repository_id, key_hash)
);

CREATE INDEX idx_runner_sticky_disk_generations_repo
    ON runner_sticky_disk_generations (provider, provider_installation_id, provider_repository_id, updated_at DESC);

-- ─── GitHub App integration ────────────────────────────────────────────────

CREATE TABLE github_accounts (
    account_id    BIGINT      PRIMARY KEY CHECK (account_id > 0),
    account_login TEXT        NOT NULL CHECK (account_login <> ''),
    account_type  TEXT        NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_github_accounts_login
    ON github_accounts (lower(account_login), account_type);

CREATE TABLE github_installations (
    installation_id      BIGINT      PRIMARY KEY CHECK (installation_id > 0),
    account_id           BIGINT      NOT NULL REFERENCES github_accounts(account_id) ON DELETE RESTRICT,
    active               BOOLEAN     NOT NULL DEFAULT true,
    repository_selection TEXT        NOT NULL DEFAULT '',
    permissions_json     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_github_installations_account
    ON github_installations (account_id, active, updated_at DESC);

CREATE TABLE github_installation_connections (
    connection_id         UUID        PRIMARY KEY,
    installation_id       BIGINT      NOT NULL REFERENCES github_installations(installation_id) ON DELETE CASCADE,
    org_id                BIGINT      NOT NULL CHECK (org_id > 0),
    connected_by_actor_id TEXT        NOT NULL CHECK (connected_by_actor_id <> ''),
    state                 TEXT        NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'inactive')),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (installation_id, org_id)
);

CREATE INDEX idx_github_installation_connections_org
    ON github_installation_connections (org_id, state, updated_at DESC);

CREATE TABLE github_installation_states (
    state        TEXT        PRIMARY KEY,
    org_id       BIGINT      NOT NULL CHECK (org_id > 0),
    actor_id     TEXT        NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_github_installation_states_expires
    ON github_installation_states (expires_at);

CREATE TABLE runner_provider_repositories (
    provider               TEXT        NOT NULL CHECK (provider <> ''),
    provider_repository_id BIGINT      NOT NULL,
    org_id                 BIGINT      NOT NULL CHECK (org_id > 0),
    project_id             UUID,
    source_repository_id   UUID,
    provider_owner         TEXT        NOT NULL CHECK (provider_owner <> ''),
    provider_repo          TEXT        NOT NULL CHECK (provider_repo <> ''),
    repository_full_name   TEXT        NOT NULL CHECK (repository_full_name <> ''),
    active                 BOOLEAN     NOT NULL DEFAULT true,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, provider_repository_id),
    UNIQUE (provider, repository_full_name),
    CHECK (provider <> 'forgejo' OR (project_id IS NOT NULL AND source_repository_id IS NOT NULL))
);

CREATE INDEX idx_runner_provider_repositories_org
    ON runner_provider_repositories (org_id, provider, active, updated_at DESC);

CREATE INDEX idx_runner_provider_repositories_source
    ON runner_provider_repositories (source_repository_id)
    WHERE source_repository_id IS NOT NULL;

CREATE INDEX idx_runner_provider_repositories_project
    ON runner_provider_repositories (org_id, project_id, provider, active, updated_at DESC)
    WHERE project_id IS NOT NULL;

CREATE TABLE runner_jobs (
    provider               TEXT        NOT NULL CHECK (provider <> ''),
    provider_job_id        BIGINT      NOT NULL,
    provider_installation_id BIGINT    NOT NULL DEFAULT 0,
    provider_repository_id BIGINT      NOT NULL,
    repository_full_name   TEXT        NOT NULL,
    provider_run_id        BIGINT      NOT NULL DEFAULT 0,
    provider_task_id       BIGINT      NOT NULL DEFAULT 0,
    provider_job_handle    TEXT        NOT NULL DEFAULT '',
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
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, provider_job_id)
);

CREATE INDEX idx_runner_jobs_installation_status
    ON runner_jobs (provider, provider_installation_id, status, updated_at DESC);
CREATE INDEX idx_runner_jobs_repository_status
    ON runner_jobs (provider, provider_repository_id, status, updated_at DESC);
CREATE INDEX idx_runner_jobs_runner
    ON runner_jobs (provider, runner_id, runner_name)
    WHERE runner_id <> 0 OR runner_name <> '';

CREATE TABLE runner_allocations (
    allocation_id                  UUID        PRIMARY KEY,
    provider                       TEXT        NOT NULL CHECK (provider <> ''),
    provider_installation_id       BIGINT      NOT NULL DEFAULT 0,
    provider_repository_id         BIGINT      NOT NULL DEFAULT 0,
    runner_class                   TEXT        NOT NULL REFERENCES runner_classes(runner_class),
    runner_name                    TEXT        NOT NULL,
    provider_runner_id             BIGINT      NOT NULL DEFAULT 0,
    execution_id                   UUID        REFERENCES executions(execution_id) ON DELETE SET NULL,
    attempt_id                     UUID        REFERENCES execution_attempts(attempt_id) ON DELETE SET NULL,
    state                          TEXT        NOT NULL,
    requested_for_provider_job_id  BIGINT      NOT NULL DEFAULT 0,
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

CREATE INDEX idx_runner_allocations_state
    ON runner_allocations (provider, state, updated_at);
CREATE UNIQUE INDEX idx_runner_allocations_execution
    ON runner_allocations (execution_id)
    WHERE execution_id IS NOT NULL;
CREATE UNIQUE INDEX idx_runner_allocations_runner_name
    ON runner_allocations (provider, runner_name)
    WHERE runner_name <> '';
CREATE UNIQUE INDEX idx_runner_allocations_active_job
    ON runner_allocations (provider, requested_for_provider_job_id)
    WHERE requested_for_provider_job_id <> 0 AND state NOT IN ('failed', 'cleaned');

CREATE TABLE runner_bootstrap_configs (
    allocation_id      UUID        PRIMARY KEY REFERENCES runner_allocations(allocation_id) ON DELETE CASCADE,
    attempt_id         UUID        NOT NULL REFERENCES execution_attempts(attempt_id) ON DELETE CASCADE,
    fetch_token_hash   TEXT        NOT NULL UNIQUE CHECK (fetch_token_hash <> ''),
    bootstrap_kind     TEXT        NOT NULL CHECK (bootstrap_kind <> ''),
    bootstrap_payload  TEXT        NOT NULL CHECK (bootstrap_payload <> ''),
    expires_at         TIMESTAMPTZ NOT NULL,
    consumed_at        TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_runner_bootstrap_configs_attempt
    ON runner_bootstrap_configs (attempt_id);
CREATE INDEX idx_runner_bootstrap_configs_expires
    ON runner_bootstrap_configs (expires_at)
    WHERE consumed_at IS NULL;

CREATE TABLE runner_job_bindings (
    binding_id       UUID        PRIMARY KEY,
    allocation_id    UUID        NOT NULL REFERENCES runner_allocations(allocation_id) ON DELETE CASCADE,
    provider         TEXT        NOT NULL CHECK (provider <> ''),
    provider_job_id  BIGINT      NOT NULL,
    provider_runner_id BIGINT    NOT NULL DEFAULT 0,
    runner_name      TEXT        NOT NULL DEFAULT '',
    bound_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (allocation_id),
    UNIQUE (provider, provider_job_id),
    FOREIGN KEY (provider, provider_job_id) REFERENCES runner_jobs(provider, provider_job_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX idx_runner_job_bindings_runner
    ON runner_job_bindings (provider, provider_runner_id, runner_name)
    WHERE provider_runner_id <> 0 OR runner_name <> '';

-- ─── Execution sticky-disk mounts ──────────────────────────────────────────
-- Defined after allocations so the allocation_id FK resolves.

CREATE TABLE execution_sticky_disk_mounts (
    mount_id             UUID        PRIMARY KEY,
    execution_id         UUID        NOT NULL REFERENCES executions(execution_id) ON DELETE CASCADE,
    attempt_id           UUID        NOT NULL REFERENCES execution_attempts(attempt_id) ON DELETE CASCADE,
    allocation_id        UUID        NOT NULL REFERENCES runner_allocations(allocation_id) ON DELETE CASCADE,
    mount_name           TEXT        NOT NULL CHECK (mount_name <> ''),
    key_hash             TEXT        NOT NULL CHECK (key_hash <> ''),
    key                  TEXT        NOT NULL CHECK (key <> ''),
    mount_path           TEXT        NOT NULL CHECK (mount_path LIKE '/%' AND mount_path <> '/'),
    base_generation      BIGINT      NOT NULL CHECK (base_generation >= 0),
    source_ref           TEXT        NOT NULL CHECK (source_ref <> ''),
    target_source_ref    TEXT        NOT NULL CHECK (target_source_ref <> ''),
    save_requested       BOOLEAN     NOT NULL DEFAULT false,
    save_state           TEXT        NOT NULL CHECK (save_state IN ('not_requested', 'requested', 'running', 'committed', 'failed', 'skipped')),
    committed_generation BIGINT      NOT NULL DEFAULT 0 CHECK (committed_generation >= 0),
    committed_snapshot   TEXT        NOT NULL DEFAULT '',
    failure_reason       TEXT        NOT NULL DEFAULT '',
    sort_order           INTEGER     NOT NULL DEFAULT 0,
    requested_at         TIMESTAMPTZ,
    started_at           TIMESTAMPTZ,
    completed_at         TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (attempt_id, key_hash, mount_path),
    UNIQUE (execution_id, mount_name)
);

CREATE INDEX idx_execution_sticky_disk_mounts_attempt_state
    ON execution_sticky_disk_mounts (attempt_id, save_state, requested_at);

-- ─── Recurring execution schedules (Temporal-backed) ───────────────────────

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
    project_id            UUID        NOT NULL,
    source_repository_id  UUID        NOT NULL,
    workflow_path         TEXT        NOT NULL CHECK (workflow_path <> ''),
    ref                   TEXT        NOT NULL DEFAULT '',
    inputs_json           JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, idempotency_key),
    UNIQUE (temporal_schedule_id)
);

CREATE INDEX idx_execution_schedules_org_created
    ON execution_schedules (org_id, created_at DESC, schedule_id);

CREATE INDEX idx_execution_schedules_org_state
    ON execution_schedules (org_id, state, updated_at DESC, schedule_id);

CREATE INDEX idx_execution_schedules_project_created
    ON execution_schedules (org_id, project_id, created_at DESC, schedule_id);

CREATE TABLE execution_schedule_dispatches (
    dispatch_id           UUID        PRIMARY KEY,
    schedule_id           UUID        NOT NULL REFERENCES execution_schedules(schedule_id) ON DELETE CASCADE,
    temporal_workflow_id  TEXT        NOT NULL CHECK (temporal_workflow_id <> ''),
    temporal_run_id       TEXT        NOT NULL CHECK (temporal_run_id <> ''),
    project_id            UUID        NOT NULL,
    source_workflow_run_id UUID,
    workflow_state        TEXT        NOT NULL DEFAULT '',
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

CREATE INDEX idx_execution_schedule_dispatches_workflow_run
    ON execution_schedule_dispatches (source_workflow_run_id)
    WHERE source_workflow_run_id IS NOT NULL;

-- ─── River queue runtime (v0.34.0 end-state) ───────────────────────────────
-- SQL derived from River v0.34.0 riverdriver/riverpgxv5/migration/main
-- (MPL-2.0). Tables reflect the post-v1..v6 schema; the seed rows at the end
-- tell River not to re-apply those baseline migrations. Keep in lockstep with
-- go.mod's River pin.

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
