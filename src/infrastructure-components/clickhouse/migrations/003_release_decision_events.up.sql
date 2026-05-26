CREATE USER IF NOT EXISTS release_service IDENTIFIED WITH ssl_certificate SAN 'URI:spiffe://spiffe.verself.sh/svc/release-service' HOST LOCAL;
ALTER USER release_service IDENTIFIED WITH ssl_certificate SAN 'URI:spiffe://spiffe.verself.sh/svc/release-service' HOST LOCAL;

CREATE TABLE IF NOT EXISTS verself.release_decision_events
(
    recorded_at        DateTime64(6, 'UTC')   CODEC(DoubleDelta, ZSTD(3)),
    event_date         Date                   DEFAULT toDate(recorded_at) CODEC(Delta(2), ZSTD(3)),
    occurred_at        DateTime64(6, 'UTC')   CODEC(DoubleDelta, ZSTD(3)),
    event_type         LowCardinality(String) CODEC(ZSTD(3)),
    org_id             LowCardinality(String) CODEC(ZSTD(3)),
    package_id         UUID,
    package_name       LowCardinality(String) CODEC(ZSTD(3)),
    release_version_id UUID,
    canonical_version  LowCardinality(String) CODEC(ZSTD(3)),
    version_kind       LowCardinality(String) CODEC(ZSTD(3)),
    artifact_id        UUID,
    artifact_digest    String                 DEFAULT '' CODEC(ZSTD(3)),
    oci_repository     String                 DEFAULT '' CODEC(ZSTD(3)),
    source_commit      String                 DEFAULT '' CODEC(ZSTD(3)),
    bazel_target       String                 DEFAULT '' CODEC(ZSTD(3)),
    signer_identity    String                 DEFAULT '' CODEC(ZSTD(3)),
    builder_id         String                 DEFAULT '' CODEC(ZSTD(3)),
    policy_ref         String                 DEFAULT '' CODEC(ZSTD(3)),
    policy_digest      String                 DEFAULT '' CODEC(ZSTD(3)),
    lifecycle_status   LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    provider_kind      LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    provider_package   String                 DEFAULT '' CODEC(ZSTD(3)),
    provider_version   LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    provider_channel   LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    publication_id     UUID,
    decision           LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    reason             String                 DEFAULT '' CODEC(ZSTD(3)),
    actor              String                 DEFAULT '' CODEC(ZSTD(3)),
    request_id         String                 DEFAULT '' CODEC(ZSTD(3)),
    trace_id           String                 DEFAULT '' CODEC(ZSTD(3)),
    span_id            String                 DEFAULT '' CODEC(ZSTD(3)),
    attributes_json    String                 DEFAULT '{}' CODEC(ZSTD(3)),
    INDEX idx_release_version release_version_id TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_publication publication_id TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_date)
ORDER BY (event_type, org_id, package_name, provider_kind, occurred_at, release_version_id)
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

GRANT SELECT, INSERT ON verself.release_decision_events TO release_service;
