-- Add trace_id for distributed trace correlation.
-- ALTER TABLE ... ADD COLUMN IF NOT EXISTS is idempotent.
ALTER TABLE forge_metal.metering
    ADD COLUMN IF NOT EXISTS trace_id String DEFAULT '' CODEC(ZSTD(3));

ALTER TABLE forge_metal.sandbox_job_events
    ADD COLUMN IF NOT EXISTS trace_id String DEFAULT '' CODEC(ZSTD(3));
