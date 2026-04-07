CREATE TABLE IF NOT EXISTS forge_metal.metering (
    org_id             LowCardinality(String)               CODEC(ZSTD(3)),
    actor_id           String DEFAULT ''                    CODEC(ZSTD(3)),
    product_id         LowCardinality(String)               CODEC(ZSTD(3)),
    source_type        LowCardinality(String)               CODEC(ZSTD(3)),
    source_ref         String                               CODEC(ZSTD(3)),
    window_seq         UInt32                               CODEC(Delta(4), ZSTD(3)),
    started_at         DateTime64(6)                        CODEC(DoubleDelta, ZSTD(3)),
    ended_at           DateTime64(6)                        CODEC(DoubleDelta, ZSTD(3)),
    billed_seconds     UInt32                               CODEC(Delta(4), ZSTD(3)),
    pricing_phase      LowCardinality(String)               CODEC(ZSTD(3)),
    dimensions         Map(LowCardinality(String), Float64) CODEC(ZSTD(3)),
    charge_units       UInt64                               CODEC(T64, ZSTD(3)),
    free_tier_units    UInt64                               CODEC(T64, ZSTD(3)),
    subscription_units UInt64                               CODEC(T64, ZSTD(3)),
    purchase_units     UInt64                               CODEC(T64, ZSTD(3)),
    promo_units        UInt64                               CODEC(T64, ZSTD(3)),
    refund_units       UInt64                               CODEC(T64, ZSTD(3)),
    recorded_at        DateTime64(6) DEFAULT now64(6)       CODEC(DoubleDelta, ZSTD(3)),
    trace_id           String DEFAULT ''                    CODEC(ZSTD(3))
)
ENGINE = MergeTree()
ORDER BY (org_id, product_id, started_at, source_ref, window_seq);
