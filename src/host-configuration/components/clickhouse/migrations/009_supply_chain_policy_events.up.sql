CREATE TABLE IF NOT EXISTS verself.supply_chain_policy_events
(
    `event_at`        DateTime64(9)           CODEC(Delta(8), ZSTD(3)),
    `deploy_run_key`  String                  CODEC(ZSTD(3)),
    `site`            LowCardinality(String)  CODEC(ZSTD(3)),
    `source_path`     String                  CODEC(ZSTD(3)),
    `line`            UInt32                  CODEC(T64, ZSTD(3)),
    `source_kind`     LowCardinality(String)  CODEC(ZSTD(3)),
    `surface`         LowCardinality(String)  CODEC(ZSTD(3)),
    `artifact`        String                  CODEC(ZSTD(3)),
    `upstream_url`    String      DEFAULT ''  CODEC(ZSTD(3)),
    `digest`          String      DEFAULT ''  CODEC(ZSTD(3)),
    `policy_result`   LowCardinality(String)  CODEC(ZSTD(3)),
    `policy_reason`   String      DEFAULT ''  CODEC(ZSTD(3)),
    `admission_state` LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    `tuf_target_path` String      DEFAULT ''  CODEC(ZSTD(3)),
    `storage_uri`     String      DEFAULT ''  CODEC(ZSTD(3)),
    `trace_id`        String      DEFAULT ''  CODEC(ZSTD(3)),
    `span_id`         String      DEFAULT ''  CODEC(ZSTD(3)),
    `evidence`        String      DEFAULT ''  CODEC(ZSTD(3))
)
ENGINE = MergeTree
ORDER BY (site, policy_result, surface, source_kind, event_at, deploy_run_key)
SETTINGS index_granularity = 8192;
