-- Append-only OCI image evidence observed from Nomad allocation events.
--
-- This table is intentionally observe-only: rows record the declared immutable
-- image digest from the Nomad job payload and leave measured_digest empty until
-- an on-host runtime measurement source is wired in. Policy engines should
-- treat decision='unmeasured' as non-admissible for secret gating.
CREATE TABLE IF NOT EXISTS verself.workload_oci_attestations
(
    site                LowCardinality(String) CODEC(ZSTD(3)),
    nomad_namespace     LowCardinality(String) CODEC(ZSTD(3)),
    nomad_job_id        LowCardinality(String) CODEC(ZSTD(3)),
    nomad_group         LowCardinality(String) CODEC(ZSTD(3)),
    nomad_task          LowCardinality(String) CODEC(ZSTD(3)),
    alloc_id            String                 CODEC(ZSTD(3)),
    node_id             String DEFAULT ''      CODEC(ZSTD(3)),
    node_name           LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    image_ref           String                 CODEC(ZSTD(3)),
    declared_digest     String DEFAULT ''      CODEC(ZSTD(3)),
    measured_digest     String DEFAULT ''      CODEC(ZSTD(3)),
    source_commit       String DEFAULT ''      CODEC(ZSTD(3)),
    deploy_run_key      String DEFAULT ''      CODEC(ZSTD(3)),
    spec_sha256         String DEFAULT ''      CODEC(ZSTD(3)),
    artifact_sha256     String DEFAULT ''      CODEC(ZSTD(3)),
    alloc_client_status LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    task_state          LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    decision            LowCardinality(String) CODEC(ZSTD(3)),
    measurement_source  LowCardinality(String) CODEC(ZSTD(3)),
    reason              String DEFAULT ''      CODEC(ZSTD(3)),
    alloc_modify_index  UInt64 DEFAULT 0       CODEC(T64, ZSTD(3)),
    trace_id            String DEFAULT ''      CODEC(ZSTD(3)),
    span_id             String DEFAULT ''      CODEC(ZSTD(3)),
    observed_at         DateTime64(3, 'UTC') DEFAULT now64(3) CODEC(DoubleDelta, ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(toDate(observed_at))
ORDER BY (site, nomad_namespace, nomad_job_id, nomad_group, nomad_task, decision, observed_at, alloc_id)
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

CREATE USER IF NOT EXISTS nomad_observer IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/nomad-observer' HOST LOCAL;
ALTER USER nomad_observer IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/nomad-observer' HOST LOCAL;
GRANT INSERT ON verself.workload_oci_attestations TO nomad_observer;
