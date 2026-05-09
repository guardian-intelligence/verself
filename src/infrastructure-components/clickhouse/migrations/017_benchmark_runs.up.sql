-- benchmark_runs is the typed fact table the checkpoint canary writes into so
-- "are we faster than Blacksmith on this workload" is one query, not a manual
-- comparison of unrelated job logs.
--
-- One row per (workload, provider, attempt) execution. cache_state is 'cold'
-- on the first run for a key (no committed generation existed) and 'warm'
-- when a Checkpoint generation was restored at mount time.
--
-- ORDER BY columns are sorted ascending by cardinality. workload + provider
-- + cache_state are LowCardinality, observed_at is the natural recency axis,
-- attempt_id is the unique key.
CREATE TABLE IF NOT EXISTS verself.benchmark_runs
(
    benchmark_run_id        UUID,
    workload                LowCardinality(String) DEFAULT '',
    provider                LowCardinality(String) DEFAULT '',
    cache_state             LowCardinality(String) DEFAULT '',
    runner_class            LowCardinality(String) DEFAULT '',
    repository_full_name    LowCardinality(String) DEFAULT '',
    head_branch             LowCardinality(String) DEFAULT '',
    commit_sha              String DEFAULT ''       CODEC(ZSTD(3)),
    provider_run_id         UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    provider_job_id         UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    attempt_id              UUID,
    execution_id            UUID,
    queued_ms               UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    boot_ms                 UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    checkout_ms             UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    setup_ms                UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    install_ms              UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    test_ms                 UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    wall_clock_ms           UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    checkpoint_restore_ms   UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    checkpoint_save_ms      UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    checkpoint_used_bytes   UInt64 DEFAULT 0        CODEC(T64, ZSTD(3)),
    cache_hit               UInt8  DEFAULT 0,
    result                  LowCardinality(String) DEFAULT '',
    error_class             LowCardinality(String) DEFAULT '',
    observed_at             DateTime64(6, 'UTC')   CODEC(DoubleDelta, ZSTD(3)),
    trace_id                String DEFAULT ''       CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(observed_at)
ORDER BY (workload, provider, cache_state, observed_at, attempt_id)
TTL toDateTime(observed_at) + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;
