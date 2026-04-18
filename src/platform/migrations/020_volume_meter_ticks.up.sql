DROP TABLE IF EXISTS forge_metal.volume_meter_ticks;

CREATE TABLE forge_metal.volume_meter_ticks
(
    meter_tick_id             UUID,
    volume_id                 UUID,
    volume_generation_id      UUID DEFAULT '00000000-0000-0000-0000-000000000000',
    org_id                    UInt64                               CODEC(T64, ZSTD(3)),
    actor_id                  String DEFAULT ''                    CODEC(ZSTD(3)),
    product_id                LowCardinality(String)               CODEC(ZSTD(3)),
    source_type               LowCardinality(String)               CODEC(ZSTD(3)),
    source_ref                String                               CODEC(ZSTD(3)),
    window_seq                UInt32                               CODEC(Delta(4), ZSTD(3)),
    window_millis             UInt32                               CODEC(T64, ZSTD(3)),
    state                     LowCardinality(String)               CODEC(ZSTD(3)),
    storage_node_id           LowCardinality(String) DEFAULT ''    CODEC(ZSTD(3)),
    pool_id                   LowCardinality(String) DEFAULT ''    CODEC(ZSTD(3)),
    dataset_ref               String DEFAULT ''                    CODEC(ZSTD(3)),
    observed_at               DateTime64(6, 'UTC')                 CODEC(DoubleDelta, ZSTD(3)),
    window_start              DateTime64(6, 'UTC')                 CODEC(DoubleDelta, ZSTD(3)),
    window_end                DateTime64(6, 'UTC')                 CODEC(DoubleDelta, ZSTD(3)),
    used_bytes                UInt64                               CODEC(T64, ZSTD(3)),
    usedbysnapshots_bytes     UInt64                               CODEC(T64, ZSTD(3)),
    billable_live_bytes       UInt64                               CODEC(T64, ZSTD(3)),
    billable_retained_bytes   UInt64                               CODEC(T64, ZSTD(3)),
    written_bytes             UInt64                               CODEC(T64, ZSTD(3)),
    provisioned_bytes         UInt64                               CODEC(T64, ZSTD(3)),
    live_gib                  Float64                              CODEC(ZSTD(3)),
    retained_gib              Float64                              CODEC(ZSTD(3)),
    dimensions                Map(LowCardinality(String), Float64) CODEC(ZSTD(3)),
    component_quantities      Map(LowCardinality(String), Float64) CODEC(ZSTD(3)),
    billing_window_id         String DEFAULT ''                    CODEC(ZSTD(3)),
    billed_charge_units       UInt64 DEFAULT 0                     CODEC(T64, ZSTD(3)),
    billing_failure_reason    String DEFAULT ''                    CODEC(ZSTD(3)),
    recorded_at               DateTime64(6, 'UTC') DEFAULT now64(6) CODEC(DoubleDelta, ZSTD(3)),
    trace_id                  String DEFAULT ''                    CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(recorded_at)
ORDER BY (org_id, product_id, state, recorded_at, volume_id, meter_tick_id)
TTL toDateTime(recorded_at) + INTERVAL 1 YEAR
SETTINGS index_granularity = 8192;
