CREATE TABLE IF NOT EXISTS verself.bazel_invocations
(
    observed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    org_id UInt64 CODEC(T64, ZSTD(3)),
    provider LowCardinality(String),
    provider_repository_id UInt64 CODEC(T64, ZSTD(3)),
    provider_run_id UInt64 CODEC(T64, ZSTD(3)),
    provider_job_id UInt64 CODEC(T64, ZSTD(3)),
    execution_id UUID,
    attempt_id UUID,
    invocation_id String CODEC(ZSTD(3)),
    command LowCardinality(String),
    args Array(String),
    target_patterns Array(String),
    working_directory String DEFAULT '' CODEC(ZSTD(3)),
    github_workflow LowCardinality(String) DEFAULT '',
    github_job LowCardinality(String) DEFAULT '',
    github_ref String DEFAULT '' CODEC(ZSTD(3)),
    github_sha String DEFAULT '' CODEC(ZSTD(3)),
    exit_code Int32 CODEC(ZSTD(3)),
    duration_ms Int64 CODEC(Delta(8), ZSTD(3)),
    profile_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    bep_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    execution_log_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    profile_span_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    package_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    spawn_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    target_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    failed_target_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    started_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    completed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    trace_id String DEFAULT '' CODEC(ZSTD(3)),
    span_id String DEFAULT '' CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(observed_at)
ORDER BY (command, provider, org_id, provider_repository_id, provider_run_id, provider_job_id, observed_at, invocation_id)
TTL toDateTime(observed_at) + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS verself.bazel_events
(
    observed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    org_id UInt64 CODEC(T64, ZSTD(3)),
    provider LowCardinality(String),
    provider_repository_id UInt64 CODEC(T64, ZSTD(3)),
    provider_run_id UInt64 CODEC(T64, ZSTD(3)),
    provider_job_id UInt64 CODEC(T64, ZSTD(3)),
    execution_id UUID,
    attempt_id UUID,
    invocation_id String CODEC(ZSTD(3)),
    command LowCardinality(String),
    event_name LowCardinality(String),
    result LowCardinality(String),
    reason String DEFAULT '' CODEC(ZSTD(3)),
    exit_code Int32 CODEC(ZSTD(3)),
    duration_ms Int64 CODEC(Delta(8), ZSTD(3)),
    profile_span_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    package_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    spawn_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    target_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    trace_id String DEFAULT '' CODEC(ZSTD(3)),
    span_id String DEFAULT '' CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(observed_at)
ORDER BY (event_name, result, command, provider, org_id, provider_repository_id, provider_run_id, observed_at, invocation_id)
TTL toDateTime(observed_at) + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS verself.bazel_profile_spans
(
    observed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    org_id UInt64 CODEC(T64, ZSTD(3)),
    provider LowCardinality(String),
    provider_repository_id UInt64 CODEC(T64, ZSTD(3)),
    provider_run_id UInt64 CODEC(T64, ZSTD(3)),
    provider_job_id UInt64 CODEC(T64, ZSTD(3)),
    execution_id UUID,
    attempt_id UUID,
    invocation_id String CODEC(ZSTD(3)),
    command LowCardinality(String),
    span_kind LowCardinality(String),
    category LowCardinality(String),
    event_name String CODEC(ZSTD(3)),
    package_name String DEFAULT '' CODEC(ZSTD(3)),
    build_file String DEFAULT '' CODEC(ZSTD(3)),
    external_repo LowCardinality(String) DEFAULT '',
    started_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    duration_ms Int64 CODEC(Delta(8), ZSTD(3)),
    trace_id String DEFAULT '' CODEC(ZSTD(3)),
    span_id String DEFAULT '' CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(observed_at)
ORDER BY (span_kind, command, provider, org_id, provider_repository_id, provider_run_id, build_file, started_at, invocation_id)
TTL toDateTime(observed_at) + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS verself.bazel_spawns
(
    observed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    org_id UInt64 CODEC(T64, ZSTD(3)),
    provider LowCardinality(String),
    provider_repository_id UInt64 CODEC(T64, ZSTD(3)),
    provider_run_id UInt64 CODEC(T64, ZSTD(3)),
    provider_job_id UInt64 CODEC(T64, ZSTD(3)),
    execution_id UUID,
    attempt_id UUID,
    invocation_id String CODEC(ZSTD(3)),
    command LowCardinality(String),
    target_label String CODEC(ZSTD(3)),
    package_name String DEFAULT '' CODEC(ZSTD(3)),
    build_file String DEFAULT '' CODEC(ZSTD(3)),
    rule_name LowCardinality(String) DEFAULT '',
    mnemonic LowCardinality(String) DEFAULT '',
    runner LowCardinality(String) DEFAULT '',
    cache_hit UInt8 DEFAULT 0 CODEC(T64, ZSTD(3)),
    exit_code Int32 CODEC(ZSTD(3)),
    status String DEFAULT '' CODEC(ZSTD(3)),
    output_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    output_first String DEFAULT '' CODEC(ZSTD(3)),
    started_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    duration_ms Int64 CODEC(Delta(8), ZSTD(3)),
    trace_id String DEFAULT '' CODEC(ZSTD(3)),
    span_id String DEFAULT '' CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(observed_at)
ORDER BY (command, mnemonic, runner, cache_hit, provider, org_id, provider_repository_id, provider_run_id, build_file, started_at, invocation_id)
TTL toDateTime(observed_at) + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS verself.bazel_targets
(
    observed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    org_id UInt64 CODEC(T64, ZSTD(3)),
    provider LowCardinality(String),
    provider_repository_id UInt64 CODEC(T64, ZSTD(3)),
    provider_run_id UInt64 CODEC(T64, ZSTD(3)),
    provider_job_id UInt64 CODEC(T64, ZSTD(3)),
    execution_id UUID,
    attempt_id UUID,
    invocation_id String CODEC(ZSTD(3)),
    command LowCardinality(String),
    label String CODEC(ZSTD(3)),
    package_name String DEFAULT '' CODEC(ZSTD(3)),
    build_file String DEFAULT '' CODEC(ZSTD(3)),
    rule_name LowCardinality(String) DEFAULT '',
    success UInt8 DEFAULT 0 CODEC(T64, ZSTD(3)),
    output_group_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    output_file_count UInt32 DEFAULT 0 CODEC(T64, ZSTD(3)),
    trace_id String DEFAULT '' CODEC(ZSTD(3)),
    span_id String DEFAULT '' CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(observed_at)
ORDER BY (command, success, provider, org_id, provider_repository_id, provider_run_id, build_file, observed_at, invocation_id)
TTL toDateTime(observed_at) + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;

GRANT SELECT, INSERT ON verself.bazel_invocations TO sandbox_rental;
GRANT SELECT, INSERT ON verself.bazel_events TO sandbox_rental;
GRANT SELECT, INSERT ON verself.bazel_profile_spans TO sandbox_rental;
GRANT SELECT, INSERT ON verself.bazel_spawns TO sandbox_rental;
GRANT SELECT, INSERT ON verself.bazel_targets TO sandbox_rental;
