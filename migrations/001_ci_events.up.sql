-- Wide event table: one row per CI job.
-- All dimensions denormalized. No JOINs needed for any CI performance query.

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

    -- Timing (nanoseconds). Delta(8): values cluster within each (region, node_id)
    -- group but aren't monotonic across jobs — first-delta is sufficient.
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

    -- Exit codes. Small fixed domain (0/1/2), too few distinct values for Delta or T64.
    lint_exit          Int8                                    CODEC(ZSTD(3)),
    typecheck_exit     Int8                                    CODEC(ZSTD(3)),
    build_exit         Int8                                    CODEC(ZSTD(3)),
    test_exit          Int8                                    CODEC(ZSTD(3)),

    -- Resource usage (peak, from cgroup stats). T64: most values use <32 of 64 bits.
    cpu_user_ms        UInt64                                  CODEC(T64, ZSTD(3)),
    cpu_system_ms      UInt64                                  CODEC(T64, ZSTD(3)),
    memory_peak_bytes  UInt64                                  CODEC(T64, ZSTD(3)),
    io_read_bytes      UInt64                                  CODEC(T64, ZSTD(3)),
    io_write_bytes     UInt64                                  CODEC(T64, ZSTD(3)),
    zfs_written_bytes  UInt64                                  CODEC(T64, ZSTD(3)),

    -- Cache effectiveness. Booleans: ZSTD only, too small for specialized codecs.
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
    golden_age_hours   Float32                                 CODEC(Gorilla, ZSTD(3)), -- Gorilla: adjacent rows share node, so golden ages correlate
    node_version       LowCardinality(String)                  CODEC(ZSTD(3)),
    npm_version        LowCardinality(String)                  CODEC(ZSTD(3)),

    -- Timestamps. DoubleDelta: ORDER BY ends with created_at, so timestamps are
    -- monotonic — unlike OTel metrics where multi-key sort creates bimodal deltas.
    created_at         DateTime64(9, 'UTC')                    CODEC(DoubleDelta, ZSTD(3)),
    started_at         DateTime64(9, 'UTC')                    CODEC(DoubleDelta, ZSTD(3)),
    completed_at       DateTime64(9, 'UTC')                    CODEC(DoubleDelta, ZSTD(3)),

    -- Firecracker VM metrics (from metrics FIFO, one flush per job)
    vm_boot_time_us      UInt64                                CODEC(Delta(8), ZSTD(3)),
    block_read_bytes     UInt64                                CODEC(T64, ZSTD(3)),
    block_write_bytes    UInt64                                CODEC(T64, ZSTD(3)),
    block_read_count     UInt64                                CODEC(T64, ZSTD(3)),
    block_write_count    UInt64                                CODEC(T64, ZSTD(3)),
    net_rx_bytes         UInt64                                CODEC(T64, ZSTD(3)),
    net_tx_bytes         UInt64                                CODEC(T64, ZSTD(3)),
    vcpu_exit_count      UInt64                                CODEC(T64, ZSTD(3)),
    vm_exit_code         Int32                                 CODEC(ZSTD(3)),
    job_config_json      String                                CODEC(ZSTD(3)),

    -- vmproto runtime telemetry
    boot_to_ready_ns     Int64                                 CODEC(Delta(8), ZSTD(3)),
    service_start_ns     Int64                                 CODEC(Delta(8), ZSTD(3)),
    stdout_bytes         UInt64                                CODEC(T64, ZSTD(3)),
    stderr_bytes         UInt64                                CODEC(T64, ZSTD(3)),
    dropped_log_bytes    UInt64                                CODEC(T64, ZSTD(3)),

    -- Guest artifact footprint
    guest_rootfs_tree_bytes       UInt64                       CODEC(T64, ZSTD(3)),
    guest_rootfs_allocated_bytes  UInt64                       CODEC(T64, ZSTD(3)),
    guest_rootfs_filesystem_bytes UInt64                       CODEC(T64, ZSTD(3)),
    guest_rootfs_used_bytes       UInt64                       CODEC(T64, ZSTD(3)),
    guest_kernel_bytes            UInt64                       CODEC(T64, ZSTD(3)),
    guest_package_count           UInt32                       CODEC(T64, ZSTD(3)),

    -- Warm-path gating telemetry
    event_kind                 LowCardinality(String)          CODEC(ZSTD(3)),
    warm_filesystem_check_ns   Int64                           CODEC(Delta(8), ZSTD(3)),
    warm_snapshot_promotion_ns Int64                           CODEC(Delta(8), ZSTD(3)),
    warm_previous_destroy_ns   Int64                           CODEC(Delta(8), ZSTD(3)),
    warm_filesystem_check_ok   UInt8                           CODEC(ZSTD(3)),

    -- VM shutdown
    vm_exit_wait_ns    Int64                                   CODEC(Delta(8), ZSTD(3)),
    vm_exit_forced     UInt8                                   CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (region, node_id, created_at)
TTL created_at + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;