-- Deploy event telemetry. One row per ansible-playbook run.
-- Written by the deploy_events callback plugin + upload-deploy-event.sh.

CREATE TABLE IF NOT EXISTS deploy_events (
    -- Identity
    deploy_id          UUID                                    CODEC(ZSTD(3)),
    playbook           LowCardinality(String)                  CODEC(ZSTD(3)),
    plays              Array(String)                           CODEC(ZSTD(3)),

    -- Git metadata
    commit_sha         String                                  CODEC(ZSTD(3)),
    branch             LowCardinality(String)                  CODEC(ZSTD(3)),
    commit_message     String                                  CODEC(ZSTD(3)),
    author             LowCardinality(String)                  CODEC(ZSTD(3)),
    dirty              UInt8                                   CODEC(ZSTD(3)),

    -- Timing
    started_at         DateTime64(9, 'UTC')                    CODEC(DoubleDelta, ZSTD(3)),
    completed_at       DateTime64(9, 'UTC')                    CODEC(DoubleDelta, ZSTD(3)),
    total_ns           Int64                                   CODEC(Delta(8), ZSTD(3)),

    -- Task counts
    ok                 UInt8                                   CODEC(ZSTD(3)),
    tasks_ok           UInt32                                  CODEC(T64, ZSTD(3)),
    tasks_failed       UInt32                                  CODEC(T64, ZSTD(3)),
    tasks_skipped      UInt32                                  CODEC(T64, ZSTD(3)),
    tasks_changed      UInt32                                  CODEC(T64, ZSTD(3)),
    tasks_unreachable  UInt32                                  CODEC(T64, ZSTD(3)),
    task_count         UInt32                                  CODEC(T64, ZSTD(3)),

    -- Per-host breakdown (JSON: host -> {ok, failures, ...})
    hosts              String                                  CODEC(ZSTD(3)),

    -- Top 10 slowest tasks (JSON: [{name, duration_ns}, ...])
    slowest_tasks      String                                  CODEC(ZSTD(3)),

    -- Metadata
    ansible_version    LowCardinality(String)                  CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(started_at)
ORDER BY (playbook, started_at)
TTL started_at + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;
