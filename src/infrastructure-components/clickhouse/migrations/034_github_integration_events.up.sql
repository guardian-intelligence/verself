CREATE USER IF NOT EXISTS github_integration_service IDENTIFIED WITH ssl_certificate SAN 'URI:spiffe://spiffe.verself.sh/svc/github-integration-service' HOST LOCAL;
ALTER USER github_integration_service IDENTIFIED WITH ssl_certificate SAN 'URI:spiffe://spiffe.verself.sh/svc/github-integration-service' HOST LOCAL;

CREATE TABLE IF NOT EXISTS verself.github_integration_events
(
    observed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    event_name LowCardinality(String),
    result LowCardinality(String),
    reason String DEFAULT '' CODEC(ZSTD(3)),
    delivery_id String DEFAULT '' CODEC(ZSTD(3)),
    action LowCardinality(String) DEFAULT '',
    org_id LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    provider_installation_id UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    provider_repository_id UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    provider_run_id UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    provider_run_attempt UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    provider_job_id UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    repository_full_name LowCardinality(String) DEFAULT '',
    runner_id UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    runner_name String DEFAULT '' CODEC(ZSTD(3)),
    runner_class LowCardinality(String) DEFAULT '',
    allocation_id UUID,
    execution_id UUID,
    attempt_id UUID,
    started_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    completed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    duration_ms Int64 CODEC(Delta(8), ZSTD(3)),
    attributes_json String DEFAULT '' CODEC(ZSTD(3)),
    trace_id String DEFAULT '' CODEC(ZSTD(3)),
    span_id String DEFAULT '' CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(observed_at)
ORDER BY (event_name, result, org_id, provider_repository_id, provider_run_id, provider_job_id, observed_at);

GRANT SELECT, INSERT ON verself.github_integration_events TO github_integration_service;
