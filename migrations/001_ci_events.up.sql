-- Wide event table: one row per CI job.
-- All dimensions denormalized. No JOINs needed for any CI performance query.
--
-- Compression codecs chosen per column type:
--   Timestamps:       DoubleDelta + ZSTD(3) — monotonically increasing
--   Durations (ns):   Delta(8) + ZSTD(3)    — delta encoding for integer time series
--   Byte counters:    T64 + ZSTD(3)         — crops unused high bits
--   Small integers:   T64 + ZSTD(3)         — efficient for small-range values
--   Booleans:         ZSTD(3) only          — too small for delta/gorilla
--   Low-cardinality:  LowCardinality + ZSTD(3) — dictionary encoding
--   High-cardinality: ZSTD(3) only

CREATE TABLE IF NOT EXISTS ci_events (
    -- Identity
    job_id             UUID                                    CODEC(ZSTD(3)),
    run_id             String                                  CODEC(ZSTD(3)),
    node_id            LowCardinality(String)                  CODEC(ZSTD(3)),
    region             LowCardinality(String)                  CODEC(ZSTD(3)),
    plan               LowCardinality(String)                  CODEC(ZSTD(3)),

    -- Git metadata
    repo               LowCardinality(String)                  CODEC(ZSTD(3)),
    branch             String                                  CODEC(ZSTD(3)),
    commit_sha         FixedString(40)                         CODEC(ZSTD(3)),
    pr_number          UInt32                                  CODEC(T64, ZSTD(3)),
    pr_author          LowCardinality(String)                  CODEC(ZSTD(3)),
    base_branch        LowCardinality(String)                  CODEC(ZSTD(3)),
    diff_files_changed UInt16                                  CODEC(T64, ZSTD(3)),
    diff_lines_added   UInt32                                  CODEC(T64, ZSTD(3)),
    diff_lines_deleted UInt32                                  CODEC(T64, ZSTD(3)),

    -- Timing (nanoseconds)
    zfs_clone_ns       Int64                                   CODEC(Delta(8), ZSTD(3)),
    gvisor_setup_ns    Int64                                   CODEC(Delta(8), ZSTD(3)),
    deps_install_ns    Int64                                   CODEC(Delta(8), ZSTD(3)),
    lint_ns            Int64                                   CODEC(Delta(8), ZSTD(3)),
    typecheck_ns       Int64                                   CODEC(Delta(8), ZSTD(3)),
    build_ns           Int64                                   CODEC(Delta(8), ZSTD(3)),
    test_ns            Int64                                   CODEC(Delta(8), ZSTD(3)),
    total_ci_ns        Int64                                   CODEC(Delta(8), ZSTD(3)),
    total_e2e_ns       Int64                                   CODEC(Delta(8), ZSTD(3)),
    cleanup_ns         Int64                                   CODEC(Delta(8), ZSTD(3)),
    gvisor_teardown_ns Int64                                   CODEC(Delta(8), ZSTD(3)),

    -- Exit codes
    lint_exit          Int8                                    CODEC(ZSTD(3)),
    typecheck_exit     Int8                                    CODEC(ZSTD(3)),
    build_exit         Int8                                    CODEC(ZSTD(3)),
    test_exit          Int8                                    CODEC(ZSTD(3)),

    -- Resource usage (peak, from cgroup stats)
    cpu_user_ms        UInt64                                  CODEC(T64, ZSTD(3)),
    cpu_system_ms      UInt64                                  CODEC(T64, ZSTD(3)),
    memory_peak_bytes  UInt64                                  CODEC(T64, ZSTD(3)),
    io_read_bytes      UInt64                                  CODEC(T64, ZSTD(3)),
    io_write_bytes     UInt64                                  CODEC(T64, ZSTD(3)),
    zfs_written_bytes  UInt64                                  CODEC(T64, ZSTD(3)),

    -- Cache effectiveness
    npm_cache_hit      UInt8                                   CODEC(ZSTD(3)),
    next_cache_hit     UInt8                                   CODEC(ZSTD(3)),
    tsc_cache_hit      UInt8                                   CODEC(ZSTD(3)),
    lockfile_changed   UInt8                                   CODEC(ZSTD(3)),

    -- Hardware
    cpu_model          LowCardinality(String)                  CODEC(ZSTD(3)),
    cores              UInt16                                  CODEC(T64, ZSTD(3)),
    memory_mb          UInt32                                  CODEC(T64, ZSTD(3)),
    disk_type          LowCardinality(String)                  CODEC(ZSTD(3)),

    -- Environment
    golden_snapshot    LowCardinality(String)                  CODEC(ZSTD(3)),
    golden_age_hours   Float32                                 CODEC(Gorilla, ZSTD(3)),
    node_version       LowCardinality(String)                  CODEC(ZSTD(3)),
    npm_version        LowCardinality(String)                  CODEC(ZSTD(3)),

    -- Timestamps
    created_at         DateTime64(9, 'UTC')                    CODEC(DoubleDelta, ZSTD(3)),
    started_at         DateTime64(9, 'UTC')                    CODEC(DoubleDelta, ZSTD(3)),
    completed_at       DateTime64(9, 'UTC')                    CODEC(DoubleDelta, ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (region, node_id, created_at)
TTL created_at + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;
