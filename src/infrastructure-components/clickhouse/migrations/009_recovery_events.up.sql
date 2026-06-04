CREATE TABLE IF NOT EXISTS verself.recovery_events
(
    `site` LowCardinality(String) CODEC(ZSTD(3)),
    `component` LowCardinality(String) CODEC(ZSTD(3)),
    `mechanism` LowCardinality(String) CODEC(ZSTD(3)),
    `action` LowCardinality(String) CODEC(ZSTD(3)),
    `status` LowCardinality(String) CODEC(ZSTD(3)),
    `point` String CODEC(ZSTD(3)),
    `backup_name` String CODEC(ZSTD(3)),
    `backup_operation_id` String CODEC(ZSTD(3)),
    `restore_operation_id` String CODEC(ZSTD(3)),
    `source_database` LowCardinality(String) CODEC(ZSTD(3)),
    `target_database` LowCardinality(String) CODEC(ZSTD(3)),
    `expected_table_count` UInt32 CODEC(T64, ZSTD(3)),
    `restored_table_count` UInt32 CODEC(T64, ZSTD(3)),
    `expected_rows` UInt64 CODEC(T64, ZSTD(3)),
    `restored_rows` UInt64 CODEC(T64, ZSTD(3)),
    `backup_num_files` UInt64 CODEC(T64, ZSTD(3)),
    `backup_total_bytes` UInt64 CODEC(T64, ZSTD(3)),
    `backup_uncompressed_bytes` UInt64 CODEC(T64, ZSTD(3)),
    `backup_compressed_bytes` UInt64 CODEC(T64, ZSTD(3)),
    `backup_duration_ms` UInt32 CODEC(T64, ZSTD(3)),
    `restore_duration_ms` UInt32 CODEC(T64, ZSTD(3)),
    `verify_duration_ms` UInt32 CODEC(T64, ZSTD(3)),
    `error_kind` String CODEC(ZSTD(3)),
    `error_message` String CODEC(ZSTD(3)),
    `details_json` String CODEC(ZSTD(3)),
    `event_at` DateTime64(9) CODEC(Delta(8), ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toDate(event_at)
ORDER BY (site, component, mechanism, action, status, toDateTime(event_at), point)
SETTINGS index_granularity = 8192;
