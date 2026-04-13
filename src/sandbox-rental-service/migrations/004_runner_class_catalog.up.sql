-- Runner classes are the product-level scheduling contract used by direct
-- executions, Forgejo/GitHub Actions adapters, cron, and future VM sessions.

CREATE TABLE runner_classes (
    runner_class     TEXT        PRIMARY KEY,
    product_id       TEXT        NOT NULL DEFAULT 'sandbox',
    display_name     TEXT        NOT NULL,
    os_family        TEXT        NOT NULL,
    os_version       TEXT        NOT NULL,
    arch             TEXT        NOT NULL DEFAULT 'x86_64',
    vcpus            INTEGER     NOT NULL CHECK (vcpus > 0),
    memory_mib       INTEGER     NOT NULL CHECK (memory_mib > 0),
    rootfs_gib       INTEGER     NOT NULL CHECK (rootfs_gib > 0),
    runtime_image    TEXT        NOT NULL,
    active           BOOLEAN     NOT NULL DEFAULT true,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
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
)
ON CONFLICT (runner_class) DO UPDATE
SET product_id = EXCLUDED.product_id,
    display_name = EXCLUDED.display_name,
    os_family = EXCLUDED.os_family,
    os_version = EXCLUDED.os_version,
    arch = EXCLUDED.arch,
    vcpus = EXCLUDED.vcpus,
    memory_mib = EXCLUDED.memory_mib,
    rootfs_gib = EXCLUDED.rootfs_gib,
    runtime_image = EXCLUDED.runtime_image,
    active = true,
    updated_at = now();

ALTER TABLE executions
  ADD COLUMN runner_class TEXT NOT NULL DEFAULT 'metal-4vcpu-ubuntu-2404',
  ADD COLUMN external_provider TEXT NOT NULL DEFAULT '',
  ADD COLUMN external_task_id TEXT NOT NULL DEFAULT '';

ALTER TABLE executions
  ADD CONSTRAINT fk_executions_runner_class
  FOREIGN KEY (runner_class) REFERENCES runner_classes(runner_class);

CREATE INDEX idx_executions_runner_class_updated
    ON executions (runner_class, updated_at DESC);

CREATE INDEX idx_executions_external_task
    ON executions (external_provider, external_task_id)
    WHERE external_provider <> '' AND external_task_id <> '';
