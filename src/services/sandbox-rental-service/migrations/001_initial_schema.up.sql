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

-- Per-org VM resource ceilings. Defaults mirror sandbox runtime DefaultBounds.
CREATE TABLE vm_resource_bounds (
    org_id             TEXT        PRIMARY KEY CHECK (length(btrim(org_id)) > 0),
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
    org_id                  TEXT        NOT NULL CHECK (length(btrim(org_id)) > 0),
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
    org_id       TEXT        NOT NULL CHECK (length(btrim(org_id)) > 0),
    attempt_id   UUID        NOT NULL REFERENCES execution_attempts(attempt_id) ON DELETE CASCADE,
    seq          INTEGER     NOT NULL,
    stream       TEXT        NOT NULL,
    chunk        TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (attempt_id, seq)
);

CREATE INDEX idx_execution_logs_org_attempt_seq
    ON execution_logs (org_id, attempt_id, seq);

-- ─── Runner-class filesystem composition ───────────────────────────────────
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
-- the substrate image and composes the GitHub Actions runner toolchain
-- read-only. source_ref values match the
-- composable image catalog in authored substrate topology:
-- firecracker.images, which the daemon resolves to ZFS snapshots at
-- lease boot. Per-execution writable volume mounts arrive via StartExecRequest,
-- not this table.
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

CREATE TABLE runner_provider_repositories (
    provider               TEXT        NOT NULL CHECK (provider <> ''),
    provider_repository_id BIGINT      NOT NULL,
    org_id                 TEXT        NOT NULL CHECK (length(btrim(org_id)) > 0),
    project_id             UUID,
    source_repository_id   UUID,
    provider_owner         TEXT        NOT NULL CHECK (provider_owner <> ''),
    provider_repo          TEXT        NOT NULL CHECK (provider_repo <> ''),
    repository_full_name   TEXT        NOT NULL CHECK (repository_full_name <> ''),
    active                 BOOLEAN     NOT NULL DEFAULT true,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, provider_repository_id),
    UNIQUE (provider, repository_full_name)
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
    provider_run_attempt   BIGINT      NOT NULL DEFAULT 0,
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

CREATE TABLE provider_workflow_runs (
    provider               TEXT        NOT NULL DEFAULT 'github' CHECK (provider <> ''),
    provider_installation_id BIGINT    NOT NULL DEFAULT 0,
    provider_repository_id BIGINT      NOT NULL,
    provider_run_id        BIGINT      NOT NULL,
    provider_run_attempt   BIGINT      NOT NULL DEFAULT 0,
    repository_full_name   TEXT        NOT NULL,
    event_name             TEXT        NOT NULL DEFAULT '',
    head_sha               TEXT        NOT NULL DEFAULT '',
    head_branch            TEXT        NOT NULL DEFAULT '',
    head_repository_full_name TEXT     NOT NULL DEFAULT '',
    base_sha               TEXT        NOT NULL DEFAULT '',
    base_branch            TEXT        NOT NULL DEFAULT '',
    pull_request_number    BIGINT      NOT NULL DEFAULT 0,
    workflow_path          TEXT        NOT NULL DEFAULT '',
    commit_count           BIGINT,
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, provider_installation_id, provider_repository_id, provider_run_id, provider_run_attempt)
);

CREATE INDEX idx_provider_workflow_runs_run
    ON provider_workflow_runs (provider, provider_repository_id, provider_run_id, provider_run_attempt);

CREATE INDEX idx_runner_jobs_installation_status
    ON runner_jobs (provider, provider_installation_id, status, updated_at DESC);
CREATE INDEX idx_runner_jobs_repository_status
    ON runner_jobs (provider, provider_repository_id, status, updated_at DESC);
CREATE INDEX idx_runner_jobs_runner
    ON runner_jobs (provider, runner_id, runner_name)
    WHERE runner_id <> 0 OR runner_name <> '';

CREATE TABLE runner_job_cache_manifests (
    provider          TEXT        NOT NULL CHECK (provider <> ''),
    provider_job_id   BIGINT      NOT NULL,
    source_kind       TEXT        NOT NULL CHECK (source_kind <> ''),
    source_path       TEXT        NOT NULL CHECK (source_path <> ''),
    source_sha        TEXT        NOT NULL CHECK (source_sha <> ''),
    content_sha256    TEXT        NOT NULL CHECK (content_sha256 ~ '^[0-9a-f]{64}$'),
    content_bytes     BYTEA       NOT NULL CHECK (octet_length(content_bytes) <= 65536),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, provider_job_id),
    FOREIGN KEY (provider, provider_job_id) REFERENCES runner_jobs(provider, provider_job_id) ON DELETE CASCADE
);

