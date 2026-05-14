TRUNCATE TABLE IF EXISTS verself.analytics_access_events;
TRUNCATE TABLE IF EXISTS verself.analytics_events;
TRUNCATE TABLE IF EXISTS verself.analytics_ingest_events;
TRUNCATE TABLE IF EXISTS verself.api_activity_events;
TRUNCATE TABLE IF EXISTS verself.api_activity_payloads;
TRUNCATE TABLE IF EXISTS verself.api_activity_resources;
TRUNCATE TABLE IF EXISTS verself.bazel_events;
TRUNCATE TABLE IF EXISTS verself.bazel_invocations;
TRUNCATE TABLE IF EXISTS verself.bazel_profile_spans;
TRUNCATE TABLE IF EXISTS verself.bazel_spawns;
TRUNCATE TABLE IF EXISTS verself.bazel_targets;
TRUNCATE TABLE IF EXISTS verself.billing_events;
TRUNCATE TABLE IF EXISTS verself.domain_update_ledger;
TRUNCATE TABLE IF EXISTS verself.durable_events;
TRUNCATE TABLE IF EXISTS verself.metering;
TRUNCATE TABLE IF EXISTS verself.notification_events;
TRUNCATE TABLE IF EXISTS verself.object_access_events;

DROP TABLE IF EXISTS verself.job_logs;
DROP TABLE IF EXISTS verself.job_events;

CREATE TABLE verself.job_logs
(
    execution_id         UUID,
    attempt_id           UUID,
    org_id               LowCardinality(String),
    source_kind          LowCardinality(String) DEFAULT '',
    workload_kind        LowCardinality(String) DEFAULT '',
    runner_class         LowCardinality(String) DEFAULT '',
    external_provider    LowCardinality(String) DEFAULT '',
    product_id           LowCardinality(String) DEFAULT '',
    correlation_id       String DEFAULT ''       CODEC(ZSTD(3)),
    repository_full_name LowCardinality(String) DEFAULT '',
    workflow_name        LowCardinality(String) DEFAULT '',
    job_name             LowCardinality(String) DEFAULT '',
    head_branch          LowCardinality(String) DEFAULT '',
    schedule_id          String DEFAULT ''       CODEC(ZSTD(3)),
    seq                  UInt32,
    stream               LowCardinality(String),
    chunk                String                  CODEC(ZSTD(3)),
    created_at           DateTime64(6, 'UTC')   CODEC(DoubleDelta, ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (org_id, source_kind, runner_class, created_at, execution_id, attempt_id, seq)
TTL toDateTime(created_at) + INTERVAL 90 DAY
SETTINGS index_granularity = 8192;

CREATE TABLE verself.job_events
(
    execution_id             UUID,
    attempt_id               UUID,
    org_id                   LowCardinality(String),
    actor_id                 LowCardinality(String),
    kind                     LowCardinality(String),
    source_kind              LowCardinality(String) DEFAULT '',
    workload_kind            LowCardinality(String) DEFAULT '',
    source_ref               String DEFAULT ''       CODEC(ZSTD(3)),
    runner_class             LowCardinality(String) DEFAULT '',
    external_provider        LowCardinality(String) DEFAULT '',
    external_task_id         String DEFAULT ''       CODEC(ZSTD(3)),
    provider                 LowCardinality(String),
    product_id               LowCardinality(String),
    lease_id                 String DEFAULT ''       CODEC(ZSTD(3)),
    exec_id                  String DEFAULT ''       CODEC(ZSTD(3)),
    repository_full_name     LowCardinality(String) DEFAULT '',
    workflow_name            LowCardinality(String) DEFAULT '',
    job_name                 LowCardinality(String) DEFAULT '',
    head_branch              LowCardinality(String) DEFAULT '',
    head_sha                 String DEFAULT ''       CODEC(ZSTD(3)),
    provider_installation_id UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    provider_run_id          UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    provider_job_id          UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    schedule_id              String DEFAULT ''       CODEC(ZSTD(3)),
    schedule_display_name    LowCardinality(String) DEFAULT '',
    temporal_workflow_id     String DEFAULT ''       CODEC(ZSTD(3)),
    temporal_run_id          String DEFAULT ''       CODEC(ZSTD(3)),
    run_command              String                  CODEC(ZSTD(3)),
    status                   LowCardinality(String),
    exit_code                Int32                   CODEC(ZSTD(3)),
    duration_ms              Int64                   CODEC(Delta(8), ZSTD(3)),
    zfs_written              UInt64                  CODEC(T64, ZSTD(3)),
    stdout_bytes             UInt64                  CODEC(T64, ZSTD(3)),
    stderr_bytes             UInt64                  CODEC(T64, ZSTD(3)),
    billing_job_id           Int64 DEFAULT 0         CODEC(ZSTD(3)),
    reserved_charge_units    UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    billed_charge_units      UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    writeoff_charge_units    UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    cost_per_unit            UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    pricing_phase            LowCardinality(String) DEFAULT '',
    rootfs_provisioned_bytes UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    boot_time_us             UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    block_read_bytes         UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    block_write_bytes        UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    net_rx_bytes             UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    net_tx_bytes             UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    vcpu_exit_count          UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    correlation_id           String DEFAULT ''       CODEC(ZSTD(3)),
    started_at               DateTime64(6, 'UTC')    CODEC(DoubleDelta, ZSTD(3)),
    completed_at             DateTime64(6, 'UTC')    CODEC(DoubleDelta, ZSTD(3)),
    created_at               DateTime64(6, 'UTC')    CODEC(DoubleDelta, ZSTD(3)),
    trace_id                 String DEFAULT ''       CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (org_id, source_kind, runner_class, repository_full_name, created_at, execution_id)
TTL toDateTime(created_at) + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;
