CREATE TABLE IF NOT EXISTS forge_metal.webhook_delivery_events
(
    delivery_id          UUID,
    endpoint_id          UUID,
    integration_id       UUID,
    org_id               UInt64,
    provider             LowCardinality(String),
    provider_host        LowCardinality(String),
    provider_delivery_id String               CODEC(ZSTD(3)),
    event_type           LowCardinality(String),
    state                LowCardinality(String),
    attempt_count        UInt16               CODEC(T64, ZSTD(3)),
    payload_sha256       String               CODEC(ZSTD(3)),
    error                String DEFAULT ''    CODEC(ZSTD(3)),
    trace_id             String DEFAULT ''    CODEC(ZSTD(3)),
    created_at           DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (org_id, provider, state, created_at, delivery_id)
TTL toDateTime(created_at) + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;
