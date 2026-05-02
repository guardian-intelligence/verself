ALTER TABLE verself.supply_chain_policy_events
    ADD COLUMN IF NOT EXISTS `oci_repository` String DEFAULT '' CODEC(ZSTD(3)),
    ADD COLUMN IF NOT EXISTS `oci_manifest_digest` String DEFAULT '' CODEC(ZSTD(3)),
    ADD COLUMN IF NOT EXISTS `oci_media_type` LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    ADD COLUMN IF NOT EXISTS `signature_digest` String DEFAULT '' CODEC(ZSTD(3)),
    ADD COLUMN IF NOT EXISTS `attestation_digest` String DEFAULT '' CODEC(ZSTD(3)),
    ADD COLUMN IF NOT EXISTS `sbom_digest` String DEFAULT '' CODEC(ZSTD(3)),
    ADD COLUMN IF NOT EXISTS `provenance_digest` String DEFAULT '' CODEC(ZSTD(3)),
    ADD COLUMN IF NOT EXISTS `scanner_result_digest` String DEFAULT '' CODEC(ZSTD(3)),
    ADD COLUMN IF NOT EXISTS `minimum_age_result` LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    ADD COLUMN IF NOT EXISTS `scanner_results` LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    ADD COLUMN IF NOT EXISTS `scanner_name` LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    ADD COLUMN IF NOT EXISTS `scanner_version` String DEFAULT '' CODEC(ZSTD(3)),
    ADD COLUMN IF NOT EXISTS `scanner_database_digest` String DEFAULT '' CODEC(ZSTD(3)),
    ADD COLUMN IF NOT EXISTS `guac_subject` String DEFAULT '' CODEC(ZSTD(3));

CREATE TABLE IF NOT EXISTS verself.artifact_admission_events
(
    `event_at`                DateTime64(9)           CODEC(Delta(8), ZSTD(3)),
    `deploy_run_key`          String                  CODEC(ZSTD(3)),
    `site`                    LowCardinality(String)  CODEC(ZSTD(3)),
    `artifact`                String                  CODEC(ZSTD(3)),
    `source_path`             String                  CODEC(ZSTD(3)),
    `source_kind`             LowCardinality(String)  CODEC(ZSTD(3)),
    `upstream_url`            String      DEFAULT ''  CODEC(ZSTD(3)),
    `upstream_digest`         String      DEFAULT ''  CODEC(ZSTD(3)),
    `released_at`             DateTime64(9) DEFAULT toDateTime64(0, 9) CODEC(Delta(8), ZSTD(3)),
    `observed_at`             DateTime64(9) DEFAULT toDateTime64(0, 9) CODEC(Delta(8), ZSTD(3)),
    `minimum_age_result`      LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    `oci_repository`          String      DEFAULT ''  CODEC(ZSTD(3)),
    `oci_manifest_digest`     String      DEFAULT ''  CODEC(ZSTD(3)),
    `oci_media_type`          LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    `signature_digest`        String      DEFAULT ''  CODEC(ZSTD(3)),
    `attestation_digest`      String      DEFAULT ''  CODEC(ZSTD(3)),
    `sbom_digest`             String      DEFAULT ''  CODEC(ZSTD(3)),
    `provenance_digest`       String      DEFAULT ''  CODEC(ZSTD(3)),
    `scanner_result_digest`   String      DEFAULT ''  CODEC(ZSTD(3)),
    `scanner_name`            LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    `scanner_version`         String      DEFAULT ''  CODEC(ZSTD(3)),
    `scanner_database_digest` String      DEFAULT ''  CODEC(ZSTD(3)),
    `tuf_target_path`         String      DEFAULT ''  CODEC(ZSTD(3)),
    `storage_uri`             String      DEFAULT ''  CODEC(ZSTD(3)),
    `policy_result`           LowCardinality(String)  CODEC(ZSTD(3)),
    `policy_reason`           String      DEFAULT ''  CODEC(ZSTD(3)),
    `guac_subject`            String      DEFAULT ''  CODEC(ZSTD(3)),
    `trace_id`                String      DEFAULT ''  CODEC(ZSTD(3)),
    `span_id`                 String      DEFAULT ''  CODEC(ZSTD(3)),
    `evidence`                String      DEFAULT ''  CODEC(ZSTD(3))
)
ENGINE = MergeTree
ORDER BY (site, policy_result, scanner_name, source_kind, event_at, deploy_run_key, artifact)
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS verself.artifact_install_verification_events
(
    `event_at`            DateTime64(9)           CODEC(Delta(8), ZSTD(3)),
    `deploy_run_key`      String                  CODEC(ZSTD(3)),
    `site`                LowCardinality(String)  CODEC(ZSTD(3)),
    `surface`             LowCardinality(String)  CODEC(ZSTD(3)),
    `installer`           LowCardinality(String)  CODEC(ZSTD(3)),
    `artifact`            String                  CODEC(ZSTD(3)),
    `oci_reference`       String      DEFAULT ''  CODEC(ZSTD(3)),
    `oci_repository`      String      DEFAULT ''  CODEC(ZSTD(3)),
    `oci_manifest_digest` String      DEFAULT ''  CODEC(ZSTD(3)),
    `signature_digest`    String      DEFAULT ''  CODEC(ZSTD(3)),
    `attestation_digest`  String      DEFAULT ''  CODEC(ZSTD(3)),
    `policy_result`       LowCardinality(String)  CODEC(ZSTD(3)),
    `policy_reason`       String      DEFAULT ''  CODEC(ZSTD(3)),
    `trace_id`            String      DEFAULT ''  CODEC(ZSTD(3)),
    `span_id`             String      DEFAULT ''  CODEC(ZSTD(3)),
    `evidence`            String      DEFAULT ''  CODEC(ZSTD(3))
)
ENGINE = MergeTree
ORDER BY (site, policy_result, surface, installer, event_at, deploy_run_key, artifact)
SETTINGS index_granularity = 8192;
