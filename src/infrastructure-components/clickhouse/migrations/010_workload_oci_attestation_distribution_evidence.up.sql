-- Idempotent online upgrade for sites that created workload_oci_attestations
-- before distribution-service admission/provenance evidence was joined.
ALTER TABLE verself.workload_oci_attestations
    ADD COLUMN IF NOT EXISTS distribution_artifact_digest String DEFAULT '' CODEC(ZSTD(3)) AFTER measured_digest;

ALTER TABLE verself.workload_oci_attestations
    ADD COLUMN IF NOT EXISTS distribution_artifact_state LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)) AFTER distribution_artifact_digest;

ALTER TABLE verself.workload_oci_attestations
    ADD COLUMN IF NOT EXISTS distribution_oci_repository String DEFAULT '' CODEC(ZSTD(3)) AFTER distribution_artifact_state;

ALTER TABLE verself.workload_oci_attestations
    ADD COLUMN IF NOT EXISTS distribution_public_oci_reference String DEFAULT '' CODEC(ZSTD(3)) AFTER distribution_oci_repository;

ALTER TABLE verself.workload_oci_attestations
    ADD COLUMN IF NOT EXISTS distribution_verification_decision LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)) AFTER distribution_public_oci_reference;

ALTER TABLE verself.workload_oci_attestations
    ADD COLUMN IF NOT EXISTS distribution_verification_reason String DEFAULT '' CODEC(ZSTD(3)) AFTER distribution_verification_decision;

ALTER TABLE verself.workload_oci_attestations
    ADD COLUMN IF NOT EXISTS distribution_policy_ref String DEFAULT '' CODEC(ZSTD(3)) AFTER distribution_verification_reason;

ALTER TABLE verself.workload_oci_attestations
    ADD COLUMN IF NOT EXISTS distribution_builder_id String DEFAULT '' CODEC(ZSTD(3)) AFTER distribution_policy_ref;

ALTER TABLE verself.workload_oci_attestations
    ADD COLUMN IF NOT EXISTS distribution_signer_identity String DEFAULT '' CODEC(ZSTD(3)) AFTER distribution_builder_id;

ALTER TABLE verself.workload_oci_attestations
    ADD COLUMN IF NOT EXISTS distribution_source_repository LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)) AFTER distribution_signer_identity;

ALTER TABLE verself.workload_oci_attestations
    ADD COLUMN IF NOT EXISTS distribution_source_commit String DEFAULT '' CODEC(ZSTD(3)) AFTER distribution_source_repository;

ALTER TABLE verself.workload_oci_attestations
    ADD COLUMN IF NOT EXISTS distribution_source_ref String DEFAULT '' CODEC(ZSTD(3)) AFTER distribution_source_commit;

ALTER TABLE verself.workload_oci_attestations
    ADD COLUMN IF NOT EXISTS slsa_predicate_type LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)) AFTER distribution_source_ref;

ALTER TABLE verself.workload_oci_attestations
    ADD COLUMN IF NOT EXISTS slsa_subject_digest String DEFAULT '' CODEC(ZSTD(3)) AFTER slsa_predicate_type;

ALTER TABLE verself.workload_oci_attestations
    ADD COLUMN IF NOT EXISTS slsa_document_digest String DEFAULT '' CODEC(ZSTD(3)) AFTER slsa_subject_digest;

ALTER TABLE verself.workload_oci_attestations
    ADD COLUMN IF NOT EXISTS slsa_oci_referrer_digest String DEFAULT '' CODEC(ZSTD(3)) AFTER slsa_document_digest;

GRANT INSERT ON verself.workload_oci_attestations TO nomad_observer;
