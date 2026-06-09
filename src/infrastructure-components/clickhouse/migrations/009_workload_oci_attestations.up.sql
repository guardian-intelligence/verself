-- Append-only OCI image evidence observed from Nomad allocation events.
--
-- This table is intentionally observe-only: rows record the declared immutable
-- image digest from the Nomad job payload and the measured Podman image digest
-- when the host runtime can observe a matching container, then join that digest
-- to distribution-service admission and SLSA provenance evidence. Policy
-- engines should treat any decision other than 'verified' as non-admissible for
-- secret gating.
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
    distribution_artifact_digest       String DEFAULT '' CODEC(ZSTD(3)),
    distribution_artifact_state        LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    distribution_oci_repository        String DEFAULT '' CODEC(ZSTD(3)),
    distribution_public_oci_reference  String DEFAULT '' CODEC(ZSTD(3)),
    distribution_verification_decision LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    distribution_verification_reason   String DEFAULT '' CODEC(ZSTD(3)),
    distribution_policy_ref            String DEFAULT '' CODEC(ZSTD(3)),
    distribution_builder_id            String DEFAULT '' CODEC(ZSTD(3)),
    distribution_signer_identity       String DEFAULT '' CODEC(ZSTD(3)),
    distribution_source_repository     LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    distribution_source_commit         String DEFAULT '' CODEC(ZSTD(3)),
    distribution_source_ref            String DEFAULT '' CODEC(ZSTD(3)),
    slsa_predicate_type                LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    slsa_subject_digest                String DEFAULT '' CODEC(ZSTD(3)),
    slsa_document_digest               String DEFAULT '' CODEC(ZSTD(3)),
    slsa_oci_referrer_digest           String DEFAULT '' CODEC(ZSTD(3)),
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
