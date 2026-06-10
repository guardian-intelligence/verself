-- Idempotent online upgrade: deployment-channel evidence verification is now
-- cryptographic and carries the derived key identity of the verifying ring key.
ALTER TABLE verself.workload_oci_attestations
    ADD COLUMN IF NOT EXISTS signing_key_id LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)) AFTER slsa_oci_referrer_digest;

GRANT INSERT ON verself.workload_oci_attestations TO nomad_observer;
