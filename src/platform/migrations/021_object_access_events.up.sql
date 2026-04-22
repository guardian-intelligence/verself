CREATE DATABASE IF NOT EXISTS forge_metal;

DROP TABLE IF EXISTS forge_metal.object_access_events;

CREATE TABLE forge_metal.object_access_events
(
    recorded_at DateTime64(6, 'UTC'),
    event_date Date DEFAULT toDate(recorded_at),

    environment LowCardinality(String),
    service_version LowCardinality(String),
    writer_instance_id String,

    org_id LowCardinality(String),
    bucket_id String,
    bucket_name LowCardinality(String),
    requested_bucket LowCardinality(String),
    resolved_alias LowCardinality(String),
    resolved_prefix String,

    operation LowCardinality(String),
    auth_mode LowCardinality(String),
    access_key_id String,
    spiffe_peer_id String,
    trace_id String,
    span_id String,

    status UInt16,
    bytes_in UInt64 CODEC(T64, ZSTD(3)),
    bytes_out UInt64 CODEC(T64, ZSTD(3)),
    latency_ms UInt32 CODEC(T64, ZSTD(3)),
    client_ip_hash String,
    user_agent_hash String,
    error_class LowCardinality(String),
    error_message String,
    upstream_status UInt16,
    upstream_request_uri String
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_date)
ORDER BY (org_id, operation, auth_mode, status, event_date, recorded_at, bucket_id)
TTL toDateTime(recorded_at) + INTERVAL 30 DAY
SETTINGS index_granularity = 8192;
