DROP TABLE IF EXISTS forge_metal.billing_events;

CREATE TABLE forge_metal.billing_events (
    event_id        String                 CODEC(ZSTD(3)),
    event_type      LowCardinality(String) CODEC(ZSTD(3)),
    aggregate_type  LowCardinality(String) CODEC(ZSTD(3)),
    aggregate_id    String                 CODEC(ZSTD(3)),
    org_id          LowCardinality(String) CODEC(ZSTD(3)),
    product_id      LowCardinality(String) CODEC(ZSTD(3)),
    occurred_at     DateTime64(6, 'UTC')  CODEC(DoubleDelta, ZSTD(3)),
    payload         String                 CODEC(ZSTD(3)),
    recorded_at     DateTime64(6, 'UTC')  CODEC(DoubleDelta, ZSTD(3))
)
ENGINE = ReplacingMergeTree(recorded_at)
ORDER BY (event_id, occurred_at, aggregate_type, aggregate_id);
