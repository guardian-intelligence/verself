CREATE USER IF NOT EXISTS distribution_service IDENTIFIED WITH ssl_certificate SAN 'URI:spiffe://spiffe.verself.sh/svc/distribution-service' HOST LOCAL;
ALTER USER distribution_service IDENTIFIED WITH ssl_certificate SAN 'URI:spiffe://spiffe.verself.sh/svc/distribution-service' HOST LOCAL;

CREATE TABLE IF NOT EXISTS verself.distribution_events
(
    recorded_at     DateTime64(6, 'UTC')   CODEC(DoubleDelta, ZSTD(3)),
    event_date      Date                   DEFAULT toDate(recorded_at) CODEC(Delta(2), ZSTD(3)),
    occurred_at     DateTime64(6, 'UTC')   CODEC(DoubleDelta, ZSTD(3)),
    event_type      LowCardinality(String) CODEC(ZSTD(3)),
    decision        LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    package_name    LowCardinality(String) CODEC(ZSTD(3)),
    channel_name    LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    platform_os     LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    platform_arch   LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    artifact_digest String                 DEFAULT '' CODEC(ZSTD(3)),
    actor           String                 DEFAULT '' CODEC(ZSTD(3)),
    reason          String                 DEFAULT '' CODEC(ZSTD(3)),
    request_id      String                 DEFAULT '' CODEC(ZSTD(3)),
    trace_id        String                 DEFAULT '' CODEC(ZSTD(3)),
    span_id         String                 DEFAULT '' CODEC(ZSTD(3)),
    INDEX idx_distribution_digest artifact_digest TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_distribution_trace trace_id TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_date)
ORDER BY (event_type, decision, package_name, channel_name, platform_os, platform_arch, occurred_at)
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

GRANT SELECT, INSERT ON verself.distribution_events TO distribution_service;
