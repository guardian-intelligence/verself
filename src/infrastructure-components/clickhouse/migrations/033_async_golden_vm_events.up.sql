ALTER TABLE verself.golden_vm_events
    ADD COLUMN IF NOT EXISTS from_state LowCardinality(String) DEFAULT '' AFTER reason,
    ADD COLUMN IF NOT EXISTS to_state LowCardinality(String) DEFAULT '' AFTER from_state,
    ADD COLUMN IF NOT EXISTS lease_id String DEFAULT '' CODEC(ZSTD(3)) AFTER to_state,
    ADD COLUMN IF NOT EXISTS exec_id String DEFAULT '' CODEC(ZSTD(3)) AFTER lease_id,
    ADD COLUMN IF NOT EXISTS river_job_id Int64 DEFAULT 0 CODEC(T64, ZSTD(3)) AFTER exec_id;

GRANT SELECT, INSERT ON verself.golden_vm_events TO sandbox_rental;