CREATE INDEX idx_runner_job_cache_manifests_source
    ON runner_job_cache_manifests (provider, source_sha, updated_at DESC);

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
    WHERE requested_for_provider_job_id <> 0
      AND state IN ('pending', 'jit_creating', 'jit_created', 'vm_submitted', 'runner_config_fetched');

CREATE TABLE runner_bootstrap_configs (
    allocation_id      UUID        PRIMARY KEY REFERENCES runner_allocations(allocation_id) ON DELETE CASCADE,
    attempt_id         UUID        NOT NULL REFERENCES execution_attempts(attempt_id) ON DELETE CASCADE,
    fetch_token_hash   TEXT        NOT NULL UNIQUE CHECK (fetch_token_hash <> ''),
    bootstrap_kind     TEXT        NOT NULL CHECK (bootstrap_kind <> ''),
    bootstrap_secret_name TEXT     NOT NULL CHECK (bootstrap_secret_name <> ''),
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

-- ─── Recurring execution schedules (Temporal-backed) ───────────────────────

CREATE TABLE execution_schedules (
    schedule_id           UUID        PRIMARY KEY,
    org_id                TEXT        NOT NULL CHECK (length(btrim(org_id)) > 0),
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


-- Durable cache model.

CREATE TABLE job_shape (
    job_shape_id             UUID        PRIMARY KEY,
    repository_id            BIGINT      NOT NULL CHECK (repository_id > 0),
    provider                 TEXT        NOT NULL,
    workflow_identity        TEXT        NOT NULL DEFAULT '',
    called_workflow_identity TEXT        NOT NULL DEFAULT '',
    job_identity             TEXT        NOT NULL DEFAULT '',
    matrix_key               TEXT        NOT NULL DEFAULT '',
    runner_class             TEXT        NOT NULL,
    guest_arch               TEXT        NOT NULL DEFAULT 'x86_64',
    platform_image_id        TEXT        NOT NULL DEFAULT '',
    kernel_image_id          TEXT        NOT NULL DEFAULT '',
    runner_toolchain_image_id TEXT       NOT NULL DEFAULT '',
    cache_spec_hash          TEXT        NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (
        repository_id, provider, workflow_identity, called_workflow_identity,
        job_identity, matrix_key, runner_class, guest_arch, platform_image_id,
        kernel_image_id, runner_toolchain_image_id, cache_spec_hash
    )
);

CREATE TABLE durable_scope (
    durable_scope_id       UUID        PRIMARY KEY,
    org_id                 TEXT        NOT NULL CHECK (org_id <> ''),
    repository_id          BIGINT      NOT NULL CHECK (repository_id > 0),
    provider               TEXT        NOT NULL,
    provider_repository_id BIGINT      NOT NULL CHECK (provider_repository_id > 0),
    scope_kind             TEXT        NOT NULL,
    scope_ref              TEXT        NOT NULL,
    job_shape_id           UUID        NOT NULL REFERENCES job_shape(job_shape_id) ON DELETE RESTRICT,
    cache_name             TEXT        NOT NULL CHECK (cache_name <> ''),
    trust_class            TEXT        NOT NULL,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, repository_id, provider, provider_repository_id, scope_kind, scope_ref, job_shape_id, cache_name, trust_class)
);

CREATE TABLE durable_operation (
    operation_id            UUID        PRIMARY KEY,
    execution_id            UUID        NOT NULL REFERENCES executions(execution_id) ON DELETE CASCADE,
    attempt_id              UUID        NOT NULL REFERENCES execution_attempts(attempt_id) ON DELETE CASCADE,
    allocation_id           UUID        REFERENCES runner_allocations(allocation_id) ON DELETE SET NULL,
    durable_scope_id        UUID        NOT NULL REFERENCES durable_scope(durable_scope_id) ON DELETE RESTRICT,
    source_generation_id    UUID,
    source_snapshot_ref     TEXT        NOT NULL DEFAULT '',
    source_skip_reason      TEXT        NOT NULL DEFAULT '',
    candidate_generation_id UUID        NOT NULL,
    mount_name              TEXT        NOT NULL,
    internal_mount_path     TEXT        NOT NULL,
    bind_paths_json         JSONB       NOT NULL DEFAULT '[]'::jsonb,
    trust_class             TEXT        NOT NULL,
    promotion_eligible      BOOLEAN     NOT NULL DEFAULT false,
    required                BOOLEAN     NOT NULL DEFAULT false,
    sort_order              INTEGER     NOT NULL DEFAULT 0,
    requested_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    host_accepted_at        TIMESTAMPTZ,
    mounted_at              TIMESTAMPTZ,
    seal_started_at         TIMESTAMPTZ,
    sealed_at               TIMESTAMPTZ,
    result_recorded_at      TIMESTAMPTZ,
    final_state             TEXT        NOT NULL CHECK (final_state IN ('requested', 'mounted', 'committed', 'skipped', 'failed')),
    failure_reason          TEXT        NOT NULL DEFAULT ''
);

CREATE INDEX idx_durable_operation_attempt
    ON durable_operation (attempt_id, final_state, requested_at DESC);
CREATE INDEX idx_durable_operation_scope
    ON durable_operation (durable_scope_id, final_state, requested_at DESC);

CREATE TABLE durable_generation (
    durable_generation_id UUID        PRIMARY KEY,
    durable_scope_id      UUID        NOT NULL REFERENCES durable_scope(durable_scope_id) ON DELETE RESTRICT,
    operation_id          UUID        NOT NULL REFERENCES durable_operation(operation_id) ON DELETE RESTRICT,
    source_generation_id  UUID,
    head_sha              TEXT        NOT NULL DEFAULT '',
    tree_hash             TEXT        NOT NULL DEFAULT '',
    provider_run_id       BIGINT      NOT NULL DEFAULT 0,
    provider_run_attempt  BIGINT      NOT NULL DEFAULT 0,
    provider_job_id       BIGINT      NOT NULL DEFAULT 0,
    result                TEXT        NOT NULL,
    promotion_eligible    BOOLEAN     NOT NULL DEFAULT false,
    state                 TEXT        NOT NULL CHECK (state IN ('committed', 'retained', 'invalidated', 'reapable', 'reaped')),
    zfs_snapshot_ref      TEXT        NOT NULL,
    zfs_snapshot_guid     TEXT        NOT NULL DEFAULT '',
    used_bytes            BIGINT      NOT NULL DEFAULT 0,
    written_bytes         BIGINT      NOT NULL DEFAULT 0,
    sealed_at             TIMESTAMPTZ NOT NULL,
    committed_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at            TIMESTAMPTZ,
    UNIQUE (operation_id)
);

ALTER TABLE durable_operation
    ADD CONSTRAINT durable_operation_source_generation_fk
    FOREIGN KEY (source_generation_id) REFERENCES durable_generation(durable_generation_id) ON DELETE SET NULL;

ALTER TABLE durable_generation
    ADD CONSTRAINT durable_generation_source_generation_fk
    FOREIGN KEY (source_generation_id) REFERENCES durable_generation(durable_generation_id) ON DELETE SET NULL;

CREATE INDEX idx_durable_generation_scope_committed
    ON durable_generation (durable_scope_id, committed_at DESC);
CREATE INDEX idx_durable_generation_run
    ON durable_generation (provider_run_id, provider_run_attempt, provider_job_id, head_sha);
CREATE INDEX idx_durable_generation_retention
    ON durable_generation (state, expires_at, committed_at);
CREATE INDEX idx_durable_generation_eviction
    ON durable_generation (state, last_used_at, committed_at);

CREATE TABLE durable_current_pointer (
    durable_scope_id          UUID        PRIMARY KEY REFERENCES durable_scope(durable_scope_id) ON DELETE CASCADE,
    current_generation_id     UUID        REFERENCES durable_generation(durable_generation_id) ON DELETE SET NULL,
    promoted_by_operation_id  UUID        REFERENCES durable_operation(operation_id) ON DELETE SET NULL,
    promoted_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE golden_vm_operation (
    operation_id                    UUID        PRIMARY KEY,
    execution_id                    UUID        NOT NULL REFERENCES executions(execution_id) ON DELETE CASCADE,
    attempt_id                      UUID        NOT NULL REFERENCES execution_attempts(attempt_id) ON DELETE CASCADE,
    allocation_id                   UUID        REFERENCES runner_allocations(allocation_id) ON DELETE SET NULL,
    org_id                          TEXT        NOT NULL CHECK (org_id <> ''),
    repository_id                   BIGINT      NOT NULL CHECK (repository_id > 0),
    provider                        TEXT        NOT NULL,
    provider_repository_id          BIGINT      NOT NULL CHECK (provider_repository_id > 0),
    scope_kind                      TEXT        NOT NULL,
    scope_ref                       TEXT        NOT NULL,
    job_shape_id                    UUID        NOT NULL REFERENCES job_shape(job_shape_id) ON DELETE RESTRICT,
    trust_class                     TEXT        NOT NULL,
    source_generation_set_hash      TEXT        NOT NULL DEFAULT '',
    generation_set_hash             TEXT        NOT NULL DEFAULT '',
    candidate_golden_vm_snapshot_id UUID        NOT NULL,
    promotion_eligible              BOOLEAN     NOT NULL DEFAULT false,
    provider_run_id                 BIGINT      NOT NULL DEFAULT 0,
    provider_run_attempt            BIGINT      NOT NULL DEFAULT 0,
    provider_job_id                 BIGINT      NOT NULL DEFAULT 0,
    head_sha                        TEXT        NOT NULL DEFAULT '',
    lease_id                        TEXT        NOT NULL DEFAULT '',
    exec_id                         TEXT        NOT NULL DEFAULT '',
    create_job_id                   BIGINT      NOT NULL DEFAULT 0,
    snapshot_key                    TEXT        NOT NULL DEFAULT '',
    root_snapshot_ref               TEXT        NOT NULL DEFAULT '',
    root_snapshot_guid              TEXT        NOT NULL DEFAULT '',
    vmstate_artifact_ref            TEXT        NOT NULL DEFAULT '',
    memory_artifact_ref             TEXT        NOT NULL DEFAULT '',
    mount_snapshots_json            JSONB       NOT NULL DEFAULT '[]'::jsonb,
    state_bytes                     BIGINT      NOT NULL DEFAULT 0,
    memory_bytes                    BIGINT      NOT NULL DEFAULT 0,
    requested_at                    TIMESTAMPTZ NOT NULL DEFAULT now(),
    create_queued_at                TIMESTAMPTZ,
    creating_started_at             TIMESTAMPTZ,
    created_at                      TIMESTAMPTZ,
    publishing_started_at           TIMESTAMPTZ,
    published_at                    TIMESTAMPTZ,
    promoting_started_at            TIMESTAMPTZ,
    promoted_at                     TIMESTAMPTZ,
    result_recorded_at              TIMESTAMPTZ,
    state                           TEXT        NOT NULL CHECK (state IN ('requested', 'create_queued', 'creating', 'created', 'publishing', 'published', 'promoting', 'committed', 'skipped', 'failed')),
    failure_reason                  TEXT        NOT NULL DEFAULT '',
    updated_at                      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (candidate_golden_vm_snapshot_id)
);

CREATE INDEX idx_golden_vm_operation_attempt
    ON golden_vm_operation (attempt_id, state, requested_at DESC);
CREATE INDEX idx_golden_vm_operation_scope
    ON golden_vm_operation (org_id, provider_repository_id, scope_kind, scope_ref, job_shape_id, trust_class, requested_at DESC);
CREATE INDEX idx_golden_vm_operation_state
    ON golden_vm_operation (state, updated_at);
CREATE INDEX idx_golden_vm_operation_run
    ON golden_vm_operation (provider_run_id, provider_run_attempt, provider_job_id, head_sha);

CREATE TABLE golden_vm_snapshot (
    golden_vm_snapshot_id       UUID        PRIMARY KEY,
    operation_id                UUID        NOT NULL REFERENCES golden_vm_operation(operation_id) ON DELETE RESTRICT,
    org_id                      TEXT        NOT NULL CHECK (org_id <> ''),
    repository_id               BIGINT      NOT NULL CHECK (repository_id > 0),
    provider                    TEXT        NOT NULL,
    provider_repository_id      BIGINT      NOT NULL CHECK (provider_repository_id > 0),
    scope_kind                  TEXT        NOT NULL,
    scope_ref                   TEXT        NOT NULL,
    job_shape_id                UUID        NOT NULL REFERENCES job_shape(job_shape_id) ON DELETE RESTRICT,
    trust_class                 TEXT        NOT NULL,
    generation_set_hash         TEXT        NOT NULL,
    root_snapshot_ref           TEXT        NOT NULL,
    root_snapshot_guid          TEXT        NOT NULL DEFAULT '',
    snapshot_key                TEXT        NOT NULL,
    vmstate_artifact_ref        TEXT        NOT NULL,
    memory_artifact_ref         TEXT        NOT NULL,
    state_bytes                 BIGINT      NOT NULL DEFAULT 0,
    memory_bytes                BIGINT      NOT NULL DEFAULT 0,
    drive_manifest_hash         TEXT        NOT NULL DEFAULT '',
    mount_manifest_hash         TEXT        NOT NULL DEFAULT '',
    firecracker_abi_hash        TEXT        NOT NULL DEFAULT '',
    host_abi_hash               TEXT        NOT NULL DEFAULT '',
    network_model_hash          TEXT        NOT NULL DEFAULT '',
    vsock_model_hash            TEXT        NOT NULL DEFAULT '',
    clock_model_hash            TEXT        NOT NULL DEFAULT '',
    vmproto_version             INTEGER     NOT NULL DEFAULT 0,
    after_restore_hook_version  TEXT        NOT NULL DEFAULT '',
    before_snapshot_hook_version TEXT       NOT NULL DEFAULT '',
    warm_profile_hash           TEXT        NOT NULL DEFAULT '',
    vcpus                       INTEGER     NOT NULL DEFAULT 0,
    memory_mib                  INTEGER     NOT NULL DEFAULT 0,
    provider_run_id             BIGINT      NOT NULL DEFAULT 0,
    provider_run_attempt        BIGINT      NOT NULL DEFAULT 0,
    provider_job_id             BIGINT      NOT NULL DEFAULT 0,
    head_sha                    TEXT        NOT NULL DEFAULT '',
    tree_hash                   TEXT        NOT NULL DEFAULT '',
    state                       TEXT        NOT NULL CHECK (state IN ('candidate', 'current', 'retained', 'invalidated', 'reapable', 'reaped')),
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at                  TIMESTAMPTZ,
    reaped_at                   TIMESTAMPTZ,
    UNIQUE (snapshot_key)
);

CREATE INDEX idx_golden_vm_snapshot_scope
    ON golden_vm_snapshot (org_id, provider_repository_id, scope_kind, scope_ref, job_shape_id, trust_class, state, created_at DESC);
CREATE INDEX idx_golden_vm_snapshot_run
    ON golden_vm_snapshot (provider_run_id, provider_run_attempt, provider_job_id, head_sha);
CREATE INDEX idx_golden_vm_snapshot_retention
    ON golden_vm_snapshot (state, expires_at, created_at);
CREATE INDEX idx_golden_vm_snapshot_org_ring_retention
    ON golden_vm_snapshot (org_id, state, last_used_at DESC, created_at DESC, golden_vm_snapshot_id DESC)
    WHERE state IN ('candidate', 'current', 'retained');

CREATE TABLE golden_vm_snapshot_generation (
    golden_vm_snapshot_id UUID    NOT NULL REFERENCES golden_vm_snapshot(golden_vm_snapshot_id) ON DELETE CASCADE,
    durable_scope_id      UUID    NOT NULL REFERENCES durable_scope(durable_scope_id) ON DELETE RESTRICT,
    durable_generation_id UUID    NOT NULL REFERENCES durable_generation(durable_generation_id) ON DELETE RESTRICT,
    cache_name            TEXT    NOT NULL CHECK (cache_name <> ''),
    zfs_snapshot_ref      TEXT    NOT NULL,
    zfs_snapshot_guid     TEXT    NOT NULL DEFAULT '',
    drive_id              TEXT    NOT NULL,
    mount_path            TEXT    NOT NULL,
    bind_paths_json       JSONB   NOT NULL DEFAULT '[]'::jsonb,
    fs_type               TEXT    NOT NULL DEFAULT 'ext4',
    read_only             BOOLEAN NOT NULL DEFAULT false,
    required              BOOLEAN NOT NULL DEFAULT false,
    sort_order            INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (golden_vm_snapshot_id, durable_scope_id)
);

CREATE INDEX idx_golden_vm_snapshot_generation_generation
    ON golden_vm_snapshot_generation (durable_generation_id);

CREATE TABLE golden_vm_current_pointer (
    org_id                        TEXT        NOT NULL CHECK (org_id <> ''),
    repository_id                 BIGINT      NOT NULL CHECK (repository_id > 0),
    provider                      TEXT        NOT NULL,
    provider_repository_id        BIGINT      NOT NULL CHECK (provider_repository_id > 0),
    scope_kind                    TEXT        NOT NULL,
    scope_ref                     TEXT        NOT NULL,
    job_shape_id                  UUID        NOT NULL REFERENCES job_shape(job_shape_id) ON DELETE RESTRICT,
    trust_class                   TEXT        NOT NULL,
    current_golden_vm_snapshot_id UUID        REFERENCES golden_vm_snapshot(golden_vm_snapshot_id) ON DELETE SET NULL,
    promoted_by_operation_id      UUID        REFERENCES golden_vm_operation(operation_id) ON DELETE SET NULL,
    promoted_at                   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, repository_id, provider, provider_repository_id, scope_kind, scope_ref, job_shape_id, trust_class)
);

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
