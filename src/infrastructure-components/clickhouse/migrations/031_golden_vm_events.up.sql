CREATE TABLE IF NOT EXISTS verself.golden_vm_events
(
    observed_at DateTime64(6, 'UTC') CODEC(DoubleDelta, ZSTD(3)),
    org_id LowCardinality(String) CODEC(ZSTD(3)),
    repository_id UInt64 CODEC(T64, ZSTD(3)),
    provider LowCardinality(String),
    provider_repository_id UInt64 CODEC(T64, ZSTD(3)),
    provider_run_id UInt64 CODEC(T64, ZSTD(3)),
    provider_run_attempt UInt64 CODEC(T64, ZSTD(3)),
    provider_job_id UInt64 CODEC(T64, ZSTD(3)),
    execution_id UUID,
    attempt_id UUID,
    operation_id UUID,
    golden_vm_snapshot_id UUID,
    job_shape_id UUID,
    event_name LowCardinality(String),
    result LowCardinality(String),
    reason String DEFAULT '' CODEC(ZSTD(3)),
    from_state LowCardinality(String) DEFAULT '',
    to_state LowCardinality(String) DEFAULT '',
    lease_id String DEFAULT '' CODEC(ZSTD(3)),
    exec_id String DEFAULT '' CODEC(ZSTD(3)),
    river_job_id Int64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    generation_set_hash String DEFAULT '' CODEC(ZSTD(3)),
    source_generation_set_hash String DEFAULT '' CODEC(ZSTD(3)),
    snapshot_key String DEFAULT '' CODEC(ZSTD(3)),
    activation_mode LowCardinality(String),
    vmstate_artifact_ref String DEFAULT '' CODEC(ZSTD(3)),
    memory_artifact_ref String DEFAULT '' CODEC(ZSTD(3)),
    root_snapshot_ref String DEFAULT '' CODEC(ZSTD(3)),
    root_snapshot_guid String DEFAULT '' CODEC(ZSTD(3)),
    state_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    memory_bytes UInt64 DEFAULT 0 CODEC(T64, ZSTD(3)),
    trace_id String DEFAULT '' CODEC(ZSTD(3)),
    span_id String DEFAULT '' CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(observed_at)
ORDER BY (event_name, provider, org_id, repository_id, observed_at, operation_id, golden_vm_snapshot_id);

GRANT SELECT, INSERT ON verself.golden_vm_events TO sandbox_rental;
