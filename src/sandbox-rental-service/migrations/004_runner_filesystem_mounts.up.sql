-- Runner-class filesystem composition and zfs-native sticky disks.
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

CREATE TABLE github_sticky_disk_generations (
    installation_id     BIGINT      NOT NULL,
    repository_id       BIGINT      NOT NULL,
    key_hash            TEXT        NOT NULL CHECK (key_hash <> ''),
    key                 TEXT        NOT NULL CHECK (key <> ''),
    current_generation  BIGINT      NOT NULL CHECK (current_generation >= 0),
    current_source_ref  TEXT        NOT NULL CHECK (current_source_ref <> ''),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (installation_id, repository_id, key_hash)
);

CREATE INDEX idx_github_sticky_disk_generations_repo
    ON github_sticky_disk_generations (installation_id, repository_id, updated_at DESC);

CREATE TABLE execution_sticky_disk_mounts (
    mount_id             UUID        PRIMARY KEY,
    execution_id         UUID        NOT NULL REFERENCES executions(execution_id) ON DELETE CASCADE,
    attempt_id           UUID        NOT NULL REFERENCES execution_attempts(attempt_id) ON DELETE CASCADE,
    allocation_id        UUID        NOT NULL REFERENCES github_runner_allocations(allocation_id) ON DELETE CASCADE,
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

INSERT INTO runner_class_filesystem_mounts (
    runner_class,
    mount_name,
    source_ref,
    mount_path,
    fs_type,
    read_only,
    sort_order
) VALUES (
    'metal-4vcpu-ubuntu-2404',
    'viteplus',
    'viteplus',
    '/opt/forge-metal/nodejs',
    'ext4',
    true,
    10
), (
    'metal-2vcpu-ubuntu-2404',
    'viteplus',
    'viteplus',
    '/opt/forge-metal/nodejs',
    'ext4',
    true,
    10
) ON CONFLICT (runner_class, mount_name) DO NOTHING;
