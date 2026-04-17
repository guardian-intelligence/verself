-- Per-execution VM shape and per-org resource ceilings.
-- Defaults mirror the default runner class and apiwire.DefaultBounds.

ALTER TABLE executions
    ADD COLUMN requested_vcpus        INT  NOT NULL DEFAULT 4     CHECK (requested_vcpus > 0),
    ADD COLUMN requested_memory_mib   INT  NOT NULL DEFAULT 16384 CHECK (requested_memory_mib > 0),
    ADD COLUMN requested_root_disk_gib INT NOT NULL DEFAULT 80    CHECK (requested_root_disk_gib > 0),
    ADD COLUMN requested_kernel_image TEXT NOT NULL DEFAULT 'default';

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
